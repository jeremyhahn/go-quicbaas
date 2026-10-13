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
	"math"
	"testing"

	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestOrchestratorHandlers_ResponseOutOfRange proves that an orchestration
// counter too large for its int32 wire field becomes codes.Internal instead
// of a wrapped integer.
func TestOrchestratorHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := tenantctx.WithTenant(context.Background(), "t")
	jobStatus := func(mutate func(*orchestration.JobStatus)) *grpcTestAgentService {
		return &grpcTestAgentService{
			getJobStatusFunc: func(context.Context, string, string) (*orchestration.JobStatus, error) {
				js := &orchestration.JobStatus{JobID: "job-1"}
				mutate(js)
				return js, nil
			},
		}
	}

	tests := []struct {
		name  string
		field string
		svc   *grpcTestAgentService
		call  func(srv *OrchestratorServer) error
	}{
		{
			name:  "GetJobStatus iteration",
			field: "iteration",
			svc:   jobStatus(func(js *orchestration.JobStatus) { js.Iteration = tooBig }),
		},
		{
			name:  "GetJobStatus attempt budget",
			field: "attempt_budget",
			svc:   jobStatus(func(js *orchestration.JobStatus) { js.AttemptBudget = tooBig }),
		},
		{
			name:  "GetJobStatus attempt record",
			field: "attempt",
			svc: jobStatus(func(js *orchestration.JobStatus) {
				js.Attempts = []orchestration.JobAttemptRecord{{Attempt: tooBig}}
			}),
		},
		{
			name:  "GetJobStatus review round",
			field: "round",
			svc: jobStatus(func(js *orchestration.JobStatus) {
				js.Delivery = &orchestration.JobDelivery{Reviews: []orchestration.JobReviewRound{{Round: tooBig}}}
			}),
		},
		{
			name:  "GetJobStatus pull request number",
			field: "pull_request_number",
			svc: jobStatus(func(js *orchestration.JobStatus) {
				js.Delivery = &orchestration.JobDelivery{PullRequestNumber: tooBig}
			}),
		},
		{
			name:  "ListJobs task count",
			field: "task_count",
			svc:   &grpcTestAgentService{listJobs: []*orchestration.JobStatus{{JobID: "job-1", TaskCount: tooBig}}},
			call: func(srv *OrchestratorServer) error {
				_, err := srv.ListJobs(ctx, &pb.OrchestratorListJobsRequest{TenantId: "t"})
				return err
			},
		},
		{
			name:  "ListWorkflows max parallelism",
			field: "max_parallelism",
			svc: &grpcTestAgentService{listWorkflows: []*orchestration.WorkflowDefinition{{
				Name:     "wf",
				Settings: orchestration.WorkflowSettings{MaxParallelism: tooBig},
			}}},
			call: func(srv *OrchestratorServer) error {
				_, err := srv.ListWorkflows(ctx, &pb.OrchestratorListWorkflowsRequest{TenantId: "t"})
				return err
			},
		},
		{
			name:  "Health total sessions",
			field: "total_sessions",
			svc: &grpcTestAgentService{
				healthCheckFunc: func(context.Context) (*orchestration.HealthCheckReport, error) {
					return &orchestration.HealthCheckReport{TotalSessions: tooBig}, nil
				},
			},
			call: func(srv *OrchestratorServer) error {
				_, err := srv.Health(ctx, &pb.OrchestratorHealthRequest{TenantId: "t"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewOrchestratorServer(tt.svc, createOrchestratorGRPCTestLogger())
			call := tt.call
			if call == nil {
				call = func(srv *OrchestratorServer) error {
					_, err := srv.GetJobStatus(ctx, &pb.OrchestratorGetJobStatusRequest{TenantId: "t", JobId: "job-1"})
					return err
				}
			}
			requireRangeStatus(t, call(srv), codes.Internal, tt.field)
		})
	}
}
