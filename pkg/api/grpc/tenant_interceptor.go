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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/grpc"
)

// tenantContextFromClientCert applies qrdb's canonical certificate-to-tenant
// rule to a verified peer certificate and binds the normalized tenant.
func tenantContextFromClientCert(ctx context.Context, cert *x509.Certificate, trustDomain string) (context.Context, error) {
	tenantID, err := qrdbsdk.TenantFromClientCert(cert, trustDomain)
	if err != nil {
		return ctx, err
	}
	return qrdbsdk.ContextWithTenant(ctx, qrdbsdk.NormalizeTenantID(tenantID)), nil
}

// TenantContextFromVerifiedPeer binds the qrdb tenant of the caller's verified
// mTLS peer certificate onto ctx. A call with no verified certificate returns
// ctx unchanged; a present certificate that qrdb refuses returns qrdb's error.
func TenantContextFromVerifiedPeer(ctx context.Context, trustDomain string) (context.Context, error) {
	cert := verifiedPeerCertFromGRPC(ctx)
	if cert == nil {
		return ctx, nil
	}
	return tenantContextFromClientCert(ctx, cert, trustDomain)
}

// UnaryTenantInterceptor stamps the qrdb tenant derived from the caller's
// verified mTLS peer certificate onto the request context. Certless calls still
// pass through unchanged; a present certificate that qrdb refuses rejects the
// call with qrdb's error.
func UnaryTenantInterceptor(trustDomain string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		scoped, err := TenantContextFromVerifiedPeer(ctx, trustDomain)
		if err != nil {
			return nil, err
		}
		return handler(scoped, req)
	}
}

// StreamTenantInterceptor is the streaming counterpart of
// UnaryTenantInterceptor. It wraps the stream so downstream handlers observe
// the tenant-scoped context.
func StreamTenantInterceptor(trustDomain string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		scoped, err := TenantContextFromVerifiedPeer(ctx, trustDomain)
		if err != nil {
			return err
		}
		if scoped != ctx {
			ss = wrapServerStream(ss, scoped)
		}
		return handler(srv, ss)
	}
}
