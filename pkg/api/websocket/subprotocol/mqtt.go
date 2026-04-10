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

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/mochi-mqtt/server/v2/packets"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// MQTTHandler implements the "mqtt" subprotocol for MQTT over WebSocket.
// It supports both MQTT 3.1.1 (protocol version 4) and MQTT 5.0 (protocol version 5).
type MQTTHandler struct {
	// hub manages topic subscriptions and message distribution.
	hub PubSubHub

	// messagesReceived tracks the total number of messages received.
	messagesReceived atomic.Uint64

	// messagesSent tracks the total number of messages sent.
	messagesSent atomic.Uint64

	// clientsMu protects the clients map.
	clientsMu sync.RWMutex

	// clients maps session IDs to MQTT client state.
	clients map[string]*mqttClientState
}

// mqttClientState holds the state for an MQTT client connection.
type mqttClientState struct {
	clientID        string
	protocolVersion byte
	keepalive       uint16
	cleanSession    bool
	subscriptions   map[string]byte // topic filter -> QoS
	mu              sync.RWMutex
}

// NewMQTTHandler creates a new MQTT subprotocol handler.
// If hub is nil, an in-memory pub/sub hub is created.
func NewMQTTHandler(hub PubSubHub) *MQTTHandler {
	if hub == nil {
		hub = NewInMemoryPubSubHub()
	}
	return &MQTTHandler{
		hub:     hub,
		clients: make(map[string]*mqttClientState),
	}
}

// Name returns the subprotocol name.
func (h *MQTTHandler) Name() string {
	return "mqtt"
}

// HandleConnection handles a WebSocket connection using the MQTT protocol.
func (h *MQTTHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	clientState := &mqttClientState{
		subscriptions: make(map[string]byte),
	}

	// Message delivery channel for this client
	deliveryChan := make(chan mqttDelivery, 100)
	defer close(deliveryChan)

	// Register cleanup on connection close
	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, session.ID)
		h.clientsMu.Unlock()

		if clientState.clientID != "" {
			h.hub.UnregisterClient(clientState.clientID)
		}
	}()

	// Start message delivery goroutine
	deliveryCtx, cancelDelivery := context.WithCancel(ctx)
	defer cancelDelivery()

	go h.deliverMessages(deliveryCtx, conn, clientState, deliveryChan)

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

		// Parse and handle MQTT packet
		response, err := h.handlePacket(ctx, data, session, clientState, deliveryChan)
		if err != nil {
			if errors.Is(err, ErrMQTTDisconnect) {
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

// mqttDelivery represents a message to be delivered to a client.
type mqttDelivery struct {
	topic   string
	payload []byte
	qos     byte
	retain  bool
}

// deliverMessages handles outbound message delivery to the client.
func (h *MQTTHandler) deliverMessages(ctx context.Context, conn *websocket.Conn, state *mqttClientState, deliveryChan <-chan mqttDelivery) {
	var packetID uint16

	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveryChan:
			if !ok {
				return
			}

			// Build PUBLISH packet
			packetID++
			if packetID == 0 {
				packetID = 1
			}

			pk := packets.Packet{
				FixedHeader: packets.FixedHeader{
					Type:   packets.Publish,
					Qos:    delivery.qos,
					Retain: delivery.retain,
				},
				TopicName:       delivery.topic,
				Payload:         delivery.payload,
				PacketID:        packetID,
				ProtocolVersion: state.protocolVersion,
			}

			buf := &bytes.Buffer{}
			if err := pk.PublishEncode(buf); err != nil {
				continue
			}

			if err := conn.Write(ctx, websocket.MessageBinary, buf.Bytes()); err != nil {
				return
			}
			h.messagesSent.Add(1)
		}
	}
}

// ErrMQTTDisconnect indicates a clean MQTT disconnect.
var ErrMQTTDisconnect = errors.New("mqtt: client disconnected")

// ErrMQTTProtocolError indicates an MQTT protocol violation.
type ErrMQTTProtocolError struct {
	Message string
}

func (e *ErrMQTTProtocolError) Error() string {
	return "mqtt protocol error: " + e.Message
}

// handlePacket parses and handles an MQTT packet.
func (h *MQTTHandler) handlePacket(ctx context.Context, data []byte, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	if len(data) < 2 {
		return nil, &ErrMQTTProtocolError{Message: "packet too short"}
	}

	// Decode fixed header
	fh := packets.FixedHeader{}
	if err := fh.Decode(data[0]); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}

	// Decode remaining length
	buf := bytes.NewBuffer(data[1:])
	remaining, _, err := packets.DecodeLength(buf)
	if err != nil {
		return nil, &ErrMQTTProtocolError{Message: "invalid remaining length"}
	}
	fh.Remaining = remaining

	// Get payload bytes (skip fixed header)
	headerLen := len(data) - buf.Len()
	payload := data[headerLen:]

	// Create packet and set protocol version from client state
	pk := packets.Packet{
		FixedHeader:     fh,
		ProtocolVersion: state.protocolVersion,
	}

	// Dispatch based on packet type
	handlers := map[byte]func(context.Context, []byte, *packets.Packet, *ws.Session, *mqttClientState, chan<- mqttDelivery) ([]byte, error){
		packets.Connect:     h.handleConnect,
		packets.Publish:     h.handlePublish,
		packets.Subscribe:   h.handleSubscribe,
		packets.Unsubscribe: h.handleUnsubscribe,
		packets.Pingreq:     h.handlePingreq,
		packets.Disconnect:  h.handleDisconnect,
		packets.Puback:      h.handlePuback,
		packets.Pubrec:      h.handlePubrec,
		packets.Pubrel:      h.handlePubrel,
		packets.Pubcomp:     h.handlePubcomp,
	}

	handler, ok := handlers[fh.Type]
	if !ok {
		return nil, &ErrMQTTProtocolError{Message: "unsupported packet type"}
	}

	return handler(ctx, payload, &pk, session, state, deliveryChan)
}

// handleConnect processes a CONNECT packet.
func (h *MQTTHandler) handleConnect(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	if err := pk.ConnectDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}

	// Validate connect packet
	code := pk.ConnectValidate()
	if code.Code != packets.CodeSuccess.Code {
		return h.buildConnack(pk.ProtocolVersion, false, code.Code), nil
	}

	// Update client state
	state.clientID = pk.Connect.ClientIdentifier
	state.protocolVersion = pk.ProtocolVersion
	state.keepalive = pk.Connect.Keepalive
	state.cleanSession = pk.Connect.Clean

	// Register with hub
	h.hub.RegisterClient(state.clientID, func(topic string, payload []byte, qos byte, retain bool) {
		select {
		case deliveryChan <- mqttDelivery{
			topic:   topic,
			payload: payload,
			qos:     qos,
			retain:  retain,
		}:
		default:
			// Channel full, drop message
		}
	})

	// Store client state
	h.clientsMu.Lock()
	h.clients[session.ID] = state
	h.clientsMu.Unlock()

	return h.buildConnack(pk.ProtocolVersion, false, packets.CodeSuccess.Code), nil
}

// buildConnack builds a CONNACK packet.
func (h *MQTTHandler) buildConnack(protocolVersion byte, sessionPresent bool, reasonCode byte) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Connack,
		},
		ProtocolVersion: protocolVersion,
		SessionPresent:  sessionPresent,
		ReasonCode:      reasonCode,
	}

	buf := &bytes.Buffer{}
	pk.ConnackEncode(buf)
	return buf.Bytes()
}

// handlePublish processes a PUBLISH packet.
func (h *MQTTHandler) handlePublish(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion

	if err := pk.PublishDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}

	// Publish to hub
	h.hub.Publish(pk.TopicName, pk.Payload, pk.FixedHeader.Qos, pk.FixedHeader.Retain)

	// Send acknowledgment based on QoS
	switch pk.FixedHeader.Qos {
	case 1:
		return h.buildPuback(state.protocolVersion, pk.PacketID), nil
	case 2:
		return h.buildPubrec(state.protocolVersion, pk.PacketID), nil
	}

	return nil, nil
}

// buildPuback builds a PUBACK packet.
func (h *MQTTHandler) buildPuback(protocolVersion byte, packetID uint16) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Puback,
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
	}

	buf := &bytes.Buffer{}
	pk.PubackEncode(buf)
	return buf.Bytes()
}

// buildPubrec builds a PUBREC packet.
func (h *MQTTHandler) buildPubrec(protocolVersion byte, packetID uint16) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pubrec,
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
	}

	buf := &bytes.Buffer{}
	pk.PubrecEncode(buf)
	return buf.Bytes()
}

// handleSubscribe processes a SUBSCRIBE packet.
func (h *MQTTHandler) handleSubscribe(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion

	if err := pk.SubscribeDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}

	// Process subscriptions
	reasonCodes := make([]byte, len(pk.Filters))
	for i, filter := range pk.Filters {
		if err := h.hub.Subscribe(state.clientID, filter.Filter, filter.Qos); err != nil {
			reasonCodes[i] = 0x80 // Unspecified error
		} else {
			reasonCodes[i] = filter.Qos // Granted QoS

			// Update local state
			state.mu.Lock()
			state.subscriptions[filter.Filter] = filter.Qos
			state.mu.Unlock()

			// Update session subscriptions
			session.Subscribe(filter.Filter)
		}
	}

	return h.buildSuback(state.protocolVersion, pk.PacketID, reasonCodes), nil
}

// buildSuback builds a SUBACK packet.
func (h *MQTTHandler) buildSuback(protocolVersion byte, packetID uint16, reasonCodes []byte) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Suback,
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
		ReasonCodes:     reasonCodes,
	}

	buf := &bytes.Buffer{}
	pk.SubackEncode(buf)
	return buf.Bytes()
}

// handleUnsubscribe processes an UNSUBSCRIBE packet.
func (h *MQTTHandler) handleUnsubscribe(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion

	if err := pk.UnsubscribeDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}

	// Process unsubscriptions
	reasonCodes := make([]byte, len(pk.Filters))
	for i, filter := range pk.Filters {
		state.mu.Lock()
		_, exists := state.subscriptions[filter.Filter]
		if exists {
			delete(state.subscriptions, filter.Filter)
		}
		state.mu.Unlock()

		if err := h.hub.Unsubscribe(state.clientID, filter.Filter); err != nil {
			reasonCodes[i] = 0x80 // Unspecified error
		} else if exists {
			reasonCodes[i] = 0x00 // Success
		} else {
			reasonCodes[i] = 0x11 // No subscription existed
		}

		session.Unsubscribe(filter.Filter)
	}

	return h.buildUnsuback(state.protocolVersion, pk.PacketID, reasonCodes), nil
}

// buildUnsuback builds an UNSUBACK packet.
func (h *MQTTHandler) buildUnsuback(protocolVersion byte, packetID uint16, reasonCodes []byte) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsuback,
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
		ReasonCodes:     reasonCodes,
	}

	buf := &bytes.Buffer{}
	pk.UnsubackEncode(buf)
	return buf.Bytes()
}

// handlePingreq processes a PINGREQ packet.
func (h *MQTTHandler) handlePingreq(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	return h.buildPingresp(), nil
}

// buildPingresp builds a PINGRESP packet.
func (h *MQTTHandler) buildPingresp() []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pingresp,
		},
	}

	buf := &bytes.Buffer{}
	pk.PingrespEncode(buf)
	return buf.Bytes()
}

// handleDisconnect processes a DISCONNECT packet.
func (h *MQTTHandler) handleDisconnect(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	return nil, ErrMQTTDisconnect
}

// handlePuback processes a PUBACK packet (QoS 1 acknowledgment from client).
func (h *MQTTHandler) handlePuback(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion
	if err := pk.PubackDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}
	// In a full implementation, we would mark the message as acknowledged
	return nil, nil
}

// handlePubrec processes a PUBREC packet (QoS 2 step 1 acknowledgment).
func (h *MQTTHandler) handlePubrec(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion
	if err := pk.PubrecDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}
	// Send PUBREL
	return h.buildPubrel(state.protocolVersion, pk.PacketID), nil
}

// buildPubrel builds a PUBREL packet.
func (h *MQTTHandler) buildPubrel(protocolVersion byte, packetID uint16) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pubrel,
			Qos:  1, // PUBREL has QoS 1 in fixed header
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
	}

	buf := &bytes.Buffer{}
	pk.PubrelEncode(buf)
	return buf.Bytes()
}

// handlePubrel processes a PUBREL packet (QoS 2 step 2).
func (h *MQTTHandler) handlePubrel(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion
	if err := pk.PubrelDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}
	// Send PUBCOMP
	return h.buildPubcomp(state.protocolVersion, pk.PacketID), nil
}

// buildPubcomp builds a PUBCOMP packet.
func (h *MQTTHandler) buildPubcomp(protocolVersion byte, packetID uint16) []byte {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pubcomp,
		},
		ProtocolVersion: protocolVersion,
		PacketID:        packetID,
	}

	buf := &bytes.Buffer{}
	pk.PubcompEncode(buf)
	return buf.Bytes()
}

// handlePubcomp processes a PUBCOMP packet (QoS 2 complete).
func (h *MQTTHandler) handlePubcomp(ctx context.Context, payload []byte, pk *packets.Packet, session *ws.Session, state *mqttClientState, deliveryChan chan<- mqttDelivery) ([]byte, error) {
	pk.ProtocolVersion = state.protocolVersion
	if err := pk.PubcompDecode(payload); err != nil {
		return nil, &ErrMQTTProtocolError{Message: err.Error()}
	}
	// In a full implementation, we would mark the QoS 2 flow as complete
	return nil, nil
}

// MessagesReceived returns the total number of messages received.
func (h *MQTTHandler) MessagesReceived() uint64 {
	return h.messagesReceived.Load()
}

// MessagesSent returns the total number of messages sent.
func (h *MQTTHandler) MessagesSent() uint64 {
	return h.messagesSent.Load()
}

// ResetCounters resets the message counters.
func (h *MQTTHandler) ResetCounters() {
	h.messagesReceived.Store(0)
	h.messagesSent.Store(0)
}

// Hub returns the underlying pub/sub hub.
func (h *MQTTHandler) Hub() PubSubHub {
	return h.hub
}

// ClientCount returns the number of connected clients.
func (h *MQTTHandler) ClientCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}
