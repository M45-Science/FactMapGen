const test = require("node:test");
const assert = require("node:assert/strict");
const { layout } = require("./preview-grid.js");

test("grid uses one power-of-two spacing and stays sparse across zooms and sizes", () => {
  for (const size of [256, 512, 768, 1024, 4096]) {
    for (const tilesPerPixel of [1 / 64, 0.4, 1, 2.75, 25, 64]) {
      const grid = layout({ size, tilesPerPixel, centerX: 123456.75, centerY: -987654.25 });
      assert.ok(Number.isInteger(Math.log2(grid.spacing)));
      assert.ok(grid.spacing >= size * tilesPerPixel / 4);
      assert.ok(grid.spacing < size * tilesPerPixel / 2);
      for (const axis of [grid.x, grid.y]) {
        assert.ok(axis.length >= 2 && axis.length <= 5);
        assert.ok(axis.every((pixel) => pixel >= 0 && pixel <= size));
      }
    }
  }
});

test("grid remains aligned to world coordinates while panning, including negative coordinates", () => {
  const view = { size: 1024, tilesPerPixel: 1, centerX: 0, centerY: 0 };
  assert.deepEqual(layout(view).x, [0, 256, 512, 768, 1024]);
  const moved = layout({ ...view, centerX: 100, centerY: -100 });
  assert.equal(moved.spacing, 256);
  assert.deepEqual(moved.x, [156, 412, 668, 924]);
  assert.deepEqual(moved.y, [100, 356, 612, 868]);
});

test("grid spacing switches with zoom and rejects invalid views", () => {
  const view = { size: 1024, tilesPerPixel: 1, centerX: 0, centerY: 0 };
  assert.equal(layout({ ...view, tilesPerPixel: 0.5 }).spacing, 128);
  assert.equal(layout({ ...view, tilesPerPixel: 2 }).spacing, 512);
  assert.equal(layout({ ...view, tilesPerPixel: NaN }), null);
  assert.equal(layout({ ...view, size: 0 }), null);
});
