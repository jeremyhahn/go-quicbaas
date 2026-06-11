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

// lifecycle_mcp_authz_test.go covers the fail-closed branches of
// configureMCPQRDBAuthz: every path returns false (enforcement not installed,
// legacy token-only path left intact) rather than installing a half-wired
// validator. The mcp server argument is only touched on the success path (which
// needs a live OIDC provider), so each fail-closed branch passes a nil server.

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
	assert.False(t, s.configureMCPQRDBAuthz(nil))
}

// RequireAuthz=false leaves the legacy token-only path intact (returns false).
func TestConfigureMCPQRDBAuthz_AuthzDisabled(t *testing.T) {
	s := &Server{
		logger: buildTestLogger(t),
		config: &config.Config{Security: config.SecurityConfig{RequireAuthz: false}},
	}
	assert.False(t, s.configureMCPQRDBAuthz(nil))
}

// RequireAuthz=true but no OIDC provider getter: enforcement is not installed.
func TestConfigureMCPQRDBAuthz_NoGetter(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: nil,
	}
	assert.False(t, s.configureMCPQRDBAuthz(nil))
}

// A getter returning (nil, nil) (IAM not yet initialised) yields false.
func TestConfigureMCPQRDBAuthz_NilProviderSkips(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &mcpOIDCGetterService{provider: nil, err: nil},
	}
	assert.False(t, s.configureMCPQRDBAuthz(nil))
}

// A getter error likewise yields false.
func TestConfigureMCPQRDBAuthz_GetterError(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &mcpOIDCGetterService{err: errors.New("provider boom")},
	}
	assert.False(t, s.configureMCPQRDBAuthz(nil))
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

	assert.True(t, s.configureMCPQRDBAuthz(mcpServer),
		"expected enforcement installed with a live provider")
}
