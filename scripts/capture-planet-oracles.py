#!/usr/bin/env python3
"""Capture all five planets from an isolated Factorio 2.1 installation.

Usage: capture-planet-oracles.py FACTORIO_ROOT OUTPUT_DIRECTORY [--previews]
The binary and data are read-only; all game state goes in a temporary directory.
The manifest records the exact binary version, surface seeds and map settings.
"""
import json
import pathlib
import subprocess
import sys
import tempfile

PLANETS = ["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"]


def main():
    root, output = map(lambda s: pathlib.Path(s).resolve(), sys.argv[1:3])
    binary = root / "bin/x64/factorio"
    version = subprocess.check_output([str(binary), "--version"], text=True).splitlines()[0]
    if not version.split()[1].startswith("2.1."):
        raise SystemExit(f"Expected Factorio 2.1, got {version}")
    output.mkdir(parents=True, exist_ok=True)
    positions = [[x, y] for y in range(-256, 257, 16) for x in range(-256, 257, 16)]
    with tempfile.TemporaryDirectory(prefix="factmapgen-planets-") as tmp:
        work = pathlib.Path(tmp)
        mod = work / "mods/planet-oracle_1.0.0"
        mod.mkdir(parents=True)
        (mod / "info.json").write_text(json.dumps(dict(name="planet-oracle", version="1.0.0", title="Planet oracle", author="FactMapGen", factorio_version="2.1", dependencies=["space-age"])))
        (work / "mods/mod-list.json").write_text(json.dumps({"mods": [{"name": n, "enabled": True} for n in ["base", "quality", "elevated-rails", "space-age", "planet-oracle"]]}))
        config = work / "config.ini"
        config.write_text(f"[path]\nread-data={root / 'data'}\nwrite-data={work}\n[general]\ncheck-updates=false\n")
        command = [str(binary), "--config", str(config), "--mod-directory", str(work / "mods")]
        (mod / "control.lua").write_text('''local positions=helpers.json_to_table([==[''' + json.dumps(positions) + ''']==])
script.on_init(function()
  local result={positions=positions,cases={}}
  for _,name in ipairs({"nauvis","vulcanus","gleba","fulgora","aquilo"}) do
    local s=game.planets[name].surface or game.planets[name].create_surface()
    local original=s.map_gen_settings
    local settings=s.map_gen_settings
    s.request_to_generate_chunks({0,0},8)
    s.force_generate_chunk_requests()
    local entities={}
    for _,e in ipairs(s.find_entities_filtered{area={{-256,-256},{256,256}}}) do
      if e.type~="cliff" and e.position.x>=-256 and e.position.x<256 and e.position.y>=-256 and e.position.y<256 then
        entities[#entities+1]={name=e.name,x=e.position.x,y=e.position.y}
      end
    end
    local fields=s.calculate_tile_properties({"elevation","cliff_elevation","cliffiness"},positions)
    -- Isolate cliffs from entity collision/removal for the placement oracle.
    settings.autoplace_settings.entity={treat_missing_as_default=false,settings={}}
    s=game.create_surface("oracle-"..name,settings)
    s.request_to_generate_chunks({0,0},8)
    s.force_generate_chunk_requests()
    local cliffs={}
    for _,e in ipairs(s.find_entities_filtered{type="cliff",area={{-256,-256},{256,256}}}) do
      if e.position.x>=-256 and e.position.x<256 and e.position.y>=-256 and e.position.y<256 then
        cliffs[#cliffs+1]={x=e.position.x,y=e.position.y,orientation=e.cliff_orientation}
      end
    end
    result.cases[#result.cases+1]={planet=name,seed=settings.seed,settings=original,fields=fields,cliffs=cliffs,entities=entities}
  end
  helpers.write_file("planet-oracle.json",helpers.table_to_json(result))
  error("PLANET-ORACLE-COMPLETE")
end)
''')
        for seed in [123456, 987654321]:
            run = subprocess.run(command + ["--create", str(work / "oracle.zip"), "--map-gen-seed", str(seed)], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=180)
            captured = work / "script-output/planet-oracle.json"
            if not captured.exists() or "PLANET-ORACLE-COMPLETE" not in run.stdout:
                raise SystemExit(run.stdout)
            result = json.loads(captured.read_text())
            result.update(factorioVersion=version, mapSeed=seed)
            (output / f"fields.seed{seed}.json").write_text(json.dumps(result, separators=(",", ":")) + "\n")
            print(f"{version}: captured five planets, seed {seed}", flush=True)
        if "--previews" in sys.argv[3:]:
            (mod / "control.lua").write_text("")
            for layer in ["terrain", "cliffs", "full"]:
                fixes = ""
                if layer != "full":
                    fixes = '''for _,p in pairs(data.raw.planet) do
  local m=p.map_gen_settings
  if m then
    m.autoplace_settings=m.autoplace_settings or {}
    m.autoplace_settings.entity={treat_missing_as_default=false,settings={}}
'''
                    if layer == "terrain":
                        fixes += '    m.cliff_settings=m.cliff_settings or {}; m.cliff_settings.name=""\n'
                    fixes += "  end\nend\n"
                (mod / "data-final-fixes.lua").write_text(fixes)
                for planet in PLANETS:
                    for seed in [123456, 987654321]:
                        png = output / f"{planet}-{layer}.seed{seed}.png"
                        settings = {"seed": seed}
                        if layer != "full":
                            settings["autoplace_settings"] = {"entity": {"treat_missing_as_default": False, "settings": {}}}
                        if layer == "terrain":
                            settings["cliff_settings"] = {"name": ""}
                        path = work / "map-gen.json"
                        path.write_text(json.dumps(settings))
                        run = subprocess.run(command + ["--generate-map-preview", str(png), "--map-gen-settings", str(path), "--map-preview-size", "512", "--map-preview-planet", planet], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
                        if run.returncode:
                            raise SystemExit(run.stdout)
                        print(f"Captured {png.name}", flush=True)


if __name__ == "__main__":
    main()
