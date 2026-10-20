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

package apimodule

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// peerTenantContext binds the tenant of the caller's verified client
// certificate when go-qrdb's auth chain bound none. With auth disabled the
// embedded node installs no auth chain, so the certificate is the only
// statement of the caller's tenant; this is the binding the legacy Unix
// listener's tenant interceptor applies. A tenant the chain did bind is never
// replaced, a call with no verified certificate is left unbound (the service
// then refuses it), and a certificate qrdb's rule refuses fails the call.
// An empty trustDomain binds nothing: qrdb reads an unpinned trust domain as
// "accept any", which a server must never do.
func peerTenantContext(ctx context.Context, trustDomain string) (context.Context, error) {
	if trustDomain == "" {
		return ctx, nil
	}
	if _, bound := tenantctx.Lookup(ctx); bound {
		return ctx, nil
	}
	if identity := qrdbsdk.IdentityFromContext(ctx); identity != nil && identity.TenantID != "" {
		return ctx, nil
	}
	scoped, err := grpcapi.TenantContextFromVerifiedPeer(ctx, trustDomain)
	if err != nil {
		return ctx, &PeerCertificateTenantError{Cause: err}
	}
	return scoped, nil
}
