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
// It allows draining buffered messages even after the stream is closed, and
// returns ErrRecvOnClosedStream once a closed stream's buffer is empty.
func (s *stream) Recv() (interface{}, error) {
	// Drain a buffered message first: buffered values outlive Close.
	select {
	case msg := <-s.recvCh:
		return msg, nil
	default:
	}

	if s.closed.Load() {
		return s.drainAfterClose()
	}

	select {
	case <-s.ctx.Done():
		// Close cancels the context, so a closed stream lands here too. The
		// handler sends its last value BEFORE it calls Close, so that value is
		// buffered by the time the close is observable and must still be
		// handed out rather than lost to the cancellation.
		if s.closed.Load() {
			return s.drainAfterClose()
		}
		return nil, s.ctx.Err()
	case msg := <-s.recvCh:
		return msg, nil
	}
}

// drainAfterClose returns a message still buffered when the stream was
// closed, or ErrRecvOnClosedStream once there is none. A receiver that found
// the buffer empty and then observed the close can race the handler's last
// send-then-Close; checking the buffer once more after the close is observed
// closes that window, because the value was sent before the close flag was set.
func (s *stream) drainAfterClose() (interface{}, error) {
	select {
	case msg := <-s.recvCh:
		return msg, nil
	default:
		return nil, ErrRecvOnClosedStream
	}
}

// CloseSend closes the send side of the stream.
func (s *stream) CloseSend() error {
	if s.sendDone.CompareAndSwap(false, true) {
		close(s.sendCh)
	}
	return nil
}

// Close closes the stream entirely. It cancels the stream context, which
// unblocks Recv and every deliver; it never closes recvCh, so a handler that
// is mid-send when the caller closes the stream cannot panic.
func (s *stream) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
	}
	return nil
}

// deliver hands msg to the caller's Recv. It is the only path by which a
// value enters recvCh. It returns the stream context's error, without
// sending, once the stream is closed or its context is cancelled.
func (s *stream) deliver(msg interface{}) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case s.recvCh <- msg:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
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
