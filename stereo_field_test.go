package main

import (
	"bytes"
	"math"
	"testing"
)

func TestStereoPaletteChangesOnlyRGB(t *testing.T) {
	defer setTheme(0)
	for theme := 0; theme < 2; theme++ {
		setTheme(theme)
		for style := 0; style < 3; style++ {
			for mode := 0; mode < 3; mode++ {
				s := newStereoPhosphorTestState(101, 101)
				s.dc, s.bitmap = 1, 1
				var points [128]StereoPoint
				for i := range points {
					points[i] = StereoPoint{float32(.5 * math.Sin(float64(i)*.19)), float32(.4 * math.Cos(float64(i)*.23))}
				}
				for tick := 1; tick <= 4; tick++ {
					s.advanceStyle(points[:], uint64(tick*len(points)), uint64(tick), mode, style, 101, 101)
				}
				gold := bytes.Clone(s.pixels)
				s.blue = true
				s.renderStereoColors()
				if bytes.Equal(gold, s.pixels) {
					t.Fatalf("no color change: theme=%d style=%d mode=%d", theme, style, mode)
				}
				for i := 3; i < len(gold); i += 4 {
					if gold[i] != s.pixels[i] {
						t.Fatalf("alpha changed at pixel %d", i/4)
					}
				}
				s.blue = false
				s.renderStereoColors()
				if !bytes.Equal(gold, s.pixels) {
					t.Fatal("switching back changed the original frame")
				}
				s.stopRenderPool()
			}
		}
	}
}

func TestStereoPaletteButtonFitsHeader(t *testing.T) {
	for _, w := range []int32{800, 960, 1024, 1200, 1600} {
		for _, right := range []int32{360, 410, 620} {
			a := App{rightWidth: right}
			l := a.calculateLayout(makeRect(0, 0, w, 800))
			end := l.Stereo.Left
			for _, r := range []RECT{l.StereoColorBtn, l.StereoNormalizeBtn, l.StereoStyleBtn, l.StereoModeBtn} {
				if r.Left < end || r.Right > l.Stereo.Right || rectW(r) < 30 {
					t.Fatalf("window %d, right %d: button %+v outside stereo header %+v or overlapping", w, right, r, l.Stereo)
				}
				end = r.Right
			}
		}
	}
}

func TestLineQuadratureFormsAContinuousOutline(t *testing.T) {
	const size = 201
	s := newStereoPhosphorTestState(size, size)
	s.dc, s.bitmap = 1, 1
	defer s.stopRenderPool()
	s.setLinePointRate(24000)
	var pts [128]StereoPoint
	for i := range pts {
		a := 2 * math.Pi * float64(i) / 24
		pts[i] = StereoPoint{float32(.38 * math.Cos(a)), float32(.5 * math.Sin(a))}
	}
	s.advance(pts[:], 128, 2, 0, size, size)
	for j := 0; j < 48; j++ {
		a := 2 * math.Pi * float64(j) / 48
		p := stereoPhosphorMapPoint(StereoPoint{float32(.38 * math.Cos(a)), float32(.5 * math.Sin(a))}, 0, size, size)
		x, y := int(math.Round(float64(p.X))), int(math.Round(float64(p.Y)))
		var alpha byte
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				alpha = max(alpha, s.pixels[((y+dy)*size+x+dx)*4+3])
			}
		}
		if alpha < 24 {
			t.Fatalf("missing outline at %d degrees", j*360/48)
		}
	}
	if s.pixels[((size/2)*size+size/2)*4+3] != 0 {
		t.Fatal("unexpected trace through the ellipse center")
	}
}

func TestGonioSignalFootprint(t *testing.T) {
	const size = 201
	for _, tc := range []struct {
		name  string
		phase float64
		hz    float64
	}{{"mono", 0, 1000}, {"antiphase", math.Pi, 1000}, {"quadrature", math.Pi / 2, 1000}, {"quadrature4k", math.Pi / 2, 4000}, {"silence", 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStereoPhosphorTestState(size, size)
			s.dc, s.bitmap = 1, 1
			s.setLinePointRate(24000)
			var seq uint64
			for tick := 1; tick <= 32; tick++ {
				var pts [200]StereoPoint
				for i := range pts {
					theta := 2 * math.Pi * tc.hz * float64(seq) / 24000
					l, r := .5*math.Sin(theta), .5*math.Sin(theta+tc.phase)
					pts[i] = StereoPoint{float32((l - r) / math.Sqrt2), float32((l + r) / math.Sqrt2)}
					seq++
				}
				s.advanceStyle(pts[:], seq, uint64(tick), 0, 0, size, size)
			}
			var x, y int
			for i, v := range s.compositeCoverage {
				if v >= 0.05 {
					x = max(x, int(math.Abs(float64(i%size-size/2))))
					y = max(y, int(math.Abs(float64(i/size-size/2))))
				}
			}
			t.Logf("visible half-span: x=%d y=%d px", x, y)
			switch tc.name {
			case "mono":
				if x > 4 || y < 58 {
					t.Fail()
				}
			case "antiphase":
				if y > 4 || x < 58 {
					t.Fail()
				}
			case "silence":
				if x > 4 || y > 4 {
					t.Fail()
				}
			default:
				if x < 42 || y < 42 || math.Abs(float64(x-y)) > 5 {
					t.Fail()
				}
			}
		})
	}
}
