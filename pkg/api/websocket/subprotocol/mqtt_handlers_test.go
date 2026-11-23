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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mochi-mqtt/server/v2/packets"
)

// newTestMQTTState creates a connected MQTT client state for testing.
func newTestMQTTState() *mqttClientState {
	return &mqttClientState{
		clientID:        "test-client",
		protocolVersion: 4,
		keepalive:       60,
		cleanSession:    true,
		subscriptions:   make(map[string]byte),
	}
}

// extractPayload strips the fixed header and remaining length from an encoded MQTT packet,
// returning only the payload portion that handler methods expect.
func extractPayload(encoded []byte) []byte {
	fh := packets.FixedHeader{}
	fh.Decode(encoded[0])
	payloadBuf := bytes.NewBuffer(encoded[1:])
	packets.DecodeLength(payloadBuf)
	return payloadBuf.Bytes()
}

// buildMQTTSubscribePacket creates a raw SUBSCRIBE packet for sending over WebSocket.
func buildMQTTSubscribePacket(packetID uint16, filters []struct {
	topic string
	qos   byte
}) []byte {
	// SUBSCRIBE fixed header: type 8, QoS 1 reserved = 0x82
	// Variable header: packet ID (2 bytes)
	// Payload: topic filter length (2) + topic + requested QoS (1)
	payload := &bytes.Buffer{}
	payload.WriteByte(byte(packetID >> 8))
	payload.WriteByte(byte(packetID))
	for _, f := range filters {
		payload.WriteByte(byte(len(f.topic) >> 8))
		payload.WriteByte(byte(len(f.topic)))
		payload.WriteString(f.topic)
		payload.WriteByte(f.qos)
	}

	result := &bytes.Buffer{}
	result.WriteByte(0x82) // SUBSCRIBE with QoS 1
	// Write remaining length
	remaining := payload.Len()
	for remaining > 0 {
		encodedByte := byte(remaining % 128)
		remaining = remaining / 128
		if remaining > 0 {
			encodedByte |= 0x80
		}
		result.WriteByte(encodedByte)
	}
	result.Write(payload.Bytes())
	return result.Bytes()
}

func TestMQTTHandler_HandlePublish_QoS0(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  0,
		},
		ProtocolVersion: 4,
	}

	publishPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  0,
		},
		TopicName:       "test/topic",
		Payload:         []byte("hello"),
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	publishPk.PublishEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePublish(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePublish() error = %v", err)
	}
	if response != nil {
		t.Error("QoS 0 PUBLISH should have no response")
	}
}

func TestMQTTHandler_HandlePublish_QoS1(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  1,
		},
		ProtocolVersion: 4,
	}

	publishPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  1,
		},
		TopicName:       "test/topic",
		Payload:         []byte("hello"),
		PacketID:        42,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	publishPk.PublishEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePublish(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePublish() error = %v", err)
	}
	if response == nil {
		t.Fatal("QoS 1 PUBLISH should return PUBACK")
	}
	if response[0] != 0x40 {
		t.Errorf("Response type = 0x%02x, want 0x40 (PUBACK)", response[0])
	}
}

func TestMQTTHandler_HandlePublish_QoS2(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  2,
		},
		ProtocolVersion: 4,
	}

	publishPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  2,
		},
		TopicName:       "test/topic",
		Payload:         []byte("hello"),
		PacketID:        99,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	publishPk.PublishEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePublish(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePublish() error = %v", err)
	}
	if response == nil {
		t.Fatal("QoS 2 PUBLISH should return PUBREC")
	}
	if response[0] != 0x50 {
		t.Errorf("Response type = 0x%02x, want 0x50 (PUBREC)", response[0])
	}
}

func TestMQTTHandler_HandleSubscribe(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	handler.hub.RegisterClient(state.clientID, func(topic string, payload []byte, qos byte, retain bool) {})

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Subscribe,
		},
		ProtocolVersion: 4,
	}

	subscribePk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Subscribe,
		},
		PacketID:        100,
		ProtocolVersion: 4,
		Filters: packets.Subscriptions{
			{Filter: "test/topic1", Qos: 0},
			{Filter: "test/topic2", Qos: 1},
		},
	}
	buf := &bytes.Buffer{}
	subscribePk.SubscribeEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handleSubscribe(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handleSubscribe() error = %v", err)
	}
	if response == nil {
		t.Fatal("SUBSCRIBE should return SUBACK")
	}
	if response[0] != 0x90 {
		t.Errorf("Response type = 0x%02x, want 0x90 (SUBACK)", response[0])
	}

	state.mu.RLock()
	_, hasTopic1 := state.subscriptions["test/topic1"]
	_, hasTopic2 := state.subscriptions["test/topic2"]
	state.mu.RUnlock()

	if !hasTopic1 || !hasTopic2 {
		t.Error("Subscriptions not recorded in state")
	}
}

func TestMQTTHandler_HandleUnsubscribe(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	state.subscriptions["test/topic1"] = 0
	state.subscriptions["test/topic2"] = 1
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	handler.hub.RegisterClient(state.clientID, func(topic string, payload []byte, qos byte, retain bool) {})
	handler.hub.Subscribe(state.clientID, "test/topic1", 0)
	handler.hub.Subscribe(state.clientID, "test/topic2", 1)

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsubscribe,
		},
		ProtocolVersion: 4,
	}

	unsubPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsubscribe,
		},
		PacketID:        200,
		ProtocolVersion: 4,
		Filters: packets.Subscriptions{
			{Filter: "test/topic1"},
		},
	}
	buf := &bytes.Buffer{}
	unsubPk.UnsubscribeEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handleUnsubscribe(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handleUnsubscribe() error = %v", err)
	}
	if response == nil {
		t.Fatal("UNSUBSCRIBE should return UNSUBACK")
	}
	if response[0] != 0xB0 {
		t.Errorf("Response type = 0x%02x, want 0xB0 (UNSUBACK)", response[0])
	}

	state.mu.RLock()
	_, hasTopic1 := state.subscriptions["test/topic1"]
	state.mu.RUnlock()

	if hasTopic1 {
		t.Error("test/topic1 should be unsubscribed")
	}
}

func TestMQTTHandler_HandleUnsubscribe_NonExistent(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	handler.hub.RegisterClient(state.clientID, func(topic string, payload []byte, qos byte, retain bool) {})

	pk := &packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsubscribe,
		},
		ProtocolVersion: 4,
	}

	unsubPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsubscribe,
		},
		PacketID:        201,
		ProtocolVersion: 4,
		Filters: packets.Subscriptions{
			{Filter: "nonexistent/topic"},
		},
	}
	buf := &bytes.Buffer{}
	unsubPk.UnsubscribeEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handleUnsubscribe(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handleUnsubscribe() error = %v", err)
	}
	if response == nil {
		t.Fatal("UNSUBSCRIBE should return UNSUBACK even for non-existent")
	}
}

func TestMQTTHandler_HandlePuback(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Puback},
		ProtocolVersion: 4,
	}

	pubackPk := packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Puback},
		PacketID:        42,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	pubackPk.PubackEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePuback(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePuback() error = %v", err)
	}
	if response != nil {
		t.Error("PUBACK should have no response")
	}
}

func TestMQTTHandler_HandlePubrec(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubrec},
		ProtocolVersion: 4,
	}

	pubrecPk := packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubrec},
		PacketID:        55,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	pubrecPk.PubrecEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePubrec(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePubrec() error = %v", err)
	}
	if response == nil {
		t.Fatal("PUBREC should return PUBREL")
	}
	if response[0] != 0x62 {
		t.Errorf("Response type = 0x%02x, want 0x62 (PUBREL)", response[0])
	}
}

func TestMQTTHandler_HandlePubrel(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubrel, Qos: 1},
		ProtocolVersion: 4,
	}

	pubrelPk := packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubrel, Qos: 1},
		PacketID:        66,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	pubrelPk.PubrelEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePubrel(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePubrel() error = %v", err)
	}
	if response == nil {
		t.Fatal("PUBREL should return PUBCOMP")
	}
	if response[0] != 0x70 {
		t.Errorf("Response type = 0x%02x, want 0x70 (PUBCOMP)", response[0])
	}
}

func TestMQTTHandler_HandlePubcomp(t *testing.T) {
	handler := NewMQTTHandler(nil)
	state := newTestMQTTState()
	session := newGrantedSession("test", "mqtt", nil)
	deliveryChan := make(chan mqttDelivery, 10)
	defer close(deliveryChan)

	pk := &packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubcomp},
		ProtocolVersion: 4,
	}

	pubcompPk := packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Pubcomp},
		PacketID:        77,
		ProtocolVersion: 4,
	}
	buf := &bytes.Buffer{}
	pubcompPk.PubcompEncode(buf)
	payload := extractPayload(buf.Bytes())

	response, err := handler.handlePubcomp(context.Background(), payload, pk, session, state, deliveryChan)
	if err != nil {
		t.Fatalf("handlePubcomp() error = %v", err)
	}
	if response != nil {
		t.Error("PUBCOMP should have no response")
	}
}

func TestMQTTHandler_HandleConnection_SubscribePublish(t *testing.T) {
	handler := NewMQTTHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := newGrantedSession("test-session", "mqtt", conn)
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

	// Send CONNECT
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTConnectPacket("sub-pub-client", 4))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNACK
	_, connackData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}
	if connackData[0] != 0x20 {
		t.Errorf("Expected CONNACK (0x20), got 0x%02x", connackData[0])
	}

	// Send SUBSCRIBE using raw packet builder to ensure correct wire format
	subscribePacket := buildMQTTSubscribePacket(1, []struct {
		topic string
		qos   byte
	}{
		{"test/topic", 1},
	})

	err = conn.Write(ctx, websocket.MessageBinary, subscribePacket)
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	// Read SUBACK
	_, subackData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read SUBACK failed: %v", err)
	}
	if subackData[0] != 0x90 {
		t.Errorf("Expected SUBACK (0x90), got 0x%02x", subackData[0])
	}

	// Publish via hub to trigger message delivery
	handler.Hub().Publish("test/topic", []byte("hello world"), 1, false)

	// Read delivered PUBLISH message
	_, publishData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read PUBLISH delivery failed: %v", err)
	}
	if (publishData[0] >> 4) != packets.Publish {
		t.Errorf("Expected PUBLISH type, got %d", publishData[0]>>4)
	}

	// Send DISCONNECT
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTDisconnectPacket())
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	select {
	case err := <-handlerDone:
		if err != nil {
			t.Errorf("Handler returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete")
	}
}

// startMQTTConn serves an MQTT handler on hub and dials it. The returned
// channel delivers HandleConnection's result.
func startMQTTConn(t *testing.T, ctx context.Context, hub PubSubHub) (*websocket.Conn, <-chan error) {
	t.Helper()
	handler := NewMQTTHandler(hub)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := newGrantedSession("test-session", "mqtt", conn)
		handlerDone <- handler.HandleConnection(session.Context(), conn, session)
	}))
	t.Cleanup(server.Close)

	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn, handlerDone
}

// mqttWrite sends one binary WebSocket message.
func mqttWrite(t *testing.T, ctx context.Context, conn *websocket.Conn, data []byte) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// mqttExpect reads one WebSocket message and checks its first byte.
func mqttExpect(t *testing.T, ctx context.Context, conn *websocket.Conn, first byte, what string) {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read %s failed: %v", what, err)
	}
	if len(data) == 0 || data[0] != first {
		t.Fatalf("Expected %s (0x%02x), got % x", what, first, data)
	}
}

// buildMQTTPublishQoS0 builds a QoS 0 PUBLISH packet with a one-byte
// Remaining Length.
func buildMQTTPublishQoS0(topic string, payload []byte) []byte {
	body := []byte{byte(len(topic) >> 8), byte(len(topic))}
	body = append(body, topic...)
	body = append(body, payload...)
	return append([]byte{0x30, byte(len(body))}, body...)
}

// TestMQTTHandler_HandleConnection_CoalescedPackets: every packet of a
// WebSocket message carrying several MQTT packets is handled, in order.
func TestMQTTHandler_HandleConnection_CoalescedPackets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, handlerDone := startMQTTConn(t, ctx, NewInMemoryPubSubHub())

	mqttWrite(t, ctx, conn, buildMQTTConnectPacket("coalesced-client", 4))
	mqttExpect(t, ctx, conn, 0x20, "CONNACK")

	mqttWrite(t, ctx, conn, []byte{0xC0, 0x00, 0xC0, 0x00})
	mqttExpect(t, ctx, conn, 0xD0, "first PINGRESP")
	mqttExpect(t, ctx, conn, 0xD0, "second PINGRESP")

	mqttWrite(t, ctx, conn, append(buildMQTTPingreqPacket(), buildMQTTDisconnectPacket()...))
	mqttExpect(t, ctx, conn, 0xD0, "PINGRESP before DISCONNECT")
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Errorf("Handler returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete")
	}
}

// TestMQTTHandler_HandleConnection_PacketSplitAcrossMessages: a PUBLISH split
// across two WebSocket messages reaches a hub subscriber once, whole.
func TestMQTTHandler_HandleConnection_PacketSplitAcrossMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hub := NewInMemoryPubSubHub()
	got := subscribeTestHub(t, hub, "split-subscriber", "split/topic")
	conn, _ := startMQTTConn(t, ctx, hub)

	mqttWrite(t, ctx, conn, buildMQTTConnectPacket("split-client", 4))
	mqttExpect(t, ctx, conn, 0x20, "CONNACK")

	payload := []byte("the whole payload")
	pkt := buildMQTTPublishQoS0("split/topic", payload)
	mqttWrite(t, ctx, conn, pkt[:6])
	mqttWrite(t, ctx, conn, pkt[6:])

	select {
	case p := <-got:
		if !bytes.Equal(p, payload) {
			t.Fatalf("payload = %q, want %q", p, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("split PUBLISH did not reach the subscriber")
	}

	// A PINGREQ round trip after the publish proves the handler has
	// processed everything; the subscriber must not see a second delivery.
	mqttWrite(t, ctx, conn, buildMQTTPingreqPacket())
	mqttExpect(t, ctx, conn, 0xD0, "PINGRESP")
	select {
	case p := <-got:
		t.Fatalf("unexpected second delivery %q", p)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestMQTTHandler_HandleConnection_OversizePacket: a fixed header declaring
// more than the packet size bound ends the connection with a protocol error.
func TestMQTTHandler_HandleConnection_OversizePacket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, handlerDone := startMQTTConn(t, ctx, NewInMemoryPubSubHub())

	mqttWrite(t, ctx, conn, buildMQTTConnectPacket("oversize-client", 4))
	mqttExpect(t, ctx, conn, 0x20, "CONNACK")

	// A PUBLISH declaring a 2 MiB Remaining Length.
	mqttWrite(t, ctx, conn, []byte{0x30, 0x80, 0x80, 0x80, 0x01})

	select {
	case err := <-handlerDone:
		var perr *ErrMQTTProtocolError
		if !errors.As(err, &perr) {
			t.Fatalf("Handler returned %v, want *ErrMQTTProtocolError", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not complete")
	}
}

// TestMQTTPacketLength covers the Remaining Length decoding.
func TestMQTTPacketLength(t *testing.T) {
	tests := []struct {
		name    string
		buf     []byte
		wantN   int
		wantOK  bool
		wantErr bool
	}{
		{"empty", nil, 0, false, false},
		{"header byte only", []byte{0xC0}, 0, false, false},
		{"one-byte length", []byte{0xC0, 0x00}, 2, true, false},
		{"one-byte length, payload pending", []byte{0x30, 0x05, 0x00}, 7, true, false},
		{"two-byte length 321", []byte{0x30, 0xC1, 0x02}, 1 + 2 + 321, true, false},
		{"incomplete length", []byte{0x30, 0xC1}, 0, false, false},
		{"five length bytes", []byte{0x30, 0x80, 0x80, 0x80, 0x80, 0x01}, 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok, err := mqttPacketLength(tt.buf)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if n != tt.wantN || ok != tt.wantOK {
				t.Fatalf("mqttPacketLength = (%d, %v), want (%d, %v)", n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

func TestMQTTHandler_HandleConnection_PublishQoS1(t *testing.T) {
	handler := NewMQTTHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := newGrantedSession("test-session", "mqtt", conn)
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

	// CONNECT
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTConnectPacket("pub-qos1-client", 4))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx) // CONNACK
	if err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}

	// Build and send QoS 1 PUBLISH
	publishPk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Publish,
			Qos:  1,
		},
		TopicName:       "pub/test",
		Payload:         []byte("qos1 message"),
		PacketID:        1,
		ProtocolVersion: 4,
	}
	pubBuf := &bytes.Buffer{}
	publishPk.PublishEncode(pubBuf)

	err = conn.Write(ctx, websocket.MessageBinary, pubBuf.Bytes())
	if err != nil {
		t.Fatalf("Write PUBLISH failed: %v", err)
	}

	// Read PUBACK
	_, pubackData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read PUBACK failed: %v", err)
	}
	if pubackData[0] != 0x40 {
		t.Errorf("Expected PUBACK (0x40), got 0x%02x", pubackData[0])
	}
}

func TestMQTTHandler_DeliverMessages(t *testing.T) {
	handler := NewMQTTHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := newGrantedSession("test-session", "mqtt", conn)
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

	// CONNECT
	err = conn.Write(ctx, websocket.MessageBinary, buildMQTTConnectPacket("deliver-client", 4))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx) // CONNACK
	if err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}

	// SUBSCRIBE with raw packet
	subscribePacket := buildMQTTSubscribePacket(1, []struct {
		topic string
		qos   byte
	}{
		{"deliver/test", 0},
	})
	err = conn.Write(ctx, websocket.MessageBinary, subscribePacket)
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	_, _, err = conn.Read(ctx) // SUBACK
	if err != nil {
		t.Fatalf("Read SUBACK failed: %v", err)
	}

	// Publish multiple messages via hub
	for i := 0; i < 3; i++ {
		handler.Hub().Publish("deliver/test", []byte("msg"), 0, false)
	}

	// Read delivered messages
	for i := 0; i < 3; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read delivery %d failed: %v", i, err)
		}
		if (data[0] >> 4) != packets.Publish {
			t.Errorf("Delivery %d: expected PUBLISH type, got %d", i, data[0]>>4)
		}
	}
}
