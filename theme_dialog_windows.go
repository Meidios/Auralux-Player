package main

import (
	"syscall"
	"unsafe"
)

var (
	commonControls  = syscall.NewLazyDLL("comctl32.dll")
	subclassButton  = commonControls.NewProc("SetWindowSubclass")
	defaultSubclass = commonControls.NewProc("DefSubclassProc")
	buttonCallback  = syscall.NewCallback(buttonHoverProc)
	hotButton       uintptr
	popupRedraw     = modUser32.NewProc("RedrawWindow")

	dialogModal   = modUser32.NewProc("DialogBoxIndirectParamW")
	endModal      = modUser32.NewProc("EndDialog")
	modalCallback = syscall.NewCallback(modalProc)
)

func buttonHoverProc(hwnd uintptr, msg uint32, wp, lp, id, data uintptr) uintptr {
	switch msg {
	case WM_MOUSEMOVE:
		if hotButton != hwnd {
			hotButton = hwnd
			track := TRACKMOUSEEVENT{DwFlags: TME_LEAVE, HwndTrack: hwnd}
			track.CbSize = uint32(unsafe.Sizeof(track))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&track)))
			invalidate(hwnd)
		}
	case WM_MOUSELEAVE, WM_DESTROY:
		if hotButton == hwnd {
			hotButton = 0
			if msg != WM_DESTROY {
				invalidate(hwnd)
			}
		}
	}
	r, _, _ := defaultSubclass.Call(hwnd, uintptr(msg), wp, lp)
	return r
}
func drawDialogButton(dc uintptr, r RECT, label string, font uintptr, dpi uint32, hover, pressed, focus, disabled bool) {
	bg, border, fg := cSurfaceRaised, cBorder, cText
	if hover {
		bg = cPanel2
		border = cAccent
	}
	if pressed {
		bg = cBlueDark
		border = cAccent
	}
	if focus {
		border = cAccent
	}
	if disabled {
		fg = cFaint
	}
	fill(dc, r, cBG)
	fillRound(dc, r, scaleDPI(5, dpi), bg)
	strokeRound(dc, r, scaleDPI(5, dpi), border, 1)
	text(dc, label, r, font, fg, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}
func ownerButton(lp uintptr, dpi uint32) bool {
	if lp == 0 {
		return false
	}
	it := (*drawItem)(unsafe.Pointer(lp))
	if it.kind != 4 {
		return false
	}
	font, _, _ := procSendMessageW.Call(it.hwnd, 0x31, 0, 0)
	drawDialogButton(it.dc, it.rect, nameDialogText(it.hwnd), font, dpi, hotButton == it.hwnd, it.state&1 != 0, it.state&0x10 != 0, it.state&4 != 0)
	return true
}
func themeChild(h uintptr, dark bool) {
	name := "Explorer"
	if dark {
		name = "DarkMode_Explorer"
	}
	setControlTheme.Call(h, uintptr(unsafe.Pointer(utf16Ptr(name))), 0)
}
func (a *App) refreshHelpTheme() {
	h := &a.help
	if h.hwnd == 0 {
		return
	}
	setWindowThemeMode(h.hwnd, a.themeMode == 0)
	for _, w := range []uintptr{h.body, h.combo} {
		if w != 0 {
			themeChild(w, a.themeMode == 0)
		}
	}
	popupRedraw.Call(h.hwnd, 0, 0, 0x1|0x4|0x80|0x100)
}

// A native modal dialog keeps owner activation, keyboard navigation and
// synchronous Yes/No return values, while allowing themed client controls.
type notice struct {
	title, body string
	flags       uint32
	dpi         uint32
	font        uintptr
}

var currentNotice *notice

func themedNotice(owner uintptr, body, title string, flags uint32) uintptr {
	n := &notice{title: title, body: body, flags: flags, dpi: theApp.currentDPI()}
	old := currentNotice
	currentNotice = n
	defer func() { currentNotice = old }()
	n.font = createFont(scaleDPI(14, n.dpi), FW_NORMAL)
	defer deleteObject(n.font)
	// DLGTEMPLATE, followed by empty menu, class and caption (all WORDs).
	var tmpl [8]uint32
	tmpl[3] = 240 << 16 // cx and cy occupy WORD offsets 7 and 8.
	tmpl[4] = 100
	tmpl[0] = WS_CAPTION | WS_SYSMENU | 0x80 | 0x80000000 // DS_MODALFRAME | WS_POPUP
	r, _, _ := dialogModal.Call(getModuleHandle(), uintptr(unsafe.Pointer(&tmpl[0])), owner, modalCallback, 0)
	if int32(r) == -1 {
		r, _, _ = procMessageBoxW.Call(owner, uintptr(unsafe.Pointer(utf16Ptr(body))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(flags))
	}
	return r
}
func modalProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	n := currentNotice
	if n == nil {
		return 0
	}
	switch msg {
	case 0x110: // WM_INITDIALOG
		s := func(v int32) int32 { return scaleDPI(v, n.dpi) }
		setText(hwnd, n.title)
		setWindowThemeMode(hwnd, theApp.themeMode == 0)
		dc, _, _ := windowDC.Call(hwnd)
		old := selectObject(dc, n.font)
		r := makeRect(0, 0, s(360), 0)
		procDrawTextW.Call(dc, uintptr(unsafe.Pointer(utf16Ptr(n.body))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x10|DT_CALCRECT|DT_NOPREFIX)
		selectObject(dc, old)
		releaseDC.Call(hwnd, dc)
		bodyH := maxi32(s(34), rectH(r)+s(6))
		if bodyH > s(240) {
			bodyH = s(240)
		}
		w := s(440)
		clientH := s(66) + bodyH
		var outer, client, owner RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&outer)))
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		getOwner, _, _ := modUser32.NewProc("GetWindow").Call(hwnd, 4)
		procGetWindowRect.Call(getOwner, uintptr(unsafe.Pointer(&owner)))
		h := clientH + rectH(outer) - rectH(client)
		ow := w + rectW(outer) - rectW(client)
		procSetWindowPos.Call(hwnd, 0, uintptr(owner.Left+(rectW(owner)-ow)/2), uintptr(owner.Top+(rectH(owner)-h)/2), uintptr(ow), uintptr(h), SWP_NOZORDER)
		icon := uintptr(32516)
		if n.flags&0xf0 == MB_ICONERROR {
			icon = 32513
		} else if n.flags&0xf0 == MB_ICONQUESTION {
			icon = 32514
		}
		ico, _, _ := procLoadIconW.Call(0, icon)
		img := createDialogControl(hwnd, "STATIC", "", WS_CHILD|WS_VISIBLE|3, 0, s(16), s(16), s(32), s(32), 0)
		procSendMessageW.Call(img, 0x170, ico, 0)
		body := createDialogControl(hwnd, "EDIT", windowsLines(n.body), WS_CHILD|WS_VISIBLE|0x0800|0x0004|0x0040, 0, s(60), s(16), s(360), bodyH, 0)
		procSendMessageW.Call(body, WM_SETFONT, n.font, 1)
		add := func(id uintptr, label string, right int32) uintptr {
			b := createDialogControl(hwnd, "BUTTON", label, WS_TABSTOP|WS_VISIBLE|WS_CHILD|0xb, 0, w-s(right), clientH-s(42), s(80), s(30), id)
			procSendMessageW.Call(b, WM_SETFONT, n.font, 1)
			subclassButton.Call(b, buttonCallback, 1, 0)
			return b
		}
		var first uintptr
		if n.flags&0xf == MB_YESNO {
			first = add(IDYES, tr("Yes"), 185)
			add(7, tr("No"), 96)
		} else {
			first = add(IDOK, tr("确定"), 96)
		}
		procSetFocus.Call(first)
		return 0
	case WM_COMMAND:
		id := wp & 0xffff
		if id == IDYES || id == 7 || id == IDOK || id == IDCANCEL {
			endModal.Call(hwnd, id)
			return 1
		}
	case WM_CLOSE:
		endModal.Call(hwnd, IDCANCEL)
		return 1
	case 0x2b:
		if ownerButton(lp, n.dpi) {
			return 1
		}
	case 0x136, 0x138, WM_CTLCOLOREDIT:
		procSetTextColor.Call(wp, uintptr(cText))
		procSetBkColor.Call(wp, uintptr(cBG))
		return cachedBrush(cBG)
	}
	return 0
}

// Paint only the closed face of the version selector. The original COMBOBOX
// still owns its accessible value, keyboard selection and drop-down list.
var comboCallback = syscall.NewCallback(comboFaceProc)

func comboFaceProc(hwnd uintptr, msg uint32, wp, lp, id, data uintptr) uintptr {
	if (msg == WM_PAINT || msg == 0x318) && theApp != nil {
		var ps PAINTSTRUCT
		dc := wp
		if msg == WM_PAINT {
			dc, _, _ = procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		if dc == 0 {
			return 0
		}
		var r RECT
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		s := func(v int32) int32 { return scaleDPI(v, theApp.currentDPI()) }
		focus, _, _ := modUser32.NewProc("GetFocus").Call()
		edge := cBorder
		if focus == hwnd {
			edge = cAccent
		}
		fill(dc, r, cBG)
		fillRound(dc, r, s(5), cSurfaceRaised)
		strokeRound(dc, r, s(5), edge, 1)
		drawUIAsset(dc, theApp.uiImages, "chevron_down", makeRect(r.Right-s(25), (rectH(r)-s(16))/2, s(16), s(16)))
		r.Left += s(10)
		r.Right -= s(29)
		text(dc, nameDialogText(hwnd), r, theApp.help.font, cText, DT_LEFT|DT_SINGLELINE|DT_VCENTER)
		return 0
	}
	v, _, _ := defaultSubclass.Call(hwnd, uintptr(msg), wp, lp)
	switch msg {
	case 7, 8, 0x14e, 0x14f:
		invalidate(hwnd)
	}
	return v
}
