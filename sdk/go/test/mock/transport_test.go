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
	"io"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestTransport_Request(t *testing.T) {
	t.Run("returns configured response", func(t *testing.T) {
		transport := NewTransport()
		expectedResponse := map[string]string{"key": "value"}
		transport.SetResponse("kv", "Get", expectedResponse, nil)

		resp, err := transport.Request(context.Background(), "kv", "Get", map[string]string{"key": "test"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		result, ok := resp.(map[string]string)
		if !ok {
			t.Fatalf("unexpected response type: %T", resp)
		}
		if result["key"] != "value" {
			t.Errorf("expected key=value, got key=%s", result["key"])
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		transport := NewTransport()
		expectedErr := errors.NewNotFound("test", "key not found")
		transport.SetResponse("kv", "Get", nil, expectedErr)

		_, err := transport.Request(context.Background(), "kv", "Get", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("captures requests", func(t *testing.T) {
		transport := NewTransport()
		payload := map[string]string{"key": "test"}

		_, _ = transport.Request(context.Background(), "kv", "Get", payload)

		requests := transport.Requests()
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}
		if requests[0].Service != "kv" {
			t.Errorf("expected service=kv, got %s", requests[0].Service)
		}
		if requests[0].Method != "Get" {
			t.Errorf("expected method=Get, got %s", requests[0].Method)
		}
	})

	t.Run("returns default response when no specific config", func(t *testing.T) {
		transport := NewTransport()
		transport.DefaultResponse = "default"

		resp, err := transport.Request(context.Background(), "unknown", "method", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "default" {
			t.Errorf("expected default response, got: %v", resp)
		}
	})

	t.Run("returns default error when no specific config", func(t *testing.T) {
		transport := NewTransport()
		transport.DefaultError = errors.NewInternal("test", "default error")

		_, err := transport.Request(context.Background(), "unknown", "method", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTransport_Request_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		transport := NewTransport()
		_ = transport.Close()

		_, err := transport.Request(context.Background(), "kv", "Get", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Code(err).IsServerError() && errors.Code(err) != errors.CodeConnection {
			t.Errorf("expected connection error, got: %v", err)
		}
	})

	t.Run("returns error when context canceled", func(t *testing.T) {
		transport := NewTransport()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := transport.Request(ctx, "kv", "Get", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when context deadline exceeded", func(t *testing.T) {
		transport := NewTransport()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		_, err := transport.Request(ctx, "kv", "Get", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTransport_RequestStream(t *testing.T) {
	t.Run("returns configured stream", func(t *testing.T) {
		transport := NewTransport()
		transport.SetStreamConfig("watch", "Watch", StreamConfig{
			Messages: []interface{}{"msg1", "msg2", "msg3"},
		})

		stream, err := transport.RequestStream(context.Background(), "watch", "Watch", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stream == nil {
			t.Fatal("expected stream, got nil")
		}

		// Receive messages
		msg1, err := stream.Recv()
		if err != nil {
			t.Fatalf("unexpected error on recv: %v", err)
		}
		if msg1 != "msg1" {
			t.Errorf("expected msg1, got %v", msg1)
		}

		msg2, err := stream.Recv()
		if err != nil {
			t.Fatalf("unexpected error on recv: %v", err)
		}
		if msg2 != "msg2" {
			t.Errorf("expected msg2, got %v", msg2)
		}
	})

	t.Run("captures stream requests", func(t *testing.T) {
		transport := NewTransport()

		_, _ = transport.RequestStream(context.Background(), "watch", "Watch", "payload")

		requests := transport.StreamRequests()
		if len(requests) != 1 {
			t.Fatalf("expected 1 stream request, got %d", len(requests))
		}
		if requests[0].Service != "watch" {
			t.Errorf("expected service=watch, got %s", requests[0].Service)
		}
	})
}

func TestTransport_RequestStream_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		transport := NewTransport()
		_ = transport.Close()

		_, err := transport.RequestStream(context.Background(), "watch", "Watch", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when context canceled", func(t *testing.T) {
		transport := NewTransport()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := transport.RequestStream(ctx, "watch", "Watch", nil)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTransport_Close(t *testing.T) {
	t.Run("closes successfully", func(t *testing.T) {
		transport := NewTransport()

		err := transport.Close()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !transport.IsClosed() {
			t.Error("expected transport to be closed")
		}
	})

	t.Run("returns error when already closed", func(t *testing.T) {
		transport := NewTransport()
		_ = transport.Close()

		err := transport.Close()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTransport_Healthy(t *testing.T) {
	t.Run("returns true when healthy", func(t *testing.T) {
		transport := NewTransport()

		if !transport.Healthy() {
			t.Error("expected transport to be healthy")
		}
	})

	t.Run("returns false when unhealthy", func(t *testing.T) {
		transport := NewTransport()
		transport.SetHealthy(false)

		if transport.Healthy() {
			t.Error("expected transport to be unhealthy")
		}
	})

	t.Run("returns false when closed", func(t *testing.T) {
		transport := NewTransport()
		_ = transport.Close()

		if transport.Healthy() {
			t.Error("expected closed transport to be unhealthy")
		}
	})
}

func TestTransport_Conn(t *testing.T) {
	t.Run("returns connection", func(t *testing.T) {
		transport := NewTransport()

		conn := transport.Conn()

		if conn == nil {
			t.Fatal("expected connection, got nil")
		}
	})

	t.Run("returns custom connection", func(t *testing.T) {
		transport := NewTransport()
		customConn := "custom"
		transport.SetConn(customConn)

		conn := transport.Conn()

		if conn != customConn {
			t.Errorf("expected custom connection, got %v", conn)
		}
	})
}

func TestTransport_LastRequest(t *testing.T) {
	t.Run("returns nil when no requests", func(t *testing.T) {
		transport := NewTransport()

		last := transport.LastRequest()

		if last != nil {
			t.Errorf("expected nil, got %v", last)
		}
	})

	t.Run("returns last request", func(t *testing.T) {
		transport := NewTransport()
		_, _ = transport.Request(context.Background(), "svc1", "method1", nil)
		_, _ = transport.Request(context.Background(), "svc2", "method2", nil)

		last := transport.LastRequest()

		if last == nil {
			t.Fatal("expected last request, got nil")
		}
		if last.Service != "svc2" {
			t.Errorf("expected service=svc2, got %s", last.Service)
		}
	})
}

func TestTransport_RequestCount(t *testing.T) {
	t.Run("returns zero when no requests", func(t *testing.T) {
		transport := NewTransport()

		count := transport.RequestCount()

		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})

	t.Run("returns correct count", func(t *testing.T) {
		transport := NewTransport()
		_, _ = transport.Request(context.Background(), "svc", "method", nil)
		_, _ = transport.Request(context.Background(), "svc", "method", nil)
		_, _ = transport.Request(context.Background(), "svc", "method", nil)

		count := transport.RequestCount()

		if count != 3 {
			t.Errorf("expected 3, got %d", count)
		}
	})
}

func TestTransport_Reset(t *testing.T) {
	t.Run("clears all state", func(t *testing.T) {
		transport := NewTransport()
		transport.SetResponse("svc", "method", "response", nil)
		_, _ = transport.Request(context.Background(), "svc", "method", nil)
		_, _ = transport.RequestStream(context.Background(), "stream", "method", nil)
		_ = transport.Close()

		transport.Reset()

		if transport.RequestCount() != 0 {
			t.Error("expected requests to be cleared")
		}
		if len(transport.StreamRequests()) != 0 {
			t.Error("expected stream requests to be cleared")
		}
		if transport.IsClosed() {
			t.Error("expected transport to not be closed after reset")
		}
		if !transport.Healthy() {
			t.Error("expected transport to be healthy after reset")
		}
	})
}

func TestStream_Send(t *testing.T) {
	t.Run("captures sent messages", func(t *testing.T) {
		stream := NewStream(StreamConfig{})

		err := stream.Send("message1")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sent := stream.SentMessages()
		if len(sent) != 1 {
			t.Fatalf("expected 1 sent message, got %d", len(sent))
		}
		if sent[0] != "message1" {
			t.Errorf("expected message1, got %v", sent[0])
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		expectedErr := errors.NewInternal("test", "send failed")
		stream := NewStream(StreamConfig{
			SendError: expectedErr,
		})

		err := stream.Send("message")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestStream_Send_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		stream := NewStream(StreamConfig{})
		_ = stream.Close()

		err := stream.Send("message")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when send closed", func(t *testing.T) {
		stream := NewStream(StreamConfig{})
		_ = stream.CloseSend()

		err := stream.Send("message")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStream_Recv(t *testing.T) {
	t.Run("returns configured messages", func(t *testing.T) {
		stream := NewStream(StreamConfig{
			Messages: []interface{}{"msg1", "msg2"},
		})

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

	t.Run("returns EOF when no more messages", func(t *testing.T) {
		stream := NewStream(StreamConfig{
			Messages: []interface{}{"msg1"},
		})

		_, _ = stream.Recv() // consume first message

		_, err := stream.Recv()

		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("returns configured error after messages", func(t *testing.T) {
		expectedErr := errors.NewInternal("test", "recv failed")
		stream := NewStream(StreamConfig{
			Messages:  []interface{}{"msg1"},
			RecvError: expectedErr,
		})

		_, _ = stream.Recv() // consume first message

		_, err := stream.Recv()

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestStream_Recv_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		stream := NewStream(StreamConfig{
			Messages: []interface{}{"msg1"},
		})
		_ = stream.Close()

		_, err := stream.Recv()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStream_CloseSend(t *testing.T) {
	t.Run("closes send side", func(t *testing.T) {
		stream := NewStream(StreamConfig{})

		err := stream.CloseSend()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !stream.IsSendClosed() {
			t.Error("expected send side to be closed")
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		expectedErr := errors.NewInternal("test", "close send failed")
		stream := NewStream(StreamConfig{
			CloseSendError: expectedErr,
		})

		err := stream.CloseSend()

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestStream_CloseSend_Error(t *testing.T) {
	t.Run("returns error when closed", func(t *testing.T) {
		stream := NewStream(StreamConfig{})
		_ = stream.Close()

		err := stream.CloseSend()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when already close sent", func(t *testing.T) {
		stream := NewStream(StreamConfig{})
		_ = stream.CloseSend()

		err := stream.CloseSend()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStream_Close(t *testing.T) {
	t.Run("closes stream", func(t *testing.T) {
		stream := NewStream(StreamConfig{})

		err := stream.Close()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !stream.IsClosed() {
			t.Error("expected stream to be closed")
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		expectedErr := errors.NewInternal("test", "close failed")
		stream := NewStream(StreamConfig{
			CloseError: expectedErr,
		})

		err := stream.Close()

		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestStream_Close_Error(t *testing.T) {
	t.Run("returns error when already closed", func(t *testing.T) {
		stream := NewStream(StreamConfig{})
		_ = stream.Close()

		err := stream.Close()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStream_StateQueries(t *testing.T) {
	t.Run("IsClosed returns correct state", func(t *testing.T) {
		stream := NewStream(StreamConfig{})

		if stream.IsClosed() {
			t.Error("expected stream to not be closed initially")
		}

		_ = stream.Close()

		if !stream.IsClosed() {
			t.Error("expected stream to be closed after Close()")
		}
	})

	t.Run("IsSendClosed returns correct state", func(t *testing.T) {
		stream := NewStream(StreamConfig{})

		if stream.IsSendClosed() {
			t.Error("expected send side to not be closed initially")
		}

		_ = stream.CloseSend()

		if !stream.IsSendClosed() {
			t.Error("expected send side to be closed after CloseSend()")
		}
	})
}
