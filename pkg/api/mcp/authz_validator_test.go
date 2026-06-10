//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// fakeValidatorAuthorizer is a test double for the unified platform-token
// authorization seam. It records the arguments of the last Authorize call so the
// MCP decision path (perm derivation, no-DPoP convention) can be asserted, and
// returns a fixed AuthorizedContext or error.
type fakeValidatorAuthorizer struct {
	authzCtx *platformtoken.AuthorizedContext
	err      error

	// Captured arguments from the last Authorize call.
	gotToken string
	gotPerm  string
	gotProof string
	gotHTM   string
	gotHTU   string
	calls    int
}

func (f *fakeValidatorAuthorizer) Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	f.calls++
	f.gotToken = token
	f.gotPerm = requiredPerm
	f.gotProof = dpopProof
	f.gotHTM = htm
	f.gotHTU = htu
	if f.err != nil {
		return nil, f.err
	}
	return f.authzCtx, nil
}

// newValidatorChecker builds a checker wired like the RequireAuthz=true validator
// posture: a single platform-token Validator seam, no OIDC/authService PDP.
func newValidatorChecker(v ValidatorAuthorizer) *AuthorizationChecker {
	ac := NewAuthorizationChecker(nil, nil)
	ac.SetValidatorAuthorizer(v)
	return ac
}

// TestValidatorAuthz_Allow asserts the validator path admits a permitted tool,
// derives the qrdb-canonical "resource:action", and calls the validator with NO
// DPoP proof (the intended, secure MCP convention).
func TestValidatorAuthz_Allow(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "admin",
			TenantID: "system",
			Roles:    []string{"admin"},
		},
	}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "good")
	assert.Nil(t, err, "permitted tool must be allowed")

	require.Equal(t, 1, v.calls)
	assert.Equal(t, "good", v.gotToken)
	assert.Equal(t, "kv:put", v.gotPerm, "derives qrdb-canonical resource:action")
	assert.Empty(t, v.gotProof, "MCP carries no DPoP proof")
	assert.Empty(t, v.gotHTM)
	assert.Empty(t, v.gotHTU)
}

// TestValidatorAuthz_PrincipalInjected asserts the WithContext path reconstructs
// and injects the principal (incl. Username) + tenant from the AuthorizedContext.
func TestValidatorAuthz_PrincipalInjected(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "admin",
			TenantID: "system",
			Roles:    []string{"admin", "reader"},
		},
	}
	ac := newValidatorChecker(v)

	ctx, err := ac.CheckToolAuthorizationWithContext(context.Background(), "quicbaas_put", "good")
	require.Nil(t, err)

	subject, ok := auth.SubjectFromContext(ctx)
	require.True(t, ok, "principal must be injected")
	assert.Equal(t, "u1", subject.ID)
	assert.Equal(t, "admin", subject.Username, "username preserved from token name claim")
	assert.Equal(t, "system", subject.TenantID)
	assert.Equal(t, []string{"admin", "reader"}, subject.Roles)
	assert.True(t, subject.Authenticated)

	assert.Equal(t, "system", iam.TenantFromContext(ctx), "tenant injected for RLS")
}

// TestValidatorAuthz_ScopeDenied asserts a missing-scope denial maps to
// permission-denied.
func TestValidatorAuthz_ScopeDenied(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ScopeDeniedError{Required: "kv:delete"}}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_delete", "good")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
}

// TestValidatorAuthz_TokenInactive asserts a revoked/disabled token maps to
// invalid-token.
func TestValidatorAuthz_TokenInactive(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.TokenInactiveError{}}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "revoked")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
}

// TestValidatorAuthz_ConfigError asserts a server-side misconfiguration fails
// closed as a misconfigured error (JSON-RPC InternalError), not a client error.
func TestValidatorAuthz_ConfigError(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ConfigError{}}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "good")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorMisconfigured, err.Type, "config error must fail closed as misconfigured")
}

// TestValidatorAuthz_UnknownError asserts an unrecognised validation error (bad
// signature / issuer / expiry / DPoP) fails closed as invalid-token.
func TestValidatorAuthz_UnknownError(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: errors.New("bad signature")}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "tampered")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
}

// TestValidatorAuthz_MissingToken asserts a non-public tool with no token is
// denied before the validator is called.
func TestValidatorAuthz_MissingToken(t *testing.T) {
	v := &fakeValidatorAuthorizer{}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorMissingToken, err.Type)
	assert.Equal(t, 0, v.calls, "validator not called for token-less request")
}

// TestValidatorAuthz_UnmappedDenied asserts a tool with no qrdb permission family
// is default-denied before the validator is called.
func TestValidatorAuthz_UnmappedDenied(t *testing.T) {
	v := &fakeValidatorAuthorizer{authzCtx: &platformtoken.AuthorizedContext{Subject: "u1"}}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_kms_generate", "good")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
	assert.Equal(t, 0, v.calls, "validator not called for unmapped tool")
}

// TestValidatorAuthz_PublicBypass asserts a public tool is admitted without a
// token and without calling the validator, and injects no principal.
func TestValidatorAuthz_PublicBypass(t *testing.T) {
	v := &fakeValidatorAuthorizer{}
	ac := newValidatorChecker(v)

	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_health", "")
	assert.Nil(t, err)
	assert.Equal(t, 0, v.calls)

	ctx, werr := ac.CheckToolAuthorizationWithContext(context.Background(), "quicbaas_health", "")
	require.Nil(t, werr)
	_, ok := auth.SubjectFromContext(ctx)
	assert.False(t, ok, "public tool injects no principal")
}

// TestValidatorAuthz_DenyInjectsNoPrincipal asserts a denied WithContext call
// returns the original context with no principal injected.
func TestValidatorAuthz_DenyInjectsNoPrincipal(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ScopeDeniedError{Required: "kv:delete"}}
	ac := newValidatorChecker(v)

	ctx, err := ac.CheckToolAuthorizationWithContext(context.Background(), "quicbaas_delete", "good")
	require.NotNil(t, err)
	_, ok := auth.SubjectFromContext(ctx)
	assert.False(t, ok, "denied request must not inject a principal")
}

// TestServer_SetValidatorAuthorizer verifies the MCP server delegates the
// validator installation to its authorization checker.
func TestServer_SetValidatorAuthorizer(t *testing.T) {
	srv := NewServer(nil, &Config{}, nil)
	v := &fakeValidatorAuthorizer{authzCtx: &platformtoken.AuthorizedContext{Subject: "u1"}}
	srv.SetValidatorAuthorizer(v)
	require.NotNil(t, srv.authChecker)
	require.NotNil(t, srv.authChecker.validatorAuthorizer)

	// The installed validator takes the decision path: a permitted tool is allowed.
	err := srv.authChecker.CheckToolAuthorization(context.Background(), "quicbaas_get", "good")
	assert.Nil(t, err)
	assert.Equal(t, "kv:get", v.gotPerm)
}
