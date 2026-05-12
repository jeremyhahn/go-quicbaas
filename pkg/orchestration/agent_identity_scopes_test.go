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

package orchestration

import "testing"

// TestAgentRuntimeScopes_AreValidFormat protects against drift between the
// scope constants and the scopeFormatRegex used in Validate().
func TestAgentRuntimeScopes_AreValidFormat(t *testing.T) {
	for _, s := range AgentRuntimeScopes() {
		if !IsValidScopeFormat(s) {
			t.Errorf("scope %q does not match scopeFormatRegex", s)
		}
	}
}

// TestAgentRuntimeScopes_HasScope_Exact verifies HasScope returns true for
// each of the three new runtime scopes when they appear verbatim in the
// identity's Scopes slice.
func TestAgentRuntimeScopes_HasScope_Exact(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "agent-runtime",
		Scopes:           AgentRuntimeScopes(),
		ClientID:         "c",
	}

	for _, want := range []string{
		AgentScopeRuntimeSpawn,
		AgentScopeRuntimePrompt,
		AgentScopeRuntimeStatus,
	} {
		if !ai.HasScope(want) {
			t.Errorf("HasScope(%q) = false, want true", want)
		}
	}
}

// TestAgentRuntimeScopes_Validate verifies an AgentIdentity with only the
// three runtime scopes validates cleanly — the existing Validate() must
// accept them without modification.
func TestAgentRuntimeScopes_Validate(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "agent-xyz",
		ServiceAccountID: "sa-xyz",
		TenantID:         "tenant-1",
		Role:             "agent-runtime",
		Scopes:           AgentRuntimeScopes(),
		ClientID:         "client-xyz",
	}
	if err := ai.Validate(); err != nil {
		t.Fatalf("expected Validate() to accept runtime scopes, got %v", err)
	}
}

// TestAgentRuntimeScopes_WildcardMatch verifies that a broader "agent:*"
// scope is still recognized as covering the three new runtime scopes, so
// existing wildcard-based tenant policies continue to work.
func TestAgentRuntimeScopes_WildcardMatch(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "w",
		Scopes:           []string{"agent:*"},
		ClientID:         "c",
	}
	for _, s := range AgentRuntimeScopes() {
		if !ai.HasScope(s) {
			t.Errorf("wildcard agent:* should cover %q", s)
		}
	}
}

// TestAgentRuntimeScopes_NarrowerGrant verifies that granting only one of
// the three runtime scopes does not implicitly grant the others.
func TestAgentRuntimeScopes_NarrowerGrant(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "w",
		Scopes:           []string{AgentScopeRuntimeStatus},
		ClientID:         "c",
	}
	if !ai.HasScope(AgentScopeRuntimeStatus) {
		t.Error("status scope should be present when granted")
	}
	if ai.HasScope(AgentScopeRuntimeSpawn) {
		t.Error("spawn scope must not leak from status grant")
	}
	if ai.HasScope(AgentScopeRuntimePrompt) {
		t.Error("prompt scope must not leak from status grant")
	}
}
