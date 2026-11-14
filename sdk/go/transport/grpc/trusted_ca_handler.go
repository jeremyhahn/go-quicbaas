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
	"strings"

	"google.golang.org/grpc"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// trustedCAServicePrefix is the service identifier the re-exported go-xkms
// CAService (Security().TrustedCA()) routes through on every transport.
const trustedCAServicePrefix = "quicbaas.TrustedCA"

// isTrustedCAService reports whether svc names the re-exported go-xkms CA.
// Its go-xkms wire structs are not proto.Message values, so they are
// converted to the generated TrustedCAService proto types first (#1330).
func isTrustedCAService(svc string) bool {
	return strings.HasPrefix(svc, trustedCAServicePrefix) || svc == "TrustedCA"
}

// trustedCAHandler converts between the go-xkms CA wire structs and the
// generated TrustedCAService proto types, then calls the server's
// TrustedCAService. The Unix transport delegates to it verbatim.
type trustedCAHandler struct {
	conn *grpc.ClientConn
}

func newTrustedCAHandler(conn *grpc.ClientConn) *trustedCAHandler {
	return &trustedCAHandler{conn: conn}
}

type trustedCAMethodHandler func(context.Context, pb.TrustedCAServiceClient, interface{}) (interface{}, error)

// trustedCAMethods maps the go-xkms CAService methods onto their handlers.
var trustedCAMethods = map[string]trustedCAMethodHandler{
	"GetCABundle":       trustedCAGetCABundle,
	"GetCACertificate":  trustedCAGetCACertificate,
	"SignCSR":           trustedCASignCSR,
	"IssueCertificate":  trustedCAIssueCertificate,
	"RevokeCertificate": trustedCARevokeCertificate,
	"GenerateCRL":       trustedCAGenerateCRL,
	"IsRevoked":         trustedCAIsRevoked,
}

func (h *trustedCAHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	fn, ok := trustedCAMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("trustedCAHandler.handleRequest",
			"method not exposed by TrustedCAService gRPC proto: "+method)
	}
	return fn(ctx, pb.NewTrustedCAServiceClient(h.conn), req)
}

// trustedCAReq type-checks the go-xkms request of one method.
func trustedCAReq[Req any](method string, req interface{}) (*Req, error) {
	r, ok := req.(*Req)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("trustedCAHandler."+method,
			"invalid request type, expected the go-xkms "+method+" request")
	}
	return r, nil
}

func trustedCAGetCABundle(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GetCABundleRequest]("GetCABundle", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.GetCABundle(ctx, &pb.TrustedCAGetCABundleRequest{StoreType: r.StoreType, Algorithm: r.Algorithm, TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.GetCABundle")
	}
	return &xkmstransport.GetCABundleResponse{BundlePEM: resp.BundlePem, Certificates: resp.Certificates, ContentType: resp.ContentType}, nil
}

func trustedCAGetCACertificate(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GetCACertificateRequest]("GetCACertificate", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.GetCACertificate(ctx, &pb.TrustedCAGetCACertificateRequest{Identity: r.Identity, TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.GetCACertificate")
	}
	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: resp.CertificatePem, Subject: resp.Subject, Issuer: resp.Issuer,
		SerialNumber: resp.SerialNumber, NotBefore: resp.NotBefore, NotAfter: resp.NotAfter, IsCA: resp.IsCa,
	}, nil
}

func trustedCASignCSR(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.SignCSRRequest]("SignCSR", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.SignCSR(ctx, &pb.TrustedCASignCSRRequest{
		CsrPem: r.CSRPEM, Profile: r.Profile, ValidityDays: int32(r.ValidityDays), TenantId: r.TenantID,
		AttestationFormat: r.AttestationFormat, AttestationChain: r.AttestationChain,
		AttestationData: r.AttestationData, AttestationSig: r.AttestationSig, AttestationNonce: r.AttestationNonce,
	})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.SignCSR")
	}
	return &xkmstransport.SignCSRResponse{CertificatePEM: resp.CertificatePem, ChainPEM: resp.ChainPem, SerialNumber: resp.SerialNumber}, nil
}

func trustedCAIssueCertificate(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IssueCertificateRequest]("IssueCertificate", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.IssueCertificate(ctx, &pb.TrustedCAIssueCertificateRequest{
		Profile: r.Profile, CommonName: r.CommonName, Organization: r.Organization, Sans: r.SANs,
		ValidityDays: int32(r.ValidityDays), Algorithm: r.Algorithm, TenantId: r.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.IssueCertificate")
	}
	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: resp.CertificatePem, ChainPEM: resp.ChainPem,
		PrivateKeyPEM: resp.PrivateKeyPem, SerialNumber: resp.SerialNumber,
	}, nil
}

func trustedCARevokeCertificate(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.RevokeCertificateRequest]("RevokeCertificate", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.RevokeCertificate(ctx, &pb.TrustedCARevokeCertificateRequest{SerialNumber: r.SerialNumber, Reason: int32(r.Reason), TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.RevokeCertificate")
	}
	return &xkmstransport.RevokeCertificateResponse{Success: resp.Success, Message: resp.Message}, nil
}

func trustedCAGenerateCRL(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GenerateCRLRequest]("GenerateCRL", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.GenerateCRL(ctx, &pb.TrustedCAGenerateCRLRequest{TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.GenerateCRL")
	}
	return &xkmstransport.GenerateCRLResponse{CRLPEM: resp.CrlPem}, nil
}

func trustedCAIsRevoked(ctx context.Context, c pb.TrustedCAServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IsRevokedRequest]("IsRevoked", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.IsRevoked(ctx, &pb.TrustedCAIsRevokedRequest{SerialNumber: r.SerialNumber, TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "trustedCAHandler.IsRevoked")
	}
	return &xkmstransport.IsRevokedResponse{Revoked: resp.Revoked, Reason: int(resp.Reason), Message: resp.Message}, nil
}
