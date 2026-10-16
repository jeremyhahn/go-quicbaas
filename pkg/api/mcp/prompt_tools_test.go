//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// newPromptRegistry is a tool registry over a fresh prompt fixture when
// mounted is true and with the prompt service unmounted otherwise.
func newPromptRegistry(t *testing.T, mounted bool) (*ToolRegistry, *pt.Fixture) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mock := testutil.NewMockQuicBaaS()
	var fixture *pt.Fixture
	if mounted {
		fixture = pt.New(t)
		mock.SetPromptService(fixture.Service)
	}
	return NewToolRegistry(mock, log), fixture
}

// promptCtx is a caller of the fixture's tenant.
func promptCtx() context.Context {
	return tenantctx.WithTenant(pt.Context(), pt.Tenant)
}

// charterArgs addresses the fixture's charter, plus extra arguments.
func charterArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{"set": pt.Set, "name": pt.Charter}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestPromptTools_RegisteredAndDispatched(t *testing.T) {
	registry, _ := newPromptRegistry(t, false)
	names := map[string]bool{}
	for _, tool := range registry.ListTools() {
		names[tool.Name] = true
	}
	for name := range promptTools {
		require.True(t, names[name], "%s not registered", name)
		require.True(t, isPromptTool(name))
	}
	require.Len(t, promptTools, 8)
	require.False(t, isPromptTool("sdlc_binding"))

	_, err := registry.executePromptTool(promptCtx(), "prompt_nope", nil)
	require.Error(t, err)
}

func TestPromptTools_SuccessPaths(t *testing.T) {
	registry, _ := newPromptRegistry(t, true)
	ctx := promptCtx()
	run := func(name string, args map[string]interface{}) interface{} {
		t.Helper()
		got, err := registry.ExecuteTool(ctx, name, args)
		require.NoError(t, err, name)
		return got
	}

	list := run("prompt_list", map[string]interface{}{"set": pt.Set}).(*service.ListPromptsResponse)
	require.Len(t, list.Prompts, len(pt.Spec().Prompts))

	effective := run("prompt_effective", charterArgs(nil)).(*service.EffectivePrompt)
	require.Equal(t, pt.CharterDefault, effective.Content)

	body := pt.CharterDefault + "\ntenant addition"
	put := run("prompt_put", charterArgs(map[string]interface{}{
		"content": body, "note": pt.Note, "expected_version": float64(0),
	})).(*service.PromptView)
	require.Equal(t, int64(1), put.Version)

	got := run("prompt_get", charterArgs(nil)).(*service.PromptView)
	require.Equal(t, body, got.Content)
	first := run("prompt_get", charterArgs(map[string]interface{}{"version": float64(1)})).(*service.PromptView)
	require.Equal(t, int64(1), first.Version)

	diff := run("prompt_diff", charterArgs(nil)).(*service.PromptDiff)
	require.False(t, diff.Same)

	run("prompt_put", charterArgs(map[string]interface{}{
		"content": pt.CharterDefault + "\nsecond", "note": pt.Note, "expected_version": float64(1),
	}))
	rolled := run("prompt_rollback", charterArgs(map[string]interface{}{
		"to_version": float64(1), "note": pt.Note, "expected_version": float64(2),
	})).(*service.PromptView)
	require.Equal(t, body, rolled.Content)

	reset := run("prompt_reset", charterArgs(map[string]interface{}{
		"note": pt.Note, "expected_version": float64(3),
	})).(*service.PromptView)
	require.True(t, reset.FollowParent)

	history := run("prompt_history", charterArgs(nil)).(*service.PromptHistory)
	require.Len(t, history.Revisions, 4)
}

func TestPromptTools_StatusMapping(t *testing.T) {
	registry, _ := newPromptRegistry(t, true)
	ctx := promptCtx()
	cases := []struct {
		name string
		tool string
		args map[string]interface{}
		code int
	}{
		{"nothing overridden", "prompt_get", charterArgs(nil), mcptransport.MCPResourceNotFound},
		{"stale version", "prompt_put", charterArgs(map[string]interface{}{
			"content": pt.CharterDefault, "note": pt.Note, "expected_version": float64(4),
		}), mcptransport.MCPResourceConflict},
		{"missing marker", "prompt_put", charterArgs(map[string]interface{}{
			"content": "no marker here", "note": pt.Note,
		}), mcptransport.MCPInvalidInput},
		{"unknown set", "prompt_list", map[string]interface{}{"set": "nope"}, mcptransport.MCPInvalidInput},
		{"unknown name", "prompt_effective", map[string]interface{}{"set": pt.Set, "name": "nope"}, mcptransport.MCPInvalidInput},
		{"system write", "prompt_put", charterArgs(map[string]interface{}{
			"scope": "system", "content": pt.CharterDefault, "note": pt.Note,
		}), mcptransport.MCPInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.ExecuteTool(ctx, tc.tool, tc.args)
			require.Error(t, err)
			require.Equal(t, tc.code, toolErrorCode(err), err.Error())
		})
	}
}

func TestPromptTools_MissingMarkerNamesTheMarker(t *testing.T) {
	registry, _ := newPromptRegistry(t, true)
	_, err := registry.ExecuteTool(promptCtx(), "prompt_put", charterArgs(map[string]interface{}{
		"content": "no marker here", "note": pt.Note,
	}))
	require.Error(t, err)
	log, logErr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, logErr)
	require.Contains(t, toolErrorMessage(log, "prompt_put", err), pt.Marker)
}

func TestPromptTools_BadIntegerArgumentIsRefused(t *testing.T) {
	registry, _ := newPromptRegistry(t, true)
	ctx := promptCtx()
	for _, tool := range []string{"prompt_get", "prompt_put", "prompt_rollback", "prompt_reset"} {
		args := charterArgs(map[string]interface{}{
			"version": "x", "expected_version": "x", "to_version": "x", "content": pt.CharterDefault, "note": pt.Note,
		})
		_, err := registry.ExecuteTool(ctx, tool, args)
		require.Error(t, err, tool)
	}
}

func TestPromptTools_UnreachableBackendIsUnavailable(t *testing.T) {
	registry, fixture := newPromptRegistry(t, true)
	fixture.Resolver.Fail()
	_, err := registry.ExecuteTool(promptCtx(), "prompt_effective", charterArgs(nil))
	require.Error(t, err)
	require.Equal(t, mcptransport.MCPServiceUnavailable, toolErrorCode(err))
}

func TestPromptTools_UnmountedIsUnavailable(t *testing.T) {
	registry, _ := newPromptRegistry(t, false)
	for name := range promptTools {
		_, err := registry.ExecuteTool(promptCtx(), name, charterArgs(nil))
		require.ErrorIs(t, err, service.ErrPromptsUnavailable, name)
		require.Equal(t, mcptransport.MCPServiceUnavailable, toolErrorCode(err), name)
	}
}

func TestPromptTools_TenantMismatchIsRefused(t *testing.T) {
	registry, _ := newPromptRegistry(t, true)
	for name := range promptTools {
		_, err := registry.ExecuteTool(promptCtx(), name, charterArgs(map[string]interface{}{"tenant_id": "tenant-b"}))
		require.Error(t, err, name)
		_, err = registry.ExecuteTool(pt.Context(), name, charterArgs(nil))
		require.Error(t, err, name+" without a tenant")
	}
	_, err := registry.ExecuteTool(promptCtx(), "prompt_effective", charterArgs(map[string]interface{}{"tenant_id": pt.Tenant}))
	require.NoError(t, err)
}

// TestPromptToolsHaveQRDBPermissionMapping pins each tool to the permission
// the REST route and the gRPC method of the same verb require. A principal
// without it is refused, and an unmapped tool is denied and hidden.
func TestPromptToolsHaveQRDBPermissionMapping(t *testing.T) {
	want := map[string][2]string{
		"prompt_list":      {"prompts", "read"},
		"prompt_get":       {"prompts", "read"},
		"prompt_effective": {"prompts", "read"},
		"prompt_diff":      {"prompts", "read"},
		"prompt_history":   {"prompts", "history"},
		"prompt_put":       {"prompts", "write"},
		"prompt_rollback":  {"prompts", "write"},
		"prompt_reset":     {"prompts", "write"},
	}
	for name := range promptTools {
		expected, declared := want[name]
		require.True(t, declared, "%s has no expected permission", name)
		resource, action, ok := getToolQRDBPermission(name)
		require.True(t, ok, "%s is unmapped", name)
		require.Equal(t, expected, [2]string{resource, action}, name)
	}
	_, _, ok := getToolQRDBPermission("prompt_nope")
	require.False(t, ok)
}
