// Package domain contains transport-independent conversation data.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/allisonhere/tidesms/internal/contacts"
	"strings"
	"time"
)

type Direction string

const (
	Incoming Direction = "incoming"
	Outgoing Direction = "outgoing"
)

type Status string

const (
	Unknown   Status = "unknown"
	Queued    Status = "queued"
	Scheduled Status = "scheduled"
	Sending   Status = "sending"
	Sent      Status = "sent"
	Failed    Status = "failed"
	Delivered Status = "delivered"
	Read      Status = "read"
	Submitted Status = "submitted"
)

type Participant struct{ RawNumber, Number, Name string }

func ParticipantFor(raw string) Participant {
	n, err := contacts.Normalize(raw)
	if err != nil {
		n = raw
	}
	return Participant{RawNumber: raw, Number: n}
}

// DedupeParticipants collapses addresses that name the same person written more
// than once, or written in different formats such as 8165550182 and
// +18165550182. Phones commonly do both, and without this a one-to-one thread
// is mistaken for a group: its sender becomes anonymous and replying is refused.
func DedupeParticipants(ps []Participant) []Participant {
	seen := make(map[string]bool, len(ps))
	out := make([]Participant, 0, len(ps))
	for _, p := range ps {
		key := contacts.MatchKey(p.Number)
		if key == "" {
			key = p.Number
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

type Thread struct {
	ID, DeviceID, BackendID, DisplayName, LastMessage, ThemeID, LastBackendID string
	// ThemeIn and ThemeOut are this thread's received and sent bubble palettes.
	// Empty keeps the derived surface.
	ThemeIn, ThemeOut string
	Participants      []Participant
	LastTimestamp     time.Time
	UnreadCount       int
	IsGroup           bool
	// Pinned threads sort first; archived threads leave the main list.
	Pinned   bool
	Archived bool
}
type Message struct {
	ID, DeviceID, ThreadID, BackendID, Sender, Body string
	Timestamp                                       time.Time
	Direction                                       Direction
	Status                                          Status
	Unread                                          bool
	Participants                                    []Participant
	IsGroup                                         bool
	// Attachments are media parts. They may be metadata-only until fetched; a
	// conversation renders without waiting for them.
	Attachments []Attachment
}

// AttachmentState tracks how much of an attachment is available locally.
type AttachmentState string

const (
	// AttachmentMetadata means only the part's description is known.
	AttachmentMetadata AttachmentState = "metadata"
	// AttachmentDownloading is in progress.
	AttachmentDownloading AttachmentState = "downloading"
	// AttachmentAvailable is present at LocalPath.
	AttachmentAvailable AttachmentState = "available"
	// AttachmentFailed could not be fetched.
	AttachmentFailed AttachmentState = "failed"
)

// Attachment is one media part of a message.
type Attachment struct {
	ID, MessageID, MIMEType, Filename string
	Size                              int64
	LocalPath, RemoteID               string
	// PartID is the backend's part number, used to request the file.
	PartID        int64
	Width, Height int
	State         AttachmentState
}

// HasMedia reports whether a message carries any attachment.
func (m Message) HasMedia() bool { return len(m.Attachments) > 0 }

func ThreadID(device, id string) string { return "thread:" + device + ":" + id }
func (m Message) StableID() string {
	if m.ID != "" {
		return m.ID
	}
	key := []any{m.DeviceID, m.ThreadID, m.BackendID}
	if m.BackendID == "" {
		key = []any{m.DeviceID, m.ThreadID, m.Sender, m.Timestamp.UnixMilli(), m.Body, m.Direction}
	}
	b, _ := json.Marshal(key)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (m Message) Thread() Thread {
	backendID := strings.TrimPrefix(m.ThreadID, ThreadID(m.DeviceID, ""))
	if strings.HasPrefix(backendID, "local-") {
		backendID = ""
	}
	return Thread{ID: m.ThreadID, DeviceID: m.DeviceID, BackendID: backendID, Participants: m.Participants, LastMessage: m.Body, LastTimestamp: m.Timestamp, IsGroup: m.IsGroup}
}

type MessageQuery struct{ Offset, Limit int }
type Event struct {
	Kind      string
	Message   *Message
	ThreadID  string
	Connected bool
	Err       error
}

const (
	EventMessage    = "message"
	EventConnection = "connection"
	EventRefresh    = "refresh"
	EventError      = "error"
)
