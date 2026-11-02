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

  // The paper keeps its 794px (A4 at 96dpi) logical width: print and PDF need
  // it. Anything narrower is handled by scaling the wrapper, not the sheet.
  const PAPER_W = 794;

  const state = {
    templates: [],
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
  };

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
  async function loadState() {
    let data;
    try {
      data = await api('/api/state');
    } catch (e) {
      $('demoCards').innerHTML = `<div class="notice"><strong>读取失败。</strong><span>${escapeHTML(e.message)}</span></div>`;
      return;
    }
    state.templates = data.templates || [];
    state.ai = data.ai || state.ai;
    state.quota = data.quota || state.quota;
    renderAIDisabled();
    renderDemos();
  }

  function renderAIDisabled() {
    // 没配 key 时按钮保持可点：点下去要给一句明确的说明，
    // 而不是留一个灰按钮让人猜原因。额度用完才是真的不能再点。
    const off = !state.ai.configured;
    $('aiNotice').hidden = !off;
    if (off) {
      $('aiNoticeText').textContent =
        `服务器上没有配置 DeepSeek API Key（${state.ai.keyEnv}），提交会返回提示。` +
        '表格本身仍可以手动填写、打印和导出。';
      $('fillBtn').textContent = 'AI 填写（未开通）';
    } else {
      $('fillBtn').textContent = '开始 AI 填写';
    }
    state.aiOff = off;
    renderQuota();
  }

  // renderQuota draws today's budget. The limit is a server-side counter, not
  // a browser one, so every page load re-reads it from /api/state.
  // The home page stays silent until the budget is actually gone: a permanent
  // counter there is noise for someone who only came to open a form.
  function renderQuota() {
    const q = state.quota;
    const inline = $('quotaInline');
    const spent = !!q && !q.unlimited && q.remaining <= 0;

    $('quotaSpent').hidden = !spent;
    if (spent) {
      $('quotaSpentText').textContent =
        `每天 ${q.limit} 次，明天 00:00 重置；表格照常可以手动填写、打印和导出。`;
    }

    if (!q || q.unlimited) {
      inline.hidden = true;
      $('fillBtn').disabled = !!state.aiOff;
      return;
    }

    inline.hidden = false;
    inline.textContent = spent
      ? `今日 AI 填表次数已用完（${q.limit}/${q.limit}），明天 00:00 重置。`
      : `AI 填表每天限 ${q.limit} 次，今天还剩 ${q.remaining} 次。`;
    inline.classList.toggle('spent', spent);

    if (spent) {
      $('fillBtn').disabled = true;
      $('fillBtn').textContent = state.aiOff ? 'AI 填写（未开通）' : '今日次数已用完';
    } else {
      $('fillBtn').disabled = false;
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

  // Every card on the home page comes from this array. Adding a second demo
  // is one more entry here: nothing in renderDemos() names a specific tool.
  const DEMOS = [
    {
      key: 'ai-fill',
      title: 'AI 填表',
      desc: '上传 Excel 或粘贴文字，自动填进学生表格。对不上的格子留空，绝不编造。',
      tags: (tpls) => [
        `共 ${tpls.length} 种表格`,
        `共 ${tpls.reduce((n, t) => n + (t.fieldCount || 0), 0)} 个可填字段`,
        { text: 'AI 语义匹配', brand: true },
      ],
      note: (tpls) => tpls.map((t) => t.name).join(' · '),
      cta: '开始填写',
      available: (tpls) => tpls.length > 0,
      open: (tpls) => openStudio(pickTemplate(tpls).slug, { autoDemo: true }),
    },
  ];

  function renderDemos() {
    const box = $('demoCards');
    const tpls = state.templates || [];
    const items = DEMOS.filter((d) => !d.available || d.available(tpls));
    if (!items.length) {
      box.innerHTML = '<p class="dz-sub">暂时没有可用的 demo。</p>';
      return;
    }
    box.innerHTML = items.map((d) => {
      const tags = (d.tags ? d.tags(tpls) : []).map((raw) => {
        const t = typeof raw === 'string' ? { text: raw } : raw;
        return `<span class="tag${t.brand ? ' brand' : ''}">${escapeHTML(t.text)}</span>`;
      }).join('');
      const note = d.note ? d.note(tpls) : '';
      return `
      <article class="demo-card" data-demo="${escapeHTML(d.key)}">
        <div class="meta">${tags}</div>
        <h3>${escapeHTML(d.title)}</h3>
        <p>${escapeHTML(d.desc)}</p>
        <div class="foot">
          <span class="dz-sub">${escapeHTML(note)}</span>
          <button class="primary" type="button">${escapeHTML(d.cta || '打开')}</button>
        </div>
      </article>`;
    }).join('');
    // The button has no handler of its own: its click bubbles to the card, so
    // mouse and keyboard land on the same code path exactly once.
    box.querySelectorAll('[data-demo]').forEach((card) => {
      const d = DEMOS.find((x) => x.key === card.dataset.demo);
      if (!d || !d.open) return;
      card.addEventListener('click', () => d.open(tpls));
    });
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

  function showDemoBanner() {
    $('demoBanner').hidden = !state.demo;
  }

  // The sample goes into the material pane only. The sheet stays empty until
  // the model fills it, because the thing worth watching is the model reading
  // prose and choosing the cells itself; a pre-painted sheet would show the
  // result and hide the work.
  function fillDemo() {
    if (!state.current) return;
    $('pasteBox').value = demoSourceText();
    state.draft.text = $('pasteBox').value;
    state.demo = false;
    showDemoBanner();
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
    showDemoBanner();
  }

  // --- studio ---------------------------------------------------------------
  // A template is openable from the URL, so a teacher can bookmark one form
  // instead of walking through the list every time.
  function slugFromHash() {
    return decodeURIComponent((location.hash || '').replace(/^#\/?/, ''));
  }

  async function openStudio(slug, opts = {}) {
    const { sessionId = '', draft = null, autoDemo = false } = opts;
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
    window.scrollTo({ top: 0 });

    resetPanel();
    if (draft) restoreDraft(draft);
    renderTplSwitch();
    mountPaper(data.html);
    showDemoBanner();
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
    $('studio').hidden = true;
    $('app').hidden = false;
    if (location.hash) history.replaceState(null, '', location.pathname);
    state.current = null;
    state.doc = null;
    state.demo = false;
    // A fresh reload keeps the list in step with what was just saved.
    loadState();
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
      return `<button type="button" role="tab" class="tpl-tab${on ? ' on' : ''}" data-slug="${escapeHTML(t.slug)}" aria-selected="${on ? 'true' : 'false'}">${escapeHTML(t.name)} · ${escapeHTML(String(t.fieldCount))} 字段</button>`;
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
      pill.className = 'pill';
      pill.textContent = `AI 填了 ${n}/${total} 项`;
      setStatus('ok', `AI 已填写 <b>${n}</b> 项（共 ${total} 项），其余留空。请核对后导出。`);
    } else if (sess.status === 'manual') {
      // A hand-filled form is a normal outcome, not a failure: say so plainly
      // and leave the status line alone so it does not wipe a real message.
      const n = Object.keys(sess.values || {}).length;
      const total = state.current ? state.current.fields.length : 0;
      pill.className = 'pill manual';
      pill.textContent = `手动填写 ${n}/${total} 项`;
    } else {
      pill.className = 'pill fail';
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
        'AI 填写还没开通：服务器上缺少 DeepSeek API Key（' +
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
        showDemoBanner();
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
  // or a hand-edited URL. Unknown slugs are left alone on purpose.
  window.addEventListener('hashchange', () => {
    const slug = slugFromHash();
    if (!slug || !state.templates.some((t) => t.slug === slug)) return;
    if (state.current && state.current.slug === slug) return;
    if ($('studio').hidden) openStudio(slug, { autoDemo: true });
    else switchTemplate(slug);
  });

  // A reload starts from a clean sheet: the template opens, but nothing is
  // painted into it and the material pane is empty. Only an in-page arrival
  // (the card, the sample button, back/forward) brings the sample in, because
  // someone who refreshed is about to type their own data and does not want a
  // half-filled form they have to clear first.
  loadState().then(() => {
    const slug = slugFromHash();
    if (slug && state.templates.some((t) => t.slug === slug)) {
      openStudio(slug);
    }
  });
})();
