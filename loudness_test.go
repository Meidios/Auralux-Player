//go:build windows

package main

import "testing"

func TestUpdateMeterDisplayUsesCurrentLoudnessValues(t *testing.T) {
	a := App{
		peakL:                0.5,
		peakR:                0.4,
		truePeak:             -1.2,
		lufsM:                -12.3,
		lufsS:                -15.4,
		lufsI:                -18.5,
		lra:                  7.6,
		meterDisplayPeak:     -30,
		meterDisplayTruePeak: -30,
		meterDisplayM:        -60,
		meterDisplayS:        -60,
		meterDisplayI:        -60,
		meterDisplayLRA:      0,
		meterDisplayInit:     true,
	}

	a.updateMeterDisplay()

	if a.meterDisplayM != a.lufsM || a.meterDisplayS != a.lufsS || a.meterDisplayI != a.lufsI {
		t.Fatalf("loudness display = (%.1f, %.1f, %.1f), want current values (%.1f, %.1f, %.1f)",
			a.meterDisplayM, a.meterDisplayS, a.meterDisplayI, a.lufsM, a.lufsS, a.lufsI)
	}
	if a.meterDisplayLRA != a.lra {
		t.Fatalf("LRA display = %.1f, want %.1f", a.meterDisplayLRA, a.lra)
	}
	if a.meterDisplayPeak == -30 || a.meterDisplayTruePeak == -30 {
		t.Fatal("peak display ballistics did not advance")
	}
}

func TestUpdateMeterDisplayInitializesFromCurrentSnapshot(t *testing.T) {
	a := App{
		lufsM:    -20.1,
		lufsS:    -21.2,
		lufsI:    -22.3,
		lra:      4.5,
		truePeak: -0.8,
	}

	a.updateMeterDisplay()

	if !a.meterDisplayInit {
		t.Fatal("meter display was not initialized")
	}
	if a.meterDisplayM != a.lufsM || a.meterDisplayS != a.lufsS || a.meterDisplayI != a.lufsI || a.meterDisplayLRA != a.lra {
		t.Fatal("initial meter display does not match current loudness snapshot")
	}
}

func TestLoudnessMeterRefreshDueCapturesFinalFrame(t *testing.T) {
	if !loudnessMeterRefreshDue(7, true, false) {
		t.Fatal("analysis stop between scheduled refreshes did not request a final meter frame")
	}
	if loudnessMeterRefreshDue(7, false, false) {
		t.Fatal("idle meter requested an unnecessary refresh")
	}
	if !loudnessMeterRefreshDue(12, true, true) {
		t.Fatal("active meter missed its scheduled 20 Hz refresh")
	}
}
