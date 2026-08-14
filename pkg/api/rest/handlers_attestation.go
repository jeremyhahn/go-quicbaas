//go:build rest

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

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Attestation Service Route Registration
// =============================================================================

// registerAttestationRoutes registers the trusted-computing routes under
// /api/v1/attestation. Verify and verify-chain delegate to the service-layer
// AttestationService, which composes the go-xkms attestation verifier;
// attest-container crosses the orchestration seam and returns a typed
// not-configured error until an application registers a container attestor.
func registerAttestationRoutes(s *Server, r chi.Router) {
	r.Route("/attestation", func(r chi.Router) {
		r.Post("/verify", s.HandleAttestationVerify)
		r.Post("/verify-chain", s.HandleAttestationVerifyChain)
		r.Post("/attest-container", s.HandleAttestationAttestContainer)
	})
}

// =============================================================================
// Request/Response Types
// =============================================================================
//
// The JSON shapes mirror the service-layer request/response structs
// field-for-field, which is what lets the SDK's REST handler decode them
// straight into the leaf client/attestation types.

// AttestationVerifyOptionsREST is the JSON form of the verification options.
type AttestationVerifyOptionsREST struct {
	TrustedRootsPEM        [][]byte          `json:"trusted_roots_pem,omitempty"`
	CheckFreshness         bool              `json:"check_freshness,omitempty"`
	ExpectedNonce          []byte            `json:"expected_nonce,omitempty"`
	FreshnessWindowSeconds int64             `json:"freshness_window_seconds,omitempty"`
	VerifyPCRs             bool              `json:"verify_pcrs,omitempty"`
	ExpectedPCRs           map[uint32][]byte `json:"expected_pcrs,omitempty"`
	AllowSelfSigned        bool              `json:"allow_self_signed,omitempty"`
}

// toService converts the REST options into the service-layer form. A nil
// receiver stays nil so the service applies the xkms secure defaults.
func (o *AttestationVerifyOptionsREST) toService() *service.VerifyAttestationOptions {
	if o == nil {
		return nil
	}
	return &service.VerifyAttestationOptions{
		TrustedRootsPEM:        o.TrustedRootsPEM,
		CheckFreshness:         o.CheckFreshness,
		ExpectedNonce:          o.ExpectedNonce,
		FreshnessWindowSeconds: o.FreshnessWindowSeconds,
		VerifyPCRs:             o.VerifyPCRs,
		ExpectedPCRs:           o.ExpectedPCRs,
		AllowSelfSigned:        o.AllowSelfSigned,
	}
}

// AttestationVerifyRESTRequest is the JSON body for
// POST /api/v1/attestation/verify.
type AttestationVerifyRESTRequest struct {
	Format             string                        `json:"format"`
	Backend            string                        `json:"backend,omitempty"`
	ChainPEM           [][]byte                      `json:"chain_pem"`
	AttestedKeyDER     []byte                        `json:"attested_key_der"`
	AttestationData    []byte                        `json:"attestation_data"`
	Signature          []byte                        `json:"signature"`
	SignatureAlgorithm string                        `json:"signature_algorithm,omitempty"`
	Nonce              []byte                        `json:"nonce,omitempty"`
	CreatedAt          string                        `json:"created_at,omitempty"`
	PCRs               map[uint32][]byte             `json:"pcrs,omitempty"`
	Options            *AttestationVerifyOptionsREST `json:"options,omitempty"`
}

// AttestationVerifyRESTResponse is the JSON body returned by
// POST /api/v1/attestation/verify.
type AttestationVerifyRESTResponse struct {
	Valid          bool   `json:"valid"`
	Format         string `json:"format,omitempty"`
	Backend        string `json:"backend,omitempty"`
	ChainValid     bool   `json:"chain_valid"`
	SignatureValid bool   `json:"signature_valid"`
	FreshnessValid bool   `json:"freshness_valid"`
	PCRsValid      bool   `json:"pcrs_valid"`
	Reason         string `json:"reason,omitempty"`
}

// AttestationVerifyChainRESTRequest is the JSON body for
// POST /api/v1/attestation/verify-chain.
type AttestationVerifyChainRESTRequest struct {
	Format   string                        `json:"format"`
	ChainPEM [][]byte                      `json:"chain_pem"`
	Options  *AttestationVerifyOptionsREST `json:"options,omitempty"`
}

// AttestationVerifyChainRESTResponse is the JSON body returned by
// POST /api/v1/attestation/verify-chain.
type AttestationVerifyChainRESTResponse struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

// AttestContainerRESTRequest is the JSON body for
// POST /api/v1/attestation/attest-container.
type AttestContainerRESTRequest struct {
	DeviceID        string            `json:"device_id,omitempty"`
	ContainerID     string            `json:"container_id,omitempty"`
	Image           string            `json:"image"`
	ImageDigest     string            `json:"image_digest,omitempty"`
	TrustLabel      string            `json:"trust_label,omitempty"`
	TrustLevel      string            `json:"trust_level,omitempty"`
	ExpectedPCRs    map[uint32][]byte `json:"expected_pcrs,omitempty"`
	MeasurementPCR  int               `json:"measurement_pcr,omitempty"`
	MeasurementBank string            `json:"measurement_bank,omitempty"`
	Nonce           []byte            `json:"nonce,omitempty"`
}

// AttestContainerRESTResponse is the JSON body returned by
// POST /api/v1/attestation/attest-container.
type AttestContainerRESTResponse struct {
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
// Handler Functions
// =============================================================================

// HandleAttestationVerify handles POST /api/v1/attestation/verify — validates
// a complete attestation statement through the go-xkms verifier.
func (s *Server) HandleAttestationVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AttestationVerifyRESTRequest
	if !decodeAttestationBody(w, r, &req) {
		return
	}

	attestationSvc := s.service.Attestation()
	if attestationSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrAttestationServiceUnavailable)
		return
	}

	svcResp, err := attestationSvc.Verify(ctx, &service.VerifyAttestationRequest{
		Format:             req.Format,
		Backend:            req.Backend,
		ChainPEM:           req.ChainPEM,
		AttestedKeyDER:     req.AttestedKeyDER,
		AttestationData:    req.AttestationData,
		Signature:          req.Signature,
		SignatureAlgorithm: req.SignatureAlgorithm,
		Nonce:              req.Nonce,
		CreatedAt:          req.CreatedAt,
		PCRs:               req.PCRs,
		Options:            req.Options.toService(),
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AttestationVerifyRESTResponse{
		Valid:          svcResp.Valid,
		Format:         svcResp.Format,
		Backend:        svcResp.Backend,
		ChainValid:     svcResp.ChainValid,
		SignatureValid: svcResp.SignatureValid,
		FreshnessValid: svcResp.FreshnessValid,
		PCRsValid:      svcResp.PCRsValid,
		Reason:         svcResp.Reason,
	})
}

// HandleAttestationVerifyChain handles POST /api/v1/attestation/verify-chain —
// validates only the certificate chain of an attestation.
func (s *Server) HandleAttestationVerifyChain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AttestationVerifyChainRESTRequest
	if !decodeAttestationBody(w, r, &req) {
		return
	}
	if len(req.ChainPEM) == 0 {
		RespondBadRequest(w, r, "chain_pem is required")
		return
	}

	attestationSvc := s.service.Attestation()
	if attestationSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrAttestationServiceUnavailable)
		return
	}

	svcResp, err := attestationSvc.VerifyChain(ctx, &service.VerifyAttestationChainRequest{
		Format:   req.Format,
		ChainPEM: req.ChainPEM,
		Options:  req.Options.toService(),
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AttestationVerifyChainRESTResponse{
		Valid:  svcResp.Valid,
		Reason: svcResp.Reason,
	})
}

// HandleAttestationAttestContainer handles
// POST /api/v1/attestation/attest-container — crosses the orchestration seam
// to the registered container attestor.
func (s *Server) HandleAttestationAttestContainer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AttestContainerRESTRequest
	if !decodeAttestationBody(w, r, &req) {
		return
	}
	if req.Image == "" {
		RespondBadRequest(w, r, "image is required")
		return
	}

	attestationSvc := s.service.Attestation()
	if attestationSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrAttestationServiceUnavailable)
		return
	}

	svcResp, err := attestationSvc.AttestContainer(ctx, &service.AttestContainerRequest{
		DeviceID:        req.DeviceID,
		ContainerID:     req.ContainerID,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		TrustLabel:      req.TrustLabel,
		TrustLevel:      req.TrustLevel,
		ExpectedPCRs:    req.ExpectedPCRs,
		MeasurementPCR:  req.MeasurementPCR,
		MeasurementBank: req.MeasurementBank,
		Nonce:           req.Nonce,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &AttestContainerRESTResponse{
		Attested:          svcResp.Attested,
		DeviceID:          svcResp.DeviceID,
		ContainerID:       svcResp.ContainerID,
		Image:             svcResp.Image,
		ImageDigest:       svcResp.ImageDigest,
		ConfigDigest:      svcResp.ConfigDigest,
		TrustLabel:        svcResp.TrustLabel,
		TrustLevel:        svcResp.TrustLevel,
		Nonce:             svcResp.Nonce,
		Quoted:            svcResp.Quoted,
		Signature:         svcResp.Signature,
		PCRs:              svcResp.PCRs,
		EventLog:          svcResp.EventLog,
		Measured:          svcResp.Measured,
		MeasurementPCR:    svcResp.MeasurementPCR,
		MeasurementBank:   svcResp.MeasurementBank,
		LaunchMeasurement: svcResp.LaunchMeasurement,
	}
	if !svcResp.AttestedAt.IsZero() {
		resp.AttestedAt = svcResp.AttestedAt.UTC().Format(time.RFC3339Nano)
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// decodeAttestationBody reads and JSON-decodes a request body, responding
// with a 400 and returning false when it cannot.
func decodeAttestationBody(w http.ResponseWriter, r *http.Request, out interface{}) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, out); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}
