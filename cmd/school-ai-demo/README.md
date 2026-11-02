# school-ai-demo

Fill a school form from material the user brings: upload an Excel, paste some
text, and the service places what it finds onto the right blanks. It is a demo
of one idea, not a form tool — it knows no form's schema.

Live at <https://liangz77.cn/school-ai-demo/>.

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
| `SCHOOL_AI_DEMO_BASE_PATH` | `/school-ai-demo` | must match the Caddy `handle` path |
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
