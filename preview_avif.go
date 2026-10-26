package main

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Keep encoded tiles as well as renderer pixels: revisiting a tile should not
// launch another encoder. The budget includes payload bytes, not codec scratch.
const previewAVIFCacheBytes = 32 << 20

type previewEncodedImage struct {
	data        []byte
	contentType string
	ext         string
}

type previewAVIFCacheEntry struct {
	key   [32]byte
	image previewEncodedImage
}

type previewAVIFEncoder struct {
	mu      sync.Mutex
	entries map[[32]byte]*list.Element
	lru     list.List
	bytes   int
}

func (p *previewer) avifEncoder() *previewAVIFEncoder {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.avif == nil {
		p.avif = &previewAVIFEncoder{entries: make(map[[32]byte]*list.Element)}
	}
	return p.avif
}

func previewImagePixelKey(img *image.RGBA) [32]byte {
	hash := sha256.New()
	var dimensions [16]byte
	binary.LittleEndian.PutUint64(dimensions[:8], uint64(img.Rect.Dx()))
	binary.LittleEndian.PutUint64(dimensions[8:], uint64(img.Rect.Dy()))
	hash.Write(dimensions[:])
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		offset := img.PixOffset(img.Rect.Min.X, y)
		hash.Write(img.Pix[offset : offset+img.Rect.Dx()*4])
	}
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	return key
}

func (c *previewAVIFEncoder) get(key [32]byte) (previewEncodedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.lru.MoveToFront(entry)
		return entry.Value.(previewAVIFCacheEntry).image, true
	}
	return previewEncodedImage{}, false
}

func (c *previewAVIFEncoder) store(key [32]byte, img previewEncodedImage) {
	if len(img.data) > previewAVIFCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[key] != nil {
		return
	}
	c.entries[key] = c.lru.PushFront(previewAVIFCacheEntry{key, img})
	c.bytes += len(img.data)
	for c.bytes > previewAVIFCacheBytes || len(c.entries) > 512 {
		oldest := c.lru.Back()
		entry := oldest.Value.(previewAVIFCacheEntry)
		delete(c.entries, entry.key)
		c.bytes -= len(entry.image.data)
		c.lru.Remove(oldest)
	}
}

func (p *previewer) encodePreviewImage(ctx context.Context, img *image.RGBA, format string) (previewEncodedImage, error) {
	png := func() (previewEncodedImage, error) {
		data, err := encodeOpaquePreviewPNG(img)
		return previewEncodedImage{data, "image/png", ".png"}, err
	}
	if err := ctx.Err(); err != nil {
		return previewEncodedImage{}, err
	}
	if format != "avif" {
		return png()
	}
	c := p.avifEncoder()
	key := previewImagePixelKey(img)
	if cached, ok := c.get(key); ok {
		return cached, nil
	}
	data, err := encodeAVIFPreview(ctx, img)
	if ctx.Err() != nil {
		return previewEncodedImage{}, ctx.Err()
	}
	if err != nil {
		return previewEncodedImage{}, err
	}
	result := previewEncodedImage{data, "image/avif", ".avif"}
	c.store(key, result)
	return result, nil
}

// Use raw pixels, bounded threads and all-intra mode to avoid the old PNG pipe
// and expensive default FFmpeg threading/encoder setup for every tile.
func encodeAVIFPreview(ctx context.Context, img *image.RGBA) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := os.CreateTemp("", "factmapgen-*.avif")
	if err != nil {
		return nil, err
	}
	name := out.Name()
	defer os.Remove(name)
	if err := out.Close(); err != nil {
		return nil, err
	}
	pixels := img.Pix
	if img.Stride != img.Rect.Dx()*4 || len(pixels) != img.Rect.Dx()*img.Rect.Dy()*4 {
		pixels = make([]byte, 0, img.Rect.Dx()*img.Rect.Dy()*4)
		for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
			start := img.PixOffset(img.Rect.Min.X, y)
			pixels = append(pixels, img.Pix[start:start+img.Rect.Dx()*4]...)
		}
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-threads", "1", "-filter_threads", "1", "-f", "rawvideo", "-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", img.Rect.Dx(), img.Rect.Dy()), "-i", "pipe:0",
		"-frames:v", "1", "-c:v", "libaom-av1", "-usage", "allintra", "-cpu-used", "8",
		"-crf", "24", "-pix_fmt", "yuv444p", "-threads", "2", "-still-picture", "1", name)
	cmd.Stdin = bytes.NewReader(pixels)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("AVIF encode: %w: %s", err, clippedOutput(stderr.String(), 500))
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return nil, fmt.Errorf("invalid AVIF output")
	}
	return data, nil
}
