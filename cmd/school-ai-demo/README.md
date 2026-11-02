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
| GET | `/api/state` | templates, recent fills, whether a key is configured |
| GET | `/api/templates/{slug}` | one template: its fields and its bound HTML |
| POST | `/api/fill` | material + template → a session of filled values |
| GET | `/api/sessions/{id}` | one session |
| PATCH | `/api/sessions/{id}` | save values the user edited by hand |
| GET | `/api/sessions/{id}/xlsx` | the filled form as a spreadsheet |
| GET | `/health` | liveness, model name, key readiness |

`POST /api/fill` takes either `multipart/form-data` (`template`, `file` or
`text`) or a JSON body (`{"template": "...", "text": "..."}`).

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `SCHOOL_AI_DEMO_ADDR` | `127.0.0.1:9025` | Caddy proxies to this; keep it on loopback |
| `SCHOOL_AI_DEMO_BASE_PATH` | `/school-ai-demo` | must match the Caddy `handle` path |
| `SCHOOL_AI_DEMO_DATA_DIR` | `./school-ai-demo-data` | SQLite (WAL) lives here |
| `SCHOOL_AI_DEEPSEEK_API_KEY` | *(empty)* | without it the page works and says AI filling is off |
| `SCHOOL_AI_DEEPSEEK_BASE_URL` | `https://api.deepseek.com/chat/completions` | for a proxy or a test double |
| `SCHOOL_AI_DEEPSEEK_MODEL` | `deepseek-chat` | |

A missing key is not an error state the service hides. `/api/state` reports
`ai.configured: false`, the page shows a notice naming the variable, and
`/api/fill` answers `503` with `{"code": "no_key", "error": "..."}` — never a
500 and never a blank page.

## Running it

```sh
go build -o school-ai-demo ./cmd/school-ai-demo
SCHOOL_AI_DEEPSEEK_API_KEY=sk-... ./school-ai-demo
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
