# 作文与数学判卷 companion API

2026-11-24 已上线数学图片实时判卷 `/api/math-review`，与作文共用原额度表和每日50次预算；数学提示词可在页面上编辑（后端限制 2000 字并追加固定输出约束），数学接口不再要求 `consent`，作文接口仍要求。当前 companion release `20261124-140314-math-prompt`，`previous` 为 `20261124-130024-math-live`。详见 [math-live.md](math-live.md)。下方首次上线记录与配置仍有效；文中「作文点评额度」等历史措辞现在也覆盖数学，接口会返回 `quotaScope: essay-and-math`。

**当前状态：2026-11-24 已上线，文字和图片真实点评均已通过公开页面验收。** 不替换 `school-ai-demo` 的现有二进制；该二进制的部分内嵌首页资产比 checkout 更新，直接重建可能导致回退。

前端位于 `../school-ai-demo/web/essay-review.html` 与 `web/essay/`；首页 `app.js` 已增加「批改作文」卡片。主要界面设计由 `kimi-k3` 产出，见 `docs/school-essay-design.md`，实现时补充了服务端限额、双方输入隐私提示和图片归一化。

## 当前部署

- 页面：<https://liangz77.cn/school-ai-demo/essay-review.html>；首页已有「批改作文」卡片，`/private` 沿用原 School AI Demo 入口。
- systemd：`school-essay-demo`，用户 `schoolai`，仅监听 `127.0.0.1:9026`，已启用开机启动；配置见同目录 `school-essay-demo.service`。
- API release：`/srv/apps/school-essay-demo/releases/20261124-103100-essay`。
- 当前静态 release：`/srv/apps/school-ai-demo/page-overrides/releases/20261124-111138-math`（数学示例上线后切换，原有作文/教案资源字节不变；本 API release 未变）。
- 数据库：`/srv/apps/school-essay-demo/shared/quota.db`，只保存每日计数。
- 回滚备份：`/srv/apps/school-essay-demo/deploy-backups/20261124-103100-essay/site-before.caddy`；原静态 release `20261124-553fc7142a8c` 保留未改。
- 二进制 SHA-256：`069e20e1137beab65d2e1eceed40e54988205f47830e29921f193562e0d33058`。
- release 内含 `release.json`、`source.tar.gz` 和 `SHA256SUMS`。从经过审核的隔离源码快照构建，基线 commit `4b4e0b7bc59fb81682359e3205d9d46b22a9baf2`；本次工作区修改未提交或推送 GitHub。
- 上线验收：46 个公开资源与本地逐字节哈希一致（含原始 PPTX、36 张课件预览、两张作文原图）；桌面/390px 手机浏览器、首页入口、复制提示词/评语、真实文字与图片调用通过。新服务重启后剩余额度不变；旧填表、主站及 keys 服务 PID/启动时间均未改变。

## 行为与限制

- 两组原始作文图片和用户提供的点评逐字作为预置示例，不请求模型，不替换为实时输出。
- 提示词：`作为小学语文老师，给出真实，口语化评价，点评一个优点，给出一个建议，100字以内，第二人称`。
- 文字输入 1–8000 Unicode 字符；一次一张图片，原图 ≤5MiB；支持选择、拖入和剪贴板粘贴。
- 浏览器将 JPEG/PNG/WebP 归一化为 JPEG，最长边 ≤2400px，去除元数据；服务端只接收 JPEG/PNG，验证 MIME、图片头、5MiB 和 4000 万像素上限。
- 只提交当前模式，模式切换清空另一模式；文本和图片都要求勾选发送同意。
- 复用现有 DeepSeek 环境变量，默认 `deepseek-v4.1-flash`。图片直接交给相同模型，不用 OCR、不偷偷更换模型。
- 只输出评语；超过 100 个字符（含标点）时同模型精简一次，仍超长则报错，不机械截断句子。
- 单请求总模型时间上限 110 秒，浏览器等待上限 118 秒；服务端最多并行 2 次，每来源 IP 每分钟 5 次（IP 仅短暂留在内存中）。
- 独立每日成功点评上限 50 次，上海时区午夜重置；SQLite WAL 原子预占，失败归还。进程崩溃时宁可保留预占，不重置预算。一次成功点评可能发生一次精简和主备 key 重试，因此额度不是精确的模型请求/token 数。
- SQLite 只保存 `day`、`used`；作文原文、图片、输出不落盘、不写数据库、不写日志。上游数据政策不由本站承诺。

## 接口

| 方法 | 路径 | 内容 |
| --- | --- | --- |
| GET | `/school-ai-demo/api/essay-review/healthz` | 数据库健康检查 |
| GET | `/school-ai-demo/api/essay-review/status` | 模型、提示词、配置是否就绪、当日剩余额度 |
| POST | `/school-ai-demo/api/essay-review` | multipart：`mode=text` + `text`，或 `mode=image` + `image`；均需 `consent=yes` |
| GET | `/school-ai-demo/api/math-review/status` | 数学模型状态与共用剩余额度 |
| POST | `/school-ai-demo/api/math-review` | multipart：单张 `image` + 可选 `prompt`（≤2000 字）；返回逐题结构化判卷 |

成功返回 `review`、`model`、`characters`、`remaining`；错误返回 `code`、`error`，不透出上游原始响应或凭据。额度耗尽、频率限制、繁忙均有不同错误码。上游不支持图片时明确提示改用文字，不自动替用户切换输入。

## 配置与本地运行

沿用 `SCHOOL_AI_DEEPSEEK_API_KEY`、`SCHOOL_AI_DEEPSEEK_API_KEY_BACKUP`、`SCHOOL_AI_DEEPSEEK_BASE_URL`、`SCHOOL_AI_DEEPSEEK_MODEL`。凭据只放私有环境配置中，不提交 Git，不写入前端。

新增变量：`ESSAY_ADDR=127.0.0.1:9026`、`ESSAY_DB_PATH=school-essay-quota.db`、`ESSAY_DAILY_LIMIT=50`。base 默认 `https://opencode.ai/inference/go/openai/v1`；网关请求使用 UUID 会话头，主备只换 key 不换模型。

在仓库根目录执行（先从私有配置加载模型环境变量）：

```sh
go run ./cmd/school-essay-demo
# 另一终端，需要本机已安装 Caddy：
caddy run --config cmd/school-essay-demo/Caddyfile.dev
```

浏览器打开 `http://localhost:9027/school-ai-demo/essay-review.html`。开发 Caddyfile 只预览新页面及新 API，不启动旧填表服务。

## 测试

```sh
go test ./...
go test -race ./cmd/school-essay-demo
go vet ./cmd/school-essay-demo
go build -o /tmp/school-essay-demo ./cmd/school-essay-demo
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/school-essay-linux-amd64 ./cmd/school-essay-demo
node cmd/school-essay-demo/browser-test.cjs
```

浏览器测试使用独立临时 Chrome profile 和 mock API，不调用模型。默认 Chrome 为 macOS 应用路径，其他环境可通过 `CHROME_BIN` 指定。覆盖示例无请求、图片放大、文字提交、剪贴板图片、模式隔离、字数上限、390px 手机布局及 JS 异常；截图写系统临时目录。

2026-11-24：Go 全库测试、race、vet、Mac/Linux 构建及浏览器测试通过。使用现有配置在服务器隔离临时端口运行新 API，真实文字/图片点评均通过（52 / 38 字）。本机网络曾遇到一次图片请求超时，接口正确返回 504；服务器侧复测成功。临时测试进程已退出，没有变更现有服务、Caddy 路由或线上数据库。

## 后续增量发布方案

不要运行主站全量 `deploy.ps1`，不要重建旧 `school-ai-demo`，也不要发布工作区中其他项目的变更。

1. 新 API 单独发布到 `/srv/apps/school-essay-demo/releases/<id>/school-essay-demo`，`current` 指向已校验的产物；以现有非 root 用户 `schoolai` 运行，仅监听 9026。首次发布先核实端口及服务名空闲；后续确认仍归本服务所有，保留 previous 以便回滚。
2. 通过 systemd `EnvironmentFile=/srv/apps/school-ai-demo/shared/school-ai-demo.env` 复用模型配置；额外设置 `ESSAY_DB_PATH=/srv/apps/school-essay-demo/shared/quota.db`。只给新 shared 目录写权限，使用 `NoNewPrivileges`、`PrivateTmp`、`ProtectSystem=strict` 等限制。
3. 在新静态 overlay release **复制完整的现行 lesson 资产**，加入 `essay-review.html`、`essay/*` 和新的 `app.js`；不要覆盖旧 release。原始 PPTX 与全部预览保持不变。
4. 备份 Caddy 后加入：

```caddyfile
@school_essay_api path /school-ai-demo/api/essay-review /school-ai-demo/api/essay-review/*
handle @school_essay_api {
    reverse_proxy 127.0.0.1:9026 {
        header_up X-Real-IP {remote_host}
    }
}
```

同时将 `/school-ai-demo/essay-review.html`、`/school-ai-demo/essay/*` 加入 static matcher，更新版本化静态根目录，并从旧 9025 代理 matcher **显式排除所有新 API 和静态路径**。三个 matcher 必须互斥。不要提供 shared 目录。同步仓库根 `Caddyfile` 后 validate / reload。

5. 验证新页面、两个示例的原图哈希、文字/图片真实点评、旧填表健康、教案 18 页预览及 PPTX 原始哈希。首页新增卡片即可，无需额外改 `/private` 已有 School AI Demo 入口。
6. 首次发布失败则恢复 Caddy 备份并 reload，停用本次新增服务；后续发布恢复 previous 二进制/静态 release。不要恢复或覆盖旧应用数据库。

### 回滚本次首次上线

注意：Caddy 后续已加入数学示例，不能直接整文件覆盖。只有确认线上 Caddy 仍是作文首次发布版本、没有任何后续变更，才可恢复上述 `site-before.caddy` 到 `/etc/caddy/sites-enabled/liangz77.cn.caddy`，执行 `caddy validate --config /etc/caddy/Caddyfile` 和 `systemctl reload caddy`，随后 `systemctl disable --now school-essay-demo`。保留 shared、release 和备份，不删除数据。同步回退仓库 Caddy 中仅本次新增的路由，检查原填表与教案恢复。若 Caddy 已有后续变更，只撤销本次相关路由，不整文件覆盖。
