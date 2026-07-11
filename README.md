# clipsbot

Telegram bot that imports videos forwarded from an admin-controlled channel,
processes them into square watermarked clips, and serves them to the public
via inline query, backed by cached `file_id`s (no re-upload, no reprocessing
per query).

## Two platform constraints that shaped this design

**1. No `getMessages` in the Bot API.** Unlike a Telethon/MTProto user
client, a bot cannot fetch arbitrary historical messages by ID range. It only
ever sees messages as live updates. So `/import` doesn't call Telegram to
"get messages 100–104" — instead, **every message the admin sends is staged
into a local `staged_messages` table as it arrives** (`internal/bot/router.go
→ handleMessage`), and `/import` resolves its range against that local
buffer (`internal/repository/postgres/staging_repository.go → GetRange`).
This means the admin's forwarded videos must arrive while the bot is running
and reachable — if the bot was down when a video was forwarded, it never got
staged, and it won't be included even though its message ID falls in range.

**2. No cached "video note" inline result type.** Telegram's inline query
results support `CachedVideo`, `CachedGif`, `CachedPhoto`, etc., but there is
no `CachedVideoNote`. The circular bubble only exists when a message is sent
via `sendVideoNote` directly into a chat — inline results can't produce it.
This build processes clips into square video and serves them as
`InlineQueryResultCachedVideo`. If the round bubble is a hard requirement,
the alternative is: drop inline delivery, have users `/start` a DM with the
bot, and `sendVideoNote` clips to them on request (happy to build that
variant instead — it's a different public-facing flow, not just a tweak).

## Import flow

```
Admin forwards videos 100, 101, 102, 103 into the bot's private chat
    -> each arrives as a Message update
    -> handleMessage() stages each one into staged_messages (upsert on chat_id+message_id)

Admin replies to message 100 with "/import" (this reply is itself message 104)
    -> handleImportCommand() reads:
         startID = cmd.ReplyToMessage.MessageID   (100)
         endID   = cmd.MessageID                  (104)
    -> ImportService.RunImport(chatID, 100, 104, adminID)
         -> staging.GetRange(chatID, 100, 104)     // local buffer read, not a Telegram API call
         -> creates an `imports` row (status RUNNING)
         -> for each staged message in range:
              - creates an `import_items` row (PENDING)
              - if no video: mark SKIPPED_NO_VIDEO, continue
              - ClipService.ProcessStagedVideo(sm):
                  - idempotency check: clip_sources.source_file_unique_id
                    already exists? -> return existing clip, Skipped=true
                  - else: create `clips` row (NEW) + `clip_sources` row
                    (provenance, recorded BEFORE processing so concurrent
                    duplicate imports also get caught)
                  - download raw file (status -> DOWNLOADING)
                  - ffmpeg: square crop, scale to 640x640, watermark
                    (status -> PROCESSING)
                  - upload processed file to STORAGE_CHAT_ID via sendVideo
                    (status -> UPLOADING)
                  - persist file_id/file_unique_id/dimensions, status -> READY
              - import_items row updated to READY / SKIPPED_DUPLICATE / FAILED
         -> `imports` row finalized with imported/skipped/failed counts
    -> admin gets a summary reply
```

## Idempotency

Two layers:

- **Source-level** (the one that matters for "don't re-import the same
  forward twice"): `clip_sources.source_file_unique_id` has a unique index.
  `ClipService.ProcessStagedVideo` checks this before doing any work. Telegram
  guarantees `file_unique_id` is stable for the same underlying file content
  regardless of how many times or where it's re-forwarded.
- **Item-level** (crash resumption within one import run):
  `import_items (import_id, source_message_id)` is unique with an
  `ON CONFLICT DO UPDATE`, so re-running the same range never creates
  duplicate item rows — it just updates them.

`ImportService.ResumeIncomplete`, called on boot, re-runs any import left in
`RUNNING` status when the process previously died. Because both idempotency
layers hold, this is safe: already-`READY` clips are skipped via the
source-level check, so nothing gets downloaded/processed/uploaded twice.

## Processing version / reprocessing

`clips.processing_version` is stamped from `PROCESSING_VERSION` at import
time. `ClipRepository.ListNeedingReprocess(currentVersion)` finds `READY`
clips below the current version — wire this into a new admin command or
background job when you improve the ffmpeg pipeline; it's intentionally not
triggered automatically, since reprocessing implies re-uploading and you may
want to review before doing that at scale.

## Title extraction

`internal/importer/title.go` takes the first line of the caption, trims
whitespace, and strips a single leading `-`, `—`, or `–`. Both
`original_caption` (untouched) and `clean_title` are stored, per spec.

## Database schema

See `internal/repository/postgres/migrations/0001_init.sql`. Tables:
`clips`, `clip_sources`, `imports`, `import_items`, plus `staged_messages`
(the local buffer described above, not in the original spec's table list but
required to make `/import` work at all against the Bot API).

`clip_sources.provider` is a text column constrained to
`telegram|youtube|tiktok|instagram` — adding a new importer means writing a
new implementation that produces `ClipSource` + raw file data and reuses
`ClipService` for the download/process/upload/persist part; no schema change.

## Package responsibilities

```
cmd/bot/                    wiring only — no business logic
internal/domain/            entities + interfaces (ports). No dependency on
                             anything else in this project.
internal/config/            env var loading
internal/logger/            slog setup
internal/repository/postgres/  domain interface implementations, SQL only
internal/ffmpeg/            domain.VideoProcessor implementation
internal/telegram/          domain.TelegramClient implementation
internal/importer/          title extraction, staged-message construction
                             (pure functions, no I/O)
internal/services/          ClipService (per-clip pipeline), ImportService
                             (range resolution + orchestration) — the only
                             layer that coordinates repositories + adapters
internal/search/            thin layer over ClipRepository.Search, isolated
                             so ranking/engine changes don't touch bot/
internal/bot/                Telegram update loop, admin command handling,
                             inline query handling — the only package
                             (besides internal/telegram) that imports tgbotapi
```

Dependency direction is one-way: `bot` and `repository/postgres` depend on
`domain` and `services`; nothing in `domain` or `services` imports `bot`,
`telegram`, `ffmpeg`, or `postgres` — only their interfaces.

## Error handling strategy

- Per-clip failures (download/ffmpeg/upload) are caught in
  `ClipService.runPipeline`, stored on the clip as `status=FAILED` +
  `failure_reason`, and surfaced per-item in `import_items.error`. One
  failing clip does not abort the batch — `ImportService.RunImport` continues
  to the next staged message.
- Repository methods wrap driver errors with context (`fmt.Errorf("...: %w",
  err)`) so failures are traceable without leaking `pgx` internals into
  callers.
- The admin gets a plain-text summary after every `/import`
  (imported/skipped/failed counts); detailed errors live in
  `import_items.error` and structured logs, not spammed into the chat.

## Suggested libraries (already reflected in go.mod)

- `github.com/go-telegram-bot-api/telegram-bot-api/v5` — Bot API client
- `github.com/jackc/pgx/v5` — Postgres driver + pool
- `log/slog` (stdlib) — structured logging, no external dependency needed
- ffmpeg/ffprobe as external binaries (not a Go dependency)

Deliberately not included: an ORM (repository pattern over raw SQL is more
predictable for this schema size), a job queue library (v1 processes
synchronously within `/import`; if imports get large enough to want
concurrency/retries as background jobs, that's a clean place to introduce
one later behind the existing `ClipService`/`ImportService` boundary without
changing their interfaces).

## Milestones

1. **Schema + repositories** — run the migration, implement/verify the
   Postgres layer against a local DB (included above).
2. **Staging + `/import` skeleton** — get messages landing in
   `staged_messages`, `/import` resolving a range and logging what it would
   process, no ffmpeg yet.
3. **FFmpeg pipeline** — wire `internal/ffmpeg`, tune the watermark
   position/crop for your actual source framing (the `y=h-th-40` offset is a
   starting point, not a guarantee it clears every face position).
4. **Upload + persistence** — confirm `sendVideo` to the storage chat
   returns usable `file_id`s and that inline results actually render.
5. **Public inline search** — verify `plainto_tsquery('russian', ...)`
   ranking is reasonable against your real caption text; may want
   `pg_trgm` fuzzy matching alongside full-text if queries are often
   partial/misspelled.
6. **Crash resumption** — kill the process mid-import, confirm
   `ResumeIncomplete` picks it back up without duplicating anything.
7. **Reprocessing workflow** — add an admin command that calls
   `ListNeedingReprocess` and re-runs the pipeline against existing
   `clip_sources` rows once you bump `PROCESSING_VERSION`.

## Known gaps / things to decide before relying on this in prod

- No rate limiting on `/import` batch size — a very large forward batch
  processes fully synchronously before replying; fine for tens of clips,
  worth chunking or backgrounding for hundreds.
- `internal/ffmpeg`'s `ffprobePath` guess (`ffmpegPath + "probe"`) only works
  if you pass a plain `ffmpeg`/`ffprobe` on PATH or a matching custom pair;
  set both explicitly if your setup differs.
- No tests included — `domain` interfaces make `services` and `importer`
  straightforward to unit test with fakes; that's the next thing I'd add
  before calling this production-ready.
- Could not `go build`/`go mod tidy` this in the sandbox (no access to
  proxy.golang.org), so it's hand-verified for syntax/API shape, not
  compiler-verified. Run `go mod tidy && go build ./...` locally first.
