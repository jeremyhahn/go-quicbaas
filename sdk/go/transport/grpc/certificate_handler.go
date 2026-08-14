// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// certificateServicePrefix is the gRPC-style service identifier the SDK
// certificate client routes through. It matches the const used by the
// REST and (forthcoming) QUIC/MCP transports.
const certificateServicePrefix = "quicbaas.Certificate"

// isCertificateService reports whether the service name targets the
// quicbaas.Certificate surface so doRequest can branch into the typed
// certificate handler. The SDK certificate client uses plain Go structs
// that do not implement proto.Message, so the bare conn.Invoke path
// cannot serialize them — they must be converted to the generated proto
// types first.
func isCertificateService(svc string) bool {
	return strings.HasPrefix(svc, certificateServicePrefix)
}

// certificateHandler converts between the SDK certificate client structs
// and the generated CertificateService proto types, then calls the gRPC
// CertificateService registered by the server lifecycle.
type certificateHandler struct {
	conn *grpc.ClientConn
}

// newCertificateHandler creates a new certificate handler bound to conn.
func newCertificateHandler(conn *grpc.ClientConn) *certificateHandler {
	return &certificateHandler{conn: conn}
}

// certificateHandlerMap maps method names to handler functions for O(1)
// dispatch. Only the methods exposed by the CertificateService proto are
// wired here; CA-administration methods that have no proto analogue
// (CAInfo, GetCABundle, ListCertificates, IsRevoked, IsInitialized,
// GenerateCRL) return a clear unimplemented error rather than silently
// falling through to the untyped conn.Invoke path.
var certificateHandlerMap = map[string]func(h *certificateHandler, ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error){
	"IssueCertificate":  (*certificateHandler).handleIssueCertificate,
	"SignCSR":           (*certificateHandler).handleSignCSR,
	"RevokeCertificate": (*certificateHandler).handleRevokeCertificate,
	"VerifyCertificate": (*certificateHandler).handleVerifyCertificate,
	"GetCACertificate":  (*certificateHandler).handleGetCACertificate,
	"GetTLSConfig":      (*certificateHandler).handleGetTLSConfig,

	// Certificate extensions — Security().CertificateExt(). See
	// certificate_extensions_handler.go.
	"CrossSign":           (*certificateHandler).handleCrossSign,
	"Chain":               (*certificateHandler).handleChain,
	"IssueDeviceIdentity": (*certificateHandler).handleIssueDeviceIdentity,
}

// handleRequest routes Certificate requests to the appropriate handler
// based on method name.
func (h *certificateHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	client := pb.NewCertificateServiceClient(h.conn)

	handler, ok := certificateHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("certificateHandler.handleRequest",
			"method not exposed by CertificateService gRPC proto: "+method+
				" (use the REST transport for CA-administration verbs)")
	}

	return handler(h, ctx, client, req)
}

// handleIssueCertificate converts the SDK issue request to proto, calls
// the gRPC CertificateService, and converts the proto response back to the
// SDK response type the certificate client expects.
func (h *certificateHandler) handleIssueCertificate(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.IssueCertificateRequest)
	if !ok {
		return nil, errors.NewInvalidInput("certificateHandler.handleIssueCertificate",
			"invalid request type, expected *certificate.IssueCertificateRequest")
	}

	pbReq := &pb.IssueCertificateRequest{
		CommonName:         sdkReq.CommonName,
		Organization:       sdkReq.Organization,
		OrganizationalUnit: sdkReq.OrganizationalUnit,
		Country:            sdkReq.Country,
		Province:           sdkReq.Province,
		Locality:           sdkReq.Locality,
		DnsNames:           sdkReq.DNSNames,
		IpAddresses:        sdkReq.IPAddresses,
		EmailAddresses:     sdkReq.EmailAddresses,
		Uris:               sdkReq.URIs,
		ValidityDays:       int32(sdkReq.ValidityDays),
		Usage:              certUsageToProto(sdkReq.Usage),
		IsCa:               sdkReq.IsCA,
		MaxPathLen:         int32(sdkReq.MaxPathLen),
		KeyAlgorithm:       certKeyAlgToProto(sdkReq.KeyAlgorithm),
		KeySize:            int32(sdkReq.KeySize),
		Metadata:           sdkReq.Metadata,
	}

	pbResp, err := client.IssueCertificate(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleIssueCertificate")
	}

	return &certificate.IssueCertificateResponse{
		CertificatePEM:   pbResp.CertificatePem,
		PrivateKeyPEM:    pbResp.PrivateKeyPem,
		CACertificatePEM: pbResp.CaCertificatePem,
		ChainPEM:         pbResp.ChainPem,
		SerialNumber:     pbResp.SerialNumber,
		NotBefore:        pbResp.NotBefore.AsTime(),
		NotAfter:         pbResp.NotAfter.AsTime(),
	}, nil
}

// handleSignCSR converts the SDK sign-CSR request to proto, calls the
// gRPC CertificateService, and converts the proto response back.
func (h *certificateHandler) handleSignCSR(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.SignCSRRequest)
	if !ok {
		return nil, errors.NewInvalidInput("certificateHandler.handleSignCSR",
			"invalid request type, expected *certificate.SignCSRRequest")
	}

	pbReq := &pb.SignCSRRequest{
		CsrPem:       sdkReq.CSRPEM,
		ValidityDays: int32(sdkReq.ValidityDays),
		Usage:        certUsageToProto(sdkReq.Usage),
	}

	pbResp, err := client.SignCSR(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleSignCSR")
	}

	return &certificate.SignCSRResponse{
		CertificatePEM:   pbResp.CertificatePem,
		CACertificatePEM: pbResp.CaCertificatePem,
		ChainPEM:         pbResp.ChainPem,
		SerialNumber:     pbResp.SerialNumber,
		NotBefore:        pbResp.NotBefore.AsTime(),
		NotAfter:         pbResp.NotAfter.AsTime(),
	}, nil
}

// handleRevokeCertificate converts the SDK revoke request to proto, calls
// the gRPC CertificateService, and converts the proto response back.
func (h *certificateHandler) handleRevokeCertificate(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.RevokeCertificateRequest)
	if !ok {
		return nil, errors.NewInvalidInput("certificateHandler.handleRevokeCertificate",
			"invalid request type, expected *certificate.RevokeCertificateRequest")
	}

	pbReq := &pb.RevokeCertificateRequest{
		SerialNumber: sdkReq.SerialNumber,
	}

	pbResp, err := client.RevokeCertificate(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleRevokeCertificate")
	}

	return &certificate.RevokeCertificateResponse{
		Success: pbResp.Success,
	}, nil
}

// handleVerifyCertificate converts the SDK verify request to proto, calls
// the gRPC CertificateService, and converts the proto response back.
func (h *certificateHandler) handleVerifyCertificate(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.VerifyCertificateRequest)
	if !ok {
		return nil, errors.NewInvalidInput("certificateHandler.handleVerifyCertificate",
			"invalid request type, expected *certificate.VerifyCertificateRequest")
	}

	pbReq := &pb.VerifyCertificateRequest{
		CertificatePem: sdkReq.CertificatePEM,
	}

	pbResp, err := client.VerifyCertificate(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleVerifyCertificate")
	}

	return &certificate.VerifyCertificateResponse{
		Valid:        pbResp.Valid,
		ErrorMessage: pbResp.ErrorMessage,
	}, nil
}

// handleGetCACertificate calls the gRPC CertificateService and converts
// the proto response to the SDK GetCACertificateResponse.
func (h *certificateHandler) handleGetCACertificate(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	// The SDK request carries no fields; tolerate either the typed empty
	// request or a nil request.
	if req != nil {
		if _, ok := req.(*certificate.GetCACertificateRequest); !ok {
			return nil, errors.NewInvalidInput("certificateHandler.handleGetCACertificate",
				"invalid request type, expected *certificate.GetCACertificateRequest")
		}
	}

	pbResp, err := client.GetCACertificate(ctx, &pb.GetCACertificateRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleGetCACertificate")
	}

	return &certificate.GetCACertificateResponse{
		CACertificatePEM: pbResp.CaCertificatePem,
		CABundlePEM:      pbResp.CaBundlePem,
		Identity:         pbResp.Identity,
	}, nil
}

// handleGetTLSConfig calls the gRPC CertificateService GetTLSConfig RPC.
// The tls.Config is constructed client-side from the configuration; the
// gRPC response carries only the echoed configuration parameters, so the
// SDK response Config remains nil here (matching the server contract that
// does not serialize a tls.Config over the wire).
func (h *certificateHandler) handleGetTLSConfig(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.GetTLSConfigRequest)
	if !ok {
		return nil, errors.NewInvalidInput("certificateHandler.handleGetTLSConfig",
			"invalid request type, expected *certificate.GetTLSConfigRequest")
	}

	pbReq := &pb.GetTLSConfigRequest{
		ServerName:        sdkReq.ServerName,
		RequireClientCert: sdkReq.RequireClientCert,
		IsServer:          sdkReq.IsServer,
		MinVersion:        tlsVersionToProto(sdkReq.MinVersion),
	}

	if _, err := client.GetTLSConfig(ctx, pbReq); err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleGetTLSConfig")
	}

	return &certificate.GetTLSConfigResponse{}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// certUsageToProto converts the SDK usage string to the proto enum.
func certUsageToProto(usage string) pb.CertificateUsage {
	switch usage {
	case "client":
		return pb.CertificateUsage_CERTIFICATE_USAGE_CLIENT
	case "server":
		return pb.CertificateUsage_CERTIFICATE_USAGE_SERVER
	case "both":
		return pb.CertificateUsage_CERTIFICATE_USAGE_BOTH
	default:
		return pb.CertificateUsage_CERTIFICATE_USAGE_UNSPECIFIED
	}
}

// certKeyAlgToProto converts the SDK key-algorithm string to the proto enum.
func certKeyAlgToProto(alg string) pb.CertKeyAlgorithm {
	switch alg {
	case "ecdsa":
		return pb.CertKeyAlgorithm_CERT_KEY_ALG_ECDSA
	case "rsa":
		return pb.CertKeyAlgorithm_CERT_KEY_ALG_RSA
	default:
		return pb.CertKeyAlgorithm_CERT_KEY_ALG_UNSPECIFIED
	}
}

// tlsVersionToProto converts the SDK TLS-version string to the proto enum.
func tlsVersionToProto(version string) pb.TLSVersion {
	switch version {
	case "1.2":
		return pb.TLSVersion_TLS_VERSION_1_2
	case "1.3":
		return pb.TLSVersion_TLS_VERSION_1_3
	default:
		return pb.TLSVersion_TLS_VERSION_UNSPECIFIED
	}
}
