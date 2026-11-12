//go:build windows

package main

import "math"

const (
	correlationWindowSeconds = 0.300
	correlationBucketCount   = 2048
	correlationSignalFloor   = 1e-12 // -120 dBFS mean-square power
)

type correlationBucket struct {
	frames              float64
	sumL2, sumR2, sumLR float64
}

// correlationAccumulator keeps an energy-weighted, time-bounded window. Raw
// moments must be accumulated before division; averaging per-buffer correlation
// values would give a quiet buffer the same influence as a loud one.
type correlationAccumulator struct {
	buckets             [correlationBucketCount]correlationBucket
	head, count         int
	frames              float64
	sumL2, sumR2, sumLR float64
}

func (a *correlationAccumulator) reset() {
	a.head, a.count = 0, 0
	a.frames = 0
	a.sumL2, a.sumR2, a.sumLR = 0, 0, 0
}

func (a *correlationAccumulator) compactOldest() {
	if a.count < 2 {
		return
	}
	oldest := &a.buckets[a.head]
	nextIndex := (a.head + 1) % len(a.buckets)
	next := &a.buckets[nextIndex]
	next.frames += oldest.frames
	next.sumL2 += oldest.sumL2
	next.sumR2 += oldest.sumR2
	next.sumLR += oldest.sumLR
	*oldest = correlationBucket{}
	a.head = nextIndex
	a.count--
}

func (a *correlationAccumulator) removeOldest(frames float64) {
	for frames > 0 && a.count > 0 {
		bucket := &a.buckets[a.head]
		if frames >= bucket.frames {
			frames -= bucket.frames
			a.frames -= bucket.frames
			a.sumL2 -= bucket.sumL2
			a.sumR2 -= bucket.sumR2
			a.sumLR -= bucket.sumLR
			*bucket = correlationBucket{}
			a.head = (a.head + 1) % len(a.buckets)
			a.count--
			continue
		}

		fraction := frames / bucket.frames
		a.frames -= frames
		a.sumL2 -= bucket.sumL2 * fraction
		a.sumR2 -= bucket.sumR2 * fraction
		a.sumLR -= bucket.sumLR * fraction
		remaining := 1 - fraction
		bucket.frames -= frames
		bucket.sumL2 *= remaining
		bucket.sumR2 *= remaining
		bucket.sumLR *= remaining
		frames = 0
	}
	if a.frames < 1e-9 {
		a.reset()
		return
	}
	// Limit insignificant subtraction residue before the denominator is formed.
	if a.sumL2 < 0 {
		a.sumL2 = 0
	}
	if a.sumR2 < 0 {
		a.sumR2 = 0
	}
}

func (a *correlationAccumulator) add(sumL2, sumR2, sumLR float64, frames int, sampleRate uint32) (float32, bool) {
	if frames <= 0 || math.IsNaN(sumL2) || math.IsNaN(sumR2) || math.IsNaN(sumLR) ||
		math.IsInf(sumL2, 0) || math.IsInf(sumR2, 0) || math.IsInf(sumLR, 0) {
		return 0, false
	}
	if sampleRate == 0 {
		sampleRate = 48000
	}

	if a.count == len(a.buckets) {
		// Extremely small ASIO buffers can produce more callbacks than the ring
		// has slots. Merge the two oldest buckets instead of shortening the
		// requested time window or allocating from the real-time audio callback.
		a.compactOldest()
	}
	tail := (a.head + a.count) % len(a.buckets)
	bucket := correlationBucket{frames: float64(frames), sumL2: sumL2, sumR2: sumR2, sumLR: sumLR}
	a.buckets[tail] = bucket
	a.count++
	a.frames += bucket.frames
	a.sumL2 += bucket.sumL2
	a.sumR2 += bucket.sumR2
	a.sumLR += bucket.sumLR

	targetFrames := math.Max(1, math.Round(float64(sampleRate)*correlationWindowSeconds))
	if excess := a.frames - targetFrames; excess > 0 {
		a.removeOldest(excess)
	}
	if a.frames <= 0 || a.sumL2/a.frames <= correlationSignalFloor || a.sumR2/a.frames <= correlationSignalFloor {
		return 0, false
	}
	denominator := math.Sqrt(a.sumL2 * a.sumR2)
	if denominator <= 0 || math.IsNaN(denominator) || math.IsInf(denominator, 0) {
		return 0, false
	}
	corr := a.sumLR / denominator
	if corr < -1 {
		corr = -1
	} else if corr > 1 {
		corr = 1
	}
	return float32(corr), true
}
