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
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// qrdbKVServer mirrors the quicbaas server's qrdb_put, qrdb_get and
// qrdb_delete tools (pkg/api/mcp tools.go executePut, executeGet,
// executeDelete): an "entity_type" and numeric "id", base64 "data" on put,
// and the service response marshalled as JSON text.
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

// entity reads the entity type and ID a request addresses, and the canonical
// key the service would build for them.
func entity(req mcp.CallToolRequest) (uint64, string) {
	id := uint64(mcp.ParseFloat64(req, "id", 0))
	return id, mcp.ParseString(req, "entity_type", "") + ":" + strconv.FormatUint(id, 10)
}

func jsonAnswer(v interface{}) (*mcp.CallToolResult, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(out)), nil
}

func (s *qrdbKVServer) put(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_put", req)
	id, key := entity(req)
	value, err := base64.StdEncoding.DecodeString(mcp.ParseString(req, "data", ""))
	if err != nil {
		return mcp.NewToolResultError("data is not base64"), nil
	}
	s.mu.Lock()
	s.store[key] = value
	s.mu.Unlock()
	return jsonAnswer(&service.PutResponse{ID: id, Version: 1, Timestamp: 42})
}

func (s *qrdbKVServer) get(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_get", req)
	id, key := entity(req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getAnswer != "" {
		return mcp.NewToolResultText(s.getAnswer), nil
	}
	value, ok := s.store[key]
	return jsonAnswer(&service.GetResponse{ID: id, Data: value, Version: 1, Found: ok})
}

func (s *qrdbKVServer) delete(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_delete", req)
	_, key := entity(req)
	s.mu.Lock()
	_, ok := s.store[key]
	delete(s.store, key)
	s.mu.Unlock()
	return jsonAnswer(&service.DeleteResponse{Deleted: ok, Timestamp: 43})
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

// assertEntityArgs checks a tool received exactly the entity type, the
// numeric ID and the extra arguments, and no "key".
func assertEntityArgs(t *testing.T, tool string, got map[string]interface{}, extra map[string]interface{}) {
	t.Helper()
	if _, ok := got["key"]; ok {
		t.Errorf("%s args carry a key: %v", tool, got)
	}
	if got["entity_type"] != "users" || got["id"] != float64(7) {
		t.Errorf("%s args = %v, want entity_type users and numeric id 7", tool, got)
	}
	if len(got) != 2+len(extra) {
		t.Errorf("%s args = %v, want only entity_type, id and %v", tool, got, extra)
	}
	for name, want := range extra {
		if got[name] != want {
			t.Errorf("%s arg %s = %v, want %v", tool, name, got[name], want)
		}
	}
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
	put := putResp.(*service.PutResponse)
	if put.ID != 7 || put.Version != 1 || put.Timestamp != 42 {
		t.Errorf("Put = %+v, want the server's ID 7 version 1 timestamp 42", put)
	}
	assertEntityArgs(t, "qrdb_put", kv.args["qrdb_put"],
		map[string]interface{}{"data": base64.StdEncoding.EncodeToString(data)})

	getResp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := getResp.(*service.GetResponse)
	if !got.Found || got.ID != 7 || !bytes.Equal(got.Data, data) {
		t.Errorf("Get = %+v, want found ID 7 data %q", got, data)
	}
	assertEntityArgs(t, "qrdb_get", kv.args["qrdb_get"], nil)

	delResp, err := tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if del := delResp.(*service.DeleteResponse); !del.Deleted || del.Timestamp != 43 {
		t.Errorf("Delete = %+v, want deleted at timestamp 43", del)
	}
	assertEntityArgs(t, "qrdb_delete", kv.args["qrdb_delete"], nil)

	// The server reports absence; the SDK keeps Found and Deleted as sent.
	getResp, err = tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if getResp.(*service.GetResponse).Found {
		t.Error("Get after Delete reported found")
	}
	delResp, err = tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if delResp.(*service.DeleteResponse).Deleted {
		t.Error("second Delete reported deleted")
	}
}

func TestKV_Errors(t *testing.T) {
	tr, kv := newQRDBKVTransport(t)
	ctx := context.Background()

	// A request that is not the typed SDK struct passes through unchanged.
	if _, err := tr.Request(ctx, "quicbaas.KV", "Put", map[string]interface{}{"data": "not base64!"}); err == nil {
		t.Error("raw Put with non-base64 data succeeded, want the tool's error")
	}
	if kv.args["qrdb_put"]["data"] != "not base64!" {
		t.Errorf("raw Put args = %v, want data passed through", kv.args["qrdb_put"])
	}

	kv.getAnswer = "not json"
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1}); err == nil {
		t.Error("Get with an undecodable answer succeeded, want an error")
	}
}

func TestKVResult_UndecodableAnswer(t *testing.T) {
	decoders := map[string]func(interface{}, string) (interface{}, error){
		"Put": kvPutResult, "Get": kvGetResult, "Delete": kvDeleteResult,
	}
	for name, decode := range decoders {
		if _, err := decode(nil, "ok"); err == nil {
			t.Errorf("%s decoded the answer %q, want an error", name, "ok")
		}
	}
}
