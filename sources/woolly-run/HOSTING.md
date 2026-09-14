# woolly-run 本站托管

- 上游：https://github.com/mzhangdd-pixel/woolly-run_macaron-adventure
- 导入提交：3112e6ec25fb0db6b9f7c21abd82365a2c7473f9
- 公开地址：https://liangz77.cn/mu/woolly-run/
- 源码保留在本目录；发布产物在 `apps/works/woolly-run/`。
- React、Tailwind 样式和 Varela Round 字体均打包为本站资源。
- 不使用上游模板中的 Gemini 环境变量；游戏无需服务端 API。

在本目录运行 `npm ci`、`npm run build`、`npx tsc --noEmit`，然后将 `dist/` 内容复制到 `../../apps/works/woolly-run/`，提交源码和产物后运行仓库根目录的 `deploy.ps1`。

`Caddyfile` 将 `/mu/*` 映射到 `apps/works/*`，原 `/apps/works/*` 地址永久跳转到 `/mu/*`。不带尾斜杠的目录由文件服务器补齐。导航仍在 `/works`。

部署时排除 `sources/`，避免上传依赖和构建源码。发布前验证游戏启动、设置面板、控制台和静态资源；发布后验证 5 个作品地址、旧地址跳转与导航条目。服务器保留上一个静态发布目录；回退本次路由变更还需恢复上版 Caddyfile 并重新加载 Caddy。

构建配置参考：[Vite 子路径部署](https://vite.dev/guide/build.html#public-base-path)、[Tailwind CSS 3 安装](https://v3.tailwindcss.com/docs/installation)。
