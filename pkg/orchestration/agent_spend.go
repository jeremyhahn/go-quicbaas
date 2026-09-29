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
	"errors"
	"strconv"
	"time"
)

// =============================================================================
// An Agent's own spend limits
// =============================================================================
//
// AN AGENT TAKES SPEND LIMITS AS A PROJECT DOES (owner ruling 2026-09-25): the
// same SpendLimits, judged by the same comparison (QuotaAuthority.AdmitScope),
// over the Agent quota plane the ledger writes for every turn of a session
// that ran as the Agent.
//
// THEY ARE CHECKED WHEN A SESSION IS SPAWNED, per Agent, as a spawn by
// reference walks its candidates. An Agent over a window is a refusal the next
// member of a pool may not share, so the walk moves on and records the switch
// as AgentFailoverOverBudget; a spawn naming the Agent alone is refused with
// AgentSpendExhaustedError, which a task attempt reads as a deferral until the
// window frees up rather than as a failed attempt.
//
// THE PROJECT'S AND THE TENANT'S LIMITS ARE UNCHANGED: admission still defers
// a spent project or tenant (admission_spend.go), and every turn still counts
// against them. An Agent's limits are one more spender on top.
//
// FAIL CLOSED. An Agent with limits whose window cannot be summed — no ledger
// wired, a store that failed — is refused with AgentSpendUndecidableError,
// which does not fail over: the next Agent's window would be read from the
// same broken ledger.

var (
	// ErrAgentSpendExhausted indicates a tenant Agent whose own spend window
	// is spent.
	ErrAgentSpendExhausted = errors.New("agent: spend limit reached")

	// ErrAgentSpendUndecidable indicates an Agent's spend limits that could
	// not be checked.
	ErrAgentSpendUndecidable = errors.New("agent: spend limits could not be checked")
)

// AgentFailoverOverBudget: the Agent's own spend window is spent.
const AgentFailoverOverBudget = "over_budget"

// AgentSpendExhaustedError reports an Agent whose own spend window is spent,
// with the evidence: the window, which allowance ran out, how much of it, and
// when it frees up.
type AgentSpendExhaustedError struct {
	AgentID string

	// Window names the window that refused, and Axis which of its two
	// allowances ran out: QuotaAxisTokens or QuotaAxisEquivalentMicros.
	Window string
	Axis   string

	Used  int64
	Limit int64

	// RetryAt is when the window's oldest entry ages out; zero when nothing
	// can say.
	RetryAt time.Time
}

// Error implements the error interface.
func (e *AgentSpendExhaustedError) Error() string {
	msg := ErrAgentSpendExhausted.Error() + ": agent " + e.AgentID + " spent " +
		strconv.FormatInt(e.Used, 10) + " of " + strconv.FormatInt(e.Limit, 10) + " " + e.Axis +
		" in window " + strconv.Quote(e.Window)
	if !e.RetryAt.IsZero() {
		msg += "; frees up at " + e.RetryAt.UTC().Format(time.RFC3339)
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentSpendExhaustedError) Unwrap() error { return ErrAgentSpendExhausted }

// AgentSpendUndecidableError reports an Agent's spend limits that could not
// be checked, so its session was not started.
type AgentSpendUndecidableError struct {
	AgentID string
	Cause   error
}

// Error implements the error interface.
func (e *AgentSpendUndecidableError) Error() string {
	msg := ErrAgentSpendUndecidable.Error() + ": agent " + e.AgentID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is support.
func (e *AgentSpendUndecidableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrAgentSpendUndecidable}
	}
	return []error{ErrAgentSpendUndecidable, e.Cause}
}

// checkAgentSpend refuses a spawn of agent when one of its own spend windows
// is spent. An Agent whose limits meter nothing is admitted without reading
// the ledger.
func (s *agentServiceImpl) checkAgentSpend(ctx context.Context, tenantID string, agent Agent) error {
	if !agent.Spend.Metered() {
		return nil
	}
	if s.ledger == nil {
		return &AgentSpendUndecidableError{AgentID: agent.ID, Cause: &QuotaInvalidError{Field: "ledger", Message: "no consumption ledger is wired"}}
	}
	decision, err := NewQuotaAuthority(s.ledger).AdmitScope(ContextWithTenant(ctx, tenantID),
		ScopeAdmitRequest{TenantID: tenantID, AgentID: agent.ID, Limits: agent.Spend}, time.Now().UTC())
	if err != nil {
		return &AgentSpendUndecidableError{AgentID: agent.ID, Cause: err}
	}
	if decision.Allowed {
		return nil
	}
	return &AgentSpendExhaustedError{
		AgentID: agent.ID,
		Window:  decision.Window,
		Axis:    decision.Axis,
		Used:    decision.Used,
		Limit:   decision.Limit,
		RetryAt: decision.RetryAt,
	}
}

// agentSpendRetryAt is when an over-budget Agent's window frees up, or the
// zero time when the error names none.
func agentSpendRetryAt(err error) time.Time {
	var spent *AgentSpendExhaustedError
	if !errors.As(err, &spent) {
		return time.Time{}
	}
	return spent.RetryAt
}

// agentSpendReason states that the Agent is waiting on its own allowance, not
// broken, and says when.
func agentSpendReason(err error) string {
	reason := "the Agent this work runs as has spent the allowance its own spend limits set"
	var spent *AgentSpendExhaustedError
	if errors.As(err, &spent) {
		reason = "Agent " + spent.AgentID + " has spent its own " + spent.Window + " allowance (" +
			strconv.FormatInt(spent.Used, 10) + " of " + strconv.FormatInt(spent.Limit, 10) + " " + spent.Axis + ")"
	}
	reason += ". Nothing is broken — these are the operator's limits. No session was started and this attempt was not charged"
	if retryAt := agentSpendRetryAt(err); !retryAt.IsZero() {
		reason += "; the window frees up at " + retryAt.UTC().Format(time.RFC3339) + " and the task is retried then"
	}
	return reason + ". Raise the Agent's limit or bind the slot to a pool with another Agent."
}
