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

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestSecretsGetWithBytes_RefusedOverMCP pins that MCP fails closed on the
// protected secrets read without contacting the server: an MCP tool result
// is a text string, so the secret could not be kept out of plain memory.
func TestSecretsGetWithBytes_RefusedOverMCP(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tr, err := New(&transport.Config{Address: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	called := false
	err = secretsclient.NewClient(tr).GetWithBytes(context.Background(), "bgp/peer1", "password",
		func([]byte) error {
			called = true
			return nil
		})
	if err == nil {
		t.Fatal("expected GetWithBytes to be refused over MCP")
	}
	if errors.Code(err) != errors.CodeUnimplemented {
		t.Errorf("err = %v, want a CodeUnimplemented QBError", err)
	}
	if called {
		t.Error("fn ran although the read was refused")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server contacted %d times; the refusal must precede any I/O", n)
	}
}

func TestIsSecretsGetData(t *testing.T) {
	cases := []struct {
		service, method string
		want            bool
	}{
		{"quicbaas.Secrets", "GetData", true},
		{"quicbaas.Secrets", "Get", false},
		{"quicbaas.DynamicSecrets", "GetData", false},
	}
	for _, tc := range cases {
		if got := isSecretsGetData(tc.service, tc.method); got != tc.want {
			t.Errorf("isSecretsGetData(%q, %q) = %v, want %v", tc.service, tc.method, got, tc.want)
		}
	}
}
