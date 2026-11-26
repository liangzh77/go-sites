async () => {
  // Run on /ai-demo/. Only isolated iframe mocks and synthetic template data.
  const [html, script] = await Promise.all([
    fetch('/ai-demo/').then(r=>r.text()), fetch('/ai-demo/app.js').then(r=>r.text()),
  ]);
  const key = 'ai-demo:catalog:v1:/ai-demo';
  const memory = new Map();
  const results=[];
  let frame;
  const check=(name,ok)=>{if(!ok)throw Error(name);results.push(name);};
  const tick=()=>new Promise(r=>setTimeout(r,30));
  const until=async test=>{for(let n=0;n<100;n++){if(test())return;await tick();}throw Error('UI timeout');};
  const stateData={templates:[{slug:'enrollment',name:'缓存测试表（模拟）',fieldCount:0,fields:['DO_NOT_CACHE_FIELDS']}],ai:{configured:true,model:'test-only'},quota:{remaining:0,unlimited:false},recent:[{id:'DO_NOT_CACHE_SESSION'}]};
  const load=async (hash='',opts={})=>{
    frame?.remove();frame=document.createElement('iframe');frame.style.cssText='width:375px;height:700px;position:fixed;top:0;left:0;background:#fff;z-index:9999';
    document.body.append(frame);
    // Start on an API-free static page. Loading the real form first would
    // leave an in-flight real state request racing the isolated mock below.
    await new Promise(resolve=>{frame.onload=resolve;frame.src='/ai-demo/quotation/';});
    const w=frame.contentWindow,d=w.document;
    d.open();d.write(html.replace(/<script src="app\.js(?:\?[^"]*)?"><\/script>/,''));d.close();
    w.history.replaceState(null,'','/ai-demo/'+hash);
    Object.defineProperty(w,'sessionStorage',{configurable:true,value:{
      getItem:k=>{if(opts.storageBlocked)throw Error('Storage disabled');return memory.get(k)||null;},
      setItem:(k,v)=>{if(opts.storageBlocked)throw Error('Storage disabled');memory.set(k,v);},
    }});
    const requests=[];
    w.fetch=async url=>{
      requests.push(String(url));
      if(opts.delay)await new Promise(r=>setTimeout(r,opts.delay));
      if(opts.fail)throw Error('offline');
      const value=String(url).endsWith('/api/state') ? stateData : {slug:'enrollment',name:'缓存测试表（模拟）',fields:[],html:'<!doctype html><html><body><h1>模拟空表</h1></body></html>'};
      return {ok:true,json:async()=>value};
    };
    w.eval(script);
    return {w,d,requests};
  };
  try {
    let env=await load();
    check('cold list renders all 15 cards synchronously',env.d.querySelectorAll('[data-demo]').length===15);
    check('cold list makes no API requests',env.requests.length===0);
    check('cold school category has four cards',env.d.querySelectorAll('[data-demo]:not([hidden])').length===4);
    check('unknown template count is not fabricated',!env.d.querySelector('[data-demo="ai-fill"]').textContent.includes('共 0'));
    env.d.querySelector('#demoFilters [data-cat="trade"]').click();
    check('category persisted',JSON.parse(memory.get(key)).filter==='trade');
    env=await load();
    check('return restores trade without hash',env.d.querySelectorAll('[data-demo]:not([hidden])').length===3&&env.d.getElementById('categoryTitle').textContent.includes('外贸'));
    check('return makes no API request',env.requests.length===0);
    env=await load('#school',{delay:120});
    check('explicit school overrides cached selection',env.d.querySelectorAll('[data-demo]:not([hidden])').length===4);
    const open=env.d.querySelector('[data-demo="ai-fill"] button');open.click();open.click();
    check('rapid duplicate form clicks coalesced',env.requests.length===1);
    await until(()=>!env.d.getElementById('studio').hidden);
    check('form fetches state and template once each',env.requests.length===2);
    check('fresh exhausted quota disables AI',env.d.getElementById('fillBtn').disabled);
    const cached=JSON.parse(memory.get(key));
    check('only summary fields stored',JSON.stringify(Object.keys(cached.templates[0]).sort())===JSON.stringify(['fieldCount','name','slug']));
    check('no quota, AI config or session data cached',!['quota','ai','recent','fields'].some(k=>Object.hasOwn(cached,k))&&!memory.get(key).includes('DO_NOT_CACHE'));
    env.d.getElementById('backBtn').click();
    check('closing form uses existing cards, no refetch',env.requests.length===2&&env.d.getElementById('studio').hidden);
    env=await load('#school');
    check('summary restored on return',env.d.querySelector('[data-demo="ai-fill"]').textContent.includes('缓存测试表'));
    check('cached summary needs no API call',env.requests.length===0);
    env=await load('#enrollment');
    await until(()=>!env.d.getElementById('studio').hidden);
    check('direct form bookmark still loads fresh status',env.requests.length===2&&env.d.getElementById('fillBtn').disabled);
    env.d.getElementById('backBtn').click();
    check('direct form back adds no API call',env.requests.length===2);
    memory.set(key,'{broken');env=await load('#trade');
    check('corrupt storage keeps all cards',env.d.querySelectorAll('[data-demo]').length===15&&env.requests.length===0);
    memory.set(key,JSON.stringify({templates:[{slug:'bad/path',name:'invalid',fieldCount:0}],filter:'trade'}));env=await load();
    check('invalid schema ignored safely',env.d.querySelectorAll('[data-demo]').length===15&&env.requests.length===0);
    env=await load('#trade',{storageBlocked:true});
    check('disabled browser storage still shows trade immediately',env.d.querySelectorAll('[data-demo]:not([hidden])').length===3&&env.requests.length===0);
    env=await load('#school',{fail:true});env.d.querySelector('[data-demo="ai-fill"] button').click();
    await until(()=>env.d.getElementById('toast').textContent.includes('无法读取'));
    check('failed status never hides catalog or opens stale form',env.d.getElementById('studio').hidden&&env.d.querySelectorAll('[data-demo]').length===15);
    // Actual navigation, no mocks: all three detail pages return to cached catalog.
    frame.remove();frame=document.createElement('iframe');document.body.append(frame);
    await new Promise(resolve=>{frame.onload=resolve;frame.src='/ai-demo/#trade';});
    for (const slug of ['customer-email','quotation','order-validation']) {
      check(slug+' native home has no state request',!frame.contentWindow.performance.getEntriesByType('resource').some(r=>r.name.includes('/api/state')));
      await new Promise(resolve=>{frame.onload=resolve;frame.contentDocument.querySelector('[data-demo="'+slug+'"] button').click();});
      check(slug+' detail opened',frame.contentWindow.location.pathname.endsWith('/'+slug+'/'));
      await new Promise(resolve=>{frame.onload=resolve;frame.contentDocument.querySelector('a[href="../#trade"]').click();});
      check(slug+' return immediately has three visible cards',frame.contentDocument.querySelectorAll('[data-demo]:not([hidden])').length===3);
      check(slug+' return has no state request',!frame.contentWindow.performance.getEntriesByType('resource').some(r=>r.name.includes('/api/state')));
    }
    return {passed:results.length,results};
  } finally {frame?.remove();}
}
