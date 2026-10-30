package location

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"family-location/backend/internal/auth"
	"family-location/backend/internal/storage"
)

type fixture struct {
	s      *Service
	db     *sql.DB
	device auth.Device
	admin  auth.Admin
	now    time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "location.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`INSERT INTO families(id,name,created_at,public_id) VALUES(1,'test',0,'test-family'),(2,'other',0,'other-family'); INSERT INTO users VALUES(1,1,'test','unused',0),(2,2,'other','unused',0); INSERT INTO devices(id,family_id,name,token_hash,created_at) VALUES('device',1,'模拟设备',x'01',0),('other',2,'other',x'02',0); INSERT INTO family_admins SELECT family_id,id FROM users; INSERT INTO family_devices SELECT family_id,id FROM devices; INSERT INTO device_configs(device_id) SELECT id FROM devices;`)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, device: auth.Device{ID: "device", FamilyID: 1}, admin: auth.Admin{ID: 1, FamilyID: 1}, now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	f.s = New(db)
	f.s.now = func() time.Time { return f.now }
	return f
}
func (f *fixture) start(t *testing.T) string {
	t.Helper()
	v, e := f.s.Start(context.Background(), f.device)
	if e != nil {
		t.Fatal(e)
	}
	return v.SharingSessionID
}
func point(id string, seq int64, at time.Time) Input {
	b := 73
	return Input{SharingSessionID: id, Sequence: seq, Latitude: 31.234567, Longitude: 121.456789, CoordinateSystem: "GCJ-02", AccuracyM: 12.5, BatteryPercent: &b, CapturedAt: at}
}
func mustUpload(t *testing.T, f *fixture, p Input, want bool) {
	t.Helper()
	v, e := f.s.Upload(context.Background(), f.device, p)
	if e != nil || v.Accepted != want {
		t.Fatalf("accepted=%v err=%v", v.Accepted, e)
	}
}
func list(t *testing.T, f *fixture) []Location {
	t.Helper()
	v, e := f.s.List(context.Background(), f.admin)
	if e != nil {
		t.Fatal(e)
	}
	return v.Locations
}
func TestLifecycleOrderingAndSessionIsolation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if got := list(t, f); len(got) != 0 {
		t.Fatal("paired device sharing")
	}
	if _, e := f.s.Upload(ctx, f.device, point("absent", 1, f.now)); !errors.Is(e, ErrSession) {
		t.Fatal(e)
	}
	id := f.start(t)
	p := point(id, 1, f.now)
	mustUpload(t, f, p, true)
	initial := list(t, f)[0]
	f.now = f.now.Add(time.Minute)
	mustUpload(t, f, p, false)
	if got := list(t, f)[0]; got.ReceivedAt != initial.ReceivedAt || got.LeaseExpiresAt != initial.LeaseExpiresAt {
		t.Fatal("retry renewed lease")
	}
	p.Sequence = 3
	p.CapturedAt = f.now
	p.BatteryPercent = nil
	mustUpload(t, f, p, true)
	p.Sequence = 2
	if _, e := f.s.Upload(ctx, f.device, p); !errors.Is(e, ErrSequence) {
		t.Fatal(e)
	}
	p.Sequence = 4
	p.CapturedAt = p.CapturedAt.Add(-time.Second)
	if _, e := f.s.Upload(ctx, f.device, p); !errors.Is(e, ErrSequence) {
		t.Fatal("regressing capture accepted", e)
	}
	var count int
	if e := f.db.QueryRow("SELECT COUNT(*) FROM latest_locations").Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	got := list(t, f)[0]
	if got.Sequence != 3 || got.BatteryPercent != nil || got.Latitude != p.Latitude || got.AccuracyM != p.AccuracyM {
		t.Fatal("incorrect latest fields")
	}
	other, e := f.s.List(ctx, auth.Admin{ID: 2, FamilyID: 2})
	if e != nil || len(other.Locations) != 0 {
		t.Fatal("family leaked", e)
	}
	if _, e := f.s.List(ctx, auth.Admin{ID: 1, FamilyID: 2}); !errors.Is(e, auth.ErrUnauthorized) {
		t.Fatal(e)
	}
	next := f.start(t)
	if next == id || len(list(t, f)) != 0 {
		t.Fatal("restart didn't clear")
	}
	if e := f.s.Stop(ctx, f.device, id); !errors.Is(e, ErrSession) {
		t.Fatal("old stop closed new session", e)
	}
	if _, e := f.s.Upload(ctx, f.device, point(id, 5, f.now)); !errors.Is(e, ErrSession) {
		t.Fatal(e)
	}
	mustUpload(t, f, point(next, 1, f.now), true)
	for i := 0; i < 2; i++ {
		if e := f.s.Stop(ctx, f.device, next); e != nil {
			t.Fatal(e)
		}
	}
	if len(list(t, f)) != 0 {
		t.Fatal("stopped location visible")
	}
	if _, e := f.s.Upload(ctx, f.device, point(next, 2, f.now)); !errors.Is(e, ErrSession) {
		t.Fatal(e)
	}
}
func TestStationarySamplingFreshnessMargin(t *testing.T) {
	f := setup(t)
	id := f.start(t)
	captured := f.now
	// A delayed receipt keeps the lease alive past the capture freshness boundary.
	f.now = captured.Add(30 * time.Second)
	mustUpload(t, f, point(id, 1, captured), true)
	for _, age := range []time.Duration{5 * time.Minute, 330 * time.Second, 7*time.Minute - time.Millisecond, 7 * time.Minute} {
		f.now = captured.Add(age)
		if got, want := list(t, f)[0].Stale, age >= 7*time.Minute; got != want {
			t.Errorf("capture age %s: stale = %v, want %v", age, got, want)
		}
	}
}

func TestLeaseFreshnessCleanupAndReplay(t *testing.T) {
	f := setup(t)
	id := f.start(t)
	p := point(id, 1, f.now.Add(-7*time.Minute))
	mustUpload(t, f, p, true)
	if !list(t, f)[0].Stale {
		t.Fatal("delayed upload appears fresh")
	}
	f.now = f.now.Add(7*time.Minute - time.Millisecond)
	if len(list(t, f)) != 1 {
		t.Fatal("early expiry")
	}
	f.now = f.now.Add(time.Millisecond)
	if got := list(t, f); len(got) != 1 || !got[0].Stale {
		t.Fatal("last position must remain stale after reporting timeout")
	}
	if e := f.s.Cleanup(context.Background()); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := f.db.QueryRow("SELECT COUNT(*) FROM latest_locations").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	mustUpload(t, f, p, false)
	if got := list(t, f); len(got) != 1 || got[0].ReceivedAt != p.CapturedAt.Add(7*time.Minute) {
		t.Fatal("retry changed retained location")
	}
	p.Sequence = 2
	p.CapturedAt = f.now
	mustUpload(t, f, p, true)
	if list(t, f)[0].Stale {
		t.Fatal("new capture stale")
	}
	f.now = f.now.Add(7 * time.Minute)
	if !list(t, f)[0].Stale {
		t.Fatal("freshness boundary")
	}
}
func TestInputValidation(t *testing.T) {
	f := setup(t)
	id := f.start(t)
	cases := map[string]func(*Input){"latitude": func(p *Input) { p.Latitude = 91 }, "longitude": func(p *Input) { p.Longitude = -181 }, "nan": func(p *Input) { p.Latitude = math.NaN() }, "inf": func(p *Input) { p.AccuracyM = math.Inf(1) }, "accuracy": func(p *Input) { p.AccuracyM = -1 }, "battery": func(p *Input) { v := 101; p.BatteryPercent = &v }, "zero_seq": func(p *Input) { p.Sequence = 0 }, "overflow_seq": func(p *Input) { p.Sequence = 9007199254740992 }, "coordinate": func(p *Input) { p.CoordinateSystem = "WGS-84" }, "future": func(p *Input) { p.CapturedAt = f.now.Add(2*time.Minute + time.Millisecond) }, "old": func(p *Input) { p.CapturedAt = f.now.Add(-24*time.Hour - time.Millisecond) }, "missing_time": func(p *Input) { p.CapturedAt = time.Time{} }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := point(id, 1, f.now)
			change(&p)
			if _, e := f.s.Upload(context.Background(), f.device, p); !errors.Is(e, auth.ErrInvalidInput) {
				t.Fatal(e)
			}
		})
	}
	p := point(id, 1, f.now.Add(-24*time.Hour))
	p.Latitude = -90
	p.Longitude = 180
	p.AccuracyM = 0
	b := 0
	p.BatteryPercent = &b
	mustUpload(t, f, p, true)
	p.Sequence = 2
	p.CapturedAt = f.now.Add(2 * time.Minute)
	b = 100
	mustUpload(t, f, p, true)
}
func TestConcurrentStopAndRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "revoke"}[revoke], func(t *testing.T) {
			f := setup(t)
			id := f.start(t)
			ctx := context.Background()
			gate := make(chan struct{})
			errs := make(chan error, 20)
			var wg sync.WaitGroup
			for i := 1; i <= 20; i++ {
				wg.Add(1)
				go func(seq int64) {
					defer wg.Done()
					<-gate
					_, e := f.s.Upload(ctx, f.device, point(id, seq, f.now))
					errs <- e
				}(int64(i))
			}
			close(gate)
			var e error
			if revoke {
				e = auth.New(f.db).RevokeDevice(ctx, f.admin, f.device.ID)
			} else {
				e = f.s.Stop(ctx, f.device, id)
			}
			if e != nil {
				t.Fatal(e)
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil && !errors.Is(e, ErrSession) && !errors.Is(e, ErrSequence) && !errors.Is(e, auth.ErrUnauthorized) {
					t.Fatal(e)
				}
			}
			if len(list(t, f)) != 0 {
				t.Fatal("concurrent write resurrected location")
			}
			var n int
			if e = f.db.QueryRow("SELECT COUNT(*) FROM latest_locations").Scan(&n); e != nil || n != 0 {
				t.Fatal(n, e)
			}
			if revoke {
				if _, e = f.s.Start(ctx, f.device); !errors.Is(e, auth.ErrUnauthorized) {
					t.Fatal("revoked device restarted", e)
				}
			}
		})
	}
}
