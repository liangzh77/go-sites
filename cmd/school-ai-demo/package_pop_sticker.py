"""Rebuild the public pinned skill release from a reviewed local skill directory."""
import argparse, hashlib, json, zipfile
from pathlib import Path
from shutil import copyfile

files = """SKILL.md agents/openai.yaml examples/scene.json examples/art/pan.webp examples/art/raw.webp examples/art/seared.webp references/style-board.jpg references/motion.md scripts/export_web.py scripts/stage.html scripts/render.py scripts/outline.py""".split()
p=argparse.ArgumentParser(description=__doc__);p.add_argument('source',type=Path);args=p.parse_args()
out=Path(__file__).parent/'web/pop-sticker-studio';out.mkdir(exist_ok=True);name='pop-sticker-studio';version='1.0.1';base='https://liangz77.cn/ai-demo/'+name+'/'
archive=out/f'{name}-{version}.zip'
with zipfile.ZipFile(archive,'w') as z:
    for f in sorted(files):
        src=args.source/f
        if not src.is_file() or src.is_symlink():raise ValueError(f)
        info=zipfile.ZipInfo(name+'/'+f,(2026,1,1,0,0,0));info.create_system=3;info.external_attr=0o100644<<16
        z.writestr(info,src.read_bytes(),compress_type=zipfile.ZIP_DEFLATED)
copyfile(args.source/'SKILL.md',out/'SKILL.md');copyfile(args.source/'references/style-board.jpg',out/'style-board.jpg')
digest=hashlib.sha256(archive.read_bytes()).hexdigest()
(out/'SHA256SUMS').write_text(digest+'  '+archive.name+'\n')
meta=dict(name=name,version=version,page=base,instructions=base+'install.md',archive=base+archive.name,sha256=digest,archive_root=name,entrypoint='SKILL.md',files=sorted(files))
(out/'manifest.json').write_text(json.dumps(meta,ensure_ascii=False,indent=2)+'\n')
