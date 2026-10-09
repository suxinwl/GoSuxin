"""Package adapted add-ons without demo data or private files."""
from pathlib import Path
import hashlib, json, zipfile

root = Path(__file__).resolve().parents[1]
for name, title, version in [('privatecode', '私有插件仓', '1.1.5'), ('analysis', '统计分析', '1.0.0')]:
    paths = [f'internal/addons/{name}', 'internal/addons/support', f'internal/router/plugin_{name}.go', f'web/src/views/{name}', f'plugins/{name}']
    files = {}
    for relative in paths:
        source = root / relative
        for p in ([source] if source.is_file() else source.rglob('*')):
            if p.is_file() and not any(part in {'.git','node_modules','__pycache__'} for part in p.parts):
                assert p.suffix not in {'.log','.exe','.local','.bak'}
                files[p.relative_to(root).as_posix()] = p.read_bytes()
    manifest = {'format':'suxin-source-v1','name':name,'title':title,'version':version,
                'description':'Gosuxin 原生源码插件，仅基础数据。',
                'files':{path:hashlib.sha256(data).hexdigest() for path,data in sorted(files.items())}}
    output = root / f'devsource/codemarket/release/{name}.zip'
    output.parent.mkdir(parents=True,exist_ok=True)
    with zipfile.ZipFile(output,'w',zipfile.ZIP_DEFLATED) as z:
        z.writestr('plugin.json',json.dumps(manifest,ensure_ascii=False,indent=2))
        for path,data in sorted(files.items()): z.writestr(path,data)
    with zipfile.ZipFile(output) as z: assert z.testzip() is None
    print(f'{name}: {len(files)} files, {output.stat().st_size} bytes')
