package storage

import (
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/queue"
	"github.com/allisonhere/tidesms/internal/scheduler"
	"github.com/allisonhere/tidesms/internal/search"
	"path/filepath"
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestQueueRoundTripAndClaimIsIdempotent(t *testing.T) {
	s := openStore(t)
	now := time.UnixMilli(1_700_000_000_000)
	item := queue.Item{ID: "q1", DeviceID: "phone", Recipient: "+15551230000", Body: "hello", State: queue.Queued, CreatedAt: now, UpdatedAt: now}
	if err := s.Enqueue(item); err != nil {
		t.Fatal(err)
	}
	all, err := s.Queue()
	if err != nil || len(all) != 1 || all[0].Recipient != "+15551230000" {
		t.Fatalf("queue = %+v err=%v", all, err)
	}
	due, err := s.DueQueue(now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %+v err=%v", due, err)
	}
	ok, err := s.ClaimQueue("q1", now)
	if err != nil || !ok {
		t.Fatalf("claim = %v err=%v", ok, err)
	}
	if again, _ := s.ClaimQueue("q1", now); again {
		t.Fatal("double claim succeeded")
	}
	got, ok, err := s.QueueItem("q1")
	if err != nil || !ok {
		t.Fatalf("item lookup: ok=%v err=%v", ok, err)
	}
	if got.State != queue.Sending || got.AttemptCount != 1 {
		t.Fatalf("after claim: %+v", got)
	}
	got.State = queue.Sent
	if err = s.UpdateQueue(got); err != nil {
		t.Fatal(err)
	}
	if due, _ = s.DueQueue(now, 10); len(due) != 0 {
		t.Fatal("sent item still due")
	}
	if n, err := s.CountQueue(queue.Sent); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	if err = s.RemoveQueue("q1"); err != nil {
		t.Fatal(err)
	}
	if all, _ = s.Queue(); len(all) != 0 {
		t.Fatal("remove failed")
	}
}

func TestOfflineWaitFlagRoundTrips(t *testing.T) {
	s := openStore(t)
	now := time.UnixMilli(1_700_000_000_000)
	item := queue.Item{ID: "q1", DeviceID: "phone", Recipient: "+15551230000", Body: "hi", State: queue.Queued, CreatedAt: now, OfflineWait: true, NextAttemptAt: now.Add(time.Minute)}
	if err := s.Enqueue(item); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.QueueItem("q1")
	if err != nil || !ok || !got.OfflineWait {
		t.Fatalf("offline flag lost: %+v ok=%v err=%v", got, ok, err)
	}
	if err := s.ReleaseOfflineWaits(); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.QueueItem("q1")
	if got.OfflineWait || !got.NextAttemptAt.IsZero() {
		t.Fatalf("offline wait not cleared: %+v", got)
	}
	if due, _ := s.DueQueue(now, 10); len(due) != 1 {
		t.Fatal("item should be due after the offline wait clears")
	}
}

func TestScheduledRoundTripAndClaim(t *testing.T) {
	s := openStore(t)
	now := time.UnixMilli(1_700_000_000_000)
	at := now.Add(time.Hour)
	item := scheduler.Item{ID: "s1", DeviceID: "phone", Recipient: "+15551230000", Body: "later", SendAfter: at, State: scheduler.Scheduled, CreatedAt: now, UpdatedAt: now}
	if err := s.Schedule(item); err != nil {
		t.Fatal(err)
	}
	if due, err := s.DueScheduled(now); err != nil || len(due) != 0 {
		t.Fatalf("released early: %+v err=%v", due, err)
	}
	if n, err := s.CountScheduled(); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	if ok, _ := s.ClaimScheduled("s1", now); ok {
		t.Fatal("claimed before due")
	}
	ok, err := s.ClaimScheduled("s1", at)
	if err != nil || !ok {
		t.Fatalf("claim=%v err=%v", ok, err)
	}
	if again, _ := s.ClaimScheduled("s1", at); again {
		t.Fatal("scheduled double claim")
	}
	got, found, _ := s.ScheduledItem("s1")
	if !found || got.State != scheduler.Queued {
		t.Fatalf("after claim: %+v found=%v", got, found)
	}
	got.State = scheduler.Sent
	if err = s.UpdateScheduled(got); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountScheduled(); n != 0 {
		t.Fatal("sent still counts as scheduled")
	}
	if err = s.RemoveScheduled("s1"); err != nil {
		t.Fatal(err)
	}
}

func TestPreferencesAndContactSources(t *testing.T) {
	s := openStore(t)
	if _, _, _, ok, err := s.AIPolicy(ScopeThread, "t1"); err != nil || ok {
		t.Fatalf("unexpected default: ok=%v err=%v", ok, err)
	}
	if err := s.SetAIPolicy(ScopeThread, "t1", "disabled", "", ""); err != nil {
		t.Fatal(err)
	}
	if p, _, _, ok, _ := s.AIPolicy(ScopeThread, "t1"); !ok || p != "disabled" {
		t.Fatalf("policy = %q ok=%v", p, ok)
	}
	if err := s.ClearAIPolicy(ScopeThread, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok, _ := s.AIPolicy(ScopeThread, "t1"); ok {
		t.Fatal("clear failed")
	}

	if err := s.SetNotificationMode(ScopeContact, "amy", "muted"); err != nil {
		t.Fatal(err)
	}
	if m, ok, _ := s.NotificationMode(ScopeContact, "amy"); !ok || m != "muted" {
		t.Fatalf("mode = %q ok=%v", m, ok)
	}

	src := contacts.Source{ContactID: "amy", Source: contacts.SourcePhone, SourceID: "p1", DisplayName: "Allison Bayless", PhoneNumber: "+15551234567"}
	if err := s.SaveContactSource(src); err != nil {
		t.Fatal(err)
	}
	list, err := s.ContactSources("amy")
	if err != nil || len(list) != 1 || list[0].DisplayName != "Allison Bayless" {
		t.Fatalf("sources = %+v err=%v", list, err)
	}
	ids, err := s.ContactIDsByNumber("+15551234567")
	if err != nil || len(ids) != 1 || ids[0] != "amy" {
		t.Fatalf("ids = %v err=%v", ids, err)
	}
}

func TestGlobalSearchUsesFTSAndFilters(t *testing.T) {
	s := openStore(t)
	base := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)
	p := []domain.Participant{domain.ParticipantFor("+15551234567")}
	thread := domain.Thread{ID: domain.ThreadID("phone", "amy"), DeviceID: "phone", BackendID: "amy", DisplayName: "Amy", Participants: p}
	if err := s.MergeThreads([]domain.Thread{thread}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeMessages([]domain.Message{
		{ID: "m1", DeviceID: "phone", ThreadID: thread.ID, BackendID: "b1", Sender: "+15551234567", Body: "Your dentist appointment is at 2", Timestamp: base, Direction: domain.Incoming, Status: domain.Unknown, Participants: p},
		{ID: "m2", DeviceID: "phone", ThreadID: thread.ID, BackendID: "b2", Sender: "+15551234567", Body: "How did the dentist appointment go?", Timestamp: base.Add(48 * time.Hour), Direction: domain.Incoming, Status: domain.Unknown, Participants: p},
	}); err != nil {
		t.Fatal(err)
	}

	all, err := s.GlobalSearch(search.Parse("dentist"), 50)
	if err != nil || len(all) != 2 {
		t.Fatalf("dentist = %d err=%v", len(all), err)
	}
	// Newest first.
	if all[0].Body != "How did the dentist appointment go?" {
		t.Fatalf("order wrong: %+v", all)
	}
	// from: matches the thread's display name.
	if got, err := s.GlobalSearch(search.Parse("from:Amy dentist"), 50); err != nil || len(got) != 2 {
		t.Fatalf("from filter = %d err=%v", len(got), err)
	}
	if got, err := s.GlobalSearch(search.Parse("from:Bob dentist"), 50); err != nil || len(got) != 0 {
		t.Fatalf("wrong sender matched: %d err=%v", len(got), err)
	}
	// Date filters.
	if got, err := s.GlobalSearch(search.Parse("dentist after:2026-09-20"), 50); err != nil || len(got) != 1 {
		t.Fatalf("after filter = %d err=%v", len(got), err)
	}
	if got, err := s.GlobalSearch(search.Parse("dentist before:2026-09-20"), 50); err != nil || len(got) != 1 {
		t.Fatalf("before filter = %d err=%v", len(got), err)
	}
	// Filter-only query returns matches without FTS.
	if got, err := s.GlobalSearch(search.Parse("from:Amy"), 50); err != nil || len(got) != 2 {
		t.Fatalf("filter only = %d err=%v", len(got), err)
	}
	// A term that does not occur returns nothing.
	if got, err := s.GlobalSearch(search.Parse("zebra"), 50); err != nil || len(got) != 0 {
		t.Fatalf("absent term = %d err=%v", len(got), err)
	}
}

func TestBubbleThemesRoundTrip(t *testing.T) {
	s := openStore(t)
	// Contacts keep their per-direction bubble palettes.
	c := contacts.Contact{ID: "amy", Name: "Amy", PhoneNumber: "+15551234567", Theme: "nord", ThemeIn: "dracula", ThemeOut: "gruvbox-light"}
	if err := s.SaveContact(c); err != nil {
		t.Fatal(err)
	}
	cs, err := s.Contacts()
	if err != nil || len(cs) != 1 {
		t.Fatalf("contacts = %+v err=%v", cs, err)
	}
	if got := cs[0]; got.Theme != "nord" || got.ThemeIn != "dracula" || got.ThemeOut != "gruvbox-light" {
		t.Fatalf("contact bubbles lost: %+v", got)
	}

	// Threads carry the same fields, preserved across a re-sync.
	thread := domain.Thread{ID: domain.ThreadID("phone", "amy"), DeviceID: "phone", BackendID: "amy", DisplayName: "Amy", Participants: []domain.Participant{domain.ParticipantFor("+15551234567")}}
	if err = s.MergeThreads([]domain.Thread{thread}); err != nil {
		t.Fatal(err)
	}
	if err = s.ThreadBubbleThemes(thread.ID, "coral-sunset", "one-dark"); err != nil {
		t.Fatal(err)
	}
	if err = s.MergeThreads([]domain.Thread{thread}); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Threads("phone")
	if err != nil || len(ts) != 1 {
		t.Fatalf("threads = %+v err=%v", ts, err)
	}
	if ts[0].ThemeIn != "coral-sunset" || ts[0].ThemeOut != "one-dark" {
		t.Fatalf("thread bubbles lost on resync: %+v", ts[0])
	}
}

func TestSearchIndexFollowsUpdates(t *testing.T) {
	s := openStore(t)
	base := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)
	p := []domain.Participant{domain.ParticipantFor("+15551234567")}
	m := domain.Message{ID: "m1", DeviceID: "phone", ThreadID: domain.ThreadID("phone", "amy"), BackendID: "b1", Sender: "+15551234567", Body: "original text", Timestamp: base, Direction: domain.Incoming, Status: domain.Unknown, Participants: p}
	if _, err := s.MergeMessages([]domain.Message{m}); err != nil {
		t.Fatal(err)
	}
	m.Body = "revised wording"
	if _, err := s.MergeMessages([]domain.Message{m}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GlobalSearch(search.Parse("original"), 50); len(got) != 0 {
		t.Fatalf("stale body still indexed: %+v", got)
	}
	if got, _ := s.GlobalSearch(search.Parse("revised"), 50); len(got) != 1 {
		t.Fatalf("updated body not indexed: %+v", got)
	}
}
