(() => {
  "use strict";

  const state = {
    csrfToken: "",
    providers: [],
    options: { presets: [], protocols: [], authModes: [] },
    statusLabels: {},
    filters: { search: "", provider: "", status: "", expiry: false },
    editProviderID: "",
    editKeyID: "",
    editModelID: "",
    activeKeyID: "",
    activeProviderID: "",
    modelProviderID: "",
    modelSource: "manual",
    pending: new Set(),
    toastTimer: 0,
    quotaTooltipKeyID: "",
    quotaTooltipTimer: 0,
  };

  const $ = (id) => document.getElementById(id);
  const loginView = $("loginView");
  const appView = $("appView");
  const providerList = $("providerList");
  const busyBar = $("busyBar");
  const toast = $("toast");

  class ApiError extends Error {
    constructor(message, status, code) {
      super(message);
      this.status = status;
      this.code = code;
    }
  }

  function escapeHTML(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function escapeAttr(value) {
    return escapeHTML(value).replaceAll("\n", "&#10;");
  }

  function apiPath(path) {
    return `/keys/api/${String(path).replace(/^\/+/, "")}`;
  }

  async function api(path, options = {}) {
    const method = (options.method || "GET").toUpperCase();
    const headers = { Accept: "application/json" };
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    if (method !== "GET" && method !== "HEAD" && state.csrfToken) {
      headers["X-CSRF-Token"] = state.csrfToken;
    }
    const response = await fetch(apiPath(path), {
      method,
      credentials: "same-origin",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    let payload = null;
    try {
      payload = await response.json();
    } catch {
      payload = null;
    }
    if (response.status === 401 && path !== "session") {
      showLogin();
    }
    if (!response.ok) {
      const message = payload?.error?.message || `请求失败（HTTP ${response.status}）`;
      throw new ApiError(message, response.status, payload?.error?.code || "");
    }
    return payload;
  }

  function setBusy(busy) {
    busyBar.hidden = !busy;
    document.body.setAttribute("aria-busy", busy ? "true" : "false");
  }

  function showToast(message, type = "success") {
    window.clearTimeout(state.toastTimer);
    toast.textContent = message;
    toast.className = `toast show${type === "error" ? " error" : ""}`;
    state.toastTimer = window.setTimeout(() => {
      toast.className = "toast";
    }, 3600);
  }

  function errorMessage(error) {
    if (error instanceof ApiError) return error.message;
    if (error?.name === "AbortError") return "请求超时";
    return error?.message || "操作失败";
  }

  function setButtonPending(id, pending, pendingText = "处理中…") {
    const button = typeof id === "string" ? $(id) : id;
    if (!button) return;
    if (pending) {
      if (!button.dataset.originalText) button.dataset.originalText = button.textContent;
      button.disabled = true;
      button.textContent = pendingText;
    } else {
      button.disabled = false;
      if (button.dataset.originalText) button.textContent = button.dataset.originalText;
    }
  }

  function showLogin(message = "") {
    state.csrfToken = "";
    state.providers = [];
    state.pending.clear();
    closeAllDialogs();
    appView.hidden = true;
    loginView.hidden = false;
    $("loginPassword").value = "";
    $("loginError").textContent = message;
    window.setTimeout(() => $("loginPassword").focus(), 30);
  }

  function showApp() {
    loginView.hidden = true;
    appView.hidden = false;
  }

  function closeAllDialogs() {
    document.querySelectorAll("dialog[open]").forEach((dialog) => dialog.close());
    clearRevealState();
    clearExportState();
  }

  function providerByID(id) {
    return state.providers.find((provider) => provider.id === id);
  }

  function keyByID(id) {
    for (const provider of state.providers) {
      const key = provider.keys.find((item) => item.id === id);
      if (key) return { provider, key };
    }
    return null;
  }

  function modelByID(provider, id) {
    return provider?.models.find((model) => model.id === id);
  }

  function modelMetadata(model) {
    if (!model?.metadata) return {};
    if (typeof model.metadata === "object") return model.metadata;
    try {
      return JSON.parse(model.metadata);
    } catch {
      return {};
    }
  }

  function modelDisplayName(model) {
    return modelMetadata(model).name || model?.modelId || "未命名模型";
  }

  function tokenLimit(value) {
    return typeof value === "number" ? value.toLocaleString("zh-CN") : "未知";
  }

  function modelCapabilityChips(model) {
    const metadata = modelMetadata(model);
    const chips = [];
    if (model.supportsThinking === true) chips.push("思考");
    if (metadata.tool_call === true) chips.push("工具调用");
    if (metadata.structured_output === true) chips.push("结构化输出");
    if (metadata.attachment === true) chips.push("附件");
    if (metadata.temperature === true) chips.push("Temperature");
    return chips;
  }

  function renderModelCatalogItem(model) {
    const metadata = modelMetadata(model);
    const chips = modelCapabilityChips(model);
    return `<article class="model-catalog-item">
      <div class="model-catalog-main">
        <div class="model-catalog-title">
          <strong>${escapeHTML(modelDisplayName(model))}</strong>
          ${model.defaultTest ? '<span class="mini-badge">默认测试</span>' : ""}
        </div>
        <code>${escapeHTML(model.modelId)}</code>
        ${metadata.description ? `<p>${escapeHTML(metadata.description)}</p>` : ""}
      </div>
      <div class="model-specs">
        <span>上下文 <strong>${tokenLimit(model.contextWindow)}</strong></span>
        <span>最大输出 <strong>${tokenLimit(model.maxOutput)}</strong></span>
        <span>思考 <strong>${model.supportsThinking === true ? "支持" : model.supportsThinking === false ? "不支持" : "未知"}</strong></span>
        ${model.thinkingLevel ? `<span>模式 <strong>${escapeHTML(model.thinkingLevel)}</strong></span>` : ""}
        ${chips.length ? `<span>能力 <strong>${escapeHTML(chips.join(" · "))}</strong></span>` : ""}
      </div>
    </article>`;
  }

  function protocolLabel(id) {
    return state.options.protocols.find((item) => item.id === id)?.label || id;
  }

  function presetByID(id) {
    return state.options.presets.find((item) => item.id === id);
  }

  function statusLabel(id) {
    return state.statusLabels[id] || id;
  }

  function isExpiring(key) {
    if (typeof key.expiryDays !== "number") return false;
    return key.expiryDays <= (Number(key.reminderDays) || 0);
  }

  function matchesStatus(key, filter) {
    if (!filter) return true;
    if (filter === "failure") return !["success", "untested"].includes(key.status);
    if (filter === "disabled") return !key.enabled;
    if (filter === "stale") return Boolean(key.statusStale);
    return key.status === filter;
  }

  function filteredProviders() {
    const query = state.filters.search.trim().toLowerCase();
    return state.providers
      .filter((provider) => !state.filters.provider || provider.id === state.filters.provider)
      .map((provider) => {
        const providerMatch = [provider.name, provider.note, provider.category, provider.baseUrl]
          .join(" ")
          .toLowerCase()
          .includes(query);
        const keys = provider.keys.filter((key) => {
          const haystack = [key.alias, key.note, key.testModel, key.maskedKey]
            .join(" ")
            .toLowerCase();
          const searchMatch = !query || providerMatch || haystack.includes(query);
          const expiryMatch = !state.filters.expiry || isExpiring(key);
          return searchMatch && expiryMatch && matchesStatus(key, state.filters.status);
        });
        return { provider, keys };
      })
      .filter(({ provider, keys }) => {
        if (state.filters.search && !keys.length) return false;
        if (state.filters.status && state.filters.status !== "disabled" && !keys.length) return false;
        if (state.filters.expiry && !keys.length) return false;
        return true;
      });
  }

  function renderStats() {
    const keys = state.providers.flatMap((provider) => provider.keys);
    const active = keys.filter((key) => key.enabled);
    const success = active.filter((key) => key.status === "success" && !key.statusStale);
    const expiring = keys.filter((key) => key.enabled && isExpiring(key));
    $("providerStat").textContent = String(state.providers.length);
    $("providerStatHint").textContent = state.providers.length ? `${state.providers.filter((p) => p.enabled).length} 个启用` : "暂无配置";
    $("keyStat").textContent = String(active.length);
    $("keyStatHint").textContent = `${keys.length - active.length} 个 Key 已停用`;
    $("successStat").textContent = String(success.length);
    $("expiryStat").textContent = String(expiring.length);
  }

  function renderProviderFilter() {
    const select = $("providerFilter");
    const current = state.filters.provider;
    select.innerHTML = '<option value="">全部 Provider</option>' + state.providers
      .map((provider) => `<option value="${escapeAttr(provider.id)}">${escapeHTML(provider.name)}</option>`)
      .join("");
    select.value = current;
  }

  function renderStatus(key) {
    if (!key.enabled) {
      return '<span class="status-badge status-untested">已停用</span>';
    }
    const label = statusLabel(key.status);
    const stale = key.statusStale && key.status !== "untested"
      ? '<span class="mini-badge">配置已修改，待重测</span>'
      : "";
    return `<div class="status-group">
      <span class="status-badge status-${escapeAttr(key.status)}">${escapeHTML(label)}</span>
      ${stale}
    </div>`;
  }

  function renderExpiry(key) {
    if (!key.expiresOn) return '<span class="mini-badge neutral">未填写</span>';
    if (typeof key.expiryDays !== "number") return `<span class="meta-stack"><strong>${escapeHTML(key.expiresOn)}</strong></span>`;
    const days = key.expiryDays;
    const klass = days < 0 ? "danger" : days <= (key.reminderDays || 0) ? "" : "neutral";
    const text = days < 0 ? `已过期 ${Math.abs(days)} 天` : days === 0 ? "今天到期" : `剩 ${days} 天`;
    return `<span class="meta-stack"><strong>${escapeHTML(key.expiresOn)}</strong><span class="mini-badge ${klass}">${text}</span></span>`;
  }

  function renderDetectionMeta(key) {
    if (!key.testedAt) return '<span class="meta-stack"><span>尚未检测</span></span>';
    const timestamp = new Date(key.testedAt);
    const formatted = Number.isNaN(timestamp.valueOf())
      ? escapeHTML(key.testedAt)
      : timestamp.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
    const model = key.testedModel || key.testModel || "未指定模型";
    return `<span class="meta-stack"><strong>${escapeHTML(model)}</strong><span>${escapeHTML(formatted)} · ${Number(key.durationMs || 0).toLocaleString("zh-CN")} ms</span></span>`;
  }

  function formatUSD(value) {
    return `$${Number(value || 0).toFixed(2)}`;
  }

  function formatQuotaReset(value) {
    if (!value) return "未知";
    const date = new Date(value);
    return Number.isNaN(date.valueOf())
      ? escapeHTML(value)
      : escapeHTML(date.toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      }));
  }

  function formatQuotaRemaining(value, now = Date.now()) {
    if (!value) return "剩余时间未知";
    const reset = new Date(value).valueOf();
    if (Number.isNaN(reset)) return "剩余时间未知";
    const milliseconds = reset - now;
    if (milliseconds <= 0) return "即将重置";
    const totalMinutes = Math.max(1, Math.ceil(milliseconds / 60_000));
    const days = Math.floor(totalMinutes / (24 * 60));
    const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
    const minutes = totalMinutes % 60;
    if (days > 0) return `${days} 天 ${hours} 小时`;
    if (hours > 0) return `${hours} 小时 ${minutes} 分`;
    return `${minutes} 分钟`;
  }

  function quotaMeterHTML(label, meter) {
    const percent = Number(meter.percent || 0);
    const level = percent >= 90 ? "danger" : percent >= 70 ? "warning" : "";
    const remaining = formatQuotaRemaining(meter.resetsAt);
    const resetText = remaining === "剩余时间未知" || remaining === "即将重置"
      ? remaining
      : `剩余 ${remaining}`;
    return `<div class="quota-meter">
      <div class="quota-meter-head">
        <span>${label}</span>
        <strong>${percent.toFixed(2)}%</strong>
      </div>
      <progress class="quota-progress ${level}" max="100" value="${Math.min(percent, 100)}"></progress>
      <div class="quota-meter-foot">
        <span>${formatUSD(meter.usedUsd)} / ${formatUSD(meter.limitUsd)}</span>
        <span>${meter.limited ? "已触顶 · " : ""}${resetText}</span>
      </div>
      <small class="quota-reset-at">重置于 ${formatQuotaReset(meter.resetsAt)}</small>
    </div>`;
  }

  function renderQuotaTooltip(key) {
    if (key.quota) {
      const warning = key.quotaError
        ? `<p class="quota-warning">${escapeHTML(key.quotaError)}（当前显示上次成功结果）</p>`
        : "";
      return `<div class="quota-tooltip-title">
        <strong>OpenCode Go 额度</strong>
        <span>${escapeHTML(key.quota.fetchedAt ? new Date(key.quota.fetchedAt).toLocaleString("zh-CN") : key.quotaFetchedAt || "刚刚")}</span>
      </div>
      ${quotaMeterHTML("5 小时", key.quota.rolling)}
      ${quotaMeterHTML("本周", key.quota.weekly)}
      ${quotaMeterHTML("本月", key.quota.monthly)}
      ${warning}`;
    }
    if (key.quotaError) {
      return `<div class="quota-tooltip-title"><strong>OpenCode Go 额度</strong></div>
        <p class="quota-warning">${escapeHTML(key.quotaError)}</p>
        <p class="quota-hint">点击“检测”可再次尝试获取。</p>`;
    }
    return `<div class="quota-tooltip-title"><strong>OpenCode Go 额度</strong></div>
      <p class="quota-hint">尚未获取。点击对应 Key 的“检测”后会自动读取 5 小时、周、月额度。</p>`;
  }

  function positionQuotaTooltip(trigger) {
    const tooltip = $("keyQuotaTooltip");
    const rect = trigger.getBoundingClientRect();
    const tooltipRect = tooltip.getBoundingClientRect();
    let left = Math.max(10, Math.min(rect.left, window.innerWidth - tooltipRect.width - 10));
    let top = rect.bottom + 9;
    if (top + tooltipRect.height > window.innerHeight - 10) {
      top = Math.max(10, rect.top - tooltipRect.height - 9);
    }
    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
  }

  function showQuotaTooltip(trigger) {
    const found = keyByID(trigger.dataset.quotaKeyId);
    if (!found) return;
    const tooltip = $("keyQuotaTooltip");
    state.quotaTooltipKeyID = found.key.id;
    tooltip.innerHTML = renderQuotaTooltip(found.key);
    tooltip.hidden = false;
    positionQuotaTooltip(trigger);
    window.clearInterval(state.quotaTooltipTimer);
    state.quotaTooltipTimer = window.setInterval(() => {
      if (!state.quotaTooltipKeyID) return;
      const current = keyByID(state.quotaTooltipKeyID);
      const currentTrigger = document.querySelector(
        `.quota-trigger[data-quota-key-id="${CSS.escape(state.quotaTooltipKeyID)}"]`,
      );
      if (!current || !currentTrigger || tooltip.hidden) {
        hideQuotaTooltip();
        return;
      }
      tooltip.innerHTML = renderQuotaTooltip(current.key);
      positionQuotaTooltip(currentTrigger);
    }, 60_000);
  }

  function hideQuotaTooltip() {
    window.clearInterval(state.quotaTooltipTimer);
    state.quotaTooltipTimer = 0;
    state.quotaTooltipKeyID = "";
    $("keyQuotaTooltip").hidden = true;
  }

  function renderKeyRow(provider, key) {
    const disabled = key.enabled ? "" : " is-disabled";
    const quotaTrigger = provider.presetId === "opencode-go" || key.quota || key.quotaError
      ? ` class="key-alias quota-trigger" tabindex="0" data-quota-key-id="${escapeAttr(key.id)}"`
      : ' class="key-alias"';
    return `<tr class="${disabled}">
      <td data-label="KEY">
        <span${quotaTrigger}>${escapeHTML(key.alias)}</span>
        ${key.note ? `<span class="key-note">${escapeHTML(key.note)}</span>` : ""}
      </td>
      <td data-label="脱敏密钥"><code class="masked-key">${escapeHTML(key.maskedKey)}</code></td>
      <td data-label="检测状态">${renderStatus(key)}</td>
      <td data-label="最近检测">${renderDetectionMeta(key)}</td>
      <td data-label="指定模型">${escapeHTML(key.testModel || "未指定")}</td>
      <td data-label="预计到期">${renderExpiry(key)}</td>
      <td data-label="操作">
        <div class="row-actions">
          <button class="tiny-button" type="button" data-action="test-key" data-key-id="${escapeAttr(key.id)}" ${state.pending.has(`test:${key.id}`) ? "disabled" : ""}>${state.pending.has(`test:${key.id}`) ? "检测中…" : "检测"}</button>
          <button class="tiny-button" type="button" data-action="reveal-key" data-key-id="${escapeAttr(key.id)}">显示</button>
          <button class="tiny-button" type="button" data-action="edit-key" data-key-id="${escapeAttr(key.id)}">编辑</button>
          <button class="tiny-button" type="button" data-action="toggle-key" data-key-id="${escapeAttr(key.id)}">${key.enabled ? "停用" : "启用"}</button>
          <button class="tiny-button" type="button" data-action="key-history" data-key-id="${escapeAttr(key.id)}">历史</button>
          <button class="tiny-button danger" type="button" data-action="delete-key" data-key-id="${escapeAttr(key.id)}">删除</button>
        </div>
      </td>
    </tr>`;
  }

  function renderProvider(provider, keys) {
    const visibleModels = provider.models.slice(0, 6);
    const hiddenModelCount = Math.max(0, provider.models.length - visibleModels.length);
    const models = provider.models.length
      ? visibleModels.map((model) => `<span class="model-pill-group">
          <button class="model-pill ${model.defaultTest ? "default" : ""}" type="button" data-action="edit-model" data-model-id="${escapeAttr(model.id)}" title="${escapeAttr(modelDisplayName(model))}">${escapeHTML(modelDisplayName(model))}${model.defaultTest ? " · default" : ""}</button>
          <button class="model-delete" type="button" data-action="delete-model" data-model-id="${escapeAttr(model.id)}" title="删除模型配置" aria-label="删除模型 ${escapeAttr(model.modelId)}">×</button>
        </span>`).join("") +
        (hiddenModelCount
          ? `<button class="model-pill model-more" type="button" data-action="show-models" data-provider-id="${escapeAttr(provider.id)}">＋${hiddenModelCount} 个模型</button>`
          : "")
      : '<span class="model-strip-label">NO MODEL PROFILES</span>';
    const visibleKeys = keys.length
      ? `<div class="table-wrap"><table class="key-table">
          <thead><tr><th>KEY</th><th>脱敏密钥</th><th>检测状态</th><th>最近检测</th><th>指定模型</th><th>预计到期</th><th>操作</th></tr></thead>
          <tbody>${keys.map((key) => renderKeyRow(provider, key)).join("")}</tbody>
        </table></div>`
      : '<div class="empty-state"><div class="empty-glyph">＋</div><h2>这个 Provider 还没有 Key</h2><p>添加 Key 后可指定测试模型、到期提醒和备注。</p><button class="primary-button" type="button" data-action="add-key" data-provider-id="' + escapeAttr(provider.id) + '">新增第一把 Key</button></div>';
    return `<article class="provider-card ${provider.enabled ? "" : "is-disabled"}" data-provider-id="${escapeAttr(provider.id)}">
      <header class="provider-head">
        <div class="provider-identity">
          <div class="provider-kicker">
            <span>${escapeHTML(provider.category || "自定义")}</span>
            <span>/</span>
            <span>${escapeHTML(protocolLabel(provider.protocol))}</span>
            ${provider.enabled ? '<span class="mini-badge neutral">启用</span>' : '<span class="mini-badge danger">停用</span>'}
          </div>
          <h2>${escapeHTML(provider.name)}</h2>
          <div class="provider-meta">
            <code>${escapeHTML(provider.baseUrl)}</code>
            <span>${provider.keyCount} 个 Key</span>
            ${provider.note ? `<span>${escapeHTML(provider.note)}</span>` : ""}
          </div>
        </div>
        <div class="provider-actions">
          <button class="tiny-button" type="button" data-action="test-provider" data-provider-id="${escapeAttr(provider.id)}">检测启用 Key</button>
          <button class="tiny-button" type="button" data-action="add-key" data-provider-id="${escapeAttr(provider.id)}">＋ Key</button>
          <button class="tiny-button" type="button" data-action="add-model" data-provider-id="${escapeAttr(provider.id)}">＋ 模型</button>
          <button class="tiny-button" type="button" data-action="discover-models" data-provider-id="${escapeAttr(provider.id)}">同步模型</button>
          <button class="tiny-button" type="button" data-action="edit-provider" data-provider-id="${escapeAttr(provider.id)}">配置</button>
          <button class="tiny-button danger" type="button" data-action="delete-provider" data-provider-id="${escapeAttr(provider.id)}">删除</button>
        </div>
      </header>
      ${visibleKeys}
      <footer class="provider-footer">
        <div class="model-strip"><span class="model-strip-label">MODEL PROFILES · ${provider.models.length}</span>${models}</div>
      </footer>
    </article>`;
  }

  function renderProviders() {
    hideQuotaTooltip();
    renderStats();
    renderProviderFilter();
    const providers = filteredProviders();
    if (!state.providers.length) {
      providerList.innerHTML = `<div class="empty-state">
        <div class="empty-glyph">K</div>
        <h2>密钥柜还是空的</h2>
        <p>从一个明确的 Provider 入口开始：先配置地址与协议，再添加模型和 Key。官方、国际站、Coding Plan 与中转站可以分别建独立配置。</p>
        <button class="primary-button" type="button" data-action="add-provider">新增 Provider</button>
      </div>`;
      return;
    }
    if (!providers.length) {
      providerList.innerHTML = `<div class="empty-state">
        <div class="empty-glyph">⌕</div>
        <h2>没有匹配结果</h2>
        <p>调整搜索关键词、状态或到期筛选后再试。</p>
        <button class="secondary-button" type="button" data-action="clear-filters">清除筛选</button>
      </div>`;
      return;
    }
    providerList.innerHTML = providers.map(({ provider, keys }) => renderProvider(provider, keys)).join("");
  }

  async function loadBootstrap() {
    setBusy(true);
    providerList.innerHTML = '<div class="skeleton-card"></div><div class="skeleton-card"></div>';
    try {
      const payload = await api("bootstrap");
      state.providers = Array.isArray(payload.providers) ? payload.providers : [];
      state.options = payload.options || state.options;
      state.statusLabels = payload.statusLabels || {};
      populateStaticOptions();
      renderProviders();
    } finally {
      setBusy(false);
    }
  }

  function populateStaticOptions() {
    $("providerPreset").innerHTML = state.options.presets
      .map((preset) => `<option value="${escapeAttr(preset.id)}">${escapeHTML(preset.category)} · ${escapeHTML(preset.name)}</option>`)
      .join("");
    $("providerProtocol").innerHTML = state.options.protocols
      .map((option) => `<option value="${escapeAttr(option.id)}">${escapeHTML(option.label)}</option>`)
      .join("");
    $("providerAuth").innerHTML = state.options.authModes
      .map((option) => `<option value="${escapeAttr(option.id)}">${escapeHTML(option.label)}</option>`)
      .join("");
  }

  function openDialog(dialog) {
    if (typeof dialog.showModal === "function") dialog.showModal();
    else dialog.setAttribute("open", "");
  }

  function setFormError(id, message = "") {
    $(id).textContent = message;
  }

  function applyPresetToProviderForm(presetID, force = false) {
    const preset = presetByID(presetID);
    if (!preset) return;
    if (force || !$("providerName").value.trim()) $("providerName").value = preset.name;
    if (force || !$("providerCategory").value.trim()) $("providerCategory").value = preset.category;
    $("providerProtocol").value = preset.protocol;
    $("providerAuth").value = preset.authMode;
    if (force || !$("providerBaseURL").value.trim()) $("providerBaseURL").value = preset.baseUrl;
  }

  function openProviderDialog(provider = null) {
    state.editProviderID = provider?.id || "";
    setFormError("providerFormError");
    $("providerModalEyebrow").textContent = provider ? "EDIT PROVIDER" : "NEW PROVIDER";
    $("providerModalTitle").textContent = provider ? "编辑 Provider" : "新增 Provider";
    $("providerSubmitButton").textContent = provider ? "保存修改" : "保存 Provider";
    $("providerPreset").value = provider?.presetId || "openai";
    $("providerName").value = provider?.name || "";
    $("providerCategory").value = provider?.category || "";
    $("providerProtocol").value = provider?.protocol || "openai_chat";
    $("providerAuth").value = provider?.authMode || "bearer";
    $("providerBaseURL").value = provider?.baseUrl || "";
    $("providerNote").value = provider?.note || "";
    $("providerEnabled").checked = provider?.enabled ?? true;
    if (!provider) applyPresetToProviderForm("openai", true);
    openDialog($("providerDialog"));
    window.setTimeout(() => $("providerName").focus(), 20);
  }

  async function importProviderModels(providerID) {
    return api(`providers/${encodeURIComponent(providerID)}/discover-models`, {
      method: "POST",
      body: {},
    });
  }

  async function submitProvider(event) {
    event.preventDefault();
    const body = {
      name: $("providerName").value.trim(),
      presetId: $("providerPreset").value,
      category: $("providerCategory").value.trim(),
      protocol: $("providerProtocol").value,
      baseUrl: $("providerBaseURL").value.trim(),
      authMode: $("providerAuth").value,
      note: $("providerNote").value.trim(),
      enabled: $("providerEnabled").checked,
    };
    setFormError("providerFormError");
    setButtonPending("providerSubmitButton", true, "保存中…");
    try {
      let createdProvider = null;
      if (state.editProviderID) {
        await api(`providers/${encodeURIComponent(state.editProviderID)}`, { method: "PUT", body });
        showToast("Provider 配置已更新，旧检测结果已标记为过期");
      } else {
        const response = await api("providers", { method: "POST", body });
        createdProvider = response.provider;
        showToast("Provider 已创建");
      }
      $("providerDialog").close();
      await loadBootstrap();
      const preset = presetByID(body.presetId);
      if (createdProvider && preset?.autoImportModels) {
        try {
          const imported = await importProviderModels(createdProvider.id);
          await loadBootstrap();
          showToast(`已自动导入 ${imported.discovered} 个官方模型`);
        } catch (error) {
          showToast(`Provider 已创建，但官方模型自动导入失败：${errorMessage(error)}`, "error");
        }
      }
    } catch (error) {
      setFormError("providerFormError", errorMessage(error));
    } finally {
      setButtonPending("providerSubmitButton", false);
    }
  }

  function populateKeyModelSelect(provider, selected = "") {
    const options = [];
    for (const model of provider.models) {
      options.push(`<option value="${escapeAttr(model.modelId)}" label="${escapeAttr(modelDisplayName(model))}${model.defaultTest ? " · 默认" : ""}"></option>`);
    }
    $("keyTestModelOptions").innerHTML = options.join("");
    const fallback = provider.models.find((model) => model.defaultTest)?.modelId || "";
    $("keyTestModel").value = selected || fallback;
  }

  function openKeyDialog(provider, key = null) {
    state.editKeyID = key?.id || "";
    state.activeProviderID = provider.id;
    setFormError("keyFormError");
    $("keyModalEyebrow").textContent = key ? "EDIT KEY" : "NEW KEY";
    $("keyModalTitle").textContent = key ? "编辑 Key" : "新增 Key";
    $("keyProviderName").textContent = `${provider.name} · ${protocolLabel(provider.protocol)}`;
    $("keyAlias").value = key?.alias || "";
    populateKeyModelSelect(provider, key?.testModel || "");
    $("keySecret").value = "";
    $("keySecret").required = !key;
    $("keySecret").placeholder = key ? "留空表示不修改现有密钥" : "粘贴 API Key";
    $("keyPurchaseDate").value = key?.purchaseDate || "";
    $("keyExpiresOn").value = key?.expiresOn || "";
    $("keyReminderDays").value = key?.reminderDays ?? 30;
    $("keyEnabled").checked = key?.enabled ?? true;
    $("keyNote").value = key?.note || "";
    $("keySubmitButton").textContent = key ? "保存修改" : "保存 Key";
    openDialog($("keyDialog"));
    window.setTimeout(() => $("keyAlias").focus(), 20);
    if (!provider.models.length) {
      setFormError("keyFormError", "当前 Provider 尚未同步模型。你可以直接输入 Model ID / Endpoint ID，或先保存 Key 后点击“同步模型”。");
    }
  }

  async function submitKey(event) {
    event.preventDefault();
    const provider = providerByID(state.activeProviderID);
    if (!provider) return;
    const body = {
      alias: $("keyAlias").value.trim(),
      enabled: $("keyEnabled").checked,
      testModel: $("keyTestModel").value,
      purchaseDate: $("keyPurchaseDate").value,
      expiresOn: $("keyExpiresOn").value,
      reminderDays: Number($("keyReminderDays").value || 0),
      note: $("keyNote").value.trim(),
    };
    if ($("keySecret").value.trim()) body.secret = $("keySecret").value.trim();
    setFormError("keyFormError");
    setButtonPending("keySubmitButton", true, "保存中…");
    try {
      let createdKey = null;
      if (state.editKeyID) {
        await api(`keys/${encodeURIComponent(state.editKeyID)}`, { method: "PUT", body });
        showToast("Key 已更新；配置变化已使旧检测结果过期");
      } else {
        const response = await api(`providers/${encodeURIComponent(provider.id)}/keys`, { method: "POST", body });
        createdKey = response.key;
        showToast("Key 已加密保存");
      }
      $("keyDialog").close();
      await loadBootstrap();
      const preset = presetByID(provider.presetId);
      if (createdKey && preset?.discoverModels) {
        try {
          const imported = await importProviderModels(provider.id);
          await loadBootstrap();
          showToast(`Key 已保存，并同步 ${imported.discovered} 个可用模型`);
        } catch (error) {
          showToast(`Key 已保存，但模型同步失败：${errorMessage(error)}`, "error");
        }
      }
    } catch (error) {
      setFormError("keyFormError", errorMessage(error));
    } finally {
      setButtonPending("keySubmitButton", false);
    }
  }

  function openModelDialog(provider, model = null, presetModelID = "") {
    state.editModelID = model?.id || "";
    state.modelProviderID = provider.id;
    state.modelSource = model?.source || (presetModelID ? "discovered" : "manual");
    setFormError("modelFormError");
    $("modelModalEyebrow").textContent = model ? "EDIT MODEL" : "MODEL PROFILE";
    $("modelModalTitle").textContent = model ? "编辑模型配置" : "新增模型配置";
    $("modelProviderName").textContent = provider.name;
    $("modelID").value = model?.modelId || presetModelID || "";
    $("modelSource").value = state.modelSource;
    $("modelContext").value = model?.contextWindow ?? "";
    $("modelOutput").value = model?.maxOutput ?? "";
    $("modelThinkingSupport").value = model?.supportsThinking === true ? "true" : model?.supportsThinking === false ? "false" : "";
    $("modelThinkingLevel").value = model?.thinkingLevel || "";
    $("modelThinkingBudget").value = model?.thinkingBudget ?? "";
    const params = model?.testParams;
    $("modelTestParams").value = params && Object.keys(params).length ? JSON.stringify(params, null, 2) : "";
    $("modelDefault").checked = Boolean(model?.defaultTest);
    $("modelSubmitButton").textContent = model ? "保存修改" : "保存模型配置";
    openDialog($("modelDialog"));
    window.setTimeout(() => $("modelID").focus(), 20);
  }

  function nullableNumber(id) {
    const raw = $(id).value.trim();
    if (raw === "") return null;
    const number = Number(raw);
    return Number.isFinite(number) ? number : null;
  }

  async function submitModel(event) {
    event.preventDefault();
    const provider = providerByID(state.modelProviderID);
    if (!provider) return;
    let testParams = {};
    const json = $("modelTestParams").value.trim();
    if (json) {
      try {
        testParams = JSON.parse(json);
      } catch {
        setFormError("modelFormError", "额外测试参数不是有效 JSON");
        return;
      }
      if (!testParams || Array.isArray(testParams) || typeof testParams !== "object") {
        setFormError("modelFormError", "额外测试参数必须是 JSON 对象");
        return;
      }
    }
    const thinkingSupport = $("modelThinkingSupport").value;
    const body = {
      modelId: $("modelID").value.trim(),
      contextWindow: nullableNumber("modelContext"),
      maxOutput: nullableNumber("modelOutput"),
      supportsThinking: thinkingSupport === "" ? null : thinkingSupport === "true",
      thinkingLevel: $("modelThinkingLevel").value.trim(),
      thinkingBudget: nullableNumber("modelThinkingBudget"),
      testParams,
      defaultTest: $("modelDefault").checked,
      source: $("modelSource").value.trim() || "manual",
    };
    setFormError("modelFormError");
    setButtonPending("modelSubmitButton", true, "保存中…");
    try {
      if (state.editModelID) {
        await api(`models/${encodeURIComponent(state.editModelID)}`, { method: "PUT", body });
        showToast("模型配置已更新");
      } else {
        await api(`providers/${encodeURIComponent(provider.id)}/models`, { method: "POST", body });
        showToast("模型配置已保存");
      }
      $("modelDialog").close();
      await loadBootstrap();
    } catch (error) {
      setFormError("modelFormError", errorMessage(error));
    } finally {
      setButtonPending("modelSubmitButton", false);
    }
  }

  async function testOneKey(keyID, button = null) {
    if (state.pending.has(`test:${keyID}`)) return;
    state.pending.add(`test:${keyID}`);
    if (button) setButtonPending(button, true, "检测中…");
    renderProviders();
    try {
      const payload = await api(`keys/${encodeURIComponent(keyID)}/test`, { method: "POST", body: {} });
      const detection = payload.detection;
      showToast(`检测完成：${statusLabel(detection.status)}${detection.detail ? ` · ${detection.detail}` : ""}`, detection.status === "success" ? "success" : "error");
      await refreshProviders();
    } catch (error) {
      showToast(errorMessage(error), "error");
    } finally {
      state.pending.delete(`test:${keyID}`);
      if (button) setButtonPending(button, false);
      renderProviders();
    }
  }

  async function batchTest(providerID = "") {
    const provider = providerID ? providerByID(providerID) : null;
    const keys = provider
      ? provider.keys.filter((key) => key.enabled && key.testModel)
      : state.providers.flatMap((item) => item.keys).filter((key) => key.enabled && key.testModel);
    if (!keys.length) {
      showToast("没有启用且已配置测试模型的 Key", "error");
      return;
    }
    if (keys.length > 64) {
      showToast("一次最多检测 64 个 Key", "error");
      return;
    }
    const scope = provider ? `「${provider.name}」中的` : "全部 Provider 中";
    if (!window.confirm(`将检测${scope} ${keys.length} 个启用 Key。\n\n这会从服务器向厂商发送 ${keys.length} 次极小真实请求，可能产生少量费用，且不会自动重试。继续吗？`)) return;
    const button = provider ? null : $("batchTestButton");
    if (button) setButtonPending(button, true, "检测中…");
    setBusy(true);
    try {
      const endpoint = providerID ? `providers/${encodeURIComponent(providerID)}/test` : "test-enabled";
      const payload = await api(endpoint, { method: "POST", body: { confirm: true } });
      const successCount = payload.detections.filter((item) => item.status === "success").length;
      showToast(`批量检测完成：${payload.count} 个 Key，${successCount} 个调用成功`, successCount === payload.count ? "success" : "error");
      await refreshProviders();
    } catch (error) {
      showToast(errorMessage(error), "error");
    } finally {
      setBusy(false);
      if (button) setButtonPending(button, false);
    }
  }

  async function refreshProviders() {
    const payload = await api("providers");
    state.providers = Array.isArray(payload.providers) ? payload.providers : [];
    renderProviders();
  }

  function clearRevealState() {
    state.activeKeyID = "";
    $("revealPassword").value = "";
    $("revealSecret").value = "";
    $("revealAuthBlock").hidden = false;
    $("revealSecretBlock").hidden = true;
    $("revealSubmitButton").hidden = false;
    setFormError("revealFormError");
  }

  function openRevealDialog(keyID) {
    const found = keyByID(keyID);
    if (!found) return;
    clearRevealState();
    state.activeKeyID = keyID;
    $("revealKeyName").textContent = `${found.provider.name} · ${found.key.alias}`;
    openDialog($("revealDialog"));
    window.setTimeout(() => $("revealPassword").focus(), 30);
  }

  async function submitReveal(event) {
    event.preventDefault();
    setFormError("revealFormError");
    setButtonPending("revealSubmitButton", true, "验证中…");
    try {
      const payload = await api(`keys/${encodeURIComponent(state.activeKeyID)}/reveal`, {
        method: "POST",
        body: { password: $("revealPassword").value },
      });
      $("revealSecret").value = payload.secret;
      $("revealPassword").value = "";
      $("revealAuthBlock").hidden = true;
      $("revealSecretBlock").hidden = false;
      $("revealSubmitButton").hidden = true;
      showToast("重新验证成功；关闭弹窗后会清空完整 Key");
    } catch (error) {
      setFormError("revealFormError", errorMessage(error));
      $("revealPassword").select();
    } finally {
      setButtonPending("revealSubmitButton", false);
    }
  }

  async function copyRevealedSecret() {
    const value = $("revealSecret").value;
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      showToast("完整密钥已复制");
    } catch {
      $("revealSecret").focus();
      $("revealSecret").select();
      document.execCommand("copy");
      showToast("完整密钥已复制");
    }
  }

  async function toggleKey(keyID) {
    const found = keyByID(keyID);
    if (!found) return;
    const { provider, key } = found;
    const verb = key.enabled ? "停用" : "启用";
    if (key.enabled && !window.confirm(`停用「${key.alias}」？\n\n停用只影响本站记录和批量检测，不会撤销厂商密钥。`)) return;
    const body = {
      alias: key.alias,
      enabled: !key.enabled,
      testModel: key.testModel,
      purchaseDate: key.purchaseDate || "",
      expiresOn: key.expiresOn || "",
      reminderDays: key.reminderDays || 0,
      note: key.note || "",
    };
    try {
      await api(`keys/${encodeURIComponent(key.id)}`, { method: "PUT", body });
      showToast(`Key 已${verb}`);
      await loadBootstrap();
    } catch (error) {
      showToast(errorMessage(error), "error");
    }
  }

  async function deleteKey(keyID) {
    const found = keyByID(keyID);
    if (!found) return;
    if (!window.confirm(`删除「${found.key.alias}」？\n\n只删除本站记录，不会撤销厂商密钥。该 Key 的检测历史也会一并删除，且无法恢复。`)) return;
    try {
      await api(`keys/${encodeURIComponent(keyID)}`, { method: "DELETE", body: {} });
      showToast("Key 记录已删除");
      await loadBootstrap();
    } catch (error) {
      showToast(errorMessage(error), "error");
    }
  }

  async function deleteProvider(providerID) {
    const provider = providerByID(providerID);
    if (!provider) return;
    const modelCount = provider.models.length;
    if (!window.confirm(`删除 Provider「${provider.name}」？\n\n将同时删除 ${provider.keyCount} 个 Key、${modelCount} 个模型配置及其检测历史。厂商密钥不会被撤销。此操作无法恢复。`)) return;
    try {
      await api(`providers/${encodeURIComponent(providerID)}`, { method: "DELETE", body: {} });
      showToast("Provider 及其本站记录已删除");
      await loadBootstrap();
    } catch (error) {
      showToast(errorMessage(error), "error");
    }
  }

  async function deleteModel(modelID) {
    const provider = state.providers.find((item) => item.models.some((model) => model.id === modelID));
    const model = provider?.models.find((item) => item.id === modelID);
    if (!provider || !model) return;
    if (!window.confirm(`删除模型配置「${model.modelId}」？\n\n如果它仍被 Key 用作测试模型，服务端会拒绝删除。`)) return;
    try {
      await api(`models/${encodeURIComponent(modelID)}`, { method: "DELETE", body: {} });
      showToast("模型配置已删除");
      await loadBootstrap();
    } catch (error) {
      showToast(errorMessage(error), "error");
    }
  }

  function showModelCatalog(provider, models, summary) {
    $("discoveryProviderName").textContent = provider.name;
    $("discoverySummary").textContent = summary;
    $("discoveredModels").innerHTML = models.length
      ? models.map(renderModelCatalogItem).join("")
      : '<p class="sensitive-copy">当前没有模型配置。</p>';
    if (!$("discoveryDialog").open) openDialog($("discoveryDialog"));
  }

  async function openDiscovery(providerID) {
    const provider = providerByID(providerID);
    if (!provider) return;
    const preset = presetByID(provider.presetId);
    if (preset && !preset.discoverModels) {
      showToast("该 Provider 预设没有可靠的模型目录接口，请手动添加", "error");
      return;
    }
    $("discoveryProviderName").textContent = provider.name;
    $("discoverySummary").textContent = "正在读取可用模型并合并官方参数…";
    $("discoveredModels").innerHTML = '<div class="skeleton-card short"></div>';
    setFormError("discoveryError");
    openDialog($("discoveryDialog"));
    try {
      const payload = await importProviderModels(provider.id);
      const source = payload.metadataSource === "models.dev" ? "Models.dev 官方目录" : "Provider 模型接口";
      showModelCatalog(
        provider,
        payload.models || [],
        `已同步 ${payload.discovered} 个模型：新建 ${payload.created}，补充/更新 ${payload.updated}。参数来源：${source}。`,
      );
      await loadBootstrap();
    } catch (error) {
      $("discoverySummary").textContent = "";
      $("discoveredModels").innerHTML = "";
      setFormError("discoveryError", errorMessage(error));
    }
  }

  function showProviderModels(providerID) {
    const provider = providerByID(providerID);
    if (!provider) return;
    const metadataCount = provider.models.filter((model) => model.metadata && Object.keys(modelMetadata(model)).length).length;
    showModelCatalog(
      provider,
      provider.models,
      `共 ${provider.models.length} 个模型，其中 ${metadataCount} 个带官方参数。点击“同步模型”可刷新目录。`,
    );
    setFormError("discoveryError");
  }

  function renderExportTree() {
    const tree = $("exportTree");
    if (!state.providers.length) {
      tree.innerHTML = '<div class="export-tree-empty">暂无可导出的 Provider。</div>';
      updateExportSelectionCount();
      return;
    }
    tree.innerHTML = state.providers.map((provider) => {
      const keys = provider.keys.map((key) => `<label class="export-key">
        <input type="checkbox" data-export-key="${escapeAttr(key.id)}" data-provider-id="${escapeAttr(provider.id)}">
        <span>${escapeHTML(key.alias)}${key.enabled ? "" : "（停用）"}</span>
        <code>${escapeHTML(key.maskedKey)}</code>
      </label>`).join("");
      return `<section class="export-provider" data-provider-id="${escapeAttr(provider.id)}">
        <label class="export-provider-head">
          <input type="checkbox" data-export-provider="${escapeAttr(provider.id)}" ${provider.keys.length ? "" : "disabled"}>
          <span>${escapeHTML(provider.name)}</span>
          <small>${provider.keys.length} Keys${provider.enabled ? "" : " · 停用"}</small>
        </label>
        ${keys ? `<div class="export-keys">${keys}</div>` : '<div class="export-tree-empty">这个 Provider 没有 Key。</div>'}
      </section>`;
    }).join("");
    updateExportSelectionCount();
  }

  function selectedExportKeyIDs() {
    return [...document.querySelectorAll("#exportTree input[data-export-key]:checked")]
      .map((input) => input.dataset.exportKey);
  }

  function updateExportSelectionCount() {
    $("exportSelectionCount").textContent = `已选择 ${selectedExportKeyIDs().length} 个 Key`;
  }

  function syncExportProviderCheckbox(providerID) {
    const providerCheckbox = document.querySelector(`#exportTree input[data-export-provider="${CSS.escape(providerID)}"]`);
    if (!providerCheckbox) return;
    const keyCheckboxes = [...document.querySelectorAll(`#exportTree input[data-provider-id="${CSS.escape(providerID)}"][data-export-key]`)];
    const checked = keyCheckboxes.filter((input) => input.checked).length;
    providerCheckbox.checked = keyCheckboxes.length > 0 && checked === keyCheckboxes.length;
    providerCheckbox.indeterminate = checked > 0 && checked < keyCheckboxes.length;
  }

  function clearExportState() {
    $("exportText").value = "";
    $("exportPassword").value = "";
    $("exportOutputBlock").hidden = true;
    $("copyExportButton").hidden = true;
    $("exportSubmitButton").hidden = false;
    setFormError("exportFormError");
  }

  function openExportDialog() {
    clearExportState();
    renderExportTree();
    openDialog($("exportDialog"));
  }

  async function submitExport(event) {
    event.preventDefault();
    const ids = selectedExportKeyIDs();
    if (!ids.length) {
      setFormError("exportFormError", "至少选择一个 Key");
      return;
    }
    setFormError("exportFormError");
    setButtonPending("exportSubmitButton", true, "验证并生成中…");
    try {
      const payload = await api("export", {
        method: "POST",
        body: {
          password: $("exportPassword").value,
          keyIds: ids,
        },
      });
      $("exportPassword").value = "";
      $("exportText").value = payload.text || "";
      $("exportOutputBlock").hidden = false;
      $("copyExportButton").hidden = false;
      $("exportSubmitButton").hidden = true;
      $("exportText").focus();
      $("exportText").select();
      showToast(`已生成 ${payload.count} 个 Key 的配置文本`);
    } catch (error) {
      setFormError("exportFormError", errorMessage(error));
      $("exportPassword").select();
    } finally {
      setButtonPending("exportSubmitButton", false);
    }
  }

  async function copyExportText() {
    const text = $("exportText").value;
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      $("exportText").focus();
      $("exportText").select();
      document.execCommand("copy");
    }
    showToast("导出文本已复制");
  }

  async function openHistory(keyID) {
    const found = keyByID(keyID);
    if (!found) return;
    $("historyKeyName").textContent = `${found.provider.name} · ${found.key.alias}`;
    $("historyContent").innerHTML = '<div class="skeleton-card medium"></div>';
    openDialog($("historyDialog"));
    try {
      const payload = await api(`keys/${encodeURIComponent(keyID)}/detections?limit=50`);
      const rows = Array.isArray(payload.detections) ? payload.detections : [];
      $("historyContent").innerHTML = rows.length
        ? `<table class="history-table"><thead><tr><th>时间</th><th>状态</th><th>模型</th><th>HTTP</th><th>耗时</th><th>摘要</th></tr></thead><tbody>${rows.map((item) => {
            const date = new Date(item.startedAt);
            const formatted = Number.isNaN(date.valueOf()) ? item.startedAt : date.toLocaleString("zh-CN");
            return `<tr>
              <td>${escapeHTML(formatted)}</td>
              <td><span class="status-badge status-${escapeAttr(item.status)}">${escapeHTML(statusLabel(item.status))}</span>${item.stale ? ' <span class="mini-badge">结果已过期</span>' : ""}</td>
              <td><span class="mono">${escapeHTML(item.modelId)}</span></td>
              <td>${item.httpStatus || "—"}</td>
              <td>${Number(item.durationMs || 0).toLocaleString("zh-CN")} ms</td>
              <td>${escapeHTML(item.detail || "—")}</td>
            </tr>`;
          }).join("")}</tbody></table>`
        : '<p class="sensitive-copy">暂无检测历史。</p>';
    } catch (error) {
      $("historyContent").innerHTML = `<p class="form-error">${escapeHTML(errorMessage(error))}</p>`;
    }
  }

  function handleProviderAction(action, element) {
    const providerID = element.dataset.providerId;
    const provider = providerByID(providerID);
    if (!provider) return false;
    switch (action) {
      case "add-key": openKeyDialog(provider); return true;
      case "edit-provider": openProviderDialog(provider); return true;
      case "delete-provider": deleteProvider(provider.id); return true;
      case "add-model": openModelDialog(provider); return true;
      case "test-provider": batchTest(provider.id); return true;
      case "discover-models": openDiscovery(provider.id); return true;
      case "show-models": showProviderModels(provider.id); return true;
      default: return false;
    }
  }

  async function handleAction(element) {
    const action = element.dataset.action;
    if (action === "add-provider") {
      openProviderDialog();
      return;
    }
    if (action === "clear-filters") {
      clearFilters();
      return;
    }
    if (handleProviderAction(action, element)) return;
    const keyID = element.dataset.keyId;
    const modelID = element.dataset.modelId;
    switch (action) {
      case "test-key": await testOneKey(keyID, element); break;
      case "reveal-key": openRevealDialog(keyID); break;
      case "edit-key": {
        const found = keyByID(keyID);
        if (found) openKeyDialog(found.provider, found.key);
        break;
      }
      case "toggle-key": await toggleKey(keyID); break;
      case "delete-key": await deleteKey(keyID); break;
      case "key-history": await openHistory(keyID); break;
      case "edit-model": {
        const provider = state.providers.find((item) => item.models.some((model) => model.id === modelID));
        const model = provider?.models.find((item) => item.id === modelID);
        if (provider && model) openModelDialog(provider, model);
        break;
      }
      case "delete-model": await deleteModel(modelID); break;
    }
  }

  function clearFilters() {
    state.filters = { search: "", provider: "", status: "", expiry: false };
    $("searchInput").value = "";
    $("providerFilter").value = "";
    $("statusFilter").value = "";
    $("expiryFilter").checked = false;
    renderProviders();
  }

  function bindEvents() {
    $("loginForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      $("loginError").textContent = "";
      setButtonPending("loginButton", true, "验证中…");
      try {
        const response = await fetch("/keys/api/session", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          body: JSON.stringify({ password: $("loginPassword").value }),
        });
        const payload = await response.json().catch(() => null);
        if (!response.ok) {
          throw new ApiError(payload?.error?.message || "登录失败", response.status, payload?.error?.code);
        }
        state.csrfToken = payload.csrfToken;
        $("loginPassword").value = "";
        showApp();
        await loadBootstrap();
      } catch (error) {
        $("loginError").textContent = errorMessage(error);
        $("loginPassword").select();
      } finally {
        setButtonPending("loginButton", false);
      }
    });

    $("togglePassword").addEventListener("click", () => {
      const input = $("loginPassword");
      const show = input.type === "password";
      input.type = show ? "text" : "password";
      $("togglePassword").textContent = show ? "隐藏" : "显示";
      input.focus();
    });

    $("logoutButton").addEventListener("click", async () => {
      try {
        await api("logout", { method: "POST", body: {} });
      } catch {
        // A stale session still needs a clean client-side exit.
      }
      showLogin();
      showToast("已退出管理台");
    });

    $("addProviderButton").addEventListener("click", () => openProviderDialog());
    $("batchTestButton").addEventListener("click", () => batchTest());
    $("exportButton").addEventListener("click", openExportDialog);
    $("exportForm").addEventListener("submit", submitExport);
    $("copyExportButton").addEventListener("click", copyExportText);
    $("exportSelectAllButton").addEventListener("click", () => {
      document.querySelectorAll("#exportTree input[data-export-key]").forEach((input) => {
        input.checked = true;
      });
      document.querySelectorAll("#exportTree input[data-export-provider]").forEach((input) => {
        if (!input.disabled) {
          input.checked = true;
          input.indeterminate = false;
        }
      });
      updateExportSelectionCount();
    });
    $("exportClearButton").addEventListener("click", () => {
      document.querySelectorAll("#exportTree input[type=checkbox]").forEach((input) => {
        input.checked = false;
        input.indeterminate = false;
      });
      clearExportState();
      updateExportSelectionCount();
    });
    $("exportTree").addEventListener("change", (event) => {
      const providerCheckbox = event.target.closest("input[data-export-provider]");
      if (providerCheckbox) {
        const providerID = providerCheckbox.dataset.exportProvider;
        providerCheckbox.indeterminate = false;
        document.querySelectorAll(`#exportTree input[data-provider-id="${CSS.escape(providerID)}"][data-export-key]`)
          .forEach((input) => { input.checked = providerCheckbox.checked; });
      }
      const keyCheckbox = event.target.closest("input[data-export-key]");
      if (keyCheckbox) syncExportProviderCheckbox(keyCheckbox.dataset.providerId);
      updateExportSelectionCount();
      clearExportState();
    });
    providerList.addEventListener("click", (event) => {
      const button = event.target.closest("button[data-action]");
      if (button) handleAction(button);
    });
    providerList.addEventListener("mouseover", (event) => {
      const trigger = event.target.closest(".quota-trigger");
      if (trigger && !trigger.contains(event.relatedTarget)) showQuotaTooltip(trigger);
    });
    providerList.addEventListener("mouseout", (event) => {
      const trigger = event.target.closest(".quota-trigger");
      if (trigger && !trigger.contains(event.relatedTarget)) hideQuotaTooltip();
    });
    providerList.addEventListener("focusin", (event) => {
      const trigger = event.target.closest(".quota-trigger");
      if (trigger) showQuotaTooltip(trigger);
    });
    providerList.addEventListener("focusout", (event) => {
      const trigger = event.target.closest(".quota-trigger");
      if (trigger) hideQuotaTooltip();
    });
    window.addEventListener("scroll", hideQuotaTooltip, true);
    window.addEventListener("resize", hideQuotaTooltip);
    document.addEventListener("click", (event) => {
      const button = event.target.closest("[data-action]");
      if (!button || providerList.contains(button)) return;
      handleAction(button);
    });

    document.querySelectorAll(".close-dialog").forEach((button) => {
      button.addEventListener("click", () => {
        const dialog = button.closest("dialog");
        if (dialog?.id === "revealDialog") clearRevealState();
        if (dialog?.id === "exportDialog") clearExportState();
        dialog?.close();
      });
    });
    document.querySelectorAll("dialog").forEach((dialog) => {
      dialog.addEventListener("close", () => {
        if (dialog.id === "revealDialog") clearRevealState();
        if (dialog.id === "exportDialog") clearExportState();
      });
    });

    $("providerPreset").addEventListener("change", () => applyPresetToProviderForm($("providerPreset").value, true));
    $("providerForm").addEventListener("submit", submitProvider);
    $("keyForm").addEventListener("submit", submitKey);
    $("modelForm").addEventListener("submit", submitModel);
    $("revealForm").addEventListener("submit", submitReveal);
    $("copySecretButton").addEventListener("click", copyRevealedSecret);

    let searchTimer = 0;
    $("searchInput").addEventListener("input", () => {
      window.clearTimeout(searchTimer);
      searchTimer = window.setTimeout(() => {
        state.filters.search = $("searchInput").value;
        renderProviders();
      }, 120);
    });
    $("providerFilter").addEventListener("change", () => {
      state.filters.provider = $("providerFilter").value;
      renderProviders();
    });
    $("statusFilter").addEventListener("change", () => {
      state.filters.status = $("statusFilter").value;
      renderProviders();
    });
    $("expiryFilter").addEventListener("change", () => {
      state.filters.expiry = $("expiryFilter").checked;
      renderProviders();
    });
    $("clearFiltersButton").addEventListener("click", clearFilters);

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeAllDialogs();
    });
  }

  async function bootstrapSession() {
    try {
      const response = await fetch("/keys/api/session", {
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      });
      if (!response.ok) {
        showLogin();
        return;
      }
      const payload = await response.json();
      if (!payload?.authenticated) {
        showLogin();
        return;
      }
      state.csrfToken = payload.csrfToken;
      showApp();
      await loadBootstrap();
    } catch {
      showLogin("无法连接密钥服务");
    }
  }

  bindEvents();
  bootstrapSession();
})();
