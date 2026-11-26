#!/usr/bin/env python3
"""Build synthetic case pages from reviewed fixtures. No model/API calls."""
import html
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
WEB = ROOT / 'web'

def load(name):
    return json.loads((ROOT / 'cases' / (name + '-demo.json')).read_text())

def esc(value):
    return html.escape(str(value), quote=True)

def table(head, rows):
    return '<div class="case-table" tabindex="0" role="region" aria-label="可横向滚动的数据表"><table><thead><tr>' + ''.join('<th scope="col">'+esc(x)+'</th>' for x in head) + '</tr></thead><tbody>' + ''.join('<tr>'+''.join('<td>'+esc(x)+'</td>' for x in row)+'</tr>' for row in rows) + '</tbody></table></div>'

def raw_html(id, title, body, opened=False, summary=''):
    meaning = '<div class="source-meaning"><h4>中文要点 / 核对提示</h4><p>'+esc(summary)+'</p></div>' if summary else ''
    return f'<article class="source-document" id="{esc(id)}" data-source-title="{esc(title)}" data-default="{str(opened).lower()}"><h3>{esc(id)} · {esc(title)}</h3>{body}{meaning}</article>'

def raw(id, title, body, opened=False, summary=''):
    return raw_html(id,title,'<pre>'+esc(body)+'</pre>',opened,summary)

def source_browser(sources):
    return '<div class="source-browser"><div class="source-list" role="group" aria-label="选择原始材料"></div><div class="source-view">'+sources+'</div></div>'

def field(label, value, evidence, source, pending=False, full=False):
    cls = 'field'+(' pending' if pending else '')+(' full' if full else '')
    return f'<div class="{cls}"><p class="field-label">{esc(label)}</p><p class="field-value">{esc(value)}</p><details class="evidence"><summary>查看依据 · {esc(source)}</summary><blockquote>{esc(evidence)}</blockquote><a href="#{esc(source)}" data-source="{esc(source)}">打开完整原文 ↗</a></details></div>'

def todo(who, title, text):
    return f'<div class="todo"><span>{esc(who)}</span><h3>{esc(title)}</h3><p>{esc(text)}</p></div>'

def note(text, kind='hold'):
    return f'<p class="case-note {kind}">{esc(text)}</p>'

def section(id, title, body):
    steps = ['materials','result','handoff']
    if id not in steps:
        return f'<section class="case-card" id="{id}"><h2>{title}</h2>{body}</section>'
    index = steps.index(id)
    controls = ('<button type="button" class="btn btn-o" data-step="'+steps[index-1]+'">← 上一步</button>') if index else '<span></span>'
    if index < 2:
        controls += '<button type="button" class="btn btn-b" data-step="'+steps[index+1]+'">'+['下一步：看整理结果 →','下一步：看如何接手 →'][index]+'</button>'
    return f'<section class="step-body" id="{id}" role="tabpanel" aria-labelledby="step-{id}" tabindex="0"><h2>{title}</h2>{body}<div class="step-controls">{controls}</div></section>'

def draft(text, id='draft'):
    return f'<div class="draft-box"><div class="draft-heading"><h3>待审草稿 · 英文</h3><span class="draft-badge">人工待审 · 未发送</span></div><pre id="{id}" class="draft">{esc(text)}</pre><button type="button" class="btn btn-o" data-copy="{id}">复制待审草稿</button><p class="draft-note">经授权人审批后，由业务员在自己的系统中操作；本页不发送、不写入。</p></div>'

def checks(data):
    return '<details class="source"><summary>不只看顺利流程：这些情况必须停下来</summary>'+table(['情况','预期处理'],[(x['input'],x['expected']) for x in data['negative_tests']])+'</details>'

CASES = [('customer-email','客户邮件与跟进'),('quotation','报价准备与审批'),('order-validation','订单核对与录入')]

def page(slug, title, intro, sections, data):
    nav = ''.join('<a href="../'+s+'/" '+('aria-current="page"' if s == slug else '')+'>'+f'{i+1:02d} '+esc(t)+'</a>' for i,(s,t) in enumerate(CASES))
    headlines = {'customer-email':'同事休假了，这个客户你接得住吗？','quotation':'“3.70 美元能不能做？”——先把账算清','order-validation':'客户 PO 来了，能直接排产吗？'}
    before_after = {
        'customer-email': ('翻邮件、追问休假同事，按旧表 500 个核价。','先看带来源的最新需求，知道缺什么、找谁，再审回复。'),
        'quotation': ('只看裸杯单价，漏掉附加费用，客户一压价就凭感觉答应。','统一口径、列清成本，看到降价后的毛利，再交负责人决定。'),
        'order-validation': ('金额没算错就录单，忽略箱数、贸易术语和账期变化。','先比约定与采购单；修订对齐后形成草稿，收款和图稿仍单独把关。'),
    }
    before, after = before_after[slug]
    compare = '<section class="case-card compare-section"><h2>同一件工作，两种做法</h2><div class="compare"><div><h3>原来的做法</h3><p>'+before+'</p></div><div><h3>这个流程想改变的</h3><p>'+after+'</p></div></div><p class="draft-note">这是模拟工作方式，不是已经测得的客户成效。</p></section>'
    content = f'''<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="theme-color" content="#fcfbf8"><title>{title} · AI Demo</title><meta name="description" content="{esc(intro)}"><link rel="icon" href="../favicon.svg"><link rel="stylesheet" href="../app.css"><link rel="stylesheet" href="../cases.css"></head>
<body class="case-page"><header class="nav"><a class="brand" href="../"><span class="case-logo" aria-hidden="true">AI</span>AI Demo</a><a class="btn btn-o" href="../#trade">← 全部案例</a></header>
<main class="case-wrap"><nav class="case-siblings" aria-label="外贸案例">{nav}</nav><header class="case-intro"><span class="case-label">外贸 · {title}</span><h1>{headlines[slug]}</h1><p>{esc(intro)}</p></header>
{note('业务模拟，非真实客户成果。公司、客户和单据均为虚构；输入与展示结果由人工编写预置，未运行真实模型、未连接邮箱或 ERP。','neutral')}
<p class="context">宁波澄禾礼品贸易（虚构，12 人） · 德国客户 Harbor Sample Trading / Alex Demo · 2027 年 8 月</p>
<div class="case-workspace"><div class="case-steps" role="tablist" aria-label="演示步骤"><button type="button" role="tab" id="step-materials" aria-controls="materials" aria-selected="true">① 看原始材料</button><button type="button" role="tab" id="step-result" aria-controls="result" aria-selected="false" tabindex="-1">② 看整理结果</button><button type="button" role="tab" id="step-handoff" aria-controls="handoff" aria-selected="false" tabindex="-1">③ 看如何接手</button></div>
{sections}</div>{compare}{section('boundaries','落地时，哪些事不能自动做？',checks(data)+note('AI 理解材料、生成草稿；程序负责确定性计算和校验；人批准价格、账期、交期、外发与生产放行。当前页面只演示流程，不执行这些操作。','neutral'))}
<p class="case-end"><a class="btn btn-o" href="../#trade">← 返回外贸案例</a><a href="#materials">回到原始材料 ↑</a></p></main><footer>AI Demo · 学校 / 外贸 · <a href="https://liangz77.cn/">liangz77.cn</a></footer><p id="copy-status" class="case-toast" role="status" aria-live="polite"></p><script src="../cases.js"></script></body></html>'''
    path = WEB / slug / 'index.html'
    path.parent.mkdir(exist_ok=True)
    path.write_text(content)


def email():
    d = load('customer-email')
    titles = ['首次询盘 · 8 月 2 日','工艺与包装 · 8 月 3 日','追问进度 · 8 月 5 日','改变需求 · 8 月 5 日','主管内部安排 · 8 月 6 日']
    summaries = ['客户要 500 个 750ml 不锈钢水瓶，带 Logo；9 月 30 日到汉堡是客户期望。','数量仍是 500；客户偏好激光，独立纸盒。工厂是否可做尚待核实。','客户追问进展，并询问到港目标是否可行；没有新增我方承诺。','数量改为 800：黑 600、白 200，B750。包装改为无印刷独立白盒；要求 FOB 宁波，客户货代安排海运，矢量图稿待发。','林悦休假，周宁获授权代班；陈晓核实成本与排期。16:00 是内部反馈期限，价格和交期仍按原权限审批。']
    sources = ''.join(raw(m['id'],title,m['from']+' → '+m['to']+'\\n'+m['timestamp']+'\\nSubject: '+m['subject']+'\\n\\n'+m['body'],m['id']=='M4',summary) for m,title,summary in zip(d['messages'],titles,summaries))
    stale = '<h3>与此同时，旧 Excel 跟进表里还是这样：</h3>'+table(['客户','产品','数量','状态','更新时间'],[['Harbor Sample Trading','750ml 水瓶','500','待客户确认 Logo 与包装','8 月 2 日']])+note('只看旧表，会按 500 个核价。最新 M4 已改为 800，包装也已明确；9 月 30 日仍只是客户到港目标。')
    facts = field('客户 / 联系人','Harbor Sample Trading · Alex Demo','虚构客户档案 + alex@harbor.example；真实业务须人工核对客户关系，不能仅凭同名合并。','M4')
    facts += field('最新数量','500 → 800','M1 / M2 是 500；M4: Our team has revised the quantity to 800.','M4')
    facts += field('规格与配色','750ml · B750 · 哑光黑 600 + 哑光白 200','容量见 M1；M4: 600 matte black and 200 matte white, model B750.','M4')
    facts += field('Logo 工艺与图稿','客户要求激光 · 图稿待收、工厂待核实','M2: we prefer laser engraving. M4: We will send the vector logo artwork after internal review.','M2',True)
    facts += field('包装','独立白盒 · 无零售印刷','M4: plain white individual boxes, with no retail printing. 替代 M2 的 individual cardboard boxes。','M4')
    facts += field('贸易条款','FOB 宁波 · 客户货代负责海运','Quote FOB Ningbo; our forwarder will arrange the sea freight.','M4')
    facts += field('到货日期','2027 年 9 月 30 日 = 客户期望，尚未承诺','Our requested arrival date is still September 30, 2027. 未补全：成本、生产排期、图稿到达时间，等待内部核实。','M4',True,True)
    result = '<p class="step-desc">先看最新状态，再按需展开依据。没有来源的字段不猜，拿不准的标为待确认。</p><div class="fields">'+facts+'</div>'
    tasks = '<div class="todos">'+todo('陈晓 · 采购','核实工厂成本与生产条件','按 800 个（黑 600 / 白 200、激光、白盒）重核成本和排期。依据：M4 + H1。')+todo('周宁 · 代班业务','检查回复，等待矢量图稿','图稿未到，只请客户发送，不承诺打样时间。依据：M4 + H1。')+todo('周宁 · 代班业务','16:00 前反馈内部进展','这是主管给的内部期限，不是对客户的交付承诺。依据：H1。')+todo('主管 · 审批','价格与到货口径由授权人确认','审批后由业务员在公司邮箱发送；本页不提供发送。依据：H1。')+'</div>'
    handoff = '<p class="step-desc">8 月 6 日上午，主管指定周宁代班、陈晓核实工厂。每个人接哪一件事，先说清楚。</p>'+tasks+draft(d['draft_reply']['text'])+'<p class="draft-note">中文说明：复述更新后的需求，请客户提供图稿；明确到港日期未确认，海运时效需同客户货代核对。</p>'
    sections = section('materials','业务员邮箱里的原始样子','<p class="step-desc">四封客户邮件 + 一条主管安排。默认打开最新 M4，左侧可切换原文，右侧看中文要点。</p>'+source_browser(sources)+stale)+section('result','整理后的客户记录：每条都能核对原文',result)+section('handoff','周宁接班当天，要做什么',handoff)
    page('customer-email','客户邮件与跟进','把散落在邮件里的需求变更、沟通历史和待办，整理成另一位同事可以直接接手的客户记录。报价、排期和到货承诺，仍由人确认。',sections,d)


def quotation():
    d = load('quotation'); q = d['customer_quote']; internal = d['internal']
    sources = ''.join(raw(s['id'],{'A1':'工厂 A 报价','B1':'工厂 B 报价','F1':'起运港侧费用','R1':'内部测算规则','T1':'客户目标价格'}[s['id']],s['body'],s['id']=='A1') for s in d['sources'])
    sources = '<p class="step-desc">同一笔 800 个水瓶订单，工厂、货代和内部规则各给了一部分信息。先看原始口径，再决定怎样算。</p>'+source_browser(sources)+note('承接 M4：B750 共 800 个，黑 600 / 白 200，单面激光、独立白盒，FOB 宁波。','neutral')
    comparison = table(['比较项','工厂 A · A1','工厂 B · B1'],[['裸杯','18.80 元 / 个，含税','18.30 元 / 个，未税'],['激光 / 白盒 / 开机','0.90 / 0.80 / 240 元','1.10 / 1.00 / 300 元'],['单色起订量','200 个；黑白均满足','500 个；白色仅需 200，不满足'],['排期','定金与图稿齐备后 25 自然日','待询'],['结论','模拟情节中由人审核选用','税费、MOQ、排期未统一，不能直接比价']])
    costs = table(['项目','数量','单价（元）','小计（元）','来源'],[[r['label'],r['quantity'],r['unit_cny'],r['subtotal_cny'],r['source_id']] for r in internal['cost_rows']])
    calc = '''<div class="internal"><p class="case-label">内部试算 · 不属于对客报价</p><details class="source"><summary>展开 7 项成本明细 · 合计 ¥18,080.00</summary>'''+costs+'''</details><p>按含税现金支出测算，暂不计退税。验货、银行与单证为预算；不是完整会计利润，也不是实时汇率或市场报价。</p><p class="formula">18,080 ÷ 800 ÷ 7.20 ÷ (1 − 20%) = 3.9236…<br>向上取美分 → USD 3.93 / 个 · 总额 USD 3,144.00 · 测算毛利率 20.13%</p><label for="target-price">试算客户目标单价（USD / 个，仅本地计算）</label><div class="case-actions"><input id="target-price" type="number" value="3.70" min="0.01" max="9999" step="0.01"><button class="btn btn-o" type="button" id="calculate-price">核算，不批准</button></div><p id="price-result" role="status">USD 3.70 → 总额 2,960.00；测算毛利率 15.17%，低于目标，须另行审批。</p></div>'''
    quote = f'''QUOTATION · {q['id']} / V1
Date: 2027-08-06 | Valid until: 2027-08-13
Customer: Harbor Sample Trading (fictional)
B750 matte black: 600 PCS × USD 3.93 = USD 2,358.00
B750 matte white: 200 PCS × USD 3.93 = USD 786.00
Total: 800 PCS · USD 3,144.00
FOB Ningbo · Incoterms 2020
Single-position laser engraving, max 30×20mm; plain white individual box.
30% deposit: USD 943.20; 70% balance: USD 2,200.80, after inspection and before shipment.
Production: 25 calendar days after BOTH deposit receipt and final artwork approval.
September 30 is the customer's requested arrival date only, not a confirmed commitment.
Excludes international sea freight, insurance, destination and import-related costs.
BUSINESS SIMULATION — FOR REVIEW ONLY, NOT SENT.'''
    handoff = '<p class="step-desc">把内部算账留在内部，对客只给审核过的规格、价格和条款。本页不执行审批；下一个案例以“模拟已批准 V1”为基准。</p><div class="todos">'+todo('陈晓 · 采购','复核费用与工厂排期','A1 / F1 有效至 8 月 13 日；数量、装箱或工艺变化须重核。')+todo('许总 · 审批','决定是否接受客户目标价','3.70 对应 15.17% 测算毛利，低于目标须另批；3.93 也不是自动批准。')+todo('周宁 · 业务','只发送对客报价','不夹带供应商底价、成本表和内部毛利阈值。')+todo('财务 / 业务','保留生产启动条件','30% 定金 + 最终图稿确认两项齐备后 25 自然日生产，不承诺到港日。')+'</div>'+draft(quote,'customer-quote')+note('复制按钮仅复制对客草稿，不包含内部成本、供应商底价和毛利阈值。数量、工艺面积、贸易术语或有效期变化，都须重核；客户接受 3.70 不等于内部批准。')
    fields = field('可解释的总成本','¥18,080.00 · ¥22.60 / 个','A1 裸杯 / 激光 / 白盒 / 开机；F1 起运港侧费用；R1 验货、银行及单证预算。见下方成本明细。','A1')
    fields += field('目标毛利下的建议报价','USD 3.93 / 个 · 合计 USD 3,144','按汇率 7.20、目标毛利 20%，单价向上取美分。测算毛利 20.13%；正式报价仍须批准。','R1')
    fields += field('客户希望的价格','USD 3.70 → 毛利 15.17% · 需另批','T1 客户目标；按相同成本和汇率测算，低于内部 20% 目标。客户同意不等于内部批准。','T1',True)
    fields += field('看起来更便宜的工厂 B','不能直接比较 · 白色数量不满足 MOQ','18.30 元是未税裸杯价；单色起订 500，白色仅需 200。税费与排期待询。','B1',True)
    result = '<p class="step-desc">关键结论放在前面，计算过程和来源按需展开。这里的数字都是模拟假设，不是市场行情。</p><div class="fields">'+fields+'</div><details class="source"><summary>逐项比较工厂 A / B 的口径</summary>'+comparison+'</details>'+calc
    sections = section('materials','报价信息原本散在这些地方',sources)+section('result','先统一口径，再回答“这个价能不能做”',result)+section('handoff','内部测算与对客报价，分开交接',handoff)
    page('quotation','报价准备与审批','把散落在邮件里的费用补齐，算出能解释的报价；客户压价时，先算清再审批。',sections,d)


def order():
    d = load('order-validation')
    sources = raw('P0','客户催开工邮件',d['customer_cover_email']['body'],summary='客户附件发来采购单，要求尽快生产。这不是内部放行授权。')+raw('Q1','模拟已批准报价 · Q-20270806-017 / V1','黑 600，白 200；共 800 个 × USD 3.93 = USD 3,144\nFOB Ningbo，30%定金，70%验货后出货前支付。\n定金及图稿齐备后 25 自然日生产；9 月 30 日是客户目标，不是到港承诺。')
    for p in d['documents']:
        body = table(['客户 SKU','箱数（CTN）','每箱 PCS','折算 PCS','USD / PCS','行金额 USD'],[[i['customer_sku'],i['quantity'],i['pcs_per_carton'],i['derived_pcs'],i['unit_price_usd'],i['amount_usd']] for i in p['items']])
        body += '<p>'+esc(f"合计 {p['total_cartons']} 箱 / {p['total_pcs']} PCS / USD {p['total_usd']}；{p['trade_term']}；{p['payment']}")+'</p>'
        sources += raw_html('PO'+str(p['revision']),'采购单 Rev'+str(p['revision'])+' · PO-HS-270809',body,p['revision']==0,'Rev0：20 箱黑 + 5 箱白，每箱 40 个，金额算术正确，但数量与条款要同报价核对。' if p['revision']==0 else 'Rev1：黑色改为 15 箱，共 800 个，恢复 FOB 和定金尾款；定金及图稿仍待确认。')
    sources += raw('C1','客户澄清邮件',d['customer_revision_email']['body'],summary='客户保留黑 600 / 白 200，承认 DDP 与 Net 60 来自旧模板，按报价恢复条款；9 月 30 日仍取决于船期。')+raw('MAP1','人工核对过的 SKU 映射（模拟）','HB-B750-BK → CH-B750-BK\nHB-B750-WH → CH-B750-WH\n本单每箱 40 个；不可推广为所有订单的固定箱规。')
    diffs = table(['核对项','报价 V1','采购单 Rev0','结论'],[['黑色数量','600 个','20 箱 × 40 = 800 个','多 200 个'],['总数量 / 金额','800 / USD 3,144','1,000 / USD 3,930','金额自洽，但与报价不符'],['贸易术语','FOB Ningbo','DDP Hamburg','责任和费用变化，不能直接接单'],['付款','30% 定金，70% 验货后出货前','收货后 60 天','账期风险需批准'],['到港日期','未承诺','要求 9 月 30 日','目标不等于双方承诺']])
    revise = table(['修订后的字段','Rev1 草稿值'],[['内部 SKU / 数量','CH-B750-BK：600；CH-B750-WH：200'],['单价 / 总额','USD 3.93 / USD 3,144.00'],['贸易术语','FOB Ningbo'],['定金 / 尾款','USD 943.20 / USD 2,200.80'],['定金状态','待赵敏（财务）确认到账'],['图稿状态','待最终批准'],['生产完成日','留空；未开始计算'],['订单状态','待吴静（跟单）人工复核，不放行生产']])
    draft_text = '''Hi Alex,
Before we process PO-HS-270809, please confirm:
1. The PO lists 20 cartons of black bottles (800 PCS), while the quotation lists 600 black and 200 white. Is the increase intended?
2. The PO says DDP Hamburg rather than FOB Ningbo. Should we re-quote, or keep FOB Ningbo?
3. Please confirm 30% deposit and 70% balance after inspection before shipment, instead of Net 60 after receipt.
4. September 30 is still an unconfirmed arrival target. Production starts only after deposit receipt AND final artwork approval.
Best regards,
Zhou Ning'''
    events = '''<div class="version-demo"><h3>再收到一次单据，会怎样？</h3><p>只在本页模拟状态，不上传文件、不写 ERP；刷新可重置。</p><div class="case-actions"><button class="btn btn-o" data-po="0">模拟收到 Rev0</button><button class="btn btn-b" data-po="1">模拟收到 Rev1</button><button class="btn btn-o" data-po="2">模拟收到 Rev2</button></div><p id="po-state" role="status">尚未生成本页模拟草稿。先核对 Rev0，或试收 Rev1。</p></div>'''
    fields = field('数量与金额','800 → 1,000 个 · 多 USD 786','Rev0 黑 20 箱 × 40 = 800，白 5 箱 × 40 = 200；报价黑 600 / 白 200。算术自洽不等于与约定一致。','PO0',True)
    fields += field('贸易术语','FOB 宁波 → DDP 汉堡','报价 Q1 是 FOB；Rev0 变成 DDP，责任和费用变化，不能按原价直接接。','PO0',True)
    fields += field('付款方式','定金 / 尾款 → 收货后 60 天','报价是 30% 定金、70% 验货后出货前付；采购单变成 Net 60，需澄清并审批。','PO0',True)
    fields += field('到港与生产','9 月 30 日仍未承诺 · 不可放行','生产须定金到账及图稿批准后才能开始计时，客户要求尽快生产不能替代内部审批。','Q1',True)
    result = '<p class="step-desc">先看四处关键差异。不能把 CTN 当 PCS，更不能只验算金额就把订单交给工厂。</p><div class="fields">'+fields+'</div><details class="source"><summary>展开逐项对照：报价 V1 vs 采购单 Rev0</summary>'+diffs+'</details>'+note('Rev0 暂停录入可执行订单。先由周宁审核下面的澄清草稿，与客户确认改动是否有意。')+draft(draft_text,'clarification')
    tasks = '<div class="todos">'+todo('周宁 · 业务','核对客户澄清与 Rev1','C1 明确旧模板误带条款；Rev1 黑 15 箱、白 5 箱，共 800 个，恢复 FOB / 定金尾款。')+todo('吴静 · 跟单','复核后暂存订单草稿','SKU 映射按 MAP1，由人工确认；不把单据一致误当成已放行生产。')+todo('赵敏 · 财务','确认定金 USD 943.20 到账','当前状态仍是待确认，不能把客户 PO 当作到账凭证。')+todo('业务 / 工厂','确认最终图稿再启动生产','图稿仍待最终批准；两项齐备后才计算 25 自然日，完成日先留空。')+'</div>'
    handoff = '<p class="step-desc">客户修订后，先分清“资料已对齐”和“生产条件已满足”。这两件事不能混在一起。</p>'+tasks+'<details class="source"><summary>查看 Rev1 待人工复核订单草稿</summary>'+revise+'</details>'+note('单据一致 ≠ 生产放行。定金到账与图稿确认两项未齐备，不开始生产倒计时。')+events
    sections = section('materials','客户采购单与原约定，放在一起看','<p class="step-desc">默认打开有冲突的 Rev0。左侧切换原报价、客户邮件与修订版；都是本案预置单据。</p>'+source_browser(sources))+section('result','金额算对了，但这四件事还没对上',result)+section('handoff','修订对齐以后，各自还要确认什么',handoff)
    page('order-validation','订单核对与录入','采购单的金额没算错，却比报价多了 200 个，还换了贸易与付款条款。先找差异，再建草稿。',sections,d)

if __name__ == '__main__':
    email(); quotation(); order()
    print('Built 3 synthetic case pages')
