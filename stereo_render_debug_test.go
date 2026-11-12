//go:build windows

package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Render comparison harness for LINE-mode smoothing work. Disabled unless
// AURALUX_RENDER_DEBUG points at an output directory; AURALUX_RENDER_TAG
// distinguishes before/after runs.
func TestStereoLineRenderDebug(t *testing.T) {
	outDir := os.Getenv("AURALUX_RENDER_DEBUG")
	if outDir == "" {
		t.Skip("set AURALUX_RENDER_DEBUG to render comparison frames")
	}
	tag := os.Getenv("AURALUX_RENDER_TAG")
	if tag == "" {
		tag = "out"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if v := os.Getenv("AURALUX_CORNER_SMOOTH"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoPhosphorCornerSmooth = float32(f)
		}
	}
	if v := os.Getenv("AURALUX_CORNER_PASSES"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			stereoPhosphorCornerPasses = n
		}
	}
	if v := os.Getenv("AURALUX_CURVE_MIN_SPACING"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoPhosphorCurveMinSpacing = float32(f)
		}
	}
	if v := os.Getenv("AURALUX_SPEED_EXP"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoPhosphorSpeedEnergyExponent = f
		}
	}
	if v := os.Getenv("AURALUX_DISPLAY_GAIN"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoLineDisplayGain = float32(f)
		}
	}
	if v := os.Getenv("AURALUX_LOWPASS_HZ"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoLineLowpassHz = float32(f)
		}
	}
	if v := os.Getenv("AURALUX_FRESH_MS"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			stereoLineFreshWindowSec = float32(f) / 1000
		}
	}

	const size = 450
	state := newStereoPhosphorTestState(size, size)
	state.dc, state.bitmap = 1, 1
	// The harness feeds 96 points per 120 Hz tick, so its effective collection
	// rate is 11520 points per second; the gesture low-pass keys off this.
	state.setLinePointRate(11520)

	// Synthetic music-like stereo stream: two slow irrational-ratio oscillators
	// forming the lissajous body, a faster component for texture, plus noise so
	// the path carries the same kind of jitter a real 120 Hz snapshot has.
	signal := os.Getenv("AURALUX_SIGNAL")
	var sequence uint64
	sampleIndex := 0
	noise := uint64(0x123456789abcdef)
	nextNoise := func() float64 {
		noise ^= noise << 13
		noise ^= noise >> 7
		noise ^= noise << 17
		return float64(noise>>11)/(1<<53) - 0.5
	}
	makePoints := func(n int) []StereoPoint {
		points := make([]StereoPoint, n)
		for i := range points {
			t := float64(sampleIndex) / 12000
			var x, y float64
			switch signal {
			case "circle":
				// Pure geometric fidelity probe: a constant-radius circle.
				x = 0.7 * math.Sin(2*math.Pi*3*t)
				y = 0.7 * math.Cos(2*math.Pi*3*t)
			case "sweep":
				// Transient-brightness probe: a slow orbit alternates with a
				// burst of fast single-pass diagonal sweeps (~25x speed). With
				// sqrt energy the sweeps read ~5x dimmer per pixel.
				phase := sampleIndex % 2400
				if phase < 1200 {
					x = 0.5 * math.Sin(2*math.Pi*3*t)
					y = 0.5 * math.Cos(2*math.Pi*3*t)
				} else {
					p := float64(phase-1200) / 1200
					u := 6 * p
					tri := math.Abs(u-math.Floor(u+0.5)) * 2
					x = -0.7 + 1.4*tri
					y = x
				}
			case "speedmix":
				// Brightness-vs-speed probe: alternating slow and fast orbits
				// at identical amplitude must read at comparable brightness.
				freq := 2.0
				if (sampleIndex/2400)%2 == 1 {
					freq = 16.0
				}
				x = 0.6 * math.Sin(2*math.Pi*freq*t)
				y = 0.6 * math.Cos(2*math.Pi*freq*t)
			default:
				x = 0.62*math.Sin(2*math.Pi*3.07*t) + 0.22*math.Sin(2*math.Pi*17.9*t+1.3) + 0.05*nextNoise()
				y = 0.62*math.Sin(2*math.Pi*2.83*t+0.6) + 0.22*math.Sin(2*math.Pi*23.1*t+0.4) + 0.05*nextNoise()
				if sampleIndex%1200 > 1100 {
					x += 0.3 * math.Sin(2*math.Pi*47*t)
					y -= 0.3 * math.Sin(2*math.Pi*53*t+0.2)
				}
			}
			points[i] = StereoPoint{X: float32(x), Y: float32(y)}
			sampleIndex++
		}
		return points
	}
	for tick := uint64(1); tick <= 240; tick++ {
		points := makePoints(96)
		sequence += uint64(len(points))
		state.advanceStyle(points, sequence, tick, 1, 0, size, size)
	}
	// Capture immediately after the last content tick so the phosphor trails
	// are still at full visibility (like a live screenshot).

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const gain = 4.0 // display-only boost so faint trails are evaluable
	for i := 0; i < size*size; i++ {
		off := i * 4
		a := float64(state.pixels[off+3]) / 255
		// Composite the premultiplied BGRA stroke over the app's deep surface tone.
		img.Pix[off+0] = uint8(min(255, float64(state.pixels[off+2])*gain + 16*(1-a)))
		img.Pix[off+1] = uint8(min(255, float64(state.pixels[off+1])*gain + 20*(1-a)))
		img.Pix[off+2] = uint8(min(255, float64(state.pixels[off+0])*gain + 26*(1-a)))
		img.Pix[off+3] = 255
	}
	path := filepath.Join(outDir, "line_"+tag+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
