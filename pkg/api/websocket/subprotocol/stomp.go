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

//go:build quicbaas_stomp

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// STOMP commands (client to server).
const (
	CmdConnect     = "CONNECT"
	CmdStomp       = "STOMP" // STOMP 1.2 alternative to CONNECT
	CmdSubscribe   = "SUBSCRIBE"
	CmdUnsubscribe = "UNSUBSCRIBE"
	CmdSend        = "SEND"
	CmdAck         = "ACK"
	CmdNack        = "NACK"
	CmdDisconnect  = "DISCONNECT"
	CmdBegin       = "BEGIN"
	CmdCommit      = "COMMIT"
	CmdAbort       = "ABORT"
)

// STOMP server frames (server to client).
const (
	STOMPFrameConnected = "CONNECTED"
	STOMPFrameMessage   = "MESSAGE"
	STOMPFrameReceipt   = "RECEIPT"
	STOMPFrameError     = "ERROR"
)

// STOMP header keys.
const (
	HeaderAcceptVersion = "accept-version"
	HeaderVersion       = "version"
	HeaderHost          = "host"
	HeaderLogin         = "login"
	HeaderPasscode      = "passcode"
	HeaderSession       = "session"
	HeaderServer        = "server"
	HeaderHeartBeat     = "heart-beat"
	HeaderDestination   = "destination"
	HeaderID            = "id"
	HeaderAck           = "ack"
	HeaderMessageID     = "message-id"
	HeaderSubscription  = "subscription"
	HeaderContentType   = "content-type"
	HeaderContentLength = "content-length"
	HeaderReceipt       = "receipt"
	HeaderReceiptID     = "receipt-id"
	HeaderMessage       = "message"
	HeaderTransaction   = "transaction"
)

// ACK modes per STOMP 1.2 specification.
const (
	AckModeAuto             = "auto"
	AckModeClient           = "client"
	AckModeClientIndividual = "client-individual"
)

// STOMPFrame represents a parsed STOMP frame.
type STOMPFrame struct {
	Command string
	Headers map[string]string
	Body    []byte
}

// STOMPHandler implements the "stomp" subprotocol for STOMP 1.2 over WebSocket.
type STOMPHandler struct {
	// hub manages topic subscriptions and message distribution.
	hub PubSubHub

	// messagesReceived tracks the total number of messages received.
	messagesReceived atomic.Uint64

	// messagesSent tracks the total number of messages sent.
	messagesSent atomic.Uint64

	// clientsMu protects the clients map.
	clientsMu sync.RWMutex

	// clients maps session IDs to STOMP client state.
	clients map[string]*stompClientState
}

// stompClientState holds the state for a STOMP client connection.
type stompClientState struct {
	connected     bool
	version       string
	session       string
	subscriptions map[string]stompSubscription // subscription ID -> subscription
	transactions  map[string]*stompTransaction // transaction ID -> transaction
	pendingAcks   map[string]*pendingMessage   // ack ID -> pending message
	mu            sync.RWMutex
	ackCounter    uint64
}

// stompSubscription represents a STOMP subscription.
type stompSubscription struct {
	id          string
	destination string
	ackMode     string // auto, client, client-individual
}

// stompTransaction represents a STOMP transaction.
type stompTransaction struct {
	id       string
	messages []transactionMessage
	acks     []string
	nacks    []string
}

// transactionMessage represents a message queued in a transaction.
type transactionMessage struct {
	destination string
	body        []byte
	contentType string
	headers     map[string]string
}

// pendingMessage represents a message awaiting acknowledgment.
type pendingMessage struct {
	ackID          string
	messageID      string
	subscriptionID string
	destination    string
}

// stompDelivery represents a message to be delivered to a client.
type stompDelivery struct {
	destination    string
	subscriptionID string
	payload        []byte
	contentType    string
	ackMode        string
}

// ErrSTOMPDisconnect indicates a clean STOMP disconnect.
var ErrSTOMPDisconnect = errors.New("stomp: client disconnected")

// ErrSTOMPProtocolError represents a STOMP protocol violation.
type ErrSTOMPProtocolError struct {
	Message string
}

func (e *ErrSTOMPProtocolError) Error() string {
	return "stomp protocol error: " + e.Message
}

// STOMPMissingHeaderError indicates a required header is missing.
type STOMPMissingHeaderError struct {
	Header string
}

func (e *STOMPMissingHeaderError) Error() string {
	return "stomp protocol error: missing required header: " + e.Header
}

// STOMPInvalidEscapeError indicates an invalid escape sequence was encountered.
type STOMPInvalidEscapeError struct {
	Sequence string
}

func (e *STOMPInvalidEscapeError) Error() string {
	return "stomp protocol error: undefined escape sequence: " + e.Sequence
}

// STOMPInvalidTransactionError indicates a transaction error.
type STOMPInvalidTransactionError struct {
	TransactionID string
	Reason        string
}

func (e *STOMPInvalidTransactionError) Error() string {
	return "stomp protocol error: transaction " + e.TransactionID + ": " + e.Reason
}

// NewSTOMPHandler creates a new STOMP subprotocol handler.
// If hub is nil, an in-memory pub/sub hub is created.
func NewSTOMPHandler(hub PubSubHub) *STOMPHandler {
	if hub == nil {
		hub = NewInMemoryPubSubHub()
	}
	return &STOMPHandler{
		hub:     hub,
		clients: make(map[string]*stompClientState),
	}
}

// Name returns the subprotocol name.
func (h *STOMPHandler) Name() string {
	return "stomp"
}

// HandleConnection handles a WebSocket connection using the STOMP 1.2 protocol.
func (h *STOMPHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	clientState := &stompClientState{
		subscriptions: make(map[string]stompSubscription),
		transactions:  make(map[string]*stompTransaction),
		pendingAcks:   make(map[string]*pendingMessage),
	}

	// Message delivery channel for this client. Hub callbacks send on it
	// outside the hub lock, so one may still be sending after
	// UnregisterClient returns: never close it. cancelDelivery stops the
	// delivery goroutine, and the callbacks never block.
	deliveryChan := make(chan stompDelivery, 100)

	// Register cleanup on connection close
	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, session.ID)
		h.clientsMu.Unlock()

		// Implicitly abort any uncommitted transactions per spec
		clientState.mu.Lock()
		clientState.transactions = make(map[string]*stompTransaction)
		clientState.mu.Unlock()

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
		clientState.mu.RLock()
		// Find subscription ID and ack mode for this destination
		var subID, ackMode string
		for id, sub := range clientState.subscriptions {
			if sub.destination == topic {
				subID = id
				ackMode = sub.ackMode
				break
			}
		}
		clientState.mu.RUnlock()

		if subID == "" {
			return
		}

		select {
		case deliveryChan <- stompDelivery{
			destination:    topic,
			subscriptionID: subID,
			payload:        payload,
			ackMode:        ackMode,
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

		// Parse and handle STOMP frame
		response, err := h.handleFrame(ctx, data, session, clientState)

		// Send response if any (even if there's an error, for DISCONNECT with receipt)
		if response != nil {
			if writeErr := conn.Write(ctx, websocket.MessageText, response); writeErr != nil {
				return writeErr
			}
			h.messagesSent.Add(1)
		}

		// Handle errors after sending response
		if err != nil {
			if errors.Is(err, ErrSTOMPDisconnect) {
				return nil
			}
			// Send ERROR frame for protocol errors
			var protoErr *ErrSTOMPProtocolError
			var missingErr *STOMPMissingHeaderError
			var escapeErr *STOMPInvalidEscapeError
			var txErr *STOMPInvalidTransactionError

			var errMsg string
			switch {
			case errors.As(err, &protoErr):
				errMsg = protoErr.Message
			case errors.As(err, &missingErr):
				errMsg = "missing required header: " + missingErr.Header
			case errors.As(err, &escapeErr):
				errMsg = "undefined escape sequence: " + escapeErr.Sequence
			case errors.As(err, &txErr):
				errMsg = "transaction error: " + txErr.Reason
			default:
				errMsg = err.Error()
			}

			errorFrame := h.buildErrorFrame(errMsg, "", nil)
			writeErr := conn.Write(ctx, websocket.MessageText, errorFrame)
			if writeErr == nil {
				h.messagesSent.Add(1)
			}
			return err
		}
	}
}

// deliverMessages handles outbound message delivery to the client.
func (h *STOMPHandler) deliverMessages(ctx context.Context, conn *websocket.Conn, state *stompClientState, deliveryChan <-chan stompDelivery) {
	var messageID uint64

	for {
		select {
		case <-ctx.Done():
			return
		case delivery := <-deliveryChan:

			messageID++
			msgIDStr := strconv.FormatUint(messageID, 10)

			// Generate ack ID for client/client-individual modes
			var ackID string
			if delivery.ackMode == AckModeClient || delivery.ackMode == AckModeClientIndividual {
				state.mu.Lock()
				state.ackCounter++
				ackID = strconv.FormatUint(state.ackCounter, 10)
				state.pendingAcks[ackID] = &pendingMessage{
					ackID:          ackID,
					messageID:      msgIDStr,
					subscriptionID: delivery.subscriptionID,
					destination:    delivery.destination,
				}
				state.mu.Unlock()
			}

			// Build MESSAGE frame
			messageFrame := h.buildMessageFrame(
				delivery.destination,
				delivery.subscriptionID,
				msgIDStr,
				delivery.payload,
				delivery.contentType,
				ackID,
			)

			if err := conn.Write(ctx, websocket.MessageText, messageFrame); err != nil {
				return
			}
			h.messagesSent.Add(1)
		}
	}
}

// parseFrame parses raw data into a STOMPFrame.
// The command parameter determines whether header escaping should be applied.
func (h *STOMPHandler) parseFrame(data []byte) (*STOMPFrame, error) {
	// STOMP frame format:
	// COMMAND
	// header1:value1
	// header2:value2
	//
	// body^@
	//
	// Where ^@ is the NULL byte (0x00)
	// Optional trailing EOLs after NULL are allowed

	if len(data) == 0 {
		return nil, &ErrSTOMPProtocolError{Message: "empty frame"}
	}

	// Keep original data for content-length processing
	originalData := data

	// Handle optional trailing EOLs after NULL for non-binary parsing
	data = bytes.TrimRight(data, "\r\n")

	// Split command from rest (EOL is optional CR + required LF)
	var commandEnd int
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			commandEnd = i
			break
		}
	}
	if commandEnd == 0 {
		return nil, &ErrSTOMPProtocolError{Message: "invalid frame format"}
	}

	// Extract command (remove optional CR before LF)
	command := data[:commandEnd]
	if len(command) > 0 && command[len(command)-1] == '\r' {
		command = command[:len(command)-1]
	}
	if len(command) == 0 {
		return nil, &ErrSTOMPProtocolError{Message: "missing command"}
	}

	cmdStr := string(command)

	frame := &STOMPFrame{
		Command: cmdStr,
		Headers: make(map[string]string),
	}

	// Determine if this command should have escaped headers
	// CONNECT and CONNECTED frames do NOT escape headers for backward compatibility
	shouldUnescape := cmdStr != CmdConnect && cmdStr != CmdStomp && cmdStr != STOMPFrameConnected

	// Parse headers - find the blank line separating headers from body
	rest := data[commandEnd+1:]

	// Find blank line (end of headers, start of body)
	// Blank line is an EOL with nothing before it
	var headerEnd int
	var foundBlankLine bool
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\n' {
			// Check if this is a blank line (just LF or CR LF)
			if i == 0 || (i == 1 && rest[0] == '\r') {
				headerEnd = i + 1
				foundBlankLine = true
				break
			}
			// Check if previous char was also newline (two consecutive newlines)
			if i > 0 && rest[i-1] == '\n' {
				headerEnd = i + 1
				foundBlankLine = true
				break
			}
			// Check for CRLF CRLF or LF CRLF patterns
			if i >= 2 && rest[i-1] == '\r' && rest[i-2] == '\n' {
				headerEnd = i + 1
				foundBlankLine = true
				break
			}
		}
	}

	if !foundBlankLine {
		// No blank line found, all remaining is headers (no body)
		headerEnd = len(rest)
	}

	// Parse headers
	headerData := rest[:headerEnd]
	lines := bytes.Split(headerData, []byte{'\n'})

	// Check for content-length header first to properly parse body
	var contentLength = -1

	for _, line := range lines {
		// Remove optional CR
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			continue
		}

		// Parse header: key:value
		// STOMP 1.2: MUST NOT trim or pad headers with spaces
		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx > 0 {
			key := string(line[:colonIdx])
			value := string(line[colonIdx+1:])

			// Apply unescaping if needed (all frames except CONNECT/CONNECTED)
			if shouldUnescape {
				var err error
				value, err = h.decodeHeaderValue(value)
				if err != nil {
					return nil, err
				}
			}

			// First occurrence wins (STOMP spec)
			if _, exists := frame.Headers[key]; !exists {
				frame.Headers[key] = value
			}

			// Track content-length
			if key == HeaderContentLength {
				if cl, err := strconv.Atoi(value); err == nil {
					contentLength = cl
				}
			}
		}
	}

	// Extract body
	// Calculate body start position in original data
	bodyStartOffset := commandEnd + 1 + headerEnd

	if foundBlankLine && bodyStartOffset < len(originalData) {
		if contentLength >= 0 {
			// Use content-length to determine body size from ORIGINAL data
			// This allows NULL bytes within the body
			if bodyStartOffset+contentLength <= len(originalData) {
				frame.Body = originalData[bodyStartOffset : bodyStartOffset+contentLength]
			} else {
				// Not enough data, use what we have
				frame.Body = originalData[bodyStartOffset:]
			}
		} else {
			// No content-length, body ends at first NULL byte
			bodyData := originalData[bodyStartOffset:]
			nullIdx := bytes.IndexByte(bodyData, 0x00)
			if nullIdx >= 0 {
				frame.Body = bodyData[:nullIdx]
			} else {
				frame.Body = bodyData
			}
		}
	}

	return frame, nil
}

// decodeHeaderValue decodes STOMP header value escapes.
// Returns error for undefined escape sequences per STOMP 1.2 spec.
func (h *STOMPHandler) decodeHeaderValue(value string) (string, error) {
	// STOMP 1.2 escapes:
	// \r -> carriage return (octet 13)
	// \n -> line feed (octet 10)
	// \c -> colon (octet 58)
	// \\ -> backslash (octet 92)
	// Undefined escape sequences MUST be treated as fatal protocol error

	result := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		switch {
		case value[i] == '\\' && i+1 < len(value):
			switch value[i+1] {
			case 'r':
				result = append(result, '\r')
				i++
			case 'n':
				result = append(result, '\n')
				i++
			case 'c':
				result = append(result, ':')
				i++
			case '\\':
				result = append(result, '\\')
				i++
			default:
				// Undefined escape sequence is a fatal protocol error
				return "", &STOMPInvalidEscapeError{Sequence: "\\" + string(value[i+1])}
			}
		case value[i] == '\\' && i+1 >= len(value):
			// Trailing backslash - undefined escape
			return "", &STOMPInvalidEscapeError{Sequence: "\\"}
		default:
			result = append(result, value[i])
		}
	}
	return string(result), nil
}

// encodeHeaderValue encodes STOMP header value escapes.
// Only used for frames other than CONNECT/CONNECTED.
func (h *STOMPHandler) encodeHeaderValue(value string) string {
	// STOMP 1.2 escapes:
	// carriage return -> \r
	// line feed -> \n
	// colon -> \c
	// backslash -> \\
	result := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\r':
			result = append(result, '\\', 'r')
		case '\n':
			result = append(result, '\\', 'n')
		case ':':
			result = append(result, '\\', 'c')
		case '\\':
			result = append(result, '\\', '\\')
		default:
			result = append(result, value[i])
		}
	}
	return string(result)
}

// handleFrame parses and handles a STOMP frame.
func (h *STOMPHandler) handleFrame(ctx context.Context, data []byte, session *ws.Session, state *stompClientState) ([]byte, error) {
	frame, err := h.parseFrame(data)
	if err != nil {
		return nil, err
	}

	// Command dispatch using map for O(1) lookup
	type commandHandler func(context.Context, *STOMPFrame, *ws.Session, *stompClientState) ([]byte, error)

	handlers := map[string]commandHandler{
		CmdConnect:     h.handleConnect,
		CmdStomp:       h.handleConnect, // STOMP 1.2 alternative
		CmdSubscribe:   h.handleSubscribe,
		CmdUnsubscribe: h.handleUnsubscribe,
		CmdSend:        h.handleSend,
		CmdAck:         h.handleAck,
		CmdNack:        h.handleNack,
		CmdDisconnect:  h.handleDisconnect,
		CmdBegin:       h.handleBegin,
		CmdCommit:      h.handleCommit,
		CmdAbort:       h.handleAbort,
	}

	handler, ok := handlers[frame.Command]
	if !ok {
		return nil, &ErrSTOMPProtocolError{Message: "unknown command: " + frame.Command}
	}

	return handler(ctx, frame, session, state)
}

// handleConnect processes a CONNECT or STOMP command.
func (h *STOMPHandler) handleConnect(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	// STOMP 1.2 REQUIRED headers: accept-version, host
	acceptVersion := frame.Headers[HeaderAcceptVersion]
	if acceptVersion == "" {
		// For backward compatibility with 1.0, assume 1.0 if missing
		acceptVersion = "1.0"
	}

	host := frame.Headers[HeaderHost]
	// Host is required for STOMP 1.2, but we accept connections without it for 1.0 compatibility

	// Negotiate version (prefer 1.2)
	var version string
	switch {
	case bytes.Contains([]byte(acceptVersion), []byte("1.2")):
		version = "1.2"
		// For 1.2, host is required
		if host == "" {
			return h.buildErrorFrame("Supported protocol versions are 1.0, 1.1, 1.2", frame.Headers[HeaderReceipt], nil), nil
		}
	case bytes.Contains([]byte(acceptVersion), []byte("1.1")):
		version = "1.1"
	case bytes.Contains([]byte(acceptVersion), []byte("1.0")):
		version = "1.0"
	default:
		return h.buildErrorFrame("Supported protocol versions are 1.0, 1.1, 1.2", frame.Headers[HeaderReceipt], nil), nil
	}

	state.mu.Lock()
	state.connected = true
	state.version = version
	state.session = session.ID
	state.mu.Unlock()

	return h.buildConnectedFrame(version, session.ID, frame.Headers[HeaderHeartBeat]), nil
}

// handleSubscribe processes a SUBSCRIBE command.
func (h *STOMPHandler) handleSubscribe(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED headers: destination, id
	destination := frame.Headers[HeaderDestination]
	if destination == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderDestination}
	}

	id := frame.Headers[HeaderID]
	if id == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderID}
	}

	// OPTIONAL: ack (auto, client, client-individual)
	ackMode := frame.Headers[HeaderAck]
	if ackMode == "" {
		ackMode = AckModeAuto
	}

	// Validate ack mode
	if ackMode != AckModeAuto && ackMode != AckModeClient && ackMode != AckModeClientIndividual {
		return nil, &ErrSTOMPProtocolError{Message: "invalid ack mode: " + ackMode}
	}

	// Store subscription
	state.mu.Lock()
	state.subscriptions[id] = stompSubscription{
		id:          id,
		destination: destination,
		ackMode:     ackMode,
	}
	state.mu.Unlock()

	// Subscribe via hub
	if err := h.hub.Subscribe(session.ID, destination, 0); err != nil {
		return nil, &ErrSTOMPProtocolError{Message: "subscription failed: " + err.Error()}
	}

	// Update session subscriptions
	session.Subscribe(destination)

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleUnsubscribe processes an UNSUBSCRIBE command.
func (h *STOMPHandler) handleUnsubscribe(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: id
	id := frame.Headers[HeaderID]
	if id == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderID}
	}

	// Find and remove subscription
	state.mu.Lock()
	sub, exists := state.subscriptions[id]
	if exists {
		delete(state.subscriptions, id)
	}
	state.mu.Unlock()

	if exists {
		if err := h.hub.Unsubscribe(session.ID, sub.destination); err != nil {
			return nil, err
		}
		session.Unsubscribe(sub.destination)
	}

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleSend processes a SEND command.
func (h *STOMPHandler) handleSend(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: destination
	destination := frame.Headers[HeaderDestination]
	if destination == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderDestination}
	}

	// OPTIONAL: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID != "" {
		state.mu.Lock()
		tx, exists := state.transactions[txID]
		if !exists {
			state.mu.Unlock()
			return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction not found"}
		}
		// Queue message in transaction
		tx.messages = append(tx.messages, transactionMessage{
			destination: destination,
			body:        frame.Body,
			contentType: frame.Headers[HeaderContentType],
			headers:     frame.Headers,
		})
		state.mu.Unlock()
	} else {
		// Publish immediately via hub
		if err := h.hub.Publish(destination, frame.Body, 0, false); err != nil {
			return nil, &ErrSTOMPProtocolError{Message: "publish failed: " + err.Error()}
		}
	}

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleAck processes an ACK command.
func (h *STOMPHandler) handleAck(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: id
	ackID := frame.Headers[HeaderID]
	if ackID == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderID}
	}

	// OPTIONAL: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID != "" {
		state.mu.Lock()
		tx, exists := state.transactions[txID]
		if !exists {
			state.mu.Unlock()
			return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction not found"}
		}
		tx.acks = append(tx.acks, ackID)
		state.mu.Unlock()
	} else {
		// Process ACK immediately
		state.mu.Lock()
		pending, exists := state.pendingAcks[ackID]
		if exists {
			// Find subscription to check ack mode
			sub, subExists := state.subscriptions[pending.subscriptionID]
			if subExists && sub.ackMode == AckModeClient {
				// Cumulative ACK - remove this and all earlier pending messages for this subscription
				for id, p := range state.pendingAcks {
					if p.subscriptionID == pending.subscriptionID {
						delete(state.pendingAcks, id)
					}
				}
			} else {
				// Non-cumulative (client-individual) - just remove this one
				delete(state.pendingAcks, ackID)
			}
		}
		state.mu.Unlock()
	}

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleNack processes a NACK command.
func (h *STOMPHandler) handleNack(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: id
	ackID := frame.Headers[HeaderID]
	if ackID == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderID}
	}

	// OPTIONAL: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID != "" {
		state.mu.Lock()
		tx, exists := state.transactions[txID]
		if !exists {
			state.mu.Unlock()
			return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction not found"}
		}
		tx.nacks = append(tx.nacks, ackID)
		state.mu.Unlock()
	} else {
		// Process NACK immediately
		state.mu.Lock()
		pending, exists := state.pendingAcks[ackID]
		if exists {
			sub, subExists := state.subscriptions[pending.subscriptionID]
			if subExists && sub.ackMode == AckModeClient {
				// Cumulative NACK
				for id, p := range state.pendingAcks {
					if p.subscriptionID == pending.subscriptionID {
						delete(state.pendingAcks, id)
					}
				}
			} else {
				delete(state.pendingAcks, ackID)
			}
		}
		state.mu.Unlock()
		// In a full implementation, NACK'd messages could be requeued
	}

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleDisconnect processes a DISCONNECT command.
func (h *STOMPHandler) handleDisconnect(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	// Send RECEIPT if requested before disconnecting
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), ErrSTOMPDisconnect
	}

	return nil, ErrSTOMPDisconnect
}

// handleBegin processes a BEGIN command (transaction start).
func (h *STOMPHandler) handleBegin(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderTransaction}
	}

	state.mu.Lock()
	if _, exists := state.transactions[txID]; exists {
		state.mu.Unlock()
		return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction already exists"}
	}
	state.transactions[txID] = &stompTransaction{
		id:       txID,
		messages: make([]transactionMessage, 0),
		acks:     make([]string, 0),
		nacks:    make([]string, 0),
	}
	state.mu.Unlock()

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleCommit processes a COMMIT command (transaction commit).
func (h *STOMPHandler) handleCommit(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderTransaction}
	}

	state.mu.Lock()
	tx, exists := state.transactions[txID]
	if !exists {
		state.mu.Unlock()
		return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction not found"}
	}

	// Process all queued messages atomically
	for _, msg := range tx.messages {
		if err := h.hub.Publish(msg.destination, msg.body, 0, false); err != nil {
			state.mu.Unlock()
			return nil, &ErrSTOMPProtocolError{Message: "transaction commit failed: " + err.Error()}
		}
	}

	// Process ACKs
	for _, ackID := range tx.acks {
		delete(state.pendingAcks, ackID)
	}

	// Process NACKs (remove from pending)
	for _, nackID := range tx.nacks {
		delete(state.pendingAcks, nackID)
	}

	delete(state.transactions, txID)
	state.mu.Unlock()

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// handleAbort processes an ABORT command (transaction rollback).
func (h *STOMPHandler) handleAbort(ctx context.Context, frame *STOMPFrame, session *ws.Session, state *stompClientState) ([]byte, error) {
	state.mu.RLock()
	connected := state.connected
	state.mu.RUnlock()

	if !connected {
		return nil, &ErrSTOMPProtocolError{Message: "not connected"}
	}

	// REQUIRED: transaction
	txID := frame.Headers[HeaderTransaction]
	if txID == "" {
		return nil, &STOMPMissingHeaderError{Header: HeaderTransaction}
	}

	state.mu.Lock()
	if _, exists := state.transactions[txID]; !exists {
		state.mu.Unlock()
		return nil, &STOMPInvalidTransactionError{TransactionID: txID, Reason: "transaction not found"}
	}
	// Simply discard the transaction
	delete(state.transactions, txID)
	state.mu.Unlock()

	// Send RECEIPT if requested
	if receipt := frame.Headers[HeaderReceipt]; receipt != "" {
		return h.buildReceiptFrame(receipt), nil
	}

	return nil, nil
}

// buildConnectedFrame builds a CONNECTED frame.
// CONNECTED frames do NOT escape header values per STOMP 1.2 spec.
func (h *STOMPHandler) buildConnectedFrame(version, sessionID, clientHeartBeat string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(STOMPFrameConnected)
	buf.WriteByte('\n')
	buf.WriteString(HeaderVersion)
	buf.WriteByte(':')
	buf.WriteString(version)
	buf.WriteByte('\n')
	buf.WriteString(HeaderSession)
	buf.WriteByte(':')
	buf.WriteString(sessionID)
	buf.WriteByte('\n')
	buf.WriteString(HeaderServer)
	buf.WriteString(":go-quicbaas/1.0")
	buf.WriteByte('\n')

	// Calculate heart-beat response
	// Format: <sx>,<sy> where sx is what server can do, sy is what server wants
	// For now, we support 0,0 (no heart-beats)
	buf.WriteString(HeaderHeartBeat)
	buf.WriteString(":0,0")
	buf.WriteByte('\n')

	buf.WriteByte('\n')
	buf.WriteByte(0x00)
	return buf.Bytes()
}

// buildMessageFrame builds a MESSAGE frame.
// MESSAGE frames MUST escape header values per STOMP 1.2 spec.
func (h *STOMPHandler) buildMessageFrame(destination, subscription, messageID string, body []byte, contentType string, ackID string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(STOMPFrameMessage)
	buf.WriteByte('\n')
	buf.WriteString(HeaderDestination)
	buf.WriteByte(':')
	buf.WriteString(h.encodeHeaderValue(destination))
	buf.WriteByte('\n')
	buf.WriteString(HeaderSubscription)
	buf.WriteByte(':')
	buf.WriteString(h.encodeHeaderValue(subscription))
	buf.WriteByte('\n')
	buf.WriteString(HeaderMessageID)
	buf.WriteByte(':')
	buf.WriteString(h.encodeHeaderValue(messageID))
	buf.WriteByte('\n')

	// Include ack header if client/client-individual mode
	if ackID != "" {
		buf.WriteString(HeaderAck)
		buf.WriteByte(':')
		buf.WriteString(h.encodeHeaderValue(ackID))
		buf.WriteByte('\n')
	}

	if contentType != "" {
		buf.WriteString(HeaderContentType)
		buf.WriteByte(':')
		buf.WriteString(h.encodeHeaderValue(contentType))
		buf.WriteByte('\n')
	}
	if len(body) > 0 {
		buf.WriteString(HeaderContentLength)
		buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(len(body)))
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	if len(body) > 0 {
		buf.Write(body)
	}
	buf.WriteByte(0x00)
	return buf.Bytes()
}

// buildReceiptFrame builds a RECEIPT frame.
// RECEIPT frames MUST escape header values per STOMP 1.2 spec.
func (h *STOMPHandler) buildReceiptFrame(receiptID string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(STOMPFrameReceipt)
	buf.WriteByte('\n')
	buf.WriteString(HeaderReceiptID)
	buf.WriteByte(':')
	buf.WriteString(h.encodeHeaderValue(receiptID))
	buf.WriteByte('\n')
	buf.WriteByte('\n')
	buf.WriteByte(0x00)
	return buf.Bytes()
}

// buildErrorFrame builds an ERROR frame.
// ERROR frames MUST escape header values per STOMP 1.2 spec.
func (h *STOMPHandler) buildErrorFrame(message, receiptID string, body []byte) []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(STOMPFrameError)
	buf.WriteByte('\n')
	buf.WriteString(HeaderMessage)
	buf.WriteByte(':')
	buf.WriteString(h.encodeHeaderValue(message))
	buf.WriteByte('\n')
	if receiptID != "" {
		buf.WriteString(HeaderReceiptID)
		buf.WriteByte(':')
		buf.WriteString(h.encodeHeaderValue(receiptID))
		buf.WriteByte('\n')
	}
	if len(body) > 0 {
		buf.WriteString(HeaderContentType)
		buf.WriteString(":text/plain")
		buf.WriteByte('\n')
		buf.WriteString(HeaderContentLength)
		buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(len(body)))
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	if len(body) > 0 {
		buf.Write(body)
	}
	buf.WriteByte(0x00)
	return buf.Bytes()
}

// MessagesReceived returns the total number of messages received.
func (h *STOMPHandler) MessagesReceived() uint64 {
	return h.messagesReceived.Load()
}

// MessagesSent returns the total number of messages sent.
func (h *STOMPHandler) MessagesSent() uint64 {
	return h.messagesSent.Load()
}

// ResetCounters resets the message counters.
func (h *STOMPHandler) ResetCounters() {
	h.messagesReceived.Store(0)
	h.messagesSent.Store(0)
}

// Hub returns the underlying pub/sub hub.
func (h *STOMPHandler) Hub() PubSubHub {
	return h.hub
}

// ClientCount returns the number of connected clients.
func (h *STOMPHandler) ClientCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}
