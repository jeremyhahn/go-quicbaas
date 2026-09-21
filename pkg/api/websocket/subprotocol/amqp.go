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

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// AMQP frame types per AMQP 0-9-1 specification.
const (
	FrameMethod    byte = 1
	FrameHeader    byte = 2
	FrameBody      byte = 3
	FrameHeartbeat byte = 8
)

// AMQP protocol constants per AMQP 0-9-1 specification.
const (
	FrameMinSize uint32 = 4096
	FrameEnd     byte   = 0xCE // 206
)

// AMQP class IDs per AMQP 0-9-1 specification.
const (
	ClassConnection uint16 = 10
	ClassChannel    uint16 = 20
	ClassExchange   uint16 = 40
	ClassQueue      uint16 = 50
	ClassBasic      uint16 = 60
	ClassTx         uint16 = 90
)

// Connection class method IDs per AMQP 0-9-1 specification.
const (
	MethodConnectionStart    uint16 = 10
	MethodConnectionStartOk  uint16 = 11
	MethodConnectionSecure   uint16 = 20
	MethodConnectionSecureOk uint16 = 21
	MethodConnectionTune     uint16 = 30
	MethodConnectionTuneOk   uint16 = 31
	MethodConnectionOpen     uint16 = 40
	MethodConnectionOpenOk   uint16 = 41
	MethodConnectionClose    uint16 = 50
	MethodConnectionCloseOk  uint16 = 51
)

// Channel class method IDs per AMQP 0-9-1 specification.
const (
	MethodChannelOpen    uint16 = 10
	MethodChannelOpenOk  uint16 = 11
	MethodChannelFlow    uint16 = 20
	MethodChannelFlowOk  uint16 = 21
	MethodChannelClose   uint16 = 40
	MethodChannelCloseOk uint16 = 41
)

// Exchange class method IDs per AMQP 0-9-1 specification.
const (
	MethodExchangeDeclare   uint16 = 10
	MethodExchangeDeclareOk uint16 = 11
	MethodExchangeDelete    uint16 = 20
	MethodExchangeDeleteOk  uint16 = 21
)

// Queue class method IDs per AMQP 0-9-1 specification.
const (
	MethodQueueDeclare   uint16 = 10
	MethodQueueDeclareOk uint16 = 11
	MethodQueueBind      uint16 = 20
	MethodQueueBindOk    uint16 = 21
	MethodQueuePurge     uint16 = 30
	MethodQueuePurgeOk   uint16 = 31
	MethodQueueDelete    uint16 = 40
	MethodQueueDeleteOk  uint16 = 41
	MethodQueueUnbind    uint16 = 50
	MethodQueueUnbindOk  uint16 = 51
)

// Basic class method IDs per AMQP 0-9-1 specification.
const (
	MethodBasicQos          uint16 = 10
	MethodBasicQosOk        uint16 = 11
	MethodBasicConsume      uint16 = 20
	MethodBasicConsumeOk    uint16 = 21
	MethodBasicCancel       uint16 = 30
	MethodBasicCancelOk     uint16 = 31
	MethodBasicPublish      uint16 = 40
	MethodBasicReturn       uint16 = 50
	MethodBasicDeliver      uint16 = 60
	MethodBasicGet          uint16 = 70
	MethodBasicGetOk        uint16 = 71
	MethodBasicGetEmpty     uint16 = 72
	MethodBasicAck          uint16 = 80
	MethodBasicReject       uint16 = 90
	MethodBasicRecoverAsync uint16 = 100
	MethodBasicRecover      uint16 = 110
	MethodBasicRecoverOk    uint16 = 111
)

// Tx class method IDs per AMQP 0-9-1 specification.
const (
	MethodTxSelect     uint16 = 10
	MethodTxSelectOk   uint16 = 11
	MethodTxCommit     uint16 = 20
	MethodTxCommitOk   uint16 = 21
	MethodTxRollback   uint16 = 30
	MethodTxRollbackOk uint16 = 31
)

// Basic content property flags per AMQP 0-9-1 specification.
const (
	BasicPropertyContentType     uint16 = 0x8000
	BasicPropertyContentEncoding uint16 = 0x4000
	BasicPropertyHeaders         uint16 = 0x2000
	BasicPropertyDeliveryMode    uint16 = 0x1000
	BasicPropertyPriority        uint16 = 0x0800
	BasicPropertyCorrelationID   uint16 = 0x0400
	BasicPropertyReplyTo         uint16 = 0x0200
	BasicPropertyExpiration      uint16 = 0x0100
	BasicPropertyMessageID       uint16 = 0x0080
	BasicPropertyTimestamp       uint16 = 0x0040
	BasicPropertyType            uint16 = 0x0020
	BasicPropertyUserID          uint16 = 0x0010
	BasicPropertyAppID           uint16 = 0x0008
	BasicPropertyReserved        uint16 = 0x0004
)

// AMQP reply codes per AMQP 0-9-1 specification.
const (
	ReplySuccess       uint16 = 200
	ContentTooLarge    uint16 = 311
	NoConsumers        uint16 = 313
	ConnectionForced   uint16 = 320
	InvalidPath        uint16 = 402
	AccessRefused      uint16 = 403
	NotFound           uint16 = 404
	ResourceLocked     uint16 = 405
	PreconditionFailed uint16 = 406
	FrameError         uint16 = 501
	SyntaxError        uint16 = 502
	CommandInvalid     uint16 = 503
	ChannelError       uint16 = 504
	UnexpectedFrame    uint16 = 505
	ResourceError      uint16 = 506
	NotAllowed         uint16 = 530
	NotImplemented     uint16 = 540
	InternalError      uint16 = 541
)

// AMQPFrame represents an AMQP 0-9-1 frame.
type AMQPFrame struct {
	Type    byte
	Channel uint16
	Payload []byte
}

// AMQPMethod represents an AMQP method frame payload.
type AMQPMethod struct {
	ClassID  uint16
	MethodID uint16
	Args     []byte
}

// BasicProperties represents AMQP Basic content properties.
type BasicProperties struct {
	ContentType     string
	ContentEncoding string
	Headers         map[string]interface{}
	DeliveryMode    uint8
	Priority        uint8
	CorrelationID   string
	ReplyTo         string
	Expiration      string
	MessageID       string
	Timestamp       uint64
	Type            string
	UserID          string
	AppID           string
}

// AMQPHandler implements the "amqp" subprotocol for AMQP 0-9-1 over WebSocket.
type AMQPHandler struct {
	// hub manages exchange/queue-based pub/sub.
	hub PubSubHub

	// messagesReceived tracks the total number of messages received.
	messagesReceived atomic.Uint64

	// messagesSent tracks the total number of messages sent.
	messagesSent atomic.Uint64

	// clientsMu protects the clients map.
	clientsMu sync.RWMutex

	// clients maps session IDs to AMQP client state.
	clients map[string]*amqpClientState
}

// amqpClientState holds the state for an AMQP client connection.
type amqpClientState struct {
	connected bool
	vhost     string
	channels  map[uint16]*amqpChannelState
	consumers map[string]amqpConsumer // consumerTag -> consumer
	mu        sync.RWMutex
}

// amqpChannelState holds the state for an AMQP channel.
type amqpChannelState struct {
	open            bool
	flowActive      bool
	transacted      bool
	queues          map[string]*amqpQueueState
	exchanges       map[string]*amqpExchangeState
	prefetchSize    uint32
	prefetchCount   uint16
	prefetchGlobal  bool
	unackedMessages map[uint64]*amqpUnackedMessage
	nextDeliveryTag uint64
}

// amqpQueueState holds the state for an AMQP queue.
type amqpQueueState struct {
	name       string
	durable    bool
	exclusive  bool
	autoDelete bool
	messages   [][]byte
	bindings   []amqpBinding
}

// amqpExchangeState holds the state for an AMQP exchange.
type amqpExchangeState struct {
	name    string
	typ     string
	durable bool
}

// amqpBinding represents a queue-to-exchange binding.
type amqpBinding struct {
	exchange   string
	routingKey string
}

// amqpUnackedMessage represents an unacknowledged message.
type amqpUnackedMessage struct {
	deliveryTag uint64
	queue       string
}

// amqpConsumer represents a message consumer.
type amqpConsumer struct {
	queueName   string
	consumerTag string
	channel     uint16
	noLocal     bool
	noAck       bool
	exclusive   bool
}

// ErrAMQPDisconnect indicates a clean AMQP disconnect.
var ErrAMQPDisconnect = errors.New("amqp: client disconnected")

// ErrAMQPProtocolError represents an AMQP protocol violation.
type ErrAMQPProtocolError struct {
	Message string
}

func (e *ErrAMQPProtocolError) Error() string {
	return "amqp protocol error: " + e.Message
}

// ErrAMQPChannelError represents an AMQP channel error.
type ErrAMQPChannelError struct {
	ReplyCode uint16
	ReplyText string
	ClassID   uint16
	MethodID  uint16
}

func (e *ErrAMQPChannelError) Error() string {
	return "amqp channel error: " + e.ReplyText
}

// ErrAMQPConnectionError represents an AMQP connection error.
type ErrAMQPConnectionError struct {
	ReplyCode uint16
	ReplyText string
	ClassID   uint16
	MethodID  uint16
}

func (e *ErrAMQPConnectionError) Error() string {
	return "amqp connection error: " + e.ReplyText
}

// NewAMQPHandler creates a new AMQP subprotocol handler.
// If hub is nil, an in-memory pub/sub hub is created.
func NewAMQPHandler(hub PubSubHub) *AMQPHandler {
	if hub == nil {
		hub = NewInMemoryPubSubHub()
	}
	return &AMQPHandler{
		hub:     hub,
		clients: make(map[string]*amqpClientState),
	}
}

// Name returns the subprotocol name.
func (h *AMQPHandler) Name() string {
	return "amqp"
}

// HandleConnection handles a WebSocket connection using the AMQP 0-9-1 protocol.
func (h *AMQPHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	clientState := &amqpClientState{
		channels:  make(map[uint16]*amqpChannelState),
		consumers: make(map[string]amqpConsumer),
	}

	// Message delivery channel for this client
	deliveryChan := make(chan amqpDelivery, 100)
	defer close(deliveryChan)

	// Register cleanup on connection close
	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, session.ID)
		h.clientsMu.Unlock()

		// Best effort: the connection is already going away and there is
		// nobody left to report a hub failure to.
		_ = h.hub.UnregisterClient(session.ID)
	}()

	// Store client state
	h.clientsMu.Lock()
	h.clients[session.ID] = clientState
	h.clientsMu.Unlock()

	// Register with hub for message delivery
	if err := h.hub.RegisterClient(session.ID, func(topic string, payload []byte, qos byte, retain bool) {
		select {
		case deliveryChan <- amqpDelivery{
			queue:   topic,
			payload: payload,
		}:
		default:
			// Channel full, drop message
		}
	}); err != nil {
		return err
	}

	// Start message delivery goroutine
	deliveryCtx, cancelDelivery := context.WithCancel(ctx)
	defer cancelDelivery()

	go h.deliverMessages(deliveryCtx, conn, clientState, deliveryChan)

	// Read protocol header first
	_, headerData, err := conn.Read(ctx)
	if err != nil {
		if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return nil
		}
		return err
	}

	h.messagesReceived.Add(1)
	session.UpdateActivity()

	// Validate AMQP protocol header
	if !h.validateProtocolHeader(headerData) {
		// Send protocol header response
		response := h.buildProtocolHeader()
		if err := conn.Write(ctx, websocket.MessageBinary, response); err != nil {
			return err
		}
		h.messagesSent.Add(1)
		return &ErrAMQPProtocolError{Message: "invalid protocol header"}
	}

	// Send Connection.Start
	startFrame := h.buildConnectionStart()
	if err := conn.Write(ctx, websocket.MessageBinary, startFrame); err != nil {
		return err
	}
	h.messagesSent.Add(1)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read WebSocket message
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			return err
		}

		h.messagesReceived.Add(1)
		session.UpdateActivity()

		// Parse and handle AMQP frame
		response, err := h.handleFrame(ctx, data, session, clientState)
		if err != nil {
			if errors.Is(err, ErrAMQPDisconnect) {
				return nil
			}
			return err
		}

		// Send response if any
		if response != nil {
			if err := conn.Write(ctx, websocket.MessageBinary, response); err != nil {
				return err
			}
			h.messagesSent.Add(1)
		}
	}
}

// amqpDelivery represents a message to be delivered to a client.
type amqpDelivery struct {
	queue   string
	payload []byte
}

// deliverMessages handles outbound message delivery to the client.
func (h *AMQPHandler) deliverMessages(ctx context.Context, conn *websocket.Conn, state *amqpClientState, deliveryChan <-chan amqpDelivery) {
	var deliveryTag uint64

	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveryChan:
			if !ok {
				return
			}

			state.mu.RLock()
			// Find consumer for this queue
			var consumer amqpConsumer
			var found bool
			for _, c := range state.consumers {
				if c.queueName == delivery.queue {
					consumer = c
					found = true
					break
				}
			}
			state.mu.RUnlock()

			if !found {
				continue
			}

			deliveryTag++

			// Build Basic.Deliver frame
			deliverFrame := h.buildBasicDeliver(consumer.channel, consumer.consumerTag, deliveryTag, delivery.queue, delivery.payload)

			if err := conn.Write(ctx, websocket.MessageBinary, deliverFrame); err != nil {
				return
			}
			h.messagesSent.Add(1)
		}
	}
}

// validateProtocolHeader checks if the data is a valid AMQP 0-9-1 protocol header.
// AMQP 0-9-1 header format: 'A' 'M' 'Q' 'P' 0x00 major minor revision
// For AMQP 0-9-1: 'A' 'M' 'Q' 'P' 0x00 0x00 0x09 0x01.
func (h *AMQPHandler) validateProtocolHeader(data []byte) bool {
	expected := []byte{'A', 'M', 'Q', 'P', 0x00, 0x00, 0x09, 0x01}
	if len(data) < 8 {
		return false
	}
	return bytes.Equal(data[:8], expected)
}

// buildProtocolHeader builds an AMQP 0-9-1 protocol header.
// Format: 'A' 'M' 'Q' 'P' 0x00 major minor revision.
func (h *AMQPHandler) buildProtocolHeader() []byte {
	return []byte{'A', 'M', 'Q', 'P', 0x00, 0x00, 0x09, 0x01}
}

// buildConnectionStart builds a Connection.Start method frame.
func (h *AMQPHandler) buildConnectionStart() []byte {
	// Build method arguments
	args := &bytes.Buffer{}
	// Version major (octet)
	args.WriteByte(0)
	// Version minor (octet)
	args.WriteByte(9)
	// Server properties (table)
	writeTable(args, map[string]interface{}{
		"product":     "go-quicbaas",
		"version":     "1.0.0",
		"platform":    "Go",
		"information": "https://github.com/jeremyhahn/go-quicbaas",
	})
	// Mechanisms (longstring)
	mechanisms := "PLAIN"
	writeLongString(args, mechanisms)
	// Locales (longstring)
	locales := "en_US"
	writeLongString(args, locales)

	return h.buildMethodFrame(0, ClassConnection, MethodConnectionStart, args.Bytes())
}

// buildMethodFrame builds an AMQP method frame.
func (h *AMQPHandler) buildMethodFrame(channel uint16, classID, methodID uint16, args []byte) []byte {
	// Method frame payload: class-id (2) + method-id (2) + args
	// Writes to a bytes.Buffer never fail.
	payload := &bytes.Buffer{}
	_ = binary.Write(payload, binary.BigEndian, classID)
	_ = binary.Write(payload, binary.BigEndian, methodID)
	payload.Write(args)

	return h.buildFrame(FrameMethod, channel, payload.Bytes())
}

// buildFrame builds an AMQP frame.
// Frame format: type (1) + channel (2) + size (4) + payload + frame-end (1).
func (h *AMQPHandler) buildFrame(frameType byte, channel uint16, payload []byte) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(frameType)
	_ = binary.Write(buf, binary.BigEndian, channel)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)
	buf.WriteByte(FrameEnd)
	return buf.Bytes()
}

// handleFrame parses and handles an AMQP frame.
func (h *AMQPHandler) handleFrame(ctx context.Context, data []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	frame, err := h.parseFrame(data)
	if err != nil {
		return nil, err
	}

	switch frame.Type {
	case FrameMethod:
		return h.handleMethodFrame(ctx, frame, session, state)
	case FrameHeader:
		// Content header - part of publish flow
		return nil, nil
	case FrameBody:
		// Content body - part of publish flow
		return nil, nil
	case FrameHeartbeat:
		return h.handleHeartbeat(frame)
	default:
		return nil, &ErrAMQPProtocolError{Message: "unknown frame type"}
	}
}

// parseFrame parses raw data into an AMQPFrame.
func (h *AMQPHandler) parseFrame(data []byte) (*AMQPFrame, error) {
	if len(data) < 8 {
		return nil, &ErrAMQPProtocolError{Message: "frame too short"}
	}

	frameType := data[0]
	channel := binary.BigEndian.Uint16(data[1:3])
	size := binary.BigEndian.Uint32(data[3:7])

	if len(data) < int(7+size+1) {
		return nil, &ErrAMQPProtocolError{Message: "frame payload incomplete"}
	}

	// Verify frame-end marker
	if data[7+size] != FrameEnd {
		return nil, &ErrAMQPProtocolError{Message: "invalid frame-end marker"}
	}

	payload := data[7 : 7+size]

	return &AMQPFrame{
		Type:    frameType,
		Channel: channel,
		Payload: payload,
	}, nil
}

// methodKey is used for method dispatch lookup.
type methodKey struct {
	classID  uint16
	methodID uint16
}

// methodHandler is a function type for handling AMQP methods.
type methodHandler func(context.Context, *AMQPFrame, []byte, *ws.Session, *amqpClientState) ([]byte, error)

// handleMethodFrame handles an AMQP method frame.
func (h *AMQPHandler) handleMethodFrame(ctx context.Context, frame *AMQPFrame, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(frame.Payload) < 4 {
		return nil, &ErrAMQPProtocolError{Message: "method frame too short"}
	}

	classID := binary.BigEndian.Uint16(frame.Payload[0:2])
	methodID := binary.BigEndian.Uint16(frame.Payload[2:4])
	args := frame.Payload[4:]

	// Method dispatch using map for O(1) lookup
	handlers := map[methodKey]methodHandler{
		// Connection class
		{ClassConnection, MethodConnectionStartOk}:  h.handleConnectionStartOk,
		{ClassConnection, MethodConnectionSecureOk}: h.handleConnectionSecureOk,
		{ClassConnection, MethodConnectionTuneOk}:   h.handleConnectionTuneOk,
		{ClassConnection, MethodConnectionOpen}:     h.handleConnectionOpen,
		{ClassConnection, MethodConnectionClose}:    h.handleConnectionClose,
		// Channel class
		{ClassChannel, MethodChannelOpen}:  h.handleChannelOpen,
		{ClassChannel, MethodChannelFlow}:  h.handleChannelFlow,
		{ClassChannel, MethodChannelClose}: h.handleChannelClose,
		// Exchange class
		{ClassExchange, MethodExchangeDeclare}: h.handleExchangeDeclare,
		{ClassExchange, MethodExchangeDelete}:  h.handleExchangeDelete,
		// Queue class
		{ClassQueue, MethodQueueDeclare}: h.handleQueueDeclare,
		{ClassQueue, MethodQueueBind}:    h.handleQueueBind,
		{ClassQueue, MethodQueueUnbind}:  h.handleQueueUnbind,
		{ClassQueue, MethodQueuePurge}:   h.handleQueuePurge,
		{ClassQueue, MethodQueueDelete}:  h.handleQueueDelete,
		// Basic class
		{ClassBasic, MethodBasicQos}:          h.handleBasicQos,
		{ClassBasic, MethodBasicConsume}:      h.handleBasicConsume,
		{ClassBasic, MethodBasicCancel}:       h.handleBasicCancel,
		{ClassBasic, MethodBasicPublish}:      h.handleBasicPublish,
		{ClassBasic, MethodBasicGet}:          h.handleBasicGet,
		{ClassBasic, MethodBasicAck}:          h.handleBasicAck,
		{ClassBasic, MethodBasicReject}:       h.handleBasicReject,
		{ClassBasic, MethodBasicRecoverAsync}: h.handleBasicRecoverAsync,
		{ClassBasic, MethodBasicRecover}:      h.handleBasicRecover,
		// Tx class
		{ClassTx, MethodTxSelect}:   h.handleTxSelect,
		{ClassTx, MethodTxCommit}:   h.handleTxCommit,
		{ClassTx, MethodTxRollback}: h.handleTxRollback,
	}

	key := methodKey{classID: classID, methodID: methodID}
	handler, ok := handlers[key]
	if !ok {
		return nil, &ErrAMQPProtocolError{Message: "unsupported method"}
	}

	return handler(ctx, frame, args, session, state)
}

// Connection class handlers

// handleConnectionStartOk handles Connection.Start-Ok.
func (h *AMQPHandler) handleConnectionStartOk(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse client properties and authentication (simplified)
	// Send Connection.Tune
	tuneArgs := &bytes.Buffer{}
	_ = binary.Write(tuneArgs, binary.BigEndian, uint16(2047))   // channel-max
	_ = binary.Write(tuneArgs, binary.BigEndian, uint32(131072)) // frame-max
	_ = binary.Write(tuneArgs, binary.BigEndian, uint16(60))     // heartbeat

	return h.buildMethodFrame(0, ClassConnection, MethodConnectionTune, tuneArgs.Bytes()), nil
}

// handleConnectionSecureOk handles Connection.Secure-Ok (SASL challenge response).
func (h *AMQPHandler) handleConnectionSecureOk(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// SASL authentication challenge/response
	// For PLAIN mechanism, just proceed to Tune
	tuneArgs := &bytes.Buffer{}
	_ = binary.Write(tuneArgs, binary.BigEndian, uint16(2047))   // channel-max
	_ = binary.Write(tuneArgs, binary.BigEndian, uint32(131072)) // frame-max
	_ = binary.Write(tuneArgs, binary.BigEndian, uint16(60))     // heartbeat

	return h.buildMethodFrame(0, ClassConnection, MethodConnectionTune, tuneArgs.Bytes()), nil
}

// handleConnectionTuneOk handles Connection.Tune-Ok.
func (h *AMQPHandler) handleConnectionTuneOk(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Client accepted tune parameters, waiting for Connection.Open
	return nil, nil
}

// handleConnectionOpen handles Connection.Open.
func (h *AMQPHandler) handleConnectionOpen(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse virtual host (shortstring)
	if len(args) < 1 {
		return nil, &ErrAMQPProtocolError{Message: "missing vhost"}
	}
	vhostLen := int(args[0])
	if len(args) < 1+vhostLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid vhost"}
	}
	vhost := string(args[1 : 1+vhostLen])
	// Skip reserved-1 (shortstring) and reserved-2 (bit)

	state.mu.Lock()
	state.vhost = vhost
	state.connected = true
	state.mu.Unlock()

	// Send Connection.Open-Ok with reserved-1 (shortstring, empty)
	openOkArgs := &bytes.Buffer{}
	writeShortString(openOkArgs, "") // reserved-1

	return h.buildMethodFrame(0, ClassConnection, MethodConnectionOpenOk, openOkArgs.Bytes()), nil
}

// handleConnectionClose handles Connection.Close.
func (h *AMQPHandler) handleConnectionClose(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse: reply-code (short), reply-text (shortstring), class-id (short), method-id (short)
	// Send Connection.Close-Ok (no fields)
	closeOk := h.buildMethodFrame(0, ClassConnection, MethodConnectionCloseOk, nil)
	return closeOk, ErrAMQPDisconnect
}

// Channel class handlers

// handleChannelOpen handles Channel.Open.
func (h *AMQPHandler) handleChannelOpen(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse reserved-1 (shortstring)
	state.mu.Lock()
	if state.channels[frame.Channel] == nil {
		state.channels[frame.Channel] = &amqpChannelState{
			open:            true,
			flowActive:      true, // channels start with flow active
			queues:          make(map[string]*amqpQueueState),
			exchanges:       make(map[string]*amqpExchangeState),
			unackedMessages: make(map[uint64]*amqpUnackedMessage),
		}
	}
	state.mu.Unlock()

	// Send Channel.Open-Ok with reserved-1 (longstring, empty)
	openOkArgs := &bytes.Buffer{}
	writeLongString(openOkArgs, "") // reserved-1

	return h.buildMethodFrame(frame.Channel, ClassChannel, MethodChannelOpenOk, openOkArgs.Bytes()), nil
}

// handleChannelFlow handles Channel.Flow.
func (h *AMQPHandler) handleChannelFlow(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse: active (bit)
	active := len(args) > 0 && (args[0]&0x01) != 0

	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		ch.flowActive = active
	}
	state.mu.Unlock()

	// Send Channel.Flow-Ok with active (bit)
	flowOkArgs := &bytes.Buffer{}
	if active {
		flowOkArgs.WriteByte(1)
	} else {
		flowOkArgs.WriteByte(0)
	}

	return h.buildMethodFrame(frame.Channel, ClassChannel, MethodChannelFlowOk, flowOkArgs.Bytes()), nil
}

// handleChannelClose handles Channel.Close.
func (h *AMQPHandler) handleChannelClose(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse: reply-code (short), reply-text (shortstring), class-id (short), method-id (short)
	state.mu.Lock()
	delete(state.channels, frame.Channel)
	state.mu.Unlock()

	// Send Channel.Close-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassChannel, MethodChannelCloseOk, nil), nil
}

// Exchange class handlers

// handleExchangeDeclare handles Exchange.Declare.
func (h *AMQPHandler) handleExchangeDeclare(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "exchange declare args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Exchange name (shortstring)
	exchangeNameLen := int(args[offset])
	offset++
	if len(args) < offset+exchangeNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange name"}
	}
	exchangeName := string(args[offset : offset+exchangeNameLen])
	offset += exchangeNameLen

	// Type (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing exchange type"}
	}
	typeLen := int(args[offset])
	offset++
	if len(args) < offset+typeLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange type"}
	}
	exchangeType := string(args[offset : offset+typeLen])
	offset += typeLen

	// Flags (1 byte): passive, durable, reserved-2, reserved-3, no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	passive := (flags & 0x01) != 0
	durable := (flags & 0x02) != 0
	noWait := (flags & 0x10) != 0

	// Store exchange state
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch == nil {
		state.mu.Unlock()
		return nil, &ErrAMQPProtocolError{Message: "channel not open"}
	}

	if passive {
		// Check if exchange exists
		if ch.exchanges[exchangeName] == nil {
			state.mu.Unlock()
			return h.buildChannelClose(frame.Channel, NotFound, "NOT_FOUND - no exchange '"+exchangeName+"'", ClassExchange, MethodExchangeDeclare), nil
		}
	} else {
		ch.exchanges[exchangeName] = &amqpExchangeState{
			name:    exchangeName,
			typ:     exchangeType,
			durable: durable,
		}
	}
	state.mu.Unlock()

	// Send Exchange.Declare-Ok if not no-wait
	if noWait {
		return nil, nil
	}
	return h.buildMethodFrame(frame.Channel, ClassExchange, MethodExchangeDeclareOk, nil), nil
}

// handleExchangeDelete handles Exchange.Delete.
func (h *AMQPHandler) handleExchangeDelete(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "exchange delete args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Exchange name (shortstring)
	exchangeNameLen := int(args[offset])
	offset++
	if len(args) < offset+exchangeNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange name"}
	}
	exchangeName := string(args[offset : offset+exchangeNameLen])
	offset += exchangeNameLen

	// Flags (1 byte): if-unused, no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noWait := (flags & 0x02) != 0

	// Delete exchange
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch == nil {
		state.mu.Unlock()
		return nil, &ErrAMQPProtocolError{Message: "channel not open"}
	}
	delete(ch.exchanges, exchangeName)
	state.mu.Unlock()

	// Send Exchange.Delete-Ok if not no-wait
	if noWait {
		return nil, nil
	}
	return h.buildMethodFrame(frame.Channel, ClassExchange, MethodExchangeDeleteOk, nil), nil
}

// Queue class handlers

// handleQueueDeclare handles Queue.Declare.
func (h *AMQPHandler) handleQueueDeclare(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "queue declare args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := string(args[offset : offset+queueNameLen])
	offset += queueNameLen

	// Flags (1 byte): passive, durable, exclusive, auto-delete, no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	passive := (flags & 0x01) != 0
	durable := (flags & 0x02) != 0
	exclusive := (flags & 0x04) != 0
	autoDelete := (flags & 0x08) != 0
	noWait := (flags & 0x10) != 0

	// Store queue state
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch == nil {
		state.mu.Unlock()
		return nil, &ErrAMQPProtocolError{Message: "channel not open"}
	}

	if passive {
		// Check if queue exists
		if ch.queues[queueName] == nil {
			state.mu.Unlock()
			return h.buildChannelClose(frame.Channel, NotFound, "NOT_FOUND - no queue '"+queueName+"'", ClassQueue, MethodQueueDeclare), nil
		}
	} else {
		ch.queues[queueName] = &amqpQueueState{
			name:       queueName,
			durable:    durable,
			exclusive:  exclusive,
			autoDelete: autoDelete,
			messages:   make([][]byte, 0),
			bindings:   make([]amqpBinding, 0),
		}
	}
	state.mu.Unlock()

	// Register queue as topic in hub
	if err := h.hub.Subscribe(session.ID, queueName, 0); err != nil {
		return nil, err
	}

	// Send Queue.Declare-Ok if not no-wait
	if noWait {
		return nil, nil
	}

	// Queue.Declare-Ok: queue (shortstring), message-count (long), consumer-count (long)
	declareOkArgs := &bytes.Buffer{}
	writeShortString(declareOkArgs, queueName)
	_ = binary.Write(declareOkArgs, binary.BigEndian, uint32(0)) // message-count
	_ = binary.Write(declareOkArgs, binary.BigEndian, uint32(0)) // consumer-count

	return h.buildMethodFrame(frame.Channel, ClassQueue, MethodQueueDeclareOk, declareOkArgs.Bytes()), nil
}

// handleQueueBind handles Queue.Bind.
func (h *AMQPHandler) handleQueueBind(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "queue bind args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := args[offset : offset+queueNameLen]
	offset += queueNameLen

	// Exchange name (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing exchange name"}
	}
	exchangeNameLen := int(args[offset])
	offset++
	if len(args) < offset+exchangeNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange name"}
	}
	exchangeName := string(args[offset : offset+exchangeNameLen])
	offset += exchangeNameLen

	// Routing key (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing routing key"}
	}
	routingKeyLen := int(args[offset])
	offset++
	routingKey := ""
	if len(args) >= offset+routingKeyLen {
		routingKey = string(args[offset : offset+routingKeyLen])
		offset += routingKeyLen
	}

	// Flags (1 byte): no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noWait := (flags & 0x01) != 0

	// Store binding
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		queue := ch.queues[string(queueName)]
		if queue != nil {
			queue.bindings = append(queue.bindings, amqpBinding{
				exchange:   exchangeName,
				routingKey: routingKey,
			})
		}
	}
	state.mu.Unlock()

	// Send Queue.Bind-Ok if not no-wait
	if noWait {
		return nil, nil
	}
	return h.buildMethodFrame(frame.Channel, ClassQueue, MethodQueueBindOk, nil), nil
}

// handleQueueUnbind handles Queue.Unbind.
func (h *AMQPHandler) handleQueueUnbind(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "queue unbind args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := args[offset : offset+queueNameLen]
	offset += queueNameLen

	// Exchange name (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing exchange name"}
	}
	exchangeNameLen := int(args[offset])
	offset++
	if len(args) < offset+exchangeNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange name"}
	}
	exchangeName := string(args[offset : offset+exchangeNameLen])
	offset += exchangeNameLen

	// Routing key (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing routing key"}
	}
	routingKeyLen := int(args[offset])
	offset++
	routingKey := ""
	if len(args) >= offset+routingKeyLen {
		routingKey = string(args[offset : offset+routingKeyLen])
	}

	// Remove binding
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		queue := ch.queues[string(queueName)]
		if queue != nil {
			newBindings := make([]amqpBinding, 0, len(queue.bindings))
			for _, b := range queue.bindings {
				if b.exchange != exchangeName || b.routingKey != routingKey {
					newBindings = append(newBindings, b)
				}
			}
			queue.bindings = newBindings
		}
	}
	state.mu.Unlock()

	// Send Queue.Unbind-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassQueue, MethodQueueUnbindOk, nil), nil
}

// handleQueuePurge handles Queue.Purge.
func (h *AMQPHandler) handleQueuePurge(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "queue purge args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := args[offset : offset+queueNameLen]
	offset += queueNameLen

	// Flags (1 byte): no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noWait := (flags & 0x01) != 0

	// Purge queue messages
	var messageCount uint32
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		queue := ch.queues[string(queueName)]
		if queue != nil {
			messageCount = uint32(len(queue.messages))
			queue.messages = make([][]byte, 0)
		}
	}
	state.mu.Unlock()

	// Send Queue.Purge-Ok if not no-wait
	if noWait {
		return nil, nil
	}

	// Queue.Purge-Ok: message-count (long)
	purgeOkArgs := &bytes.Buffer{}
	_ = binary.Write(purgeOkArgs, binary.BigEndian, messageCount)

	return h.buildMethodFrame(frame.Channel, ClassQueue, MethodQueuePurgeOk, purgeOkArgs.Bytes()), nil
}

// handleQueueDelete handles Queue.Delete.
func (h *AMQPHandler) handleQueueDelete(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "queue delete args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := string(args[offset : offset+queueNameLen])
	offset += queueNameLen

	// Flags (1 byte): if-unused, if-empty, no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noWait := (flags & 0x04) != 0

	// Delete queue
	var messageCount uint32
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		queue := ch.queues[queueName]
		if queue != nil {
			messageCount = uint32(len(queue.messages))
		}
		delete(ch.queues, queueName)
	}
	state.mu.Unlock()

	// Unsubscribe from hub
	if err := h.hub.Unsubscribe(session.ID, queueName); err != nil {
		return nil, err
	}

	// Send Queue.Delete-Ok if not no-wait
	if noWait {
		return nil, nil
	}

	// Queue.Delete-Ok: message-count (long)
	deleteOkArgs := &bytes.Buffer{}
	_ = binary.Write(deleteOkArgs, binary.BigEndian, messageCount)

	return h.buildMethodFrame(frame.Channel, ClassQueue, MethodQueueDeleteOk, deleteOkArgs.Bytes()), nil
}

// Basic class handlers

// handleBasicQos handles Basic.Qos.
func (h *AMQPHandler) handleBasicQos(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 7 {
		return nil, &ErrAMQPProtocolError{Message: "basic qos args too short"}
	}

	// Parse: prefetch-size (long), prefetch-count (short), global (bit)
	prefetchSize := binary.BigEndian.Uint32(args[0:4])
	prefetchCount := binary.BigEndian.Uint16(args[4:6])
	global := (args[6] & 0x01) != 0

	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		ch.prefetchSize = prefetchSize
		ch.prefetchCount = prefetchCount
		ch.prefetchGlobal = global
	}
	state.mu.Unlock()

	// Send Basic.Qos-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicQosOk, nil), nil
}

// handleBasicConsume handles Basic.Consume.
func (h *AMQPHandler) handleBasicConsume(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 4 {
		return nil, &ErrAMQPProtocolError{Message: "consume args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := string(args[offset : offset+queueNameLen])
	offset += queueNameLen

	// Consumer tag (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing consumer tag length"}
	}
	consumerTagLen := int(args[offset])
	offset++
	var consumerTag string
	if consumerTagLen > 0 {
		if len(args) < offset+consumerTagLen {
			return nil, &ErrAMQPProtocolError{Message: "invalid consumer tag"}
		}
		consumerTag = string(args[offset : offset+consumerTagLen])
	} else {
		// Generate consumer tag
		consumerTag = session.ID + "-" + queueName
	}
	offset += consumerTagLen

	// Flags (1 byte): no-local, no-ack, exclusive, no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noLocal := (flags & 0x01) != 0
	noAck := (flags & 0x02) != 0
	exclusive := (flags & 0x04) != 0
	noWait := (flags & 0x08) != 0

	// Register consumer
	state.mu.Lock()
	state.consumers[consumerTag] = amqpConsumer{
		queueName:   queueName,
		consumerTag: consumerTag,
		channel:     frame.Channel,
		noLocal:     noLocal,
		noAck:       noAck,
		exclusive:   exclusive,
	}
	state.mu.Unlock()

	// Subscribe to queue via hub
	if err := h.hub.Subscribe(session.ID, queueName, 0); err != nil {
		return nil, err
	}

	// Send Basic.Consume-Ok if not no-wait
	if noWait {
		return nil, nil
	}

	// Basic.Consume-Ok: consumer-tag (shortstring)
	consumeOkArgs := &bytes.Buffer{}
	writeShortString(consumeOkArgs, consumerTag)

	return h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicConsumeOk, consumeOkArgs.Bytes()), nil
}

// handleBasicCancel handles Basic.Cancel.
func (h *AMQPHandler) handleBasicCancel(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 1 {
		return nil, &ErrAMQPProtocolError{Message: "cancel args too short"}
	}

	// Consumer tag (shortstring)
	consumerTagLen := int(args[0])
	offset := 1
	if len(args) < offset+consumerTagLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid consumer tag"}
	}
	consumerTag := string(args[offset : offset+consumerTagLen])
	offset += consumerTagLen

	// Flags (1 byte): no-wait
	var flags byte
	if len(args) > offset {
		flags = args[offset]
	}
	noWait := (flags & 0x01) != 0

	// Remove consumer
	var unsubErr error
	state.mu.Lock()
	consumer, exists := state.consumers[consumerTag]
	if exists {
		delete(state.consumers, consumerTag)
		// Unsubscribe from queue if no other consumers
		hasOtherConsumer := false
		for _, c := range state.consumers {
			if c.queueName == consumer.queueName {
				hasOtherConsumer = true
				break
			}
		}
		if !hasOtherConsumer {
			unsubErr = h.hub.Unsubscribe(session.ID, consumer.queueName)
		}
	}
	state.mu.Unlock()

	if unsubErr != nil {
		return nil, unsubErr
	}

	// Send Basic.Cancel-Ok if not no-wait
	if noWait {
		return nil, nil
	}

	// Basic.Cancel-Ok: consumer-tag (shortstring)
	cancelOkArgs := &bytes.Buffer{}
	writeShortString(cancelOkArgs, consumerTag)

	return h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicCancelOk, cancelOkArgs.Bytes()), nil
}

// handleBasicPublish handles Basic.Publish.
func (h *AMQPHandler) handleBasicPublish(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 4 {
		return nil, &ErrAMQPProtocolError{Message: "publish args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Exchange name (shortstring)
	exchangeNameLen := int(args[offset])
	offset++
	if len(args) < offset+exchangeNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid exchange name"}
	}
	// Skip exchange name for now
	offset += exchangeNameLen

	// Routing key (shortstring)
	if len(args) < offset+1 {
		return nil, &ErrAMQPProtocolError{Message: "missing routing key length"}
	}
	routingKeyLen := int(args[offset])
	offset++
	if len(args) < offset+routingKeyLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid routing key"}
	}
	routingKey := string(args[offset : offset+routingKeyLen])

	// Flags (1 byte): mandatory, immediate - parsed but not used in this simplified implementation

	// Note: The actual content comes in Header and Body frames
	// For now, we'll publish an empty message; a full implementation
	// would accumulate Header+Body frames for the complete message
	if err := h.hub.Publish(routingKey, nil, 0, false); err != nil {
		return nil, err
	}

	// Basic.Publish doesn't have a response
	return nil, nil
}

// handleBasicGet handles Basic.Get.
func (h *AMQPHandler) handleBasicGet(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 3 {
		return nil, &ErrAMQPProtocolError{Message: "get args too short"}
	}

	// Skip reserved-1 (short, 2 bytes)
	offset := 2

	// Queue name (shortstring)
	queueNameLen := int(args[offset])
	offset++
	if len(args) < offset+queueNameLen {
		return nil, &ErrAMQPProtocolError{Message: "invalid queue name"}
	}
	queueName := string(args[offset : offset+queueNameLen])
	offset += queueNameLen

	// Flags (1 byte): no-ack
	var noAck bool
	if len(args) > offset {
		noAck = (args[offset] & 0x01) != 0
	}

	// Try to get a message from the queue
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	var message []byte
	var messageCount uint32
	if ch != nil {
		queue := ch.queues[queueName]
		if queue != nil && len(queue.messages) > 0 {
			message = queue.messages[0]
			queue.messages = queue.messages[1:]
			messageCount = uint32(len(queue.messages))

			if !noAck {
				ch.nextDeliveryTag++
				ch.unackedMessages[ch.nextDeliveryTag] = &amqpUnackedMessage{
					deliveryTag: ch.nextDeliveryTag,
					queue:       queueName,
				}
			}
		}
	}
	deliveryTag := uint64(0)
	if ch != nil {
		deliveryTag = ch.nextDeliveryTag
	}
	state.mu.Unlock()

	if message == nil {
		// Send Basic.Get-Empty with reserved-1 (shortstring, empty)
		getEmptyArgs := &bytes.Buffer{}
		writeShortString(getEmptyArgs, "") // reserved-1
		return h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicGetEmpty, getEmptyArgs.Bytes()), nil
	}

	// Send Basic.Get-Ok: delivery-tag (longlong), redelivered (bit), exchange (shortstring), routing-key (shortstring), message-count (long)
	getOkArgs := &bytes.Buffer{}
	_ = binary.Write(getOkArgs, binary.BigEndian, deliveryTag)
	getOkArgs.WriteByte(0)                 // redelivered flag, always false here
	writeShortString(getOkArgs, "")        // exchange
	writeShortString(getOkArgs, queueName) // routing-key
	_ = binary.Write(getOkArgs, binary.BigEndian, messageCount)

	methodFrame := h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicGetOk, getOkArgs.Bytes())

	// Build content header and body frames
	if len(message) > 0 {
		headerPayload := &bytes.Buffer{}
		_ = binary.Write(headerPayload, binary.BigEndian, ClassBasic)           // class-id
		_ = binary.Write(headerPayload, binary.BigEndian, uint16(0))            // weight (reserved)
		_ = binary.Write(headerPayload, binary.BigEndian, uint64(len(message))) // body-size
		_ = binary.Write(headerPayload, binary.BigEndian, uint16(0))            // property-flags (no properties)
		headerFrame := h.buildFrame(FrameHeader, frame.Channel, headerPayload.Bytes())

		bodyFrame := h.buildFrame(FrameBody, frame.Channel, message)

		result := make([]byte, 0, len(methodFrame)+len(headerFrame)+len(bodyFrame))
		result = append(result, methodFrame...)
		result = append(result, headerFrame...)
		result = append(result, bodyFrame...)
		return result, nil
	}

	return methodFrame, nil
}

// handleBasicAck handles Basic.Ack.
func (h *AMQPHandler) handleBasicAck(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 9 {
		return nil, &ErrAMQPProtocolError{Message: "ack args too short"}
	}

	// Parse: delivery-tag (longlong), multiple (bit)
	deliveryTag := binary.BigEndian.Uint64(args[0:8])
	multiple := (args[8] & 0x01) != 0

	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		if multiple {
			// Acknowledge all messages up to and including delivery-tag
			for tag := range ch.unackedMessages {
				if tag <= deliveryTag {
					delete(ch.unackedMessages, tag)
				}
			}
		} else {
			delete(ch.unackedMessages, deliveryTag)
		}
	}
	state.mu.Unlock()

	// Basic.Ack has no response
	return nil, nil
}

// handleBasicReject handles Basic.Reject.
func (h *AMQPHandler) handleBasicReject(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	if len(args) < 9 {
		return nil, &ErrAMQPProtocolError{Message: "reject args too short"}
	}

	// Parse: delivery-tag (longlong). The requeue bit is parsed by the
	// caller's frame but not honoured: this implementation does not retain
	// message content, so a rejected message is dropped from the unacked set
	// either way.
	deliveryTag := binary.BigEndian.Uint64(args[0:8])

	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		delete(ch.unackedMessages, deliveryTag)
	}
	state.mu.Unlock()

	// Basic.Reject has no response
	return nil, nil
}

// handleBasicRecoverAsync handles Basic.Recover-Async (deprecated).
func (h *AMQPHandler) handleBasicRecoverAsync(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse: requeue (bit)
	// This is deprecated, just acknowledge receipt
	// No response for async version
	return nil, nil
}

// handleBasicRecover handles Basic.Recover.
func (h *AMQPHandler) handleBasicRecover(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	// Parse: requeue (bit)
	// In a full implementation, we'd redeliver unacknowledged messages

	// Send Basic.Recover-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassBasic, MethodBasicRecoverOk, nil), nil
}

// Tx class handlers

// handleTxSelect handles Tx.Select.
func (h *AMQPHandler) handleTxSelect(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	state.mu.Lock()
	ch := state.channels[frame.Channel]
	if ch != nil {
		ch.transacted = true
	}
	state.mu.Unlock()

	// Send Tx.Select-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassTx, MethodTxSelectOk, nil), nil
}

// handleTxCommit handles Tx.Commit.
func (h *AMQPHandler) handleTxCommit(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	state.mu.RLock()
	ch := state.channels[frame.Channel]
	transacted := ch != nil && ch.transacted
	state.mu.RUnlock()

	if !transacted {
		return h.buildChannelClose(frame.Channel, PreconditionFailed, "channel is not transacted", ClassTx, MethodTxCommit), nil
	}

	// In a full implementation, commit pending operations

	// Send Tx.Commit-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassTx, MethodTxCommitOk, nil), nil
}

// handleTxRollback handles Tx.Rollback.
func (h *AMQPHandler) handleTxRollback(ctx context.Context, frame *AMQPFrame, args []byte, session *ws.Session, state *amqpClientState) ([]byte, error) {
	state.mu.RLock()
	ch := state.channels[frame.Channel]
	transacted := ch != nil && ch.transacted
	state.mu.RUnlock()

	if !transacted {
		return h.buildChannelClose(frame.Channel, PreconditionFailed, "channel is not transacted", ClassTx, MethodTxRollback), nil
	}

	// In a full implementation, rollback pending operations

	// Send Tx.Rollback-Ok (no fields)
	return h.buildMethodFrame(frame.Channel, ClassTx, MethodTxRollbackOk, nil), nil
}

// handleHeartbeat handles heartbeat frames.
func (h *AMQPHandler) handleHeartbeat(frame *AMQPFrame) ([]byte, error) {
	// Respond with heartbeat
	return h.buildFrame(FrameHeartbeat, 0, nil), nil
}

// buildChannelClose builds a Channel.Close method frame.
func (h *AMQPHandler) buildChannelClose(channel uint16, replyCode uint16, replyText string, classID, methodID uint16) []byte {
	args := &bytes.Buffer{}
	_ = binary.Write(args, binary.BigEndian, replyCode)
	writeShortString(args, replyText)
	_ = binary.Write(args, binary.BigEndian, classID)
	_ = binary.Write(args, binary.BigEndian, methodID)
	return h.buildMethodFrame(channel, ClassChannel, MethodChannelClose, args.Bytes())
}

// buildBasicDeliver builds a Basic.Deliver method frame with content.
func (h *AMQPHandler) buildBasicDeliver(channel uint16, consumerTag string, deliveryTag uint64, routingKey string, payload []byte) []byte {
	// Build method frame
	// Basic.Deliver: consumer-tag (shortstring), delivery-tag (longlong), redelivered (bit), exchange (shortstring), routing-key (shortstring)
	methodArgs := &bytes.Buffer{}
	writeShortString(methodArgs, consumerTag)
	_ = binary.Write(methodArgs, binary.BigEndian, deliveryTag)
	methodArgs.WriteByte(0)          // redelivered flag, always false here
	writeShortString(methodArgs, "") // exchange
	writeShortString(methodArgs, routingKey)

	methodFrame := h.buildMethodFrame(channel, ClassBasic, MethodBasicDeliver, methodArgs.Bytes())

	if len(payload) == 0 {
		return methodFrame
	}

	// Build content header frame
	headerPayload := &bytes.Buffer{}
	_ = binary.Write(headerPayload, binary.BigEndian, ClassBasic)           // class-id
	_ = binary.Write(headerPayload, binary.BigEndian, uint16(0))            // weight (reserved)
	_ = binary.Write(headerPayload, binary.BigEndian, uint64(len(payload))) // body-size
	_ = binary.Write(headerPayload, binary.BigEndian, uint16(0))            // property-flags (no properties)
	headerFrame := h.buildFrame(FrameHeader, channel, headerPayload.Bytes())

	// Build content body frame
	bodyFrame := h.buildFrame(FrameBody, channel, payload)

	// Concatenate all frames
	result := make([]byte, 0, len(methodFrame)+len(headerFrame)+len(bodyFrame))
	result = append(result, methodFrame...)
	result = append(result, headerFrame...)
	result = append(result, bodyFrame...)

	return result
}

// MessagesReceived returns the total number of messages received.
func (h *AMQPHandler) MessagesReceived() uint64 {
	return h.messagesReceived.Load()
}

// MessagesSent returns the total number of messages sent.
func (h *AMQPHandler) MessagesSent() uint64 {
	return h.messagesSent.Load()
}

// ResetCounters resets the message counters.
func (h *AMQPHandler) ResetCounters() {
	h.messagesReceived.Store(0)
	h.messagesSent.Store(0)
}

// Hub returns the underlying pub/sub hub.
func (h *AMQPHandler) Hub() PubSubHub {
	return h.hub
}

// ClientCount returns the number of connected clients.
func (h *AMQPHandler) ClientCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

// Helper functions for AMQP encoding

// writeShortString writes a short string (length byte + string bytes).
func writeShortString(buf *bytes.Buffer, s string) {
	if len(s) > 255 {
		s = s[:255]
	}
	buf.WriteByte(byte(len(s)))
	buf.WriteString(s)
}

// writeLongString writes a long string (4-byte length + string bytes).
func writeLongString(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.BigEndian, uint32(len(s)))
	buf.WriteString(s)
}

// writeTable writes an AMQP field table.
func writeTable(buf *bytes.Buffer, table map[string]interface{}) {
	tableBuf := &bytes.Buffer{}
	for key, value := range table {
		writeShortString(tableBuf, key)
		switch v := value.(type) {
		case string:
			tableBuf.WriteByte('S') // Long string type
			writeLongString(tableBuf, v)
		case int:
			tableBuf.WriteByte('I') // Signed 32-bit integer
			_ = binary.Write(tableBuf, binary.BigEndian, int32(v))
		case int32:
			tableBuf.WriteByte('I') // Signed 32-bit integer
			_ = binary.Write(tableBuf, binary.BigEndian, v)
		case int64:
			tableBuf.WriteByte('l') // Signed 64-bit integer
			_ = binary.Write(tableBuf, binary.BigEndian, v)
		case bool:
			tableBuf.WriteByte('t') // Boolean
			if v {
				tableBuf.WriteByte(1)
			} else {
				tableBuf.WriteByte(0)
			}
		case map[string]interface{}:
			tableBuf.WriteByte('F') // Nested table
			writeTable(tableBuf, v)
		default:
			// Skip unknown types
		}
	}
	_ = binary.Write(buf, binary.BigEndian, uint32(tableBuf.Len()))
	buf.Write(tableBuf.Bytes())
}
