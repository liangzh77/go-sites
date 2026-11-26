# math-review.html 设计规范（静态展示页）

## 1. 技术约束
- 纯静态：`math-review.html` + `math/math.css` + 页面内联 `<script>`（约 60 行），**不引入** `app.js`/`essay.js`，无 fetch、无模型 API、无登录。
- 复用 essay/essay.css 中的 token：背景 `#fcfbf8`、文字 `#2b2622`、主橙 `#b84e10`、圆角 14px、系统中文字体栈；math.css 只补充增量样式。

## 2. 信息层级与布局

**页面结构（自上而下）**
1. 顶部导航（复用现有 header）：品牌 `School AI Demo`；导航四项：`全部 Demo / 教案 / 作文 / 数学`，当前项加 `aria-current="page"` + 橙色下划线。
2. 页面头区：H1「数学判卷 · 静态示例」；副标题一行说明：**「本页展示预置的 AI 判卷输出，非本次实时生成，不支持上传或提交」**（14px，次要色 `#6b6157`）。
3. 警示条（全局一次，非每例重复）：浅橙底 `#fdf1e7`、1px 边框 `#f0d3bd`、圆角 14px、内边距 12×16：
   > AI 判定与分值仅作演示，可能存在误判；实际判卷须由老师核对题目与评分标准，请勿将本页输出视为正确答案。
4. 两个示例块 `<section class="math-case">` 纵向排列，间距 48px。每例内部：
   - 标题行：H2（如「示例一 · 判卷结果（满分 30 分，每题 5 分）」，原文保留）+ 右侧提示词复制按钮。
   - 桌面（≥900px）左右分栏：`grid-template-columns: minmax(0,5fr) minmax(0,7fr); gap:24px`；左图右表。图片列宽不超过 420px，`align-items:start`。
   - 移动端单列上下：图在前、表在后。

**原图卡片**
- `<figure>` 白底卡片，padding 12px，圆角 14px，轻阴影 `0 2px 10px rgba(43,38,34,.06)`。
- 示例一图 500×794，示例二 1080×1580：`<img>` 设 `width/height` 属性（防 CLS），CSS `max-width:100%; height:auto; object-fit:contain`——**保持比例、不裁切**；示例二为长图，卡片内 `max-height:70vh` + 纵向滚动不可行（会破坏灯箱预期），改为直接等比缩放显示全图。
- 图片包在 `<button class="zoom-btn">` 内，右下角浮层图标「🔍 点击放大」；figcaption 注明尺寸与「预置原图」。

**判卷表格**
- 原文内容逐字保留（含长算式、解释、原有标题句），不补总分、不重新评判。
- 表格容器 `.table-scroll`：`overflow-x:auto; max-width:100%`，滚动时右侧渐变阴影提示可滚；容器上方 12px 小字「表格较宽，可左右滑动 →」（仅当 `scrollWidth > clientWidth` 时 JS 显示）。
- 样式：`border-collapse:collapse`，表头浅橙底 `#fdf1e7`、字重 600；单元格 padding 10×14，行间 1px `#ece5db` 分隔；判定列用徽章：✓ 正确（绿 `#2e7d32` 文字 + `#e8f3e9` 底）、✗ 错误（红 `#b3261e` + `#fbeae8`）、△ 半对（橙 `#b84e10` + `#fdf1e7`）；得分右对齐等宽数字。
- 页面 `body{overflow-x:hidden}` 兜底，杜绝横向溢出。

**复制区（每例）**
- 两个按钮（次要描边样式）：「复制提示词」「复制表格（Markdown）」。
- 提示词原文两例共享：`作为初一数学老师，判卷`（按钮旁以 `<code>` 展示原文）。
- Markdown 表格内容由 JS 从 DOM 表格序列化生成（`| 题 | 判定 | 得分 | 批注 |` 等，表头按各例实际列数），保证与页面一致、无需手维护两份。

## 3. 交互与无障碍

**复制按钮状态机**
- 默认 → 点击 `navigator.clipboard.writeText`（失败回退 `execCommand`）→ 成功：按钮文字变「✓ 已复制」、背景转浅绿，2s 恢复；失败：变「复制失败，请手动选择」。
- 每个按钮旁有 `aria-live="polite"` 的 visually-hidden 状态区，朗读「提示词已复制」。
- `focus-visible`：2px 橙色 outline + 2px offset；全程可 Tab 到达，Enter/Space 触发。

**灯箱（查看原图）**
- 结构：半透明遮罩 `rgba(30,25,20,.72)` + 居中 `<img>`（`max-width:92vw; max-height:88vh; object-fit:contain`）+ 右上「× 关闭」按钮 + 底部「在新标签页打开原图」链接（`<a href="原图路径" target="_blank" rel="noopener">`）。
- 打开时：记录触发按钮，`role="dialog" aria-modal="true" aria-label="查看原图"`，焦点移至关闭按钮，Tab 焦点圈禁在灯箱内，禁止背景滚动（`body{overflow:hidden}`）。
- 关闭途径：Esc、关闭按钮、点击遮罩（点图片本身不关闭）；关闭后焦点还回原按钮，`aria-expanded` 同步。

**键盘顺序**：导航 → 提示词复制 → 表格 Markdown 复制 → 图片放大按钮 → 下一例。

## 4. 移动端规则（<900px）
- 导航折叠为可横滑一排（不引入汉堡 JS，纯 `overflow-x:auto`）。
- 每例上下布局，间距 32px；图片宽度 100%（上限 420px 居中）。
- 表格容器横滚 + 滑动提示常显；字号不低于 14px。
- 灯箱图片 `max-width:96vw`；按钮最小触控 44×44px。
- 警示条允许折行，不用固定高度。

## 5. 首页改动（index）
- 作文分类下/并列新增「数学判卷」卡片：标题 + 一句描述「预置的数学判卷展示（静态示例，不支持提交与实时判卷）」+ 标签 `静态示例`，链接 `math-review.html`，沿用现有卡片组件与橙色 hover 态，不新增分类逻辑之外的代码。

**验收要点**：两例原文逐字一致；无网络请求（除两张本地图）；Esc/遮罩/按钮三态关闭可用；窄屏无横向滚动条；复制后剪贴板为合法 Markdown 表格。
