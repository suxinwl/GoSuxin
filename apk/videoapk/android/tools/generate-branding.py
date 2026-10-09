"""Resize the supplied brand artwork into native Android resources.

Run from any directory: python android/tools/generate-branding.py
The bundled original keeps the Android source archive reproducible without the CMS.
Requires Pillow; this script only scales/composites the original, never redraws it.
"""

from pathlib import Path
import argparse
import shutil

from PIL import Image, ImageDraw, ImageFont


ANDROID = Path(__file__).resolve().parents[1]
DENSITIES = {"mdpi": 48, "hdpi": 72, "xhdpi": 96, "xxhdpi": 144, "xxxhdpi": 192}


def art_on_canvas(art: Image.Image, size: int, content: int, *, white: bool = True) -> Image.Image:
    canvas = Image.new("RGBA", (size, size), "white" if white else (0, 0, 0, 0))
    scaled = art.copy()
    scaled.thumbnail((content, content), Image.Resampling.LANCZOS)
    canvas.alpha_composite(scaled, ((size - scaled.width) // 2, (size - scaled.height) // 2))
    return canvas


def save(image: Image.Image, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path, optimize=True)


def font(size: int) -> ImageFont.FreeTypeFont:
    candidates = [
        Path("C:/Windows/Fonts/msyh.ttc"),
        Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
        Path("/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"),
    ]
    for path in candidates:
        if path.is_file():
            return ImageFont.truetype(str(path), size)
    raise SystemExit("A Chinese font is required for the TV banner (Microsoft YaHei or Noto Sans CJK).")


def banner(art: Image.Image, tv: bool) -> Image.Image:
    image = Image.new("RGBA", (320, 180), "#0B1118")
    image.alpha_composite(art_on_canvas(art, 132, 124), (12, 24))
    drawing = ImageDraw.Draw(image)
    drawing.text((155, 51), "小柒影视", fill="white", font=font(28))
    drawing.text((155, 101), "TV · Android" if tv else "手机 / 平板", fill="#E5C37A", font=font(17))
    return image


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path)
    options = parser.parse_args()
    bundled = ANDROID / "branding" / "xiaoqilogo.png"
    cms_master = ANDROID.parent / "xiaoqilogo.png"
    source = options.source or (cms_master if cms_master.is_file() else bundled)
    if not source.is_file():
        raise SystemExit(f"Brand master does not exist: {source}")
    bundled.parent.mkdir(parents=True, exist_ok=True)
    if source.resolve() != bundled.resolve():
        shutil.copyfile(source, bundled)
    art = Image.open(bundled).convert("RGBA")
    design = ANDROID / "core/design/src/main/res/drawable-nodpi"
    save(art_on_canvas(art, 256, 240), design / "xiaoqi_logo.png")
    save(art_on_canvas(art, 256, 240), design / "default_avatar.png")

    for name in ("app-mobile", "app-tv"):
        res = ANDROID / name / "src/main/res"
        for density, size in DENSITIES.items():
            legacy = art_on_canvas(art, size, round(size * .86))
            save(legacy, res / f"mipmap-{density}/ic_launcher.png")
            circle = Image.new("L", (size, size), 0)
            ImageDraw.Draw(circle).ellipse((0, 0, size - 1, size - 1), fill=255)
            rounded = legacy.copy()
            rounded.putalpha(circle)
            save(rounded, res / f"mipmap-{density}/ic_launcher_round.png")
        # The complete foreground artwork stays inside the central 66 dp safe area.
        # 58 dp content on the 108 dp adaptive canvas retains the logo on any launcher mask.
        save(art_on_canvas(art, 432, 232, white=False), res / "drawable-nodpi/ic_launcher_foreground.png")
        save(banner(art, name == "app-tv"), res / "drawable-xhdpi/tv_banner.png")
        # TV banners must be 320 x 180 px at xhdpi, not a screen-size-scaled bitmap.
        (res / "drawable/tv_banner.xml").unlink(missing_ok=True)
        for code in ("ic_launcher", "ic_launcher_round"):
            path = res / f"mipmap-anydpi-v26/{code}.xml"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n'
                            '    <background android:drawable="@drawable/ic_launcher_background" />\n'
                            '    <foreground android:drawable="@drawable/ic_launcher_foreground" />\n'
                            '</adaptive-icon>\n', encoding="utf-8")
    print("Generated Android branding for mobile/tablet and TV from the supplied original.")


if __name__ == "__main__":
    main()
