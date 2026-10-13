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
	"encoding/binary"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Key prefix constants for cost tracking KV keys.
const (
	costKeyPrefix  = "cost:"
	jobSegment     = ":job:"
	agentSegment   = ":agent:"
	totalSuffix    = ":total"
	budgetSuffix   = ":budget"
	accountSegment = "cost:account:"
	projectSegment = "cost:project:"
)

// kvCostTracker is a KV-backed implementation of CostTracker.
type kvCostTracker struct {
	store       interfaces.KVStore
	logger      interfaces.Logger
	budgetCache sync.Map // caches budget limits to reduce KV reads
}

// NewCostTracker creates a new CostTracker backed by a KVStore.
func NewCostTracker(store interfaces.KVStore, logger interfaces.Logger) CostTracker {
	return &kvCostTracker{
		store:  store,
		logger: logger,
	}
}

// RecordTokenUsage adds token usage for an agent within a job.
func (ct *kvCostTracker) RecordTokenUsage(ctx context.Context, tenantID, jobID, agentID string, tokens int64) error {
	if err := ct.validateRecordInput(tenantID, jobID, agentID, tokens); err != nil {
		return err
	}

	// Update job total
	totalKey := ct.jobTotalKey(tenantID, jobID)
	if err := ct.incrementCounter(ctx, totalKey, tokens); err != nil {
		return &UsageRecordError{
			TenantID: tenantID,
			JobID:    jobID,
			AgentID:  agentID,
			Cause:    err,
		}
	}

	// Update per-agent counter
	agentKey := ct.agentKey(tenantID, jobID, agentID)
	if err := ct.incrementCounter(ctx, agentKey, tokens); err != nil {
		return &UsageRecordError{
			TenantID: tenantID,
			JobID:    jobID,
			AgentID:  agentID,
			Cause:    err,
		}
	}

	ct.logger.WithFields(map[string]interface{}{
		"tenant_id": tenantID,
		"job_id":    jobID,
		"agent_id":  agentID,
		"tokens":    tokens,
	}).Debug("recorded token usage")

	return nil
}

// GetJobUsage returns the total usage report for a job.
func (ct *kvCostTracker) GetJobUsage(ctx context.Context, tenantID, jobID string) (*UsageReport, error) {
	if tenantID == "" {
		return nil, &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if jobID == "" {
		return nil, &CostInvalidInputError{Field: "job_id", Message: "must not be empty"}
	}

	// Read job total
	totalKey := ct.jobTotalKey(tenantID, jobID)
	total, err := ct.readCounter(ctx, totalKey)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, &UsageRetrievalError{
				TenantID: tenantID,
				JobID:    jobID,
				Cause:    &errors.MessageError{Message: "job usage not found"},
			}
		}
		return nil, &UsageRetrievalError{
			TenantID: tenantID,
			JobID:    jobID,
			Cause:    err,
		}
	}

	// Read budget limit
	budgetLimit, _ := ct.readBudgetLimit(ctx, tenantID, jobID)

	// Collect per-agent usage
	agentUsage, err := ct.collectAgentUsage(ctx, tenantID, jobID)
	if err != nil {
		return nil, &UsageRetrievalError{
			TenantID: tenantID,
			JobID:    jobID,
			Cause:    err,
		}
	}

	var percentUsed float64
	if budgetLimit > 0 {
		percentUsed = float64(total) / float64(budgetLimit) * 100.0
	}

	return &UsageReport{
		TotalTokens: total,
		BudgetLimit: budgetLimit,
		PercentUsed: percentUsed,
		ByAgent:     agentUsage,
		LastUpdated: time.Now().UnixNano(),
	}, nil
}

// GetAgentUsage returns usage for a specific agent within a job.
func (ct *kvCostTracker) GetAgentUsage(ctx context.Context, tenantID, jobID, agentID string) (*UsageReport, error) {
	if tenantID == "" {
		return nil, &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if jobID == "" {
		return nil, &CostInvalidInputError{Field: "job_id", Message: "must not be empty"}
	}
	if agentID == "" {
		return nil, &CostInvalidInputError{Field: "agent_id", Message: "must not be empty"}
	}

	agentKey := ct.agentKey(tenantID, jobID, agentID)
	agentTokens, err := ct.readCounter(ctx, agentKey)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, &UsageRetrievalError{
				TenantID: tenantID,
				JobID:    jobID,
				Cause:    &errors.MessageError{Message: "agent usage not found for " + agentID},
			}
		}
		return nil, &UsageRetrievalError{
			TenantID: tenantID,
			JobID:    jobID,
			Cause:    err,
		}
	}

	// Read budget limit for the job
	budgetLimit, _ := ct.readBudgetLimit(ctx, tenantID, jobID)

	var percentUsed float64
	if budgetLimit > 0 {
		percentUsed = float64(agentTokens) / float64(budgetLimit) * 100.0
	}

	return &UsageReport{
		TotalTokens: agentTokens,
		BudgetLimit: budgetLimit,
		PercentUsed: percentUsed,
		ByAgent:     map[string]int64{agentID: agentTokens},
		LastUpdated: time.Now().UnixNano(),
	}, nil
}

// CheckBudget checks if the job is within budget.
func (ct *kvCostTracker) CheckBudget(ctx context.Context, tenantID, jobID string) (BudgetStatus, error) {
	if tenantID == "" {
		return "", &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if jobID == "" {
		return "", &CostInvalidInputError{Field: "job_id", Message: "must not be empty"}
	}

	// Read budget limit (check cache first)
	budgetLimit, err := ct.readBudgetLimit(ctx, tenantID, jobID)
	if err != nil {
		return "", &BudgetNotSetError{
			TenantID: tenantID,
			JobID:    jobID,
		}
	}

	if budgetLimit <= 0 {
		return "", &BudgetNotSetError{
			TenantID: tenantID,
			JobID:    jobID,
		}
	}

	// Read current total
	totalKey := ct.jobTotalKey(tenantID, jobID)
	total, err := ct.readCounter(ctx, totalKey)
	if err != nil {
		// If not found, the job hasn't recorded any usage yet (total = 0)
		if errors.IsNotFound(err) {
			return BudgetOK, nil
		}
		return "", &UsageRetrievalError{
			TenantID: tenantID,
			JobID:    jobID,
			Cause:    err,
		}
	}

	// Determine status
	if total >= budgetLimit {
		return BudgetExceeded, nil
	}

	ratio := float64(total) / float64(budgetLimit)
	if ratio >= budgetWarningThreshold {
		return BudgetWarning, nil
	}

	return BudgetOK, nil
}

// incrementCounter reads the current value from a KV key, adds delta, and writes it back.
func (ct *kvCostTracker) incrementCounter(ctx context.Context, key string, delta int64) error {
	current, err := ct.readCounter(ctx, key)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		current = 0
	}

	newValue := current + delta
	return ct.store.Put(ctx, key, int64ToBytes(newValue))
}

// readCounter reads an int64 counter from the KV store.
func (ct *kvCostTracker) readCounter(ctx context.Context, key string) (int64, error) {
	data, err := ct.store.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	return bytesToInt64(data)
}

// readBudgetLimit reads the budget limit for a job, checking the cache first.
func (ct *kvCostTracker) readBudgetLimit(ctx context.Context, tenantID, jobID string) (int64, error) {
	cacheKey := tenantID + ":" + jobID

	// Check cache first
	if cached, ok := ct.budgetCache.Load(cacheKey); ok {
		limit, valid := cached.(int64)
		if valid {
			return limit, nil
		}
	}

	// Read from KV
	budgetKey := ct.budgetKey(tenantID, jobID)
	limit, err := ct.readCounter(ctx, budgetKey)
	if err != nil {
		return 0, err
	}

	// Cache the result
	ct.budgetCache.Store(cacheKey, limit)

	return limit, nil
}

// collectAgentUsage scans for per-agent usage keys under a job and returns the breakdown.
func (ct *kvCostTracker) collectAgentUsage(ctx context.Context, tenantID, jobID string) (map[string]int64, error) {
	prefix := costKeyPrefix + tenantID + jobSegment + jobID + agentSegment
	agentUsage := make(map[string]int64)

	err := ct.store.Scan(ctx, prefix, func(key string, value []byte) error {
		// Extract agent ID from key: cost:{tenantID}:job:{jobID}:agent:{agentID}
		agentID := key[len(prefix):]
		if agentID == "" {
			return nil
		}

		tokens, parseErr := bytesToInt64(value)
		if parseErr != nil {
			ct.logger.WithFields(map[string]interface{}{
				"key":   key,
				"error": parseErr.Error(),
			}).Warn("skipping corrupted agent usage entry")
			return nil
		}

		agentUsage[agentID] = tokens
		return nil
	})

	if err != nil {
		return nil, err
	}

	return agentUsage, nil
}

// validateRecordInput validates the inputs for RecordTokenUsage.
func (ct *kvCostTracker) validateRecordInput(tenantID, jobID, agentID string, tokens int64) error {
	if tenantID == "" {
		return &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if jobID == "" {
		return &CostInvalidInputError{Field: "job_id", Message: "must not be empty"}
	}
	if agentID == "" {
		return &CostInvalidInputError{Field: "agent_id", Message: "must not be empty"}
	}
	if tokens <= 0 {
		return &CostInvalidInputError{Field: "tokens", Message: "must be positive"}
	}
	return nil
}

// Key construction helpers.

func (ct *kvCostTracker) jobTotalKey(tenantID, jobID string) string {
	return costKeyPrefix + tenantID + jobSegment + jobID + totalSuffix
}

func (ct *kvCostTracker) agentKey(tenantID, jobID, agentID string) string {
	return costKeyPrefix + tenantID + jobSegment + jobID + agentSegment + agentID
}

func (ct *kvCostTracker) budgetKey(tenantID, jobID string) string {
	return costKeyPrefix + tenantID + jobSegment + jobID + budgetSuffix
}

func (ct *kvCostTracker) accountKey(tenantID, accountID string) string {
	return accountSegment + tenantID + ":" + accountID
}

func (ct *kvCostTracker) projectKey(tenantID, projectID string) string {
	return projectSegment + tenantID + ":" + projectID
}

// =============================================================================
// Account / Project attribution
// =============================================================================

// RecordTurnUsage attributes usage.Tokens to every populated dimension of
// usage in one call. See the CostTracker.RecordTurnUsage doc comment for why
// a single entry point exists instead of requiring the caller to sequence
// several Record* calls.
func (ct *kvCostTracker) RecordTurnUsage(ctx context.Context, usage TurnUsage) error {
	if usage.TenantID == "" {
		return &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if usage.Tokens <= 0 {
		return &CostInvalidInputError{Field: "tokens", Message: "must be positive"}
	}

	if usage.JobID != "" && usage.AgentID != "" {
		if err := ct.RecordTokenUsage(ctx, usage.TenantID, usage.JobID, usage.AgentID, usage.Tokens); err != nil {
			return err
		}
	}
	if usage.AccountID != "" {
		if err := ct.incrementCounter(ctx, ct.accountKey(usage.TenantID, usage.AccountID), usage.Tokens); err != nil {
			return &UsageRecordError{TenantID: usage.TenantID, JobID: usage.JobID, AgentID: usage.AgentID, Cause: err}
		}
	}
	if usage.ProjectID != "" {
		if err := ct.incrementCounter(ctx, ct.projectKey(usage.TenantID, usage.ProjectID), usage.Tokens); err != nil {
			return &UsageRecordError{TenantID: usage.TenantID, JobID: usage.JobID, AgentID: usage.AgentID, Cause: err}
		}
	}
	return nil
}

// GetAccountUsage returns the cumulative usage report for a pooled provider
// account.
func (ct *kvCostTracker) GetAccountUsage(ctx context.Context, tenantID, accountID string) (*UsageReport, error) {
	if tenantID == "" {
		return nil, &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if accountID == "" {
		return nil, &CostInvalidInputError{Field: "account_id", Message: "must not be empty"}
	}
	total, err := ct.readCounter(ctx, ct.accountKey(tenantID, accountID))
	if err != nil {
		if errors.IsNotFound(err) {
			return &UsageReport{LastUpdated: time.Now().UnixNano()}, nil
		}
		return nil, &UsageRetrievalError{TenantID: tenantID, Cause: err}
	}
	return &UsageReport{TotalTokens: total, LastUpdated: time.Now().UnixNano()}, nil
}

// GetProjectUsage returns the cumulative usage report for a project.
func (ct *kvCostTracker) GetProjectUsage(ctx context.Context, tenantID, projectID string) (*UsageReport, error) {
	if tenantID == "" {
		return nil, &CostInvalidInputError{Field: "tenant_id", Message: "must not be empty"}
	}
	if projectID == "" {
		return nil, &CostInvalidInputError{Field: "project_id", Message: "must not be empty"}
	}
	total, err := ct.readCounter(ctx, ct.projectKey(tenantID, projectID))
	if err != nil {
		if errors.IsNotFound(err) {
			return &UsageReport{LastUpdated: time.Now().UnixNano()}, nil
		}
		return nil, &UsageRetrievalError{TenantID: tenantID, Cause: err}
	}
	return &UsageReport{TotalTokens: total, LastUpdated: time.Now().UnixNano()}, nil
}

// AccountTokens implements AccountUsageLookup so a CostTracker can be handed
// directly to LeastUsageAccountStrategy.
func (ct *kvCostTracker) AccountTokens(ctx context.Context, tenantID, accountID string) (int64, error) {
	report, err := ct.GetAccountUsage(ctx, tenantID, accountID)
	if err != nil {
		return 0, err
	}
	return report.TotalTokens, nil
}

// int64ToBytes encodes an int64 as a big-endian byte slice.
func int64ToBytes(v int64) []byte {
	buf := make([]byte, 8)
	// int64 and uint64 share a width, so the conversion reinterprets the
	// two's-complement bits losslessly and bytesToInt64 reverses it exactly.
	binary.BigEndian.PutUint64(buf, uint64(v))
	return buf
}

// bytesToInt64 decodes a big-endian byte slice into an int64.
func bytesToInt64(data []byte) (int64, error) {
	if len(data) != 8 {
		return 0, &CostInvalidInputError{
			Field:   "counter_value",
			Message: "expected 8 bytes for int64 encoding",
		}
	}
	return int64(binary.BigEndian.Uint64(data)), nil
}
