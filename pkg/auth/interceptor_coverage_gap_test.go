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

package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamAuthInterceptor_Disabled verifies the backward-compatible
// StreamAuthInterceptor wrapper short-circuits to the handler when auth is
// disabled, exercising the disabled branch of the wrapper (previously 0%).
func TestStreamAuthInterceptor_Disabled(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: false, JWTSecret: "s"}, log)
	require.NoError(t, err)

	interceptor := StreamAuthInterceptor(am, log)

	called := false
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		called = true
		return nil
	}
	info := &grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/List"}
	stream := &mockServerStream{ctx: context.Background()}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
	assert.True(t, called, "handler must run when auth is disabled")
}

// TestStreamAuthInterceptor_PublicMethod verifies a default public method
// bypasses authentication through the backward-compatible wrapper.
func TestStreamAuthInterceptor_PublicMethod(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: true, JWTSecret: "s"}, log)
	require.NoError(t, err)

	interceptor := StreamAuthInterceptor(am, log)

	called := false
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		called = true
		return nil
	}
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}
	stream := &mockServerStream{ctx: context.Background()}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
	assert.True(t, called, "public health method must bypass auth")
}

// TestInjectSubjectIntoContext_InvalidToken verifies an invalid token leaves
// the context untouched (no Subject injected), so a downstream per-key authz
// gate fails closed rather than trusting an unauthenticated principal.
func TestInjectSubjectIntoContext_InvalidToken(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: true, JWTSecret: "test-secret-key"}, log)
	require.NoError(t, err)

	cfg := &InterceptorConfig{AuthManager: am, Logger: log}
	base := context.Background()

	out := InjectSubjectIntoContext(base, cfg, "not-a-real-jwt")

	_, ok := SubjectFromContext(out)
	assert.False(t, ok, "invalid token must not inject a Subject")
}

// TestInjectSubjectIntoContext_ValidToken verifies a valid token results in the
// derived Subject being injected for downstream service-layer authorization.
func TestInjectSubjectIntoContext_ValidToken(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: true, JWTSecret: "test-secret-key"}, log)
	require.NoError(t, err)

	user, err := am.CreateUser("injectuser", []string{"readwrite"})
	require.NoError(t, err)
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	cfg := &InterceptorConfig{AuthManager: am, Logger: log}
	out := InjectSubjectIntoContext(context.Background(), cfg, token.AccessToken)

	subj, ok := SubjectFromContext(out)
	require.True(t, ok, "valid token must inject a Subject")
	assert.True(t, subj.Authenticated)
	assert.Equal(t, "injectuser", subj.Username)
}

// TestStreamAuthInterceptor_AllowAnonymousMissingToken verifies the stream
// interceptor does not reject on a missing token when AllowAnonymous is set,
// instead proceeding into the authorization stage (which still fails closed).
// This guards the AllowAnonymous branch of the missing-token handling.
func TestStreamAuthInterceptor_AllowAnonymousMissingToken(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{
		Enabled:        true,
		AllowAnonymous: true,
		JWTSecret:      "test-secret-key",
	}, log)
	require.NoError(t, err)

	interceptor := StreamAuthInterceptor(am, log)

	// No authorization metadata: extractToken fails but AllowAnonymous lets it
	// continue past the Unauthenticated gate into the legacy fail-closed authz.
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	stream := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/List"}
	handler := func(srv interface{}, ss grpc.ServerStream) error { return nil }

	err = interceptor(nil, stream, info, handler)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"anonymous access still fails closed at the authz gate, not Unauthenticated")
}

// TestStreamAuthInterceptor_LegacyFailClosed verifies the legacy AuthManager
// authorization path (AuthzService == nil) fails closed with PermissionDenied
// for a valid token, since Authorize is now unconditionally fail-closed.
func TestStreamAuthInterceptor_LegacyFailClosed(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: true, JWTSecret: "test-secret-key"}, log)
	require.NoError(t, err)

	user, err := am.CreateUser("legacyuser", []string{"readwrite"})
	require.NoError(t, err)
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Backward-compatible wrapper => AuthzService nil => legacy Authorize path.
	interceptor := StreamAuthInterceptor(am, log)

	md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/List"}

	handler := func(srv interface{}, ss grpc.ServerStream) error { return nil }

	err = interceptor(nil, stream, info, handler)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"legacy authz path must fail closed with PermissionDenied")
}
