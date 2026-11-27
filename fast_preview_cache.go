package main

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	defaultFastPreviewCacheWorlds = 5
	fastPreviewMaxRegionsPerField = 64
	fastPreviewRendererRevision   = "factorio-preview-v11"
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
	poolMu     sync.Mutex
	idleWorlds []*fastPreviewWorld
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

	maxBytes         int64
	entityCacheBytes int64
	maxWorlds        int
	usedBytes        int64

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
		renderGate:       make(chan struct{}, fastPreviewRenderConcurrency()),
		maxBytes:         maxBytes,
		entityCacheBytes: defaultFastPreviewEntityCacheBytes,
		maxWorlds:        maxWorlds,
		worlds:           make(map[fastPreviewWorldKey]*fastPreviewWorldCache),
		tiles:            make(map[fastPreviewTileKey]*fastPreviewTile),
	}
}

func fastPreviewRenderConcurrency() int {
	return max(1, runtime.NumCPU())
}

func fastPreviewCacheKey(raw json.RawMessage, effectiveSeed uint32) (fastPreviewWorldKey, error) {
	return fastPreviewCacheKeyForPlanet(raw, effectiveSeed, fastPreviewPlanetNauvis)
}

func fastPreviewCacheKeyForPlanet(
	raw json.RawMessage,
	effectiveSeed uint32,
	planet string,
) (fastPreviewWorldKey, error) {
	normalizedPlanet, err := normalizeFastPreviewPlanet(planet)
	if err != nil {
		return fastPreviewWorldKey{}, err
	}
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
	_, _ = hash.Write([]byte(normalizedPlanet))
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := c.acquireWorld(key, settings)
	defer c.releaseWorld(entry)

	centerPixelX, alignedX := fastPreviewRasterCoordinate(centerX, tilesPerPixel)
	centerPixelY, alignedY := fastPreviewRasterCoordinate(centerY, tilesPerPixel)
	cacheable := entry.retained && size%2 == 0 && alignedX && alignedY
	startX, startY := centerPixelX-int64(size/2), centerPixelY-int64(size/2)
	var uses []fastPreviewTileUse
	var missing []int
	if cacheable {
		uses, missing = c.viewportTiles(key, size, tilesPerPixel, startX, startY)
	}
	if cacheable && tilesPerPixel == 1 && size <= 512 && len(missing) == 0 {
		// Unit-zoom raster tiles already contain every overlay. Serving
		// them does not require a renderer or a spot in the render queue.
		result := assembleFastPreviewTiles(uses, size, startX, startY)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	if err := entry.acquireRender(ctx); err != nil {
		return nil, err
	}
	defer entry.releaseRender()
	if err := acquireFastPreviewGate(ctx, c.renderGate); err != nil {
		return nil, err
	}
	defer releaseFastPreviewGate(c.renderGate)
	if len(missing) > 0 {
		missing = c.refreshPreviewTiles(uses, missing)
	}
	// Each admitted render owns its mutable evaluator state. Tile pixels and
	// the synchronized entity LRU remain shared across those evaluators.
	world := entry.takeRenderWorld()
	defer entry.returnRenderWorld(world)
	defer world.trimSpatialCaches()

	if !cacheable {
		return world.render(ctx, size, tilesPerPixel, centerX, centerY)
	}

	if len(missing) > 0 {
		if tilesPerPixel == 1 && size%fastPreviewTileEdge == 0 && len(missing) == len(uses) && startX%fastPreviewTileEdge == 0 && startY%fastPreviewTileEdge == 0 {
			img, err := world.render(ctx, size, 1, centerX, centerY)
			if err != nil {
				return nil, err
			}
			if err := c.storeAlignedTileBatch(ctx, entry, uses, img, startX, startY); err != nil {
				return nil, err
			}
			return img, nil
		}
		if tilesPerPixel != 1 {
			prepareFastPreviewCacheTiles(world, uses, missing, tilesPerPixel)
		}
		if err := c.renderMissingTiles(ctx, entry, world, uses, missing, tilesPerPixel); err != nil {
			return nil, err
		}
	}

	result := assembleFastPreviewTiles(uses, size, startX, startY)
	if tilesPerPixel != 1 {
		originX, originY := float64(startX)*tilesPerPixel, float64(startY)*tilesPerPixel
		if err := world.renderOverlays(ctx, result, originX, originY, tilesPerPixel); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *fastPreviewCache) viewportTiles(key fastPreviewWorldKey, size int, tpp float64, x, y int64) ([]fastPreviewTileUse, []int) {
	firstTileX := fastPreviewFloorDiv(x, fastPreviewTileEdge)
	firstTileY := fastPreviewFloorDiv(y, fastPreviewTileEdge)
	lastTileX := fastPreviewFloorDiv(x+int64(size)-1, fastPreviewTileEdge)
	lastTileY := fastPreviewFloorDiv(y+int64(size)-1, fastPreviewTileEdge)
	count := int((lastTileX - firstTileX + 1) * (lastTileY - firstTileY + 1))
	uses := make([]fastPreviewTileUse, 0, count)
	var missing []int
	c.mu.Lock()
	defer c.mu.Unlock()
	for ty := firstTileY; ty <= lastTileY; ty++ {
		for tx := firstTileX; tx <= lastTileX; tx++ {
			tileKey := fastPreviewTileKey{world: key, tilesPerPixel: math.Float64bits(tpp), rasterTileX: tx, rasterTileY: ty}
			use := fastPreviewTileUse{key: tileKey, x: tx, y: ty}
			if tile := c.tiles[tileKey]; tile != nil {
				c.hits++
				c.tileLRU.MoveToFront(tile.element)
				use.image = tile.image
			} else {
				c.misses++
				if missing == nil {
					missing = make([]int, 0, count)
				}
				missing = append(missing, len(uses))
			}
			uses = append(uses, use)
		}
	}
	return uses, missing
}

// Another admitted request may have filled a miss while we waited.
func (c *fastPreviewCache) refreshPreviewTiles(uses []fastPreviewTileUse, missing []int) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := missing[:0]
	for _, i := range missing {
		if tile := c.tiles[uses[i].key]; tile != nil {
			c.tileLRU.MoveToFront(tile.element)
			uses[i].image = tile.image
		} else {
			remaining = append(remaining, i)
		}
	}
	return remaining
}

func assembleFastPreviewTiles(uses []fastPreviewTileUse, size int, x, y int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for _, use := range uses {
		copyFastPreviewTile(img, use.image, x, y, use.x, use.y)
	}
	return img
}

// Include the native missing-neighbor mask in the world key so standalone
// frames can reuse their tiles without affecting the seamless browser map.
func (c *fastPreviewCache) renderFrame(ctx context.Context, key fastPreviewWorldKey, settings fastPreviewSettings, size int, centerX, centerY float64) (*image.RGBA, error) {
	if size%2 != 0 {
		// Factorio subtracts the integer half-size to find the first tile.
		centerX += .5
		centerY += .5
	}
	frame := fastPreviewChunkBounds(size, 1, centerX, centerY)
	settings.entityBounds = frame
	var framedKey [sha256.Size + 4*8]byte
	copy(framedKey[:], key[:])
	for i, v := range [...]int{frame.Min.X, frame.Min.Y, frame.Max.X, frame.Max.Y} {
		binary.LittleEndian.PutUint64(framedKey[sha256.Size+i*8:], uint64(v))
	}
	return c.render(ctx, sha256.Sum256(framedKey[:]), settings, size, 1, centerX, centerY)
}

func (c *fastPreviewCache) storeAlignedTileBatch(
	ctx context.Context,
	entry *fastPreviewWorldCache,
	uses []fastPreviewTileUse,
	img *image.RGBA,
	startX, startY int64,
) error {
	for _, use := range uses {
		if err := ctx.Err(); err != nil {
			return err
		}
		tile := sliceFastPreviewTile(img, use.x*fastPreviewTileEdge-startX, use.y*fastPreviewTileEdge-startY)
		c.storeTile(entry, use.key, tile)
	}
	return nil
}

func sliceFastPreviewTile(img *image.RGBA, sourceX, sourceY int64) *image.RGBA {
	tile := image.NewRGBA(image.Rect(0, 0, fastPreviewTileEdge, fastPreviewTileEdge))
	for y := 0; y < fastPreviewTileEdge; y++ {
		sourceOffset := (int(sourceY)+y)*img.Stride + int(sourceX)*4
		destinationOffset := y * tile.Stride
		copy(tile.Pix[destinationOffset:destinationOffset+tile.Stride], img.Pix[sourceOffset:sourceOffset+tile.Stride])
	}
	return tile
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
	world *fastPreviewWorld,
	uses []fastPreviewTileUse,
	missing []int,
	tilesPerPixel float64,
) error {
	if tilesPerPixel == 1 && len(missing) > 1 {
		if done, err := c.renderUnitTileRectangle(ctx, entry, world, uses, missing); done || err != nil {
			return err
		}
	}
	renderOne := func(index int) error {
		use := &uses[index]
		tile, err := renderFastPreviewCacheTile(ctx, world, use.x, use.y, tilesPerPixel)
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

// A horizontal or vertical pan often reveals a strip of adjacent tiles.
// Rendering it together shares chart and cliff work across its edges.
func (c *fastPreviewCache) renderUnitTileRectangle(ctx context.Context, entry *fastPreviewWorldCache, world *fastPreviewWorld, uses []fastPreviewTileUse, missing []int) (bool, error) {
	first := uses[missing[0]]
	left, top, right, bottom := first.x, first.y, first.x, first.y
	for _, i := range missing[1:] {
		u := uses[i]
		left, top, right, bottom = min(left, u.x), min(top, u.y), max(right, u.x), max(bottom, u.y)
	}
	width, height := right-left+1, bottom-top+1
	if width*height != int64(len(missing)) {
		return false, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, int(width)*fastPreviewTileEdge, int(height)*fastPreviewTileEdge))
	if err := world.renderAlignedUnit(ctx, img, int(left)*fastPreviewTileEdge, int(top)*fastPreviewTileEdge); err != nil {
		return true, err
	}
	for _, i := range missing {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		u := &uses[i]
		u.image = sliceFastPreviewTile(img, (u.x-left)*fastPreviewTileEdge, (u.y-top)*fastPreviewTileEdge)
		c.storeTile(entry, u.key, u.image)
	}
	return true, nil
}

func renderFastPreviewCacheTile(
	ctx context.Context,
	world *fastPreviewWorld,
	tileX, tileY int64,
	tilesPerPixel float64,
) (*image.RGBA, error) {
	if tilesPerPixel == 1 {
		centerX := float64(tileX*fastPreviewTileEdge + fastPreviewTileEdge/2)
		centerY := float64(tileY*fastPreviewTileEdge + fastPreviewTileEdge/2)
		return world.render(ctx, fastPreviewTileEdge, 1, centerX, centerY)
	}
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
	if padding == 0 {
		return scratch, nil
	}
	return sliceFastPreviewTile(scratch, int64(padding), int64(padding)), nil
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
			return newFastPreviewWorldCache(key, settings, false, c.entityCacheBytes)
		}
		c.removeWorldLocked(victim)
	}

	entry := newFastPreviewWorldCache(key, settings, true, c.entityCacheBytes)
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
	entityCacheBytes int64,
) *fastPreviewWorldCache {
	world := newFastPreviewWorld(settings)
	world.setEntityCacheBudget(entityCacheBytes)
	return &fastPreviewWorldCache{
		key:        key,
		world:      world,
		renderGate: make(chan struct{}, fastPreviewRenderConcurrency()),
		idleWorlds: []*fastPreviewWorld{world},
		retained:   retained,
		references: 1,
	}
}

// takeRenderWorld is called only after acquiring both render admission gates.
// Extra evaluators are allocated on demand, so idle worlds retain one until
// concurrent work actually needs another. The gates bound pool growth.
func (entry *fastPreviewWorldCache) takeRenderWorld() *fastPreviewWorld {
	entry.poolMu.Lock()
	if n := len(entry.idleWorlds); n > 0 {
		world := entry.idleWorlds[n-1]
		entry.idleWorlds = entry.idleWorlds[:n-1]
		entry.poolMu.Unlock()
		return world
	}
	entry.poolMu.Unlock()
	world := newFastPreviewWorld(entry.world.settings)
	world.shareEntityCaches(entry.world)
	return world
}

func (entry *fastPreviewWorldCache) returnRenderWorld(world *fastPreviewWorld) {
	entry.poolMu.Lock()
	entry.idleWorlds = append(entry.idleWorlds, world)
	entry.poolMu.Unlock()
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
