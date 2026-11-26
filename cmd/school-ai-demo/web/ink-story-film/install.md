# 安装水墨视频（ink-story-film）
用户要求安装时，从 https://liangz77.cn/ai-demo/ink-story-film/manifest.json 获取同域版本化 ZIP 的下载地址与 SHA-256。只浏览或观看作品时无需安装。
1. 下载 ZIP 到临时目录，计算 SHA-256 并与清单核对。不一致则停止。先检查压缩包仅含清单内的普通文件、路径均位于 ink-story-film/ 下；拒绝绝对路径、.. 和符号链接。解压后审阅 SKILL.md。
2. 将整个 ink-story-film 文件夹复制到当前 Agent 的用户级技能目录。Codex 为 $CODEX_HOME/skills/（默认 ~/.codex/skills/），Pi 为 ~/.pi/agent/skills/。遇同名但不同内容的安装先询问，勿直接覆盖。若存在旧名 still-story-film，提示用户这是更名，不自行删除旧版或其工程。
3. 重载或新开会话，确认技能列表里有 ink-story-film，然后调用 $ink-story-film。安装无须管理员权限、不执行生成任务、不读取账户凭据。只有在制作时才需要生图、语音工具以及 FFmpeg。
