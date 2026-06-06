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
	xkmsca "github.com/jeremyhahn/go-xkms/pkg/ca"
)

// Type aliases
type (
	IssuedCertificate      = xkmsca.IssuedCertificate
	Params                 = xkmsca.Params
	RevokeOptions          = xkmsca.RevokeOptions
	SignOptions            = xkmsca.SignOptions
	TCGCA                  = xkmsca.TCGCA
	TrustedCA              = xkmsca.TrustedCA
	CertStoreFactoryFunc   = xkmsca.CertStoreFactoryFunc
	KeyStoreFactoryFunc    = xkmsca.KeyStoreFactoryFunc
	ObjectStoreFactoryFunc = xkmsca.ObjectStoreFactoryFunc

	// CA-prefixed aliases for the canonical CA surface. These previously
	// lived only in the ACME SDK layer (sdk/go/acme), which made consumers
	// import the acme package just to name a CA type. They are plain CA
	// types from go-xkms/pkg/ca and belong here so the CA lives in one place.
	CACertificateAuthority    = xkmsca.TCGCA
	CACertificateRequest      = xkmsca.CertificateRequest
	CASubject                 = xkmsca.Subject
	CASubjectAlternativeNames = xkmsca.SubjectAlternativeNames
)

// Re-exported function values and helpers
var (
	NewCA                        = xkmsca.NewCA
	DefaultMultiIdentityCAConfig = xkmsca.DefaultMultiIdentityCAConfig
)
