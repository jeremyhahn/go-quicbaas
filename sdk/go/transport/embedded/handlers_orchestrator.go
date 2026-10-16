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
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
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
		AgentID:      r.AgentID,
		PoolID:       r.PoolID,
		Role:         orchestration.AgentRole(r.Role),
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
		PromptAppend: r.PromptAppend,
		Keep:         r.Keep,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.SpawnSessionResponse{
		SessionID:       resp.SessionID,
		Backend:         resp.Backend,
		Model:           resp.Model,
		ContainerID:     resp.ContainerID,
		ResolvedAgentID: resp.ResolvedAgentID,
		Failovers:       agentAttemptsFromDomain(resp.Failovers),
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
	return orchestratorSDK.NewPromptSessionResponse(resp.SessionID, promptContentBlocks(resp.Content), resp.Model, resp.StopReason)
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

	report, err := orchestration.ReadSpendReport(tenantCtx, orchestration.SpendSourcesOf(accessor),
		&orchestration.SpendReportRequest{
			TenantID:  r.TenantID,
			ProjectID: r.ProjectID,
			TaskID:    r.TaskID,
			JobID:     r.JobID,
			SessionID: r.SessionID,
			Round:     r.Round,
			RoundSet:  r.RoundSet,
			From:      embeddedInstant(r.From),
			To:        embeddedInstant(r.To),
			Host:      r.Host,
			Agent:     r.Agent,
		})
	if err != nil {
		return nil, err
	}
	return embeddedSpendReport(report), nil
}

// embeddedInstant reads an optional SDK instant: nil is the zero time, which
// the report reads as "no bound".
func embeddedInstant(at *time.Time) time.Time {
	if at == nil {
		return time.Time{}
	}
	return at.UTC()
}

// embeddedSpendReport converts the projected report into the SDK shape.
func embeddedSpendReport(report *orchestration.SpendReport) *orchestratorSDK.GetSpendReportResponse {
	out := &orchestratorSDK.GetSpendReportResponse{
		Level:       orchestratorSDK.SpendLevel(report.Level),
		TenantID:    report.TenantID,
		ProjectID:   report.ProjectID,
		TaskID:      report.TaskID,
		JobID:       report.JobID,
		SessionID:   report.SessionID,
		Round:       report.Round,
		ProjectName: report.ProjectName,
		SpendTotals: embeddedSpendTotals(report.ConsumptionTotals),
		Currency:    report.Currency,
		FirstAt:     report.FirstAt,
		LastAt:      report.LastAt,
		From:        report.From,
		To:          report.To,
		ChildLevel:  orchestratorSDK.SpendLevel(report.ChildLevel),

		SubscriptionFeeMicros: report.SubscriptionFeeMicros,
		ScopePolicyStatus:     orchestratorSDK.SpendPolicyStatus(report.ScopePolicyStatus),
		Host:                  report.Host,
	}
	for _, host := range report.Hosts {
		out.Hosts = append(out.Hosts, orchestratorSDK.SpendHost{
			Host:         host.Host,
			Unattributed: host.Unattributed,
			SpendTotals:  embeddedSpendTotals(host.ConsumptionTotals),
		})
	}
	for _, window := range report.ScopeWindows {
		out.ScopeWindows = append(out.ScopeWindows, embeddedSpendWindow(window))
	}
	out.Agent = report.Agent
	out.AgentPolicyStatus = orchestratorSDK.SpendPolicyStatus(report.AgentPolicyStatus)
	for _, agent := range report.Agents {
		out.Agents = append(out.Agents, orchestratorSDK.SpendAgent{
			AgentID:      agent.AgentID,
			Name:         agent.Name,
			Unattributed: agent.Unattributed,
			SpendTotals:  embeddedSpendTotals(agent.ConsumptionTotals),
		})
	}
	for _, window := range report.AgentWindows {
		out.AgentWindows = append(out.AgentWindows, embeddedSpendWindow(window))
	}
	for _, account := range report.Accounts {
		out.Accounts = append(out.Accounts, embeddedSpendAccount(account))
	}
	for _, model := range report.Models {
		out.Models = append(out.Models, orchestratorSDK.SpendModel{
			Engine:      model.Engine,
			Model:       model.Model,
			SpendTotals: embeddedSpendTotals(model.ConsumptionTotals),
		})
	}
	for _, child := range report.Children {
		out.Children = append(out.Children, orchestratorSDK.SpendChild{
			ID:           child.ID,
			Level:        orchestratorSDK.SpendLevel(child.Level),
			Unattributed: child.Unattributed,
			Name:         child.Name,
			SpendTotals:  embeddedSpendTotals(child.ConsumptionTotals),
		})
	}
	return out
}

// embeddedSpendTotals converts one set of consumption totals.
func embeddedSpendTotals(totals orchestration.ConsumptionTotals) orchestratorSDK.SpendTotals {
	return orchestratorSDK.SpendTotals{
		Entries:            totals.Entries,
		InputTokens:        totals.InputTokens,
		OutputTokens:       totals.OutputTokens,
		CacheReadTokens:    totals.CacheReadTokens,
		CacheWriteTokens:   totals.CacheWriteTokens,
		TotalTokens:        totals.TotalTokens,
		EquivalentMicros:   totals.EquivalentMicros,
		PricedEntries:      totals.PricedEntries,
		UnpricedEntries:    totals.UnpricedEntries,
		BilledMicros:       totals.BilledMicros,
		BilledEntries:      totals.BilledEntries,
		UnbilledEntries:    totals.UnbilledEntries,
		DurationMillis:     totals.DurationMillis,
		EnergyMilliWh:      totals.EnergyMilliWh,
		ElectricityEntries: totals.ElectricityEntries,
		Successes:          totals.Successes,
		Failures:           totals.Failures,

		MeasuredEnergyEntries: totals.MeasuredEnergyEntries,
	}
}

// embeddedSpendAccount converts one account's contribution and allowance.
func embeddedSpendAccount(account orchestration.SpendAccount) orchestratorSDK.SpendAccount {
	out := orchestratorSDK.SpendAccount{
		AccountID:    account.AccountID,
		DisplayName:  account.DisplayName,
		Provider:     account.Provider,
		SpendTotals:  embeddedSpendTotals(account.ConsumptionTotals),
		Billing:      orchestratorSDK.SpendAccountBilling{Mode: string(account.Billing.Mode), MonthlyFeeMicros: account.Billing.MonthlyFeeMicros},
		PolicyStatus: orchestratorSDK.SpendPolicyStatus(account.PolicyStatus),

		SubscriptionFeeMicros: account.SubscriptionFeeMicros,
	}
	for _, window := range account.Windows {
		out.Windows = append(out.Windows, embeddedSpendWindow(window))
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
			SessionID:   s.SessionID,
			Backend:     s.Backend,
			Model:       s.Model,
			Status:      string(s.State),
			ContainerID: s.ContainerID,
			CreatedAt:   s.CreatedAt,
			LastActive:  s.LastActivity,
			JobID:       s.JobID,
			Runner:      s.Runner,
			AgentID:     s.AgentID,
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
		PromptAppend:  r.PromptAppend,
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
		SourceKind: orchestration.TaskSourceKind(r.SourceKind),
		SourceRef:  r.SourceRef,
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

// convertAttentionItem converts an sdlc.AttentionItem to its SDK wire shape.
func convertAttentionItem(item *sdlc.AttentionItem) *orchestratorSDK.AttentionItem {
	if item == nil {
		return nil
	}
	actions := make([]string, len(item.Actions))
	for i, a := range item.Actions {
		actions[i] = string(a)
	}
	return &orchestratorSDK.AttentionItem{
		ID: item.ID, Kind: string(item.Kind),
		ProjectID: item.ProjectID, ProjectName: item.ProjectName,
		Subject: item.Subject, IssueRef: item.IssueRef, IssueURL: item.IssueURL,
		BlockKind: string(item.BlockKind), Condition: string(item.Condition),
		Question: item.Question, Recommendation: item.Recommendation, Truncated: item.Truncated,
		RulingPosted: item.RulingPosted, Message: item.Message, Severity: item.Severity,
		CreatedAt: item.CreatedAt, StatusChangedAt: item.StatusChangedAt, Attempts: item.Attempts,
		Actions: actions,
	}
}

func handleOrchestratorAttentionList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.AttentionListRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "AttentionList",
			"*orchestrator.AttentionListRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc := svc.SDLC()
	if sdlcSvc == nil || sdlcSvc.Attention() == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "AttentionList")
	}
	result, err := sdlcSvc.Attention().List(ctx, r.TenantID,
		sdlc.AttentionFilter{ProjectID: r.ProjectID, Kind: sdlc.AttentionKind(r.Kind)},
		pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	items := make([]*orchestratorSDK.AttentionItem, len(result.Items))
	for i, item := range result.Items {
		items[i] = convertAttentionItem(item)
	}
	return &orchestratorSDK.AttentionListResponse{
		Items: items, Count: len(items), Envelope: envelopeFromPage(result),
	}, nil
}

func handleOrchestratorAttentionResolve(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.AttentionResolveRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "AttentionResolve",
			"*orchestrator.AttentionResolveRequest", reflect.TypeOf(req).String())
	}
	sdlcSvc := svc.SDLC()
	if sdlcSvc == nil || sdlcSvc.Attention() == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "AttentionResolve")
	}
	item, err := sdlcSvc.Attention().Resolve(ctx, r.TenantID, r.TaskID, sdlc.AttentionResolution{
		Action: sdlc.AttentionAction(r.Action), Answer: r.Answer,
	})
	if err != nil {
		return nil, err
	}
	return convertAttentionItem(item), nil
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
	result, err := orchestration.ListTaskArtifactsPage(ctx, agentSvc, r.TenantID, r.TaskID, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	sdkArtifacts := make([]*orchestratorSDK.TaskArtifact, len(result.Items))
	for i, a := range result.Items {
		sdkArtifacts[i] = artifactToSDK(a, false)
	}
	return &orchestratorSDK.ListTaskArtifactsResponse{
		TaskID:    r.TaskID,
		Artifacts: sdkArtifacts,
		Count:     len(sdkArtifacts),
		Envelope:  envelopeFromPage(result),
	}, nil
}

// =============================================================================
// Orchestrator — Agent Registry Operations
// =============================================================================

func handleFleetRegister(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.RegisterFleetNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "RegisterFleetNode",
			"*orchestrator.RegisterFleetNodeRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "RegisterFleetNode")
	}
	resp, err := agentSvc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: r.TenantID,
		Name:     r.Name,
		Host:     r.Host,
		NodeType: r.NodeType,
		Backend:  r.Backend,
		Model:    r.Model,
		WorkDir:  r.WorkDir,
		Metadata: r.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.RegisterFleetNodeResponse{
		Node: convertFleetNode(resp.Node),
	}, nil
}

func handleFleetGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetFleetNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetFleetNode",
			"*orchestrator.GetFleetNodeRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetFleetNode")
	}
	agent, err := agentSvc.GetFleetNode(ctx, r.TenantID, r.NodeID)
	if err != nil {
		return nil, err
	}
	return convertFleetNode(agent), nil
}

func handleFleetList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListFleetNodesRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListFleetNodes",
			"*orchestrator.ListFleetNodesRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListFleetNodes")
	}
	agents, err := agentSvc.ListFleetNodesPage(ctx, r.TenantID, &orchestration.FleetNodeFilter{
		Host:     r.Host,
		Status:   orchestration.FleetNodeStatus(r.Status),
		NodeType: r.NodeType,
		Backend:  r.Backend,
		Model:    r.Model,
	}, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	sdkAgents := make([]*orchestratorSDK.FleetNode, len(agents.Items))
	for i, a := range agents.Items {
		sdkAgents[i] = convertFleetNode(a)
	}
	return &orchestratorSDK.ListFleetNodesResponse{
		Nodes:    sdkAgents,
		Count:    len(sdkAgents),
		Envelope: envelopeFromPage(agents),
	}, nil
}

func handleFleetRemove(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.RemoveFleetNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "RemoveFleetNode",
			"*orchestrator.RemoveFleetNodeRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "RemoveFleetNode")
	}
	if err := agentSvc.RemoveFleetNode(ctx, r.TenantID, r.NodeID); err != nil {
		return nil, err
	}
	return &orchestratorSDK.RemoveFleetNodeResponse{NodeID: r.NodeID, Removed: true}, nil
}

func handleFleetHeartbeat(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.FleetNodeHeartbeatRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "FleetNodeHeartbeat",
			"*orchestrator.FleetNodeHeartbeatRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "FleetNodeHeartbeat")
	}
	// The observation decodes through the same report a REST body does, so
	// presence and unread readings mean the same in-process as on the wire.
	var observed *orchestration.FleetNodeObservation
	if r.Observation != nil {
		decoded, err := orchestration.DecodeFleetHeartbeatReport(r.Observation)
		if err != nil {
			return nil, err
		}
		observed = decoded
	}
	if err := agentSvc.FleetNodeHeartbeat(ctx, r.TenantID, r.NodeID, observed); err != nil {
		return nil, err
	}
	return &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: r.NodeID, Heartbeat: true}, nil
}

// =============================================================================
// Orchestrator — Agent Admin Operations
// =============================================================================

func handleFleetDisable(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.DisableFleetNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "DisableFleetNode",
			"*orchestrator.DisableFleetNodeRequest", reflect.TypeOf(req).String())
	}
	adminSvc := svc.FleetAdmin()
	if adminSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "DisableFleetNode")
	}
	resp, err := adminSvc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: r.TenantID,
		NodeID:   r.NodeID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.DisableFleetNodeResponse{
		NodeID:     resp.NodeID,
		Disabled:   resp.Disabled,
		DisabledAt: resp.DisabledAt,
		Timestamp:  resp.Timestamp,
	}, nil
}

func handleFleetEnable(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.EnableFleetNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "EnableFleetNode",
			"*orchestrator.EnableFleetNodeRequest", reflect.TypeOf(req).String())
	}
	adminSvc := svc.FleetAdmin()
	if adminSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "EnableFleetNode")
	}
	resp, err := adminSvc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: r.TenantID,
		NodeID:   r.NodeID,
	})
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.EnableFleetNodeResponse{
		NodeID:    resp.NodeID,
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
		Lead:          agentRefToDomain(r.Lead),
		LeadAgentID:   r.LeadAgentID,
		ProjectID:     r.ProjectID,
		WorkflowName:  r.WorkflowName,
		WorkflowFile:  r.WorkflowFile,
		Runner:        r.Runner,
		MaxIterations: r.MaxIterations,
		PollInterval:  r.PollInterval,
		TaskID:        r.TaskID,
		Attempt:       r.Attempt,
		PromptAppend:  r.PromptAppend,
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
			TaskID:      s.TaskID,
			Attempt:     s.Attempt,
			Source:      taskSourceToSDK(s.Source),
		}
	}
	return &orchestratorSDK.ListJobsResponse{Jobs: sdkSummaries, Envelope: envelopeFromPage(jobs)}, nil
}

func handleOrchestratorListWorkflows(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListWorkflowsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListWorkflows",
			"*orchestrator.ListWorkflowsRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListWorkflows")
	}
	tenantID := r.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	workflowCtx := orchestration.ContextWithTenant(ctx, tenantID)
	workflows, err := agentSvc.ListWorkflowsPage(workflowCtx, &orchestration.WorkflowFilter{
		ProjectID: r.ProjectID,
		Mode:      orchestration.WorkflowMode(r.Mode),
	}, pageFromSDK(r.Params))
	if err != nil {
		return nil, err
	}
	items := make([]*orchestratorSDK.WorkflowDefinition, 0, len(workflows.Items))
	for _, workflow := range workflows.Items {
		items = append(items, workflowToSDK(workflow))
	}
	return &orchestratorSDK.ListWorkflowsResponse{Workflows: items, Count: len(items), Envelope: envelopeFromPage(workflows)}, nil
}

func workflowToSDK(workflow *orchestration.WorkflowDefinition) *orchestratorSDK.WorkflowDefinition {
	if workflow == nil {
		return nil
	}
	agents := make([]orchestratorSDK.AgentRoleSpec, 0, len(workflow.Agents))
	for _, agent := range workflow.Agents {
		agents = append(agents, orchestratorSDK.AgentRoleSpec{
			Role:            agent.Role,
			Backend:         agent.Backend,
			Model:           agent.Model,
			Capabilities:    append([]string(nil), agent.Capabilities...),
			MCPScopes:       append([]string(nil), agent.MCPScopes...),
			Count:           agent.Count,
			Env:             cloneStringMap(agent.Env),
			WorkspacePath:   agent.WorkspacePath,
			PermissionMode:  agent.PermissionMode,
			SkipPermissions: agent.SkipPermissions,
			Sandboxed:       agent.Sandboxed,
			ContainerImage:  agent.ContainerImage,
			HostAffinity:    agent.HostAffinity,
			NumCtx:          agent.NumCtx,
			PromptAppend:    agent.PromptAppend,
		})
	}
	reviewCriteria := make([]orchestratorSDK.ReviewCriterion, 0, len(workflow.Settings.ReviewCriteria))
	for _, criterion := range workflow.Settings.ReviewCriteria {
		reviewCriteria = append(reviewCriteria, orchestratorSDK.ReviewCriterion{
			Name:      criterion.Name,
			Validator: criterion.Validator,
			Config:    criterion.Config,
			Required:  criterion.Required,
			Phase:     string(criterion.Phase),
		})
	}
	return &orchestratorSDK.WorkflowDefinition{
		Name:        workflow.Name,
		Description: workflow.Description,
		Mode:        orchestratorSDK.WorkflowMode(workflow.Mode),
		Agents:      agents,
		Settings: orchestratorSDK.WorkflowSettings{
			MaxIterations:  workflow.Settings.MaxIterations,
			MaxParallelism: workflow.Settings.MaxParallelism,
			PollInterval:   workflow.Settings.PollInterval,
			TimeoutPerItem: workflow.Settings.TimeoutPerItem,
			TotalTimeout:   workflow.Settings.TotalTimeout,
			ReviewCriteria: reviewCriteria,
			Topology:       string(workflow.Settings.Topology),
			Isolation:      string(workflow.Settings.Isolation),
			PeerMessaging: orchestratorSDK.PeerMessagingSettings{
				MaxMessageSize: workflow.Settings.PeerMessaging.MaxMessageSize,
				MaxInboxSize:   workflow.Settings.PeerMessaging.MaxInboxSize,
				OverflowAction: workflow.Settings.PeerMessaging.OverflowAction,
				InboxTTL:       workflow.Settings.PeerMessaging.InboxTTL,
			},
			FileLocking: orchestratorSDK.FileLockSettings{
				LeaseTTL:          workflow.Settings.FileLocking.LeaseTTL,
				HeartbeatInterval: workflow.Settings.FileLocking.HeartbeatInterval,
				MaxRenewals:       workflow.Settings.FileLocking.MaxRenewals,
			},
			VectorDB: orchestratorSDK.VectorDBSettings{
				Enabled:        workflow.Settings.VectorDB.Enabled,
				IndexType:      workflow.Settings.VectorDB.IndexType,
				AutoIndex:      workflow.Settings.VectorDB.AutoIndex,
				Triggers:       append([]string(nil), workflow.Settings.VectorDB.Triggers...),
				GPUAccelerated: workflow.Settings.VectorDB.GPUAccelerated,
			},
			CostControls: orchestratorSDK.CostControlSettings{
				MaxTokensPerJob:   workflow.Settings.CostControls.MaxTokensPerJob,
				MaxTokensPerAgent: workflow.Settings.CostControls.MaxTokensPerAgent,
				BudgetAction:      workflow.Settings.CostControls.BudgetAction,
			},
			Recovery: orchestratorSDK.RecoverySettings{
				AutoRestart:    workflow.Settings.Recovery.AutoRestart,
				MaxRestarts:    workflow.Settings.Recovery.MaxRestarts,
				RestartBackoff: workflow.Settings.Recovery.RestartBackoff,
			},
			Verbosity: workflow.Settings.Verbosity,
			NumCtx:    workflow.Settings.NumCtx,
			Hooks:     workflow.Settings.Hooks,
		},
	}
}

// workflowFromSDK converts an SDK workflow definition into the orchestration
// definition the agent service stores. The tenant is carried on the context,
// so TenantID is left unset.
func workflowFromSDK(workflow *orchestratorSDK.WorkflowDefinition) *orchestration.WorkflowDefinition {
	agents := make([]orchestration.AgentRoleSpec, 0, len(workflow.Agents))
	for _, agent := range workflow.Agents {
		agents = append(agents, orchestration.AgentRoleSpec{
			Role:            agent.Role,
			Backend:         agent.Backend,
			Model:           agent.Model,
			Capabilities:    append([]string(nil), agent.Capabilities...),
			MCPScopes:       append([]string(nil), agent.MCPScopes...),
			Count:           agent.Count,
			Env:             cloneStringMap(agent.Env),
			WorkspacePath:   agent.WorkspacePath,
			PermissionMode:  agent.PermissionMode,
			SkipPermissions: agent.SkipPermissions,
			Sandboxed:       agent.Sandboxed,
			ContainerImage:  agent.ContainerImage,
			HostAffinity:    agent.HostAffinity,
			NumCtx:          agent.NumCtx,
			PromptAppend:    agent.PromptAppend,
		})
	}
	reviewCriteria := make([]orchestration.ReviewCriterion, 0, len(workflow.Settings.ReviewCriteria))
	for _, criterion := range workflow.Settings.ReviewCriteria {
		reviewCriteria = append(reviewCriteria, orchestration.ReviewCriterion{
			Name:      criterion.Name,
			Validator: criterion.Validator,
			Config:    criterion.Config,
			Required:  criterion.Required,
			Phase:     orchestration.CriterionPhase(criterion.Phase),
		})
	}
	return &orchestration.WorkflowDefinition{
		Name:        workflow.Name,
		Description: workflow.Description,
		Mode:        orchestration.WorkflowMode(workflow.Mode),
		Agents:      agents,
		Settings: orchestration.WorkflowSettings{
			MaxIterations:  workflow.Settings.MaxIterations,
			MaxParallelism: workflow.Settings.MaxParallelism,
			PollInterval:   workflow.Settings.PollInterval,
			TimeoutPerItem: workflow.Settings.TimeoutPerItem,
			TotalTimeout:   workflow.Settings.TotalTimeout,
			ReviewCriteria: reviewCriteria,
			Topology:       orchestration.CommunicationTopology(workflow.Settings.Topology),
			Isolation:      orchestration.WorkspaceIsolation(workflow.Settings.Isolation),
			PeerMessaging: orchestration.PeerMessagingSettings{
				MaxMessageSize: workflow.Settings.PeerMessaging.MaxMessageSize,
				MaxInboxSize:   workflow.Settings.PeerMessaging.MaxInboxSize,
				OverflowAction: workflow.Settings.PeerMessaging.OverflowAction,
				InboxTTL:       workflow.Settings.PeerMessaging.InboxTTL,
			},
			FileLocking: orchestration.FileLockSettings{
				LeaseTTL:          workflow.Settings.FileLocking.LeaseTTL,
				HeartbeatInterval: workflow.Settings.FileLocking.HeartbeatInterval,
				MaxRenewals:       workflow.Settings.FileLocking.MaxRenewals,
			},
			VectorDB: orchestration.VectorDBSettings{
				Enabled:        workflow.Settings.VectorDB.Enabled,
				IndexType:      workflow.Settings.VectorDB.IndexType,
				AutoIndex:      workflow.Settings.VectorDB.AutoIndex,
				Triggers:       append([]string(nil), workflow.Settings.VectorDB.Triggers...),
				GPUAccelerated: workflow.Settings.VectorDB.GPUAccelerated,
			},
			CostControls: orchestration.CostControlSettings{
				MaxTokensPerJob:   workflow.Settings.CostControls.MaxTokensPerJob,
				MaxTokensPerAgent: workflow.Settings.CostControls.MaxTokensPerAgent,
				BudgetAction:      workflow.Settings.CostControls.BudgetAction,
			},
			Recovery: orchestration.RecoverySettings{
				AutoRestart:    workflow.Settings.Recovery.AutoRestart,
				MaxRestarts:    workflow.Settings.Recovery.MaxRestarts,
				RestartBackoff: workflow.Settings.Recovery.RestartBackoff,
			},
			Verbosity: workflow.Settings.Verbosity,
			NumCtx:    workflow.Settings.NumCtx,
			Hooks:     workflow.Settings.Hooks,
		},
	}
}

// workflowTenantContext scopes ctx to tenantID, defaulting to the "default"
// tenant exactly as ListWorkflows does.
func workflowTenantContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		tenantID = "default"
	}
	return orchestration.ContextWithTenant(ctx, tenantID)
}

func handleOrchestratorCreateWorkflow(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.CreateWorkflowRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "CreateWorkflow",
			"*orchestrator.CreateWorkflowRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "CreateWorkflow")
	}
	if r.Definition == nil {
		return nil, orchestration.ErrInvalidInput
	}
	definition := workflowFromSDK(r.Definition)
	if err := agentSvc.RegisterWorkflow(workflowTenantContext(ctx, r.TenantID), definition); err != nil {
		return nil, err
	}
	return workflowToSDK(definition), nil
}

func handleOrchestratorGetWorkflow(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetWorkflowRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetWorkflow",
			"*orchestrator.GetWorkflowRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "GetWorkflow")
	}
	definition, err := agentSvc.GetWorkflow(workflowTenantContext(ctx, r.TenantID), r.Name)
	if err != nil {
		return nil, err
	}
	return workflowToSDK(definition), nil
}

func handleOrchestratorUpdateWorkflow(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.UpdateWorkflowRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "UpdateWorkflow",
			"*orchestrator.UpdateWorkflowRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "UpdateWorkflow")
	}
	if r.Definition == nil {
		return nil, orchestration.ErrInvalidInput
	}
	definition := workflowFromSDK(r.Definition)
	// The request name wins over the embedded name, as on REST and gRPC.
	definition.Name = r.Name
	if err := agentSvc.UpdateWorkflow(workflowTenantContext(ctx, r.TenantID), definition); err != nil {
		return nil, err
	}
	return workflowToSDK(definition), nil
}

func handleOrchestratorDeleteWorkflow(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.DeleteWorkflowRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "DeleteWorkflow",
			"*orchestrator.DeleteWorkflowRequest", reflect.TypeOf(req).String())
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "DeleteWorkflow")
	}
	if err := agentSvc.DeleteWorkflow(workflowTenantContext(ctx, r.TenantID), r.Name); err != nil {
		return nil, err
	}
	return nil, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
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
	r, ok := req.(*orchestratorSDK.ListBackendsRequest)
	if !ok && req != nil {
		if _, empty := req.(struct{}); !empty {
			return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListBackends",
				"*orchestrator.ListBackendsRequest", reflect.TypeOf(req).String())
		}
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListBackends")
	}
	var params page.Params
	if r != nil {
		params = r.Params
	}
	result, err := orchestration.ListBackendsPage(ctx, agentSvc, pageFromSDK(params))
	if err != nil {
		return nil, err
	}
	sdkBackends := make([]*orchestratorSDK.BackendInfo, len(result.Items))
	for i, b := range result.Items {
		sdkBackends[i] = &orchestratorSDK.BackendInfo{
			Name:         b.Name,
			Type:         b.Type,
			Available:    true,
			DefaultModel: b.DefaultModel,
		}
	}
	return &orchestratorSDK.ListBackendsResponse{Backends: sdkBackends, Count: len(sdkBackends), Envelope: envelopeFromPage(result)}, nil
}

// handleOrchestratorListRunners answers ListRunners in process.
//
// The runner list is REGISTRY data — the same for every caller, so no tenant is
// derived — but it is still gated on the agent service being present, matching
// the REST handler: a node that runs no orchestrator drives no jobs, and
// answering "these are the runners" there would describe a capability the node
// does not have.
func handleOrchestratorListRunners(_ context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.ListRunnersRequest)
	if !ok && req != nil {
		if _, empty := req.(struct{}); !empty {
			return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "ListRunners",
				"*orchestrator.ListRunnersRequest", reflect.TypeOf(req).String())
		}
	}
	if svc.Agent() == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "ListRunners")
	}
	var params page.Params
	if r != nil {
		params = r.Params
	}
	result, err := orchestration.ListJobRunnersPage(pageFromSDK(params))
	if err != nil {
		return nil, err
	}
	runners := make([]*orchestratorSDK.JobRunnerInfo, 0, len(result.Items))
	for _, runner := range result.Items {
		states := make([]orchestratorSDK.JobStateSpec, 0, len(runner.States))
		for _, spec := range runner.States {
			states = append(states, orchestratorSDK.JobStateSpec{
				State:         string(spec.State),
				Label:         spec.Label,
				SessionBacked: spec.SessionBacked,
			})
		}
		runners = append(runners, &orchestratorSDK.JobRunnerInfo{Name: runner.Name, States: states})
	}
	return &orchestratorSDK.ListJobRunnersResponse{Runners: runners, Count: len(runners), Envelope: envelopeFromPage(result)}, nil
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
	if r.NodeID != "" && r.NodeID != "any" {
		lookupCtx = orchestration.WithHostAffinity(lookupCtx, r.NodeID)
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
		TenantID:             r.TenantID,
		Name:                 r.Name,
		Description:          r.Description,
		WorkspaceFleetNodeID: r.WorkspaceFleetNodeID,
		WorkspacePath:        r.WorkspacePath,
		HostAffinity:         r.HostAffinity,
		DefaultLeadAgent:     agentRefToDomain(r.DefaultLeadAgent),
		GitRepoURL:           r.GitRepoURL,
		GitDefaultBranch:     r.GitDefaultBranch,
		Spend:                spendLimitsToOrchestration(r.Spend),
		GPUMinutes:           r.GPUMinutes,
		MaxConcurrency:       r.MaxConcurrency,
		Metadata:             r.Metadata,

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
		WorkspaceTier:       orchestration.WorkspaceTier(r.WorkspaceTier),
		WorkspaceTmpfsMB:    r.WorkspaceTmpfsMB,
		ContainerMemoryMB:   r.ContainerMemoryMB,
		SessionRotation:     orchestration.SessionRotationPolicy(r.SessionRotation),

		WorkspaceManaged:       r.WorkspaceManaged,
		GitCredentialSecretRef: r.GitCredentialSecretRef,
		GitCredentialUsername:  r.GitCredentialUsername,
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
	retirement, err := service.ParseGitSigningRetirement(r.GitSigningRetireKeyName,
		r.GitSigningRetireKeyScope, r.GitSigningRetireKeyOwner, r.GitSigningRetiredAt)
	if err != nil {
		return nil, err
	}
	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:             r.TenantID,
		ProjectID:            r.ProjectID,
		Name:                 r.Name,
		Description:          r.Description,
		ClearDescription:     r.ClearDescription,
		WorkspaceFleetNodeID: r.WorkspaceFleetNodeID,
		WorkspacePath:        r.WorkspacePath,
		HostAffinity:         r.HostAffinity,
		DefaultLeadAgent:     agentRefToDomain(r.DefaultLeadAgent),
		GitRepoURL:           r.GitRepoURL,
		GitDefaultBranch:     r.GitDefaultBranch,
		GitCurrentSHA:        r.GitCurrentSHA,
		FleetNodeRoles:       sdkFleetNodeRolesToOrchestration(r.FleetNodeRoles),
		Spend:                optionalSpendLimitsToOrchestration(r.Spend),
		GPUMinutes:           r.GPUMinutes,
		MaxConcurrency:       r.MaxConcurrency,
		Metadata:             r.Metadata,

		DefaultWorkflow:      r.DefaultWorkflow,
		ClearDefaultWorkflow: r.ClearDefaultWorkflow,
		Autonomous:           r.Autonomous,
		Parked:               r.Parked,
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
		GitSigningRetire:     retirement,
		WorkspaceTier:        orchestration.WorkspaceTier(r.WorkspaceTier),
		WorkspaceTmpfsMB:     r.WorkspaceTmpfsMB,
		ContainerMemoryMB:    r.ContainerMemoryMB,
		SessionRotation:      orchestration.SessionRotationPolicy(r.SessionRotation),
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

func handleProjectGrantFleetNodeRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.GrantFleetNodeRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "GrantFleetNodeRole",
			"*project.GrantFleetNodeRoleRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "GrantFleetNodeRole")
	}
	resp, err := projectSvc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    r.TenantID,
		ProjectID:   r.ProjectID,
		FleetNodeID: r.FleetNodeID,
		Role:        orchestration.ProjectRole(r.Role),
	})
	if err != nil {
		return nil, err
	}
	return &projectSDK.GrantFleetNodeRoleResponse{
		ProjectID:   resp.ProjectID,
		FleetNodeID: resp.FleetNodeID,
		Role:        projectSDK.ProjectRole(resp.Role),
		Timestamp:   resp.Timestamp,
	}, nil
}

func handleProjectRevokeFleetNodeRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*projectSDK.RevokeFleetNodeRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Project", "RevokeFleetNodeRole",
			"*project.RevokeFleetNodeRoleRequest", reflect.TypeOf(req).String())
	}
	projectSvc := svc.Project()
	if projectSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Project", "RevokeFleetNodeRole")
	}
	resp, err := projectSvc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    r.TenantID,
		ProjectID:   r.ProjectID,
		FleetNodeID: r.FleetNodeID,
	})
	if err != nil {
		return nil, err
	}
	return &projectSDK.RevokeFleetNodeRoleResponse{
		ProjectID:   resp.ProjectID,
		FleetNodeID: resp.FleetNodeID,
		Timestamp:   resp.Timestamp,
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
		PromptAppend:                 t.PromptAppend,
		Routing: orchestratorSDK.TaskRouting{
			Backend:      t.Routing.Backend,
			HostAffinity: t.Routing.HostAffinity,
			GpuRequired:  t.Routing.GpuRequired,
		},
		DispatchReason:   t.DispatchReason,
		BlockFaultClass:  t.BlockFaultClass,
		ConclusionReason: string(t.ConclusionReason),
		ChargedAttempts:  t.ChargedAttempts(),
		EffectiveBudget:  t.EffectiveAttemptBudget(),
		FaultClass:       t.FaultClass(),
		FaultOrigin:      string(t.FaultOrigin()),
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

func convertFleetNode(a *orchestration.FleetNode) *orchestratorSDK.FleetNode {
	if a == nil {
		return nil
	}
	return &orchestratorSDK.FleetNode{
		ID:           a.ID,
		TenantID:     a.TenantID,
		Name:         a.Name,
		Host:         a.Host,
		Backend:      a.Backend,
		Model:        a.Model,
		NodeType:     a.NodeType,
		Status:       string(a.Status),
		WorkDir:      a.WorkDir,
		LastSeen:     a.LastSeen,
		RegisteredAt: a.RegisteredAt,
		Version:      a.Version,
		Metadata:     a.Metadata,
		Backends:     a.ServedBackends(),
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
		PromptAppend:  s.PromptAppend,

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
		Source:         taskSourceToSDK(s.Source),
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
	// An empty role matrix stays nil, as it arrives over every wire
	// transport, so the in-process caller sees the same ProjectInfo.
	var fleetNodeRoles map[string]projectSDK.ProjectRole
	if len(p.Permissions.FleetNodeRoles) > 0 {
		fleetNodeRoles = make(map[string]projectSDK.ProjectRole, len(p.Permissions.FleetNodeRoles))
		for id, role := range p.Permissions.FleetNodeRoles {
			fleetNodeRoles[id] = projectSDK.ProjectRole(role)
		}
	}
	return &projectSDK.ProjectInfo{
		ID:               p.ID,
		TenantID:         p.TenantID,
		Name:             p.Name,
		Description:      p.Description,
		Workspace:        projectSDK.ProjectWorkspace{FleetNodeID: p.Workspace.FleetNodeID, Path: p.Workspace.Path},
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: agentRefFromDomain(p.DefaultLeadAgent),
		DefaultWorkflow:  p.DefaultWorkflow,
		Autonomous:       p.Autonomous,
		Parked:           p.Parked,
		ParkedAt:         p.ParkedAt,
		ParkedBy:         p.ParkedBy,
		ContextRepos:     contextReposToSDK(p.ContextRepos),
		Storage: projectSDK.ProjectStorage{
			WorkspaceTier:     projectSDK.WorkspaceTier(p.Storage.WorkspaceTier),
			WorkspaceTmpfsMB:  p.Storage.WorkspaceTmpfsMB,
			ContainerMemoryMB: p.Storage.ContainerMemoryMB,
		},
		SessionRotation: projectSDK.SessionRotationPolicy(p.SessionRotation),
		Git: projectSDK.ProjectGit{
			RepoURL:       p.Git.RepoURL,
			DefaultBranch: p.Git.DefaultBranch,
			CurrentSHA:    p.Git.CurrentSHA,
			Author:        projectSDK.GitAuthor{Name: p.Git.Author.Name, Email: p.Git.Author.Email},
			Signing: projectSDK.CommitSigning{
				Enabled:   p.Git.Signing.Enabled,
				Algorithm: p.Git.Signing.Algorithm,
				Key:       projectSDK.KMSKeyRef{Name: p.Git.Signing.Key.Name, Scope: p.Git.Signing.Key.Scope, Owner: p.Git.Signing.Key.Owner},
				Retired:   retiredSigningKeysToSDK(p.Git.Signing.Retired),
			},
			SSHKey: projectSDK.KMSKeyRef{Name: p.Git.SSHKey.Name, Scope: p.Git.SSHKey.Scope, Owner: p.Git.SSHKey.Owner},
		},
		Provisioning: projectSDK.ProjectProvisioning{
			Managed:   p.Provisioning.Managed,
			State:     projectSDK.ProvisioningState(p.Provisioning.State),
			Error:     p.Provisioning.Error,
			UpdatedAt: p.Provisioning.UpdatedAt,
		},
		Permissions: projectSDK.ProjectPermissions{
			FleetNodeRoles: fleetNodeRoles,
		},
		Quotas: projectSDK.ProjectQuotas{
			GPUMinutes:     p.Quotas.GPUMinutes,
			MaxConcurrency: p.Quotas.MaxConcurrency,
		},
		Spend:     spendLimitsFromOrchestration(p.Spend),
		Metadata:  p.Metadata,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

// sdkFleetNodeRolesToOrchestration converts an SDK-level agent role map to its
// orchestration-domain equivalent. Returns nil when the input map is nil so
// the service-layer Update can distinguish "leave unchanged" from "clear all".
func sdkFleetNodeRolesToOrchestration(roles map[string]projectSDK.ProjectRole) map[string]orchestration.ProjectRole {
	if roles == nil {
		return nil
	}
	out := make(map[string]orchestration.ProjectRole, len(roles))
	for id, role := range roles {
		out[id] = orchestration.ProjectRole(role)
	}
	return out
}

// embeddedSpendWindow converts one window and the usage against it.
func embeddedSpendWindow(window orchestration.SpendWindowStatus) orchestratorSDK.SpendWindowStatus {
	return orchestratorSDK.SpendWindowStatus{
		Name:                      window.Name,
		Size:                      int64(window.Size),
		MaxTokens:                 window.MaxTokens,
		UsedTokens:                window.UsedTokens,
		RemainingTokens:           window.RemainingTokens,
		MaxEquivalentMicros:       window.MaxEquivalentMicros,
		UsedEquivalentMicros:      window.UsedEquivalentMicros,
		RemainingEquivalentMicros: window.RemainingEquivalentMicros,
		Exhausted:                 window.Exhausted,
		FreesAt:                   window.FreesAt,
	}
}

// tenantSpendAccessorOf resolves the in-process tenant spend-policy store.
func tenantSpendAccessorOf(svc service.QuicBaaS, method string) (service.TenantSpendPolicySource, error) {
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", method)
	}
	accessor, ok := agentSvc.(service.TenantSpendPolicySource)
	if !ok {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", method)
	}
	return accessor, nil
}

// handleOrchestratorGetTenantSpendPolicy answers GetTenantSpendPolicy in
// process. service.GetTenantSpendPolicy scopes the storage namespace.
func handleOrchestratorGetTenantSpendPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetTenantSpendPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetTenantSpendPolicy",
			"*orchestrator.GetTenantSpendPolicyRequest", reflect.TypeOf(req).String())
	}
	accessor, err := tenantSpendAccessorOf(svc, "GetTenantSpendPolicy")
	if err != nil {
		return nil, err
	}
	report, err := service.GetTenantSpendPolicy(ctx, accessor, r.TenantID)
	if err != nil {
		return nil, err
	}
	return embeddedTenantSpendPolicy(report), nil
}

// handleOrchestratorSetTenantSpendPolicy answers SetTenantSpendPolicy in
// process.
func handleOrchestratorSetTenantSpendPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SetTenantSpendPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SetTenantSpendPolicy",
			"*orchestrator.SetTenantSpendPolicyRequest", reflect.TypeOf(req).String())
	}
	accessor, err := tenantSpendAccessorOf(svc, "SetTenantSpendPolicy")
	if err != nil {
		return nil, err
	}
	report, err := service.SetTenantSpendPolicy(ctx, accessor, r.TenantID, spendLimitsToOrchestration(r.Limits))
	if err != nil {
		return nil, err
	}
	return embeddedTenantSpendPolicy(report), nil
}

// embeddedTenantSpendPolicy converts a tenant's limits and window usage.
func embeddedTenantSpendPolicy(report *orchestration.TenantSpendPolicyReport) *orchestratorSDK.TenantSpendPolicy {
	out := &orchestratorSDK.TenantSpendPolicy{
		TenantID: report.TenantID,
		Limits:   spendLimitsFromOrchestration(report.Limits),
	}
	if !report.UpdatedAt.IsZero() {
		updated := report.UpdatedAt
		out.UpdatedAt = &updated
	}
	for _, window := range report.Windows {
		out.Windows = append(out.Windows, embeddedSpendWindow(window))
	}
	return out
}

// retiredSigningKeysToSDK renders a project's trusted signing history.
func retiredSigningKeysToSDK(keys []orchestration.RetiredSigningKey) []projectSDK.RetiredSigningKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]projectSDK.RetiredSigningKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, projectSDK.RetiredSigningKey{PublicKey: key.PublicKey, Name: key.Name, RetiredAt: key.RetiredAt})
	}
	return out
}
