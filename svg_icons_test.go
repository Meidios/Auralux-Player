//go:build windows

package main

import (
	"image/color"
	"testing"
)

func TestEmbeddedThemeSVGIconsRasterize(t *testing.T) {
	tests := []struct {
		path        string
		filledPoint imagePoint
		clearPoint  imagePoint
	}{
		{
			path:        "assets/ui/theme_sun.svg",
			filledPoint: imagePoint{x: 48, y: 48},
			clearPoint:  imagePoint{x: 0, y: 0},
		},
		{
			path:        "assets/ui/theme_moon.svg",
			filledPoint: imagePoint{x: 16, y: 48},
			clearPoint:  imagePoint{x: 60, y: 36},
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			data, err := embeddedUIAssets.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			img, err := rasterizeSVGIcon(data, 96, color.NRGBA{R: 17, G: 34, B: 51, A: 255})
			if err != nil {
				t.Fatal(err)
			}
			if alpha := img.NRGBAAt(test.filledPoint.x, test.filledPoint.y).A; alpha == 0 {
				t.Fatalf("expected geometry at (%d, %d)", test.filledPoint.x, test.filledPoint.y)
			}
			if alpha := img.NRGBAAt(test.clearPoint.x, test.clearPoint.y).A; alpha != 0 {
				t.Fatalf("expected transparency at (%d, %d), got alpha %d", test.clearPoint.x, test.clearPoint.y, alpha)
			}
		})
	}
}

func TestRasterizeSVGIconRejectsInvalidInput(t *testing.T) {
	if _, err := rasterizeSVGIcon([]byte(`<svg viewBox="0 0 0 24"><circle cx="12" cy="12" r="4"/></svg>`), 24, color.NRGBA{A: 255}); err == nil {
		t.Fatal("expected invalid viewBox to be rejected")
	}
	if _, err := rasterizeSVGIcon([]byte(`<svg viewBox="0 0 24 24"><path d="M0 0"/></svg>`), 24, color.NRGBA{A: 255}); err == nil {
		t.Fatal("expected unsupported empty geometry to be rejected")
	}
}

func TestEmbeddedThemeSVGIconsLoadAsUIBitmaps(t *testing.T) {
	assets, err := loadEmbeddedUIAssets()
	if err != nil {
		t.Fatal(err)
	}
	defer destroyUIAssets(assets)

	for _, name := range []string{"theme_sun", "theme_sun_day", "theme_moon", "theme_moon_day"} {
		asset := assets[name]
		if asset == nil || asset.Handle == 0 {
			t.Fatalf("missing UI bitmap %q", name)
		}
		if asset.Width != 96 || asset.Height != 96 {
			t.Fatalf("unexpected %s dimensions: %dx%d", name, asset.Width, asset.Height)
		}
	}
}

type imagePoint struct {
	x int
	y int
}
