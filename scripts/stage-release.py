"""Create a fresh, sanitized release tree. Never modifies the original Git history."""
from pathlib import Path
import shutil,zipfile,re,json
root=Path(__file__).resolve().parents[1];stage=root/'runtime/release-source-final'
if stage.exists():raise SystemExit('Release tree already exists; inspect it before rebuilding.')
stage.mkdir(parents=True)
skip={'.git','.github','node_modules','dist','runtime','.idea','.vscode','__pycache__'}
private={Path(x) for x in ['manifest/config/config.yaml','manifest/config/app.yaml','manifest/config/upload.yaml','hack/config.yaml','devsource/developer/install/install.lock','devsource/developer/install/webcode.zip']}
roots=['api','internal','utility','framework','web','manifest','devsource','hack','docs','scripts']
files=['README.MD','LICENSE','.gitignore','.gitattributes','go.mod','go.sum','go.work','main.go','Makefile']
for name in roots:
 for p in (root/name).rglob('*'):
  if not p.is_file():continue
  rel=p.relative_to(root)
  if any(x in skip for x in rel.parts) or rel in private or p.suffix in {'.exe','.log','.bak'} or p.name.endswith('.local'):continue
  out=stage/rel;out.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(p,out)
for name in files:shutil.copy2(root/name,stage/name)
for name in ['resource/static/brand','resource/public','resource/template']:
 shutil.copytree(root/name,stage/name,dirs_exist_ok=True)
shutil.copytree(root/'runtime/public-webdist-final',stage/'resource/webadmin')
# Browser API verification is public protocol configuration, not an authentication key.
env=stage/'web/.env';s=env.read_text(encoding='utf-8-sig');s=re.sub(r'(?m)^(\s*VITE_ENCRYPT\s*=).+$',r'\1suxin-public-example',s);env.write_text(s,encoding='utf-8')
with zipfile.ZipFile(stage/'devsource/developer/install/webcode.zip','w',zipfile.ZIP_DEFLATED) as z:
 for p in sorted((stage/'web').rglob('*')):
  if p.is_file():z.write(p,'webcode/'+p.relative_to(stage/'web').as_posix())
# Keep the local installer package synchronized with the sanitized release template.
shutil.copy2(stage/'devsource/developer/install/webcode.zip',root/'devsource/developer/install/webcode.zip')
print('Release tree:',stage)
print('Files:',sum(1 for p in stage.rglob('*') if p.is_file()))
