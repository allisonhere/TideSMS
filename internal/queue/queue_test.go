package queue

import (
	"testing"
	"time"
)

func TestBackoffGrowsAndCaps(t *testing.T) {
	if got := Backoff(1); got != 5*time.Second {
		t.Fatalf("attempt 1 = %v", got)
	}
	if got := Backoff(2); got != 10*time.Second {
		t.Fatalf("attempt 2 = %v", got)
	}
	if got := Backoff(1); got == Backoff(2) {
		t.Fatal("backoff did not grow")
	}
	if got := Backoff(100); got != backoffCap {
		t.Fatalf("backoff did not cap: %v", got)
	}
	if got := Backoff(0); got != 5*time.Second {
		t.Fatalf("attempt 0 = %v", got)
	}
}

func TestNextAttemptStopsAtMax(t *testing.T) {
	if _, ok := NextAttempt(4, 5); !ok {
		t.Fatal("below max should retry")
	}
	if _, ok := NextAttempt(5, 5); ok {
		t.Fatal("at max should stop")
	}
	if _, ok := NextAttempt(9, 0); !ok {
		t.Fatal("unlimited budget should retry")
	}
}

func TestDueRespectsStateAndDelay(t *testing.T) {
	now := time.Now()
	item := Item{State: Queued}
	if !item.Due(now) {
		t.Fatal("queued with no delay should be due")
	}
	item.NextAttemptAt = now.Add(time.Minute)
	if item.Due(now) {
		t.Fatal("future retry should not be due")
	}
	if !item.Due(now.Add(time.Minute)) {
		t.Fatal("retry should be due once reached")
	}
	item.State = Sending
	if item.Due(now.Add(time.Hour)) {
		t.Fatal("sending is not due for another attempt")
	}
}

func TestTransitionGuardsTerminalStates(t *testing.T) {
	if CanTransition(Sent, Queued) {
		t.Fatal("a sent message must not return to the queue")
	}
	if !CanTransition(Queued, Sending) || !CanTransition(Sending, Sent) {
		t.Fatal("normal forward transitions rejected")
	}
	if !CanTransition(Failed, Queued) {
		t.Fatal("a failed message may be retried by hand")
	}
	if !CanTransition(Sending, Paused) {
		t.Fatal("an interrupted send may be paused")
	}
}
