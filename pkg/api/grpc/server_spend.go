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

package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// spendLedgerProvider is orchestration's own SpendLedgerAccessor under a local
// name, the same way sessionEventLogProvider aliases SessionLogAccessor: every
// transport reaches the consumption ledger through one declaration, so a
// deployment that wires it or does not behaves identically on all of them.
type spendLedgerProvider = orchestration.SpendLedgerAccessor

// GetSpendReport implements OrchestratorService.GetSpendReport: one scope of
// the consumption ledger, its drill-down, and the allowance status of every
// account that paid.
//
// The projection lives in the orchestration package so a gRPC caller and an
// HTTP caller roll the ledger up identically rather than each having their own
// idea of what a project's spend is.
func (s *OrchestratorServer) GetSpendReport(ctx context.Context, req *pb.GetSpendReportRequest) (*pb.GetSpendReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.agentSvc.(spendLedgerProvider)
	if !ok {
		return nil, status.Error(codes.Unavailable, "consumption ledger not available")
	}

	// THE TENANT GOES ON THE CONTEXT, not only in the scope. Every sibling
	// orchestrator RPC wraps this way, and it is load-bearing rather than
	// cosmetic: the orchestration KV is tenant-routed (pkg/server
	// durableKVStore), so on a barrier-isolated tenant a read carrying no
	// tenant resolves against the wrong store — the defect go-quicbaas#820
	// fixed for session transcripts. It also arms AccountStore's scopeCheck,
	// which can only ever refuse more strictly and never broaden access.
	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	report, err := orchestration.ReadSpendReport(tenantCtx, orchestration.SpendSourcesOf(provider),
		&orchestration.SpendReportRequest{
			TenantID:  req.GetTenantId(),
			ProjectID: req.GetProjectId(),
			TaskID:    req.GetTaskId(),
			JobID:     req.GetJobId(),
			SessionID: req.GetSessionId(),
			Round:     int(req.GetRound()),
			RoundSet:  req.GetRoundSet(),
			From:      spendInstant(req.GetFrom()),
			To:        spendInstant(req.GetTo()),
			Host:      req.GetHost(),
			Agent:     req.GetAgent(),
		})
	if err != nil {
		return nil, spendStatusError(err)
	}
	return spendReportToProto(report), nil
}

// spendStatusError maps a refused spend read onto a gRPC code.
//
// THE THREE OUTCOMES STAY DISTINCT, matching the REST handler's mapping. A
// malformed scope is the caller's (InvalidArgument); a deployment that records
// no consumption is a missing capability and not an empty ledger
// (Unavailable); anything else is a store that could not be read (Internal).
// One code for all three would make "this cluster is not recording spend"
// read as "nothing has been spent".
func spendStatusError(err error) error {
	switch {
	case errors.Is(err, orchestration.ErrSpendLedgerRequired):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, orchestration.ErrConsumptionInvalid), errors.Is(err, orchestration.ErrSpendRequestRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "spend report: %v", err)
	}
}

// spendReportToProto renders the report onto the wire.
//
// FIELD BY FIELD, not as an opaque JSON blob: the gRPC projection of an
// orchestrator record is field-by-field everywhere else, and a client that has
// to parse JSON out of a protobuf has no schema for the thing it is reading.
func spendReportToProto(report *orchestration.SpendReport) *pb.GetSpendReportResponse {
	out := &pb.GetSpendReportResponse{
		Level:       string(report.Level),
		TenantId:    report.TenantID,
		ProjectId:   report.ProjectID,
		TaskId:      report.TaskID,
		JobId:       report.JobID,
		SessionId:   report.SessionID,
		Round:       int32(report.Round),
		ProjectName: report.ProjectName,
		Totals:      spendTotalsToProto(report.ConsumptionTotals),
		Currency:    report.Currency,
		FirstAt:     spendTimestamp(report.FirstAt),
		LastAt:      spendTimestamp(report.LastAt),
		From:        spendTimestamp(report.From),
		To:          spendTimestamp(report.To),
		ChildLevel:  string(report.ChildLevel),

		SubscriptionFeeMicros: report.SubscriptionFeeMicros,
		ScopePolicyStatus:     string(report.ScopePolicyStatus),
		Host:                  report.Host,
	}
	for _, host := range report.Hosts {
		out.Hosts = append(out.Hosts, &pb.SpendHost{
			Host:         host.Host,
			Unattributed: host.Unattributed,
			Totals:       spendTotalsToProto(host.ConsumptionTotals),
		})
	}
	for _, window := range report.ScopeWindows {
		out.ScopeWindows = append(out.ScopeWindows, spendWindowStatusToProto(window))
	}
	out.Agent = report.Agent
	out.AgentPolicyStatus = string(report.AgentPolicyStatus)
	for _, agent := range report.Agents {
		out.Agents = append(out.Agents, &pb.SpendAgent{
			AgentId:      agent.AgentID,
			Name:         agent.Name,
			Unattributed: agent.Unattributed,
			Totals:       spendTotalsToProto(agent.ConsumptionTotals),
		})
	}
	for _, window := range report.AgentWindows {
		out.AgentWindows = append(out.AgentWindows, spendWindowStatusToProto(window))
	}
	for _, account := range report.Accounts {
		out.Accounts = append(out.Accounts, spendAccountToProto(account))
	}
	for _, model := range report.Models {
		out.Models = append(out.Models, &pb.SpendModel{
			Engine: model.Engine,
			Model:  model.Model,
			Totals: spendTotalsToProto(model.ConsumptionTotals),
		})
	}
	for _, child := range report.Children {
		out.Children = append(out.Children, &pb.SpendChild{
			Id:           child.ID,
			Level:        string(child.Level),
			Unattributed: child.Unattributed,
			Name:         child.Name,
			Totals:       spendTotalsToProto(child.ConsumptionTotals),
		})
	}
	return out
}

// spendTotalsToProto renders one set of consumption totals.
func spendTotalsToProto(totals orchestration.ConsumptionTotals) *pb.SpendTotals {
	return &pb.SpendTotals{
		Entries:            int32(totals.Entries),
		InputTokens:        totals.InputTokens,
		OutputTokens:       totals.OutputTokens,
		CacheReadTokens:    totals.CacheReadTokens,
		CacheWriteTokens:   totals.CacheWriteTokens,
		TotalTokens:        totals.TotalTokens,
		EquivalentMicros:   totals.EquivalentMicros,
		PricedEntries:      int32(totals.PricedEntries),
		UnpricedEntries:    int32(totals.UnpricedEntries),
		DurationMs:         totals.DurationMillis,
		Successes:          int32(totals.Successes),
		Failures:           int32(totals.Failures),
		BilledMicros:       totals.BilledMicros,
		BilledEntries:      int32(totals.BilledEntries),
		UnbilledEntries:    int32(totals.UnbilledEntries),
		EnergyMwh:          totals.EnergyMilliWh,
		ElectricityEntries: int32(totals.ElectricityEntries),

		MeasuredEnergyEntries: int32(totals.MeasuredEnergyEntries),
	}
}

// spendAccountToProto renders one account's contribution and allowance.
func spendAccountToProto(account orchestration.SpendAccount) *pb.SpendAccount {
	out := &pb.SpendAccount{
		AccountId:        account.AccountID,
		DisplayName:      account.DisplayName,
		Provider:         account.Provider,
		Totals:           spendTotalsToProto(account.ConsumptionTotals),
		PolicyStatus:     string(account.PolicyStatus),
		BillingMode:      string(account.Billing.Mode),
		MonthlyFeeMicros: account.Billing.MonthlyFeeMicros,

		SubscriptionFeeMicros: account.SubscriptionFeeMicros,
	}
	for _, window := range account.Windows {
		out.Windows = append(out.Windows, spendWindowStatusToProto(window))
	}
	if account.Breaker != nil {
		out.Breaker = &pb.SpendBreakerVerdict{
			State:         string(account.Breaker.State),
			Failures:      int32(account.Breaker.Failures),
			Successes:     int32(account.Breaker.Successes),
			LastFailureAt: spendTimestamp(account.Breaker.LastFailureAt),
			LastSuccessAt: spendTimestamp(account.Breaker.LastSuccessAt),
			OpenUntil:     spendTimestamp(account.Breaker.OpenUntil),
		}
	}
	return out
}

// spendInstant reads an optional request instant: unset is the zero time,
// which the report reads as "no bound".
func spendInstant(at *timestamppb.Timestamp) time.Time {
	if at == nil {
		return time.Time{}
	}
	return at.AsTime().UTC()
}

// spendTimestamp renders an instant, leaving an unset one UNSET.
//
// timestamppb.New of a zero time.Time is the year 1 — a real instant on the
// wire that a client renders as a date. A moment the report does not have must
// cross as absent, which is the same rule the report itself follows about a
// figure it does not have.
func spendTimestamp(at *time.Time) *timestamppb.Timestamp {
	if at == nil || at.IsZero() {
		return nil
	}
	return timestamppb.New(*at)
}

// spendWindowStatusToProto renders one window and the usage against it.
func spendWindowStatusToProto(window orchestration.SpendWindowStatus) *pb.SpendWindowStatus {
	return &pb.SpendWindowStatus{
		Name:                      window.Name,
		SizeNanos:                 int64(window.Size),
		MaxTokens:                 window.MaxTokens,
		UsedTokens:                window.UsedTokens,
		RemainingTokens:           window.RemainingTokens,
		MaxEquivalentMicros:       window.MaxEquivalentMicros,
		UsedEquivalentMicros:      window.UsedEquivalentMicros,
		RemainingEquivalentMicros: window.RemainingEquivalentMicros,
		Exhausted:                 window.Exhausted,
		FreesAt:                   spendTimestamp(window.FreesAt),
	}
}
