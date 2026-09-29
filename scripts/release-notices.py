"""Collect notices from the locked dependencies already installed locally."""
import json
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[1]
parts = [("Tongxi third-party notices", (root / "THIRD_PARTY_NOTICES.md").read_text())]

def collect(name, directory):
    if not directory:
        return
    for pattern in ("LICENSE*", "LICENCE*", "NOTICE*", "COPYING*", "license*", "licence*", "notice*", "copying*"):
        for path in sorted(pathlib.Path(directory).glob(pattern)):
            if path.is_file():
                parts.append((f"{name} / {path.name}", path.read_text(errors="replace")))

decoder = json.JSONDecoder()
modules = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=root, text=True)
while modules.strip():
    module, end = decoder.raw_decode(modules.lstrip())
    modules = modules.lstrip()[end:]
    if not module.get("Main"):
        collect(module["Path"] + " " + module.get("Version", ""), module.get("Dir"))
collect("Go runtime", subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
lock = json.loads((root / "frontend/package-lock.json").read_text())
for location, package in lock["packages"].items():
    if location and not package.get("dev"):
        collect(location + " " + package.get("version", ""), root / "frontend" / location)
pathlib.Path(sys.argv[1]).write_text("\n\n".join(f"=== {name} ===\n{body}" for name, body in parts))
