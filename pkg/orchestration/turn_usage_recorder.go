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

import (
	"context"
	"time"
)

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

// TurnAttribution names the work a turn is charged to. It is set by whoever
// knows the job: the control loop for its own agents' turns, and JobRun.Prompt
// for every runner that prompts through the job envelope.
type TurnAttribution struct {
	// JobID is the job the turn belongs to.
	JobID string

	// TaskID is the task that job is an attempt at, empty for a job started
	// directly. It is the ledger's chain level ABOVE the job (see
	// account_ledger.go): without it every attempt at one task would be a
	// separate root in the attribution tree and a task's total spend across
	// its retries could not be summed.
	TaskID string

	// AgentID is the identity of the agent that ran the turn, as minted by the
	// control loop (see AgentIdentity). Empty for a runner that prompts one
	// session directly rather than through a fleet of agents; see
	// WithTurnAttribution for what that costs.
	AgentID string
}

// turnAttributionKey is the unexported context key carrying a TurnAttribution.
type turnAttributionKey struct{}

// WithTurnAttribution returns a context that charges any turn run under it to
// the given job.
//
// THE JOB IS REQUIRED AND THE AGENT IS NOT, and the asymmetry is the two
// consumers wanting different things. The advisory counters' job dimension is
// per-agent-within-job, so they record a job total only when both are present
// — a half-populated attribution there would produce a job total with no agent
// breakdown. The CONSUMPTION LEDGER's chain has no agent level at all: it is
// tenant/project/task/job/session/round, and a runner that prompts one session
// directly (pkg/sdlc) has a job and no agent. Requiring the agent here would
// have dropped every one of those turns from the spend authority's record,
// which is the population the cap exists to bound.
func WithTurnAttribution(ctx context.Context, attribution TurnAttribution) context.Context {
	if attribution.JobID == "" {
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
// A turn whose backend reported no usage is recorded as an OCCURRENCE and not
// as a quantity. Backends whose CLI publishes no token counts (the ACP
// JSON-RPC dialect, `codex exec`) leave PromptResponse.Usage nil, and their
// turns are absent from the COUNTERS rather than represented by an estimate —
// an invented figure would be indistinguishable from a measured one to every
// downstream reader, including account selection and chargeback. They are
// still written to the LEDGER, with no tokens on them, because that is what
// the circuit breaker reads; see the note at the empty check below.
//
// A turn dispatched to a REMOTE daemon is likewise not recorded here: that
// daemon ran the turn through its own orchestrator and charged it to its own
// ledger, and its wire response carries no usage back, so there is exactly one
// record of it and no double counting.
func (s *agentServiceImpl) recordTurnUsage(ctx context.Context, turn completedTurn, usage *TokenUsage) {
	sessionID := turn.sessionID
	projectID := turn.projectID
	if s.costTracker == nil && s.ledger == nil {
		return
	}

	// A REPAIRED TURN IS NOT A FREE TURN, and before #242 the two were the same
	// four zeroes. {"input_tokens":1,"output_tokens":-1} summed to zero, was
	// read as "the CLI reported nothing", and left the ledger untouched — the
	// turn was billed as nothing and nothing anywhere said so. The counts are
	// repaired at the decode boundary now, so the arithmetic no longer cancels;
	// this is the other half, and it is the half an operator can act on.
	//
	// LOGGED BEFORE THE EMPTY CHECK, because the case that most needs saying is
	// exactly the one that returns below: a record whose every dimension was
	// negative clamps to empty and is dropped, which is indistinguishable from
	// a turn that genuinely cost nothing unless something says otherwise here.
	//
	// A warning, not a failure. The turn has already produced its answer, and
	// this function is best-effort by construction (see above) — failing it over
	// bookkeeping would discard work on account of the record describing it.
	// Whether a malformed record should instead fail the turn is the owner's
	// call, recorded in #242 and deliberately not taken here.
	if usage.Malformed() {
		s.logger.Warnf(
			"orchestration: agent reported a negative token count; the turn's accounting was repaired and may under-report: session=%s tokens=%d recorded=%t",
			sessionID, usage.Total(), !usage.Empty())
	}

	report := TurnUsage{
		TenantID:  StorageTenant(ctx),
		ProjectID: projectID,
		Tokens:    usage.Total(),
	}
	attribution := TurnAttributionFromContext(ctx)
	// BOTH OR NEITHER: the counters' job dimension is per-agent-within-job, so
	// a job id with no agent would record a job total with no breakdown under
	// it. The ledger below has no agent level and takes the job on its own.
	if attribution.JobID != "" && attribution.AgentID != "" {
		report.JobID = attribution.JobID
		report.AgentID = attribution.AgentID
	}
	accountID := ""
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		accountID = mem.AccountID()
		report.AccountID = accountID
		if report.ProjectID == "" {
			report.ProjectID = mem.ProjectID
		}
	}

	// THE EMPTY CHECK BINDS THE COUNTERS AND NOT THE LEDGER, and the
	// asymmetry is the point.
	//
	// The counters are a QUANTITY, and LeastUsageAccountStrategy balances a
	// pool on them: recording an unmeasured turn as zero there would make the
	// account that ran it look perpetually least-used and every subsequent
	// selection would land on it again. So a turn whose backend published no
	// token counts is absent from them rather than represented by a figure
	// nobody measured.
	//
	// The ledger is an OCCURRENCE, and the breaker is derived from it. A
	// success is what says an account is working again — BreakerPolicy.tripped
	// compares LastSuccessAt against LastFailureAt — so dropping an unmeasured
	// success is how a recovered account stays parked for the whole failure
	// window. The failure path already records the occurrence with no tokens
	// (recordFailedTurn); recording only one side of that would leave a
	// breaker that can trip and cannot be talked out of it by the very turn
	// that proves recovery. The backends this bites are named at the head of
	// this function: the ACP JSON-RPC dialect and `codex exec` publish no
	// counts at all, so for them EVERY success is unmeasured.
	if s.costTracker != nil && !usage.Empty() {
		if err := s.costTracker.RecordTurnUsage(ctx, report); err != nil {
			s.logger.Warnf("orchestration: failed to record turn usage: session=%s tokens=%d err=%s",
				sessionID, report.Tokens, err.Error())
		}
	}
	s.recordConsumption(ctx, turn, attribution, report.ProjectID, accountID, usage, ConsumptionOutcomeSuccess)
}

// completedTurn is what the turn path knows about one turn beyond its token
// counts: which session and project it ran under, which engine and model
// served it, and the identity that makes recording it idempotent.
//
// It is a struct rather than five parameters because every field is a string
// and four of them are interchangeable at a call site — a transposed engine
// and model would compile, store, and produce a per-model breakdown that is
// quietly wrong.
type completedTurn struct {
	// turnID is the ledger's idempotency key. Minted once per turn when the
	// turn is planned, so the synchronous and asynchronous paths carry the
	// same identity and neither invents one at the moment of recording.
	turnID string

	sessionID string
	projectID string

	// engine is the backend registry name that served the turn; model is the
	// model it resolved to.
	engine string
	model  string
}

// recordConsumption appends one entry to the consumption ledger — the record
// the spend authority makes its decisions from.
//
// SEPARATE FROM THE COUNTERS ABOVE, AND FED BY THE SAME CALL. The counters
// report; this decides. Writing both from one site is what keeps them from
// disagreeing about a turn: there is one place a turn becomes cost data, and
// it either records everywhere or nowhere.
//
// AN UNPOOLED TURN IS NOT LEDGERED, and that is not a gap. The ledger's quota
// plane is keyed by account because an allowance belongs to an account; a turn
// that drew on no pooled account drew on no allowance, and filing it under a
// placeholder would make a fictional account look like the tenant's largest
// spender. Such turns remain visible in the counters, which is what the
// counters are for.
//
// BEST-EFFORT, LIKE ITS CALLER. The turn has already happened and its answer
// is already the caller's; failing it here would discard delivered work over
// bookkeeping. The failure is logged, because an entry missing from the
// authority's record is an account that can spend past its cap.
func (s *agentServiceImpl) recordConsumption(
	ctx context.Context,
	turn completedTurn,
	attribution TurnAttribution,
	projectID, accountID string,
	usage *TokenUsage,
	outcome ConsumptionOutcome,
) {
	if s.ledger == nil || accountID == "" || turn.turnID == "" {
		return
	}
	entry := ConsumptionEntry{
		EntryID:      turn.turnID,
		TenantID:     StorageTenant(ctx),
		ProjectID:    projectID,
		TaskID:       attribution.TaskID,
		JobID:        attribution.JobID,
		SessionID:    turn.sessionID,
		AccountID:    accountID,
		Engine:       turn.engine,
		Model:        turn.model,
		Outcome:      outcome,
		InputTokens:  usage.consumptionInput(),
		OutputTokens: usage.consumptionOutput(),
		OccurredAt:   time.Now().UTC(),
	}
	if err := s.ledger.Record(ctx, entry); err != nil {
		s.logger.Warnf(
			"orchestration: failed to record consumption; this account's spend cap is now under-counting: session=%s account=%s entry=%s err=%s",
			turn.sessionID, accountID, turn.turnID, err.Error())
	}
}

// failedTurnUsage is what the CLI said the failed turn cost, or nil when it
// said nothing.
//
// NIL IS NOT ZERO here either. A backend that reported no accounting for a
// failure records an entry with no tokens on it, which is the truth; a
// backend that reported zero records the same, which is also the truth. The
// distinction that matters — did this turn happen — is carried by the entry's
// EXISTENCE, and the breaker counts entries rather than tokens.
func failedTurnUsage(err error) *TokenUsage {
	evidence, carried := EvidenceOf(err)
	if !carried {
		return nil
	}
	return evidence.Usage
}

// recordFailedTurn appends a FAILURE entry for a turn that produced no answer.
//
// A FAILED TURN IS STILL A TURN, and this is the population the circuit
// breaker is derived from — without these entries the breaker can never trip,
// because a ledger of successes says every account is healthy. It is recorded
// even when the backend reported no tokens: the breaker counts occurrences,
// not spend, and a provider that refuses instantly costs nothing and is
// exactly the failure worth tripping on.
func (s *agentServiceImpl) recordFailedTurn(ctx context.Context, turn completedTurn, usage *TokenUsage) {
	if s.ledger == nil {
		return
	}
	accountID := ""
	projectID := turn.projectID
	if mem, ok := s.liveSessionRecord(turn.sessionID); ok {
		accountID = mem.AccountID()
		if projectID == "" {
			projectID = mem.ProjectID
		}
	}
	s.recordConsumption(ctx, turn, TurnAttributionFromContext(ctx), projectID, accountID, usage, ConsumptionOutcomeFailure)
}
