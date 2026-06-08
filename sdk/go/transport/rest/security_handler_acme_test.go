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

// TestACMERESTHandler_NewOrder_HappyPath verifies the broker-as-CA NewOrder
// verb routes over POST /api/v1/acme/new-order.
func TestACMERESTHandler_NewOrder_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/acme/new-order" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req acmeclient.NewOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(req.Identifiers) != 1 || req.Identifiers[0].Value != "example.com" {
			t.Errorf("unexpected identifiers: %+v", req.Identifiers)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acmeclient.NewOrderResponse{
			OrderID:     7,
			Status:      "pending",
			Identifiers: req.Identifiers,
			Finalize:    "https://acme.test/acme/order/7/finalize",
			URL:         "https://acme.test/acme/order/7",
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.ACME", "NewOrder",
		&acmeclient.NewOrderRequest{
			Identifiers:   []acmeclient.OrderIdentifier{{Type: "dns", Value: "example.com"}},
			ChallengeType: "http-01",
		})
	if err != nil {
		t.Fatalf("NewOrder failed: %v", err)
	}
	resp, ok := result.(*acmeclient.NewOrderResponse)
	if !ok {
		t.Fatalf("expected *acme.NewOrderResponse, got %T", result)
	}
	if resp.OrderID != 7 || resp.Finalize == "" {
		t.Errorf("unexpected order: %+v", resp)
	}
}

// TestACMERESTHandler_FinalizeOrder_HappyPath verifies FinalizeOrder routes
// over POST /api/v1/acme/finalize and returns the issued chain.
func TestACMERESTHandler_FinalizeOrder_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/acme/finalize" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req acmeclient.FinalizeOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(req.CSRPEM) != "CSR" {
			t.Errorf("unexpected csr: %q", req.CSRPEM)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acmeclient.FinalizeOrderResponse{
			CertificatePEM: []byte("CERT"),
			ChainPEM:       []byte("CHAIN"),
			SerialNumber:   "ff",
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.ACME", "FinalizeOrder",
		&acmeclient.FinalizeOrderRequest{OrderURL: "url", CSRPEM: []byte("CSR")})
	if err != nil {
		t.Fatalf("FinalizeOrder failed: %v", err)
	}
	resp, ok := result.(*acmeclient.FinalizeOrderResponse)
	if !ok {
		t.Fatalf("expected *acme.FinalizeOrderResponse, got %T", result)
	}
	if string(resp.CertificatePEM) != "CERT" || resp.SerialNumber != "ff" {
		t.Errorf("unexpected finalize response: %+v", resp)
	}
}

// TestACMERESTHandler_Renew_PropagatesError verifies the Renew verb routes over
// POST /api/v1/acme/renew and surfaces the server's typed needs-CSR error.
func TestACMERESTHandler_Renew_PropagatesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/acme/renew" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req acmeclient.RenewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.CertURL != "ff" {
			t.Errorf("unexpected cert url: %q", req.CertURL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "renewal requires a fresh CSR via NewOrder/FinalizeOrder",
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "Renew",
		&acmeclient.RenewRequest{CertURL: "ff"})
	if err == nil {
		t.Fatal("expected error from renew, got nil")
	}
}

// TestACMERESTHandler_UnknownMethod verifies an unrecognised ACME method still
// returns a typed unimplemented error.
func TestACMERESTHandler_UnknownMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached for unknown method, got %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.ACME", "Bogus", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Errorf("expected unimplemented error, got %v", err)
	}
}
