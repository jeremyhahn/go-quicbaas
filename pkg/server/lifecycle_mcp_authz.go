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

package server

import (
	"context"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// mcpOIDCProviderGetter is the narrow accessor the MCP authz wiring uses to
// reach the IAM OIDC provider. It mirrors the REST / gRPC / QUIC authz getters
// so the same in-process provider validates tokens across every transport.
type mcpOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// configureMCPQRDBAuthz installs the unified platform-token authorization
// decision point on the MCP server when RequireAuthz=true. It builds the single
// central platform-token Validator (the SAME validator the REST gate uses) and
// installs it through the MCP enforcement seam: per tool call, ONE
// validator.Authorize performs offline signature / iss / aud / exp verification,
// DPoP sender-constraint (RFC 9449) for cnf.jkt-bound tokens, the offline scp ->
// permission decision against the qrdb-canonical "resource:action" the tool
// requires, and the per-tenant liveness/revocation check. This REPLACES the prior
// online qrdb role-PDP path on MCP.
//
// MCP carries no DPoP proof channel; the validator is called with an empty proof
// per tool call. A cnf.jkt-bound token presented over MCP is therefore denied —
// intended, not a relaxation.
//
// It is a no-op when RequireAuthz=false (and the caller leaves the prior
// SetAuthorizationService(nil, authManager) wiring intact), so MCP keeps its
// legacy token-only behaviour: a non-public tool requires a valid token but the
// authorization decision is not enforced. It returns true when enforcement was
// installed so the caller can skip the legacy wiring. On a validator-build
// failure it logs and returns false, mirroring configureRESTAuthz: enforcement is
// not installed and the legacy token-only path is left in place.
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

	// Build the single central platform-token Validator and install the unified
	// enforcement seam. One Validator call per tool decides: offline token
	// validation, DPoP sender-constraint for cnf.jkt-bound tokens, the offline scp
	// -> permission decision, and the per-tenant liveness/revocation check.
	platformValidator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		s.logger.WithError(err).Error("MCP authz disabled: failed to build platform-token validator")
		return false
	}

	mcpServer.SetValidatorAuthorizer(platformValidator)
	s.logger.Info("MCP platform-token validator authorization installed (RequireAuthz=true)")
	return true
}
