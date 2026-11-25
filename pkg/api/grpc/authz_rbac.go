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

package grpc

import (
	"context"
	"errors"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// GRPCRBACAuthorizer is go-qrdb's RBAC decision point as the gRPC gate sees
// it: the embedded node's RBACClient.Authorize, with its ABAC decorator. The
// gate asks it after the platform-token Validator has authenticated the
// caller and granted the permission from the token's scp, so a request
// passes only when the token and go-qrdb both allow it.
type GRPCRBACAuthorizer interface {
	Authorize(ctx context.Context, identity *qrdbtransport.Identity, resource, action string) error
}

// WithRBACAuthorizer installs go-qrdb's authorizer on the gRPC authorization
// interceptors. The server always installs it under RequireAuthz.
func WithRBACAuthorizer(rbac GRPCRBACAuthorizer) AuthzInterceptorOption {
	return func(cfg *authzInterceptorConfig) {
		cfg.rbac = rbac
	}
}

// authorizedContextToQRDBIdentity projects the validated token onto the
// identity go-qrdb's authorizer decides for: the token's subject, tenant and
// roles verbatim, and nothing the caller supplies outside the token. A nil
// AuthorizedContext projects to an empty identity, which go-qrdb denies.
func authorizedContextToQRDBIdentity(authzCtx *platformtoken.AuthorizedContext) *qrdbtransport.Identity {
	if authzCtx == nil {
		return &qrdbtransport.Identity{}
	}
	return &qrdbtransport.Identity{
		Subject:  authzCtx.Subject,
		TenantID: authzCtx.TenantID,
		Roles:    authzCtx.Roles,
	}
}

// rbacGRPCStatus maps a go-qrdb authorizer error to a coarse gRPC status,
// fail-closed. go-qrdb's RBAC denial and its ABAC decorator's explicit deny
// are PermissionDenied; every other error (the node is not up, role
// resolution failed, the context was cancelled) means go-qrdb could not
// decide, and is Unavailable. There is no allow path.
func rbacGRPCStatus(err error) error {
	var rbacDenied *qrdbsdk.PermissionDeniedError
	if errors.As(err, &rbacDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	var abacDenied *qrdbsdk.ABACPolicyDeniedError
	if errors.As(err, &abacDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return status.Error(codes.Unavailable, "authorization decision unavailable")
}

// rbacAuthorize asks go-qrdb's authorizer, when one is installed, whether the
// validated caller holds perm ("resource:action"). ctx must carry the
// caller's tenant (contextWithAuthorizedSubject binds it), because go-qrdb
// reads the tenant from the context. Without an installed authorizer the
// validator's decision stands alone.
func rbacAuthorize(ctx context.Context, cfg authzInterceptorConfig, authzCtx *platformtoken.AuthorizedContext, perm string) error {
	if cfg.rbac == nil {
		return nil
	}
	resource, action, ok := strings.Cut(perm, ":")
	if !ok {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	if err := cfg.rbac.Authorize(ctx, authorizedContextToQRDBIdentity(authzCtx), resource, action); err != nil {
		return rbacGRPCStatus(err)
	}
	return nil
}
