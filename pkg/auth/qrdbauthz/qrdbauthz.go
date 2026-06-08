//go:build iam

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

// Package qrdbauthz provides the single QuicBaaS authorizer adapter that
// delegates every authorization decision to qrdb's policy decision point
// (PDP) via the embedded SDK RBACClient.
//
// QuicBaaS is the composer: it owns no RBAC model of its own. This adapter is
// the one Policy Enforcement Point seam between the QuicBaaS transports
// (REST + the in-process embedded path, in this stage) and qrdb's authorizer.
// It builds a qrdb transport.Identity from a transport-agnostic Subject,
// threads the request's target tenant onto the context (so qrdb's
// system-tenant / tenant-only scope rules resolve against the right tenant),
// and calls RBACClient.Authorize with a qrdb-canonical resource + action.
//
// An optional ABAC overlay (iam.PolicyEvaluator) runs after a qrdb allow, in
// deny-overrides fashion: an explicit ABAC deny converts the allow into a
// typed denial. An ABAC result that merely fails to match any allow policy
// carries no opinion and preserves the RBAC allow, so installing the overlay
// never newly denies a request that RBAC already permitted.
package qrdbauthz

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// Subject is the transport-agnostic principal the adapter authorizes. It is a
// minimal projection of the authenticated caller: only the fields the qrdb
// PDP and the ABAC overlay actually consume. Transports map their own
// principal type (interfaces.Subject, auth.Claims) onto this.
type Subject struct {
	// ID is the principal identifier (the qrdb identity Subject — e.g. the
	// CN from a client cert or the OIDC token subject).
	ID string

	// TenantID is the principal's home tenant. It must be non-empty; qrdb's
	// system-tenant rule keys cross-tenant scope off the value "system".
	TenantID string

	// Roles are the qrdb-canonical role names assigned to the principal
	// (e.g. "admin"). They are resolved to permissions by qrdb's authorizer.
	Roles []string

	// Attributes carries ABAC attributes for the optional PolicyEvaluator
	// overlay. It does not influence the qrdb RBAC decision.
	Attributes map[string]interface{}
}

// RBACClientfunc late-binds the qrdb RBAC client. The qrdb node client is
// constructed during server lifecycle, after the adapter is wired, so the
// adapter resolves the client per request rather than capturing it at
// construction time. It returns nil before the node is ready.
type RBACClientFunc func() qrdbtransport.RBACClient

// Authorizer is the small authorization contract the QuicBaaS PEPs depend on.
// It is intentionally narrow — one decision verb — so transports do not couple
// to the forked provider surface.
type Authorizer interface {
	// Authorize returns nil when the subject is permitted to perform action
	// on resource and a typed denial otherwise. resource and action are
	// qrdb-canonical (e.g. "keys", "get"); the qrdb authorizer joins them
	// internally into the registered "resource:action" permission.
	Authorize(ctx context.Context, subject Subject, resource, action string) error
}

// Adapter is the single qrdb-backed Authorizer. It is safe for concurrent use:
// it holds no mutable state beyond the immutable rbacClient resolver and the
// concurrency-safe ABAC evaluator.
type Adapter struct {
	rbacClient RBACClientFunc
	evaluator  *iam.PolicyEvaluator
}

// Compile-time check that Adapter satisfies Authorizer.
var _ Authorizer = (*Adapter)(nil)

// New constructs the adapter. rbacClient must be non-nil; it late-binds the
// qrdb RBAC client. evaluator is optional: when non-nil the ABAC overlay runs
// after a qrdb allow.
func New(rbacClient RBACClientFunc, evaluator *iam.PolicyEvaluator) (*Adapter, error) {
	if rbacClient == nil {
		return nil, &NilRBACResolverError{}
	}
	return &Adapter{rbacClient: rbacClient, evaluator: evaluator}, nil
}

// Authorize delegates the decision to qrdb's PDP and applies the optional ABAC
// overlay. The request's target tenant — the tenant the operation acts upon —
// is read from the context (threaded by the transport via iam.ContextWithTenant
// / set by the service layer) and re-stamped with qrdb's tenant package so the
// qrdb authorizer's scope rules resolve against it. When the context carries no
// target tenant the request is treated as targeting the subject's own tenant.
func (a *Adapter) Authorize(ctx context.Context, subject Subject, resource, action string) error {
	if subject.TenantID == "" {
		return &MissingTenantError{Subject: subject.ID}
	}

	client := a.rbacClient()
	if client == nil {
		return &PDPUnavailableError{}
	}

	identity := &qrdbtransport.Identity{
		Subject:  subject.ID,
		TenantID: subject.TenantID,
		Roles:    subject.Roles,
	}

	// Thread the target tenant onto the context for qrdb's scope rules. The
	// iam layer carries the request's target tenant via iam.TenantFromContext;
	// fall back to the subject's own tenant when none is set so daemon-internal
	// and self-scope flows resolve correctly.
	targetTenant := iam.TenantFromContext(ctx)
	if targetTenant == "" {
		targetTenant = subject.TenantID
	}
	ctx = qrdbtenant.WithTenantID(ctx, targetTenant)

	// qrdb RBAC decision. Surfaces qrdb's typed errors (PermissionDeniedError,
	// InvalidIdentityError, ...) unchanged so callers can inspect the cause.
	if err := client.Authorize(ctx, identity, resource, action); err != nil {
		return err
	}

	// ABAC overlay (deny-overrides). Only an explicit deny match converts the
	// RBAC allow into a denial; a "no matching allow policy" result carries no
	// opinion and preserves the allow, so the overlay never newly denies what
	// RBAC permitted.
	if a.evaluator != nil {
		resp, err := a.evaluator.Evaluate(ctx, &iam.CheckRequest{
			Subject: iam.Subject{
				ID:         subject.ID,
				TenantID:   subject.TenantID,
				Roles:      subject.Roles,
				Attributes: subject.Attributes,
			},
			Action:   action,
			Resource: resource,
		})
		if err != nil {
			return &ABACEvaluationError{Cause: err}
		}
		// An explicit deny is signalled by a matched policy/rule with
		// Allowed=false. The default "no matching allow policy" result has no
		// matched policy and must not override the RBAC allow.
		if !resp.Allowed && resp.MatchedPolicy != "" {
			return &AuthorizationDeniedError{
				Subject:  subject.ID,
				Resource: resource,
				Action:   action,
				Reason:   resp.Reason,
			}
		}
	}

	return nil
}
