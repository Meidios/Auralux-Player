//go:build windows

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWaveformFrequencyBandBoundaries(t *testing.T) {
	tests := []struct {
		freq float64
		want int
	}{
		{19.9, -1}, {20, 0}, {59.9, 0}, {60, 1}, {249.9, 1},
		{250, 2}, {1999.9, 2}, {2000, 3}, {5999.9, 3},
		{6000, 4}, {19999.9, 4}, {20000, -1},
	}
	for _, test := range tests {
		if got := waveformFrequencyBand(test.freq); got != test.want {
			t.Fatalf("frequency %.1f Hz: got band %d, want %d", test.freq, got, test.want)
		}
	}
}

func TestWaveformSpectrumAnalyzerMapsToneToExpectedBand(t *testing.T) {
	frequencies := []float64{46.875, 117.1875, 703.125, 3515.625, 7968.75}
	for wantBand, frequency := range frequencies {
		t.Run(string(rune('A'+wantBand)), func(t *testing.T) {
			points := analyzeTestTone(frequency, false)
			var average [waveformBandCount]float64
			for _, point := range points[len(points)/4 : len(points)*3/4] {
				for band, value := range point.Bands {
					average[band] += float64(value)
				}
			}
			for band, value := range average {
				if band != wantBand && value >= average[wantBand] {
					t.Fatalf("%.1f Hz: band %d energy %.3f >= expected band %d energy %.3f", frequency, band, value, wantBand, average[wantBand])
				}
			}
		})
	}
}

func TestWaveformSpectrumAnalyzerDoesNotCancelAntiphaseStereo(t *testing.T) {
	points := analyzeTestTone(703.125, true)
	var mid float32
	for _, point := range points {
		mid += point.Bands[2]
	}
	if mid/float32(len(points)) < 0.8 {
		t.Fatalf("antiphase stereo mid-band energy was lost: %.3f", mid/float32(len(points)))
	}
}

func TestWaveformBandsFromFFTMapsLiveToneColors(t *testing.T) {
	const rate = 48000
	frequencies := []float64{46.875, 117.1875, 703.125, 3515.625, 7968.75}
	for wantBand, frequency := range frequencies {
		fft := make([]float32, 1024)
		bin := int(math.Round(frequency * float64(len(fft)*2) / rate))
		fft[bin] = 0.5
		bands := waveformBandsFromFFT(fft, rate)
		winner := 0
		for band := 1; band < waveformBandCount; band++ {
			if bands[band] > bands[winner] {
				winner = band
			}
		}
		if winner != wantBand || bands[wantBand] < 0.9 {
			t.Fatalf("%.1f Hz: got live bands %v, want band %d", frequency, bands, wantBand)
		}
	}
}

func TestLiveWaveformEnvelopeAggregatesCallbackBlocks(t *testing.T) {
	engine := newAudioEngine()
	start := engine.LiveWaveformSequence()
	engine.publishLiveWaveEnvelope(-0.25, 0.4, -0.3, 0.2)
	engine.publishLiveWaveEnvelope(-0.8, 0.1, -0.1, 0.9)
	point, sequence, ok := engine.LiveWaveformEnvelopeSince(start)
	if !ok || sequence != start+2 {
		t.Fatalf("unexpected live waveform snapshot: ok=%v sequence=%d", ok, sequence)
	}
	if point.MinL != -0.8 || point.MaxL != 0.4 || point.MinR != -0.3 || point.MaxR != 0.9 {
		t.Fatalf("callback envelopes were not aggregated: %+v", point)
	}
	if _, _, ok := engine.LiveWaveformEnvelopeSince(sequence); ok {
		t.Fatal("already-consumed live waveform data was returned twice")
	}
}

func TestLiveWaveformPublishDoesNotAllocate(t *testing.T) {
	engine := newAudioEngine()
	allocations := testing.AllocsPerRun(1000, func() {
		engine.publishLiveWaveEnvelope(-0.5, 0.5, -0.4, 0.4)
	})
	if allocations != 0 {
		t.Fatalf("audio callback publication allocated %.2f objects", allocations)
	}
}

func TestAppendLiveWavePointRollsInPlace(t *testing.T) {
	points := []WavePoint{{MaxL: 1}, {MaxL: 2}, {MaxL: 3}}
	appendLiveWavePoint(points, WavePoint{MaxL: 4})
	if points[0].MaxL != 2 || points[1].MaxL != 3 || points[2].MaxL != 4 {
		t.Fatalf("unexpected rolling waveform: %+v", points)
	}
}

func analyzeTestTone(frequency float64, stereoAntiphase bool) []WavePoint {
	const rate = 48000
	const frames = rate
	const bins = 256
	channels := 1
	if stereoAntiphase {
		channels = 2
	}
	analyzer := newWaveformSpectrumAnalyzer(rate, channels, frames, bins)
	points := make([]WavePoint, bins)
	for i := range points {
		points[i] = WavePoint{MinL: -0.5, MaxL: 0.5, MinR: -0.5, MaxR: 0.5}
	}
	for frame := 0; frame < frames; frame++ {
		sample := float32(0.5 * math.Sin(2*math.Pi*frequency*float64(frame)/rate))
		right := sample
		if stereoAntiphase {
			right = -sample
		}
		analyzer.Add(sample, right)
	}
	analyzer.Apply(points)
	return points
}

func TestWaveformCacheVersionAndChannels(t *testing.T) {
	points := make([]WavePoint, 256)
	points[0].Bands[3] = 1
	encoded, err := json.Marshal(waveformCache{Version: 4, Bins: 256, Channels: 1, Points: points})
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok := decodeWaveformCache(encoded, 256)
	if !ok || decoded.Channels != 1 || decoded.Points[0].Bands[3] != 1 {
		t.Fatalf("new waveform cache did not round-trip: ok=%v channels=%d", ok, decoded.Channels)
	}
	legacy, _ := json.Marshal(waveformCache{Version: 3, Bins: 256, Channels: 1, Points: points})
	if _, ok := decodeWaveformCache(legacy, 256); ok {
		t.Fatal("legacy waveform cache should be rejected")
	}
}

func TestWaveformLanesFollowChannelCount(t *testing.T) {
	plot := RECT{Left: 10, Top: 20, Right: 210, Bottom: 120}
	mono := waveformLanes(plot, 1)
	if len(mono) != 1 || mono[0].Top != 23 || mono[0].Bottom != 117 {
		t.Fatalf("unexpected mono lane: %+v", mono)
	}
	stereo := waveformLanes(plot, 2)
	if len(stereo) != 2 || stereo[0].Bottom >= stereo[1].Top {
		t.Fatalf("unexpected stereo lanes: %+v", stereo)
	}
}

func TestWaveformBandColorsAndMixing(t *testing.T) {
	want := []uint32{
		rgb(24, 116, 255), rgb(125, 52, 255), rgb(255, 55, 63),
		rgb(255, 205, 35), rgb(31, 218, 96),
	}
	for band := 0; band < waveformBandCount; band++ {
		var energies [waveformBandCount]float32
		energies[band] = 1
		counts, ok := quantizeWaveformBands(energies)
		if !ok || mixedWaveformColor(counts, false) != want[band] {
			t.Fatalf("band %d did not map to its base color", band)
		}
	}
	mixed, ok := quantizeWaveformBands([waveformBandCount]float32{1, 1})
	if !ok || mixed[0] == 0 || mixed[1] == 0 {
		t.Fatalf("adjacent bands were not mixed: %v", mixed)
	}
	if _, ok := quantizeWaveformBands([waveformBandCount]float32{}); ok {
		t.Fatal("silence should not produce a frequency color")
	}
}

func TestWaveformBandSalienceAppliesFixedTilt(t *testing.T) {
	var sums [waveformBandCount]float64
	var counts [waveformBandCount]int
	for band := 0; band < waveformBandCount; band++ {
		center := math.Sqrt(waveformBandEdges[band] * waveformBandEdges[band+1])
		rawDB := -36 - waveformTiltDB*math.Log2(center/1000)
		sums[band] = math.Pow(10, rawDB/10)
		counts[band] = 1
	}
	weights := waveformBandSalience(sums, counts)
	for band := 1; band < waveformBandCount; band++ {
		if math.Abs(float64(weights[band]-weights[0])) > 0.001 {
			t.Fatalf("tilt-compensated spectrum should be balanced: %v", weights)
		}
	}
}

func TestWaveformBandSalienceSeparatesDominantBandsInWidebandAudio(t *testing.T) {
	for dominant := 0; dominant < waveformBandCount; dominant++ {
		var sums [waveformBandCount]float64
		var counts [waveformBandCount]int
		for band := 0; band < waveformBandCount; band++ {
			center := math.Sqrt(waveformBandEdges[band] * waveformBandEdges[band+1])
			rawDB := -42 - waveformTiltDB*math.Log2(center/1000)
			if band == dominant {
				rawDB += 12
			}
			sums[band] = math.Pow(10, rawDB/10)
			counts[band] = 1
		}
		weights, ok := dominantWaveformBands(waveformBandSalience(sums, counts))
		if !ok || weights[dominant] < 0.9 {
			t.Fatalf("dominant band %d was not visually separated: %v", dominant, weights)
		}
	}
}

func TestDominantWaveformBandsKeepsOnlyTwoBands(t *testing.T) {
	input := [waveformBandCount]float32{0.4, 0.8, 0.2, 1.0, 0.6}
	got, ok := dominantWaveformBands(input)
	if !ok {
		t.Fatal("non-silent bands were rejected")
	}
	active := 0
	var total float32
	for _, value := range got {
		if value > 0 {
			active++
			total += value
		}
	}
	if active != 2 || math.Abs(float64(total-1)) > 0.0001 || got[3] <= got[1] {
		t.Fatalf("expected the two strongest normalized bands, got %v", got)
	}
}

func TestWaveformColorTransitionUsesTwelveSteps(t *testing.T) {
	previousBlue := uint8(waveformColorSteps + 1)
	for step := 0; step <= waveformColorSteps; step++ {
		bands := [waveformBandCount]float32{
			float32(waveformColorSteps-step) / waveformColorSteps,
			float32(step) / waveformColorSteps,
		}
		counts, ok := quantizeWaveformBands(bands)
		if !ok || counts[0] > previousBlue || int(counts[0])+int(counts[1]) != waveformColorSteps {
			t.Fatalf("non-monotonic blue-to-purple transition at step %d: %v", step, counts)
		}
		previousBlue = counts[0]
	}
}

func TestWaveformRealFileIntegration(t *testing.T) {
	audioPath := os.Getenv("AURALUX_WAVEFORM_TEST_FILE")
	if audioPath == "" {
		t.Skip("set AURALUX_WAVEFORM_TEST_FILE to run the BASS integration test")
	}
	engine := newAudioEngine()
	if err := engine.Load(filepath.Join("..", "runtime"), 0); err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	waveform, err := engine.GenerateWaveform(audioPath, 12000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if waveform.Channels < 1 || len(waveform.Points) != 12000 {
		t.Fatalf("unexpected waveform metadata: channels=%d points=%d", waveform.Channels, len(waveform.Points))
	}
	var winners [waveformBandCount]int
	active := 0
	dominanceTotal := 0.0
	for _, point := range waveform.Points {
		winner := -1
		activeBands := 0
		for band, value := range point.Bands {
			if value > 0 {
				activeBands++
				if winner < 0 || value > point.Bands[winner] {
					winner = band
				}
			}
		}
		if activeBands > 2 {
			t.Fatalf("point contains %d active color bands: %v", activeBands, point.Bands)
		}
		if winner >= 0 {
			winners[winner]++
			dominanceTotal += float64(point.Bands[winner])
			active++
		}
	}
	if active == 0 {
		t.Fatal("real file produced no active frequency colors")
	}
	t.Logf("channels=%d active=%d dominant bands=%v average dominance=%.3f", waveform.Channels, active, winners, dominanceTotal/float64(active))
}
