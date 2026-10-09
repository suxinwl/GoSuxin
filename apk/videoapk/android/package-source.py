"""Package the reproducible Android source without SDKs, builds or secrets."""
from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT.parent / "xiaoqi-android-source.zip"
SKIP = {".gradle", ".kotlin", "build", ".idea", "captures", "__pycache__"}
SECRET_NAMES = {"local.properties", "keystore.properties", "signing.properties", "signing.json"}
files = [p for p in ROOT.rglob("*") if p.is_file()
         and not set(p.relative_to(ROOT).parts).intersection(SKIP)
         and p.name not in SECRET_NAMES
         and p.suffix.lower() not in {".jks", ".keystore", ".p12", ".apk", ".log"}]
required = {ROOT / "gradlew.bat", ROOT / "gradle/wrapper/gradle-wrapper.jar",
            ROOT / "app-mobile/build.gradle.kts", ROOT / "app-tv/build.gradle.kts"}
if not required.issubset(files):
    raise SystemExit("Android wrapper or application source is missing")
with ZipFile(OUTPUT, "w", ZIP_DEFLATED, compresslevel=9) as archive:
    for p in sorted(files):
        archive.write(p, "android/" + p.relative_to(ROOT).as_posix())
with ZipFile(OUTPUT) as archive:
    if archive.testzip():
        raise SystemExit("Android source archive failed integrity check")
print(f"Created {OUTPUT} ({len(files)} source files)")
