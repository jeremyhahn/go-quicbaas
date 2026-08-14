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

package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeOrchestrator is a minimal OrchestratorService server that records
// pushed events and acks them. It is the control-plane stand-in for the
// EventForwarder integration tests.
type fakeOrchestrator struct {
	pb.UnimplementedOrchestratorServiceServer

	mu       sync.Mutex
	received []*pb.PushEventRequest
	gotEvent chan struct{}

	// failRecv, when set, makes the first Recv return an error so the
	// forwarder's reconnect path is exercised.
	failRecv bool
	// dropAck, when set, suppresses acks so alreadyAcked never short
	// circuits.
	dropAck bool
}

func newFakeOrchestrator() *fakeOrchestrator {
	return &fakeOrchestrator{gotEvent: make(chan struct{}, 64)}
}

func (s *fakeOrchestrator) PushEvents(stream pb.OrchestratorService_PushEventsServer) error {
	s.mu.Lock()
	fail := s.failRecv
	s.mu.Unlock()
	if fail {
		return errors.New("simulated stream failure")
	}
	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		s.mu.Lock()
		s.received = append(s.received, req)
		s.mu.Unlock()
		select {
		case s.gotEvent <- struct{}{}:
		default:
		}
		if !s.dropAck {
			_ = stream.Send(&pb.PushEventAck{
				SessionId: req.SessionId,
				Sequence:  req.Sequence,
			})
		}
	}
}

func (s *fakeOrchestrator) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.received)
}

// startFakeOrchestrator stands up the fake server on a bufconn listener
// and returns a DialFn the EventForwarder can use plus a cleanup.
func startFakeOrchestrator(t *testing.T, srv *fakeOrchestrator) (func(ctx context.Context, target string) (*grpc.ClientConn, error), func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	grpcSrv := grpc.NewServer()
	pb.RegisterOrchestratorServiceServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()

	dialFn := func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough:///bufnet",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return lis.Dial()
			}),
		)
	}
	cleanup := func() {
		grpcSrv.Stop()
		_ = lis.Close()
	}
	return dialFn, cleanup
}

func newTestForwarder(t *testing.T, cfg EventForwarderConfig) *EventForwarder {
	t.Helper()
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = "bufnet"
	}
	if cfg.Logger == nil {
		cfg.Logger = &testLogger{t: t}
	}
	f, err := NewEventForwarder(cfg)
	if err != nil {
		t.Fatalf("NewEventForwarder: %v", err)
	}
	return f
}

// -----------------------------------------------------------------------------
// NewEventForwarder validation
// -----------------------------------------------------------------------------

func TestNewEventForwarder_Errors(t *testing.T) {
	if _, err := NewEventForwarder(EventForwarderConfig{Logger: &testLogger{t: t}}); !errors.Is(err, ErrForwarderNoServer) {
		t.Fatalf("want ErrForwarderNoServer, got %v", err)
	}
	if _, err := NewEventForwarder(EventForwarderConfig{ServerAddr: "x"}); !errors.Is(err, ErrForwarderNoLogger) {
		t.Fatalf("want ErrForwarderNoLogger, got %v", err)
	}
}

func TestNewEventForwarder_CustomQueueSize(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 4})
	if cap(f.queue) != 4 {
		t.Fatalf("queue cap=%d want 4", cap(f.queue))
	}
}

// -----------------------------------------------------------------------------
// Start / Stop / run lifecycle through a real bufconn server
// -----------------------------------------------------------------------------

// TestEventForwarder_DeliversEvents drives the full happy path: Start
// dials the fake control plane, ObserveEvent enqueues, run/runOnce
// streams the event, and the server acks it.
func TestEventForwarder_DeliversEvents(t *testing.T) {
	srv := newFakeOrchestrator()
	dialFn, cleanup := startFakeOrchestrator(t, srv)
	defer cleanup()

	f := newTestForwarder(t, EventForwarderConfig{})
	f.DialFn = dialFn

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	// Second Start is a no-op (covers the CompareAndSwap guard).
	f.Start(ctx)

	f.ObserveEvent(&orchestration.SessionEvent{
		SessionID: "s1",
		TenantID:  "t1",
		Kind:      orchestration.KindAssistantChunk,
	})

	select {
	case <-srv.gotEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event delivery")
	}
	if srv.count() < 1 {
		t.Fatalf("expected at least one received event, got %d", srv.count())
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := f.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Second Stop is a no-op.
	if err := f.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestEventForwarder_Stop_ContextDeadline covers the ctx.Done branch of
// Stop when the forwarder goroutine is never started (doneCh never
// closes).
func TestEventForwarder_Stop_ContextDeadline(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{})
	// Do not Start, so the run goroutine never closes doneCh.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.Stop(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestEventForwarder_ReconnectsOnStreamError exercises run's backoff /
// reconnect loop by making the server fail the first stream. The
// forwarder must retry and eventually deliver.
func TestEventForwarder_ReconnectsOnStreamError(t *testing.T) {
	srv := newFakeOrchestrator()
	srv.failRecv = true
	dialFn, cleanup := startFakeOrchestrator(t, srv)
	defer cleanup()

	f := newTestForwarder(t, EventForwarderConfig{})
	f.DialFn = dialFn

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)

	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s1", TenantID: "t1"})

	// Allow one failed attempt, then recover the server.
	srv.mu.Lock()
	srv.failRecv = false
	srv.mu.Unlock()

	select {
	case <-srv.gotEvent:
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for reconnect delivery")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := f.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestEventForwarder_AttachAuth_TokenError makes the token source fail
// so attachAuth's error path (surfaced through runOnce → run reconnect)
// is exercised.
func TestEventForwarder_AttachAuth_TokenError(t *testing.T) {
	dialFn, cleanup := startFakeOrchestrator(t, newFakeOrchestrator())
	defer cleanup()

	f := newTestForwarder(t, EventForwarderConfig{
		TokenSource: failingTokenSource{},
	})
	f.DialFn = dialFn

	// Directly exercise runOnce so we can assert the typed error without
	// racing the backoff loop.
	err := f.runOnce(context.Background())
	if err == nil {
		t.Fatal("expected attach auth error")
	}
}

// TestEventForwarder_AttachAuth_Success confirms a working token source
// returns an annotated context.
func TestEventForwarder_AttachAuth_Success(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{
		TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "abc"}),
	})
	ctx, err := f.attachAuth(context.Background())
	if err != nil {
		t.Fatalf("attachAuth: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}

// TestEventForwarder_AttachAuth_NoToken returns the original context
// unchanged when no token source is configured.
func TestEventForwarder_AttachAuth_NoToken(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{})
	base := context.Background()
	ctx, err := f.attachAuth(base)
	if err != nil {
		t.Fatalf("attachAuth: %v", err)
	}
	if ctx != base {
		t.Fatal("expected original context returned")
	}
}

// TestEventForwarder_DialError covers runOnce when dial fails.
func TestEventForwarder_DialError(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{})
	f.DialFn = func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return nil, errors.New("dial boom")
	}
	if err := f.runOnce(context.Background()); err == nil {
		t.Fatal("expected dial error")
	}
}

// -----------------------------------------------------------------------------
// CallbackFor — stamping + enqueue of structured events
// -----------------------------------------------------------------------------

// TestEventForwarder_CallbackFor_StampsAndEnqueues verifies the structured
// callback stamps tenant and source onto backend-emitted events (which leave
// both empty) and enqueues them for forwarding.
func TestEventForwarder_CallbackFor_StampsAndEnqueues(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 64})
	cb := f.CallbackFor("tenantA", "acp")

	cb(&orchestration.SessionEvent{
		SessionID:      "sess",
		Kind:           orchestration.KindAssistantChunk,
		AssistantChunk: &orchestration.AssistantChunkPayload{Text: "hello"},
	})

	if len(f.queue) != 1 {
		t.Fatalf("queue len=%d, want 1", len(f.queue))
	}
	entry := <-f.queue
	ev := entry.event
	if ev.TenantID != "tenantA" {
		t.Fatalf("tenant=%q, want tenantA", ev.TenantID)
	}
	if ev.Source != "agent:acp" {
		t.Fatalf("source=%q, want agent:acp", ev.Source)
	}
	if ev.Kind != orchestration.KindAssistantChunk || ev.AssistantChunk == nil || ev.AssistantChunk.Text != "hello" {
		t.Fatalf("payload not preserved: %+v", ev)
	}
	if ev.Sequence == 0 {
		t.Fatal("expected ObserveEvent to assign a sequence")
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("expected ObserveEvent to assign a timestamp")
	}
}

// TestEventForwarder_CallbackFor_PreservesExistingStamps verifies the
// callback does not overwrite tenant/source already set upstream, and that a
// nil event is ignored rather than enqueued.
func TestEventForwarder_CallbackFor_PreservesExistingStamps(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 64})
	cb := f.CallbackFor("tenantA", "acp")

	cb(nil)
	if len(f.queue) != 0 {
		t.Fatal("nil event must not enqueue")
	}

	cb(&orchestration.SessionEvent{
		SessionID: "sess",
		TenantID:  "tenantB",
		Source:    "operator:alice",
		Kind:      orchestration.KindSystemMessage,
		System:    &orchestration.SystemMessagePayload{Level: "info", Message: "m"},
	})
	entry := <-f.queue
	if entry.event.TenantID != "tenantB" {
		t.Fatalf("tenant overwritten: %q", entry.event.TenantID)
	}
	if entry.event.Source != "operator:alice" {
		t.Fatalf("source overwritten: %q", entry.event.Source)
	}
}

// -----------------------------------------------------------------------------
// ObserveEvent — sequence assignment, timestamp default, drop policy
// -----------------------------------------------------------------------------

func TestObserveEvent_NilIgnored(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 2})
	f.ObserveEvent(nil)
	if len(f.queue) != 0 {
		t.Fatal("nil event must not enqueue")
	}
}

func TestObserveEvent_AssignsSequenceAndTimestamp(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 4})
	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s"})
	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s"})
	e1 := <-f.queue
	e2 := <-f.queue
	if e1.event.Sequence != 1 || e2.event.Sequence != 2 {
		t.Fatalf("sequences: %d, %d", e1.event.Sequence, e2.event.Sequence)
	}
	if e1.event.Timestamp.IsZero() {
		t.Fatal("expected timestamp default")
	}
}

func TestObserveEvent_PreservesExplicitSequence(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 2})
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s", Sequence: 99, Timestamp: ts})
	e := <-f.queue
	if e.event.Sequence != 99 {
		t.Fatalf("sequence=%d want 99", e.event.Sequence)
	}
	if !e.event.Timestamp.Equal(ts) {
		t.Fatal("explicit timestamp overwritten")
	}
}

// TestObserveEvent_DropsOldestWhenFull drives the slow-consumer eviction
// path and the Drops counter.
func TestObserveEvent_DropsOldestWhenFull(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{QueueSize: 1})
	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s", Sequence: 1})
	// Queue is now full; next enqueue must evict the oldest and bump drops.
	f.ObserveEvent(&orchestration.SessionEvent{SessionID: "s", Sequence: 2})
	if f.Drops() == 0 {
		t.Fatal("expected at least one drop")
	}
}

// -----------------------------------------------------------------------------
// recordAck / alreadyAcked
// -----------------------------------------------------------------------------

func TestRecordAck_And_AlreadyAcked(t *testing.T) {
	f := newTestForwarder(t, EventForwarderConfig{})
	if f.alreadyAcked("s", 1) {
		t.Fatal("unseen session should not be acked")
	}
	f.recordAck("s", 5)
	// A lower ack must not lower the watermark.
	f.recordAck("s", 3)
	if !f.alreadyAcked("s", 5) {
		t.Fatal("seq 5 should be acked")
	}
	if !f.alreadyAcked("s", 3) {
		t.Fatal("seq 3 below watermark should be acked")
	}
	if f.alreadyAcked("s", 6) {
		t.Fatal("seq 6 above watermark should not be acked")
	}
}

// failingTokenSource always fails Token() to exercise auth error paths.
type failingTokenSource struct{}

func (failingTokenSource) Token() (*oauth2.Token, error) {
	return nil, errors.New("token unavailable")
}

// TestEventForwarder_Dial_TLSPath covers the dial() TLS branch (tlsConfig
// non-nil) and the insecure branch. grpc.NewClient is lazy, so no real
// connection is required — we only assert a client is returned and closed.
func TestEventForwarder_Dial_TLSPath(t *testing.T) {
	tlsForwarder := newTestForwarder(t, EventForwarderConfig{
		ServerAddr: "127.0.0.1:9",
		TLSConfig:  &tls.Config{MinVersion: tls.VersionTLS13},
	})
	conn, err := tlsForwarder.dial(context.Background())
	if err != nil {
		t.Fatalf("dial (tls): %v", err)
	}
	_ = conn.Close()

	plainForwarder := newTestForwarder(t, EventForwarderConfig{ServerAddr: "127.0.0.1:9"})
	conn2, err := plainForwarder.dial(context.Background())
	if err != nil {
		t.Fatalf("dial (insecure): %v", err)
	}
	_ = conn2.Close()
}
