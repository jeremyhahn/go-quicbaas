//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"
)

// =============================================================================
// handleBeginLogin / handleFinishLogin guard branches.
//
// Both handlers must fail closed when the LoginCeremony facade is not wired and
// must reject malformed payloads before invoking the facade.
// =============================================================================

// TestHandleBeginLogin_NilLoginCeremony verifies begin-login fails when the
// service exposes no LoginCeremony facade (loginSvc is nil).
func TestHandleBeginLogin_NilLoginCeremony(t *testing.T) {
	// mockService.loginSvc is nil by default.
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	resp := h.handleBeginLogin(context.Background(), &Request{Opcode: OpAuthBeginLogin, Payload: []byte("{}")})
	if resp.Status == StatusOK {
		t.Fatal("expected error when LoginCeremony facade is not configured")
	}
}

// TestHandleBeginLogin_DecodeError verifies begin-login rejects a malformed
// payload before reaching the facade.
func TestHandleBeginLogin_DecodeError(t *testing.T) {
	h := newQUICLoginHandler(&quicLoginFakeClient{})

	resp := h.handleBeginLogin(context.Background(), &Request{Opcode: OpAuthBeginLogin, Payload: []byte("{not-json")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for malformed begin-login payload")
	}
}

// TestHandleFinishLogin_NilLoginCeremony verifies finish-login fails when the
// LoginCeremony facade is not wired.
func TestHandleFinishLogin_NilLoginCeremony(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	resp := h.handleFinishLogin(context.Background(), &Request{Opcode: OpAuthFinishLogin, Payload: []byte("{}")})
	if resp.Status == StatusOK {
		t.Fatal("expected error when LoginCeremony facade is not configured")
	}
}

// TestHandleFinishLogin_DecodeError verifies finish-login rejects a malformed
// payload before reaching the facade.
func TestHandleFinishLogin_DecodeError(t *testing.T) {
	h := newQUICLoginHandler(&quicLoginFakeClient{})

	resp := h.handleFinishLogin(context.Background(), &Request{Opcode: OpAuthFinishLogin, Payload: []byte("{not-json")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for malformed finish-login payload")
	}
}
