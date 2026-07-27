const test = require("node:test");
const assert = require("node:assert/strict");
const {
  TilePreviewViewer,
  perimeterPrefetchCoordinates,
  screenPointToWorld,
  snapTileScreenRect,
  tileRangeForView,
  tileRequestCoordinates,
  tileScreenRect,
} = require("./tile-viewer.js");

test("screen points map to world coordinates through pan, zoom, and display scaling", () => {
  const view = { size: 1000, scale: 2, centerX: 100, centerY: -50 };
  assert.deepEqual(screenPointToWorld(view, 250, 125, 500, 250), { x: 100, y: -50 });
  assert.deepEqual(screenPointToWorld(view, 0, 0, 500, 250), { x: -900, y: -1050 });
  assert.deepEqual(screenPointToWorld(view, 500, 250, 500, 250), { x: 1100, y: 950 });
  assert.deepEqual(
    screenPointToWorld({ size: 100, tilesPerPixel: 4, centerX: 10, centerY: 20 }, 0, 0, 100, 100),
    { x: -190, y: -180 },
  );
});

class FakeImage {
  constructor() {
    this.style = {};
    this.removed = false;
  }

  set src(value) {
    this.url = value;
    queueMicrotask(() => this.onload?.());
  }

  remove() {
    this.removed = true;
  }
}

function createFakeContainer() {
  const children = [];
  return {
    children,
    style: {},
    append(image) {
      children.push(image);
    },
  };
}

test("perimeter prefetch dimensions follow the current viewport", () => {
  const singleTileView = { size: 512, scale: 1, centerX: 256, centerY: 256 };
  assert.equal(perimeterPrefetchCoordinates(singleTileView).length, 8);

  const fourTileView = { size: 1024, scale: 1, centerX: 0, centerY: 0 };
  assert.equal(perimeterPrefetchCoordinates(fourTileView).length, 12);
});

test("viewer prefetches one viewport perimeter after visible tiles load", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const requests = [];
  const view = { size: 512, scale: 1, centerX: 256, centerY: 256 };
  const coordinateKeys = (coordinates) => new Set(
    coordinates.map(({ tileX, tileY }) => `${tileX},${tileY}`),
  );
  const firstRing = coordinateKeys(perimeterPrefetchCoordinates(view));
  let resolveVisible;
  const viewer = new TilePreviewViewer(createFakeContainer(), {
    tileSize: 512,
    maxTiles: 64,
    maxConcurrent: 8,
  });
  viewer.configure({
    sourceKey: "seed-123456",
    tileScale: 1,
    view,
    requestTile: (tile) => {
      requests.push(`${tile.tileX},${tile.tileY}`);
      if (tile.tileX === 0 && tile.tileY === 0) {
        return new Promise((resolve) => { resolveVisible = resolve; });
      }
      return `/tiles/${tile.tileX}/${tile.tileY}`;
    },
  });

  const refresh = viewer.refresh();
  await Promise.resolve();
  assert.deepEqual(requests, ["0,0"], "offscreen requests must wait for the visible tile");

  resolveVisible("/tiles/0/0");
  assert.deepEqual(await refresh, {
    loaded: 1, failed: 0, pending: 0, total: 1, complete: true,
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(new Set(requests), new Set(["0,0", ...firstRing]));
});

test("changing settings or presets clears the prior client tile cache", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const requests = [];
  const viewer = new TilePreviewViewer(createFakeContainer(), {
    tileSize: 512, maxTiles: 64, prefetchPerimeters: false,
  });
  const configure = (sourceKey) => viewer.configure({
    sourceKey,
    tileScale: 1,
    view: { size: 512, scale: 1, centerX: 256, centerY: 256 },
    requestTile: async (tile) => {
      requests.push(tile.sourceKey);
      return `/tiles/${tile.sourceKey}/${tile.tileX}/${tile.tileY}`;
    },
  });

  configure("preset-a-settings-a");
  await viewer.refresh();
  const previousImages = Array.from(viewer.cache.values(), (entry) => entry.image);
  assert.equal(viewer.cache.size, 1);
  configure("preset-a-settings-a");
  assert.equal(viewer.cache.size, 1, "reconfiguring the same source should retain its cached tiles");
  assert.ok(previousImages.every((image) => !image.removed));

  configure("preset-b-settings-b");
  assert.equal(viewer.cache.size, 0, "old source tiles should be discarded before loading the new source");
  assert.ok(previousImages.every((image) => image.removed));
  await viewer.refresh();
  assert.equal(viewer.cache.size, 1);
  assert.deepEqual(requests, ["preset-a-settings-a", "preset-b-settings-b"]);
});

test("tile viewer reuses one server resolution while panning and zooming", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const container = createFakeContainer();
  const requests = [];
  const viewer = new TilePreviewViewer(container, {
    tileSize: 512, maxTiles: 64, maxConcurrent: 2, prefetchPerimeters: false,
  });
  viewer.configure({
    sourceKey: "seed-123456",
    view: { size: 1024, scale: 1, centerX: 0, centerY: 0 },
    requestTile: async (tile) => {
      requests.push(tile);
      return `/tiles/${tile.scale}/${tile.tileX}/${tile.tileY}`;
    },
  });

  assert.deepEqual(await viewer.refresh(), {
    loaded: 4, failed: 0, pending: 0, total: 4, complete: true,
  });
  assert.equal(requests.length, 4);

  viewer.setView({ size: 1024, scale: 1, centerX: 256, centerY: 0 });
  assert.deepEqual(await viewer.refresh(), {
    loaded: 6, failed: 0, pending: 0, total: 6, complete: true,
  });
  assert.equal(requests.length, 6, "pan should request only two newly exposed tiles");

  viewer.setView({ size: 1024, scale: 1, centerX: 0, centerY: 0 });
  await viewer.refresh();
  assert.equal(requests.length, 6, "returning to cached tiles should not request them again");
  viewer.setView({ size: 1024, scale: 1, centerX: 256, centerY: 0 });
  await viewer.refresh();
  assert.equal(requests.length, 6, "revisiting the panned area should reuse its tiles");

  for (let centerX = 512; centerX <= 8192; centerX += 512) {
    viewer.setView({ size: 1024, scale: 1, centerX, centerY: 0 });
    await viewer.refresh();
  }
  const requestsAfterLongPan = requests.length;
  assert.ok(viewer.cache.size > 6, "loaded offscreen tiles should remain in the client LRU");
  viewer.setView({ size: 1024, scale: 1, centerX: 0, centerY: 0 });
  await viewer.refresh();
  assert.equal(requests.length, requestsAfterLongPan, "a long pan should not wipe cached origin tiles");
  viewer.setView({ size: 1024, scale: 1, centerX: 256, centerY: 0 });
  await viewer.refresh();

  viewer.setView({ size: 1024, scale: 0.5, centerX: 256, centerY: 0 });
  const zoomed = container.children.filter((image) => image.style.display === "block");
  assert.ok(zoomed.length > 0, "fixed-resolution tiles should scale immediately");
  assert.ok(zoomed.some((image) => image.style.width === "1024px"));
  assert.equal(requests.length, requestsAfterLongPan, "client zoom should not request tiles");

  await viewer.refresh();
  assert.equal(requests.length, requestsAfterLongPan, "later refresh should reuse the fixed-resolution tiles");
  assert.ok(requests.every((tile) => tile.scale === 1));
});

test("zooming out requests only missing canonical source tiles", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const requests = [];
  const viewer = new TilePreviewViewer(createFakeContainer(), {
    tileSize: 512,
    maxTiles: 64,
    maxConcurrent: 4,
    prefetchPerimeters: false,
  });
  viewer.configure({
    sourceKey: "seed-123456",
    tileScale: 1,
    view: { size: 1024, scale: 1, centerX: 0, centerY: 0 },
    requestTile: async (tile) => {
      requests.push(tile);
      return `/tiles/${tile.tileX}/${tile.tileY}`;
    },
  });

  await viewer.refresh();
  assert.equal(requests.length, 4);
  viewer.setView({ size: 1024, scale: 2, centerX: 0, centerY: 0 });
  assert.equal(requests.length, 4, "client transform should be immediate and make no synchronous request");
  const summary = await viewer.refresh();
  assert.deepEqual(summary, {
    loaded: 16, failed: 0, pending: 0, total: 16, complete: true,
  });
  assert.equal(requests.length, 16, "zoom-out should request the twelve newly visible tiles");
  assert.ok(requests.every((tile) => tile.scale === 1), "zoom must retain one source resolution");

  viewer.setView({ size: 1024, scale: 1, centerX: 0, centerY: 0 });
  await viewer.refresh();
  assert.equal(requests.length, 16, "zooming back into cached tiles should make no request");
});

test("loaded tiles remain cached until the configured LRU limit", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const requests = [];
  const viewer = new TilePreviewViewer(createFakeContainer(), {
    tileSize: 512,
    maxTiles: 4,
    maxConcurrent: 2,
    prefetchPerimeters: false,
  });
  viewer.configure({
    sourceKey: "seed-123456",
    tileScale: 1,
    view: { size: 512, scale: 1, centerX: 256, centerY: 256 },
    requestTile: async (tile) => {
      requests.push(`${tile.tileX},${tile.tileY}`);
      return `/tiles/${tile.tileX}/${tile.tileY}`;
    },
  });

  for (let tileX = 0; tileX < 4; tileX++) {
    viewer.setView({ size: 512, scale: 1, centerX: tileX * 512 + 256, centerY: 256 });
    await viewer.refresh();
  }
  assert.equal(requests.length, 4);
  assert.equal(viewer.cache.size, 4);

  viewer.setView({ size: 512, scale: 1, centerX: 256, centerY: 256 });
  await viewer.refresh();
  assert.equal(requests.length, 4, "tiles inside the LRU budget should be reused");

  viewer.setView({ size: 512, scale: 1, centerX: 4 * 512 + 256, centerY: 256 });
  await viewer.refresh();
  assert.equal(viewer.cache.size, 4, "the decoded tile cache should honor its limit");
  assert.equal(requests.length, 5);
});

test("zooming back in aborts obsolete active and queued tile requests", async (t) => {
  const originalDocument = global.document;
  global.document = { createElement: () => new FakeImage() };
  t.after(() => { global.document = originalDocument; });

  const aborted = [];
  const viewer = new TilePreviewViewer(createFakeContainer(), {
    tileSize: 512,
    maxTiles: 64,
    maxConcurrent: 8,
    prefetchPerimeters: false,
  });
  viewer.configure({
    sourceKey: "seed-123456",
    tileScale: 1,
    view: { size: 1024, scale: 4, centerX: 0, centerY: 0 },
    requestTile: (tile) => new Promise((resolve, reject) => {
      tile.signal.addEventListener("abort", () => {
        aborted.push(`${tile.tileX},${tile.tileY}`);
        const error = new Error("aborted");
        error.name = "AbortError";
        reject(error);
      }, { once: true });
    }),
  });

  const wideRefresh = viewer.refresh();
  await Promise.resolve();
  assert.equal(viewer.activeRequests, 8);
  assert.equal(viewer.queue.length, 56);

  viewer.setView({ size: 1024, scale: 1, centerX: 4096, centerY: 0 });
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(aborted.length, 8, "obsolete active fetches should receive abort signals");
  assert.equal(viewer.queue.length, 0, "obsolete queued tiles should be discarded");

  viewer.deactivate();
  await wideRefresh;
  await Promise.resolve();
  assert.equal(viewer.activeRequests, 0);
});

test("tile requests use deterministic center-biased jitter instead of row-major order", () => {
  const view = { size: 1024, scale: 4, centerX: 0, centerY: 0 };
  const ordered = tileRequestCoordinates(view, 512, 1);
  assert.equal(ordered.length, 64);
  assert.deepEqual(ordered, tileRequestCoordinates(view, 512, 1));
  assert.notDeepEqual(
    { tileX: ordered[0].tileX, tileY: ordered[0].tileY },
    { tileX: -4, tileY: -4 },
    "the first request should not start in the top-left corner",
  );
  const firstEight = ordered.slice(0, 8);
  assert.ok(new Set(firstEight.map((tile) => tile.tileX)).size >= 3);
  assert.ok(new Set(firstEight.map((tile) => tile.tileY)).size >= 3);
});

test("1024px origin view uses four aligned 512px tiles", () => {
  assert.deepEqual(
    tileRangeForView({ size: 1024, scale: 1, centerX: 0, centerY: 0 }),
    { minX: -1, maxX: 0, minY: -1, maxY: 0 },
  );
  assert.deepEqual(
    tileScreenRect(-1, -1, 1, { size: 1024, scale: 1, centerX: 0, centerY: 0 }),
    { left: 0, top: 0, size: 512 },
  );
  assert.deepEqual(
    tileScreenRect(0, 0, 1, { size: 1024, scale: 1, centerX: 0, centerY: 0 }),
    { left: 512, top: 512, size: 512 },
  );
});

test("panning requests only newly exposed tile columns", () => {
  const original = tileRangeForView({ size: 1024, scale: 1, centerX: 0, centerY: 0 });
  const panned = tileRangeForView({ size: 1024, scale: 1, centerX: 256, centerY: 0 });
  assert.deepEqual(original, { minX: -1, maxX: 0, minY: -1, maxY: 0 });
  assert.deepEqual(panned, { minX: -1, maxX: 1, minY: -1, maxY: 0 });
});

test("fixed-resolution tiles scale around the client view", () => {
  const zoomedView = { size: 1024, scale: 0.5, centerX: 0, centerY: 0 };
  assert.deepEqual(
    tileScreenRect(-1, -1, 1, zoomedView),
    { left: -512, top: -512, size: 1024 },
  );
  assert.deepEqual(
    tileScreenRect(0, 0, 1, zoomedView),
    { left: 512, top: 512, size: 1024 },
  );
});

test("fractional client zoom snaps adjacent tiles to one shared edge", () => {
  const view = { size: 1024, scale: 1.7, centerX: 352, centerY: -176 };
  const left = snapTileScreenRect(tileScreenRect(-1, 0, 1, view));
  const right = snapTileScreenRect(tileScreenRect(0, 0, 1, view));
  const top = snapTileScreenRect(tileScreenRect(0, -1, 1, view));
  const bottom = snapTileScreenRect(tileScreenRect(0, 0, 1, view));
  assert.equal(left.left + left.width, right.left);
  assert.equal(top.top + top.height, bottom.top);
});

test("fractional scales preserve seamless world alignment", () => {
  const view = { size: 1024, scale: 2.75, centerX: 352, centerY: -176 };
  const left = tileScreenRect(-1, 0, 2.75, view);
  const right = tileScreenRect(0, 0, 2.75, view);
  assert.equal(left.left + left.size, right.left);
  assert.equal(left.top, right.top);
  assert.equal(left.size, 512);
});
