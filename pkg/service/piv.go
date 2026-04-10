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
// PIV Service - Personal Identity Verification Operations
// =============================================================================

// PIVService handles Personal Identity Verification (PIV) operations for
// managing smart card certificates and key pairs in PIV-compliant slots.
//
// PIV operations follow the NIST SP 800-73 standard for PIV credentials:
//   - Slot-based certificate and key management
//   - Hardware-backed key generation in PIV slots
//   - Certificate lifecycle management per slot
//   - Certificate Signing Request generation for CA-issued certificates
//
// All operations delegate to the underlying go-xkms PIVService, with
// QuicBaaS-specific tenant context injection and access control enforcement.
type PIVService interface {
	// ListPIVSlots returns the status of all PIV slots in the specified backend.
	// Each slot entry includes occupancy status, certificate subject, algorithm,
	// and expiration information.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying the backend to query
	// Returns:
	//   - PIVListSlotsResponse: Contains a list of slot status entries
	//   - error: ErrUnavailable, ErrUnauthorized, ErrTimeout
	ListPIVSlots(ctx context.Context, req *PIVListSlotsRequest) (*PIVListSlotsResponse, error)

	// GetPIVCertificate retrieves the certificate stored in a specific PIV slot.
	// The certificate can be returned in PEM or DER format.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying backend, slot, and desired output format
	// Returns:
	//   - PIVGetCertificateResponse: Contains the certificate data and format
	//   - error: ErrNotFound, ErrUnavailable, ErrUnauthorized, ErrTimeout
	GetPIVCertificate(ctx context.Context, req *PIVGetCertificateRequest) (*PIVGetCertificateResponse, error)

	// StorePIVCertificate stores a certificate in a specific PIV slot.
	// This overwrites any existing certificate in the target slot.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying backend, slot, certificate data, and format
	// Returns:
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	StorePIVCertificate(ctx context.Context, req *PIVStoreCertificateRequest) error

	// DeletePIVCertificate removes the certificate from a specific PIV slot.
	// The key pair in the slot (if any) is not affected.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying backend and slot
	// Returns:
	//   - error: ErrNotFound, ErrUnavailable, ErrUnauthorized, ErrTimeout
	DeletePIVCertificate(ctx context.Context, req *PIVDeleteCertificateRequest) error

	// GeneratePIVKey generates a new key pair in a PIV slot with a self-signed
	// certificate. The existing key and certificate in the slot are replaced.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying backend, slot, algorithm, and subject
	// Returns:
	//   - PIVGenerateKeyResponse: Contains the self-signed certificate and public key
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	GeneratePIVKey(ctx context.Context, req *PIVGenerateKeyRequest) (*PIVGenerateKeyResponse, error)

	// GeneratePIVCSR generates a PKCS#10 Certificate Signing Request for the key
	// in the specified PIV slot. The CSR can be submitted to a CA for signing.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request specifying backend, slot, and subject
	// Returns:
	//   - PIVGenerateCSRResponse: Contains the PEM-encoded CSR
	//   - error: ErrNotFound, ErrUnavailable, ErrUnauthorized, ErrTimeout
	GeneratePIVCSR(ctx context.Context, req *PIVGenerateCSRRequest) (*PIVGenerateCSRResponse, error)
}

// =============================================================================
// PIV Service Request/Response Types
// =============================================================================

// PIVListSlotsRequest contains parameters for listing PIV slots.
type PIVListSlotsRequest struct {
	// Backend identifies the hardware or software backend to query.
	Backend string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVSlotStatus contains the status of a single PIV slot.
type PIVSlotStatus struct {
	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// Name is the human-readable name of the slot.
	Name string

	// Description provides details about the slot's intended purpose.
	Description string

	// HasCert indicates whether a certificate is present in the slot.
	HasCert bool

	// Subject is the certificate subject DN, empty if no certificate.
	Subject string

	// Issuer is the certificate issuer DN, empty if no certificate.
	Issuer string

	// Algorithm is the key algorithm (e.g., "RSA-2048", "ECDSA-P256").
	Algorithm string

	// KeySize is the key size in bits.
	KeySize int

	// NotAfter is the certificate expiration date in RFC 3339 format.
	NotAfter string

	// Fingerprint is the SHA-256 fingerprint of the certificate.
	Fingerprint string

	// Backend is the backend that contains the slot.
	Backend string
}

// PIVListSlotsResponse contains the list of PIV slots and their status.
type PIVListSlotsResponse struct {
	// Slots contains the status of each PIV slot in the backend.
	Slots []PIVSlotStatus
}

// PIVGetCertificateRequest contains parameters for retrieving a PIV certificate.
type PIVGetCertificateRequest struct {
	// Backend identifies the hardware or software backend.
	Backend string

	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// Format specifies the output format: "pem" or "der".
	Format string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVGetCertificateResponse contains the certificate data from a PIV slot.
type PIVGetCertificateResponse struct {
	// Slot is the PIV slot the certificate was retrieved from.
	Slot string

	// Certificate is the certificate data in the requested format.
	Certificate []byte

	// Format is the format of the certificate data ("pem" or "der").
	Format string
}

// PIVStoreCertificateRequest contains parameters for storing a PIV certificate.
type PIVStoreCertificateRequest struct {
	// Backend identifies the hardware or software backend.
	Backend string

	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// Certificate is the certificate data to store.
	Certificate []byte

	// Format specifies the certificate format: "pem" or "der".
	Format string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVDeleteCertificateRequest contains parameters for deleting a PIV certificate.
type PIVDeleteCertificateRequest struct {
	// Backend identifies the hardware or software backend.
	Backend string

	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVGenerateKeyRequest contains parameters for generating a PIV key pair.
type PIVGenerateKeyRequest struct {
	// Backend identifies the hardware or software backend.
	Backend string

	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// Algorithm specifies the key algorithm: "rsa2048", "rsa4096",
	// "ecdsap256", "ecdsap384", or "ed25519".
	Algorithm string

	// Subject is the distinguished name for the self-signed certificate.
	Subject string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVGenerateKeyResponse contains the result of PIV key generation.
type PIVGenerateKeyResponse struct {
	// Slot is the PIV slot where the key was generated.
	Slot string

	// Certificate is the PEM-encoded self-signed certificate.
	Certificate []byte

	// PublicKey is the PEM-encoded public key.
	PublicKey []byte
}

// PIVGenerateCSRRequest contains parameters for generating a PIV CSR.
type PIVGenerateCSRRequest struct {
	// Backend identifies the hardware or software backend.
	Backend string

	// Slot is the PIV slot identifier (e.g., "9a", "9c", "9d", "9e").
	Slot string

	// Subject is the distinguished name for the CSR subject.
	Subject string

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PIVGenerateCSRResponse contains the generated certificate signing request.
type PIVGenerateCSRResponse struct {
	// Slot is the PIV slot the CSR was generated for.
	Slot string

	// CSR is the PEM-encoded PKCS#10 certificate signing request.
	CSR []byte
}

// =============================================================================
// PIV Service Error Types
// =============================================================================

// PIVNotAvailableError indicates that the PIV service is not configured
// or the underlying hardware backend does not support PIV operations.
type PIVNotAvailableError struct{}

// Error implements the error interface.
func (e *PIVNotAvailableError) Error() string {
	return "PIV service not available"
}

// Is supports errors.Is matching by type.
func (e *PIVNotAvailableError) Is(target error) bool {
	_, ok := target.(*PIVNotAvailableError)
	return ok
}

// PIVSlotNotFoundError indicates that the specified PIV slot does not exist
// or does not contain a certificate.
type PIVSlotNotFoundError struct {
	Slot string
}

// Error implements the error interface.
func (e *PIVSlotNotFoundError) Error() string {
	return "PIV slot not found: " + e.Slot
}

// Is supports errors.Is matching by type.
func (e *PIVSlotNotFoundError) Is(target error) bool {
	_, ok := target.(*PIVSlotNotFoundError)
	return ok
}

// PIVInvalidSlotError indicates that the specified PIV slot identifier is invalid.
type PIVInvalidSlotError struct {
	Slot string
}

// Error implements the error interface.
func (e *PIVInvalidSlotError) Error() string {
	return "invalid PIV slot: " + e.Slot
}

// Is supports errors.Is matching by type.
func (e *PIVInvalidSlotError) Is(target error) bool {
	_, ok := target.(*PIVInvalidSlotError)
	return ok
}

// PIVKeyGenerationError indicates that key generation in a PIV slot failed.
type PIVKeyGenerationError struct {
	Slot  string
	Cause error
}

// Error implements the error interface.
func (e *PIVKeyGenerationError) Error() string {
	return "PIV key generation failed for slot " + e.Slot + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *PIVKeyGenerationError) Unwrap() error {
	return e.Cause
}

// PIVCertificateOperationError indicates a certificate operation in a PIV slot failed.
type PIVCertificateOperationError struct {
	Slot      string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *PIVCertificateOperationError) Error() string {
	return "PIV certificate " + e.Operation + " failed for slot " + e.Slot + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *PIVCertificateOperationError) Unwrap() error {
	return e.Cause
}
