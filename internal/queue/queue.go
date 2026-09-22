// Package queue models the durable outgoing-message queue shared by the TUI
// and the optional background service. It holds the pure state machine; SQL
// lives in internal/storage.
package queue

import "time"

type State string

const (
	Queued  State = "queued"
	Sending State = "sending"
	Sent    State = "sent"
	Failed  State = "failed"
	Paused  State = "paused"
)

// Item is one queued message.
type Item struct {
	ID           string
	DeviceID     string
	ThreadID     string
	Recipient    string
	Body         string
	State        State
	AttemptCount int
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// NextAttemptAt is when the item becomes eligible again after a failure.
	// The zero value means immediately.
	NextAttemptAt time.Time
	LastAttemptAt time.Time
}

// Due reports whether the item may be attempted at now: queued, and past any
// retry delay.
func (i Item) Due(now time.Time) bool {
	if i.State != Queued {
		return false
	}
	return !i.NextAttemptAt.After(now)
}

// Terminal reports whether no further automatic action will happen.
func (s State) Terminal() bool { return s == Sent || s == Failed }

// CanTransition reports whether a state change is legal. It exists so a stray
// duplicate reconnect cannot move a sent message back to queued and send it a
// second time.
func CanTransition(from, to State) bool {
	if from == to {
		return true
	}
	switch from {
	case Queued:
		return to == Sending || to == Paused || to == Failed
	case Sending:
		return to == Sent || to == Failed || to == Queued || to == Paused
	case Failed:
		return to == Queued || to == Paused
	case Paused:
		return to == Queued || to == Failed
	default:
		return false
	}
}

const (
	backoffBase = 5 * time.Second
	backoffCap  = 30 * time.Minute
)

// Backoff is the delay before the attempt following attempts failed attempts.
// attempts must be at least 1; values below that are treated as 1.
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := backoffBase
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= backoffCap {
			return backoffCap
		}
	}
	if d > backoffCap {
		return backoffCap
	}
	return d
}

// NextAttempt returns the delay before retrying after attempts failures.
// Reaching max leaves the item failed instead.
func NextAttempt(attempts, max int) (time.Duration, bool) {
	if max > 0 && attempts >= max {
		return 0, false
	}
	return Backoff(attempts), true
}
