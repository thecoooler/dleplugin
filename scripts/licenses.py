#!/usr/bin/env python3
"""Collect license/notice texts for offline release distribution."""
import argparse
import json
import subprocess
import tempfile
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    output = parser.parse_args().output
    raw = subprocess.check_output(["go", "list", "-mod=readonly", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    modules = []
    while raw.strip():
        raw = raw.lstrip()
        item, end = decoder.raw_decode(raw)
        modules.append(item)
        raw = raw[end:]
    goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
    output.mkdir(parents=True, exist_ok=True)
    (output / "Go-LICENSE").write_bytes((goroot / "LICENSE").read_bytes())
    index = []
    for module in modules:
        if module.get("Main"):
            continue
        if not module.get("Dir"):
            with tempfile.TemporaryDirectory(prefix="dleplugin-licenses-") as cache_work:
                download = subprocess.check_output(
                    [str(goroot / "bin" / "go"), "mod", "download", "-json", module["Path"] + "@" + module["Version"]],
                    cwd=cache_work, text=True,
                )
            module["Dir"] = json.loads(download)["Dir"]
        root = Path(module["Dir"])
        files = [p for p in root.iterdir() if p.is_file() and p.name.lower().startswith(("license", "copying", "notice"))]
        if not files:
            raise SystemExit("Missing license text: " + module["Path"])
        target = output / module["Path"].replace("/", "__")
        target.mkdir(exist_ok=True)
        for source in files:
            (target / source.name).write_bytes(source.read_bytes())
        index.append({"module": module["Path"], "version": module["Version"], "files": sorted(p.name for p in files)})
    (output / "index.json").write_text(json.dumps(index, indent=2) + "\n")


if __name__ == "__main__":
    main()
