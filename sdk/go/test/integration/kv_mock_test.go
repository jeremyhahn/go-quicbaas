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

package integration

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/mock"
)

// TestMockKVPut tests Put operations with mock transport.
func TestMockKVPut(t *testing.T) {
	t.Run("put small payload succeeds", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedVersion := uint64(100)
		transport.SetResponse("quicbaas.KV", "Put", &service.PutResponse{
			Version: expectedVersion,
		}, nil)

		ctx := context.Background()
		resp, err := client.Put(ctx, &service.PutRequest{
			EntityType: "users",
			ID:         123,
			Data:       []byte(`{"name":"John"}`),
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Version != expectedVersion {
			t.Errorf("expected version %d, got %d", expectedVersion, resp.Version)
		}

		// Verify request was captured correctly
		requests := transport.Requests()
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}

		req := requests[0]
		if req.Service != "quicbaas.KV" {
			t.Errorf("expected service quicbaas.KV, got %s", req.Service)
		}
		if req.Method != "Put" {
			t.Errorf("expected method Put, got %s", req.Method)
		}

		putReq, ok := req.Payload.(*service.PutRequest)
		if !ok {
			t.Fatal("payload is not a PutRequest")
		}
		if putReq.EntityType != "users" {
			t.Errorf("expected entity type users, got %s", putReq.EntityType)
		}
		if putReq.ID != 123 {
			t.Errorf("expected ID 123, got %d", putReq.ID)
		}
	})

	t.Run("put large payload succeeds", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		// Create a 1MB payload
		largeValue := make([]byte, 1024*1024)
		for i := range largeValue {
			largeValue[i] = byte(i % 256)
		}

		transport.SetResponse("quicbaas.KV", "Put", &service.PutResponse{
			Version: 200,
		}, nil)

		ctx := context.Background()
		resp, err := client.Put(ctx, &service.PutRequest{
			EntityType: "documents",
			ID:         1,
			Data:       largeValue,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Version != 200 {
			t.Errorf("expected version 200, got %d", resp.Version)
		}

		// Verify large value was transmitted correctly
		lastReq := transport.LastRequest()
		if lastReq == nil {
			t.Fatal("expected a request to be recorded")
		}
		putReq := lastReq.Payload.(*service.PutRequest)
		if len(putReq.Data) != len(largeValue) {
			t.Errorf("expected data length %d, got %d", len(largeValue), len(putReq.Data))
		}
	})

	t.Run("put with nil request returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Put(ctx, nil)

		if err == nil {
			t.Fatal("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("put with empty entity type returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Put(ctx, &service.PutRequest{
			EntityType: "",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error for empty entity type")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

// TestMockKVGet tests Get operations with mock transport.
func TestMockKVGet(t *testing.T) {
	t.Run("get existing entity returns data", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedData := []byte(`{"name":"Jane","age":30}`)
		transport.SetResponse("quicbaas.KV", "Get", &service.GetResponse{
			Found:   true,
			Data:    expectedData,
			Version: 50,
		}, nil)

		ctx := context.Background()
		resp, err := client.Get(ctx, &service.GetRequest{
			EntityType: "users",
			ID:         456,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Found {
			t.Error("expected entity to be found")
		}
		if string(resp.Data) != string(expectedData) {
			t.Errorf("expected data %s, got %s", expectedData, resp.Data)
		}
		if resp.Version != 50 {
			t.Errorf("expected version 50, got %d", resp.Version)
		}
	})

	t.Run("get non-existing entity returns not found", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Get", &service.GetResponse{
			Found: false,
		}, nil)

		ctx := context.Background()
		resp, err := client.Get(ctx, &service.GetRequest{
			EntityType: "users",
			ID:         999,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Found {
			t.Error("expected entity to not be found")
		}
	})

	t.Run("get nil response returns not found", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Get", nil, nil)

		ctx := context.Background()
		resp, err := client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Found {
			t.Error("expected not found for nil response")
		}
	})

	t.Run("get with nil request returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Get(ctx, nil)

		if err == nil {
			t.Fatal("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("get with empty entity type returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Get(ctx, &service.GetRequest{
			EntityType: "",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error for empty entity type")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

// TestMockKVDelete tests Delete operations with mock transport.
func TestMockKVDelete(t *testing.T) {
	t.Run("delete existing entity succeeds", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Delete", &service.DeleteResponse{
			Deleted: true,
		}, nil)

		ctx := context.Background()
		resp, err := client.Delete(ctx, &service.DeleteRequest{
			EntityType: "users",
			ID:         789,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Deleted {
			t.Error("expected deleted to be true")
		}

		// Verify request
		lastReq := transport.LastRequest()
		deleteReq := lastReq.Payload.(*service.DeleteRequest)
		if deleteReq.ID != 789 {
			t.Errorf("expected ID 789, got %d", deleteReq.ID)
		}
	})

	t.Run("delete non-existing entity returns deleted false", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Delete", &service.DeleteResponse{
			Deleted: false,
		}, nil)

		ctx := context.Background()
		resp, err := client.Delete(ctx, &service.DeleteRequest{
			EntityType: "users",
			ID:         999,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Deleted {
			t.Error("expected deleted to be false for non-existing entity")
		}
	})

	t.Run("delete with nil request returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Delete(ctx, nil)

		if err == nil {
			t.Fatal("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("delete with empty entity type returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Delete(ctx, &service.DeleteRequest{
			EntityType: "",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error for empty entity type")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

// TestMockKVList tests List operations with mock transport.
func TestMockKVList(t *testing.T) {
	t.Run("list returns matching entities", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedEntities := []*service.Entity{
			{EntityType: "users", ID: 1},
			{EntityType: "users", ID: 2},
			{EntityType: "users", ID: 3},
		}
		transport.SetResponse("quicbaas.KV", "List", &service.ListResponse{
			Entities:      expectedEntities,
			NextPageToken: "",
			TotalCount:    3,
		}, nil)

		ctx := context.Background()
		resp, err := client.List(ctx, &service.ListRequest{
			EntityType: "users",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Entities) != 3 {
			t.Errorf("expected 3 entities, got %d", len(resp.Entities))
		}

		// Verify request
		lastReq := transport.LastRequest()
		listReq := lastReq.Payload.(*service.ListRequest)
		if listReq.EntityType != "users" {
			t.Errorf("expected entity type users, got %s", listReq.EntityType)
		}
	})

	t.Run("list with pagination returns page token", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "List", &service.ListResponse{
			Entities: []*service.Entity{
				{EntityType: "data", ID: 1},
				{EntityType: "data", ID: 2},
			},
			NextPageToken: "cursor123",
			TotalCount:    10,
		}, nil)

		ctx := context.Background()
		resp, err := client.List(ctx, &service.ListRequest{
			EntityType: "data",
			PageSize:   2,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Entities) != 2 {
			t.Errorf("expected 2 entities, got %d", len(resp.Entities))
		}
		if resp.NextPageToken != "cursor123" {
			t.Errorf("expected page token cursor123, got %s", resp.NextPageToken)
		}
		if resp.TotalCount != 10 {
			t.Errorf("expected total count 10, got %d", resp.TotalCount)
		}

		// Verify page size in request
		lastReq := transport.LastRequest()
		listReq := lastReq.Payload.(*service.ListRequest)
		if listReq.PageSize != 2 {
			t.Errorf("expected page size 2, got %d", listReq.PageSize)
		}
	})

	t.Run("list with page token continues from position", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "List", &service.ListResponse{
			Entities: []*service.Entity{
				{EntityType: "data", ID: 3},
				{EntityType: "data", ID: 4},
			},
			NextPageToken: "",
			TotalCount:    4,
		}, nil)

		ctx := context.Background()
		resp, err := client.List(ctx, &service.ListRequest{
			EntityType: "data",
			PageSize:   2,
			PageToken:  "cursor123",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Entities) != 2 {
			t.Errorf("expected 2 entities, got %d", len(resp.Entities))
		}
		if resp.NextPageToken != "" {
			t.Errorf("expected empty next page token, got %s", resp.NextPageToken)
		}

		// Verify page token in request
		lastReq := transport.LastRequest()
		listReq := lastReq.Payload.(*service.ListRequest)
		if listReq.PageToken != "cursor123" {
			t.Errorf("expected page token cursor123, got %s", listReq.PageToken)
		}
	})

	t.Run("list empty results", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "List", &service.ListResponse{
			Entities: []*service.Entity{},
		}, nil)

		ctx := context.Background()
		resp, err := client.List(ctx, &service.ListRequest{
			EntityType: "empty",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Entities) != 0 {
			t.Errorf("expected 0 entities, got %d", len(resp.Entities))
		}
	})

	t.Run("list nil response returns empty list", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "List", nil, nil)

		ctx := context.Background()
		resp, err := client.List(ctx, &service.ListRequest{
			EntityType: "test",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Entities == nil {
			t.Error("expected Entities to be initialized")
		}
		if len(resp.Entities) != 0 {
			t.Errorf("expected 0 entities, got %d", len(resp.Entities))
		}
	})

	t.Run("list with nil request returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.List(ctx, nil)

		if err == nil {
			t.Fatal("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

// TestMockKVWatch tests Watch operations with mock transport.
func TestMockKVWatch(t *testing.T) {
	t.Run("watch key receives PUT events", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		putEvent := &kv.WatchEvent{
			Type:      kv.EventPut,
			Key:       "config:db",
			Value:     []byte("postgres://localhost"),
			Revision:  10,
			Timestamp: time.Now(),
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{putEvent},
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.Watch(ctx, "config:db")
		if err != nil {
			t.Fatalf("unexpected error starting watch: %v", err)
		}
		defer watcher.Close()

		select {
		case event := <-watcher.Events():
			if event.Type != kv.EventPut {
				t.Errorf("expected PUT event, got %s", event.Type.String())
			}
			if event.Key != "config:db" {
				t.Errorf("expected key config:db, got %s", event.Key)
			}
			if string(event.Value) != "postgres://localhost" {
				t.Errorf("expected value postgres://localhost, got %s", event.Value)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	})

	t.Run("watch key receives DELETE events", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		deleteEvent := &kv.WatchEvent{
			Type:      kv.EventDelete,
			Key:       "temp:key",
			Revision:  20,
			Timestamp: time.Now(),
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{deleteEvent},
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.Watch(ctx, "temp:key")
		if err != nil {
			t.Fatalf("unexpected error starting watch: %v", err)
		}
		defer watcher.Close()

		select {
		case event := <-watcher.Events():
			if event.Type != kv.EventDelete {
				t.Errorf("expected DELETE event, got %s", event.Type.String())
			}
			if event.Key != "temp:key" {
				t.Errorf("expected key temp:key, got %s", event.Key)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	})

	t.Run("watch with empty key returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.Watch(ctx, "")

		if err == nil {
			t.Fatal("expected error for empty key")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("watch stores request correctly", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{},
			RecvError: io.EOF,
		})

		ctx := context.Background()
		watcher, err := client.Watch(ctx, "test:key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		streamReqs := transport.StreamRequests()
		if len(streamReqs) != 1 {
			t.Fatalf("expected 1 stream request, got %d", len(streamReqs))
		}

		req := streamReqs[0]
		if req.Service != "quicbaas.KV" {
			t.Errorf("expected service quicbaas.KV, got %s", req.Service)
		}
		if req.Method != "Watch" {
			t.Errorf("expected method Watch, got %s", req.Method)
		}
	})
}

// TestMockKVWatchPrefix tests WatchPrefix operations with mock transport.
func TestMockKVWatchPrefix(t *testing.T) {
	t.Run("watch prefix receives events for multiple keys", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		events := []interface{}{
			&kv.WatchEvent{
				Type:      kv.EventPut,
				Key:       "users:1",
				Value:     []byte("user1"),
				Revision:  1,
				Timestamp: time.Now(),
			},
			&kv.WatchEvent{
				Type:      kv.EventPut,
				Key:       "users:2",
				Value:     []byte("user2"),
				Revision:  2,
				Timestamp: time.Now(),
			},
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  events,
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.WatchPrefix(ctx, "users:")
		if err != nil {
			t.Fatalf("unexpected error starting watch: %v", err)
		}
		defer watcher.Close()

		receivedCount := 0
		for i := 0; i < 2; i++ {
			select {
			case event := <-watcher.Events():
				if !strings.HasPrefix(event.Key, "users:") {
					t.Errorf("expected key with prefix users:, got %s", event.Key)
				}
				receivedCount++
			case <-ctx.Done():
				t.Fatalf("timeout waiting for event %d", i+1)
			}
		}

		if receivedCount != 2 {
			t.Errorf("expected 2 events, got %d", receivedCount)
		}
	})

	t.Run("watch prefix with empty prefix returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx := context.Background()
		_, err := client.WatchPrefix(ctx, "")

		if err == nil {
			t.Fatal("expected error for empty prefix")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

// TestMockKVWatchOptions tests Watch with various options.
func TestMockKVWatchOptions(t *testing.T) {
	t.Run("watch with StartRevision option", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{},
			RecvError: io.EOF,
		})

		ctx := context.Background()
		watcher, err := client.Watch(ctx, "key", kv.WithStartRevision(100))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		// Verify stream request contains StartRevision
		streamReqs := transport.StreamRequests()
		if len(streamReqs) == 0 {
			t.Fatal("expected stream request to be recorded")
		}
	})

	t.Run("watch with PrevKV option", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		prevValue := []byte("old-value")
		newValue := []byte("new-value")
		event := &kv.WatchEvent{
			Type:      kv.EventPut,
			Key:       "key",
			Value:     newValue,
			PrevValue: prevValue,
			Revision:  50,
			Timestamp: time.Now(),
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{event},
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.Watch(ctx, "key", kv.WithPrevKV())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		select {
		case e := <-watcher.Events():
			if string(e.PrevValue) != string(prevValue) {
				t.Errorf("expected prev value %s, got %s", prevValue, e.PrevValue)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	})

	t.Run("watch with FilterPut filters PUT events", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		events := []interface{}{
			&kv.WatchEvent{
				Type:      kv.EventPut,
				Key:       "key",
				Value:     []byte("value"),
				Revision:  1,
				Timestamp: time.Now(),
			},
			&kv.WatchEvent{
				Type:      kv.EventDelete,
				Key:       "key",
				Revision:  2,
				Timestamp: time.Now(),
			},
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  events,
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.Watch(ctx, "key", kv.WithFilterPut())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		// Should only receive DELETE event
		select {
		case event := <-watcher.Events():
			if event.Type != kv.EventDelete {
				t.Errorf("expected DELETE event only, got %s", event.Type.String())
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	})

	t.Run("watch with FilterDelete filters DELETE events", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		events := []interface{}{
			&kv.WatchEvent{
				Type:      kv.EventDelete,
				Key:       "key",
				Revision:  1,
				Timestamp: time.Now(),
			},
			&kv.WatchEvent{
				Type:      kv.EventPut,
				Key:       "key",
				Value:     []byte("value"),
				Revision:  2,
				Timestamp: time.Now(),
			},
		}

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  events,
			RecvError: io.EOF,
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		watcher, err := client.Watch(ctx, "key", kv.WithFilterDelete())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		// Should only receive PUT event
		select {
		case event := <-watcher.Events():
			if event.Type != kv.EventPut {
				t.Errorf("expected PUT event only, got %s", event.Type.String())
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	})
}

// TestMockKVErrorHandling tests error handling scenarios.
func TestMockKVErrorHandling(t *testing.T) {
	t.Run("transport error on Put", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedErr := errors.NewUnavailable("test", "server unavailable")
		transport.SetResponse("quicbaas.KV", "Put", nil, expectedErr)

		ctx := context.Background()
		_, err := client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("transport error on Get", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedErr := errors.NewTimeout("test", "request timeout")
		transport.SetResponse("quicbaas.KV", "Get", nil, expectedErr)

		ctx := context.Background()
		_, err := client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsTimeout(err) {
			t.Errorf("expected timeout error, got %v", err)
		}
	})

	t.Run("transport error on Delete", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedErr := errors.NewInternal("test", "internal error")
		transport.SetResponse("quicbaas.KV", "Delete", nil, expectedErr)

		ctx := context.Background()
		_, err := client.Delete(ctx, &service.DeleteRequest{
			EntityType: "test",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected internal error, got %v", err)
		}
	})

	t.Run("transport error on List", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		expectedErr := errors.NewConnection("test", "connection refused")
		transport.SetResponse("quicbaas.KV", "List", nil, expectedErr)

		ctx := context.Background()
		_, err := client.List(ctx, &service.ListRequest{
			EntityType: "test",
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Code(err) != errors.CodeConnection {
			t.Errorf("expected connection error, got %v", err)
		}
	})

	t.Run("context cancellation on Put", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error for canceled context")
		}
		if !errors.IsCanceled(err) {
			t.Errorf("expected canceled error, got %v", err)
		}
	})

	t.Run("context cancellation on Get", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error for canceled context")
		}
		if !errors.IsCanceled(err) {
			t.Errorf("expected canceled error, got %v", err)
		}
	})

	t.Run("closed transport returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		err := transport.Close()
		if err != nil {
			t.Fatalf("failed to close transport: %v", err)
		}

		ctx := context.Background()
		_, err = client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error for closed transport")
		}
		if errors.Code(err) != errors.CodeConnection {
			t.Errorf("expected connection error, got %v", err)
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		// Return wrong response type
		transport.SetResponse("quicbaas.KV", "Put", &service.GetResponse{}, nil)

		ctx := context.Background()
		_, err := client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error for invalid response type")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected internal error, got %v", err)
		}
	})

	t.Run("nil response on Put returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Put", nil, nil)

		ctx := context.Background()
		_, err := client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value"),
		})

		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected internal error, got %v", err)
		}
	})

	t.Run("nil response on Delete returns error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Delete", nil, nil)

		ctx := context.Background()
		_, err := client.Delete(ctx, &service.DeleteRequest{
			EntityType: "test",
			ID:         1,
		})

		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected internal error, got %v", err)
		}
	})
}

// TestMockKVWatcherLifecycle tests watcher lifecycle management.
func TestMockKVWatcherLifecycle(t *testing.T) {
	t.Run("watcher close is idempotent", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{},
			RecvError: io.EOF,
		})

		ctx := context.Background()
		watcher, err := client.Watch(ctx, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Close multiple times should not panic
		if err := watcher.Close(); err != nil {
			t.Errorf("first close returned error: %v", err)
		}
		if err := watcher.Close(); err != nil {
			t.Errorf("second close returned error: %v", err)
		}
	})

	t.Run("watcher Err returns stream error", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		streamErr := errors.NewInternal("stream", "stream broke")
		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{},
			RecvError: streamErr,
		})

		ctx := context.Background()
		watcher, err := client.Watch(ctx, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		// Wait for events channel to close (indicating stream ended)
		for range watcher.Events() {
			// Drain events
		}

		// Check error
		watchErr := watcher.Err()
		if watchErr == nil {
			t.Error("expected error from watcher")
		}
	})

	t.Run("watcher events channel closes on stream end", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetStreamConfig("quicbaas.KV", "Watch", mock.StreamConfig{
			Messages:  []interface{}{},
			RecvError: io.EOF,
		})

		ctx := context.Background()
		watcher, err := client.Watch(ctx, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer watcher.Close()

		// Events channel should close
		select {
		case _, ok := <-watcher.Events():
			if ok {
				// Got an event, keep waiting
			} else {
				// Channel closed as expected
				return
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for events channel to close")
		}
	})
}

// TestMockKVClientNil tests nil transport handling.
func TestMockKVClientNil(t *testing.T) {
	t.Run("NewClient with nil transport returns nil", func(t *testing.T) {
		client := kv.NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

// TestMockKVTransportReset tests transport reset functionality.
func TestMockKVTransportReset(t *testing.T) {
	t.Run("transport reset clears requests", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Put", &service.PutResponse{Version: 1}, nil)

		ctx := context.Background()
		_, _ = client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("value1"),
		})
		_, _ = client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         2,
			Data:       []byte("value2"),
		})

		if transport.RequestCount() != 2 {
			t.Errorf("expected 2 requests, got %d", transport.RequestCount())
		}

		transport.Reset()

		if transport.RequestCount() != 0 {
			t.Errorf("expected 0 requests after reset, got %d", transport.RequestCount())
		}
	})

	t.Run("transport reset clears responses", func(t *testing.T) {
		transport := mock.NewTransport()
		client := kv.NewClient(transport)

		transport.SetResponse("quicbaas.KV", "Get", &service.GetResponse{
			Found: true,
			Data:  []byte("value"),
		}, nil)

		ctx := context.Background()
		resp, err := client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Found {
			t.Error("expected entity to be found before reset")
		}

		transport.Reset()

		// After reset, default behavior returns nil response
		resp, err = client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Found {
			t.Error("expected entity to not be found after reset")
		}
	})
}

// TestMockKVTransportHealth tests transport health functionality.
func TestMockKVTransportHealth(t *testing.T) {
	t.Run("transport health status", func(t *testing.T) {
		transport := mock.NewTransport()

		if !transport.Healthy() {
			t.Error("expected transport to be healthy initially")
		}

		transport.SetHealthy(false)
		if transport.Healthy() {
			t.Error("expected transport to be unhealthy after SetHealthy(false)")
		}

		transport.SetHealthy(true)
		if !transport.Healthy() {
			t.Error("expected transport to be healthy after SetHealthy(true)")
		}
	})

	t.Run("closed transport is not healthy", func(t *testing.T) {
		transport := mock.NewTransport()

		if !transport.Healthy() {
			t.Error("expected transport to be healthy initially")
		}

		if err := transport.Close(); err != nil {
			t.Fatalf("failed to close transport: %v", err)
		}

		if transport.Healthy() {
			t.Error("expected closed transport to be unhealthy")
		}
	})
}

// TestMockEventTypeString tests EventType String method.
func TestMockEventTypeString(t *testing.T) {
	tests := []struct {
		eventType kv.EventType
		expected  string
	}{
		{kv.EventPut, "PUT"},
		{kv.EventDelete, "DELETE"},
		{kv.EventType(99), "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := tc.eventType.String()
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}
