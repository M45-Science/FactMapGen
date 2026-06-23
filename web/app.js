const state = {
  config: {
    presetDir: "presets",
    previewDir: "previews",
    previewEnabled: false,
  },
  profiles: [],
  selected: null,
  mapGen: null,
  mapSettings: null,
  dirty: false,
};

const defaultPreviewSize = 768;
const autoPreviewDelay = 900;

let autoPreviewTimer = null;
let autoPreviewPending = false;
let previewInFlight = false;

const resources = [
  { key: "coal", label: "Coal", color: "#2b2d2a", richness: true },
  { key: "iron-ore", label: "Iron ore", color: "#8c8f8a", richness: true },
  { key: "copper-ore", label: "Copper ore", color: "#b06433", richness: true },
  { key: "stone", label: "Stone", color: "#c6b887", richness: true },
  { key: "uranium-ore", label: "Uranium ore", color: "#79a845", richness: true },
  { key: "crude-oil", label: "Crude oil", color: "#141414", richness: true },
  { key: "water", label: "Water", color: "#4b89a6", richness: false },
  { key: "trees", label: "Trees", color: "#4f7e3e", richness: false },
];

const $ = (selector) => document.querySelector(selector);

const els = {
  createForm: $("#createForm"),
  profileName: $("#profileName"),
  presetSelect: $("#presetSelect"),
  profileSelect: $("#profileSelect"),
  statusLine: $("#statusLine"),
  duplicateBtn: $("#duplicateBtn"),
  deleteBtn: $("#deleteBtn"),
  downloadBtn: $("#downloadBtn"),
  saveBtn: $("#saveBtn"),
  previewBtn: $("#previewBtn"),
  previewSize: $("#previewSize"),
  mapgenBody: $(".mapgen-body"),
  previewPlanet: $("#previewPlanet"),
  autoRefreshPreview: $("#autoRefreshPreview"),
  seedValue: $("#seedValue"),
  seedRandom: $("#seedRandom"),
  previewImage: $("#previewImage"),
  previewEmpty: $("#previewEmpty"),
  previewStatus: $("#previewStatus"),
  mapgenSubtabs: document.querySelectorAll(".mapgen-subtab"),
  mapgenSubpanels: {
    resources: $("#resourcesSubpanel"),
    terrain: $("#terrainSubpanel"),
    enemy: $("#enemySubpanel"),
    advanced: $("#advancedSubpanel"),
  },
  worldControls: $("#worldControls"),
  terrainControls: $("#terrainControls"),
  autoplaceGrid: $("#autoplaceGrid"),
  enemyControls: $("#enemyControls"),
  simulationControls: $("#simulationControls"),
  toast: $("#toast"),
};

document.addEventListener("DOMContentLoaded", init);

async function init() {
  els.createForm.addEventListener("submit", createProfile);
  els.profileSelect.addEventListener("change", () => loadProfile(els.profileSelect.value));
  els.saveBtn.addEventListener("click", saveProfile);
  els.previewBtn.addEventListener("click", () => generatePreview());
  els.previewSize.addEventListener("change", () => {
    updatePreviewFrameSize();
    scheduleAutoPreview(0);
  });
  els.previewPlanet.addEventListener("input", () => scheduleAutoPreview());
  els.autoRefreshPreview.addEventListener("change", () => {
    if (els.autoRefreshPreview.checked) {
      scheduleAutoPreview(0);
    } else {
      cancelAutoPreview();
    }
  });
  els.downloadBtn.addEventListener("click", downloadPreset);
  els.seedValue.addEventListener("input", syncSeedFromToolbar);
  els.seedRandom.addEventListener("change", syncSeedFromToolbar);
  els.deleteBtn.addEventListener("click", deleteProfile);
  els.duplicateBtn.addEventListener("click", duplicateProfile);

  for (const tab of els.mapgenSubtabs) {
    tab.addEventListener("click", () => setMapgenTab(tab.dataset.mapgenTab));
  }

  setControlsEnabled(false);
  updatePreviewFrameSize();
  await loadConfig();
  loadProfiles(false);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const body = await response.json();
      if (body.error) message = body.error;
    } catch {
      // Keep the HTTP status text.
    }
    throw new Error(message);
  }
  if (response.status === 204) return null;
  return response.json();
}

async function loadConfig() {
  try {
    state.config = await api("/api/config");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function loadProfiles(keepSelection) {
  try {
    const body = await api("/api/profiles");
    state.profiles = (body.profiles || []).sort((a, b) => a.name.localeCompare(b.name));
    renderProfileSelect();

    const selectedStillExists = state.selected && state.profiles.some((profile) => profile.name === state.selected);
    if (keepSelection && selectedStillExists) {
      await loadProfile(state.selected);
      return;
    }
    if (selectedStillExists) {
      renderHeader();
      return;
    }
    if (state.profiles.length > 0) {
      await loadProfile(state.profiles[0].name);
      return;
    }

    state.selected = null;
    state.mapGen = null;
    state.mapSettings = null;
    renderAll();
  } catch (error) {
    showToast(error.message, true);
  }
}

async function loadProfile(name) {
  if (!name) return;
  try {
    const body = await api(`/api/profiles/${encodeURIComponent(name)}`);
    cancelAutoPreview();
    state.selected = body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    renderAll();
    scheduleAutoPreview(0);
  } catch (error) {
    showToast(error.message, true);
  }
}

async function createProfile(event) {
  event.preventDefault();
  const name = els.profileName.value.trim();
  if (!name) {
    showToast("Enter a preset name.", true);
    return;
  }

  try {
    const body = await api("/api/profiles", {
      method: "POST",
      body: JSON.stringify({ name, preset: els.presetSelect.value }),
    });
    els.profileName.value = "";
    state.selected = body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    await loadProfiles(true);
    showToast("Preset created.");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function saveProfile() {
  await saveCurrentProfile(false);
}

async function saveCurrentProfile(silent) {
  if (!state.selected || !state.mapGen || !state.mapSettings) return false;
  ensureRandomSeedDefault();

  try {
    const body = await api(`/api/profiles/${encodeURIComponent(state.selected)}`, {
      method: "PUT",
      body: JSON.stringify({
        mapGen: state.mapGen,
        mapSettings: state.mapSettings,
      }),
    });
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    renderHeader();
    if (!silent) await loadProfiles(true);
    if (!silent) showToast("Saved to server files.");
    if (!silent && els.autoRefreshPreview.checked && state.config.previewEnabled) {
      scheduleAutoPreview(0);
    }
    return true;
  } catch (error) {
    if (silent) throw error;
    showToast(error.message, true);
    return false;
  }
}

async function duplicateProfile() {
  if (!state.selected) return;
  const name = window.prompt("Duplicate preset name", `${state.selected} copy`);
  if (!name) return;

  try {
    const body = await api(`/api/profiles/${encodeURIComponent(state.selected)}/duplicate`, {
      method: "POST",
      body: JSON.stringify({ name: name.trim() }),
    });
    state.selected = body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    await loadProfiles(true);
    showToast("Preset duplicated.");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function deleteProfile() {
  if (!state.selected) return;
  if (!window.confirm(`Delete ${state.selected}?`)) return;

  try {
    await api(`/api/profiles/${encodeURIComponent(state.selected)}`, { method: "DELETE" });
    state.selected = null;
    state.mapGen = null;
    state.mapSettings = null;
    state.dirty = false;
    await loadProfiles(false);
    showToast("Preset deleted.");
  } catch (error) {
    showToast(error.message, true);
  }
}

function downloadPreset() {
  if (!state.selected) return;
  window.location.href = `/api/profiles/${encodeURIComponent(state.selected)}/download.zip`;
}

function canAutoRefreshPreview() {
  return Boolean(state.selected && state.config.previewEnabled && els.autoRefreshPreview.checked);
}

function cancelAutoPreview() {
  window.clearTimeout(autoPreviewTimer);
  autoPreviewTimer = null;
  autoPreviewPending = false;
}

function scheduleAutoPreview(delay = autoPreviewDelay) {
  if (!canAutoRefreshPreview()) return;
  if (previewInFlight) {
    autoPreviewPending = true;
    return;
  }
  window.clearTimeout(autoPreviewTimer);
  autoPreviewTimer = window.setTimeout(() => {
    autoPreviewTimer = null;
    generatePreview({ automatic: true });
  }, delay);
}

async function generatePreview(options = {}) {
  const automatic = Boolean(options.automatic);
  if (!state.selected || !state.mapGen) return false;
  if (automatic && !canAutoRefreshPreview()) return false;
  if (!state.config.previewEnabled) {
    if (!automatic) showToast("Factorio preview is not configured.", true);
    return false;
  }
  if (previewInFlight) {
    if (automatic) autoPreviewPending = true;
    return false;
  }
  previewInFlight = true;
  const previewProfile = state.selected;

  try {
    const size = currentPreviewSize();
    const planet = els.previewPlanet.value.trim() || "nauvis";
    updatePreviewFrameSize(size);
    els.previewBtn.disabled = true;
    els.previewStatus.classList.remove("error");
    els.previewStatus.textContent = "Generating preview...";

    const body = await api(`/api/profiles/${encodeURIComponent(previewProfile)}/preview`, {
      method: "POST",
      body: JSON.stringify({ size, planet, mapGen: state.mapGen }),
    });
    if (state.selected !== previewProfile) return true;
    updatePreviewFrameSize(body.size);
    showPreviewImage(body.url);
    els.previewStatus.classList.remove("error");
    els.previewStatus.textContent = "";
    if (!automatic) showToast("Preview generated.");
  } catch (error) {
    els.previewStatus.classList.add("error");
    els.previewStatus.textContent = clippedClientMessage(error.message, 1200);
    if (!automatic) showToast("Preview failed.", true);
    return false;
  } finally {
    previewInFlight = false;
    els.previewBtn.disabled = !state.selected || !state.config.previewEnabled;
    if (autoPreviewPending) {
      autoPreviewPending = false;
      scheduleAutoPreview();
    }
  }
  return true;
}

function setMapgenTab(tabName) {
  for (const tab of els.mapgenSubtabs) {
    tab.classList.toggle("active", tab.dataset.mapgenTab === tabName);
  }
  for (const [name, panel] of Object.entries(els.mapgenSubpanels)) {
    panel.classList.toggle("active", name === tabName);
  }
}

function renderAll() {
  renderProfileSelect();
  renderHeader();
  setControlsEnabled(Boolean(state.selected));
  if (state.selected) {
    updateSeedToolbar();
    renderVisualControls();
    renderPreview();
  } else {
    updateSeedToolbar();
    clearVisualControls();
    hidePreview("No preview image");
  }
}

function renderProfileSelect() {
  const selected = state.selected;
  els.profileSelect.innerHTML = "";
  if (state.profiles.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No presets";
    els.profileSelect.append(option);
    return;
  }

  for (const profile of state.profiles) {
    const option = document.createElement("option");
    option.value = profile.name;
    option.textContent = profile.name;
    option.selected = profile.name === selected;
    els.profileSelect.append(option);
  }
}

function renderHeader() {
  if (!state.selected) {
    els.statusLine.textContent = "Create a preset to start.";
    return;
  }
  els.profileSelect.value = state.selected;
  els.statusLine.textContent = state.dirty ? "Unsaved changes" : "";
}

function setControlsEnabled(enabled) {
  for (const button of [
    els.saveBtn,
    els.deleteBtn,
    els.duplicateBtn,
    els.downloadBtn,
  ]) {
    button.disabled = !enabled;
  }
  els.profileSelect.disabled = state.profiles.length === 0;
  els.seedRandom.disabled = !enabled;
  els.seedValue.disabled = !enabled || !state.mapGen || isRandomSeed(state.mapGen.seed);
  els.autoRefreshPreview.disabled = !enabled;
  els.previewBtn.disabled = !enabled || !state.config.previewEnabled;
  els.previewSize.disabled = !enabled;
  els.previewPlanet.disabled = !enabled;
}

function clearVisualControls() {
  for (const node of [
    els.worldControls,
    els.terrainControls,
    els.autoplaceGrid,
    els.enemyControls,
    els.simulationControls,
  ]) {
    node.innerHTML = "";
  }
}

function renderVisualControls() {
  clearVisualControls();
  if (!state.mapGen || !state.mapSettings) return;

  addNumberField(els.worldControls, "Width", state.mapGen, ["width"], 0, 1000000, 1);
  addNumberField(els.worldControls, "Height", state.mapGen, ["height"], 0, 1000000, 1);
  addNumberField(els.worldControls, "Starting area", state.mapGen, ["starting_area"], 0, 10, 0.1);
  updateSeedToolbar();

  addSliderField(els.terrainControls, "Cliff elevation", state.mapGen, ["cliff_settings", "cliff_elevation_0"], -100, 100, 1);
  addSliderField(els.terrainControls, "Cliff interval", state.mapGen, ["cliff_settings", "cliff_elevation_interval"], 1, 200, 1);
  addSliderField(els.terrainControls, "Cliff continuity", state.mapGen, ["cliff_settings", "richness"], 0, 10, 0.1);
  addExpressionSliderField(els.terrainControls, "Moisture frequency", state.mapGen, ["property_expression_names", "control:moisture:frequency"], 0.1, 6, 0.1);
  addExpressionSliderField(els.terrainControls, "Moisture bias", state.mapGen, ["property_expression_names", "control:moisture:bias"], -1, 1, 0.05);
  addExpressionSliderField(els.terrainControls, "Terrain frequency", state.mapGen, ["property_expression_names", "control:aux:frequency"], 0.1, 6, 0.1);
  addExpressionSliderField(els.terrainControls, "Terrain bias", state.mapGen, ["property_expression_names", "control:aux:bias"], -1, 1, 0.05);
  addTextField(els.terrainControls, "Elevation", state.mapGen, ["property_expression_names", "elevation"], "");

  renderAutoplace();

  addNoBitersField(els.enemyControls);
  addEnemyBasesField(els.enemyControls);
  addCheckboxField(els.enemyControls, "Peaceful mode", state.mapGen, ["peaceful_mode"], { rerender: true });
  addCheckboxField(els.enemyControls, "Evolution", state.mapSettings, ["enemy_evolution", "enabled"], { rerender: true });
  addNumberField(els.enemyControls, "Time factor", state.mapSettings, ["enemy_evolution", "time_factor"], 0, 1, 0.000001);
  addNumberField(els.enemyControls, "Destroy factor", state.mapSettings, ["enemy_evolution", "destroy_factor"], 0, 1, 0.0001);
  addNumberField(els.enemyControls, "Pollution factor", state.mapSettings, ["enemy_evolution", "pollution_factor"], 0, 1, 0.0000001);
  addCheckboxField(els.enemyControls, "Expansion", state.mapSettings, ["enemy_expansion", "enabled"], { rerender: true });
  addNumberField(els.enemyControls, "Min cooldown", state.mapSettings, ["enemy_expansion", "min_expansion_cooldown"], 0, 1000000, 60);
  addNumberField(els.enemyControls, "Max cooldown", state.mapSettings, ["enemy_expansion", "max_expansion_cooldown"], 0, 1000000, 60);

  addNumberField(els.simulationControls, "Technology price", state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 0.01, 100, 0.01);
  addNumberField(els.simulationControls, "Spoil time", state.mapSettings, ["difficulty_settings", "spoil_time_modifier"], 0.01, 100, 0.01);
  addCheckboxField(els.simulationControls, "Pollution", state.mapSettings, ["pollution", "enabled"]);
  addNumberField(els.simulationControls, "Diffusion ratio", state.mapSettings, ["pollution", "diffusion_ratio"], 0, 1, 0.01);
  addNumberField(els.simulationControls, "Pollution ageing", state.mapSettings, ["pollution", "ageing"], 0, 100, 0.01);
  addNumberField(els.simulationControls, "Asteroid spawning", state.mapSettings, ["asteroids", "spawning_rate"], 0, 100, 0.01);
}

function ensureRandomSeedDefault() {
  if (!state.mapGen || state.mapGen.seed === undefined || state.mapGen.seed === null) {
    state.mapGen.seed = 0;
  }
}

function updateSeedToolbar() {
  if (!state.mapGen) {
    els.seedRandom.checked = true;
    els.seedValue.value = "";
    els.seedValue.disabled = true;
    return;
  }
  const random = isRandomSeed(state.mapGen.seed);
  els.seedRandom.checked = random;
  els.seedValue.value = random ? "" : state.mapGen.seed;
  els.seedValue.disabled = !state.selected || random;
}

function syncSeedFromToolbar() {
  if (!state.mapGen) return;
  if (els.seedRandom.checked) {
    state.mapGen.seed = 0;
    els.seedValue.value = "";
    els.seedValue.disabled = true;
  } else {
    if (els.seedValue.value === "" || numericValue(els.seedValue.value) < 1) {
      els.seedValue.value = "1";
    }
    state.mapGen.seed = Math.max(1, numericValue(els.seedValue.value));
    els.seedValue.disabled = false;
  }
  afterVisualEdit();
}

function renderPreview() {
  if (!state.selected) {
    hidePreview("No preview image");
    return;
  }
  els.previewStatus.classList.remove("error");
  els.previewStatus.textContent = state.config.previewEnabled ? "" : "Preview binary unavailable.";
  updatePreviewFrameSize();
  showPreviewImage(`/api/profiles/${encodeURIComponent(state.selected)}/preview.png?ts=${Date.now()}`);
}

function currentPreviewSize() {
  const size = Number(els.previewSize.value || defaultPreviewSize);
  if (!Number.isFinite(size)) return defaultPreviewSize;
  return Math.min(4096, Math.max(256, Math.round(size)));
}

function updatePreviewFrameSize(size = currentPreviewSize()) {
  els.mapgenBody.style.setProperty("--preview-size", `${size}px`);
}

function showPreviewImage(url) {
  els.previewEmpty.style.display = "none";
  els.previewImage.style.display = "block";
  els.previewImage.onerror = () => hidePreview(state.config.previewEnabled ? "No preview image" : "Preview binary unavailable");
  els.previewImage.onload = () => {
    els.previewEmpty.style.display = "none";
    els.previewImage.style.display = "block";
  };
  els.previewImage.src = url;
}

function hidePreview(message) {
  els.previewImage.removeAttribute("src");
  els.previewImage.style.display = "none";
  els.previewEmpty.style.display = "grid";
  els.previewEmpty.textContent = message;
}

function renderAutoplace() {
  const controls = ensurePath(state.mapGen, ["autoplace_controls"], {});
  for (const resource of resources) {
    const current = ensurePath(controls, [resource.key], {});
    if (typeof current.frequency !== "number") current.frequency = Number(current.frequency ?? 1);
    if (typeof current.size !== "number") current.size = Number(current.size ?? 1);
    if (resource.richness && typeof current.richness !== "number") current.richness = Number(current.richness ?? 1);

    const row = document.createElement("div");
    row.className = "autoplace-row";

    const label = document.createElement("div");
    label.className = "resource-name";
    const swatch = document.createElement("span");
    swatch.className = "swatch";
    swatch.style.background = resource.color;
    const text = document.createElement("span");
    text.textContent = resource.label;
    label.append(swatch, text);

    const sliders = document.createElement("div");
    sliders.className = "resource-controls";
    addSliderField(sliders, "Frequency", current, ["frequency"], 0, Math.max(6, Number(current.frequency) || 0), 0.1);
    addSliderField(sliders, "Size", current, ["size"], 0, Math.max(6, Number(current.size) || 0), 0.1);
    if (resource.richness) {
      addSliderField(sliders, "Richness", current, ["richness"], 0, Math.max(10, Number(current.richness) || 0), 0.1);
    }

    row.append(label, sliders);
    els.autoplaceGrid.append(row);
  }
}

function addNoBitersField(parent) {
  const wrap = document.createElement("label");
  wrap.className = "check-field";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = noBitersEnabled();
  const text = document.createElement("span");
  text.textContent = "No biters";
  input.addEventListener("change", () => {
    applyNoBiters(input.checked);
    afterVisualEdit();
    renderVisualControls();
  });
  wrap.append(input, text);
  parent.append(wrap);
}

function addEnemyBasesField(parent) {
  const wrap = document.createElement("label");
  wrap.className = "check-field";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = enemyBasesEnabled();
  const text = document.createElement("span");
  text.textContent = "Enemy bases";
  input.addEventListener("change", () => {
    setEnemyBasesEnabled(input.checked);
    afterVisualEdit();
    renderVisualControls();
  });
  wrap.append(input, text);
  parent.append(wrap);
}

function noBitersEnabled() {
  return (
    getPath(state.mapGen, ["peaceful_mode"], false) === true &&
    !enemyBasesEnabled() &&
    getPath(state.mapSettings, ["enemy_evolution", "enabled"], true) === false &&
    getPath(state.mapSettings, ["enemy_expansion", "enabled"], true) === false
  );
}

function applyNoBiters(enabled) {
  if (enabled) {
    setEnemyBasesEnabled(false);
    state.mapGen.peaceful_mode = true;
    setPath(state.mapSettings, ["enemy_evolution", "enabled"], false);
    setPath(state.mapSettings, ["enemy_expansion", "enabled"], false);
    return;
  }

  setEnemyBasesEnabled(true);
  state.mapGen.peaceful_mode = false;
  setPath(state.mapSettings, ["enemy_evolution", "enabled"], true);
  setPath(state.mapSettings, ["enemy_expansion", "enabled"], true);
}

function enemyBasesEnabled() {
  const base = getPath(state.mapGen, ["autoplace_controls", "enemy-base"], null);
  if (!base || typeof base !== "object") return true;
  return numericValue(base.frequency ?? 1) > 0 || numericValue(base.size ?? 1) > 0;
}

function setEnemyBasesEnabled(enabled) {
  const base = ensurePath(state.mapGen, ["autoplace_controls", "enemy-base"], {});
  if (enabled) {
    if (numericValue(base.frequency ?? 0) <= 0 && numericValue(base.size ?? 0) <= 0) {
      base.frequency = 1;
      base.size = 1;
    }
    return;
  }

  base.frequency = 0;
  base.size = 0;
  if ("richness" in base) base.richness = 0;
}

function addNumberField(parent, labelText, root, path, min, max, step) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const label = document.createElement("label");
  label.textContent = labelText;
  const input = document.createElement("input");
  input.type = "number";
  input.min = String(min);
  input.max = String(max);
  input.step = String(step);
  input.value = getPath(root, path, 0);
  input.addEventListener("input", () => {
    setPath(root, path, numericValue(input.value));
    afterVisualEdit();
  });
  wrap.append(label, input);
  parent.append(wrap);
}

function addTextField(parent, labelText, root, path, fallback = "0") {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const label = document.createElement("label");
  label.textContent = labelText;
  const input = document.createElement("input");
  input.type = "text";
  input.value = getPath(root, path, fallback) ?? "";
  input.addEventListener("input", () => {
    setPath(root, path, input.value);
    afterVisualEdit();
  });
  wrap.append(label, input);
  parent.append(wrap);
}

function addCheckboxField(parent, labelText, root, path, options = {}) {
  const wrap = document.createElement("label");
  wrap.className = "check-field";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = Boolean(getPath(root, path, false));
  const text = document.createElement("span");
  text.textContent = labelText;
  input.addEventListener("change", () => {
    setPath(root, path, input.checked);
    afterVisualEdit();
    if (options.rerender) renderVisualControls();
  });
  wrap.append(input, text);
  parent.append(wrap);
}

function isRandomSeed(seed) {
  return seed === null || seed === undefined || Number(seed) === 0;
}

function addSliderField(parent, labelText, root, path, min, max, step) {
  const value = Number(getPath(root, path, 1));
  const wrap = document.createElement("div");
  wrap.className = "slider-field";

  const label = document.createElement("label");
  label.textContent = labelText;
  const slider = document.createElement("input");
  slider.type = "range";
  slider.min = String(min);
  slider.max = String(max);
  slider.step = String(step);
  slider.value = String(value);
  const number = document.createElement("input");
  number.type = "number";
  number.min = String(min);
  number.max = String(max);
  number.step = String(step);
  number.value = String(value);

  const sync = (source) => {
    const next = numericValue(source.value);
    slider.value = String(next);
    number.value = String(next);
    setPath(root, path, next);
    afterVisualEdit();
  };

  slider.addEventListener("input", () => sync(slider));
  number.addEventListener("input", () => sync(number));
  wrap.append(label, slider, number);
  parent.append(wrap);
}

function addExpressionSliderField(parent, labelText, root, path, min, max, step) {
  const rawValue = getPath(root, path, "0");
  const value = Number(rawValue);
  const fallback = Number.isFinite(value) ? value : 0;
  const wrap = document.createElement("div");
  wrap.className = "slider-field";

  const label = document.createElement("label");
  label.textContent = labelText;
  const slider = document.createElement("input");
  slider.type = "range";
  slider.min = String(min);
  slider.max = String(max);
  slider.step = String(step);
  slider.value = String(fallback);
  const number = document.createElement("input");
  number.type = "number";
  number.min = String(min);
  number.max = String(max);
  number.step = String(step);
  number.value = String(fallback);

  const sync = (source) => {
    const next = numericValue(source.value);
    slider.value = String(next);
    number.value = String(next);
    setPath(root, path, String(next));
    afterVisualEdit();
  };

  slider.addEventListener("input", () => sync(slider));
  number.addEventListener("input", () => sync(number));
  wrap.append(label, slider, number);
  parent.append(wrap);
}

function afterVisualEdit() {
  state.dirty = true;
  renderHeader();
  scheduleAutoPreview();
}

function numericValue(value) {
  if (value === "" || value === null || value === undefined) return 0;
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}

function getPath(root, path, fallback) {
  let current = root;
  for (const key of path) {
    if (!current || typeof current !== "object" || !(key in current)) {
      return fallback;
    }
    current = current[key];
  }
  return current;
}

function setPath(root, path, value) {
  let current = root;
  for (const key of path.slice(0, -1)) {
    if (!current[key] || typeof current[key] !== "object" || Array.isArray(current[key])) {
      current[key] = {};
    }
    current = current[key];
  }
  current[path[path.length - 1]] = value;
}

function ensurePath(root, path, fallback) {
  let current = root;
  for (const key of path) {
    if (!current[key] || typeof current[key] !== "object" || Array.isArray(current[key])) {
      current[key] = structuredClone(fallback);
    }
    current = current[key];
  }
  return current;
}

function clippedClientMessage(message, limit) {
  const text = String(message || "").trim();
  if (text.length <= limit) return text;
  return `${text.slice(0, limit)}\n... message clipped ...`;
}

let toastTimer = null;
function showToast(message, error = false) {
  window.clearTimeout(toastTimer);
  els.toast.textContent = message;
  els.toast.classList.toggle("error", error);
  els.toast.classList.add("show");
  toastTimer = window.setTimeout(() => {
    els.toast.classList.remove("show");
  }, 3200);
}
