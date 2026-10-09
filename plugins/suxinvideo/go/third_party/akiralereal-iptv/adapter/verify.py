"""Verify untouched upstream and mirror only explicitly recorded public source files."""
import hashlib
import json
from pathlib import Path
import shutil

ROOT = Path(__file__).resolve().parent.parent
HOST = ROOT.parent.parent
MIRROR = HOST / "plugins/suxinvideo/go/third_party/akiralereal-iptv"


def main():
    manifest = json.loads((ROOT / "UPSTREAM.json").read_text(encoding="utf-8"))
    recorded = manifest["upstream_files_sha256"]
    for name, digest in recorded.items():
        path = ROOT / name
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise SystemExit(f"Upstream file changed: {name}")
    files = [ROOT / name for name in recorded]
    files += [ROOT / "UPSTREAM.json", ROOT / "ADAPTER.md"]
    files += [path for path in (ROOT / "adapter").rglob("*") if path.is_file() and "__pycache__" not in path.parts]
    for path in files:
        relative = path.relative_to(ROOT)
        if any(part in ("node_modules", "data", ".runtime", "__pycache__") for part in relative.parts):
            raise SystemExit(f"Private path cannot be mirrored: {relative}")
        target = MIRROR / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, target)
    print(f"Verified {len(recorded)} untouched upstream files; mirrored {len(files)} source files")


if __name__ == "__main__":
    main()
