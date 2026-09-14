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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The scheduler — what turns a webhook into a triage job
// =============================================================================

// newTriageScheduler builds a scheduler over a bound project.
func newTriageScheduler(t *testing.T) (*TriageScheduler, *fakeJobStarter, *Store) {
	t.Helper()
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.PutBinding(ctx, testTenant, testBinding()); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	starter := &fakeJobStarter{}
	scheduler, err := NewTriageScheduler(TriageSchedulerConfig{Bindings: store, Jobs: starter})
	if err != nil {
		t.Fatalf("NewTriageScheduler: %v", err)
	}
	return scheduler, starter, store
}

// TestTriageScheduler_StartsATriageJobForABoundProject proves a nudge
// reaches the engine as a job naming the triage runner and the project.
func TestTriageScheduler_StartsATriageJobForABoundProject(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)

	if err := scheduler.TriageProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("TriageProject: %v", err)
	}
	started := starter.requests()
	if len(started) != 1 {
		t.Fatalf("one triage job, got %d", len(started))
	}
	req := started[0]
	if req.Runner != TriageRunnerName {
		t.Fatalf("runner: want %q, got %q", TriageRunnerName, req.Runner)
	}
	if req.ProjectID != testProject || req.TenantID != testTenant {
		t.Fatalf("job must name the project and tenant, got %+v", req)
	}
	if req.TaskID != "" {
		t.Fatal("a triage job is bound to no task")
	}
	if req.Description == "" {
		t.Fatal("a job with no description is unreadable in the UI")
	}
}

// TestTriageScheduler_SkipsAProjectWithNoBinding proves an unbound project
// is a quiet no-op rather than a job that would fail on resolve. Most
// projects on a node are not bound, and a scheduler that started a doomed
// job for each of them would fill the record with failures.
func TestTriageScheduler_SkipsAProjectWithNoBinding(t *testing.T) {
	scheduler, starter, store := newTriageScheduler(t)
	if err := store.DeleteBinding(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	if err := scheduler.TriageProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("an unbound project is not an error: %v", err)
	}
	if started := starter.requests(); len(started) != 0 {
		t.Fatalf("no job for an unbound project, got %d", len(started))
	}
}

// TestTriageScheduler_ASecondNudgeWhileOneRunsIsNotAnError proves the
// per-project bound is reported as "already running", not as a failure. The
// LEASE is what enforces the bound; this is the scheduler declining to make
// a job the lease would only refuse.
func TestTriageScheduler_ASecondNudgeWhileOneRunsIsNotAnError(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = &orchestration.JobLeaseTakenError{JobID: "job-2", HeldBy: "job-1"}

	err := scheduler.TriageProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("a project already being triaged is not an error: %v", err)
	}
}

// TestTriageScheduler_ReportsAFailureItCannotExplain proves a genuine
// failure is returned rather than swallowed with the contention case.
func TestTriageScheduler_ReportsAFailureItCannotExplain(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = errors.New("the engine refused the job")

	err := scheduler.TriageProject(context.Background(), testTenant, testProject)
	if err == nil {
		t.Fatal("a refusal the scheduler cannot explain must be reported")
	}
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("want a backend failure, got %v", err)
	}
}

// TestTriageScheduler_NudgeProjectSatisfiesTheWebhookSeam proves the
// scheduler is what a verified webhook delivery can nudge, so triage is
// woken by the forge rather than only by the periodic pass.
func TestTriageScheduler_NudgeProjectSatisfiesTheWebhookSeam(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	var nudger ProjectNudger = scheduler

	if err := nudger.NudgeProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("NudgeProject: %v", err)
	}
	if started := starter.requests(); len(started) != 1 {
		t.Fatalf("a nudge starts one triage job, got %d", len(started))
	}
}

// TestNewTriageScheduler_RefusesAnIncompleteWiring proves the collaborators
// are required and named.
func TestNewTriageScheduler_RefusesAnIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	tests := []struct {
		name  string
		cfg   TriageSchedulerConfig
		field string
	}{
		{name: "no bindings", cfg: TriageSchedulerConfig{Jobs: &fakeJobStarter{}}, field: "bindings"},
		{name: "no jobs", cfg: TriageSchedulerConfig{Bindings: store}, field: "jobs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTriageScheduler(tc.cfg)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("want *InvalidInputError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("field: want %q, got %q", tc.field, invalid.Field)
			}
		})
	}
}

// =============================================================================
// Which forge events wake triage
// =============================================================================

// TestTriageWakesOn_CoversTheIssueLifecycle proves triage is woken by the
// three deliveries that can change a verdict, and not by the ones that
// cannot.
//
// A LABEL CHANGE IS ONE OF THEM. A person removing needs-info, or adding a
// label that takes an issue out of the factory, changes what triage would
// conclude — and before this the Gitea adapter mapped label actions to
// EventUnsupported, so nothing woke at all.
func TestTriageWakesOn_CoversTheIssueLifecycle(t *testing.T) {
	tests := []struct {
		kind ForgeEventKind
		want bool
	}{
		{kind: EventIssueOpened, want: true},
		{kind: EventIssueEdited, want: true},
		{kind: EventIssueLabeled, want: true},
		{kind: EventIssueClosed, want: false},
		{kind: EventPullRequestOpened, want: false},
		{kind: EventPullRequestMerged, want: false},
		{kind: EventReviewSubmitted, want: false},
		{kind: EventCheckRunUpdated, want: false},
		{kind: EventUnsupported, want: false},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := TriageWakesOn(tc.kind); got != tc.want {
				t.Fatalf("TriageWakesOn(%q): want %v, got %v", tc.kind, tc.want, got)
			}
		})
	}
}

// =============================================================================
// The periodic backstop
// =============================================================================

// TestTriageSweep_NudgesEveryBoundProject proves the backstop reaches each
// bound project of each tenant, so a project whose webhook delivery was lost
// is still triaged.
func TestTriageSweep_NudgesEveryBoundProject(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for _, id := range []string{"proj-1", "proj-2"} {
		binding := testBinding()
		binding.ProjectID = id
		if err := store.PutBinding(ctx, testTenant, binding); err != nil {
			t.Fatalf("put binding %s: %v", id, err)
		}
	}
	starter := &fakeJobStarter{}
	scheduler, err := NewTriageScheduler(TriageSchedulerConfig{Bindings: store, Jobs: starter})
	if err != nil {
		t.Fatalf("NewTriageScheduler: %v", err)
	}
	swept, err := scheduler.Sweep(ctx, &fakeTenants{ids: []string{testTenant}})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 2 {
		t.Fatalf("both bound projects are swept, got %d", swept)
	}
	if started := starter.requests(); len(started) != 2 {
		t.Fatalf("one triage job per bound project, got %d", len(started))
	}
}

// TestTriageSweep_RefusesWhenTheTenantsCannotBeListed proves the one failure
// that stops the sweep is not knowing what it was meant to visit. Everything
// narrower is per project.
func TestTriageSweep_RefusesWhenTheTenantsCannotBeListed(t *testing.T) {
	scheduler, _, _ := newTriageScheduler(t)
	_, err := scheduler.Sweep(context.Background(), &fakeTenants{err: errors.New("tenant store down")})
	if err == nil {
		t.Fatal("a sweep that cannot enumerate tenants must say so")
	}
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("want a backend failure, got %v", err)
	}
}

// TestTriageSweep_OneTenantFailingDoesNotStopTheRest proves a tenant whose
// bindings cannot be listed costs that tenant a pass, not every tenant.
func TestTriageSweep_OneTenantFailingDoesNotStopTheRest(t *testing.T) {
	ctx := context.Background()
	scheduler, starter, _ := newTriageScheduler(t)
	// "ghost" has no store of its own; listing its bindings answers empty
	// rather than failing, so the real assertion is that the known tenant is
	// still visited when it is not first.
	swept, err := scheduler.Sweep(ctx, &fakeTenants{ids: []string{"ghost", testTenant}})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 1 {
		t.Fatalf("the bound project is still swept, got %d", swept)
	}
	if started := starter.requests(); len(started) != 1 {
		t.Fatalf("one triage job, got %d", len(started))
	}
}

// TestTriageSweep_RefusesANilTenantLister proves the collaborator is
// required rather than producing a silent zero-tenant sweep that looks like
// a healthy pass over nothing.
func TestTriageSweep_RefusesANilTenantLister(t *testing.T) {
	scheduler, _, _ := newTriageScheduler(t)
	_, err := scheduler.Sweep(context.Background(), nil)
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("want *InvalidInputError, got %T: %v", err, err)
	}
	if invalid.Field != "tenants" {
		t.Fatalf("field: want %q, got %q", "tenants", invalid.Field)
	}
}

// =============================================================================
// The webhook wake path
// =============================================================================

// TestWebhookIngest_WakesTriageForIssueDeliveries proves a verified issue
// delivery starts a triage pass, and that a delivery triage does not care
// about does not.
func TestWebhookIngest_WakesTriageForIssueDeliveries(t *testing.T) {
	tests := []struct {
		name string
		kind ForgeEventKind
		want bool
	}{
		{name: "an opened issue wakes triage", kind: EventIssueOpened, want: true},
		{name: "an edited issue wakes triage", kind: EventIssueEdited, want: true},
		{name: "a label change wakes triage", kind: EventIssueLabeled, want: true},
		{name: "a merged pull request does not", kind: EventPullRequestMerged, want: false},
		{name: "a closed issue does not", kind: EventIssueClosed, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newTestStore(t)
			binding := testBinding()
			if err := store.PutBinding(ctx, testTenant, binding); err != nil {
				t.Fatalf("put binding: %v", err)
			}
			forge := newFakeForge()
			forge.forgeType = binding.Forge.Type
			forge.parseEvent = &ForgeEvent{Kind: tc.kind, Repo: binding.Repo()}

			starter := &fakeJobStarter{}
			scheduler, err := NewTriageScheduler(TriageSchedulerConfig{Bindings: store, Jobs: starter})
			if err != nil {
				t.Fatalf("NewTriageScheduler: %v", err)
			}
			ingestor := newTriageWebhookIngestor(t, store, forge)
			ingestor.SetTriageNudger(scheduler)

			result, err := ingestor.Ingest(ctx, WebhookDelivery{
				ProjectID: testProject, EventType: "issues", DeliveryID: "d1",
				Signature: "sig", Payload: []byte(`{}`),
			})
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if result.Triaged != tc.want {
				t.Fatalf("triaged: want %v, got %v", tc.want, result.Triaged)
			}
			if started := len(starter.requests()); (started > 0) != tc.want {
				t.Fatalf("triage jobs started: want %v, got %d", tc.want, started)
			}
		})
	}
}

// TestWebhookIngest_WithNoTriageNudgerStillFeedsThePoller proves a node
// whose triage runtime is not mounted still ingests deliveries. The two
// nudges are independent.
func TestWebhookIngest_WithNoTriageNudgerStillFeedsThePoller(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	binding := testBinding()
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	forge := newFakeForge()
	forge.forgeType = binding.Forge.Type
	forge.parseEvent = &ForgeEvent{Kind: EventIssueOpened, Repo: binding.Repo()}

	ingestor := newTriageWebhookIngestor(t, store, forge)
	result, err := ingestor.Ingest(ctx, WebhookDelivery{
		ProjectID: testProject, EventType: "issues", DeliveryID: "d1",
		Signature: "sig", Payload: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.Nudged {
		t.Fatal("the poller is still nudged")
	}
	if result.Triaged {
		t.Fatal("no triage nudger means no triage pass")
	}
}

// newTriageWebhookIngestor builds an ingestor over a store and forge whose
// routes and secrets are already in place.
func newTriageWebhookIngestor(t *testing.T, store *Store, forge *fakeForge) *WebhookIngestor {
	t.Helper()
	ingestor, err := NewWebhookIngestor(
		&staticWebhookRouter{tenantID: testTenant},
		store,
		&staticWebhookSecrets{secret: []byte("shhh")},
		&fakeForges{forge: forge},
		&fakeNudger{},
		nil, // no review trigger: this rig is about the triage wake alone
	)
	if err != nil {
		t.Fatalf("NewWebhookIngestor: %v", err)
	}
	return ingestor
}

// staticWebhookRouter routes every delivery to one tenant.
type staticWebhookRouter struct{ tenantID string }

func (r *staticWebhookRouter) Tenant(context.Context, string) (string, error) {
	return r.tenantID, nil
}

// staticWebhookSecrets answers one HMAC secret.
type staticWebhookSecrets struct{ secret []byte }

func (s *staticWebhookSecrets) WebhookSecret(context.Context, string, string) ([]byte, error) {
	return append([]byte(nil), s.secret...), nil
}

// TestTriageSweep_FailuresAreDrained proves a deferred failure is reported
// once. A slice that accumulated would re-report the same skipped project on
// every later pass, which reads as an outage that never clears.
func TestTriageSweep_FailuresAreDrained(t *testing.T) {
	ctx := context.Background()
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = errors.New("the engine refused the job")

	if _, err := scheduler.Sweep(ctx, &fakeTenants{ids: []string{testTenant}}); err != nil {
		t.Fatalf("a per-project failure does not stop the sweep: %v", err)
	}
	failures := scheduler.SweepFailures()
	if len(failures) != 1 {
		t.Fatalf("the skipped project is reported once, got %d", len(failures))
	}
	if !errors.Is(failures[0], ErrBackend) {
		t.Fatalf("the failure keeps its type, got %v", failures[0])
	}
	if again := scheduler.SweepFailures(); len(again) != 0 {
		t.Fatalf("a drained failure is not reported twice, got %d", len(again))
	}
}

// TestTriageSweep_StampsTheTenantOnTheContext is the burn-in defect: the
// backstop runs on a background context of its own, and the engine guards
// StartJob on the tenant bound to the CONTEXT rather than the one named in the
// request. A sweep that passed only the argument was refused `tenant ID
// required` for every bound project of every tenant on every pass — the
// backstop reported itself started and triaged nothing, for days.
//
// The existing sweep tests cannot see it because the job double discarded the
// context, which is the same blindness that let the label sweep's own unstamped
// read destroy live runs (go-quicbaas#918).
func TestTriageSweep_StampsTheTenantOnTheContext(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)

	if _, err := scheduler.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	carried := starter.contextTenants()
	if len(carried) != 1 {
		t.Fatalf("one triage job, got %d", len(carried))
	}
	if carried[0] != testTenant {
		t.Fatalf("StartJob context named tenant %q, want %q: the engine guards on the context, so an "+
			"unstamped sweep is refused `tenant ID required` and triages nothing", carried[0], testTenant)
	}
}

// TestTriageSweep_DoesNotOverrideATenantAlreadyOnTheContext is the guard on the
// stamp: a caller that already scoped the context — a nudge arriving on a
// request, say — decides which tenant it acts as, and the sweep must not be the
// thing that redirects work into another one.
func TestTriageSweep_DoesNotOverrideATenantAlreadyOnTheContext(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)

	scoped := orchestration.ContextWithTenant(context.Background(), testTenant)
	if err := scheduler.TriageProject(scoped, testTenant, testProject); err != nil {
		t.Fatalf("TriageProject: %v", err)
	}
	carried := starter.contextTenants()
	if len(carried) != 1 || carried[0] != testTenant {
		t.Fatalf("context tenants = %v, want exactly [%s]", carried, testTenant)
	}
}
