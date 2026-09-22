CREATE TABLE threads (
 id TEXT PRIMARY KEY, device_id TEXT NOT NULL, backend_id TEXT NOT NULL DEFAULT '',
 display_name TEXT NOT NULL DEFAULT '', last_message TEXT NOT NULL DEFAULT '',
 last_timestamp INTEGER NOT NULL DEFAULT 0, theme TEXT NOT NULL DEFAULT '', is_group INTEGER NOT NULL DEFAULT 0,
 unread_override INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX threads_activity ON threads(device_id,last_timestamp DESC);
CREATE TABLE thread_participants (
 thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
 number TEXT NOT NULL, raw_number TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(thread_id,number)
);
CREATE TABLE messages (
 id TEXT PRIMARY KEY, device_id TEXT NOT NULL, thread_id TEXT NOT NULL REFERENCES threads(id),
 backend_id TEXT NOT NULL DEFAULT '', sender TEXT NOT NULL, body TEXT NOT NULL,
 timestamp INTEGER NOT NULL, direction TEXT NOT NULL, status TEXT NOT NULL,
 unread INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX messages_thread_time ON messages(thread_id,timestamp DESC,id DESC);
CREATE UNIQUE INDEX messages_backend_id ON messages(device_id,thread_id,backend_id) WHERE backend_id <> '';
CREATE TABLE sync_state (
 thread_id TEXT PRIMARY KEY REFERENCES threads(id), last_backend_message_id TEXT NOT NULL DEFAULT '',
 last_timestamp INTEGER NOT NULL DEFAULT 0, last_sync_at INTEGER NOT NULL DEFAULT 0
);
PRAGMA user_version=2;
