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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BlockTask
// =============================================================================

func validBlockTaskRequest() *BlockTaskRequest {
	return &BlockTaskRequest{TenantID: "t1", TaskID: "tk1", Kind: "needs_info", Reason: "which branch?"}
}

func TestBlockTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "BlockTask", &BlockTaskResponse{TaskID: "tk1", Kind: "needs_info", Blocked: true}))
	resp, err := c.BlockTask(context.Background(), validBlockTaskRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Blocked)
	assert.Equal(t, "tk1", resp.TaskID)
}

func TestBlockTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.BlockTask(context.Background(), nil)
	require.Error(t, err)

	cases := []struct {
		name string
		req  *BlockTaskRequest
		want string
	}{
		{"tenant", &BlockTaskRequest{TaskID: "tk1", Kind: "blocked", Reason: "r"}, "tenant ID is required"},
		{"task", &BlockTaskRequest{TenantID: "t1", Kind: "blocked", Reason: "r"}, "task ID is required"},
		{"kind", &BlockTaskRequest{TenantID: "t1", TaskID: "tk1", Reason: "r"}, "kind is required"},
		{"reason", &BlockTaskRequest{TenantID: "t1", TaskID: "tk1", Kind: "blocked"}, "reason is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.BlockTask(context.Background(), tc.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestBlockTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "BlockTask", &BlockTaskResponse{}))
	// A nil context reaches the client as a nil interface value, which is
	// what a caller that forgot to thread one passes; it is refused after
	// request validation, before any transport call.
	var noCtx context.Context
	_, err := c.BlockTask(noCtx, validBlockTaskRequest())
	require.Error(t, err)
}

func TestBlockTask_TransportFailures(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(failTransport()).BlockTask(context.Background(), validBlockTaskRequest())
		require.Error(t, err)
	})
	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(nilTransport()).BlockTask(context.Background(), validBlockTaskRequest())
		require.Error(t, err)
	})
	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(mismatchTransport()).BlockTask(context.Background(), validBlockTaskRequest())
		require.Error(t, err)
	})
}

// =============================================================================
// ReadmitTask
// =============================================================================

func TestReadmitTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ReadmitTask", &ReadmitTaskResponse{TaskID: "tk1", Readmitted: true}))
	resp, err := c.ReadmitTask(context.Background(), &ReadmitTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Readmitted)
}

func TestReadmitTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ReadmitTask(context.Background(), nil)
	require.Error(t, err)
	_, err = c.ReadmitTask(context.Background(), &ReadmitTaskRequest{TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
	_, err = c.ReadmitTask(context.Background(), &ReadmitTaskRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID is required")
}

func TestReadmitTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ReadmitTask", &ReadmitTaskResponse{}))
	var noCtx context.Context
	_, err := c.ReadmitTask(noCtx, &ReadmitTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestReadmitTask_TransportFailures(t *testing.T) {
	req := &ReadmitTaskRequest{TenantID: "t1", TaskID: "tk1"}
	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(failTransport()).ReadmitTask(context.Background(), req)
		require.Error(t, err)
	})
	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(nilTransport()).ReadmitTask(context.Background(), req)
		require.Error(t, err)
	})
	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(mismatchTransport()).ReadmitTask(context.Background(), req)
		require.Error(t, err)
	})
}
