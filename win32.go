//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	CW_USEDEFAULT    = 0x80000000
	SW_SHOW          = 5
	SW_SHOWMAXIMIZED = 3
	SW_RESTORE       = 9

	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_THICKFRAME       = 0x00040000
	WS_MINIMIZEBOX      = 0x00020000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000
	WS_OVERLAPPEDWINDOW = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX
	WS_EX_DLGMODALFRAME = 0x00000001
	WS_EX_CLIENTEDGE    = 0x00000200

	ES_AUTOHSCROLL   = 0x0080
	BS_PUSHBUTTON    = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001

	CS_VREDRAW  = 0x0001
	CS_HREDRAW  = 0x0002
	CS_DBLCLKS  = 0x0008
	IDC_ARROW   = 32512
	IDC_SIZEWE  = 32644
	IDC_SIZENS  = 32645
	IDI_APPICON = 101

	WM_CREATE        = 0x0001
	WM_DESTROY       = 0x0002
	WM_SIZE          = 0x0005
	WM_KILLFOCUS     = 0x0008
	WM_PAINT         = 0x000F
	WM_CLOSE         = 0x0010
	WM_COMMAND       = 0x0111
	WM_CTLCOLOREDIT  = 0x0133
	WM_COPYDATA      = 0x004A
	WM_ERASEBKGND    = 0x0014
	WM_SETCURSOR     = 0x0020
	WM_GETMINMAXINFO = 0x0024
	WM_KEYDOWN       = 0x0100
	WM_SETFONT       = 0x0030
	WM_TIMER         = 0x0113
	WM_MOUSEMOVE     = 0x0200
	WM_LBUTTONDOWN   = 0x0201
	WM_LBUTTONUP     = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205
	WM_MOUSEWHEEL    = 0x020A
	WM_MOUSELEAVE    = 0x02A3
	WM_DROPFILES     = 0x0233
	WM_DPICHANGED    = 0x02E0
	WM_APP           = 0x8000

	MK_SHIFT   = 0x0004
	MK_CONTROL = 0x0008
	MK_LBUTTON = 0x0001
	VK_MENU    = 0x12
	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11

	VK_SPACE  = 0x20
	VK_RETURN = 0x0D
	VK_LEFT   = 0x25
	VK_RIGHT  = 0x27
	VK_UP     = 0x26
	VK_DOWN   = 0x28
	VK_DELETE = 0x2E
	VK_HOME   = 0x24
	VK_END    = 0x23
	VK_ESCAPE = 0x1B

	DT_LEFT         = 0x00000000
	DT_CENTER       = 0x00000001
	DT_RIGHT        = 0x00000002
	DT_VCENTER      = 0x00000004
	DT_SINGLELINE   = 0x00000020
	DT_END_ELLIPSIS = 0x00008000
	DT_CALCRECT     = 0x00000400
	DT_NOPREFIX     = 0x00000800

	TRANSPARENT     = 1
	PS_SOLID        = 0
	PS_GEOMETRIC    = 0x00010000
	PS_ENDCAP_ROUND = 0x00000000
	PS_JOIN_ROUND   = 0x00000000
	BS_SOLID        = 0
	SRCCOPY         = 0x00CC0020
	MM_TEXT         = 1
	MM_ANISOTROPIC  = 8
	HALFTONE        = 4
	DIB_RGB_COLORS  = 0
	BI_RGB          = 0
	AC_SRC_OVER     = 0
	AC_SRC_ALPHA    = 1

	FW_NORMAL         = 400
	FW_MEDIUM         = 500
	FW_SEMIBOLD       = 600
	FW_BOLD           = 700
	DEFAULT_CHARSET   = 1
	CLEARTYPE_QUALITY = 5

	OFN_ALLOWMULTISELECT = 0x00000200
	OFN_EXPLORER         = 0x00080000
	OFN_FILEMUSTEXIST    = 0x00001000
	OFN_PATHMUSTEXIST    = 0x00000800
	OFN_HIDEREADONLY     = 0x00000004

	LR_LOADFROMFILE = 0x0010
	IMAGE_BITMAP    = 0
	IMAGE_ICON      = 1

	MF_STRING     = 0x0000
	MF_GRAYED     = 0x0001
	MF_CHECKED    = 0x0008
	MF_SEPARATOR  = 0x0800
	TPM_LEFTALIGN = 0x0000
	TPM_TOPALIGN  = 0x0000
	TPM_RETURNCMD = 0x0100

	MB_OK              = 0x00000000
	MB_YESNO           = 0x00000004
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040
	MB_ICONQUESTION    = 0x00000020
	IDYES              = 6
	IDOK               = 1
	IDCANCEL           = 2

	GWL_STYLE = -16

	WHEEL_DELTA = 120
	TME_LEAVE   = 0x00000002

	SPI_GETWORKAREA       = 0x0030
	MONITOR_DEFAULTTONULL = 0x00000000

	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010

	// Shell / OLE constants used for dragging playlist rows out to Explorer.
	CF_HDROP         = 15
	GMEM_MOVEABLE    = 0x0002
	DROPEFFECT_NONE  = 0x00000000
	DROPEFFECT_COPY  = 0x00000001
	DVASPECT_CONTENT = 1
	TYMED_HGLOBAL    = 1
)

type POINT struct{ X, Y int32 }

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type LOGBRUSH struct {
	LbStyle uint32
	LbColor uint32
	LbHatch uintptr
}
type RECT struct{ Left, Top, Right, Bottom int32 }
type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}
type COPYDATASTRUCT struct {
	DwData uintptr
	CbData uint32
	LpData uintptr
}

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
type MINMAXINFO struct {
	PtReserved     POINT
	PtMaxSize      POINT
	PtMaxPosition  POINT
	PtMinTrackSize POINT
	PtMaxTrackSize POINT
}
type WINDOWPLACEMENT struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    POINT
	PtMaxPosition    POINT
	RcNormalPosition RECT
}
type OPENFILENAME struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}
type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modComdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modDwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	modMsimg32  = syscall.NewLazyDLL("msimg32.dll")
	modWinmm    = syscall.NewLazyDLL("winmm.dll")
	modOle32    = syscall.NewLazyDLL("ole32.dll")

	procRegisterClassExW          = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW           = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW            = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow             = modUser32.NewProc("DestroyWindow")
	procEnableWindow              = modUser32.NewProc("EnableWindow")
	procShowWindow                = modUser32.NewProc("ShowWindow")
	procUpdateWindow              = modUser32.NewProc("UpdateWindow")
	procGetMessageW               = modUser32.NewProc("GetMessageW")
	procTranslateMessage          = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW          = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage           = modUser32.NewProc("PostQuitMessage")
	procBeginPaint                = modUser32.NewProc("BeginPaint")
	procEndPaint                  = modUser32.NewProc("EndPaint")
	procGetClientRect             = modUser32.NewProc("GetClientRect")
	procGetWindowRect             = modUser32.NewProc("GetWindowRect")
	procGetWindowTextLengthW      = modUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW            = modUser32.NewProc("GetWindowTextW")
	procInvalidateRect            = modUser32.NewProc("InvalidateRect")
	procSetTimer                  = modUser32.NewProc("SetTimer")
	procKillTimer                 = modUser32.NewProc("KillTimer")
	procLoadCursorW               = modUser32.NewProc("LoadCursorW")
	procLoadIconW                 = modUser32.NewProc("LoadIconW")
	procLoadImageW                = modUser32.NewProc("LoadImageW")
	procSetWindowTextW            = modUser32.NewProc("SetWindowTextW")
	procPostMessageW              = modUser32.NewProc("PostMessageW")
	procSetCapture                = modUser32.NewProc("SetCapture")
	procSetCursor                 = modUser32.NewProc("SetCursor")
	procReleaseCapture            = modUser32.NewProc("ReleaseCapture")
	procGetCursorPos              = modUser32.NewProc("GetCursorPos")
	procScreenToClient            = modUser32.NewProc("ScreenToClient")
	procTrackMouseEvent           = modUser32.NewProc("TrackMouseEvent")
	procMessageBoxW               = modUser32.NewProc("MessageBoxW")
	procSetProcessDPIAware        = modUser32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwarenessCtx = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow           = modUser32.NewProc("GetDpiForWindow")
	procGetDpiForSystem           = modUser32.NewProc("GetDpiForSystem")
	procSetWindowPos              = modUser32.NewProc("SetWindowPos")
	procGetWindowPlacement        = modUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement        = modUser32.NewProc("SetWindowPlacement")
	procSystemParametersInfoW     = modUser32.NewProc("SystemParametersInfoW")
	procMonitorFromRect           = modUser32.NewProc("MonitorFromRect")
	procCreatePopupMenu           = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW               = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu            = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu               = modUser32.NewProc("DestroyMenu")
	procClientToScreen            = modUser32.NewProc("ClientToScreen")
	procFindWindowW               = modUser32.NewProc("FindWindowW")
	procSendMessageW              = modUser32.NewProc("SendMessageW")
	procSetForegroundWindow       = modUser32.NewProc("SetForegroundWindow")
	procSetFocus                  = modUser32.NewProc("SetFocus")
	procIsDialogMessageW          = modUser32.NewProc("IsDialogMessageW")
	procGetKeyState               = modUser32.NewProc("GetKeyState")
	procOpenClipboard             = modUser32.NewProc("OpenClipboard")
	procEmptyClipboard            = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData          = modUser32.NewProc("SetClipboardData")
	procCloseClipboard            = modUser32.NewProc("CloseClipboard")

	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procSaveDC                 = modGdi32.NewProc("SaveDC")
	procRestoreDC              = modGdi32.NewProc("RestoreDC")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procIntersectClipRect      = modGdi32.NewProc("IntersectClipRect")
	procCreateDIBSection       = modGdi32.NewProc("CreateDIBSection")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procCreateSolidBrush       = modGdi32.NewProc("CreateSolidBrush")
	procCreatePen              = modGdi32.NewProc("CreatePen")
	procExtCreatePen           = modGdi32.NewProc("ExtCreatePen")
	procRectangle              = modGdi32.NewProc("Rectangle")
	procRoundRect              = modGdi32.NewProc("RoundRect")
	procEllipse                = modGdi32.NewProc("Ellipse")
	procMoveToEx               = modGdi32.NewProc("MoveToEx")
	procLineTo                 = modGdi32.NewProc("LineTo")
	procPolygon                = modGdi32.NewProc("Polygon")
	procPolyline               = modGdi32.NewProc("Polyline")
	procPolyPolyline           = modGdi32.NewProc("PolyPolyline")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procSetTextColor           = modGdi32.NewProc("SetTextColor")
	procSetBkColor             = modGdi32.NewProc("SetBkColor")
	procSetBkMode              = modGdi32.NewProc("SetBkMode")
	procCreateFontW            = modGdi32.NewProc("CreateFontW")
	procGetStockObject         = modGdi32.NewProc("GetStockObject")
	procSetMapMode             = modGdi32.NewProc("SetMapMode")
	procSetWindowExtEx         = modGdi32.NewProc("SetWindowExtEx")
	procSetViewportExtEx       = modGdi32.NewProc("SetViewportExtEx")
	procSetStretchBltMode      = modGdi32.NewProc("SetStretchBltMode")

	procDrawTextW = modUser32.NewProc("DrawTextW")
	procFillRect  = modUser32.NewProc("FillRect")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procCreateMutexW     = modKernel32.NewProc("CreateMutexW")
	procCloseHandle      = modKernel32.NewProc("CloseHandle")
	procGlobalAlloc      = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modKernel32.NewProc("GlobalUnlock")
	procGlobalFree       = modKernel32.NewProc("GlobalFree")

	procTimeBeginPeriod = modWinmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod   = modWinmm.NewProc("timeEndPeriod")

	procGetOpenFileNameW = modComdlg32.NewProc("GetOpenFileNameW")

	procDragAcceptFiles = modShell32.NewProc("DragAcceptFiles")
	procDragQueryFileW  = modShell32.NewProc("DragQueryFileW")
	procDragFinish      = modShell32.NewProc("DragFinish")

	procOleInitialize = modOle32.NewProc("OleInitialize")
	procDoDragDrop    = modOle32.NewProc("DoDragDrop")

	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
	procAlphaBlend            = modMsimg32.NewProc("AlphaBlend")
)

func utf16Ptr(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }
func rgb(r, g, b byte) uint32   { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
func loword(v uintptr) int32    { return int32(uint16(v & 0xffff)) }
func hiword(v uintptr) int32    { return int32(uint16((v >> 16) & 0xffff)) }
func keyDown(vk uint32) bool {
	r, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(r&0xffff) < 0
}

func signedHiword(v uintptr) int32 { return int32(int16((v >> 16) & 0xffff)) }
func pointInRect(x, y int32, r RECT) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
func rectW(r RECT) int32 { return r.Right - r.Left }
func rectH(r RECT) int32 { return r.Bottom - r.Top }

func makeRect(x, y, w, h int32) RECT { return RECT{x, y, x + w, y + h} }
func inflateRect(r RECT, dx, dy int32) RECT {
	return RECT{r.Left - dx, r.Top - dy, r.Right + dx, r.Bottom + dy}
}

func getModuleHandle() uintptr {
	r, _, _ := procGetModuleHandleW.Call(0)
	return r
}
func invalidate(hwnd uintptr) { procInvalidateRect.Call(hwnd, 0, 0) }
func invalidateArea(hwnd uintptr, r RECT) {
	if rectW(r) <= 0 || rectH(r) <= 0 {
		return
	}
	if theApp != nil && theApp.hwnd == hwnd {
		r = theApp.logicalToPhysicalRect(r)
	}
	procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(&r)), 0)
}
func postMessage(hwnd uintptr, msg uint32, wp, lp uintptr) {
	procPostMessageW.Call(hwnd, uintptr(msg), wp, lp)
}
func setWindowText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(s))))
}
func messageBox(hwnd uintptr, text, title string, flags uint32) {
	messageBoxResult(hwnd, text, title, flags)
}
func messageBoxResult(hwnd uintptr, text, title string, flags uint32) uintptr {
	if theApp != nil {
		return themedNotice(hwnd, text, title, flags)
	}
	r, _, _ := procMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(flags))
	return r
}
