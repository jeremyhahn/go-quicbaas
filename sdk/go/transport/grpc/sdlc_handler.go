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
	"Conformance": handleSDLCConformance,
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
		TenantId: sdkReq.TenantID, Binding: sdlcBindingToProto(&binding),
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
		Reviewer: &pb.ReviewerConfig{
			Backend: b.Reviewer.Backend, Model: b.Reviewer.Model, Image: b.Reviewer.Image,
			Login: b.Reviewer.Login, TokenSecretRef: b.Reviewer.TokenSecretRef,
			StatusContext: b.Reviewer.StatusContext,
		},
		Discovery:       b.Discovery,
		Triage:          &pb.WorkerConfig{Backend: b.Triage.Backend, Model: b.Triage.Model, Image: b.Triage.Image},
		Strategy:        b.Strategy,
		Worker:          &pb.WorkerConfig{Backend: b.Worker.Backend, Model: b.Worker.Model, Image: b.Worker.Image},
		Verification:    append([]string(nil), b.Verification...),
		MaxReviewRounds: int32(b.MaxReviewRounds),
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
	if p.Worker != nil {
		b.Worker = sdlc.WorkerConfig{Backend: p.Worker.Backend, Model: p.Worker.Model, Image: p.Worker.Image}
	}
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
	if p.Reviewer != nil {
		b.Reviewer = sdlc.ReviewerConfig{
			Backend: p.Reviewer.Backend, Model: p.Reviewer.Model, Image: p.Reviewer.Image,
			Login: p.Reviewer.Login, TokenSecretRef: p.Reviewer.TokenSecretRef,
			StatusContext: p.Reviewer.StatusContext,
		}
	}
	b.Discovery = p.Discovery
	if p.Triage != nil {
		b.Triage = sdlc.WorkerConfig{Backend: p.Triage.Backend, Model: p.Triage.Model, Image: p.Triage.Image}
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
