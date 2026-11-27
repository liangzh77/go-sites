// Verify the new accounting category + VAT case page. Local static server, headless Chrome.
const http = require('node:http');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');
const assert = require('node:assert/strict');
const root = path.join(__dirname, 'web');
const chromeBin = process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

(async () => {
  const requests = [];
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://local');
    requests.push(url.pathname);
    if (url.pathname === '/ai-demo/api/state') {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ templates: [], ai: { configured: true, model: 'test' }, sessions: [], quota: { limit: 100, used: 0, remaining: 100, unlimited: false } }));
      return;
    }
    let rel = decodeURIComponent(url.pathname).replace(/^\/ai-demo\/?/, '');
    if (rel === '' || rel.endsWith('/')) rel += 'index.html';
    const file = path.resolve(root, rel);
    if (!file.startsWith(root + path.sep)) { res.writeHead(403); res.end(); return; }
    try {
      const body = fs.readFileSync(file);
      const type = ({ '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'application/javascript', '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg', '.webp': 'image/webp', '.woff2': 'font/woff2' })[path.extname(file)] || 'application/octet-stream';
      res.setHeader('Content-Type', type); res.end(body);
    } catch { res.writeHead(404); res.end(); }
  });
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'vat-case.'));
  const chrome = spawn(chromeBin, ['--headless=new', '--remote-debugging-port=0', '--user-data-dir=' + profile, '--no-first-run', '--no-default-browser-check', 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] });
  let ws;
  try {
    const endpoint = await new Promise((resolve, reject) => {
      let log = ''; const timer = setTimeout(() => reject(Error('Chrome startup timeout')), 20000);
      chrome.stderr.on('data', d => { log += d; const m = log.match(/DevTools listening on (ws:\/\/[^\s]+)/); if (m) { clearTimeout(timer); resolve(m[1]); } });
    });
    ws = new WebSocket(endpoint); await new Promise(r => ws.addEventListener('open', r));
    let id = 0; const pending = new Map(); const errors = [];
    ws.addEventListener('message', e => {
      const m = JSON.parse(e.data);
      if (m.id) { const p = pending.get(m.id); pending.delete(m.id); m.error ? p.reject(Error(JSON.stringify(m.error))) : p.resolve(m.result); }
      if (m.method === 'Runtime.exceptionThrown') errors.push(m.params.exceptionDetails.text);
      if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') errors.push('console.error');
    });
    const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => { const n = ++id; pending.set(n, { resolve, reject }); ws.send(JSON.stringify({ id: n, method, params, sessionId })); });
    const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
    const call = (m, p) => send(m, p, sessionId);
    await call('Page.enable'); await call('Runtime.enable');
    const ev = async expr => {
      const r = await call('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true, userGesture: true });
      if (r.exceptionDetails) throw Error(JSON.stringify(r.exceptionDetails));
      return r.result.value;
    };
    const wait = async expr => { for (let i = 0; i < 100; i++) { if (await ev(expr)) return; await new Promise(r => setTimeout(r, 100)); } throw Error('timeout: ' + expr); };
    // cases.js attaches listeners after DOMContentLoaded; retry the click instead of assuming.
    const openStep = async (tabSel, panelSel) => {
      for (let i = 0; i < 30; i++) {
        if (await ev(`!document.querySelector('${panelSel}').hidden`)) return;
        await ev(`document.querySelector('${tabSel}').click()`);
        for (let k = 0; k < 10; k++) {
          if (await ev(`!document.querySelector('${panelSel}').hidden`)) return;
          await new Promise(r => setTimeout(r, 100));
        }
      }
      throw Error('could not open ' + panelSel);
    };
    const key = async k => { await call('Input.dispatchKeyEvent', { type: 'keyDown', key: k, code: k, windowsVirtualKeyCode: k === 'ArrowRight' ? 39 : 37 }); await call('Input.dispatchKeyEvent', { type: 'keyUp', key: k, code: k, windowsVirtualKeyCode: k === 'ArrowRight' ? 39 : 37 }); };

    await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await call('Page.navigate', { url: origin + '/ai-demo/' });
    await wait("document.querySelectorAll('[data-demo]').length > 10");

    // 1. Pills: 会计 must sit directly to the right of 外贸, same level.
    const pills = await ev("[...document.querySelectorAll('#demoFilters button')].map(b=>b.textContent.trim())");
    const tradeIndex = pills.findIndex(t => t === '外贸');
    assert(tradeIndex >= 0, 'trade pill missing: ' + pills.join(' | '));
    assert.equal(pills[tradeIndex + 1], '会计', 'accounting pill must follow 外贸: ' + pills.join(' | '));
    assert.equal(pills[0], '学校');
    assert.deepEqual(pills, ['学校', '外贸', '会计', '制作', '工具'], 'pill order: ' + pills.join(' | '));

    // 2. The case card exists under the accounting category only.
    assert.equal(await ev("document.querySelectorAll('[data-demo=\"vat-reconciliation\"]').length"), 1);
    assert.equal(await ev("document.querySelector('[data-demo=\"vat-reconciliation\"]').dataset.cat"), 'accounting');
    await ev("[...document.querySelectorAll('#demoFilters button')].find(b=>b.textContent.trim()==='会计').click()");
    assert.equal(await ev("document.querySelector('#categoryTitle').textContent"), '会计 · 把每月的税对平');
    assert.equal(await ev("[...document.querySelectorAll('[data-demo]')].filter(c=>!c.hidden).length"), 1);
    assert.equal(await ev("document.querySelector('[data-demo=\"vat-reconciliation\"]').hidden"), false);
    assert.equal(await ev("location.hash"), '#accounting');

    // 3. Open it from the card.
    await ev("document.querySelector('[data-demo=\"vat-reconciliation\"] button').click()");
    await wait("location.pathname.endsWith('/vat-reconciliation/') && !!document.querySelector('#step-basics')");
    await wait("document.querySelector('#step-days').tabIndex === -1");
    assert.equal(await ev("document.querySelectorAll('.case-steps [role=tab]').length"), 3);
    assert.equal(await ev("document.querySelector('#step-basics').getAttribute('aria-selected')"), 'true');
    assert.equal(await ev("document.querySelector('#days').hidden"), true);

    // 4. Teaching example numbers are present and correct.
    const basicsText = await ev("document.querySelector('#basics').innerText");
    for (const t of ['£1,200', '£200', '£600', '£100', '£120', '£20']) assert(basicsText.includes(t), 'missing ' + t);
    // Client removed four notices; their strings and the containers that only held them must be gone.
    for (const t of ['公开案例 + 教学示例', '天试点 · 来源', '教学示例，不是真实客户数据', '都是为了讲清概念编的']) {
      assert.equal(await ev(`document.body.innerText.includes(${JSON.stringify(t)})`), false, 'removed text still on page: ' + t);
    }
    assert.equal(await ev("document.querySelectorAll('.vat-demo-tag, .context').length"), 0);
    assert.equal(await ev("[...document.querySelectorAll('.case-note')].filter(n=>n.textContent.trim()==='').length"), 0, 'empty notice box');
    assert.equal(await ev("document.querySelectorAll('#basics .vat-diff').length"), 1);
    assert.equal(await ev("document.querySelectorAll('#basics .vat-diff .todo').length"), 3);

    // 5. Tab switching by click, by keyboard and by the step buttons.
    await openStep('#step-days', '#days');
    assert.equal(await ev("document.querySelectorAll('#days .vat-day').length"), 4);
    assert.equal(await ev("[...document.querySelectorAll('#days .vat-day-num')].map(n=>n.textContent.trim()).join(',')"), 'DAY 1,DAY 2,DAY 3,DAY 4');
    const daysText = await ev("document.querySelector('#days').innerText");
    for (const t of ['17 类', '46 人', 'Thomas & Co.', '账务系统', '历史', '吻合']) assert(daysText.includes(t), 'days missing ' + t);
    await openStep('#step-division', '#division');
    const divisionText = await ev("document.querySelector('#division').innerText");
    for (const t of ['AI 备稿，会计签字', '不是把整家事务所自动化', '50 倍', '第三方审计']) assert(divisionText.includes(t), 'division missing ' + t);
    const sourceLink = await ev("(() => { const a=document.querySelector('#division a[target=\"_blank\"]'); return a ? {href:a.href, rel:a.rel, text:a.textContent.trim()} : null; })()");
    assert(sourceLink, 'source link missing');
    assert.equal(sourceLink.href, 'https://www.youtube.com/watch?v=IXDYEF22CG8');
    assert.equal(sourceLink.rel, 'noopener noreferrer');
    assert(sourceLink.text.includes('Liam Ottley') || sourceLink.text.includes('46-Person'), 'source link text: ' + sourceLink.text);
    await ev("[...document.querySelectorAll('#division [data-step]')].find(b=>b.dataset.step==='days').click()");
    await wait("!document.querySelector('#days').hidden");
    assert.equal(await ev("document.querySelector('#days').hidden"), false);
    await ev("document.querySelector('#step-basics').focus()");
    await key('ArrowRight');
    assert.equal(await ev("document.querySelector('#days').hidden"), false);
    assert.equal(await ev("document.activeElement.id"), 'step-days');
    await key('ArrowLeft');
    assert.equal(await ev("document.querySelector('#basics').hidden"), false);

    // 6. Responsive: no page-level horizontal overflow; day cards stack/grid by width.
    const columns = async () => ev("getComputedStyle(document.querySelector('.vat-days')).gridTemplateColumns.split(' ').length");
    for (const [w, expected] of [[1440, 4], [768, 2], [375, 1]]) {
      await openStep('#step-days', '#days');
      await call('Emulation.setDeviceMetricsOverride', { width: w, height: 900, deviceScaleFactor: 1, mobile: w < 500 });
      await new Promise(r => setTimeout(r, 150));
      assert.equal(await ev('document.documentElement.scrollWidth <= window.innerWidth'), true, 'horizontal overflow at ' + w);
      assert.equal(await columns(), expected, `day card columns at ${w}`);
    }
    await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await ev("document.querySelector('#step-basics').click();window.scrollTo(0,0)");
    const shot = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    fs.writeFileSync(path.join(os.tmpdir(), 'vat-case-desktop.png'), Buffer.from(shot.data, 'base64'));
    await ev("document.querySelector('#step-days').click()");
    const shot2 = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    fs.writeFileSync(path.join(os.tmpdir(), 'vat-case-days.png'), Buffer.from(shot2.data, 'base64'));
    await call('Emulation.setDeviceMetricsOverride', { width: 375, height: 900, deviceScaleFactor: 1, mobile: true });
    await ev("document.querySelector('#step-division').click();window.scrollTo(0,0)");
    await new Promise(r => setTimeout(r, 200));
    const shot3 = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    fs.writeFileSync(path.join(os.tmpdir(), 'vat-case-mobile.png'), Buffer.from(shot3.data, 'base64'));
    assert.deepEqual(errors, []);
    console.log('PASS removed notices (no empty boxes), pills order (会计 right after 外贸), card category, deep link #accounting, 3 tabs click/keyboard/step buttons, teaching numbers, 4 day cards, boundaries, external source link, 1440/768/375 layout, no JS errors');
  } finally {
    if (ws) ws.close();
    if (chrome.exitCode === null) { const exited = new Promise(r => chrome.once('exit', r)); chrome.kill('SIGTERM'); await exited; }
    server.close(); fs.rmSync(profile, { recursive: true, force: true });
  }
})().catch(e => { console.error(e); process.exitCode = 1; });
