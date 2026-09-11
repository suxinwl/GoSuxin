from pathlib import Path
import re,base64,gzip,zipfile,io

def rewrite(blob):
 out=io.BytesIO()
 with zipfile.ZipFile(io.BytesIO(blob)) as source,zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED) as target:
  for entry in source.infolist():
   data=source.read(entry)
   if entry.filename.endswith('.zip'): data=rewrite(data)
   else:
    try:
     s=data.decode('utf-8');s=s.replace('github.com/suxinwl/GoSuxin/api/wxapp/v1','github.com/gogf/template-single/api/hello/v1');s=s.replace('github.com/gogf/','github.com/suxinwl/templates/').replace('@gf ','@suxin ').replace('GoFrame','Suxin');s=s.replace('gfcli','suxincli').replace('gf build','suxin build').replace('gf run','suxin run').replace('gf gen','suxin gen')
     if entry.filename.endswith('hack-cli.mk'): s='# Install the pinned Suxin CLI release.\n.PHONY: cli cli.install\ncli:\n\tgo install github.com/suxinwl/GoSuxin/framework/cmd/suxin@v1.0.0\ncli.install:\n\tsuxin version\n'
     if entry.filename.endswith('go.mod'):
      name=re.search(r'^module (.+)',s,re.M)[1];s='module '+name+'\n\ngo 1.25.1\n\nrequire github.com/suxinwl/GoSuxin/framework v1.0.0\n'
     elif entry.filename.endswith('go.sum'): s=''
     data=s.encode('utf-8')
    except UnicodeDecodeError: pass
   target.writestr(entry,data)
 return out.getvalue()
for p in Path('framework/cmd/suxin/internal/packed').glob('template*.go'):
 s=p.read_text();m=re.search(r'gres.Add\("([^"]+)"',s)
 if m:
  out=base64.b64encode(gzip.compress(rewrite(gzip.decompress(base64.b64decode(m[1]))),mtime=0)).decode();p.write_text(s[:m.start(1)]+out+s[m.end(1):])
