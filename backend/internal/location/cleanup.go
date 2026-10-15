package location

import (
	"context"
	"time"
)

func (s *Service) Cleanup(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM latest_locations WHERE lease_expires_at<=?`, s.now().UnixMilli())
	return e
}

// RunCleanup blocks until cancellation. The owner must wait for it before
// closing the database. onError receives no location or credential data.
func (s *Service) RunCleanup(ctx context.Context, onError func()) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.cleanupLoop(ctx, ticker.C, onError)
}

func (s *Service) cleanupLoop(ctx context.Context, ticks <-chan time.Time, onError func()) {
	for {
		if ctx.Err() != nil {
			return
		}
		cleanCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.Cleanup(cleanCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			onError()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
