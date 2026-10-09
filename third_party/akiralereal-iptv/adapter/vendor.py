"""Fetch and verify the exact upstream source; never copy local runtime data."""
import hashlib
import io
import json
from pathlib import Path
import urllib.request
import zipfile

COMMIT = "7b1e2d5fdc6dbe35de63f099be6264a201c6a975"
ROOT = Path(__file__).resolve().parent.parent
URL = f"https://codeload.github.com/akiralereal/iptv/zip/{COMMIT}"


def main():
    request = urllib.request.Request(URL, headers={"User-Agent": "Xiaoqi-IPTV-vendor"})
    with urllib.request.urlopen(request, timeout=120) as response:
        archive_bytes = response.read()
    files = {}
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as archive:
        for entry in archive.infolist():
            parts = Path(entry.filename).parts[1:]
            if entry.is_dir() or not parts:
                continue
            if any(part in ("..", ".git", "node_modules") for part in parts):
                raise RuntimeError(f"unsafe upstream path: {entry.filename}")
            relative = Path(*parts)
            target = (ROOT / relative).resolve()
            if not target.is_relative_to(ROOT.resolve()) or parts[0] == "adapter":
                raise RuntimeError(f"unexpected upstream path: {entry.filename}")
            body = archive.read(entry)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(body)
            files[relative.as_posix()] = hashlib.sha256(body).hexdigest()
    manifest = {
        "repository": "https://github.com/akiralereal/iptv",
        "version": "4.27.0",
        "commit": COMMIT,
        "license": "GPL-3.0-only",
        "archive_sha256": hashlib.sha256(archive_bytes).hexdigest(),
        "upstream_files_sha256": files,
    }
    (ROOT / "UPSTREAM.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Vendored {len(files)} original files at {COMMIT}")


if __name__ == "__main__":
    main()
