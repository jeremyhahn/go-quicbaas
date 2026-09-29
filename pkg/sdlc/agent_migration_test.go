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

package sdlc

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// legacySeat is a review seat as a pre-Agents binding stored it: the forge
// identity plus the tuple.
type legacySeat struct {
	ReviewerConfig
	legacyTuple
}

// legacyRecord is a whole pre-Agents binding record. Its tuple fields shadow
// the embedded binding's references, so it encodes exactly what an old node
// wrote.
type legacyRecord struct {
	ProjectBinding
	Worker    legacyTuple  `json:"worker"`
	Triage    legacyTuple  `json:"triage"`
	Reviewer  legacySeat   `json:"reviewer"`
	Reviewers []legacySeat `json:"reviewers"`
}

// writeLegacyBinding stores record under its project exactly as an old node
// would have.
func writeLegacyBinding(t *testing.T, store *Store, record *legacyRecord) {
	t.Helper()
	ctx := context.Background()
	dao, err := entityDAO(ctx, store, "test", testTenant, bindingEntityType, func() *legacyRecord { return &legacyRecord{} })
	if err != nil {
		t.Fatal(err)
	}
	record.SetEntityID(entityIDFor(record.ProjectID))
	if err := dao.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
}

// storedLegacyBinding is a binding written before Agents: a Claude worker, an
// OpenVINO triage, and two Codex seats sharing one tuple.
func storedLegacyBinding(t *testing.T, store *Store, projectID string) {
	t.Helper()
	binding := testBinding()
	binding.ProjectID = projectID
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	stored, err := store.GetBinding(context.Background(), testTenant, projectID)
	if err != nil {
		t.Fatal(err)
	}
	codex := legacyTuple{Backend: "codex", Model: "gpt-5.5", Image: "worker:1", ReasoningEffort: "high"}
	writeLegacyBinding(t, store, &legacyRecord{
		ProjectBinding: *stored,
		Worker:         legacyTuple{Backend: "claude", Image: "worker:1"},
		Triage:         legacyTuple{Backend: "openvino", Model: "qwen3-8b"},
		Reviewer:       legacySeat{ReviewerConfig{Login: "codex-bot", TokenSecretRef: "tok"}, codex},
		Reviewers:      []legacySeat{{ReviewerConfig{Name: "second", Login: "codex-2", TokenSecretRef: "tok2"}, codex}},
	})
}

func TestMigrateBindingAgents_SeedsAgentsAndRewritesReferences(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	storedLegacyBinding(t, store, "proj-a")
	storedLegacyBinding(t, store, "proj-b")
	catalog := orchestration.NewInMemoryAgentCatalogStore()
	seeder, err := orchestration.NewAgentSeeder(ctx, catalog, testTenant)
	if err != nil {
		t.Fatal(err)
	}

	report, err := MigrateBindingAgents(ctx, store, seeder, testTenant)
	if err != nil {
		t.Fatalf("MigrateBindingAgents: %v", err)
	}
	if !slices.Equal(report.Migrated, []string{"proj-a", "proj-b"}) || len(report.Failed) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if !slices.Equal(seeder.Created, []string{"claude-coder", "openvino-triage", "codex-reviewer"}) {
		t.Fatalf("one Agent per distinct tuple, got %v", seeder.Created)
	}
	binding, err := store.GetBinding(ctx, testTenant, "proj-b")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Worker.AgentID != "claude-coder" || binding.Triage.AgentID != "openvino-triage" {
		t.Fatalf("slots = %+v / %+v", binding.Worker, binding.Triage)
	}
	if binding.Reviewer.Agent != "codex-reviewer" || binding.Reviewer.Login != "codex-bot" {
		t.Fatalf("the default seat keeps its forge identity: %+v", binding.Reviewer)
	}
	if len(binding.Reviewers) != 1 || binding.Reviewers[0].Agent != "codex-reviewer" || binding.Reviewers[0].Login != "codex-2" {
		t.Fatalf("seats = %+v", binding.Reviewers)
	}
	reviewer, err := catalog.GetAgent(ctx, testTenant, "codex-reviewer")
	if err != nil || reviewer.ReasoningEffort != "high" || !reviewer.HasRole(orchestration.AgentRoleReview) {
		t.Fatalf("reviewer agent = %+v, %v", reviewer, err)
	}

	// A migrated binding stores references, so a second boot finds nothing.
	again, err := MigrateBindingAgents(ctx, store, seeder, testTenant)
	if err != nil || len(again.Migrated) != 0 || len(again.Failed) != 0 {
		t.Fatalf("second pass = %+v, %v", again, err)
	}
}

func TestMigrateBindingAgents_LeavesAnUnmigratableBindingForTheOperator(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	binding := testBinding()
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	// A worker on a backend with no default model cannot become an Agent.
	writeLegacyBinding(t, store, &legacyRecord{ProjectBinding: *stored, Worker: legacyTuple{Backend: "openvino"}})
	seeder, err := orchestration.NewAgentSeeder(ctx, orchestration.NewInMemoryAgentCatalogStore(), testTenant)
	if err != nil {
		t.Fatal(err)
	}
	report, err := MigrateBindingAgents(ctx, store, seeder, testTenant)
	if err != nil {
		t.Fatalf("MigrateBindingAgents: %v", err)
	}
	if len(report.Migrated) != 0 || !errors.Is(report.Failed[testProject], orchestration.ErrAgentSeed) {
		t.Fatalf("report = %+v", report)
	}
}

func TestMigrateBindingAgents_RefusesAnUnreachableStore(t *testing.T) {
	resolver := newMemoryResolver()
	resolver.err = errors.New("tenant store unreachable")
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	seeder, err := orchestration.NewAgentSeeder(context.Background(), orchestration.NewInMemoryAgentCatalogStore(), testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateBindingAgents(context.Background(), store, seeder, testTenant); err == nil {
		t.Fatal("an unreachable store must be reported")
	}
}
