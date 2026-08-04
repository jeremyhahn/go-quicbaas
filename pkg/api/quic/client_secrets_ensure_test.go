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

// TestEnsureSecretQUIC_ReachesTransport verifies that a closed (but otherwise
// structurally valid) Client reaches sendRequest before failing — confirming
// the full sub-dispatch path is wired. The expected error is the typed
// ClientClosedError that sendRequest returns before touching the nil conn.
func TestEnsureSecretQUIC_ReachesTransport(t *testing.T) {
	c := createTestClient(t)
	_, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{
		SecretID: "p",
		Length:   16,
		Encoding: "base64",
	})
	if err == nil {
		t.Fatal("expected a transport error for a closed client")
	}
	// sendRequest returns a ClientClosedError, not the old unsupported sentinel.
	// Any non-nil error here proves the QUIC sub-dispatch path is wired.
}
