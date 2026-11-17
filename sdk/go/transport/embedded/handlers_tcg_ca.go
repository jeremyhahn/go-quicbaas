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

package embedded

import (
	"context"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// tcgCACall serves one quicbaas.TCGCA verb (Security().TCGCA()) from the
// wired go-xkms CA adapter, which also implements go-xkms's TCGCAService
// over the CA's TCG issuer. A CA without TCG support is not configured.
func tcgCACall[Req any](svc service.QuicBaaS, method string, req interface{},
	call func(xkmstransport.TCGCAService, *Req) (interface{}, error)) (interface{}, error) {
	tcg, ok := svc.TrustedCA().(xkmstransport.TCGCAService)
	if !ok || tcg == nil {
		return nil, NewServiceNotConfiguredError("TCGCA")
	}
	r, ok := req.(*Req)
	if !ok || r == nil {
		var zero *Req
		return nil, NewInvalidRequestTypeError("quicbaas.TCGCA", method, typeNameOf(zero), typeNameOf(req))
	}
	return call(tcg, r)
}

func handleTCGCAIssueEKCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return tcgCACall(svc, "IssueEKCertificate", req, func(ca xkmstransport.TCGCAService, r *xkmstransport.IssueEKCertificateRequest) (interface{}, error) {
		return ca.IssueEKCertificate(ctx, r)
	})
}

func handleTCGCAIssueAKCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return tcgCACall(svc, "IssueAKCertificate", req, func(ca xkmstransport.TCGCAService, r *xkmstransport.IssueAKCertificateRequest) (interface{}, error) {
		return ca.IssueAKCertificate(ctx, r)
	})
}

func handleTCGCASignTCGCSR(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return tcgCACall(svc, "SignTCGCSR", req, func(ca xkmstransport.TCGCAService, r *xkmstransport.SignTCGCSRRequest) (interface{}, error) {
		return ca.SignTCGCSR(ctx, r)
	})
}

func handleTCGCAEnrollDevice(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return tcgCACall(svc, "EnrollDevice", req, func(ca xkmstransport.TCGCAService, r *xkmstransport.EnrollDeviceRequest) (interface{}, error) {
		return ca.EnrollDevice(ctx, r)
	})
}
