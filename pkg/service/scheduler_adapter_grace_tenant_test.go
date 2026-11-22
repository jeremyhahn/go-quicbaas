package service

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// recordingGraceEngine returns a scheduler mock that records each query it
// is asked to list.
func recordingGraceEngine(queries *[]*scheduler.JobQuery) *covMockSchedulerEngine {
	return &covMockSchedulerEngine{
		listJobsFn: func(_ context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			q := *query
			*queries = append(*queries, &q)
			return nil, "", nil
		},
	}
}

func TestGetCompletedGracePeriodVersions_IdentityOnlyContextReadsItsTenant(t *testing.T) {
	var queries []*scheduler.JobQuery
	adapter := &SchedulerAdapter{scheduler: recordingGraceEngine(&queries)}

	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "u", TenantID: "acme"})
	_, err := adapter.GetCompletedGracePeriodVersions(ctx, "test-key")
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "acme", queries[0].OwnerID)
}

func TestGetCompletedGracePeriodVersions_UnboundContextRefused(t *testing.T) {
	var queries []*scheduler.JobQuery
	adapter := &SchedulerAdapter{scheduler: recordingGraceEngine(&queries)}

	result, err := adapter.GetCompletedGracePeriodVersions(context.Background(), "test-key")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, queries, "listJobsFn must not be called for an unbound context")
}

func TestGetCompletedGracePeriodVersions_SystemTenantReadsSystemNamespace(t *testing.T) {
	var queries []*scheduler.JobQuery
	adapter := &SchedulerAdapter{scheduler: recordingGraceEngine(&queries)}

	_, err := adapter.GetCompletedGracePeriodVersions(systemTenantCtx(), "test-key")
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "", queries[0].OwnerID)
}
