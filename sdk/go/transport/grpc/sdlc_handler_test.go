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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCHandler_ConvertersAndDispatch(t *testing.T) {
	if !isSDLCService("quicbaas.SDLC") || isSDLCService("quicbaas.Project") || !(&Transport{}).SupportsSDLC() {
		t.Fatal("service detection")
	}
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: "gitea", BaseURL: "u", Owner: "o", Repo: "r",
			Labels: map[string]string{"merged": "done"}, TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok"},
		Strategy:        "gitflow",
		Worker:          agents.AgentRef{AgentID: "claude-coder"},
		Verification:    []string{"make build", "make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 2, RequireIndependentReviewer: true, ChecksSettleWindow: time.Minute, Reviewers: map[string]string{"codex": "openai"}},
		MaxReviewRounds: 4,
		Ranking:         sdlc.RankingPolicy{DefaultPriority: 1, AnalyzerName: "declared", DeclaredDependents: []string{"web"}},
	}
	proto := sdlcBindingToProto(binding)
	proto.CreatedAtUnix = 1_700_000_000
	back := sdlcBindingFromProto(proto)
	if back.ProjectID != "p1" || back.Forge.Labels["merged"] != "done" || back.Review.ChecksSettleWindow != time.Minute || back.Review.Reviewers["codex"] != "openai" ||
		back.Strategy != "gitflow" || back.Worker != binding.Worker || len(back.Verification) != 2 || back.MaxReviewRounds != 4 ||
		back.Ranking.DeclaredDependents[0] != "web" || back.CreatedAt.Unix() != 1_700_000_000 || !back.UpdatedAt.IsZero() {
		t.Fatalf("binding round trip: %+v", back)
	}
	if sdlcBindingFromProto(nil) != nil || sdlcDeliveryFromProto(nil) != nil {
		t.Fatal("nil converts to nil")
	}
	if sparse := sdlcBindingFromProto(&pb.ProjectBinding{ProjectId: "p2"}); sparse.ProjectID != "p2" {
		t.Fatalf("sparse: %+v", sparse)
	}
	d := sdlcDeliveryFromProto(&pb.Delivery{
		TaskId: "t", Attempt: 2, Branch: "b", Base: "develop", Target: "main", RequiresPullRequest: true, DeleteAfterMerge: true,
		HeadSha: "h", Version: 3, CreatedAtUnix: 1_700_000_000,
		PullRequest: &pb.PullRequest{Number: 4, State: "open", Url: "u"},
		Reviews:     []*pb.ReviewRecord{{Reviewer: "codex", Verdict: "approve", HeadSha: "h", Round: 1, SubmittedAtUnix: 1_700_000_000}},
		Checks:      []*pb.CheckRecord{{Name: "ci", Status: "success", FinishedAtUnix: 1_700_000_000}},
		SupersededRuns: []*pb.SupersessionRecord{{
			Name: "ci", ForgeRunId: "run-h0", HeadSha: "h0", SupersededBy: "h", Status: "pending",
			Disposition: "orphaned", Detail: "no cancel route", ObservedAtUnix: 1_700_000_000,
		}},
	})
	if d.PullRequest.Number != 4 || d.Reviews[0].SubmittedAt.Unix() != 1_700_000_000 || d.Checks[0].Name != "ci" || d.Attempt != 2 || d.Version != 3 ||
		d.Base != "develop" || d.Target != "main" || !d.RequiresPullRequest || !d.DeleteAfterMerge {
		t.Fatalf("delivery: %+v", d)
	}
	// The displaced run has to survive the wire: it is the only thing that tells
	// a caller not to re-run a run it can still see on the forge.
	if len(d.SupersededRuns) != 1 {
		t.Fatalf("superseded runs = %+v, want the displacement carried", d.SupersededRuns)
	}
	if r := d.SupersededRuns[0]; r.ForgeRunID != "run-h0" || r.HeadSHA != "h0" || r.SupersededBy != "h" ||
		r.Disposition != "orphaned" || r.Detail != "no cancel route" || r.ObservedAt.Unix() != 1_700_000_000 {
		t.Fatalf("superseded run: %+v", r)
	}
	if sparse := sdlcDeliveryFromProto(&pb.Delivery{TaskId: "t"}); sparse.PullRequest.Number != 0 || len(sparse.SupersededRuns) != 0 {
		t.Fatalf("sparse delivery: %+v", sparse)
	}
	h := newSDLCHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.SDLC", "Nope", nil); err == nil {
		t.Fatal("unknown method accepted")
	}
	for _, method := range []string{"Bind", "Unbind", "Binding", "Delivery"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.SDLC", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
}

// TestSDLCOnboardingConverters_RoundTrip proves the gRPC transport carries
// every field of a report back across the wire.
//
// It matters more than a usual converter test: the report is the ONLY thing
// an operator sees for a repository that is not being driven, and a field
// silently dropped turns a finding with a remedy into a finding without one.
func TestSDLCOnboardingConverters_RoundTrip(t *testing.T) {
	wire := &pb.OnboardingReport{
		ProjectId: "p1", TenantId: "t", Repo: "acme/api",
		WebhookUrl: "https://factory.test/api/v1/sdlc/webhook/p1", WebhookEnsured: true,
		LabelsCreated: []string{"task:ready"},
		Conformance: &pb.ConformanceReport{
			ProjectId: "p1", Conformant: false,
			Checks:   []*pb.ConformanceCheckResult{{Name: "labels", Status: "fail", Findings: 1}},
			Findings: []*pb.ConformanceFinding{{Class: "label_missing", Check: "labels", Subject: "factory:blocked", Detail: "absent", Remedy: "onboard"}},
		},
	}

	report := sdlcOnboardingReportFromProto(wire)
	if report == nil || report.ProjectID != "p1" || !report.WebhookEnsured {
		t.Fatalf("report = %+v", report)
	}
	if report.Conformance == nil || len(report.Conformance.Findings) != 1 {
		t.Fatalf("conformance = %+v", report.Conformance)
	}
	finding := report.Conformance.Findings[0]
	if finding.Class != "label_missing" || finding.Subject != "factory:blocked" || finding.Remedy == "" {
		t.Fatalf("finding = %+v; a finding without its remedy is one the reader has to guess at", finding)
	}
	if len(report.Conformance.Checks) != 1 || report.Conformance.Checks[0].Status != "fail" {
		t.Fatalf("checks = %+v", report.Conformance.Checks)
	}

	if sdlcOnboardingReportFromProto(nil) != nil || sdlcConformanceFromProto(nil) != nil {
		t.Error("a nil report converted to a non-nil one")
	}
}

// TestSDLCBindingConverters_CarryTheOnboardingBlock: the binding's own
// onboarding policy has to survive the wire, or a repository is provisioned
// against the module's defaults instead of the operator's declaration.
func TestSDLCBindingConverters_CarryTheOnboardingBlock(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Onboarding: sdlc.OnboardingConfig{
			WebhookEvents: []string{"issues"}, ProtectedBranch: "trunk",
			RequiredStatusContexts: []string{"quicbaas/review"},
			ForbiddenPaths:         []string{".github/workflows/automerge.yml"},
			ForbiddenPathsRef:      "trunk",
			LabelColors:            map[string]string{"blocked": "ff0000"},
		},
	}

	back := sdlcBindingFromProto(sdlcBindingToProto(binding))
	if back == nil {
		t.Fatal("the binding did not survive the round trip")
	}
	got := back.Onboarding
	if got.ProtectedBranch != "trunk" || got.ForbiddenPathsRef != "trunk" {
		t.Fatalf("onboarding = %+v", got)
	}
	if len(got.WebhookEvents) != 1 || len(got.RequiredStatusContexts) != 1 || len(got.ForbiddenPaths) != 1 {
		t.Fatalf("onboarding lists = %+v", got)
	}
	if got.LabelColors["blocked"] != "ff0000" {
		t.Fatalf("label colours = %v", got.LabelColors)
	}
}

// TestSDLCBindingConverters_ReviewerSurvivesTheWire mirrors the server-side
// regression test: both ends of the gRPC hop have to carry the reviewer, and a
// gap at either end has the same effect — a project that can never be reviewed,
// silently.
func TestSDLCBindingConverters_ReviewerSurvivesTheWire(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer: sdlc.ReviewerConfig{
			Agent:          "codex-reviewer",
			Login:          "codex",
			TokenSecretRef: "sdlc/reviewer/go-luks2/token",
			StatusContext:  "quicbaas/review",
			Endpoint:       "http://gpu-host.internal:11434",
			HostAffinity:   "gpu-host",
		},
		Discovery: "poll",
	}

	proto := sdlcBindingToProto(binding)
	if proto.Reviewer == nil {
		t.Fatal("reviewer dropped on the way to the wire")
	}
	if proto.Discovery != "poll" {
		t.Fatalf("discovery on the wire = %q, want %q", proto.Discovery, "poll")
	}

	back := sdlcBindingFromProto(proto)
	if back.Reviewer != binding.Reviewer {
		t.Fatalf("reviewer round trip: got %+v, want %+v", back.Reviewer, binding.Reviewer)
	}
	if back.Discovery != "poll" {
		t.Fatalf("discovery round trip: got %q, want %q", back.Discovery, "poll")
	}
}

// TestSDLCBindingConverters_UnconfiguredReviewerStaysUnconfigured guards
// against the mappers inventing a reviewer for a project that binds none.
func TestSDLCBindingConverters_UnconfiguredReviewerStaysUnconfigured(t *testing.T) {
	back := sdlcBindingFromProto(sdlcBindingToProto(&sdlc.ProjectBinding{ProjectID: "p2"}))
	if back.Reviewer.Agent != "" || back.Reviewer.Login != "" || back.Reviewer.TokenSecretRef != "" {
		t.Fatalf("empty reviewer came back populated: %+v", back.Reviewer)
	}
	if back.Discovery != "" {
		t.Fatalf("empty discovery came back as %q; the zero value must stay zero", back.Discovery)
	}
}

// TestSDLCOpenWork_ConverterAndDispatch proves every field of a project's open
// work survives the wire, and that the verb refuses the wrong request type.
func TestSDLCOpenWork_ConverterAndDispatch(t *testing.T) {
	work := sdlcOpenWorkFromProto(&pb.OpenWork{
		ProjectId: "p1", Repo: "acme/api", Agent: "agent-api", ReadAtUnix: 1_700_000_000,
		PullRequests: []*pb.OpenPullRequest{{
			Number: 7, Title: "fix", HeadRef: "issue/7-fix", HeadSha: "abc", CiState: "pending",
			ReviewState: "awaiting_review", Url: "u", CreatedAtUnix: 1_699_999_000, AgeSeconds: 1000,
		}},
		Issues: []*pb.OpenIssue{{Number: 3, Title: "held", Labels: []string{"task:blocked"}, Url: "i", CreatedAtUnix: 1_699_998_000, AgeSeconds: 2000}},
	})
	if work.ProjectID != "p1" || work.Repo != "acme/api" || work.Agent != "agent-api" || work.ReadAt.Unix() != 1_700_000_000 {
		t.Fatalf("work = %+v", work)
	}
	pr := work.PullRequests[0]
	if pr.Number != 7 || pr.Title != "fix" || pr.HeadRef != "issue/7-fix" || pr.HeadSHA != "abc" || pr.CIState != "pending" ||
		pr.ReviewState != "awaiting_review" || pr.URL != "u" || pr.Created.Unix() != 1_699_999_000 || pr.AgeSeconds != 1000 {
		t.Fatalf("pull request = %+v", pr)
	}
	issue := work.Issues[0]
	if issue.Number != 3 || issue.Title != "held" || issue.Labels[0] != "task:blocked" || issue.URL != "i" ||
		issue.Created.Unix() != 1_699_998_000 || issue.AgeSeconds != 2000 {
		t.Fatalf("issue = %+v", issue)
	}
	if sdlcOpenWorkFromProto(nil) != nil {
		t.Fatal("nil open work converted to a non-nil one")
	}
	if _, err := newSDLCHandler(nil).handleRequest(context.Background(), "quicbaas.SDLC", "OpenWork", "wrong"); err == nil {
		t.Fatal("OpenWork accepted the wrong request type")
	}
}

// TestSDLCBindingConverters_SlotAppendsSurviveTheWire: a bind replaces the
// whole record, so a slot's prompt append this transport dropped would be
// erased by every rebind through it.
func TestSDLCBindingConverters_SlotAppendsSurviveTheWire(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID:          "p1",
		WorkerPromptAppend: "worker material",
		TriagePromptAppend: "triage material",
		Reviewer:           sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "tok", PromptAppend: "seat material"},
		Reviewers:          []sdlc.ReviewerConfig{{Name: "qwen", Agent: "qwen-reviewer", PromptAppend: "alternate material"}},
	}
	back := sdlcBindingFromProto(sdlcBindingToProto(binding))
	if back.WorkerPromptAppend != "worker material" || back.TriagePromptAppend != "triage material" ||
		back.Reviewer.PromptAppend != "seat material" || len(back.Reviewers) != 1 || back.Reviewers[0].PromptAppend != "alternate material" {
		t.Fatalf("round trip lost a slot's append: %+v", back)
	}
	if empty := sdlcBindingFromProto(sdlcBindingToProto(&sdlc.ProjectBinding{ProjectID: "p2"})); empty.WorkerPromptAppend != "" || empty.Reviewer.PromptAppend != "" {
		t.Fatalf("the converters invented an append: %+v", empty)
	}
}
