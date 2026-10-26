package main

import (
	"image"
	"image/color"
	"math"
)

// Each entry contains candidates only; tile pixels remain in the main cache.
// Bound this separately from the much larger resource-region field caches.
const factorioChartChunkCacheLimit = 1024

type factorioChartEntity struct {
	x, y float64
	kind int
}

func factorioPlaceEveryN(x, y, spacingX, spacingY, offsetX, offsetY float32) float32 {
	x, y = x+offsetX, y+offsetY
	mod := func(v, spacing float32) float32 { return v - float32(math.Floor(float64(v/spacing)))*spacing }
	return min(noiseBool(mod((x+y*.93819)/1.41983, spacingX) <= 1), noiseBool(mod((x/4.1875839-y)*.913853883, spacingY) <= 1))
}

// Chart rectangles use the collision box, quantized to 1/256 tile before it is
// translated. Rasterization floors the lower and ceils the exclusive upper
// edge. Preview entity ink is clipped to the entity's own 32-tile chunk.
// In particular, chart boxes are neither one pixel nor map-generation boxes.
func factorioChartEntityRect(x, y float64, box [4]float64) image.Rectangle {
	fixed := func(v float64) float64 { return math.Trunc(v*256) / 256 }
	cx, cy := int(math.Floor(x/32))*32, int(math.Floor(y/32))*32
	return image.Rect(
		int(math.Floor(x+fixed(box[0]))), int(math.Floor(y+fixed(box[1]))),
		int(math.Ceil(x+fixed(box[2]))), int(math.Ceil(y+fixed(box[3]))),
	).Intersect(image.Rect(cx, cy, cx+32, cy+32))
}

func paintFactorioChartEntity(img *image.RGBA, settings fastPreviewSettings, x, y float64, box [4]float64, ink color.RGBA, originX, originY, tpp float64) {
	rect := factorioChartEntityRect(x, y, box)
	if rect.Empty() {
		return
	}
	lx, hx := factorioCliffPixelSpan(float64(rect.Min.X), float64(rect.Max.X), originX, tpp)
	ly, hy := factorioCliffPixelSpan(float64(rect.Min.Y), float64(rect.Max.Y), originY, tpp)
	blend := int(ink.A) + int(ink.A>>7)
	for py := max(0, ly); py <= min(img.Bounds().Dy()-1, hy); py++ {
		for px := max(0, lx); px <= min(img.Bounds().Dx()-1, hx); px++ {
			if fastOutOfMapBounds(settings, math.Floor(originX+float64(px)*tpp), math.Floor(originY+float64(py)*tpp)) {
				continue
			}
			i := py*img.Stride + px*4
			img.Pix[i] = uint8(((256-blend)*int(img.Pix[i]) + blend*int(ink.R)) >> 8)
			img.Pix[i+1] = uint8(((256-blend)*int(img.Pix[i+1]) + blend*int(ink.G)) >> 8)
			img.Pix[i+2] = uint8(((256-blend)*int(img.Pix[i+2]) + blend*int(ink.B)) >> 8)
		}
	}
}
