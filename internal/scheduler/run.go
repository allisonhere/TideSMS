package scheduler

import (
	"github.com/allisonhere/tidesms/internal/clock"
	"github.com/allisonhere/tidesms/internal/queue"
	"time"
)

// Store is the persistence the releaser needs.
type Store interface {
	DueScheduled(now time.Time) ([]Item, error)
	ClaimScheduled(id string, now time.Time) (bool, error)
	UpdateScheduled(Item) error
}

// Queue accepts a released message for delivery.
type Queue interface {
	Enqueue(queue.Item) error
}

// Releaser moves scheduled messages that have come due into the outgoing queue.
// It never touches a message before its time, and an atomic claim means a
// duplicate tick cannot release the same message twice.
type Releaser struct {
	Store Store
	Queue Queue
	Clock clock.Clock
}

// Release hands every due message to the queue and returns how many were moved.
func (r *Releaser) Release(now time.Time) (int, error) {
	due, err := r.Store.DueScheduled(now)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, item := range due {
		claimed, err := r.Store.ClaimScheduled(item.ID, now)
		if err != nil {
			return moved, err
		}
		if !claimed {
			continue
		}
		item.State = Queued
		item.UpdatedAt = now
		if err := r.Store.UpdateScheduled(item); err != nil {
			return moved, err
		}
		// Reuse the scheduled id so the queue result can be written back.
		if err := r.Queue.Enqueue(queue.Item{
			ID:        item.ID,
			DeviceID:  item.DeviceID,
			ThreadID:  item.ThreadID,
			Recipient: item.Recipient,
			Body:      item.Body,
			State:     queue.Queued,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}
