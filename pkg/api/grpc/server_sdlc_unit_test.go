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

package grpc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type nilSDLCProvider struct{}

func (nilSDLCProvider) SDLC() *service.SDLCService { return nil }

func sdlcTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	return log
}

func TestSDLCServer_AnswersUnavailableWithoutTheModule(t *testing.T) {
	for name, srv := range map[string]*SDLCServer{
		"nil provider":    NewSDLCServer(nil, sdlcTestLogger(t)),
		"disabled module": NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t)),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := tenantctx.WithSystem(context.Background())
			if _, err := srv.BindProject(ctx, &pb.BindProjectRequest{Binding: &pb.ProjectBinding{ProjectId: "p"}}); status.Code(err) != codes.Unavailable {
				t.Fatalf("BindProject: %v", err)
			}
			if _, err := srv.UnbindProject(ctx, &pb.UnbindProjectRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
				t.Fatalf("UnbindProject: %v", err)
			}
			if _, err := srv.GetBinding(ctx, &pb.GetBindingRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
				t.Fatalf("GetBinding: %v", err)
			}
			if _, err := srv.GetDelivery(ctx, &pb.GetDeliveryRequest{TaskId: "t"}); status.Code(err) != codes.Unavailable {
				t.Fatalf("GetDelivery: %v", err)
			}
		})
	}
}

func TestSDLCServer_TenantResolution(t *testing.T) {
	// An unbound call is refused, whether or not it names a tenant: the
	// request is not trusted to name its own (go-quicbaas#1150).
	if _, err := sdlcTenant(context.Background(), ""); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("no tenant anywhere: %v", err)
	}
	if _, err := sdlcTenant(context.Background(), "t1"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("requested only: %v", err)
	}
	bound := tenantctx.WithTenant(context.Background(), "t1")
	if got, err := sdlcTenant(bound, ""); err != nil || got != "t1" {
		t.Fatalf("bound only: %q %v", got, err)
	}
	if got, err := sdlcTenant(bound, "t1"); err != nil || got != "t1" {
		t.Fatalf("bound and same: %q %v", got, err)
	}
	if _, err := sdlcTenant(bound, "t2"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("bound and different: %v", err)
	}
}

func TestSDLCServer_ErrorMapping(t *testing.T) {
	cases := map[error]codes.Code{
		&sdlc.NotFoundError{Entity: "binding"}:             codes.NotFound,
		&service.ProjectNotFoundError{ProjectID: "p"}:      codes.NotFound,
		&sdlc.AlreadyExistsError{}:                         codes.AlreadyExists,
		&sdlc.VersionConflictError{}:                       codes.AlreadyExists,
		&sdlc.WebhookRouteConflictError{ProjectID: "p"}:    codes.AlreadyExists,
		&sdlc.InvalidInputError{Field: "f"}:                codes.InvalidArgument,
		&sdlc.ForgeNotRegisteredError{Type: "x"}:           codes.InvalidArgument,
		&sdlc.StrategyNotRegisteredError{Name: "nope"}:     codes.InvalidArgument,
		&orchestration.WorkflowValidationError{Field: "f"}: codes.InvalidArgument,
		sdlc.ErrUnavailable:                                codes.Unavailable,
		&sdlc.AdoptionRefusedError{Subject: "a#1"}:         codes.FailedPrecondition,
		&sdlc.AdoptionIncompleteError{TaskID: "t"}:         codes.Unavailable,
		&orchestration.TaskSourceAlreadyAdoptedError{}:     codes.AlreadyExists,
		errors.New("mystery"):                              codes.Internal,
	}
	for err, want := range cases {
		if got := status.Code(convertSDLCError(err)); got != want {
			t.Fatalf("%T → %s, want %s", err, got, want)
		}
	}
}

func TestSDLCConverters_RoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: sdlc.ForgeGitea, BaseURL: "http://f", Owner: "o", Repo: "r",
			Labels: map[string]string{"merged": "done"}, TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok"},
		Strategy:        sdlc.GitFlowName,
		Worker:          orchestration.AgentRefTo("claude-coder"),
		Verification:    []string{"make build", "make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 2, RequireIndependentReviewer: true, RequireGreenChecks: true, ChecksSettleWindow: time.Minute, Reviewers: map[string]string{"codex": "openai"}},
		MaxReviewRounds: 4,
		Ranking:         sdlc.RankingPolicy{DefaultPriority: 3, AnalyzerName: "declared", DeclaredDependents: []string{"web"}},
		CreatedAt:       now, UpdatedAt: now,
	}
	proto := bindingToProto(binding)
	if proto.CreatedAtUnix != now.Unix() || proto.Forge.Labels["merged"] != "done" || proto.Review.ChecksSettleWindowNs != int64(time.Minute) {
		t.Fatalf("to proto: %+v", proto)
	}
	back := bindingFromProto(proto)
	back.CreatedAt, back.UpdatedAt = now, now
	if back.ProjectID != binding.ProjectID || back.Forge.Type != binding.Forge.Type || back.Forge.Labels["merged"] != "done" ||
		back.Review.RequiredApprovals != 2 || !back.Review.RequireIndependentReviewer || !back.Review.RequireGreenChecks || back.Review.ChecksSettleWindow != time.Minute ||
		back.Review.Reviewers["codex"] != "openai" || back.Strategy != sdlc.GitFlowName || back.Worker != binding.Worker ||
		len(back.Verification) != 2 || back.Verification[1] != "make test" || back.MaxReviewRounds != 4 ||
		back.Ranking.DefaultPriority != 3 || back.Ranking.DeclaredDependents[0] != "web" {
		t.Fatalf("round trip lost fields: %+v", back)
	}
	if bindingToProto(nil) != nil || deliveryToProto(nil) != nil {
		t.Fatal("nil converts to nil")
	}
	sparse := bindingFromProto(&pb.ProjectBinding{ProjectId: "p2"})
	if sparse.ProjectID != "p2" || sparse.Forge.Type != "" {
		t.Fatalf("sparse proto: %+v", sparse)
	}

	delivery := &sdlc.Delivery{
		TaskID: "t1", ProjectID: "p1", Attempt: 2, Branch: "b", Base: "main", HeadSHA: "h", WorkDir: "/w", Version: 3,
		PullRequest: sdlc.DeliveryPullRequest{Number: 7, URL: "u", State: sdlc.PullRequestOpen, Title: "T", Base: "main"},
		Reviews:     []sdlc.ReviewRecord{{Reviewer: "codex", ModelFamily: "openai", Verdict: sdlc.VerdictApprove, HeadSHA: "h", ExternalID: "9", Round: 1, Findings: "ok", SubmittedAt: now}},
		Checks:      []sdlc.CheckRecord{{Name: "ci", HeadSHA: "h", Status: sdlc.CheckSuccess, ForgeRunID: "1", FinishedAt: now}},
		SupersededRuns: []sdlc.SupersessionRecord{{
			Name: "ci", ForgeRunID: "run-h0", HeadSHA: "h0", SupersededBy: "h", Status: sdlc.CheckPending,
			Disposition: sdlc.SupersessionOrphaned, Detail: "gitea publishes no route to cancel a running check",
			ObservedAt: now,
		}},
		CreatedAt: now, UpdatedAt: now,
	}
	d := deliveryToProto(delivery)
	if d.PullRequest.Number != 7 || d.Reviews[0].SubmittedAtUnix != now.Unix() || d.Checks[0].Status != "success" || d.Version != 3 || d.Attempt != 2 {
		t.Fatalf("delivery proto: %+v", d)
	}
	// THE SUPERSEDED RUN CROSSES THE WIRE. REST serialises the domain record
	// directly, so a gRPC mapper that dropped it would leave two transports
	// disagreeing about whether a displaced run exists — and the caller on the
	// wrong one re-runs it.
	if len(d.SupersededRuns) != 1 {
		t.Fatalf("superseded runs = %+v, want the displacement carried", d.SupersededRuns)
	}
	if r := d.SupersededRuns[0]; r.Name != "ci" || r.ForgeRunId != "run-h0" || r.HeadSha != "h0" ||
		r.SupersededBy != "h" || r.Status != "pending" || r.Disposition != "orphaned" ||
		r.Detail == "" || r.ObservedAtUnix != now.Unix() {
		t.Fatalf("superseded run proto: %+v", r)
	}
	if bare := deliveryToProto(&sdlc.Delivery{TaskID: "t1"}); len(bare.SupersededRuns) != 0 {
		t.Fatalf("a delivery with no displacement produced %+v", bare.SupersededRuns)
	}
	if copyStringMap(nil) != nil {
		t.Fatal("empty map copies to nil")
	}
}

// TestSDLCServer_OnboardingVerbsAnswerUnavailableWithoutTheModule keeps a
// node that does not run the module from answering OK for a repository it
// could never have provisioned.
func TestSDLCServer_OnboardingVerbsAnswerUnavailableWithoutTheModule(t *testing.T) {
	for name, srv := range map[string]*SDLCServer{
		"nil provider":    NewSDLCServer(nil, sdlcTestLogger(t)),
		"disabled module": NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t)),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := tenantctx.WithSystem(context.Background())
			if _, err := srv.Onboard(ctx, &pb.OnboardRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
				t.Errorf("Onboard: %v", err)
			}
			if _, err := srv.GetConformance(ctx, &pb.GetConformanceRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
				t.Errorf("GetConformance: %v", err)
			}
		})
	}
}

// TestSDLCOnboardingConverters_RoundTrip proves every field of a report
// reaches the wire.
//
// It matters more than a usual converter test: the report is the ONLY thing
// an operator sees for a repository that is not being driven, and a field
// silently dropped turns a finding with a remedy into a finding without one.
func TestSDLCOnboardingConverters_RoundTrip(t *testing.T) {
	report := &sdlc.OnboardingReport{
		ProjectID: "p1", TenantID: "t", Repo: "acme/api", WebhookEnsured: true,
		WebhookURL: "https://factory.test/api/v1/sdlc/webhook/p1", LabelsCreated: []string{"task:ready"},
		Conformance: &sdlc.ConformanceReport{
			ProjectID: "p1", Conformant: false, CheckedAt: time.Now().UTC(),
			Checks: []sdlc.ConformanceCheckResult{{Name: sdlc.CheckLabels, Status: sdlc.StatusFail, Findings: 1}},
			Findings: []sdlc.ConformanceFinding{{
				Class: sdlc.ConformanceLabelMissing, Check: sdlc.CheckLabels,
				Subject: "factory:blocked", Detail: "absent", Remedy: "onboard the project",
			}},
		},
	}

	wire := onboardingReportToProto(report)
	if wire == nil || wire.ProjectId != "p1" || !wire.WebhookEnsured || len(wire.LabelsCreated) != 1 {
		t.Fatalf("wire = %+v", wire)
	}
	if wire.Conformance == nil || len(wire.Conformance.Findings) != 1 || len(wire.Conformance.Checks) != 1 {
		t.Fatalf("conformance = %+v", wire.Conformance)
	}
	finding := wire.Conformance.Findings[0]
	if finding.Class != string(sdlc.ConformanceLabelMissing) || finding.Remedy == "" {
		t.Fatalf("finding = %+v; a finding without its remedy is one the reader has to guess at", finding)
	}
	if wire.Conformance.CheckedAtUnix == 0 {
		t.Error("the report lost the instant it was taken")
	}

	if onboardingReportToProto(nil) != nil || conformanceToProto(nil) != nil {
		t.Error("a nil report converted to a non-nil one")
	}
}

// TestSDLCBindingConverters_CarryTheOnboardingBlock: the binding's own
// onboarding policy has to survive the wire in both directions, or a
// repository is provisioned against the module's defaults instead of the
// operator's declaration.
func TestSDLCBindingConverters_CarryTheOnboardingBlock(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Onboarding: sdlc.OnboardingConfig{
			WebhookEvents: []string{"issues"}, ProtectedBranch: "trunk",
			RequiredStatusContexts: []string{sdlc.DefaultReviewStatusContext},
			ForbiddenPaths:         []string{".github/workflows/automerge.yml"},
			ForbiddenPathsRef:      "trunk",
			LabelColors:            map[string]string{sdlc.LabelBlocked: "ff0000"},
		},
	}

	back := bindingFromProto(bindingToProto(binding))
	got := back.Onboarding
	if got.ProtectedBranch != "trunk" || got.ForbiddenPathsRef != "trunk" {
		t.Fatalf("onboarding = %+v", got)
	}
	if len(got.WebhookEvents) != 1 || len(got.RequiredStatusContexts) != 1 || len(got.ForbiddenPaths) != 1 {
		t.Fatalf("onboarding lists = %+v", got)
	}
	if got.LabelColors[sdlc.LabelBlocked] != "ff0000" {
		t.Fatalf("label colours = %v", got.LabelColors)
	}
}

// TestSDLCConverters_ReviewerSurvivesTheWire is the regression test for a
// binding field that had no wire representation at all.
//
// WHAT THE GAP COST, MEASURED ON THE BURN-IN FLEET (2026-09-14): ReviewerConfig
// existed in pkg/orchestration/sdlc and in the REST body, but not in ProjectBinding.proto. So
// a reviewer could be configured over REST and nowhere else; `sdlc binding`
// reported "reviewer: null" whether one was set or not; and because Bind
// REPLACES the whole record, a rebind through gRPC silently erased the reviewer
// a project already had. A project with no reviewer is skipped by BOTH dispatch
// paths — TriggerReview and ReviewSweeper.sweepBinding each return (false, nil)
// on !Reviewer.Configured() — so the symptom was finished, green pull requests
// sitting unreviewed with nothing logged anywhere.
//
// ASSERTED FIELD BY FIELD RATHER THAN AS A STRUCT so that a field added to
// ReviewerConfig and forgotten in the mappers fails here by name.
func TestSDLCConverters_ReviewerSurvivesTheWire(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer: sdlc.ReviewerConfig{
			Agent:          "codex-reviewer",
			Login:          "codex",
			TokenSecretRef: "sdlc/reviewer/go-luks2/token",
			StatusContext:  "quicbaas/review",
		},
		Discovery: sdlc.DiscoveryPoll,
	}

	proto := bindingToProto(binding)
	if proto.Reviewer == nil {
		t.Fatal("reviewer dropped on the way to the wire")
	}
	if proto.Discovery != string(sdlc.DiscoveryPoll) {
		t.Fatalf("discovery on the wire = %q, want %q", proto.Discovery, sdlc.DiscoveryPoll)
	}

	back := bindingFromProto(proto)
	if back.Reviewer != binding.Reviewer {
		t.Fatalf("reviewer round trip: got %+v, want %+v", back.Reviewer, binding.Reviewer)
	}
	if back.Discovery != sdlc.DiscoveryPoll {
		t.Fatalf("discovery round trip: got %q, want %q", back.Discovery, sdlc.DiscoveryPoll)
	}
	if !back.Reviewer.Configured() {
		t.Fatal("a reviewer that survived the wire must read as configured; this is the predicate both dispatch paths gate on")
	}
}

// TestSDLCConverters_UnconfiguredReviewerStaysUnconfigured guards the other
// direction: an absent reviewer must not be invented by the mappers.
//
// A PROJECT REVIEWED BY PEOPLE BINDS NO REVIEWER, and that is a legitimate
// configuration rather than a half-finished one. If the round trip produced a
// non-zero ReviewerConfig, Configured() would start returning true for those
// projects and the review runner would be dispatched for a project that has no
// reviewer account and no token to post with.
func TestSDLCConverters_UnconfiguredReviewerStaysUnconfigured(t *testing.T) {
	back := bindingFromProto(bindingToProto(&sdlc.ProjectBinding{ProjectID: "p2"}))
	if back.Reviewer.Configured() {
		t.Fatalf("empty reviewer became configured: %+v", back.Reviewer)
	}
	if back.Discovery != "" {
		t.Fatalf("empty discovery became %q; the zero value must stay zero so DiscoveryMode.Effective picks the default", back.Discovery)
	}

	// A proto that carries no reviewer message at all is the older-peer case.
	sparse := bindingFromProto(&pb.ProjectBinding{ProjectId: "p3"})
	if sparse.Reviewer.Configured() {
		t.Fatalf("absent reviewer message became configured: %+v", sparse.Reviewer)
	}
}

// profiledProtoBinding is a binding with a fully populated default reviewer
// and two alternates: one advisory by omission, one switched on.
func profiledProtoBinding() *sdlc.ProjectBinding {
	advisory, counted := false, true
	return &sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer: sdlc.ReviewerConfig{
			Name: "codex", Agent: "codex-reviewer", Login: "codex",
			TokenSecretRef: "codex-tok", StatusContext: "quicbaas/review"},
		Reviewers: []sdlc.ReviewerConfig{
			{Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok", CountsTowardMerge: &advisory,
				Endpoint: "http://gpu-host.internal:11434", HostAffinity: "gpu-host"},
			{Name: "gpt", Agent: "codex-reviewer", Login: "gpt-reviewer", TokenSecretRef: "gpt-tok", CountsTowardMerge: &counted},
			{Name: "unset", Agent: "codex-reviewer", Login: "unset-reviewer", TokenSecretRef: "unset-tok"},
		},
	}
}

// TestSDLCConverters_ReviewerProfilesSurviveARebind is the regression test the
// reviewer_not_on_the_wire incident asks of every binding-writing transport: a
// bind REPLACES the record, so a mapper that dropped the alternates — or the
// presence of counts_toward_merge, or reasoning_effort — would erase them on
// the next rebind through gRPC, silently.
func TestSDLCConverters_ReviewerProfilesSurviveARebind(t *testing.T) {
	binding := profiledProtoBinding()
	back := bindingFromProto(bindingToProto(binding))
	if !reflect.DeepEqual(back.Reviewer, binding.Reviewer) {
		t.Fatalf("default reviewer: got %+v, want %+v", back.Reviewer, binding.Reviewer)
	}
	if !reflect.DeepEqual(back.Reviewers, binding.Reviewers) {
		t.Fatalf("alternates: got %+v, want %+v", back.Reviewers, binding.Reviewers)
	}
	// UNSET STAYS UNSET: nil means "the role's default", and turning it into
	// an explicit false would flip a default reviewer to advisory.
	if back.Reviewers[2].CountsTowardMerge != nil {
		t.Fatal("an unset counts_toward_merge came back set")
	}
	if back.Reviewers[0].CountsTowardMerge == nil || *back.Reviewers[0].CountsTowardMerge {
		t.Fatal("an explicit false did not survive")
	}
	// And a rebind of the round-tripped record is the same record again.
	if again := bindingFromProto(bindingToProto(back)); !reflect.DeepEqual(again.Reviewers, binding.Reviewers) {
		t.Fatalf("a second rebind changed the alternates: %+v", again.Reviewers)
	}
	// No alternates is nil, not an empty slice a JSON reader would render.
	if none := bindingFromProto(bindingToProto(&sdlc.ProjectBinding{ProjectID: "p"})); none.Reviewers != nil {
		t.Fatalf("absent alternates became %+v", none.Reviewers)
	}
}

func TestSDLCServer_ReviewerVerbsRefuseWithoutTheModuleOrAProject(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	unavailable := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	if _, err := unavailable.GetReviewerSelection(ctx, &pb.GetReviewerSelectionRequest{ProjectId: "p", PullRequest: 1}); status.Code(err) != codes.Unavailable {
		t.Fatalf("GetReviewerSelection: %v", err)
	}
	if _, err := unavailable.SetReviewerOverride(ctx, &pb.SetReviewerOverrideRequest{ProjectId: "p", PullRequest: 1, Profile: "qwen"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("SetReviewerOverride: %v", err)
	}
	if _, err := unavailable.ClearReviewerOverride(ctx, &pb.ClearReviewerOverrideRequest{ProjectId: "p", PullRequest: 1}); status.Code(err) != codes.Unavailable {
		t.Fatalf("ClearReviewerOverride: %v", err)
	}
	// A nil request names no project, and is refused before a tenant is read.
	if _, _, err := (&SDLCServer{provider: fixedSDLCProvider{svc: &service.SDLCService{}}}).reviewerRequest(ctx, "t", ""); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no project: %v", err)
	}
	if _, _, err := (&SDLCServer{provider: fixedSDLCProvider{svc: &service.SDLCService{}}}).reviewerRequest(context.Background(), "", "p"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("no tenant bound: %v", err)
	}
}

// fixedSDLCProvider hands back one service value.
type fixedSDLCProvider struct{ svc *service.SDLCService }

func (p fixedSDLCProvider) SDLC() *service.SDLCService { return p.svc }

func TestSDLCConverters_ReviewerSelection(t *testing.T) {
	if reviewerSelectionToProto(nil) != nil {
		t.Fatal("a nil selection became a message")
	}
	at := time.Unix(1_700_000_000, 0).UTC()
	got := reviewerSelectionToProto(&sdlc.ReviewerSelection{
		ProjectID: "p", PullRequest: 41, Profile: "qwen", Overridden: true,
		StatusContext: "quicbaas/review/qwen", UpdatedAt: at,
	})
	if got.ProjectId != "p" || got.PullRequest != 41 || got.Profile != "qwen" || !got.Overridden ||
		got.CountsTowardMerge || got.StatusContext != "quicbaas/review/qwen" || got.UpdatedAtUnix != at.Unix() {
		t.Fatalf("selection = %+v", got)
	}
}

// TestSDLCServer_AdoptIssueRefusesWithoutTheModuleOrAProject: the verb needs
// the module and a project, exactly as the other operator verbs do.
func TestSDLCServer_AdoptIssueRefusesWithoutTheModuleOrAProject(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	unavailable := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	if _, err := unavailable.AdoptIssue(ctx, &pb.AdoptIssueRequest{ProjectId: "p", Issue: 1}); status.Code(err) != codes.Unavailable {
		t.Fatalf("AdoptIssue without the module: %v", err)
	}
	server := &SDLCServer{provider: fixedSDLCProvider{svc: &service.SDLCService{}}}
	if _, err := server.AdoptIssue(ctx, &pb.AdoptIssueRequest{TenantId: "t", Issue: 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("AdoptIssue without a project: %v", err)
	}
}

// TestSDLCServer_AttentionRefusesWithoutTheModule: both inbox verbs answer
// Unavailable on a node without the module.
func TestSDLCServer_AttentionRefusesWithoutTheModule(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	unavailable := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	if _, err := unavailable.ListAttention(ctx, &pb.ListAttentionRequest{TenantId: "t"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("ListAttention: %v", err)
	}
	if _, err := unavailable.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "t", TaskId: "x", Action: "readmit"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("ResolveAttention: %v", err)
	}
}

func TestAttentionConverters(t *testing.T) {
	if attentionItemToProto(nil) != nil || facetsToProto(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	item := attentionItemToProto(&sdlc.AttentionItem{ID: "t1", Kind: sdlc.AttentionKind("parked_task"), Attempts: 2,
		Actions: []sdlc.AttentionAction{"readmit", "note"}, RulingPosted: true, CreatedAt: 10})
	if item.GetId() != "t1" || item.GetAttempts() != 2 || len(item.GetActions()) != 2 || !item.GetRulingPosted() || item.GetCreatedAt() != 10 {
		t.Fatalf("item = %+v", item)
	}
	facets := facetsToProto(orchestration.Facets{"kind": {"parked_task": 3}})
	if facets["kind"].GetCounts()["parked_task"] != 3 {
		t.Fatalf("facets = %+v", facets)
	}
}

// TestSDLCServer_GetOpenWorkRefusesWithoutTheModuleOrAProject keeps a node
// without the module from answering an empty project, and a request naming no
// project from reaching it.
func TestSDLCServer_GetOpenWorkRefusesWithoutTheModuleOrAProject(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	for name, srv := range map[string]*SDLCServer{
		"nil provider":    NewSDLCServer(nil, sdlcTestLogger(t)),
		"disabled module": NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := srv.GetOpenWork(ctx, &pb.GetOpenWorkRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
				t.Errorf("GetOpenWork: %v", err)
			}
		})
	}
	if _, err := (&SDLCServer{provider: fixedSDLCProvider{svc: &service.SDLCService{}}}).GetOpenWork(ctx, &pb.GetOpenWorkRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("no project: %v, want InvalidArgument", err)
	}
}

// TestSDLCConverters_OpenWorkRoundTrip proves every field of a project's open
// work reaches the wire.
func TestSDLCConverters_OpenWorkRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	work := &sdlc.OpenWork{
		ProjectID: "p1", Repo: "acme/api", Agent: "agent-api", ReadAt: now,
		PullRequests: []sdlc.OpenPullRequest{{
			Number: 7, Title: "fix", HeadRef: "issue/7-fix", HeadSHA: "abc", CIState: sdlc.CIFailure,
			ReviewState: sdlc.ReviewChangesRequested, URL: "https://forge.test/pulls/7", Created: now.Add(-time.Hour), AgeSeconds: 3600,
		}},
		Issues: []sdlc.OpenIssue{{
			Number: 3, Title: "needs info", Labels: []string{"factory:needs-info"}, URL: "https://forge.test/issues/3",
			Created: now.Add(-2 * time.Hour), AgeSeconds: 7200,
		}},
	}
	out := openWorkToProto(work)
	if out.ProjectId != "p1" || out.Repo != "acme/api" || out.Agent != "agent-api" || out.ReadAtUnix != now.Unix() {
		t.Fatalf("work = %+v", out)
	}
	pr := out.PullRequests[0]
	if pr.Number != 7 || pr.Title != "fix" || pr.HeadRef != "issue/7-fix" || pr.HeadSha != "abc" ||
		pr.CiState != "failure" || pr.ReviewState != "changes_requested" || pr.Url == "" ||
		pr.CreatedAtUnix != now.Add(-time.Hour).Unix() || pr.AgeSeconds != 3600 {
		t.Fatalf("pull request = %+v", pr)
	}
	issue := out.Issues[0]
	if issue.Number != 3 || issue.Title != "needs info" || len(issue.Labels) != 1 || issue.Url == "" ||
		issue.CreatedAtUnix != now.Add(-2*time.Hour).Unix() || issue.AgeSeconds != 7200 {
		t.Fatalf("issue = %+v", issue)
	}
	if openWorkToProto(nil) != nil {
		t.Fatal("nil open work converted to a message")
	}
}

func TestSDLCConverters_SlotAppendsSurviveTheWire(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID:          "p1",
		WorkerPromptAppend: "worker material",
		TriagePromptAppend: "triage material",
		Reviewer:           sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "tok", PromptAppend: "seat material"},
		Reviewers:          []sdlc.ReviewerConfig{{Name: "qwen", Agent: "qwen-reviewer", PromptAppend: "alternate material"}},
	}
	back := bindingFromProto(bindingToProto(binding))
	if back.WorkerPromptAppend != "worker material" || back.TriagePromptAppend != "triage material" ||
		back.Reviewer.PromptAppend != "seat material" || len(back.Reviewers) != 1 || back.Reviewers[0].PromptAppend != "alternate material" {
		t.Fatalf("round trip lost a slot's append: %+v", back)
	}
	if empty := bindingFromProto(&pb.ProjectBinding{ProjectId: "p2"}); empty.WorkerPromptAppend != "" || empty.Reviewer.PromptAppend != "" {
		t.Fatalf("the converters invented an append: %+v", empty)
	}
}
