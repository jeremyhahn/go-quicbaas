// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quicbaas

import (
	"context"
	"crypto/x509"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// EmbeddedIdentityBinder binds a context to a qrdb identity on the embedded
// node. The embedding server satisfies it: ServiceContext binds the server's
// service principal, and DelegatedContext binds the identity a tenant-issued
// delegation certificate grants that principal inside the delegating tenant.
type EmbeddedIdentityBinder interface {
	ServiceContext(ctx context.Context) (context.Context, error)
	DelegatedContext(ctx context.Context, chain []*x509.Certificate) (context.Context, error)
}

// EmbeddedIdentity is the qrdb identity every call of an embedded client runs
// as. Build one with ServicePrincipalIdentity or DelegatedIdentity and pass it
// to WithEmbeddedIdentity. The zero value names no identity.
type EmbeddedIdentity struct {
	bind func(ctx context.Context) (context.Context, error)
	// delegator is the binder of a delegated identity, nil for the service
	// principal. A delegation's grants are enforced by its AuthMiddleware.
	delegator EmbeddedIdentityBinder
	// principal is the binder of the service principal, nil for a
	// delegation. The principal's grants are enforced by its AuthMiddleware
	// when the node runs authorization.
	principal EmbeddedIdentityBinder
}

// ServicePrincipalIdentity names the embedding server's service principal,
// for a client that acts as the system. qrdb keeps the call's tenant, so the
// system identity reaches only the system tenant.
func ServicePrincipalIdentity(binder EmbeddedIdentityBinder) EmbeddedIdentity {
	if binder == nil {
		return EmbeddedIdentity{}
	}
	return EmbeddedIdentity{bind: binder.ServiceContext, principal: binder}
}

// DelegatedIdentity names the identity a tenant-issued delegation certificate
// (chain, leaf first) grants inside that tenant, for a client that acts inside
// the tenant. The node verifies the delegation on every call.
func DelegatedIdentity(binder EmbeddedIdentityBinder, chain []*x509.Certificate) EmbeddedIdentity {
	if binder == nil || len(chain) == 0 {
		return EmbeddedIdentity{}
	}
	return EmbeddedIdentity{
		bind: func(ctx context.Context) (context.Context, error) {
			return binder.DelegatedContext(ctx, chain)
		},
		delegator: binder,
	}
}

// isZero reports whether the identity names nothing.
func (id EmbeddedIdentity) isZero() bool {
	return id.bind == nil
}

// WithEmbeddedIdentity names the qrdb identity an embedded client's calls run
// as. It is required with WithEmbedded: the embedded transport binds every
// call's context through this identity before dispatch, and nothing binds an
// identity implicitly.
func WithEmbeddedIdentity(id EmbeddedIdentity) Option {
	return func(o *clientOptions) error {
		if id.isZero() {
			return errors.NewInvalidInput("WithEmbeddedIdentity",
				"identity must come from ServicePrincipalIdentity or DelegatedIdentity with a binder")
		}
		o.embeddedIdentity = id
		return nil
	}
}

// EmbeddedAuthorizer is implemented by a binder that also authorizes the
// calls it binds: the qrdb node (and the quicbaas server over it) hands out
// the same AuthMiddleware its network transports enforce with.
type EmbeddedAuthorizer interface {
	AuthMiddleware() *qrdbsdk.AuthMiddleware
}

// DelegationWithoutAuthorizationError reports a delegated identity whose
// binder supplies no authorization middleware. A delegation is a bounded
// grant; run without the node's middleware it would be bounded by nothing,
// so the qrdb sub-client is never built for it.
type DelegationWithoutAuthorizationError struct{}

func (*DelegationWithoutAuthorizationError) Error() string {
	return "quicbaas: delegated identity: the binder supplies no qrdb AuthMiddleware to enforce the delegation's grants"
}

// authMiddleware returns the middleware that enforces the identity's grants,
// taken from its binder. For the service principal it is the node's
// middleware, or nil when the node runs no authorization; a delegation whose
// binder supplies none gets DelegationWithoutAuthorizationError.
func (id EmbeddedIdentity) authMiddleware() (*qrdbsdk.AuthMiddleware, error) {
	if id.delegator == nil {
		if authorizer, ok := id.principal.(EmbeddedAuthorizer); ok {
			return authorizer.AuthMiddleware(), nil
		}
		return nil, nil
	}
	authorizer, ok := id.delegator.(EmbeddedAuthorizer)
	if !ok {
		return nil, &DelegationWithoutAuthorizationError{}
	}
	mw := authorizer.AuthMiddleware()
	if mw == nil {
		return nil, &DelegationWithoutAuthorizationError{}
	}
	return mw, nil
}

// authorize enforces perms for a delegated identity through the node's own
// AuthMiddleware, against the tenant the delegation binds to. It guards
// calls whose backend runs as the service principal, where node
// authorization would otherwise never see the delegation's grants. The
// service principal authorizes nothing here: its calls are not delegated.
func (id EmbeddedIdentity) authorize(ctx context.Context, perms ...qrdbsdk.Permission) error {
	if id.delegator == nil {
		return nil
	}
	mw, err := id.authMiddleware()
	if err != nil {
		return err
	}
	if mw == nil {
		return nil
	}
	bound, err := id.bind(ctx)
	if err != nil {
		return err
	}
	// An unbound tenant is the system scope, as for qrdb's own embedded
	// transport.
	tenantID, _ := qrdbsdk.TenantFromContext(bound)
	for _, perm := range perms {
		if err := mw.AuthorizeTenant(bound, tenantID, perm); err != nil {
			return err
		}
	}
	return nil
}
