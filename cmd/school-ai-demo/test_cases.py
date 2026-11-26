import json
import subprocess
import unittest
from decimal import Decimal, ROUND_CEILING
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parent
WEB = ROOT / 'web'

def fixture(name):
    return json.loads((ROOT / 'cases' / (name+'-demo.json')).read_text())

class Links(HTMLParser):
    def __init__(self):
        super().__init__(); self.links=[]
    def handle_starttag(self, tag, attrs):
        self.links += [v for k,v in attrs if k in ('href','src') and v]

class CasesTest(unittest.TestCase):
    def test_synthetic_and_permissions(self):
        for name in ['customer-email','quotation','order-validation']:
            d=fixture(name)
            self.assertTrue(d['is_synthetic'])
            self.assertFalse(any(d['permissions'].values()))

    def test_quote_math_and_handoff(self):
        q=fixture('quotation'); i=q['internal']; o=fixture('order-validation')
        cost=sum(Decimal(r['unit_cny'])*r['quantity'] for r in i['cost_rows'])
        self.assertEqual(cost,Decimal('18080'))
        unit=(cost/800/Decimal('7.20')/Decimal('.80')).quantize(Decimal('.01'),rounding=ROUND_CEILING)
        self.assertEqual(unit,Decimal('3.93'))
        self.assertEqual(unit*800,Decimal(q['customer_quote']['total_amount']))
        self.assertEqual(q['customer_quote'],o['approved_quote'])
        self.assertEqual(fixture('customer-email')['expected_snapshot']['facts'][0]['value'],q['request']['quantity'])
        for price,margin in [('3.93','20.13'),('3.70','15.17')]:
            revenue=Decimal(price)*800*Decimal('7.20')
            self.assertEqual(((revenue-cost)/revenue*100).quantize(Decimal('.01')),Decimal(margin))

    def test_order_units_and_hold(self):
        d=fixture('order-validation')
        for p in d['documents']:
            self.assertEqual(sum(x['quantity']*x['pcs_per_carton'] for x in p['items']),p['total_pcs'])
            self.assertEqual(sum(Decimal(x['amount_usd']) for x in p['items']),Decimal(p['total_usd']))
        self.assertTrue(d['expected_v0']['arithmetic_valid'])
        self.assertFalse(d['expected_v0']['create_executable_order'])
        self.assertFalse(d['expected_v1_draft']['production_released'])
        self.assertIsNone(d['expected_v1_draft']['production_complete_on'])

    def test_local_assets_and_no_old_links(self):
        for path in WEB.rglob('*.html'):
            p=Links();p.feed(path.read_text())
            for link in p.links:
                self.assertNotIn('/school-ai-demo/',link)
                if link.startswith(('https:','http:','#','data:')): continue
                target=(path.parent/link.split('?')[0].split('#')[0]).resolve()
                self.assertTrue(target.exists(),str(target))
        for path in WEB.rglob('*.js'):
            self.assertNotIn('/school-ai-demo/api/',path.read_text())

    def test_generated_pages_current(self):
        pages=[WEB/s/'index.html' for s in ['customer-email','quotation','order-validation']]
        before=[p.read_bytes() for p in pages]
        subprocess.run(['python3','-B',str(ROOT/'build_cases.py')],check=True,capture_output=True)
        self.assertEqual(before,[p.read_bytes() for p in pages])
        for p in pages:
            self.assertIn('人工编写预置',p.read_text())

if __name__ == '__main__': unittest.main()
