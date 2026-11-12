//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Dragging playlist rows out to Explorer needs real OLE drag and drop, which
// WM_DROPFILES cannot do. The player stays dependency free by implementing the
// smallest useful piece of COM: one IDataObject that only offers CF_HDROP, one
// IDropSource, and a call to DoDragDrop. Every drop target (Explorer, media
// players, DAWs) understands that data object.
//
// Only one drag can exist at a time and it always runs on the UI thread, so the
// payload and the COM identities live in package level variables instead of
// per-object state.

const (
	hrSOK                        = 0x00000000
	hrSFalse                     = 0x00000001
	hrENotImpl                   = 0x80004001
	hrEInvalidArg                = 0x80070057
	hrENoInterface               = 0x80004002
	hrEOutOfMemory               = 0x8007000E
	hrDVEFormatEtc               = 0x80040064
	hrOleEAdviseNotSupported     = 0x80040003
	hrOleEChangedMode            = 0x80010106
	hrDragDropSDrop              = 0x00040100
	hrDragDropSCancel            = 0x00040101
	hrDragDropSUseDefaultCursors = 0x00040102

	datadirGet = 1

	// formatEtcCount is how many formats the data object offers. The enumerator
	// below has to walk exactly this many entries and then report S_FALSE.
	formatEtcCount = 1
)

type formatEtc struct {
	CfFormat uint16
	Ptd      uintptr
	DwAspect uint32
	Lindex   int32
	Tymed    uint32
}

// stgMedium mirrors STGMEDIUM. Go inserts the same padding the C layout has on
// 64-bit Windows, so the field offsets the shell reads are correct.
type stgMedium struct {
	Tymed          uint32
	Value          uintptr
	PUnkForRelease uintptr
}

// dropFiles mirrors DROPFILES, the CF_HDROP payload header.
type dropFiles struct {
	PFiles uint32
	Pt     POINT
	FNC    int32
	FWide  int32
}

type oleGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func (g oleGUID) equals(ptr uintptr) bool {
	if ptr == 0 {
		return false
	}
	other := (*oleGUID)(unsafe.Pointer(ptr))
	return g.Data1 == other.Data1 && g.Data2 == other.Data2 && g.Data3 == other.Data3 && g.Data4 == other.Data4
}

var (
	iidIUnknown       = oleGUID{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDataObject    = oleGUID{0x0000010E, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropSource    = oleGUID{0x00000121, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIEnumFORMATETC = oleGUID{0x00000103, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// comObject is a bare COM object whose first word is its vtable pointer, which
// is exactly what every COM call site dereferences. The callbacks slice is kept
// as a field so the code thunks stay reachable for the process lifetime.
type comObject struct {
	slot      [1]uintptr
	callbacks []uintptr
}

func (o *comObject) install(methods ...any) {
	callbacks := make([]uintptr, 0, len(methods))
	for _, method := range methods {
		callbacks = append(callbacks, syscall.NewCallback(method))
	}
	o.installShared(callbacks)
}

// installShared publishes an already built callback array, so objects that
// differ only by instance state can share one vtable.
func (o *comObject) installShared(callbacks []uintptr) {
	o.callbacks = callbacks
	o.slot[0] = uintptr(unsafe.Pointer(&callbacks[0]))
}

func (o *comObject) ptr() uintptr { return uintptr(unsafe.Pointer(&o.slot[0])) }

// formatEnumerator is one IEnumFORMATETC cursor over the single CF_HDROP entry
// the data object advertises. A target walks it with
// `while (Next(1, &fe, &c) == S_OK)`, so the cursor has to reach the end and
// report S_FALSE; a stateless enumerator that answers S_OK forever puts the
// target in an infinite loop, and because those calls are dispatched onto this
// thread inside the DoDragDrop modal loop, the whole player stops responding.
type formatEnumerator struct {
	comObject
	cursor int
	refs   int
}

var (
	dropDataObject   comObject
	dropSourceObject comObject

	enumFormatCallbacks []uintptr

	// enumFormatMu guards formatEnumLive and the enumerator refcounts.
	enumFormatMu sync.Mutex
	// formatEnumLive pins every enumerator handed out to COM. The shell keeps a
	// raw pointer that the Go collector cannot see, so membership here is what
	// keeps a live enumerator from being collected.
	formatEnumLive = map[*formatEnumerator]struct{}{}

	// dragDropBlob is the CF_HDROP payload the data object renders on demand.
	// A target may keep the data object around past the drop, so the blob is
	// only replaced by the next drag rather than cleared.
	dragDropBlob []byte
)

func newFormatEnumerator(cursor int) *formatEnumerator {
	e := &formatEnumerator{cursor: cursor, refs: 1}
	e.installShared(enumFormatCallbacks)
	enumFormatMu.Lock()
	formatEnumLive[e] = struct{}{}
	enumFormatMu.Unlock()
	return e
}

func (e *formatEnumerator) asPtr() uintptr { return e.comObject.ptr() }

func enumFormatFromThis(this uintptr) *formatEnumerator {
	// comObject is the first field, so the interface pointer is the object.
	return (*formatEnumerator)(unsafe.Pointer(this))
}

func init() {
	dropDataObject.install(
		dataObjectQueryInterface,
		comAddRef,
		comRelease,
		dataObjectGetData,
		dataObjectGetDataHere,
		dataObjectQueryGetData,
		dataObjectGetCanonicalFormatEtc,
		dataObjectSetData,
		dataObjectEnumFormatEtc,
		dataObjectDAdvise,
		dataObjectDUnadvise,
		dataObjectEnumDAdvise,
	)
	dropSourceObject.install(
		dropSourceQueryInterface,
		comAddRef,
		comRelease,
		dropSourceQueryContinueDrag,
		dropSourceGiveFeedback,
	)
	// Enumerators are created per request and share one vtable, so their thunks
	// are built once here instead of leaking a set per instance.
	enumFormatCallbacks = make([]uintptr, 0, 7)
	for _, method := range []any{
		enumFormatQueryInterface,
		enumFormatAddRef,
		enumFormatRelease,
		enumFormatNext,
		enumFormatSkip,
		enumFormatReset,
		enumFormatClone,
	} {
		enumFormatCallbacks = append(enumFormatCallbacks, syscall.NewCallback(method))
	}
}

func hdropFormatSupported(fe *formatEtc) bool {
	if fe.CfFormat != CF_HDROP {
		return false
	}
	if fe.DwAspect != 0 && fe.DwAspect&DVASPECT_CONTENT == 0 {
		return false
	}
	if fe.Tymed != 0 && fe.Tymed&TYMED_HGLOBAL == 0 {
		return false
	}
	return true
}

func dataObjectQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrEInvalidArg
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	if iidIUnknown.equals(riid) || iidIDataObject.equals(riid) {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		return hrSOK
	}
	return hrENoInterface
}

func comAddRef(this uintptr) uintptr  { return 1 }
func comRelease(this uintptr) uintptr { return 1 }

func dataObjectGetData(this, pformatetc, pmedium uintptr) uintptr {
	if pformatetc == 0 || pmedium == 0 {
		return hrEInvalidArg
	}
	if !hdropFormatSupported((*formatEtc)(unsafe.Pointer(pformatetc))) {
		return hrDVEFormatEtc
	}
	handle := buildHDROPHandle(dragDropBlob)
	if handle == 0 {
		return hrEOutOfMemory
	}
	medium := (*stgMedium)(unsafe.Pointer(pmedium))
	medium.Tymed = TYMED_HGLOBAL
	medium.Value = handle
	medium.PUnkForRelease = 0
	return hrSOK
}

func dataObjectGetDataHere(this, pformatetc, pmedium uintptr) uintptr { return hrENotImpl }

func dataObjectQueryGetData(this, pformatetc uintptr) uintptr {
	if pformatetc == 0 {
		return hrEInvalidArg
	}
	if hdropFormatSupported((*formatEtc)(unsafe.Pointer(pformatetc))) {
		return hrSOK
	}
	return hrDVEFormatEtc
}

func dataObjectGetCanonicalFormatEtc(this, pformatetcIn, pformatetcOut uintptr) uintptr {
	return hrENotImpl
}

func dataObjectSetData(this, pformatetc, pmedium, release uintptr) uintptr { return hrENotImpl }

func dataObjectEnumFormatEtc(this, dwDirection, ppenum uintptr) uintptr {
	if ppenum == 0 {
		return hrEInvalidArg
	}
	*(*uintptr)(unsafe.Pointer(ppenum)) = 0
	if uint32(dwDirection) != datadirGet {
		return hrENotImpl
	}
	// Every request gets its own cursor, and the caller owns one reference.
	*(*uintptr)(unsafe.Pointer(ppenum)) = newFormatEnumerator(0).asPtr()
	return hrSOK
}

func dataObjectDAdvise(this, pformatetc, advf, pAdvSink, pdwConnection uintptr) uintptr {
	return hrOleEAdviseNotSupported
}

func dataObjectDUnadvise(this, dwConnection uintptr) uintptr { return hrOleEAdviseNotSupported }

func dataObjectEnumDAdvise(this, ppenum uintptr) uintptr { return hrOleEAdviseNotSupported }

func enumFormatQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrEInvalidArg
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	if !iidIUnknown.equals(riid) && !iidIEnumFORMATETC.equals(riid) {
		return hrENoInterface
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	enumFormatAddRef(this)
	return hrSOK
}

func enumFormatAddRef(this uintptr) uintptr {
	e := enumFormatFromThis(this)
	enumFormatMu.Lock()
	e.refs++
	refs := e.refs
	enumFormatMu.Unlock()
	return uintptr(refs)
}

func enumFormatRelease(this uintptr) uintptr {
	e := enumFormatFromThis(this)
	enumFormatMu.Lock()
	e.refs--
	refs := e.refs
	if refs <= 0 {
		delete(formatEnumLive, e)
	}
	enumFormatMu.Unlock()
	if refs < 0 {
		return 0
	}
	return uintptr(refs)
}

// enumFormatNext walks the single entry once. ULONG and BOOL arguments only
// have their low 32 bits defined on x64, so they are read back through uint32
// rather than trusted as full width uintptr values.
func enumFormatNext(this, celt, rgelt, pceltFetched uintptr) uintptr {
	count := uint32(celt)
	if pceltFetched != 0 {
		*(*uint32)(unsafe.Pointer(pceltFetched)) = 0
	}
	if count == 0 {
		return hrSOK
	}
	if rgelt == 0 {
		return hrEInvalidArg
	}
	e := enumFormatFromThis(this)
	fetched := int(count)
	if remaining := formatEtcCount - e.cursor; fetched > remaining {
		fetched = remaining
	}
	if fetched > 0 {
		fe := (*formatEtc)(unsafe.Pointer(rgelt))
		fe.CfFormat = CF_HDROP
		fe.Ptd = 0
		fe.DwAspect = DVASPECT_CONTENT
		fe.Lindex = -1
		fe.Tymed = TYMED_HGLOBAL
		e.cursor += fetched
	}
	if pceltFetched != 0 {
		*(*uint32)(unsafe.Pointer(pceltFetched)) = uint32(fetched)
	}
	if uint32(fetched) < count {
		return hrSFalse
	}
	return hrSOK
}

func enumFormatSkip(this, celt uintptr) uintptr {
	count := int(uint32(celt))
	e := enumFormatFromThis(this)
	skipped := count
	if remaining := formatEtcCount - e.cursor; skipped > remaining {
		skipped = remaining
	}
	e.cursor += skipped
	if skipped < count {
		return hrSFalse
	}
	return hrSOK
}

func enumFormatReset(this uintptr) uintptr {
	enumFormatFromThis(this).cursor = 0
	return hrSOK
}

func enumFormatClone(this, ppenum uintptr) uintptr {
	if ppenum == 0 {
		return hrEInvalidArg
	}
	*(*uintptr)(unsafe.Pointer(ppenum)) = 0
	e := enumFormatFromThis(this)
	*(*uintptr)(unsafe.Pointer(ppenum)) = newFormatEnumerator(e.cursor).asPtr()
	return hrSOK
}

func dropSourceQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrEInvalidArg
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	if iidIUnknown.equals(riid) || iidIDropSource.equals(riid) {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		return hrSOK
	}
	return hrENoInterface
}

func dropSourceQueryContinueDrag(this, escapePressed, keyState uintptr) uintptr {
	// fEscapePressed is a BOOL: only the low 32 bits are defined on x64, so
	// reading all 64 would see garbage in the upper half and cancel every drag.
	if uint32(escapePressed) != 0 {
		return hrDragDropSCancel
	}
	if uint32(keyState)&MK_LBUTTON == 0 {
		return hrDragDropSDrop
	}
	return hrSOK
}

func dropSourceGiveFeedback(this, effect uintptr) uintptr { return hrDragDropSUseDefaultCursors }

// dropFilesBytes renders the CF_HDROP payload: a fixed 20 byte header followed
// by the UTF-16 file list, each path NUL terminated plus a final empty string.
func dropFilesBytes(paths []string) []byte {
	var body []uint16
	for _, path := range paths {
		if path == "" {
			continue
		}
		body = append(body, syscall.StringToUTF16(path)...)
	}
	if len(body) == 0 {
		return nil
	}
	body = append(body, 0)
	header := dropFiles{PFiles: uint32(unsafe.Sizeof(dropFiles{})), FWide: 1}
	buf := make([]byte, int(header.PFiles)+len(body)*2)
	*(*dropFiles)(unsafe.Pointer(&buf[0])) = header
	for i, unit := range body {
		binary.LittleEndian.PutUint16(buf[int(header.PFiles)+i*2:], unit)
	}
	return buf
}

// buildHDROPHandle copies data into a moveable global memory block. The caller
// owns the block: it is either stored on the clipboard or handed to a drop
// target, both of which free it.
func buildHDROPHandle(data []byte) uintptr {
	if len(data) == 0 {
		return 0
	}
	handle, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
	if handle == 0 {
		return 0
	}
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		procGlobalFree.Call(handle)
		return 0
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(data)), data)
	procGlobalUnlock.Call(handle)
	return handle
}

// oleInitialize prepares the UI thread for OLE drag and drop.
func oleInitialize() bool {
	r, _, _ := procOleInitialize.Call(0)
	hr := int32(uint32(r))
	// S_FALSE means the apartment was already initialized, RPC_E_CHANGED_MODE
	// means someone initialized it with a different model; both still allow a
	// drag source to run.
	return hr == 0 || hr == 1 || uint32(hr) == hrOleEChangedMode
}

// doDragDropFiles hands the paths to the shell drag loop. It blocks until the
// user drops or cancels, so it must be called from the UI thread.
func doDragDropFiles(paths []string) uint32 {
	blob := dropFilesBytes(paths)
	if len(blob) == 0 {
		return DROPEFFECT_NONE
	}
	dragDropBlob = blob
	effect := uint32(DROPEFFECT_NONE)
	hr, _, _ := procDoDragDrop.Call(
		dropDataObject.ptr(),
		dropSourceObject.ptr(),
		uintptr(DROPEFFECT_COPY),
		uintptr(unsafe.Pointer(&effect)),
	)
	if int32(uint32(hr)) < 0 {
		return DROPEFFECT_NONE
	}
	return effect
}

// copyFilesToClipboard publishes the paths as CF_HDROP so Explorer can paste
// them with Ctrl+V. The clipboard owns the memory block afterwards.
func copyFilesToClipboard(hwnd uintptr, paths []string) error {
	data := dropFilesBytes(paths)
	if len(data) == 0 {
		return errors.New("没有可复制的文件")
	}
	for attempt := 0; attempt < 3; attempt++ {
		opened, _, _ := procOpenClipboard.Call(hwnd)
		if opened == 0 {
			time.Sleep(30 * time.Millisecond)
			continue
		}
		procEmptyClipboard.Call()
		handle := buildHDROPHandle(data)
		if handle == 0 {
			procCloseClipboard.Call()
			return errors.New("无法分配剪贴板数据")
		}
		if stored, _, _ := procSetClipboardData.Call(CF_HDROP, handle); stored == 0 {
			procCloseClipboard.Call()
			procGlobalFree.Call(handle)
			return errors.New("剪贴板被其他程序占用")
		}
		procCloseClipboard.Call()
		return nil
	}
	return errors.New("剪贴板被其他程序占用")
}
