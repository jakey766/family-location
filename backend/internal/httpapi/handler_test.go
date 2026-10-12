package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"family-location/backend/internal/storage"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func checkJSON(t *testing.T, h http.Handler, path string, code int, body map[string]string) {
	t.Helper()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
	if r.Code != code {
		t.Fatalf("%s status=%d body=%s", path, r.Code, r.Body.String())
	}
	if r.Header().Get("Content-Type") != "application/json" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers=%v", r.Header())
	}
	var got map[string]string
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, body) {
		t.Fatalf("body=%v want=%v", got, body)
	}
}

func TestHealthAndReadiness(t *testing.T) {
	db := testDB(t)
	h := New(db)
	checkJSON(t, h, "/healthz", 200, map[string]string{"status": "ok"})
	checkJSON(t, h, "/readyz", 200, map[string]string{"status": "ok", "database": "ok"})
	db.Close()
	checkJSON(t, h, "/readyz", 503, map[string]string{"status": "unavailable", "database": "unavailable"})
	checkJSON(t, h, "/healthz", 200, map[string]string{"status": "ok"})
}

func TestReadinessRequiresExpectedSchema(t *testing.T) {
	for _, statement := range []string{"DROP TABLE schema_migrations", "DELETE FROM schema_migrations", "UPDATE schema_migrations SET version=999 WHERE version=(SELECT MAX(version) FROM schema_migrations)"} {
		t.Run(statement, func(t *testing.T) {
			db := testDB(t)
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			checkJSON(t, New(db), "/readyz", 503, map[string]string{"status": "unavailable", "database": "unavailable"})
		})
	}
}

func TestRouting(t *testing.T) {
	h := New(testDB(t))
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"POST", "/healthz", 405}, {"POST", "/readyz", 405}, {"GET", "/", 404},
		{"GET", "/readyz/extra", 404}, {"HEAD", "/healthz", 200}, {"HEAD", "/readyz", 200},
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != tc.code {
			t.Fatalf("%s %s: %d", tc.method, tc.path, r.Code)
		}
		if tc.method == "HEAD" && r.Body.Len() != 0 {
			t.Fatal("HEAD response has body")
		}
	}
}

func TestReadinessTimeout(t *testing.T) {
	db := testDB(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	checkJSON(t, New(db), "/readyz", 503, map[string]string{"status": "unavailable", "database": "unavailable"})
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("readiness took %s", elapsed)
	}
}

func TestReadinessHonorsRequestCancellation(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRecorder()
	New(db).ServeHTTP(r, httptest.NewRequest("GET", "/readyz", nil).WithContext(ctx))
	if r.Code != 503 {
		t.Fatalf("canceled request status=%d", r.Code)
	}
}
