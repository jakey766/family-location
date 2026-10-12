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

.PHONY: prepare deps test test-race vet fmt build build-linux run smoke init-admin auth-smoke
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
