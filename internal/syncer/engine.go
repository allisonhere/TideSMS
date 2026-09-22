// Package syncer owns backend-to-cache synchronization, independent of the UI.
package syncer

import (
	"context"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/domain"
	"sync"
	"time"
)

type Cache interface {
	MergeThreads([]domain.Thread) error
	MergeMessages([]domain.Message) ([]domain.Message, error)
	LastSync(string) (string, time.Time, error)
	SaveSync(string, string, time.Time) error
}
type Engine struct {
	Backend  backend.ConversationBackend
	Store    Cache
	PageSize int
	mu       sync.Mutex
}

// Refresh replays recent pages only until each saved watermark is encountered.
// No timestamp-only uniqueness or watermark filter is used, preserving ties.
func (e *Engine) Refresh(ctx context.Context, device string, progress func(int)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	threads, err := e.Backend.Threads(ctx, device)
	if err != nil {
		return err
	}
	if err = e.Store.MergeThreads(threads); err != nil {
		return err
	}
	if progress != nil {
		progress(0)
	}
	for i, t := range threads {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		anchor, _, err := e.Store.LastSync(t.ID)
		if err != nil {
			return err
		}
		if anchor != "" && anchor == t.LastBackendID {
			continue
		}
		for offset := 0; ; {
			ms, err := e.Backend.Messages(ctx, device, t.BackendID, domain.MessageQuery{Offset: offset, Limit: max(1, e.PageSize)})
			if err != nil {
				return err
			}
			if _, err = e.Store.MergeMessages(ms); err != nil {
				return err
			}
			found := anchor == ""
			for _, m := range ms {
				if m.BackendID == anchor {
					found = true
				}
			}
			if found || len(ms) == 0 {
				break
			}
			offset += len(ms)
		}
		if err = e.Store.SaveSync(t.ID, t.LastBackendID, t.LastTimestamp); err != nil {
			return err
		}
		if progress != nil {
			progress(i + 1)
		}
	}
	return nil
}
func (e *Engine) Older(ctx context.Context, t domain.Thread, offset, limit int) error {
	ms, err := e.Backend.Messages(ctx, t.DeviceID, t.BackendID, domain.MessageQuery{Offset: offset, Limit: limit})
	if err != nil {
		return err
	}
	_, err = e.Store.MergeMessages(ms)
	return err
}
