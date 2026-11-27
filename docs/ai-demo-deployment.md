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

## 「网页 PPT」技能页（2026-11-26）

- 新增 `/ai-demo/guizang-ppt-skill/`，介绍 `guizang-ppt-skill`（作者歸藏，AGPL-3.0）的六步制作流程、四组示例提示词和安装说明；首页「制作」分类第三张卡片。
- 示例为《江雪》十页演示稿，放在 `guizang-ppt-skill/jiangxue/`：`index.html` + `images/`（3 张公版古画）+ `assets/motion.min.js`，共 1.8MB。演示稿是 16:9 设计，窄屏加了一条可关闭的观看提示，不影响桌面演示。
- 安装说明 `install.md` 指向 GitHub 上游并按提交钉版本，本站不托管技能 ZIP；技能由上游维护，更新只改 `install.md` 里的提交号。
- 只发布静态资源：不改 Caddy、不重启 `school-ai-demo` / `school-essay-demo` 进程、不动数据库。
- 回归：`check_cases_browser.js` 135 项（新增三张制作卡、PPT 卡片海报与示例文案、新页面三种宽度的横向溢出）、`check_catalog_browser.js` 34 项（卡片总数 16）通过；`go test ./...`、两处 Go 构建、`node --check` 通过。

## 站点图标换成分格标（2026-11-26）

- 原来的图标是渐变方块里一个「学」字，只覆盖学校场景；页面里还并存三种写法（首页内联 SVG 的「学」、学校页的 `<span class="logo">学</span>`、外贸页的 `<span class="case-logo">AI</span>`）。现在统一成一个文件：`web/logo.svg` 渐变方块 + 2×2 白色圆角方块，代表四类案例。
- 页头、页脚、各案例页、技能页都用 `<img class="brandmark" src="…/logo.svg" width="26" height="26" alt="">`；`.case-logo`、`.logo` 这两个渐变 span 规则删掉，尺寸改由 `app.css` / `cases.css` / `essay/essay.css` 的 `.brandmark` 规则统一到 26px（页脚 20px）。
- `favicon.svg` 换成同一图案，并给所有页面的 `<link rel="icon">` 加 `?v=mark-1`，否则浏览器会一直显示旧的「学」；`app.css?v=mark-1`、`essay/essay.css?v=mark-1` 同步换号。
- 部署脚本的线上校验清单加上 `/ai-demo/logo.svg` 和 `/ai-demo/guizang-ppt-skill/`。
- 仍只动静态文件：不改 Caddy、不重启服务、不写库。回归 `check_cases_browser.js` 135 项、`check_catalog_browser.js` 34 项通过。

## 首页前端缓存（2026-11-26 增量修复）

- 案例定义本来就随 `app.js` 发布；首页改为同步渲染，不再等待 `/api/state`。首次打开、分类切换、详情返回、浏览器后退及填表返回都不为列表请求后端。
- `sessionStorage` 的 `ai-demo:catalog:v1:<basePath>` 只保存当前分类、模板的 slug / name / fieldCount。同一标签页跨页面返回复用；显式 `#school` / `#trade` 优先。缓存损坏或浏览器禁用存储时仍显示静态案例，不呈现错误的“0 种表格”。
- 不缓存学生资料、最近会话、表格字段、模型配置或额度；真正进入填表时刷新状态及模板，状态请求失败不会用旧额度打开表格，快速重复点击合并处理。
- 缓存回归脚本 `cmd/school-ai-demo/check_catalog_browser.js`：34 项检查通过，覆盖无缓存/损坏/禁用存储、返回分类、敏感字段排除、额度更新、直达表格、错误恢复与三个案例真实页面往返；另以浏览器后退实际确认外贸三张卡片和 0 次 state 请求。原有 126 项页面检查及 Go / Python 测试仍通过。
- 本轮只从已提交 `web/` 制作新静态 release，核对旧 REVISION 后原子切换 current；保留旧 release 和 previous-target，失败切回。无需改 Caddy、导航或重启后台。

## 案例返回入口统一（2026-11-26）

学校填表（两个模板）、教案、作文、数学与三个外贸页，均通过 `case-nav.css` 使用同一右上角「← 全部案例」。去掉学校页原顶部导航/正文返回，以及数学和外贸页底部重复的列表返回；保留页内定位与业务操作。填表返回按钮从工具栏移到页头，首页隐藏；返回学校分类并聚焦该分类，仍不请求列表接口。学校静态页显式返回 `./#school`，外贸返回 `../#trade`。

`check_return_browser.js` 覆盖 8 种入口 × 375/768/1440 CSS 像素宽度：按钮唯一、右上同排、无横向溢出、正确分类及缓存返回，共 122 项本地检查通过。原有 126 项页面及 34 项缓存检查仍通过；Go 全量测试、构建和 5 项 Python 素材/链接测试通过。仍只发布前端，不改变后台和路由。

## 管理员调用统计

首页页脚「管理后台」链接指向 `/ai-demo/admin/`，登录只填密码（服务端 `AI_DEMO_ADMIN_PASSWORD`，未配置时禁用）。会话使用限于后台路径的 Secure、HttpOnly Cookie，服务重启后重新登录。只展示汇总数字，未登录不能访问，连续错密会触发短期限速。设计参考 Kimi K3 的无外链、服务端 SVG 折线图与每日表格方案。

填表、作文、数学的模型客户端在收到可用结果后记录一次成功。失败、超时、被截断或无法解析的结果不计；主密钥失败后备用请求成功只记一次。统计用北京时间，展示最近 30 天，开始采集前不补录。两个服务共用 `SCHOOL_AI_DEMO_DATA_DIR` 中的 `model-usage.db`，不包含个人数据。功能发布需要更新两项后台二进制、服务端密码配置和 Caddy 的精确路由；不用替换静态前端或业务数据库。

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
- Caddy 候选配置校验通过。首次上线校验发现 `rewrite` 与 `uri` 的默认排序导致学校 API 路径错误，脚本已自动回滚并核验原站恢复；改为单个 `uri replace` 避免顺序问题。随后在服务器独立 loopback 18767 Caddy（admin 关闭）验证首页、三个案例、模板、作文/数学状态接口全部 200，旧路径及查询参数 308 保留、未知页面 404。部署后另核验 HTTPS、下载、学校接口、导航和真实页面交互；不调用真实模型以避免消耗用户额度。

## 模型账号改为按北京时间分时（2026-11-28）

- 旧的 OpenCode Go 订阅和两把 `SCHOOL_AI_DEEPSEEK_*` 密钥已从服务器环境文件删除，替换成两组账号：
  - `DEEPSEEK_*`：DeepSeek 官方 `https://api.deepseek.com`，`deepseek-flash`，**北京时间 20:00–02:00** 使用；
  - `OLLAMA_*`：Ollama Cloud `https://ollama.com/v1`，`deepseek-v4.1-flash`，**其余时段**使用。
- 两个账号都在 `/srv/apps/school-ai-demo/shared/school-ai-demo.env`（600，root:root），由 `school-ai-demo` 与 `school-essay-demo` 共用；密钥只存在服务器，不写入仓库、日志、状态接口或页面。
- 每次请求按北京时钟选账号（`internal/aiprovider`）。窗口是 `[20:00, 02:00)`，20:00 整切到官方、02:00 整切回 Ollama；UTC 时钟按固定 +08:00 换算，不受服务器时区影响。
- **取消了原来的主/备 key 重试**：账号不是彼此的备份，选中的账号失败就如实报错（401/403 报“密钥不可用”，429/402 报“额度不足或繁忙”），不会静默换模型。
- 状态接口（`/api/state`、`/api/essay-review/status`、`/api/math-review/status`）返回 `configured`、`provider`、`model`、`window`，不含密钥；页面显示当前账号名。
- 两个账号都实测过：DeepSeek 官方与 Ollama Cloud 均支持纯文本、图片输入（vision）和 `response_format: json_object`；模型名分别是 `deepseek-flash` 与 `deepseek-v4.1-flash`。
- `AI_PROVIDER_NOW=<RFC3339>` 可临时钉住时钟，用于在备用端口预演另一时段；生产环境不设置。设置时启动日志会警告。
- 测试：分时边界（19:59 / 20:00 / 01:59 / 02:00 / 12:00 以及 UTC 等价时刻）、单账号失败不串号、状态不含密钥、base URL 拼接、rehearsal 时钟回退。
- 上线核对：00:1x（窗口内）线上 `/api/state` 与作文/数学状态显示 `DeepSeek / deepseek-flash`，真实作文点评 67 字、数学图片判卷 11 项、填表 3 个字段均由官方账号返回；随后用 `AI_PROVIDER_NOW` 在 19027 端口预演 09:00 时段，显示 `Ollama Cloud / deepseek-v4.1-flash` 并成功返回 49 字点评，预演进程与临时目录已清理。
- 回滚：二进制 release `20261128-keys-1b3cba8`（后续 `20261128-keys-b291c97`、`20261128-formlog` 只加日志）之前是 `20261127-034259-admin-logout` / `20261127-032255-usage-admin`；旧环境文件备份在 `/srv/apps/school-ai-demo/deploy-backups/20261128-keys-1b3cba8/school-ai-demo.env`。回滚时把 `current` 指回旧 release、恢复该 env 并重启两个服务。
