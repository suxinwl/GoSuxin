"""Check release version, module boundaries, installer package and documentation links."""
from pathlib import Path
import json,re,zipfile,sys
root=Path(sys.argv[1]).resolve() if len(sys.argv)>1 else Path(__file__).resolve().parents[1]
errors=[]
def check(ok,msg):
 if not ok:errors.append(msg)
mods=json.loads((root/'docs/upstream/modules.json').read_text())
for m in mods+[{'module':'github.com/suxinwl/GoSuxin','version':'1.0.0'}]:
 rel=m['module'].removeprefix('github.com/suxinwl/GoSuxin').lstrip('/');s=(root/rel/'go.mod').read_text(encoding='utf-8-sig')
 check(re.search(r'^module '+re.escape(m['module'])+r'\s*$',s,re.M),'module name: '+rel)
 check(not re.search(r'^replace\s',s,re.M),'published replace: '+rel)
 check('github.com/gogf/gf' not in s,'old framework dependency: '+rel)
 for line in s.splitlines():
  if 'github.com/suxinwl/GoSuxin' in line and not line.startswith('module'):
   check(bool(re.search(r'\sv1\.0\.0(?:\s|$)',line)),'module version: '+line.strip())
for f in ['web/package.json','web/package-lock.json']:
 d=json.loads((root/f).read_text(encoding='utf-8-sig'));check(d['version']=='1.0.0',f)
 if 'packages' in d:check(d['packages']['']['version']=='1.0.0',f+' root package')
check('"v1.0.0"' in (root/'framework/version.go').read_text(),'framework version')
for f in ['manifest/config/app.example.yaml','manifest/config/app.yaml']:
 if (root/f).exists():check(bool(re.search(r'version:\s*[\'"]?1\.0\.0',(root/f).read_text(encoding='utf-8-sig'))),f)
for f in ['manifest/sql/v1.0.0.sql','devsource/developer/install/goframepro.sql']:
 s=(root/f).read_text(encoding='utf-8');check("'app_version', '1.0.0'" in s,f)
zp=root/'devsource/developer/install/webcode.zip'
with zipfile.ZipFile(zp) as z:
 check(not any('.git' in Path(n).parts or 'node_modules' in Path(n).parts for n in z.namelist()),'installer private directories')
 check(json.loads(z.read('webcode/package.json'))['version']=='1.0.0','installer version')
nav=json.loads((root/'docs/site-migration/navigation.json').read_text(encoding='utf-8'))
check(nav['version']=='1.0.0','navigation version')
for p in nav['pages']:check((root/'docs/site-migration'/p['path']).is_file(),'missing chapter '+p['path'])
for p in [root/'README.MD',*(root/'docs/site-migration').rglob('*.md')]:
 for target in re.findall(r'\]\(([^)]+)\)',p.read_text(encoding='utf-8')):
  if re.match(r'^[a-z]+:|^#',target):continue
  target=target.split('#')[0].strip('<>')
  if target:check((p.parent/target).exists(),'broken link '+str(p.relative_to(root))+': '+target)
print(json.dumps({'version':'1.0.0','modules':len(mods)+1,'chapters':len(nav['pages']),'errors':errors},ensure_ascii=False,indent=2))
sys.exit(bool(errors))
