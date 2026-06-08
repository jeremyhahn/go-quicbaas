//go:build mcp && iam

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

package server

import (
	"context"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/qrdbauthz"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// mcpOIDCProviderGetter is the narrow accessor the MCP authz wiring uses to
// reach the IAM OIDC provider. It mirrors the REST / gRPC / QUIC authz getters
// so the same in-process provider validates tokens across every transport.
type mcpOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// configureMCPQRDBAuthz installs the qrdb-backed authorization decision point on
// the MCP server when RequireAuthz=true. It wires the OIDC provider (so the tool
// authorization checker validates the SAME tokens REST / gRPC / QUIC accept and
// resolves the caller's roles + tenant), installs the qrdb-backed authorization
// service, and switches the checker to the qrdb-canonical tool -> permission
// mapper with the default-deny posture for unmapped tools.
//
// It is a no-op when RequireAuthz=false (and the caller leaves the prior
// SetAuthorizationService(nil, authManager) wiring intact), so MCP keeps its
// legacy token-only behaviour: a non-public tool requires a valid token but the
// authorization decision is not enforced. Returns true when enforcement was
// installed so the caller can skip the legacy wiring.
func (s *Server) configureMCPQRDBAuthz(mcpServer *mcpapi.Server) bool {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return false
	}

	getter, ok := s.quicBaaSService.(mcpOIDCProviderGetter)
	if !ok {
		s.logger.Debug("MCP authz disabled: service does not expose an OIDC provider")
		return false
	}
	provider, err := getter.GetOIDCProvider(context.Background())
	if err != nil || provider == nil {
		s.logger.Debug("MCP authz disabled: OIDC provider not available")
		return false
	}

	// Build the single qrdb-backed authorizer adapter. It late-binds the qrdb
	// RBAC client (resolved per request via GetQRDBNodeClient().RBAC()).
	rbacResolver := func() qrdbtransport.RBACClient {
		node := s.GetQRDBNodeClient()
		if node == nil {
			return nil
		}
		return node.RBAC()
	}
	adapter, err := qrdbauthz.New(rbacResolver, nil)
	if err != nil {
		s.logger.WithError(err).Error("MCP authz disabled: failed to build qrdb authorizer adapter")
		return false
	}

	// Validate tokens against the OIDC provider (roles + tenant), enforce the
	// decision through the qrdb PDP, and use the qrdb-canonical tool mapper.
	mcpServer.SetOIDCProvider(iam.NewOIDCProviderAdapter(provider))
	mcpServer.SetAuthorizationService(newQRDBAuthzService(adapter), nil)
	mcpServer.EnableQRDBAuthzMapping()
	s.logger.Info("MCP qrdb PDP authorization installed (RequireAuthz=true)")
	return true
}
