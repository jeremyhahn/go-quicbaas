// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestCryptoHandlerMethodsHappyPaths drives the Crypto methods that previously
// lacked coverage, asserting the HTTP method and path each builds.
func TestCryptoHandlerMethodsHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"Decrypt", "Decrypt",
			&service.DecryptRequest{KeyID: "k1"},
			http.MethodPost, "/api/v1/crypto/decrypt/k1"},
		{"Rewrap", "Rewrap",
			&service.RewrapRequest{KeyID: "k1"},
			http.MethodPost, "/api/v1/crypto/rewrap/k1"},
		{"Verify", "Verify",
			&service.VerifyRequest{KeyID: "k1"},
			http.MethodPost, "/api/v1/crypto/verify/k1"},
		{"GenerateRandom", "GenerateRandom",
			&service.GenerateRandomRequest{NumBytes: 16},
			http.MethodPost, "/api/v1/crypto/random"},
		{"EncryptBatch", "EncryptBatch",
			&service.EncryptBatchRequest{KeyID: "k1"},
			http.MethodPost, "/api/v1/crypto/encrypt/k1/batch"},
		{"DecryptBatch", "DecryptBatch",
			&service.DecryptBatchRequest{KeyID: "k1"},
			http.MethodPost, "/api/v1/crypto/decrypt/k1/batch"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			resp, err := tr.Request(ctx, "Crypto", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestCryptoHandlerMethodsValidation exercises the request-required and
// key-ID-required guards.
func TestCryptoHandlerMethodsValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"DecryptBadType", "Decrypt", "x"},
		{"DecryptNoKey", "Decrypt", &service.DecryptRequest{}},
		{"RewrapBadType", "Rewrap", 1},
		{"RewrapNoKey", "Rewrap", &service.RewrapRequest{}},
		{"VerifyBadType", "Verify", "x"},
		{"VerifyNoKey", "Verify", &service.VerifyRequest{}},
		{"GenerateRandomBadType", "GenerateRandom", "x"},
		{"EncryptBatchBadType", "EncryptBatch", "x"},
		{"EncryptBatchNoKey", "EncryptBatch", &service.EncryptBatchRequest{}},
		{"DecryptBatchBadType", "DecryptBatch", "x"},
		{"DecryptBatchNoKey", "DecryptBatch", &service.DecryptBatchRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "Crypto", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestCryptoHandlerUnknownMethodFallback verifies an unknown method falls
// through to the generic retry path.
func TestCryptoHandlerUnknownMethodFallback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Crypto", "GenerateHMAC", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/crypto/generatehmac" {
		t.Errorf("unexpected fallback path: %s", gotPath)
	}
}
