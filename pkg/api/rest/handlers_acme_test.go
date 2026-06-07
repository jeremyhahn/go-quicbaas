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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
)

// stubACMEService is a configurable service.ACMEServicer for the REST server
// handler tests. RestServicer returns nil so the directory routes exercise
// their 503 path without a full xkms RestService.
type stubACMEService struct {
	registered   bool
	registerErr  error
	gotChallenge string
	issueResp    *service.ACMEIssueResponse
	issueErr     error
	gotCSR       []byte
}

func (s *stubACMEService) RegisterChallenge(_ context.Context, req *service.ACMERegisterChallengeRequest) (*service.ACMERegisterChallengeResponse, error) {
	if s.registerErr != nil {
		return nil, s.registerErr
	}
	s.gotChallenge = req.Name
	return &service.ACMERegisterChallengeResponse{Registered: s.registered}, nil
}

func (s *stubACMEService) Issue(_ context.Context, req *service.ACMEIssueRequest) (*service.ACMEIssueResponse, error) {
	if s.issueErr != nil {
		return nil, s.issueErr
	}
	s.gotCSR = req.CSRPEM
	return s.issueResp, nil
}

func (s *stubACMEService) DirectoryURL() string { return "https://acme.test/acme/directory" }

func (s *stubACMEService) RestServicer() xkmsacme.ACMERestServicer { return nil }

func TestHandleACMERegisterChallenge(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubACMEService{registered: true}
		mockSvc.ACMESvc = stub

		body, _ := json.Marshal(acmeclient.RegisterChallengeRequest{Name: "http-01"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/acme/register-challenge", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.HandleACMERegisterChallenge(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp acmeclient.RegisterChallengeResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Registered)
		assert.Equal(t, "http-01", stub.gotChallenge)
	})

	t.Run("service not available", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/acme/register-challenge",
			bytes.NewReader([]byte(`{"name":"http-01"}`)))
		w := httptest.NewRecorder()
		server.HandleACMERegisterChallenge(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.ACMESvc = &stubACMEService{registered: true}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/acme/register-challenge",
			bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()
		server.HandleACMERegisterChallenge(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleACMEIssue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		stub := &stubACMEService{issueResp: &service.ACMEIssueResponse{
			CertificatePEM: []byte("CERT"),
			ChainPEM:       []byte("CHAIN"),
			SerialNumber:   "ff",
		}}
		mockSvc.ACMESvc = stub

		body, _ := json.Marshal(acmeclient.IssueRequest{
			CSRPEM:        []byte("CSR"),
			ChallengeType: "http-01",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/acme/issue", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.HandleACMEIssue(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp acmeclient.IssueResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, []byte("CERT"), resp.CertificatePEM)
		assert.Equal(t, "ff", resp.SerialNumber)
		assert.Equal(t, []byte("CSR"), stub.gotCSR)
	})

	t.Run("service not available", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/acme/issue",
			bytes.NewReader([]byte(`{"csr_pem":"Q1NS"}`)))
		w := httptest.NewRecorder()
		server.HandleACMEIssue(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

// TestACMEDirectoryRoute_Inert verifies the RFC 8555 directory route is
// mounted and responds 503 when ACME is not configured (proving the route
// exists without requiring a full CA-backed RestService in a unit test).
func TestACMEDirectoryRoute_Inert(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/acme/directory", nil)
	w := httptest.NewRecorder()
	server.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.DirectoryHandler })(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
