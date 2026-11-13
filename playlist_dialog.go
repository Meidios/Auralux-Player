//go:build windows

package main

import (
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	nameDialogClass = "AuraluxPlaylistNameDialog"
	nameEditID      = 6101
	emSetSel        = 0x00B1
)

type nameDialogState struct {
	hwnd         uintptr
	edit         uintptr
	result       string
	errorMessage string
	done         bool
	accepted     bool
	dpi          uint32
	hoverButton  int
	editBrush    uintptr
	fontBody     uintptr
	fontSmall    uintptr
	fontButton   uintptr
	validate     func(string) string
}

var (
	nameDialogOnce     sync.Once
	nameDialogReady    bool
	nameDialogCallback uintptr
	currentNameDialog  *nameDialogState
)

func registerNameDialogClass() bool {
	nameDialogOnce.Do(func() {
		nameDialogCallback = syscall.NewCallback(nameDialogWndProc)
		wc := WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
			LpfnWndProc:   nameDialogCallback,
			HInstance:     getModuleHandle(),
			HCursor:       mustLoadCursor(IDC_ARROW),
			LpszClassName: utf16Ptr(nameDialogClass),
		}
		r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		nameDialogReady = r != 0
	})
	return nameDialogReady
}

func nameDialogText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, int(n)+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func nameDialogInputRect(dpi uint32) RECT {
	return makeRect(scaleDPI(20, dpi), scaleDPI(46, dpi), scaleDPI(380, dpi), scaleDPI(36, dpi))
}

func nameDialogButtonRects(dpi uint32) (RECT, RECT) {
	ok := makeRect(scaleDPI(234, dpi), scaleDPI(116, dpi), scaleDPI(78, dpi), scaleDPI(32, dpi))
	cancel := makeRect(scaleDPI(322, dpi), scaleDPI(116, dpi), scaleDPI(78, dpi), scaleDPI(32, dpi))
	return ok, cancel
}

func submitNameDialog(state *nameDialogState) {
	if state == nil || state.done {
		return
	}
	value := nameDialogText(state.edit)
	if state.validate != nil {
		if reason := state.validate(value); reason != "" {
			state.errorMessage = reason
			invalidate(state.hwnd)
			procSetFocus.Call(state.edit)
			procSendMessageW.Call(state.edit, emSetSel, 0, ^uintptr(0))
			return
		}
	}
	state.result = strings.TrimSpace(value)
	state.accepted = true
	state.done = true
	procDestroyWindow.Call(state.hwnd)
}

func cancelNameDialog(state *nameDialogState) {
	if state == nil || state.done {
		return
	}
	state.done = true
	procDestroyWindow.Call(state.hwnd)
}

func drawNameDialogButton(dc uintptr, state *nameDialogState, r RECT, label string, kind int, primary bool) {
	drawDialogButton(dc, r, label, state.fontButton, state.dpi, state.hoverButton == kind, false, primary, false)
}

func paintNameDialog(hwnd uintptr, state *nameDialogState) {
	var ps PAINTSTRUCT
	dc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var client RECT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	fill(dc, client, cBG)
	dpi := state.dpi
	text(dc, tr("Playlist 名称"), makeRect(scaleDPI(20, dpi), scaleDPI(17, dpi), scaleDPI(380, dpi), scaleDPI(22, dpi)), state.fontBody, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	input := nameDialogInputRect(dpi)
	fillRound(dc, input, scaleDPI(6, dpi), cSurfaceDeep)
	strokeRound(dc, input, scaleDPI(6, dpi), cAccent2, 1)
	message := tr("最长 64 个字符，名称不可重复")
	messageColor := cFaint
	if state.errorMessage != "" {
		message = state.errorMessage
		messageColor = cDanger
	}
	text(dc, message, makeRect(scaleDPI(22, dpi), scaleDPI(87, dpi), scaleDPI(376, dpi), scaleDPI(20, dpi)), state.fontSmall, messageColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	ok, cancel := nameDialogButtonRects(dpi)
	drawNameDialogButton(dc, state, ok, tr("确定"), 1, true)
	drawNameDialogButton(dc, state, cancel, tr("取消"), 2, false)
}

func nameDialogWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	state := currentNameDialog
	switch msg {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		if state != nil {
			paintNameDialog(hwnd, state)
			return 0
		}
	case WM_CTLCOLOREDIT:
		if state != nil {
			procSetTextColor.Call(wparam, uintptr(cText))
			procSetBkColor.Call(wparam, uintptr(cSurfaceDeep))
			return state.editBrush
		}
	case WM_MOUSEMOVE:
		if state != nil {
			x, y := loword(lparam), hiword(lparam)
			ok, cancel := nameDialogButtonRects(state.dpi)
			hover := 0
			if pointInRect(x, y, ok) {
				hover = 1
			} else if pointInRect(x, y, cancel) {
				hover = 2
			}
			if hover != state.hoverButton {
				state.hoverButton = hover
				invalidate(hwnd)
			}
			return 0
		}
	case WM_LBUTTONDOWN:
		if state != nil {
			x, y := loword(lparam), hiword(lparam)
			ok, cancel := nameDialogButtonRects(state.dpi)
			if pointInRect(x, y, ok) {
				submitNameDialog(state)
				return 0
			}
			if pointInRect(x, y, cancel) {
				cancelNameDialog(state)
				return 0
			}
		}
	case WM_CLOSE:
		cancelNameDialog(state)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

func createDialogControl(parent uintptr, className, textValue string, style, exStyle uintptr, x, y, w, h int32, id uintptr) uintptr {
	r, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(textValue))),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, getModuleHandle(), 0,
	)
	return r
}

func destroyNameDialogResources(state *nameDialogState) {
	deleteObject(state.editBrush)
	for _, font := range []uintptr{state.fontBody, state.fontSmall, state.fontButton} {
		deleteObject(font)
	}
}

func promptPlaylistName(app *App, title, initial string, validate func(string) string) (string, bool) {
	if app == nil || app.hwnd == 0 || !registerNameDialogClass() {
		return "", false
	}
	dpi := app.currentDPI()
	w, h := scaleDPI(420, dpi), scaleDPI(190, dpi)
	var owner RECT
	procGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&owner)))
	x := owner.Left + (rectW(owner)-w)/2
	y := owner.Top + (rectH(owner)-h)/2
	state := &nameDialogState{
		dpi:        dpi,
		validate:   validate,
		editBrush:  newBrush(cSurfaceDeep),
		fontBody:   createFont(scaleDPI(14, dpi), FW_NORMAL),
		fontSmall:  createFont(scaleDPI(11, dpi), FW_NORMAL),
		fontButton: createFont(scaleDPI(13, dpi), FW_SEMIBOLD),
	}
	currentNameDialog = state
	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_DLGMODALFRAME,
		uintptr(unsafe.Pointer(utf16Ptr(nameDialogClass))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		WS_CAPTION|WS_SYSMENU,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		app.hwnd, 0, getModuleHandle(), 0,
	)
	if hwnd == 0 {
		currentNameDialog = nil
		destroyNameDialogResources(state)
		return "", false
	}
	state.hwnd = hwnd
	setWindowThemeMode(hwnd, app.themeMode == 0)

	input := nameDialogInputRect(dpi)
	state.edit = createDialogControl(
		hwnd, "EDIT", initial,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL, 0,
		input.Left+scaleDPI(8, dpi), input.Top+scaleDPI(6, dpi), rectW(input)-scaleDPI(16, dpi), rectH(input)-scaleDPI(12, dpi), nameEditID,
	)
	if state.edit == 0 {
		procDestroyWindow.Call(hwnd)
		currentNameDialog = nil
		destroyNameDialogResources(state)
		return "", false
	}
	procSendMessageW.Call(state.edit, WM_SETFONT, state.fontBody, 1)

	procEnableWindow.Call(app.hwnd, 0)
	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(state.edit)
	procSendMessageW.Call(state.edit, emSetSel, 0, ^uintptr(0))

	quitRequested := false
	for !state.done {
		var msg MSG
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			quitRequested = r == 0
			break
		}
		if msg.Message == WM_KEYDOWN {
			switch uint32(msg.WParam) {
			case VK_RETURN:
				submitNameDialog(state)
				continue
			case VK_ESCAPE:
				cancelNameDialog(state)
				continue
			}
		}
		if msg.Message == WM_MOUSEMOVE && msg.Hwnd != hwnd && state.hoverButton != 0 {
			state.hoverButton = 0
			invalidate(hwnd)
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if !state.done {
		procDestroyWindow.Call(hwnd)
	}
	procEnableWindow.Call(app.hwnd, 1)
	procSetForegroundWindow.Call(app.hwnd)
	currentNameDialog = nil
	destroyNameDialogResources(state)
	if quitRequested {
		procPostQuitMessage.Call(0)
	}
	return state.result, state.accepted
}
