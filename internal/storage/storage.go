package storage

import (
	"database/sql"
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/migrations"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Store struct{ db *sql.DB }
type Draft struct {
	Body     string
	Revision int64
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { _ = db.Close(); return nil, e }
	// WAL plus a busy timeout lets the TUI and the optional background service
	// share the file: writers serialize, and a brief overlap waits rather than
	// failing. Only one process should run the sender (see tidesms-daemon's
	// lock file); claims keep delivery correct even if two do.
	if _, err = db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;"); err != nil {
		return fail(err)
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 14 {
		return fail(fmt.Errorf("database belongs to a newer TideSMS version"))
	}

	for v, script := range []string{migrations.Initial, migrations.Conversations, migrations.SyncedContacts, migrations.ContactMatch, migrations.GroupFromParticipants, migrations.QueueAndPreferences, migrations.MessageSearch, migrations.BubbleThemes, migrations.QueueOfflineWait, migrations.ParticipantIdentity, migrations.AttachmentsAndThreadState, migrations.AttachmentPartID, migrations.ClearBadAttachmentPaths, migrations.AttachmentThumbnails} {
		if version > v {
			continue
		}
		tx, e := db.Begin()
		if e != nil {
			return fail(e)
		}
		if _, e = tx.Exec(script); e != nil {
			_ = tx.Rollback()
			return fail(e)
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
	}
	// An interrupted send has an uncertain external outcome. Never auto-retry it.
	if _, err = db.Exec("UPDATE messages SET status='unknown' WHERE status='sending'"); err != nil {
		return fail(err)
	}
	// A queue row caught mid-send has the same uncertainty, so it is paused
	// rather than re-sent automatically when the next process starts.
	if _, err = db.Exec("UPDATE outgoing_queue SET state='paused', last_error='interrupted during send' WHERE state='sending'"); err != nil {
		return fail(err)
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Contacts() ([]contacts.Contact, error) {
	rows, err := s.db.Query("SELECT id,name,phone_number,theme,theme_in,theme_out FROM contacts ORDER BY name COLLATE NOCASE,id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // Read-only rows; iteration errors are checked below.
	var out []contacts.Contact
	for rows.Next() {
		var c contacts.Contact
		if err = rows.Scan(&c.ID, &c.Name, &c.PhoneNumber, &c.Theme, &c.ThemeIn, &c.ThemeOut); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) SaveContact(c contacts.Contact) error {
	_, err := s.db.Exec("INSERT INTO contacts(id,name,phone_number,theme,theme_in,theme_out) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,phone_number=excluded.phone_number,theme=excluded.theme,theme_in=excluded.theme_in,theme_out=excluded.theme_out", c.ID, c.Name, c.PhoneNumber, c.Theme, c.ThemeIn, c.ThemeOut)
	return err
}

// Drafts are addressed by canonical phone number, so editing a contact never sends its old draft to a new number.
func (s *Store) DeleteContact(id string) error {
	_, err := s.db.Exec("DELETE FROM contacts WHERE id=?", id)
	return err
}
func (s *Store) Drafts() (map[string]Draft, error) {
	rows, err := s.db.Query("SELECT recipient,body,revision FROM drafts")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // Read-only rows; iteration errors are checked below.
	out := map[string]Draft{}
	for rows.Next() {
		var key string
		var d Draft
		if err = rows.Scan(&key, &d.Body, &d.Revision); err != nil {
			return nil, err
		}
		out[key] = d
	}
	return out, rows.Err()
}

// Revision guards prevent an old debounce or in-flight send completion overwriting newer text.
func (s *Store) SaveDraft(key string, d Draft) error {
	_, err := s.db.Exec(`INSERT INTO drafts(recipient,body,revision) VALUES(?,?,?) ON CONFLICT(recipient) DO UPDATE SET body=excluded.body,revision=excluded.revision,updated_at=CURRENT_TIMESTAMP WHERE excluded.revision >= drafts.revision`, key, d.Body, d.Revision)
	return err
}
