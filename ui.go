//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	cBG             uint32
	cHeader         uint32
	cPanel          uint32
	cPanel2         uint32
	cPanel3         uint32
	cBorder         uint32
	cGrid           uint32
	cGridStrong     uint32
	cText           uint32
	cMuted          uint32
	cFaint          uint32
	cAccent         uint32
	cAccent2        uint32
	cReady          uint32
	cBlueDark       uint32
	cSpecFill       uint32
	cSpecLine       uint32
	cSpecTrace      uint32
	cWarn           uint32
	cDanger         uint32
	cSurfaceDeep    uint32
	cSurfaceAlt     uint32
	cSurfaceRaised  uint32
	cWaveInactive   uint32
	cClipGuide      uint32
	cSpecShadow     uint32
	cVUGreenOn      uint32
	cVUGreenOff     uint32
	cVUYellowOff    uint32
	cVURedOff       uint32
	cVUHighlight    uint32
	cSplitterLine   uint32
	cSplitterHover  uint32
	cCorrelationNeg uint32
	cCorrelationPos uint32
	cTargetZone     uint32
	cHistoryOutline uint32
	cStereoTrail    uint32
	cStereoLine     uint32
	cStereoCore     uint32
	cStereoPeak     uint32
	cStereoFill     uint32
)

func setTheme(mode int) {
	if mode == 1 { // light / day mode
		// Keep the day palette cool and restrained. The previous palette put
		// most surfaces within a few points of white, which flattened panel
		// hierarchy and made long analysis sessions unnecessarily bright.
		cBG = rgb(229, 233, 238)
		cHeader = rgb(237, 240, 244)
		cPanel = rgb(244, 246, 249)
		cPanel2 = rgb(235, 239, 244)
		cPanel3 = rgb(226, 231, 237)
		cBorder = rgb(174, 183, 194)
		cGrid = rgb(215, 221, 228)
		cGridStrong = rgb(185, 194, 204)
		cText = rgb(28, 35, 42)
		cMuted = rgb(73, 87, 102)
		cFaint = rgb(112, 126, 140)
		cAccent = rgb(30, 116, 190)
		cAccent2 = rgb(44, 139, 211)
		cReady = rgb(31, 153, 82)
		cBlueDark = rgb(210, 226, 241)
		cSpecFill = rgb(207, 226, 241)
		cSpecLine = rgb(48, 126, 193)
		cSpecTrace = rgb(123, 137, 151)
		cWarn = rgb(177, 119, 25)
		cDanger = rgb(194, 61, 67)
		cSurfaceDeep = rgb(238, 242, 246)
		cSurfaceAlt = rgb(232, 237, 242)
		cSurfaceRaised = rgb(248, 250, 252)
		cWaveInactive = rgb(105, 119, 133)
		cClipGuide = rgb(193, 104, 112)
		cSpecShadow = rgb(142, 174, 196)
		cVUGreenOn = rgb(31, 145, 82)
		cVUGreenOff = rgb(203, 222, 210)
		cVUYellowOff = rgb(229, 215, 182)
		cVURedOff = rgb(231, 198, 201)
		cVUHighlight = rgb(246, 252, 248)
		cSplitterLine = rgb(161, 171, 182)
		cSplitterHover = rgb(54, 124, 178)
		cCorrelationNeg = rgb(195, 70, 82)
		cCorrelationPos = rgb(31, 139, 88)
		cTargetZone = rgb(218, 232, 243)
		cHistoryOutline = rgb(248, 250, 252)
		cStereoTrail = rgb(145, 190, 207)
		cStereoLine = rgb(61, 145, 177)
		cStereoCore = rgb(24, 119, 163)
		cStereoPeak = rgb(10, 90, 135)
		cStereoFill = rgb(216, 235, 242)
		return
	}
	// default dark gray theme
	cBG = rgb(20, 20, 22)
	cHeader = rgb(27, 27, 29)
	cPanel = rgb(32, 32, 34)
	cPanel2 = rgb(37, 38, 40)
	cPanel3 = rgb(42, 43, 46)
	cBorder = rgb(68, 72, 78)
	cGrid = rgb(52, 55, 60)
	cGridStrong = rgb(78, 82, 88)
	cText = rgb(236, 238, 241)
	cMuted = rgb(165, 170, 178)
	cFaint = rgb(110, 116, 124)
	cAccent = rgb(90, 168, 225)
	cAccent2 = rgb(126, 198, 248)
	cReady = rgb(60, 206, 118)
	cBlueDark = rgb(47, 53, 60)
	cSpecFill = rgb(35, 63, 82)
	cSpecLine = rgb(110, 180, 230)
	cSpecTrace = rgb(124, 132, 138)
	cWarn = rgb(224, 174, 74)
	cDanger = rgb(224, 79, 79)
	cSurfaceDeep = rgb(24, 24, 26)
	cSurfaceAlt = rgb(29, 30, 32)
	cSurfaceRaised = rgb(36, 37, 39)
	cWaveInactive = rgb(112, 116, 121)
	cClipGuide = rgb(104, 52, 56)
	cSpecShadow = rgb(64, 78, 88)
	cVUGreenOn = rgb(63, 190, 121)
	cVUGreenOff = rgb(33, 48, 39)
	cVUYellowOff = rgb(54, 47, 31)
	cVURedOff = rgb(57, 31, 34)
	cVUHighlight = rgb(224, 239, 230)
	cSplitterLine = rgb(67, 70, 75)
	cSplitterHover = rgb(92, 134, 166)
	cCorrelationNeg = rgb(192, 70, 86)
	cCorrelationPos = rgb(54, 180, 113)
	cTargetZone = rgb(31, 39, 46)
	cHistoryOutline = rgb(10, 10, 12)
	cStereoTrail = rgb(38, 88, 105)
	cStereoLine = rgb(60, 153, 181)
	cStereoCore = rgb(116, 218, 238)
	cStereoPeak = rgb(236, 253, 255)
	cStereoFill = rgb(27, 52, 60)
}

func init() { setTheme(0) }

func rectsOverlap(a, b RECT) bool {
	return a.Left < b.Right && a.Right > b.Left && a.Top < b.Bottom && a.Bottom > b.Top
}

func (a *App) ensureBackBuffer(hdc uintptr, w, h int32) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	if a.backDC == 0 {
		dc, _, _ := procCreateCompatibleDC.Call(hdc)
		if dc == 0 {
			return false
		}
		a.backDC = dc
	}
	if a.backBitmap != 0 && a.backW == w && a.backH == h {
		return true
	}
	if a.backBitmap != 0 {
		if a.backOldBitmap != 0 {
			procSelectObject.Call(a.backDC, a.backOldBitmap)
		}
		procDeleteObject.Call(a.backBitmap)
		a.backBitmap = 0
	}
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return false
	}
	old, _, _ := procSelectObject.Call(a.backDC, bmp)
	if a.backOldBitmap == 0 {
		a.backOldBitmap = old
	}
	a.backBitmap = bmp
	a.backW, a.backH = w, h
	return true
}

func (a *App) destroyBackBuffer() {
	if a.backDC != 0 {
		if a.backBitmap != 0 {
			if a.backOldBitmap != 0 {
				procSelectObject.Call(a.backDC, a.backOldBitmap)
			}
			procDeleteObject.Call(a.backBitmap)
		}
		procDeleteDC.Call(a.backDC)
	}
	a.backDC, a.backBitmap, a.backOldBitmap = 0, 0, 0
	a.backW, a.backH = 0, 0
}

func (a *App) paint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var physicalClient RECT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&physicalClient)))
	pw, ph := rectW(physicalClient), rectH(physicalClient)
	if pw <= 0 || ph <= 0 || !a.ensureBackBuffer(hdc, pw, ph) {
		return
	}
	dpi := a.currentDPI()
	logicalClient := RECT{0, 0, unscaleDPI(pw, dpi), unscaleDPI(ph, dpi)}
	if logicalClient.Right <= 0 || logicalClient.Bottom <= 0 {
		return
	}

	paintRect := ps.RcPaint
	if rectW(paintRect) <= 0 || rectH(paintRect) <= 0 {
		paintRect = physicalClient
	}
	logicalPaint := a.physicalToLogicalRect(paintRect)
	logicalPaint = inflateRect(logicalPaint, 2, 2)
	if logicalPaint.Left < 0 {
		logicalPaint.Left = 0
	}
	if logicalPaint.Top < 0 {
		logicalPaint.Top = 0
	}
	if logicalPaint.Right > logicalClient.Right {
		logicalPaint.Right = logicalClient.Right
	}
	if logicalPaint.Bottom > logicalClient.Bottom {
		logicalPaint.Bottom = logicalClient.Bottom
	}

	mem := a.backDC
	saved, _, _ := procSaveDC.Call(mem)
	if saved != 0 {
		defer procRestoreDC.Call(mem, saved)
	}
	procSetMapMode.Call(mem, MM_ANISOTROPIC)
	procSetWindowExtEx.Call(mem, uintptr(logicalClient.Right), uintptr(logicalClient.Bottom), 0)
	procSetViewportExtEx.Call(mem, uintptr(pw), uintptr(ph), 0)
	procSetStretchBltMode.Call(mem, HALFTONE)
	procIntersectClipRect.Call(mem, uintptr(logicalPaint.Left), uintptr(logicalPaint.Top), uintptr(logicalPaint.Right), uintptr(logicalPaint.Bottom))

	a.layout = a.calculateLayout(logicalClient)
	dirty := a.paintDirty
	fullPaint := paintRect.Left <= physicalClient.Left && paintRect.Top <= physicalClient.Top && paintRect.Right >= physicalClient.Right && paintRect.Bottom >= physicalClient.Bottom
	useDirty := dirty != 0 && !fullPaint
	shouldDraw := func(bit uint32, r RECT) bool {
		if useDirty {
			return dirty&bit != 0
		}
		return rectsOverlap(logicalPaint, r)
	}

	if !useDirty {
		fill(mem, logicalPaint, cBG)
	}
	if shouldDraw(dirtyHeader, a.layout.Header) {
		a.drawHeader(mem)
	}
	if shouldDraw(dirtyPlaylist, a.layout.Playlist) {
		a.drawPlaylistPanel(mem)
	}
	if !useDirty && rectsOverlap(logicalPaint, a.layout.FileInfo) {
		a.drawFileInfoPanel(mem)
	}
	if shouldDraw(dirtyWave, a.layout.Wave) {
		a.drawWavePanel(mem)
	}
	if shouldDraw(dirtyLoudness, a.layout.Loudness) {
		a.drawLoudnessHistory(mem)
	}
	if shouldDraw(dirtySpectrum, a.layout.Spectrum) {
		if a.analyzerView == analyzerViewSpectrum {
			a.drawSpectrumPanel(mem)
		} else {
			a.drawSpectrogramPanel(mem)
		}
	}
	if shouldDraw(dirtyMeter, a.layout.Meter) {
		a.drawMeterPanel(mem)
	}
	if shouldDraw(dirtyStereo, a.layout.Stereo) {
		a.drawStereoPanel(mem)
	}
	if shouldDraw(dirtyLevel, a.layout.VU) {
		a.drawLevelMeterPanel(mem)
	}
	if shouldDraw(dirtyControls, a.layout.Controls) {
		a.drawControls(mem)
	}
	if useDirty {
		if dirty&dirtySplitters != 0 {
			a.drawSplitters(mem)
		}
	} else if rectsOverlap(logicalPaint, a.layout.SplitPlaylist) || rectsOverlap(logicalPaint, a.layout.SplitRight) || rectsOverlap(logicalPaint, a.layout.SplitPlaylistInfo) || rectsOverlap(logicalPaint, a.layout.SplitLoudnessSpectrum) || rectsOverlap(logicalPaint, a.layout.SplitMeterStereo) {
		a.drawSplitters(mem)
	}

	procSetMapMode.Call(mem, MM_TEXT)
	procBitBlt.Call(hdc, uintptr(paintRect.Left), uintptr(paintRect.Top), uintptr(rectW(paintRect)), uintptr(rectH(paintRect)), mem, uintptr(paintRect.Left), uintptr(paintRect.Top), SRCCOPY)
	if dirty != 0 {
		a.paintDirty &^= dirty
	}
}

func (a *App) calculateLayout(c RECT) Layout {
	l := Layout{Client: c}
	headerH := int32(58)
	controlsH := int32(96)
	split := int32(7)
	controlsTop := c.Bottom - controlsH
	contentW := rectW(c)
	playlistW := a.playlistWidth
	if playlistW < 210 {
		playlistW = 210
	}
	if playlistW > 420 {
		playlistW = 420
	}
	rightW := a.rightWidth
	if rightW < 360 {
		rightW = 360
	}
	if rightW > 620 {
		rightW = 620
	}
	minCenter := int32(520)
	if playlistW+rightW+split*2+minCenter > contentW {
		over := playlistW + rightW + split*2 + minCenter - contentW
		cutP := over / 2
		cutR := over - cutP
		playlistW -= cutP
		rightW -= cutR
		if playlistW < 210 {
			playlistW = 210
		}
		if rightW < 240 {
			rightW = 240
		}
	}

	l.Header = RECT{0, 0, playlistW, headerH}
	l.Controls = makeRect(0, controlsTop, rectW(c), controlsH)
	l.Content = RECT{0, 0, c.Right, controlsTop}
	minPlaylistH := int32(180)
	minInfoH := int32(140)
	fileInfoH := a.fileInfoHeight
	maxInfoH := controlsTop - headerH - split - minPlaylistH
	if maxInfoH < minInfoH {
		maxInfoH = minInfoH
	}
	if fileInfoH < minInfoH {
		fileInfoH = minInfoH
	}
	if fileInfoH > maxInfoH {
		fileInfoH = maxInfoH
	}
	l.FileInfo = RECT{0, controlsTop - fileInfoH, playlistW, controlsTop}
	l.FileInfoFolderBtn = RECT{}
	l.FileInfoCopyBtn = RECT{}
	l.FileInfoPath = RECT{}
	l.SplitPlaylistInfo = RECT{0, l.FileInfo.Top - split, playlistW, l.FileInfo.Top}
	l.Playlist = RECT{0, headerH, playlistW, l.SplitPlaylistInfo.Top}
	l.SplitPlaylist = RECT{playlistW, 0, playlistW + split, controlsTop}
	l.Right = RECT{c.Right - rightW, 0, c.Right, controlsTop}
	l.SplitRight = RECT{l.Right.Left - split, 0, l.Right.Left, controlsTop}
	l.Center = RECT{l.SplitPlaylist.Right, 0, l.SplitRight.Left, controlsTop}

	// Center column order: waveform, loudness history, spectrum.
	l.MetricStrip = RECT{}
	waveH := int32(200)
	minWave := int32(170)
	minHistory := int32(110)
	minSpec := int32(170)
	maxWave := rectH(l.Center) - split - minHistory - minSpec
	if maxWave < minWave {
		maxWave = minWave
	}
	if waveH > maxWave {
		waveH = maxWave
	}
	l.Wave = RECT{l.Center.Left, l.Center.Top, l.Center.Right, l.Center.Top + waveH}

	historyH := a.loudnessHeight
	maxHistory := l.Center.Bottom - l.Wave.Bottom - split - minSpec
	if historyH < minHistory {
		historyH = minHistory
	}
	if historyH > maxHistory {
		historyH = maxHistory
	}
	l.Loudness = RECT{l.Center.Left, l.Wave.Bottom, l.Center.Right, l.Wave.Bottom + historyH}
	l.SplitLoudnessSpectrum = RECT{l.Center.Left, l.Loudness.Bottom, l.Center.Right, l.Loudness.Bottom + split}
	l.Spectrum = RECT{l.Center.Left, l.SplitLoudnessSpectrum.Bottom, l.Center.Right, l.Center.Bottom}

	totalRH := rectH(l.Right)
	minMeter := int32(286)
	minStereo := int32(180)
	meterH := a.meterHeight
	if meterH < minMeter {
		meterH = minMeter
	}
	if meterH > totalRH-minStereo-split {
		meterH = totalRH - minStereo - split
	}
	vuW := int32(94)
	analysisRight := l.Right.Right - vuW - split
	if analysisRight < l.Right.Left+250 {
		analysisRight = l.Right.Left + 250
		vuW = l.Right.Right - analysisRight - split
		if vuW < 76 {
			vuW = 76
			analysisRight = l.Right.Right - vuW - split
		}
	}
	l.Meter = RECT{l.Right.Left, l.Right.Top, analysisRight, l.Right.Top + meterH}
	l.SplitMeterStereo = RECT{l.Right.Left, l.Meter.Bottom, analysisRight, l.Meter.Bottom + split}
	l.Stereo = RECT{l.Right.Left, l.SplitMeterStereo.Bottom, analysisRight, l.Right.Bottom}
	l.VU = RECT{analysisRight + split, l.Right.Top, l.Right.Right, l.Right.Bottom}

	// Playlist header actions.
	l.OpenBtn = makeRect(l.Playlist.Right-44, l.Playlist.Top+13, 28, 28)
	l.ClearPlaylistBtn = makeRect(l.Playlist.Right-80, l.Playlist.Top+13, 28, 28)
	l.PlaylistSelector = RECT{l.Playlist.Left + 12, l.Playlist.Top + 9, l.ClearPlaylistBtn.Left - 6, l.Playlist.Top + 45}

	// Bottom transport + audio routing layout. Keep input/output together as a
	// compact routing block on the far right; volume sits immediately to its left.
	// On narrower windows the transport group shifts left just enough to avoid
	// colliding with the routing controls.
	progressLeft := int32(14)
	progressRight := c.Right - 14
	l.Progress = RECT{progressLeft, l.Controls.Top + 5, progressRight, l.Controls.Top + 9}

	routeW := int32(188)
	routeLeft := c.Right - 14 - routeW
	if routeLeft < 720 {
		routeW = 166
		routeLeft = c.Right - 12 - routeW
	}
	// Input sits above output so the routing block reads from source to destination.
	l.InputBtn = makeRect(routeLeft, l.Controls.Top+31, routeW, 26)
	l.DeviceBtn = makeRect(routeLeft, l.Controls.Top+57, routeW, 26)
	volumeRight := routeLeft - 18
	// Align the transport and volume controls to the visual midline of the footer.
	// They previously sat about 8 px too low relative to the progress baseline.
	l.Volume = makeRect(volumeRight-108, l.Controls.Top+55, 108, 5)
	l.Speaker = makeRect(l.Volume.Left-34, l.Volume.Top-12, 28, 28)

	cy := l.Controls.Top + 57
	center := rectW(c) / 2
	maxCenter := l.Speaker.Left - 195
	if center > maxCenter {
		center = maxCenter
	}
	if center < 430 {
		center = 430
	}
	l.PrevBtn = makeRect(center-126, cy-19, 38, 38)
	l.PlayBtn = makeRect(center-72, cy-25, 50, 50)
	l.NextBtn = makeRect(center-7, cy-19, 38, 38)
	l.StopBtn = makeRect(center+43, cy-19, 38, 38)
	l.LoopBtn = makeRect(center+91, cy-19, 48, 38)
	l.PlayModeBtn = makeRect(center+147, cy-19, 38, 38)
	l.ResetBtn = makeRect(l.Meter.Right-64, l.Meter.Top+8, 50, 22)
	l.ThemeBtn = makeRect(l.Header.Right-38, l.Header.Top+15, 28, 28)
	historyPanelTop := l.Loudness.Top + 7
	l.LoudnessFullBtn = makeRect(l.Loudness.Right-56, historyPanelTop, 42, 18)
	l.Loudness60Btn = makeRect(l.LoudnessFullBtn.Left-40, historyPanelTop, 36, 18)
	l.Loudness30Btn = makeRect(l.Loudness60Btn.Left-40, historyPanelTop, 36, 18)
	l.Loudness15Btn = makeRect(l.Loudness30Btn.Left-40, historyPanelTop, 36, 18)
	l.Loudness5Btn = makeRect(l.Loudness15Btn.Left-36, historyPanelTop, 32, 18)
	l.LoudnessTargetBtn = makeRect(l.Loudness5Btn.Left-72, historyPanelTop, 66, 18)
	controlY := l.Spectrum.Top + 7
	l.AnalyzerViewBtn = makeRect(l.Spectrum.Left+12, controlY, 96, 20)
	l.SpectrumFreezeBtn = makeRect(l.Spectrum.Right-70, controlY, 58, 20)
	l.SpectrumHoldBtn = makeRect(l.SpectrumFreezeBtn.Left-54, controlY, 48, 20)
	l.SpectrumBandBtn = makeRect(l.SpectrumHoldBtn.Left-76, controlY, 70, 20)
	l.SpectrumTiltBtn = makeRect(l.SpectrumBandBtn.Left-70, controlY, 64, 20)
	l.SpectrumSmoothBtn = makeRect(l.SpectrumTiltBtn.Left-60, controlY, 54, 20)
	l.SpectrumFFTBtn = makeRect(l.SpectrumSmoothBtn.Left-60, controlY, 54, 20)
	l.SpectrogramFreezeBtn = l.SpectrumFreezeBtn
	l.SpectrogramRangeBtn = makeRect(l.SpectrogramFreezeBtn.Left-78, controlY, 72, 20)
	l.SpectrogramTimeBtn = makeRect(l.SpectrogramRangeBtn.Left-70, controlY, 64, 20)
	l.SpectrogramScaleBtn = makeRect(l.SpectrogramTimeBtn.Left-80, controlY, 74, 20)
	if a.analyzerView == analyzerViewLiveSpectrogram {
		l.SpectrumFFTBtn = makeRect(l.SpectrogramScaleBtn.Left-60, controlY, 54, 20)
	}
	modeW, styleW, normalizeW := int32(64), int32(50), int32(68)
	if rectW(l.Stereo) < 290 {
		modeW, styleW, normalizeW = 56, 44, 50
	}
	l.StereoModeBtn = makeRect(l.Stereo.Right-12-modeW, l.Stereo.Top+5, modeW, 22)
	l.StereoStyleBtn = makeRect(l.StereoModeBtn.Left-6-styleW, l.Stereo.Top+5, styleW, 22)
	l.StereoNormalizeBtn = makeRect(l.StereoStyleBtn.Left-6-normalizeW, l.Stereo.Top+5, normalizeW, 22)
	waveControlY := l.Wave.Top + 6
	l.WaveZoomOutBtn = makeRect(l.Wave.Left+100, waveControlY, 22, 18)
	l.WaveFitBtn = makeRect(l.WaveZoomOutBtn.Right+4, waveControlY, 42, 18)
	l.WaveZoomInBtn = makeRect(l.WaveFitBtn.Right+4, waveControlY, 22, 18)
	return l
}

func logoMenuRect(header RECT) RECT {
	return makeRect(header.Left+7, header.Top+9, 40, 40)
}

func (a *App) drawHeader(dc uintptr) {
	r := a.layout.Header
	fill(dc, r, cHeader)
	line(dc, r.Left, r.Bottom-1, r.Right, r.Bottom-1, cBorder, 1)
	line(dc, r.Right-1, r.Top, r.Right-1, r.Bottom, cBorder, 1)

	logo := logoMenuRect(r)
	if pointInRect(a.hover.X, a.hover.Y, logo) {
		fillRound(dc, logo, 8, cPanel2)
		strokeRound(dc, logo, 8, cAccent2, 1)
	}
	drawUIAsset(dc, a.uiImages, "brand_wave", inflateRect(logo, -5, -5))

	th := a.layout.ThemeBtn
	compact := rectW(r) < 250
	status := makeRect(r.Right-116, r.Top+15, 70, 28)
	if compact {
		status = makeRect(r.Right-74, r.Top+15, 28, 28)
	}
	titleRight := status.Left - 8
	titleW := titleRight - (r.Left + 50)
	if titleW < 36 {
		titleW = 36
	}
	brandTitle := "Auralux Player"
	brandFont := a.fontTitle
	if compact {
		brandTitle = "Auralux"
		brandFont = a.fontSection
	}
	text(dc, brandTitle, makeRect(r.Left+50, r.Top+11, titleW, 29), brandFont, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	// Compact engine status, adaptive to the playlist width.
	fillRound(dc, status, 8, cPanel3)
	strokeRound(dc, status, 8, cBorder, 1)
	ledAsset := "led_wait"
	label := "WAIT"
	labelCol := cMuted
	if a.engineReady.Load() {
		ledAsset = "led_ready"
		label = "READY"
		labelCol = cText
	}
	if compact {
		drawUIAsset(dc, a.uiImages, ledAsset, makeRect(status.Left+5, status.Top+5, 18, 18))
	} else {
		drawUIAsset(dc, a.uiImages, ledAsset, makeRect(status.Left+7, status.Top+5, 18, 18))
		text(dc, label, makeRect(status.Left+29, status.Top, rectW(status)-34, rectH(status)), a.fontTiny, labelCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}

	// Theme icon button.
	hoveredTheme := pointInRect(a.hover.X, a.hover.Y, th)
	bg := cPanel3
	border := cBorder
	if hoveredTheme {
		bg = cPanel2
		border = cAccent2
	}
	fillRound(dc, th, 8, bg)
	strokeRound(dc, th, 8, border, 1)
	iconName := "theme_moon"
	if a.themeMode == 1 {
		iconName = "theme_sun"
	}
	drawUIAsset(dc, a.uiImages, iconName, makeRect(th.Left+4, th.Top+4, rectW(th)-8, rectH(th)-8))
}

const (
	playlistDropdownRowHeight int32 = 30
	playlistDropdownPadding   int32 = 6
	playlistDropdownSeparator int32 = 9
)

type playlistDropdownLayout struct {
	Rect             RECT
	ListTop          int32
	VisiblePlaylists int
	AddAudio         RECT
	Rename           RECT
	Delete           RECT
}

func (a *App) playlistDropdownGeometry() playlistDropdownLayout {
	left := a.layout.Playlist.Left + 8
	right := a.layout.Playlist.Right - 8
	top := a.layout.Playlist.Top + 54
	maxBottom := a.layout.Playlist.Bottom - 8
	fixedHeight := playlistDropdownPadding*2 + playlistDropdownSeparator + playlistDropdownRowHeight*3
	visible := int((maxBottom - top - fixedHeight) / playlistDropdownRowHeight)
	if visible < 1 {
		visible = 1
	}
	if visible > len(a.playlists) {
		visible = len(a.playlists)
	}
	listTop := top + playlistDropdownPadding
	actionsTop := listTop + int32(visible)*playlistDropdownRowHeight + playlistDropdownSeparator
	return playlistDropdownLayout{
		Rect:             RECT{left, top, right, actionsTop + playlistDropdownRowHeight*3 + playlistDropdownPadding},
		ListTop:          listTop,
		VisiblePlaylists: visible,
		AddAudio:         makeRect(left+playlistDropdownPadding, actionsTop, right-left-playlistDropdownPadding*2, playlistDropdownRowHeight),
		Rename:           makeRect(left+playlistDropdownPadding, actionsTop+playlistDropdownRowHeight, right-left-playlistDropdownPadding*2, playlistDropdownRowHeight),
		Delete:           makeRect(left+playlistDropdownPadding, actionsTop+playlistDropdownRowHeight*2, right-left-playlistDropdownPadding*2, playlistDropdownRowHeight),
	}
}

func (a *App) clampPlaylistMenuScroll(visible int) {
	maxScroll := len(a.playlists) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.playlistMenuScroll < 0 {
		a.playlistMenuScroll = 0
	}
	if a.playlistMenuScroll > maxScroll {
		a.playlistMenuScroll = maxScroll
	}
}

func (a *App) playlistDropdownIndexAt(x, y int32, geometry playlistDropdownLayout) int {
	listBottom := geometry.ListTop + int32(geometry.VisiblePlaylists)*playlistDropdownRowHeight
	if x < geometry.Rect.Left || x >= geometry.Rect.Right || y < geometry.ListTop || y >= listBottom {
		return -1
	}
	index := a.playlistMenuScroll + int((y-geometry.ListTop)/playlistDropdownRowHeight)
	if index < 0 || index >= len(a.playlists) {
		return -1
	}
	return index
}

func (a *App) drawPlaylistDropdown(dc uintptr) {
	if !a.playlistMenuOpen {
		return
	}
	geometry := a.playlistDropdownGeometry()
	a.clampPlaylistMenuScroll(geometry.VisiblePlaylists)
	shadow := RECT{geometry.Rect.Left + 3, geometry.Rect.Top + 4, geometry.Rect.Right + 3, geometry.Rect.Bottom + 4}
	fillRound(dc, shadow, 8, cBorder)
	fillRound(dc, geometry.Rect, 8, cSurfaceRaised)
	strokeRound(dc, geometry.Rect, 8, cBorder, 1)

	for row := 0; row < geometry.VisiblePlaylists; row++ {
		index := a.playlistMenuScroll + row
		if index >= len(a.playlists) {
			break
		}
		r := makeRect(geometry.Rect.Left+playlistDropdownPadding, geometry.ListTop+int32(row)*playlistDropdownRowHeight, rectW(geometry.Rect)-playlistDropdownPadding*2, playlistDropdownRowHeight)
		hovered := pointInRect(a.hover.X, a.hover.Y, r)
		active := a.playlists[index].ID == a.activePlaylistID
		if active {
			fillRound(dc, r, 5, cBlueDark)
		} else if hovered {
			fillRound(dc, r, 5, cPanel2)
		}
		if active {
			fillRound(dc, makeRect(r.Left+8, r.Top+(rectH(r)-6)/2, 6, 6), 3, cAccent2)
		}
		color := cMuted
		if active || hovered {
			color = cText
		}
		text(dc, playlistDisplayName(a.playlists[index]), makeRect(r.Left+22, r.Top, rectW(r)-30, rectH(r)), a.fontSmall, color, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if a.playlistMenuScroll > 0 {
		drawUIAsset(dc, a.uiImages, "scroll_up", makeRect(geometry.Rect.Right-24, geometry.ListTop+3, 14, 14))
	}
	if a.playlistMenuScroll+geometry.VisiblePlaylists < len(a.playlists) {
		bottomRowTop := geometry.ListTop + int32(geometry.VisiblePlaylists-1)*playlistDropdownRowHeight
		drawUIAsset(dc, a.uiImages, "scroll_down", makeRect(geometry.Rect.Right-24, bottomRowTop+3, 14, 14))
	}

	separatorY := geometry.AddAudio.Top - playlistDropdownSeparator/2
	line(dc, geometry.Rect.Left+12, separatorY, geometry.Rect.Right-12, separatorY, cBorder, 1)
	drawAction := func(r RECT, label string, enabled bool, danger bool) {
		hovered := enabled && pointInRect(a.hover.X, a.hover.Y, r)
		if hovered {
			fillRound(dc, r, 5, cPanel2)
		}
		color := cText
		if !enabled {
			color = cFaint
		} else if danger {
			color = cDanger
		} else if hovered {
			color = cAccent2
		}
		text(dc, label, makeRect(r.Left+12, r.Top, rectW(r)-24, rectH(r)), a.fontSmall, color, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	custom := a.activePlaylistID != defaultPlaylistID
	drawAction(geometry.AddAudio, tr("添加音频…"), true, false)
	drawAction(geometry.Rename, tr("重命名当前 Playlist…"), custom, false)
	drawAction(geometry.Delete, tr("删除当前 Playlist…"), custom, true)
}

func (a *App) drawPlaylistPanel(dc uintptr) {
	r := a.layout.Playlist
	fill(dc, r, cPanel)
	line(dc, r.Right-1, r.Top, r.Right-1, r.Bottom, cBorder, 1)
	p := a.activePlaylist()
	a.clampPlaylistScroll()
	selector := a.layout.PlaylistSelector
	if pointInRect(a.hover.X, a.hover.Y, selector) {
		fillRound(dc, selector, 6, cPanel2)
	}
	name := "Default playlist"
	if p != nil && p.ID != defaultPlaylistID {
		name = p.Name
	}
	text(dc, name, RECT{selector.Left + 6, selector.Top, selector.Right - 22, selector.Bottom}, a.fontSection, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	chevronName := "chevron_down"
	if a.playlistMenuOpen {
		chevronName = "chevron_up"
	}
	drawUIAsset(dc, a.uiImages, chevronName, makeRect(selector.Right-20, selector.Top+(rectH(selector)-14)/2, 14, 14))
	drawUIAsset(dc, a.uiImages, func() string {
		if pointInRect(a.hover.X, a.hover.Y, a.layout.ClearPlaylistBtn) {
			return "playlist_clear_hover"
		}
		return "playlist_clear"
	}(), a.layout.ClearPlaylistBtn)
	drawUIAsset(dc, a.uiImages, func() string {
		if pointInRect(a.hover.X, a.hover.Y, a.layout.OpenBtn) {
			return "playlist_add_hover"
		}
		return "playlist_add"
	}(), a.layout.OpenBtn)
	line(dc, r.Left, r.Top+52, r.Right, r.Top+52, cBorder, 1)

	top, bottom := a.playlistListBounds()
	visible := a.playlistVisibleRows()
	if p == nil || len(p.Tracks) == 0 {
		text(dc, "No audio yet\nDrop files here or add audio from the playlist menu.", RECT{r.Left + 18, top, r.Right - 18, bottom}, a.fontBody, cFaint, DT_CENTER|DT_VCENTER)
	} else {
		selection := make(map[int]bool, 1)
		for _, selectedIdx := range a.selectedRows() {
			selection[selectedIdx] = true
		}
		for row := 0; row < visible; row++ {
			idx := p.Scroll + row
			if idx >= len(p.Tracks) {
				break
			}
			rr := a.playlistRowRect(idx)
			selected := selection[idx]
			current := p.ID == a.playbackPlaylistID && idx == a.current
			if selected {
				fillRound(dc, rr, 5, cBlueDark)
				// The anchor keeps the historic accent bar; rows added by a
				// Ctrl/Shift extension get the secondary accent so the active
				// row stays readable inside a multi-selection.
				barColor := cAccent2
				if idx == p.Selected {
					barColor = cAccent
				}
				fill(dc, RECT{rr.Left, rr.Top, rr.Left + 2, rr.Bottom}, barColor)
			} else if pointInRect(a.hover.X, a.hover.Y, rr) {
				fillRound(dc, rr, 5, cPanel2)
			}
			trackAsset := "playlist_track"
			if current {
				trackAsset = "playlist_track_current"
			}
			drawUIAsset(dc, a.uiImages, trackAsset, makeRect(rr.Left+10, rr.Top+9, 26, 26))
			t := p.Tracks[idx]
			nameCol := cText
			if t.Missing {
				nameCol = cFaint
			}
			text(dc, t.Name, makeRect(rr.Left+44, rr.Top+5, rectW(rr)-88, 20), a.fontSmall, nameCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(t.Path), "."))
			info := ext
			if t.Missing {
				info = ext + "  •  OFFLINE"
			}
			if current {
				rate, ch := a.engine.StreamInfo()
				info = fmt.Sprintf("%s  •  %.1f kHz  •  %d ch", ext, float64(rate)/1000, ch)
			}
			text(dc, info, makeRect(rr.Left+44, rr.Top+27, rectW(rr)-82, 16), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			removeR := playlistRemoveRect(rr)
			hoverRemove := pointInRect(a.hover.X, a.hover.Y, removeR)
			showRemove := pointInRect(a.hover.X, a.hover.Y, rr) || selected
			if showRemove {
				asset := "playlist_remove"
				if hoverRemove {
					asset = "playlist_remove_hover"
				}
				drawUIAsset(dc, a.uiImages, asset, removeR)
			} else if current {
				text(dc, formatTime(a.engine.DurationSeconds()), makeRect(rr.Right-48, rr.Top+5, 40, 20), a.fontTiny, cAccent2, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
			}
		}
	}
	if track, thumb, ok := a.playlistScrollRects(); ok {
		a.layout.PlaylistScrollTrack = track
		a.layout.PlaylistScrollThumb = thumb
		fillRound(dc, track, 3, cSurfaceDeep)
		thumbCol := cMuted
		if a.dragMode == dragPlaylistScrollMode || pointInRect(a.hover.X, a.hover.Y, inflateRect(thumb, 5, 2)) {
			thumbCol = cAccent2
		}
		fillRound(dc, thumb, 3, thumbCol)
	} else {
		a.layout.PlaylistScrollTrack = RECT{}
		a.layout.PlaylistScrollThumb = RECT{}
	}
	itemCount := 0
	if p != nil {
		itemCount = len(p.Tracks)
	}
	text(dc, fmt.Sprintf("%d items", itemCount), makeRect(r.Left+18, r.Bottom-26, r.Right-r.Left-36, 20), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	a.drawPlaylistDropdown(dc)
}

func drawFileInfoCell(dc uintptr, r RECT, label, value string, accent uint32, fontLabel, fontValue uintptr) {
	fillRound(dc, r, 5, cSurfaceDeep)
	strokeRound(dc, r, 5, cBorder, 1)
	line(dc, r.Left+1, r.Top+1, r.Right-1, r.Top+1, accent, 1)
	text(dc, label, makeRect(r.Left+8, r.Top+4, rectW(r)-16, 12), fontLabel, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(dc, value, makeRect(r.Left+8, r.Top+16, rectW(r)-16, rectH(r)-18), fontValue, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func (a *App) drawFileInfoPanel(dc uintptr) {
	r := a.layout.FileInfo
	fill(dc, r, cPanel)
	line(dc, r.Right-1, r.Top, r.Right-1, r.Bottom, cBorder, 1)
	text(dc, "FILE INFO", makeRect(r.Left+18, r.Top+10, 120, 20), a.fontSmall, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	line(dc, r.Left, r.Top+34, r.Right, r.Top+34, cBorder, 1)

	track, currentLoaded := a.selectedFileInfoTrack()
	if track == nil {
		text(dc, "No file loaded", makeRect(r.Left+18, r.Top+52, rectW(r)-36, 20), a.fontBody, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		text(dc, "Select or play a file to see its details.", makeRect(r.Left+18, r.Top+76, rectW(r)-36, 18), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	t := *track
	name := t.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(t.Path)
	}

	var size int64
	modified := "—"
	if st, err := os.Stat(t.Path); err == nil {
		size = st.Size()
		modified = st.ModTime().Format("2006-01-02 15:04")
	}
	var rate, ch uint32
	dur := 0.0
	if currentLoaded {
		rate, ch = a.engine.StreamInfo()
		dur = a.engine.DurationSeconds()
	}
	channelLabel := channelLayoutLabel(int(ch))
	if channelLabel == "—" {
		channelLabel = "Stereo"
	}
	text(dc, name, makeRect(r.Left+18, r.Top+44, rectW(r)-36, 22), a.fontBody, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	badgeY := r.Top + 68
	badgeGap := int32(6)
	badge1 := makeRect(r.Left+18, badgeY, 54, 18)
	badge2 := makeRect(badge1.Right+badgeGap, badgeY, 62, 18)
	badge3 := makeRect(badge2.Right+badgeGap, badgeY, 58, 18)
	badge4 := makeRect(badge3.Right+badgeGap, badgeY, 64, 18)
	for _, b := range []RECT{badge1, badge2, badge3, badge4} {
		fillRound(dc, b, 9, cSurfaceRaised)
		strokeRound(dc, b, 9, cBorder, 1)
	}
	text(dc, fileExtLabel(t.Path), badge1, a.fontTiny, cAccent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(dc, channelLabel, badge2, a.fontTiny, cReady, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(dc, func() string {
		if dur > 0 {
			return formatTime(dur)
		}
		return "--:--"
	}(), badge3, a.fontTiny, cAccent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(dc, func() string {
		if size > 0 {
			return formatBytes(size)
		}
		return "—"
	}(), badge4, a.fontTiny, cWarn, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	text(dc, "DETAILS", makeRect(r.Left+18, r.Top+96, 90, 14), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	detailsBox := RECT{r.Left + 18, r.Top + 112, r.Right - 16, r.Bottom - 12}
	fillRound(dc, detailsBox, 8, cSurfaceDeep)
	strokeRound(dc, detailsBox, 8, cBorder, 1)

	rows := []struct{ label, value string }{
		{"Format", fileExtLabel(t.Path)},
		{"Channels", channelLabel},
		{"Duration", func() string {
			if dur > 0 {
				return formatTime(dur)
			}
			return "—"
		}()},
		{"File Size", func() string {
			if size > 0 {
				return formatBytes(size)
			}
			return "—"
		}()},
		{"Sample Rate", func() string {
			if rate > 0 {
				return fmt.Sprintf("%.1f kHz", float64(rate)/1000)
			}
			return "—"
		}()},
		{"Bit Depth", formatWAVBitDepth(t.Path)},
		{"Avg Bitrate", formatAvgBitrate(size, dur)},
		{"Modified", modified},
	}
	rowCount := int32(len(rows))
	innerTop := detailsBox.Top + 8
	innerBottom := detailsBox.Bottom - 8
	rowH := (innerBottom - innerTop) / rowCount
	if rowH < 16 {
		rowH = 16
	}
	labelW := int32(88)
	for i, row := range rows {
		y := innerTop + int32(i)*rowH
		if y+rowH > detailsBox.Bottom-4 {
			break
		}
		if i > 0 {
			line(dc, detailsBox.Left+8, y, detailsBox.Right-8, y, cBorder, 1)
		}
		text(dc, row.label, makeRect(detailsBox.Left+10, y, labelW, rowH), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		text(dc, row.value, makeRect(detailsBox.Left+10+labelW, y, rectW(detailsBox)-labelW-20, rowH), a.fontTiny, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func (a *App) drawMetricStrip(dc uintptr) {
	r := a.layout.MetricStrip
	fill(dc, r, cHeader)
	line(dc, r.Left, r.Bottom-1, r.Right, r.Bottom-1, cBorder, 1)
	peak := maxf(ampToDB(a.peakL), ampToDB(a.peakR))
	truePeak := a.truePeak
	lufsI, lufsS, lufsM, lra := a.lufsI, a.lufsS, a.lufsM, a.lra
	if a.meterDisplayInit {
		peak = a.meterDisplayPeak
		truePeak = a.meterDisplayTruePeak
		lufsI, lufsS, lufsM, lra = a.meterDisplayI, a.meterDisplayS, a.meterDisplayM, a.meterDisplayLRA
	}
	items := []struct {
		label string
		value string
		col   uint32
	}{
		{"PEAK", dbText(peak) + " dB", metricColor(peak, 0)},
		{"TRUE PEAK", dbText(truePeak) + " dB", metricColor(truePeak, -0.1)},
		{"INTEGRATED", dbText(lufsI) + " LUFS", cWarn},
		{"SHORT TERM", dbText(lufsS) + " LUFS", cReady},
		{"MOMENTARY", dbText(lufsM) + " LUFS", cAccent2},
		{"LRA", fmt.Sprintf("%.1f LU", lra), cAccent2},
	}
	gap := int32(5)
	cardArea := RECT{r.Left, r.Top, r.Right, r.Top + 58}
	cellW := (rectW(cardArea) - gap*int32(len(items)+1)) / int32(len(items))
	for i, it := range items {
		cell := makeRect(cardArea.Left+gap+int32(i)*(cellW+gap), cardArea.Top+6, cellW, rectH(cardArea)-12)
		fillRound(dc, cell, 5, cSurfaceRaised)
		strokeRound(dc, cell, 5, cBorder, 1)
		line(dc, cell.Left+1, cell.Top+1, cell.Right-1, cell.Top+1, it.col, 2)
		text(dc, it.label, makeRect(cell.Left+8, cell.Top+5, rectW(cell)-16, 14), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		text(dc, it.value, makeRect(cell.Left+8, cell.Top+19, rectW(cell)-16, 20), a.fontSmall, it.col, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

type historyBucket struct {
	have                   bool
	segment                uint32
	time                   float64
	mSum, sSum, iSum       float64
	mMin, mMax, sMin, sMax float32
	iMin, iMax             float32
	count                  int
}

type historyRange struct {
	x          int32
	minY, maxY int32
}

type historyCurve struct {
	segments [][]POINT
	ranges   []historyRange
}

type historyRenderData struct {
	buckets                          []historyBucket
	momentary, shortTerm, integrated historyCurve
}

func (a *App) drawLoudnessHistory(dc uintptr) {
	r := a.layout.Loudness
	fill(dc, r, cPanel)
	if rectW(r) < 180 || rectH(r) < 70 {
		return
	}

	text(dc, "LOUDNESS HISTORY", makeRect(r.Left+14, r.Top+3, 138, 26), a.fontSmall, cAccent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	line(dc, r.Left+12, r.Top+30, r.Right-12, r.Top+30, cBorder, 1)

	legendX := r.Left + 166
	drawHistoryLegend(dc, legendX, r.Top+8, "M", cText, a.fontTiny)
	drawHistoryLegend(dc, legendX+38, r.Top+8, "S", cReady, a.fontTiny)
	drawHistoryLegend(dc, legendX+76, r.Top+8, "I", cWarn, a.fontTiny)

	drawHistoryWindowButton(dc, a.layout.LoudnessTargetBtn, a.loudnessTargetLabel(), true, a.fontTiny)
	drawHistoryWindowButton(dc, a.layout.Loudness5Btn, "5s", a.loudnessWindow == 0, a.fontTiny)
	drawHistoryWindowButton(dc, a.layout.Loudness15Btn, "15s", a.loudnessWindow == 1, a.fontTiny)
	drawHistoryWindowButton(dc, a.layout.Loudness30Btn, "30s", a.loudnessWindow == 2, a.fontTiny)
	drawHistoryWindowButton(dc, a.layout.Loudness60Btn, "60s", a.loudnessWindow == 3, a.fontTiny)
	drawHistoryWindowButton(dc, a.layout.LoudnessFullBtn, "FULL", a.loudnessWindow == 4, a.fontTiny)

	plot := loudnessPlotRect(r)
	if rectW(plot) < 40 || rectH(plot) < 30 {
		return
	}
	fill(dc, plot, cSurfaceDeep)
	strokeRound(dc, plot, 4, cBorder, 1)
	inner := inflateRect(plot, -3, -3)

	minDB, maxDB := a.loudnessYMin, a.loudnessYMax
	if maxDB-minDB < 6 {
		minDB, maxDB = -60, 0
	}
	gridStep := loudnessGridStep(maxDB - minDB)
	firstGrid := float32(math.Ceil(float64(minDB/gridStep))) * gridStep
	for db := firstGrid; db <= maxDB+0.001; db += gridStep {
		y := loudnessYRange(inner, db, minDB, maxDB)
		col := cGrid
		if math.Abs(float64(db)) < 0.01 || math.Abs(float64(db+60)) < 0.01 {
			col = cGridStrong
		}
		line(dc, inner.Left, y, inner.Right, y, col, 1)
		label := fmt.Sprintf("%.0f", db)
		if gridStep < 5 {
			label = fmt.Sprintf("%.1f", db)
		}
		text(dc, label, makeRect(r.Left+4, y-7, 32, 14), a.fontTiny, cFaint, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}

	// One selected delivery target plus a subtle ±1 LU acceptance zone.
	target := a.loudnessTarget()
	yTop := loudnessYRange(inner, target+1, minDB, maxDB)
	yBottom := loudnessYRange(inner, target-1, minDB, maxDB)
	if yTop > yBottom {
		yTop, yBottom = yBottom, yTop
	}
	if target+1 >= minDB && target-1 <= maxDB {
		if yTop < inner.Top {
			yTop = inner.Top
		}
		if yBottom > inner.Bottom {
			yBottom = inner.Bottom
		}
		fill(dc, RECT{inner.Left, yTop, inner.Right, yBottom + 1}, cTargetZone)
	}
	targetY := loudnessYRange(inner, target, minDB, maxDB)
	targetCol := cWarn
	if a.loudnessTargetMode == 1 {
		targetCol = cAccent2
	} else if a.loudnessTargetMode == 2 {
		targetCol = cFaint
	}
	if target >= minDB && target <= maxDB {
		drawDashedLine(dc, inner.Left, targetY, inner.Right, targetCol)
		text(dc, fmt.Sprintf("%.1f", target), makeRect(inner.Right-34, targetY-13, 32, 12), a.fontTiny, targetCol, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}

	startTime, endTime := a.loudnessViewRange()
	drawHistoryTimeLabels(dc, inner, startTime, endTime, a.fontTiny)
	drawHistoryLoopMarkers(dc, inner, a.loudnessLoopMarkers, startTime, endTime, a.fontTiny)
	zoomHint := "WHEEL H-ZOOM  ·  ALT+WHEEL V-ZOOM"
	if a.loudnessHZoom > 1.001 || minDB > -59.9 || maxDB < -0.1 {
		zoomHint = fmt.Sprintf("H %.1fx  ·  V %.0f…%.0f LUFS", a.loudnessHZoom, minDB, maxDB)
	}
	text(dc, zoomHint, makeRect(inner.Left+6, inner.Bottom-16, 220, 12), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	data := buildLoudnessRenderData(a.loudnessHistory, startTime, endTime, inner, minDB, maxDB)

	// FULL mode preserves local extremes using per-pixel min/max ranges.
	if a.loudnessWindow == 4 {
		drawHistoryRanges(dc, data.momentary.ranges, cFaint)
		drawHistoryRanges(dc, data.shortTerm.ranges, cSpecTrace)
	}
	// Crisp layered strokes improve readability on both dark and light themes.
	// M stays visually secondary, S remains the primary trend, I is the stable reference.
	drawHistoryCurveCrisp(dc, data.momentary, cText, 1)
	drawHistoryCurveCrisp(dc, data.shortTerm, cReady, 2)
	drawHistoryCurveCrisp(dc, data.integrated, cWarn, 2)

	labelM, labelS, labelI := a.lufsM, a.lufsS, a.lufsI
	if a.loudnessLabelInit {
		labelM, labelS, labelI = a.loudnessLabelM, a.loudnessLabelS, a.loudnessLabelI
	}
	drawHistoryCurrentLabels(dc, inner, labelM, labelS, labelI, minDB, maxDB, a.fontTiny)
	a.drawLoudnessHover(dc, inner, startTime, endTime, data)
}

func loudnessPlotRect(r RECT) RECT {
	return RECT{r.Left + 42, r.Top + 34, r.Right - 12, r.Bottom - 24}
}

func drawHistoryLegend(dc uintptr, x, y int32, label string, col uint32, font uintptr) {
	line(dc, x, y+7, x+13, y+7, col, 2)
	text(dc, label, makeRect(x+17, y, 18, 14), font, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
}

func drawHistoryWindowButton(dc uintptr, r RECT, label string, active bool, font uintptr) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	bg := cPanel3
	border := cBorder
	col := cMuted
	if active {
		bg = cBlueDark
		border = cAccent2
		col = cAccent2
	} else if hovered {
		border = cSplitterHover
		col = cText
	}
	fillRound(dc, r, 4, bg)
	strokeRound(dc, r, 4, border, 1)
	text(dc, label, r, font, col, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func loudnessYRange(r RECT, value, minDB, maxDB float32) int32 {
	if maxDB <= minDB {
		minDB, maxDB = -60, 0
	}
	if value > maxDB {
		value = maxDB
	}
	if value < minDB {
		value = minDB
	}
	t := float64(maxDB-value) / float64(maxDB-minDB)
	return r.Top + int32(t*float64(rectH(r)))
}

func loudnessGridStep(span float32) float32 {
	switch {
	case span >= 45:
		return 20
	case span >= 24:
		return 10
	case span >= 12:
		return 5
	default:
		return 2
	}
}

func drawDashedLine(dc uintptr, x1, y, x2 int32, col uint32) {
	const dash int32 = 5
	const gap int32 = 4
	for x := x1; x < x2; x += dash + gap {
		end := x + dash
		if end > x2 {
			end = x2
		}
		line(dc, x, y, end, y, col, 1)
	}
}

func drawHistoryTimeLabels(dc uintptr, r RECT, start, end float64, font uintptr) {
	if end < start {
		end = start
	}
	for i := 0; i <= 4; i++ {
		t := start + (end-start)*float64(i)/4
		x := r.Left + int32(i)*rectW(r)/4
		label := formatHistoryTime(t)
		flags := uint32(DT_CENTER | DT_VCENTER | DT_SINGLELINE)
		box := makeRect(x-24, r.Bottom+3, 48, 12)
		if i == 0 {
			flags = DT_LEFT | DT_VCENTER | DT_SINGLELINE
			box = makeRect(x, r.Bottom+3, 48, 12)
		} else if i == 4 {
			flags = DT_RIGHT | DT_VCENTER | DT_SINGLELINE
			box = makeRect(x-48, r.Bottom+3, 48, 12)
		}
		text(dc, label, box, font, cFaint, flags)
	}
}

func drawDashedVerticalLine(dc uintptr, x, y1, y2 int32, col uint32) {
	const dash int32 = 4
	const gap int32 = 4
	for y := y1; y < y2; y += dash + gap {
		end := y + dash
		if end > y2 {
			end = y2
		}
		line(dc, x, y, x, end, col, 1)
	}
}

func drawHistoryLoopMarkers(dc uintptr, plot RECT, markers []float64, start, end float64, font uintptr) {
	if len(markers) == 0 || end <= start {
		return
	}
	span := end - start
	lastLineX, lastLabelX := int32(-1000), int32(-1000)
	for _, t := range markers {
		if t < start || t > end {
			continue
		}
		x := plot.Left + int32((t-start)/span*float64(rectW(plot)-1))
		if x-lastLineX < 3 {
			continue
		}
		lastLineX = x
		drawDashedVerticalLine(dc, x, plot.Top+2, plot.Bottom-2, cFaint)
		if x-lastLabelX >= 42 {
			text(dc, "LOOP", makeRect(x+4, plot.Top+3, 34, 12), font, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			lastLabelX = x
		}
	}
}

func formatHistoryTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	if sec >= 3600 {
		h := int(sec) / 3600
		m := (int(sec) / 60) % 60
		return fmt.Sprintf("%d:%02d", h, m)
	}
	m := int(sec) / 60
	s := int(sec) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

func buildLoudnessRenderData(samples []LoudnessSample, start, end float64, plot RECT, minDB, maxDB float32) historyRenderData {
	width := int(rectW(plot))
	data := historyRenderData{}
	if width <= 1 || end <= start || len(samples) == 0 {
		return data
	}
	data.buckets = make([]historyBucket, width)
	span := end - start
	for _, sample := range samples {
		if sample.Time < start || sample.Time > end {
			continue
		}
		x := int((sample.Time - start) / span * float64(width-1))
		if x < 0 {
			x = 0
		}
		if x >= width {
			x = width - 1
		}
		b := &data.buckets[x]
		m := clampLoudness(sample.Momentary)
		s := clampLoudness(sample.ShortTerm)
		i := clampLoudness(sample.Integrated)
		if !b.have || sample.Segment != b.segment {
			*b = historyBucket{have: true, segment: sample.Segment, time: sample.Time, mMin: m, mMax: m, sMin: s, sMax: s, iMin: i, iMax: i}
		}
		b.time = sample.Time
		b.mSum += float64(m)
		b.sSum += float64(s)
		b.iSum += float64(i)
		if m < b.mMin {
			b.mMin = m
		}
		if m > b.mMax {
			b.mMax = m
		}
		if s < b.sMin {
			b.sMin = s
		}
		if s > b.sMax {
			b.sMax = s
		}
		if i < b.iMin {
			b.iMin = i
		}
		if i > b.iMax {
			b.iMax = i
		}
		b.count++
	}
	data.momentary = buildHistoryCurve(data.buckets, plot, 0, minDB, maxDB)
	data.shortTerm = buildHistoryCurve(data.buckets, plot, 1, minDB, maxDB)
	data.integrated = buildHistoryCurve(data.buckets, plot, 2, minDB, maxDB)
	return data
}

func buildHistoryCurve(buckets []historyBucket, plot RECT, kind int, minDB, maxDB float32) historyCurve {
	curve := historyCurve{}
	var current []POINT
	var lastSeg uint32
	haveSeg := false
	for x, b := range buckets {
		if !b.have || b.count == 0 {
			continue
		}
		var avg, minV, maxV float32
		switch kind {
		case 1:
			avg = float32(b.sSum / float64(b.count))
			minV, maxV = b.sMin, b.sMax
		case 2:
			avg = float32(b.iSum / float64(b.count))
			minV, maxV = b.iMin, b.iMax
		default:
			avg = float32(b.mSum / float64(b.count))
			minV, maxV = b.mMin, b.mMax
		}
		if !haveSeg || b.segment != lastSeg {
			if len(current) > 1 {
				curve.segments = append(curve.segments, current)
			}
			current = make([]POINT, 0, 64)
			lastSeg = b.segment
			haveSeg = true
		}
		px := plot.Left + int32(x)
		current = append(current, POINT{px, loudnessYRange(plot, avg, minDB, maxDB)})
		curve.ranges = append(curve.ranges, historyRange{px, loudnessYRange(plot, maxV, minDB, maxDB), loudnessYRange(plot, minV, minDB, maxDB)})
	}
	if len(current) > 1 {
		curve.segments = append(curve.segments, current)
	}
	return curve
}

func drawHistoryCurve(dc uintptr, curve historyCurve, col uint32, width int32) {
	for _, seg := range curve.segments {
		if len(seg) > 1 {
			polyline(dc, seg, col, width)
		}
	}
}

func drawHistoryCurveCrisp(dc uintptr, curve historyCurve, col uint32, width int32) {
	outlineWidth := width + 2
	for _, seg := range curve.segments {
		if len(seg) > 1 {
			polyline(dc, seg, cHistoryOutline, outlineWidth)
			polyline(dc, seg, col, width)
		}
	}
}

func drawHistoryRanges(dc uintptr, ranges []historyRange, col uint32) {
	if len(ranges) == 0 {
		return
	}
	pts := make([]POINT, 0, len(ranges)*2)
	counts := make([]uint32, 0, len(ranges))
	for _, rr := range ranges {
		if rr.maxY == rr.minY {
			continue
		}
		pts = append(pts, POINT{rr.x, rr.maxY}, POINT{rr.x, rr.minY})
		counts = append(counts, 2)
	}
	if len(counts) == 0 {
		return
	}
	pen := newPen(col, 1)
	old := selectObject(dc, pen)
	procPolyPolyline.Call(dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(unsafe.Pointer(&counts[0])), uintptr(len(counts)))
	selectObject(dc, old)
	deleteObject(pen)
}

func drawHistoryCurrentLabels(dc uintptr, plot RECT, m, s, i, minDB, maxDB float32, font uintptr) {
	type item struct {
		label string
		value float32
		col   uint32
	}
	items := []item{{"M", m, cText}, {"S", s, cReady}, {"I", i, cWarn}}
	type placed struct {
		y  int32
		it item
	}
	p := make([]placed, 0, 3)
	for _, it := range items {
		v := clampLoudness(it.value)
		if v < minDB || v > maxDB {
			continue
		}
		p = append(p, placed{loudnessYRange(plot, v, minDB, maxDB), it})
	}
	sort.Slice(p, func(a, b int) bool { return p[a].y < p[b].y })
	for n := 1; n < len(p); n++ {
		if p[n].y-p[n-1].y < 15 {
			p[n].y = p[n-1].y + 15
		}
	}
	for n := len(p) - 1; n >= 0; n-- {
		if p[n].y > plot.Bottom-8 {
			p[n].y = plot.Bottom - 8
		}
		if n > 0 && p[n].y-p[n-1].y < 15 {
			p[n-1].y = p[n].y - 15
		}
	}
	for _, v := range p {
		box := makeRect(plot.Right-72, v.y-7, 68, 14)
		fillRound(dc, box, 4, cSurfaceRaised)
		strokeRound(dc, box, 4, cBorder, 1)
		text(dc, fmt.Sprintf("%s %.1f", v.it.label, clampLoudness(v.it.value)), box, font, v.it.col, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func (a *App) drawLoudnessHover(dc uintptr, plot RECT, start, end float64, data historyRenderData) {
	if !pointInRect(a.hover.X, a.hover.Y, plot) || len(data.buckets) == 0 || end <= start {
		return
	}
	x := a.hover.X
	if x < plot.Left {
		x = plot.Left
	}
	if x >= plot.Right {
		x = plot.Right - 1
	}
	idx := int(x - plot.Left)
	best := -1
	for radius := 0; radius <= 10 && best < 0; radius++ {
		for _, cand := range []int{idx - radius, idx + radius} {
			if cand >= 0 && cand < len(data.buckets) && data.buckets[cand].have && data.buckets[cand].count > 0 {
				best = cand
				break
			}
		}
	}
	if best < 0 {
		return
	}
	b := data.buckets[best]
	cursorX := plot.Left + int32(best)
	line(dc, cursorX, plot.Top, cursorX, plot.Bottom, cAccent2, 1)
	m := float32(b.mSum / float64(b.count))
	s := float32(b.sSum / float64(b.count))
	i := float32(b.iSum / float64(b.count))
	label := fmt.Sprintf("%s   M %.1f   S %.1f   I %.1f LUFS", formatHistoryTimePrecise(b.time), m, s, i)
	boxW := int32(238)
	box := makeRect(cursorX+8, plot.Top+8, boxW, 22)
	if box.Right > plot.Right-4 {
		box.Left = cursorX - boxW - 8
		box.Right = box.Left + boxW
	}
	fillRound(dc, box, 5, cSurfaceRaised)
	strokeRound(dc, box, 5, cAccent2, 1)
	text(dc, label, makeRect(box.Left+7, box.Top, rectW(box)-14, rectH(box)), a.fontTiny, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func formatHistoryTimePrecise(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	m := int(sec) / 60
	s := sec - float64(m*60)
	if m >= 60 {
		h := m / 60
		m %= 60
		return fmt.Sprintf("%d:%02d:%04.1f", h, m, s)
	}
	return fmt.Sprintf("%d:%04.1f", m, s)
}

func clampLoudness(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < -60 {
		return -60
	}
	if v > 0 {
		return 0
	}
	return v
}

func (a *App) drawWavePanel(dc uintptr) {
	r := a.layout.Wave
	fill(dc, r, cPanel)
	text(dc, "WAVEFORM", makeRect(r.Left+14, r.Top+3, 92, 26), a.fontSmall, cAccent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	live := a.engine.InputActive()
	if live {
		text(dc, "LIVE · 6 SEC", makeRect(r.Right-112, r.Top+3, 98, 26), a.fontTiny, cAccent2, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
	line(dc, r.Left+12, r.Top+30, r.Right-12, r.Top+30, cBorder, 1)
	plot := a.wavePlotRect()
	fill(dc, plot, cSurfaceDeep)
	strokeRound(dc, plot, 4, cBorder, 1)

	var pts []WavePoint
	var channels int
	loading := false
	progress := 0.0
	cacheHit := false
	if live {
		pts = a.liveWaveform
		channels = a.liveWaveChannels
	} else {
		a.waveMu.RLock()
		pts = a.waveform
		channels = a.waveChannels
		loading = a.waveLoading
		progress = a.waveProgress
		cacheHit = a.waveCacheHit
		a.waveMu.RUnlock()
	}

	// Dedicated left gutter holds channel badges and dB scale; waveform itself
	// spans fully to the right without the old P/R readout boxes.
	lanes := waveformLanes(plot, channels)
	for _, lane := range lanes {
		// Keep only the center silence reference line. L/R channel badges were
		// intentionally removed from the UI; the stereo waveform lanes remain.
		midY := lane.Top + rectH(lane)/2
		line(dc, lane.Left, midY, lane.Right, midY, cClipGuide, 1)
		text(dc, "-∞", makeRect(r.Left+4, midY-7, 32, 14), a.fontTiny, cFaint, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}

	if len(pts) == 0 {
		if loading {
			text(dc, fmt.Sprintf("Generating waveform… %d%%", int(progress*100)), plot, a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
		return
	}

	startRatio, endRatio := 0.0, 1.0
	if !live {
		startRatio, endRatio = a.waveView()
	}
	span := endRatio - startRatio
	dur := float64(liveWaveformSeconds)
	posRatio := 1.0
	if !live {
		dur = a.engine.DurationSeconds()
		posRatio = 0
		if dur > 0 {
			posRatio = a.engine.PositionSeconds() / dur
		}
	}
	if posRatio < 0 {
		posRatio = 0
	}
	if posRatio > 1 {
		posRatio = 1
	}

	// A screen pixel often represents many cached waveform bins. Sampling only
	// one bin per pixel can completely miss short drum transients. Aggregate
	// the full source range covered by each pixel so min/max peaks are preserved.
	waveRangeAt := func(local0, local1 float64) WavePoint {
		if local0 > local1 {
			local0, local1 = local1, local0
		}
		r0 := startRatio + local0*span
		r1 := startRatio + local1*span
		if r0 < 0 {
			r0 = 0
		}
		if r1 > 1 {
			r1 = 1
		}
		i0 := int(math.Floor(r0 * float64(len(pts)-1)))
		i1 := int(math.Ceil(r1 * float64(len(pts)-1)))
		if i0 < 0 {
			i0 = 0
		}
		if i1 < i0 {
			i1 = i0
		}
		if i1 >= len(pts) {
			i1 = len(pts) - 1
		}
		out := WavePoint{MinL: 1, MaxL: -1, MinR: 1, MaxR: -1}
		for i := i0; i <= i1; i++ {
			p := pts[i]
			if p.MinL < out.MinL {
				out.MinL = p.MinL
			}
			if p.MaxL > out.MaxL {
				out.MaxL = p.MaxL
			}
			if p.MinR < out.MinR {
				out.MinR = p.MinR
			}
			if p.MaxR > out.MaxR {
				out.MaxR = p.MaxR
			}
			for band := range out.Bands {
				out.Bands[band] += p.Bands[band]
			}
		}
		count := float32(i1 - i0 + 1)
		for band := range out.Bands {
			out.Bands[band] /= count
		}
		if out.MinL > out.MaxL {
			out.MinL, out.MaxL = 0, 0
		}
		if out.MinR > out.MaxR {
			out.MinR, out.MaxR = out.MinL, out.MaxL
		}
		return out
	}

	visibleStart := int(startRatio * float64(len(pts)-1))
	visibleEnd := int(endRatio * float64(len(pts)-1))
	if visibleStart < 0 {
		visibleStart = 0
	}
	if visibleEnd >= len(pts) {
		visibleEnd = len(pts) - 1
	}

	// Gentle display-only auto gain makes quieter recordings easier to read
	// without changing the underlying audio or the dB values shown on hover.
	// It is intentionally capped at +12 dB so noise is never blown up wildly.
	globalPeak := float32(0)
	for i := range pts {
		p := pts[i]
		for _, v := range []float32{p.MinL, p.MaxL, p.MinR, p.MaxR} {
			av := v
			if av < 0 {
				av = -av
			}
			if av > globalPeak {
				globalPeak = av
			}
		}
	}
	displayGain := float32(1)
	if globalPeak > 0.0001 && globalPeak < 0.82 {
		displayGain = 0.82 / globalPeak
		if displayGain > 4 {
			displayGain = 4
		}
	}

	clipVisible := 0
	for i := visibleStart; i <= visibleEnd; i++ {
		p := pts[i]
		if nearFullScale(p.MinL, p.MaxL) || nearFullScale(p.MinR, p.MaxR) {
			clipVisible++
		}
	}

	grayBrush := cachedBrush(cWaveInactive)
	redBrush := cachedBrush(cDanger)
	for li, lane := range lanes {
		height := float32(rectH(lane)-8) / 2
		mid := lane.Top + rectH(lane)/2
		laneW := maxi32(rectW(lane), 1)
		for x := lane.Left; x < lane.Right; x++ {
			px := x - lane.Left
			local0 := float64(px) / float64(laneW)
			local1 := float64(px+1) / float64(laneW)
			p := waveRangeAt(local0, local1)
			mn, mx := p.MinL, p.MaxL
			if li == 1 {
				mn, mx = p.MinR, p.MaxR
			}
			rawMin, rawMax := mn, mx
			mn *= displayGain
			mx *= displayGain
			if mn < -1 {
				mn = -1
			}
			if mx > 1 {
				mx = 1
			}
			globalRatio := startRatio + ((local0+local1)*0.5)*span
			brush := a.waveformColorBrush(p.Bands, globalRatio > posRatio)
			if brush == 0 {
				brush = grayBrush
			}
			if nearFullScale(rawMin, rawMax) {
				brush = redBrush
			}
			y1 := mid - int32(mx*height)
			y2 := mid - int32(mn*height)
			if y1 < lane.Top+2 {
				y1 = lane.Top + 2
			}
			if y2 > lane.Bottom-2 {
				y2 = lane.Bottom - 2
			}
			if y2 <= y1 {
				y2 = y1 + 1
				if y2 > lane.Bottom-2 {
					y1 = y2 - 1
				}
			}
			// One FillRect per column replaces the old
			// SelectObject+MoveTo+LineTo triple and needs no DC pen state.
			// FillRect is right/bottom-exclusive, matching LineTo's endpoint
			// behavior, so the rendered pixels are identical.
			colRect := RECT{Left: x, Top: y1, Right: x + 1, Bottom: y2}
			procFillRect.Call(dc, uintptr(unsafe.Pointer(&colRect)), brush)
		}
	}

	// Explicit clip markers across the top edge of the waveform view.
	clipW := maxi32(rectW(plot)-4, 1)
	for x := plot.Left + 2; x < plot.Right-2; x++ {
		px := x - (plot.Left + 2)
		local0 := float64(px) / float64(clipW)
		local1 := float64(px+1) / float64(clipW)
		p := waveRangeAt(local0, local1)
		if nearFullScale(p.MinL, p.MaxL) || nearFullScale(p.MinR, p.MaxR) {
			line(dc, x, plot.Top+1, x, plot.Top+6, cDanger, 1)
		}
	}

	if !live && posRatio >= startRatio && posRatio <= endRatio && span > 0 {
		playX := plot.Left + int32(((posRatio-startRatio)/span)*float64(rectW(plot)))
		line(dc, playX-1, plot.Top, playX-1, plot.Bottom, cAccent2, 1)
		line(dc, playX, plot.Top, playX, plot.Bottom, cText, 1)
		fillRound(dc, makeRect(playX-3, plot.Top-1, 6, 6), 3, cText)
	}

	for i := 0; i <= 8; i++ {
		x := plot.Left + int32(i)*rectW(plot)/8
		label := "NOW"
		if live {
			remaining := dur * float64(8-i) / 8
			if i < 8 {
				label = fmt.Sprintf("-%.1fs", remaining)
			}
		} else {
			ratio := startRatio + span*float64(i)/8
			label = formatTime(dur * ratio)
		}
		text(dc, label, makeRect(x-24, plot.Bottom+4, 48, 16), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}

	if live {
		text(dc, "ROLLING", makeRect(plot.Left+8, plot.Bottom-20, 72, 16), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	} else if a.waveZoom > 1 {
		text(dc, "SHIFT + WHEEL  PAN", makeRect(plot.Left+8, plot.Bottom-20, 132, 16), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	} else {
		text(dc, "WHEEL  ZOOM", makeRect(plot.Left+8, plot.Bottom-20, 100, 16), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
	if cacheHit {
		text(dc, "CACHED", makeRect(plot.Right-66, plot.Bottom-20, 58, 16), a.fontTiny, cFaint, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
	if clipVisible > 0 {
		box := makeRect(plot.Right-144, plot.Bottom-42, 136, 18)
		fillRound(dc, box, 4, cSurfaceRaised)
		strokeRound(dc, box, 4, cDanger, 1)
		text(dc, fmt.Sprintf("NEAR FS BINS %d", clipVisible), box, a.fontTiny, cDanger, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	if pointInRect(a.hover.X, a.hover.Y, plot) {
		hx := a.hover.X
		if hx < plot.Left {
			hx = plot.Left
		}
		if hx > plot.Right {
			hx = plot.Right
		}
		plotW := maxi32(rectW(plot), 1)
		local := float64(hx-plot.Left) / float64(plotW)
		local1 := float64(hx-plot.Left+1) / float64(plotW)
		p := waveRangeAt(local, local1)
		ratio := startRatio + local*span
		lAmp := maxf(float32(math.Abs(float64(p.MinL))), float32(math.Abs(float64(p.MaxL))))
		rAmp := maxf(float32(math.Abs(float64(p.MinR))), float32(math.Abs(float64(p.MaxR))))
		line(dc, hx, plot.Top+1, hx, plot.Bottom-1, cMuted, 1)
		label := fmt.Sprintf("%s   L %s dB   R %s dB", formatTime(dur*ratio), dbText(ampToDB(lAmp)), dbText(ampToDB(rAmp)))
		boxW := int32(218)
		if channels == 1 {
			label = fmt.Sprintf("%s   M %s dB", formatTime(dur*ratio), dbText(ampToDB(lAmp)))
			boxW = 150
		}
		boxX := hx + 10
		if boxX+boxW > plot.Right-5 {
			boxX = hx - boxW - 10
		}
		if boxX < plot.Left+5 {
			boxX = plot.Left + 5
		}
		box := makeRect(boxX, plot.Top+38, boxW, 22)
		fillRound(dc, box, 5, cSurfaceRaised)
		strokeRound(dc, box, 5, cAccent2, 1)
		text(dc, label, makeRect(box.Left+7, box.Top, rectW(box)-14, rectH(box)), a.fontTiny, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

// Spectrum panel geometry cache. The grid x positions, dB row y positions,
// per-band curve x coordinates, and per-band frequency values depend only on
// the plot rectangle and the band count, none of which change frame to frame
// unless the window is resized or the BAND mode is switched. Computing them
// once and reusing them removes all per-frame log10/pow calls from the panel.
type spectrumGridCache struct {
	valid  bool
	left   int32
	width  int32
	top    int32
	height int32
	bands  int
	tonalX [6][2]int32
	freqX  [10]int32
	dbY    [10]int32
	curveX [384]int32
	freqHz [384]float64
}

var spectrumTonalBands = [6]struct {
	f0, f1 float64
	label  string
}{
	{20, 60, "SUB"}, {60, 250, "LOW"},
	{250, 500, "LOW-MID"}, {500, 2000, "MID"},
	{2000, 6000, "PRESENCE"}, {6000, 20000, "AIR"},
}

var spectrumFreqLabels = [10]float64{20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000}

// Smoothing kernel weights 1/(d+1); spectrumSmoothRadius is at most 4.
var spectrumSmoothWeights [8]float64

func init() {
	for i := range spectrumSmoothWeights {
		spectrumSmoothWeights[i] = 1.0 / float64(i+1)
	}
}

func (g *spectrumGridCache) ensure(inner RECT, bands int) {
	w := rectW(inner)
	h := rectH(inner)
	if g.valid && g.left == inner.Left && g.width == w && g.top == inner.Top && g.height == h && g.bands == bands {
		return
	}
	*g = spectrumGridCache{valid: true, left: inner.Left, width: w, top: inner.Top, height: h, bands: bands}
	for bi, b := range spectrumTonalBands {
		g.tonalX[bi][0] = inner.Left + int32((math.Log10(b.f0/20)/3)*float64(w-1))
		g.tonalX[bi][1] = inner.Left + int32((math.Log10(b.f1/20)/3)*float64(w-1))
	}
	for i, f := range spectrumFreqLabels {
		g.freqX[i] = inner.Left + int32((math.Log10(f/20)/3)*float64(w-1))
	}
	for i := 0; i < 10; i++ {
		g.dbY[i] = inner.Top + int32(float64(i)/9*float64(h))
	}
	denom := float64(maxi(bands-1, 1))
	for i := 0; i < bands && i < len(g.curveX); i++ {
		t := float64(i) / denom
		g.curveX[i] = inner.Left + int32(t*float64(w-1))
		g.freqHz[i] = 20 * math.Pow(10, 3*t)
	}
}

func (a *App) drawSpectrumPanel(dc uintptr) {
	r := a.layout.Spectrum
	fill(dc, r, cPanel)
	drawAnalyzerControl(dc, a.layout.AnalyzerViewBtn, a.analyzerViewLabel(), a.fontTiny)
	line(dc, r.Left+12, r.Top+30, r.Right-12, r.Top+30, cBorder, 1)
	drawAnalyzerControl(dc, a.layout.SpectrumFFTBtn, a.spectrumFFTLabel(), a.fontTiny)
	drawAnalyzerControl(dc, a.layout.SpectrumSmoothBtn, a.spectrumSmoothLabel(), a.fontTiny)
	drawAnalyzerControl(dc, a.layout.SpectrumTiltBtn, a.spectrumTiltLabel(), a.fontTiny)
	drawAnalyzerControl(dc, a.layout.SpectrumBandBtn, a.spectrumBandLabel(), a.fontTiny)
	drawAnalyzerToggle(dc, a.layout.SpectrumHoldBtn, "HOLD", a.fontTiny, a.spectrumMode == spectrumModeHold)
	drawAnalyzerToggle(dc, a.layout.SpectrumFreezeBtn, "FREEZE", a.fontTiny, a.spectrumMode == spectrumModeFreeze)

	plot := RECT{r.Left + 42, r.Top + 34, r.Right - 12, r.Bottom - 24}
	fill(dc, plot, cSurfaceDeep)
	strokeRound(dc, plot, 4, cBorder, 1)
	inner := inflateRect(plot, -4, -4)

	bands := a.spectrumBandCount()
	grid := &a.spectrumGrid
	grid.ensure(inner, bands)

	// Tonal-band boundaries are the primary vertical reference lines.
	for bi, b := range spectrumTonalBands {
		x0 := grid.tonalX[bi][0]
		x1 := grid.tonalX[bi][1]
		if bi > 0 {
			line(dc, x0, inner.Top, x0, inner.Bottom, cGrid, 1)
		}
		text(dc, b.label, makeRect(x0+4, inner.Top+4, maxi32(x1-x0-8, 20), 12), a.fontTiny, cFaint, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	// Uniform 10 dB horizontal grid from -90 dB to 0 dB. No special red -60 line.
	for i := 0; i < 10; i++ {
		db := -i * 10
		y := grid.dbY[i]
		col := cGrid
		if i == 0 || i == 9 {
			col = cGridStrong
		}
		line(dc, inner.Left, y, inner.Right, y, col, 1)
		text(dc, fmt.Sprintf("%d", db), makeRect(r.Left+6, y-7, 30, 14), a.fontTiny, cFaint, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}

	for i, f := range spectrumFreqLabels {
		text(dc, formatFreqCompact(f), makeRect(grid.freqX[i]-20, plot.Bottom+4, 40, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}

	if bands <= 0 {
		return
	}
	var smooth, holdSmooth [384]float64
	radius := a.spectrumSmoothRadius()
	for i := 0; i < bands; i++ {
		sum, holdSum, weight := 0.0, 0.0, 0.0
		for k := -radius; k <= radius; k++ {
			j := i + k
			if j < 0 || j >= bands {
				continue
			}
			w := spectrumSmoothWeights[absInt(k)]
			sum += float64(a.spectrum[j]) * w
			holdSum += float64(a.spectrumHold[j]) * w
			weight += w
		}
		if weight > 0 {
			smooth[i] = sum / weight
			holdSmooth[i] = holdSum / weight
		}
		if smooth[i] < 0 {
			smooth[i] = 0
		}
		if smooth[i] > 1 {
			smooth[i] = 1
		}
		if holdSmooth[i] < smooth[i] {
			holdSmooth[i] = smooth[i]
		}
		if holdSmooth[i] > 1 {
			holdSmooth[i] = 1
		}
	}

	var curve, hold [384]POINT
	var polyPts [386]POINT
	polyPts[0] = POINT{inner.Left, inner.Bottom}
	peakIdx := 0
	holdPeakIdx := 0
	for i := 0; i < bands; i++ {
		v := smooth[i]
		hv := holdSmooth[i]
		if v > smooth[peakIdx] {
			peakIdx = i
		}
		if hv > holdSmooth[holdPeakIdx] {
			holdPeakIdx = i
		}
		x := grid.curveX[i]
		curve[i] = POINT{x, inner.Bottom - int32(v*float64(rectH(inner)-4))}
		hold[i] = POINT{x, inner.Bottom - int32(hv*float64(rectH(inner)-4))}
		polyPts[i+1] = curve[i]
	}
	polyPts[bands+1] = POINT{inner.Right, inner.Bottom}

	// Real-time spectrum: visually translucent blue fill + thin blue outline.
	poly(dc, polyPts[:bands+2], cSpecFill)
	polyline(dc, curve[:bands], cSpecLine, 1)
	// Peak Hold: neutral gray/white 1 px contour.
	polyline(dc, hold[:bands], cSpecTrace, 1)

	peakFreq := grid.freqHz[holdPeakIdx]
	peakDB := holdSmooth[holdPeakIdx]*90 - 90
	if holdSmooth[holdPeakIdx] > 0.02 {
		peakText := "PEAK  " + formatFreqCompact(peakFreq) + "  " + fmt.Sprintf("%.1f dB", peakDB)
		// Size the badge to its actual text instead of reserving a fixed wide box.
		// Keep a small amount of horizontal breathing room so the border feels
		// compact without crowding the value.
		boxW := measureTextWidth(dc, peakText, a.fontTiny) + 16
		if boxW < 92 {
			boxW = 92
		}
		if boxW > 148 {
			boxW = 148
		}
		box := makeRect(inner.Right-boxW-8, inner.Top+5, boxW, 20)
		fillRound(dc, box, 4, cSurfaceRaised)
		strokeRound(dc, box, 4, cBorder, 1)
		text(dc, peakText, makeRect(box.Left+8, box.Top, rectW(box)-16, rectH(box)), a.fontTiny, cSpecTrace, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	// Frequency markers are detected from the Peak Hold contour, not the live trace.
	type peakMark struct {
		idx int
		mag float64
	}
	var markStore [384]peakMark
	markCount := 0
	for i := 2; i < bands-2; i++ {
		v := holdSmooth[i]
		if v < .25 {
			continue
		}
		if v >= holdSmooth[i-1] && v >= holdSmooth[i+1] && v >= holdSmooth[i-2] && v >= holdSmooth[i+2] {
			// Require a small local prominence so dense high-band modes do not label noise.
			shoulder := math.Max(math.Max(holdSmooth[i-2], holdSmooth[i+2]), math.Max(holdSmooth[i-1], holdSmooth[i+1]))
			if v-shoulder < 0.003 {
				continue
			}
			markStore[markCount] = peakMark{i, v}
			markCount++
		}
	}
	marks := markStore[:markCount]
	sort.Slice(marks, func(i, j int) bool { return marks[i].mag > marks[j].mag })
	if len(marks) > 7 {
		marks = marks[:7]
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].idx < marks[j].idx })
	lastLabelX := int32(-1000)
	for _, m := range marks {
		pt := hold[m.idx]
		if pt.X-lastLabelX < 46 {
			continue
		}
		fillRound(dc, makeRect(pt.X-3, pt.Y-3, 6, 6), 3, cAccent2)
		freq := grid.freqHz[m.idx]
		text(dc, formatFreqCompact(freq), makeRect(pt.X-24, pt.Y-20, 48, 14), a.fontTiny, cText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		lastLabelX = pt.X
	}

	// Hover inspector: logarithmic frequency + display dB at the live spectrum.
	if pointInRect(a.hover.X, a.hover.Y, inner) {
		hx := a.hover.X
		if hx < inner.Left {
			hx = inner.Left
		}
		if hx >= inner.Right {
			hx = inner.Right - 1
		}
		xnorm := float64(hx-inner.Left) / float64(maxi32(rectW(inner)-1, 1))
		idx := int(math.Round(xnorm * float64(bands-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= bands {
			idx = bands - 1
		}
		freq := grid.freqHz[idx]
		db := smooth[idx]*90 - 90
		y := curve[idx].Y
		line(dc, hx, inner.Top, hx, inner.Bottom, cAccent2, 1)
		line(dc, inner.Left, y, inner.Right, y, cGridStrong, 1)
		fillRound(dc, makeRect(hx-3, y-3, 6, 6), 3, cAccent2)
		label := fmt.Sprintf("%s   %.1f dB", formatFreqCompact(freq), db)
		boxW := int32(116)
		box := makeRect(hx+8, inner.Top+32, boxW, 22)
		if box.Right > inner.Right-4 {
			box.Left = hx - boxW - 8
			box.Right = box.Left + boxW
		}
		fillRound(dc, box, 5, cSurfaceRaised)
		strokeRound(dc, box, 5, cAccent2, 1)
		text(dc, label, makeRect(box.Left+7, box.Top, rectW(box)-14, rectH(box)), a.fontTiny, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func analyzerPlotRect(r RECT) RECT {
	return RECT{r.Left + 42, r.Top + 34, r.Right - 12, r.Bottom - 24}
}

func (a *App) drawSpectrogramPanel(dc uintptr) {
	r := a.layout.Spectrum
	fill(dc, r, cPanel)
	drawAnalyzerControl(dc, a.layout.AnalyzerViewBtn, a.analyzerViewLabel(), a.fontTiny)
	line(dc, r.Left+12, r.Top+30, r.Right-12, r.Top+30, cBorder, 1)
	if a.analyzerView == analyzerViewLiveSpectrogram {
		drawAnalyzerControl(dc, a.layout.SpectrumFFTBtn, a.spectrumFFTLabel(), a.fontTiny)
		drawAnalyzerControl(dc, a.layout.SpectrogramScaleBtn, a.spectrogramScaleLabel(), a.fontTiny)
		drawAnalyzerControl(dc, a.layout.SpectrogramTimeBtn, a.spectrogramTimeLabel(), a.fontTiny)
		drawAnalyzerControl(dc, a.layout.SpectrogramRangeBtn, a.spectrogramRangeLabel(), a.fontTiny)
		drawAnalyzerToggle(dc, a.layout.SpectrogramFreezeBtn, "FREEZE", a.fontTiny, a.liveSpectrogram.frozen)
	} else {
		drawAnalyzerControl(dc, a.layout.SpectrogramScaleBtn, a.spectrogramScaleLabel(), a.fontTiny)
		drawAnalyzerControl(dc, a.layout.SpectrogramRangeBtn, a.spectrogramRangeLabel(), a.fontTiny)
	}

	plot := analyzerPlotRect(r)
	fill(dc, plot, cSurfaceDeep)
	inner := inflateRect(plot, -4, -4)
	rate, _ := a.engine.AnalysisInfo()
	if a.analyzerView == analyzerViewTrackSpectrogram {
		a.spectrogramMu.RLock()
		if a.trackSpectrogram.Rate != 0 {
			rate = a.trackSpectrogram.Rate
		}
		a.spectrogramMu.RUnlock()
	}
	if rate == 0 {
		rate = 48000
	}
	maxFreq := math.Min(20000, float64(rate)*0.5)
	minViewFreq, maxViewFreq := a.spectrogramFrequencyView(maxFreq)
	physicalW := int(scaleDPI(rectW(inner), a.currentDPI()))
	physicalH := int(scaleDPI(rectH(inner), a.currentDPI()))
	if a.spectrogramSurface.ensure(physicalW, physicalH, a.themeMode) {
		if a.analyzerView == analyzerViewLiveSpectrogram {
			a.spectrogramSurface.renderLive(&a.liveSpectrogram, a.spectrogramTimeSeconds(), a.spectrogramRange, a.spectrogramScale, minViewFreq, maxViewFreq, maxFreq)
		} else {
			a.spectrogramMu.RLock()
			data := a.trackSpectrogram
			a.spectrogramMu.RUnlock()
			start, end := a.waveView()
			a.spectrogramSurface.renderTrack(data, atomic.LoadUint64(&a.spectrogramToken), start, end, a.spectrogramRange, a.spectrogramScale, minViewFreq, maxViewFreq, maxFreq)
		}
		a.spectrogramSurface.draw(dc, inner)
	}
	strokeRound(dc, plot, 4, cBorder, 1)

	labelMask := spectrogramFrequencyLabelMask(a.spectrogramScale, minViewFreq, maxViewFreq, rectH(inner))
	for i, tick := range spectrogramFrequencyTicks {
		if tick.frequency < minViewFreq || tick.frequency > maxViewFreq {
			continue
		}
		y := inner.Top + spectrogramFrequencyTickOffset(tick.frequency, a.spectrogramScale, minViewFreq, maxViewFreq, rectH(inner))
		if tick.major {
			line(dc, inner.Left, y, inner.Right, y, cGridStrong, 1)
		} else if labelMask[i] {
			line(dc, inner.Left-5, y, inner.Left+4, y, cGridStrong, 1)
		}
		if labelMask[i] {
			labelY := y
			if labelY < inner.Top+7 {
				labelY = inner.Top + 7
			} else if labelY > inner.Bottom-7 {
				labelY = inner.Bottom - 7
			}
			text(dc, formatFreqCompact(tick.frequency), makeRect(r.Left+2, labelY-7, 36, 14), a.fontTiny, cFaint, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
		}
	}

	if a.analyzerView == analyzerViewLiveSpectrogram {
		seconds := a.spectrogramTimeSeconds()
		text(dc, fmt.Sprintf("-%.0fs", seconds), makeRect(inner.Left-4, plot.Bottom+4, 40, 14), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		text(dc, fmt.Sprintf("-%.0fs", seconds/2), makeRect(inner.Left+rectW(inner)/2-20, plot.Bottom+4, 40, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "NOW", makeRect(inner.Right-38, plot.Bottom+4, 38, 14), a.fontTiny, cMuted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	} else {
		start, end := a.waveView()
		duration := a.engine.DurationSeconds()
		text(dc, formatTime(start*duration), makeRect(inner.Left-4, plot.Bottom+4, 64, 14), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		text(dc, formatTime((start+end)*0.5*duration), makeRect(inner.Left+rectW(inner)/2-32, plot.Bottom+4, 64, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, formatTime(end*duration), makeRect(inner.Right-64, plot.Bottom+4, 64, 14), a.fontTiny, cMuted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)

		a.spectrogramMu.RLock()
		loading, progress, specErr := a.spectrogramLoading, a.spectrogramProgress, a.spectrogramError
		ready := len(a.trackSpectrogram.Values) > 0
		a.spectrogramMu.RUnlock()
		if loading {
			label := fmt.Sprintf("ANALYZING %d%%", int(progress*100+0.5))
			text(dc, label, inner, a.fontSmall, cText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		} else if !ready && specErr != "" {
			text(dc, "SPECTROGRAM UNAVAILABLE", inner, a.fontSmall, cWarn, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}

		if ready && duration > 0 {
			position := a.engine.PositionSeconds() / duration
			if position >= start && position <= end {
				x := inner.Left + int32((position-start)/(end-start)*float64(rectW(inner)))
				line(dc, x, inner.Top, x, inner.Bottom, cAccent2, 1)
			}
		}
	}

	if pointInRect(a.hover.X, a.hover.Y, inner) {
		hx, hy := a.hover.X, a.hover.Y
		xnorm := float64(hx-inner.Left) / float64(maxi32(rectW(inner), 1))
		ynorm := 1 - float64(hy-inner.Top)/float64(maxi32(rectH(inner), 1))
		freq := spectrogramFrequencyAtNormalized(ynorm, a.spectrogramScale, minViewFreq, maxViewFreq)
		band := int(math.Round(spectrogramLinearBandPosition(freq, maxFreq, spectrogramBandCount)))
		if band < 0 {
			band = 0
		} else if band >= spectrogramBandCount {
			band = spectrogramBandCount - 1
		}
		value := uint8(0)
		timeLabel := ""
		if a.analyzerView == analyzerViewLiveSpectrogram {
			seconds := a.spectrogramTimeSeconds()
			start, count, _, last := a.liveSpectrogram.visibleRange(seconds)
			target := last - seconds + xnorm*seconds
			value = a.liveSpectrogram.valueAtTime(target, start, count, band)
			timeLabel = fmt.Sprintf("%.1fs", -seconds+xnorm*seconds)
		} else {
			a.spectrogramMu.RLock()
			data := a.trackSpectrogram
			a.spectrogramMu.RUnlock()
			start, end := a.waveView()
			ratio := start + xnorm*(end-start)
			column := int(ratio * float64(data.Columns))
			if column >= 0 && column < data.Columns && band < data.Bands && len(data.Values) >= data.Columns*data.Bands {
				value = data.Values[column*data.Bands+band]
			}
			timeLabel = formatTime(ratio * a.engine.DurationSeconds())
		}
		line(dc, hx, inner.Top, hx, inner.Bottom, cAccent2, 1)
		line(dc, inner.Left, hy, inner.Right, hy, cGridStrong, 1)
		label := fmt.Sprintf("%s   %s  %s   %.1f dBFS", timeLabel, formatFreqCompact(freq), formatPitch(freq), dequantizeSpectrogramDB(value))
		boxW := measureTextWidth(dc, label, a.fontTiny) + 16
		maxBoxW := rectW(inner) - 8
		if boxW > maxBoxW {
			boxW = maxBoxW
		}
		box := makeRect(hx+8, inner.Top+8, boxW, 22)
		if box.Right > inner.Right-4 {
			box.Left, box.Right = hx-boxW-8, hx-8
		}
		if box.Left < inner.Left+4 {
			box.Left = inner.Left + 4
			box.Right = box.Left + int32(boxW)
		}
		fillRound(dc, box, 5, cSurfaceRaised)
		strokeRound(dc, box, 5, cAccent2, 1)
		text(dc, label, makeRect(box.Left+7, box.Top, rectW(box)-14, rectH(box)), a.fontTiny, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func (a *App) drawMeterPanel(dc uintptr) {
	r := a.layout.Meter
	fill(dc, r, cPanel)
	line(dc, r.Left, r.Bottom-1, r.Right, r.Bottom-1, cBorder, 1)
	text(dc, "METERS", makeRect(r.Left+14, r.Top+6, 120, 24), a.fontSmall, cAccent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	if a.engine.InputActive() {
		live := makeRect(r.Left+86, r.Top+9, 62, 18)
		fillRound(dc, live, 5, cBlueDark)
		strokeRound(dc, live, 5, cAccent2, 1)
		text(dc, "LIVE INPUT", live, a.fontTiny, cAccent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawLabeledButton(dc, a.uiImages, a.layout.ResetBtn, "reset_btn", "RESET", a.fontTiny)
	line(dc, r.Left+12, r.Top+34, r.Right-12, r.Top+34, cBorder, 1)

	peak := maxf(ampToDB(a.peakL), ampToDB(a.peakR))
	truePeak := a.truePeak
	lufsI, lufsS, lufsM, lra := a.lufsI, a.lufsS, a.lufsM, a.lra
	if a.meterDisplayInit {
		peak = a.meterDisplayPeak
		truePeak = a.meterDisplayTruePeak
		lufsI, lufsS, lufsM, lra = a.meterDisplayI, a.meterDisplayS, a.meterDisplayM, a.meterDisplayLRA
	}
	items := []struct {
		label, value string
		col          uint32
	}{
		{"PEAK", dbText(peak) + " dB", metricColor(peak, 0)},
		{"TRUE PEAK", dbText(truePeak) + " dB", metricColor(truePeak, -0.1)},
		{"INTEGRATED LUFS", dbText(lufsI) + " LUFS", cWarn},
		{"SHORT TERM", dbText(lufsS) + " LUFS", cReady},
		{"MOMENTARY", dbText(lufsM) + " LUFS", cAccent2},
		{"LRA", fmt.Sprintf("%.1f LU", lra), cAccent2},
	}
	gap := int32(8)
	left := r.Left + 12
	top := r.Top + 42
	cardW := (rectW(r) - 24 - gap) / 2
	clipH := int32(42)
	cardH := (rectH(r) - 42 - clipH - 14 - 3*gap) / 3
	if cardH < 56 {
		cardH = 56
	}
	valueFont := a.fontSmall
	if cardH >= 78 {
		valueFont = a.fontSection
	}
	for i, it := range items {
		col := i % 2
		row := i / 2
		cr := makeRect(left+int32(col)*(cardW+gap), top+int32(row)*(cardH+gap), cardW, cardH)
		drawMetricCell(dc, cr, it.label, it.value, it.col, a.fontTiny, valueFont)
	}
	clipTop := top + 3*(cardH+gap)
	clipR := makeRect(left, clipTop, rectW(r)-24, clipH)
	fillRound(dc, clipR, 5, cSurfaceRaised)
	strokeRound(dc, clipR, 5, cBorder, 1)
	line(dc, clipR.Left+1, clipR.Top+1, clipR.Right-1, clipR.Top+1, cAccent2, 2)
	clipCol := cMuted
	if a.clips > 0 {
		clipCol = cDanger
	}
	text(dc, "SAMPLE OVERS", makeRect(clipR.Left+10, clipR.Top, rectW(clipR)*3/5-10, rectH(clipR)), a.fontTiny, clipCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	text(dc, fmt.Sprintf("%d", a.clips), makeRect(clipR.Left+rectW(clipR)*3/5, clipR.Top, rectW(clipR)*2/5-10, rectH(clipR)), a.fontSection, cText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func (a *App) drawStereoPanel(dc uintptr) {
	r := a.layout.Stereo
	fill(dc, r, cPanel)
	titleRight := a.layout.StereoNormalizeBtn.Left - 6
	titleLabel := "STEREO FIELD"
	if titleRight-(r.Left+14) < 70 {
		titleLabel = "FIELD"
	}
	text(dc, titleLabel, RECT{r.Left + 14, r.Top + 6, titleRight, r.Top + 30}, a.fontSmall, cAccent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	normalizeLabel := "NORMALIZE"
	if rectW(a.layout.StereoNormalizeBtn) < 60 {
		normalizeLabel = "NORM"
	}
	drawAnalyzerToggle(dc, a.layout.StereoNormalizeBtn, normalizeLabel, a.fontTiny, a.stereoNormalize)
	drawAnalyzerControl(dc, a.layout.StereoStyleBtn, a.stereoStyleLabel(), a.fontTiny)
	drawAnalyzerControl(dc, a.layout.StereoModeBtn, a.stereoModeLabel(), a.fontTiny)
	corr := a.correlationDisplay
	statusText, statusCol := stereoStatus(corr)
	line(dc, r.Left+12, r.Top+34, r.Right-12, r.Top+34, cBorder, 1)
	corrBar := makeRect(r.Left+18, r.Top+52, rectW(r)-36, 10)
	mid := corrBar.Left + rectW(corrBar)/2
	// Two-tone correlation scale: negative correlation on the left,
	// positive correlation on the right. Clip a single rounded bar into
	// two halves so the outer ends stay cleanly rounded in both themes.
	savedCorr, _, _ := procSaveDC.Call(dc)
	procIntersectClipRect.Call(dc, uintptr(corrBar.Left), uintptr(corrBar.Top), uintptr(mid), uintptr(corrBar.Bottom))
	fillRound(dc, corrBar, 5, cCorrelationNeg)
	if savedCorr != 0 {
		procRestoreDC.Call(dc, savedCorr)
	}
	savedCorr, _, _ = procSaveDC.Call(dc)
	procIntersectClipRect.Call(dc, uintptr(mid), uintptr(corrBar.Top), uintptr(corrBar.Right), uintptr(corrBar.Bottom))
	fillRound(dc, corrBar, 5, cCorrelationPos)
	if savedCorr != 0 {
		procRestoreDC.Call(dc, savedCorr)
	}
	strokeRound(dc, corrBar, 5, cBorder, 1)
	line(dc, mid, corrBar.Top+1, mid, corrBar.Bottom-1, cBorder, 1)
	if corr < -1 {
		corr = -1
	}
	if corr > 1 {
		corr = 1
	}
	kx := corrBar.Left + int32((corr+1)*.5*float32(rectW(corrBar)))
	// Correlation is a read-only meter. Keep its indicator circular and
	// visually stable on hover so it is not mistaken for an interactive knob.
	drawUIAsset(dc, a.uiImages, "correlation_thumb", makeRect(kx-14, corrBar.Top-9, 28, 28))
	text(dc, "-1", makeRect(corrBar.Left, corrBar.Top-18, 24, 14), a.fontTiny, cCorrelationNeg, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	text(dc, "CORRELATION", makeRect(mid-48, corrBar.Top-18, 96, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(dc, "+1", makeRect(corrBar.Right-24, corrBar.Top-18, 24, 14), a.fontTiny, cCorrelationPos, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	// The Normalize toggle occupies the former header-status space at normal
	// panel widths. Keep the phase assessment visible in the gap below the bar.
	text(dc, statusText, RECT{corrBar.Left, corrBar.Bottom + 1, corrBar.Right, corrBar.Bottom + 15}, a.fontTiny, statusCol, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)

	plot := RECT{r.Left + 18, corrBar.Bottom + 16, r.Right - 18, r.Bottom - 12}
	fillRound(dc, plot, 6, cSurfaceDeep)
	strokeRound(dc, plot, 6, cBorder, 1)
	plot = inflateRect(plot, -5, -5)
	cx, cy, rad, valid := stereoPlotGeometry(plot)
	if !valid {
		return
	}
	switch a.stereoMode {
	case 1:
		drawLissajousGrid(dc, cx, cy, rad)
	case 2:
		drawPolarGrid(dc, cx, cy, rad)
	default:
		drawGonioGrid(dc, cx, cy, rad)
	}
	pts := a.stereoPoints[:a.stereoPointCount]
	if a.stereoMode == 2 && a.stereoStyle == 0 {
		if len(pts) > 0 || stereoLineFadeRunning(a.renderTickSerial.Load(), a.stereoLineFadeUntil) {
			a.drawPolarStereoVisualization(dc, pts, cx, cy, rad)
		}
	} else {
		a.drawStereoVisualization(dc, pts, a.stereoMode, a.stereoStyle, cx, cy, rad)
	}
	fillRound(dc, makeRect(cx-2, cy-2, 4, 4), 2, cAccent2)
	switch a.stereoMode {
	case 1:
		d := int32(float64(rad) * 0.70710678)
		text(dc, "L", makeRect(cx-d-18, cy-d-12, 18, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "R", makeRect(cx+d, cy-d-12, 18, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	case 2:
		text(dc, "MONO", makeRect(cx-22, cy-rad-16, 44, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "L", makeRect(cx-rad-12, cy-8, 16, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "R", makeRect(cx+rad-4, cy-8, 16, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	default:
		text(dc, "M", makeRect(cx-10, cy-rad-16, 20, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "S", makeRect(cx-10, cy+rad+2, 20, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "L", makeRect(cx-rad-16, cy-7, 16, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		text(dc, "R", makeRect(cx+rad, cy-7, 16, 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func stereoPlotGeometry(plot RECT) (cx, cy, rad int32, valid bool) {
	cx = (plot.Left + plot.Right) / 2
	cy = (plot.Top + plot.Bottom) / 2
	rad = int32(math.Min(float64(rectW(plot)), float64(rectH(plot)))/2) - 10
	return cx, cy, rad, rad >= 10
}

func stereoLineGeometry(panel RECT, dpi uint32) (destination RECT, width, height int, valid bool) {
	corrBar := makeRect(panel.Left+18, panel.Top+52, rectW(panel)-36, 10)
	plot := RECT{panel.Left + 18, corrBar.Bottom + 16, panel.Right - 18, panel.Bottom - 12}
	plot = inflateRect(plot, -5, -5)
	cx, cy, rad, valid := stereoPlotGeometry(plot)
	if !valid {
		return RECT{}, 0, 0, false
	}
	destination = RECT{Left: cx - rad, Top: cy - rad, Right: cx + rad + 1, Bottom: cy + rad + 1}
	width = int(scaleDPI(rectW(destination), dpi))
	height = int(scaleDPI(rectH(destination), dpi))
	return destination, width, height, width > 0 && height > 0
}

func drawGonioGrid(dc uintptr, cx, cy, rad int32) {
	// Mastering-style reference grid: a clean outer circle, a restrained inner
	// guide, a strong mono/side axis and the two diagonal L/R axes. This keeps the
	// field readable without the dense twelve-spoke radar look of the older scope.
	strokeEllipse(dc, RECT{cx - rad, cy - rad, cx + rad, cy + rad}, cGridStrong, 1)
	inner := rad * 58 / 100
	strokeEllipse(dc, RECT{cx - inner, cy - inner, cx + inner, cy + inner}, cGrid, 1)
	line(dc, cx, cy-rad, cx, cy+rad, cGridStrong, 1)
	d := int32(float64(rad) * 0.70710678)
	line(dc, cx-d, cy-d, cx+d, cy+d, cGridStrong, 1)
	line(dc, cx-d, cy+d, cx+d, cy-d, cGridStrong, 1)
	line(dc, cx-rad, cy, cx+rad, cy, cGrid, 1)
}

func drawLissajousGrid(dc uintptr, cx, cy, rad int32) {
	// Rotate the former square scope by 45 degrees. The resulting diamond
	// matches the rotated L/R trace and gives mono material a clean vertical axis.
	outer := []POINT{{X: cx, Y: cy - rad}, {X: cx + rad, Y: cy}, {X: cx, Y: cy + rad}, {X: cx - rad, Y: cy}, {X: cx, Y: cy - rad}}
	innerR := rad / 2
	inner := []POINT{{X: cx, Y: cy - innerR}, {X: cx + innerR, Y: cy}, {X: cx, Y: cy + innerR}, {X: cx - innerR, Y: cy}, {X: cx, Y: cy - innerR}}
	polyline(dc, outer, cGridStrong, 1)
	polyline(dc, inner, cGrid, 1)
	line(dc, cx, cy-rad, cx, cy+rad, cGridStrong, 1)
	line(dc, cx-rad, cy, cx+rad, cy, cGrid, 1)
	// Two secondary diagonals keep the phase relationship readable without the
	// boxed look of the previous Lissajous grid.
	d := int32(float64(rad) * 0.70710678)
	line(dc, cx-d, cy-d, cx+d, cy+d, cGrid, 1)
	line(dc, cx-d, cy+d, cx+d, cy-d, cGrid, 1)
}

func drawPolarGrid(dc uintptr, cx, cy, rad int32) {
	for _, rr := range []int32{rad / 3, rad * 2 / 3, rad} {
		drawArcStroke(dc, float64(cx), float64(cy), float64(rr), math.Pi, 2*math.Pi, cGridStrong, 1, 40)
	}
	line(dc, cx-rad, cy, cx+rad, cy, cGridStrong, 1)
	for _, deg := range []float64{210, 240, 270, 300, 330} {
		a := deg * math.Pi / 180
		x := cx + int32(math.Cos(a)*float64(rad))
		y := cy + int32(math.Sin(a)*float64(rad))
		line(dc, cx, cy, x, y, cGrid, 1)
	}
}

func (a *App) drawStereoVisualization(dc uintptr, pts []StereoPoint, mode, style int, cx, cy, rad int32) {
	dpi := a.currentDPI()
	destination := RECT{Left: cx - rad, Top: cy - rad, Right: cx + rad + 1, Bottom: cy + rad + 1}
	width := int(scaleDPI(rectW(destination), dpi))
	height := int(scaleDPI(rectH(destination), dpi))
	if a.stereoPhosphor.surfaceMatches(width, height) {
		a.stereoPhosphor.draw(dc, destination)
	}
}

func (a *App) drawPolarStereoVisualization(dc uintptr, pts []StereoPoint, cx, cy, rad int32) {
	a.drawPolarStereoTrace(dc, pts, cx, cy, rad)
}

func (a *App) drawPolarStereoTrace(dc uintptr, pts []StereoPoint, cx, cy, rad int32) {
	const bins = 128
	var env [bins]float32
	for i := range env {
		env[i] = 0
	}
	start := len(pts) - 900
	if start < 0 {
		start = 0
	}
	for i := start; i < len(pts); i++ {
		x, y := stereoPointForMode(pts[i], 2, cx, cy, rad)
		dx := float64(x - cx)
		dy := float64(y - cy)
		angle := math.Atan2(dy, dx)
		if angle < 0 {
			angle += 2 * math.Pi
		}
		if angle < math.Pi || angle > 2*math.Pi {
			continue
		}
		t := (angle - math.Pi) / math.Pi
		idx := int(t * bins)
		if idx < 0 {
			idx = 0
		}
		if idx >= bins {
			idx = bins - 1
		}
		rn := float32(math.Sqrt(dx*dx+dy*dy) / float64(rad))
		if rn > 1 {
			rn = 1
		}
		// Recent samples get slightly more weight to keep the display lively.
		age := float32(i-start) / float32(maxi(len(pts)-start, 1))
		rn *= 0.82 + 0.18*age
		if rn > env[idx] {
			env[idx] = rn
		}
	}
	// Smooth the envelope so the lobe looks fluid instead of spiky.
	for pass := 0; pass < 2; pass++ {
		var smooth [bins]float32
		for i := 0; i < bins; i++ {
			prev := env[maxi(i-1, 0)]
			next := env[mini(i+1, bins-1)]
			smooth[i] = prev*0.25 + env[i]*0.5 + next*0.25
		}
		env = smooth
	}
	for i := 0; i < bins; i++ {
		target := env[i]
		if target > a.polarEnvelope[i] {
			a.polarEnvelope[i] = a.polarEnvelope[i]*0.58 + target*0.42
		} else {
			a.polarEnvelope[i] = a.polarEnvelope[i]*0.86 + target*0.14
		}
		env[i] = a.polarEnvelope[i]
	}
	var polyPts [bins + 2]POINT
	polyPts[0] = POINT{X: cx, Y: cy}
	for i := 0; i < bins; i++ {
		ang := math.Pi + (float64(i)/float64(bins-1))*math.Pi
		rr := float64(rad) * 0.92 * float64(env[i])
		polyPts[i+1] = POINT{
			X: cx + int32(math.Cos(ang)*rr),
			Y: cy + int32(math.Sin(ang)*rr),
		}
	}
	polyPts[bins+1] = POINT{X: cx, Y: cy}
	poly(dc, polyPts[:], cStereoFill)
	polylineRound(dc, polyPts[1:bins+1], cStereoLine, 2)
	polyline(dc, polyPts[1:bins+1], cStereoCore, 1)

	// Overlay a recent raw trace to match a mastering-style polar scope.
	recentStart := len(pts) - 240
	if recentStart < 0 {
		recentStart = 0
	}
	var raw [240]POINT
	rawN := 0
	for i := recentStart; i < len(pts) && rawN < len(raw); i++ {
		x, y := stereoPointForMode(pts[i], 2, cx, cy, rad)
		raw[rawN] = POINT{X: x, Y: y}
		rawN++
	}
	if rawN >= 2 {
		polyline(dc, raw[:rawN], cStereoTrail, 1)
	}
}

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const stereoDisplayGain float32 = 1.35
const stereoPolarDisplayGain float32 = 2.70

func stereoDisplayPoint(x, y float32, cx, cy, rad int32) (int32, int32) {
	// The raw Mid/Side sample cloud is mathematically correct but visually very
	// conservative at normal programme levels. Apply a display-only gain so the
	// field uses more of the available scope area, while preserving direction
	// and aspect ratio. A radial limiter prevents the trace from drawing outside
	// the outer reference circle on near/full-scale transients.
	scale := float32(rad) * 0.82 * stereoDisplayGain
	dx := x * scale
	dy := y * scale
	limit := float32(rad) * 0.94
	mag := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if mag > limit && mag > 0 {
		k := limit / mag
		dx *= k
		dy *= k
	}
	return cx + int32(dx), cy - int32(dy)
}

func stereoPointForMode(p StereoPoint, mode int, cx, cy, rad int32) (int32, int32) {
	switch mode {
	case 1:
		l := (p.Y + p.X) * 0.70710678
		r := (p.Y - p.X) * 0.70710678
		// Rotate the Lissajous presentation by 45 degrees so the former square
		// becomes a diamond and in-phase mono energy rises vertically. Mirror the
		// horizontal display axis so a left-only signal is drawn on the left and a
		// right-only signal is drawn on the right. The L/R labels themselves stay
		// fixed; only the animated trace direction is corrected here.
		rx := (r - l) * 0.70710678
		ry := (l + r) * 0.70710678
		return stereoDisplayPoint(rx, ry, cx, cy, rad)
	case 2:
		l := (p.Y + p.X) * 0.70710678
		r := (p.Y - p.X) * 0.70710678
		den := float32(math.Abs(float64(l)) + math.Abs(float64(r)) + 0.000001)
		pan := (r - l) / den
		level := float32(math.Sqrt(float64((l*l+r*r)*0.5))) * stereoPolarDisplayGain
		if level > 1 {
			level = 1
		}
		ang := -math.Pi/2 + float64(pan)*(math.Pi/2)
		radius := float64(level * float32(rad) * 0.92)
		return cx + int32(math.Cos(ang)*radius), cy + int32(math.Sin(ang)*radius)
	default:
		// Raw goniometer X is (L-R)/sqrt(2), which points right for a left-only
		// signal in screen coordinates. Mirror X for conventional L-left/R-right
		// presentation without changing the underlying analysis data.
		return stereoDisplayPoint(-p.X, p.Y, cx, cy, rad)
	}
}

func (a *App) drawLevelMeterPanel(dc uintptr) {
	r := a.layout.VU
	fill(dc, r, cPanel)
	line(dc, r.Left, r.Top, r.Left, r.Bottom, cBorder, 1)
	line(dc, r.Left, r.Bottom-1, r.Right, r.Bottom-1, cBorder, 1)

	header := makeRect(r.Left+8, r.Top+6, rectW(r)-16, 24)
	fillRound(dc, header, 6, cSurfaceRaised)
	strokeRound(dc, header, 6, cBorder, 1)
	text(dc, "PEAK / RMS", header, a.fontTiny, cAccent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	readTop := header.Bottom + 8
	readGap := int32(4)
	readW := (rectW(r) - 16 - readGap) / 2
	leftRead := makeRect(r.Left+8, readTop, readW, 40)
	rightRead := makeRect(leftRead.Right+readGap, readTop, readW, 40)
	drawLevelReadout(dc, leftRead, "L", a.peakL, a.rmsL, a.vuHoldL)
	drawLevelReadout(dc, rightRead, "R", a.peakR, a.rmsR, a.vuHoldR)

	plot := RECT{r.Left + 8, rightRead.Bottom + 8, r.Right - 8, r.Bottom - 10}
	barW := int32(18)
	scaleW := int32(28)
	gap := int32(5)
	leftBar := RECT{plot.Left, plot.Top + 16, plot.Left + barW, plot.Bottom - 20}
	scale := RECT{leftBar.Right + gap, leftBar.Top, leftBar.Right + gap + scaleW, leftBar.Bottom}
	rightBar := RECT{scale.Right + gap, leftBar.Top, scale.Right + gap + barW, leftBar.Bottom}
	if rightBar.Right > plot.Right {
		over := rightBar.Right - plot.Right
		leftBar.Left -= over / 2
		leftBar.Right -= over / 2
		scale.Left -= over / 2
		scale.Right -= over / 2
		rightBar.Left -= over / 2
		rightBar.Right -= over / 2
	}
	text(dc, "L", makeRect(leftBar.Left, plot.Top, rectW(leftBar), 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(dc, "R", makeRect(rightBar.Left, plot.Top, rectW(rightBar), 14), a.fontTiny, cMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawLevelLedBar(dc, leftBar, a.peakL, a.rmsL, a.vuHoldL)
	drawLevelScale(dc, scale)
	drawLevelLedBar(dc, rightBar, a.peakR, a.rmsR, a.vuHoldR)
	text(dc, "dBFS", makeRect(scale.Left-4, scale.Bottom+3, rectW(scale)+8, 14), a.fontTiny, cFaint, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawLevelReadout(dc uintptr, r RECT, channel string, peak, rms, hold float32) {
	fillRound(dc, r, 6, cSurfaceRaised)
	strokeRound(dc, r, 6, cBorder, 1)
	peakDB := ampToDB(maxf(peak, hold))
	rmsDB := ampToDB(rms)
	peakCol := cAccent2
	if peakDB >= -1 {
		peakCol = cDanger
	} else if peakDB >= -6 {
		peakCol = cWarn
	}
	text(dc, channel, makeRect(r.Left+5, r.Top+2, 12, 14), theApp.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	text(dc, "P "+dbText(peakDB), makeRect(r.Left+17, r.Top+2, rectW(r)-21, 14), theApp.fontTiny, peakCol, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	text(dc, "R "+dbText(rmsDB), makeRect(r.Left+5, r.Top+18, rectW(r)-10, 17), theApp.fontTiny, cText, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	if peak >= 1 {
		fillRound(dc, makeRect(r.Right-10, r.Top+5, 5, 5), 3, cDanger)
	}
}

func drawLevelLedBar(dc uintptr, r RECT, peak, rms, hold float32) {
	fillRound(dc, r, 5, cSurfaceDeep)
	strokeRound(dc, r, 5, cBorder, 1)
	inner := inflateRect(r, -4, -4)
	segments := 32
	gap := int32(2)
	segH := (rectH(inner) - int32(segments-1)*gap) / int32(segments)
	if segH < 3 {
		segH = 3
	}
	// The illuminated bar follows true digital peak in dBFS. RMS is drawn as
	// a separate marker, and peak hold is a white outline. This avoids the
	// previous arbitrary VU-like gain compensation.
	active := int(levelDBNorm(ampToDB(peak))*float32(segments) + 0.999)
	if active > segments {
		active = segments
	}
	rmsIdx := int(levelDBNorm(ampToDB(rms))*float32(segments) + 0.5)
	if rmsIdx < 0 {
		rmsIdx = 0
	}
	if rmsIdx >= segments {
		rmsIdx = segments - 1
	}
	holdIdx := int(levelDBNorm(ampToDB(hold))*float32(segments) + 0.5)
	if holdIdx < 0 {
		holdIdx = 0
	}
	if holdIdx >= segments {
		holdIdx = segments - 1
	}
	for i := 0; i < segments; i++ {
		idxFromBottom := segments - 1 - i
		top := inner.Top + int32(i)*(segH+gap)
		seg := RECT{inner.Left, top, inner.Right, top + segH}
		segDB := -60 + (float32(idxFromBottom)+0.5)*63/float32(segments)
		onCol := cVUGreenOn
		offCol := cVUGreenOff
		if segDB >= -12 {
			onCol = cWarn
			offCol = cVUYellowOff
		}
		if segDB >= -3 {
			onCol = cDanger
			offCol = cVURedOff
		}
		fillCol := offCol
		if idxFromBottom < active {
			fillCol = onCol
		}
		fillRound(dc, seg, 2, fillCol)
		if idxFromBottom < active {
			line(dc, seg.Left+2, seg.Top+1, seg.Right-2, seg.Top+1, cVUHighlight, 1)
		}
		if idxFromBottom == rmsIdx && rms > 0 {
			line(dc, seg.Left+2, seg.Bottom-1, seg.Right-2, seg.Bottom-1, cAccent2, 1)
		}
		if idxFromBottom == holdIdx && hold > 0 {
			strokeRound(dc, inflateRect(seg, 1, 1), 2, cText, 1)
		}
	}
}

func drawLevelScale(dc uintptr, r RECT) {
	for _, db := range []float32{3, 0, -3, -6, -12, -18, -24, -36, -48, -60} {
		y := r.Bottom - int32(levelDBNorm(db)*float32(rectH(r)))
		line(dc, r.Left, y, r.Left+5, y, cGridStrong, 1)
		line(dc, r.Right-5, y, r.Right, y, cGridStrong, 1)
		label := fmt.Sprintf("%.0f", db)
		if db > 0 {
			label = "+" + label
		}
		text(dc, label, makeRect(r.Left+3, y-7, rectW(r)-6, 14), theApp.fontTiny, cFaint, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func levelDBNorm(db float32) float32 {
	if db <= -60 {
		return 0
	}
	if db >= 3 {
		return 1
	}
	return (db + 60) / 63
}

func (a *App) drawControls(dc uintptr) {
	r := a.layout.Controls
	fill(dc, r, cHeader)
	line(dc, r.Left, r.Top, r.Right, r.Top, cBorder, 1)

	thumb := makeRect(18, r.Top+35, 48, 48)
	fillRound(dc, thumb, 4, cBlueDark)
	a.drawMiniWave(dc, thumb)
	title := "No audio open"
	meta := "—"
	if p := a.playbackPlaylist(); p != nil && a.current >= 0 && a.current < len(p.Tracks) {
		t := p.Tracks[a.current]
		title = filepath.Base(t.Path)
		rate, ch := a.engine.StreamInfo()
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(t.Path), "."))
		meta = fmt.Sprintf("%s  •  %.1f kHz  •  %d ch", ext, float64(rate)/1000, ch)
	}
	infoRight := a.layout.Playlist.Right + 65
	if infoRight < 320 {
		infoRight = 320
	}
	// Keep the now-playing text clear of the transport cluster when the window
	// becomes narrow and the transport group has shifted left.
	if transportLimit := a.layout.PrevBtn.Left - 14; transportLimit > 170 && infoRight > transportLimit {
		infoRight = transportLimit
	}
	text(dc, title, makeRect(80, r.Top+33, infoRight-92, 22), a.fontSmall, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(dc, meta, makeRect(80, r.Top+56, infoRight-92, 18), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	transportIcon(dc, a.layout.PrevBtn, "prev", false)
	transportIcon(dc, a.layout.PlayBtn, func() string {
		if a.engine.IsPlaying() {
			return "pause"
		}
		return "play"
	}(), true)
	transportIcon(dc, a.layout.NextBtn, "next", false)
	transportIcon(dc, a.layout.StopBtn, "stop", false)
	transportIcon(dc, a.layout.LoopBtn, "loop", a.loop)
	transportPlayModeIcon(dc, a.layout.PlayModeBtn, a.continuousPlay)

	p := a.layout.Progress
	pos := a.engine.PositionSeconds()
	dur := a.engine.DurationSeconds()
	ratio := 0.0
	if dur > 0 {
		ratio = pos / dur
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	drawUIAsset(dc, a.uiImages, "progress_track_bg", p)
	played := p
	played.Right = p.Left + int32(float64(rectW(p))*ratio)
	if rectW(played) > 0 {
		drawUIAsset(dc, a.uiImages, "progress_track_fill", played)
	}
	progressAsset := "progress_thumb"
	if pointInRect(a.hover.X, a.hover.Y, inflateRect(p, 8, 8)) {
		progressAsset = "progress_thumb_hover"
	}
	drawUIAsset(dc, a.uiImages, progressAsset, makeRect(played.Right-9, p.Top-7, 18, 18))
	text(dc, formatTime(pos), makeRect(p.Left, p.Bottom+4, 42, 16), a.fontTiny, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	text(dc, formatTime(dur), makeRect(p.Right-42, p.Bottom+4, 42, 16), a.fontTiny, cMuted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)

	vr := a.layout.Volume
	vv := a.engine.Volume()
	if vv < 0 {
		vv = 0
	}
	if vv > 1 {
		vv = 1
	}
	speakerLevel := "high"
	if vv <= 0.001 {
		speakerLevel = "mute"
	} else if vv < 0.34 {
		speakerLevel = "low"
	} else if vv < 0.70 {
		speakerLevel = "mid"
	}
	speakerRect := a.layout.Speaker
	if pointInRect(a.hover.X, a.hover.Y, speakerRect) {
		speakerLevel += "_hover"
	}
	drawUIAsset(dc, a.uiImages, "speaker_"+speakerLevel, speakerRect)
	drawUIAsset(dc, a.uiImages, "volume_track_bg", vr)
	vf := vr
	vf.Right = vr.Left + int32(float32(rectW(vr))*vv)
	if rectW(vf) > 0 {
		drawUIAsset(dc, a.uiImages, "volume_track_fill", vf)
	}
	volumeAsset := "volume_thumb"
	if pointInRect(a.hover.X, a.hover.Y, inflateRect(vr, 8, 8)) {
		volumeAsset = "volume_thumb_hover"
	}
	thumbRect := makeRect(vf.Right-8, vr.Top-6, 16, 16)
	drawUIAsset(dc, a.uiImages, volumeAsset, thumbRect)
	if a.dragMode == dragVolumeMode || (!a.volumePopupUntil.IsZero() && time.Now().Before(a.volumePopupUntil)) {
		percent := int(math.Round(float64(vv * 100)))
		bubble := makeRect(thumbRect.Left-18, vr.Top-38, 52, 24)
		if bubble.Left < speakerRect.Right+4 {
			bubble.Left = speakerRect.Right + 4
			bubble.Right = bubble.Left + 52
		}
		fillRound(dc, bubble, 6, cSurfaceRaised)
		strokeRound(dc, bubble, 6, cBorder, 1)
		text(dc, fmt.Sprintf("%d%%", percent), bubble, a.fontTiny, cText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawRoutingDeviceGroup(
		dc, a.uiImages, a.layout.DeviceBtn, a.layout.InputBtn,
		a.deviceButtonLabel(), a.inputButtonLabel(), a.engine.InputActive(), a.fontTiny,
	)
}

func drawAnalyzerControl(dc uintptr, r RECT, label string, font uintptr) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	bg := cSurfaceRaised
	border := cBorder
	col := cMuted
	if hovered {
		bg = cPanel2
		border = cAccent2
		col = cText
	}
	fillRound(dc, r, 6, bg)
	strokeRound(dc, r, 6, border, 1)
	text(dc, label, r, font, col, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawAnalyzerToggle(dc uintptr, r RECT, label string, font uintptr, active bool) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	bg := cSurfaceRaised
	border := cBorder
	col := cMuted
	if active {
		bg = cBlueDark
		border = cAccent2
		col = cAccent2
	} else if hovered {
		bg = cPanel2
		border = cAccent2
		col = cText
	}
	fillRound(dc, r, 6, bg)
	strokeRound(dc, r, 6, border, 1)
	text(dc, label, r, font, col, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawMetricCell(dc uintptr, r RECT, label, value string, accent uint32, labelFont, valueFont uintptr) {
	fillRound(dc, r, 6, cSurfaceRaised)
	strokeRound(dc, r, 6, cBorder, 1)
	line(dc, r.Left+2, r.Top+1, r.Right-2, r.Top+1, accent, 2)
	labelH := int32(16)
	labelTop := r.Top + 8
	text(dc, label, makeRect(r.Left+9, labelTop, rectW(r)-18, labelH), labelFont, cMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	valueTop := labelTop + labelH + 3
	valueH := r.Bottom - valueTop - 8
	if valueH < 22 {
		valueH = 22
	}
	text(dc, value, makeRect(r.Left+9, valueTop, rectW(r)-18, valueH), valueFont, accent, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func stereoStatus(corr float32) (string, uint32) {
	switch {
	case corr < -0.2:
		return "PHASE RISK", cDanger
	case corr < 0.2:
		return "WIDE", cWarn
	case corr < 0.75:
		return "BALANCED", cAccent2
	default:
		return "MONO-LEAN", cMuted
	}
}

func (a *App) drawMiniWave(dc uintptr, r RECT) {
	a.waveMu.RLock()
	pts := a.waveform
	a.waveMu.RUnlock()
	if len(pts) == 0 {
		drawUIAsset(dc, a.uiImages, "miniwave_placeholder", r)
		return
	}
	mid := (r.Top + r.Bottom) / 2
	h := float32(rectH(r)-10) / 2
	for x := r.Left + 4; x < r.Right-4; x++ {
		idx := int(int64(x-r.Left-4) * int64(len(pts)) / int64(maxi32(rectW(r)-8, 1)))
		if idx >= len(pts) {
			idx = len(pts) - 1
		}
		p := pts[idx]
		line(dc, x, mid-int32(p.MaxL*h), x, mid-int32(p.MinL*h), cAccent2, 1)
	}
}

const (
	splitterHandleLength    int32 = 64
	splitterHandleThickness int32 = 11
)

func splitterHandleRect(splitter RECT, vertical bool) RECT {
	halfThickness := splitterHandleThickness / 2
	if vertical {
		axis := splitter.Left + rectW(splitter)/2
		length := splitterHandleLength
		if rectH(splitter) < length {
			length = rectH(splitter)
		}
		top := splitter.Top + (rectH(splitter)-length)/2
		return RECT{axis - halfThickness, top, axis + halfThickness + 1, top + length}
	}
	axis := splitter.Top + rectH(splitter)/2
	length := splitterHandleLength
	if rectW(splitter) < length {
		length = rectW(splitter)
	}
	left := splitter.Left + (rectW(splitter)-length)/2
	return RECT{left, axis - halfThickness, left + length, axis + halfThickness + 1}
}

func (a *App) drawSplitters(dc uintptr) {
	type splitInfo struct {
		r    RECT
		mode int
		hv   bool
	}
	items := []splitInfo{
		{a.layout.SplitPlaylist, dragSplitPlaylistMode, true},
		{a.layout.SplitRight, dragSplitRightMode, true},
		{a.layout.SplitPlaylistInfo, dragSplitPlaylistInfoMode, false},
		{a.layout.SplitLoudnessSpectrum, dragSplitLoudnessMode, false},
		{a.layout.SplitMeterStereo, dragSplitStereoMode, false},
	}
	for _, item := range items {
		hovered := pointInRect(a.hover.X, a.hover.Y, inflateRect(item.r, 4, 4))
		active := a.dragMode == item.mode
		lineCol := cSplitterLine
		if hovered {
			lineCol = cSplitterHover
		}
		if active {
			lineCol = cAccent2
		}
		if item.hv {
			x := item.r.Left + rectW(item.r)/2
			line(dc, x, item.r.Top, x, item.r.Bottom, lineCol, 1)
			asset := "split_handle_v_normal"
			if hovered {
				asset = "split_handle_v_hover"
			}
			if active {
				asset = "split_handle_v_active"
			}
			drawUIAsset(dc, a.uiImages, asset, splitterHandleRect(item.r, true))
		} else {
			y := item.r.Top + rectH(item.r)/2
			line(dc, item.r.Left, y, item.r.Right, y, lineCol, 1)
			asset := "split_handle_h_normal"
			if hovered {
				asset = "split_handle_h_hover"
			}
			if active {
				asset = "split_handle_h_active"
			}
			drawUIAsset(dc, a.uiImages, asset, splitterHandleRect(item.r, false))
		}
	}
}

func drawArcStroke(dc uintptr, cx, cy, radius, start, end float64, color uint32, width int32, segments int) {
	if segments < 1 {
		segments = 1
	}
	prevX := int32(cx + math.Cos(start)*radius)
	prevY := int32(cy + math.Sin(start)*radius)
	for i := 1; i <= segments; i++ {
		t := float64(i) / float64(segments)
		a := start + (end-start)*t
		x := int32(cx + math.Cos(a)*radius)
		y := int32(cy + math.Sin(a)*radius)
		line(dc, prevX, prevY, x, y, color, width)
		prevX, prevY = x, y
	}
}

func metricColor(v, threshold float32) uint32 {
	if v >= threshold {
		return cDanger
	}
	if v > threshold-3 {
		return cWarn
	}
	return cAccent2
}
func meterNormVU(vu float32) float32 {
	if vu <= -20 {
		return 0
	}
	if vu >= 3 {
		return 1
	}
	if vu < -10 {
		return (vu + 20) / 10 * .25
	}
	if vu < -3 {
		return .25 + (vu+10)/7*.38
	}
	return .63 + (vu+3)/6*.37
}

func drawLabeledButton(dc uintptr, assets map[string]*UIBitmap, r RECT, baseName, label string, font uintptr) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	fillCol := cSurfaceAlt
	border := cBorder
	if hovered {
		fillCol = cSurfaceRaised
		border = cAccent2
	}
	fillRound(dc, r, 5, fillCol)
	strokeRound(dc, r, 5, border, 1)
	text(dc, label, r, font, cText, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawRoutingDeviceGroup(dc uintptr, assets map[string]*UIBitmap, outRect, inRect RECT, outLabel, inLabel string, inputActive bool, font uintptr) {
	// Input is intentionally displayed above output: signal source first, destination second.
	group := RECT{inRect.Left, inRect.Top, inRect.Right, outRect.Bottom}
	fillRound(dc, group, 7, cSurfaceAlt)
	strokeRound(dc, group, 7, cBorder, 1)
	line(dc, group.Left+8, outRect.Top, group.Right-8, outRect.Top, cBorder, 1)
	drawRoutingDeviceRow(dc, assets, inRect, "IN", inLabel, inputActive, font)
	drawRoutingDeviceRow(dc, assets, outRect, "OUT", outLabel, true, font)
}

func drawRoutingDeviceRow(dc uintptr, assets map[string]*UIBitmap, r RECT, role, label string, active bool, font uintptr) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	if hovered {
		fillRound(dc, RECT{r.Left + 1, r.Top + 1, r.Right - 1, r.Bottom - 1}, 6, cSurfaceRaised)
	}
	roleCol := cMuted
	if role == "OUT" || active {
		roleCol = cAccent2
	}
	if hovered {
		roleCol = cAccent2
	}
	roleRect := makeRect(r.Left+9, r.Top, 30, rectH(r))
	text(dc, role, roleRect, font, roleCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	line(dc, r.Left+42, r.Top+6, r.Left+42, r.Bottom-6, cBorder, 1)
	labelCol := cText
	if role == "IN" && !active {
		labelCol = cMuted
	}
	text(dc, label, makeRect(r.Left+50, r.Top, rectW(r)-72, rectH(r)), font, labelCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	chevron := "device_chevron"
	if hovered {
		chevron = "device_chevron_hover"
	}
	drawUIAsset(dc, assets, chevron, makeRect(r.Right-17, r.Top+(rectH(r)-6)/2, 10, 6))
}

func drawDeviceButton(dc uintptr, assets map[string]*UIBitmap, r RECT, label string, font uintptr) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	fillCol := cSurfaceAlt
	border := cBorder
	chevron := "device_chevron"
	if hovered {
		fillCol = cSurfaceRaised
		border = cAccent2
		chevron = "device_chevron_hover"
	}
	fillRound(dc, r, 6, fillCol)
	strokeRound(dc, r, 6, border, 1)
	text(dc, label, makeRect(r.Left+12, r.Top, rectW(r)-28, rectH(r)), font, cText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawUIAsset(dc, assets, chevron, makeRect(r.Right-16, r.Top+(rectH(r)-6)/2, 10, 6))
}

func transportPlayModeIcon(dc uintptr, r RECT, continuous bool) {
	kind := "single"
	if continuous {
		kind = "continuous"
	}
	state := "normal"
	if theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r) {
		state = "hover"
	}
	drawUIAsset(dc, theApp.uiImages, "playmode_"+kind+"_"+state, r)
}

func transportIcon(dc uintptr, r RECT, kind string, active bool) {
	hovered := theApp != nil && pointInRect(theApp.hover.X, theApp.hover.Y, r)
	state := "normal"
	if hovered {
		state = "hover"
	}
	asset := kind + "_" + state
	if kind == "loop" && active {
		if hovered {
			asset = "loop_on_hover"
		} else {
			asset = "loop_on"
		}
	}
	drawUIAsset(dc, theApp.uiImages, asset, r)
}

func fill(dc uintptr, r RECT, color uint32) {
	procFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), cachedBrush(color))
}
func fillRound(dc uintptr, r RECT, radius int32, color uint32) {
	b := cachedBrush(color)
	nullPen, _, _ := procGetStockObject.Call(8)
	oldB := selectObject(dc, b)
	oldP := selectObject(dc, nullPen)
	procRoundRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right+1), uintptr(r.Bottom+1), uintptr(radius*2), uintptr(radius*2))
	selectObject(dc, oldP)
	selectObject(dc, oldB)
}
func strokeRound(dc uintptr, r RECT, radius int32, color uint32, width int32) {
	p := cachedPen(color, width)
	nullBrush, _, _ := procGetStockObject.Call(5)
	oldP := selectObject(dc, p)
	oldB := selectObject(dc, nullBrush)
	procRoundRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius*2), uintptr(radius*2))
	selectObject(dc, oldB)
	selectObject(dc, oldP)
}
func strokeEllipse(dc uintptr, r RECT, color uint32, width int32) {
	p := cachedPen(color, width)
	nullBrush, _, _ := procGetStockObject.Call(5)
	oldP := selectObject(dc, p)
	oldB := selectObject(dc, nullBrush)
	procEllipse.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
	selectObject(dc, oldB)
	selectObject(dc, oldP)
}
func line(dc uintptr, x1, y1, x2, y2 int32, color uint32, width int32) {
	p := cachedPen(color, width)
	old := selectObject(dc, p)
	moveTo(dc, x1, y1)
	procLineTo.Call(dc, uintptr(x2), uintptr(y2))
	selectObject(dc, old)
}
func moveTo(dc uintptr, x, y int32) { procMoveToEx.Call(dc, uintptr(x), uintptr(y), 0) }
func poly(dc uintptr, pts []POINT, color uint32) {
	if len(pts) < 3 {
		return
	}
	b := cachedBrush(color)
	nullPen, _, _ := procGetStockObject.Call(8)
	ob := selectObject(dc, b)
	op := selectObject(dc, nullPen)
	procPolygon.Call(dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	selectObject(dc, op)
	selectObject(dc, ob)
}
func polyline(dc uintptr, pts []POINT, color uint32, width int32) {
	if len(pts) < 2 {
		return
	}
	p := cachedPen(color, width)
	old := selectObject(dc, p)
	procPolyline.Call(dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	selectObject(dc, old)
}
func polylineRound(dc uintptr, pts []POINT, color uint32, width int32) {
	if len(pts) < 2 {
		return
	}
	if width <= 1 {
		polyline(dc, pts, color, width)
		return
	}
	p := cachedRoundPen(color, width)
	if p == 0 {
		polyline(dc, pts, color, width)
		return
	}
	old := selectObject(dc, p)
	procPolyline.Call(dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	selectObject(dc, old)
}

func measureTextWidth(dc uintptr, s string, font uintptr) int32 {
	if s == "" {
		return 0
	}
	old := selectObject(dc, font)
	u := syscall.StringToUTF16(s)
	r := RECT{}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)), uintptr(DT_SINGLELINE|DT_CALCRECT|DT_NOPREFIX))
	selectObject(dc, old)
	return rectW(r)
}

func text(dc uintptr, s string, r RECT, font uintptr, color uint32, flags uint32) {
	if s == "" {
		return
	}
	old := selectObject(dc, font)
	procSetBkMode.Call(dc, TRANSPARENT)
	procSetTextColor.Call(dc, uintptr(color))
	u := syscall.StringToUTF16(s)
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)), uintptr(flags|DT_NOPREFIX))
	selectObject(dc, old)
}
func newBrush(c uint32) uintptr { r, _, _ := procCreateSolidBrush.Call(uintptr(c)); return r }
func newPen(c uint32, w int32) uintptr {
	r, _, _ := procCreatePen.Call(PS_SOLID, uintptr(w), uintptr(c))
	return r
}

// Cached GDI objects. The UI palette is a small finite set of colors and pen
// widths, so caching pens/brushes for the whole frame lifetime turns hundreds
// of Create*/Delete* calls per frame into map lookups. All drawing happens on
// the window thread, so no locking is needed. The caches are flushed on theme
// change and on window destroy (see destroyGDICache call sites in app.go).
var (
	gdiPenCache   = map[uint64]uintptr{}
	gdiBrushCache = map[uint32]uintptr{}
)

func gdiPenKey(color uint32, width int32, geometric bool) uint64 {
	key := uint64(color)<<8 | uint64(uint8(width))
	if geometric {
		key |= 1 << 62
	}
	return key
}

func cachedPen(color uint32, width int32) uintptr {
	key := gdiPenKey(color, width, false)
	if p := gdiPenCache[key]; p != 0 {
		return p
	}
	p := newPen(color, width)
	if p != 0 {
		gdiPenCache[key] = p
	}
	return p
}

func cachedRoundPen(color uint32, width int32) uintptr {
	key := gdiPenKey(color, width, true)
	if p := gdiPenCache[key]; p != 0 {
		return p
	}
	lb := LOGBRUSH{LbStyle: BS_SOLID, LbColor: color}
	style := uintptr(PS_GEOMETRIC | PS_SOLID | PS_ENDCAP_ROUND | PS_JOIN_ROUND)
	p, _, _ := procExtCreatePen.Call(style, uintptr(width), uintptr(unsafe.Pointer(&lb)), 0, 0)
	if p != 0 {
		gdiPenCache[key] = p
	}
	return p
}

func cachedBrush(color uint32) uintptr {
	if b := gdiBrushCache[color]; b != 0 {
		return b
	}
	b := newBrush(color)
	if b != 0 {
		gdiBrushCache[color] = b
	}
	return b
}

func destroyGDICache() {
	for _, p := range gdiPenCache {
		deleteObject(p)
	}
	gdiPenCache = map[uint64]uintptr{}
	for _, b := range gdiBrushCache {
		deleteObject(b)
	}
	gdiBrushCache = map[uint32]uintptr{}
}
func selectObject(dc, obj uintptr) uintptr { r, _, _ := procSelectObject.Call(dc, obj); return r }
func deleteObject(obj uintptr) {
	if obj != 0 {
		procDeleteObject.Call(obj)
	}
}

func waveformLanes(plot RECT, channels int) []RECT {
	inner := RECT{plot.Left + 3, plot.Top + 3, plot.Right - 3, plot.Bottom - 3}
	if channels == 1 {
		return []RECT{inner}
	}
	gap := int32(10)
	half := (rectH(plot) - gap) / 2
	return []RECT{
		{plot.Left + 3, plot.Top + 3, plot.Right - 3, plot.Top + half - 1},
		{plot.Left + 3, plot.Top + half + gap + 1, plot.Right - 3, plot.Bottom - 3},
	}
}

const waveformColorSteps = 12

func quantizeWaveformBands(bands [waveformBandCount]float32) ([waveformBandCount]uint8, bool) {
	var result [waveformBandCount]uint8
	var fractions [waveformBandCount]float32
	bands, ok := dominantWaveformBands(bands)
	if !ok {
		return result, false
	}
	var total float32
	for _, value := range bands {
		if value > 0 {
			total += value
		}
	}
	if total < 0.0001 {
		return result, false
	}
	used := 0
	for i, value := range bands {
		if value < 0 {
			value = 0
		}
		scaled := value / total * waveformColorSteps
		whole := int(scaled)
		result[i] = uint8(whole)
		fractions[i] = scaled - float32(whole)
		used += whole
	}
	for used < waveformColorSteps {
		best := 0
		for i := 1; i < waveformBandCount; i++ {
			if fractions[i] > fractions[best] {
				best = i
			}
		}
		result[best]++
		fractions[best] = -1
		used++
	}
	return result, true
}

func waveformPenKey(counts [waveformBandCount]uint8, inactive bool) uint32 {
	var key uint32
	for _, count := range counts {
		key = key*(waveformColorSteps+1) + uint32(count)
	}
	if inactive {
		key |= 1 << 31
	}
	return key
}

func mixedWaveformColor(counts [waveformBandCount]uint8, inactive bool) uint32 {
	bases := [waveformBandCount][3]float64{
		{24, 116, 255}, // sub: blue
		{125, 52, 255}, // low: purple
		{255, 55, 63},  // mid: red
		{255, 205, 35}, // presence: yellow
		{31, 218, 96},  // high: green
	}
	var linear [3]float64
	total := 0.0
	for band, count := range counts {
		weight := float64(count)
		total += weight
		for component := 0; component < 3; component++ {
			linear[component] += srgbToLinear(bases[band][component]/255) * weight
		}
	}
	if total == 0 {
		return cWaveInactive
	}
	var color [3]float64
	for component := range color {
		color[component] = linearToSRGB(linear[component]/total) * 255
	}
	if inactive {
		luma := color[0]*0.2126 + color[1]*0.7152 + color[2]*0.0722
		brightness := 0.62
		if theApp != nil && theApp.themeMode == 1 {
			brightness = 0.72
		}
		for component := range color {
			color[component] = (luma + (color[component]-luma)*0.48) * brightness
		}
	}
	return rgb(byte(clampf64(color[0], 0, 255)+0.5), byte(clampf64(color[1], 0, 255)+0.5), byte(clampf64(color[2], 0, 255)+0.5))
}

func (a *App) waveformColorBrush(bands [waveformBandCount]float32, inactive bool) uintptr {
	counts, ok := quantizeWaveformBands(bands)
	if !ok {
		return 0
	}
	key := waveformPenKey(counts, inactive)
	if brush := a.waveBrushes[key]; brush != 0 {
		return brush
	}
	if a.waveBrushes == nil {
		a.waveBrushes = make(map[uint32]uintptr)
	}
	brush := newBrush(mixedWaveformColor(counts, inactive))
	a.waveBrushes[key] = brush
	return brush
}

func (a *App) destroyWaveBrushes() {
	for _, brush := range a.waveBrushes {
		deleteObject(brush)
	}
	a.waveBrushes = nil
}

func srgbToLinear(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func linearToSRGB(value float64) float64 {
	if value <= 0.0031308 {
		return value * 12.92
	}
	return 1.055*math.Pow(value, 1/2.4) - 0.055
}

func clampf64(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func formatFreqCompact(freq float64) string {
	if freq >= 1000 {
		if math.Abs(math.Mod(freq, 1000)) < 1 {
			return fmt.Sprintf("%dk", int(freq/1000))
		}
		return fmt.Sprintf("%.1fk", freq/1000)
	}
	return fmt.Sprintf("%d", int(freq+0.5))
}
