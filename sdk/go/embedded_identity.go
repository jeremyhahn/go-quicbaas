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
}

// ServicePrincipalIdentity names the embedding server's service principal,
// for a client that acts as the system. qrdb keeps the call's tenant, so the
// system identity reaches only the system tenant.
func ServicePrincipalIdentity(binder EmbeddedIdentityBinder) EmbeddedIdentity {
	if binder == nil {
		return EmbeddedIdentity{}
	}
	return EmbeddedIdentity{bind: binder.ServiceContext}
}

// DelegatedIdentity names the identity a tenant-issued delegation certificate
// (chain, leaf first) grants inside that tenant, for a client that acts inside
// the tenant. The node verifies the delegation on every call.
func DelegatedIdentity(binder EmbeddedIdentityBinder, chain []*x509.Certificate) EmbeddedIdentity {
	if binder == nil || len(chain) == 0 {
		return EmbeddedIdentity{}
	}
	return EmbeddedIdentity{bind: func(ctx context.Context) (context.Context, error) {
		return binder.DelegatedContext(ctx, chain)
	}}
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
