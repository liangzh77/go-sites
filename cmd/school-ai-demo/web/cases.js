(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const tabs = [...document.querySelectorAll('.case-steps [role="tab"]')];
  function showStep(id, updateURL = true) {
    if (!tabs.some(tab => tab.getAttribute('aria-controls') === id)) return;
    tabs.forEach(tab => {
      const active = tab.getAttribute('aria-controls') === id;
      tab.setAttribute('aria-selected', String(active));
      tab.tabIndex = active ? 0 : -1;
      $(tab.getAttribute('aria-controls')).hidden = !active;
    });
    if (updateURL) history.replaceState(null, '', '#' + id);
    $('copy-status').textContent = '';
  }
  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => showStep(tab.getAttribute('aria-controls')));
    tab.addEventListener('keydown', event => {
      let next;
      if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
      if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length;
      if (event.key === 'Home') next = 0;
      if (event.key === 'End') next = tabs.length - 1;
      if (next === undefined) return;
      event.preventDefault(); tabs[next].focus(); tabs[next].click();
    });
  });
  document.querySelectorAll('[data-step]').forEach(button => button.addEventListener('click', () => {
    showStep(button.dataset.step);
    $('step-' + button.dataset.step).focus({preventScroll:true});
    document.querySelector('.case-workspace').scrollIntoView({block:'start'});
  }));
  const documents = [...document.querySelectorAll('.source-document')];
  function selectSource(id, updateURL = false) {
    if (!documents.some(doc => doc.id === id)) return;
    documents.forEach(doc => { doc.hidden = doc.id !== id; });
    document.querySelectorAll('[data-select-source]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.selectSource === id)));
    if (updateURL) history.replaceState(null, '', '#' + id);
  }
  const sourceList = document.querySelector('.source-list');
  documents.forEach(doc => {
    const button = document.createElement('button');
    button.type = 'button'; button.dataset.selectSource = doc.id;
    button.setAttribute('aria-controls', doc.id);
    const id = document.createElement('b'); id.textContent = doc.id;
    button.append(id, document.createTextNode(doc.dataset.sourceTitle));
    button.addEventListener('click', () => selectSource(doc.id, true));
    sourceList.append(button);
  });
  selectSource(documents.find(doc => doc.dataset.default === 'true')?.id || documents[0]?.id);
  function revealHash() {
    const id = location.hash.slice(1);
    const source = documents.find(doc => doc.id === id);
    showStep(source ? 'materials' : tabs.some(tab => tab.getAttribute('aria-controls') === id) ? id : 'materials', false);
    if (source) selectSource(id);
  }
  document.querySelectorAll('[data-source]').forEach(link => link.addEventListener('click', event => {
    event.preventDefault();
    showStep('materials', false); selectSource(link.dataset.source, true);
    const button = document.querySelector('[data-select-source="' + link.dataset.source + '"]');
    button?.focus({preventScroll:true});
    document.querySelector('.source-browser').scrollIntoView({block:'start'});
  }));
  revealHash();
  window.addEventListener('hashchange', revealHash);
  document.querySelectorAll('[data-copy]').forEach(button => button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText($(button.dataset.copy).textContent);
      $('copy-status').textContent = '已复制待审草稿，未发送。';
    } catch (_) {
      $('copy-status').textContent = '复制未成功，请手动选择上方草稿复制。';
    }
  }));
  $('calculate-price')?.addEventListener('click', () => {
    const price = Number($('target-price').value);
    if (!$('target-price').checkValidity() || !Number.isFinite(price) || price <= 0) {
      $('price-result').textContent = '请输入 0.01–9999.00 的单价，最多两位小数。';
      return;
    }
    const revenue = price * 800 * 7.2;
    const margin = (revenue - 18080) / revenue * 100;
    $('price-result').textContent = `USD ${price.toFixed(2)} → 总额 ${(price * 800).toFixed(2)}；测算毛利率 ${margin.toFixed(2)}%。${margin < 20 ? '低于目标，须另行审批。' : '达到测算目标，但正式报价仍须人工批准。'} 此操作未更改对客报价。`;
  });
  let staged = false;
  document.querySelectorAll('[data-po]').forEach(button => button.addEventListener('click', () => {
    const revision = button.dataset.po;
    let text;
    if (revision === '0') text = 'Rev0：数量与条款冲突，暂停可执行订单录入。'+(staged ? '已有 Rev1 草稿不被旧版覆盖。' : '没有生成草稿。');
    else if (revision === '2') text = 'Rev2：新修订必须取原文、逐项对比并人工复核；不能当重复件忽略，也不能自动覆盖 Rev1。此处没有预置 Rev2 内容，不猜测改了什么。';
    else {
      text = staged ? '重复 Rev1：关联已有模拟草稿，未重复建单。定金与图稿仍待确认，不放行生产。' : 'Rev1：数量和条款已对齐，暂存 1 份待复核模拟草稿；未写 ERP。定金与图稿仍待确认，不放行生产。';
      staged = true;
    }
    $('po-state').textContent = text;
  }));
})();
