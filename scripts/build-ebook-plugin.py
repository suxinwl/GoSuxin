"""Package the electronic album source plugin, excluding private data and tools."""
from pathlib import Path
import hashlib
import json
import zipfile

root = Path(__file__).resolve().parents[1]
paths = [
    'internal/addons/ebook', 'internal/router/plugin_ebook.go',
    'web/src/views/album', 'web/src/api/album.ts', 'web/src/utils/album-site.ts',
    'plugins/ebook', 'resource/plugins/ebook',
]
assert (root / 'resource/plugins/ebook/reader/index.html').is_file(), 'Build the reader first.'
files = {}
for name in paths:
    source = root / name
    for p in ([source] if source.is_file() else source.rglob('*')):
        if not p.is_file():
            continue
        rel = p.relative_to(root)
        if any(part in {'node_modules', '.git', 'dist', '__pycache__'} for part in rel.parts):
            continue
        if p.suffix in {'.log', '.exe', '.bak'} or p.name.endswith('.local'):
            continue
        files[rel.as_posix()] = p.read_bytes()
manifest = {
    'format': 'suxin-source-v1', 'name': 'ebook', 'title': 'Suxin 电子画册',
    'version': '1.0.0',
    'description': '分类管理、图片与 PDF 画册、翻页阅读、私密分享及独立的本地/123 云盘存储。',
    'files': {name: hashlib.sha256(data).hexdigest() for name, data in sorted(files.items())},
}
output = root / 'devsource/codemarket/release/ebook.zip'
output.parent.mkdir(parents=True, exist_ok=True)
with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as z:
    z.writestr('plugin.json', json.dumps(manifest, ensure_ascii=False, indent=2))
    for name, data in sorted(files.items()):
        z.writestr(name, data)
print(f'{output}: {len(files)} files, {output.stat().st_size} bytes')
