//go:build windows

package main

import (
	"math"
	"testing"
)

func spectrumValueClose(got, want float32) bool {
	return math.Abs(float64(got-want)) < 0.000001
}

func TestUpdateSpectrumBandNormalAttackAndRelease(t *testing.T) {
	cur, hold, age := updateSpectrumBand(0.2, 0.8, 0.25, 12, 0.5, 0.75, spectrumModeNormal)
	if cur != 0.5 || hold != 0.5 || age != 0 {
		t.Fatalf("attack = (%v, %v, %d), want (0.5, 0.5, 0)", cur, hold, age)
	}

	cur, hold, age = updateSpectrumBand(0.8, 0.2, 0.9, 94, 0.5, 0.75, spectrumModeNormal)
	if !spectrumValueClose(cur, 0.65) {
		t.Fatalf("release cur = %v, want 0.65", cur)
	}
	if hold >= 0.9 || age != 95 {
		t.Fatalf("peak release = (%v, %d), want a lower hold at age 95", hold, age)
	}
}

func TestUpdateSpectrumBandHoldAccumulatesPerBandMaximum(t *testing.T) {
	cur, hold, age := updateSpectrumBand(0.7, 0.2, 0.8, 500, 0.5, 0.75, spectrumModeHold)
	if cur != 0.7 || hold != 0.8 || age != 0 {
		t.Fatalf("lower input changed held state: got (%v, %v, %d)", cur, hold, age)
	}

	cur, hold, age = updateSpectrumBand(cur, 1.0, hold, age, 0.5, 0.75, spectrumModeHold)
	if !spectrumValueClose(cur, 0.85) || !spectrumValueClose(hold, 0.85) || age != 0 {
		t.Fatalf("higher input = (%v, %v, %d), want (0.85, 0.85, 0)", cur, hold, age)
	}
}

func TestSpectrumFreezeSkipsFrameUpdate(t *testing.T) {
	a := &App{spectrumMode: spectrumModeFreeze}
	a.spectrum[0] = 0.4
	a.spectrumTarget[0] = 0.9
	a.spectrumHold[0] = 0.6
	a.spectrumHoldAge[0] = 42

	a.updateSpectrumFrame()

	if a.spectrum[0] != 0.4 || a.spectrumTarget[0] != 0.9 || a.spectrumHold[0] != 0.6 || a.spectrumHoldAge[0] != 42 {
		t.Fatalf("freeze mutated spectrum state")
	}
}

func TestNextSpectrumModeIsMutuallyExclusive(t *testing.T) {
	tests := []struct {
		current, requested, want spectrumDisplayMode
	}{
		{spectrumModeNormal, spectrumModeHold, spectrumModeHold},
		{spectrumModeHold, spectrumModeHold, spectrumModeNormal},
		{spectrumModeNormal, spectrumModeFreeze, spectrumModeFreeze},
		{spectrumModeFreeze, spectrumModeFreeze, spectrumModeNormal},
		{spectrumModeHold, spectrumModeFreeze, spectrumModeFreeze},
		{spectrumModeFreeze, spectrumModeHold, spectrumModeHold},
	}
	for _, tt := range tests {
		if got := nextSpectrumMode(tt.current, tt.requested); got != tt.want {
			t.Fatalf("nextSpectrumMode(%d, %d) = %d, want %d", tt.current, tt.requested, got, tt.want)
		}
	}
}

func TestResetSpectrumHoldAges(t *testing.T) {
	a := &App{}
	a.spectrumHoldAge[0] = 12
	a.spectrumHoldAge[200] = 65535
	a.resetSpectrumHoldAges()
	for i, age := range a.spectrumHoldAge {
		if age != 0 {
			t.Fatalf("hold age %d = %d, want 0", i, age)
		}
	}
}

func TestSpectrumHeaderControlsFitMinimumLayout(t *testing.T) {
	a := &App{
		playlistWidth:  260,
		rightWidth:     410,
		fileInfoHeight: 220,
		loudnessHeight: 260,
		meterHeight:    318,
	}
	l := a.calculateLayout(RECT{Right: 1180, Bottom: 720})
	controls := []RECT{
		l.SpectrumFFTBtn,
		l.SpectrumSmoothBtn,
		l.SpectrumTiltBtn,
		l.SpectrumBandBtn,
		l.SpectrumHoldBtn,
		l.SpectrumFreezeBtn,
	}
	if controls[0].Left < l.Spectrum.Left+114 || controls[len(controls)-1].Right > l.Spectrum.Right-12 {
		t.Fatalf("spectrum controls exceed the minimum-width header: spectrum=%+v controls=%+v", l.Spectrum, controls)
	}
	for i := 1; i < len(controls); i++ {
		if controls[i-1].Right > controls[i].Left {
			t.Fatalf("spectrum controls %d and %d overlap: %+v %+v", i-1, i, controls[i-1], controls[i])
		}
	}
}
