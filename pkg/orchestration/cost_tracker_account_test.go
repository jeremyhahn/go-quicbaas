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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKVCostTracker_RecordTurnUsage_AttributesAccountAndProject(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	err := tracker.RecordTurnUsage(ctx, TurnUsage{
		TenantID:  "tenant-a",
		JobID:     "job-1",
		AgentID:   "agent-1",
		AccountID: "acct-1",
		ProjectID: "proj-1",
		Tokens:    250,
	})
	require.NoError(t, err)

	jobUsage, err := tracker.GetJobUsage(ctx, "tenant-a", "job-1")
	require.NoError(t, err)
	assert.Equal(t, int64(250), jobUsage.TotalTokens)

	acctUsage, err := tracker.GetAccountUsage(ctx, "tenant-a", "acct-1")
	require.NoError(t, err)
	assert.Equal(t, int64(250), acctUsage.TotalTokens)

	projUsage, err := tracker.GetProjectUsage(ctx, "tenant-a", "proj-1")
	require.NoError(t, err)
	assert.Equal(t, int64(250), projUsage.TotalTokens)
}

func TestKVCostTracker_RecordTurnUsage_AccumulatesAcrossTurns(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-1", Tokens: 100})
		require.NoError(t, err)
	}

	usage, err := tracker.GetAccountUsage(ctx, "tenant-a", "acct-1")
	require.NoError(t, err)
	assert.Equal(t, int64(300), usage.TotalTokens)
}

func TestKVCostTracker_RecordTurnUsage_OmitsUnsetDimensions(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	// No AccountID, no ProjectID, no JobID/AgentID: only Tokens/TenantID set.
	// Must not fabricate a placeholder attribution for any dimension.
	err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", Tokens: 100})
	require.NoError(t, err)

	usage, err := tracker.GetAccountUsage(ctx, "tenant-a", "acct-never-named")
	require.NoError(t, err)
	assert.Equal(t, int64(0), usage.TotalTokens, "no account was named on the turn, so no account should have accrued usage")
}

func TestKVCostTracker_RecordTurnUsage_InvalidInput(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	err := tracker.RecordTurnUsage(ctx, TurnUsage{AccountID: "acct-1", Tokens: 100})
	assert.Error(t, err, "missing tenant_id must be rejected")

	err = tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-1", Tokens: 0})
	assert.Error(t, err, "non-positive tokens must be rejected")
}

func TestKVCostTracker_GetAccountUsage_TenantIsolation(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()

	require.NoError(t, tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-1", Tokens: 500}))

	// Same account ID string under a DIFFERENT tenant must read as unused —
	// the key is namespaced by tenant, not merely by account ID.
	usage, err := tracker.GetAccountUsage(ctx, "tenant-b", "acct-1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), usage.TotalTokens)
}

func TestKVCostTracker_GetAccountUsage_NoUsageYet(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	usage, err := tracker.GetAccountUsage(context.Background(), "tenant-a", "never-used")
	require.NoError(t, err)
	assert.Equal(t, int64(0), usage.TotalTokens)
}

func TestKVCostTracker_AccountTokens_SatisfiesUsageLookup(t *testing.T) {
	store := newCostTestKVStore()
	tracker := NewCostTracker(store, &costTestLogger{})
	ctx := context.Background()
	require.NoError(t, tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-1", Tokens: 42}))

	var lookup AccountUsageLookup = tracker
	tokens, err := lookup.AccountTokens(ctx, "tenant-a", "acct-1")
	require.NoError(t, err)
	assert.Equal(t, int64(42), tokens)
}
