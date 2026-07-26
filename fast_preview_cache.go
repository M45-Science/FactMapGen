package main

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"runtime"
	"strconv"
	"sync"
)

const (
	fastPreviewTileEdge           = 128
	defaultFastPreviewCacheBytes  = 2 << 30
	defaultFastPreviewCacheWorlds = 4
	fastPreviewMaxRegionsPerField = 64
	fastPreviewRendererRevision   = "factorio-preview-v3"
)

type fastPreviewWorldKey [sha256.Size]byte

type fastPreviewTileKey struct {
	world         fastPreviewWorldKey
	tilesPerPixel uint64
	rasterTileX   int64
	rasterTileY   int64
}

type fastPreviewTile struct {
	key     fastPreviewTileKey
	image   *image.RGBA
	bytes   int64
	element *list.Element
}

type fastPreviewTileUse struct {
	key   fastPreviewTileKey
	x     int64
	y     int64
	image *image.RGBA
}

type fastPreviewWorldCache struct {
	key        fastPreviewWorldKey
	world      *fastPreviewWorld
	renderGate chan struct{}
	retained   bool
	references int
	element    *list.Element
}

type fastPreviewCache struct {
	mu         sync.Mutex
	renderGate chan struct{}

	maxBytes  int64
	maxWorlds int
	usedBytes int64

	worlds   map[fastPreviewWorldKey]*fastPreviewWorldCache
	worldLRU list.List
	tiles    map[fastPreviewTileKey]*fastPreviewTile
	tileLRU  list.List

	hits      uint64
	misses    uint64
	evictions uint64
}

type fastPreviewCacheStats struct {
	Worlds    int
	Tiles     int
	Bytes     int64
	Hits      uint64
	Misses    uint64
	Evictions uint64
}

func newFastPreviewCache(maxBytes int64, maxWorlds int) *fastPreviewCache {
	if maxBytes < fastPreviewTileEdge*fastPreviewTileEdge*4 {
		maxBytes = fastPreviewTileEdge * fastPreviewTileEdge * 4
	}
	if maxWorlds < 1 {
		maxWorlds = 1
	}
	return &fastPreviewCache{
		renderGate: make(chan struct{}, fastPreviewRenderConcurrency()),
		maxBytes:   maxBytes,
		maxWorlds:  maxWorlds,
		worlds:     make(map[fastPreviewWorldKey]*fastPreviewWorldCache),
		tiles:      make(map[fastPreviewTileKey]*fastPreviewTile),
	}
}

func fastPreviewRenderConcurrency() int {
	if runtime.GOMAXPROCS(0) >= 2*fastPreviewMaxWorkers {
		return 2
	}
	return 1
}

func fastPreviewCacheKey(raw json.RawMessage, effectiveSeed uint32) (fastPreviewWorldKey, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fastPreviewWorldKey{}, err
	}
	object, ok := root.(map[string]any)
	if !ok {
		return fastPreviewWorldKey{}, fmt.Errorf("map generation settings must be a JSON object")
	}
	object["seed"] = json.Number(strconv.FormatUint(uint64(effectiveSeed), 10))
	canonical, err := json.Marshal(object)
	if err != nil {
		return fastPreviewWorldKey{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(fastPreviewRendererRevision))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonical)
	var key fastPreviewWorldKey
	copy(key[:], hash.Sum(nil))
	return key, nil
}

func (c *fastPreviewCache) render(
	ctx context.Context,
	key fastPreviewWorldKey,
	settings fastPreviewSettings,
	size int,
	tilesPerPixel float64,
	centerX, centerY float64,
) (*image.RGBA, error) {
	entry := c.acquireWorld(key, settings)
	defer c.releaseWorld(entry)

	if err := entry.acquireRender(ctx); err != nil {
		return nil, err
	}
	defer entry.releaseRender()
	if err := acquireFastPreviewGate(ctx, c.renderGate); err != nil {
		return nil, err
	}
	defer releaseFastPreviewGate(c.renderGate)
	defer entry.world.trimSpatialCaches()

	if !entry.retained {
		return entry.world.render(ctx, size, tilesPerPixel, centerX, centerY)
	}

	centerPixelX, alignedX := fastPreviewRasterCoordinate(centerX, tilesPerPixel)
	centerPixelY, alignedY := fastPreviewRasterCoordinate(centerY, tilesPerPixel)
	if size%2 != 0 || !alignedX || !alignedY {
		return entry.world.render(ctx, size, tilesPerPixel, centerX, centerY)
	}

	startX := centerPixelX - int64(size/2)
	startY := centerPixelY - int64(size/2)
	endX := startX + int64(size)
	endY := startY + int64(size)
	firstTileX := fastPreviewFloorDiv(startX, fastPreviewTileEdge)
	firstTileY := fastPreviewFloorDiv(startY, fastPreviewTileEdge)
	lastTileX := fastPreviewFloorDiv(endX-1, fastPreviewTileEdge)
	lastTileY := fastPreviewFloorDiv(endY-1, fastPreviewTileEdge)

	tileCount := int((lastTileX - firstTileX + 1) * (lastTileY - firstTileY + 1))
	uses := make([]fastPreviewTileUse, 0, tileCount)
	missing := make([]int, 0, tileCount)
	for tileY := firstTileY; tileY <= lastTileY; tileY++ {
		for tileX := firstTileX; tileX <= lastTileX; tileX++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			use := fastPreviewTileUse{key: fastPreviewTileKey{
				world:         key,
				tilesPerPixel: math.Float64bits(tilesPerPixel),
				rasterTileX:   tileX,
				rasterTileY:   tileY,
			}, x: tileX, y: tileY}
			use.image, _ = c.getTile(use.key)
			uses = append(uses, use)
			if use.image == nil {
				missing = append(missing, len(uses)-1)
			}
		}
	}
	if len(missing) > 0 {
		if tilesPerPixel == 1 && size%fastPreviewTileEdge == 0 && len(missing) == len(uses) && startX%fastPreviewTileEdge == 0 && startY%fastPreviewTileEdge == 0 {
			if err := c.renderAlignedTileBatch(ctx, entry, uses, size, startX, startY); err != nil {
				return nil, err
			}
		} else {
			prepareFastPreviewCacheTiles(entry.world, uses, missing, tilesPerPixel)
			if err := c.renderMissingTiles(ctx, entry, uses, missing, tilesPerPixel); err != nil {
				return nil, err
			}
		}
	}

	result := image.NewRGBA(image.Rect(0, 0, size, size))
	for _, use := range uses {
		copyFastPreviewTile(result, use.image, startX, startY, use.x, use.y)
	}

	originX := float64(startX) * tilesPerPixel
	originY := float64(startY) * tilesPerPixel
	if err := entry.world.renderOverlays(ctx, result, originX, originY, tilesPerPixel); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *fastPreviewCache) renderAlignedTileBatch(
	ctx context.Context,
	entry *fastPreviewWorldCache,
	uses []fastPreviewTileUse,
	size int,
	startX, startY int64,
) error {
	base := image.NewRGBA(image.Rect(0, 0, size, size))
	if err := entry.world.renderBase(ctx, base, float64(startX), float64(startY), 1); err != nil {
		return err
	}
	for index := range uses {
		use := &uses[index]
		tile := image.NewRGBA(image.Rect(0, 0, fastPreviewTileEdge, fastPreviewTileEdge))
		sourceX := int(use.x*fastPreviewTileEdge - startX)
		sourceY := int(use.y*fastPreviewTileEdge - startY)
		for y := 0; y < fastPreviewTileEdge; y++ {
			sourceOffset := (sourceY+y)*base.Stride + sourceX*4
			destinationOffset := y * tile.Stride
			copy(
				tile.Pix[destinationOffset:destinationOffset+tile.Stride],
				base.Pix[sourceOffset:sourceOffset+tile.Stride],
			)
		}
		use.image = tile
		c.storeTile(entry, use.key, tile)
	}
	return nil
}

func prepareFastPreviewCacheTiles(
	world *fastPreviewWorld,
	uses []fastPreviewTileUse,
	missing []int,
	tilesPerPixel float64,
) {
	if world.resources == nil {
		return
	}
	padding := fastPreviewTileHistoryPadding(tilesPerPixel)
	for _, index := range missing {
		use := uses[index]
		rasterX := use.x*fastPreviewTileEdge - int64(padding)
		rasterY := use.y*fastPreviewTileEdge - int64(padding)
		renderSize := int64(fastPreviewTileEdge + padding)
		world.resources.prepareForBounds(
			math.Floor(float64(rasterX)*tilesPerPixel),
			math.Floor(float64(rasterY)*tilesPerPixel),
			math.Floor(float64(rasterX+renderSize-1)*tilesPerPixel),
			math.Floor(float64(rasterY+renderSize-1)*tilesPerPixel),
		)
	}
}

func (c *fastPreviewCache) renderMissingTiles(
	ctx context.Context,
	entry *fastPreviewWorldCache,
	uses []fastPreviewTileUse,
	missing []int,
	tilesPerPixel float64,
) error {
	renderOne := func(index int) error {
		use := &uses[index]
		tile, err := renderFastPreviewCacheTile(ctx, entry.world, use.x, use.y, tilesPerPixel)
		if err != nil {
			return err
		}
		use.image = tile
		c.storeTile(entry, use.key, tile)
		return nil
	}
	if tilesPerPixel == 1 || len(missing) == 1 {
		for _, index := range missing {
			if err := renderOne(index); err != nil {
				return err
			}
		}
		return nil
	}

	jobs := make(chan int)
	workerCount := min(len(missing), fastPreviewMaxWorkers)
	var workers sync.WaitGroup
	var errorMu sync.Mutex
	var firstError error
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				errorMu.Lock()
				failed := firstError != nil
				errorMu.Unlock()
				if failed {
					continue
				}
				if err := renderOne(index); err != nil {
					errorMu.Lock()
					if firstError == nil {
						firstError = err
					}
					errorMu.Unlock()
				}
			}
		}()
	}
	for _, index := range missing {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return firstError
}

func renderFastPreviewCacheTile(
	ctx context.Context,
	world *fastPreviewWorld,
	tileX, tileY int64,
	tilesPerPixel float64,
) (*image.RGBA, error) {
	padding := fastPreviewTileHistoryPadding(tilesPerPixel)
	renderSize := fastPreviewTileEdge + padding
	scratch := image.NewRGBA(image.Rect(0, 0, renderSize, renderSize))
	rasterOriginX := tileX*fastPreviewTileEdge - int64(padding)
	rasterOriginY := tileY*fastPreviewTileEdge - int64(padding)
	if err := world.renderBase(
		ctx,
		scratch,
		float64(rasterOriginX)*tilesPerPixel,
		float64(rasterOriginY)*tilesPerPixel,
		tilesPerPixel,
	); err != nil {
		return nil, err
	}
	tile := image.NewRGBA(image.Rect(0, 0, fastPreviewTileEdge, fastPreviewTileEdge))
	for y := 0; y < fastPreviewTileEdge; y++ {
		sourceOffset := (y+padding)*scratch.Stride + padding*4
		destinationOffset := y * tile.Stride
		copy(tile.Pix[destinationOffset:destinationOffset+tile.Stride], scratch.Pix[sourceOffset:sourceOffset+tile.Stride])
	}
	return tile, nil
}

func fastPreviewTileHistoryPadding(tilesPerPixel float64) int {
	if tilesPerPixel >= 1 {
		return 0
	}
	return int(math.Ceil(1 / tilesPerPixel))
}

func fastPreviewRasterCoordinate(center, tilesPerPixel float64) (int64, bool) {
	if tilesPerPixel <= 0 || math.IsNaN(center) || math.IsInf(center, 0) {
		return 0, false
	}
	rasterPosition := center / tilesPerPixel
	raster := math.Round(rasterPosition)
	maxInt64Exclusive := -float64(math.MinInt64)
	if raster < float64(math.MinInt64) || raster >= maxInt64Exclusive {
		return 0, false
	}
	ulpBelow := math.Abs(rasterPosition - math.Nextafter(rasterPosition, math.Inf(-1)))
	ulpAbove := math.Abs(math.Nextafter(rasterPosition, math.Inf(1)) - rasterPosition)
	tolerance := 4 * max(ulpBelow, ulpAbove)
	return int64(raster), math.Abs(rasterPosition-raster) <= tolerance
}

func fastPreviewFloorDiv(value int64, divisor int64) int64 {
	quotient := value / divisor
	if value < 0 && value%divisor != 0 {
		quotient--
	}
	return quotient
}

func copyFastPreviewTile(
	destination *image.RGBA,
	source *image.RGBA,
	viewportStartX, viewportStartY int64,
	tileX, tileY int64,
) {
	tileStartX := tileX * fastPreviewTileEdge
	tileStartY := tileY * fastPreviewTileEdge
	copyStartX := max(viewportStartX, tileStartX)
	copyStartY := max(viewportStartY, tileStartY)
	copyEndX := min(viewportStartX+int64(destination.Bounds().Dx()), tileStartX+fastPreviewTileEdge)
	copyEndY := min(viewportStartY+int64(destination.Bounds().Dy()), tileStartY+fastPreviewTileEdge)
	copyWidth := int(copyEndX - copyStartX)
	if copyWidth <= 0 || copyEndY <= copyStartY {
		return
	}
	sourceX := int(copyStartX - tileStartX)
	destinationX := int(copyStartX - viewportStartX)
	for worldY := copyStartY; worldY < copyEndY; worldY++ {
		sourceY := int(worldY - tileStartY)
		destinationY := int(worldY - viewportStartY)
		sourceOffset := sourceY*source.Stride + sourceX*4
		destinationOffset := destinationY*destination.Stride + destinationX*4
		copy(
			destination.Pix[destinationOffset:destinationOffset+copyWidth*4],
			source.Pix[sourceOffset:sourceOffset+copyWidth*4],
		)
	}
}

func (c *fastPreviewCache) acquireWorld(
	key fastPreviewWorldKey,
	settings fastPreviewSettings,
) *fastPreviewWorldCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.worlds[key]; ok {
		entry.references++
		c.worldLRU.MoveToFront(entry.element)
		return entry
	}
	for len(c.worlds) >= c.maxWorlds {
		victim := c.unreferencedWorldLocked()
		if victim == nil {
			return newFastPreviewWorldCache(key, settings, false)
		}
		c.removeWorldLocked(victim)
	}

	entry := newFastPreviewWorldCache(key, settings, true)
	entry.element = c.worldLRU.PushFront(entry)
	c.worlds[key] = entry
	return entry
}

func (c *fastPreviewCache) releaseWorld(entry *fastPreviewWorldCache) {
	if !entry.retained {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.references--
}

func newFastPreviewWorldCache(
	key fastPreviewWorldKey,
	settings fastPreviewSettings,
	retained bool,
) *fastPreviewWorldCache {
	return &fastPreviewWorldCache{
		key:        key,
		world:      newFastPreviewWorld(settings),
		renderGate: make(chan struct{}, 1),
		retained:   retained,
		references: 1,
	}
}

func (entry *fastPreviewWorldCache) acquireRender(ctx context.Context) error {
	return acquireFastPreviewGate(ctx, entry.renderGate)
}

func (entry *fastPreviewWorldCache) releaseRender() {
	releaseFastPreviewGate(entry.renderGate)
}

func acquireFastPreviewGate(ctx context.Context, gate chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseFastPreviewGate(gate chan struct{}) {
	<-gate
}

func (c *fastPreviewCache) unreferencedWorldLocked() *fastPreviewWorldCache {
	for element := c.worldLRU.Back(); element != nil; element = element.Prev() {
		candidate := element.Value.(*fastPreviewWorldCache)
		if candidate.references == 0 {
			return candidate
		}
	}
	return nil
}

func (c *fastPreviewCache) removeWorldLocked(entry *fastPreviewWorldCache) {
	delete(c.worlds, entry.key)
	c.worldLRU.Remove(entry.element)
	for key, tile := range c.tiles {
		if key.world != entry.key {
			continue
		}
		c.removeTileLocked(key, tile)
	}
}

func (c *fastPreviewCache) getTile(key fastPreviewTileKey) (*image.RGBA, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tile, ok := c.tiles[key]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	c.tileLRU.MoveToFront(tile.element)
	return tile.image, true
}

func (c *fastPreviewCache) storeTile(
	entry *fastPreviewWorldCache,
	key fastPreviewTileKey,
	img *image.RGBA,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !entry.retained {
		return
	}
	if existing, ok := c.tiles[key]; ok {
		c.tileLRU.MoveToFront(existing.element)
		return
	}
	tile := &fastPreviewTile{key: key, image: img, bytes: int64(len(img.Pix))}
	tile.element = c.tileLRU.PushFront(tile)
	c.tiles[key] = tile
	c.usedBytes += tile.bytes
	for c.usedBytes > c.maxBytes {
		oldest := c.tileLRU.Back()
		if oldest == nil {
			break
		}
		victim := oldest.Value.(*fastPreviewTile)
		c.removeTileLocked(victim.key, victim)
		c.evictions++
	}
}

func (c *fastPreviewCache) removeTileLocked(key fastPreviewTileKey, tile *fastPreviewTile) {
	delete(c.tiles, key)
	c.tileLRU.Remove(tile.element)
	c.usedBytes -= tile.bytes
}

func (c *fastPreviewCache) stats() fastPreviewCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fastPreviewCacheStats{
		Worlds: len(c.worlds), Tiles: len(c.tiles), Bytes: c.usedBytes,
		Hits: c.hits, Misses: c.misses, Evictions: c.evictions,
	}
}
