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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func newIssueSourceRig(t *testing.T) (*IssueSource, *fakeForge, *Store) {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	source, err := NewIssueSource(store, &fakeForges{forge: forge}, 10)
	if err != nil {
		t.Fatal(err)
	}
	return source, forge, store
}

func TestIssueSource_PollAdoptsTriggeredIssuesExactly(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready", "bug")
	forge.putIssue(26, "Wrong case", "Task:Ready")
	forge.putIssue(27, "No label")
	closed := forge.putIssue(28, "Closed", "task:ready")
	closed.State = "closed"
	forge.comments[25] = []ForgeComment{{Body: "please also do X", Author: "alice"}}

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %+v, want exactly the exact-case triggered open issue", specs)
	}
	spec := specs[0]
	if spec.Source.Kind != TaskSourceKindForgeIssue || spec.Source.Ref != "acme/api#25" || !strings.HasSuffix(spec.Source.URL, "/issues/25") {
		t.Fatalf("source = %+v", spec.Source)
	}
	if spec.Subject != "Add the thing" || !strings.Contains(spec.Description, "body of 25") || !strings.Contains(spec.Description, "please also do X") {
		t.Fatalf("subject/description = %q / %q", spec.Subject, spec.Description)
	}
	if spec.Metadata[MetadataIssueNumber] != "25" || spec.Metadata[MetadataRepository] != "acme/api" || spec.Metadata[MetadataForge] != string(ForgeGitea) {
		t.Fatalf("metadata = %+v", spec.Metadata)
	}
	if spec.Priority != 0 || spec.AttemptBudget != 0 {
		t.Fatalf("priority/budget = %d/%d, want the engine defaults", spec.Priority, spec.AttemptBudget)
	}
	if err := spec.Validate(TaskSourceKindForgeIssue); err != nil {
		t.Fatalf("the engine would refuse the spec: %v", err)
	}
	if source.Name() != TaskSourceName {
		t.Fatal("Name")
	}
}

func TestIssueSource_PriorityFollowsTheBinding(t *testing.T) {
	source, forge, store := newIssueSourceRig(t)
	binding := testBinding()
	binding.Ranking = RankingPolicy{DefaultPriority: 7, AnalyzerName: DeclaredAnalyzerName, DeclaredDependents: []string{"web"}}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	forge.putIssue(1, "x", "task:ready")
	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil || len(specs) != 1 || specs[0].Priority != 17 {
		t.Fatalf("specs = %+v, %v (want priority 17)", specs, err)
	}
}

func TestIssueSource_UnboundProjectHasNoWork(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(1, "x", "task:ready")
	other := testOrchProject()
	other.ID = "unbound"
	specs, err := source.Poll(context.Background(), other)
	if err != nil || specs != nil {
		t.Fatalf("unbound project: %+v, %v", specs, err)
	}
	if _, err := source.Poll(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil project: %v", err)
	}
}

func TestIssueSource_ForgeOutageIsReportedAsUnavailable(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.listIssuesErr = backendError("ListIssues", errors.New("502"))
	_, err := source.Poll(context.Background(), testOrchProject())
	var unavailable *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Source != TaskSourceName {
		t.Fatalf("outage not classified: %v", err)
	}
	forge.listIssuesErr = nil
	forge.putIssue(1, "x", "task:ready")
	forge.listCommentsErr = backendError("ListIssueComments", errors.New("502"))
	if _, err := source.Poll(context.Background(), testOrchProject()); !errors.As(err, &unavailable) {
		t.Fatalf("comment outage not classified: %v", err)
	}
	forge.listCommentsErr = &InvalidInputError{Field: "x", Message: "y"}
	if _, err := source.Poll(context.Background(), testOrchProject()); errors.As(err, &unavailable) {
		t.Fatalf("a non-backend error must not be reported as an outage: %v", err)
	}
}

func TestIssueSource_ForgeResolutionFailures(t *testing.T) {
	store := newTestStore(t)
	if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	failing, err := NewIssueSource(store, &fakeForges{err: errors.New("no token")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Poll(context.Background(), testOrchProject()); err == nil {
		t.Fatal("forge resolution failure swallowed")
	}
	nilForge, _ := NewIssueSource(store, &fakeForges{}, 0)
	if _, err := nilForge.Poll(context.Background(), testOrchProject()); !errors.Is(err, ErrForgeNotRegistered) {
		t.Fatalf("nil forge: %v", err)
	}
	if nilForge.pageLimit != DefaultIssuePageLimit {
		t.Fatal("page limit default")
	}
	if _, err := NewIssueSource(nil, &fakeForges{}, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil bindings")
	}
	if _, err := NewIssueSource(store, nil, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil forges")
	}
	resolver := newMemoryResolver()
	resolver.err = errors.New("sealed")
	sealed, _ := NewStore(resolver)
	src, _ := NewIssueSource(sealed, &fakeForges{forge: newFakeForge()}, 0)
	if _, err := src.Poll(context.Background(), testOrchProject()); !errors.Is(err, ErrBackend) {
		t.Fatalf("sealed store: %v", err)
	}
}

// TestIssueSource_SecondAdoptionIsRefusedByTheIndex proves dedupe is the
// engine's: the same spec on two sweeps creates one Task, the second refused
// by the task store's guarded source index.
func TestIssueSource_SecondAdoptionIsRefusedByTheIndex(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	registry := orchestration.NewTaskSourceRegistry()
	if err := registry.Register(TaskSourceKindForgeIssue, source); err != nil {
		t.Fatal(err)
	}
	tasks := orchestration.NewTaskStore(orchestration.NewMemoryTaskKV(), orchestration.NewProcessClaimArbiter())
	ctx := orchestration.ContextWithTenant(context.Background(), testTenant)
	adopt := func(id string) error {
		specs, err := source.Poll(ctx, testOrchProject())
		if err != nil || len(specs) != 1 {
			t.Fatalf("poll: %+v %v", specs, err)
		}
		src := specs[0].Source
		return tasks.CreateTask(ctx, &orchestration.Task{
			ID: id, TenantID: testTenant, ProjectID: testProject, Subject: specs[0].Subject, Description: specs[0].Description,
			Status: orchestration.TaskStatusPending, Source: &src,
		})
	}
	if err := adopt("task-first"); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	if err := adopt("task-second"); !errors.Is(err, orchestration.ErrTaskSourceAlreadyAdopted) {
		t.Fatalf("second adoption: %v, want ErrTaskSourceAlreadyAdopted", err)
	}
	listed, err := tasks.ListTasks(ctx, testTenant, &orchestration.TaskFilter{ProjectID: testProject})
	if err != nil || len(listed) != 1 {
		t.Fatalf("tasks = %d, %v", len(listed), err)
	}
}
