-- 001: initial schema (phase 1)

CREATE TABLE users (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    pw_hash      TEXT NOT NULL,
    is_admin     INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT 'cookie',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    user_agent TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE prefs (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    json    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE albums (
    id             TEXT PRIMARY KEY,
    dir            TEXT NOT NULL UNIQUE,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    date_text      TEXT NOT NULL DEFAULT '',
    cover_photo_id TEXT NOT NULL DEFAULT '',
    photo_count    INTEGER NOT NULL DEFAULT 0,
    color          TEXT NOT NULL DEFAULT '',
    mtime          INTEGER NOT NULL DEFAULT 0,
    sort_mode      TEXT NOT NULL DEFAULT 'name'   -- name | time (from about.txt)
);

CREATE TABLE photos (
    id        TEXT PRIMARY KEY,
    album_id  TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    rel_path  TEXT NOT NULL UNIQUE,
    size      INTEGER NOT NULL,
    mtime     INTEGER NOT NULL,
    width     INTEGER NOT NULL DEFAULT 0,
    height    INTEGER NOT NULL DEFAULT 0,
    taken_at  INTEGER NOT NULL DEFAULT 0,
    color     TEXT NOT NULL DEFAULT '',
    exif_json TEXT NOT NULL DEFAULT '',
    sort_key  TEXT NOT NULL,
    processed INTEGER NOT NULL DEFAULT 0  -- 1 when metadata + s-size thumbnail exist
);
CREATE INDEX idx_photos_album ON photos(album_id, sort_key);

CREATE TABLE tracks (
    id            TEXT PRIMARY KEY,
    rel_path      TEXT NOT NULL UNIQUE,
    folder        TEXT NOT NULL,
    title         TEXT NOT NULL,
    artist        TEXT NOT NULL DEFAULT '',
    album         TEXT NOT NULL DEFAULT '',
    album_artist  TEXT NOT NULL DEFAULT '',
    track_no      INTEGER NOT NULL DEFAULT 0,
    disc_no       INTEGER NOT NULL DEFAULT 0,
    year          INTEGER NOT NULL DEFAULT 0,
    genre         TEXT NOT NULL DEFAULT '',
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    format        TEXT NOT NULL,
    size          INTEGER NOT NULL,
    mtime         INTEGER NOT NULL,
    cover_key     TEXT NOT NULL DEFAULT '',
    color         TEXT NOT NULL DEFAULT '',
    lrc_path      TEXT NOT NULL DEFAULT '',
    has_embedded_lyrics INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_tracks_folder ON tracks(folder);
CREATE INDEX idx_tracks_artist ON tracks(artist);
CREATE INDEX idx_tracks_album ON tracks(album);

-- lyrics downloaded from online providers (status: found | none | instrumental)
CREATE TABLE lyrics_cache (
    track_id   TEXT PRIMARY KEY,
    status     TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    fetched_at INTEGER NOT NULL
);

CREATE TABLE play_events (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    source      TEXT NOT NULL,
    ref         TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    artist      TEXT NOT NULL DEFAULT '',
    cover       TEXT NOT NULL DEFAULT '',
    listened_ms INTEGER NOT NULL DEFAULT 0,
    at          INTEGER NOT NULL
);
CREATE INDEX idx_play_user_at ON play_events(user_id, at);
CREATE INDEX idx_play_user_ref ON play_events(user_id, ref);

CREATE TABLE view_events (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    ref     TEXT NOT NULL,
    at      INTEGER NOT NULL
);
CREATE INDEX idx_view_user_at ON view_events(user_id, at);
CREATE INDEX idx_view_user_ref ON view_events(user_id, kind, ref);

-- server egress traffic, aggregated per day and category
CREATE TABLE traffic_daily (
    day      TEXT NOT NULL,     -- YYYY-MM-DD (server local time)
    category TEXT NOT NULL,     -- photo | cover | track | proxy | app
    bytes    INTEGER NOT NULL DEFAULT 0,
    requests INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, category)
);

-- one row per playback stream that went through the server (local track, proxy)
CREATE TABLE stream_sessions (
    sid        TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL,
    category   TEXT NOT NULL,
    ref        TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL DEFAULT '',
    bytes      INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER NOT NULL,
    last_at    INTEGER NOT NULL
);
CREATE INDEX idx_stream_started ON stream_sessions(started_at);

