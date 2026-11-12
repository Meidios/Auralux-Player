//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"syscall"
	"testing"
	"unsafe"
)

func decodeHDROPPayload(t *testing.T, data []byte) []string {
	t.Helper()
	if len(data) < int(unsafe.Sizeof(dropFiles{})) {
		t.Fatalf("payload is shorter than the DROPFILES header: %d bytes", len(data))
	}
	header := (*dropFiles)(unsafe.Pointer(&data[0]))
	if header.PFiles != uint32(unsafe.Sizeof(dropFiles{})) {
		t.Fatalf("pFiles = %d, want %d", header.PFiles, unsafe.Sizeof(dropFiles{}))
	}
	units := make([]uint16, 0, (len(data)-int(header.PFiles))/2)
	for offset := int(header.PFiles); offset+1 < len(data); offset += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[offset:]))
	}
	var paths []string
	current := make([]uint16, 0, 8)
	for _, unit := range units {
		if unit == 0 {
			if len(current) == 0 {
				break
			}
			paths = append(paths, syscall.UTF16ToString(current))
			current = current[:0]
			continue
		}
		current = append(current, unit)
	}
	return paths
}

func TestDropFilesBytesHeaderAndTermination(t *testing.T) {
	paths := []string{`C:\Audio\kick.wav`, `D:\Sessions\最终混音 1.mp3`}
	data := dropFilesBytes(paths)
	if len(data) == 0 {
		t.Fatal("dropFilesBytes returned no payload")
	}

	header := (*dropFiles)(unsafe.Pointer(&data[0]))
	if header.PFiles != 20 {
		t.Fatalf("pFiles = %d, want 20", header.PFiles)
	}
	if header.FWide != 1 {
		t.Fatalf("fWide = %d, want 1", header.FWide)
	}
	if header.FNC != 0 {
		t.Fatalf("fNC = %d, want 0", header.FNC)
	}

	// Every path is NUL terminated and the list ends with an extra empty string.
	units := (len(data) - int(header.PFiles)) / 2
	wantUnits := 0
	for _, path := range paths {
		wantUnits += len(syscall.StringToUTF16(path))
	}
	wantUnits++
	if units != wantUnits {
		t.Fatalf("payload holds %d UTF-16 units, want %d", units, wantUnits)
	}
	if tail := binary.LittleEndian.Uint16(data[len(data)-2:]); tail != 0 {
		t.Fatalf("payload does not end with a NUL terminator")
	}
	if tail := binary.LittleEndian.Uint16(data[len(data)-4:]); tail != 0 {
		t.Fatalf("payload does not end with a double NUL terminator")
	}

	if got := decodeHDROPPayload(t, data); !reflect.DeepEqual(got, paths) {
		t.Fatalf("decoded paths = %v, want %v", got, paths)
	}
}

func TestDropFilesBytesSkipsEmptyPaths(t *testing.T) {
	data := dropFilesBytes([]string{"", ""})
	if data != nil {
		t.Fatalf("empty path list produced %d bytes", len(data))
	}
	if len(dropFilesBytes(nil)) != 0 {
		t.Fatal("nil path list produced a payload")
	}
}

func TestDropFilesBytesSinglePath(t *testing.T) {
	data := dropFilesBytes([]string{`C:\a.wav`})
	if got, want := decodeHDROPPayload(t, data), []string{`C:\a.wav`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded paths = %v, want %v", got, want)
	}
}

func TestHdropFormatEtcSupport(t *testing.T) {
	accepted := []formatEtc{
		{CfFormat: CF_HDROP},
		{CfFormat: CF_HDROP, DwAspect: DVASPECT_CONTENT, Tymed: TYMED_HGLOBAL},
	}
	for _, fe := range accepted {
		if !hdropFormatSupported(&fe) {
			t.Errorf("format %#v should be accepted", fe)
		}
	}
	rejected := []formatEtc{
		{CfFormat: 1, DwAspect: DVASPECT_CONTENT, Tymed: TYMED_HGLOBAL},
		{CfFormat: CF_HDROP, DwAspect: 4},
		{CfFormat: CF_HDROP, Tymed: 2},
	}
	for _, fe := range rejected {
		if hdropFormatSupported(&fe) {
			t.Errorf("format %#v should be rejected", fe)
		}
	}
}

// The COM identities are hand rolled, so the vtable shape is worth pinning: the
// first word has to point at the callback array, and each object must publish
// exactly the slots its interface declares.
func TestComVtableLayoutMatchesInterfaces(t *testing.T) {
	enumerator := newFormatEnumerator(0)
	for _, test := range []struct {
		name    string
		object  *comObject
		methods int
	}{
		// IUnknown (3) + IDataObject (9: GetData, GetDataHere, QueryGetData,
		// GetCanonicalFormatEtc, SetData, EnumFormatEtc, DAdvise, DUnadvise,
		// EnumDAdvise).
		{"IDataObject", &dropDataObject, 12},
		{"IDropSource", &dropSourceObject, 5},
		// IUnknown (3) + IEnumFORMATETC (4: Next, Skip, Reset, Clone).
		{"IEnumFORMATETC", &enumerator.comObject, 7},
	} {
		if len(test.object.callbacks) != test.methods {
			t.Errorf("%s publishes %d vtable slots, want %d", test.name, len(test.object.callbacks), test.methods)
		}
		if want := uintptr(unsafe.Pointer(&test.object.callbacks[0])); test.object.slot[0] != want {
			t.Errorf("%s vtable pointer does not lead to its callback array", test.name)
		}
		if test.object.ptr() != uintptr(unsafe.Pointer(&test.object.slot[0])) {
			t.Errorf("%s interface pointer is not its own first word", test.name)
		}
	}

	// Enumerators are separate objects with shared code: one vtable, distinct
	// identities, so their cursors cannot collide.
	other := newFormatEnumerator(0)
	if enumerator.slot[0] != other.slot[0] {
		t.Fatal("enumerators do not share one vtable")
	}
	if enumerator.asPtr() == other.asPtr() {
		t.Fatal("two enumerators were handed out as the same object")
	}
}

func TestDataObjectQueryInterface(t *testing.T) {
	var ppv uintptr
	for _, iid := range []oleGUID{iidIUnknown, iidIDataObject} {
		given := iid
		if hr := dataObjectQueryInterface(dropDataObject.ptr(), uintptr(unsafe.Pointer(&given)), uintptr(unsafe.Pointer(&ppv))); hr != hrSOK {
			t.Fatalf("QueryInterface returned 0x%08X, want S_OK", uint32(hr))
		}
		if ppv != dropDataObject.ptr() {
			t.Fatalf("QueryInterface returned %#x, want the object itself", ppv)
		}
	}
	other := oleGUID{Data1: 0xDEADBEEF}
	if hr := dataObjectQueryInterface(dropDataObject.ptr(), uintptr(unsafe.Pointer(&other)), uintptr(unsafe.Pointer(&ppv))); hr != hrENoInterface {
		t.Fatalf("QueryInterface accepted an unknown IID: 0x%08X", uint32(hr))
	}
	if ppv != 0 {
		t.Fatalf("QueryInterface left a stale interface pointer on failure")
	}
}

func TestEnumFormatEtcAdvertisesHDROP(t *testing.T) {
	var enum uintptr
	if hr := dataObjectEnumFormatEtc(dropDataObject.ptr(), datadirGet, uintptr(unsafe.Pointer(&enum))); hr != hrSOK || enum == 0 {
		t.Fatalf("EnumFormatEtc(DATADIR_GET) = 0x%08X, enum=%#x", uint32(hr), enum)
	}
	var second uintptr
	if hr := dataObjectEnumFormatEtc(dropDataObject.ptr(), datadirGet, uintptr(unsafe.Pointer(&second))); hr != hrSOK || second == 0 {
		t.Fatalf("second EnumFormatEtc(DATADIR_GET) = 0x%08X, enum=%#x", uint32(hr), second)
	}
	if second == enum {
		t.Fatal("EnumFormatEtc reused one object, so callers would share a cursor")
	}
	// DATADIR_SET clears its out parameter, so it must not share a variable with
	// the enumerator the test keeps using.
	var unsupported uintptr
	if hr := dataObjectEnumFormatEtc(dropDataObject.ptr(), 2, uintptr(unsafe.Pointer(&unsupported))); hr != hrENotImpl || unsupported != 0 {
		t.Fatalf("EnumFormatEtc(DATADIR_SET) should not be implemented")
	}
	var fe formatEtc
	var fetched uint32
	if hr := enumFormatNext(enum, 1, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSOK || fetched != 1 {
		t.Fatalf("Next() = 0x%08X, fetched=%d", uint32(hr), fetched)
	}
	if !hdropFormatSupported(&fe) {
		t.Fatalf("enumerated format is not accepted by GetData: %#v", fe)
	}
	// Walking the other enumerator starts from its own cursor, not the first.
	if hr := enumFormatNext(second, 1, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSOK || fetched != 1 {
		t.Fatalf("Next() on the second enumerator = 0x%08X, fetched=%d", uint32(hr), fetched)
	}
}

// A drop target consumes the enumerator with `while (Next(1, &fe, &c) == S_OK)`.
// An enumerator that keeps answering S_OK spins that loop forever, and because
// the calls are dispatched onto the UI thread inside the DoDragDrop modal loop,
// the player stops responding. This is the regression pin for that freeze.
func TestEnumFormatEtcTerminatesForDropTargets(t *testing.T) {
	enum := newFormatEnumerator(0).asPtr()
	var fe formatEtc
	var fetched uint32
	seen := 0
	for i := 0; i <= formatEtcCount; i++ {
		hr := enumFormatNext(enum, 1, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched)))
		if hr != hrSOK {
			if hr != hrSFalse {
				t.Fatalf("Next() = 0x%08X, want S_FALSE at the end of the list", uint32(hr))
			}
			break
		}
		seen++
		if fetched != 1 {
			t.Fatalf("element %d reported %d fetched, want 1", seen, fetched)
		}
		if !hdropFormatSupported(&fe) {
			t.Fatalf("element %d is not a CF_HDROP format: %#v", seen, fe)
		}
	}
	if seen != formatEtcCount {
		t.Fatalf("enumerator produced %d entries and never ended; drop targets would loop forever", seen)
	}
	if fetched != 0 {
		t.Fatalf("the final Next() reported %d fetched, want 0", fetched)
	}
}

func TestEnumFormatEtcCursorOperations(t *testing.T) {
	enum := newFormatEnumerator(0).asPtr()
	var fe formatEtc
	var fetched uint32

	// Requesting more than the list holds reports S_FALSE and what it could give.
	if hr := enumFormatNext(enum, 2, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSFalse || fetched != 1 {
		t.Fatalf("Next(2) = 0x%08X, fetched=%d, want S_FALSE and 1", uint32(hr), fetched)
	}
	if hr := enumFormatNext(enum, 1, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSFalse || fetched != 0 {
		t.Fatalf("Next() past the end = 0x%08X, fetched=%d, want S_FALSE and 0", uint32(hr), fetched)
	}
	if hr := enumFormatNext(enum, 0, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSOK {
		t.Fatalf("Next(0) = 0x%08X, want S_OK", uint32(hr))
	}

	// The upper half of a ULONG argument is undefined on x64.
	garbageCelt := uintptr(1) | uintptr(0xDEADBEEF)<<32
	if hr := enumFormatReset(enum); hr != hrSOK {
		t.Fatalf("Reset() = 0x%08X, want S_OK", uint32(hr))
	}
	if hr := enumFormatNext(enum, garbageCelt, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSOK || fetched != 1 {
		t.Fatalf("Next with junk in the upper bits = 0x%08X, fetched=%d, want S_OK and 1", uint32(hr), fetched)
	}

	// Skip reports whether it could move as far as asked.
	enumFormatReset(enum)
	if hr := enumFormatSkip(enum, 1); hr != hrSOK {
		t.Fatalf("Skip(1) = 0x%08X, want S_OK", uint32(hr))
	}
	if hr := enumFormatSkip(enum, 1); hr != hrSFalse {
		t.Fatalf("Skip(1) past the end = 0x%08X, want S_FALSE", uint32(hr))
	}
	if hr := enumFormatSkip(enum, 0); hr != hrSOK {
		t.Fatalf("Skip(0) = 0x%08X, want S_OK", uint32(hr))
	}

	// A clone inherits the position and keeps its own cursor; releasing it drops
	// the reference the caller was handed.
	enumFormatReset(enum)
	if hr := enumFormatSkip(enum, 1); hr != hrSOK {
		t.Fatalf("Skip(1) = 0x%08X, want S_OK", uint32(hr))
	}
	var clone uintptr
	if hr := enumFormatClone(enum, uintptr(unsafe.Pointer(&clone))); hr != hrSOK || clone == 0 {
		t.Fatalf("Clone() = 0x%08X, clone=%#x", uint32(hr), clone)
	}
	if clone == enum {
		t.Fatal("Clone returned the original object")
	}
	if hr := enumFormatNext(clone, 1, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&fetched))); hr != hrSFalse {
		t.Fatalf("clone did not inherit the cursor: 0x%08X", uint32(hr))
	}
	if hr := enumFormatRelease(clone); hr != 0 {
		t.Fatalf("Release() on an enumerator = %d, want 0", hr)
	}
}

func TestFormatEnumeratorQueryInterfaceRefCounts(t *testing.T) {
	enum := newFormatEnumerator(0).asPtr()
	var ppv uintptr
	for _, iid := range []oleGUID{iidIUnknown, iidIEnumFORMATETC} {
		given := iid
		if hr := enumFormatQueryInterface(enum, uintptr(unsafe.Pointer(&given)), uintptr(unsafe.Pointer(&ppv))); hr != hrSOK || ppv != enum {
			t.Fatalf("QueryInterface(0x%08X) = 0x%08X, ptr=%#x", given.Data1, uint32(hr), ppv)
		}
	}
	other := oleGUID{Data1: 0xDEADBEEF}
	if hr := enumFormatQueryInterface(enum, uintptr(unsafe.Pointer(&other)), uintptr(unsafe.Pointer(&ppv))); hr != hrENoInterface || ppv != 0 {
		t.Fatalf("QueryInterface accepted an unknown IID: 0x%08X, ptr=%#x", uint32(hr), ppv)
	}
	// One initial reference plus the two QueryInterface references taken above.
	for i := 0; i < 2; i++ {
		if hr := enumFormatRelease(enum); hr != uintptr(2-i) {
			t.Fatalf("Release() = %d, want %d", hr, 2-i)
		}
	}
	if hr := enumFormatRelease(enum); hr != 0 {
		t.Fatalf("final Release() = %d, want 0", hr)
	}
}

// GetData is the path Explorer actually calls, so lock down the STGMEDIUM it
// hands over and the bytes inside the HGLOBAL.
func TestDataObjectGetDataDeliversHDROPMedium(t *testing.T) {
	previous := dragDropBlob
	defer func() { dragDropBlob = previous }()
	paths := []string{`C:\Audio\kick.wav`, `C:\Audio\snare.wav`}
	dragDropBlob = dropFilesBytes(paths)

	fe := formatEtc{CfFormat: CF_HDROP, DwAspect: DVASPECT_CONTENT, Tymed: TYMED_HGLOBAL}
	var medium stgMedium
	if hr := dataObjectGetData(dropDataObject.ptr(), uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&medium))); hr != hrSOK {
		t.Fatalf("GetData returned 0x%08X, want S_OK", uint32(hr))
	}
	if medium.Tymed != TYMED_HGLOBAL || medium.Value == 0 || medium.PUnkForRelease != 0 {
		t.Fatalf("unexpected STGMEDIUM: %#v", medium)
	}
	defer procGlobalFree.Call(medium.Value)

	ptr, _, _ := procGlobalLock.Call(medium.Value)
	if ptr == 0 {
		t.Fatal("GetData handed over a block that cannot be locked")
	}
	got := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(dragDropBlob))
	procGlobalUnlock.Call(medium.Value)
	if !bytes.Equal(got, dragDropBlob) {
		t.Fatal("STGMEDIUM content does not match the CF_HDROP payload")
	}

	bad := formatEtc{CfFormat: 1, DwAspect: DVASPECT_CONTENT, Tymed: TYMED_HGLOBAL}
	if hr := dataObjectGetData(dropDataObject.ptr(), uintptr(unsafe.Pointer(&bad)), uintptr(unsafe.Pointer(&medium))); hr != hrDVEFormatEtc {
		t.Fatalf("GetData accepted an unsupported format: 0x%08X", uint32(hr))
	}
}

func TestDropSourceDragDecisions(t *testing.T) {
	this := dropSourceObject.ptr()
	if hr := dropSourceQueryContinueDrag(this, 1, MK_LBUTTON); hr != hrDragDropSCancel {
		t.Fatalf("Esc during a drag = 0x%08X, want DRAGDROP_S_CANCEL", uint32(hr))
	}
	if hr := dropSourceQueryContinueDrag(this, 0, 0); hr != hrDragDropSDrop {
		t.Fatalf("released button = 0x%08X, want DRAGDROP_S_DROP", uint32(hr))
	}
	if hr := dropSourceQueryContinueDrag(this, 0, MK_LBUTTON); hr != hrSOK {
		t.Fatalf("still held = 0x%08X, want S_OK", uint32(hr))
	}
	if hr := dropSourceGiveFeedback(this, DROPEFFECT_COPY); hr != hrDragDropSUseDefaultCursors {
		t.Fatalf("GiveFeedback = 0x%08X, want DRAGDROP_S_USEDEFAULTCURSORS", uint32(hr))
	}

	// BOOL and DWORD arguments only define their low 32 bits on x64, so junk in
	// the upper half must not be mistaken for a pressed Esc or a held button.
	junk := uintptr(0xFFFFFFFF) << 32
	if hr := dropSourceQueryContinueDrag(this, junk, MK_LBUTTON); hr != hrSOK {
		t.Fatalf("junk above fEscapePressed cancelled the drag: 0x%08X", uint32(hr))
	}
	if hr := dropSourceQueryContinueDrag(this, 0, junk); hr != hrDragDropSDrop {
		t.Fatalf("junk above grfKeyState kept a released drag alive: 0x%08X", uint32(hr))
	}
}

// The shell never calls these handlers as Go functions: it reads the vtable out
// of the interface pointer and performs a real COM call. The tests above call
// the handlers directly, so this one drives the published vtable the way a drop
// target does, including the `while (Next(1, &fe, &c) == S_OK)` walk that used
// to spin forever.
func TestDropTargetConsumesPublishedVtable(t *testing.T) {
	previous := dragDropBlob
	defer func() { dragDropBlob = previous }()
	paths := []string{`C:\Audio\kick.wav`, `C:\Audio\snare.wav`}
	dragDropBlob = dropFilesBytes(paths)

	// Slots are numbered the way COM numbers them: the three IUnknown methods
	// first, then the interface's own methods.
	const (
		slotQueryInterface = 0
		slotGetData        = 3
		slotQueryGetData   = 5
		slotEnumFormatEtc  = 8
		slotEnumRelease    = 2
		slotEnumNext       = 3
	)
	call := func(iface uintptr, slot int, args ...uintptr) uintptr {
		t.Helper()
		vtable := *(*uintptr)(unsafe.Pointer(iface))
		method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
		result, _, _ := syscall.SyscallN(method, args...)
		return result
	}

	dataObject := dropDataObject.ptr()
	iid := iidIDataObject
	var queried uintptr
	if hr := call(dataObject, slotQueryInterface, dataObject, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&queried))); hr != hrSOK || queried != dataObject {
		t.Fatalf("vtable QueryInterface = 0x%08X, ptr=%#x", uint32(hr), queried)
	}

	var enumerator uintptr
	if hr := call(dataObject, slotEnumFormatEtc, dataObject, datadirGet, uintptr(unsafe.Pointer(&enumerator))); hr != hrSOK || enumerator == 0 {
		t.Fatalf("vtable EnumFormatEtc = 0x%08X, enum=%#x", uint32(hr), enumerator)
	}

	var format formatEtc
	var fetched uint32
	seen := 0
	for {
		hr := call(enumerator, slotEnumNext, enumerator, 1, uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&fetched)))
		if hr != hrSOK {
			if hr != hrSFalse {
				t.Fatalf("vtable Next = 0x%08X, want S_OK or S_FALSE", uint32(hr))
			}
			break
		}
		seen++
		if seen > formatEtcCount {
			t.Fatal("the vtable enumerator never ended, so a drop target would hang")
		}
		if !hdropFormatSupported(&format) {
			t.Fatalf("the vtable enumerated an unusable format: %#v", format)
		}
		if hr := call(dataObject, slotQueryGetData, dataObject, uintptr(unsafe.Pointer(&format))); hr != hrSOK {
			t.Fatalf("vtable QueryGetData on an enumerated format = 0x%08X", uint32(hr))
		}
	}
	if seen != formatEtcCount || fetched != 0 {
		t.Fatalf("vtable walk produced %d entries with %d fetched at the end", seen, fetched)
	}

	var medium stgMedium
	if hr := call(dataObject, slotGetData, dataObject, uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&medium))); hr != hrSOK {
		t.Fatalf("vtable GetData = 0x%08X", uint32(hr))
	}
	if medium.Tymed != TYMED_HGLOBAL || medium.Value == 0 || medium.PUnkForRelease != 0 {
		t.Fatalf("unexpected STGMEDIUM from the vtable: %#v", medium)
	}
	defer procGlobalFree.Call(medium.Value)
	ptr, _, _ := procGlobalLock.Call(medium.Value)
	if ptr == 0 {
		t.Fatal("vtable GetData handed over a block that cannot be locked")
	}
	got := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(dragDropBlob))
	procGlobalUnlock.Call(medium.Value)
	if !bytes.Equal(got, dragDropBlob) {
		t.Fatal("vtable GetData content does not match the CF_HDROP payload")
	}

	// The enumerator reference EnumFormatEtc handed out belongs to the caller.
	if hr := call(enumerator, slotEnumRelease, enumerator); hr != 0 {
		t.Fatalf("vtable Release on the enumerator = %d, want 0", hr)
	}
}
