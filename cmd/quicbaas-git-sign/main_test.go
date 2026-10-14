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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/gitsign"
)

var errClosed = errors.New("closed")

// closedWriter is a stderr that cannot be written.
type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, errClosed }

func noEnv(string) string { return "" }

func TestRunReportsMalformedInvocationBeforeConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run(context.Background(), []string{"-Y", "verify", "-n", "git", "-f", "k"}, noEnv, strings.NewReader(""), &stdout, &stderr)
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), gitsign.ErrInvalidInvocation.Error()) {
		t.Fatalf("stderr does not report the invocation: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout was written: %q", stdout.String())
	}
}

func TestRunReportsMissingSessionConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run(context.Background(), []string{"-Y", "sign", "-n", "git", "-f", "k"}, noEnv, strings.NewReader(""), &stdout, &stderr)
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), gitsign.EnvMCPURL) {
		t.Fatalf("stderr does not name the missing variable: %q", stderr.String())
	}
}

func TestRunExitsTwoWhenStderrFails(t *testing.T) {
	var stdout bytes.Buffer
	if status := run(context.Background(), nil, noEnv, strings.NewReader(""), &stdout, closedWriter{}); status != 2 {
		t.Fatalf("status = %d, want 2", status)
	}
}
