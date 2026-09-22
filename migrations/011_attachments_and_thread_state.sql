-- Message attachments. A part starts as metadata-only and becomes available
-- once fetched; rendering never waits on it.
CREATE TABLE attachments (
 id TEXT PRIMARY KEY,
 message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 mime TEXT NOT NULL DEFAULT '',
 filename TEXT NOT NULL DEFAULT '',
 size INTEGER NOT NULL DEFAULT 0,
 local_path TEXT NOT NULL DEFAULT '',
 remote_id TEXT NOT NULL DEFAULT '',
 width INTEGER NOT NULL DEFAULT 0,
 height INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL DEFAULT 'metadata'
);
CREATE INDEX attachments_message ON attachments(message_id);

-- Thread state for pinning and archiving. Archived threads leave the main list
-- but stay searchable.
ALTER TABLE threads ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE threads ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

PRAGMA user_version=11;
