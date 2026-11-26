# AI Demo 制作栏目

- 目录：`/ai-demo/#production`；稳定的分享/安装入口：`/ai-demo/pop-sticker-studio/`。首页 HTML 有普通链接，即使 Agent 不运行 JS 也能发现详情页。
- 独立互动案例：`/ai-demo/pop-sticker-studio/steak/`。从 bistro-steak 的已验收源复制 HTML 与运行时 assets，保留三层上滑引导、锅内舀油动作、配乐和完整食谱，仅增加「技能介绍」返回链接。没有跳转、嵌入或借用旧 demo 的资源。
- 详情页设计参考 Kimi K3：暖白卡片、牛排样例、复制 URL 主操作、原生链接安装和 ZIP 下载；无 JS 时仍能读取安装用途、网址与用法。浏览器数据仅用于按需复制，不调用模型或生成服务。
- `install.md` 为纯文本说明；`manifest.json` 指向同域固定版本 ZIP 和 SHA-256；下载内容只由 `package_pop_sticker.py` 允许的 12 个技能文件组成，不含本机设置。版本内容变化应提升版本号并更新详情页、安装文档与历史包保留策略，避免覆盖旧下载链接。
- 打包：`python3 -B cmd/school-ai-demo/package_pop_sticker.py /path/to/pop-sticker-studio`。测试：`python3 -B -m unittest discover -s cmd/school-ai-demo -p 'test_*.py'`。
- 此变更只需更新静态 release，不需改 Caddy、导航或重启服务。回滚：仅在 current 仍指向本次 release 时，原子切回 backups 下同名目录的 previous-target。
