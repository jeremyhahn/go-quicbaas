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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// batchKVServer mirrors quicbaas's service-backed quicbaas_batch_put,
// quicbaas_batch_get and quicbaas_batch_delete tools (pkg/api/mcp tools.go
// executeBatch*): an "operations" array of {entityType, id, data} objects in,
// and the service's batch response as JSON text out. It also serves qrdb_put,
// so a request that is not the typed SDK struct can be seen to keep the
// generic path.
type batchKVServer struct {
	mu    sync.Mutex
	store map[string][]byte
	args  map[string]map[string]interface{}
	// answer, when set, replaces every batch tool's answer text.
	answer string
}

func (s *batchKVServer) record(tool string, req mcp.CallToolRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.args[tool] = req.GetArguments()
}

// operation returns the store key and data of the request's single operation.
func (s *batchKVServer) operation(req mcp.CallToolRequest) (string, string, error) {
	ops, ok := req.GetArguments()["operations"].([]interface{})
	if !ok || len(ops) != 1 {
		return "", "", fmt.Errorf("operations must hold one operation, got %v", req.GetArguments()["operations"])
	}
	op, ok := ops[0].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("operation must be an object, got %T", ops[0])
	}
	entityType, _ := op["entityType"].(string)
	id, ok := op["id"].(float64)
	if entityType == "" || !ok {
		return "", "", fmt.Errorf("operation needs entityType and a numeric id, got %v", op)
	}
	data, _ := op["data"].(string)
	return fmt.Sprintf("%s:%d", entityType, uint64(id)), data, nil
}

// answerJSON answers v as the tool's JSON text, or the scripted answer.
func (s *batchKVServer) answerJSON(v interface{}) (*mcp.CallToolResult, error) {
	s.mu.Lock()
	answer := s.answer
	s.mu.Unlock()
	if answer != "" {
		return mcp.NewToolResultText(answer), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(raw)), nil
}

func (s *batchKVServer) put(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("quicbaas_batch_put", req)
	key, dataArg, err := s.operation(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, err := base64.StdEncoding.DecodeString(dataArg)
	if err != nil || dataArg == "" {
		return mcp.NewToolResultError("data must be base64-encoded"), nil
	}
	s.mu.Lock()
	s.store[key] = data
	s.mu.Unlock()
	return s.answerJSON(&service.BatchPutResponse{Responses: []*service.PutResponse{{ID: 7, Version: 1}}})
}

func (s *batchKVServer) get(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("quicbaas_batch_get", req)
	key, _, err := s.operation(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s.mu.Lock()
	data, found := s.store[key]
	s.mu.Unlock()
	return s.answerJSON(&service.BatchGetResponse{Responses: []*service.GetResponse{{ID: 7, Data: data, Found: found}}})
}

func (s *batchKVServer) delete(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("quicbaas_batch_delete", req)
	key, _, err := s.operation(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s.mu.Lock()
	_, found := s.store[key]
	delete(s.store, key)
	s.mu.Unlock()
	return s.answerJSON(&service.BatchDeleteResponse{Responses: []*service.DeleteResponse{{Deleted: found}}})
}

func (s *batchKVServer) rawPut(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.record("qrdb_put", req)
	return mcp.NewToolResultError("value is required"), nil
}

func newBatchKVTransport(t *testing.T) (*Transport, *batchKVServer) {
	t.Helper()
	kv := &batchKVServer{store: map[string][]byte{}, args: map[string]map[string]interface{}{}}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcp.NewTool("quicbaas_batch_put"), kv.put)
	srv.AddTool(mcp.NewTool("quicbaas_batch_get"), kv.get)
	srv.AddTool(mcp.NewTool("quicbaas_batch_delete"), kv.delete)
	srv.AddTool(mcp.NewTool("qrdb_put"), kv.rawPut)
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

// assertOperation checks that a batch tool got exactly one operation holding
// entityType users, the numeric id 7 and, when data is set, that data.
func assertOperation(t *testing.T, tool string, args map[string]interface{}, data []byte) {
	t.Helper()
	if len(args) != 1 {
		t.Errorf("%s args = %v, want only operations", tool, args)
	}
	ops, ok := args["operations"].([]interface{})
	if !ok || len(ops) != 1 {
		t.Fatalf("%s operations = %v, want one operation", tool, args["operations"])
	}
	op, ok := ops[0].(map[string]interface{})
	if !ok {
		t.Fatalf("%s operation = %T, want an object", tool, ops[0])
	}
	want := map[string]interface{}{"entityType": "users", "id": float64(7)}
	if data != nil {
		want["data"] = base64.StdEncoding.EncodeToString(data)
	}
	if len(op) != len(want) {
		t.Errorf("%s operation = %v, want %v", tool, op, want)
	}
	for k, v := range want {
		if op[k] != v {
			t.Errorf("%s operation[%q] = %v (%T), want %v (%T)", tool, k, op[k], op[k], v, v)
		}
	}
}

func TestKV_PutGetDelete_RoundTripOverBatchTools(t *testing.T) {
	tr, kv := newBatchKVTransport(t)
	ctx := context.Background()
	data := []byte{0x00, 0x01, 0xfe}

	putResp, err := tr.Request(ctx, "quicbaas.KV", "Put",
		&service.PutRequest{EntityType: "users", ID: 7, Data: data})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := putResp.(*service.PutResponse); got.ID != 7 || got.Version != 1 {
		t.Errorf("Put = %+v, want ID 7 version 1", got)
	}
	assertOperation(t, "quicbaas_batch_put", kv.args["quicbaas_batch_put"], data)

	getResp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := getResp.(*service.GetResponse)
	if !got.Found || got.ID != 7 || !bytes.Equal(got.Data, data) {
		t.Errorf("Get = %+v, want found ID 7 data %v", got, data)
	}
	assertOperation(t, "quicbaas_batch_get", kv.args["quicbaas_batch_get"], nil)

	delResp, err := tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !delResp.(*service.DeleteResponse).Deleted {
		t.Error("Delete reported not deleted")
	}
	assertOperation(t, "quicbaas_batch_delete", kv.args["quicbaas_batch_delete"], nil)

	// A missing entity is Found=false, as the service reports it, not an error.
	getResp, err = tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if getResp.(*service.GetResponse).Found {
		t.Error("Get after Delete found the entity")
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
	tr, kv := newBatchKVTransport(t)
	ctx := context.Background()

	// A request that is not the typed SDK struct keeps the generic path.
	if _, err := tr.Request(ctx, "quicbaas.KV", "Put", map[string]interface{}{"key": "raw"}); err == nil {
		t.Error("raw Put succeeded, want the qrdb_put tool's error")
	}
	if kv.args["qrdb_put"]["key"] != "raw" {
		t.Errorf("raw Put args = %v, want key raw passed through to qrdb_put", kv.args["qrdb_put"])
	}
	if _, called := kv.args["quicbaas_batch_put"]; called {
		t.Error("raw Put reached quicbaas_batch_put")
	}

	kv.answer = "not json"
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1}); err == nil {
		t.Error("Get with an undecodable answer succeeded, want an error")
	}
}

func TestKVResult_UndecodableAnswer(t *testing.T) {
	decoders := map[string]func(string) (interface{}, error){
		"Put":    kvPutResult,
		"Get":    kvGetResult,
		"Delete": kvDeleteResult,
	}
	for name, decode := range decoders {
		for _, answer := range []string{"", "not json", `{"Responses":[]}`, `{"Responses":[{},{}]}`, `{"Responses":[null]}`} {
			if resp, err := decode(answer); err == nil {
				t.Errorf("%s(%q) = %+v, want an error", name, answer, resp)
			}
		}
	}
}
