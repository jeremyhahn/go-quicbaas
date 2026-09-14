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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

// projectTenantFixture is a barrier-isolated tenant, which is what every real
// tenant on the burn-in node is.
const projectTenantFixture = "tenant-a"

// seedProject writes a project the way the store reads it, on a context that
// names the tenant — the tenanted WRITE whose untenanted READ is the defect.
//
// It writes through the KV rather than through Create because Create needs a
// store that can decide name uniqueness (projectStore.guarded) and the routing
// harness is a plain KV. The record encoding is the store's own: a JSON body
// under projectRecordKey.
func seedProject(t *testing.T, kv *iamtest.TenantRoutingKV, project *Project) {
	t.Helper()
	key, err := projectRecordKey(project.TenantID, project.ID)
	if err != nil {
		t.Fatalf("projectRecordKey: %v", err)
	}
	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx := ContextWithTenant(context.Background(), project.TenantID)
	if err := kv.Put(ctx, key, body); err != nil {
		t.Fatalf("seed put: %v", err)
	}
}

// newRoutedProjectStore builds a project store over the tenant-ROUTING KV, the
// only kind that can reproduce this family of defect.
//
// A flat store cannot: NewInMemoryProjectStore is one namespace, so a write and
// a read that disagree about the tenant still find each other and the test
// passes while production fails. Every orchestration test used the flat store,
// which is a large part of why five prior instances of this bug all shipped.
func newRoutedProjectStore(t *testing.T) (ProjectStore, *iamtest.TenantRoutingKV) {
	t.Helper()
	kv := iamtest.NewTenantRoutingKV()
	// The harness reads iam's tenant key; orchestration carries its own, and
	// production's tenantFromAnyContext reads BOTH. Registering it here is what
	// makes this store route the way the daemon's does.
	kv.AlsoReadTenantFrom(TenantFromContext)
	kv.BarrierTenant(projectTenantFixture)
	return NewProjectStore(kv), kv
}

// TestProjectStore_GetOnAnUntenantedContextFindsTheProject is the regression
// guard for the resumed SDLC jobs that died on the burn-in node:
//
//	job <id> completed with error: sdlc: invalid project: the job carries no
//	project record; the worker's checkout is described from it
//
// recoverOrchestrationJobs builds its context from context.Background(), and
// launchJob reads the project through loadJobProject thirty-five lines BEFORE it
// stamps the tenant on anything. The routing KV resolved the shared store, the
// project was not there, loadJobProject swallowed the not-found, and the resumed
// job refused — and was charged an attempt for it.
//
// This is the sixth instance of the same defect (#663 agents, #644 oauth2
// applications, #535 pooled accounts, #634 projects at the SERVICE layer, #918
// tasks). The stamp belongs in the store, as taskTenantContext argues: a reader
// that forgets is invisible until it destroys something.
func TestProjectStore_GetOnAnUntenantedContextFindsTheProject(t *testing.T) {
	t.Parallel()
	store, kv := newRoutedProjectStore(t)
	seedProject(t, kv, &Project{ID: "proj-1", TenantID: projectTenantFixture, Name: "go-truststrap"})

	// The read job recovery makes: the tenant is an ARGUMENT, not on the context.
	got, err := store.Get(context.Background(), projectTenantFixture, "proj-1")
	if err != nil {
		t.Fatalf("Get on an untenanted context: %v; this is the read job recovery makes, and a resumed job dies without it", err)
	}
	if got.Name != "go-truststrap" {
		t.Errorf("Name = %q, want go-truststrap", got.Name)
	}
}

// TestProjectStore_ListByTenantOnAnUntenantedContextScansTheTenant: the same
// defect with a scan instead of a read, which is how #918 destroyed live work —
// an empty result is indistinguishable from "this tenant has nothing".
func TestProjectStore_ListByTenantOnAnUntenantedContextScansTheTenant(t *testing.T) {
	t.Parallel()
	store, kv := newRoutedProjectStore(t)
	seedProject(t, kv, &Project{ID: "proj-1", TenantID: projectTenantFixture, Name: "one"})
	seedProject(t, kv, &Project{ID: "proj-2", TenantID: projectTenantFixture, Name: "two"})

	listed, err := store.ListByTenant(context.Background(), projectTenantFixture)
	if err != nil {
		t.Fatalf("ListByTenant on an untenanted context: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("ListByTenant returned %d projects, want 2; an untenanted scan reads the wrong subtree and reports the tenant as empty", len(listed))
	}
	// Nothing was written to or read from the shared store: an isolated tenant's
	// projects in the shared namespace would be a cross-tenant leak, not merely
	// a missed read.
	if n := kv.SharedKeyCount(); n != 0 {
		t.Errorf("shared store holds %d keys; an isolated tenant's projects must never land there", n)
	}
}

// TestProjectStore_ASealedTenantStillFailsClosed: the stamp must not turn a
// sealed tenant into a silent shared-store read. Failing closed is the whole
// reason the routing store exists, and a helper that routed AROUND it would be
// worse than the bug it fixes.
func TestProjectStore_ASealedTenantStillFailsClosed(t *testing.T) {
	t.Parallel()
	store, kv := newRoutedProjectStore(t)
	seedProject(t, kv, &Project{ID: "proj-1", TenantID: projectTenantFixture, Name: "go-truststrap"})
	kv.SealTenant(projectTenantFixture)

	if _, err := store.Get(context.Background(), projectTenantFixture, "proj-1"); err == nil {
		t.Fatal("a sealed tenant's project was readable; the stamp must route TO the tenant, never around its barrier")
	}
}

// TestProjectTenantContext_NeverOverridesTheCallersTenant: the stamp fills a
// gap, it does not redirect. Which tenant a caller may act as is an admission
// decision made above this layer, so a context that already names one must reach
// the store unchanged — otherwise the helper that closes a read bug opens a
// write into a tenant the caller was never admitted to.
func TestProjectTenantContext_NeverOverridesTheCallersTenant(t *testing.T) {
	t.Parallel()
	ctx := ContextWithTenant(context.Background(), "tenant-caller")
	if got := TenantFromContext(projectTenantContext(ctx, "tenant-other")); got != "tenant-caller" {
		t.Errorf("tenant = %q, want tenant-caller preserved; this helper must never redirect an admitted caller", got)
	}
	bare := context.Background()
	if got := TenantFromContext(projectTenantContext(bare, "")); got != "" {
		t.Errorf("tenant = %q, want empty; there is nothing to stamp", got)
	}
	if got := TenantFromContext(projectTenantContext(bare, projectTenantFixture)); got != projectTenantFixture {
		t.Errorf("tenant = %q, want %q; this is the gap the helper exists to fill", got, projectTenantFixture)
	}
}
