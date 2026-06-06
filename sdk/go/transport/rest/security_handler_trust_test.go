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

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
)

func TestTrustRESTHandler_CrossSignedRootPool_HappyPath(t *testing.T) {
	rootPEM := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/trust/cross-signed-roots" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			RootPEMs [][]byte `json:"root_pems"`
		}{RootPEMs: [][]byte{rootPEM}})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	if err != nil {
		t.Fatalf("CrossSignedRootPool failed: %v", err)
	}
	resp, ok := result.(*trustclient.CrossSignedRootPoolResponse)
	if !ok {
		t.Fatalf("expected *trust.CrossSignedRootPoolResponse, got %T", result)
	}
	if len(resp.RootPEMs) != 1 || string(resp.RootPEMs[0]) != string(rootPEM) {
		t.Errorf("root pems mismatch: %v", resp.RootPEMs)
	}
}

func TestTrustRESTHandler_AutoImportAIA_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/trust/auto-import-aia" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			ChainDER [][]byte `json:"chain_der"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Imported int `json:"imported"`
		}{Imported: len(req.ChainDER)})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA",
		&trustclient.AutoImportAIARequest{ChainDER: [][]byte{{0x30, 0x82}, {0x30, 0x82}}})
	if err != nil {
		t.Fatalf("AutoImportAIA failed: %v", err)
	}
	resp, ok := result.(*trustclient.AutoImportAIAResponse)
	if !ok {
		t.Fatalf("expected *trust.AutoImportAIAResponse, got %T", result)
	}
	if resp.Imported != 2 {
		t.Errorf("expected imported=2, got %d", resp.Imported)
	}
}

func TestTrustRESTHandler_AutoImportAIA_EmptyChain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be reached for empty chain")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA",
		&trustclient.AutoImportAIARequest{})
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
}

func TestTrustRESTHandler_PublishDANE_Unimplemented(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Trust", "PublishDANE", struct{}{})
	if err == nil {
		t.Fatal("expected unimplemented error for PublishDANE")
	}
}

func TestTrustRESTHandler_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}
