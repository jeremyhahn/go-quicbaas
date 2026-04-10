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

package mock

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestServer_StartStop(t *testing.T) {
	t.Run("starts and stops successfully", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		err := server.Start()
		if err != nil {
			t.Fatalf("unexpected error starting server: %v", err)
		}

		if !server.IsRunning() {
			t.Error("expected server to be running")
		}

		addr := server.Address()
		if addr == "" {
			t.Error("expected non-empty address")
		}

		err = server.Stop()
		if err != nil {
			t.Fatalf("unexpected error stopping server: %v", err)
		}

		if server.IsRunning() {
			t.Error("expected server to not be running")
		}
	})

	t.Run("returns empty address when not running", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		addr := server.Address()

		if addr != "" {
			t.Errorf("expected empty address, got %s", addr)
		}
	})
}

func TestServer_StartStop_Error(t *testing.T) {
	t.Run("returns error when already running", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		_ = server.Start()
		defer server.Stop()

		err := server.Start()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when not running", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		err := server.Stop()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error for invalid address", func(t *testing.T) {
		config := ServerConfig{
			Address: "invalid-address-format",
			Network: "tcp",
		}
		server := NewServer(config)

		err := server.Start()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestServer_HandleRequest(t *testing.T) {
	t.Run("dispatches to registered handler", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		expectedResponse := "response"
		server.RegisterHandler("kv", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return expectedResponse, nil
		})

		resp, err := server.HandleRequest(context.Background(), "kv", "Get", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != expectedResponse {
			t.Errorf("expected %v, got %v", expectedResponse, resp)
		}
	})

	t.Run("dispatches to default handler when no specific handler", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		expectedResponse := "default"
		server.SetDefaultHandler(func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return expectedResponse, nil
		})

		resp, err := server.HandleRequest(context.Background(), "unknown", "method", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != expectedResponse {
			t.Errorf("expected %v, got %v", expectedResponse, resp)
		}
	})

	t.Run("captures requests", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		server.RegisterHandler("kv", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return nil, nil
		})

		_, _ = server.HandleRequest(context.Background(), "kv", "Get", "payload")

		requests := server.Requests()
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}
		if requests[0].Service != "kv" {
			t.Errorf("expected service=kv, got %s", requests[0].Service)
		}
		if requests[0].Method != "Get" {
			t.Errorf("expected method=Get, got %s", requests[0].Method)
		}
		if requests[0].Payload != "payload" {
			t.Errorf("expected payload=payload, got %v", requests[0].Payload)
		}
	})
}

func TestServer_HandleRequest_Error(t *testing.T) {
	t.Run("returns error when no handler registered", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		_, err := server.HandleRequest(context.Background(), "unknown", "method", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("returns handler error", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		expectedErr := errors.NewInternal("test", "handler error")
		server.RegisterHandler("kv", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		})

		_, err := server.HandleRequest(context.Background(), "kv", "Get", nil)

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestServer_HandleStreamRequest(t *testing.T) {
	t.Run("dispatches to registered stream handler", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		handlerCalled := false
		server.RegisterStreamHandler("watch", func(ctx context.Context, method string, req interface{}, stream ServerStream) error {
			handlerCalled = true
			return nil
		})

		stream := NewMockServerStream()
		err := server.HandleStreamRequest(context.Background(), "watch", "Watch", nil, stream)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handlerCalled {
			t.Error("expected handler to be called")
		}
	})

	t.Run("dispatches to default stream handler", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		handlerCalled := false
		server.SetDefaultStreamHandler(func(ctx context.Context, method string, req interface{}, stream ServerStream) error {
			handlerCalled = true
			return nil
		})

		stream := NewMockServerStream()
		err := server.HandleStreamRequest(context.Background(), "unknown", "method", nil, stream)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handlerCalled {
			t.Error("expected default handler to be called")
		}
	})

	t.Run("captures stream requests", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		server.RegisterStreamHandler("watch", func(ctx context.Context, method string, req interface{}, stream ServerStream) error {
			return nil
		})

		stream := NewMockServerStream()
		_ = server.HandleStreamRequest(context.Background(), "watch", "Watch", "payload", stream)

		requests := server.Requests()
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}
		if requests[0].Service != "watch" {
			t.Errorf("expected service=watch, got %s", requests[0].Service)
		}
	})
}

func TestServer_HandleStreamRequest_Error(t *testing.T) {
	t.Run("returns error when no handler registered", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		stream := NewMockServerStream()
		err := server.HandleStreamRequest(context.Background(), "unknown", "method", nil, stream)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("returns handler error", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		expectedErr := errors.NewInternal("test", "stream handler error")
		server.RegisterStreamHandler("watch", func(ctx context.Context, method string, req interface{}, stream ServerStream) error {
			return expectedErr
		})

		stream := NewMockServerStream()
		err := server.HandleStreamRequest(context.Background(), "watch", "Watch", nil, stream)

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestServer_RequestTracking(t *testing.T) {
	t.Run("LastRequest returns nil when no requests", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())

		last := server.LastRequest()

		if last != nil {
			t.Errorf("expected nil, got %v", last)
		}
	})

	t.Run("LastRequest returns last request", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		server.RegisterHandler("svc", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return nil, nil
		})
		_, _ = server.HandleRequest(context.Background(), "svc", "method1", nil)
		_, _ = server.HandleRequest(context.Background(), "svc", "method2", nil)

		last := server.LastRequest()

		if last == nil {
			t.Fatal("expected last request, got nil")
		}
		if last.Method != "method2" {
			t.Errorf("expected method=method2, got %s", last.Method)
		}
	})

	t.Run("RequestCount returns correct count", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		server.RegisterHandler("svc", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return nil, nil
		})
		_, _ = server.HandleRequest(context.Background(), "svc", "method", nil)
		_, _ = server.HandleRequest(context.Background(), "svc", "method", nil)
		_, _ = server.HandleRequest(context.Background(), "svc", "method", nil)

		count := server.RequestCount()

		if count != 3 {
			t.Errorf("expected 3, got %d", count)
		}
	})
}

func TestServer_Reset(t *testing.T) {
	t.Run("clears all state", func(t *testing.T) {
		server := NewServer(DefaultServerConfig())
		server.RegisterHandler("svc", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return nil, nil
		})
		server.RegisterStreamHandler("stream", func(ctx context.Context, method string, req interface{}, stream ServerStream) error {
			return nil
		})
		server.SetDefaultHandler(func(ctx context.Context, method string, req interface{}) (interface{}, error) {
			return "default", nil
		})
		_, _ = server.HandleRequest(context.Background(), "svc", "method", nil)

		server.Reset()

		if server.RequestCount() != 0 {
			t.Error("expected requests to be cleared")
		}

		// Verify handlers are cleared by checking for not found error
		_, err := server.HandleRequest(context.Background(), "svc", "method", nil)
		if !errors.IsNotFound(err) {
			t.Error("expected handlers to be cleared")
		}
	})
}

func TestServer_Network(t *testing.T) {
	t.Run("returns configured network", func(t *testing.T) {
		config := ServerConfig{
			Address: "127.0.0.1:0",
			Network: "tcp",
		}
		server := NewServer(config)

		network := server.Network()

		if network != "tcp" {
			t.Errorf("expected tcp, got %s", network)
		}
	})
}

func TestMockServerStream_Send(t *testing.T) {
	t.Run("captures sent messages", func(t *testing.T) {
		stream := NewMockServerStream()

		err := stream.Send("message1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		err = stream.Send("message2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sent := stream.SentMessages()
		if len(sent) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(sent))
		}
		if sent[0] != "message1" {
			t.Errorf("expected message1, got %v", sent[0])
		}
		if sent[1] != "message2" {
			t.Errorf("expected message2, got %v", sent[1])
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		stream := NewMockServerStream()
		expectedErr := errors.NewInternal("test", "send failed")
		stream.SetSendError(expectedErr)

		err := stream.Send("message")

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestMockServerStream_Send_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		stream := NewMockServerStream()
		_ = stream.Close()

		err := stream.Send("message")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestMockServerStream_Recv(t *testing.T) {
	t.Run("returns configured messages", func(t *testing.T) {
		stream := NewMockServerStream()
		stream.SetRecvMessages([]interface{}{"msg1", "msg2"})

		msg1, err := stream.Recv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg1 != "msg1" {
			t.Errorf("expected msg1, got %v", msg1)
		}

		msg2, err := stream.Recv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg2 != "msg2" {
			t.Errorf("expected msg2, got %v", msg2)
		}
	})

	t.Run("returns configured error after messages", func(t *testing.T) {
		stream := NewMockServerStream()
		stream.SetRecvMessages([]interface{}{"msg1"})
		expectedErr := errors.NewInternal("test", "recv failed")
		stream.SetRecvError(expectedErr)

		_, _ = stream.Recv() // consume first message

		_, err := stream.Recv()

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestMockServerStream_Recv_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		stream := NewMockServerStream()
		stream.SetRecvMessages([]interface{}{"msg1"})
		_ = stream.Close()

		_, err := stream.Recv()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when no more messages", func(t *testing.T) {
		stream := NewMockServerStream()
		// No messages configured

		_, err := stream.Recv()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestMockServerStream_Close(t *testing.T) {
	t.Run("closes stream", func(t *testing.T) {
		stream := NewMockServerStream()

		err := stream.Close()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !stream.IsClosed() {
			t.Error("expected stream to be closed")
		}
	})
}

func TestMockServerStream_Close_Error(t *testing.T) {
	t.Run("returns error when already closed", func(t *testing.T) {
		stream := NewMockServerStream()
		_ = stream.Close()

		err := stream.Close()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestDefaultServerConfig(t *testing.T) {
	t.Run("returns sensible defaults", func(t *testing.T) {
		config := DefaultServerConfig()

		if config.Address == "" {
			t.Error("expected non-empty address")
		}
		if config.Network == "" {
			t.Error("expected non-empty network")
		}
		if config.Network != "tcp" {
			t.Errorf("expected tcp network, got %s", config.Network)
		}
	})
}
