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

package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// mcpProtocolVersion is the MCP protocol version declared during initialization.
	mcpProtocolVersion = "2024-11-05"

	// channelNotificationMethod is the JSON-RPC method for pushing channel events.
	channelNotificationMethod = "notifications/claude/channel"

	// defaultEventBufferSize is the default capacity of the event channel.
	defaultEventBufferSize = 256
)

// ReplyHandler is a callback invoked when Claude Code sends a reply via the
// reply tool. The handler receives the target channel name and the message.
type ReplyHandler func(ctx context.Context, channel string, message string) error

// ChannelServer is an MCP server that pushes events into Claude Code sessions.
// It bridges QuicBaaS messaging/task events to Claude Code via the channel protocol.
//
// Claude Code spawns this as a subprocess and communicates via JSON-RPC over stdio.
// Events arrive as <channel source="quicbaas" ...> tags in the Claude Code session.
type ChannelServer struct {
	config       ChannelServerConfig
	reader       *bufio.Scanner
	readerCloser io.Closer // optional: closed on Stop to unblock scanner
	writer       io.Writer
	writerMu     sync.Mutex
	eventCh      chan *ChannelEvent
	replyHandler ReplyHandler
	initialized  atomic.Bool
	closed       atomic.Bool
	started      atomic.Bool
	done         chan struct{}
	logger       interfaces.Logger
}

// ChannelServerConfig holds configuration for a ChannelServer.
type ChannelServerConfig struct {
	// ServerName is the MCP server name (appears as channel source).
	ServerName string

	// Instructions are added to Claude's system prompt.
	Instructions string

	// EnableReply enables the reply tool for two-way communication.
	EnableReply bool

	// EnablePermissionRelay enables remote permission approval/denial.
	EnablePermissionRelay bool

	// EventBufferSize is the size of the event channel buffer.
	// Zero uses the default of 256.
	EventBufferSize int

	// QuicBaaSAddress is the address of the QuicBaaS server to connect to
	// for event sourcing (messaging subscription). Empty = in-process.
	QuicBaaSAddress string
}

// ChannelEvent represents an event to push into a Claude Code session.
type ChannelEvent struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"meta,omitempty"`
}

// channelNotificationParams matches the MCP notifications/claude/channel schema.
type channelNotificationParams struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// mcpInitializeResult is the result payload for the initialize handshake.
type mcpInitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	ServerInfo      mcpServerInfo   `json:"serverInfo"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	Instructions    string          `json:"instructions,omitempty"`
}

// mcpServerInfo identifies the MCP server.
type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// mcpCapabilities declares the MCP server capabilities.
type mcpCapabilities struct {
	Experimental map[string]any `json:"experimental,omitempty"`
	Tools        *mcpToolsCap   `json:"tools,omitempty"`
}

// mcpToolsCap declares tool capabilities.
type mcpToolsCap struct{}

// mcpToolDefinition describes a single tool exposed by the server.
type mcpToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// mcpToolsListResult is the result payload for tools/list.
type mcpToolsListResult struct {
	Tools []mcpToolDefinition `json:"tools"`
}

// mcpToolCallParams is the params payload for tools/call.
type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// mcpToolCallResult is the result payload for tools/call.
type mcpToolCallResult struct {
	Content []mcpToolContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

// mcpToolContent is a content block in a tool call result.
type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewChannelServer creates a new channel server that reads JSON-RPC from reader
// and writes responses/notifications to writer. The reader/writer pair typically
// corresponds to os.Stdin and os.Stdout when Claude Code spawns this process.
// If the reader implements io.Closer, it will be closed on Stop to unblock
// any pending reads.
func NewChannelServer(config ChannelServerConfig, reader io.Reader, writer io.Writer, logger interfaces.Logger) *ChannelServer {
	bufSize := config.EventBufferSize
	if bufSize <= 0 {
		bufSize = defaultEventBufferSize
	}

	if config.ServerName == "" {
		config.ServerName = "quicbaas"
	}

	srv := &ChannelServer{
		config:  config,
		reader:  bufio.NewScanner(reader),
		writer:  writer,
		eventCh: make(chan *ChannelEvent, bufSize),
		done:    make(chan struct{}),
		logger:  logger,
	}

	// Store the reader closer if it supports closing, so we can
	// unblock the scanner on shutdown.
	if rc, ok := reader.(io.Closer); ok {
		srv.readerCloser = rc
	}

	return srv
}

// SetReplyHandler sets the callback invoked when Claude Code sends a reply.
// Must be called before Start.
func (s *ChannelServer) SetReplyHandler(handler ReplyHandler) {
	s.replyHandler = handler
}

// Start begins serving the MCP channel protocol. It runs two concurrent loops:
// one reading JSON-RPC requests from the reader, and one draining the event
// channel as notifications to the writer. Start blocks until the context is
// canceled, the connection closes, or Stop is called.
func (s *ChannelServer) Start(ctx context.Context) error {
	if s.closed.Load() {
		return &ChannelConnectionClosedError{Reason: "server already stopped"}
	}

	s.logger.Info("channel server starting")
	s.started.Store(true)

	readDone := make(chan error, 1)
	go func() {
		readDone <- s.readLoop(ctx)
	}()

	drainDone := make(chan error, 1)
	go func() {
		drainDone <- s.drainLoop(ctx)
	}()

	// Wait for either loop to finish or for external stop.
	select {
	case err := <-readDone:
		s.logger.Info("channel server read loop exited")
		s.closeOnce()
		<-drainDone
		return err
	case err := <-drainDone:
		s.logger.Info("channel server drain loop exited")
		s.closeOnce()
		<-readDone
		return err
	case <-ctx.Done():
		s.logger.Info("channel server context canceled")
		s.closeOnce()
		<-readDone
		<-drainDone
		return &ChannelServerError{Message: "context canceled", Cause: ctx.Err()}
	case <-s.done:
		s.logger.Info("channel server stopped")
		<-readDone
		<-drainDone
		return nil
	}
}

// PushEvent queues an event to be sent into the Claude Code session.
// Returns an error if the server is closed or the context expires before
// the event can be buffered.
func (s *ChannelServer) PushEvent(ctx context.Context, event *ChannelEvent) error {
	if event == nil {
		return &ChannelEventError{Message: "event must not be nil"}
	}

	if s.closed.Load() {
		return &ChannelConnectionClosedError{Reason: "server is closed"}
	}

	if !s.started.Load() {
		return &ChannelServerError{Message: "server not started"}
	}

	select {
	case s.eventCh <- event:
		return nil
	case <-ctx.Done():
		return &ChannelEventError{Message: "context expired while buffering event", Cause: ctx.Err()}
	case <-s.done:
		return &ChannelConnectionClosedError{Reason: "server stopped while buffering event"}
	}
}

// Stop gracefully shuts down the channel server.
func (s *ChannelServer) Stop() error {
	s.closeOnce()
	return nil
}

// Initialized reports whether the MCP initialization handshake has completed.
func (s *ChannelServer) Initialized() bool {
	return s.initialized.Load()
}

// closeOnce idempotently closes the done channel, marks the server closed,
// and closes the reader to unblock any pending scanner reads.
func (s *ChannelServer) closeOnce() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.done)
		// Close the reader to unblock scanner.Scan() if it's waiting on I/O.
		if s.readerCloser != nil {
			// Shutdown proceeds either way; a failed close is only worth reporting.
			if err := s.readerCloser.Close(); err != nil {
				s.logger.Warn("channel server: closing reader failed", "error", err.Error())
			}
		}
	}
}

// readLoop reads newline-delimited JSON-RPC messages from the reader and
// dispatches them to the appropriate handler.
func (s *ChannelServer) readLoop(ctx context.Context) error {
	for s.reader.Scan() {
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return nil
		default:
		}

		line := s.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			s.logger.Debugf("channel server: ignoring malformed JSON-RPC message")
			continue
		}

		if err := s.handleRequest(ctx, &msg); err != nil {
			s.logger.Debugf("channel server: error handling request method=%s: %s", msg.Method, err.Error())
		}
	}

	return nil
}

// drainLoop reads events from the event channel and writes them as
// notifications/claude/channel JSON-RPC notifications.
func (s *ChannelServer) drainLoop(ctx context.Context) error {
	for {
		select {
		case event, ok := <-s.eventCh:
			if !ok {
				return nil
			}
			if !s.initialized.Load() {
				// Buffer events internally until initialized; re-queue.
				// If the server never initializes, events drain on close.
				select {
				case s.eventCh <- event:
				default:
					s.logger.Warn("channel server: dropping event, buffer full before initialization")
				}
				continue
			}
			if err := s.sendNotification(event); err != nil {
				s.logger.Debugf("channel server: failed to send notification: %s", err.Error())
			}
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		}
	}
}

// handleRequest dispatches a JSON-RPC request to the correct handler based on
// the method field. Requests without an ID are treated as notifications from
// the client and are currently ignored.
func (s *ChannelServer) handleRequest(ctx context.Context, msg *JSONRPCMessage) error {
	// Map-based dispatch for O(1) lookup.
	type handlerFunc func(ctx context.Context, msg *JSONRPCMessage) error
	handlers := map[string]handlerFunc{
		"initialize":                s.handleInitialize,
		"notifications/initialized": s.handleInitializedNotification,
		"tools/list":                s.handleToolsList,
		"tools/call":                s.handleToolsCall,
		"ping":                      s.handlePing,
	}

	handler, ok := handlers[msg.Method]
	if !ok {
		// Unknown methods with an ID get a method-not-found error response.
		if msg.ID != nil {
			return s.writeResponse(msg.ID, nil, &JSONRPCError{
				Code:    -32601,
				Message: "method not found: " + msg.Method,
			})
		}
		return nil
	}

	return handler(ctx, msg)
}

// handleInitialize responds to the MCP initialize handshake.
func (s *ChannelServer) handleInitialize(_ context.Context, msg *JSONRPCMessage) error {
	caps := mcpCapabilities{
		Experimental: map[string]any{
			"claude/channel": map[string]any{},
		},
	}

	if s.config.EnablePermissionRelay {
		caps.Experimental["claude/channel/permission"] = map[string]any{}
	}

	if s.config.EnableReply {
		caps.Tools = &mcpToolsCap{}
	}

	result := mcpInitializeResult{
		ProtocolVersion: mcpProtocolVersion,
		ServerInfo: mcpServerInfo{
			Name:    s.config.ServerName,
			Version: "1.0.0",
		},
		Capabilities: caps,
		Instructions: s.config.Instructions,
	}

	return s.writeResponse(msg.ID, result, nil)
}

// handleInitializedNotification processes the initialized notification from
// Claude Code, which signals that the client is ready to receive notifications.
func (s *ChannelServer) handleInitializedNotification(_ context.Context, _ *JSONRPCMessage) error {
	s.initialized.Store(true)
	s.logger.Info("channel server: MCP initialization complete")
	return nil
}

// handlePing responds to MCP ping requests.
func (s *ChannelServer) handlePing(_ context.Context, msg *JSONRPCMessage) error {
	return s.writeResponse(msg.ID, map[string]any{}, nil)
}

// handleToolsList returns the list of tools exposed by this channel server.
func (s *ChannelServer) handleToolsList(_ context.Context, msg *JSONRPCMessage) error {
	var tools []mcpToolDefinition

	if s.config.EnableReply {
		tools = append(tools, mcpToolDefinition{
			Name:        "reply",
			Description: "Send a message back through the QuicBaaS channel",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{
						"type":        "string",
						"description": "Target channel name",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Message to send",
					},
				},
				"required": []string{"channel", "message"},
			},
		})
	}

	result := mcpToolsListResult{Tools: tools}
	return s.writeResponse(msg.ID, result, nil)
}

// handleToolsCall dispatches a tool call to the appropriate handler.
func (s *ChannelServer) handleToolsCall(ctx context.Context, msg *JSONRPCMessage) error {
	rawParams, err := json.Marshal(msg.Params)
	if err != nil {
		return s.writeResponse(msg.ID, nil, &JSONRPCError{
			Code:    -32602,
			Message: "invalid params",
		})
	}

	var params mcpToolCallParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return s.writeResponse(msg.ID, nil, &JSONRPCError{
			Code:    -32602,
			Message: "invalid params: " + err.Error(),
		})
	}

	switch params.Name {
	case "reply":
		return s.handleReplyTool(ctx, msg.ID, params.Arguments)
	default:
		return s.writeResponse(msg.ID, nil, &JSONRPCError{
			Code:    -32602,
			Message: "unknown tool: " + params.Name,
		})
	}
}

// handleReplyTool processes a reply tool invocation.
func (s *ChannelServer) handleReplyTool(ctx context.Context, id any, args map[string]any) error {
	if !s.config.EnableReply {
		return s.writeResponse(id, mcpToolCallResult{
			Content: []mcpToolContent{{Type: "text", Text: "reply tool is not enabled"}},
			IsError: true,
		}, nil)
	}

	channelVal, ok := args["channel"]
	if !ok {
		return s.writeResponse(id, mcpToolCallResult{
			Content: []mcpToolContent{{Type: "text", Text: "missing required argument: channel"}},
			IsError: true,
		}, nil)
	}
	channel, ok := channelVal.(string)
	if !ok {
		return s.writeResponse(id, mcpToolCallResult{
			Content: []mcpToolContent{{Type: "text", Text: "channel must be a string"}},
			IsError: true,
		}, nil)
	}

	messageVal, ok := args["message"]
	if !ok {
		return s.writeResponse(id, mcpToolCallResult{
			Content: []mcpToolContent{{Type: "text", Text: "missing required argument: message"}},
			IsError: true,
		}, nil)
	}
	message, ok := messageVal.(string)
	if !ok {
		return s.writeResponse(id, mcpToolCallResult{
			Content: []mcpToolContent{{Type: "text", Text: "message must be a string"}},
			IsError: true,
		}, nil)
	}

	if s.replyHandler != nil {
		if err := s.replyHandler(ctx, channel, message); err != nil {
			return s.writeResponse(id, mcpToolCallResult{
				Content: []mcpToolContent{{Type: "text", Text: "reply failed: " + err.Error()}},
				IsError: true,
			}, nil)
		}
	}

	return s.writeResponse(id, mcpToolCallResult{
		Content: []mcpToolContent{{Type: "text", Text: "reply sent to channel: " + channel}},
	}, nil)
}

// sendNotification writes a notifications/claude/channel JSON-RPC notification.
func (s *ChannelServer) sendNotification(event *ChannelEvent) error {
	msg := &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		Method:  channelNotificationMethod,
		Params: channelNotificationParams{
			Content: event.Content,
			Meta:    event.Metadata,
		},
	}

	return s.writeMessage(msg)
}

// writeResponse writes a JSON-RPC response with the given ID, result, and
// optional error.
func (s *ChannelServer) writeResponse(id any, result any, rpcErr *JSONRPCError) error {
	msg := &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}

	return s.writeMessage(msg)
}

// writeMessage serializes a JSON-RPC message and writes it as a newline-delimited
// JSON line to the writer. The mutex protects concurrent writes.
func (s *ChannelServer) writeMessage(msg *JSONRPCMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return &ChannelEventError{Message: "marshal failed", Cause: err}
	}
	data = append(data, '\n')

	s.writerMu.Lock()
	defer s.writerMu.Unlock()

	if _, err := s.writer.Write(data); err != nil {
		return &ChannelEventError{Message: "write failed", Cause: err}
	}

	return nil
}

// extractInt64ID extracts an int64 from a JSON-RPC message ID, which may
// arrive as float64 or json.Number from JSON unmarshaling.
func extractInt64ID(id any) (int64, bool) {
	switch v := id.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}
