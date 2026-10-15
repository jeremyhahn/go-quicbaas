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
	"reflect"
	"strconv"
	"time"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Delegated-administration handlers (quicbaas.IAM.Admin/*).
//
// The gRPC and REST transports have carried the Admin service since it
// shipped; the embedded transport did not, so an in-process host could not
// grant a super-admin assignment or open an impersonation session at all —
// the one deployment shape where delegated administration matters most.
// These handlers close that gap against the same *iam.AdminManager the REST
// handlers use, so every authorization and audit rule the manager enforces
// applies identically in-process.
//
// Delegated administration is privileged, so the REST routes are additionally
// permission-gated by the frozen route table
// (pkg/api/rest/routes_permissions_generated.go, e.g. roles:assign for
// POST /api/v1/admin/assignments). Whenever the request context carries an
// authenticated Subject, these handlers evaluate the SAME resource/action pair
// against the policy engine before touching the manager, and fail closed if no
// engine is wired — so putting the SDK behind an authenticated surface does not
// make the embedded transport the cheapest way to mint a super-admin. A context
// with NO Subject is the host process calling its own service before any
// transport has run — the pre-existing contract for every embedded IAM handler
// — and is unrestricted.

// Route-equivalent permissions, transcribed from the frozen REST route table
// so the two cannot drift silently: a reviewer can diff these against
// pkg/api/rest/routes_permissions_generated.go by name.
const (
	adminResourceRoles       = "roles"
	adminResourceUsers       = "users"
	adminResourcePermissions = "permissions"

	adminActionAssign   = "assign"
	adminActionUnassign = "unassign"
	adminActionRead     = "read"
	adminActionList     = "list"
	adminActionUpdate   = "update"
	adminActionResolve  = "resolve"
)

// tenantScopedSubject returns the subject a tenant-scoping rule applies to.
//
// The second result is false for an unbound context (the host process) AND for
// a system-tenant principal: the system scope is the platform itself, not a
// tenant named "system", so pinning it to its own "tenant" would make
// cross-tenant platform administration impossible — the very capability the
// system tenant exists to carry. Only a subject bound to the system tenant id
// itself is system (auth.IsSystemSubject): a bound subject with an empty
// tenant belongs to no tenant and is refused with a permission-denied error
// naming service and method (#348).
func tenantScopedSubject(ctx context.Context, service, method string) (interfaces.Subject, bool, error) {
	subject, bound := auth.SubjectFromContext(ctx)
	if !bound {
		return interfaces.Subject{}, false, nil
	}
	if subject.TenantID == "" {
		return interfaces.Subject{}, false, NewPermissionDeniedError(service, method,
			"the authenticated subject carries no tenant binding")
	}
	if auth.IsSystemSubject(subject) {
		return interfaces.Subject{}, false, nil
	}
	return subject, true, nil
}

// requireSubjectTenant resolves the tenant a tenant-scoped subject may act in.
// An omitted tenant is filled from the subject; naming a different one is
// refused. A system-scope or unbound caller keeps whatever it asked for.
func requireSubjectTenant(ctx context.Context, method, requested string) (string, error) {
	subject, scoped, err := tenantScopedSubject(ctx, "quicbaas.IAM.Admin", method)
	if err != nil {
		return "", err
	}
	if !scoped {
		return requested, nil
	}
	if requested != "" && requested != subject.TenantID {
		return "", NewPermissionDeniedError("quicbaas.IAM.Admin", method,
			"tenant_id does not match the authenticated subject")
	}
	return subject.TenantID, nil
}

// requireAdminPermission evaluates the route-equivalent permission for the
// authenticated subject, when there is one.
//
// It fails CLOSED: a subject-bound call on a host with no policy engine is
// denied rather than allowed, because "no policy engine" cannot be
// distinguished from "policies not loaded yet", and defaulting to allow on a
// delegated-administration surface is the wrong side to err on.
func requireAdminPermission(ctx context.Context, svc service.QuicBaaS, method, resource, action string) error {
	subject, bound := auth.SubjectFromContext(ctx)
	if !bound {
		return nil
	}

	engine, err := policyEngineFor(ctx, svc)
	if err != nil {
		return NewPermissionDeniedError("quicbaas.IAM.Admin", method,
			"no policy engine is configured to authorize "+resource+":"+action)
	}

	resp, err := engine.Evaluate(ctx, &iam.CheckRequest{
		Subject: iam.Subject{
			ID:            subject.ID,
			Name:          subject.Username,
			TenantID:      subject.TenantID,
			Tenant:        subject.TenantID,
			Roles:         subject.Roles,
			Groups:        subject.Groups,
			Authenticated: true,
		},
		Resource: resource,
		Action:   action,
	})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Allowed {
		return NewPermissionDeniedError("quicbaas.IAM.Admin", method,
			"subject lacks "+resource+":"+action)
	}
	return nil
}

type adminManagerProvider interface {
	GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
}

// adminManagerFor resolves the admin manager through the same structural type
// assertion the rest of the embedded IAM dispatch uses.
func adminManagerFor(ctx context.Context, svc service.QuicBaaS) (*iam.AdminManager, error) {
	p, ok := svc.(adminManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Admin")
	}
	mgr, err := p.GetAdminManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError("IAM.Admin")
	}
	return mgr, nil
}

// =============================================================================
// Assignments
// =============================================================================

func handleIAMAdminCreateAssignment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "CreateAssignment", adminResourceRoles, adminActionAssign); err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.CreateAdminAssignmentRequest](req, "CreateAdminAssignmentRequest")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "CreateAssignment",
			"*iam.CreateAdminAssignmentRequest", reflect.TypeOf(req).String())
	}

	// RoleID names the predefined admin role — it is the field the CLI and
	// every other transport fill from --role. The manager rejects an empty or
	// unknown role with a typed required/invalid-field error, so validation
	// stays in one place rather than being re-implemented per transport.
	tenantIDs, err := adminTenantIDs(r)
	if err != nil {
		return nil, err
	}
	// A tenant-scoped subject may only grant administration over its own
	// tenant. An omitted scope would otherwise be a GLOBAL grant — the
	// broadest escalation available on this surface — so it is filled from the
	// subject rather than passed through. A system-scope caller keeps the
	// cross-tenant and global forms.
	subject, scoped, err := tenantScopedSubject(ctx, "quicbaas.IAM.Admin", "CreateAssignment")
	if err != nil {
		return nil, err
	}
	if scoped {
		switch {
		case len(tenantIDs) == 0:
			tenantIDs = []string{subject.TenantID}
		case len(tenantIDs) != 1 || tenantIDs[0] != subject.TenantID:
			return nil, NewPermissionDeniedError("quicbaas.IAM.Admin", "CreateAssignment",
				"a tenant-scoped subject may only grant administration over its own tenant")
		}
	}

	// GrantedBy is the audit actor and the returned creator of a privileged
	// assignment, so a subject-bound caller must not be able to name someone
	// else and forge who granted it. An omitted value is filled from the
	// subject; a mismatched one is refused.
	grantedBy := r.GrantedBy
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if grantedBy != "" && grantedBy != subject.ID {
			return nil, NewPermissionDeniedError("quicbaas.IAM.Admin", "CreateAssignment",
				"granted_by does not match the authenticated subject")
		}
		grantedBy = subject.ID
	}

	adminID, err := parseIAMUserID("quicbaas.IAM.Admin", "CreateAssignment", r.AdminID)
	if err != nil {
		return nil, err
	}
	opts := iam.CreateAdminAssignmentOptions{
		UserID:      adminID,
		AdminRole:   iam.AdminRole(r.RoleID),
		TenantIDs:   tenantIDs,
		Permissions: adminPermissions(r.Permissions),
		GrantedBy:   grantedBy,
		Metadata:    r.Metadata,
	}
	if !r.ExpiresAt.IsZero() {
		expiresAt := r.ExpiresAt
		opts.ExpiresAt = &expiresAt
	}
	if r.Constraints != nil {
		opts.DeniedPermissions = adminPermissions(r.Constraints.DeniedOperations)
		scopes, scopeErr := resourceScopesFromFilters("CreateAssignment", r.Constraints.ResourceFilters)
		if scopeErr != nil {
			return nil, scopeErr
		}
		opts.ResourceScopes = scopes
	}

	assignment, err := mgr.CreateAssignment(ctx, opts)
	if err != nil {
		return nil, err
	}
	return adminAssignmentToSDK(assignment), nil
}

func handleIAMAdminGetAssignment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "GetAssignment", adminResourceRoles, adminActionRead); err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "AssignmentID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "GetAssignment",
			"struct with AssignmentID field", reflect.TypeOf(req).String())
	}
	assignment, err := mgr.GetAssignment(ctx, id)
	if err != nil {
		return nil, err
	}
	return adminAssignmentToSDK(assignment), nil
}

func handleIAMAdminUpdateAssignment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "UpdateAssignment", adminResourceRoles, adminActionAssign); err != nil {
		return nil, err
	}
	id, hasID := iamRequestField(req, "AssignmentID")
	r, hasUpdate := iamRequestPayload[iamsdk.UpdateAdminAssignmentRequest](req, "Update")
	if !hasID || !hasUpdate {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "UpdateAssignment",
			"struct with AssignmentID and Update fields", reflect.TypeOf(req).String())
	}

	opts := iam.UpdateAdminAssignmentOptions{
		Permissions: adminPermissions(r.Permissions),
		Metadata:    r.Metadata,
	}
	if !r.ExpiresAt.IsZero() {
		expiresAt := r.ExpiresAt
		opts.ExpiresAt = &expiresAt
	}
	if r.Constraints != nil {
		opts.DeniedPermissions = adminPermissions(r.Constraints.DeniedOperations)
		scopes, scopeErr := resourceScopesFromFilters("UpdateAssignment", r.Constraints.ResourceFilters)
		if scopeErr != nil {
			return nil, scopeErr
		}
		opts.ResourceScopes = scopes
	}
	// The SDK models assignment lifecycle as a status enum while the domain
	// stores a single Active flag: only the active status maps to true, every
	// other status (pending, suspended, expired, revoked) leaves the
	// assignment inactive.
	if r.Status != nil {
		active := *r.Status == iamsdk.AdminAssignmentStatusActive
		opts.Active = &active
	}

	assignment, err := mgr.UpdateAssignment(ctx, id, opts)
	if err != nil {
		return nil, err
	}
	return adminAssignmentToSDK(assignment), nil
}

func handleIAMAdminDeleteAssignment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "DeleteAssignment", adminResourceRoles, adminActionUnassign); err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "AssignmentID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "DeleteAssignment",
			"struct with AssignmentID field", reflect.TypeOf(req).String())
	}
	if err := mgr.DeleteAssignment(ctx, id); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMAdminListAssignments(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "ListAssignments", adminResourceRoles, adminActionList); err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.ListAdminAssignmentsRequest](req, "Filter")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "ListAssignments",
			"*iam.ListAdminAssignmentsRequest", reflect.TypeOf(req).String())
	}

	// The tenant filter is caller-supplied and the store applies no tenant
	// scoping of its own, so a tenant-scoped subject is pinned to its own
	// tenant: roles:list says the caller may list assignments, not that it may
	// list another tenant's. A system-scope caller keeps the cross-tenant view,
	// including the unfiltered one.
	tenantID, err := requireSubjectTenant(ctx, "ListAssignments", r.TenantID)
	if err != nil {
		return nil, err
	}

	// AdminID is an optional filter; only a supplied one must be a user id.
	var adminID uint64
	if r.AdminID != "" {
		adminID, err = parseIAMUserID("quicbaas.IAM.Admin", "ListAssignments", r.AdminID)
		if err != nil {
			return nil, err
		}
	}
	opts := iam.ListAdminAssignmentsOptions{
		UserID:   adminID,
		TenantID: tenantID,
		Limit:    r.Limit,
		Offset:   r.Offset,
	}
	// ActiveOnly is set by exactly one thing: an explicit "active" status
	// filter. This mirrors the REST transport's mapping
	// (sdk/go/transport/rest/iam_handler.go, ListAssignments) so an
	// unfiltered call returns the same rows on every transport — an
	// embedded-only default of "active assignments" would silently hide
	// suspended grants from an auditor who asked for everything.
	//
	// The domain's list filter is a single Active flag, so it cannot express
	// "only suspended" or "only revoked"; a non-active status filter is
	// therefore not narrowed here, exactly as it is not narrowed over REST or
	// gRPC. IncludeExpired has no domain counterpart for the same reason.
	// Diverging in either direction would trade a shared behavior for a
	// transport-specific one.
	if r.Status != nil && *r.Status == iamsdk.AdminAssignmentStatusActive {
		opts.ActiveOnly = true
	}

	assignments, err := mgr.ListAssignments(ctx, opts)
	if err != nil {
		return nil, err
	}

	out := make([]iamsdk.AdminAssignment, 0, len(assignments))
	for _, a := range assignments {
		if a == nil {
			continue
		}
		out = append(out, *adminAssignmentToSDK(a))
	}
	return &iamsdk.ListAdminAssignmentsResponse{
		Assignments: out,
		TotalCount:  len(out),
		HasMore:     opts.Limit > 0 && len(out) == opts.Limit,
	}, nil
}

// =============================================================================
// Permission checks
// =============================================================================

func handleIAMAdminCheckPermission(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "CheckPermission", adminResourcePermissions, adminActionResolve); err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.AdminCheckRequest](req, "Check")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "CheckPermission",
			"*iam.AdminCheckRequest", reflect.TypeOf(req).String())
	}

	// Permission is the precise grant name; Action is the looser alias the
	// SDK accepts when a caller only knows the verb. Permission wins when
	// both are set so a caller can never widen a check by also passing an
	// action.
	permission := r.Permission
	if permission == "" {
		permission = r.Action
	}

	// AdminID stays free: it names the admin being INSPECTED, and answering
	// "does this admin hold X" is the point of the call. The tenant is the
	// scope the answer is computed in, so a tenant-scoped subject is pinned to
	// its own; a system-scope caller may inspect across tenants.
	tenantID, err := requireSubjectTenant(ctx, "CheckPermission", r.TenantID)
	if err != nil {
		return nil, err
	}

	adminID, err := parseIAMUserID("quicbaas.IAM.Admin", "CheckPermission", r.AdminID)
	if err != nil {
		return nil, err
	}
	resp, err := mgr.CheckPermission(ctx, &iam.AdminCheckRequest{
		UserID:       adminID,
		Permission:   iam.AdminPermission(permission),
		TenantID:     tenantID,
		ResourceType: r.ResourceType,
		ResourceID:   r.ResourceID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &iamsdk.AdminCheckResponse{Allowed: false}, nil
	}
	return &iamsdk.AdminCheckResponse{
		Allowed:      resp.Allowed,
		AssignmentID: resp.AssignmentID,
		DenialReason: resp.Reason,
	}, nil
}

// =============================================================================
// Impersonation
// =============================================================================

func handleIAMAdminStartImpersonation(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "StartImpersonation", adminResourceUsers, adminActionUpdate); err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.ImpersonationRequest](req, "Impersonation")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "StartImpersonation",
			"*iam.ImpersonationRequest", reflect.TypeOf(req).String())
	}

	// The acting admin is the AUTHENTICATED caller, resolved inside
	// StartImpersonation from the context (go-quicbaas#383) — never a
	// request-supplied value, so no actor id travels on the wire. The tenant
	// is pinned only for a tenant-scoped subject, since a system-scope admin
	// impersonating into a tenant is the legitimate cross-tenant case; an
	// unauthenticated caller is refused with ErrImpersonationActorRequired.
	tenantID, err := requireSubjectTenant(ctx, "StartImpersonation", r.TenantID)
	if err != nil {
		return nil, err
	}

	targetUserID, err := parseIAMUserID("quicbaas.IAM.Admin", "StartImpersonation", r.TargetUserID)
	if err != nil {
		return nil, err
	}
	session, err := mgr.StartImpersonation(ctx, &iam.StartImpersonationOptions{
		TargetUserID: targetUserID,
		TenantID:     tenantID,
		Reason:       r.Reason,
		Duration:     r.Duration,
	})
	if err != nil {
		return nil, err
	}
	return impersonationSessionToSDK(session), nil
}

func handleIAMAdminEndImpersonation(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "EndImpersonation", adminResourceUsers, adminActionUpdate); err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "EndImpersonation",
			"struct with SessionID field", reflect.TypeOf(req).String())
	}

	// AdminManager validates that the session exists and is active, but not
	// that it belongs to the caller. users:update is a generic grant, so
	// without this a tenant-scoped admin who obtained a session id could end
	// another admin's — or another tenant's — live impersonation.
	subject, scoped, err := tenantScopedSubject(ctx, "quicbaas.IAM.Admin", "EndImpersonation")
	if err != nil {
		return nil, err
	}
	if scoped {
		session, getErr := mgr.GetImpersonationSession(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		if err := requireImpersonationOwner(subject, session, "EndImpersonation"); err != nil {
			return nil, err
		}
	}

	if err := mgr.EndImpersonation(ctx, id); err != nil {
		return nil, err
	}
	return nil, nil
}

// requireImpersonationOwner enforces that the impersonation session belongs to
// the authenticated admin, in their own tenant. Both halves matter: the admin
// id alone would still let a caller reach across tenants if ids collide, and
// the tenant alone would let one admin end a peer's session.
func requireImpersonationOwner(subject interfaces.Subject, session *iam.ImpersonationSession, method string) error {
	if session == nil || subject.UserID == 0 || session.AdminUserID != subject.UserID || session.TenantID != subject.TenantID {
		return NewPermissionDeniedError("quicbaas.IAM.Admin", method, "access denied")
	}
	return nil
}

func handleIAMAdminListActiveImpersonations(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := adminManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := requireAdminPermission(ctx, svc, "ListActiveImpersonations", adminResourceUsers, adminActionRead); err != nil {
		return nil, err
	}
	requested, ok := iamRequestField(req, "AdminUserID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", "ListActiveImpersonations",
			"struct with AdminUserID field", reflect.TypeOf(req).String())
	}
	var adminUserID uint64
	if requested != "" {
		adminUserID, err = parseIAMUserID("quicbaas.IAM.Admin", "ListActiveImpersonations", requested)
		if err != nil {
			return nil, err
		}
	}

	// The storage API is keyed by admin id alone and applies no tenant filter,
	// so a subject-bound caller is pinned to its own admin id and its results
	// are narrowed to its own tenant. Otherwise users:read would let a caller
	// enumerate other admins' — and other tenants' — impersonation metadata.
	subject, scoped, err := tenantScopedSubject(ctx, "quicbaas.IAM.Admin", "ListActiveImpersonations")
	if err != nil {
		return nil, err
	}
	if scoped {
		if subject.UserID == 0 || (adminUserID != 0 && adminUserID != subject.UserID) {
			return nil, NewPermissionDeniedError("quicbaas.IAM.Admin", "ListActiveImpersonations",
				"admin_user_id does not match the authenticated subject")
		}
		adminUserID = subject.UserID
	}

	sessions, err := mgr.GetActiveImpersonations(ctx, adminUserID)
	if err != nil {
		return nil, err
	}

	out := make([]iamsdk.ImpersonationSession, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if scoped && s.TenantID != subject.TenantID {
			continue
		}
		out = append(out, *impersonationSessionToSDK(s))
	}
	return &iamsdk.ListImpersonationsResponse{
		Sessions:   out,
		TotalCount: len(out),
	}, nil
}

// =============================================================================
// Conversions
// =============================================================================

// adminTenantIDs resolves the tenant an assignment applies to. The SDK request
// can name a tenant twice — once in TenantID and once in a tenant-scoped
// ScopeID — and the two disagreeing is an ambiguous grant, not a request for
// both: taking the union would hand the admin a tenant the caller never
// deliberately named. An ambiguous request is refused instead.
//
// A group- or project-scoped ScopeID is not a tenant id at all and is ignored,
// because adding it to TenantIDs would scope the grant to a tenant that does
// not exist.
func adminTenantIDs(r *iamsdk.CreateAdminAssignmentRequest) ([]string, error) {
	scopeID := ""
	if r.Scope == iamsdk.AdminScopeTenant {
		scopeID = r.ScopeID
	}

	switch {
	case r.TenantID != "" && scopeID != "" && r.TenantID != scopeID:
		return nil, NewPermissionDeniedError("quicbaas.IAM.Admin", "CreateAssignment",
			"ambiguous tenant scope: tenant_id and the tenant-scoped scope_id name different tenants")
	case r.TenantID != "":
		return []string{r.TenantID}, nil
	case scopeID != "":
		return []string{scopeID}, nil
	default:
		return nil, nil
	}
}

// adminPermissions converts SDK permission strings to the domain's typed
// permission values. It returns nil for an empty input so the manager sees
// "no explicit permissions" (role defaults apply) rather than an empty grant.
func adminPermissions(in []string) []iam.AdminPermission {
	if len(in) == 0 {
		return nil
	}
	out := make([]iam.AdminPermission, 0, len(in))
	for _, p := range in {
		if p == "" {
			continue
		}
		out = append(out, iam.AdminPermission(p))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resourceScopesFromFilters converts the SDK's resource filters to the
// domain's resource scopes. ResourceScope carries a type and a pattern only:
// the filter's Tags have no domain counterpart, and encoding them into the
// pattern would change what the pattern matches. Accepting Tags and dropping
// them would store a constraint weaker than the one the caller asked for, so a
// filter that carries Tags is refused rather than silently widened.
func resourceScopesFromFilters(method string, filters []iamsdk.ResourceFilter) ([]iam.ResourceScope, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	out := make([]iam.ResourceScope, 0, len(filters))
	for _, f := range filters {
		if len(f.Tags) > 0 {
			return nil, NewInvalidRequestTypeError("quicbaas.IAM.Admin", method,
				"resource filter without Tags (tag constraints are not enforceable)",
				"resource filter with "+strconv.Itoa(len(f.Tags))+" tag(s)")
		}
		out = append(out, iam.ResourceScope{
			ResourceType:    f.Type,
			ResourcePattern: f.Pattern,
		})
	}
	return out, nil
}

// adminAssignmentToSDK converts a domain admin assignment to the SDK type.
// The domain holds the role name in AdminRole, which the SDK exposes as
// RoleID/RoleName.
//
// Scope is derived from the assignment's tenant list rather than guessed from
// the role: an assignment with no tenants is global (its permissions apply
// across all tenants) and one with tenants is tenant-scoped. Leaving Scope at
// its zero value would report every tenant-scoped grant as AdminScopeGlobal,
// which reads as strictly more privilege than the assignment actually carries.
//
// The SDK type holds a single TenantID, so an assignment spanning several
// tenants reports its first; the full list is not representable here. Every
// assignment this transport creates is single-tenant (see adminTenantIDs),
// so the truncation can only surface for a multi-tenant grant written through
// another surface.
func adminAssignmentToSDK(a *iam.AdminAssignment) *iamsdk.AdminAssignment {
	if a == nil {
		return nil
	}
	out := &iamsdk.AdminAssignment{
		ID:            a.ID,
		AdminID:       strconv.FormatUint(a.UserID, 10),
		Permissions:   permissionStrings(a.Permissions),
		RoleID:        string(a.AdminRole),
		RoleName:      string(a.AdminRole),
		Scope:         iamsdk.AdminScopeGlobal,
		Status:        adminAssignmentStatus(a),
		Active:        a.Active,
		CreatedAt:     a.GrantedAt,
		CreatedBy:     a.GrantedBy,
		UpdatedAt:     a.GrantedAt,
		Metadata:      a.Metadata,
		Justification: a.Metadata["justification"],
	}
	if len(a.TenantIDs) > 0 {
		out.Scope = iamsdk.AdminScopeTenant
		out.TenantID = a.TenantIDs[0]
		out.ScopeID = a.TenantIDs[0]
	}
	if a.ExpiresAt != nil {
		out.ExpiresAt = *a.ExpiresAt
	}
	if a.LastUsedAt != nil {
		out.LastUsedAt = *a.LastUsedAt
	}
	return out
}

// adminAssignmentStatus maps the domain's Active flag and expiry onto the
// SDK's status enum. An assignment past its expiry reports expired even when
// the stored flag still says active, so a caller reading the status never
// sees a grant the manager would refuse to honor.
func adminAssignmentStatus(a *iam.AdminAssignment) iamsdk.AdminAssignmentStatus {
	if a.ExpiresAt != nil && a.ExpiresAt.Before(time.Now()) {
		return iamsdk.AdminAssignmentStatusExpired
	}
	if !a.Active {
		return iamsdk.AdminAssignmentStatusSuspended
	}
	return iamsdk.AdminAssignmentStatusActive
}

// permissionStrings converts typed domain permissions back to the SDK's
// string form.
func permissionStrings(in []iam.AdminPermission) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, string(p))
	}
	return out
}

// impersonationSessionToSDK converts a domain impersonation session to the
// SDK type. Active is derived from EndedAt rather than stored separately: a
// session with no end timestamp is still running.
func impersonationSessionToSDK(s *iam.ImpersonationSession) *iamsdk.ImpersonationSession {
	if s == nil {
		return nil
	}
	out := &iamsdk.ImpersonationSession{
		ID:             s.ID,
		AdminID:        strconv.FormatUint(s.AdminUserID, 10),
		AdminName:      s.AdminUsername,
		TargetUserID:   strconv.FormatUint(s.TargetUserID, 10),
		TargetUserName: s.TargetUsername,
		TenantID:       s.TenantID,
		Reason:         s.Reason,
		StartedAt:      s.StartedAt,
		ExpiresAt:      s.ExpiresAt,
		Active:         s.EndedAt == nil,
	}
	if s.EndedAt != nil {
		out.EndedAt = *s.EndedAt
	}
	return out
}
