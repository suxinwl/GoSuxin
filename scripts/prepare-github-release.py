"""Archive committed sources and verified runtime packages for a GitHub release."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
PLUGINS = {'ebook': '1.0.0', 'suxinvideo': '2.5.0',
           'privatecode': '1.1.5', 'analysis': '1.0.0'}
SHARED = ('internal/addons/support/', 'internal/pluginworker/',
          'internal/runtimeplugin/', 'internal/plugins/', 'utility/httpstream/')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def selected(path, prefixes):
    return any(path == p.rstrip('/') or path.startswith(p) for p in prefixes)


def plugin_source(path, name):
    prefixes = SHARED + (f'plugins/{name}/', f'internal/router/plugin_{name}.go',
                         'web/src/utils/runtime-plugin.ts', 'docs/runtime-plugins.md',
                         'LICENSE', 'scripts/build-runtime-plugins.py')
    if name == 'ebook':
        prefixes += ('internal/addons/ebook/', 'web/src/views/album/',
                     'web/src/api/album.ts', 'web/src/utils/album-site.ts',
                     'resource/plugins/ebook/')
    elif name in ('privatecode', 'analysis'):
        prefixes += (f'internal/addons/{name}/', f'web/src/views/{name}/')
    else:
        prefixes += ('api/suxinvideo/', 'internal/controller/suxinvideo/',
                     'internal/erciyuan/', 'internal/yqksign/', 'internal/xiaoqiapp/',
                     'internal/mediaplaylist/', 'internal/mediastream/',
                     'internal/liveruntime/', 'internal/xiaoqiwebui/',
                     'third_party/akiralereal-iptv/', 'web/src/views/suxinvideo/',
                     'resource/static/suxinvideo/')
        if path.startswith(('plugins/suxinvideo/go/', 'plugins/suxinvideo/vue/')):
            return False  # Historical mirrors are not the current build sources.
        if path.startswith(('internal/dao/sx_', 'internal/dao/internal/sx_',
                            'internal/model/do/sx_', 'internal/model/entity/sx_')):
            return True
    return selected(path, prefixes)


def write_archive(path, files, manifest=None, modes=None):
    with zipfile.ZipFile(path, 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        if manifest:
            z.writestr('source.json', json.dumps(manifest, ensure_ascii=False, indent=2))
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, date_time=(2026, 10, 9, 0, 0, 0))
            info.create_system = 3
            info.external_attr = ((modes or {}).get(name, 0o100644)) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data, compresslevel=6)
    with zipfile.ZipFile(path) as z:
        if z.testzip() is not None:
            raise ValueError(f'ZIP integrity check failed: {path.name}')


def verify_runtime(path, name, version, system):
    with zipfile.ZipFile(path) as z:
        m = json.loads(z.read('plugin.json'))
        assert (m['format'], m['name'], m['version'], m['os'], m['arch'], m['protocol']) == (
            'suxin-runtime-v1', name, version, system, 'amd64', 1), path.name
        assert set(z.namelist()) == {'plugin.json', *m['files']}, path.name
        for member, digest in m['files'].items():
            parts = PurePosixPath(member).parts
            assert not member.startswith('/') and '..' not in parts and '\\' not in member
            assert sha(z.read(member)) == digest, f'{path.name}: {member}'
    return len(m['files'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--commit', default='HEAD')
    parser.add_argument('--tag', default='v1.0.0-runtime.20261009')
    parser.add_argument('--output', type=Path, default=ROOT/'runtime/github-release-20261009')
    args = parser.parse_args()
    commit = subprocess.check_output(['git', 'rev-parse', args.commit], cwd=ROOT,
                                     text=True).strip()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    full = out/f'GoSuxin-{args.tag}-source.zip'
    subprocess.run(['git', 'archive', '--format=zip', '--prefix=GoSuxin/',
                    f'--output={full}', commit], cwd=ROOT, check=True)
    with zipfile.ZipFile(full) as z:
        assert z.testzip() is None
        sources = {n.removeprefix('GoSuxin/'): z.read(n)
                   for n in z.namelist() if not n.endswith('/')}
        source_modes = {i.filename.removeprefix('GoSuxin/'): i.external_attr >> 16
                        for i in z.infolist() if not i.is_dir()}
    records = []

    def record(path, kind, **extra):
        r = dict(file=path.name, kind=kind, bytes=path.stat().st_size,
                 sha256=sha(path.read_bytes()), sourceCommit=commit, **extra)
        records.append(r)
        print(json.dumps(r, ensure_ascii=False), flush=True)

    record(full, 'repository-source')
    for name, version in PLUGINS.items():
        files = {p: data for p, data in sources.items() if plugin_source(p, name)}
        note = (f'{name} {version} 当前主线源码，提交 {commit}\n'
                '在同一提交的完整 GoSuxin 源码中开发和编译；共用框架及模块由完整仓库提供。\n'
                '本 ZIP 供开发机使用，不作为服务器安装包。服务器请安装对应系统的运行 ZIP。\n'
                '逐文件 SHA-256 见 source.json；构建说明见 docs/runtime-plugins.md。\n')
        files['SOURCE-README.txt'] = note.encode()
        m = dict(format='suxin-source-archive-v1', name=name, version=version,
                 sourceCommit=commit, runtimeFormat='suxin-runtime-v1',
                 requiresFullRepository=True,
                 files={p: sha(data) for p, data in sorted(files.items())})
        path = out/f'{name}-{version}-source-20261009.zip'
        write_archive(path, files, m, source_modes)
        record(path, 'plugin-source', name=name, version=version, files=len(files))

    android = {p.removeprefix('apk/videoapk/android/'): data
               for p, data in sources.items() if p.startswith('apk/videoapk/android/')}
    assert b'versionName = "1.0.16"' in android['app-mobile/build.gradle.kts']
    assert b'versionName = "1.0.16"' in android['app-tv/build.gradle.kts']
    path = out/'xiaoqi-android-1.0.16-source.zip'
    manifest = dict(format='android-source-archive-v1', version='1.0.16',
                    sourceCommit=commit, files={p: sha(data) for p, data in sorted(android.items())})
    write_archive(path, android, manifest, {'gradlew': 0o100755})
    record(path, 'android-source', name='xiaoqi_video_app', version='1.0.16', files=len(android))

    for system in ('windows', 'linux'):
        built = ROOT/'devsource/codemarket/runtime-release'/f'{system}-amd64'
        expected = {r['name']: r for r in json.loads((built/'packages.json').read_text(encoding='utf-8'))}
        for name, version in PLUGINS.items():
            source = built/f'{name}-{version}-{system}-amd64.zip'
            assert sha(source.read_bytes()) == expected[name]['sha256'], source.name
            count = verify_runtime(source, name, version, system)
            target = out/source.name
            shutil.copy2(source, target)
            record(target, 'plugin-runtime', name=name, version=version,
                   platform=f'{system}/amd64', files=count)
        exe = 'gosuxin.exe' if system == 'windows' else 'gosuxin'
        binary = built/exe
        assert binary.is_file(), binary
        host = {p: data for p, data in sources.items()
                if selected(p, ('resource/suxinweb/', 'resource/plugins/ebook/',
                                'resource/static/suxinvideo/', 'manifest/sql/',
                                'manifest/codeinstall/', 'docs/runtime-plugins.md',
                                'docs/changes/2026-10-09.md', 'scripts/init-local-config.py',
                                'hack/config.example.yaml',
                                'third_party/akiralereal-iptv/', 'framework/LICENSE',
                                'devsource/developer/install/', 'README.MD', 'LICENSE'))
                or (p.startswith('manifest/config/') and '.example.' in p)}
        host[exe] = binary.read_bytes()
        if system == 'windows':
            ffmpeg = ROOT/'resource/static/suxinvideo/bin/windows-amd64/ffmpeg.exe'
            if ffmpeg.is_file():
                host['resource/static/suxinvideo/bin/windows-amd64/ffmpeg.exe'] = ffmpeg.read_bytes()
        host['DEPLOY-README.txt'] = (
            f'GoSuxin {args.tag}, {system}/amd64, source {commit}\n'
            '新部署：python scripts/init-local-config.py，填写数据库配置，再启动宿主并完成后台安装。\n'
            '已有部署：在独立目录解压，仅替换宿主与静态资源，保留原配置、数据库、storage、data、上传文件和插件状态。\n'
            '首次升级需重启宿主一次，之后四个插件运行包安装/更新/卸载不重启宿主。\n'
            '工作目录设为部署根目录；Linux 执行 chmod +x gosuxin 后运行 ./gosuxin。\n'
            'PDF 另需 Poppler；Linux 影视转码另需系统 FFmpeg。详情见 docs/runtime-plugins.md。\n'
            'Linux 包完成交叉编译及校验，尚未进行 Linux 实机运行验收。\n').encode()
        host_path = out/f'GoSuxin-{args.tag}-{system}-amd64.zip'
        write_archive(host_path, host, modes={exe: 0o100755})
        record(host_path, 'host-runtime', platform=f'{system}/amd64', files=len(host))

    index = out/'packages.json'
    index.write_text(json.dumps(dict(tag=args.tag, sourceCommit=commit, packages=records),
                                ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    sums = [(r['sha256'], r['file']) for r in records]
    sums.append((sha(index.read_bytes()), index.name))
    (out/'SHA256SUMS.txt').write_text(''.join(f'{h}  {name}\n' for h, name in sorted(sums, key=lambda x:x[1])),
                                    encoding='utf-8')
    print(f'Ready: {len(records)} archives, package index and SHA-256 list in {out}', flush=True)


if __name__ == '__main__':
    main()
