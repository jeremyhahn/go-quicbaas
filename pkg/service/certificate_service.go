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

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// CertificateService provides certificate operations through the service layer.
// It delegates to a go-xkms CAService via the transport.Client interface.
type CertificateService interface {
	IssueCertificate(ctx context.Context, req *IssueCertificateRequest) (*IssueCertificateResponse, error)
	SignCSR(ctx context.Context, req *SignCSRRequest) (*SignCSRResponse, error)
	RevokeCertificate(ctx context.Context, req *RevokeCertificateRequest) (*RevokeCertificateResponse, error)
	VerifyCertificate(ctx context.Context, req *VerifyCertificateRequest) (*VerifyCertificateResponse, error)
	GetCACertificate(ctx context.Context, req *GetCACertificateRequest) (*GetCACertificateResponse, error)
	GetTLSConfig(ctx context.Context, req *GetTLSConfigRequest) (*GetTLSConfigResponse, error)
	IsInitialized() bool
	CAService() xkmstransport.Client
	GetCABundle(ctx context.Context, req *GetCABundleRequest) (*GetCABundleResponse, error)
	GenerateCRL(ctx context.Context, req *GenerateCRLRequest) (*GenerateCRLResponse, error)
	IsRevoked(ctx context.Context, req *IsRevokedRequest) (*IsRevokedResponse, error)

	// CAInfo returns parsed metadata describing the active issuing CA:
	// subject/issuer/serial, validity window, key type/size, signature
	// algorithm, plus the CA leaf PEM and the full CA bundle PEM.
	CAInfo(ctx context.Context, req *CAInfoRequest) (*CAInfoResponse, error)

	// ListCertificates enumerates certificates stored in the CA's
	// CertStore, with optional filters for Common Name, revocation
	// status, and expiry.
	ListCertificates(ctx context.Context, req *ListCertificatesRequest) (*ListCertificatesResponse, error)

	// CrossSign cross-signs an external root certificate with the local
	// CA. See certificate_extensions.go.
	CrossSign(ctx context.Context, req *CrossSignRequest) (*CrossSignResponse, error)

	// Chain resolves the issuer chain for a certificate by walking its
	// Authority Information Access extension. See certificate_extensions.go.
	Chain(ctx context.Context, req *CertificateChainRequest) (*CertificateChainResponse, error)

	// IssueDeviceIdentity issues a TCG device identity certificate
	// (EK / AK / IDevID). See certificate_extensions.go.
	IssueDeviceIdentity(ctx context.Context, req *IssueDeviceIdentityRequest) (*IssueDeviceIdentityResponse, error)
}

// CertificateService Request/Response types

// IssueCertificateRequest contains parameters for certificate issuance.
type IssueCertificateRequest struct {
	CommonName         string            `json:"common_name"`
	Organization       string            `json:"organization,omitempty"`
	OrganizationalUnit string            `json:"organizational_unit,omitempty"`
	Country            string            `json:"country,omitempty"`
	Province           string            `json:"province,omitempty"`
	Locality           string            `json:"locality,omitempty"`
	DNSNames           []string          `json:"dns_names,omitempty"`
	IPAddresses        []string          `json:"ip_addresses,omitempty"`
	EmailAddresses     []string          `json:"email_addresses,omitempty"`
	URIs               []string          `json:"uris,omitempty"`
	ValidityDays       int               `json:"validity_days,omitempty"`
	Usage              string            `json:"usage,omitempty"` // "client", "server", "both"
	IsCA               bool              `json:"is_ca,omitempty"`
	MaxPathLen         int               `json:"max_path_len,omitempty"`
	KeyAlgorithm       string            `json:"key_algorithm,omitempty"` // "ecdsa", "rsa"
	KeySize            int               `json:"key_size,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// IssueCertificateResponse contains the issued certificate.
type IssueCertificateResponse struct {
	CertificatePEM   string    `json:"certificate_pem"`
	PrivateKeyPEM    string    `json:"private_key_pem"`
	CACertificatePEM string    `json:"ca_certificate_pem"`
	ChainPEM         string    `json:"chain_pem"`
	SerialNumber     string    `json:"serial_number"`
	NotBefore        time.Time `json:"not_before"`
	NotAfter         time.Time `json:"not_after"`
}

// SignCSRRequest contains a CSR to sign.
type SignCSRRequest struct {
	CSRPEM       string `json:"csr_pem"`
	ValidityDays int    `json:"validity_days,omitempty"`
	Usage        string `json:"usage,omitempty"` // "client", "server", "both"
}

// SignCSRResponse contains the signed certificate.
type SignCSRResponse struct {
	CertificatePEM   string    `json:"certificate_pem"`
	CACertificatePEM string    `json:"ca_certificate_pem"`
	ChainPEM         string    `json:"chain_pem"`
	SerialNumber     string    `json:"serial_number"`
	NotBefore        time.Time `json:"not_before"`
	NotAfter         time.Time `json:"not_after"`
}

// RevokeCertificateRequest contains the certificate to revoke.
type RevokeCertificateRequest struct {
	SerialNumber string `json:"serial_number"`
}

// RevokeCertificateResponse contains the revocation result.
type RevokeCertificateResponse struct {
	Success bool `json:"success"`
}

// VerifyCertificateRequest contains the certificate to verify.
type VerifyCertificateRequest struct {
	CertificatePEM string `json:"certificate_pem"`
}

// VerifyCertificateResponse contains the verification result.
type VerifyCertificateResponse struct {
	Valid        bool   `json:"valid"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// GetCACertificateRequest is the request for CA certificate.
type GetCACertificateRequest struct{}

// GetCACertificateResponse contains the CA certificate.
type GetCACertificateResponse struct {
	CACertificatePEM string `json:"ca_certificate_pem"`
	CABundlePEM      string `json:"ca_bundle_pem"`
	Identity         string `json:"identity"`
}

// GetTLSConfigRequest contains parameters for TLS configuration.
type GetTLSConfigRequest struct {
	ServerName        string `json:"server_name,omitempty"`
	RequireClientCert bool   `json:"require_client_cert,omitempty"`
	IsServer          bool   `json:"is_server,omitempty"`
	MinVersion        string `json:"min_version,omitempty"` // "1.2", "1.3"
}

// GetTLSConfigResponse contains the TLS configuration.
type GetTLSConfigResponse struct {
	Config *tls.Config `json:"-"` // Not serializable
}

// GetCABundleRequest contains parameters for retrieving a CA bundle.
type GetCABundleRequest struct {
	StoreType string `json:"store_type,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
}

// GetCABundleResponse contains the CA certificate bundle.
type GetCABundleResponse struct {
	BundlePEM string `json:"bundle_pem"`
}

// GenerateCRLRequest contains parameters for generating a CRL.
type GenerateCRLRequest struct{}

// GenerateCRLResponse contains the generated CRL.
type GenerateCRLResponse struct {
	CRLPEM string `json:"crl_pem"`
}

// IsRevokedRequest checks whether a certificate serial number is revoked.
type IsRevokedRequest struct {
	SerialNumber string `json:"serial_number"`
}

// IsRevokedResponse contains the revocation check result.
type IsRevokedResponse struct {
	Revoked bool   `json:"revoked"`
	Reason  int    `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// Typed sentinel errors for certificate service operations.
var (
	ErrCertNilRequest        = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertNilRequest}
	ErrCertMissingCommonName = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertMissingCommonName}
	ErrCertMissingCSR        = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertMissingCSR}
	ErrCertMissingSerial     = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertMissingSerial}
	ErrCertMissingPEM        = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertMissingPEM}
	ErrCertInvalidPEM        = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertInvalidPEM}
	ErrCertParseFailed       = &errors.QBError{Code: errors.ErrInvalidInput, Op: "CertificateService", Err: errCertParseFailed}
)

var (
	errCertNilRequest        = stderrors.New("request must not be nil")
	errCertMissingCommonName = stderrors.New("common name is required")
	errCertMissingCSR        = stderrors.New("CSR PEM is required")
	errCertMissingSerial     = stderrors.New("serial number is required")
	errCertMissingPEM        = stderrors.New("certificate PEM is required")
	errCertInvalidPEM        = stderrors.New("invalid PEM format")
	errCertParseFailed       = stderrors.New("failed to parse certificate")
)

type certificateService struct {
	caService xkmstransport.Client
}

// NewCertificateService creates a new CertificateService backed by an xkms CAService.
func NewCertificateService(caService xkmstransport.Client) CertificateService {
	if caService == nil {
		return nil
	}
	return &certificateService{caService: caService}
}

func (s *certificateService) IssueCertificate(ctx context.Context, req *IssueCertificateRequest) (*IssueCertificateResponse, error) {
	if req == nil {
		return nil, ErrCertNilRequest
	}
	if req.CommonName == "" {
		return nil, ErrCertMissingCommonName
	}

	tenantID, err := callerTenant(ctx)
	if err != nil {
		return nil, err
	}

	sans := buildSANs(req.DNSNames, req.IPAddresses)
	profile := usageToProfile(req.Usage)

	xkmsReq := &xkmstransport.IssueCertificateRequest{
		CommonName:   req.CommonName,
		Organization: req.Organization,
		SANs:         sans,
		Profile:      profile,
		Algorithm:    req.KeyAlgorithm,
		ValidityDays: req.ValidityDays,
		TenantID:     tenantID,
	}

	resp, err := s.caService.IssueCertificate(ctx, xkmsReq)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.IssueCertificate",
			Err:  err,
		}
	}

	return &IssueCertificateResponse{
		CertificatePEM: string(resp.CertificatePEM),
		ChainPEM:       string(resp.ChainPEM),
		PrivateKeyPEM:  string(resp.PrivateKeyPEM),
		SerialNumber:   resp.SerialNumber,
	}, nil
}

func (s *certificateService) SignCSR(ctx context.Context, req *SignCSRRequest) (*SignCSRResponse, error) {
	if req == nil || req.CSRPEM == "" {
		return nil, ErrCertMissingCSR
	}

	// Capture the request tenant from context (do NOT discard it). When a
	// trust domain is configured server-side, the issuing CA stamps a
	// tenant-scoped SPIFFE URI SAN onto the leaf so mTLS/PIV login can derive
	// the tenant from the verified peer certificate. The TenantID flows on the
	// transport request to every server-side CA adapter (embedded, REST, gRPC,
	// QUIC, MCP, Unix); the trust domain and SPIFFE kind are resolved by the
	// adapter from its operator config and the requested profile.
	tenantID, err := callerTenant(ctx)
	if err != nil {
		return nil, err
	}

	profile := usageToProfile(req.Usage)

	xkmsReq := &xkmstransport.SignCSRRequest{
		CSRPEM:       []byte(req.CSRPEM),
		Profile:      profile,
		ValidityDays: req.ValidityDays,
		TenantID:     tenantID,
	}

	resp, err := s.caService.SignCSR(ctx, xkmsReq)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.SignCSR",
			Err:  err,
		}
	}

	return &SignCSRResponse{
		CertificatePEM: string(resp.CertificatePEM),
		ChainPEM:       string(resp.ChainPEM),
		SerialNumber:   resp.SerialNumber,
	}, nil
}

func (s *certificateService) RevokeCertificate(ctx context.Context, req *RevokeCertificateRequest) (*RevokeCertificateResponse, error) {
	if req == nil || req.SerialNumber == "" {
		return nil, ErrCertMissingSerial
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	xkmsReq := &xkmstransport.RevokeCertificateRequest{
		SerialNumber: req.SerialNumber,
		Reason:       0,
	}

	resp, err := s.caService.RevokeCertificate(ctx, xkmsReq)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.RevokeCertificate",
			Err:  err,
		}
	}

	return &RevokeCertificateResponse{
		Success: resp.Success,
	}, nil
}

func (s *certificateService) VerifyCertificate(ctx context.Context, req *VerifyCertificateRequest) (*VerifyCertificateResponse, error) {
	if req == nil || req.CertificatePEM == "" {
		return nil, ErrCertMissingPEM
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(req.CertificatePEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: "invalid PEM format",
		}, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: err.Error(),
		}, nil
	}

	isRevokedResp, err := s.caService.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{
		SerialNumber: cert.SerialNumber.String(),
	})
	if err != nil {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: err.Error(),
		}, nil
	}

	if isRevokedResp.Revoked {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: "certificate is revoked",
		}, nil
	}

	caResp, err := s.caService.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{})
	if err != nil {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: err.Error(),
		}, nil
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caResp.CertificatePEM) {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: "failed to parse CA certificate",
		}, nil
	}

	_, err = cert.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
	})
	if err != nil {
		return &VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: err.Error(),
		}, nil
	}

	return &VerifyCertificateResponse{
		Valid: true,
	}, nil
}

func (s *certificateService) GetCACertificate(ctx context.Context, req *GetCACertificateRequest) (*GetCACertificateResponse, error) {
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	caResp, err := s.caService.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GetCACertificate",
			Err:  err,
		}
	}

	bundleResp, err := s.caService.GetCABundle(ctx, &xkmstransport.GetCABundleRequest{})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GetCACertificate",
			Err:  err,
		}
	}

	return &GetCACertificateResponse{
		CACertificatePEM: string(caResp.CertificatePEM),
		CABundlePEM:      string(bundleResp.BundlePEM),
		Identity:         caResp.Subject,
	}, nil
}

func (s *certificateService) GetTLSConfig(ctx context.Context, req *GetTLSConfigRequest) (*GetTLSConfigResponse, error) {
	if req == nil {
		req = &GetTLSConfigRequest{}
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	caResp, err := s.caService.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GetTLSConfig",
			Err:  err,
		}
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caResp.CertificatePEM) {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GetTLSConfig",
			Err:  errCertInvalidPEM,
		}
	}

	var clientAuth tls.ClientAuthType
	if req.RequireClientCert {
		clientAuth = tls.RequireAndVerifyClientCert
	}

	config := &tls.Config{
		RootCAs:    roots,
		MinVersion: tls.VersionTLS13,
		ServerName: req.ServerName,
	}

	if req.IsServer {
		config.ClientCAs = roots
		config.ClientAuth = clientAuth
	}

	return &GetTLSConfigResponse{
		Config: config,
	}, nil
}

func (s *certificateService) IsInitialized() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.caService.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{})
	return err == nil
}

func (s *certificateService) CAService() xkmstransport.Client {
	return s.caService
}

func (s *certificateService) GetCABundle(ctx context.Context, req *GetCABundleRequest) (*GetCABundleResponse, error) {
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	xkmsReq := &xkmstransport.GetCABundleRequest{}
	if req != nil {
		xkmsReq.StoreType = req.StoreType
		xkmsReq.Algorithm = req.Algorithm
	}

	resp, err := s.caService.GetCABundle(ctx, xkmsReq)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GetCABundle",
			Err:  err,
		}
	}

	return &GetCABundleResponse{
		BundlePEM: string(resp.BundlePEM),
	}, nil
}

func (s *certificateService) GenerateCRL(ctx context.Context, req *GenerateCRLRequest) (*GenerateCRLResponse, error) {
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	resp, err := s.caService.GenerateCRL(ctx, &xkmstransport.GenerateCRLRequest{})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.GenerateCRL",
			Err:  err,
		}
	}

	return &GenerateCRLResponse{
		CRLPEM: string(resp.CRLPEM),
	}, nil
}

func (s *certificateService) IsRevoked(ctx context.Context, req *IsRevokedRequest) (*IsRevokedResponse, error) {
	if req == nil || req.SerialNumber == "" {
		return nil, ErrCertMissingSerial
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	resp, err := s.caService.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{
		SerialNumber: req.SerialNumber,
	})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.IsRevoked",
			Err:  err,
		}
	}

	return &IsRevokedResponse{
		Revoked: resp.Revoked,
		Reason:  resp.Reason,
		Message: resp.Message,
	}, nil
}

// buildSANs converts DNS names and IP addresses into xkms SAN format strings.
func buildSANs(dnsNames, ipAddresses []string) []string {
	var sans []string
	for _, dns := range dnsNames {
		sans = append(sans, fmt.Sprintf("DNS:%s", dns))
	}
	for _, ip := range ipAddresses {
		sans = append(sans, fmt.Sprintf("IP:%s", ip))
	}
	return sans
}

// usageToProfile converts a QuicBaaS usage string to an xkms certificate profile.
func usageToProfile(usage string) string {
	switch usage {
	case "client":
		return "client"
	case "server":
		return "server"
	default:
		return "server"
	}
}

// SPIFFEKindForProfile maps an xkms certificate profile to the SPIFFE workload
// kind used when stamping a tenant-scoped SPIFFE URI SAN.
//
// The "client" profile is the human-login (TLS client authentication) path, so
// it resolves to spiffe KindUser. Every other profile — server, codesigning,
// email, ocsp, timestamping, ca — describes a non-human service / device /
// workload identity and resolves to KindService ("svc"). This guarantees a
// service or device certificate is NEVER stamped as a human login subject (the
// xkms default of empty kind -> "user" would otherwise misclassify a server
// cert). The decision is deterministic from the profile alone; there is no
// ambiguity given the locked profile set.
func SPIFFEKindForProfile(profile string) string {
	if strings.EqualFold(strings.TrimSpace(profile), "client") {
		return qrdbsdk.SpiffeKindUser
	}
	return qrdbsdk.SpiffeKindService
}

// =============================================================================
// CA Administration Surface — types & implementation
// =============================================================================

// CAInfoRequest is the input to CertificateService.CAInfo. No fields
// today; the method returns metadata for the active issuing CA.
type CAInfoRequest struct{}

// CAInfoResponse describes the issuing CA's leaf certificate plus
// parsed metadata extracted from the PEM (subject, issuer, serial,
// validity, key type / size, signature algorithm). CertificatePEM and
// CABundlePEM are included so a single CAInfo call can drive downstream
// trust-store configuration without a separate GetCACertificate hop.
type CAInfoResponse struct {
	CommonName         string    `json:"common_name"`
	Subject            string    `json:"subject"`
	Issuer             string    `json:"issuer"`
	SerialNumber       string    `json:"serial_number"`
	NotBefore          time.Time `json:"not_before"`
	NotAfter           time.Time `json:"not_after"`
	IsCA               bool      `json:"is_ca"`
	KeyType            string    `json:"key_type"`
	KeyBits            int       `json:"key_bits,omitempty"`
	SignatureAlgorithm string    `json:"signature_algorithm"`
	CertificatePEM     string    `json:"certificate_pem"`
	CABundlePEM        string    `json:"ca_bundle_pem"`
}

// CertificateSummary describes one certificate from a ListCertificates
// response. The full PEM body is intentionally omitted to keep list
// responses small.
type CertificateSummary struct {
	CommonName   string    `json:"common_name"`
	Subject      string    `json:"subject"`
	Issuer       string    `json:"issuer"`
	SerialNumber string    `json:"serial_number"`
	NotBefore    time.Time `json:"not_before"`
	NotAfter     time.Time `json:"not_after"`
	IsCA         bool      `json:"is_ca"`
	KeyUsage     string    `json:"key_usage,omitempty"`
	DNSNames     []string  `json:"dns_names,omitempty"`
}

// ListCertificatesRequest carries optional filters for the
// ListCertificates operation.
type ListCertificatesRequest struct {
	CommonNameFilter string `json:"common_name_filter,omitempty"`
	IncludeRevoked   bool   `json:"include_revoked,omitempty"`
	IncludeExpired   bool   `json:"include_expired,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

// ListCertificatesResponse contains the certificate summaries that
// matched a ListCertificates request, plus the total unfiltered count.
type ListCertificatesResponse struct {
	Certificates []CertificateSummary `json:"certificates"`
	TotalCount   int                  `json:"total_count"`
}

// CertStoreLister is the optional capability satisfied by CA service
// adapters that can enumerate stored certificates. The quicbaas
// caServiceAdapter (pkg/server) implements this by walking the
// embedded TrustedCA's CertStore. CertificateService.ListCertificates
// type-asserts its underlying caService to this interface; backends
// that don't expose CertStore access return ErrCertListNotSupported.
//
// The method name (ListStoredCertificates rather than the more
// natural ListCertificates) deliberately avoids the xkmstransport.Client
// method of the same name, which has an incompatible signature
// (ListCertificates(ctx, backend, opts...) returns key-store backed
// certs, NOT CA-store entries). A type assertion would be a compile
// error if both methods shared a name on a type that satisfied both.
type CertStoreLister interface {
	ListStoredCertificates(ctx context.Context) ([]*x509.Certificate, error)
}

// ErrCertListNotSupported is returned by CertificateService.ListCertificates
// when the underlying CA service adapter does not implement
// CertStoreLister.
var ErrCertListNotSupported = &errors.QBError{
	Code: errors.ErrUnimplemented,
	Op:   "CertificateService.ListCertificates",
	Err:  errCertListNotSupported,
}

var errCertListNotSupported = stderrors.New("CA service does not expose certificate enumeration")

// CAInfo returns parsed metadata for the configured issuing CA. The
// implementation derives the parsed fields client-side by decoding the
// CA leaf PEM returned by GetCACertificate so that all transports get
// the same shape without each having to teach its handlers to parse
// X.509.
func (s *certificateService) CAInfo(ctx context.Context, req *CAInfoRequest) (*CAInfoResponse, error) {
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	caResp, err := s.GetCACertificate(ctx, &GetCACertificateRequest{})
	if err != nil {
		return nil, err
	}
	if caResp == nil || caResp.CACertificatePEM == "" {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "CertificateService.CAInfo",
			Err:  errCertNotInitialized,
		}
	}

	cert, perr := decodeFirstCertPEM([]byte(caResp.CACertificatePEM))
	if perr != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.CAInfo",
			Err:  perr,
		}
	}

	keyType, keyBits := describePublicKey(cert)

	return &CAInfoResponse{
		CommonName:         cert.Subject.CommonName,
		Subject:            cert.Subject.String(),
		Issuer:             cert.Issuer.String(),
		SerialNumber:       cert.SerialNumber.Text(16),
		NotBefore:          cert.NotBefore.UTC(),
		NotAfter:           cert.NotAfter.UTC(),
		IsCA:               cert.IsCA,
		KeyType:            keyType,
		KeyBits:            keyBits,
		SignatureAlgorithm: cert.SignatureAlgorithm.String(),
		CertificatePEM:     caResp.CACertificatePEM,
		CABundlePEM:        caResp.CABundlePEM,
	}, nil
}

// ListCertificates enumerates certificates stored in the CA's
// CertStore. The underlying caService must implement CertStoreLister;
// otherwise ErrCertListNotSupported is returned. Filtering is performed
// in-process: backends do not currently push filters down to storage.
func (s *certificateService) ListCertificates(ctx context.Context, req *ListCertificatesRequest) (*ListCertificatesResponse, error) {
	if req == nil {
		req = &ListCertificatesRequest{}
	}
	if req.Limit < 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CertificateService.ListCertificates",
			Err:  errCertNegativeLimit,
		}
	}
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	lister, ok := s.caService.(CertStoreLister)
	if !ok {
		return nil, ErrCertListNotSupported
	}

	certs, err := lister.ListStoredCertificates(ctx)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.ListCertificates",
			Err:  err,
		}
	}

	now := time.Now()
	filter := strings.ToLower(strings.TrimSpace(req.CommonNameFilter))
	out := make([]CertificateSummary, 0, len(certs))
	for _, cert := range certs {
		if cert == nil {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(cert.Subject.CommonName), filter) {
			continue
		}
		if !req.IncludeExpired && cert.NotAfter.Before(now) {
			continue
		}
		if !req.IncludeRevoked {
			revoked, _ := s.caService.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{
				SerialNumber: cert.SerialNumber.Text(16),
			})
			if revoked != nil && revoked.Revoked {
				continue
			}
		}
		out = append(out, summariseCertificate(cert))
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
	}

	return &ListCertificatesResponse{
		Certificates: out,
		TotalCount:   len(certs),
	}, nil
}

// summariseCertificate projects an x509.Certificate into the compact
// CertificateSummary returned by ListCertificates.
func summariseCertificate(cert *x509.Certificate) CertificateSummary {
	return CertificateSummary{
		CommonName:   cert.Subject.CommonName,
		Subject:      cert.Subject.String(),
		Issuer:       cert.Issuer.String(),
		SerialNumber: cert.SerialNumber.Text(16),
		NotBefore:    cert.NotBefore.UTC(),
		NotAfter:     cert.NotAfter.UTC(),
		IsCA:         cert.IsCA,
		KeyUsage:     formatKeyUsage(cert.KeyUsage),
		DNSNames:     append([]string(nil), cert.DNSNames...),
	}
}

// decodeFirstCertPEM parses the first CERTIFICATE block out of the
// supplied PEM bytes. Returns errCertInvalidPEM when no CERTIFICATE
// block is present and errCertParseFailed when the DER body is
// malformed.
func decodeFirstCertPEM(pemBytes []byte) (*x509.Certificate, error) {
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, errCertInvalidPEM
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, errCertParseFailed
		}
		return cert, nil
	}
}

// describePublicKey returns a human-readable key algorithm label and
// the key bit length where applicable.
func describePublicKey(cert *x509.Certificate) (string, int) {
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA", pub.N.BitLen()
	case *ecdsa.PublicKey:
		if pub.Curve == nil || pub.Params() == nil {
			return "ECDSA", 0
		}
		return "ECDSA", pub.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", len(pub) * 8
	default:
		return "Unknown", 0
	}
}

// formatKeyUsage joins enabled X509 KeyUsage flags into a human-readable
// comma-separated string. Returns "" when no flags are set.
func formatKeyUsage(usage x509.KeyUsage) string {
	names := []struct {
		flag x509.KeyUsage
		name string
	}{
		{x509.KeyUsageDigitalSignature, "DigitalSignature"},
		{x509.KeyUsageContentCommitment, "ContentCommitment"},
		{x509.KeyUsageKeyEncipherment, "KeyEncipherment"},
		{x509.KeyUsageDataEncipherment, "DataEncipherment"},
		{x509.KeyUsageKeyAgreement, "KeyAgreement"},
		{x509.KeyUsageCertSign, "CertSign"},
		{x509.KeyUsageCRLSign, "CRLSign"},
		{x509.KeyUsageEncipherOnly, "EncipherOnly"},
		{x509.KeyUsageDecipherOnly, "DecipherOnly"},
	}
	var parts []string
	for _, n := range names {
		if usage&n.flag != 0 {
			parts = append(parts, n.name)
		}
	}
	return strings.Join(parts, ", ")
}

var (
	errCertNotInitialized = stderrors.New("CA certificate not available")
	errCertNegativeLimit  = stderrors.New("limit cannot be negative")
)
