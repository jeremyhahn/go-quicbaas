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

// services_attestation.go provides the Security().Attestation() sub-service:
// the SDK front door to QuicBaaS's standalone trusted-computing surface.
//
// It has two halves, and the split matters:
//
//   - Verify / VerifyChain are a thin, transport-routed facade over the
//     go-xkms attestation verifier running on the server. QuicBaaS
//     reimplements no signature check and no chain walk.
//
//   - AttestContainer crosses the orchestration seam. QuicBaaS ships the
//     standalone tiers itself (image digest pinning, host TPM quote against
//     expected PCRs, measured launch — all in pkg/orchestration), and an
//     embedder may substitute a fleet implementation. Until one is
//     registered on the server, this verb returns a typed not-configured
//     error, exactly as Trust().PublishDANE does without a DANE publisher.
//
// VerifyImageSignature is a client-side convenience: it turns the Tier 0
// detached image endorsement into a Verify call rather than adding a fourth
// verb to every transport.

package quicbaas

import (
	"context"
	"crypto/x509"
	"encoding/pem"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// attestationServiceName is the service identifier every transport routes
// Attestation requests through.
const attestationServiceName = attestationclient.ServiceName

// Attestation wire-type re-exports. Callers use these through
// Security().Attestation() and never import the leaf client package.
type (
	// AttestationVerifyOptions configures an attestation verification.
	AttestationVerifyOptions = attestationclient.VerifyOptions

	// AttestationVerifyRequest is a wire-portable attestation statement.
	AttestationVerifyRequest = attestationclient.VerifyRequest

	// AttestationVerifyResponse reports the outcome of a verification.
	AttestationVerifyResponse = attestationclient.VerifyResponse

	// AttestationVerifyChainRequest carries a certificate chain to check.
	AttestationVerifyChainRequest = attestationclient.VerifyChainRequest

	// AttestationVerifyChainResponse reports whether a chain validated.
	AttestationVerifyChainResponse = attestationclient.VerifyChainResponse

	// AttestContainerRequest describes a container to attest.
	AttestContainerRequest = attestationclient.AttestContainerRequest

	// AttestContainerResponse is the evidence produced for a container.
	AttestContainerResponse = attestationclient.AttestContainerResponse

	// AttestationEndorsement is a Verifier trust anchor.
	AttestationEndorsement = attestationclient.Endorsement

	// AttestationPutEndorsementRequest stores an endorsement.
	AttestationPutEndorsementRequest = attestationclient.PutEndorsementRequest

	// AttestationListEndorsementsResponse lists endorsements.
	AttestationListEndorsementsResponse = attestationclient.ListEndorsementsResponse

	// AttestationReferenceValues describes stored, signed reference values.
	AttestationReferenceValues = attestationclient.ReferenceValues

	// AttestationListReferenceValuesResponse lists stored reference values.
	AttestationListReferenceValuesResponse = attestationclient.ListReferenceValuesResponse

	// AttestationAppraiseQuoteRequest is TPM quote evidence to appraise.
	AttestationAppraiseQuoteRequest = attestationclient.AppraiseQuoteRequest

	// AttestationAppraisalCheck is one check of an appraisal.
	AttestationAppraisalCheck = attestationclient.AppraisalCheck

	// AttestationResult is the Verifier's attestation result.
	AttestationResult = attestationclient.AttestationResult
)

// Verifier endorsement kinds and appraisal outcomes.
const (
	AttestationEndorsementReferenceSigner = attestationclient.EndorsementReferenceSigner
	AttestationEndorsementAKRoot          = attestationclient.EndorsementAKRoot
	AttestationAppraisalPass              = attestationclient.AppraisalPass
	AttestationAppraisalForged            = attestationclient.AppraisalForged
	AttestationAppraisalAbsent            = attestationclient.AppraisalAbsent
	AttestationAppraisalSkipped           = attestationclient.AppraisalSkipped
)

// ImageEndorsement is the Tier 0 detached endorsement of a pinned container
// image digest: a signature over the canonical "sha256:<hex>" digest string,
// plus the signer's certificate chain.
//
// It mirrors orchestration.ImageSignature, restated here so SDK consumers do
// not import pkg/orchestration.
type ImageEndorsement struct {
	// Digest is the endorsed image content digest, "sha256:<hex>".
	Digest string

	// Signature is the raw signature over the ASCII Digest string.
	Signature []byte

	// SignerChainPEM is the signer certificate chain, leaf first.
	SignerChainPEM [][]byte

	// SignatureAlgorithm names the x509 signature algorithm. Empty asks
	// the server to infer it from the signer certificate.
	SignatureAlgorithm string
}

// ImageEndorsementFormat is the attestation Format string used for Tier 0
// container image endorsements. It distinguishes them from hardware key
// attestations ("tpm2", "pkcs11") in audit output.
const ImageEndorsementFormat = "oci-image"

// AttestationService is the Security() sub-service for trusted computing.
type AttestationService interface {
	// Verify validates a complete attestation statement: certificate
	// chain, signature over the attestation data, and — when requested —
	// nonce freshness and PCR values. The verification itself runs in
	// go-xkms on the server.
	Verify(ctx context.Context, req *AttestationVerifyRequest) (*AttestationVerifyResponse, error)

	// VerifyChain validates only the certificate chain of an attestation,
	// so a caller can establish that an attesting key is genuine before
	// paying for a full verification.
	VerifyChain(ctx context.Context, req *AttestationVerifyChainRequest) (*AttestationVerifyChainResponse, error)

	// VerifyImageSignature verifies a Tier 0 detached image endorsement
	// against the supplied digest. It is a client-side convenience over
	// Verify: the endorsement is expressed as an attestation statement
	// whose attestation data is the ASCII digest string.
	VerifyImageSignature(ctx context.Context, endorsement *ImageEndorsement, opts *AttestationVerifyOptions) (*AttestationVerifyResponse, error)

	// AttestContainer requests attestation evidence for a container from
	// the attestor registered on the server. Returns a typed
	// not-configured error when none is registered.
	AttestContainer(ctx context.Context, req *AttestContainerRequest) (*AttestContainerResponse, error)

	// PutEndorsement stores (or replaces) a Verifier trust anchor: a
	// reference-value signer or an attestation-key root.
	PutEndorsement(ctx context.Context, req *AttestationPutEndorsementRequest) (*AttestationEndorsement, error)

	// ListEndorsements lists endorsements; an empty kind lists all.
	ListEndorsements(ctx context.Context, kind string) (*AttestationListEndorsementsResponse, error)

	// DeleteEndorsement removes an endorsement.
	DeleteEndorsement(ctx context.Context, name string) error

	// PutReferenceValues stores a signed CoRIM that a stored
	// reference-signer endorsement verifies; unendorsed values are refused.
	PutReferenceValues(ctx context.Context, name string, signedCoRIM []byte) (*AttestationReferenceValues, error)

	// GetReferenceValues returns stored reference values.
	GetReferenceValues(ctx context.Context, name string) (*AttestationReferenceValues, error)

	// ListReferenceValues lists stored reference values.
	ListReferenceValues(ctx context.Context) (*AttestationListReferenceValuesResponse, error)

	// DeleteReferenceValues removes stored reference values.
	DeleteReferenceValues(ctx context.Context, name string) error

	// AppraiseQuote appraises TPM quote evidence against stored, signed
	// reference values and returns the attestation result. Evidence that
	// fails is a successful call whose result says Verified=false.
	AppraiseQuote(ctx context.Context, req *AttestationAppraiseQuoteRequest) (*AttestationResult, error)
}

// attestationServiceImpl is the transport-backed AttestationService.
type attestationServiceImpl struct {
	t transport.Transport
}

// newAttestationService builds the transport-backed attestation service.
func newAttestationService(t transport.Transport) AttestationService {
	return &attestationServiceImpl{t: t}
}

// Verify sends a full attestation statement for verification.
func (s *attestationServiceImpl) Verify(ctx context.Context, req *AttestationVerifyRequest) (*AttestationVerifyResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.Verify", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.Verify", "request is required")
	}
	if req.Format == "" {
		return nil, sdkerrors.NewInvalidInput("attestation.Verify", "format is required")
	}
	if len(req.ChainPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.Verify", "chain is required")
	}
	if len(req.Signature) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.Verify", "signature is required")
	}

	resp, err := s.t.Request(ctx, attestationServiceName, "Verify", req)
	if err != nil {
		return nil, err
	}
	payload, ok := resp.(*AttestationVerifyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("attestation.Verify", "invalid response type")
	}
	return payload, nil
}

// VerifyChain sends a certificate chain for validation.
func (s *attestationServiceImpl) VerifyChain(ctx context.Context, req *AttestationVerifyChainRequest) (*AttestationVerifyChainResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyChain", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyChain", "request is required")
	}
	if len(req.ChainPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyChain", "chain is required")
	}

	resp, err := s.t.Request(ctx, attestationServiceName, "VerifyChain", req)
	if err != nil {
		return nil, err
	}
	payload, ok := resp.(*AttestationVerifyChainResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("attestation.VerifyChain", "invalid response type")
	}
	return payload, nil
}

// VerifyImageSignature expresses a Tier 0 image endorsement as an attestation
// statement and verifies it.
//
// The attested key is the signer's own public key: an image endorsement
// attests to CONTENT (the digest in AttestationData), not to another key, and
// the xkms statement contract requires an attested key to be present. Reusing
// the signer's key keeps the statement well-formed without inventing a
// meaningless second key.
func (s *attestationServiceImpl) VerifyImageSignature(ctx context.Context, endorsement *ImageEndorsement, opts *AttestationVerifyOptions) (*AttestationVerifyResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyImageSignature", "context is required")
	}
	if endorsement == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyImageSignature", "endorsement is required")
	}
	if endorsement.Digest == "" {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyImageSignature", "digest is required")
	}
	if len(endorsement.Signature) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyImageSignature", "signature is required")
	}
	if len(endorsement.SignerChainPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.VerifyImageSignature", "signer chain is required")
	}

	signerKeyDER, err := leafPublicKeyDER(endorsement.SignerChainPEM[0])
	if err != nil {
		return nil, err
	}

	return s.Verify(ctx, &AttestationVerifyRequest{
		Format:             ImageEndorsementFormat,
		Backend:            ImageEndorsementFormat,
		ChainPEM:           endorsement.SignerChainPEM,
		AttestedKeyDER:     signerKeyDER,
		AttestationData:    []byte(endorsement.Digest),
		Signature:          endorsement.Signature,
		SignatureAlgorithm: endorsement.SignatureAlgorithm,
		Options:            opts,
	})
}

// AttestContainer crosses the orchestration seam.
func (s *attestationServiceImpl) AttestContainer(ctx context.Context, req *AttestContainerRequest) (*AttestContainerResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.AttestContainer", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.AttestContainer", "request is required")
	}
	if req.Image == "" {
		return nil, sdkerrors.NewInvalidInput("attestation.AttestContainer", "image is required")
	}

	resp, err := s.t.Request(ctx, attestationServiceName, "AttestContainer", req)
	if err != nil {
		return nil, err
	}
	payload, ok := resp.(*AttestContainerResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("attestation.AttestContainer", "invalid response type")
	}
	return payload, nil
}

// leafPublicKeyDER extracts the PKIX DER encoding of the public key in a
// PEM- or DER-encoded certificate.
func leafPublicKeyDER(certBlob []byte) ([]byte, error) {
	der := certBlob
	if block, _ := pem.Decode(certBlob); block != nil {
		der = block.Bytes
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput,
			"attestation.VerifyImageSignature", "failed to parse signer certificate")
	}
	return cert.RawSubjectPublicKeyInfo, nil
}

// verifierCall sends one Verifier request and asserts the response type.
func verifierCall[T any](ctx context.Context, t transport.Transport, method string, req interface{}) (*T, error) {
	op := "attestation." + method
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput(op, "context is required")
	}
	resp, err := t.Request(ctx, attestationServiceName, method, req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*T)
	if !ok {
		return nil, sdkerrors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// requireName rejects an empty endorsement or reference-values name.
func requireName(op, name string) error {
	if name == "" {
		return sdkerrors.NewInvalidInput(op, "name is required")
	}
	return nil
}

// PutEndorsement stores a Verifier trust anchor.
func (s *attestationServiceImpl) PutEndorsement(ctx context.Context, req *AttestationPutEndorsementRequest) (*AttestationEndorsement, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.PutEndorsement", "request is required")
	}
	if err := requireName("attestation.PutEndorsement", req.Name); err != nil {
		return nil, err
	}
	return verifierCall[AttestationEndorsement](ctx, s.t, "PutEndorsement", req)
}

// ListEndorsements lists endorsements.
func (s *attestationServiceImpl) ListEndorsements(ctx context.Context, kind string) (*AttestationListEndorsementsResponse, error) {
	return verifierCall[AttestationListEndorsementsResponse](ctx, s.t, "ListEndorsements", &attestationclient.ListEndorsementsRequest{Kind: kind})
}

// DeleteEndorsement removes an endorsement.
func (s *attestationServiceImpl) DeleteEndorsement(ctx context.Context, name string) error {
	if err := requireName("attestation.DeleteEndorsement", name); err != nil {
		return err
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("attestation.DeleteEndorsement", "context is required")
	}
	_, err := s.t.Request(ctx, attestationServiceName, "DeleteEndorsement", &attestationclient.NameRequest{Name: name})
	return err
}

// PutReferenceValues stores endorsed, signed reference values.
func (s *attestationServiceImpl) PutReferenceValues(ctx context.Context, name string, signedCoRIM []byte) (*AttestationReferenceValues, error) {
	if err := requireName("attestation.PutReferenceValues", name); err != nil {
		return nil, err
	}
	if len(signedCoRIM) == 0 {
		return nil, sdkerrors.NewInvalidInput("attestation.PutReferenceValues", "signed CoRIM is required")
	}
	return verifierCall[AttestationReferenceValues](ctx, s.t, "PutReferenceValues",
		&attestationclient.PutReferenceValuesRequest{Name: name, SignedCoRIM: signedCoRIM})
}

// GetReferenceValues returns stored reference values.
func (s *attestationServiceImpl) GetReferenceValues(ctx context.Context, name string) (*AttestationReferenceValues, error) {
	if err := requireName("attestation.GetReferenceValues", name); err != nil {
		return nil, err
	}
	return verifierCall[AttestationReferenceValues](ctx, s.t, "GetReferenceValues", &attestationclient.NameRequest{Name: name})
}

// ListReferenceValues lists stored reference values.
func (s *attestationServiceImpl) ListReferenceValues(ctx context.Context) (*AttestationListReferenceValuesResponse, error) {
	return verifierCall[AttestationListReferenceValuesResponse](ctx, s.t, "ListReferenceValues", &attestationclient.ListEndorsementsRequest{})
}

// DeleteReferenceValues removes stored reference values.
func (s *attestationServiceImpl) DeleteReferenceValues(ctx context.Context, name string) error {
	if err := requireName("attestation.DeleteReferenceValues", name); err != nil {
		return err
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("attestation.DeleteReferenceValues", "context is required")
	}
	_, err := s.t.Request(ctx, attestationServiceName, "DeleteReferenceValues", &attestationclient.NameRequest{Name: name})
	return err
}

// AppraiseQuote appraises TPM quote evidence.
func (s *attestationServiceImpl) AppraiseQuote(ctx context.Context, req *AttestationAppraiseQuoteRequest) (*AttestationResult, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("attestation.AppraiseQuote", "request is required")
	}
	if req.ReferenceValues == "" {
		return nil, sdkerrors.NewInvalidInput("attestation.AppraiseQuote", "reference values name is required")
	}
	return verifierCall[AttestationResult](ctx, s.t, "AppraiseQuote", req)
}
