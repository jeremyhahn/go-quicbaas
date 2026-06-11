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

// lifecycle_grpc_authz_test.go covers the gRPC (and Unix-over-gRPC) authorization
// wiring after it was unified onto the central platform-token Validator. These
// tests replace the prior online-role-PDP gRPC tests (grpcOIDCTokenValidator /
// grpcQRDBAuthorizer), which were deleted with the bespoke two-step path: the
// gRPC transport now enforces exclusively through buildPlatformValidator (offline
// scp + per-tenant liveness), the same Validator REST and QUIC use.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errGRPCAuthzProviderBoom is a sentinel for the gRPC validator error-path tests.
var errGRPCAuthzProviderBoom = errors.New("grpc provider boom")

// newGRPCAuthzTestLogger builds a quiet logger for the gRPC authz wiring tests.
// It is self-contained (the rest-tagged buildTestLogger is not visible in a
// grpc-only build).
func newGRPCAuthzTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return log
}

// grpcOIDCGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService field, and implements grpcOIDCProviderGetter so the gRPC authz
// wiring takes the getter path. GetOIDCProvider returns the configured
// provider/err; none of the embedded methods are reached on these paths.
type grpcOIDCGetterService struct {
	service.QuicBaaS
	provider *iam.OIDCProvider
	err      error
}

func (s *grpcOIDCGetterService) GetOIDCProvider(context.Context) (*iam.OIDCProvider, error) {
	return s.provider, s.err
}

// TestGRPCAuthorizationInterceptors_AuthzDisabled asserts that when RequireAuthz
// is false the builder installs NO enforcement interceptors, leaving the gRPC
// chain unchanged (no silent-allow gate, no deny gate — pre-authz behavior).
func TestGRPCAuthorizationInterceptors_AuthzDisabled(t *testing.T) {
	s := &Server{
		logger: newGRPCAuthzTestLogger(t),
		config: &config.Config{Security: config.SecurityConfig{RequireAuthz: false}},
	}
	unary, stream := s.grpcAuthorizationInterceptors()
	assert.Nil(t, unary, "no unary interceptor when RequireAuthz=false")
	assert.Nil(t, stream, "no stream interceptor when RequireAuthz=false")
}

// TestGRPCAuthorizationInterceptors_NilConfig asserts a nil config also disables
// enforcement (fail-safe: no gate installed against an unconfigured server).
func TestGRPCAuthorizationInterceptors_NilConfig(t *testing.T) {
	s := &Server{logger: newGRPCAuthzTestLogger(t)}
	unary, stream := s.grpcAuthorizationInterceptors()
	assert.Nil(t, unary)
	assert.Nil(t, stream)
}

// TestGRPCAuthorizationInterceptors_NoGetter asserts that when RequireAuthz=true
// but the service does not expose an OIDC provider, the builder declines to
// install a gate (it cannot validate tokens) rather than installing one that
// would deny every request opaquely.
func TestGRPCAuthorizationInterceptors_NoGetter(t *testing.T) {
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: nil,
	}
	unary, stream := s.grpcAuthorizationInterceptors()
	assert.Nil(t, unary)
	assert.Nil(t, stream)
}

// TestGRPCAuthorizationInterceptors_NilProviderSkips covers the
// provider-unavailable branch: a getter returning (nil, nil) (IAM not yet
// initialised) must make the builder return nil interceptors without panicking
// on the subsequent validator construction.
func TestGRPCAuthorizationInterceptors_NilProviderSkips(t *testing.T) {
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &grpcOIDCGetterService{provider: nil, err: nil},
	}
	unary, stream := s.grpcAuthorizationInterceptors()
	assert.Nil(t, unary)
	assert.Nil(t, stream)
}

// TestGRPCAuthorizationInterceptors_Installed asserts that with RequireAuthz=true
// and a live OIDC provider the builder constructs the platform-token validator
// and returns NON-nil unary + stream interceptors (the enforcement gate the gRPC
// and Unix transports share).
func TestGRPCAuthorizationInterceptors_Installed(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &grpcOIDCGetterService{provider: provider},
	}
	unary, stream := s.grpcAuthorizationInterceptors()
	assert.NotNil(t, unary, "unary interceptor installed with a live provider")
	assert.NotNil(t, stream, "stream interceptor installed with a live provider")
}

// TestBuildPlatformValidator_NotAGetter asserts buildPlatformValidator fails
// closed with a typed *GRPCAuthzError when the service cannot supply an OIDC
// provider (the field is a nil interface, so the type assertion fails).
func TestBuildPlatformValidator_NotAGetter(t *testing.T) {
	s := &Server{logger: newGRPCAuthzTestLogger(t)}
	v, err := s.buildPlatformValidator(context.Background())
	assert.Nil(t, v)
	var gerr *GRPCAuthzError
	require.True(t, errors.As(err, &gerr))
	assert.Equal(t, "build platform validator", gerr.Op)
	assert.True(t, errors.Is(err, errGRPCOIDCProviderUnavailable))
}

// TestBuildPlatformValidator_GetterError asserts a failure obtaining the OIDC
// provider surfaces as a typed *GRPCAuthzError wrapping the underlying cause.
func TestBuildPlatformValidator_GetterError(t *testing.T) {
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		quicBaaSService: &grpcOIDCGetterService{err: errGRPCAuthzProviderBoom},
	}
	v, err := s.buildPlatformValidator(context.Background())
	assert.Nil(t, v)
	var gerr *GRPCAuthzError
	require.True(t, errors.As(err, &gerr))
	assert.Equal(t, "get oidc provider", gerr.Op)
	assert.True(t, errors.Is(err, errGRPCAuthzProviderBoom))
}

// TestBuildPlatformValidator_NilProvider asserts a nil provider (IAM not yet
// initialised) yields a *GRPCAuthzError carrying errGRPCOIDCProviderUnavailable
// so the caller fails closed instead of constructing a validator with no key
// source.
func TestBuildPlatformValidator_NilProvider(t *testing.T) {
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		quicBaaSService: &grpcOIDCGetterService{provider: nil, err: nil},
	}
	v, err := s.buildPlatformValidator(context.Background())
	assert.Nil(t, v)
	var gerr *GRPCAuthzError
	require.True(t, errors.As(err, &gerr))
	assert.Equal(t, "get oidc provider", gerr.Op)
	assert.True(t, errors.Is(err, errGRPCOIDCProviderUnavailable))
}
