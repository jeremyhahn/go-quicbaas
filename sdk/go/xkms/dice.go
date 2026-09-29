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

// dice.go re-exports go-xkms's TCG DICE device-identity surface.
//
// WHY THIS FILE EXISTS. go-trusted-platform's layer guard fails any import of
// github.com/jeremyhahn/go-xkms and names this package as the route. Without
// these aliases DICE verification is not merely inconvenient from Layer 5, it
// is unreachable: the capability is implemented in go-xkms, exposed through
// its SDK, and routed only by quicbaas. A consumer reaching for go-xkms
// directly is the thing the guard exists to stop.
//
// Every name here is an ALIAS, never a wrapper. A lookalike type would compile,
// pass this package's tests, and fail only where a consumer hands a value
// across the boundary -- which is why dice_reexport_test.go asserts
// assignability in both directions rather than merely that the names exist.
//
// The verification itself stays in go-xkms. quicbaas routes; it does not
// verify.

package xkms

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// TCG DICE device identity. A device with no clock cannot offer a timestamp
// that means anything, so freshness here is always the verifier's nonce,
// echoed by the device in the eat_nonce claim of the tcg-dice-UCCS-evidence
// extension -- the mechanism DICE Attestation Architecture section 6.1.4
// defines, so another DICE verifier reads it where it expects to.
type (
	// DICEEvidence is what a verified DICE chain established.
	DICEEvidence = xkmssdk.DICEEvidence

	// DICEVerifyOptions is what the relying party requires of the chain,
	// including the nonce it issued.
	DICEVerifyOptions = xkmssdk.DICEVerifyOptions

	// DICETcbInfo is the decoded DiceTcbInfo extension: the device's identity
	// and firmware measurements as the device stated them.
	DICETcbInfo = xkmssdk.DICETcbInfo

	// DICEFWID is one firmware measurement -- a hash algorithm and a digest.
	DICEFWID = xkmssdk.DICEFWID

	// DICEOperationalFlags carries notSecure and its siblings. A device on a
	// software root of trust sets notSecure, and a verifier that ignores it
	// has not read the evidence.
	DICEOperationalFlags = xkmssdk.DICEOperationalFlags

	// DICEUeid is the device's unique entity identifier.
	DICEUeid = xkmssdk.DICEUeid

	// DICEClaimsSet is the CWT claims set inside tcg-dice-UCCS-evidence,
	// where the nonce lives.
	DICEClaimsSet = xkmssdk.DICEClaimsSet
)

// The DICE entry points. DICEVerify is the one that renders a verdict; the
// rest read a certificate without judging it.
var (
	// DICEVerify verifies a DICE certificate chain against the options,
	// including the nonce. It is the verification entry point.
	DICEVerify = xkmssdk.DICEVerify

	// DICEChallenge extracts the nonce a certificate echoed, which is how a
	// verifier confirms the evidence answers the challenge it issued.
	DICEChallenge = xkmssdk.DICEChallenge

	// DICEUccsFromCertificate reads the UCCS evidence extension.
	DICEUccsFromCertificate = xkmssdk.DICEUccsFromCertificate

	// DICETcbInfoFromCertificate reads the DiceTcbInfo extension.
	DICETcbInfoFromCertificate = xkmssdk.DICETcbInfoFromCertificate

	// DICEUeidFromCertificate reads the tcg-dice-Ueid extension.
	DICEUeidFromCertificate = xkmssdk.DICEUeidFromCertificate

	// DICEHasExtensions reports whether a certificate carries DICE
	// extensions at all, which is how a caller tells a DICE leaf from any
	// other certificate before trying to read one.
	DICEHasExtensions = xkmssdk.DICEHasExtensions

	// DICECreateUccsEvidenceExtension, DICECreateTcbInfoExtension and
	// DICECreateUeidExtension build the extensions. A platform issuing a
	// DICE identity needs these; a platform only verifying one does not.
	DICECreateUccsEvidenceExtension = xkmssdk.DICECreateUccsEvidenceExtension
	DICECreateTcbInfoExtension      = xkmssdk.DICECreateTcbInfoExtension
	DICECreateUeidExtension         = xkmssdk.DICECreateUeidExtension
)

// The DICE error sentinels, so a consumer can errors.Is across the boundary
// rather than matching on message text. They are aliased individually and not
// collapsed into a single "DICE failed" value, because the distinction a
// verifier must act on -- a claim that was ABSENT versus one that was WRONG --
// is exactly the distinction that collapsing them destroys.
var (
	ErrDICENilCertificate   = xkmssdk.ErrDICENilCertificate
	ErrDICENilTcbInfo       = xkmssdk.ErrDICENilTcbInfo
	ErrDICENilClaimsSet     = xkmssdk.ErrDICENilClaimsSet
	ErrDICENilVerifyOptions = xkmssdk.ErrDICENilVerifyOptions

	ErrDICETcbInfoNotFound      = xkmssdk.ErrDICETcbInfoNotFound
	ErrDICEUeidNotFound         = xkmssdk.ErrDICEUeidNotFound
	ErrDICEUccsEvidenceNotFound = xkmssdk.ErrDICEUccsEvidenceNotFound

	ErrDICEMalformedTcbInfo      = xkmssdk.ErrDICEMalformedTcbInfo
	ErrDICEMalformedUeid         = xkmssdk.ErrDICEMalformedUeid
	ErrDICEMalformedFWID         = xkmssdk.ErrDICEMalformedFWID
	ErrDICEMalformedUccsEvidence = xkmssdk.ErrDICEMalformedUccsEvidence
	ErrDICEMalformedClaimsSet    = xkmssdk.ErrDICEMalformedClaimsSet
	ErrDICEMalformedNonceClaim   = xkmssdk.ErrDICEMalformedNonceClaim

	ErrDICEUnknownTcbInfoField    = xkmssdk.ErrDICEUnknownTcbInfoField
	ErrDICETcbInfoFieldOrder      = xkmssdk.ErrDICETcbInfoFieldOrder
	ErrDICENegativeTcbInfoValue   = xkmssdk.ErrDICENegativeTcbInfoValue
	ErrDICEUnknownOperationalFlag = xkmssdk.ErrDICEUnknownOperationalFlag
	ErrDICEEmptyUeid              = xkmssdk.ErrDICEEmptyUeid
	ErrDICEDuplicateClaim         = xkmssdk.ErrDICEDuplicateClaim

	// Freshness. A missing nonce and a wrong nonce are different failures:
	// the first is a device that did not answer the challenge, the second is
	// evidence replayed from a different exchange.
	ErrDICENonceClaimMissing = xkmssdk.ErrDICENonceClaimMissing
	ErrDICENonceClaimLength  = xkmssdk.ErrDICENonceClaimLength
	ErrDICENonceRequired     = xkmssdk.ErrDICENonceRequired
	ErrDICENonceMismatch     = xkmssdk.ErrDICENonceMismatch

	ErrDICEEmptyChain                    = xkmssdk.ErrDICEEmptyChain
	ErrDICEChainVerificationFailed       = xkmssdk.ErrDICEChainVerificationFailed
	ErrDICEUeidMismatch                  = xkmssdk.ErrDICEUeidMismatch
	ErrDICENoMeasurement                 = xkmssdk.ErrDICENoMeasurement
	ErrDICEOperationalStateNotPermitted  = xkmssdk.ErrDICEOperationalStateNotPermitted
	ErrDICECriticalEvidenceUnreadable    = xkmssdk.ErrDICECriticalEvidenceUnreadable
	ErrDICEUnsupportedKeyType            = xkmssdk.ErrDICEUnsupportedKeyType
	ErrDICEUnsupportedSignatureAlgorithm = xkmssdk.ErrDICEUnsupportedSignatureAlgorithm
)
