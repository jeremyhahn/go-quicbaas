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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// sdlcNilFacade is a facade with no SDLC module; every other verb of the
// embedded nil interface is unreachable in these tests.
type sdlcNilFacade struct{ service.QuicBaaS }

func (sdlcNilFacade) SDLC() *service.SDLCService { return nil }

func TestSDLCHandlers_RefuseWithoutTheModuleAndOnBadTypes(t *testing.T) {
	tr, err := New(sdlcNilFacade{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !tr.SupportsSDLC() {
		t.Fatal("SupportsSDLC")
	}
	for method, req := range map[string]interface{}{
		"Bind":     &sdlc.BindRequest{ProjectID: "p"},
		"Unbind":   &sdlc.UnbindRequest{ProjectID: "p"},
		"Binding":  &sdlc.GetBindingRequest{ProjectID: "p"},
		"Delivery": &sdlc.GetDeliveryRequest{TaskID: "t"},
	} {
		if _, err := tr.Request(ctx, "quicbaas.SDLC", method, req); !errors.Is(err, pkgsdlc.ErrUnavailable) {
			t.Fatalf("%s without the module: %v", method, err)
		}
		if _, err := tr.Request(ctx, "quicbaas.SDLC", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
	if _, err := sdlcServiceOf(nil, "Bind"); err == nil {
		t.Fatal("nil facade")
	}
}

func TestSDLCConverters_RoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	in := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: "gitea", BaseURL: "u", Owner: "o", Repo: "r",
			Labels: map[string]string{"merged": "done"}, TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok"},
		Strategy:        "trunk-based",
		Worker:          agents.AgentRef{AgentID: "claude-coder"},
		Verification:    []string{"make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true, ChecksSettleWindow: time.Minute, Reviewers: map[string]string{"codex": "openai"}},
		MaxReviewRounds: 2,
		Ranking:         sdlc.RankingPolicy{DefaultPriority: 2, DeclaredDependents: []string{"web"}},
	}
	domain := sdlcBindingToDomain(in)
	domain.CreatedAt, domain.UpdatedAt = now, now
	out := sdlcBindingFromDomain(domain)
	if out.ProjectID != "p1" || out.Forge.Type != "gitea" || out.Forge.Labels["merged"] != "done" || out.Review.ChecksSettleWindow != time.Minute ||
		out.Review.Reviewers["codex"] != "openai" || out.Strategy != "trunk-based" || out.Worker != in.Worker ||
		len(out.Verification) != 1 || out.MaxReviewRounds != 2 || out.Ranking.DeclaredDependents[0] != "web" || !out.CreatedAt.Equal(now) {
		t.Fatalf("binding round trip: %+v", out)
	}
	if sdlcBindingFromDomain(nil) != nil || sdlcDeliveryFromDomain(nil) != nil {
		t.Fatal("nil converts to nil")
	}
	d := sdlcDeliveryFromDomain(&pkgsdlc.Delivery{
		TaskID: "t", Attempt: 1, Branch: "b", Base: "develop", Target: "main", RequiresPullRequest: true, DeleteAfterMerge: true,
		HeadSHA: "h", Version: 2,
		PullRequest: pkgsdlc.DeliveryPullRequest{Number: 4, State: pkgsdlc.PullRequestMerged},
		Reviews:     []pkgsdlc.ReviewRecord{{Reviewer: "codex", Verdict: pkgsdlc.VerdictApprove, HeadSHA: "h", SubmittedAt: now}},
		Checks:      []pkgsdlc.CheckRecord{{Name: "ci", Status: pkgsdlc.CheckFailure}},
		SupersededRuns: []pkgsdlc.SupersessionRecord{{
			Name: "ci", ForgeRunID: "run-h0", HeadSHA: "h0", SupersededBy: "h", Status: pkgsdlc.CheckPending,
			Disposition: pkgsdlc.SupersessionOrphaned, Detail: "no cancel route", ObservedAt: now,
		}},
	})
	if d.PullRequest.State != "merged" || d.Reviews[0].Verdict != "approve" || d.Checks[0].Status != "failure" || d.Version != 2 ||
		d.Base != "develop" || d.Target != "main" || !d.RequiresPullRequest || !d.DeleteAfterMerge {
		t.Fatalf("delivery: %+v", d)
	}
	// The in-process transport carries the displacement too: an embedded caller
	// re-running a run it can see on the forge is the same mistake as a remote
	// one making it.
	if len(d.SupersededRuns) != 1 {
		t.Fatalf("superseded runs = %+v, want the displacement carried", d.SupersededRuns)
	}
	if r := d.SupersededRuns[0]; r.Status != "pending" || r.Disposition != "orphaned" || r.SupersededBy != "h" ||
		r.ForgeRunID != "run-h0" || !r.ObservedAt.Equal(now) {
		t.Fatalf("superseded run: %+v", r)
	}
	if bare := sdlcDeliveryFromDomain(&pkgsdlc.Delivery{TaskID: "t"}); len(bare.SupersededRuns) != 0 {
		t.Fatalf("a delivery with no displacement produced %+v", bare.SupersededRuns)
	}
}

// TestSDLCOnboardingHandlers_RefuseWithoutTheModuleAndOnBadTypes is the
// error half of the two new in-process verbs.
func TestSDLCOnboardingHandlers_RefuseWithoutTheModuleAndOnBadTypes(t *testing.T) {
	ctx := context.Background()

	if _, err := handleSDLCOnboard(ctx, nil, &sdlc.OnboardRequest{ProjectID: "p1"}); err == nil {
		t.Error("Onboard answered without the module")
	}
	if _, err := handleSDLCConformance(ctx, nil, &sdlc.ConformanceRequest{ProjectID: "p1"}); err == nil {
		t.Error("Conformance answered without the module")
	}
	if _, err := handleSDLCOnboard(ctx, nil, &sdlc.ConformanceRequest{}); err == nil {
		t.Error("Onboard accepted the wrong request type")
	}
	if _, err := handleSDLCConformance(ctx, nil, &sdlc.OnboardRequest{}); err == nil {
		t.Error("Conformance accepted the wrong request type")
	}
}

// TestSDLCOnboardingConverters_RoundTrip proves the in-process transport
// carries every field of a report across the domain/SDK boundary.
//
// It matters more than a usual converter test: the report is the ONLY thing
// an operator sees for a repository that is not being driven, and a field
// silently dropped here turns a finding with a remedy into a finding without
// one.
func TestSDLCOnboardingConverters_RoundTrip(t *testing.T) {
	domain := &pkgsdlc.OnboardingReport{
		ProjectID: "p1", TenantID: "t", Repo: "acme/api",
		WebhookURL: "https://factory.test/api/v1/sdlc/webhook/p1", WebhookEnsured: true,
		LabelsCreated: []string{"task:ready"},
		Conformance: &pkgsdlc.ConformanceReport{
			ProjectID: "p1", TenantID: "t", Repo: "acme/api", Conformant: false,
			WebhookURL: "https://factory.test/api/v1/sdlc/webhook/p1",
			Checks: []pkgsdlc.ConformanceCheckResult{
				{Name: pkgsdlc.CheckBinding, Status: pkgsdlc.StatusPass},
				{Name: pkgsdlc.CheckLabels, Status: pkgsdlc.StatusFail, Findings: 1},
			},
			Findings: []pkgsdlc.ConformanceFinding{{
				Class: pkgsdlc.ConformanceLabelMissing, Check: pkgsdlc.CheckLabels,
				Subject: "factory:blocked", Detail: "not defined", Remedy: "onboard the project",
			}},
		},
	}

	out := sdlcOnboardingReportFromDomain(domain)
	if out == nil {
		t.Fatal("the converter dropped the whole report")
	}
	if out.ProjectID != "p1" || out.Repo != "acme/api" || !out.WebhookEnsured {
		t.Fatalf("report = %+v", out)
	}
	if len(out.LabelsCreated) != 1 || out.LabelsCreated[0] != "task:ready" {
		t.Fatalf("LabelsCreated = %v", out.LabelsCreated)
	}
	if out.Conformance == nil {
		t.Fatal("the conformance half was dropped")
	}
	if len(out.Conformance.Checks) != 2 || out.Conformance.Checks[1].Status != string(pkgsdlc.StatusFail) {
		t.Fatalf("Checks = %+v", out.Conformance.Checks)
	}
	if len(out.Conformance.Findings) != 1 {
		t.Fatalf("Findings = %+v", out.Conformance.Findings)
	}
	finding := out.Conformance.Findings[0]
	if finding.Class != string(pkgsdlc.ConformanceLabelMissing) || finding.Subject != "factory:blocked" || finding.Remedy == "" {
		t.Fatalf("finding = %+v; a finding without its remedy is one the reader has to guess at", finding)
	}

	if sdlcOnboardingReportFromDomain(nil) != nil || sdlcConformanceFromDomain(nil) != nil {
		t.Error("a nil report converted to a non-nil one")
	}
}

// TestSDLCBindingDomainConverters_ReviewerSurvivesTheHop covers the embedded
// transport's leg of the same round trip.
//
// THE EMBEDDED TRANSPORT CONVERTS RATHER THAN PASSING THROUGH — the SDK type
// and the domain type are deliberately distinct — so it can lose a field
// exactly as the wire transports can, with no serialization step to blame.
func TestSDLCBindingDomainConverters_ReviewerSurvivesTheHop(t *testing.T) {
	in := &sdlc.ProjectBinding{
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

	domain := sdlcBindingToDomain(in)
	if !domain.Reviewer.Configured() {
		t.Fatal("reviewer dropped converting to the domain type; both dispatch paths gate on Configured()")
	}
	if string(domain.Discovery) != "poll" {
		t.Fatalf("discovery to domain = %q, want poll", domain.Discovery)
	}

	out := sdlcBindingFromDomain(domain)
	if out.Reviewer != in.Reviewer {
		t.Fatalf("reviewer round trip: got %+v, want %+v", out.Reviewer, in.Reviewer)
	}
	if out.Discovery != "poll" {
		t.Fatalf("discovery round trip: got %q, want poll", out.Discovery)
	}
}

// TestSDLCBindingDomainConverters_UnconfiguredReviewerStaysUnconfigured is the
// error-side companion: a project that binds no reviewer must not acquire one.
func TestSDLCBindingDomainConverters_UnconfiguredReviewerStaysUnconfigured(t *testing.T) {
	domain := sdlcBindingToDomain(&sdlc.ProjectBinding{ProjectID: "p2"})
	if domain.Reviewer.Configured() {
		t.Fatalf("empty reviewer became configured: %+v", domain.Reviewer)
	}
	out := sdlcBindingFromDomain(domain)
	if out.Reviewer.Agent != "" || out.Discovery != "" {
		t.Fatalf("zero values did not survive: reviewer=%+v discovery=%q", out.Reviewer, out.Discovery)
	}
}

// TestSDLCOpenWorkHandler_RefusesWithoutTheModuleAndOnBadTypes is the error
// half of the open-work verb.
func TestSDLCOpenWorkHandler_RefusesWithoutTheModuleAndOnBadTypes(t *testing.T) {
	ctx := context.Background()
	if _, err := handleSDLCOpenWork(ctx, nil, &sdlc.OpenWorkRequest{ProjectID: "p1"}); err == nil {
		t.Error("OpenWork answered without the module")
	}
	if _, err := handleSDLCOpenWork(ctx, nil, &sdlc.ConformanceRequest{ProjectID: "p1"}); err == nil {
		t.Error("OpenWork accepted the wrong request type")
	}
}

// TestSDLCOpenWorkConverter_RoundTrip proves every field of a project's open
// work crosses the domain/SDK boundary.
func TestSDLCOpenWorkConverter_RoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	domain := &pkgsdlc.OpenWork{
		ProjectID: "p1", Repo: "acme/api", Agent: "agent-api", ReadAt: now,
		PullRequests: []pkgsdlc.OpenPullRequest{{
			Number: 7, Title: "fix", HeadRef: "issue/7-fix", HeadSHA: "abc", CIState: pkgsdlc.CISuccess,
			ReviewState: pkgsdlc.ReviewApproved, URL: "https://forge.test/pulls/7", Created: now, AgeSeconds: 60,
		}},
		Issues: []pkgsdlc.OpenIssue{{Number: 3, Title: "held", Labels: []string{"task:blocked"}, URL: "u", Created: now, AgeSeconds: 120}},
	}
	out := sdlcOpenWorkFromDomain(domain)
	if out.ProjectID != "p1" || out.Repo != "acme/api" || out.Agent != "agent-api" || !out.ReadAt.Equal(now) {
		t.Fatalf("work = %+v", out)
	}
	pr := out.PullRequests[0]
	if pr.Number != 7 || pr.Title != "fix" || pr.HeadRef != "issue/7-fix" || pr.HeadSHA != "abc" || pr.CIState != "success" ||
		pr.ReviewState != "approved" || pr.URL == "" || !pr.Created.Equal(now) || pr.AgeSeconds != 60 {
		t.Fatalf("pull request = %+v", pr)
	}
	issue := out.Issues[0]
	if issue.Number != 3 || issue.Title != "held" || len(issue.Labels) != 1 || issue.URL != "u" || issue.AgeSeconds != 120 {
		t.Fatalf("issue = %+v", issue)
	}
	if sdlcOpenWorkFromDomain(nil) != nil {
		t.Error("nil open work converted to a non-nil one")
	}
}

func TestSDLCConverters_SlotAppendsSurviveTheDomain(t *testing.T) {
	in := &sdlc.ProjectBinding{
		ProjectID:          "p1",
		WorkerPromptAppend: "worker material",
		TriagePromptAppend: "triage material",
		Reviewer:           sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "tok", PromptAppend: "seat material"},
		Reviewers:          []sdlc.ReviewerConfig{{Name: "qwen", Agent: "qwen-reviewer", PromptAppend: "alternate material"}},
	}
	out := sdlcBindingFromDomain(sdlcBindingToDomain(in))
	if out.WorkerPromptAppend != "worker material" || out.TriagePromptAppend != "triage material" ||
		out.Reviewer.PromptAppend != "seat material" || len(out.Reviewers) != 1 || out.Reviewers[0].PromptAppend != "alternate material" {
		t.Fatalf("round trip lost a slot's append: %+v", out)
	}
	if empty := sdlcBindingFromDomain(sdlcBindingToDomain(&sdlc.ProjectBinding{ProjectID: "p2"})); empty.WorkerPromptAppend != "" {
		t.Fatalf("the converters invented an append: %+v", empty)
	}
}
