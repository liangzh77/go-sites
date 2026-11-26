# 安装网页 PPT（guizang-ppt-skill）

用户要求安装时，从 https://github.com/op7418/guizang-ppt-skill 取技能源码。只浏览《江雪》成片时无需安装。

1. 先确认用户环境：Codex 的用户级技能目录是 `$CODEX_HOME/skills/`（默认 `~/.codex/skills/`），Pi 是 `~/.pi/agent/skills/`。同名目录已存在且内容不同时先询问，不要直接覆盖。
2. 取源码。推荐浅克隆指定提交，保证内容与本文一致：

   ```bash
   git clone --depth 1 https://github.com/op7418/guizang-ppt-skill.git
   cd guizang-ppt-skill
   git fetch --depth 50 origin 93d21bf4a73a851907a6eb00acbf30618236667a
   git checkout 93d21bf4a73a851907a6eb00acbf30618236667a
   ```

   网络受限时改用仓库当前的 `main`，并在安装后告诉用户实际提交号。
3. 把整个目录复制到第 1 步确认的技能目录，目录名保持 `guizang-ppt-skill`。只复制技能自身文件；不要带上 `.git` 之外的构建产物、`node_modules` 或用户自己的工程。
4. 审阅 `SKILL.md`：它要求开工前在技能目录跑一次 `git fetch` 检查上游更新。保留 `.git` 才能检查；如果复制时去掉了 `.git`，在回复里说明技能会跳过更新检查，用户可用 `git pull` 手动更新。
5. 依赖：只有 Node（渲染模板、校验脚本）。`npm install` 不必要；做图文卡片出图才需要 Playwright，本技能不需要。
6. 重载或新开会话，确认技能列表里有 `guizang-ppt-skill`，然后调用 `$guizang-ppt-skill`。安装脚本不生成演示稿、不读取账户凭据、不向外发送内容。
7. 许可：AGPL-3.0，作者歸藏（<https://github.com/op7418>）。保留仓库里的 LICENSE，不要在衍生分发里删除署名。
