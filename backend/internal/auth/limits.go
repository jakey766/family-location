package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
)

// Limits are fixed windows anchored at the first request. Every attempted request
// counts, including successful logins; counters and cleanup survive restarts.
type limitRule struct {
	key string
	max int
}

func (s *Service) limit(ctx context.Context, rules ...limitRule) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin limits: %w", err)
	}
	defer tx.Rollback()
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, "DELETE FROM auth_limits WHERE window_start<=?", now-600); err != nil {
		return fmt.Errorf("clean limits: %w", err)
	}
	denied := false
	for _, r := range rules {
		key := base64.RawURLEncoding.EncodeToString(digest(r.key))
		var count int
		err := tx.QueryRowContext(ctx, "SELECT count FROM auth_limits WHERE key=?", key).Scan(&count)
		if errors.Is(err, sql.ErrNoRows) {
			var size int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_limits").Scan(&size); err != nil {
				return fmt.Errorf("count limits: %w", err)
			}
			if size >= 10000 {
				denied = true
				continue
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO auth_limits(key,window_start,count) VALUES (?,?,1)", key, now); err != nil {
				return fmt.Errorf("create limit: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("read limit: %w", err)
		} else {
			if count >= r.max {
				denied = true
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE auth_limits SET count=count+1 WHERE key=?", key); err != nil {
				return fmt.Errorf("increment limit: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit limits: %w", err)
	}
	if denied {
		return ErrRateLimited
	}
	return nil
}
