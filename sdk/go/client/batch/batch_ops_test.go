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

	t.Run("more responses than requests leaves entity type empty", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return &service.BatchPutResponse{
					Responses: []*service.PutResponse{
						{ID: 1, Version: 1},
						{ID: 2, Version: 2},
					},
				}, nil
			},
		}
		client := NewClient(mockT)
		// Only one request but two responses.
		resp, err := client.Put(context.Background(), []*service.PutRequest{
			{EntityType: "users", ID: 1, Data: []byte("a")},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Responses[0].EntityType != "users" {
			t.Errorf("expected first entity type users, got %q", resp.Responses[0].EntityType)
		}
		if resp.Responses[1].EntityType != "" {
			t.Errorf("expected second entity type empty, got %q", resp.Responses[1].EntityType)
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

	t.Run("more responses than requests leaves entity type empty", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				return &service.BatchGetResponse{
					Responses: []*service.GetResponse{
						{ID: 1, Found: true},
						{ID: 2, Found: true},
					},
				}, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.Get(context.Background(), []*service.GetRequest{
			{EntityType: "users", ID: 1},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Responses[0].EntityType != "users" {
			t.Errorf("expected first entity type users, got %q", resp.Responses[0].EntityType)
		}
		if resp.Responses[1].EntityType != "" {
			t.Errorf("expected second entity type empty, got %q", resp.Responses[1].EntityType)
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
