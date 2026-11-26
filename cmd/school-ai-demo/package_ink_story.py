"""Rebuild the four-file, portable Ink Story Film release (no machine config or demo media)."""
import argparse, hashlib, json, shutil, zipfile
from pathlib import Path
args=argparse.ArgumentParser(description=__doc__);args.add_argument('source',type=Path);root=args.parse_args().source
name='ink-story-film'; version='1.0.0'; out=Path(__file__).parent/'web'/name; base='https://liangz77.cn/ai-demo/'+name+'/'
files=sorted(['SKILL.md','agents/openai.yaml','assets/风格板.jpg','references/制作与验收.md'])
archive=out/f'{name}-{version}.zip'
with zipfile.ZipFile(archive,'w') as z:
    for f in files:
        path=root/f
        if not path.is_file() or path.is_symlink(): raise ValueError(f)
        data=path.read_bytes()
        if f.endswith(('.md','.yaml')) and any(x in data for x in (b'/Users/',b'dangerously-bypass',b'127.0.0.1:')):raise ValueError('Not portable: '+f)
        i=zipfile.ZipInfo(name+'/'+f,(2026,1,1,0,0,0));i.create_system=3;i.external_attr=0o100644<<16
        z.writestr(i,data,compress_type=zipfile.ZIP_DEFLATED)
sha=hashlib.sha256(archive.read_bytes()).hexdigest()
(out/'SHA256SUMS').write_text(sha+'  '+archive.name+'\n')
(out/'manifest.json').write_text(json.dumps(dict(name=name,version=version,page=base,instructions=base+'install.md',archive=base+archive.name,sha256=sha,archive_root=name,entrypoint='SKILL.md',files=files),ensure_ascii=False,indent=2)+'\n')
shutil.copyfile(root/'SKILL.md',out/'SKILL.md')
