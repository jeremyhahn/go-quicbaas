// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newRPCTestTransport spins up a JSON-RPC test server that records the last
// decoded request and replies with the supplied result, then returns a
// transport pointed at it.
func newRPCTestTransport(t *testing.T, result interface{}, rpcErr *JSONRPCError) (*Transport, *atomic.Pointer[jsonRPCRequest]) {
	t.Helper()

	last := &atomic.Pointer[jsonRPCRequest]{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			return
		}
		last.Store(&req)

		resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
		if rpcErr == nil {
			resp.Result = mustMarshalJSON(result)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	tr, err := New(&transport.Config{Address: server.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	return tr, last
}

// TestTransport_CallRPC_ForwardsMethodAndParams verifies the happy path: the
// method and params reach the server verbatim and the raw result is returned
// without any tool-name mapping or implicit initialize handshake.
func TestTransport_CallRPC_ForwardsMethodAndParams(t *testing.T) {
	tr, last := newRPCTestTransport(t, map[string]interface{}{"tools": []interface{}{}}, nil)

	params := json.RawMessage(`{"name":"quicbaas_kv_get","arguments":{"key":"k1"}}`)
	result, err := tr.CallRPC(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	req := last.Load()
	if req == nil {
		t.Fatal("server never received a request")
	}
	if req.Method != "tools/call" {
		t.Errorf("expected method tools/call, got %s", req.Method)
	}

	// Params must survive the round trip byte-for-byte in structure.
	got, err := json.Marshal(req.Params)
	if err != nil {
		t.Fatalf("failed to re-marshal params: %v", err)
	}
	var want, have map[string]interface{}
	if err := json.Unmarshal(params, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(got, &have); err != nil {
		t.Fatalf("unmarshal have: %v", err)
	}
	if have["name"] != want["name"] {
		t.Errorf("expected tool name %v, got %v", want["name"], have["name"])
	}

	if len(result) == 0 {
		t.Error("expected a non-empty raw result")
	}

	// CallRPC must not perform the SDK's implicit initialize handshake.
	if tr.IsInitialized() {
		t.Error("CallRPC must not mark the session initialized")
	}
}

// TestTransport_CallRPC_OmitsEmptyParams verifies a nil params slice is sent as
// an absent member rather than the JSON literal null, which parameterless
// methods such as tools/list would otherwise reject.
func TestTransport_CallRPC_OmitsEmptyParams(t *testing.T) {
	tr, last := newRPCTestTransport(t, map[string]interface{}{"tools": []interface{}{}}, nil)

	if _, err := tr.CallRPC(context.Background(), "tools/list", nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	req := last.Load()
	if req == nil {
		t.Fatal("server never received a request")
	}
	if req.Params != nil {
		t.Errorf("expected params to be absent, got %#v", req.Params)
	}
}

// TestTransport_CallRPC_EmptyMethod verifies an empty method is rejected
// locally as invalid input instead of being sent to the server.
func TestTransport_CallRPC_EmptyMethod(t *testing.T) {
	tr, last := newRPCTestTransport(t, nil, nil)

	if _, err := tr.CallRPC(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error for an empty method")
	}
	if last.Load() != nil {
		t.Error("empty method must not reach the server")
	}
}

// TestTransport_CallRPC_ServerError verifies a JSON-RPC error response is
// surfaced as a typed error rather than an empty result.
func TestTransport_CallRPC_ServerError(t *testing.T) {
	tr, _ := newRPCTestTransport(t, nil, &JSONRPCError{
		Code:    JSONRPCMethodNotFound,
		Message: "Method not found",
	})

	result, err := tr.CallRPC(context.Background(), "does/not/exist", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown method")
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %s", result)
	}
}

// TestTransport_CallRPC_ClosedTransport verifies calls on a closed transport
// fail with a connection error instead of dialing.
func TestTransport_CallRPC_ClosedTransport(t *testing.T) {
	tr, last := newRPCTestTransport(t, nil, nil)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := tr.CallRPC(context.Background(), "tools/list", nil); err == nil {
		t.Fatal("expected an error on a closed transport")
	}
	if last.Load() != nil {
		t.Error("closed transport must not reach the server")
	}
}
