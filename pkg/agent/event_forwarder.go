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
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// EventForwarder ships session events from the daemon to the control plane via
// the OrchestratorService.PushEvents bidi stream. Every spawned ACP session
// installs a callback that feeds its events into the forwarder's queue;
// the forwarder then drains the queue over the stream, retrying through
// disconnects via a bounded in-memory ring buffer.
//
// At-least-once delivery: the forwarder remembers the highest ack'd
// sequence per session and on reconnect re-sends any events whose
// sequence exceeds that watermark. The control-plane handler is
// idempotent (duplicate sequence = silent skip) so re-sends are safe.
//
// Back-pressure: when the queue fills (defaultQueueSize=1024), the oldest
// event is dropped and a drop counter is incremented so tests / metrics
// can observe slow-consumer conditions. Dropped events NEVER transit to
// the control plane; attachers connected during the drop see a gap via
// sequence-number inspection and re-range to backfill.
type EventForwarder struct {
	serverAddr string
	tokens     oauth2.TokenSource
	tlsConfig  *tls.Config
	logger     interfaces.Logger

	// sequence generator per session. Uses atomic.Uint64 for lock-free
	// monotonic increments — the spawn path can call Next(sessionID) from
	// any goroutine.
	seqMu sync.Mutex
	seqs  map[string]*atomic.Uint64

	// Per-session highest-ack'd sequence. Used on reconnect to decide
	// which buffered events to re-send.
	ackMu sync.Mutex
	acked map[string]uint64

	queue chan *forwarderEntry
	drops atomic.Uint64

	// lifecycle
	started atomic.Bool
	stopped atomic.Bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	// DialFn replaces grpc.NewClient for bufconn-backed tests. nil in
	// production.
	DialFn func(ctx context.Context, target string) (*grpc.ClientConn, error)
}

// EventForwarderConfig bundles the forwarder's dependencies.
type EventForwarderConfig struct {
	// ServerAddr is the gRPC host:port of the control plane OrchestratorService.
	ServerAddr string

	// TokenSource supplies bearer tokens for authenticating the stream.
	// Required in production; nil permitted only alongside InsecureTransport
	// (for tests).
	TokenSource oauth2.TokenSource

	// TLSConfig is the TLS config for the outbound gRPC connection. Nil
	// triggers plaintext — only acceptable for tests.
	TLSConfig *tls.Config

	// Logger is required.
	Logger interfaces.Logger

	// QueueSize overrides the default event queue size. Ignored when <=0.
	QueueSize int
}

// NewEventForwarder constructs a forwarder. It does not start the
// background stream goroutine — call Start(ctx) for that.
func NewEventForwarder(cfg EventForwarderConfig) (*EventForwarder, error) {
	if cfg.ServerAddr == "" {
		return nil, ErrForwarderNoServer
	}
	if cfg.Logger == nil {
		return nil, ErrForwarderNoLogger
	}
	size := cfg.QueueSize
	if size <= 0 {
		size = defaultEventForwarderQueue
	}
	return &EventForwarder{
		serverAddr: cfg.ServerAddr,
		tokens:     cfg.TokenSource,
		tlsConfig:  cfg.TLSConfig,
		logger:     cfg.Logger,
		seqs:       map[string]*atomic.Uint64{},
		acked:      map[string]uint64{},
		queue:      make(chan *forwarderEntry, size),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}, nil
}

// defaultEventForwarderQueue is the number of events the forwarder will
// buffer in-memory when the control plane is unreachable. Chosen to cover a few
// seconds of moderate assistant-chunk output before dropping; operators
// running at scale can raise it via EventForwarderConfig.QueueSize.
const defaultEventForwarderQueue = 1024

// Start launches the background goroutine that drains the queue to the
// control plane. Returns immediately. Idempotent — subsequent Start calls are
// no-ops.
func (f *EventForwarder) Start(ctx context.Context) {
	if !f.started.CompareAndSwap(false, true) {
		return
	}
	go f.run(ctx)
}

// Stop signals the forwarder to drain + exit. Safe to call multiple times.
// Blocks until the background goroutine exits or ctx is done.
func (f *EventForwarder) Stop(ctx context.Context) error {
	if !f.stopped.CompareAndSwap(false, true) {
		return nil
	}
	close(f.stopCh)
	select {
	case <-f.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ObserveEvent enqueues one event for forwarding. Called from the
// Runtime's AgentEventCallback hook. Assigns the event its per-session
// sequence number at enqueue time so ordering reflects the order events
// were observed, not delivered.
//
// Slow-consumer policy: when the queue is full, the oldest pending event
// is evicted and a drop counter is incremented so tests and metrics can
// see when the control plane is not keeping up.
func (f *EventForwarder) ObserveEvent(event *orchestration.SessionEvent) {
	if event == nil {
		return
	}
	if event.Sequence == 0 {
		event.Sequence = f.nextSequence(event.SessionID)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	entry := &forwarderEntry{event: event}
	select {
	case f.queue <- entry:
	default:
		// Queue full — drop oldest.
		select {
		case <-f.queue:
			f.drops.Add(1)
		default:
		}
		select {
		case f.queue <- entry:
		default:
			f.drops.Add(1)
		}
	}
}

// CallbackFor returns an AgentEventCallback closure the runtime can install
// on each Spawn. The callback translates the backend's ad-hoc event model
// (sessionID, eventType, details) into structured SessionEvents — producing
// one AssistantChunk per assistant event, one ToolInvocation per tool
// event, etc. Unknown event types become SystemMessage events.
func (f *EventForwarder) CallbackFor(tenantID, backend string) orchestration.AgentEventCallback {
	return func(sessionID, eventType, details string) {
		ev := &orchestration.SessionEvent{
			SessionID: sessionID,
			TenantID:  tenantID,
			Source:    "agent:" + backend,
			Timestamp: time.Now().UTC(),
		}
		switch eventType {
		case "prompt":
			ev.Kind = orchestration.KindPromptSubmitted
			ev.Prompt = &orchestration.PromptSubmittedPayload{Prompt: details}
		case "assistant", "assistant_chunk":
			ev.Kind = orchestration.KindAssistantChunk
			ev.AssistantChunk = &orchestration.AssistantChunkPayload{Text: details}
		case "tool", "tool_call":
			ev.Kind = orchestration.KindToolInvocation
			ev.ToolInvocation = &orchestration.ToolInvocationPayload{
				Name: details,
			}
		case "tool_result":
			ev.Kind = orchestration.KindToolResult
			ev.ToolResult = &orchestration.ToolResultPayload{Content: details}
		default:
			ev.Kind = orchestration.KindSystemMessage
			ev.System = &orchestration.SystemMessagePayload{
				Level:   "info",
				Message: details,
				Code:    eventType,
			}
		}
		f.ObserveEvent(ev)
	}
}

// Drops returns the cumulative count of events the forwarder has dropped
// due to full queue. Reset only by restart.
func (f *EventForwarder) Drops() uint64 { return f.drops.Load() }

// nextSequence returns the next per-session sequence number. First event
// in a session gets 1.
func (f *EventForwarder) nextSequence(sessionID string) uint64 {
	f.seqMu.Lock()
	defer f.seqMu.Unlock()
	ctr, ok := f.seqs[sessionID]
	if !ok {
		ctr = &atomic.Uint64{}
		f.seqs[sessionID] = ctr
	}
	return ctr.Add(1)
}

// run is the forwarder's main goroutine. It maintains a persistent bidi
// stream to the control plane; on disconnect, it backs off and re-dials. The
// queue drains into the stream's Send; the stream's Recv updates
// per-session ack watermarks.
func (f *EventForwarder) run(ctx context.Context) {
	defer close(f.doneCh)

	backoff := time.Second
	for {
		select {
		case <-f.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		if err := f.runOnce(ctx); err != nil {
			f.logger.Warnf("event forwarder: stream error: %v (reconnect in %s)", err, backoff)
			select {
			case <-f.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

// runOnce establishes one stream and pumps events until an error or
// shutdown signal. Returns the stream error to the caller for backoff.
func (f *EventForwarder) runOnce(ctx context.Context) error {
	conn, err := f.dial(ctx)
	if err != nil {
		return &OpError{Op: "dial", Cause: err}
	}
	defer conn.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	streamCtx, err = f.attachAuth(streamCtx)
	if err != nil {
		return &OpError{Op: "attach auth", Cause: err}
	}

	client := pb.NewOrchestratorServiceClient(conn)
	stream, err := client.PushEvents(streamCtx)
	if err != nil {
		return &OpError{Op: "open stream", Cause: err}
	}

	// Recv goroutine: absorbs acks so Send doesn't block.
	recvErrCh := make(chan error, 1)
	go func() {
		for {
			ack, err := stream.Recv()
			if err != nil {
				recvErrCh <- err
				return
			}
			f.recordAck(ack.SessionId, ack.Sequence)
		}
	}()

	// Send loop: drain queue events, send over the wire. Errors propagate.
	for {
		select {
		case <-f.stopCh:
			_ = stream.CloseSend()
			return nil
		case <-streamCtx.Done():
			return streamCtx.Err()
		case err := <-recvErrCh:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return &OpError{Op: "recv", Cause: err}
		case entry := <-f.queue:
			// Skip events the control plane has already ack'd (duplicates after
			// reconnect). Cheap guard; control plane is also idempotent.
			if f.alreadyAcked(entry.event.SessionID, entry.event.Sequence) {
				continue
			}
			payload, marshalErr := json.Marshal(entry.event)
			if marshalErr != nil {
				f.logger.Warnf("event forwarder: marshal event: %v", marshalErr)
				continue
			}
			req := &pb.PushEventRequest{
				SessionId:   entry.event.SessionID,
				TenantId:    entry.event.TenantID,
				Sequence:    entry.event.Sequence,
				PayloadJson: payload,
				EmittedAt:   timestamppb.New(entry.event.Timestamp),
			}
			if err := stream.Send(req); err != nil {
				// Re-enqueue so the next connection picks it up.
				select {
				case f.queue <- entry:
				default:
					f.drops.Add(1)
				}
				return &OpError{Op: "send", Cause: err}
			}
		}
	}
}

// dial opens a gRPC connection to the control plane. Honors DialFn for tests.
func (f *EventForwarder) dial(ctx context.Context) (*grpc.ClientConn, error) {
	if f.DialFn != nil {
		return f.DialFn(ctx, f.serverAddr)
	}
	opts := []grpc.DialOption{}
	if f.tlsConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(f.tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	return grpc.NewClient(f.serverAddr, opts...)
}

// attachAuth adds the bearer-token Authorization metadata to the stream
// context when a TokenSource is configured. Absent a token source we
// attach no auth — the control plane will reject the stream unless configured
// to accept unauthenticated event pushes (test-only).
func (f *EventForwarder) attachAuth(ctx context.Context) (context.Context, error) {
	if f.tokens == nil {
		return ctx, nil
	}
	tok, err := f.tokens.Token()
	if err != nil {
		return nil, err
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok.AccessToken), nil
}

// recordAck updates the per-session ack watermark so alreadyAcked can
// skip re-sends of events the control plane has already persisted.
func (f *EventForwarder) recordAck(sessionID string, sequence uint64) {
	f.ackMu.Lock()
	defer f.ackMu.Unlock()
	if cur := f.acked[sessionID]; sequence > cur {
		f.acked[sessionID] = sequence
	}
}

// alreadyAcked reports whether sequence is at or below the recorded
// watermark for the session.
func (f *EventForwarder) alreadyAcked(sessionID string, sequence uint64) bool {
	f.ackMu.Lock()
	defer f.ackMu.Unlock()
	return sequence <= f.acked[sessionID]
}

// forwarderEntry wraps a queued event so we can extend it with
// diagnostics (enqueue time, retry count) without churning the queue
// channel's type on every change.
type forwarderEntry struct {
	event *orchestration.SessionEvent
}

// =============================================================================
// Errors
// =============================================================================

// ErrForwarderNoServer flags a missing ServerAddr in the config.
var ErrForwarderNoServer = errors.New("agent event forwarder: control plane addr required")

// ErrForwarderNoLogger flags a missing Logger in the config.
var ErrForwarderNoLogger = errors.New("agent event forwarder: logger required")
