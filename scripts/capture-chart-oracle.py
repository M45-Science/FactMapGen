#!/usr/bin/env python3
"""Isolate Factorio 2.1 chart drawing from natural placement noise.

Usage: capture-chart-oracle.py FACTORIO_ROOT OUTPUT_DIRECTORY
Cloned prototypes use fixed probability islands and empty collision masks so
world census positions also describe preview candidates, including overlaps.
All game writes are isolated in a temporary profile.
"""
import json
import pathlib
import subprocess
import sys
import tempfile

FIXES = '''
local m=data.raw.planet.nauvis.map_gen_settings
m.cliff_settings={name=""}
m.autoplace_settings={entity={treat_missing_as_default=false,settings={}},decorative={treat_missing_as_default=false,settings={}},tile={treat_missing_as_default=false,settings={["grass-1"]={}}}}
data.raw.tile["grass-1"].autoplace={probability_expression=1}
data.raw.tile["grass-1"].map_color={101,73,37}
local function add(kind,old,name,expr,suffix)
 local p=table.deepcopy(data.raw[kind][old]);p.name=name
 p.autoplace={order=(p.autoplace.order or "")..suffix,probability_expression=expr,richness_expression=100}
 p.collision_mask={layers={}}
 data:extend{p};m.autoplace_settings.entity.settings[name]={}
end
add("tree","tree-01","chart-tree", "(abs(y+24)<0.5)*((abs(x+33)<0.5)+(abs(x+17)<0.5)+(abs(x+1)<0.5)+(abs(x-15)<0.5)+(abs(x-31)<0.5))", "-0")
add("tree","tree-01","chart-tree-a", "(abs(y+8)<0.5)*((abs(x+16)<0.5)+(abs(x-16)<0.5))", "-1")
add("tree","tree-01","chart-tree-b", "(abs(y+8)<0.5)*((abs(x+16)<0.5)+(abs(x-16)<0.5))", "-2")
add("simple-entity","big-rock","chart-rock", "(abs(y-8)<0.5)*((abs(x+24)<0.5)+(abs(x+8)<0.5)+(abs(x-8)<0.5)+(abs(x-24)<0.5))+(abs(y+8)<0.5)*(abs(x+16)<0.5)", "")
add("resource","iron-ore","chart-ore", "(abs(y-24)<4)*(abs(x)<20)+(abs(y+8)<2)*(abs(x+16)<2)", "")
'''
CONTROL = '''script.on_init(function()
 local s=game.surfaces.nauvis;s.request_to_generate_chunks({0,0},3);s.force_generate_chunk_requests()
 local result={entities={},settings=s.map_gen_settings}
 for _,e in ipairs(s.find_entities_filtered{area={{-64,-64},{64,64}}}) do
  result.entities[#result.entities+1]={name=e.name,x=e.position.x,y=e.position.y}
 end
 helpers.write_file("census.json",helpers.table_to_json(result));error("CHART-COMPLETE")
end)'''


def main():
    root, output = [pathlib.Path(v).resolve() for v in sys.argv[1:3]]
    binary = root / "bin/x64/factorio"
    version = subprocess.check_output([str(binary), "--version"], text=True).splitlines()[0]
    if not version.split()[1].startswith("2.1."):
        raise SystemExit(f"Expected Factorio 2.1, got {version}")
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="factmapgen-chart-") as tmp:
        work = pathlib.Path(tmp)
        mod = work / "mods/chart-probe_1.0.0"
        mod.mkdir(parents=True)
        (mod / "info.json").write_text(json.dumps(dict(name="chart-probe", version="1.0.0", title="Chart probe", author="FactMapGen", factorio_version="2.1", dependencies=["space-age"])))
        (work / "mods/mod-list.json").write_text(json.dumps({"mods": [{"name": n, "enabled": True} for n in ["base", "quality", "elevated-rails", "space-age", "chart-probe"]]}))
        (work / "config.ini").write_text(f"[path]\nread-data={root}/data\nwrite-data={work}\n[general]\ncheck-updates=false\n")
        (mod / "data-final-fixes.lua").write_text(FIXES)
        (mod / "control.lua").write_text(CONTROL)
        cmd = [str(binary), "--config", str(work / "config.ini"), "--mod-directory", str(work / "mods")]
        run = subprocess.run(cmd + ["--create", str(work / "oracle.zip"), "--map-gen-seed", "123456"], capture_output=True, text=True, timeout=120)
        if "CHART-COMPLETE" not in run.stdout:
            raise SystemExit(run.stdout + run.stderr)
        census = json.loads((work / "script-output/census.json").read_text())
        census.update(factorioVersion=version, origin=[-64, -64], size=128, background=[101, 73, 37, 255])
        census["prototypes"] = {
            "chart-tree": {"order": "a[tree]-b[forest]-a[wet]-c[cold]-0", "box": [-.4, -.4, .4, .4], "color": [48, 99, 48, 102]},
            "chart-tree-a": {"order": "a[tree]-b[forest]-a[wet]-c[cold]-1", "box": [-.4, -.4, .4, .4], "color": [48, 99, 48, 102]},
            "chart-tree-b": {"order": "a[tree]-b[forest]-a[wet]-c[cold]-2", "box": [-.4, -.4, .4, .4], "color": [48, 99, 48, 102]},
            "chart-rock": {"order": "a[doodad]-a[rock]-b[big]", "box": [-1, -.9, 1, 1], "color": [129, 105, 78, 255]},
            "chart-ore": {"order": "b", "box": [-.1, -.1, .1, .1], "color": [105, 133, 147, 255], "dither": True},
        }
        (output / "census.json").write_text(json.dumps(census, separators=(",", ":")) + "\n")
        (mod / "control.lua").write_text("")
        (work / "map.json").write_text('{"seed":123456}')
        run = subprocess.run(cmd + ["--generate-map-preview", str(output / "chart.png"), "--map-gen-settings", str(work / "map.json"), "--map-preview-size", "128"], capture_output=True, text=True, timeout=120)
        if run.returncode:
            raise SystemExit(run.stdout + run.stderr)
    print(f"{version}: captured {output}")


if __name__ == "__main__":
    main()
