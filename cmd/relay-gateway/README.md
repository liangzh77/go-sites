# relay-gateway

本机（Windows 上的 Paseo / 本地 Pi）与云端 Grok Bot 之间的存储转发中转站。

两边都只主动出网，谁也不用访问对方，所以家里的动态 IP 不影响使用。

## 端点

全部挂在 `RELAY_BASE_PATH` 下（默认 `/relay`）。

| 方法 | 路径 | 谁用 | 说明 |
| --- | --- | --- | --- |
| GET | `/health` | 任何人 | 存活检查，返回排队数，不含任何密钥 |
| POST | `/to-cloud` | 本机 | 把一条消息转发到 Grok Bot 的 webhook，唤醒云端 |
| GET | `/to-local?wait=N` | 本机 | 长轮询领任务，最长等 N 秒（上限 60），204 表示暂时没有 |
| POST | `/ack` | 本机 | 回报任务结果，`status` 取 `done` / `failed` / `blocked` |
| POST | `/from-cloud` | 云端 | 给本机排一条任务 |
| GET | `/result/{id}` | 云端 | 读任务结果 |
| GET | `/queue` | 云端 | 看当前排队情况 |

## 鉴权

两个 Bearer token，必须不同：

- `RELAY_LOCAL_TOKEN`：本机持有。能领任务、能回执、能往云端发消息。
- `RELAY_CLOUD_TOKEN`：云端持有。能排任务、能读结果，**不能**往 webhook 发消息。

这样即使云端这边出问题，也拿不到往你手机上推消息的能力。比较用常量时间，避免时序泄漏。

## 技术栈

Go 标准库 + `modernc.org/sqlite`（纯 Go，无 CGO）。SQLite 以 WAL 模式运行：

- 读（健康检查、队列查看、结果查询）不阻塞写，写入串行化由数据库自己保证，不需要一把全局互斥锁罩住所有请求。
- 单个 `relay.db` 文件，热备只需 `sqlite3 relay.db ".backup ..."`，也省掉了扫目录、逐文件反序列化的开销。
- `synchronous(NORMAL)` + WAL 在掉电时最多丢最后一个事务，对"排队一条任务"这种场景足够；真要更严可以改成 `FULL`。

## 可靠性

- 队列和结果都在 `data/relay.db` 里，事务提交即落盘，进程重启不丢；旧版的文件队列会在启动时自动导入一次。
- 领取带租约：`RELAY_LEASE_SECONDS` 内没回执，任务自动重新可领，本机崩了也不会卡死。
- `/from-cloud` 幂等：同一个 `id` 重复提交只入队一次。
- 队列上限 500 条，满了返回 507 而不是无限堆积。

## 配置

见 `relay.env.example`。生产环境放在 `/srv/apps/relay-gateway/shared/relay.env`（权限 600）。

## 部署

和主站一样走 release 目录：

```
/srv/apps/relay-gateway/
  current -> releases/<时间戳>
  shared/relay.env      # 密钥
  shared/data/          # 队列与结果
```

unit 模板见 `relay-gateway.service`，Caddy 侧只是把 `/relay/*` 反代到 `127.0.0.1:9015`。
