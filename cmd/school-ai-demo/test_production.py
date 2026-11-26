import hashlib,json,unittest,zipfile
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlparse

ROOT=Path(__file__).parent/'web/pop-sticker-studio'
class ProductionTest(unittest.TestCase):
    def test_install_bundle(self):
        m=json.loads((ROOT/'manifest.json').read_text());p=ROOT/Path(urlparse(m['archive']).path).name
        self.assertEqual(hashlib.sha256(p.read_bytes()).hexdigest(),m['sha256'])
        self.assertIn(m['sha256'],(ROOT/'SHA256SUMS').read_text())
        with zipfile.ZipFile(p) as z:
            self.assertEqual(sorted(z.namelist()),[m['name']+'/'+f for f in sorted(m['files'])])
            self.assertEqual(z.read(m['name']+'/SKILL.md'),(ROOT/'SKILL.md').read_bytes())
            self.assertEqual(z.testzip(),None)
    def test_independent_example(self):
        self.assertIn('href="steak/"',(ROOT/'index.html').read_text())
        for p in [ROOT/'index.html',ROOT/'steak/index.html',*(ROOT/'steak/assets').glob('*.js')]:
            self.assertNotIn('/demo/bistro-steak',p.read_text())
        self.assertIn('id="swipeCue"',(ROOT/'steak/index.html').read_text())
        self.assertTrue((ROOT/'steak/assets/spoon-empty.webp').is_file())
