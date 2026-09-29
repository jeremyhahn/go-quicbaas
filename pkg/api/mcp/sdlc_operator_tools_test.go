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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mcpIssueAuditor accepts every filing the MCP rig's service audits.
type mcpIssueAuditor struct{}

func (mcpIssueAuditor) RecordIssueFiled(context.Context, sdlc.IssueFiledEvent) {}

func bindMCPProject(t *testing.T, registry *ToolRegistry, binding map[string]interface{}) {
	t.Helper()
	if _, err := registry.ExecuteTool(context.Background(), "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": binding,
	}); err != nil {
		t.Fatalf("sdlc_bind: %v", err)
	}
}

func TestSDLCOperatorTools_FileAnIssue(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	bindMCPProject(t, registry, profiledToolBinding(t, forge))
	forge.PutIssue(3, "parent")

	result, err := registry.ExecuteTool(context.Background(), "sdlc_issue_create", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "title": "Follow-up",
		"labels": []interface{}{"task:ready"}, "parent": float64(3),
	})
	if err != nil {
		t.Fatalf("sdlc_issue_create: %v", err)
	}
	issue := result.(*sdlc.ForgeIssue)
	if issue.Title != "Follow-up" || issue.Body != "Parent: #3" || len(issue.Labels) != 1 {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestSDLCOperatorTools_RefuseBadInput(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	bindMCPProject(t, registry, profiledToolBinding(t, forge))
	cases := map[string]map[string]interface{}{
		"no title":           {"tenant_id": "tenant-a", "project_id": "p1"},
		"labels not a list":  {"tenant_id": "tenant-a", "project_id": "p1", "title": "t", "labels": "task:ready"},
		"a label not a text": {"tenant_id": "tenant-a", "project_id": "p1", "title": "t", "labels": []interface{}{1.0}},
		"a missing parent":   {"tenant_id": "tenant-a", "project_id": "p1", "title": "t", "parent": float64(99)},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := registry.ExecuteTool(context.Background(), "sdlc_issue_create", args); !errors.Is(err, sdlc.ErrInvalidInput) {
				t.Fatalf("sdlc_issue_create = %v, want ErrInvalidInput", err)
			}
		})
	}
	if forge.IssueCount() != 0 {
		t.Fatalf("issues = %d, want none filed", forge.IssueCount())
	}
}

func TestSDLCOperatorTools_Collaborators(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	bindMCPProject(t, registry, profiledToolBinding(t, forge))
	forge.PutCollaborator("lead-agent")

	result, err := registry.ExecuteTool(context.Background(), "sdlc_collaborators", map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1"})
	if err != nil {
		t.Fatalf("sdlc_collaborators: %v", err)
	}
	listed := result.(map[string]interface{})
	if listed["count"] != 2 {
		t.Fatalf("collaborators = %+v, want the owner and lead-agent", listed)
	}
	if _, err := registry.ExecuteTool(context.Background(), "sdlc_collaborators", map[string]interface{}{"tenant_id": "tenant-a", "project_id": "nope"}); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("an unbound project = %v, want ErrNotFound", err)
	}
}

func TestSDLCOperatorTools_RulersSurviveARebind(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	forge.PutCollaborator("lead-agent")
	binding := profiledToolBinding(t, forge)
	binding["rulers"] = []interface{}{"lead-agent"}
	bindMCPProject(t, registry, binding)
	read, err := registry.ExecuteTool(context.Background(), "sdlc_binding", map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1"})
	if err != nil {
		t.Fatalf("sdlc_binding: %v", err)
	}
	if rulers := read.(*sdlc.ProjectBinding).Rulers; len(rulers) != 1 || rulers[0] != "lead-agent" {
		t.Fatalf("rulers = %v, want [lead-agent]", rulers)
	}
}

func TestSDLCOperatorTools_RegisteredGatedAndRefusedWithoutTheModule(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewToolRegistry(testutil.NewMockQuicBaaS(), log)
	registered := map[string]bool{}
	for _, tool := range registry.ListTools() {
		registered[tool.Name] = true
	}
	for tool, action := range map[string]string{"sdlc_issue_create": "issue_create", "sdlc_collaborators": "read"} {
		if !registered[tool] || !isSDLCTool(tool) {
			t.Fatalf("%s is not registered and dispatched", tool)
		}
		if perm, ok := qrdbToolExactMapping[tool]; !ok || perm != (qrdbToolResource{"sdlc", action}) {
			t.Fatalf("%s is gated %v, want sdlc:%s", tool, perm, action)
		}
		if _, err := registry.executeSDLCTool(context.Background(), tool, map[string]interface{}{"project_id": "p", "title": "t"}); err == nil {
			t.Fatalf("%s answered without the module", tool)
		}
	}
}
