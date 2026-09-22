package storage

import (
	"database/sql"
	"github.com/allisonhere/tidesms/internal/domain"
)

const attachmentColumns = "id,message_id,mime,filename,size,local_path,remote_id,width,height,state"

// SaveAttachments upserts a message's media parts.
func (s *Store) SaveAttachments(messageID string, atts []domain.Attachment) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, a := range atts {
		a.MessageID = messageID
		if err = upsertAttachment(tx, a); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Attachments returns a message's media parts, oldest id first.
func (s *Store) Attachments(messageID string) ([]domain.Attachment, error) {
	rows, err := s.db.Query("SELECT "+attachmentColumns+" FROM attachments WHERE message_id=? ORDER BY id", messageID)
	if err != nil {
		return nil, err
	}
	return scanAttachments(rows)
}

// AttachmentsForThread groups a thread's attachments by message id, so a
// conversation can render them without a query per message.
func (s *Store) AttachmentsForThread(thread string) (map[string][]domain.Attachment, error) {
	rows, err := s.db.Query("SELECT a.id,a.message_id,a.mime,a.filename,a.size,a.local_path,a.remote_id,a.width,a.height,a.state FROM attachments a JOIN messages m ON m.id=a.message_id WHERE m.thread_id=? ORDER BY a.id", thread)
	if err != nil {
		return nil, err
	}
	list, err := scanAttachments(rows)
	if err != nil {
		return nil, err
	}
	out := map[string][]domain.Attachment{}
	for _, a := range list {
		out[a.MessageID] = append(out[a.MessageID], a)
	}
	return out, nil
}

// SetAttachmentState records the download outcome for one part.
func (s *Store) SetAttachmentState(id string, state domain.AttachmentState, localPath string) error {
	_, err := s.db.Exec("UPDATE attachments SET state=?, local_path=CASE WHEN ?<>'' THEN ? ELSE local_path END WHERE id=?", string(state), localPath, localPath, id)
	return err
}

func upsertAttachment(tx *sql.Tx, a domain.Attachment) error {
	_, err := tx.Exec(`INSERT INTO attachments(id,message_id,mime,filename,size,local_path,remote_id,width,height,state)
 VALUES(?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET mime=excluded.mime,filename=excluded.filename,size=excluded.size,remote_id=excluded.remote_id,width=excluded.width,height=excluded.height,state=excluded.state`,
		a.ID, a.MessageID, a.MIMEType, a.Filename, a.Size, a.LocalPath, a.RemoteID, a.Width, a.Height, string(a.State))
	return err
}

func scanAttachments(rows *sql.Rows) ([]domain.Attachment, error) {
	defer func() { _ = rows.Close() }()
	var out []domain.Attachment
	for rows.Next() {
		var a domain.Attachment
		var state string
		if err := rows.Scan(&a.ID, &a.MessageID, &a.MIMEType, &a.Filename, &a.Size, &a.LocalPath, &a.RemoteID, &a.Width, &a.Height, &state); err != nil {
			return nil, err
		}
		a.State = domain.AttachmentState(state)
		out = append(out, a)
	}
	return out, rows.Err()
}
