# 数学判卷 Demo（静态示例 + 实时判卷，均已上线）

2026-11-24 已上线：<https://liangz77.cn/school-ai-demo/math-review.html>。首页已有分类与入口，`/private` 沿用原 School AI Demo 链接。

2026-11-24 已上线：两组静态示例 + 上传/粘贴/拖入试卷的实时 AI 判卷；提示词在实时区输入框内编辑（顶部不再有共用提示词卡片），示例图片可一键放入实时区，示例卡片各自显示提示词。上传区不再要求遮盖提醒或发送同意勾选。实时链路复用 DeepSeek companion，与作文共用每日 50 次额度。详见 [数学实时判卷](../school-essay-demo/math-live.md)。当前工作区修改尚未提交或推送 GitHub。

## 文件与内容

- 页面：`web/math-review.html`；线上路径：`/school-ai-demo/math-review.html`。
- 新资源：`web/math/math.css`、`math.js`、`example-01.png`、`example-02.png`。
- 复用现有 `web/essay/essay.css` 的基础视觉样式；不加载 `essay.js` 或首页 `app.js`。
- 首页 `web/app.js` 新增「数学判卷」分类、卡片和链接。保留填表、教案、作文的原有行为。
- 设计由 `kimi-k3` 提供，见 `docs/school-math-design.md`；实际实现采用独立 JS 文件和原生 `<dialog>`。

提示词逐字保留：`作为初一数学老师，判卷`。

图 1 表格为 6 行，标题 `判卷结果（满分 30 分，每题 5 分）`；图 2 为 4 行，标题 `判卷：全对，100 分`。题号、判定、得分、批注、符号及数字均按用户给定的 AI 输出保留，没有重新判卷、补总分或统一评分尺度。页面明确标注预置输出，并提醒教师核对原题和评分标准，不将其视为标准答案。

图片保留原始上传字节：

| 文件 | 尺寸 | SHA-256 |
| --- | --- | --- |
| `example-01.png` | 500×794 | `b0d855d21d3106f67b04c2b1f0297e2c4dde50c8afe674e53c56d143469e254d` |
| `example-02.png` | 1080×1580 | `487aeebc0bb48fefcef61e68828d5758a27daaa878339b8f4648e2248913f38a` |

## 交互与本地预览

- 每组表格可复制为带原标题的 Markdown；示例图下的按钮可把该图放入实时判卷区（不自动提交）；示例卡片各自显示提示词。
- “选择试卷图片”是原生 `<label for>`，由浏览器打开系统文件选择器（不依赖脚本 `input.click()`）；键盘 Enter/空格同样可触发。
- 原图等比显示，支持灯箱、Esc/按钮/遮罩关闭、焦点返回及点开示例图。
- 桌面左右对照，手机上下排列；宽表格仅在局部滚动，出现左右滑动提示。
- 示例图片、提示词和判卷表格不依赖 JavaScript 渲染；查看/复制示例不请求模型。新版实时区需 JavaScript，只在点击提交后调用模型。

仓库根目录运行：

```sh
python3 -m http.server 9027 --bind 127.0.0.1 --directory cmd/school-ai-demo/web
```

打开 `http://127.0.0.1:9027/math-review.html`，此方式只预览静态内容，不能调用实时 API；实时预览使用 [math-live.md](../school-essay-demo/math-live.md) 中的 companion + 开发代理。注意：当前 checkout 的首页 HTML/CSS 比线上旧，因此这条命令用于预览新数学页面，不代表已完成首页资产的全量恢复。

## 验证

```sh
go test ./...
go build -o /tmp/school-ai-demo-build-check ./cmd/school-ai-demo
node --check cmd/school-ai-demo/web/math/math.js
node cmd/school-ai-demo/math-browser-test.cjs
```

浏览器测试使用 Node 22+ 原生 WebSocket 和独立临时 Chrome profile，无 npm 依赖、无模型调用。默认 macOS Chrome；可通过 `CHROME_BIN` 指定其他 Chrome。

覆盖 10 行表格/标题的逐字比对、提示词与 Markdown 剪贴板、复制降级/失败提示、灯箱关闭/焦点恢复、320/390px 布局和表格滚动、禁用 JS 后内容可见、无 API 请求及 JS 异常。截图写入系统临时目录 `school-math-desktop.png`、`school-math-mobile.png`。

首页 DOM 测试可额外使用当前线上首页的只读 HTML 快照，加载本地新 `app.js` 与 mock `/api/state`（不触发模型）：

```sh
curl -fsS https://liangz77.cn/school-ai-demo/ -o /tmp/school-ai-home-fixture.html
SCHOOL_HOME_FIXTURE=/tmp/school-ai-home-fixture.html node cmd/school-ai-demo/math-browser-test.cjs
```

2026-11-24：以上测试（含首页四张卡片、数学分类筛选和跳转）通过；作文浏览器回归测试通过；两张原图哈希一致。Go 构建仅作校验，未发布新二进制。

## 当前发布记录

- 静态 release：`/srv/apps/school-ai-demo/page-overrides/releases/20261124-111138-math`。
- 上一版 `20261124-103100-essay` 完整保留；原有 45 个教案/作文资源逐字节未变，只更新 `app.js` 并加入 5 个数学页面/资源文件。
- 备份与来源记录：`/srv/apps/school-ai-demo/page-overrides/backups/20261124-111138-math/`，包含 `site-before.caddy`、`main-before.caddy`、`apply.sh`、`payload.tgz`、`release.json` 和 `SHA256SUMS`。
- 从审核后的独立文件快照发布；基线 commit `4b4e0b7bc59fb81682359e3205d9d46b22a9baf2`，此次工作区变更尚未提交。没有发布 keys 等无关改动。
- 公开 HTTPS 浏览器验收通过：全部 10 行表格/标题、原提示词与 Markdown 复制、复制降级、灯箱三种关闭方式与焦点恢复、无 JS 静态内容、320/390px 布局、首页分类/卡片跳转、无数学 API 请求。
- 51 个公开资产的 SHA-256 全部匹配本地，包含原 PPTX、36 张课件预览和作文资源；填表/作文 API 健康正常。四个现有应用进程 PID 与启动时间未变，作文剩余额度保持 48 次。

## 后续发布与回滚

以下为历史静态版发布方案。**实时升级已按 [math-live.md](../school-essay-demo/math-live.md) 发布，涉及 companion 二进制和数学 API 路由，不能只发布静态文件。**

仅更新预置静态内容时，使用静态 overlay 增量发布，不重发 Go 二进制：

1. 复制当前完整静态 release（目前为 `20261124-111138-math`）到新目录；加入数学页面、`math/*`，更新 `app.js`。保留旧目录和所有既有资产。
2. 在 Caddy `@school_ai_static` 中加入 `/school-ai-demo/math-review.html`、`/school-ai-demo/math/*`，并在 `@school_ai_app` 中显式排除同样路径。保留作文 API matcher 和其他服务配置不变。
3. 备份、validate、reload，验证首页数学入口、两组原图/表格、静态资源哈希、旧教案与作文功能；同步仓库根 Caddyfile 和发布状态。
4. 失败恢复本次 Caddy 备份，检查旧页面；不要覆盖或恢复应用数据库。现有 `/private` 的 School AI Demo 入口无需重复添加。

回滚本次数学上线：先确认 Caddy 没有后续变更，再将本次 `site-before.caddy` 恢复到 `/etc/caddy/sites-enabled/liangz77.cn.caddy`，执行 `caddy validate --config /etc/caddy/Caddyfile` 和 `systemctl reload caddy`。无需停止或重启任何应用服务。保留所有 release 与数据库，同步回退仓库中本次的 Caddy matcher/root 变更。若已有他人后续配置变更，只定向撤销数学路由，不整文件覆盖。
