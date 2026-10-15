package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr           string
	DBPath             string
	AMapJSKey          string
	AMapSecurityJSCode string
}

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{HTTPAddr: ":8080", DBPath: "data/family-location.db"}
	if value, ok := lookup("FAMILY_HTTP_ADDR"); ok {
		cfg.HTTPAddr = value
	}
	if value, ok := lookup("FAMILY_DB_PATH"); ok {
		cfg.DBPath = value
	}
	if value, ok := lookup("FAMILY_AMAP_JS_KEY"); ok {
		cfg.AMapJSKey = value
	}
	if value, ok := lookup("FAMILY_AMAP_SECURITY_JSCODE"); ok {
		cfg.AMapSecurityJSCode = value
	}
	if (cfg.AMapJSKey == "") != (cfg.AMapSecurityJSCode == "") {
		return Config{}, fmt.Errorf("FAMILY_AMAP_JS_KEY and FAMILY_AMAP_SECURITY_JSCODE must be configured together")
	}
	for _, credential := range []string{cfg.AMapJSKey, cfg.AMapSecurityJSCode} {
		if len(credential) > 128 {
			return Config{}, fmt.Errorf("invalid AMap credential format")
		}
		for _, c := range credential {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				return Config{}, fmt.Errorf("invalid AMap credential format")
			}
		}
	}
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, fmt.Errorf("FAMILY_HTTP_ADDR must be host:port: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("FAMILY_HTTP_ADDR port must be 1..65535")
	}
	if strings.TrimSpace(cfg.DBPath) == "" || cfg.DBPath == ":memory:" {
		return Config{}, fmt.Errorf("FAMILY_DB_PATH must be a persistent database file path")
	}
	return cfg, nil
}
