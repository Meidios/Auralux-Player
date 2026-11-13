//go:build windows

package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"
)

const (
	spectrogramBandCount       = 512
	spectrogramTickCount       = 22
	trackSpectrogramColumns    = 12000
	liveSpectrogramMaxColumns  = 1800
	spectrogramCacheVersion    = 2
	spectrogramCacheLimitBytes = int64(256 << 20)
)

var spectrogramCacheMagic = [8]byte{'A', 'U', 'R', 'S', 'P', 'E', 'C', '2'}

type spectrogramFrequencyScale uint8

const (
	spectrogramScaleMel spectrogramFrequencyScale = iota
	spectrogramScaleLog
	spectrogramScaleLinear
)

type spectrogramFrequencyTick struct {
	frequency float64
	major     bool
}

var spectrogramFrequencyTicks = [spectrogramTickCount]spectrogramFrequencyTick{
	{20000, false}, {15000, false}, {12000, false}, {10000, true},
	{9000, false}, {8000, false}, {7000, false}, {6000, false}, {5000, false},
	{4000, false}, {3500, false}, {3000, false}, {2500, false}, {2000, false},
	{1500, false}, {1200, false}, {1000, true}, {700, false}, {500, false},
	{300, false}, {200, false}, {100, true},
}

// SpectrogramData stores absolute magnitudes quantized over -120..0 dBFS.
// Values are column-major so one time slice can be appended or copied cheaply.
type SpectrogramData struct {
	Columns  int
	Bands    int
	Rate     uint32
	Duration float64
	Values   []uint8
}

func quantizeSpectrogramDB(db float64) uint8 {
	if math.IsNaN(db) || db <= -120 {
		return 0
	}
	if db >= 0 {
		return 255
	}
	return uint8(math.Round((db + 120) * (255.0 / 120.0)))
}

func dequantizeSpectrogramDB(v uint8) float64 {
	return float64(v)*(120.0/255.0) - 120
}

func spectrogramBandForFrequency(freq, maxFreq float64) int {
	if maxFreq > 20000 {
		maxFreq = 20000
	}
	if maxFreq <= 20 || freq < 20 || freq > maxFreq {
		return -1
	}
	band := int(math.Round(freq / maxFreq * float64(spectrogramBandCount-1)))
	if band < 0 {
		return -1
	}
	if band >= spectrogramBandCount {
		band = spectrogramBandCount - 1
	}
	return band
}

func spectrogramFrequencyForBand(band int, maxFreq float64) float64 {
	if maxFreq > 20000 {
		maxFreq = 20000
	}
	if maxFreq <= 20 {
		return 20
	}
	if band < 0 {
		band = 0
	}
	if band >= spectrogramBandCount {
		band = spectrogramBandCount - 1
	}
	return float64(band) / float64(spectrogramBandCount-1) * maxFreq
}

func spectrogramScaleCoordinate(freq float64, scale spectrogramFrequencyScale) float64 {
	switch scale {
	case spectrogramScaleLinear:
		return freq
	case spectrogramScaleLog:
		return math.Log(math.Max(freq, 1))
	default:
		return math.Log1p(math.Max(freq, 0) / 700)
	}
}

func spectrogramFrequencyFromScaleCoordinate(value float64, scale spectrogramFrequencyScale) float64 {
	switch scale {
	case spectrogramScaleLinear:
		return value
	case spectrogramScaleLog:
		return math.Exp(value)
	default:
		return 700 * math.Expm1(value)
	}
}

func spectrogramFrequencyAtNormalized(position float64, scale spectrogramFrequencyScale, minFreq, maxFreq float64) float64 {
	if maxFreq <= minFreq {
		return minFreq
	}
	if position < 0 {
		position = 0
	} else if position > 1 {
		position = 1
	}
	lo := spectrogramScaleCoordinate(minFreq, scale)
	hi := spectrogramScaleCoordinate(maxFreq, scale)
	value := lo + position*(hi-lo)
	return spectrogramFrequencyFromScaleCoordinate(value, scale)
}

func spectrogramNormalizedForFrequency(freq float64, scale spectrogramFrequencyScale, minFreq, maxFreq float64) float64 {
	if maxFreq <= minFreq {
		return 0
	}
	lo := spectrogramScaleCoordinate(minFreq, scale)
	hi := spectrogramScaleCoordinate(maxFreq, scale)
	return (spectrogramScaleCoordinate(freq, scale) - lo) / (hi - lo)
}

func spectrogramLinearBandPosition(freq, maxFreq float64, bands int) float64 {
	if bands <= 1 || maxFreq <= 0 {
		return 0
	}
	position := freq / maxFreq * float64(bands-1)
	if position < 0 {
		return 0
	}
	if position > float64(bands-1) {
		return float64(bands - 1)
	}
	return position
}

func normalizedSpectrogramFrequencyRange(minFreq, maxFreq, dataMaxFreq float64) (float64, float64) {
	const fullMin = 20.0
	if dataMaxFreq < fullMin {
		dataMaxFreq = fullMin
	}
	if minFreq < fullMin || maxFreq <= minFreq {
		return fullMin, dataMaxFreq
	}
	if maxFreq > dataMaxFreq {
		maxFreq = dataMaxFreq
	}
	if minFreq >= maxFreq {
		return fullMin, dataMaxFreq
	}
	return minFreq, maxFreq
}

func zoomSpectrogramFrequencyRange(scale spectrogramFrequencyScale, minFreq, maxFreq, dataMaxFreq, anchor, factor float64) (float64, float64) {
	minFreq, maxFreq = normalizedSpectrogramFrequencyRange(minFreq, maxFreq, dataMaxFreq)
	if anchor < 0 {
		anchor = 0
	} else if anchor > 1 {
		anchor = 1
	}
	fullLo := spectrogramScaleCoordinate(20, scale)
	fullHi := spectrogramScaleCoordinate(dataMaxFreq, scale)
	lo := spectrogramScaleCoordinate(minFreq, scale)
	hi := spectrogramScaleCoordinate(maxFreq, scale)
	span := hi - lo
	newSpan := span * factor
	minSpan := (fullHi - fullLo) / 32
	if newSpan < minSpan {
		newSpan = minSpan
	}
	if newSpan > fullHi-fullLo {
		newSpan = fullHi - fullLo
	}
	anchorCoord := lo + anchor*span
	newLo := anchorCoord - anchor*newSpan
	newHi := newLo + newSpan
	if newLo < fullLo {
		newHi += fullLo - newLo
		newLo = fullLo
	}
	if newHi > fullHi {
		newLo -= newHi - fullHi
		newHi = fullHi
	}
	return spectrogramFrequencyFromScaleCoordinate(newLo, scale), spectrogramFrequencyFromScaleCoordinate(newHi, scale)
}

func panSpectrogramFrequencyRange(scale spectrogramFrequencyScale, minFreq, maxFreq, dataMaxFreq, fraction float64) (float64, float64) {
	minFreq, maxFreq = normalizedSpectrogramFrequencyRange(minFreq, maxFreq, dataMaxFreq)
	fullLo := spectrogramScaleCoordinate(20, scale)
	fullHi := spectrogramScaleCoordinate(dataMaxFreq, scale)
	lo := spectrogramScaleCoordinate(minFreq, scale)
	hi := spectrogramScaleCoordinate(maxFreq, scale)
	shift := (hi - lo) * fraction
	lo += shift
	hi += shift
	if lo < fullLo {
		hi += fullLo - lo
		lo = fullLo
	}
	if hi > fullHi {
		lo -= hi - fullHi
		hi = fullHi
	}
	return spectrogramFrequencyFromScaleCoordinate(lo, scale), spectrogramFrequencyFromScaleCoordinate(hi, scale)
}

func spectrogramFrequencyTickOffset(freq float64, scale spectrogramFrequencyScale, minFreq, maxFreq float64, height int32) int32 {
	if height <= 0 || maxFreq <= minFreq || freq < minFreq {
		return height
	}
	t := spectrogramNormalizedForFrequency(freq, scale, minFreq, maxFreq)
	offset := height - int32(math.Round(t*float64(height)))
	if offset < 0 {
		return 0
	}
	if offset > height {
		return height
	}
	return offset
}

func spectrogramFrequencyLabelMask(scale spectrogramFrequencyScale, minFreq, maxFreq float64, height int32) [spectrogramTickCount]bool {
	var visible [spectrogramTickCount]bool
	// At the reference-sized panel every Insight-style label fits. Compact
	// layouts keep the three decade anchors and add as many minor labels as
	// possible without letting glyphs collide.
	if height >= 420 && scale != spectrogramScaleLinear {
		for i, tick := range spectrogramFrequencyTicks {
			visible[i] = tick.frequency >= minFreq && tick.frequency <= maxFreq
		}
		return visible
	}
	minGap := int32(12)
	if height < 220 {
		minGap = 14
	}
	tryAdd := func(i int) {
		tick := spectrogramFrequencyTicks[i]
		if tick.frequency < minFreq || tick.frequency > maxFreq {
			return
		}
		y := spectrogramFrequencyTickOffset(tick.frequency, scale, minFreq, maxFreq, height)
		for j, other := range spectrogramFrequencyTicks {
			if !visible[j] {
				continue
			}
			otherY := spectrogramFrequencyTickOffset(other.frequency, scale, minFreq, maxFreq, height)
			if math.Abs(float64(y-otherY)) < float64(minGap) {
				return
			}
		}
		visible[i] = true
	}
	for i, tick := range spectrogramFrequencyTicks {
		if tick.major {
			tryAdd(i)
		}
	}
	for i, tick := range spectrogramFrequencyTicks {
		if !tick.major {
			tryAdd(i)
		}
	}
	return visible
}

func formatPitch(freq float64) string {
	if freq <= 0 || math.IsNaN(freq) || math.IsInf(freq, 0) {
		return "--"
	}
	names := [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	midiFloat := 69 + 12*math.Log2(freq/440)
	midi := int(math.Round(midiFloat))
	cents := int(math.Round((midiFloat - float64(midi)) * 100))
	note := midi % 12
	if note < 0 {
		note += 12
	}
	octave := int(math.Floor(float64(midi)/12)) - 1
	return fmt.Sprintf("%s%d %+d ct", names[note], octave, cents)
}

func combineIndividualFFT(dst, individual []float32, channels int) []float32 {
	if channels < 1 {
		channels = 1
	}
	bins := len(individual) / channels
	if bins > len(dst) {
		bins = len(dst)
	}
	for bin := 0; bin < bins; bin++ {
		l := individual[bin*channels]
		if channels == 1 {
			dst[bin] = l
			continue
		}
		r := individual[bin*channels+1]
		dst[bin] = float32(math.Sqrt(float64(l*l+r*r) * 0.5))
	}
	return dst[:bins]
}

func spectrogramColumnFromFFT(dst []uint8, fft []float32, rate uint32) {
	if len(dst) < spectrogramBandCount {
		return
	}
	clear(dst[:spectrogramBandCount])
	if len(fft) < 2 || rate == 0 {
		return
	}
	maxFreq := math.Min(20000, float64(rate)*0.5)
	fftSize := len(fft) * 2
	var maxima [spectrogramBandCount]float64
	var present [spectrogramBandCount]bool
	for bin := 1; bin < len(fft); bin++ {
		freq := float64(bin) * float64(rate) / float64(fftSize)
		band := spectrogramBandForFrequency(freq, maxFreq)
		if band >= 0 {
			present[band] = true
			if amp := float64(fft[bin]); amp > maxima[band] {
				maxima[band] = amp
			}
		}
	}
	quantizeSpectrogramMagnitudes(dst, maxima[:], present[:])
}

func quantizeSpectrogramMagnitudes(dst []uint8, maxima []float64, present []bool) {
	clear(dst)
	limit := len(dst)
	if len(maxima) < limit {
		limit = len(maxima)
	}
	if len(present) < limit {
		limit = len(present)
	}
	previous := -1
	for band := 0; band < limit; band++ {
		if !present[band] {
			continue
		}
		value := uint8(0)
		if maxima[band] > 0 {
			value = quantizeSpectrogramDB(20 * math.Log10(maxima[band]))
		}
		dst[band] = value
		if previous >= 0 && band > previous+1 {
			left := float64(dst[previous])
			for gap := previous + 1; gap < band; gap++ {
				t := float64(gap-previous) / float64(band-previous)
				dst[gap] = uint8(math.Round(left + (float64(value)-left)*t))
			}
		}
		previous = band
	}
	if previous >= 0 {
		for band := previous + 1; band < limit; band++ {
			dst[band] = dst[previous]
		}
	}
}

type liveSpectrogramState struct {
	values [liveSpectrogramMaxColumns * spectrogramBandCount]uint8
	times  [liveSpectrogramMaxColumns]float64
	write  int
	count  int
	frozen bool
	seq    uint64 // increments on every appendFFT and reset; used as a render key input
}

func (s *liveSpectrogramState) reset() {
	clear(s.values[:])
	clear(s.times[:])
	s.write, s.count = 0, 0
	s.seq++
}

func (s *liveSpectrogramState) appendFFT(fft []float32, rate uint32, at float64) {
	if s.frozen {
		return
	}
	base := s.write * spectrogramBandCount
	spectrogramColumnFromFFT(s.values[base:base+spectrogramBandCount], fft, rate)
	s.times[s.write] = at
	s.write = (s.write + 1) % liveSpectrogramMaxColumns
	if s.count < liveSpectrogramMaxColumns {
		s.count++
	}
	s.seq++
}

func (s *liveSpectrogramState) chronologicalIndex(index int) int {
	start := s.write - s.count
	if start < 0 {
		start += liveSpectrogramMaxColumns
	}
	return (start + index) % liveSpectrogramMaxColumns
}

func (s *liveSpectrogramState) nextTime(step float64) float64 {
	if s.count == 0 {
		return 0
	}
	last := s.times[s.chronologicalIndex(s.count-1)]
	return last + step
}

func (s *liveSpectrogramState) visibleRange(seconds float64) (start, count int, first, last float64) {
	if s.count == 0 {
		return 0, 0, 0, 0
	}
	lastIndex := s.chronologicalIndex(s.count - 1)
	last = s.times[lastIndex]
	cutoff := last - seconds
	start = 0
	for start < s.count-1 && s.times[s.chronologicalIndex(start)] < cutoff {
		start++
	}
	count = s.count - start
	first = s.times[s.chronologicalIndex(start)]
	return
}

func (s *liveSpectrogramState) valueAt(visibleStart, visibleCount, x, width, band int) uint8 {
	if visibleCount <= 0 || width <= 0 || band < 0 || band >= spectrogramBandCount {
		return 0
	}
	column := visibleStart + x*visibleCount/width
	if column >= visibleStart+visibleCount {
		column = visibleStart + visibleCount - 1
	}
	physical := s.chronologicalIndex(column)
	return s.values[physical*spectrogramBandCount+band]
}

func (s *liveSpectrogramState) valueAtTime(target float64, visibleStart, visibleCount, band int) uint8 {
	if visibleCount <= 0 || band < 0 || band >= spectrogramBandCount {
		return 0
	}
	firstIndex := s.chronologicalIndex(visibleStart)
	lastIndex := s.chronologicalIndex(visibleStart + visibleCount - 1)
	first, last := s.times[firstIndex], s.times[lastIndex]
	if target < first {
		return 0
	}
	column := visibleStart + visibleCount - 1
	if last > first && target < last {
		column = visibleStart + int((target-first)/(last-first)*float64(visibleCount-1))
	}
	physical := s.chronologicalIndex(column)
	return s.values[physical*spectrogramBandCount+band]
}

func (s *liveSpectrogramState) sampleColumnsAtTime(target float64, visibleStart, visibleCount int) (left, right int, fraction float64, ok bool) {
	if visibleCount <= 0 {
		return 0, 0, 0, false
	}
	first := s.times[s.chronologicalIndex(visibleStart)]
	lastOffset := visibleCount - 1
	last := s.times[s.chronologicalIndex(visibleStart+lastOffset)]
	if target < first {
		return 0, 0, 0, false
	}
	if target >= last || visibleCount == 1 {
		physical := s.chronologicalIndex(visibleStart + lastOffset)
		return physical, physical, 0, true
	}

	lo, hi := 0, lastOffset
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if s.times[s.chronologicalIndex(visibleStart+mid)] <= target {
			lo = mid
		} else {
			hi = mid
		}
	}
	leftTime := s.times[s.chronologicalIndex(visibleStart+lo)]
	rightTime := s.times[s.chronologicalIndex(visibleStart+hi)]
	if rightTime > leftTime {
		fraction = (target - leftTime) / (rightTime - leftTime)
	}
	return s.chronologicalIndex(visibleStart + lo), s.chronologicalIndex(visibleStart + hi), fraction, true
}

func sampleSpectrogramBands(values []uint8, base, bands int, bandPosition float64) float64 {
	if bands <= 0 || base < 0 || base+bands > len(values) {
		return 0
	}
	if bandPosition <= 0 {
		return float64(values[base])
	}
	if bandPosition >= float64(bands-1) {
		return float64(values[base+bands-1])
	}
	low := int(math.Floor(bandPosition))
	fraction := bandPosition - float64(low)
	return float64(values[base+low]) + (float64(values[base+low+1])-float64(values[base+low]))*fraction
}

func (s *liveSpectrogramState) sampleAtTime(target float64, visibleStart, visibleCount int, bandPosition float64) float64 {
	left, right, fraction, ok := s.sampleColumnsAtTime(target, visibleStart, visibleCount)
	if !ok {
		return 0
	}
	a := sampleSpectrogramBands(s.values[:], left*spectrogramBandCount, spectrogramBandCount, bandPosition)
	if left == right {
		return a
	}
	b := sampleSpectrogramBands(s.values[:], right*spectrogramBandCount, spectrogramBandCount, bandPosition)
	return a + (b-a)*fraction
}

// spectrogramRenderKey identifies every input that can change the rendered
// pixels. All fields are comparable, so a single == check decides whether the
// DIB can be reused as-is.
type spectrogramRenderKey struct {
	width, height int
	theme         int
	rangeDB       int
	scale         spectrogramFrequencyScale
	minFreq       float64
	maxFreq       float64
	dataMaxFreq   float64

	// Live view inputs.
	liveSeq     uint64  // liveSpectrogramState append counter
	liveSeconds float64 // time window

	// Track view inputs.
	trackToken uint64 // App.spectrogramToken at render time
	trackReady bool   // len(Values) > 0; flips when analysis lands
	viewStart  float64
	viewEnd    float64
}

type spectrogramSurface struct {
	width, height int
	dc, bitmap    uintptr
	oldBitmap     uintptr
	pixels        []byte
	rowBands      []float64
	rowBandLow    []float64
	rowBandHigh   []float64
	palette       [256]uint32
	theme         int
	key           spectrogramRenderKey
	keyValid      bool
}

func (s *spectrogramSurface) destroy() {
	if s.dc != 0 {
		if s.bitmap != 0 {
			if s.oldBitmap != 0 {
				procSelectObject.Call(s.dc, s.oldBitmap)
			}
			procDeleteObject.Call(s.bitmap)
		}
		procDeleteDC.Call(s.dc)
	}
	s.width, s.height = 0, 0
	s.dc, s.bitmap, s.oldBitmap = 0, 0, 0
	s.pixels = nil
	s.rowBands = nil
	s.rowBandLow = nil
	s.rowBandHigh = nil
	s.keyValid = false // a rebuilt DIB is blank; the next paint must render
}

func (s *spectrogramSurface) ensure(width, height, theme int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	if s.width != width || s.height != height || s.dc == 0 {
		s.destroy()
		bmi := BITMAPINFO{}
		bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
		bmi.BmiHeader.BiWidth = int32(width)
		bmi.BmiHeader.BiHeight = -int32(height)
		bmi.BmiHeader.BiPlanes = 1
		bmi.BmiHeader.BiBitCount = 32
		bmi.BmiHeader.BiCompression = BI_RGB
		var bits uintptr
		bitmap, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
		if bitmap == 0 || bits == 0 {
			return false
		}
		dc, _, _ := procCreateCompatibleDC.Call(0)
		if dc == 0 {
			procDeleteObject.Call(bitmap)
			return false
		}
		old, _, _ := procSelectObject.Call(dc, bitmap)
		if old == 0 {
			procDeleteDC.Call(dc)
			procDeleteObject.Call(bitmap)
			return false
		}
		s.width, s.height = width, height
		s.dc, s.bitmap, s.oldBitmap = dc, bitmap, old
		s.pixels = unsafe.Slice((*byte)(unsafe.Pointer(bits)), width*height*4)
		s.rowBands = make([]float64, height)
		s.rowBandLow = make([]float64, height)
		s.rowBandHigh = make([]float64, height)
		s.theme = -1
	}
	if s.theme != theme {
		s.buildPalette(theme)
		s.theme = theme
	}
	return true
}

func lerpByte(a, b byte, t float64) byte {
	return byte(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

func (s *spectrogramSurface) buildPalette(theme int) {
	type stop struct {
		at      float64
		r, g, b byte
	}
	dark := []stop{{0, 7, 9, 13}, {.18, 28, 22, 66}, {.42, 32, 91, 150}, {.66, 28, 180, 190}, {.84, 232, 165, 62}, {1, 252, 250, 238}}
	day := []stop{{0, 244, 246, 249}, {.18, 210, 218, 234}, {.42, 70, 91, 170}, {.66, 25, 139, 154}, {.84, 211, 116, 39}, {1, 91, 25, 16}}
	stops := dark
	if theme != 0 {
		stops = day
	}
	for i := range s.palette {
		t := float64(i) / 255
		lo, hi := stops[0], stops[len(stops)-1]
		for j := 1; j < len(stops); j++ {
			if t <= stops[j].at {
				lo, hi = stops[j-1], stops[j]
				break
			}
		}
		local := 0.0
		if hi.at > lo.at {
			local = (t - lo.at) / (hi.at - lo.at)
		}
		r, g, b := lerpByte(lo.r, hi.r, local), lerpByte(lo.g, hi.g, local), lerpByte(lo.b, hi.b, local)
		s.palette[i] = uint32(b) | uint32(g)<<8 | uint32(r)<<16 | 0xff000000
	}
}

func remapSpectrogramValue(v uint8, rangeDB int) uint8 {
	return remapSpectrogramSample(float64(v), rangeDB)
}

func remapSpectrogramSample(v float64, rangeDB int) uint8 {
	if rangeDB != 60 && rangeDB != 120 {
		rangeDB = 90
	}
	if v < 0 {
		v = 0
	} else if v > 255 {
		v = 255
	}
	db := v*(120.0/255.0) - 120
	floor := -float64(rangeDB)
	if db <= floor {
		return 0
	}
	if db >= 0 {
		return 255
	}
	return uint8(math.Round((db - floor) * 255 / -floor))
}

func (s *spectrogramSurface) put(index int, color uint32) {
	o := index * 4
	s.pixels[o] = byte(color)
	s.pixels[o+1] = byte(color >> 8)
	s.pixels[o+2] = byte(color >> 16)
	s.pixels[o+3] = 255
}

func (s *spectrogramSurface) prepareRowBands(scale spectrogramFrequencyScale, minFreq, maxFreq, dataMaxFreq float64, bands int) {
	if len(s.rowBands) != s.height || len(s.rowBandLow) != s.height || len(s.rowBandHigh) != s.height {
		s.rowBands = make([]float64, s.height)
		s.rowBandLow = make([]float64, s.height)
		s.rowBandHigh = make([]float64, s.height)
	}
	for y := 0; y < s.height; y++ {
		center := 1 - (float64(y)+0.5)/float64(s.height)
		top := 1 - float64(y)/float64(s.height)
		bottom := 1 - float64(y+1)/float64(s.height)
		s.rowBands[y] = spectrogramLinearBandPosition(spectrogramFrequencyAtNormalized(center, scale, minFreq, maxFreq), dataMaxFreq, bands)
		s.rowBandLow[y] = spectrogramLinearBandPosition(spectrogramFrequencyAtNormalized(bottom, scale, minFreq, maxFreq), dataMaxFreq, bands)
		s.rowBandHigh[y] = spectrogramLinearBandPosition(spectrogramFrequencyAtNormalized(top, scale, minFreq, maxFreq), dataMaxFreq, bands)
	}
}

func sampleSpectrogramBandSpan(values []uint8, base, bands int, center, low, high float64) float64 {
	if high < low {
		low, high = high, low
	}
	if high-low < 1 {
		return sampleSpectrogramBands(values, base, bands, center)
	}
	peak := sampleSpectrogramBands(values, base, bands, low)
	if edge := sampleSpectrogramBands(values, base, bands, high); edge > peak {
		peak = edge
	}
	start, end := int(math.Ceil(low)), int(math.Floor(high))
	if start < 0 {
		start = 0
	}
	if end >= bands {
		end = bands - 1
	}
	for band := start; band <= end; band++ {
		if value := float64(values[base+band]); value > peak {
			peak = value
		}
	}
	return peak
}

func (s *spectrogramSurface) renderLive(state *liveSpectrogramState, seconds float64, rangeDB int, scale spectrogramFrequencyScale, minFreq, maxFreq, dataMaxFreq float64) {
	key := spectrogramRenderKey{
		width: s.width, height: s.height, theme: s.theme, rangeDB: rangeDB,
		scale: scale, minFreq: minFreq, maxFreq: maxFreq, dataMaxFreq: dataMaxFreq,
		liveSeq: state.seq, liveSeconds: seconds,
	}
	if s.keyValid && s.key == key {
		return // unchanged inputs: keep the DIB, caller still AlphaBlends it
	}
	start, count, _, last := state.visibleRange(seconds)
	s.prepareRowBands(scale, minFreq, maxFreq, dataMaxFreq, spectrogramBandCount)
	// A pixel is a time interval. Keep the strongest measurement in it, and
	// hold the previous measurement between frames rather than anticipating
	// the next attack. Include the newest frame at the right edge.
	next := start
	for x := 0; x < s.width; x++ {
		edge := last - seconds + float64(x+1)/float64(s.width)*seconds
		if x == s.width-1 {
			edge = last
		}
		first := next
		for next < start+count && state.times[state.chronologicalIndex(next)] <= edge {
			next++
		}
		if first == next && first > start {
			first--
		}
		for y := 0; y < s.height; y++ {
			v := 0.0
			for i := first; i < next; i++ {
				base := state.chronologicalIndex(i) * spectrogramBandCount
				v = max(v, sampleSpectrogramBandSpan(state.values[:], base, spectrogramBandCount, s.rowBands[y], s.rowBandLow[y], s.rowBandHigh[y]))
			}
			s.put(y*s.width+x, s.palette[remapSpectrogramSample(v, rangeDB)])
		}
	}
	s.key, s.keyValid = key, true
}

func spectrogramBandPositionForRow(row, height, bands int) float64 {
	if height <= 1 || bands <= 1 {
		return 0
	}
	return float64(height-1-row) * float64(bands-1) / float64(height-1)
}

func sampleTrackSpectrogram(data SpectrogramData, columnPosition, bandPosition float64) float64 {
	if data.Columns <= 0 || data.Bands <= 0 || len(data.Values) < data.Columns*data.Bands {
		return 0
	}
	if columnPosition <= 0 {
		return sampleSpectrogramBands(data.Values, 0, data.Bands, bandPosition)
	}
	if columnPosition >= float64(data.Columns-1) {
		return sampleSpectrogramBands(data.Values, (data.Columns-1)*data.Bands, data.Bands, bandPosition)
	}
	left := int(math.Floor(columnPosition))
	fraction := columnPosition - float64(left)
	a := sampleSpectrogramBands(data.Values, left*data.Bands, data.Bands, bandPosition)
	b := sampleSpectrogramBands(data.Values, (left+1)*data.Bands, data.Bands, bandPosition)
	return a + (b-a)*fraction
}

func sampleTrackSpectrogramSpan(data SpectrogramData, columnPosition, bandPosition, bandLow, bandHigh float64) float64 {
	if data.Columns <= 0 || data.Bands <= 0 || len(data.Values) < data.Columns*data.Bands {
		return 0
	}
	if columnPosition <= 0 {
		return sampleSpectrogramBandSpan(data.Values, 0, data.Bands, bandPosition, bandLow, bandHigh)
	}
	if columnPosition >= float64(data.Columns-1) {
		return sampleSpectrogramBandSpan(data.Values, (data.Columns-1)*data.Bands, data.Bands, bandPosition, bandLow, bandHigh)
	}
	left := int(math.Floor(columnPosition))
	fraction := columnPosition - float64(left)
	a := sampleSpectrogramBandSpan(data.Values, left*data.Bands, data.Bands, bandPosition, bandLow, bandHigh)
	b := sampleSpectrogramBandSpan(data.Values, (left+1)*data.Bands, data.Bands, bandPosition, bandLow, bandHigh)
	return a + (b-a)*fraction
}

func (s *spectrogramSurface) renderTrack(data SpectrogramData, token uint64, viewStart, viewEnd float64, rangeDB int, scale spectrogramFrequencyScale, minFreq, maxFreq, dataMaxFreq float64) {
	key := spectrogramRenderKey{
		width: s.width, height: s.height, theme: s.theme, rangeDB: rangeDB,
		scale: scale, minFreq: minFreq, maxFreq: maxFreq, dataMaxFreq: dataMaxFreq,
		trackToken: token, trackReady: len(data.Values) > 0,
		viewStart: viewStart, viewEnd: viewEnd,
	}
	if s.keyValid && s.key == key {
		return
	}
	if data.Columns <= 0 || data.Bands <= 0 || len(data.Values) < data.Columns*data.Bands {
		clear(s.pixels)
		s.key, s.keyValid = key, true
		return
	}
	if viewStart < 0 {
		viewStart = 0
	}
	if viewEnd > 1 {
		viewEnd = 1
	}
	s.prepareRowBands(scale, minFreq, maxFreq, dataMaxFreq, data.Bands)
	sourceColumnsPerPixel := (viewEnd - viewStart) * float64(data.Columns) / float64(s.width)
	if sourceColumnsPerPixel < 1 {
		for x := 0; x < s.width; x++ {
			ratio := viewStart + (float64(x)+0.5)/float64(s.width)*(viewEnd-viewStart)
			columnPosition := ratio*float64(data.Columns) - 0.5
			for y := 0; y < s.height; y++ {
				bandPosition := s.rowBands[y]
				v := sampleTrackSpectrogramSpan(data, columnPosition, bandPosition, s.rowBandLow[y], s.rowBandHigh[y])
				s.put(y*s.width+x, s.palette[remapSpectrogramSample(v, rangeDB)])
			}
		}
		s.key, s.keyValid = key, true
		return
	}

	var maxima [spectrogramBandCount]uint8
	for x := 0; x < s.width; x++ {
		clear(maxima[:])
		ratio0 := viewStart + float64(x)/float64(s.width)*(viewEnd-viewStart)
		ratio1 := viewStart + float64(x+1)/float64(s.width)*(viewEnd-viewStart)
		column0 := int(math.Floor(ratio0 * float64(data.Columns)))
		column1 := int(math.Ceil(ratio1 * float64(data.Columns)))
		if column0 < 0 {
			column0 = 0
		}
		if column1 <= column0 {
			column1 = column0 + 1
		}
		if column1 > data.Columns {
			column1 = data.Columns
		}
		for column := column0; column < column1; column++ {
			values := data.Values[column*data.Bands : (column+1)*data.Bands]
			for band, value := range values {
				if value > maxima[band] {
					maxima[band] = value
				}
			}
		}
		for y := 0; y < s.height; y++ {
			bandPosition := s.rowBands[y]
			v := sampleSpectrogramBandSpan(maxima[:], 0, data.Bands, bandPosition, s.rowBandLow[y], s.rowBandHigh[y])
			s.put(y*s.width+x, s.palette[remapSpectrogramSample(v, rangeDB)])
		}
	}
	s.key, s.keyValid = key, true
}

func (s *spectrogramSurface) draw(dc uintptr, destination RECT) bool {
	if s.dc == 0 || rectW(destination) <= 0 || rectH(destination) <= 0 {
		return false
	}
	blend := uint32(AC_SRC_OVER) | uint32(255)<<16 | uint32(AC_SRC_ALPHA)<<24
	ok, _, _ := procAlphaBlend.Call(dc, uintptr(destination.Left), uintptr(destination.Top), uintptr(rectW(destination)), uintptr(rectH(destination)), s.dc, 0, 0, uintptr(s.width), uintptr(s.height), uintptr(blend))
	return ok != 0
}

func spectrogramCachePath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	seed := fmt.Sprintf("v%d|%s|%d|%d|%d|%d", spectrogramCacheVersion, strings.ToLower(filepath.Clean(path)), info.Size(), info.ModTime().UnixNano(), trackSpectrogramColumns, spectrogramBandCount)
	sum := sha256.Sum256([]byte(seed))
	dir := filepath.Join(appDataDir(), "spectrograms")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%x.spc", sum[:16])), nil
}

func encodeSpectrogramCache(data SpectrogramData) ([]byte, error) {
	if data.Columns <= 0 || data.Bands <= 0 || len(data.Values) != data.Columns*data.Bands {
		return nil, fmt.Errorf("invalid spectrogram dimensions")
	}
	var payload bytes.Buffer
	zw := zlib.NewWriter(&payload)
	if _, err := zw.Write(data.Values); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, spectrogramCacheMagic)
	_ = binary.Write(&out, binary.LittleEndian, uint32(spectrogramCacheVersion))
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Columns))
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Bands))
	_ = binary.Write(&out, binary.LittleEndian, data.Rate)
	_ = binary.Write(&out, binary.LittleEndian, data.Duration)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(data.Values)))
	_ = binary.Write(&out, binary.LittleEndian, crc32.ChecksumIEEE(data.Values))
	_, _ = out.Write(payload.Bytes())
	return out.Bytes(), nil
}

func decodeSpectrogramCache(encoded []byte) (SpectrogramData, bool) {
	const headerSize = 8 + 4*6 + 8
	if len(encoded) < headerSize {
		return SpectrogramData{}, false
	}
	r := bytes.NewReader(encoded)
	var magic [8]byte
	var version, columns, bands, rate, length, checksum uint32
	var duration float64
	for _, v := range []any{&magic, &version, &columns, &bands, &rate, &duration, &length, &checksum} {
		if binary.Read(r, binary.LittleEndian, v) != nil {
			return SpectrogramData{}, false
		}
	}
	if magic != spectrogramCacheMagic || version != spectrogramCacheVersion || columns != trackSpectrogramColumns || bands != spectrogramBandCount || length != columns*bands || length > 64<<20 {
		return SpectrogramData{}, false
	}
	zr, err := zlib.NewReader(r)
	if err != nil {
		return SpectrogramData{}, false
	}
	values, err := io.ReadAll(io.LimitReader(zr, int64(length)+1))
	zr.Close()
	if err != nil || len(values) != int(length) || crc32.ChecksumIEEE(values) != checksum {
		return SpectrogramData{}, false
	}
	return SpectrogramData{Columns: int(columns), Bands: int(bands), Rate: rate, Duration: duration, Values: values}, true
}

func loadSpectrogramCache(path string) (SpectrogramData, bool) {
	cachePath, err := spectrogramCachePath(path)
	if err != nil {
		return SpectrogramData{}, false
	}
	encoded, err := os.ReadFile(cachePath)
	if err != nil {
		return SpectrogramData{}, false
	}
	data, ok := decodeSpectrogramCache(encoded)
	if !ok {
		_ = os.Remove(cachePath)
		return SpectrogramData{}, false
	}
	now := time.Now()
	_ = os.Chtimes(cachePath, now, now)
	return data, true
}

func saveSpectrogramCache(path string, data SpectrogramData) {
	cachePath, err := spectrogramCachePath(path)
	if err != nil {
		return
	}
	encoded, err := encodeSpectrogramCache(data)
	if err != nil {
		return
	}
	tmp := cachePath + ".tmp"
	if os.WriteFile(tmp, encoded, 0644) != nil {
		return
	}
	_ = os.Remove(cachePath)
	if os.Rename(tmp, cachePath) != nil {
		_ = os.Remove(tmp)
		return
	}
	pruneSpectrogramCache(filepath.Dir(cachePath), spectrogramCacheLimitBytes)
}

func pruneSpectrogramCache(dir string, limit int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		path string
		size int64
		time time.Time
	}
	var files []item
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".spc") {
			continue
		}
		if info, err := entry.Info(); err == nil {
			files = append(files, item{filepath.Join(dir, entry.Name()), info.Size(), info.ModTime()})
			total += info.Size()
		}
	}
	if total <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].time.Before(files[j].time) })
	for _, file := range files {
		if total <= limit {
			break
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}
