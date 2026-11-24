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
	"strings"
	"testing"
	"time"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// newTestAMQPState creates a connected AMQP client state with an open channel for testing.
func newTestAMQPState() *amqpClientState {
	return &amqpClientState{
		connected: true,
		vhost:     "/",
		channels: map[uint16]*amqpChannelState{
			1: {
				open:            true,
				flowActive:      true,
				queues:          make(map[string]*amqpQueueState),
				exchanges:       make(map[string]*amqpExchangeState),
				unackedMessages: make(map[uint64]*amqpUnackedMessage),
			},
		},
		consumers: make(map[string]amqpConsumer),
	}
}

func TestErrAMQPChannelError(t *testing.T) {
	err := &ErrAMQPChannelError{
		ReplyCode: ChannelError,
		ReplyText: "test channel error",
		ClassID:   ClassBasic,
		MethodID:  MethodBasicPublish,
	}

	expected := "amqp channel error: test channel error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestErrAMQPConnectionError(t *testing.T) {
	err := &ErrAMQPConnectionError{
		ReplyCode: ConnectionForced,
		ReplyText: "test connection error",
		ClassID:   ClassConnection,
		MethodID:  MethodConnectionClose,
	}

	expected := "amqp connection error: test connection error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestAMQPHandler_HandleChannelFlow(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := newTestAMQPState()
	session := newGrantedSession("test", "amqp", nil)

	tests := []struct {
		name       string
		args       []byte
		wantActive bool
	}{
		{"flow active", []byte{0x01}, true},
		{"flow inactive", []byte{0x00}, false},
		{"empty args defaults inactive", []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
			response, err := handler.handleChannelFlow(context.Background(), frame, tt.args, session, state)
			if err != nil {
				t.Fatalf("handleChannelFlow() error = %v", err)
			}
			if response == nil {
				t.Fatal("handleChannelFlow() returned nil response")
			}
			if response[0] != FrameMethod {
				t.Errorf("Response frame type = %d, want %d", response[0], FrameMethod)
			}

			state.mu.RLock()
			ch := state.channels[1]
			active := ch.flowActive
			state.mu.RUnlock()

			if active != tt.wantActive {
				t.Errorf("flowActive = %v, want %v", active, tt.wantActive)
			}
		})
	}
}

func TestAMQPHandler_HandleChannelClose(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := newTestAMQPState()
	session := newGrantedSession("test", "amqp", nil)

	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(200))
	writeShortString(args, "Normal")
	binary.Write(args, binary.BigEndian, uint16(0))
	binary.Write(args, binary.BigEndian, uint16(0))

	response, err := handler.handleChannelClose(context.Background(), frame, args.Bytes(), session, state)
	if err != nil {
		t.Fatalf("handleChannelClose() error = %v", err)
	}
	if response == nil {
		t.Fatal("handleChannelClose() returned nil response")
	}

	state.mu.RLock()
	_, exists := state.channels[1]
	state.mu.RUnlock()

	if exists {
		t.Error("Channel 1 should be removed after close")
	}
}

func TestAMQPHandler_HandleConnectionSecureOk(t *testing.T) {
	handler := NewAMQPHandler(nil)
	state := newTestAMQPState()
	session := newGrantedSession("test", "amqp", nil)

	frame := &AMQPFrame{Type: FrameMethod, Channel: 0}
	response, err := handler.handleConnectionSecureOk(context.Background(), frame, []byte{}, session, state)
	if err != nil {
		t.Fatalf("handleConnectionSecureOk() error = %v", err)
	}
	if response == nil {
		t.Fatal("handleConnectionSecureOk() returned nil response (should return Tune frame)")
	}
	if response[0] != FrameMethod {
		t.Errorf("Response frame type = %d, want %d", response[0], FrameMethod)
	}
}

// buildExchangeDeclareArgs builds Exchange.Declare method arguments for testing.
func buildExchangeDeclareArgs(exchangeName, exchangeType string, flags byte) []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0)) // reserved
	writeShortString(args, exchangeName)
	writeShortString(args, exchangeType)
	args.WriteByte(flags)                           // flags: passive(0x01), durable(0x02), no-wait(0x10)
	binary.Write(args, binary.BigEndian, uint32(0)) // arguments (empty table)
	return args.Bytes()
}

func TestAMQPHandler_HandleExchangeDeclare(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("declare new exchange", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildExchangeDeclareArgs("test-exchange", "direct", 0x02) // durable

		response, err := handler.handleExchangeDeclare(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleExchangeDeclare() error = %v", err)
		}
		if response == nil {
			t.Fatal("handleExchangeDeclare() returned nil response")
		}

		state.mu.RLock()
		ex := state.channels[1].exchanges["test-exchange"]
		state.mu.RUnlock()

		if ex == nil {
			t.Fatal("Exchange not found in state")
		}
		if ex.typ != "direct" {
			t.Errorf("Exchange type = %q, want direct", ex.typ)
		}
		if !ex.durable {
			t.Error("Exchange should be durable")
		}
	})

	t.Run("passive declare existing exchange", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].exchanges["existing"] = &amqpExchangeState{name: "existing", typ: "fanout"}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildExchangeDeclareArgs("existing", "fanout", 0x01) // passive

		response, err := handler.handleExchangeDeclare(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleExchangeDeclare() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response for passive declare of existing exchange")
		}
	})

	t.Run("passive declare non-existent exchange returns channel close", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildExchangeDeclareArgs("missing", "direct", 0x01) // passive

		response, err := handler.handleExchangeDeclare(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleExchangeDeclare() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Channel.Close response for missing exchange")
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildExchangeDeclareArgs("nowait-exchange", "topic", 0x10) // no-wait

		response, err := handler.handleExchangeDeclare(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleExchangeDeclare() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait declare")
		}
	})

	t.Run("channel not open", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 99}
		args := buildExchangeDeclareArgs("test", "direct", 0x00)

		_, err := handler.handleExchangeDeclare(context.Background(), frame, args, session, state)
		if err == nil {
			t.Error("Expected error for channel not open")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleExchangeDeclare(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleExchangeDelete(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("delete existing exchange", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].exchanges["to-delete"] = &amqpExchangeState{name: "to-delete"}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0)) // reserved
		writeShortString(args, "to-delete")
		args.WriteByte(0x00) // flags

		response, err := handler.handleExchangeDelete(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleExchangeDelete() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		_, exists := state.channels[1].exchanges["to-delete"]
		state.mu.RUnlock()

		if exists {
			t.Error("Exchange should be deleted")
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test")
		args.WriteByte(0x02) // no-wait

		response, err := handler.handleExchangeDelete(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleExchangeDelete() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})

	t.Run("channel not open", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 99}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test")
		args.WriteByte(0x00)

		_, err := handler.handleExchangeDelete(context.Background(), frame, args.Bytes(), session, state)
		if err == nil {
			t.Error("Expected error for channel not open")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleExchangeDelete(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

// buildQueueBindArgs builds Queue.Bind method arguments for testing.
func buildQueueBindArgs(queueName, exchangeName, routingKey string, noWait bool) []byte {
	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0)) // reserved
	writeShortString(args, queueName)
	writeShortString(args, exchangeName)
	writeShortString(args, routingKey)
	var flags byte
	if noWait {
		flags |= 0x01
	}
	args.WriteByte(flags)
	binary.Write(args, binary.BigEndian, uint32(0)) // arguments (empty table)
	return args.Bytes()
}

func TestAMQPHandler_HandleQueueBind(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("bind queue to exchange", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["test-queue"] = &amqpQueueState{
			name:     "test-queue",
			bindings: make([]amqpBinding, 0),
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildQueueBindArgs("test-queue", "test-exchange", "test.key", false)

		response, err := handler.handleQueueBind(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleQueueBind() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		bindings := state.channels[1].queues["test-queue"].bindings
		state.mu.RUnlock()

		if len(bindings) != 1 {
			t.Fatalf("Expected 1 binding, got %d", len(bindings))
		}
		if bindings[0].exchange != "test-exchange" {
			t.Errorf("Binding exchange = %q, want test-exchange", bindings[0].exchange)
		}
		if bindings[0].routingKey != "test.key" {
			t.Errorf("Binding routing key = %q, want test.key", bindings[0].routingKey)
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["q"] = &amqpQueueState{name: "q", bindings: make([]amqpBinding, 0)}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}
		args := buildQueueBindArgs("q", "ex", "key", true)

		response, err := handler.handleQueueBind(context.Background(), frame, args, session, state)
		if err != nil {
			t.Fatalf("handleQueueBind() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleQueueBind(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleQueueUnbind(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("unbind existing binding", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["test-queue"] = &amqpQueueState{
			name: "test-queue",
			bindings: []amqpBinding{
				{exchange: "ex", routingKey: "key1"},
				{exchange: "ex", routingKey: "key2"},
			},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test-queue")
		writeShortString(args, "ex")
		writeShortString(args, "key1")
		binary.Write(args, binary.BigEndian, uint32(0)) // arguments

		response, err := handler.handleQueueUnbind(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleQueueUnbind() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		bindings := state.channels[1].queues["test-queue"].bindings
		state.mu.RUnlock()

		if len(bindings) != 1 {
			t.Fatalf("Expected 1 binding after unbind, got %d", len(bindings))
		}
		if bindings[0].routingKey != "key2" {
			t.Errorf("Remaining binding routing key = %q, want key2", bindings[0].routingKey)
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleQueueUnbind(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleQueuePurge(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("purge queue with messages", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["test-queue"] = &amqpQueueState{
			name:     "test-queue",
			messages: [][]byte{[]byte("msg1"), []byte("msg2"), []byte("msg3")},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test-queue")
		args.WriteByte(0x00) // flags

		response, err := handler.handleQueuePurge(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleQueuePurge() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		msgs := state.channels[1].queues["test-queue"].messages
		state.mu.RUnlock()

		if len(msgs) != 0 {
			t.Errorf("Expected 0 messages after purge, got %d", len(msgs))
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["q"] = &amqpQueueState{name: "q", messages: [][]byte{[]byte("m")}}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "q")
		args.WriteByte(0x01) // no-wait

		response, err := handler.handleQueuePurge(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleQueuePurge() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleQueuePurge(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleQueueDelete(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("delete existing queue", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["to-delete"] = &amqpQueueState{
			name:     "to-delete",
			messages: [][]byte{[]byte("msg1")},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "to-delete")
		args.WriteByte(0x00) // flags

		response, err := handler.handleQueueDelete(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleQueueDelete() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		_, exists := state.channels[1].queues["to-delete"]
		state.mu.RUnlock()

		if exists {
			t.Error("Queue should be deleted")
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["q"] = &amqpQueueState{name: "q"}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "q")
		args.WriteByte(0x04) // no-wait

		response, err := handler.handleQueueDelete(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleQueueDelete() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleQueueDelete(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicQos(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("set prefetch parameters", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint32(1024)) // prefetch-size
		binary.Write(args, binary.BigEndian, uint16(10))   // prefetch-count
		args.WriteByte(0x01)                               // global

		response, err := handler.handleBasicQos(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicQos() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		ch := state.channels[1]
		state.mu.RUnlock()

		if ch.prefetchSize != 1024 {
			t.Errorf("prefetchSize = %d, want 1024", ch.prefetchSize)
		}
		if ch.prefetchCount != 10 {
			t.Errorf("prefetchCount = %d, want 10", ch.prefetchCount)
		}
		if !ch.prefetchGlobal {
			t.Error("prefetchGlobal should be true")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleBasicQos(context.Background(), frame, []byte{0, 0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicConsume(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("consume with explicit tag", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0)) // reserved
		writeShortString(args, "test-queue")
		writeShortString(args, "my-consumer")
		args.WriteByte(0x02) // no-ack
		binary.Write(args, binary.BigEndian, uint32(0))

		response, err := handler.handleBasicConsume(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicConsume() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Basic.Consume-Ok response")
		}

		state.mu.RLock()
		consumer, exists := state.consumers["my-consumer"]
		state.mu.RUnlock()

		if !exists {
			t.Fatal("Consumer not registered")
		}
		if consumer.queueName != "test-queue" {
			t.Errorf("Consumer queue = %q, want test-queue", consumer.queueName)
		}
		if !consumer.noAck {
			t.Error("Consumer should have noAck=true")
		}
	})

	t.Run("consume with auto-generated tag", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "auto-queue")
		writeShortString(args, "") // empty tag -> auto-generate
		args.WriteByte(0x00)
		binary.Write(args, binary.BigEndian, uint32(0))

		response, err := handler.handleBasicConsume(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicConsume() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		found := false
		for _, c := range state.consumers {
			if c.queueName == "auto-queue" {
				found = true
			}
		}
		state.mu.RUnlock()

		if !found {
			t.Error("Auto-generated consumer not found")
		}
	})

	t.Run("no-wait flag", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "q")
		writeShortString(args, "tag")
		args.WriteByte(0x08) // no-wait
		binary.Write(args, binary.BigEndian, uint32(0))

		response, err := handler.handleBasicConsume(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicConsume() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})
}

func TestAMQPHandler_HandleBasicCancel(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("cancel existing consumer", func(t *testing.T) {
		state := newTestAMQPState()
		state.consumers["my-consumer"] = amqpConsumer{
			queueName:   "test-queue",
			consumerTag: "my-consumer",
			channel:     1,
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		writeShortString(args, "my-consumer")
		args.WriteByte(0x00) // flags

		response, err := handler.handleBasicCancel(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicCancel() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected response")
		}

		state.mu.RLock()
		_, exists := state.consumers["my-consumer"]
		state.mu.RUnlock()

		if exists {
			t.Error("Consumer should be removed")
		}
	})

	t.Run("cancel with no-wait", func(t *testing.T) {
		state := newTestAMQPState()
		state.consumers["tag"] = amqpConsumer{queueName: "q", consumerTag: "tag", channel: 1}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		writeShortString(args, "tag")
		args.WriteByte(0x01) // no-wait

		response, err := handler.handleBasicCancel(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicCancel() error = %v", err)
		}
		if response != nil {
			t.Error("Expected nil response for no-wait")
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleBasicCancel(context.Background(), frame, []byte{}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicPublish(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("publish to routing key", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0)) // reserved
		writeShortString(args, "")                      // exchange (default)
		writeShortString(args, "test.routing.key")
		args.WriteByte(0x00) // flags

		response, err := handler.handleBasicPublish(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicPublish() error = %v", err)
		}
		if response != nil {
			t.Error("Basic.Publish should have no response")
		}
	})

	t.Run("invalid exchange name length", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		args.WriteByte(255) // exchange name length = 255 but no data follows

		_, err := handler.handleBasicPublish(context.Background(), frame, args.Bytes(), session, state)
		if err == nil {
			t.Error("Expected error for invalid exchange name")
		}
	})
}

func TestAMQPHandler_HandleBasicGet(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("get from empty queue returns Get-Empty", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["test-queue"] = &amqpQueueState{
			name:     "test-queue",
			messages: make([][]byte, 0),
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test-queue")
		args.WriteByte(0x00) // no-ack = false

		response, err := handler.handleBasicGet(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicGet() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Get-Empty response")
		}
	})

	t.Run("get from queue with messages returns Get-Ok", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["test-queue"] = &amqpQueueState{
			name:     "test-queue",
			messages: [][]byte{[]byte("hello"), []byte("world")},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "test-queue")
		args.WriteByte(0x00) // no-ack = false

		response, err := handler.handleBasicGet(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicGet() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Get-Ok response with content")
		}

		// Should track unacked message
		state.mu.RLock()
		unacked := len(state.channels[1].unackedMessages)
		remaining := len(state.channels[1].queues["test-queue"].messages)
		state.mu.RUnlock()

		if unacked != 1 {
			t.Errorf("Expected 1 unacked message, got %d", unacked)
		}
		if remaining != 1 {
			t.Errorf("Expected 1 remaining message, got %d", remaining)
		}
	})

	t.Run("get with no-ack does not track unacked", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["q"] = &amqpQueueState{
			name:     "q",
			messages: [][]byte{[]byte("msg")},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, "q")
		args.WriteByte(0x01) // no-ack = true

		_, err := handler.handleBasicGet(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicGet() error = %v", err)
		}

		state.mu.RLock()
		unacked := len(state.channels[1].unackedMessages)
		state.mu.RUnlock()

		if unacked != 0 {
			t.Errorf("Expected 0 unacked messages with no-ack, got %d", unacked)
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleBasicGet(context.Background(), frame, []byte{0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicAck(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("single ack", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].unackedMessages = map[uint64]*amqpUnackedMessage{
			1: {deliveryTag: 1, queue: "q"},
			2: {deliveryTag: 2, queue: "q"},
			3: {deliveryTag: 3, queue: "q"},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint64(2)) // delivery-tag
		args.WriteByte(0x00)                            // multiple = false

		response, err := handler.handleBasicAck(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicAck() error = %v", err)
		}
		if response != nil {
			t.Error("Basic.Ack should have no response")
		}

		state.mu.RLock()
		unacked := len(state.channels[1].unackedMessages)
		state.mu.RUnlock()

		if unacked != 2 {
			t.Errorf("Expected 2 unacked messages, got %d", unacked)
		}
	})

	t.Run("multiple ack", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].unackedMessages = map[uint64]*amqpUnackedMessage{
			1: {deliveryTag: 1, queue: "q"},
			2: {deliveryTag: 2, queue: "q"},
			3: {deliveryTag: 3, queue: "q"},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint64(2)) // delivery-tag
		args.WriteByte(0x01)                            // multiple = true

		_, err := handler.handleBasicAck(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicAck() error = %v", err)
		}

		state.mu.RLock()
		unacked := len(state.channels[1].unackedMessages)
		state.mu.RUnlock()

		if unacked != 1 {
			t.Errorf("Expected 1 unacked message after multiple ack, got %d", unacked)
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleBasicAck(context.Background(), frame, []byte{0, 0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicReject(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("reject without requeue", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].unackedMessages = map[uint64]*amqpUnackedMessage{
			1: {deliveryTag: 1, queue: "q"},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint64(1)) // delivery-tag
		args.WriteByte(0x00)                            // requeue = false

		response, err := handler.handleBasicReject(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicReject() error = %v", err)
		}
		if response != nil {
			t.Error("Basic.Reject should have no response")
		}

		state.mu.RLock()
		unacked := len(state.channels[1].unackedMessages)
		state.mu.RUnlock()

		if unacked != 0 {
			t.Errorf("Expected 0 unacked messages, got %d", unacked)
		}
	})

	t.Run("reject with requeue", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].queues["q"] = &amqpQueueState{name: "q", messages: make([][]byte, 0)}
		state.channels[1].unackedMessages = map[uint64]*amqpUnackedMessage{
			1: {deliveryTag: 1, queue: "q"},
		}
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		args := &bytes.Buffer{}
		binary.Write(args, binary.BigEndian, uint64(1))
		args.WriteByte(0x01) // requeue = true

		_, err := handler.handleBasicReject(context.Background(), frame, args.Bytes(), session, state)
		if err != nil {
			t.Fatalf("handleBasicReject() error = %v", err)
		}
	})

	t.Run("args too short", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		_, err := handler.handleBasicReject(context.Background(), frame, []byte{0, 0, 0}, session, state)
		if err == nil {
			t.Error("Expected error for args too short")
		}
	})
}

func TestAMQPHandler_HandleBasicRecoverAsync(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)
	state := newTestAMQPState()
	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

	response, err := handler.handleBasicRecoverAsync(context.Background(), frame, []byte{0x01}, session, state)
	if err != nil {
		t.Fatalf("handleBasicRecoverAsync() error = %v", err)
	}
	if response != nil {
		t.Error("BasicRecoverAsync should have no response")
	}
}

func TestAMQPHandler_HandleBasicRecover(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)
	state := newTestAMQPState()
	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

	response, err := handler.handleBasicRecover(context.Background(), frame, []byte{0x01}, session, state)
	if err != nil {
		t.Fatalf("handleBasicRecover() error = %v", err)
	}
	if response == nil {
		t.Fatal("Expected Recover-Ok response")
	}
}

func TestAMQPHandler_HandleTxSelect(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)
	state := newTestAMQPState()
	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

	response, err := handler.handleTxSelect(context.Background(), frame, nil, session, state)
	if err != nil {
		t.Fatalf("handleTxSelect() error = %v", err)
	}
	if response == nil {
		t.Fatal("Expected Tx.Select-Ok response")
	}

	state.mu.RLock()
	transacted := state.channels[1].transacted
	state.mu.RUnlock()

	if !transacted {
		t.Error("Channel should be in transacted mode")
	}
}

func TestAMQPHandler_HandleTxCommit(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("commit on transacted channel", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].transacted = true
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		response, err := handler.handleTxCommit(context.Background(), frame, nil, session, state)
		if err != nil {
			t.Fatalf("handleTxCommit() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Tx.Commit-Ok response")
		}
	})

	t.Run("commit on non-transacted channel returns channel close", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		response, err := handler.handleTxCommit(context.Background(), frame, nil, session, state)
		if err != nil {
			t.Fatalf("handleTxCommit() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Channel.Close response for non-transacted")
		}
	})
}

func TestAMQPHandler_HandleTxRollback(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)

	t.Run("rollback on transacted channel", func(t *testing.T) {
		state := newTestAMQPState()
		state.channels[1].transacted = true
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		response, err := handler.handleTxRollback(context.Background(), frame, nil, session, state)
		if err != nil {
			t.Fatalf("handleTxRollback() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Tx.Rollback-Ok response")
		}
	})

	t.Run("rollback on non-transacted channel returns channel close", func(t *testing.T) {
		state := newTestAMQPState()
		frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

		response, err := handler.handleTxRollback(context.Background(), frame, nil, session, state)
		if err != nil {
			t.Fatalf("handleTxRollback() error = %v", err)
		}
		if response == nil {
			t.Fatal("Expected Channel.Close response for non-transacted")
		}
	})
}

func TestAMQPHandler_BuildChannelClose(t *testing.T) {
	handler := NewAMQPHandler(nil)

	result := handler.buildChannelClose(1, NotFound, "not found", ClassQueue, MethodQueueDeclare)
	if result == nil {
		t.Fatal("buildChannelClose() returned nil")
	}
	if result[0] != FrameMethod {
		t.Errorf("Frame type = %d, want %d", result[0], FrameMethod)
	}

	// Verify channel number
	channel := binary.BigEndian.Uint16(result[1:3])
	if channel != 1 {
		t.Errorf("Channel = %d, want 1", channel)
	}
}

func TestAMQPHandler_HandleFrame_HeaderAndBodyFrames(t *testing.T) {
	session := newGrantedSession("test", "amqp", nil)

	t.Run("body split over two frames is published byte for byte", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")

		publishAMQPMethod(t, handler, session, state, "orders")
		body := []byte("hello, amqp body")
		mustNoResponse(t, handler, session, state, handler.buildFrame(FrameHeader, 1, amqpContentHeader(uint64(len(body)))))
		select {
		case p := <-got:
			t.Fatalf("published before the body was complete: %q", p)
		default:
		}
		mustNoResponse(t, handler, session, state, handler.buildFrame(FrameBody, 1, body[:5]))
		mustNoResponse(t, handler, session, state, handler.buildFrame(FrameBody, 1, body[5:]))
		select {
		case p := <-got:
			if !bytes.Equal(p, body) {
				t.Fatalf("payload = %q, want %q", p, body)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("body was not published")
		}
	})

	t.Run("zero body size publishes at the header", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")
		publishAMQPMethod(t, handler, session, state, "orders")
		mustNoResponse(t, handler, session, state, handler.buildFrame(FrameHeader, 1, amqpContentHeader(0)))
		select {
		case p := <-got:
			if len(p) != 0 {
				t.Fatalf("payload = %q, want empty", p)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("empty body was not published")
		}
	})

	t.Run("over-long body is refused", func(t *testing.T) {
		handler := NewAMQPHandler(NewInMemoryPubSubHub())
		state := newTestAMQPState()
		publishAMQPMethod(t, handler, session, state, "orders")
		mustNoResponse(t, handler, session, state, handler.buildFrame(FrameHeader, 1, amqpContentHeader(3)))
		if _, err := handler.handleFrame(context.Background(), handler.buildFrame(FrameBody, 1, []byte("four")), session, state); err == nil {
			t.Fatal("a body longer than declared was accepted")
		}
	})

	t.Run("declared size over the maximum is refused", func(t *testing.T) {
		handler := NewAMQPHandler(NewInMemoryPubSubHub())
		state := newTestAMQPState()
		publishAMQPMethod(t, handler, session, state, "orders")
		if _, err := handler.handleFrame(context.Background(), handler.buildFrame(FrameHeader, 1, amqpContentHeader(amqpMaxBodySize+1)), session, state); err == nil {
			t.Fatal("an oversized declared body was accepted")
		}
	})

	t.Run("orphan header and body frames answer UNEXPECTED_FRAME", func(t *testing.T) {
		handler := NewAMQPHandler(NewInMemoryPubSubHub())
		state := newTestAMQPState()
		for _, data := range [][]byte{
			handler.buildFrame(FrameHeader, 1, amqpContentHeader(2)),
			handler.buildFrame(FrameBody, 1, []byte{0x01, 0x02}),
		} {
			response, err := handler.handleFrame(context.Background(), data, session, state)
			if err != nil {
				t.Fatalf("handleFrame error = %v", err)
			}
			want := handler.buildChannelClose(1, UnexpectedFrame, "", 0, 0)[:13]
			if len(response) < 13 || !bytes.Equal(response[7:11], want[7:11]) {
				t.Fatalf("response is not a Channel.Close: %x", response)
			}
			if code := binary.BigEndian.Uint16(response[11:13]); code != UnexpectedFrame {
				t.Fatalf("reply code = %d, want %d", code, UnexpectedFrame)
			}
		}
	})

	t.Run("short content header is a protocol error", func(t *testing.T) {
		handler := NewAMQPHandler(NewInMemoryPubSubHub())
		state := newTestAMQPState()
		publishAMQPMethod(t, handler, session, state, "orders")
		if _, err := handler.handleFrame(context.Background(), handler.buildFrame(FrameHeader, 1, []byte{0x01, 0x02}), session, state); err == nil {
			t.Fatal("a short content header was accepted")
		}
	})
}

// basicPublishFrame builds a Basic.Publish method frame on channel 1 to the
// default exchange with the given routing key.
func basicPublishFrame(handler *AMQPHandler, routingKey string) []byte {
	args := &bytes.Buffer{}
	_ = binary.Write(args, binary.BigEndian, uint16(0))
	writeShortString(args, "")
	writeShortString(args, routingKey)
	args.WriteByte(0x00)
	return handler.buildMethodFrame(1, ClassBasic, MethodBasicPublish, args.Bytes())
}

// coalescedPublication concatenates Basic.Publish, its content header and
// one body frame per part, as a client sends them in one WebSocket message.
func coalescedPublication(handler *AMQPHandler, routingKey string, parts ...[]byte) []byte {
	var size int
	for _, p := range parts {
		size += len(p)
	}
	msg := basicPublishFrame(handler, routingKey)
	msg = append(msg, handler.buildFrame(FrameHeader, 1, amqpContentHeader(uint64(size)))...)
	for _, p := range parts {
		msg = append(msg, handler.buildFrame(FrameBody, 1, p)...)
	}
	return msg
}

func TestAMQPHandler_HandleFrame_CoalescedPublication(t *testing.T) {
	session := newGrantedSession("test", "amqp", nil)

	receive := func(t *testing.T, got <-chan []byte, want []byte) {
		t.Helper()
		select {
		case p := <-got:
			if !bytes.Equal(p, want) {
				t.Fatalf("payload = %q, want %q", p, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q was not published", want)
		}
	}

	t.Run("publish, header and body in one message are published", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")

		body := []byte("coalesced body")
		mustNoResponse(t, handler, session, state, coalescedPublication(handler, "orders", body))
		receive(t, got, body)
	})

	t.Run("two publications in one message are both published", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")

		msg := coalescedPublication(handler, "orders", []byte("one"))
		msg = append(msg, coalescedPublication(handler, "orders", []byte("two"))...)
		mustNoResponse(t, handler, session, state, msg)
		receive(t, got, []byte("one"))
		receive(t, got, []byte("two"))
	})

	t.Run("a body split over two body frames in one message", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")

		body := []byte("hello, split amqp body")
		mustNoResponse(t, handler, session, state, coalescedPublication(handler, "orders", body[:6], body[6:]))
		receive(t, got, body)
	})

	t.Run("trailing partial frame is a protocol error", func(t *testing.T) {
		handler := NewAMQPHandler(NewInMemoryPubSubHub())
		state := newTestAMQPState()

		msg := basicPublishFrame(handler, "orders")
		msg = append(msg, handler.buildFrame(FrameHeader, 1, amqpContentHeader(4))[:5]...)
		if _, err := handler.handleFrame(context.Background(), msg, session, state); err == nil {
			t.Fatal("a trailing partial frame was accepted")
		}
	})

	t.Run("frames after a denied publish are discarded", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		handler := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")
		denied := newDeniedSession("denied", "amqp", nil)

		response, err := handler.handleFrame(context.Background(), coalescedPublication(handler, "orders", []byte("refused")), denied, state)
		if err != nil {
			t.Fatalf("handleFrame error = %v", err)
		}
		if !isChannelClose(response) || len(response) < 13 {
			t.Fatalf("response is not a Channel.Close: %x", response)
		}
		if code := binary.BigEndian.Uint16(response[11:13]); code != AccessRefused {
			t.Fatalf("reply code = %d, want %d", code, AccessRefused)
		}
		_, n, err := handler.nextFrame(response)
		if err != nil {
			t.Fatalf("response does not parse: %v", err)
		}
		if n != len(response) {
			t.Fatalf("response carries %d bytes after the Channel.Close: %x", len(response)-n, response[n:])
		}
		select {
		case p := <-got:
			t.Fatalf("denied publication reached the subscriber: %q", p)
		case <-time.After(200 * time.Millisecond):
		}
	})
}

// TestAMQPTopicMatch covers AMQP topic binding key matching.
func TestAMQPTopicMatch(t *testing.T) {
	tests := []struct {
		pattern, key string
		want         bool
	}{
		{"order.*", "order.created", true},
		{"order.*", "order.created.eu", false},
		{"order.*", "order", false},
		{"order.#", "order", true},
		{"order.#", "order.created.eu", true},
		{"#", "anything.at.all", true},
		{"#", "", true},
		{"*.created", "order.created", true},
		{"*.created", "order.deleted", false},
		{"order.created", "order.created", true},
		{"order.created", "order.deleted", false},
		{"#.eu", "order.created.eu", true},
		{"#.#", "a", true},
		{"a.#.b", "a.b", true},
		{"a.#.b", "a.x.y.b", true},
		{"a.#.b", "a.x.y.c", false},
		{"*.#", "a", true},
		// An empty routing key splits into one empty word, so '*' matches
		// it and '*.#' matches the empty key.
		{"*.#", "", true},
		{"#", "", true},
		{"a.*.#.c", "a.b.c", true},
	}
	for _, tt := range tests {
		if got := amqpTopicMatch(tt.pattern, tt.key); got != tt.want {
			t.Errorf("amqpTopicMatch(%q, %q) = %v, want %v", tt.pattern, tt.key, got, tt.want)
		}
	}
}

// TestAMQPTopicMatch_AdversarialPatternIsLinear checks that a binding key
// made of many '#' words with a non-matching last word does not take
// exponential time on a short routing key.
func TestAMQPTopicMatch_AdversarialPatternIsLinear(t *testing.T) {
	pattern := strings.Repeat("#.", 40) + "z"
	key := strings.TrimSuffix(strings.Repeat("a.", 40), ".")

	done := make(chan bool, 1)
	go func() { done <- amqpTopicMatch(pattern, key) }()
	select {
	case got := <-done:
		if got {
			t.Fatalf("amqpTopicMatch(%q, %q) = true, want false", pattern, key)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("amqpTopicMatch did not return within 5s on an adversarial pattern")
	}

	if !amqpTopicMatch(strings.Repeat("#.", 40)+"a", key) {
		t.Fatal("amqpTopicMatch of '#' words then 'a' against 'a' words = false, want true")
	}
}

// TestAMQPHandler_PublishRoutesThroughBindings: publications to a named
// exchange reach the queues bound to it, by exchange type.
func TestAMQPHandler_PublishRoutesThroughBindings(t *testing.T) {
	session := newGrantedSession("test", "amqp", nil)

	declareExchange := func(t *testing.T, h *AMQPHandler, state *amqpClientState, name, typ string) {
		t.Helper()
		frame := h.buildMethodFrame(1, ClassExchange, MethodExchangeDeclare, buildExchangeDeclareArgs(name, typ, 0))
		if _, err := h.handleFrame(context.Background(), frame, session, state); err != nil {
			t.Fatalf("Exchange.Declare: %v", err)
		}
	}
	bind := func(t *testing.T, h *AMQPHandler, state *amqpClientState, method uint16, queue, exchange, key string) {
		t.Helper()
		frame := h.buildMethodFrame(1, ClassQueue, method, buildQueueBindArgs(queue, exchange, key, false))
		if _, err := h.handleFrame(context.Background(), frame, session, state); err != nil {
			t.Fatalf("Queue bind method %d: %v", method, err)
		}
	}
	publish := func(t *testing.T, h *AMQPHandler, state *amqpClientState, exchange, key string, body []byte) {
		t.Helper()
		args := &bytes.Buffer{}
		_ = binary.Write(args, binary.BigEndian, uint16(0))
		writeShortString(args, exchange)
		writeShortString(args, key)
		args.WriteByte(0x00)
		msg := h.buildMethodFrame(1, ClassBasic, MethodBasicPublish, args.Bytes())
		msg = append(msg, h.buildFrame(FrameHeader, 1, amqpContentHeader(uint64(len(body))))...)
		msg = append(msg, h.buildFrame(FrameBody, 1, body)...)
		mustNoResponse(t, h, session, state, msg)
	}
	receive := func(t *testing.T, got <-chan []byte, want []byte) {
		t.Helper()
		select {
		case p := <-got:
			if !bytes.Equal(p, want) {
				t.Fatalf("payload = %q, want %q", p, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q was not delivered", want)
		}
	}
	nothing := func(t *testing.T, got <-chan []byte) {
		t.Helper()
		select {
		case p := <-got:
			t.Fatalf("unexpected delivery %q", p)
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Run("direct", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")
		declareExchange(t, h, state, "events", "direct")
		bind(t, h, state, MethodQueueBind, "orders", "events", "order.created")

		publish(t, h, state, "events", "order.created", []byte("created"))
		receive(t, got, []byte("created"))
		publish(t, h, state, "events", "order.deleted", []byte("deleted"))
		nothing(t, got)
	})

	t.Run("fanout", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		gotA := subscribeTestHub(t, hub, "sub-a", "audit")
		gotB := subscribeTestHub(t, hub, "sub-b", "billing")
		declareExchange(t, h, state, "broadcast", "fanout")
		bind(t, h, state, MethodQueueBind, "audit", "broadcast", "")
		bind(t, h, state, MethodQueueBind, "billing", "broadcast", "ignored")

		publish(t, h, state, "broadcast", "any.key", []byte("all"))
		receive(t, gotA, []byte("all"))
		receive(t, gotB, []byte("all"))
	})

	t.Run("topic", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")
		declareExchange(t, h, state, "events", "topic")
		bind(t, h, state, MethodQueueBind, "orders", "events", "order.*")

		publish(t, h, state, "events", "order.created", []byte("created"))
		receive(t, got, []byte("created"))
		publish(t, h, state, "events", "invoice.created", []byte("invoice"))
		nothing(t, got)
	})

	t.Run("default exchange", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")

		publish(t, h, state, "", "orders", []byte("direct to queue"))
		receive(t, got, []byte("direct to queue"))
	})

	t.Run("unbind", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "orders")
		declareExchange(t, h, state, "events", "direct")
		bind(t, h, state, MethodQueueBind, "orders", "events", "order.created")
		bind(t, h, state, MethodQueueUnbind, "orders", "events", "order.created")

		publish(t, h, state, "events", "order.created", []byte("created"))
		nothing(t, got)
	})

	t.Run("unroutable", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		h := NewAMQPHandler(hub)
		state := newTestAMQPState()
		got := subscribeTestHub(t, hub, "sub", "order.created")
		declareExchange(t, h, state, "events", "direct")

		publish(t, h, state, "events", "order.created", []byte("dropped"))
		nothing(t, got)
	})
}

// TestAMQPHandler_BasicPublish_DeniedWithoutPublishPermission: a session
// whose grant refuses messaging:publish gets Channel.Close ACCESS_REFUSED and
// nothing reaches the hub, even when the content follows.
func TestAMQPHandler_BasicPublish_DeniedWithoutPublishPermission(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewAMQPHandler(hub)
	state := newTestAMQPState()
	got := subscribeTestHub(t, hub, "sub", "orders")
	session := newDeniedSession("test", "amqp", nil)

	args := &bytes.Buffer{}
	_ = binary.Write(args, binary.BigEndian, uint16(0))
	writeShortString(args, "")
	writeShortString(args, "orders")
	args.WriteByte(0x00)
	response, err := handler.handleBasicPublish(context.Background(), &AMQPFrame{Type: FrameMethod, Channel: 1}, args.Bytes(), session, state)
	if err != nil {
		t.Fatalf("handleBasicPublish error = %v", err)
	}
	if len(response) < 13 || binary.BigEndian.Uint16(response[11:13]) != AccessRefused {
		t.Fatalf("response is not Channel.Close ACCESS_REFUSED: %x", response)
	}
	_, _ = handler.handleFrame(context.Background(), handler.buildFrame(FrameHeader, 1, amqpContentHeader(2)), session, state)
	_, _ = handler.handleFrame(context.Background(), handler.buildFrame(FrameBody, 1, []byte("hi")), session, state)
	select {
	case p := <-got:
		t.Fatalf("a denied publish reached the hub: %q", p)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestAMQPHandler_ParseFrame_OversizedDeclaredLength: a declared payload
// size beyond the data, including sizes where 7+size+1 wraps in uint32, is
// an error and never an out-of-range index.
func TestAMQPHandler_ParseFrame_OversizedDeclaredLength(t *testing.T) {
	handler := NewAMQPHandler(nil)
	cases := map[string][]byte{
		"size 0xfffffff8 wraps to zero": {0x01, 0x00, 0x00, 0xff, 0xff, 0xff, 0xf8, 0xce},
		"size 0xffffffff":               {0x01, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xce},
		"size one larger than the data": {0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xaa, 0xce},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("parseFrame panicked: %v", r)
				}
			}()
			if _, err := handler.parseFrame(data); err == nil {
				t.Fatal("parseFrame accepted a frame whose declared size exceeds the data")
			}
		})
	}
}

// amqpContentHeader builds a Basic content-header payload: class 60,
// weight 0, the body size and empty property flags.
func amqpContentHeader(bodySize uint64) []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.BigEndian, uint16(ClassBasic))
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	_ = binary.Write(buf, binary.BigEndian, bodySize)
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	return buf.Bytes()
}

func publishAMQPMethod(t *testing.T, handler *AMQPHandler, session *ws.Session, state *amqpClientState, routingKey string) {
	t.Helper()
	args := &bytes.Buffer{}
	_ = binary.Write(args, binary.BigEndian, uint16(0))
	writeShortString(args, "")
	writeShortString(args, routingKey)
	args.WriteByte(0x00)
	response, err := handler.handleBasicPublish(context.Background(), &AMQPFrame{Type: FrameMethod, Channel: 1}, args.Bytes(), session, state)
	if err != nil || response != nil {
		t.Fatalf("handleBasicPublish = %x, %v; want no response", response, err)
	}
}

func mustNoResponse(t *testing.T, handler *AMQPHandler, session *ws.Session, state *amqpClientState, data []byte) {
	t.Helper()
	response, err := handler.handleFrame(context.Background(), data, session, state)
	if err != nil || response != nil {
		t.Fatalf("handleFrame = %x, %v; want no response", response, err)
	}
}

func TestWriteTable_Int32Value(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"count": int32(42),
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestWriteTable_Int64Value(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"bigcount": int64(9999999),
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestWriteTable_BoolFalseValue(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"flag": false,
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestWriteTable_NestedTable(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"nested": map[string]interface{}{
			"inner": "value",
		},
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestWriteTable_UnknownTypeSkipped(t *testing.T) {
	buf := &bytes.Buffer{}
	table := map[string]interface{}{
		"unknown": float64(3.14), // not handled
	}
	writeTable(buf, table)

	if buf.Len() < 4 {
		t.Fatalf("Buffer too short: %d bytes", buf.Len())
	}
}

func TestAMQPHandler_HandleQueueDeclare_PassiveNonExistent(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)
	state := newTestAMQPState()
	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0))
	writeShortString(args, "missing-queue")
	args.WriteByte(0x01) // passive flag

	response, err := handler.handleQueueDeclare(context.Background(), frame, args.Bytes(), session, state)
	if err != nil {
		t.Fatalf("handleQueueDeclare() error = %v", err)
	}
	if response == nil {
		t.Fatal("Expected Channel.Close response for passive declare of non-existent queue")
	}
}

func TestAMQPHandler_HandleQueueDeclare_NoWait(t *testing.T) {
	handler := NewAMQPHandler(nil)
	session := newGrantedSession("test", "amqp", nil)
	state := newTestAMQPState()
	frame := &AMQPFrame{Type: FrameMethod, Channel: 1}

	args := &bytes.Buffer{}
	binary.Write(args, binary.BigEndian, uint16(0))
	writeShortString(args, "nowait-queue")
	args.WriteByte(0x10) // no-wait flag

	response, err := handler.handleQueueDeclare(context.Background(), frame, args.Bytes(), session, state)
	if err != nil {
		t.Fatalf("handleQueueDeclare() error = %v", err)
	}
	if response != nil {
		t.Error("Expected nil response for no-wait declare")
	}
}
