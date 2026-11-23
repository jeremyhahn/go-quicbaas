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

package orchestrator

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// scriptedStream returns the given messages in order, then terminal as the
// Recv error.
func scriptedStream(msgs []interface{}, terminal error) *mockStream {
	idx := 0
	return &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(msgs) {
				return nil, terminal
			}
			m := msgs[idx]
			idx++
			return m, nil
		},
	}
}

func streamTransport(ms *mockStream) *mockTransport {
	return &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}
}

// drainSessionEvents reads ch until it closes; a hang fails the test.
func drainSessionEvents(t *testing.T, ch <-chan *SessionEventEnvelope) []*SessionEventEnvelope {
	t.Helper()
	var got []*SessionEventEnvelope
	timeout := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, env)
		case <-timeout:
			t.Fatalf("session event channel did not close; got %d values", len(got))
			return got
		}
	}
}

// drainJobEvents reads ch until it closes; a hang fails the test.
func drainJobEvents(t *testing.T, ch <-chan *JobEvent) []*JobEvent {
	t.Helper()
	var got []*JobEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("job event channel did not close; got %d values", len(got))
			return got
		}
	}
}

func subscribeSessionWith(t *testing.T, ms *mockStream) []*SessionEventEnvelope {
	t.Helper()
	c := NewClient(streamTransport(ms))
	ch, err := c.SubscribeSessionEvents(context.Background(), &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.NoError(t, err)
	return drainSessionEvents(t, ch)
}

func TestSubscribeSessionEvents_StreamErrorReachesChannel(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{&SessionEventEnvelope{SessionID: "s1", Sequence: 1}},
		errors.NewUnavailable("mock", "connection reset"),
	)
	got := subscribeSessionWith(t, ms)

	require.Len(t, got, 2)
	assert.Equal(t, uint64(1), got[0].Sequence)
	assert.NoError(t, got[0].Err)
	assert.Equal(t, "s1", got[1].SessionID)
	require.Error(t, got[1].Err)
	assert.Contains(t, got[1].Err.Error(), "connection reset")
}

func TestSubscribeSessionEvents_CleanEndDeliversNoErr(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{&SessionEventEnvelope{SessionID: "s1", Sequence: 1}},
		io.EOF,
	)
	got := subscribeSessionWith(t, ms)

	require.Len(t, got, 1)
	assert.NoError(t, got[0].Err)
}

func TestSubscribeSessionEvents_WrappedEOFIsCleanEnd(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{&SessionEventEnvelope{SessionID: "s1", Sequence: 1}},
		errors.Wrap(io.EOF, errors.CodeUnknown, "grpc.orchestrator.session.Recv", "unknown error"),
	)
	got := subscribeSessionWith(t, ms)

	require.Len(t, got, 1)
	assert.NoError(t, got[0].Err)
}

func TestSubscribeSessionEvents_ErrorMessageReachesChannel(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{errors.NewInternal("svc", "handler failed")},
		io.EOF,
	)
	got := subscribeSessionWith(t, ms)

	require.Len(t, got, 1)
	assert.Equal(t, "s1", got[0].SessionID)
	require.Error(t, got[0].Err)
	assert.Contains(t, got[0].Err.Error(), "handler failed")
}

func TestSubscribeSessionEvents_CancelDeliversNoErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			<-ctx.Done()
			return nil, errors.NewCanceled("mock", "cancelled")
		},
	}
	c := NewClient(streamTransport(ms))
	ch, err := c.SubscribeSessionEvents(ctx, &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.NoError(t, err)

	cancel()
	got := drainSessionEvents(t, ch)
	for _, env := range got {
		assert.NoError(t, env.Err, "a cancelled subscription must not deliver an Err value")
	}
}

func TestSubscribeJobEvents_StreamErrorReachesChannel(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{&JobEvent{JobID: "job_1", Type: "progress"}},
		errors.NewUnavailable("mock", "connection reset"),
	)
	c := NewClient(streamTransport(ms))
	ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{TenantID: "tenant-1", JobID: "job_1"})
	require.NoError(t, err)
	got := drainJobEvents(t, ch)

	require.Len(t, got, 2)
	assert.Equal(t, "progress", got[0].Type)
	assert.NoError(t, got[0].Err)
	assert.Equal(t, "job_1", got[1].JobID)
	require.Error(t, got[1].Err)
	assert.Contains(t, got[1].Err.Error(), "connection reset")
}

func TestSubscribeJobEvents_CleanEndDeliversNoErr(t *testing.T) {
	ms := scriptedStream(
		[]interface{}{&JobEvent{JobID: "job_1", Type: "progress"}},
		io.EOF,
	)
	c := NewClient(streamTransport(ms))
	ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{TenantID: "tenant-1", JobID: "job_1"})
	require.NoError(t, err)
	got := drainJobEvents(t, ch)

	require.Len(t, got, 1)
	assert.NoError(t, got[0].Err)
}
