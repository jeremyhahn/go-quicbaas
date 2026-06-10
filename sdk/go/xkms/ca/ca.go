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
	CASubject                 = xkmssdk.TrustedCASubject
	CASubjectAlternativeNames = xkmssdk.TrustedCASubjectAlternativeNames
)

// Re-exported function values and helpers
var (
	NewCA                        = xkmssdk.NewCA
	DefaultMultiIdentityCAConfig = xkmssdk.DefaultMultiIdentityCAConfig
)
