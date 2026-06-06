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
)

func TestCryptoRESTHandler_Encrypt_SubstitutesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/crypto/encrypt/master-key" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body service.EncryptRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(body.Plaintext) != "hello" {
			t.Errorf("unexpected plaintext %q", body.Plaintext)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.EncryptResponse{KeyID: "master-key", Ciphertext: []byte("ct")})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Crypto", "Encrypt", &service.EncryptRequest{
		KeyID:     "master-key",
		Plaintext: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if string(result.(*service.EncryptResponse).Ciphertext) != "ct" {
		t.Errorf("unexpected ciphertext")
	}
}

func TestCryptoRESTHandler_Sign_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/crypto/sign/signing-key" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.SignResponse{KeyID: "signing-key", Signature: []byte("sig")})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Crypto", "Sign", &service.SignRequest{
		KeyID:   "signing-key",
		Message: []byte("data"),
	})
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if string(result.(*service.SignResponse).Signature) != "sig" {
		t.Errorf("unexpected signature")
	}
}

func TestCryptoRESTHandler_Hash_StaticEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/crypto/hash" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.HashResponse{Hash: []byte("h")})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Crypto", "Hash", &service.HashRequest{Data: []byte("x")})
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if string(result.(*service.HashResponse).Hash) != "h" {
		t.Errorf("unexpected hash")
	}
}

func TestCryptoRESTHandler_GenerateDataKey_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/crypto/datakey/kek" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.GenerateDataKeyResponse{
			KeyID: "kek", Plaintext: []byte("pt"), CiphertextBlob: []byte("blob"),
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Crypto", "GenerateDataKey", &service.GenerateDataKeyRequest{
		KeyID: "kek",
	})
	if err != nil {
		t.Fatalf("GenerateDataKey failed: %v", err)
	}
	if string(result.(*service.GenerateDataKeyResponse).CiphertextBlob) != "blob" {
		t.Errorf("unexpected blob")
	}
}

func TestCryptoRESTHandler_Encrypt_MissingKeyID(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	defer tr.Close()
	_, err := tr.Request(context.Background(), "quicbaas.Crypto", "Encrypt", &service.EncryptRequest{Plaintext: []byte("x")})
	if err == nil {
		t.Fatal("expected error for missing key ID")
	}
}

func TestCryptoRESTHandler_Encrypt_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad","code":"invalid_input"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Crypto", "Encrypt", &service.EncryptRequest{
		KeyID: "k", Plaintext: []byte("x"),
	})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestCryptoRESTHandler_Sign_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Crypto", "Sign", &service.SignRequest{
		KeyID: "k", Message: []byte("x"),
	})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
