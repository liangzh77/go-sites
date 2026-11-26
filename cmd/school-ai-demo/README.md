# school-ai-demo

Fill a school form from material the user brings: upload an Excel, paste some
text, and the service places what it finds onto the right blanks. It is a demo
of one idea, not a form tool — it knows no form's schema.

Live at <https://liangz77.cn/ai-demo/>.

## AI Demo 统一入口（2026-11-26）

首页恢复原学校站视觉，分类为「学校」「外贸」。外贸三页：
`/ai-demo/customer-email/`、`/ai-demo/quotation/`、`/ai-demo/order-validation/`。
全部是人工编写的业务模拟，不是已交付客户成果，不接业务系统。

发布静态资源为 `web/`；案例素材在 `cases/`，修改后运行
`python3 -B cmd/school-ai-demo/build_cases.py`。测试及最小部署/回滚见
[AI Demo 部署说明](../../docs/ai-demo-deployment.md)。学校后台内部路径继续沿用
`/school-ai-demo`，Caddy 将新公开 API 路径映射到旧进程；旧公开链接只作 308 跳转。


## Why it is built this way

The hard part of this job is not the form. It is that **field names do not
match**. The same child's details arrive labelled 姓名 in one place and 学生姓名
in another; a form asks for 现居地址 and the material says 家庭住址（详细）. A
fixed mapping table is wrong the day the next school sends a slightly different
sheet, and the families filling these in cannot fix it themselves.

So nothing is mapped in code:

1. **Fields come from the HTML.** Every fillable slot in a template is discovered
   by parsing the template document: `contenteditable` cells and spans, plus
   radio groups (which cannot be contenteditable but are still blanks to fill).
   Each field records its `aria-label`, the caption the form puts to its left,
   the column caption above it, and the row-group heading it sits under. Those
   last three are the `hint`, and they are what disambiguate 手机号码1 from
   手机号码2 and row 3 of 家庭成员 from row 4.
2. **The mapping is a model call.** The whole field list (id, label, section,
   hint, options) is sent with the user's material to DeepSeek, which returns
   `{"fields": {"f19": "张伟"}}`. The system prompt is explicit that names need
   semantic matching, that repeated structures map row by row, and that anything
   not literally in the material must be left out.
3. **Nothing is invented.** A value that is unknown, empty, `null`, `未知`, or
   keyed to a field id the template does not have is dropped, and the id is
   reported back as a warning. A blank cell is a correct answer here; a plausible
   wrong one gets printed onto a form a child hands to a school.

Templates are data, not code: drop another annotated HTML file into
`templates/` and it appears in the demo list on the next boot, with its fields
already discovered. The bundled two are the real 学生入学登记表 and 学生信息采集登记表,
kept byte-for-byte in layout and print CSS.

## 「网页 PPT」技能页（已上线）

<https://liangz77.cn/ai-demo/guizang-ppt-skill/>，源码 `web/guizang-ppt-skill/`。
页面讲 `guizang-ppt-skill` 的六步制作流程、示例提示词和安装说明；示例为《江雪》
十页演示稿，静态文件在 `web/guizang-ppt-skill/jiangxue/`。安装说明指向 GitHub
上游（AGPL-3.0），本站不托管技能压缩包。首页卡片在 `web/app.js` 的 `DEMOS`。

## 作文点评（已上线）

新增页面源码为 `web/essay-review.html`，包含两组预置作文图片/点评及
文字、上传、粘贴图片的实时点评入口；首页卡片位于 `web/app.js`。
新 API 独立于本应用运行，复用现有 DeepSeek 配置，避免重建当前线上二进制。
本地实现、测试及最小发布方案见 [school-essay-demo](../school-essay-demo/README.md)。
2026-11-24 已上线：<https://liangz77.cn/ai-demo/essay-review.html>，首页可直接进入。新 API 独立运行于 9026；旧填表进程和数据库未改动。

## 数学判卷（示例 + 实时判卷，已上线）

新增静态页 `web/math-review.html`，展示两张原始数学练习照片和用户提供的
AI 判卷表格，统一提示词为 `作为初一数学老师，判卷`。支持原图放大、
复制提示词/Markdown 表格；首页源码已新增分类和入口。
2026-11-24 已上线：<https://liangz77.cn/ai-demo/math-review.html>。
支持上传/粘贴试卷、DeepSeek 实时分析和逐题结果表格，同时保留两组预置示例。
数学提示词可在页面编辑（默认 `作为初一数学老师，判卷`），示例图可一键放入实时区。
`/api/math-review` 与作文共用 50 次日额度，失败退还；模糊题目标为待确认，
没有明确评分依据时不自行编造总分，结果需老师复核。
内容见 [math-demo.md](math-demo.md)，实时升级与发布方案见
[math-live.md](../school-essay-demo/math-live.md)。

## Endpoints

All under `SCHOOL_AI_DEMO_BASE_PATH` (default `/school-ai-demo`).

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/` | the demo list and the fill screen |
| GET | `/api/state` | templates, recent fills, key readiness, today's quota |
| GET | `/api/templates/{slug}` | one template: its fields and its bound HTML |
| POST | `/api/fill` | material + template → a session of filled values |
| POST | `/api/sessions` | open a session for a hand-filled form (no model call) |
| GET | `/api/sessions/{id}` | one session |
| PATCH | `/api/sessions/{id}` | save values the user edited by hand |
| GET | `/api/sessions/{id}/xlsx` | the filled form as a spreadsheet |
| GET | `/health` | liveness, model name, key readiness, today's quota |

`POST /api/fill` takes either `multipart/form-data` (`template`, `file` or
`text`) or a JSON body (`{"template": "...", "text": "..."}`).

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `SCHOOL_AI_DEMO_ADDR` | `127.0.0.1:9025` | Caddy proxies to this; keep it on loopback |
| `SCHOOL_AI_DEMO_BASE_PATH` | `/school-ai-demo` | internal prefix; Caddy maps public `/ai-demo` APIs onto it |
| `SCHOOL_AI_DEMO_DATA_DIR` | `./school-ai-demo-data` | SQLite (WAL) lives here |
| `SCHOOL_AI_DEEPSEEK_API_KEY` | *(empty)* | primary key; without it the page works and says AI filling is off |
| `SCHOOL_AI_DEEPSEEK_API_KEY_BACKUP` | *(empty)* | optional; used once if the primary answers 401, 402 or 429 |
| `SCHOOL_AI_DEEPSEEK_BASE_URL` | `https://api.deepseek.com/v1` | a **base**, not a full URL: `/chat/completions` is appended |
| `SCHOOL_AI_DEEPSEEK_MODEL` | `deepseek-chat` | |
| `SCHOOL_AI_DAILY_LIMIT` | `100` | AI fills per local day; `0` means unlimited |

`SCHOOL_AI_DAILY_LIMIT` is the daily budget of **AI fills only**. Exporting a
spreadsheet, editing values by hand and listing demos are not counted.

A missing key is not an error state the service hides. `/api/state` reports
`ai.configured: false`, the page shows a notice naming the variable, and
`/api/fill` answers `503` with `{"code": "no_key", "error": "..."}` — never a
500 and never a blank page.

### The daily quota

The count lives in SQLite (`quota_days`: one row per day, `day` primary key,
`used`), so a restart does not hand back the day's budget. Days are cut on
**Asia/Shanghai** midnight, not UTC: the server's clock is UTC, and a teacher
filling a form at 07:00 Beijing time must still be on the same "today" that the
page showed them. `/api/state` and `/health` both carry a `quota` object
(`day`, `used`, `limit`, `remaining`, `unlimited`, `resetsAt`).

A slot is claimed with a single statement, so two requests racing for the last
call cannot both win:

```sql
INSERT INTO quota_days (day, used, updated_at) VALUES (?, 1, ?)
ON CONFLICT(day) DO UPDATE SET used = used + 1, updated_at = excluded.updated_at
WHERE used < ?
```

The claim happens after the material has been read (a corrupt upload cannot
spend a slot) and before the model is called; if the call then fails the slot is
given back. Over the limit the endpoint answers `429`, not `500`:

```json
{"ok": false, "code": "QUOTA_EXCEEDED", "remaining": 0,
 "error": "今日 AI 填表次数已用完（100/100），明天 00:00 重置",
 "quota": {"day": "2026-11-02", "used": 100, "limit": 100, "remaining": 0,
           "unlimited": false, "resetsAt": "2026-11-03T00:00:00+08:00"}}
```

The page shows the remainder in a bar above the demo list and beside the fill
button, and greys the button out at zero.

### Talking to the model

The endpoint is OpenAI-compatible but is not always the DeepSeek API itself —
the deployment uses an OpenCode Go subscription. `SCHOOL_AI_DEEPSEEK_BASE_URL`
is therefore read as a base and `/chat/completions` appended (a base that
already ends in that path is left alone). When the base mentions `opencode`,
every request also carries `x-opencode-session: <uuid>`: the gateway rejects a
request without it (`MissingSessionID`), and one fill reuses one session id
across its retry. `User-Agent: school-ai-demo/1.0` is sent so the gateway can
see who is calling.

`max_tokens` is 4096: this family of models spends tokens on reasoning first,
and a small cap returns an empty `content` with a 200 status. An empty
`content` is reported as "模型没有返回内容，请重试" rather than as a form with
no fields. `aiTimeout` is 90s, because the first request over a cold connection
can take tens of seconds.

Answers are repaired before being rejected. Stripping a ``` fence, the
`"fields{":` shape that has been seen in the wild, smart quotes and trailing
commas all get one more parse; only what survives none of that becomes a
readable error.

## Running it

```sh
go build -o school-ai-demo ./cmd/school-ai-demo
SCHOOL_AI_DEEPSEEK_API_KEY=sk-... SCHOOL_AI_DAILY_LIMIT=100 ./school-ai-demo
```

On the server it runs as a systemd unit on `127.0.0.1:9025`, with the key in
`/srv/apps/school-ai-demo/shared/school-ai-demo.env` (mode 600). Releases live
in `/srv/apps/school-ai-demo/releases/<timestamp>` behind a `current` symlink.

### Caddy route

Keep this route in the repository's root `Caddyfile` as well as the live site
configuration; a later main-site deployment installs that file and otherwise
removes server-only routes. The backend expects the `/school-ai-demo` prefix,
so use `handle`, not `handle_path`. The following is only the basic proxy
illustration: **do not replace the current routes with it**. The root `Caddyfile`
is authoritative and also includes disjoint lesson/essay/math static and essay API
matchers, described below.

```caddyfile
handle /school-ai-demo {
    redir * /school-ai-demo/ permanent
}
handle /school-ai-demo/* {
    reverse_proxy 127.0.0.1:9025
}
```

For a route-only repair, back up the live site configuration, apply only this
route, run `caddy validate --config /etc/caddy/Caddyfile`, then reload Caddy.
Do not run the full `deploy.ps1` or restart the application for this change.
Check the public page, `app.css`, `app.js`, and `/school-ai-demo/health`.
If the backend on port 9025 returns 200 but the public URL returns 404, check
that this route is present and loaded before rebuilding the application.

### Versioned lesson assets

The live binary originally included lesson assets missing from this checkout.
The WorkBuddy lesson page, live `app.js`, lesson CSS and preview script have now
been recovered. The prompt matches the user's supplied wording, including
`45 分钟教案 ppt`. The downloadable file is the unchanged uploaded
`从百草园到三味书屋·第一课时教案.pptx` (18 slides, 1,497,052 bytes), stored as
`web/lesson/baicaoyuan-lesson-plan.pptx`.

SHA-256: `553fc7142a8cc6024611e2d97a741062d84186b095f0a795af74a66b36f9fd92`.

Previews are rendered from this deck using LibreOffice PDF export and PDFKit,
with CJK font fallbacks, then converted to WebP (1600×900 full / 480×270 thumb).
The original PPTX is never re-exported. Preview captions and homepage counts
match all 18 slides. Asset URLs use the deck hash as a cache-busting version.

Caddy serves `lesson-plan.html`, `app.js`, `lesson/*`, `essay-review.html`,
`essay/*`, `math-review.html` and `math/*` from
`/srv/apps/school-ai-demo/page-overrides/releases/20261124-111138-math`.
The original 45 lesson/essay assets are byte-for-byte unchanged in this new release;
`20261124-103100-essay` and `20261124-553fc7142a8c` remain available for rollback.
`/api/essay-review` and its child routes proxy to the companion API on 9026.
All remaining application paths still proxy to the original binary on 9025.
The static, essay API and original application matchers are explicitly disjoint. Never serve the application's `shared`
directory. `Cache-Control: no-cache` keeps browsers from retaining old assets.

For updates, upload a complete new overlay release, verify its files, back up
the live Caddy config, update its versioned root, validate and reload Caddy,
then check the prompt, all previews, and the downloaded PPTX hash. Restore the
previous Caddy config and reload to roll back; previous assets remain intact.
Keep source and root `Caddyfile` in sync. Do not run the full main-site deploy.

Before rebuilding the Go application, reconcile the remaining live homepage
HTML/CSS and other assets missing or older in this checkout. Once all frontend
assets are embedded in a verified release, remove the overlay route and test
the backend-served pages, previews and PPTX download.

## Tests

```sh
go test ./cmd/school-ai-demo/
```

The tests parse both real templates and assert the things that would silently
ruin a filled form: field counts, the 51 and 73 slots; that 家庭成员4手机号码2 exists
and is distinct from row 3's; that column captions from the 曾就读学校 block do not
bleed into 现就读班级 below it; that the original `@page` and print rules survive
annotation; that a model answer with an invented field id keeps only the real
one; and that a missing key produces a readable 503 rather than a crash.

`quota_test.go` covers the day boundary (Shanghai, not UTC), the 101st call
being refused, the count surviving a restart and resetting the next day, and
forty concurrent claims against a limit of ten letting exactly ten through.
`ai_opencode_test.go` covers the session header, the primary/backup switch on
401/402/429 (and no switch on 500), the base-URL join, malformed-JSON repair,
and the empty-content case.

## Choices worth knowing about

- **Radio groups are fields.** 是否独子 and 户籍性质 cannot be `contenteditable`, so
  they are modelled as `kind: "choice"` with their option list. The model is told
  to answer with one of the listed options, and the UI checks the matching radio.
- **Excel is read two ways when it is narrow.** A two-column sheet is either a
  table (header plus records) or a label/value form, and guessing wrong loses
  every value. A narrow sheet is sent labelled under both readings and the model
  picks the one that makes sense. A wide sheet is only ever a table.
- **The export is section/field/value, not the printed layout.** Rebuilding
  merged cells in an `.xlsx` would look unlike the paper form and be useless to
  paste anywhere. The printed PDF path keeps the original layout; the spreadsheet
  is for moving data.
- **Sessions are stored.** Every fill keeps the material it came from, so a
  wrong result can be explained and a template hint improved, rather than being
  a one-off that left no trace.
