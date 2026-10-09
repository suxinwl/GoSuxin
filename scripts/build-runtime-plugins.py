"""Build immutable hot-install packages on a development machine, never on a server."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
PLUGINS = {
    'ebook': ('电子画册', '1.0.0', '/albums-admin/manage'),
    'suxinvideo': ('影视CMS', '2.5.0', '/suxinvideo/admin'),
    'privatecode': ('私有插件仓', '1.1.5', '/privatecode'),
    'analysis': ('统计分析', '1.0.0', '/analysis'),
}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', choices=['windows', 'linux'], default='windows')
    parser.add_argument('--arch', choices=['amd64', 'arm64'], default='amd64')
    parser.add_argument('--plugin', choices=list(PLUGINS), action='append')
    parser.add_argument('--build-web', action='store_true')
    parser.add_argument('--build-host', action='store_true')
    args = parser.parse_args()
    os.chdir(ROOT)
    env = os.environ.copy()
    env.update(GOOS=args.os, GOARCH=args.arch, CGO_ENABLED='0')
    workspace = ROOT / 'runtime/local.go.work'
    if workspace.exists():
        env['GOWORK'] = str(workspace)
    if args.build_web:
        subprocess.run(['npm.cmd' if os.name == 'nt' else 'npm', 'run', 'build'], cwd=ROOT/'web', check=True)
    frontend = ROOT/'web/dist'
    if not (frontend/'index.html').exists():
        raise SystemExit('先运行 npm run build，或添加 --build-web')
    output = ROOT/'devsource/codemarket/runtime-release'/f'{args.os}-{args.arch}'
    output.mkdir(parents=True, exist_ok=True)
    ext = '.exe' if args.os == 'windows' else ''
    if args.build_host:
        subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-o', str(output/('gosuxin'+ext)), '.'], env=env, check=True)
    report = []
    for name in args.plugin or PLUGINS:
        title, version, entry = PLUGINS[name]
        binary = output/(name+ext)
        subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-ldflags=-s -w', '-o', str(binary), f'./plugins/{name}/cmd'], env=env, check=True)
        files = {'bin/'+binary.name: binary.read_bytes()}
        for source in frontend.rglob('*'):
            if not source.is_file() or source.suffix == '.gz':
                continue
            data = source.read_bytes()
            if source.suffix in {'.html', '.js', '.css', '.json', '.svg'}:
                data = data.replace(b'/suxinweb/', f'/plugins/{name}/admin/'.encode())
            if source.name == 'config.js':
                data += f'\nwindow.globalConfig.RouterHome={json.dumps(entry.lstrip("/"))};\n'.encode()
            files['admin/'+source.relative_to(frontend).as_posix()] = data
        public = ROOT/'resource/plugins/ebook/reader' if name == 'ebook' else ROOT/'resource/static/suxinvideo' if name == 'suxinvideo' else None
        if public and public.exists():
            for source in public.rglob('*'):
                if source.is_file():
                    prefix = 'public/reader/' if name == 'ebook' else 'public/suxinvideo/'
                    files[prefix+source.relative_to(public).as_posix()] = source.read_bytes()
        files['README.md'] = (f'{title} {version} 独立运行包\n平台：{args.os}/{args.arch}\n'
            '后台插件市场上传本 ZIP 即可安装或更新。无需 Go/Node 编译器，无需重启主服务。\n'
            '卸载保留运行包、数据库、文件与权限。重新安装恢复。更新启动失败自动回退。\n'
            '插件进程更新会中断该插件正在处理的请求，请避开上传与长任务。\n'
            '画册 PDF 转换另需 Poppler。保留主程序的数据库和业务文件目录。\n').encode()
        manifest = dict(format='suxin-runtime-v1', name=name, title=title, version=version,
                        protocol=1, os=args.os, arch=args.arch,
                        files={p: hashlib.sha256(data).hexdigest() for p, data in files.items()})
        archive = output/f'{name}-{version}-{args.os}-{args.arch}.zip'
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as z:
            z.writestr('plugin.json', json.dumps(manifest, ensure_ascii=False, indent=2))
            for path, data in files.items():
                z.writestr(path, data)
        result = dict(name=name, version=version, platform=f'{args.os}/{args.arch}',
                      path=str(archive.relative_to(ROOT)), bytes=archive.stat().st_size,
                      sha256=hashlib.sha256(archive.read_bytes()).hexdigest(), files=len(files))
        report.append(result)
        print(json.dumps(result, ensure_ascii=False), flush=True)
    index = output/'packages.json'
    if args.plugin and index.exists():
        updated = {item['name'] for item in report}
        report += [item for item in json.loads(index.read_text(encoding='utf-8')) if item['name'] not in updated]
        report.sort(key=lambda item: list(PLUGINS).index(item['name']))
    index.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')

if __name__ == '__main__':
    main()
