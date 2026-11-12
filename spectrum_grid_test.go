package main

import (
	"math"
	"testing"
)

func TestSpectrumGridCacheMatchesDirectComputation(t *testing.T) {
	inner := RECT{Left: 46, Top: 38, Right: 1000, Bottom: 300}
	bands := 384

	var g spectrumGridCache
	g.ensure(inner, bands)

	w := rectW(inner)
	h := rectH(inner)
	denom := float64(bands - 1)
	for i := 0; i < bands; i++ {
		wantX := inner.Left + int32((float64(i)/denom)*float64(w-1))
		if g.curveX[i] != wantX {
			t.Fatalf("curveX[%d] = %d, want %d", i, g.curveX[i], wantX)
		}
		wantF := 20 * math.Pow(10, 3*float64(i)/denom)
		if math.Abs(g.freqHz[i]-wantF) > 1e-9*wantF {
			t.Fatalf("freqHz[%d] = %v, want %v", i, g.freqHz[i], wantF)
		}
	}
	if math.Abs(g.freqHz[0]-20) > 1e-9 {
		t.Fatalf("freqHz[0] = %v, want 20", g.freqHz[0])
	}
	if math.Abs(g.freqHz[bands-1]-20000) > 1e-6 {
		t.Fatalf("freqHz[last] = %v, want 20000", g.freqHz[bands-1])
	}
	if g.dbY[0] != inner.Top || g.dbY[9] != inner.Top+int32(float64(h)) {
		t.Fatalf("dbY endpoints wrong: %d %d", g.dbY[0], g.dbY[9])
	}
	// Frequency axis is logarithmic: 20..20000 over 3 decades.
	if g.freqX[0] != inner.Left {
		t.Fatalf("freqX[0] = %d, want %d", g.freqX[0], inner.Left)
	}
	if g.freqX[9] != inner.Left+int32(w-1) {
		t.Fatalf("freqX[9] = %d, want %d", g.freqX[9], inner.Left+int32(w-1))
	}
}

func TestSpectrumGridCacheReuseAndInvalidate(t *testing.T) {
	inner := RECT{Left: 46, Top: 38, Right: 1000, Bottom: 300}

	var g spectrumGridCache
	g.ensure(inner, 384)
	first := g
	g.ensure(inner, 384)
	if g != first {
		t.Fatalf("same geometry must reuse cached values")
	}

	// Resize invalidates and recomputes.
	wider := inner
	wider.Right += 100
	g.ensure(wider, 384)
	if g.width != rectW(wider) || g.curveX[383] == first.curveX[383] {
		t.Fatalf("resize must recompute curveX")
	}

	// Band-count change invalidates too.
	g.ensure(wider, 96)
	if g.bands != 96 {
		t.Fatalf("band change must recompute cache header")
	}
	if math.Abs(g.freqHz[95]-20000) > 1e-6 {
		t.Fatalf("freqHz[95] with 96 bands = %v, want 20000", g.freqHz[95])
	}
}

func TestSpectrumSmoothWeights(t *testing.T) {
	for i, w := range spectrumSmoothWeights {
		want := 1.0 / float64(i+1)
		if w != want {
			t.Fatalf("spectrumSmoothWeights[%d] = %v, want %v", i, w, want)
		}
	}
}
