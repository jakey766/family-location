package config

import (
	"strings"
	"testing"
)

func TestAMapConfigDefaultsAndEnvironment(t *testing.T) {
	cfg, err := load(func(string) (string, bool) { return "absent", false })
	if err != nil || cfg.AMapJSKey != "" || cfg.AMapSecurityJSCode != "" {
		t.Fatal("absent map configuration must stay disabled")
	}
	for _, values := range [][2]string{{"", ""}, {"key_123-ABC", "secret_456-DEF"}, {strings.Repeat("k", 128), strings.Repeat("s", 128)}} {
		cfg, err := load(func(key string) (string, bool) {
			switch key {
			case "FAMILY_AMAP_JS_KEY":
				return values[0], true
			case "FAMILY_AMAP_SECURITY_JSCODE":
				return values[1], true
			default:
				return "", false
			}
		})
		if err != nil || cfg.AMapJSKey != values[0] || cfg.AMapSecurityJSCode != values[1] {
			t.Fatal("valid map configuration was not preserved")
		}
	}
}

func TestAMapConfigRejectsPartialAndInvalidSecrets(t *testing.T) {
	for _, values := range [][2]string{
		{"test-key", ""}, {"", "test-secret"}, {"key with space", "test-secret"},
		{"test-key", "secret&jscode=other"}, {strings.Repeat("k", 129), "test-secret"},
		{"test-key", "秘密"}, {"test-key", "secret\n"},
	} {
		_, err := load(func(key string) (string, bool) {
			switch key {
			case "FAMILY_AMAP_JS_KEY":
				return values[0], true
			case "FAMILY_AMAP_SECURITY_JSCODE":
				return values[1], true
			default:
				return "", false
			}
		})
		if err == nil {
			t.Errorf("invalid map credentials accepted")
			continue
		}
		for _, value := range values {
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("error exposed credential")
			}
		}
	}
}

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
	t.Setenv("FAMILY_AMAP_JS_KEY", "web-test-key")
	t.Setenv("FAMILY_AMAP_SECURITY_JSCODE", "web-test-secret")
	cfg, err := Load()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.DBPath != "data/custom.db" || cfg.AMapJSKey != "web-test-key" || cfg.AMapSecurityJSCode != "web-test-secret" {
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
