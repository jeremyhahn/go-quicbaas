// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package kv

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_Put_Validation(t *testing.T) {
	// Mock transport that returns valid responses
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.PutResponse{ID: 1, Version: 1, Timestamp: 1234567890}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *service.PutRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty entity type",
			req:     &service.PutRequest{ID: 1, Data: []byte("value")},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &service.PutRequest{EntityType: "users", ID: 1, Data: []byte("value")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Put(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Put() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Put_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.PutResponse{ID: 1, Version: 1, Timestamp: 1234567890, LeaderID: 1}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.Put(ctx, &service.PutRequest{
		EntityType: "users",
		ID:         1,
		Data:       []byte("test data"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ID != 1 {
		t.Errorf("expected ID 1, got %d", resp.ID)
	}
	if resp.Version != 1 {
		t.Errorf("expected Version 1, got %d", resp.Version)
	}
}

func TestClient_Put_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Put(ctx, &service.PutRequest{
		EntityType: "users",
		ID:         1,
		Data:       []byte("test data"),
	})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_Put_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return "invalid", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Put(ctx, &service.PutRequest{
		EntityType: "users",
		ID:         1,
		Data:       []byte("test data"),
	})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_Get_Validation(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *service.GetRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty entity type",
			req:     &service.GetRequest{ID: 1},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &service.GetRequest{EntityType: "users", ID: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Get(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Get_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.GetResponse{
				ID:        1,
				Data:      []byte("test data"),
				Version:   1,
				Timestamp: 1234567890,
				Found:     true,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.Get(ctx, &service.GetRequest{EntityType: "users", ID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Found {
		t.Error("expected Found to be true")
	}
	if string(resp.Data) != "test data" {
		t.Errorf("expected data 'test data', got %q", string(resp.Data))
	}
}

func TestClient_Get_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.Get(ctx, &service.GetRequest{EntityType: "users", ID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Found {
		t.Error("expected Found to be false for nil response")
	}
}

func TestClient_Get_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return "invalid", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Get(ctx, &service.GetRequest{EntityType: "users", ID: 1})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_Delete_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.DeleteResponse{Deleted: true, Timestamp: 1234567890}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *service.DeleteRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty entity type",
			req:     &service.DeleteRequest{ID: 1},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &service.DeleteRequest{EntityType: "users", ID: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Delete(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Delete_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.DeleteResponse{Deleted: true, Timestamp: 1234567890}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.Delete(ctx, &service.DeleteRequest{EntityType: "users", ID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}
}

func TestClient_Delete_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Delete(ctx, &service.DeleteRequest{EntityType: "users", ID: 1})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_Delete_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return "invalid", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Delete(ctx, &service.DeleteRequest{EntityType: "users", ID: 1})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_List_Validation(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *service.ListRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty entity type",
			req:     &service.ListRequest{PageSize: 10},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &service.ListRequest{EntityType: "users"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.List(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_List_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return &service.ListResponse{
				Entities: []*service.Entity{
					{EntityType: "users", ID: 1, Data: []byte("user1"), Version: 1},
					{EntityType: "users", ID: 2, Data: []byte("user2"), Version: 1},
				},
				TotalCount:    2,
				NextPageToken: "",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.List(ctx, &service.ListRequest{EntityType: "users", PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(resp.Entities))
	}
}

func TestClient_List_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.List(ctx, &service.ListRequest{EntityType: "users"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Entities == nil {
		t.Error("expected non-nil Entities slice")
	}
	if len(resp.Entities) != 0 {
		t.Errorf("expected empty Entities slice, got %d", len(resp.Entities))
	}
}

func TestClient_List_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return "invalid", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.List(ctx, &service.ListRequest{EntityType: "users"})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestClient_Watch_Validation(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	t.Run("empty key", func(t *testing.T) {
		_, err := client.Watch(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})
}

func TestClient_WatchPrefix_Validation(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	t.Run("empty prefix", func(t *testing.T) {
		_, err := client.WatchPrefix(ctx, "")
		if err == nil {
			t.Error("expected error for empty prefix")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})
}

func TestWatchOptions(t *testing.T) {
	opts := &watchOptions{}

	WithStartRevision(100)(opts)
	if opts.startRevision != 100 {
		t.Error("WithStartRevision not applied")
	}

	WithPrevKV()(opts)
	if !opts.prevKV {
		t.Error("WithPrevKV not applied")
	}

	WithProgressNotify()(opts)
	if !opts.progressNotify {
		t.Error("WithProgressNotify not applied")
	}

	WithFilterPut()(opts)
	if !opts.filterPut {
		t.Error("WithFilterPut not applied")
	}

	WithFilterDelete()(opts)
	if !opts.filterDelete {
		t.Error("WithFilterDelete not applied")
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventPut, "PUT"},
		{EventDelete, "DELETE"},
		{EventType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// mockStream implements transport.Stream for testing.
type mockStream struct {
	mu          sync.Mutex
	recvFn      func() (interface{}, error)
	sendFn      func(msg interface{}) error
	closeSendFn func() error
	closeFn     func() error
	closed      atomic.Bool
}

func (m *mockStream) Send(msg interface{}) error {
	if m.sendFn != nil {
		return m.sendFn(msg)
	}
	return nil
}

func (m *mockStream) Recv() (interface{}, error) {
	if m.recvFn != nil {
		return m.recvFn()
	}
	return nil, nil
}

func (m *mockStream) CloseSend() error {
	if m.closeSendFn != nil {
		return m.closeSendFn()
	}
	return nil
}

func (m *mockStream) Close() error {
	m.closed.Store(true)
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func TestClient_Watch_Success(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return &WatchEvent{
						Type:     EventPut,
						Key:      "test-key",
						Value:    []byte("value"),
						Revision: 1,
					}, nil
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	watcher, err := client.Watch(ctx, "test-key",
		WithStartRevision(100),
		WithPrevKV(),
		WithProgressNotify(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer watcher.Close()

	// Events channel should be available
	events := watcher.Events()
	if events == nil {
		t.Error("expected non-nil events channel")
	}
}

func TestClient_Watch_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, errors.NewConnection("test", "stream failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.Watch(ctx, "test-key")
	if err == nil {
		t.Fatal("expected error from transport")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestClient_WatchPrefix_Success(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return &WatchEvent{
						Type:     EventDelete,
						Key:      "prefix/key1",
						Revision: 5,
					}, nil
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	watcher, err := client.WatchPrefix(ctx, "prefix/",
		WithFilterPut(),
		WithFilterDelete(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer watcher.Close()
}

func TestClient_WatchPrefix_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, errors.NewUnavailable("test", "server down")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.WatchPrefix(ctx, "prefix/")
	if err == nil {
		t.Fatal("expected error from transport")
	}
}

func TestWatcher_Events(t *testing.T) {
	events := make(chan *WatchEvent, 10)
	w := &watcher{
		events: events,
	}

	// Send an event
	events <- &WatchEvent{Type: EventPut, Key: "test"}

	// Receive via Events()
	ch := w.Events()
	event := <-ch
	if event.Key != "test" {
		t.Errorf("expected key='test', got %q", event.Key)
	}
}

func TestWatcher_Err(t *testing.T) {
	testErr := errors.NewInternal("test", "stream error")
	w := &watcher{
		err: testErr,
	}

	if w.Err() != testErr {
		t.Error("expected Err to return stored error")
	}
}

func TestWatcher_Close(t *testing.T) {
	ms := &mockStream{}
	w := &watcher{
		stream: ms,
		done:   make(chan struct{}),
		events: make(chan *WatchEvent, 10),
	}

	err := w.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Close again should be no-op
	err = w.Close()
	if err != nil {
		t.Errorf("unexpected error on second close: %v", err)
	}

	if !w.closed.Load() {
		t.Error("expected watcher to be marked closed")
	}
}

func TestWatcher_CloseWithNilStream(t *testing.T) {
	w := &watcher{
		stream: nil,
		done:   make(chan struct{}),
		events: make(chan *WatchEvent, 10),
	}

	err := w.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWatcher_ReceiveLoop_Done(t *testing.T) {
	done := make(chan struct{})
	events := make(chan *WatchEvent, 10)
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			// Block until done
			<-done
			return nil, errors.NewCanceled("test", "canceled")
		},
	}

	w := &watcher{
		stream:  ms,
		events:  events,
		done:    done,
		options: &watchRequest{},
	}

	// Run receive loop in background
	go w.receiveLoop()

	// Close immediately
	close(done)

	// Wait for events channel to close
	select {
	case _, ok := <-events:
		if ok {
			// Received an event, keep draining
		}
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable for this test
	}
}

func TestWatcher_ReceiveLoop_Error(t *testing.T) {
	done := make(chan struct{})
	events := make(chan *WatchEvent, 10)
	testErr := errors.NewConnection("test", "connection lost")

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			return nil, testErr
		},
	}

	w := &watcher{
		stream:  ms,
		events:  events,
		done:    done,
		options: &watchRequest{},
	}

	// Run receive loop in background
	go w.receiveLoop()

	// Wait for events channel to close
	select {
	case _, ok := <-events:
		if ok {
			t.Error("expected events channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for events channel to close")
	}

	if w.Err() != testErr {
		t.Error("expected error to be stored")
	}
}

func TestWatcher_ReceiveLoop_NonEventMessage(t *testing.T) {
	done := make(chan struct{})
	events := make(chan *WatchEvent, 10)
	callCount := 0

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			callCount++
			if callCount == 1 {
				// Return non-WatchEvent message
				return "not a WatchEvent", nil
			}
			if callCount == 2 {
				// Return actual event
				return &WatchEvent{Type: EventPut, Key: "test"}, nil
			}
			// Return error to end loop
			return nil, errors.NewCanceled("test", "done")
		},
	}

	w := &watcher{
		stream:  ms,
		events:  events,
		done:    done,
		options: &watchRequest{},
	}

	go w.receiveLoop()

	// Should receive the valid event
	select {
	case event := <-events:
		if event.Key != "test" {
			t.Errorf("expected key='test', got %q", event.Key)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestWatcher_ReceiveLoop_FilterPut(t *testing.T) {
	done := make(chan struct{})
	events := make(chan *WatchEvent, 10)
	callCount := 0

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			callCount++
			if callCount == 1 {
				// PUT event (should be filtered)
				return &WatchEvent{Type: EventPut, Key: "filtered"}, nil
			}
			if callCount == 2 {
				// DELETE event (should pass through)
				return &WatchEvent{Type: EventDelete, Key: "passed"}, nil
			}
			return nil, errors.NewCanceled("test", "done")
		},
	}

	w := &watcher{
		stream:  ms,
		events:  events,
		done:    done,
		options: &watchRequest{FilterPut: true},
	}

	go w.receiveLoop()

	// Should only receive DELETE event
	select {
	case event := <-events:
		if event.Key != "passed" {
			t.Errorf("expected key='passed', got %q", event.Key)
		}
		if event.Type != EventDelete {
			t.Errorf("expected DELETE event, got %v", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestWatcher_ReceiveLoop_FilterDelete(t *testing.T) {
	done := make(chan struct{})
	events := make(chan *WatchEvent, 10)
	callCount := 0

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			callCount++
			if callCount == 1 {
				// DELETE event (should be filtered)
				return &WatchEvent{Type: EventDelete, Key: "filtered"}, nil
			}
			if callCount == 2 {
				// PUT event (should pass through)
				return &WatchEvent{Type: EventPut, Key: "passed"}, nil
			}
			return nil, errors.NewCanceled("test", "done")
		},
	}

	w := &watcher{
		stream:  ms,
		events:  events,
		done:    done,
		options: &watchRequest{FilterDelete: true},
	}

	go w.receiveLoop()

	// Should only receive PUT event
	select {
	case event := <-events:
		if event.Key != "passed" {
			t.Errorf("expected key='passed', got %q", event.Key)
		}
		if event.Type != EventPut {
			t.Errorf("expected PUT event, got %v", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestWatchEvent_Fields(t *testing.T) {
	now := time.Now()
	event := &WatchEvent{
		Type:      EventPut,
		Key:       "test-key",
		Value:     []byte("new-value"),
		PrevValue: []byte("old-value"),
		Revision:  100,
		Timestamp: now,
	}

	if event.Type != EventPut {
		t.Errorf("expected EventPut, got %v", event.Type)
	}
	if event.Key != "test-key" {
		t.Errorf("expected key='test-key', got %q", event.Key)
	}
	if string(event.Value) != "new-value" {
		t.Errorf("expected value='new-value', got %q", string(event.Value))
	}
	if string(event.PrevValue) != "old-value" {
		t.Errorf("expected prevValue='old-value', got %q", string(event.PrevValue))
	}
	if event.Revision != 100 {
		t.Errorf("expected revision=100, got %d", event.Revision)
	}
	if !event.Timestamp.Equal(now) {
		t.Errorf("expected timestamp=%v, got %v", now, event.Timestamp)
	}
}

func TestWatchRequest_Fields(t *testing.T) {
	req := &watchRequest{
		Key:            "key",
		Prefix:         "prefix",
		IsPrefix:       true,
		StartRevision:  50,
		PrevKV:         true,
		ProgressNotify: true,
		FilterPut:      true,
		FilterDelete:   true,
	}

	if req.Key != "key" {
		t.Error("Key not set correctly")
	}
	if req.Prefix != "prefix" {
		t.Error("Prefix not set correctly")
	}
	if !req.IsPrefix {
		t.Error("IsPrefix not set correctly")
	}
	if req.StartRevision != 50 {
		t.Error("StartRevision not set correctly")
	}
	if !req.PrevKV {
		t.Error("PrevKV not set correctly")
	}
	if !req.ProgressNotify {
		t.Error("ProgressNotify not set correctly")
	}
	if !req.FilterPut {
		t.Error("FilterPut not set correctly")
	}
	if !req.FilterDelete {
		t.Error("FilterDelete not set correctly")
	}
}

func TestClient_Watch_StartRevisionOverflow(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			t.Fatal("a watch with an unrepresentable start revision must not reach the transport")
			return nil, nil
		},
	}
	client := NewClient(mockT)

	_, err := client.Watch(context.Background(), "test-key", WithStartRevision(math.MaxUint64))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput error, got %v", err)
	}
}

func TestWatchEvents_ServiceResponse(t *testing.T) {
	resp := &service.WatchResponse{
		WatchID: 1,
		Events: []*service.WatchEvent{
			{Type: service.WatchEventTypePut, Key: []byte("a"), Value: []byte("1"), Revision: 7, Timestamp: 42},
			nil,
			{Type: service.WatchEventTypeUnspecified, Key: []byte("skipped")},
			{Type: service.WatchEventTypeDelete, Key: []byte("b"), PrevValue: []byte("2"), Revision: -1},
		},
	}

	events := watchEvents(resp)
	if len(events) != 2 {
		t.Fatalf("expected 2 mapped events, got %d", len(events))
	}
	put, del := events[0], events[1]
	if put.Type != EventPut || put.Key != "a" || string(put.Value) != "1" || put.Revision != 7 {
		t.Errorf("unexpected put event: %+v", put)
	}
	if !put.Timestamp.Equal(time.Unix(0, 42)) {
		t.Errorf("unexpected put timestamp: %v", put.Timestamp)
	}
	if del.Type != EventDelete || del.Key != "b" || string(del.PrevValue) != "2" || del.Revision != 0 {
		t.Errorf("unexpected delete event: %+v", del)
	}
}

func TestWatchEvents_NoEvents(t *testing.T) {
	if got := watchEvents(&service.WatchResponse{WatchID: 1, Created: true}); len(got) != 0 {
		t.Errorf("creation confirmation must carry no events, got %d", len(got))
	}
	if got := watchEvents("not a watch message"); got != nil {
		t.Errorf("unknown message must carry no events, got %v", got)
	}
}
