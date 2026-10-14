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

// Package tenantctx re-exports pkg/tenantctx, the one place go-quicbaas reads
// and binds the tenant a request acts for, so an embedding host binds and
// reads the tenant exactly as the server's own services do:
//
//	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
//
// becomes:
//
//	"github.com/jeremyhahn/go-quicbaas/sdk/go/tenantctx"
//
// Every symbol is a function value of the pkg/tenantctx function, so there is
// no behavior here: an absent tenant is still refused rather than read as the
// system tenant, and a request naming another tenant is still refused.
package tenantctx

import (
	pkgtenantctx "github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

var (
	// WithTenant binds tenantID as the request's tenant under go-qrdb's
	// central key. An empty tenantID binds nothing.
	WithTenant = pkgtenantctx.WithTenant

	// FromContext returns the authenticated tenant of the request, or an
	// ErrForbidden error when none is bound. op names the calling operation.
	FromContext = pkgtenantctx.FromContext

	// Target resolves the tenant a request acts for when the caller names
	// one: empty or the caller's own tenant resolves to the caller's tenant,
	// and any other tenant is refused with an ErrForbidden error.
	Target = pkgtenantctx.Target
)
