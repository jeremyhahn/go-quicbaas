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

// The re-exported go-xkms CAService (Security().TrustedCA()) on the embedded
// transport: each handler hands go-xkms's own request to the CA the server
// composes (service.QuicBaaS.TrustedCA, the adapter the REST and MCP
// transports serve) and returns go-xkms's own response (#1330).

// trustedCACall type-checks the go-xkms request and calls one CA verb.
func trustedCACall[Req any](ctx context.Context, svc service.QuicBaaS, method string, req interface{},
	call func(service.TrustedCAService, *Req) (interface{}, error)) (interface{}, error) {
	ca := svc.TrustedCA()
	if ca == nil {
		return nil, NewServiceNotConfiguredError("TrustedCA")
	}
	r, ok := req.(*Req)
	if !ok || r == nil {
		var zero *Req
		return nil, NewInvalidRequestTypeError("quicbaas.TrustedCA", method, typeNameOf(zero), typeNameOf(req))
	}
	return call(ca, r)
}

func handleTrustedCAGetCABundle(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "GetCABundle", req, func(ca service.TrustedCAService, r *xkmstransport.GetCABundleRequest) (interface{}, error) {
		return ca.GetCABundle(ctx, r)
	})
}

func handleTrustedCAGetCACertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "GetCACertificate", req, func(ca service.TrustedCAService, r *xkmstransport.GetCACertificateRequest) (interface{}, error) {
		return ca.GetCACertificate(ctx, r)
	})
}

func handleTrustedCASignCSR(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "SignCSR", req, func(ca service.TrustedCAService, r *xkmstransport.SignCSRRequest) (interface{}, error) {
		return ca.SignCSR(ctx, r)
	})
}

func handleTrustedCAIssueCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "IssueCertificate", req, func(ca service.TrustedCAService, r *xkmstransport.IssueCertificateRequest) (interface{}, error) {
		return ca.IssueCertificate(ctx, r)
	})
}

func handleTrustedCARevokeCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "RevokeCertificate", req, func(ca service.TrustedCAService, r *xkmstransport.RevokeCertificateRequest) (interface{}, error) {
		return ca.RevokeCertificate(ctx, r)
	})
}

func handleTrustedCAGenerateCRL(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "GenerateCRL", req, func(ca service.TrustedCAService, r *xkmstransport.GenerateCRLRequest) (interface{}, error) {
		return ca.GenerateCRL(ctx, r)
	})
}

func handleTrustedCAIsRevoked(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return trustedCACall(ctx, svc, "IsRevoked", req, func(ca service.TrustedCAService, r *xkmstransport.IsRevokedRequest) (interface{}, error) {
		return ca.IsRevoked(ctx, r)
	})
}
