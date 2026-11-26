// Node 22+ and Chrome. No npm dependencies, no model calls, isolated browser profile.
const http = require('node:http');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');
const assert = require('node:assert/strict');
const root = path.join(__dirname, 'web');
const cases = [
  { id: 'case1', heading: '判卷结果（满分 30 分，每题 5 分）', rows: [
    ['题', '判定', '得分', '批注'],
    ['第(6)题', '✓ 全对', '5', '113/24，四步全对'],
    ['(1)', '✓ 全对', '5', '−27.8，算式正确'],
    ['(2)', '△ 半对', '4', '−9/24 未约分，应写 −3/8'],
    ['(3)', '✗ 错', '1', '355/20 − 85/20 抄成 65/20，应得 27/2（过程对，最后一步错）'],
    ['(4)', '✗ 过程错', '2', 'y 应 = 5+3 = 8，写成 −5+3 = 8 不成立；13 这个结论对，但算式不成立'],
    ['数学园地', '✗ 空白', '0', '未作答'],
  ] },
  { id: 'case2', heading: '判卷：全对，100 分', rows: [
    ['题', '判定', '说明'],
    ['1 填表', '✓ 4/4', '5×8=+40；(−3)×(−4)=+12；4×(−7)=−28；(−0.4)×9=−3.6，符号和积全部正确'],
    ['2 倒数', '✓ 2/2', '−1/3 的倒数是 −3；3/2 的倒数是 2/3'],
    ['3 计算', '✓ 4/4', '−7；0；−4/5；−32，四道全对'],
    ['4 计算', '✓ 2/2', '7/2；−1，两道全对，过程也规范（3.5 化成分数 7/2 再约，这一步做得好）'],
  ] },
];
(async () => {
  const requests = [], submissions = [];
  let nextFailure = null;
  const sampleResult = { summary: '识别到两题，不预设总分。', rows: [{ question: '1', studentAnswer: '-7', verdict: '正确', feedback: '(-3.5)×2=-7' }, { question: '2', studentAnswer: '看不清', verdict: '待确认', feedback: '<img src=x onerror="window.mathXSS=1"> 请重拍符号位置。' }], notice: '请老师复核。' };
  const server = http.createServer((req, res) => {
    requests.push(req.url);
    const url = new URL(req.url, 'http://localhost');
    if (url.pathname === '/school-ai-demo/api/math-review/status') {
      res.setHeader('Content-Type', 'application/json');res.end(JSON.stringify({ configured: true, imageSupported: true, model: 'deepseek-v4.1-flash', remaining: 50, limit: 50, quotaScope: 'essay-and-math' }));return;
    }
    if (req.method === 'POST' && url.pathname === '/school-ai-demo/api/math-review') {
      const parts = []; req.on('data', b => parts.push(b));req.on('end', () => {
        submissions.push(Buffer.concat(parts));const failure=nextFailure;nextFailure=null;
        setTimeout(() => {res.setHeader('Content-Type', 'application/json');res.statusCode=failure?.status||200;res.end(JSON.stringify(failure?.payload||{result:sampleResult,model:'deepseek-v4.1-flash',remaining:49}));},120);
      });return;
    }
    if (url.pathname === '/school-ai-demo/api/state') {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ templates: [{ slug: 'test', name: '测试表格' }], ai: { configured: true }, sessions: [] }));
      return;
    }
    if (url.pathname === '/school-ai-demo/' && process.env.SCHOOL_HOME_FIXTURE) {
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      res.end(fs.readFileSync(process.env.SCHOOL_HOME_FIXTURE));
      return;
    }
    const relative = decodeURIComponent(url.pathname).replace(/^\/school-ai-demo\//, '');
    const file = path.resolve(root, relative);
    if (!file.startsWith(root + path.sep)) { res.writeHead(403); res.end(); return; }
    try {
      const body = fs.readFileSync(file);
      res.setHeader('Content-Type', ({ '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'application/javascript', '.png': 'image/png', '.svg': 'image/svg+xml', '.webp': 'image/webp' })[path.extname(file)] || 'application/octet-stream');
      res.end(body);
    } catch (_) { res.writeHead(404); res.end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'school-math-browser.'));
  const chrome = spawn(process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', ['--headless=new', '--remote-debugging-port=0', '--user-data-dir=' + profile, '--no-first-run', '--no-default-browser-check', 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] });
  let ws;
  try {
    const endpoint = await new Promise((resolve, reject) => {
      let log = '';
      const timer = setTimeout(() => reject(Error('Chrome startup timeout')), 20000);
      chrome.once('error', error => { clearTimeout(timer); reject(error); });
      chrome.stderr.on('data', data => {
        log += data;
        const match = log.match(/DevTools listening on (ws:\/\/[^\s]+)/);
        if (match) { clearTimeout(timer); resolve(match[1]); }
      });
    });
    ws = new WebSocket(endpoint);
    await new Promise(resolve => ws.addEventListener('open', resolve));
    let id = 0;
    let choosers = 0;
    const pending = new Map(), errors = [];
    ws.addEventListener('message', event => {
      const message = JSON.parse(event.data);
      if (message.id) {
        const entry = pending.get(message.id);
        pending.delete(message.id);
        message.error ? entry.reject(Error(JSON.stringify(message.error))) : entry.resolve(message.result);
      }
      if (message.method === 'Runtime.exceptionThrown') errors.push(message.params.exceptionDetails.text);
      if (message.method === 'Page.fileChooserOpened') choosers++;
    });
    const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
      const n = ++id;
      pending.set(n, { resolve, reject });
      ws.send(JSON.stringify({ id: n, method, params, sessionId }));
    });
    const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
    const call = (method, params) => send(method, params, sessionId);
    await call('Page.enable');
    await call('Page.setInterceptFileChooserDialog', { enabled: true });
    await call('Runtime.enable');
    const evaluate = async expression => {
      const response = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true, userGesture: true });
      if (response.exceptionDetails) throw Error(JSON.stringify(response.exceptionDetails));
      return response.result.value;
    };
    const wait = async expression => {
      for (let n = 0; n < 80; n++) {
        if (await evaluate(expression)) return;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      throw Error('Timed out: ' + expression);
    };
    await call('Emulation.setDeviceMetricsOverride', { width: 1200, height: 1000, deviceScaleFactor: 1, mobile: false });
    await call('Page.navigate', { url: origin + '/school-ai-demo/math-review.html' });
    await wait("document.querySelectorAll('.paper img').length===2 && [...document.querySelectorAll('.paper img')].every(img=>img.complete&&img.naturalWidth>0)");
    assert.equal(await evaluate("document.querySelectorAll('.prompt-card').length"), 0, "the shared prompt card should be gone");
    assert.equal(await evaluate("document.body.innerText.includes('共用提示词')"), false);
    assert.equal(await evaluate("document.querySelectorAll('.math-case').length"), 2);
    assert.equal(await evaluate("document.querySelectorAll('form,input[type=file]').length"), 2);
    await wait("document.querySelector('#mathStatus').textContent.includes('今日剩余')");
    const removedTexts = ["我已遮盖个人信息", "请先遮盖姓名", "一次一页", "JPEG / PNG / WebP，原图不超过 5MB", "预置 AI 输出", "看图 2", "回看图 1", "看示例，也要复核", "共用每日", "也可以先对照下面的两组示例"];
    for (const text of removedTexts) assert.equal(await evaluate(`document.body.innerText.includes(${JSON.stringify(text)})`), false, "removed text still present: " + text);
    assert.equal(await evaluate("document.querySelectorAll('.example-prompt code').length"), 2);
    assert.equal(await evaluate("[...document.querySelectorAll('.example-prompt code')].every(code=>code.textContent==='作为初一数学老师，判卷')"), true);
    assert.equal(await evaluate("document.querySelector('#mathPromptInput').value"), '作为初一数学老师，判卷');
    assert.equal(await evaluate("document.querySelector('#mathSubmit').disabled"), true);
    await send('Browser.grantPermissions', { origin, permissions: ['clipboardReadWrite', 'clipboardSanitizedWrite'] });
    for (const test of cases) {
      const actual = await evaluate(`[...document.querySelectorAll('#${test.id} table tr')].map(row=>[...row.cells].map(cell=>cell.textContent.trim()))`);
      assert.deepEqual(actual, test.rows);
      assert.equal(await evaluate(`document.querySelector('#${test.id} .grade-title').textContent`), test.heading);
      const line = cells => '| ' + cells.join(' | ') + ' |';
      const expected = '**' + test.heading + '**\n\n' + [line(test.rows[0]), line(test.rows[0].map(() => '---')), ...test.rows.slice(1).map(line)].join('\n');
      await evaluate(`document.querySelector('[data-copy-case=${test.id}]').click()`);
      await wait(`navigator.clipboard.readText().then(text=>text===${JSON.stringify(expected)})`);
    }
    await evaluate("document.querySelector('[data-image]').click()");
    assert.equal(await evaluate("document.querySelector('dialog').open"), true);
    assert.equal(await evaluate("document.activeElement.id"), 'closeDialog');
    await evaluate("document.querySelector('#dialogImage').click()");
    assert.equal(await evaluate("document.querySelector('dialog').open"), true);
    await call('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
    await call('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
    await wait("!document.querySelector('dialog').open && document.activeElement.hasAttribute('data-image')");
    assert.equal(await evaluate("document.body.style.overflow"), '');
    await evaluate("document.querySelectorAll('[data-image]')[1].click();document.querySelector('#closeDialog').click()");
    await wait("!document.querySelector('dialog').open && document.querySelectorAll('[data-image]')[1].getAttribute('aria-expanded')==='false'");
    await evaluate("document.querySelector('[data-image]').click()");
    await call('Input.dispatchMouseEvent', { type: 'mousePressed', x: 2, y: 2, button: 'left', clickCount: 1 });
    await call('Input.dispatchMouseEvent', { type: 'mouseReleased', x: 2, y: 2, button: 'left', clickCount: 1 });
    await wait("!document.querySelector('dialog').open");
    // Both clipboard fallback paths (success and a readable failure message).
    await evaluate("window.savedClipboard=navigator.clipboard;Object.defineProperty(navigator,'clipboard',{value:undefined,configurable:true});document.querySelector('[data-copy-case=case2]').click()");
    await wait("window.savedClipboard.readText().then(text=>text.includes('判卷：全对，100 分'))");
    await evaluate("window.savedExec=document.execCommand;document.execCommand=()=>false;document.querySelector('[data-copy-case=case2]').click()");
    await wait("document.querySelector('#toast').textContent.includes('请手动选择')");
    await evaluate("document.execCommand=window.savedExec;delete navigator.clipboard;window.scrollTo(0,0)");
    await wait("document.querySelector('#toast').hidden");
    assert.equal(submissions.length, 0, 'Viewing/copying examples must not call the model');
    assert.equal(requests.filter(url => url.endsWith('/api/math-review/status')).length, 1);
    await evaluate("(()=>{const dt=new DataTransfer();dt.items.add(new File([new Uint8Array(5*1024*1024+1)],'huge.png',{type:'image/png'}));document.dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt,bubbles:true}));})()");
    await wait("document.querySelector('#mathError').textContent.includes('5MB')");
    assert.equal(submissions.length, 0);
    // The visible control must be a native label for the file input: a physical
    // click then opens the picker without any script-triggered click.
    assert.equal(await evaluate("document.querySelector('#mathChoose').tagName"), 'LABEL');
    assert.equal(await evaluate("document.querySelector('#mathChoose').htmlFor"), 'mathImageFile');
    const clickedChoosers = choosers;
    await evaluate("document.querySelector('#mathChoose').click()");
    await wait(`(${choosers} > ${clickedChoosers})`);
    const doc = await call('DOM.getDocument');const fileInput = await call('DOM.querySelector', {nodeId:doc.root.nodeId,selector:'#mathImageFile'});
    await call('DOM.setFileInputFiles',{nodeId:fileInput.nodeId,files:[path.join(root,'math/example-02.png')]});
    await wait("!document.querySelector('#mathPreviewBox').hidden");
    assert.equal(await evaluate("document.querySelector('#mathSubmit').disabled"),false);
    await evaluate("document.querySelector('#mathPreviewZoom').click();document.querySelector('#closeDialog').click()");
    await wait("!document.querySelector('dialog').open");
    // A prompt edited by the teacher is sent instead of the default.
    await evaluate("document.querySelector('#mathPromptInput').value='作为初二数学老师，只批改方程题';document.querySelector('#mathPromptInput').dispatchEvent(new Event('input'));document.querySelector('#mathPromptReset').click()");
    assert.equal(await evaluate("document.querySelector('#mathPromptInput').value"),'作为初一数学老师，判卷');
    await evaluate("document.querySelector('#mathPromptInput').value='作为初二数学老师，只批改方程题';document.querySelector('#mathPromptInput').dispatchEvent(new Event('input'));document.querySelector('#mathSubmit').click()");
    assert.equal(await evaluate("document.querySelector('#mathRemove').disabled"),true);
    await wait("document.querySelector('#mathResult').classList.contains('ready')");
    assert.equal(submissions.length,1);
    assert(submissions[0].includes(Buffer.from('filename="math-paper.jpg"')));
    assert(submissions[0].includes(Buffer.from('Content-Type: image/jpeg')));
    assert(submissions[0].includes(Buffer.from('作为初二数学老师，只批改方程题')),'custom prompt missing');
    assert(!submissions[0].includes(Buffer.from('name="consent"')),'consent field must be gone');
    assert.equal(await evaluate("document.querySelectorAll('#mathRows tr').length"),2);
    assert.equal(await evaluate("document.querySelectorAll('#mathRows img').length"),0);
    assert.equal(await evaluate("window.mathXSS || 0"),0);
    await evaluate("document.querySelector('#copyMathResult').click()");
    await wait("navigator.clipboard.readText().then(t=>t.includes('待确认')&&t.includes('&lt;img')&&t.includes('-7'))");
    // Clicking an example fills the live area without auto-submitting.
    const beforeExample = submissions.length;
    await evaluate("document.querySelector('[data-use-example=\"math/example-01.png\"]').click()");
    await wait("!document.querySelector('#mathPreviewBox').hidden && document.querySelector('#mathImageInfo').textContent.includes('500')");
    assert.equal(submissions.length, beforeExample);
    assert.equal(await evaluate("document.querySelectorAll('#mathRows tr').length"),0);
    assert.equal(await evaluate("document.querySelector('#mathPreview').src.startsWith('blob:')"),true);
    nextFailure={status:429,payload:{code:'RATE_LIMIT',error:'操作太频繁，请一分钟后再试。'}};
    await evaluate("document.querySelector('#mathSubmit').click()");await wait("!document.querySelector('#mathError').hidden");
    assert.equal(await evaluate("document.querySelector('#mathPreviewBox').hidden"),false);
    assert.equal(await evaluate("document.querySelector('#mathSubmit').disabled"),false);
    await evaluate("document.querySelector('#mathSubmit').click()");await wait("document.querySelector('#mathResult').classList.contains('ready')");
    assert.equal(submissions.length,3);
    assert(submissions[2].includes(Buffer.from('作为初二数学老师，只批改方程题')),'edited prompt should survive retry');
    await wait("document.querySelector('#toast').hidden");await evaluate('window.scrollTo(0,0)');
    let screenshot = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    fs.writeFileSync(path.join(os.tmpdir(), 'school-math-desktop.png'), Buffer.from(screenshot.data, 'base64'));
    for (const width of [390, 320]) {
      await call('Emulation.setDeviceMetricsOverride', { width, height: 844, deviceScaleFactor: 1, mobile: true });
      await wait("!document.querySelector('#scrollHint1').hidden");
      assert(await evaluate(`document.documentElement.scrollWidth<=${width}`));
      await evaluate("document.querySelector('.table-scroll').scrollLeft=200");
      assert(await evaluate("document.querySelector('.table-scroll').scrollLeft>0"));
    }
    await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await evaluate("document.querySelector('.table-scroll').scrollLeft=0;window.scrollTo(0,0)");
    screenshot = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    fs.writeFileSync(path.join(os.tmpdir(), 'school-math-mobile.png'), Buffer.from(screenshot.data, 'base64'));
    assert.equal(requests.filter(url => url.endsWith('/api/math-review/status')).length, 1, 'Do not poll or generate automatically');
    nextFailure={status:429,payload:{code:'DAILY_LIMIT',error:'今日分析额度已用完。'}};
    await evaluate("document.querySelector('#mathSubmit').click()");await wait("document.querySelector('#mathError').textContent.includes('额度')");
    assert.equal(await evaluate("document.querySelector('#mathSubmit').disabled"),true);
    await evaluate("document.querySelector('#mathRemove').click()");assert.equal(await evaluate("document.querySelector('#mathPreviewBox').hidden"),true);
    // Static examples remain visible even when JS is disabled.
    await call('Emulation.setScriptExecutionDisabled', { value: true });
    await call('Page.navigate', { url: origin + '/school-ai-demo/math-review.html' });
    await wait("document.querySelectorAll('tbody tr').length===10");
    await call('Emulation.setScriptExecutionDisabled', { value: false });
    if (process.env.SCHOOL_HOME_FIXTURE) {
      await call('Page.navigate', { url: origin + '/school-ai-demo/' });
      await wait("document.querySelectorAll('[data-demo]').length===4");
      await evaluate("[...document.querySelectorAll('#demoFilters button')].find(button=>button.textContent==='数学判卷').click()");
      assert.equal(await evaluate("document.querySelector('[data-demo=math-review]').hidden"), false);
      assert.equal(await evaluate("document.querySelector('[data-demo=essay-review]').hidden"), true);
      await evaluate("document.querySelector('[data-demo=math-review] button').click()");
      await wait("location.pathname.endsWith('/math-review.html') && document.querySelectorAll('tbody tr').length===10");
      console.log('PASS homepage category and math card navigation (current homepage fixture + local app.js)');
    } else console.log('Homepage DOM test skipped: set SCHOOL_HOME_FIXTURE to a captured current homepage HTML');
    assert.deepEqual(errors, []);
    console.log('PASS exact 10 table rows/headings, prompt/Markdown clipboard, fallback, all three lightbox closing methods/focus, native file-picker label, no model calls for examples, editable prompt sent to API, removed shared prompt card and labels/hints, example-to-live fill, live upload/paste, duplicate prevention, safe DOM rendering, retry/quota states, no-JS examples, desktop/390/320px layouts, scrollable tables');
  } finally {
    if (ws) ws.close();
    if (chrome.pid && chrome.exitCode === null) { const exited = new Promise(resolve => chrome.once('exit', resolve)); chrome.kill('SIGTERM'); await exited; }
    server.close();
    fs.rmSync(profile, { recursive: true, force: true });
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
