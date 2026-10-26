#!/usr/bin/env python3
"""Capture enemy fields from an isolated Factorio 2.1 profile.

Usage: capture-enemy-oracle.py FACTORIO_ROOT OUTPUT_DIRECTORY [--previews]
Includes fractional distant positions and an integer grid near spawn, preserving
the point batch order needed to reproduce random_penalty's conditional stream.
"""
import json
import pathlib
import subprocess
import sys
import tempfile


def main():
    root, output = [pathlib.Path(v).resolve() for v in sys.argv[1:3]]
    binary = root / "bin/x64/factorio"
    version = subprocess.check_output([str(binary), "--version"], text=True).splitlines()[0]
    if not version.split()[1].startswith("2.1."):
        raise SystemExit(f"Expected Factorio 2.1, got {version}")
    repo = pathlib.Path(__file__).resolve().parent.parent
    points = json.loads((repo / "testdata/gleba/oracle-features-2.1.23.json").read_text())["cases"][0]["positions"][:1024]
    points += [[x, y] for y in range(-256, 257, 16) for x in range(-256, 257, 16)]
    with tempfile.TemporaryDirectory(prefix="factmapgen-enemy-") as tmp:
        work = pathlib.Path(tmp)
        mod = work / "mods/enemy-oracle_1.0.0"
        mod.mkdir(parents=True)
        (mod / "info.json").write_text(json.dumps(dict(name="enemy-oracle", version="1.0.0", title="Enemy oracle", author="FactMapGen", factorio_version="2.1", dependencies=["space-age"])))
        (work / "mods/mod-list.json").write_text(json.dumps({"mods": [{"name": n, "enabled": True} for n in ["base", "quality", "elevated-rails", "space-age", "enemy-oracle"]]}))
        (work / "config.ini").write_text(f"[path]\nread-data={root}/data\nwrite-data={work}\n[general]\ncheck-updates=false\n")
        (mod / "control.lua").write_text('''local points=helpers.json_to_table([==[''' + json.dumps(points) + ''']==])
script.on_init(function()
 local result={positions=points,cases={}}
 for _,planet in ipairs({"nauvis","gleba"}) do
  local surface=game.planets[planet].surface or game.planets[planet].create_surface()
  for _,seed in ipairs({123456,987654321}) do
   local s=surface.map_gen_settings;s.seed=seed;surface.map_gen_settings=s
   local names={"enemy_base_probability"}
   for _,n in ipairs(planet=="nauvis" and {"biter-spawner","spitter-spawner","small-worm-turret","medium-worm-turret","big-worm-turret","behemoth-worm-turret"} or {"gleba-spawner","gleba-spawner-small"}) do
    names[#names+1]="entity:"..n..":probability"
   end
   result.cases[#result.cases+1]={planet=planet,seed=seed,settings=s,fields=surface.calculate_tile_properties(names,points)}
  end
 end
 helpers.write_file("enemy-fields.json",helpers.table_to_json(result));error("ENEMY-ORACLE-COMPLETE")
end)''')
        run = subprocess.run([str(binary), "--config", str(work / "config.ini"), "--mod-directory", str(work / "mods"), "--create", str(work / "map.zip"), "--map-gen-seed", "123456"], capture_output=True, text=True, timeout=120)
        if "ENEMY-ORACLE-COMPLETE" not in run.stdout:
            raise SystemExit(run.stdout + run.stderr)
        result = json.loads((work / "script-output/enemy-fields.json").read_text())
        result["factorioVersion"] = version
        output.mkdir(parents=True, exist_ok=True)
        (output / "fields.json").write_text(json.dumps(result, separators=(",", ":")) + "\n")
        if "--previews" in sys.argv[3:]:
            # An independent seed, distant worms, and denser enemy controls.
            (mod / "control.lua").write_text('''script.on_init(function()
 local result={}
 for _,planet in ipairs({"nauvis","gleba"}) do
  local s=game.planets[planet].surface or game.planets[planet].create_surface()
  result[planet]=s.map_gen_settings
 end
 helpers.write_file("settings.json",helpers.table_to_json(result));error("SETTINGS-COMPLETE")
end)''')
            cmd = [str(binary), "--config", str(work / "config.ini"), "--mod-directory", str(work / "mods")]
            run = subprocess.run(cmd + ["--create", str(work / "map.zip"), "--map-gen-seed", "424242"], capture_output=True, text=True, timeout=120)
            if "SETTINGS-COMPLETE" not in run.stdout:
                raise SystemExit(run.stdout + run.stderr)
            defaults = json.loads((work / "script-output/settings.json").read_text())
            (mod / "control.lua").write_text("")
            previews = {"factorioVersion": version, "mapSeed": 424242, "size": 512, "cases": []}
            for planet in ["nauvis", "gleba"]:
                for variant, center in [("spawn", [0, 0]), ("distant", [4096, 0]), ("dense", [0, 0])]:
                    settings = json.loads(json.dumps(defaults[planet]))
                    request = {"seed": 424242}
                    if variant == "dense":
                        control = "enemy-base" if planet == "nauvis" else "gleba_enemy_base"
                        request["autoplace_controls"] = {control: {"frequency": 2, "size": 2, "richness": 1}}
                        settings["autoplace_controls"][control] = request["autoplace_controls"][control]
                    path = work / "preview.json"
                    path.write_text(json.dumps(request))
                    name = f"{planet}-{variant}.png"
                    run = subprocess.run(cmd + ["--generate-map-preview", str(output / name), "--map-gen-settings", str(path), "--map-preview-size", "512", "--map-preview-offset", ",".join(map(str, center)), "--map-preview-planet", planet], capture_output=True, text=True, timeout=120)
                    if run.returncode:
                        raise SystemExit(run.stdout + run.stderr)
                    padded = f"{planet}-{variant}-padded.png"
                    run = subprocess.run(cmd + ["--generate-map-preview", str(output / padded), "--map-gen-settings", str(path), "--map-preview-size", "640", "--map-preview-offset", ",".join(map(str, center)), "--map-preview-planet", planet], capture_output=True, text=True, timeout=120)
                    if run.returncode:
                        raise SystemExit(run.stdout + run.stderr)
                    previews["cases"].append(dict(planet=planet, variant=variant, center=center, settings=settings, image=name, paddedImage=padded))
                    print(f"Captured {name}", flush=True)
            (output / "previews.json").write_text(json.dumps(previews, separators=(",", ":")) + "\n")
    print(f"{version}: captured {output}")


if __name__ == "__main__":
    main()
