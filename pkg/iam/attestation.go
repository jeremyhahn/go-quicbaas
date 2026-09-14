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

package iam

import (
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Attestation vocabulary
//
// WHAT THIS FILE EXISTS TO PREVENT
//
// A credential record that says "fido2" or "piv" names a credential KIND. It
// says nothing about whether the private key lives in a security key or in a
// browser profile, and until this file every path that cared about the
// difference simply asserted it: the PIV login handler and the self-service
// registration handler both passed the literal string "hardware" into token
// issuance without anything having proven hardware. A software authenticator
// and a YubiKey produced byte-identical claims.
//
// The fix is to make the assertion unforgeable BY CONSTRUCTION rather than by
// convention. AttestationResult carries only unexported fields and is produced
// only by the verifiers in this package (webauthn_attestation.go for FIDO2,
// piv_attestation.go for smartcards). Package rest — and every other caller
// outside pkg/iam — can hold one and read it, but cannot mint one. There is no
// spelling of `&AttestationResult{level: AttestLevelHardware}` available to
// them, so "claims hardware" and "proved hardware" are the same statement.
// =============================================================================

// AttestLevel is the provenance a ceremony actually PROVED about the key that
// authenticated, as distinct from the credential kind it was enrolled under.
// It is the value carried in the `attest_level` token claim and recorded on the
// stored auth factor.
type AttestLevel string

const (
	// AttestLevelNone means no attestation statement was conveyed at all. The
	// authenticator asserted nothing about itself, so nothing about the key's
	// storage is known. This is what a `none` conveyance ceremony yields.
	AttestLevelNone AttestLevel = AttestLevel(qrdbsdk.AttestationLevelNone)

	// AttestLevelSoftware means an attestation statement was conveyed and
	// verified, but it was SELF-signed — signed by the very credential key it
	// describes (WebAuthn "basic_surrogate", a self-signed PIV slot
	// certificate). A self-signature proves possession of the key and nothing
	// else: any software authenticator can produce one. It is deliberately NOT
	// a weaker grade of hardware evidence; it is an absence of hardware
	// evidence that happens to be cryptographically well-formed.
	AttestLevelSoftware AttestLevel = AttestLevel(qrdbsdk.AttestationLevelSoftware)

	// AttestLevelHardware means the attestation statement chained to a
	// manufacturer trust anchor the operator configured — a batch attestation
	// certificate (WebAuthn "basic_full" / "attca" / "anonca") or a PIV slot
	// certificate chaining to a trusted PIV attestation CA. Only this level is
	// evidence that the private key was generated inside, and cannot leave, a
	// hardware authenticator.
	AttestLevelHardware AttestLevel = AttestLevel(qrdbsdk.AttestationLevelHardware)
)

// IsHardware reports whether the level is manufacturer-attested hardware. It is
// the single predicate every policy decision keys on, so "is this good enough
// for an administrator" is answered in one place rather than by string
// comparisons scattered across handlers.
func (l AttestLevel) IsHardware() bool {
	return l == AttestLevelHardware
}

// Valid reports whether the level is one of the three defined values. The empty
// string is NOT valid: an unset level is an unanswered question, and callers
// must treat it as such rather than defaulting it to anything.
func (l AttestLevel) Valid() bool {
	switch l {
	case AttestLevelNone, AttestLevelSoftware, AttestLevelHardware:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (l AttestLevel) String() string {
	return string(l)
}

// Attestation method identifiers recorded on an AttestationResult. They name
// the ceremony that produced the evidence, not the credential kind.
const (
	// AttestMethodWebAuthn identifies evidence from a WebAuthn registration
	// attestation statement.
	AttestMethodWebAuthn = "webauthn"

	// AttestMethodPIV identifies evidence from a PIV slot attestation
	// certificate chain.
	AttestMethodPIV = "piv"
)

// AuthFactorMetadataAttestLevel is the qrdb auth-factor metadata key holding
// the AttestLevel a credential proved at enrollment. Enrollment writes it; the
// LOGIN that later consumes the factor reads it and replays it onto the session
// it issues (pkg/server login_session_issuer.go), so the two sides must spell it
// identically — hence one exported constant rather than two string literals.
//
// The comment here used to say this was "the field login reads to decide whether
// a stored factor may authenticate an administrator". It was not: the field was
// written at enrollment and read NOWHERE, and no login consulted it (#394). Two
// corrections, because the difference matters to anyone reasoning about what
// enforces what:
//
//   - it is read now, but by the SESSION ISSUER, which copies it onto the token
//     rather than deciding anything with it;
//   - the decision it described belongs to the enrollment-side gates
//     (requireProvenHardware and the WebAuthn assurance gate). Enforcement lives
//     at issuance and enrollment; no PEP in this repository reads attest_level
//     off a token today. The claim exists so a downstream consumer CAN, which is
//     why it has to be present on every path that proved it.
//
// A doc describing a control that does not exist is worse than no doc: it reads
// as an assurance in code review.
//
// The value is qrdb's own: since go-qrdb v0.16.2-alpha the server writes the
// key (AttestationRecorder for a ceremony, the plain Add/Rotate for an
// unattested factor) and a caller may not. The AttestLevel type stays local
// because login policy needs IsHardware on it; its VALUES are the SDK's.
const AuthFactorMetadataAttestLevel = qrdbsdk.MetadataAttestLevel

// AttestationResult is the proven provenance of one credential. Its fields are
// unexported on purpose: the only way to obtain one carrying
// AttestLevelHardware is to run a verifier in this package that actually
// checked a certificate chain. See the file commentary for why.
type AttestationResult struct {
	level  AttestLevel
	aaguid string
	format string
	method string
}

// Level returns the proven attestation level.
func (r *AttestationResult) Level() AttestLevel {
	if r == nil {
		return ""
	}
	return r.level
}

// AAGUID returns the hex-encoded authenticator model identifier for WebAuthn
// evidence, or the empty string for PIV (smartcards have no AAGUID).
func (r *AttestationResult) AAGUID() string {
	if r == nil {
		return ""
	}
	return r.aaguid
}

// Format returns the attestation statement format the evidence came from — the
// WebAuthn attestation type ("basic_full", "basic_surrogate", ...) or the PIV
// chain descriptor. It is operator-facing detail for audit records.
func (r *AttestationResult) Format() string {
	if r == nil {
		return ""
	}
	return r.format
}

// Method returns the ceremony that produced the evidence: AttestMethodWebAuthn
// or AttestMethodPIV.
func (r *AttestationResult) Method() string {
	if r == nil {
		return ""
	}
	return r.method
}

// IsHardware reports whether this result proves a hardware-backed key. A nil
// result is not hardware — an absent proof never satisfies a hardware
// requirement.
func (r *AttestationResult) IsHardware() bool {
	return r != nil && r.level.IsHardware()
}

// attestLevelOrNone reports the level a result proves, treating an absent
// result as AttestLevelNone. It exists so refusal messages name a level rather
// than printing an empty string for the "no evidence at all" case.
func attestLevelOrNone(r *AttestationResult) AttestLevel {
	if level := r.Level(); level.Valid() {
		return level
	}
	return AttestLevelNone
}

// webAuthnAttestLevel maps a verified WebAuthn attestation type — the value
// go-webauthn writes to Credential.AttestationType after running the format's
// verification procedure — onto an AttestLevel.
//
// The mapping is the FIDO metadata taxonomy: basic_full, attca and anonca all
// mean the statement was signed by a key the authenticator's manufacturer
// controls, which is the property that makes the AAGUID trustworthy.
// basic_surrogate is a self-signature and proves nothing about storage; an
// empty or unrecognised type is treated as no attestation, because an
// attestation we cannot classify must never be read as evidence of hardware.
func webAuthnAttestLevel(attestationType string) AttestLevel {
	switch strings.ToLower(strings.TrimSpace(attestationType)) {
	case "basic_full", "attca", "anonca":
		return AttestLevelHardware
	case "basic_surrogate", "self":
		return AttestLevelSoftware
	default:
		return AttestLevelNone
	}
}

// =============================================================================
// Typed errors
// =============================================================================

// AttestationRequiredError reports that a ceremony demanded a minimum
// attestation level and the credential proved less. It is the operator-facing
// refusal for "you asked to enroll a security key and presented something that
// cannot prove it is one".
type AttestationRequiredError struct {
	// Operation names the ceremony that refused ("webauthn enrollment",
	// "piv login").
	Operation string

	// Required is the level the policy demanded.
	Required AttestLevel

	// Proven is the level the credential actually established.
	Proven AttestLevel

	// Format is the attestation statement format the evidence came from, when
	// known. Empty when no statement was conveyed.
	Format string
}

// Error implements error.
func (e *AttestationRequiredError) Error() string {
	if e == nil {
		return "iam.attestation: required attestation was not proven"
	}
	msg := "iam.attestation: " + e.Operation + " requires " + string(e.Required) +
		" attestation but the authenticator proved " + string(e.Proven)
	if e.Format != "" {
		msg += " (attestation format " + e.Format + ")"
	}
	switch e.Proven {
	case AttestLevelNone:
		msg += "; the authenticator conveyed no attestation statement — a virtual or software authenticator cannot satisfy this requirement, enroll a hardware security key"
	case AttestLevelSoftware:
		msg += "; the attestation statement is self-signed, which proves possession of the key but not that it lives in hardware — enroll a hardware security key"
	}
	return msg
}

// AttestationUnprovenError reports that a caller supplied no attestation
// evidence at all where evidence was mandatory. It is distinct from
// AttestationRequiredError: that one means "we looked and it was not good
// enough", this one means "nothing was ever verified", which is a wiring defect
// rather than a rejected authenticator.
type AttestationUnprovenError struct {
	// Operation names the call site that was missing evidence.
	Operation string
}

// Error implements error.
func (e *AttestationUnprovenError) Error() string {
	if e == nil || e.Operation == "" {
		return "iam.attestation: no verified attestation evidence was supplied"
	}
	return "iam.attestation: " + e.Operation +
		" was asked to assert hardware attestation without verified evidence; " +
		"the attestation must be proven by a verifier in pkg/iam, never asserted by the caller"
}

// AuthFactorMetadataAttestAAGUID is the qrdb auth-factor metadata key holding
// the attested authenticator model. Like AuthFactorMetadataAttestLevel it is
// written by the enrollment ceremony, never by a caller.
const AuthFactorMetadataAttestAAGUID = qrdbsdk.MetadataAttestAAGUID

// AuthFactorAttestationMetadataError reports that a caller tried to set an
// attestation field on an auth factor directly.
//
// The attestation fields record what a CEREMONY proved. An API that writes a
// factor row without running one proved nothing, so a value it supplied would
// be a forged claim wearing the field name login policy trusts — the same
// forgery the token path refuses, one layer down.
type AuthFactorAttestationMetadataError struct {
	// Field is the reserved metadata key the caller tried to set.
	Field string
}

// Error implements error.
func (e *AuthFactorAttestationMetadataError) Error() string {
	field := "an attestation field"
	if e != nil && e.Field != "" {
		field = e.Field
	}
	return "iam.authfactor: " + field + " is not a caller-supplied field: it records what an enrollment " +
		"ceremony proved about the authenticator, and a factor written through this API proved nothing. " +
		"Enroll through the WebAuthn or PIV ceremony to obtain an attested credential."
}

// Unwrap reports this as an invalid FACTOR, which is what it is: the caller
// sent a field they may not set.
//
// The sentinel is load-bearing rather than decorative. Every transport that
// carries this surface classifies a servicer error by matching qrdb's factor
// sentinels — qrdb's gRPC mapper switches on errors.Is(err,
// auth.ErrInvalidFactor) and falls through to codes.Internal for anything it
// does not recognise. Without this, a caller who supplied a reserved
// attest_level got INTERNAL, which says "the server broke" about a request the
// server correctly refused: it reads as an outage, it invites a retry that
// cannot succeed, and the one thing the caller needs — that their input was
// rejected, and which field — arrives labelled as a server fault.
func (e *AuthFactorAttestationMetadataError) Unwrap() error {
	return qrdbsdk.ErrInvalidFactor
}

// ValidateAuthFactorMetadata refuses metadata that asserts an attestation the
// caller did not prove.
//
// It is the single definition of that rule, called from every surface that can
// write an auth factor without running a ceremony — the SDK's Add and Rotate,
// and the qrdb auth-factor servicer proxy that backs the gRPC and MCP tools.
// Keeping one function rather than a check per surface is what stops the next
// transport from quietly reopening the hole.
//
// It REFUSES rather than stripping. A caller who set attest_level and got a
// success would reasonably believe the factor is recorded as hardware; saying
// the field is not theirs to set is the difference between a clear contract and
// a silent one.
//
// The rule is qrdb's (ReservedMetadataPrefix, go-qrdb#234): every key inside
// the server-owned attest_* namespace is refused, matched case-insensitively
// as a prefix, so an attestation field invented later is covered without an
// edit here. qrdb enforces the same rule at its service; this check exists so
// a QuicBaaS surface can refuse EARLY with its own typed error, before a wire
// round trip, and so the SDK's input validation and the servicer proxy agree.
func ValidateAuthFactorMetadata(metadata map[string]string) error {
	for key := range metadata {
		if len(key) >= len(qrdbsdk.ReservedMetadataPrefix) &&
			strings.EqualFold(key[:len(qrdbsdk.ReservedMetadataPrefix)], qrdbsdk.ReservedMetadataPrefix) {
			return &AuthFactorAttestationMetadataError{Field: key}
		}
	}
	return nil
}

// VerifiedAttestationFor renders what a ceremony proved as the value qrdb's
// AttestationRecorder records. A nil or invalid result records nothing
// (qrdb's Unattested), never a guess; aaguidHex is the attested authenticator
// model when the ceremony verified one.
func VerifiedAttestationFor(result *AttestationResult, aaguidHex string) qrdbsdk.VerifiedAttestation {
	level := result.Level()
	if !level.Valid() {
		return qrdbsdk.Unattested()
	}
	return qrdbsdk.VerifiedAttestation{
		Level:  qrdbsdk.AttestationLevel(level),
		AAGUID: aaguidHex,
	}
}
