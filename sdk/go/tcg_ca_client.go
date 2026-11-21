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

// tcgCAServiceName is the transport service name of the re-exported go-xkms
// TCGCAService (Security().TCGCA()).
const tcgCAServiceName = "quicbaas.TCGCA"

// tcgCATransportClient is the transport-backed TCGCAService. Every verb is
// one quicbaas.TCGCA round-trip carrying go-xkms's own request and response
// types, so the SDK re-exports the go-xkms TCG CA under go-xkms's names and
// types (#1330).
type tcgCATransportClient struct {
	t transport.Transport
}

var _ TCGCAService = (*tcgCATransportClient)(nil)

func newTCGCAService(t transport.Transport) TCGCAService {
	return &tcgCATransportClient{t: t}
}

// tcgCARequest performs one quicbaas.TCGCA round-trip and asserts the go-xkms
// response type.
func tcgCARequest[Resp any](ctx context.Context, c *tcgCATransportClient, method string, req interface{}, isNil bool) (*Resp, error) {
	op := "tcgca." + method
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput(op, "context is required")
	}
	if isNil {
		return nil, sdkerrors.NewInvalidInput(op, "request is required")
	}
	if c.t == nil {
		return nil, sdkerrors.NewUnavailable(op, "transport is not configured")
	}
	resp, err := c.t.Request(ctx, tcgCAServiceName, method, req)
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

// IssueEKCertificate issues an Endorsement Key certificate.
func (c *tcgCATransportClient) IssueEKCertificate(ctx context.Context, req *xkmstransport.IssueEKCertificateRequest) (*xkmstransport.IssueEKCertificateResponse, error) {
	return tcgCARequest[xkmstransport.IssueEKCertificateResponse](ctx, c, "IssueEKCertificate", req, req == nil)
}

// IssueAKCertificate issues an Attestation Key certificate.
func (c *tcgCATransportClient) IssueAKCertificate(ctx context.Context, req *xkmstransport.IssueAKCertificateRequest) (*xkmstransport.IssueAKCertificateResponse, error) {
	return tcgCARequest[xkmstransport.IssueAKCertificateResponse](ctx, c, "IssueAKCertificate", req, req == nil)
}

// SignTCGCSR signs a packed TCG-CSR-IDEVID and returns the IAK and IDevID
// certificates.
func (c *tcgCATransportClient) SignTCGCSR(ctx context.Context, req *xkmstransport.SignTCGCSRRequest) (*xkmstransport.SignTCGCSRResponse, error) {
	return tcgCARequest[xkmstransport.SignTCGCSRResponse](ctx, c, "SignTCGCSR", req, req == nil)
}

// EnrollDevice runs TCG device enrollment and returns the certificates with
// the credential-activation challenge.
func (c *tcgCATransportClient) EnrollDevice(ctx context.Context, req *xkmstransport.EnrollDeviceRequest) (*xkmstransport.EnrollDeviceResponse, error) {
	return tcgCARequest[xkmstransport.EnrollDeviceResponse](ctx, c, "EnrollDevice", req, req == nil)
}
