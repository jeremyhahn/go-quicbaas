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

// lifecycle_quic_authz_test.go covers the fail-closed branches of
// configureQUICAuthz: every path that must decline to install the validator
// enforcement seam (leaving the QUIC handler on its no-enforcement path) rather
// than installing a half-wired gate. The server argument is only touched on the
// success path (which needs a live OIDC provider), so each fail-closed branch
// is exercised with a nil quic server.

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// quicOIDCGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService field, and implements quicOIDCProviderGetter so the QUIC authz
// wiring takes the getter path.
type quicOIDCGetterService struct {
	service.QuicBaaS
	provider *iam.OIDCProvider
	err      error
}

func (s *quicOIDCGetterService) GetOIDCProvider(context.Context) (*iam.OIDCProvider, error) {
	return s.provider, s.err
}

// A nil config disables QUIC authz: the function must return before touching the
// (nil) quic server.
func TestConfigureQUICAuthz_NilConfig(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	require.NoError(t, s.configureQUICAuthz(nil))
}

// RequireAuthz=false disables QUIC authz, preserving the no-enforcement path.
func TestConfigureQUICAuthz_AuthzDisabled(t *testing.T) {
	s := &Server{
		logger: buildTestLogger(t),
		config: &config.Config{Security: config.SecurityConfig{RequireAuthz: false}},
	}
	require.NoError(t, s.configureQUICAuthz(nil))
}

// RequireAuthz=true but the service exposes no OIDC provider: FAIL-CLOSED
// typed construction error (daemon refuses to start) — QUIC is never served
// with a silently-missing enforcement gate.
func TestConfigureQUICAuthz_NoGetterFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: nil,
	}
	err := s.configureQUICAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr), "typed construction error, got %v", err)
	require.Equal(t, "quic", cerr.Transport)
}

// A getter that returns (nil, nil) (IAM not yet initialised) is a typed
// construction error under RequireAuthz.
func TestConfigureQUICAuthz_NilProviderFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &quicOIDCGetterService{provider: nil, err: nil},
	}
	err := s.configureQUICAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	require.True(t, errors.Is(err, errGRPCOIDCProviderUnavailable))
}

// A getter error likewise fails closed with the cause wrapped.
func TestConfigureQUICAuthz_GetterErrorFailsClosed(t *testing.T) {
	boom := errors.New("provider boom")
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &quicOIDCGetterService{err: boom},
	}
	err := s.configureQUICAuthz(nil)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	require.True(t, errors.Is(err, boom))
}

// With RequireAuthz=true and a live OIDC provider, configureQUICAuthz builds the
// central platform-token validator and installs it on the QUIC server. The
// server is constructed (an empty TLS config satisfies the non-nil requirement;
// no listener is opened) so the validator-install seam runs.
func TestConfigureQUICAuthz_InstallsValidator(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &quicOIDCGetterService{provider: provider},
	}

	quicServer, err := quicapi.NewServer(
		s.quicBaaSService,
		&quicapi.Config{ListenAddr: "127.0.0.1:0", TLSConfig: &tls.Config{}},
		s.logger,
	)
	require.NoError(t, err)

	// Must install enforcement without panicking or erroring.
	require.NoError(t, s.configureQUICAuthz(quicServer))
}
