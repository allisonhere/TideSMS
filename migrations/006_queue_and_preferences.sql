-- Offline outgoing queue. A row lives here from the moment the user chose to
-- queue it until it is sent, paused or permanently failed. Timestamps are Unix
-- milliseconds so ordering and "due" checks need no text parsing.
CREATE TABLE outgoing_queue (
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL,
 thread_id TEXT NOT NULL DEFAULT '',
 recipient TEXT NOT NULL,
 body TEXT NOT NULL,
 state TEXT NOT NULL,
 attempt_count INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL DEFAULT 0,
 last_attempt_at INTEGER NOT NULL DEFAULT 0,
 next_attempt_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX outgoing_queue_due ON outgoing_queue(state,next_attempt_at,created_at);

-- Scheduled messages. send_after is an absolute instant: a scheduled message
-- must never leave early because the phone happened to reconnect.
CREATE TABLE scheduled_messages (
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL,
 thread_id TEXT NOT NULL DEFAULT '',
 recipient TEXT NOT NULL,
 body TEXT NOT NULL,
 send_after INTEGER NOT NULL,
 state TEXT NOT NULL,
 attempt_count INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX scheduled_messages_due ON scheduled_messages(state,send_after);

-- Per-contact and per-thread AI policy overrides. The global default lives in
-- the TOML config; only explicit overrides are stored here.
CREATE TABLE ai_preferences (
 scope TEXT NOT NULL,
 scope_id TEXT NOT NULL DEFAULT '',
 policy TEXT NOT NULL,
 provider TEXT NOT NULL DEFAULT '',
 model TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(scope,scope_id)
);

-- Where a contact came from. Local rows are authoritative for the alias; phone
-- rows keep the original imported name and number for display and merging.
CREATE TABLE contact_sources (
 contact_id TEXT NOT NULL,
 source TEXT NOT NULL,
 source_id TEXT NOT NULL DEFAULT '',
 display_name TEXT NOT NULL DEFAULT '',
 phone_number TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(contact_id,source,source_id)
);
CREATE INDEX contact_sources_number ON contact_sources(phone_number);

-- Per-contact and per-thread notification overrides: normal, muted, mentions,
-- or privacy (sender only). Global defaults live in the TOML config.
CREATE TABLE notification_preferences (
 scope TEXT NOT NULL,
 scope_id TEXT NOT NULL DEFAULT '',
 mode TEXT NOT NULL,
 updated_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(scope,scope_id)
);

PRAGMA user_version=6;
