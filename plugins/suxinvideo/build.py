"""Build the GoFramePro code-package ZIP consumed by InstallLocalCode."""

from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile
import json
import re
import runpy

ROOT = Path(__file__).resolve().parent
HOST = ROOT.parent.parent
OUTPUT = HOST / "devsource" / "codemarket" / "release" / "suxinvideo.zip"


def main() -> None:
    runpy.run_path(str(ROOT / "tools" / "generate_menu.py"), run_name="__main__")
    config = (ROOT / "config.yml").read_text(encoding="utf-8")
    if not re.search(r"(?m)^\s*packtables:\s*['\"]?['\"]?\s*$", config):
        raise SystemExit("packtables must stay empty so uninstall preserves sx_* data")
    if not re.search(r"(?m)^\s*version:\s*2\.5\.0\s*$", config):
        raise SystemExit("expected plugin version 2.5.0")
    for name in ("LICENSE", "UPSTREAM.json", "package-lock.json", "adapter/server.mjs", "ADAPTER.md", "adapter/install-ubuntu.sh", "adapter/install-windows.ps1"):
        if not (ROOT / "go" / "third_party" / "akiralereal-iptv" / name).is_file():
            raise SystemExit(f"pinned IPTV corresponding source is missing: {name}")
    if '{"path":"/internal/liveruntime","isDir":true}' not in config:
        raise SystemExit("managed engine runtime is missing from goFiles")
    install_sql = (ROOT / "install.sql").read_text(encoding="utf-8")
    tables = re.findall(r"CREATE TABLE IF NOT EXISTS `?(sx_[a-z_]+)`?", install_sql)
    if len(tables) != 48 or len(set(tables)) != len(tables):
        raise SystemExit("expected 48 distinct CMS, native client and live business tables")
    for table in ("sx_live_group", "sx_live_channel", "sx_live_stream", "sx_live_subscription", "sx_live_job"):
        if table not in tables:
            raise SystemExit(f"live business table is missing: {table}")
    for name in ("live_schema.go", "live_catalog.go", "live_group_layout.go", "live_region_metadata.go", "live_import.go", "live_admin.go", "live_scheduler.go", "live_seed.go", "live_subscription_network.go", "live_media.go", "live_api.go", "live_site.go", "live_web.js", "live_web.css"):
        if not (ROOT / "go" / "internal" / "controller" / "suxinvideo" / name).is_file():
            raise SystemExit(f"live implementation is missing: {name}")
    if not (ROOT / "vue" / "src" / "views" / "suxinvideo" / "pages" / "live.vue").is_file():
        raise SystemExit("live administration page is missing")
    seed = install_sql.split("-- BEGIN PHP PLATFORM SEED", 1)
    if len(seed) != 2 or "-- END PHP PLATFORM SEED" not in seed[1]:
        raise SystemExit("PHP platform seed is missing")
    for name in ("banner.svg", "banner2.svg", "banner3.svg", "banner4.svg", "banner5.svg"):
        if not (ROOT / "suxinvideo" / "themes" / "suxinlite" / "static" / "img" / name).is_file():
            raise SystemExit(f"seed slide image is missing: {name}")
    json.loads((ROOT / "adminmenu.json").read_text(encoding="utf-8"))
    if '{"path":"/internal/erciyuan","isDir":true}' not in config:
        raise SystemExit("Erciyuan provider is missing from goFiles")
    for name in ("client.go", "client_test.go"):
        if not (ROOT / "go" / "internal" / "erciyuan" / name).is_file():
            raise SystemExit(f"Erciyuan provider is missing: {name}")
    # Both CMS proxies and the APP downloader import this shared package.
    # Listing it in goFiles is required for installation into a fresh host.
    if '{"path":"/internal/mediaplaylist","isDir":true}' not in config:
        raise SystemExit("shared media playlist module is missing from goFiles")
    for name in ("filter.go", "filter_test.go"):
        if not (ROOT / "go" / "internal" / "mediaplaylist" / name).is_file():
            raise SystemExit(f"shared media playlist module is missing: {name}")
    if '{"path":"/internal/mediastream","isDir":true}' not in config:
        raise SystemExit("native HLS engine is missing from goFiles")
    for name in ("hls.go", "hls_test.go", "command_windows.go", "command_other.go"):
        if not (ROOT / "go" / "internal" / "mediastream" / name).is_file():
            raise SystemExit(f"native HLS engine is missing: {name}")
    for code in ("suxinlite", "suxinpro", "iqiyi", "guoguo"):
        theme = ROOT / "suxinvideo" / "themes" / code
        manifest = json.loads((theme / "manifest.json").read_text(encoding="utf-8"))
        if manifest.get("code") != code:
            raise SystemExit(f"theme manifest code mismatch: {code}")
        for name in ("main.css", "main.js", "suxinplayer.js", "hls.js", "source-select.js", "source-discovery.js", "episode-next.js", "nopic.svg", "favicon.svg"):
            folder = "player" if name in ("suxinplayer.js", "hls.js", "source-select.js", "source-discovery.js", "episode-next.js") else "css" if name.endswith(".css") else "js" if name.endswith(".js") else "img"
            if not (theme / "static" / folder / name).is_file():
                raise SystemExit(f"theme asset is missing: {code}/{name}")
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    with ZipFile(OUTPUT, "w", ZIP_DEFLATED) as archive:
        for path in sorted(ROOT.rglob("*")):
            if not path.is_file() or path == OUTPUT or path.name == "build.py" or path == ROOT / "go" / "go.mod" or any(part in {"checkroutes","node_modules",".git","__pycache__",".cache","runtime","browser-profiles","Crashpad","Default"} for part in path.relative_to(ROOT).parts):
                continue
            if path.name.endswith((".log", ".pid", ".secret", ".jks", ".keystore")):
                continue
            if "tools" in path.parts and path != ROOT / "tools" / "live_seed" / "main.go":
                continue
            archive.write(path, Path("suxinvideo") / path.relative_to(ROOT))
    print(OUTPUT)


if __name__ == "__main__":
    main()
