const state = {
  config: {
    presetDir: "presets",
    previewEnabled: false,
    fastPreviewEnabled: true,
    factorio: {
      previewEnabled: false,
      installManaged: false,
      updateAvailable: false,
    },
  },
  profiles: [],
  localProfile: null,
  selected: null,
  mapGen: null,
  mapSettings: null,
  previewSeeds: {},
  previewViews: {},
  dirty: false,
  session: null,
};

const defaultPreviewSize = 768;
const minPreviewSize = 256;
const maxPreviewSizePx = 4096;
const guestMaxPreviewSize = 512;
const minPreviewScale = 1 / 64;
const maxPreviewScale = 64;
const previewWheelDebounceMs = 180;
const safePreviewSizes = [256, 512, 768, 1024, 1536, 2048, 3072, 4096];
const maxPreviewSeed = 4294967295;
const minAutoplaceFrequency = 0.1;
const maxProfileNameLength = 64;

let autoPreviewTimer = null;
let autoPreviewPending = false;
let previewInFlight = false;
let previewImageLoadID = 0;
let previewRequestRevision = 0;
let previewWheelTimer = null;
let previewPanGesture = null;
let displayedPreview = null;
let previewPointerView = null;
let factorioUpdateNoticeShown = false;

const resources = [
  { key: "coal", label: "Coal", color: "#2b2d2a", richness: true },
  { key: "iron-ore", label: "Iron ore", color: "#8c8f8a", richness: true },
  { key: "copper-ore", label: "Copper ore", color: "#b06433", richness: true },
  { key: "stone", label: "Stone", color: "#c6b887", richness: true },
  { key: "uranium-ore", label: "Uranium ore", color: "#79a845", richness: true },
  { key: "crude-oil", label: "Crude oil", color: "#141414", richness: true },
  { key: "water", label: "Water", color: "#4b89a6", richness: false, tooltip: "Water autoplace control. Frequency and size change water coverage; in Factorio, size mainly affects the starting area." },
  { key: "trees", label: "Trees", color: "#4f7e3e", richness: false },
  { key: "rocks", label: "Rocks", color: "#837d70", richness: false },
  { key: "starting_area_moisture", label: "Starting area moisture", color: "#6da6a8", richness: false, tooltip: "Moisture around the starting area, which influences local terrain and vegetation." },
  { key: "nauvis_cliff", label: "Cliffs", color: "#9a8c78", richness: false, tooltip: "Nauvis cliff autoplace control. This is separate from cliff frequency and continuity settings." },
];

const resourceKeys = ["coal", "iron-ore", "copper-ore", "stone", "uranium-ore", "crude-oil"];
const knownPlanets = ["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"];
const spaceAgeControlGroups = [
  {
    key: "nauvis",
    label: "Nauvis",
    controls: [],
  },
  {
    key: "vulcanus",
    label: "Vulcanus",
    controls: [
      { key: "vulcanus_coal", label: "Vulcanus coal", color: "#2b2d2a", richness: true },
      { key: "tungsten_ore", label: "Tungsten ore", color: "#5b6f78", richness: true },
      { key: "calcite", label: "Calcite", color: "#ddd6bd", richness: true },
      { key: "sulfuric_acid_geyser", label: "Sulfuric acid geysers", color: "#d6c34a", richness: true },
      { key: "vulcanus_volcanism", label: "Volcanism", color: "#d65832", richness: false, tooltip: "Space Age Vulcanus terrain control for lava and volcanic terrain. Factorio marks this as not disableable." },
    ],
  },
  {
    key: "gleba",
    label: "Gleba",
    controls: [
      { key: "gleba_stone", label: "Gleba stone", color: "#c6b887", richness: true },
      { key: "gleba_water", label: "Gleba water", color: "#3f8e7a", richness: false, tooltip: "Space Age Gleba water and wetland terrain control. Factorio marks this as not disableable." },
      { key: "gleba_plants", label: "Gleba plants", color: "#6f9f38", richness: false, tooltip: "Space Age Gleba plant coverage control. Factorio marks this as not disableable." },
      { key: "gleba_cliff", label: "Gleba cliffs", color: "#8c7c63", richness: false },
      { key: "gleba_enemy_base", label: "Pentapod nests", color: "#a45a71", richness: false, tooltip: "Space Age Gleba enemy-base control for pentapod nests. Factorio marks this as not disableable." },
    ],
  },
  {
    key: "fulgora",
    label: "Fulgora",
    controls: [
      { key: "scrap", label: "Scrap", color: "#9a8f7a", richness: true },
      { key: "fulgora_islands", label: "Fulgora islands", color: "#c4a56a", richness: false, tooltip: "Space Age Fulgora island terrain control. Factorio marks this as not disableable." },
      { key: "fulgora_cliff", label: "Fulgora cliffs", color: "#7f756a", richness: false },
    ],
  },
  {
    key: "aquilo",
    label: "Aquilo",
    controls: [
      { key: "aquilo_crude_oil", label: "Aquilo crude oil", color: "#141414", richness: true },
      { key: "lithium_brine", label: "Lithium brine", color: "#b8d7d0", richness: true },
      { key: "fluorine_vent", label: "Fluorine vents", color: "#a7d86f", richness: true },
    ],
  },
];
const planetMapGenDefaults = {
  nauvis: {
    top_level: {
      aux_climate_control: true,
      moisture_climate_control: true,
    },
    property_expression_names: {},
    cliff_settings: {
      name: "cliff",
      control: "nauvis_cliff",
      cliff_smoothing: 0,
    },
    autoplace_controls: ["iron-ore", "copper-ore", "stone", "coal", "uranium-ore", "crude-oil", "water", "trees", "enemy-base", "rocks", "starting_area_moisture", "nauvis_cliff"],
    autoplace_settings: {
      tile: ["grass-1", "grass-2", "grass-3", "grass-4", "dry-dirt", "dirt-1", "dirt-2", "dirt-3", "dirt-4", "dirt-5", "dirt-6", "dirt-7", "sand-1", "sand-2", "sand-3", "red-desert-0", "red-desert-1", "red-desert-2", "red-desert-3", "water", "deepwater"],
      decorative: ["brown-hairy-grass", "green-hairy-grass", "brown-carpet-grass", "green-carpet-grass", "green-small-grass", "green-asterisk", "brown-asterisk-mini", "green-asterisk-mini", "brown-asterisk", "red-asterisk", "dark-mud-decal", "light-mud-decal", "cracked-mud-decal", "red-desert-decal", "sand-decal", "sand-dune-decal", "green-pita", "red-pita", "green-croton", "red-croton", "green-pita-mini", "brown-fluff", "brown-fluff-dry", "green-desert-bush", "red-desert-bush", "white-desert-bush", "garballo-mini-dry", "garballo", "green-bush-mini", "medium-rock", "small-rock", "tiny-rock", "medium-sand-rock", "small-sand-rock"],
      entity: ["iron-ore", "copper-ore", "stone", "coal", "crude-oil", "uranium-ore", "fish", "big-sand-rock", "huge-rock", "big-rock"],
    },
  },
  vulcanus: {
    property_expression_names: {
      elevation: "vulcanus_elevation",
      temperature: "vulcanus_temperature",
      moisture: "vulcanus_moisture",
      aux: "vulcanus_aux",
      cliffiness: "cliffiness_basic",
      cliff_elevation: "cliff_elevation_from_elevation",
      "entity:tungsten-ore:probability": "vulcanus_tungsten_ore_probability",
      "entity:tungsten-ore:richness": "vulcanus_tungsten_ore_richness",
      "entity:coal:probability": "vulcanus_coal_probability",
      "entity:coal:richness": "vulcanus_coal_richness",
      "entity:calcite:probability": "vulcanus_calcite_probability",
      "entity:calcite:richness": "vulcanus_calcite_richness",
      "entity:sulfuric-acid-geyser:probability": "vulcanus_sulfuric_acid_geyser_probability",
      "entity:sulfuric-acid-geyser:richness": "vulcanus_sulfuric_acid_geyser_richness",
    },
    cliff_settings: {
      name: "cliff-vulcanus",
      cliff_elevation_interval: 120,
      cliff_elevation_0: 70,
    },
    territory_settings: {
      units: ["small-demolisher", "medium-demolisher", "big-demolisher"],
      territory_index_expression: "demolisher_territory_expression",
      territory_variation_expression: "demolisher_variation_expression",
      minimum_territory_size: 10,
    },
    autoplace_controls: ["vulcanus_coal", "sulfuric_acid_geyser", "tungsten_ore", "calcite", "vulcanus_volcanism"],
    autoplace_settings: {
      tile: ["volcanic-soil-dark", "volcanic-soil-light", "volcanic-ash-soil", "volcanic-ash-flats", "volcanic-ash-light", "volcanic-ash-dark", "volcanic-cracks", "volcanic-cracks-warm", "volcanic-folds", "volcanic-folds-flat", "lava", "lava-hot", "volcanic-folds-warm", "volcanic-pumice-stones", "volcanic-cracks-hot", "volcanic-jagged-ground", "volcanic-smooth-stone", "volcanic-smooth-stone-warm", "volcanic-ash-cracks"],
      decorative: ["v-brown-carpet-grass", "v-green-hairy-grass", "v-brown-hairy-grass", "v-red-pita", "vulcanus-rock-decal-large", "vulcanus-crack-decal-large", "vulcanus-crack-decal-huge-warm", "vulcanus-dune-decal", "vulcanus-sand-decal", "vulcanus-lava-fire", "calcite-stain", "calcite-stain-small", "sulfur-stain", "sulfur-stain-small", "sulfuric-acid-puddle", "sulfuric-acid-puddle-small", "crater-small", "crater-large", "pumice-relief-decal", "small-volcanic-rock", "medium-volcanic-rock", "tiny-volcanic-rock", "tiny-rock-cluster", "small-sulfur-rock", "tiny-sulfur-rock", "sulfur-rock-cluster", "waves-decal"],
      entity: ["coal", "calcite", "sulfuric-acid-geyser", "tungsten-ore", "huge-volcanic-rock", "big-volcanic-rock", "crater-cliff", "vulcanus-chimney", "vulcanus-chimney-faded", "vulcanus-chimney-cold", "vulcanus-chimney-short", "vulcanus-chimney-truncated", "ashland-lichen-tree", "ashland-lichen-tree-flaming"],
    },
  },
  gleba: {
    property_expression_names: {
      elevation: "gleba_elevation",
      aux: "gleba_aux",
      moisture: "gleba_moisture",
      temperature: "gleba_temperature",
      cliffiness: "gleba_cliffiness",
      cliff_elevation: "cliff_elevation_from_elevation",
      enemy_base_radius: "gleba_enemy_base_radius",
      enemy_base_frequency: "gleba_enemy_base_frequency",
      "entity:stone:richness": "gleba_stone_richness",
      "entity:stone:probability": "gleba_stone_probability",
      "decorative:red-desert-bush:probability": "gleba_red_desert_bush_probability",
      "decorative:white-desert-bush:probability": "gleba_white_desert_bush_probability",
      "decorative:red-pita:probability": "gleba_red_pita_probability",
      "decorative:green-bush-mini:probability": "gleba_green_bush_probability",
      "decorative:green-croton:probability": "gleba_green_cronton_probability",
      "decorative:green-pita:probability": "gleba_green_pita_probability",
      "decorative:green-pita-mini:probability": "gleba_green_pita_mini_probability",
      "decorative:lichen-decal:probability": "gleba_orange_lichen_probability",
      "decorative:shroom-decal:probability": "gleba_carpet_shroom_probability",
      "decorative:cracked-mud-decal:probability": "gleba_cracked_mud_probability",
      "decorative:light-mud-decal:probability": "gleba_light_mud_probability",
      "decorative:dark-mud-decal:probability": "gleba_dark_mud_probability",
      "decorative:green-carpet-grass:probability": "gleba_green_carpet_grass_probability",
      "decorative:green-hairy-grass:probability": "gleba_green_hairy_grass_probability",
    },
    cliff_settings: {
      name: "cliff-gleba",
      control: "gleba_cliff",
      cliff_elevation_0: 40,
      cliff_elevation_interval: 60,
      richness: 0.8,
      cliff_smoothing: 0,
    },
    autoplace_controls: ["gleba_stone", "gleba_plants", "gleba_enemy_base", "gleba_water", "gleba_cliff"],
    autoplace_settings: {
      tile: ["natural-yumako-soil", "natural-jellynut-soil", "wetland-yumako", "wetland-jellynut", "wetland-blue-slime", "wetland-light-green-slime", "wetland-green-slime", "wetland-light-dead-skin", "wetland-dead-skin", "wetland-pink-tentacle", "wetland-red-tentacle", "gleba-deep-lake", "lowland-brown-blubber", "lowland-olive-blubber", "lowland-olive-blubber-2", "lowland-olive-blubber-3", "lowland-pale-green", "lowland-cream-cauliflower", "lowland-cream-cauliflower-2", "lowland-dead-skin", "lowland-dead-skin-2", "lowland-cream-red", "lowland-red-vein", "lowland-red-vein-2", "lowland-red-vein-3", "lowland-red-vein-4", "lowland-red-vein-dead", "lowland-red-infection", "midland-turquoise-bark", "midland-turquoise-bark-2", "midland-cracked-lichen", "midland-cracked-lichen-dull", "midland-cracked-lichen-dark", "midland-yellow-crust", "midland-yellow-crust-2", "midland-yellow-crust-3", "midland-yellow-crust-4", "highland-dark-rock", "highland-dark-rock-2", "highland-yellow-rock", "pit-rock"],
      decorative: ["yellow-lettuce-lichen-1x1", "yellow-lettuce-lichen-3x3", "yellow-lettuce-lichen-6x6", "yellow-lettuce-lichen-cups-1x1", "yellow-lettuce-lichen-cups-3x3", "yellow-lettuce-lichen-cups-6x6", "green-lettuce-lichen-1x1", "green-lettuce-lichen-3x3", "green-lettuce-lichen-6x6", "green-lettuce-lichen-water-1x1", "green-lettuce-lichen-water-3x3", "green-lettuce-lichen-water-6x6", "honeycomb-fungus", "honeycomb-fungus-1x1", "honeycomb-fungus-decayed", "split-gill-1x1", "split-gill-2x2", "split-gill-dying-1x1", "split-gill-dying-2x2", "split-gill-red-1x1", "split-gill-red-2x2", "veins", "veins-small", "mycelium", "coral-water", "coral-land", "black-sceptre", "pink-phalanges", "pink-lichen-decal", "red-lichen-decal", "green-cup", "brown-cup", "blood-grape", "blood-grape-vibrant", "brambles", "polycephalum-slime", "polycephalum-balloon", "fuchsia-pita", "wispy-lichen", "grey-cracked-mud-decal", "barnacles-decal", "coral-stunted", "coral-stunted-grey", "red-nerve-roots-veins-dense", "red-nerve-roots-veins-sparse", "purple-nerve-roots-veins-dense", "purple-nerve-roots-veins-sparse", "cream-nerve-roots-veins-dense", "cream-nerve-roots-veins-sparse", "yellow-coral", "solo-barnacle", "curly-roots-orange", "knobbly-roots", "knobbly-roots-orange", "matches-small", "pale-lettuce-lichen-cups-1x1", "pale-lettuce-lichen-cups-3x3", "pale-lettuce-lichen-cups-6x6", "pale-lettuce-lichen-1x1", "pale-lettuce-lichen-3x3", "pale-lettuce-lichen-6x6", "pale-lettuce-lichen-water-1x1", "pale-lettuce-lichen-water-3x3", "pale-lettuce-lichen-water-6x6", "white-carpet-grass", "green-carpet-grass", "green-hairy-grass", "light-mud-decal", "dark-mud-decal", "cracked-mud-decal", "red-desert-bush", "white-desert-bush", "red-pita", "green-bush-mini", "green-croton", "green-pita", "green-pita-mini", "lichen-decal", "shroom-decal"],
      entity: ["stone", "iron-stromatolite", "copper-stromatolite"],
    },
  },
  fulgora: {
    property_expression_names: {
      elevation: "fulgora_elevation",
      temperature: "temperature_basic",
      moisture: "moisture_basic",
      aux: "aux_basic",
      cliffiness: "fulgora_cliffiness",
      cliff_elevation: "cliff_elevation_from_elevation",
    },
    cliff_settings: {
      name: "cliff-fulgora",
      control: "fulgora_cliff",
      cliff_elevation_0: 80,
      cliff_elevation_interval: 40,
      cliff_smoothing: 0,
      richness: 0.95,
    },
    autoplace_controls: ["scrap", "fulgora_islands", "fulgora_cliff"],
    autoplace_settings: {
      tile: ["oil-ocean-shallow", "oil-ocean-deep", "fulgoran-rock", "fulgoran-dust", "fulgoran-sand", "fulgoran-dunes", "fulgoran-walls", "fulgoran-paving", "fulgoran-conduit", "fulgoran-machinery"],
      decorative: ["fulgoran-ruin-tiny", "fulgoran-gravewort", "urchin-cactus", "medium-fulgora-rock", "small-fulgora-rock", "tiny-fulgora-rock"],
      entity: ["scrap", "fulgoran-ruin-vault", "fulgoran-ruin-attractor", "fulgoran-ruin-colossal", "fulgoran-ruin-huge", "fulgoran-ruin-big", "fulgoran-ruin-stonehenge", "fulgoran-ruin-medium", "fulgoran-ruin-small", "fulgurite", "big-fulgora-rock"],
    },
  },
  aquilo: {
    property_expression_names: {
      elevation: "aquilo_elevation",
      temperature: "aquilo_temperature",
      moisture: "moisture_basic",
      aux: "aquilo_aux",
      cliffiness: "cliffiness_basic",
      cliff_elevation: "cliff_elevation_from_elevation",
      "entity:crude-oil:probability": "aquilo_crude_oil_probability",
      "entity:crude-oil:richness": "aquilo_crude_oil_richness",
    },
    autoplace_controls: ["lithium_brine", "fluorine_vent", "aquilo_crude_oil"],
    autoplace_settings: {
      tile: ["snow-flat", "snow-crests", "snow-lumpy", "snow-patchy", "ice-rough", "ice-smooth", "brash-ice", "ammoniacal-ocean", "ammoniacal-ocean-2"],
      decorative: ["lithium-iceberg-medium", "lithium-iceberg-small", "lithium-iceberg-tiny", "floating-iceberg-large", "floating-iceberg-small", "aqulio-ice-decal-blue", "aqulio-snowy-decal", "snow-drift-decal"],
      entity: ["crude-oil", "lithium-brine", "fluorine-vent", "lithium-iceberg-huge", "lithium-iceberg-big"],
    },
  },
};
const factorioScaleValues = {
  none: 0,
  "very-low": 0.25,
  "very-small": 0.25,
  low: 0.5,
  small: 0.5,
  normal: 1,
  regular: 1,
  high: 2,
  big: 2,
  good: 2,
  "very-high": 3,
  "very-big": 3,
  "very-good": 3,
};

const bundledPresetProfiles = {
  default: "Default",
  "no-biters": "No-Biters",
  "rail-world": "Railworld",
  "death-world": "Deathworld",
  "rich-resources": "Rich-Resources",
  marathon: "Marathon",
  "death-world-marathon": "Deathworld-Marathon",
  "peaceful-rich": "Rich-Peaceful",
  lakes: "Lakes",
  island: "Island",
  "ribbon-world": "Ribbon-World",
  "empty-sandbox": "Empty-Sandbox",
  "marathon-frontier": "Marathon-Frontier",
  "dense-forest": "Dense-Forest",
  "desert-scarcity": "Desert-Scarcity",
  "cliffside-lakes": "Cliffside-Lakes",
  "oil-baron": "Oil-Baron",
  "tiny-death-spiral": "Tiny-Death-Spiral",
  "megabase-plain": "Megabase-Plain",
  waterworld: "Waterworld",
  "forest-deathworld": "Forest-Deathworld",
  "ore-patchwork": "Ore-Patchwork",
  archipelago: "Archipelago",
  "fragmented-coast": "Fragmented-Coast",
  "hive-expansion": "Hive-Expansion",
  "sparse-rich-desert": "Sparse-Rich-Desert",
  "island-escape": "Island-Escape",
};

const controlTooltips = {
  "World size": "Width and height in tiles. Leave a dimension unset for an infinite map; Ribbon keeps height at 128 tiles.",
  "Starting point": "Initial spawn point in map coordinates. Most presets keep this at 0,0.",
  "Starting area": "Multiplier for the biter-free starting area radius. Larger values give a safer spawn.",
  "Missing autoplace controls": "When enabled, Factorio treats autoplace controls not listed in this file as enabled at their prototype defaults.",
  "Built-in resources": "Applies resource portions of Factorio's bundled map generation presets without changing unrelated settings.",
  "Space Age controls": "Space Age-only autoplace controls are separated because base Factorio installs may not recognize these prototype names.",
  "Built-in terrain": "Applies terrain portions of Factorio's bundled map generation presets without changing unrelated settings.",
  Elevation: "Chooses the base elevation expression. Lakes and Island match Factorio's built-in preset map types.",
  "Cliff prototype": "Name of the cliff prototype to generate. Nauvis uses cliff; Space Age planets use planet-specific cliff prototypes.",
  "Cliff control": "Autoplace control that drives cliff placement. Nauvis uses nauvis_cliff; Space Age planets use planet-specific controls.",
  "Cliff base elevation": "Elevation of the first cliff row. The default Nauvis value is 10.",
  "Cliff interval": "Elevation gap between cliff rows. Lower interval values create more frequent cliff lines.",
  "Cliff continuity": "How connected cliff rows are. 0 disables cliffs; higher values make longer, denser cliff lines.",
  "Cliff smoothing": "Extra coast-following cliff smoothing used by Factorio's Lakes and Island presets.",
  "Moisture frequency": "Lower values create broader wet and dry regions; higher values create busier patches.",
  "Moisture bias": "Biases terrain moisture toward drier or wetter regions.",
  "Terrain frequency": "Lower values create broader sand and grass regions; higher values create busier patches.",
  "Terrain bias": "Biases terrain type toward sandy or greener terrain.",
  "No biters": "Turns off enemy bases, evolution, and expansion while enabling peaceful mode.",
  "Enemy bases": "Controls whether enemy-base autoplace is active.",
  "No enemies mode": "Disables enemy spawning at map generation time in Factorio 2.x exchange strings.",
  "Enemy base frequency": "Controls how often enemy bases are placed. Set to 0 to disable enemy bases.",
  "Enemy base size": "Controls enemy base patch size. Set to 0 to disable enemy bases.",
  "Built-in enemy": "Applies enemy-related portions of Factorio's Death world, Death world marathon, and Rail world presets.",
  "Peaceful mode": "Enemies do not attack first, but bases may still exist unless enemy bases are disabled.",
  "Evolution profile": "Sets the three evolution factors together.",
  "Expansion distance": "Maximum distance in chunks from existing enemy bases for new expansion candidates.",
  "Expansion cooldown": "Minimum time before enemy expansion attempts. Saved as ticks; shown here as minutes.",
  "Built-in advanced": "Applies advanced portions of Factorio's Marathon and Death world presets.",
  "Science/research cost": "Multiplier for technology science pack costs. Marathon uses 4x.",
  "Spoil time": "Multiplier for Space Age item spoilage timers.",
  "Pollution profile": "Sets pollution spread, absorption, and attack cost together.",
  "Pollution spread": "Percentage of pollution diffused to neighboring chunks each second.",
  "Attack pollution cost": "Pollution consumed to form attacks; lower values make pollution-triggered attacks cheaper.",
  "Path goal pressure": "Path finder pressure toward the target. Higher values favor more direct routes.",
  "Asteroid spawning": "Space Age asteroid spawning rate multiplier.",
};

const $ = (selector) => document.querySelector(selector);

const els = {
  createForm: $("#createForm"),
  createPresetDialog: $("#createPresetDialog"),
  openCreateBtn: $("#openCreateBtn"),
  closeCreateBtn: $("#closeCreateBtn"),
  cancelCreateBtn: $("#cancelCreateBtn"),
  profileName: $("#profileName"),
  presetSelect: $("#presetSelect"),
  profileSelect: $("#profileSelect"),
  statusLine: $("#statusLine"),
  factorioVersion: $("#factorioVersion"),
  renameBtn: $("#renameBtn"),
  duplicateBtn: $("#duplicateBtn"),
  deleteBtn: $("#deleteBtn"),
  downloadBtn: $("#downloadBtn"),
  downloadZipBtn: $("#downloadZipBtn"),
  saveBtn: $("#saveBtn"),
  openImportBtn: $("#openImportBtn"),
  importPresetDialog: $("#importPresetDialog"),
  importForm: $("#importForm"),
  closeImportBtn: $("#closeImportBtn"),
  cancelImportBtn: $("#cancelImportBtn"),
  importProfileName: $("#importProfileName"),
  exchangeStringInput: $("#exchangeStringInput"),
  exportStringDialog: $("#exportStringDialog"),
  closeExportStringBtn: $("#closeExportStringBtn"),
  copyExportStringBtn: $("#copyExportStringBtn"),
  exportStringOutput: $("#exportStringOutput"),
  renamePresetDialog: $("#renamePresetDialog"),
  renameForm: $("#renameForm"),
  closeRenameBtn: $("#closeRenameBtn"),
  cancelRenameBtn: $("#cancelRenameBtn"),
  renameProfileName: $("#renameProfileName"),
  previewBtn: $("#previewBtn"),
  previewEngine: $("#previewEngine"),
  previewSize: $("#previewSize"),
  mapgenBody: $(".mapgen-body"),
  previewPlanet: $("#previewPlanet"),
  previewZoom: $("#previewZoom"),
  previewScaleField: $("#previewScaleField"),
  previewResetView: $("#previewResetView"),
  previewLossless: $("#previewLossless"),
  previewLosslessField: $("#previewLosslessField"),
  autoRefreshPreview: $("#autoRefreshPreview"),
  seedValue: $("#seedValue"),
  seedRandom: $("#seedRandom"),
  seedRerollBtn: $("#seedRerollBtn"),
  previewFrame: $(".preview-frame"),
  previewImage: $("#previewImage"),
  previewEmpty: $("#previewEmpty"),
  previewStatus: $("#previewStatus"),
  mapgenSubtabs: document.querySelectorAll(".mapgen-subtab"),
  mapgenSubpanels: {
    resources: $("#resourcesSubpanel"),
    terrain: $("#terrainSubpanel"),
    enemy: $("#enemySubpanel"),
    spaceage: $("#spaceageSubpanel"),
    advanced: $("#advancedSubpanel"),
  },
  worldControls: $("#worldControls"),
  terrainControls: $("#terrainControls"),
  autoplaceGrid: $("#autoplaceGrid"),
  enemyControls: $("#enemyControls"),
  spaceAgeControls: $("#spaceAgeControls"),
  simulationControls: $("#simulationControls"),
  loginDialog: $("#loginDialog"),
  loginForm: $("#loginForm"),
  loginUsername: $("#loginUsername"),
  loginPassword: $("#loginPassword"),
  loginError: $("#loginError"),
  guestBtn: $("#guestBtn"),
  currentUser: $("#currentUser"),
  loginBtn: $("#loginBtn"),
  adminBtn: $("#adminBtn"),
  passwordBtn: $("#passwordBtn"),
  logoutBtn: $("#logoutBtn"),
  passwordDialog: $("#passwordDialog"),
  passwordForm: $("#passwordForm"),
  closePasswordBtn: $("#closePasswordBtn"),
  cancelPasswordBtn: $("#cancelPasswordBtn"),
  currentPassword: $("#currentPassword"),
  newSelfPassword: $("#newSelfPassword"),
  confirmSelfPassword: $("#confirmSelfPassword"),
  passwordError: $("#passwordError"),
  adminPanel: $("#adminPanel"),
  closeAdminBtn: $("#closeAdminBtn"),
  userCreateForm: $("#userCreateForm"),
  newUsername: $("#newUsername"),
  newPassword: $("#newPassword"),
  newIsAdmin: $("#newIsAdmin"),
  usersList: $("#usersList"),
  auditList: $("#auditList"),
  factorioAdminStatus: $("#factorioAdminStatus"),
  refreshFactorioBtn: $("#refreshFactorioBtn"),
  installFactorioBtn: $("#installFactorioBtn"),
  toast: $("#toast"),
  toastMessage: $("#toastMessage"),
  toastClose: $("#toastClose"),
};

document.addEventListener("DOMContentLoaded", init);

async function init() {
  els.loginForm.addEventListener("submit", login);
  els.guestBtn.addEventListener("click", continueAsGuest);
  els.loginBtn.addEventListener("click", showLogin);
  els.logoutBtn.addEventListener("click", logout);
  els.passwordBtn.addEventListener("click", openPasswordDialog);
  els.adminBtn.addEventListener("click", openAdminPanel);
  els.closeAdminBtn.addEventListener("click", closeAdminPanel);
  els.closePasswordBtn.addEventListener("click", closePasswordDialog);
  els.cancelPasswordBtn.addEventListener("click", closePasswordDialog);
  els.passwordForm.addEventListener("submit", changePassword);
  els.passwordDialog.addEventListener("click", (event) => {
    if (event.target === els.passwordDialog) closePasswordDialog();
  });
  els.userCreateForm.addEventListener("submit", createUser);
  els.toastClose.addEventListener("click", hideToast);
  els.refreshFactorioBtn.addEventListener("click", () => refreshFactorioStatus({ toast: true }));
  els.installFactorioBtn.addEventListener("click", installFactorio);
  els.openCreateBtn.addEventListener("click", openCreateDialog);
  els.openImportBtn.addEventListener("click", openImportDialog);
  els.closeImportBtn.addEventListener("click", closeImportDialog);
  els.cancelImportBtn.addEventListener("click", closeImportDialog);
  els.importForm.addEventListener("submit", importPreset);
  els.importPresetDialog.addEventListener("click", (event) => {
    if (event.target === els.importPresetDialog) closeImportDialog();
  });
  els.closeExportStringBtn.addEventListener("click", closeExportStringDialog);
  els.copyExportStringBtn.addEventListener("click", copyExportString);
  els.exportStringDialog.addEventListener("click", (event) => {
    if (event.target === els.exportStringDialog) closeExportStringDialog();
  });
  els.closeCreateBtn.addEventListener("click", closeCreateDialog);
  els.cancelCreateBtn.addEventListener("click", closeCreateDialog);
  els.createPresetDialog.addEventListener("click", (event) => {
    if (event.target === els.createPresetDialog) closeCreateDialog();
  });
  els.closeRenameBtn.addEventListener("click", closeRenameDialog);
  els.cancelRenameBtn.addEventListener("click", closeRenameDialog);
  els.renamePresetDialog.addEventListener("click", (event) => {
    if (event.target === els.renamePresetDialog) closeRenameDialog();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !els.createPresetDialog.hidden) closeCreateDialog();
    if (event.key === "Escape" && !els.renamePresetDialog.hidden) closeRenameDialog();
    if (event.key === "Escape" && !els.importPresetDialog.hidden) closeImportDialog();
    if (event.key === "Escape" && !els.exportStringDialog.hidden) closeExportStringDialog();
    if (event.key === "Escape" && !els.passwordDialog.hidden) closePasswordDialog();
  });
  els.createForm.addEventListener("submit", createProfile);
  els.renameForm.addEventListener("submit", renameProfile);
  els.profileSelect.addEventListener("change", () => loadProfile(els.profileSelect.value));
  els.saveBtn.addEventListener("click", saveProfile);
  els.downloadZipBtn.addEventListener("click", downloadPresetZip);
  els.previewBtn.addEventListener("click", () => generatePreview());
  els.previewEngine.addEventListener("change", () => {
    renderPreviewEngineControls();
    renderPreviewScaleControl(Boolean(state.selected));
    updatePreviewViewControls();
    renderPreview();
    scheduleAutoPreview();
  });
  els.previewSize.addEventListener("change", () => {
    updatePreviewFrameSize();
    scheduleAutoPreview();
  });
  window.addEventListener("resize", () => {
    const changed = updatePreviewFrameSize();
    if (changed && previewSizeIsAuto()) scheduleAutoPreview();
  });
  els.previewPlanet.addEventListener("change", () => scheduleAutoPreview());
  els.previewZoom.addEventListener("input", () => {
    previewPointerView = null;
    updatePreviewViewControls();
    scheduleAutoPreview();
  });
  els.previewResetView.addEventListener("click", resetPreviewView);
  els.previewFrame.addEventListener("pointerdown", beginPreviewPan);
  els.previewFrame.addEventListener("pointermove", updatePreviewPan);
  els.previewFrame.addEventListener("pointerup", finishPreviewPan);
  els.previewFrame.addEventListener("pointercancel", cancelPreviewPan);
  els.previewFrame.addEventListener("lostpointercapture", cancelPreviewPan);
  els.previewFrame.addEventListener("wheel", zoomPreviewAtPointer, { passive: false });
  els.previewImage.addEventListener("dragstart", (event) => event.preventDefault());
  els.previewLossless.addEventListener("change", () => scheduleAutoPreview());
  els.autoRefreshPreview.addEventListener("change", () => {
    renderPreviewButtonState();
    if (els.autoRefreshPreview.checked) {
      scheduleAutoPreview();
    } else {
      cancelAutoPreview();
    }
  });
  els.downloadBtn.addEventListener("click", downloadPreset);
  els.seedValue.addEventListener("input", syncSeedFromToolbar);
  els.seedRandom.addEventListener("change", syncSeedFromToolbar);
  els.seedRerollBtn.addEventListener("click", rerollPreviewSeed);
  els.deleteBtn.addEventListener("click", deleteProfile);
  els.renameBtn.addEventListener("click", openRenameDialog);
  els.duplicateBtn.addEventListener("click", duplicateProfile);

  for (const tab of els.mapgenSubtabs) {
    tab.addEventListener("click", () => setMapgenTab(tab.dataset.mapgenTab));
  }

  setControlsEnabled(false);
  updatePreviewFrameSize();
  await loadConfig();
  await loadSession();
  hideLogin();
  await loadProfiles(false);
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
    if (response.status === 401 && path !== "/api/session") {
      state.session = null;
      renderSession();
      showLogin();
    }
    throw new Error(message);
  }
  if (response.status === 204) return null;
  return response.json();
}

function localProfileDocument(name, mapGen, mapSettings, updatedAt = new Date().toISOString()) {
  return {
    name,
    id: localProfileIdentifier(name),
    source: "local",
    readOnly: false,
    updatedAt,
    directory: "browser memory",
    hasMapGen: true,
    hasMapSettings: true,
    mapGen: cloneJSON(mapGen),
    mapSettings: cloneJSON(mapSettings),
  };
}

function localProfileSummary(profile) {
  return {
    name: profile.name,
    id: profile.id,
    source: profile.source,
    readOnly: false,
    updatedAt: profile.updatedAt,
    directory: profile.directory,
    hasMapGen: true,
    hasMapSettings: true,
    mapGenBytes: JSON.stringify(profile.mapGen || {}).length,
    mapSettingsBytes: JSON.stringify(profile.mapSettings || {}).length,
  };
}

function localProfileIdentifier(name) {
  return `local:${name}`;
}

function isLocalProfileIdentifier(identifier) {
  return String(identifier || "").startsWith("local:");
}

function localProfileByIdentifier(identifier) {
  return state.localProfile?.id === identifier ? state.localProfile : null;
}

function selectedProfileIsLocal() {
  return selectedProfile()?.source === "local";
}

function canonicalProfileName(name) {
  const sanitized = sanitizeProfileName(name);
  return sanitized || "";
}

function sanitizeProfileName(name) {
  let result = "";
  let previousSeparator = false;
  for (const char of String(name || "").trim()) {
    const code = char.charCodeAt(0);
    const alnum = (code >= 48 && code <= 57) || (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
    if (alnum) {
      if (result.length + 1 > maxProfileNameLength) break;
      result += char;
      previousSeparator = false;
      continue;
    }

    let separator = "";
    if (char === " " || char === "." || char === "_" || char === "-") separator = char;
    else if (char === "\t" || char === "\n" || char === "\r") separator = " ";
    else separator = "-";
    if (!result || previousSeparator) continue;
    if (result.length + 1 > maxProfileNameLength) break;
    result += separator;
    previousSeparator = true;
  }
  return result.replace(/[ ._-]+$/g, "");
}

function ensureAvailableLocalProfileName(name, currentID = "") {
  const next = canonicalProfileName(name);
  if (!next) throw new Error("Enter a preset name with letters or numbers.");
  return next;
}

function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value ?? {}));
}

function presetProfileIdentifier(presetKey) {
  return `default:${bundledPresetProfiles[presetKey] || bundledPresetProfiles.default}`;
}

function selectLocalProfile(doc) {
  state.localProfile = doc;
  state.selected = doc.id;
  state.mapGen = cloneJSON(doc.mapGen);
  state.mapSettings = cloneJSON(doc.mapSettings);
  ensureRandomSeedDefault();
  state.dirty = false;
  renderAll();
  scheduleAutoPreview();
}

function upsertLocalProfile(doc) {
  state.localProfile = doc;
}

function saveSelectedLocalProfile() {
  const profile = localProfileByIdentifier(state.selected);
  if (!profile || !state.mapGen || !state.mapSettings) return false;
  profile.mapGen = cloneJSON(state.mapGen);
  profile.mapSettings = cloneJSON(state.mapSettings);
  profile.updatedAt = new Date().toISOString();
  return true;
}

async function loadSession() {
  try {
    const body = await api("/api/session");
    state.session = body.authenticated ? body.user : null;
    renderSession();
    return Boolean(state.session);
  } catch (error) {
    state.session = null;
    renderSession();
    return false;
  }
}

async function login(event) {
  event.preventDefault();
  els.loginError.textContent = "";
  try {
    const body = await api("/api/session", {
      method: "POST",
      body: JSON.stringify({
        username: els.loginUsername.value,
        password: els.loginPassword.value,
      }),
    });
    state.session = body.user;
    els.loginPassword.value = "";
    hideLogin();
    renderSession();
    await loadProfiles(true);
  } catch (error) {
    els.loginError.textContent = error.message;
  }
}

async function logout() {
  try {
    await api("/api/session", { method: "DELETE" });
  } catch {
    // Expire local UI state even if the server already dropped the session.
  }
  state.session = null;
  state.dirty = false;
  closeAdminPanel();
  closePasswordDialog();
  closeRenameDialog();
  hideLogin();
  renderSession();
  await loadProfiles(true);
  showToast("Signed out. Browsing as guest.");
}

function continueAsGuest(event) {
  if (event) event.preventDefault();
  els.loginError.textContent = "";
  hideLogin();
  renderSession();
  renderHeader();
}

function showLogin() {
  els.loginError.textContent = "";
  els.loginDialog.classList.add("active");
  els.loginUsername.focus();
  els.statusLine.textContent = "Sign in to edit presets.";
}

function hideLogin() {
  els.loginDialog.classList.remove("active");
}

function renderSession() {
  const user = state.session;
  els.currentUser.textContent = user ? user.username : "Guest";
  els.currentUser.classList.toggle("guest-user", !user);
  els.loginBtn.hidden = Boolean(user);
  els.adminBtn.hidden = !user || !user.isAdmin;
  els.passwordBtn.hidden = !user;
  els.logoutBtn.hidden = !user;
  els.openCreateBtn.hidden = false;
  els.openImportBtn.hidden = false;
  notifyFactorioUpdate();
}

function openPasswordDialog() {
  if (!state.session) {
    showLogin();
    return;
  }
  els.passwordError.textContent = "";
  els.passwordForm.reset();
  els.passwordDialog.hidden = false;
  window.setTimeout(() => els.currentPassword.focus(), 0);
}

function closePasswordDialog() {
  els.passwordDialog.hidden = true;
  els.passwordError.textContent = "";
  els.passwordForm.reset();
}

async function changePassword(event) {
  event.preventDefault();
  els.passwordError.textContent = "";
  const next = els.newSelfPassword.value;
  if (next !== els.confirmSelfPassword.value) {
    els.passwordError.textContent = "New passwords do not match.";
    return;
  }
  try {
    const body = await api("/api/session/password", {
      method: "PUT",
      body: JSON.stringify({
        currentPassword: els.currentPassword.value,
        newPassword: next,
      }),
    });
    state.session = body.user;
    closePasswordDialog();
    renderSession();
    showToast("Password changed.");
  } catch (error) {
    els.passwordError.textContent = error.message;
  }
}

async function openAdminPanel() {
  if (!state.session?.isAdmin) return;
  els.adminPanel.hidden = false;
  await refreshAdminPanel();
}

function closeAdminPanel() {
  els.adminPanel.hidden = true;
}

function openCreateDialog() {
  seedCreatePresetName();
  els.createPresetDialog.hidden = false;
  window.setTimeout(() => {
    els.profileName.focus();
    const prefixLength = createPresetNamePrefix().length;
    els.profileName.setSelectionRange(prefixLength, els.profileName.value.length);
  }, 0);
}

function seedCreatePresetName() {
  const current = els.profileName.value.trim();
  if (current) return;
  els.profileName.value = `${createPresetNamePrefix()}NewPreset`;
}

function createPresetNamePrefix() {
  return state.session?.username ? `${state.session.username}-` : "";
}

function closeCreateDialog() {
  els.createPresetDialog.hidden = true;
  els.createForm.reset();
}

function openRenameDialog() {
  if (!canEditSelectedProfile()) {
    showToast("Only local or signed-in custom presets can be renamed.", true);
    return;
  }
  els.renameForm.reset();
  els.renameProfileName.value = selectedProfileName();
  els.renamePresetDialog.hidden = false;
  window.setTimeout(() => {
    els.renameProfileName.focus();
    els.renameProfileName.select();
  }, 0);
}

function closeRenameDialog() {
  els.renamePresetDialog.hidden = true;
  els.renameForm.reset();
}

function openImportDialog() {
  els.importForm.reset();
  els.importPresetDialog.hidden = false;
  window.setTimeout(() => els.importProfileName.focus(), 0);
}

function closeImportDialog() {
  els.importPresetDialog.hidden = true;
  els.importForm.reset();
}

async function importPreset(event) {
  event.preventDefault();
  const name = canonicalProfileName(els.importProfileName.value);
  const exchangeString = els.exchangeStringInput.value.trim();
  if (!name || !exchangeString) {
    showToast("Enter a preset name and exchange string.", true);
    return;
  }
  try {
    const body = await api("/api/profiles/import-exchange", {
      method: "POST",
      body: JSON.stringify({ name, exchangeString }),
    });
    closeImportDialog();
    if (!state.session) {
      const localName = ensureAvailableLocalProfileName(body.name || name);
      const doc = localProfileDocument(localName, body.mapGen, body.mapSettings, body.updatedAt);
      upsertLocalProfile(doc);
      await loadProfiles(false);
      selectLocalProfile(doc);
      showToast("Local preset imported.");
      return;
    }
    state.selected = body.id || body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    await loadProfiles(true);
    showToast("Preset imported.");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function refreshAdminPanel() {
  try {
    const [usersBody, auditBody, factorioStatus] = await Promise.all([
      api("/api/users"),
      api("/api/audit?limit=200"),
      api("/api/factorio"),
    ]);
    applyFactorioStatus(factorioStatus);
    renderUsers(usersBody.users || []);
    renderAudit(auditBody.audit || []);
  } catch (error) {
    showToast(error.message, true);
  }
}

async function createUser(event) {
  event.preventDefault();
  try {
    await api("/api/users", {
      method: "POST",
      body: JSON.stringify({
        username: els.newUsername.value,
        password: els.newPassword.value,
        isAdmin: els.newIsAdmin.checked,
      }),
    });
    els.newUsername.value = "";
    els.newPassword.value = "";
    els.newIsAdmin.checked = true;
    await refreshAdminPanel();
    showToast("Account added.");
  } catch (error) {
    showToast(error.message, true);
  }
}

function renderUsers(users) {
  els.usersList.innerHTML = "";
  for (const user of users) {
    const row = document.createElement("div");
    row.className = "user-row";

    const name = document.createElement("input");
    name.value = user.username;
    name.autocomplete = "off";

    const adminLabel = document.createElement("label");
    adminLabel.className = "check-field";
    const admin = document.createElement("input");
    admin.type = "checkbox";
    admin.checked = user.isAdmin;
    const adminText = document.createElement("span");
    adminText.textContent = "Admin";
    adminLabel.append(admin, adminText);

    const password = document.createElement("input");
    password.type = "password";
    password.placeholder = "New password";
    password.autocomplete = "new-password";

    const save = document.createElement("button");
    save.type = "button";
    save.textContent = "Save";
    save.addEventListener("click", async () => {
      try {
        await api(`/api/users/${user.id}`, {
          method: "PUT",
          body: JSON.stringify({
            username: name.value,
            password: password.value,
            isAdmin: admin.checked,
          }),
        });
        password.value = "";
        await refreshAdminPanel();
        showToast("Account saved.");
      } catch (error) {
        showToast(error.message, true);
      }
    });

    const del = document.createElement("button");
    del.type = "button";
    del.className = "danger";
    del.textContent = "Delete";
    del.addEventListener("click", async () => {
      if (!window.confirm(`Delete account ${user.username}?`)) return;
      try {
        await api(`/api/users/${user.id}`, { method: "DELETE" });
        await refreshAdminPanel();
        showToast("Account deleted.");
      } catch (error) {
        showToast(error.message, true);
      }
    });

    row.append(name, adminLabel, password, save, del);
    els.usersList.append(row);
  }
}

function renderAudit(entries) {
  els.auditList.innerHTML = "";
  for (const entry of entries) {
    const row = document.createElement("div");
    row.className = "audit-row";
    const when = new Date(entry.createdAt).toLocaleString();
    row.textContent = `${when}  ${entry.actorUsername}  ${entry.action} ${entry.targetType}:${entry.targetId}`;
    if (entry.detail) {
      const detail = document.createElement("span");
      detail.textContent = entry.detail;
      row.append(detail);
    }
    els.auditList.append(row);
  }
}

function currentFactorioStatus() {
  return state.config.factorio || {
    previewEnabled: Boolean(state.config.factorioBin),
    bin: state.config.factorioBin || "",
    installManaged: false,
    releaseChannel: "experimental",
    updateAvailable: false,
  };
}

function factorioReleaseChannel(status = currentFactorioStatus()) {
  return status.releaseChannel === "stable" ? "stable" : "experimental";
}

function factorioReleaseChannelLabel(status = currentFactorioStatus()) {
  const channel = factorioReleaseChannel(status);
  return channel.charAt(0).toUpperCase() + channel.slice(1);
}

function applyFactorioStatus(status) {
  if (!status) return;
  state.config.factorio = status;
  state.config.previewEnabled = Boolean(state.config.fastPreviewEnabled || status.previewEnabled);
  state.config.factorioBin = status.bin || "";
  renderFactorioStatus(status);
  renderFactorioAdmin(status);
  setControlsEnabled(Boolean(state.selected));
  notifyFactorioUpdate();
}

function renderFactorioStatus(status = currentFactorioStatus()) {
  if (!els.factorioVersion) return;
  const version = status.version || "";
  const latest = status.latestVersion || "";
  const updateAvailable = Boolean(status.updateAvailable && latest);
  const channel = factorioReleaseChannel(status);
  const unavailable = !version && !status.previewEnabled && !fastPreviewAvailable();

  els.factorioVersion.classList.toggle("update", updateAvailable);
  els.factorioVersion.classList.toggle("error", unavailable || Boolean(status.statusError && !version && !fastPreviewAvailable()));
  els.factorioVersion.title = status.statusError || "";

  if (status.installing) {
    els.factorioVersion.textContent = "Factorio: installing...";
  } else if (version) {
    els.factorioVersion.textContent = updateAvailable ? `Factorio ${version} - ${channel} ${latest} available` : `Factorio ${version}`;
  } else if (status.previewEnabled) {
    els.factorioVersion.textContent = "Factorio: version unavailable";
  } else if (fastPreviewAvailable()) {
    els.factorioVersion.textContent = "Fast preview ready";
  } else {
    els.factorioVersion.textContent = "Factorio: not installed";
  }
}

function renderFactorioAdmin(status = currentFactorioStatus()) {
  if (!els.factorioAdminStatus) return;
  els.factorioAdminStatus.innerHTML = "";
  appendFactorioAdminLine("Current", status.version || (status.previewEnabled ? "version unavailable" : "not installed"));
  const channel = factorioReleaseChannel(status);
  const channelLabel = factorioReleaseChannelLabel(status);
  appendFactorioAdminLine("Release channel", channelLabel);
  appendFactorioAdminLine(`Latest ${channel}`, status.latestVersion || "not checked");
  appendFactorioAdminLine("Binary", status.bin || "not configured");
  appendFactorioAdminLine("Install dir", status.installDir || "not configured");
  appendFactorioAdminLine("Managed install", status.installManaged ? "yes" : "no");
  if (status.updateAvailable && status.latestVersion) {
    appendFactorioAdminLine("Update", `${status.latestVersion} is available`, "update");
  }
  if (status.statusError) {
    appendFactorioAdminLine("Status", status.statusError, "error");
  }

  els.refreshFactorioBtn.disabled = Boolean(status.installing);
  els.installFactorioBtn.disabled = !status.installManaged || Boolean(status.installing);
  els.installFactorioBtn.textContent = status.installing ? "Installing..." : `Delete and reinstall ${channel}`;
  els.installFactorioBtn.title = status.installManaged
    ? `Delete the managed Factorio install and install the latest ${channel} headless build.`
    : "Install management is available only when the active binary is from -factorio-dir.";
}

function appendFactorioAdminLine(label, value, className = "") {
  const row = document.createElement("div");
  row.className = className ? `factorio-admin-line ${className}` : "factorio-admin-line";
  const key = document.createElement("span");
  key.textContent = label;
  const val = document.createElement("strong");
  val.textContent = value;
  row.append(key, val);
  els.factorioAdminStatus.append(row);
}

async function refreshFactorioStatus(options = {}) {
  try {
    const status = await api("/api/factorio");
    applyFactorioStatus(status);
    if (options.toast) showToast("Factorio status refreshed.");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function installFactorio() {
  const status = currentFactorioStatus();
  if (!status.installManaged || status.installing) return;
  const channel = factorioReleaseChannel(status);
  if (!window.confirm(`Delete the current managed Factorio install and install a fresh ${channel} headless copy?`)) return;

  renderFactorioAdmin({ ...status, installing: true });
  renderFactorioStatus({ ...status, installing: true });
  try {
    const nextStatus = await api("/api/factorio/install", { method: "POST" });
    applyFactorioStatus(nextStatus);
    if (state.selected) renderPreview();
    await refreshAdminPanel();
    showToast(`Factorio ${nextStatus.version || "headless"} installed.`);
  } catch (error) {
    renderFactorioAdmin(status);
    renderFactorioStatus(status);
    showToast(error.message, true);
  }
}

function notifyFactorioUpdate() {
  const status = currentFactorioStatus();
  if (!status.updateAvailable) {
    factorioUpdateNoticeShown = false;
    return;
  }
  if (!state.session?.isAdmin || factorioUpdateNoticeShown || !status.latestVersion) return;
  showToast(`Factorio ${status.latestVersion} is available on the ${factorioReleaseChannel(status)} channel.`);
  factorioUpdateNoticeShown = true;
}

async function loadConfig() {
  try {
    state.config = await api("/api/config");
    // Match the server's boot-warmed Default world. The cache itself is entirely
    // server-side; this only gives a seed:null preset a stable initial cache key.
    const warmSeed = Number(state.config.fastPreviewWarmSeed);
    if (
      !state.previewSeeds["default:Default"]
      && Number.isInteger(warmSeed)
      && warmSeed >= 1
      && warmSeed <= maxPreviewSeed
    ) state.previewSeeds["default:Default"] = String(warmSeed);
    state.config.fastPreviewEnabled = state.config.fastPreviewEnabled !== false;
    state.config.previewEnabled = Boolean(state.config.fastPreviewEnabled || state.config.factorio?.previewEnabled);
    renderFactorioStatus();
    notifyFactorioUpdate();
  } catch (error) {
    showToast(error.message, true);
  }
}

async function loadProfiles(keepSelection) {
  try {
    const body = await api("/api/profiles");
    const localSummaries = state.localProfile ? [localProfileSummary(state.localProfile)] : [];
    state.profiles = [...localSummaries, ...(body.profiles || [])].sort(compareProfiles);
    renderProfileSelect();

    const selectedStillExists = state.selected && state.profiles.some((profile) => profileIdentifier(profile) === state.selected);
    if (keepSelection && selectedStillExists) {
      await loadProfile(state.selected);
      return;
    }
    if (selectedStillExists) {
      renderHeader();
      return;
    }
    if (state.profiles.length > 0) {
      await loadProfile(initialProfileIdentifier());
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
  if (isLocalProfileIdentifier(name)) {
    const doc = localProfileByIdentifier(name);
    if (!doc) {
      showToast("Local preset not found.", true);
      await loadProfiles(false);
      return;
    }
    cancelAutoPreview();
    state.selected = doc.id;
    state.mapGen = cloneJSON(doc.mapGen);
    state.mapSettings = cloneJSON(doc.mapSettings);
    ensureRandomSeedDefault();
    state.dirty = false;
    renderAll();
    scheduleAutoPreview();
    return;
  }
  try {
    const body = await api(`/api/profiles/${encodeURIComponent(name)}`);
    cancelAutoPreview();
    state.selected = body.id || body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    renderAll();
    if (!canUseDefaultCachedPreview()) scheduleAutoPreview();
  } catch (error) {
    showToast(error.message, true);
  }
}

async function createProfile(event) {
  event.preventDefault();
  const name = canonicalProfileName(els.profileName.value);
  if (!name) {
    showToast("Enter a preset name.", true);
    return;
  }

  if (!state.session) {
    try {
      const template = await api(`/api/profiles/${encodeURIComponent(presetProfileIdentifier(els.presetSelect.value))}`);
      const localName = ensureAvailableLocalProfileName(name);
      const doc = localProfileDocument(localName, template.mapGen, template.mapSettings);
      upsertLocalProfile(doc);
      els.profileName.value = "";
      closeCreateDialog();
      await loadProfiles(false);
      selectLocalProfile(doc);
      showToast("Local preset created.");
    } catch (error) {
      showToast(error.message, true);
    }
    return;
  }

  try {
    const body = await api("/api/profiles", {
      method: "POST",
      body: JSON.stringify({ name, preset: els.presetSelect.value }),
    });
    els.profileName.value = "";
    state.selected = body.id || body.name;
    state.mapGen = body.mapGen;
    state.mapSettings = body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = false;
    await loadProfiles(true);
    closeCreateDialog();
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
  if (selectedProfileIsLocal()) {
    const saved = saveSelectedLocalProfile();
    if (saved) {
      state.dirty = false;
      renderHeader();
      await loadProfiles(true);
      if (!silent) showToast("Saved locally.");
    }
    return saved;
  }
  if (!state.session) {
    if (!silent) showToast("Guest changes are local-only; duplicate or create a local preset to keep them in this browser.", true);
    return false;
  }
  if (!canEditSelectedProfile()) {
    if (!silent) showToast("Duplicate this read-only preset before saving changes.", true);
    return false;
  }
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
      scheduleAutoPreview();
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
  const name = window.prompt("Duplicate preset name", `${selectedProfileName()} copy`);
  if (!name) return;

  if (!state.session || selectedProfileIsLocal()) {
    try {
      const localName = ensureAvailableLocalProfileName(name.trim());
      const doc = localProfileDocument(localName, state.mapGen, state.mapSettings);
      upsertLocalProfile(doc);
      await loadProfiles(false);
      selectLocalProfile(doc);
      showToast("Local preset duplicated.");
    } catch (error) {
      showToast(error.message, true);
    }
    return;
  }

  try {
    const body = await api(`/api/profiles/${encodeURIComponent(state.selected)}/duplicate`, {
      method: "POST",
      body: JSON.stringify({ name: name.trim() }),
    });
    state.selected = body.id || body.name;
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

async function renameProfile(event) {
  event.preventDefault();
  if (!state.selected) return;
  if (selectedProfileIsLocal()) {
    const oldID = state.selected;
    try {
      const name = ensureAvailableLocalProfileName(els.renameProfileName.value, oldID);
      const profile = localProfileByIdentifier(oldID);
      if (!profile) throw new Error("Local preset not found.");
      if (name === profile.name) {
        closeRenameDialog();
        return;
      }
      profile.name = name;
      profile.id = localProfileIdentifier(name);
      profile.updatedAt = new Date().toISOString();
      if (state.previewSeeds[oldID]) {
        state.previewSeeds[profile.id] = state.previewSeeds[oldID];
        delete state.previewSeeds[oldID];
      }
      if (state.previewViews[oldID]) {
        state.previewViews[profile.id] = state.previewViews[oldID];
        delete state.previewViews[oldID];
      }
      state.selected = profile.id;
      closeRenameDialog();
      await loadProfiles(true);
      showToast("Local preset renamed.");
    } catch (error) {
      showToast(error.message, true);
    }
    return;
  }
  if (!canEditSelectedProfile()) {
    showToast("Only custom presets can be renamed.", true);
    return;
  }
  const name = els.renameProfileName.value.trim();
  if (!name) {
    showToast("Enter a preset name.", true);
    return;
  }
  const oldID = state.selected;
  if (name === selectedProfileName()) {
    closeRenameDialog();
    return;
  }
  const wasDirty = state.dirty;
  const currentMapGen = state.mapGen;
  const currentMapSettings = state.mapSettings;

  try {
    const body = await api(`/api/profiles/${encodeURIComponent(oldID)}/rename`, {
      method: "POST",
      body: JSON.stringify({ name }),
    });
    const newID = body.id || body.name;
    if (oldID !== newID && state.previewSeeds[oldID]) {
      state.previewSeeds[newID] = state.previewSeeds[oldID];
      delete state.previewSeeds[oldID];
    }
    if (oldID !== newID && state.previewViews[oldID]) {
      state.previewViews[newID] = state.previewViews[oldID];
      delete state.previewViews[oldID];
    }
    state.selected = newID;
    state.mapGen = wasDirty ? currentMapGen : body.mapGen;
    state.mapSettings = wasDirty ? currentMapSettings : body.mapSettings;
    ensureRandomSeedDefault();
    state.dirty = wasDirty;
    closeRenameDialog();

    await loadProfiles(true);
    if (!canUseDefaultCachedPreview()) scheduleAutoPreview();
    showToast("Preset renamed.");
  } catch (error) {
    showToast(error.message, true);
  }
}

async function deleteProfile() {
  if (!state.selected) return;
  if (selectedProfileIsLocal()) {
    if (!window.confirm(`Delete local preset ${selectedProfileName()}?`)) return;
    state.localProfile = null;
    state.selected = null;
    state.mapGen = null;
    state.mapSettings = null;
    state.dirty = false;
    await loadProfiles(false);
    showToast("Local preset deleted.");
    return;
  }
  if (!canEditSelectedProfile()) {
    showToast("This preset cannot be deleted from the current session.", true);
    return;
  }
  if (!window.confirm(`Delete ${selectedProfileName()}?`)) return;

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

async function downloadPreset() {
  if (!state.selected) return;
  ensureRandomSeedDefault();
  try {
    const path = "/api/profiles/" + encodeURIComponent(state.selected) + "/exchange-string";
    const body = state.mapGen && state.mapSettings
      ? await api(path, {
          method: "POST",
          body: JSON.stringify({ mapGen: state.mapGen, mapSettings: state.mapSettings }),
        })
      : await api(path);
    showExportString(body.exchangeString);
    await copyTextToClipboard(body.exchangeString);
  } catch (error) {
    showToast(error.message, true);
  }
}

async function downloadPresetZip() {
  if (!state.selected) return;
  if (!state.mapGen || !state.mapSettings) {
    window.location.href = "/api/profiles/" + encodeURIComponent(state.selected) + "/download.zip";
    return;
  }
  ensureRandomSeedDefault();
  try {
    const response = await fetch("/api/profiles/" + encodeURIComponent(state.selected) + "/download.zip", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: selectedProfileName(), mapGen: state.mapGen, mapSettings: state.mapSettings }),
    });
    if (!response.ok) {
      let message = response.status + " " + response.statusText;
      try {
        const body = await response.json();
        if (body.error) message = body.error;
      } catch {
        // Keep the HTTP status text.
      }
      throw new Error(message);
    }
    const blob = await response.blob();
    const link = document.createElement("a");
    const url = URL.createObjectURL(blob);
    link.href = url;
    link.download = downloadFilenameFromResponse(response) || ((selectedProfileName() || "preset") + ".zip");
    document.body.append(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  } catch (error) {
    showToast(error.message, true);
  }
}

function downloadFilenameFromResponse(response) {
  const header = response.headers.get("Content-Disposition") || "";
  const match = header.match(/filename="?([^";]+)"?/i);
  return match ? match[1] : "";
}

function showExportString(exchangeString) {
  els.exportStringOutput.value = exchangeString;
  els.exportStringDialog.hidden = false;
  window.setTimeout(() => {
    els.exportStringOutput.focus();
    els.exportStringOutput.select();
  }, 0);
}

function closeExportStringDialog() {
  els.exportStringDialog.hidden = true;
}

async function copyExportString() {
  const text = els.exportStringOutput.value;
  if (!text) return;
  try {
    await copyTextToClipboard(text);
    els.exportStringOutput.focus();
    els.exportStringOutput.select();
  } catch (error) {
    showToast(error.message, true);
  }
}

async function copyTextToClipboard(text) {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      showToast("Export string copied.");
      return;
    } catch {
      // Fall through to the selected-text fallback.
    }
  }
  els.exportStringOutput.focus();
  els.exportStringOutput.select();
  if (document.execCommand && document.execCommand("copy")) {
    showToast("Export string copied.");
    return;
  }
  showToast("Export string ready to copy.");
}

function canAutoRefreshPreview() {
  return Boolean(state.selected && state.config.previewEnabled && els.autoRefreshPreview.checked);
}

function invalidatePreviewResponse() {
  previewRequestRevision++;
  return previewRequestRevision;
}

function clearPreviewWheelTimer() {
  window.clearTimeout(previewWheelTimer);
  previewWheelTimer = null;
}

function cancelAutoPreview() {
  window.clearTimeout(autoPreviewTimer);
  autoPreviewTimer = null;
  clearPreviewWheelTimer();
  autoPreviewPending = false;
  invalidatePreviewResponse();
}

function scheduleAutoPreview() {
  invalidatePreviewResponse();
  clearPreviewWheelTimer();
  requestAutoPreview();
}

function requestAutoPreview() {
  if (!canAutoRefreshPreview()) return;
  if (previewInFlight) {
    autoPreviewPending = true;
    setPreviewUpdating("Generating updated preview...");
    return;
  }
  window.clearTimeout(autoPreviewTimer);
  autoPreviewTimer = null;
  generatePreview({ automatic: true, revision: previewRequestRevision });
}

async function generatePreview(options = {}) {
  const automatic = Boolean(options.automatic);
  if (!state.selected || !state.mapGen) return false;
  if (automatic && !canAutoRefreshPreview()) return false;
  if (!state.config.previewEnabled) {
    if (!automatic) showToast(previewUnavailableMessage(), true);
    return false;
  }
  if (previewInFlight) {
    if (automatic) {
      autoPreviewPending = true;
      setPreviewUpdating("Generating updated preview...");
    }
    return false;
  }
  const requestRevision = Number.isInteger(options.revision)
    ? options.revision
    : invalidatePreviewResponse();
  previewInFlight = true;
  const previewProfile = state.selected;

  try {
    const size = currentPreviewSize();
    const planet = knownPlanetName(els.previewPlanet.value);
    updatePreviewFrameSize(size);
    els.previewBtn.disabled = true;
    setPreviewUpdating("Generating preview...");

    const seed = previewSeedOverride();
    const view = previewViewForCurrentProfile();
    const payload = {
      engine: currentPreviewEngine(),
      size,
      planet,
      zoom: canUsePreviewZoom() ? currentPreviewScale() : "1",
      centerX: view.centerX,
      centerY: view.centerY,
      lossless: canUseLosslessPreview() && els.previewLossless.checked,
      mapGen: previewMapGenPayload(),
    };
    if (seed) payload.seed = seed;
    const body = await api(`/api/profiles/${encodeURIComponent(previewProfile)}/preview`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
    if (!previewRequestIsCurrent(previewProfile, requestRevision)) return true;
    const renderedSize = positivePreviewNumber(body.size, size);
    const renderedScale = positivePreviewNumber(body.tilesPerPixel, Number(payload.zoom));
    const renderedCenterX = finitePreviewNumber(body.centerX, payload.centerX);
    const renderedCenterY = finitePreviewNumber(body.centerY, payload.centerY);
    updatePreviewFrameSize(renderedSize);
    showPreviewImage(body.url, {
      profile: previewProfile,
      size: renderedSize,
      tilesPerPixel: renderedScale,
      centerX: renderedCenterX,
      centerY: renderedCenterY,
    }, { revision: requestRevision });
    if (!automatic) showToast("Preview generated.");
  } catch (error) {
    if (!previewRequestIsCurrent(previewProfile, requestRevision)) return true;
    settlePreviewUpdating();
    els.previewStatus.classList.add("error");
    els.previewStatus.textContent = clippedClientMessage(error.message, 1200);
    if (!automatic) showToast("Preview failed.", true);
    return false;
  } finally {
    previewInFlight = false;
    renderPreviewButtonState();
    if (autoPreviewPending) {
      autoPreviewPending = false;
      if (previewWheelTimer === null) requestAutoPreview();
    } else if (
      requestRevision !== previewRequestRevision
      && previewWheelTimer === null
      && state.selected === previewProfile
    ) {
      settlePreviewUpdating();
    }
  }
  return true;
}

function previewRequestIsCurrent(profile, revision) {
  return state.selected === profile && previewRequestRevision === revision;
}

function setMapgenTab(tabName) {
  for (const tab of els.mapgenSubtabs) {
    tab.classList.toggle("active", tab.dataset.mapgenTab === tabName);
  }
  for (const [name, panel] of Object.entries(els.mapgenSubpanels)) {
    panel.classList.toggle("active", name === tabName);
  }
  if (state.selected) renderVisualControls();
}

function currentMapgenTab() {
  const tab = Array.from(els.mapgenSubtabs).find((entry) => entry.classList.contains("active"));
  return tab?.dataset.mapgenTab || "resources";
}

function renderAll() {
  renderProfileSelect();
  renderHeader();
  if (state.selected) {
    updateSeedToolbar();
    renderVisualControls();
    renderPreview();
  } else {
    updateSeedToolbar();
    clearVisualControls();
    hidePreview("No preview image");
  }
  setControlsEnabled(Boolean(state.selected));
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

  for (const group of groupedProfiles()) {
    const parent = document.createElement("optgroup");
    parent.label = group.label;
    for (const profile of group.profiles) {
      parent.append(profileOption(profile, selected));
    }
    els.profileSelect.append(parent);
  }
}

function profileOption(profile, selected) {
  const option = document.createElement("option");
  option.value = profileIdentifier(profile);
  option.textContent = profileDisplayName(profile);
  option.selected = profileIdentifier(profile) === selected;
  return option;
}

function groupedProfiles() {
  const local = [];
  const custom = [];
  const defaults = [];
  for (const profile of state.profiles) {
    if (profile.source === "local") local.push(profile);
    else if (profile.source === "custom") custom.push(profile);
    else defaults.push(profile);
  }
  return [
    { label: "Local presets", profiles: local },
    { label: "My presets", profiles: custom },
    { label: "Base presets", profiles: defaults },
  ].filter((group) => group.profiles.length > 0);
}

function compareProfiles(a, b) {
  const source = profileSourceRank(a) - profileSourceRank(b);
  if (source !== 0) return source;
  return a.name.localeCompare(b.name);
}

function profileSourceRank(profile) {
  if (profile.source === "local") return 0;
  if (profile.source === "custom") return 1;
  return 2;
}

function initialProfileIdentifier() {
  const defaultProfile = state.profiles.find((profile) => profile.source === "default" && profile.name === "Default");
  return profileIdentifier(defaultProfile || state.profiles[0]);
}

function profileIdentifier(profile) {
  return profile.id || profile.name;
}

function profileDisplayName(profile) {
  return profile.readOnly ? `${profile.name} (read-only)` : profile.name;
}

function selectedProfile() {
  return state.profiles.find((profile) => profileIdentifier(profile) === state.selected) || null;
}

function selectedProfileName() {
  return selectedProfile()?.name || state.selected || "";
}

function renderHeader() {
  if (!state.selected) {
    els.statusLine.textContent = state.session ? "Create a preset to start." : "Guest access: choose a preset to view or download.";
    return;
  }
  els.profileSelect.value = state.selected;
  if (!state.session) {
    if (selectedProfileIsLocal()) {
      els.statusLine.textContent = "Local working preset; export or download when done";
    } else {
      els.statusLine.textContent = state.dirty ? "Guest local-only changes" : "Guest access: create, import, preview, export, and download locally";
    }
    return;
  }
  if (selectedProfile()?.readOnly && state.dirty) {
    els.statusLine.textContent = "Preview-only changes; duplicate before saving";
    return;
  }
  els.statusLine.textContent = state.dirty ? "Unsaved changes" : "";
}

function canEditSelectedProfile() {
  const profile = selectedProfile();
  if (!state.selected || !profile || profile.readOnly) return false;
  return profile.source === "local" || Boolean(state.session);
}

function canDuplicateSelectedProfile() {
  return Boolean(state.selected && state.mapGen && state.mapSettings);
}

function maxPreviewSizeForSession() {
  return state.session ? maxPreviewSizePx : guestMaxPreviewSize;
}

function factorioPreviewAvailable() {
  return Boolean(currentFactorioStatus().previewEnabled);
}

function fastPreviewAvailable() {
  return state.config.fastPreviewEnabled !== false;
}

function currentPreviewEngine() {
  return els.previewEngine?.value === "factorio" ? "factorio" : "fast";
}

function canUsePreviewZoom() {
  return Boolean(state.session || currentPreviewEngine() === "fast");
}

function currentPreviewScale() {
  const scale = Number.parseFloat(els.previewZoom.value);
  if (!Number.isFinite(scale)) return "1";
  const bounded = Math.min(maxPreviewScale, Math.max(minPreviewScale, scale));
  if (bounded !== scale) els.previewZoom.value = String(bounded);
  return String(bounded);
}

function previewViewForCurrentProfile() {
  if (!state.selected) return { centerX: 0, centerY: 0 };
  let view = state.previewViews[state.selected];
  if (!view) {
    view = { centerX: 0, centerY: 0 };
    state.previewViews[state.selected] = view;
  }
  if (!Number.isFinite(view.centerX)) view.centerX = 0;
  if (!Number.isFinite(view.centerY)) view.centerY = 0;
  return view;
}

function displayedPreviewForCurrentProfile() {
  if (!displayedPreview || displayedPreview.profile !== state.selected) return null;
  return displayedPreview;
}

function pointerViewForCurrentPreview() {
  const displayed = displayedPreviewForCurrentProfile();
  if (!displayed) return null;
  if (
    previewPointerView
    && previewPointerView.profile === displayed.profile
    && previewPointerView.displayedLoadID === displayed.loadID
  ) {
    return previewPointerView;
  }
  return {
    profile: displayed.profile,
    displayedLoadID: displayed.loadID,
    size: displayed.size,
    tilesPerPixel: displayed.tilesPerPixel,
    centerX: displayed.centerX,
    centerY: displayed.centerY,
  };
}

function previewViewIsReset() {
  const view = previewViewForCurrentProfile();
  return view.centerX === 0 && view.centerY === 0 && currentPreviewScale() === "1";
}

function updatePreviewViewControls() {
  const enabled = Boolean(state.selected && state.config.previewEnabled);
  const displayed = displayedPreviewForCurrentProfile();
  const interactive = Boolean(
    enabled
    && displayed
    && els.previewImage.getAttribute("src")
    && els.previewEmpty.style.display === "none",
  );
  els.previewResetView.disabled = !enabled || previewViewIsReset();
  els.previewFrame.classList.toggle("preview-interactive", interactive);
  els.previewFrame.title = interactive
    ? canUsePreviewZoom()
      ? "Drag to pan. Use the mouse wheel to zoom around the pointer."
      : "Drag to pan."
    : "";
}

function resetPreviewView() {
  if (!state.selected) return;
  cancelPreviewPan();
  previewPointerView = null;
  const view = previewViewForCurrentProfile();
  view.centerX = 0;
  view.centerY = 0;
  els.previewZoom.value = "1";
  updatePreviewViewControls();
  if (canUseDefaultCachedPreview()) {
    invalidatePreviewResponse();
    clearPreviewWheelTimer();
    autoPreviewPending = false;
    showDefaultCachedPreview();
    return;
  }
  scheduleAutoPreview();
}

function previewInteractionEnabled() {
  return els.previewFrame.classList.contains("preview-interactive");
}

function beginPreviewPan(event) {
  if (!previewInteractionEnabled() || event.isPrimary === false || event.button !== 0) return;
  const rect = els.previewFrame.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return;
  const pointerView = pointerViewForCurrentPreview();
  if (!pointerView) return;
  previewPanGesture = {
    pointerID: event.pointerId,
    profile: pointerView.profile,
    displayedLoadID: pointerView.displayedLoadID,
    size: pointerView.size,
    tilesPerPixel: pointerView.tilesPerPixel,
    startClientX: event.clientX,
    startClientY: event.clientY,
    centerX: pointerView.centerX,
    centerY: pointerView.centerY,
    worldUnitsPerClientX: pointerView.tilesPerPixel * pointerView.size / rect.width,
    worldUnitsPerClientY: pointerView.tilesPerPixel * pointerView.size / rect.height,
    dx: 0,
    dy: 0,
  };
  els.previewFrame.classList.add("preview-panning");
  els.previewFrame.setPointerCapture(event.pointerId);
  event.preventDefault();
}

function updatePreviewPan(event) {
  const gesture = previewPanGesture;
  if (!gesture || gesture.pointerID !== event.pointerId) return;
  gesture.dx = event.clientX - gesture.startClientX;
  gesture.dy = event.clientY - gesture.startClientY;
  els.previewImage.style.transform = `translate(${gesture.dx}px, ${gesture.dy}px)`;
  event.preventDefault();
}

function finishPreviewPan(event) {
  const gesture = previewPanGesture;
  if (!gesture || gesture.pointerID !== event.pointerId) return;
  const moved = Math.hypot(gesture.dx, gesture.dy) >= 2;
  previewPanGesture = null;
  clearPreviewPanTransform();
  if (els.previewFrame.hasPointerCapture(event.pointerId)) {
    els.previewFrame.releasePointerCapture(event.pointerId);
  }
  if (moved) {
    const centerX = gesture.centerX - gesture.dx * gesture.worldUnitsPerClientX;
    const centerY = gesture.centerY - gesture.dy * gesture.worldUnitsPerClientY;
    const view = previewViewForCurrentProfile();
    view.centerX = centerX;
    view.centerY = centerY;
    previewPointerView = {
      profile: gesture.profile,
      displayedLoadID: gesture.displayedLoadID,
      size: gesture.size,
      tilesPerPixel: gesture.tilesPerPixel,
      centerX,
      centerY,
    };
    updatePreviewViewControls();
    scheduleAutoPreview();
  }
  event.preventDefault();
}

function cancelPreviewPan(event) {
  const gesture = previewPanGesture;
  if (event && gesture && gesture.pointerID !== event.pointerId) return;
  previewPanGesture = null;
  clearPreviewPanTransform();
  if (gesture && els.previewFrame.hasPointerCapture(gesture.pointerID)) {
    els.previewFrame.releasePointerCapture(gesture.pointerID);
  }
}

function clearPreviewPanTransform() {
  els.previewFrame.classList.remove("preview-panning");
  els.previewImage.style.removeProperty("transform");
}

function zoomPreviewAtPointer(event) {
  if (!previewInteractionEnabled() || !canUsePreviewZoom()) return;
  const rect = els.previewFrame.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return;

  const pointerView = pointerViewForCurrentPreview();
  if (!pointerView) return;
  const oldScale = pointerView.tilesPerPixel;
  let delta = event.deltaY;
  if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) delta *= 16;
  if (event.deltaMode === WheelEvent.DOM_DELTA_PAGE) delta *= rect.height;
  const scaled = Math.min(
    maxPreviewScale,
    Math.max(minPreviewScale, oldScale * Math.pow(2, delta / 300)),
  );
  const nextScale = Number(scaled.toPrecision(8));
  if (!Number.isFinite(nextScale) || nextScale === oldScale) return;
  event.preventDefault();

  const size = pointerView.size;
  const pixelX = (event.clientX - rect.left) * size / rect.width;
  const pixelY = (event.clientY - rect.top) * size / rect.height;
  const centerX = pointerView.centerX + (pixelX - size / 2) * (oldScale - nextScale);
  const centerY = pointerView.centerY + (pixelY - size / 2) * (oldScale - nextScale);
  const view = previewViewForCurrentProfile();
  view.centerX = centerX;
  view.centerY = centerY;
  previewPointerView = {
    profile: pointerView.profile,
    displayedLoadID: pointerView.displayedLoadID,
    size,
    tilesPerPixel: nextScale,
    centerX,
    centerY,
  };
  els.previewZoom.value = String(nextScale);
  updatePreviewViewControls();

  invalidatePreviewResponse();
  clearPreviewWheelTimer();
  previewWheelTimer = window.setTimeout(() => {
    previewWheelTimer = null;
    requestAutoPreview();
  }, previewWheelDebounceMs);
}

function renderPreviewScaleControl(enabled) {
  const canUseScale = canUsePreviewZoom();
  els.previewScaleField.hidden = !canUseScale;
  if (!canUseScale) els.previewZoom.value = "1";
  els.previewZoom.disabled = !enabled || !state.config.previewEnabled || !canUseScale;
}

function canUseLosslessPreview() {
  return Boolean(state.session);
}

function setControlsEnabled(enabled) {
  const canEdit = canEditSelectedProfile();
  const canDuplicate = canDuplicateSelectedProfile();
  const isLocal = selectedProfileIsLocal();
  els.saveBtn.hidden = !state.session || isLocal;
  els.deleteBtn.hidden = !state.session && !isLocal;
  els.renameBtn.hidden = !state.session && !isLocal;
  els.duplicateBtn.hidden = false;
  els.saveBtn.disabled = !canEdit;
  els.deleteBtn.disabled = !canEdit;
  els.renameBtn.disabled = !canEdit;
  els.duplicateBtn.disabled = !canDuplicate;
  els.downloadBtn.disabled = !enabled;
  els.downloadZipBtn.disabled = !enabled;
  els.profileSelect.disabled = state.profiles.length === 0;
  els.seedRandom.disabled = !enabled;
  els.seedValue.disabled = !enabled || !state.mapGen;
  els.seedRerollBtn.disabled = !enabled || !state.mapGen || !isRandomSeed(state.mapGen.seed);
  els.autoRefreshPreview.disabled = !enabled || !state.config.previewEnabled;
  renderPreviewButtonState();
  renderPreviewEngineControls();
  const maxPreviewSize = maxPreviewSizeForSession();
  for (const option of els.previewSize.options) {
    const optionSize = Number(option.value);
    option.disabled = Number.isFinite(optionSize) && optionSize > maxPreviewSize;
  }
  const selectedPreviewSize = Number(els.previewSize.value);
  if (Number.isFinite(selectedPreviewSize) && selectedPreviewSize > maxPreviewSize) els.previewSize.value = String(maxPreviewSize);

  renderPreviewScaleControl(enabled);

  const canUseLossless = canUseLosslessPreview();
  els.previewLosslessField.hidden = !canUseLossless;
  if (!canUseLossless) els.previewLossless.checked = false;
  els.previewLossless.disabled = !enabled || !state.config.previewEnabled || !canUseLossless;

  els.previewSize.disabled = !enabled || !state.config.previewEnabled;
  els.previewPlanet.disabled = !enabled || !state.config.previewEnabled;
  updatePreviewViewControls();
}

function renderPreviewEngineControls() {
  if (!els.previewEngine) return;
  const exactOption = Array.from(els.previewEngine.options).find((option) => option.value === "factorio");
  const fastOption = Array.from(els.previewEngine.options).find((option) => option.value === "fast");
  const exactAvailable = factorioPreviewAvailable();
  if (fastOption) fastOption.disabled = !fastPreviewAvailable();
  if (exactOption) exactOption.disabled = !exactAvailable;
  if (currentPreviewEngine() === "factorio" && !exactAvailable) els.previewEngine.value = "fast";
  if (currentPreviewEngine() === "fast" && !fastPreviewAvailable() && exactAvailable) els.previewEngine.value = "factorio";
  els.previewEngine.disabled = !state.selected || !state.config.previewEnabled;

  const fast = currentPreviewEngine() === "fast";
  for (const option of els.previewPlanet.options) {
    option.disabled = fast && option.value !== "nauvis";
  }
  if (fast && knownPlanetName(els.previewPlanet.value) !== "nauvis") els.previewPlanet.value = "nauvis";
  els.previewPlanet.title = fast
    ? "Fast custom preview supports Nauvis-style maps. Use Exact for Space Age planets."
    : "Preview surface. Space Age planets require a Space Age-capable Factorio install.";
}

function clearVisualControls() {
  for (const node of [
    els.worldControls,
    els.terrainControls,
    els.autoplaceGrid,
    els.enemyControls,
    els.spaceAgeControls,
    els.simulationControls,
  ]) {
    node.innerHTML = "";
  }
}

function renderVisualControls() {
  clearVisualControls();
  if (!state.mapGen || !state.mapSettings) return;

  addWorldSizeField(els.worldControls);
  addStartingPointField(els.worldControls);
  addPresetSliderField(els.worldControls, "Starting area (x)", state.mapGen, ["starting_area"], 0.25, 6, 0.05, [
    { label: "Small", value: factorioScaleValue("small") },
    { label: "Normal", value: 1 },
    { label: "Ribbon", value: 3 },
    { label: "Huge", value: 4 },
  ]);
  updateSeedToolbar();

  addPresetButtons(els.terrainControls, "Built-in terrain", [
    { label: "Default", tooltip: "Normal settings; the recommended way to play Factorio.", apply: () => applyTerrainPreset("default") },
    { label: "Lakes", tooltip: "Lakes with consistent size, cliffs that follow coastlines, and disabled forest paths.", apply: () => applyTerrainPreset("lakes") },
    { label: "Island", tooltip: "A large island in an endless ocean with disabled forest paths.", apply: () => applyTerrainPreset("island") },
    { label: "Ribbon", tooltip: "Uses the terrain portion of Factorio's Ribbon world preset.", apply: () => applyTerrainPreset("ribbon-world") },
  ]);
  addSelectField(els.terrainControls, "Elevation", state.mapGen, ["property_expression_names", "elevation"], [
    { label: "Normal", value: "" },
    { label: "Lakes", value: "elevation_lakes" },
    { label: "Island", value: "elevation_island" },
  ]);
  addTextField(els.terrainControls, "Cliff prototype", state.mapGen, ["cliff_settings", "name"], "cliff");
  addTextField(els.terrainControls, "Cliff control", state.mapGen, ["cliff_settings", "control"], "nauvis_cliff");
  addNumberField(els.terrainControls, "Cliff base elevation", state.mapGen, ["cliff_settings", "cliff_elevation_0"], -1000, 1000, 1);
  addPresetSliderField(els.terrainControls, "Cliff interval (elevation)", state.mapGen, ["cliff_settings", "cliff_elevation_interval"], 1, 200, 1, [
    { label: "Very high", value: 10 },
    { label: "High", value: 20 },
    { label: "Normal", value: 40 },
    { label: "Low", value: 80 },
    { label: "Very low", value: 160 },
  ]);
  addPresetSliderField(els.terrainControls, "Cliff continuity (x)", state.mapGen, ["cliff_settings", "richness"], 0, 10, 0.1, [
    { label: "Off", value: 0 },
    { label: "Low", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "High", value: 4 },
    { label: "Solid", value: 10 },
  ]);
  addPresetSliderField(els.terrainControls, "Cliff smoothing (x)", state.mapGen, ["cliff_settings", "cliff_smoothing"], 0, 1, 0.05, [
    { label: "Off", value: 0 },
    { label: "Lakes", value: 1 },
  ], { fallback: 0 });
  addPresetSliderField(els.terrainControls, "Moisture frequency (x)", state.mapGen, ["property_expression_names", "control:moisture:frequency"], 0.1, 6, 0.1, [
    { label: "Broad", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "Busy", value: 3 },
  ], { asString: true });
  addPresetSliderField(els.terrainControls, "Moisture bias (-1 to 1)", state.mapGen, ["property_expression_names", "control:moisture:bias"], -1, 1, 0.05, [
    { label: "Dry", value: -0.75 },
    { label: "Neutral", value: 0 },
    { label: "Wet", value: 0.75 },
  ], { asString: true });
  addPresetSliderField(els.terrainControls, "Terrain frequency (x)", state.mapGen, ["property_expression_names", "control:aux:frequency"], 0.1, 6, 0.1, [
    { label: "Broad", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "Busy", value: 3 },
  ], { asString: true });
  addPresetSliderField(els.terrainControls, "Terrain bias (-1 to 1)", state.mapGen, ["property_expression_names", "control:aux:bias"], -1, 1, 0.05, [
    { label: "Sandy", value: -0.6 },
    { label: "Neutral", value: 0 },
    { label: "Green", value: 0.6 },
  ], { asString: true });

  renderAutoplace();
  if (currentMapgenTab() === "spaceage") renderSpaceAgeControls();

  addNoBitersField(els.enemyControls);
  addEnemyBasesField(els.enemyControls);
  addPresetSliderField(els.enemyControls, "Enemy base frequency", state.mapGen, ["autoplace_controls", "enemy-base", "frequency"], 0, 6, 0.1, [
    { label: "Off", value: 0 },
    { label: "Low", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "High", value: 2 },
    { label: "Very high", value: 3 },
  ], { fallback: 1 });
  addPresetSliderField(els.enemyControls, "Enemy base size", state.mapGen, ["autoplace_controls", "enemy-base", "size"], 0, 6, 0.1, [
    { label: "Off", value: 0 },
    { label: "Small", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "Big", value: 2 },
    { label: "Very big", value: 3 },
  ], { fallback: 1 });
  addPresetButtons(els.enemyControls, "Built-in enemy", [
    { label: "Default", tooltip: "Restores normal enemy bases, evolution, expansion, and attack group limits.", apply: () => applyEnemyPreset("default") },
    { label: "Death", tooltip: "Biters are more dangerous and evolve faster.", apply: () => applyEnemyPreset("death-world") },
    { label: "Death marathon", tooltip: "Dangerous and plentiful biters, plus marathon technology costs.", apply: () => applyEnemyPreset("death-world-marathon") },
    { label: "Rail", tooltip: "Keeps bases but disables enemy re-expansion, matching Factorio's Rail world preset.", apply: () => applyEnemyPreset("rail-world") },
  ]);
  addCheckboxField(els.enemyControls, "Peaceful mode", state.mapGen, ["peaceful_mode"], { rerender: true });
  addCheckboxField(els.enemyControls, "No enemies mode", state.mapGen, ["no_enemies_mode"], { rerender: true });
  addCheckboxField(els.enemyControls, "Evolution", state.mapSettings, ["enemy_evolution", "enabled"], { rerender: true });
  addPresetButtons(els.enemyControls, "Evolution profile", [
    { label: "Off", apply: () => setEvolutionProfile(false, 0, 0, 0) },
    { label: "Slow", apply: () => setEvolutionProfile(true, 0.000002, 0.001, 0.0000004) },
    { label: "Normal", apply: () => setEvolutionProfile(true, 0.000004, 0.002, 0.0000009) },
    { label: "Fast", apply: () => setEvolutionProfile(true, 0.000008, 0.003, 0.0000012) },
    { label: "Death", apply: () => setEvolutionProfile(true, 0.00002, 0.004, 0.0000015) },
  ]);
  addPresetSliderField(els.enemyControls, "Evolution per second (%)", state.mapSettings, ["enemy_evolution", "time_factor"], 0, 0.00003, 0.000001, [
    { label: "Slow", value: 0.000002 },
    { label: "Normal", value: 0.000004 },
    { label: "Fast", value: 0.000008 },
    { label: "Death", value: 0.00002 },
  ], { displayScale: 100, displayDecimals: 4 });
  addPresetSliderField(els.enemyControls, "Evolution per spawner kill (%)", state.mapSettings, ["enemy_evolution", "destroy_factor"], 0, 0.01, 0.0001, [
    { label: "Low", value: 0.001 },
    { label: "Normal", value: 0.002 },
    { label: "High", value: 0.004 },
  ], { displayScale: 100, displayDecimals: 2 });
  addPresetSliderField(els.enemyControls, "Evolution per pollution unit (%)", state.mapSettings, ["enemy_evolution", "pollution_factor"], 0, 0.000003, 0.0000001, [
    { label: "Low", value: 0.0000004 },
    { label: "Normal", value: 0.0000009 },
    { label: "High", value: 0.0000015 },
  ], { displayScale: 100, displayDecimals: 5 });
  addCheckboxField(els.enemyControls, "Expansion", state.mapSettings, ["enemy_expansion", "enabled"], { rerender: true });
  addPresetSliderField(els.enemyControls, "Expansion distance (chunks)", state.mapSettings, ["enemy_expansion", "max_expansion_distance"], 0, 32, 1, [
    { label: "Close", value: 4 },
    { label: "Normal", value: 7 },
    { label: "Far", value: 14 },
  ]);
  addPresetSliderField(els.enemyControls, "Settler group size (biters)", state.mapSettings, ["enemy_expansion", "settler_group_max_size"], 0, 80, 1, [
    { label: "Small", value: 10 },
    { label: "Normal", value: 20 },
    { label: "Large", value: 40 },
  ]);
  addPresetSliderField(els.enemyControls, "Expansion cooldown (minutes)", state.mapSettings, ["enemy_expansion", "min_expansion_cooldown"], 0, 216000, 600, [
    { label: "Frequent", value: 3600 },
    { label: "Normal", value: 14400 },
    { label: "Rare", value: 43200 },
  ], { displayScale: 1 / 3600, displayStep: 0.25, displayDecimals: 2 });
  addPresetSliderField(els.enemyControls, "Attack group limit (units)", state.mapSettings, ["unit_group", "max_unit_group_size"], 1, 1000, 1, [
    { label: "Small", value: 100 },
    { label: "Normal", value: 200 },
    { label: "Horde", value: 500 },
  ]);

  addPresetButtons(els.simulationControls, "Built-in advanced", [
    { label: "Default", tooltip: "Restores normal technology price, pollution profile, path pressure, and asteroid rate.", apply: () => applyAdvancedPreset("default") },
    { label: "Marathon", tooltip: "Technologies are more expensive.", apply: () => applyAdvancedPreset("marathon") },
    { label: "Death", tooltip: "Uses Death world pollution aging and attack pollution cost.", apply: () => applyAdvancedPreset("death-world") },
    { label: "Death marathon", tooltip: "Uses Death world marathon technology and pollution settings.", apply: () => applyAdvancedPreset("death-world-marathon") },
  ]);
  addPresetSliderField(els.simulationControls, "Science/research cost (x)", state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 0.25, 10, 0.25, [
    { label: "Cheap", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "Marathon", value: 4 },
  ]);
  addPresetSliderField(els.simulationControls, "Spoil time (x)", state.mapSettings, ["difficulty_settings", "spoil_time_modifier"], 0.1, 10, 0.1, [
    { label: "Fast", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "Slow", value: 2 },
  ]);
  addCheckboxField(els.simulationControls, "Pollution", state.mapSettings, ["pollution", "enabled"]);
  addPresetButtons(els.simulationControls, "Pollution profile", [
    { label: "Clean", apply: () => setPollutionProfile(0.01, 2, 0.5, 0.5) },
    { label: "Normal", apply: () => setPollutionProfile(0.02, 15, 1, 1) },
    { label: "Choked", apply: () => setPollutionProfile(0.04, 5, 0.5, 1.5) },
  ]);
  addPresetSliderField(els.simulationControls, "Pollution spread (%/sec)", state.mapSettings, ["pollution", "diffusion_ratio"], 0, 0.2, 0.005, [
    { label: "Low", value: 0.01 },
    { label: "Normal", value: 0.02 },
    { label: "High", value: 0.04 },
  ], { displayScale: 100, displayDecimals: 1 });
  addPresetSliderField(els.simulationControls, "Pollution absorption (x)", state.mapSettings, ["pollution", "ageing"], 0, 3, 0.05, [
    { label: "Low", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "High", value: 2 },
  ]);
  addPresetSliderField(els.simulationControls, "Attack pollution cost (x)", state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], 0, 5, 0.05, [
    { label: "Low", value: 0.5 },
    { label: "Normal", value: 1 },
    { label: "High", value: 1.5 },
  ]);
  addPresetSliderField(els.simulationControls, "Path goal pressure (x)", state.mapSettings, ["path_finder", "goal_pressure_ratio"], 0.5, 8, 0.1, [
    { label: "Wide", value: 1 },
    { label: "Normal", value: 2 },
    { label: "Direct", value: 4 },
  ]);
  addPresetSliderField(els.simulationControls, "Asteroid spawning (x)", state.mapSettings, ["asteroids", "spawning_rate"], 0, 5, 0.05, [
    { label: "Off", value: 0 },
    { label: "Normal", value: 1 },
    { label: "Heavy", value: 2 },
  ]);
}

function ensureRandomSeedDefault() {
  if (!state.mapGen || state.mapGen.seed !== undefined) return;
  state.mapGen.seed = null;
}

function updateSeedToolbar() {
  if (!state.mapGen) {
    els.seedRandom.checked = true;
    els.seedValue.value = "";
    els.seedValue.readOnly = false;
    els.seedValue.disabled = true;
    els.seedValue.title = "Positive seeds are repeatable; leave random checked to save seed: null.";
    els.seedRerollBtn.hidden = false;
    els.seedRerollBtn.disabled = true;
    return;
  }
  const random = isRandomSeed(state.mapGen.seed);
  els.seedRandom.checked = random;
  els.seedValue.value = random ? previewSeedForCurrentProfile() : state.mapGen.seed;
  els.seedValue.readOnly = random;
  els.seedValue.disabled = !state.selected;
  els.seedValue.title = random
    ? "Preview seed only. This preset still saves seed: null so Factorio chooses a new seed for each real map."
    : "Fixed seed saved into map-gen-settings.json.";
  els.seedRerollBtn.hidden = !random;
  els.seedRerollBtn.disabled = !state.selected || !random;
}

function syncSeedFromToolbar() {
  if (!state.mapGen) return;
  if (els.seedRandom.checked) {
    state.mapGen.seed = null;
    previewSeedForCurrentProfile();
  } else {
    if (els.seedValue.value === "" || numericValue(els.seedValue.value) < 1) {
      els.seedValue.value = previewSeedForCurrentProfile() || "1";
    }
    state.mapGen.seed = Math.max(1, numericValue(els.seedValue.value));
  }
  updateSeedToolbar();
  afterVisualEdit();
}

function rerollPreviewSeed() {
  if (!state.selected || !state.mapGen || !isRandomSeed(state.mapGen.seed)) return;
  state.previewSeeds[state.selected] = randomPreviewSeed();
  updateSeedToolbar();
  if (els.autoRefreshPreview.checked) {
    scheduleAutoPreview();
  } else {
    invalidatePreviewResponse();
    els.previewStatus.classList.remove("error");
    els.previewStatus.textContent = "Preview seed changed; generate a preview to update the image.";
  }
}

function previewSeedOverride() {
  if (!state.mapGen || !isRandomSeed(state.mapGen.seed)) return "";
  return previewSeedForCurrentProfile();
}

function previewSeedForCurrentProfile() {
  if (!state.selected) return "";
  if (!state.previewSeeds[state.selected]) {
    state.previewSeeds[state.selected] = randomPreviewSeed();
  }
  return state.previewSeeds[state.selected];
}

function randomPreviewSeed() {
  if (window.crypto && window.crypto.getRandomValues) {
    const values = new Uint32Array(1);
    window.crypto.getRandomValues(values);
    return String(values[0] || 1);
  }
  return String(Math.floor(Math.random() * maxPreviewSeed) + 1);
}

function renderPreviewButtonState() {
  const autoRefresh = els.autoRefreshPreview.checked;
  els.previewBtn.hidden = autoRefresh;
  els.previewBtn.disabled = autoRefresh || previewInFlight || !state.selected || !state.config.previewEnabled;
}

function previewMapGenPayload() {
  const copy = JSON.parse(JSON.stringify(state.mapGen || {}));
  sanitizeAutoplaceFrequencies(copy);
  return copy;
}

function sanitizeAutoplaceFrequencies(mapGen) {
  const controls = mapGen?.autoplace_controls;
  if (!controls || typeof controls !== "object") return;
  for (const control of Object.values(controls)) {
    if (!control || typeof control !== "object" || Array.isArray(control)) continue;
    control.frequency = validAutoplaceFrequency(control.frequency ?? 1);
  }
}

function validAutoplaceFrequency(value) {
  return Math.max(minAutoplaceFrequency, factorioScaleValue(value, 1));
}

function canUseDefaultCachedPreview() {
  const preview = state.config.defaultPreview;
  const view = previewViewForCurrentProfile();
  return Boolean(
    preview?.url
    && currentPreviewEngine() === "factorio"
    && currentPreviewScale() === "1"
    && view.centerX === 0
    && view.centerY === 0
    && state.selected === "default:Default"
    && !state.dirty
    && knownPlanetName(els.previewPlanet.value) === "nauvis"
  );
}

function showDefaultCachedPreview() {
  if (!canUseDefaultCachedPreview()) return false;
  const preview = state.config.defaultPreview;
  const size = positivePreviewNumber(preview.size, guestMaxPreviewSize);
  updatePreviewFrameSize(size);
  showPreviewImage(preview.url, {
    profile: state.selected,
    size,
    tilesPerPixel: 1,
    centerX: 0,
    centerY: 0,
  });
  return true;
}

function renderPreview() {
  invalidatePreviewResponse();
  cancelPreviewPan();
  updatePreviewViewControls();
  if (showDefaultCachedPreview()) return;
  if (!state.selected) {
    hidePreview("No preview image");
    return;
  }
  els.previewStatus.classList.remove("error");
  els.previewStatus.textContent = state.config.previewEnabled ? "" : previewUnavailableMessage();
  updatePreviewFrameSize();
  hidePreview("No preview image");
}

function previewUnavailableMessage() {
  return currentPreviewEngine() === "factorio" ? "Exact preview binary unavailable." : "Preview renderer unavailable.";
}

function previewSizeIsAuto() {
  return els.previewSize.value === "auto";
}

function currentPreviewSize() {
  if (previewSizeIsAuto()) return clampPreviewSize(autoPreviewSize());
  const size = Number(els.previewSize.value || defaultPreviewSize);
  if (!Number.isFinite(size)) return defaultPreviewSize;
  return clampPreviewSize(size);
}

function autoPreviewSize() {
  const pane = els.mapgenBody?.querySelector(".mapgen-preview-pane");
  if (!pane) return defaultPreviewSize;
  const styles = window.getComputedStyle(pane);
  const horizontalPadding = numericCSSPixels(styles.paddingLeft) + numericCSSPixels(styles.paddingRight);
  const verticalPadding = numericCSSPixels(styles.paddingTop) + numericCSSPixels(styles.paddingBottom);
  const tools = pane.querySelector(".preview-tools");
  const status = els.previewStatus;
  const usedHeight = verticalPadding + outerBlockSize(tools, "height") + outerBlockSize(status, "height") + 8;
  const availableWidth = pane.clientWidth - horizontalPadding - 8;
  const availableHeight = pane.clientHeight - usedHeight;
  const fit = Math.min(availableWidth, availableHeight);
  if (!Number.isFinite(fit) || fit <= 0) return defaultPreviewSize;
  return largestSafePreviewSize(fit);
}

function largestSafePreviewSize(fit) {
  for (let i = safePreviewSizes.length - 1; i >= 0; i--) {
    if (safePreviewSizes[i] <= fit) return safePreviewSizes[i];
  }
  return safePreviewSizes[0];
}

function outerBlockSize(element, dimension) {
  if (!element) return 0;
  const styles = window.getComputedStyle(element);
  const margins = dimension === "height"
    ? numericCSSPixels(styles.marginTop) + numericCSSPixels(styles.marginBottom)
    : numericCSSPixels(styles.marginLeft) + numericCSSPixels(styles.marginRight);
  return (dimension === "height" ? element.offsetHeight : element.offsetWidth) + margins;
}

function numericCSSPixels(value) {
  const number = Number.parseFloat(value);
  return Number.isFinite(number) ? number : 0;
}

function clampPreviewSize(size) {
  return Math.min(maxPreviewSizeForSession(), Math.max(minPreviewSize, Math.round(size)));
}

function updatePreviewFrameSize(size = currentPreviewSize()) {
  const next = clampPreviewSize(size);
  const value = `${next}px`;
  if (els.mapgenBody.style.getPropertyValue("--preview-size") === value) return false;
  els.mapgenBody.style.setProperty("--preview-size", value);
  return true;
}

function positivePreviewNumber(value, fallback) {
  const number = Number(value);
  if (Number.isFinite(number) && number > 0) return number;
  const fallbackNumber = Number(fallback);
  return Number.isFinite(fallbackNumber) && fallbackNumber > 0 ? fallbackNumber : 1;
}

function finitePreviewNumber(value, fallback) {
  const number = Number(value);
  if (Number.isFinite(number)) return number;
  const fallbackNumber = Number(fallback);
  return Number.isFinite(fallbackNumber) ? fallbackNumber : 0;
}

function setPreviewUpdating(message) {
  els.previewStatus.classList.remove("error");
  els.previewStatus.textContent = message;
  els.previewEmpty.style.display = "grid";
  els.previewEmpty.textContent = message;
  els.previewEmpty.classList.add("updating");
  els.previewImage.classList.add("updating");
  updatePreviewViewControls();
}

function clearPreviewUpdating() {
  els.previewEmpty.classList.remove("updating");
  els.previewImage.classList.remove("updating");
  els.previewStatus.classList.remove("error");
  els.previewStatus.textContent = "";
}

function previewReplacementPending(requestRevision) {
  if (requestRevision === previewRequestRevision) return false;
  return Boolean(
    previewInFlight
    || autoPreviewPending
    || (previewWheelTimer !== null && canAutoRefreshPreview())
  );
}

function settlePreviewUpdating(message = state.config.previewEnabled ? "No preview image" : previewUnavailableMessage()) {
  const preview = displayedPreviewForCurrentProfile();
  if (!preview) {
    hidePreview(message);
    return false;
  }
  if (els.previewImage.getAttribute("src") !== preview.url) {
    showPreviewImage(
      preview.url,
      preview,
      { revision: previewRequestRevision, syncView: false },
    );
    return false;
  }
  clearPreviewUpdating();
  els.previewEmpty.style.display = "none";
  els.previewImage.style.display = "block";
  updatePreviewViewControls();
  return true;
}

function showPreviewImage(url, metadata = {}, options = {}) {
  const requestRevision = Number.isInteger(options.revision)
    ? options.revision
    : previewRequestRevision;
  const fallbackView = previewViewForCurrentProfile();
  const nextPreview = {
    profile: metadata.profile || state.selected,
    url,
    size: positivePreviewNumber(metadata.size, currentPreviewSize()),
    tilesPerPixel: positivePreviewNumber(metadata.tilesPerPixel, Number(currentPreviewScale())),
    centerX: finitePreviewNumber(metadata.centerX, fallbackView.centerX),
    centerY: finitePreviewNumber(metadata.centerY, fallbackView.centerY),
  };
  const loadID = ++previewImageLoadID;
  els.previewImage.onload = () => {
    if (loadID !== previewImageLoadID) return;
    if (nextPreview.profile !== state.selected) {
      hidePreview("No preview image");
      return;
    }
    displayedPreview = { ...nextPreview, loadID };
    previewPointerView = null;
    els.previewImage.style.display = "block";
    if (options.syncView !== false && previewRequestIsCurrent(nextPreview.profile, requestRevision)) {
      const view = previewViewForCurrentProfile();
      view.centerX = nextPreview.centerX;
      view.centerY = nextPreview.centerY;
      els.previewZoom.value = String(nextPreview.tilesPerPixel);
    }
    if (previewReplacementPending(requestRevision)) {
      updatePreviewViewControls();
      return;
    }
    settlePreviewUpdating();
  };
  els.previewImage.onerror = () => {
    if (loadID !== previewImageLoadID) return;
    if (nextPreview.profile !== state.selected) {
      hidePreview("No preview image");
      return;
    }
    const fallback = displayedPreviewForCurrentProfile();
    if (fallback && fallback.url !== url) {
      settlePreviewUpdating();
      return;
    }
    displayedPreview = null;
    previewPointerView = null;
    hidePreview(state.config.previewEnabled ? "No preview image" : previewUnavailableMessage());
  };
  const hadImage = Boolean(displayedPreviewForCurrentProfile());
  if (!hadImage) els.previewImage.style.display = "none";
  els.previewStatus.classList.remove("error");
  els.previewStatus.textContent = "Loading preview...";
  els.previewEmpty.style.display = "grid";
  els.previewEmpty.textContent = "Loading preview...";
  els.previewEmpty.classList.add("updating");
  els.previewImage.classList.toggle("updating", hadImage);
  els.previewImage.src = url;
  updatePreviewViewControls();
}

function hidePreview(message) {
  cancelPreviewPan();
  displayedPreview = null;
  previewPointerView = null;
  previewImageLoadID++;
  els.previewImage.removeAttribute("src");
  els.previewImage.classList.remove("updating");
  els.previewImage.style.display = "none";
  els.previewEmpty.classList.remove("updating");
  els.previewEmpty.style.display = "grid";
  els.previewEmpty.textContent = message;
  updatePreviewViewControls();
}

function renderAutoplace() {
  addCheckboxField(els.autoplaceGrid, "Missing autoplace controls", state.mapGen, ["default_enable_all_autoplace_controls"]);
  addPresetButtons(els.autoplaceGrid, "Built-in resources", [
    { label: "Default", tooltip: "Restores normal resource, water, tree, rock, moisture, and cliff autoplace multipliers.", apply: () => applyResourcePreset("default") },
    { label: "Rich", tooltip: "Resource patches have much higher richness, matching Factorio's Rich resources preset.", apply: () => applyResourcePreset("rich-resources") },
    { label: "Rail", tooltip: "Large, sparse resource patches and larger water bodies, matching Factorio's Rail world preset.", apply: () => applyResourcePreset("rail-world") },
    { label: "Ribbon", tooltip: "Frequent smaller rich patches, more water, and reduced cliffs for Ribbon world.", apply: () => applyResourcePreset("ribbon-world") },
    { label: "Lakes trees", tooltip: "Applies the tree settings from Factorio's Lakes and Island presets.", apply: () => applyResourcePreset("lakes") },
  ]);

  for (const resource of resources) {
    renderAutoplaceControl(els.autoplaceGrid, resource);
  }
}

function renderSpaceAgeControls() {
  for (const group of spaceAgeControlGroups) {
    const defaults = planetMapGenDefaults[group.key] || {};
    const section = document.createElement("details");
    section.className = "space-age-group";

    const header = document.createElement("summary");
    header.className = "space-age-group-header";
    const title = document.createElement("h3");
    title.textContent = group.label;
    addTooltipMark(title, group.tooltip, group.label);
    header.append(title);
    section.append(header);

    addPresetButtons(section, "Planet preset", [
      { label: "Apply defaults", tooltip: `Apply Factorio's bundled ${group.label} map generation defaults.`, apply: () => applyPlanetDefaults(group.key) },
    ]);

    for (const resource of group.controls) {
      renderAutoplaceControl(section, resource, { materialize: false });
    }
    renderPlanetSettings(section, group, defaults);
    els.spaceAgeControls.append(section);
  }
}

function renderPlanetSettings(parent, group, defaults) {
  if (defaults.top_level) {
    addPlanetDetails(parent, "Planet flags", false, (body) => {
      renderPlanetTopLevelFields(body, defaults.top_level);
    });
  }
  if (Object.keys(defaults.property_expression_names || {}).length > 0) {
    addPlanetDetails(parent, "Property expressions", false, (body) => {
      renderPlanetPropertyExpressionFields(body, defaults.property_expression_names || {});
    });
  }
  if (defaults.cliff_settings) {
    addPlanetDetails(parent, "Cliff settings", false, (body) => {
      renderPlanetCliffFields(body, defaults.cliff_settings);
    });
  }
  if (defaults.territory_settings) {
    addPlanetDetails(parent, "Territory settings", false, (body) => {
      renderTerritoryFields(body, defaults.territory_settings);
    });
  }
  for (const type of ["tile", "decorative", "entity"]) {
    const names = defaults.autoplace_settings?.[type] || [];
    if (names.length === 0) continue;
    addPlanetDetails(parent, `${titleCase(type)} autoplace settings`, false, (body) => {
      renderAutoplaceSettingCheckboxGroup(body, type, names);
    });
  }
}

function renderPlanetTopLevelFields(parent, fields) {
  for (const [key, fallback] of Object.entries(fields)) {
    if (typeof fallback === "boolean") {
      addCheckboxField(parent, readableExpressionName(key), state.mapGen, [key], { fallback });
    } else if (typeof fallback === "number") {
      addNumberField(parent, readableExpressionName(key), state.mapGen, [key], -1000000, 1000000, 1, fallback);
    } else {
      addTextField(parent, readableExpressionName(key), state.mapGen, [key], fallback);
    }
  }
}

function addPlanetDetails(parent, labelText, open, renderBody) {
  const details = document.createElement("details");
  details.className = "planet-details";
  details.open = open;
  const summary = document.createElement("summary");
  summary.textContent = labelText;
  details.append(summary);
  const body = document.createElement("div");
  body.className = "planet-detail-body";
  renderBody(body);
  details.append(body);
  parent.append(details);
}

function renderPlanetPropertyExpressionFields(parent, expressions) {
  for (const [key, fallback] of Object.entries(expressions)) {
    addTextField(parent, readableExpressionName(key), state.mapGen, ["property_expression_names", key], fallback);
  }
}

function renderPlanetCliffFields(parent, defaults) {
  addTextField(parent, "Cliff prototype", state.mapGen, ["cliff_settings", "name"], defaults.name || "cliff");
  if ("control" in defaults) {
    addTextField(parent, "Cliff control", state.mapGen, ["cliff_settings", "control"], defaults.control || "");
  }
  if ("cliff_elevation_0" in defaults) {
    addNumberField(parent, "Cliff base elevation", state.mapGen, ["cliff_settings", "cliff_elevation_0"], -1000, 1000, 1, defaults.cliff_elevation_0);
  }
  if ("cliff_elevation_interval" in defaults) {
    addNumberField(parent, "Cliff interval", state.mapGen, ["cliff_settings", "cliff_elevation_interval"], 1, 1000, 1, defaults.cliff_elevation_interval);
  }
  if ("richness" in defaults) {
    addNumberField(parent, "Cliff continuity", state.mapGen, ["cliff_settings", "richness"], 0, 10, 0.05, defaults.richness);
  }
  if ("cliff_smoothing" in defaults) {
    addNumberField(parent, "Cliff smoothing", state.mapGen, ["cliff_settings", "cliff_smoothing"], 0, 1, 0.05, defaults.cliff_smoothing);
  }
}

function renderTerritoryFields(parent, defaults) {
  addStringArrayField(parent, "Territory units", state.mapGen, ["territory_settings", "units"], defaults.units || []);
  addTextField(parent, "Territory index", state.mapGen, ["territory_settings", "territory_index_expression"], defaults.territory_index_expression || "");
  addTextField(parent, "Territory variation", state.mapGen, ["territory_settings", "territory_variation_expression"], defaults.territory_variation_expression || "");
  addNumberField(parent, "Minimum territory size", state.mapGen, ["territory_settings", "minimum_territory_size"], 0, 1000, 1, defaults.minimum_territory_size || 0);
}

function renderAutoplaceSettingCheckboxGroup(parent, type, names) {
  const wrap = document.createElement("div");
  wrap.className = "planet-autoplace-setting-group";
  const toolbar = document.createElement("div");
  toolbar.className = "planet-setting-toolbar";
  const label = document.createElement("label");
  label.textContent = `${titleCase(type)} prototypes`;
  const buttons = document.createElement("div");
  buttons.className = "preset-buttons";
  for (const action of [
    { label: "All", enabled: true },
    { label: "None", enabled: false },
  ]) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "preset-button";
    button.textContent = action.label;
    button.addEventListener("click", () => {
      setAllAutoplaceSettings(type, names, action.enabled);
      afterVisualEdit();
      renderVisualControls();
    });
    buttons.append(button);
  }
  toolbar.append(label, buttons);

  const checks = document.createElement("div");
  checks.className = "planet-setting-checks";
  for (const name of names) {
    const item = document.createElement("label");
    item.className = "check-field compact-check";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = autoplaceSettingEnabled(type, name);
    const text = document.createElement("span");
    text.textContent = name;
    input.addEventListener("change", () => {
      setAutoplaceSettingEnabled(type, name, input.checked);
      afterVisualEdit();
    });
    item.append(input, text);
    checks.append(item);
  }
  wrap.append(toolbar, checks);
  parent.append(wrap);
}

function renderAutoplaceControl(parent, resource, options = {}) {
  const controls = ensurePath(state.mapGen, ["autoplace_controls"], {});
  const existing = getPath(controls, [resource.key], null);
  const current = options.materialize === false && (!existing || typeof existing !== "object" || Array.isArray(existing))
    ? {}
    : ensurePath(controls, [resource.key], {});
  const materialize = () => {
    if (!controls[resource.key] || typeof controls[resource.key] !== "object" || Array.isArray(controls[resource.key])) {
      controls[resource.key] = current;
    }
  };
  current.frequency = validAutoplaceFrequency(current.frequency ?? 1);
  current.size = factorioScaleValue(current.size ?? 1, 1);
  if (resource.richness) current.richness = factorioScaleValue(current.richness ?? 1, 1);

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
  addTooltipMark(label, resource.tooltip, resource.label, { labelClass: false });

  const sliders = document.createElement("div");
  sliders.className = "resource-controls";
  addSliderField(sliders, "Frequency", current, ["frequency"], minAutoplaceFrequency, Math.max(6, Number(current.frequency) || minAutoplaceFrequency), 0.1, "", { beforeWrite: materialize });
  addSliderField(sliders, "Size", current, ["size"], 0, Math.max(6, Number(current.size) || 0), 0.1, "", { beforeWrite: materialize });
  if (resource.richness) {
    addSliderField(sliders, "Richness", current, ["richness"], 0, Math.max(10, Number(current.richness) || 0), 0.1, "", { beforeWrite: materialize });
  }

  row.append(label, sliders);
  parent.append(row);
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
  addTooltipMark(wrap, controlTooltip("No biters"), "No biters", { labelClass: false });
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
  addTooltipMark(wrap, controlTooltip("Enemy bases"), "Enemy bases", { labelClass: false });
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

function addWorldSizeField(parent) {
  const wrap = document.createElement("div");
  wrap.className = "preset-field world-size-field";
  const label = document.createElement("label");
  label.textContent = "World size (tiles)";
  addTooltipMark(label, controlTooltip("World size"), "World size");

  const controls = document.createElement("div");
  controls.className = "preset-control";
  const buttons = document.createElement("div");
  buttons.className = "preset-buttons";
  const currentWidth = numericValue(state.mapGen.width ?? 0);
  const currentHeight = numericValue(state.mapGen.height ?? 0);
  const presets = [
    { label: "Infinite", width: 0, height: 0, tooltip: "Factorio's unbounded world size; width and height are omitted." },
    { label: "Compact", width: 512, height: 512, tooltip: "A small finite 512 by 512 tile map." },
    { label: "Standard", width: 1024, height: 1024, tooltip: "A finite 1024 by 1024 tile map." },
    { label: "Large", width: 2048, height: 2048, tooltip: "A finite 2048 by 2048 tile map." },
    { label: "4K", width: 4096, height: 4096, tooltip: "A finite 4096 by 4096 tile map." },
    { label: "8K", width: 8192, height: 8192, tooltip: "A finite 8192 by 8192 tile map." },
    { label: "16K", width: 16384, height: 16384, tooltip: "A finite 16384 by 16384 tile map." },
    { label: "Ribbon", width: 0, height: 128, tooltip: "Factorio's Ribbon world height: infinite width and 128 tiles tall." },
  ];
  for (const preset of presets) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = preset.label;
    button.title = preset.tooltip || `${preset.label}: ${preset.width} x ${preset.height} tiles`;
    button.className = currentWidth === preset.width && currentHeight === preset.height ? "preset-button active" : "preset-button";
    button.addEventListener("click", () => {
      setMapDimension("width", preset.width);
      setMapDimension("height", preset.height);
      afterVisualEdit();
      renderVisualControls();
    });
    buttons.append(button);
  }

  const custom = document.createElement("div");
  custom.className = "paired-inputs";
  const width = document.createElement("input");
  width.type = "number";
  width.min = "0";
  width.step = "1";
  width.value = String(currentWidth);
  width.title = "Width in tiles, 0 leaves it unset for infinite";
  width.ariaLabel = "World width";
  const height = document.createElement("input");
  height.type = "number";
  height.min = "0";
  height.step = "1";
  height.value = String(currentHeight);
  height.title = "Height in tiles, 0 leaves it unset for infinite";
  height.ariaLabel = "World height";
  const sync = () => {
    setMapDimension("width", Math.max(0, Math.round(numericValue(width.value))));
    setMapDimension("height", Math.max(0, Math.round(numericValue(height.value))));
    afterVisualEdit();
  };
  width.addEventListener("input", sync);
  height.addEventListener("input", sync);
  custom.append(width, height);

  controls.append(buttons, custom);
  wrap.append(label, controls);
  parent.append(wrap);
}

function setMapDimension(key, value) {
  if (!state.mapGen) return;
  if (!Number.isFinite(value) || value <= 0) {
    delete state.mapGen[key];
    return;
  }
  state.mapGen[key] = value;
}

function addStartingPointField(parent) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const label = document.createElement("label");
  label.textContent = "Starting point";
  addTooltipMark(label, controlTooltip("Starting point"), "Starting point");

  const point = firstStartingPoint();
  const inputs = document.createElement("div");
  inputs.className = "paired-inputs";
  const x = document.createElement("input");
  x.type = "number";
  x.step = "1";
  x.value = String(numericValue(point.x));
  x.title = "Starting X coordinate";
  x.ariaLabel = "Starting point X";
  const y = document.createElement("input");
  y.type = "number";
  y.step = "1";
  y.value = String(numericValue(point.y));
  y.title = "Starting Y coordinate";
  y.ariaLabel = "Starting point Y";

  const sync = () => {
    point.x = numericValue(x.value);
    point.y = numericValue(y.value);
    afterVisualEdit();
  };
  x.addEventListener("input", sync);
  y.addEventListener("input", sync);

  inputs.append(x, y);
  wrap.append(label, inputs);
  parent.append(wrap);
}

function firstStartingPoint() {
  if (!Array.isArray(state.mapGen.starting_points)) {
    state.mapGen.starting_points = [];
  }
  if (!state.mapGen.starting_points[0] || typeof state.mapGen.starting_points[0] !== "object" || Array.isArray(state.mapGen.starting_points[0])) {
    state.mapGen.starting_points[0] = { x: 0, y: 0 };
  }
  return state.mapGen.starting_points[0];
}

function addSelectField(parent, labelText, root, path, choices) {
  const wrap = document.createElement("div");
  wrap.className = "field select-field";
  const label = document.createElement("label");
  label.textContent = labelText;
  const tooltip = controlTooltip(labelText);
  addTooltipMark(label, tooltip, labelText);
  const select = document.createElement("select");
  if (tooltip) select.title = tooltip;
  const current = getPath(root, path, "") ?? "";
  let matched = false;
  for (const choice of choices) {
    const option = document.createElement("option");
    option.value = choice.value;
    option.textContent = choice.label;
    option.selected = current === choice.value;
    if (option.selected) matched = true;
    select.append(option);
  }
  if (!matched && current !== "") {
    const option = document.createElement("option");
    option.value = current;
    option.textContent = "Custom";
    option.selected = true;
    select.append(option);
  }
  select.addEventListener("change", () => {
    setPath(root, path, select.value);
    afterVisualEdit();
  });
  wrap.append(label, select);
  parent.append(wrap);
}

function addPresetButtons(parent, labelText, presets) {
  const wrap = document.createElement("div");
  wrap.className = "preset-field";
  const label = document.createElement("label");
  label.textContent = labelText;
  addTooltipMark(label, controlTooltip(labelText), labelText);
  const buttons = document.createElement("div");
  buttons.className = "preset-buttons";
  for (const preset of presets) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "preset-button";
    button.textContent = preset.label;
    button.title = preset.tooltip || `${labelText}: ${preset.label}`;
    button.addEventListener("click", () => {
      preset.apply();
      afterVisualEdit();
      renderVisualControls();
    });
    buttons.append(button);
  }
  wrap.append(label, buttons);
  parent.append(wrap);
}

function addPresetSliderField(parent, labelText, root, path, min, max, step, presets, options = {}) {
  const fallback = options.fallback ?? presets.find((preset) => preset.label === "Normal")?.value ?? min;
  const value = factorioScaleValue(getPath(root, path, fallback), factorioScaleValue(fallback, min));
  const tooltip = controlTooltip(labelText);
  const displayScale = options.displayScale ?? 1;
  const displayStep = options.displayStep ?? step * displayScale;
  const displayDecimals = options.displayDecimals ?? decimalPlaces(displayStep);
  const displayValue = (raw) => formatDisplayValue(numericValue(raw) * displayScale, displayDecimals);
  const rawValue = (display) => displayScale === 0 ? numericValue(display) : numericValue(display) / displayScale;
  const wrap = document.createElement("div");
  wrap.className = "preset-slider-field";

  const label = document.createElement("label");
  label.textContent = labelText;
  addTooltipMark(label, tooltip, labelText);
  const controls = document.createElement("div");
  controls.className = "preset-control";
  const row = document.createElement("div");
  row.className = "slider-input-row";
  const slider = document.createElement("input");
  slider.type = "range";
  slider.min = displayValue(min);
  slider.max = displayValue(max);
  slider.step = formatDisplayValue(displayStep, displayDecimals);
  slider.value = displayValue(value);
  if (tooltip) slider.title = tooltip;
  const number = document.createElement("input");
  number.type = "number";
  number.min = displayValue(min);
  number.max = displayValue(max);
  number.step = formatDisplayValue(displayStep, displayDecimals);
  number.value = displayValue(value);
  if (tooltip) number.title = tooltip;
  const buttons = document.createElement("div");
  buttons.className = "preset-buttons";

  const writeRaw = (nextRaw, editOptions = {}) => {
    const rounded = normalizeStepValue(nextRaw, step);
    const display = displayValue(rounded);
    slider.value = display;
    number.value = display;
    setPath(root, path, options.asString ? String(rounded) : rounded);
    for (const button of buttons.querySelectorAll("button")) {
      button.classList.toggle("active", sameNumericValue(button.dataset.value, rounded));
    }
    afterVisualEdit(editOptions);
  };
  const writeDisplay = (nextDisplay, editOptions = {}) => writeRaw(rawValue(nextDisplay), editOptions);

  slider.addEventListener("input", () => writeDisplay(slider.value, { preview: false }));
  slider.addEventListener("change", () => scheduleAutoPreview());
  number.addEventListener("input", () => writeDisplay(number.value));
  for (const preset of presets) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = sameNumericValue(preset.value, value) ? "preset-button active" : "preset-button";
    button.dataset.value = String(preset.value);
    button.textContent = preset.label;
    button.title = preset.tooltip || `${labelText}: ${preset.label} (${displayValue(preset.value)})`;
    button.addEventListener("click", () => writeRaw(preset.value));
    buttons.append(button);
  }

  row.append(slider, number);
  controls.append(row, buttons);
  wrap.append(label, controls);
  parent.append(wrap);
}

function controlTooltip(labelText) {
  return controlTooltips[labelText] || controlTooltips[baseControlLabel(labelText)] || "";
}

function baseControlLabel(labelText) {
  return String(labelText).replace(/\s*\([^)]*\)$/, "");
}

function knownPlanetName(name) {
  return knownPlanets.includes(name) ? name : "nauvis";
}

function titleCase(value) {
  const text = String(value || "");
  return text ? text[0].toUpperCase() + text.slice(1) : "";
}

function readableExpressionName(key) {
  return String(key)
    .replace(/^entity:/, "Entity: ")
    .replace(/^decorative:/, "Decorative: ")
    .replace(/:probability$/, " probability")
    .replace(/:richness$/, " richness")
    .replaceAll("_", " ");
}

function addTooltipMark(parent, tooltip, labelText, options = {}) {
  const text = typeof tooltip === "string" ? tooltip.trim() : "";
  if (!text) {
    parent.removeAttribute("title");
    return;
  }
  parent.title = text;
  if (options.labelClass !== false) parent.classList.add("tooltip-label");
  const mark = document.createElement("span");
  mark.className = "tooltip-mark";
  mark.textContent = "i";
  mark.tabIndex = 0;
  mark.title = text;
  mark.setAttribute("aria-label", `${labelText}: ${text}`);
  parent.append(mark);
}

function factorioScaleValue(value, fallback = 0) {
  if (typeof value === "number") return Number.isFinite(value) ? value : fallback;
  if (typeof value === "string") {
    const key = value.trim();
    if (key in factorioScaleValues) return factorioScaleValues[key];
    const number = Number(key);
    if (Number.isFinite(number)) return number;
  }
  return fallback;
}

function setAutoplaceControl(key, values) {
  const control = ensurePath(state.mapGen, ["autoplace_controls", key], {});
  for (const [field, value] of Object.entries(values)) {
    control[field] = factorioScaleValue(value, 1);
  }
}

function setResourceControls(values) {
  for (const key of resourceKeys) {
    setAutoplaceControl(key, values);
  }
}

function setTerrainExpressions(values) {
  const expressions = ensurePath(state.mapGen, ["property_expression_names"], {});
  for (const key of ["elevation", "moisture", "aux", "cliffiness", "cliff_elevation", "trees_forest_path_cutout"]) {
    if (!(key in values)) delete expressions[key];
  }
  for (const [key, value] of Object.entries(values)) {
    expressions[key] = value;
  }
}

function applyPlanetDefaults(planetKey) {
  const defaults = planetMapGenDefaults[planetKey];
  if (!defaults || !state.mapGen) return;
  for (const key of allPlanetTopLevelKeys()) {
    if (!defaults.top_level || !(key in defaults.top_level)) delete state.mapGen[key];
  }
  for (const [key, value] of Object.entries(defaults.top_level || {})) {
    state.mapGen[key] = cloneJSON(value);
  }
  state.mapGen.property_expression_names = cloneJSON(defaults.property_expression_names || {});
  if (defaults.cliff_settings) {
    state.mapGen.cliff_settings = cloneJSON(defaults.cliff_settings);
  } else {
    delete state.mapGen.cliff_settings;
  }
  if (defaults.territory_settings) {
    state.mapGen.territory_settings = cloneJSON(defaults.territory_settings);
  } else {
    delete state.mapGen.territory_settings;
  }
  state.mapGen.autoplace_settings = planetAutoplaceSettings(defaults.autoplace_settings || {});
  const controls = ensurePath(state.mapGen, ["autoplace_controls"], {});
  for (const key of defaults.autoplace_controls || []) {
    if (!controls[key] || typeof controls[key] !== "object" || Array.isArray(controls[key])) {
      controls[key] = {};
    }
  }
  setPreviewPlanet(planetKey);
}

function allPlanetTopLevelKeys() {
  const keys = new Set();
  for (const defaults of Object.values(planetMapGenDefaults)) {
    for (const key of Object.keys(defaults.top_level || {})) keys.add(key);
  }
  return keys;
}

function planetAutoplaceSettings(settings) {
  const out = {};
  for (const [type, names] of Object.entries(settings)) {
    out[type] = { settings: {} };
    for (const name of names) {
      out[type].settings[name] = {};
    }
  }
  return out;
}

function setPreviewPlanet(planetKey) {
  if (!knownPlanets.includes(planetKey)) return;
  els.previewPlanet.value = planetKey;
  scheduleAutoPreview();
}

function applyResourcePreset(name) {
  switch (name) {
    case "rich-resources":
      setResourceControls({ frequency: 1, size: 1, richness: "very-good" });
      break;
    case "rail-world":
      setResourceControls({ frequency: 0.33333333333, size: 3, richness: 1 });
      setAutoplaceControl("water", { frequency: 0.5, size: 1.5 });
      break;
    case "ribbon-world":
      setResourceControls({ frequency: 3, size: 0.5, richness: 2 });
      setAutoplaceControl("water", { frequency: 4, size: 0.25 });
      setAutoplaceControl("nauvis_cliff", { frequency: 0.25, size: 0.75 });
      break;
    case "lakes":
      setAutoplaceControl("trees", { frequency: 1, size: 0.5 });
      break;
    default:
      setResourceControls({ frequency: 1, size: 1, richness: 1 });
      for (const key of ["water", "trees", "rocks", "starting_area_moisture", "nauvis_cliff"]) {
        setAutoplaceControl(key, { frequency: 1, size: 1 });
      }
      break;
  }
}

function applyTerrainPreset(name) {
  switch (name) {
    case "lakes":
      setTerrainExpressions({
        elevation: "elevation_lakes",
        moisture: "moisture_basic",
        aux: "aux_basic",
        cliffiness: "cliffiness_basic",
        cliff_elevation: "cliff_elevation_from_elevation",
        trees_forest_path_cutout: 1,
      });
      setPath(state.mapGen, ["cliff_settings", "cliff_smoothing"], 1);
      setAutoplaceControl("trees", { frequency: 1, size: 0.5 });
      break;
    case "island":
      setTerrainExpressions({
        elevation: "elevation_island",
        moisture: "moisture_basic",
        aux: "aux_basic",
        cliffiness: "cliffiness_basic",
        cliff_elevation: "cliff_elevation_from_elevation",
        trees_forest_path_cutout: 1,
      });
      setPath(state.mapGen, ["cliff_settings", "cliff_smoothing"], 1);
      setAutoplaceControl("trees", { frequency: 1, size: 0.5 });
      break;
    case "ribbon-world":
      setTerrainExpressions({ elevation: "elevation_lakes", trees_forest_path_cutout: 1 });
      setAutoplaceControl("water", { frequency: 4, size: 0.25 });
      setAutoplaceControl("nauvis_cliff", { frequency: 0.25, size: 0.75 });
      break;
    default:
      setTerrainExpressions({ elevation: "" });
      setPath(state.mapGen, ["cliff_settings", "cliff_smoothing"], 0);
      setAutoplaceControl("trees", { frequency: 1, size: 1 });
      setAutoplaceControl("water", { frequency: 1, size: 1 });
      setAutoplaceControl("nauvis_cliff", { frequency: 1, size: 1 });
      break;
  }
}

function applyEnemyPreset(name) {
  state.mapGen.peaceful_mode = false;
  switch (name) {
    case "death-world":
      setAutoplaceControl("enemy-base", { frequency: "very-high", size: "very-big" });
      state.mapGen.starting_area = factorioScaleValue("small");
      setEvolutionProfile(true, 0.00002, 0.002, 0.0000012);
      setPath(state.mapSettings, ["enemy_expansion", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "ageing"], 0.5);
      setPath(state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], 0.5);
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 1);
      break;
    case "death-world-marathon":
      setAutoplaceControl("enemy-base", { frequency: "very-high", size: "very-big" });
      state.mapGen.starting_area = factorioScaleValue("small");
      setEvolutionProfile(true, 0.000015, 0.002, 0.0000010);
      setPath(state.mapSettings, ["enemy_expansion", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "ageing"], 0.5);
      setPath(state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], 0.8);
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 4);
      break;
    case "rail-world":
      setAutoplaceControl("enemy-base", { frequency: 1, size: 1 });
      setEvolutionProfile(true, 0.000002, 0.002, 0.0000009);
      setPath(state.mapSettings, ["enemy_expansion", "enabled"], false);
      break;
    default:
      setAutoplaceControl("enemy-base", { frequency: 1, size: 1 });
      setEvolutionProfile(true, 0.000004, 0.002, 0.0000009);
      setPath(state.mapSettings, ["enemy_expansion", "enabled"], true);
      setPath(state.mapSettings, ["enemy_expansion", "max_expansion_distance"], 7);
      setPath(state.mapSettings, ["enemy_expansion", "settler_group_max_size"], 20);
      setPath(state.mapSettings, ["enemy_expansion", "min_expansion_cooldown"], 14400);
      setPath(state.mapSettings, ["unit_group", "max_unit_group_size"], 200);
      break;
  }
}

function applyAdvancedPreset(name) {
  switch (name) {
    case "marathon":
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 4);
      break;
    case "death-world":
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 1);
      setPath(state.mapSettings, ["pollution", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "ageing"], 0.5);
      setPath(state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], 0.5);
      break;
    case "death-world-marathon":
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 4);
      setPath(state.mapSettings, ["pollution", "enabled"], true);
      setPath(state.mapSettings, ["pollution", "ageing"], 0.5);
      setPath(state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], 0.8);
      break;
    default:
      setPath(state.mapSettings, ["difficulty_settings", "technology_price_multiplier"], 1);
      setPath(state.mapSettings, ["difficulty_settings", "spoil_time_modifier"], 1);
      setPollutionProfile(0.02, 15, 1, 1);
      setPath(state.mapSettings, ["path_finder", "goal_pressure_ratio"], 2);
      setPath(state.mapSettings, ["asteroids", "spawning_rate"], 1);
      break;
  }
}

function setEvolutionProfile(enabled, timeFactor, destroyFactor, pollutionFactor) {
  setPath(state.mapSettings, ["enemy_evolution", "enabled"], enabled);
  setPath(state.mapSettings, ["enemy_evolution", "time_factor"], timeFactor);
  setPath(state.mapSettings, ["enemy_evolution", "destroy_factor"], destroyFactor);
  setPath(state.mapSettings, ["enemy_evolution", "pollution_factor"], pollutionFactor);
}

function setPollutionProfile(diffusionRatio, minToDiffuse, ageing, attackModifier) {
  setPath(state.mapSettings, ["pollution", "enabled"], true);
  setPath(state.mapSettings, ["pollution", "diffusion_ratio"], diffusionRatio);
  setPath(state.mapSettings, ["pollution", "min_to_diffuse"], minToDiffuse);
  setPath(state.mapSettings, ["pollution", "ageing"], ageing);
  setPath(state.mapSettings, ["pollution", "enemy_attack_pollution_consumption_modifier"], attackModifier);
}

function normalizeStepValue(value, step) {
  const number = numericValue(value);
  if (!Number.isFinite(number)) return 0;
  return Number(number.toFixed(decimalPlaces(step)));
}

function decimalPlaces(value) {
  const text = String(value);
  if (text.includes("e-")) return Number(text.split("e-")[1]) || 0;
  if (!text.includes(".")) return 0;
  return text.split(".")[1].length;
}

function formatDisplayValue(value, decimals) {
  const number = numericValue(value);
  if (!Number.isFinite(number)) return "0";
  return String(Number(number.toFixed(decimals)));
}

function sameNumericValue(left, right) {
  return Math.abs(numericValue(left) - numericValue(right)) < 1e-9;
}

function addNumberField(parent, labelText, root, path, min, max, step, fallback = 0) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const label = document.createElement("label");
  label.textContent = labelText;
  const tooltip = controlTooltip(labelText);
  addTooltipMark(label, tooltip, labelText);
  const input = document.createElement("input");
  if (tooltip) input.title = tooltip;
  input.type = "number";
  input.min = String(min);
  input.max = String(max);
  input.step = String(step);
  input.value = getPath(root, path, fallback);
  input.addEventListener("input", () => {
    setPath(root, path, numericValue(input.value));
    afterVisualEdit();
  });
  wrap.append(label, input);
  parent.append(wrap);
}

function addStringArrayField(parent, labelText, root, path, fallback = []) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const label = document.createElement("label");
  label.textContent = labelText;
  addTooltipMark(label, controlTooltip(labelText), labelText);
  const input = document.createElement("input");
  input.type = "text";
  const current = getPath(root, path, fallback);
  input.value = Array.isArray(current) ? current.join(", ") : String(current || "");
  input.addEventListener("input", () => {
    const values = input.value.split(",").map((value) => value.trim()).filter(Boolean);
    setPath(root, path, values);
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
  const tooltip = controlTooltip(labelText);
  addTooltipMark(label, tooltip, labelText);
  const input = document.createElement("input");
  if (tooltip) input.title = tooltip;
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
  input.checked = Boolean(getPath(root, path, options.fallback ?? false));
  const text = document.createElement("span");
  text.textContent = labelText;
  addTooltipMark(wrap, controlTooltip(labelText), labelText, { labelClass: false });
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

function addSliderField(parent, labelText, root, path, min, max, step, tooltip = controlTooltip(labelText), options = {}) {
  const value = factorioScaleValue(getPath(root, path, 1), 1);
  const wrap = document.createElement("div");
  wrap.className = "slider-field";

  const label = document.createElement("label");
  label.textContent = labelText;
  addTooltipMark(label, tooltip, labelText);
  const slider = document.createElement("input");
  slider.type = "range";
  slider.min = String(min);
  slider.max = String(max);
  slider.step = String(step);
  slider.value = String(value);
  if (tooltip) slider.title = tooltip;
  const number = document.createElement("input");
  number.type = "number";
  number.min = String(min);
  number.max = String(max);
  number.step = String(step);
  number.value = String(value);
  if (tooltip) number.title = tooltip;

  const sync = (source, editOptions = {}) => {
    const next = Math.min(max, Math.max(min, numericValue(source.value)));
    slider.value = String(next);
    number.value = String(next);
    if (options.beforeWrite) options.beforeWrite();
    setPath(root, path, next);
    afterVisualEdit(editOptions);
  };

  slider.addEventListener("input", () => sync(slider, { preview: false }));
  slider.addEventListener("change", () => scheduleAutoPreview());
  number.addEventListener("input", () => sync(number));
  wrap.append(label, slider, number);
  parent.append(wrap);
}

function addExpressionSliderField(parent, labelText, root, path, min, max, step) {
  const rawValue = getPath(root, path, "0");
  const value = Number(rawValue);
  const fallback = Number.isFinite(value) ? value : 0;
  const tooltip = controlTooltip(labelText);
  const wrap = document.createElement("div");
  wrap.className = "slider-field";

  const label = document.createElement("label");
  label.textContent = labelText;
  addTooltipMark(label, tooltip, labelText);
  const slider = document.createElement("input");
  slider.type = "range";
  slider.min = String(min);
  slider.max = String(max);
  slider.step = String(step);
  slider.value = String(fallback);
  if (tooltip) slider.title = tooltip;
  const number = document.createElement("input");
  number.type = "number";
  number.min = String(min);
  number.max = String(max);
  number.step = String(step);
  number.value = String(fallback);
  if (tooltip) number.title = tooltip;

  const sync = (source, editOptions = {}) => {
    const next = numericValue(source.value);
    slider.value = String(next);
    number.value = String(next);
    setPath(root, path, String(next));
    afterVisualEdit(editOptions);
  };

  slider.addEventListener("input", () => sync(slider, { preview: false }));
  slider.addEventListener("change", () => scheduleAutoPreview());
  number.addEventListener("input", () => sync(number));
  wrap.append(label, slider, number);
  parent.append(wrap);
}

function afterVisualEdit(options = {}) {
  state.dirty = !selectedProfileIsLocal();
  if (selectedProfileIsLocal()) saveSelectedLocalProfile();
  renderHeader();
  if (options.preview !== false) {
    scheduleAutoPreview();
  } else {
    invalidatePreviewResponse();
  }
}

function numericValue(value) {
  if (value === "" || value === null || value === undefined) return 0;
  return factorioScaleValue(value, 0);
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

function deletePath(root, path) {
  let current = root;
  for (const key of path.slice(0, -1)) {
    if (!current || typeof current !== "object") return;
    current = current[key];
  }
  if (current && typeof current === "object") delete current[path[path.length - 1]];
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

function autoplaceSettingEnabled(type, name) {
  return getPath(state.mapGen, ["autoplace_settings", type, "settings", name], undefined) !== undefined;
}

function setAllAutoplaceSettings(type, names, enabled) {
  for (const name of names) {
    setAutoplaceSettingEnabled(type, name, enabled);
  }
}

function setAutoplaceSettingEnabled(type, name, enabled) {
  if (enabled) {
    setPath(state.mapGen, ["autoplace_settings", type, "settings", name], {});
    return;
  }
  deletePath(state.mapGen, ["autoplace_settings", type, "settings", name]);
  cleanupAutoplaceSettings(type);
}

function cleanupAutoplaceSettings(type) {
  const settings = getPath(state.mapGen, ["autoplace_settings", type, "settings"], null);
  if (settings && typeof settings === "object" && Object.keys(settings).length === 0) {
    deletePath(state.mapGen, ["autoplace_settings", type]);
  }
  const autoplaceSettings = getPath(state.mapGen, ["autoplace_settings"], null);
  if (autoplaceSettings && typeof autoplaceSettings === "object" && Object.keys(autoplaceSettings).length === 0) {
    delete state.mapGen.autoplace_settings;
  }
}

function clippedClientMessage(message, limit) {
  const text = String(message || "").trim();
  if (text.length <= limit) return text;
  return `${text.slice(0, limit)}\n... message clipped ...`;
}

const toastDurationMs = 12000;
const toastErrorDurationMs = 20000;
let toastTimer = null;

function showToast(message, error = false) {
  window.clearTimeout(toastTimer);
  els.toastMessage.textContent = message;
  els.toast.classList.toggle("error", error);
  els.toast.setAttribute("role", error ? "alert" : "status");
  els.toast.classList.add("show");
  toastTimer = window.setTimeout(hideToast, error ? toastErrorDurationMs : toastDurationMs);
}

function hideToast() {
  window.clearTimeout(toastTimer);
  toastTimer = null;
  els.toast.classList.remove("show");
}
