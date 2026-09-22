package storage

import (
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestConversationMergeUnreadAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	thread := domain.ThreadID("phone", "42")
	now := time.UnixMilli(1700000000000)
	msgs := []domain.Message{{DeviceID: "phone", ThreadID: thread, BackendID: "1", Sender: "12345", Body: "dinner 世界", Timestamp: now, Direction: domain.Incoming, Status: domain.Unknown, Unread: true, Participants: []domain.Participant{domain.ParticipantFor("12345")}}, {DeviceID: "phone", ThreadID: thread, BackendID: "2", Sender: "12345", Body: "same millisecond", Timestamp: now, Direction: domain.Incoming, Status: domain.Unknown, Unread: true}}
	added, err := s.MergeMessages(msgs)
	if err != nil || len(added) != 2 {
		t.Fatalf("%v %v", added, err)
	}
	if err = s.MarkRead(thread, []string{added[0].ID}); err != nil {
		t.Fatal(err)
	}
	added, err = s.MergeMessages(msgs)
	if err != nil || len(added) != 0 {
		t.Fatal("duplicates", err)
	}
	ts, err := s.Threads("phone")
	if err != nil || len(ts) != 1 || ts[0].UnreadCount != 1 {
		t.Fatalf("%+v %v", ts, err)
	}
	if err = s.ThreadTheme(thread, "rose"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts, err = s.Threads("phone")
	if err != nil || ts[0].ThemeID != "rose" || ts[0].UnreadCount != 1 {
		t.Fatalf("%v %v", ts, err)
	}
	found, err := s.Search(thread, "DINNER")
	if err != nil || len(found) != 1 {
		t.Fatalf("search %v %v", found, err)
	}
	other := msgs[0]
	other.DeviceID = "other"
	other.ThreadID = domain.ThreadID("other", "42")
	if _, err = s.MergeMessages([]domain.Message{other}); err != nil {
		t.Fatal(err)
	}
	ms, err := s.Messages(thread, 100)
	if err != nil || len(ms) != 2 {
		t.Fatalf("%v %v", ms, err)
	}
}
func TestOutgoingEchoReconciliation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	m := domain.Message{ID: "local-1", DeviceID: "phone", ThreadID: domain.ThreadID("phone", "1"), Sender: "You", Body: "on my way", Timestamp: time.Now(), Direction: domain.Outgoing, Status: domain.Submitted}
	if _, err = s.MergeMessages([]domain.Message{m}); err != nil {
		t.Fatal(err)
	}
	m.ID = ""
	m.BackendID = "23"
	m.Status = domain.Sent
	if _, err = s.MergeMessages([]domain.Message{m}); err != nil {
		t.Fatal(err)
	}
	ms, err := s.Messages(m.ThreadID, 100)
	if err != nil || len(ms) != 1 || ms[0].BackendID != "23" {
		t.Fatalf("%+v %v", ms, err)
	}
}

// A contact saved on the phone without a country code still names a thread whose
// address arrived in E.164, but only while the match is unambiguous.
func TestSyncedContactsMatchAcrossNumberFormats(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	thread := domain.ThreadID("phone", "9")
	msg := domain.Message{DeviceID: "phone", ThreadID: thread, Sender: "+18165550182", Body: "hi",
		Timestamp: time.UnixMilli(1700000000000), Direction: domain.Incoming, Status: domain.Unknown,
		Participants: []domain.Participant{domain.ParticipantFor("+18165550182")}}
	if _, err = s.MergeMessages([]domain.Message{msg}); err != nil {
		t.Fatal(err)
	}
	name := func() string {
		ts, err := s.Threads("phone")
		if err != nil || len(ts) != 1 {
			t.Fatalf("%+v %v", ts, err)
		}
		return ts[0].DisplayName
	}
	if got := name(); got != "+18165550182" {
		t.Fatalf("precondition: %q", got)
	}

	// Stored without a country code, as Android commonly does.
	if err = s.ReplaceSyncedContacts("phone", []contacts.Synced{{UID: "1", Name: "Rina", PhoneNumber: "8165550182", RawNumber: "(816) 555-0182"}}); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "Rina" {
		t.Fatalf("format-tolerant match failed: %q", got)
	}

	// Two different people sharing the national portion must not be guessed at.
	if err = s.ReplaceSyncedContacts("phone", []contacts.Synced{
		{UID: "1", Name: "Rina", PhoneNumber: "8165550182", RawNumber: "8165550182"},
		{UID: "2", Name: "Someone Else", PhoneNumber: "+448165550182", RawNumber: "+448165550182"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "+18165550182" {
		t.Fatalf("ambiguous match was guessed: %q", got)
	}

	// An exact match always wins over the looser one.
	if err = s.ReplaceSyncedContacts("phone", []contacts.Synced{
		{UID: "1", Name: "Loose", PhoneNumber: "8165550182", RawNumber: "8165550182"},
		{UID: "2", Name: "Exact", PhoneNumber: "+18165550182", RawNumber: "+18165550182"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "Exact" {
		t.Fatalf("exact match did not win: %q", got)
	}
}

// The group flag follows the participants actually known, so a thread once
// mislabelled from duplicated addresses becomes answerable again.
func TestGroupFlagFollowsParticipants(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	thread := domain.ThreadID("phone", "7")
	one := domain.Message{DeviceID: "phone", ThreadID: thread, BackendID: "1", Sender: "+18165550182",
		Body: "hi", Timestamp: time.UnixMilli(1700000000000), Direction: domain.Incoming, Status: domain.Unknown,
		IsGroup: true, Participants: []domain.Participant{domain.ParticipantFor("+18165550182")}}
	if _, err = s.MergeMessages([]domain.Message{one}); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Threads("phone")
	if err != nil || len(ts) != 1 {
		t.Fatalf("%+v %v", ts, err)
	}
	if ts[0].IsGroup {
		t.Fatal("one participant was treated as a group")
	}

	// A later message naming a second person does make it a group.
	two := one
	two.BackendID = "2"
	two.Participants = append(two.Participants, domain.ParticipantFor("+15559876543"))
	if _, err = s.MergeMessages([]domain.Message{two}); err != nil {
		t.Fatal(err)
	}
	if ts, err = s.Threads("phone"); err != nil || !ts[0].IsGroup {
		t.Fatalf("second participant did not make a group: %+v %v", ts, err)
	}
}
