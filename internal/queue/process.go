package queue

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/clock"
	"time"
)

// ErrOffline tells the processor that the phone could not be reached. It does
// not count against the retry budget; the item simply waits.
var ErrOffline = errors.New("phone offline")

// offlineRetry is how long an item waits after an offline result. It is short
// because this is not a failure, just a delay.
const offlineRetry = 30 * time.Second

// Store is the persistence the processor needs.
type Store interface {
	DueQueue(now time.Time, limit int) ([]Item, error)
	ClaimQueue(id string, now time.Time) (bool, error)
	UpdateQueue(Item) error
}

// Sender performs the actual transmission for one item.
type Sender interface {
	Send(ctx context.Context, item Item) error
}

// Result records what happened to one item during a pass.
type Result struct {
	ID  string
	Err error
}

// Processor drains due queue items oldest first. Every attempt is guarded by an
// atomic claim, so duplicate reconnect events cannot send the same message
// twice.
type Processor struct {
	Store       Store
	Sender      Sender
	Clock       clock.Clock
	MaxAttempts int
	// After, when set, observes each item's final state for the pass. It is how
	// the daemon mirrors a scheduled message's outcome back to its row.
	After func(Item)
}

// ProcessOnce attempts up to limit due items and returns one result each.
func (p *Processor) ProcessOnce(ctx context.Context, limit int) ([]Result, error) {
	if p.Clock == nil {
		p.Clock = clock.System{}
	}
	if limit <= 0 {
		limit = 1
	}
	now := p.Clock.Now()
	due, err := p.Store.DueQueue(now, limit)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, item := range due {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		claimed, err := p.Store.ClaimQueue(item.ID, now)
		if err != nil || !claimed {
			continue
		}
		out = append(out, p.attempt(ctx, item, now))
	}
	return out, nil
}

func (p *Processor) attempt(ctx context.Context, item Item, now time.Time) Result {
	// ClaimQueue bumped the attempt count in the database; mirror it here.
	item.AttemptCount++
	item.UpdatedAt = now
	item.LastAttemptAt = now
	item.State = Sending

	err := p.Sender.Send(ctx, item)
	switch {
	case err == nil:
		item.State = Sent
		item.LastError = ""
		item.NextAttemptAt = time.Time{}
		item.OfflineWait = false
	case errors.Is(err, ErrOffline):
		// Not a failure: undo the attempt bump and wait without spending budget.
		item.State = Queued
		item.AttemptCount--
		item.LastError = "waiting for phone"
		item.NextAttemptAt = now.Add(offlineRetry)
		item.OfflineWait = true
	default:
		item.OfflineWait = false
		item.LastError = err.Error()
		if p.MaxAttempts > 0 && item.AttemptCount >= p.MaxAttempts {
			item.State = Failed
			item.NextAttemptAt = time.Time{}
		} else {
			item.State = Queued
			item.NextAttemptAt = now.Add(Backoff(item.AttemptCount))
		}
	}
	if uerr := p.Store.UpdateQueue(item); uerr != nil && err == nil {
		err = uerr
	}
	if p.After != nil {
		p.After(item)
	}
	return Result{ID: item.ID, Err: err}
}
