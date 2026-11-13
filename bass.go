//go:build windows

package main

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	BASS_UNICODE             = 0x80000000
	BASS_SAMPLE_FLOAT        = 0x00000100
	BASS_SAMPLE_MONO         = 0x00000002
	BASS_SAMPLE_LOOP         = 0x00000004
	BASS_STREAM_PRESCAN      = 0x00020000
	BASS_STREAM_DECODE       = 0x00200000
	BASS_POS_BYTE            = 0
	BASS_ACTIVE_STOPPED      = 0
	BASS_ACTIVE_PLAYING      = 1
	BASS_ACTIVE_STALLED      = 2
	BASS_ACTIVE_PAUSED       = 3
	BASS_DATA_FFT2048        = 0x80000003
	BASS_DATA_FFT4096        = 0x80000004
	BASS_DATA_FFT8192        = 0x80000005
	BASS_DATA_FFT16384       = 0x80000006
	BASS_DATA_FFT_INDIVIDUAL = 0x00000010
	BASS_DATA_FFT_REMOVEDC   = 0x00000040
	BASS_ERROR_ALREADY       = 14

	// Keep playback analysis close to the audio that is actually heard.
	// BASS defaults to a 500 ms stream buffer and 100 ms update period, which
	// is comfortable for playback but visibly separates DSP-driven meters from
	// short transient sounds. Local file playback is cheap enough to use a
	// substantially shorter analysis/playback buffer safely.
	BASS_CONFIG_BUFFER       = 0
	BASS_CONFIG_UPDATEPERIOD = 1
	BASS_CONFIG_DEV_NONSTOP  = 50
	BASS_CONFIG_NORAMP       = 74

	BASS_DEVICE_ENABLED = 1
	BASS_DEVICE_DEFAULT = 2

	OUTPUT_SYSTEM = 0
	OUTPUT_ASIO   = 1

	INPUT_NONE   = 0
	INPUT_SYSTEM = 1
	INPUT_ASIO   = 2

	BASS_ASIO_RESET_ENABLE = 1
	BASS_ASIO_RESET_JOIN   = 2
	BASS_ASIO_RESET_PAUSE  = 4
	BASS_ASIO_ALLOUTPUTS   = 16

	BASS_SYNC_END = 2

	BASS_LOUDNESS_CURRENT    = 0
	BASS_LOUDNESS_INTEGRATED = 1
	BASS_LOUDNESS_RANGE      = 2
	BASS_LOUDNESS_PEAK       = 4
	BASS_LOUDNESS_TRUEPEAK   = 8
	BASS_LOUDNESS_AUTOFREE   = 0x8000
)

type bassChannelInfo struct {
	Freq     uint32
	Chans    uint32
	Flags    uint32
	CType    uint32
	OrigRes  uint32
	Plugin   uint32
	Sample   uint32
	_pad     uint32
	Filename uintptr
}

type bassDeviceInfo struct {
	Name   uintptr
	Driver uintptr
	Flags  uint32
	_pad   uint32
}

type OutputDevice struct {
	Index   int
	Name    string
	Default bool
}

type InputDevice struct {
	Index   int
	Name    string
	Default bool
}

type bassASIODeviceInfo struct {
	Name   uintptr
	Driver uintptr
}

type ASIODevice struct {
	Index  int
	Name   string
	Driver string
}

type WavePoint struct {
	MinL  float32
	MaxL  float32
	MinR  float32
	MaxR  float32
	Bands [waveformBandCount]float32
}

type WaveformData struct {
	Channels int
	Points   []WavePoint
}

// The audio callback publishes one envelope per callback into this lock-free
// ring. The UI consumes and aggregates those envelopes at its own cadence, so
// a delayed paint can never hold up the real-time audio thread.
const liveWaveRingSize = 512

type liveWaveSlot struct {
	sequence atomic.Uint64
	minLBits atomic.Uint32
	maxLBits atomic.Uint32
	minRBits atomic.Uint32
	maxRBits atomic.Uint32
}

type StereoPoint struct {
	X float32
	Y float32
}

type AudioEngine struct {
	mu sync.Mutex

	bassDLL *syscall.DLL
	loudDLL *syscall.DLL
	asioDLL *syscall.DLL

	pInit                *syscall.Proc
	pFree                *syscall.Proc
	pSetConfig           *syscall.Proc
	pErrorGetCode        *syscall.Proc
	pStreamCreate        *syscall.Proc
	pStreamCreateFile    *syscall.Proc
	pStreamFree          *syscall.Proc
	pChannelPlay         *syscall.Proc
	pChannelPause        *syscall.Proc
	pChannelStop         *syscall.Proc
	pChannelIsActive     *syscall.Proc
	pChannelGetData      *syscall.Proc
	pChannelGetLength    *syscall.Proc
	pChannelGetPos       *syscall.Proc
	pChannelSetPos       *syscall.Proc
	pChannelGetInfo      *syscall.Proc
	pChannelSetDSP       *syscall.Proc
	pChannelRemoveDSP    *syscall.Proc
	pChannelFlags        *syscall.Proc
	pChannelSetSync      *syscall.Proc
	pChannelUpdate       *syscall.Proc
	pPluginLoad          *syscall.Proc
	pGetDeviceInfo       *syscall.Proc
	pGetDevice           *syscall.Proc
	pSetDevice           *syscall.Proc
	pChannelSetDevice    *syscall.Proc
	pRecordGetDeviceInfo *syscall.Proc
	pRecordInit          *syscall.Proc
	pRecordFree          *syscall.Proc
	pRecordStart         *syscall.Proc

	pASIOGetVersion        *syscall.Proc
	pASIOErrorGetCode      *syscall.Proc
	pASIOSetUnicode        *syscall.Proc
	pASIOGetDeviceInfo     *syscall.Proc
	pASIOInit              *syscall.Proc
	pASIOFree              *syscall.Proc
	pASIOSetWindow         *syscall.Proc
	pASIOStart             *syscall.Proc
	pASIOStop              *syscall.Proc
	pASIOControlPanel      *syscall.Proc
	pASIOChannelEnableBASS *syscall.Proc
	pASIOChannelPause      *syscall.Proc
	pASIOChannelReset      *syscall.Proc

	pLoudStart    *syscall.Proc
	pLoudGetLevel *syscall.Proc
	pLoudStop     *syscall.Proc

	hwnd                 uintptr
	stream               uint32
	analysisStream       uint32
	dsp                  uint32
	loud                 uint32
	sampleRate           uint32
	channels             uint32
	totalBytes           uint64
	currentFile          string
	currentLoop          bool
	selectedDevice       int
	initializedDevices   map[int]bool
	outputMode           int
	asioDevice           int
	inputMode            int
	inputDevice          int
	inputASIODevice      int
	inputHandle          uint32
	inputRate            uint32
	inputChannels        uint32
	dspChannel           uint32
	volumeDSP            uint32
	asioInitialized      bool
	asioStarted          bool
	asioPaused           bool
	streamGeneration     uint64
	nextStreamGeneration uint64
	stereoMu             sync.RWMutex
	stereoPoints         []StereoPoint
	stereoSequence       uint64
	correlationWindow    correlationAccumulator
	fftBuffer            [8192]float32
	fftIndividual        []float32
	correlationBits      atomic.Uint32
	liveWaveRing         [liveWaveRingSize]liveWaveSlot
	liveWaveWrite        atomic.Uint64

	volumeBits    atomic.Uint32
	peakLBits     atomic.Uint32
	peakRBits     atomic.Uint32
	rmsLBits      atomic.Uint32
	rmsRBits      atomic.Uint32
	clipCount     atomic.Uint64
	samplesSeen   atomic.Uint64
	ready         atomic.Bool
	inputActive   atomic.Bool
	analysisChans atomic.Uint32
	analysisRate  atomic.Uint32
}

type preparedPlaybackStream struct {
	stream         uint32
	analysisStream uint32
	sampleRate     uint32
	channels       uint32
	totalBytes     uint64
	path           string
	loop           bool
	generation     uint64
}

var globalEngine atomic.Pointer[AudioEngine]
var globalDSPCallback uintptr
var globalRecordCallback uintptr
var globalVolumeDSPCallback uintptr
var globalPlaybackEndCallback uintptr

const defaultPlaybackVolume = 1.0
const maxPlaybackVolume = 1.25

func newAudioEngine() *AudioEngine {
	e := &AudioEngine{selectedDevice: -1, asioDevice: -1, inputDevice: -1, inputASIODevice: -1, initializedDevices: make(map[int]bool)}
	e.volumeBits.Store(math.Float32bits(defaultPlaybackVolume))
	return e
}

func findProc(dll *syscall.DLL, name string) (*syscall.Proc, error) {
	p, err := dll.FindProc(name)
	if err != nil {
		return nil, fmt.Errorf("Missing function %s: %w", name, err)
	}
	return p, nil
}

func (e *AudioEngine) Load(runtimeDir string, hwnd uintptr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hwnd = hwnd

	bassPath := filepath.Join(runtimeDir, "bass.dll")
	loudPath := filepath.Join(runtimeDir, "bassloud.dll")
	asioPath := filepath.Join(runtimeDir, "bassasio.dll")
	bass, err := syscall.LoadDLL(bassPath)
	if err != nil {
		return fmt.Errorf("Could not load bass.dll: %w", err)
	}
	loud, err := syscall.LoadDLL(loudPath)
	if err != nil {
		bass.Release()
		return fmt.Errorf("Could not load bassloud.dll: %w", err)
	}
	e.bassDLL, e.loudDLL = bass, loud
	// BASSASIO is optional for normal Windows output. When present, it adds
	// direct ASIO driver output while keeping the existing BASS device path.
	if asio, asioErr := syscall.LoadDLL(asioPath); asioErr == nil {
		e.asioDLL = asio
	}

	bind := func(dst **syscall.Proc, dll *syscall.DLL, name string) error {
		p, er := findProc(dll, name)
		if er != nil {
			return er
		}
		*dst = p
		return nil
	}
	for _, b := range []struct {
		dst  **syscall.Proc
		dll  *syscall.DLL
		name string
	}{
		{&e.pInit, bass, "BASS_Init"}, {&e.pFree, bass, "BASS_Free"}, {&e.pSetConfig, bass, "BASS_SetConfig"}, {&e.pErrorGetCode, bass, "BASS_ErrorGetCode"},
		{&e.pStreamCreate, bass, "BASS_StreamCreate"}, {&e.pStreamCreateFile, bass, "BASS_StreamCreateFile"}, {&e.pStreamFree, bass, "BASS_StreamFree"},
		{&e.pChannelPlay, bass, "BASS_ChannelPlay"}, {&e.pChannelPause, bass, "BASS_ChannelPause"}, {&e.pChannelStop, bass, "BASS_ChannelStop"},
		{&e.pChannelIsActive, bass, "BASS_ChannelIsActive"}, {&e.pChannelGetData, bass, "BASS_ChannelGetData"},
		{&e.pChannelGetLength, bass, "BASS_ChannelGetLength"}, {&e.pChannelGetPos, bass, "BASS_ChannelGetPosition"},
		{&e.pChannelSetPos, bass, "BASS_ChannelSetPosition"}, {&e.pChannelGetInfo, bass, "BASS_ChannelGetInfo"},
		{&e.pChannelSetDSP, bass, "BASS_ChannelSetDSP"}, {&e.pChannelRemoveDSP, bass, "BASS_ChannelRemoveDSP"},
		{&e.pChannelFlags, bass, "BASS_ChannelFlags"}, {&e.pChannelSetSync, bass, "BASS_ChannelSetSync"},
		{&e.pChannelUpdate, bass, "BASS_ChannelUpdate"}, {&e.pPluginLoad, bass, "BASS_PluginLoad"},
		{&e.pGetDeviceInfo, bass, "BASS_GetDeviceInfo"}, {&e.pGetDevice, bass, "BASS_GetDevice"}, {&e.pSetDevice, bass, "BASS_SetDevice"}, {&e.pChannelSetDevice, bass, "BASS_ChannelSetDevice"},
		{&e.pRecordGetDeviceInfo, bass, "BASS_RecordGetDeviceInfo"}, {&e.pRecordInit, bass, "BASS_RecordInit"}, {&e.pRecordFree, bass, "BASS_RecordFree"}, {&e.pRecordStart, bass, "BASS_RecordStart"},
		{&e.pLoudStart, loud, "BASS_Loudness_Start"}, {&e.pLoudGetLevel, loud, "BASS_Loudness_GetLevel"}, {&e.pLoudStop, loud, "BASS_Loudness_Stop"},
	} {
		if err := bind(b.dst, b.dll, b.name); err != nil {
			return err
		}
	}

	if e.asioDLL != nil {
		asioBindings := []struct {
			dst  **syscall.Proc
			name string
		}{
			{&e.pASIOGetVersion, "BASS_ASIO_GetVersion"},
			{&e.pASIOErrorGetCode, "BASS_ASIO_ErrorGetCode"},
			{&e.pASIOSetUnicode, "BASS_ASIO_SetUnicode"},
			{&e.pASIOGetDeviceInfo, "BASS_ASIO_GetDeviceInfo"},
			{&e.pASIOInit, "BASS_ASIO_Init"},
			{&e.pASIOFree, "BASS_ASIO_Free"},
			{&e.pASIOSetWindow, "BASS_ASIO_SetWindow"},
			{&e.pASIOStart, "BASS_ASIO_Start"},
			{&e.pASIOStop, "BASS_ASIO_Stop"},
			{&e.pASIOControlPanel, "BASS_ASIO_ControlPanel"},
			{&e.pASIOChannelEnableBASS, "BASS_ASIO_ChannelEnableBASS"},
			{&e.pASIOChannelPause, "BASS_ASIO_ChannelPause"},
			{&e.pASIOChannelReset, "BASS_ASIO_ChannelReset"},
		}
		for _, b := range asioBindings {
			if err := bind(b.dst, e.asioDLL, b.name); err != nil {
				e.asioDLL.Release()
				e.asioDLL = nil
				break
			}
		}
		if e.asioDLL != nil {
			version, _, _ := e.pASIOGetVersion.Call()
			// ALLOUTPUTS and safe hot reconfiguration are provided by 1.4.3.
			if uint32(version) < 0x01040300 {
				e.asioDLL.Release()
				e.asioDLL = nil
			} else {
				// Device names are exposed as UTF-16 when this is called before the
				// first enumeration/init, which avoids mojibake in non-English drivers.
				e.pASIOSetUnicode.Call(1)
			}
		}
	}

	// Lower the stream buffering/update latency before channels are created.
	// The official BASS defaults are 500 ms / 100 ms. A 120 ms playback buffer
	// with 10 ms update cycles keeps the custom DSP, BASSloud and phase scope
	// much closer to short transients while leaving a sensible safety margin.
	e.pSetConfig.Call(BASS_CONFIG_UPDATEPERIOD, 10)
	e.pSetConfig.Call(BASS_CONFIG_BUFFER, 120)
	// Sound-design assets often begin with a full-level transient. Preserve that
	// first transient while retaining BASS' ramp-out protection on pause/stop.
	if ok, _, _ := e.pSetConfig.Call(BASS_CONFIG_NORAMP, 2); ok == 0 {
		return fmt.Errorf("Could not disable startup fade-in (BASS error %d)", e.lastError())
	}

	// Initialize the default output. BASS accepts -1 as the default device.
	r, _, _ := e.pInit.Call(uintptr(^uint32(0)), 48000, 0, hwnd, 0)
	if r == 0 {
		return fmt.Errorf("Could not initialize audio device (BASS error %d)", e.lastError())
	}
	actual, _, _ := e.pGetDevice.Call()
	e.selectedDevice = int(actual)
	e.initializedDevices[e.selectedDevice] = true
	// Keep the Windows endpoint fed with silence between successive previews so
	// USB/WASAPI devices do not sleep and lose the next sound's leading edge.
	if ok, _, _ := e.pSetConfig.Call(BASS_CONFIG_DEV_NONSTOP, 1); ok == 0 {
		return fmt.Errorf("Could not keep the output active (BASS error %d)", e.lastError())
	}

	// Load mature format add-ons. A missing optional decoder is non-fatal.
	for _, name := range []string{"bassflac.dll", "bassopus.dll"} {
		pluginPath := filepath.Join(runtimeDir, name)
		p := utf16Ptr(pluginPath)
		e.pPluginLoad.Call(uintptr(unsafe.Pointer(p)), BASS_UNICODE)
	}

	if globalDSPCallback == 0 {
		globalDSPCallback = syscall.NewCallback(audioDSPProc)
	}
	if globalRecordCallback == 0 {
		globalRecordCallback = syscall.NewCallback(recordProc)
	}
	if globalVolumeDSPCallback == 0 {
		globalVolumeDSPCallback = syscall.NewCallback(volumeDSPProc)
	}
	if globalPlaybackEndCallback == 0 {
		globalPlaybackEndCallback = syscall.NewCallback(playbackEndSyncProc)
	}
	globalEngine.Store(e)
	e.ready.Store(true)
	return nil
}

func (e *AudioEngine) lastError() int {
	if e.pErrorGetCode == nil {
		return -1
	}
	r, _, _ := e.pErrorGetCode.Call()
	return int(int32(r))
}

func (e *AudioEngine) lastASIOError() int {
	if e.pASIOErrorGetCode == nil {
		return -1
	}
	r, _, _ := e.pASIOErrorGetCode.Call()
	return int(int32(r))
}

func (e *AudioEngine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopInputLocked(false)
	e.freeStreamLocked()
	e.freeASIODriverLocked()
	if e.pFree != nil {
		e.pFree.Call()
	}
	if e.asioDLL != nil {
		e.asioDLL.Release()
		e.asioDLL = nil
	}
	if e.loudDLL != nil {
		e.loudDLL.Release()
		e.loudDLL = nil
	}
	if e.bassDLL != nil {
		e.bassDLL.Release()
		e.bassDLL = nil
	}
	e.ready.Store(false)
	globalEngine.Store(nil)
}

func (e *AudioEngine) stopASIOStreamLocked() {
	if e.outputMode != OUTPUT_ASIO || !e.asioInitialized {
		return
	}
	if e.asioStarted && e.pASIOStop != nil {
		e.pASIOStop.Call()
	}
	e.asioStarted = false
	e.asioPaused = false
	if e.pASIOChannelReset != nil {
		// Disable/unjoin all outputs before releasing/rebinding a BASS decode stream.
		e.pASIOChannelReset.Call(0, uintptr(^uint32(0)), BASS_ASIO_RESET_ENABLE|BASS_ASIO_RESET_JOIN)
	}
}

func (e *AudioEngine) freeASIODriverLocked() {
	if !e.asioInitialized {
		return
	}
	if e.asioStarted && e.pASIOStop != nil {
		e.pASIOStop.Call()
	}
	if e.pASIOChannelReset != nil {
		e.pASIOChannelReset.Call(0, uintptr(^uint32(0)), BASS_ASIO_RESET_ENABLE|BASS_ASIO_RESET_JOIN)
	}
	if e.pASIOFree != nil {
		e.pASIOFree.Call()
	}
	e.asioInitialized = false
	e.asioStarted = false
	e.asioPaused = false
	e.asioDevice = -1
}

func (e *AudioEngine) freeStreamLocked() {
	if e.stream == 0 && e.analysisStream == 0 {
		return
	}
	if e.outputMode == OUTPUT_ASIO {
		e.stopASIOStreamLocked()
	}
	if e.stream != 0 {
		if e.dspChannel == e.stream {
			e.detachAnalysisLocked()
		}
		e.removePlaybackVolumeDSPLocked()
		e.pStreamFree.Call(uintptr(e.stream))
	}
	if e.analysisStream != 0 {
		e.pStreamFree.Call(uintptr(e.analysisStream))
	}
	e.stream, e.analysisStream = 0, 0
	e.sampleRate, e.channels, e.totalBytes = 0, 0, 0
	e.streamGeneration = 0
	e.currentFile = ""
	e.currentLoop = false
}

func (e *AudioEngine) freePreparedStreamLocked(s preparedPlaybackStream) {
	if s.stream != 0 {
		e.pStreamFree.Call(uintptr(s.stream))
	}
	if s.analysisStream != 0 {
		e.pStreamFree.Call(uintptr(s.analysisStream))
	}
}

func (e *AudioEngine) preparePlaybackStreamLocked(path string, loop bool) (preparedPlaybackStream, error) {
	var prepared preparedPlaybackStream
	if e.outputMode == OUTPUT_SYSTEM && e.selectedDevice >= 0 {
		e.pSetDevice.Call(uintptr(e.selectedDevice))
	}

	flags := uintptr(BASS_SAMPLE_FLOAT | BASS_STREAM_PRESCAN | BASS_UNICODE)
	if e.outputMode == OUTPUT_ASIO {
		flags |= BASS_STREAM_DECODE
	}
	if loop {
		flags |= BASS_SAMPLE_LOOP
	}
	p := utf16Ptr(path)
	r, _, _ := e.pStreamCreateFile.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, flags)
	if r == 0 {
		return prepared, fmt.Errorf("Could not open audio file (BASS error %d)", e.lastError())
	}
	prepared.stream = uint32(r)
	prepared.path = path
	prepared.loop = loop

	var info bassChannelInfo
	if ok, _, _ := e.pChannelGetInfo.Call(uintptr(prepared.stream), uintptr(unsafe.Pointer(&info))); ok == 0 {
		e.freePreparedStreamLocked(prepared)
		return preparedPlaybackStream{}, fmt.Errorf("Could not read stream information (BASS error %d)", e.lastError())
	}
	prepared.sampleRate, prepared.channels = info.Freq, info.Chans
	length, _, _ := e.pChannelGetLength.Call(uintptr(prepared.stream), BASS_POS_BYTE)
	prepared.totalBytes = uint64(length)

	if e.outputMode == OUTPUT_ASIO {
		ar, _, _ := e.pStreamCreateFile.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, flags)
		if ar == 0 {
			e.freePreparedStreamLocked(prepared)
			return preparedPlaybackStream{}, fmt.Errorf("Could not create ASIO analysis stream (BASS error %d)", e.lastError())
		}
		prepared.analysisStream = uint32(ar)
	}

	e.nextStreamGeneration++
	prepared.generation = e.nextStreamGeneration
	if syncHandle, _, _ := e.pChannelSetSync.Call(
		uintptr(prepared.stream), BASS_SYNC_END, 0,
		globalPlaybackEndCallback, uintptr(prepared.generation),
	); syncHandle == 0 {
		e.freePreparedStreamLocked(prepared)
		return preparedPlaybackStream{}, fmt.Errorf("Could not watch for end of playback (BASS error %d)", e.lastError())
	}
	return prepared, nil
}

func (e *AudioEngine) installPreparedStreamLocked(s preparedPlaybackStream) {
	e.stream = s.stream
	e.analysisStream = s.analysisStream
	e.sampleRate = s.sampleRate
	e.channels = s.channels
	e.totalBytes = s.totalBytes
	e.currentFile = s.path
	e.currentLoop = s.loop
	e.streamGeneration = s.generation
}

func (e *AudioEngine) currentPreparedStreamLocked() preparedPlaybackStream {
	return preparedPlaybackStream{
		stream:         e.stream,
		analysisStream: e.analysisStream,
		sampleRate:     e.sampleRate,
		channels:       e.channels,
		totalBytes:     e.totalBytes,
		path:           e.currentFile,
		loop:           e.currentLoop,
		generation:     e.streamGeneration,
	}
}

func (e *AudioEngine) detachCurrentProcessingLocked() {
	if e.dspChannel == e.stream {
		e.detachAnalysisLocked()
	}
	e.removePlaybackVolumeDSPLocked()
}

func (e *AudioEngine) attachCurrentProcessingLocked() {
	if !e.inputActive.Load() {
		e.attachAnalysisLocked(e.stream)
	} else {
		e.ensurePlaybackVolumeDSPLocked()
	}
}

func (e *AudioEngine) initASIODriverLocked(index int) error {
	if e.asioDLL == nil || e.pASIOInit == nil {
		return errors.New("ASIO unavailable. Reinstall the audio runtime.")
	}
	if e.asioInitialized && e.asioDevice == index {
		return nil
	}
	if e.asioInitialized {
		e.freeASIODriverLocked()
	}
	if e.pASIOSetWindow != nil {
		e.pASIOSetWindow.Call(e.hwnd)
	}
	r, _, _ := e.pASIOInit.Call(uintptr(index), BASS_ASIO_ALLOUTPUTS)
	if r == 0 {
		return fmt.Errorf("Could not initialize ASIO device (BASSASIO error %d)", e.lastASIOError())
	}
	e.asioInitialized = true
	e.asioDevice = index
	e.asioStarted = false
	e.asioPaused = false
	return nil
}

func (e *AudioEngine) configureASIOStreamLocked() error {
	if e.stream == 0 || !e.asioInitialized {
		return errors.New("ASIO output is not ready.")
	}
	if e.channels < 1 || e.channels > 2 {
		return fmt.Errorf("ASIO supports mono or stereo files; this file has %d channels.", e.channels)
	}
	r, _, _ := e.pASIOChannelEnableBASS.Call(0, 0, uintptr(e.stream), 1)
	if r == 0 {
		return fmt.Errorf("Could not connect ASIO output channels 1/2 (BASSASIO error %d)", e.lastASIOError())
	}
	return nil
}

func (e *AudioEngine) analysisHandleLocked() uint32 {
	if e.inputActive.Load() && e.inputHandle != 0 {
		return e.inputHandle
	}
	return e.stream
}

func (e *AudioEngine) detachAnalysisLocked() {
	if e.dsp != 0 && e.dspChannel != 0 && e.pChannelRemoveDSP != nil {
		e.pChannelRemoveDSP.Call(uintptr(e.dspChannel), uintptr(e.dsp))
	}
	if e.loud != 0 && e.pLoudStop != nil {
		e.pLoudStop.Call(uintptr(e.loud))
	}
	e.dsp, e.loud, e.dspChannel = 0, 0, 0
	e.analysisChans.Store(0)
	e.analysisRate.Store(0)
}

func (e *AudioEngine) ensurePlaybackVolumeDSPLocked() {
	if e.stream == 0 || e.volumeDSP != 0 || e.pChannelSetDSP == nil {
		return
	}
	d, _, _ := e.pChannelSetDSP.Call(uintptr(e.stream), globalVolumeDSPCallback, 0, 1)
	e.volumeDSP = uint32(d)
}

func (e *AudioEngine) removePlaybackVolumeDSPLocked() {
	if e.stream != 0 && e.volumeDSP != 0 && e.pChannelRemoveDSP != nil {
		e.pChannelRemoveDSP.Call(uintptr(e.stream), uintptr(e.volumeDSP))
	}
	e.volumeDSP = 0
}

func (e *AudioEngine) attachAnalysisLocked(handle uint32) {
	e.detachAnalysisLocked()
	if handle == 0 {
		e.analysisChans.Store(0)
		e.analysisRate.Store(0)
		return
	}
	e.analysisChans.Store(0)
	e.analysisRate.Store(0)
	var info bassChannelInfo
	if ok, _, _ := e.pChannelGetInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&info))); ok != 0 {
		e.analysisChans.Store(info.Chans)
		e.analysisRate.Store(info.Freq)
		if e.inputActive.Load() {
			e.inputRate, e.inputChannels = info.Freq, info.Chans
		}
	}
	d, _, _ := e.pChannelSetDSP.Call(uintptr(handle), globalDSPCallback, 0, 0)
	e.dsp = uint32(d)
	e.dspChannel = handle
	e.restartLoudLocked()
}

func (e *AudioEngine) openStreamLocked(path string, loop bool) error {
	if e.outputMode == OUTPUT_SYSTEM && e.selectedDevice >= 0 {
		e.pSetDevice.Call(uintptr(e.selectedDevice))
	}

	flags := uintptr(BASS_SAMPLE_FLOAT | BASS_STREAM_PRESCAN | BASS_UNICODE)
	if e.outputMode == OUTPUT_ASIO {
		flags |= BASS_STREAM_DECODE
	}
	if loop {
		flags |= BASS_SAMPLE_LOOP
	}
	p := utf16Ptr(path)
	r, _, _ := e.pStreamCreateFile.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, flags)
	if r == 0 {
		return fmt.Errorf("Could not open audio file (BASS error %d)", e.lastError())
	}
	e.stream = uint32(r)
	e.currentFile = path
	e.currentLoop = loop

	var info bassChannelInfo
	ok, _, _ := e.pChannelGetInfo.Call(uintptr(e.stream), uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		e.freeStreamLocked()
		return fmt.Errorf("Could not read stream information (BASS error %d)", e.lastError())
	}
	e.sampleRate, e.channels = info.Freq, info.Chans
	length, _, _ := e.pChannelGetLength.Call(uintptr(e.stream), BASS_POS_BYTE)
	e.totalBytes = uint64(length)
	e.nextStreamGeneration++
	e.streamGeneration = e.nextStreamGeneration
	if syncHandle, _, _ := e.pChannelSetSync.Call(
		uintptr(e.stream), BASS_SYNC_END, 0,
		globalPlaybackEndCallback, uintptr(e.streamGeneration),
	); syncHandle == 0 {
		e.freeStreamLocked()
		return fmt.Errorf("Could not watch for end of playback (BASS error %d)", e.lastError())
	}

	if e.outputMode == OUTPUT_ASIO {
		// A decoding channel is consumed by BASSASIO. Keep a second lightweight
		// decode handle for FFT queries so UI spectrum reads never advance/steal
		// samples from the actual ASIO playback stream.
		ar, _, _ := e.pStreamCreateFile.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, flags)
		if ar == 0 {
			e.freeStreamLocked()
			return fmt.Errorf("Could not create ASIO analysis stream (BASS error %d)", e.lastError())
		}
		e.analysisStream = uint32(ar)
	}

	// The analyzer normally follows the player, but when a live input is active
	// it follows that input instead and leaves playback untouched.
	if !e.inputActive.Load() {
		e.attachAnalysisLocked(e.stream)
	} else {
		e.ensurePlaybackVolumeDSPLocked()
	}

	if e.outputMode == OUTPUT_ASIO {
		if err := e.configureASIOStreamLocked(); err != nil {
			e.freeStreamLocked()
			return err
		}
	} else if r, _, _ := e.pChannelUpdate.Call(uintptr(e.stream), 0); r == 0 {
		e.freeStreamLocked()
		return fmt.Errorf("Audio prebuffer failed (BASS error %d)", e.lastError())
	}
	return nil
}

func (e *AudioEngine) Open(path string, loop bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ready.Load() {
		return errors.New("The audio engine is not ready.")
	}
	e.freeStreamLocked()
	e.resetMetersLocked()
	return e.openStreamLocked(path, loop)
}

// ReplaceAndPlay prepares the next file while the current stream and output
// device remain active, then swaps and starts it as one engine operation. This
// keeps rapid auditioning from creating a device-idle window between files.
func (e *AudioEngine) ReplaceAndPlay(path string, loop bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ready.Load() {
		return errors.New("The audio engine is not ready.")
	}

	prepared, err := e.preparePlaybackStreamLocked(path, loop)
	if err != nil {
		return err
	}
	previous := e.currentPreparedStreamLocked()

	// Move DSP/loudness processing before pre-buffering so the leading samples
	// receive the same volume and analysis treatment as the rest of the file.
	e.detachCurrentProcessingLocked()
	e.installPreparedStreamLocked(prepared)
	e.resetMetersLocked()
	e.attachCurrentProcessingLocked()

	rollback := func(swapErr error) error {
		e.detachCurrentProcessingLocked()
		e.freePreparedStreamLocked(prepared)
		e.installPreparedStreamLocked(previous)
		if previous.stream != 0 {
			e.attachCurrentProcessingLocked()
			if e.outputMode == OUTPUT_ASIO {
				_ = e.configureASIOStreamLocked()
			}
		}
		return swapErr
	}

	if e.outputMode == OUTPUT_ASIO {
		// BASSASIO 1.4.3 permits an already-enabled output to be rebound while
		// the driver is running. Keep the driver clock alive between files.
		if err := e.configureASIOStreamLocked(); err != nil {
			return rollback(err)
		}
		if err := e.playASIOLocked(); err != nil {
			return rollback(err)
		}
	} else {
		// Fill the channel buffer before replacing the previous stream. The END
		// sync was installed first, so even very short files retain notification.
		if r, _, _ := e.pChannelUpdate.Call(uintptr(e.stream), 0); r == 0 {
			return rollback(fmt.Errorf("Audio prebuffer failed (BASS error %d)", e.lastError()))
		}
		if r, _, _ := e.pChannelPlay.Call(uintptr(e.stream), 0); r == 0 {
			return rollback(fmt.Errorf("Playback failed (BASS error %d)", e.lastError()))
		}
	}

	// The output is already consuming the prepared stream (or has it buffered),
	// so releasing the previous handles cannot put the device to sleep.
	e.freePreparedStreamLocked(previous)
	return nil
}

func (e *AudioEngine) playASIOLocked() error {
	if e.stream == 0 || !e.asioInitialized {
		return errors.New("ASIO output is not ready.")
	}
	state, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
	if state == BASS_ACTIVE_STOPPED && e.totalBytes > 0 {
		pos, _, _ := e.pChannelGetPos.Call(uintptr(e.stream), BASS_POS_BYTE)
		if uint64(pos) >= e.totalBytes {
			e.pChannelSetPos.Call(uintptr(e.stream), 0, BASS_POS_BYTE)
		}
	}
	if !e.asioStarted {
		r, _, _ := e.pASIOStart.Call(0, 0)
		if r == 0 && e.lastASIOError() != BASS_ERROR_ALREADY {
			return fmt.Errorf("Could not start ASIO output (BASSASIO error %d)", e.lastASIOError())
		}
		e.asioStarted = true
		e.asioPaused = false
		return nil
	}
	if e.asioPaused {
		r, _, _ := e.pASIOChannelReset.Call(0, uintptr(^uint32(0)), BASS_ASIO_RESET_PAUSE)
		if r == 0 {
			return fmt.Errorf("Could not resume ASIO output (BASSASIO error %d)", e.lastASIOError())
		}
		e.asioPaused = false
	}
	return nil
}

func (e *AudioEngine) PlayPause() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stream == 0 {
		return errors.New("Open an audio file first.")
	}
	if e.outputMode == OUTPUT_ASIO {
		if e.asioStarted && !e.asioPaused {
			state, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
			if state == BASS_ACTIVE_PLAYING {
				r, _, _ := e.pASIOChannelPause.Call(0, uintptr(^uint32(0)))
				if r == 0 {
					return fmt.Errorf("Could not pause ASIO output (BASSASIO error %d)", e.lastASIOError())
				}
				e.asioPaused = true
				return nil
			}
		}
		return e.playASIOLocked()
	}
	state, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
	if state == BASS_ACTIVE_PLAYING || state == BASS_ACTIVE_STALLED {
		r, _, _ := e.pChannelPause.Call(uintptr(e.stream))
		if r == 0 {
			return fmt.Errorf("Could not pause playback (BASS error %d)", e.lastError())
		}
	} else {
		r, _, _ := e.pChannelPlay.Call(uintptr(e.stream), 0)
		if r == 0 {
			return fmt.Errorf("Playback failed (BASS error %d)", e.lastError())
		}
	}
	return nil
}

func (e *AudioEngine) Play() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stream == 0 {
		return errors.New("Open an audio file first.")
	}
	if e.outputMode == OUTPUT_ASIO {
		return e.playASIOLocked()
	}
	r, _, _ := e.pChannelPlay.Call(uintptr(e.stream), 0)
	if r == 0 {
		return fmt.Errorf("Playback failed (BASS error %d)", e.lastError())
	}
	return nil
}

func (e *AudioEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stream == 0 {
		return
	}
	if e.outputMode == OUTPUT_ASIO {
		if e.asioStarted && e.pASIOStop != nil {
			e.pASIOStop.Call()
		}
		e.asioStarted = false
		e.asioPaused = false
		e.pChannelSetPos.Call(uintptr(e.stream), 0, BASS_POS_BYTE)
		return
	}
	e.pChannelStop.Call(uintptr(e.stream))
	e.pChannelSetPos.Call(uintptr(e.stream), 0, BASS_POS_BYTE)
}

func (e *AudioEngine) isPlayingLocked() bool {
	if e.stream == 0 {
		return false
	}
	if e.outputMode == OUTPUT_ASIO {
		if !e.asioStarted || e.asioPaused {
			return false
		}
		r, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
		return r == BASS_ACTIVE_PLAYING
	}
	r, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
	return r == BASS_ACTIVE_PLAYING || r == BASS_ACTIVE_STALLED
}

func (e *AudioEngine) IsPlaying() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isPlayingLocked()
}

func (e *AudioEngine) MatchesPlaybackEnd(generation uint64, stream uint32) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stream != 0 && e.stream == stream && e.streamGeneration == generation
}

func (e *AudioEngine) State() uintptr {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stream == 0 {
		return BASS_ACTIVE_STOPPED
	}
	if e.outputMode == OUTPUT_ASIO {
		if e.asioPaused {
			return BASS_ACTIVE_PAUSED
		}
		if e.isPlayingLocked() {
			return BASS_ACTIVE_PLAYING
		}
		return BASS_ACTIVE_STOPPED
	}
	r, _, _ := e.pChannelIsActive.Call(uintptr(e.stream))
	return r
}

func (e *AudioEngine) SetLoop(loop bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.currentLoop = loop
	if e.stream == 0 {
		return
	}
	var f uintptr
	if loop {
		f = BASS_SAMPLE_LOOP
	}
	e.pChannelFlags.Call(uintptr(e.stream), f, BASS_SAMPLE_LOOP)
	if e.analysisStream != 0 {
		e.pChannelFlags.Call(uintptr(e.analysisStream), f, BASS_SAMPLE_LOOP)
	}
}

func (e *AudioEngine) positionSecondsLocked() float64 {
	if e.stream == 0 || e.sampleRate == 0 || e.channels == 0 {
		return 0
	}
	p, _, _ := e.pChannelGetPos.Call(uintptr(e.stream), BASS_POS_BYTE)
	if uint64(p) == ^uint64(0) {
		return 0
	}
	return float64(uint64(p)) / float64(e.sampleRate*e.channels*4)
}

func (e *AudioEngine) PositionSeconds() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.positionSecondsLocked()
}

func (e *AudioEngine) durationSecondsLocked() float64 {
	if e.sampleRate == 0 || e.channels == 0 {
		return 0
	}
	return float64(e.totalBytes) / float64(e.sampleRate*e.channels*4)
}

func (e *AudioEngine) DurationSeconds() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.durationSecondsLocked()
}

func (e *AudioEngine) seekSecondsLocked(sec float64) {
	if e.stream == 0 || e.sampleRate == 0 || e.channels == 0 {
		return
	}
	if sec < 0 {
		sec = 0
	}
	max := e.durationSecondsLocked()
	if sec > max {
		sec = max
	}
	bytes := uint64(sec * float64(e.sampleRate*e.channels*4))
	bytes -= bytes % uint64(e.channels*4)
	e.pChannelSetPos.Call(uintptr(e.stream), uintptr(bytes), BASS_POS_BYTE)
	if e.analysisStream != 0 {
		e.pChannelSetPos.Call(uintptr(e.analysisStream), uintptr(bytes), BASS_POS_BYTE)
	}
}

func (e *AudioEngine) SeekSeconds(sec float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seekSecondsLocked(sec)
}

func (e *AudioEngine) SetVolume(v float32) {
	if v < 0 {
		v = 0
	}
	if v > maxPlaybackVolume {
		v = maxPlaybackVolume
	}
	e.volumeBits.Store(math.Float32bits(v))
}
func (e *AudioEngine) Volume() float32 { return math.Float32frombits(e.volumeBits.Load()) }

func (e *AudioEngine) ResetMeters() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resetMetersLocked()
	e.restartLoudLocked()
}
func (e *AudioEngine) restartLoudLocked() {
	handle := e.analysisHandleLocked()
	if handle == 0 || e.pLoudStart == nil {
		return
	}
	if e.loud != 0 && e.pLoudStop != nil {
		e.pLoudStop.Call(uintptr(e.loud))
	}
	loudFlags := uintptr((3000 << 16) | BASS_LOUDNESS_INTEGRATED | BASS_LOUDNESS_RANGE | BASS_LOUDNESS_TRUEPEAK | BASS_LOUDNESS_AUTOFREE)
	l, _, _ := e.pLoudStart.Call(uintptr(handle), loudFlags, uintptr(int64(20)))
	e.loud = uint32(l)
}

func (e *AudioEngine) resetMetersLocked() {
	e.peakLBits.Store(0)
	e.peakRBits.Store(0)
	e.rmsLBits.Store(0)
	e.rmsRBits.Store(0)
	e.clipCount.Store(0)
	e.samplesSeen.Store(0)
	e.stereoMu.Lock()
	e.correlationWindow.reset()
	e.correlationBits.Store(0)
	e.stereoPoints = nil
	e.stereoMu.Unlock()
}

func (e *AudioEngine) MeterSnapshot() (peakL, peakR, rmsL, rmsR float32, clips uint64) {
	return math.Float32frombits(e.peakLBits.Swap(0)), math.Float32frombits(e.peakRBits.Swap(0)),
		math.Float32frombits(e.rmsLBits.Load()), math.Float32frombits(e.rmsRBits.Load()), e.clipCount.Load()
}

func (e *AudioEngine) StereoSnapshot() ([]StereoPoint, float32) {
	e.stereoMu.RLock()
	pts := make([]StereoPoint, len(e.stereoPoints))
	copy(pts, e.stereoPoints)
	corr := math.Float32frombits(e.correlationBits.Load())
	e.stereoMu.RUnlock()
	return pts, corr
}

func (e *AudioEngine) StereoSnapshotInto(dst []StereoPoint) (int, float32, uint64) {
	e.stereoMu.RLock()
	n := len(e.stereoPoints)
	if n > len(dst) {
		n = len(dst)
	}
	if n > 0 {
		start := len(e.stereoPoints) - n
		copy(dst[:n], e.stereoPoints[start:])
	}
	corr := math.Float32frombits(e.correlationBits.Load())
	sequence := e.stereoSequence
	e.stereoMu.RUnlock()
	return n, corr, sequence
}

func (e *AudioEngine) publishLiveWaveEnvelope(minL, maxL, minR, maxR float32) {
	// Only the active analysis DSP writes this ring, so publishing the sequence
	// last gives the UI a complete slot without a mutex or callback allocation.
	sequence := e.liveWaveWrite.Load() + 1
	slot := &e.liveWaveRing[(sequence-1)%liveWaveRingSize]
	slot.minLBits.Store(math.Float32bits(minL))
	slot.maxLBits.Store(math.Float32bits(maxL))
	slot.minRBits.Store(math.Float32bits(minR))
	slot.maxRBits.Store(math.Float32bits(maxR))
	slot.sequence.Store(sequence)
	e.liveWaveWrite.Store(sequence)
}

func (e *AudioEngine) LiveWaveformSequence() uint64 {
	return e.liveWaveWrite.Load()
}

// LiveWaveformEnvelopeSince folds every callback envelope published after the
// supplied sequence into one paint-cadence point. A 512-slot ring tolerates UI
// stalls while keeping the callback path bounded and allocation-free.
func (e *AudioEngine) LiveWaveformEnvelopeSince(after uint64) (WavePoint, uint64, bool) {
	latest := e.liveWaveWrite.Load()
	if after >= latest {
		return WavePoint{}, latest, false
	}
	start := after + 1
	if available := latest - start + 1; available > liveWaveRingSize {
		start = latest - liveWaveRingSize + 1
	}
	result := WavePoint{MinL: 1, MaxL: -1, MinR: 1, MaxR: -1}
	found := false
	for sequence := start; sequence <= latest; sequence++ {
		slot := &e.liveWaveRing[(sequence-1)%liveWaveRingSize]
		if slot.sequence.Load() != sequence {
			continue
		}
		minL := math.Float32frombits(slot.minLBits.Load())
		maxL := math.Float32frombits(slot.maxLBits.Load())
		minR := math.Float32frombits(slot.minRBits.Load())
		maxR := math.Float32frombits(slot.maxRBits.Load())
		if slot.sequence.Load() != sequence {
			continue
		}
		if minL < result.MinL {
			result.MinL = minL
		}
		if maxL > result.MaxL {
			result.MaxL = maxL
		}
		if minR < result.MinR {
			result.MinR = minR
		}
		if maxR > result.MaxR {
			result.MaxR = maxR
		}
		found = true
	}
	if !found {
		return WavePoint{}, latest, false
	}
	return result, latest, true
}

func (e *AudioEngine) Loudness() (m, s, i, lra, tp float32, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loud == 0 || e.pLoudGetLevel == nil {
		return -120, -120, -120, 0, -120, false
	}
	get := func(mode uintptr) (float32, bool) {
		var val float32
		r, _, _ := e.pLoudGetLevel.Call(uintptr(e.loud), mode, uintptr(unsafe.Pointer(&val)))
		if r == 0 {
			return -120, false
		}
		return val, true
	}
	var a, b, c, d, f bool
	m, a = get(400 << 16)
	s, b = get(3000 << 16)
	i, c = get(BASS_LOUDNESS_INTEGRATED)
	lra, f = get(BASS_LOUDNESS_RANGE)
	if !f || lra < 0 || math.IsNaN(float64(lra)) || math.IsInf(float64(lra), 0) {
		lra = 0
	}
	var linear float32
	linear, d = get(BASS_LOUDNESS_TRUEPEAK)
	if d && linear > 0 {
		tp = 20 * float32(math.Log10(float64(linear)))
	} else {
		tp = -120
	}
	return m, s, i, lra, tp, a || b || c || d || f
}

func (e *AudioEngine) FFT(size int) []float32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	bins := 1024
	flag := uintptr(BASS_DATA_FFT2048)
	switch size {
	case 4096:
		bins = 2048
		flag = BASS_DATA_FFT4096
	case 8192:
		bins = 4096
		flag = BASS_DATA_FFT8192
	case 16384:
		bins = 8192
		flag = BASS_DATA_FFT16384
	default:
		size = 2048
	}
	fft := e.fftBuffer[:bins]
	clear(fft)
	if e.inputActive.Load() && e.inputHandle != 0 {
		e.pChannelGetData.Call(uintptr(e.inputHandle), uintptr(unsafe.Pointer(&fft[0])), flag|BASS_DATA_FFT_REMOVEDC)
		return fft
	}
	if e.stream == 0 {
		return fft
	}
	handle := e.stream
	if e.outputMode == OUTPUT_ASIO && e.analysisStream != 0 {
		// BASS_ChannelGetData advances a decoding stream. Align and query the
		// dedicated analysis stream instead of disturbing ASIO playback.
		pos, _, _ := e.pChannelGetPos.Call(uintptr(e.stream), BASS_POS_BYTE)
		if uint64(pos) != ^uint64(0) {
			e.pChannelSetPos.Call(uintptr(e.analysisStream), pos, BASS_POS_BYTE)
		}
		handle = e.analysisStream
	}
	e.pChannelGetData.Call(uintptr(handle), uintptr(unsafe.Pointer(&fft[0])), flag|BASS_DATA_FFT_REMOVEDC)
	return fft
}

// FFTIndividual returns interleaved per-channel magnitudes. The returned slice
// aliases engine-owned scratch memory and is valid only until the next FFT call.
func (e *AudioEngine) FFTIndividual(size int) ([]float32, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	bins := 1024
	flag := uintptr(BASS_DATA_FFT2048)
	switch size {
	case 4096:
		bins = 2048
		flag = BASS_DATA_FFT4096
	case 8192:
		bins = 4096
		flag = BASS_DATA_FFT8192
	case 16384:
		bins = 8192
		flag = BASS_DATA_FFT16384
	default:
		size = 2048
	}
	channels := int(e.channels)
	if e.inputActive.Load() {
		channels = int(e.inputChannels)
	}
	if channels < 1 {
		channels = 1
	}
	needed := bins * channels
	if cap(e.fftIndividual) < needed {
		e.fftIndividual = make([]float32, needed)
	} else {
		e.fftIndividual = e.fftIndividual[:needed]
		clear(e.fftIndividual)
	}
	fft := e.fftIndividual
	flags := flag | BASS_DATA_FFT_REMOVEDC | BASS_DATA_FFT_INDIVIDUAL
	if e.inputActive.Load() && e.inputHandle != 0 {
		e.pChannelGetData.Call(uintptr(e.inputHandle), uintptr(unsafe.Pointer(&fft[0])), flags)
		return fft, channels
	}
	if e.stream == 0 {
		return fft, channels
	}
	handle := e.stream
	if e.outputMode == OUTPUT_ASIO && e.analysisStream != 0 {
		pos, _, _ := e.pChannelGetPos.Call(uintptr(e.stream), BASS_POS_BYTE)
		if uint64(pos) != ^uint64(0) {
			e.pChannelSetPos.Call(uintptr(e.analysisStream), pos, BASS_POS_BYTE)
		}
		handle = e.analysisStream
	}
	e.pChannelGetData.Call(uintptr(handle), uintptr(unsafe.Pointer(&fft[0])), flags)
	return fft, channels
}

func (e *AudioEngine) StreamInfo() (rate, ch uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sampleRate, e.channels
}

func (e *AudioEngine) AnalysisInfo() (rate, ch uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inputActive.Load() {
		return e.inputRate, e.inputChannels
	}
	return e.sampleRate, e.channels
}

func (e *AudioEngine) InputActive() bool { return e.inputActive.Load() }

func (e *AudioEngine) InputMode() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inputMode
}

func (e *AudioEngine) CurrentInputDevice() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inputDevice
}

func (e *AudioEngine) CurrentInputASIODevice() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inputASIODevice
}

func (e *AudioEngine) InputDevices() []InputDevice {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pRecordGetDeviceInfo == nil {
		return nil
	}
	var out []InputDevice
	for i := 0; i < 64; i++ {
		var di bassDeviceInfo
		r, _, _ := e.pRecordGetDeviceInfo.Call(uintptr(i), uintptr(unsafe.Pointer(&di)))
		if r == 0 {
			if i > 24 {
				break
			}
			continue
		}
		if di.Flags&BASS_DEVICE_ENABLED == 0 {
			continue
		}
		out = append(out, InputDevice{Index: i, Name: cString(di.Name), Default: di.Flags&BASS_DEVICE_DEFAULT != 0})
	}
	return out
}

func (e *AudioEngine) stopInputLocked(restorePlayback bool) {
	if !e.inputActive.Load() && e.inputMode == INPUT_NONE {
		return
	}
	e.detachAnalysisLocked()
	mode := e.inputMode
	if mode == INPUT_SYSTEM {
		if e.inputHandle != 0 && e.pChannelStop != nil {
			e.pChannelStop.Call(uintptr(e.inputHandle))
		}
		if e.pRecordFree != nil {
			e.pRecordFree.Call()
		}
	} else if mode == INPUT_ASIO {
		if e.pASIOChannelReset != nil && e.asioInitialized {
			e.pASIOChannelReset.Call(1, uintptr(^uint32(0)), BASS_ASIO_RESET_ENABLE|BASS_ASIO_RESET_JOIN)
		}
		if e.inputHandle != 0 && e.pStreamFree != nil {
			e.pStreamFree.Call(uintptr(e.inputHandle))
		}
		if e.outputMode != OUTPUT_ASIO && e.asioInitialized {
			e.freeASIODriverLocked()
		}
	}
	e.inputHandle = 0
	e.inputMode = INPUT_NONE
	e.inputDevice, e.inputASIODevice = -1, -1
	e.inputRate, e.inputChannels = 0, 0
	e.inputActive.Store(false)
	e.analysisChans.Store(e.channels)
	e.resetMetersLocked()
	if restorePlayback && e.stream != 0 {
		e.attachAnalysisLocked(e.stream)
	}
}

func (e *AudioEngine) StopInput() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopInputLocked(true)
}

func (e *AudioEngine) StartSystemInput(index int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pRecordInit == nil || e.pRecordStart == nil {
		return errors.New("Recording input unavailable.")
	}
	e.stopInputLocked(false)
	restore := func() {
		if e.stream != 0 {
			e.attachAnalysisLocked(e.stream)
		}
	}
	r, _, _ := e.pRecordInit.Call(uintptr(index))
	if r == 0 {
		restore()
		return fmt.Errorf("Could not initialize input device (BASS error %d)", e.lastError())
	}
	try := [][2]uint32{{48000, 2}, {44100, 2}, {48000, 1}, {44100, 1}}
	var handle uintptr
	for _, f := range try {
		h, _, _ := e.pRecordStart.Call(uintptr(f[0]), uintptr(f[1]), BASS_SAMPLE_FLOAT, globalRecordCallback, 0)
		if h != 0 {
			handle = h
			break
		}
	}
	if handle == 0 {
		e.pRecordFree.Call()
		restore()
		return fmt.Errorf("Could not start input monitoring (BASS error %d)", e.lastError())
	}
	e.inputMode = INPUT_SYSTEM
	e.inputDevice = index
	e.inputHandle = uint32(handle)
	e.inputActive.Store(true)
	e.ensurePlaybackVolumeDSPLocked()
	e.resetMetersLocked()
	e.attachAnalysisLocked(e.inputHandle)
	return nil
}

func (e *AudioEngine) StartASIOInput(index int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.asioDLL == nil || e.pASIOChannelEnableBASS == nil || e.pStreamCreate == nil {
		return errors.New("ASIO input unavailable.")
	}
	if e.outputMode == OUTPUT_ASIO && e.asioInitialized && e.asioDevice != index {
		return errors.New("Use the same ASIO driver for simultaneous input and output.")
	}
	e.stopInputLocked(false)
	restore := func() {
		if e.stream != 0 {
			e.attachAnalysisLocked(e.stream)
		}
	}
	if e.outputMode == OUTPUT_ASIO {
		if !e.asioInitialized || e.asioDevice != index {
			restore()
			return errors.New("The current ASIO output driver is not initialized.")
		}
	} else {
		if err := e.initASIODriverLocked(index); err != nil {
			restore()
			return err
		}
	}
	if e.selectedDevice >= 0 {
		e.pSetDevice.Call(uintptr(e.selectedDevice))
	}
	const streamProcPush = ^uintptr(0)
	makePush := func(ch uint32) uint32 {
		h, _, _ := e.pStreamCreate.Call(48000, uintptr(ch), BASS_SAMPLE_FLOAT|BASS_STREAM_DECODE, streamProcPush, 0)
		return uint32(h)
	}
	handle := makePush(2)
	chans := uint32(2)
	if handle == 0 {
		handle = makePush(1)
		chans = 1
	}
	if handle == 0 {
		if e.outputMode != OUTPUT_ASIO {
			e.freeASIODriverLocked()
		}
		restore()
		return fmt.Errorf("Could not create ASIO input analysis stream (BASS error %d)", e.lastError())
	}
	if e.pASIOChannelReset != nil {
		e.pASIOChannelReset.Call(1, uintptr(^uint32(0)), BASS_ASIO_RESET_ENABLE|BASS_ASIO_RESET_JOIN)
	}
	r, _, _ := e.pASIOChannelEnableBASS.Call(1, 0, uintptr(handle), 1)
	if r == 0 && chans == 2 {
		e.pStreamFree.Call(uintptr(handle))
		handle = makePush(1)
		chans = 1
		if handle != 0 {
			r, _, _ = e.pASIOChannelEnableBASS.Call(1, 0, uintptr(handle), 0)
		}
	}
	if r == 0 || handle == 0 {
		if handle != 0 {
			e.pStreamFree.Call(uintptr(handle))
		}
		if e.outputMode != OUTPUT_ASIO {
			e.freeASIODriverLocked()
		}
		restore()
		return fmt.Errorf("Could not connect ASIO input channels (BASSASIO error %d)", e.lastASIOError())
	}
	if !e.asioStarted {
		r, _, _ = e.pASIOStart.Call(0, 0)
		if r == 0 && e.lastASIOError() != BASS_ERROR_ALREADY {
			e.pASIOChannelReset.Call(1, uintptr(^uint32(0)), BASS_ASIO_RESET_ENABLE|BASS_ASIO_RESET_JOIN)
			e.pStreamFree.Call(uintptr(handle))
			if e.outputMode != OUTPUT_ASIO {
				e.freeASIODriverLocked()
			}
			restore()
			return fmt.Errorf("Could not start ASIO input (BASSASIO error %d)", e.lastASIOError())
		}
		e.asioStarted = true
	}
	e.inputMode = INPUT_ASIO
	e.inputASIODevice = index
	e.inputHandle = handle
	e.inputRate = 48000
	e.inputChannels = chans
	e.inputActive.Store(true)
	e.ensurePlaybackVolumeDSPLocked()
	e.resetMetersLocked()
	e.attachAnalysisLocked(handle)
	return nil
}

func (e *AudioEngine) Devices() []OutputDevice {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []OutputDevice
	for i := 1; i < 64; i++ {
		var di bassDeviceInfo
		r, _, _ := e.pGetDeviceInfo.Call(uintptr(i), uintptr(unsafe.Pointer(&di)))
		if r == 0 {
			if i > 24 {
				break
			}
			continue
		}
		if di.Flags&BASS_DEVICE_ENABLED == 0 {
			continue
		}
		out = append(out, OutputDevice{Index: i, Name: cString(di.Name), Default: di.Flags&BASS_DEVICE_DEFAULT != 0})
	}
	return out
}

func cString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	b := make([]byte, 0, 128)
	for i := 0; i < 512; i++ {
		c := *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	s := string(b)
	if strings.TrimSpace(s) == "" {
		return "Unnamed device"
	}
	return s
}

func wideCString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	u := make([]uint16, 0, 128)
	for i := 0; i < 512; i++ {
		c := *(*uint16)(unsafe.Pointer(ptr + uintptr(i*2)))
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	if len(u) == 0 {
		return "Unnamed ASIO device"
	}
	return syscall.UTF16ToString(u)
}

func (e *AudioEngine) ASIOAvailable() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.asioDLL != nil && e.pASIOGetDeviceInfo != nil
}

func (e *AudioEngine) ASIODevices() []ASIODevice {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.asioDLL == nil || e.pASIOGetDeviceInfo == nil {
		return nil
	}
	var out []ASIODevice
	for i := 0; i < 64; i++ {
		var di bassASIODeviceInfo
		r, _, _ := e.pASIOGetDeviceInfo.Call(uintptr(i), uintptr(unsafe.Pointer(&di)))
		if r == 0 {
			break
		}
		out = append(out, ASIODevice{Index: i, Name: wideCString(di.Name), Driver: wideCString(di.Driver)})
	}
	return out
}

func (e *AudioEngine) OutputMode() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.outputMode
}

func (e *AudioEngine) CurrentDevice() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.selectedDevice
}

func (e *AudioEngine) CurrentASIODevice() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.asioDevice
}

func (e *AudioEngine) restoreStreamLocked(path string, loop bool, pos float64, play bool) error {
	if path == "" {
		return nil
	}
	if err := e.openStreamLocked(path, loop); err != nil {
		return err
	}
	e.seekSecondsLocked(pos)
	if play {
		if e.outputMode == OUTPUT_ASIO {
			return e.playASIOLocked()
		}
		r, _, _ := e.pChannelPlay.Call(uintptr(e.stream), 0)
		if r == 0 {
			return fmt.Errorf("Could not resume playback (BASS error %d)", e.lastError())
		}
	}
	return nil
}

func (e *AudioEngine) ensureSystemDeviceLocked(index int) error {
	if !e.initializedDevices[index] {
		r, _, _ := e.pInit.Call(uintptr(index), 48000, 0, e.hwnd, 0)
		if r == 0 && e.lastError() != BASS_ERROR_ALREADY {
			return fmt.Errorf("Could not initialize output device (BASS error %d)", e.lastError())
		}
		e.initializedDevices[index] = true
	}
	if r, _, _ := e.pSetDevice.Call(uintptr(index)); r == 0 {
		return fmt.Errorf("Could not select output device (BASS error %d)", e.lastError())
	}
	if r, _, _ := e.pSetConfig.Call(BASS_CONFIG_DEV_NONSTOP, 1); r == 0 {
		return fmt.Errorf("Could not keep the output active (BASS error %d)", e.lastError())
	}
	return nil
}

func (e *AudioEngine) SwitchDevice(index int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inputMode == INPUT_ASIO && e.outputMode == OUTPUT_ASIO {
		e.stopInputLocked(false)
	}
	if err := e.ensureSystemDeviceLocked(index); err != nil {
		return err
	}
	if e.outputMode == OUTPUT_SYSTEM {
		if e.stream != 0 {
			r, _, _ := e.pChannelSetDevice.Call(uintptr(e.stream), uintptr(index))
			if r == 0 {
				return fmt.Errorf("Could not switch output device (BASS error %d)", e.lastError())
			}
		}
		e.selectedDevice = index
		e.pSetDevice.Call(uintptr(index))
		return nil
	}

	path, loop, pos, wasPlaying := e.currentFile, e.currentLoop, e.positionSecondsLocked(), e.isPlayingLocked()
	e.freeStreamLocked()
	e.freeASIODriverLocked()
	e.outputMode = OUTPUT_SYSTEM
	e.selectedDevice = index
	e.pSetDevice.Call(uintptr(index))
	if err := e.restoreStreamLocked(path, loop, pos, wasPlaying); err != nil {
		return err
	}
	return nil
}

func (e *AudioEngine) SwitchASIODevice(index int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inputMode == INPUT_ASIO && e.inputASIODevice != index {
		e.stopInputLocked(false)
	}
	if e.asioDLL == nil {
		return errors.New("ASIO unavailable. Reinstall the audio runtime.")
	}
	if e.outputMode == OUTPUT_ASIO && e.asioInitialized && e.asioDevice == index {
		return nil
	}

	path, loop, pos, wasPlaying := e.currentFile, e.currentLoop, e.positionSecondsLocked(), e.isPlayingLocked()
	oldMode, oldASIO := e.outputMode, e.asioDevice

	if oldMode == OUTPUT_SYSTEM {
		// Validate/open the driver before interrupting the current Windows output.
		if err := e.initASIODriverLocked(index); err != nil {
			return err
		}
		e.freeStreamLocked()
		e.outputMode = OUTPUT_ASIO
		if err := e.restoreStreamLocked(path, loop, pos, wasPlaying); err != nil {
			e.freeStreamLocked()
			e.freeASIODriverLocked()
			e.outputMode = OUTPUT_SYSTEM
			e.pSetDevice.Call(uintptr(e.selectedDevice))
			_ = e.restoreStreamLocked(path, loop, pos, wasPlaying)
			return err
		}
		return nil
	}

	// ASIO -> ASIO requires releasing the current driver before opening another.
	e.freeStreamLocked()
	e.freeASIODriverLocked()
	if err := e.initASIODriverLocked(index); err != nil {
		// Best-effort rollback to the previous ASIO driver and transport.
		if oldASIO >= 0 && e.initASIODriverLocked(oldASIO) == nil {
			e.outputMode = OUTPUT_ASIO
			_ = e.restoreStreamLocked(path, loop, pos, wasPlaying)
		} else {
			e.outputMode = OUTPUT_SYSTEM
			e.pSetDevice.Call(uintptr(e.selectedDevice))
			_ = e.restoreStreamLocked(path, loop, pos, wasPlaying)
		}
		return err
	}
	e.outputMode = OUTPUT_ASIO
	if err := e.restoreStreamLocked(path, loop, pos, wasPlaying); err != nil {
		return err
	}
	return nil
}

func (e *AudioEngine) ASIOControlPanel() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inputMode == INPUT_ASIO {
		e.stopInputLocked(false)
	}
	if e.outputMode != OUTPUT_ASIO || !e.asioInitialized || e.pASIOControlPanel == nil {
		return errors.New("Select an ASIO device first.")
	}
	path, loop, pos, wasPlaying := e.currentFile, e.currentLoop, e.positionSecondsLocked(), e.isPlayingLocked()
	device := e.asioDevice
	r, _, _ := e.pASIOControlPanel.Call()
	if r == 0 {
		return fmt.Errorf("Could not open ASIO control panel (BASSASIO error %d)", e.lastASIOError())
	}
	// Drivers commonly apply buffer/rate changes after reinitialization. Reopen
	// the selected driver and restore the transport when the panel closes.
	e.freeStreamLocked()
	e.freeASIODriverLocked()
	if err := e.initASIODriverLocked(device); err != nil {
		e.outputMode = OUTPUT_SYSTEM
		e.pSetDevice.Call(uintptr(e.selectedDevice))
		_ = e.restoreStreamLocked(path, loop, pos, wasPlaying)
		return fmt.Errorf("ASIO settings changed but reinitialization failed: %w", err)
	}
	e.outputMode = OUTPUT_ASIO
	return e.restoreStreamLocked(path, loop, pos, wasPlaying)
}

func (e *AudioEngine) GenerateTrackVisuals(path string, bins int, progress func(float64)) (WaveformData, SpectrogramData, error) {
	e.mu.Lock()
	device := e.selectedDevice
	e.mu.Unlock()
	if device >= 0 {
		e.pSetDevice.Call(uintptr(device))
	}
	if bins < 256 {
		bins = 256
	}
	p := utf16Ptr(path)
	flags := uintptr(BASS_SAMPLE_FLOAT | BASS_STREAM_DECODE | BASS_STREAM_PRESCAN | BASS_UNICODE)
	r, _, _ := e.pStreamCreateFile.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, flags)
	if r == 0 {
		return WaveformData{}, SpectrogramData{}, fmt.Errorf("Waveform decoding failed (BASS error %d)", e.lastError())
	}
	stream := uint32(r)
	defer e.pStreamFree.Call(uintptr(stream))
	var info bassChannelInfo
	e.pChannelGetInfo.Call(uintptr(stream), uintptr(unsafe.Pointer(&info)))
	chans := int(info.Chans)
	if chans < 1 {
		chans = 1
	}
	l, _, _ := e.pChannelGetLength.Call(uintptr(stream), BASS_POS_BYTE)
	totalFrames := uint64(l) / uint64(maxi(chans, 1)*4)
	if totalFrames == 0 || totalFrames > 1<<60 {
		return WaveformData{}, SpectrogramData{}, errors.New("Could not read audio duration.")
	}
	per := (totalFrames + uint64(bins) - 1) / uint64(bins)
	points := make([]WavePoint, bins)
	for i := range points {
		points[i].MinL = 1
		points[i].MinR = 1
		points[i].MaxL = -1
		points[i].MaxR = -1
	}
	buf := make([]float32, 16384)
	spectrum := newWaveformSpectrumAnalyzer(float64(info.Freq), chans, totalFrames, bins)
	var framesSeen uint64
	for {
		got, _, _ := e.pChannelGetData.Call(uintptr(stream), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*4))
		if uint32(got) == 0xffffffff || got == 0 {
			break
		}
		n := int(got) / 4
		for j := 0; j < n; j += chans {
			idx := int(framesSeen / per)
			if idx >= bins {
				idx = bins - 1
			}
			l := buf[j]
			r := l
			if chans > 1 && j+1 < n {
				r = buf[j+1]
			}
			spectrum.Add(l, r)
			if l < points[idx].MinL {
				points[idx].MinL = l
			}
			if l > points[idx].MaxL {
				points[idx].MaxL = l
			}
			if r < points[idx].MinR {
				points[idx].MinR = r
			}
			if r > points[idx].MaxR {
				points[idx].MaxR = r
			}
			framesSeen++
		}
		if progress != nil && framesSeen%(uint64(len(buf)/maxi(chans, 1))*8) == 0 {
			progress(float64(framesSeen) / float64(totalFrames))
		}
	}
	spectrum.Apply(points)
	for i := range points {
		if points[i].MinL > points[i].MaxL {
			points[i].MinL = 0
			points[i].MaxL = 0
		}
		if points[i].MinR > points[i].MaxR {
			points[i].MinR = points[i].MinL
			points[i].MaxR = points[i].MaxL
		}
	}
	if progress != nil {
		progress(1)
	}
	return WaveformData{Channels: chans, Points: points}, spectrum.Spectrogram(trackSpectrogramColumns), nil
}

func (e *AudioEngine) GenerateWaveform(path string, bins int, progress func(float64)) (WaveformData, error) {
	waveform, _, err := e.GenerateTrackVisuals(path, bins, progress)
	return waveform, err
}

func nearFullScale(lo, hi float32) bool { return hi >= 0.999 || lo <= -0.999 }
func sampleOver(s float32) bool         { return s > 1 || s < -1 }

func atomicMaxFloat(dst *atomic.Uint32, v float32) {
	for {
		oldBits := dst.Load()
		old := math.Float32frombits(oldBits)
		if old >= v {
			return
		}
		if dst.CompareAndSwap(oldBits, math.Float32bits(v)) {
			return
		}
	}
}

func volumeDSPProc(hDSP, channel, buffer, length, user uintptr) uintptr {
	e := globalEngine.Load()
	if e == nil || buffer == 0 || length == 0 {
		return 0
	}
	vol := math.Float32frombits(e.volumeBits.Load())
	samples := unsafe.Slice((*float32)(unsafe.Pointer(buffer)), int(length)/4)
	for i := range samples {
		samples[i] *= vol
	}
	return 0
}

func playbackEndSyncProc(handle, channel, data, user uintptr) uintptr {
	e := globalEngine.Load()
	if e != nil && e.hwnd != 0 {
		// The callback may run on BASS' real-time/update thread. Only enqueue the
		// immutable stream identity; file I/O and playlist mutation stay on the UI
		// thread. Generation rejects late callbacks after a rapid manual switch.
		postMessage(e.hwnd, msgPlaybackEnd, user, channel)
	}
	return 0
}

func recordProc(handle, buffer, length, user uintptr) uintptr {
	// Returning TRUE keeps the recording channel running; analysis is performed
	// by the DSP/BASSloud chain attached to the HRECORD itself.
	return 1
}

func audioDSPProc(hDSP, channel, buffer, length, user uintptr) uintptr {
	e := globalEngine.Load()
	if e == nil || buffer == 0 || length == 0 {
		return 0
	}
	n := int(length) / 4
	samples := unsafe.Slice((*float32)(unsafe.Pointer(buffer)), n)
	chans := int(e.analysisChans.Load())
	if chans < 1 {
		chans = 1
	}
	vol := math.Float32frombits(e.volumeBits.Load())
	liveInput := e.inputActive.Load()
	applyVolume := !liveInput
	var pL, pR float32
	minL, maxL := float32(1), float32(-1)
	minR, maxR := float32(1), float32(-1)
	var sumL, sumR, sumLR float64
	var frames int
	var clips uint64
	var collected [1024]StereoPoint
	collectedCount := 0
	const frameStep = 2
	for base := 0; base < n; base += chans {
		l := samples[base]
		r := l
		if chans > 1 && base+1 < n {
			r = samples[base+1]
		}
		al := l
		if al < 0 {
			al = -al
		}
		ar := r
		if ar < 0 {
			ar = -ar
		}
		if al > pL {
			pL = al
		}
		if ar > pR {
			pR = ar
		}
		if l < minL {
			minL = l
		}
		if l > maxL {
			maxL = l
		}
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		lf, rf := float64(l), float64(r)
		sumL += lf * lf
		sumR += rf * rf
		sumLR += lf * rf
		if frames%frameStep == 0 && collectedCount < len(collected) {
			x := (l - r) * 0.70710678
			y := (l + r) * 0.70710678
			collected[collectedCount] = StereoPoint{X: x, Y: y}
			collectedCount++
		}
		frames++
		for c := 0; c < chans && base+c < n; c++ {
			raw := samples[base+c]
			if sampleOver(raw) {
				clips++
			}
			if applyVolume {
				samples[base+c] = raw * vol
			}
		}
	}
	atomicMaxFloat(&e.peakLBits, pL)
	atomicMaxFloat(&e.peakRBits, pR)
	if frames > 0 {
		e.rmsLBits.Store(math.Float32bits(float32(math.Sqrt(sumL / float64(frames)))))
		e.rmsRBits.Store(math.Float32bits(float32(math.Sqrt(sumR / float64(frames)))))
	}
	if clips > 0 {
		e.clipCount.Add(clips)
	}
	if liveInput && frames > 0 {
		e.publishLiveWaveEnvelope(minL, maxL, minR, maxR)
	}
	if frames > 0 || collectedCount > 0 {
		e.stereoMu.Lock()
		if frames > 0 {
			corr, valid := e.correlationWindow.add(sumL, sumR, sumLR, frames, e.analysisRate.Load())
			if !valid {
				corr = 0
			}
			e.correlationBits.Store(math.Float32bits(corr))
		}
		if collectedCount > 0 {
			e.stereoPoints = append(e.stereoPoints, collected[:collectedCount]...)
			e.stereoSequence += uint64(collectedCount)
			if len(e.stereoPoints) > 2304 {
				start := len(e.stereoPoints) - 2304
				copy(e.stereoPoints, e.stereoPoints[start:])
				e.stereoPoints = e.stereoPoints[:2304]
			}
		}
		e.stereoMu.Unlock()
	}
	e.samplesSeen.Add(uint64(n))
	return 0
}

func ampToDB(v float32) float32 {
	if v <= 0.0000001 {
		return -120
	}
	return 20 * float32(math.Log10(float64(v)))
}
