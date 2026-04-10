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

// Package service provides the unified service facade for QuicBaaS.
package service

import "context"

// =============================================================================
// CA Service - Certificate Authority Operations
// =============================================================================

// CAService handles Certificate Authority (CA) operations for issuing,
// revoking, and managing X.509 certificates. This service provides a
// complete PKI lifecycle management interface.
//
// Certificate Authority operations include:
//   - CA certificate and bundle retrieval
//   - CSR signing with configurable certificate profiles
//   - Full certificate issuance with key generation
//   - Certificate revocation with RFC 5280 reason codes
//   - Certificate Revocation List (CRL) generation
//   - Revocation status checking
//
// All operations delegate to the underlying go-xkms CAService, with
// QuicBaaS-specific tenant context injection and access control enforcement.
type CAService interface {
	// GetCABundle retrieves the CA certificate bundle containing the full
	// certificate chain from root to issuing CA. The bundle can be filtered
	// by store type and algorithm.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request with optional store type and algorithm filters
	// Returns:
	//   - CAGetBundleResponse: Contains PEM bundle and individual DER certificates
	//   - error: ErrUnavailable, ErrUnauthorized, ErrTimeout
	GetCABundle(ctx context.Context, req *CAGetBundleRequest) (*CAGetBundleResponse, error)

	// GetCACertificate retrieves the CA certificate for a specific identity.
	// If no identity is specified, returns the default CA certificate.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request with optional CA identity filter
	// Returns:
	//   - CAGetCertificateResponse: Contains the PEM certificate and metadata
	//   - error: ErrNotFound, ErrUnavailable, ErrUnauthorized, ErrTimeout
	GetCACertificate(ctx context.Context, req *CAGetCertificateRequest) (*CAGetCertificateResponse, error)

	// SignCSR signs a PKCS#10 Certificate Signing Request using the CA private key.
	// The resulting certificate uses the subject from the CSR with CA-enforced
	// extensions based on the selected certificate profile.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the PEM-encoded CSR, profile, and validity period
	// Returns:
	//   - CASignCSRResponse: Contains the signed certificate, chain, and serial number
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	SignCSR(ctx context.Context, req *CASignCSRRequest) (*CASignCSRResponse, error)

	// IssueCertificate generates a new key pair and issues a certificate in a
	// single operation. This is a convenience method that combines key generation
	// and CSR signing.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing subject, SANs, profile, validity, and algorithm
	// Returns:
	//   - CAIssueCertificateResponse: Contains certificate, chain, optional private key, and serial number
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	IssueCertificate(ctx context.Context, req *CAIssueCertificateRequest) (*CAIssueCertificateResponse, error)

	// RevokeCertificate marks a certificate as revoked using the specified
	// RFC 5280 reason code. Revoked certificates appear in subsequent CRL
	// generations.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the serial number and revocation reason code
	// Returns:
	//   - CARevokeCertificateResponse: Contains success status and message
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	RevokeCertificate(ctx context.Context, req *CARevokeCertificateRequest) (*CARevokeCertificateResponse, error)

	// GenerateCRL generates a Certificate Revocation List containing all
	// currently revoked certificates. The CRL is signed by the CA.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request with optional tenant scope
	// Returns:
	//   - CAGenerateCRLResponse: Contains the PEM-encoded CRL
	//   - error: ErrUnavailable, ErrUnauthorized, ErrTimeout
	GenerateCRL(ctx context.Context, req *CAGenerateCRLRequest) (*CAGenerateCRLResponse, error)

	// IsRevoked checks whether a certificate with the given serial number
	// has been revoked. Returns the revocation status and reason code.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the certificate serial number
	// Returns:
	//   - CAIsRevokedResponse: Contains revocation status, reason, and message
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	IsRevoked(ctx context.Context, req *CAIsRevokedRequest) (*CAIsRevokedResponse, error)
}

// =============================================================================
// CA Service Request/Response Types
// =============================================================================

// CAGetBundleRequest contains parameters for retrieving the CA certificate bundle.
type CAGetBundleRequest struct {
	// StoreType filters the bundle by backend store type: "software", "tpm2", "pkcs11".
	StoreType string

	// Algorithm filters the bundle by key algorithm: "RSA", "ECDSA", "Ed25519".
	Algorithm string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CAGetBundleResponse contains the CA certificate bundle.
type CAGetBundleResponse struct {
	// BundlePEM is the PEM-encoded certificate chain from root to issuing CA.
	BundlePEM []byte

	// Certificates contains individual DER-encoded certificates in the chain.
	Certificates [][]byte

	// ContentType is the MIME type of the bundle (e.g., "application/x-pem-file").
	ContentType string
}

// CAGetCertificateRequest contains parameters for retrieving the CA certificate.
type CAGetCertificateRequest struct {
	// Identity is the CA identity (Common Name) to retrieve. Empty for the default CA.
	Identity string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CAGetCertificateResponse contains the CA certificate and its metadata.
type CAGetCertificateResponse struct {
	// CertificatePEM is the PEM-encoded CA certificate.
	CertificatePEM []byte

	// Subject is the certificate subject distinguished name.
	Subject string

	// Issuer is the certificate issuer distinguished name.
	Issuer string

	// SerialNumber is the certificate serial number in hexadecimal.
	SerialNumber string

	// NotBefore is the certificate validity start date in RFC 3339 format.
	NotBefore string

	// NotAfter is the certificate validity end date in RFC 3339 format.
	NotAfter string

	// IsCA indicates whether this certificate has the CA basic constraint set.
	IsCA bool
}

// CASignCSRRequest contains parameters for signing a Certificate Signing Request.
type CASignCSRRequest struct {
	// CSRPEM is the PEM-encoded PKCS#10 Certificate Signing Request.
	CSRPEM []byte

	// Profile is the certificate profile determining extensions and usage.
	// Supported values: "server", "client", "idevid", "ldevid", etc.
	Profile string

	// ValidityDays overrides the default certificate validity period.
	ValidityDays int

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CASignCSRResponse contains the signed certificate.
type CASignCSRResponse struct {
	// CertificatePEM is the PEM-encoded signed certificate.
	CertificatePEM []byte

	// ChainPEM is the PEM-encoded certificate chain (excludes the leaf certificate).
	ChainPEM []byte

	// SerialNumber is the serial number assigned to the certificate.
	SerialNumber string
}

// CAIssueCertificateRequest contains parameters for issuing a new certificate.
type CAIssueCertificateRequest struct {
	// Profile is the certificate profile: "server", "client", "idevid", etc.
	Profile string

	// CommonName is the subject Common Name for the certificate.
	CommonName string

	// Organization is the optional subject Organization.
	Organization string

	// SANs is a list of Subject Alternative Names (e.g., "DNS:example.com", "IP:10.0.0.1").
	SANs []string

	// ValidityDays overrides the default certificate validity period.
	ValidityDays int

	// Algorithm specifies the key algorithm: "ecdsa-p256", "rsa2048", "ed25519".
	Algorithm string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CAIssueCertificateResponse contains the issued certificate and optional key material.
type CAIssueCertificateResponse struct {
	// CertificatePEM is the PEM-encoded issued certificate.
	CertificatePEM []byte

	// ChainPEM is the PEM-encoded certificate chain.
	ChainPEM []byte

	// PrivateKeyPEM is the PEM-encoded private key, only present when the key
	// was generated as part of issuance.
	PrivateKeyPEM []byte

	// SerialNumber is the serial number assigned to the certificate.
	SerialNumber string
}

// CARevokeCertificateRequest contains parameters for revoking a certificate.
type CARevokeCertificateRequest struct {
	// SerialNumber is the serial number of the certificate to revoke.
	SerialNumber string

	// Reason is the RFC 5280 CRL reason code:
	//   0: Unspecified
	//   1: KeyCompromise
	//   2: CACompromise
	//   3: AffiliationChanged
	//   4: Superseded
	//   5: CessationOfOperation
	//   6: CertificateHold
	Reason int

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CARevokeCertificateResponse contains the result of certificate revocation.
type CARevokeCertificateResponse struct {
	// Success indicates whether the certificate was successfully revoked.
	Success bool

	// Message provides additional context about the revocation.
	Message string
}

// CAGenerateCRLRequest contains parameters for generating a Certificate Revocation List.
type CAGenerateCRLRequest struct {
	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CAGenerateCRLResponse contains the generated CRL.
type CAGenerateCRLResponse struct {
	// CRLPEM is the PEM-encoded Certificate Revocation List.
	CRLPEM []byte
}

// CAIsRevokedRequest contains parameters for checking certificate revocation status.
type CAIsRevokedRequest struct {
	// SerialNumber is the serial number of the certificate to check.
	SerialNumber string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// CAIsRevokedResponse contains the revocation status of a certificate.
type CAIsRevokedResponse struct {
	// Revoked indicates whether the certificate has been revoked.
	Revoked bool

	// Reason is the RFC 5280 CRL reason code if the certificate is revoked.
	Reason int

	// Message provides additional context about the revocation status.
	Message string
}

// =============================================================================
// CA Service Error Types
// =============================================================================

// CANotAvailableError indicates that the Certificate Authority service is not
// configured or the underlying backend does not support CA operations.
type CANotAvailableError struct{}

// Error implements the error interface.
func (e *CANotAvailableError) Error() string {
	return "CA service not available"
}

// Is supports errors.Is matching by type.
func (e *CANotAvailableError) Is(target error) bool {
	_, ok := target.(*CANotAvailableError)
	return ok
}

// CASigningError indicates that the CA failed to sign a certificate or CSR.
type CASigningError struct {
	Cause error
}

// Error implements the error interface.
func (e *CASigningError) Error() string {
	return "CA signing failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CASigningError) Unwrap() error {
	return e.Cause
}

// CARevocationError indicates that a certificate revocation operation failed.
type CARevocationError struct {
	SerialNumber string
	Cause        error
}

// Error implements the error interface.
func (e *CARevocationError) Error() string {
	return "CA revocation failed for certificate " + e.SerialNumber + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CARevocationError) Unwrap() error {
	return e.Cause
}

// CACertificateNotFoundError indicates that the requested CA certificate
// or identity was not found.
type CACertificateNotFoundError struct {
	Identity string
}

// Error implements the error interface.
func (e *CACertificateNotFoundError) Error() string {
	if e.Identity != "" {
		return "CA certificate not found for identity: " + e.Identity
	}
	return "CA certificate not found"
}

// Is supports errors.Is matching by type.
func (e *CACertificateNotFoundError) Is(target error) bool {
	_, ok := target.(*CACertificateNotFoundError)
	return ok
}

// CAInvalidCSRError indicates that the provided CSR is malformed or invalid.
type CAInvalidCSRError struct {
	Cause error
}

// Error implements the error interface.
func (e *CAInvalidCSRError) Error() string {
	return "invalid CSR: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CAInvalidCSRError) Unwrap() error {
	return e.Cause
}

// CACRLGenerationError indicates that CRL generation failed.
type CACRLGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *CACRLGenerationError) Error() string {
	return "CRL generation failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CACRLGenerationError) Unwrap() error {
	return e.Cause
}
