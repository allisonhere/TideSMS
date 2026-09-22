CREATE TABLE synced_contacts (
 device_id TEXT NOT NULL, uid TEXT NOT NULL, number TEXT NOT NULL,
 raw_number TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(device_id,uid,number)
);
CREATE INDEX synced_contacts_number ON synced_contacts(number);
PRAGMA user_version=3;
