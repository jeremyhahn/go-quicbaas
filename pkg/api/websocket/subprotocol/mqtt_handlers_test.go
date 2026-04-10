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
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mochi-mqtt/server/v2/packets"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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
	session := ws.NewSession("test", "mqtt", nil)
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

func TestMQTTHandler_HandleConnection_PublishQoS1(t *testing.T) {
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
