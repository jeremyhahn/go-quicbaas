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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStream(t *testing.T) {
	t.Run("creates a valid stream", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 10)
		require.NotNil(t, s)
		assert.NotNil(t, s.recvCh)
		assert.NotNil(t, s.sendCh)
		assert.False(t, s.isClosed())
		assert.NotNil(t, s.context())
	})

	t.Run("channels are functional", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		// Send a message into sendCh and read it back
		s.sendCh <- "hello"
		msg := <-s.sendCh
		assert.Equal(t, "hello", msg)
	})
}

func TestStream_Send_Success(t *testing.T) {
	t.Run("sends message and receives from sendCh", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Send("test-message")
		require.NoError(t, err)

		received := <-s.sendChan()
		assert.Equal(t, "test-message", received)
	})
}

func TestStream_Send_Closed(t *testing.T) {
	t.Run("returns error when stream is closed", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)

		err = s.Send("should-fail")
		assert.ErrorIs(t, err, ErrSendOnClosedStream)
	})
}

func TestStream_Send_CloseSendDone(t *testing.T) {
	t.Run("returns error after CloseSend", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.CloseSend()
		require.NoError(t, err)

		err = s.Send("should-fail")
		assert.ErrorIs(t, err, ErrSendOnClosedStream)
	})
}

func TestStream_Send_ContextCanceled(t *testing.T) {
	t.Run("returns context error when context is canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Buffer size 0 forces Send to block on the channel
		s := newStream(ctx, 0)

		cancel()

		err := s.Send("should-fail")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStream_Recv_Success(t *testing.T) {
	t.Run("receives message from recvCh", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		s.recvChan() <- "response-data"

		msg, err := s.Recv()
		require.NoError(t, err)
		assert.Equal(t, "response-data", msg)
	})
}

func TestStream_Recv_ChannelClosed_NotExplicit(t *testing.T) {
	t.Run("returns io.EOF when recvCh is closed directly", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		// Close the recv channel directly (not via Close()) to simulate
		// a natural end of stream without marking closed flag.
		close(s.recvCh)

		msg, err := s.Recv()
		assert.Nil(t, msg)
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestStream_Recv_Closed(t *testing.T) {
	t.Run("returns ErrRecvOnClosedStream after Close", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)

		msg, err := s.Recv()
		assert.Nil(t, msg)
		assert.ErrorIs(t, err, ErrRecvOnClosedStream)
	})
}

func TestStream_Recv_ContextCanceled(t *testing.T) {
	t.Run("returns context error when context is canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Buffer size 0 so Recv blocks waiting for a message
		s := newStream(ctx, 0)

		done := make(chan struct{})
		var recvErr error

		go func() {
			defer close(done)
			_, recvErr = s.Recv()
		}()

		cancel()

		<-done
		assert.ErrorIs(t, recvErr, context.Canceled)
	})
}

func TestStream_Recv_DrainBuffer(t *testing.T) {
	t.Run("drains buffered messages before returning error", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 3)

		// Buffer messages before closing
		s.recvChan() <- "msg1"
		s.recvChan() <- "msg2"
		s.recvChan() <- "msg3"

		err := s.Close()
		require.NoError(t, err)

		// Drain all buffered messages
		msg1, err := s.Recv()
		require.NoError(t, err)
		assert.Equal(t, "msg1", msg1)

		msg2, err := s.Recv()
		require.NoError(t, err)
		assert.Equal(t, "msg2", msg2)

		msg3, err := s.Recv()
		require.NoError(t, err)
		assert.Equal(t, "msg3", msg3)

		// Now buffer is empty and stream is closed
		msg, err := s.Recv()
		assert.Nil(t, msg)
		assert.ErrorIs(t, err, ErrRecvOnClosedStream)
	})
}

func TestStream_CloseSend(t *testing.T) {
	t.Run("closes sendCh successfully", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.CloseSend()
		require.NoError(t, err)

		// Verify sendCh is closed by reading from it
		_, ok := <-s.sendCh
		assert.False(t, ok, "sendCh should be closed")
	})

	t.Run("idempotent - no panic on double call", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.CloseSend()
		require.NoError(t, err)

		err = s.CloseSend()
		require.NoError(t, err)
	})
}

func TestStream_Close(t *testing.T) {
	t.Run("closes stream successfully", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)
		assert.True(t, s.isClosed())
	})

	t.Run("idempotent - no panic on double call", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)

		err = s.Close()
		require.NoError(t, err)
		assert.True(t, s.isClosed())
	})
}

func TestStream_RecvChan(t *testing.T) {
	t.Run("returns write-only channel for recvCh", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		ch := s.recvChan()
		require.NotNil(t, ch)

		// Verify it is the same underlying channel by writing to recvChan
		// and reading from recvCh.
		ch <- "via-recv-chan"
		msg := <-s.recvCh
		assert.Equal(t, "via-recv-chan", msg)
	})
}

func TestStream_SendChan(t *testing.T) {
	t.Run("returns read-only channel for sendCh", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		ch := s.sendChan()
		require.NotNil(t, ch)

		// Verify it is the same underlying channel by writing to sendCh
		// and reading from sendChan.
		s.sendCh <- "via-send-ch"
		msg := <-ch
		assert.Equal(t, "via-send-ch", msg)
	})
}

func TestStream_Context(t *testing.T) {
	t.Run("returns the stream context", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		streamCtx := s.context()
		require.NotNil(t, streamCtx)

		// Context should not be done yet
		select {
		case <-streamCtx.Done():
			t.Fatal("context should not be done")
		default:
			// expected
		}
	})

	t.Run("context is canceled after Close", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)

		select {
		case <-s.context().Done():
			// expected - context should be canceled
		default:
			t.Fatal("context should be done after Close")
		}
	})
}

func TestStream_IsClosed(t *testing.T) {
	t.Run("false initially", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)
		assert.False(t, s.isClosed())
	})

	t.Run("true after Close", func(t *testing.T) {
		ctx := context.Background()
		s := newStream(ctx, 1)

		err := s.Close()
		require.NoError(t, err)
		assert.True(t, s.isClosed())
	})
}
