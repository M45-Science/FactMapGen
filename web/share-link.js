(function initShareLink(root, factory) {
  const exported = factory();
  if (typeof module === "object" && module.exports) module.exports = exported;
  if (root) root.PreviewShareLink = exported;
})(typeof window !== "undefined" ? window : globalThis, function shareLinkFactory() {
  const planets = ["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"];
  const maxFragmentLength = 512000;

  function viewFromParams(params) {
    const planet = params.get("p") || "nauvis";
    if (!planets.includes(planet)) throw new Error("Unknown planet in shared link.");
    const number = (key, fallback, min, max) => {
      const raw = params.get(key);
      const value = raw === null ? fallback : Number(raw);
      if (raw === "" || !Number.isFinite(value) || value < min || value > max) {
        throw new Error("Invalid view in shared link.");
      }
      return value;
    };
    return {
      planet,
      centerX: number("x", 0, -1e9, 1e9),
      centerY: number("y", 0, -1e9, 1e9),
      scale: number("z", 1, 1 / 64, 25),
    };
  }

  function parse(hash) {
    const fragment = String(hash || "").replace(/^#/, "");
    if (fragment.length > maxFragmentLength) throw new Error("Shared link is too large.");
    const params = new URLSearchParams(fragment);
    if (!params.has("m")) return null;
    const encoded = params.get("m");
    if (!/^[A-Za-z0-9_-]+$/.test(encoded) || encoded.length % 4 === 1) {
      throw new Error("Invalid map exchange string in shared link.");
    }
    const base64 = encoded.replace(/-/g, "+").replace(/_/g, "/");
    return {
      exchangeString: `>>>${base64.padEnd(Math.ceil(base64.length / 4) * 4, "=")}<<<`,
      ...viewFromParams(params),
    };
  }

  function create(baseURL, exchangeString, view = {}) {
    const match = /^>>>([A-Za-z0-9+/]+={0,2})<<<$/.exec(String(exchangeString).replace(/\s/g, ""));
    if (!match) throw new Error("Cannot share an invalid map exchange string.");
    // The exchange string is already compressed. Only replace its Base64
    // alphabet and omit padding/wrappers to avoid URL escaping overhead.
    const params = new URLSearchParams({ m: match[1].replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") });
    if (view.planet && view.planet !== "nauvis") params.set("p", view.planet);
    if (view.centerX !== undefined && view.centerX !== 0) params.set("x", String(view.centerX));
    if (view.centerY !== undefined && view.centerY !== 0) params.set("y", String(view.centerY));
    if (view.scale !== undefined && view.scale !== 1) params.set("z", String(view.scale));
    const url = new URL(baseURL);
    url.search = "";
    url.hash = params.toString();
    parse(url.hash);
    return url.href;
  }

  return { create, parse };
});
