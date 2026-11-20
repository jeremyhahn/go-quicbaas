// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// tenantWitness records the tenant each call into the orchestration service
// found in its context. A tenant-routed store resolves its backend from the
// context, not from the tenantID argument, so a call that arrives with a
// bare context lands on the default tenant's store (#1828, the #820 class).
type tenantWitness struct {
	mu   sync.Mutex
	seen []string
}

func (w *tenantWitness) record(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = append(w.seen, orchestration.TenantFromContext(ctx))
}

func (w *tenantWitness) tenants() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.seen...)
}

// tenantWitnessAgentService is an agent service whose every method a
// tenant-carrying orchestrator handler reaches records the context's tenant.
type tenantWitnessAgentService struct {
	*fakeAgentService
	w *tenantWitness
}

func (s *tenantWitnessAgentService) CreateTask(ctx context.Context, _ *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	s.w.record(ctx)
	return &orchestration.Task{}, nil
}

func (s *tenantWitnessAgentService) GetTask(ctx context.Context, _, _ string) (*orchestration.Task, error) {
	s.w.record(ctx)
	return &orchestration.Task{}, nil
}

func (s *tenantWitnessAgentService) ListTasksPage(ctx context.Context, _ string, _ *orchestration.TaskFilter, _ *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	s.w.record(ctx)
	return &orchestration.PageResult[*orchestration.Task]{}, nil
}

func (s *tenantWitnessAgentService) ClaimTask(ctx context.Context, _, _, _ string) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) CompleteTask(ctx context.Context, _, _ string, _ *orchestration.TaskResult) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) BlockTask(ctx context.Context, _, _ string, _ orchestration.TaskBlockKind, _ string) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) ReadmitTask(ctx context.Context, _, _ string) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) DeleteTask(ctx context.Context, _, _ string) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) SubmitTaskArtifacts(ctx context.Context, _, _ string, _ []orchestration.Artifact) error {
	s.w.record(ctx)
	return nil
}

func (s *tenantWitnessAgentService) GetTaskArtifact(ctx context.Context, _, _, _ string) (*orchestration.Artifact, error) {
	s.w.record(ctx)
	return &orchestration.Artifact{}, nil
}

func (s *tenantWitnessAgentService) ListTaskArtifacts(ctx context.Context, _, _ string) ([]orchestration.Artifact, error) {
	s.w.record(ctx)
	return nil, nil
}

func (s *tenantWitnessAgentService) StartJob(ctx context.Context, _ *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	s.w.record(ctx)
	return &orchestration.StartJobResponse{}, nil
}

func (s *tenantWitnessAgentService) GetJobStatus(ctx context.Context, _, _ string) (*orchestration.JobStatus, error) {
	s.w.record(ctx)
	return &orchestration.JobStatus{}, nil
}

func (s *tenantWitnessAgentService) ListJobsPage(ctx context.Context, _ string, _ *orchestration.JobFilter, _ *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	s.w.record(ctx)
	return &orchestration.PageResult[*orchestration.JobStatus]{}, nil
}

func (s *tenantWitnessAgentService) CancelJob(ctx context.Context, _, _ string) error {
	s.w.record(ctx)
	return nil
}

// SessionEventLog makes the witness a SessionLogAccessor for
// GetSessionConversation.
func (s *tenantWitnessAgentService) SessionEventLog() orchestration.SessionEventLog {
	return &tenantWitnessSessionLog{w: s.w}
}

// TenantSpendPolicies and ConsumptionLedger make the witness a
// service.TenantSpendPolicySource for the tenant spend policy handlers.
func (s *tenantWitnessAgentService) TenantSpendPolicies() orchestration.TenantSpendPolicyStore {
	return &tenantWitnessSpendPolicies{w: s.w}
}

func (s *tenantWitnessAgentService) ConsumptionLedger() orchestration.ConsumptionLedger { return nil }

type tenantWitnessSessionLog struct {
	orchestration.SessionEventLog
	w *tenantWitness
}

func (l *tenantWitnessSessionLog) GetMeta(ctx context.Context, _, _ string) (*orchestration.SessionMeta, error) {
	l.w.record(ctx)
	return nil, orchestration.ErrSessionLogNotFound
}

func (l *tenantWitnessSessionLog) Range(ctx context.Context, _, _ string, _ uint64, _ int) ([]*orchestration.SessionEvent, uint64, error) {
	l.w.record(ctx)
	return nil, 0, nil
}

type tenantWitnessSpendPolicies struct {
	w *tenantWitness
}

func (p *tenantWitnessSpendPolicies) Get(ctx context.Context, tenantID string) (*orchestration.TenantSpendPolicy, error) {
	p.w.record(ctx)
	return &orchestration.TenantSpendPolicy{TenantID: tenantID}, nil
}

func (p *tenantWitnessSpendPolicies) Put(ctx context.Context, _ *orchestration.TenantSpendPolicy) error {
	p.w.record(ctx)
	return nil
}

// TestEmbeddedOrchestrator_StampsTheTenantIntoTheContext hands every
// orchestrator handler whose request carries a tenant a BARE context, which
// is what the SDK client hands in, and asserts the orchestration service
// found the request's tenant in the context of every call it received.
func TestEmbeddedOrchestrator_StampsTheTenantIntoTheContext(t *testing.T) {
	const tenantID = "acme"

	rows := []struct {
		method string
		req    interface{}
	}{
		{"GetSessionConversation", &orchestratorSDK.GetSessionConversationRequest{TenantID: tenantID, SessionID: "s1"}},
		{"CreateTask", &orchestratorSDK.CreateTaskRequest{TenantID: tenantID, Subject: "subject"}},
		{"GetTask", &orchestratorSDK.GetTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"ListTasks", &orchestratorSDK.ListTasksRequest{TenantID: tenantID}},
		{"ClaimTask", &orchestratorSDK.ClaimTaskRequest{TenantID: tenantID, TaskID: "t1", AgentID: "agent-1"}},
		{"BlockTask", &orchestratorSDK.BlockTaskRequest{TenantID: tenantID, TaskID: "t1", Kind: "needs_info", Reason: "why"}},
		{"ReadmitTask", &orchestratorSDK.ReadmitTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"DeleteTask", &orchestratorSDK.DeleteTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"CompleteTask", &orchestratorSDK.CompleteTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"SubmitTaskArtifacts", &orchestratorSDK.SubmitTaskArtifactsRequest{TenantID: tenantID, TaskID: "t1"}},
		{"GetTaskArtifact", &orchestratorSDK.GetTaskArtifactRequest{TenantID: tenantID, TaskID: "t1", Name: "a"}},
		{"ListTaskArtifacts", &orchestratorSDK.ListTaskArtifactsRequest{TenantID: tenantID, TaskID: "t1"}},
		{"StartJob", &orchestratorSDK.StartJobRequest{TenantID: tenantID, Description: "d"}},
		{"GetJobStatus", &orchestratorSDK.GetJobStatusRequest{TenantID: tenantID, JobID: "j1"}},
		{"ListJobs", &orchestratorSDK.ListJobsRequest{TenantID: tenantID}},
		{"CancelJob", &orchestratorSDK.CancelJobRequest{TenantID: tenantID, JobID: "j1"}},
		{"GetTenantSpendPolicy", &orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: tenantID}},
		{"SetTenantSpendPolicy", &orchestratorSDK.SetTenantSpendPolicyRequest{TenantID: tenantID}},
	}

	for _, row := range rows {
		t.Run(row.method, func(t *testing.T) {
			w := &tenantWitness{}
			tr, err := New(&orchMock{
				mockQuicBaaS: &mockQuicBaaS{},
				agent:        &tenantWitnessAgentService{fakeAgentService: &fakeAgentService{}, w: w},
				proj:         &fakeProjectService{},
				admin:        &fakeFleetAdminService{},
			})
			require.NoError(t, err)

			// The handler's own result does not matter here: only the context
			// the service was called with does.
			_, _ = tr.Request(context.Background(), "quicbaas.Orchestrator", row.method, row.req)

			seen := w.tenants()
			require.NotEmpty(t, seen, "%s never reached the orchestration service", row.method)
			for i, got := range seen {
				assert.Equal(t, tenantID, got,
					"%s call %d reached the service with tenant %q in context, want %q: a tenant-routed "+
						"store resolves the backend from the context, so it lands on the wrong store",
					row.method, i, got, tenantID)
			}
		})
	}
}
