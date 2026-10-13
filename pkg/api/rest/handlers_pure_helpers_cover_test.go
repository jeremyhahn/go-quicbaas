//go:build rest

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

package rest

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestWriteSSEError emits a terminal error frame and verifies the SSE wire
// format (event header + data line). The flush argument is a no-op closure
// since the recorder buffers everything in memory.
func TestWriteSSEError(t *testing.T) {
	rec := httptest.NewRecorder()

	err := writeSSEError(rec, func() error { return nil }, "stream aborted")

	assert.NoError(t, err)
	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "event: error"), "missing error event header: %q", body)
	assert.True(t, strings.Contains(body, "data: stream aborted"), "missing error data line: %q", body)
}

func TestWriteSSEError_FlushFailureReturned(t *testing.T) {
	flushErr := errors.New("connection reset")

	err := writeSSEError(httptest.NewRecorder(), func() error { return flushErr }, "stream aborted")

	assert.ErrorIs(t, err, flushErr)
}

// TestProjectToREST_Branches covers the nil-input guard and the populated
// conversion path including the FleetNodeRoles permission matrix branch.
func TestProjectToREST_Branches(t *testing.T) {
	t.Run("nil project returns nil", func(t *testing.T) {
		assert.Nil(t, projectToREST(nil))
	})

	t.Run("populated project maps every field including agent roles", func(t *testing.T) {
		p := &orchestration.Project{
			ID:               "proj-1",
			TenantID:         "tenant-1",
			Name:             "Demo",
			Description:      "demo project",
			HostAffinity:     "host-a",
			DefaultLeadAgent: orchestration.AgentRefTo("agent-lead"),
			Metadata:         map[string]string{"k": "v"},
			Permissions: orchestration.ProjectPermissions{
				FleetNodeRoles: map[string]orchestration.ProjectRole{
					"agent-1": orchestration.ProjectRoleLead,
				},
			},
		}
		p.Workspace.FleetNodeID = "agent-ws"
		p.Workspace.Path = "/work"
		p.Git.RepoURL = "https://example.com/repo.git"
		p.Git.DefaultBranch = "main"

		resp := projectToREST(p)
		require.NotNil(t, resp)
		assert.Equal(t, "proj-1", resp.ID)
		assert.Equal(t, "tenant-1", resp.TenantID)
		assert.Equal(t, "Demo", resp.Name)
		assert.Equal(t, "agent-ws", resp.WorkspaceFleetNodeID)
		assert.Equal(t, "/work", resp.WorkspacePath)
		assert.Equal(t, "https://example.com/repo.git", resp.GitRepoURL)
		require.NotNil(t, resp.FleetNodeRoles)
		assert.Equal(t, orchestration.ProjectRoleLead, resp.FleetNodeRoles["agent-1"])
	})
}

// TestValidateHookPayloads_MissingHookEventName covers the hook_event_name guard
// branch of validateTaskCompletedPayload and validateTeammateIdlePayload, which
// was previously only exercised for the TaskCreated variant.
func TestValidateHookPayloads_MissingHookEventName(t *testing.T) {
	t.Run("task completed without hook event name", func(t *testing.T) {
		err := validateTaskCompletedPayload(&HookPayload{TaskID: "t1", TeamName: "team"})
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.ErrorAs(t, err, &hookErr)
		assert.Equal(t, "hook_event_name", hookErr.Field)
	})

	t.Run("teammate idle without hook event name", func(t *testing.T) {
		err := validateTeammateIdlePayload(&HookPayload{TeammateName: "tm", TeamName: "team"})
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.ErrorAs(t, err, &hookErr)
		assert.Equal(t, "hook_event_name", hookErr.Field)
	})
}
