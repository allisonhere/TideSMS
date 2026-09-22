-- The backend part number is needed to request an attachment's file.
ALTER TABLE attachments ADD COLUMN part_id INTEGER NOT NULL DEFAULT 0;

PRAGMA user_version=12;
