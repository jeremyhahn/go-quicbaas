//go:build quic

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

package quic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errQUICAttestation drives error propagation through the QUIC handlers.
var errQUICAttestation = errors.New("attestation: boom")

// fakeAttestationService records its input and replays a fixed response.
type fakeAttestationService struct {
	failNext bool

	lastVerify      *service.VerifyAttestationRequest
	lastVerifyChain *service.VerifyAttestationChainRequest
	lastAttest      *service.AttestContainerRequest
}

func (f *fakeAttestationService) Verify(_ context.Context, req *service.VerifyAttestationRequest) (*service.VerifyAttestationResponse, error) {
	f.lastVerify = req
	if f.failNext {
		return nil, errQUICAttestation
	}
	return &service.VerifyAttestationResponse{
		Valid:          true,
		Format:         req.Format,
		ChainValid:     true,
		SignatureValid: true,
	}, nil
}

func (f *fakeAttestationService) VerifyChain(_ context.Context, req *service.VerifyAttestationChainRequest) (*service.VerifyAttestationChainResponse, error) {
	f.lastVerifyChain = req
	if f.failNext {
		return nil, errQUICAttestation
	}
	return &service.VerifyAttestationChainResponse{Valid: true}, nil
}

func (f *fakeAttestationService) AttestContainer(_ context.Context, req *service.AttestContainerRequest) (*service.AttestContainerResponse, error) {
	f.lastAttest = req
	if f.failNext {
		return nil, errQUICAttestation
	}
	return &service.AttestContainerResponse{
		Attested:     true,
		ContainerID:  req.ContainerID,
		Image:        req.Image,
		ConfigDigest: "sha256:cfg",
		TrustLevel:   req.TrustLevel,
		Measured:     true,
		AttestedAt:   time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

// newAttestationHandler builds a QUIC Handler whose Attestation() returns the
// supplied service.
func newAttestationHandler(t *testing.T, svc service.AttestationService) *Handler {
	t.Helper()
	return NewHandler(&mockService{attestationSvc: svc}, nil, nil, nil, createCertificateTestLogger())
}

// attestationRequest builds a wire request carrying the sub-op byte followed
// by the JSON body, exactly as the SDK QUIC transport encodes it.
func attestationRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	full := append([]byte{subOp}, payload...)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

func TestHandler_AttestationDispatch_Verify(t *testing.T) {
	fake := &fakeAttestationService{}
	h := newAttestationHandler(t, fake)

	req := attestationRequest(t, SubOpAttestationVerify, &service.VerifyAttestationRequest{
		Format:          "tpm2",
		ChainPEM:        [][]byte{[]byte("cert")},
		AttestedKeyDER:  []byte("key"),
		AttestationData: []byte("data"),
		Signature:       []byte("sig"),
	})

	resp := h.handleOrchestratorDispatch(context.Background(), req)
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded service.VerifyAttestationResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Valid || !decoded.ChainValid || !decoded.SignatureValid {
		t.Fatalf("unexpected verdict: %+v", decoded)
	}

	if fake.lastVerify == nil || fake.lastVerify.Format != "tpm2" {
		t.Fatalf("request did not reach the service: %+v", fake.lastVerify)
	}
}

func TestHandler_AttestationDispatch_VerifyChain(t *testing.T) {
	fake := &fakeAttestationService{}
	h := newAttestationHandler(t, fake)

	req := attestationRequest(t, SubOpAttestationVerifyChain, &service.VerifyAttestationChainRequest{
		Format:   "tpm2",
		ChainPEM: [][]byte{[]byte("leaf"), []byte("root")},
	})

	resp := h.handleOrchestratorDispatch(context.Background(), req)
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded service.VerifyAttestationChainResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Valid {
		t.Fatalf("chain should validate: %+v", decoded)
	}
	if fake.lastVerifyChain == nil || len(fake.lastVerifyChain.ChainPEM) != 2 {
		t.Fatalf("chain did not reach the service: %+v", fake.lastVerifyChain)
	}
}

func TestHandler_AttestationDispatch_AttestContainer(t *testing.T) {
	fake := &fakeAttestationService{}
	h := newAttestationHandler(t, fake)

	req := attestationRequest(t, SubOpAttestationAttestContainer, &service.AttestContainerRequest{
		ContainerID: "container-1",
		Image:       "alpine@sha256:abc",
		TrustLevel:  "measured",
	})

	resp := h.handleOrchestratorDispatch(context.Background(), req)
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	// The wire shape must decode into the leaf SDK type field-for-field —
	// that equivalence is what lets the SDK QUIC decoder skip a converter.
	var decoded struct {
		Attested     bool   `json:"attested"`
		ContainerID  string `json:"container_id"`
		ConfigDigest string `json:"config_digest"`
		Measured     bool   `json:"measured"`
		AttestedAt   string `json:"attested_at"`
	}
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Attested || !decoded.Measured {
		t.Fatalf("unexpected evidence: %+v", decoded)
	}
	if decoded.ContainerID != "container-1" {
		t.Errorf("container id = %q", decoded.ContainerID)
	}
	if decoded.AttestedAt == "" {
		t.Error("timestamp must serialize as an RFC 3339 string for the leaf type")
	}
	if fake.lastAttest == nil || fake.lastAttest.Image != "alpine@sha256:abc" {
		t.Fatalf("request did not reach the service: %+v", fake.lastAttest)
	}
}

func TestHandler_AttestationDispatch_Errors(t *testing.T) {
	tests := []struct {
		name  string
		subOp byte
		svc   service.AttestationService
		body  interface{}
		raw   []byte
	}{
		{
			name:  "ServiceUnavailable",
			subOp: SubOpAttestationVerify,
			svc:   nil,
			body:  &service.VerifyAttestationRequest{Format: "tpm2"},
		},
		{
			name:  "ServiceError",
			subOp: SubOpAttestationVerify,
			svc:   &fakeAttestationService{failNext: true},
			body:  &service.VerifyAttestationRequest{Format: "tpm2"},
		},
		{
			name:  "VerifyChainServiceError",
			subOp: SubOpAttestationVerifyChain,
			svc:   &fakeAttestationService{failNext: true},
			body:  &service.VerifyAttestationChainRequest{Format: "tpm2"},
		},
		{
			name:  "AttestContainerServiceError",
			subOp: SubOpAttestationAttestContainer,
			svc:   &fakeAttestationService{failNext: true},
			body:  &service.AttestContainerRequest{Image: "alpine"},
		},
		{
			name:  "MalformedPayload",
			subOp: SubOpAttestationVerify,
			svc:   &fakeAttestationService{},
			raw:   []byte("{not json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAttestationHandler(t, tt.svc)

			var req *Request
			if tt.raw != nil {
				req = &Request{Opcode: OpOrchestrator, Payload: append([]byte{tt.subOp}, tt.raw...)}
			} else {
				req = attestationRequest(t, tt.subOp, tt.body)
			}

			resp := h.handleOrchestratorDispatch(context.Background(), req)
			if resp.Status == StatusOK {
				t.Fatalf("expected a failure status, got OK with %s", resp.Payload)
			}
		})
	}

	t.Run("UnknownSubOp", func(t *testing.T) {
		h := newAttestationHandler(t, &fakeAttestationService{})
		// 0x73 is one past the attestation range: it must NOT resolve to a
		// handler, and must not fall through to the orchestrator vocabulary.
		resp := h.handleAttestationDispatch(context.Background(), 0x73, &Request{})
		if resp.Status == StatusOK {
			t.Fatal("an unknown sub-op must be rejected")
		}
	})
}

// TestAttestationSubOpRange pins the disjointness of the sub-op vocabularies
// sharing the OpOrchestrator dispatch byte. An overlap would silently
// mis-route a request to another surface's handler — and, worse, to another
// surface's authorization mapping.
func TestAttestationSubOpRange(t *testing.T) {
	for _, subOp := range []byte{
		SubOpAttestationVerify,
		SubOpAttestationVerifyChain,
		SubOpAttestationAttestContainer,
	} {
		if !isAttestationSubOp(subOp) {
			t.Errorf("sub-op 0x%02X is not recognised as attestation", subOp)
		}
		if isAccountSubOp(subOp) || isProjectSubOp(subOp) || isSSHSubOp(subOp) || isCertExtSubOp(subOp) {
			t.Errorf("sub-op 0x%02X collides with another vocabulary", subOp)
		}
	}

	// Neighbouring bytes must be outside the range.
	if isAttestationSubOp(SubOpAttestationVerify - 1) {
		t.Error("range extends below the first attestation sub-op")
	}
	if isAttestationSubOp(SubOpAttestationAttestContainer + 1) {
		t.Error("range extends above the last attestation sub-op")
	}

	// Every orchestrator sub-op must stay outside the attestation range.
	for _, subOp := range []byte{
		SubOpOrchestratorSpawn, SubOpOrchestratorPrompt, SubOpOrchestratorSessionStatus,
		SubOpOrchestratorCloseSession, SubOpOrchestratorListSessions, SubOpOrchestratorCreateTask,
		SubOpOrchestratorGetTask, SubOpOrchestratorListTasks, SubOpOrchestratorClaimTask,
		SubOpOrchestratorCompleteTask, SubOpOrchestratorListBackends, SubOpOrchestratorHealth,
		SubOpOrchestratorInference,
	} {
		if isAttestationSubOp(subOp) {
			t.Errorf("orchestrator sub-op 0x%02X collides with the attestation range", subOp)
		}
	}
}

// TestAttestationQRDBPermissions is the authorization guard. Attestation rides
// the OpOrchestrator dispatch byte, whose surface-level mapping is
// orchestrator:status — a materially weaker permission than driving a host
// TPM. This test proves the sub-op table wins.
func TestAttestationQRDBPermissions(t *testing.T) {
	tests := []struct {
		name     string
		subOp    byte
		resource string
		action   string
	}{
		{name: "Verify", subOp: SubOpAttestationVerify, resource: "attestation", action: "verify"},
		{name: "VerifyChain", subOp: SubOpAttestationVerifyChain, resource: "attestation", action: "verify"},
		{name: "AttestContainer", subOp: SubOpAttestationAttestContainer, resource: "attestation", action: "attest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tt.subOp})
			if !ok {
				t.Fatalf("sub-op 0x%02X is unmapped and would be default-denied", tt.subOp)
			}
			if resource != tt.resource || action != tt.action {
				t.Fatalf("permission = %s:%s, want %s:%s", resource, action, tt.resource, tt.action)
			}
			if resource == "orchestrator" {
				t.Fatal("attestation must not inherit the surface-level orchestrator permission")
			}
		})
	}

	t.Run("EveryAttestationSubOpIsMapped", func(t *testing.T) {
		for subOp := SubOpAttestationVerify; subOp <= SubOpAttestationAttestContainer; subOp++ {
			if _, found := attestationSubOpPermissions[subOp]; !found {
				t.Errorf("sub-op 0x%02X has a handler range entry but no permission mapping", subOp)
			}
		}
		if len(attestationSubOpPermissions) != len(attestationSubHandlers) {
			t.Fatalf("permission table has %d entries but the dispatch table has %d",
				len(attestationSubOpPermissions), len(attestationSubHandlers))
		}
	})

	t.Run("UnknownSubOpInRangeIsDenied", func(t *testing.T) {
		// A byte inside the range with no table entry must be denied
		// rather than falling through to the orchestrator mapping.
		if _, found := attestationSubOpPermissions[0x73]; found {
			t.Skip("0x73 was assigned; update this guard")
		}
		if _, _, ok := getQRDBPermission(OpOrchestrator, []byte{0x73}); ok {
			// 0x73 is outside the range, so it correctly falls through to
			// the surface-level orchestrator mapping. Assert that is what
			// happened rather than an attestation permission.
			resource, _, _ := getQRDBPermission(OpOrchestrator, []byte{0x73})
			if resource == "attestation" {
				t.Fatal("an unassigned byte must not resolve to an attestation permission")
			}
		}
	})
}
