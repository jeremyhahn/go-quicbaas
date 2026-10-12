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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// countingResolver counts resolutions and records what each one asked for,
// delegating to a real resolver (or refusing with err).
type countingResolver struct {
	next  *prompts.Resolver
	err   error
	calls atomic.Int32
	asked atomic.Value
}

func (r *countingResolver) Resolve(ctx context.Context, tenantID, projectID string, kind prompts.SetKind) (*prompts.ResolvedSet, error) {
	r.calls.Add(1)
	r.asked.Store(tenantID + "/" + projectID + "/" + string(kind))
	if r.err != nil {
		return nil, r.err
	}
	return r.next.Resolve(ctx, tenantID, projectID, kind)
}

// tenantEditedResolver is a resolver over a store in which the test tenant
// has edited the sdlc prompt name to open with marker.
func tenantEditedResolver(t *testing.T, name, marker string) *prompts.Resolver {
	t.Helper()
	store, err := prompts.NewStore(newMemoryResolver())
	if err != nil {
		t.Fatal(err)
	}
	spec, known := SDLCPrompts().Spec().Prompt(name)
	if !known {
		t.Fatalf("%s is not in the sdlc set", name)
	}
	ref := prompts.Ref{TenantID: testTenant, Set: SetSDLC, Name: spec.Name}
	if _, err := store.Save(context.Background(), ref, prompts.Change{
		Stamp:   prompts.Stamp{Author: "alice"},
		Content: marker + spec.Default,
	}); err != nil {
		t.Fatal(err)
	}
	return prompts.NewResolver(store)
}

func TestIssueSource_RendersContractsFromTheProjectsResolvedSet(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.putIssue(26, "And the other", "task:ready")
	forge.comments[25] = []ForgeComment{{Body: "please also do X", Author: "alice"}}
	forge.comments[26] = []ForgeComment{{Body: "and Y", Author: "bob"}}
	resolver := &countingResolver{next: tenantEditedResolver(t, "issue_contract_comment", "TENANT-EDITED ")}
	source.SetPromptResolver(resolver)

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	for _, spec := range specs {
		if !strings.Contains(spec.Description, "TENANT-EDITED ") {
			t.Fatalf("contract for %s ignores the tenant's edit:\n%s", spec.Source.Ref, spec.Description)
		}
	}
	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("resolved %d times for one poll, want once per project", got)
	}
	if got := resolver.asked.Load(); got != testTenant+"/"+testProject+"/"+string(SetSDLC) {
		t.Fatalf("resolved %v, want the project's sdlc set", got)
	}
}

func TestIssueSource_RefusesAPollWhosePromptsCannotResolve(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	refused := &prompts.ResolveError{TenantID: testTenant, Set: SetSDLC, Cause: errors.New("store down")}
	source.SetPromptResolver(&countingResolver{err: refused})

	specs, err := source.Poll(context.Background(), testOrchProject())
	if !errors.Is(err, prompts.ErrResolve) || specs != nil {
		t.Fatalf("Poll = %d specs, %v; want the resolve error and no work", len(specs), err)
	}
}

func TestIssueSource_SetPromptResolverIgnoresNil(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.comments[25] = []ForgeComment{{Body: "please also do X", Author: "alice"}}
	source.SetPromptResolver(&countingResolver{next: tenantEditedResolver(t, "issue_contract_comment", "TENANT-EDITED ")})
	source.SetPromptResolver(nil)

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil || len(specs) != 1 || !strings.Contains(specs[0].Description, "TENANT-EDITED ") {
		t.Fatalf("a nil resolver replaced the wired one: %d specs, %v", len(specs), err)
	}
}

func TestJobPrompts_PrefersTheLaunchedSet(t *testing.T) {
	edited, err := tenantEditedResolver(t, "issue_contract_comment", "TENANT-EDITED ").Resolve(context.Background(), testTenant, testProject, SetSDLC)
	if err != nil {
		t.Fatal(err)
	}
	vars := prompts.Vars{"index": "1", "attribution": "alice", "body": "x"}
	run := &orchestration.JobRun{Prompts: edited}
	if got := jobPrompts(run).MustRender("issue_contract_comment", vars); !strings.HasPrefix(got, "TENANT-EDITED ") {
		t.Fatalf("jobPrompts ignored the launched set: %q", got)
	}
	jobCtx := &orchestration.JobContext{Prompts: edited}
	if got := contextPrompts(jobCtx).MustRender("issue_contract_comment", vars); !strings.HasPrefix(got, "TENANT-EDITED ") {
		t.Fatalf("contextPrompts ignored the launched set: %q", got)
	}
}

func TestJobPrompts_WithoutALaunchedSetRendersTheBuild(t *testing.T) {
	want := sdlcPrompts.MustRender("worker_charter", nil)
	for name, set := range map[string]prompts.Renderer{
		"nil run":         jobPrompts(nil),
		"run without set": jobPrompts(&orchestration.JobRun{}),
		"nil context":     contextPrompts(nil),
		"bare context":    contextPrompts(&orchestration.JobContext{}),
	} {
		if got := set.MustRender("worker_charter", nil); got != want {
			t.Fatalf("%s: rendered %q, want the build's default", name, got)
		}
	}
}
