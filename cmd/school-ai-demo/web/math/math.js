(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const timers = new WeakMap();
  let toastTimer;
  function announce(message) {
    $('toast').textContent = message;
    $('toast').hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { $('toast').hidden = true; }, 2600);
  }
  async function copy(text, button) {
    let ok = false;
    try { await navigator.clipboard.writeText(text); ok = true; } catch (_) {
      const box = document.createElement('textarea');
      box.value = text;
      box.style.cssText = 'position:fixed;left:-10000px;top:0';
      document.body.append(box);
      box.select();
      try { ok = document.execCommand('copy'); } catch (_) { /* show manual-copy hint */ }
      box.remove();
      button.focus({ preventScroll: true });
    }
    if (!ok) { announce('无法自动复制，请手动选择提示词或表格复制。'); return; }
    const label = button.dataset.originalLabel || button.textContent;
    button.dataset.originalLabel = label;
    clearTimeout(timers.get(button));
    button.textContent = '✓ 已复制';
    button.classList.add('copied');
    timers.set(button, setTimeout(() => { button.textContent = label; button.classList.remove('copied'); }, 2000));
    announce('已复制到剪贴板');
  }
  document.querySelectorAll('[data-copy-case]').forEach(button => {
    button.addEventListener('click', () => {
      const section = $(button.dataset.copyCase);
      const rows = [...section.querySelectorAll('table tr')].map(row => [...row.cells].map(cell => cell.textContent.trim().replace(/\|/g, '\\|').replace(/\r?\n/g, '<br>')));
      const line = cells => '| ' + cells.join(' | ') + ' |';
      const markdown = '**' + section.querySelector('.grade-title').textContent + '**\n\n' + [line(rows[0]), line(rows[0].map(() => '---')), ...rows.slice(1).map(line)].join('\n');
      copy(markdown, button);
    });
  });

  const dialog = $('imageDialog');
  let trigger = null;
  let previousOverflow = '';
  function openImage(button, src, title, alt) {
    trigger = button;
    $('dialogTitle').textContent = title;
    $('dialogImage').alt = alt;
    $('dialogImage').src = src;
    $('dialogOriginal').href = src;
    previousOverflow = document.body.style.overflow;
    dialog.showModal();
    document.body.style.overflow = 'hidden';
    button.setAttribute('aria-expanded', 'true');
    $('closeDialog').focus();
  }
  document.querySelectorAll('[data-image]').forEach(button => {
    button.addEventListener('click', () => openImage(button, button.dataset.image, button.dataset.imageTitle, button.querySelector('img').alt));
  });
  $('closeDialog').addEventListener('click', () => dialog.close());
  dialog.addEventListener('click', event => {
    const rect = dialog.getBoundingClientRect();
    if (event.target === dialog && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) dialog.close();
  });
  dialog.addEventListener('close', () => {
    document.body.style.overflow = previousOverflow;
    $('dialogImage').removeAttribute('src');
    $('dialogOriginal').removeAttribute('href');
    if (trigger) { trigger.setAttribute('aria-expanded', 'false'); trigger.focus({ preventScroll: true }); trigger = null; }
  });
  function updateScrollHints() {
    document.querySelectorAll('.table-scroll').forEach(box => {
      const scrolls = box.scrollWidth > box.clientWidth + 1;
      $(box.getAttribute('aria-describedby')).hidden = !scrolls;
      box.tabIndex = scrolls ? 0 : -1;
    });
  }
  if (typeof ResizeObserver !== 'undefined') {
    const observer = new ResizeObserver(updateScrollHints);
    document.querySelectorAll('.table-scroll').forEach(box => observer.observe(box));
  } else window.addEventListener('resize', updateScrollHints);
  updateScrollHints();

  const API = '/ai-demo/api/math-review';
  let available = false, remaining = 0, busy = false, preparing = false, selected = null;
  let previewURL = '', pickVersion = 0, output = '', model = 'deepseek-v4.1-flash';
  function liveError(message = '') { $('mathError').textContent = message; $('mathError').hidden = !message; }
  function refresh() {
    const prompt = $('mathPromptInput').value.trim();
    $('mathSubmit').disabled = busy || preparing || !available || remaining <= 0 || !selected || !prompt;
    $('mathSubmit').textContent = busy ? '判卷中…' : '开始 AI 判卷';
    for (const id of ['mathImageFile', 'mathRemove', 'mathPromptInput', 'mathPromptReset']) $(id).disabled = busy;
    $('mathChoose').setAttribute('aria-disabled', String(busy));
    $('mathDropzone').setAttribute('aria-disabled', String(busy));
    $('mathForm').setAttribute('aria-busy', String(busy || preparing));
    $('mathStatus').textContent = busy ? '正在逐题分析，最长约 120 秒，请勿重复提交' : preparing ? '正在处理图片…' : !available ? '等待模型服务就绪' : remaining <= 0 ? '今日分析额度已用完' : `今日剩余 ${remaining} 次`;
  }
  function clearResult() {
    output = '';
    $('copyMathResult').hidden = true;
    $('mathResult').classList.remove('ready');
    $('mathResultTitle').textContent = '判卷结果会出现在这里';
    $('mathResultMeta').textContent = '尚未提交当前试卷';
    $('mathSummary').textContent = '点击“开始 AI 判卷”，再查看当前图片的分析结果。';
    $('mathRows').replaceChildren();
    $('liveTableBox').hidden = true;
    $('liveScrollHint').hidden = true;
    $('mathResultNotice').hidden = true;
  }
  function removeImage() {
    pickVersion++; preparing = false; selected = null;
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = '';
    $('mathPreview').removeAttribute('src');
    $('mathImageFile').value = '';
    $('mathPreviewBox').hidden = true;
    clearResult(); refresh();
  }
  async function prepare(file) {
    if (busy) return;
    removeImage(); liveError();
    if (!file || !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) { liveError('请选择 JPEG、PNG 或 WebP 图片。'); return; }
    if (file.size > 5 * 1024 * 1024) { liveError('原图超过 5MB，请缩小后重试。'); return; }
    const version = ++pickVersion;
    preparing = true; refresh();
    let rawURL = '';
    try {
      rawURL = URL.createObjectURL(file);
      const image = new Image(); image.src = rawURL; await image.decode();
      if (image.naturalWidth * image.naturalHeight > 40000000) throw new Error('图片像素过大，请缩小后重试。');
      const scale = Math.min(1, 2400 / Math.max(image.naturalWidth, image.naturalHeight));
      const canvas = document.createElement('canvas');
      canvas.width = Math.max(1, Math.round(image.naturalWidth * scale)); canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
      const ctx = canvas.getContext('2d'); ctx.fillStyle = '#fff'; ctx.fillRect(0, 0, canvas.width, canvas.height); ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', .94));
      if (!blob || blob.size > 5 * 1024 * 1024) throw new Error('图片处理失败或仍超过 5MB，请换图重试。');
      if (version !== pickVersion) return;
      selected = blob; previewURL = URL.createObjectURL(blob);
      $('mathPreview').src = previewURL; $('mathPreviewBox').hidden = false;
      $('mathImageInfo').textContent = `${canvas.width} × ${canvas.height} · ${Math.ceil(blob.size / 1024)} KB · 已去除元数据`;
    } catch (error) { if (version === pickVersion) liveError(error.message || '无法读取图片，请重新选择。'); }
    finally { if (rawURL) URL.revokeObjectURL(rawURL); if (version === pickVersion) { preparing = false; refresh(); } }
  }
  // The visible control is a <label for> the file input, so the browser opens the
  // picker natively instead of relying on a script-triggered input.click().
  $('mathChoose').addEventListener('keydown', event => {
    if (!busy && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); $('mathImageFile').click(); }
  });
  $('mathImageFile').addEventListener('change', () => {
    if ($('mathImageFile').files.length !== 1) { liveError('一次请选择一张试卷图片。'); return; }
    prepare($('mathImageFile').files[0]);
  });
  $('mathRemove').addEventListener('click', () => { if (!busy) { removeImage(); liveError(); } });
  $('mathPromptInput').addEventListener('input', refresh);
  $('mathPromptReset').addEventListener('click', () => { if (!busy) { $('mathPromptInput').value = '作为初一数学老师，判卷'; refresh(); } });
  document.querySelectorAll('[data-use-example]').forEach(button => button.addEventListener('click', async () => {
    if (busy) return;
    const label = button.textContent; button.disabled = true; button.textContent = '正在放入…';
    try {
      const response = await fetch(button.dataset.useExample);
      if (!response.ok) throw new Error();
      const blob = await response.blob();
      await prepare(new File([blob], 'example.png', { type: blob.type || 'image/png' }));
      $('liveTitle').scrollIntoView({ block: 'start' });
      $('mathChoose').focus({ preventScroll: true });
    } catch (_) { liveError('示例图片读取失败，请重试。'); }
    finally { button.disabled = false; button.textContent = label; }
  }));
  $('mathPreviewZoom').addEventListener('click', () => { if (previewURL) openImage($('mathPreviewZoom'), previewURL, '待提交的试卷图片', '本次待提交的数学试卷'); });
  document.addEventListener('paste', event => {
    if (busy || dialog.open) return;
    const images = [...(event.clipboardData?.items || [])].filter(item => item.type.startsWith('image/'));
    if (!images.length) return;
    event.preventDefault();
    if (images.length !== 1) { liveError('一次请粘贴一张试卷图片。'); return; }
    prepare(images[0].getAsFile());
  });
  for (const name of ['dragover', 'dragleave', 'drop']) $('mathDropzone').addEventListener(name, event => {
    event.preventDefault(); $('mathDropzone').classList.toggle('dragging', name === 'dragover' && !busy);
    if (name === 'drop' && !busy) {
      if (event.dataTransfer.files.length !== 1) { liveError('一次请拖入一张试卷图片。'); return; }
      prepare(event.dataTransfer.files[0]);
    }
  });
  $('mathDropzone').addEventListener('keydown', event => {
    if (!busy && event.target === $('mathDropzone') && ['Enter', ' '].includes(event.key)) { event.preventDefault(); $('mathImageFile').click(); }
  });
  async function checkService() {
    available = false; $('retryService').hidden = true; refresh();
    try {
      const response = await fetch(API + '/status', { cache: 'no-store', signal: AbortSignal.timeout(10000) });
      if (!response.ok) throw new Error('服务暂时连接不上，请重新检查。');
      const data = await response.json();
      available = !!data.configured && data.imageSupported !== false; remaining = Number(data.remaining) || 0; model = data.model;
      $('liveModel').hidden = true;
      if (!available) throw new Error('模型暂时无法分析图片，请稍后重试。');
      liveError();
    } catch (error) { available = false; $('liveModel').hidden = false; $('liveModel').textContent = '实时服务暂未就绪，可稍后重试。'; liveError(error.message || '无法连接服务，请稍后重试。'); $('retryService').hidden = false; }
    refresh();
  }
  $('retryService').addEventListener('click', checkService);
  function showResult(result, responseModel) {
    if (!result || typeof result.summary !== 'string' || !Array.isArray(result.rows)) throw new Error('服务未返回完整的判卷结果，请重试。');
    const styles = { '正确': 'pass', '错误': 'wrong', '部分正确': 'partial', '未作答': 'blank', '待确认': 'unclear' };
    const symbols = { '正确': '✓ ', '错误': '✗ ', '部分正确': '△ ', '未作答': '— ', '待确认': '? ' };
    const fragment = document.createDocumentFragment();
    for (const row of result.rows) {
      if (!Object.hasOwn(styles, row.verdict) || !['question', 'studentAnswer', 'feedback'].every(key => typeof row[key] === 'string')) throw new Error('判卷结果格式异常，请重试。');
      const tr = document.createElement('tr');
      for (const key of ['question', 'studentAnswer', 'verdict', 'feedback']) {
        const cell = document.createElement(key === 'question' ? 'th' : 'td');
        if (key === 'question') cell.scope = 'row';
        if (key === 'verdict') { const badge = document.createElement('span'); badge.className = 'verdict ' + styles[row.verdict]; badge.textContent = symbols[row.verdict] + row.verdict; cell.append(badge); }
        else cell.textContent = row[key];
        tr.append(cell);
      }
      fragment.append(tr);
    }
    $('mathRows').replaceChildren(fragment);
    $('mathResultTitle').textContent = '这一次的实时判卷结果';
    $('mathResultMeta').textContent = `本次实时生成 · ${responseModel} · ${result.rows.length} 项判定`;
    $('mathSummary').textContent = result.summary;
    $('mathResultNotice').textContent = result.notice || ''; $('mathResultNotice').hidden = !result.notice;
    $('liveTableBox').hidden = result.rows.length === 0;
    $('mathResult').classList.add('ready');
    const safe = text => String(text).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\|/g, '\\|').replace(/\r?\n/g, '<br>');
    const lines = result.rows.map(row => '| ' + [row.question, row.studentAnswer, row.verdict, row.feedback].map(safe).join(' | ') + ' |');
    output = '**本次实时判卷结果**\n\n' + safe(result.summary) + '\n\n' + (lines.length ? '| 题号 | 识别到的作答 | 判定 | 批注 |\n| --- | --- | --- | --- |\n' + lines.join('\n') + '\n\n' : '') + safe(result.notice || '') + '\n\nAI 判卷仅供参考，请老师结合原卷复核。';
    $('copyMathResult').hidden = false; updateScrollHints();
    $('mathResultTitle').focus({ preventScroll: true }); $('mathResult').scrollIntoView({ block: 'nearest' });
  }
  $('copyMathResult').addEventListener('click', event => copy(output, event.currentTarget));
  $('mathForm').addEventListener('submit', async event => {
    event.preventDefault(); refresh(); if ($('mathSubmit').disabled) return;
    const prompt = $('mathPromptInput').value.trim();
    if (!prompt) { liveError('请填写提示词。'); return; }
    if ([...prompt].length > 2000) { liveError('提示词过长，请控制在 2000 字以内。'); return; }
    const data = new FormData(); data.append('image', selected, 'math-paper.jpg'); data.append('prompt', prompt);
    busy = true; liveError(); clearResult(); $('mathResultTitle').textContent = '正在阅读并核算试卷…'; $('mathSummary').textContent = '正在识别题目与作答，请稍候。'; refresh();
    const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), 118000);
    try {
      const response = await fetch(API, { method: 'POST', body: data, signal: controller.signal });
      let payload; try { payload = await response.json(); } catch (_) { throw new Error('服务返回异常，请稍后重试。'); }
      if (!response.ok) { if (payload.code === 'DAILY_LIMIT') remaining = 0; throw new Error(payload.error || '判卷失败，请稍后重试。'); }
      remaining = Number(payload.remaining) || 0; showResult(payload.result, payload.model);
    } catch (error) { clearResult(); liveError(error.name === 'AbortError' ? '判卷超时，图片已保留，可稍后重试。' : error.message || '网络异常，图片已保留，请重试。'); $('mathError').scrollIntoView({ block: 'nearest' }); }
    finally { clearTimeout(timer); busy = false; refresh(); }
  });
  refresh(); checkService();
})();
