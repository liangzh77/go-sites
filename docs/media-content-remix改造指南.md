# media-content-remix 轻量服务器部署改造指南

## 背景

项目将部署到阿里云轻量服务器，作为 `liangz77.cn` 下的一个独立子域名服务。

整体架构约束：

- 使用 Go + SQLite + WAL。
- 使用 Caddy 作为 HTTPS 入口和反向代理。
- 使用 systemd 管理 Go 服务。
- 使用 Git push 自动部署。
- 不使用 Docker、Kubernetes、Nginx、RDS。
- 项目保持独立仓库，不合并到 `go-sites` 仓库。
- 服务器统一目录使用 `/srv`，不要使用 `/opt`。

目标线上结构：

```text
/srv/git/media-content-remix.git

/srv/apps/media-content-remix/
  releases/
    20260421-153000/
      media-content-remix
  current -> releases/当前版本
  previous -> releases/上一版本
  shared/
    app.db
    config.env
    data/
```

Caddy 反代：

```caddyfile
mcr.liangz77.cn {
    reverse_proxy 127.0.0.1:8080
}
```

## 改造目标

请对项目做以下改造，保持代码简单、直接、低依赖。

## 1. 增加健康检查接口

新增公开接口：

```text
GET /healthz
```

返回：

```json
{"ok":true}
```

要求：

- 不需要登录。
- 返回 HTTP 200。
- 可以顺便检查 SQLite 是否可用，例如执行 `SELECT 1`。
- 部署脚本会用它判断新版本是否启动成功。

## 2. 配置改为支持环境变量

当前 `cmd/web/main.go` 里写死：

```go
Addr:      ":8080",
DBPath:    "app.db",
```

请改成从环境变量读取，未设置时保留默认值。

建议环境变量：

```text
MCR_ADDR=127.0.0.1:8080
MCR_DB_PATH=app.db
SILICONFLOW_BASE_URL=https://api.siliconflow.cn/v1
YT_DLP_BIN=/usr/local/bin/yt-dlp
```

要求：

- 线上默认监听 `127.0.0.1:8080`，不要监听 `:8080`。
- 本地如果不设置环境变量，也能正常运行。
- 可新增一个小函数，例如：

```go
func envOrDefault(key, fallback string) string
```

注意：

- `docs/dy在线资源.xlsx` 只是早期开发调试文件，正式运行不再依赖。
- 参考博主列表以后以 SQLite 数据库和 Web 后台录入为准。
- 如果代码里仍有 `ExcelPath`，请只保留为旧命令行抓取工具的兼容逻辑，不要让 Web 服务启动依赖它。
- 线上 `config.env` 不需要配置 `MCR_EXCEL_PATH`。

## 3. systemd 配置改为 `/srv` 结构

更新 `docs/ubuntu_deploy.md`，不要再使用 `/opt/media-content-remix`。

改为：

```text
/srv/apps/media-content-remix/
  current/
  shared/
    app.db
    config.env
    data/
```

systemd 示例改成：

```ini
[Unit]
Description=Media Content Remix
After=network.target

[Service]
Type=simple
WorkingDirectory=/srv/apps/media-content-remix/shared
EnvironmentFile=/srv/apps/media-content-remix/shared/config.env
ExecStart=/srv/apps/media-content-remix/current/media-content-remix
Restart=always
RestartSec=3
User=www-data
Group=www-data

[Install]
WantedBy=multi-user.target
```

`config.env` 示例：

```env
MCR_ADDR=127.0.0.1:8080
MCR_DB_PATH=app.db
YT_DLP_BIN=/usr/local/bin/yt-dlp
SILICONFLOW_BASE_URL=https://api.siliconflow.cn/v1
```

## 4. 增加 Git push 自动部署说明

在 `docs/ubuntu_deploy.md` 中补充 Git push 自动部署方式。

目标体验：

```bash
git push prod main
```

服务器结构：

```text
/srv/git/media-content-remix.git
/srv/build/media-content-remix
/srv/apps/media-content-remix
```

部署流程：

```text
1. 接收 git push
2. checkout 到 /srv/build/media-content-remix
3. go build -o media-content-remix ./cmd/web
4. 创建 releases/时间戳/
5. 复制二进制到 release
6. 切换 previous 和 current
7. systemctl restart media-content-remix
8. curl http://127.0.0.1:8080/healthz
9. 失败则切回 previous 并重启
```

可以先只写文档，不一定马上实现完整脚本。

## 5. 增加示例 post-receive hook

请在文档中给出一个可用的 `post-receive` hook 示例。

要求：

- 用 bash。
- 构建命令：

```bash
go build -o media-content-remix ./cmd/web
```

- release 目录使用时间戳：

```bash
date +%Y%m%d-%H%M%S
```

- 健康检查：

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

- 失败时回滚到 previous。
- 不要处理数据库迁移之外的复杂事情。

## 6. 确认 SQLite/WAL 设置

当前 `internal/webapp/db.go` 已有：

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
```

请检查并保持。

建议补充：

```sql
PRAGMA synchronous = NORMAL;
```

要求：

- 不要引入 PostgreSQL。
- 不要引入 ORM。
- 继续使用当前 `database/sql + modernc.org/sqlite`。

## 7. 数据目录统一放到 shared

线上所有运行期数据必须放在：

```text
/srv/apps/media-content-remix/shared/
```

包括：

```text
app.db
app.db-wal
app.db-shm
data/
config.env
```

不要让运行期数据写入 `current` 或 `releases`。

代码层面需要确保：

- 默认 `MCR_DB_PATH=app.db` 时，因为 `WorkingDirectory` 是 `shared`，数据库会落在 `shared/app.db`。
- 默认素材目录 `data/materials/user-x` 也会落在 `shared/data/...`。

## 8. 本地开发保持简单

本地开发不要引入 Caddy/systemd。

本地启动方式保持：

```powershell
C:\tools\go\bin\go.exe run ./cmd/web
```

或如果 PATH 里有 Go：

```bash
go run ./cmd/web
```

本地访问：

```text
http://127.0.0.1:8080
```

本地数据库继续使用仓库根目录下的：

```text
app.db
data/
```

这些已经在 `.gitignore` 里，不要提交。

## 9. 检查 yt-dlp.exe 是否应继续留在仓库

当前仓库里有：

```text
yt-dlp.exe
```

请判断是否保留。

建议：

- 如果只是 Windows 本地开发方便，可以保留，但文档说明它只用于本地。
- Linux 线上统一使用 `/usr/local/bin/yt-dlp`。
- 如果决定不纳入版本控制，需要先确认不会影响本地开发，再加入 `.gitignore` 并从 Git 追踪中移除。

不要误删用户仍然依赖的文件。

## 10. 测试要求

改完后至少运行：

```powershell
C:\tools\go\bin\go.exe test ./...
```

如果 PATH 里有 Go，也可以：

```bash
go test ./...
```

再本地启动：

```powershell
C:\tools\go\bin\go.exe run ./cmd/web
```

检查：

```text
http://127.0.0.1:8080/healthz
http://127.0.0.1:8080/login
```

## 11. 不要做的事

本次不要：

- 不要迁移到 Docker。
- 不要引入 Nginx。
- 不要引入 PostgreSQL/RDS。
- 不要引入 Redis。
- 不要改成前后端分离。
- 不要大规模重构模板系统。
- 不要移动项目到 `go-sites` 仓库。
- 不要删除用户数据文件。
- 不要提交 `app.db`、`app.db-wal`、`app.db-shm`、`data/`。
