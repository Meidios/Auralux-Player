package main

import "testing"

func TestGDIPenCacheReturnsSameHandle(t *testing.T) {
	destroyGDICache()
	t.Cleanup(destroyGDICache)

	p1 := cachedPen(0x00112233, 1)
	p2 := cachedPen(0x00112233, 1)
	if p1 == 0 || p1 != p2 {
		t.Fatalf("cachedPen should reuse handle, got %x then %x", p1, p2)
	}
	if other := cachedPen(0x00112233, 2); other == p1 {
		t.Fatalf("different width must not share a pen")
	}
	if other := cachedPen(0x00AABBCC, 1); other == p1 {
		t.Fatalf("different color must not share a pen")
	}
	if round := cachedRoundPen(0x00112233, 2); round == p1 || round == 0 {
		t.Fatalf("geometric pen must be distinct and valid, got %x", round)
	}
	if round2 := cachedRoundPen(0x00112233, 2); round2 != cachedRoundPen(0x00112233, 2) {
		t.Fatalf("cachedRoundPen should reuse handle")
	}
}

func TestGDIBrushCacheReturnsSameHandle(t *testing.T) {
	destroyGDICache()
	t.Cleanup(destroyGDICache)

	b1 := cachedBrush(0x00345678)
	b2 := cachedBrush(0x00345678)
	if b1 == 0 || b1 != b2 {
		t.Fatalf("cachedBrush should reuse handle, got %x then %x", b1, b2)
	}
	if other := cachedBrush(0x00ABCDEF); other == b1 {
		t.Fatalf("different color must not share a brush")
	}
}

func TestDestroyGDICacheDropsEntries(t *testing.T) {
	destroyGDICache()

	p := cachedPen(0x00010203, 1)
	b := cachedBrush(0x00040506)
	if p == 0 || b == 0 {
		t.Fatalf("expected valid objects, got pen=%x brush=%x", p, b)
	}
	destroyGDICache()
	if len(gdiPenCache) != 0 || len(gdiBrushCache) != 0 {
		t.Fatalf("destroyGDICache must empty both caches")
	}
	// After a flush the same request must create a fresh valid handle.
	if p2 := cachedPen(0x00010203, 1); p2 == 0 {
		t.Fatalf("cachedPen after destroy returned null")
	}
	destroyGDICache()
}

func TestWaveformColorBrushCache(t *testing.T) {
	a := &App{}
	t.Cleanup(a.destroyWaveBrushes)

	bands := [waveformBandCount]float32{0.9, 0.1, 0.4, 0.2, 0.05}
	b1 := a.waveformColorBrush(bands, false)
	b2 := a.waveformColorBrush(bands, false)
	if b1 == 0 || b1 != b2 {
		t.Fatalf("waveformColorBrush should reuse handle, got %x then %x", b1, b2)
	}
	if inactive := a.waveformColorBrush(bands, true); inactive == b1 {
		t.Fatalf("inactive variant must not share the active brush")
	}
	if empty := a.waveformColorBrush([waveformBandCount]float32{}, false); empty != 0 {
		t.Fatalf("silent bands must return null brush, got %x", empty)
	}
	a.destroyWaveBrushes()
	if a.waveBrushes != nil {
		t.Fatalf("destroyWaveBrushes must clear the cache")
	}
}
