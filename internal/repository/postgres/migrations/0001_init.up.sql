-- 0001_init.sql
-- Run with your migration tool of choice (goose, migrate, etc.) or psql -f.

CREATE TABLE IF NOT EXISTS clips (
    id                       BIGSERIAL PRIMARY KEY,
    title                    TEXT NOT NULL DEFAULT '',
    original_caption         TEXT NOT NULL DEFAULT '',
    clean_title              TEXT NOT NULL DEFAULT '',
    telegram_file_id         TEXT NOT NULL DEFAULT '',
    telegram_unique_file_id  TEXT NOT NULL DEFAULT '',
    duration                 INTEGER NOT NULL DEFAULT 0,
    width                    INTEGER NOT NULL DEFAULT 0,
    height                   INTEGER NOT NULL DEFAULT 0,
    mime_type                TEXT NOT NULL DEFAULT '',
    size                     BIGINT NOT NULL DEFAULT 0,
    processing_version       INTEGER NOT NULL DEFAULT 0,
    status                   TEXT NOT NULL DEFAULT 'NEW'
        CHECK (status IN ('NEW','DOWNLOADING','PROCESSING','UPLOADING','READY','FAILED')),
    storage_chat_id          BIGINT NOT NULL DEFAULT 0,
    storage_message_id       INTEGER NOT NULL DEFAULT 0,
    failure_reason           TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unique only when non-empty: a NEW clip may not have a processed
-- file_unique_id yet. Partial unique index avoids collisions on ''.
CREATE UNIQUE INDEX IF NOT EXISTS idx_clips_unique_file_id
    ON clips (telegram_unique_file_id)
    WHERE telegram_unique_file_id <> '';

CREATE INDEX IF NOT EXISTS idx_clips_status ON clips (status);
CREATE INDEX IF NOT EXISTS idx_clips_processing_version ON clips (processing_version);

-- Full text search over title/caption for inline query search.
ALTER TABLE clips ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('russian', coalesce(clean_title, '')), 'A') ||
        setweight(to_tsvector('russian', coalesce(original_caption, '')), 'B')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_clips_search_vector ON clips USING GIN (search_vector);

CREATE TABLE IF NOT EXISTS clip_sources (
    id                      BIGSERIAL PRIMARY KEY,
    clip_id                 BIGINT NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
    provider                TEXT NOT NULL
        CHECK (provider IN ('telegram','youtube','tiktok','instagram')),
    source_chat_id          BIGINT NOT NULL DEFAULT 0,
    source_message_id       INTEGER NOT NULL DEFAULT 0,
    source_file_id          TEXT NOT NULL DEFAULT '',
    source_file_unique_id   TEXT NOT NULL DEFAULT '',
    source_url              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- This is the true idempotency key: the RAW file's unique id, before any
-- processing. Prevents importing the same forwarded video twice even if it
-- produces a "new" clip row via a race.
CREATE UNIQUE INDEX IF NOT EXISTS idx_clip_sources_unique_file_id
    ON clip_sources (source_file_unique_id)
    WHERE source_file_unique_id <> '';

CREATE INDEX IF NOT EXISTS idx_clip_sources_clip_id ON clip_sources (clip_id);

CREATE TABLE IF NOT EXISTS imports (
    id                BIGSERIAL PRIMARY KEY,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at       TIMESTAMPTZ,
    status            TEXT NOT NULL DEFAULT 'RUNNING'
        CHECK (status IN ('RUNNING','COMPLETED','FAILED')),
    imported_count    INTEGER NOT NULL DEFAULT 0,
    skipped_count     INTEGER NOT NULL DEFAULT 0,
    failed_count      INTEGER NOT NULL DEFAULT 0,
    initiated_by      BIGINT NOT NULL,
    source_chat_id    BIGINT NOT NULL,
    start_message_id  INTEGER NOT NULL,
    end_message_id    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_imports_status ON imports (status);

CREATE TABLE IF NOT EXISTS import_items (
    id                 BIGSERIAL PRIMARY KEY,
    import_id          BIGINT NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
    source_message_id  INTEGER NOT NULL,
    clip_id            BIGINT REFERENCES clips(id) ON DELETE SET NULL,
    status             TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING','PROCESSING','READY','SKIPPED_DUPLICATE','SKIPPED_NO_VIDEO','FAILED')),
    error              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_import_items_import_id ON import_items (import_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_import_items_import_message
    ON import_items (import_id, source_message_id);

-- Local buffer of every message the bot has seen in the admin's chat.
-- This exists because the Bot API cannot fetch arbitrary historical
-- messages by ID — the bot has to have seen them go by as live updates
-- and stashed them itself. /import resolves its ID range against this
-- table, not against Telegram.
CREATE TABLE IF NOT EXISTS staged_messages (
    id              BIGSERIAL PRIMARY KEY,
    chat_id         BIGINT NOT NULL,
    message_id      INTEGER NOT NULL,
    from_user_id    BIGINT NOT NULL,
    has_video       BOOLEAN NOT NULL DEFAULT false,
    file_id         TEXT NOT NULL DEFAULT '',
    file_unique_id  TEXT NOT NULL DEFAULT '',
    caption         TEXT NOT NULL DEFAULT '',
    duration        INTEGER NOT NULL DEFAULT 0,
    width           INTEGER NOT NULL DEFAULT 0,
    height          INTEGER NOT NULL DEFAULT 0,
    mime_type       TEXT NOT NULL DEFAULT '',
    size            BIGINT NOT NULL DEFAULT 0,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_staged_messages_chat_message
    ON staged_messages (chat_id, message_id);
CREATE INDEX IF NOT EXISTS idx_staged_messages_range
    ON staged_messages (chat_id, message_id);
