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
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

func TestSecretsRESTHandler_Store_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/secrets/app/db/password" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body secretsStoreWireRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(service.StoreSecretResponse{SecretID: "app/db/password", Version: 4})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Secrets", "Store", &secretsclient.StoreSecretRequest{
		Path: "app/db/password",
		Data: map[string]interface{}{"password": "s3cret"},
	})
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	resp := result.(*secretsclient.StoreSecretResponse)
	if resp.Version != 4 {
		t.Errorf("expected version 4, got %d", resp.Version)
	}
}

func TestSecretsRESTHandler_Get_DecodesData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/secrets/app/key" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.GetSecretResponse{
			SecretID: "app/key",
			Version:  2,
			Data:     []byte(`{"token":"abc"}`),
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Secrets", "Get", &struct {
		Path    string
		Version int
	}{Path: "app/key"})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	secret := result.(*secretsclient.Secret)
	if secret.Path != "app/key" || secret.Version != 2 {
		t.Errorf("unexpected secret: %+v", secret)
	}
	if secret.Data["token"] != "abc" {
		t.Errorf("expected token 'abc', got %v", secret.Data["token"])
	}
}

func TestSecretsRESTHandler_List_ExtractsPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/secrets" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.ListSecretsResponse{
			Secrets: []*service.Secret{{SecretID: "a", Name: "alpha"}, {SecretID: "b"}},
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Secrets", "List", &struct{ Prefix string }{Prefix: ""})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	resp := result.(*secretsclient.ListSecretsResponse)
	if len(resp.Paths) != 2 || resp.Paths[0] != "alpha" || resp.Paths[1] != "b" {
		t.Errorf("unexpected paths: %v", resp.Paths)
	}
}

func TestSecretsRESTHandler_Rotate_UsesActionQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") != "rotate" {
			t.Errorf("expected action=rotate, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.RotateSecretResponse{SecretID: "k", OldVersion: 1, NewVersion: 2})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Secrets", "Rotate", &secretsclient.RotateSecretRequest{
		SecretID: "k",
		NewData:  []byte("new"),
	})
	if err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}
	if result.(*service.RotateSecretResponse).NewVersion != 2 {
		t.Errorf("expected new version 2")
	}
}

func TestSecretsRESTHandler_Delete_ReturnsNilOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.DeleteSecretResponse{})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Secrets", "Delete", &struct{ Path string }{Path: "k"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestSecretsRESTHandler_Store_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"denied","code":"forbidden"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "Store", &secretsclient.StoreSecretRequest{
		Path: "k", Data: map[string]interface{}{"a": 1},
	})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestSecretsRESTHandler_Get_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "Get", &struct {
		Path    string
		Version int
	}{Path: "k"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
