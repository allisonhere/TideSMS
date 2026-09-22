-- A thread is a group when it has more than one distinct participant. The flag
-- was previously taken from KDE Connect's multi-target bit, which is also set
-- when the phone lists one person's address twice, wrongly marking one-to-one
-- threads read-only.
UPDATE threads SET is_group = (SELECT COUNT(*) FROM thread_participants p WHERE p.thread_id = threads.id) > 1;
UPDATE messages SET sender = COALESCE((SELECT p.number FROM thread_participants p WHERE p.thread_id = messages.thread_id), sender)
 WHERE sender = 'Group participant'
   AND (SELECT COUNT(*) FROM thread_participants p WHERE p.thread_id = messages.thread_id) = 1;
PRAGMA user_version=5;
