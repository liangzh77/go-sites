const http=require('node:http'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),{spawn}=require('node:child_process'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../school-ai-demo/web');
(async()=>{
 let submissions=[];
 const server=http.createServer((req,res)=>{
  if(req.url.endsWith('/api/essay-review/status')){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({configured:true,remaining:50,limit:50,model:'deepseek-v4.1-flash',imageSupported:true}));return;}
  if(req.method==='POST'){let parts=[];req.on('data',b=>parts.push(b));req.on('end',()=>{submissions.push(Buffer.concat(parts));res.setHeader('Content-Type','application/json');res.end(JSON.stringify({review:'你写的小猫玩毛线很生动。建议补充一个跳起来的动作。',model:'deepseek-v4.1-flash',characters:29,remaining:49}));});return;}
  const name=decodeURIComponent(req.url.split('?')[0]).replace('/school-ai-demo/','');const file=path.join(root,name);
  try{const ext=path.extname(file);res.setHeader('Content-Type',({'.html':'text/html; charset=utf-8','.js':'application/javascript','.css':'text/css','.png':'image/png'})[ext]||'application/octet-stream');res.end(fs.readFileSync(file));}catch(_){res.statusCode=404;res.end();}
 });await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin=`http://127.0.0.1:${server.address().port}`;
 const profile=fs.mkdtempSync(path.join(os.tmpdir(),'school-essay-browser.'));
 const chrome=spawn(process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--headless=new','--remote-debugging-port=0','--user-data-dir='+profile,'--no-first-run','--no-default-browser-check','about:blank'],{stdio:['ignore','ignore','pipe']});
 let ws;
 try{
  const endpoint=await new Promise((resolve,reject)=>{let log='';const timer=setTimeout(()=>reject(Error('Chrome startup timed out')),20000);chrome.stderr.on('data',b=>{log+=b;const m=log.match(/DevTools listening on (ws:\/\/[^\s]+)/);if(m){clearTimeout(timer);resolve(m[1]);}});});
  ws=new WebSocket(endpoint);await new Promise(r=>ws.addEventListener('open',r));let id=0;const pending=new Map();const errors=[];
  ws.addEventListener('message',e=>{const m=JSON.parse(e.data);if(m.id){const p=pending.get(m.id);pending.delete(m.id);if(m.error)p.reject(Error(JSON.stringify(m.error)));else p.resolve(m.result);}if(m.method==='Runtime.exceptionThrown')errors.push(m.params.exceptionDetails.text);});
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const n=++id;pending.set(n,{resolve,reject});ws.send(JSON.stringify({id:n,method,params,sessionId}));});
  const target=await send('Target.createTarget',{url:'about:blank'});const {sessionId}=await send('Target.attachToTarget',{targetId:target.targetId,flatten:true});
  const call=(m,p)=>send(m,p,sessionId);await call('Page.enable');await call('Runtime.enable');
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;};
  const wait=async expression=>{for(let i=0;i<60;i++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,100));}throw Error('Timed out: '+expression);};
  await call('Emulation.setDeviceMetricsOverride',{width:1200,height:1000,deviceScaleFactor:1,mobile:false});await call('Page.navigate',{url:origin+'/school-ai-demo/essay-review.html'});
  await wait("document.querySelector('#modelStatus')?.textContent.includes('今日上限')");
  await send('Browser.grantPermissions',{origin,permissions:['clipboardReadWrite','clipboardSanitizedWrite']});
  await evaluate("document.querySelector('#copyPrompt').click()");
  await wait("navigator.clipboard.readText().then(t=>t===document.querySelector('#promptText').textContent)");
  assert.equal(await evaluate("document.querySelectorAll('.example').length"),2);assert.equal(submissions.length,0);
  await evaluate("document.querySelector('[data-image]').click()");assert.equal(await evaluate("document.querySelector('dialog').open"),true);await evaluate("document.querySelector('#closeDialog').click()");
  await evaluate("document.querySelector('#essayText').value='小猫在玩毛线，它跳了起来。';document.querySelector('#essayText').dispatchEvent(new Event('input'));document.querySelector('#consent').click()");
  assert.equal(await evaluate("document.querySelector('#submitBtn').disabled"),false);await evaluate("document.querySelector('#submitBtn').click()");await wait("document.querySelector('#resultBox').classList.contains('ready')");assert.equal(submissions.length,1);assert(submissions[0].includes(Buffer.from('小猫在玩毛线')));
  await evaluate("document.querySelector('#copyResult').click()");await wait("navigator.clipboard.readText().then(t=>t===document.querySelector('#reviewText').textContent)");
  await evaluate("(async()=>{const blob=await(await fetch('essay/example-dad.png')).blob();const dt=new DataTransfer();dt.items.add(new File([blob],'essay.png',{type:'image/png'}));document.dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt,bubbles:true}));})()");
  await wait("!document.querySelector('#previewBox').hidden && !document.querySelector('#submitBtn').disabled");assert.equal(await evaluate("document.querySelector('#imageTab').getAttribute('aria-selected')"),'true');assert.equal(await evaluate("document.querySelector('#essayText').value"),'');
  await evaluate("document.querySelector('#submitBtn').click()");await wait("document.querySelector('#resultBox').classList.contains('ready')");assert.equal(submissions.length,2);assert(submissions[1].includes(Buffer.from('filename="essay.jpg"')));assert(!submissions[1].includes(Buffer.from('小猫在玩毛线')));
  const doc=await call('DOM.getDocument');const field=await call('DOM.querySelector',{nodeId:doc.root.nodeId,selector:'#imageFile'});await call('DOM.setFileInputFiles',{nodeId:field.nodeId,files:[path.join(root,'essay/example-aunt.png')]});await wait("!document.querySelector('#previewBox').hidden && document.querySelector('#imageInfo').textContent.includes('636')");await evaluate("document.querySelector('#removeImage').click()");assert.equal(await evaluate("document.querySelector('#previewBox').hidden"),true);assert.equal(await evaluate("document.querySelector('#submitBtn').disabled"),true);
  await evaluate("document.querySelector('#textTab').click();document.querySelector('#essayText').value='字'.repeat(8001);document.querySelector('#essayText').dispatchEvent(new Event('input'))");assert.equal(await evaluate("document.querySelector('#submitBtn').disabled"),true);
  await evaluate("document.querySelector('#essayText').value='';document.querySelector('#essayText').dispatchEvent(new Event('input'));window.scrollTo(0,0)");
  const screenshot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});fs.writeFileSync(path.join(os.tmpdir(),'school-essay-desktop.png'),Buffer.from(screenshot.data,'base64'));
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await new Promise(r=>setTimeout(r,200));assert(await evaluate('document.documentElement.scrollWidth <= 390'));
  const mobile=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});fs.writeFileSync(path.join(os.tmpdir(),'school-essay-mobile.png'),Buffer.from(mobile.data,'base64'));
  assert.deepEqual(errors,[]);console.log('PASS: static examples/no model requests, image lightbox, prompt/result clipboard, text submission, clipboard image, image upload/removal, mode isolation, 8000-char bound, desktop/mobile layout, no JS exceptions');
 }finally{if(ws)ws.close();const exited=new Promise(r=>chrome.once('exit',r));chrome.kill('SIGTERM');await exited;server.close();fs.rmSync(profile,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1;});
