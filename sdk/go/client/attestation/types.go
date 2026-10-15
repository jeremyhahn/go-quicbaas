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

// Package attestation holds the wire-format request/response types for the
// quicbaas.Attestation service (Security().Attestation()). They live in this
// leaf package — not in sdk/go — so the transport implementations
// (rest/quic/grpc/embedded/mcp) can return and decode them without creating
// an import cycle back into the top-level SDK package, which imports every
// transport. The high-level Security().Attestation() surface in sdk/go
// re-exports these types via aliases.
//
// The JSON tags mirror pkg/service's attestation request/response structs
// field-for-field, so the QUIC transport can unmarshal the server's
// service-layer encoding straight into these types, exactly as the Trust
// surface does.
package attestation

// ServiceName is the gRPC-style service identifier used by the SDK to route
// Attestation requests across every transport.
const ServiceName = "quicbaas.Attestation"

// VerifyOptions is the wire-portable form of the go-xkms attestation
// VerifyOptions.
type VerifyOptions struct {
	// TrustedRootsPEM are additional PEM-encoded trust anchors accepted
	// for this call, on top of any configured on the server.
	TrustedRootsPEM [][]byte `json:"trusted_roots_pem,omitempty"`

	// CheckFreshness enables the nonce and timestamp freshness check.
	CheckFreshness bool `json:"check_freshness,omitempty"`

	// ExpectedNonce is the nonce the attestation must carry. Only
	// consulted when CheckFreshness is set.
	ExpectedNonce []byte `json:"expected_nonce,omitempty"`

	// FreshnessWindowSeconds is the maximum acceptable attestation age.
	// Zero selects the xkms default of 300 seconds.
	FreshnessWindowSeconds int64 `json:"freshness_window_seconds,omitempty"`

	// VerifyPCRs enables PCR comparison against ExpectedPCRs.
	VerifyPCRs bool `json:"verify_pcrs,omitempty"`

	// ExpectedPCRs are the golden PCR values keyed by index.
	ExpectedPCRs map[uint32][]byte `json:"expected_pcrs,omitempty"`

	// AllowSelfSigned permits a self-signed attesting certificate. Lab and
	// simulator use only.
	AllowSelfSigned bool `json:"allow_self_signed,omitempty"`
}

// VerifyRequest carries a wire-portable attestation statement plus the
// options to verify it under.
type VerifyRequest struct {
	Format             string            `json:"format"`
	Backend            string            `json:"backend,omitempty"`
	ChainPEM           [][]byte          `json:"chain_pem"`
	AttestedKeyDER     []byte            `json:"attested_key_der"`
	AttestationData    []byte            `json:"attestation_data"`
	Signature          []byte            `json:"signature"`
	SignatureAlgorithm string            `json:"signature_algorithm,omitempty"`
	Nonce              []byte            `json:"nonce,omitempty"`
	CreatedAt          string            `json:"created_at,omitempty"`
	PCRs               map[uint32][]byte `json:"pcrs,omitempty"`
	Options            *VerifyOptions    `json:"options,omitempty"`
}

// VerifyResponse reports the outcome of a verification. Valid is
// authoritative; the component flags say WHY a verification failed without
// requiring the caller to parse Reason.
type VerifyResponse struct {
	Valid          bool   `json:"valid"`
	Format         string `json:"format,omitempty"`
	Backend        string `json:"backend,omitempty"`
	ChainValid     bool   `json:"chain_valid"`
	SignatureValid bool   `json:"signature_valid"`
	FreshnessValid bool   `json:"freshness_valid"`
	PCRsValid      bool   `json:"pcrs_valid"`
	Reason         string `json:"reason,omitempty"`
}

// VerifyChainRequest carries the certificate chain to validate.
type VerifyChainRequest struct {
	Format   string         `json:"format"`
	ChainPEM [][]byte       `json:"chain_pem"`
	Options  *VerifyOptions `json:"options,omitempty"`
}

// VerifyChainResponse reports whether the chain validated.
type VerifyChainResponse struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

// AttestContainerRequest describes the container whose execution environment
// is to be attested.
type AttestContainerRequest struct {
	DeviceID    string `json:"device_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Image       string `json:"image"`
	ImageDigest string `json:"image_digest,omitempty"`
	TrustLabel  string `json:"trust_label,omitempty"`
	TrustLevel  string `json:"trust_level,omitempty"`
	// ReferenceValues names the stored reference values the Verifier
	// appraises the host quote against. Required at "attested" and above.
	ReferenceValues string `json:"reference_values,omitempty"`
	MeasurementPCR  int    `json:"measurement_pcr,omitempty"`
	MeasurementBank string `json:"measurement_bank,omitempty"`
	Nonce           []byte `json:"nonce,omitempty"`
}

// AttestContainerResponse is the attestation evidence produced for a
// container. AttestedAt is carried as an RFC 3339 string rather than a
// time.Time so the shape is identical on every transport, including the
// proto-backed ones.
type AttestContainerResponse struct {
	Attested          bool              `json:"attested"`
	DeviceID          string            `json:"device_id,omitempty"`
	ContainerID       string            `json:"container_id,omitempty"`
	Image             string            `json:"image,omitempty"`
	ImageDigest       string            `json:"image_digest,omitempty"`
	ConfigDigest      string            `json:"config_digest,omitempty"`
	TrustLabel        string            `json:"trust_label,omitempty"`
	TrustLevel        string            `json:"trust_level,omitempty"`
	Nonce             []byte            `json:"nonce,omitempty"`
	Quoted            []byte            `json:"quoted,omitempty"`
	Signature         []byte            `json:"signature,omitempty"`
	PCRs              map[uint32][]byte `json:"pcrs,omitempty"`
	EventLog          []byte            `json:"event_log,omitempty"`
	Measured          bool              `json:"measured"`
	MeasurementPCR    int               `json:"measurement_pcr,omitempty"`
	MeasurementBank   string            `json:"measurement_bank,omitempty"`
	LaunchMeasurement []byte            `json:"launch_measurement,omitempty"`
	AttestedAt        string            `json:"attested_at,omitempty"`
}

// =============================================================================
// Verifier (RFC 9334): endorsements, signed reference values, appraisal.
// Timestamps are RFC 3339 strings so the shape is identical on every
// transport, including the proto-backed ones.
// =============================================================================

// Endorsement kinds.
const (
	// EndorsementReferenceSigner anchors signed reference values.
	EndorsementReferenceSigner = "reference-signer"

	// EndorsementAKRoot anchors attestation-key certificates.
	EndorsementAKRoot = "ak-root"
)

// Appraisal outcomes.
const (
	AppraisalPass    = "pass"
	AppraisalForged  = "forged"
	AppraisalAbsent  = "absent"
	AppraisalSkipped = "skipped"
)

// Endorsement is a Verifier trust anchor.
type Endorsement struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	CertificatePEM []byte `json:"certificate_pem,omitempty"`
	PublicKeyPEM   []byte `json:"public_key_pem,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

// PutEndorsementRequest stores (or replaces) an endorsement. Exactly one of
// CertificatePEM and PublicKeyPEM is set; an ak-root is a CA certificate.
type PutEndorsementRequest struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	CertificatePEM []byte `json:"certificate_pem,omitempty"`
	PublicKeyPEM   []byte `json:"public_key_pem,omitempty"`
}

// ListEndorsementsResponse lists endorsements, ordered by name.
type ListEndorsementsResponse struct {
	Endorsements []*Endorsement `json:"endorsements"`
}

// ReferenceValues describes stored, endorsed, signed reference values.
type ReferenceValues struct {
	Name        string   `json:"name"`
	ManifestID  string   `json:"manifest_id"`
	Profile     string   `json:"profile,omitempty"`
	Signer      string   `json:"signer,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	PCRIndices  []uint32 `json:"pcr_indices"`
	StoredAt    string   `json:"stored_at,omitempty"`
	SignedCoRIM []byte   `json:"signed_corim,omitempty"`
}

// ListReferenceValuesResponse lists stored reference values (without the
// signed CoRIM bodies).
type ListReferenceValuesResponse struct {
	ReferenceValues []*ReferenceValues `json:"reference_values"`
}

// AppraiseQuoteRequest is TPM quote evidence plus the reference values to
// appraise it against.
type AppraiseQuoteRequest struct {
	AKCertificatePEM []byte            `json:"ak_certificate_pem"`
	Quote            []byte            `json:"quote"`
	Signature        []byte            `json:"signature"`
	Nonce            []byte            `json:"nonce"`
	PCRs             map[uint32][]byte `json:"pcrs"`
	EventLog         []byte            `json:"event_log,omitempty"`
	ReferenceValues  string            `json:"reference_values"`
}

// AppraisalCheck is one check of an appraisal.
type AppraisalCheck struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// AttestationResult is the Verifier's result for one appraisal. Verified is
// true only when every check passed; CryptographicFailure marks evidence that
// was present and wrong (grounds for quarantine) as opposed to absent.
type AttestationResult struct {
	Verified             bool             `json:"verified"`
	CryptographicFailure bool             `json:"cryptographic_failure"`
	Checks               []AppraisalCheck `json:"checks"`
	ReferenceValues      string           `json:"reference_values,omitempty"`
	ManifestID           string           `json:"manifest_id,omitempty"`
	Signer               string           `json:"signer,omitempty"`
	BaselineSource       string           `json:"baseline_source,omitempty"`
	AppraisedAt          string           `json:"appraised_at,omitempty"`
}

// The Verifier's request envelopes that carry only a name or a filter.
type (
	// ListEndorsementsRequest filters endorsements by kind.
	ListEndorsementsRequest struct {
		Kind string `json:"kind,omitempty"`
	}

	// NameRequest names an endorsement or reference values.
	NameRequest struct {
		Name string `json:"name"`
	}

	// PutReferenceValuesRequest stores signed reference values.
	PutReferenceValuesRequest struct {
		Name        string `json:"name"`
		SignedCoRIM []byte `json:"signed_corim"`
	}
)
