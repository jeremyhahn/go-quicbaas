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
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// promptRunner is a fakeRunner that declares the workflow prompt set.
type promptRunner struct{ *fakeRunner }

func (promptRunner) PromptSet() prompts.SetKind { return SetWorkflow }

// countingPromptResolver resolves to the embedded defaults, or fails with err,
// and counts every call with the tenant and project it was asked for.
type countingPromptResolver struct {
	calls     atomic.Int32
	err       error
	tenantID  atomic.Value
	projectID atomic.Value
}

func (r *countingPromptResolver) Resolve(ctx context.Context, tenantID, projectID string, kind prompts.SetKind) (*prompts.ResolvedSet, error) {
	r.calls.Add(1)
	r.tenantID.Store(tenantID)
	r.projectID.Store(projectID)
	if r.err != nil {
		return nil, r.err
	}
	return embeddedPrompts.Resolve(ctx, tenantID, projectID, kind)
}

// launchPromptJob registers runner and launches one job of it for tenant
// acme, project proj-1, returning the job ID.
func launchPromptJob(t *testing.T, svc *agentServiceImpl, runner JobRunner, resumed *JobStatus) (string, error) {
	t.Helper()
	jobID := ""
	if resumed != nil {
		jobID = resumed.JobID
	}
	return svc.launchJob(context.Background(), &JobConfig{
		TenantID: "acme", ProjectID: "proj-1", Description: "prompted", Runner: runner.Name(),
	}, jobID, resumed)
}

// awaitPrompts waits for the job's Run to report the set it was handed.
func awaitPrompts(t *testing.T, seen <-chan *prompts.ResolvedSet) *prompts.ResolvedSet {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case set := <-seen:
		return set
	case <-timer.C:
		t.Fatal("the job never ran")
		return nil
	}
}

// reportingRun is a Run that hands the job's prompt set — as JobContext
// carries it — to seen, then blocks until release closes.
func reportingRun(seen chan<- *prompts.ResolvedSet, release <-chan struct{}) func(context.Context, *JobRun) error {
	return func(_ context.Context, job *JobRun) error {
		seen <- job.Context().Prompts
		<-release
		return nil
	}
}

// TestLaunchJob_RefusesAJobWhosePromptsCannotResolve proves a resolve failure
// refuses the launch with the resolver's own error and registers nothing.
func TestLaunchJob_RefusesAJobWhosePromptsCannotResolve(t *testing.T) {
	svc := newTestService(t)
	refusal := &prompts.ResolveError{TenantID: "acme", Set: SetWorkflow, Prompt: "worker", Cause: errors.New("barrier sealed")}
	resolver := &countingPromptResolver{err: refusal}
	svc.SetPromptResolver(resolver)
	runner := promptRunner{&fakeRunner{name: "prompt-refusal-test"}}
	registerJobRunnerForTest(t, runner)

	_, err := launchPromptJob(t, svc, runner, nil)
	if !errors.Is(err, prompts.ErrResolve) {
		t.Fatalf("launchJob error = %v, want the resolve refusal", err)
	}
	if resolver.calls.Load() != 1 {
		t.Fatalf("the set was resolved %d times, want once", resolver.calls.Load())
	}
	count := 0
	svc.jobs.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatalf("a refused job must not be registered, found %d", count)
	}
}

// TestLaunchJob_ARunnerWithoutAPromptSetResolvesNothing proves a runner that
// does not declare a set is launched with no prompts and no resolver call.
func TestLaunchJob_ARunnerWithoutAPromptSetResolvesNothing(t *testing.T) {
	svc := newTestService(t)
	resolver := &countingPromptResolver{err: errors.New("must not be asked")}
	svc.SetPromptResolver(resolver)
	seen := make(chan *prompts.ResolvedSet, 1)
	release := make(chan struct{})
	defer close(release)
	runner := &fakeRunner{name: "prompt-none-test", run: reportingRun(seen, release)}
	registerJobRunnerForTest(t, runner)

	jobID, err := launchPromptJob(t, svc, runner, nil)
	if err != nil {
		t.Fatalf("launchJob: %v", err)
	}
	defer cancelAllJobs(svc)

	if set := awaitPrompts(t, seen); set != nil {
		t.Fatalf("a runner without a prompt set was handed %v", set.Kind())
	}
	if resolver.calls.Load() != 0 {
		t.Fatal("the resolver was asked for a runner that renders no prompts")
	}
	status, err := svc.GetJobStatus(ContextWithTenant(context.Background(), "acme"), "acme", jobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	if status.Prompts != nil {
		t.Fatalf("the status records prompts for a runner with none: %v", status.Prompts)
	}
	for _, event := range status.Events {
		if event.Type == AuditPromptsResolved {
			t.Fatal("a job with no prompt set audited a resolution")
		}
	}
}

// TestLaunchJob_RecordsTheResolvedPrompts proves a runner declaring a set is
// handed the set resolved for its tenant and project, and that the versions it
// ran with are audited, persisted and returned by GetJobStatus.
func TestLaunchJob_RecordsTheResolvedPrompts(t *testing.T) {
	svc := newTestService(t)
	svc.kvStore = newMockKVStore()
	resolver := &countingPromptResolver{}
	svc.SetPromptResolver(resolver)
	seen := make(chan *prompts.ResolvedSet, 1)
	release := make(chan struct{})
	defer close(release)
	runner := promptRunner{&fakeRunner{name: "prompt-record-test", run: reportingRun(seen, release)}}
	registerJobRunnerForTest(t, runner)

	jobID, err := launchPromptJob(t, svc, runner, nil)
	if err != nil {
		t.Fatalf("launchJob: %v", err)
	}
	defer cancelAllJobs(svc)

	set := awaitPrompts(t, seen)
	if set == nil || set.Kind() != SetWorkflow {
		t.Fatalf("the job was handed %v, want the workflow set", set)
	}
	if resolver.tenantID.Load() != "acme" || resolver.projectID.Load() != "proj-1" {
		t.Fatalf("resolved for %v/%v, want acme/proj-1", resolver.tenantID.Load(), resolver.projectID.Load())
	}
	want := set.Refs()
	if len(want) != len(workflowPrompts.Names()) {
		t.Fatalf("%d refs for %d workflow prompts", len(want), len(workflowPrompts.Names()))
	}

	status, err := svc.GetJobStatus(ContextWithTenant(context.Background(), "acme"), "acme", jobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	requireRefs(t, "returned", status.Prompts, want)
	persisted, err := svc.loadJobStatus("acme", jobID)
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	requireRefs(t, "persisted", persisted.Prompts, want)

	audited := false
	for _, event := range status.Events {
		if event.Type == AuditPromptsResolved {
			audited = strings.HasPrefix(event.Details, "set=workflow")
		}
	}
	if !audited {
		t.Fatalf("no %s event naming the set in %+v", AuditPromptsResolved, status.Events)
	}
}

// TestResumeJob_ResolvesThePromptsAfresh proves a resumed job does not inherit
// the versions its first launch recorded: it resolves again, and a store that
// can no longer resolve refuses the resumption.
func TestResumeJob_ResolvesThePromptsAfresh(t *testing.T) {
	svc := newTestService(t)
	svc.kvStore = newMockKVStore()
	resolver := &countingPromptResolver{}
	svc.SetPromptResolver(resolver)
	seen := make(chan *prompts.ResolvedSet, 1)
	release := make(chan struct{})
	defer close(release)
	runner := promptRunner{&fakeRunner{name: "prompt-resume-test", run: reportingRun(seen, release)}}
	registerJobRunnerForTest(t, runner)

	stale := []prompts.VersionRef{{Set: SetWorkflow, Name: "worker", Scope: prompts.ScopeTenant, ScopeID: "acme", Version: 7, Hash: "old"}}
	interrupted := &JobStatus{JobID: "job-resumed", TenantID: "acme", State: JobStateInitializing,
		Runner: runner.Name(), Prompts: stale}
	resolver.err = errors.New("barrier sealed")
	if _, err := launchPromptJob(t, svc, runner, interrupted); err == nil {
		t.Fatal("a resumption whose prompts cannot resolve must be refused")
	}

	resolver.err = nil
	if _, err := launchPromptJob(t, svc, runner, interrupted); err != nil {
		t.Fatalf("resume launch: %v", err)
	}
	defer cancelAllJobs(svc)

	set := awaitPrompts(t, seen)
	if resolver.calls.Load() != 2 {
		t.Fatalf("resolver called %d times, want once per launch", resolver.calls.Load())
	}
	status, err := svc.GetJobStatus(ContextWithTenant(context.Background(), "acme"), "acme", "job-resumed")
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	requireRefs(t, "resumed", status.Prompts, set.Refs())
}

// requireRefs fails unless got lists exactly want.
func requireRefs(t *testing.T, what string, got, want []prompts.VersionRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s prompts: %d refs, want %d", what, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s prompts[%d] = %+v, want %+v", what, i, got[i], want[i])
		}
	}
}

// TestPromptsResolvedDetail proves the audit line names the set, every
// prompt something other than the build answered, and how many the build did.
func TestPromptsResolvedDetail(t *testing.T) {
	t.Parallel()
	set, err := embeddedPrompts.Resolve(context.Background(), "acme", "", SetWorkflow)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got := promptsResolvedDetail(set)
	want := "set=workflow embedded=" + strconv.Itoa(len(workflowPrompts.Names()))
	if got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
}

// TestJobRun_WorkflowRendererIsTheLaunchedSet proves the control loop renders
// from the set the job was launched with, and from the build's defaults for a
// run that has none.
func TestJobRun_WorkflowRendererIsTheLaunchedSet(t *testing.T) {
	t.Parallel()
	set, err := embeddedPrompts.Resolve(context.Background(), "acme", "", SetWorkflow)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	launched := newEnvelopeRun(t, JobRunConfig{Config: &JobConfig{TenantID: "acme"}, Prompts: set})
	if got := launched.workflowRenderer(); got != prompts.Renderer(set) {
		t.Fatalf("a launched job renders from %T, want its resolved set", got)
	}
	if got := launched.Context().Prompts; got != set {
		t.Fatal("JobContext does not carry the launched set")
	}

	bare := newEnvelopeRun(t, JobRunConfig{Config: &JobConfig{TenantID: "acme"}})
	if got := bare.workflowRenderer(); got != prompts.Renderer(workflowPrompts) {
		t.Fatalf("a run with no set renders from %T, want the build's defaults", got)
	}
	var none *JobRun
	if got := none.workflowRenderer(); got != prompts.Renderer(workflowPrompts) {
		t.Fatal("a nil run must render from the build's defaults")
	}
}

// TestSetPromptResolver_UnsetFallsBackToTheBuild proves a service nothing
// wired a resolver into resolves to the embedded defaults rather than failing.
func TestSetPromptResolver_UnsetFallsBackToTheBuild(t *testing.T) {
	t.Parallel()
	svc := &agentServiceImpl{}
	set, err := svc.resolveJobPrompts(context.Background(), SetWorkflow, &JobConfig{TenantID: "acme"})
	if err != nil {
		t.Fatalf("resolveJobPrompts: %v", err)
	}
	for _, ref := range set.Refs() {
		if ref.Scope != prompts.ScopeEmbedded {
			t.Fatalf("%s resolved from %s with no resolver wired", ref.Name, ref.Scope)
		}
	}

	if _, err := svc.resolveJobPrompts(context.Background(), "no-such-set", &JobConfig{TenantID: "acme"}); !errors.Is(err, prompts.ErrResolve) {
		t.Fatalf("an unknown set resolved: %v", err)
	}
}
