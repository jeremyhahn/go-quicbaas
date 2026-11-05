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
	"bytes"
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// qrdbKVServer mirrors go-qrdb's native qrdb_put, qrdb_get and qrdb_delete
// tools (go-qrdb pkg/api/mcp tools_kv.go): a required string "key", a
// required base64 "value" on put, and plain-text answers.
type qrdbKVServer struct {
	mu    sync.Mutex
	store map[string][]byte
	args  map[string]map[string]interface{}
	// getAnswer, when set, replaces qrdb_get's answer text.
	getAnswer string
}

func (s *qrdbKVServer) record(tool string, req mcp.CallToolRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.args[tool] = req.GetArguments()
}

func (s *qrdbKVServer) put(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_put", req)
	key := mcp.ParseString(req, "key", "")
	if key == "" {
		return mcp.NewToolResultError("key is required"), nil
	}
	valueArg := mcp.ParseString(req, "value", "")
	if valueArg == "" {
		return mcp.NewToolResultError("value is required"), nil
	}
	value, err := base64.StdEncoding.DecodeString(valueArg)
	if err != nil {
		return mcp.NewToolResultError("value is not base64"), nil
	}
	s.mu.Lock()
	s.store[key] = value
	s.mu.Unlock()
	return mcp.NewToolResultText("ok"), nil
}

func (s *qrdbKVServer) get(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_get", req)
	key := mcp.ParseString(req, "key", "")
	if key == "" {
		return mcp.NewToolResultError("key is required"), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getAnswer != "" {
		return mcp.NewToolResultText(s.getAnswer), nil
	}
	value, ok := s.store[key]
	if !ok {
		return mcp.NewToolResultError("key not found"), nil
	}
	return mcp.NewToolResultText(base64.StdEncoding.EncodeToString(value)), nil
}

func (s *qrdbKVServer) delete(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_delete", req)
	key := mcp.ParseString(req, "key", "")
	if key == "" {
		return mcp.NewToolResultError("key is required"), nil
	}
	s.mu.Lock()
	delete(s.store, key)
	s.mu.Unlock()
	return mcp.NewToolResultText("ok"), nil
}

func newQRDBKVTransport(t *testing.T) (*Transport, *qrdbKVServer) {
	t.Helper()
	kv := &qrdbKVServer{store: map[string][]byte{}, args: map[string]map[string]interface{}{}}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcp.NewTool("qrdb_put"), kv.put)
	srv.AddTool(mcp.NewTool("qrdb_get"), kv.get)
	srv.AddTool(mcp.NewTool("qrdb_delete"), kv.delete)
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		httpSrv.Close()
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		httpSrv.Close()
	})
	return tr, kv
}

func TestKV_PutGetDelete_RoundTripOverQRDBTools(t *testing.T) {
	tr, kv := newQRDBKVTransport(t)
	ctx := context.Background()
	data := []byte("hello qrdb")

	putResp, err := tr.Request(ctx, "quicbaas.KV", "Put",
		&service.PutRequest{EntityType: "users", ID: 7, Data: data})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := putResp.(*service.PutResponse).ID; got != 7 {
		t.Errorf("Put ID = %d, want 7", got)
	}
	wantPut := map[string]interface{}{"key": "users/7", "value": base64.StdEncoding.EncodeToString(data)}
	if len(kv.args["qrdb_put"]) != len(wantPut) ||
		kv.args["qrdb_put"]["key"] != wantPut["key"] || kv.args["qrdb_put"]["value"] != wantPut["value"] {
		t.Errorf("qrdb_put args = %v, want %v", kv.args["qrdb_put"], wantPut)
	}

	getResp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := getResp.(*service.GetResponse)
	if !got.Found || got.ID != 7 || !bytes.Equal(got.Data, data) {
		t.Errorf("Get = %+v, want found ID 7 data %q", got, data)
	}
	if len(kv.args["qrdb_get"]) != 1 || kv.args["qrdb_get"]["key"] != "users/7" {
		t.Errorf("qrdb_get args = %v, want only key users/7", kv.args["qrdb_get"])
	}

	delResp, err := tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !delResp.(*service.DeleteResponse).Deleted {
		t.Error("Delete reported not deleted")
	}
	if len(kv.args["qrdb_delete"]) != 1 || kv.args["qrdb_delete"]["key"] != "users/7" {
		t.Errorf("qrdb_delete args = %v, want only key users/7", kv.args["qrdb_delete"])
	}
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7}); err == nil {
		t.Error("Get after Delete succeeded, want the tool's not-found error")
	}
}

func TestKV_Errors(t *testing.T) {
	tr, kv := newQRDBKVTransport(t)
	ctx := context.Background()

	// A request that is not the typed SDK struct passes through unchanged.
	if _, err := tr.Request(ctx, "quicbaas.KV", "Put", map[string]interface{}{"key": "raw"}); err == nil {
		t.Error("raw Put without a value succeeded, want the tool's value-required error")
	}
	if kv.args["qrdb_put"]["key"] != "raw" {
		t.Errorf("raw Put args = %v, want key raw passed through", kv.args["qrdb_put"])
	}

	kv.getAnswer = "not base64!"
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1}); err == nil {
		t.Error("Get with a non-base64 answer succeeded, want an error")
	}
}
