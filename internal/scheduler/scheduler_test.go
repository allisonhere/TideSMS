package scheduler

import (
	"github.com/allisonhere/tidesms/internal/queue"
	"testing"
	"time"
)

func TestDueOnlyAfterSendTime(t *testing.T) {
	at := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	i := Item{State: Scheduled, SendAfter: at}
	if i.Due(at.Add(-time.Second)) {
		t.Fatal("released early")
	}
	if !i.Due(at) {
		t.Fatal("not released when due")
	}
	i.State = Queued
	if i.Due(at.Add(time.Hour)) {
		t.Fatal("already released should not be due again")
	}
}

func TestPresetsResolveForward(t *testing.T) {
	now := time.Date(2026, 9, 23, 9, 15, 0, 0, time.UTC)
	for _, p := range Presets {
		if got := p.At(now); !got.After(now) {
			t.Errorf("preset %q resolved to %v, not after now", p.Label, got)
		}
	}
}

type memStore struct {
	items map[string]Item
}

func (m *memStore) DueScheduled(now time.Time) ([]Item, error) {
	var out []Item
	for _, i := range m.items {
		if i.Due(now) {
			out = append(out, i)
		}
	}
	return out, nil
}
func (m *memStore) ClaimScheduled(id string, now time.Time) (bool, error) {
	i, ok := m.items[id]
	if !ok || i.State != Scheduled || i.SendAfter.After(now) {
		return false, nil
	}
	i.State = Queued
	m.items[id] = i
	return true, nil
}
func (m *memStore) UpdateScheduled(i Item) error { m.items[i.ID] = i; return nil }

type memQueue struct{ items []queue.Item }

func (m *memQueue) Enqueue(i queue.Item) error { m.items = append(m.items, i); return nil }

func TestReleaserMovesDueOnceAndKeepsId(t *testing.T) {
	at := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	store := &memStore{items: map[string]Item{"s1": {ID: "s1", State: Scheduled, SendAfter: at, Recipient: "+1", Body: "x"}}}
	q := &memQueue{}
	r := &Releaser{Store: store, Queue: q}

	n, err := r.Release(at.Add(-time.Hour))
	if err != nil || n != 0 || len(q.items) != 0 {
		t.Fatalf("released early: n=%d err=%v", n, err)
	}
	n, err = r.Release(at)
	if err != nil || n != 1 || len(q.items) != 1 {
		t.Fatalf("did not release: n=%d err=%v", n, err)
	}
	if q.items[0].ID != "s1" {
		t.Fatalf("queue id = %q, want the scheduled id", q.items[0].ID)
	}
	// A duplicate tick must not enqueue a second copy.
	if n, _ := r.Release(at.Add(time.Minute)); n != 0 || len(q.items) != 1 {
		t.Fatalf("duplicate release: n=%d items=%d", n, len(q.items))
	}
}
