package main

import (
	"syscall"
	"unsafe"
)

// Keep native HMENU input, command IDs and accessibility names. Only the
// measurement and painting belong to the player's current theme.
type menuInfo struct {
	size, mask, style, height uint32
	brush                     uintptr
	helpID                    uint32
	data                      uintptr
}
type menuItemInfo struct {
	size, mask, kind, state, id          uint32
	sub, checked, unchecked, data, label uintptr
	length                               uint32
	bitmap                               uintptr
}
type measureItem struct {
	kind, ctl, id, width, height uint32
	data                         uintptr
}
type drawItem struct {
	kind, ctl, id, action, state uint32
	hwnd, dc                     uintptr
	rect                         RECT
	data                         uintptr
}
type popupRow struct {
	label     string
	separator bool
}
type popupSkin struct {
	rows []popupRow
	font uintptr
	dpi  uint32
}

var activePopup *popupSkin
var (
	getMenuCount     = modUser32.NewProc("GetMenuItemCount")
	getItemInfo      = modUser32.NewProc("GetMenuItemInfoW")
	setItemInfo      = modUser32.NewProc("SetMenuItemInfoW")
	setMenuInfo      = modUser32.NewProc("SetMenuInfo")
	windowDC         = modUser32.NewProc("GetDC")
	releaseDC        = modUser32.NewProc("ReleaseDC")
	drawFrame        = modUser32.NewProc("DrawFrameControl")
	createMonoBitmap = modGdi32.NewProc("CreateBitmap")
	setControlTheme  = syscall.NewLazyDLL("uxtheme.dll").NewProc("SetWindowTheme")
)

func (p *popupSkin) prepare(m uintptr) {
	info := menuInfo{mask: 2, brush: cachedBrush(cSurfaceRaised)}
	info.size = uint32(unsafe.Sizeof(info))
	setMenuInfo.Call(m, uintptr(unsafe.Pointer(&info)))
	count, _, _ := getMenuCount.Call(m)
	for i := 0; i < int(int32(count)); i++ {
		item := menuItemInfo{mask: 0x100 | 0x40 | 4} // FTYPE, STRING, SUBMENU
		item.size = uint32(unsafe.Sizeof(item))
		if ok, _, _ := getItemInfo.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&item))); ok == 0 {
			continue
		}
		buf := make([]uint16, item.length+1)
		item.label = uintptr(unsafe.Pointer(&buf[0]))
		item.length = uint32(len(buf))
		getItemInfo.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&item)))
		p.rows = append(p.rows, popupRow{syscall.UTF16ToString(buf), item.kind&MF_SEPARATOR != 0})
		item.mask = 0x100 | 0x20 // FTYPE and DATA; leave native string/state/submenu intact
		item.kind |= 0x100       // MFT_OWNERDRAW
		item.data = uintptr(len(p.rows))
		setItemInfo.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&item)))
		if item.sub != 0 {
			p.prepare(item.sub)
		}
	}
}

func (a *App) trackPopup(m, flags uintptr, pt POINT) uintptr {
	p := &popupSkin{dpi: a.currentDPI(), font: createFont(scaleDPI(14, a.currentDPI()), FW_NORMAL)}
	defer deleteObject(p.font)
	prev := activePopup
	activePopup = p
	defer func() { activePopup = prev }()
	p.prepare(m)
	cmd, _, _ := procTrackPopupMenu.Call(m, flags, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	return cmd
}

// Draw the stock Windows check mask in the same foreground as the item text.
func paintMenuCheck(dc uintptr, r RECT, fg, bg uint32) {
	mem, _, _ := procCreateCompatibleDC.Call(dc)
	if mem == 0 {
		return
	}
	defer procDeleteDC.Call(mem)
	bm, _, _ := createMonoBitmap.Call(uintptr(rectW(r)), uintptr(rectH(r)), 1, 1, 0)
	if bm == 0 {
		return
	}
	defer deleteObject(bm)
	old := selectObject(mem, bm)
	defer selectObject(mem, old)
	box := makeRect(0, 0, rectW(r), rectH(r))
	drawFrame.Call(mem, uintptr(unsafe.Pointer(&box)), 2, 1)
	f, _, _ := procSetTextColor.Call(dc, uintptr(fg))
	b, _, _ := procSetBkColor.Call(dc, uintptr(bg))
	procBitBlt.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(rectW(r)), uintptr(rectH(r)), mem, 0, 0, SRCCOPY)
	procSetTextColor.Call(dc, f)
	procSetBkColor.Call(dc, b)
}
func (p *popupSkin) handle(hwnd uintptr, msg uint32, lp uintptr) bool {
	if p == nil || lp == 0 {
		return false
	}
	s := func(n int32) int32 { return scaleDPI(n, p.dpi) }
	switch msg {
	case 0x2c: // WM_MEASUREITEM
		it := (*measureItem)(unsafe.Pointer(lp))
		if it.kind != 1 || it.data == 0 || it.data > uintptr(len(p.rows)) {
			return false
		}
		row := p.rows[it.data-1]
		if row.separator {
			it.width = 1
			it.height = uint32(s(9))
			return true
		}
		dc, _, _ := windowDC.Call(hwnd)
		it.width = uint32(measureTextWidth(dc, row.label, p.font) + s(38))
		it.height = uint32(s(30))
		releaseDC.Call(hwnd, dc)
		return true
	case 0x2b: // WM_DRAWITEM
		it := (*drawItem)(unsafe.Pointer(lp))
		if it.kind != 1 || it.data == 0 || it.data > uintptr(len(p.rows)) {
			return false
		}
		row := p.rows[it.data-1]
		r := it.rect
		fill(it.dc, r, cSurfaceRaised)
		if row.separator {
			y := (r.Top + r.Bottom) / 2
			line(it.dc, r.Left+s(8), y, r.Right-s(8), y, cBorder, 1)
			return true
		}
		fg, bg := cText, cSurfaceRaised
		if it.state&6 != 0 {
			fg = cFaint
		} else if it.state&1 != 0 {
			bg = cBlueDark
			fillRound(it.dc, inflateRect(r, -s(2), -s(1)), s(4), bg)
		}
		if it.state&8 != 0 {
			paintMenuCheck(it.dc, makeRect(r.Left+s(6), (r.Top+r.Bottom-s(13))/2, s(13), s(13)), fg, bg)
		}
		r.Left += s(27)
		r.Right -= s(12)
		text(it.dc, row.label, r, p.font, fg, DT_LEFT|DT_SINGLELINE|DT_VCENTER)
		return true
	}
	return false
}
