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

package orchestration

import "context"

// =============================================================================
// Turn attribution
//
// A completed turn has to be charged to something. Four dimensions are
// available and each comes from a different place:
//
//   - tenant  — the caller's storage tenant, always present.
//   - account — the pooled provider account the session's agent process
//               authenticates as, resolved at spawn time and mirrored onto the
//               in-memory session record.
//   - project — the project the session was spawned under.
//   - job/agent — only meaningful inside a control-loop job, which is the one
//               caller that knows both. It travels on the context rather than
//               on PromptSessionRequest because it is orchestrator-internal
//               bookkeeping, not something an operator-facing transport should
//               be able to assert (a caller that could name any job ID could
//               charge its tokens to someone else's budget).
// =============================================================================

// TurnAttribution names the job and agent a turn is charged to. It is set by
// the control loop, which is the only caller that knows both.
type TurnAttribution struct {
	// JobID is the control-loop job the turn belongs to.
	JobID string

	// AgentID is the identity of the agent that ran the turn, as minted by the
	// control loop (see AgentIdentity).
	AgentID string
}

// turnAttributionKey is the unexported context key carrying a TurnAttribution.
type turnAttributionKey struct{}

// WithTurnAttribution returns a context that charges any turn run under it to
// the given job and agent. Both fields are required: the cost ledger's job
// dimension is per-agent-within-job, so a half-populated attribution would
// record a job total with no agent breakdown. An incomplete attribution is
// therefore dropped rather than partially recorded.
func WithTurnAttribution(ctx context.Context, attribution TurnAttribution) context.Context {
	if attribution.JobID == "" || attribution.AgentID == "" {
		return ctx
	}
	return context.WithValue(ctx, turnAttributionKey{}, attribution)
}

// TurnAttributionFromContext returns the job/agent attribution carried by ctx,
// or the zero value when none was set. Safe to call on any context.
func TurnAttributionFromContext(ctx context.Context) TurnAttribution {
	if ctx == nil {
		return TurnAttribution{}
	}
	attribution, _ := ctx.Value(turnAttributionKey{}).(TurnAttribution)
	return attribution
}

// =============================================================================
// Recording
// =============================================================================

// recordTurnUsage charges one completed turn's reported token usage to every
// dimension the orchestrator can attribute it to.
//
// It is deliberately best-effort: a turn that has already produced its answer
// must not be reported as failed because the ledger write did not land. The
// failure is logged so a gap in the ledger is visible rather than silent.
//
// Nothing is recorded when the backend reported no usage. Backends whose CLI
// publishes no token counts (the ACP JSON-RPC dialect, `codex exec`) leave
// PromptResponse.Usage nil, and their turns are absent from the ledger rather
// than represented by an estimate — an invented figure would be indistinguishable
// from a measured one to every downstream reader, including account selection
// and chargeback.
//
// A turn dispatched to a REMOTE daemon is likewise not recorded here: that
// daemon ran the turn through its own orchestrator and charged it to its own
// ledger, and its wire response carries no usage back, so there is exactly one
// record of it and no double counting.
func (s *agentServiceImpl) recordTurnUsage(ctx context.Context, sessionID string, projectID string, usage *TokenUsage) {
	if s.costTracker == nil || usage.Empty() {
		return
	}

	turn := TurnUsage{
		TenantID:  StorageTenant(ctx),
		ProjectID: projectID,
		Tokens:    usage.Total(),
	}
	if attribution := TurnAttributionFromContext(ctx); attribution.JobID != "" {
		turn.JobID = attribution.JobID
		turn.AgentID = attribution.AgentID
	}
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		turn.AccountID = mem.AccountID()
		if turn.ProjectID == "" {
			turn.ProjectID = mem.ProjectID
		}
	}

	if err := s.costTracker.RecordTurnUsage(ctx, turn); err != nil {
		s.logger.Warnf("orchestration: failed to record turn usage: session=%s tokens=%d err=%s",
			sessionID, turn.Tokens, err.Error())
	}
}
