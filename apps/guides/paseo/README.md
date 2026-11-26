# Paseo · 口袋里的工作台

纯前端、无依赖的中文交互教程。源码为 `index.html`，发布地址：

- https://liangz77.cn/demo/paseo/

## 内容与设计

参照 `/demo/check-run/` 的章节式介绍方式。内容基于本机 Paseo 桌面版 0.11.4、Tailscale 私网直连、Codex / pi 双 Agent 的使用方式（2026-10-18 核对）。UI 和布局由实际调用的 `moonshotai-cn/kimi-k3` 设计实现，随后进行内容复核和浏览器测试。

页面仅展示使用说明与示例，不连接真实 Agent，不包含账号、密钥、配对信息或真实设备地址。示例 `100.101.102.103` 必须由读者替换。

## 本地预览

在本目录运行：

```sh
python3 -m http.server 8080 --bind 127.0.0.1
```

访问 `http://127.0.0.1:8080/`。也可以直接打开 `index.html`；浏览器禁用剪贴板时可手工复制。

## 发布

使用仓库 `docs/演示管理接口.md` 中的 `POST /api/demos/publish`，发布单文件 HTML，不需要构建或重启站点。

- title：`Paseo 使用指南`
- slug：`paseo`
- feature：`通过 Tailscale 在手机上使用电脑的 Codex 与 pi，按任务选择 DeepSeek、Codex 和 Kimi K3。`

首次发布前检查同名标题和 slug，避免覆盖不相关演示。后续以同名标题更新，保留原 URL。认证信息仅在发布进程中读取，不嵌入 HTML。

## 发布验证（2026-10-18）

- 发布接口返回 `201 Created`；公开演示列表中可见 `Paseo 使用指南`。
- 线上 HTML 与本地源码逐字节一致；SHA-256：`09f2461400c30f45eded825ba86eb327e5966adb651ce3f9019ade09c337b63e`。
- Chrome / Playwright 本地与线上各通过 77 项检查：8 个宽度 × 8 章的布局、深链接与浏览器前进后退、键盘导航、主题保存、剪贴板、禁用存储/剪贴板时的降级、无 JS 阅读、减少动画偏好、无运行时错误与外部依赖。
- 宽度：320、375、390、640、768、960、1024、1440px。实际手机上的系统键盘、Safari 等平台仍需人工体验。
- 本次仅发布单个 demo；未修改或重启本机 Paseo，也未部署站点后端。

## 资料

- https://paseo.sh/docs
- https://paseo.sh/docs/connectivity
- https://paseo.sh/docs/supported-providers
- https://tailscale.com/download
- https://pi.dev
- https://developers.openai.com/codex/cli
