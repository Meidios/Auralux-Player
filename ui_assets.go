//go:build windows

package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io/fs"
	"path/filepath"
	"strings"
	"unsafe"
)

//go:embed assets/ui/*.png assets/ui/*.svg
var embeddedUIAssets embed.FS

type UIBitmap struct {
	Handle uintptr
	Width  int32
	Height int32
}

func loadEmbeddedUIAssets() (map[string]*UIBitmap, error) {
	paths, err := fs.Glob(embeddedUIAssets, "assets/ui/*.png")
	if err != nil {
		return nil, err
	}
	assets := make(map[string]*UIBitmap, len(paths))
	for _, path := range paths {
		data, err := embeddedUIAssets.ReadFile(path)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		bmp, err := createAlphaBitmap(img)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", path, err)
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		// Keep the established PNG artwork authoritative during the migration.
		// SVG is used only when no same-name PNG asset exists.
		if _, exists := assets[name]; exists {
			continue
		}
		assets[name] = bmp
	}
	svgPaths, err := fs.Glob(embeddedUIAssets, "assets/ui/*.svg")
	if err != nil {
		destroyUIAssets(assets)
		return nil, err
	}
	for _, path := range svgPaths {
		data, err := embeddedUIAssets.ReadFile(path)
		if err != nil {
			destroyUIAssets(assets)
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		// A PNG family is authoritative as a whole. This check must happen
		// before generating SVG hover/day/2x variants; otherwise a base PNG
		// can still be shadowed by generated names such as name_hover.
		if _, exists := assets[name]; exists {
			continue
		}
		if _, exists := assets[name+"_normal"]; exists {
			continue
		}
		for _, variant := range []struct {
			suffix     string
			day, hover bool
		}{
			{}, {suffix: "_hover", hover: true},
			{suffix: "_day", day: true}, {suffix: "_day_hover", day: true, hover: true},
		} {
			tint := svgThemeIconTint(name, variant.day)
			if name == "correlation_thumb" {
				tint = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			}
			if variant.hover {
				tint = svgHoverIconTint(variant.day)
				if name == "correlation_thumb" {
					tint = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				}
			}
			for _, scale := range []struct {
				suffix string
				size   int
			}{{suffix: "", size: 96}, {suffix: "_2x", size: 192}} {
				img, err := rasterizeSVGIcon(data, scale.size, tint)
				if err != nil {
					destroyUIAssets(assets)
					return nil, fmt.Errorf("render %s: %w", path, err)
				}
				bmp, err := createAlphaBitmap(img)
				if err != nil {
					destroyUIAssets(assets)
					return nil, fmt.Errorf("create %s: %w", path, err)
				}
				assets[name+variant.suffix+scale.suffix] = bmp
			}
		}
	}
	return assets, nil
}

func createAlphaBitmap(img image.Image) (*UIBitmap, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid image size")
	}
	bmi := BITMAPINFO{}
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
	bmi.BmiHeader.BiWidth = int32(w)
	bmi.BmiHeader.BiHeight = -int32(h) // top-down DIB
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = BI_RGB
	var bits uintptr
	hbm, _, _ := procCreateDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&bmi)),
		DIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hbm == 0 || bits == 0 {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(bits)), w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			off := (y*w + x) * 4
			buf[off+0] = byte(bl >> 8)
			buf[off+1] = byte(g >> 8)
			buf[off+2] = byte(r >> 8)
			buf[off+3] = byte(a >> 8)
		}
	}
	return &UIBitmap{Handle: hbm, Width: int32(w), Height: int32(h)}, nil
}

func destroyUIAssets(assets map[string]*UIBitmap) {
	for _, asset := range assets {
		if asset != nil && asset.Handle != 0 {
			procDeleteObject.Call(asset.Handle)
			asset.Handle = 0
		}
	}
}

func drawUIAsset(dc uintptr, assets map[string]*UIBitmap, name string, r RECT) bool {
	assetName := name
	if theApp != nil && theApp.themeMode == 1 && !strings.HasSuffix(assetName, "_day") {
		if _, ok := assets[assetName+"_day"]; ok {
			assetName += "_day"
		}
	}
	// Prefer pre-scaled 2x assets on high-DPI monitors. This avoids enlarging
	// low-resolution controls at paint time and keeps edges noticeably cleaner.
	if theApp != nil && theApp.currentDPI() >= 144 {
		if _, ok := assets[assetName+"_2x"]; ok {
			assetName += "_2x"
		}
	}
	asset := assets[assetName]
	if asset == nil || asset.Handle == 0 || rectW(r) <= 0 || rectH(r) <= 0 {
		return false
	}
	mem, _, _ := procCreateCompatibleDC.Call(dc)
	if mem == 0 {
		return false
	}
	defer procDeleteDC.Call(mem)
	old := selectObject(mem, asset.Handle)
	defer selectObject(mem, old)
	blend := uint32(AC_SRC_OVER) | uint32(255)<<16 | uint32(AC_SRC_ALPHA)<<24
	ok, _, _ := procAlphaBlend.Call(
		dc,
		uintptr(r.Left), uintptr(r.Top), uintptr(rectW(r)), uintptr(rectH(r)),
		mem,
		0, 0, uintptr(asset.Width), uintptr(asset.Height),
		uintptr(blend),
	)
	return ok != 0
}

func svgHoverIconTint(day bool) color.NRGBA {
	if day {
		return color.NRGBA{R: 36, G: 116, B: 183, A: 255}
	}
	return color.NRGBA{R: 168, G: 220, B: 255, A: 255}
}
