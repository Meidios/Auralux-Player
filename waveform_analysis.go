//go:build windows

package main

import (
	"math"
	"math/cmplx"
)

const (
	waveformBandCount = 5
	waveformFFTSize   = 2048
	waveformTiltDB    = 4.5
	waveformSoftmaxDB = 4.5
)

var waveformBandEdges = [waveformBandCount + 1]float64{20, 60, 250, 2000, 6000, 20000}

type waveformSpectrumFrame struct {
	center      uint64
	bands       [waveformBandCount]float32
	spectrogram [spectrogramBandCount]uint8
}

type waveformSpectrumAnalyzer struct {
	rate        float64
	channels    int
	totalFrames uint64
	hop         uint64
	windowStart uint64
	skip        uint64
	left        []float64
	right       []float64
	window      [waveformFFTSize]float64
	windowSum   float64
	fftScratch  []complex128
	frames      []waveformSpectrumFrame
}

func newWaveformSpectrumAnalyzer(rate float64, channels int, totalFrames uint64, bins int) *waveformSpectrumAnalyzer {
	if rate <= 0 {
		rate = 48000
	}
	if bins < 1 {
		bins = 1
	}
	hop := uint64(waveformFFTSize / 2)
	if adaptive := (totalFrames + uint64(bins) - 1) / uint64(bins); adaptive > hop {
		hop = adaptive
	}
	a := &waveformSpectrumAnalyzer{
		rate:        rate,
		channels:    channels,
		totalFrames: totalFrames,
		hop:         hop,
		left:        make([]float64, 0, waveformFFTSize),
		right:       make([]float64, 0, waveformFFTSize),
		fftScratch:  make([]complex128, waveformFFTSize),
		frames:      make([]waveformSpectrumFrame, 0, mini(bins, 12000)+1),
	}
	for i := range a.window {
		a.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(waveformFFTSize-1))
		a.windowSum += a.window[i]
	}
	return a
}

func (a *waveformSpectrumAnalyzer) Add(left, right float32) {
	if a.skip > 0 {
		a.skip--
		return
	}
	a.left = append(a.left, float64(left))
	if a.channels > 1 {
		a.right = append(a.right, float64(right))
	}
	if len(a.left) < waveformFFTSize {
		return
	}
	a.analyzeWindow()
	if a.hop < waveformFFTSize {
		keep := uint64(waveformFFTSize) - a.hop
		copy(a.left, a.left[a.hop:])
		a.left = a.left[:keep]
		if a.channels > 1 {
			copy(a.right, a.right[a.hop:])
			a.right = a.right[:keep]
		}
	} else {
		a.left = a.left[:0]
		a.right = a.right[:0]
		a.skip = a.hop - waveformFFTSize
	}
	a.windowStart += a.hop
}

func (a *waveformSpectrumAnalyzer) analyzeWindow() {
	data := a.fftScratch
	for i := range data {
		realPart := a.left[i] * a.window[i]
		imagPart := 0.0
		if a.channels > 1 && i < len(a.right) {
			imagPart = a.right[i] * a.window[i]
		}
		data[i] = complex(realPart, imagPart)
	}
	fftComplex(data)

	var sums [waveformBandCount]float64
	var counts [waveformBandCount]int
	var spectrogramMax [spectrogramBandCount]float64
	var spectrogramPresent [spectrogramBandCount]bool
	maxFreq := math.Min(20000, a.rate*0.5)
	if a.windowSum <= 0 {
		return
	}
	for k := 1; k < waveformFFTSize/2; k++ {
		freq := float64(k) * a.rate / waveformFFTSize
		band := waveformFrequencyBand(freq)
		if band < 0 {
			continue
		}
		var power float64
		if a.channels > 1 {
			mirror := cmplx.Conj(data[waveformFFTSize-k])
			leftFFT := (data[k] + mirror) * 0.5
			rightFFT := (data[k] - mirror) * complex(0, -0.5)
			leftAmp := 2 * cmplx.Abs(leftFFT) / a.windowSum
			rightAmp := 2 * cmplx.Abs(rightFFT) / a.windowSum
			power = (leftAmp*leftAmp + rightAmp*rightAmp) * 0.5
		} else {
			amp := 2 * cmplx.Abs(data[k]) / a.windowSum
			power = amp * amp
		}
		sums[band] += power
		counts[band]++
		if specBand := spectrogramBandForFrequency(freq, maxFreq); specBand >= 0 {
			amp := math.Sqrt(power)
			spectrogramPresent[specBand] = true
			if amp > spectrogramMax[specBand] {
				spectrogramMax[specBand] = amp
			}
		}
	}

	frame := waveformSpectrumFrame{center: a.windowStart + waveformFFTSize/2}
	frame.bands = waveformBandSalience(sums, counts)
	quantizeSpectrogramMagnitudes(frame.spectrogram[:], spectrogramMax[:], spectrogramPresent[:])
	a.frames = append(a.frames, frame)
}

func (a *waveformSpectrumAnalyzer) Spectrogram(columns int) SpectrogramData {
	if columns < 1 || len(a.frames) == 0 {
		return SpectrogramData{}
	}
	values := make([]uint8, columns*spectrogramBandCount)
	frameIndex := 0
	for column := 0; column < columns; column++ {
		center := (float64(column) + 0.5) * float64(a.totalFrames) / float64(columns)
		for frameIndex+1 < len(a.frames) && float64(a.frames[frameIndex+1].center) <= center {
			frameIndex++
		}
		left := a.frames[frameIndex]
		right := left
		if frameIndex+1 < len(a.frames) {
			right = a.frames[frameIndex+1]
		}
		t := 0.0
		if right.center > left.center {
			t = (center - float64(left.center)) / float64(right.center-left.center)
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		base := column * spectrogramBandCount
		for band := 0; band < spectrogramBandCount; band++ {
			v := float64(left.spectrogram[band]) + t*float64(int(right.spectrogram[band])-int(left.spectrogram[band]))
			values[base+band] = uint8(math.Round(v))
		}
	}
	return SpectrogramData{
		Columns:  columns,
		Bands:    spectrogramBandCount,
		Rate:     uint32(math.Round(a.rate)),
		Duration: float64(a.totalFrames) / a.rate,
		Values:   values,
	}
}

func waveformBandSalience(sums [waveformBandCount]float64, counts [waveformBandCount]int) [waveformBandCount]float32 {
	var rawDB [waveformBandCount]float64
	var tiltedDB [waveformBandCount]float64
	maxRaw := math.Inf(-1)
	maxTilted := math.Inf(-1)
	for band := 0; band < waveformBandCount; band++ {
		if counts[band] == 0 {
			rawDB[band] = math.Inf(-1)
			tiltedDB[band] = math.Inf(-1)
			continue
		}
		meanPower := sums[band] / float64(counts[band])
		rawDB[band] = 10 * math.Log10(math.Max(meanPower, 1e-20))
		centerFreq := math.Sqrt(waveformBandEdges[band] * waveformBandEdges[band+1])
		tiltedDB[band] = rawDB[band] + waveformTiltDB*math.Log2(centerFreq/1000)
		if rawDB[band] > maxRaw {
			maxRaw = rawDB[band]
		}
		if tiltedDB[band] > maxTilted {
			maxTilted = tiltedDB[band]
		}
	}
	var result [waveformBandCount]float32
	if maxRaw < -80 || math.IsInf(maxTilted, -1) {
		return result
	}
	for band := range result {
		if math.IsInf(tiltedDB[band], -1) {
			continue
		}
		delta := tiltedDB[band] - maxTilted
		if delta < -36 {
			continue
		}
		result[band] = float32(math.Exp(delta / waveformSoftmaxDB))
	}
	return result
}

func (a *waveformSpectrumAnalyzer) Apply(points []WavePoint) {
	if len(points) == 0 {
		return
	}
	// Analyze a final zero-padded window for short files and for any remaining
	// tail that was not covered by the regular hop sequence.
	if len(a.left) > 0 && (len(a.frames) == 0 || a.windowStart+waveformFFTSize/2 < a.totalFrames) {
		for len(a.left) < waveformFFTSize {
			a.left = append(a.left, 0)
			if a.channels > 1 {
				a.right = append(a.right, 0)
			}
		}
		a.analyzeWindow()
	}
	if len(a.frames) == 0 {
		return
	}

	frameIndex := 0
	for i := range points {
		center := (float64(i) + 0.5) * float64(a.totalFrames) / float64(len(points))
		for frameIndex+1 < len(a.frames) && float64(a.frames[frameIndex+1].center) <= center {
			frameIndex++
		}
		left := a.frames[frameIndex]
		right := left
		if frameIndex+1 < len(a.frames) {
			right = a.frames[frameIndex+1]
		}
		t := 0.0
		if right.center > left.center {
			t = (center - float64(left.center)) / float64(right.center-left.center)
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		for band := 0; band < waveformBandCount; band++ {
			points[i].Bands[band] = left.bands[band] + float32(t)*(right.bands[band]-left.bands[band])
		}
	}

	binSeconds := float64(a.totalFrames) / a.rate / float64(len(points))
	alpha := float32(1 - math.Exp(-binSeconds/0.04))
	if alpha < 0.001 {
		alpha = 0.001
	} else if alpha > 1 {
		alpha = 1
	}
	var forward [waveformBandCount]float32
	for i := range points {
		for band := range forward {
			forward[band] += (points[i].Bands[band] - forward[band]) * alpha
			points[i].Bands[band] = forward[band]
		}
	}
	var backward [waveformBandCount]float32
	for i := len(points) - 1; i >= 0; i-- {
		for band := range backward {
			backward[band] += (points[i].Bands[band] - backward[band]) * alpha
			points[i].Bands[band] = (points[i].Bands[band] + backward[band]) * 0.5
		}
		peak := maxf(maxf(absf(points[i].MinL), absf(points[i].MaxL)), maxf(absf(points[i].MinR), absf(points[i].MaxR)))
		if peak < 0.00001 {
			points[i].Bands = [waveformBandCount]float32{}
			continue
		}
		points[i].Bands, _ = dominantWaveformBands(points[i].Bands)
	}
}

func dominantWaveformBands(bands [waveformBandCount]float32) ([waveformBandCount]float32, bool) {
	first, second := -1, -1
	for band, value := range bands {
		if value <= 0 {
			continue
		}
		if first < 0 || value > bands[first] {
			second = first
			first = band
		} else if second < 0 || value > bands[second] {
			second = band
		}
	}
	var result [waveformBandCount]float32
	if first < 0 {
		return result, false
	}
	total := bands[first]
	result[first] = bands[first]
	if second >= 0 {
		result[second] = bands[second]
		total += bands[second]
	}
	if total < 0.0001 {
		return [waveformBandCount]float32{}, false
	}
	result[first] /= total
	if second >= 0 {
		result[second] /= total
	}
	return result, true
}

func waveformFrequencyBand(freq float64) int {
	if freq < waveformBandEdges[0] || freq >= waveformBandEdges[len(waveformBandEdges)-1] {
		return -1
	}
	for i := 0; i < waveformBandCount; i++ {
		if freq < waveformBandEdges[i+1] {
			return i
		}
	}
	return -1
}

// waveformBandsFromFFT converts the already-acquired live spectrum into the
// same five perceptual colors used by cached file waveforms. It runs on the UI
// thread; the real-time audio callback only captures the waveform envelope.
func waveformBandsFromFFT(fft []float32, rate uint32) [waveformBandCount]float32 {
	var sums [waveformBandCount]float64
	var counts [waveformBandCount]int
	if len(fft) < 2 {
		return [waveformBandCount]float32{}
	}
	if rate == 0 {
		rate = 48000
	}
	fftSize := len(fft) * 2
	for bin := 1; bin < len(fft); bin++ {
		freq := float64(bin) * float64(rate) / float64(fftSize)
		band := waveformFrequencyBand(freq)
		if band < 0 {
			continue
		}
		amplitude := float64(fft[bin])
		sums[band] += amplitude * amplitude
		counts[band]++
	}
	bands := waveformBandSalience(sums, counts)
	bands, _ = dominantWaveformBands(bands)
	return bands
}

func fftComplex(values []complex128) {
	n := len(values)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j &^= bit
		}
		j |= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		step := cmplx.Rect(1, angle)
		for start := 0; start < n; start += length {
			weight := complex(1.0, 0)
			half := length / 2
			for offset := 0; offset < half; offset++ {
				even := values[start+offset]
				odd := values[start+offset+half] * weight
				values[start+offset] = even + odd
				values[start+offset+half] = even - odd
				weight *= step
			}
		}
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
