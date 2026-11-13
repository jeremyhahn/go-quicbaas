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

// certificate_extensions.go implements the three higher-level certificate
// verbs the SDK surfaces through Security().CertificateExt(): CrossSign,
// Chain and IssueDeviceIdentity.
//
// The service layer owns validation, tenant scoping and error typing; every
// cryptographic operation is delegated to go-xkms through the narrow
// CertificateExtensionsCA capability, which the pkg/server caServiceAdapter
// satisfies by composing the xkms TrustedCA / TCGCA it already wraps. No
// certificate is built here.

package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// CA capability
// =============================================================================

// CertificateExtensionsCA is the narrow capability the certificate-extension
// verbs need from the composed Certificate Authority. The pkg/server
// caServiceAdapter implements it over the xkms TrustedCA / TCGCA it already
// wraps; CertificateService type-asserts its underlying caService to this
// interface and fails closed with ErrCertExtNotSupported when the assertion
// does not hold.
//
// Keeping the capability narrow (and primitive-typed) means the service layer
// never imports xkms certificate types, while the adapter side carries the
// compile-time assertions that pin these signatures to the upstream
// ca.TrustedCA / ca.TCGCA method sets.
type CertificateExtensionsCA interface {
	// CrossSignRoot re-certifies the subject and public key of a foreign root
	// certificate under the local CA and returns the PEM-encoded cross-signed
	// certificate.
	CrossSignRoot(ctx context.Context, foreignRootPEM []byte) ([]byte, error)

	// IssuerChainPEM resolves the issuer chain for certPEM, walking the
	// Authority Information Access extension, and returns the chain ordered
	// issuer-first and root-last. The leaf itself is not included.
	IssuerChainPEM(ctx context.Context, certPEM []byte) ([][]byte, error)

	// IssueDeviceIdentityCert issues a TCG device identity certificate
	// (EK, AK or IDevID) as selected by req.Type.
	IssueDeviceIdentityCert(ctx context.Context, req *DeviceIdentityIssuance) (*IssuedDeviceIdentity, error)
}

// =============================================================================
// Capability request/response types
// =============================================================================

// DeviceIdentityIssuance is the transport-neutral device-identity issuance
// request handed to CertificateExtensionsCA. It mirrors the SDK's
// DeviceIdentityRequest after validation and tenant resolution.
type DeviceIdentityIssuance struct {
	// Type selects the identity flavor: "ek", "ak" or "idevid".
	Type string

	// DeviceID uniquely identifies the device. It becomes the certificate
	// subject Common Name and the TCG PermanentIdentifier extension.
	DeviceID string

	// CSR is the TCG-CSR-IDEVID, either PEM-wrapped or raw packed bytes.
	// Required for the "idevid" flavor and ignored otherwise.
	CSR []byte

	// EKPub is the Endorsement Key public key, PEM- or DER-encoded PKIX.
	// Required for the "ek" flavor.
	EKPub []byte

	// AKPub is the Attestation Key public key, PEM- or DER-encoded PKIX.
	// Required for the "ak" flavor.
	AKPub []byte

	// QualifyingData optionally binds the certificate to a TPM quote.
	QualifyingData []byte

	// ValidityDays overrides the profile default validity period.
	ValidityDays int

	// TenantID scopes the issuance to a specific tenant.
	TenantID string
}

// IssuedDeviceIdentity is the certificate produced by a device-identity
// issuance.
type IssuedDeviceIdentity struct {
	CertificatePEM []byte
	ChainPEM       []byte
	SerialNumber   string
	NotBefore      time.Time
	NotAfter       time.Time
}

// =============================================================================
// Service request/response types
// =============================================================================

// CrossSignRequest asks the CA to cross-sign an external root certificate.
type CrossSignRequest struct {
	// ForeignRootPEM is the PEM-encoded external root certificate.
	ForeignRootPEM []byte `json:"foreign_root_pem"`

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string `json:"tenant_id,omitempty"`
}

// CrossSignResponse carries the cross-signed certificate.
type CrossSignResponse struct {
	// CrossSignedPEM is the PEM-encoded cross-signed certificate.
	CrossSignedPEM []byte `json:"cross_signed_pem"`
}

// CertificateChainRequest asks the CA to resolve a certificate's issuer chain.
type CertificateChainRequest struct {
	// CertificatePEM is the PEM-encoded certificate whose chain is resolved.
	CertificatePEM []byte `json:"certificate_pem"`

	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string `json:"tenant_id,omitempty"`
}

// CertificateChainResponse carries the resolved chain, ordered issuer-first
// and root-last.
type CertificateChainResponse struct {
	// ChainPEM holds one PEM-encoded certificate per chain element.
	ChainPEM [][]byte `json:"chain_pem"`
}

// IssueDeviceIdentityRequest asks the CA to issue a TCG device identity
// certificate. The JSON tags match the SDK wire shape field-for-field so the
// QUIC and embedded transports can round-trip the same bytes.
type IssueDeviceIdentityRequest struct {
	Type           string `json:"type"`
	DeviceID       string `json:"device_id"`
	CSRPEM         []byte `json:"csr_pem,omitempty"`
	EKPub          []byte `json:"ek_pub,omitempty"`
	AKPub          []byte `json:"ak_pub,omitempty"`
	QualifyingData []byte `json:"qualifying_data,omitempty"`
	ValidityDays   int    `json:"validity_days,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
}

// IssueDeviceIdentityResponse carries the issued device identity certificate.
type IssueDeviceIdentityResponse struct {
	CertificatePEM []byte    `json:"certificate_pem"`
	ChainPEM       []byte    `json:"chain_pem"`
	SerialNumber   string    `json:"serial_number"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
}

// =============================================================================
// Device identity types
// =============================================================================

// Device identity flavors accepted by IssueDeviceIdentity. They match the SDK
// DeviceIdentityType constants.
const (
	DeviceIdentityTypeEK     = "ek"
	DeviceIdentityTypeAK     = "ak"
	DeviceIdentityTypeIDevID = "idevid"
)

// deviceIdentityValidators maps each device identity flavor to the validation
// applied to its request, giving O(1) dispatch and a single place where a new
// flavor declares its required inputs.
var deviceIdentityValidators = map[string]func(*IssueDeviceIdentityRequest) error{
	DeviceIdentityTypeEK: func(req *IssueDeviceIdentityRequest) error {
		if len(req.EKPub) == 0 {
			return ErrCertExtMissingEKPub
		}
		return nil
	},
	DeviceIdentityTypeAK: func(req *IssueDeviceIdentityRequest) error {
		if len(req.AKPub) == 0 {
			return ErrCertExtMissingAKPub
		}
		return nil
	},
	DeviceIdentityTypeIDevID: func(req *IssueDeviceIdentityRequest) error {
		if len(req.CSRPEM) == 0 {
			return ErrCertExtMissingTCGCSR
		}
		return nil
	},
}

// =============================================================================
// Typed errors
// =============================================================================

var (
	// ErrCertExtNotSupported is returned when the configured CA service does
	// not implement CertificateExtensionsCA. This is the fail-closed path for
	// interface drift: a CA that no longer satisfies the capability must
	// refuse the operation rather than appear to succeed.
	ErrCertExtNotSupported = &errors.QBError{
		Code: errors.ErrUnimplemented,
		Op:   "CertificateService.Extensions",
		Err:  errCertExtNotSupported,
	}

	// ErrCertExtMissingForeignRoot is returned when CrossSign is called
	// without a foreign root certificate.
	ErrCertExtMissingForeignRoot = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.CrossSign",
		Err:  errCertExtMissingForeignRoot,
	}

	// ErrCertExtMissingCertificate is returned when Chain is called without a
	// certificate.
	ErrCertExtMissingCertificate = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.Chain",
		Err:  errCertExtMissingCertificate,
	}

	// ErrCertExtMissingDeviceID is returned when IssueDeviceIdentity is called
	// without a device identifier.
	ErrCertExtMissingDeviceID = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtMissingDeviceID,
	}

	// ErrCertExtUnknownIdentityType is returned when IssueDeviceIdentity is
	// called with a device identity flavor outside {ek, ak, idevid}.
	ErrCertExtUnknownIdentityType = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtUnknownIdentityType,
	}

	// ErrCertExtMissingEKPub is returned when an EK certificate is requested
	// without the endorsement key public key.
	ErrCertExtMissingEKPub = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtMissingEKPub,
	}

	// ErrCertExtMissingAKPub is returned when an AK certificate is requested
	// without the attestation key public key.
	ErrCertExtMissingAKPub = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtMissingAKPub,
	}

	// ErrCertExtMissingTCGCSR is returned when an IDevID certificate is
	// requested without a TCG-CSR-IDEVID.
	ErrCertExtMissingTCGCSR = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtMissingTCGCSR,
	}

	// ErrCertExtDeviceIdentityTenantUnsupported is returned when a caller
	// outside the system tenant asks for a device identity certificate.
	// go-xkms records no issuing tenant on EK, AK or IDevID certificates, so
	// the tenant could never revoke one. The refusal lifts once go-xkms
	// #1764 is pinned.
	ErrCertExtDeviceIdentityTenantUnsupported = &errors.QBError{
		Code: errors.ErrUnimplemented,
		Op:   "CertificateService.IssueDeviceIdentity",
		Err:  errCertExtDeviceIdentityTenantUnsupported,
	}
)

var (
	errCertExtNotSupported = stderrors.New(
		"CA service does not implement the certificate extension capability")
	errCertExtMissingForeignRoot  = stderrors.New("foreign root PEM is required")
	errCertExtMissingCertificate  = stderrors.New("certificate PEM is required")
	errCertExtMissingDeviceID     = stderrors.New("device ID is required")
	errCertExtUnknownIdentityType = stderrors.New(
		"device identity type must be one of: ek, ak, idevid")
	errCertExtMissingEKPub  = stderrors.New("EK public key is required for EK certificates")
	errCertExtMissingAKPub  = stderrors.New("AK public key is required for AK certificates")
	errCertExtMissingTCGCSR = stderrors.New("TCG-CSR-IDEVID is required for IDevID certificates")

	errCertExtDeviceIdentityTenantUnsupported = stderrors.New(
		"device identity certificates are issued to the system tenant only")
)

// =============================================================================
// CertificateService implementation
// =============================================================================

// extensionsCA resolves the certificate-extension capability from the
// configured CA service, returning ErrCertExtNotSupported when the backing
// adapter does not implement it.
func (s *certificateService) extensionsCA() (CertificateExtensionsCA, error) {
	ca, ok := s.caService.(CertificateExtensionsCA)
	if !ok {
		return nil, ErrCertExtNotSupported
	}
	return ca, nil
}

// CrossSign cross-signs an external root certificate with the local CA and
// returns the PEM-encoded cross-signed certificate.
func (s *certificateService) CrossSign(ctx context.Context, req *CrossSignRequest) (*CrossSignResponse, error) {
	if req == nil {
		return nil, ErrCertNilRequest
	}
	if len(req.ForeignRootPEM) == 0 {
		return nil, ErrCertExtMissingForeignRoot
	}
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	ca, err := s.extensionsCA()
	if err != nil {
		return nil, err
	}

	crossSignedPEM, err := ca.CrossSignRoot(ctx, req.ForeignRootPEM)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.CrossSign",
			Err:  err,
		}
	}
	return &CrossSignResponse{CrossSignedPEM: crossSignedPEM}, nil
}

// Chain resolves the issuer chain for the supplied certificate, walking the
// Authority Information Access extension. The chain is ordered issuer-first
// and root-last and excludes the leaf.
func (s *certificateService) Chain(ctx context.Context, req *CertificateChainRequest) (*CertificateChainResponse, error) {
	if req == nil {
		return nil, ErrCertNilRequest
	}
	if len(req.CertificatePEM) == 0 {
		return nil, ErrCertExtMissingCertificate
	}
	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	ca, err := s.extensionsCA()
	if err != nil {
		return nil, err
	}

	chain, err := ca.IssuerChainPEM(ctx, req.CertificatePEM)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.Chain",
			Err:  err,
		}
	}
	return &CertificateChainResponse{ChainPEM: chain}, nil
}

// IssueDeviceIdentity issues a TCG device identity certificate (EK, AK or
// IDevID) for the requested device.
func (s *certificateService) IssueDeviceIdentity(ctx context.Context, req *IssueDeviceIdentityRequest) (*IssueDeviceIdentityResponse, error) {
	if req == nil {
		return nil, ErrCertNilRequest
	}
	if req.DeviceID == "" {
		return nil, ErrCertExtMissingDeviceID
	}
	validate, known := deviceIdentityValidators[req.Type]
	if !known {
		return nil, ErrCertExtUnknownIdentityType
	}
	if err := validate(req); err != nil {
		return nil, err
	}

	// A device identity is issued in the caller's own tenant only. A request
	// naming another tenant is refused, the system tenant included: go-qrdb
	// v0.28 lets no caller mint another tenant's certificates.
	target, err := tenantctx.Target(ctx, "CertificateService.IssueDeviceIdentity", req.TenantID)
	if err != nil {
		return nil, err
	}
	// Until go-xkms records the issuing tenant on device identity
	// certificates (go-xkms #1764), a tenant could not revoke one issued to
	// it, so only the system tenant may issue them.
	if !tenantctx.IsSystem(target) {
		return nil, ErrCertExtDeviceIdentityTenantUnsupported
	}

	ca, err := s.extensionsCA()
	if err != nil {
		return nil, err
	}

	issued, err := ca.IssueDeviceIdentityCert(ctx, &DeviceIdentityIssuance{
		Type:           req.Type,
		DeviceID:       req.DeviceID,
		CSR:            req.CSRPEM,
		EKPub:          req.EKPub,
		AKPub:          req.AKPub,
		QualifyingData: req.QualifyingData,
		ValidityDays:   req.ValidityDays,
		// The CA stamps this tenant into the certificate and refuses none,
		// so the literal system tenant is named, not the services' ""
		// platform scope prepareTenantContext maps it to.
		TenantID: target,
	})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.IssueDeviceIdentity",
			Err:  err,
		}
	}
	if issued == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CertificateService.IssueDeviceIdentity",
			Err:  errCertExtNotSupported,
		}
	}

	return &IssueDeviceIdentityResponse{
		CertificatePEM: issued.CertificatePEM,
		ChainPEM:       issued.ChainPEM,
		SerialNumber:   issued.SerialNumber,
		NotBefore:      issued.NotBefore,
		NotAfter:       issued.NotAfter,
	}, nil
}
