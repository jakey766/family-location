ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
export GOCACHE := $(ROOT)/.cache/go-build
export GOMODCACHE := $(ROOT)/.cache/go-mod
export GOPATH := $(ROOT)/.cache/go
export GOTMPDIR := $(ROOT)/.cache/tmp
export TMPDIR := $(ROOT)/.cache/tmp
export GOTELEMETRY := off
export GOTOOLCHAIN := local
export FAMILY_DB_PATH ?= $(ROOT)/data/family-location.db
GOARCH ?= amd64

.PHONY: prepare deps test test-race vet fmt build build-linux run smoke init-admin auth-smoke location-smoke location-demo web-deps web-test web-smoke web-browser-test web-browser-install
prepare:
	@mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(ROOT)/bin"

deps: prepare
	cd "$(ROOT)/backend" && go mod tidy

test: prepare
	cd "$(ROOT)/backend" && go test ./...

test-race: prepare
	cd "$(ROOT)/backend" && go test -race ./...

vet: prepare
	cd "$(ROOT)/backend" && go vet ./...

fmt: prepare
	cd "$(ROOT)/backend" && gofmt -w cmd internal

build: prepare
	cd "$(ROOT)/backend" && CGO_ENABLED=0 go build -trimpath -o "$(ROOT)/bin/family-location" ./cmd/server

build-linux: prepare
	cd "$(ROOT)/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -o "$(ROOT)/bin/family-location-linux-$(GOARCH)" ./cmd/server

run: build
	cd "$(ROOT)" && "$(ROOT)/bin/family-location"

smoke: build
	python3 "$(ROOT)/scripts/smoke.py"

init-admin: build
	cd "$(ROOT)" && "$(ROOT)/bin/family-location" init-admin

auth-smoke: build
	python3 "$(ROOT)/scripts/auth_smoke.py"

location-smoke: build
	PYTHONDONTWRITEBYTECODE=1 python3 "$(ROOT)/scripts/location_smoke.py"

location-demo: build
	PYTHONDONTWRITEBYTECODE=1 python3 "$(ROOT)/scripts/location_smoke.py" --demo

web-test:
	node --test scripts/web_state_test.mjs scripts/web_map_test.mjs

$(ROOT)/.cache/web-test/node_modules/.package-lock.json: scripts/web-test-deps/package.json scripts/web-test-deps/package-lock.json
	@mkdir -p "$(ROOT)/.cache/web-test"
	cp scripts/web-test-deps/package.json scripts/web-test-deps/package-lock.json "$(ROOT)/.cache/web-test/"
	npm ci --prefix "$(ROOT)/.cache/web-test" --cache "$(ROOT)/.cache/npm" --no-audit --no-fund

web-deps: prepare $(ROOT)/.cache/web-test/node_modules/.package-lock.json

# DOM + real HTTP integration; not a browser rendering or iPhone acceptance test.
web-smoke: build web-deps web-test
	PYTHONDONTWRITEBYTECODE=1 python3 "$(ROOT)/scripts/web_smoke.py"

web-browser-install: web-deps
	PLAYWRIGHT_BROWSERS_PATH="$(ROOT)/.cache/ms-playwright" node "$(ROOT)/.cache/web-test/node_modules/playwright/cli.js" install chromium webkit

web-browser-test: build web-deps
	PYTHONDONTWRITEBYTECODE=1 python3 "$(ROOT)/scripts/web_smoke.py" --browser
