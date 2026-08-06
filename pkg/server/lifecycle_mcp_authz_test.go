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

// lifecycle_mcp_authz_test.go covers the branches of configureMCPQRDBAuthz.
// With RequireAuthz=false (or nil config) the function is a no-op returning
// (false, nil) so the legacy token-only path stays intact. With
// RequireAuthz=true, gate construction is FAIL-CLOSED: a missing OIDC
// provider or validator-build failure returns a typed
// *AuthzGateConstructionError that aborts daemon startup. The mcp server
// argument is only touched on the success path (which needs a live OIDC
// provider), so each error branch passes a nil server.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mcpOIDCGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService field, and implements mcpOIDCProviderGetter so the MCP authz
// wiring takes the getter path.
type mcpOIDCGetterService struct {
	service.QuicBaaS
	provider *iam.OIDCProvider
	err      error
}

func (s *mcpOIDCGetterService) GetOIDCProvider(context.Context) (*iam.OIDCProvider, error) {
	return s.provider, s.err
}

// A nil config disables MCP authz and returns false without touching the server.
func TestConfigureMCPQRDBAuthz_NilConfig(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	installed, err := s.configureMCPQRDBAuthz(nil)
	require.NoError(t, err)
	assert.False(t, installed)
}

// RequireAuthz=false leaves the legacy token-only path intact (returns false).
func TestConfigureMCPQRDBAuthz_AuthzDisabled(t *testing.T) {
	s := &Server{
		logger: buildTestLogger(t),
		config: &config.Config{Security: config.SecurityConfig{RequireAuthz: false}},
	}
	installed, err := s.configureMCPQRDBAuthz(nil)
	require.NoError(t, err)
	assert.False(t, installed)
}

// RequireAuthz=true but no OIDC provider getter: FAIL-CLOSED typed
// construction error (daemon refuses to start), never a silent false.
func TestConfigureMCPQRDBAuthz_NoGetterFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: nil,
	}
	installed, err := s.configureMCPQRDBAuthz(nil)
	assert.False(t, installed)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr), "typed construction error, got %v", err)
	assert.Equal(t, "mcp", cerr.Transport)
}

// A getter returning (nil, nil) (IAM not yet initialised) is a typed
// construction error under RequireAuthz.
func TestConfigureMCPQRDBAuthz_NilProviderFailsClosed(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &mcpOIDCGetterService{provider: nil, err: nil},
	}
	installed, err := s.configureMCPQRDBAuthz(nil)
	assert.False(t, installed)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	assert.True(t, errors.Is(err, errGRPCOIDCProviderUnavailable))
}

// A getter error likewise fails closed with the cause wrapped.
func TestConfigureMCPQRDBAuthz_GetterErrorFailsClosed(t *testing.T) {
	boom := errors.New("provider boom")
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &mcpOIDCGetterService{err: boom},
	}
	installed, err := s.configureMCPQRDBAuthz(nil)
	assert.False(t, installed)
	var cerr *AuthzGateConstructionError
	require.True(t, errors.As(err, &cerr))
	assert.True(t, errors.Is(err, boom))
}

// With RequireAuthz=true and a live OIDC provider, configureMCPQRDBAuthz builds
// the central platform-token validator and installs it on the MCP server,
// returning true so the caller skips the legacy token-only wiring. This drives
// the success path including buildPlatformValidator's full construction.
func TestConfigureMCPQRDBAuthz_InstallsValidator(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &mcpOIDCGetterService{provider: provider},
	}

	mcpServer := mcpapi.NewServer(s.quicBaaSService, &mcpapi.Config{}, s.logger)
	require.NotNil(t, mcpServer)

	installed, err := s.configureMCPQRDBAuthz(mcpServer)
	require.NoError(t, err)
	assert.True(t, installed, "expected enforcement installed with a live provider")
}
