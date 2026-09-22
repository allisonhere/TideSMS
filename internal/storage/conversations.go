package storage

import (
	"database/sql"
	"errors"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"sort"
	"strings"
	"time"
)

func upsertThread(tx *sql.Tx, t domain.Thread) error {
	_, err := tx.Exec(`INSERT INTO threads(id,device_id,backend_id,display_name,last_message,last_timestamp,is_group) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET backend_id=CASE WHEN excluded.backend_id<>'' THEN excluded.backend_id ELSE threads.backend_id END,
 display_name=CASE WHEN excluded.display_name<>'' THEN excluded.display_name ELSE threads.display_name END,
 last_message=CASE WHEN excluded.last_timestamp>=threads.last_timestamp THEN excluded.last_message ELSE threads.last_message END,
 last_timestamp=MAX(threads.last_timestamp,excluded.last_timestamp)`, t.ID, t.DeviceID, t.BackendID, t.DisplayName, t.LastMessage, t.LastTimestamp.UnixMilli(), t.IsGroup)
	if err != nil {
		return err
	}
	for _, p := range t.Participants {
		if _, err = tx.Exec(`INSERT INTO thread_participants(thread_id,number,raw_number,name) VALUES(?,?,?,?) ON CONFLICT(thread_id,number) DO UPDATE SET raw_number=excluded.raw_number`, t.ID, p.Number, p.RawNumber, p.Name); err != nil {
			return err
		}
	}

	// The group flag is derived from the participants actually known, so a thread
	// mislabelled from one message's duplicated addresses corrects itself.
	if _, err = tx.Exec("UPDATE threads SET is_group=(SELECT COUNT(*) FROM thread_participants p WHERE p.thread_id=?)>1 WHERE id=?", t.ID, t.ID); err != nil {
		return err
	}

	if t.BackendID != "" && !strings.HasPrefix(t.BackendID, "local-") && len(t.Participants) == 1 && !t.IsGroup {
		local := domain.ThreadID(t.DeviceID, "local-"+t.Participants[0].Number)
		if _, err = tx.Exec("UPDATE messages SET thread_id=? WHERE thread_id=?", t.ID, local); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO drafts(recipient,body,revision) SELECT ?,body,revision FROM drafts WHERE recipient=? ON CONFLICT(recipient) DO NOTHING`, t.ID, local); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE threads SET theme=COALESCE((SELECT NULLIF(theme,'') FROM threads WHERE id=?),theme),theme_in=COALESCE((SELECT NULLIF(theme_in,'') FROM threads WHERE id=?),theme_in),theme_out=COALESCE((SELECT NULLIF(theme_out,'') FROM threads WHERE id=?),theme_out) WHERE id=? AND (theme='' OR theme_in='' OR theme_out='')`, local, local, local, t.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM threads WHERE id=?", local); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) MergeThreads(threads []domain.Thread) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range threads {
		if err = upsertThread(tx, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MergeMessages changes unread only for newly inserted rows. Replayed history
// never re-marks a locally read message. Backend IDs are scoped to device/thread.
func (s *Store) MergeMessages(messages []domain.Message) ([]domain.Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var added []domain.Message
	for _, m := range messages {
		m.ID = m.StableID()
		if err = upsertThread(tx, m.Thread()); err != nil {
			return nil, err
		}
		var exists string
		err = tx.QueryRow("SELECT id FROM messages WHERE id=?", m.ID).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		// A single unmatched local submission may be reconciled with its phone echo.
		// Never collapse ambiguous repeated identical sends or failed/unknown attempts.
		if errors.Is(err, sql.ErrNoRows) && m.Direction == domain.Outgoing && m.BackendID != "" {
			var count int
			var local sql.NullString
			err = tx.QueryRow(`SELECT COUNT(*),MIN(id) FROM messages WHERE thread_id=? AND backend_id='' AND body=? AND status IN ('sending','submitted') AND ABS(timestamp-?)<120000`, m.ThreadID, m.Body, m.Timestamp.UnixMilli()).Scan(&count, &local)
			if err != nil {
				return nil, err
			}
			if count == 1 {
				if _, err = tx.Exec("DELETE FROM messages WHERE id=?", local.String); err != nil {
					return nil, err
				}
			}
		}
		result, e := tx.Exec(`INSERT INTO messages(id,device_id,thread_id,backend_id,sender,body,timestamp,direction,status,unread) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, m.ID, m.DeviceID, m.ThreadID, m.BackendID, m.Sender, m.Body, m.Timestamp.UnixMilli(), m.Direction, m.Status, m.Unread)
		if e != nil {
			return nil, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return nil, e
		}
		if n > 0 {
			added = append(added, m)
		} else if m.BackendID != "" {
			if _, err = tx.Exec("UPDATE messages SET status=?,body=? WHERE id=?", m.Status, m.Body, m.ID); err != nil {
				return nil, err
			}
		}

	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return added, nil
}
func (s *Store) Threads(device string) ([]domain.Thread, error) {
	rows, err := s.db.Query(`SELECT t.id,t.device_id,t.backend_id,t.display_name,t.last_message,t.last_timestamp,t.theme,t.theme_in,t.theme_out,t.is_group,
 MAX(t.unread_override,(SELECT COUNT(*) FROM messages m WHERE m.thread_id=t.id AND m.unread=1)) FROM threads t WHERE (?='' OR device_id=?) ORDER BY last_timestamp DESC,id`, device, device)
	if err != nil {
		return nil, err
	}
	var out []domain.Thread
	for rows.Next() {
		var t domain.Thread
		var ms int64
		if err = rows.Scan(&t.ID, &t.DeviceID, &t.BackendID, &t.DisplayName, &t.LastMessage, &ms, &t.ThemeID, &t.ThemeIn, &t.ThemeOut, &t.IsGroup, &t.UnreadCount); err != nil {
			_ = rows.Close()
			return nil, err
		}
		t.LastTimestamp = time.UnixMilli(ms)
		out = append(out, t)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	// Separate query avoids nested use of the one-connection SQLite pool.
	for i := range out {
		ps, e := s.db.Query(`SELECT p.number,p.raw_number,COALESCE((SELECT NULLIF(name,'') FROM contacts c WHERE c.phone_number=p.number ORDER BY id LIMIT 1),(SELECT NULLIF(name,'') FROM synced_contacts s WHERE s.number=p.number ORDER BY device_id,uid LIMIT 1),
 (SELECT NULLIF(s.name,'') FROM synced_contacts s WHERE s.match_key<>'' AND s.match_key=CASE WHEN length(replace(p.number,'+',''))>=10 THEN substr(replace(p.number,'+',''),-10) ELSE '' END
   GROUP BY s.match_key HAVING COUNT(DISTINCT NULLIF(s.name,''))=1),
 NULLIF(p.name,''),p.raw_number) FROM thread_participants p WHERE thread_id=? ORDER BY p.number`, out[i].ID)
		if e != nil {
			return nil, e
		}
		for ps.Next() {
			var p domain.Participant
			if e = ps.Scan(&p.Number, &p.RawNumber, &p.Name); e != nil {
				_ = ps.Close()
				return nil, e
			}
			out[i].Participants = append(out[i].Participants, p)
		}
		e = ps.Err()
		_ = ps.Close()
		if e != nil {
			return nil, e
		}
		if out[i].DisplayName == "" {
			// One person often holds several numbers, so their name would
			// otherwise be repeated in the thread title.
			names, seen := []string{}, map[string]bool{}
			for _, p := range out[i].Participants {
				if p.Name == "" || seen[p.Name] {
					continue
				}
				seen[p.Name] = true
				names = append(names, p.Name)
			}
			out[i].DisplayName = strings.Join(names, ", ")
			if out[i].DisplayName == "" {
				out[i].DisplayName = "Unknown sender"
			}
		}
	}
	return out, nil
}
func scanMessages(rows *sql.Rows) ([]domain.Message, error) {
	defer func() { _ = rows.Close() }()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var ms int64
		if err := rows.Scan(&m.ID, &m.DeviceID, &m.ThreadID, &m.BackendID, &m.Sender, &m.Body, &ms, &m.Direction, &m.Status, &m.Unread); err != nil {
			return nil, err
		}
		m.Timestamp = time.UnixMilli(ms)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].ID < out[j].ID
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, rows.Err()
}

const messageColumns = "id,device_id,thread_id,backend_id,sender,body,timestamp,direction,status,unread"

func (s *Store) Messages(thread string, limit int) ([]domain.Message, error) {
	rows, err := s.db.Query("SELECT "+messageColumns+" FROM messages WHERE thread_id=? ORDER BY timestamp DESC,id DESC LIMIT ?", thread, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}
func (s *Store) Search(thread, query string) ([]domain.Message, error) {
	rows, err := s.db.Query("SELECT "+messageColumns+" FROM messages WHERE thread_id=? AND instr(lower(body),lower(?))>0 ORDER BY timestamp DESC,id DESC LIMIT 1000", thread, query)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}
func (s *Store) MarkRead(thread string, ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err = tx.Exec("UPDATE messages SET unread=0 WHERE thread_id=? AND id=?", thread, id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE threads SET unread_override=0 WHERE id=?", thread); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) MarkUnread(thread string) error {
	_, err := s.db.Exec("UPDATE threads SET unread_override=1 WHERE id=?", thread)
	return err
}
func (s *Store) ThreadTheme(thread, theme string) error {
	_, err := s.db.Exec("UPDATE threads SET theme=? WHERE id=?", theme, thread)
	return err
}

// ThreadBubbleThemes stores a thread's received and sent bubble palettes.
// An empty string keeps the derived surface for that direction.
func (s *Store) ThreadBubbleThemes(thread, in, out string) error {
	_, err := s.db.Exec("UPDATE threads SET theme_in=?,theme_out=? WHERE id=?", in, out, thread)
	return err
}
func (s *Store) MessageStatus(id string, status domain.Status) error {
	_, err := s.db.Exec("UPDATE messages SET status=? WHERE id=? AND backend_id=''", status, id)
	return err
}

// DeleteLocalMessage removes a message the phone never acknowledged, used when
// the user discards a queued or failed local submission. Messages that came
// from the phone are left alone.
func (s *Store) DeleteLocalMessage(id string) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE id=? AND backend_id=''", id)
	return err
}

func (s *Store) LastSync(thread string) (string, time.Time, error) {
	var id string
	var ms int64
	err := s.db.QueryRow("SELECT last_backend_message_id,last_timestamp FROM sync_state WHERE thread_id=?", thread).Scan(&id, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	return id, time.UnixMilli(ms), err
}

func (s *Store) SaveSync(thread, id string, stamp time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sync_state(thread_id,last_backend_message_id,last_timestamp,last_sync_at) VALUES(?,?,?,?) ON CONFLICT(thread_id) DO UPDATE SET last_backend_message_id=excluded.last_backend_message_id,last_timestamp=excluded.last_timestamp,last_sync_at=excluded.last_sync_at`, thread, id, stamp.UnixMilli(), time.Now().UnixMilli())
	return err
}

// ReplaceSyncedContacts makes the phone's address book the authority for its own
// device only. Locally created contacts live in a separate table and are never
// written here, so a re-sync cannot edit or delete anything the user made.
func (s *Store) ReplaceSyncedContacts(device string, cs []contacts.Synced) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec("DELETE FROM synced_contacts WHERE device_id=?", device); err != nil {
		return err
	}
	for _, c := range cs {
		if c.PhoneNumber == "" {
			continue
		}
		if _, err = tx.Exec(`INSERT INTO synced_contacts(device_id,uid,number,raw_number,name,match_key) VALUES(?,?,?,?,?,?)
 ON CONFLICT(device_id,uid,number) DO UPDATE SET raw_number=excluded.raw_number,name=excluded.name,match_key=excluded.match_key`,
			device, c.UID, c.PhoneNumber, c.RawNumber, c.Name, contacts.MatchKey(c.PhoneNumber)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SyncedContacts(device string) ([]contacts.Synced, error) {
	rows, err := s.db.Query(`SELECT uid,number,raw_number,name FROM synced_contacts WHERE (?='' OR device_id=?)
 GROUP BY number ORDER BY name COLLATE NOCASE,number`, device, device)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // Read-only rows; iteration errors are checked below.
	var out []contacts.Synced
	for rows.Next() {
		var c contacts.Synced
		if err = rows.Scan(&c.UID, &c.PhoneNumber, &c.RawNumber, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
