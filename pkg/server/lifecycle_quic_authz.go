//go:build quic && iam

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

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// quicOIDCProviderGetter is the narrow accessor the QUIC authz wiring uses to
// reach the IAM OIDC provider. It mirrors the REST and gRPC authz getters so the
// same in-process provider validates tokens across all transports.
type quicOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// configureQUICAuthz installs the unified platform-token authorization decision
// point on the QUIC server when RequireAuthz=true. It is a no-op otherwise,
// leaving the QUIC handler on its no-enforcement path so current behaviour is
// unchanged. It mirrors configureRESTAuthz: every transport routes authorization
// through the same central platform-token Validator. A single Validator call per
// opcode does offline signature / iss / aud / exp verification, DPoP
// sender-constraint (RFC 9449) for cnf.jkt-bound tokens, the offline scp ->
// permission decision against the opcode's qrdb-canonical permission, and the
// per-tenant liveness / revocation check.
func (s *Server) configureQUICAuthz(quicServer *quicapi.Server) {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return
	}

	getter, ok := s.quicBaaSService.(quicOIDCProviderGetter)
	if !ok {
		s.logger.Debug("QUIC authz disabled: service does not expose an OIDC provider")
		return
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		s.logger.Debug("QUIC authz disabled: OIDC provider not available")
		return
	}

	// Build the single central platform-token Validator and install the unified
	// QUIC enforcement seam. QUIC carries no DPoP, so cnf.jkt-bound tokens are
	// rejected by the validator's sender-constraint check (they cannot prove
	// possession over this transport); bearer / service tokens pass.
	platformValidator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		s.logger.WithError(err).Error("QUIC authz disabled: failed to build platform-token validator")
		return
	}

	quicServer.SetValidatorEnforcement(platformValidator)
	s.logger.Info("QUIC platform-token validator authorization installed (RequireAuthz=true)")
}
