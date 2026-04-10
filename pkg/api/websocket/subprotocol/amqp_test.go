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

//go:build quicbaas_amqp

package subprotocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// waitForCounter polls a counter function until it reaches or exceeds the expected value.
// It returns true if the condition is met within the timeout, false otherwise.
func waitForCounter(getCount func() uint64, expected uint64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if getCount() >= expected {
			return true
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

func TestNewAMQPHandler(t *testing.T) {
	handler := NewAMQPHandler(nil)

	if handler == nil {
		t.Fatal("NewAMQPHandler(nil) returned nil")
	}

	if handler.Hub() == nil {
		t.Error("NewAMQPHandler(nil) should create default hub")
	}

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestNewAMQPHandler_WithHub(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewAMQPHandler(hub)

	if handler.Hub() != hub {
		t.Error("NewAMQPHandler should use provided hub")
	}
}

func TestAMQPHandler_Name(t *testing.T) {
	handler := NewAMQPHandler(nil)

	if handler.Name() != "amqp" {
		t.Errorf("Name() = %q, want amqp", handler.Name())
	}
}

func TestAMQPHandler_MessagesReceived(t *testing.T) {
	handler := NewAMQPHandler(nil)

	if handler.MessagesReceived() != 0 {
		t.Errorf("Initial MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	handler.messagesReceived.Add(5)

	if handler.MessagesReceived() != 5 {
		t.Errorf("MessagesReceived() = %d, want 5", handler.MessagesReceived())
	}
}

func TestAMQPHandler_MessagesSent(t *testing.T) {
	handler := NewAMQPHandler(nil)

	if handler.MessagesSent() != 0 {
		t.Errorf("Initial MessagesSent() = %d, want 0", handler.MessagesSent())
	}

	handler.messagesSent.Add(3)

	if handler.MessagesSent() != 3 {
		t.Errorf("MessagesSent() = %d, want 3", handler.MessagesSent())
	}
}

func TestAMQPHandler_ResetCounters(t *testing.T) {
	handler := NewAMQPHandler(nil)

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

func TestAMQPHandler_ConcurrentCounterAccess(t *testing.T) {
	handler := NewAMQPHandler(nil)

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

func TestAMQPHandler_ClientCount(t *testing.T) {
	handler := NewAMQPHandler(nil)

	if handler.ClientCount() != 0 {
		t.Errorf("Initial ClientCount() = %d, want 0", handler.ClientCount())
	}

	// Add client state directly
	handler.clientsMu.Lock()
	handler.clients["test-client"] = &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	handler.clientsMu.Unlock()

	if handler.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", handler.ClientCount())
	}
}

func TestAMQPHandler_Hub(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewAMQPHandler(hub)

	if handler.Hub() != hub {
		t.Error("Hub() should return the configured hub")
	}
}

func TestAMQPHandler_ValidateProtocolHeader(t *testing.T) {
	handler := NewAMQPHandler(nil)

	tests := []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"valid header", []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}, true},
		{"too short", []byte{'A', 'M', 'Q', 'P'}, false},
		{"invalid protocol", []byte{'H', 'T', 'T', 'P', 0, 0, 9, 1}, false},
		{"wrong version", []byte{'A', 'M', 'Q', 'P', 0, 1, 0, 0}, false},
		{"empty", []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handler.validateProtocolHeader(tt.data); got != tt.valid {
				t.Errorf("validateProtocolHeader() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestAMQPHandler_BuildProtocolHeader(t *testing.T) {
	handler := NewAMQPHandler(nil)
	header := handler.buildProtocolHeader()

	expected := []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}
	if !bytes.Equal(header, expected) {
		t.Errorf("buildProtocolHeader() = %v, want %v", header, expected)
	}
}

func TestAMQPHandler_BuildFrame(t *testing.T) {
	handler := NewAMQPHandler(nil)

	frame := handler.buildFrame(FrameMethod, 1, []byte{0x01, 0x02, 0x03})

	// Frame should be: type(1) + channel(2) + size(4) + payload + frame-end(1)
	// 1 + 2 + 4 + 3 + 1 = 11 bytes
	if len(frame) != 11 {
		t.Errorf("Frame length = %d, want 11", len(frame))
	}

	if frame[0] != FrameMethod {
		t.Errorf("Frame type = %d, want %d", frame[0], FrameMethod)
	}

	channel := binary.BigEndian.Uint16(frame[1:3])
	if channel != 1 {
		t.Errorf("Frame channel = %d, want 1", channel)
	}

	size := binary.BigEndian.Uint32(frame[3:7])
	if size != 3 {
		t.Errorf("Frame size = %d, want 3", size)
	}

	if frame[len(frame)-1] != 0xCE {
		t.Errorf("Frame-end marker = 0x%02x, want 0xCE", frame[len(frame)-1])
	}
}

func TestAMQPHandler_BuildMethodFrame(t *testing.T) {
	handler := NewAMQPHandler(nil)

	frame := handler.buildMethodFrame(1, ClassConnection, MethodConnectionStart, []byte{0x00, 0x09})

	if len(frame) < 8 {
		t.Fatalf("Method frame too short: %d bytes", len(frame))
	}

	if frame[0] != FrameMethod {
		t.Errorf("Frame type = %d, want %d", frame[0], FrameMethod)
	}

	// Parse the payload to verify class and method IDs
	size := binary.BigEndian.Uint32(frame[3:7])
	payload := frame[7 : 7+size]

	classID := binary.BigEndian.Uint16(payload[0:2])
	methodID := binary.BigEndian.Uint16(payload[2:4])

	if classID != ClassConnection {
		t.Errorf("Class ID = %d, want %d", classID, ClassConnection)
	}

	if methodID != MethodConnectionStart {
		t.Errorf("Method ID = %d, want %d", methodID, MethodConnectionStart)
	}
}

func TestAMQPHandler_ParseFrame(t *testing.T) {
	handler := NewAMQPHandler(nil)

	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "too short",
			data:    []byte{1, 0, 1, 0, 0, 0},
			wantErr: true,
		},
		{
			name:    "invalid frame-end",
			data:    []byte{1, 0, 1, 0, 0, 0, 1, 0x01, 0x00},
			wantErr: true,
		},
		{
			name:    "valid frame",
			data:    []byte{1, 0, 1, 0, 0, 0, 1, 0x01, 0xCE},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handler.parseFrame(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFrame() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAMQPHandler_ParseFrame_Valid(t *testing.T) {
	handler := NewAMQPHandler(nil)

	// Build a valid frame
	payload := []byte{0x01, 0x02, 0x03}
	data := handler.buildFrame(FrameMethod, 5, payload)

	frame, err := handler.parseFrame(data)
	if err != nil {
		t.Fatalf("parseFrame() error = %v", err)
	}

	if frame.Type != FrameMethod {
		t.Errorf("Frame type = %d, want %d", frame.Type, FrameMethod)
	}

	if frame.Channel != 5 {
		t.Errorf("Frame channel = %d, want 5", frame.Channel)
	}

	if !bytes.Equal(frame.Payload, payload) {
		t.Errorf("Frame payload = %v, want %v", frame.Payload, payload)
	}
}

func TestAMQPHandler_BuildConnectionStart(t *testing.T) {
	handler := NewAMQPHandler(nil)

	frame := handler.buildConnectionStart()

	if len(frame) < 8 {
		t.Fatalf("Connection.Start frame too short: %d bytes", len(frame))
	}

	if frame[0] != FrameMethod {
		t.Errorf("Frame type = %d, want %d", frame[0], FrameMethod)
	}

	// Verify channel 0 for connection methods
	channel := binary.BigEndian.Uint16(frame[1:3])
	if channel != 0 {
		t.Errorf("Frame channel = %d, want 0", channel)
	}

	// Parse method
	size := binary.BigEndian.Uint32(frame[3:7])
	payload := frame[7 : 7+size]

	classID := binary.BigEndian.Uint16(payload[0:2])
	methodID := binary.BigEndian.Uint16(payload[2:4])

	if classID != ClassConnection {
		t.Errorf("Class ID = %d, want %d", classID, ClassConnection)
	}

	if methodID != MethodConnectionStart {
		t.Errorf("Method ID = %d, want %d", methodID, MethodConnectionStart)
	}
}

func TestAMQPHandler_HandleHeartbeat(t *testing.T) {
	handler := NewAMQPHandler(nil)

	frame := &AMQPFrame{
		Type:    FrameHeartbeat,
		Channel: 0,
		Payload: nil,
	}

	response, err := handler.handleHeartbeat(frame)
	if err != nil {
		t.Fatalf("handleHeartbeat() error = %v", err)
	}

	if response == nil {
		t.Fatal("handleHeartbeat() returned nil response")
	}

	// Response should be a heartbeat frame
	if response[0] != FrameHeartbeat {
		t.Errorf("Response frame type = %d, want %d", response[0], FrameHeartbeat)
	}
}

func TestErrAMQPProtocolError(t *testing.T) {
	err := &ErrAMQPProtocolError{Message: "test error"}

	expected := "amqp protocol error: test error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestErrAMQPDisconnect(t *testing.T) {
	expected := "amqp: client disconnected"
	if ErrAMQPDisconnect.Error() != expected {
		t.Errorf("ErrAMQPDisconnect.Error() = %q, want %q", ErrAMQPDisconnect.Error(), expected)
	}
}

func TestAMQPHandler_ResetCountersMultiple(t *testing.T) {
	handler := NewAMQPHandler(nil)

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

func TestWriteShortString(t *testing.T) {
	buf := &bytes.Buffer{}
	writeShortString(buf, "hello")

	if buf.Len() != 6 {
		t.Errorf("Buffer length = %d, want 6", buf.Len())
	}

	data := buf.Bytes()
	if data[0] != 5 {
		t.Errorf("String length byte = %d, want 5", data[0])
	}

	if string(data[1:]) != "hello" {
		t.Errorf("String content = %q, want hello", string(data[1:]))
	}
}

func TestWriteShortString_Truncate(t *testing.T) {
	buf := &bytes.Buffer{}
	longString := strings.Repeat("a", 300)
	writeShortString(buf, longString)

	data := buf.Bytes()
	if data[0] != 255 {
		t.Errorf("String length byte = %d, want 255 (truncated)", data[0])
	}

	if len(data) != 256 {
		t.Errorf("Buffer length = %d, want 256", len(data))
	}
}

func TestWriteLongString(t *testing.T) {
	buf := &bytes.Buffer{}
	writeLongString(buf, "hello world")

	if buf.Len() != 15 { // 4 bytes length + 11 bytes string
		t.Errorf("Buffer length = %d, want 15", buf.Len())
	}

	data := buf.Bytes()
	length := binary.BigEndian.Uint32(data[0:4])
	if length != 11 {
		t.Errorf("String length = %d, want 11", length)
	}

	if string(data[4:]) != "hello world" {
		t.Errorf("String content = %q, want 'hello world'", string(data[4:]))
	}
}

func TestWriteTable(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"product": "test",
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}

	data := buf.Bytes()
	tableLen := binary.BigEndian.Uint32(data[0:4])

	if int(tableLen) != buf.Len()-4 {
		t.Errorf("Table length = %d, want %d", tableLen, buf.Len()-4)
	}
}

func TestWriteTable_BooleanValue(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"flag": true,
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestWriteTable_IntegerValue(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"count": 42,
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

// buildAMQPProtocolHeader creates the AMQP 0-9-1 protocol header.
func buildAMQPProtocolHeader() []byte {
	return []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}
}

// buildAMQPMethodFrame builds an AMQP method frame for testing.
func buildAMQPMethodFrame(channel uint16, classID, methodID uint16, args []byte) []byte {
	payload := &bytes.Buffer{}
	binary.Write(payload, binary.BigEndian, classID)
	binary.Write(payload, binary.BigEndian, methodID)
	payload.Write(args)

	buf := &bytes.Buffer{}
	buf.WriteByte(FrameMethod)
	binary.Write(buf, binary.BigEndian, channel)
	binary.Write(buf, binary.BigEndian, uint32(payload.Len()))
	buf.Write(payload.Bytes())
	buf.WriteByte(0xCE)
	return buf.Bytes()
}

// buildConnectionStartOkFrame builds a Connection.Start-Ok frame.
func buildConnectionStartOkFrame() []byte {
	args := &bytes.Buffer{}
	// Client properties (empty table)
	binary.Write(args, binary.BigEndian, uint32(0))
	// Mechanism
	writeShortString(args, "PLAIN")
	// Response
	writeLongString(args, "\x00guest\x00guest")
	// Locale
	writeShortString(args, "en_US")

	return buildAMQPMethodFrame(0, ClassConnection, MethodConnectionStartOk, args.Bytes())
}

// buildConnectionTuneOkFrame builds a Connection.Tune-Ok frame.
func buildConnectionTuneOkFrame() []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(2047))   // channel-max
	binary.Write(args, binary.BigEndian, uint32(131072)) // frame-max
	binary.Write(args, binary.BigEndian, uint16(60))     // heartbeat

	return buildAMQPMethodFrame(0, ClassConnection, MethodConnectionTuneOk, args.Bytes())
}

// buildConnectionOpenFrame builds a Connection.Open frame.
func buildConnectionOpenFrame(vhost string) []byte {
	args := &bytes.Buffer{}
	writeShortString(args, vhost)
	args.WriteByte(0) // reserved
	args.WriteByte(0) // reserved

	return buildAMQPMethodFrame(0, ClassConnection, MethodConnectionOpen, args.Bytes())
}

// buildConnectionCloseFrame builds a Connection.Close frame.
func buildConnectionCloseFrame() []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(200)) // reply-code
	writeShortString(args, "Normal shutdown")         // reply-text
	binary.Write(args, binary.BigEndian, uint16(0))   // class-id
	binary.Write(args, binary.BigEndian, uint16(0))   // method-id

	return buildAMQPMethodFrame(0, ClassConnection, MethodConnectionClose, args.Bytes())
}

// buildChannelOpenFrame builds a Channel.Open frame.
func buildChannelOpenFrame(channel uint16) []byte {
	args := &bytes.Buffer{}
	writeShortString(args, "") // reserved

	return buildAMQPMethodFrame(channel, ClassChannel, MethodChannelOpen, args.Bytes())
}

// buildQueueDeclareFrame builds a Queue.Declare frame.
func buildQueueDeclareFrame(channel uint16, queueName string, durable, exclusive, autoDelete bool) []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0)) // reserved
	writeShortString(args, queueName)
	var flags byte
	if durable {
		flags |= 0x02
	}
	if exclusive {
		flags |= 0x04
	}
	if autoDelete {
		flags |= 0x08
	}
	args.WriteByte(flags)
	binary.Write(args, binary.BigEndian, uint32(0)) // arguments (empty table)

	return buildAMQPMethodFrame(channel, ClassQueue, MethodQueueDeclare, args.Bytes())
}

// buildBasicConsumeFrame builds a Basic.Consume frame.
func buildBasicConsumeFrame(channel uint16, queueName, consumerTag string, noAck bool) []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0)) // reserved
	writeShortString(args, queueName)
	writeShortString(args, consumerTag)
	var flags byte
	if noAck {
		flags |= 0x02
	}
	args.WriteByte(flags)
	binary.Write(args, binary.BigEndian, uint32(0)) // arguments (empty table)

	return buildAMQPMethodFrame(channel, ClassBasic, MethodBasicConsume, args.Bytes())
}

func TestAMQPHandler_HandleConnection_ProtocolHeader(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Send AMQP protocol header
	err = conn.Write(ctx, websocket.MessageBinary, buildAMQPProtocolHeader())
	if err != nil {
		t.Fatalf("Write protocol header failed: %v", err)
	}

	// Read Connection.Start
	_, startData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Connection.Start failed: %v", err)
	}

	// Verify it's a method frame
	if len(startData) < 1 || startData[0] != FrameMethod {
		t.Errorf("Expected method frame (type 1), got type %d", startData[0])
	}

	// Verify message counter
	if handler.MessagesReceived() != 1 {
		t.Errorf("MessagesReceived() = %d, want 1", handler.MessagesReceived())
	}

	// Wait for the MessagesSent counter to be incremented
	if !waitForCounter(handler.MessagesSent, 1, 1*time.Second) {
		t.Errorf("MessagesSent() = %d, want >= 1", handler.MessagesSent())
	}
}

func TestAMQPHandler_HandleConnection_FullHandshake(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Send protocol header
	err = conn.Write(ctx, websocket.MessageBinary, buildAMQPProtocolHeader())
	if err != nil {
		t.Fatalf("Write protocol header failed: %v", err)
	}

	// Read Connection.Start
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Connection.Start failed: %v", err)
	}

	// Send Connection.Start-Ok
	err = conn.Write(ctx, websocket.MessageBinary, buildConnectionStartOkFrame())
	if err != nil {
		t.Fatalf("Write Connection.Start-Ok failed: %v", err)
	}

	// Read Connection.Tune
	_, tuneData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Connection.Tune failed: %v", err)
	}

	if tuneData[0] != FrameMethod {
		t.Errorf("Expected method frame for Tune")
	}

	// Send Connection.Tune-Ok
	err = conn.Write(ctx, websocket.MessageBinary, buildConnectionTuneOkFrame())
	if err != nil {
		t.Fatalf("Write Connection.Tune-Ok failed: %v", err)
	}

	// Send Connection.Open
	err = conn.Write(ctx, websocket.MessageBinary, buildConnectionOpenFrame("/"))
	if err != nil {
		t.Fatalf("Write Connection.Open failed: %v", err)
	}

	// Read Connection.Open-Ok
	_, openOkData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Connection.Open-Ok failed: %v", err)
	}

	if openOkData[0] != FrameMethod {
		t.Errorf("Expected method frame for Open-Ok")
	}

	// Send Connection.Close
	err = conn.Write(ctx, websocket.MessageBinary, buildConnectionCloseFrame())
	if err != nil {
		t.Fatalf("Write Connection.Close failed: %v", err)
	}

	// Wait for handler to complete
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Errorf("Handler returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete after close")
	}
}

func TestAMQPHandler_HandleConnection_ChannelOpen(t *testing.T) {
	handler := NewAMQPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Complete connection handshake
	conn.Write(ctx, websocket.MessageBinary, buildAMQPProtocolHeader())
	conn.Read(ctx) // Connection.Start
	conn.Write(ctx, websocket.MessageBinary, buildConnectionStartOkFrame())
	conn.Read(ctx) // Connection.Tune
	conn.Write(ctx, websocket.MessageBinary, buildConnectionTuneOkFrame())
	conn.Write(ctx, websocket.MessageBinary, buildConnectionOpenFrame("/"))
	conn.Read(ctx) // Connection.Open-Ok

	// Open channel 1
	err = conn.Write(ctx, websocket.MessageBinary, buildChannelOpenFrame(1))
	if err != nil {
		t.Fatalf("Write Channel.Open failed: %v", err)
	}

	// Read Channel.Open-Ok
	_, channelOpenOkData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Channel.Open-Ok failed: %v", err)
	}

	if channelOpenOkData[0] != FrameMethod {
		t.Errorf("Expected method frame for Channel.Open-Ok")
	}

	// Verify channel is recorded in state
	handler.clientsMu.RLock()
	clientState := handler.clients["test-session"]
	handler.clientsMu.RUnlock()

	if clientState == nil {
		t.Fatal("Client state not found")
	}

	clientState.mu.RLock()
	ch := clientState.channels[1]
	clientState.mu.RUnlock()

	if ch == nil {
		t.Error("Channel 1 not found in state")
	}

	if !ch.open {
		t.Error("Channel 1 should be open")
	}
}

func TestAMQPHandler_HandleConnection_QueueDeclare(t *testing.T) {
	handler := NewAMQPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Complete connection and channel handshake
	conn.Write(ctx, websocket.MessageBinary, buildAMQPProtocolHeader())
	conn.Read(ctx) // Connection.Start
	conn.Write(ctx, websocket.MessageBinary, buildConnectionStartOkFrame())
	conn.Read(ctx) // Connection.Tune
	conn.Write(ctx, websocket.MessageBinary, buildConnectionTuneOkFrame())
	conn.Write(ctx, websocket.MessageBinary, buildConnectionOpenFrame("/"))
	conn.Read(ctx) // Connection.Open-Ok
	conn.Write(ctx, websocket.MessageBinary, buildChannelOpenFrame(1))
	conn.Read(ctx) // Channel.Open-Ok

	// Declare queue
	err = conn.Write(ctx, websocket.MessageBinary, buildQueueDeclareFrame(1, "test-queue", true, false, false))
	if err != nil {
		t.Fatalf("Write Queue.Declare failed: %v", err)
	}

	// Read Queue.Declare-Ok
	_, queueDeclareOkData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read Queue.Declare-Ok failed: %v", err)
	}

	if queueDeclareOkData[0] != FrameMethod {
		t.Errorf("Expected method frame for Queue.Declare-Ok")
	}

	// Verify queue is recorded in state
	handler.clientsMu.RLock()
	clientState := handler.clients["test-session"]
	handler.clientsMu.RUnlock()

	clientState.mu.RLock()
	ch := clientState.channels[1]
	queue := ch.queues["test-queue"]
	clientState.mu.RUnlock()

	if queue == nil {
		t.Error("Queue not found in state")
	}

	if queue.name != "test-queue" {
		t.Errorf("Queue name = %q, want test-queue", queue.name)
	}

	if !queue.durable {
		t.Error("Queue should be durable")
	}
}

func TestAMQPHandler_HandleConnection_NormalClosure(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Close with normal closure immediately
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

func TestAMQPHandler_HandleConnection_ContextCancel(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handlerDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

func TestAMQPHandler_HandleFrame_UnknownFrameType(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	// Build a frame with unknown type (99)
	buf := &bytes.Buffer{}
	buf.WriteByte(99) // Unknown frame type
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(0))
	buf.WriteByte(0xCE)

	_, err := handler.handleFrame(context.Background(), buf.Bytes(), session, state)
	if err == nil {
		t.Error("Expected error for unknown frame type")
	}
}

func TestAMQPHandler_HandleMethodFrame_UnsupportedMethod(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	// Build a method frame with unsupported class/method
	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 0,
		Payload: []byte{0xFF, 0xFF, 0xFF, 0xFF}, // Invalid class/method IDs
	}

	_, err := handler.handleMethodFrame(context.Background(), frame, session, state)
	if err == nil {
		t.Error("Expected error for unsupported method")
	}
}

func TestAMQPHandler_HandleMethodFrame_TooShort(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 0,
		Payload: []byte{0x00, 0x0A}, // Only 2 bytes, need at least 4
	}

	_, err := handler.handleMethodFrame(context.Background(), frame, session, state)
	if err == nil {
		t.Error("Expected error for method frame too short")
	}
}

func TestAMQPHandler_BuildBasicDeliver(t *testing.T) {
	handler := NewAMQPHandler(nil)

	result := handler.buildBasicDeliver(1, "consumer-tag", 123, "test.queue", []byte("hello"))

	if len(result) < 8 {
		t.Fatalf("Deliver frames too short: %d bytes", len(result))
	}

	// First frame should be method frame
	if result[0] != FrameMethod {
		t.Errorf("First frame type = %d, want %d (method)", result[0], FrameMethod)
	}
}

func TestAMQPHandler_BuildBasicDeliver_EmptyPayload(t *testing.T) {
	handler := NewAMQPHandler(nil)

	result := handler.buildBasicDeliver(1, "consumer-tag", 123, "test.queue", nil)

	if len(result) < 8 {
		t.Fatalf("Deliver frame too short: %d bytes", len(result))
	}

	// Should only have method frame for empty payload
	if result[0] != FrameMethod {
		t.Errorf("Frame type = %d, want %d (method)", result[0], FrameMethod)
	}
}

func TestAMQPHandler_HandleConnectionOpen_MissingVhost(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 0,
		Payload: nil,
	}

	_, err := handler.handleConnectionOpen(context.Background(), frame, []byte{}, session, state)
	if err == nil {
		t.Error("Expected error for missing vhost")
	}
}

func TestAMQPHandler_HandleConnectionOpen_InvalidVhost(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 0,
		Payload: nil,
	}

	// Vhost length says 10 but only 2 bytes follow
	_, err := handler.handleConnectionOpen(context.Background(), frame, []byte{10, '/', '/'}, session, state)
	if err == nil {
		t.Error("Expected error for invalid vhost")
	}
}

func TestAMQPHandler_HandleQueueDeclare_ChannelNotOpen(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 1, // Channel not open
		Payload: nil,
	}

	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0)) // reserved
	args.WriteByte(4)                               // queue name length
	args.WriteString("test")
	args.WriteByte(0) // flags

	_, err := handler.handleQueueDeclare(context.Background(), frame, args.Bytes(), session, state)
	if err == nil {
		t.Error("Expected error when channel not open")
	}
}

func TestAMQPHandler_HandleQueueDeclare_ArgsTooShort(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 1,
		Payload: nil,
	}

	_, err := handler.handleQueueDeclare(context.Background(), frame, []byte{0, 0}, session, state)
	if err == nil {
		t.Error("Expected error for args too short")
	}
}

func TestAMQPHandler_HandleBasicPublish_ArgsTooShort(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 1,
		Payload: nil,
	}

	_, err := handler.handleBasicPublish(context.Background(), frame, []byte{0, 0, 0}, session, state)
	if err == nil {
		t.Error("Expected error for args too short")
	}
}

func TestAMQPHandler_HandleBasicConsume_ArgsTooShort(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}
	session := ws.NewSession("test", "amqp", nil)

	frame := &AMQPFrame{
		Type:    FrameMethod,
		Channel: 1,
		Payload: nil,
	}

	_, err := handler.handleBasicConsume(context.Background(), frame, []byte{0, 0, 0}, session, state)
	if err == nil {
		t.Error("Expected error for args too short")
	}
}

func TestAMQPHandler_InvalidProtocolHeader(t *testing.T) {
	handler := NewAMQPHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "amqp", conn)

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

	// Send invalid protocol header
	err = conn.Write(ctx, websocket.MessageBinary, []byte("INVALID!"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Wait for handler to complete with error
	select {
	case err := <-handlerDone:
		if err == nil {
			t.Error("Handler should return error for invalid protocol header")
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete")
	}
}

func TestAMQPHandler_ConcurrentClients(t *testing.T) {
	handler := NewAMQPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("session-"+r.RemoteAddr, "amqp", conn)

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

			// Send protocol header
			err = conn.Write(ctx, websocket.MessageBinary, buildAMQPProtocolHeader())
			if err != nil {
				t.Errorf("Client %d write protocol header failed: %v", idx, err)
				return
			}

			// Read Connection.Start
			_, _, err = conn.Read(ctx)
			if err != nil {
				t.Errorf("Client %d read Connection.Start failed: %v", idx, err)
				return
			}
		}(i)
	}

	wg.Wait()

	// Each client sends protocol header (1 message)
	expectedReceived := uint64(numClients)
	if handler.MessagesReceived() != expectedReceived {
		t.Errorf("MessagesReceived() = %d, want %d", handler.MessagesReceived(), expectedReceived)
	}

	// Each client receives Connection.Start (1 message)
	expectedSent := uint64(numClients)
	if handler.MessagesSent() != expectedSent {
		t.Errorf("MessagesSent() = %d, want %d", handler.MessagesSent(), expectedSent)
	}
}
