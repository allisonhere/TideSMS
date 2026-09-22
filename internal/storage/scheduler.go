package storage

import (
	"github.com/allisonhere/tidesms/internal/scheduler"
	"time"
)

// Schedule stores a new scheduled message.
func (s *Store) Schedule(i scheduler.Item) error {
	_, err := s.db.Exec(`INSERT INTO scheduled_messages(id,device_id,thread_id,recipient,body,send_after,state,attempt_count,last_error,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		i.ID, i.DeviceID, i.ThreadID, i.Recipient, i.Body, ms(i.SendAfter), string(i.State), i.AttemptCount, i.LastError, ms(i.CreatedAt), ms(i.UpdatedAt))
	return err
}

// UpdateScheduled writes the mutable fields of a scheduled message.
func (s *Store) UpdateScheduled(i scheduler.Item) error {
	_, err := s.db.Exec(`UPDATE scheduled_messages SET thread_id=?,recipient=?,body=?,send_after=?,state=?,attempt_count=?,last_error=?,updated_at=? WHERE id=?`,
		i.ThreadID, i.Recipient, i.Body, ms(i.SendAfter), string(i.State), i.AttemptCount, i.LastError, ms(i.UpdatedAt), i.ID)
	return err
}

// ClaimScheduled atomically moves a scheduled message to queued once it is due.
// Returning false means it was already claimed, so a duplicate tick cannot
// release the same message twice.
func (s *Store) ClaimScheduled(id string, now time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE scheduled_messages SET state='queued', updated_at=? WHERE id=? AND state='scheduled' AND send_after<=?`, ms(now), id, ms(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Scheduled returns every scheduled message, soonest first.
func (s *Store) Scheduled() ([]scheduler.Item, error) {
	return s.scheduledWhere("ORDER BY send_after", nil)
}

// DueScheduled returns scheduled messages whose time has arrived.
func (s *Store) DueScheduled(now time.Time) ([]scheduler.Item, error) {
	return s.scheduledWhere("WHERE state='scheduled' AND send_after<=? ORDER BY send_after", []any{ms(now)})
}

// ScheduledItem looks up a single scheduled message.
func (s *Store) ScheduledItem(id string) (scheduler.Item, bool, error) {
	items, err := s.scheduledWhere("WHERE id=?", []any{id})
	if err != nil || len(items) == 0 {
		return scheduler.Item{}, false, err
	}
	return items[0], true, nil
}

// RemoveScheduled deletes a scheduled message, used when the user cancels it.
func (s *Store) RemoveScheduled(id string) error {
	_, err := s.db.Exec("DELETE FROM scheduled_messages WHERE id=?", id)
	return err
}

// CountScheduled counts messages still waiting to be released.
func (s *Store) CountScheduled() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM scheduled_messages WHERE state='scheduled'").Scan(&n)
	return n, err
}

func (s *Store) scheduledWhere(clause string, args []any) ([]scheduler.Item, error) {
	rows, err := s.db.Query(`SELECT id,device_id,thread_id,recipient,body,send_after,state,attempt_count,last_error,created_at,updated_at FROM scheduled_messages `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []scheduler.Item
	for rows.Next() {
		var i scheduler.Item
		var state string
		var sendAfter, created, updated int64
		if err = rows.Scan(&i.ID, &i.DeviceID, &i.ThreadID, &i.Recipient, &i.Body, &sendAfter, &state, &i.AttemptCount, &i.LastError, &created, &updated); err != nil {
			return nil, err
		}
		i.State = scheduler.State(state)
		i.SendAfter = fromMS(sendAfter)
		i.CreatedAt = fromMS(created)
		i.UpdatedAt = fromMS(updated)
		out = append(out, i)
	}
	return out, rows.Err()
}
