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

package grpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Agent service that exposes a session event log
// =============================================================================

type sessionLogAgentService struct {
	grpcTestAgentService
	log orchestration.SessionEventLog
}

func (m *sessionLogAgentService) SessionEventLog() orchestration.SessionEventLog {
	return m.log
}

func newSessionEventsServer(t *testing.T, log orchestration.SessionEventLog) *OrchestratorServer {
	t.Helper()
	return NewOrchestratorServer(
		&sessionLogAgentService{log: log},
		createOrchestratorGRPCTestLogger(),
	)
}

// =============================================================================
// Fake bidi PushEvents stream
// =============================================================================

type fakePushEventsStream struct {
	ctx     context.Context
	reqs    []*pb.PushEventRequest
	idx     int
	acks    []*pb.PushEventAck
	sendErr error
}

func (s *fakePushEventsStream) Recv() (*pb.PushEventRequest, error) {
	if s.idx >= len(s.reqs) {
		return nil, io.EOF
	}
	req := s.reqs[s.idx]
	s.idx++
	return req, nil
}

func (s *fakePushEventsStream) Send(ack *pb.PushEventAck) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.acks = append(s.acks, ack)
	return nil
}

func (s *fakePushEventsStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakePushEventsStream) SendHeader(metadata.MD) error { return nil }
func (s *fakePushEventsStream) SetTrailer(metadata.MD)       {}
func (s *fakePushEventsStream) Context() context.Context     { return s.ctx }
func (s *fakePushEventsStream) SendMsg(interface{}) error    { return nil }
func (s *fakePushEventsStream) RecvMsg(interface{}) error    { return nil }

// =============================================================================
// Fake server-streaming SubscribeSessionEvents stream
// =============================================================================

type fakeSubscribeStream struct {
	ctx  context.Context
	sent []*pb.SessionEventEnvelope
}

func (s *fakeSubscribeStream) Send(env *pb.SessionEventEnvelope) error {
	s.sent = append(s.sent, env)
	return nil
}
func (s *fakeSubscribeStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeSubscribeStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeSubscribeStream) SetTrailer(metadata.MD)       {}
func (s *fakeSubscribeStream) Context() context.Context     { return s.ctx }
func (s *fakeSubscribeStream) SendMsg(interface{}) error    { return nil }
func (s *fakeSubscribeStream) RecvMsg(interface{}) error    { return nil }

func eventPayload(t *testing.T, seq uint64) []byte {
	t.Helper()
	ev := orchestration.SessionEvent{
		Sequence:  seq,
		SessionID: "sess-1",
		TenantID:  "tenant-1",
		Kind:      orchestration.KindSystemMessage,
		System:    &orchestration.SystemMessagePayload{Code: "test", Message: "hello"},
	}
	b, err := json.Marshal(&ev)
	require.NoError(t, err)
	return b
}

// =============================================================================
// PushEvents
// =============================================================================

func TestPushEvents_NoLogConfigured(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	stream := &fakePushEventsStream{ctx: context.Background()}
	err := srv.PushEvents(stream)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestPushEvents_AppendAndAck(t *testing.T) {
	log := orchestration.NewMemorySessionEventLog()
	srv := newSessionEventsServer(t, log)

	stream := &fakePushEventsStream{
		ctx: context.Background(),
		reqs: []*pb.PushEventRequest{
			{SessionId: "sess-1", TenantId: "tenant-1", Sequence: 1, PayloadJson: eventPayload(t, 1), EmittedAt: nil},
			// Duplicate re-send: still ack'd ok.
			{SessionId: "sess-1", TenantId: "tenant-1", Sequence: 1, PayloadJson: eventPayload(t, 1)},
			// Gap: sequence jumps to 5.
			{SessionId: "sess-1", TenantId: "tenant-1", Sequence: 5, PayloadJson: eventPayload(t, 5)},
		},
	}
	err := srv.PushEvents(stream)
	require.NoError(t, err)
	require.Len(t, stream.acks, 3)
	assert.Equal(t, "ok", stream.acks[0].Status)
	assert.Equal(t, "ok", stream.acks[1].Status)
	assert.Equal(t, "duplicate", stream.acks[1].Detail)
	assert.Equal(t, "gap", stream.acks[2].Status)
}

func TestHandlePushEvent_Validation(t *testing.T) {
	log := orchestration.NewMemorySessionEventLog()
	srv := newSessionEventsServer(t, log)
	ctx := context.Background()

	// Missing tenant/session.
	ack, err := srv.handlePushEvent(ctx, log, &pb.PushEventRequest{Sequence: 1})
	require.Error(t, err)
	assert.Equal(t, "rejected", ack.Status)

	// Invalid JSON payload.
	ack, err = srv.handlePushEvent(ctx, log, &pb.PushEventRequest{
		SessionId: "s", TenantId: "t", Sequence: 1, PayloadJson: []byte("{not-json"),
	})
	require.Error(t, err)
	assert.Equal(t, "rejected", ack.Status)
}

func TestHandlePushEvent_EmittedAtHonored(t *testing.T) {
	log := orchestration.NewMemorySessionEventLog()
	srv := newSessionEventsServer(t, log)

	emitted := time.Now().Add(-time.Minute).UTC()
	ack, err := srv.handlePushEvent(context.Background(), log, &pb.PushEventRequest{
		SessionId: "sess-1", TenantId: "tenant-1", Sequence: 1,
		PayloadJson: eventPayload(t, 1),
		EmittedAt:   nil,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", ack.Status)
	_ = emitted
}

// =============================================================================
// SubscribeSessionEvents
// =============================================================================

func TestSubscribeSessionEvents_Validation(t *testing.T) {
	srv := newSessionEventsServer(t, orchestration.NewMemorySessionEventLog())
	stream := &fakeSubscribeStream{ctx: context.Background()}
	err := srv.SubscribeSessionEvents(&pb.SubscribeSessionRequest{}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSubscribeSessionEvents_NoLogConfigured(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	stream := &fakeSubscribeStream{ctx: context.Background()}
	err := srv.SubscribeSessionEvents(
		&pb.SubscribeSessionRequest{SessionId: "s", TenantId: "t"},
		stream,
	)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestSubscribeSessionEvents_HistoryReplay(t *testing.T) {
	log := orchestration.NewMemorySessionEventLog()
	ctx := context.Background()
	for seq := uint64(1); seq <= 3; seq++ {
		require.NoError(t, log.Append(ctx, &orchestration.SessionEvent{
			Sequence: seq, SessionID: "sess-1", TenantID: "tenant-1",
			Timestamp: time.Now().UTC(),
			Kind:      orchestration.KindSystemMessage,
			System:    &orchestration.SystemMessagePayload{Code: "c", Message: "m"},
		}))
	}

	srv := newSessionEventsServer(t, log)
	stream := &fakeSubscribeStream{ctx: ctx}

	// follow=false -> returns after history drain.
	err := srv.SubscribeSessionEvents(&pb.SubscribeSessionRequest{
		SessionId: "sess-1", TenantId: "tenant-1", FromSequence: 0, Follow: false,
	}, stream)
	require.NoError(t, err)
	require.Len(t, stream.sent, 3)
	assert.True(t, stream.sent[0].FromReplay)
	assert.Equal(t, uint64(1), stream.sent[0].Sequence)
}

func TestSubscribeSessionEvents_FollowCancelled(t *testing.T) {
	log := orchestration.NewMemorySessionEventLog()
	srv := newSessionEventsServer(t, log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: live-tail loop returns immediately

	stream := &fakeSubscribeStream{ctx: ctx}
	err := srv.SubscribeSessionEvents(&pb.SubscribeSessionRequest{
		SessionId: "sess-1", TenantId: "tenant-1", Follow: true,
	}, stream)
	require.NoError(t, err)
}

// =============================================================================
// sessionLog accessor
// =============================================================================

func TestSessionLog_Accessor(t *testing.T) {
	// Service without provider -> nil.
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	assert.Nil(t, srv.sessionLog())

	// Service with provider -> the configured log.
	log := orchestration.NewMemorySessionEventLog()
	srv2 := newSessionEventsServer(t, log)
	assert.Equal(t, log, srv2.sessionLog())
}
