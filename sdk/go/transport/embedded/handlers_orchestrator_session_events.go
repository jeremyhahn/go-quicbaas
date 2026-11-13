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
	"encoding/json"
	stderrors "errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// orchestratorServiceName is the service the session event verbs belong to.
const orchestratorServiceName = "quicbaas.Orchestrator"

// sessionEventPageSize is how many events one history read takes from the
// session event log, the page size the gRPC SubscribeSessionEvents drain uses.
const sessionEventPageSize = 256

// The session event verbs register here, apart from the transport.go maps,
// so this file merges independently of other routing changes.
func init() {
	unaryHandlers[orchestratorServiceName+"/GetSessionHistory"] = handleOrchestratorGetSessionHistory
	streamHandlers[orchestratorServiceName+"/SubscribeSessionEvents"] = handleOrchestratorSubscribeSessionEventsStream
}

// SessionEventEncodeError reports a logged session event that could not be
// encoded as the JSON payload of a SessionEventEnvelope.
type SessionEventEncodeError struct {
	SessionID string
	Sequence  uint64
	Cause     error
}

// Error implements the error interface.
func (e *SessionEventEncodeError) Error() string {
	return "INTERNAL: embedded: quicbaas.Orchestrator: encode session " + e.SessionID +
		" event " + strconv.FormatUint(e.Sequence, 10) + ": " + e.Cause.Error()
}

// Unwrap returns the encoding failure.
func (e *SessionEventEncodeError) Unwrap() error {
	return e.Cause
}

// NewSessionEventEncodeError creates a new SessionEventEncodeError.
func NewSessionEventEncodeError(sessionID string, sequence uint64, cause error) *SessionEventEncodeError {
	return &SessionEventEncodeError{SessionID: sessionID, Sequence: sequence, Cause: cause}
}

// sessionEventLogFor resolves the session event log a session verb reads and
// the tenant it reads it for. The request may name only the tenant the call
// is bound to, the check the gRPC SubscribeSessionEvents handler makes. A
// session the log has never heard of is not found.
func sessionEventLogFor(ctx context.Context, svc service.QuicBaaS, method, tenantID, sessionID string) (orchestration.SessionEventLog, string, error) {
	if sessionID == "" {
		return nil, "", &orchestration.SessionHistoryError{Cause: orchestration.ErrSessionIDRequired}
	}
	tenant, err := tenantctx.Target(ctx, orchestratorServiceName+"/"+method, tenantID)
	if err != nil {
		return nil, "", err
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, "", NewServiceNotConfiguredError(orchestratorServiceName)
	}
	accessor, ok := agentSvc.(orchestration.SessionLogAccessor)
	if !ok || accessor.SessionEventLog() == nil {
		return nil, "", &orchestration.SessionHistoryError{TenantID: tenant, SessionID: sessionID, Cause: orchestration.ErrSessionLogRequired}
	}
	log := accessor.SessionEventLog()
	if err := requireKnownSession(ctx, log, method, tenant, sessionID); err != nil {
		return nil, "", err
	}
	return log, tenant, nil
}

// requireKnownSession refuses a session with no metadata record and no
// logged event as not found. A registered session with no events yet, and a
// session whose events were logged without a metadata record, are known.
func requireKnownSession(ctx context.Context, log orchestration.SessionEventLog, method, tenantID, sessionID string) error {
	_, err := log.GetMeta(ctx, tenantID, sessionID)
	if err == nil {
		return nil
	}
	if !stderrors.Is(err, orchestration.ErrSessionLogNotFound) {
		return &orchestration.SessionHistoryError{TenantID: tenantID, SessionID: sessionID, Cause: err}
	}
	latest, err := log.LatestSequence(ctx, tenantID, sessionID)
	if err != nil {
		return &orchestration.SessionHistoryError{TenantID: tenantID, SessionID: sessionID, Cause: err}
	}
	if latest > 0 {
		return nil
	}
	return errors.Wrap(orchestration.ErrSessionLogNotFound, errors.CodeNotFound,
		"embedded."+orchestratorServiceName+"."+method, "session "+sessionID+" not found")
}

// firstSessionSequence maps a request's from-sequence to the first sequence
// the log holds: 0 means "from the beginning", which the log numbers 1.
func firstSessionSequence(fromSequence uint64) uint64 {
	if fromSequence == 0 {
		return 1
	}
	return fromSequence
}

// sessionEventEnvelope encodes one logged event the way the gRPC transport
// does: the whole event as JSON, addressed by session, tenant and sequence.
func sessionEventEnvelope(ev *orchestration.SessionEvent, fromReplay bool) (*orchestratorSDK.SessionEventEnvelope, error) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return nil, NewSessionEventEncodeError(ev.SessionID, ev.Sequence, err)
	}
	return &orchestratorSDK.SessionEventEnvelope{
		SessionID:   ev.SessionID,
		TenantID:    ev.TenantID,
		Sequence:    ev.Sequence,
		PayloadJSON: payload,
		Timestamp:   ev.Timestamp,
		FromReplay:  fromReplay,
	}, nil
}

// handleOrchestratorGetSessionHistory returns one page of a session's raw
// event log, read from the same log the gRPC SubscribeSessionEvents drain
// reads. NextSeq is the sequence of the next page, or 0 at the end.
func handleOrchestratorGetSessionHistory(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetSessionHistoryRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(orchestratorServiceName, "GetSessionHistory",
			"*orchestrator.GetSessionHistoryRequest", typeNameOf(req))
	}
	log, tenantID, err := sessionEventLogFor(ctx, svc, "GetSessionHistory", r.TenantID, r.SessionID)
	if err != nil {
		return nil, err
	}
	events, next, err := log.Range(ctx, tenantID, r.SessionID, firstSessionSequence(r.FromSequence), r.Limit)
	if err != nil {
		return nil, &orchestration.SessionHistoryError{TenantID: tenantID, SessionID: r.SessionID, Cause: err}
	}
	out := &orchestratorSDK.GetSessionHistoryResponse{
		Events:  make([]*orchestratorSDK.SessionEventEnvelope, 0, len(events)),
		NextSeq: next,
	}
	for _, ev := range events {
		env, encErr := sessionEventEnvelope(ev, true)
		if encErr != nil {
			return nil, encErr
		}
		out.Events = append(out.Events, env)
	}
	return out, nil
}

// handleOrchestratorSubscribeSessionEventsStream replays a session's event
// log from the requested sequence and, when Follow is set, tails it live. It
// subscribes before it replays, as the gRPC handler does, so no event appended
// during the replay is lost, and it drops live events the replay already sent.
func handleOrchestratorSubscribeSessionEventsStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*orchestratorSDK.SubscribeSessionEventsRequest)
	if !ok || r == nil {
		return NewInvalidRequestTypeError(orchestratorServiceName, "SubscribeSessionEvents",
			"*orchestrator.SubscribeSessionEventsRequest", typeNameOf(req))
	}
	log, tenantID, err := sessionEventLogFor(ctx, svc, "SubscribeSessionEvents", r.TenantID, r.SessionID)
	if err != nil {
		return err
	}
	live, unsubscribe, err := log.Subscribe(ctx, tenantID, r.SessionID)
	if err != nil {
		return &orchestration.SessionHistoryError{TenantID: tenantID, SessionID: r.SessionID, Cause: err}
	}
	defer unsubscribe()

	delivered := uint64(0)
	send := func(ev *orchestration.SessionEvent, fromReplay bool) error {
		env, encErr := sessionEventEnvelope(ev, fromReplay)
		if encErr != nil {
			return encErr
		}
		delivered = ev.Sequence
		return s.deliver(env)
	}

	cursor := firstSessionSequence(r.FromSequence)
	for cursor != 0 {
		page, next, rangeErr := log.Range(ctx, tenantID, r.SessionID, cursor, sessionEventPageSize)
		if rangeErr != nil {
			return &orchestration.SessionHistoryError{TenantID: tenantID, SessionID: r.SessionID, Cause: rangeErr}
		}
		for _, ev := range page {
			if sendErr := send(ev, true); sendErr != nil {
				return sendErr
			}
		}
		cursor = next
	}
	if !r.Follow {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.context().Done():
			return nil
		case ev, open := <-live:
			if !open {
				return nil
			}
			if ev.Sequence <= delivered {
				continue
			}
			if sendErr := send(ev, false); sendErr != nil {
				return sendErr
			}
		}
	}
}
