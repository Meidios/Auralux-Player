package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var procIsDialogMessage = modUser32.NewProc("IsDialogMessageW")
var setWinText = modUser32.NewProc("SetWindowTextW")
var shellOpen = modShell32.NewProc("ShellExecuteW")
var helpCallback uintptr

func init() { helpCallback = syscall.NewCallback(helpProc) }

const helpClass = "AuraluxHelpWindow"

type jobResult struct {
	release stableRelease
	path    string
	err     error
}
type helpController struct {
	hwnd, body, combo, font, titleFont                           uintptr
	controls                                                     []uintptr
	actions                                                      map[int]func()
	page, version, upgradeFrom, job                              string
	prefs                                                        updatePrefs
	release                                                      stableRelease
	initialized, engineSetupDone, startChecked, manual, canceled bool
	cancel                                                       context.CancelFunc
	results                                                      chan jobResult
}

func setText(h uintptr, s string) { setWinText.Call(h, uintptr(unsafe.Pointer(utf16Ptr(s)))) }
func openOfficial(a *App, u string) {
	ret, _, _ := shellOpen.Call(a.hwnd, 0, uintptr(unsafe.Pointer(utf16Ptr(u))), 0, 0, SW_SHOW)
	if ret <= 32 {
		messageBox(a.hwnd, tr("请在浏览器中打开：")+u, tr("无法打开浏览器"), MB_OK)
	}
}
func cacheSetupDir() string { return filepath.Join(os.TempDir(), "AuraluxPlayer-updates") }
func (a *App) initUpdates() {
	h := &a.help
	if h.initialized {
		return
	}
	h.initialized = true
	h.results = make(chan jobResult, 1)
	// The single-instance mutex ensures no other player is actively downloading.
	old, _ := filepath.Glob(filepath.Join(cacheSetupDir(), "Auralux-*-setup.exe"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	var err error
	h.prefs, h.upgradeFrom, err = recordLaunch(stateConfigDir(), appVersion)
	if err != nil {
		a.setStatus("Could not save update preferences: "+err.Error(), true)
	}
	if h.upgradeFrom != "" {
		a.showHelpPage("upgraded")
	}
	a.autoCheckWhenReady()
}
func (a *App) autoCheckWhenReady() {
	h := &a.help
	if !h.initialized || !h.engineSetupDone || h.startChecked || h.page == "upgraded" {
		return
	}
	h.startChecked = true
	if !h.prefs.DisableAuto {
		a.checkUpdates(false)
	}
}
func (a *App) savePrefs(p updatePrefs) bool {
	if e := p.save(stateConfigDir()); e != nil {
		messageBox(a.hwnd, tr("无法保存设置：")+e.Error(), "Auralux Player", MB_OK|MB_ICONERROR)
		return false
	}
	a.help.prefs = p
	return true
}
func (a *App) helpMenu() {
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	for i, label := range []string{tr("使用帮助    F1"), tr("检查更新…"), tr("启动时检查更新"), tr("更新记录"), tr("关于 Auralux Player")} {
		flags := uintptr(MF_STRING)
		if i == 2 && !a.help.prefs.DisableAuto {
			flags |= MF_CHECKED
		}
		procAppendMenuW.Call(m, flags, uintptr(i+1), uintptr(unsafe.Pointer(utf16Ptr(label))))
	}
	appendLanguageMenu(m)
	r := a.logicalToPhysicalRect(logoMenuRect(a.layout.Header))
	pt := POINT{r.Left, r.Bottom}
	procClientToScreen.Call(a.hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd := a.trackPopup(m, TPM_LEFTALIGN|TPM_TOPALIGN|TPM_RETURNCMD, pt)
	if cmd >= 6200 && cmd < 6200+uintptr(len(localeCodes)) {
		a.selectLanguage(localeCodes[cmd-6200])
		return
	}
	switch cmd {
	case 1:
		a.showHelpPage("guide")
	case 2:
		a.checkUpdates(true)
	case 3:
		p := a.help.prefs
		p.DisableAuto = !p.DisableAuto
		a.savePrefs(p)
	case 4:
		a.showHelpPage("history")
	case 5:
		a.showHelpPage("about")
	}
}
func (a *App) checkUpdates(manual bool) {
	h := &a.help
	if !h.initialized {
		a.initUpdates()
	}
	if h.job != "" {
		if manual {
			h.manual = true
			if h.job == "download" {
				a.showHelpPage("download")
			} else {
				a.showHelpPage("checking")
			}
		}
		return
	}
	h.manual = manual
	h.job = "check"
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	if manual {
		a.showHelpPage("checking")
	}
	hwnd := a.hwnd
	out := h.results
	go func() {
		r, e := fetchRelease(ctx, updateClient(false))
		out <- jobResult{release: r, err: e}
		postMessage(hwnd, msgUpdateEvent, 0, 0)
	}()
}
func (a *App) downloadUpdate() {
	h := &a.help
	if h.job != "" {
		return
	}
	r := h.release
	asset, ok := r.setup()
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.job = "download"
	h.canceled = false
	a.showHelpPage("download")
	hwnd := a.hwnd
	out := h.results
	go func() {
		last := -1
		p, e := fetchSetup(ctx, updateClient(true), r, cacheSetupDir(), func(n int64) {
			percent := int(n * 100 / asset.Size)
			if percent != last && percent <= 100 {
				postMessage(hwnd, msgUpdateProgress, uintptr(percent), 0)
				last = percent
			}
		})
		out <- jobResult{path: p, err: e}
		postMessage(hwnd, msgUpdateEvent, 0, 0)
	}()
}
func (a *App) finishUpdateJob() {
	h := &a.help
	var res jobResult
	select {
	case res = <-h.results:
	default:
		return
	}
	job := h.job
	h.job = ""
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	if job == "check" {
		if res.err != nil {
			if h.manual {
				a.displayHelp("error", tr("检查更新失败"), tr("无法连接或读取正式版信息。请重试，或打开官方发布页。\n\n")+res.err.Error(), a.retryCheck(), a.releaseLink(releasesURL))
			}
			return
		}
		if h.prefs.offer(res.release, h.manual) {
			h.release = res.release
			a.showHelpPage("offer")
			return
		}
		if h.manual {
			msg := tr("当前版本：") + appVersion + tr("。没有更高的正式版。")
			if res.release.Tag == "" {
				msg = tr("当前版本：") + appVersion + tr("。官方渠道尚未提供可用的正式版。")
			}
			a.displayHelp("current", tr("检查更新"), msg, a.releaseLink(releasesURL))
		}
		return
	}
	if h.canceled || errors.Is(res.err, context.Canceled) {
		if res.path != "" {
			os.Remove(res.path)
		}
		return
	}
	if res.err != nil {
		a.downloadError(res.err)
		return
	}
	// Inno receives the running installation directory as a single argument. The
	// player closes normally so its existing layout and playlists are flushed.
	cmd := exec.Command(res.path, "/DIR="+executableDir())
	if e := cmd.Start(); e != nil {
		os.Remove(res.path)
		a.downloadError(e)
		return
	}
	go cmd.Wait()
	postMessage(a.hwnd, WM_CLOSE, 0, 0)
}
func (a *App) downloadError(e error) {
	a.displayHelp("error", tr("更新未安装"), e.Error()+tr("\n\n当前版本仍可使用。"), helpAction{tr("重新下载"), a.downloadUpdate}, a.releaseLink(a.help.release.page()))
}
func (a *App) showDownloadProgress(n int) {
	if a.help.page == "download" && a.help.body != 0 && !a.help.canceled {
		setText(a.help.body, fmt.Sprintf(tr("正在下载安装包：%d%%\r\n\r\n下载完成后将校验大小和 SHA-256，再打开安装向导。"), n))
	}
}

type helpAction struct {
	label string
	run   func()
}

func (a *App) releaseLink(u string) helpAction {
	return helpAction{tr("打开发布页"), func() { openOfficial(a, u) }}
}
func (a *App) retryCheck() helpAction {
	return helpAction{tr("重试"), func() { a.checkUpdates(true) }}
}
func (a *App) closeHelp() {
	h := &a.help
	if h.page == "download" && h.job == "download" && h.cancel != nil {
		h.canceled = true
		h.cancel()
	}
	if h.page == "checking" {
		h.manual = false
	}
	if h.hwnd != 0 {
		procDestroyWindow.Call(h.hwnd)
	}
	a.autoCheckWhenReady()
}
func (a *App) showHelpPage(page string) {
	h := &a.help
	if h.job == "download" && page != "download" {
		page = "download"
	}
	switch page {
	case "guide":
		a.displayHelp(page, tr("使用帮助"), tr("Auralux Player 用于连续试听音效、对比混音并观察波形、频谱、响度和声场。\n\n试听一组音效\n1. 把音频文件或文件夹拖进播放器。\n2. 双击列表中的曲目播放，按空格暂停或继续。\n3. 点击波形定位瞬态，在频谱和声场面板查看它的频率分布和立体声关系。\n4. 双击下一条音效进行对比。Delete 仅从列表移除选中项。"))
	case "about":
		a.displayHelp(page, tr("关于 Auralux Player"), "Auralux Player "+appVersion+tr("  ·  Windows x64\n\n面向声音设计与音乐制作的试听和分析播放器。\n维护：Meidios / Auralux Player contributors\n\n如果这个工具对你有帮助，欢迎到 GitHub 点个 Star。"), helpAction{tr("官方主页"), func() { openOfficial(a, projectURL) }})
	case "history", "upgraded":
		title := tr("更新记录")
		if page == "upgraded" {
			title = tr("已从 ") + h.upgradeFrom + tr(" 更新到 ") + appVersion
		}
		h.version = appVersion
		a.displayHelp(page, title, localNotes(appVersion))
	case "checking":
		a.displayHelp(page, tr("检查更新"), tr("正在检查官方正式版…"))
	case "download":
		a.displayHelp(page, tr("下载更新"), tr("正在连接下载服务器…"))
	case "offer":
		r := h.release
		body := tr("当前版本 ") + appVersion + tr("    →    正式版 ") + r.version() + "\n\n" + noteText(r.Body)
		var actions []helpAction
		if _, ok := r.setup(); ok {
			body += tr("\n\n安装前播放器会保存播放列表和布局并关闭，播放将中断。")
			actions = append(actions, helpAction{tr("下载并更新"), a.downloadUpdate})
		} else {
			body += tr("\n\n未找到可校验的 Windows x64 安装包，请查看发布页。")
		}
		actions = append(actions, helpAction{tr("跳过此版本"), func() {
			p := h.prefs
			p.Skipped = r.version()
			if a.savePrefs(p) {
				a.closeHelp()
			}
		}}, a.releaseLink(r.page()))
		a.displayHelp(page, tr("发现新版本"), body, actions...)
	}
}

func helpProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	if theApp != nil {
		a := theApp
		h := &a.help
		switch msg {
		case WM_CLOSE:
			a.closeHelp()
			return 0
		case WM_COMMAND:
			id := int(wp & 0xffff)
			if id == IDCANCEL {
				a.closeHelp()
				return 0
			}
			if id == 700 && wp>>16 == 1 {
				index, _, _ := procSendMessageW.Call(h.combo, 0x147, 0, 0)
				all := noteVersions()
				if index < uintptr(len(all)) {
					h.version = all[index]
					setText(h.body, windowsLines(localNotes(h.version)))
					invalidate(h.combo)
				}
				return 0
			}
			if f := h.actions[id]; f != nil && wp>>16 == 0 {
				f()
				return 0
			}
		case WM_ERASEBKGND:
			var r RECT
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			fill(wp, r, cBG)
			return 1
		case 0x2b:
			if ownerButton(lp, a.currentDPI()) {
				return 1
			}
		case 0x0134, 0x0138, WM_CTLCOLOREDIT:
			procSetTextColor.Call(wp, uintptr(cText))
			procSetBkColor.Call(wp, uintptr(cBG))
			return cachedBrush(cBG)
		case WM_DESTROY:
			h.hwnd = 0
			h.body = 0
			h.combo = 0
			h.controls = nil
			h.actions = nil
			h.page = ""
			deleteObject(h.titleFont)
			deleteObject(h.font)
			h.titleFont = 0
			h.font = 0
			return 0
		}
	}
	v, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return v
}
func windowsLines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}
func (a *App) displayHelp(page, title, body string, buttons ...helpAction) {
	h := &a.help
	dpi := a.currentDPI()
	scale := func(n int32) int32 { return scaleDPI(n, dpi) }
	if h.hwnd == 0 {
		wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: helpCallback, HInstance: getModuleHandle(), HCursor: mustLoadCursor(IDC_ARROW), HIcon: loadAppIcon(), LpszClassName: utf16Ptr(helpClass)}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		var owner RECT
		procGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&owner)))
		w, yh := scale(562), scale(364)
		x, y := owner.Left+(rectW(owner)-w)/2, owner.Top+(rectH(owner)-yh)/2
		h.hwnd, _, _ = procCreateWindowExW.Call(WS_EX_DLGMODALFRAME, uintptr(unsafe.Pointer(utf16Ptr(helpClass))), uintptr(unsafe.Pointer(utf16Ptr(title))), WS_CAPTION|WS_SYSMENU, uintptr(x), uintptr(y), uintptr(w), uintptr(yh), a.hwnd, 0, getModuleHandle(), 0)
		if h.hwnd == 0 {
			return
		}
		h.titleFont = createFont(scale(22), FW_SEMIBOLD)
		h.font = createFont(scale(15), FW_NORMAL)
		setWindowThemeMode(h.hwnd, a.themeMode == 0)
	} else {
		for _, c := range h.controls {
			procDestroyWindow.Call(c)
		}
		h.controls = nil
		setText(h.hwnd, title)
	}
	h.page = page
	h.actions = map[int]func(){}
	h.combo = 0
	ctrl := func(class, label string, style uintptr, x, y, w, ht int32, id int) uintptr {
		v := createDialogControl(h.hwnd, class, label, WS_CHILD|WS_VISIBLE|style, 0, scale(x), scale(y), scale(w), scale(ht), uintptr(id))
		h.controls = append(h.controls, v)
		procSendMessageW.Call(v, WM_SETFONT, h.font, 1)
		return v
	}
	// Short notices use a compact window; long notes retain a scrollable reader.
	width, height := int32(550), int32(328)
	short := page == "checking" || page == "current" || page == "download"
	if short {
		width, height = 470, 142
	} else if page == "about" {
		width, height = 500, 260
	}
	var outer, client RECT
	procGetWindowRect.Call(h.hwnd, uintptr(unsafe.Pointer(&outer)))
	procGetClientRect.Call(h.hwnd, uintptr(unsafe.Pointer(&client)))
	w := scale(width) + rectW(outer) - rectW(client)
	ht := scale(height) + rectH(outer) - rectH(client)
	procSetWindowPos.Call(h.hwnd, 0, uintptr(outer.Left+(rectW(outer)-w)/2), uintptr(outer.Top+(rectH(outer)-ht)/2), uintptr(w), uintptr(ht), SWP_NOZORDER|SWP_NOACTIVATE)
	y := int32(18)
	if page == "about" {
		img := ctrl("STATIC", "", 3|0x40, 20, y, 42, 42, 710)
		ico, _, _ := procLoadImageW.Call(getModuleHandle(), 1, IMAGE_ICON, uintptr(scale(42)), uintptr(scale(42)), 0x8000) // LR_SHARED
		procSendMessageW.Call(img, 0x170, ico, 0)
		heading := ctrl("STATIC", "Auralux Player", 0, 72, y, 360, 32, 711)
		procSendMessageW.Call(heading, WM_SETFONT, h.titleFont, 1)
		parts := strings.SplitN(body, "\n\n", 2)
		ctrl("STATIC", strings.TrimPrefix(parts[0], "Auralux Player "), 0, 72, y+32, 360, 23, 712)
		if len(parts) == 2 {
			body = parts[1]
		}
		y += 70
	}
	if page == "history" || page == "upgraded" {
		h.combo = ctrl("COMBOBOX", "", WS_TABSTOP|0x0003|0x00200000, 18, y, 190, 140, 700)
		subclassButton.Call(h.combo, comboCallback, 1, 0)
		for i, v := range noteVersions() {
			procSendMessageW.Call(h.combo, 0x143, 0, uintptr(unsafe.Pointer(utf16Ptr(v))))
			if v == appVersion {
				procSendMessageW.Call(h.combo, 0x14e, uintptr(i), 0)
			}
		}
		y += 38
	}
	style := uintptr(WS_TABSTOP | 0x0800 | 0x0004 | 0x0040)
	if !short && page != "about" {
		style |= 0x00200000
	}
	bodyX := int32(20)
	if short {
		img := ctrl("STATIC", "", 3|0x40, 18, y, 32, 32, 713)
		ico, _, _ := procLoadIconW.Call(0, 32516)
		procSendMessageW.Call(img, 0x170, ico, 0)
		bodyX = 64
	}
	h.body = ctrl("EDIT", windowsLines(body), style, bodyX, y, width-bodyX-20, height-66-y, 701)
	closeLabel := tr("关闭")
	if page == "offer" {
		closeLabel = tr("稍后")
	}
	if page == "download" {
		closeLabel = tr("取消下载")
	}
	buttons = append(buttons, helpAction{closeLabel, a.closeHelp})
	for i, b := range buttons {
		id := 720 + i
		h.actions[id] = b.run
		bw := int32(118)
		if len(buttons) == 1 {
			bw = 84
		}
		x := width - 18 - int32(len(buttons)-i)*(bw+8) + 8
		button := ctrl("BUTTON", b.label, WS_TABSTOP|0xb, x, height-48, bw, 32, id)
		subclassButton.Call(button, buttonCallback, 1, 0)
	}
	a.refreshHelpTheme()
	invalidate(h.hwnd)
	procShowWindow.Call(h.hwnd, SW_SHOW)
	procSetForegroundWindow.Call(h.hwnd)
	procSetFocus.Call(h.controls[len(h.controls)-1])
}
