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

  const state = {
    templates: [],
    ai: { configured: false, model: '', keyEnv: '' },
    current: null,        // { slug, name, fields, html }
    sessionId: '',
    values: {},           // fieldId -> string
    dirty: false,
    doc: null,            // the iframe's document
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
      throw err;
    }
    return data;
  }

  const fmtTime = (iso) => {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getMonth() + 1}月${d.getDate()}日 ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };

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
    renderAIDisabled();
    renderDemos();
    renderRecent(data.recent || []);
  }

  function renderAIDisabled() {
    // 没配 key 时按钮保持可点：点下去要给一句明确的说明，
    // 而不是留一个灰按钮让人猜原因。
    const off = !state.ai.configured;
    $('aiNotice').hidden = !off;
    $('fillBtn').disabled = false;
    if (off) {
      $('aiNoticeText').textContent =
        `服务器上没有配置 DeepSeek API Key（${state.ai.keyEnv}），提交会返回提示。` +
        '表格本身仍可以手动填写、打印和导出。';
      $('fillBtn').textContent = 'AI 填写（未开通）';
    } else {
      $('fillBtn').textContent = '开始 AI 填写';
    }
  }

  function renderDemos() {
    const box = $('demoCards');
    if (!state.templates.length) {
      box.innerHTML = '<p class="dz-sub">暂时没有可用模板。</p>';
      return;
    }
    box.innerHTML = state.templates.map((t, i) => `
      <article class="demo-card">
        <div class="meta">
          <span class="tag brand">Demo ${i + 1}</span>
          <span class="tag">${t.fieldCount} 个可填字段</span>
          <span class="tag">AI 语义匹配</span>
        </div>
        <h3>${escapeHTML(t.name)}</h3>
        <p>${i === 0
          ? '上传学生资料 Excel 或粘贴一段文字，自动填进入学登记表的每个格子；对不上的留空，不编造。'
          : '上传资料或粘贴文字，自动填写信息采集表，含单选项目。'}</p>
        <div class="foot">
          <span class="dz-sub">${escapeHTML(t.sourceFile)}</span>
          <button class="primary" type="button" data-slug="${escapeHTML(t.slug)}">开始填写</button>
        </div>
      </article>`).join('');
    box.querySelectorAll('button[data-slug]').forEach((b) => {
      b.addEventListener('click', () => openStudio(b.dataset.slug));
    });
  }

  function renderRecent(list) {
    const block = $('recentBlock');
    if (!list.length) { block.hidden = true; return; }
    block.hidden = false;
    $('recentList').innerHTML = list.map((s) => {
      const n = Object.keys(s.values || {}).length;
      const badge = s.status === 'filled'
        ? `${n} 项已填`
        : (statusLabel[s.status] || s.status);
      const tpl = state.templates.find((t) => t.slug === s.template);
      return `<li><button type="button" data-id="${escapeHTML(s.id)}" data-tpl="${escapeHTML(s.template)}">
        <span class="rt">${escapeHTML(s.inputName || '一次填写')} → ${escapeHTML(tpl ? tpl.name : s.template)}</span>
        <span class="rm">${escapeHTML(badge)} · ${escapeHTML(fmtTime(s.createdAt))}</span>
      </button></li>`;
    }).join('');
    $('recentList').querySelectorAll('button[data-id]').forEach((b) => {
      b.addEventListener('click', () => openStudio(b.dataset.tpl, b.dataset.id));
    });
  }

  // --- studio ---------------------------------------------------------------
  // A template is openable from the URL, so a teacher can bookmark one form
  // instead of walking through the list every time.
  function slugFromHash() {
    return decodeURIComponent((location.hash || '').replace(/^#\/?/, ''));
  }

  async function openStudio(slug, sessionId) {
    let data;
    try {
      data = await api('/api/templates/' + encodeURIComponent(slug));
    } catch (e) { toast(e.message); return; }

    state.current = data;
    state.sessionId = '';
    state.values = {};
    state.dirty = false;
    $('studioTitle').textContent = data.name;
    if (slugFromHash() !== slug) history.replaceState(null, '', '#' + encodeURIComponent(slug));
    $('studio').hidden = false;
    $('app').hidden = true;
    window.scrollTo({ top: 0 });

    resetPanel();
    mountPaper(data.html);
    $('exportBtn').disabled = true;
    $('saveBtn').disabled = true;
    $('fillPill').hidden = true;
    $('warnBox').hidden = true;

    if (sessionId) {
      try {
        const res = await api('/api/sessions/' + encodeURIComponent(sessionId));
        applySession(res.session);
      } catch (e) { toast(e.message); }
    }
  }

  function closeStudio() {
    $('studio').hidden = true;
    $('app').hidden = false;
    if (location.hash) history.replaceState(null, '', location.pathname);
    state.doc = null;
    // A fresh reload keeps the list in step with what was just saved.
    loadState();
  }

  function resetPanel() {
    $('pasteBox').value = '';
    $('fileInput').value = '';
    $('dzSub').textContent = '支持 .xlsx / .csv / .txt，最大 8MB';
    $('dropzone').classList.remove('has-file');
    setStatus('', '');
  }

  function mountPaper(html) {
    const frame = $('paper');
    const doc = frame.contentDocument || frame.contentWindow.document;
    doc.open();
    doc.write(html);
    doc.close();
    state.doc = doc;
    decorateFrame();
    frame.contentWindow.addEventListener('beforeprint', () => {});
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
      [data-field-id]:hover { background: #eef4ff !important; }
      [data-field-id]:focus { outline: 2px solid #2f6bd8 !important; outline-offset: -2px; background: #f4f8ff !important; }
      [data-field-id].ai-filled { background: #f2f7ff; }
      [data-field-id].ai-filled:empty::after,
      [data-field-id].ai-filled:not(:empty) { }
      .choices[data-field-id] { background: transparent; }
      .choices[data-field-id].ai-filled { background: #f2f7ff; }
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

  function fitPaper() {
    const frame = $('paper');
    const doc = state.doc;
    if (!doc || !doc.documentElement) return;
    const h = Math.max(
      doc.documentElement.scrollHeight || 0,
      doc.body ? doc.body.scrollHeight : 0,
      1120,
    );
    frame.style.height = (h + 8) + 'px';
  }

  // --- filling --------------------------------------------------------------
  async function doFill() {
    if (!state.current) return;
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
    setStatus('info', '<span class="spin"></span>正在读取资料并让 AI 对齐字段，通常几秒到十几秒…');
    $('warnBox').hidden = true;

    try {
      let res;
      if (file) {
        const fd = new FormData();
        fd.append('template', state.current.slug);
        fd.append('file', file);
        res = await api('/api/fill', { method: 'POST', body: fd });
      } else {
        res = await api('/api/fill', {
          method: 'POST',
          body: JSON.stringify({ template: state.current.slug, text }),
        });
      }
      applySession(res.session);
      const n = Object.keys((res.session && res.session.values) || {}).length;
      const filled = Object.keys(collectValues()).length;
      if (n === 0 && filled === 0) {
        setStatus('err', 'AI 没有从这段资料里找到能填的内容。可能是资料太短，或与表格要求的项目无关。');
      } else {
        toast(`已填 ${n} 项`);
      }
      fitPaper();
    } catch (e) {
      setStatus('err', escapeHTML(e.message));
    } finally {
      $('fillBtn').disabled = false;
    }
  }

  async function saveEdits() {
    if (!state.sessionId) return;
    const values = collectValues();
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

  function exportXLSX() {
    if (!state.sessionId) return;
    const url = BASE + '/api/sessions/' + encodeURIComponent(state.sessionId) + '/xlsx';
    window.location.href = url;
  }

  function printForm() {
    const frame = $('paper');
    if (!frame.contentWindow) return;
    fitPaper();
    frame.contentWindow.focus();
    frame.contentWindow.print();
  }

  // --- wiring ---------------------------------------------------------------
  $('backBtn').addEventListener('click', closeStudio);
  $('fillBtn').addEventListener('click', doFill);
  $('saveBtn').addEventListener('click', saveEdits);
  $('exportBtn').addEventListener('click', exportXLSX);
  $('printBtn').addEventListener('click', printForm);
  $('clearBtn').addEventListener('click', () => { resetPanel(); });

  $('fileInput').addEventListener('change', () => {
    const f = $('fileInput').files[0];
    const dz = $('dropzone');
    if (!f) { dz.classList.remove('has-file'); $('dzSub').textContent = '支持 .xlsx / .csv / .txt，最大 8MB'; return; }
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

  loadState().then(() => {
    const slug = slugFromHash();
    if (slug && state.templates.some((t) => t.slug === slug)) openStudio(slug);
  });
})();
