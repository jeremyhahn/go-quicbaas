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

//go:build quicbaas_mqtt

package subprotocol

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mochi-mqtt/server/v2/packets"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

func TestNewMQTTHandler(t *testing.T) {
	handler := NewMQTTHandler(nil)

	if handler == nil {
		t.Fatal("NewMQTTHandler(nil) returned nil")
	}

	if handler.Hub() == nil {
		t.Error("NewMQTTHandler(nil) should create default hub")
	}

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestNewMQTTHandler_WithHub(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewMQTTHandler(hub)

	if handler.Hub() != hub {
		t.Error("NewMQTTHandler should use provided hub")
	}
}

func TestMQTTHandler_Name(t *testing.T) {
	handler := NewMQTTHandler(nil)

	if handler.Name() != "mqtt" {
		t.Errorf("Name() = %q, want mqtt", handler.Name())
	}
}

func TestMQTTHandler_MessagesReceived(t *testing.T) {
	handler := NewMQTTHandler(nil)

	if handler.MessagesReceived() != 0 {
		t.Errorf("Initial MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	handler.messagesReceived.Add(5)

	if handler.MessagesReceived() != 5 {
		t.Errorf("MessagesReceived() = %d, want 5", handler.MessagesReceived())
	}
}

func TestMQTTHandler_MessagesSent(t *testing.T) {
	handler := NewMQTTHandler(nil)

	if handler.MessagesSent() != 0 {
		t.Errorf("Initial MessagesSent() = %d, want 0", handler.MessagesSent())
	}

	handler.messagesSent.Add(3)

	if handler.MessagesSent() != 3 {
		t.Errorf("MessagesSent() = %d, want 3", handler.MessagesSent())
	}
}

func TestMQTTHandler_ResetCounters(t *testing.T) {
	handler := NewMQTTHandler(nil)

	handler.messagesReceived.Add(10)
	handler.messagesSent.Add(8)

	if handler.MessagesReceived() != 10 {
		t.Errorf("MessagesReceived() = %d, want 10", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 8 {
		t.Errorf("MessagesSent() = %d, want 8", handler.MessagesSent())
	}

	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestMQTTHandler_ConcurrentCounterAccess(t *testing.T) {
	handler := NewMQTTHandler(nil)

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			handler.messagesReceived.Add(1)
		}()
		go func() {
			defer wg.Done()
			handler.messagesSent.Add(1)
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = handler.MessagesReceived()
		}()
		go func() {
			defer wg.Done()
			_ = handler.MessagesSent()
		}()
	}

	wg.Wait()

	if handler.MessagesReceived() != 100 {
		t.Errorf("MessagesReceived() = %d, want 100", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 100 {
		t.Errorf("MessagesSent() = %d, want 100", handler.MessagesSent())
	}
}

func TestMQTTHandler_ClientCount(t *testing.T) {
	handler := NewMQTTHandler(nil)

	if handler.ClientCount() != 0 {
		t.Errorf("Initial ClientCount() = %d, want 0", handler.ClientCount())
	}

	// Add client state directly
	handler.clientsMu.Lock()
	handler.clients["test-client"] = &mqttClientState{
		clientID:      "test-client",
		subscriptions: make(map[string]byte),
	}
	handler.clientsMu.Unlock()

	if handler.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", handler.ClientCount())
	}
}

func TestMQTTHandler_BuildConnack(t *testing.T) {
	handler := NewMQTTHandler(nil)

	// Test MQTT 3.1.1 (version 4)
	connack := handler.buildConnack(4, false, 0)
	if len(connack) < 4 {
		t.Fatalf("CONNACK too short: %d bytes", len(connack))
	}

	// Fixed header byte should be 0x20 (CONNACK type)
	if connack[0] != 0x20 {
		t.Errorf("CONNACK type byte = 0x%02x, want 0x20", connack[0])
	}

	// Test MQTT 5.0 (version 5)
	connack5 := handler.buildConnack(5, true, 0)
	if len(connack5) < 4 {
		t.Fatalf("MQTT5 CONNACK too short: %d bytes", len(connack5))
	}
}

func TestMQTTHandler_BuildPingresp(t *testing.T) {
	handler := NewMQTTHandler(nil)

	pingresp := handler.buildPingresp()

	// PINGRESP is exactly 2 bytes: 0xD0 0x00
	if len(pingresp) != 2 {
		t.Fatalf("PINGRESP length = %d, want 2", len(pingresp))
	}

	if pingresp[0] != 0xD0 {
		t.Errorf("PINGRESP type byte = 0x%02x, want 0xD0", pingresp[0])
	}

	if pingresp[1] != 0x00 {
		t.Errorf("PINGRESP remaining length = 0x%02x, want 0x00", pingresp[1])
	}
}

func TestMQTTHandler_BuildPuback(t *testing.T) {
	handler := NewMQTTHandler(nil)

	puback := handler.buildPuback(4, 1234)

	// PUBACK fixed header byte should be 0x40
	if puback[0] != 0x40 {
		t.Errorf("PUBACK type byte = 0x%02x, want 0x40", puback[0])
	}

	// Check packet ID (bytes 2-3)
	if len(puback) < 4 {
		t.Fatalf("PUBACK too short: %d bytes", len(puback))
	}

	packetID := uint16(puback[2])<<8 | uint16(puback[3])
	if packetID != 1234 {
		t.Errorf("PUBACK packet ID = %d, want 1234", packetID)
	}
}

func TestMQTTHandler_BuildSuback(t *testing.T) {
	handler := NewMQTTHandler(nil)

	reasonCodes := []byte{0x00, 0x01, 0x02}
	suback := handler.buildSuback(4, 5678, reasonCodes)

	// SUBACK fixed header byte should be 0x90
	if suback[0] != 0x90 {
		t.Errorf("SUBACK type byte = 0x%02x, want 0x90", suback[0])
	}

	// MQTT 3.1.1 SUBACK format:
	// - Fixed header: 1 byte (0x90)
	// - Remaining length: 1 byte (variable length encoding)
	// - Packet ID: 2 bytes
	// - Reason codes: N bytes
	// Minimum: 1 + 1 + 2 + 3 = 7 bytes
	expectedMinLen := 1 + 1 + 2 + len(reasonCodes)
	if len(suback) < expectedMinLen {
		t.Fatalf("SUBACK too short: %d bytes, want at least %d", len(suback), expectedMinLen)
	}
}

func TestErrMQTTProtocolError(t *testing.T) {
	err := &ErrMQTTProtocolError{Message: "test error"}

	expected := "mqtt protocol error: test error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestMQTTHandler_HandlePacket_InvalidPacket(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := &mqttClientState{
		subscriptions: make(map[string]byte),
	}
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	session := ws.NewSession("test", "mqtt", nil)

	// Test empty packet
	_, err := handler.handlePacket(context.Background(), []byte{}, session, state, deliveryChan)
	if err == nil {
		t.Error("Expected error for empty packet")
	}

	// Test packet with only 1 byte
	_, err = handler.handlePacket(context.Background(), []byte{0x10}, session, state, deliveryChan)
	if err == nil {
		t.Error("Expected error for 1-byte packet")
	}
}

func TestMQTTHandler_HandlePacket_UnsupportedType(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := &mqttClientState{
		subscriptions: make(map[string]byte),
	}
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	session := ws.NewSession("test", "mqtt", nil)

	// Reserved packet type (0x00)
	_, err := handler.handlePacket(context.Background(), []byte{0x00, 0x00}, session, state, deliveryChan)
	if err == nil {
		t.Error("Expected error for reserved packet type")
	}
}

// buildMQTTConnectPacket creates a valid MQTT CONNECT packet for testing.
func buildMQTTConnectPacket(clientID string, protocolVersion byte) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Connect,
		},
		ProtocolVersion: protocolVersion,
		Connect: packets.ConnectParams{
			ProtocolName:     []byte("MQTT"),
			ClientIdentifier: clientID,
			Keepalive:        60,
			Clean:            true,
		},
	}

	buf := &bytes.Buffer{}
	pk.ConnectEncode(buf)
	return buf.Bytes()
}

// buildMQTTPingreqPacket creates a PINGREQ packet.
func buildMQTTPingreqPacket() []byte {
	return []byte{0xC0, 0x00}
}

// buildMQTTDisconnectPacket creates a DISCONNECT packet.
func buildMQTTDisconnectPacket() []byte {
	return []byte{0xE0, 0x00}
}

func TestMQTTHandler_HandleConnection_ConnectDisconnect(t *testing.T) {
	handler := NewMQTTHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}

		session := ws.NewSession("test-session", "mqtt", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerDone <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send CONNECT packet
	connectPacket := buildMQTTConnectPacket("test-client", 4)
	err = conn.Write(ctx, websocket.MessageBinary, connectPacket)
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNACK
	_, connackData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}

	// Verify CONNACK type
	if len(connackData) < 1 || connackData[0] != 0x20 {
		t.Errorf("Expected CONNACK (0x20), got 0x%02x", connackData[0])
	}

	// Send DISCONNECT
	disconnectPacket := buildMQTTDisconnectPacket()
	err = conn.Write(ctx, websocket.MessageBinary, disconnectPacket)
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	// Wait for handler to complete
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Errorf("Handler returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete after disconnect")
	}

	// Verify message counters
	if handler.MessagesReceived() != 2 {
		t.Errorf("MessagesReceived() = %d, want 2", handler.MessagesReceived())
	}

	// CONNACK was sent
	if handler.MessagesSent() < 1 {
		t.Errorf("MessagesSent() = %d, want >= 1", handler.MessagesSent())
	}
}

func TestMQTTHandler_HandleConnection_Pingreq(t *testing.T) {
	handler := NewMQTTHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "mqtt", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send CONNECT
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTConnectPacket("test-client", 4))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNACK
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}

	// Send PINGREQ
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTPingreqPacket())
	if err != nil {
		t.Fatalf("Write PINGREQ failed: %v", err)
	}

	// Read PINGRESP
	_, pingrespData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read PINGRESP failed: %v", err)
	}

	// Verify PINGRESP
	if len(pingrespData) < 2 || pingrespData[0] != 0xD0 {
		t.Errorf("Expected PINGRESP (0xD0), got 0x%02x", pingrespData[0])
	}
}

func TestMQTTHandler_HandleConnection_ContextCancel(t *testing.T) {
	handler := NewMQTTHandler(nil)
	handlerDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "mqtt", conn)

		go func() {
			_ = handler.HandleConnection(session.Context(), conn, session)
			close(handlerDone)
		}()

		// Close session after brief delay
		time.Sleep(50 * time.Millisecond)
		session.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for handler to complete
	select {
	case <-handlerDone:
		// Handler completed
	case <-time.After(time.Second):
		t.Error("Handler did not complete after context cancel")
	}
}

func TestMQTTHandler_HandleConnection_NormalClosure(t *testing.T) {
	handler := NewMQTTHandler(nil)
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "mqtt", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerErr <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Close with normal closure
	conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for handler to complete
	select {
	case err := <-handlerErr:
		if err != nil {
			t.Errorf("Handler should return nil on normal closure, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Handler did not complete after normal closure")
	}
}

func TestMQTTHandler_ConcurrentClients(t *testing.T) {
	handler := NewMQTTHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("session-"+r.RemoteAddr, "mqtt", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	var wg sync.WaitGroup
	numClients := 5

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, resp, err := websocket.Dial(ctx, wsURL, nil)
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if err != nil {
				t.Errorf("Client %d dial failed: %v", idx, err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "test complete")

			// Send CONNECT
			clientID := "client-" + string(rune('A'+idx))
			connectPacket := buildMQTTConnectPacket(clientID, 4)
			err = conn.Write(ctx, websocket.MessageBinary, connectPacket)
			if err != nil {
				t.Errorf("Client %d write CONNECT failed: %v", idx, err)
				return
			}

			// Read CONNACK
			_, _, err = conn.Read(ctx)
			if err != nil {
				t.Errorf("Client %d read CONNACK failed: %v", idx, err)
				return
			}

			// Send PINGREQ
			err = conn.Write(ctx, websocket.MessageBinary, buildMQTTPingreqPacket())
			if err != nil {
				t.Errorf("Client %d write PINGREQ failed: %v", idx, err)
				return
			}

			// Read PINGRESP
			_, _, err = conn.Read(ctx)
			if err != nil {
				t.Errorf("Client %d read PINGRESP failed: %v", idx, err)
				return
			}
		}(i)
	}

	wg.Wait()

	// Each client sends CONNECT and PINGREQ
	expectedReceived := uint64(numClients * 2)
	if handler.MessagesReceived() != expectedReceived {
		t.Errorf("MessagesReceived() = %d, want %d", handler.MessagesReceived(), expectedReceived)
	}

	// Each client receives CONNACK and PINGRESP
	expectedSent := uint64(numClients * 2)
	if handler.MessagesSent() != expectedSent {
		t.Errorf("MessagesSent() = %d, want %d", handler.MessagesSent(), expectedSent)
	}
}

func TestMQTTHandler_ResetCountersMultiple(t *testing.T) {
	handler := NewMQTTHandler(nil)

	for i := 0; i < 5; i++ {
		handler.messagesReceived.Add(5)
		handler.messagesSent.Add(5)
		handler.ResetCounters()

		if handler.MessagesReceived() != 0 {
			t.Errorf("Iteration %d: MessagesReceived() = %d, want 0", i, handler.MessagesReceived())
		}

		if handler.MessagesSent() != 0 {
			t.Errorf("Iteration %d: MessagesSent() = %d, want 0", i, handler.MessagesSent())
		}
	}
}

func TestInMemoryPubSubHub_PublishQoSDowngrade(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	var receivedQos atomic.Uint32

	hub.RegisterClient("client1", func(topic string, payload []byte, qos byte, retain bool) {
		receivedQos.Store(uint32(qos))
	})

	// Subscribe with QoS 0
	hub.Subscribe("client1", "test/topic", 0)

	// Publish with QoS 2 - should be downgraded to 0
	hub.Publish("test/topic", []byte("test"), 2, false)

	time.Sleep(10 * time.Millisecond)

	if receivedQos.Load() != 0 {
		t.Errorf("Received QoS = %d, want 0 (downgraded)", receivedQos.Load())
	}
}

func TestInMemoryPubSubHub_UnsubscribeNonExistent(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	// Should not error
	err := hub.Unsubscribe("client1", "nonexistent/topic")
	if err != nil {
		t.Errorf("Unsubscribe should not error for non-existent: %v", err)
	}
}

func TestInMemoryPubSubHub_WildcardSubscription(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	var received atomic.Int32
	var mu sync.Mutex
	topics := make([]string, 0)

	hub.RegisterClient("client1", func(topic string, payload []byte, qos byte, retain bool) {
		received.Add(1)
		mu.Lock()
		topics = append(topics, topic)
		mu.Unlock()
	})

	hub.Subscribe("client1", "test/#", 1)

	// Publish to multiple matching topics
	hub.Publish("test/a", []byte("1"), 1, false)
	hub.Publish("test/b", []byte("2"), 1, false)
	hub.Publish("test/c/d", []byte("3"), 1, false)
	hub.Publish("other/topic", []byte("4"), 1, false) // Should not match

	time.Sleep(20 * time.Millisecond)

	count := received.Load()
	if count != 3 {
		mu.Lock()
		t.Errorf("Received %d messages (topics: %v), want 3", count, topics)
		mu.Unlock()
	}
}

func TestMQTTHandler_BuildPubrec(t *testing.T) {
	handler := NewMQTTHandler(nil)

	pubrec := handler.buildPubrec(4, 1234)

	// PUBREC fixed header byte should be 0x50
	if pubrec[0] != 0x50 {
		t.Errorf("PUBREC type byte = 0x%02x, want 0x50", pubrec[0])
	}

	// Check packet ID (bytes 2-3)
	if len(pubrec) < 4 {
		t.Fatalf("PUBREC too short: %d bytes", len(pubrec))
	}

	packetID := uint16(pubrec[2])<<8 | uint16(pubrec[3])
	if packetID != 1234 {
		t.Errorf("PUBREC packet ID = %d, want 1234", packetID)
	}
}

func TestMQTTHandler_BuildPubrel(t *testing.T) {
	handler := NewMQTTHandler(nil)

	pubrel := handler.buildPubrel(4, 5678)

	// PUBREL fixed header byte should be 0x62 (type 6, QoS 1)
	if pubrel[0] != 0x62 {
		t.Errorf("PUBREL type byte = 0x%02x, want 0x62", pubrel[0])
	}

	// Check packet ID (bytes 2-3)
	if len(pubrel) < 4 {
		t.Fatalf("PUBREL too short: %d bytes", len(pubrel))
	}

	packetID := uint16(pubrel[2])<<8 | uint16(pubrel[3])
	if packetID != 5678 {
		t.Errorf("PUBREL packet ID = %d, want 5678", packetID)
	}
}

func TestMQTTHandler_BuildPubcomp(t *testing.T) {
	handler := NewMQTTHandler(nil)

	pubcomp := handler.buildPubcomp(4, 9012)

	// PUBCOMP fixed header byte should be 0x70
	if pubcomp[0] != 0x70 {
		t.Errorf("PUBCOMP type byte = 0x%02x, want 0x70", pubcomp[0])
	}

	// Check packet ID (bytes 2-3)
	if len(pubcomp) < 4 {
		t.Fatalf("PUBCOMP too short: %d bytes", len(pubcomp))
	}

	packetID := uint16(pubcomp[2])<<8 | uint16(pubcomp[3])
	if packetID != 9012 {
		t.Errorf("PUBCOMP packet ID = %d, want 9012", packetID)
	}
}

func TestMQTTHandler_BuildUnsuback(t *testing.T) {
	handler := NewMQTTHandler(nil)

	reasonCodes := []byte{0x00, 0x11} // Success, No subscription existed
	unsuback := handler.buildUnsuback(4, 1111, reasonCodes)

	// UNSUBACK fixed header byte should be 0xB0
	if unsuback[0] != 0xB0 {
		t.Errorf("UNSUBACK type byte = 0x%02x, want 0xB0", unsuback[0])
	}
}

func TestMQTTHandler_Hub(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewMQTTHandler(hub)

	if handler.Hub() != hub {
		t.Error("Hub() should return the configured hub")
	}
}

func TestErrMQTTDisconnect(t *testing.T) {
	expected := "mqtt: client disconnected"
	if ErrMQTTDisconnect.Error() != expected {
		t.Errorf("ErrMQTTDisconnect.Error() = %q, want %q", ErrMQTTDisconnect.Error(), expected)
	}
}
