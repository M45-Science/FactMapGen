package main

import (
	"container/list"
	"context"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
	"unsafe"
)

// Each entry contains candidates only; tile pixels remain in the main cache.
// Bound this separately from the much larger resource-region field caches.
const defaultFastPreviewEntityCacheBytes = 256 << 20

type factorioChartEntity struct {
	x, y float64
	kind int
}

// Keep neighboring views reusable when the cache fills, instead of dropping
// every chunk at once. Published entity slices are immutable.
type factorioChartChunkCache struct {
	mu        sync.Mutex
	entries   map[[2]int]*list.Element
	lru       list.List
	maxBytes  int64
	usedBytes int64
}

type factorioChartChunkEntry struct {
	key      [2]int
	entities []factorioChartEntity
	bytes    int64
}

func (c *factorioChartChunkCache) get(key [2]int) ([]factorioChartEntity, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(entry)
	return entry.Value.(factorioChartChunkEntry).entities, true
}

func (c *factorioChartChunkCache) store(key [2]int, entities []factorioChartEntity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		c.lru.MoveToFront(entry)
		return
	}
	// Charge the backing array, plus an allowance for the key, LRU node and
	// map entry. Empty chunks also consume budget; this is not a process RSS cap.
	bytes := int64(cap(entities))*int64(unsafe.Sizeof(factorioChartEntity{})) + 128
	budget := c.maxBytes
	if budget <= 0 {
		budget = defaultFastPreviewEntityCacheBytes
	}
	if bytes > budget {
		return
	}
	if c.entries == nil {
		c.entries = make(map[[2]int]*list.Element)
	}
	c.entries[key] = c.lru.PushFront(factorioChartChunkEntry{key, entities, bytes})
	c.usedBytes += bytes
	for c.usedBytes > budget {
		oldest := c.lru.Back()
		entry := oldest.Value.(factorioChartChunkEntry)
		delete(c.entries, entry.key)
		c.usedBytes -= entry.bytes
		c.lru.Remove(oldest)
	}
}

// Generate independent chunks concurrently, then draw in the original order.
// Batches bound temporary entity storage even for large, zoomed-out previews.
func renderFactorioChartChunks(
	ctx context.Context, img *image.RGBA, x0, y0, tpp float64,
	chunk func(context.Context, int, int) ([]factorioChartEntity, error),
	draw func([]factorioChartEntity),
) error {
	minX, minY := int(math.Floor(x0/32)), int(math.Floor(y0/32))
	maxX := int(math.Ceil((x0 + float64(img.Bounds().Dx())*tpp) / 32))
	maxY := int(math.Ceil((y0 + float64(img.Bounds().Dy())*tpp) / 32))
	width := maxX - minX
	count := width * (maxY - minY)
	const batchSize = 64
	type result struct {
		entities []factorioChartEntity
		err      error
	}
	results := make([]result, min(batchSize, count))
	for first := 0; first < count; first += batchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := results[:min(batchSize, count-first)]
		workerCount := min(runtime.GOMAXPROCS(0), len(batch), fastPreviewMaxWorkers)
		var workers sync.WaitGroup
		for worker := range workerCount {
			workers.Go(func() {
				for index := worker; index < len(batch); index += workerCount {
					if err := ctx.Err(); err != nil {
						batch[index] = result{err: err}
						return
					}
					position := first + index
					entities, err := chunk(ctx, minX+position%width, minY+position/width)
					batch[index] = result{entities, err}
				}
			})
		}
		workers.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, result := range batch {
			if result.err != nil {
				return result.err
			}
			draw(result.entities)
		}
	}
	return ctx.Err()
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
