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
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubAttestationService is a configurable service.AttestationService for the
// REST server handler tests. It records its input so the JSON decoding can be
// asserted end to end.
type stubAttestationService struct {
	verifyErr      error
	verifyChainErr error
	attestErr      error

	lastVerify      *service.VerifyAttestationRequest
	lastVerifyChain *service.VerifyAttestationChainRequest
	lastAttest      *service.AttestContainerRequest
}

func (s *stubAttestationService) Verify(_ context.Context, req *service.VerifyAttestationRequest) (*service.VerifyAttestationResponse, error) {
	s.lastVerify = req
	if s.verifyErr != nil {
		return nil, s.verifyErr
	}
	return &service.VerifyAttestationResponse{
		Valid:          true,
		Format:         req.Format,
		Backend:        req.Backend,
		ChainValid:     true,
		SignatureValid: true,
	}, nil
}

func (s *stubAttestationService) VerifyChain(_ context.Context, req *service.VerifyAttestationChainRequest) (*service.VerifyAttestationChainResponse, error) {
	s.lastVerifyChain = req
	if s.verifyChainErr != nil {
		return nil, s.verifyChainErr
	}
	return &service.VerifyAttestationChainResponse{Valid: true}, nil
}

func (s *stubAttestationService) AttestContainer(_ context.Context, req *service.AttestContainerRequest) (*service.AttestContainerResponse, error) {
	s.lastAttest = req
	if s.attestErr != nil {
		return nil, s.attestErr
	}
	return &service.AttestContainerResponse{
		Attested:        true,
		DeviceID:        req.DeviceID,
		ContainerID:     req.ContainerID,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		ConfigDigest:    "sha256:cfg",
		TrustLabel:      req.TrustLabel,
		TrustLevel:      req.TrustLevel,
		Measured:        true,
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
		Quoted:          []byte("tpms-attest"),
		Signature:       []byte("sig"),
		PCRs:            map[uint32][]byte{7: {0x01}},
		AttestedAt:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

// postAttestation issues a POST with a JSON body to the supplied handler.
func postAttestation(t *testing.T, handler http.HandlerFunc, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(http.MethodPost, path, reader)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestHandleAttestationVerify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		w := postAttestation(t, server.HandleAttestationVerify, "/api/v1/attestation/verify",
			&AttestationVerifyRESTRequest{
				Format:          "tpm2",
				Backend:         "tpm2",
				ChainPEM:        [][]byte{[]byte("cert")},
				AttestedKeyDER:  []byte("key"),
				AttestationData: []byte("data"),
				Signature:       []byte("sig"),
				Options: &AttestationVerifyOptionsREST{
					CheckFreshness:  true,
					AllowSelfSigned: true,
				},
			})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp AttestationVerifyRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChainValid)
		assert.True(t, resp.SignatureValid)

		require.NotNil(t, stub.lastVerify)
		assert.Equal(t, []byte("data"), stub.lastVerify.AttestationData)
		require.NotNil(t, stub.lastVerify.Options)
		assert.True(t, stub.lastVerify.Options.CheckFreshness)
	})

	// An absent options block must stay nil across the wire so the service
	// applies the xkms secure defaults instead of an all-false struct.
	t.Run("nil options stay nil", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		w := postAttestation(t, server.HandleAttestationVerify, "/api/v1/attestation/verify",
			&AttestationVerifyRESTRequest{Format: "tpm2", ChainPEM: [][]byte{[]byte("cert")}})

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, stub.lastVerify)
		assert.Nil(t, stub.lastVerify.Options)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{}

		w := postAttestation(t, server.HandleAttestationVerify, "/api/v1/attestation/verify",
			[]byte("{not json"))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = nil

		w := postAttestation(t, server.HandleAttestationVerify, "/api/v1/attestation/verify",
			&AttestationVerifyRESTRequest{Format: "tpm2"})
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{verifyErr: stderrors.New("boom")}

		w := postAttestation(t, server.HandleAttestationVerify, "/api/v1/attestation/verify",
			&AttestationVerifyRESTRequest{Format: "tpm2"})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleAttestationVerifyChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		w := postAttestation(t, server.HandleAttestationVerifyChain, "/api/v1/attestation/verify-chain",
			&AttestationVerifyChainRESTRequest{
				Format:   "tpm2",
				ChainPEM: [][]byte{[]byte("leaf"), []byte("root")},
			})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp AttestationVerifyChainRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Valid)
		require.NotNil(t, stub.lastVerifyChain)
		assert.Len(t, stub.lastVerifyChain.ChainPEM, 2)
	})

	t.Run("missing chain", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{}

		w := postAttestation(t, server.HandleAttestationVerifyChain, "/api/v1/attestation/verify-chain",
			&AttestationVerifyChainRESTRequest{Format: "tpm2"})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{}

		w := postAttestation(t, server.HandleAttestationVerifyChain, "/api/v1/attestation/verify-chain",
			[]byte("{"))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = nil

		w := postAttestation(t, server.HandleAttestationVerifyChain, "/api/v1/attestation/verify-chain",
			&AttestationVerifyChainRESTRequest{ChainPEM: [][]byte{[]byte("leaf")}})
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{verifyChainErr: stderrors.New("boom")}

		w := postAttestation(t, server.HandleAttestationVerifyChain, "/api/v1/attestation/verify-chain",
			&AttestationVerifyChainRESTRequest{ChainPEM: [][]byte{[]byte("leaf")}})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleAttestationAttestContainer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		w := postAttestation(t, server.HandleAttestationAttestContainer, "/api/v1/attestation/attest-container",
			&AttestContainerRESTRequest{
				DeviceID:     "device-1",
				ContainerID:  "container-1",
				Image:        "alpine@sha256:abc",
				ImageDigest:  "sha256:abc",
				TrustLevel:   "measured",
				ExpectedPCRs: map[uint32][]byte{7: {0x01}},
			})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp AttestContainerRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Attested)
		assert.True(t, resp.Measured)
		assert.Equal(t, "sha256:cfg", resp.ConfigDigest)
		assert.Equal(t, "2026-08-09T12:00:00Z", resp.AttestedAt,
			"the timestamp must be RFC 3339 so every transport agrees on the shape")
		assert.Len(t, resp.PCRs, 1)

		require.NotNil(t, stub.lastAttest)
		assert.Equal(t, "device-1", stub.lastAttest.DeviceID)
		assert.Len(t, stub.lastAttest.ExpectedPCRs, 1)
	})

	t.Run("missing image", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{}

		w := postAttestation(t, server.HandleAttestationAttestContainer,
			"/api/v1/attestation/attest-container", &AttestContainerRESTRequest{})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{}

		w := postAttestation(t, server.HandleAttestationAttestContainer,
			"/api/v1/attestation/attest-container", []byte("]["))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = nil

		w := postAttestation(t, server.HandleAttestationAttestContainer,
			"/api/v1/attestation/attest-container",
			&AttestContainerRESTRequest{Image: "alpine@sha256:abc"})
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	// The nil-seam contract must survive the transport: an unconfigured
	// deployment reports "not configured", not a generic 500.
	t.Run("no attestor registered", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = service.NewAttestationService(nil, nil)

		w := postAttestation(t, server.HandleAttestationAttestContainer,
			"/api/v1/attestation/attest-container",
			&AttestContainerRESTRequest{Image: "alpine@sha256:abc"})
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.AttestationSvc = &stubAttestationService{attestErr: stderrors.New("boom")}

		w := postAttestation(t, server.HandleAttestationAttestContainer,
			"/api/v1/attestation/attest-container",
			&AttestContainerRESTRequest{Image: "alpine@sha256:abc"})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestAttestationVerifyOptionsREST_ToService(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		var opts *AttestationVerifyOptionsREST
		assert.Nil(t, opts.toService())
	})

	t.Run("fields pass through", func(t *testing.T) {
		opts := (&AttestationVerifyOptionsREST{
			TrustedRootsPEM:        [][]byte{[]byte("root")},
			CheckFreshness:         true,
			ExpectedNonce:          []byte("nonce"),
			FreshnessWindowSeconds: 42,
			VerifyPCRs:             true,
			ExpectedPCRs:           map[uint32][]byte{7: {0x01}},
			AllowSelfSigned:        true,
		}).toService()

		require.NotNil(t, opts)
		assert.Len(t, opts.TrustedRootsPEM, 1)
		assert.True(t, opts.CheckFreshness)
		assert.Equal(t, int64(42), opts.FreshnessWindowSeconds)
		assert.True(t, opts.VerifyPCRs)
		assert.Len(t, opts.ExpectedPCRs, 1)
		assert.True(t, opts.AllowSelfSigned)
	})
}
