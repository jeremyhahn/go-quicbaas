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
		Worker:          sdlc.WorkerConfig{Backend: "claude", Model: "claude-opus-5"},
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
			Backend:        "codex",
			Model:          "gpt-5-codex",
			Image:          "quicbaas-software-factory-worker:0.7.0-alpha",
			Login:          "codex",
			TokenSecretRef: "sdlc/reviewer/go-luks2/token",
			StatusContext:  "quicbaas/review",
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
	if back.Reviewer.Backend != "" || back.Reviewer.Login != "" || back.Reviewer.TokenSecretRef != "" {
		t.Fatalf("empty reviewer came back populated: %+v", back.Reviewer)
	}
	if back.Discovery != "" {
		t.Fatalf("empty discovery came back as %q; the zero value must stay zero", back.Discovery)
	}
}
