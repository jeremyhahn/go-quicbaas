//go:build rest

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

package rest

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// HandleOrchestratorCost serves GET /api/v1/orchestrator/cost — one scope of
// the consumption ledger, its drill-down, and the allowance status of every
// account that paid.
//
// ONE ROUTE, SCOPED BY QUERY, because the attribution chain is a PATH:
// tenant/project/task/job/session/round. "The project's spend" and "the job's
// spend" are the same prefix scan at two depths, so four routes over one scan
// would be four places for the drill-down's arithmetic to stop agreeing with
// itself. The levels must be supplied contiguously; a scope that skips one is
// refused by the ledger rather than answered by a scan of the tenant's whole
// history.
//
// Query parameters:
//
//	?tenant_id=   — override the authenticated principal's tenant
//	&project_id=  — narrow to one project
//	&task_id=     — narrow to one task (requires project_id)
//	&job_id=      — narrow to one job (requires task_id)
//	&session_id=  — narrow to one session (requires job_id)
//	&round=       — narrow to one round (requires session_id)
//
// Gated on orchestrator:status, the same permission every other orchestrator
// read carries. A spend report names no secret material: it reports token
// counts and an account's id and label, never a credential.
func (s *Server) HandleOrchestratorCost(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID := resolveOrchestratorTenant(r)

	req := &orchestration.SpendReportRequest{
		TenantID:  tenantID,
		ProjectID: r.URL.Query().Get("project_id"),
		TaskID:    r.URL.Query().Get("task_id"),
		JobID:     r.URL.Query().Get("job_id"),
		SessionID: r.URL.Query().Get("session_id"),
	}
	if raw := r.URL.Query().Get("round"); raw != "" {
		round, err := strconv.Atoi(raw)
		if err != nil {
			RespondBadRequest(w, r, "invalid round parameter: "+err.Error())
			return
		}
		req.Round = round
		req.RoundSet = true
	}

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}
	accessor, ok := agentSvc.(orchestration.SpendLedgerAccessor)
	if !ok {
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{
			"error": "consumption ledger not available",
		})
		return
	}

	report, err := orchestration.ReadSpendReport(ctx, orchestration.SpendSourcesOf(accessor), req)
	if err != nil {
		respondSpendError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// respondSpendError maps a refused spend read onto a status code.
//
// THE THREE OUTCOMES ARE DELIBERATELY DISTINCT. A malformed scope is the
// caller's fault (400); a deployment that records no consumption is a missing
// capability, not an empty ledger (503); anything else is a store that could
// not be read (500). Collapsing them onto one code would make "you asked for
// a task without a project" indistinguishable from "this cluster is not
// recording spend", and an operator would read both as "no spend".
func respondSpendError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, orchestration.ErrSpendLedgerRequired):
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case errors.Is(err, orchestration.ErrConsumptionInvalid), errors.Is(err, orchestration.ErrSpendRequestRequired):
		RespondBadRequest(w, r, err.Error())
	default:
		RespondJSON(w, r, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
