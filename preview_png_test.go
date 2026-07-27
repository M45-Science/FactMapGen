package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
)

var benchmarkEncodedPreviewPNG []byte

func BenchmarkEncodeOpaquePreviewPNG1024(b *testing.B) {
	_, _, _, settings := benchmarkPreviewSettings()
	img, _, err := renderFastMapPreview(
		context.Background(), settings, 1024,
		previewZoom{tilesPerPixel: 1, renderSize: 1024},
	)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := encodeOpaquePreviewPNG(img); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(img.Pix)))
	b.ResetTimer()
	for range b.N {
		benchmarkEncodedPreviewPNG, err = encodeOpaquePreviewPNG(img)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(benchmarkEncodedPreviewPNG)), "encoded-B")
}

func TestEncodeOpaquePreviewPNGPreservesPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 7, 5))
	fillOpaquePreviewPNGTestImage(img)

	encoded, err := encodeOpaquePreviewPNG(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 26 || encoded[25] != 2 {
		t.Fatalf("PNG IHDR color type = %d, want truecolor (2)", encoded[25])
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewPNGPixelsEqual(t, img, decoded)
}

func TestEncodeOpaquePreviewPNGHandlesSubimageStrideAndBounds(t *testing.T) {
	parent := image.NewRGBA(image.Rect(-4, -3, 9, 8))
	fillOpaquePreviewPNGTestImage(parent)
	sub := parent.SubImage(image.Rect(-1, 0, 6, 5)).(*image.RGBA)

	encoded, err := encodeOpaquePreviewPNG(sub)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewPNGPixelsEqual(t, sub, decoded)
}

func TestCompactOpaqueRGBARow(t *testing.T) {
	for width := 1; width <= 19; width++ {
		source := make([]byte, width*4)
		want := make([]byte, width*3)
		for pixel := 0; pixel < width; pixel++ {
			for channel := 0; channel < 4; channel++ {
				source[pixel*4+channel] = byte(pixel*29 + channel*53)
			}
			copy(want[pixel*3:pixel*3+3], source[pixel*4:pixel*4+3])
		}
		got := make([]byte, width*3)
		compactOpaqueRGBARow(got, source)
		if !bytes.Equal(got, want) {
			t.Fatalf("width %d packed row incorrectly\n got: %v\nwant: %v", width, got, want)
		}
	}
}

func TestEncodeOpaquePreviewPNGIsConcurrentAndDeterministic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fillOpaquePreviewPNGTestImage(img)
	want, err := encodeOpaquePreviewPNG(img)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 24
	const encodesPerGoroutine = 4
	errors := make(chan error, goroutines)
	var wait sync.WaitGroup
	for range goroutines {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range encodesPerGoroutine {
				got, err := encodeOpaquePreviewPNG(img)
				if err != nil {
					errors <- err
					return
				}
				if !bytes.Equal(got, want) {
					errors <- errPreviewPNGChanged
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func TestEncodePNGPreviewImageRejectsUnsupportedImageType(t *testing.T) {
	_, _, _, err := encodePNGPreviewImage(image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	if err == nil {
		t.Fatal("encodePNGPreviewImage accepted a non-RGBA image")
	}
}

func TestEncodeOpaquePreviewPNGRejectsInvalidDimensions(t *testing.T) {
	if _, err := encodeOpaquePreviewPNG(image.NewRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty image was accepted")
	}
	tooWide := &image.RGBA{Rect: image.Rect(0, 0, maxPreviewOutputSize+1, 1)}
	if _, err := encodeOpaquePreviewPNG(tooWide); err == nil {
		t.Fatal("oversized image was accepted")
	}
}

func fillOpaquePreviewPNGTestImage(img *image.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x*31 + y*7),
				G: uint8(x*11 - y*23),
				B: uint8(x*3 + y*47),
				A: 255,
			})
		}
	}
}

func assertPreviewPNGPixelsEqual(t *testing.T, source *image.RGBA, decoded image.Image) {
	t.Helper()
	sourceBounds := source.Bounds()
	if decoded.Bounds().Dx() != sourceBounds.Dx() || decoded.Bounds().Dy() != sourceBounds.Dy() {
		t.Fatalf("decoded bounds = %v, want %dx%d", decoded.Bounds(), sourceBounds.Dx(), sourceBounds.Dy())
	}
	for y := 0; y < sourceBounds.Dy(); y++ {
		for x := 0; x < sourceBounds.Dx(); x++ {
			want := source.RGBAAt(sourceBounds.Min.X+x, sourceBounds.Min.Y+y)
			got := color.RGBAModel.Convert(decoded.At(decoded.Bounds().Min.X+x, decoded.Bounds().Min.Y+y)).(color.RGBA)
			if got != want {
				t.Fatalf("pixel (%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}
}

type previewPNGChangedError struct{}

func (previewPNGChangedError) Error() string { return "parallel PNG encoding changed output" }

var errPreviewPNGChanged previewPNGChangedError
