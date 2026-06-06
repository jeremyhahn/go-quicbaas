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

package certificate

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Certificate"
)

// Client provides certificate management operations.
type Client interface {
	// IssueCertificate generates a new certificate.
	IssueCertificate(ctx context.Context, req *IssueCertificateRequest) (*IssueCertificateResponse, error)

	// SignCSR signs a Certificate Signing Request.
	SignCSR(ctx context.Context, req *SignCSRRequest) (*SignCSRResponse, error)

	// RevokeCertificate marks a certificate as revoked.
	RevokeCertificate(ctx context.Context, req *RevokeCertificateRequest) (*RevokeCertificateResponse, error)

	// VerifyCertificate checks if a certificate is valid.
	VerifyCertificate(ctx context.Context, req *VerifyCertificateRequest) (*VerifyCertificateResponse, error)

	// GetCACertificate returns the CA certificate.
	GetCACertificate(ctx context.Context, req *GetCACertificateRequest) (*GetCACertificateResponse, error)

	// GetCABundle returns the CA certificate bundle.
	GetCABundle(ctx context.Context, req *GetCABundleRequest) (*GetCABundleResponse, error)

	// GetTLSConfig returns a TLS configuration.
	GetTLSConfig(ctx context.Context, req *GetTLSConfigRequest) (*GetTLSConfigResponse, error)

	// IsInitialized returns whether the certificate service is ready.
	IsInitialized(ctx context.Context) (bool, error)

	// GenerateCRL generates a Certificate Revocation List.
	GenerateCRL(ctx context.Context, req *GenerateCRLRequest) (*GenerateCRLResponse, error)

	// IsRevoked checks whether a certificate serial number is revoked.
	IsRevoked(ctx context.Context, req *IsRevokedRequest) (*IsRevokedResponse, error)

	// CAInfo returns parsed metadata for the active issuing CA: subject,
	// issuer, serial, validity window, key type/size, signature
	// algorithm, the CA leaf PEM and the full bundle PEM.
	CAInfo(ctx context.Context, req *CAInfoRequest) (*CAInfoResponse, error)

	// ListCertificates enumerates certificates stored in the CA's
	// certificate store, optionally filtered by Common Name substring
	// and revocation / expiration status.
	ListCertificates(ctx context.Context, req *ListCertificatesRequest) (*ListCertificatesResponse, error)
}

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

// GetCABundleRequest contains parameters for retrieving a CA bundle.
type GetCABundleRequest struct {
	StoreType string `json:"store_type,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
}

// GetCABundleResponse contains the CA certificate bundle.
type GetCABundleResponse struct {
	BundlePEM string `json:"bundle_pem"`
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

// IsInitializedResponse contains the initialization status.
type IsInitializedResponse struct {
	Initialized bool `json:"initialized"`
}

// isInitializedRequest is the internal request for checking initialization.
type isInitializedRequest struct{}

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

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Certificate client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// IssueCertificate generates a new certificate.
func (c *client) IssueCertificate(ctx context.Context, req *IssueCertificateRequest) (*IssueCertificateResponse, error) {
	if err := validateIssueCertificateRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.IssueCertificate", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "IssueCertificate", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.IssueCertificate", "nil response from server")
	}

	issueResp, ok := resp.(*IssueCertificateResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.IssueCertificate", "invalid response type")
	}

	return issueResp, nil
}

// SignCSR signs a Certificate Signing Request.
func (c *client) SignCSR(ctx context.Context, req *SignCSRRequest) (*SignCSRResponse, error) {
	if err := validateSignCSRRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.SignCSR", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SignCSR", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.SignCSR", "nil response from server")
	}

	signResp, ok := resp.(*SignCSRResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.SignCSR", "invalid response type")
	}

	return signResp, nil
}

// RevokeCertificate marks a certificate as revoked.
func (c *client) RevokeCertificate(ctx context.Context, req *RevokeCertificateRequest) (*RevokeCertificateResponse, error) {
	if err := validateRevokeCertificateRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.RevokeCertificate", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeCertificate", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.RevokeCertificate", "nil response from server")
	}

	revokeResp, ok := resp.(*RevokeCertificateResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.RevokeCertificate", "invalid response type")
	}

	return revokeResp, nil
}

// VerifyCertificate checks if a certificate is valid.
func (c *client) VerifyCertificate(ctx context.Context, req *VerifyCertificateRequest) (*VerifyCertificateResponse, error) {
	if err := validateVerifyCertificateRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.VerifyCertificate", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "VerifyCertificate", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.VerifyCertificate", "nil response from server")
	}

	verifyResp, ok := resp.(*VerifyCertificateResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.VerifyCertificate", "invalid response type")
	}

	return verifyResp, nil
}

// GetCACertificate returns the CA certificate.
func (c *client) GetCACertificate(ctx context.Context, req *GetCACertificateRequest) (*GetCACertificateResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.GetCACertificate", "context is required")
	}

	if req == nil {
		req = &GetCACertificateRequest{}
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCACertificate", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.GetCACertificate", "nil response from server")
	}

	caResp, ok := resp.(*GetCACertificateResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.GetCACertificate", "invalid response type")
	}

	return caResp, nil
}

// GetCABundle returns the CA certificate bundle.
func (c *client) GetCABundle(ctx context.Context, req *GetCABundleRequest) (*GetCABundleResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.GetCABundle", "context is required")
	}

	if req == nil {
		req = &GetCABundleRequest{}
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCABundle", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.GetCABundle", "nil response from server")
	}

	bundleResp, ok := resp.(*GetCABundleResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.GetCABundle", "invalid response type")
	}

	return bundleResp, nil
}

// GetTLSConfig returns a TLS configuration.
func (c *client) GetTLSConfig(ctx context.Context, req *GetTLSConfigRequest) (*GetTLSConfigResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.GetTLSConfig", "context is required")
	}

	if req == nil {
		req = &GetTLSConfigRequest{}
	}

	if err := validateGetTLSConfigRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTLSConfig", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.GetTLSConfig", "nil response from server")
	}

	tlsResp, ok := resp.(*GetTLSConfigResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.GetTLSConfig", "invalid response type")
	}

	return tlsResp, nil
}

// IsInitialized returns whether the certificate service is ready.
func (c *client) IsInitialized(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.NewInvalidInput("certificate.IsInitialized", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "IsInitialized", &isInitializedRequest{})
	if err != nil {
		return false, err
	}

	if resp == nil {
		return false, errors.NewInternal("certificate.IsInitialized", "nil response from server")
	}

	initResp, ok := resp.(*IsInitializedResponse)
	if !ok {
		return false, errors.NewInternal("certificate.IsInitialized", "invalid response type")
	}

	return initResp.Initialized, nil
}

// GenerateCRL generates a Certificate Revocation List.
func (c *client) GenerateCRL(ctx context.Context, req *GenerateCRLRequest) (*GenerateCRLResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.GenerateCRL", "context is required")
	}

	if req == nil {
		req = &GenerateCRLRequest{}
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateCRL", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.GenerateCRL", "nil response from server")
	}

	crlResp, ok := resp.(*GenerateCRLResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.GenerateCRL", "invalid response type")
	}

	return crlResp, nil
}

// IsRevoked checks whether a certificate serial number is revoked.
func (c *client) IsRevoked(ctx context.Context, req *IsRevokedRequest) (*IsRevokedResponse, error) {
	if err := validateIsRevokedRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.IsRevoked", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "IsRevoked", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("certificate.IsRevoked", "nil response from server")
	}

	revokedResp, ok := resp.(*IsRevokedResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.IsRevoked", "invalid response type")
	}

	return revokedResp, nil
}

// Validation functions

func validateIssueCertificateRequest(req *IssueCertificateRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.IssueCertificate", "request is required")
	}
	if req.CommonName == "" {
		return errors.NewInvalidInput("certificate.IssueCertificate", "common name is required")
	}

	if req.Usage != "" {
		switch req.Usage {
		case "client", "server", "both":
			// Valid usage
		default:
			return errors.NewInvalidInput("certificate.IssueCertificate", "invalid usage type").
				WithContext("usage", req.Usage).
				WithContext("valid_values", []string{"client", "server", "both"})
		}
	}

	if req.KeyAlgorithm != "" {
		switch req.KeyAlgorithm {
		case "ecdsa", "rsa":
			// Valid algorithm
		default:
			return errors.NewInvalidInput("certificate.IssueCertificate", "invalid key algorithm").
				WithContext("key_algorithm", req.KeyAlgorithm).
				WithContext("valid_values", []string{"ecdsa", "rsa"})
		}
	}

	if req.ValidityDays < 0 {
		return errors.NewInvalidInput("certificate.IssueCertificate", "validity days cannot be negative").
			WithContext("validity_days", req.ValidityDays)
	}

	if req.MaxPathLen < 0 {
		return errors.NewInvalidInput("certificate.IssueCertificate", "max path length cannot be negative").
			WithContext("max_path_len", req.MaxPathLen)
	}

	if req.KeySize < 0 {
		return errors.NewInvalidInput("certificate.IssueCertificate", "key size cannot be negative").
			WithContext("key_size", req.KeySize)
	}

	return nil
}

func validateSignCSRRequest(req *SignCSRRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.SignCSR", "request is required")
	}
	if req.CSRPEM == "" {
		return errors.NewInvalidInput("certificate.SignCSR", "CSR PEM is required")
	}

	if req.Usage != "" {
		switch req.Usage {
		case "client", "server", "both":
			// Valid usage
		default:
			return errors.NewInvalidInput("certificate.SignCSR", "invalid usage type").
				WithContext("usage", req.Usage).
				WithContext("valid_values", []string{"client", "server", "both"})
		}
	}

	if req.ValidityDays < 0 {
		return errors.NewInvalidInput("certificate.SignCSR", "validity days cannot be negative").
			WithContext("validity_days", req.ValidityDays)
	}

	return nil
}

func validateRevokeCertificateRequest(req *RevokeCertificateRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.RevokeCertificate", "request is required")
	}
	if req.SerialNumber == "" {
		return errors.NewInvalidInput("certificate.RevokeCertificate", "serial number is required")
	}
	return nil
}

func validateVerifyCertificateRequest(req *VerifyCertificateRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.VerifyCertificate", "request is required")
	}
	if req.CertificatePEM == "" {
		return errors.NewInvalidInput("certificate.VerifyCertificate", "certificate PEM is required")
	}
	return nil
}

func validateGetTLSConfigRequest(req *GetTLSConfigRequest) error {
	if req.MinVersion != "" {
		switch req.MinVersion {
		case "1.2", "1.3":
			// Valid min version
		default:
			return errors.NewInvalidInput("certificate.GetTLSConfig", "invalid TLS min version").
				WithContext("min_version", req.MinVersion).
				WithContext("valid_values", []string{"1.2", "1.3"})
		}
	}
	return nil
}

func validateIsRevokedRequest(req *IsRevokedRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.IsRevoked", "request is required")
	}
	if req.SerialNumber == "" {
		return errors.NewInvalidInput("certificate.IsRevoked", "serial number is required")
	}
	return nil
}

// =============================================================================
// CA Administration Surface
// =============================================================================
//
// The following types and methods extend the certificate client with
// CA-administration primitives that back the canonical `quicbaas ca` CLI
// verb set (info, bundle, list, issue, revoke, export-bundle). Issue and
// Revoke are deliberately re-used from the existing IssueCertificate and
// RevokeCertificate methods above — the CA admin surface focuses on
// inspection (CAInfo, ListCertificates) and bulk-export operations
// (ExportBundle) that have no analogue on the per-certificate methods.

// CAInfoRequest is the request payload for CAInfo. No fields today; the
// server returns metadata about the configured issuing CA.
type CAInfoRequest struct{}

// CAInfoResponse describes the issuing CA's leaf certificate, its
// trust-chain bundle, and parsed metadata extracted from the certificate
// (subject, issuer, serial, validity window, key type, signature
// algorithm, IsCA flag).
type CAInfoResponse struct {
	// CommonName is the CA leaf certificate's Common Name.
	CommonName string `json:"common_name"`

	// Subject is the full RFC 4514 subject distinguished name.
	Subject string `json:"subject"`

	// Issuer is the full RFC 4514 issuer distinguished name. Equal to
	// Subject when the CA is self-signed.
	Issuer string `json:"issuer"`

	// SerialNumber is the issuing CA certificate's serial number,
	// hex-encoded (no 0x prefix).
	SerialNumber string `json:"serial_number"`

	// NotBefore is the start of the CA certificate validity window
	// (UTC, RFC 3339).
	NotBefore time.Time `json:"not_before"`

	// NotAfter is the end of the CA certificate validity window
	// (UTC, RFC 3339).
	NotAfter time.Time `json:"not_after"`

	// IsCA reports whether the BasicConstraints extension marks this
	// certificate as a CA.
	IsCA bool `json:"is_ca"`

	// KeyType describes the public key algorithm ("RSA", "ECDSA",
	// "Ed25519", "Unknown").
	KeyType string `json:"key_type"`

	// KeyBits is the public key size in bits (e.g. 2048 for RSA, 256
	// for P-256 ECDSA). Zero when not applicable.
	KeyBits int `json:"key_bits,omitempty"`

	// SignatureAlgorithm describes the algorithm used to sign the CA
	// certificate ("SHA256-RSA", "ECDSA-SHA384", ...).
	SignatureAlgorithm string `json:"signature_algorithm"`

	// CertificatePEM is the PEM-encoded CA leaf certificate.
	CertificatePEM string `json:"certificate_pem"`

	// CABundlePEM is the PEM-encoded CA trust chain (issuer-first).
	// Equal to CertificatePEM when the CA is a single self-signed root.
	CABundlePEM string `json:"ca_bundle_pem"`
}

// CertificateSummary is a compact description of a single certificate as
// returned by ListCertificates. The full PEM body is omitted to keep
// list responses small; callers that need the PEM should issue a
// follow-up GetCACertificate / dedicated retrieval call.
type CertificateSummary struct {
	// CommonName is the certificate's Common Name. Used as the
	// CertStore lookup key by the underlying backend.
	CommonName string `json:"common_name"`

	// Subject is the full RFC 4514 subject distinguished name.
	Subject string `json:"subject"`

	// Issuer is the full RFC 4514 issuer distinguished name.
	Issuer string `json:"issuer"`

	// SerialNumber is the certificate's serial number hex-encoded.
	SerialNumber string `json:"serial_number"`

	// NotBefore is the start of the certificate's validity window.
	NotBefore time.Time `json:"not_before"`

	// NotAfter is the end of the certificate's validity window.
	NotAfter time.Time `json:"not_after"`

	// IsCA reports whether this certificate is itself a CA.
	IsCA bool `json:"is_ca"`

	// KeyUsage is a comma-joined string of the key usage flags
	// ("DigitalSignature, KeyEncipherment, ...").
	KeyUsage string `json:"key_usage,omitempty"`

	// DNSNames lists the DNS SAN entries (may be empty).
	DNSNames []string `json:"dns_names,omitempty"`
}

// ListCertificatesRequest contains optional filters for ListCertificates.
type ListCertificatesRequest struct {
	// CommonNameFilter, when non-empty, restricts the response to
	// certificates whose Subject Common Name contains the filter
	// substring (case-insensitive). Empty matches everything.
	CommonNameFilter string `json:"common_name_filter,omitempty"`

	// IncludeRevoked, when true, includes revoked certificates in the
	// response. Defaults to false (only currently-valid certificates).
	IncludeRevoked bool `json:"include_revoked,omitempty"`

	// IncludeExpired, when true, includes certificates that have
	// already expired (NotAfter < now). Defaults to false.
	IncludeExpired bool `json:"include_expired,omitempty"`

	// Limit caps the response size. Zero means "no caller-supplied
	// limit"; the server may still apply a hard maximum.
	Limit int `json:"limit,omitempty"`
}

// ListCertificatesResponse is the response payload for ListCertificates.
type ListCertificatesResponse struct {
	// Certificates is the (possibly empty) list of certificate
	// summaries that matched the request filters.
	Certificates []CertificateSummary `json:"certificates"`

	// TotalCount is the unfiltered total number of certificates in the
	// CertStore. Useful to display "showing N of M" in UIs.
	TotalCount int `json:"total_count"`
}

// CAInfo returns parsed metadata for the active issuing CA, including
// subject/issuer/validity, key type/size, signature algorithm, the CA
// leaf PEM, and the full CA bundle PEM. Equivalent to GetCACertificate
// plus client-side parsing, but performed once on the server so all
// transports return the same parsed shape.
func (c *client) CAInfo(ctx context.Context, req *CAInfoRequest) (*CAInfoResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.CAInfo", "context is required")
	}
	if req == nil {
		req = &CAInfoRequest{}
	}

	resp, err := c.transport.Request(ctx, serviceName, "CAInfo", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("certificate.CAInfo", "nil response from server")
	}
	infoResp, ok := resp.(*CAInfoResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.CAInfo", "invalid response type")
	}
	return infoResp, nil
}

// ListCertificates enumerates certificates currently stored in the CA's
// certificate store. Returns summaries (Subject/Issuer/Serial/validity)
// rather than full PEM bodies to keep responses compact.
func (c *client) ListCertificates(ctx context.Context, req *ListCertificatesRequest) (*ListCertificatesResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("certificate.ListCertificates", "context is required")
	}
	if req == nil {
		req = &ListCertificatesRequest{}
	}
	if err := validateListCertificatesRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListCertificates", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("certificate.ListCertificates", "nil response from server")
	}
	listResp, ok := resp.(*ListCertificatesResponse)
	if !ok {
		return nil, errors.NewInternal("certificate.ListCertificates", "invalid response type")
	}
	return listResp, nil
}

func validateListCertificatesRequest(req *ListCertificatesRequest) error {
	if req == nil {
		return errors.NewInvalidInput("certificate.ListCertificates", "request is required")
	}
	if req.Limit < 0 {
		return errors.NewInvalidInput("certificate.ListCertificates", "limit cannot be negative").
			WithContext("limit", req.Limit)
	}
	return nil
}
