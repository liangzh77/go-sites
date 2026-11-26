# AI Demo · 学校 / 外贸

## 本次范围（2026-11-26）

- 公开首页：<https://liangz77.cn/ai-demo/>，学校第一类、外贸第二类。
- 外贸三页：`customer-email/`、`quotation/`、`order-validation/`，都位于 `/ai-demo/` 下。
- 学校保留 `#enrollment` / `#collection` 填表、`lesson-plan.html`、`essay-review.html`、`math-review.html`。
- 页面、脚本和 `/private` 的「我的 → 应用」导航不再使用旧 URL。旧 `/school-ai-demo/…` 仅作 308 跳转，保留查询参数；浏览器保留片段。后台服务名及内部路径保留不代表仍以旧 URL 对外提供页面。
- 源码沿用 `cmd/school-ai-demo`，没有搬工程、换技术栈、创建数据库或接入邮箱/ERP。
- 业务素材来自 ai-business-lab 的三份已校验 JSON；均为虚构、人工预置。页面模拟测试不是模型准确率或交付收益验收。

## 设计与维护

恢复迁移前线上首页的 `index.html` / `app.css`，不是恢复本地较旧的版本。学校静态课件、作文和数学资源保持既有内容。外贸主要页面实际调用 `kimi-k3`，结果见 [迁移设计稿](ai-demo-kimi-migration-design.md)。用户反馈纵向展开不如初版清楚后，最终复用 ai-business-lab 已确认的 Kimi K3 初版布局，而不是迁移稿的长页面：三步 Tab、左侧来源切换 / 右侧原文和中文要点、双列结果卡片及展开依据、责任人待办和待审草稿。仅视觉沿用当前学校站暖白、黑字和渐变。来源切换和步骤有键盘/深链接支持；无脚本时所有内容仍可阅读，打印展开全部步骤。对客报价复制仅包含报价正文，不包含内部成本。

```bash
python3 -B cmd/school-ai-demo/build_cases.py
python3 -B -m unittest discover -s cmd/school-ai-demo -p 'test_cases.py' -v
go test ./...
go build ./cmd/school-ai-demo
go build ./cmd/school-essay-demo
node --check cmd/school-ai-demo/web/app.js
node --check cmd/school-ai-demo/web/cases.js
```

素材位于 `cmd/school-ai-demo/cases/`。生成器和生成后的 HTML 一起提交；测试检查生成结果无漂移、金额和跨案例衔接、相对链接和旧路径清除。浏览器回归脚本为 `cmd/school-ai-demo/check_cases_browser.js`，在同源测试页执行（可通过 Chrome DevTools evaluate_script）。脚本仅在测试 iframe 操作，不发送邮件、不调模型，不修改系统剪贴板；375/768/1440 CSS 像素宽度覆盖不是实际手机触摸测试。

## 最小部署

- 前端：`/srv/apps/ai-demo/releases/<时间>-<commit>/`；`current` 原子软链；`REVISION` 为实际源码提交。
- 仅修改 Caddy 站点文件 `/etc/caddy/sites-enabled/liangz77.cn.caddy` 中相关路由和主站当前目录的 `data.js`。
- 原填表服务 `school-ai-demo` / `127.0.0.1:9025`，原作文数学服务 `school-essay-demo` / `127.0.0.1:9026`，均不重建、不重启，不修改环境变量或数据库。
- Caddy 把公开 `/ai-demo/api/...` 映射到进程原有 `/school-ai-demo/api/...`；静态页面直接由 Caddy 发布，无 SPA 兜底，未知资源返回 404。
- `/ai-demo/` 是公开演示页；`/private` 只作为导航，不宣称有访问控制。素材全部为模拟内容。

提交并推送通过测试的版本后：

```bash
bash cmd/school-ai-demo/deploy_ai_demo.sh <当前线上Caddyfile和data.js对应的Git提交>
```

默认参数为 `HEAD^`。脚本仅从 HEAD 打包已提交文件，先比对线上 Caddy 与导航是否符合预期；有漂移即停止。复用既有 SSH 配置，默认主机及密钥位置见脚本，也可通过 `AI_DEMO_SSH_HOST` / `AI_DEMO_SSH_KEY` 指定。上传前端、校验 Caddy、备份、切换、reload 后检查新旧路径、静态资源与三个学校 API；应用通过后才更新导航。禁止为此运行整个站点的 `deploy.ps1`。

## 回滚

每次发布自动保留 `/srv/apps/ai-demo/backups/<release-id>/`：
`site.caddy`、`data.js`、`previous-target`、`commit`。
脚本上线阶段失败时自动恢复这两份文件和旧软链并 reload Caddy。人工回滚同样只恢复这些文件：

1. 核实回滚后不会覆盖发布后其他人的导航/路由更新。
2. 还原备份 Caddy 与实际主站当前目录的 data.js。
3. previous-target 非空则原子切回；首次迁移为空则仅移除本次 current 软链（不删除 release 或原学校服务）。
4. `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile` 后 `systemctl reload caddy`，验证恢复后的页面及 API。

无需回滚数据库。首次迁移前页面仍在原学校进程及 `/srv/apps/school-ai-demo/page-overrides/releases/20261124-232836-math-nocard`，未覆盖。

## 迁移前验收

- `go test ./...` 通过；两项相关 Go 构建通过。
- 5 项新工程素材/生成/链接测试通过；来源工程原有 6 项素材测试通过。
- 最终三步布局 Chrome 本地 126 项检查通过（7 页 × 3 宽度；三个案例各步骤单独检查；无旧链接、分类、来源切换/跳转、键盘步骤切换、复制成功/失败、3.70/3.93 及非法价格、重复单与新修订）。查看了邮件结果页实际截图。
- Caddy 候选配置校验通过。部署后另核验 HTTPS、下载、学校接口、导航和真实页面交互；不调用真实模型以避免消耗用户额度。
