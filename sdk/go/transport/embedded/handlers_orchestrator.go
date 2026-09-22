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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// =============================================================================
// Orchestrator — Session Operations
// =============================================================================

func handleOrchestratorSpawnSession(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SpawnSessionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SpawnSession",
			"*orchestrator.SpawnSessionRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "SpawnSession")
	}
	resp, err := agentSvc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend:      r.Backend,
		ProjectID:    r.ProjectID,
		Model:        r.Model,
		WorkDir:      r.WorkDir,
		Env:          r.Env,
		HostAffinity: r.HostAffinity,
		// Unset means the deployment's default, which is a durable session --
		// the same answer REST gives an omitted field, so the in-process
		// caller and the wire callers agree.
		Persistent: r.Persistent == nil || *r.Persistent,

		// Pooled provider account selection. AccountAuto is the only way to
		// reach the dynamic (least-usage) strategy.
		AccountID:       r.AccountID,
		AccountAuto:     r.AccountAuto,
		AccountStrategy: r.AccountStrategy,
		// AccountAllow is the second thing that authorizes rotation (the other
		// being AccountAuto); dropping it here would make an explicitly
		// enumerated pool unreachable in-process.
		AccountAllow: r.AccountAllow,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.SpawnSessionResponse{
		SessionID:   resp.SessionID,
		Backend:     resp.Backend,
		ContainerID: resp.ContainerID,
	}, nil
}

func handleOrchestratorPromptSession(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.PromptSessionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "PromptSession",
			"*orchestrator.PromptSessionRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "PromptSession")
	}
	svcReq := &orchestration.PromptSessionRequest{
		SessionID: r.SessionID,
		Prompt:    r.Prompt,
	}

	// A prompt naming a session is asynchronous even in-process: the embedded
	// transport must honour the same turn contract as the remote ones, or a
	// caller that switches transports would see the turn semantics change.
	if svcReq.SessionID != "" {
		ack, ackErr := agentSvc.PromptSessionAsync(ctx, svcReq)
		if ackErr != nil {
			return nil, ackErr
		}
		return orchestratorSDK.NewPromptSessionAccepted(ack.SessionID, ack.TurnID, ack.Sequence, ack.Model)
	}

	resp, err := agentSvc.PromptSession(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	// Flatten through the shared converter so the embedded transport applies
	// the same content and turn-completion rules as the remote transports.
	blocks := make([]orchestratorSDK.PromptContentBlock, 0, len(resp.Content))
	for _, block := range resp.Content {
		blocks = append(blocks, orchestratorSDK.PromptContentBlock{
			Type: block.Type,
			Text: block.Text,
		})
	}
	return orchestratorSDK.NewPromptSessionResponse(resp.SessionID, blocks, resp.Model, resp.StopReason)
}

func handleOrchestratorSessionStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SessionStatusRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SessionStatus",
			"*orchestrator.SessionStatusRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "SessionStatus")
	}
	resp, err := agentSvc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: r.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.SessionStatusResponse{
		SessionID:  resp.SessionID,
		Status:     string(resp.State),
		LastActive: resp.LastActivity,
	}, nil
}

// handleOrchestratorGetSessionConversation reads one page of a session's
// conversation in process, against the same session event log the daemon's
// remote transports read. The projection lives in the orchestration package,
// so the embedded caller and an HTTP caller fold the transcript identically
// rather than each having their own idea of what a turn is.
func handleOrchestratorGetSessionConversation(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetSessionConversationRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetSessionConversation",
			"*orchestrator.GetSessionConversationRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetSessionConversation")
	}
	accessor, ok := agentSvc.(orchestration.SessionLogAccessor)
	if !ok {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetSessionConversation")
	}
	page, err := orchestration.ReadSessionHistory(ctx, accessor.SessionEventLog(), &orchestration.SessionHistoryRequest{
		TenantID:  r.TenantID,
		SessionID: r.SessionID,
		Cursor:    r.Cursor,
		Limit:     r.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := &orchestratorSDK.GetSessionConversationResponse{
		SessionID:      page.SessionID,
		TenantID:       page.TenantID,
		Turns:          make([]*orchestratorSDK.SessionTurn, 0, len(page.Turns)),
		NextCursor:     page.NextCursor,
		OldestSequence: page.OldestSequence,
	}
	for _, turn := range page.Turns {
		out.Turns = append(out.Turns, embeddedSessionTurn(turn))
	}
	return out, nil
}

// embeddedSessionTurn converts one projected turn into the SDK shape.
func embeddedSessionTurn(turn *orchestration.SessionTurn) *orchestratorSDK.SessionTurn {
	out := &orchestratorSDK.SessionTurn{
		Sequence:       turn.Sequence,
		SessionID:      turn.SessionID,
		TenantID:       turn.TenantID,
		Role:           orchestratorSDK.SessionTurnRole(turn.Role),
		Timestamp:      turn.Timestamp,
		Text:           turn.Text,
		TruncatedBytes: turn.TruncatedBytes,
		Model:          turn.Model,
		StopReason:     turn.StopReason,
	}
	for _, tool := range turn.Tools {
		out.Tools = append(out.Tools, orchestratorSDK.SessionToolUse{
			InvocationID: tool.InvocationID,
			Name:         tool.Name,
			IsError:      tool.IsError,
			ResultBytes:  tool.ResultBytes,
		})
	}
	if turn.Usage != nil {
		out.Usage = &orchestratorSDK.SessionTokenUsage{
			InputTokens:              turn.Usage.InputTokens,
			OutputTokens:             turn.Usage.OutputTokens,
			CacheCreationInputTokens: turn.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     turn.Usage.CacheReadInputTokens,
		}
	}
	return out
}

// handleOrchestratorGetSpendReport answers GetSpendReport in process, against
// the same consumption ledger the daemon's remote transports read. The
// roll-up lives in the orchestration package, so an embedded caller and an
// HTTP caller cannot disagree about what a project cost.
func handleOrchestratorGetSpendReport(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetSpendReportRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetSpendReport",
			"*orchestrator.GetSpendReportRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetSpendReport")
	}
	accessor, ok := agentSvc.(orchestration.SpendLedgerAccessor)
	if !ok {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetSpendReport")
	}
	// STAMP THE TENANT INTO THE CONTEXT, exactly as the gRPC handler does
	// (pkg/api/grpc/server_spend.go). The durable orchestration KV is
	// tenant-routed: pkg/server/tenant_kvstore.go resolves the backend from the
	// CONTEXT tenant, not from the request body. A read carrying the tenant only
	// in the body therefore resolves against whatever store the caller's context
	// happens to name -- the wrong one, or none.
	//
	// The embedded transport is where this bites hardest: the SDK client does not
	// stamp a tenant (sdk/go/client/orchestrator has no ContextWithTenant), so the
	// outer ctx is routinely bare. This is the go-quicbaas#820 class, which this
	// same pull request closed for gRPC and left open here.
	//
	// It also arms AccountStore's scopeCheck, which can only ever refuse more
	// strictly and never broaden access.
	tenantCtx := orchestration.ContextWithTenant(ctx, r.TenantID)

	report, err := orchestration.ReadSpendReport(tenantCtx, accessor.ConsumptionLedger(), accessor.AccountStore(),
		&orchestration.SpendReportRequest{
			TenantID:  r.TenantID,
			ProjectID: r.ProjectID,
			TaskID:    r.TaskID,
			JobID:     r.JobID,
			SessionID: r.SessionID,
			Round:     r.Round,
			RoundSet:  r.RoundSet,
		})
	if err != nil {
		return nil, err
	}
	return embeddedSpendReport(report), nil
}

// embeddedSpendReport converts the projected report into the SDK shape.
func embeddedSpendReport(report *orchestration.SpendReport) *orchestratorSDK.GetSpendReportResponse {
	out := &orchestratorSDK.GetSpendReportResponse{
		Level:        orchestratorSDK.SpendLevel(report.Level),
		TenantID:     report.TenantID,
		ProjectID:    report.ProjectID,
		TaskID:       report.TaskID,
		JobID:        report.JobID,
		SessionID:    report.SessionID,
		Round:        report.Round,
		Entries:      report.Entries,
		InputTokens:  report.InputTokens,
		OutputTokens: report.OutputTokens,
		TotalTokens:  report.TotalTokens,
		CostMicros:   report.CostMicros,
		CostMeasured: report.CostMeasured,
		Successes:    report.Successes,
		Failures:     report.Failures,
		FirstAt:      report.FirstAt,
		LastAt:       report.LastAt,
		ChildLevel:   orchestratorSDK.SpendLevel(report.ChildLevel),
	}
	for _, account := range report.Accounts {
		out.Accounts = append(out.Accounts, embeddedSpendAccount(account))
	}
	for _, model := range report.Models {
		out.Models = append(out.Models, orchestratorSDK.SpendModel{
			Engine:      model.Engine,
			Model:       model.Model,
			TotalTokens: model.TotalTokens,
		})
	}
	for _, child := range report.Children {
		out.Children = append(out.Children, orchestratorSDK.SpendChild{
			ID:           child.ID,
			Level:        orchestratorSDK.SpendLevel(child.Level),
			Unattributed: child.Unattributed,
			TotalTokens:  child.TotalTokens,
		})
	}
	return out
}

// embeddedSpendAccount converts one account's contribution and allowance.
func embeddedSpendAccount(account orchestration.SpendAccount) orchestratorSDK.SpendAccount {
	out := orchestratorSDK.SpendAccount{
		AccountID:    account.AccountID,
		DisplayName:  account.DisplayName,
		Provider:     account.Provider,
		TotalTokens:  account.TotalTokens,
		PolicyStatus: orchestratorSDK.SpendPolicyStatus(account.PolicyStatus),
	}
	for _, window := range account.Windows {
		out.Windows = append(out.Windows, orchestratorSDK.SpendWindowStatus{
			Name:            window.Name,
			Size:            int64(window.Size),
			MaxTokens:       window.MaxTokens,
			UsedTokens:      window.UsedTokens,
			RemainingTokens: window.RemainingTokens,
			Exhausted:       window.Exhausted,
			FreesAt:         window.FreesAt,
		})
	}
	if account.Breaker != nil {
		out.Breaker = &orchestratorSDK.SpendBreakerVerdict{
			State:         string(account.Breaker.State),
			Failures:      account.Breaker.Failures,
			Successes:     account.Breaker.Successes,
			LastFailureAt: account.Breaker.LastFailureAt,
			LastSuccessAt: account.Breaker.LastSuccessAt,
			OpenUntil:     account.Breaker.OpenUntil,
		}
	}
	return out
}

func handleOrchestratorCloseSession(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.CloseSessionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "CloseSession",
			"*orchestrator.CloseSessionRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "CloseSession")
	}
	resp, err := agentSvc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: r.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.CloseSessionResponse{
		SessionID: resp.SessionID,
		Closed:    true,
	}, nil
}

// handleOrchestratorRotateSessionAccount moves a session onto a different
// pooled provider account. The rotation verb is not on AgentService — the
// account pool is optional — so it is reached through the same capability
// narrowing every other transport uses.
func handleOrchestratorRotateSessionAccount(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.RotateSessionAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "RotateSessionAccount",
			"*orchestrator.RotateSessionAccountRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "RotateSessionAccount")
	}
	rotator, err := orchestration.RotationServiceFrom(agentSvc, "embedded")
	if err != nil {
		return nil, err
	}
	result, err := rotator.RotateSessionAccount(ctx, &orchestration.RotateSessionAccountRequest{
		SessionID:    r.SessionID,
		Reason:       r.Reason,
		HoldOutgoing: r.HoldOutgoing,
		AvailableAt:  r.AvailableAt,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.RotateSessionAccountResponse{
		SessionID:         result.SessionID,
		PreviousAccountID: result.PreviousAccountID,
		AccountID:         result.AccountID,
		Strategy:          result.Strategy,
		Mode:              string(result.Mode),
	}, nil
}

func handleOrchestratorListSessions(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListSessionsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListSessions",
			"*orchestrator.ListSessionsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListSessions")
	}
	resp, err := agentSvc.ListSessions(ctx, &orchestration.ListSessionsRequest{
		Backend:   r.Backend,
		Status:    orchestration.SessionState(r.Status),
		ProjectID: r.ProjectID,
		JobID:     r.JobID,
		Runner:    r.Runner,
		AgentID:   r.AgentID,
		Page:      pageFromSDK(r.Params),
	})
	if err != nil {
		return nil, err
	}
	sessions := make([]*orchestratorSDK.SessionStatusResponse, len(resp.Sessions))
	for i, s := range resp.Sessions {
		sessions[i] = &orchestratorSDK.SessionStatusResponse{
			SessionID:  s.SessionID,
			Backend:    s.Backend,
			Model:      s.Model,
			Status:     string(s.State),
			CreatedAt:  s.CreatedAt,
			LastActive: s.LastActivity,
			JobID:      s.JobID,
			Runner:     s.Runner,
			AgentID:    s.AgentID,
		}
	}
	return &orchestratorSDK.ListSessionsResponse{
		Sessions: sessions,
		Count:    len(sessions),
		Envelope: page.Envelope{
			Facets:     resp.Facets,
			Total:      resp.Total,
			Limit:      resp.Limit,
			Offset:     resp.Offset,
			NextOffset: resp.NextOffset,
		},
	}, nil
}

// =============================================================================
// Orchestrator — Task Operations
// =============================================================================

func handleOrchestratorCreateTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.CreateTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "CreateTask",
			"*orchestrator.CreateTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "CreateTask")
	}
	task, err := agentSvc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:      r.TenantID,
		TeamName:      r.TeamName,
		Subject:       r.Subject,
		Description:   r.Description,
		AssignedTo:    r.AssignedTo,
		Dependencies:  r.Dependencies,
		Metadata:      r.Metadata,
		ProjectID:     r.ProjectID,
		AutoSpawn:     r.AutoSpawn,
		Backend:       r.Backend,
		GpuRequired:   r.GpuRequired,
		HostAffinity:  r.HostAffinity,
		Source:        taskSourceFromSDK(r.Source),
		Priority:      r.Priority,
		AttemptBudget: r.AttemptBudget,
		Runner:        r.Runner,
	})
	if err != nil {
		return nil, err
	}
	return convertTask(task), nil
}

func handleOrchestratorGetTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetTask",
			"*orchestrator.GetTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetTask")
	}
	task, err := agentSvc.GetTask(ctx, r.TenantID, r.TaskID)
	if err != nil {
		return nil, err
	}
	return convertTask(task), nil
}

func handleOrchestratorListTasks(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListTasksRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListTasks",
			"*orchestrator.ListTasksRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListTasks")
	}
	// A STATUS THE ENGINE DOES NOT HAVE IS REFUSED IN-PROCESS TOO, because an
	// embedded caller must meet the same contract a remote one does: a typo
	// that reads back as an empty page over REST must not read back as an
	// empty page for a different reason here.
	statuses, err := orchestration.TaskStatusesFrom("statuses", r.Statuses)
	if err != nil {
		return nil, err
	}

	tasks, err := agentSvc.ListTasksPage(ctx, r.TenantID, &orchestration.TaskFilter{
		Status:     orchestration.TaskStatus(r.Status),
		TeamName:   r.TeamName,
		AssignedTo: r.AssignedTo,
		ProjectID:  r.ProjectID,
		Statuses:   statuses,
		AutoSpawn:  r.AutoSpawn,
		AgentID:    r.AgentID,
		JobID:      r.JobID,
	}, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	sdkTasks := make([]*orchestratorSDK.Task, len(tasks.Items))
	for i, t := range tasks.Items {
		sdkTasks[i] = convertTask(t)
	}
	return &orchestratorSDK.ListTasksResponse{
		Tasks:    sdkTasks,
		Count:    len(sdkTasks),
		Envelope: envelopeFromPage(tasks),
	}, nil
}

func handleOrchestratorClaimTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ClaimTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ClaimTask",
			"*orchestrator.ClaimTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ClaimTask")
	}
	if err := agentSvc.ClaimTask(ctx, r.TenantID, r.TaskID, r.AgentID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.ClaimTaskResponse{
		TaskID:  r.TaskID,
		AgentID: r.AgentID,
		Claimed: true,
	}, nil
}

func handleOrchestratorBlockTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.BlockTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "BlockTask",
			"*orchestrator.BlockTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "BlockTask")
	}
	if err := agentSvc.BlockTask(ctx, r.TenantID, r.TaskID, orchestration.TaskBlockKind(r.Kind), r.Reason); err != nil {
		return nil, err
	}
	return &orchestratorSDK.BlockTaskResponse{
		TaskID:  r.TaskID,
		Kind:    r.Kind,
		Blocked: true,
	}, nil
}

func handleOrchestratorReadmitTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ReadmitTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ReadmitTask",
			"*orchestrator.ReadmitTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ReadmitTask")
	}
	if err := agentSvc.ReadmitTask(ctx, r.TenantID, r.TaskID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.ReadmitTaskResponse{
		TaskID:     r.TaskID,
		Readmitted: true,
	}, nil
}

func handleOrchestratorDeleteTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.DeleteTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "DeleteTask",
			"*orchestrator.DeleteTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "DeleteTask")
	}
	if err := agentSvc.DeleteTask(ctx, r.TenantID, r.TaskID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.DeleteTaskResponse{
		TaskID:  r.TaskID,
		Deleted: true,
	}, nil
}

func handleOrchestratorCompleteTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.CompleteTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "CompleteTask",
			"*orchestrator.CompleteTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "CompleteTask")
	}
	var result *orchestration.TaskResult
	if r.Result != nil {
		result = &orchestration.TaskResult{
			Output:   r.Result.Output,
			Files:    r.Result.Files,
			Metadata: r.Result.Metadata,
		}
	}
	if err := agentSvc.CompleteTask(ctx, r.TenantID, r.TaskID, result); err != nil {
		return nil, err
	}
	return &orchestratorSDK.CompleteTaskResponse{
		TaskID:    r.TaskID,
		Completed: true,
	}, nil
}

// =============================================================================
// Orchestrator — Task Artifact Operations
// =============================================================================

func handleOrchestratorSubmitTaskArtifacts(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SubmitTaskArtifactsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SubmitTaskArtifacts",
			"*orchestrator.SubmitTaskArtifactsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "SubmitTaskArtifacts")
	}
	artifacts := make([]orchestration.Artifact, len(r.Artifacts))
	for i, a := range r.Artifacts {
		artifacts[i] = orchestration.Artifact{
			Kind:     orchestration.ArtifactKind(a.Kind),
			Name:     a.Name,
			MIMEType: a.MIMEType,
			Content:  a.Content,
			Size:     a.Size,
			Ref:      artifactRefToDomain(a.Ref),
			Metadata: a.Metadata,
		}
	}
	if err := agentSvc.SubmitTaskArtifacts(ctx, r.TenantID, r.TaskID, artifacts); err != nil {
		return nil, err
	}
	return &orchestratorSDK.SubmitTaskArtifactsResponse{
		TaskID: r.TaskID,
		Stored: len(artifacts),
	}, nil
}

func handleOrchestratorGetTaskArtifact(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetTaskArtifactRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetTaskArtifact",
			"*orchestrator.GetTaskArtifactRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetTaskArtifact")
	}
	artifact, err := agentSvc.GetTaskArtifact(ctx, r.TenantID, r.TaskID, r.Name)
	if err != nil {
		return nil, err
	}
	return artifactToSDK(*artifact, true), nil
}

func handleOrchestratorListTaskArtifacts(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListTaskArtifactsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListTaskArtifacts",
			"*orchestrator.ListTaskArtifactsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListTaskArtifacts")
	}
	artifacts, err := agentSvc.ListTaskArtifacts(ctx, r.TenantID, r.TaskID)
	if err != nil {
		return nil, err
	}
	sdkArtifacts := make([]*orchestratorSDK.TaskArtifact, len(artifacts))
	for i, a := range artifacts {
		sdkArtifacts[i] = artifactToSDK(a, false)
	}
	return &orchestratorSDK.ListTaskArtifactsResponse{
		TaskID:    r.TaskID,
		Artifacts: sdkArtifacts,
		Count:     len(sdkArtifacts),
	}, nil
}

// =============================================================================
// Orchestrator — Agent Registry Operations
// =============================================================================

func handleOrchestratorRegisterAgent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.RegisterAgentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "RegisterAgent",
			"*orchestrator.RegisterAgentRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "RegisterAgent")
	}
	resp, err := agentSvc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  r.TenantID,
		Name:      r.Name,
		Host:      r.Host,
		AgentType: r.AgentType,
		Backend:   r.Backend,
		Model:     r.Model,
		WorkDir:   r.WorkDir,
		Metadata:  r.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.RegisterAgentResponse{
		Agent: convertAgentRegistration(resp.Agent),
	}, nil
}

func handleOrchestratorGetAgent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetAgentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetAgent",
			"*orchestrator.GetAgentRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetAgent")
	}
	agent, err := agentSvc.GetAgent(ctx, r.TenantID, r.AgentID)
	if err != nil {
		return nil, err
	}
	return convertAgentRegistration(agent), nil
}

func handleOrchestratorListAgents(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListAgentsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListAgents",
			"*orchestrator.ListAgentsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListAgents")
	}
	agents, err := agentSvc.ListAgentsPage(ctx, r.TenantID, &orchestration.AgentFilter{
		Host:      r.Host,
		Status:    orchestration.AgentStatus(r.Status),
		AgentType: r.AgentType,
		Backend:   r.Backend,
		Model:     r.Model,
	}, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	sdkAgents := make([]*orchestratorSDK.AgentRegistration, len(agents.Items))
	for i, a := range agents.Items {
		sdkAgents[i] = convertAgentRegistration(a)
	}
	return &orchestratorSDK.ListAgentsResponse{
		Agents:   sdkAgents,
		Count:    len(sdkAgents),
		Envelope: envelopeFromPage(agents),
	}, nil
}

func handleOrchestratorRemoveAgent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.RemoveAgentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "RemoveAgent",
			"*orchestrator.RemoveAgentRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "RemoveAgent")
	}
	if err := agentSvc.RemoveAgent(ctx, r.TenantID, r.AgentID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.RemoveAgentResponse{AgentID: r.AgentID, Removed: true}, nil
}

func handleOrchestratorAgentHeartbeat(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.AgentHeartbeatRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "AgentHeartbeat",
			"*orchestrator.AgentHeartbeatRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "AgentHeartbeat")
	}
	// The SDK heartbeat request carries no served-backend list, so it observes
	// nothing and reconciles nothing.
	if err := agentSvc.AgentHeartbeat(ctx, r.TenantID, r.AgentID, nil); err != nil {
		return nil, err
	}
	return &orchestratorSDK.AgentHeartbeatResponse{AgentID: r.AgentID, Heartbeat: true}, nil
}

// =============================================================================
// Orchestrator — Agent Admin Operations
// =============================================================================

func handleOrchestratorDisableAgent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.DisableAgentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "DisableAgent",
			"*orchestrator.DisableAgentRequest", reflect.TypeOf(req).String())
	}
	adminSvc := svc.AgentAdmin()
	if adminSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "DisableAgent")
	}
	resp, err := adminSvc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: r.TenantID,
		AgentID:  r.AgentID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.DisableAgentResponse{
		AgentID:    resp.AgentID,
		Disabled:   resp.Disabled,
		DisabledAt: resp.DisabledAt,
		Timestamp:  resp.Timestamp,
	}, nil
}

func handleOrchestratorEnableAgent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.EnableAgentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "EnableAgent",
			"*orchestrator.EnableAgentRequest", reflect.TypeOf(req).String())
	}
	adminSvc := svc.AgentAdmin()
	if adminSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "EnableAgent")
	}
	resp, err := adminSvc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: r.TenantID,
		AgentID:  r.AgentID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.EnableAgentResponse{
		AgentID:   resp.AgentID,
		Enabled:   resp.Enabled,
		Timestamp: resp.Timestamp,
	}, nil
}

// =============================================================================
// Orchestrator — Job Operations
// =============================================================================

func handleOrchestratorStartJob(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.StartJobRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "StartJob",
			"*orchestrator.StartJobRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "StartJob")
	}
	resp, err := agentSvc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:      r.TenantID,
		Description:   r.Description,
		LeadBackend:   r.LeadBackend,
		LeadModel:     r.LeadModel,
		LeadAgentID:   r.LeadAgentID,
		ProjectID:     r.ProjectID,
		WorkflowName:  r.WorkflowName,
		WorkflowFile:  r.WorkflowFile,
		Runner:        r.Runner,
		MaxIterations: r.MaxIterations,
		PollInterval:  r.PollInterval,
		TaskID:        r.TaskID,
		Attempt:       r.Attempt,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.StartJobResponse{
		JobID:     resp.JobID,
		Timestamp: resp.Timestamp,
	}, nil
}

func handleOrchestratorGetJobStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetJobStatusRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetJobStatus",
			"*orchestrator.GetJobStatusRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetJobStatus")
	}
	status, err := agentSvc.GetJobStatus(ctx, r.TenantID, r.JobID)
	if err != nil {
		return nil, err
	}
	return convertJobStatus(status), nil
}

func handleOrchestratorListJobs(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListJobsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListJobs",
			"*orchestrator.ListJobsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListJobs")
	}
	states, err := orchestration.JobStatesFrom("statuses", r.Statuses)
	if err != nil {
		return nil, err
	}

	jobs, err := agentSvc.ListJobsPage(ctx, r.TenantID, &orchestration.JobFilter{
		TaskID:    r.TaskID,
		Runner:    r.Runner,
		ProjectID: r.ProjectID,
		AgentID:   r.AgentID,
		Statuses:  states,
		JobIDs:    r.JobIDs,
	}, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	sdkSummaries := make([]orchestratorSDK.JobSummary, len(jobs.Items))
	for i, s := range jobs.Items {
		sdkSummaries[i] = orchestratorSDK.JobSummary{
			JobID:       s.JobID,
			State:       string(s.State),
			Iteration:   s.Iteration,
			TaskCount:   s.TaskCount,
			Completed:   s.Completed,
			Failed:      s.Failed,
			Description: s.Description,
			StartedAt:   s.StartedAt,
			UpdatedAt:   s.UpdatedAt,
			Error:       s.Error,
			EventCount:  len(s.Events),
		}
	}
	return &orchestratorSDK.ListJobsResponse{Jobs: sdkSummaries, Envelope: envelopeFromPage(jobs)}, nil
}

func handleOrchestratorCancelJob(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.CancelJobRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "CancelJob",
			"*orchestrator.CancelJobRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "CancelJob")
	}
	if err := agentSvc.CancelJob(ctx, r.TenantID, r.JobID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.CancelJobResponse{JobID: r.JobID, Cancelled: true}, nil
}

func handleOrchestratorHealth(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "Health")
	}
	report, err := agentSvc.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}
	components := make(map[string]string, len(report.Results))
	for _, r := range report.Results {
		status := "unhealthy"
		if r.Healthy {
			status = "healthy"
		}
		components[r.SessionID] = status
	}
	healthy := report.Unhealthy == 0
	status := "healthy"
	if !healthy {
		status = "degraded"
	}
	return &orchestratorSDK.HealthCheckReport{
		Healthy:    healthy,
		Status:     status,
		Components: components,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func handleOrchestratorListBackends(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListBackends")
	}
	backends, err := agentSvc.ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	sdkBackends := make([]*orchestratorSDK.BackendInfo, len(backends))
	for i, b := range backends {
		sdkBackends[i] = &orchestratorSDK.BackendInfo{
			Name:      b.Name,
			Type:      b.Type,
			Available: true, // backends returned from ListBackends are registered and available
		}
	}
	return &orchestratorSDK.ListBackendsResponse{Backends: sdkBackends, Count: len(sdkBackends)}, nil
}

// handleOrchestratorListRunners answers ListRunners in process.
//
// The runner list is REGISTRY data — the same for every caller, so no tenant is
// derived — but it is still gated on the agent service being present, matching
// the REST handler: a node that runs no orchestrator drives no jobs, and
// answering "these are the runners" there would describe a capability the node
// does not have.
func handleOrchestratorListRunners(_ context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	if svc.Agent() == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListRunners")
	}
	registered := orchestration.JobRunners()
	runners := make([]*orchestratorSDK.JobRunnerInfo, 0, len(registered))
	for _, runner := range registered {
		specs := runner.States()
		states := make([]orchestratorSDK.JobStateSpec, 0, len(specs))
		for _, spec := range specs {
			states = append(states, orchestratorSDK.JobStateSpec{
				State:         string(spec.State),
				Label:         spec.Label,
				SessionBacked: spec.SessionBacked,
			})
		}
		runners = append(runners, &orchestratorSDK.JobRunnerInfo{Name: runner.Name(), States: states})
	}
	return &orchestratorSDK.ListJobRunnersResponse{Runners: runners, Count: len(runners)}, nil
}

func handleOrchestratorListBackendModels(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListBackendModelsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListBackendModels",
			"*orchestrator.ListBackendModelsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListBackendModels")
	}
	// Thread the agent ID as host affinity so the remote backend resolver can
	// forward the request to the daemon hosting the agent. Pass the tenant ID
	// through context so the resolver dispatches to the correct fleet.
	lookupCtx := ctx
	if r.AgentID != "" && r.AgentID != "any" {
		lookupCtx = orchestration.WithHostAffinity(lookupCtx, r.AgentID)
	}
	tenantID := r.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	lookupCtx = orchestration.ContextWithTenant(lookupCtx, tenantID)
	models, err := agentSvc.ListBackendModels(lookupCtx, r.BackendName)
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}
	return &orchestratorSDK.ListBackendModelsResponse{
		Backend: r.BackendName,
		Models:  models,
		Count:   len(models),
	}, nil
}

func handleOrchestratorDecomposeTask(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.DecomposeTaskRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "DecomposeTask",
			"*orchestrator.DecomposeTaskRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "DecomposeTask")
	}
	resp, err := agentSvc.DecomposeTask(ctx, &orchestration.DecomposeTaskRequest{
		Backend:    r.Backend,
		Model:      r.Model,
		Task:       r.Task,
		ProjectIDs: r.ProjectIDs,
	})
	if err != nil {
		return nil, err
	}
	sdkItems := make([]*orchestratorSDK.WorkItem, len(resp.WorkItems))
	for i, item := range resp.WorkItems {
		sdkItems[i] = &orchestratorSDK.WorkItem{
			ID:           item.ID,
			ProjectID:    item.ProjectID,
			Title:        item.Title,
			Description:  item.Description,
			Priority:     item.Priority,
			Dependencies: item.Dependencies,
		}
	}
	return &orchestratorSDK.DecomposeTaskResponse{
		WorkItems: sdkItems,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func handleOrchestratorSpawnChildJob(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SpawnChildJobRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SpawnChildJob",
			"*orchestrator.SpawnChildJobRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "SpawnChildJob")
	}
	tasks := make([]*orchestration.TaskSpec, len(r.Tasks))
	for i, t := range r.Tasks {
		tasks[i] = &orchestration.TaskSpec{
			Title:                t.Title,
			Description:          t.Description,
			Dependencies:         t.Dependencies,
			RequiredCapabilities: t.RequiredCapabilities,
		}
	}
	resp, err := agentSvc.SpawnChildJob(ctx, &orchestration.SpawnChildJobRequest{
		ParentJobID:        r.ParentJobID,
		CallerAgentID:      r.CallerAgentID,
		WorkflowID:         r.WorkflowID,
		ProjectID:          r.ProjectID,
		Tasks:              tasks,
		HostAffinity:       r.HostAffinity,
		GpuRequired:        r.GpuRequired,
		HostAffinityStrict: r.HostAffinityStrict,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.SpawnChildJobResponse{ChildJobID: resp.ChildJobID}, nil
}

// =============================================================================
// Project Operations
// =============================================================================

func handleProjectCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.CreateProjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "Create",
			"*project.CreateProjectRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "Create")
	}
	resp, err := projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:         r.TenantID,
		Name:             r.Name,
		Description:      r.Description,
		WorkspaceAgentID: r.WorkspaceAgentID,
		WorkspacePath:    r.WorkspacePath,
		HostAffinity:     r.HostAffinity,
		DefaultLeadAgent: r.DefaultLeadAgent,
		GitRepoURL:       r.GitRepoURL,
		GitDefaultBranch: r.GitDefaultBranch,
		TokenBudget:      r.TokenBudget,
		GPUMinutes:       r.GPUMinutes,
		MaxConcurrency:   r.MaxConcurrency,
		Metadata:         r.Metadata,

		DefaultWorkflow:     r.DefaultWorkflow,
		Autonomous:          r.Autonomous,
		ContextRepos:        contextReposToOrchestration(r.ContextRepos),
		GitAuthorName:       r.GitAuthorName,
		GitAuthorEmail:      r.GitAuthorEmail,
		GitSigningEnabled:   r.GitSigningEnabled,
		GitSigningAlgorithm: r.GitSigningAlgorithm,
		GitSigningKeyName:   r.GitSigningKeyName,
		GitSigningKeyScope:  r.GitSigningKeyScope,
		GitSigningKeyOwner:  r.GitSigningKeyOwner,
		GitSSHKeyName:       r.GitSSHKeyName,
		GitSSHKeyScope:      r.GitSSHKeyScope,
		GitSSHKeyOwner:      r.GitSSHKeyOwner,
	})
	if err != nil {
		return nil, err
	}
	return convertProjectToSDK(resp.Project), nil
}

func handleProjectGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.GetProjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "Get",
			"*project.GetProjectRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "Get")
	}
	resp, err := projectSvc.GetProject(ctx, &service.GetProjectRequest{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	return convertProjectToSDK(resp.Project), nil
}

func handleProjectGetByName(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.GetProjectByNameRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "GetByName",
			"*project.GetProjectByNameRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "GetByName")
	}
	resp, err := projectSvc.GetProjectByName(ctx, &service.GetProjectByNameRequest{
		TenantID: r.TenantID,
		Name:     r.Name,
	})
	if err != nil {
		return nil, err
	}
	return convertProjectToSDK(resp.Project), nil
}

func handleProjectUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.UpdateProjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "Update",
			"*project.UpdateProjectRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "Update")
	}
	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:         r.TenantID,
		ProjectID:        r.ProjectID,
		Name:             r.Name,
		Description:      r.Description,
		ClearDescription: r.ClearDescription,
		WorkspaceAgentID: r.WorkspaceAgentID,
		WorkspacePath:    r.WorkspacePath,
		HostAffinity:     r.HostAffinity,
		DefaultLeadAgent: r.DefaultLeadAgent,
		GitRepoURL:       r.GitRepoURL,
		GitDefaultBranch: r.GitDefaultBranch,
		GitCurrentSHA:    r.GitCurrentSHA,
		AgentRoles:       sdkAgentRolesToOrchestration(r.AgentRoles),
		TokenBudget:      r.TokenBudget,
		GPUMinutes:       r.GPUMinutes,
		MaxConcurrency:   r.MaxConcurrency,
		Metadata:         r.Metadata,

		DefaultWorkflow:      r.DefaultWorkflow,
		ClearDefaultWorkflow: r.ClearDefaultWorkflow,
		Autonomous:           r.Autonomous,
		ContextRepos:         contextReposToOrchestration(r.ContextRepos),
		ClearContextRepos:    r.ClearContextRepos,
		GitAuthorName:        r.GitAuthorName,
		GitAuthorEmail:       r.GitAuthorEmail,
		GitSigningEnabled:    r.GitSigningEnabled,
		GitSigningAlgorithm:  r.GitSigningAlgorithm,
		GitSigningKeyName:    r.GitSigningKeyName,
		GitSigningKeyScope:   r.GitSigningKeyScope,
		GitSigningKeyOwner:   r.GitSigningKeyOwner,
		GitSSHKeyName:        r.GitSSHKeyName,
		GitSSHKeyScope:       r.GitSSHKeyScope,
		GitSSHKeyOwner:       r.GitSSHKeyOwner,
	})
	if err != nil {
		return nil, err
	}
	return convertProjectToSDK(resp.Project), nil
}

func handleProjectDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.DeleteProjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "Delete",
			"*project.DeleteProjectRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "Delete")
	}
	_, err := projectSvc.DeleteProject(ctx, &service.DeleteProjectRequest{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	return &projectSDK.DeleteProjectResponse{Deleted: true}, nil
}

func handleProjectList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.ListProjectsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "List",
			"*project.ListProjectsRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "List")
	}
	resp, err := projectSvc.ListProjects(ctx, &service.ListProjectsRequest{
		TenantID:   r.TenantID,
		Autonomous: r.Autonomous,
		Page:       pageFromSDK(r.Params),
	})
	if err != nil {
		return nil, err
	}
	sdkProjects := make([]*projectSDK.ProjectInfo, len(resp.Projects))
	for i, p := range resp.Projects {
		sdkProjects[i] = convertProjectToSDK(p)
	}
	return &projectSDK.ListProjectsResponse{
		Projects: sdkProjects,
		Envelope: page.Envelope{
			Facets:     resp.Facets,
			Total:      resp.Total,
			Limit:      resp.Limit,
			Offset:     resp.Offset,
			NextOffset: resp.NextOffset,
		},
	}, nil
}

func handleProjectGrantAgentRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.GrantAgentRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "GrantAgentRole",
			"*project.GrantAgentRoleRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "GrantAgentRole")
	}
	resp, err := projectSvc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
		AgentID:   r.AgentID,
		Role:      orchestration.ProjectRole(r.Role),
	})
	if err != nil {
		return nil, err
	}
	return &projectSDK.GrantAgentRoleResponse{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Role:      projectSDK.ProjectRole(resp.Role),
		Timestamp: resp.Timestamp,
	}, nil
}

func handleProjectRevokeAgentRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.RevokeAgentRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "RevokeAgentRole",
			"*project.RevokeAgentRoleRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "RevokeAgentRole")
	}
	resp, err := projectSvc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
		AgentID:   r.AgentID,
	})
	if err != nil {
		return nil, err
	}
	return &projectSDK.RevokeAgentRoleResponse{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Timestamp: resp.Timestamp,
	}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

func convertTask(t *orchestration.Task) *orchestratorSDK.Task {
	if t == nil {
		return nil
	}
	var result *orchestratorSDK.TaskResult
	if t.Result != nil {
		result = &orchestratorSDK.TaskResult{
			Output:   t.Result.Output,
			Files:    t.Result.Files,
			Metadata: t.Result.Metadata,
		}
	}
	return &orchestratorSDK.Task{
		ID:           t.ID,
		TenantID:     t.TenantID,
		TeamName:     t.TeamName,
		Subject:      t.Subject,
		Description:  t.Description,
		Status:       string(t.Status),
		AssignedTo:   t.AssignedTo,
		Dependencies: t.Dependencies,
		CreatedAt:    t.CreatedAt,
		ClaimedAt:    t.ClaimedAt,
		CompletedAt:  t.CompletedAt,
		Version:      t.Version,
		Result:       result,
		Metadata:     t.Metadata,
		AutoSpawn:    t.AutoSpawn,
		SpawnedJobID: t.SpawnedJobID,
		SpawnedAt:    t.SpawnedAt,

		ProjectID:                    t.ProjectID,
		OutputArtifacts:              convertTaskArtifacts(t.OutputArtifacts),
		Source:                       taskSourceToSDK(t.Source),
		Priority:                     t.Priority,
		Attempts:                     t.Attempts,
		AttemptBudget:                t.AttemptBudget,
		EnvironmentFaults:            t.EnvironmentFaults,
		QuestionAttempts:             t.QuestionAttempts,
		ConsecutiveEnvironmentFaults: t.ConsecutiveEnvironmentFaults,
		RetryNotBefore:               t.RetryNotBefore,
		BlockedReason:                t.BlockedReason,
		BlockKind:                    string(t.BlockKind),
		StatusChangedAt:              t.StatusChangedAt,
		Runner:                       t.Runner,
		Routing: orchestratorSDK.TaskRouting{
			Backend:      t.Routing.Backend,
			HostAffinity: t.Routing.HostAffinity,
			GpuRequired:  t.Routing.GpuRequired,
		},
		DispatchReason: t.DispatchReason,
	}
}

// convertTaskArtifacts renders a task's output artifacts as metadata plus
// reference, the same shape ListTaskArtifacts returns: content is never
// inlined into a task listing.
func convertTaskArtifacts(artifacts []orchestration.Artifact) []*orchestratorSDK.TaskArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]*orchestratorSDK.TaskArtifact, len(artifacts))
	for i, a := range artifacts {
		out[i] = artifactToSDK(a, false)
	}
	return out
}

// artifactToSDK renders one orchestration Artifact in the SDK's shape.
//
// ONE CONVERTER, BECAUSE THREE COPIES DISAGREED. The three call sites each
// flattened Ref to its Path and dropped Kind and Metadata, so the embedded
// transport answered with a strictly smaller artifact than REST served for the
// same task — a parity gap that no test caught because each transport was
// checked against its own expectation.
//
// inlineContent is false for listings: a task's artifact list is metadata plus
// a reference, and inlining every payload into it is what the kv_key and file
// kinds exist to avoid.
func artifactToSDK(a orchestration.Artifact, inlineContent bool) *orchestratorSDK.TaskArtifact {
	out := &orchestratorSDK.TaskArtifact{
		Kind:     string(a.Kind),
		Name:     a.Name,
		MIMEType: a.MIMEType,
		Size:     a.Size,
		Metadata: a.Metadata,
	}
	if inlineContent {
		out.Content = a.Content
	}
	if a.Ref != (orchestration.ArtifactRef{}) {
		out.Ref = &orchestratorSDK.TaskArtifactRef{
			AgentID: a.Ref.AgentID, Path: a.Ref.Path, SHA: a.Ref.SHA, Branch: a.Ref.Branch,
		}
	}
	return out
}

// artifactRefToDomain is the inbound half; a nil ref is the zero ref.
func artifactRefToDomain(ref *orchestratorSDK.TaskArtifactRef) orchestration.ArtifactRef {
	if ref == nil {
		return orchestration.ArtifactRef{}
	}
	return orchestration.ArtifactRef{
		AgentID: ref.AgentID, Path: ref.Path, SHA: ref.SHA, Branch: ref.Branch,
	}
}

// taskSourceToSDK converts a task's provenance; nil stays nil.
func taskSourceToSDK(src *orchestration.TaskSourceRef) *orchestratorSDK.TaskSource {
	if src == nil {
		return nil
	}
	return &orchestratorSDK.TaskSource{Kind: string(src.Kind), Ref: src.Ref, URL: src.URL}
}

// taskSourceFromSDK converts a caller-supplied provenance; nil stays nil.
func taskSourceFromSDK(src *orchestratorSDK.TaskSource) *orchestration.TaskSourceRef {
	if src == nil {
		return nil
	}
	return &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKind(src.Kind), Ref: src.Ref, URL: src.URL}
}

// THE STATUS CONVERTERS THAT USED TO LIVE HERE ARE GONE. They turned any
// string into an enum value and left an unknown one to match nothing, which
// answered a typo with an empty page; the shared, VALIDATING
// orchestration.TaskStatusesFrom / JobStatesFrom refuse it instead, and an
// embedded caller now meets the same refusal a REST caller does.

func convertAgentRegistration(a *orchestration.AgentRegistration) *orchestratorSDK.AgentRegistration {
	if a == nil {
		return nil
	}
	return &orchestratorSDK.AgentRegistration{
		ID:           a.ID,
		TenantID:     a.TenantID,
		Name:         a.Name,
		Host:         a.Host,
		Backend:      a.Backend,
		Model:        a.Model,
		AgentType:    a.AgentType,
		Status:       string(a.Status),
		WorkDir:      a.WorkDir,
		LastSeen:     a.LastSeen,
		RegisteredAt: a.RegisteredAt,
		Version:      a.Version,
		Metadata:     a.Metadata,
	}
}

func convertJobStatus(s *orchestration.JobStatus) *orchestratorSDK.JobStatus {
	if s == nil {
		return nil
	}
	events := make([]orchestratorSDK.JobAuditEvent, len(s.Events))
	for i, e := range s.Events {
		events[i] = orchestratorSDK.JobAuditEvent{
			Timestamp: e.Timestamp,
			Type:      e.Type,
			Details:   e.Details,
		}
	}
	return &orchestratorSDK.JobStatus{
		JobID:       s.JobID,
		State:       string(s.State),
		Iteration:   s.Iteration,
		TaskCount:   s.TaskCount,
		Completed:   s.Completed,
		Failed:      s.Failed,
		Description: s.Description,
		StartedAt:   s.StartedAt,
		UpdatedAt:   s.UpdatedAt,
		Error:       s.Error,
		Events:      events,

		TaskID:        s.TaskID,
		Attempt:       s.Attempt,
		Node:          s.Node,
		HeartbeatAt:   s.HeartbeatAt,
		FailureClass:  s.FailureClass,
		LeadSessionID: s.LeadSessionID,
		SessionIDs:    s.SessionIDs,
		Stage:         s.Stage,
		Runner:        s.Runner,

		// Provenance — the in-process transport reads the domain record
		// directly, so it is the one converter with nothing standing between
		// it and the fields. Dropping them here would make the embedded
		// transport the only one that cannot render a job page.
		AgentID:        s.AgentID,
		Stages:         convertJobStages(s.Stages),
		Attempts:       convertJobAttempts(s.Attempts),
		AttemptBudget:  s.AttemptBudget,
		TerminalReason: string(s.TerminalReason),
		ReadmitAllowed: s.ReadmitAllowed,
		Delivery:       convertJobDelivery(s.Delivery),
		Verification:   convertJobVerification(s.Verification),
	}
}

// convertJobStages converts the lifecycle moments the job entered.
func convertJobStages(stages []orchestration.JobStageRecord) []orchestratorSDK.JobStageRecord {
	if len(stages) == 0 {
		return nil
	}
	out := make([]orchestratorSDK.JobStageRecord, len(stages))
	for i, stage := range stages {
		out[i] = orchestratorSDK.JobStageRecord{
			Stage:     string(stage.Stage),
			EnteredAt: stage.EnteredAt,
		}
	}
	return out
}

// convertJobAttempts converts the attempts at the job's task.
func convertJobAttempts(attempts []orchestration.JobAttemptRecord) []orchestratorSDK.JobAttemptRecord {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]orchestratorSDK.JobAttemptRecord, len(attempts))
	for i, attempt := range attempts {
		out[i] = orchestratorSDK.JobAttemptRecord{
			Attempt: attempt.Attempt,
			JobID:   attempt.JobID,
			Current: attempt.Current,
		}
	}
	return out
}

// convertJobDelivery converts the git side of the attempt.
func convertJobDelivery(delivery *orchestration.JobDelivery) *orchestratorSDK.JobDelivery {
	if delivery == nil {
		return nil
	}
	var reviews []orchestratorSDK.JobReviewRound
	if len(delivery.Reviews) > 0 {
		reviews = make([]orchestratorSDK.JobReviewRound, len(delivery.Reviews))
		for i, round := range delivery.Reviews {
			reviews[i] = orchestratorSDK.JobReviewRound{
				Round:       round.Round,
				Reviewer:    round.Reviewer,
				Verdict:     round.Verdict,
				HeadSHA:     round.HeadSHA,
				Findings:    round.Findings,
				SubmittedAt: round.SubmittedAt,
			}
		}
	}
	return &orchestratorSDK.JobDelivery{
		Branch:            delivery.Branch,
		HeadSHA:           delivery.HeadSHA,
		PullRequestNumber: delivery.PullRequestNumber,
		PullRequestURL:    delivery.PullRequestURL,
		PullRequestState:  delivery.PullRequestState,
		Reviews:           reviews,
	}
}

// convertJobVerification converts the last validator pipeline's results.
func convertJobVerification(results []orchestration.ValidationResult) []orchestratorSDK.JobVerificationResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]orchestratorSDK.JobVerificationResult, len(results))
	for i, result := range results {
		out[i] = orchestratorSDK.JobVerificationResult{
			Name:    result.Name,
			Passed:  result.Passed,
			Message: result.Message,
			Details: result.Details,
		}
	}
	return out
}

// contextReposToOrchestration converts the SDK context repository list to the
// domain type, preserving nil so an update can tell "unchanged" from "empty".
func contextReposToOrchestration(repos []projectSDK.ContextRepo) []orchestration.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]orchestration.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, orchestration.ContextRepo{Name: repo.Name, RepoURL: repo.RepoURL, Ref: repo.Ref})
	}
	return out
}

// contextReposToSDK converts the domain context repository list to the SDK
// type, preserving nil.
func contextReposToSDK(repos []orchestration.ContextRepo) []projectSDK.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]projectSDK.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, projectSDK.ContextRepo{Name: repo.Name, RepoURL: repo.RepoURL, Ref: repo.Ref})
	}
	return out
}

func convertProjectToSDK(p *orchestration.Project) *projectSDK.ProjectInfo {
	if p == nil {
		return nil
	}
	agentRoles := make(map[string]projectSDK.ProjectRole, len(p.Permissions.AgentRoles))
	for id, role := range p.Permissions.AgentRoles {
		agentRoles[id] = projectSDK.ProjectRole(role)
	}
	return &projectSDK.ProjectInfo{
		ID:               p.ID,
		TenantID:         p.TenantID,
		Name:             p.Name,
		Description:      p.Description,
		Workspace:        projectSDK.ProjectWorkspace{AgentID: p.Workspace.AgentID, Path: p.Workspace.Path},
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: p.DefaultLeadAgent,
		DefaultWorkflow:  p.DefaultWorkflow,
		Autonomous:       p.Autonomous,
		ContextRepos:     contextReposToSDK(p.ContextRepos),
		Git: projectSDK.ProjectGit{
			RepoURL:       p.Git.RepoURL,
			DefaultBranch: p.Git.DefaultBranch,
			CurrentSHA:    p.Git.CurrentSHA,
			Author:        projectSDK.GitAuthor{Name: p.Git.Author.Name, Email: p.Git.Author.Email},
			Signing: projectSDK.CommitSigning{
				Enabled:   p.Git.Signing.Enabled,
				Algorithm: p.Git.Signing.Algorithm,
				Key:       projectSDK.KMSKeyRef{Name: p.Git.Signing.Key.Name, Scope: p.Git.Signing.Key.Scope, Owner: p.Git.Signing.Key.Owner},
			},
			SSHKey: projectSDK.KMSKeyRef{Name: p.Git.SSHKey.Name, Scope: p.Git.SSHKey.Scope, Owner: p.Git.SSHKey.Owner},
		},
		Permissions: projectSDK.ProjectPermissions{
			AgentRoles: agentRoles,
		},
		Quotas: projectSDK.ProjectQuotas{
			TokenBudget:    p.Quotas.TokenBudget,
			GPUMinutes:     p.Quotas.GPUMinutes,
			MaxConcurrency: p.Quotas.MaxConcurrency,
		},
		Metadata:  p.Metadata,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

// sdkAgentRolesToOrchestration converts an SDK-level agent role map to its
// orchestration-domain equivalent. Returns nil when the input map is nil so
// the service-layer Update can distinguish "leave unchanged" from "clear all".
func sdkAgentRolesToOrchestration(roles map[string]projectSDK.ProjectRole) map[string]orchestration.ProjectRole {
	if roles == nil {
		return nil
	}
	out := make(map[string]orchestration.ProjectRole, len(roles))
	for id, role := range roles {
		out[id] = orchestration.ProjectRole(role)
	}
	return out
}
