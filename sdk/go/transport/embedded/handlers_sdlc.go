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
	"reflect"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// SupportsSDLC declares the SDLC service on the embedded transport.
func (t *Transport) SupportsSDLC() bool { return true }

var _ sdlc.SDLCCapableTransport = (*Transport)(nil)

// sdlcServiceOf resolves the module's binding service or refuses typed.
func sdlcServiceOf(svc service.QuicBaaS, method string) (*service.SDLCService, error) {
	if svc == nil {
		return nil, NewUnknownMethodError("quicbaas.SDLC", method)
	}
	sdlcSvc := svc.SDLC()
	if sdlcSvc == nil {
		return nil, pkgsdlc.ErrUnavailable
	}
	return sdlcSvc, nil
}

func handleSDLCBind(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.BindRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Bind", "*sdlc.BindRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Bind")
	if err != nil {
		return nil, err
	}
	binding := r.Binding
	if binding.ProjectID == "" {
		binding.ProjectID = r.ProjectID
	}
	bound, err := sdlcSvc.Bind(ctx, r.TenantID, sdlcBindingToDomain(&binding))
	if err != nil {
		return nil, err
	}
	return sdlcBindingFromDomain(bound), nil
}

func handleSDLCUnbind(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.UnbindRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Unbind", "*sdlc.UnbindRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Unbind")
	if err != nil {
		return nil, err
	}
	if err := sdlcSvc.Unbind(ctx, r.TenantID, r.ProjectID); err != nil {
		return nil, err
	}
	return &sdlc.UnbindResponse{Unbound: true}, nil
}

func handleSDLCBinding(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.GetBindingRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Binding", "*sdlc.GetBindingRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Binding")
	if err != nil {
		return nil, err
	}
	binding, err := sdlcSvc.Binding(ctx, r.TenantID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	return sdlcBindingFromDomain(binding), nil
}

func handleSDLCDelivery(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.GetDeliveryRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Delivery", "*sdlc.GetDeliveryRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Delivery")
	if err != nil {
		return nil, err
	}
	delivery, err := sdlcSvc.Delivery(ctx, r.TenantID, r.TaskID)
	if err != nil {
		return nil, err
	}
	return sdlcDeliveryFromDomain(delivery), nil
}

// handleSDLCOnboard provisions the project's repository — the explicit
// repair verb, kept apart from the read so a report can never rewrite a
// repository's configuration.
func handleSDLCOnboard(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.OnboardRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Onboard", "*sdlc.OnboardRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Onboard")
	if err != nil {
		return nil, err
	}
	report, err := sdlcSvc.Onboard(ctx, r.TenantID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	return sdlcOnboardingReportFromDomain(report), nil
}

// handleSDLCConformance reads the project's onboarding conformance.
func handleSDLCConformance(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.ConformanceRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Conformance", "*sdlc.ConformanceRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Conformance")
	if err != nil {
		return nil, err
	}
	report, err := sdlcSvc.Conformance(ctx, r.TenantID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	return sdlcConformanceFromDomain(report), nil
}

// handleSDLCForges answers the forge adapters this build carries. Build
// capability, not tenant data, so the request carries nothing and no tenant is
// derived; the module-not-running answer stays ErrUnavailable, distinct from
// an empty list.
func handleSDLCForges(_ context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	if _, err := sdlcServiceOf(svc, "ListForges"); err != nil {
		return nil, err
	}
	// RegisteredForgeTypes returns its types sorted, so two reads serialize
	// identically without a second sort here.
	registered := pkgsdlc.RegisteredForgeTypes()
	forges := make([]string, 0, len(registered))
	for _, forgeType := range registered {
		forges = append(forges, string(forgeType))
	}
	return &sdlc.ListForgesResponse{Forges: forges, Count: len(forges)}, nil
}

// =============================================================================
// Converters — the SDK wire types and the module's records share JSON shape
// but not identity, so the embedded transport converts like every other.
// =============================================================================

func sdlcBindingToDomain(b *sdlc.ProjectBinding) *pkgsdlc.ProjectBinding {
	return &pkgsdlc.ProjectBinding{
		ProjectID: b.ProjectID,
		Forge: pkgsdlc.ForgeConfig{
			Type: pkgsdlc.ForgeType(b.Forge.Type), BaseURL: b.Forge.BaseURL, Owner: b.Forge.Owner, Repo: b.Forge.Repo,
			Labels:       b.Forge.Labels,
			TriggerLabel: b.Forge.TriggerLabel, WebhookSecretRef: b.Forge.WebhookSecretRef, TokenSecretRef: b.Forge.TokenSecretRef,
			SSHHostKey: b.Forge.SSHHostKey,
		},
		Reviewer: pkgsdlc.ReviewerConfig{
			Backend: b.Reviewer.Backend, Model: b.Reviewer.Model, Image: b.Reviewer.Image,
			Login: b.Reviewer.Login, TokenSecretRef: b.Reviewer.TokenSecretRef,
			StatusContext: b.Reviewer.StatusContext,
		},
		Discovery:    pkgsdlc.DiscoveryMode(b.Discovery),
		Strategy:     b.Strategy,
		Worker:       pkgsdlc.WorkerConfig{Backend: b.Worker.Backend, Model: b.Worker.Model, Image: b.Worker.Image},
		Verification: append([]string(nil), b.Verification...),
		Review: pkgsdlc.ReviewPolicy{
			RequiredApprovals: b.Review.RequiredApprovals, RequireIndependentReviewer: b.Review.RequireIndependentReviewer,
			RequireGreenChecks: b.Review.RequireGreenChecks, ChecksSettleWindow: b.Review.ChecksSettleWindow, Reviewers: b.Review.Reviewers,
		},
		MaxReviewRounds: b.MaxReviewRounds,
		Ranking: pkgsdlc.RankingPolicy{
			DefaultPriority: b.Ranking.DefaultPriority, AnalyzerName: b.Ranking.AnalyzerName, DeclaredDependents: b.Ranking.DeclaredDependents,
		},
		Onboarding: pkgsdlc.OnboardingConfig{
			WebhookEvents:          append([]string(nil), b.Onboarding.WebhookEvents...),
			ProtectedBranch:        b.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), b.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), b.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      b.Onboarding.ForbiddenPathsRef,
			LabelColors:            b.Onboarding.LabelColors,
		},
	}
}

func sdlcBindingFromDomain(b *pkgsdlc.ProjectBinding) *sdlc.ProjectBinding {
	if b == nil {
		return nil
	}
	return &sdlc.ProjectBinding{
		ProjectID: b.ProjectID,
		Forge: sdlc.ForgeConfig{
			Type: string(b.Forge.Type), BaseURL: b.Forge.BaseURL, Owner: b.Forge.Owner, Repo: b.Forge.Repo,
			Labels:       b.Forge.Labels,
			TriggerLabel: b.Forge.TriggerLabel, WebhookSecretRef: b.Forge.WebhookSecretRef, TokenSecretRef: b.Forge.TokenSecretRef,
			SSHHostKey: b.Forge.SSHHostKey,
		},
		Reviewer: sdlc.ReviewerConfig{
			Backend: b.Reviewer.Backend, Model: b.Reviewer.Model, Image: b.Reviewer.Image,
			Login: b.Reviewer.Login, TokenSecretRef: b.Reviewer.TokenSecretRef,
			StatusContext: b.Reviewer.StatusContext,
		},
		Discovery:    string(b.Discovery),
		Strategy:     b.Strategy,
		Worker:       sdlc.WorkerConfig{Backend: b.Worker.Backend, Model: b.Worker.Model, Image: b.Worker.Image},
		Verification: append([]string(nil), b.Verification...),
		Review: sdlc.ReviewPolicy{
			RequiredApprovals: b.Review.RequiredApprovals, RequireIndependentReviewer: b.Review.RequireIndependentReviewer,
			RequireGreenChecks: b.Review.RequireGreenChecks, ChecksSettleWindow: b.Review.ChecksSettleWindow, Reviewers: b.Review.Reviewers,
		},
		MaxReviewRounds: b.MaxReviewRounds,
		Ranking: sdlc.RankingPolicy{
			DefaultPriority: b.Ranking.DefaultPriority, AnalyzerName: b.Ranking.AnalyzerName, DeclaredDependents: b.Ranking.DeclaredDependents,
		},
		Onboarding: sdlc.OnboardingConfig{
			WebhookEvents:          append([]string(nil), b.Onboarding.WebhookEvents...),
			ProtectedBranch:        b.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), b.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), b.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      b.Onboarding.ForbiddenPathsRef,
			LabelColors:            b.Onboarding.LabelColors,
		},
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

func sdlcOnboardingReportFromDomain(r *pkgsdlc.OnboardingReport) *sdlc.OnboardingReport {
	if r == nil {
		return nil
	}
	return &sdlc.OnboardingReport{
		ProjectID: r.ProjectID, TenantID: r.TenantID, Repo: r.Repo,
		WebhookURL: r.WebhookURL, WebhookEnsured: r.WebhookEnsured,
		LabelsCreated: append([]string(nil), r.LabelsCreated...),
		Conformance:   sdlcConformanceFromDomain(r.Conformance),
	}
}

func sdlcConformanceFromDomain(r *pkgsdlc.ConformanceReport) *sdlc.ConformanceReport {
	if r == nil {
		return nil
	}
	out := &sdlc.ConformanceReport{
		ProjectID: r.ProjectID, TenantID: r.TenantID, Repo: r.Repo,
		Conformant: r.Conformant, WebhookURL: r.WebhookURL, CheckedAt: r.CheckedAt,
		Checks: make([]sdlc.ConformanceCheckResult, 0, len(r.Checks)),
	}
	for _, check := range r.Checks {
		out.Checks = append(out.Checks, sdlc.ConformanceCheckResult{
			Name: check.Name, Status: string(check.Status), Findings: check.Findings,
		})
	}
	for _, finding := range r.Findings {
		out.Findings = append(out.Findings, sdlc.ConformanceFinding{
			Class: string(finding.Class), Check: finding.Check, Subject: finding.Subject,
			Detail: finding.Detail, Remedy: finding.Remedy,
		})
	}
	return out
}

func sdlcDeliveryFromDomain(d *pkgsdlc.Delivery) *sdlc.Delivery {
	if d == nil {
		return nil
	}
	out := &sdlc.Delivery{
		TaskID: d.TaskID, ProjectID: d.ProjectID, Attempt: d.Attempt, Branch: d.Branch, Base: d.Base, HeadSHA: d.HeadSHA,
		Target: d.Target, RequiresPullRequest: d.RequiresPullRequest, DeleteAfterMerge: d.DeleteAfterMerge,
		WorkDir: d.WorkDir, Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
		PullRequest: sdlc.PullRequest{
			Number: d.PullRequest.Number, URL: d.PullRequest.URL, State: string(d.PullRequest.State),
			Title: d.PullRequest.Title, Base: d.PullRequest.Base,
		},
	}
	for _, r := range d.Reviews {
		out.Reviews = append(out.Reviews, sdlc.ReviewRecord{
			Reviewer: r.Reviewer, ModelFamily: r.ModelFamily, Verdict: string(r.Verdict), HeadSHA: r.HeadSHA,
			ExternalID: r.ExternalID, Round: r.Round, Findings: r.Findings, SubmittedAt: r.SubmittedAt,
		})
	}
	for _, c := range d.Checks {
		out.Checks = append(out.Checks, sdlc.CheckRecord{
			Name: c.Name, HeadSHA: c.HeadSHA, Status: string(c.Status), ForgeRunID: c.ForgeRunID, FinishedAt: c.FinishedAt,
		})
	}
	for _, r := range d.SupersededRuns {
		out.SupersededRuns = append(out.SupersededRuns, sdlc.SupersessionRecord{
			Name: r.Name, ForgeRunID: r.ForgeRunID, HeadSHA: r.HeadSHA, SupersededBy: r.SupersededBy,
			Status: string(r.Status), Disposition: string(r.Disposition), Detail: r.Detail, ObservedAt: r.ObservedAt,
		})
	}
	return out
}
