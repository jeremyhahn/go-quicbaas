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

//go:build !integration

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// serviceExitTimeout bounds how long a test waits for the service goroutine
// to return after the handler has. A goroutine that never returns is a leak.
const serviceExitTimeout = 5 * time.Second

// subscribeUntilSendFails runs Subscribe against a stream whose Send always
// fails and whose context is never cancelled, so only the handler can stop
// the service goroutine. It returns once the service double has exited.
func subscribeUntilSendFails(t *testing.T, subscribe func(ctx context.Context, updates chan<- *PolicyUpdate)) {
	t.Helper()

	serviceDone := make(chan struct{})
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(ctx context.Context, _ string, _ []string, updates chan<- *PolicyUpdate) error {
			defer close(serviceDone)
			subscribe(ctx, updates)
			return ctx.Err()
		},
	}
	srv := newTestPEPServer(t, svc)
	stream := &mockPEPSubscribeStream{
		ctx:       context.Background(),
		sendError: errors.New("connection lost"),
	}

	err := srv.Subscribe(&iampb.SubscribeRequest{ServiceId: "test-svc"}, stream)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))

	select {
	case <-serviceDone:
	case <-time.After(serviceExitTimeout):
		t.Fatal("PEP service goroutine still running after Subscribe returned")
	}
}

// TestPEPSubscribe_SendFailureWhileServiceSending drives a stream whose Send
// fails while the service is still flooding updates. The handler used to
// close the updates channel on return, so the still-sending service panicked
// with "send on closed channel". The service must instead be stopped by the
// handler cancelling its context.
func TestPEPSubscribe_SendFailureWhileServiceSending(t *testing.T) {
	const iterations = 200

	for i := 0; i < iterations; i++ {
		subscribeUntilSendFails(t, func(ctx context.Context, updates chan<- *PolicyUpdate) {
			for version := int64(1); ; version++ {
				select {
				case <-ctx.Done():
					return
				case updates <- &PolicyUpdate{Type: PolicyUpdateSnapshot, PolicyID: "policy", Version: version}:
				}
			}
		})
	}
}

// TestPEPSubscribe_ServiceExitsAfterHandlerReturns shows the service goroutine
// does not outlive the handler when the stream context stays live: a service
// that sends one update and then waits on its context must be released when
// Send fails.
func TestPEPSubscribe_ServiceExitsAfterHandlerReturns(t *testing.T) {
	subscribeUntilSendFails(t, func(ctx context.Context, updates chan<- *PolicyUpdate) {
		updates <- &PolicyUpdate{Type: PolicyUpdateSnapshot, PolicyID: "policy", Version: 1}
		<-ctx.Done()
	})
}
