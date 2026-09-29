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
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// sessionEventLogProvider is the optional interface the AgentService may
// satisfy to expose its session event log. It is orchestration's own
// SessionLogAccessor under a local name: every transport reaches the log
// through that one declaration, so a service that gains or loses a log
// behaves identically on all of them.
type sessionEventLogProvider = orchestration.SessionLogAccessor

// PushEvents implements OrchestratorService.PushEvents. The daemon opens a
// bidi stream and sends one PushEventRequest per session event it emits;
// the central validates and appends each to the session event log, then
// acks with the accepted sequence.
//
// At-least-once semantics: duplicate sequences are silently ack'd without
// re-appending so daemons can safely re-send on reconnect. Gaps (sequence
// skips) are rejected with a "gap" ack — the daemon must restart from
// accepted_seq+1.
//
// Authorization: orchestrator:hook, per the method's entry in
// grpcMethodPermissions (authz_mapping.go) — the table the interceptor
// actually reads. An enrolled daemon is granted that permission on its OAuth2
// application so its event stream is admitted (iam.FleetRuntimeGrant,
// go-quicbaas#653). Tenant is extracted from the per-request Context via
// TenantFromContext.
func (s *OrchestratorServer) PushEvents(stream pb.OrchestratorService_PushEventsServer) error {
	log := s.sessionLog()
	if log == nil {
		return status.Error(codes.Unavailable, "session event log not configured")
	}

	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		ack, appendErr := s.handlePushEvent(stream.Context(), log, req)
		if err := stream.Send(ack); err != nil {
			return err
		}
		// If handling produced a non-gap error (permanent rejection), log
		// and continue — the daemon gets the ack and decides whether to
		// retry or abandon. Gaps are expected on reconnect and are logged
		// at debug level only.
		if appendErr != nil && !errors.Is(appendErr, orchestration.ErrSessionLogGap) {
			s.logger.WithError(appendErr).Warnf("PushEvents: session=%s seq=%d rejected", req.SessionId, req.Sequence)
		}
	}
}

// handlePushEvent validates + persists one incoming event and returns the
// ack the client expects. The err return lets PushEvents log-and-continue;
// the ack always carries machine-readable status.
func (s *OrchestratorServer) handlePushEvent(ctx context.Context, log orchestration.SessionEventLog, req *pb.PushEventRequest) (*pb.PushEventAck, error) {
	if req.SessionId == "" || req.TenantId == "" {
		return &pb.PushEventAck{
			SessionId: req.SessionId,
			Sequence:  req.Sequence,
			Status:    "rejected",
			Detail:    "session_id and tenant_id required",
		}, errors.New("session_id and tenant_id required")
	}

	// Translate the opaque JSON into a structured event so the log can
	// enforce monotonic ordering + encrypt the payload consistently with
	// events appended by the local orchestrator path.
	var event orchestration.SessionEvent
	if err := json.Unmarshal(req.PayloadJson, &event); err != nil {
		return &pb.PushEventAck{
			SessionId: req.SessionId,
			Sequence:  req.Sequence,
			Status:    "rejected",
			Detail:    "invalid payload_json: " + err.Error(),
		}, err
	}

	// Normalize: force the envelope fields from the request (wire) to win
	// over anything in the JSON body. Prevents a malicious or buggy daemon
	// from overriding its own tenant / sequence.
	event.SessionID = req.SessionId
	event.TenantID = req.TenantId
	event.Sequence = req.Sequence
	if req.EmittedAt != nil {
		event.Timestamp = req.EmittedAt.AsTime()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Latest-sequence check up front so we can distinguish "already
	// persisted" (idempotent re-send) from "gap" (sequence skip).
	latest, latestErr := log.LatestSequence(ctx, req.TenantId, req.SessionId)
	if latestErr != nil {
		return &pb.PushEventAck{
			SessionId: req.SessionId,
			Sequence:  req.Sequence,
			Status:    "rejected",
			Detail:    "latest sequence lookup failed: " + latestErr.Error(),
		}, latestErr
	}
	if req.Sequence <= latest {
		// Duplicate re-send after daemon reconnect. Ack without re-appending.
		return &pb.PushEventAck{
			SessionId:   req.SessionId,
			Sequence:    req.Sequence,
			Status:      "ok",
			Detail:      "duplicate",
			AcceptedSeq: latest,
		}, nil
	}
	if req.Sequence != latest+1 {
		return &pb.PushEventAck{
			SessionId:   req.SessionId,
			Sequence:    req.Sequence,
			Status:      "gap",
			Detail:      "expected next sequence",
			AcceptedSeq: latest,
		}, orchestration.ErrSessionLogGap
	}

	if err := log.Append(ctx, &event); err != nil {
		return &pb.PushEventAck{
			SessionId:   req.SessionId,
			Sequence:    req.Sequence,
			Status:      "rejected",
			Detail:      err.Error(),
			AcceptedSeq: latest,
		}, err
	}
	return &pb.PushEventAck{
		SessionId:   req.SessionId,
		Sequence:    req.Sequence,
		Status:      "ok",
		AcceptedSeq: req.Sequence,
	}, nil
}

// SubscribeSessionEvents implements OrchestratorService.SubscribeSessionEvents.
// Delivers an ordered stream of events to the client: first a replay from
// from_sequence (inclusive), then if follow=true a live tail until the
// client disconnects.
//
// The replay → live transition is gap-free: we register the fanout
// subscription BEFORE draining history, buffer any live events received
// during the drain in a local slice, then after draining history flush
// those buffered events in order. This ensures no event is missed or
// duplicated at the handoff boundary.
func (s *OrchestratorServer) SubscribeSessionEvents(req *pb.SubscribeSessionRequest, stream pb.OrchestratorService_SubscribeSessionEventsServer) error {
	if req.SessionId == "" || req.TenantId == "" {
		return status.Error(codes.InvalidArgument, "session_id and tenant_id required")
	}
	log := s.sessionLog()
	if log == nil {
		return status.Error(codes.Unavailable, "session event log not configured")
	}

	ctx := stream.Context()

	// Register the live subscription FIRST so we don't miss events produced
	// during the historical drain. Events that arrive while we're sending
	// history get buffered here, then flushed after history is done.
	liveCh, unsubscribe, err := log.Subscribe(ctx, req.TenantId, req.SessionId)
	if err != nil {
		return status.Errorf(codes.Internal, "subscribe: %v", err)
	}
	defer unsubscribe()

	// Drain history in pages of 256. The client's from_sequence may be 0
	// (meaning "from the beginning"), which the log treats as 1.
	cursor := req.FromSequence
	if cursor == 0 {
		cursor = 1
	}
	historyEnd := uint64(0)
	for {
		page, next, pageErr := log.Range(ctx, req.TenantId, req.SessionId, cursor, 256)
		if pageErr != nil {
			return status.Errorf(codes.Internal, "range: %v", pageErr)
		}
		for _, ev := range page {
			if err := sendEnvelope(stream, ev, true); err != nil {
				return err
			}
			if ev.Sequence > historyEnd {
				historyEnd = ev.Sequence
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}

	if !req.Follow {
		return nil
	}

	// Live tail. Drop any buffered live events that fall within the range
	// we already delivered via Range (they can be duplicated between the
	// Subscribe registration and the Range drain).
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-liveCh:
			if !ok {
				return nil
			}
			if ev.Sequence <= historyEnd {
				continue // already delivered via history
			}
			if err := sendEnvelope(stream, ev, false); err != nil {
				return err
			}
			if ev.Sequence > historyEnd {
				historyEnd = ev.Sequence
			}
		}
	}
}

// sendEnvelope marshals a SessionEvent into a wire envelope and sends it.
// Shared between the history and live-tail phases so encoding is uniform.
func sendEnvelope(stream pb.OrchestratorService_SubscribeSessionEventsServer, ev *orchestration.SessionEvent, fromReplay bool) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return status.Errorf(codes.Internal, "marshal: %v", err)
	}
	env := &pb.SessionEventEnvelope{
		SessionId:   ev.SessionID,
		TenantId:    ev.TenantID,
		Sequence:    ev.Sequence,
		PayloadJson: payload,
		Timestamp:   timestamppb.New(ev.Timestamp),
		FromReplay:  fromReplay,
	}
	return stream.Send(env)
}

// sessionLog returns the session event log via the AgentService's optional
// sessionEventLogProvider interface. Returns nil when the service doesn't
// expose a log (embedded mode, tests with a limited fake service).
func (s *OrchestratorServer) sessionLog() orchestration.SessionEventLog {
	p, ok := s.agentSvc.(sessionEventLogProvider)
	if !ok {
		return nil
	}
	return p.SessionEventLog()
}

// GetSessionConversation implements OrchestratorService.GetSessionConversation:
// one page of a session's CONVERSATION.
//
// It is deliberately unary while its sibling SubscribeSessionEvents is a
// stream, because they answer different questions. The stream serves the raw
// log to an attacher that wants every fragment as it happens; this serves the
// folded conversation to a client that wants to render a page of scrollback
// and then stop. Paging a stream by reading it until it closes — which is how
// the SDK reached history before this RPC existed — cannot express a cursor,
// so a client could not resume where a page ended.
func (s *OrchestratorServer) GetSessionConversation(ctx context.Context, req *pb.GetSessionConversationRequest) (*pb.GetSessionConversationResponse, error) {
	if req == nil || req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	log := s.sessionLog()
	if log == nil {
		return nil, status.Error(codes.Unavailable, "session event log not configured")
	}

	page, err := orchestration.ReadSessionHistory(ctx, log, &orchestration.SessionHistoryRequest{
		TenantID:  req.GetTenantId(),
		SessionID: req.GetSessionId(),
		Cursor:    req.GetCursor(),
		Limit:     int(req.GetLimit()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "session history: %v", err)
	}

	turns := make([]*pb.SessionTurn, 0, len(page.Turns))
	for _, turn := range page.Turns {
		wire, marshalErr := sessionTurnToProto(turn)
		if marshalErr != nil {
			return nil, marshalErr
		}
		turns = append(turns, wire)
	}
	return &pb.GetSessionConversationResponse{
		SessionId:      page.SessionID,
		TenantId:       page.TenantID,
		Turns:          turns,
		NextCursor:     page.NextCursor,
		OldestSequence: page.OldestSequence,
	}, nil
}

// sessionTurnToProto renders one turn onto the wire.
//
// Token usage crosses as JSON for the same reason a session event's payload
// does: the accounting shape belongs to the orchestration package, and giving
// it a second, protobuf-shaped definition would create two records of one
// thing that drift the first time a dimension is added.
func sessionTurnToProto(turn *orchestration.SessionTurn) (*pb.SessionTurn, error) {
	tools := make([]*pb.SessionToolUse, 0, len(turn.Tools))
	for _, tool := range turn.Tools {
		tools = append(tools, &pb.SessionToolUse{
			InvocationId: tool.InvocationID,
			Name:         tool.Name,
			IsError:      tool.IsError,
			ResultBytes:  tool.ResultBytes,
		})
	}
	var usageJSON []byte
	if turn.Usage != nil {
		encoded, err := json.Marshal(turn.Usage)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "marshal usage: %v", err)
		}
		usageJSON = encoded
	}
	return &pb.SessionTurn{
		Sequence:       turn.Sequence,
		SessionId:      turn.SessionID,
		TenantId:       turn.TenantID,
		Role:           string(turn.Role),
		Timestamp:      timestamppb.New(turn.Timestamp),
		Text:           turn.Text,
		TruncatedBytes: turn.TruncatedBytes,
		Model:          turn.Model,
		StopReason:     turn.StopReason,
		Tools:          tools,
		UsageJson:      usageJSON,
	}, nil
}
