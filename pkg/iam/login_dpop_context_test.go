//go:build iam

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

package iam

import (
	"context"
	"testing"
)

// TestLoginDPoPContext_RoundTrip proves a presented login DPoP proof threads
// through the context the SessionIssuer reads, so a session bearer can be
// sender-constrained without changing the qrdb login-ceremony signature.
func TestLoginDPoPContext_RoundTrip(t *testing.T) {
	d := &LoginDPoP{
		Proof:  "eyJ0eXAiOiJkcG9wK2p3dCJ9.payload.sig",
		Method: "POST",
		URI:    "https://broker.example/api/v1/auth/login/finish",
	}
	ctx := ContextWithLoginDPoP(context.Background(), d)

	got := LoginDPoPFromContext(ctx)
	if got == nil {
		t.Fatal("expected login DPoP on context, got nil")
	}
	if got.Proof != d.Proof || got.Method != d.Method || got.URI != d.URI {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, d)
	}
}

// TestLoginDPoPContext_AbsentAndNoOp proves the accessor returns nil on a bare
// context and that a nil or empty-proof value is a no-op (the context is
// unchanged), so transport handlers need not branch on header presence.
func TestLoginDPoPContext_AbsentAndNoOp(t *testing.T) {
	if got := LoginDPoPFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil on bare context, got %+v", got)
	}

	base := context.Background()
	if ContextWithLoginDPoP(base, nil) != base {
		t.Error("nil LoginDPoP must return the context unchanged")
	}
	if ContextWithLoginDPoP(base, &LoginDPoP{Method: "POST"}) != base {
		t.Error("empty-proof LoginDPoP must return the context unchanged")
	}
	if got := LoginDPoPFromContext(ContextWithLoginDPoP(base, &LoginDPoP{})); got != nil {
		t.Fatalf("empty-proof value must not be retrievable, got %+v", got)
	}
}
