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

// profiles.go re-exports go-xkms's certificate-profile selection: the
// registry names a caller puts on SignOptions.Profile, the ipsec-vtep and
// x509-svid profiles (go-xkms#1385), name-constrained subordinate CAs for a
// caller-held key, and the tp-attestationEvidence extension an ipsec-vtep
// certificate carries.
//
// Every name is an alias of the go-xkms SDK name, never a wrapper. The
// profiles, the name-constraint checks and the extension encoding all stay in
// go-xkms.

package ca

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// Registry names of the built-in certificate profiles, for
// SignOptions.Profile.
const (
	ProfileNameCA                 = xkmssdk.ProfileNameCA
	ProfileNameACMELeaf           = xkmssdk.ProfileNameACMELeaf
	ProfileNameTCGEndorsementKey  = xkmssdk.ProfileNameTCGEndorsementKey
	ProfileNameTCGAttestationKey  = xkmssdk.ProfileNameTCGAttestationKey
	ProfileNameTCGInitialAK       = xkmssdk.ProfileNameTCGInitialAK
	ProfileNameTCGDeviceIdentity  = xkmssdk.ProfileNameTCGDeviceIdentity
	ProfileNameFIDO2Attestation   = xkmssdk.ProfileNameFIDO2Attestation
	ProfileNameDICEDeviceIdentity = xkmssdk.ProfileNameDICEDeviceIdentity

	// ProfileNameIPsecVTEP issues IKEv2 tunnel-endpoint certificates:
	// id-kp-ipsecIKE, caller-supplied IP SANs, 30-day default lifetime.
	ProfileNameIPsecVTEP = xkmssdk.ProfileNameIPsecVTEP

	// ProfileNameX509SVID issues SPIFFE X.509-SVID leaves: one
	// caller-supplied SPIFFE URI SAN, serverAuth+clientAuth, 1-hour default
	// lifetime.
	ProfileNameX509SVID = xkmssdk.ProfileNameX509SVID
)

// DefaultSubordinateLifetime is the lifetime of a subordinate CA whose
// request names none.
const DefaultSubordinateLifetime = xkmssdk.DefaultSubordinateLifetime

type (
	// ProfileProvider is a certificate profile.
	ProfileProvider = xkmssdk.ProfileProvider

	// ProfileRegistry manages certificate profile registrations.
	ProfileRegistry = xkmssdk.ProfileRegistry

	// NameConstraints is the RFC 5280 name constraints of a subordinate CA.
	// URI constraints bind only the host (the SPIFFE trust domain), never a
	// path.
	NameConstraints = xkmssdk.NameConstraints

	// SubordinateCARequest describes a name-constrained subordinate CA for a
	// caller-held key.
	SubordinateCARequest = xkmssdk.SubordinateCARequest

	// SubordinateIssuer is implemented by a CA that issues name-constrained
	// subordinate CAs; assert `trustedCA.(SubordinateIssuer)`.
	SubordinateIssuer = xkmssdk.SubordinateIssuer

	// CallerSANProfile, LifetimeBoundedProfile and TemplateValidator are the
	// optional contracts a custom ProfileProvider may implement.
	CallerSANProfile       = xkmssdk.CallerSANProfile
	LifetimeBoundedProfile = xkmssdk.LifetimeBoundedProfile
	TemplateValidator      = xkmssdk.TemplateValidator

	// AttestationEvidence is the content of the tp-attestationEvidence
	// extension a caller attaches to an ipsec-vtep certificate.
	AttestationEvidence = xkmssdk.AttestationEvidence
)

var (
	// OIDIPsecIKE is id-kp-ipsecIKE (1.3.6.1.5.5.7.3.17).
	OIDIPsecIKE = xkmssdk.OIDIPsecIKE

	// OIDAttestationEvidence identifies the tp-attestationEvidence extension
	// (1.3.6.1.4.1.29377.101.23).
	OIDAttestationEvidence = xkmssdk.OIDAttestationEvidence

	// NewDefaultProfileRegistry returns the registry of built-in profiles.
	NewDefaultProfileRegistry = xkmssdk.NewDefaultProfileRegistry

	// NewIPsecVTEPProfile creates the ipsec-vtep profile.
	NewIPsecVTEPProfile = xkmssdk.NewIPsecVTEPProfile

	// NewX509SVIDProfile creates the x509-svid profile.
	NewX509SVIDProfile = xkmssdk.NewX509SVIDProfile

	// CreateAttestationEvidenceExtension encodes evidence for
	// SignOptions.ExtraExtensions.
	CreateAttestationEvidenceExtension = xkmssdk.CreateAttestationEvidenceExtension

	// ParseAttestationEvidenceExtension reads the evidence a certificate
	// carries.
	ParseAttestationEvidenceExtension = xkmssdk.ParseAttestationEvidenceExtension
)

// Errors returned by caller-named profiles, lifetimes, name constraints and
// the evidence extension, matched with errors.Is.
var (
	ErrCACallerSANsRequired         = xkmssdk.ErrCACallerSANsRequired
	ErrCATenantSPIFFEWithCallerSANs = xkmssdk.ErrCATenantSPIFFEWithCallerSANs
	ErrCAInvalidSAN                 = xkmssdk.ErrCAInvalidSAN
	ErrCAInvalidValidity            = xkmssdk.ErrCAInvalidValidity
	ErrCAValidityExceedsProfile     = xkmssdk.ErrCAValidityExceedsProfile
	ErrCAValidityExceedsIssuer      = xkmssdk.ErrCAValidityExceedsIssuer
	ErrCAUnsupportedProfileKey      = xkmssdk.ErrCAUnsupportedProfileKey
	ErrCAInvalidNameConstraints     = xkmssdk.ErrCAInvalidNameConstraints
	ErrCANameConstraintViolation    = xkmssdk.ErrCANameConstraintViolation
	ErrCAInvalidAttestationEvidence = xkmssdk.ErrCAInvalidAttestationEvidence
)
