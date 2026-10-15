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
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// sdlcServicePrefix is the service name the SDLC client routes under.
const sdlcServicePrefix = "quicbaas.SDLC"

// SupportsSDLC declares the SDLC service on this transport, and so on the
// Unix transport that delegates to it.
func (t *Transport) SupportsSDLC() bool { return true }

var _ sdlc.SDLCCapableTransport = (*Transport)(nil)

// isSDLCService reports whether the service name is the SDLC service.
func isSDLCService(svc string) bool {
	return strings.HasPrefix(svc, sdlcServicePrefix)
}

type sdlcHandlerFunc func(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error)

// sdlcMethodMap dispatches SDLC methods.
var sdlcMethodMap = map[string]sdlcHandlerFunc{
	"Bind":        handleSDLCBind,
	"Unbind":      handleSDLCUnbind,
	"Binding":     handleSDLCBinding,
	"Delivery":    handleSDLCDelivery,
	"ListForges":  handleSDLCForges,
	"Onboard":     handleSDLCOnboard,
	"OnboardRepo": handleSDLCOnboardRepo,
	"Conformance": handleSDLCConformance,
	"OpenWork":    handleSDLCOpenWork,

	"Reviewer":           handleSDLCReviewer,
	"SetReviewer":        handleSDLCSetReviewer,
	"AdoptIssue":         handleSDLCAdoptIssue,
	"ClearReviewer":      handleSDLCClearReviewer,
	"WorkerImageStatus":  handleSDLCWorkerImageStatus,
	"RefreshWorkerImage": handleSDLCRefreshWorkerImage,
	"FileIssue":          handleSDLCFileIssue,
	"Collaborators":      handleSDLCCollaborators,
}

type sdlcHandler struct {
	conn *grpc.ClientConn
}

func newSDLCHandler(conn *grpc.ClientConn) *sdlcHandler {
	return &sdlcHandler{conn: conn}
}

func (h *sdlcHandler) handleRequest(ctx context.Context, _, method string, req interface{}) (interface{}, error) {
	handler, ok := sdlcMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.sdlc."+method, "unknown sdlc method")
	}
	return handler(ctx, h.conn, req)
}

func handleSDLCBind(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Bind"
	sdkReq, ok := req.(*sdlc.BindRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	binding := sdkReq.Binding
	if binding.ProjectID == "" {
		binding.ProjectID = sdkReq.ProjectID
	}
	resp, err := pb.NewSDLCServiceClient(conn).BindProject(ctx, &pb.BindProjectRequest{
		TenantId: sdkReq.TenantID, Binding: sdlcBindingToProto(&binding), Paused: sdkReq.Paused,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcBindingFromProto(resp.Binding), nil
}

func handleSDLCUnbind(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Unbind"
	sdkReq, ok := req.(*sdlc.UnbindRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).UnbindProject(ctx, &pb.UnbindProjectRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sdlc.UnbindResponse{Unbound: resp.Unbound}, nil
}

func handleSDLCBinding(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Binding"
	sdkReq, ok := req.(*sdlc.GetBindingRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).GetBinding(ctx, &pb.GetBindingRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcBindingFromProto(resp.Binding), nil
}

func handleSDLCDelivery(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Delivery"
	sdkReq, ok := req.(*sdlc.GetDeliveryRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).GetDelivery(ctx, &pb.GetDeliveryRequest{
		TenantId: sdkReq.TenantID, TaskId: sdkReq.TaskID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcDeliveryFromProto(resp.Delivery), nil
}

// handleSDLCOnboard invokes Onboard: the explicit repair verb, kept apart
// from the read so a report can never rewrite a repository's configuration.
func handleSDLCOnboard(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Onboard"
	sdkReq, ok := req.(*sdlc.OnboardRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).Onboard(ctx, &pb.OnboardRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcOnboardingReportFromProto(resp.GetReport()), nil
}

func handleSDLCOnboardRepo(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.OnboardRepo"
	sdkReq, ok := req.(*sdlc.RepoOnboardRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).OnboardRepo(ctx, &pb.OnboardRepoRequest{
		TenantId: sdkReq.TenantID, Repo: sdkReq.Repo, Like: sdkReq.Like,
		MaxConcurrency: int32(sdkReq.MaxConcurrency), Paused: sdkReq.Paused,
		Preflight: sdkReq.Preflight,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcRepoOnboardingReportFromProto(resp.GetReport()), nil
}

// handleSDLCConformance invokes GetConformance: read-only.
func handleSDLCConformance(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Conformance"
	sdkReq, ok := req.(*sdlc.ConformanceRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).GetConformance(ctx, &pb.GetConformanceRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcConformanceFromProto(resp.GetReport()), nil
}

// handleSDLCOpenWork invokes GetOpenWork: read-only.
func handleSDLCOpenWork(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.OpenWork"
	sdkReq, ok := req.(*sdlc.OpenWorkRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).GetOpenWork(ctx, &pb.GetOpenWorkRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcOpenWorkFromProto(resp.GetWork()), nil
}

// handleSDLCReviewer invokes GetReviewerSelection: read-only.
func handleSDLCReviewer(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Reviewer"
	sdkReq, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).GetReviewerSelection(ctx, &pb.GetReviewerSelectionRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, PullRequest: int32(sdkReq.PullRequest),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcReviewerSelectionFromProto(resp.GetSelection()), nil
}

// handleSDLCSetReviewer invokes SetReviewerOverride: an operator verb.
func handleSDLCSetReviewer(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.SetReviewer"
	sdkReq, ok := req.(*sdlc.SetReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).SetReviewerOverride(ctx, &pb.SetReviewerOverrideRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, PullRequest: int32(sdkReq.PullRequest),
		Profile: sdkReq.Profile,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcReviewerSelectionFromProto(resp.GetSelection()), nil
}

// handleSDLCClearReviewer invokes ClearReviewerOverride: an operator verb.
func handleSDLCClearReviewer(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.ClearReviewer"
	sdkReq, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).ClearReviewerOverride(ctx, &pb.ClearReviewerOverrideRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, PullRequest: int32(sdkReq.PullRequest),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcReviewerSelectionFromProto(resp.GetSelection()), nil
}

// handleSDLCFileIssue invokes FileIssue: an audited operator verb.
func handleSDLCFileIssue(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.FileIssue"
	sdkReq, ok := req.(*sdlc.FileIssueRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).FileIssue(ctx, &pb.FileIssueRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, Title: sdkReq.Title, Body: sdkReq.Body,
		Labels: append([]string(nil), sdkReq.Labels...), Parent: int32(sdkReq.Parent),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	issue := resp.GetIssue()
	if issue == nil {
		return nil, errors.NewInternal(op, "the server answered no issue")
	}
	return &sdlc.ForgeIssue{
		Number: int(issue.GetNumber()), Title: issue.GetTitle(), Body: issue.GetBody(), State: issue.GetState(),
		Labels: append([]string(nil), issue.GetLabels()...), Author: issue.GetAuthor(), URL: issue.GetUrl(),
	}, nil
}

// handleSDLCCollaborators invokes ListCollaborators: read-only.
func handleSDLCCollaborators(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.Collaborators"
	sdkReq, ok := req.(*sdlc.CollaboratorsRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).ListCollaborators(ctx, &pb.ListCollaboratorsRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	users := make([]sdlc.ForgeUser, 0, len(resp.GetUsers()))
	for _, user := range resp.GetUsers() {
		users = append(users, sdlc.ForgeUser{Login: user.GetLogin(), FullName: user.GetFullName()})
	}
	return &sdlc.CollaboratorsResponse{Users: users, Count: len(users)}, nil
}

// handleSDLCForges invokes ListForges: the forge adapters this build carries.
// The request carries no tenant because the adapter set is the build's.
func handleSDLCForges(ctx context.Context, conn *grpc.ClientConn, _ interface{}) (interface{}, error) {
	const op = "grpc.sdlc.ListForges"
	resp, err := pb.NewSDLCServiceClient(conn).ListForges(ctx, &pb.ListForgesRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	forges := append([]string(nil), resp.GetForges()...)
	return &sdlc.ListForgesResponse{Forges: forges, Count: len(forges)}, nil
}

// =============================================================================
// Converters
// =============================================================================

func sdlcBindingToProto(b *sdlc.ProjectBinding) *pb.ProjectBinding {
	return &pb.ProjectBinding{
		ProjectId: b.ProjectID,
		Forge: &pb.ForgeConfig{
			Type: b.Forge.Type, BaseUrl: b.Forge.BaseURL, Owner: b.Forge.Owner, Repo: b.Forge.Repo,
			Labels: b.Forge.Labels, TriggerLabel: b.Forge.TriggerLabel,
			WebhookSecretRef: b.Forge.WebhookSecretRef, TokenSecretRef: b.Forge.TokenSecretRef,
			SshHostKey: b.Forge.SSHHostKey,
		},
		Review: &pb.ReviewPolicy{
			RequiredApprovals: int32(b.Review.RequiredApprovals), RequireIndependentReviewer: b.Review.RequireIndependentReviewer,
			RequireGreenChecks: b.Review.RequireGreenChecks, ChecksSettleWindowNs: int64(b.Review.ChecksSettleWindow),
			Reviewers: b.Review.Reviewers,
		},
		Reviewer:           sdlcReviewerToProto(b.Reviewer),
		Reviewers:          sdlcReviewersToProto(b.Reviewers),
		Discovery:          b.Discovery,
		TriageMayClose:     b.TriageMayClose,
		WorkerPromptAppend: b.WorkerPromptAppend,
		TriagePromptAppend: b.TriagePromptAppend,
		Rulers:             append([]string(nil), b.Rulers...),
		MergeTrain:         &pb.MergeTrainPolicy{Disabled: b.MergeTrain.Disabled, MaxSize: int32(b.MergeTrain.MaxSize)},
		Clustering:         &pb.ClusteringPolicy{Disabled: b.Clustering.Disabled, MaxSize: int32(b.Clustering.MaxSize)},
		Triage:             agentRefToProto(b.Triage),
		Strategy:           b.Strategy,
		Worker:             agentRefToProto(b.Worker),
		Verification:       append([]string(nil), b.Verification...),
		MaxReviewRounds:    int32(b.MaxReviewRounds),
		Ranking: &pb.RankingPolicy{
			DefaultPriority: int32(b.Ranking.DefaultPriority), AnalyzerName: b.Ranking.AnalyzerName,
			DeclaredDependents: b.Ranking.DeclaredDependents,
		},
		Onboarding: &pb.OnboardingConfig{
			WebhookEvents:          append([]string(nil), b.Onboarding.WebhookEvents...),
			ProtectedBranch:        b.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), b.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), b.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      b.Onboarding.ForbiddenPathsRef,
			LabelColors:            b.Onboarding.LabelColors,
		},
	}
}

func sdlcBindingFromProto(p *pb.ProjectBinding) *sdlc.ProjectBinding {
	if p == nil {
		return nil
	}
	b := &sdlc.ProjectBinding{
		ProjectID: p.ProjectId, CreatedAt: timeFromUnix(p.CreatedAtUnix), UpdatedAt: timeFromUnix(p.UpdatedAtUnix),
		Strategy: p.Strategy, Verification: append([]string(nil), p.Verification...), MaxReviewRounds: int(p.MaxReviewRounds),
	}
	b.Worker = agentRefFromProto(p.GetWorker())
	b.Triage = agentRefFromProto(p.GetTriage())
	if p.Forge != nil {
		b.Forge = sdlc.ForgeConfig{
			Type: p.Forge.Type, BaseURL: p.Forge.BaseUrl, Owner: p.Forge.Owner, Repo: p.Forge.Repo,
			Labels: p.Forge.Labels, TriggerLabel: p.Forge.TriggerLabel,
			WebhookSecretRef: p.Forge.WebhookSecretRef, TokenSecretRef: p.Forge.TokenSecretRef,
			SSHHostKey: p.Forge.SshHostKey,
		}
	}
	if p.Review != nil {
		b.Review = sdlc.ReviewPolicy{
			RequiredApprovals: int(p.Review.RequiredApprovals), RequireIndependentReviewer: p.Review.RequireIndependentReviewer,
			RequireGreenChecks: p.Review.RequireGreenChecks, ChecksSettleWindow: time.Duration(p.Review.ChecksSettleWindowNs),
			Reviewers: p.Review.Reviewers,
		}
	}
	b.Reviewer = sdlcReviewerFromProto(p.Reviewer)
	b.Reviewers = sdlcReviewersFromProto(p.Reviewers)
	b.Discovery = p.Discovery
	b.TriageMayClose = p.TriageMayClose
	b.WorkerPromptAppend = p.WorkerPromptAppend
	b.TriagePromptAppend = p.TriagePromptAppend
	b.Rulers = append([]string(nil), p.Rulers...)
	if p.MergeTrain != nil {
		b.MergeTrain = sdlc.MergeTrainPolicy{Disabled: p.MergeTrain.Disabled, MaxSize: int(p.MergeTrain.MaxSize)}
	}
	if p.Clustering != nil {
		b.Clustering = sdlc.ClusteringPolicy{Disabled: p.Clustering.Disabled, MaxSize: int(p.Clustering.MaxSize)}
	}
	if p.Ranking != nil {
		b.Ranking = sdlc.RankingPolicy{
			DefaultPriority: int(p.Ranking.DefaultPriority), AnalyzerName: p.Ranking.AnalyzerName, DeclaredDependents: p.Ranking.DeclaredDependents,
		}
	}
	if p.Onboarding != nil {
		b.Onboarding = sdlc.OnboardingConfig{
			WebhookEvents:          append([]string(nil), p.Onboarding.WebhookEvents...),
			ProtectedBranch:        p.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), p.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), p.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      p.Onboarding.ForbiddenPathsRef,
			LabelColors:            p.Onboarding.LabelColors,
		}
	}
	return b
}

// sdlcReviewerToProto maps one reviewer profile onto the wire, keeping the
// PRESENCE of counts_toward_merge: unset means the role's default, and an
// explicit false must not be confused with it.
func sdlcReviewerToProto(r sdlc.ReviewerConfig) *pb.ReviewerConfig {
	p := &pb.ReviewerConfig{
		Name: r.Name, AgentId: r.Agent,
		Login: r.Login, TokenSecretRef: r.TokenSecretRef, StatusContext: r.StatusContext,
		Endpoint: r.Endpoint, HostAffinity: r.HostAffinity, PromptAppend: r.PromptAppend,
	}
	if r.CountsTowardMerge != nil {
		counts := *r.CountsTowardMerge
		p.CountsTowardMerge = &counts
	}
	return p
}

// sdlcReviewerFromProto maps one reviewer profile off the wire.
func sdlcReviewerFromProto(p *pb.ReviewerConfig) sdlc.ReviewerConfig {
	if p == nil {
		return sdlc.ReviewerConfig{}
	}
	r := sdlc.ReviewerConfig{
		Name: p.Name, Agent: p.AgentId,
		Login: p.Login, TokenSecretRef: p.TokenSecretRef, StatusContext: p.StatusContext,
		Endpoint: p.Endpoint, HostAffinity: p.HostAffinity, PromptAppend: p.PromptAppend,
	}
	if p.CountsTowardMerge != nil {
		counts := p.GetCountsTowardMerge()
		r.CountsTowardMerge = &counts
	}
	return r
}

func sdlcReviewersToProto(rs []sdlc.ReviewerConfig) []*pb.ReviewerConfig {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*pb.ReviewerConfig, 0, len(rs))
	for _, r := range rs {
		out = append(out, sdlcReviewerToProto(r))
	}
	return out
}

func sdlcReviewersFromProto(ps []*pb.ReviewerConfig) []sdlc.ReviewerConfig {
	if len(ps) == 0 {
		return nil
	}
	out := make([]sdlc.ReviewerConfig, 0, len(ps))
	for _, p := range ps {
		out = append(out, sdlcReviewerFromProto(p))
	}
	return out
}

func sdlcReviewerSelectionFromProto(p *pb.ReviewerSelection) *sdlc.ReviewerSelection {
	if p == nil {
		return nil
	}
	return &sdlc.ReviewerSelection{
		ProjectID: p.ProjectId, PullRequest: int(p.PullRequest), Profile: p.Profile,
		Overridden: p.Overridden, CountsTowardMerge: p.CountsTowardMerge,
		StatusContext: p.StatusContext, UpdatedAt: timeFromUnix(p.UpdatedAtUnix),
		Redispatched: p.Redispatched,
	}
}

func sdlcOnboardingReportFromProto(p *pb.OnboardingReport) *sdlc.OnboardingReport {
	if p == nil {
		return nil
	}
	return &sdlc.OnboardingReport{
		ProjectID: p.ProjectId, TenantID: p.TenantId, Repo: p.Repo,
		WebhookURL: p.WebhookUrl, WebhookEnsured: p.WebhookEnsured,
		LabelsCreated: append([]string(nil), p.LabelsCreated...),
		Conformance:   sdlcConformanceFromProto(p.Conformance),
	}
}

func sdlcConformanceFromProto(p *pb.ConformanceReport) *sdlc.ConformanceReport {
	if p == nil {
		return nil
	}
	out := &sdlc.ConformanceReport{
		ProjectID: p.ProjectId, TenantID: p.TenantId, Repo: p.Repo,
		Conformant: p.Conformant, WebhookURL: p.WebhookUrl,
		CheckedAt: timeFromUnix(p.CheckedAtUnix),
		Checks:    make([]sdlc.ConformanceCheckResult, 0, len(p.Checks)),
	}
	for _, check := range p.Checks {
		out.Checks = append(out.Checks, sdlc.ConformanceCheckResult{
			Name: check.Name, Status: check.Status, Findings: int(check.Findings),
		})
	}
	for _, finding := range p.Findings {
		out.Findings = append(out.Findings, sdlc.ConformanceFinding{
			Class: finding.Class, Check: finding.Check, Subject: finding.Subject,
			Detail: finding.Detail, Remedy: finding.Remedy,
		})
	}
	return out
}

func sdlcRepoOnboardingReportFromProto(p *pb.RepoOnboardingReport) *sdlc.RepoOnboardingReport {
	if p == nil {
		return &sdlc.RepoOnboardingReport{}
	}
	steps := make([]sdlc.RepoOnboardStep, 0, len(p.GetSteps()))
	for _, step := range p.GetSteps() {
		steps = append(steps, sdlc.RepoOnboardStep{
			Step: step.GetStep(), Outcome: step.GetOutcome(), Detail: step.GetDetail(), Remedy: step.GetRemedy(),
		})
	}
	report := &sdlc.RepoOnboardingReport{
		TenantID: p.GetTenantId(), ProjectID: p.GetProjectId(), Repo: p.GetRepo(), Steps: steps,
	}
	if p.GetConformance() != nil {
		report.Conformance = sdlcConformanceFromProto(p.GetConformance())
	}
	return report
}

func sdlcOpenWorkFromProto(p *pb.OpenWork) *sdlc.OpenWork {
	if p == nil {
		return nil
	}
	out := &sdlc.OpenWork{
		ProjectID: p.ProjectId, Repo: p.Repo, Agent: p.Agent,
		ReadAt:       timeFromUnix(p.ReadAtUnix),
		PullRequests: make([]sdlc.OpenPullRequest, 0, len(p.PullRequests)),
		Issues:       make([]sdlc.OpenIssue, 0, len(p.Issues)),
	}
	for _, pr := range p.PullRequests {
		out.PullRequests = append(out.PullRequests, sdlc.OpenPullRequest{
			Number: int(pr.Number), Title: pr.Title, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSha,
			CIState: pr.CiState, ReviewState: pr.ReviewState, URL: pr.Url,
			Created: timeFromUnix(pr.CreatedAtUnix), AgeSeconds: pr.AgeSeconds,
		})
	}
	for _, issue := range p.Issues {
		out.Issues = append(out.Issues, sdlc.OpenIssue{
			Number: int(issue.Number), Title: issue.Title, Labels: append([]string(nil), issue.Labels...),
			URL: issue.Url, Created: timeFromUnix(issue.CreatedAtUnix), AgeSeconds: issue.AgeSeconds,
		})
	}
	return out
}

func sdlcDeliveryFromProto(p *pb.Delivery) *sdlc.Delivery {
	if p == nil {
		return nil
	}
	d := &sdlc.Delivery{
		TaskID: p.TaskId, ProjectID: p.ProjectId, Attempt: int(p.Attempt), Branch: p.Branch, Base: p.Base,
		Target: p.Target, RequiresPullRequest: p.RequiresPullRequest, DeleteAfterMerge: p.DeleteAfterMerge,
		HeadSHA: p.HeadSha, WorkDir: p.WorkDir, Version: p.Version,
		CreatedAt: timeFromUnix(p.CreatedAtUnix), UpdatedAt: timeFromUnix(p.UpdatedAtUnix),
	}
	if p.PullRequest != nil {
		d.PullRequest = sdlc.PullRequest{
			Number: int(p.PullRequest.Number), URL: p.PullRequest.Url, State: p.PullRequest.State,
			Title: p.PullRequest.Title, Base: p.PullRequest.Base,
		}
	}
	for _, r := range p.Reviews {
		d.Reviews = append(d.Reviews, sdlc.ReviewRecord{
			Reviewer: r.Reviewer, ModelFamily: r.ModelFamily, Verdict: r.Verdict, HeadSHA: r.HeadSha,
			ExternalID: r.ExternalId, Round: int(r.Round), Findings: r.Findings, SubmittedAt: timeFromUnix(r.SubmittedAtUnix),
		})
	}
	for _, c := range p.Checks {
		d.Checks = append(d.Checks, sdlc.CheckRecord{
			Name: c.Name, HeadSHA: c.HeadSha, Status: c.Status, ForgeRunID: c.ForgeRunId, FinishedAt: timeFromUnix(c.FinishedAtUnix),
		})
	}
	for _, r := range p.SupersededRuns {
		d.SupersededRuns = append(d.SupersededRuns, sdlc.SupersessionRecord{
			Name: r.Name, ForgeRunID: r.ForgeRunId, HeadSHA: r.HeadSha, SupersededBy: r.SupersededBy,
			Status: r.Status, Disposition: r.Disposition, Detail: r.Detail, ObservedAt: timeFromUnix(r.ObservedAtUnix),
		})
	}
	return d
}

// handleSDLCAdoptIssue invokes AdoptIssue: an operator verb.
func handleSDLCAdoptIssue(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.sdlc.AdoptIssue"
	sdkReq, ok := req.(*sdlc.AdoptIssueRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).AdoptIssue(ctx, &pb.AdoptIssueRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, Issue: int32(sdkReq.Issue),
		PullRequest: int32(sdkReq.PullRequest), Ruling: sdkReq.Ruling,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sdlc.Adoption{
		TaskID: resp.GetTaskId(), Issue: int(resp.GetIssue()), PullRequest: int(resp.GetPullRequest()),
		Branch: resp.GetBranch(), Superseded: resp.GetSuperseded(),
	}, nil
}
