// Package api is TideSMS's interface for other programs: a dashboard showing
// new messages, a script sending one. It reads the same SQLite cache the app
// renders from and sends through the same backend, so what it reports and what
// the app shows cannot disagree.
//
// It is reached through `tidesms api …`, which prints JSON. The shapes here are
// the contract: fields are only ever added, and SchemaVersion changes if one
// has to change meaning.
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/media"
)

// SchemaVersion is the version of every document this package prints.
const SchemaVersion = 1

// Store is what the API reads and writes. *storage.Store satisfies it.
type Store interface {
	Threads(device string) ([]domain.Thread, error)
	Messages(thread string, limit int) ([]domain.Message, error)
	MergeMessages([]domain.Message) ([]domain.Message, error)
	MessageStatus(id string, status domain.Status) error
	MarkRead(thread string, ids []string) error
}

// Thread is one conversation as the API reports it.
type Thread struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Numbers     []string  `json:"numbers"`
	Group       bool      `json:"group"`
	Unread      int       `json:"unread"`
	LastMessage string    `json:"lastMessage"`
	LastTime    time.Time `json:"lastTime"`
	// Replyable is false for a group, which KDE Connect cannot send to here.
	Replyable bool `json:"replyable"`
}

// Threads is the answer to `tidesms api threads`.
type Threads struct {
	SchemaVersion int       `json:"schemaVersion"`
	GeneratedAt   time.Time `json:"generatedAt"`
	// Unread is the total across every conversation, not only those listed.
	Unread  int      `json:"unread"`
	Threads []Thread `json:"threads"`
}

// Message is one message as the API reports it.
type Message struct {
	ID       string    `json:"id"`
	From     string    `json:"from"`
	FromMe   bool      `json:"fromMe"`
	Body     string    `json:"body"`
	Time     time.Time `json:"time"`
	Status   string    `json:"status"`
	Unread   bool      `json:"unread"`
	Pictures int       `json:"pictures"`
}

// Messages is the answer to `tidesms api messages`.
type Messages struct {
	SchemaVersion int       `json:"schemaVersion"`
	Thread        Thread    `json:"thread"`
	Messages      []Message `json:"messages"`
}

// Sent is the answer to `tidesms api send`.
type Sent struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Thread        string `json:"thread"`
	// Status is "submitted": KDE Connect took it. Nothing confirms delivery.
	Status string `json:"status"`
}

// ListThreads reports conversations newest first. unreadOnly keeps those with
// unread messages; limit caps the list, and 0 means no cap.
func ListThreads(s Store, limit int, unreadOnly bool) (Threads, error) {
	all, err := s.Threads("")
	if err != nil {
		return Threads{}, err
	}
	out := Threads{SchemaVersion: SchemaVersion, GeneratedAt: time.Now().UTC(), Threads: []Thread{}}
	for _, t := range all {
		if t.Archived {
			continue
		}
		out.Unread += t.UnreadCount
		if unreadOnly && t.UnreadCount == 0 {
			continue
		}
		if limit > 0 && len(out.Threads) >= limit {
			continue
		}
		out.Threads = append(out.Threads, thread(t))
	}
	return out, nil
}

func thread(t domain.Thread) Thread {
	group := len(domain.DedupeParticipants(t.Participants)) > 1
	out := Thread{ID: t.ID, Name: contacts.SafeLabel(t.DisplayName), Group: group, Unread: t.UnreadCount,
		LastMessage: contacts.SafeLabel(t.LastMessage), LastTime: t.LastTimestamp.UTC(), Replyable: !group, Numbers: []string{}}
	for _, p := range domain.DedupeParticipants(t.Participants) {
		out.Numbers = append(out.Numbers, p.Number)
	}
	return out
}

// ErrNoThread means the id names no conversation in the cache.
var ErrNoThread = errors.New("no such conversation")

// FindThread looks a conversation up by its id.
func FindThread(s Store, id string) (domain.Thread, error) {
	all, err := s.Threads("")
	if err != nil {
		return domain.Thread{}, err
	}
	for _, t := range all {
		if t.ID == id {
			return t, nil
		}
	}
	return domain.Thread{}, ErrNoThread
}

// ListMessages reports a conversation's newest messages, oldest first.
func ListMessages(s Store, id string, limit int) (Messages, error) {
	t, err := FindThread(s, id)
	if err != nil {
		return Messages{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	ms, err := s.Messages(id, limit)
	if err != nil {
		return Messages{}, err
	}
	names := map[string]string{}
	for _, p := range t.Participants {
		names[p.Number] = p.Name
		if key := contacts.MatchKey(p.Number); key != "" {
			names[key] = p.Name
		}
	}
	out := Messages{SchemaVersion: SchemaVersion, Thread: thread(t), Messages: []Message{}}
	for _, m := range ms {
		from := "You"
		if m.Direction != domain.Outgoing {
			from = m.Sender
			if n := names[m.Sender]; n != "" {
				from = n
			} else if n := names[contacts.MatchKey(m.Sender)]; n != "" {
				from = n
			}
		}
		out.Messages = append(out.Messages, Message{ID: m.ID, From: contacts.SafeLabel(from), FromMe: m.Direction == domain.Outgoing,
			Body: m.Body, Time: m.Timestamp.UTC(), Status: string(m.Status), Unread: m.Unread, Pictures: len(m.Attachments)})
	}
	return out, nil
}

// MarkRead marks every message in a conversation read, as opening it does.
func MarkRead(s Store, id string) error {
	ms, err := s.Messages(id, 1000)
	if err != nil {
		return err
	}
	var ids []string
	for _, m := range ms {
		if m.Unread {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.MarkRead(id, ids)
}

// Send replies in a conversation: the message is stored as sending, handed to
// the backend, and stored again as submitted or failed, as the app does, so the
// app shows it either way. Pictures are paths; they are prepared as the app
// prepares attachments.
func Send(ctx context.Context, s Store, b backend.MessagingBackend, id, text string, pictures []string) (Sent, error) {
	t, err := FindThread(s, id)
	if err != nil {
		return Sent{}, err
	}
	people := domain.DedupeParticipants(t.Participants)
	if len(people) != 1 {
		return Sent{}, errors.New("group conversations can't be replied to through KDE Connect")
	}
	if strings.TrimSpace(text) == "" && len(pictures) == 0 {
		return Sent{}, errors.New("nothing to send")
	}
	var parts []domain.Attachment
	var files []string
	for i, p := range pictures {
		o, err := media.Prepare(p)
		if err != nil {
			return Sent{}, err
		}
		files = append(files, o.Path)
		parts = append(parts, domain.Attachment{ID: fmt.Sprintf("api-%d-%d", time.Now().UnixNano(), i), MIMEType: o.MIME, Filename: o.Name,
			Size: o.Size, LocalPath: o.Path, Width: o.Width, Height: o.Height, State: domain.AttachmentAvailable})
	}
	msg := domain.Message{ID: fmt.Sprintf("api-%d", time.Now().UnixNano()), DeviceID: t.DeviceID, ThreadID: t.ID, Sender: "You", Body: text,
		Timestamp: time.Now(), Direction: domain.Outgoing, Status: domain.Sending, Participants: t.Participants, Attachments: parts}
	for i := range msg.Attachments {
		msg.Attachments[i].MessageID = msg.ID
	}
	if _, err := s.MergeMessages([]domain.Message{msg}); err != nil {
		return Sent{}, err
	}
	sendErr := b.Send(ctx, backend.SendRequest{DeviceID: t.DeviceID, PhoneNumber: people[0].Number, ThreadID: t.ID, Message: text, Attachments: files})
	status := domain.Submitted
	if sendErr != nil {
		status = domain.Failed
	}
	if err := s.MessageStatus(msg.ID, status); err != nil && sendErr == nil {
		return Sent{}, fmt.Errorf("sent, but its status could not be saved: %w", err)
	}
	if sendErr != nil {
		return Sent{}, sendErr
	}
	return Sent{SchemaVersion: SchemaVersion, ID: msg.ID, Thread: t.ID, Status: string(status)}, nil
}
