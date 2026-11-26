// 生成教案 — static page: copy the prompt, preview 18 slides. No API calls.
(function () {
  'use strict';
  const $ = (id) => document.getElementById(id);

  // Titles taken from the deck itself, so the grid is readable before an
  // image has loaded and the lightbox can say where you are.
  const SLIDES = [
    '封面 · 从百草园到三味书屋',
    '学情 · 学生会写景，但大多只写“看见的”',
    '教学目标 · 这一节课，学生要带走三样东西',
    '教学重难点 · 怎么写，为什么好玩',
    '课前准备 · 老师备三样，学生备三样',
    '教学过程 · 45 分钟，五段推进',
    '导入 · 你小时候最爱玩的地方',
    '整体感知 · 园子里到底有什么',
    '品味写景 · “不必说……单是……”句式',
    '多感官 · 看见、听见、尝到、摸到',
    '小组讨论 · 换一种感官重写句子',
    '写法提炼 · 小地方、五官分工、准动词',
    '体会乐趣 · 同一个园子，两双眼睛',
    '讨论小结 · 自由、发现、想象',
    '小结与作业 · 把方法带走',
    '板书设计 · 讲到哪儿写到哪儿',
    '分层作业 · 基础必做、选做提升',
    '教学反思 · 下课就填三行',
  ];
  const ASSET_VERSION = '553fc7142a8c';
  const pad = (n) => String(n).padStart(2, '0');
  const full = (i) => `lesson/slides/slide-${pad(i + 1)}.webp?v=${ASSET_VERSION}`;
  const thumb = (i) => `lesson/thumbs/slide-${pad(i + 1)}.webp?v=${ASSET_VERSION}`;

  // --- toast ------------------------------------------------------------------
  let toastTimer = 0;
  function toast(msg) {
    const t = $('toast');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove('show'), 2200);
  }

  // --- copy -------------------------------------------------------------------
  // navigator.clipboard only exists on https/localhost; the site is also
  // reachable over plain http by IP, so execCommand is the fallback, and
  // selecting the text is the last resort.
  function legacyCopy(text) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;';
    document.body.appendChild(ta);
    ta.select();
    ta.setSelectionRange(0, text.length);
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }
  function selectPrompt() {
    const r = document.createRange();
    r.selectNodeContents($('promptText'));
    const s = window.getSelection();
    s.removeAllRanges();
    s.addRange(r);
  }
  let copyTimer = 0;
  async function copyPrompt() {
    const btn = $('copyBtn');
    const label = $('copyLabel');
    const text = $('promptText').textContent;
    let ok = false;
    if (navigator.clipboard && window.isSecureContext) {
      try { await navigator.clipboard.writeText(text); ok = true; } catch (e) { ok = false; }
    }
    if (!ok) ok = legacyCopy(text);
    if (!ok) {
      selectPrompt();
      toast('浏览器不允许自动复制，已帮你选中全文，请按 Ctrl+C / ⌘C');
      return;
    }
    label.textContent = '已复制 ✓';
    btn.classList.add('done');
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      label.textContent = '复制提示词';
      btn.classList.remove('done');
    }, 2000);
  }
  $('copyBtn').addEventListener('click', copyPrompt);

  // --- grid -------------------------------------------------------------------
  // The card is a real <button>: click, Enter and Space all open the same page,
  // and the caption underneath is the deck's own title.
  $('slideGrid').innerHTML = SLIDES.map((t, i) => `
    <li><button class="sl" type="button" data-i="${i}" aria-label="放大第 ${i + 1} 页：${t}">
      <span class="thumb"><img src="${thumb(i)}" alt="第 ${i + 1} 页：${t}" loading="lazy" decoding="async" width="480" height="270"></span>
      <span class="t"><em>${pad(i + 1)}</em><span>${t}</span></span>
    </button></li>`).join('');
  $('slideGrid').addEventListener('click', (e) => {
    const b = e.target.closest('[data-i]');
    if (b) open(Number(b.dataset.i), b);
  });

  // --- lightbox ---------------------------------------------------------------
  const box = $('lightbox');
  const img = $('lbImg');
  let cur = 0;
  let opener = null;

  function show(i) {
    cur = (i + SLIDES.length) % SLIDES.length;
    img.src = full(cur);
    img.alt = `第 ${cur + 1} 页：${SLIDES[cur]}`;
    $('lbCount').textContent = `${pad(cur + 1)} / ${SLIDES.length}`;
    $('lbTitle').textContent = SLIDES[cur];
    // Warm the neighbours so the next arrow press is instant.
    [cur + 1, cur - 1].forEach((n) => {
      const k = (n + SLIDES.length) % SLIDES.length;
      const p = new Image();
      p.src = full(k);
    });
  }
  function open(i, from) {
    opener = from || null;
    box.hidden = false;
    document.body.classList.add('lp-locked');
    show(i);
    $('lbClose').focus();
  }
  function close() {
    box.hidden = true;
    document.body.classList.remove('lp-locked');
    img.removeAttribute('src');
    if (opener) opener.focus();
  }
  $('lbPrev').addEventListener('click', (e) => { e.stopPropagation(); show(cur - 1); });
  $('lbNext').addEventListener('click', (e) => { e.stopPropagation(); show(cur + 1); });
  $('lbClose').addEventListener('click', close);
  // A click on the dark backdrop (not the card, the slide or a control) closes.
  $('lightbox').addEventListener('click', (e) => { if (e.target === e.currentTarget) close(); });
  $('lbStage').addEventListener('click', (e) => { if (e.target === e.currentTarget) close(); });

  document.addEventListener('keydown', (e) => {
    if (box.hidden) return;
    if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') { e.preventDefault(); show(cur + 1); }
    else if (e.key === 'ArrowLeft' || e.key === 'PageUp') { e.preventDefault(); show(cur - 1); }
    else if (e.key === 'Home') { e.preventDefault(); show(0); }
    else if (e.key === 'End') { e.preventDefault(); show(SLIDES.length - 1); }
    else if (e.key === 'Escape') { e.preventDefault(); close(); }
    else if (e.key === 'Tab') {
      // Keep focus inside the dialog while it is open.
      const f = [$('lbClose'), $('lbPrev'), $('lbNext')];
      const at = f.indexOf(document.activeElement);
      e.preventDefault();
      f[(at + (e.shiftKey ? f.length - 1 : 1)) % f.length].focus();
    }
  });

  // Swipe left/right on touch screens.
  let sx = 0, sy = 0, tracking = false;
  $('lbStage').addEventListener('touchstart', (e) => {
    if (e.touches.length !== 1) { tracking = false; return; }
    tracking = true; sx = e.touches[0].clientX; sy = e.touches[0].clientY;
  }, { passive: true });
  $('lbStage').addEventListener('touchend', (e) => {
    if (!tracking) return;
    tracking = false;
    const dx = e.changedTouches[0].clientX - sx;
    const dy = e.changedTouches[0].clientY - sy;
    if (Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy) * 1.2) show(cur + (dx < 0 ? 1 : -1));
  }, { passive: true });

  // #slide-5 opens straight on that page, so a page can be linked to.
  const m = /^#slide-(\d{1,2})$/.exec(location.hash);
  if (m) { const n = Number(m[1]); if (n >= 1 && n <= SLIDES.length) open(n - 1); }
})();
