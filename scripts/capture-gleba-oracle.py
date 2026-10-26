#!/usr/bin/env python3
"""Capture Gleba terrain fields with an isolated Factorio 2.1 installation.

Usage: python3 scripts/capture-gleba-oracle.py FACTORIO_ROOT OUTPUT.json [--previews]
Requires Space Age. Never reads or writes the player's saves, mods or config.
The output is suitable for TestFactorioGlebaReleaseCases.
"""
import json
import pathlib
import subprocess
import sys
import tempfile


def main():
    root = pathlib.Path(sys.argv[1]).resolve()
    output = pathlib.Path(sys.argv[2]).resolve()
    binary = root / "bin/x64/factorio"
    version = subprocess.check_output([str(binary), "--version"], text=True).splitlines()[0]
    release = version.split()[1]
    if not release.startswith("2.1."):
        raise SystemExit(f"Expected Factorio 2.1: {version}")
    repo = pathlib.Path(__file__).resolve().parent.parent
    original = json.loads((repo / "testdata/gleba/oracle-gleba-fields.seed123456.json").read_text())
    cases = [
        {"seed": 123456, "positions": original["positions"]},
        {"seed": 3461896550, "waterFrequency": 0.5, "waterSize": 2, "plantFrequency": 3},
        {"seed": 4294967295, "waterFrequency": 6, "waterSize": 0.17, "plantFrequency": 0.5},
        {"seed": 0, "waterFrequency": 1, "waterSize": 1, "plantFrequency": 1,
         "startingPoints": [{"x": 512, "y": -256}, {"x": -1024, "y": 768}]},
    ]
    if "--features" in sys.argv[3:]:
        cases = [{"seed": 123456, "positions": original["positions"] +
                  [[x, y] for y in range(-70, 15, 2) for x in range(0, 89, 2)]}]
    for case in cases:
        case.setdefault("positions", original["positions"][:256])
    with tempfile.TemporaryDirectory(prefix="factmapgen-gleba-oracle-") as directory:
        work = pathlib.Path(directory)
        mod = work / "mods/gleba-oracle_1.0.0"
        mod.mkdir(parents=True)
        (mod / "info.json").write_text(json.dumps({"name": "gleba-oracle", "version": "1.0.0", "title": "Gleba oracle", "author": "FactMapGen", "factorio_version": "2.1", "dependencies": ["space-age"]}))
        (work / "mods/mod-list.json").write_text(json.dumps({"mods": [{"name": n, "enabled": True} for n in ["base", "quality", "elevated-rails", "space-age", "gleba-oracle"]]}))
        (mod / "control.lua").write_text('''local cases = helpers.json_to_table([==[''' + json.dumps(cases) + ''']==])
script.on_init(function()
  local result = {cases = {}}
  local surface = game.planets.gleba.create_surface()
  for _, case in ipairs(cases) do
    local settings = surface.map_gen_settings
    settings.seed = case.seed
    settings.starting_points = case.startingPoints or {{x=0,y=0}}
    settings.autoplace_controls.gleba_water = {frequency=case.waterFrequency or 1,size=case.waterSize or 1,richness=1}
    settings.autoplace_controls.gleba_plants = {frequency=case.plantFrequency or 1,size=1,richness=1}
    surface.map_gen_settings = settings
    local names = {"elevation", "aux", "moisture", "temperature"}
    for name in pairs(settings.autoplace_settings.tile.settings) do names[#names+1]="tile:"..name..":probability" end
    for _, name in ipairs({"yumako-tree","jellystem","sunnycomb","boompuff","stingfrond","lickmaw","teflilly","funneltrunk","slipstack","hairyclubnub","cuttlepop","water-cane","iron-stromatolite","copper-stromatolite","stone","gleba-spawner","gleba-spawner-small"}) do
      names[#names+1]="entity:"..name..":probability"
    end
    for _, name in ipairs({"entity:stone:richness","cliffiness","cliff_elevation","enemy_base_probability"}) do names[#names+1]=name end
    case.settings = settings
    local fields = surface.calculate_tile_properties(names, case.positions)
    for _, vs in pairs(fields) do
      for i,v in ipairs(vs) do
        if v ~= v or v == math.huge or v == -math.huge then error("Nonfinite oracle field") end
      end
    end
    case.fields = fields
    result.cases[#result.cases+1] = case
  end
  helpers.write_file("gleba-oracle.json", helpers.table_to_json(result))
  error("GLEBA-ORACLE-COMPLETE")
end)
''')
        config = work / "config.ini"
        config.write_text(f"[path]\nread-data={root / 'data'}\nwrite-data={work}\n[general]\ncheck-updates=false\n")
        run = subprocess.run([str(binary), "--config", str(config), "--mod-directory", str(work / "mods"), "--create", str(work / "oracle.zip"), "--map-gen-seed", "123456"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
        captured = work / "script-output/gleba-oracle.json"
        if not captured.exists() or "GLEBA-ORACLE-COMPLETE" not in run.stdout:
            raise SystemExit(run.stdout)
        result = json.loads(captured.read_text())
        result["factorioVersion"] = version
        output.write_text(json.dumps(result, separators=(",", ":")) + "\n")
        print(f"{version}: captured {len(cases)} cases to {output}")
        if "--previews" in sys.argv[3:]:
            (mod / "control.lua").write_text("")
            (mod / "data-final-fixes.lua").write_text('''
local mgs = data.raw.planet.gleba.map_gen_settings
mgs.cliff_settings.richness = 0
mgs.autoplace_settings.entity = {treat_missing_as_default=false, settings={}}
''')
            for seed, frequency, size, plant in [(123456, 1, 1, 1), (987654321, 0.5, 2, 3)]:
                settings = work / "map-gen.json"
                settings.write_text(json.dumps({"seed": seed, "cliff_settings": {"name": "cliff-gleba", "control": "gleba_cliff", "richness": 0}, "autoplace_controls": {
                    "gleba_water": {"frequency": frequency, "size": size, "richness": 1},
                    "gleba_plants": {"frequency": plant, "size": 1, "richness": 1},
                    "gleba_stone": {"frequency": 1, "size": 0, "richness": 1},
                    "gleba_cliff": {"frequency": 1, "size": 0, "richness": 0},
                }}))
                png = output.parent / f"oracle-preview-gleba-{release}.seed{seed}.png"
                run = subprocess.run([str(binary), "--config", str(config), "--mod-directory", str(work / "mods"), "--generate-map-preview", str(png), "--map-gen-settings", str(settings), "--map-preview-size", "512", "--map-preview-planet", "gleba"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
                if run.returncode != 0:
                    raise SystemExit(run.stdout)
                print(f"Captured {png}")


if __name__ == "__main__":
    main()
