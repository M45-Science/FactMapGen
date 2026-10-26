(function initPreviewGrid(root, factory) {
  const exported = factory();
  if (typeof module === "object" && module.exports) module.exports = exported;
  if (root) root.PreviewGrid = exported;
})(typeof window !== "undefined" ? window : globalThis, function previewGridFactory() {
  function layout(view) {
    const { size, tilesPerPixel, centerX, centerY } = view;
    if (![size, tilesPerPixel, centerX, centerY].every(Number.isFinite) || size <= 0 || tilesPerPixel <= 0) return null;
    const span = size * tilesPerPixel;
    // One spacing only: roughly two to four cells across the visible map.
    const spacing = Math.max(1, 2 ** Math.ceil(Math.log2(span / 4)));
    const positions = (center) => {
      const start = center - span / 2;
      const first = Math.ceil(start / spacing);
      const last = Math.floor((center + span / 2) / spacing);
      const lines = [];
      for (let index = first; index <= last; index++) {
        lines.push((index * spacing - start) / tilesPerPixel);
      }
      return lines;
    };
    return { spacing, size, x: positions(centerX), y: positions(centerY) };
  }

  return { layout };
});
