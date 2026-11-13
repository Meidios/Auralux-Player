package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestShortLoopHistoryClock(t *testing.T) {
	base := time.Now()
	var c historyClock
	c.step(base, true)
	c.step(base.Add(110*time.Millisecond), true)
	c.pending = 5
	marks := c.markers(nil, .020, .010)
	if len(marks) != 5 {
		t.Fatalf("wraps: %v", marks)
	}
	for i, m := range marks {
		if math.Abs(m-.020*float64(i+1)) > 1e-6 {
			t.Fatal(marks)
		}
	}
	c.step(base.Add(120*time.Millisecond), false)
	c.step(base.Add(5*time.Second), true)
	if math.Abs(c.elapsed-.12) > 1e-6 {
		t.Fatal("pause counted in history", c.elapsed)
	}
	a := App{historyClock: c}
	a.historyClock.pending = 1
	a.markLoudnessDiscontinuity()
	if a.historyClock.pending != 0 || a.historyClock.elapsed != c.elapsed || a.loudnessSegment != 1 {
		t.Fatal("seek advanced history or retained a pending marker")
	}
}

func TestLoopNotificationsOnWindowsOutput(t *testing.T) {
	if os.Getenv("AURALUX_AUDIO_SMOKE") != "1" {
		t.Skip("requires BASS and a Windows output")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// HWND_MESSAGE: the real callback posts to this thread without opening a UI.
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("STATIC"))), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, getModuleHandle(), 0)
	if hwnd == 0 {
		t.Fatal("could not create message-only test window")
	}
	defer procDestroyWindow.Call(hwnd)
	e := newAudioEngine()
	if err := e.Load("runtime", hwnd); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.SetVolume(0)
	path := filepath.Join(t.TempDir(), "20ms.wav")
	if err := os.WriteFile(path, silentWAV(48000, 2, 960), 0600); err != nil {
		t.Fatal(err)
	}
	a := App{engine: e, loop: true, current: 0}
	if err := e.ReplaceAndPlay(path, true); err != nil {
		t.Fatal(err)
	}
	a.historyClock.step(time.Now(), true)
	time.Sleep(450 * time.Millisecond)
	peek := modUser32.NewProc("PeekMessageW")
	for i := 0; i < 100; i++ {
		var msg MSG
		r, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), hwnd, msgPlaybackEnd, msgPlaybackEnd, 1)
		if r == 0 {
			break
		}
		a.onPlaybackEnded(uint64(msg.WParam), uint32(msg.LParam))
	}
	a.historyClock.step(time.Now(), true)
	marks := a.historyClock.markers(nil, e.DurationSeconds(), e.PositionSeconds())
	if len(marks) < 4 {
		t.Fatalf("received only %d markers", len(marks))
	}
	for i := 1; i < len(marks); i++ {
		if math.Abs(marks[i]-marks[i-1]-.020) > 1e-6 {
			t.Fatal(marks)
		}
	}
	t.Logf("20 ms looping stereo stream: %d correctly spaced markers", len(marks))
}
func TestNearFullScaleAndSampleOvers(t *testing.T) {
	for _, r := range []struct {
		v          float32
		near, over bool
	}{{.998, false, false}, {.999, true, false}, {1, true, false}, {-1, true, false}, {1.001, true, true}, {-1.001, true, true}} {
		if nearFullScale(min(r.v, 0), max(r.v, 0)) != r.near || sampleOver(r.v) != r.over {
			t.Fatal(r)
		}
	}
}

func TestLiveSpectrogramKeepsFirstAndSubpixelAttack(t *testing.T) {
	for _, frames := range []int{1, 16} {
		var state liveSpectrogramState
		for i := 0; i < frames; i++ {
			state.appendFFT(nil, 48000, float64(i)/60)
		}
		for j := 0; j < spectrogramBandCount; j++ {
			state.values[(frames/3)*spectrogramBandCount+j] = 255
		}
		s := spectrogramSurface{width: 2, height: 4, pixels: make([]byte, 32), theme: -1}
		s.buildPalette(0)
		s.renderLive(&state, 3, 120, spectrogramScaleLog, 20, 20000, 20000)
		if binary.LittleEndian.Uint32(s.pixels[4:8]) != s.palette[255] || binary.LittleEndian.Uint32(s.pixels[:4]) != s.palette[0] {
			t.Fatalf("%d frames: dropped or misplaced attack", frames)
		}
	}
}

func TestFocusedDotsReduceMonoTraceSpread(t *testing.T) {
	spread := func(diffuse bool) float64 {
		s := newStereoPhosphorTestState(450, 450)
		s.diffuseDots = diffuse
		for i := 0; i < 2400; i++ {
			s.exposeDot(stereoPhosphorPoint{X: 225, Y: float32(60 + i%330)}, 1)
		}
		var total, ms float64
		for i, v := range s.energyLayers[0] {
			d := float64(i%450) - 225
			ms += float64(v) * d * d
			total += float64(v)
		}
		if total == 0 {
			t.Fatal("empty DOT render")
		}
		return math.Sqrt(ms / total)
	}
	a, b := spread(false), spread(true)
	if a >= 0.65*b {
		t.Fatalf("mono RMS spread: focused %.2f px, diffuse %.2f px", a, b)
	}
	t.Logf("mono RMS spread: focused %.2f px, diffuse %.2f px", a, b)
}

// Capture the final BASS device mix and silence this test output before it goes
// to the Windows endpoint. This exercises prebuffering, playback and mixing,
// without relying on a microphone or exposing the listener to test impulses.
func TestFirstAttackAtDeviceMix(t *testing.T) {
	if os.Getenv("AURALUX_AUDIO_SMOKE") != "1" {
		t.Skip("requires a Windows output and BASS runtime")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	e := newAudioEngine()
	if err := e.Load("runtime", 0); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	create, err := e.bassDLL.FindProc("BASS_StreamCreate")
	if err != nil {
		t.Fatal(err)
	}
	mix, _, _ := create.Call(0, 0, 0, ^uintptr(1), 0)
	if mix == 0 {
		t.Fatalf("device mix: %d", e.lastError())
	}
	var info bassChannelInfo
	e.pChannelGetInfo.Call(mix, uintptr(unsafe.Pointer(&info)))
	if info.Freq == 0 || info.Chans < 2 {
		t.Fatal(info)
	}
	var mu sync.Mutex
	var audio []float32
	cb := syscall.NewCallback(func(_, _, buffer, length, _ uintptr) uintptr {
		s := unsafe.Slice((*float32)(unsafe.Pointer(buffer)), int(length)/4)
		mu.Lock()
		for i := 0; i < len(s); i += int(info.Chans) {
			audio = append(audio, s[i])
		}
		mu.Unlock()
		clear(s)
		return 0
	})
	dsp, _, _ := e.pChannelSetDSP.Call(mix, cb, 0, 0)
	if dsp == 0 {
		t.Fatal("device DSP unavailable")
	}
	defer e.pChannelRemoveDSP.Call(mix, dsp)
	rate := int(info.Freq)
	w := silentWAV(rate, 2, rate/4)
	// A one-frame attack followed by silence and an identical reference pulse.
	for _, i := range []int{0, rate / 20} {
		for ch := 0; ch < 2; ch++ {
			binary.LittleEndian.PutUint16(w[44+i*4+2*ch:], 8192)
		}
	}
	path := filepath.Join(t.TempDir(), "attack.wav")
	if err = os.WriteFile(path, w, 0600); err != nil {
		t.Fatal(err)
	}
	e.SetVolume(1)
	for pass := 1; pass <= 2; pass++ {
		mu.Lock()
		audio = nil
		mu.Unlock()
		if err = e.ReplaceAndPlay(path, false); err != nil {
			t.Fatal(err)
		}
		time.Sleep(400 * time.Millisecond)
		mu.Lock()
		got := append([]float32(nil), audio...)
		mu.Unlock()
		var peaks []int
		for i, x := range got {
			if math.Abs(float64(x)) > .005 {
				peaks = append(peaks, i)
			}
		}
		if len(peaks) != 2 || peaks[1]-peaks[0] != rate/20 {
			t.Fatalf("pass %d: non-silent frames %v, captured %d at %d Hz", pass, peaks, len(got), rate)
		}
		a, b := got[peaks[0]], got[peaks[1]]
		if math.Abs(float64(a-b)) > .0001 || a < .02 {
			t.Fatalf("pass %d: attack %g vs reference %g", pass, a, b)
		}
		t.Logf("pass %d: first frame %.6f, reference %.6f, separation %d frames at %d Hz", pass, a, b, peaks[1]-peaks[0], rate)
	}
}
