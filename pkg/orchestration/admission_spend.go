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
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// The spend axis — a project's and its tenant's own limits
// =============================================================================
//
// ADMISSION'S FOURTH INPUT (go-quicbaas#1058, ruling F6). An account's limits
// are enforced where its credential is released; a project's and a tenant's
// cannot be, because no single account carries them. They are enforced HERE,
// at admission: a project whose own windows are spent, or whose tenant's are,
// is DEFERRED with a typed reason and the moment it frees up, and its share of
// the fleet goes to projects that can still spend.
//
// The comparison is the one every spender is judged by (evaluateWindows, via
// QuotaAuthority.AdmitScope), over the project and tenant quota planes the
// ledger writes in the same Record (ruling F2). Local inference counts: those
// planes hold every turn charged to the project or tenant, pooled or not.
//
// THE TENANT IS ASKED FIRST. Its limits stop every project it owns, so it is
// the answer that explains the most; a project that is also over its own
// limits is named by the next sweep once the tenant frees up.
//
// FAIL CLOSED, WITH AN ERROR AND NO ANSWER, as the provider axis does: a
// window that could not be summed is not a window that admits.

// Admission axes of the spend input.
const (
	// AdmissionAxisProjectSpend means the project's own spend limits decided:
	// a window is spent, so the project is DEFERRED until it frees up.
	AdmissionAxisProjectSpend AdmissionAxis = "project_spend"

	// AdmissionAxisTenantSpend means the project's tenant's spend limits
	// decided, deferring every project the tenant owns.
	AdmissionAxisTenantSpend AdmissionAxis = "tenant_spend"
)

// ScopeSpendVerdict is the spend axis's answer for one project.
type ScopeSpendVerdict struct {
	// Deferred reports a spent project or tenant.
	Deferred bool

	// Axis is AdmissionAxisProjectSpend or AdmissionAxisTenantSpend when
	// Deferred.
	Axis AdmissionAxis

	// Decision is the quota authority's refusal that deferred, carrying the
	// window, axis, usage and limit.
	Decision *QuotaDecision

	// Reason is the operator-facing sentence.
	Reason string
}

// ScopeSpendReader answers whether a project and its tenant are within their
// own spend limits. It returns an error and no answer whenever it could not
// establish one.
type ScopeSpendReader interface {
	ScopeSpend(ctx context.Context, project *Project, now time.Time) (ScopeSpendVerdict, error)
}

// scopeSpendAdmission is the ledger-backed ScopeSpendReader.
type scopeSpendAdmission struct {
	quota   QuotaAuthority
	tenants TenantSpendPolicyStore

	// auditor and logger receive the deferral report; the metric is always
	// counted. See WithScopeSpendReporting.
	auditor StoreAuditor
	logger  interfaces.Logger

	// reported remembers, per project, the deferral last reported, so a
	// project that stays spent is reported once.
	reportMu sync.Mutex
	reported map[string]string
}

// ScopeSpendOption configures NewScopeSpendAdmission.
type ScopeSpendOption func(*scopeSpendAdmission)

// WithScopeSpendReporting routes a spend deferral to the audit trail and the
// log. Either may be nil.
func WithScopeSpendReporting(auditor StoreAuditor, logger interfaces.Logger) ScopeSpendOption {
	return func(s *scopeSpendAdmission) {
		s.auditor = auditor
		s.logger = logger
	}
}

// Compile-time interface satisfaction check.
var _ ScopeSpendReader = (*scopeSpendAdmission)(nil)

// NewScopeSpendAdmission returns the ScopeSpendReader over the quota authority
// and the tenants' spend policies.
func NewScopeSpendAdmission(quota QuotaAuthority, tenants TenantSpendPolicyStore, opts ...ScopeSpendOption) (ScopeSpendReader, error) {
	switch {
	case quota == nil:
		return nil, &AdmissionConfigError{Field: "quota", Message: "a project's and a tenant's windows are decided by the quota authority"}
	case tenants == nil:
		return nil, &AdmissionConfigError{Field: "tenants", Message: "a tenant's own limits are read from its spend policy"}
	}
	s := &scopeSpendAdmission{quota: quota, tenants: tenants, reported: make(map[string]string)}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// ScopeSpend implements ScopeSpendReader.
func (s *scopeSpendAdmission) ScopeSpend(ctx context.Context, project *Project, now time.Time) (ScopeSpendVerdict, error) {
	if project == nil || project.TenantID == "" {
		return ScopeSpendVerdict{}, &AdmissionConfigError{Field: "project", Message: "a spend verdict is for one project of one tenant"}
	}
	tenantCtx := ContextWithTenant(ctx, project.TenantID)

	policy, err := s.tenants.Get(tenantCtx, project.TenantID)
	if err != nil {
		return ScopeSpendVerdict{}, &AdmissionUndecidableError{Input: "spend policy of tenant " + project.TenantID, Cause: err}
	}
	verdict, err := s.judge(tenantCtx, ScopeAdmitRequest{TenantID: project.TenantID, Limits: policy.Limits},
		AdmissionAxisTenantSpend, "tenant "+project.TenantID, now)
	if err != nil {
		return ScopeSpendVerdict{}, err
	}
	if !verdict.Deferred {
		verdict, err = s.judge(tenantCtx, ScopeAdmitRequest{TenantID: project.TenantID, ProjectID: project.ID, Limits: project.Spend},
			AdmissionAxisProjectSpend, "project "+project.ID, now)
		if err != nil {
			return ScopeSpendVerdict{}, err
		}
	}
	s.report(ctx, project, verdict)
	return verdict, nil
}

// judge asks the quota authority about one spender's limits. Limits that
// meter nothing admit without reading the ledger.
func (s *scopeSpendAdmission) judge(ctx context.Context, req ScopeAdmitRequest, axis AdmissionAxis, spender string, now time.Time) (ScopeSpendVerdict, error) {
	if !req.Limits.Metered() {
		return ScopeSpendVerdict{}, nil
	}
	decision, err := s.quota.AdmitScope(ctx, req, now)
	if err != nil {
		return ScopeSpendVerdict{}, &AdmissionUndecidableError{Input: "spend windows of " + spender, Cause: err}
	}
	if decision.Allowed {
		return ScopeSpendVerdict{}, nil
	}
	return ScopeSpendVerdict{
		Deferred: true,
		Axis:     axis,
		Decision: decision,
		Reason:   spendDeferralReason(spender, decision),
	}, nil
}

// spendDeferralReason is the sentence an operator reads on a deferred project.
func spendDeferralReason(spender string, decision *QuotaDecision) string {
	msg := "the " + spender + " spend window " + strconv.Quote(decision.Window) + " is spent: "
	if decision.Axis == QuotaAxisEquivalentMicros {
		msg += "$" + microsToDollars(decision.Used) + " of $" + microsToDollars(decision.Limit) + " at list price"
	} else {
		msg += strconv.FormatInt(decision.Used, 10) + " of " + strconv.FormatInt(decision.Limit, 10) + " tokens"
	}
	if !decision.RetryAt.IsZero() {
		msg += "; it frees up at " + decision.RetryAt.UTC().Format(time.RFC3339)
	}
	return msg
}

// microsToDollars renders micro-dollars to the cent.
func microsToDollars(micros int64) string {
	return strconv.FormatFloat(float64(micros)/1e6, 'f', 2, 64)
}

// report records a spend deferral on the metric, the audit trail and the log
// when it starts or changes, never on every sweep; a project that frees up is
// forgotten so its next deferral is reported again.
func (s *scopeSpendAdmission) report(ctx context.Context, project *Project, verdict ScopeSpendVerdict) {
	key := project.TenantID + keySeparator + project.ID
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	if !verdict.Deferred {
		delete(s.reported, key)
		return
	}
	resourceID := spendDeferralResourceID(project.ID, verdict)
	if s.reported[key] == resourceID {
		return
	}
	s.reported[key] = resourceID
	metrics.GetMetrics().RecordFactorySpendDeferral(project.TenantID, project.ID, string(verdict.Axis))
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   project.TenantID,
		Resource:   AuditResourceProject,
		ResourceID: resourceID,
		Action:     AuditActionSpendDeferred,
		Success:    false,
	})
	if s.logger != nil {
		s.logger.Warnf("orchestration: admission deferred project %s (tenant %s): %s",
			project.ID, project.TenantID, verdict.Reason)
	}
}

// spendDeferralResourceID names a deferral on the audit trail: identifiers
// only — the project, which spender's limit, the refusal and the window —
// like every other audit event.
func spendDeferralResourceID(projectID string, verdict ScopeSpendVerdict) string {
	return projectID + "/" + string(verdict.Axis) + "/" + string(verdict.Decision.Reason) + "/" + verdict.Decision.Window
}
