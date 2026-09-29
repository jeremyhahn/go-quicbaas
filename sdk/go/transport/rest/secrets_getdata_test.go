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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// TestSecretsRESTHandler_GetWithBytes drives Client.GetWithBytes over REST.
// The lent value is compared in place inside fn, never copied out
// (testing-guidelines.md).
func TestSecretsRESTHandler_GetWithBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/secrets/bgp/peer1" || r.URL.Query().Get("version") != "5" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.GetSecretResponse{
			SecretID: "bgp/peer1",
			Version:  5,
			Data:     []byte(`{"md5_password":"p\"w\\d"}`),
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	matched := false
	err := secretsclient.NewClient(tr).GetWithBytes(context.Background(), "bgp/peer1", "md5_password",
		func(b []byte) error {
			matched = bytes.Equal(b, []byte(`p"w\d`))
			return nil
		}, secretsclient.WithVersion(5))
	if err != nil {
		t.Fatalf("GetWithBytes: %v", err)
	}
	if !matched {
		t.Error("lent bytes differ from the stored field")
	}
}

func TestSecretsRESTHandler_GetData_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"c2VjcmV0",bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.Secrets", "GetData", &struct {
		Path    string
		Version int
	}{}); err == nil {
		t.Error("expected error for an empty path")
	}

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "GetData", &struct {
		Path    string
		Version int
	}{Path: "k"})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if bytes.Contains([]byte(err.Error()), []byte("c2VjcmV0")) || bytes.Contains([]byte(err.Error()), []byte("bad")) {
		t.Errorf("decode error quotes the response body: %v", err)
	}
}

func TestWipeBytes(t *testing.T) {
	b := []byte("material")
	wipeBytes(b)
	if !bytes.Equal(b, make([]byte, len(b))) {
		t.Error("wipeBytes left non-zero bytes")
	}
	wipeBytes(nil)
}
