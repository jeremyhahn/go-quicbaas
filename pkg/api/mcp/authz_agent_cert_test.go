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
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// agentCertCtx builds a context carrying a verified peer certificate with the
// given subject, the way the MCP HTTP listener does from r.TLS.VerifiedChains.
// The trust decision (chain verification against the client CA) happens in the
// TLS layer; these tests exercise the identity/authorization layer above it.
func agentCertCtx(cn, org, ou string) context.Context {
	subject := pkix.Name{CommonName: cn}
	if org != "" {
		subject.Organization = []string{org}
	}
	if ou != "" {
		subject.OrganizationalUnit = []string{ou}
	}
	cert := &x509.Certificate{Subject: subject}
	return service.WithVerifiedPeerCert(context.Background(), cert)
}

// TestAgentCertAuthz_AllowInScopeTool asserts a verified agent certificate
// (OU=agents) admits an in-scope data-plane tool with NO bearer token and NO
// validator round-trip, and that the WithContext path injects the agent's
// principal and the tenant from the certificate's O= field for RLS.
func TestAgentCertAuthz_AllowInScopeTool(t *testing.T) {
	v := &fakeValidatorAuthorizer{}
	ac := newValidatorChecker(v)
	ctx := agentCertCtx("agent-vm-host-1", "tenant-a", service.DefaultAgentCertOU)

	gotCtx, authErr := ac.CheckToolAuthorizationWithContext(ctx, "qrdb_get", "")
	require.Nil(t, authErr, "in-scope tool must be allowed for an agent cert")
	assert.Equal(t, 0, v.calls, "certificate identity must not invoke the token validator")

	subject, ok := auth.SubjectFromContext(gotCtx)
	require.True(t, ok, "agent principal must be injected")
	assert.Equal(t, "agent-vm-host-1", subject.ID)
	assert.Equal(t, "agent-vm-host-1", subject.Username)
	assert.Equal(t, "tenant-a", subject.TenantID)
	assert.Equal(t, []string{orchestration.AgentRuntimeRole}, subject.Roles)
	assert.True(t, subject.Authenticated)

	assert.Equal(t, "tenant-a", iam.TenantFromContext(gotCtx),
		"tenant from the cert O= field must be injected for row-level security")
}

// TestAgentCertAuthz_AllowsExactlyTheMCPScopeSet walks every scope in
// AgentMCPScopes through a representative tool to guard the granted floor.
func TestAgentCertAuthz_AllowsExactlyTheMCPScopeSet(t *testing.T) {
	tools := map[string]string{
		"qrdb_get":                        "kv:get",
		"qrdb_put":                        "kv:put",
		"qrdb_list":                       "kv:list",
		"quicbaas_orchestrator_inference": "orchestrator:inference",
	}
	for tool, perm := range tools {
		ac := newValidatorChecker(&fakeValidatorAuthorizer{})
		ctx := agentCertCtx("agent-x", "tenant-a", service.DefaultAgentCertOU)
		authErr := ac.CheckToolAuthorization(ctx, tool, "")
		assert.Nilf(t, authErr, "tool %s (%s) must be inside the agent scope floor", tool, perm)
	}
}

// TestAgentCertAuthz_DeniesOutOfScopeTools asserts the bounded scope set holds:
// an agent certificate must NOT reach the platform's control plane or any
// data-plane verb outside AgentMCPScopes.
func TestAgentCertAuthz_DeniesOutOfScopeTools(t *testing.T) {
	outOfScope := []string{
		"qrdb_delete",                  // kv:delete — write floor is put only
		"quicbaas_orchestrator_spawn",  // orchestrator control plane
		"quicbaas_orchestrator_status", // even reads outside the floor are denied
	}
	for _, tool := range outOfScope {
		v := &fakeValidatorAuthorizer{}
		ac := newValidatorChecker(v)
		ctx := agentCertCtx("agent-x", "tenant-a", service.DefaultAgentCertOU)

		authErr := ac.CheckToolAuthorization(ctx, tool, "")
		require.NotNilf(t, authErr, "tool %s must be denied for an agent cert", tool)
		assert.Equal(t, AuthErrorPermissionDenied, authErr.Type)
		assert.Equal(t, 0, v.calls)
	}
}

// TestAgentCertAuthz_UnmappedToolDenied asserts default-deny for a tool with no
// qrdb permission mapping, even with a valid agent certificate.
func TestAgentCertAuthz_UnmappedToolDenied(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})
	ctx := agentCertCtx("agent-x", "tenant-a", service.DefaultAgentCertOU)

	authErr := ac.CheckToolAuthorization(ctx, "totally_unknown_tool", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorPermissionDenied, authErr.Type)
}

// TestAgentCertAuthz_NonAgentCertFallsThroughToTokenRequirement asserts a
// verified client certificate WITHOUT the agents OU (e.g. an operator's PIV
// login-factor cert) is never granted agent scopes: the token-less request
// fails with missing-token exactly as if no certificate were present.
func TestAgentCertAuthz_NonAgentCertFallsThroughToTokenRequirement(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})
	ctx := agentCertCtx("operator-piv", "tenant-a", "operators")

	authErr := ac.CheckToolAuthorization(ctx, "qrdb_get", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorMissingToken, authErr.Type)
}

// TestAgentCertAuthz_OUMatchIsExact asserts the OU sentinel comparison is an
// exact per-value match — a superstring OU is not an agent identity.
func TestAgentCertAuthz_OUMatchIsExact(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})
	ctx := agentCertCtx("agent-x", "tenant-a", "agents-extra")

	authErr := ac.CheckToolAuthorization(ctx, "qrdb_get", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorMissingToken, authErr.Type)
}

// TestAgentCertAuthz_MissingTenantFailsClosed asserts an agent cert without an
// O= tenant is DENIED, not admitted tenant-less and not fallen through.
func TestAgentCertAuthz_MissingTenantFailsClosed(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})
	ctx := agentCertCtx("agent-x", "", service.DefaultAgentCertOU)

	authErr := ac.CheckToolAuthorization(ctx, "qrdb_get", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorPermissionDenied, authErr.Type)
	assert.Contains(t, authErr.Message, "tenant")
}

// TestAgentCertAuthz_MissingCommonNameFailsClosed asserts an agent cert with no
// CN (no stable subject to audit) is denied.
func TestAgentCertAuthz_MissingCommonNameFailsClosed(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})
	ctx := agentCertCtx("", "tenant-a", service.DefaultAgentCertOU)

	authErr := ac.CheckToolAuthorization(ctx, "qrdb_get", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorPermissionDenied, authErr.Type)
	assert.Contains(t, authErr.Message, "common name")
}

// TestAgentCertAuthz_BearerTokenTakesPrecedence asserts that when BOTH a token
// and an agent certificate are presented, the unified validator decides from
// the token — the certificate path never runs for token-bearing callers.
func TestAgentCertAuthz_BearerTokenTakesPrecedence(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		err: &mockScopeDeniedError{},
	}
	ac := newValidatorChecker(v)
	ctx := agentCertCtx("agent-x", "tenant-a", service.DefaultAgentCertOU)

	// qrdb_get is inside the agent scope floor, so an (incorrect) cert-first
	// implementation would ALLOW this call. The validator double denies, so a
	// denial here proves the token decided.
	authErr := ac.CheckToolAuthorization(ctx, "qrdb_get", "some-token")
	require.NotNil(t, authErr, "the token's denial must win over the cert's allow")
	assert.Equal(t, 1, v.calls, "validator must be consulted for the token")
	assert.Equal(t, "some-token", v.gotToken)
}

// TestAgentCertAuthz_PublicToolNeedsNoIdentity asserts public tools stay public
// regardless of certificate presence and inject no principal.
func TestAgentCertAuthz_PublicToolNeedsNoIdentity(t *testing.T) {
	v := &fakeValidatorAuthorizer{}
	ac := newValidatorChecker(v)
	ctx := agentCertCtx("agent-x", "tenant-a", service.DefaultAgentCertOU)

	gotCtx, authErr := ac.CheckToolAuthorizationWithContext(ctx, "quicbaas_health", "")
	require.Nil(t, authErr)
	assert.Equal(t, 0, v.calls)
	_, ok := auth.SubjectFromContext(gotCtx)
	assert.False(t, ok, "public tools must not synthesize a principal")
}

// TestAgentCertAuthz_NoCertNoToken asserts the unchanged baseline: with neither
// credential the request fails with missing-token.
func TestAgentCertAuthz_NoCertNoToken(t *testing.T) {
	ac := newValidatorChecker(&fakeValidatorAuthorizer{})

	authErr := ac.CheckToolAuthorization(context.Background(), "qrdb_get", "")
	require.NotNil(t, authErr)
	assert.Equal(t, AuthErrorMissingToken, authErr.Type)
}

// mockScopeDeniedError stands in for a validator denial in the precedence test;
// any non-typed error maps to invalid-token, which is still a denial.
type mockScopeDeniedError struct{}

func (e *mockScopeDeniedError) Error() string { return "denied by validator double" }
