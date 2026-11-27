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

    def test_vat_teaching_example_and_boundaries(self):
        d=fixture('vat-reconciliation')
        self.assertTrue(d['is_synthetic'])
        self.assertFalse(any(d['permissions'].values()))
        ex=d['teaching_example']
        self.assertEqual(ex['sales']['gross_gbp']-ex['sales']['output_vat_gbp'],ex['sales']['net_gbp'])
        self.assertEqual(ex['purchase']['gross_gbp']-ex['purchase']['input_vat_gbp'],ex['purchase']['net_gbp'])
        self.assertEqual(ex['sales']['output_vat_gbp']-ex['purchase']['input_vat_gbp'],ex['payable_gbp'])
        self.assertEqual(ex['return_as_filed_gbp']-ex['payable_gbp'],-ex['difference_gbp'])
        self.assertIn('不是真实客户数据',ex['note'])
        self.assertEqual(len(d['days']),4)
        self.assertEqual([x['day'] for x in d['days']],[1,2,3,4])
        self.assertEqual(d['firm']['headcount'],46)
        self.assertEqual(d['firm']['pilot_days'],4)
        self.assertIn('17',d['days'][0]['body'])
        self.assertIn('youtube.com/watch?v=IXDYEF22CG8',d['source']['url'])
        joined=' '.join(d['boundaries'])
        for phrase in ['不是把整家事务所自动化','50 倍','第三方审计']:
            self.assertIn(phrase,joined)

    def test_vat_page_content_and_links(self):
        page=(WEB/'vat-reconciliation'/'index.html').read_text()
        for text in ['£200','£100','£120','£20','46 人','17 类','DAY 4','AI 备稿，会计签字','vat-diff']:
            self.assertIn(text,page)
        # Removed by client request: four notices and the note/tag that carried them.
        for text in ['公开案例 + 教学示例','天试点 · 来源','教学示例，不是真实客户数据','都是为了讲清概念编的','class="context"','vat-demo-tag','三条边界','不是把整家事务所自动化','50 倍','第三方审计','vat-sub','vat-boundary','IXDYEF22CG8']:
            self.assertNotIn(text,page)
        self.assertNotIn('<p class="case-note"></p>',page)
        self.assertIn('href="../#accounting"',page)
        self.assertNotIn('/school-ai-demo/',page)

    def test_generated_pages_current(self):
        pages=[WEB/s/'index.html' for s in ['customer-email','quotation','order-validation','vat-reconciliation']]
        before=[p.read_bytes() for p in pages]
        subprocess.run(['python3','-B',str(ROOT/'build_cases.py')],check=True,capture_output=True)
        self.assertEqual(before,[p.read_bytes() for p in pages])
        for p in pages:
            if p.parent.name != 'vat-reconciliation':
                self.assertIn('人工编写预置',p.read_text())

if __name__ == '__main__': unittest.main()
