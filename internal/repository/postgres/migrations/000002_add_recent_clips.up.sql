CREATE TABLE clip_sends (
    user_id     BIGINT      NOT NULL,
    clip_id     BIGINT      NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
    sent_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    send_count  INTEGER     NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, clip_id)
);

CREATE INDEX idx_clip_sends_sent_at ON clip_sends(user_id, sent_at DESC);