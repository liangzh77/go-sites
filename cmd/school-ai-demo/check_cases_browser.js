async () => {
  const results = [];
  const check = (name, ok) => { results.push({name,ok:!!ok}); if (!ok) throw new Error(name); };
  const frame = document.createElement('iframe');
  frame.style.cssText='position:fixed;left:0;top:0;height:800px;z-index:99999;background:white;border:0';
  document.body.append(frame);
  const open = async (path,width=375) => {
    frame.style.width=width+'px';
    await new Promise((resolve,reject)=>{const t=setTimeout(()=>reject(new Error('load timeout')),15000);frame.onload=()=>{clearTimeout(t);resolve();};frame.src='/ai-demo/'+path;});
    await new Promise(r=>setTimeout(r,150));
    return frame.contentDocument;
  };
  try {
    for (const width of [375,768,1440]) for (const path of ['', 'customer-email/', 'quotation/', 'order-validation/', 'lesson-plan.html','essay-review.html','math-review.html']) {
      const d=await open(path,width);
      check(width+' '+(path||'home')+' no horizontal overflow',d.documentElement.scrollWidth<=width+1);
      check(path+' no old URLs',![...d.querySelectorAll('[href],[src]')].some(el=>/\/school-ai-demo\//.test(el.getAttribute('href')||el.getAttribute('src')||'')));
      for (const tab of d.querySelectorAll('.case-steps [role="tab"]')) {
        tab.click();
        check(width+' '+path+' '+tab.id+' only one panel',d.querySelectorAll('.step-body:not([hidden])').length===1);
        check(width+' '+path+' '+tab.id+' no overflow',d.documentElement.scrollWidth<=width+1);
      }
    }
    let d=await open('');
    for(let n=0;n<60&&!d.querySelector('[data-demo]');n++) await new Promise(r=>setTimeout(r,100));
    check('tools follows production',Array.from(d.querySelectorAll('#demoFilters button'),b=>b.dataset.cat).join(',')==='school,trade,production,tools');
    d.querySelector('#demoFilters [data-cat="tools"]').click();
    check('tools links and prices',Array.from(d.querySelectorAll('[data-demo]:not([hidden])'),a=>a.getAttribute('href')+' '+a.querySelector('.tg').textContent).join('|')==='https://www.wogg.cool 每月 19 元|https://ikuuu.top 每月 12 元|https://developers.openai.com/codex/app/ 编程助手|https://www.deepseek.com/harness/ Agent 框架|https://pi.dev/ 终端编程助手|https://paseo.sh/ Agent 管理');
    d.querySelector('#demoFilters [data-cat="production"]').click();
    check('two production skills',d.querySelectorAll('[data-demo]:not([hidden])').length===2&&['pop-sticker-studio','ink-story-film'].every(k=>d.querySelector('[data-demo="'+k+'"]:not([hidden])')));
    check('four school cards',d.querySelectorAll('[data-demo][data-cat="school"]').length===4);
    d.querySelector('#demoFilters [data-cat="trade"]').click();
    check('three trade cards',d.querySelectorAll('[data-demo]:not([hidden])').length===3);
    d=await open('customer-email/');
    check('email starts with latest M4',!d.getElementById('M4').hidden);
    check('email five sources',d.querySelectorAll('[data-select-source]').length===5);
    for (const source of d.querySelectorAll('[data-select-source]')) {
      source.click();check('source '+source.dataset.selectSource,d.querySelectorAll('.source-document:not([hidden])').length===1&&!d.getElementById(source.dataset.selectSource).hidden);
    }
    d.getElementById('step-materials').dispatchEvent(new frame.contentWindow.KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));
    check('keyboard step forward',!d.getElementById('result').hidden);
    d.querySelector('.evidence').open=true;
    check('inline source evidence',d.querySelector('.evidence').open);
    d.querySelector('[data-source="M2"]').click();
    check('source link reveals original and step',!d.getElementById('M2').hidden&&!d.getElementById('materials').hidden);
    d.getElementById('step-handoff').click();
    let copied='';
    Object.defineProperty(frame.contentWindow.navigator,'clipboard',{configurable:true,value:{writeText:async text=>{copied=text;}}});
    d.querySelector('[data-copy]').click();await new Promise(r=>setTimeout(r,20));
    check('email draft copies only draft',copied.startsWith('Hi Alex,')&&!copied.includes('内部交接'));
    Object.defineProperty(frame.contentWindow.navigator,'clipboard',{configurable:true,value:{writeText:async()=>{throw Error('denied');}}});
    d.querySelector('[data-copy]').click();await new Promise(r=>setTimeout(r,20));
    check('clipboard failure handled',d.getElementById('copy-status').textContent.includes('未成功'));
    d=await open('quotation/');
    check('quote five sources',d.querySelectorAll('[data-select-source]').length===5);
    d.getElementById('step-result').click();
    for(const [value,expected] of [['3.70','15.17%'],['3.93','20.13%'],['0','请输入'],['','请输入'],['3.999','请输入']]){
      d.getElementById('target-price').value=value;d.getElementById('calculate-price').click();
      check('price '+value,d.getElementById('price-result').textContent.includes(expected));
    }
    d.getElementById('step-handoff').click();
    Object.defineProperty(frame.contentWindow.navigator,'clipboard',{configurable:true,value:{writeText:async text=>{copied=text;}}});
    d.querySelector('[data-copy]').click();await new Promise(r=>setTimeout(r,20));
    check('public quote excludes internal costs',copied.includes('3,144.00')&&!/18,080|18.80|毛利|20%|18.30/.test(copied));
    d=await open('order-validation/');
    check('order six source documents',d.querySelectorAll('[data-select-source]').length===6);
    check('order starts with conflicting PO0',!d.getElementById('PO0').hidden);
    d.getElementById('step-handoff').click();
    for(const [rev,expected] of [['0','没有生成'],['1','暂存 1 份'],['1','未重复建单'],['0','不被旧版覆盖'],['2','不能自动覆盖']]){
      d.querySelector('[data-po="'+rev+'"]').click();check('PO '+rev+' '+expected,d.getElementById('po-state').textContent.includes(expected));
    }
    check('all cases clearly synthetic',d.body.textContent.includes('人工编写预置'));
    return {passed:results.length,results};
  } finally { frame.remove(); }
}
