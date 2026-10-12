package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("FAMILY_HTTP_ADDR", "")
	t.Setenv("FAMILY_DB_PATH", "")
	// Unset is distinct from an explicitly empty configuration.
	cfg, err := load(func(string) (string, bool) { return "", false })
	if err != nil || cfg.HTTPAddr != ":8080" || cfg.DBPath != "data/family-location.db" {
		t.Fatalf("defaults = %+v, %v", cfg, err)
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Setenv("FAMILY_HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("FAMILY_DB_PATH", "data/custom.db")
	cfg, err := Load()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.DBPath != "data/custom.db" {
		t.Fatalf("environment = %+v, %v", cfg, err)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"FAMILY_HTTP_ADDR", ""}, {"FAMILY_HTTP_ADDR", "8080"},
		{"FAMILY_HTTP_ADDR", ":0"}, {"FAMILY_HTTP_ADDR", ":65536"},
		{"FAMILY_HTTP_ADDR", ":http"}, {"FAMILY_DB_PATH", ""},
		{"FAMILY_DB_PATH", "   "}, {"FAMILY_DB_PATH", ":memory:"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			_, err := load(func(key string) (string, bool) { return tc.value, key == tc.key })
			if err == nil {
				t.Fatal("expected invalid configuration error")
			}
		})
	}
}
