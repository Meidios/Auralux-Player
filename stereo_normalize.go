//go:build windows

package main

import "math"

const (
	stereoNormalizeTargetRMS        = float32(0.35)
	stereoNormalizeTargetPeak       = float32(0.85)
	stereoNormalizeMaxGain          = float32(64.0)
	stereoNormalizeSignalFloor      = float32(0.00001) // -100 dBFS amplitude
	stereoNormalizeGainUpResponse   = float32(0.04)
	stereoNormalizeGainDownResponse = float32(0.12)
)

// normalizeStereoPoints applies a display-only gain to a complete Stereo Field
// snapshot. RMS supplies a stable target for quiet programme material while the
// peak constraint prevents short transients from expanding beyond the scope.
// Gain falls quickly when the signal gets louder and rises more slowly to avoid
// visible pumping between audio buffers.
func normalizeStereoPoints(points []StereoPoint, previousGain float32) float32 {
	return normalizeStereoPointsWithResponse(points, previousGain, stereoNormalizeGainUpResponse, stereoNormalizeGainDownResponse)
}

func normalizeStereoPointsWithResponse(points []StereoPoint, previousGain, gainUpResponse, gainDownResponse float32) float32 {
	if previousGain < 1 || math.IsNaN(float64(previousGain)) {
		previousGain = 1
	}
	if len(points) == 0 {
		return 1
	}

	var sumSquares float64
	var peakSquared float32
	for _, point := range points {
		magnitudeSquared := point.X*point.X + point.Y*point.Y
		sumSquares += float64(magnitudeSquared)
		if magnitudeSquared > peakSquared {
			peakSquared = magnitudeSquared
		}
	}
	rms := float32(math.Sqrt(sumSquares / float64(len(points))))
	peak := float32(math.Sqrt(float64(peakSquared)))
	if rms < stereoNormalizeSignalFloor || peak < stereoNormalizeSignalFloor {
		return 1
	}

	targetGain := stereoNormalizeTargetRMS / rms
	peakLimitedGain := stereoNormalizeTargetPeak / peak
	if peakLimitedGain < targetGain {
		targetGain = peakLimitedGain
	}
	if targetGain < 1 {
		targetGain = 1
	}
	if targetGain > stereoNormalizeMaxGain {
		targetGain = stereoNormalizeMaxGain
	}

	// Keep the complete field from visibly breathing with programme dynamics.
	// Peak/radial limiting still catches sudden excursions while the display
	// scale itself moves on a slower, visually stable trajectory.
	response := gainUpResponse
	if targetGain < previousGain {
		response = gainDownResponse
	}
	gain := previousGain + (targetGain-previousGain)*response
	for i := range points {
		points[i].X *= gain
		points[i].Y *= gain
	}
	return gain
}
