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

package quicbaas

import (
	"context"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// trustedCAServiceName is the transport service name of the re-exported
// go-xkms CAService. Each concrete transport bridges it to the go-xkms CA the
// server composes (REST: /api/v1/xkms/ca/*).
const trustedCAServiceName = "quicbaas.TrustedCA"

// trustedCATransportClient is the transport-backed TrustedCAService. Every
// verb is one quicbaas.TrustedCA round-trip carrying go-xkms's own request and
// response types, so the SDK re-exports the go-xkms CA under go-xkms's names
// and types (#1330).
type trustedCATransportClient struct {
	t transport.Transport
}

var _ TrustedCAService = (*trustedCATransportClient)(nil)

func newTrustedCAService(t transport.Transport) TrustedCAService {
	return &trustedCATransportClient{t: t}
}

// trustedCARequest performs one quicbaas.TrustedCA round-trip and asserts the
// go-xkms response type.
func trustedCARequest[Resp any](ctx context.Context, c *trustedCATransportClient, method string, req interface{}, isNil bool) (*Resp, error) {
	op := "trustedca." + method
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput(op, "context is required")
	}
	if isNil {
		return nil, sdkerrors.NewInvalidInput(op, "request is required")
	}
	if c.t == nil {
		return nil, sdkerrors.NewUnavailable(op, "transport is not configured")
	}
	resp, err := c.t.Request(ctx, trustedCAServiceName, method, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return new(Resp), nil
	}
	out, ok := resp.(*Resp)
	if !ok {
		return nil, sdkerrors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// GetCABundle retrieves the CA certificate bundle.
func (c *trustedCATransportClient) GetCABundle(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	return trustedCARequest[xkmstransport.GetCABundleResponse](ctx, c, "GetCABundle", req, req == nil)
}

// GetCACertificate retrieves a CA certificate.
func (c *trustedCATransportClient) GetCACertificate(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	return trustedCARequest[xkmstransport.GetCACertificateResponse](ctx, c, "GetCACertificate", req, req == nil)
}

// SignCSR signs a certificate signing request.
func (c *trustedCATransportClient) SignCSR(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	return trustedCARequest[xkmstransport.SignCSRResponse](ctx, c, "SignCSR", req, req == nil)
}

// IssueCertificate issues a new certificate.
func (c *trustedCATransportClient) IssueCertificate(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	return trustedCARequest[xkmstransport.IssueCertificateResponse](ctx, c, "IssueCertificate", req, req == nil)
}

// RevokeCertificate revokes a certificate.
func (c *trustedCATransportClient) RevokeCertificate(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	return trustedCARequest[xkmstransport.RevokeCertificateResponse](ctx, c, "RevokeCertificate", req, req == nil)
}

// GenerateCRL generates a certificate revocation list.
func (c *trustedCATransportClient) GenerateCRL(ctx context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	return trustedCARequest[xkmstransport.GenerateCRLResponse](ctx, c, "GenerateCRL", req, req == nil)
}

// IsRevoked reports whether a certificate is revoked.
func (c *trustedCATransportClient) IsRevoked(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	return trustedCARequest[xkmstransport.IsRevokedResponse](ctx, c, "IsRevoked", req, req == nil)
}
