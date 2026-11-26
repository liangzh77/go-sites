/* School AI Demo — one page, no framework.
 *
 * The form itself is not markup this app knows: the server sends back the
 * template HTML with data-field-id on every fillable slot, and this file only
 * binds those slots to a values map. So a new template needs no front-end work.
 */
(() => {
  'use strict';

  const BASE = new URL('.', location.href).pathname.replace(/\/$/, '');
  const $ = (id) => document.getElementById(id);

  // Before the first paint: a hash means a form is (or is about to be) open, so
  // the header keeps its nav links; anything else is the demo list. This runs
  // from the script at the end of <body>, i.e. before the first paint, so a
  // direct load of #enrollment never flashes the slim header.
  if (!location.hash) setNav('home');

  // The paper keeps its 794px (A4 at 96dpi) logical width: print and PDF need
  // it. Anything narrower is handled by scaling the wrapper, not the sheet.
  const PAPER_W = 794;

  // Card definitions live in this JS bundle. Only non-sensitive template
  // summaries and the selected category survive page navigation in this tab.
  const CATALOG_KEY = 'ai-demo:catalog:v1:' + BASE;
  const isCategory = key => ['school', 'trade', 'production', 'tools'].includes(key);
  function templateSummaries(value) {
    if (!Array.isArray(value) || value.length > 100 || !value.every(t =>
      t && typeof t.slug === 'string' && /^[a-z0-9-]{1,80}$/.test(t.slug) &&
      typeof t.name === 'string' && t.name.length <= 160 &&
      Number.isInteger(t.fieldCount) && t.fieldCount >= 0)) return null;
    return value.map(({ slug, name, fieldCount }) => ({ slug, name, fieldCount }));
  }
  function readCatalog() {
    try {
      const saved = JSON.parse(sessionStorage.getItem(CATALOG_KEY));
      const templates = templateSummaries(saved?.templates);
      if (templates && isCategory(saved.filter)) return { templates, filter: saved.filter };
    } catch (_) { /* Storage may be disabled or contain an older/broken value. */ }
    return { templates: [], filter: 'school' };
  }
  const cachedCatalog = readCatalog();
  const state = {
    templates: cachedCatalog.templates,
    ai: { configured: false, model: '', keyEnv: '' },
    current: null,        // { slug, name, fields, html }
    sessionId: '',
    values: {},           // fieldId -> string
    dirty: false,
    draft: { file: null, text: '' },  // material to keep across a template switch
    doc: null,            // the iframe's document
    quota: null,          // today's AI budget, as the server reports it
    aiOff: false,         // no API key configured on the server
    demo: false,          // the sheet currently holds the generated sample data
    filter: isCategory(location.hash.slice(1)) ? location.hash.slice(1) : cachedCatalog.filter,
  };

  function saveCatalog() {
    try {
      sessionStorage.setItem(CATALOG_KEY, JSON.stringify({
        templates: templateSummaries(state.templates) || [], filter: state.filter,
      }));
    } catch (_) { /* Static cards still work without browser storage. */ }
  }

  // --- tiny helpers ---------------------------------------------------------
  const escapeHTML = (s) => String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  let toastTimer = 0;
  function toast(msg) {
    const el = $('toast');
    el.textContent = msg;
    el.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove('show'), 2600);
  }

  function setStatus(kind, html) {
    const el = $('status');
    if (!kind) { el.hidden = true; el.className = 'status'; el.innerHTML = ''; return; }
    el.hidden = false;
    el.className = 'status ' + kind;
    el.innerHTML = html;
  }

  async function api(path, opts = {}) {
    const res = await fetch(BASE + path, {
      headers: opts.body instanceof FormData ? {} : { 'Content-Type': 'application/json' },
      ...opts,
    });
    let data = {};
    try { data = await res.json(); } catch (_) { /* non-JSON error page */ }
    if (!res.ok) {
      const err = new Error(data.error || ('请求失败（' + res.status + '）'));
      err.code = data.code || '';
      if (data.quota) err.quota = data.quota;
      throw err;
    }
    return data;
  }

  const statusLabel = {
    filled: '已填写', pending: '处理中', failed: '失败', no_key: '缺 Key',
  };

  // --- demo list ------------------------------------------------------------
  // Browsing/returning to the catalog never needs /api/state. Fetch current
  // templates, readiness and quota only when entering a form; don't cache the
  // response's recent sessions, field data, AI configuration or quota.
  let stateRequest = null;
  function loadState() {
    if (stateRequest) return stateRequest;
    stateRequest = (async () => {
      try {
        const data = await api('/api/state');
        const templates = templateSummaries(data.templates);
        if (!templates) throw new Error('表格列表格式异常，请稍后重试。');
        state.templates = templates;
        state.ai = data.ai || state.ai;
        state.quota = data.quota || state.quota;
        renderAIDisabled();
        renderDemos();
        return true;
      } catch (e) {
        toast('暂时无法读取填表状态：' + e.message);
        return false;
      } finally { stateRequest = null; }
    })();
    return stateRequest;
  }

  function renderAIDisabled() {
    // 没配 key 时按钮保持可点：点下去要给一句明确的说明，
    // 而不是留一个灰按钮让人猜原因。额度用完才是真的不能再点。
    // The button's own label is owned by renderQuota(), which knows both states.
    const off = !state.ai.configured;
    $('aiNotice').hidden = !off;
    if (off) {
      $('aiNoticeText').textContent =
        `服务器上没有配置模型 API Key（${state.ai.keyEnv}），提交会返回提示。` +
        '表格本身仍可以手动填写、打印和导出。';
    }
    state.aiOff = off;
    renderQuota();
  }

  // renderQuota draws today's budget. The limit is a server-side counter, not
  // a browser one, so entering a form re-reads it from /api/state.
  // Nothing here prints a count: the only visible state is "gone", and the
  // button says what happened the one time it matters.
  function renderQuota() {
    const q = state.quota;
    const spent = !!q && !q.unlimited && q.remaining <= 0;

    // The card lives in the studio, where the fill happens: it is the one place
    // where a spent budget is worth saying out loud.
    $('quotaSpent').hidden = !spent;
    if (spent) {
      $('quotaSpentText').textContent =
        '今天的次数用完了，明天 00:00 自动恢复。表格照常可以手动填写、打印和导出，已填的内容不会丢。';
    }

    const fill = $('fillBtn');
    if (spent) {
      fill.disabled = true;
      fill.textContent = '今日次数已用完 · 明天再来';
    } else {
      // No key is not the same as no budget: the button stays clickable so the
      // click can explain itself.
      fill.disabled = false;
      fill.textContent = state.aiOff ? 'AI 填写（未开通）' : '开始 AI 填写';
    }
  }

  // afterFill re-reads the budget so the count drops as soon as a fill lands.
  async function refreshQuota() {
    try {
      const res = await api('/api/state');
      state.quota = res.quota || state.quota;
      state.ai = res.ai || state.ai;
      renderQuota();
    } catch (e) { /* a stale count is not worth an error banner */ }
  }

  // Every card on the home page comes from this array. Adding a second demo is
  // one more entry here: nothing in renderDemos() names a specific tool.
  // `cat` drives the filter pills, `cover` picks the decorative cover art.
  const DEMO_CATS = [
    { key: 'school', label: '学校' },
    { key: 'trade', label: '外贸' },
    { key: 'production', label: '制作' },
    { key: 'tools', label: '工具' },
  ];

  const TRADE_CASES = [
    ['customer-email', '客户邮件与跟进', '邮件', '同一客户改了数量、包装和交期要求。接班同事如何读懂来龙去脉，准备一封不乱承诺的回复？'],
    ['quotation', '报价准备与审批', '报价', '工厂报得便宜就能选吗？把不同口径的成本算清，区分内部测算与对客报价。'],
    ['order-validation', '订单核对与录入', '订单', '客户采购单金额没算错，却多了 200 个、改了贸易和付款条款。先拦住，再核对。'],
  ].map(([key, title, glyph, desc]) => ({
    key, title, glyph, desc, cat: 'trade', cover: 'trade',
    typeLabel: '业务模拟 · 预置演示',
    tags: () => ['原始单据', {text: '人工审批', outline: true}],
    note: () => '未连接真实业务系统', cta: '查看案例',
    open: () => { window.location.href = BASE + '/' + key + '/'; },
  }));

  const DEMOS = [
    {
      key: 'ai-fill',
      cat: 'school',
      title: 'AI 填表',
      glyph: '填表',
      typeLabel: 'AI 语义匹配',
      cover: 'form',
      desc: '上传 Excel 或粘贴文字，自动填进学生表格。对不上的格子留空，绝不编造。',
      // Use cached template summaries if known; otherwise show generic copy.
      // Never invent a count, or hide this static card while waiting for data.
      tags: (tpls) => [
        tpls.length ? `共 ${tpls.length} 种表格` : '校对后导出',
        { text: 'Excel / CSV / 文字', outline: true },
      ],
      note: (tpls) => tpls.length ? tpls.map((t) => t.name).join(' · ') : '打开后选择表格',
      cta: '开始填写',
      open: () => openFill(),
    },
    {
      // A static page next to this one: no templates, no model call.
      key: 'lesson-plan',
      cat: 'school',
      title: '生成教案',
      glyph: '教案',
      typeLabel: '提示词示例',
      cover: 'slide',
      desc: '一段提示词，生成可以直接上课的 45 分钟教案和 18 页课件。提示词原文可一键复制。',
      tags: () => [
        '45 分钟 · 18 页课件',
        { text: '静态展示', outline: true },
      ],
      note: () => '人教版七上《从百草园到三味书屋》',
      cta: '查看教案',
      open: () => { window.location.href = BASE + '/lesson-plan.html'; },
    },
    {
      key: 'essay-review',
      cat: 'school',
      title: '批改作文',
      glyph: '作文',
      typeLabel: '示例 + 实时点评',
      cover: 'essay',
      desc: '看两组作文点评示例，也可粘贴图片或文字。用百字以内，点评一个优点，给出一个建议。',
      tags: () => ['图片 / 文字', { text: '第二人称 · 100 字以内', outline: true }],
      note: () => 'DeepSeek-V4.1 Flash',
      cta: '试试批改作文',
      open: () => { window.location.href = BASE + '/essay-review.html'; },
    },
    {
      key: 'math-review',
      cat: 'school',
      title: '数学判卷',
      glyph: '判卷',
      typeLabel: '示例 + 实时判卷',
      cover: 'math',
      desc: '上传或粘贴数学试卷照片，让 AI 逐题核算、判定和批注。也可先看两组预置示例。',
      tags: () => ['上传 / 粘贴图片', { text: '逐题分析', outline: true }],
      note: () => '初一数学 · 需教师复核',
      cta: '查看数学判卷',
      open: () => { window.location.href = BASE + '/math-review.html'; },
    },
  ];

  DEMOS.push(...TRADE_CASES, {
    key: 'pop-sticker-studio', cat: 'production', cover: 'stickers', title: '波普贴纸工作室',
    glyph: '贴纸', typeLabel: 'Agent Skill', keepTypeEnglish: true, desc: '先画贴纸，再用代码编排动作。同一套素材可以做成滚动互动网页，也可以导出 MP4。',
    tags: () => ['互动网页', { text: '视频', outline: true }],
    note: () => '复制网址即可让 Agent 安装', cta: '介绍与安装',
    open: () => { window.location.href = BASE + '/pop-sticker-studio/'; },
  }, {
    key: 'ink-story-film', cat: 'production', cover: 'ink', title: '水墨视频',
    glyph: '水墨', typeLabel: 'Agent Skill', keepTypeEnglish: true,
    desc: '用几幅静止的插画讲一个故事。先确认分镜，再试听旁白，最后用推拉和横移引导视线。',
    tags: () => ['分镜插画', { text: '视频', outline: true }],
    note: () => '示例：《雪夜访戴》', cta: '介绍与安装',
    open: () => { window.location.href = BASE + '/ink-story-film/'; },
  });

  DEMOS.push(
    { key: 'wogg', cat: 'tools', title: 'WOGG', url: 'https://www.wogg.cool', price: '每月 19 元' },
    { key: 'ikuuu', cat: 'tools', title: 'iKuuu', url: 'https://ikuuu.top', price: '每月 12 元' },
    { key: 'codex-app', cat: 'tools', title: 'Codex App', url: 'https://developers.openai.com/codex/app/', kind: '编程助手', desc: '在桌面上使用 OpenAI 的编程助手。' },
    { key: 'deepseek-harness', cat: 'tools', title: 'DeepSeek Harness', url: 'https://www.deepseek.com/harness/', kind: 'Agent 框架', desc: '用插件搭建和扩展自己的 DeepSeek 智能体。' },
    { key: 'pi-agent', cat: 'tools', title: 'Pi Agent', url: 'https://pi.dev/', kind: '终端编程助手', desc: '在终端里读写代码，按需添加扩展。' },
    { key: 'paseo', cat: 'tools', title: 'Paseo', url: 'https://paseo.sh/', kind: 'Agent 管理', desc: '从电脑或手机查看和管理多个编程助手。' },
  );

  // The cover art is decoration, not data: the form sketch mirrors the sample
  // the studio itself can load, and the slide is a real exported thumbnail.
  const COVER_FORM = `
    <div class="shot mform"><table>
      <tr><td class="l">姓名</td><td class="f">陈知远</td><td class="l">性别</td><td class="f">男</td></tr>
      <tr><td class="l">民族</td><td class="f">汉族</td><td class="l">学号</td><td class="f">20260312</td></tr>
      <tr><td class="l">住址</td><td class="f" colspan="3">苏州市吴中区宝带西路…</td></tr>
    </table></div>`;

  function demoCard(d, tpls) {
    if (d.cat === 'tools') return `
      <a class="demo" data-demo="${escapeHTML(d.key)}" data-cat="tools" href="${escapeHTML(d.url)}" target="_blank" rel="noopener noreferrer">
        <div class="body"><div class="tags"><span class="tg">${escapeHTML(d.price || d.kind)}</span></div>
          <h3 class="tool-title"><img src="tool-icons/${escapeHTML(d.key)}.png" width="36" height="36" alt="">${escapeHTML(d.title)}</h3><p>${escapeHTML(d.desc || d.url)}</p>
          <div class="foot"><span>${escapeHTML(d.price ? '价格以官网为准' : new URL(d.url).hostname)}</span><span class="btn btn-b btn-sm">访问网站 ↗</span></div>
        </div>
      </a>`;
    const tags = (d.tags ? d.tags(tpls) : []).map((raw) => {
      const t = typeof raw === 'string' ? { text: raw } : raw;
      return `<span class="tg${t.outline ? ' o' : ''}">${escapeHTML(t.text)}</span>`;
    }).join('');
    const cover = d.cover === 'ink'
      ? '<img class="shot" src="ink-story-film/snow-night-visit/poster.jpg" alt="" width="190" height="107" loading="lazy">'
      : d.cover === 'stickers'
      ? '<img class="shot" src="pop-sticker-studio/style-board.jpg" alt="" width="190" height="107" loading="lazy">'
      : d.cover === 'trade'
      ? '<div class="shot" style="padding:22px;background:#fff;border-radius:12px;font-size:14px;line-height:2">原始材料 → 整理校验<br>人工确认 → 再往下走</div>'
      : d.cover === 'slide'
      ? '<img class="shot" src="lesson/thumbs/slide-01.webp?v=553fc7142a8c" alt="" width="190" height="107">'
      : d.cover === 'essay'
        ? '<img class="shot" src="essay/example-aunt.png" alt="" width="190" height="107">'
        : d.cover === 'math'
          ? '<img class="shot" src="math/example-01.png" alt="" width="190" height="107">'
          : COVER_FORM;
    const note = d.note ? d.note(tpls) : '';
    return `
      <article class="demo" data-demo="${escapeHTML(d.key)}" data-cat="${escapeHTML(d.cat || '')}">
        <div class="cover">
          <span class="glyph" aria-hidden="true">${escapeHTML(d.glyph || '')}</span>
          ${cover}
          <span class="type${d.keepTypeEnglish ? ' notranslate' : ''}"${d.keepTypeEnglish ? ' lang="en" translate="no"' : ''}><i></i>${escapeHTML(d.typeLabel || '')}</span>
        </div>
        <div class="body">
          <div class="tags">${tags}</div>
          <h3>${escapeHTML(d.title)}</h3>
          <p>${escapeHTML(d.desc)}</p>
          <div class="foot">
            <span>${escapeHTML(note)}</span>
            <button class="btn btn-b btn-sm" type="button">${escapeHTML(d.cta || '打开')}</button>
          </div>
        </div>
      </article>`;
  }

  function renderDemos() {
    const box = $('demoCards');
    const tpls = state.templates || [];
    const items = DEMOS.filter((d) => !d.available || d.available(tpls));
    box.querySelectorAll('[data-demo], .notice').forEach((n) => n.remove());
    $('demoFilters').innerHTML = '';
    if (!items.length) {
      applyFilter();
      return;
    }
    $('demoSoon').insertAdjacentHTML('beforebegin', items.map((d) => demoCard(d, tpls)).join(''));
    // The button has no handler of its own: its click bubbles to the card, so
    // mouse and keyboard land on the same code path exactly once.
    box.querySelectorAll('[data-demo]').forEach((card) => {
      const d = DEMOS.find((x) => x.key === card.dataset.demo);
      if (!d || !d.open) return;
      card.addEventListener('click', () => d.open(tpls));
    });
    renderFilters(items);
  }

  // Pills exist only for categories that have a card, and the count is the
  // number of cards that actually rendered — never a number typed by hand.
  function renderFilters(items) {
    const box = $('demoFilters');
    const counts = {};
    items.forEach((d) => { counts[d.cat] = (counts[d.cat] || 0) + 1; });
    const cats = DEMO_CATS.filter((c) => c.key === 'all' || counts[c.key]);
    if (!cats.some((c) => c.key === state.filter)) state.filter = cats[0]?.key || 'school';
    box.innerHTML = cats.map((c) => {
      const on = c.key === state.filter;
      const label = c.key === 'all' ? `${c.label} · ${items.length}` : c.label;
      return `<button type="button" class="${on ? 'on' : ''}" data-cat="${c.key}" aria-pressed="${on}">${escapeHTML(label)}</button>`;
    }).join('');
    box.querySelectorAll('button[data-cat]').forEach((b) => {
      b.addEventListener('click', () => { state.filter = b.dataset.cat; history.replaceState(null, '', '#' + state.filter); applyFilter(); });
    });
    applyFilter();
  }

  function applyFilter() {
    saveCatalog();
    $('categoryTitle').textContent = ({ school: '学校 · 日常教学小工具', trade: '外贸 · 把一笔生意往前推进', production: '制作 · 从素材到作品', tools: '工具 · 常用网址' })[state.filter];
    $('demoCards').querySelectorAll('[data-demo]').forEach((card) => {
      card.hidden = state.filter !== 'all' && card.dataset.cat !== state.filter;
    });
    // The "next up" slot is a placeholder, not a demo: it is never counted and
    // only shows while everything is on screen.
    $('demoSoon').hidden = state.filter !== 'all';
    $('demoFilters').querySelectorAll('button[data-cat]').forEach((b) => {
      const on = b.dataset.cat === state.filter;
      b.classList.toggle('on', on);
      b.setAttribute('aria-pressed', on ? 'true' : 'false');
    });
  }

  // One entry point for every "fill a form" affordance on the page: the nav
  // link, 开始填表 and the AI 填表 card all land here.
  let openingFill = false;
  async function openFill() {
    // Already filling? Don't throw away the current sheet.
    if (!$('studio').hidden) {
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    if (openingFill) return;
    openingFill = true;
    try {
      if (!await loadState()) return;
      if (!state.templates.length) { toast('暂时没有可用的表格。'); return; }
      await openStudio(pickTemplate(state.templates).slug, { autoDemo: true, stateReady: true });
    } finally { openingFill = false; }
  }

  // A bookmarked hash wins; otherwise the first template in the folder opens.
  function pickTemplate(tpls) {
    const wanted = slugFromHash();
    const list = tpls || state.templates;
    if (wanted && list.some((t) => t.slug === wanted)) return list.find((t) => t.slug === wanted);
    return list[0];
  }

  // --- sample data ----------------------------------------------------------
  const demoDate = () => {
    const d = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  };

  // The sample is a whitelist, not a rule engine: only the fields named here
  // get a value and every other cell stays empty, so the sheet reads like a
  // form a parent filled halfway instead of a fabricated student record.
  const DEMO_ADDRESS = '江苏省苏州市吴中区长桥街道宝带西路 218 号 6 幢 502 室';
  const DEMO_STUDENT = '陈知远';
  const DEMO_FATHER = '陈立国';
  const DEMO_MOTHER = '周雅琴';
  // Placeholder numbers: a real-looking prefix with an all-zero subscriber
  // part, which no carrier hands out, so nothing here reaches a person.
  const DEMO_FATHER_PHONE = '138-0000-0000';
  const DEMO_MOTHER_PHONE = '139-0000-0000';

  // Labels are shared between the two templates, so one table fills both. A
  // value whose label is absent from a template is simply never looked up.
  const DEMO_VALUES = {
    // 学生入学登记表 / 信息采集表 共有的身份项
    '姓名': DEMO_STUDENT,
    '性别': '男',
    '出生年月': '2013年4月',
    '民族': '汉族',
    '学号': '20260312',
    '班主任': '刘敏',

    // 入学登记表专属
    '登记姓名': DEMO_STUDENT,
    '毕业学校': '苏州市吴中区城南小学',
    '身高': '152cm',
    '详细家庭住址': DEMO_ADDRESS,
    '小学担任职务': '学习委员',
    '特长': '围棋、书法',
    '家庭主要经济来源': '父母工资收入',
    '家庭成员1关系': '父亲',
    '家庭成员2关系': '母亲',
    '家庭成员1手机号码1': DEMO_FATHER_PHONE,
    '家庭成员2手机号码1': DEMO_MOTHER_PHONE,

    // 信息采集表专属
    '登记日期': demoDate,
    '年龄': '13',
    '籍贯': '江苏苏州',
    '是否独子': '是',
    '户口所在地': '江苏省苏州市吴中区',
    '现居地址': DEMO_ADDRESS,
    '曾就读学校1起止年月': '2019年9月—2026年6月',
    '曾就读学校1名称': '苏州市吴中区城南小学',
    '现就读班级': '初一（3）班',
    '入学年月': '2026年9月',
    '健康状况': '良好',
    '是否有不适于运动的疾病': '否',
    '兴趣爱好第一行': '阅读、天文',
    '个人特长第一行': '围棋、书法',

    // 家庭成员 1 / 2（两张表编号对应的称谓不同）
    '家庭成员1姓名': DEMO_FATHER,
    '家庭成员1称谓': '父亲',
    '家庭成员1年龄': '43',
    '家庭成员1工作单位': '苏州华远精密机械有限公司',
    '家庭成员1职业': '工程师',
    '家庭成员1联系电话': DEMO_FATHER_PHONE,
    '家庭成员2姓名': DEMO_MOTHER,
    '家庭成员2称谓': '母亲',
    '家庭成员2年龄': '41',
    '家庭成员2工作单位': '苏州市吴中区实验小学',
    '家庭成员2职业': '教师',
    '家庭成员2联系电话': DEMO_MOTHER_PHONE,
  };

  function demoValueFor(f) {
    const label = (f.label || '').trim();
    const raw = DEMO_VALUES[label];
    if (raw === undefined) return '';
    const v = typeof raw === 'function' ? raw() : raw;
    // A radio group only accepts its own labels; anything else would leave the
    // group unusable, so an unknown answer drops the cell back to empty.
    if (f.kind === 'choice') {
      const opts = f.options || [];
      return opts.includes(v) ? v : '';
    }
    return v || '';
  }

  function demoValues(fields) {
    const out = {};
    (fields || []).forEach((f) => {
      const v = demoValueFor(f);
      if (v) out[f.id] = v;
    });
    return out;
  }

  // A sample has to read like the material a parent actually sends: a few
  // sentences in their own words, never the form's own labels. Mirroring the
  // labels would hide the one thing the demo exists to show, which is the
  // model reading prose and finding the right cells by itself.
  function demoSourceText() {
    return [
      '示例资料（虚构，仅用于演示）',
      '',
      `孩子叫${DEMO_STUDENT}，男孩，2013 年 4 月出生，汉族，今年 13 岁，` +
        '独生子，在苏州出生长大，籍贯苏州。今年 9 月刚上初一（3）班，' +
        '学号 20260312，班主任是刘敏老师。',
      '',
      '小学念的是苏州市吴中区城南小学，2019 年 9 月入学，2026 年 6 月毕业，' +
        '毕业那会儿身高 152 厘米，在班上当过学习委员。',
      '',
      `我们家住${DEMO_ADDRESS}，户口也在吴中区。` +
        `爸爸${DEMO_FATHER}，43 岁，在苏州华远精密机械有限公司做工程师，手机 ${DEMO_FATHER_PHONE}。` +
        `妈妈${DEMO_MOTHER}，41 岁，在苏州市吴中区实验小学当老师，手机 ${DEMO_MOTHER_PHONE}。`,
      '',
      '孩子平时喜欢看书、看星星，围棋和书法练了几年。' +
        '家里收入就靠我们俩的工资。身体一直挺好，没有什么不能上体育课的毛病。',
      '',
      `今天是 ${demoDate()}，表是班主任发的，晚上填好交回去。`,
    ].join('\n');
  }

  // Every filled cell has to be sayable from the text above: a value the
  // sample never mentions would be the model inventing, which is the one
  // thing this demo promises it does not do.

  // The sample goes into the material pane only. The sheet stays empty until
  // the model fills it, because the thing worth watching is the model reading
  // prose and choosing the cells itself; a pre-painted sheet would show the
  // result and hide the work.
  function fillDemo() {
    if (!state.current) return;
    $('pasteBox').value = demoSourceText();
    state.draft.text = $('pasteBox').value;
    state.demo = false;
    setStatus('', '');
    toast('示例资料已放进左边，点「开始 AI 填写」看看它怎么填表');
  }

  // An AI fill that reproduces the sample is still a sample: the export keeps
  // its marker even though none of those cells was typed by hand.
  function isSampleFill(sess) {
    if (!state.current) return false;
    const values = (sess && sess.values) || {};
    const sample = demoValues(state.current.fields);
    const ids = Object.keys(values).filter((id) => String(values[id] || '').trim());
    if (!ids.length) return false;
    return ids.every((id) => String(values[id]).trim() === String(sample[id] || '').trim());
  }

  // Any real input drops the sample state: once a cell no longer holds the
  // generated value the sheet is the user's own work and the export is theirs.
  function clearDemo() {
    if (!state.demo) return;
    state.demo = false;
  }

  // --- studio ---------------------------------------------------------------
  // The header is shared between the list and the studio, so it has to say
  // where the reader is: the home list shows only the logo and the site link,
  // while a case has one explicit return control in the top-right corner.
  function setNav(which) {
    const home = which === 'home';
    document.body.classList.toggle('is-home', home);
    $('backBtn').hidden = home;
    $('homeSiteLink').hidden = !home;
  }

  // A template is openable from the URL, so a teacher can bookmark one form
  // instead of walking through the list every time.
  function slugFromHash() {
    return decodeURIComponent((location.hash || '').replace(/^#\/?/, ''));
  }

  async function openStudio(slug, opts = {}) {
    const { sessionId = '', draft = null, autoDemo = false, stateReady = false } = opts;
    if (!stateReady && !await loadState()) return;
    if (!state.templates.some(t => t.slug === slug)) { toast('这张表格已不可用，请重新选择。'); return; }
    let data;
    try {
      data = await api('/api/templates/' + encodeURIComponent(slug));
    } catch (e) { toast(e.message); return; }

    state.current = data;
    state.sessionId = '';
    state.values = {};
    state.dirty = false;
    state.demo = false;
    if (slugFromHash() !== slug) history.replaceState(null, '', '#' + encodeURIComponent(slug));
    $('studio').hidden = false;
    $('app').hidden = true;
    setNav('fill');
    window.scrollTo({ top: 0 });

    resetPanel();
    if (draft) restoreDraft(draft);
    renderTplSwitch();
    mountPaper(data.html);
    // Both export and save work on a hand-filled form too: they create a
    // session on first use instead of waiting for an AI fill.
    $('exportBtn').disabled = false;
    $('saveBtn').disabled = true;
    $('fillPill').hidden = true;
    $('warnBox').hidden = true;

    if (sessionId) {
      try {
        const res = await api('/api/sessions/' + encodeURIComponent(sessionId));
        applySession(res.session);
      } catch (e) { toast(e.message); }
    } else if (autoDemo) {
      // Only a direct entry gets the sample: switching forms must leave the
      // sheet empty, or a stale sample is one click away from being exported.
      fillDemo();
    }
    fitPaper();
  }

  function closeStudio() {
    state.filter = 'school';
    $('studio').hidden = true;
    $('app').hidden = false;
    setNav('home');
    history.replaceState(null, '', '#' + state.filter);
    state.current = null;
    state.doc = null;
    state.demo = false;
    // The cards and their cached metadata are already here; no return fetch.
    applyFilter();
    window.scrollTo({ top: 0 });
    document.querySelector('#demoFilters [data-cat="school"]')?.focus({ preventScroll: true });
  }

  // The tab list is built from /api/state, so a file added to templates/
  // appears here without any front-end change.
  function renderTplSwitch() {
    const box = $('tplSwitch');
    const many = state.templates.length > 1;
    box.hidden = !many;
    $('tplSwitchNote').hidden = !many;
    if (!many) {
      box.innerHTML = '';
      // With one form the tab list is gone, so the bar still has to say which
      // form is open; with several the tabs say it and the bar stays generic.
      $('studioTitle').textContent = (state.current && state.current.name) || 'AI 填表';
      return;
    }
    $('studioTitle').textContent = 'AI 填表';
    box.innerHTML = state.templates.map((t) => {
      const on = !!state.current && state.current.slug === t.slug;
      return `<button type="button" role="tab" class="tpl-tab${on ? ' on' : ''}" data-slug="${escapeHTML(t.slug)}" aria-selected="${on ? 'true' : 'false'}">${escapeHTML(t.name)}<small>· ${escapeHTML(String(t.fieldCount))} 字段</small></button>`;
    }).join('');
    box.querySelectorAll('button[data-slug]').forEach((b) => {
      b.addEventListener('click', () => {
        if (state.current && b.dataset.slug === state.current.slug) return;
        switchTemplate(b.dataset.slug);
      });
    });
  }

  async function switchTemplate(slug) {
    if (!state.current || slug === state.current.slug) return;
    if (state.dirty && !confirm('当前表格有未保存的修改，切换会丢失，继续吗？')) return;
    // Two forms ask about the same student from different angles; making the
    // teacher re-upload the file after every switch would be the wrong kind of
    // clean slate. Answers are dropped, the material is not. Passing the draft
    // also keeps the sample data out of the switched-to form.
    const draft = captureDraft();
    await openStudio(slug, { draft });
  }

  function resetPanel() {
    $('pasteBox').value = '';
    $('fileInput').value = '';
    $('dzSub').textContent = '支持 .xlsx / .csv / .txt，最大 8MB';
    $('dropzone').classList.remove('has-file');
    state.draft = { file: null, text: '' };
    setStatus('', '');
  }

  // What the user gave us lives in the panel, so a switch has to copy it out
  // before resetPanel runs and put it back into the fresh panel afterwards.
  function captureDraft() {
    return {
      file: ($('fileInput').files && $('fileInput').files[0]) || null,
      text: $('pasteBox').value || '',
    };
  }

  function restoreDraft(draft) {
    state.draft = draft || { file: null, text: '' };
    $('pasteBox').value = state.draft.text;
    if (state.draft.file) {
      // input.files is read-only; a DataTransfer is the script-side way in.
      const dt = new DataTransfer();
      dt.items.add(state.draft.file);
      $('fileInput').files = dt.files;
    } else {
      $('fileInput').value = '';
    }
    // Reuse the change handler so #dzSub and the dropzone highlight follow.
    $('fileInput').dispatchEvent(new Event('change'));
  }

  function mountPaper(html) {
    const frame = $('paper');
    const doc = frame.contentDocument || frame.contentWindow.document;
    doc.open();
    doc.write(html);
    doc.close();
    state.doc = doc;
    decorateFrame();
    // The frame is a document we control: every slot becomes editable and
    // reports back, so the parent never has to know the form's layout.
    bindSlots();
    fitPaper();
    setTimeout(fitPaper, 60);
  }

  function decorateFrame() {
    const doc = state.doc;
    if (!doc || !doc.head) return;
    const style = doc.createElement('style');
    style.textContent = `
      [data-field-id] { position: relative; transition: background .15s ease; }
      [data-field-id]:hover { background: #eef2fb !important; }
      [data-field-id]:focus { outline: 2px solid #24408e !important; outline-offset: -2px; background: #f2f5fd !important; }
      [data-field-id].ai-filled { background: #f1f5fc; }
      .choices[data-field-id] { background: transparent; }
      .choices[data-field-id].ai-filled { background: #f1f5fc; }
      @media print {
        [data-field-id].ai-filled { background: transparent !important; }
        [data-field-id]:hover { background: transparent !important; }
      }`;
    doc.head.appendChild(style);
  }

  function bindSlots() {
    const doc = state.doc;
    if (!doc) return;
    const slots = doc.querySelectorAll('[data-field-id]');
    slots.forEach((el) => {
      const id = el.getAttribute('data-field-id');
      if (el.getAttribute('data-field-kind') === 'choice') {
        el.querySelectorAll('input[type="radio"]').forEach((r) => {
          r.addEventListener('change', () => {
            if (!r.checked) return;
            setValue(id, r.value, { fromFrame: true });
            el.classList.add('ai-filled');
          });
        });
        return;
      }
      el.setAttribute('contenteditable', 'true');
      el.addEventListener('focus', () => {
        const ph = el.getAttribute('data-placeholder') || '';
        if (ph && (el.innerText || '').trim() === ph) el.textContent = '';
      });
      const read = () => {
        const text = (el.innerText || '').replace(/\u00a0/g, ' ').replace(/\n+$/, '').trim();
        return text === (el.getAttribute('data-placeholder') || '') ? '' : text;
      };
      el.addEventListener('input', () => setValue(id, read(), { fromFrame: true }));
      el.addEventListener('blur', () => {
        const text = read();
        if (text !== (state.values[id] || '')) setValue(id, text, { fromFrame: true });
      });
    });
  }

  function setValue(id, text, opts = {}) {
    // A keystroke in the sheet means the generated sample is being replaced,
    // even when the text happens to match what was already there.
    if (opts.fromFrame) clearDemo();
    const clean = String(text || '');
    if ((state.values[id] || '') === clean) return;
    state.values[id] = clean;
    state.dirty = true;
    $('saveBtn').disabled = !state.sessionId;
    if (!opts.fromFrame) paintField(id);
  }

  function paintField(id) {
    const doc = state.doc;
    if (!doc) return;
    const el = doc.querySelector(`[data-field-id="${id}"]`);
    if (!el) return;
    const val = state.values[id] || '';
    const placeholder = el.getAttribute('data-placeholder') || '';
    if (el.getAttribute('data-field-kind') === 'choice') {
      el.querySelectorAll('input[type="radio"]').forEach((r) => { r.checked = (r.value === val); });
    } else {
      const shown = (el.innerText || '').trim();
      if (val === '' && placeholder !== '') {
        // An empty answer restores the template's own caption (照片), not a
        // blank cell: the form is printed, and that caption belongs on it.
        if (shown !== placeholder) el.textContent = placeholder;
      } else if (shown !== val.trim()) {
        el.textContent = val;
      }
    }
    el.classList.toggle('ai-filled', val !== '');
  }

  function applyValues(values) {
    state.values = Object.assign({}, values);
    Object.keys(state.values).forEach((id) => paintField(id));
  }

  function applySession(sess) {
    if (!sess) return;
    state.sessionId = sess.id;
    applyValues(sess.values || {});
    state.dirty = false;
    $('saveBtn').disabled = true;
    $('exportBtn').disabled = false;
    const pill = $('fillPill');
    pill.hidden = false;
    if (sess.status === 'filled') {
      const n = Object.keys(sess.values || {}).length;
      const total = state.current ? state.current.fields.length : 0;
      pill.className = 'fpill';
      pill.textContent = `AI 填了 ${n}/${total} 项`;
      setStatus('ok', `AI 已填写 <b>${n}</b> 项（共 ${total} 项），其余留空。请核对后导出。`);
    } else if (sess.status === 'manual') {
      // A hand-filled form is a normal outcome, not a failure: say so plainly
      // and leave the status line alone so it does not wipe a real message.
      const n = Object.keys(sess.values || {}).length;
      const total = state.current ? state.current.fields.length : 0;
      pill.className = 'fpill manual';
      pill.textContent = `手动填写 ${n}/${total} 项`;
    } else {
      pill.className = 'fpill fail';
      pill.textContent = statusLabel[sess.status] || sess.status;
      if (sess.error) setStatus('err', escapeHTML(sess.error));
    }
    if (sess.warnings && sess.warnings.length) {
      $('warnBox').hidden = false;
      $('warnBox').innerHTML = '<ul>' + sess.warnings.map((w) => `<li>${escapeHTML(w)}</li>`).join('') + '</ul>';
    } else {
      $('warnBox').hidden = true;
    }
  }

  function collectValues() {
    // Trust the frame's live text over the cached map, in case a slot was
    // edited without an input event (autofill, paste via context menu).
    const doc = state.doc;
    if (doc) {
      doc.querySelectorAll('[data-field-id]').forEach((el) => {
        const id = el.getAttribute('data-field-id');
        if (el.getAttribute('data-field-kind') === 'choice') {
          const checked = el.querySelector('input[type="radio"]:checked');
          state.values[id] = checked ? checked.value : '';
          return;
        }
        const text = (el.innerText || '').replace(/\u00a0/g, ' ').replace(/\n+$/, '').trim();
        // Untouched placeholder text is not an answer.
        state.values[id] = text === (el.getAttribute('data-placeholder') || '') ? '' : text;
      });
    }
    const out = {};
    Object.keys(state.values).forEach((k) => {
      const v = (state.values[k] || '').trim();
      if (v) out[k] = v;
    });
    return out;
  }

  // The sheet is 794px wide no matter what; the browser window is not. Scaling
  // the wrapper keeps the paper's own layout (and therefore print/PDF) intact
  // while the column on screen stays free of a horizontal scrollbar.
  function fitPaper() {
    const frame = $('paper');
    const doc = state.doc;
    if (!doc || !doc.documentElement) return;
    const contentH = Math.max(
      doc.documentElement.scrollHeight || 0,
      doc.body ? doc.body.scrollHeight : 0,
      1120,
    ) + 8;
    frame.style.height = contentH + 'px';
    $('paperFit').style.height = contentH + 'px';

    const scroll = $('paperScroll');
    const cs = getComputedStyle(scroll);
    const avail = scroll.clientWidth
      - (parseFloat(cs.paddingLeft) || 0)
      - (parseFloat(cs.paddingRight) || 0);
    const k = avail > 0 ? Math.min(1, avail / PAPER_W) : 1;

    $('paperFit').style.transform = k < 1 ? `scale(${k})` : 'none';
    // The holder is what takes up room in the scroll area: without its own
    // size the document would keep the untransformed 794 x contentH box and
    // leave either a dead gutter or a scrollbar behind.
    $('paperHolder').style.width = Math.floor(PAPER_W * k) + 'px';
    $('paperHolder').style.height = Math.ceil(contentH * k) + 'px';
  }

  function resetPaperScale() {
    $('paperFit').style.transform = 'none';
    $('paperHolder').style.width = PAPER_W + 'px';
  }

  // --- filling --------------------------------------------------------------
  async function doFill() {
    if (!state.current) return;
    const slug = state.current.slug;
    if (!state.ai.configured) {
      // 没配 key 也把资料读一遍再提示，这样用户能确认文件本身没问题，
      // 也保证「上传 → 报错」这条链路在没 key 时依然走通。
      const picked = $('fileInput').files[0];
      const typed = $('pasteBox').value.trim();
      if (!picked && !typed) {
        setStatus('err', '请先选一个文件，或粘贴一些文字。');
        return;
      }
      setStatus(
        'err',
        'AI 填写还没开通：服务器上缺少模型 API Key（' +
          state.ai.keyEnv +
          '）。请把 key 交给管理员配置后重试；你现在也可以直接在右侧表格里手动填写。'
      );
      return;
    }
    const file = $('fileInput').files[0];
    const text = $('pasteBox').value.trim();
    if (!file && !text) {
      setStatus('err', '请先选一个文件，或粘贴一些文字。');
      return;
    }

    $('fillBtn').disabled = true;
    // A reasoning model can take a while on the first call (cold connection),
    // so say so rather than letting the button look stuck.
    setStatus('info', '<span class="spin"></span>正在读取资料并让 AI 对齐字段，可能要十几秒，请不要关掉页面…');
    $('warnBox').hidden = true;

    try {
      let res;
      if (file) {
        const fd = new FormData();
        fd.append('template', slug);
        fd.append('file', file);
        res = await api('/api/fill', { method: 'POST', body: fd });
      } else {
        res = await api('/api/fill', {
          method: 'POST',
          body: JSON.stringify({ template: slug, text }),
        });
      }
      // The user may have switched forms while the request was in flight; the
      // answer belongs to the form they left, not the one now on screen.
      if (!state.current || state.current.slug !== slug) return;
      // The sheet now holds real answers, so the sample marker must go: the
      // exported file is no longer a demo.
      clearDemo();
      applySession(res.session);
      if (isSampleFill(res.session)) {
        state.demo = true;
          }
      refreshQuota();
      const n = Object.keys((res.session && res.session.values) || {}).length;
      const filled = Object.keys(collectValues()).length;
      if (n === 0 && filled === 0) {
        setStatus('err', 'AI 没有从这段资料里找到能填的内容。可能是资料太短，或与表格要求的项目无关。');
      } else {
        toast(`已填 ${n} 项`);
      }
      fitPaper();
    } catch (e) {
      if (e.code === 'QUOTA_EXCEEDED') {
        // The server is the authority on the budget; take its count so the
        // page agrees with what the next request would see.
        if (e.quota) { state.quota = e.quota; }
        else if (state.quota) { state.quota = Object.assign({}, state.quota, { remaining: 0, used: state.quota.limit }); }
        renderQuota();
      }
      if (!state.current || state.current.slug !== slug) return;
      setStatus('err', escapeHTML(e.message));
    } finally {
      if (!state.quota || state.quota.unlimited || state.quota.remaining > 0) {
        $('fillBtn').disabled = false;
      }
    }
  }

  async function saveEdits() {
    if (!state.current) return;
    const values = collectValues();
    if (!state.sessionId) {
      $('saveBtn').disabled = true;
      try {
        const res = await api('/api/sessions', {
          method: 'POST',
          body: JSON.stringify({ template: state.current.slug, values }),
        });
        applySession(res.session);
        setStatus('ok', `已保存 <b>${Object.keys(values).length}</b> 项。`);
        $('saveBtn').disabled = true;
      } catch (e) {
        setStatus('err', escapeHTML(e.message));
        $('saveBtn').disabled = false;
      }
      return;
    }
    $('saveBtn').disabled = true;
    try {
      const res = await api('/api/sessions/' + encodeURIComponent(state.sessionId), {
        method: 'PATCH',
        body: JSON.stringify({ values }),
      });
      state.dirty = false;
      toast('已保存 ' + Object.keys(values).length + ' 项');
      if (res.session) applySession(res.session);
      state.dirty = false;
      $('saveBtn').disabled = true;
    } catch (e) {
      toast(e.message);
      $('saveBtn').disabled = false;
    }
  }

  async function exportXLSX() {
    // Export reads the values back from a stored session, so a form the user
    // filled by hand (or is filling while AI is not configured) needs one
    // created first; otherwise the button would do nothing.
    // The demo flag is read before the session round-trips: saving must not be
    // what decides whether the file is marked as a sample.
    const demo = state.demo;
    const values = collectValues();
    if (!state.sessionId) {
      if (!state.current) return;
      try {
        const res = await api('/api/sessions', {
          method: 'POST',
          body: JSON.stringify({ template: state.current.slug, values }),
        });
        applySession(res.session);
      } catch (e) {
        setStatus('err', escapeHTML(e.message));
        return;
      }
    } else if (state.dirty) {
      try {
        const res = await api('/api/sessions/' + encodeURIComponent(state.sessionId), {
          method: 'PATCH',
          body: JSON.stringify({ values }),
        });
        if (res.session) applySession(res.session);
      } catch (e) {
        setStatus('err', escapeHTML(e.message));
        return;
      }
    }
    let url = BASE + '/api/sessions/' + encodeURIComponent(state.sessionId) + '/xlsx';
    // The server stamps the file name and the first row, so a sample that gets
    // shared by mistake still says what it is.
    if (demo) url += '?demo=1';
    window.location.href = url;
  }

  function printForm() {
    const frame = $('paper');
    if (!frame.contentWindow) return;
    fitPaper();
    // A transformed frame is printed by some Chrome builds at the preview
    // scale, so drop the zoom for the duration of the print and put it back
    // afterwards.
    resetPaperScale();
    frame.contentWindow.focus();
    frame.contentWindow.print();
    setTimeout(fitPaper, 0);
  }

  // --- wiring ---------------------------------------------------------------
  $('backBtn').addEventListener('click', closeStudio);
  $('fillBtn').addEventListener('click', doFill);
  $('saveBtn').addEventListener('click', saveEdits);
  $('exportBtn').addEventListener('click', exportXLSX);
  $('printBtn').addEventListener('click', printForm);
  $('demoBtn').addEventListener('click', fillDemo);
  // Clearing the panel leaves the sample rows on the sheet, so the demo mark
  // has to stay: without it the exported file loses its "do not use" label.
  $('clearBtn').addEventListener('click', () => { resetPanel(); });

  document.querySelectorAll('[data-open-fill]').forEach((el) => {
    el.addEventListener('click', (e) => { e.preventDefault(); openFill(); });
  });

  // 全部 demo and the logo mean the same thing as the studio's back link: leave
  // the form and go back to the list. On the list itself they keep behaving
  // like ordinary links (scroll to the grid / reload the page).
  document.querySelectorAll('[data-home-link]').forEach((el) => {
    el.addEventListener('click', (e) => {
      if ($('studio').hidden) return;
      e.preventDefault();
      closeStudio();
      window.scrollTo({ top: 0 });
    });
  });

  // "手动填写这张表" on the spent-budget card: the form is still there and still
  // editable, so take the reader to it instead of leaving them at the notice.
  $('manualBtn').addEventListener('click', () => {
    const area = $('paperScroll');
    if (!area) return;
    area.scrollIntoView({ behavior: 'smooth', block: 'start' });
    const doc = state.doc;
    const first = doc && doc.querySelector('[data-field-id]');
    if (first && first.focus) first.focus({ preventScroll: true });
  });

  $('fileInput').addEventListener('change', () => {
    const f = $('fileInput').files[0];
    const dz = $('dropzone');
    if (!f) { dz.classList.remove('has-file'); $('dzSub').textContent = '支持 .xlsx / .csv / .txt，最大 8MB'; return; }
    clearDemo();
    dz.classList.add('has-file');
    $('dzSub').textContent = `${f.name} · ${(f.size / 1024).toFixed(0)} KB`;
  });

  ['dragenter', 'dragover'].forEach((ev) => $('dropzone').addEventListener(ev, (e) => {
    e.preventDefault(); $('dropzone').classList.add('drag');
  }));
  ['dragleave', 'drop'].forEach((ev) => $('dropzone').addEventListener(ev, (e) => {
    e.preventDefault(); $('dropzone').classList.remove('drag');
  }));
  $('dropzone').addEventListener('drop', (e) => {
    const f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
    if (!f) return;
    const dt = new DataTransfer();
    dt.items.add(f);
    $('fileInput').files = dt.files;
    $('fileInput').dispatchEvent(new Event('change'));
  });

  window.addEventListener('beforeunload', (e) => {
    if (state.dirty) { e.preventDefault(); e.returnValue = ''; }
  });

  // The paper column changes width with the window, the side panel and the
  // grid, so re-fit on whichever of those moves first. rAF-coalescing keeps a
  // resize drag from recomputing the sheet on every pixel.
  let fitPending = false;
  function scheduleFit() {
    if (fitPending) return;
    fitPending = true;
    requestAnimationFrame(() => { fitPending = false; fitPaper(); });
  }
  window.addEventListener('resize', scheduleFit);
  window.addEventListener('orientationchange', scheduleFit);
  if (window.ResizeObserver) {
    // Observe the column, not the scroll box: the scroll box's own size depends
    // on the fit, which would make the observer chase its own output.
    new ResizeObserver(scheduleFit).observe(document.querySelector('.paper-area'));
  }

  // replaceState does not fire hashchange, so this only runs for back/forward
  // or a hand-edited URL. Form slugs are checked against fresh server metadata.
  window.addEventListener('hashchange', () => {
    const slug = slugFromHash();
    if (!slug || slug === 'demos' || isCategory(slug)) {
      if (isCategory(slug)) state.filter = slug;
      if (!$('studio').hidden) closeStudio();
      else { applyFilter(); setNav('home'); }
      return;
    }
    if (slug === 'fill') { openFill(); return; }
    if (state.current && state.current.slug === slug) return;
    if ($('studio').hidden) openStudio(slug, { autoDemo: true });
    else switchTemplate(slug);
  });

  // A reload starts from a clean sheet: the template opens, but nothing is
  // painted into it and the material pane is empty. Only an in-page arrival
  // (the card, the sample button, back/forward) brings the sample in, because
  // someone who refreshed is about to type their own data and does not want a
  // half-filled form they have to clear first.
  // Synchronous first render: static cases don't wait for any backend response.
  renderDemos();
  const initialSlug = slugFromHash();
  if (initialSlug === 'fill') openFill();
  else if (initialSlug && initialSlug !== 'demos' && !isCategory(initialSlug)) openStudio(initialSlug);
  else setNav('home');
})();
