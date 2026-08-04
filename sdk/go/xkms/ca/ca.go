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
	CAKeystoresConfig        = xkmssdk.TrustedCAKeystoresConfig
	CAKeystoreSoftwareConfig = xkmssdk.TrustedCAKeystoreSoftwareConfig
	CAKeystoreTPM2Config     = xkmssdk.TrustedCAKeystoreTPM2Config
	CAKeystorePKCS11Config   = xkmssdk.TrustedCAKeystorePKCS11Config
	BackendForStoreFunc      = xkmssdk.TrustedCABackendForStoreFunc
)

// Re-exported function values and helpers
var (
	NewCA                        = xkmssdk.NewCA
	DefaultMultiIdentityCAConfig = xkmssdk.DefaultMultiIdentityCAConfig

	// OIDTPNetworkZone identifies the network zone extension
	// (1.3.6.1.4.1.29377.101.21).
	OIDTPNetworkZone = xkmssdk.OIDTPNetworkZone

	// CreateNetworkZoneExtension creates a network zone X.509 extension.
	CreateNetworkZoneExtension = xkmssdk.CreateNetworkZoneExtension

	// ParseNetworkZoneExtension parses a network zone X.509 extension.
	ParseNetworkZoneExtension = xkmssdk.ParseNetworkZoneExtension
)
