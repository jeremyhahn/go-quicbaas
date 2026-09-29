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
)

// withRoleCharter installs a charter for role for the length of the test.
func withRoleCharter(t *testing.T, role AgentRole, charter RoleCharterFunc) {
	t.Helper()
	previous, had := roleCharters[role]
	RegisterRoleCharter(role, charter)
	t.Cleanup(func() {
		if had {
			RegisterRoleCharter(role, previous)
			return
		}
		RegisterRoleCharter(role, nil)
	})
}

func TestRoleCharter_ReturnsTheRegisteredCharter(t *testing.T) {
	withRoleCharter(t, AgentRoleGeneral, func() string { return "general charter" })
	got, ok := RoleCharter(AgentRoleGeneral)
	if !ok || got != "general charter" {
		t.Fatalf("RoleCharter = %q, %v", got, ok)
	}
}

func TestRoleCharter_UnknownOrRemovedRoleHasNone(t *testing.T) {
	if got, ok := RoleCharter(AgentRole("nonexistent")); ok || got != "" {
		t.Fatalf("RoleCharter(nonexistent) = %q, %v", got, ok)
	}
	withRoleCharter(t, AgentRoleGeneral, func() string { return "x" })
	RegisterRoleCharter(AgentRoleGeneral, nil)
	if _, ok := RoleCharter(AgentRoleGeneral); ok {
		t.Fatal("a nil registration must remove the charter")
	}
}

func TestSpawnSession_AgentWithoutPromptStartsWithTheRoleCharter(t *testing.T) {
	withRoleCharter(t, AgentRoleGeneral, func() string { return "general charter" })
	f := newSpawnFixture(t, nil, nil)
	if _, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "second", Role: AgentRoleGeneral}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	got := f.secondary.requests()
	if len(got) != 1 || got[0].SystemPrompt != "general charter" {
		t.Fatalf("the backend saw %+v, want the role's charter", got)
	}
}

func TestSpawnSession_AgentPromptWinsOverTheRoleCharter(t *testing.T) {
	withRoleCharter(t, AgentRoleGeneral, func() string { return "general charter" })
	f := newSpawnFixture(t, nil, nil)
	if _, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "first", Role: AgentRoleGeneral}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if got := f.primary.requests(); len(got) != 1 || got[0].SystemPrompt != "you are first" {
		t.Fatalf("the backend saw %+v, want the Agent's own prompt", got)
	}
}

func TestSpawnSession_AgentWithoutPromptOrCharterHasNoSystemPrompt(t *testing.T) {
	withRoleCharter(t, AgentRoleGeneral, nil)
	f := newSpawnFixture(t, nil, nil)
	if _, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "second", Role: AgentRoleGeneral}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if got := f.secondary.requests(); len(got) != 1 || got[0].SystemPrompt != "" {
		t.Fatalf("the backend saw %+v, want no system prompt", got)
	}
}
