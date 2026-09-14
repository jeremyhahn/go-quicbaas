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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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
// the call wins, and a requested tenant naming another is refused.
func sdlcTenant(ctx context.Context, requested string) (string, error) {
	bound := iam.TenantFromContext(ctx)
	switch {
	case bound == "" && requested == "":
		return "", status.Error(codes.InvalidArgument, "tenant_id is required")
	case bound == "":
		return requested, nil
	case requested != "" && requested != bound:
		return "", status.Error(codes.PermissionDenied, "tenant_id must be the caller's own tenant")
	default:
		return bound, nil
	}
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
	bound, err := svc.Bind(ctx, tenantID, bindingFromProto(req.Binding))
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
	var (
		notRegistered  *sdlc.ForgeNotRegisteredError
		strategy       *sdlc.StrategyNotRegisteredError
		projectMissing *service.ProjectNotFoundError
	)
	switch {
	case stderrors.Is(err, sdlc.ErrNotFound), stderrors.As(err, &projectMissing), stderrors.Is(err, orchestration.ErrProjectNotFound):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.Is(err, sdlc.ErrAlreadyExists), stderrors.Is(err, sdlc.ErrVersionConflict), stderrors.Is(err, sdlc.ErrWebhookRouteConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case stderrors.Is(err, sdlc.ErrInvalidInput), stderrors.As(err, &notRegistered), stderrors.As(err, &strategy),
		stderrors.Is(err, orchestration.ErrWorkflowValidation):
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
	if p.Worker != nil {
		b.Worker = sdlc.WorkerConfig{Backend: p.Worker.Backend, Model: p.Worker.Model, Image: p.Worker.Image}
	}
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
	if p.Reviewer != nil {
		b.Reviewer = sdlc.ReviewerConfig{
			Backend: p.Reviewer.Backend, Model: p.Reviewer.Model, Image: p.Reviewer.Image,
			Login: p.Reviewer.Login, TokenSecretRef: p.Reviewer.TokenSecretRef,
			StatusContext: p.Reviewer.StatusContext,
		}
	}
	b.Discovery = sdlc.DiscoveryMode(p.Discovery)
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
		Reviewer: &pb.ReviewerConfig{
			Backend: b.Reviewer.Backend, Model: b.Reviewer.Model, Image: b.Reviewer.Image,
			Login: b.Reviewer.Login, TokenSecretRef: b.Reviewer.TokenSecretRef,
			StatusContext: b.Reviewer.StatusContext,
		},
		Discovery:       string(b.Discovery),
		Strategy:        b.Strategy,
		Worker:          &pb.WorkerConfig{Backend: b.Worker.Backend, Model: b.Worker.Model, Image: b.Worker.Image},
		Verification:    append([]string(nil), b.Verification...),
		MaxReviewRounds: int32(b.MaxReviewRounds),
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
