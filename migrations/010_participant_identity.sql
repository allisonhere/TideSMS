-- A thread is a group when it has more than one distinct person, not when the
-- same number was written in more than one format. Phones frequently list
-- +15124100124, 15124100124 and 5124100124 for one person, which wrongly marked
-- one-to-one threads as groups and blocked replying. Collapse rows that share a
-- trailing ten-digit identity, then recompute the flag.
DELETE FROM thread_participants
WHERE rowid NOT IN (
  SELECT MIN(rowid) FROM thread_participants
  GROUP BY thread_id,
           CASE WHEN length(replace(number,'+','')) >= 10
                THEN substr(replace(number,'+',''),-10)
                ELSE number END
);
UPDATE threads SET is_group = (
  SELECT COUNT(DISTINCT CASE WHEN length(replace(number,'+','')) >= 10
                             THEN substr(replace(number,'+',''),-10)
                             ELSE number END)
  FROM thread_participants p WHERE p.thread_id = threads.id
) > 1;

PRAGMA user_version=10;
