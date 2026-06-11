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
	"crypto/x509"

	"google.golang.org/grpc"
)

// TenantContextFunc derives the caller's tenant from a VERIFIED peer
// certificate and returns a context carrying it. It is supplied by the server
// layer (which owns the SPIFFE trust domain + home-realm directory) so this
// transport package needs no dependency on pkg/iam.
//
// The contract is BEST-EFFORT and PERMISSIVE: an implementation MUST return a
// usable context in every case. When the cert resolves to a tenant it returns a
// child context with the tenant set; otherwise it returns the input context
// unchanged. It MUST NOT return an error and MUST NOT reject the call — tenant
// enforcement (e.g. login) is the responsibility of the specific operation,
// never of this global hook.
type TenantContextFunc func(ctx context.Context, cert *x509.Certificate) context.Context

// UnaryTenantInterceptor stamps the caller's tenant onto the request context
// when a verified mTLS peer certificate is present and resolves to a tenant.
// It is best-effort: certless calls (the norm under VerifyClientCertIfGiven)
// and certs that do not map to a tenant proceed unchanged. The call is NEVER
// rejected for lack of a tenant.
//
// A nil resolve func makes this a transparent pass-through.
func UnaryTenantInterceptor(resolve TenantContextFunc) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if resolve != nil {
			if cert := verifiedPeerCertFromGRPC(ctx); cert != nil {
				ctx = resolve(ctx, cert)
			}
		}
		return handler(ctx, req)
	}
}

// StreamTenantInterceptor is the streaming counterpart of
// UnaryTenantInterceptor. It wraps the stream so downstream handlers observe
// the tenant-scoped context. Best-effort; never rejects.
//
// A nil resolve func makes this a transparent pass-through.
func StreamTenantInterceptor(resolve TenantContextFunc) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if resolve != nil {
			ctx := ss.Context()
			if cert := verifiedPeerCertFromGRPC(ctx); cert != nil {
				if scoped := resolve(ctx, cert); scoped != ctx {
					ss = wrapServerStream(ss, scoped)
				}
			}
		}
		return handler(srv, ss)
	}
}
