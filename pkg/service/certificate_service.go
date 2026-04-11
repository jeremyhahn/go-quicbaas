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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"

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

	if _, err := certPrepareTenantContext(ctx); err != nil {
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

	if _, err := certPrepareTenantContext(ctx); err != nil {
		return nil, err
	}

	profile := usageToProfile(req.Usage)

	xkmsReq := &xkmstransport.SignCSRRequest{
		CSRPEM:       []byte(req.CSRPEM),
		Profile:      profile,
		ValidityDays: req.ValidityDays,
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

	if _, err := certPrepareTenantContext(ctx); err != nil {
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

	if _, err := certPrepareTenantContext(ctx); err != nil {
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
	if _, err := certPrepareTenantContext(ctx); err != nil {
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

	if _, err := certPrepareTenantContext(ctx); err != nil {
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
	if _, err := certPrepareTenantContext(ctx); err != nil {
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
	if _, err := certPrepareTenantContext(ctx); err != nil {
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

	if _, err := certPrepareTenantContext(ctx); err != nil {
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
