# 会计案例：AI 辅助增值税（VAT）对账

本文件只记录这一个增量案例；站点整体发布说明仍见 [AI Demo 部署](ai-demo-deployment.md)。

- 首页在「外贸」右侧新增同级分类「会计」，下面只有一个案例卡片，点开是 `/ai-demo/vat-reconciliation/`。其他分类与案例未改。
- 页面面向非会计读者：第一步用一笔编造的小生意区分「算税」和「对账」——销售收款 £1,200（销项税 £200）、进货 £600（可抵扣进项税 £100）、应缴 £100；再假设申报表写成 £120，把那 £20 差异拆成三条要查的原因。两处标注「教学示例，不是真实客户数据」。
- 第二步讲英国 Thomas & Co.（46 人）的四天试点：盘 17 类工作底稿并按频率/耗时/难度选 VAT；表格并入数据库、协调助手分派核对；接账务 API 与可检索税法，语音访谈定「缺资料先查找，解决不了转会计」；交付代码与看板，用一份人工做完的历史对账做端到端比对，团队报告吻合。
- 第三步是职责表（AI 备稿标差异 / 会计复核签字）和三条边界：只此一个环节试点、不是全所自动化；没有测出 50 倍提效；视频是创作者自述，不是第三方审计。附原片链接 <https://www.youtube.com/watch?v=IXDYEF22CG8>。
- 设计沿用既有 case-page 骨架，仅新增 5 个 `.vat-` 类；布局由 `kimi-k3` 出稿，见 [案例设计稿](ai-demo-vat-case-design.md)。
- 素材 `cases/vat-reconciliation-demo.json`（`is_synthetic: true`、权限全 false）与生成页一起提交；`build_cases.py` 生成四页。测试新增：示例金额自洽（1,200−200=1,000、600−100=500、200−100=100、120−100=20）、四天结构、46/17/4 与边界措辞、页面必备文案与外链属性、生成结果无漂移。
- 回归：`check_vat_browser.cjs` 在本地静态服务 + 无头 Chrome 覆盖分类药丸顺序（学校/外贸/会计/制作/工具）、卡片归属与 `#accounting` 深链、三步 Tab 的鼠标/键盘/步骤按钮、示例金额、四张日程卡、边界文案、外链 `rel`、1440/768/375 无横向溢出与日程卡列数、无 JS 报错；`go test ./...`、`go build ./cmd/school-ai-demo`、`node --check web/app.js` 通过。
- 本次发布只换前端静态 release（`/srv/apps/ai-demo/releases/<时间>-<commit>`），不动 Caddy、不动 `data.js`、不重启 `school-ai-demo` / `school-essay-demo`。工作区当时另有未提交的管理后台改动，因此没有走要求 clean tree 的 `deploy_ai_demo.sh`；改动单独提交后从 `HEAD` 打包 `cmd/school-ai-demo/web`，并先核对线上 release 与 `HEAD` 的其余文件逐字节一致。

## 线上发布与验收（2026-11-27）

- 直达：<https://liangz77.cn/ai-demo/vat-reconciliation/>；首页分类栏为「学校 / 外贸 / 会计 / 制作 / 工具」，「会计」紧挨「外贸」右侧，卡片归属 `accounting`。
- 静态 release：`/srv/apps/ai-demo/releases/20261127-vat-3d42a89/`，`REVISION` = `3d42a899a49ae0dff8ee0cddae6386ce02f087b3`。备份（旧指向与前一日志）：`/srv/apps/ai-demo/backups/20261127-vat-3d42a89/`。
- 本次只新增 `vat-reconciliation/` 并替换 `app.js`、`cases.css`；发布脚本在服务器上逐字节比对，确认其余 110 个文件未变。`index.html` / `lesson-plan.html` 保持线上原样（仓库 HEAD 版本与线上不同，且有人正在改），不改 Caddy、不改 `data.js`、不重启 `school-ai-demo` / `school-essay-demo`。
- 线上浏览器验收（无头 Chrome 访问真实域名）：分类顺序、卡片归属、`#accounting` 深链、三步 Tab 的点击/键盘/步骤按钮、示例金额与两处「教学示例」标注、四张日程卡、三条边界、外链 `target`/`rel`、1440/768/375 无横向溢出、无 JS 报错；`/ai-demo/`、三个外贸页、学校静态页与三个 API 均 200，未知路径 404，旧 `/school-ai-demo/` 仍 308。
- 已知遗留：仓库里 `web/index.html` 与 `web/lesson-plan.html` 存在他人未提交改动（页脚「管理后台」链接），线上已是该版本；`deploy_ai_demo.sh` 要求工作区干净，因此本次未用它，改用等价的「从 HEAD 打包 + 只覆盖本次文件 + 原子切链」流程。

## 文案调整：删掉四处提示（2026-11-27）

按客户要求删除四处文案，且不留空框、不以别的文案替代：

1. 页面顶部「公开案例 + 教学示例。案例过程按创作者视频整理……」整段（`.case-note`）。
2. 「英国 Thomas & Co.（46 人） · 4 天试点 · 来源：Liam Ottley 公开视频」这一行（`.context`）。
3. 示例表下方「教学示例，不是真实客户数据。这里的 1,200、600 和申报结果……」整段。
4. 差异卡里的「教学示例，不是真实客户数据」胶囊标签（`.vat-demo-tag`，样式一并删除）。

其余内容不动：示例金额、四天流程、职责表、三条边界、原片链接仍在。生成器、页面、CSS 与测试同步修改；测试新增「四处文案不得出现」「`.vat-demo-tag` / `.context` 计数为 0」「不存在空 `.case-note`」的断言。

- 本次发布：release `/srv/apps/ai-demo/releases/20261127-vat-copy-d5627cf/`（`REVISION` = `d5627cf9d2aa7c0363559ee6499a458a72c3ea8b`），备份 `/srv/apps/ai-demo/backups/20261127-vat-copy-d5627cf/`；只替换 `cases.css` 与案例页，其余文件逐字节未变，未动 Caddy / `data.js`、未重启服务。
- 线上复验：真实域名下四处文案已消失、无空提示条（`.case-note` 无空节点、`.vat-demo-tag`/`.context` 计数 0），金额、四天流程、职责表、边界与原片链接仍在；首页与三个外贸页、学校页、API 均 200，未知路径 404，旧 `/school-ai-demo/` 仍 308。
