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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// tenantWitnessAgentService records the tenant each orchestration service call
// finds in its context, then delegates to the plain fake.
type tenantWitnessAgentService struct {
	*fakeAgentService

	log  orchestration.SessionEventLog
	seen []string
}

func (w *tenantWitnessAgentService) witness(ctx context.Context) {
	w.seen = append(w.seen, orchestration.TenantFromContext(ctx))
}

func (w *tenantWitnessAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	w.witness(ctx)
	return w.fakeAgentService.CreateTask(ctx, req)
}

func (w *tenantWitnessAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	w.witness(ctx)
	return w.fakeAgentService.GetTask(ctx, tenantID, taskID)
}

func (w *tenantWitnessAgentService) ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	w.witness(ctx)
	return w.fakeAgentService.ListTasksPage(ctx, tenantID, filter, page)
}

func (w *tenantWitnessAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	w.witness(ctx)
	return w.fakeAgentService.ClaimTask(ctx, tenantID, taskID, agentID)
}

func (w *tenantWitnessAgentService) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	w.witness(ctx)
	return w.fakeAgentService.BlockTask(ctx, tenantID, taskID, kind, reason)
}

func (w *tenantWitnessAgentService) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	w.witness(ctx)
	return w.fakeAgentService.ReadmitTask(ctx, tenantID, taskID)
}

func (w *tenantWitnessAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	w.witness(ctx)
	return w.fakeAgentService.DeleteTask(ctx, tenantID, taskID)
}

func (w *tenantWitnessAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	w.witness(ctx)
	return w.fakeAgentService.CompleteTask(ctx, tenantID, taskID, result)
}

func (w *tenantWitnessAgentService) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []orchestration.Artifact) error {
	w.witness(ctx)
	return w.fakeAgentService.SubmitTaskArtifacts(ctx, tenantID, taskID, artifacts)
}

func (w *tenantWitnessAgentService) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*orchestration.Artifact, error) {
	w.witness(ctx)
	return w.fakeAgentService.GetTaskArtifact(ctx, tenantID, taskID, name)
}

func (w *tenantWitnessAgentService) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]orchestration.Artifact, error) {
	w.witness(ctx)
	return w.fakeAgentService.ListTaskArtifacts(ctx, tenantID, taskID)
}

func (w *tenantWitnessAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	w.witness(ctx)
	return w.fakeAgentService.StartJob(ctx, req)
}

func (w *tenantWitnessAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	w.witness(ctx)
	return w.fakeAgentService.GetJobStatus(ctx, tenantID, jobID)
}

func (w *tenantWitnessAgentService) ListJobsPage(ctx context.Context, tenantID string, filter *orchestration.JobFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	w.witness(ctx)
	return w.fakeAgentService.ListJobsPage(ctx, tenantID, filter, page)
}

func (w *tenantWitnessAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	w.witness(ctx)
	return w.fakeAgentService.CancelJob(ctx, tenantID, jobID)
}

// SessionEventLog satisfies orchestration.SessionLogAccessor, which
// GetSessionConversation needs; the returned log records the tenant too.
func (w *tenantWitnessAgentService) SessionEventLog() orchestration.SessionEventLog {
	return &tenantWitnessLog{SessionEventLog: w.log, owner: w}
}

// tenantWitnessLog records the tenant in the context of each GetMeta and
// delegates to the wrapped log.
type tenantWitnessLog struct {
	orchestration.SessionEventLog

	owner *tenantWitnessAgentService
}

func (l *tenantWitnessLog) GetMeta(ctx context.Context, tenantID, sessionID string) (*orchestration.SessionMeta, error) {
	l.owner.witness(ctx)
	return l.SessionEventLog.GetMeta(ctx, tenantID, sessionID)
}

// TestEmbeddedOrchestrator_StampsTheTenantIntoTheContext proves every embedded
// orchestrator handler whose request carries a tenant reaches the orchestration
// service with that tenant in the context. The SDK client hands in a bare
// context, and the tenant-routed KV resolves its backend from the context, so a
// handler that passes the bare context reads and writes the wrong store.
//
// AttentionList and AttentionResolve stamp the tenant the same way but have no
// row: they reach the concrete *service.SDLCService, which the mock returns nil.
func TestEmbeddedOrchestrator_StampsTheTenantIntoTheContext(t *testing.T) {
	const tenantID = "acme"

	rows := []struct {
		method string
		req    any
	}{
		{"GetSessionConversation", &orchestratorSDK.GetSessionConversationRequest{TenantID: tenantID, SessionID: "s1"}},
		{"CreateTask", &orchestratorSDK.CreateTaskRequest{TenantID: tenantID}},
		{"GetTask", &orchestratorSDK.GetTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"ListTasks", &orchestratorSDK.ListTasksRequest{TenantID: tenantID}},
		{"ClaimTask", &orchestratorSDK.ClaimTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"BlockTask", &orchestratorSDK.BlockTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"ReadmitTask", &orchestratorSDK.ReadmitTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"DeleteTask", &orchestratorSDK.DeleteTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"CompleteTask", &orchestratorSDK.CompleteTaskRequest{TenantID: tenantID, TaskID: "t1"}},
		{"SubmitTaskArtifacts", &orchestratorSDK.SubmitTaskArtifactsRequest{TenantID: tenantID, TaskID: "t1"}},
		{"GetTaskArtifact", &orchestratorSDK.GetTaskArtifactRequest{TenantID: tenantID, TaskID: "t1", Name: "a1"}},
		{"ListTaskArtifacts", &orchestratorSDK.ListTaskArtifactsRequest{TenantID: tenantID, TaskID: "t1"}},
		{"StartJob", &orchestratorSDK.StartJobRequest{TenantID: tenantID}},
		{"GetJobStatus", &orchestratorSDK.GetJobStatusRequest{TenantID: tenantID, JobID: "j1"}},
		{"ListJobs", &orchestratorSDK.ListJobsRequest{TenantID: tenantID}},
		{"CancelJob", &orchestratorSDK.CancelJobRequest{TenantID: tenantID, JobID: "j1"}},
	}

	for _, row := range rows {
		t.Run(row.method, func(t *testing.T) {
			witness := &tenantWitnessAgentService{
				fakeAgentService: &fakeAgentService{},
				log:              orchestration.NewMemorySessionEventLog(),
			}
			tr, err := New(&orchMock{
				mockQuicBaaS: &mockQuicBaaS{},
				agent:        witness,
				proj:         &fakeProjectService{},
				admin:        &fakeFleetAdminService{},
			})
			require.NoError(t, err)

			// A BARE context, which is what the SDK client actually hands in.
			_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", row.method, row.req)
			require.NoError(t, err)

			require.NotEmpty(t, witness.seen, "the service was never called")
			for i, got := range witness.seen {
				assert.Equal(t, tenantID, got,
					"%s call %d reached the service with tenant %q in context, want %q: a tenant-routed "+
						"store resolves the backend from the context, so this call lands on the wrong store",
					row.method, i, got, tenantID)
			}
		})
	}
}
