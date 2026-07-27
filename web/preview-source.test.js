const test = require("node:test");
const assert = require("node:assert/strict");
const {
  fastTileSourceKey,
  inferPreviewPlanet,
  knownPlanetName,
  knownPlanets,
  preferredPreviewEngine,
} = require("./preview-source.js");

test("all supported preview planets retain their identity", () => {
  assert.deepEqual(knownPlanets, ["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"]);
  for (const planet of knownPlanets) assert.equal(knownPlanetName(planet), planet);
  assert.equal(knownPlanetName(" VULCANUS "), "vulcanus");
  assert.equal(knownPlanetName("unknown"), "nauvis");
});

test("Fast tile source identity changes when only the planet changes", () => {
  const payload = {
    seed: "123456",
    mapGen: { seed: null, property_expression_names: {} },
  };
  const keys = knownPlanets.map((planet) => fastTileSourceKey("default:Default", {
    ...payload,
    planet,
  }));
  assert.equal(new Set(keys).size, knownPlanets.length);
});

test("Fast tile source identity ignores viewport-only changes", () => {
  const payload = {
    planet: "gleba",
    seed: "123456",
    mapGen: { seed: null, property_expression_names: { elevation: "gleba_elevation" } },
  };
  assert.equal(
    fastTileSourceKey("custom:planet", payload),
    fastTileSourceKey("custom:planet", {
      ...payload,
      size: 2048,
      zoom: "4",
      centerX: 512,
      centerY: -256,
    }),
  );
});

test("planet inference recognizes bundled elevation and cliff markers", () => {
  assert.equal(inferPreviewPlanet({ property_expression_names: { elevation: "vulcanus_elevation" } }), "vulcanus");
  assert.equal(inferPreviewPlanet({ property_expression_names: { elevation: "gleba_elevation" } }), "gleba");
  assert.equal(inferPreviewPlanet({ cliff_settings: { name: "cliff-fulgora" } }), "fulgora");
  assert.equal(inferPreviewPlanet({ property_expression_names: { elevation: "aquilo_elevation" } }), "aquilo");
  assert.equal(inferPreviewPlanet({ property_expression_names: {} }), "nauvis");
});

test("Space Age planets prefer Exact whenever Factorio is available", () => {
  for (const planet of knownPlanets.slice(1)) {
    assert.equal(preferredPreviewEngine("fast", planet, true, true), "factorio");
    assert.equal(preferredPreviewEngine("factorio", planet, true, true), "factorio");
  }
});

test("Nauvis preserves the requested engine and Space Age falls back when Exact is unavailable", () => {
  assert.equal(preferredPreviewEngine("fast", "nauvis", true, true), "fast");
  assert.equal(preferredPreviewEngine("factorio", "nauvis", true, true), "factorio");
  assert.equal(preferredPreviewEngine("fast", "gleba", false, true), "fast");
  assert.equal(preferredPreviewEngine("factorio", "gleba", false, true), "fast");
});
