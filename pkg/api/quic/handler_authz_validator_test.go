//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Unified platform-token authorization path (authorizeViaValidator).
//
// These tests replace the migrated online-role-PDP QUIC behaviour: the handler
// no longer runs a token-validate + qrdb-decision two-step. When a
// ValidatorAuthorizer is installed it performs ONE Authorize call per opcode
// (offline sig/iss/aud/exp, DPoP sender-constraint for cnf.jkt tokens, offline
// scp decision, per-tenant liveness) and reconstructs the principal from the
// returned AuthorizedContext.
// =============================================================================

// fakeValidatorAuthorizer is a test double for the unified authorization seam.
// It records the arguments of the last Authorize call so the test can assert
// the QUIC PEP joins the opcode permission and passes empty DPoP parameters.
type fakeValidatorAuthorizer struct {
	authzCtx *platformtoken.AuthorizedContext
	err      error

	gotToken        string
	gotRequiredPerm string
	gotDPoP         string
	gotHTM          string
	gotHTU          string
}

func (f *fakeValidatorAuthorizer) Authorize(_ context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	f.gotToken = token
	f.gotRequiredPerm = requiredPerm
	f.gotDPoP = dpopProof
	f.gotHTM = htm
	f.gotHTU = htu
	if f.err != nil {
		return nil, f.err
	}
	return f.authzCtx, nil
}

// TestAuthorizeViaValidator_AllowInjectsPrincipal verifies the unified path
// authorizes via a single Authorize call, joins the opcode permission to the
// qrdb-canonical "resource:action", passes EMPTY DPoP parameters (QUIC carries
// no DPoP), and injects the reconstructed principal (Username included).
func TestAuthorizeViaValidator_AllowInjectsPrincipal(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "tenant-a",
			Roles:    []string{"admin"},
		},
	}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	ctx, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow, got %v", resp)
	}

	// OpGet maps to kv:get.
	if v.gotRequiredPerm != "kv:get" {
		t.Fatalf("expected requiredPerm kv:get, got %q", v.gotRequiredPerm)
	}
	if v.gotToken != "tok" {
		t.Fatalf("expected token forwarded, got %q", v.gotToken)
	}
	if v.gotDPoP != "" || v.gotHTM != "" || v.gotHTU != "" {
		t.Fatalf("QUIC must pass empty DPoP params, got dpop=%q htm=%q htu=%q", v.gotDPoP, v.gotHTM, v.gotHTU)
	}

	subject, ok := auth.SubjectFromContext(ctx)
	if !ok {
		t.Fatal("expected subject injected into context")
	}
	if subject.ID != "user-1" || subject.Username != "alice" || subject.TenantID != "tenant-a" || !subject.Authenticated {
		t.Fatalf("unexpected subject: %+v", subject)
	}
	if len(subject.Roles) != 1 || subject.Roles[0] != "admin" {
		t.Fatalf("unexpected roles: %+v", subject.Roles)
	}
}

// TestAuthorizeViaValidator_UnmappedOpcodeDenied verifies an opcode with no
// qrdb permission mapping is default-denied without ever calling the validator.
func TestAuthorizeViaValidator_UnmappedOpcodeDenied(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{Subject: "u"},
	}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	// 0xFE is not a registered opcode and has no permission mapping.
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: 0xFE, Token: "tok"})
	if resp == nil || resp.Status != StatusPermissionDenied {
		t.Fatalf("expected StatusPermissionDenied for unmapped opcode, got %v", resp)
	}
	if v.gotToken != "" {
		t.Fatal("validator must not be called for an unmapped opcode")
	}
}

// TestAuthorizeViaValidator_ScopeDeniedIsPermissionDenied verifies a
// ScopeDeniedError maps to StatusPermissionDenied (authenticated but the granted
// scopes do not admit the required permission).
func TestAuthorizeViaValidator_ScopeDeniedIsPermissionDenied(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ScopeDeniedError{Required: "kv:get"}}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusPermissionDenied {
		t.Fatalf("expected StatusPermissionDenied, got %v", resp)
	}
}

// TestAuthorizeViaValidator_TokenInactiveIsUnauthorized verifies a
// TokenInactiveError (revoked / subject disabled) maps to StatusUnauthorized.
func TestAuthorizeViaValidator_TokenInactiveIsUnauthorized(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.TokenInactiveError{Subject: "u"}}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", resp)
	}
}

// TestAuthorizeViaValidator_ConfigErrorIsInternalError verifies a server-side
// misconfiguration (ConfigError) fails closed as StatusInternalError — the
// request is DENIED, never admitted.
func TestAuthorizeViaValidator_ConfigErrorIsInternalError(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ConfigError{Detail: "replay cache required"}}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusInternalError {
		t.Fatalf("expected StatusInternalError, got %v", resp)
	}
}

// TestAuthorizeViaValidator_GenericErrorIsUnauthorized verifies any other
// validation failure (e.g. invalid signature / expiry / missing DPoP proof for
// a cnf.jkt token) fails closed as StatusUnauthorized.
func TestAuthorizeViaValidator_GenericErrorIsUnauthorized(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: errors.New("invalid signature")}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", resp)
	}
}

// TestAuthorizeViaValidator_MissingTokenUnauthorized verifies a non-public op
// without a token is rejected before the validator is consulted, even on the
// unified path.
func TestAuthorizeViaValidator_MissingTokenUnauthorized(t *testing.T) {
	v := &fakeValidatorAuthorizer{authzCtx: &platformtoken.AuthorizedContext{Subject: "u"}}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", resp)
	}
	if v.gotToken != "" {
		t.Fatal("validator must not be called when the token is missing")
	}
}

// TestAuthorizeViaValidator_PublicOperationSkips verifies a public opcode
// bypasses the unified gate entirely (no token required).
func TestAuthorizeViaValidator_PublicOperationSkips(t *testing.T) {
	v := &fakeValidatorAuthorizer{authzCtx: &platformtoken.AuthorizedContext{Subject: "u"}}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpHealth})
	if resp != nil {
		t.Fatalf("public op must not be blocked, got %v", resp)
	}
	if v.gotToken != "" {
		t.Fatal("validator must not be called for a public opcode")
	}
}

// TestValidatorErrorStatus_Mapping exercises the error-to-status mapping table
// directly, including the fail-closed default for an unrecognised error.
func TestValidatorErrorStatus_Mapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want byte
	}{
		{"scope denied", &platformtoken.ScopeDeniedError{Required: "kv:get"}, StatusPermissionDenied},
		{"token inactive", &platformtoken.TokenInactiveError{Subject: "u"}, StatusUnauthorized},
		{"config error", &platformtoken.ConfigError{Detail: "x"}, StatusInternalError},
		{"generic", errors.New("boom"), StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validatorErrorStatus(tt.err); got != tt.want {
				t.Fatalf("validatorErrorStatus(%v) = 0x%02X, want 0x%02X", tt.err, got, tt.want)
			}
		})
	}
}
