//go:build codec_json

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

package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// profiledRig binds the rig's project with a codex default and an advisory
// qwen alternate, both tokens resolvable.
func profiledRig(t *testing.T) *sdlcServiceRig {
	t.Helper()
	rig := newSDLCServiceRig(t)
	rig.secrets.secrets["codex-tok"] = []byte("codex")
	rig.secrets.secrets["qwen-tok"] = []byte("qwen")
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, profiledServiceBinding(rig), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return rig
}

func profiledServiceBinding(rig *sdlcServiceRig) *sdlc.ProjectBinding {
	binding := rig.binding()
	binding.Reviewer = sdlc.ReviewerConfig{Agent: serviceReviewerAgent, Login: "codex", TokenSecretRef: "codex-tok"}
	binding.Reviewers = []sdlc.ReviewerConfig{{
		Name: "qwen", Agent: serviceQwenAgent, Login: "qwen-reviewer", TokenSecretRef: "qwen-tok",
	}}
	return binding
}

// TestSDLCService_BindChecksEveryReviewerProfilesToken is the second trap the
// reviewer_not_on_the_wire note records, extended to alternates: a dangling
// token ref must fail at bind, not after a model run the operator asked for.
func TestSDLCService_BindChecksEveryReviewerProfilesToken(t *testing.T) {
	rig := newSDLCServiceRig(t)
	rig.secrets.secrets["codex-tok"] = []byte("codex")
	binding := profiledServiceBinding(rig)
	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{})
	if err == nil {
		t.Fatal("Bind accepted an alternate whose token secret does not resolve")
	}
	if _, getErr := rig.store.GetBinding(context.Background(), sdlcTestTenant, sdlcTestProject); !errors.Is(getErr, sdlc.ErrNotFound) {
		t.Fatalf("a refused bind stored the binding: %v", getErr)
	}
}

// TestSDLCService_BindReviewerTokenReferences pins which error each reviewer
// token outcome is: an absent secret is a missing reference on the profile's
// own field, and a secrets store that cannot answer is unavailable — never a
// false "does not exist".
func TestSDLCService_BindReviewerTokenReferences(t *testing.T) {
	t.Run("alternate token missing", func(t *testing.T) {
		rig := newSDLCServiceRig(t)
		rig.secrets.secrets["codex-tok"] = []byte("codex")
		_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, profiledServiceBinding(rig), BindOptions{})
		requireReferenceNotFound(t, err, "reviewers[0].token_secret_ref")
	})
	t.Run("default token, secrets store cannot answer", func(t *testing.T) {
		rig := newSDLCServiceRig(t)
		rig.secrets.secrets["qwen-tok"] = []byte("qwen")
		rig.secrets.errFor = map[string]error{"codex-tok": errReferenceStoreSealed}
		_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, profiledServiceBinding(rig), BindOptions{})
		requireReferenceUnavailable(t, err, "reviewer.token_secret_ref")
	})
	t.Run("webhook secret, secrets store cannot answer", func(t *testing.T) {
		rig := newSDLCServiceRig(t)
		rig.secrets.errFor = map[string]error{"wh": errReferenceStoreSealed}
		_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{})
		requireReferenceUnavailable(t, err, "forge.webhook_secret_ref")
	})
}

func TestSDLCService_BindKeepsTheAlternates(t *testing.T) {
	rig := profiledRig(t)
	stored, err := rig.svc.Binding(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Reviewers) != 1 || stored.Reviewers[0].Name != "qwen" || stored.Reviewers[0].TokenSecretRef != "qwen-tok" {
		t.Fatalf("the alternates did not survive the bind: %+v", stored.Reviewers)
	}
}

func TestSDLCService_ReviewerOverrideVerbs(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()

	selection, err := rig.svc.ReviewerSelection(ctx, sdlcTestTenant, sdlcTestProject, 41)
	if err != nil || selection.Overridden || selection.Profile != sdlc.DefaultReviewerProfileName || !selection.CountsTowardMerge {
		t.Fatalf("with no override: %+v, %v", selection, err)
	}

	selection, err = rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen")
	if err != nil || !selection.Overridden || selection.Profile != "qwen" || selection.CountsTowardMerge ||
		selection.StatusContext != sdlc.DefaultReviewStatusContext+"/qwen" {
		t.Fatalf("after set: %+v, %v", selection, err)
	}
	// The dispatcher's port reads exactly what the verb wrote.
	override, err := rig.svc.GetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41)
	if err != nil || override.Profile != "qwen" {
		t.Fatalf("the override port read %+v, %v", override, err)
	}

	selection, err = rig.svc.ClearReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41)
	if err != nil || selection.Overridden || selection.Profile != sdlc.DefaultReviewerProfileName {
		t.Fatalf("after clear: %+v, %v", selection, err)
	}
	if _, err := rig.svc.ClearReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41); err != nil {
		t.Fatalf("clearing twice is not an error: %v", err)
	}
}

func TestSDLCService_ReviewerOverrideRefusals(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	cases := map[string]struct {
		tenant, project, profile string
		pr                       int
		want                     error
	}{
		"a profile the binding does not carry": {sdlcTestTenant, sdlcTestProject, "gpt-9", 41, sdlc.ErrInvalidInput},
		"no profile at all":                    {sdlcTestTenant, sdlcTestProject, " ", 41, sdlc.ErrInvalidInput},
		"pull request zero":                    {sdlcTestTenant, sdlcTestProject, "qwen", 0, sdlc.ErrInvalidInput},
		"no tenant":                            {"", sdlcTestProject, "qwen", 41, sdlc.ErrInvalidInput},
		"an unbound project":                   {sdlcTestTenant, "proj-unbound", "qwen", 41, sdlc.ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rig.svc.SetReviewerOverride(ctx, tc.tenant, tc.project, tc.pr, tc.profile); !errors.Is(err, tc.want) {
				t.Fatalf("set: want %v, got %v", tc.want, err)
			}
		})
	}
	if _, err := rig.svc.ReviewerSelection(ctx, sdlcTestTenant, sdlcTestProject, -1); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("read of pull request -1: %v", err)
	}
	if _, err := rig.svc.ClearReviewerOverride(ctx, sdlcTestTenant, "proj-unbound", 41); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("clear on an unbound project: %v", err)
	}
}

func TestSDLCService_ReviewerOverrideNeedsAReviewerToChooseBetween(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, sdlc.DefaultReviewerProfileName); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("an override on a project with no reviewer was accepted: %v", err)
	}
}

// TestSDLCService_UnbindForgetsTheOverrides proves a later binding, whose
// profiles may differ, does not inherit choices made about this one.
func TestSDLCService_UnbindForgetsTheOverrides(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	if _, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.store.GetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("an override outlived its binding: %v", err)
	}
}

// An override whose binding check passed before an unbind, and whose write
// lands after the unbind cleared the overrides, must not reach the project's
// next binding: an override chooses among one binding's reviewers only.
func TestSDLCService_OverrideRacingAnUnbindDoesNotReachTheNextBinding(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	// SetReviewerOverride's first read is its binding check; the unbind and
	// the rebind land between that check and the override's write.
	pauseAfterNextRead(t, rig, func() {
		if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
			t.Errorf("Unbind: %v", err)
		}
		if _, err := rig.svc.Bind(ctx, sdlcTestTenant, profiledServiceBinding(rig), BindOptions{}); err != nil {
			t.Errorf("Bind: %v", err)
		}
	})
	selection, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen")
	if err != nil {
		t.Fatalf("SetReviewerOverride: %v", err)
	}
	if selection.Overridden {
		t.Fatalf("the answer to the set reports an override the new binding never chose: %+v", selection)
	}
	selection, err = rig.svc.ReviewerSelection(ctx, sdlcTestTenant, sdlcTestProject, 41)
	if err != nil || selection.Overridden || selection.Profile != sdlc.DefaultReviewerProfileName {
		t.Fatalf("the new binding inherited an override made under the old one: %+v, %v", selection, err)
	}

	// An override set under the new binding applies to it.
	selection, err = rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen")
	if err != nil || !selection.Overridden || selection.Profile != "qwen" {
		t.Fatalf("an override under the current binding: %+v, %v", selection, err)
	}
}

// pausingKV runs a hook once, right after the next read returns from the
// store, so a test lands a write between a caller's check and its write.
type pausingKV struct {
	qrdbsdk.AtomicKVStore

	mu        sync.Mutex
	afterRead func()
}

func (s *pausingKV) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	s.mu.Lock()
	hook := s.afterRead
	s.afterRead = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return value, err
}

// pauseAfterNextRead wraps the rig tenant's store so hook runs after the
// next read from it.
func pauseAfterNextRead(t *testing.T, rig *sdlcServiceRig, hook func()) {
	t.Helper()
	rig.resolver.mu.Lock()
	defer rig.resolver.mu.Unlock()
	held, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	rig.resolver.stores[sdlcTestTenant] = &pausingKV{AtomicKVStore: held, afterRead: hook}
}

// reviewJobs is the engine a reviewer override dispatches on. While a review
// of the pull request runs, the engine refuses another on the pull request's
// lease, exactly as the job lease does; startErr fails every start.
type reviewJobs struct {
	mu       sync.Mutex
	running  bool
	startErr error
	started  []*orchestration.StartJobRequest
}

func (j *reviewJobs) StartJob(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.startErr != nil {
		return nil, j.startErr
	}
	if j.running {
		return nil, &orchestration.JobLeaseTakenError{JobID: "new", HeldBy: "review-41"}
	}
	j.started = append(j.started, req)
	return &orchestration.StartJobResponse{JobID: "review-by-new-seat"}, nil
}

// TestSDLCService_ReviewerOverrideStartsTheChosenSeat: with no review
// running, an override starts one now, as the chosen seat.
func TestSDLCService_ReviewerOverrideStartsTheChosenSeat(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	jobs := &reviewJobs{}
	dispatcher, err := sdlc.NewReviewDispatcher(rig.svc, rig.svc, jobs, rig.svc)
	if err != nil {
		t.Fatal(err)
	}
	rig.svc.SetReviewDispatcher(dispatcher)

	selection, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen")
	if err != nil {
		t.Fatalf("SetReviewerOverride: %v", err)
	}
	if !selection.Redispatched {
		t.Fatalf("selection = %+v, want a review by the chosen seat started", selection)
	}
	if len(jobs.started) != 1 || jobs.started[0].Params[sdlc.ParamReviewerProfile] != "qwen" ||
		jobs.started[0].Lead.AgentID != serviceQwenAgent {
		t.Fatalf("the review started = %+v, want the qwen seat", jobs.started)
	}

	// Detached, an override is only recorded.
	rig.svc.SetReviewDispatcher(nil)
	selection, err = rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, sdlc.DefaultReviewerProfileName)
	if err != nil || selection.Redispatched || len(jobs.started) != 1 {
		t.Fatalf("with no dispatcher: %+v, %v", selection, err)
	}
}

// TestSDLCService_ReviewerOverrideNeverCancelsARunningReview is owner ruling
// 2026-09-25: switching reviewer cancels nothing. The running review keeps the
// pull request, the switch is recorded, and it reports that nothing started.
func TestSDLCService_ReviewerOverrideNeverCancelsARunningReview(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	jobs := &reviewJobs{running: true}
	dispatcher, err := sdlc.NewReviewDispatcher(rig.svc, rig.svc, jobs, rig.svc)
	if err != nil {
		t.Fatal(err)
	}
	rig.svc.SetReviewDispatcher(dispatcher)

	selection, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen")
	if err != nil {
		t.Fatalf("SetReviewerOverride: %v", err)
	}
	if selection.Redispatched || selection.Profile != "qwen" || !selection.Overridden {
		t.Fatalf("selection = %+v, want the override recorded and nothing started while a review runs", selection)
	}
}

// TestSDLCService_ReviewerOverrideReportsAFailedDispatch: the engine's refusal
// is the caller's answer, and the override it followed is still in force.
func TestSDLCService_ReviewerOverrideReportsAFailedDispatch(t *testing.T) {
	rig := profiledRig(t)
	ctx := context.Background()
	boom := errors.New("engine down")
	dispatcher, err := sdlc.NewReviewDispatcher(rig.svc, rig.svc, &reviewJobs{startErr: boom}, rig.svc)
	if err != nil {
		t.Fatal(err)
	}
	rig.svc.SetReviewDispatcher(dispatcher)

	if _, err := rig.svc.SetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41, "qwen"); !errors.Is(err, boom) {
		t.Fatalf("SetReviewerOverride = %v, want the engine's refusal", err)
	}
	override, err := rig.store.GetReviewerOverride(ctx, sdlcTestTenant, sdlcTestProject, 41)
	if err != nil || override.Profile != "qwen" {
		t.Fatalf("override = %+v, %v; want it recorded despite the failed dispatch", override, err)
	}
}

func TestSDLCService_AgentReferencesListsEveryBindingsSlots(t *testing.T) {
	rig := profiledRig(t)
	refs, err := rig.svc.AgentReferences(context.Background(), sdlcTestTenant)
	if err != nil {
		t.Fatalf("AgentReferences: %v", err)
	}
	var slots []string
	for _, ref := range refs {
		slots = append(slots, ref.Usage.Slot+"="+ref.Ref.AgentID)
	}
	want := []string{"worker=" + serviceCoderAgent, "reviewer:default=" + serviceReviewerAgent, "reviewer:qwen=" + serviceQwenAgent}
	if !slices.Equal(slots, want) {
		t.Fatalf("slots = %v, want %v", slots, want)
	}
}

func TestSDLCService_BindRefusesAnAgentTheTenantDoesNotDefine(t *testing.T) {
	rig := newSDLCServiceRig(t)
	binding := rig.binding()
	binding.Worker = orchestration.AgentRefTo("ghost")
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{}); !errors.Is(err, orchestration.ErrAgentUnknown) {
		t.Fatalf("Bind = %v, want the unknown agent refused", err)
	}
	binding.Worker = orchestration.AgentRefTo(serviceReviewerAgent)
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{}); !errors.Is(err, orchestration.ErrAgentRoleMismatch) {
		t.Fatalf("Bind = %v, want a reviewer refused as the worker", err)
	}
}
