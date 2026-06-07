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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestACMERESTHandler_RegisterChallenge_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/acme/register-challenge" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req acmeclient.RegisterChallengeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Name != "http-01" {
			t.Errorf("expected http-01, got %s", req.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acmeclient.RegisterChallengeResponse{Registered: true})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.ACME", "RegisterChallenge",
		&acmeclient.RegisterChallengeRequest{Name: "http-01"})
	if err != nil {
		t.Fatalf("RegisterChallenge failed: %v", err)
	}
	resp, ok := result.(*acmeclient.RegisterChallengeResponse)
	if !ok {
		t.Fatalf("expected *acme.RegisterChallengeResponse, got %T", result)
	}
	if !resp.Registered {
		t.Errorf("expected registered=true")
	}
}

func TestACMERESTHandler_Issue_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/acme/issue" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req acmeclient.IssueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(req.CSRPEM) != "CSR" || req.ChallengeType != "http-01" {
			t.Errorf("unexpected issue request: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acmeclient.IssueResponse{
			CertificatePEM: []byte("CERT"),
			ChainPEM:       []byte("CHAIN"),
			SerialNumber:   "ff",
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.ACME", "Issue",
		&acmeclient.IssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	resp, ok := result.(*acmeclient.IssueResponse)
	if !ok {
		t.Fatalf("expected *acme.IssueResponse, got %T", result)
	}
	if string(resp.CertificatePEM) != "CERT" || resp.SerialNumber != "ff" {
		t.Errorf("unexpected issue response: %+v", resp)
	}
}

// TestACMERESTHandler_ProtocolClientVerbsUnsupported verifies the
// protocol-client verbs are rejected with a typed unimplemented error rather
// than being routed to a nonexistent server endpoint — they are not transport
// RPCs.
func TestACMERESTHandler_ProtocolClientVerbsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached for protocol-client verb, got %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	for _, method := range []string{"NewOrder", "FinalizeOrder", "Renew"} {
		_, err := tr.Request(context.Background(), "quicbaas.ACME", method, nil)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", method)
		}
		if sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
			t.Errorf("%s: expected unimplemented error, got %v", method, err)
		}
	}
}
