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
	"errors"

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/qrdbauthz"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// errQUICOIDCProviderUnavailable indicates the IAM OIDC provider is not yet
// initialized. The QUIC authz validator treats this as a token-validation
// failure rather than a server error.
var errQUICOIDCProviderUnavailable = errors.New("oidc provider unavailable")

// QUICAuthzError wraps a failure that occurs while validating a QUIC bearer
// token. It carries the failing operation and the underlying cause.
type QUICAuthzError struct {
	Op    string
	Cause error
}

func (e *QUICAuthzError) Error() string {
	if e.Cause != nil {
		return "quic authz: " + e.Op + ": " + e.Cause.Error()
	}
	return "quic authz: " + e.Op
}

func (e *QUICAuthzError) Unwrap() error { return e.Cause }

// quicOIDCProviderGetter is the narrow accessor the QUIC authz wiring uses to
// reach the IAM OIDC provider. It mirrors the REST and gRPC authz getters so the
// same in-process provider validates tokens across all transports.
type quicOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// quicOIDCTokenValidator adapts the IAM OIDCProvider to the QUIC handler's
// AuthzTokenValidator seam. It verifies the inbound bearer against the
// provider's signing key and projects the OIDC claims onto a
// *quicapi.AuthzIdentity. It is the single QUIC token-validation seam and
// accepts the SAME OIDC tokens the daemon issues across REST and gRPC.
type quicOIDCTokenValidator struct {
	getter quicOIDCProviderGetter
}

// Compile-time check that quicOIDCTokenValidator satisfies the QUIC seam.
var _ quicapi.AuthzTokenValidator = (*quicOIDCTokenValidator)(nil)

// ValidateAuthzToken verifies the access token and returns the authenticated
// identity. Any verification failure surfaces as a typed error so the QUIC
// handler responds Unauthorized.
func (v *quicOIDCTokenValidator) ValidateAuthzToken(ctx context.Context, token string) (*quicapi.AuthzIdentity, error) {
	provider, err := v.getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &QUICAuthzError{Op: "get oidc provider", Cause: err}
	}
	if provider == nil {
		return nil, &QUICAuthzError{Op: "get oidc provider", Cause: errQUICOIDCProviderUnavailable}
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, &QUICAuthzError{Op: "validate access token", Cause: err}
	}

	return &quicapi.AuthzIdentity{
		ID:       claims.Subject,
		Username: claims.Name,
		TenantID: claims.TenantID,
		Roles:    claims.Roles,
	}, nil
}

// configureQUICAuthz installs the qrdb-backed authorization decision point and
// its OIDC-backed token validator on the QUIC server when RequireAuthz=true. It
// is a no-op otherwise, leaving the QUIC handler on its legacy allow-all path so
// current behaviour is unchanged. It mirrors configureRESTAuthz and the gRPC
// authz interceptor wiring: every transport routes authorization to the same
// qrdb PDP.
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
		s.logger.WithError(err).Error("QUIC authz disabled: failed to build qrdb authorizer adapter")
		return
	}

	quicServer.SetAuthzEnforcement(
		&quicOIDCTokenValidator{getter: getter},
		newQRDBAuthzService(adapter),
	)
	s.logger.Info("QUIC qrdb PDP authorization installed (RequireAuthz=true)")
}
