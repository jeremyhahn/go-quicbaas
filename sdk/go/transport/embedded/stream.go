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

package embedded

import (
	"context"
	"io"
	"sync/atomic"
)

// stream implements transport.Stream for embedded in-process streaming.
// It bridges between Go channels and the transport.Stream interface.
type stream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	recvCh   chan interface{}
	sendCh   chan interface{}
	closed   atomic.Bool
	sendDone atomic.Bool
}

// newStream creates a new embedded stream.
func newStream(ctx context.Context, bufferSize int) *stream {
	ctx, cancel := context.WithCancel(ctx)
	return &stream{
		ctx:    ctx,
		cancel: cancel,
		recvCh: make(chan interface{}, bufferSize),
		sendCh: make(chan interface{}, bufferSize),
	}
}

// Send sends a message on the stream.
func (s *stream) Send(msg interface{}) error {
	if s.closed.Load() {
		return ErrSendOnClosedStream
	}
	if s.sendDone.Load() {
		return ErrSendOnClosedStream
	}

	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.sendCh <- msg:
		return nil
	}
}

// Recv receives a message from the stream.
// It allows draining buffered messages even after the stream is closed.
func (s *stream) Recv() (interface{}, error) {
	// First, try to receive any buffered messages without blocking.
	// This allows draining the buffer even after the stream is marked as closed.
	select {
	case msg, ok := <-s.recvCh:
		if !ok {
			// Channel is closed. Check if it was explicitly closed via Close()
			// or if it's a natural end of stream.
			if s.closed.Load() {
				return nil, ErrRecvOnClosedStream
			}
			return nil, io.EOF
		}
		return msg, nil
	default:
		// No message available, check if we should block or return error
	}

	// If closed and no buffered messages, return error
	if s.closed.Load() {
		return nil, ErrRecvOnClosedStream
	}

	// Block waiting for a message or context cancellation
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case msg, ok := <-s.recvCh:
		if !ok {
			// Channel is closed. Check if it was explicitly closed via Close()
			// or if it's a natural end of stream.
			if s.closed.Load() {
				return nil, ErrRecvOnClosedStream
			}
			return nil, io.EOF
		}
		return msg, nil
	}
}

// CloseSend closes the send side of the stream.
func (s *stream) CloseSend() error {
	if s.sendDone.CompareAndSwap(false, true) {
		close(s.sendCh)
	}
	return nil
}

// Close closes the stream entirely.
func (s *stream) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		// Close recv channel to unblock any receivers
		close(s.recvCh)
	}
	return nil
}

// recvChan returns the receive channel for the service to send responses.
func (s *stream) recvChan() chan<- interface{} {
	return s.recvCh
}

// sendChan returns the send channel for the service to receive requests.
func (s *stream) sendChan() <-chan interface{} {
	return s.sendCh
}

// context returns the stream context.
func (s *stream) context() context.Context {
	return s.ctx
}

// isClosed returns true if the stream is closed.
func (s *stream) isClosed() bool {
	return s.closed.Load()
}
