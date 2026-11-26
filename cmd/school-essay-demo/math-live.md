# 数学实时判卷（2026-11-24 已上线，可自定义提示词）

线上已支持上传/粘贴/拖入试卷、DeepSeek 逐题分析及结构化结果表格；两组预置示例和原有功能保持不变。

直达：<https://liangz77.cn/school-ai-demo/math-review.html>；首页已有入口，`/private` 沿用原 School AI Demo 链接。

## 接口与返回

沿用 `school-essay-demo` companion 服务、9026 端口、现有 DeepSeek 主备 key、模型和 HTTP 客户端，新增：

- `GET /school-ai-demo/api/math-review/status`
- `POST /school-ai-demo/api/math-review`：multipart，单个 `image` 文件 + 可选 `prompt`（≤2000 字，空则用默认）。**本次起不再要求 `consent` 字段**；不接受混入文字或多张图片。

提示词默认 `作为初一数学老师，判卷`，页面上的提示词框可修改并随请求发送；后端的输出格式与判卷约束会追加在用户提示词之后，避免自定义提示词破坏 JSON 结构。示例卡片也显示同一提示词，并可把示例图片一键放入实时区（只预览，不自动提交）。

成功返回 `result: {summary, rows: [{question, studentAnswer, verdict, feedback}], notice}`、`model`、`remaining`、`quotaScope: "essay-and-math"`。判定只允许“正确 / 错误 / 部分正确 / 未作答 / 待确认”。没有识别到可判题目时返回空 rows 和明确说明。输出限制 60 行、JSON 总量和各字段长度，不向前端透出模型额外添加的字段。超长、截断、坏 JSON、模型不支持图片均返回可读错误，不伪造成功结果。

原提示词仍为“作为初一数学老师，判卷”，并补充逐题独立核算、模糊处待确认、印刷示范不当漏填、数值等价分数不随意判错、无评分依据不擅设100分/总分等规则。不调用 OCR 或偷偷更换模型。图像使用 `detail=high`；数学最大输出预算 16384 tokens，作文仍为 4096。总模型处理时限仍为110秒，浏览器118秒；达到预算提示缩小本次输入，而不是返回半截判卷。

**AI 仍可能看错符号、分母、印刷字或算错，不是可靠的自动评分器。** UI 显示识别到的作答，常驻教师复核提醒。真实图片测试也曾出现误读；高细节参数和规则有所改善，但不保证正确性。

## 预算与隐私

- 作文和数学共用原 `quota` 表、每日50次成功分析预算，不增加表结构、不扩大每日次数。当前历史作文计数继续生效；前后端显示共用额度。
- 共用并发2和每IP每分钟5次限制。失败归还额度；图片不保存、不公开、不写日志，数据库只存日期和次数。
- 图片原图≤5MiB；浏览器支持 JPEG/PNG/WebP，转 JPEG（最长边2400px、质量0.94）去元数据。后端验证实际 MIME、JPEG/PNG 图片头、像素与大小；不信任文件扩展名。
- 2026-11-24 起按用户要求移除了上传区的遮盖个人信息提醒与发送同意勾选；前端不再发送 `consent`，后端数学接口也不再校验。作文接口仍保留同意要求。图片仍不落盘、不写日志，但会以原样发送给模型服务，由上游按其政策处理。

## 前端行为

实时区放在两组示例之前；上传、粘贴和拖拽只做本地预览，不自动判卷。页面载入仅检查一次 `/status`；提交才 POST，锁住按钮和换图操作，失败保留图片可重试。换图/移除清空旧结果。实时表格、模糊提示、复制本次 Markdown 结果均与预置示例区分；两组原始图片和表格内容保持不变。

模型文本仅经 `textContent` 渲染，不使用 `innerHTML`；复制 Markdown 时转义 HTML。手机表格局部横向滚动，页面不溢出。实时部分需 JS，禁用 JS 时仍可查看原示例。

增量设计由 kimi-k3 提供：`docs/school-math-live-design.md`。

## 测试记录（2026-11-24）

- `go test ./...`、`go test -race ./cmd/school-essay-demo`、`go vet ./cmd/school-essay-demo`；Mac/Linux amd64 构建通过。
- 后端新增测试覆盖提示词/图像隔离、模型参数、同库额度、无效上传、同意/跨站请求、JSON schema、限流/忙、失败退款、上游错误不泄露原文。
- 数学 Chrome 测试覆盖原10行示例不变、上传/粘贴/移除、超大图片、同意、防重复、XSS安全渲染、复制、限流重试/额度耗尽、灯箱、320/390px 布局和首页入口。
- 作文浏览器回归测试通过；原有作文接口测试通过。
- 使用现有配置在服务器随机 loopback 端口、临时数据库中验证新 API；不部署、不改生产数据。两张原始数学图片成功返回结构化判定。最终高细节版本用图2复测，返回12项，正确识别印刷示范并未擅设总分；临时进程正常退出。首次8192 token预算曾返回不完整结果，已提高数学预算并细化截断提示。

## 本地预览

加载私有环境配置后，从仓库根执行 `go run ./cmd/school-essay-demo`，另开终端执行 `caddy run --config cmd/school-essay-demo/Caddyfile.dev`，访问 `http://localhost:9027/school-ai-demo/math-review.html`。开发配置已包含两个 API 前缀。

## 移除顶部共用提示词卡片（20261124-232836-math-nocard，静态）

按用户要求删除了上传区上方的整块卡片（标题「示例与实时判卷，共用提示词」、提示词文本、复制提示词按钮），同时移除相关 JS 绑定与废弃 CSS。判卷提示词仍可在实时区输入框里查看和修改，两组示例卡片照旧各自显示提示词。

仅静态发布：release `20261124-232836-math-nocard`，48 个无关文件逐字节不变，未重启任何服务。资源版本提升到 `v=5`。验证：本地浏览器回归测试通过（示例提示词仍在、复制仍可用、原生文件选择控件正常），线上两处页面确认 `prompt-card` 为 0、示例提示词为 2。

## 选择文件控件修复（20261124-142851-math-label，静态）

用户反馈“选择试卷图片”按钮点击无反应。原因：可见控件是 `<button>`，靠脚本 `fileInput.click()` 打开选择器；改成原生 `<label for="mathImageFile">`（样式仍为次要按钮，`role=button`，Enter/空格键触发），由浏览器直接打开系统文件选择器，不依赖脚本点击。文件 input 不再使用 `hidden`，改为视觉隐藏但可被 label 激活。

同时把 `math.js`/`math.css` 资源版本提升到 `v=4`，避免旧缓存。仅静态发布：新增 release `20261124-142851-math-label`，48 个无关文件逐字节不变，未重启任何服务。验证：本机与线上各用真实浏览器 + 文件选择器拦截测试，点击控件与按 Enter 都能打开选择器；其余交互回归测试通过。

## 提示词 / 文本精简发布（20261124-140314-math-prompt）

- companion release：`/srv/apps/school-essay-demo/releases/20261124-140314-math-prompt`，上一版 `20261124-130024-math-live` 保留在 `previous`。
- 静态 release：`/srv/apps/school-ai-demo/page-overrides/releases/20261124-140314-math-prompt`（47 个原有文件逐字节未变，仅替换数学页面、`math.js`、`math.css`）。
- 备份：`/srv/apps/school-essay-demo/deploy-backups/20261124-140314-math-prompt-final/`（含一致性 SQLite 备份与站点配置）。
- 变更：提示词可编辑并随请求发送；示例卡片显示提示词；示例图可一键放入实时区；移除同意勾选、隐私说明、容量/格式提示、预置徽标、示例间跳转链接、顶部提示条与模型额度行。
- 验收：在用户 Chrome 中把提示词改为“只批改第1题填表”，用示例图提交，返回 4 项且仅第 1 题，额度 46→45；作文真实提交通过（52 字，45→44）；51 个公开资源哈希一致；教案五行提示词不变；填表/主站/keys 服务未重启。

## 上一次发布记录（20261124-130024-math-live）

| 项 | 值 |
| --- | --- |
| companion release | `/srv/apps/school-essay-demo/releases/20261124-130024-math-live` |
| 上一 companion | `/srv/apps/school-essay-demo/releases/20261124-103100-essay`（`previous` 已指向） |
| 静态 release | `/srv/apps/school-ai-demo/page-overrides/releases/20261124-130024-math-live` |
| 备份 | `/srv/apps/school-essay-demo/deploy-backups/20261124-130024-math-live/` |
| 二进制 SHA-256 | `f21198f87d65ae35bf986983f44d3f29bf32223902492b2e3074240388702cb8` |
| 源码 | 审核后的隔离快照，基线 commit `4b4e0b7bc59fb81682359e3205d9d46b22a9baf2`；未提交/推送 |

备份含 `site-before.caddy`、`main-before.caddy`、`service-before`、`apply.sh`、`payload.tgz`、`release.json`、`SHA256SUMS`，以及**一致性 SQLite 备份** `quota-before.db` 和计数快照 `quota-before.json`。

发布为最小范围：只重启 `school-essay-demo`；`school-ai-demo`、`go-sites-demo`、`go-sites-keys` 的 PID 与启动时间未变。替换前等待 9026 无已建立连接（连续两次检查），避免旧版退出中断在途分析。未迁移表结构、未重置任何计数。

线上验收：在用户已登录的 Chrome 中经真实界面选择本地示例图（浏览器压缩为 1080×1580 / 228KB）→ 勾选同意 → 提交，得到“本次实时生成 · deepseek-v4.1-flash · 11 项判定”，额度 48→47；随后作文真实提交成功（53 字，47→46），说明两条链路共用同一服务和额度。51 个公开资源哈希与本地一致（含原 PPTX、36 张课件预览、两组数学原图与示例表格）；教案提示词仍为 5 行；不存在路径穿越读取 quota 的入口；页面打印的 `Uncaught (in promise) Object` 经对照裸 PNG 页面确认来自浏览器/扩展，与本站脚本无关。

## 回滚

最近一次（20261124-140314-math-prompt）：确认 Caddy 未被后续改动后，把根目录恢复为 `releases/20261124-130024-math-live`（或用该备份目录里的站点配置覆盖），再把 `current` 指回 `releases/20261124-130024-math-live` 并重启服务。注意旧版仍要求数学 `consent` 字段，回滚后必须同时回滚静态页面（旧页面会发送该字段）。

再往前回滚首次实时上线：恢复对应备份的 `site-before.caddy` 到 `/etc/caddy/sites-enabled/liangz77.cn.caddy`，`caddy validate` 通过后 `systemctl reload caddy`；再把 `/srv/apps/school-essay-demo/current` 指回 `releases/20261124-103100-essay` 并 `systemctl restart school-essay-demo`。**不要恢复或清空 quota 数据库**：新版本只增加了路由，计数结构未变，回滚后旧版继续使用同一计数。保留两个 release 与备份，便于再次发布。若 Caddy 已有他人后续变更，只定向撤销数学路由与静态 root，不整文件覆盖。

## 后续部署范围

1. 从隔离源码快照构建 companion 新二进制（包括 main.go、math.go），保留旧 current/previous 和数据库。无需迁移 quota。新版本修正了关闭顺序，SIGTERM 时等待在途分析结算后退出。
2. **只更新 school-essay-demo，不重建或重启 school-ai-demo、主站或 keys。** 上线前检查旧进程在途请求，避免旧版退出行为中断分析。
3. 完整克隆现行静态 overlay 至新 release；更新 `app.js`、`math-review.html`、`math/math.js`、`math/math.css`、`essay/essay.js`（共用额度说明）。两组数学原图、示例表格及教案/PPTX不变。
4. 在 Caddy 现有 `@school_essay_api` 增加 `/school-ai-demo/api/math-review` 与 `/school-ai-demo/api/math-review/*`；在 `@school_ai_app` 显式排除同样路径。静态 matcher 不用新增路径，只更新版本化 root。
5. 备份→validate→reload→验证数学图片真实提交、作文真实提交、既有资源哈希及额度持久化。若失败，恢复旧 companion 二进制和 Caddy/静态版本，**不回滚数据库或清空已用额度**。
