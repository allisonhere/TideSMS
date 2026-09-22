package storage

import (
	"github.com/allisonhere/tidesms/internal/search"
	"strings"
)

// GlobalSearch runs a global message query. Text is served by the FTS index; the
// from/before/after filters are applied in SQL. Results are newest first.
func (s *Store) GlobalSearch(q search.Query, limit int) ([]search.Result, error) {
	if q.Empty() {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	match := search.MatchExpression(q.Text)

	var (
		from    []string
		where   []string
		args    []any
		selectQ string
	)
	if match != "" {
		selectQ = `SELECT m.id,m.device_id,m.thread_id,m.sender,m.body,m.timestamp,COALESCE(t.display_name,'')
 FROM message_fts f JOIN messages m ON m.rowid=f.rowid LEFT JOIN threads t ON t.id=m.thread_id`
		where = append(where, "message_fts MATCH ?")
		args = append(args, match)
	} else {
		selectQ = `SELECT m.id,m.device_id,m.thread_id,m.sender,m.body,m.timestamp,COALESCE(t.display_name,'')
 FROM messages m LEFT JOIN threads t ON t.id=m.thread_id`
	}
	if q.From != "" {
		like := "%" + strings.TrimSpace(q.From) + "%"
		from = append(from,
			"m.sender LIKE ?",
			"t.display_name LIKE ?",
			"EXISTS(SELECT 1 FROM thread_participants p WHERE p.thread_id=m.thread_id AND (p.name LIKE ? OR p.number LIKE ?))")
		args = append(args, like, like, like, like)
		where = append(where, "("+strings.Join(from, " OR ")+")")
	}
	if !q.Before.IsZero() {
		where = append(where, "m.timestamp < ?")
		args = append(args, ms(q.Before))
	}
	if !q.After.IsZero() {
		where = append(where, "m.timestamp >= ?")
		args = append(args, ms(q.After))
	}
	selectQ += " WHERE " + strings.Join(where, " AND ")
	selectQ += " ORDER BY m.timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(selectQ, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []search.Result
	for rows.Next() {
		var r search.Result
		var ts int64
		if err = rows.Scan(&r.MessageID, &r.DeviceID, &r.ThreadID, &r.Sender, &r.Body, &ts, &r.ThreadName); err != nil {
			return nil, err
		}
		r.Timestamp = fromMS(ts)
		out = append(out, r)
	}
	return out, rows.Err()
}
