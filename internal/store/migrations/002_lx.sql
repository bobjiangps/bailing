-- Phase 3: LX Music custom-source scripts. Script metadata lives in the
-- .js files themselves; the database only remembers whether each one is
-- enabled (id = first 12 hex chars of sha1(file name)).
CREATE TABLE lx_scripts (
	id         TEXT PRIMARY KEY,
	file       TEXT NOT NULL,
	enabled    INTEGER NOT NULL DEFAULT 1,
	updated_at INTEGER NOT NULL DEFAULT 0
);

