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

// Package mock provides mock implementations for testing the QuicBaaS SDK.
package mock

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// RequestRecord captures a request for verification.
type RequestRecord struct {
	Service string
	Method  string
	Payload interface{}
}

// StreamMessage represents a message sent or received on a stream.
type StreamMessage struct {
	Payload   interface{}
	Direction StreamDirection
}

// StreamDirection indicates the direction of a stream message.
type StreamDirection int

const (
	// StreamDirectionSend indicates a message sent to the server.
	StreamDirectionSend StreamDirection = iota
	// StreamDirectionRecv indicates a message received from the server.
	StreamDirectionRecv
)

// ResponseConfig configures a mock response for a specific service/method.
type ResponseConfig struct {
	Response interface{}
	Error    error
}

// StreamConfig configures a mock stream for a specific service/method.
type StreamConfig struct {
	// Messages to be returned by Recv in order.
	Messages []interface{}
	// Error to return after all messages are received.
	RecvError error
	// Error to return on Send.
	SendError error
	// Error to return on CloseSend.
	CloseSendError error
	// Error to return on Close.
	CloseError error
}

// Transport is a mock implementation of transport.Transport for testing.
type Transport struct {
	mu sync.RWMutex

	// Responses maps service/method to configured responses.
	responses map[string]ResponseConfig

	// StreamConfigs maps service/method to stream configurations.
	streamConfigs map[string]StreamConfig

	// Requests stores all captured requests.
	requests []RequestRecord

	// StreamRequests stores all captured stream requests.
	streamRequests []RequestRecord

	// closed indicates whether the transport is closed.
	closed atomic.Bool

	// healthy indicates the health status.
	healthy atomic.Bool

	// conn is the mock connection object.
	conn interface{}

	// DefaultError is returned if no specific response is configured.
	DefaultError error

	// DefaultResponse is returned if no specific response is configured.
	DefaultResponse interface{}
}

// NewTransport creates a new mock transport.
func NewTransport() *Transport {
	t := &Transport{
		responses:     make(map[string]ResponseConfig),
		streamConfigs: make(map[string]StreamConfig),
		requests:      make([]RequestRecord, 0),
		conn:          &mockConn{},
	}
	t.healthy.Store(true)
	return t
}

// Request implements transport.Transport.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("MockTransport.Request", "transport is closed")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "MockTransport.Request", "context canceled")
	default:
	}

	t.mu.Lock()
	t.requests = append(t.requests, RequestRecord{
		Service: service,
		Method:  method,
		Payload: req,
	})
	t.mu.Unlock()

	key := t.makeKey(service, method)

	t.mu.RLock()
	cfg, ok := t.responses[key]
	t.mu.RUnlock()

	if ok {
		return cfg.Response, cfg.Error
	}

	if t.DefaultError != nil {
		return nil, t.DefaultError
	}

	return t.DefaultResponse, nil
}

// RequestStream implements transport.Transport.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("MockTransport.RequestStream", "transport is closed")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "MockTransport.RequestStream", "context canceled")
	default:
	}

	t.mu.Lock()
	t.streamRequests = append(t.streamRequests, RequestRecord{
		Service: service,
		Method:  method,
		Payload: req,
	})
	t.mu.Unlock()

	key := t.makeKey(service, method)

	t.mu.RLock()
	cfg, ok := t.streamConfigs[key]
	t.mu.RUnlock()

	if !ok {
		cfg = StreamConfig{}
	}

	return NewStream(cfg), nil
}

// Conn implements transport.Transport.
func (t *Transport) Conn() interface{} {
	return t.conn
}

// Close implements transport.Transport.
func (t *Transport) Close() error {
	if t.closed.Swap(true) {
		return errors.NewConnection("MockTransport.Close", "transport already closed")
	}
	return nil
}

// Healthy implements transport.Transport.
func (t *Transport) Healthy() bool {
	return t.healthy.Load() && !t.closed.Load()
}

// SetResponse configures a response for a specific service/method.
func (t *Transport) SetResponse(service, method string, response interface{}, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.responses[t.makeKey(service, method)] = ResponseConfig{
		Response: response,
		Error:    err,
	}
}

// SetStreamConfig configures a stream for a specific service/method.
func (t *Transport) SetStreamConfig(service, method string, cfg StreamConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamConfigs[t.makeKey(service, method)] = cfg
}

// SetHealthy sets the health status of the mock transport.
func (t *Transport) SetHealthy(healthy bool) {
	t.healthy.Store(healthy)
}

// SetConn sets the mock connection object.
func (t *Transport) SetConn(conn interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.conn = conn
}

// Requests returns a copy of all captured requests.
func (t *Transport) Requests() []RequestRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]RequestRecord, len(t.requests))
	copy(result, t.requests)
	return result
}

// StreamRequests returns a copy of all captured stream requests.
func (t *Transport) StreamRequests() []RequestRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]RequestRecord, len(t.streamRequests))
	copy(result, t.streamRequests)
	return result
}

// LastRequest returns the last captured request, or nil if none.
func (t *Transport) LastRequest() *RequestRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.requests) == 0 {
		return nil
	}
	r := t.requests[len(t.requests)-1]
	return &r
}

// RequestCount returns the number of captured requests.
func (t *Transport) RequestCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.requests)
}

// Reset clears all captured requests and configured responses.
func (t *Transport) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = make([]RequestRecord, 0)
	t.streamRequests = make([]RequestRecord, 0)
	t.responses = make(map[string]ResponseConfig)
	t.streamConfigs = make(map[string]StreamConfig)
	t.closed.Store(false)
	t.healthy.Store(true)
}

// IsClosed returns true if the transport is closed.
func (t *Transport) IsClosed() bool {
	return t.closed.Load()
}

func (t *Transport) makeKey(service, method string) string {
	return service + "/" + method
}

// mockConn is a placeholder connection object.
type mockConn struct{}

// Stream is a mock implementation of transport.Stream.
type Stream struct {
	mu sync.Mutex

	// messages to return on Recv.
	messages []interface{}
	// current position in messages.
	position int
	// sent messages captured.
	sentMessages []interface{}
	// configuration.
	config StreamConfig
	// closed states.
	sendClosed bool
	closed     bool
}

// NewStream creates a new mock stream with the given configuration.
func NewStream(cfg StreamConfig) *Stream {
	return &Stream{
		messages:     cfg.Messages,
		config:       cfg,
		sentMessages: make([]interface{}, 0),
	}
}

// Send implements transport.Stream.
func (s *Stream) Send(msg interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.NewConnection("MockStream.Send", "stream is closed")
	}
	if s.sendClosed {
		return errors.NewConnection("MockStream.Send", "send side is closed")
	}
	if s.config.SendError != nil {
		return s.config.SendError
	}

	s.sentMessages = append(s.sentMessages, msg)
	return nil
}

// Recv implements transport.Stream.
func (s *Stream) Recv() (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errors.NewConnection("MockStream.Recv", "stream is closed")
	}

	if s.position >= len(s.messages) {
		if s.config.RecvError != nil {
			return nil, s.config.RecvError
		}
		return nil, io.EOF
	}

	msg := s.messages[s.position]
	s.position++
	return msg, nil
}

// CloseSend implements transport.Stream.
func (s *Stream) CloseSend() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.NewConnection("MockStream.CloseSend", "stream is closed")
	}
	if s.sendClosed {
		return errors.NewConnection("MockStream.CloseSend", "send side already closed")
	}
	if s.config.CloseSendError != nil {
		return s.config.CloseSendError
	}

	s.sendClosed = true
	return nil
}

// Close implements transport.Stream.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.NewConnection("MockStream.Close", "stream already closed")
	}
	if s.config.CloseError != nil {
		return s.config.CloseError
	}

	s.closed = true
	s.sendClosed = true
	return nil
}

// SentMessages returns a copy of all sent messages.
func (s *Stream) SentMessages() []interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]interface{}, len(s.sentMessages))
	copy(result, s.sentMessages)
	return result
}

// IsClosed returns true if the stream is closed.
func (s *Stream) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// IsSendClosed returns true if the send side is closed.
func (s *Stream) IsSendClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendClosed
}

// Verify that Transport implements transport.Transport.
var _ transport.Transport = (*Transport)(nil)

// Verify that Stream implements transport.Stream.
var _ transport.Stream = (*Stream)(nil)
