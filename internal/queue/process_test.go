package queue

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/clock"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu    sync.Mutex
	items map[string]Item
}

func newMemStore(items ...Item) *memStore {
	m := &memStore{items: map[string]Item{}}
	for _, i := range items {
		m.items[i.ID] = i
	}
	return m
}

func (m *memStore) DueQueue(now time.Time, limit int) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for _, i := range m.items {
		if i.Due(now) {
			out = append(out, i)
		}
	}
	// Oldest first, like the SQL implementation.
	for a := 0; a < len(out); a++ {
		for b := a + 1; b < len(out); b++ {
			if out[b].CreatedAt.Before(out[a].CreatedAt) {
				out[a], out[b] = out[b], out[a]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memStore) ClaimQueue(id string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.items[id]
	if !ok || i.State != Queued {
		return false, nil
	}
	i.State = Sending
	i.AttemptCount++
	m.items[id] = i
	return true, nil
}

func (m *memStore) UpdateQueue(i Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[i.ID] = i
	return nil
}

func (m *memStore) get(id string) Item { return m.items[id] }

type fakeSender struct {
	mu    sync.Mutex
	err   error
	calls []string
}

func (f *fakeSender) Send(_ context.Context, item Item) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, item.ID)
	return f.err
}

func baseItem(id string) Item {
	return Item{ID: id, DeviceID: "phone", Recipient: "+15551230000", Body: "hi", State: Queued, CreatedAt: time.Unix(0, 0)}
}

func TestProcessorSendsAndDoesNotRepeat(t *testing.T) {
	store := newMemStore(baseItem("a"))
	sender := &fakeSender{}
	clk := clock.NewFake(time.Unix(1000, 0))
	p := &Processor{Store: store, Sender: sender, Clock: clk, MaxAttempts: 3}

	if _, err := p.ProcessOnce(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if got := store.get("a").State; got != Sent {
		t.Fatalf("state = %s", got)
	}
	// A duplicate reconnect event must not send again.
	if _, err := p.ProcessOnce(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sent %v", sender.calls)
	}
}

func TestProcessorOfflineDoesNotSpendAttempts(t *testing.T) {
	store := newMemStore(baseItem("a"))
	sender := &fakeSender{err: ErrOffline}
	clk := clock.NewFake(time.Unix(1000, 0))
	p := &Processor{Store: store, Sender: sender, Clock: clk, MaxAttempts: 2}

	if _, err := p.ProcessOnce(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	i := store.get("a")
	if i.State != Queued || i.AttemptCount != 0 {
		t.Fatalf("offline should not count: %+v", i)
	}
	if !i.OfflineWait {
		t.Fatal("offline wait should be flagged, not inferred from the error text")
	}
	if i.NextAttemptAt.IsZero() {
		t.Fatal("offline should schedule a short retry")
	}
	// Still not due immediately.
	if _, err := p.ProcessOnce(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("offline retried too soon: %v", sender.calls)
	}
}

func TestProcessorFailsAfterMaxAttempts(t *testing.T) {
	store := newMemStore(baseItem("a"))
	sender := &fakeSender{err: errors.New("transport")}
	clk := clock.NewFake(time.Unix(1000, 0))
	p := &Processor{Store: store, Sender: sender, Clock: clk, MaxAttempts: 3}

	for i := 0; i < 5; i++ {
		if _, err := p.ProcessOnce(context.Background(), 5); err != nil {
			t.Fatal(err)
		}
		if store.get("a").State == Failed {
			break
		}
		i := store.get("a")
		clk.Set(i.NextAttemptAt)
	}
	i := store.get("a")
	if i.State != Failed || i.AttemptCount != 3 {
		t.Fatalf("expected failure at the cap: %+v", i)
	}
	if len(sender.calls) != 3 {
		t.Fatalf("sent %d times", len(sender.calls))
	}
}
