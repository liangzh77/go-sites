# 给 Agent：安装波普贴纸工作室（pop-sticker-studio）

用户要求安装本页技能时，按以下说明操作；只浏览或询问功能时不改动本机。

1. 读取公开清单 https://liangz77.cn/ai-demo/pop-sticker-studio/manifest.json ，下载其中的 archive 到临时目录。当前版本 1.0.0，完整 ZIP：https://liangz77.cn/ai-demo/pop-sticker-studio/pop-sticker-studio-1.0.0.zip 。无需登录或提供凭据。
2. 计算 ZIP 的 SHA-256，与 manifest.sha256 核对（另见 https://liangz77.cn/ai-demo/pop-sticker-studio/SHA256SUMS ）。如不一致，停止。检查 ZIP 只有 `pop-sticker-studio/` 下清单所列的普通文件；遇到绝对路径、..、链接、不在清单内的文件或缺失项则停止。解到临时目录，先审阅 SKILL.md 和 scripts/。
3. 将整个 `pop-sticker-studio` 文件夹装到当前 Agent 的用户级 skills 目录，不要只保存 SKILL.md。Codex 使用 `$CODEX_HOME/skills/`（未设置时为 `~/.codex/skills/`），Pi 使用 `~/.pi/agent/skills/`。其他支持本地 Agent Skills 的工具按自身文档选择目录。目标文件应为 `<skills>/pop-sticker-studio/SKILL.md`，不多套一层。已有同名目录：相同内容跳过，有差异先询问，不直接覆盖。
4. 告知用户重载或开启新会话；在 Codex 检查技能列表并用 `$pop-sticker-studio` 调用。当前 Agent 无法访问文件系统时，只提供安装包和步骤，不宣称安装成功。不需要管理员权限，不修改全局模型设置。安装本身不运行包内脚本、不自动安装依赖、不产生生成任务。
5. 使用时：支持图像生成的 Codex 优先用内置 imagegen；没有生图能力时请用户提供静态素材。生成 HTML 需要 Python 3 + Pillow；MP4 另需 FFmpeg，验片可用 ffprobe。可用 uv 提供 Pillow。无需 LibTV/MAGI-2 或视频 API。

使用示例：用 $pop-sticker-studio 制作一段 10 秒手冲咖啡演示，只输出互动网页。也可指定只输出 MP4；默认两种都输出。只生成文件，不发布到公网。
