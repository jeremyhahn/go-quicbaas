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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// In-Memory KVStore for Testing
// =============================================================================

// costTestKVStore is a simple in-memory KVStore for cost tracker unit testing.
type costTestKVStore struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newCostTestKVStore() *costTestKVStore {
	return &costTestKVStore{
		data: make(map[string][]byte),
	}
}

func (m *costTestKVStore) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *costTestKVStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, &qerrors.QBError{
			Code: qerrors.ErrNotFound,
			Op:   "costTestKVStore.Get",
			Err:  &qerrors.MessageError{Message: "key not found: " + key},
		}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *costTestKVStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *costTestKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *costTestKVStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *costTestKVStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// =============================================================================
// Test Logger
// =============================================================================

// costTestLogger implements interfaces.Logger for cost tracker tests.
type costTestLogger struct{}

func (l *costTestLogger) Debug(string, ...any)                                {}
func (l *costTestLogger) Debugf(string, ...interface{})                       {}
func (l *costTestLogger) Info(string, ...any)                                 {}
func (l *costTestLogger) Infof(string, ...interface{})                        {}
func (l *costTestLogger) Warn(string, ...any)                                 {}
func (l *costTestLogger) Warnf(string, ...interface{})                        {}
func (l *costTestLogger) Error(string, ...any)                                {}
func (l *costTestLogger) Errorf(string, ...interface{})                       {}
func (l *costTestLogger) Fatal(string)                                        {}
func (l *costTestLogger) Fatalf(string, ...interface{})                       {}
func (l *costTestLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l *costTestLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l *costTestLogger) WithError(error) interfaces.Logger                   { return l }

// =============================================================================
// Helper: seed budget in KV store
// =============================================================================

func seedBudget(t *testing.T, store *costTestKVStore, tenantID, jobID string, limit int64) {
	t.Helper()
	key := costKeyPrefix + tenantID + jobSegment + jobID + budgetSuffix
	err := store.Put(context.Background(), key, int64ToBytes(limit))
	require.NoError(t, err)
}

// =============================================================================
// RecordTokenUsage Tests
// =============================================================================

func TestRecordTokenUsage_Success(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 100)
	require.NoError(t, err)

	// Verify job total was written
	totalKey := costKeyPrefix + "tenant-1" + jobSegment + "job-1" + totalSuffix
	data, getErr := store.Get(ctx, totalKey)
	require.NoError(t, getErr)
	val, parseErr := bytesToInt64(data)
	require.NoError(t, parseErr)
	assert.Equal(t, int64(100), val)

	// Verify per-agent counter was written
	agentKey := costKeyPrefix + "tenant-1" + jobSegment + "job-1" + agentSegment + "agent-a"
	data, getErr = store.Get(ctx, agentKey)
	require.NoError(t, getErr)
	val, parseErr = bytesToInt64(data)
	require.NoError(t, parseErr)
	assert.Equal(t, int64(100), val)

	// Record more usage and verify accumulation
	err = tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 50)
	require.NoError(t, err)

	data, getErr = store.Get(ctx, totalKey)
	require.NoError(t, getErr)
	val, parseErr = bytesToInt64(data)
	require.NoError(t, parseErr)
	assert.Equal(t, int64(150), val)
}

func TestRecordTokenUsage_InvalidInput(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	tests := []struct {
		name     string
		tenantID string
		jobID    string
		agentID  string
		tokens   int64
	}{
		{
			name:     "empty tenant ID",
			tenantID: "",
			jobID:    "job-1",
			agentID:  "agent-a",
			tokens:   100,
		},
		{
			name:     "empty job ID",
			tenantID: "tenant-1",
			jobID:    "",
			agentID:  "agent-a",
			tokens:   100,
		},
		{
			name:     "empty agent ID",
			tenantID: "tenant-1",
			jobID:    "job-1",
			agentID:  "",
			tokens:   100,
		},
		{
			name:     "zero tokens",
			tenantID: "tenant-1",
			jobID:    "job-1",
			agentID:  "agent-a",
			tokens:   0,
		},
		{
			name:     "negative tokens",
			tenantID: "tenant-1",
			jobID:    "job-1",
			agentID:  "agent-a",
			tokens:   -10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tracker.RecordTokenUsage(ctx, tt.tenantID, tt.jobID, tt.agentID, tt.tokens)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrCostInvalidInput))
		})
	}
}

// =============================================================================
// CheckBudget Tests
// =============================================================================

func TestCheckBudget_OK(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record some usage (well under 80%)
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 1000)
	require.NoError(t, err)

	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetOK, status)
}

func TestCheckBudget_Warning(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record usage to trigger warning (85%)
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 8500)
	require.NoError(t, err)

	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetWarning, status)
}

func TestCheckBudget_Exceeded(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record usage exceeding budget
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 10000)
	require.NoError(t, err)

	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetExceeded, status)

	// Also test exceeding by a large margin
	err = tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 5000)
	require.NoError(t, err)

	status, err = tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetExceeded, status)
}

func TestCheckBudget_NoBudgetSet(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	// Do not set a budget
	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.Error(t, err)
	assert.Equal(t, BudgetStatus(""), status)
	assert.True(t, errors.Is(err, ErrBudgetNotSet))
}

func TestCheckBudget_InvalidInput(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	_, err := tracker.CheckBudget(ctx, "", "job-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))

	_, err = tracker.CheckBudget(ctx, "tenant-1", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))
}

func TestCheckBudget_NoUsageYet(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// No usage recorded - should be OK
	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetOK, status)
}

// =============================================================================
// GetJobUsage Tests
// =============================================================================

func TestGetJobUsage_Success(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record usage from multiple agents
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 300)
	require.NoError(t, err)
	err = tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-b", 700)
	require.NoError(t, err)

	report, err := tracker.GetJobUsage(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, int64(1000), report.TotalTokens)
	assert.Equal(t, int64(10000), report.BudgetLimit)
	assert.InDelta(t, 10.0, report.PercentUsed, 0.01)
	assert.Equal(t, int64(300), report.ByAgent["agent-a"])
	assert.Equal(t, int64(700), report.ByAgent["agent-b"])
	assert.Greater(t, report.LastUpdated, int64(0))
}

func TestGetJobUsage_NotFound(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	// No usage recorded at all
	report, err := tracker.GetJobUsage(ctx, "tenant-1", "nonexistent-job")
	require.Error(t, err)
	assert.Nil(t, report)
	assert.True(t, errors.Is(err, ErrUsageRetrieval))
}

func TestGetJobUsage_InvalidInput(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	_, err := tracker.GetJobUsage(ctx, "", "job-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))

	_, err = tracker.GetJobUsage(ctx, "tenant-1", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))
}

func TestGetJobUsage_NoBudgetSet(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	// Record usage without setting a budget
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 500)
	require.NoError(t, err)

	report, err := tracker.GetJobUsage(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, int64(500), report.TotalTokens)
	assert.Equal(t, int64(0), report.BudgetLimit)
	assert.Equal(t, float64(0), report.PercentUsed) // no budget = 0% used
}

// =============================================================================
// GetAgentUsage Tests
// =============================================================================

func TestGetAgentUsage_Success(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 250)
	require.NoError(t, err)

	report, err := tracker.GetAgentUsage(ctx, "tenant-1", "job-1", "agent-a")
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, int64(250), report.TotalTokens)
	assert.Equal(t, int64(10000), report.BudgetLimit)
	assert.InDelta(t, 2.5, report.PercentUsed, 0.01)
	assert.Equal(t, int64(250), report.ByAgent["agent-a"])
}

func TestGetAgentUsage_NotFound(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	report, err := tracker.GetAgentUsage(ctx, "tenant-1", "job-1", "nonexistent-agent")
	require.Error(t, err)
	assert.Nil(t, report)
	assert.True(t, errors.Is(err, ErrUsageRetrieval))
}

func TestGetAgentUsage_InvalidInput(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	_, err := tracker.GetAgentUsage(ctx, "", "job-1", "agent-a")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))

	_, err = tracker.GetAgentUsage(ctx, "tenant-1", "", "agent-a")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))

	_, err = tracker.GetAgentUsage(ctx, "tenant-1", "job-1", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestBudgetExceededError(t *testing.T) {
	err := &BudgetExceededError{
		TenantID:    "tenant-1",
		JobID:       "job-1",
		BudgetLimit: 10000,
		CurrentUsed: 12000,
	}

	assert.Contains(t, err.Error(), "budget exceeded")
	assert.Contains(t, err.Error(), "tenant-1")
	assert.Contains(t, err.Error(), "job-1")
	assert.True(t, errors.Is(err, ErrBudgetExceeded))
}

func TestBudgetNotSetError(t *testing.T) {
	err := &BudgetNotSetError{
		TenantID: "tenant-1",
		JobID:    "job-1",
	}

	assert.Contains(t, err.Error(), "budget not set")
	assert.Contains(t, err.Error(), "tenant-1")
	assert.True(t, errors.Is(err, ErrBudgetNotSet))
}

func TestUsageRecordError(t *testing.T) {
	cause := &qerrors.MessageError{Message: "kv write failed"}
	err := &UsageRecordError{
		TenantID: "tenant-1",
		JobID:    "job-1",
		AgentID:  "agent-a",
		Cause:    cause,
	}

	assert.Contains(t, err.Error(), "failed to record usage")
	assert.Contains(t, err.Error(), "agent-a")
	assert.True(t, errors.Is(err, ErrUsageRecord))
}

func TestUsageRetrievalError(t *testing.T) {
	cause := &qerrors.MessageError{Message: "kv read failed"}
	err := &UsageRetrievalError{
		TenantID: "tenant-1",
		JobID:    "job-1",
		Cause:    cause,
	}

	assert.Contains(t, err.Error(), "failed to retrieve usage")
	assert.Contains(t, err.Error(), "tenant-1")
	assert.True(t, errors.Is(err, ErrUsageRetrieval))
}

func TestCostInvalidInputError(t *testing.T) {
	err := &CostInvalidInputError{
		Field:   "tenant_id",
		Message: "must not be empty",
	}

	assert.Contains(t, err.Error(), "invalid input")
	assert.Contains(t, err.Error(), "tenant_id")
	assert.True(t, errors.Is(err, ErrCostInvalidInput))
}

// =============================================================================
// Encoding Tests
// =============================================================================

func TestInt64ToBytes_RoundTrip(t *testing.T) {
	tests := []int64{0, 1, -1, 100, 9999999, -9999999, 1 << 62, -(1 << 62)}

	for _, v := range tests {
		encoded := int64ToBytes(v)
		decoded, err := bytesToInt64(encoded)
		require.NoError(t, err)
		assert.Equal(t, v, decoded)
	}
}

func TestBytesToInt64_InvalidLength(t *testing.T) {
	_, err := bytesToInt64([]byte{1, 2, 3})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCostInvalidInput))
}

func TestInt64FormatInBudgetExceededError(t *testing.T) {
	err := &BudgetExceededError{
		TenantID:    "t1",
		JobID:       "j1",
		BudgetLimit: 12345,
		CurrentUsed: -1,
	}
	assert.Contains(t, err.Error(), "12345")
	assert.Contains(t, err.Error(), "-1")
}

// =============================================================================
// Budget Cache Tests
// =============================================================================

func TestCheckBudget_CachesBudgetLimit(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 100)
	require.NoError(t, err)

	// First check reads from KV
	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetOK, status)

	// Delete budget from KV - cache should still serve the value
	budgetKey := costKeyPrefix + "tenant-1" + jobSegment + "job-1" + budgetSuffix
	deleteErr := store.Delete(ctx, budgetKey)
	require.NoError(t, deleteErr)

	// Second check should still work (from cache)
	status, err = tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetOK, status)
}

// =============================================================================
// Multiple Agent Accumulation Tests
// =============================================================================

func TestRecordTokenUsage_MultipleAgents(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	// Record usage from different agents
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 100)
	require.NoError(t, err)
	err = tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-b", 200)
	require.NoError(t, err)
	err = tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-c", 300)
	require.NoError(t, err)

	// Verify total is sum of all agents
	totalKey := costKeyPrefix + "tenant-1" + jobSegment + "job-1" + totalSuffix
	data, getErr := store.Get(ctx, totalKey)
	require.NoError(t, getErr)
	total, parseErr := bytesToInt64(data)
	require.NoError(t, parseErr)
	assert.Equal(t, int64(600), total)

	// Verify individual agent counters
	report, err := tracker.GetJobUsage(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, int64(100), report.ByAgent["agent-a"])
	assert.Equal(t, int64(200), report.ByAgent["agent-b"])
	assert.Equal(t, int64(300), report.ByAgent["agent-c"])
}

// =============================================================================
// Budget Boundary Tests
// =============================================================================

func TestCheckBudget_ExactlyAtWarningThreshold(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record exactly 80% usage (at the threshold boundary)
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 8000)
	require.NoError(t, err)

	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetWarning, status)
}

func TestCheckBudget_JustBelowWarningThreshold(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	seedBudget(t, store, "tenant-1", "job-1", 10000)

	// Record 79.99% usage (just below threshold)
	err := tracker.RecordTokenUsage(ctx, "tenant-1", "job-1", "agent-a", 7999)
	require.NoError(t, err)

	status, err := tracker.CheckBudget(ctx, "tenant-1", "job-1")
	require.NoError(t, err)
	assert.Equal(t, BudgetOK, status)
}
