package location

import (
	"context"
	"testing"
	"time"
)

func TestCleanupLoopRunsAtStartupOnTickAndCancels(t *testing.T) {
	f := setup(t)
	id := f.start(t)
	mustUpload(t, f, point(id, 1, f.now), true)
	// Use immutable now while the worker runs. Simulate a revoked residual row.
	expire := func() {
		t.Helper()
		if _, e := f.db.Exec("UPDATE devices SET revoked_at=?", f.now.UnixMilli()); e != nil {
			t.Fatal(e)
		}
	}
	waitEmpty := func() {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			var n int
			if e := f.db.QueryRow("SELECT COUNT(*) FROM latest_locations").Scan(&n); e != nil {
				t.Fatal(e)
			}
			if n == 0 {
				return
			}
			select {
			case <-deadline:
				t.Fatal("cleanup did not run")
			case <-time.After(time.Millisecond):
			}
		}
	}
	expire()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	failures := make(chan struct{}, 1)
	go func() { defer close(done); f.s.cleanupLoop(ctx, ticks, func() { failures <- struct{}{} }) }()
	waitEmpty()
	if _, e := f.db.Exec("UPDATE devices SET revoked_at=NULL"); e != nil {
		t.Fatal(e)
	}
	mustUpload(t, f, point(id, 2, f.now), true)
	expire()
	ticks <- f.now
	waitEmpty()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker failed to stop")
	}
	select {
	case <-failures:
		t.Fatal("unexpected cleanup error")
	default:
	}
}
func TestCleanupFailureReportsWithoutLeakingData(t *testing.T) {
	f := setup(t)
	f.db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	f.s.cleanupLoop(ctx, make(chan time.Time), func() { calls++; cancel() })
	if calls != 1 {
		t.Fatal(calls)
	}
}
