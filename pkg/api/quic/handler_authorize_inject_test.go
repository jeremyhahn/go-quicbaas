//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// authorizeAndInject branch coverage.
//
// authorizeAndInject gates non-public opcodes: it validates the request token
// via the auth manager, performs an authorization decision via the authz
// service, and on success injects the resolved Subject into the context for
// downstream per-key authorization. Each branch is exercised directly.
// =============================================================================

// TestAuthorizeAndInject_PublicOperationSkips verifies a public opcode bypasses
// auth entirely and returns the original context untouched.
func TestAuthorizeAndInject_PublicOperationSkips(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{}))

	ctx := context.Background()
	got, resp := h.authorizeAndInject(ctx, &Request{Opcode: OpHealth})
	if resp != nil {
		t.Fatalf("public op must not be blocked, got %v", resp)
	}
	if _, ok := auth.SubjectFromContext(got); ok {
		t.Fatal("public op must not inject a subject")
	}
}

// TestAuthorizeAndInject_NoAuthConfiguredAllows verifies backward-compatible
// allow-all when neither auth manager nor authz service is configured.
func TestAuthorizeAndInject_NoAuthConfiguredAllows(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet})
	if resp != nil {
		t.Fatalf("expected allow-all with no auth services, got %v", resp)
	}
}

// TestAuthorizeAndInject_MissingToken verifies a non-public op without a token
// is rejected as unauthorized.
func TestAuthorizeAndInject_MissingToken(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", resp)
	}
}

// TestAuthorizeAndInject_TokenValidationError verifies a validation error from
// the auth manager yields unauthorized.
func TestAuthorizeAndInject_TokenValidationError(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return nil, errors.New("token expired")
			},
		}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", resp)
	}
}

// TestAuthorizeAndInject_InvalidValidationResult verifies a result that is not a
// valid *auth.TokenValidationResult yields unauthorized.
func TestAuthorizeAndInject_InvalidValidationResult(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return &auth.TokenValidationResult{Valid: false}, nil
			},
		}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized for invalid result, got %v", resp)
	}
}

// TestAuthorizeAndInject_ValidTokenInjectsSubject verifies a valid token with no
// authz service injects the resolved subject and allows the request.
func TestAuthorizeAndInject_ValidTokenInjectsSubject(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return &auth.TokenValidationResult{
					Valid:    true,
					UserID:   "user-1",
					Username: "alice",
					Roles:    []string{"admin"},
				}, nil
			},
		}))

	ctx, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow, got %v", resp)
	}
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok {
		t.Fatal("expected subject injected into context")
	}
	if subject.ID != "user-1" || subject.Username != "alice" || !subject.Authenticated {
		t.Fatalf("unexpected subject: %+v", subject)
	}
}

// TestAuthorizeAndInject_AuthzAllow verifies the authz service allow path with a
// valid token from the auth manager.
func TestAuthorizeAndInject_AuthzAllow(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return &auth.TokenValidationResult{Valid: true, UserID: "u", Username: "u"}, nil
			},
		}),
		WithHandlerAuthorizationService(&mockAuthzService{
			authorizeFunc: func(context.Context, *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				return &interfaces.AuthorizationDecision{Allowed: true}, nil
			},
		}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow, got %v", resp)
	}
}

// TestAuthorizeAndInject_AuthzDeny verifies the authz deny path returns
// permission-denied including the decision reason.
func TestAuthorizeAndInject_AuthzDeny(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return &auth.TokenValidationResult{Valid: true, UserID: "u"}, nil
			},
		}),
		WithHandlerAuthorizationService(&mockAuthzService{
			authorizeFunc: func(context.Context, *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				return &interfaces.AuthorizationDecision{Allowed: false, Reason: "role missing"}, nil
			},
		}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusPermissionDenied {
		t.Fatalf("expected StatusPermissionDenied, got %v", resp)
	}
}

// TestAuthorizeAndInject_AuthzError verifies an authz service error yields an
// internal error response.
func TestAuthorizeAndInject_AuthzError(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthManager(&mockAuthManager{
			validateTokenFunc: func(context.Context, string) (interface{}, error) {
				return &auth.TokenValidationResult{Valid: true, UserID: "u"}, nil
			},
		}),
		WithHandlerAuthorizationService(&mockAuthzService{
			authorizeFunc: func(context.Context, *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				return nil, errors.New("authz backend down")
			},
		}))

	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp == nil || resp.Status != StatusInternalError {
		t.Fatalf("expected StatusInternalError, got %v", resp)
	}
}

// TestAuthorizeAndInject_AuthzOnlyDefaultSubject verifies that with an authz
// service but no auth manager, a default authenticated subject is used and the
// request is allowed.
func TestAuthorizeAndInject_AuthzOnlyDefaultSubject(t *testing.T) {
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerAuthorizationService(&mockAuthzService{
			authorizeFunc: func(_ context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				if !req.Subject.Authenticated || req.Subject.ID != "unknown" {
					return &interfaces.AuthorizationDecision{Allowed: false, Reason: "unexpected subject"}, nil
				}
				return &interfaces.AuthorizationDecision{Allowed: true}, nil
			},
		}))

	ctx, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow with default subject, got %v", resp)
	}
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || subject.ID != "unknown" {
		t.Fatalf("expected default 'unknown' subject injected, got %+v (ok=%v)", subject, ok)
	}
}
