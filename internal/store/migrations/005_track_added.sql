-- 005: when a track first appeared in the library, for the "recently added"
-- sort. Existing rows take their file time as a best guess.

ALTER TABLE tracks ADD COLUMN added_at INTEGER NOT NULL DEFAULT 0;
UPDATE tracks SET added_at = CASE WHEN mtime > 0 THEN mtime ELSE CAST(strftime('%s','now') AS INTEGER) END;

