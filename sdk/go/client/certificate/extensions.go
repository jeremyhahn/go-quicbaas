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

// extensions.go declares the wire types for the three certificate-extension
// verbs surfaced by Security().CertificateExt(): CrossSign, Chain and
// IssueDeviceIdentity.
//
// They live in this leaf package — rather than alongside the SDK interface in
// sdk/go/services_security.go — because every transport implementation
// (embedded / REST / gRPC / QUIC / MCP / Unix) must construct the exact
// response value the SDK type-asserts on, and the root sdk/go package imports
// all six transport packages. A type declared in the root is therefore
// unreachable from any transport. The root declarations are aliases of the
// types below, which is the same arrangement already used by the Trust surface
// (sdk/go/client/trust).

package certificate

import "time"

// CrossSignRequest asks the server to cross-sign an external root certificate
// with the local CA.
type CrossSignRequest struct {
	// ForeignRootPEM is the PEM-encoded external root certificate whose
	// subject and public key are re-certified under the local CA.
	ForeignRootPEM []byte `json:"foreign_root_pem"`
}

// CrossSignResponse carries the cross-signed certificate.
type CrossSignResponse struct {
	// CrossSignedPEM is the PEM-encoded cross-signed certificate.
	CrossSignedPEM []byte `json:"cross_signed_pem"`
}

// ChainRequest asks the server to resolve the issuer chain for a certificate.
type ChainRequest struct {
	// CertificatePEM is the PEM-encoded certificate whose chain is resolved.
	CertificatePEM []byte `json:"certificate_pem"`
}

// ChainResponse carries the resolved chain, ordered issuer-first and
// root-last. The leaf itself is not included.
type ChainResponse struct {
	// ChainPEM holds one PEM-encoded certificate per chain element.
	ChainPEM [][]byte `json:"chain_pem"`
}

// DeviceIdentityType selects the TCG identity certificate flavor.
type DeviceIdentityType string

const (
	// DeviceIdentityEK issues an Endorsement Key certificate.
	DeviceIdentityEK DeviceIdentityType = "ek"

	// DeviceIdentityAK issues an Attestation Key certificate.
	DeviceIdentityAK DeviceIdentityType = "ak"

	// DeviceIdentityIDevID issues an IEEE 802.1AR IDevID certificate.
	DeviceIdentityIDevID DeviceIdentityType = "idevid"
)

// DeviceIdentityRequest describes a TCG device identity issuance request.
type DeviceIdentityRequest struct {
	// Type selects the identity flavor (EK, AK, IDevID).
	Type DeviceIdentityType `json:"type"`

	// DeviceID uniquely identifies the device requesting identity.
	DeviceID string `json:"device_id"`

	// CSRPEM is the PEM-encoded CSR (TCG-CSR-IDEVID for IDevID).
	CSRPEM []byte `json:"csr_pem,omitempty"`

	// EKPub is the EK public key PEM (required for EK/AK issuance).
	EKPub []byte `json:"ek_pub,omitempty"`

	// AKPub is the AK public key PEM (required for AK certificates).
	AKPub []byte `json:"ak_pub,omitempty"`

	// QualifyingData optionally binds the certificate to a TPM quote.
	QualifyingData []byte `json:"qualifying_data,omitempty"`

	// ValidityDays overrides the profile default validity period.
	ValidityDays int `json:"validity_days,omitempty"`

	// TenantID scopes the issuance to a specific tenant.
	TenantID string `json:"tenant_id,omitempty"`
}

// DeviceIdentityResponse is the issued device identity certificate.
type DeviceIdentityResponse struct {
	CertificatePEM []byte    `json:"certificate_pem"`
	ChainPEM       []byte    `json:"chain_pem"`
	SerialNumber   string    `json:"serial_number"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
}
