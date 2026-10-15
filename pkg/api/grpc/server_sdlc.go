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
	stderrors "errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// SDLCServiceProvider resolves the module's binding service per call, so a
// module that is disabled on this node answers Unavailable rather than
// leaving the service unregistered.
type SDLCServiceProvider interface {
	SDLC() *service.SDLCService
}

// SDLCServer implements pb.SDLCServiceServer over the binding service.
type SDLCServer struct {
	pb.UnimplementedSDLCServiceServer
	provider SDLCServiceProvider
	logger   *logger.Logger
}

// NewSDLCServer creates the gRPC server for the SDLC module.
func NewSDLCServer(provider SDLCServiceProvider, log *logger.Logger) *SDLCServer {
	return &SDLCServer{provider: provider, logger: log.WithField("component", "sdlc-grpc-server")}
}

var _ pb.SDLCServiceServer = (*SDLCServer)(nil)

// sdlcService resolves the running module or answers Unavailable.
func (s *SDLCServer) sdlcService() (*service.SDLCService, error) {
	if s.provider == nil {
		return nil, status.Error(codes.Unavailable, sdlc.ErrUnavailable.Error())
	}
	svc := s.provider.SDLC()
	if svc == nil {
		return nil, status.Error(codes.Unavailable, sdlc.ErrUnavailable.Error())
	}
	return svc, nil
}

// sdlcTenant decides the tenant a request operates in: the tenant bound to
// the call; naming another is refused for every caller. A call with no
// tenant bound is refused rather than trusted to name its own.
func sdlcTenant(ctx context.Context, requested string) (string, error) {
	tenantID, err := tenantctx.Target(ctx, "grpc.SDLC", requested)
	if err != nil {
		return "", toProtoError(err)
	}
	return tenantID, nil
}

// BindProject implements pb.SDLCServiceServer.
func (s *SDLCServer) BindProject(ctx context.Context, req *pb.BindProjectRequest) (*pb.BindProjectResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.Binding == nil || req.Binding.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "binding.project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	bound, err := svc.Bind(ctx, tenantID, bindingFromProto(req.Binding), service.BindOptions{LeavePaused: req.Paused})
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.BindProjectResponse{Binding: bindingToProto(bound)}, nil
}

// UnbindProject implements pb.SDLCServiceServer.
func (s *SDLCServer) UnbindProject(ctx context.Context, req *pb.UnbindProjectRequest) (*pb.UnbindProjectResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	if err := svc.Unbind(ctx, tenantID, req.ProjectId); err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.UnbindProjectResponse{Unbound: true}, nil
}

// GetBinding implements pb.SDLCServiceServer.
func (s *SDLCServer) GetBinding(ctx context.Context, req *pb.GetBindingRequest) (*pb.GetBindingResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	binding, err := svc.Binding(ctx, tenantID, req.ProjectId)
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.GetBindingResponse{Binding: bindingToProto(binding)}, nil
}

// GetDelivery implements pb.SDLCServiceServer.
func (s *SDLCServer) GetDelivery(ctx context.Context, req *pb.GetDeliveryRequest) (*pb.GetDeliveryResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	delivery, err := svc.Delivery(ctx, tenantID, req.TaskId)
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.GetDeliveryResponse{Delivery: deliveryToProto(delivery)}, nil
}

// Onboard implements pb.SDLCServiceServer: the explicit repair verb.
//
// Separate from GetConformance and under the write permission, because an
// operator asking what state a repository is in must be able to ask WITHOUT
// changing it.
func (s *SDLCServer) Onboard(ctx context.Context, req *pb.OnboardRequest) (*pb.OnboardResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	report, err := svc.Onboard(ctx, tenantID, req.ProjectId)
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.OnboardResponse{Report: onboardingReportToProto(report)}, nil
}

// OnboardRepo onboards one forge repository like an existing bound project.
func (s *SDLCServer) OnboardRepo(ctx context.Context, req *pb.OnboardRepoRequest) (*pb.OnboardRepoResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "repo and like are required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	report, err := svc.OnboardRepo(ctx, tenantID, sdlc.RepoOnboardRequest{
		Repo: req.Repo, Like: req.Like, MaxConcurrency: int(req.MaxConcurrency), Paused: req.Paused,
		Preflight: req.Preflight,
	})
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.OnboardRepoResponse{Report: repoOnboardingReportToProto(report)}, nil
}

// GetConformance implements pb.SDLCServiceServer: read-only.
//
// A non-conformant project is still an OK response. The report IS the answer,
// and a status code cannot carry which of seven things went wrong.
func (s *SDLCServer) GetConformance(ctx context.Context, req *pb.GetConformanceRequest) (*pb.GetConformanceResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	report, err := svc.Conformance(ctx, tenantID, req.ProjectId)
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.GetConformanceResponse{Report: conformanceToProto(report)}, nil
}

// GetOpenWork implements pb.SDLCServiceServer: read-only.
func (s *SDLCServer) GetOpenWork(ctx context.Context, req *pb.GetOpenWorkRequest) (*pb.GetOpenWorkResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	work, err := svc.OpenWork(ctx, tenantID, req.ProjectId)
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.GetOpenWorkResponse{Work: openWorkToProto(work)}, nil
}

// GetReviewerSelection implements pb.SDLCServiceServer: read-only.
func (s *SDLCServer) GetReviewerSelection(ctx context.Context, req *pb.GetReviewerSelectionRequest) (*pb.ReviewerSelectionResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	selection, err := svc.ReviewerSelection(ctx, tenantID, req.GetProjectId(), int(req.GetPullRequest()))
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.ReviewerSelectionResponse{Selection: reviewerSelectionToProto(selection)}, nil
}

// SetReviewerOverride implements pb.SDLCServiceServer: an operator verb.
func (s *SDLCServer) SetReviewerOverride(ctx context.Context, req *pb.SetReviewerOverrideRequest) (*pb.ReviewerSelectionResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	selection, err := svc.SetReviewerOverride(ctx, tenantID, req.GetProjectId(), int(req.GetPullRequest()), req.GetProfile())
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.ReviewerSelectionResponse{Selection: reviewerSelectionToProto(selection)}, nil
}

// ClearReviewerOverride implements pb.SDLCServiceServer: an operator verb.
func (s *SDLCServer) ClearReviewerOverride(ctx context.Context, req *pb.ClearReviewerOverrideRequest) (*pb.ReviewerSelectionResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	selection, err := svc.ClearReviewerOverride(ctx, tenantID, req.GetProjectId(), int(req.GetPullRequest()))
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.ReviewerSelectionResponse{Selection: reviewerSelectionToProto(selection)}, nil
}

// FileIssue implements pb.SDLCServiceServer: an audited operator verb.
func (s *SDLCServer) FileIssue(ctx context.Context, req *pb.FileIssueRequest) (*pb.FileIssueResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	issue, err := svc.FileIssue(ctx, tenantID, req.GetProjectId(), sdlc.IssueFiling{
		Title: req.GetTitle(), Body: req.GetBody(), Labels: append([]string(nil), req.GetLabels()...), Parent: int(req.GetParent()),
	})
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.FileIssueResponse{Issue: forgeIssueToProto(issue)}, nil
}

// ListCollaborators implements pb.SDLCServiceServer: read-only.
func (s *SDLCServer) ListCollaborators(ctx context.Context, req *pb.ListCollaboratorsRequest) (*pb.ListCollaboratorsResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	users, err := svc.Collaborators(ctx, tenantID, req.GetProjectId())
	if err != nil {
		return nil, convertSDLCError(err)
	}
	out := make([]*pb.ForgeUser, 0, len(users))
	for _, user := range users {
		out = append(out, &pb.ForgeUser{Login: user.Login, FullName: user.FullName})
	}
	return &pb.ListCollaboratorsResponse{Users: out}, nil
}

// forgeIssueToProto maps a filed issue onto the wire.
func forgeIssueToProto(issue *sdlc.ForgeIssue) *pb.ForgeIssue {
	if issue == nil {
		return nil
	}
	return &pb.ForgeIssue{
		Number: int32(issue.Number), Title: issue.Title, Body: issue.Body, State: issue.State,
		Labels: append([]string(nil), issue.Labels...), Author: issue.Author, Url: issue.URL,
	}
}

// reviewerRequest resolves the module and the tenant every reviewer-override
// verb needs, refusing a request that names no project.
func (s *SDLCServer) reviewerRequest(ctx context.Context, requestedTenant, projectID string) (*service.SDLCService, string, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, "", err
	}
	if projectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	tenantID, err := sdlcTenant(ctx, requestedTenant)
	if err != nil {
		return nil, "", err
	}
	return svc, tenantID, nil
}

// AdoptIssue implements pb.SDLCServiceServer: an operator verb that creates
// the task for one forge issue and hands the issue back to the factory.
func (s *SDLCServer) AdoptIssue(ctx context.Context, req *pb.AdoptIssueRequest) (*pb.AdoptIssueResponse, error) {
	svc, tenantID, err := s.reviewerRequest(ctx, req.GetTenantId(), req.GetProjectId())
	if err != nil {
		return nil, err
	}
	adoption, err := svc.AdoptIssue(ctx, tenantID, sdlc.AdoptIssueRequest{
		ProjectID: req.GetProjectId(), Issue: int(req.GetIssue()), PullRequest: int(req.GetPullRequest()), Ruling: req.GetRuling(),
	})
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.AdoptIssueResponse{
		TaskId: adoption.TaskID, Issue: int32(adoption.Issue), PullRequest: int32(adoption.PullRequest),
		Branch: adoption.Branch, Superseded: adoption.Superseded,
	}, nil
}

// ListForges implements pb.SDLCServiceServer.
//
// The forge adapters this build actually carries, so a caller can show an
// unavailable platform DISABLED with the reason rather than hide it. This is
// BUILD capability, not tenant data, so no tenant is derived — exactly as the
// REST route GET /api/v1/sdlc/forges behaves. The module-not-running answer
// stays Unavailable, distinct from an OK listing nothing.
func (s *SDLCServer) ListForges(_ context.Context, _ *pb.ListForgesRequest) (*pb.ListForgesResponse, error) {
	if _, err := s.sdlcService(); err != nil {
		return nil, err
	}
	// RegisteredForgeTypes already returns its types sorted, so two reads
	// serialize identically without a second sort here.
	registered := sdlc.RegisteredForgeTypes()
	forges := make([]string, 0, len(registered))
	for _, forgeType := range registered {
		forges = append(forges, string(forgeType))
	}
	return &pb.ListForgesResponse{Forges: forges, Count: int32(len(forges))}, nil
}

// convertSDLCError maps the module's typed errors onto gRPC codes.
func convertSDLCError(err error) error {
	if mapped, ok := referenceStatus(err); ok {
		return mapped
	}
	var (
		notRegistered  *sdlc.ForgeNotRegisteredError
		strategy       *sdlc.StrategyNotRegisteredError
		projectMissing *service.ProjectNotFoundError
	)
	if isAgentCatalogError(err) {
		return agentCatalogStatusError(err)
	}
	switch {
	case stderrors.Is(err, sdlc.ErrNotFound), stderrors.As(err, &projectMissing), stderrors.Is(err, orchestration.ErrProjectNotFound),
		stderrors.Is(err, orchestration.ErrTaskNotFound):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.Is(err, sdlc.ErrAlreadyExists), stderrors.Is(err, sdlc.ErrVersionConflict), stderrors.Is(err, sdlc.ErrWebhookRouteConflict),
		stderrors.Is(err, orchestration.ErrTaskSourceAlreadyAdopted):
		return status.Error(codes.AlreadyExists, err.Error())
	case stderrors.Is(err, sdlc.ErrAdoptionRefused), stderrors.Is(err, orchestration.ErrTaskInvalidState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case stderrors.Is(err, sdlc.ErrAdoptionIncomplete):
		// The task exists and runs; a forge write after it did not land.
		return status.Error(codes.Unavailable, err.Error())
	case stderrors.Is(err, sdlc.ErrInvalidInput), stderrors.As(err, &notRegistered), stderrors.As(err, &strategy),
		stderrors.Is(err, orchestration.ErrWorkflowValidation), stderrors.Is(err, sdlc.ErrReview),
		stderrors.Is(err, orchestration.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, err.Error())
	case stderrors.Is(err, sdlc.ErrUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// =============================================================================
// Converters
// =============================================================================

func bindingFromProto(p *pb.ProjectBinding) *sdlc.ProjectBinding {
	b := &sdlc.ProjectBinding{
		ProjectID:       p.ProjectId,
		Strategy:        p.Strategy,
		Verification:    append([]string(nil), p.Verification...),
		MaxReviewRounds: int(p.MaxReviewRounds),
	}
	b.Worker = AgentRefFromProto(p.GetWorker())
	b.Triage = AgentRefFromProto(p.GetTriage())
	if p.Forge != nil {
		b.Forge = sdlc.ForgeConfig{
			Type: sdlc.ForgeType(p.Forge.Type), BaseURL: p.Forge.BaseUrl, Owner: p.Forge.Owner, Repo: p.Forge.Repo,
			Labels: copyStringMap(p.Forge.Labels), TriggerLabel: p.Forge.TriggerLabel,
			WebhookSecretRef: p.Forge.WebhookSecretRef, TokenSecretRef: p.Forge.TokenSecretRef,
			SSHHostKey: p.Forge.SshHostKey,
		}
	}
	if p.Review != nil {
		b.Review = sdlc.ReviewPolicy{
			RequiredApprovals: int(p.Review.RequiredApprovals), RequireIndependentReviewer: p.Review.RequireIndependentReviewer,
			RequireGreenChecks: p.Review.RequireGreenChecks, ChecksSettleWindow: time.Duration(p.Review.ChecksSettleWindowNs),
			Reviewers: copyStringMap(p.Review.Reviewers),
		}
	}
	b.Reviewer = reviewerFromProto(p.Reviewer)
	b.Reviewers = reviewersFromProto(p.Reviewers)
	b.Discovery = sdlc.DiscoveryMode(p.Discovery)
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
			DefaultPriority: int(p.Ranking.DefaultPriority), AnalyzerName: p.Ranking.AnalyzerName,
			DeclaredDependents: append([]string(nil), p.Ranking.DeclaredDependents...),
		}
	}
	if p.Onboarding != nil {
		b.Onboarding = sdlc.OnboardingConfig{
			WebhookEvents:          append([]string(nil), p.Onboarding.WebhookEvents...),
			ProtectedBranch:        p.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), p.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), p.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      p.Onboarding.ForbiddenPathsRef,
			LabelColors:            copyStringMap(p.Onboarding.LabelColors),
		}
	}
	return b
}

func bindingToProto(b *sdlc.ProjectBinding) *pb.ProjectBinding {
	if b == nil {
		return nil
	}
	return &pb.ProjectBinding{
		ProjectId: b.ProjectID,
		Forge: &pb.ForgeConfig{
			Type: string(b.Forge.Type), BaseUrl: b.Forge.BaseURL, Owner: b.Forge.Owner, Repo: b.Forge.Repo,
			Labels: copyStringMap(b.Forge.Labels), TriggerLabel: b.Forge.TriggerLabel,
			WebhookSecretRef: b.Forge.WebhookSecretRef, TokenSecretRef: b.Forge.TokenSecretRef,
			SshHostKey: b.Forge.SSHHostKey,
		},
		Review: &pb.ReviewPolicy{
			RequiredApprovals: int32(b.Review.RequiredApprovals), RequireIndependentReviewer: b.Review.RequireIndependentReviewer,
			RequireGreenChecks: b.Review.RequireGreenChecks, ChecksSettleWindowNs: int64(b.Review.ChecksSettleWindow),
			Reviewers: copyStringMap(b.Review.Reviewers),
		},
		Reviewer:           reviewerToProto(b.Reviewer),
		Reviewers:          reviewersToProto(b.Reviewers),
		Discovery:          string(b.Discovery),
		TriageMayClose:     b.TriageMayClose,
		WorkerPromptAppend: b.WorkerPromptAppend,
		TriagePromptAppend: b.TriagePromptAppend,
		Rulers:             append([]string(nil), b.Rulers...),
		MergeTrain:         &pb.MergeTrainPolicy{Disabled: b.MergeTrain.Disabled, MaxSize: int32(b.MergeTrain.MaxSize)},
		Clustering:         &pb.ClusteringPolicy{Disabled: b.Clustering.Disabled, MaxSize: int32(b.Clustering.MaxSize)},
		Triage:             AgentRefToProto(b.Triage),
		Strategy:           b.Strategy,
		Worker:             AgentRefToProto(b.Worker),
		Verification:       append([]string(nil), b.Verification...),
		MaxReviewRounds:    int32(b.MaxReviewRounds),
		Ranking: &pb.RankingPolicy{
			DefaultPriority: int32(b.Ranking.DefaultPriority), AnalyzerName: b.Ranking.AnalyzerName,
			DeclaredDependents: append([]string(nil), b.Ranking.DeclaredDependents...),
		},
		Onboarding: &pb.OnboardingConfig{
			WebhookEvents:          append([]string(nil), b.Onboarding.WebhookEvents...),
			ProtectedBranch:        b.Onboarding.ProtectedBranch,
			RequiredStatusContexts: append([]string(nil), b.Onboarding.RequiredStatusContexts...),
			ForbiddenPaths:         append([]string(nil), b.Onboarding.ForbiddenPaths...),
			ForbiddenPathsRef:      b.Onboarding.ForbiddenPathsRef,
			LabelColors:            copyStringMap(b.Onboarding.LabelColors),
		},
		CreatedAtUnix: unixOrZero(b.CreatedAt),
		UpdatedAtUnix: unixOrZero(b.UpdatedAt),
	}
}

// reviewerFromProto maps one reviewer profile off the wire. Every field is
// carried, because a bind replaces the whole record and a field this dropped
// would be erased by every rebind through this transport.
func reviewerFromProto(p *pb.ReviewerConfig) sdlc.ReviewerConfig {
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

// reviewerToProto maps one reviewer profile onto the wire.
func reviewerToProto(r sdlc.ReviewerConfig) *pb.ReviewerConfig {
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

func reviewersFromProto(ps []*pb.ReviewerConfig) []sdlc.ReviewerConfig {
	if len(ps) == 0 {
		return nil
	}
	out := make([]sdlc.ReviewerConfig, 0, len(ps))
	for _, p := range ps {
		out = append(out, reviewerFromProto(p))
	}
	return out
}

func reviewersToProto(rs []sdlc.ReviewerConfig) []*pb.ReviewerConfig {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*pb.ReviewerConfig, 0, len(rs))
	for _, r := range rs {
		out = append(out, reviewerToProto(r))
	}
	return out
}

func reviewerSelectionToProto(s *sdlc.ReviewerSelection) *pb.ReviewerSelection {
	if s == nil {
		return nil
	}
	return &pb.ReviewerSelection{
		ProjectId: s.ProjectID, PullRequest: int32(s.PullRequest), Profile: s.Profile,
		Overridden: s.Overridden, CountsTowardMerge: s.CountsTowardMerge,
		StatusContext: s.StatusContext, UpdatedAtUnix: unixOrZero(s.UpdatedAt),
		Redispatched: s.Redispatched,
	}
}

func onboardingReportToProto(r *sdlc.OnboardingReport) *pb.OnboardingReport {
	if r == nil {
		return nil
	}
	return &pb.OnboardingReport{
		ProjectId: r.ProjectID, TenantId: r.TenantID, Repo: r.Repo,
		WebhookUrl: r.WebhookURL, WebhookEnsured: r.WebhookEnsured,
		LabelsCreated: append([]string(nil), r.LabelsCreated...),
		Conformance:   conformanceToProto(r.Conformance),
	}
}

func repoOnboardingReportToProto(r *sdlc.RepoOnboardingReport) *pb.RepoOnboardingReport {
	if r == nil {
		return nil
	}
	out := &pb.RepoOnboardingReport{
		TenantId: r.TenantID, ProjectId: r.ProjectID, Repo: r.Repo,
		Steps:       make([]*pb.RepoOnboardStep, 0, len(r.Steps)),
		Conformance: conformanceToProto(r.Conformance),
	}
	for _, step := range r.Steps {
		out.Steps = append(out.Steps, &pb.RepoOnboardStep{
			Step: step.Step, Outcome: string(step.Outcome), Detail: step.Detail, Remedy: step.Remedy,
		})
	}
	return out
}

func conformanceToProto(r *sdlc.ConformanceReport) *pb.ConformanceReport {
	if r == nil {
		return nil
	}
	out := &pb.ConformanceReport{
		ProjectId: r.ProjectID, TenantId: r.TenantID, Repo: r.Repo,
		Conformant: r.Conformant, WebhookUrl: r.WebhookURL,
		CheckedAtUnix: unixOrZero(r.CheckedAt),
		Checks:        make([]*pb.ConformanceCheckResult, 0, len(r.Checks)),
		Findings:      make([]*pb.ConformanceFinding, 0, len(r.Findings)),
	}
	for _, check := range r.Checks {
		out.Checks = append(out.Checks, &pb.ConformanceCheckResult{
			Name: check.Name, Status: string(check.Status), Findings: int32(check.Findings),
		})
	}
	for _, finding := range r.Findings {
		out.Findings = append(out.Findings, &pb.ConformanceFinding{
			Class: string(finding.Class), Check: finding.Check, Subject: finding.Subject,
			Detail: finding.Detail, Remedy: finding.Remedy,
		})
	}
	return out
}

func openWorkToProto(w *sdlc.OpenWork) *pb.OpenWork {
	if w == nil {
		return nil
	}
	out := &pb.OpenWork{
		ProjectId: w.ProjectID, Repo: w.Repo, Agent: w.Agent,
		ReadAtUnix:   unixOrZero(w.ReadAt),
		PullRequests: make([]*pb.OpenPullRequest, 0, len(w.PullRequests)),
		Issues:       make([]*pb.OpenIssue, 0, len(w.Issues)),
	}
	for _, pr := range w.PullRequests {
		out.PullRequests = append(out.PullRequests, &pb.OpenPullRequest{
			Number: int32(pr.Number), Title: pr.Title, HeadRef: pr.HeadRef, HeadSha: pr.HeadSHA,
			CiState: string(pr.CIState), ReviewState: string(pr.ReviewState), Url: pr.URL,
			CreatedAtUnix: unixOrZero(pr.Created), AgeSeconds: pr.AgeSeconds,
		})
	}
	for _, issue := range w.Issues {
		out.Issues = append(out.Issues, &pb.OpenIssue{
			Number: int32(issue.Number), Title: issue.Title, Labels: append([]string(nil), issue.Labels...),
			Url: issue.URL, CreatedAtUnix: unixOrZero(issue.Created), AgeSeconds: issue.AgeSeconds,
		})
	}
	return out
}

func deliveryToProto(d *sdlc.Delivery) *pb.Delivery {
	if d == nil {
		return nil
	}
	out := &pb.Delivery{
		TaskId: d.TaskID, ProjectId: d.ProjectID, Attempt: int32(d.Attempt), Branch: d.Branch, Base: d.Base,
		Target: d.Target, RequiresPullRequest: d.RequiresPullRequest, DeleteAfterMerge: d.DeleteAfterMerge,
		HeadSha: d.HeadSHA, WorkDir: d.WorkDir,
		PullRequest: &pb.PullRequest{
			Number: int32(d.PullRequest.Number), Url: d.PullRequest.URL, State: string(d.PullRequest.State),
			Title: d.PullRequest.Title, Base: d.PullRequest.Base,
		},
		Version: d.Version, CreatedAtUnix: unixOrZero(d.CreatedAt), UpdatedAtUnix: unixOrZero(d.UpdatedAt),
	}
	for _, r := range d.Reviews {
		out.Reviews = append(out.Reviews, &pb.ReviewRecord{
			Reviewer: r.Reviewer, ModelFamily: r.ModelFamily, Verdict: string(r.Verdict), HeadSha: r.HeadSHA,
			ExternalId: r.ExternalID, Round: int32(r.Round), Findings: r.Findings, SubmittedAtUnix: unixOrZero(r.SubmittedAt),
		})
	}
	for _, c := range d.Checks {
		out.Checks = append(out.Checks, &pb.CheckRecord{
			Name: c.Name, HeadSha: c.HeadSHA, Status: string(c.Status), ForgeRunId: c.ForgeRunID, FinishedAtUnix: unixOrZero(c.FinishedAt),
		})
	}
	for _, r := range d.SupersededRuns {
		out.SupersededRuns = append(out.SupersededRuns, &pb.SupersessionRecord{
			Name: r.Name, ForgeRunId: r.ForgeRunID, HeadSha: r.HeadSHA, SupersededBy: r.SupersededBy,
			Status: string(r.Status), Disposition: string(r.Disposition), Detail: r.Detail,
			ObservedAtUnix: unixOrZero(r.ObservedAt),
		})
	}
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
