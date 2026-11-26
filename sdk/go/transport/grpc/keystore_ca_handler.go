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

	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const tcgCAServicePrefix = "quicbaas.TCGCA"

// isTCGCAService reports whether svc names the re-exported go-xkms
// TCGCAService (Security().TCGCA()).
func isTCGCAService(svc string) bool {
	return strings.HasPrefix(svc, tcgCAServicePrefix) || svc == "TCGCA"
}

// onUnixSocket reports whether cfg addresses the node's Unix socket listener.
func onUnixSocket(cfg *transport.Config) bool {
	if cfg == nil {
		return false
	}
	if cfg.AddressType == transport.AddressTypeUnix || cfg.AddressType == transport.AddressTypeUnixAbstract {
		return true
	}
	return cfg.Address != "" && transport.ParseAddress(cfg.Address).IsUnix()
}

// keystoreCAHandler converts between the go-xkms CA and TCG CA wire structs
// and go-xkms's xkms.v1.KeystoreService messages. The node's TCP gRPC
// listener serves the go-xkms CA and TCG CA verbs only on
// xkms.v1.KeystoreService; it does not mount TrustedCAService.
type keystoreCAHandler struct {
	conn *grpc.ClientConn
}

func newKeystoreCAHandler(conn *grpc.ClientConn) *keystoreCAHandler {
	return &keystoreCAHandler{conn: conn}
}

type keystoreCAMethodHandler func(context.Context, xkmsv1.KeystoreServiceClient, interface{}) (interface{}, error)

// keystoreCAMethods maps the go-xkms CAService methods onto their handlers.
var keystoreCAMethods = map[string]keystoreCAMethodHandler{
	"GetCABundle":       keystoreCAGetCABundle,
	"GetCACertificate":  keystoreCAGetCACertificate,
	"SignCSR":           keystoreCASignCSR,
	"IssueCertificate":  keystoreCAIssueCertificate,
	"RevokeCertificate": keystoreCARevokeCertificate,
	"GenerateCRL":       keystoreCAGenerateCRL,
	"IsRevoked":         keystoreCAIsRevoked,
}

// keystoreTCGCAMethods maps the go-xkms TCGCAService methods onto their
// handlers.
var keystoreTCGCAMethods = map[string]keystoreCAMethodHandler{
	"IssueEKCertificate": keystoreCAIssueEKCertificate,
	"IssueAKCertificate": keystoreCAIssueAKCertificate,
	"SignTCGCSR":         keystoreCASignTCGCSR,
	"EnrollDevice":       keystoreCAEnrollDevice,
}

func (h *keystoreCAHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	methods := keystoreCAMethods
	if isTCGCAService(service) {
		methods = keystoreTCGCAMethods
	}
	fn, ok := methods[method]
	if !ok {
		return nil, errors.NewUnimplemented("keystoreCAHandler.handleRequest",
			"method not exposed by xkms.v1.KeystoreService: "+method)
	}
	return fn(ctx, xkmsv1.NewKeystoreServiceClient(h.conn), req)
}

// keystoreCARefuseTenant refuses a request that names a tenant.
// KeystoreService has no tenant_id field, so the field is refused, never
// dropped: dropping it would silently re-scope the request to the caller's
// own tenant.
func keystoreCARefuseTenant(method, tenantID string) error {
	if tenantID == "" {
		return nil
	}
	return errors.NewInvalidInput("keystoreCAHandler."+method,
		"xkms.v1.KeystoreService carries no tenant_id; the node serves the caller's own tenant over gRPC (TCP): leave TenantID empty")
}

func keystoreCAGetCABundle(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GetCABundleRequest]("GetCABundle", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("GetCABundle", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.GetCABundle(ctx, &xkmsv1.GetCABundleRequest{StoreType: r.StoreType, Algorithm: r.Algorithm})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.GetCABundle")
	}
	return &xkmstransport.GetCABundleResponse{BundlePEM: resp.BundlePem, Certificates: resp.Certificates, ContentType: resp.ContentType}, nil
}

func keystoreCAGetCACertificate(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GetCACertificateRequest]("GetCACertificate", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("GetCACertificate", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.GetCACertificate(ctx, &xkmsv1.GetCACertificateRequest{Identity: r.Identity})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.GetCACertificate")
	}
	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: resp.CertificatePem, Subject: resp.Subject, Issuer: resp.Issuer,
		SerialNumber: resp.SerialNumber, NotBefore: resp.NotBefore, NotAfter: resp.NotAfter, IsCA: resp.IsCa,
	}, nil
}

func keystoreCASignCSR(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.SignCSRRequest]("SignCSR", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("SignCSR", r.TenantID); err != nil {
		return nil, err
	}
	// KeystoreService has no attestation fields: refuse the evidence rather
	// than drop it, which would sign the CSR without attestation.
	if r.AttestationFormat != "" || len(r.AttestationChain) > 0 || len(r.AttestationData) > 0 ||
		len(r.AttestationSig) > 0 || len(r.AttestationNonce) > 0 {
		return nil, errors.NewInvalidInput("keystoreCAHandler.SignCSR",
			"xkms.v1.KeystoreService carries no attestation evidence; send SignCSR without attestation fields over gRPC (TCP)")
	}
	resp, err := c.SignCSR(ctx, &xkmsv1.SignCSRRequest{CsrPem: r.CSRPEM, Profile: r.Profile, ValidityDays: int32(r.ValidityDays)})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.SignCSR")
	}
	return &xkmstransport.SignCSRResponse{CertificatePEM: resp.CertificatePem, ChainPEM: resp.ChainPem, SerialNumber: resp.SerialNumber}, nil
}

func keystoreCAIssueCertificate(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IssueCertificateRequest]("IssueCertificate", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("IssueCertificate", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.IssueCertificate(ctx, &xkmsv1.IssueCertificateRequest{
		Profile: r.Profile, CommonName: r.CommonName, Organization: r.Organization, Sans: r.SANs,
		ValidityDays: int32(r.ValidityDays), Algorithm: r.Algorithm,
	})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.IssueCertificate")
	}
	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: resp.CertificatePem, ChainPEM: resp.ChainPem,
		PrivateKeyPEM: resp.PrivateKeyPem, SerialNumber: resp.SerialNumber,
	}, nil
}

func keystoreCARevokeCertificate(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.RevokeCertificateRequest]("RevokeCertificate", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("RevokeCertificate", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.RevokeCertificate(ctx, &xkmsv1.RevokeCertificateRequest{SerialNumber: r.SerialNumber, Reason: int32(r.Reason)})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.RevokeCertificate")
	}
	return &xkmstransport.RevokeCertificateResponse{Success: resp.Success, Message: resp.Message}, nil
}

func keystoreCAGenerateCRL(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.GenerateCRLRequest]("GenerateCRL", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("GenerateCRL", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.GenerateCRL(ctx, &xkmsv1.GenerateCRLRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.GenerateCRL")
	}
	return &xkmstransport.GenerateCRLResponse{CRLPEM: resp.CrlPem}, nil
}

func keystoreCAIsRevoked(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IsRevokedRequest]("IsRevoked", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("IsRevoked", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.IsRevoked(ctx, &xkmsv1.IsRevokedRequest{SerialNumber: r.SerialNumber})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.IsRevoked")
	}
	return &xkmstransport.IsRevokedResponse{Revoked: resp.Revoked, Reason: int(resp.Reason), Message: resp.Message}, nil
}

func keystoreCAIssueEKCertificate(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IssueEKCertificateRequest]("IssueEKCertificate", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("IssueEKCertificate", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.IssueEKCertificate(ctx, &xkmsv1.IssueEKCertificateRequest{
		CommonName: r.CommonName, Organization: r.Organization, EkPublicKey: r.EKPublicKey,
	})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.IssueEKCertificate")
	}
	return &xkmstransport.IssueEKCertificateResponse{
		CertificateDER: resp.CertificateDer, CertificatePEM: resp.CertificatePem, SerialNumber: resp.SerialNumber,
	}, nil
}

func keystoreCAIssueAKCertificate(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.IssueAKCertificateRequest]("IssueAKCertificate", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("IssueAKCertificate", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.IssueAKCertificate(ctx, &xkmsv1.IssueAKCertificateRequest{
		CommonName: r.CommonName, Organization: r.Organization, PublicKey: r.PublicKey,
	})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.IssueAKCertificate")
	}
	return &xkmstransport.IssueAKCertificateResponse{
		CertificateDER: resp.CertificateDer, CertificatePEM: resp.CertificatePem, SerialNumber: resp.SerialNumber,
	}, nil
}

func keystoreCASignTCGCSR(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.SignTCGCSRRequest]("SignTCGCSR", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("SignTCGCSR", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.SignTCGCSR(ctx, &xkmsv1.SignTCGCSRRequest{
		CommonName: r.CommonName, Organization: r.Organization, TcgCsr: r.TCGCSR,
	})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.SignTCGCSR")
	}
	return &xkmstransport.SignTCGCSRResponse{IAKCertDER: resp.IakCertDer, IDevIDCertDER: resp.IdevidCertDer}, nil
}

func keystoreCAEnrollDevice(ctx context.Context, c xkmsv1.KeystoreServiceClient, req interface{}) (interface{}, error) {
	r, err := trustedCAReq[xkmstransport.EnrollDeviceRequest]("EnrollDevice", req)
	if err != nil {
		return nil, err
	}
	if err := keystoreCARefuseTenant("EnrollDevice", r.TenantID); err != nil {
		return nil, err
	}
	resp, err := c.EnrollDevice(ctx, &xkmsv1.EnrollDeviceRequest{
		CommonName: r.CommonName, Organization: r.Organization, PackedCsr: r.PackedCSR,
	})
	if err != nil {
		return nil, convertGRPCError(err, "keystoreCAHandler.EnrollDevice")
	}
	return &xkmstransport.EnrollDeviceResponse{
		IAKCertDER: resp.IakCertDer, IDevIDCertDER: resp.IdevidCertDer,
		CredentialBlob: resp.CredentialBlob, EncryptedSecret: resp.EncryptedSecret, PlainSecret: resp.PlainSecret,
	}, nil
}
