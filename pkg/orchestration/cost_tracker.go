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

package orchestration

import (
	"context"
	"errors"
	"strconv"
)

// BudgetStatus represents the current budget state.
type BudgetStatus string

const (
	// BudgetOK indicates the job is well within its token budget.
	BudgetOK BudgetStatus = "ok"

	// BudgetWarning indicates the job has consumed more than 80% of its budget.
	BudgetWarning BudgetStatus = "warning"

	// BudgetExceeded indicates the job has exceeded its token budget.
	BudgetExceeded BudgetStatus = "exceeded"

	// budgetWarningThreshold is the fraction of budget usage that triggers a warning.
	budgetWarningThreshold = 0.80
)

// UsageReport contains token usage details for a job, agent, account, or
// project.
type UsageReport struct {
	TotalTokens int64            `json:"total_tokens"`
	BudgetLimit int64            `json:"budget_limit"`
	PercentUsed float64          `json:"percent_used"`
	ByAgent     map[string]int64 `json:"by_agent"`
	LastUpdated int64            `json:"last_updated"`
}

// TurnUsage attributes one agent turn's token consumption across every
// dimension the caller has available for it. JobID/AgentID are the
// pre-existing job-scoped attribution (RecordTokenUsage); AccountID and
// ProjectID are additive dimensions recorded only when non-empty, so a
// caller that does not yet know one of them (e.g. a spawn that used no
// pooled account) simply omits it rather than recording a zero/placeholder
// value.
type TurnUsage struct {
	TenantID  string
	JobID     string
	AgentID   string
	AccountID string
	ProjectID string
	Tokens    int64
}

// CostTracker manages token budget tracking for orchestration jobs.
type CostTracker interface {
	// RecordTokenUsage adds token usage for an agent within a job.
	RecordTokenUsage(ctx context.Context, tenantID, jobID, agentID string, tokens int64) error

	// GetJobUsage returns the total usage report for a job.
	GetJobUsage(ctx context.Context, tenantID, jobID string) (*UsageReport, error)

	// GetAgentUsage returns usage for a specific agent within a job.
	GetAgentUsage(ctx context.Context, tenantID, jobID, agentID string) (*UsageReport, error)

	// CheckBudget checks if the job is within budget.
	CheckBudget(ctx context.Context, tenantID, jobID string) (BudgetStatus, error)

	// RecordTurnUsage attributes one agent turn's token usage to every
	// dimension the caller populated on usage, in a single call: the job
	// (usage.JobID, when set — unchanged, existing behavior via
	// RecordTokenUsage), the pooled account that served the turn
	// (usage.AccountID, when set — see account.go), and the project the turn
	// ran under (usage.ProjectID, when set). A single entry point exists so a
	// caller reporting one turn's tokens cannot record job usage but skip
	// account/project attribution (or vice versa) through a partial call
	// sequence; unset dimensions are simply not recorded, not zero-filled.
	RecordTurnUsage(ctx context.Context, usage TurnUsage) error

	// GetAccountUsage returns the cumulative usage report for a pooled
	// provider account across every job/turn it has ever served.
	GetAccountUsage(ctx context.Context, tenantID, accountID string) (*UsageReport, error)

	// GetProjectUsage returns the cumulative usage report for a project
	// across every job/turn run under it.
	GetProjectUsage(ctx context.Context, tenantID, projectID string) (*UsageReport, error)

	// AccountTokens returns the cumulative tokens recorded against
	// tenantID/accountID. It satisfies AccountUsageLookup so a CostTracker
	// can be handed directly to LeastUsageAccountStrategy.
	AccountTokens(ctx context.Context, tenantID, accountID string) (int64, error)
}

// Sentinel errors for cost tracking operations.
var (
	// ErrBudgetExceeded indicates a job has exceeded its token budget.
	ErrBudgetExceeded = errors.New("cost: budget exceeded")

	// ErrBudgetNotSet indicates no budget has been configured for a job.
	ErrBudgetNotSet = errors.New("cost: budget not set")

	// ErrUsageRecord indicates a failure to record token usage.
	ErrUsageRecord = errors.New("cost: usage record failed")

	// ErrUsageRetrieval indicates a failure to retrieve token usage.
	ErrUsageRetrieval = errors.New("cost: usage retrieval failed")

	// ErrCostInvalidInput indicates invalid input to a cost tracking operation.
	ErrCostInvalidInput = errors.New("cost: invalid input")
)

// BudgetExceededError provides details about a budget that has been exceeded.
type BudgetExceededError struct {
	TenantID    string
	JobID       string
	BudgetLimit int64
	CurrentUsed int64
}

// Error implements the error interface.
func (e *BudgetExceededError) Error() string {
	return "cost: budget exceeded for tenant " + e.TenantID +
		" job " + e.JobID +
		": limit " + strconv.FormatInt(e.BudgetLimit, 10) +
		", used " + strconv.FormatInt(e.CurrentUsed, 10)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BudgetExceededError) Unwrap() error {
	return ErrBudgetExceeded
}

// BudgetNotSetError indicates no budget was configured for the given job.
type BudgetNotSetError struct {
	TenantID string
	JobID    string
}

// Error implements the error interface.
func (e *BudgetNotSetError) Error() string {
	return "cost: budget not set for tenant " + e.TenantID + " job " + e.JobID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BudgetNotSetError) Unwrap() error {
	return ErrBudgetNotSet
}

// UsageRecordError indicates a failure to record token usage.
type UsageRecordError struct {
	TenantID string
	JobID    string
	AgentID  string
	Cause    error
}

// Error implements the error interface.
func (e *UsageRecordError) Error() string {
	base := "cost: failed to record usage for tenant " + e.TenantID +
		" job " + e.JobID +
		" agent " + e.AgentID
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UsageRecordError) Unwrap() error {
	return ErrUsageRecord
}

// UsageRetrievalError indicates a failure to retrieve token usage data.
type UsageRetrievalError struct {
	TenantID string
	JobID    string
	Cause    error
}

// Error implements the error interface.
func (e *UsageRetrievalError) Error() string {
	base := "cost: failed to retrieve usage for tenant " + e.TenantID +
		" job " + e.JobID
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UsageRetrievalError) Unwrap() error {
	return ErrUsageRetrieval
}

// CostInvalidInputError indicates invalid input to a cost tracking operation.
type CostInvalidInputError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *CostInvalidInputError) Error() string {
	return "cost: invalid input for " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *CostInvalidInputError) Unwrap() error {
	return ErrCostInvalidInput
}
