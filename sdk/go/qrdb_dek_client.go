// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	"google.golang.org/grpc/metadata"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qbmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// qrdbContextDEKClient forwards to a go-qrdb tenant DEK client after binding
// the call's context: a bearer as outgoing gRPC authorization metadata, a
// tenant under go-qrdb's tenant context key, or both.
type qrdbContextDEKClient struct {
	inner  QRDBDEKClient
	bearer string
	tenant string
}

var _ QRDBDEKClient = (*qrdbContextDEKClient)(nil)

func (b *qrdbContextDEKClient) bind(ctx context.Context) context.Context {
	if b.bearer != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
	}
	if b.tenant != "" {
		ctx = qrdbsdk.ContextWithTenant(ctx, b.tenant)
	}
	return ctx
}

// TenantDEKStatus forwards to go-qrdb's TenantDEKStatus on the bound context.
func (b *qrdbContextDEKClient) TenantDEKStatus(ctx context.Context, req *QRDBTenantDEKRequest) (*QRDBTenantDEKStatusResponse, error) {
	return b.inner.TenantDEKStatus(b.bind(ctx), req)
}

// TenantDEKRotate forwards to go-qrdb's TenantDEKRotate on the bound context.
func (b *qrdbContextDEKClient) TenantDEKRotate(ctx context.Context, req *QRDBTenantDEKRequest) (*QRDBTenantDEKStatusResponse, error) {
	return b.inner.TenantDEKRotate(b.bind(ctx), req)
}

// TenantDEKRetire forwards to go-qrdb's TenantDEKRetire on the bound context.
func (b *qrdbContextDEKClient) TenantDEKRetire(ctx context.Context, req *QRDBTenantDEKRetireRequest) (*QRDBTenantDEKStatusResponse, error) {
	return b.inner.TenantDEKRetire(b.bind(ctx), req)
}

// TenantDEKEncrypt forwards to go-qrdb's TenantDEKEncrypt on the bound context.
func (b *qrdbContextDEKClient) TenantDEKEncrypt(ctx context.Context, req *QRDBTenantDEKEncryptRequest) (*QRDBTenantDEKEncryptResponse, error) {
	return b.inner.TenantDEKEncrypt(b.bind(ctx), req)
}

// TenantDEKDecrypt forwards to go-qrdb's TenantDEKDecrypt on the bound context.
func (b *qrdbContextDEKClient) TenantDEKDecrypt(ctx context.Context, req *QRDBTenantDEKDecryptRequest) (*QRDBTenantDEKDecryptResponse, error) {
	return b.inner.TenantDEKDecrypt(b.bind(ctx), req)
}

// TenantDEKRewrap forwards to go-qrdb's TenantDEKRewrap on the bound context.
func (b *qrdbContextDEKClient) TenantDEKRewrap(ctx context.Context, req *QRDBTenantDEKRewrapRequest) (*QRDBTenantDEKRewrapResponse, error) {
	return b.inner.TenantDEKRewrap(b.bind(ctx), req)
}

// qrdbTenantDEK returns the tenant DEK keyring client for the active
// transport, carrying the caller's identity on every call:
//
//   - REST and QUIC: the SDK's own REST transport (QUIC on its HTTP/3
//     client) carries the call, so the bearer set by WithAuthToken is sent;
//     go-qrdb's REST sub-client sets no Authorization header.
//   - MCP: the SDK's own MCP transport carries the call, so the bearer set by
//     WithMCPAuthToken (or WithAuthToken) rides the session's initialize and
//     every tool call.
//   - gRPC and Unix: go-qrdb's sub-client carries the call, and the bearer
//     set by WithAuthToken rides each call as authorization metadata.
//   - Embedded (WithEmbeddedQRDB): the call dispatches in-process, where no
//     bearer exists, so the tenant named by WithTenant is bound under
//     go-qrdb's tenant context key. Without it go-qrdb's keyring service
//     would act on the system tenant.
func (c *client) qrdbTenantDEK() (QRDBDEKClient, error) {
	embedded := c.opts != nil && c.opts.embeddedQRDB != nil
	if !embedded && c.opts != nil {
		switch c.opts.transportType {
		case TransportREST, TransportQUIC:
			if rt, ok := c.transport.(interface{ QRDBDEK() *rest.QRDBDEKClient }); ok {
				return rt.QRDBDEK(), nil
			}
		case TransportMCP:
			if mt, ok := c.transport.(*qbmcp.Transport); ok {
				return mt.QRDBDEK(), nil
			}
		}
	}
	qrdb, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	dek := qrdb.DEK()
	if c.opts == nil {
		return dek, nil
	}
	if embedded {
		if c.opts.tenantID == "" {
			return dek, nil
		}
		return &qrdbContextDEKClient{inner: dek, tenant: qrdbsdk.NormalizeTenantID(c.opts.tenantID)}, nil
	}
	if c.opts.authToken != "" && qrdbSubClientSpeaksGRPC(c.opts.transportType) {
		return &qrdbContextDEKClient{inner: dek, bearer: "Bearer " + c.opts.authToken}, nil
	}
	return dek, nil
}
