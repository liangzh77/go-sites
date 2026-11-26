async () => {
  const checks=[];
  const check=(name,ok)=>{if(!ok)throw Error(name);checks.push(name);};
  const wait=async test=>{for(let n=0;n<100;n++){if(test())return;await new Promise(r=>setTimeout(r,50));}throw Error('Page not ready');};
  const frame=document.createElement('iframe');
  frame.style.cssText='position:fixed;top:0;left:0;height:800px;border:0;background:#fff;z-index:9999';
  document.body.append(frame);
  const pages=['#enrollment','#collection','lesson-plan.html','essay-review.html','math-review.html','customer-email/','quotation/','order-validation/'];
  try {
    for(const width of [375,768,1440]) for(const path of pages) {
      frame.style.width=width+'px';
      const [file,hash]=path.split('#');
      await new Promise(resolve=>{frame.onload=resolve;frame.src='/ai-demo/'+file+'?return-check='+Date.now()+(hash?'#'+hash:'');});
      const d=frame.contentDocument,w=frame.contentWindow;
      if(path.startsWith('#'))await wait(()=>!d.getElementById('studio').hidden);
      await new Promise(r=>w.requestAnimationFrame(()=>w.requestAnimationFrame(r)));
      const back=d.querySelector('.case-nav .case-return');
      const nav=d.querySelector('.case-nav');
      const rect=back.getBoundingClientRect(),nr=nav.getBoundingClientRect(),brand=nav.querySelector('.brand').getBoundingClientRect();
      check(path+' '+width+' one visible header return',d.querySelectorAll('.case-return').length===1&&rect.width>0&&rect.height>=44);
      check(path+' '+width+' top-right same row',Math.abs(rect.right-(nr.right-parseFloat(w.getComputedStyle(nav).paddingRight)))<=2&&Math.abs((rect.top+rect.bottom-brand.top-brand.bottom)/2)<2);
      const explicit=[...d.querySelectorAll('a,button')].filter(el=>/全部\s*(demo|案例)|返回外贸案例|浏览全部/.test(el.textContent)&&el.getBoundingClientRect().height>0);
      check(path+' '+width+' no duplicate return',explicit.length===1&&explicit[0]===back);
      check(path+' '+width+' no horizontal overflow',d.documentElement.scrollWidth<=width+1);
      if(width===375) {
        const form=path.startsWith('#');
        const before=w.performance.getEntriesByType('resource').filter(r=>r.name.includes('/api/state')).length;
        if(form)back.click();else await new Promise(resolve=>{frame.onload=resolve;back.click();});
        const home=frame.contentDocument,hw=frame.contentWindow;
        const category=path.includes('-plan.html')||path.includes('-review.html')||form?'school':'trade';
        check(path+' restores category',hw.location.hash==='#'+category&&home.querySelector('#demoFilters [aria-pressed="true"]').dataset.cat===category);
        check(path+' cards immediately visible',home.querySelectorAll('[data-demo]:not([hidden])').length===(category==='school'?4:3));
        const after=hw.performance.getEntriesByType('resource').filter(r=>r.name.includes('/api/state')).length;
        check(path+' return adds no state request',after===(form?before:0));
        if(form)check(path+' return control hidden on home',home.getElementById('backBtn').hidden&&!home.getElementById('homeSiteLink').hidden);
      }
    }
    return {passed:checks.length,checks};
  } finally {frame.remove();}
}
