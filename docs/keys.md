# API 密钥管理

`/keys/` 是一个独立的单用户 Go 服务，用于保存和管理不同厂商、入口与账号体系下的 API Key、模型参数和手动检测记录。服务代码在 `cmd/keys/`，数据库与主密钥不随代码发布覆盖。

## 当前线上状态

- 页面：`https://liangz77.cn/keys/`
- 首次线上 release：`20261110-185644`
- 服务：`go-sites-keys.service`，监听 `127.0.0.1:9020`
- 数据库：`/srv/apps/go-sites-keys/shared/data/keys.db`
- 主密钥：`/srv/secrets/liangz77-keys/master.key`，与数据库目录分开保存
- 首次一致性备份已验证：SQLite integrity 为 `ok`

## 已实现范围

- 一个 Provider 保存多把 Key；同一厂商可建立多个独立 Provider。
- Provider、Key、模型配置的增删改查。
- 搜索 Provider 名称、Key 别名、备注、模型，按 Provider、检测状态、即将到期、停用筛选。
- 单个 Key、单个 Provider、全部启用 Key 的手动真实检测。
- 检测状态、HTTP 状态、耗时、模型与历史记录；配置变化后旧结果标记为过期。
- 预计到期日、提前提醒天数、购买日期和备注。
- 模型 ID、上下文、最大输出、思考能力、思考等级 / 预算、额外测试参数、默认测试模型。
- 从支持模型列表接口的 Provider 获取模型 ID；OpenCode Go 还会合并 Models.dev 官方目录中的上下文、最大输出、思考模式和附加能力。
- OpenCode Go 创建 Provider 后自动导入全部当前模型；已有 Provider 可点击“同步模型”全量刷新，不需要逐个手工填写。
- 火山方舟保存 Key 后自动调用 `GET /api/v3/models`，只导入支持 Chat Completions 的模型及参数。
- 测试模型字段始终允许直接输入 Model ID 或 Endpoint ID；同步失败或使用自定义接入点时不会被空下拉框阻塞。
- AES-256-GCM 加密落库、列表脱敏、重新验证后临时查看完整 Key。
- 服务端 bcrypt 密码校验、HttpOnly / SameSite=Strict 会话、CSRF 校验和敏感响应禁缓存。
- SQLite WAL、单文件备份命令、systemd 单元和部署 / 回滚入口。

第一版不包含聊天界面、请求中转、自动轮询、自动续费、外部通知或多用户协作。

## 安全边界

### 会话与密码

管理密码只以 bcrypt 哈希存在服务端。浏览器页面不读取、不保存密码，也不通过静态 `site-config.js` 判断权限。登录 cookie 为 HttpOnly、Secure、SameSite=Strict，服务端每次 API 请求都会重新验签。

所有修改操作还必须提交页面临时会话中的 CSRF token。完整 Key 查看接口会再次校验管理密码；完整 Key 不会写入 URL、日志、数据库普通字段或浏览器持久存储。

### 密钥加密

每把 API Key 使用 AES-256-GCM 加密，密文与 Key 记录 ID 绑定。缺少或无法解析 `KEYS_MASTER_KEY` / `KEYS_MASTER_KEY_FILE` 时服务拒绝启动。

数据库不包含可直接使用的明文 Key。列表接口只读取保存时生成的脱敏串；只有单条查看、检测和模型发现会解密对应记录。

### 模型目录自动导入

OpenCode Go 的推理模型接口 `GET /inference/go/v1/models` 提供当前 Key 可用的模型 ID，但参数并不在该响应中。服务端会同时读取 OpenCode 官方 Models.dev 目录：

```text
https://models.dev/api.json?type=all
```

目录中 `opencode-go` 的模型会与当前 Key 可见 ID 求交集，并自动写入：

- 上下文上限和最大输出。
- 是否支持思考。
- effort / toggle / budget_tokens 等思考模式。
- 工具调用、结构化输出、附件、Temperature、官方名称和描述等扩展元数据。

已手工覆盖且不为空的参数不会被官方目录覆盖；未知字段会由目录补齐。重复同步是幂等的，不会重复创建模型。

### OpenCode Go 额度

每次检测 OpenCode Go Key 后，服务还会使用同一把 Key 调用：

```text
GET https://opencode.ai/console/api/go/status
```

只保存 `access.meters` 中的 5 小时、周、月三组额度：

- 已用金额、总额度和剩余金额。
- 使用百分比及是否触顶。
- 重置时间与最后更新时间。

不会保存接口返回的账号 ID、支付方式、续费状态等无关字段。Key 需要具备 `Usage: Read` 权限；额度读取失败不会把已经成功的模型检测改成失败，而是保留上次成功额度并显示错误。

在 OpenCode Go Key 名称上悬停或键盘聚焦，会显示三组额度的进度和重置时间；移开或失焦后立即消失。

### 导出配置文本

页面顶部的“导出配置”支持按 Provider 或具体 Key 勾选。生成前必须重新输入管理密码，服务端会返回一份 `.env` 风格的纯文本：

```text
VOLCENGINE_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
VOLCENGINE_PROTOCOL=openai_chat
VOLCENGINE_AUTH_MODE=bearer
VOLCENGINE_MODEL=doubao-example
VOLCENGINE_API_KEY=ep-or-key
```

同一 Provider 下导出多个 Key 时，变量前缀会自动追加 Key 别名并去重。包含空格、`#` 等字符的值会自动加引号。完整密钥只出现在重新验证后的响应中，不写入浏览器持久存储；关闭弹窗会立即清空文本和密码。

### 自定义 Provider

- 地址必须是 HTTPS，不能包含用户名、密码、查询参数或片段。
- 发起请求时再次解析 DNS，并直接连接已经验证的公网 IP，避免“检查公网、请求时改指内网”的 DNS 重绑定。
- loopback、私网、链路本地、CGNAT、文档网段、NAT64 和常见 IPv4 嵌入 IPv6 网段均被拒绝。
- 不读取系统代理环境变量。
- 阻止所有 HTTP 重定向，避免认证头随跳转发送到其他主机。
- TLS 最低版本为 1.2，响应最多读取 64 KiB，单次请求超时 25 秒。
- 同一批次最多检测 64 个 Key，服务端整体并发上限为 4，批次工作协程为 3。

这一层只允许探测公网 HTTPS 地址，不提供任意端口扫描或内网服务探测能力。

### 与用户演示页隔离

`/demo/*` 可能运行用户上传的 HTML 和脚本。密钥服务通过 `Content-Security-Policy: sandbox allow-scripts …` 为演示页创建不带 `allow-same-origin` 的 opaque origin。演示脚本仍可运行，但不能读取本站 cookie、localStorage 或同源 `/keys` 响应。

该隔离会带来预期限制：演示页不能使用本站持久存储、Service Worker 或同源接口。需要账号数据的应用应作为独立产品部署，而不是放进公开演示目录。

## Provider 支持状态

协议的请求体、认证头和常见错误分类已有自动化测试。没有经过授权测试 Key 的厂商一律只算“协议测试通过，待真实联调”，不能据此宣称线上可用。

| Provider | 预设地址 | 协议 | 模型发现 | 验证状态 |
| --- | --- | --- | --- | --- |
| OpenAI | `https://api.openai.com/v1` | Chat / Responses | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Anthropic | `https://api.anthropic.com/v1` | Messages | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Google Gemini | `https://generativelanguage.googleapis.com/v1beta` | GenerateContent | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| xAI | `https://api.x.ai/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Mistral | `https://api.mistral.ai/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| DeepSeek | `https://api.deepseek.com` | OpenAI Chat / Responses | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| 阿里云百炼 | `https://dashscope.aliyuncs.com/compatible-mode/v1` | OpenAI Chat | 否 | 官方兼容协议核对，自动协议测试通过，待真实联调 |
| 火山方舟 | `https://ark.cn-beijing.volces.com/api/v3` | OpenAI Chat | 是 | 官方模型接口已核对；自动导入 Chat 模型参数，同时允许填写 Endpoint ID |
| Moonshot / Kimi | `https://api.moonshot.cn/v1` | OpenAI Chat | 是 | 官方地址核对，自动协议测试通过，待真实联调 |
| 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4` | OpenAI Chat | 是 | 官方地址核对，自动协议测试通过，待真实联调 |
| MiniMax | `https://api.minimax.chat/v1/text` | ChatCompletion v2 | 否 | 专用协议适配，自动协议测试通过，待真实联调；接口变更需重新核对文档 |
| 百度千帆 | `https://qianfan.baidubce.com/v2` | OpenAI Chat | 否 | 预设与通用协议测试通过，上线前需用当前账号文档再次核对认证与模型 ID |
| 腾讯混元 | `https://api.hunyuan.cloud.tencent.com/v1` | OpenAI Chat | 否 | 官方兼容接口核对，自动协议测试通过，待真实联调；迁移到 TokenHub 时修改地址 |
| OpenRouter | `https://openrouter.ai/api/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| OpenCode Go | `https://opencode.ai/inference/go/openai/v1` | OpenAI Chat | 是 | 官方协议核对；自动发送 `x-opencode-session`，自动协议测试通过 |
| 硅基流动 | `https://api.siliconflow.cn/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Groq | `https://api.groq.com/openai/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Together AI | `https://api.together.xyz/v1` | OpenAI Chat | 是 | 官方协议核对，自动协议测试通过，待真实联调 |
| Fireworks | `https://api.fireworks.ai/inference/v1` | OpenAI Chat | 否 | 官方兼容接口核对，自动协议测试通过，待真实联调 |

主要官方文档：

- [OpenAI API reference](https://platform.openai.com/docs/api-reference)
- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [Gemini GenerateContent](https://ai.google.dev/api/generate-content)
- [xAI REST API](https://docs.x.ai/developers/rest-api-reference)
- [Mistral API](https://docs.mistral.ai/api/)
- [DeepSeek API](https://api-docs.deepseek.com/api/deepseek-api/)
- [阿里云百炼 OpenAI 兼容](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope)
- [火山方舟推理 API](https://www.volcengine.com/docs/82379/1399008)
- [Moonshot 开发文档](https://platform.moonshot.cn/docs)
- [智谱 API](https://open.bigmodel.cn/dev/api)
- [MiniMax ChatCompletion](https://platform.minimaxi.com/document/ChatCompletion)
- [百度千帆文档](https://cloud.baidu.com/doc/WENXINWORKSHOP/index.html)
- [腾讯混元 OpenAI 兼容接口](https://cloud.tencent.com/document/product/1729/111007)
- [OpenRouter API](https://openrouter.ai/docs/api-reference/overview)
- [OpenCode Go](https://opencode.ai/docs/console/go/)
- [硅基流动 API](https://docs.siliconflow.cn/)
- [Groq API](https://console.groq.com/docs/api-reference)
- [Together AI Chat](https://docs.together.ai/reference/chat-completions)
- [Fireworks API](https://docs.fireworks.ai/)

Provider 默认地址并不是永久承诺。上线或真实联调前应重新核对厂商当前控制台、地域和入口；同一厂商的不同入口应分别创建 Provider，不能混用地址。

## 本地运行

服务需要三个安全配置：

1. 32 字节主密钥，base64 编码。
2. 至少 32 字符的会话签名密钥。
3. bcrypt 管理密码哈希。

Linux / macOS：

```bash
mkdir -p .local-keys-data .local-keys-secrets
openssl rand -base64 32 > .local-keys-secrets/master.key
openssl rand -base64 48 > .local-keys-secrets/session.key
printf '%s' 'replace-with-a-password' | go run ./cmd/keys -hash-password
```

PowerShell 配置示例：

```powershell
$env:KEYS_DATA_DIR = "$PWD\.local-keys-data"
$env:KEYS_PUBLIC_ORIGIN = "http://127.0.0.1:9020"
$env:KEYS_ADDR = "127.0.0.1:9020"
$env:KEYS_COOKIE_SECURE = "false"
$env:KEYS_MASTER_KEY_FILE = "$PWD\.local-keys-secrets\master.key"
$env:KEYS_SESSION_SECRET_FILE = "$PWD\.local-keys-secrets\session.key"
$env:KEYS_ADMIN_PASSWORD_HASH = '<上一步输出的 bcrypt hash>'
go run ./cmd/keys
```

打开 `http://127.0.0.1:9020/keys/`。生产环境必须使用 HTTPS 和 `KEYS_COOKIE_SECURE=true`。

检查配置但不启动服务：

```bash
./go-sites-keys -check-config
```

## 部署

仓库根目录的 `deploy.ps1` 会：

1. 以 Linux amd64 构建 `cmd/keys`。
2. 上传到 `/srv/apps/go-sites-keys/releases/<时间>/go-sites-keys`。
3. 首次部署时基于现有 `DEMO_ADMIN_PASSWORD` 生成 bcrypt 哈希，不把原始密码写进新配置。
4. 在主数据目录之外生成并保存 `master.key` 与 `session.key`。
5. 创建 `/srv/apps/go-sites-keys/shared/keys.env`，权限 600。
6. 切换 `current`、重启 `go-sites-keys` 并检查 `/keys/healthz`。
7. 健康检查失败时切回 `previous`。

实际生产目录：

```text
/srv/apps/go-sites-keys/
  releases/
  current -> releases/<timestamp>
  previous -> releases/<previous>
  shared/
    keys.env
    data/
      keys.db
      keys.db-wal
      keys.db-shm

/srv/secrets/liangz77-keys/
  master.key
  session.key
```

Caddy 将 `/keys` 永久跳转到 `/keys/`，将 `/keys/*` 反向代理到 `127.0.0.1:9020`。

部署前先完成本地测试和授权。方案中原有“部署需单独确认”的边界仍然有效；仓库落盘不等于线上发布。

### 首次部署前必须完成的一次性迁移

旧版前端曾从公开的 `shared/site-config.js` 读取 `privateArea.password`，旧部署脚本也曾包含固定的 Demo 发布 Key。即使当前代码已经不再使用这些值，也要把它们视为曾公开：

1. 从线上 `shared/site-config.js` 删除整个 `privateArea` 配置。
2. 在服务器私有的 Demo `config.env` 中更换 `DEMO_ADMIN_PASSWORD`，重启 Demo 服务并用新密码登录验证。
3. 生成新的 `DEMO_API_KEY`，更新所有调用发布 API 的外部项目；旧值失效后再删除旧配置。
4. 首次部署密钥服务时，其管理密码哈希会取自已经更换的 `DEMO_ADMIN_PASSWORD`；若 `keys.env` 已存在，则单独生成新的 bcrypt 哈希并更新。

这些操作不修改厂商 API Key，只处理本站在旧版本中曾经公开的本地管理凭据。

## 备份

### 加密主密钥

`master.key` 与数据库分开保存。没有它，数据库中的 Key 无法恢复；泄露它则会失去数据库静态加密的意义。

建议至少保存两份到独立密码库或离线介质，并记录创建日期。不要把 `master.key` 与数据库备份打进同一压缩包。

### SQLite

使用服务自带的一致性备份命令：

```bash
sudo bash /srv/apps/go-sites-keys/current/backup.sh \
  /srv/backups/sqlite/go-sites-keys-$(date +%F).db
```

或直接使用 CLI：

```bash
sudo -u www-data env \
  KEYS_DB_PATH=/srv/apps/go-sites-keys/shared/data/keys.db \
  KEYS_MASTER_KEY_FILE=/srv/secrets/liangz77-keys/master.key \
  /srv/apps/go-sites-keys/current/go-sites-keys \
  -backup /srv/backups/sqlite/go-sites-keys-2026-11-10.db
```

备份命令使用 SQLite `VACUUM INTO`，不需要直接复制正在写入的 `.db`、`.db-wal`、`.db-shm`。目标文件必须不存在。

### 恢复步骤

1. 停止 `go-sites-keys`。
2. 将当前 `keys.db`、`keys.db-wal`、`keys.db-shm` 移到隔离目录，不直接覆盖。
3. 把经过校验的备份复制为 `/srv/apps/go-sites-keys/shared/data/keys.db`。
4. 确认 `master.key` 与备份来自同一加密时期。
5. 修正数据库目录和文件所有者、权限。
6. 启动服务，检查 `/keys/healthz`。
7. 登录后确认 Provider / Key 列表、单条重认证查看和一次授权的测试检测。

如果主密钥丢失或换错，服务可能正常启动但所有 Key 解密失败；不要用新的空数据库继续写入。

## 回滚

```powershell
.\deploy.ps1 -Rollback
```

回滚会同时切换主站和密钥服务的 `current` / `previous`，重启密钥服务并检查健康接口。数据库与主密钥位于 `shared` / `secrets`，不会被 release 切换覆盖。

发布新二进制前不做数据库破坏性迁移；当前 schema 使用 `CREATE TABLE IF NOT EXISTS`。后续若增加不可向后兼容的迁移，必须先把数据库备份和恢复测试纳入发布门禁。

## API 摘要

所有数据接口要求服务端会话；`POST` / `PUT` / `DELETE` 还要求 `X-CSRF-Token`。

```text
GET    /keys/api/session
POST   /keys/api/session
POST   /keys/api/logout

GET    /keys/api/bootstrap
GET    /keys/api/providers
POST   /keys/api/providers
PUT    /keys/api/providers/{id}
DELETE /keys/api/providers/{id}

POST   /keys/api/providers/{id}/keys
PUT    /keys/api/keys/{id}
DELETE /keys/api/keys/{id}

POST   /keys/api/providers/{id}/models
PUT    /keys/api/models/{id}
DELETE /keys/api/models/{id}

POST   /keys/api/keys/{id}/test
POST   /keys/api/providers/{id}/test
POST   /keys/api/test-enabled
POST   /keys/api/keys/{id}/reveal
POST   /keys/api/keys/{id}/discover-models
POST   /keys/api/providers/{id}/discover-models
POST   /keys/api/export
GET    /keys/api/keys/{id}/detections
```

批量检测请求需要 `{"confirm": true}`。Provider 或 Key 停用后不参与批量检测。

## 状态语义

检测状态只代表请求发生时的 Key、地址、协议、模型和参数：

- `success`：调用成功。
- `auth_failed`：认证失败。
- `quota`：余额或额度不足。
- `rate_limited`：限流。
- `model_permission`：模型不存在、Endpoint 不存在或无权限。
- `parameters`：请求参数不兼容。
- `network`：DNS、TLS、超时或公网策略失败。
- `service`：厂商 5xx、接口地址不存在或其他服务异常。
- `unknown`：证据不足，不做扩大解释。

预计到期日与检测状态独立。到期不是厂商失效判据；厂商返回 `unknown` 时也不会被强制归类成 Key 失效。

## 已知边界

- 当前为单用户、单实例服务，不是多租户系统。
- 会话是签名 cookie；退出会清除当前浏览器 cookie，但服务端没有会话列表。管理员密码哈希变化会使旧会话立即失效。
- 目前没有自动轮询、余额查询、外部提醒和自动续费。
- 没有真实测试 Key 时，不能把 Provider 状态改成“已实测”。
- MiniMax、百度千帆等厂商若调整认证或端点，必须在真实联调前更新预设与协议测试。
- 自定义 URL 的 DNS 公网判断依赖运行主机 DNS 和 TCP/IP 栈；安全回归测试覆盖常见内网、元数据与 IPv6 映射形式，但新增特殊云网络时仍需补充网络策略。
