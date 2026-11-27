package main

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Keep encoded tiles as well as renderer pixels: revisiting a tile should not
// launch another encoder. The budget includes payload bytes, not codec scratch.
const previewAVIFCacheBytes = 32 << 20
const previewAVIFRequestSlots = 512

type previewEncodedImage struct {
	data        []byte
	contentType string
	ext         string
}

type previewAVIFCacheEntry struct {
	key   [32]byte
	image previewEncodedImage
}

type previewAVIFRequestAlias struct {
	request [32]byte
	pixels  [32]byte
}

type previewAVIFPending struct {
	done  chan struct{}
	image previewEncodedImage
	err   error
	retry bool
}

type previewAVIFEncoder struct {
	mu      sync.Mutex
	entries map[[32]byte]*list.Element
	pending map[[32]byte]*previewAVIFPending
	lru     list.List
	bytes   int
	aliases [previewAVIFRequestSlots]previewAVIFRequestAlias
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
	return c.getLocked(key)
}

func (c *previewAVIFEncoder) getLocked(key [32]byte) (previewEncodedImage, bool) {
	if entry := c.entries[key]; entry != nil {
		c.lru.MoveToFront(entry)
		return entry.Value.(previewAVIFCacheEntry).image, true
	}
	return previewEncodedImage{}, false
}

// Request aliases avoid composing and hashing pixels for identical Fast
// requests. Encoded bytes stay in the pixel-keyed LRU and are charged once.
func fastPreviewAVIFRequestKey(mapGen []byte, seed, planet string, size int, tpp, x, y float64, framed bool) [32]byte {
	hash := sha256.New()
	var geometry [33]byte
	binary.LittleEndian.PutUint64(geometry[:8], uint64(size))
	binary.LittleEndian.PutUint64(geometry[8:16], math.Float64bits(tpp))
	binary.LittleEndian.PutUint64(geometry[16:24], math.Float64bits(x))
	binary.LittleEndian.PutUint64(geometry[24:32], math.Float64bits(y))
	if framed {
		geometry[32] = 1
	}
	hash.Write(geometry[:])
	hash.Write([]byte(planet))
	hash.Write([]byte{0})
	hash.Write([]byte(seed))
	hash.Write([]byte{0})
	hash.Write(mapGen)
	var key [32]byte
	hash.Sum(key[:0])
	return key
}

func (c *previewAVIFEncoder) getRequest(key [32]byte) (previewEncodedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	alias := c.aliases[binary.LittleEndian.Uint16(key[:2])%previewAVIFRequestSlots]
	if alias.request == key {
		return c.getLocked(alias.pixels)
	}
	return previewEncodedImage{}, false
}

func (c *previewAVIFEncoder) storeRequest(key, pixels [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aliases[binary.LittleEndian.Uint16(key[:2])%previewAVIFRequestSlots] = previewAVIFRequestAlias{key, pixels}
}

func (c *previewAVIFEncoder) store(key [32]byte, img previewEncodedImage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storeLocked(key, img)
}

func (c *previewAVIFEncoder) storeLocked(key [32]byte, img previewEncodedImage) {
	if len(img.data) > previewAVIFCacheBytes || c.entries[key] != nil {
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
	return p.avifEncoder().encode(ctx, img, previewImagePixelKey(img))
}

func (c *previewAVIFEncoder) encode(ctx context.Context, img *image.RGBA, key [32]byte) (previewEncodedImage, error) {
	for {
		if err := ctx.Err(); err != nil {
			return previewEncodedImage{}, err
		}
		c.mu.Lock()
		if cached, ok := c.getLocked(key); ok {
			c.mu.Unlock()
			return cached, nil
		}
		if pending := c.pending[key]; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return previewEncodedImage{}, ctx.Err()
			case <-pending.done:
				if err := ctx.Err(); err != nil {
					return previewEncodedImage{}, err
				}
				// A waiting request can take over if the original request
				// canceled. A codec error is shared with all current waiters.
				if pending.retry {
					continue
				}
				return pending.image, pending.err
			}
		}
		pending := &previewAVIFPending{done: make(chan struct{})}
		if c.pending == nil {
			c.pending = make(map[[32]byte]*previewAVIFPending)
		}
		c.pending[key] = pending
		c.mu.Unlock()

		data, err := encodeAVIFPreview(ctx, img)
		if canceled := ctx.Err(); canceled != nil {
			pending.err, pending.retry = canceled, true
		} else if err != nil {
			pending.err = err
		} else {
			pending.image = previewEncodedImage{data, "image/avif", ".avif"}
		}
		c.mu.Lock()
		if pending.err == nil {
			c.storeLocked(key, pending.image)
		}
		delete(c.pending, key)
		close(pending.done)
		c.mu.Unlock()
		return pending.image, pending.err
	}
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
