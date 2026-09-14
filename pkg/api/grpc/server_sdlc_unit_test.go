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
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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
			ctx := context.Background()
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
	if _, err := sdlcTenant(context.Background(), ""); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no tenant anywhere: %v", err)
	}
	if got, err := sdlcTenant(context.Background(), "t1"); err != nil || got != "t1" {
		t.Fatalf("requested only: %q %v", got, err)
	}
	bound := iam.ContextWithTenant(context.Background(), "t1")
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
		Worker:          sdlc.WorkerConfig{Backend: "claude", Model: "claude-opus-5"},
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
			ctx := context.Background()
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
// existed in pkg/sdlc and in the REST body, but not in ProjectBinding.proto. So
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
			Backend:        "codex",
			Model:          "gpt-5-codex",
			Image:          "quicbaas-software-factory-worker:0.7.0-alpha",
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
