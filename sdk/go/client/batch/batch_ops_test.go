// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package batch

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Put
// =============================================================================

func TestClient_Put(t *testing.T) {
	t.Run("happy path converts service response", func(t *testing.T) {
		var capturedMethod string
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				capturedMethod = method
				if svc != serviceName {
					t.Errorf("expected service %q, got %q", serviceName, svc)
				}
				bpr, ok := req.(*service.BatchPutRequest)
				if !ok {
					t.Fatalf("expected *service.BatchPutRequest, got %T", req)
				}
				if len(bpr.Requests) != 2 {
					t.Fatalf("expected 2 requests, got %d", len(bpr.Requests))
				}
				return &service.BatchPutResponse{
					Timestamp: 1234,
					Responses: []*service.PutResponse{
						{ID: 10, Version: 1},
						{ID: 11, Version: 2},
					},
				}, nil
			},
		}
		client := NewClient(mockT)

		reqs := []*service.PutRequest{
			{EntityType: "users", ID: 10, Data: []byte("a")},
			{EntityType: "orders", ID: 11, Data: []byte("b")},
		}
		resp, err := client.Put(context.Background(), reqs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedMethod != "Put" {
			t.Errorf("expected method Put, got %q", capturedMethod)
		}
		if resp.Timestamp != 1234 {
			t.Errorf("expected timestamp 1234, got %d", resp.Timestamp)
		}
		if len(resp.Responses) != 2 {
			t.Fatalf("expected 2 responses, got %d", len(resp.Responses))
		}
		if resp.Responses[0].EntityType != "users" || resp.Responses[0].ID != 10 || resp.Responses[0].Version != 1 || !resp.Responses[0].Success {
			t.Errorf("unexpected first result: %+v", resp.Responses[0])
		}
		if resp.Responses[1].EntityType != "orders" || resp.Responses[1].ID != 11 || resp.Responses[1].Version != 2 || !resp.Responses[1].Success {
			t.Errorf("unexpected second result: %+v", resp.Responses[1])
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		_, err := client.Put(nil, []*service.PutRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		if errors.Code(err) != errors.CodeInvalidInput {
			t.Errorf("expected InvalidInput, got %v", errors.Code(err))
		}
	})

	t.Run("empty requests returns empty response without calling transport", func(t *testing.T) {
		called := false
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				called = true
				return nil, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.Put(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called {
			t.Error("transport should not be called for empty requests")
		}
		if resp == nil || resp.Responses == nil || len(resp.Responses) != 0 {
			t.Errorf("expected empty non-nil responses, got %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "down")
			},
		}
		client := NewClient(mockT)
		_, err := client.Put(context.Background(), []*service.PutRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected transport error")
		}
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", errors.Code(err))
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Put(context.Background(), []*service.PutRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return "not a batch put response", nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Put(context.Background(), []*service.PutRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for invalid response type")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})
}

// =============================================================================
// Get
// =============================================================================

func TestClient_Get(t *testing.T) {
	t.Run("happy path converts service response", func(t *testing.T) {
		var capturedMethod string
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				capturedMethod = method
				bgr, ok := req.(*service.BatchGetRequest)
				if !ok {
					t.Fatalf("expected *service.BatchGetRequest, got %T", req)
				}
				if len(bgr.Requests) != 2 {
					t.Fatalf("expected 2 requests, got %d", len(bgr.Requests))
				}
				return &service.BatchGetResponse{
					Responses: []*service.GetResponse{
						{ID: 10, Data: []byte("alice"), Version: 3, Found: true},
						{ID: 11, Found: false},
					},
				}, nil
			},
		}
		client := NewClient(mockT)

		reqs := []*service.GetRequest{
			{EntityType: "users", ID: 10},
			{EntityType: "orders", ID: 11},
		}
		resp, err := client.Get(context.Background(), reqs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedMethod != "Get" {
			t.Errorf("expected method Get, got %q", capturedMethod)
		}
		if len(resp.Responses) != 2 {
			t.Fatalf("expected 2 responses, got %d", len(resp.Responses))
		}
		first := resp.Responses[0]
		if first.EntityType != "users" || first.ID != 10 || string(first.Data) != "alice" || first.Version != 3 || !first.Found {
			t.Errorf("unexpected first result: %+v", first)
		}
		second := resp.Responses[1]
		if second.EntityType != "orders" || second.ID != 11 || second.Found {
			t.Errorf("unexpected second result: %+v", second)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		_, err := client.Get(nil, []*service.GetRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		if errors.Code(err) != errors.CodeInvalidInput {
			t.Errorf("expected InvalidInput, got %v", errors.Code(err))
		}
	})

	t.Run("empty requests returns empty response without calling transport", func(t *testing.T) {
		called := false
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				called = true
				return nil, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.Get(context.Background(), []*service.GetRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called {
			t.Error("transport should not be called for empty requests")
		}
		if resp == nil || resp.Responses == nil || len(resp.Responses) != 0 {
			t.Errorf("expected empty non-nil responses, got %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "down")
			},
		}
		client := NewClient(mockT)
		_, err := client.Get(context.Background(), []*service.GetRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected transport error")
		}
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", errors.Code(err))
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Get(context.Background(), []*service.GetRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return 42, nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Get(context.Background(), []*service.GetRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for invalid response type")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})
}

// =============================================================================
// Delete
// =============================================================================

func TestClient_Delete(t *testing.T) {
	t.Run("happy path converts service response", func(t *testing.T) {
		var capturedMethod string
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				capturedMethod = method
				bdr, ok := req.(*service.BatchDeleteRequest)
				if !ok {
					t.Fatalf("expected *service.BatchDeleteRequest, got %T", req)
				}
				if len(bdr.Requests) != 2 {
					t.Fatalf("expected 2 requests, got %d", len(bdr.Requests))
				}
				return &service.BatchDeleteResponse{
					Timestamp: 9999,
					Responses: []*service.DeleteResponse{
						{Deleted: true},
						{Deleted: false},
					},
				}, nil
			},
		}
		client := NewClient(mockT)

		reqs := []*service.DeleteRequest{
			{EntityType: "users", ID: 10},
			{EntityType: "orders", ID: 11},
		}
		resp, err := client.Delete(context.Background(), reqs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedMethod != "Delete" {
			t.Errorf("expected method Delete, got %q", capturedMethod)
		}
		if resp.Timestamp != 9999 {
			t.Errorf("expected timestamp 9999, got %d", resp.Timestamp)
		}
		if len(resp.Responses) != 2 {
			t.Fatalf("expected 2 responses, got %d", len(resp.Responses))
		}
		first := resp.Responses[0]
		if first.EntityType != "users" || first.ID != 10 || !first.Deleted {
			t.Errorf("unexpected first result: %+v", first)
		}
		second := resp.Responses[1]
		if second.EntityType != "orders" || second.ID != 11 || second.Deleted {
			t.Errorf("unexpected second result: %+v", second)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		_, err := client.Delete(nil, []*service.DeleteRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		if errors.Code(err) != errors.CodeInvalidInput {
			t.Errorf("expected InvalidInput, got %v", errors.Code(err))
		}
	})

	t.Run("empty requests returns empty response without calling transport", func(t *testing.T) {
		called := false
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				called = true
				return nil, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.Delete(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called {
			t.Error("transport should not be called for empty requests")
		}
		if resp == nil || resp.Responses == nil || len(resp.Responses) != 0 {
			t.Errorf("expected empty non-nil responses, got %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "down")
			},
		}
		client := NewClient(mockT)
		_, err := client.Delete(context.Background(), []*service.DeleteRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected transport error")
		}
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", errors.Code(err))
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Delete(context.Background(), []*service.DeleteRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return &service.BatchPutResponse{}, nil
			},
		}
		client := NewClient(mockT)
		_, err := client.Delete(context.Background(), []*service.DeleteRequest{{EntityType: "users"}})
		if err == nil {
			t.Fatal("expected error for invalid response type")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", errors.Code(err))
		}
	})
}

// batchVerb runs one positional batch verb against a server that returns
// resultCount results for two requests, and reports the call's outcome.
type batchVerb struct {
	name string
	call func(resultCount int) (resp interface{}, err error)
}

// positionalVerbs are the batch verbs that map each server result to the
// request at the same position.
var positionalVerbs = []batchVerb{
	{
		name: "Put",
		call: func(resultCount int) (interface{}, error) {
			results := make([]*service.PutResponse, resultCount)
			for i := range results {
				results[i] = &service.PutResponse{ID: uint64(100 + i), Version: uint64(i + 1)}
			}
			client := NewClient(&mockTransport{
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					return &service.BatchPutResponse{Responses: results}, nil
				},
			})
			resp, err := client.Put(context.Background(), []*service.PutRequest{
				{EntityType: "users"},
				{EntityType: "orders"},
			})
			if resp == nil {
				return nil, err
			}
			return resp, err
		},
	},
	{
		name: "Get",
		call: func(resultCount int) (interface{}, error) {
			results := make([]*service.GetResponse, resultCount)
			for i := range results {
				results[i] = &service.GetResponse{ID: uint64(100 + i), Version: uint64(i + 1), Found: true}
			}
			client := NewClient(&mockTransport{
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					return &service.BatchGetResponse{Responses: results}, nil
				},
			})
			resp, err := client.Get(context.Background(), []*service.GetRequest{
				{EntityType: "users", ID: 100},
				{EntityType: "orders", ID: 101},
			})
			if resp == nil {
				return nil, err
			}
			return resp, err
		},
	},
	{
		name: "Delete",
		call: func(resultCount int) (interface{}, error) {
			results := make([]*service.DeleteResponse, resultCount)
			for i := range results {
				results[i] = &service.DeleteResponse{Deleted: i%2 == 0}
			}
			client := NewClient(&mockTransport{
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					return &service.BatchDeleteResponse{Responses: results}, nil
				},
			})
			resp, err := client.Delete(context.Background(), []*service.DeleteRequest{
				{EntityType: "users", ID: 100},
				{EntityType: "orders", ID: 101},
			})
			if resp == nil {
				return nil, err
			}
			return resp, err
		},
	},
}

func TestClient_PositionalVerbs_RefuseResultCountMismatch(t *testing.T) {
	const requestCount = 2
	for _, verb := range positionalVerbs {
		for _, tc := range []struct {
			name        string
			resultCount int
		}{
			{name: "one result too many", resultCount: requestCount + 1},
			{name: "one result too few", resultCount: requestCount - 1},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s panicked on %d results for %d requests: %v",
							verb.name, tc.resultCount, requestCount, r)
					}
				}()
				resp, err := verb.call(tc.resultCount)
				if err == nil {
					t.Fatalf("expected an error for %d results, got response %+v", tc.resultCount, resp)
				}
				if errors.Code(err) != errors.CodeInternal {
					t.Errorf("expected Internal, got %v", errors.Code(err))
				}
				if resp != nil {
					t.Errorf("expected nil response, got %+v", resp)
				}
			})
		}
	}
}

func TestClient_PositionalVerbs_MatchingCountMapsInOrder(t *testing.T) {
	const requestCount = 2
	wantTypes := []string{"users", "orders"}

	t.Run("Put", func(t *testing.T) {
		resp, err := positionalVerbs[0].call(requestCount)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := resp.(*BatchPutResponse).Responses
		if len(items) != requestCount {
			t.Fatalf("expected %d items, got %d", requestCount, len(items))
		}
		for i, item := range items {
			if item.EntityType != wantTypes[i] || item.ID != uint64(100+i) ||
				item.Version != uint64(i+1) || !item.Success {
				t.Errorf("item %d: unexpected %+v", i, item)
			}
		}
	})

	t.Run("Get", func(t *testing.T) {
		resp, err := positionalVerbs[1].call(requestCount)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := resp.(*BatchGetResponse).Responses
		if len(items) != requestCount {
			t.Fatalf("expected %d items, got %d", requestCount, len(items))
		}
		for i, item := range items {
			if item.EntityType != wantTypes[i] || item.ID != uint64(100+i) ||
				item.Version != uint64(i+1) || !item.Found {
				t.Errorf("item %d: unexpected %+v", i, item)
			}
		}
	})

	t.Run("Delete", func(t *testing.T) {
		resp, err := positionalVerbs[2].call(requestCount)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := resp.(*BatchDeleteResponse).Responses
		if len(items) != requestCount {
			t.Fatalf("expected %d items, got %d", requestCount, len(items))
		}
		for i, item := range items {
			if item.EntityType != wantTypes[i] || item.ID != uint64(100+i) || item.Deleted != (i%2 == 0) {
				t.Errorf("item %d: unexpected %+v", i, item)
			}
		}
	})
}
