// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package quicbaas

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestLoggerReexport_WithHandlerSeam proves the handler-injection seam works
// through the quicbaas SDK alias surface: records land in the injected sink
// and the underlying slog logger is reachable via Slog().
func TestLoggerReexport_WithHandlerSeam(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	log, err := NewLoggerWithHandler(h, LogConfig{Level: "debug", Component: "sdk-alias"})
	if err != nil {
		t.Fatalf("NewLoggerWithHandler: %v", err)
	}
	log.Info("through-the-frontage")
	if !strings.Contains(buf.String(), "through-the-frontage") {
		t.Errorf("record did not reach injected handler: %s", buf.String())
	}
	if log.Slog() == nil {
		t.Error("Slog() accessor returned nil")
	}
}

// TestLoggerReexport_NilHandlerFails pins the error path through the alias.
func TestLoggerReexport_NilHandlerFails(t *testing.T) {
	if _, err := NewLoggerWithHandler(nil, LogConfig{Level: "info"}); err == nil {
		t.Fatal("expected error for nil handler, got nil")
	}
}
