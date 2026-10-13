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
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestStream_CloseRacesHandlerSend pins go-quicbaas#144: callers closing the
// stream from several goroutines while the handler is sending must neither
// panic with "send on closed channel" nor race, and Recv must end with
// ErrRecvOnClosedStream once the buffer is drained.
func TestStream_CloseRacesHandlerSend(t *testing.T) {
	s := newStream(context.Background(), 1)
	sendForever := func(_ context.Context, _ service.QuicBaaS, _ interface{}, s *stream) error {
		for i := 0; ; i++ {
			if err := s.deliver(i); err != nil {
				return err
			}
		}
	}

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		runStreamHandler(context.Background(), sendForever, nil, nil, s)
	}()

	var closers sync.WaitGroup
	for range 4 {
		closers.Add(1)
		go func() {
			defer closers.Done()
			if _, err := s.Recv(); err != nil && !errors.Is(err, ErrRecvOnClosedStream) {
				t.Errorf("Recv before Close: %v", err)
			}
			assert.NoError(t, s.Close())
		}()
	}
	closers.Wait()
	<-handlerDone

	// The handler has stopped, so at most a full buffer is left to drain
	// before Recv reports the close.
	for range cap(s.recvCh) + 1 {
		if _, err := s.Recv(); err != nil {
			require.ErrorIs(t, err, ErrRecvOnClosedStream)
			return
		}
	}
	t.Fatal("Recv did not end with ErrRecvOnClosedStream after Close")
}

// TestStream_CloseSendRacesSend runs Send concurrently with CloseSend. Before
// the fix, CloseSend closed sendCh under a Send that had already passed its
// sendDone check, panicking with "send on closed channel". Every Send must
// either be delivered to the handler or fail with ErrSendOnClosedStream, and
// the handler must see end of input once.
func TestStream_CloseSendRacesSend(t *testing.T) {
	for range 200 {
		s := newStream(context.Background(), 1)

		received := make(chan int)
		go func() {
			n := 0
			for range s.sendChan() {
				n++
			}
			received <- n
		}()

		var delivered atomic.Int64
		var senders sync.WaitGroup
		start := make(chan struct{})
		for range 4 {
			senders.Add(1)
			go func() {
				defer senders.Done()
				<-start
				for i := 0; ; i++ {
					err := s.Send(i)
					if err != nil {
						assert.ErrorIs(t, err, ErrSendOnClosedStream)
						return
					}
					delivered.Add(1)
				}
			}()
		}
		close(start)
		require.NoError(t, s.CloseSend())
		require.NoError(t, s.CloseSend())
		senders.Wait()

		assert.Equal(t, delivered.Load(), int64(<-received))
	}
}
