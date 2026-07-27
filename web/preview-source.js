(function initPreviewSource(root, factory) {
  const exported = factory();
  if (typeof module === "object" && module.exports) module.exports = exported;
  if (root) root.PreviewSource = exported;
})(typeof window !== "undefined" ? window : globalThis, function previewSourceFactory() {
  const knownPlanets = Object.freeze(["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"]);

  function knownPlanetName(name) {
    const normalized = String(name || "").trim().toLowerCase();
    return knownPlanets.includes(normalized) ? normalized : "nauvis";
  }

  function inferPreviewPlanet(mapGen) {
    const elevation = mapGen?.property_expression_names?.elevation;
    for (const planet of knownPlanets.slice(1)) {
      if (elevation === `${planet}_elevation`) return planet;
    }

    const cliffName = mapGen?.cliff_settings?.name;
    const cliffControl = mapGen?.cliff_settings?.control;
    for (const planet of knownPlanets.slice(1)) {
      if (cliffName === `cliff-${planet}` || cliffControl === `${planet}_cliff`) return planet;
    }
    return "nauvis";
  }

  function fastTileSourceKey(profile, payload) {
    return JSON.stringify({
      profile,
      engine: "fast",
      planet: knownPlanetName(payload?.planet),
      seed: payload?.seed || "",
      mapGen: payload?.mapGen || {},
    });
  }

  return {
    fastTileSourceKey,
    inferPreviewPlanet,
    knownPlanetName,
    knownPlanets,
  };
});
