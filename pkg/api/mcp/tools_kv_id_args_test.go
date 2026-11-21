//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kvIDRecorder is a service whose Put, Get and Delete record the entity ID
// each call reaches the service with.
type kvIDRecorder struct {
	svc *mockService
	ids []uint64
}

func newKVIDRecorder() *kvIDRecorder {
	rec := &kvIDRecorder{svc: &mockService{}}
	rec.svc.putFunc = func(_ context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		rec.ids = append(rec.ids, req.ID)
		return &service.PutResponse{ID: req.ID}, nil
	}
	rec.svc.getFunc = func(_ context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		rec.ids = append(rec.ids, req.ID)
		return &service.GetResponse{}, nil
	}
	rec.svc.deleteFunc = func(_ context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		rec.ids = append(rec.ids, req.ID)
		return &service.DeleteResponse{}, nil
	}
	return rec
}

func (rec *kvIDRecorder) registry(t *testing.T) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	return NewToolRegistry(rec.svc, log)
}

type kvIDCall func(r *ToolRegistry, ctx context.Context, args map[string]interface{}) (interface{}, error)

func kvIDCalls() map[string]kvIDCall {
	return map[string]kvIDCall{
		"put": func(r *ToolRegistry, ctx context.Context, args map[string]interface{}) (interface{}, error) {
			args["data"] = "aGVsbG8="
			return r.executePut(ctx, args)
		},
		"get": func(r *ToolRegistry, ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return r.executeGet(ctx, args)
		},
		"delete": func(r *ToolRegistry, ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return r.executeDelete(ctx, args)
		},
	}
}

func TestEntityKVTools_RefuseMalformedID(t *testing.T) {
	badIDs := map[string]interface{}{
		"not a number":     "abc",
		"overflows uint64": "18446744073709551616",
		"fractional":       float64(1.5),
		"negative":         float64(-1),
		"wrong type":       true,
	}
	for op, call := range kvIDCalls() {
		for name, id := range badIDs {
			t.Run(op+"/"+name, func(t *testing.T) {
				rec := newKVIDRecorder()
				args := map[string]interface{}{"entityType": "users", "id": id}
				if _, err := call(rec.registry(t), context.Background(), args); err == nil {
					t.Fatalf("id %v: want an error, got none", id)
				}
				if len(rec.ids) != 0 {
					t.Fatalf("id %v: service was called with %v", id, rec.ids)
				}
			})
		}
	}
}

func TestEntityKVTools_IDAbove2To53ReachesServiceExactly(t *testing.T) {
	const want uint64 = 9007199254740993
	for op, call := range kvIDCalls() {
		t.Run(op, func(t *testing.T) {
			rec := newKVIDRecorder()
			args := map[string]interface{}{"entityType": "users", "id": "9007199254740993"}
			if _, err := call(rec.registry(t), context.Background(), args); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rec.ids) != 1 || rec.ids[0] != want {
				t.Fatalf("service saw ids %v, want [%d]", rec.ids, want)
			}
		})
	}
}

func TestEntityKVTools_AbsentIDKeepsHashKey(t *testing.T) {
	rec := newKVIDRecorder()
	args := map[string]interface{}{"entityType": "users", "key": "alice"}
	if _, err := rec.registry(t).executeGet(context.Background(), args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := hashStringKey("users", "alice")
	if len(rec.ids) != 1 || rec.ids[0] != want {
		t.Fatalf("service saw ids %v, want [%d]", rec.ids, want)
	}
}
