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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// DeleteTask
// =============================================================================

func validDeleteTaskRequest() *DeleteTaskRequest {
	return &DeleteTaskRequest{TenantID: "t1", TaskID: "tk1"}
}

func TestDeleteTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "DeleteTask", &DeleteTaskResponse{TaskID: "tk1", Deleted: true}))
	resp, err := c.DeleteTask(context.Background(), validDeleteTaskRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, "tk1", resp.TaskID)
}

func TestDeleteTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.DeleteTask(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is required")

	_, err = c.DeleteTask(context.Background(), &DeleteTaskRequest{TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")

	_, err = c.DeleteTask(context.Background(), &DeleteTaskRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID is required")
}

func TestDeleteTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "DeleteTask", &DeleteTaskResponse{}))
	var noCtx context.Context
	_, err := c.DeleteTask(noCtx, validDeleteTaskRequest())
	require.Error(t, err)
}

func TestDeleteTask_TransportFailures(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(failTransport()).DeleteTask(context.Background(), validDeleteTaskRequest())
		require.Error(t, err)
	})
	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(nilTransport()).DeleteTask(context.Background(), validDeleteTaskRequest())
		require.Error(t, err)
	})
	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(mismatchTransport()).DeleteTask(context.Background(), validDeleteTaskRequest())
		require.Error(t, err)
	})
}
