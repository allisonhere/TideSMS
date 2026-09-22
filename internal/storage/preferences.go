package storage

import (
	"database/sql"
	"errors"
	"github.com/allisonhere/tidesms/internal/contacts"
	"time"
)

// Scopes for per-entity preferences. "global" rows are normally unnecessary
// because the TOML config holds the global default, but the table accepts them
// so a future UI can override without a schema change.
const (
	ScopeGlobal  = "global"
	ScopeContact = "contact"
	ScopeThread  = "thread"
)

// AIPolicy returns the stored override for a scope, if any.
func (s *Store) AIPolicy(scope, id string) (policy, provider, model string, ok bool, err error) {
	row := s.db.QueryRow("SELECT policy,provider,model FROM ai_preferences WHERE scope=? AND scope_id=?", scope, id)
	err = row.Scan(&policy, &provider, &model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	return policy, provider, model, true, nil
}

// SetAIPolicy stores or replaces an override.
func (s *Store) SetAIPolicy(scope, id, policy, provider, model string) error {
	_, err := s.db.Exec("INSERT INTO ai_preferences(scope,scope_id,policy,provider,model,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(scope,scope_id) DO UPDATE SET policy=excluded.policy,provider=excluded.provider,model=excluded.model,updated_at=excluded.updated_at",
		scope, id, policy, provider, model, ms(time.Now()))
	return err
}

// ClearAIPolicy removes an override, returning the scope to the global default.
func (s *Store) ClearAIPolicy(scope, id string) error {
	_, err := s.db.Exec("DELETE FROM ai_preferences WHERE scope=? AND scope_id=?", scope, id)
	return err
}

// NotificationMode returns the stored override for a scope, if any.
func (s *Store) NotificationMode(scope, id string) (mode string, ok bool, err error) {
	err = s.db.QueryRow("SELECT mode FROM notification_preferences WHERE scope=? AND scope_id=?", scope, id).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return mode, true, nil
}

// SetNotificationMode stores or replaces a notification override.
func (s *Store) SetNotificationMode(scope, id, mode string) error {
	_, err := s.db.Exec("INSERT INTO notification_preferences(scope,scope_id,mode,updated_at) VALUES(?,?,?,?) ON CONFLICT(scope,scope_id) DO UPDATE SET mode=excluded.mode,updated_at=excluded.updated_at",
		scope, id, mode, ms(time.Now()))
	return err
}

// ClearNotificationMode removes a notification override.
func (s *Store) ClearNotificationMode(scope, id string) error {
	_, err := s.db.Exec("DELETE FROM notification_preferences WHERE scope=? AND scope_id=?", scope, id)
	return err
}

// SaveContactSource records where a contact entry came from. Multiple sources
// may point at the same contact id.
func (s *Store) SaveContactSource(c contacts.Source) error {
	_, err := s.db.Exec("INSERT INTO contact_sources(contact_id,source,source_id,display_name,phone_number,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(contact_id,source,source_id) DO UPDATE SET display_name=excluded.display_name,phone_number=excluded.phone_number,updated_at=excluded.updated_at",
		c.ContactID, c.Source, c.SourceID, c.DisplayName, c.PhoneNumber, ms(time.Now()))
	return err
}

// ContactSources lists the sources recorded for a contact.
func (s *Store) ContactSources(contactID string) ([]contacts.Source, error) {
	rows, err := s.db.Query("SELECT contact_id,source,source_id,display_name,phone_number FROM contact_sources WHERE contact_id=? ORDER BY source,source_id", contactID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []contacts.Source
	for rows.Next() {
		var c contacts.Source
		if err = rows.Scan(&c.ContactID, &c.Source, &c.SourceID, &c.DisplayName, &c.PhoneNumber); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ContactIDsByNumber returns contact ids whose recorded sources use the given
// number. It is used by merge review, which runs only on explicit request.
func (s *Store) ContactIDsByNumber(number string) ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT contact_id FROM contact_sources WHERE phone_number=?", number)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
