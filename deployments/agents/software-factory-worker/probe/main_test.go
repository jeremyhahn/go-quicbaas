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

package main

import (
	"bytes"
	"strings"
	"testing"
)

// The probes themselves need an engine and are exercised by
// `make test-worker-image`; these tests prove the command refuses to run
// rather than certifying an image against versions nobody named.

func TestRun_RefusesUnknownFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-no-such-flag"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRun_RefusesAnIncompleteDescription(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-image", "w:1", "-base", "0.11.0", "-claude-code", "2.1.280"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "@openai/codex") {
		t.Fatalf("exit = %d stderr = %q", code, stderr.String())
	}
}

// TestNewProbeLogger_IsAConfigurationTheLoggerAccepts guards the one setting
// that reached `make test-worker-image` before any probe ran: a log format the
// logger refuses fails the target after a full image build.
func TestNewProbeLogger_IsAConfigurationTheLoggerAccepts(t *testing.T) {
	if _, err := newProbeLogger(); err != nil {
		t.Fatalf("newProbeLogger: %v", err)
	}
}
