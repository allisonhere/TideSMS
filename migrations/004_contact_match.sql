ALTER TABLE synced_contacts ADD COLUMN match_key TEXT NOT NULL DEFAULT '';
CREATE INDEX synced_contacts_match ON synced_contacts(match_key);
PRAGMA user_version=4;
