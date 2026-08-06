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

//go:build rest

// configure_rest_authz_branches_test.go covers the OIDC-provider-unavailable
// early-return branch of configureRESTAuthz: when the service implements
// oidcProviderGetter but the provider is nil (IAM not yet initialised), authz
// wiring is skipped rather than installed against a nil provider.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// oidcGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService interface, and implements oidcProviderGetter so
// configureRESTAuthz takes the getter path. GetOIDCProvider returns the
// configured provider/err; none of the embedded methods are reached.
type oidcGetterService struct {
	service.QuicBaaS
	provider *iam.OIDCProvider
	err      error
}

func (s *oidcGetterService) GetOIDCProvider(context.Context) (*iam.OIDCProvider, error) {
	return s.provider, s.err
}

// TestConfigureRESTAuthz_NilProviderSkips covers the provider-unavailable branch:
// a getter that returns (nil, nil) must make configureRESTAuthz return early
// without panicking on the subsequent validator/RBAC wiring.
func TestConfigureRESTAuthz_NilProviderSkips(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: &oidcGetterService{provider: nil, err: nil},
	}

	// restServer is unused on the early-return path; nil is safe here.
	var restServer *restapi.Server
	require.NoError(t, s.configureRESTAuthz(restServer))
}

// TestConfigureRESTAuthz_RequireAuthzNoGetterFailsClosed asserts the
// FAIL-CLOSED posture: RequireAuthz=true without an OIDC provider getter is
// a typed *AuthzGateConstructionError that aborts startup — REST is never
// served with a silently-missing enforcement gate.
func TestConfigureRESTAuthz_RequireAuthzNoGetterFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: nil,
	}
	err := s.configureRESTAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr), "typed construction error, got %v", err)
	require.Equal(t, "rest", cerr.Transport)
}

// TestConfigureRESTAuthz_RequireAuthzNilProviderFailsClosed asserts a getter
// returning (nil, nil) (IAM not initialised) under RequireAuthz is a typed
// construction error carrying the OIDC-unavailable sentinel.
func TestConfigureRESTAuthz_RequireAuthzNilProviderFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &oidcGetterService{provider: nil, err: nil},
	}
	err := s.configureRESTAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	require.True(t, errors.Is(err, ErrOIDCProviderUnavailable))
}

// TestConfigureRESTAuthz_RequireAuthzGetterErrorFailsClosed asserts a getter
// error under RequireAuthz fails closed with the cause wrapped.
func TestConfigureRESTAuthz_RequireAuthzGetterErrorFailsClosed(t *testing.T) {
	boom := errors.New("provider boom")
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &oidcGetterService{err: boom},
	}
	err := s.configureRESTAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	require.True(t, errors.Is(err, boom))
}
