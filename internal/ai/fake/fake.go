// Package fake provides a deterministic WritingAssistant for tests.
package fake

import (
	"context"
	"github.com/allisonhere/tidesms/internal/ai"
	"sync"
	"time"
)

// Assistant answers with whatever the test configured, and can simulate an
// unavailable provider, a slow local model, or malformed output (by returning
// a nil Changes slice with an error, or Changes the test chooses).
type Assistant struct {
	mu            sync.Mutex
	ReviewResult  ai.ReviewResult
	ReviewError   error
	RewriteResult ai.RewriteResult
	RewriteError  error
	ReviewDelay   time.Duration
	RewriteDelay  time.Duration
	ReviewCalls   []ai.ReviewRequest
	RewriteCalls  []ai.RewriteRequest
}

func (a *Assistant) Review(ctx context.Context, req ai.ReviewRequest) (ai.ReviewResult, error) {
	a.mu.Lock()
	a.ReviewCalls = append(a.ReviewCalls, req)
	delay, res, err := a.ReviewDelay, a.ReviewResult, a.ReviewError
	a.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ai.ReviewResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return res, err
}

func (a *Assistant) Rewrite(ctx context.Context, req ai.RewriteRequest) (ai.RewriteResult, error) {
	a.mu.Lock()
	a.RewriteCalls = append(a.RewriteCalls, req)
	delay, res, err := a.RewriteDelay, a.RewriteResult, a.RewriteError
	a.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ai.RewriteResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return res, err
}
