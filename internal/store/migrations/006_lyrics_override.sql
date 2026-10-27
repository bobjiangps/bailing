-- 006: lyrics picked by hand (manual search) for online songs, which have
-- no row in tracks. key = lower(title) \0 lower(artist) \0 lower(album).
-- Local tracks keep their manual pick in lyrics_cache (source = 'manual').

CREATE TABLE lyrics_override (
    key        TEXT PRIMARY KEY,
    status     TEXT NOT NULL,               -- found | instrumental
    body       TEXT NOT NULL DEFAULT '',
    lrclib_id  INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);

