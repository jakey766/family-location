// Package webui contains the browser assets served by the Go binary.
package webui

import "embed"

// Files contains only build-time assets; request paths never access the disk.
//
//go:embed assets/*
var Files embed.FS
