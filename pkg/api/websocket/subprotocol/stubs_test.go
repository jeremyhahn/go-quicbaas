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

//go:build !quicbaas_amqp && !quicbaas_mqtt && !quicbaas_stomp

package subprotocol

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// AMQP Stub Tests

func TestAMQPStub_NewAMQPHandler(t *testing.T) {
	handler := NewAMQPHandler(nil)
	if handler == nil {
		t.Fatal("NewAMQPHandler() returned nil")
	}
}

func TestAMQPStub_Name(t *testing.T) {
	handler := NewAMQPHandler(nil)
	if handler.Name() != "amqp" {
		t.Errorf("Name() = %q, want %q", handler.Name(), "amqp")
	}
}

func TestAMQPStub_HandleConnection_ReturnsNotCompiledError(t *testing.T) {
	handler := NewAMQPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		session := ws.NewSession("test-amqp-stub", "amqp", conn)
		err = handler.HandleConnection(session.Context(), conn, session)

		if !errors.Is(err, ErrAMQPNotCompiled) {
			t.Errorf("HandleConnection() error = %v, want ErrAMQPNotCompiled", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
}

func TestAMQPStub_MessagesReceived(t *testing.T) {
	handler := NewAMQPHandler(nil)
	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}
}

func TestAMQPStub_MessagesSent(t *testing.T) {
	handler := NewAMQPHandler(nil)
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestAMQPStub_ResetCounters(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestAMQPStub_ClientCount(t *testing.T) {
	handler := NewAMQPHandler(nil)
	if handler.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", handler.ClientCount())
	}
}

// MQTT Stub Tests

func TestMQTTStub_NewMQTTHandler(t *testing.T) {
	handler := NewMQTTHandler(nil)
	if handler == nil {
		t.Fatal("NewMQTTHandler() returned nil")
	}
}

func TestMQTTStub_Name(t *testing.T) {
	handler := NewMQTTHandler(nil)
	if handler.Name() != "mqtt" {
		t.Errorf("Name() = %q, want %q", handler.Name(), "mqtt")
	}
}

func TestMQTTStub_HandleConnection_ReturnsNotCompiledError(t *testing.T) {
	handler := NewMQTTHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		session := ws.NewSession("test-mqtt-stub", "mqtt", conn)
		err = handler.HandleConnection(session.Context(), conn, session)

		if !errors.Is(err, ErrMQTTNotCompiled) {
			t.Errorf("HandleConnection() error = %v, want ErrMQTTNotCompiled", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
}

func TestMQTTStub_MessagesReceived(t *testing.T) {
	handler := NewMQTTHandler(nil)
	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}
}

func TestMQTTStub_MessagesSent(t *testing.T) {
	handler := NewMQTTHandler(nil)
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestMQTTStub_ResetCounters(t *testing.T) {
	handler := NewMQTTHandler(nil)
	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestMQTTStub_ClientCount(t *testing.T) {
	handler := NewMQTTHandler(nil)
	if handler.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", handler.ClientCount())
	}
}

// STOMP Stub Tests

func TestSTOMPStub_NewSTOMPHandler(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	if handler == nil {
		t.Fatal("NewSTOMPHandler() returned nil")
	}
}

func TestSTOMPStub_Name(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	if handler.Name() != "stomp" {
		t.Errorf("Name() = %q, want %q", handler.Name(), "stomp")
	}
}

func TestSTOMPStub_HandleConnection_ReturnsNotCompiledError(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		session := ws.NewSession("test-stomp-stub", "stomp", conn)
		err = handler.HandleConnection(session.Context(), conn, session)

		if !errors.Is(err, ErrSTOMPNotCompiled) {
			t.Errorf("HandleConnection() error = %v, want ErrSTOMPNotCompiled", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
}

func TestSTOMPStub_MessagesReceived(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}
}

func TestSTOMPStub_MessagesSent(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestSTOMPStub_ResetCounters(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}
	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestSTOMPStub_ClientCount(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	if handler.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", handler.ClientCount())
	}
}

// Error sentinel tests

func TestErrAMQPNotCompiled_Message(t *testing.T) {
	expected := "amqp subprotocol not compiled: build with -tags quicbaas_amqp"
	if ErrAMQPNotCompiled.Error() != expected {
		t.Errorf("ErrAMQPNotCompiled = %q, want %q", ErrAMQPNotCompiled.Error(), expected)
	}
}

func TestErrMQTTNotCompiled_Message(t *testing.T) {
	expected := "mqtt subprotocol not compiled: build with -tags quicbaas_mqtt"
	if ErrMQTTNotCompiled.Error() != expected {
		t.Errorf("ErrMQTTNotCompiled = %q, want %q", ErrMQTTNotCompiled.Error(), expected)
	}
}

func TestErrSTOMPNotCompiled_Message(t *testing.T) {
	expected := "stomp subprotocol not compiled: build with -tags quicbaas_stomp"
	if ErrSTOMPNotCompiled.Error() != expected {
		t.Errorf("ErrSTOMPNotCompiled = %q, want %q", ErrSTOMPNotCompiled.Error(), expected)
	}
}
