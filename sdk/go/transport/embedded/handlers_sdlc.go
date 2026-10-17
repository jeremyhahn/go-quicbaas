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
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Bind", "*sdlc.BindRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Bind")
	if err != nil {
		return nil, err
	}
	binding := r.Binding
	if binding.ProjectID == "" {
		binding.ProjectID = r.ProjectID
	}
	bound, err := sdlcSvc.Bind(ctx, r.TenantID, sdlcBindingToDomain(&binding), service.BindOptions{LeavePaused: r.Paused})
	if err != nil {
		return nil, err
	}
	return sdlcBindingFromDomain(bound), nil
}

func handleSDLCUnbind(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.UnbindRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Unbind", "*sdlc.UnbindRequest", typeNameOf(req))
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
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Binding", "*sdlc.GetBindingRequest", typeNameOf(req))
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
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Delivery", "*sdlc.GetDeliveryRequest", typeNameOf(req))
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
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Onboard", "*sdlc.OnboardRequest", typeNameOf(req))
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

func handleSDLCOnboardRepo(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.RepoOnboardRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "OnboardRepo", "*sdlc.RepoOnboardRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "OnboardRepo")
	if err != nil {
		return nil, err
	}
	report, err := sdlcSvc.OnboardRepo(ctx, r.TenantID, pkgsdlc.RepoOnboardRequest{
		Repo: r.Repo, Like: r.Like, MaxConcurrency: r.MaxConcurrency, Paused: r.Paused,
		Preflight: r.Preflight,
	})
	if err != nil {
		return nil, err
	}
	return sdlcRepoOnboardingReportFromDomain(report), nil
}

// handleSDLCConformance reads the project's onboarding conformance.
func handleSDLCConformance(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.ConformanceRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Conformance", "*sdlc.ConformanceRequest", typeNameOf(req))
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

// handleSDLCOpenWork reads a bound project's open work from its forge.
func handleSDLCOpenWork(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.OpenWorkRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "OpenWork", "*sdlc.OpenWorkRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "OpenWork")
	if err != nil {
		return nil, err
	}
	work, err := sdlcSvc.OpenWork(ctx, r.TenantID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	return sdlcOpenWorkFromDomain(work), nil
}

// handleSDLCReviewer reads which reviewer profile reviews one pull request.
func handleSDLCReviewer(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Reviewer", "*sdlc.ReviewerRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Reviewer")
	if err != nil {
		return nil, err
	}
	selection, err := sdlcSvc.ReviewerSelection(ctx, r.TenantID, r.ProjectID, r.PullRequest)
	if err != nil {
		return nil, err
	}
	return sdlcReviewerSelectionFromDomain(selection), nil
}

// handleSDLCSetReviewer records an operator's reviewer choice for one pull
// request.
func handleSDLCSetReviewer(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.SetReviewerRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "SetReviewer", "*sdlc.SetReviewerRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "SetReviewer")
	if err != nil {
		return nil, err
	}
	selection, err := sdlcSvc.SetReviewerOverride(ctx, r.TenantID, r.ProjectID, r.PullRequest, r.Profile)
	if err != nil {
		return nil, err
	}
	return sdlcReviewerSelectionFromDomain(selection), nil
}

// handleSDLCClearReviewer removes one pull request's reviewer override.
func handleSDLCClearReviewer(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "ClearReviewer", "*sdlc.ReviewerRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "ClearReviewer")
	if err != nil {
		return nil, err
	}
	selection, err := sdlcSvc.ClearReviewerOverride(ctx, r.TenantID, r.ProjectID, r.PullRequest)
	if err != nil {
		return nil, err
	}
	return sdlcReviewerSelectionFromDomain(selection), nil
}

// handleSDLCFileIssue files an issue on a bound project's repository: an
// audited operator verb.
func handleSDLCFileIssue(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.FileIssueRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "FileIssue", "*sdlc.FileIssueRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "FileIssue")
	if err != nil {
		return nil, err
	}
	issue, err := sdlcSvc.FileIssue(ctx, r.TenantID, r.ProjectID, pkgsdlc.IssueFiling{
		Title: r.Title, Body: r.Body, Labels: append([]string(nil), r.Labels...), Parent: r.Parent,
	})
	if err != nil {
		return nil, err
	}
	return &sdlc.ForgeIssue{
		Number: issue.Number, Title: issue.Title, Body: issue.Body, State: issue.State,
		Labels: append([]string(nil), issue.Labels...), Author: issue.Author, URL: issue.URL,
	}, nil
}

// handleSDLCCollaborators lists the accounts that may rule on a bound
// project's repository.
func handleSDLCCollaborators(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.CollaboratorsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "Collaborators", "*sdlc.CollaboratorsRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "Collaborators")
	if err != nil {
		return nil, err
	}
	listed, err := sdlcSvc.Collaborators(ctx, r.TenantID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	users := make([]sdlc.ForgeUser, 0, len(listed))
	for _, user := range listed {
		users = append(users, sdlc.ForgeUser{Login: user.Login, FullName: user.FullName})
	}
	return &sdlc.CollaboratorsResponse{Users: users, Count: len(users)}, nil
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
		Reviewer:           sdlcReviewerToDomain(b.Reviewer),
		Reviewers:          sdlcReviewersToDomain(b.Reviewers),
		Discovery:          pkgsdlc.DiscoveryMode(b.Discovery),
		TriageMayClose:     b.TriageMayClose,
		WorkerPromptAppend: b.WorkerPromptAppend,
		TriagePromptAppend: b.TriagePromptAppend,
		Rulers:             append([]string(nil), b.Rulers...),
		MergeTrain:         pkgsdlc.MergeTrainPolicy{Disabled: b.MergeTrain.Disabled, MaxSize: b.MergeTrain.MaxSize},
		Clustering:         pkgsdlc.ClusteringPolicy{Disabled: b.Clustering.Disabled, MaxSize: b.Clustering.MaxSize},
		Triage:             agentRefToDomain(b.Triage),
		Strategy:           b.Strategy,
		Worker:             agentRefToDomain(b.Worker),
		Verification:       append([]string(nil), b.Verification...),
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
		Reviewer:           sdlcReviewerFromDomain(b.Reviewer),
		Reviewers:          sdlcReviewersFromDomain(b.Reviewers),
		Discovery:          string(b.Discovery),
		TriageMayClose:     b.TriageMayClose,
		WorkerPromptAppend: b.WorkerPromptAppend,
		TriagePromptAppend: b.TriagePromptAppend,
		Rulers:             append([]string(nil), b.Rulers...),
		MergeTrain:         sdlc.MergeTrainPolicy{Disabled: b.MergeTrain.Disabled, MaxSize: b.MergeTrain.MaxSize},
		Clustering:         sdlc.ClusteringPolicy{Disabled: b.Clustering.Disabled, MaxSize: b.Clustering.MaxSize},
		Triage:             agentRefFromDomain(b.Triage),
		Strategy:           b.Strategy,
		Worker:             agentRefFromDomain(b.Worker),
		Verification:       append([]string(nil), b.Verification...),
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

// sdlcReviewerToDomain maps one reviewer profile, every field of it: a bind
// replaces the record, so a field dropped here is erased by every rebind.
func sdlcReviewerToDomain(r sdlc.ReviewerConfig) pkgsdlc.ReviewerConfig {
	return pkgsdlc.ReviewerConfig{
		Name: r.Name, CountsTowardMerge: copyBoolPtr(r.CountsTowardMerge), Agent: r.Agent,
		Login: r.Login, TokenSecretRef: r.TokenSecretRef, StatusContext: r.StatusContext,
		Endpoint: r.Endpoint, HostAffinity: r.HostAffinity, PromptAppend: r.PromptAppend,
	}
}

func sdlcReviewerFromDomain(r pkgsdlc.ReviewerConfig) sdlc.ReviewerConfig {
	return sdlc.ReviewerConfig{
		Name: r.Name, CountsTowardMerge: copyBoolPtr(r.CountsTowardMerge), Agent: r.Agent,
		Login: r.Login, TokenSecretRef: r.TokenSecretRef, StatusContext: r.StatusContext,
		Endpoint: r.Endpoint, HostAffinity: r.HostAffinity, PromptAppend: r.PromptAppend,
	}
}

func sdlcReviewersToDomain(rs []sdlc.ReviewerConfig) []pkgsdlc.ReviewerConfig {
	if len(rs) == 0 {
		return nil
	}
	out := make([]pkgsdlc.ReviewerConfig, 0, len(rs))
	for _, r := range rs {
		out = append(out, sdlcReviewerToDomain(r))
	}
	return out
}

func sdlcReviewersFromDomain(rs []pkgsdlc.ReviewerConfig) []sdlc.ReviewerConfig {
	if len(rs) == 0 {
		return nil
	}
	out := make([]sdlc.ReviewerConfig, 0, len(rs))
	for _, r := range rs {
		out = append(out, sdlcReviewerFromDomain(r))
	}
	return out
}

// copyBoolPtr copies a presence-carrying flag, so the two records never share
// one pointer.
func copyBoolPtr(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

func sdlcReviewerSelectionFromDomain(s *pkgsdlc.ReviewerSelection) *sdlc.ReviewerSelection {
	if s == nil {
		return nil
	}
	return &sdlc.ReviewerSelection{
		ProjectID: s.ProjectID, PullRequest: s.PullRequest, Profile: s.Profile,
		Overridden: s.Overridden, CountsTowardMerge: s.CountsTowardMerge,
		StatusContext: s.StatusContext, UpdatedAt: s.UpdatedAt,
		Redispatched: s.Redispatched,
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

func sdlcRepoOnboardingReportFromDomain(r *pkgsdlc.RepoOnboardingReport) *sdlc.RepoOnboardingReport {
	steps := make([]sdlc.RepoOnboardStep, 0, len(r.Steps))
	for _, step := range r.Steps {
		steps = append(steps, sdlc.RepoOnboardStep{
			Step: step.Step, Outcome: string(step.Outcome), Detail: step.Detail, Remedy: step.Remedy,
		})
	}
	return &sdlc.RepoOnboardingReport{
		TenantID: r.TenantID, ProjectID: r.ProjectID, Repo: r.Repo, Steps: steps,
		Conformance: sdlcConformanceFromDomain(r.Conformance),
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

func sdlcOpenWorkFromDomain(w *pkgsdlc.OpenWork) *sdlc.OpenWork {
	if w == nil {
		return nil
	}
	out := &sdlc.OpenWork{
		ProjectID: w.ProjectID, Repo: w.Repo, Agent: w.Agent, ReadAt: w.ReadAt,
		PullRequests: make([]sdlc.OpenPullRequest, 0, len(w.PullRequests)),
		Issues:       make([]sdlc.OpenIssue, 0, len(w.Issues)),
	}
	for _, pr := range w.PullRequests {
		out.PullRequests = append(out.PullRequests, sdlc.OpenPullRequest{
			Number: pr.Number, Title: pr.Title, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA,
			CIState: string(pr.CIState), ReviewState: string(pr.ReviewState), URL: pr.URL,
			Created: pr.Created, AgeSeconds: pr.AgeSeconds,
		})
	}
	for _, issue := range w.Issues {
		out.Issues = append(out.Issues, sdlc.OpenIssue{
			Number: issue.Number, Title: issue.Title, Labels: append([]string(nil), issue.Labels...),
			URL: issue.URL, Created: issue.Created, AgeSeconds: issue.AgeSeconds,
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

// handleSDLCAdoptIssue hands one forge issue back to the factory.
func handleSDLCAdoptIssue(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*sdlc.AdoptIssueRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.SDLC", "AdoptIssue", "*sdlc.AdoptIssueRequest", typeNameOf(req))
	}
	sdlcSvc, err := sdlcServiceOf(svc, "AdoptIssue")
	if err != nil {
		return nil, err
	}
	adoption, err := sdlcSvc.AdoptIssue(ctx, r.TenantID, pkgsdlc.AdoptIssueRequest{
		ProjectID: r.ProjectID, Issue: r.Issue, PullRequest: r.PullRequest, Ruling: r.Ruling,
	})
	if err != nil {
		return nil, err
	}
	return &sdlc.Adoption{
		TaskID: adoption.TaskID, Issue: adoption.Issue, PullRequest: adoption.PullRequest,
		Branch: adoption.Branch, Superseded: adoption.Superseded,
	}, nil
}
