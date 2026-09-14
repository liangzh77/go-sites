# Chick Chick 本站托管

- 上游私有仓库：https://github.com/mzhangdd-pixel/Chick_Chick
- 导入提交：003bf4553471140a2e2ec680988272c4218575c4
- 来源：本机已有 `Chick_Chick-main.zip`，ZIP 注释中的提交编号与登录后的 GitHub 最新提交一致。
- 公开游戏地址：https://liangz77.cn/mu/chick-chick/
- 源码在本目录；构建产物在 `apps/works/chick-chick/`。源码目录不随站点发布。

运行 `npm ci`、`npm run build`、`npx tsc --noEmit`，将 `dist/` 内容复制到 `../../apps/works/chick-chick/`，再运行根目录 `deploy.ps1`。

Vite base 固定为 `/mu/chick-chick/`，字体使用本地 Fredoka Variable 并附带 OFL 许可证。移除上游模板里未被游戏使用的 Gemini 环境变量注入。Three.js 场景、音效、角色和玩法沿用上游实现；金币、皮肤和最高分保存在浏览器 localStorage。

作品在 `data.js` 的「张牧作品」中注册，在 `portfolio.js` 中按第一位展示，封面在 `apps/works/previews/chick-chick.png`。发布后检查游戏启动、键盘与触屏按钮、皮肤商店、所有资源和作品集首位入口。
