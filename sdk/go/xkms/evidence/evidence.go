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

// Package evidence re-exports go-xkms's sdk/go/evidence: the RFC 9334
// evidence-level attestation primitives -- TPM2_Quote parsing and
// verification, attestation-key and endorsement-key certificate checks, TCG
// PC Client event-log parsing and replay, TPM2_Certify verification, IAK and
// IDevID certificate checks, and signed reference values (a COSE_Sign1 CoRIM,
// VerifySignedCoRIM).
//
// These are primitives. Appraisal of evidence against reference values is
// the Verifier's job (Security().Attestation()), not this package's.
//
// Every name here is an alias, never a wrapper, so a value produced by
// go-xkms is used here without conversion and errors.Is matches the upstream
// sentinels.
package evidence

import xkmsevidence "github.com/jeremyhahn/go-xkms/sdk/go/evidence"

// Failure classification.
type (
	// Class says what kind of failure an *Error reports.
	Class = xkmsevidence.Class
	// Error is the failure of one evidence check.
	Error = xkmsevidence.Error
	// EventLogError reports an event log that does not parse.
	EventLogError = xkmsevidence.EventLogError
)

// Failure classes.
const (
	ClassForged = xkmsevidence.ClassForged
	ClassAbsent = xkmsevidence.ClassAbsent
)

// Check names carried by *Error.
const (
	CheckQuote          = xkmsevidence.CheckQuote
	CheckSignature      = xkmsevidence.CheckSignature
	CheckQualifyingData = xkmsevidence.CheckQualifyingData
	CheckPCRDigest      = xkmsevidence.CheckPCRDigest
	CheckBaseline       = xkmsevidence.CheckBaseline
	CheckAKCertificate  = xkmsevidence.CheckAKCertificate
	CheckEKCertificate  = xkmsevidence.CheckEKCertificate
	CheckEventLog       = xkmsevidence.CheckEventLog
)

// Sentinels.
var (
	ErrForged            = xkmsevidence.ErrForged
	ErrAbsent            = xkmsevidence.ErrAbsent
	ErrEventLogMalformed = xkmsevidence.ErrEventLogMalformed
)

// TPM2_Quote.
type (
	Quote      = xkmsevidence.Quote
	QuoteInput = xkmsevidence.QuoteInput
	ClockInfo  = xkmsevidence.ClockInfo
)

var (
	// ParseQuote parses a TPMS_ATTEST of type TPM_ST_ATTEST_QUOTE.
	ParseQuote = xkmsevidence.ParseQuote
	// VerifyQuote runs parse, signature, qualifying-data and PCR-digest checks.
	VerifyQuote = xkmsevidence.VerifyQuote
	// ComparePCRs compares proven PCR values with reference values.
	ComparePCRs = xkmsevidence.ComparePCRs
	// BankDigestSize returns the register size of an accepted PCR bank.
	BankDigestSize = xkmsevidence.BankDigestSize
	// FormatIndices renders PCR indices as "[0,1,7]".
	FormatIndices = xkmsevidence.FormatIndices
)

// Attestation-key and endorsement-key certificates.
type (
	AKCertificateOptions = xkmsevidence.AKCertificateOptions
	EKCertificateOptions = xkmsevidence.EKCertificateOptions
)

var (
	OIDTCGVerifiedTPMRestricted       = xkmsevidence.OIDTCGVerifiedTPMRestricted
	OIDTCGVerifiedTPMRestrictedPolicy = xkmsevidence.OIDTCGVerifiedTPMRestrictedPolicy
	OIDTCGKpEKCertificate             = xkmsevidence.OIDTCGKpEKCertificate

	ParseCertificatesPEM         = xkmsevidence.ParseCertificatesPEM
	VerifyAKCertificate          = xkmsevidence.VerifyAKCertificate
	CarriesVerifiedTPMRestricted = xkmsevidence.CarriesVerifiedTPMRestricted
	ParseEKCertificateChain      = xkmsevidence.ParseEKCertificateChain
	VerifyEKCertificate          = xkmsevidence.VerifyEKCertificate
	HasEKCertificateEKU          = xkmsevidence.HasEKCertificateEKU
	StripEKCertificateQuirks     = xkmsevidence.StripEKCertificateQuirks
)

// TCG PC Client crypto-agile event logs.
type (
	EventLog = xkmsevidence.EventLog
	Event    = xkmsevidence.Event
	SpecID   = xkmsevidence.SpecID
)

// Event log limits and event types.
const (
	EventNoAction    = xkmsevidence.EventNoAction
	MaxEventDataSize = xkmsevidence.MaxEventDataSize
	MaxEvents        = xkmsevidence.MaxEvents
	MaxAlgorithms    = xkmsevidence.MaxAlgorithms
)

var (
	ParseEventLog  = xkmsevidence.ParseEventLog
	VerifyEventLog = xkmsevidence.VerifyEventLog
	ResetValue     = xkmsevidence.ResetValue
)

// TPM2_Certify verification (pkg/tpm2).
type (
	CertifyVerification  = xkmsevidence.CertifyVerification
	VerifyCertifyOptions = xkmsevidence.VerifyCertifyOptions
)

var (
	VerifyCertify              = xkmsevidence.VerifyCertify
	VerifyCertifyByCertificate = xkmsevidence.VerifyCertifyByCertificate
	VerifyCertifyRaw           = xkmsevidence.VerifyCertifyRaw

	ErrCertifyMalformed              = xkmsevidence.ErrCertifyMalformed
	ErrCertifyNotTPMGenerated        = xkmsevidence.ErrCertifyNotTPMGenerated
	ErrCertifyWrongType              = xkmsevidence.ErrCertifyWrongType
	ErrCertifySignature              = xkmsevidence.ErrCertifySignature
	ErrCertifyNameMismatch           = xkmsevidence.ErrCertifyNameMismatch
	ErrCertifyQualifyingDataMismatch = xkmsevidence.ErrCertifyQualifyingDataMismatch
	ErrCertifyAttesterUnrestricted   = xkmsevidence.ErrCertifyAttesterUnrestricted
)

// IAK and IDevID certificate checks (pkg/attestation/peer).
type DevIDPolicy = xkmsevidence.DevIDPolicy

var (
	VerifyIAK    = xkmsevidence.VerifyIAK
	VerifyIDevID = xkmsevidence.VerifyIDevID
	IsIAK        = xkmsevidence.IsIAK
	IsIDevID     = xkmsevidence.IsIDevID
)
