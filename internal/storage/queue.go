package storage

import (
	"github.com/allisonhere/tidesms/internal/queue"
	"time"
)

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

// Enqueue inserts a new queued message.
func (s *Store) Enqueue(i queue.Item) error {
	_, err := s.db.Exec(`INSERT INTO outgoing_queue(id,device_id,thread_id,recipient,body,state,attempt_count,last_error,created_at,updated_at,last_attempt_at,next_attempt_at,offline_wait)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.ID, i.DeviceID, i.ThreadID, i.Recipient, i.Body, string(i.State), i.AttemptCount, i.LastError,
		ms(i.CreatedAt), ms(i.UpdatedAt), ms(i.LastAttemptAt), ms(i.NextAttemptAt), boolInt(i.OfflineWait))
	return err
}

// UpdateQueue writes the mutable fields of an item back. The id and creation
// time are preserved.
func (s *Store) UpdateQueue(i queue.Item) error {
	_, err := s.db.Exec(`UPDATE outgoing_queue SET state=?, attempt_count=?, last_error=?, updated_at=?, last_attempt_at=?, next_attempt_at=?, offline_wait=? WHERE id=?`,
		string(i.State), i.AttemptCount, i.LastError, ms(i.UpdatedAt), ms(i.LastAttemptAt), ms(i.NextAttemptAt), boolInt(i.OfflineWait), i.ID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ClaimQueue atomically moves an item from queued to sending. It returns false
// when another worker already claimed it, which makes duplicate reconnect
// events harmless.
func (s *Store) ClaimQueue(id string, now time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE outgoing_queue SET state='sending', attempt_count=attempt_count+1, updated_at=?, last_attempt_at=? WHERE id=? AND state='queued'`, ms(now), ms(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Queue returns every queued item oldest first, regardless of state.
func (s *Store) Queue() ([]queue.Item, error) { return s.queueWhere("", nil) }

// DueQueue returns queued items that are eligible at now, oldest first.
func (s *Store) DueQueue(now time.Time, limit int) ([]queue.Item, error) {
	if limit <= 0 {
		limit = 1
	}
	return s.queueWhere("WHERE state='queued' AND (next_attempt_at=0 OR next_attempt_at<=?) ORDER BY created_at LIMIT ?", []any{ms(now), limit})
}

// QueueItem looks up a single item.
func (s *Store) QueueItem(id string) (queue.Item, bool, error) {
	items, err := s.queueWhere("WHERE id=?", []any{id})
	if err != nil || len(items) == 0 {
		return queue.Item{}, false, err
	}
	return items[0], true, nil
}

// CountQueue counts items in any of the given states. With no states it counts
// everything.
func (s *Store) CountQueue(states ...queue.State) (int, error) {
	q := "SELECT COUNT(*) FROM outgoing_queue"
	var args []any
	if len(states) > 0 {
		q += " WHERE state IN (" + placeholders(len(states)) + ")"
		for _, st := range states {
			args = append(args, string(st))
		}
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// ReleaseOfflineWaits clears the short retry delay left by an offline attempt,
// so a reconnect sends queued messages immediately instead of waiting out the
// timer.
func (s *Store) ReleaseOfflineWaits() error {
	_, err := s.db.Exec("UPDATE outgoing_queue SET next_attempt_at=0, offline_wait=0 WHERE state='queued' AND offline_wait=1")
	return err
}

// RemoveQueue deletes an item, used when the user discards it.
func (s *Store) RemoveQueue(id string) error {
	_, err := s.db.Exec("DELETE FROM outgoing_queue WHERE id=?", id)
	return err
}

func (s *Store) queueWhere(where string, args []any) ([]queue.Item, error) {
	rows, err := s.db.Query(`SELECT id,device_id,thread_id,recipient,body,state,attempt_count,last_error,created_at,updated_at,last_attempt_at,next_attempt_at,offline_wait FROM outgoing_queue `+where, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []queue.Item
	for rows.Next() {
		var i queue.Item
		var state string
		var created, updated, lastAttempt, next int64
		var offlineWait int
		if err = rows.Scan(&i.ID, &i.DeviceID, &i.ThreadID, &i.Recipient, &i.Body, &state, &i.AttemptCount, &i.LastError, &created, &updated, &lastAttempt, &next, &offlineWait); err != nil {
			return nil, err
		}
		i.State = queue.State(state)
		i.CreatedAt = fromMS(created)
		i.UpdatedAt = fromMS(updated)
		i.LastAttemptAt = fromMS(lastAttempt)
		i.NextAttemptAt = fromMS(next)
		i.OfflineWait = offlineWait != 0
		out = append(out, i)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
