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
