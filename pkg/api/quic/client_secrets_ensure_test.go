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
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestEnsureSecretQUIC_NilRequest verifies the nil-input guard at the
// QUIC client boundary.
func TestEnsureSecretQUIC_NilRequest(t *testing.T) {
	c := &Client{}
	_, err := c.EnsureSecret(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

// TestEnsureSecretQUIC_EmptySecretID verifies the empty-path guard.
func TestEnsureSecretQUIC_EmptySecretID(t *testing.T) {
	c := &Client{}
	_, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{})
	if err == nil {
		t.Fatal("expected error for empty secret id")
	}
}

// TestEnsureSecretQUIC_UnsupportedSentinel locks in the current
// no-silent-fallback behavior: until the QUIC wire wiring lands (see
// TODO in client_secrets_ensure.go) the client returns a typed sentinel
// error that callers can detect and skip cleanly. A green-path test
// replaces this one once the sub-dispatch opcode is added.
func TestEnsureSecretQUIC_UnsupportedSentinel(t *testing.T) {
	c := &Client{}
	_, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{
		SecretID: "p",
		Length:   16,
		Encoding: "base64",
	})
	if !errors.Is(err, ErrEnsureSecretUnsupportedQUIC) {
		t.Fatalf("expected ErrEnsureSecretUnsupportedQUIC, got: %v", err)
	}
}
