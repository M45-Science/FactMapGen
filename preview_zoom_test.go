package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestScalePreviewImageSupportsArbitraryScale(t *testing.T) {
	source := image.NewRGBA(image.Rect(10, 20, 22, 32))
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}

	tests := []struct {
		name  string
		scale float64
		xs    []uint8
		ys    []uint8
	}{
		{name: "zoom in", scale: 0.75, xs: []uint8{14, 15, 16, 16}, ys: []uint8{24, 25, 26, 26}},
		{name: "zoom out", scale: 2.25, xs: []uint8{11, 13, 16, 18}, ys: []uint8{21, 23, 26, 28}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := scalePreviewImage(source, 4, test.scale).(*image.RGBA)
			for y, wantY := range test.ys {
				for x, wantX := range test.xs {
					pixel := got.RGBAAt(x, y)
					if pixel.R != wantX || pixel.G != wantY || pixel.A != 255 {
						t.Fatalf("pixel (%d,%d) = %#v, want source (%d,%d)", x, y, pixel, wantX, wantY)
					}
				}
			}
		})
	}
}

func TestFastPreviewArbitraryZoomRepeatsWholeTiles(t *testing.T) {
	const (
		size  = 64
		scale = 0.4
	)
	settings := defaultFactorioTerrainSettings(123456)
	img, gotScale, err := renderFastMapPreview(
		context.Background(),
		settings,
		size,
		previewZoom{mode: "scale", tilesPerPixel: scale, renderSize: size},
	)
	if err != nil {
		t.Fatalf("render arbitrary zoom: %v", err)
	}
	if gotScale != scale {
		t.Fatalf("tiles per pixel = %g, want %g", gotScale, scale)
	}

	origin := -float64(size) * scale / 2
	for y := 0; y < size; y++ {
		worldY := math.Floor(origin + float64(y)*scale)
		for x := 0; x < size; x++ {
			if x > 0 && math.Floor(origin+float64(x-1)*scale) == math.Floor(origin+float64(x)*scale) {
				if img.RGBAAt(x, y) != img.RGBAAt(x-1, y) {
					t.Fatalf("same world tile changed between pixels (%d,%d) and (%d,%d)", x-1, y, x, y)
				}
			}
			if y > 0 && math.Floor(origin+float64(y-1)*scale) == worldY {
				if img.RGBAAt(x, y) != img.RGBAAt(x, y-1) {
					t.Fatalf("same world tile changed between pixels (%d,%d) and (%d,%d)", x, y-1, x, y)
				}
			}
		}
	}
}
