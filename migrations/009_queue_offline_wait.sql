-- Distinguish a "waiting for the phone" delay from a real retry. A reconnect
-- clears the flag and the retry timer, so queued messages go out at once
-- instead of waiting the short delay out. Matching on the error text was
-- fragile.
ALTER TABLE outgoing_queue ADD COLUMN offline_wait INTEGER NOT NULL DEFAULT 0;

PRAGMA user_version=9;
