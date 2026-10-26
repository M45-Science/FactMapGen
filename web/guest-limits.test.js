const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

// Exercise the actual app controls without starting the application or network.
const app = fs.readFileSync(`${__dirname}/app.js`, "utf8");
function harness({ size = 1024, cap = 8192, session = null } = {}) {
  const context = vm.createContext({
    state: { session, config: { guestLimits: { maxViewSize: cap } } },
    minPreviewScale: 1 / 64,
    maxPreviewScale: 25,
    els: { previewZoom: { value: "25" } },
    currentPreviewSize: () => size,
    canUsePreviewZoom: () => true,
  });
  for (const name of ["maxPreviewScaleForSize", "currentPreviewScale", "positivePreviewNumber", "setFastTileView", "zoomPreviewAtPointer", "zoomPreviewByButton", "updatePreviewZoomControls"]) {
    const start = app.indexOf(`function ${name}(`);
    const end = app.indexOf("\n}\n", start) + 2;
    vm.runInContext(app.slice(start, end), context);
  }
  return context;
}

test("guest scale follows world view cap and viewport size; signed-in users retain their zoom", () => {
  for (const size of [512, 768, 1024, 2048, 4096]) {
    const ctx = harness({ size });
    assert.ok(Number(ctx.currentPreviewScale()) * size <= 8192);
    assert.equal(ctx.maxPreviewScaleForSize(), Math.min(25, 8192 / size));
  }
  assert.equal(harness({ cap: 4096 }).currentPreviewScale(), "4");
  assert.equal(harness({ session: {} }).currentPreviewScale(), "25");
});

test("resizing an active tiled view clamps its scale to the new view size", () => {
  const ctx = harness();
  const displayed = { tiled: true, size: 1024, tilesPerPixel: 8 };
  let view;
  Object.assign(ctx, {
    displayedPreviewForCurrentProfile: () => displayed,
    previewTileViewer: { isActive: () => true, setView: (next) => { view = next; } },
    finitePreviewNumber: Number,
    previewViewForCurrentProfile: () => ({}),
    updatePreviewViewControls: () => {},
  });
  assert.equal(ctx.setFastTileView(0, 0, 8, 2048), true);
  assert.equal(view.scale, 4);
  assert.equal(displayed.tilesPerPixel * displayed.size, 8192);
});

test("wheel zoom stops at the cap without rounding above it", () => {
  const ctx = harness({ size: 768 });
  let scale;
  Object.assign(ctx, {
    previewInteractionEnabled: () => true,
    pointerViewForCurrentPreview: () => ({ size: 768, tilesPerPixel: 10, centerX: 0, centerY: 0 }),
    WheelEvent: { DOM_DELTA_LINE: 1, DOM_DELTA_PAGE: 2 },
    previewViewForCurrentProfile: () => ({}),
    updatePreviewViewControls: () => {},
    fastTileViewerActive: () => true,
    setFastTileView: (x, y, next) => { scale = next; },
    refreshVisibleFastTiles: () => {},
  });
  ctx.els.previewFrame = { getBoundingClientRect: () => ({ width: 768, height: 768, left: 0, top: 0 }) };
  ctx.zoomPreviewAtPointer({ deltaY: 300, deltaMode: 0, clientX: 384, clientY: 384, preventDefault() {} });
  assert.ok(scale * 768 <= 8192);
  assert.equal(scale, 8192 / 768);
});


test("zoom buttons use the view center and halve or double its scale", () => {
  const ctx = harness();
  let event;
  ctx.els.previewFrame = { getBoundingClientRect: () => ({ left: 50, top: 100, width: 800, height: 600 }) };
  ctx.zoomPreviewAtPointer = (next) => { event = next; };
  ctx.zoomPreviewByButton(-1);
  assert.equal(event.deltaY, -300);
  assert.equal(event.clientX, 450);
  assert.equal(event.clientY, 400);
  ctx.zoomPreviewByButton(1);
  assert.equal(event.deltaY, 300);
});

test("zoom control disables the correct button at each limit and updates editable scale bounds", () => {
  const ctx = harness();
  ctx.els.previewZoomOut = {};
  ctx.els.previewZoomIn = {};
  ctx.pointerViewForCurrentPreview = () => ({ size: 1024 });
  ctx.updatePreviewZoomControls(true);
  assert.equal(ctx.els.previewZoom.max, "8");
  assert.equal(ctx.els.previewZoomOut.disabled, true);
  assert.equal(ctx.els.previewZoomIn.disabled, false);
  ctx.els.previewZoom.value = String(1 / 64);
  ctx.updatePreviewZoomControls(true);
  assert.equal(ctx.els.previewZoomOut.disabled, false);
  assert.equal(ctx.els.previewZoomIn.disabled, true);
  ctx.updatePreviewZoomControls(false);
  assert.equal(ctx.els.previewZoomOut.disabled, true);
  assert.equal(ctx.els.previewZoomIn.disabled, true);
});
