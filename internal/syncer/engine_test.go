package syncer

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend/fake"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

const device = "phone"

var thread = domain.ThreadID(device, "1")

func message(backendID string, minute int, body string) domain.Message {
	m := domain.Message{DeviceID: device, ThreadID: thread, BackendID: backendID, Sender: "+15551234567", Body: body,
		Timestamp: time.Date(2026, 9, 22, 10, minute, 0, 0, time.UTC),
		Direction: domain.Incoming, Status: domain.Unknown, Unread: true,
		Participants: []domain.Participant{domain.ParticipantFor("+15551234567")}}
	m.ID = m.StableID()
	return m
}

func fixture(t *testing.T) (*fake.Backend, *storage.Store, *Engine) {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	b := fake.New()
	b.History[thread] = []domain.Message{message("1", 1, "one"), message("2", 2, "two"), message("3", 3, "three")}
	b.ThreadList = []domain.Thread{{ID: thread, DeviceID: device, BackendID: "1", DisplayName: "Amy",
		LastMessage: "three", LastTimestamp: b.History[thread][2].Timestamp, LastBackendID: "3",
		Participants: []domain.Participant{domain.ParticipantFor("+15551234567")}}}
	return b, s, &Engine{Backend: b, Store: s, PageSize: 2}
}

func stored(t *testing.T, s *storage.Store) []domain.Message {
	t.Helper()
	ms, err := s.Messages(thread, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// A first sync takes one recent page; a repeat sync whose watermark still matches
// asks the phone for nothing; new activity replays only until the watermark.
func TestRefreshIsIncrementalAndNeverDuplicates(t *testing.T) {
	b, s, e := fixture(t)
	ctx := context.Background()
	var progress []int
	if err := e.Refresh(ctx, device, func(n int) { progress = append(progress, n) }); err != nil {
		t.Fatal(err)
	}
	if ms := stored(t, s); len(ms) != 2 || ms[1].Body != "three" {
		t.Fatalf("initial page: %+v", ms)
	}
	if len(progress) < 2 || progress[len(progress)-1] != 1 {
		t.Fatalf("progress not reported: %v", progress)
	}
	anchor, _, err := s.LastSync(thread)
	if err != nil || anchor != "3" {
		t.Fatalf("watermark %q %v", anchor, err)
	}

	pages := len(b.Requests)
	if err = e.Refresh(ctx, device, nil); err != nil {
		t.Fatal(err)
	}
	if len(b.Requests) != pages {
		t.Fatalf("unchanged thread re-downloaded: %+v", b.Requests[pages:])
	}
	if ms := stored(t, s); len(ms) != 2 {
		t.Fatalf("repeat sync changed the cache: %+v", ms)
	}

	b.History[thread] = append(b.History[thread], message("4", 4, "four"))
	b.ThreadList[0].LastBackendID = "4"
	b.ThreadList[0].LastTimestamp = b.History[thread][3].Timestamp
	if err = e.Refresh(ctx, device, nil); err != nil {
		t.Fatal(err)
	}
	ms := stored(t, s)
	if len(ms) != 3 || ms[2].Body != "four" {
		t.Fatalf("catch-up: %+v", ms)
	}
	if got := len(b.Requests) - pages; got != 1 {
		t.Fatalf("replayed %d pages past the watermark", got)
	}

	// Older history is fetched on demand and merges beneath what is already cached.
	if err = e.Older(ctx, b.ThreadList[0], 3, 2); err != nil {
		t.Fatal(err)
	}
	if ms = stored(t, s); len(ms) != 4 || ms[0].Body != "one" {
		t.Fatalf("older page: %+v", ms)
	}
	if err = e.Older(ctx, b.ThreadList[0], 0, 4); err != nil {
		t.Fatal(err)
	}
	if ms = stored(t, s); len(ms) != 4 {
		t.Fatalf("overlapping page duplicated messages: %+v", ms)
	}
}

// Without backend identifiers, identical replays must collapse while genuinely
// distinct messages sharing a timestamp must not.
func TestFingerprintDeduplicationKeepsDistinctMessages(t *testing.T) {
	_, s, _ := fixture(t)
	a := message("", 5, "bring dessert")
	b := message("", 5, "and plates")
	if added, err := s.MergeMessages([]domain.Message{a, b}); err != nil || len(added) != 2 {
		t.Fatalf("%+v %v", added, err)
	}
	if added, err := s.MergeMessages([]domain.Message{a, b, a}); err != nil || len(added) != 0 {
		t.Fatalf("replay added %+v %v", added, err)
	}
	if ms := stored(t, s); len(ms) != 2 {
		t.Fatalf("%+v", ms)
	}
}

// A phone that disappears mid-sync leaves the cache intact and reports the failure.
func TestRefreshFailureLeavesCacheUsable(t *testing.T) {
	b, s, e := fixture(t)
	ctx := context.Background()
	if err := e.Refresh(ctx, device, nil); err != nil {
		t.Fatal(err)
	}
	b.Connection(false)
	b.ThreadList[0].LastBackendID = "4"
	err := e.Refresh(ctx, device, nil)
	if err == nil {
		t.Fatal("offline refresh reported success")
	}
	if ms := stored(t, s); len(ms) != 2 {
		t.Fatalf("cache disturbed by failed sync: %+v", ms)
	}
	ts, err := s.Threads(device)
	if err != nil || len(ts) != 1 || ts[0].DisplayName != "Amy" {
		t.Fatalf("threads unusable offline: %+v %v", ts, err)
	}

	b.Connection(true)
	b.History[thread] = append(b.History[thread], message("4", 4, "four"))
	if err = e.Refresh(ctx, device, nil); err != nil {
		t.Fatal(err)
	}
	if ms := stored(t, s); len(ms) != 3 {
		t.Fatalf("reconnect did not catch up: %+v", ms)
	}
}

// A cancelled context stops the walk instead of looping against a dead phone.
func TestRefreshHonoursCancellation(t *testing.T) {
	b, _, e := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Refresh(ctx, device, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if b.ThreadCalls != 0 {
		t.Fatal("cancelled sync still queried the phone")
	}
}
