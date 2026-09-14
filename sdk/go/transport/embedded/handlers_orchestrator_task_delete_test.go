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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestEmbedded_DeleteTask_ForwardsToAgentService(t *testing.T) {
	fake := &fakeAgentService{}

	resp, err := handleOrchestratorDeleteTask(context.Background(), &orchMock{agent: fake},
		&orchestratorSDK.DeleteTaskRequest{TenantID: "t1", TaskID: "task-1"})
	require.NoError(t, err)
	out, ok := resp.(*orchestratorSDK.DeleteTaskResponse)
	require.True(t, ok)
	assert.True(t, out.Deleted)
	assert.Equal(t, "task-1", out.TaskID)
	require.Len(t, fake.deleteCalls, 1)
	assert.Equal(t, "t1", fake.deleteCalls[0].TenantID)
	assert.Equal(t, "task-1", fake.deleteCalls[0].TaskID)
}

func TestEmbedded_DeleteTask_Errors(t *testing.T) {
	t.Run("wrong request type", func(t *testing.T) {
		_, err := handleOrchestratorDeleteTask(context.Background(), &orchMock{agent: &fakeAgentService{}}, 42)
		require.Error(t, err)
	})
	t.Run("no agent service", func(t *testing.T) {
		_, err := handleOrchestratorDeleteTask(context.Background(), &orchMock{}, &orchestratorSDK.DeleteTaskRequest{})
		require.Error(t, err)
	})
	t.Run("service error propagates", func(t *testing.T) {
		_, err := handleOrchestratorDeleteTask(context.Background(), &orchMock{agent: &fakeAgentService{fail: true}},
			&orchestratorSDK.DeleteTaskRequest{TaskID: "x"})
		require.ErrorIs(t, err, errOrch)
	})
}

// TestEmbedded_DeleteTask_IsDispatchable proves the method is in the embedded
// transport's routing table; a handler nothing routes to is unreachable.
func TestEmbedded_DeleteTask_IsDispatchable(t *testing.T) {
	_, ok := unaryHandlers["quicbaas.Orchestrator/DeleteTask"]
	assert.True(t, ok, "DeleteTask is not routed by the embedded transport")
}
