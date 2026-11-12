//go:build windows

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpectrogramFrequencyMapping(t *testing.T) {
	for _, rate := range []float64{44100, 48000, 96000, 192000} {
		maxFreq := math.Min(20000, rate*0.5)
		band := spectrogramBandForFrequency(1000, maxFreq)
		if band < 0 || band >= spectrogramBandCount {
			t.Fatalf("rate %.0f mapped 1 kHz outside the display: %d", rate, band)
		}
		frequency := spectrogramFrequencyForBand(band, maxFreq)
		if math.Abs(math.Log2(frequency/1000)) > 0.03 {
			t.Fatalf("rate %.0f round-trip frequency = %.2f Hz", rate, frequency)
		}
	}
}

func TestSpectrogramScaleRoundTripAndHighFrequencyAllocation(t *testing.T) {
	for _, scale := range []spectrogramFrequencyScale{spectrogramScaleMel, spectrogramScaleLog, spectrogramScaleLinear} {
		for _, freq := range []float64{20, 100, 1000, 10000, 20000} {
			position := spectrogramNormalizedForFrequency(freq, scale, 20, 20000)
			got := spectrogramFrequencyAtNormalized(position, scale, 20, 20000)
			if math.Abs(got-freq) > math.Max(0.001, freq*0.000001) {
				t.Fatalf("scale %d round-trip %.1f Hz = %.6f", scale, freq, got)
			}
		}
	}
	low := spectrogramBandForFrequency(10000, 20000)
	high := spectrogramBandForFrequency(20000, 20000)
	if got := high - low; got < 250 {
		t.Fatalf("linear canonical data gives 10-20 kHz only %d bands", got)
	}
}

func TestSpectrogramFrequencyZoomPreservesAnchorAndBounds(t *testing.T) {
	for _, scale := range []spectrogramFrequencyScale{spectrogramScaleMel, spectrogramScaleLog, spectrogramScaleLinear} {
		minFreq, maxFreq := zoomSpectrogramFrequencyRange(scale, 20, 20000, 20000, 0.75, 0.5)
		anchorBefore := spectrogramFrequencyAtNormalized(0.75, scale, 20, 20000)
		anchorAfter := spectrogramFrequencyAtNormalized(0.75, scale, minFreq, maxFreq)
		if math.Abs(anchorAfter-anchorBefore) > anchorBefore*0.000001 {
			t.Fatalf("scale %d moved zoom anchor %.3f -> %.3f", scale, anchorBefore, anchorAfter)
		}
		for i := 0; i < 12; i++ {
			minFreq, maxFreq = zoomSpectrogramFrequencyRange(scale, minFreq, maxFreq, 20000, 0.5, 0.5)
		}
		fullSpan := spectrogramScaleCoordinate(20000, scale) - spectrogramScaleCoordinate(20, scale)
		span := spectrogramScaleCoordinate(maxFreq, scale) - spectrogramScaleCoordinate(minFreq, scale)
		if span < fullSpan/32-0.000001 {
			t.Fatalf("scale %d exceeded 32x zoom", scale)
		}
		minFreq, maxFreq = panSpectrogramFrequencyRange(scale, minFreq, maxFreq, 20000, 100)
		if maxFreq > 20000.000001 || minFreq < 20 {
			t.Fatalf("scale %d pan escaped bounds: %.3f..%.3f", scale, minFreq, maxFreq)
		}
	}
}

func TestSpectrogramFrequencyLabelsMatchDenseReference(t *testing.T) {
	mask := spectrogramFrequencyLabelMask(spectrogramScaleLog, 20, 20000, 474)
	for i, tick := range spectrogramFrequencyTicks {
		if !mask[i] {
			t.Fatalf("reference-sized panel omitted %.0f Hz", tick.frequency)
		}
	}
	for _, scale := range []spectrogramFrequencyScale{spectrogramScaleMel, spectrogramScaleLog, spectrogramScaleLinear} {
		mask := spectrogramFrequencyLabelMask(scale, 10000, 20000, 240)
		lastY := int32(-1000)
		for i, tick := range spectrogramFrequencyTicks {
			if !mask[i] {
				continue
			}
			y := spectrogramFrequencyTickOffset(tick.frequency, scale, 10000, 20000, 240)
			if lastY >= 0 && y-lastY < 12 {
				t.Fatalf("scale %d high-frequency labels collide at %.0f Hz", scale, tick.frequency)
			}
			lastY = y
		}
	}
	compact := spectrogramFrequencyLabelMask(spectrogramScaleLog, 20, 20000, 120)
	for i, tick := range spectrogramFrequencyTicks {
		if tick.major && !compact[i] {
			t.Fatalf("compact panel omitted major %.0f Hz label", tick.frequency)
		}
		if !compact[i] {
			continue
		}
		y := spectrogramFrequencyTickOffset(tick.frequency, spectrogramScaleLog, 20, 20000, 120)
		for j := 0; j < i; j++ {
			if compact[j] {
				otherY := spectrogramFrequencyTickOffset(spectrogramFrequencyTicks[j].frequency, spectrogramScaleLog, 20, 20000, 120)
				if math.Abs(float64(y-otherY)) < 12 {
					t.Fatalf("compact labels %.0f and %.0f Hz collide", tick.frequency, spectrogramFrequencyTicks[j].frequency)
				}
			}
		}
	}
}

func TestFormatPitch(t *testing.T) {
	for _, test := range []struct {
		freq float64
		want string
	}{
		{440, "A4 +0 ct"},
		{261.625565, "C4 +0 ct"},
		{445, "A4 +20 ct"},
		{432, "A4 -32 ct"},
		{0, "--"},
	} {
		if got := formatPitch(test.freq); got != test.want {
			t.Errorf("formatPitch(%v) = %q, want %q", test.freq, got, test.want)
		}
	}
}

func TestCombineIndividualFFTPowerDoesNotCancel(t *testing.T) {
	individual := []float32{0.2, 0.2, 0.5, 0.5, 1, 1}
	dst := make([]float32, 3)
	got := combineIndividualFFT(dst, individual, 2)
	for i, want := range []float32{0.2, 0.5, 1} {
		if math.Abs(float64(got[i]-want)) > 0.000001 {
			t.Fatalf("bin %d = %v, want %v", i, got[i], want)
		}
	}
	// Individual magnitudes contain no phase term. Reversing the right-channel
	// waveform phase therefore cannot reduce the power-combined result.
	antiPhaseMagnitudes := []float32{0.2, 0.2, 0.5, 0.5, 1, 1}
	anti := combineIndividualFFT(make([]float32, 3), antiPhaseMagnitudes, 2)
	for i := range got {
		if got[i] != anti[i] {
			t.Fatalf("anti-phase magnitude changed bin %d: %v vs %v", i, got[i], anti[i])
		}
	}
}

func TestCombineIndividualFFTMonoHasNoGainChange(t *testing.T) {
	input := []float32{0.125, 0.5, 1.25}
	got := combineIndividualFFT(make([]float32, len(input)), input, 1)
	for i := range input {
		if got[i] != input[i] {
			t.Fatalf("mono bin %d = %v, want %v", i, got[i], input[i])
		}
	}
}

func TestOfflineSpectrogramSameAndAntiPhaseMatch(t *testing.T) {
	const rate = 48000
	const frames = waveformFFTSize * 3
	build := func(sign float64) SpectrogramData {
		a := newWaveformSpectrumAnalyzer(rate, 2, frames, 32)
		for i := 0; i < frames; i++ {
			v := float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/rate))
			a.Add(v, float32(sign)*v)
		}
		return a.Spectrogram(32)
	}
	same, anti := build(1), build(-1)
	if len(same.Values) == 0 || len(same.Values) != len(anti.Values) {
		t.Fatal("offline spectrogram was not generated")
	}
	band := spectrogramBandForFrequency(1000, rate/2)
	for column := 1; column < 30; column++ {
		a := same.Values[column*spectrogramBandCount+band]
		b := anti.Values[column*spectrogramBandCount+band]
		if delta := int(a) - int(b); delta < -1 || delta > 1 {
			t.Fatalf("column %d same/anti values differ: %d vs %d", column, a, b)
		}
	}
}

func TestOfflineSpectrogramSilenceHotSignalAndShortFile(t *testing.T) {
	const rate = 48000
	build := func(frames int, amplitude float64) SpectrogramData {
		a := newWaveformSpectrumAnalyzer(rate, 1, uint64(frames), 16)
		for i := 0; i < frames; i++ {
			v := float32(amplitude * math.Sin(2*math.Pi*1000*float64(i)/rate))
			a.Add(v, v)
		}
		points := make([]WavePoint, 16)
		a.Apply(points)
		return a.Spectrogram(16)
	}
	silence := build(400, 0)
	for _, value := range silence.Values {
		if value != 0 {
			t.Fatal("silence produced visible spectrogram energy")
		}
	}
	short := build(400, 0.5)
	band := spectrogramBandForFrequency(1000, rate/2)
	peak := uint8(0)
	for column := 0; column < short.Columns; column++ {
		if v := short.Values[column*short.Bands+band]; v > peak {
			peak = v
		}
	}
	if peak == 0 {
		t.Fatal("short signal did not produce a spectrogram")
	}
	hot := build(waveformFFTSize*2, 2.0)
	peak = 0
	for column := 0; column < hot.Columns; column++ {
		if v := hot.Values[column*hot.Bands+band]; v > peak {
			peak = v
		}
	}
	if peak < 250 {
		t.Fatalf("hot signal peak = %d, want near 0 dBFS ceiling", peak)
	}
}

func TestLiveSpectrogramRingAndFreeze(t *testing.T) {
	var state liveSpectrogramState
	fft := make([]float32, 1024)
	fft[43] = 0.5
	for i := 0; i < liveSpectrogramMaxColumns+7; i++ {
		state.appendFFT(fft, 48000, float64(i)/60)
	}
	if state.count != liveSpectrogramMaxColumns || state.write != 7 {
		t.Fatalf("ring = count %d write %d", state.count, state.write)
	}
	beforeWrite := state.write
	state.frozen = true
	state.appendFFT(fft, 48000, 100)
	if state.write != beforeWrite {
		t.Fatal("frozen live spectrogram advanced")
	}
	state.frozen = false
	state.appendFFT(fft, 48000, 101)
	if state.write != (beforeWrite+1)%liveSpectrogramMaxColumns {
		t.Fatal("live spectrogram did not resume")
	}
}

func TestLiveSpectrogramRetainsNarrowHighFrequencyPeak(t *testing.T) {
	const rate = 48000
	fft := make([]float32, 8192)
	bin := int(math.Round(18000 * float64(len(fft)*2) / rate))
	fft[bin] = 0.75
	column := make([]uint8, spectrogramBandCount)
	spectrogramColumnFromFFT(column, fft, rate)
	band := spectrogramBandForFrequency(18000, 20000)
	if column[band] < quantizeSpectrogramDB(20*math.Log10(0.7)) {
		t.Fatalf("18 kHz peak lost in canonical data: band %d value %d", band, column[band])
	}
}

func TestLiveSpectrogramInterpolatesTimeAndFrequency(t *testing.T) {
	var state liveSpectrogramState
	state.count, state.write = 2, 2
	state.times[0], state.times[1] = 0, 1
	state.values[100] = 20
	state.values[101] = 40
	state.values[spectrogramBandCount+100] = 100
	state.values[spectrogramBandCount+101] = 120
	if got := state.sampleAtTime(0.5, 0, 2, 100.5); math.Abs(got-70) > 0.000001 {
		t.Fatalf("bilinear live sample = %.3f, want 70", got)
	}
	if got := state.sampleAtTime(-0.1, 0, 2, 100); got != 0 {
		t.Fatalf("sample before live history = %.3f, want 0", got)
	}
}

func TestTrackSpectrogramInterpolatesTimeAndFrequency(t *testing.T) {
	data := SpectrogramData{Columns: 2, Bands: 2, Values: []uint8{0, 40, 80, 120}}
	if got := sampleTrackSpectrogram(data, 0.5, 0.5); math.Abs(got-60) > 0.000001 {
		t.Fatalf("bilinear track sample = %.3f, want 60", got)
	}
}

func TestSpectrogramCacheRoundTripAndCorruption(t *testing.T) {
	values := make([]uint8, trackSpectrogramColumns*spectrogramBandCount)
	for i := range values {
		values[i] = uint8(i * 31)
	}
	want := SpectrogramData{Columns: trackSpectrogramColumns, Bands: spectrogramBandCount, Rate: 48000, Duration: 123.5, Values: values}
	encoded, err := encodeSpectrogramCache(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decodeSpectrogramCache(encoded)
	if !ok || got.Columns != want.Columns || got.Bands != want.Bands || got.Rate != want.Rate || got.Duration != want.Duration || len(got.Values) != len(values) {
		t.Fatalf("cache round-trip failed: ok=%v got=%+v", ok, got)
	}
	if got.Values[17003] != values[17003] {
		t.Fatal("cache payload changed")
	}
	encoded[len(encoded)-1] ^= 0xff
	if _, ok := decodeSpectrogramCache(encoded); ok {
		t.Fatal("corrupt cache was accepted")
	}
	valid, _ := encodeSpectrogramCache(want)
	binary.LittleEndian.PutUint32(valid[8:12], spectrogramCacheVersion+1)
	if _, ok := decodeSpectrogramCache(valid); ok {
		t.Fatal("future cache version was accepted")
	}
}

func TestPruneSpectrogramCacheUsesOldestAccessTime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, name := range []string{"old.spc", "middle.spc", "new.spc"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, 10), 0644); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	pruneSpectrogramCache(dir, 20)
	if _, err := os.Stat(filepath.Join(dir, "old.spc")); !os.IsNotExist(err) {
		t.Fatal("oldest cache entry was not pruned")
	}
	for _, name := range []string{"middle.spc", "new.spc"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("newer cache entry %s was pruned: %v", name, err)
		}
	}
}

func TestSpectrogramHeaderControlsFitMinimumLayout(t *testing.T) {
	a := &App{playlistWidth: 260, rightWidth: 410, fileInfoHeight: 220, loudnessHeight: 260, meterHeight: 318, analyzerView: analyzerViewLiveSpectrogram}
	l := a.calculateLayout(RECT{Right: 1180, Bottom: 720})
	controls := []RECT{l.AnalyzerViewBtn, l.SpectrumFFTBtn, l.SpectrogramScaleBtn, l.SpectrogramTimeBtn, l.SpectrogramRangeBtn, l.SpectrogramFreezeBtn}
	for i, control := range controls {
		if control.Left < l.Spectrum.Left+12 || control.Right > l.Spectrum.Right-12 {
			t.Fatalf("control %d exceeds spectrum header: %+v in %+v", i, control, l.Spectrum)
		}
	}
	for i := 1; i < len(controls); i++ {
		if controls[i-1].Right > controls[i].Left {
			t.Fatalf("controls %d and %d overlap: %+v %+v", i-1, i, controls[i-1], controls[i])
		}
	}
}

func TestAnalyzerViewToggleHasOnlyTwoStates(t *testing.T) {
	if got := nextAnalyzerView(analyzerViewSpectrum); got != analyzerViewLiveSpectrogram {
		t.Fatalf("spectrum toggled to %d, want spectrogram", got)
	}
	if got := nextAnalyzerView(analyzerViewLiveSpectrogram); got != analyzerViewSpectrum {
		t.Fatalf("spectrogram toggled to %d, want spectrum", got)
	}
	if got := nextAnalyzerView(analyzerViewTrackSpectrogram); got != analyzerViewSpectrum {
		t.Fatalf("legacy track view toggled to %d, want spectrum", got)
	}
}

func TestTrackRendererPreservesTransientWhenDownsampling(t *testing.T) {
	data := SpectrogramData{Columns: 16, Bands: spectrogramBandCount, Values: make([]uint8, 16*spectrogramBandCount)}
	data.Values[5*spectrogramBandCount+100] = 255
	surface := spectrogramSurface{width: 1, height: spectrogramBandCount / 2, pixels: make([]byte, spectrogramBandCount/2*4), theme: -1}
	surface.buildPalette(0)
	surface.renderTrack(data, 1, 0, 1, 120, spectrogramScaleLinear, 20, 20000, 20000)
	found := false
	for y := 0; y < surface.height; y++ {
		o := y * 4
		color := uint32(surface.pixels[o]) | uint32(surface.pixels[o+1])<<8 | uint32(surface.pixels[o+2])<<16 | uint32(surface.pixels[o+3])<<24
		if color == surface.palette[255] {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("downsampled transient was not preserved at full intensity")
	}
}

func TestSpectrogramRenderCacheKey(t *testing.T) {
	data := SpectrogramData{Columns: 16, Bands: spectrogramBandCount, Values: make([]uint8, 16*spectrogramBandCount)}
	data.Values[5*spectrogramBandCount+100] = 255
	newSurface := func() *spectrogramSurface {
		s := &spectrogramSurface{width: 8, height: 16, pixels: make([]byte, 8*16*4), theme: -1}
		s.buildPalette(0)
		return s
	}

	// Track view: an identical key must skip the render entirely.
	surface := newSurface()
	surface.renderTrack(data, 7, 0, 1, 120, spectrogramScaleLinear, 20, 20000, 20000)
	if !surface.keyValid {
		t.Fatal("render did not record its cache key")
	}
	clear(surface.pixels) // a re-render would repaint; a cache hit must not
	surface.renderTrack(data, 7, 0, 1, 120, spectrogramScaleLinear, 20, 20000, 20000)
	for i, b := range surface.pixels {
		if b != 0 {
			t.Fatalf("identical key triggered a re-render (pixel %d = %d)", i, b)
		}
	}
	surface.renderTrack(data, 8, 0, 1, 120, spectrogramScaleLinear, 20, 20000, 20000)
	if surface.pixels[3] != 255 {
		t.Fatal("changed token did not trigger a re-render")
	}

	// Live view: the state sequence drives the key, so a frozen/paused state
	// hits the cache and a new FFT column re-renders.
	var state liveSpectrogramState
	fft := make([]float32, 1024)
	fft[43] = 0.5
	state.appendFFT(fft, 48000, 0)
	surface.renderLive(&state, 6, 90, spectrogramScaleMel, 20, 20000, 20000)
	clear(surface.pixels)
	surface.renderLive(&state, 6, 90, spectrogramScaleMel, 20, 20000, 20000)
	for i, b := range surface.pixels {
		if b != 0 {
			t.Fatalf("unchanged live sequence triggered a re-render (pixel %d = %d)", i, b)
		}
	}
	state.appendFFT(fft, 48000, 1.0/60)
	surface.renderLive(&state, 6, 90, spectrogramScaleMel, 20, 20000, 20000)
	if surface.pixels[3] != 255 {
		t.Fatal("new FFT column did not trigger a re-render")
	}
}

func TestSpectrogramSurfaceDIBLifecycle(t *testing.T) {
	var surface spectrogramSurface
	if !surface.ensure(320, 120, 0) {
		t.Fatal("could not create spectrogram DIB")
	}
	var state liveSpectrogramState
	fft := make([]float32, 1024)
	fft[43] = 0.5
	state.appendFFT(fft, 48000, 0)
	surface.renderLive(&state, 6, 90, spectrogramScaleMel, 20, 20000, 20000)
	if surface.dc == 0 || surface.bitmap == 0 || len(surface.pixels) != 320*120*4 {
		t.Fatal("spectrogram DIB was not populated")
	}
	for i := 3; i < len(surface.pixels); i += 4 {
		if surface.pixels[i] != 255 {
			t.Fatalf("pixel alpha at %d = %d", i/4, surface.pixels[i])
		}
	}
	surface.destroy()
	if surface.dc != 0 || surface.bitmap != 0 || surface.pixels != nil {
		t.Fatal("spectrogram DIB resources were not released")
	}
}

func BenchmarkSpectrogramRenderTrack(b *testing.B) {
	data := SpectrogramData{Columns: trackSpectrogramColumns, Bands: spectrogramBandCount, Rate: 48000, Duration: 180, Values: make([]uint8, trackSpectrogramColumns*spectrogramBandCount)}
	for i := range data.Values {
		data.Values[i] = uint8(i * 17)
	}
	surface := spectrogramSurface{
		width: 800, height: 280, pixels: make([]byte, 800*280*4),
		rowBands: make([]float64, 280), rowBandLow: make([]float64, 280), rowBandHigh: make([]float64, 280), theme: -1,
	}
	surface.buildPalette(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		surface.renderTrack(data, uint64(i), 0, 1, 90, spectrogramScaleMel, 20, 20000, 20000)
	}
}

func BenchmarkSpectrogramRenderLive(b *testing.B) {
	var state liveSpectrogramState
	fft := make([]float32, 8192)
	for bin := range fft {
		fft[bin] = float32((bin%97)+1) / 100
	}
	for column := 0; column < 360; column++ {
		state.appendFFT(fft, 48000, float64(column)/60)
	}
	surface := spectrogramSurface{
		width: 800, height: 280, pixels: make([]byte, 800*280*4),
		rowBands: make([]float64, 280), rowBandLow: make([]float64, 280), rowBandHigh: make([]float64, 280), theme: -1,
	}
	surface.buildPalette(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.seq++ // force a new render key so the benchmark measures rendering, not the cache hit
		surface.renderLive(&state, 6, 90, spectrogramScaleMel, 20, 20000, 20000)
	}
}
