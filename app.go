//go:build windows

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

type Track struct {
	Path    string
	Name    string
	Missing bool
}

const defaultPlaylistID = "default"

type Playlist struct {
	ID       string
	Name     string
	Tracks   []Track
	Selected int
	Scroll   int
}

type LoudnessSample struct {
	Time                             float64
	Momentary, ShortTerm, Integrated float32
	Segment                          uint32
}

type Layout struct {
	Client, Header, Content, Playlist, FileInfo, Center, Right, MetricStrip, Wave, Loudness, Spectrum, Meter, Stereo, VU, Controls                        RECT
	ThemeBtn                                                                                                                                              RECT
	Loudness5Btn, Loudness15Btn, Loudness30Btn, Loudness60Btn, LoudnessFullBtn, LoudnessTargetBtn                                                         RECT
	WaveZoomOutBtn, WaveFitBtn, WaveZoomInBtn                                                                                                             RECT
	AnalyzerViewBtn, SpectrumFFTBtn, SpectrumSmoothBtn, SpectrumTiltBtn, SpectrumBandBtn, SpectrumHoldBtn, SpectrumFreezeBtn                              RECT
	SpectrogramScaleBtn, SpectrogramTimeBtn, SpectrogramRangeBtn, SpectrogramFreezeBtn, StereoModeBtn, StereoStyleBtn, StereoNormalizeBtn, StereoColorBtn RECT
	PlaylistSelector, OpenBtn, ClearPlaylistBtn, PrevBtn, PlayBtn, StopBtn, NextBtn, LoopBtn, PlayModeBtn, ResetBtn, DeviceBtn, InputBtn                  RECT
	Progress, Volume, Speaker, FileInfoCopyBtn, FileInfoFolderBtn, FileInfoPath, PlaylistScrollTrack, PlaylistScrollThumb                                 RECT
	SplitPlaylist, SplitRight, SplitPlaylistInfo, SplitLoudnessSpectrum, SplitMeterStereo                                                                 RECT
}

const (
	dragSeekMode              = 1
	dragVolumeMode            = 2
	dragSplitPlaylistMode     = 3
	dragSplitRightMode        = 4
	dragSplitLoudnessMode     = 5
	dragSplitStereoMode       = 6
	dragSplitPlaylistInfoMode = 7
	dragPlaylistTrackMode     = 8
	dragPlaylistScrollMode    = 9
	dragSpectrogramFreqMode   = 10
)

const (
	dirtyMetric uint32 = 1 << iota
	dirtyWave
	dirtyLoudness
	dirtySpectrum
	dirtyMeter
	dirtyStereo
	dirtyLevel
	dirtyControls
	dirtyHeader
	dirtyPlaylist
	dirtySplitters
)

type spectrumDisplayMode uint8

const (
	spectrumModeNormal spectrumDisplayMode = iota
	spectrumModeHold
	spectrumModeFreeze
)

type analyzerDisplayView uint8

const (
	analyzerViewSpectrum analyzerDisplayView = iota
	analyzerViewLiveSpectrogram
	analyzerViewTrackSpectrogram
)

type App struct {
	help        helpController
	hwnd        uintptr
	engine      *AudioEngine
	engineReady atomic.Bool

	statusMu    sync.RWMutex
	status      string
	statusError bool

	playlists          []Playlist
	activePlaylistID   string
	playbackPlaylistID string
	playlistSel        playlistSelection
	current            int
	loop               bool
	continuousPlay     bool
	pendingOpen        []string
	restoreCurrentPath string

	waveMu       sync.RWMutex
	waveform     []WavePoint
	waveChannels int
	waveProgress float64
	waveLoading  bool
	waveToken    uint64
	waveZoom     int
	waveOffset   float64
	waveCacheHit bool

	liveWaveform     []WavePoint
	liveWaveChannels int
	liveWaveSequence uint64
	liveWaveBands    [waveformBandCount]float32

	peakL, peakR, rmsL, rmsR                       float32
	vuHoldL, vuHoldR                               float32
	vuHoldAgeL, vuHoldAgeR                         uint16
	lufsM, lufsS, lufsI, lra, truePeak             float32
	loudnessHistory                                []LoudnessSample
	loudnessLoopMarkers                            []float64
	loudnessElapsed                                float64
	loudnessRenderElapsed                          float64
	historyClock                                   historyClock
	loudnessSegment                                uint32
	loudnessWindow                                 int
	loudnessHZoom                                  float64
	loudnessYMin, loudnessYMax                     float32
	loudnessTargetMode                             int
	loudnessCustomTarget                           float32
	loudnessLabelM, loudnessLabelS, loudnessLabelI float32
	loudnessLabelInit                              bool
	meterDisplayPeak, meterDisplayTruePeak         float32
	meterDisplayM, meterDisplayS, meterDisplayI    float32
	meterDisplayLRA                                float32
	meterDisplayInit                               bool
	clips                                          uint64
	spectrum                                       [384]float32
	spectrumTarget                                 [384]float32
	spectrumHold                                   [384]float32
	spectrumHoldAge                                [384]uint16
	spectrumBinStart                               [384]int
	spectrumBinEnd                                 [384]int
	spectrumTiltOffset                             [384]float32
	spectrumMapFFT                                 int
	spectrumMapRate                                uint32
	spectrumMapTilt                                int
	spectrumMapBands                               int
	spectrumFFT, spectrumSmoothing                 int
	spectrumTilt, spectrumBandMode, stereoMode     int
	stereoStyle                                    int
	stereoNormalize                                bool
	stereoNormalizeGain                            float32
	spectrumMode                                   spectrumDisplayMode
	analyzerView                                   analyzerDisplayView
	analyzerViewBeforeInput                        analyzerDisplayView
	inputForcedAnalyzer                            bool
	spectrogramTimeWindow                          int
	spectrogramRange                               int
	spectrogramScale                               spectrogramFrequencyScale
	spectrogramFreqMin, spectrogramFreqMax         float64
	liveSpectrogram                                liveSpectrogramState
	spectrogramScratch                             [8192]float32
	spectrogramMu                                  sync.RWMutex
	trackSpectrogram                               SpectrogramData
	spectrogramLoading                             bool
	spectrogramProgress                            float64
	spectrogramError                               string
	spectrogramToken                               uint64
	spectrogramSurface                             spectrogramSurface
	stereoPoints                                   [1024]StereoPoint
	stereoPointCount                               int
	stereoSequence                                 uint64
	stereoPhosphor                                 stereoPhosphorState
	stereoLineFadeUntil                            uint64
	correlation, correlationDisplay                float32
	tickCount                                      uint64
	wasPlaying                                     bool
	wasAnalysisActive                              bool
	manualStop                                     bool
	lastNonZeroVolume                              float32
	volumeDragStart                                float32
	volumeSaveAt                                   time.Time
	volumePopupUntil                               time.Time

	dragMode                 int
	dragSeek                 RECT
	dragSeekWave             bool
	dragSeekTrack            bool
	hover                    POINT
	paintedHover             POINT
	hoverDirty               bool
	mouseLeaveTracking       bool
	playlistDragIndex        int
	playlistDragMoved        bool
	playlistScrollDragOffset int32
	dragOutActive            bool
	oleReady                 bool
	spectrogramFreqDragY     int32
	playlistMenuOpen         bool
	playlistMenuScroll       int

	playlistWidth  int32
	rightWidth     int32
	fileInfoHeight int32
	loudnessHeight int32
	meterHeight    int32
	stereoHeight   int32

	layout Layout

	fontTitle, fontSection, fontBody, fontSmall, fontMetric, fontTiny uintptr
	uiImages                                                          map[string]*UIBitmap
	themeMode                                                         int
	dpi                                                               uint32
	deviceName                                                        string
	outputBackend                                                     int
	outputDeviceName                                                  string
	inputBackend                                                      int
	inputDeviceName                                                   string
	backDC, backBitmap, backOldBitmap                                 uintptr
	backW, backH                                                      int32
	waveBrushes                                                       map[uint32]uintptr
	spectrumGrid                                                      spectrumGridCache
	paintDirty                                                        uint32
	renderTickQueued                                                  atomic.Bool
	renderTickSerial                                                  atomic.Uint64
	windowX, windowY, windowW, windowH                                int32
	windowPlacementValid, windowMaximized                             bool
}

func NewApp() *App {
	a := &App{engine: newAudioEngine(), playlists: []Playlist{newDefaultPlaylist()}, activePlaylistID: defaultPlaylistID, playbackPlaylistID: defaultPlaylistID, current: -1, continuousPlay: true, dpi: 96, status: "Preparing audio engine…", lufsM: -120, lufsS: -120, lufsI: -120, lra: 0, truePeak: -120, hover: hoverOutsidePoint(), paintedHover: hoverOutsidePoint(),
		playlistWidth: 260, rightWidth: 410, fileInfoHeight: 220, loudnessHeight: 260, meterHeight: 318, stereoHeight: 238, loudnessWindow: 3, loudnessHZoom: 1, loudnessYMin: -60, loudnessYMax: 0, loudnessCustomTarget: -18, spectrumFFT: 1, spectrumSmoothing: 1, spectrumTilt: 2, spectrogramTimeWindow: 1, spectrogramRange: 90, waveZoom: 1, deviceName: "Output Device", inputDeviceName: "NONE", lastNonZeroVolume: defaultPlaybackVolume}
	a.stereoNormalizeGain = 1
	a.fontTitle = createFont(22, FW_BOLD)
	a.fontSection = createFont(15, FW_SEMIBOLD)
	a.fontBody = createFont(14, FW_NORMAL)
	a.fontSmall = createFont(12, FW_NORMAL)
	a.fontMetric = createFont(24, FW_SEMIBOLD)
	a.fontTiny = createFont(10, FW_NORMAL)
	assets, err := loadEmbeddedUIAssets()
	if err == nil {
		a.uiImages = assets
	} else {
		a.status = "Could not load interface assets: " + err.Error()
		a.statusError = true
	}
	a.loadLayoutConfig()
	a.loadPlaylistState()
	a.applyTheme()
	appendAppLog("INFO", appTitle+" started")
	return a
}

func (a *App) currentDPI() uint32 {
	if a.dpi < 96 {
		return 96
	}
	return a.dpi
}

func scaleDPI(v int32, dpi uint32) int32 {
	if dpi < 96 {
		dpi = 96
	}
	return int32((int64(v)*int64(dpi) + 48) / 96)
}

func unscaleDPI(v int32, dpi uint32) int32 {
	if dpi < 96 {
		dpi = 96
	}
	return int32((int64(v)*96 + int64(dpi)/2) / int64(dpi))
}

func (a *App) physicalToLogicalPoint(x, y int32) (int32, int32) {
	dpi := a.currentDPI()
	return unscaleDPI(x, dpi), unscaleDPI(y, dpi)
}

func (a *App) physicalToLogicalRect(r RECT) RECT {
	dpi := a.currentDPI()
	return RECT{unscaleDPI(r.Left, dpi), unscaleDPI(r.Top, dpi), unscaleDPI(r.Right, dpi), unscaleDPI(r.Bottom, dpi)}
}

func (a *App) logicalToPhysicalRect(r RECT) RECT {
	dpi := a.currentDPI()
	return RECT{scaleDPI(r.Left, dpi), scaleDPI(r.Top, dpi), scaleDPI(r.Right, dpi), scaleDPI(r.Bottom, dpi)}
}

func (a *App) setDPI(dpi uint32) {
	if dpi < 96 {
		dpi = 96
	}
	a.dpi = dpi
}

func createFontFace(faceName string, size int32, weight int32) uintptr {
	face := utf16Ptr(faceName)
	r, _, _ := procCreateFontW.Call(
		uintptr(uint32(-size)), 0, 0, 0, uintptr(weight), 0, 0, 0, DEFAULT_CHARSET,
		0, 0, CLEARTYPE_QUALITY, 0, uintptr(unsafe.Pointer(face)),
	)
	return r
}

func createFont(size int32, weight int32) uintptr {
	return createFontFace("Microsoft YaHei UI", size, weight)
}

type playlistEntryState struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Paths        []string `json:"paths"`
	SelectedPath string   `json:"selected_path,omitempty"`
	Scroll       int      `json:"scroll,omitempty"`
}

type playlistState struct {
	Version            int                  `json:"version,omitempty"`
	ActivePlaylistID   string               `json:"active_playlist_id,omitempty"`
	PlaybackPlaylistID string               `json:"playback_playlist_id,omitempty"`
	CurrentPath        string               `json:"current_path,omitempty"`
	Playlists          []playlistEntryState `json:"playlists,omitempty"`

	// Version 1 fields. They are read only and migrated into the default list.
	Paths        []string `json:"paths,omitempty"`
	SelectedPath string   `json:"selected_path,omitempty"`
}

func newDefaultPlaylist() Playlist {
	return Playlist{ID: defaultPlaylistID, Name: "PLAYLIST", Selected: -1}
}

func (a *App) playlistByID(id string) *Playlist {
	for i := range a.playlists {
		if a.playlists[i].ID == id {
			return &a.playlists[i]
		}
	}
	return nil
}

func (a *App) activePlaylist() *Playlist {
	if p := a.playlistByID(a.activePlaylistID); p != nil {
		return p
	}
	a.activePlaylistID = defaultPlaylistID
	return a.playlistByID(defaultPlaylistID)
}

func (a *App) playbackPlaylist() *Playlist {
	if p := a.playlistByID(a.playbackPlaylistID); p != nil {
		return p
	}
	a.playbackPlaylistID = defaultPlaylistID
	return a.playlistByID(defaultPlaylistID)
}

func newPlaylistID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return hex.EncodeToString(id[:])
	}
	return fmt.Sprintf("playlist-%d", time.Now().UnixNano())
}

func trackFromPath(path string) (Track, bool) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		if abs, err := filepath.Abs(clean); err == nil {
			clean = abs
		}
	}
	if !isAudioFile(clean) {
		return Track{}, false
	}
	info, err := os.Stat(clean)
	missing := err != nil || info.IsDir()
	return Track{Path: clean, Name: strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean)), Missing: missing}, true
}

func playlistFromState(entry playlistEntryState) Playlist {
	p := Playlist{ID: strings.TrimSpace(entry.ID), Name: strings.TrimSpace(entry.Name), Selected: -1, Scroll: entry.Scroll}
	if p.Scroll < 0 {
		p.Scroll = 0
	}
	seen := make(map[string]bool)
	for _, path := range entry.Paths {
		track, ok := trackFromPath(path)
		if !ok {
			continue
		}
		key := strings.ToLower(track.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		p.Tracks = append(p.Tracks, track)
	}
	if len(p.Tracks) > 0 {
		p.Selected = 0
		for i := range p.Tracks {
			if entry.SelectedPath != "" && strings.EqualFold(p.Tracks[i].Path, entry.SelectedPath) {
				p.Selected = i
				break
			}
		}
	}
	return p
}

func nextPlaylistName(playlists []Playlist) string {
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("New Playlist %d", n)
		used := false
		for i := range playlists {
			if strings.EqualFold(playlists[i].Name, candidate) {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
	}
}

func (a *App) validatePlaylistName(name, exceptID string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", tr("请输入 Playlist 名称")
	}
	if utf8.RuneCountInString(name) > 64 {
		return "", tr("名称不能超过 64 个字符")
	}
	if exceptID != defaultPlaylistID && strings.EqualFold(name, "PLAYLIST") {
		return "", tr("PLAYLIST 是默认列表的保留名称")
	}
	for i := range a.playlists {
		if a.playlists[i].ID != exceptID && strings.EqualFold(a.playlists[i].Name, name) {
			return "", tr("已存在同名 Playlist")
		}
	}
	return name, ""
}

func stateConfigDir() string {
	return filepath.Join(executableDir(), "runtime", "config")
}

func stateConfigPath(name string) string {
	return filepath.Join(stateConfigDir(), name)
}

func migrateLegacyStateFile(name string) {
	newPath := stateConfigPath(name)
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	oldPath := filepath.Join(executableDir(), name)
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	if os.MkdirAll(stateConfigDir(), 0755) != nil {
		return
	}
	if os.Rename(oldPath, newPath) == nil {
		return
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return
	}
	if os.WriteFile(newPath, data, 0644) == nil {
		_ = os.Remove(oldPath)
	}
}

func (a *App) playlistStatePath() string {
	return stateConfigPath("AuraluxPlayer.playlist.json")
}

func (a *App) loadPlaylistState() {
	migrateLegacyStateFile("AuraluxPlayer.playlist.json")
	data, err := os.ReadFile(a.playlistStatePath())
	if err != nil {
		return
	}
	var state playlistState
	if json.Unmarshal(data, &state) != nil {
		return
	}
	a.applyPlaylistState(state)
}

func (a *App) applyPlaylistState(state playlistState) {
	a.playlistSel.reset()
	a.playlists = nil
	if state.Version >= 2 && len(state.Playlists) > 0 {
		var defaultList *Playlist
		usedIDs := make(map[string]bool)
		usedNames := map[string]bool{"playlist": true}
		for _, entry := range state.Playlists {
			p := playlistFromState(entry)
			if p.ID == defaultPlaylistID && defaultList == nil {
				p.Name = "PLAYLIST"
				copy := p
				defaultList = &copy
				usedIDs[p.ID] = true
				continue
			}
			if p.ID == "" || usedIDs[p.ID] {
				p.ID = newPlaylistID()
			}
			if p.Name == "" || utf8.RuneCountInString(p.Name) > 64 || usedNames[strings.ToLower(p.Name)] {
				p.Name = nextPlaylistName(a.playlists)
			}
			usedIDs[p.ID] = true
			usedNames[strings.ToLower(p.Name)] = true
			a.playlists = append(a.playlists, p)
		}
		if defaultList == nil {
			p := newDefaultPlaylist()
			defaultList = &p
		}
		a.playlists = append([]Playlist{*defaultList}, a.playlists...)
		a.activePlaylistID = state.ActivePlaylistID
		a.playbackPlaylistID = state.PlaybackPlaylistID
	} else {
		legacy := playlistFromState(playlistEntryState{ID: defaultPlaylistID, Name: "PLAYLIST", Paths: state.Paths, SelectedPath: state.SelectedPath})
		a.playlists = []Playlist{legacy}
		a.activePlaylistID = defaultPlaylistID
		a.playbackPlaylistID = defaultPlaylistID
	}

	if a.playlistByID(a.activePlaylistID) == nil {
		a.activePlaylistID = defaultPlaylistID
	}
	if a.playlistByID(a.playbackPlaylistID) == nil {
		a.playbackPlaylistID = defaultPlaylistID
	}
	if state.CurrentPath != "" {
		if p := a.playbackPlaylist(); p != nil {
			for i := range p.Tracks {
				if strings.EqualFold(p.Tracks[i].Path, state.CurrentPath) {
					a.restoreCurrentPath = p.Tracks[i].Path
					break
				}
			}
		}
	}
}

func (a *App) savePlaylistState() {
	state := a.playlistStateSnapshot()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(stateConfigDir(), 0755) != nil {
		return
	}
	path := a.playlistStatePath()
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (a *App) playlistStateSnapshot() playlistState {
	state := playlistState{Version: 2, ActivePlaylistID: a.activePlaylistID, PlaybackPlaylistID: a.playbackPlaylistID}
	for i := range a.playlists {
		p := &a.playlists[i]
		entry := playlistEntryState{ID: p.ID, Name: p.Name, Paths: make([]string, 0, len(p.Tracks)), Scroll: p.Scroll}
		for _, track := range p.Tracks {
			entry.Paths = append(entry.Paths, track.Path)
		}
		if p.Selected >= 0 && p.Selected < len(p.Tracks) {
			entry.SelectedPath = p.Tracks[p.Selected].Path
		}
		state.Playlists = append(state.Playlists, entry)
	}
	if p := a.playbackPlaylist(); p != nil && a.current >= 0 && a.current < len(p.Tracks) {
		state.CurrentPath = p.Tracks[a.current].Path
	} else if a.restoreCurrentPath != "" {
		// Keep the last loaded context while its file is temporarily offline.
		// A later launch can retry it without autoplaying.
		state.CurrentPath = a.restoreCurrentPath
	}
	return state
}

type layoutConfig struct {
	Volume                float32 `json:"volume"`
	UnmutedVolume         float32 `json:"unmuted_volume"`
	LayoutVersion         int     `json:"layout_version"`
	PlaylistWidth         int32   `json:"playlist_width"`
	RightWidth            int32   `json:"right_width"`
	FileInfoHeight        int32   `json:"file_info_height"`
	WaveHeight            int32   `json:"wave_height,omitempty"` // legacy adjustable center-panel height; migrated in 1.5.1
	LoudnessHeight        int32   `json:"loudness_height"`
	MeterHeight           int32   `json:"meter_height"`
	StereoHeight          int32   `json:"stereo_height"`
	ThemeMode             int     `json:"theme_mode"`
	LoudnessWindow        int     `json:"loudness_window"`
	LoudnessHZoom         float64 `json:"loudness_h_zoom,omitempty"`
	LoudnessYMin          float32 `json:"loudness_y_min,omitempty"`
	LoudnessYMax          float32 `json:"loudness_y_max,omitempty"`
	LoudnessTargetMode    int     `json:"loudness_target_mode"`
	LoudnessCustomTarget  float32 `json:"loudness_custom_target"`
	SpectrumFFT           int     `json:"spectrum_fft"`
	SpectrumSmoothing     int     `json:"spectrum_smoothing"`
	SpectrumTilt          int     `json:"spectrum_tilt"`
	SpectrumBandMode      int     `json:"spectrum_band_mode"`
	AnalyzerView          int     `json:"analyzer_view,omitempty"`
	SpectrogramTimeWindow int     `json:"spectrogram_time_window,omitempty"`
	SpectrogramRange      int     `json:"spectrogram_range,omitempty"`
	SpectrogramScale      int     `json:"spectrogram_scale,omitempty"`
	SpectrogramFreqMin    float64 `json:"spectrogram_freq_min,omitempty"`
	SpectrogramFreqMax    float64 `json:"spectrogram_freq_max,omitempty"`
	StereoMode            int     `json:"stereo_mode"`
	DiffuseDots           bool    `json:"diffuse_dots,omitempty"`
	StereoStyle           int     `json:"stereo_style,omitempty"`
	StereoBlue            bool    `json:"stereo_blue,omitempty"`
	StereoNormalize       bool    `json:"stereo_normalize,omitempty"`
	ContinuousPlay        bool    `json:"continuous_play"`
	OutputBackend         int     `json:"output_backend,omitempty"`
	OutputDeviceName      string  `json:"output_device_name,omitempty"`
	WindowX               int32   `json:"window_x,omitempty"`
	WindowY               int32   `json:"window_y,omitempty"`
	WindowWidth           int32   `json:"window_width,omitempty"`
	WindowHeight          int32   `json:"window_height,omitempty"`
	WindowMaximized       bool    `json:"window_maximized,omitempty"`
}

func (a *App) layoutConfigPath() string {
	return stateConfigPath("AuraluxPlayer.layout.json")
}

func (a *App) loadLayoutConfig() {
	migrateLegacyStateFile("AuraluxPlayer.layout.json")
	data, err := os.ReadFile(a.layoutConfigPath())
	if err != nil {
		return
	}
	// Missing fields in older layouts mean first launch; an explicit zero means mute.
	cfg := layoutConfig{Volume: defaultPlaybackVolume, UnmutedVolume: defaultPlaybackVolume}
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	if cfg.Volume >= 0 && cfg.Volume <= maxPlaybackVolume {
		a.engine.SetVolume(cfg.Volume)
	}
	if v := a.engine.Volume(); v > 0.001 {
		a.lastNonZeroVolume = v
	} else if cfg.UnmutedVolume > 0.001 && cfg.UnmutedVolume <= maxPlaybackVolume {
		a.lastNonZeroVolume = cfg.UnmutedVolume
	}
	if cfg.PlaylistWidth >= 210 && cfg.PlaylistWidth <= 520 {
		a.playlistWidth = cfg.PlaylistWidth
	}
	if cfg.RightWidth >= 240 && cfg.RightWidth <= 620 {
		a.rightWidth = cfg.RightWidth
		// Migrate pre-1.5.5 layouts so the consolidated Meter cards open at a
		// comfortably readable default width. Users can still drag the left edge.
		if cfg.LayoutVersion < 2 && a.rightWidth < 400 {
			a.rightWidth = 400
		}
	}
	if cfg.FileInfoHeight >= 140 && cfg.FileInfoHeight <= 420 {
		a.fileInfoHeight = cfg.FileInfoHeight
	}
	if cfg.LoudnessHeight >= 110 && cfg.LoudnessHeight <= 700 {
		a.loudnessHeight = cfg.LoudnessHeight
	} else if cfg.WaveHeight >= 110 && cfg.WaveHeight <= 700 {
		// Migrate the adjustable center-panel height from pre-1.5.1 layouts.
		a.loudnessHeight = cfg.WaveHeight
	}
	if cfg.MeterHeight >= 286 && cfg.MeterHeight <= 700 {
		a.meterHeight = cfg.MeterHeight
	}
	if cfg.StereoHeight >= 150 && cfg.StereoHeight <= 520 {
		a.stereoHeight = cfg.StereoHeight
	}
	if cfg.ThemeMode == 1 {
		a.themeMode = 1
	}
	if cfg.LayoutVersion < 6 {
		// Migrate the old 30s / 60s / FULL enum to the new
		// 5s / 15s / 30s / 60s / FULL enum.
		switch cfg.LoudnessWindow {
		case 0:
			a.loudnessWindow = 2
		case 1:
			a.loudnessWindow = 3
		case 2:
			a.loudnessWindow = 4
		}
	} else if cfg.LoudnessWindow >= 0 && cfg.LoudnessWindow <= 4 {
		a.loudnessWindow = cfg.LoudnessWindow
	}
	if cfg.LoudnessHZoom >= 1 && cfg.LoudnessHZoom <= 64 {
		a.loudnessHZoom = cfg.LoudnessHZoom
	}
	if cfg.LoudnessYMin >= -60 && cfg.LoudnessYMin <= -4 && cfg.LoudnessYMax <= 0 && cfg.LoudnessYMax >= -56 && cfg.LoudnessYMax-cfg.LoudnessYMin >= 6 {
		a.loudnessYMin = cfg.LoudnessYMin
		a.loudnessYMax = cfg.LoudnessYMax
	}
	if cfg.LoudnessTargetMode >= 0 && cfg.LoudnessTargetMode <= 3 {
		a.loudnessTargetMode = cfg.LoudnessTargetMode
	}
	if cfg.LoudnessCustomTarget <= -8 && cfg.LoudnessCustomTarget >= -36 {
		a.loudnessCustomTarget = cfg.LoudnessCustomTarget
	}
	if cfg.SpectrumFFT >= 0 && cfg.SpectrumFFT <= 3 {
		a.spectrumFFT = cfg.SpectrumFFT
	}
	if cfg.SpectrumSmoothing >= 0 && cfg.SpectrumSmoothing <= 2 {
		a.spectrumSmoothing = cfg.SpectrumSmoothing
	}
	if cfg.SpectrumTilt >= 0 && cfg.SpectrumTilt <= 2 {
		a.spectrumTilt = cfg.SpectrumTilt
	}
	if cfg.SpectrumBandMode >= 0 && cfg.SpectrumBandMode <= 2 {
		a.spectrumBandMode = cfg.SpectrumBandMode
	}
	if cfg.LayoutVersion >= 13 {
		if cfg.AnalyzerView >= 0 && cfg.AnalyzerView <= 2 {
			a.analyzerView = analyzerDisplayView(cfg.AnalyzerView)
			if a.analyzerView == analyzerViewTrackSpectrogram {
				a.analyzerView = analyzerViewLiveSpectrogram
			}
		}
		if cfg.SpectrogramTimeWindow >= 0 && cfg.SpectrogramTimeWindow <= 3 {
			a.spectrogramTimeWindow = cfg.SpectrogramTimeWindow
		}
		if cfg.SpectrogramRange == 60 || cfg.SpectrogramRange == 90 || cfg.SpectrogramRange == 120 {
			a.spectrogramRange = cfg.SpectrogramRange
		}
	}
	if cfg.LayoutVersion >= 14 {
		if cfg.SpectrogramScale >= 0 && cfg.SpectrogramScale <= int(spectrogramScaleLinear) {
			a.spectrogramScale = spectrogramFrequencyScale(cfg.SpectrogramScale)
		}
		if cfg.SpectrogramFreqMin >= 20 && cfg.SpectrogramFreqMax > cfg.SpectrogramFreqMin {
			a.spectrogramFreqMin = cfg.SpectrogramFreqMin
			a.spectrogramFreqMax = cfg.SpectrogramFreqMax
		}
	}
	if cfg.StereoMode >= 0 && cfg.StereoMode <= 2 {
		a.stereoMode = cfg.StereoMode
	}
	a.stereoPhosphor.blue = cfg.StereoBlue
	a.stereoPhosphor.diffuseDots = cfg.DiffuseDots
	if cfg.StereoStyle >= 0 && cfg.StereoStyle <= 2 {
		a.stereoStyle = cfg.StereoStyle
	}
	if cfg.LayoutVersion >= 12 {
		a.stereoNormalize = cfg.StereoNormalize
	}
	if cfg.LayoutVersion >= 9 {
		a.continuousPlay = cfg.ContinuousPlay
	} else {
		a.continuousPlay = true
	}
	if cfg.LayoutVersion >= 10 {
		if cfg.OutputBackend == OUTPUT_ASIO {
			a.outputBackend = OUTPUT_ASIO
		}
		a.outputDeviceName = strings.TrimSpace(cfg.OutputDeviceName)
	}
	if cfg.WindowWidth >= 640 && cfg.WindowHeight >= 480 {
		a.windowX = cfg.WindowX
		a.windowY = cfg.WindowY
		a.windowW = cfg.WindowWidth
		a.windowH = cfg.WindowHeight
		a.windowMaximized = cfg.WindowMaximized
		a.windowPlacementValid = true
	}
}

func (a *App) saveLayoutConfig() {
	view := a.analyzerView
	if a.inputForcedAnalyzer {
		view = a.analyzerViewBeforeInput
	}
	cfg := layoutConfig{
		Volume: a.engine.Volume(), UnmutedVolume: a.lastNonZeroVolume,
		LayoutVersion: 14, PlaylistWidth: a.playlistWidth, RightWidth: a.rightWidth, FileInfoHeight: a.fileInfoHeight,
		LoudnessHeight: a.loudnessHeight, MeterHeight: a.meterHeight, StereoHeight: a.stereoHeight, ThemeMode: a.themeMode,
		LoudnessWindow: a.loudnessWindow, LoudnessHZoom: a.loudnessHZoom, LoudnessYMin: a.loudnessYMin, LoudnessYMax: a.loudnessYMax,
		LoudnessTargetMode: a.loudnessTargetMode, LoudnessCustomTarget: a.loudnessCustomTarget,
		SpectrumFFT: a.spectrumFFT, SpectrumSmoothing: a.spectrumSmoothing, SpectrumTilt: a.spectrumTilt, SpectrumBandMode: a.spectrumBandMode,
		AnalyzerView: int(view), SpectrogramTimeWindow: a.spectrogramTimeWindow, SpectrogramRange: a.spectrogramRange,
		SpectrogramScale: int(a.spectrogramScale), SpectrogramFreqMin: a.spectrogramFreqMin, SpectrogramFreqMax: a.spectrogramFreqMax,
		StereoMode: a.stereoMode, StereoStyle: a.stereoStyle, StereoBlue: a.stereoPhosphor.blue, DiffuseDots: a.stereoPhosphor.diffuseDots, StereoNormalize: a.stereoNormalize, ContinuousPlay: a.continuousPlay,
		OutputBackend: a.outputBackend, OutputDeviceName: a.outputDeviceName,
		WindowX: a.windowX, WindowY: a.windowY, WindowWidth: a.windowW, WindowHeight: a.windowH, WindowMaximized: a.windowMaximized,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(stateConfigDir(), 0755) != nil {
		return
	}
	_ = os.WriteFile(a.layoutConfigPath(), data, 0644)
}

func primaryWorkArea() RECT {
	var work RECT
	if r, _, _ := procSystemParametersInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&work)), 0); r != 0 && rectW(work) > 0 && rectH(work) > 0 {
		return work
	}
	return RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
}

func windowRectTouchesMonitor(r RECT) bool {
	if rectW(r) < 1 || rectH(r) < 1 {
		return false
	}
	m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), MONITOR_DEFAULTTONULL)
	return m != 0
}

func (a *App) initialWindowBounds(dpi uint32) (x, y, w, h int32) {
	if a.windowPlacementValid {
		r := RECT{Left: a.windowX, Top: a.windowY, Right: a.windowX + a.windowW, Bottom: a.windowY + a.windowH}
		if windowRectTouchesMonitor(r) {
			return a.windowX, a.windowY, a.windowW, a.windowH
		}
	}
	work := primaryWorkArea()
	w = scaleDPI(1500, dpi)
	h = scaleDPI(900, dpi)
	margin := scaleDPI(24, dpi)
	if w > rectW(work)-margin*2 {
		w = rectW(work) - margin*2
	}
	if h > rectH(work)-margin*2 {
		h = rectH(work) - margin*2
	}
	minW := scaleDPI(1180, dpi)
	minH := scaleDPI(720, dpi)
	if w < minW && rectW(work) >= minW {
		w = minW
	}
	if h < minH && rectH(work) >= minH {
		h = minH
	}
	x = work.Left + (rectW(work)-w)/2
	y = work.Top + (rectH(work)-h)/2
	return
}

func (a *App) captureWindowPlacement(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	wp := WINDOWPLACEMENT{Length: uint32(unsafe.Sizeof(WINDOWPLACEMENT{}))}
	if ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); ok == 0 {
		return
	}
	r := wp.RcNormalPosition
	if rectW(r) < 640 || rectH(r) < 480 {
		return
	}
	a.windowX = r.Left
	a.windowY = r.Top
	a.windowW = rectW(r)
	a.windowH = rectH(r)
	a.windowMaximized = wp.ShowCmd == SW_SHOWMAXIMIZED
	a.windowPlacementValid = true
}

func (a *App) toggleTheme() {
	if a.themeMode == 0 {
		a.themeMode = 1
	} else {
		a.themeMode = 0
	}
	a.applyTheme()
	a.stereoPhosphor.reset()
	a.saveLayoutConfig()
}

func (a *App) applyTheme() {
	a.destroyWaveBrushes()
	destroyGDICache()
	setTheme(a.themeMode)
	a.refreshHelpTheme()
	if a.hwnd != 0 {
		setWindowThemeMode(a.hwnd, a.themeMode == 0)
		invalidate(a.hwnd)
	}
}

func (a *App) resetLayoutConfig() {
	a.playlistWidth = 260
	a.rightWidth = 410
	a.fileInfoHeight = 220
	a.loudnessHeight = 260
	a.loudnessHZoom = 1
	a.loudnessYMin, a.loudnessYMax = -60, 0
	a.meterHeight = 318
	a.stereoHeight = 238
	a.saveLayoutConfig()
}

func (a *App) setStatus(s string, isErr bool) {
	a.statusMu.Lock()
	a.status = s
	a.statusError = isErr
	a.statusMu.Unlock()
	if isErr {
		appendAppLog("ERROR", s)
	}
	if a.hwnd != 0 {
		postMessage(a.hwnd, msgStatus, 0, 0)
	}
}

func appDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return executableDir()
	}
	return filepath.Join(base, "AuraluxPlayer")
}

func appendAppLog(level, message string) {
	dir := appDataDir()
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "AuraluxPlayer.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s [%s] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), level, strings.ReplaceAll(message, "\n", " "))
}
func (a *App) getStatus() (string, bool) {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status, a.statusError
}

func (a *App) startEngineSetup() {
	go func() {
		runtimeDir := filepath.Join(executableDir(), "runtime")
		err := ensureAudioRuntime(runtimeDir, func(s string) { a.setStatus(s, false) })
		if err == nil {
			err = a.engine.Load(runtimeDir, a.hwnd)
		}
		if err != nil {
			a.setStatus("Audio engine failed to start: "+err.Error(), true)
			postMessage(a.hwnd, msgEngineReady, 0, 0)
			return
		}
		a.engineReady.Store(true)
		appendAppLog("INFO", "audio engine ready")
		a.setStatus("Ready · Drag audio here or choose Add Audio.", false)
		postMessage(a.hwnd, msgEngineReady, 1, 0)
	}()
}

func (a *App) handleMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) (bool, uintptr) {
	switch msg {
	case 0x2b, 0x2c:
		if activePopup.handle(hwnd, msg, lparam) {
			return true, 1
		}
		return false, 0
	case msgUpdateStartup:
		a.initUpdates()
		return true, 0
	case msgUpdateEvent:
		a.finishUpdateJob()
		return true, 0
	case msgUpdateProgress:
		a.showDownloadProgress(int(wparam))
		return true, 0
	case WM_ERASEBKGND:
		return true, 1
	case WM_GETMINMAXINFO:
		m := (*MINMAXINFO)(unsafe.Pointer(lparam))
		dpi := a.currentDPI()
		m.PtMinTrackSize = POINT{scaleDPI(1180, dpi), scaleDPI(720, dpi)}
		return true, 0
	case WM_DPICHANGED:
		newDPI := uint32(loword(wparam))
		a.setDPI(newDPI)
		suggested := (*RECT)(unsafe.Pointer(lparam))
		if suggested != nil {
			procSetWindowPos.Call(hwnd, 0, uintptr(suggested.Left), uintptr(suggested.Top), uintptr(rectW(*suggested)), uintptr(rectH(*suggested)), SWP_NOZORDER|SWP_NOACTIVATE)
		}
		invalidate(hwnd)
		return true, 0
	case WM_SIZE:
		invalidate(hwnd)
		return true, 0
	case WM_PAINT:
		a.paint(hwnd)
		return true, 0
	case WM_TIMER:
		// Legacy/fallback timer messages are ignored. Animation is driven by
		// msgRenderTick so mouse input cannot starve the visual update cadence.
		return true, 0
	case msgRenderTick:
		a.renderTickQueued.Store(false)
		a.flushHoverInvalidation()
		a.onTick(a.renderTickSerial.Load())
		return true, 0
	case msgPlaybackEnd:
		a.onPlaybackEnded(uint64(wparam), uint32(lparam))
		return true, 0
	case WM_LBUTTONDOWN:
		x, y := a.physicalToLogicalPoint(loword(lparam), hiword(lparam))
		a.onMouseDown(x, y)
		return true, 0
	case WM_LBUTTONUP:
		wasLayoutDrag := a.dragMode >= dragSplitPlaylistMode && a.dragMode <= dragSplitPlaylistInfoMode
		wasPlaylistDrag := a.dragMode == dragPlaylistTrackMode && a.playlistDragMoved
		wasVolumeDrag := a.dragMode == dragVolumeMode
		wasSpectrogramFreqDrag := a.dragMode == dragSpectrogramFreqMode
		a.dragMode = 0
		a.dragSeekWave = false
		a.dragSeekTrack = false
		if wasVolumeDrag {
			v := a.engine.Volume()
			if v > 0.001 {
				a.lastNonZeroVolume = v
			} else if a.volumeDragStart > 0.001 {
				a.lastNonZeroVolume = a.volumeDragStart
			}
		}
		procReleaseCapture.Call()
		if wasLayoutDrag || wasVolumeDrag {
			a.saveLayoutConfig()
		}
		if wasPlaylistDrag {
			a.savePlaylistState()
		}
		if wasSpectrogramFreqDrag {
			a.saveLayoutConfig()
		}
		invalidate(hwnd)
		return true, 0
	case WM_RBUTTONUP:
		if a.playlistMenuOpen {
			a.playlistMenuOpen = false
			invalidateArea(a.hwnd, a.layout.Playlist)
			return true, 0
		}
		x, y := a.physicalToLogicalPoint(loword(lparam), hiword(lparam))
		if a.stereoStyle == 1 && pointInRect(x, y, a.layout.Stereo) {
			a.showDotMenu(x, y)
		} else {
			a.showPlaylistMenu(x, y)
		}
		return true, 0
	case WM_MOUSEMOVE:
		a.trackMouseLeave(hwnd)
		x, y := a.physicalToLogicalPoint(loword(lparam), hiword(lparam))
		a.hover = POINT{x, y}
		if a.dragMode != 0 {
			a.onMouseDrag(a.hover.X, a.hover.Y)
		} else {
			// Mouse input can arrive hundreds of times per second. Do not repaint
			// the whole window for every move; remember the latest hover position
			// and repaint only hover-sensitive panels on the next render tick.
			a.hoverDirty = true
		}
		return true, 0
	case WM_MOUSELEAVE:
		a.clearMouseHover()
		return true, 0
	case WM_KILLFOCUS:
		a.clearMouseHover()
		return false, 0
	case WM_SETCURSOR:
		if a.setResizeCursor(hwnd) {
			return true, 1
		}
	case WM_LBUTTONDBLCLK:
		x, y := a.physicalToLogicalPoint(loword(lparam), hiword(lparam))
		a.onDoubleClick(x, y)
		return true, 0
	case WM_MOUSEWHEEL:
		pt := POINT{
			X: int32(int16(lparam & 0xffff)),
			Y: int32(int16((lparam >> 16) & 0xffff)),
		}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		pt.X, pt.Y = a.physicalToLogicalPoint(pt.X, pt.Y)
		a.hover = pt
		a.onWheel(signedHiword(wparam), uint16(wparam&0xffff), pt.X, pt.Y)
		return true, 0
	case WM_KEYDOWN:
		a.onKey(uint32(wparam))
		return true, 0
	case WM_COPYDATA:
		if lparam == 0 {
			return true, 0
		}
		cds := (*COPYDATASTRUCT)(unsafe.Pointer(lparam))
		if cds.DwData != 0x4155524C || cds.LpData == 0 || cds.CbData < 2 {
			return true, 0
		}
		count := int(cds.CbData / 2)
		buf := unsafe.Slice((*uint16)(unsafe.Pointer(cds.LpData)), count)
		payload := syscall.UTF16ToString(buf)
		paths := startupAudioFiles(strings.Split(payload, "\n"))
		if len(paths) > 0 {
			if a.engineReady.Load() {
				a.addExternalFiles(paths)
			} else {
				a.pendingOpen = append(a.pendingOpen, paths...)
			}
		}
		return true, 1
	case WM_DROPFILES:
		a.onDrop(wparam)
		return true, 0
	case msgEngineReady:
		a.help.engineSetupDone = true
		a.autoCheckWhenReady()
		if wparam == 1 {
			a.restoreOutputSelection()
			a.refreshDeviceName()
		}
		if wparam == 1 && len(a.pendingOpen) > 0 {
			paths := append([]string(nil), a.pendingOpen...)
			a.pendingOpen = nil
			a.addExternalFiles(paths)
		} else if wparam == 1 && a.restoreCurrentPath != "" {
			if p := a.playbackPlaylist(); p != nil {
				for i, track := range p.Tracks {
					if strings.EqualFold(track.Path, a.restoreCurrentPath) {
						path := a.restoreCurrentPath
						a.loadTrackFromPlaylist(p.ID, i, false)
						if a.current == i && strings.EqualFold(p.Tracks[i].Path, path) {
							a.restoreCurrentPath = ""
						}
						break
					}
				}
			}
		}
		invalidate(hwnd)
		return true, 0
	case msgWaveReady:
		if a.analyzerView == analyzerViewTrackSpectrogram {
			a.ensureTrackSpectrogram()
		}
		invalidate(hwnd)
		return true, 0
	case msgStatus:
		invalidate(hwnd)
		return true, 0
	case WM_CLOSE:
		a.captureWindowPlacement(hwnd)
		a.saveLayoutConfig()
		return false, 0
	case WM_DESTROY:
		if a.help.cancel != nil {
			a.help.cancel()
		}
		a.captureWindowPlacement(hwnd)
		a.saveLayoutConfig()
		a.savePlaylistState()
		if a.engine != nil {
			a.engine.Close()
		}
		a.destroyBackBuffer()
		a.stereoPhosphor.destroy()
		a.spectrogramSurface.destroy()
		a.destroyWaveBrushes()
		destroyGDICache()
		for _, f := range []uintptr{a.fontTitle, a.fontSection, a.fontBody, a.fontSmall, a.fontMetric, a.fontTiny} {
			if f != 0 {
				procDeleteObject.Call(f)
			}
		}
		destroyUIAssets(a.uiImages)
		procPostQuitMessage.Call(0)
		return true, 0
	}
	return false, 0
}

func (a *App) hoverPointIn(pt POINT, r RECT) bool {
	return pointInRect(pt.X, pt.Y, r)
}

func hoverOutsidePoint() POINT {
	return POINT{-10000, -10000}
}

func (a *App) trackMouseLeave(hwnd uintptr) {
	if a.mouseLeaveTracking {
		return
	}
	tme := TRACKMOUSEEVENT{
		CbSize:    uint32(unsafe.Sizeof(TRACKMOUSEEVENT{})),
		DwFlags:   TME_LEAVE,
		HwndTrack: hwnd,
	}
	if tracked, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme))); tracked != 0 {
		a.mouseLeaveTracking = true
	}
}

func (a *App) clearMouseHover() {
	a.mouseLeaveTracking = false
	a.hover = hoverOutsidePoint()
	a.hoverDirty = true
}

func (a *App) flushHoverInvalidation() {
	if !a.hoverDirty {
		return
	}
	a.hoverDirty = false
	old := a.paintedHover
	now := a.hover
	a.paintedHover = now

	// Hover-only repaints are aligned to the render cadence and restricted to
	// panels that actually use the cursor position. This avoids a full-window
	// GDI pass for every WM_MOUSEMOVE while preserving crosshairs and hover UI.
	refresh := func(bit uint32, r RECT) {
		if a.hoverPointIn(old, r) || a.hoverPointIn(now, r) {
			a.invalidateDynamic(bit, r)
		}
	}
	refresh(dirtyHeader, a.layout.Header)
	refresh(dirtyPlaylist, a.layout.Playlist)
	refresh(dirtyWave, a.layout.Wave)
	refresh(dirtyLoudness, a.layout.Loudness)
	refresh(dirtySpectrum, a.layout.Spectrum)
	refresh(dirtyStereo, a.layout.Stereo)
	refresh(dirtyControls, a.layout.Controls)

	for _, r := range []RECT{a.layout.SplitPlaylist, a.layout.SplitRight, a.layout.SplitPlaylistInfo, a.layout.SplitLoudnessSpectrum, a.layout.SplitMeterStereo} {
		hit := inflateRect(r, 4, 4)
		if a.hoverPointIn(old, hit) || a.hoverPointIn(now, hit) {
			a.invalidateDynamic(dirtySplitters, hit)
		}
	}
}

func smoothDisplayValue(current, target, attack, release float32) float32 {
	if current <= -119 && target > -119 {
		return target
	}
	factor := release
	if target > current {
		factor = attack
	}
	return current + (target-current)*factor
}

func (a *App) updateMeterDisplay() {
	peak := maxf(ampToDB(a.peakL), ampToDB(a.peakR))
	if !a.meterDisplayInit {
		a.meterDisplayPeak = peak
		a.meterDisplayTruePeak = a.truePeak
		a.meterDisplayM = a.lufsM
		a.meterDisplayS = a.lufsS
		a.meterDisplayI = a.lufsI
		a.meterDisplayLRA = a.lra
		a.meterDisplayInit = true
		return
	}
	// Peak meters keep their display ballistics, but loudness values already
	// have standard-defined 400 ms / 3 s / integrated windows. Smoothing those
	// values again makes the cards lag behind the actual BASSloud result and can
	// hide the final Momentary value of short clips.
	a.meterDisplayPeak = smoothDisplayValue(a.meterDisplayPeak, peak, 0.88, 0.18)
	a.meterDisplayTruePeak = smoothDisplayValue(a.meterDisplayTruePeak, a.truePeak, 0.78, 0.14)
	a.meterDisplayM = a.lufsM
	a.meterDisplayS = a.lufsS
	a.meterDisplayI = a.lufsI
	a.meterDisplayLRA = a.lra
}

func (a *App) updateLoudnessMeasurement() {
	m, st, i, lra, tp, _ := a.engine.Loudness()
	a.lufsM = safeMeter(m)
	a.lufsS = safeMeter(st)
	a.lufsI = safeMeter(i)
	if !a.loudnessLabelInit {
		if a.lufsM > -100 || a.lufsS > -100 || a.lufsI > -100 {
			a.loudnessLabelM, a.loudnessLabelS, a.loudnessLabelI = a.lufsM, a.lufsS, a.lufsI
			a.loudnessLabelInit = true
		}
	} else {
		// Slow display-only smoothing keeps the history end labels readable;
		// the history curves and right-side cards use the unsmoothed values.
		a.loudnessLabelM += (a.lufsM - a.loudnessLabelM) * 0.055
		a.loudnessLabelS += (a.lufsS - a.loudnessLabelS) * 0.045
		a.loudnessLabelI += (a.lufsI - a.loudnessLabelI) * 0.025
	}
	a.lra = lra
	if a.lra < 0 || a.lra > 99 {
		a.lra = 0
	}
	a.truePeak = safeMeter(tp)
}

func loudnessMeterRefreshDue(tick uint64, wasActive, active bool) bool {
	return (active && tick%6 == 0) || (wasActive && !active)
}

func (a *App) onTick(renderTick uint64) {
	a.tickCount++
	if a.analyzerView == analyzerViewTrackSpectrogram && a.tickCount%12 == 0 {
		a.spectrogramMu.RLock()
		loading := a.spectrogramLoading
		a.spectrogramMu.RUnlock()
		if loading {
			a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
		}
	}
	if !a.volumeSaveAt.IsZero() && !time.Now().Before(a.volumeSaveAt) {
		a.volumeSaveAt = time.Time{}
		a.saveLayoutConfig()
	}
	if !a.volumePopupUntil.IsZero() && !time.Now().Before(a.volumePopupUntil) {
		a.volumePopupUntil = time.Time{}
		if rectW(a.layout.Controls) > 0 {
			a.invalidateDynamic(dirtyControls, a.layout.Controls)
		} else {
			invalidate(a.hwnd)
		}
	}
	if !a.engineReady.Load() {
		if a.tickCount%4 == 0 {
			invalidate(a.hwnd)
		}
		return
	}

	playing := a.engine.IsPlaying()
	inputActive := a.engine.InputActive()
	analysisActive := playing || inputActive
	analysisStateChanged := a.wasAnalysisActive != analysisActive
	loudnessMeterRefresh := loudnessMeterRefreshDue(a.tickCount, a.wasAnalysisActive, analysisActive)
	stereoComposed := false
	stereoFading := false
	stereoFadeFinished := false
	pL, pR, rL, rR, clips := a.engine.MeterSnapshot()
	a.peakL = maxf(pL, a.peakL*0.9592)
	a.peakR = maxf(pR, a.peakR*0.9592)
	// Fast attack / slower release makes the meter catch a one-shot immediately
	// without making the readout twitchy on the way back down.
	if rL > a.rmsL {
		a.rmsL += (rL - a.rmsL) * 0.384
	} else {
		a.rmsL += (rL - a.rmsL) * 0.046
	}
	if rR > a.rmsR {
		a.rmsR += (rR - a.rmsR) * 0.384
	} else {
		a.rmsR += (rR - a.rmsR) * 0.046
	}
	if a.clips != clips {
		a.clips = clips
		a.invalidateDynamic(dirtyMeter, a.layout.Meter)
	}

	if loudnessMeterRefresh {
		// The transition read is essential: a short clip can stop between two
		// scheduled polls. Without this final capture the next value is stored
		// after active repainting has ended and never reaches the meter panel.
		a.updateLoudnessMeasurement()
	}
	a.loudnessRenderElapsed = a.historyClock.step(time.Now(), analysisActive)
	if a.historyClock.pending != 0 {
		a.loudnessLoopMarkers = a.historyClock.markers(a.loudnessLoopMarkers, a.engine.DurationSeconds(), a.engine.PositionSeconds())
	}
	if analysisActive && a.tickCount%12 == 0 {
		a.appendLoudnessSample()
	}
	if loudnessMeterRefresh {
		a.updateMeterDisplay()
	}

	if !analysisActive {
		a.peakL, a.peakR = 0, 0
		a.rmsL, a.rmsR = 0, 0
		a.vuHoldL, a.vuHoldR = 0, 0
		a.vuHoldAgeL, a.vuHoldAgeR = 0, 0
		a.stereoPointCount = 0
		a.stereoNormalizeGain = 1
		if a.wasAnalysisActive {
			a.stereoLineFadeUntil = stereoStyleFadeUntil(renderTick, a.stereoStyle)
		}
		if stereoLineFadeRunning(renderTick, a.stereoLineFadeUntil) {
			stereoFading = true
			_, width, height, valid := stereoLineGeometry(a.layout.Stereo, a.currentDPI())
			if valid {
				_, stereoComposed = a.stereoPhosphor.advanceStyle(nil, a.stereoSequence, renderTick, a.stereoMode, a.stereoStyle, width, height)
			}
		} else {
			stereoFadeFinished = a.stereoLineFadeUntil != 0
			a.stereoLineFadeUntil = 0
			a.stereoPhosphor.reset()
		}
		a.correlation = 0
		a.correlationDisplay = 0
		if a.spectrumMode == spectrumModeNormal {
			a.resetSpectrumState()
		}
	} else {
		updateVUHold(&a.vuHoldL, &a.vuHoldAgeL, maxf(pL, a.peakL))
		updateVUHold(&a.vuHoldR, &a.vuHoldAgeR, maxf(pR, a.peakR))
		if a.tickCount%2 == 0 || (analysisStateChanged && a.analyzerView == analyzerViewLiveSpectrogram) {
			var waveformBands [waveformBandCount]float32
			acquired := false
			if a.analyzerView == analyzerViewLiveSpectrogram {
				fftSize := a.spectrumFFTSize()
				if fftSize < 8192 || a.tickCount%4 == 0 || analysisStateChanged {
					individual, channels := a.engine.FFTIndividual(fftSize)
					combined := combineIndividualFFT(a.spectrogramScratch[:], individual, channels)
					rate, _ := a.engine.AnalysisInfo()
					waveformBands = waveformBandsFromFFT(combined, rate)
					step := 1.0 / 60.0
					if fftSize >= 8192 {
						step = 1.0 / 30.0
					}
					a.liveSpectrogram.appendFFT(combined, rate, a.liveSpectrogram.nextTime(step))
					acquired = true
				}
			} else if a.analyzerView == analyzerViewSpectrum {
				waveformBands, acquired = a.updateSpectrumFrame()
			}
			if inputActive {
				// Spectrum FREEZE and the larger FFT throttling modes must not
				// freeze the live waveform color. Acquire a small FFT only when
				// the normal spectrum pass did not already provide one.
				if !acquired {
					fft := a.engine.FFT(2048)
					rate, _ := a.engine.AnalysisInfo()
					waveformBands = waveformBandsFromFFT(fft, rate)
				}
				a.liveWaveBands = waveformBands
				a.updateLiveWaveform(a.liveWaveBands)
			}
		}
		a.stereoLineFadeUntil = 0
		n, corr, sequence := a.engine.StereoSnapshotInto(a.stereoPoints[:])
		// Keep the scope focused on the newest ~transient-sized slice instead of
		// visually carrying a long tail of older phase points behind a one-shot.
		if n > 960 {
			copy(a.stereoPoints[:960], a.stereoPoints[n-960:n])
			n = 960
		}
		a.stereoPointCount = n
		a.stereoSequence = sequence
		if a.stereoNormalize {
			a.stereoNormalizeGain = normalizeStereoPoints(a.stereoPoints[:n], a.stereoNormalizeGain)
		} else {
			a.stereoNormalizeGain = 1
		}
		a.correlation = corr
		// Correlation is already stabilized by the engine's fixed-time,
		// energy-weighted window. A second frame-rate-dependent average would
		// distort that result and make response speed depend on UI load.
		a.correlationDisplay = corr
		// Capture the 120 Hz geometry stream so no audio interval is skipped, but
		// compose the full-size blurred DIB only every second tick. WM_PAINT then
		// performs only AlphaBlend, which keeps phosphor-backed output evenly paced
		// at 60 fps without putting the full pixel pass on every repaint.
		_, width, height, valid := stereoLineGeometry(a.layout.Stereo, a.currentDPI())
		if valid {
			// LINE ages the trace using the real collection cadence:
			// stereo points are taken every second audio frame (frameStep 2).
			rate, _ := a.engine.AnalysisInfo()
			a.stereoPhosphor.setLinePointRate(float32(rate) * 0.5)
			_, stereoComposed = a.stereoPhosphor.advanceStyle(a.stereoPoints[:a.stereoPointCount], a.stereoSequence, renderTick, a.stereoMode, a.stereoStyle, width, height)
		}
	}

	if playing {
		a.manualStop = false
	}
	stateChanged := a.wasPlaying != playing
	// Pause/resume at the same transport position should stay visually
	// continuous. Real seeks and track changes already create explicit
	// discontinuities elsewhere.
	a.wasPlaying = playing
	a.wasAnalysisActive = analysisActive

	if rectW(a.layout.Center) <= 0 {
		invalidate(a.hwnd)
		return
	}

	if analysisActive {
		if stereoComposed {
			a.invalidateDynamic(dirtyStereo, a.layout.Stereo)
		}
		if a.tickCount%2 == 0 {
			a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
			a.invalidateDynamic(dirtyLevel, a.layout.VU)
			if inputActive {
				// Live waveform paints at 60 fps; file waveforms retain their
				// lower-cost 30 fps playhead refresh.
				a.invalidateDynamic(dirtyWave, a.layout.Wave)
			}
		}
		if a.tickCount%4 == 0 {
			if !inputActive {
				a.invalidateDynamic(dirtyWave, a.layout.Wave)
			}
			a.invalidateDynamic(dirtyControls, a.layout.Controls)
			// Loudness History remains ~30 fps. Its data still arrives at the
			// analysis cadence, while the rolling time axis is interpolated.
			a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
		}
		// Numeric cards update at ~20 Hz so the standard loudness windows are
		// shown without adding another noticeable UI delay.
		if a.tickCount%6 == 0 {
			a.invalidateDynamic(dirtyMeter, a.layout.Meter)
		}
	} else if stereoFading {
		if stereoComposed {
			a.invalidateDynamic(dirtyStereo, a.layout.Stereo)
		}
		if stateChanged || analysisStateChanged {
			a.invalidateDynamic(dirtyWave|dirtyLoudness|dirtySpectrum, a.layout.Center)
			a.invalidateDynamic(dirtyMeter|dirtyLevel, a.layout.Right)
			a.invalidateDynamic(dirtyControls, a.layout.Controls)
		}
	} else if stateChanged || analysisStateChanged || stereoFadeFinished {
		a.invalidateDynamic(dirtyWave|dirtyLoudness|dirtySpectrum, a.layout.Center)
		a.invalidateDynamic(dirtyMeter|dirtyStereo|dirtyLevel, a.layout.Right)
		a.invalidateDynamic(dirtyControls, a.layout.Controls)
	}
}

func (a *App) invalidateDynamic(mask uint32, r RECT) {
	a.paintDirty |= mask
	invalidateArea(a.hwnd, r)
}

func (a *App) ensureSpectrumMap(fftSize int, rate uint32) {
	if rate == 0 {
		rate = 48000
	}
	bands := a.spectrumBandCount()
	if a.spectrumMapFFT == fftSize && a.spectrumMapRate == rate && a.spectrumMapTilt == a.spectrumTilt && a.spectrumMapBands == bands {
		return
	}
	maxBin := fftSize/2 - 1
	for band := 0; band < bands; band++ {
		f0 := 20 * math.Pow(1000, float64(band)/float64(bands))
		f1 := 20 * math.Pow(1000, float64(band+1)/float64(bands))
		b0 := int(f0 * float64(fftSize) / float64(rate))
		b1 := int(f1 * float64(fftSize) / float64(rate))
		if b0 < 1 {
			b0 = 1
		}
		if b0 > maxBin {
			b0 = maxBin
		}
		if b1 <= b0 {
			b1 = b0 + 1
		}
		if b1 > maxBin {
			b1 = maxBin
		}
		a.spectrumBinStart[band] = b0
		a.spectrumBinEnd[band] = b1
		centerFreq := math.Sqrt(f0 * f1)
		a.spectrumTiltOffset[band] = float32(a.spectrumTiltDB() * math.Log2(centerFreq/1000))
	}
	a.spectrumMapFFT = fftSize
	a.spectrumMapRate = rate
	a.spectrumMapTilt = a.spectrumTilt
	a.spectrumMapBands = bands
}

func (a *App) updateSpectrumFrame() ([waveformBandCount]float32, bool) {
	if a.spectrumMode == spectrumModeFreeze {
		return [waveformBandCount]float32{}, false
	}
	fftSize := a.spectrumFFTSize()
	bands := a.spectrumBandCount()
	acquireEvery := uint64(1)
	if fftSize >= 8192 {
		acquireEvery = 2
	}
	var waveformBands [waveformBandCount]float32
	acquired := false
	if a.tickCount%acquireEvery == 0 {
		fft := a.engine.FFT(fftSize)
		rate, _ := a.engine.AnalysisInfo()
		waveformBands = waveformBandsFromFFT(fft, rate)
		acquired = true
		a.ensureSpectrumMap(fftSize, rate)
		for band := 0; band < bands; band++ {
			b0, b1 := a.spectrumBinStart[band], a.spectrumBinEnd[band]
			if b0 < 0 {
				b0 = 0
			}
			if b1 >= len(fft) {
				b1 = len(fft) - 1
			}
			var mx float32
			for j := b0; j <= b1; j++ {
				if fft[j] > mx {
					mx = fft[j]
				}
			}
			db := ampToDB(mx) + a.spectrumTiltOffset[band]
			norm := (db + 90) / 90
			if norm < 0 {
				norm = 0
			} else if norm > 1 {
				norm = 1
			}
			a.spectrumTarget[band] = norm
		}
	}

	attack := []float32{0.82, 0.66, 0.48}[a.spectrumSmoothing]
	release := []float32{0.72, 0.86, 0.945}[a.spectrumSmoothing]
	for band := 0; band < bands; band++ {
		a.spectrum[band], a.spectrumHold[band], a.spectrumHoldAge[band] = updateSpectrumBand(
			a.spectrum[band], a.spectrumTarget[band], a.spectrumHold[band], a.spectrumHoldAge[band],
			attack, release, a.spectrumMode,
		)
	}
	return waveformBands, acquired
}

const (
	liveWaveformSeconds = 6
	liveWaveformFPS     = 60
	liveWaveformPoints  = liveWaveformSeconds * liveWaveformFPS
)

func (a *App) resetLiveWaveform() {
	if cap(a.liveWaveform) < liveWaveformPoints {
		a.liveWaveform = make([]WavePoint, liveWaveformPoints)
	} else {
		a.liveWaveform = a.liveWaveform[:liveWaveformPoints]
		clear(a.liveWaveform)
	}
	a.liveWaveSequence = a.engine.LiveWaveformSequence()
	a.liveWaveBands = [waveformBandCount]float32{}
	a.liveWaveChannels = 0
	if a.engine.InputActive() {
		_, channels := a.engine.AnalysisInfo()
		a.liveWaveChannels = int(channels)
		if a.liveWaveChannels < 1 {
			a.liveWaveChannels = 1
		}
	}
}

func appendLiveWavePoint(points []WavePoint, point WavePoint) {
	if len(points) == 0 {
		return
	}
	copy(points, points[1:])
	points[len(points)-1] = point
}

func (a *App) updateLiveWaveform(bands [waveformBandCount]float32) {
	point, sequence, ok := a.engine.LiveWaveformEnvelopeSince(a.liveWaveSequence)
	a.liveWaveSequence = sequence
	if !ok {
		return
	}
	peak := maxf(maxf(absf(point.MinL), absf(point.MaxL)), maxf(absf(point.MinR), absf(point.MaxR)))
	if peak >= 0.00001 {
		point.Bands = bands
	}
	appendLiveWavePoint(a.liveWaveform, point)
}

func updateSpectrumBand(cur, target, hold float32, holdAge uint16, attack, release float32, mode spectrumDisplayMode) (float32, float32, uint16) {
	if target > cur {
		cur += (target - cur) * attack
	} else if mode != spectrumModeHold {
		cur = target + (cur-target)*release
	}
	if cur < 0.0001 {
		cur = 0
	}

	// Manual HOLD is an accumulating maximum display: both the live trace and
	// the peak contour can rise, but neither may fall until HOLD is released.
	if cur >= hold {
		hold = cur
		holdAge = 0
	} else if mode == spectrumModeHold {
		holdAge = 0
	} else {
		// Normal Peak Hold: latch new peaks, hold for about 1.5 seconds at the
		// ~60 fps render cadence, then release slowly (~7 dB/sec).
		if holdAge < 65535 {
			holdAge++
		}
		if holdAge > 94 {
			hold -= 0.0013
			if hold < cur {
				hold = cur
			}
		}
	}
	return cur, hold, holdAge
}

func (a *App) appendLoudnessSample() {
	a.loudnessElapsed = a.historyClock.elapsed
	sample := LoudnessSample{
		Time:       a.loudnessElapsed,
		Momentary:  a.lufsM,
		ShortTerm:  a.lufsS,
		Integrated: a.lufsI,
		Segment:    a.loudnessSegment,
	}
	a.loudnessHistory = append(a.loudnessHistory, sample)

	// Cap at six hours at roughly 10 samples per second.
	const maxSamples = 6 * 60 * 60 * 10
	if len(a.loudnessHistory) > maxSamples {
		drop := len(a.loudnessHistory) - maxSamples
		copy(a.loudnessHistory, a.loudnessHistory[drop:])
		a.loudnessHistory = a.loudnessHistory[:maxSamples]
		if len(a.loudnessHistory) > 0 && len(a.loudnessLoopMarkers) > 0 {
			cutoff := a.loudnessHistory[0].Time
			k := 0
			for k < len(a.loudnessLoopMarkers) && a.loudnessLoopMarkers[k] < cutoff {
				k++
			}
			if k > 0 {
				copy(a.loudnessLoopMarkers, a.loudnessLoopMarkers[k:])
				a.loudnessLoopMarkers = a.loudnessLoopMarkers[:len(a.loudnessLoopMarkers)-k]
			}
		}
	}
}

func (a *App) markLoudnessDiscontinuity() {
	a.loudnessSegment++
	a.historyClock.pending = 0
	a.stereoNormalizeGain = 1
	a.stereoPhosphor.reset()
	a.resetLiveSpectrogram()
}

func (a *App) loudnessTarget() float32 {
	switch a.loudnessTargetMode {
	case 1:
		return -16
	case 2:
		return -23
	case 3:
		return a.loudnessCustomTarget
	default:
		return -14
	}
}

func (a *App) loudnessTargetLabel() string {
	if a.loudnessTargetMode == 3 {
		return fmt.Sprintf("T %.1f*", a.loudnessCustomTarget)
	}
	return fmt.Sprintf("T %.0f", a.loudnessTarget())
}

func (a *App) cycleLoudnessTarget() {
	a.loudnessTargetMode = (a.loudnessTargetMode + 1) % 4
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
}

func (a *App) adjustCustomLoudnessTarget(delta int32) {
	if delta == 0 {
		return
	}
	a.loudnessTargetMode = 3
	step := float32(0.5)
	if delta < 0 {
		step = -0.5
	}
	a.loudnessCustomTarget += step
	if a.loudnessCustomTarget > -8 {
		a.loudnessCustomTarget = -8
	}
	if a.loudnessCustomTarget < -36 {
		a.loudnessCustomTarget = -36
	}
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
}

func (a *App) setLoudnessWindow(mode int) {
	if mode < 0 || mode > 4 {
		return
	}
	a.loudnessWindow = mode
	a.loudnessHZoom = 1
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
}

func (a *App) loudnessWindowSeconds() float64 {
	switch a.loudnessWindow {
	case 0:
		return 5
	case 1:
		return 15
	case 2:
		return 30
	case 4:
		if a.loudnessElapsed > 10 {
			return a.loudnessElapsed
		}
		return 10
	default:
		return 60
	}
}

func (a *App) loudnessViewRange() (float64, float64) {
	// X axis is playback-session time, so looped passes append to the right
	// instead of reusing the source file's 0..duration coordinates.
	pos := a.loudnessRenderElapsed
	if pos < a.loudnessElapsed {
		pos = a.loudnessElapsed
	}
	if pos < 0 {
		pos = 0
	}
	baseWindow := a.loudnessWindowSeconds()
	zoom := a.loudnessHZoom
	if zoom < 1 {
		zoom = 1
	}
	window := baseWindow / zoom
	if window < 1 {
		window = 1
	}
	if a.loudnessWindow == 4 && zoom <= 1.0001 {
		end := math.Max(10, pos)
		return 0, end
	}
	end := pos
	if end < window {
		end = window
	}
	start := end - window
	if start < 0 {
		start = 0
	}
	return start, end
}

func (a *App) adjustLoudnessHorizontalZoom(delta int32) {
	if delta == 0 {
		return
	}
	steps := math.Abs(float64(delta) / 120.0)
	if steps < 1 {
		steps = 1
	}
	factor := math.Pow(1.22, steps)
	if delta > 0 {
		a.loudnessHZoom *= factor
	} else {
		a.loudnessHZoom /= factor
	}
	base := a.loudnessWindowSeconds()
	maxZoom := base
	if maxZoom < 1 {
		maxZoom = 1
	}
	if maxZoom > 64 {
		maxZoom = 64
	}
	if a.loudnessHZoom < 1 {
		a.loudnessHZoom = 1
	}
	if a.loudnessHZoom > maxZoom {
		a.loudnessHZoom = maxZoom
	}
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
}

func (a *App) adjustLoudnessVerticalZoom(delta, mouseY int32) {
	if delta == 0 {
		return
	}
	plot := loudnessPlotRect(a.layout.Loudness)
	if rectH(plot) <= 0 {
		return
	}
	minV, maxV := a.loudnessYMin, a.loudnessYMax
	if maxV-minV < 6 || minV < -60 || maxV > 0 {
		minV, maxV = -60, 0
	}
	ratio := float32(mouseY-plot.Top) / float32(rectH(plot))
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	anchor := maxV - ratio*(maxV-minV)
	span := maxV - minV
	steps := float32(math.Abs(float64(delta) / 120.0))
	if steps < 1 {
		steps = 1
	}
	factor := float32(math.Pow(1.22, float64(steps)))
	newSpan := span
	if delta > 0 {
		newSpan = span / factor
	} else {
		newSpan = span * factor
	}
	if newSpan < 8 {
		newSpan = 8
	}
	if newSpan > 60 {
		newSpan = 60
	}
	newMax := anchor + ratio*newSpan
	newMin := newMax - newSpan
	if newMax > 0 {
		shift := newMax
		newMax -= shift
		newMin -= shift
	}
	if newMin < -60 {
		shift := -60 - newMin
		newMin += shift
		newMax += shift
	}
	a.loudnessYMin, a.loudnessYMax = newMin, newMax
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyLoudness, a.layout.Loudness)
}

func (a *App) spectrumFFTSize() int {
	return []int{2048, 4096, 8192, 16384}[a.spectrumFFT]
}

func (a *App) spectrumFFTLabel() string {
	return []string{"FFT 2K", "FFT 4K", "FFT 8K", "FFT 16K"}[a.spectrumFFT]
}

func (a *App) spectrumBandCount() int {
	return []int{96, 256, 384}[a.spectrumBandMode]
}

func (a *App) spectrumBandLabel() string {
	return fmt.Sprintf("BAND %d", a.spectrumBandCount())
}

func (a *App) spectrumRelease() float32 {
	return []float32{0.84, 0.91, 0.965}[a.spectrumSmoothing]
}

func (a *App) spectrumSmoothRadius() int {
	return []int{1, 2, 4}[a.spectrumSmoothing]
}

func (a *App) spectrumSmoothLabel() string {
	return []string{"FAST", "MED", "SLOW"}[a.spectrumSmoothing]
}

func (a *App) spectrumTiltDB() float64 {
	return []float64{0, 3, 4.5}[a.spectrumTilt]
}

func (a *App) spectrumTiltLabel() string {
	return []string{"TILT 0", "TILT 3", "TILT 4.5"}[a.spectrumTilt]
}

func (a *App) stereoModeLabel() string {
	return []string{"GONIO", "LISSA", "POLAR"}[a.stereoMode]
}

func (a *App) stereoStyleLabel() string {
	return []string{"LINE", "DOT", "RAY"}[a.stereoStyle]
}

func (a *App) resetSpectrumState() {
	for i := range a.spectrum {
		a.spectrum[i] = 0
		a.spectrumTarget[i] = 0
		a.spectrumHold[i] = 0
		a.spectrumHoldAge[i] = 0
	}
	a.spectrumMapFFT = 0
	a.spectrumMapRate = 0
	a.spectrumMapTilt = -1
	a.spectrumMapBands = 0
}

func (a *App) cycleSpectrumFFT() {
	a.spectrumFFT = (a.spectrumFFT + 1) % 4
	a.resetSpectrumState()
	if a.analyzerView == analyzerViewLiveSpectrogram {
		a.resetLiveSpectrogram()
	}
	a.saveLayoutConfig()
}

func (a *App) cycleSpectrumSmoothing() {
	a.spectrumSmoothing = (a.spectrumSmoothing + 1) % 3
	a.resetSpectrumState()
	a.saveLayoutConfig()
}

func (a *App) cycleSpectrumTilt() {
	a.spectrumTilt = (a.spectrumTilt + 1) % 3
	a.resetSpectrumState()
	a.saveLayoutConfig()
}

func (a *App) cycleSpectrumBands() {
	a.spectrumBandMode = (a.spectrumBandMode + 1) % 3
	a.resetSpectrumState()
	a.saveLayoutConfig()
}

func (a *App) analyzerViewLabel() string {
	if a.analyzerView == analyzerViewSpectrum {
		return "SPECTRUM"
	}
	return "SPECTROGRAM"
}

func nextAnalyzerView(current analyzerDisplayView) analyzerDisplayView {
	if current == analyzerViewSpectrum {
		return analyzerViewLiveSpectrogram
	}
	return analyzerViewSpectrum
}

func (a *App) cycleAnalyzerView() {
	a.analyzerView = nextAnalyzerView(a.analyzerView)
	a.inputForcedAnalyzer = false
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) spectrogramTimeSeconds() float64 {
	return []float64{3, 6, 12, 30}[a.spectrogramTimeWindow]
}

func (a *App) spectrogramScaleLabel() string {
	switch a.spectrogramScale {
	case spectrogramScaleLog:
		return "FREQ LOG"
	case spectrogramScaleLinear:
		return "FREQ LINEAR"
	default:
		return "FREQ MEL"
	}
}

func (a *App) cycleSpectrogramScale() {
	a.spectrogramScale = spectrogramFrequencyScale((int(a.spectrogramScale) + 1) % 3)
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) spectrogramDataMaxFrequency() float64 {
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
	return math.Min(20000, float64(rate)*0.5)
}

func (a *App) spectrogramFrequencyView(maxFreq float64) (float64, float64) {
	return normalizedSpectrogramFrequencyRange(a.spectrogramFreqMin, a.spectrogramFreqMax, maxFreq)
}

func (a *App) spectrogramFrequencyRulerRect() RECT {
	inner := inflateRect(analyzerPlotRect(a.layout.Spectrum), -4, -4)
	return RECT{Left: a.layout.Spectrum.Left, Top: inner.Top, Right: inner.Left, Bottom: inner.Bottom}
}

func (a *App) adjustSpectrogramFrequencyZoom(delta, y int32) {
	inner := inflateRect(analyzerPlotRect(a.layout.Spectrum), -4, -4)
	anchor := 1 - float64(y-inner.Top)/float64(maxi32(rectH(inner), 1))
	steps := math.Abs(float64(delta) / 120)
	if steps < 1 {
		steps = 1
	}
	factor := math.Pow(0.8, steps)
	if delta < 0 {
		factor = math.Pow(1.25, steps)
	}
	maxFreq := a.spectrogramDataMaxFrequency()
	minFreq, viewMax := a.spectrogramFrequencyView(maxFreq)
	a.spectrogramFreqMin, a.spectrogramFreqMax = zoomSpectrogramFrequencyRange(a.spectrogramScale, minFreq, viewMax, maxFreq, anchor, factor)
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) panSpectrogramFrequency(y int32) {
	ruler := a.spectrogramFrequencyRulerRect()
	delta := y - a.spectrogramFreqDragY
	a.spectrogramFreqDragY = y
	if delta == 0 {
		return
	}
	maxFreq := a.spectrogramDataMaxFrequency()
	minFreq, viewMax := a.spectrogramFrequencyView(maxFreq)
	a.spectrogramFreqMin, a.spectrogramFreqMax = panSpectrogramFrequencyRange(a.spectrogramScale, minFreq, viewMax, maxFreq, float64(delta)/float64(maxi32(rectH(ruler), 1)))
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) resetSpectrogramFrequencyView() {
	a.spectrogramFreqMin, a.spectrogramFreqMax = 0, 0
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) spectrogramTimeLabel() string {
	return fmt.Sprintf("TIME %.0fs", a.spectrogramTimeSeconds())
}

func (a *App) cycleSpectrogramTime() {
	a.spectrogramTimeWindow = (a.spectrogramTimeWindow + 1) % 4
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) spectrogramRangeLabel() string {
	return fmt.Sprintf("RANGE %d", a.spectrogramRange)
}

func (a *App) cycleSpectrogramRange() {
	switch a.spectrogramRange {
	case 60:
		a.spectrogramRange = 90
	case 90:
		a.spectrogramRange = 120
	default:
		a.spectrogramRange = 60
	}
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) resetLiveSpectrogram() {
	a.liveSpectrogram.reset()
}

func (a *App) syncAnalyzerForInput() {
	if a.engine.InputActive() {
		if a.analyzerView == analyzerViewTrackSpectrogram {
			a.analyzerViewBeforeInput = a.analyzerView
			a.analyzerView = analyzerViewLiveSpectrogram
			a.inputForcedAnalyzer = true
		}
		return
	}
	if a.inputForcedAnalyzer {
		a.analyzerView = a.analyzerViewBeforeInput
		a.inputForcedAnalyzer = false
		if a.analyzerView == analyzerViewTrackSpectrogram {
			a.ensureTrackSpectrogram()
		}
	}
}

func nextSpectrumMode(current, requested spectrumDisplayMode) spectrumDisplayMode {
	if current == requested {
		return spectrumModeNormal
	}
	return requested
}

func (a *App) resetSpectrumHoldAges() {
	for i := range a.spectrumHoldAge {
		a.spectrumHoldAge[i] = 0
	}
}

func (a *App) toggleSpectrumMode(requested spectrumDisplayMode) {
	previous := a.spectrumMode
	a.spectrumMode = nextSpectrumMode(a.spectrumMode, requested)
	if previous == spectrumModeHold && a.spectrumMode != spectrumModeHold {
		// Give the normal Peak Hold contour its complete delay after leaving the
		// indefinite accumulating mode instead of inheriting stale ages.
		a.resetSpectrumHoldAges()
	}
	a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
}

func (a *App) cycleStereoMode() {
	a.stereoMode = (a.stereoMode + 1) % 3
	a.stereoPhosphor.reset()
	a.saveLayoutConfig()
}

func (a *App) cycleStereoStyle() {
	a.stereoStyle = (a.stereoStyle + 1) % 3
	a.stereoPhosphor.reset()
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyStereo, a.layout.Stereo)
}

func (a *App) toggleStereoNormalize() {
	a.stereoNormalize = !a.stereoNormalize
	a.stereoNormalizeGain = 1
	a.stereoPhosphor.reset()
	a.saveLayoutConfig()
	a.invalidateDynamic(dirtyStereo, a.layout.Stereo)
}

func updateVUHold(hold *float32, age *uint16, peak float32) {
	if peak >= *hold {
		*hold = peak
		*age = 0
		return
	}
	if *age < 65535 {
		*age++
	}
	// Hold for roughly 700 ms at the 8 ms UI cadence, then release smoothly.
	if *age > 88 {
		*hold *= 0.9890
		if *hold < peak {
			*hold = peak
		}
		if *hold < 0.000001 {
			*hold = 0
		}
	}
}

func safeMeter(v float32) float32 {
	if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
		return -120
	}
	if v < -120 {
		return -120
	}
	if v > 24 {
		return 24
	}
	return v
}
func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func (a *App) openFilesDialog() {
	buf := make([]uint16, 65536)
	filter := multiString(tr("音频文件"), "*.wav;*.wave;*.aif;*.aiff;*.flac;*.mp3;*.ogg;*.m4a;*.aac;*.wma;*.opus", tr("所有文件"), "*.*")
	ofn := OPENFILENAME{LStructSize: uint32(unsafe.Sizeof(OPENFILENAME{})), HwndOwner: a.hwnd, LpstrFilter: &filter[0], LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), Flags: OFN_EXPLORER | OFN_ALLOWMULTISELECT | OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_HIDEREADONLY, LpstrTitle: utf16Ptr(tr("选择音频文件"))}
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return
	}
	parts := parseMultiSelect(buf)
	a.addFiles(parts, true)
}

func multiString(parts ...string) []uint16 {
	out := make([]uint16, 0, 256)
	for _, part := range parts {
		u := syscall.StringToUTF16(part)
		out = append(out, u...)
	}
	out = append(out, 0)
	return out
}

func parseMultiSelect(buf []uint16) []string {
	var ss []string
	start := 0
	for i, v := range buf {
		if v == 0 {
			if i == start {
				break
			}
			ss = append(ss, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	if len(ss) <= 1 {
		return ss
	}
	dir := ss[0]
	out := make([]string, 0, len(ss)-1)
	for _, n := range ss[1:] {
		out = append(out, filepath.Join(dir, n))
	}
	return out
}

func (a *App) addFiles(paths []string, autoPlay bool) {
	a.addFilesToPlaylist(a.activePlaylistID, paths, autoPlay)
}

func (a *App) addExternalFiles(paths []string) {
	a.activePlaylistID = defaultPlaylistID
	a.addFilesToPlaylist(defaultPlaylistID, paths, true)
}

func (a *App) addFilesToPlaylist(playlistID string, paths []string, autoPlay bool) {
	p := a.playlistByID(playlistID)
	if p == nil {
		return
	}
	a.playlistSel.reset()
	valid := make([]string, 0, len(paths))
	for _, path := range paths {
		track, ok := trackFromPath(path)
		if !ok {
			continue
		}
		valid = append(valid, track.Path)
	}
	if len(valid) == 0 {
		a.setStatus("No supported audio files found.", true)
		return
	}

	firstRequested := -1
	firstNew := -1
	changed := false
	for _, path := range valid {
		idx := -1
		for i, t := range p.Tracks {
			if strings.EqualFold(t.Path, path) {
				idx = i
				break
			}
		}
		if idx < 0 {
			track, _ := trackFromPath(path)
			idx = len(p.Tracks)
			p.Tracks = append(p.Tracks, track)
			changed = true
			if firstNew < 0 {
				firstNew = idx
			}
		}
		if firstRequested < 0 {
			firstRequested = idx
		}
	}
	if firstRequested < 0 {
		return
	}

	p.Selected = firstRequested
	if autoPlay {
		a.loadTrackFromPlaylist(playlistID, firstRequested, true)
	} else if a.current < 0 {
		target := firstNew
		if target < 0 {
			target = firstRequested
		}
		a.loadTrackFromPlaylist(playlistID, target, true)
	}
	if changed {
		a.savePlaylistState()
	}
	invalidate(a.hwnd)
}
func isAudioFile(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".wav", ".wave", ".aif", ".aiff", ".flac", ".mp3", ".ogg", ".m4a", ".aac", ".wma", ".opus":
		return true
	}
	return false
}

func (a *App) loadTrack(index int, play bool) {
	a.loadTrackFromPlaylist(a.activePlaylistID, index, play)
}

func (a *App) loadTrackFromPlaylist(playlistID string, index int, play bool) {
	if !a.engineReady.Load() {
		a.setStatus("The audio engine is not ready.", true)
		return
	}
	p := a.playlistByID(playlistID)
	if p == nil || index < 0 || index >= len(p.Tracks) {
		return
	}
	path := p.Tracks[index].Path
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		p.Tracks[index].Missing = true
		a.setStatus("File unavailable: "+path, true)
		a.savePlaylistState()
		invalidate(a.hwnd)
		return
	}
	var openErr error
	if play {
		openErr = a.engine.ReplaceAndPlay(path, a.loop)
	} else {
		openErr = a.engine.Open(path, a.loop)
	}
	if openErr != nil {
		a.setStatus(openErr.Error(), true)
		return
	}
	p.Tracks[index].Missing = false
	a.playbackPlaylistID = playlistID
	a.current = index
	a.restoreCurrentPath = ""
	p.Selected = index
	a.manualStop = false
	a.peakL = 0
	a.peakR = 0
	a.rmsL = 0
	a.rmsR = 0
	a.clips = 0
	a.lufsM = -120
	a.lufsS = -120
	a.lufsI = -120
	a.lra = 0
	a.truePeak = -120
	a.meterDisplayInit = false
	a.loudnessHistory = nil
	a.loudnessLoopMarkers = nil
	a.loudnessLabelInit = false
	a.loudnessElapsed = 0
	a.loudnessRenderElapsed = 0
	a.historyClock = historyClock{}
	if play {
		a.historyClock.step(time.Now(), true)
	}
	a.loudnessSegment = 0
	a.stereoPointCount = 0
	a.stereoNormalizeGain = 1
	a.stereoPhosphor.reset()
	a.correlation = 0
	a.correlationDisplay = 0
	a.resetSpectrumState()
	a.startWaveform(path)
	rate, ch := a.engine.StreamInfo()
	a.setStatus(fmt.Sprintf("%s · %s · %d Hz · %d ch", filepath.Base(path), strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), ".")), rate, ch), false)
	setWindowText(a.hwnd, appTitle+" — "+filepath.Base(path))
	a.savePlaylistState()
}

type waveformCache struct {
	Version  int         `json:"version"`
	Bins     int         `json:"bins"`
	Channels int         `json:"channels"`
	Points   []WavePoint `json:"points"`
}

func waveformCachePath(path string, bins int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	seed := fmt.Sprintf("v4|%s|%d|%d|%d", strings.ToLower(filepath.Clean(path)), info.Size(), info.ModTime().UnixNano(), bins)
	sum := sha256.Sum256([]byte(seed))
	dir := filepath.Join(appDataDir(), "waveforms")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%x.json", sum[:16])), nil
}

func loadWaveformCache(path string, bins int) (WaveformData, bool) {
	cachePath, err := waveformCachePath(path, bins)
	if err != nil {
		return WaveformData{}, false
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return WaveformData{}, false
	}
	waveform, ok := decodeWaveformCache(data, bins)
	if !ok {
		_ = os.Remove(cachePath)
	}
	return waveform, ok
}

func decodeWaveformCache(data []byte, bins int) (WaveformData, bool) {
	var cache waveformCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != 4 || cache.Bins != bins || cache.Channels < 1 || len(cache.Points) < 256 {
		return WaveformData{}, false
	}
	return WaveformData{Channels: cache.Channels, Points: cache.Points}, true
}

func saveWaveformCache(path string, bins int, waveform WaveformData) {
	if waveform.Channels < 1 || len(waveform.Points) < 256 {
		return
	}
	cachePath, err := waveformCachePath(path, bins)
	if err != nil {
		return
	}
	data, err := json.Marshal(waveformCache{Version: 4, Bins: bins, Channels: waveform.Channels, Points: waveform.Points})
	if err != nil {
		return
	}
	tmp := cachePath + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		_ = os.Rename(tmp, cachePath)
	}
	pruneWaveformCache(filepath.Dir(cachePath), 96)
}

func pruneWaveformCache(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	type cacheFile struct {
		path string
		time time.Time
	}
	files := make([]cacheFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		if info, err := entry.Info(); err == nil {
			files = append(files, cacheFile{filepath.Join(dir, entry.Name()), info.ModTime()})
		}
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].time.Before(files[j].time) })
	for i := 0; i < len(files)-keep; i++ {
		_ = os.Remove(files[i].path)
	}
}

func (a *App) startWaveform(path string) {
	const waveformBins = 12000
	token := atomic.AddUint64(&a.waveToken, 1)
	spectrogramToken := atomic.AddUint64(&a.spectrogramToken, 1)
	_, streamChannels := a.engine.StreamInfo()
	a.waveMu.Lock()
	a.waveform = nil
	a.waveChannels = int(streamChannels)
	a.waveProgress = 0
	a.waveLoading = true
	a.waveCacheHit = false
	a.waveZoom = 1
	a.waveOffset = 0
	a.waveMu.Unlock()
	a.spectrogramMu.Lock()
	a.trackSpectrogram = SpectrogramData{}
	a.spectrogramLoading = true
	a.spectrogramProgress = 0
	a.spectrogramError = ""
	a.spectrogramMu.Unlock()
	a.resetLiveSpectrogram()
	go func() {
		cachedSpectrogram, spectrogramCacheHit := loadSpectrogramCache(path)
		if waveform, ok := loadWaveformCache(path, waveformBins); ok {
			if atomic.LoadUint64(&a.waveToken) != token {
				return
			}
			a.waveMu.Lock()
			a.waveform = waveform.Points
			a.waveChannels = waveform.Channels
			a.waveLoading = false
			a.waveProgress = 1
			a.waveCacheHit = true
			a.waveMu.Unlock()
			a.spectrogramMu.Lock()
			if spectrogramCacheHit && atomic.LoadUint64(&a.spectrogramToken) == spectrogramToken {
				a.trackSpectrogram = cachedSpectrogram
			}
			a.spectrogramLoading = false
			a.spectrogramProgress = 1
			a.spectrogramMu.Unlock()
			postMessage(a.hwnd, msgWaveReady, 0, 0)
			return
		}
		waveform, spectrogram, err := a.engine.GenerateTrackVisuals(path, waveformBins, func(p float64) {
			if atomic.LoadUint64(&a.waveToken) != token {
				return
			}
			a.waveMu.Lock()
			a.waveProgress = p
			a.waveMu.Unlock()
			a.spectrogramMu.Lock()
			a.spectrogramProgress = p
			a.spectrogramMu.Unlock()
		})
		if atomic.LoadUint64(&a.waveToken) != token {
			return
		}
		a.waveMu.Lock()
		if err == nil {
			a.waveform = waveform.Points
			a.waveChannels = waveform.Channels
		}
		a.waveLoading = false
		a.waveProgress = 1
		a.waveMu.Unlock()
		a.spectrogramMu.Lock()
		if atomic.LoadUint64(&a.spectrogramToken) == spectrogramToken {
			if err == nil {
				a.trackSpectrogram = spectrogram
			} else {
				a.spectrogramError = err.Error()
			}
			a.spectrogramLoading = false
			a.spectrogramProgress = 1
		}
		a.spectrogramMu.Unlock()
		if err != nil {
			a.setStatus("Audio opened, but waveform generation failed: "+err.Error(), true)
		} else {
			saveWaveformCache(path, waveformBins, waveform)
			saveSpectrogramCache(path, spectrogram)
		}
		postMessage(a.hwnd, msgWaveReady, 0, 0)
	}()
}

func (a *App) currentTrackPath() string {
	if p := a.playbackPlaylist(); p != nil && a.current >= 0 && a.current < len(p.Tracks) {
		return p.Tracks[a.current].Path
	}
	return ""
}

func (a *App) ensureTrackSpectrogram() {
	if a.engine.InputActive() {
		return
	}
	path := a.currentTrackPath()
	if path == "" {
		return
	}
	a.spectrogramMu.RLock()
	ready := len(a.trackSpectrogram.Values) > 0
	loading := a.spectrogramLoading
	a.spectrogramMu.RUnlock()
	if ready || loading {
		return
	}
	token := atomic.AddUint64(&a.spectrogramToken, 1)
	a.spectrogramMu.Lock()
	a.spectrogramLoading = true
	a.spectrogramProgress = 0
	a.spectrogramError = ""
	a.spectrogramMu.Unlock()
	go func() {
		if cached, ok := loadSpectrogramCache(path); ok {
			if atomic.LoadUint64(&a.spectrogramToken) != token {
				return
			}
			a.spectrogramMu.Lock()
			a.trackSpectrogram = cached
			a.spectrogramLoading = false
			a.spectrogramProgress = 1
			a.spectrogramMu.Unlock()
			postMessage(a.hwnd, msgWaveReady, 0, 0)
			return
		}
		_, spectrogram, err := a.engine.GenerateTrackVisuals(path, 12000, func(p float64) {
			if atomic.LoadUint64(&a.spectrogramToken) != token {
				return
			}
			a.spectrogramMu.Lock()
			a.spectrogramProgress = p
			a.spectrogramMu.Unlock()
		})
		if atomic.LoadUint64(&a.spectrogramToken) != token {
			return
		}
		a.spectrogramMu.Lock()
		if err == nil {
			a.trackSpectrogram = spectrogram
		} else {
			a.spectrogramError = err.Error()
		}
		a.spectrogramLoading = false
		a.spectrogramProgress = 1
		a.spectrogramMu.Unlock()
		if err == nil {
			saveSpectrogramCache(path, spectrogram)
		}
		postMessage(a.hwnd, msgWaveReady, 0, 0)
	}()
}

func (a *App) prevTrack() {
	p := a.playbackPlaylist()
	if a.current < 0 {
		p = a.activePlaylist()
	}
	if p == nil || len(p.Tracks) == 0 {
		return
	}
	idx := a.current - 1
	if idx < 0 {
		idx = len(p.Tracks) - 1
	}
	a.loadTrackFromPlaylist(p.ID, idx, true)
}
func (a *App) nextTrack(auto bool) {
	p := a.playbackPlaylist()
	if a.current < 0 {
		p = a.activePlaylist()
	}
	if p == nil || len(p.Tracks) == 0 {
		return
	}
	idx := a.current + 1
	if idx >= len(p.Tracks) {
		if auto {
			return
		}
		idx = 0
	}
	a.loadTrackFromPlaylist(p.ID, idx, true)
}

func (a *App) onPlaybackEnded(generation uint64, stream uint32) {
	if !a.engine.MatchesPlaybackEnd(generation, stream) {
		return
	}
	if a.loop && !a.manualStop && a.current >= 0 && !a.engine.InputActive() {
		a.historyClock.pending++
		return
	}
	if a.manualStop || a.current < 0 || a.loop || !a.continuousPlay {
		return
	}
	a.nextTrack(true)
}
func (a *App) togglePlayback() {
	if err := a.engine.PlayPause(); err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	if a.engine.IsPlaying() {
		a.manualStop = false
	}
}
func (a *App) clearCurrentVisualState() {
	atomic.AddUint64(&a.waveToken, 1)
	atomic.AddUint64(&a.spectrogramToken, 1)
	a.waveMu.Lock()
	a.waveform = nil
	a.waveProgress = 0
	a.waveLoading = false
	a.waveMu.Unlock()
	a.spectrogramMu.Lock()
	a.trackSpectrogram = SpectrogramData{}
	a.spectrogramLoading = false
	a.spectrogramProgress = 0
	a.spectrogramError = ""
	a.spectrogramMu.Unlock()
	a.resetLiveSpectrogram()
	a.peakL, a.peakR, a.rmsL, a.rmsR = 0, 0, 0, 0
	a.vuHoldL, a.vuHoldR = 0, 0
	a.vuHoldAgeL, a.vuHoldAgeR = 0, 0
	a.lufsM, a.lufsS, a.lufsI = -120, -120, -120
	a.lra, a.truePeak = 0, -120
	a.meterDisplayInit = false
	a.loudnessHistory = nil
	a.loudnessLoopMarkers = nil
	a.loudnessLabelInit = false
	a.loudnessElapsed = 0
	a.loudnessRenderElapsed = 0
	a.historyClock = historyClock{}
	a.loudnessSegment = 0
	a.stereoPointCount = 0
	a.stereoNormalizeGain = 1
	a.stereoPhosphor.reset()
	a.correlation, a.correlationDisplay = 0, 0
	a.resetSpectrumState()
}

func (a *App) removeTrackAt(index int) {
	p := a.activePlaylist()
	if p == nil || index < 0 || index >= len(p.Tracks) {
		return
	}
	a.playlistSel.reset()
	removedPath := p.Tracks[index].Path
	removedCurrent := p.ID == a.playbackPlaylistID && index == a.current
	removedRestoreContext := p.ID == a.playbackPlaylistID && a.restoreCurrentPath != "" && strings.EqualFold(removedPath, a.restoreCurrentPath)
	p.Tracks = append(p.Tracks[:index], p.Tracks[index+1:]...)
	if removedCurrent {
		if a.engineReady.Load() {
			a.engine.Stop()
		}
		a.current = -1
		a.clearCurrentVisualState()
		setWindowText(a.hwnd, appTitle)
	} else if p.ID == a.playbackPlaylistID && index < a.current {
		a.current--
	}
	if removedRestoreContext {
		a.restoreCurrentPath = ""
	}
	if len(p.Tracks) == 0 {
		p.Selected = -1
		p.Scroll = 0
	} else {
		if index < p.Selected {
			p.Selected--
		} else if p.Selected >= len(p.Tracks) {
			p.Selected = len(p.Tracks) - 1
		}
	}
	a.clampPlaylistScroll()
	a.savePlaylistState()
	invalidate(a.hwnd)
}

func (a *App) removeSelected() {
	rows := a.selectedRows()
	// Removing from the tail keeps every remaining index valid, and it reuses
	// removeTrackAt so playback and waveform side effects stay in one place.
	for i := len(rows) - 1; i >= 0; i-- {
		a.removeTrackAt(rows[i])
	}
}

func (a *App) clearPlaylist() {
	p := a.activePlaylist()
	if p == nil || len(p.Tracks) == 0 {
		return
	}
	a.playlistSel.reset()
	if messageBoxResult(a.hwnd, tr("确定要清空 Playlist “")+p.Name+tr("” 吗？"), "Auralux Player", MB_YESNO|MB_ICONQUESTION) != IDYES {
		return
	}
	clearsPlayback := p.ID == a.playbackPlaylistID
	if clearsPlayback && a.engineReady.Load() {
		a.engine.Stop()
	}
	p.Tracks = nil
	p.Selected = -1
	p.Scroll = 0
	if clearsPlayback {
		a.current = -1
		a.restoreCurrentPath = ""
		a.clearCurrentVisualState()
		setWindowText(a.hwnd, appTitle)
	}
	a.setStatus("Playlist cleared.", false)
	a.savePlaylistState()
	invalidate(a.hwnd)
}

func (a *App) moveTrack(from, to int) {
	p := a.activePlaylist()
	if p == nil || from < 0 || from >= len(p.Tracks) || to < 0 || to >= len(p.Tracks) || from == to {
		return
	}
	a.playlistSel.reset()
	currentPath := ""
	if p.ID == a.playbackPlaylistID && a.current >= 0 && a.current < len(p.Tracks) {
		currentPath = p.Tracks[a.current].Path
	}
	track := p.Tracks[from]
	if from < to {
		copy(p.Tracks[from:to], p.Tracks[from+1:to+1])
	} else {
		copy(p.Tracks[to+1:from+1], p.Tracks[to:from])
	}
	p.Tracks[to] = track
	p.Selected = to
	a.playlistDragIndex = to
	if currentPath != "" {
		for i, t := range p.Tracks {
			if strings.EqualFold(t.Path, currentPath) {
				a.current = i
				break
			}
		}
	}
}

func (a *App) switchActivePlaylist(id string) {
	if a.playlistByID(id) == nil || id == a.activePlaylistID {
		return
	}
	a.playlistSel.reset()
	a.activePlaylistID = id
	a.clampPlaylistScroll()
	a.savePlaylistState()
	invalidate(a.hwnd)
}

func boundedPlaylistIndex(current, count, steps int) int {
	if count <= 0 {
		return -1
	}
	if current < 0 {
		current = 0
	}
	if current >= count {
		current = count - 1
	}
	next := current + steps
	if next < 0 {
		return 0
	}
	if next >= count {
		return count - 1
	}
	return next
}

func (a *App) switchPlaylistByWheel(delta int32) {
	if delta == 0 || len(a.playlists) < 2 {
		return
	}
	current := 0
	for i := range a.playlists {
		if a.playlists[i].ID == a.activePlaylistID {
			current = i
			break
		}
	}
	steps := int(delta / 120)
	if steps == 0 {
		if delta > 0 {
			steps = 1
		} else {
			steps = -1
		}
	}
	// Wheel up moves toward the first playlist; wheel down moves toward the last.
	next := boundedPlaylistIndex(current, len(a.playlists), -steps)
	if next != current {
		a.switchActivePlaylist(a.playlists[next].ID)
	}
}

func (a *App) createPlaylist() {
	initial := nextPlaylistName(a.playlists)
	name, ok := promptPlaylistName(a, tr("新建 Playlist"), initial, func(value string) string {
		_, reason := a.validatePlaylistName(value, "")
		return reason
	})
	if !ok {
		return
	}
	name, _ = a.validatePlaylistName(name, "")
	p := Playlist{ID: newPlaylistID(), Name: name, Selected: -1}
	a.playlists = append(a.playlists, p)
	a.activePlaylistID = p.ID
	a.savePlaylistState()
	a.setStatus("Created playlist: "+name, false)
	invalidate(a.hwnd)
}

func (a *App) renameActivePlaylist() {
	p := a.activePlaylist()
	if p == nil || p.ID == defaultPlaylistID {
		return
	}
	name, ok := promptPlaylistName(a, tr("重命名 Playlist"), p.Name, func(value string) string {
		_, reason := a.validatePlaylistName(value, p.ID)
		return reason
	})
	if !ok {
		return
	}
	name, _ = a.validatePlaylistName(name, p.ID)
	p.Name = name
	a.savePlaylistState()
	a.setStatus("Renamed playlist: "+name, false)
	invalidate(a.hwnd)
}

func (a *App) deleteActivePlaylist() {
	p := a.activePlaylist()
	if p == nil || p.ID == defaultPlaylistID {
		return
	}
	message := tr("确定要删除 Playlist “") + p.Name + tr("” 吗？\n列表中的文件不会从磁盘删除。")
	if p.ID == a.playbackPlaylistID {
		message = "Playlist “" + p.Name + tr("” 正在提供当前播放内容。\n删除后将停止播放，确定继续吗？")
	}
	if messageBoxResult(a.hwnd, message, "Auralux Player", MB_YESNO|MB_ICONQUESTION) != IDYES {
		return
	}
	deletedID := p.ID
	for i := range a.playlists {
		if a.playlists[i].ID == deletedID {
			a.playlists = append(a.playlists[:i], a.playlists[i+1:]...)
			break
		}
	}
	a.activePlaylistID = defaultPlaylistID
	if deletedID == a.playbackPlaylistID {
		if a.engineReady.Load() {
			a.engine.Stop()
		}
		a.playbackPlaylistID = defaultPlaylistID
		a.current = -1
		a.restoreCurrentPath = ""
		a.clearCurrentVisualState()
		setWindowText(a.hwnd, appTitle)
	}
	a.savePlaylistState()
	a.setStatus("Playlist deleted.", false)
	invalidate(a.hwnd)
}

func (a *App) showPlaylistSelectorMenu() {
	a.playlistMenuOpen = true
	current := 0
	for i := range a.playlists {
		if a.playlists[i].ID == a.activePlaylistID {
			current = i
			break
		}
	}
	geometry := a.playlistDropdownGeometry()
	a.playlistMenuScroll = current - geometry.VisiblePlaylists/2
	a.clampPlaylistMenuScroll(geometry.VisiblePlaylists)
	invalidateArea(a.hwnd, a.layout.Playlist)
}

func (a *App) handlePlaylistSelectorMenuClick(x, y int32) bool {
	if !a.playlistMenuOpen {
		return false
	}
	if pointInRect(x, y, a.layout.PlaylistSelector) {
		a.playlistMenuOpen = false
		invalidateArea(a.hwnd, a.layout.Playlist)
		return true
	}
	geometry := a.playlistDropdownGeometry()
	a.playlistMenuOpen = false
	invalidateArea(a.hwnd, a.layout.Playlist)
	if !pointInRect(x, y, geometry.Rect) {
		return true
	}
	if index := a.playlistDropdownIndexAt(x, y, geometry); index >= 0 {
		a.switchActivePlaylist(a.playlists[index].ID)
		return true
	}
	switch {
	case pointInRect(x, y, geometry.AddAudio):
		a.openFilesDialog()
	case pointInRect(x, y, geometry.Rename):
		if a.activePlaylistID != defaultPlaylistID {
			a.renameActivePlaylist()
		}
	case pointInRect(x, y, geometry.Delete):
		if a.activePlaylistID != defaultPlaylistID {
			a.deleteActivePlaylist()
		}
	}
	return true
}

func (a *App) onMouseDown(x, y int32) {
	l := a.layout
	if a.handlePlaylistSelectorMenuClick(x, y) {
		return
	}
	switch {
	case pointInRect(x, y, l.SplitPlaylist):
		a.dragMode = dragSplitPlaylistMode
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.SplitRight):
		a.dragMode = dragSplitRightMode
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.SplitLoudnessSpectrum):
		a.dragMode = dragSplitLoudnessMode
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.SplitMeterStereo):
		a.dragMode = dragSplitStereoMode
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.SplitPlaylistInfo):
		a.dragMode = dragSplitPlaylistInfoMode
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.PlaylistSelector):
		a.showPlaylistSelectorMenu()
	case pointInRect(x, y, l.OpenBtn):
		a.createPlaylist()
	case pointInRect(x, y, l.ClearPlaylistBtn):
		a.clearPlaylist()
	case pointInRect(x, y, l.PrevBtn):
		a.prevTrack()
	case pointInRect(x, y, l.PlayBtn):
		p := a.activePlaylist()
		if a.current < 0 && p != nil && len(p.Tracks) > 0 {
			a.loadTrack(maxi(p.Selected, 0), true)
		} else {
			a.togglePlayback()
		}
	case pointInRect(x, y, l.StopBtn):
		a.manualStop = true
		a.historyClock.pending = 0
		a.engine.Stop()
	case pointInRect(x, y, l.NextBtn):
		a.nextTrack(false)
	case pointInRect(x, y, l.LoopBtn):
		a.loop = !a.loop
		a.engine.SetLoop(a.loop)
	case pointInRect(x, y, l.PlayModeBtn):
		a.continuousPlay = !a.continuousPlay
		a.saveLayoutConfig()
	case pointInRect(x, y, l.ThemeBtn):
		a.toggleTheme()
	case pointInRect(x, y, logoMenuRect(l.Header)):
		a.helpMenu()
	case pointInRect(x, y, l.Loudness5Btn):
		a.setLoudnessWindow(0)
	case pointInRect(x, y, l.Loudness15Btn):
		a.setLoudnessWindow(1)
	case pointInRect(x, y, l.Loudness30Btn):
		a.setLoudnessWindow(2)
	case pointInRect(x, y, l.Loudness60Btn):
		a.setLoudnessWindow(3)
	case pointInRect(x, y, l.LoudnessFullBtn):
		a.setLoudnessWindow(4)
	case pointInRect(x, y, l.LoudnessTargetBtn):
		a.cycleLoudnessTarget()
	case pointInRect(x, y, l.AnalyzerViewBtn):
		a.cycleAnalyzerView()
	case a.analyzerView != analyzerViewSpectrum && pointInRect(x, y, l.SpectrogramScaleBtn):
		a.cycleSpectrogramScale()
	case a.analyzerView != analyzerViewTrackSpectrogram && pointInRect(x, y, l.SpectrumFFTBtn):
		a.cycleSpectrumFFT()
	case a.analyzerView == analyzerViewSpectrum && pointInRect(x, y, l.SpectrumSmoothBtn):
		a.cycleSpectrumSmoothing()
	case a.analyzerView == analyzerViewSpectrum && pointInRect(x, y, l.SpectrumTiltBtn):
		a.cycleSpectrumTilt()
	case a.analyzerView == analyzerViewSpectrum && pointInRect(x, y, l.SpectrumBandBtn):
		a.cycleSpectrumBands()
	case a.analyzerView == analyzerViewSpectrum && pointInRect(x, y, l.SpectrumHoldBtn):
		a.toggleSpectrumMode(spectrumModeHold)
	case a.analyzerView == analyzerViewSpectrum && pointInRect(x, y, l.SpectrumFreezeBtn):
		a.toggleSpectrumMode(spectrumModeFreeze)
	case a.analyzerView == analyzerViewLiveSpectrogram && pointInRect(x, y, l.SpectrogramTimeBtn):
		a.cycleSpectrogramTime()
	case a.analyzerView != analyzerViewSpectrum && pointInRect(x, y, l.SpectrogramRangeBtn):
		a.cycleSpectrogramRange()
	case a.analyzerView == analyzerViewLiveSpectrogram && pointInRect(x, y, l.SpectrogramFreezeBtn):
		a.liveSpectrogram.frozen = !a.liveSpectrogram.frozen
		a.invalidateDynamic(dirtySpectrum, a.layout.Spectrum)
	case a.analyzerView != analyzerViewSpectrum && pointInRect(x, y, a.spectrogramFrequencyRulerRect()):
		a.dragMode = dragSpectrogramFreqMode
		a.spectrogramFreqDragY = y
		procSetCapture.Call(a.hwnd)
	case pointInRect(x, y, l.StereoModeBtn):
		a.cycleStereoMode()
	case pointInRect(x, y, l.StereoStyleBtn):
		a.cycleStereoStyle()
	case pointInRect(x, y, l.StereoColorBtn):
		a.stereoPhosphor.blue = !a.stereoPhosphor.blue
		if a.stereoPhosphor.composedFrameValid && !a.stereoPhosphor.composedBlank {
			a.stereoPhosphor.renderStereoColors()
		}
		a.saveLayoutConfig()
		a.invalidateDynamic(dirtyStereo, a.layout.Stereo)
	case pointInRect(x, y, l.StereoNormalizeBtn):
		a.toggleStereoNormalize()
	case pointInRect(x, y, l.WaveZoomOutBtn):
		start, end := a.waveView()
		a.zoomWave(-1, start+(end-start)/2)
	case pointInRect(x, y, l.WaveFitBtn):
		a.waveZoom = 1
		a.waveOffset = 0
	case pointInRect(x, y, l.WaveZoomInBtn):
		start, end := a.waveView()
		a.zoomWave(1, start+(end-start)/2)
	case pointInRect(x, y, l.ResetBtn):
		a.engine.ResetMeters()
		a.peakL, a.peakR = 0, 0
		a.rmsL, a.rmsR = 0, 0
		a.vuHoldL, a.vuHoldR = 0, 0
		a.vuHoldAgeL, a.vuHoldAgeR = 0, 0
		a.clips = 0
		a.lufsM = -120
		a.lufsS = -120
		a.lufsI = -120
		a.lra = 0
		a.truePeak = -120
		a.meterDisplayInit = false
		a.loudnessHistory = nil
		a.loudnessLoopMarkers = nil
		a.loudnessElapsed = 0
		a.loudnessRenderElapsed = 0
		a.historyClock = historyClock{}
		a.loudnessSegment = 0
		a.correlation = 0
		a.correlationDisplay = 0
		a.stereoPointCount = 0
		a.stereoNormalizeGain = 1
		a.stereoPhosphor.reset()
		a.resetLiveSpectrogram()
	case pointInRect(x, y, l.DeviceBtn):
		a.showDeviceMenu()
	case pointInRect(x, y, l.InputBtn):
		a.showInputMenu()
	case pointInRect(x, y, l.Speaker):
		a.toggleMute()
	case pointInRect(x, y, l.Progress):
		a.markLoudnessDiscontinuity()
		a.dragMode = dragSeekMode
		a.dragSeekWave = false
		a.dragSeek = l.Progress
		procSetCapture.Call(a.hwnd)
		a.seekAt(x, a.dragSeek)
	case pointInRect(x, y, l.Wave):
		if a.engine.InputActive() {
			break
		}
		a.markLoudnessDiscontinuity()
		a.dragMode = dragSeekMode
		a.dragSeekWave = true
		a.dragSeek = a.wavePlotRect()
		procSetCapture.Call(a.hwnd)
		a.seekWaveAt(x)
	case a.analyzerView == analyzerViewTrackSpectrogram && pointInRect(x, y, analyzerPlotRect(l.Spectrum)):
		if a.engine.InputActive() || a.current < 0 {
			break
		}
		a.markLoudnessDiscontinuity()
		a.dragMode = dragSeekMode
		a.dragSeekTrack = true
		procSetCapture.Call(a.hwnd)
		a.seekSpectrogramAt(x)
	case pointInRect(x, y, l.Volume):
		a.dragMode = dragVolumeMode
		a.volumeDragStart = a.engine.Volume()
		if a.volumeDragStart > 0.001 {
			a.lastNonZeroVolume = a.volumeDragStart
		}
		procSetCapture.Call(a.hwnd)
		a.volumeAt(x)
	case a.playlistScrollbarHit(x, y):
		track, thumb, ok := a.playlistScrollRects()
		if ok {
			if pointInRect(x, y, inflateRect(thumb, 5, 2)) {
				a.dragMode = dragPlaylistScrollMode
				a.playlistScrollDragOffset = y - thumb.Top
				procSetCapture.Call(a.hwnd)
			} else {
				page := a.playlistVisibleRows()
				if page < 1 {
					page = 1
				}
				if p := a.activePlaylist(); p != nil {
					if y < thumb.Top {
						p.Scroll -= page
					} else if y > thumb.Bottom {
						p.Scroll += page
					}
				}
				a.clampPlaylistScroll()
			}
			_ = track
		}
	case pointInRect(x, y, l.Playlist):
		idx := a.playlistIndexAt(x, y)
		if idx >= 0 {
			p := a.activePlaylist()
			row := a.playlistRowRect(idx)
			if pointInRect(x, y, playlistRemoveRect(row)) {
				a.removeTrackAt(idx)
			} else if p != nil {
				a.applyPlaylistRowClick(p, idx)
				a.playlistDragIndex = idx
				a.playlistDragMoved = false
				a.dragMode = dragPlaylistTrackMode
				procSetCapture.Call(a.hwnd)
			}
		} else if a.playlistMultiSelectActive() {
			// Clicking the empty area under the rows drops a multi-selection, the
			// same way Explorer does. A single selection is left untouched, which
			// keeps this branch invisible to the historic behaviour.
			a.playlistSel.reset()
		}
	}
	invalidate(a.hwnd)
}

func (a *App) onMouseDrag(x, y int32) {
	switch a.dragMode {
	case dragSeekMode:
		if a.dragSeekTrack {
			a.seekSpectrogramAt(x)
		} else if a.dragSeekWave {
			a.seekWaveAt(x)
		} else {
			a.seekAt(x, a.dragSeek)
		}
	case dragVolumeMode:
		a.volumeAt(x)
	case dragSplitPlaylistMode:
		a.resizePlaylist(x)
	case dragSplitRightMode:
		a.resizeRight(x)
	case dragSplitLoudnessMode:
		a.resizeLoudness(y)
	case dragSplitStereoMode:
		a.resizeMeter(y)
	case dragSplitPlaylistInfoMode:
		a.resizeFileInfo(y)
	case dragPlaylistTrackMode:
		if a.tryStartPlaylistDragOut(x, y) {
			return
		}
		a.dragPlaylistAt(y)
	case dragPlaylistScrollMode:
		a.dragPlaylistScrollbar(y)
	case dragSpectrogramFreqMode:
		a.panSpectrogramFrequency(y)
	}
	invalidate(a.hwnd)
}

func (a *App) selectedFileInfoTrack() (*Track, bool) {
	if p := a.playbackPlaylist(); p != nil && a.current >= 0 && a.current < len(p.Tracks) {
		return &p.Tracks[a.current], true
	}
	if p := a.activePlaylist(); p != nil && p.Selected >= 0 && p.Selected < len(p.Tracks) {
		return &p.Tracks[p.Selected], false
	}
	return nil, false
}

func escapePowerShellSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (a *App) copySelectedFilePath() {
	track, _ := a.selectedFileInfoTrack()
	if track == nil {
		a.setStatus("No file paths to copy.", true)
		return
	}
	path := track.Path
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "Set-Clipboard -Value '"+escapePowerShellSingleQuoted(path)+"'")
	if err := cmd.Run(); err != nil {
		a.setStatus("Could not copy path: "+err.Error(), true)
		return
	}
	a.setStatus("File path copied.", false)
}

func (a *App) openTrackFolder(idx int) {
	p := a.activePlaylist()
	if p == nil || idx < 0 || idx >= len(p.Tracks) {
		a.setStatus("No file location to open.", true)
		return
	}
	dir := filepath.Dir(p.Tracks[idx].Path)
	cmd := exec.Command("explorer.exe", dir)
	if err := cmd.Start(); err != nil {
		a.setStatus("Could not open folder: "+err.Error(), true)
		return
	}
	a.setStatus("Opened containing folder.", false)
}

func (a *App) openSelectedFileFolder() {
	track, _ := a.selectedFileInfoTrack()
	if track == nil {
		a.setStatus("No file location to open.", true)
		return
	}
	dir := filepath.Dir(track.Path)
	cmd := exec.Command("explorer.exe", dir)
	if err := cmd.Start(); err != nil {
		a.setStatus("Could not open folder: "+err.Error(), true)
		return
	}
	a.setStatus("Opened containing folder.", false)
}

func (a *App) resizePlaylist(x int32) {
	w := x - a.layout.Content.Left
	if w < 210 {
		w = 210
	}
	maxW := rectW(a.layout.Content) - a.rightWidth - 520
	if maxW > 520 {
		maxW = 520
	}
	if maxW < 210 {
		maxW = 210
	}
	if w > maxW {
		w = maxW
	}
	a.playlistWidth = w
}

func (a *App) resizeRight(x int32) {
	w := a.layout.Content.Right - x
	if w < 360 {
		w = 360
	}
	maxW := rectW(a.layout.Content) - a.playlistWidth - 520
	if maxW > 620 {
		maxW = 620
	}
	if maxW < 360 {
		maxW = 360
	}
	if w > maxW {
		w = maxW
	}
	a.rightWidth = w
}

func (a *App) resizeLoudness(y int32) {
	height := y - a.layout.Loudness.Top
	if height < 110 {
		height = 110
	}
	maxHeight := a.layout.Center.Bottom - a.layout.Loudness.Top - 170 - rectH(a.layout.SplitLoudnessSpectrum)
	if height > maxHeight {
		height = maxHeight
	}
	a.loudnessHeight = height
}

func (a *App) resizeMeter(y int32) {
	total := rectH(a.layout.Right)
	s := rectH(a.layout.SplitMeterStereo)
	minMeter := int32(286)
	minStereo := int32(180)
	meterH := y - a.layout.Right.Top
	if meterH < minMeter {
		meterH = minMeter
	}
	if meterH > total-minStereo-s {
		meterH = total - minStereo - s
	}
	a.meterHeight = meterH
}

func (a *App) resizeFileInfo(y int32) {
	height := a.layout.Content.Bottom - y
	if height < 140 {
		height = 140
	}
	maxHeight := rectH(a.layout.Content) - rectH(a.layout.Header) - rectH(a.layout.SplitPlaylistInfo) - 180
	if maxHeight < 140 {
		maxHeight = 140
	}
	if height > maxHeight {
		height = maxHeight
	}
	a.fileInfoHeight = height
}

func (a *App) wavePlotRect() RECT {
	r := a.layout.Wave
	// Keep a small left gutter for the -∞ reference label. The L/R badges and
	// visible zoom controls are hidden, so the waveform can use more width.
	return RECT{r.Left + 42, r.Top + 34, r.Right - 14, r.Bottom - 24}
}

func (a *App) waveView() (float64, float64) {
	zoom := a.waveZoom
	if zoom < 1 {
		zoom = 1
	}
	span := 1.0 / float64(zoom)
	maxStart := 1.0 - span
	start := a.waveOffset
	if start < 0 {
		start = 0
	}
	if start > maxStart {
		start = maxStart
	}
	return start, start + span
}

func (a *App) waveRatioAtX(x int32) float64 {
	plot := a.wavePlotRect()
	local := float64(x-plot.Left) / float64(maxi32(rectW(plot), 1))
	if local < 0 {
		local = 0
	}
	if local > 1 {
		local = 1
	}
	start, end := a.waveView()
	return start + local*(end-start)
}

func (a *App) spectrogramRatioAtX(x int32) float64 {
	plot := inflateRect(analyzerPlotRect(a.layout.Spectrum), -4, -4)
	local := float64(x-plot.Left) / float64(maxi32(rectW(plot), 1))
	if local < 0 {
		local = 0
	} else if local > 1 {
		local = 1
	}
	start, end := a.waveView()
	return start + local*(end-start)
}

func (a *App) setWaveZoom(zoom int, anchorRatio float64) {
	allowed := []int{1, 2, 4, 8, 16}
	best := allowed[0]
	for _, z := range allowed {
		if z <= zoom {
			best = z
		}
	}
	if zoom >= allowed[len(allowed)-1] {
		best = allowed[len(allowed)-1]
	}
	oldStart, oldEnd := a.waveView()
	oldSpan := oldEnd - oldStart
	if anchorRatio < oldStart || anchorRatio > oldEnd {
		anchorRatio = oldStart + oldSpan/2
	}
	local := 0.5
	if oldSpan > 0 {
		local = (anchorRatio - oldStart) / oldSpan
	}
	newSpan := 1.0 / float64(best)
	newStart := anchorRatio - local*newSpan
	maxStart := 1.0 - newSpan
	if newStart < 0 {
		newStart = 0
	}
	if newStart > maxStart {
		newStart = maxStart
	}
	a.waveZoom = best
	a.waveOffset = newStart
}

func (a *App) zoomWave(step int, anchorRatio float64) {
	zoom := a.waveZoom
	if zoom < 1 {
		zoom = 1
	}
	if step > 0 {
		zoom *= 2
	} else {
		zoom /= 2
	}
	a.setWaveZoom(zoom, anchorRatio)
}

func (a *App) panWave(direction int) {
	if a.waveZoom <= 1 {
		return
	}
	span := 1.0 / float64(a.waveZoom)
	a.waveOffset += float64(direction) * span * 0.16
	maxStart := 1.0 - span
	if a.waveOffset < 0 {
		a.waveOffset = 0
	}
	if a.waveOffset > maxStart {
		a.waveOffset = maxStart
	}
}

func (a *App) seekWaveAt(x int32) {
	if a.current < 0 || a.engine.InputActive() {
		return
	}
	a.engine.SeekSeconds(a.engine.DurationSeconds() * a.waveRatioAtX(x))
}

func (a *App) seekSpectrogramAt(x int32) {
	if a.current < 0 || a.engine.InputActive() {
		return
	}
	a.engine.SeekSeconds(a.engine.DurationSeconds() * a.spectrogramRatioAtX(x))
}

func (a *App) seekAt(x int32, r RECT) {
	if a.current < 0 {
		return
	}
	ratio := float64(x-r.Left) / float64(maxi32(rectW(r), 1))
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	a.engine.SeekSeconds(a.engine.DurationSeconds() * ratio)
}

func (a *App) volumeAt(x int32) {
	r := a.layout.Volume
	ratio := float32(x-r.Left) / float32(maxi32(rectW(r), 1))
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	a.engine.SetVolume(ratio)
}

// Coalesce wheel and key repeats; normal close also flushes the layout.
func (a *App) setPlaybackVolume(v float32) {
	previous := a.engine.Volume()
	a.engine.SetVolume(v)
	v = a.engine.Volume()
	if v == previous {
		return
	}
	if v > 0.001 {
		a.lastNonZeroVolume = v
	} else if previous > 0.001 {
		a.lastNonZeroVolume = previous
	}
	a.volumeSaveAt = time.Now().Add(350 * time.Millisecond)
}

func (a *App) toggleMute() {
	v := a.engine.Volume()
	if v > 0.001 {
		a.setPlaybackVolume(0)
		return
	}
	restore := a.lastNonZeroVolume
	if restore <= 0.001 {
		restore = defaultPlaybackVolume
	}
	a.setPlaybackVolume(restore)
}

const playlistRowHeight int32 = 54

func (a *App) playlistListBounds() (top, bottom int32) {
	top = a.layout.Playlist.Top + 60
	bottom = a.layout.Playlist.Bottom - 28
	return
}

func (a *App) playlistVisibleRows() int {
	top, bottom := a.playlistListBounds()
	rows := int((bottom - top) / playlistRowHeight)
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (a *App) playlistMaxScroll() int {
	p := a.activePlaylist()
	if p == nil {
		return 0
	}
	maxScroll := len(p.Tracks) - a.playlistVisibleRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	return maxScroll
}

func (a *App) playlistScrollRects() (RECT, RECT, bool) {
	maxScroll := a.playlistMaxScroll()
	if maxScroll <= 0 {
		return RECT{}, RECT{}, false
	}
	top, bottom := a.playlistListBounds()
	track := RECT{a.layout.Playlist.Right - 9, top, a.layout.Playlist.Right - 4, bottom}
	trackH := rectH(track)
	visible := a.playlistVisibleRows()
	p := a.activePlaylist()
	if p == nil || len(p.Tracks) == 0 {
		return RECT{}, RECT{}, false
	}
	thumbH := int32(float64(trackH) * float64(visible) / float64(len(p.Tracks)))
	if thumbH < 24 {
		thumbH = 24
	}
	if thumbH > trackH {
		thumbH = trackH
	}
	travel := trackH - thumbH
	thumbTop := track.Top
	if travel > 0 {
		thumbTop += int32(math.Round(float64(travel) * float64(p.Scroll) / float64(maxScroll)))
	}
	thumb := RECT{track.Left, thumbTop, track.Right, thumbTop + thumbH}
	return track, thumb, true
}

func (a *App) playlistScrollbarHit(x, y int32) bool {
	track, _, ok := a.playlistScrollRects()
	return ok && pointInRect(x, y, inflateRect(track, 7, 2))
}

func (a *App) dragPlaylistScrollbar(y int32) {
	p := a.activePlaylist()
	if p == nil {
		return
	}
	track, thumb, ok := a.playlistScrollRects()
	if !ok {
		return
	}
	thumbH := rectH(thumb)
	travel := rectH(track) - thumbH
	if travel <= 0 {
		p.Scroll = 0
		return
	}
	top := y - a.playlistScrollDragOffset
	if top < track.Top {
		top = track.Top
	}
	if top > track.Bottom-thumbH {
		top = track.Bottom - thumbH
	}
	ratio := float64(top-track.Top) / float64(travel)
	p.Scroll = int(math.Round(ratio * float64(a.playlistMaxScroll())))
	a.clampPlaylistScroll()
}

func (a *App) playlistIndexAt(x, y int32) int {
	if !pointInRect(x, y, a.layout.Playlist) || a.playlistScrollbarHit(x, y) {
		return -1
	}
	top, bottom := a.playlistListBounds()
	if y < top || y >= bottom {
		return -1
	}
	p := a.activePlaylist()
	if p == nil {
		return -1
	}
	idx := p.Scroll + int((y-top)/playlistRowHeight)
	if idx < 0 || idx >= len(p.Tracks) {
		return -1
	}
	return idx
}

func (a *App) playlistRowRect(index int) RECT {
	top, _ := a.playlistListBounds()
	scroll := 0
	if p := a.activePlaylist(); p != nil {
		scroll = p.Scroll
	}
	row := index - scroll
	right := a.layout.Playlist.Right - 8
	if _, _, ok := a.playlistScrollRects(); ok {
		right -= 10
	}
	return RECT{a.layout.Playlist.Left + 8, top + int32(row)*playlistRowHeight, right, top + int32(row+1)*playlistRowHeight - 2}
}

func playlistRemoveRect(row RECT) RECT {
	return makeRect(row.Right-28, row.Top+(rectH(row)-20)/2, 20, 20)
}

func (a *App) selectPlaylistAt(y int32) {
	idx := a.playlistIndexAt(a.layout.Playlist.Left+12, y)
	if p := a.activePlaylist(); p != nil && idx >= 0 {
		p.Selected = idx
	}
}

func (a *App) clampPlaylistScroll() {
	p := a.activePlaylist()
	if p == nil {
		return
	}
	maxScroll := a.playlistMaxScroll()
	if p.Scroll < 0 {
		p.Scroll = 0
	}
	if p.Scroll > maxScroll {
		p.Scroll = maxScroll
	}
}

func (a *App) dragPlaylistAt(y int32) {
	p := a.activePlaylist()
	// Reordering one dragged row would silently drop the rest of a multi-row
	// selection, so a multi-selection only supports dragging files out.
	if p == nil || a.playlistMultiSelectActive() || len(p.Tracks) < 2 || a.playlistDragIndex < 0 || a.playlistDragIndex >= len(p.Tracks) {
		return
	}
	top, bottom := a.playlistListBounds()
	if y < top+18 && p.Scroll > 0 {
		p.Scroll--
	} else if y > bottom-18 {
		p.Scroll++
		a.clampPlaylistScroll()
	}
	target := p.Scroll + int((y-top)/playlistRowHeight)
	if target < 0 {
		target = 0
	}
	if target >= len(p.Tracks) {
		target = len(p.Tracks) - 1
	}
	if target != a.playlistDragIndex {
		a.moveTrack(a.playlistDragIndex, target)
		a.playlistDragMoved = true
	}
}

// tryStartPlaylistDragOut turns a playlist row drag into an OLE file drag once
// the pointer leaves the window, but only while the gesture has not reordered
// anything: inside the panel a drag keeps sorting the list, outside the window
// it exports the selected files. It reports whether the OLE drag has run, in
// which case the caller must stop treating the gesture as a reorder.
func (a *App) tryStartPlaylistDragOut(x, y int32) bool {
	if a.dragOutActive || a.playlistDragMoved || pointInRect(x, y, a.layout.Client) {
		return false
	}
	if a.activePlaylist() == nil {
		return false
	}
	paths := a.selectedTrackPaths()
	if len(paths) == 0 {
		return false
	}
	if !a.oleReady {
		a.setStatus("File drag and drop is unavailable.", true)
		return false
	}
	procReleaseCapture.Call()
	a.dragMode = 0
	a.dragOutActive = true
	effect := doDragDropFiles(paths)
	a.dragOutActive = false
	a.hover = hoverOutsidePoint()
	a.hoverDirty = true
	if effect&DROPEFFECT_COPY != 0 {
		a.setStatus(fmt.Sprintf("Dragged out %d files.", len(paths)), false)
	}
	invalidate(a.hwnd)
	return true
}

// copySelectedTracksToClipboard publishes the selected files as CF_HDROP so
// they can be pasted straight into Explorer.
func (a *App) copySelectedTracksToClipboard() {
	paths := a.selectedTrackPaths()
	if len(paths) == 0 {
		a.setStatus("No files to copy.", true)
		return
	}
	if !a.oleReady {
		a.setStatus("Cannot write files to the clipboard.", true)
		return
	}
	if err := copyFilesToClipboard(a.hwnd, paths); err != nil {
		a.setStatus("Could not copy files: "+err.Error(), true)
		return
	}
	a.setStatus(fmt.Sprintf("Copied %d files to the clipboard.", len(paths)), false)
}

func (a *App) showDotMenu(x, y int32) {
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	for i, k := range []string{"DOT: focused", "DOT: diffuse"} {
		flags := uintptr(MF_STRING)
		if (i == 1) == a.stereoPhosphor.diffuseDots {
			flags |= MF_CHECKED
		}
		procAppendMenuW.Call(m, flags, uintptr(i+1), uintptr(unsafe.Pointer(utf16Ptr(tr(k)))))
	}
	pt := POINT{scaleDPI(x, a.currentDPI()), scaleDPI(y, a.currentDPI())}
	procClientToScreen.Call(a.hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd := a.trackPopup(m, TPM_RETURNCMD, pt)
	if cmd == 1 || cmd == 2 {
		a.stereoPhosphor.diffuseDots = cmd == 2
		a.stereoPhosphor.reset()
		a.saveLayoutConfig()
		invalidate(a.hwnd)
	}
}

func (a *App) showPlaylistMenu(x, y int32) {
	p := a.activePlaylist()
	if p == nil || !pointInRect(x, y, a.layout.Playlist) || a.playlistScrollbarHit(x, y) || len(p.Tracks) == 0 {
		return
	}
	idx := a.playlistIndexAt(x, y)
	if idx >= 0 && !a.playlistSel.containsRow(p.ID, idx) {
		a.playlistSel.clickSingle(p.ID, idx)
		p.Selected = idx
	}
	multi := a.playlistMultiSelectActive()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	procAppendMenuW.Call(menu, MF_STRING, 2101, uintptr(unsafe.Pointer(utf16Ptr(tr("播放")))))
	procAppendMenuW.Call(menu, MF_STRING, 2106, uintptr(unsafe.Pointer(utf16Ptr(tr("打开文件目录")))))
	procAppendMenuW.Call(menu, MF_STRING, 2107, uintptr(unsafe.Pointer(utf16Ptr(tr("复制文件到剪贴板")))))
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	// Reordering only makes sense for a single row; a multi-selection is meant
	// to be dragged out or removed as a group.
	moveFlags := uintptr(MF_STRING)
	if multi {
		moveFlags |= MF_GRAYED
	}
	procAppendMenuW.Call(menu, moveFlags, 2102, uintptr(unsafe.Pointer(utf16Ptr(tr("上移")))))
	procAppendMenuW.Call(menu, moveFlags, 2103, uintptr(unsafe.Pointer(utf16Ptr(tr("下移")))))
	procAppendMenuW.Call(menu, MF_STRING, 2104, uintptr(unsafe.Pointer(utf16Ptr(tr("从播放列表移除")))))
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(menu, MF_STRING, 2105, uintptr(unsafe.Pointer(utf16Ptr(tr("清空播放列表")))))
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd := a.trackPopup(menu, TPM_LEFTALIGN|TPM_TOPALIGN|TPM_RETURNCMD, pt)
	switch cmd {
	case 2101:
		a.loadTrack(p.Selected, true)
	case 2106:
		a.openTrackFolder(p.Selected)
	case 2107:
		a.copySelectedTracksToClipboard()
	case 2102:
		if p.Selected > 0 {
			a.moveTrack(p.Selected, p.Selected-1)
			a.savePlaylistState()
		}
	case 2103:
		if p.Selected >= 0 && p.Selected < len(p.Tracks)-1 {
			a.moveTrack(p.Selected, p.Selected+1)
			a.savePlaylistState()
		}
	case 2104:
		a.removeSelected()
	case 2105:
		a.clearPlaylist()
	}
	invalidate(a.hwnd)
}

func (a *App) onDoubleClick(x, y int32) {
	l := a.layout
	if pointInRect(x, y, l.SplitPlaylist) || pointInRect(x, y, l.SplitRight) || pointInRect(x, y, l.SplitLoudnessSpectrum) || pointInRect(x, y, l.SplitMeterStereo) || pointInRect(x, y, l.SplitPlaylistInfo) {
		a.resetLayoutConfig()
		invalidate(a.hwnd)
		return
	}
	if a.analyzerView != analyzerViewSpectrum && pointInRect(x, y, a.spectrogramFrequencyRulerRect()) {
		a.resetSpectrogramFrequencyView()
		return
	}
	if pointInRect(x, y, a.layout.Playlist) {
		idx := a.playlistIndexAt(x, y)
		if p := a.activePlaylist(); p != nil && idx >= 0 {
			p.Selected = idx
			a.loadTrack(idx, true)
		}
	}
}

func (a *App) setResizeCursor(hwnd uintptr) bool {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	pt.X, pt.Y = a.physicalToLogicalPoint(pt.X, pt.Y)
	if pointInRect(pt.X, pt.Y, a.layout.SplitPlaylist) || pointInRect(pt.X, pt.Y, a.layout.SplitRight) {
		procSetCursor.Call(mustLoadCursor(IDC_SIZEWE))
		return true
	}
	if pointInRect(pt.X, pt.Y, a.layout.SplitLoudnessSpectrum) || pointInRect(pt.X, pt.Y, a.layout.SplitMeterStereo) || pointInRect(pt.X, pt.Y, a.layout.SplitPlaylistInfo) {
		procSetCursor.Call(mustLoadCursor(IDC_SIZENS))
		return true
	}
	if a.analyzerView != analyzerViewSpectrum && pointInRect(pt.X, pt.Y, a.spectrogramFrequencyRulerRect()) {
		procSetCursor.Call(mustLoadCursor(IDC_SIZENS))
		return true
	}
	return false
}

func (a *App) onWheel(delta int32, flags uint16, x, y int32) {
	if delta == 0 {
		return
	}
	if a.playlistMenuOpen && pointInRect(x, y, a.playlistDropdownGeometry().Rect) {
		steps := int(delta / 120)
		if steps == 0 {
			if delta > 0 {
				steps = 1
			} else {
				steps = -1
			}
		}
		a.playlistMenuScroll -= steps
		a.clampPlaylistMenuScroll(a.playlistDropdownGeometry().VisiblePlaylists)
		invalidateArea(a.hwnd, a.layout.Playlist)
		return
	}
	if pointInRect(x, y, a.layout.PlaylistSelector) {
		a.switchPlaylistByWheel(delta)
		return
	}
	if pointInRect(x, y, inflateRect(a.layout.LoudnessTargetBtn, 4, 4)) {
		a.adjustCustomLoudnessTarget(delta)
		return
	}
	if pointInRect(x, y, loudnessPlotRect(a.layout.Loudness)) {
		if keyDown(VK_MENU) {
			a.adjustLoudnessVerticalZoom(delta, y)
		} else {
			a.adjustLoudnessHorizontalZoom(delta)
		}
		return
	}
	if a.analyzerView != analyzerViewSpectrum && (pointInRect(x, y, a.spectrogramFrequencyRulerRect()) || flags&MK_CONTROL != 0 && pointInRect(x, y, analyzerPlotRect(a.layout.Spectrum))) {
		a.adjustSpectrogramFrequencyZoom(delta, y)
		return
	}
	if a.analyzerView == analyzerViewTrackSpectrogram && pointInRect(x, y, analyzerPlotRect(a.layout.Spectrum)) {
		if a.engine.InputActive() || a.current < 0 {
			return
		}
		if flags&MK_SHIFT != 0 {
			direction := 1
			if delta > 0 {
				direction = -1
			}
			a.panWave(direction)
		} else {
			step := -1
			if delta > 0 {
				step = 1
			}
			a.zoomWave(step, a.spectrogramRatioAtX(x))
		}
		a.invalidateDynamic(dirtyWave|dirtySpectrum, a.layout.Center)
		return
	}
	if pointInRect(x, y, a.layout.Playlist) {
		p := a.activePlaylist()
		if p == nil {
			return
		}
		step := 1
		if delta > 0 {
			step = -1
		}
		p.Scroll += step * 3
		max := a.playlistMaxScroll()
		if p.Scroll < 0 {
			p.Scroll = 0
		}
		if p.Scroll > max {
			p.Scroll = max
		}
		invalidateArea(a.hwnd, a.layout.Playlist)
		return
	}

	if pointInRect(x, y, a.layout.Wave) {
		if a.engine.InputActive() {
			return
		}
		if flags&MK_SHIFT != 0 {
			direction := 1
			if delta > 0 {
				direction = -1
			}
			a.panWave(direction)
		} else {
			step := -1
			if delta > 0 {
				step = 1
			}
			a.zoomWave(step, a.waveRatioAtX(x))
		}
		invalidateArea(a.hwnd, a.layout.Wave)
		return
	}

	// Volume changes are accepted only while the pointer is over the visible
	// volume slider hit area. Scrolling elsewhere must not alter playback level.
	volumeHit := inflateRect(a.layout.Volume, 10, 14)
	if !pointInRect(x, y, volumeHit) {
		return
	}

	steps := float32(delta) / 120.0
	if steps > 4 {
		steps = 4
	}
	if steps < -4 {
		steps = -4
	}
	v := a.engine.Volume() + steps*0.04
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	a.setPlaybackVolume(v)
	// Keep the percentage visible briefly after the last wheel event.
	a.volumePopupUntil = time.Now().Add(900 * time.Millisecond)
	a.invalidateDynamic(dirtyControls, a.layout.Controls)
}

func (a *App) onKey(key uint32) {
	if key == 0x70 {
		a.showHelpPage("guide")
		return
	} // F1
	if key == VK_ESCAPE && a.playlistMenuOpen {
		a.playlistMenuOpen = false
		invalidateArea(a.hwnd, a.layout.Playlist)
		return
	}
	switch key {
	case VK_SPACE:
		p := a.activePlaylist()
		if a.current < 0 && p != nil && len(p.Tracks) > 0 {
			a.loadTrack(maxi(p.Selected, 0), true)
		} else {
			a.togglePlayback()
		}
	case VK_LEFT:
		a.markLoudnessDiscontinuity()
		a.engine.SeekSeconds(a.engine.PositionSeconds() - 5)
	case VK_RIGHT:
		a.markLoudnessDiscontinuity()
		a.engine.SeekSeconds(a.engine.PositionSeconds() + 5)
	case VK_UP:
		a.setPlaybackVolume(a.engine.Volume() + 0.05)
	case VK_DOWN:
		a.setPlaybackVolume(a.engine.Volume() - 0.05)
	case VK_DELETE:
		a.removeSelected()
	case VK_HOME:
		if p := a.activePlaylist(); p != nil && len(p.Tracks) > 0 {
			p.Selected = 0
			p.Scroll = 0
		}
	case VK_END:
		if p := a.activePlaylist(); p != nil && len(p.Tracks) > 0 {
			p.Selected = len(p.Tracks) - 1
		}
	case 'O':
		a.openFilesDialog()
	case 'L':
		a.loop = !a.loop
		a.engine.SetLoop(a.loop)
	case 'C':
		a.continuousPlay = !a.continuousPlay
		a.saveLayoutConfig()
	case 'T':
		a.toggleTheme()
	case 'H':
		a.setLoudnessWindow((a.loudnessWindow + 1) % 5)
	case 'Z':
		if a.engine.InputActive() {
			break
		}
		start, end := a.waveView()
		if a.waveZoom >= 16 {
			a.waveZoom = 1
			a.waveOffset = 0
		} else {
			a.zoomWave(1, start+(end-start)/2)
		}
	case 'F':
		a.cycleSpectrumFFT()
	case 'G':
		a.cycleStereoMode()
	case 'V':
		a.cycleAnalyzerView()
	}
	invalidate(a.hwnd)
}
func (a *App) onDrop(hdrop uintptr) {
	count, _, _ := procDragQueryFileW.Call(hdrop, 0xffffffff, 0, 0)
	paths := make([]string, 0, int(count))
	for i := uintptr(0); i < count; i++ {
		n, _, _ := procDragQueryFileW.Call(hdrop, i, 0, 0)
		buf := make([]uint16, n+1)
		procDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), n+1)
		p := syscall.UTF16ToString(buf)
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && isAudioFile(path) {
					paths = append(paths, path)
				}
				return nil
			})
		} else {
			paths = append(paths, p)
		}
	}
	procDragFinish.Call(hdrop)
	sort.Strings(paths)
	a.addFiles(paths, true)
}

func (a *App) restoreOutputSelection() {
	if !a.engineReady.Load() || strings.TrimSpace(a.outputDeviceName) == "" {
		return
	}
	wanted := strings.TrimSpace(a.outputDeviceName)
	if a.outputBackend == OUTPUT_ASIO {
		for _, d := range a.engine.ASIODevices() {
			if strings.EqualFold(strings.TrimSpace(d.Name), wanted) {
				if err := a.engine.SwitchASIODevice(d.Index); err != nil {
					a.setStatus("Could not restore ASIO output: "+err.Error(), true)
				}
				return
			}
		}
		return
	}
	for _, d := range a.engine.Devices() {
		if strings.EqualFold(strings.TrimSpace(d.Name), wanted) {
			if err := a.engine.SwitchDevice(d.Index); err != nil {
				a.setStatus("Could not restore output device: "+err.Error(), true)
			}
			return
		}
	}
}

func (a *App) refreshDeviceName() {
	a.deviceName = "Output Device"
	if !a.engineReady.Load() {
		return
	}
	if a.engine.OutputMode() == OUTPUT_ASIO {
		current := a.engine.CurrentASIODevice()
		for _, d := range a.engine.ASIODevices() {
			if d.Index == current {
				name := strings.TrimSpace(d.Name)
				if name != "" {
					a.deviceName = "ASIO · " + name
					a.outputBackend = OUTPUT_ASIO
					a.outputDeviceName = name
				}
				return
			}
		}
		return
	}
	current := a.engine.CurrentDevice()
	for _, d := range a.engine.Devices() {
		if d.Index == current {
			name := strings.TrimSpace(d.Name)
			if name != "" {
				a.deviceName = name
				a.outputBackend = OUTPUT_SYSTEM
				a.outputDeviceName = name
			}
			return
		}
	}
}

func (a *App) resetLiveAnalysisView() {
	a.engine.ResetMeters()
	a.peakL, a.peakR, a.rmsL, a.rmsR = 0, 0, 0, 0
	a.vuHoldL, a.vuHoldR = 0, 0
	a.vuHoldAgeL, a.vuHoldAgeR = 0, 0
	a.clips = 0
	a.lufsM, a.lufsS, a.lufsI = -120, -120, -120
	a.lra, a.truePeak = 0, -120
	a.meterDisplayInit = false
	a.loudnessHistory = nil
	a.loudnessLoopMarkers = nil
	a.loudnessLabelInit = false
	a.loudnessElapsed, a.loudnessRenderElapsed = 0, 0
	a.historyClock = historyClock{}
	a.loudnessSegment = 0
	a.stereoPointCount = 0
	a.stereoNormalizeGain = 1
	a.stereoPhosphor.reset()
	a.correlation, a.correlationDisplay = 0, 0
	a.resetLiveWaveform()
	a.resetSpectrumState()
	a.resetLiveSpectrogram()
	a.syncAnalyzerForInput()
}

func (a *App) inputButtonLabel() string {
	if !a.engine.InputActive() || strings.TrimSpace(a.inputDeviceName) == "" {
		return "NONE"
	}
	if a.inputBackend == INPUT_ASIO {
		return "ASIO · " + a.inputDeviceName
	}
	return a.inputDeviceName
}

func (a *App) showInputMenu() {
	if !a.engineReady.Load() {
		return
	}
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	mode := a.engine.InputMode()
	noneFlags := uintptr(MF_STRING)
	if mode == INPUT_NONE {
		noneFlags |= MF_CHECKED
	}
	procAppendMenuW.Call(menu, noneFlags, 3000, uintptr(unsafe.Pointer(utf16Ptr(tr("NONE · 关闭输入监测")))))
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(utf16Ptr(tr("系统输入设备")))))
	systemInputs := a.engine.InputDevices()
	currentSystem := a.engine.CurrentInputDevice()
	if len(systemInputs) == 0 {
		procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(utf16Ptr(tr("未检测到系统输入设备")))))
	} else {
		for i, d := range systemInputs {
			name := d.Name
			if d.Default {
				name += tr("  · Default")
			}
			flags := uintptr(MF_STRING)
			if mode == INPUT_SYSTEM && d.Index == currentSystem {
				flags |= MF_CHECKED
			}
			procAppendMenuW.Call(menu, flags, uintptr(3100+i), uintptr(unsafe.Pointer(utf16Ptr(name))))
		}
	}

	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(utf16Ptr(tr("ASIO 输入 · 通道 1/2")))))
	asioInputs := a.engine.ASIODevices()
	currentASIO := a.engine.CurrentInputASIODevice()
	if len(asioInputs) == 0 {
		procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(utf16Ptr(tr("未检测到 ASIO 驱动")))))
	} else {
		for i, d := range asioInputs {
			flags := uintptr(MF_STRING)
			if mode == INPUT_ASIO && d.Index == currentASIO {
				flags |= MF_CHECKED
			}
			procAppendMenuW.Call(menu, flags, uintptr(4100+i), uintptr(unsafe.Pointer(utf16Ptr("ASIO · "+d.Name))))
		}
	}

	inputRect := a.logicalToPhysicalRect(a.layout.InputBtn)
	pt := POINT{inputRect.Left, inputRect.Bottom}
	procClientToScreen.Call(a.hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd := a.trackPopup(menu, TPM_LEFTALIGN|TPM_TOPALIGN|TPM_RETURNCMD, pt)
	switch {
	case cmd == 3000:
		a.engine.StopInput()
		a.inputBackend = INPUT_NONE
		a.inputDeviceName = "NONE"
		a.resetLiveAnalysisView()
		a.setStatus("Input monitoring off · Analyzing player output", false)
	case cmd >= 3100 && int(cmd-3100) < len(systemInputs):
		d := systemInputs[int(cmd-3100)]
		if err := a.engine.StartSystemInput(d.Index); err != nil {
			a.setStatus(err.Error(), true)
		} else {
			a.inputBackend = INPUT_SYSTEM
			a.inputDeviceName = d.Name
			a.resetLiveAnalysisView()
			rate, ch := a.engine.AnalysisInfo()
			a.setStatus(fmt.Sprintf("System input: %s · %.1f kHz · %d ch", d.Name, float64(rate)/1000, ch), false)
		}
	case cmd >= 4100 && int(cmd-4100) < len(asioInputs):
		d := asioInputs[int(cmd-4100)]
		if err := a.engine.StartASIOInput(d.Index); err != nil {
			a.setStatus(err.Error(), true)
		} else {
			a.inputBackend = INPUT_ASIO
			a.inputDeviceName = d.Name
			a.resetLiveAnalysisView()
			rate, ch := a.engine.AnalysisInfo()
			a.setStatus(fmt.Sprintf("ASIO input: %s · %.1f kHz · %d ch", d.Name, float64(rate)/1000, ch), false)
		}
	}
	invalidate(a.hwnd)
}

func (a *App) deviceButtonLabel() string {
	if strings.TrimSpace(a.deviceName) == "" {
		return "Output Device"
	}
	return a.deviceName
}

func (a *App) showDeviceMenu() {
	if !a.engineReady.Load() {
		return
	}
	systemDevices := a.engine.Devices()
	asioDevices := a.engine.ASIODevices()
	if len(systemDevices) == 0 && len(asioDevices) == 0 {
		return
	}
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	mode := a.engine.OutputMode()
	currentSystem := a.engine.CurrentDevice()
	for i, d := range systemDevices {
		name := d.Name
		if d.Default {
			name += tr("（系统默认）")
		}
		flags := uintptr(MF_STRING)
		if mode == OUTPUT_SYSTEM && d.Index == currentSystem {
			flags |= MF_CHECKED
		}
		procAppendMenuW.Call(menu, flags, uintptr(1000+i), uintptr(unsafe.Pointer(utf16Ptr(name))))
	}

	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	currentASIO := a.engine.CurrentASIODevice()
	if len(asioDevices) == 0 {
		procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(utf16Ptr(tr("ASIO · 未检测到驱动")))))
	} else {
		for i, d := range asioDevices {
			name := "ASIO · " + d.Name
			flags := uintptr(MF_STRING)
			if mode == OUTPUT_ASIO && d.Index == currentASIO {
				flags |= MF_CHECKED
			}
			procAppendMenuW.Call(menu, flags, uintptr(2000+i), uintptr(unsafe.Pointer(utf16Ptr(name))))
		}
		if mode == OUTPUT_ASIO {
			procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
			procAppendMenuW.Call(menu, MF_STRING, 2900, uintptr(unsafe.Pointer(utf16Ptr(tr("ASIO 控制面板…")))))
		}
	}

	deviceRect := a.logicalToPhysicalRect(a.layout.DeviceBtn)
	pt := POINT{deviceRect.Left, deviceRect.Bottom}
	procClientToScreen.Call(a.hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd := a.trackPopup(menu, TPM_LEFTALIGN|TPM_TOPALIGN|TPM_RETURNCMD, pt)
	switch {
	case cmd >= 1000 && int(cmd-1000) < len(systemDevices):
		d := systemDevices[int(cmd-1000)]
		if err := a.engine.SwitchDevice(d.Index); err != nil {
			a.setStatus(err.Error(), true)
		} else {
			a.outputBackend = OUTPUT_SYSTEM
			a.outputDeviceName = d.Name
			a.resetLiveAnalysisView()
			a.refreshDeviceName()
			a.saveLayoutConfig()
			a.setStatus("System output: "+d.Name, false)
		}
	case cmd >= 2000 && int(cmd-2000) < len(asioDevices):
		d := asioDevices[int(cmd-2000)]
		if err := a.engine.SwitchASIODevice(d.Index); err != nil {
			a.setStatus(err.Error(), true)
		} else {
			a.outputBackend = OUTPUT_ASIO
			a.outputDeviceName = d.Name
			a.resetLiveAnalysisView()
			a.refreshDeviceName()
			a.saveLayoutConfig()
			a.setStatus("ASIO output: "+d.Name+" · Channels 1/2", false)
		}
	case cmd == 2900:
		if err := a.engine.ASIOControlPanel(); err != nil {
			a.setStatus(err.Error(), true)
		}
	}
	invalidateArea(a.hwnd, a.layout.Controls)
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func maxi32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func formatTime(sec float64) string {
	if sec < 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		sec = 0
	}
	total := int(sec + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(size) / 1024
	idx := 0
	for v >= 1024 && idx < len(units)-1 {
		v /= 1024
		idx++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[idx])
	}
	return fmt.Sprintf("%.2f %s", v, units[idx])
}

func formatAvgBitrate(size int64, dur float64) string {
	if size <= 0 || dur <= 0 {
		return "—"
	}
	kbps := float64(size*8) / dur / 1000
	return fmt.Sprintf("%.0f kbps", kbps)
}

func formatWAVBitDepth(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".wav" && ext != ".wave" {
		return "—"
	}
	f, err := os.Open(path)
	if err != nil {
		return "—"
	}
	defer f.Close()

	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return "—"
	}
	if string(header[0:4]) != "RIFF" && string(header[0:4]) != "RF64" {
		return "—"
	}
	if string(header[8:12]) != "WAVE" {
		return "—"
	}

	chunkHeader := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			return "—"
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkHeader[4:8]))
		if chunkID == "fmt " {
			readSize := chunkSize
			if readSize > 64 {
				readSize = 64
			}
			buf := make([]byte, readSize)
			if _, err := io.ReadFull(f, buf); err != nil || len(buf) < 16 {
				return "—"
			}

			formatTag := binary.LittleEndian.Uint16(buf[0:2])
			bitsPerSample := binary.LittleEndian.Uint16(buf[14:16])
			validBits := bitsPerSample
			if formatTag == 0xfffe && len(buf) >= 40 {
				if v := binary.LittleEndian.Uint16(buf[18:20]); v > 0 {
					validBits = v
				}
				// The first word of the extensible sub-format GUID contains
				// the original WAVE format tag (PCM=1, IEEE float=3).
				formatTag = binary.LittleEndian.Uint16(buf[24:26])
			}
			if validBits == 0 {
				return "—"
			}
			suffix := ""
			switch formatTag {
			case 1:
				suffix = " PCM"
			case 3:
				suffix = " Float"
			}
			return fmt.Sprintf("%d-bit%s", validBits, suffix)
		}
		skip := chunkSize
		if skip%2 != 0 {
			skip++
		}
		if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
			return "—"
		}
	}
}

func channelLayoutLabel(ch int) string {
	switch ch {
	case 0:
		return "—"
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%d ch", ch)
	}
}

func dbText(v float32) string {
	if v <= -119 {
		return "−∞"
	}
	return fmt.Sprintf("%.1f", v)
}
func fileExtLabel(path string) string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
}

func audioClassLabel(path string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "wav", "wave", "flac", "aif", "aiff", "ape", "alac":
		return "LOSSLESS"
	case "mp3", "aac", "m4a", "ogg", "opus", "wma":
		return "LOSSY"
	default:
		return "AUDIO"
	}
}
func nowString() string { return time.Now().Format("15:04:05") }
