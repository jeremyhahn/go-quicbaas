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

// quicbaasKVServer mirrors quicbaas's quicbaas_kv_put, quicbaas_kv_get and
// quicbaas_kv_delete tools (pkg/api/mcp kv_tools.go): entity_type plus
// decimal-string id, shard_id and consistency, base64 data on put, and JSON
// answers whose uint64 fields are decimal strings.
type quicbaasKVServer struct {
	mu    sync.Mutex
	store map[string][]byte
	args  map[string]map[string]interface{}
	// getAnswer, when set, replaces quicbaas_kv_get's answer text.
	getAnswer string
	// getError, when set, makes quicbaas_kv_get answer a tool error.
	getError string
}

func (s *quicbaasKVServer) record(tool string, req mcp.CallToolRequest) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.args[tool] = req.GetArguments()
	return mcp.ParseString(req, "entity_type", "") + ":" + mcp.ParseString(req, "id", "")
}

func jsonAnswer(answer map[string]interface{}) (*mcp.CallToolResult, error) {
	text, err := json.Marshal(answer)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(text)), nil
}

func (s *quicbaasKVServer) put(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := s.record("quicbaas_kv_put", req)
	value, err := base64.StdEncoding.DecodeString(mcp.ParseString(req, "data", ""))
	if err != nil {
		return mcp.NewToolResultError("data is not base64"), nil
	}
	s.mu.Lock()
	s.store[key] = value
	s.mu.Unlock()
	return jsonAnswer(map[string]interface{}{
		"id":        mcp.ParseString(req, "id", ""),
		"version":   "18446744073709551615",
		"timestamp": 1700000000,
		"leader_id": "9223372036854775813",
	})
}

func (s *quicbaasKVServer) get(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := s.record("quicbaas_kv_get", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getError != "" {
		return mcp.NewToolResultError(s.getError), nil
	}
	if s.getAnswer != "" {
		return mcp.NewToolResultText(s.getAnswer), nil
	}
	id := mcp.ParseString(req, "id", "")
	value, ok := s.store[key]
	if !ok {
		return jsonAnswer(map[string]interface{}{"id": id, "found": false, "version": "0", "timestamp": 0})
	}
	return jsonAnswer(map[string]interface{}{
		"id":        id,
		"found":     true,
		"data":      base64.StdEncoding.EncodeToString(value),
		"version":   "3",
		"timestamp": 1700000000,
	})
}

func (s *quicbaasKVServer) delete(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := s.record("quicbaas_kv_delete", req)
	s.mu.Lock()
	_, found := s.store[key]
	delete(s.store, key)
	s.mu.Unlock()
	return jsonAnswer(map[string]interface{}{"deleted": found, "timestamp": 1700000000})
}

func newQuicbaasKVTransport(t *testing.T) (*Transport, *quicbaasKVServer) {
	t.Helper()
	kv := &quicbaasKVServer{store: map[string][]byte{}, args: map[string]map[string]interface{}{}}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcp.NewTool("quicbaas_kv_put"), kv.put)
	srv.AddTool(mcp.NewTool("quicbaas_kv_get"), kv.get)
	srv.AddTool(mcp.NewTool("quicbaas_kv_delete"), kv.delete)
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

func TestKV_PutGetDelete_RoundTripOverQuicbaasKVTools(t *testing.T) {
	tr, kv := newQuicbaasKVTransport(t)
	ctx := context.Background()
	data := []byte("hello quicbaas")

	putResp, err := tr.Request(ctx, "quicbaas.KV", "Put",
		&service.PutRequest{EntityType: "users", ID: 7, Data: data})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	put := putResp.(*service.PutResponse)
	if put.ID != 7 || put.Version != 18446744073709551615 || put.LeaderID != 9223372036854775813 {
		t.Errorf("Put = %+v, want ID 7 with the exact version and leader_id", put)
	}
	wantPut := map[string]interface{}{
		"entity_type": "users",
		"id":          "7",
		"shard_id":    "0",
		"consistency": "0",
		"data":        base64.StdEncoding.EncodeToString(data),
	}
	if len(kv.args["quicbaas_kv_put"]) != len(wantPut) {
		t.Errorf("quicbaas_kv_put args = %v, want %v", kv.args["quicbaas_kv_put"], wantPut)
	}
	for key, want := range wantPut {
		if kv.args["quicbaas_kv_put"][key] != want {
			t.Errorf("quicbaas_kv_put %s = %v, want %v", key, kv.args["quicbaas_kv_put"][key], want)
		}
	}

	getResp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := getResp.(*service.GetResponse)
	if !got.Found || got.ID != 7 || got.Version != 3 || !bytes.Equal(got.Data, data) {
		t.Errorf("Get = %+v, want found ID 7 version 3 data %q", got, data)
	}

	delResp, err := tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{EntityType: "users", ID: 7})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !delResp.(*service.DeleteResponse).Deleted {
		t.Error("Delete reported not deleted")
	}
}

func TestKV_ShardAndConsistencyForwarded(t *testing.T) {
	tr, kv := newQuicbaasKVTransport(t)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.KV", "Put", &service.PutRequest{
		EntityType: "users", ID: 7, Data: []byte("v"), ShardID: 3, Consistency: service.ConsistencyLevelLinearizable,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{
		EntityType: "users", ID: 7, ShardID: 3, Consistency: service.ConsistencyLevelLinearizable,
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := tr.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{
		EntityType: "users", ID: 7, ShardID: 3, Consistency: service.ConsistencyLevelLinearizable,
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, tool := range []string{"quicbaas_kv_put", "quicbaas_kv_get", "quicbaas_kv_delete"} {
		if kv.args[tool]["shard_id"] != "3" {
			t.Errorf("%s shard_id = %v, want \"3\"", tool, kv.args[tool]["shard_id"])
		}
		if kv.args[tool]["consistency"] != strconv.Itoa(int(service.ConsistencyLevelLinearizable)) {
			t.Errorf("%s consistency = %v, want linearizable", tool, kv.args[tool]["consistency"])
		}
	}
}

func TestKV_IDAbove2To53IsExact(t *testing.T) {
	tr, kv := newQuicbaasKVTransport(t)
	ctx := context.Background()
	const id = uint64(18446744073709551557)

	putResp, err := tr.Request(ctx, "quicbaas.KV", "Put", &service.PutRequest{EntityType: "users", ID: id, Data: []byte("v")})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if kv.args["quicbaas_kv_put"]["id"] != "18446744073709551557" {
		t.Errorf("quicbaas_kv_put id = %v, want the exact decimal string", kv.args["quicbaas_kv_put"]["id"])
	}
	if got := putResp.(*service.PutResponse).ID; got != id {
		t.Errorf("Put ID = %d, want %d", got, id)
	}

	getResp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: id})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := getResp.(*service.GetResponse); !got.Found || got.ID != id {
		t.Errorf("Get = %+v, want found ID %d", got, id)
	}
}

func TestKV_FoundFalseIsAMiss(t *testing.T) {
	tr, _ := newQuicbaasKVTransport(t)
	resp, err := tr.Request(context.Background(), "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 9})
	if err != nil {
		t.Fatalf("Get of a missing entity: %v, want a miss", err)
	}
	if got := resp.(*service.GetResponse); got.Found || got.Data != nil {
		t.Errorf("Get = %+v, want found false and no data", got)
	}
}

func TestKV_Errors(t *testing.T) {
	tr, kv := newQuicbaasKVTransport(t)
	ctx := context.Background()

	kv.getError = "kv service unavailable"
	resp, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1})
	if err == nil {
		t.Errorf("Get with a tool error = %+v, want the error, never a miss", resp)
	}

	kv.getError = ""
	kv.getAnswer = "not json"
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1}); err == nil {
		t.Error("Get with a non-JSON answer succeeded, want an error")
	}

	kv.getAnswer = `{"id":7,"found":true}`
	if _, err := tr.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{EntityType: "users", ID: 1}); err == nil {
		t.Error("Get with a numeric id succeeded, want an error: ids are decimal strings")
	}
}
