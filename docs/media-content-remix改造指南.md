# Go + SQLite 项目轻量服务器部署改造通用指南

## 使用场景

这份文档用于指导另一个独立 Go Web 项目，按本仓库已经验证过的方式部署到阿里云轻量服务器。

目标不是把新项目合并进 `go-sites`，而是让新项目在自己的仓库中完成必要改造，然后通过 Git push 部署到同一台服务器或同类服务器。

适用前提：

- 项目是 Go Web 服务。
- 数据库使用 SQLite，运行期数据可以放在本机磁盘。
- 入口由 Caddy 负责 HTTPS 和反向代理。
- Go 服务由 systemd 管理。
- 部署通过 `git push prod main` 触发。
- 服务器目录统一使用 `/srv`。

不适用场景：

- 必须使用 Docker、Kubernetes、Nginx、RDS、PostgreSQL 或 Redis 的项目。
- 必须多实例横向扩容、共享数据库或复杂队列的项目。
- 前后端完全分离且需要独立构建发布多个服务的项目。

## 部署约定

把下面占位符替换成目标项目自己的值：

| 占位符 | 含义 | 示例 |
| --- | --- | --- |
| `<APP_NAME>` | 应用名、仓库名、systemd 服务名 | `media-content-remix` |
| `<APP_BINARY>` | 构建出的 Go 二进制文件名 | `media-content-remix` |
| `<APP_DOMAIN>` | 线上域名 | `mcr.liangz77.cn` |
| `<APP_PORT>` | 本机监听端口 | `8080` |
| `<ENV_PREFIX>` | 环境变量前缀 | `MCR` |
| `<BUILD_TARGET>` | Go 构建入口 | `./cmd/web` |

线上目录结构：

```text
/srv/git/<APP_NAME>.git
/srv/build/<APP_NAME>

/srv/apps/<APP_NAME>/
  releases/
    20260421-153000/
      <APP_BINARY>
  current -> releases/当前版本
  previous -> releases/上一版本
  shared/
    app.db
    config.env
    data/
```

Caddy 反代示例：

```caddyfile
<APP_DOMAIN> {
    reverse_proxy 127.0.0.1:<APP_PORT>
}
```

## 改造目标

请在目标项目仓库中完成以下改造，保持代码简单、直接、低依赖。

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
- 正常时返回 HTTP 200。
- 如果项目使用 SQLite，建议顺便执行 `SELECT 1` 确认数据库可用。
- 部署脚本会用这个接口判断新版本是否启动成功。

## 2. 配置改为支持环境变量

不要在代码里写死线上监听地址、数据库路径、第三方工具路径和外部 API 地址。

推荐做法：

```go
func envOrDefault(key, fallback string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return fallback
}
```

建议环境变量：

```text
<ENV_PREFIX>_ADDR=127.0.0.1:<APP_PORT>
<ENV_PREFIX>_DB_PATH=app.db
<ENV_PREFIX>_DATA_DIR=data
```

如果项目依赖外部命令或第三方服务，也放进 `config.env`，例如：

```text
YT_DLP_BIN=/usr/local/bin/yt-dlp
SILICONFLOW_BASE_URL=https://api.siliconflow.cn/v1
```

要求：

- 线上默认监听 `127.0.0.1:<APP_PORT>`，不要监听 `:<APP_PORT>`。
- 本地如果不设置环境变量，也能正常运行。
- Web 服务启动不应依赖开发期调试文件，例如 Excel、临时 JSON、测试素材等。
- 运行期数据路径必须能通过环境变量或相对路径落到 `shared` 目录。

## 3. systemd 配置使用 `/srv/apps`

目标项目的部署文档不要使用 `/opt/<APP_NAME>`，统一改为：

```text
/srv/apps/<APP_NAME>/
  current/
  shared/
    app.db
    config.env
    data/
```

systemd 示例：

```ini
[Unit]
Description=<APP_NAME>
After=network.target

[Service]
Type=simple
WorkingDirectory=/srv/apps/<APP_NAME>/shared
EnvironmentFile=/srv/apps/<APP_NAME>/shared/config.env
ExecStart=/srv/apps/<APP_NAME>/current/<APP_BINARY>
Restart=always
RestartSec=3
User=www-data
Group=www-data

[Install]
WantedBy=multi-user.target
```

`config.env` 示例：

```env
<ENV_PREFIX>_ADDR=127.0.0.1:<APP_PORT>
<ENV_PREFIX>_DB_PATH=app.db
<ENV_PREFIX>_DATA_DIR=data
```

如果有第三方服务密钥，也放在 `config.env`，不要提交到 Git：

```env
SILICONFLOW_API_KEY=replace-with-real-key
```

## 4. 增加 Git push 自动部署说明

目标体验：

```bash
git remote add prod deploy@<SERVER_IP>:/srv/git/<APP_NAME>.git
git push prod main
```

服务器结构：

```text
/srv/git/<APP_NAME>.git
/srv/build/<APP_NAME>
/srv/apps/<APP_NAME>
```

部署流程：

```text
1. 服务器接收 git push
2. checkout 到 /srv/build/<APP_NAME>
3. go build -o <APP_BINARY> <BUILD_TARGET>
4. 创建 releases/时间戳/
5. 复制二进制到 release
6. 切换 previous 和 current
7. systemctl restart <APP_NAME>
8. curl http://127.0.0.1:<APP_PORT>/healthz
9. 失败则切回 previous 并重启
```

可以先只写部署文档，再实现完整 hook。不要把数据库迁移、数据导入、素材处理等复杂流程塞进同一个 hook，除非项目确实已经有稳定脚本。

## 5. 示例 post-receive hook

在服务器创建裸仓库：

```bash
sudo -u deploy git init --bare /srv/git/<APP_NAME>.git
```

写入 `/srv/git/<APP_NAME>.git/hooks/post-receive`：

```bash
#!/usr/bin/env bash
set -euo pipefail

APP_NAME="<APP_NAME>"
APP_BINARY="<APP_BINARY>"
BUILD_TARGET="<BUILD_TARGET>"
APP_PORT="<APP_PORT>"
SERVICE_NAME="<APP_NAME>"

GIT_DIR="/srv/git/${APP_NAME}.git"
BUILD_DIR="/srv/build/${APP_NAME}"
APP_DIR="/srv/apps/${APP_NAME}"
RELEASES_DIR="${APP_DIR}/releases"
SHARED_DIR="${APP_DIR}/shared"

mkdir -p "${BUILD_DIR}" "${RELEASES_DIR}" "${SHARED_DIR}/data"

while read -r oldrev newrev refname; do
  if [[ "${refname}" != "refs/heads/main" ]]; then
    echo "Skip ${refname}; only main deploys."
    exit 0
  fi
done

git --work-tree="${BUILD_DIR}" --git-dir="${GIT_DIR}" checkout -f main

cd "${BUILD_DIR}"
go build -o "${APP_BINARY}" "${BUILD_TARGET}"

release_id="$(date +%Y%m%d-%H%M%S)"
new_release="${RELEASES_DIR}/${release_id}"
mkdir -p "${new_release}"
cp "${APP_BINARY}" "${new_release}/${APP_BINARY}"

if [[ -L "${APP_DIR}/current" ]]; then
  old_target="$(readlink -f "${APP_DIR}/current")"
  ln -sfn "${old_target}" "${APP_DIR}/previous"
fi

ln -sfn "${new_release}" "${APP_DIR}/current"

if ! sudo systemctl restart "${SERVICE_NAME}"; then
  echo "systemctl restart failed"
  exit 1
fi

if ! curl -fsS "http://127.0.0.1:${APP_PORT}/healthz" >/dev/null; then
  echo "Health check failed, rolling back."

  if [[ -L "${APP_DIR}/previous" ]]; then
    rollback_target="$(readlink -f "${APP_DIR}/previous")"
    ln -sfn "${rollback_target}" "${APP_DIR}/current"
    sudo systemctl restart "${SERVICE_NAME}"
  fi

  rm -rf "${new_release}"
  exit 1
fi

echo "Release deployed: ${new_release}"
```

设置权限：

```bash
chmod +x /srv/git/<APP_NAME>.git/hooks/post-receive
```

如果 hook 由 `deploy` 用户执行，需要允许它重启指定服务。示例 sudoers：

```text
deploy ALL=NOPASSWD: /usr/bin/systemctl restart <APP_NAME>, /usr/bin/systemctl status <APP_NAME>
```

## 6. SQLite/WAL 设置

如果目标项目使用 SQLite，初始化连接后保持这些 PRAGMA：

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
```

要求：

- 不要因为部署改造引入 PostgreSQL。
- 不要因为部署改造引入 ORM。
- 继续使用项目已有的 SQLite 驱动和 `database/sql` 结构，除非项目已经有明确不同的技术选型。

## 7. 运行期数据统一放到 shared

线上所有运行期数据必须放在：

```text
/srv/apps/<APP_NAME>/shared/
```

包括：

```text
app.db
app.db-wal
app.db-shm
data/
config.env
上传文件、缓存文件、生成文件
```

不要让运行期数据写入 `current` 或 `releases`。

代码层面需要确保：

- systemd 的 `WorkingDirectory` 是 `/srv/apps/<APP_NAME>/shared`。
- 默认 `<ENV_PREFIX>_DB_PATH=app.db` 时，数据库会落在 `shared/app.db`。
- 默认 `<ENV_PREFIX>_DATA_DIR=data` 时，素材或上传文件会落在 `shared/data/...`。
- release 目录只存二进制和只读静态资源。

## 8. 本地开发保持简单

本地开发不要引入 Caddy 和 systemd。

本地启动方式保持：

```powershell
C:\tools\go\bin\go.exe run <BUILD_TARGET>
```

或如果 PATH 里有 Go：

```bash
go run <BUILD_TARGET>
```

本地访问：

```text
http://127.0.0.1:<APP_PORT>
```

本地运行期数据可以继续放在仓库根目录：

```text
app.db
data/
```

这些文件必须加入 `.gitignore`，不要提交。

## 9. 外部命令和大文件处理

如果目标项目仓库里有 `.exe`、下载器、模型文件、临时数据、数据库文件或大素材，需要逐项判断：

- Windows 本地开发必须依赖的小工具，可以保留，但文档要说明只用于本地。
- Linux 线上统一使用系统路径，例如 `/usr/local/bin/<tool>`。
- 如果决定不纳入版本控制，先确认不会影响本地开发，再加入 `.gitignore` 并从 Git 追踪中移除。
- 不要误删用户仍然依赖的文件。

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
C:\tools\go\bin\go.exe run <BUILD_TARGET>
```

检查：

```text
http://127.0.0.1:<APP_PORT>/healthz
http://127.0.0.1:<APP_PORT>/
```

如果项目有登录页或核心页面，也一起检查。

## 11. 不要做的事

本次部署改造不要：

- 不要迁移到 Docker。
- 不要引入 Nginx。
- 不要引入 PostgreSQL/RDS。
- 不要引入 Redis。
- 不要改成前后端分离。
- 不要大规模重构模板系统或业务逻辑。
- 不要移动项目到 `go-sites` 仓库。
- 不要删除用户数据文件。
- 不要提交 `app.db`、`app.db-wal`、`app.db-shm`、`data/`、`config.env`。

## 12. 交付清单

目标项目改造完成后，应能回答：

- `/healthz` 是否可公开访问并返回 200？
- 监听地址、数据库路径、数据目录是否都能从环境变量配置？
- systemd 是否从 `/srv/apps/<APP_NAME>/current/<APP_BINARY>` 启动？
- systemd 的 `WorkingDirectory` 是否是 `/srv/apps/<APP_NAME>/shared`？
- Caddy 是否反代到 `127.0.0.1:<APP_PORT>`？
- `git push prod main` 是否能构建、发布、重启、健康检查和失败回滚？
- SQLite 数据和上传文件是否只写入 `shared`？
- 本地开发是否仍然可以 `go run <BUILD_TARGET>`？
