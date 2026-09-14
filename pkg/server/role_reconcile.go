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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// builtInRoleReconciler is the narrow view of the identity store the boot-time
// reconciliation needs. Taking the interface rather than *iam.IdentityStore is
// what lets the ordering contract below be proven in a test without standing up
// a node.
type builtInRoleReconciler interface {
	ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*iam.RoleReconcileReport, error)
}

// tenantLister is the narrow view of the tenant manager the reconciliation
// needs.
type tenantLister interface {
	ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error)
}

// reconcileTenantBuiltInRoles converges every tenant's stored built-in roles
// onto the definitions THIS build carries, and logs one line per tenant per
// changed role. A tenant that already agrees with the build produces no log
// line and no store write.
//
// WHY THIS RUNS ON EVERY BOOT. A tenant's built-in roles are copied from the
// build's definitions when the tenant is created and never touched again, while
// every ordinary write path refuses a built-in role afterwards (qrdb answers
// SystemRoleImmutableError to a re-permission, surfaced here as
// iam.ErrSystemRoleImmutable). So when a build renames or adds a permission,
// every pre-existing tenant keeps the old grant with no remedy available at any
// privilege level — not the tenant admin's, not the system admin's. Boot-time
// reconciliation is what closes that, and qrdb guarantees idempotency on the
// store rather than on the report, so a converged fleet pays nothing for it.
//
// WHY THE CALL ORDER MATTERS. qrdb resolves the built-in definitions through
// the process-global permission registry at CALL time. Both of this process's
// registration sites run strictly earlier: pkg/permission Register() from
// NewServer, and the module's registerRoleGrants() from apimodule.NewModule
// while the embedded node is being constructed. Calling before either would
// converge every tenant onto a definition missing exactly the verbs this build
// contributes — and report success while doing it. That is why this is invoked
// at the tail of IAM initialization and not earlier.
func (s *Server) reconcileTenantBuiltInRoles(ctx context.Context, roles builtInRoleReconciler, tenants tenantLister) error {
	if roles == nil {
		return nil
	}

	tenantIDs, err := reconcileTargets(ctx, tenants)
	if err != nil {
		return err
	}

	for _, tenantID := range tenantIDs {
		report, err := roles.ReconcileBuiltInRoles(ctx, tenantID)
		if err != nil {
			return &RoleReconcileError{TenantID: tenantID, Cause: err}
		}
		if !report.Changed() {
			continue
		}
		for _, r := range report.Roles {
			s.logger.Info("Built-in role reconciled against this build's definitions",
				"tenant", report.TenantID,
				"role", r.Name,
				"role_id", r.RoleID,
				"created", r.Created,
				"added", r.Added,
				"removed", r.Removed)
		}
	}
	return nil
}

// reconcileTargets enumerates the tenants to reconcile: the system tenant plus
// every tenant the broker knows about.
//
// The system tenant is named explicitly because the broker's tenant listing
// does not carry it — the same composition pkg/server already performs for the
// ABAC policy sweep (abac_sync.go allPolicies), and for the same reason:
// pkg/iam must not depend on qrdb's cross-tenant system-tenant concept, so the
// union is composed here. This is control-plane reconciliation of the control
// plane's own roles, not a data-plane path taking the system tenant as a
// default.
func reconcileTargets(ctx context.Context, tenants tenantLister) ([]string, error) {
	ids := []string{qrdbsdk.SystemTenantID}
	if tenants == nil {
		return ids, nil
	}

	listed, err := tenants.ListTenants(ctx, iam.ListOptions{})
	if err != nil {
		return nil, &RoleReconcileError{TenantID: "", Cause: err}
	}
	for _, t := range listed {
		if t == nil || t.ID == "" || t.ID == qrdbsdk.SystemTenantID {
			continue
		}
		ids = append(ids, t.ID)
	}
	return ids, nil
}
