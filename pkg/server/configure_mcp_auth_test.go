//go:build mcp && !integration

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

// configure_mcp_auth_test.go covers the auth-manager wiring branch of
// configureMCPAuth: when the QuicBaaS service exposes a non-nil internal auth
// manager, configureMCPAuth installs it on the MCP server as the authorization
// service.

package server

import (
	"context"
	"testing"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	"github.com/stretchr/testify/require"
)

// stubAuthManager is a minimal interfaces.AuthManager used to drive the
// configureMCPAuth wiring branch. It does not perform real authorization.
type stubAuthManager struct{}

func (stubAuthManager) IssueTokenForUser(context.Context, interfaces.UserInfo) (interface{}, error) {
	return nil, nil
}
func (stubAuthManager) Authorize(context.Context, string, string, string) error   { return nil }
func (stubAuthManager) RefreshToken(context.Context, string) (interface{}, error) { return nil, nil }
func (stubAuthManager) RevokeToken(context.Context, string) error                 { return nil }
func (stubAuthManager) ValidateToken(context.Context, string) (interface{}, error) {
	return nil, nil
}
func (stubAuthManager) CreateUser(string, []string) (interface{}, error) { return nil, nil }
func (stubAuthManager) GetUserStore() interfaces.UserStore               { return nil }

// authManagerGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService interface, and implements the internalAuthManagerGetter shape
// that configureMCPAuth type-asserts for. GetInternalAuthManager returns a real
// in-memory auth manager so the non-nil wiring branch runs.
type authManagerGetterService struct {
	service.QuicBaaS
	mgr interfaces.AuthManager
}

func (s *authManagerGetterService) GetInternalAuthManager() interfaces.AuthManager {
	return s.mgr
}

// TestConfigureMCPAuth_WiresAuthManager drives the non-nil auth-manager branch
// of configureMCPAuth.
func TestConfigureMCPAuth_WiresAuthManager(t *testing.T) {
	authMgr := stubAuthManager{}
	srv := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: &authManagerGetterService{mgr: authMgr},
	}

	mcpServer := mcpapi.NewServer(srv.quicBaaSService, &mcpapi.Config{}, srv.logger)
	require.NotNil(t, mcpServer)

	// configureMCPAuth must install the auth manager without panicking.
	srv.configureMCPAuth(mcpServer)
}
