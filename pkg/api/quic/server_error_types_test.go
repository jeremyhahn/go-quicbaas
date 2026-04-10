//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

import (
	"testing"
)

// TestServerAlreadyRunningError tests the ServerAlreadyRunningError type.
func TestServerAlreadyRunningError(t *testing.T) {
	err := &ServerAlreadyRunningError{}

	msg := err.Error()
	if msg == "" {
		t.Fatal("Error() should return a non-empty string")
	}
	if msg != "server is already running" {
		t.Errorf("expected 'server is already running', got '%s'", msg)
	}
}

// TestServerNotRunningError tests the ServerNotRunningError type.
func TestServerNotRunningError(t *testing.T) {
	err := &ServerNotRunningError{}

	msg := err.Error()
	if msg == "" {
		t.Fatal("Error() should return a non-empty string")
	}
	if msg != "server is not running" {
		t.Errorf("expected 'server is not running', got '%s'", msg)
	}
}

// TestServerAlreadyRunningError_AsError verifies the type satisfies the error interface.
func TestServerAlreadyRunningError_AsError(t *testing.T) {
	var err error = &ServerAlreadyRunningError{}
	if err == nil {
		t.Fatal("expected non-nil error")
	}
}

// TestServerNotRunningError_AsError verifies the type satisfies the error interface.
func TestServerNotRunningError_AsError(t *testing.T) {
	var err error = &ServerNotRunningError{}
	if err == nil {
		t.Fatal("expected non-nil error")
	}
}
