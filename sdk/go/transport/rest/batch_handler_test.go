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
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// batchCall records the request the test server received.
type batchCall struct {
	method string
	path   string
	body   map[string]interface{}
}

// newBatchServer answers every request with answer and records what it saw.
func newBatchServer(t *testing.T, answer string) (*Transport, *batchCall) {
	t.Helper()
	seen := &batchCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path = r.Method, r.URL.Path
		seen.body = nil
		if err := json.NewDecoder(r.Body).Decode(&seen.body); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(answer)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return newTestTransport(t, server), seen
}

// wireBody round-trips v through JSON so it compares with a decoded body.
func wireBody(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestBatchRESTHandler_CarriesEveryVerb(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		req      interface{}
		answer   string
		path     string
		wantBody map[string]interface{}
		want     interface{}
	}{
		{
			name:   "Execute",
			method: "Execute",
			req: &batch.BatchRequest{Operations: []batch.BatchOperation{
				{Type: batch.OpPut, EntityType: "users", Key: "k1", Value: []byte("v1")},
				{Type: batch.OpDelete, EntityType: "users", Key: "k2"},
			}},
			answer: `{"success":true,"results":[{"success":true},{"success":false}]}`,
			path:   "/api/v1/batch/execute",
			wantBody: map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "k1", "value": "djE="},
				map[string]interface{}{"type": "DELETE", "entity_type": "users", "key": "k2"},
			}},
			want: &batch.BatchResponse{Results: []batch.BatchResult{{Success: true}, {Success: false}}},
		},
		{
			name:   "Put",
			method: "Put",
			req: &service.BatchPutRequest{Requests: []*service.PutRequest{
				{EntityType: "users", ID: 7, Data: []byte(`{"name":"a"}`)},
			}},
			answer: `{"success":true,"responses":[{"id":7,"entity_type":"users","created":true,"timestamp":"2026-10-30T00:00:00Z"}],"timestamp":42}`,
			path:   "/api/v1/batch/put",
			wantBody: map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"entity_type": "users", "id": float64(7), "data": map[string]interface{}{"name": "a"}},
			}},
			want: &service.BatchPutResponse{
				Responses: []*service.PutResponse{{ID: 7, Timestamp: 42}},
				Timestamp: 42,
			},
		},
		{
			name:   "Get",
			method: "Get",
			req: &service.BatchGetRequest{Requests: []*service.GetRequest{
				{EntityType: "users", ID: 7},
				{EntityType: "users", ID: 8},
			}},
			answer: `{"success":true,"responses":[{"id":7,"entity_type":"users","data":{"name":"a"},"found":true},{"id":8,"entity_type":"users","found":false}],"timestamp":42}`,
			path:   "/api/v1/batch/get",
			wantBody: map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"entity_type": "users", "id": float64(7)},
				map[string]interface{}{"entity_type": "users", "id": float64(8)},
			}},
			want: &service.BatchGetResponse{Responses: []*service.GetResponse{
				{ID: 7, Data: []byte(`{"name":"a"}`), Found: true},
				{ID: 8, Found: false},
			}},
		},
		{
			name:   "Delete",
			method: "Delete",
			req: &service.BatchDeleteRequest{Requests: []*service.DeleteRequest{
				{EntityType: "users", ID: 7},
			}},
			answer: `{"success":true,"responses":[{"id":7,"entity_type":"users","deleted":true,"timestamp":"2026-10-30T00:00:00Z"}],"timestamp":42}`,
			path:   "/api/v1/batch/delete",
			wantBody: map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"entity_type": "users", "id": float64(7)},
			}},
			want: &service.BatchDeleteResponse{
				Responses: []*service.DeleteResponse{{Deleted: true, Timestamp: 42}},
				Timestamp: 42,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, seen := newBatchServer(t, tc.answer)
			got, err := tr.Request(context.Background(), "quicbaas.Batch", tc.method, tc.req)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if seen.method != http.MethodPost || seen.path != tc.path {
				t.Errorf("sent %s %s, want POST %s", seen.method, seen.path, tc.path)
			}
			if !reflect.DeepEqual(seen.body, wireBody(t, tc.wantBody)) {
				t.Errorf("body = %v, want %v", seen.body, tc.wantBody)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("response = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestBatchRESTHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	tr, seen := newBatchServer(t, `{}`)
	calls := map[string]interface{}{
		"Unknown": &batch.BatchRequest{},
		"Execute": &service.BatchPutRequest{},
		"Put":     &batch.BatchRequest{},
		"Get":     (*service.BatchGetRequest)(nil),
		"Delete":  "not a request",
	}
	for method, req := range calls {
		_, err := tr.Request(context.Background(), "quicbaas.Batch", method, req)
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s answered %v, want invalid input", method, err)
		}
	}
	if seen.path != "" {
		t.Errorf("a refused call reached the server at %s", seen.path)
	}
}

func TestBatchRESTHandler_RefusesShardSelection(t *testing.T) {
	tr, seen := newBatchServer(t, `{}`)
	calls := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"put item shard", "Put", &service.BatchPutRequest{Requests: []*service.PutRequest{{EntityType: "users", ID: 1, ShardID: 2}}}},
		{"put batch shard", "Put", &service.BatchPutRequest{ShardID: 2, Requests: []*service.PutRequest{{EntityType: "users", ID: 1}}}},
		{"get item shard", "Get", &service.BatchGetRequest{Requests: []*service.GetRequest{{EntityType: "users", ID: 1, ShardID: 2}}}},
		{"get batch shard", "Get", &service.BatchGetRequest{ShardID: 2, Requests: []*service.GetRequest{{EntityType: "users", ID: 1}}}},
		{"delete item shard", "Delete", &service.BatchDeleteRequest{Requests: []*service.DeleteRequest{{EntityType: "users", ID: 1, ShardID: 2}}}},
		{"delete batch shard", "Delete", &service.BatchDeleteRequest{ShardID: 2, Requests: []*service.DeleteRequest{{EntityType: "users", ID: 1}}}},
	}
	for _, c := range calls {
		_, err := tr.Request(context.Background(), "quicbaas.Batch", c.method, c.req)
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s answered %v, want invalid input", c.name, err)
		}
	}
	if seen.path != "" {
		t.Errorf("a shard-specific call reached the server at %s", seen.path)
	}
}

func TestBatchRESTHandler_MapsServerRefusals(t *testing.T) {
	statuses := map[int]func(error) bool{
		http.StatusBadRequest:         errors.IsInvalidInput,
		http.StatusForbidden:          errors.IsForbidden,
		http.StatusNotFound:           errors.IsNotFound,
		http.StatusServiceUnavailable: errors.IsUnavailable,
	}
	req := &batch.BatchRequest{Operations: []batch.BatchOperation{{Type: batch.OpPut, EntityType: "users", Key: "k"}}}
	for status, is := range statuses {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if _, err := w.Write([]byte(`{"error":"refused"}`)); err != nil {
				t.Errorf("write: %v", err)
			}
		}))
		tr := newTestTransport(t, server)
		_, err := tr.Request(context.Background(), "quicbaas.Batch", "Execute", req)
		server.Close()
		if !is(err) {
			t.Errorf("status %d answered %v", status, err)
		}
	}
}

func TestIsBatchService(t *testing.T) {
	for name, want := range map[string]bool{
		"quicbaas.Batch":   true,
		"Batch":            true,
		"quicbaas.Batches": false,
		"quicbaas.Prompts": false,
	} {
		if got := isBatchService(name); got != want {
			t.Errorf("isBatchService(%q) = %v, want %v", name, got, want)
		}
	}
}
