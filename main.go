//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	appTitle          = "Auralux Player " + appVersion
	windowClass       = "AuraluxNativeAudioPlayer"
	msgEngineReady    = WM_APP + 1
	msgWaveReady      = WM_APP + 2
	msgStatus         = WM_APP + 3
	msgRenderTick     = WM_APP + 4
	msgPlaybackEnd    = WM_APP + 5
	msgUpdateEvent    = WM_APP + 6
	msgUpdateProgress = WM_APP + 7
	msgUpdateStartup  = WM_APP + 8
)

// A link-time override lets isolated QA builds run beside the installed player.
var singleInstanceMutexName = "Local\\AuraluxPlayer.SingleInstance"

var theApp *App

func main() {
	runtime.LockOSThread()
	setPerMonitorDPIAwareness()

	startupFiles := startupAudioFiles(os.Args[1:])
	mutexName := utf16Ptr(singleInstanceMutexName)
	mutex, _, mutexErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutex != 0 && mutexErr == syscall.Errno(183) {
		procCloseHandle.Call(mutex)
		forwardToExistingInstance(startupFiles)
		return
	}
	if mutex != 0 {
		defer procCloseHandle.Call(mutex)
	}

	hInstance := getModuleHandle()
	className := utf16Ptr(windowClass)
	icon := loadAppIcon()
	wc := WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:         CS_HREDRAW | CS_VREDRAW | CS_DBLCLKS,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hInstance,
		HIcon:         icon,
		HCursor:       mustLoadCursor(IDC_ARROW),
		LpszClassName: className,
		HIconSm:       icon,
	}
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}

	loadLanguage(stateConfigDir(), windowsDisplayLanguage())
	app := NewApp()
	// OLE drag and drop is what lets a playlist row be dragged out to Explorer;
	// without it the player still runs, the export gestures just report that the
	// system refused to start.
	app.oleReady = oleInitialize()
	initialDPI := systemDPI()
	app.setDPI(initialDPI)
	app.pendingOpen = startupFiles
	theApp = app
	winX, winY, winW, winH := app.initialWindowBounds(initialDPI)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr(appTitle))),
		WS_OVERLAPPEDWINDOW,
		uintptr(winX), uintptr(winY), uintptr(winW), uintptr(winH),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}
	app.hwnd = hwnd
	if procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi >= 96 {
			app.setDPI(uint32(dpi))
		}
	}
	setWindowThemeMode(hwnd, app.themeMode == 0)
	procDragAcceptFiles.Call(hwnd, 1)
	procTimeBeginPeriod.Call(1)
	defer procTimeEndPeriod.Call(1)

	// WM_TIMER is a low-priority synthesized message and can be starved by a
	// continuous stream of WM_MOUSEMOVE input. Drive animation with a normal
	// posted message instead, coalescing outstanding ticks so the queue cannot
	// grow without bound. Keep analysis at 120 Hz; onTick composes the expensive
	// LINE phosphor surface every second tick for an evenly paced 60 fps output.
	renderTickerStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second / 120)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				app.renderTickSerial.Add(1)
				if app.renderTickQueued.CompareAndSwap(false, true) {
					postMessage(hwnd, msgRenderTick, 0, 0)
				}
			case <-renderTickerStop:
				return
			}
		}
	}()
	defer close(renderTickerStop)

	showCmd := uintptr(SW_SHOW)
	if app.windowPlacementValid && app.windowMaximized {
		showCmd = SW_SHOWMAXIMIZED
	}
	procShowWindow.Call(hwnd, showCmd)
	procUpdateWindow.Call(hwnd)
	app.startEngineSetup()
	postMessage(hwnd, msgUpdateStartup, 0, 0)

	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if app.help.hwnd != 0 {
			if handled, _, _ := procIsDialogMessage.Call(app.help.hwnd, uintptr(unsafe.Pointer(&msg))); handled != 0 {
				continue
			}
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func mustLoadCursor(id uintptr) uintptr { r, _, _ := procLoadCursorW.Call(0, id); return r }
func loadAppIcon() uintptr {
	// Resource ID 1 is embedded in the EXE so Windows Explorer and Open With
	// can display the app icon even when the external ICO is not beside it.
	if h := getModuleHandle(); h != 0 {
		r, _, _ := procLoadImageW.Call(h, 1, IMAGE_ICON, 64, 64, 0)
		if r != 0 {
			return r
		}
	}
	p := filepath.Join(executableDir(), "Auralux.ico")
	if _, err := os.Stat(p); err == nil {
		r, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(p))), IMAGE_ICON, 64, 64, LR_LOADFROMFILE)
		if r != 0 {
			return r
		}
	}
	r, _, _ := procLoadIconW.Call(0, 32512)
	return r
}

func forwardToExistingInstance(paths []string) {
	className := utf16Ptr(windowClass)
	for i := 0; i < 60; i++ {
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
		if hwnd != 0 {
			if len(paths) > 0 {
				payload := strings.Join(paths, "\n")
				buf, err := syscall.UTF16FromString(payload)
				if err == nil && len(buf) > 0 {
					cds := COPYDATASTRUCT{
						DwData: 0x4155524C, // "AURL"
						CbData: uint32(len(buf) * 2),
						LpData: uintptr(unsafe.Pointer(&buf[0])),
					}
					procSendMessageW.Call(hwnd, WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)))
				}
			}
			procShowWindow.Call(hwnd, SW_RESTORE)
			procSetForegroundWindow.Call(hwnd)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func startupAudioFiles(args []string) []string {
	files := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			continue
		}
		p := filepath.Clean(arg)
		if !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || !isAudioFile(p) {
			continue
		}
		files = append(files, p)
	}
	return files
}
func setPerMonitorDPIAwareness() {
	if procSetProcessDpiAwarenessCtx.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the signed value -4.
		ctx := ^uintptr(3)
		if ok, _, _ := procSetProcessDpiAwarenessCtx.Call(ctx); ok != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}

func systemDPI() uint32 {
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi >= 96 {
			return uint32(dpi)
		}
	}
	return 96
}

func setWindowThemeMode(hwnd uintptr, dark bool) {
	v := int32(0)
	if dark {
		v = 1
	}
	// Attribute 20 is used by current Windows versions; 19 covers older builds.
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
}

func wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if theApp != nil {
		if handled, result := theApp.handleMessage(hwnd, msg, wparam, lparam); handled {
			return result
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}
