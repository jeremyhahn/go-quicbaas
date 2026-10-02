// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"testing"
)

// The handler sends its last value and then closes the stream. A receiver that
// found the buffer empty an instant earlier observes the close next; the value
// must still be handed out, not lost behind ErrRecvOnClosedStream.
func TestStream_DrainAfterCloseHandsOutTheBufferedValue(t *testing.T) {
	s := newStream(context.Background(), 4)
	want := errors.New("handler error")
	s.recvChan() <- want
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := s.drainAfterClose()
	if err != nil || got != want {
		t.Fatalf("drainAfterClose = %v, %v; want the buffered value", got, err)
	}
	if _, err := s.drainAfterClose(); !errors.Is(err, ErrRecvOnClosedStream) {
		t.Fatalf("drained stream = %v, want ErrRecvOnClosedStream", err)
	}
}

func TestStream_RecvAfterCloseReturnsBufferedThenClosed(t *testing.T) {
	s := newStream(context.Background(), 4)
	s.recvChan() <- "last"
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Recv(); err != nil || got != "last" {
		t.Fatalf("Recv = %v, %v; want the buffered value", got, err)
	}
	if _, err := s.Recv(); !errors.Is(err, ErrRecvOnClosedStream) {
		t.Fatalf("Recv on drained closed stream = %v, want ErrRecvOnClosedStream", err)
	}
}

func TestStream_RecvReportsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newStream(ctx, 4)
	cancel()
	if _, err := s.Recv(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recv after the caller cancelled = %v, want context.Canceled", err)
	}
}
