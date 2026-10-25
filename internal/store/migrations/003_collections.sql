-- 003: favourites and playlists, stored per user so they follow the user
-- across devices. Items keep a JSON snapshot of the song / station
-- (source, id, title, artist, cover, extra …) because online items have
-- no row of their own in this database.

CREATE TABLE favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,              -- track | station
    source     TEXT NOT NULL,              -- local | direct | radio | lx:kw …
    ref        TEXT NOT NULL,
    data       TEXT NOT NULL,              -- item snapshot (JSON)
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, source, ref)
);
CREATE INDEX idx_fav_user_kind ON favorites(user_id, kind, created_at);

CREATE TABLE playlists (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_playlists_user ON playlists(user_id, updated_at);

CREATE TABLE playlist_items (
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    pos         INTEGER NOT NULL,
    source      TEXT NOT NULL,
    ref         TEXT NOT NULL,
    data        TEXT NOT NULL,
    PRIMARY KEY (playlist_id, pos)
);

