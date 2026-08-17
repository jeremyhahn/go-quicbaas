//go:build rest

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

// configure_rest_authz_success_test.go covers the two installed-enforcement
// paths of configureRESTAuthz against a live OIDC provider: the RequireAuthz=true
// platform-token validator gate, and the RequireAuthz=false token-binding gate.
// Each builds a real (un-started) REST server so the installation seam runs.

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// With RequireAuthz=true and a live OIDC provider, configureRESTAuthz builds the
// central platform-token validator and installs the default-deny enforcement
// gate on the REST server. This drives the RequireAuthz branch end to end
// (including buildPlatformValidator).
func TestConfigureRESTAuthz_InstallsValidatorEnforcement(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &oidcGetterService{provider: provider},
	}

	restServer, err := restapi.NewServer(s.quicBaaSService, &restapi.Config{Address: "127.0.0.1:0"}, s.logger)
	require.NoError(t, err)

	require.NoError(t, s.configureRESTAuthz(restServer))
}

// With RequireAuthz=false, configureRESTAuthz installs no local platform-token
// fallback at all (mirroring grpcAuthorizationInterceptors returning
// (nil, nil, nil)): a request reaching the module dispatch path without a
// qrdb identity passes through anonymously, the historical default. This
// drives the non-RequireAuthz early-return tail of the function.
func TestConfigureRESTAuthz_NoOpWhenRequireAuthzFalse(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: false}},
		quicBaaSService: &oidcGetterService{provider: provider},
	}

	restServer, err := restapi.NewServer(s.quicBaaSService, &restapi.Config{Address: "127.0.0.1:0"}, s.logger)
	require.NoError(t, err)

	require.NoError(t, s.configureRESTAuthz(restServer))
}
