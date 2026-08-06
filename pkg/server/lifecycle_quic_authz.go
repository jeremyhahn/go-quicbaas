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
// Gate construction is FAIL-CLOSED under RequireAuthz: when the OIDC
// provider or the platform-token Validator cannot be built, a typed
// *AuthzGateConstructionError is returned and daemon startup ABORTS
// (refuse-to-start posture) — QUIC is never served with a silently-missing
// enforcement gate.
func (s *Server) configureQUICAuthz(quicServer *quicapi.Server) error {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return nil
	}

	getter, ok := s.quicBaaSService.(quicOIDCProviderGetter)
	if !ok {
		return &AuthzGateConstructionError{
			Transport: "quic",
			Op:        "service does not expose an OIDC provider",
		}
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		if err == nil {
			err = errGRPCOIDCProviderUnavailable
		}
		return &AuthzGateConstructionError{
			Transport: "quic",
			Op:        "resolve OIDC provider",
			Cause:     err,
		}
	}

	// Build the single central platform-token Validator and install the unified
	// QUIC enforcement seam. QUIC carries no DPoP, so cnf.jkt-bound tokens are
	// rejected by the validator's sender-constraint check (they cannot prove
	// possession over this transport); bearer / service tokens pass. A build
	// failure ABORTS startup (fail-closed) instead of serving QUIC ungated.
	platformValidator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		return &AuthzGateConstructionError{
			Transport: "quic",
			Op:        "build platform-token validator",
			Cause:     err,
		}
	}

	quicServer.SetValidatorEnforcement(platformValidator)
	s.logger.Info("QUIC platform-token validator authorization installed (RequireAuthz=true)")
	return nil
}
