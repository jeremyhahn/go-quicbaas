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

package iam

import (
	"context"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// SystemTenantID is the reserved control-plane tenant that hosts operator
// identities. It mirrors qrdb's reserved "system" tenant and is prefix-isolated
// (it lives in the shared/system store, NOT a per-tenant barrier), so it is
// readable before any tenant barrier unseals. Operator SSO providers (e.g. an
// Okta or generic OIDC IdP for staff/operators) are registered against this
// tenant; a federated callback for it mints a quicbaas bearer for the operator
// through the standard broker token path.
const SystemTenantID = systemTenantID

// IsSystemTenant reports whether tenantID is the reserved control-plane/system
// tenant. Operator SSO is the only intended use of federation against this
// tenant; all other tenants federate their own data-plane users.
func IsSystemTenant(tenantID string) bool {
	return tenantID == SystemTenantID
}

// BootstrapNotReadyError indicates an operator-SSO provider was registered for
// the system tenant before the bootstrap admin existed. This protects the
// bootstrap-ordering invariant: the local-factor admin MUST be created and
// cert-bound before any external IdP can be configured, otherwise an
// IdP-misconfiguration (or IdP outage at the moment of configuration) could
// leave the control plane with no usable login path. Local-factor login
// (mTLS/WebAuthn/PIV) is the always-available break-glass path and never
// depends on federation.
type BootstrapNotReadyError struct{}

// Error implements the error interface.
func (e *BootstrapNotReadyError) Error() string {
	return "operator SSO cannot be configured before the bootstrap admin exists"
}

// ErrBootstrapNotReady is returned when an operator-SSO provider is registered
// for the system tenant before a bootstrap admin has been created.
var ErrBootstrapNotReady = &dberrors.QBError{
	Code: dberrors.ErrForbidden,
	Op:   "iam.operator_sso.register",
	Err:  &BootstrapNotReadyError{},
}

// AdminPresenceChecker reports whether at least one administrative principal
// already exists. It is the bootstrap-ordering dependency for operator SSO:
// the *AdminManager satisfies it via HasAdmin below, but the interface keeps
// the registrar decoupled and testable.
type AdminPresenceChecker interface {
	// HasAdmin returns true when at least one active admin assignment exists,
	// meaning first-boot bootstrap has completed and a local-factor admin is
	// available as the break-glass path.
	HasAdmin(ctx context.Context) (bool, error)
}

// HasAdmin reports whether any active admin assignment exists. It is the
// control-plane readiness signal used to gate operator-SSO configuration so the
// bootstrap-ordering invariant holds.
func (m *AdminManager) HasAdmin(ctx context.Context) (bool, error) {
	assignments, err := m.ListAssignments(ctx, ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      1,
	})
	if err != nil {
		return false, err
	}
	return len(assignments) > 0, nil
}

// Compile-time assertion that *AdminManager satisfies the bootstrap gate.
var _ AdminPresenceChecker = (*AdminManager)(nil)

// OperatorSSORegistrar registers external IdPs for OPERATORS against the system
// tenant. It is an additive, optional capability layered on top of the always
// available local-factor (mTLS/WebAuthn/PIV) control-plane login:
//
//   - Local-factor login NEVER routes through this registrar or the federation
//     manager; it goes through the embedded qrdb login ceremony. An external
//     IdP outage therefore cannot take down control-plane login.
//
//   - Registration of a system-tenant (operator) provider is gated on the
//     bootstrap admin already existing (the bootstrap-ordering invariant): no
//     admin -> can't configure Okta. The local-factor admin is the break-glass
//     path that must exist first.
//
// Registration for any NON-system tenant is delegated unchanged — those are
// ordinary data-plane federation providers with their own lifecycle.
type OperatorSSORegistrar struct {
	federation FederationManager
	adminGate  AdminPresenceChecker
}

// NewOperatorSSORegistrar builds an operator-SSO registrar over the federation
// manager and the admin-presence gate. Both are required: the federation
// manager performs the actual provider persistence and callback brokering, and
// the gate enforces the bootstrap-ordering invariant for the system tenant.
func NewOperatorSSORegistrar(federation FederationManager, adminGate AdminPresenceChecker) (*OperatorSSORegistrar, error) {
	if federation == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.operator_sso.new",
			"federation manager is required", nil)
	}
	if adminGate == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.operator_sso.new",
			"admin presence checker is required", nil)
	}
	return &OperatorSSORegistrar{federation: federation, adminGate: adminGate}, nil
}

// RegisterOperatorProvider registers an external IdP for operator SSO against
// the system tenant. The provider's TenantID is forced to SystemTenantID so
// callers cannot accidentally register an operator IdP against a data-plane
// tenant. It fails closed with ErrBootstrapNotReady when no bootstrap admin
// exists yet, preserving the bootstrap-ordering invariant.
func (r *OperatorSSORegistrar) RegisterOperatorProvider(ctx context.Context, provider *ExternalProvider) error {
	if provider == nil {
		return NewIAMError(ErrProviderInvalid, "iam.operator_sso.register",
			"provider is required", nil)
	}

	// Force the system tenant: operator SSO is, by definition, system-tenant
	// federation. This also prevents a caller from smuggling a data-plane
	// tenant past the bootstrap gate.
	provider.TenantID = SystemTenantID

	if err := r.ensureBootstrapReady(ctx); err != nil {
		return err
	}

	return r.federation.RegisterProvider(ctx, provider)
}

// ensureBootstrapReady enforces the bootstrap-ordering invariant: a system
// tenant (operator) IdP may only be configured once the local-factor admin
// exists. It fails closed on any error reading the admin state — never opening
// the configuration path on uncertainty.
func (r *OperatorSSORegistrar) ensureBootstrapReady(ctx context.Context) error {
	hasAdmin, err := r.adminGate.HasAdmin(ctx)
	if err != nil {
		return NewIAMError(ErrBootstrapNotReady, "iam.operator_sso.register",
			"failed to verify bootstrap admin presence", err)
	}
	if !hasAdmin {
		return ErrBootstrapNotReady
	}
	return nil
}
