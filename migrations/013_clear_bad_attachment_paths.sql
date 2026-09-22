-- Earlier versions stored KDE Connect's base64 thumbnail as if it were a file
-- path, so the viewer printed a wall of base64. Real paths are absolute; drop
-- anything else and let the next sync materialise the thumbnail properly.
UPDATE attachments
SET local_path = '', state = 'metadata'
WHERE local_path <> ''
  AND (local_path NOT LIKE '/%' OR length(local_path) > 4096);

PRAGMA user_version=13;
