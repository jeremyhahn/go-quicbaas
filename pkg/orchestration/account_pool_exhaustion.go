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

package orchestration

import (
	"context"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// A pool with nothing to give, reported once and the same way everywhere
// =============================================================================
//
// TWO PLACES FIND THE WHOLE POOL UNAVAILABLE, and an operator must hear it
// from both. The resolver finds it at spawn; admission finds it earlier and
// DEFERS, which means a pool that stays exhausted never reaches a spawn at all.
// Reporting only from the resolver (the first cut of go-quicbaas#1046) made the
// common case, the deferred one, invisible (Codex, #1047 round 2). So both call
// the one function below, and the audit event, the metric and the log line
// cannot differ by who noticed.
//
// THIS IS THE POOL SAYING IT IS BUSY, NOT BROKEN. Both conditions clear by
// themselves; what the report adds is that somebody can see a pool sitting at
// zero. The Needs-you inbox (go-quicbaas#1020) is the intended home for it as
// an item an operator clears; until it lands, these three are the surface.

// The reasons a pool is reported exhausted, as metric label values.
const (
	// AccountPoolSaturated: every candidate is at its session cap.
	AccountPoolSaturated = "saturated"

	// AccountPoolCoolingDown: every candidate is held by a provider limit or
	// an expiring credential — at admission, also by a spent usage window.
	AccountPoolCoolingDown = "cooling_down"

	// AccountPoolSpent: every candidate a spawn could select is refused by
	// its own spend policy (go-quicbaas#1058, ruling F6).
	AccountPoolSpent = "spent"
)

// accountPoolAnyProvider labels a pool report that spans every provider, which
// is what admission asks about: it cannot know which provider a task's runner
// will name until the task is dispatched.
const accountPoolAnyProvider = "any"

// accountPoolExhaustion is one report.
type accountPoolExhaustion struct {
	TenantID  string
	ProjectID string
	Provider  string
	Reason    string
	Detail    string
}

// exhaustionFromResolveError reports whether err is a resolution that found
// the whole pool unavailable, and describes it. An empty pool is a
// configuration fault, not a busy one, and is not reported here.
func exhaustionFromResolveError(req AccountResolveRequest, err error) (accountPoolExhaustion, bool) {
	var reason string
	switch {
	case errors.Is(err, ErrAccountsSaturated):
		reason = AccountPoolSaturated
	case errors.Is(err, ErrAccountsCoolingDown):
		reason = AccountPoolCoolingDown
	case errors.Is(err, ErrAccountsSpent):
		reason = AccountPoolSpent
	default:
		return accountPoolExhaustion{}, false
	}
	return accountPoolExhaustion{
		TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: string(req.Provider),
		Reason: reason, Detail: err.Error(),
	}, true
}

// reportAccountPoolExhausted records one exhausted pool on the audit trail,
// the metric and the log. Either sink may be nil; the metric always counts.
func reportAccountPoolExhausted(ctx context.Context, auditor StoreAuditor, logger interfaces.Logger, e accountPoolExhaustion) {
	metrics.GetMetrics().RecordOrchAccountPoolExhausted(e.TenantID, e.Provider, e.Reason)
	auditAccountPoolExhausted(ctx, auditor, e)
	if logger != nil {
		logger.Warnf("orchestration: every pooled %s account of tenant %s (project %q) is unavailable (%s): %s",
			e.Provider, e.TenantID, e.ProjectID, e.Reason, e.Detail)
	}
}

// auditAccountPoolExhausted records one exhausted pool on the audit trail
// only. A nil auditor records nothing.
//
// IT IS THE CONTROL PLANE'S HALF OF A REMOTE REFUSAL. A daemon that resolves
// accounts for a remote spawn has no audit sink of its own; it counts the
// metric and logs where it resolved, and the refusal travels back as a typed
// spawn fault. The control plane records the audit event from that fault
// (RemoteACPBackend.Spawn), so the metric and log are not counted twice for
// an embedded agent that shares this process.
func auditAccountPoolExhausted(ctx context.Context, auditor StoreAuditor, e accountPoolExhaustion) {
	emitStoreAudit(ctx, auditor, StoreAuditEvent{
		TenantID:   e.TenantID,
		Resource:   AuditResourceAccount,
		ResourceID: poolAuditResourceID(e.ProjectID, e.Provider),
		Action:     AuditActionPoolExhausted,
		Success:    false,
	})
}

// poolAuditResourceID names a provider pool on the audit trail: identifiers
// only, like every other audit event.
func poolAuditResourceID(projectID, provider string) string {
	if projectID == "" {
		projectID = accountLeaseScopeNone
	}
	return "pool/" + projectID + "/" + provider
}
