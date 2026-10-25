-- 004: remember whether title / artist came from the tags or were guessed
-- from the file name, so the scanner can learn a folder's naming order
-- ("Title - Artist" vs "Artist - Title") from its tagged files.
-- mtime = 0 forces a one-time re-index of every track with the new rules;
-- negative lyric lookups are dropped so they are retried with the new names.

ALTER TABLE tracks ADD COLUMN name_guessed INTEGER NOT NULL DEFAULT 0;
UPDATE tracks SET mtime = 0;
DELETE FROM lyrics_cache WHERE status <> 'found';

