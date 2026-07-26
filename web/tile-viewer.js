(function initTileViewer(root, factory) {
  const exported = factory();
  if (typeof module === "object" && module.exports) module.exports = exported;
  if (root) root.TilePreviewViewer = exported.TilePreviewViewer;
})(typeof window !== "undefined" ? window : globalThis, function tileViewerFactory() {
  function finiteNumber(value, fallback = 0) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function positiveNumber(value, fallback = 1) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : fallback;
  }

  function tileRangeForView(view, tileSize = 512, tileScale = view.scale) {
    const size = positiveNumber(view.size);
    const viewScale = positiveNumber(view.scale);
    const sourceScale = positiveNumber(tileScale, viewScale);
    const centerX = finiteNumber(view.centerX);
    const centerY = finiteNumber(view.centerY);
    const span = positiveNumber(tileSize, 512) * sourceScale;
    const halfSpan = size * viewScale / 2;
    return {
      minX: Math.floor((centerX - halfSpan) / span),
      maxX: Math.ceil((centerX + halfSpan) / span) - 1,
      minY: Math.floor((centerY - halfSpan) / span),
      maxY: Math.ceil((centerY + halfSpan) / span) - 1,
    };
  }

  function tileScreenRect(tileX, tileY, tileScale, view, tileSize = 512) {
    const size = positiveNumber(view.size);
    const viewScale = positiveNumber(view.scale);
    const sourceScale = positiveNumber(tileScale);
    const worldLeft = tileX * tileSize * sourceScale;
    const worldTop = tileY * tileSize * sourceScale;
    const worldViewLeft = finiteNumber(view.centerX) - size * viewScale / 2;
    const worldViewTop = finiteNumber(view.centerY) - size * viewScale / 2;
    const displaySize = tileSize * sourceScale / viewScale;
    return {
      left: (worldLeft - worldViewLeft) / viewScale,
      top: (worldTop - worldViewTop) / viewScale,
      size: displaySize,
    };
  }

  function screenPointToWorld(view, screenX, screenY, displayWidth, displayHeight) {
    const size = positiveNumber(view.size);
    const scale = positiveNumber(view.scale, positiveNumber(view.tilesPerPixel));
    const width = positiveNumber(displayWidth, size);
    const height = positiveNumber(displayHeight, size);
    return {
      x: finiteNumber(view.centerX) + (finiteNumber(screenX) * size / width - size / 2) * scale,
      y: finiteNumber(view.centerY) + (finiteNumber(screenY) * size / height - size / 2) * scale,
    };
  }

  function snapTileScreenRect(rect) {
    const left = Math.round(rect.left);
    const top = Math.round(rect.top);
    const right = Math.round(rect.left + rect.size);
    const bottom = Math.round(rect.top + rect.size);
    return {
      left,
      top,
      width: Math.max(1, right - left),
      height: Math.max(1, bottom - top),
    };
  }

  function sameScale(first, second) {
    return Math.abs(first - second) <= Math.max(1, Math.abs(first), Math.abs(second)) * 1e-12;
  }

  function scaleKey(scale) {
    return Number(scale).toPrecision(12);
  }

  function tileOrderJitter(tileX, tileY) {
    let hash = Math.imul(tileX ^ 0x6d2b79f5, 0x1b873593)
      ^ Math.imul(tileY ^ 0x85ebca6b, 0x27d4eb2d);
    hash ^= hash >>> 15;
    hash = Math.imul(hash, 0x2c1b3c6d);
    hash ^= hash >>> 12;
    return (hash >>> 0) / 0x100000000;
  }

  function tileRequestCoordinates(view, tileSize = 512, tileScale = view.scale) {
    const range = tileRangeForView(view, tileSize, tileScale);
    const span = positiveNumber(tileSize, 512) * positiveNumber(tileScale, view.scale);
    const centerTileX = finiteNumber(view.centerX) / span - 0.5;
    const centerTileY = finiteNumber(view.centerY) / span - 0.5;
    const coordinates = [];
    for (let tileY = range.minY; tileY <= range.maxY; tileY++) {
      for (let tileX = range.minX; tileX <= range.maxX; tileX++) {
        const distance = Math.hypot(tileX - centerTileX, tileY - centerTileY);
        coordinates.push({
          tileX,
          tileY,
          priority: distance + tileOrderJitter(tileX, tileY) * 0.8,
        });
      }
    }
    coordinates.sort((first, second) => first.priority - second.priority
      || first.tileY - second.tileY || first.tileX - second.tileX);
    return coordinates;
  }

  class TilePreviewViewer {
    constructor(container, options = {}) {
      if (!container) throw new Error("tile viewer container is required");
      this.container = container;
      this.tileSize = positiveNumber(options.tileSize, 512);
      this.maxTiles = Math.max(4, Math.round(positiveNumber(options.maxTiles, 64)));
      this.maxConcurrent = Math.max(1, Math.round(positiveNumber(options.maxConcurrent, 4)));
      this.onState = typeof options.onState === "function" ? options.onState : () => {};
      this.cache = new Map();
      this.queue = [];
      this.activeRequests = 0;
      this.active = false;
      this.sourceKey = "";
      this.requestTile = null;
      this.view = { size: 1024, scale: 1, centerX: 0, centerY: 0 };
      this.tileScale = 1;
      this.required = new Set();
      this.sequence = 0;
      this.viewRevision = 0;
    }

    configure(options) {
      const nextSourceKey = String(options.sourceKey || "");
      if (!nextSourceKey) throw new Error("tile viewer source key is required");
      if (typeof options.requestTile !== "function") throw new Error("tile viewer request callback is required");
      const sourceChanged = this.sourceKey !== nextSourceKey;
      this.active = true;
      this.sourceKey = nextSourceKey;
      this.requestTile = options.requestTile;
      if (sourceChanged) {
        this.clear();
        this.tileScale = positiveNumber(options.tileScale, options.view?.scale || this.view.scale);
      }
      this.setView(options.view || this.view);
      this.container.style.display = "block";
      this.render();
    }

    setView(view) {
      const next = {
        size: positiveNumber(view.size, this.view.size),
        scale: positiveNumber(view.scale, this.view.scale),
        centerX: finiteNumber(view.centerX, this.view.centerX),
        centerY: finiteNumber(view.centerY, this.view.centerY),
      };
      this.view = next;
      this.viewRevision++;
      this.cancelObsoleteRequests();
      this.render();
    }

    deactivate(options = {}) {
      this.active = false;
      this.cancelPending("map tile viewer was deactivated");
      this.required.clear();
      this.container.style.display = "none";
      for (const entry of this.cache.values()) entry.image.style.display = "none";
      if (options.clear) this.clear();
    }

    clear() {
      this.cancelPending("map tile cache was cleared");
      for (const entry of this.cache.values()) entry.image.remove();
      this.cache.clear();
      this.required.clear();
    }

    isActive() {
      return this.active;
    }

    async refresh() {
      if (!this.active || !this.requestTile) return { loaded: 0, failed: 0, total: 0, complete: false };
      const sourceKey = this.sourceKey;
      const targetScale = this.tileScale;
      const viewRevision = this.viewRevision;
      const required = new Set();
      const promises = [];
      const coordinates = tileRequestCoordinates(this.view, this.tileSize, targetScale);
      for (const { tileX, tileY } of coordinates) {
        const entry = this.ensureEntry(sourceKey, targetScale, tileX, tileY);
        required.add(entry.key);
        entry.lastUsed = ++this.sequence;
        promises.push(entry.promise);
      }
      this.required = required;
      this.render();
      this.emitState();
      const results = await Promise.allSettled(promises);
      if (
        !this.active
        || this.sourceKey !== sourceKey
        || !sameScale(this.tileScale, targetScale)
        || this.viewRevision !== viewRevision
      ) {
        return this.stateSummary();
      }
      this.render();
      this.emitState();
      const summary = this.stateSummary();
      if (summary.loaded === 0 && results.length > 0) {
        const rejected = results.find((result) => result.status === "rejected");
        throw rejected?.reason || new Error("map tiles failed to load");
      }
      return summary;
    }

    ensureEntry(sourceKey, scale, tileX, tileY) {
      const key = `${sourceKey}|${scaleKey(scale)}|${tileX}|${tileY}`;
      const cached = this.cache.get(key);
      if (cached) return cached;

      const image = document.createElement("img");
      image.className = "preview-tile";
      image.alt = "";
      image.draggable = false;
      image.decoding = "async";
      image.style.display = "none";
      this.container.append(image);

      let resolvePromise;
      let rejectPromise;
      const promise = new Promise((resolve, reject) => {
        resolvePromise = resolve;
        rejectPromise = reject;
      });
      const entry = {
        key, sourceKey, scale, tileX, tileY, image, promise, requestTile: this.requestTile,
        resolve: resolvePromise,
        reject: rejectPromise,
        state: "queued",
        lastUsed: ++this.sequence,
        error: null,
        controller: null,
        cancelImageLoad: null,
      };
      this.cache.set(key, entry);
      this.queue.push(entry);
      this.pumpQueue();
      return entry;
    }

    cancelPending(message) {
      for (const entry of Array.from(this.cache.values())) {
        if (entry.state === "queued" || entry.state === "loading") {
          this.cancelEntry(entry, message);
        }
      }
      this.queue = this.queue.filter((entry) => entry.state === "queued");
    }

    cancelObsoleteRequests() {
      if (!this.active) return;
      const range = tileRangeForView(this.view, this.tileSize, this.tileScale);
      const visible = new Set();
      for (const entry of Array.from(this.cache.values())) {
        const required = entry.sourceKey === this.sourceKey
          && sameScale(entry.scale, this.tileScale)
          && entry.tileX >= range.minX && entry.tileX <= range.maxX
          && entry.tileY >= range.minY && entry.tileY <= range.maxY;
        if (required) {
          visible.add(entry.key);
        } else if (entry.state === "queued" || entry.state === "loading") {
          this.cancelEntry(entry, "map tile left the active viewport");
        }
      }
      this.queue = this.queue.filter((entry) => entry.state === "queued");
      this.required = visible;
      this.prune();
      this.emitState();
    }

    cancelEntry(entry, message) {
      if (entry.state !== "queued" && entry.state !== "loading") return;
      const error = new Error(message);
      error.name = "AbortError";
      entry.state = "canceled";
      entry.error = error;
      this.cache.delete(entry.key);
      entry.controller?.abort();
      entry.cancelImageLoad?.(error);
      entry.image.remove();
      entry.reject(error);
    }

    pumpQueue() {
      if (!this.active) return;
      while (this.activeRequests < this.maxConcurrent && this.queue.length > 0) {
        const entry = this.queue.shift();
        if (!entry || entry.state !== "queued") continue;
        entry.state = "loading";
        entry.controller = new AbortController();
        this.activeRequests++;
        const centerX = (entry.tileX + 0.5) * this.tileSize * entry.scale;
        const centerY = (entry.tileY + 0.5) * this.tileSize * entry.scale;
        Promise.resolve(entry.requestTile({
          tileX: entry.tileX,
          tileY: entry.tileY,
          tileSize: this.tileSize,
          scale: entry.scale,
          centerX,
          centerY,
          sourceKey: entry.sourceKey,
          signal: entry.controller.signal,
        }))
          .then((url) => this.loadEntryImage(entry, url))
          .catch((error) => this.finishEntry(entry, "failed", error))
          .finally(() => {
            entry.controller = null;
            this.activeRequests--;
            this.pumpQueue();
          });
      }
    }

    loadEntryImage(entry, url) {
      return new Promise((resolve, reject) => {
        const settle = (callback, value) => {
          entry.cancelImageLoad = null;
          entry.image.onload = null;
          entry.image.onerror = null;
          callback(value);
        };
        entry.cancelImageLoad = (error) => settle(reject, error);
        entry.image.onload = () => settle(resolve);
        entry.image.onerror = () => settle(reject, new Error("map tile image failed to load"));
        entry.image.src = String(url || "");
      }).then(
        () => this.finishEntry(entry, "loaded"),
        (error) => this.finishEntry(entry, "failed", error),
      );
    }

    finishEntry(entry, state, error = null) {
      if (entry.state === "canceled") return;
      entry.state = state;
      entry.error = error;
      if (state === "loaded") entry.resolve(entry);
      else entry.reject(error || new Error("map tile failed to load"));
      this.prune();
      this.render();
      this.emitState();
    }

    render() {
      if (!this.active) return;
      for (const entry of this.cache.values()) {
        let visible = entry.sourceKey === this.sourceKey && entry.state === "loaded";
        const target = sameScale(entry.scale, this.tileScale);
        visible = visible && target;
        if (visible) {
          const rect = tileScreenRect(entry.tileX, entry.tileY, entry.scale, this.view, this.tileSize);
          visible = rect.left < this.view.size && rect.top < this.view.size &&
            rect.left + rect.size > 0 && rect.top + rect.size > 0;
          if (visible) {
            const snapped = snapTileScreenRect(rect);
            entry.image.style.transform = `translate3d(${snapped.left}px, ${snapped.top}px, 0)`;
            entry.image.style.width = `${snapped.width}px`;
            entry.image.style.height = `${snapped.height}px`;
            entry.image.style.zIndex = "2";
            entry.image.style.display = "block";
            entry.lastUsed = ++this.sequence;
          }
        }
        if (!visible) entry.image.style.display = "none";
      }
    }

    stateSummary() {
      let loaded = 0;
      let failed = 0;
      let pending = 0;
      for (const key of this.required) {
        const state = this.cache.get(key)?.state;
        if (state === "loaded") loaded++;
        else if (state === "failed") failed++;
        else pending++;
      }
      const total = this.required.size;
      return { loaded, failed, pending, total, complete: total > 0 && loaded === total };
    }

    emitState() {
      this.onState(this.stateSummary());
    }

    prune() {
      if (this.cache.size <= this.maxTiles) return;
      const removable = Array.from(this.cache.values())
        .filter((entry) => !this.required.has(entry.key) && (entry.state === "loaded" || entry.state === "failed"))
        .sort((first, second) => first.lastUsed - second.lastUsed);
      while (this.cache.size > this.maxTiles && removable.length > 0) {
        const entry = removable.shift();
        this.cache.delete(entry.key);
        entry.image.remove();
      }
    }
  }

  TilePreviewViewer.screenPointToWorld = screenPointToWorld;

  return {
    TilePreviewViewer,
    screenPointToWorld,
    snapTileScreenRect,
    tileRangeForView,
    tileRequestCoordinates,
    tileScreenRect,
  };
});
