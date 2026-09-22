-- A backend thumbnail was being stored as the attachment's local_path and
-- marked available, so the conversation drew KDE Connect's 100x100 preview and
-- a download was refused as already done: the real part was unreachable. Give
-- the preview its own column and put those rows back to metadata-only so the
-- full image can be fetched.
ALTER TABLE attachments ADD COLUMN thumb_path TEXT NOT NULL DEFAULT '';

UPDATE attachments
SET thumb_path = local_path, local_path = '', state = 'metadata'
WHERE local_path LIKE '%/tidesms-thumbnails/%';

PRAGMA user_version=14;
