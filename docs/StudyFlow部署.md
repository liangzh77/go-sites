# StudyFlow 部署记录

2026-10-31 通过本项目的 Caddy 入口和作品集首页接入 StudyFlow。

- 应用：<https://mu-zhang.cn/study-flow/>，不带末尾斜杠的地址会永久跳转。
- 首页：<https://mu-zhang.cn/>，网页分类与全部作品中均有 StudyFlow 卡片。
- 应用源码：<https://github.com/liangzh77/study-flow>；本项目只保存路由和首页入口，不复制应用源码或数据库。

## 运行结构

- Caddy：`Caddyfile` 中 `mu-zhang.cn` 的 `handle_path /study-flow/*`，转发到 `127.0.0.1:8093`。
- systemd：`study-flow.service`，使用专门的 `studyflow` 运行用户。
- 数据与配置：`/srv/apps/study-flow/shared/`。
- 应用版本：`/srv/apps/study-flow/releases/`，`current` 指向当前版本，`previous` 保留上一个成功版本。
- 健康检查：<https://mu-zhang.cn/study-flow/healthz>，会检查数据库。
- 服务器受限部署入口已登记 `study-flow`；在 StudyFlow 仓库提交到 `main` 后运行 `deploy.ps1` 即可发布，自动测试、构建并在健康检查失败时回滚应用。

应用使用 SQLite WAL，发布程序与业务数据库分离。登录 Cookie 设置 Secure、HttpOnly、SameSite=Strict，路径限定为 `/study-flow/`。

## 首页变更

- `data.js`：在「张牧作品」的「网页应用」中增加链接。
- `apps/works/portfolio.js`：卡片介绍、排序和文字封面。
- `apps/works/index.html`：缓存版本参数与无脚本备用链接。

本次首页发布从当前线上版本复制生成，仅替换上述三个文件，并逐文件校验其他已有内容保持一致，随后切换 `current`。没有执行旧版全站部署脚本，以免覆盖其他线上应用。

- 当前站点版本目录：`/srv/sites/liangz77.cn/releases/20261031-124359-portfolio-refresh`。
- 上一个站点目录：`/srv/sites/liangz77.cn/releases/20261031-123424-study-flow`。
- 当前应用版本目录：`/srv/apps/study-flow/releases/20261031-123758-71139776`。

## 验证

Go 集成测试覆盖子路径、健康检查、Cookie 属性及原有账号和持久化行为。云端检查通过：首页卡片跳转、静态资源与 API、注册、任务保存与刷新恢复、管理员登录、重置密码、旧会话撤销及 390px 手机布局。临时验证账号已删除。

服务器通过正式域名检查 HTTPS 和健康接口成功。本地网络直连该域名出现 TLS 连接关闭，浏览器验收使用 SSH 隧道连接实际云端 Caddy，保留正式域名、有效 HTTPS 证书和应用路径。

## 首页调整

2026-10-31 按用户要求从作品清单与无脚本备用链接中移除「王者峡谷」「冲突之塔」两个入口，保留原应用文件。主页共 8 个作品，筛选数量与随机作品同步使用更新后的清单。

StudyFlow 封面改用 `apps/works/previews/study-flow.svg`：沿用应用左上角的绿色勾选图标、StudyFlow 品牌字、英文标语，配浅绿色背景和学习规划文字。桌面与 390px 手机尺寸均已检查。此次仅发布首页文件与封面，没有重启后端或修改数据库。
