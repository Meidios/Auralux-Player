package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitterHoverAssetsAreCenteredAndBorderless(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		vertical bool
	}{
		{"split_handle_h_hover.png", 64, 11, false},
		{"split_handle_h_hover_day.png", 64, 11, false},
		{"split_handle_h_hover_2x.png", 128, 22, false},
		{"split_handle_h_hover_day_2x.png", 128, 22, false},
		{"split_handle_v_hover.png", 11, 64, true},
		{"split_handle_v_hover_day.png", 11, 64, true},
		{"split_handle_v_hover_2x.png", 22, 128, true},
		{"split_handle_v_hover_day_2x.png", 22, 128, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("assets", "ui", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			img, err := png.Decode(file)
			if err != nil {
				t.Fatal(err)
			}
			bounds := img.Bounds()
			if bounds.Dx() != tt.width || bounds.Dy() != tt.height {
				t.Fatalf("size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), tt.width, tt.height)
			}

			cross := bounds.Dy()
			long := bounds.Dx()
			if tt.vertical {
				cross, long = bounds.Dx(), bounds.Dy()
			}
			nonTransparent := 0
			for longPos := 0; longPos < long; longPos++ {
				for crossPos := 0; crossPos < cross; crossPos++ {
					x, y := longPos, crossPos
					mirrorX, mirrorY := longPos, cross-1-crossPos
					if tt.vertical {
						x, y = crossPos, longPos
						mirrorX, mirrorY = cross-1-crossPos, longPos
					}
					_, _, _, alpha := img.At(x, y).RGBA()
					_, _, _, mirrorAlpha := img.At(mirrorX, mirrorY).RGBA()
					if alpha != mirrorAlpha {
						t.Fatalf("alpha is not centered at (%d,%d)", x, y)
					}
					if alpha != 0 {
						nonTransparent++
						if crossPos == 0 || crossPos == cross-1 {
							t.Fatalf("hover artwork touches outer border at (%d,%d)", x, y)
						}
					}
				}
			}
			if nonTransparent == 0 {
				t.Fatal("hover artwork is empty")
			}
		})
	}
}
