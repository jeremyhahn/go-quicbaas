//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestMCP_UserUpdateSchemaDoesNotOfferDeleting pins that the tool stops
// advertising a state the store refuses.
//
// The schema is what an agent reads to decide what to send, so an enum listing
// `deleting` is not cosmetic: it invites precisely the request UpdateUser now
// rejects, and the caller has no way to know from the schema that it will fail.
func TestMCP_UserUpdateSchemaDoesNotOfferDeleting(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})

	var tool ToolDefinition
	for _, candidate := range reg.ListTools() {
		if candidate.Name == "quicbaas_iam_user_update" {
			tool = candidate
			break
		}
	}
	require.Equal(t, "quicbaas_iam_user_update", tool.Name, "the user update tool is not registered")

	properties, ok := tool.InputSchema["properties"].(map[string]interface{})
	require.True(t, ok)
	lifecycle, ok := properties["lifecycle"].(map[string]interface{})
	require.True(t, ok, "the update tool no longer exposes a lifecycle")
	values, ok := lifecycle["enum"].([]string)
	require.True(t, ok, "the lifecycle enum is not a string list")

	assert.NotContains(t, values, string(iam.LifecycleDeleting),
		"the update tool still offers the deletion workflow's terminal state")
	assert.NotContains(t, lifecycle["description"], "or deleting",
		"the description still offers the state the enum no longer does")
	for _, want := range []string{
		string(iam.LifecyclePending), string(iam.LifecycleActive),
		string(iam.LifecycleSuspended), string(iam.LifecycleLocked),
		string(iam.LifecycleDisabled),
	} {
		assert.Contains(t, values, want, "the update tool stopped offering a legitimate state")
	}
}

// TestMCP_UserUpdateStillForwardsALifecycle pins that narrowing the schema did
// not stop the tool sending the field. The refusal belongs to the store, which
// knows where the record currently is; a transport that dropped the value would
// refuse nothing, change nothing, and answer success.
func TestMCP_UserUpdateStillForwardsALifecycle(t *testing.T) {
	var captured *service.IAMUpdateUserRequest
	mock := &mockIAMService{
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			captured = req
			return &service.IAMUpdateUserResponse{
				User:      &iam.User{ID: req.UserID, TenantID: req.TenantID},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	_, err := reg.executeIAMUserUpdate(context.Background(), map[string]interface{}{
		"tenant_id": "acme",
		"user_id":   "1",
		"lifecycle": string(iam.LifecycleSuspended),
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.Lifecycle, "the MCP update dropped the lifecycle")
	assert.Equal(t, iam.LifecycleSuspended, *captured.Lifecycle)
}
