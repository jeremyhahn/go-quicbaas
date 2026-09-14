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
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockQuicBaaSWithAdmin exposes a real *iam.AdminManager through the
// structural seam the embedded dispatch uses.
type mockQuicBaaSWithAdmin struct {
	service.QuicBaaS
	admin *iam.AdminManager
}

func (m *mockQuicBaaSWithAdmin) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.admin, nil
}

// Mirrors of the SDK sub-client's unexported transport envelopes: same field
// names, so the reflection-based payload extraction is exercised as it is in
// production.
type embeddedCreateAssignmentEnvelope struct {
	*iamsdk.CreateAdminAssignmentRequest
}

type embeddedAssignmentIDEnvelope struct {
	AssignmentID string
}

type embeddedUpdateAssignmentEnvelope struct {
	AssignmentID string
	Update       *iamsdk.UpdateAdminAssignmentRequest
}

type embeddedListAssignmentsEnvelope struct {
	Filter *iamsdk.ListAdminAssignmentsRequest
}

type embeddedAdminCheckEnvelope struct {
	Check *iamsdk.AdminCheckRequest
}

type embeddedStartImpersonationEnvelope struct {
	Impersonation *iamsdk.ImpersonationRequest
}

type embeddedImpersonationSessionEnvelope struct {
	SessionID string
}

type embeddedListImpersonationsEnvelope struct {
	AdminUserID string
}

// embeddedAllowAllTargetResolver resolves every (tenant, user) pair so handler
// tests can exercise impersonation mechanics; target verification itself
// is pinned in pkg/iam's tenant-scope suite (#357).
type embeddedAllowAllTargetResolver struct{}

func (embeddedAllowAllTargetResolver) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	return &iam.User{ID: userID, TenantID: tenantID}, nil
}

// newAdminTestManager builds an AdminManager over in-memory storage. The
// target resolver is not optional: StartImpersonation fails closed without
// one (ErrImpersonationTargetResolverRequired, #357), exactly as the
// production manager built in pkg/server/iam_init.go does.
func newAdminTestManager(t *testing.T) *iam.AdminManager {
	t.Helper()
	mgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
		TargetResolver:       embeddedAllowAllTargetResolver{},
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}
	return mgr
}

func newAdminTestService(t *testing.T) (*mockQuicBaaSWithAdmin, *iam.AdminManager) {
	t.Helper()
	mgr := newAdminTestManager(t)
	return &mockQuicBaaSWithAdmin{admin: mgr}, mgr
}

// TestIAMAdminDispatchRegistered proves the gap in the issue is closed:
// delegated administration is reachable from the embedded transport, under
// both spellings of the impersonation-list method the gRPC handler accepts.
func TestIAMAdminDispatchRegistered(t *testing.T) {
	for _, method := range []string{
		"quicbaas.IAM.Admin/CreateAssignment",
		"quicbaas.IAM.Admin/GetAssignment",
		"quicbaas.IAM.Admin/UpdateAssignment",
		"quicbaas.IAM.Admin/DeleteAssignment",
		"quicbaas.IAM.Admin/ListAssignments",
		"quicbaas.IAM.Admin/CheckPermission",
		"quicbaas.IAM.Admin/StartImpersonation",
		"quicbaas.IAM.Admin/EndImpersonation",
		"quicbaas.IAM.Admin/ListActiveImpersonations",
		"quicbaas.IAM.Admin/GetActiveImpersonations",
	} {
		if _, ok := unaryHandlers[method]; !ok {
			t.Errorf("%s is not registered in the embedded dispatch table", method)
		}
	}
}

func createSuperAdmin(t *testing.T, svc *mockQuicBaaSWithAdmin, userID, tenantID string) *iamsdk.AdminAssignment {
	t.Helper()
	out, err := handleIAMAdminCreateAssignment(context.Background(), svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:   userID,
			TenantID:  tenantID,
			RoleID:    string(iam.AdminRoleSuperAdmin),
			GrantedBy: "bootstrap",
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}
	assignment, ok := out.(*iamsdk.AdminAssignment)
	if !ok {
		t.Fatalf("expected *iam.AdminAssignment, got %T", out)
	}
	return assignment
}

func TestHandleIAMAdminCreateAssignment(t *testing.T) {
	svc, mgr := newAdminTestService(t)
	ctx := context.Background()

	expiry := time.Now().Add(24 * time.Hour).UTC()
	out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:     "admin-1",
			TenantID:    "t-1",
			Scope:       iamsdk.AdminScopeTenant,
			ScopeID:     "t-1",
			RoleID:      string(iam.AdminRoleTenantAdmin),
			Permissions: []string{string(iam.PermUserRead)},
			GrantedBy:   "owner",
			ExpiresAt:   expiry,
			Metadata:    map[string]string{"ticket": "OPS-1"},
			Constraints: &iamsdk.AdminConstraints{
				DeniedOperations: []string{string(iam.PermUserDelete)},
				ResourceFilters: []iamsdk.ResourceFilter{
					{Type: "user", Pattern: "user:*"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}
	got, ok := out.(*iamsdk.AdminAssignment)
	if !ok {
		t.Fatalf("expected *iam.AdminAssignment, got %T", out)
	}
	if got.AdminID != "admin-1" {
		t.Errorf("AdminID = %q, want \"admin-1\"", got.AdminID)
	}
	if got.RoleID != string(iam.AdminRoleTenantAdmin) {
		t.Errorf("RoleID = %q, want %q", got.RoleID, iam.AdminRoleTenantAdmin)
	}
	if got.Status != iamsdk.AdminAssignmentStatusActive {
		t.Errorf("Status = %v, want active", got.Status)
	}

	// The persisted record is where a dropped field would show up.
	stored, err := mgr.GetAssignment(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if len(stored.TenantIDs) != 1 || stored.TenantIDs[0] != "t-1" {
		t.Errorf("TenantIDs = %v, want [t-1]", stored.TenantIDs)
	}
	// A tenant-scoped assignment must not report itself as global: that would
	// read as strictly more privilege than the grant carries.
	if got.Scope != iamsdk.AdminScopeTenant || got.ScopeID != "t-1" {
		t.Errorf("Scope = %v, ScopeID = %q; want tenant-scoped on t-1", got.Scope, got.ScopeID)
	}
	if len(stored.Permissions) != 1 || stored.Permissions[0] != iam.PermUserRead {
		t.Errorf("Permissions = %v, want [%s]", stored.Permissions, iam.PermUserRead)
	}
	if len(stored.DeniedPermissions) != 1 || stored.DeniedPermissions[0] != iam.PermUserDelete {
		t.Errorf("DeniedPermissions = %v, want [%s]", stored.DeniedPermissions, iam.PermUserDelete)
	}
	if len(stored.ResourceScopes) != 1 || stored.ResourceScopes[0].ResourcePattern != "user:*" {
		t.Errorf("ResourceScopes = %v, want one user:* scope", stored.ResourceScopes)
	}
	if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(expiry) {
		t.Errorf("ExpiresAt = %v, want %v", stored.ExpiresAt, expiry)
	}
	if stored.GrantedBy != "owner" {
		t.Errorf("GrantedBy = %q, want \"owner\"", stored.GrantedBy)
	}
}

// A non-tenant scope id is not a tenant id, so it must not become one.
func TestHandleIAMAdminCreateAssignment_NonTenantScopeIDIsNotATenant(t *testing.T) {
	svc, mgr := newAdminTestService(t)
	ctx := context.Background()

	out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:   "admin-1",
			TenantID:  "t-1",
			Scope:     iamsdk.AdminScopeGroup,
			ScopeID:   "group-42",
			RoleID:    string(iam.AdminRoleGroupAdmin),
			GrantedBy: "owner",
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}

	stored, err := mgr.GetAssignment(ctx, out.(*iamsdk.AdminAssignment).ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if len(stored.TenantIDs) != 1 || stored.TenantIDs[0] != "t-1" {
		t.Errorf("TenantIDs = %v, want only [t-1]; a group scope id is not a tenant", stored.TenantIDs)
	}
}

func TestHandleIAMAdminCreateAssignment_Errors(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without the payload field", req: struct{ Other string }{}},
		{name: "nil payload", req: &embeddedCreateAssignmentEnvelope{}},
		{
			name: "missing admin role",
			req: &embeddedCreateAssignmentEnvelope{CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
				AdminID: "admin-1", GrantedBy: "owner",
			}},
		},
		{
			name: "unknown admin role",
			req: &embeddedCreateAssignmentEnvelope{CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
				AdminID: "admin-1", RoleID: "emperor", GrantedBy: "owner",
			}},
		},
		{
			name: "missing granting principal",
			req: &embeddedCreateAssignmentEnvelope{CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
				AdminID: "admin-1", RoleID: string(iam.AdminRoleReadOnly),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMAdminCreateAssignment(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHandleIAMAdminGetAndDeleteAssignment(t *testing.T) {
	svc, mgr := newAdminTestService(t)
	ctx := context.Background()
	created := createSuperAdmin(t, svc, "admin-1", "t-1")

	out, err := handleIAMAdminGetAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: created.ID})
	if err != nil {
		t.Fatalf("handleIAMAdminGetAssignment failed: %v", err)
	}
	if got := out.(*iamsdk.AdminAssignment); got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}

	if _, err := handleIAMAdminDeleteAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: created.ID}); err != nil {
		t.Fatalf("handleIAMAdminDeleteAssignment failed: %v", err)
	}
	if _, err := mgr.GetAssignment(ctx, created.ID); err == nil {
		t.Error("expected the assignment to be gone after delete")
	}
}

func TestHandleIAMAdminGetAssignment_Errors(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	if _, err := handleIAMAdminGetAssignment(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a request with no AssignmentID field")
	}
	if _, err := handleIAMAdminGetAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: "missing"}); err == nil {
		t.Error("expected an error for an unknown assignment")
	}
	if _, err := handleIAMAdminDeleteAssignment(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a delete with no AssignmentID field")
	}
}

// The SDK models assignment lifecycle as a status enum; only "active" maps to
// the domain's Active flag.
func TestHandleIAMAdminUpdateAssignment_StatusMapsToActive(t *testing.T) {
	svc, mgr := newAdminTestService(t)
	ctx := context.Background()
	created := createSuperAdmin(t, svc, "admin-1", "t-1")

	suspended := iamsdk.AdminAssignmentStatusSuspended
	out, err := handleIAMAdminUpdateAssignment(ctx, svc, &embeddedUpdateAssignmentEnvelope{
		AssignmentID: created.ID,
		Update: &iamsdk.UpdateAdminAssignmentRequest{
			Status:      &suspended,
			Permissions: []string{string(iam.PermUserRead)},
			Metadata:    map[string]string{"reason": "offboarding"},
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminUpdateAssignment failed: %v", err)
	}
	if got := out.(*iamsdk.AdminAssignment); got.Status != iamsdk.AdminAssignmentStatusSuspended || got.Active {
		t.Errorf("Status = %v, Active = %v; want suspended and inactive", got.Status, got.Active)
	}

	stored, err := mgr.GetAssignment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if stored.Active {
		t.Error("expected the stored assignment to be inactive")
	}

	active := iamsdk.AdminAssignmentStatusActive
	if _, err := handleIAMAdminUpdateAssignment(ctx, svc, &embeddedUpdateAssignmentEnvelope{
		AssignmentID: created.ID,
		Update:       &iamsdk.UpdateAdminAssignmentRequest{Status: &active},
	}); err != nil {
		t.Fatalf("re-activating failed: %v", err)
	}
	stored, err = mgr.GetAssignment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if !stored.Active {
		t.Error("expected the stored assignment to be active again")
	}
}

func TestHandleIAMAdminUpdateAssignment_Errors(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without the payload fields", req: struct{ Other string }{}},
		{name: "missing update payload", req: &embeddedUpdateAssignmentEnvelope{AssignmentID: "a-1"}},
		{
			name: "unknown assignment",
			req:  &embeddedUpdateAssignmentEnvelope{AssignmentID: "missing", Update: &iamsdk.UpdateAdminAssignmentRequest{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMAdminUpdateAssignment(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHandleIAMAdminListAssignments(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()
	createSuperAdmin(t, svc, "admin-1", "t-1")
	createSuperAdmin(t, svc, "admin-2", "t-1")

	out, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{TenantID: "t-1"},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	got, ok := out.(*iamsdk.ListAdminAssignmentsResponse)
	if !ok {
		t.Fatalf("expected *iam.ListAdminAssignmentsResponse, got %T", out)
	}
	if len(got.Assignments) != 2 {
		t.Errorf("returned %d assignments, want 2", len(got.Assignments))
	}
	if got.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", got.TotalCount)
	}

	// Filtering by admin narrows the result rather than being ignored.
	out, err = handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{AdminID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	if page := out.(*iamsdk.ListAdminAssignmentsResponse); len(page.Assignments) != 1 {
		t.Errorf("returned %d assignments for admin-1, want 1", len(page.Assignments))
	}
}

func TestHandleIAMAdminListAssignments_BadRequest(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	if _, err := handleIAMAdminListAssignments(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a request with no Filter field")
	}
	if _, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{}); err == nil {
		t.Error("expected an error for a nil filter")
	}
}

func TestHandleIAMAdminCheckPermission(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()
	createSuperAdmin(t, svc, "admin-1", "t-1")

	out, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
		Check: &iamsdk.AdminCheckRequest{
			AdminID:    "admin-1",
			TenantID:   "t-1",
			Permission: string(iam.PermUserImpersonate),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCheckPermission failed: %v", err)
	}
	if got := out.(*iamsdk.AdminCheckResponse); !got.Allowed {
		t.Errorf("super admin denied %s: %s", iam.PermUserImpersonate, got.DenialReason)
	}

	// A principal with no assignment is denied, with a reason rather than a
	// bare false.
	out, err = handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
		Check: &iamsdk.AdminCheckRequest{
			AdminID:    "nobody",
			TenantID:   "t-1",
			Permission: string(iam.PermUserImpersonate),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCheckPermission failed: %v", err)
	}
	if got := out.(*iamsdk.AdminCheckResponse); got.Allowed {
		t.Error("an unassigned principal must not be allowed")
	}
}

// Action is the looser alias the SDK accepts when only the verb is known;
// Permission must win when both are set so a check can never be widened.
func TestHandleIAMAdminCheckPermission_ActionFallback(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()
	createSuperAdmin(t, svc, "admin-1", "t-1")

	out, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
		Check: &iamsdk.AdminCheckRequest{
			AdminID:  "admin-1",
			TenantID: "t-1",
			Action:   string(iam.PermUserImpersonate),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCheckPermission failed: %v", err)
	}
	if got := out.(*iamsdk.AdminCheckResponse); !got.Allowed {
		t.Errorf("the Action fallback was not used: %s", got.DenialReason)
	}
}

func TestHandleIAMAdminCheckPermission_BadRequest(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	if _, err := handleIAMAdminCheckPermission(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a request with no Check field")
	}
	if _, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{}); err == nil {
		t.Error("expected an error for a nil check payload")
	}
}

func TestHandleIAMAdminImpersonationLifecycle(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := context.Background()

	// The acting admin is resolved from the authenticated subject
	// (go-quicbaas#383); startImpersonationAs seeds admin-1's grant and binds
	// it as the caller before opening the session.
	session := startImpersonationAs(t, svc, "admin-1", "t-1", "user-9")
	if session.AdminID != "admin-1" || session.TargetUserID != "user-9" {
		t.Errorf("unexpected session principals: %+v", session)
	}
	if !session.Active {
		t.Error("a freshly started session must be active")
	}

	listed, err := handleIAMAdminListActiveImpersonations(ctx, svc, &embeddedListImpersonationsEnvelope{AdminUserID: "admin-1"})
	if err != nil {
		t.Fatalf("handleIAMAdminListActiveImpersonations failed: %v", err)
	}
	if got := listed.(*iamsdk.ListImpersonationsResponse); len(got.Sessions) != 1 || got.TotalCount != 1 {
		t.Errorf("expected exactly one active impersonation, got %+v", got)
	}

	if _, err := handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: session.ID}); err != nil {
		t.Fatalf("handleIAMAdminEndImpersonation failed: %v", err)
	}

	listed, err = handleIAMAdminListActiveImpersonations(ctx, svc, &embeddedListImpersonationsEnvelope{AdminUserID: "admin-1"})
	if err != nil {
		t.Fatalf("handleIAMAdminListActiveImpersonations failed: %v", err)
	}
	if got := listed.(*iamsdk.ListImpersonationsResponse); len(got.Sessions) != 0 {
		t.Errorf("expected no active impersonations after ending, got %d", len(got.Sessions))
	}
}

// Impersonation is gated on the admin actually holding the permission; the
// embedded transport must not become a way around that.
func TestHandleIAMAdminStartImpersonation_Errors(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without the payload field", req: struct{ Other string }{}},
		{name: "nil payload", req: &embeddedStartImpersonationEnvelope{}},
		{
			name: "admin without the impersonate permission",
			req: &embeddedStartImpersonationEnvelope{Impersonation: &iamsdk.ImpersonationRequest{
				TargetUserID: "user-9", TenantID: "t-1", Reason: "curiosity", Duration: time.Minute,
			}},
		},
		{
			name: "missing reason",
			req: &embeddedStartImpersonationEnvelope{Impersonation: &iamsdk.ImpersonationRequest{
				TargetUserID: "user-9", TenantID: "t-1", Duration: time.Minute,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMAdminStartImpersonation(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHandleIAMAdminImpersonation_BadRequests(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()

	if _, err := handleIAMAdminEndImpersonation(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for an end request with no SessionID field")
	}
	if _, err := handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: "missing"}); err == nil {
		t.Error("expected an error for an unknown impersonation session")
	}
	if _, err := handleIAMAdminListActiveImpersonations(ctx, svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a list request with no AdminUserID field")
	}
}

// An expired grant must report expired even while the stored flag still says
// active, so a caller never acts on a permission the manager would refuse.
func TestAdminAssignmentToSDK_StatusReflectsExpiry(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	tests := []struct {
		name       string
		assignment *iam.AdminAssignment
		want       iamsdk.AdminAssignmentStatus
	}{
		{
			name:       "active, no expiry",
			assignment: &iam.AdminAssignment{Active: true},
			want:       iamsdk.AdminAssignmentStatusActive,
		},
		{
			name:       "active, future expiry",
			assignment: &iam.AdminAssignment{Active: true, ExpiresAt: &future},
			want:       iamsdk.AdminAssignmentStatusActive,
		},
		{
			name:       "flagged active but already expired",
			assignment: &iam.AdminAssignment{Active: true, ExpiresAt: &past},
			want:       iamsdk.AdminAssignmentStatusExpired,
		},
		{
			name:       "inactive",
			assignment: &iam.AdminAssignment{Active: false},
			want:       iamsdk.AdminAssignmentStatusSuspended,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminAssignmentToSDK(tt.assignment); got.Status != tt.want {
				t.Errorf("Status = %v, want %v", got.Status, tt.want)
			}
		})
	}

	if adminAssignmentToSDK(nil) != nil {
		t.Error("a nil assignment must convert to nil")
	}
	if impersonationSessionToSDK(nil) != nil {
		t.Error("a nil impersonation session must convert to nil")
	}
}

// A host that wires no admin manager must get a typed
// ServiceNotConfiguredError rather than a nil-pointer panic.
func TestAdminHandlers_ServiceNotConfigured(t *testing.T) {
	var svc service.QuicBaaS
	ctx := context.Background()

	if _, err := handleIAMAdminGetAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: "a-1"}); err == nil {
		t.Fatal("expected an error when no admin manager is configured")
	} else if _, ok := err.(*ServiceNotConfiguredError); !ok {
		t.Errorf("expected *ServiceNotConfiguredError, got %T: %v", err, err)
	}

	nilMgr := &mockQuicBaaSWithAdmin{}
	if _, err := handleIAMAdminGetAssignment(ctx, nilMgr, &embeddedAssignmentIDEnvelope{AssignmentID: "a-1"}); err == nil {
		t.Fatal("expected an error when the admin manager is nil")
	} else if _, ok := err.(*ServiceNotConfiguredError); !ok {
		t.Errorf("expected *ServiceNotConfiguredError, got %T: %v", err, err)
	}
}

// TenantID and a tenant-scoped ScopeID naming different tenants is ambiguous.
// Granting both would hand the admin a tenant the caller never deliberately
// named, so the request is refused.
func TestHandleIAMAdminCreateAssignment_AmbiguousTenantScopeRejected(t *testing.T) {
	svc, _ := newAdminTestService(t)

	_, err := handleIAMAdminCreateAssignment(context.Background(), svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:   "admin-1",
			TenantID:  "t-1",
			Scope:     iamsdk.AdminScopeTenant,
			ScopeID:   "t-2",
			RoleID:    string(iam.AdminRoleTenantAdmin),
			GrantedBy: "owner",
		},
	})
	if err == nil {
		t.Fatal("expected an ambiguous tenant scope to be refused")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
	}
}

// A tenant-scoped ScopeID alone still scopes the grant.
func TestHandleIAMAdminCreateAssignment_ScopeIDAloneScopesTheGrant(t *testing.T) {
	svc, mgr := newAdminTestService(t)
	ctx := context.Background()

	out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:   "admin-1",
			Scope:     iamsdk.AdminScopeTenant,
			ScopeID:   "t-9",
			RoleID:    string(iam.AdminRoleTenantAdmin),
			GrantedBy: "owner",
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}
	stored, err := mgr.GetAssignment(ctx, out.(*iamsdk.AdminAssignment).ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if len(stored.TenantIDs) != 1 || stored.TenantIDs[0] != "t-9" {
		t.Errorf("TenantIDs = %v, want [t-9]", stored.TenantIDs)
	}
}

// ResourceScope carries no tags, so a tag-constrained filter cannot be stored
// as asked. Accepting it and dropping the tags would persist a weaker
// constraint than the caller specified.
func TestHandleIAMAdminCreateAssignment_TaggedResourceFilterRejected(t *testing.T) {
	svc, _ := newAdminTestService(t)

	_, err := handleIAMAdminCreateAssignment(context.Background(), svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID:   "admin-1",
			RoleID:    string(iam.AdminRoleReadOnly),
			GrantedBy: "owner",
			Constraints: &iamsdk.AdminConstraints{
				ResourceFilters: []iamsdk.ResourceFilter{
					{Type: "user", Pattern: "user:*", Tags: map[string]string{"env": "prod"}},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected a tag-constrained resource filter to be refused")
	}
	if _, ok := err.(*InvalidRequestTypeError); !ok {
		t.Errorf("expected *InvalidRequestTypeError, got %T: %v", err, err)
	}
}

// An unfiltered list must return every assignment, matching REST. An
// embedded-only "active assignments" default would hide suspended grants from
// an auditor who asked for everything.
func TestHandleIAMAdminListAssignments_UnfilteredIncludesInactive(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := context.Background()
	active := createSuperAdmin(t, svc, "admin-1", "t-1")
	suspendedAssignment := createSuperAdmin(t, svc, "admin-2", "t-1")

	suspended := iamsdk.AdminAssignmentStatusSuspended
	if _, err := handleIAMAdminUpdateAssignment(ctx, svc, &embeddedUpdateAssignmentEnvelope{
		AssignmentID: suspendedAssignment.ID,
		Update:       &iamsdk.UpdateAdminAssignmentRequest{Status: &suspended},
	}); err != nil {
		t.Fatalf("suspending the assignment failed: %v", err)
	}

	out, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	if page := out.(*iamsdk.ListAdminAssignmentsResponse); len(page.Assignments) != 2 {
		t.Errorf("an unfiltered list returned %d assignments, want both", len(page.Assignments))
	}

	// An explicit active filter is the one thing that narrows the result.
	activeStatus := iamsdk.AdminAssignmentStatusActive
	out, err = handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{Status: &activeStatus},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	page := out.(*iamsdk.ListAdminAssignmentsResponse)
	if len(page.Assignments) != 1 || page.Assignments[0].ID != active.ID {
		t.Errorf("an active filter returned %+v, want only the active assignment", page.Assignments)
	}
}

// A global assignment carries no tenants and must report itself as global.
func TestAdminAssignmentToSDK_ScopeFollowsTenants(t *testing.T) {
	global := adminAssignmentToSDK(&iam.AdminAssignment{ID: "a-1", Active: true})
	if global.Scope != iamsdk.AdminScopeGlobal || global.TenantID != "" || global.ScopeID != "" {
		t.Errorf("Scope = %v, TenantID = %q, ScopeID = %q; want global with no tenant",
			global.Scope, global.TenantID, global.ScopeID)
	}

	scoped := adminAssignmentToSDK(&iam.AdminAssignment{ID: "a-2", Active: true, TenantIDs: []string{"t-1"}})
	if scoped.Scope != iamsdk.AdminScopeTenant || scoped.TenantID != "t-1" || scoped.ScopeID != "t-1" {
		t.Errorf("Scope = %v, TenantID = %q, ScopeID = %q; want tenant-scoped on t-1",
			scoped.Scope, scoped.TenantID, scoped.ScopeID)
	}
}

// A context with no Subject is the host process calling its own service, the
// pre-existing contract for every embedded IAM handler. Delegated admin
// operations still run through *iam.AdminManager, so its own permission and
// audit rules apply — StartImpersonation below is refused for an admin with no
// assignment even on an unbound context.
func TestAdminHandlers_ManagerRulesApplyOnUnboundContext(t *testing.T) {
	svc, _ := newAdminTestService(t)

	// Unbound (in-process host) context: requireAdminPermission is skipped for
	// a trusted host, but StartImpersonation resolves its acting admin from the
	// subject (go-quicbaas#383) and there is none, so it still refuses — an
	// in-process call is not a free pass.
	_, err := handleIAMAdminStartImpersonation(context.Background(), svc, &embeddedStartImpersonationEnvelope{
		Impersonation: &iamsdk.ImpersonationRequest{
			TargetUserID: "user-9",
			TenantID:     "t-1",
			Reason:       "curiosity",
			Duration:     time.Minute,
		},
	})
	if err == nil {
		t.Fatal("an unbound in-process call with no authenticated actor must still be refused")
	}
}

// Delegated administration is route-permission-gated over REST. A
// subject-bound embedded call must clear the same gate, and must fail closed
// when the host wired no policy engine — otherwise the embedded transport
// becomes the cheapest way to mint a super-admin.
func TestAdminHandlers_SubjectBoundCallsRequireRoutePermission(t *testing.T) {
	svc, _ := newAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "caller", TenantID: "t-1"})

	tests := []struct {
		name string
		call func() (interface{}, error)
	}{
		{
			name: "CreateAssignment",
			call: func() (interface{}, error) {
				return handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
					CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
						AdminID: "admin-1", RoleID: string(iam.AdminRoleSuperAdmin), GrantedBy: "caller",
					},
				})
			},
		},
		{
			name: "GetAssignment",
			call: func() (interface{}, error) {
				return handleIAMAdminGetAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: "a-1"})
			},
		},
		{
			name: "UpdateAssignment",
			call: func() (interface{}, error) {
				return handleIAMAdminUpdateAssignment(ctx, svc, &embeddedUpdateAssignmentEnvelope{
					AssignmentID: "a-1", Update: &iamsdk.UpdateAdminAssignmentRequest{},
				})
			},
		},
		{
			name: "DeleteAssignment",
			call: func() (interface{}, error) {
				return handleIAMAdminDeleteAssignment(ctx, svc, &embeddedAssignmentIDEnvelope{AssignmentID: "a-1"})
			},
		},
		{
			name: "ListAssignments",
			call: func() (interface{}, error) {
				return handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
					Filter: &iamsdk.ListAdminAssignmentsRequest{},
				})
			},
		},
		{
			name: "CheckPermission",
			call: func() (interface{}, error) {
				return handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
					Check: &iamsdk.AdminCheckRequest{AdminID: "admin-1", Permission: string(iam.PermUserRead)},
				})
			},
		},
		{
			name: "StartImpersonation",
			call: func() (interface{}, error) {
				return handleIAMAdminStartImpersonation(ctx, svc, &embeddedStartImpersonationEnvelope{
					Impersonation: &iamsdk.ImpersonationRequest{
						TargetUserID: "user-9", TenantID: "t-1",
						Reason: "support", Duration: time.Minute,
					},
				})
			},
		},
		{
			name: "EndImpersonation",
			call: func() (interface{}, error) {
				return handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: "s-1"})
			},
		},
		{
			name: "ListActiveImpersonations",
			call: func() (interface{}, error) {
				return handleIAMAdminListActiveImpersonations(ctx, svc, &embeddedListImpersonationsEnvelope{AdminUserID: "caller"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.call()
			if err == nil {
				t.Fatal("expected a subject-bound call to be denied without a policy engine")
			}
			if _, ok := err.(*PermissionDeniedError); !ok {
				t.Errorf("expected *PermissionDeniedError (fail closed), got %T: %v", err, err)
			}
		})
	}
}

// allowAllAuthorizer is the RBAC decision point PolicyEngine delegates to. It
// allows everything so a test can get past the route-permission gate and reach
// the ownership checks behind it — the gate is a generic resource:action
// grant, so it deliberately does NOT distinguish one admin's data from
// another's.
type allowAllAuthorizer struct{}

func (allowAllAuthorizer) Authorize(_ context.Context, _ *iam.Subject, _, _ string) error {
	return nil
}

// mockQuicBaaSWithAuthorizedAdmin exposes an admin manager alongside a policy
// engine that grants every route-equivalent permission.
type mockQuicBaaSWithAuthorizedAdmin struct {
	service.QuicBaaS
	admin  *iam.AdminManager
	policy *iam.PolicyEngine
}

func (m *mockQuicBaaSWithAuthorizedAdmin) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.admin, nil
}

func (m *mockQuicBaaSWithAuthorizedAdmin) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	return m.policy, nil
}

func newAuthorizedAdminTestService(t *testing.T) *mockQuicBaaSWithAuthorizedAdmin {
	t.Helper()
	return &mockQuicBaaSWithAuthorizedAdmin{
		admin:  newAdminTestManager(t),
		policy: iam.NewPolicyEngine(iam.PolicyEngineOptions{RBACAuthorizer: allowAllAuthorizer{}}),
	}
}

// startImpersonationAs opens an impersonation session on an unbound (host)
// context so a test can then exercise the subject-bound access rules against
// an existing session.
func startImpersonationAs(t *testing.T, svc service.QuicBaaS, adminID, tenantID, targetID string) *iamsdk.ImpersonationSession {
	t.Helper()
	ctx := context.Background()
	if _, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: adminID, TenantID: tenantID,
			RoleID: string(iam.AdminRoleSuperAdmin), GrantedBy: "bootstrap",
		},
	}); err != nil {
		t.Fatalf("seeding the admin assignment failed: %v", err)
	}
	// The acting admin is resolved from the authenticated subject, never from
	// the request (go-quicbaas#383), so bind adminID as the caller.
	actorCtx := auth.SubjectIntoContext(ctx, interfaces.Subject{ID: adminID, TenantID: tenantID, Authenticated: true})
	out, err := handleIAMAdminStartImpersonation(actorCtx, svc, &embeddedStartImpersonationEnvelope{
		Impersonation: &iamsdk.ImpersonationRequest{
			TargetUserID: targetID, TenantID: tenantID,
			Reason: "support", Duration: time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("starting the impersonation session failed: %v", err)
	}
	return out.(*iamsdk.ImpersonationSession)
}

// users:update is a generic grant: it says the caller may update users, not
// that this particular impersonation session is theirs to end.
func TestHandleIAMAdminEndImpersonation_DeniesAnotherAdminsSession(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	victim := startImpersonationAs(t, svc, "admin-victim", "t-1", "user-9")

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-other", TenantID: "t-1"})
	_, err := handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: victim.ID})
	if err == nil {
		t.Fatal("expected ending another admin's impersonation session to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// The session survived the denial.
	still, listErr := svc.admin.GetActiveImpersonations(context.Background(), "admin-victim")
	if listErr != nil {
		t.Fatalf("GetActiveImpersonations failed: %v", listErr)
	}
	if len(still) != 1 {
		t.Errorf("the victim's session was ended despite the denial: %d remain", len(still))
	}
}

// A tenant-scoped admin must not reach across tenants even when the admin id
// matches, which is why both halves of the ownership check exist.
func TestHandleIAMAdminEndImpersonation_DeniesAcrossTenants(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	session := startImpersonationAs(t, svc, "admin-1", "t-1", "user-9")

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-2"})
	if _, err := handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: session.ID}); err == nil {
		t.Fatal("expected a cross-tenant end to be denied")
	}
}

// The owning admin can still end its own session.
func TestHandleIAMAdminEndImpersonation_OwnerSucceeds(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	session := startImpersonationAs(t, svc, "admin-1", "t-1", "user-9")

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1"})
	if _, err := handleIAMAdminEndImpersonation(ctx, svc, &embeddedImpersonationSessionEnvelope{SessionID: session.ID}); err != nil {
		t.Fatalf("the owning admin must be able to end its own session: %v", err)
	}

	still, err := svc.admin.GetActiveImpersonations(context.Background(), "admin-1")
	if err != nil {
		t.Fatalf("GetActiveImpersonations failed: %v", err)
	}
	if len(still) != 0 {
		t.Errorf("expected the session to be ended, %d remain", len(still))
	}
}

// The storage API is keyed by admin id alone, so a subject-bound caller is
// pinned to its own id and its results narrowed to its own tenant.
func TestHandleIAMAdminListActiveImpersonations_BoundToSubject(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	startImpersonationAs(t, svc, "admin-1", "t-1", "user-9")

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-other", TenantID: "t-1"})
	_, err := handleIAMAdminListActiveImpersonations(ctx, svc, &embeddedListImpersonationsEnvelope{AdminUserID: "admin-1"})
	if err == nil {
		t.Fatal("expected enumerating another admin's impersonations to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// An omitted admin id is filled from the subject, and a caller in another
	// tenant sees nothing even under its own id.
	ownCtx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1"})
	out, err := handleIAMAdminListActiveImpersonations(ownCtx, svc, &embeddedListImpersonationsEnvelope{})
	if err != nil {
		t.Fatalf("handleIAMAdminListActiveImpersonations failed: %v", err)
	}
	if page := out.(*iamsdk.ListImpersonationsResponse); len(page.Sessions) != 1 {
		t.Errorf("the owning admin should see its own session, got %d", len(page.Sessions))
	}

	otherTenant := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-2"})
	out, err = handleIAMAdminListActiveImpersonations(otherTenant, svc, &embeddedListImpersonationsEnvelope{})
	if err != nil {
		t.Fatalf("handleIAMAdminListActiveImpersonations failed: %v", err)
	}
	if page := out.(*iamsdk.ListImpersonationsResponse); len(page.Sessions) != 0 {
		t.Errorf("a caller in another tenant must see nothing, got %d", len(page.Sessions))
	}
}

// GrantedBy is the audit actor on a privileged grant, so a subject-bound
// caller must not be able to forge it.
func TestHandleIAMAdminCreateAssignment_GrantedByBoundToSubject(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "granter", TenantID: "t-1"})

	_, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "admin-1", TenantID: "t-1",
			RoleID: string(iam.AdminRoleSuperAdmin), GrantedBy: "someone-else",
		},
	})
	if err == nil {
		t.Fatal("expected a forged granted_by to be refused")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// Omitted, it is filled from the subject rather than left blank.
	out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "admin-1", TenantID: "t-1", RoleID: string(iam.AdminRoleSuperAdmin),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}
	stored, err := svc.admin.GetAssignment(context.Background(), out.(*iamsdk.AdminAssignment).ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if stored.GrantedBy != "granter" {
		t.Errorf("GrantedBy = %q, want the authenticated subject \"granter\"", stored.GrantedBy)
	}
}

// The manager checks the named admin's permission IN THE NAMED TENANT, so a
// subject-bound caller must not be able to name a tenant it was never scoped
// to even under its own admin id.
func TestHandleIAMAdminStartImpersonation_BindsTenantToSubject(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1"})

	_, err := handleIAMAdminStartImpersonation(ctx, svc, &embeddedStartImpersonationEnvelope{
		Impersonation: &iamsdk.ImpersonationRequest{
			TargetUserID: "user-9", TenantID: "t-2",
			Reason: "support", Duration: time.Minute,
		},
	})
	if err == nil {
		t.Fatal("expected impersonating into another tenant to be refused")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}
}

// An omitted admin/tenant is filled from the subject rather than left blank,
// so the manager's permission check runs against the caller's real scope.
func TestHandleIAMAdminStartImpersonation_FillsIdentityFromSubject(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	if _, err := handleIAMAdminCreateAssignment(context.Background(), svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "admin-1", TenantID: "t-1",
			RoleID: string(iam.AdminRoleSuperAdmin), GrantedBy: "bootstrap",
		},
	}); err != nil {
		t.Fatalf("seeding the admin assignment failed: %v", err)
	}

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1", Authenticated: true})
	out, err := handleIAMAdminStartImpersonation(ctx, svc, &embeddedStartImpersonationEnvelope{
		Impersonation: &iamsdk.ImpersonationRequest{
			TargetUserID: "user-9", Reason: "support", Duration: time.Minute,
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminStartImpersonation failed: %v", err)
	}
	session := out.(*iamsdk.ImpersonationSession)
	if session.AdminID != "admin-1" || session.TenantID != "t-1" {
		t.Errorf("session identity = %q/%q, want admin-1/t-1 from the subject", session.AdminID, session.TenantID)
	}
}

// roles:list says the caller may list assignments, not that it may list
// another tenant's.
func TestHandleIAMAdminListAssignments_BoundToSubjectTenant(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1"})

	_, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{TenantID: "t-2"},
	})
	if err == nil {
		t.Fatal("expected listing another tenant's assignments to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// An omitted filter is FILLED with the subject's tenant, not left open:
	// seed an assignment in each tenant and prove only the subject's comes back.
	seedAssignmentIn(t, svc, "admin-here", "t-1")
	seedAssignmentIn(t, svc, "admin-there", "t-2")

	out, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{},
	})
	if err != nil {
		t.Fatalf("an unfiltered subject-bound list must succeed in the subject's own tenant: %v", err)
	}
	page := out.(*iamsdk.ListAdminAssignmentsResponse)
	if len(page.Assignments) != 1 || page.Assignments[0].AdminID != "admin-here" {
		t.Errorf("an omitted tenant filter must be filled with the subject's tenant, got %+v", page.Assignments)
	}
}

// seedAssignmentIn creates a tenant-scoped assignment on an unbound (host)
// context, so a test can populate several tenants before exercising a
// subject-bound read.
func seedAssignmentIn(t *testing.T, svc service.QuicBaaS, adminID, tenantID string) {
	t.Helper()
	if _, err := handleIAMAdminCreateAssignment(context.Background(), svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: adminID, TenantID: tenantID,
			RoleID: string(iam.AdminRoleTenantAdmin), GrantedBy: "bootstrap",
		},
	}); err != nil {
		t.Fatalf("seeding an assignment in %s failed: %v", tenantID, err)
	}
}

// The platform scope keeps the cross-tenant view, including the unfiltered
// one — that is the whole point of the system tenant.
func TestHandleIAMAdminListAssignments_SystemScopeSeesEveryTenant(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	seedAssignmentIn(t, svc, "admin-here", "t-1")
	seedAssignmentIn(t, svc, "admin-there", "t-2")

	ctx := auth.SubjectIntoContext(context.Background(),
		interfaces.Subject{ID: "platform-admin", TenantID: qrdbsdk.SystemTenantID, Authenticated: true})

	out, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	if page := out.(*iamsdk.ListAdminAssignmentsResponse); len(page.Assignments) != 2 {
		t.Errorf("an unfiltered system-scope list returned %d assignments, want both tenants", len(page.Assignments))
	}

	// A tenant filter still narrows it.
	out, err = handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
		Filter: &iamsdk.ListAdminAssignmentsRequest{TenantID: "t-2"},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminListAssignments failed: %v", err)
	}
	page := out.(*iamsdk.ListAdminAssignmentsResponse)
	if len(page.Assignments) != 1 || page.Assignments[0].AdminID != "admin-there" {
		t.Errorf("an explicit system-scope tenant filter returned %+v, want only t-2's", page.Assignments)
	}
}

// The system tenant is the platform scope, not a tenant named "system".
// Pinning it to its own "tenant" would make cross-tenant platform
// administration impossible — the capability the system tenant exists for.
func TestAdminHandlers_SystemScopeIsNotTenantPinned(t *testing.T) {
	for _, systemTenant := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenant="+systemTenant, func(t *testing.T) {
			svc := newAuthorizedAdminTestService(t)
			ctx := auth.SubjectIntoContext(context.Background(),
				interfaces.Subject{ID: "platform-admin", TenantID: systemTenant})

			// A cross-tenant grant is the platform's job.
			out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
				CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
					AdminID: "admin-1", TenantID: "t-1", RoleID: string(iam.AdminRoleTenantAdmin),
				},
			})
			if err != nil {
				t.Fatalf("a system-scope subject must be able to grant into another tenant: %v", err)
			}
			if got := out.(*iamsdk.AdminAssignment); got.TenantID != "t-1" {
				t.Errorf("TenantID = %q, want the requested t-1", got.TenantID)
			}

			// So is listing and inspecting another tenant.
			if _, err := handleIAMAdminListAssignments(ctx, svc, &embeddedListAssignmentsEnvelope{
				Filter: &iamsdk.ListAdminAssignmentsRequest{TenantID: "t-1"},
			}); err != nil {
				t.Errorf("a system-scope subject must be able to list another tenant: %v", err)
			}
			if _, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
				Check: &iamsdk.AdminCheckRequest{
					AdminID: "admin-1", TenantID: "t-1", Permission: string(iam.PermUserRead),
				},
			}); err != nil {
				t.Errorf("a system-scope subject must be able to inspect another tenant: %v", err)
			}
		})
	}
}

// A system-scope admin impersonating into a tenant is the legitimate
// cross-tenant case; only the admin identity stays pinned to the caller.
func TestHandleIAMAdminStartImpersonation_SystemScopeMayCrossTenants(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(),
		interfaces.Subject{ID: "platform-admin", TenantID: qrdbsdk.SystemTenantID, Authenticated: true})

	if _, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "platform-admin", TenantID: "t-1", RoleID: string(iam.AdminRoleSuperAdmin),
		},
	}); err != nil {
		t.Fatalf("seeding the platform admin's grant failed: %v", err)
	}

	out, err := handleIAMAdminStartImpersonation(ctx, svc, &embeddedStartImpersonationEnvelope{
		Impersonation: &iamsdk.ImpersonationRequest{
			TargetUserID: "user-9", TenantID: "t-1", Reason: "support", Duration: time.Minute,
		},
	})
	if err != nil {
		t.Fatalf("a system-scope admin must be able to impersonate into a tenant: %v", err)
	}
	session := out.(*iamsdk.ImpersonationSession)
	if session.TenantID != "t-1" || session.AdminID != "platform-admin" {
		t.Errorf("session = %q/%q, want platform-admin in t-1", session.AdminID, session.TenantID)
	}
}

// A tenant-scoped subject cannot mint a GLOBAL grant, which would be the
// broadest escalation this surface offers.
func TestHandleIAMAdminCreateAssignment_TenantScopedCannotGrantGlobally(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "granter", TenantID: "t-1"})

	// An omitted scope is filled from the subject rather than left global.
	out, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "admin-1", RoleID: string(iam.AdminRoleTenantAdmin),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMAdminCreateAssignment failed: %v", err)
	}
	stored, err := svc.admin.GetAssignment(context.Background(), out.(*iamsdk.AdminAssignment).ID)
	if err != nil {
		t.Fatalf("GetAssignment failed: %v", err)
	}
	if len(stored.TenantIDs) != 1 || stored.TenantIDs[0] != "t-1" {
		t.Errorf("TenantIDs = %v, want the subject's own [t-1] rather than a global grant", stored.TenantIDs)
	}

	// Naming a different tenant is refused outright.
	if _, err := handleIAMAdminCreateAssignment(ctx, svc, &embeddedCreateAssignmentEnvelope{
		CreateAdminAssignmentRequest: &iamsdk.CreateAdminAssignmentRequest{
			AdminID: "admin-1", TenantID: "t-2", RoleID: string(iam.AdminRoleTenantAdmin),
		},
	}); err == nil {
		t.Fatal("expected granting into another tenant to be refused")
	}
}

// The tenant a permission answer is computed in is the caller's own.
func TestHandleIAMAdminCheckPermission_TenantPinnedForScopedSubject(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "admin-1", TenantID: "t-1"})

	_, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
		Check: &iamsdk.AdminCheckRequest{
			AdminID: "admin-2", TenantID: "t-2", Permission: string(iam.PermUserRead),
		},
	})
	if err == nil {
		t.Fatal("expected inspecting another tenant to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// AdminID stays free: naming the admin being inspected is the point.
	if _, err := handleIAMAdminCheckPermission(ctx, svc, &embeddedAdminCheckEnvelope{
		Check: &iamsdk.AdminCheckRequest{
			AdminID: "admin-2", Permission: string(iam.PermUserRead),
		},
	}); err != nil {
		t.Fatalf("inspecting another admin in the caller's own tenant must be allowed: %v", err)
	}
}
