// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package ca re-exports the go-xkms CA implementation surface
// (TrustedCA, TCGCA, certificate stores, key/object store
// factories, signing/revocation options) so trusted-platform's
// CA adapter and gRPC handlers depend on go-quicbaas alone.

package ca

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
)

// Type aliases
type (
	IssuedCertificate      = xkmssdk.TrustedCAIssuedCertificate
	Params                 = xkmssdk.TrustedCAParams
	RevokeOptions          = xkmssdk.RevokeOptions
	SignOptions            = xkmssdk.TrustedCASignOptions
	TCGCA                  = xkmssdk.TCGCA
	TrustedCA              = xkmssdk.TrustedCA
	CertStoreFactoryFunc   = xkmssdk.TrustedCACertStoreFactoryFunc
	KeyStoreFactoryFunc    = xkmssdk.TrustedCAKeyStoreFactoryFunc
	ObjectStoreFactoryFunc = xkmssdk.TrustedCAObjectStoreFactoryFunc

	// CA-prefixed aliases for the canonical CA surface. These previously
	// lived only in the ACME SDK layer (sdk/go/acme), which made consumers
	// import the acme package just to name a CA type. They are plain CA
	// types from go-xkms/pkg/ca and belong here so the CA lives in one place.
	CACertificateAuthority    = xkmssdk.TCGCA
	CACertificateRequest      = xkmssdk.TrustedCACertificateRequest
	CAConfig                  = xkmssdk.TrustedCAConfig
	CAIdentity                = xkmssdk.TrustedCAIdentity
	CASubject                 = xkmssdk.TrustedCASubject
	CASubjectAlternativeNames = xkmssdk.TrustedCASubjectAlternativeNames

	// TCGEnrollmentResult carries the outcome of a TCG-CSR-IDEVID
	// enrollment performed by the CA (issued certificate plus the
	// activation credential blobs returned to the enrolling device).
	TCGEnrollmentResult = xkmssdk.TCGEnrollmentResult

	// TCGNetworkZone represents the platform network zone assigned to a
	// certificate subject (extension 1.3.6.1.4.1.29377.101.21), evaluated by
	// the SDN policy engine at enforcement time.
	TCGNetworkZone = xkmssdk.TCGNetworkZone

	// Per-key multi-keystore CA surface. CAKeystoresConfig declares the
	// per-backend keystore configuration for one CA identity and
	// BackendForStoreFunc resolves an xkms backend per (CN, StoreType) so a
	// TPM2/PKCS#11-bound CA signing key lands in the configured backend. Pure
	// type aliases re-exported from go-xkms.
	// CAIAKCertificateReader returns the Initial Attestation Key certificate
	// the CA issued for a device, addressed by (model, serial). The IAK is
	// stored under its own common name (CAIAKCommonName) so the IDevID record
	// for the same device no longer overwrites it (go-xkms#371).
	CAIAKCertificateReader = xkmsacme.CAIAKCertificateReader

	CAKeystoresConfig        = xkmssdk.TrustedCAKeystoresConfig
	CAKeystoreSoftwareConfig = xkmssdk.TrustedCAKeystoreSoftwareConfig
	CAKeystoreTPM2Config     = xkmssdk.TrustedCAKeystoreTPM2Config
	CAKeystorePKCS11Config   = xkmssdk.TrustedCAKeystorePKCS11Config
	BackendForStoreFunc      = xkmssdk.TrustedCABackendForStoreFunc
)

// Re-exported function values and helpers
var (
	NewCA = xkmssdk.NewCA

	// NewParentCA creates only the root TrustedCA from Params.Identity[0].
	// The returned CA is not initialized; callers invoke Init() or Load().
	NewParentCA = xkmssdk.NewParentCA

	// NewIntermediateCA creates the intermediate TrustedCA from
	// Params.Identity[1] signed by the given parent. The parent must be
	// initialized.
	NewIntermediateCA = xkmssdk.NewIntermediateCA

	DefaultMultiIdentityCAConfig = xkmssdk.DefaultMultiIdentityCAConfig

	// CAIAKCommonName returns the subject common name ("iak-<model>-<serial>")
	// an Initial Attestation Key certificate is issued and stored under.
	CAIAKCommonName = xkmsacme.CAIAKCommonName

	// OIDTPNetworkZone identifies the network zone extension
	// (1.3.6.1.4.1.29377.101.21).
	OIDTPNetworkZone = xkmssdk.OIDTPNetworkZone

	// TCG certificate-policy OIDs (TCG TPM 2.0 Keys for Device Identity and
	// Attestation v1 r12 §8.2). A relying party MUST check
	// tcg-cap-verifiedTPMRestricted on an IAK/LAK certificate before trusting
	// an attestation; the platform is that relying party and reaches the arc
	// only through this package. These are the go-xkms values, not copies:
	// go-xkms/pkg/ca owns the constants and its SDK re-exports them.

	// OIDTCGKpAIKCertificate is tcg-kp-AIKCertificate (2.23.133.8.3), the
	// extended key usage stamped on attestation-key certificates.
	OIDTCGKpAIKCertificate = xkmssdk.OIDTCGKpAIKCertificate

	// OIDTCGVerifiedTPMResidency is tcg-cap-verifiedTPMResidency
	// (2.23.133.11.1.1): the CA verified the key resides in a TPM.
	OIDTCGVerifiedTPMResidency = xkmssdk.OIDTCGVerifiedTPMResidency

	// OIDTCGVerifiedTPMFixed is tcg-cap-verifiedTPMFixed (2.23.133.11.1.2):
	// the CA verified the key is non-migratable (fixedTPM).
	OIDTCGVerifiedTPMFixed = xkmssdk.OIDTCGVerifiedTPMFixed

	// OIDTCGVerifiedTPMRestricted is tcg-cap-verifiedTPMRestricted
	// (2.23.133.11.1.3): the CA verified the key is a restricted signing key,
	// which is what makes its quotes attestations rather than signatures.
	OIDTCGVerifiedTPMRestricted = xkmssdk.OIDTCGVerifiedTPMRestricted

	// TCGVerifiedTPMResidencyPolicy, TCGVerifiedTPMFixedPolicy and
	// TCGVerifiedTPMRestrictedPolicy are the same three arcs as x509.OID
	// values, the form x509.Certificate.Policies carries (Go 1.24+ no longer
	// marshals PolicyIdentifiers by default).
	TCGVerifiedTPMResidencyPolicy  = xkmssdk.TCGVerifiedTPMResidencyPolicy
	TCGVerifiedTPMFixedPolicy      = xkmssdk.TCGVerifiedTPMFixedPolicy
	TCGVerifiedTPMRestrictedPolicy = xkmssdk.TCGVerifiedTPMRestrictedPolicy

	// CreateNetworkZoneExtension creates a network zone X.509 extension.
	CreateNetworkZoneExtension = xkmssdk.CreateNetworkZoneExtension

	// ParseNetworkZoneExtension parses a network zone X.509 extension.
	ParseNetworkZoneExtension = xkmssdk.ParseNetworkZoneExtension
)
