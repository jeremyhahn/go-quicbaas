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
	"strconv"
	"time"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	pepsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// IAM dispatch — accessors
// =============================================================================
//
// service.QuicBaaS does not declare IAM accessor methods directly on the
// public interface; the concrete *quicBaaS implementation exposes
// GetTenantManager / GetIdentityStore / GetPolicyEngine / GetAppManager via
// structural type assertions used elsewhere in QuicBaaS (see
// pkg/api/rest/iam_*.go for the same pattern). The embedded transport
// uses identical structural assertions so callers do not need to depend on
// concrete types.

type tenantManagerProvider interface {
	GetTenantManager(ctx context.Context) (iam.TenantManager, error)
}

type identityStoreProvider interface {
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
}

type inviteStoreProvider interface {
	GetInviteStore(ctx context.Context) (iam.InviteStore, error)
}

func tenantManagerFor(ctx context.Context, svc service.QuicBaaS) (iam.TenantManager, error) {
	p, ok := svc.(tenantManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Tenant")
	}
	mgr, err := p.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError("IAM.Tenant")
	}
	return mgr, nil
}

func identityStoreFor(ctx context.Context, svc service.QuicBaaS) (*iam.IdentityStore, error) {
	p, ok := svc.(identityStoreProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Identity")
	}
	store, err := p.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, NewServiceNotConfiguredError("IAM.Identity")
	}
	return store, nil
}

// requestPath returns the value of the Path field on req via reflection.
// Several SDK request types are unexported; this avoids compile-time
// coupling to those types while still extracting the data we need.
func iamRequestField(req interface{}, name string) (string, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// iamRequestPayload extracts the *T an SDK sub-client wraps inside its
// transport request. Several of those wrapper structs are unexported (e.g.
// iam.createAdminAssignmentRequest), so the embedded transport cannot name
// their type — but their fields are exported, so reflection reaches the
// payload without the SDK having to widen its API surface. name is the field
// (or embedded type) holding the payload; a request that is already a *T is
// returned as-is, which is the shape the tenant handlers receive.
func iamRequestPayload[T any](req interface{}, name string) (*T, bool) {
	if typed, ok := req.(*T); ok {
		return typed, typed != nil
	}
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || !f.CanInterface() {
		return nil, false
	}
	typed, ok := f.Interface().(*T)
	if !ok || typed == nil {
		return nil, false
	}
	return typed, true
}

// =============================================================================
// Tenant handlers (quicbaas.IAM.Tenant/*)
// =============================================================================

func handleIAMTenantCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := tenantManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*iamsdk.CreateTenantRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Tenant", "Create",
			"*iam.CreateTenantRequest", typeNameOf(req))
	}
	// EngineQuotas is the limits-only create-time view; Quotas() widens it to
	// the engine struct with every usage counter left at zero, which is what
	// the tenant manager expects.
	t, err := mgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:                 r.ID,
		Name:               r.Name,
		IsolationType:      r.IsolationType,
		EngineQuotas:       r.EngineQuotas.Quotas(),
		EngineEntitlements: r.EngineEntitlements,
	})
	if err != nil {
		return nil, err
	}
	return tenantToSDK(t), nil
}

func handleIAMTenantGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := tenantManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "TenantID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Tenant", "Get",
			"struct with TenantID field", typeNameOf(req))
	}
	t, err := mgr.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	return tenantToSDK(t), nil
}

func handleIAMTenantUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := tenantManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	innerField := v.FieldByName("Request")
	var sdkReq *iamsdk.UpdateTenantRequest
	if innerField.IsValid() && !innerField.IsNil() {
		if r, ok := innerField.Interface().(*iamsdk.UpdateTenantRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		// Direct *iamsdk.UpdateTenantRequest call (rare).
		if r, ok := req.(*iamsdk.UpdateTenantRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Tenant", "Update",
			"*iam.UpdateTenantRequest", typeNameOf(req))
	}
	name := sdkReq.DisplayName
	opts := iam.UpdateTenantOptions{}
	if name != "" {
		opts.Name = &name
	}
	t, err := mgr.UpdateTenant(ctx, tenantID, opts)
	if err != nil {
		return nil, err
	}
	return tenantToSDK(t), nil
}

func handleIAMTenantDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := tenantManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "TenantID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Tenant", "Delete",
			"struct with TenantID field", typeNameOf(req))
	}
	if err := mgr.DeleteTenant(ctx, id); err != nil {
		return nil, err
	}
	// SDK transport returns the unexported deleteTenantResponse{} sentinel.
	return nil, nil
}

func handleIAMTenantList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := tenantManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	pageSize := 0
	if r, ok := req.(*iamsdk.ListTenantsRequest); ok {
		pageSize = r.PageSize
	}
	tenants, err := mgr.ListTenants(ctx, iam.ListOptions{Limit: pageSize})
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListTenantsResponse{Tenants: make([]iamsdk.Tenant, 0, len(tenants))}
	for _, t := range tenants {
		out.Tenants = append(out.Tenants, *tenantToSDK(t))
	}
	return out, nil
}

// iamServiceProvider is satisfied by the server's QuicBaaS, which exposes the
// IAM service layer that owns tenant provisioning.
type iamServiceProvider interface {
	IAM() service.IAMService
}

// handleIAMTenantProvision creates a tenant and its first admin through the
// IAM service, the same service-layer path the REST and gRPC handlers use.
func handleIAMTenantProvision(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	p, ok := svc.(iamServiceProvider)
	if !ok || p.IAM() == nil {
		return nil, NewServiceNotConfiguredError("IAM.Tenant")
	}
	r, ok := req.(*iamsdk.ProvisionTenantRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Tenant", "Provision",
			"*iam.ProvisionTenantRequest", typeNameOf(req))
	}
	res, err := p.IAM().ProvisionTenant(ctx, &service.IAMProvisionTenantRequest{
		TenantID:          r.TenantID,
		Name:              r.Name,
		IsolationType:     r.IsolationType,
		AdminUsername:     r.AdminUsername,
		AdminEnrollment:   service.IAMTenantAdminEnrollment(r.AdminEnrollment),
		AdminCSRPEM:       r.AdminCSRPEM,
		InviteExpiryHours: r.InviteExpiryHours,
	})
	if err != nil {
		return nil, err
	}
	return &iamsdk.ProvisionTenantResponse{
		TenantID:            res.TenantID,
		AdminUserID:         strconv.FormatUint(res.AdminUserID, 10),
		AdminUsername:       res.AdminUsername,
		AdminRoleID:         res.AdminRoleID,
		AdminPending:        res.AdminPending,
		AdminCertPEM:        res.AdminCertPEM,
		AdminCertThumbprint: res.AdminCertThumbprint,
		InviteToken:         res.InviteToken,
		InviteExpiresAt:     res.InviteExpiresAt,
		Timestamp:           res.Timestamp,
	}, nil
}

func tenantToSDK(t *iam.Tenant) *iamsdk.Tenant {
	if t == nil {
		return nil
	}
	return &iamsdk.Tenant{
		ID:          t.ID,
		Name:        t.Name,
		DisplayName: t.Settings.DisplayName,
		CreatedAt:   t.Created,
		UpdatedAt:   t.Updated,
	}
}

// =============================================================================
// User handlers (quicbaas.IAM.User/*)
// =============================================================================

func handleIAMUserCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	var sdkReq *iamsdk.CreateUserRequest
	if inner := v.FieldByName("Request"); inner.IsValid() && !inner.IsNil() {
		if r, ok := inner.Interface().(*iamsdk.CreateUserRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		if r, ok := req.(*iamsdk.CreateUserRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.User", "Create",
			"*iam.CreateUserRequest", typeNameOf(req))
	}
	opts := iam.CreateUserOptions{
		Email:       sdkReq.Email,
		Username:    sdkReq.Username,
		Name:        sdkReq.DisplayName,
		AuthMethods: sdkReq.AuthMethods,
		Attributes:  metadataToAttributes(sdkReq.Metadata),
		// Roles are validated against the tenant's role store by
		// IdentityStore.CreateUser BEFORE the user record is written
		// (validate-before-create), matching the gRPC/Unix adapter contract.
		Roles: sdkReq.Roles,
		// Enabled is left unset: nil means enabled, so saying so adds nothing.
		Lifecycle: iam.Lifecycle(sdkReq.Lifecycle),
	}
	// A service without an invite store leaves invites nil, which
	// CreateInvitedUser refuses for a pending user before writing.
	var invites iam.InviteStore
	if p, ok := svc.(inviteStoreProvider); ok && opts.EffectiveLifecycle() == iam.LifecyclePending {
		if invites, err = p.GetInviteStore(ctx); err != nil {
			return nil, err
		}
	}
	inviteTTL := time.Duration(sdkReq.InviteExpiryHours) * time.Hour
	user, token, invite, err := store.CreateInvitedUser(ctx, tenantID, opts, invites, inviteTTL)
	if err != nil {
		return nil, err
	}
	resp := userToSDK(user)
	if token != "" {
		// In process there is no origin to build an activation link under;
		// the caller has the token and builds its own.
		resp.InviteToken = token
		resp.InviteExpiresAt = &invite.ExpiresAt
	}
	return resp, nil
}

// metadataToAttributes copies the SDK's metadata map into the identity store's
// attribute map. Both are map[string]string now, so this is a copy that cannot
// drop a value it was unable to represent. A nil or empty input yields nil so a
// user created without metadata matches one created without attributes.
func metadataToAttributes(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	attributes := make(map[string]string, len(metadata))
	for k, v := range metadata {
		attributes[k] = v
	}
	return attributes
}

func handleIAMUserGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	if userID == "" {
		userID, _ = iamRequestField(req, "ID")
	}
	if tenantID == "" || userID == "" {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.User", "Get",
			"struct with TenantID + UserID/ID fields", typeNameOf(req))
	}
	id, err := parseIAMUserID("quicbaas.IAM.User", "Get", userID)
	if err != nil {
		return nil, err
	}
	u, err := store.GetUser(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return userToSDK(u), nil
}

func handleIAMUserDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	if userID == "" {
		userID, _ = iamRequestField(req, "ID")
	}
	id, err := parseIAMUserID("quicbaas.IAM.User", "Delete", userID)
	if err != nil {
		return nil, err
	}
	if err := store.DeleteUser(ctx, tenantID, id); err != nil {
		return nil, err
	}
	// SDK isUserSuccessResponse accepts map[string]bool with success=true
	// or unexported delete response types; return the map shape since the
	// concrete delete struct is unexported.
	return map[string]bool{"success": true}, nil
}

func handleIAMUserList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	limit := 0
	if r, ok := req.(*iamsdk.ListUsersRequest); ok {
		limit = r.PageSize
	}
	users, err := store.ListUsers(ctx, tenantID, iam.ListOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListUsersResponse{Users: make([]iamsdk.User, 0, len(users))}
	for _, u := range users {
		out.Users = append(out.Users, *userToSDK(u))
	}
	return out, nil
}

func userToSDK(u *iam.User) *iamsdk.User {
	if u == nil {
		return nil
	}
	return &iamsdk.User{
		ID:          strconv.FormatUint(u.ID, 10),
		TenantID:    u.TenantID,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.Name,
		Roles:       u.Roles,
		AuthMethods: u.AuthMethods,
		Lifecycle:   string(u.Lifecycle),
		CreatedAt:   u.Created,
		UpdatedAt:   u.Updated,
	}
}

func handleIAMUserUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	if userID == "" {
		userID, _ = iamRequestField(req, "ID")
	}
	var sdkReq *iamsdk.UpdateUserRequest
	if r := iamRequestStructPtr(req, "Request"); r != nil {
		if ur, ok := r.(*iamsdk.UpdateUserRequest); ok {
			sdkReq = ur
		}
	}
	opts := iam.UpdateUserOptions{}
	if sdkReq != nil {
		if sdkReq.Email != "" {
			e := sdkReq.Email
			opts.Email = &e
		}
		if sdkReq.Username != "" {
			u := sdkReq.Username
			opts.Username = &u
		}
		if sdkReq.DisplayName != "" {
			n := sdkReq.DisplayName
			opts.Name = &n
		}
		// Presence, not emptiness: an explicitly empty list clears the
		// restriction, which is a different request from not mentioning it.
		if sdkReq.AuthMethods != nil {
			methods := append([]string(nil), *sdkReq.AuthMethods...)
			opts.AuthMethods = &methods
		}
		if sdkReq.Lifecycle != "" {
			state := iam.Lifecycle(sdkReq.Lifecycle)
			opts.Lifecycle = &state
		}
	}
	id, err := parseIAMUserID("quicbaas.IAM.User", "Update", userID)
	if err != nil {
		return nil, err
	}
	u, err := store.UpdateUser(ctx, tenantID, id, opts)
	if err != nil {
		return nil, err
	}
	return userToSDK(u), nil
}

// =============================================================================
// Role handlers (quicbaas.IAM.Role/*)
// =============================================================================

func handleIAMRoleList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	limit := 0
	if r, ok := req.(*iamsdk.ListRolesRequest); ok {
		limit = r.PageSize
	}
	roles, err := store.ListRoles(ctx, tenantID, iam.ListOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListRolesResponse{Roles: make([]iamsdk.Role, 0, len(roles))}
	for _, r := range roles {
		out.Roles = append(out.Roles, iamsdk.Role{
			ID:                 r.ID,
			Name:               r.Name,
			TenantID:           r.TenantID,
			Description:        r.Description,
			Permissions:        r.Permissions,
			RetiredPermissions: r.RetiredPermissions,
			CreatedAt:          r.Created,
			UpdatedAt:          r.Updated,
		})
	}
	return out, nil
}

func handleIAMRoleCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	var sdkReq *iamsdk.CreateRoleRequest
	if r := iamRequestStructPtr(req, "Request"); r != nil {
		if cr, ok := r.(*iamsdk.CreateRoleRequest); ok {
			sdkReq = cr
		}
	}
	if sdkReq == nil {
		if r, ok := req.(*iamsdk.CreateRoleRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Role", "Create",
			"*iam.CreateRoleRequest", typeNameOf(req))
	}
	r, err := store.CreateRole(ctx, tenantID, iam.CreateRoleOptions{
		ID:          sdkReq.ID,
		Name:        sdkReq.Name,
		Description: sdkReq.Description,
		Permissions: sdkReq.Permissions,
	})
	if err != nil {
		return nil, err
	}
	return &iamsdk.Role{
		ID:                 r.ID,
		Name:               r.Name,
		TenantID:           r.TenantID,
		Description:        r.Description,
		Permissions:        r.Permissions,
		RetiredPermissions: r.RetiredPermissions,
		CreatedAt:          r.Created,
		UpdatedAt:          r.Updated,
	}, nil
}

func handleIAMRoleUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	roleID, _ := iamRequestField(req, "RoleID")
	var sdkReq *iamsdk.UpdateRoleRequest
	if r := iamRequestStructPtr(req, "Request"); r != nil {
		if ur, ok := r.(*iamsdk.UpdateRoleRequest); ok {
			sdkReq = ur
		}
	}
	opts := iam.UpdateRoleOptions{}
	if sdkReq != nil {
		if sdkReq.Name != "" {
			n := sdkReq.Name
			opts.Name = &n
		}
		if sdkReq.Description != "" {
			d := sdkReq.Description
			opts.Description = &d
		}
		if len(sdkReq.Permissions) > 0 {
			p := sdkReq.Permissions
			opts.Permissions = &p
		}
	}
	r, err := store.UpdateRole(ctx, tenantID, roleID, opts)
	if err != nil {
		return nil, err
	}
	return &iamsdk.Role{
		ID:                 r.ID,
		Name:               r.Name,
		TenantID:           r.TenantID,
		Description:        r.Description,
		Permissions:        r.Permissions,
		RetiredPermissions: r.RetiredPermissions,
		CreatedAt:          r.Created,
		UpdatedAt:          r.Updated,
	}, nil
}

func handleIAMRoleDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	roleID, _ := iamRequestField(req, "RoleID")
	if err := store.DeleteRole(ctx, tenantID, roleID); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMRoleGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	roleID, _ := iamRequestField(req, "RoleID")
	if roleID == "" {
		roleID, _ = iamRequestField(req, "ID")
	}
	r, err := store.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	return &iamsdk.Role{
		ID:                 r.ID,
		Name:               r.Name,
		TenantID:           r.TenantID,
		Description:        r.Description,
		Permissions:        r.Permissions,
		RetiredPermissions: r.RetiredPermissions,
		CreatedAt:          r.Created,
		UpdatedAt:          r.Updated,
	}, nil
}

// handleIAMRoleReconcile converges the tenant's built-in roles onto the
// definitions the running build carries. It is the only path that can
// re-permission a built-in role: UpdateRole refuses a system role, so a
// tenant seeded by an older build stays on that build's permissions until
// this runs.
func handleIAMRoleReconcile(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	report, err := store.ReconcileBuiltInRoles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ReconcileRolesResponse{
		TenantID: report.TenantID,
		Roles:    make([]iamsdk.RoleReconcileResult, 0, len(report.Roles)),
		Changed:  report.Changed(),
	}
	for _, r := range report.Roles {
		out.Roles = append(out.Roles, iamsdk.RoleReconcileResult{
			RoleID:  r.RoleID,
			Name:    r.Name,
			Created: r.Created,
			Added:   r.Added,
			Removed: r.Removed,
		})
	}
	return out, nil
}

// =============================================================================
// Policy handlers (quicbaas.IAM.Policy/*)
// =============================================================================
//
// The SDK PolicyClient internal request types are unexported, so handlers
// extract TenantID / PolicyName / Request via reflection — same pattern
// the Tenant/User/Role handlers above use. The underlying
// *iam.PolicyEngine is shared with REST/CLI/gRPC; this just gives the
// embedded transport access to the same engine.

type policyEngineProvider interface {
	GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
}

func policyEngineFor(ctx context.Context, svc service.QuicBaaS) (*iam.PolicyEngine, error) {
	p, ok := svc.(policyEngineProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Policy")
	}
	pe, err := p.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, NewServiceNotConfiguredError("IAM.Policy")
	}
	return pe, nil
}

// iamRequestStructPtr extracts a *Type from req's "Request" field when the
// underlying value is a *Type matching the supplied target. Returns nil
// when the field is missing, nil, or of the wrong type. The target lets
// the caller specify the expected concrete pointer-to-struct type via a
// zero-value pointer (e.g. (*iamsdk.ListPoliciesRequest)(nil)).
func iamRequestStructPtr(req interface{}, fieldName string) interface{} {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return nil
	}
	if f.Kind() == reflect.Ptr && f.IsNil() {
		return nil
	}
	return f.Interface()
}

// sdkPolicyFromInternal converts the internal iam.Policy (apiVersion + spec
// shape) to the SDK Policy DTO. Only metadata fields the dashboard renders
// are populated; rules/targets/conditions are intentionally elided to keep
// the embedded path lightweight — full policy retrieval still goes through
// gRPC when callers need the full document.
func sdkPolicyFromInternal(p *iam.Policy) iamsdk.Policy {
	out := iamsdk.Policy{
		Name:        p.Metadata.Name,
		TenantID:    p.Metadata.Tenant,
		Description: p.Metadata.Description,
		Priority:    p.Metadata.Priority,
		CreatedAt:   p.Metadata.Created,
		UpdatedAt:   p.Metadata.Updated,
		Labels:      p.Metadata.Labels,
	}
	if p.LoadError != nil {
		out.LoadError = &iamsdk.PolicyLoadError{
			Reason:      p.LoadError.Reason,
			UnknownKeys: p.LoadError.UnknownKeys,
		}
	}
	return out
}

func handleIAMPolicyList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	limit := 0
	if r := iamRequestStructPtr(req, "Request"); r != nil {
		if lr, ok := r.(*iamsdk.ListPoliciesRequest); ok && lr != nil {
			limit = lr.Limit
		}
	}
	policies, err := pe.ListPolicies(ctx, tenantID, iam.ListOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListPoliciesResponse{Policies: make([]iamsdk.Policy, 0, len(policies))}
	for _, p := range policies {
		out.Policies = append(out.Policies, sdkPolicyFromInternal(p))
	}
	return out, nil
}

func handleIAMPolicyGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	policyName, _ := iamRequestField(req, "PolicyName")
	p, err := pe.GetPolicy(ctx, tenantID, policyName)
	if err != nil {
		return nil, err
	}
	out := sdkPolicyFromInternal(p)
	return &out, nil
}

func handleIAMPolicyCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	var sdkReq *iamsdk.CreatePolicyRequest
	if r := iamRequestStructPtr(req, "Request"); r != nil {
		if cr, ok := r.(*iamsdk.CreatePolicyRequest); ok {
			sdkReq = cr
		}
	}
	if sdkReq == nil {
		if r, ok := req.(*iamsdk.CreatePolicyRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Policy", "Create",
			"*iam.CreatePolicyRequest", typeNameOf(req))
	}
	// Structured conditions, targets and rule subjects have no
	// translation into the engine's policy. Refuse them: storing the
	// policy without them would grant more than the caller asked for.
	if err := checkNoUntranslatableFields(sdkReq); err != nil {
		return nil, err
	}
	meta := iam.PolicyMetadata{
		Name:        sdkReq.Name,
		Tenant:      tenantID,
		Description: sdkReq.Description,
		Priority:    sdkReq.Priority,
		Labels:      sdkReq.Labels,
	}
	// SpecYAML takes precedence over the individual fields, as on REST
	// and gRPC.
	var policy *iam.Policy
	if len(sdkReq.SpecYAML) > 0 {
		policy, err = iam.ParsePolicyYAML(sdkReq.SpecYAML, meta)
		if err != nil {
			return nil, err
		}
	} else {
		policy = &iam.Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   meta,
			Spec:       iam.PolicySpec{Rules: iamRulesFromSDK(sdkReq.Rules)},
		}
	}
	if err := pe.CreatePolicy(ctx, tenantID, policy); err != nil {
		return nil, err
	}
	out := sdkPolicyFromInternal(policy)
	return &out, nil
}

// checkNoUntranslatableFields refuses a create request that sets an SDK
// field the engine's policy has no equivalent for: structured conditions
// (policy or rule level), policy targets, or rule-level subjects. The
// engine selects subjects once per policy (spec.subjects), so a per-rule
// subject list cannot be carried over without changing who a rule covers.
func checkNoUntranslatableFields(req *iamsdk.CreatePolicyRequest) error {
	refuse := func(field string) error {
		return &UnsupportedPolicyFieldError{
			Service: "quicbaas.IAM.Policy", Method: "Create", Field: field,
		}
	}
	// The engine's policy has no policy-level effect: stored policies
	// deny by default, so a requested allow would be silently dropped.
	// This matches policyfields.Refuse on gRPC and MCP. Enabled is a plain
	// bool here, so false is indistinguishable from unset and true is the
	// stored state: there is nothing to refuse.
	if req.Effect != iamsdk.EffectDeny {
		return refuse("effect")
	}
	if len(req.Conditions) > 0 {
		return refuse("conditions")
	}
	if len(req.Targets) > 0 {
		return refuse("targets")
	}
	for i, r := range req.Rules {
		if len(r.Conditions) > 0 {
			return refuse("rules[" + strconv.Itoa(i) + "].conditions")
		}
		if len(r.Subjects) > 0 {
			return refuse("rules[" + strconv.Itoa(i) + "].subjects")
		}
	}
	return nil
}

// iamRulesFromSDK maps SDK rules into the engine's rules: the SDK matcher
// objects become the engine's flat pattern slices.
func iamRulesFromSDK(sdkRules []iamsdk.Rule) []iam.Rule {
	rules := make([]iam.Rule, 0, len(sdkRules))
	for _, r := range sdkRules {
		actions := make([]string, 0, len(r.Actions))
		for _, a := range r.Actions {
			actions = append(actions, a.Pattern)
		}
		resources := make([]string, 0, len(r.Resources))
		for _, res := range r.Resources {
			resources = append(resources, res.Pattern)
		}
		// SDK Effect is an int enum (0=Deny, 1=Allow); the engine
		// expects the lowercase string literal. Translate explicitly.
		var effect iam.Effect
		switch r.Effect {
		case iamsdk.EffectAllow:
			effect = iam.EffectAllow
		case iamsdk.EffectDeny:
			effect = iam.EffectDeny
		default:
			effect = iam.EffectAllow
		}
		rules = append(rules, iam.Rule{
			Name:      r.Name,
			Effect:    effect,
			Priority:  r.Priority,
			Actions:   actions,
			Resources: resources,
		})
	}
	return rules
}

func handleIAMPolicyDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	policyName, _ := iamRequestField(req, "PolicyName")
	if err := pe.DeletePolicy(ctx, tenantID, policyName); err != nil {
		return nil, err
	}
	return nil, nil
}

// =============================================================================
// App handlers (quicbaas.IAM.App/*)
// =============================================================================
//
// AppManager covers OIDC/OAuth2 client registration. The dashboard's
// /iam/apps page calls List; future passes can add Register/Update/Rotate
// once the WebUI grows the corresponding screens.

type appManagerProvider interface {
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
}

func appManagerFor(ctx context.Context, svc service.QuicBaaS) (*iam.AppManager, error) {
	p, ok := svc.(appManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.App")
	}
	mgr, err := p.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError("IAM.App")
	}
	return mgr, nil
}

// tokenEndpointAuthMethodFromString maps the internal string-typed
// TokenEndpointAuthMethod onto the SDK's iota enum so the embedded
// transport can hand back well-typed Application DTOs without leaking
// internal types.
func tokenEndpointAuthMethodFromString(s string) iamsdk.TokenEndpointAuthMethod {
	switch s {
	case "client_secret_post":
		return iamsdk.TokenEndpointAuthClientSecretPost
	case "client_secret_jwt":
		return iamsdk.TokenEndpointAuthClientSecretJWT
	case "private_key_jwt":
		return iamsdk.TokenEndpointAuthPrivateKeyJWT
	case "none":
		return iamsdk.TokenEndpointAuthNone
	case "tls_client_auth":
		return iamsdk.TokenEndpointAuthTLSClientAuth
	case "self_signed_tls_client_auth":
		return iamsdk.TokenEndpointAuthSelfSignedTLSClientAuth
	default:
		return iamsdk.TokenEndpointAuthClientSecretBasic
	}
}

func sdkAppFromInternal(a *iam.Application) iamsdk.Application {
	return iamsdk.Application{
		ClientID:                a.ClientID,
		TenantID:                a.TenantID,
		Name:                    a.Name,
		RedirectURIs:            a.RedirectURIs,
		ResponseTypes:           a.ResponseTypes,
		TokenEndpointAuthMethod: tokenEndpointAuthMethodFromString(a.TokenEndpointAuthMethod),
		MTLSCertThumbprint:      a.MTLSCertThumbprint,
		MTLSSubjectDN:           a.MTLSSubjectDN,
		MTLSSANDNS:              a.MTLSSANDNS,
		CreatedAt:               a.Created,
		UpdatedAt:               a.Updated,
	}
}

// grantTypeToString maps the SDK's iota-based GrantType into the
// internal AppManager's string representation.
func grantTypeToString(g iamsdk.GrantType) string {
	switch g {
	case iamsdk.GrantTypeAuthorizationCode:
		return "authorization_code"
	case iamsdk.GrantTypeClientCredentials:
		return "client_credentials"
	case iamsdk.GrantTypeRefreshToken:
		return "refresh_token"
	case iamsdk.GrantTypeDeviceCode:
		return "urn:ietf:params:oauth:grant-type:device_code"
	case iamsdk.GrantTypeImplicit:
		return "implicit"
	case iamsdk.GrantTypePassword:
		return "password"
	default:
		return "authorization_code"
	}
}

// appTenant resolves the tenant an embedded app verb acts in: the tenant the
// call is bound to. A request naming another tenant is refused, and the
// system tenant gets no lift (go-quicbaas#1312).
func appTenant(ctx context.Context, method string, req interface{}) (string, error) {
	requested, _ := iamRequestField(req, "TenantID")
	return tenantctx.Target(ctx, "quicbaas.IAM.App/"+method, requested)
}

func handleIAMAppRegister(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "Register", req)
	if err != nil {
		return nil, err
	}
	var sdkReq *iamsdk.RegisterAppRequest
	if r := iamRequestStructPtr(req, "App"); r != nil {
		if rr, ok := r.(*iamsdk.RegisterAppRequest); ok {
			sdkReq = rr
		}
	}
	if sdkReq == nil {
		if r, ok := req.(*iamsdk.RegisterAppRequest); ok {
			sdkReq = r
		}
	}
	if sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.App", "Register",
			"*iam.RegisterAppRequest", typeNameOf(req))
	}
	grants := make([]string, 0, len(sdkReq.GrantTypes))
	for _, g := range sdkReq.GrantTypes {
		grants = append(grants, grantTypeToString(g))
	}
	a, err := mgr.RegisterApp(ctx, tenantID, iam.RegisterAppOptions{
		Name:           sdkReq.Name,
		RedirectURIs:   sdkReq.RedirectURIs,
		GrantTypes:     grants,
		ResponseTypes:  sdkReq.ResponseTypes,
		ResourcePrefix: "",
	})
	if err != nil {
		return nil, err
	}
	out := sdkAppFromInternal(a)
	return &out, nil
}

func handleIAMAppList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "List", req)
	if err != nil {
		return nil, err
	}
	apps, err := mgr.ListApps(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListAppsResponse{
		Applications: make([]iamsdk.Application, 0, len(apps)),
		TotalCount:   len(apps),
	}
	for _, a := range apps {
		out.Applications = append(out.Applications, sdkAppFromInternal(a))
	}
	return out, nil
}

func handleIAMAppGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "Get", req)
	if err != nil {
		return nil, err
	}
	clientID, _ := iamRequestField(req, "ClientID")
	a, err := mgr.GetAppInTenant(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}
	out := sdkAppFromInternal(a)
	return &out, nil
}

func handleIAMAppDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "Delete", req)
	if err != nil {
		return nil, err
	}
	clientID, _ := iamRequestField(req, "ClientID")
	if err := mgr.DeleteAppInTenant(ctx, tenantID, clientID); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMAppUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := iamRequestStructPtr(req, "App").(*iamsdk.UpdateAppRequest)
	if !ok || sdkReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.App", "Update",
			"*iam.updateAppRequest", typeNameOf(req))
	}
	clientID, ok := iamRequestField(req, "ClientID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.App", "Update",
			"*iam.updateAppRequest", typeNameOf(req))
	}
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "Update", req)
	if err != nil {
		return nil, err
	}
	// Only non-zero fields are applied, so an unset field keeps its value.
	opts := iam.UpdateAppOptions{}
	if sdkReq.Name != "" {
		opts.Name = &sdkReq.Name
	}
	if len(sdkReq.RedirectURIs) > 0 {
		opts.RedirectURIs = &sdkReq.RedirectURIs
	}
	if sdkReq.ResourcePrefix != "" {
		opts.ResourcePrefix = &sdkReq.ResourcePrefix
	}
	if len(sdkReq.GrantTypes) > 0 {
		grants := make([]string, 0, len(sdkReq.GrantTypes))
		for _, g := range sdkReq.GrantTypes {
			grants = append(grants, grantTypeToString(g))
		}
		opts.GrantTypes = &grants
	}
	if len(sdkReq.ResponseTypes) > 0 {
		opts.ResponseTypes = &sdkReq.ResponseTypes
	}
	a, err := mgr.UpdateAppInTenant(ctx, tenantID, clientID, opts)
	if err != nil {
		return nil, err
	}
	out := sdkAppFromInternal(a)
	return &out, nil
}

func handleIAMAppRotateSecret(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	clientID, ok := iamRequestField(req, "ClientID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.App", "RotateSecret",
			"*iam.rotateSecretRequest", typeNameOf(req))
	}
	mgr, err := appManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, err := appTenant(ctx, "RotateSecret", req)
	if err != nil {
		return nil, err
	}
	a, err := mgr.RotateSecretInTenant(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}
	return &iamsdk.RotateSecretResponse{
		ClientID:     a.ClientID,
		ClientSecret: a.ClientSecret,
	}, nil
}

// =============================================================================
// PEP handlers (quicbaas.IAM.PEP/*)
// =============================================================================

// handleIAMPEPCheck delegates to the PEP service when configured. The PEP
// surface in the embedded build is intentionally minimal — production
// authorisation goes via the gRPC PEP path; embedded callers receive a
// "not configured" error when the PEP isn't wired so they can degrade.
func handleIAMPEPCheck(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	_, ok := req.(*pepsdk.CheckRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.PEP", "Check",
			"*iam.CheckRequest", typeNameOf(req))
	}
	// PEP enforcement requires a fully wired policy engine; embedded path
	// surfaces a "not configured" envelope rather than silently allowing.
	return nil, NewServiceNotConfiguredError("IAM.PEP")
}

// =============================================================================
// WebAuthn credential administration
// =============================================================================

// webAuthnCredentialsProvider is the structural accessor for the shared
// user-scoped WebAuthn credential administration service. It is satisfied by
// the concrete *quicBaaS, matching how the IAM accessors above reach the
// identity store and tenant manager.
type webAuthnCredentialsProvider interface {
	WebAuthnCredentials() service.WebAuthnCredentialService
}

// webAuthnCredentialsFor resolves the credential administration service. The
// embedded transport deliberately does NOT re-derive the user-id-to-email
// translation the verbs need: that lives once in pkg/service so every
// transport addresses the same account for the same request.
func webAuthnCredentialsFor(svc service.QuicBaaS) (service.WebAuthnCredentialService, error) {
	p, ok := svc.(webAuthnCredentialsProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.WebAuthn")
	}
	creds := p.WebAuthnCredentials()
	if creds == nil {
		return nil, NewServiceNotConfiguredError("IAM.WebAuthn")
	}
	return creds, nil
}

func handleIAMWebAuthnCredentialsList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	creds, err := webAuthnCredentialsFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	id, err := parseIAMUserID("quicbaas.IAM.WebAuthn", "ListCredentials", userID)
	if err != nil {
		return nil, err
	}
	list, err := creds.ListCredentials(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	// The SDK client decodes {"credentials": [...], "total": n}; return that
	// shape so the embedded transport and REST agree on the wire contract.
	out := make([]map[string]interface{}, 0, len(list))
	for _, c := range list {
		out = append(out, webAuthnCredentialToMap(c))
	}
	return map[string]interface{}{"credentials": out, "total": len(out)}, nil
}

func handleIAMWebAuthnCredentialDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	creds, err := webAuthnCredentialsFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	credentialID, _ := iamRequestField(req, "CredentialID")
	id, err := parseIAMUserID("quicbaas.IAM.WebAuthn", "DeleteCredential", userID)
	if err != nil {
		return nil, err
	}
	if err := creds.DeleteCredential(ctx, tenantID, id, credentialID); err != nil {
		return nil, err
	}
	return map[string]bool{"success": true}, nil
}

func handleIAMWebAuthnCredentialsDeleteAll(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	creds, err := webAuthnCredentialsFor(svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	id, err := parseIAMUserID("quicbaas.IAM.WebAuthn", "DeleteCredentials", userID)
	if err != nil {
		return nil, err
	}
	deleted, err := creds.DeleteCredentials(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"deleted": deleted}, nil
}

// webAuthnCredentialToMap projects a credential summary onto the JSON shape the
// SDK client decodes. The field names are the summary type's own json tags.
func webAuthnCredentialToMap(c iam.WebAuthnCredentialSummary) map[string]interface{} {
	m := map[string]interface{}{
		"id":              c.ID,
		"sign_count":      c.SignCount,
		"backup_eligible": c.BackupEligible,
		"backup_state":    c.BackupState,
		"clone_warning":   c.CloneWarning,
	}
	if c.AAGUID != "" {
		m["aaguid"] = c.AAGUID
	}
	if c.AttestationType != "" {
		m["attestation_type"] = c.AttestationType
	}
	if len(c.Transports) > 0 {
		m["transports"] = c.Transports
	}
	return m
}
