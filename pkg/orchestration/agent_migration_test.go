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
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

func TestDefaultModelForBackend(t *testing.T) {
	if model, ok := DefaultModelForBackend("claude"); !ok || model != DefaultClaudeModel {
		t.Fatalf("claude = %q, %v", model, ok)
	}
	if model, ok := DefaultModelForBackend("codex"); !ok || model != "gpt-5.5" {
		t.Fatalf("codex = %q, %v", model, ok)
	}
	if _, ok := DefaultModelForBackend("openvino"); ok {
		t.Fatal("a backend with no curated catalog has no default")
	}
}

// TestAgentSeeder_OneAgentPerTuple is the migration's shape: the Claude coder,
// the Codex reviewer and the OpenVINO triage each become one Agent, however
// many bindings used them, and a tuple used for two roles gains both.
func TestAgentSeeder_OneAgentPerTuple(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAgentCatalogStore()
	seeder, err := NewAgentSeeder(ctx, store, "acme")
	if err != nil {
		t.Fatal(err)
	}
	coder := AgentTuple{Backend: "claude", Image: "worker:1"}
	reviewer := AgentTuple{Backend: "codex", Model: "gpt-5.5", Image: "worker:1", ReasoningEffort: "medium"}
	triage := AgentTuple{Backend: "openvino", Model: "qwen3-8b", Image: "worker:1"}

	ids := map[string]string{}
	for name, seed := range map[string]struct {
		tuple AgentTuple
		role  AgentRole
	}{"coder": {coder, AgentRoleCoding}, "reviewer": {reviewer, AgentRoleReview}, "triage": {triage, AgentRoleTriage}} {
		id, err := seeder.Seed(ctx, seed.tuple, seed.role)
		if err != nil {
			t.Fatalf("Seed(%s): %v", name, err)
		}
		ids[name] = id
	}
	if ids["coder"] != "claude-coder" || ids["reviewer"] != "codex-reviewer" || ids["triage"] != "openvino-triage" {
		t.Fatalf("ids = %v", ids)
	}
	// A second binding with the same tuple reuses the Agent.
	again, err := seeder.Seed(ctx, coder, AgentRoleCoding)
	if err != nil || again != "claude-coder" {
		t.Fatalf("reuse = %q, %v", again, err)
	}
	// The same tuple in another role gains that role rather than a twin.
	if id, err := seeder.Seed(ctx, coder, AgentRoleTriage); err != nil || id != "claude-coder" {
		t.Fatalf("second role = %q, %v", id, err)
	}
	agent, err := store.GetAgent(ctx, "acme", "claude-coder")
	if err != nil || !agent.HasRole(AgentRoleTriage) || agent.Model != DefaultClaudeModel || agent.Name != "Claude coder" {
		t.Fatalf("coder = %+v, %v", agent, err)
	}
	if reviewerAgent, _ := store.GetAgent(ctx, "acme", "codex-reviewer"); reviewerAgent.ReasoningEffort != "medium" {
		t.Fatalf("the reviewer's effort must carry over: %+v", reviewerAgent)
	}
	if len(seeder.Created) != 3 {
		t.Fatalf("created = %v", seeder.Created)
	}
	// A different Claude tuple gets its own, suffixed ID.
	if id, err := seeder.Seed(ctx, AgentTuple{Backend: "claude", Model: "claude-fable-5"}, AgentRoleCoding); err != nil || id != "claude-coder-2" {
		t.Fatalf("a second claude coder = %q, %v", id, err)
	}
}

func TestAgentSeeder_ReusesAnAgentAnOperatorAlreadyMade(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAgentCatalogStore()
	existing := testAgent("my-codex", "codex", "gpt-5.5", AgentRoleReview)
	if err := store.CreateAgent(ctx, "acme", &existing); err != nil {
		t.Fatal(err)
	}
	seeder, err := NewAgentSeeder(ctx, store, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := seeder.Seed(ctx, AgentTuple{Backend: "codex", Model: "gpt-5.5"}, AgentRoleReview); err != nil || id != "my-codex" {
		t.Fatalf("Seed = %q, %v", id, err)
	}
	if len(seeder.Created) != 0 {
		t.Fatal("nothing should have been created")
	}
}

func TestAgentSeeder_RefusesATupleThatCannotBecomeAnAgent(t *testing.T) {
	ctx := context.Background()
	seeder, err := NewAgentSeeder(ctx, NewInMemoryAgentCatalogStore(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seeder.Seed(ctx, AgentTuple{}, AgentRoleCoding); !errors.Is(err, ErrAgentSeed) {
		t.Fatalf("no backend = %v", err)
	}
	_, err = seeder.Seed(ctx, AgentTuple{Backend: "openvino"}, AgentRoleTriage)
	var seedErr *AgentSeedError
	if !errors.As(err, &seedErr) || !strings.Contains(seedErr.Error(), "no known default") {
		t.Fatalf("no model and no default = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewAgentSeeder(cancelled, NewInMemoryAgentCatalogStore(), "acme"); err == nil {
		t.Fatal("an unreadable catalog must be refused")
	}
}

func TestSlugifyAndDisplayBackend(t *testing.T) {
	for in, want := range map[string]string{"Claude Code": "claude-code", "ollama-agent": "ollama-agent", "  a__b  ": "a__b", "x!!y": "x-y"} {
		if got := slugify(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if displayBackend("openvino") != "OpenVINO" || displayBackend("mystery") != "Mystery" {
		t.Fatal("displayBackend")
	}
	seeder := &AgentSeeder{agents: map[string]Agent{}}
	long := strings.Repeat("a", 80)
	if id := seeder.uniqueID(long); len(id) > maxCatalogID || !validCatalogID(id) {
		t.Fatalf("a long base must be cut to a valid ID, got %q", id)
	}
}

// TestMigrateProjectLeads_DropsTheLegacyString: a free-string default lead
// named no Agent, so it is dropped and reported; a reference is left alone.
func TestMigrateProjectLeads_DropsTheLegacyString(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	legacy := `{"id":"p1","tenant_id":"acme","name":"one","default_lead_agent":"agent-7"}`
	current := `{"id":"p2","tenant_id":"acme","name":"two","default_lead_agent":{"agent_id":"claude-lead"}}`
	empty := `{"id":"p3","tenant_id":"acme","name":"three","default_lead_agent":""}`
	for id, body := range map[string]string{"p1": legacy, "p2": current, "p3": empty} {
		if err := kv.Put(ctx, projectKeyspace+"acme/"+id, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := MigrateProjectLeads(ctx, kv, "acme")
	if err != nil {
		t.Fatalf("MigrateProjectLeads: %v", err)
	}
	if !slices.Equal(dropped, []string{"p1=agent-7"}) {
		t.Fatalf("dropped = %v", dropped)
	}
	store := NewProjectStore(kv)
	for _, id := range []string{"p1", "p2", "p3"} {
		project, err := store.Get(ctx, "acme", id)
		if err != nil {
			t.Fatalf("project %s no longer decodes: %v", id, err)
		}
		if id == "p2" && project.DefaultLeadAgent.AgentID != "claude-lead" {
			t.Fatalf("a reference must be left alone: %+v", project.DefaultLeadAgent)
		}
	}
	raw, _ := kv.Get(ctx, projectKeyspace+"acme/p1")
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if _, still := record["default_lead_agent"]; still {
		t.Fatal("the legacy string must be gone")
	}
	// Idempotent: a second pass finds nothing.
	if again, err := MigrateProjectLeads(ctx, kv, "acme"); err != nil || len(again) != 0 {
		t.Fatalf("second pass = %v, %v", again, err)
	}
	if _, err := MigrateProjectLeads(ctx, kv, "a/b"); err == nil {
		t.Fatal("a malformed tenant must be refused")
	}
}
