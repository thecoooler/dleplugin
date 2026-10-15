#!/usr/bin/env python3
"""Build release archives with checksums and complete license bundles."""
import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from pathlib import Path

PLATFORMS = [os_name + "/" + arch for os_name in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version, for example v0.1.0")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    parser.add_argument("--platform", choices=PLATFORMS, action="append")
    args = parser.parse_args()
    if not re.fullmatch(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?", args.version):
        parser.error("version must be a v-prefixed semantic release version")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    artifacts = []
    with tempfile.TemporaryDirectory(prefix="dleplugin-release-") as tmp:
        workspace = Path(tmp)
        licenses = workspace / "licenses"
        subprocess.run([sys.executable, str(ROOT / "scripts/licenses.py"), str(licenses)], cwd=ROOT, check=True)
        for platform in dict.fromkeys(args.platform or PLATFORMS):
            goos, goarch = platform.split("/")
            name = "dleplugin_" + args.version + "_" + goos + "_" + goarch
            folder = workspace / name
            folder.mkdir()
            binary = folder / ("dleplugin.exe" if goos == "windows" else "dleplugin")
            env = dict(os.environ, CGO_ENABLED="0", GOOS=goos, GOARCH=goarch)
            subprocess.run(["go", "build", "-trimpath", "-ldflags", "-s -w -X github.com/thecoooler/dleplugin/internal/app.Version=" + args.version, "-o", str(binary), "./cmd/dleplugin"], cwd=ROOT, env=env, check=True)
            subprocess.run(["go", "run", "golang.org/x/vuln/cmd/govulncheck@v1.9.0", "-mode=binary", str(binary)], cwd=ROOT, check=True)
            for filename in ("LICENSE", "README.md", "THIRD_PARTY_NOTICES.md"):
                shutil.copyfile(ROOT / filename, folder / filename)
            shutil.copytree(licenses, folder / "licenses")
            if goos == "windows":
                target = output / (name + ".zip")
                with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
                    for path in sorted(folder.rglob("*")):
                        if path.is_file():
                            archive.write(path, path.relative_to(workspace).as_posix())
            else:
                target = output / (name + ".tar.gz")
                with tarfile.open(target, "w:gz") as archive:
                    archive.add(folder, arcname=name)
            artifacts.append(target)
            print(target.name, flush=True)
    checksums = [hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name for path in artifacts]
    (output / "SHA256SUMS").write_text("\n".join(checksums) + "\n")


if __name__ == "__main__":
    main()
