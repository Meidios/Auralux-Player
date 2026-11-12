//go:build windows

package main

import (
	"math"
	"testing"
)

func TestStereoNormalizeRaisesQuietFieldWithoutChangingShape(t *testing.T) {
	points := []StereoPoint{{X: 0.01, Y: 0.02}, {X: -0.02, Y: 0.01}}
	ratioBefore := points[0].X / points[0].Y
	gain := normalizeStereoPoints(points, 1)
	if gain <= 1 {
		t.Fatalf("quiet field gain = %.3f, want > 1", gain)
	}
	if math.Abs(float64(points[0].X/points[0].Y-ratioBefore)) > 0.000001 {
		t.Fatal("normalization changed stereo geometry")
	}
}

func TestStereoNormalizeDoesNotAttenuateLoudField(t *testing.T) {
	points := []StereoPoint{{X: 0.8, Y: 0.8}}
	gain := normalizeStereoPoints(points, 1)
	if gain != 1 || points[0].X != 0.8 || points[0].Y != 0.8 {
		t.Fatalf("loud field was changed: gain %.3f point %+v", gain, points[0])
	}
}

func TestStereoNormalizePeakLimitsTransient(t *testing.T) {
	points := make([]StereoPoint, 100)
	for i := range points {
		points[i] = StereoPoint{X: 0.01}
	}
	points[len(points)-1] = StereoPoint{X: 0.8}
	gain := float32(1)
	for i := 0; i < 20; i++ {
		frame := append([]StereoPoint(nil), points...)
		gain = normalizeStereoPoints(frame, gain)
	}
	if gain > stereoNormalizeTargetPeak/0.8+0.001 {
		t.Fatalf("transient-limited gain = %.3f", gain)
	}
}

func TestStereoNormalizeIgnoresSilenceAndInvalidGain(t *testing.T) {
	points := []StereoPoint{{}}
	if gain := normalizeStereoPoints(points, float32(math.NaN())); gain != 1 {
		t.Fatalf("silent field gain = %.3f, want 1", gain)
	}
}

func TestStereoNormalizeGainIsBounded(t *testing.T) {
	points := []StereoPoint{{X: 0.00002}}
	gain := float32(1)
	for i := 0; i < 400; i++ {
		frame := append([]StereoPoint(nil), points...)
		gain = normalizeStereoPoints(frame, gain)
	}
	if gain > stereoNormalizeMaxGain || gain < stereoNormalizeMaxGain-0.01 {
		t.Fatalf("settled gain = %.3f, want %.3f", gain, stereoNormalizeMaxGain)
	}
}

func TestStereoNormalizeUsesSlowerStableResponses(t *testing.T) {
	if stereoNormalizeGainDownResponse != 0.12 || stereoNormalizeGainUpResponse != 0.04 {
		t.Fatalf("phosphor normalize responses = down %.3f up %.3f", stereoNormalizeGainDownResponse, stereoNormalizeGainUpResponse)
	}
	if stereoNormalizeGainDownResponse <= stereoNormalizeGainUpResponse {
		t.Fatal("gain-down response must remain faster than gain-up response")
	}
}
