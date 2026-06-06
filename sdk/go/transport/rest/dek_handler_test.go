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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

func TestDEKRESTHandler_CreateDEK_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/kms/deks" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body service.CreateDEKRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Path != "tenant/app/key" {
			t.Errorf("expected path 'tenant/app/key', got %q", body.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(service.CreateDEKResponse{
			DEK: &service.DEKMetadata{Path: "tenant/app/key", Algorithm: "AES-256-GCM", Version: 1},
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.DEK", "CreateDEK", &dekclient.CreateDEKRequest{
		Path:      "tenant/app/key",
		Algorithm: "AES-256-GCM",
	})
	if err != nil {
		t.Fatalf("CreateDEK failed: %v", err)
	}
	resp := result.(*service.CreateDEKResponse)
	if resp.DEK == nil || resp.DEK.Path != "tenant/app/key" {
		t.Errorf("unexpected DEK: %+v", resp.DEK)
	}
}

func TestDEKRESTHandler_GetDEK_PreservesHierarchicalPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kms/deks/tenant/app/key" {
			t.Errorf("expected hierarchical path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.GetDEKResponse{DEK: &service.DEKMetadata{Path: "tenant/app/key"}})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.DEK", "GetDEK", &struct{ Path string }{Path: "tenant/app/key"})
	if err != nil {
		t.Fatalf("GetDEK failed: %v", err)
	}
	if result.(*service.GetDEKResponse).DEK.Path != "tenant/app/key" {
		t.Errorf("unexpected path")
	}
}

func TestDEKRESTHandler_DeleteDEK_ForceQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("force") != "true" {
			t.Errorf("expected force=true, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.DeleteDEKResponse{Path: "k", Deleted: true})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.DEK", "DeleteDEK", &dekclient.DeleteDEKRequest{
		Path:  "k",
		Force: true,
	})
	if err != nil {
		t.Fatalf("DeleteDEK failed: %v", err)
	}
	if !result.(*service.DeleteDEKResponse).Deleted {
		t.Errorf("expected deleted=true")
	}
}

func TestDEKRESTHandler_CreateDEK_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"exists","code":"conflict"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.DEK", "CreateDEK", &dekclient.CreateDEKRequest{Path: "k"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestDEKRESTHandler_GetDEK_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.DEK", "GetDEK", &struct{ Path string }{Path: "k"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDEKRESTHandler_GetDEK_MissingPath(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	defer tr.Close()
	_, err := tr.Request(context.Background(), "quicbaas.DEK", "GetDEK", &struct{ Path string }{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}
