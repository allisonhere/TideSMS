// Package scheduler decides when a scheduled message has become due. It stores
// absolute instants; nothing here depends on the wall clock except through an
// injected time.
package scheduler

import "time"

type State string

const (
	Scheduled State = "scheduled"
	Queued    State = "queued"
	Sent      State = "sent"
	Failed    State = "failed"
	Canceled  State = "canceled"
)

// Item is one scheduled message.
type Item struct {
	ID           string
	DeviceID     string
	ThreadID     string
	Recipient    string
	Body         string
	SendAfter    time.Time
	State        State
	AttemptCount int
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Due reports whether the message should be released at now. A message is due
// only when its absolute send time has arrived; reconnecting the phone never
// brings the time forward.
func (i Item) Due(now time.Time) bool {
	return i.State == Scheduled && !i.SendAfter.After(now)
}

// Editable reports whether the user may still change or cancel it.
func (i Item) Editable() bool { return i.State == Scheduled }

// Presets are relative labels offered by the schedule picker. They resolve to
// absolute times at the moment they are chosen.
type Preset struct {
	Label string
	At    func(now time.Time) time.Time
}

// Presets is the keyboard-first list shown by the composer's schedule action.
var Presets = []Preset{
	{"In 30 minutes", func(now time.Time) time.Time { return now.Add(30 * time.Minute) }},
	{"In 1 hour", func(now time.Time) time.Time { return now.Add(time.Hour) }},
	{"This evening", func(now time.Time) time.Time { return atHour(now, 18) }},
	{"Tomorrow morning", func(now time.Time) time.Time { return atHour(now.AddDate(0, 0, 1), 8) }},
}

func atHour(now time.Time, hour int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
}
