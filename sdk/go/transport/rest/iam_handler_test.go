// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// iamRecorder is a test server that records the last method/path/body it saw
// and returns a configurable JSON body.
type iamRecorder struct {
	server     *httptest.Server
	gotMethod  string
	gotPath    string
	gotRawPath string
	respBody   string
}

func newIAMRecorder(t *testing.T) *iamRecorder {
	t.Helper()
	rec := &iamRecorder{respBody: `{"ok":true}`}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.gotMethod = r.Method
		rec.gotPath = r.URL.Path
		rec.gotRawPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rec.respBody))
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

// TestIAMHandlerHappyPaths drives the primary verb of each IAM sub-service and
// asserts the HTTP method and URL path the handler constructs.
func TestIAMHandlerHappyPaths(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	cases := []struct {
		name       string
		service    string
		method     string
		req        map[string]interface{}
		wantMethod string
		wantPath   string
	}{
		// User
		{"UserCreate", iamUserServicePrefix, "Create",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{
				"email": "a@b.com", "username": "alice.smith@b.com", "display_name": "Alice"}},
			http.MethodPost, "/api/v1/tenants/t1/users"},
		{"UserGet", iamUserServicePrefix, "Get",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1"},
		{"UserGetByEmail", iamUserServicePrefix, "GetByEmail",
			map[string]interface{}{"tenant_id": "t1", "email": "a@b.com"},
			http.MethodGet, "/api/v1/tenants/t1/users"},
		{"UserFindByUsername", iamUserServicePrefix, "FindByUsername",
			map[string]interface{}{"tenant_id": "t1", "username": "alice"},
			http.MethodGet, "/api/v1/tenants/t1/users"},
		{"UserUpdate", iamUserServicePrefix, "Update",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "request": map[string]interface{}{"display_name": "A2"}},
			http.MethodPut, "/api/v1/tenants/t1/users/u1"},
		{"UserDelete", iamUserServicePrefix, "Delete",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1"},
		{"UserList", iamUserServicePrefix, "List",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodGet, "/api/v1/tenants/t1/users"},
		{"UserAssignRole", iamUserServicePrefix, "AssignRole",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "role_id": "r1"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/roles"},
		{"UserRevokeRole", iamUserServicePrefix, "RevokeRole",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "role_id": "r1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/roles/r1"},

		// Tenant
		{"TenantCreate", iamTenantServicePrefix, "Create",
			map[string]interface{}{"request": map[string]interface{}{"name": "Acme", "display_name": "Acme Inc"}},
			http.MethodPost, "/api/v1/tenants"},
		{"TenantGet", iamTenantServicePrefix, "Get",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodGet, "/api/v1/tenants/t1"},
		{"TenantUpdate", iamTenantServicePrefix, "Update",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"display_name": "New"}},
			http.MethodPut, "/api/v1/tenants/t1"},
		{"TenantDelete", iamTenantServicePrefix, "Delete",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodDelete, "/api/v1/tenants/t1"},
		{"TenantList", iamTenantServicePrefix, "List",
			map[string]interface{}{},
			http.MethodGet, "/api/v1/tenants"},

		// Role
		{"RoleCreate", iamRoleServicePrefix, "Create",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "admin"}},
			http.MethodPost, "/api/v1/tenants/t1/roles"},
		{"RoleGet", iamRoleServicePrefix, "Get",
			map[string]interface{}{"tenant_id": "t1", "role_id": "r1"},
			http.MethodGet, "/api/v1/tenants/t1/roles/r1"},
		{"RoleUpdate", iamRoleServicePrefix, "Update",
			map[string]interface{}{"tenant_id": "t1", "role_id": "r1", "request": map[string]interface{}{"name": "x"}},
			http.MethodPut, "/api/v1/tenants/t1/roles/r1"},
		{"RoleDelete", iamRoleServicePrefix, "Delete",
			map[string]interface{}{"tenant_id": "t1", "role_id": "r1"},
			http.MethodDelete, "/api/v1/tenants/t1/roles/r1"},
		{"RoleList", iamRoleServicePrefix, "List",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodGet, "/api/v1/tenants/t1/roles"},
		{"RoleReconcile", iamRoleServicePrefix, "Reconcile",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodPost, "/api/v1/tenants/t1/roles/reconcile"},

		// Policy
		{"PolicyGet", iamPolicyServicePrefix, "Get",
			map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"},
			http.MethodGet, "/api/v1/tenants/t1/policies/p1"},
		{"PolicyUpdate", iamPolicyServicePrefix, "Update",
			map[string]interface{}{"tenant_id": "t1", "policy_name": "p1", "request": map[string]interface{}{"effect": "allow"}},
			http.MethodPut, "/api/v1/tenants/t1/policies/p1"},
		{"PolicyDelete", iamPolicyServicePrefix, "Delete",
			map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"},
			http.MethodDelete, "/api/v1/tenants/t1/policies/p1"},
		{"PolicyValidate", iamPolicyServicePrefix, "Validate",
			map[string]interface{}{"tenant_id": "t1", "policy": map[string]interface{}{"name": "p"}},
			http.MethodPost, "/api/v1/tenants/t1/policies/validate"},
		{"PolicyTest", iamPolicyServicePrefix, "Test",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"subject": "s"}},
			http.MethodPost, "/api/v1/tenants/t1/policies/test"},

		// App
		{"AppRegister", iamAppServicePrefix, "Register",
			map[string]interface{}{"tenant_id": "t1", "app": map[string]interface{}{"name": "myapp"}},
			http.MethodPost, "/api/v1/tenants/t1/apps"},
		{"AppGet", iamAppServicePrefix, "Get",
			map[string]interface{}{"tenant_id": "t1", "client_id": "c1"},
			http.MethodGet, "/api/v1/tenants/t1/apps/c1"},
		{"AppUpdate", iamAppServicePrefix, "Update",
			map[string]interface{}{"tenant_id": "t1", "client_id": "c1", "app": map[string]interface{}{"name": "x"}},
			http.MethodPut, "/api/v1/tenants/t1/apps/c1"},
		{"AppDelete", iamAppServicePrefix, "Delete",
			map[string]interface{}{"tenant_id": "t1", "client_id": "c1"},
			http.MethodDelete, "/api/v1/tenants/t1/apps/c1"},
		{"AppList", iamAppServicePrefix, "List",
			map[string]interface{}{"tenant_id": "t1"},
			http.MethodGet, "/api/v1/tenants/t1/apps"},
		{"AppRotateSecret", iamAppServicePrefix, "RotateSecret",
			map[string]interface{}{"tenant_id": "t1", "client_id": "c1"},
			http.MethodPost, "/api/v1/tenants/t1/apps/c1/rotate-secret"},

		// Session. List and RevokeAll are parameterized admin verbs and map to
		// the path-scoped /api/v1/tenants/{t}/users/{u}/sessions surface (gRPC
		// parity); the remaining verbs stay on the self-scoped
		// /api/v1/iam/sessions surface.
		{"SessionList", iamSessionServicePrefix, "List",
			map[string]interface{}{"request": map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "limit": float64(5)}},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/sessions"},
		{"SessionGet", iamSessionServicePrefix, "Get",
			map[string]interface{}{"session_id": "s1"},
			http.MethodGet, "/api/v1/iam/sessions/s1"},
		{"SessionRevoke", iamSessionServicePrefix, "Revoke",
			map[string]interface{}{"session_id": "s1"},
			http.MethodDelete, "/api/v1/iam/sessions/s1"},
		{"SessionRevokeAll", iamSessionServicePrefix, "RevokeAll",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/sessions"},
		{"SessionRevokeOther", iamSessionServicePrefix, "RevokeOther",
			map[string]interface{}{},
			http.MethodPost, "/api/v1/iam/sessions/revoke-others"},

		// Audit
		{"AuditQuery", iamAuditServicePrefix, "Query",
			map[string]interface{}{"filter": map[string]interface{}{"tenant_id": "t1", "actor_id": "a1", "outcome": "success"}},
			http.MethodGet, "/api/v1/iam/audit"},
		{"AuditGet", iamAuditServicePrefix, "Get",
			map[string]interface{}{"event_id": "e1", "tenant_id": "t1"},
			http.MethodGet, "/api/v1/iam/audit/e1"},

		// PEP
		{"PEPCheck", iamPEPServicePrefix, "Check",
			map[string]interface{}{"request": map[string]interface{}{"subject": "u1", "action": "read"}},
			http.MethodPost, "/api/v1/iam/check"},
		{"PEPCheckBatch", iamPEPServicePrefix, "CheckBatch",
			map[string]interface{}{"request": map[string]interface{}{"requests": []interface{}{map[string]interface{}{"subject": "u1"}}}},
			http.MethodPost, "/api/v1/iam/check/batch"},

		// MFA
		{"MFABeginEnrollment", iamMFAServicePrefix, "BeginEnrollment",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "method": "totp"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/mfa/enroll"},
		{"MFACompleteEnrollment", iamMFAServicePrefix, "CompleteEnrollment",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "enrollment_id": "e1", "code": "123"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/mfa/verify"},
		{"MFACancelEnrollment", iamMFAServicePrefix, "CancelEnrollment",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "enrollment_id": "e1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/mfa/enroll/e1"},
		{"MFAGetStatus", iamMFAServicePrefix, "GetStatus",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/mfa"},
		{"MFADisable", iamMFAServicePrefix, "Disable",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/mfa"},
		{"MFABeginChallenge", iamMFAServicePrefix, "BeginChallenge",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "session_token": "tok"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/mfa/challenge"},
		{"MFAVerifyChallenge", iamMFAServicePrefix, "VerifyChallenge",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "challenge_id": "c1", "code": "1"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/mfa/verify-challenge"},
		{"MFAGenerateBackupCodes", iamMFAServicePrefix, "GenerateBackupCodes",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/mfa/backup-codes"},
		{"MFAGetBackupCodeStatus", iamMFAServicePrefix, "GetBackupCodeStatus",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/mfa/backup-codes"},

		// Admin
		{"AdminCreateAssignment", iamAdminServicePrefix, "CreateAssignment",
			map[string]interface{}{"user_id": "u1", "tenant_id": "t1", "role": "admin"},
			http.MethodPost, "/api/v1/admin/assignments"},
		{"AdminGetAssignment", iamAdminServicePrefix, "GetAssignment",
			map[string]interface{}{"assignment_id": "a1"},
			http.MethodGet, "/api/v1/admin/assignments/a1"},
		{"AdminUpdateAssignment", iamAdminServicePrefix, "UpdateAssignment",
			map[string]interface{}{"assignment_id": "a1", "update": map[string]interface{}{"status": float64(0)}},
			http.MethodPut, "/api/v1/admin/assignments/a1"},
		{"AdminDeleteAssignment", iamAdminServicePrefix, "DeleteAssignment",
			map[string]interface{}{"assignment_id": "a1"},
			http.MethodDelete, "/api/v1/admin/assignments/a1"},
		{"AdminListAssignments", iamAdminServicePrefix, "ListAssignments",
			map[string]interface{}{"filter": map[string]interface{}{"active_only": true, "user_id": "u1", "tenant_id": "t1"}},
			http.MethodGet, "/api/v1/admin/assignments"},
		{"AdminCheckPermission", iamAdminServicePrefix, "CheckPermission",
			map[string]interface{}{"check": map[string]interface{}{"permission": "p"}},
			http.MethodPost, "/api/v1/admin/check-permission"},
		{"AdminStartImpersonation", iamAdminServicePrefix, "StartImpersonation",
			map[string]interface{}{"impersonation": map[string]interface{}{"target_user_id": "u2"}},
			http.MethodPost, "/api/v1/admin/impersonation/start"},
		{"AdminEndImpersonation", iamAdminServicePrefix, "EndImpersonation",
			map[string]interface{}{"session_id": "s1"},
			http.MethodPost, "/api/v1/admin/impersonation/s1/end"},
		{"AdminGetActiveImpersonations", iamAdminServicePrefix, "GetActiveImpersonations",
			map[string]interface{}{"admin_user_id": "u1"},
			http.MethodGet, "/api/v1/admin/impersonation"},

		// GDPR
		{"GDPRGrantConsent", iamGDPRServicePrefix, "GrantConsent",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "consent": map[string]interface{}{"purpose": "marketing"}},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/gdpr/consent"},
		{"GDPRWithdrawConsent", iamGDPRServicePrefix, "WithdrawConsent",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing", "ip_address": "1.2.3.4", "user_agent": "ua"},
			http.MethodPut, "/api/v1/tenants/t1/users/u1/gdpr/consent/marketing/withdraw"},
		{"GDPRGetConsent", iamGDPRServicePrefix, "GetConsent",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/gdpr/consent/marketing"},
		{"GDPRRequestExport", iamGDPRServicePrefix, "RequestExport",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "export": map[string]interface{}{"format": float64(1)}},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/gdpr/export"},
		{"GDPRGetExportStatus", iamGDPRServicePrefix, "GetExportStatus",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "export_id": "x1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/gdpr/export/x1"},
		{"GDPRGenerateExport", iamGDPRServicePrefix, "GenerateExport",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "export_id": "x1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/gdpr/export/x1/download"},
		{"GDPRRequestDeletion", iamGDPRServicePrefix, "RequestDeletion",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "deletion": map[string]interface{}{"reason": "r"}},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/gdpr/deletion"},
		{"GDPRCancelDeletion", iamGDPRServicePrefix, "CancelDeletion",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "deletion_id": "d1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/gdpr/deletion/d1"},
		{"GDPRGetDeletionStatus", iamGDPRServicePrefix, "GetDeletionStatus",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "deletion_id": "d1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/gdpr/deletion/d1"},

		// Federation
		{"FederationCreateProvider", iamFederationServicePrefix, "CreateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider": map[string]interface{}{"name": "okta"}},
			http.MethodPost, "/api/v1/tenants/t1/providers"},
		{"FederationGetProvider", iamFederationServicePrefix, "GetProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1"},
			http.MethodGet, "/api/v1/tenants/t1/providers/p1"},
		{"FederationUpdateProvider", iamFederationServicePrefix, "UpdateProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1", "update": map[string]interface{}{"name": "x"}},
			http.MethodPut, "/api/v1/tenants/t1/providers/p1"},
		{"FederationDeleteProvider", iamFederationServicePrefix, "DeleteProvider",
			map[string]interface{}{"tenant_id": "t1", "provider_id": "p1"},
			http.MethodDelete, "/api/v1/tenants/t1/providers/p1"},

		// Identity
		{"IdentityLinkIdentity", iamIdentityServicePrefix, "LinkIdentity",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "provider_id": "p1", "external_id": "ext", "access_token": "tok"},
			http.MethodPost, "/api/v1/tenants/t1/users/u1/links"},
		{"IdentityUnlinkIdentity", iamIdentityServicePrefix, "UnlinkIdentity",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "provider_id": "p1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/links/p1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec.gotMethod, rec.gotPath = "", ""
			_, err := tr.Request(ctx, tc.service, tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, rec.gotMethod)
			}
			if rec.gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, rec.gotPath)
			}
		})
	}
}

// TestIAMHandlerListMappers exercises the Policy/Federation/Identity List verbs
// whose responses pass through dedicated SDK mapping functions.
func TestIAMHandlerListMappers(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	t.Run("PolicyCreateMapsResponse", func(t *testing.T) {
		rec.respBody = `{"name":"p1","effect":"allow","rules":[{"resources":["*"],"actions":["read"]}]}`
		_, err := tr.Request(ctx, iamPolicyServicePrefix, "Create",
			map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "p1"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.gotPath != "/api/v1/tenants/t1/policies" || rec.gotMethod != http.MethodPost {
			t.Errorf("unexpected request: %s %s", rec.gotMethod, rec.gotPath)
		}
	})

	t.Run("PolicyListMapsResponse", func(t *testing.T) {
		rec.respBody = `{"policies":[{"name":"p1","effect":"allow"}],"total_count":1}`
		_, err := tr.Request(ctx, iamPolicyServicePrefix, "List",
			map[string]interface{}{"tenant_id": "t1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.gotPath != "/api/v1/tenants/t1/policies" {
			t.Errorf("unexpected path: %s", rec.gotPath)
		}
	})

	t.Run("FederationListProviders", func(t *testing.T) {
		rec.respBody = `{"providers":[{"id":"p1","name":"okta"}]}`
		_, err := tr.Request(ctx, iamFederationServicePrefix, "ListProviders",
			map[string]interface{}{"tenant_id": "t1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.gotPath != "/api/v1/tenants/t1/providers" {
			t.Errorf("unexpected path: %s", rec.gotPath)
		}
	})

	t.Run("IdentityListUserIdentities", func(t *testing.T) {
		rec.respBody = `{"identities":[{"provider_id":"p1","external_id":"ext"}]}`
		_, err := tr.Request(ctx, iamIdentityServicePrefix, "ListUserIdentities",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.gotPath != "/api/v1/tenants/t1/users/u1/links" {
			t.Errorf("unexpected path: %s", rec.gotPath)
		}
	})

	t.Run("GDPRListConsentsArray", func(t *testing.T) {
		rec.respBody = `[{"purpose":"marketing"},{"purpose":"analytics"}]`
		resp, err := tr.Request(ctx, iamGDPRServicePrefix, "ListConsents",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m, ok := resp.(map[string]interface{})
		if !ok {
			t.Fatalf("expected wrapped map, got %T", resp)
		}
		if m["total_count"] != 2 {
			t.Errorf("expected total_count=2, got %v", m["total_count"])
		}
	})
}

// TestIAMHandlerMissingTenantID verifies the tenant_id-required guard on the
// sub-handlers that demand it.
func TestIAMHandlerMissingTenantID(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	services := []string{
		iamUserServicePrefix, iamRoleServicePrefix,
		iamPolicyServicePrefix, iamAppServicePrefix, iamMFAServicePrefix,
		iamGDPRServicePrefix, iamFederationServicePrefix, iamIdentityServicePrefix,
	}
	for _, svc := range services {
		t.Run(svc, func(t *testing.T) {
			_, err := tr.Request(ctx, svc, "List", map[string]interface{}{})
			if err == nil {
				t.Fatalf("expected tenant_id-required error for %s", svc)
			}
		})
	}
}

// TestIAMHandlerMissingResourceID verifies the per-resource ID guards.
func TestIAMHandlerMissingResourceID(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		service string
		method  string
		req     map[string]interface{}
	}{
		{"UserGetNoID", iamUserServicePrefix, "Get", map[string]interface{}{"tenant_id": "t1"}},
		{"RoleGetNoID", iamRoleServicePrefix, "Get", map[string]interface{}{"tenant_id": "t1"}},
		{"PolicyGetNoName", iamPolicyServicePrefix, "Get", map[string]interface{}{"tenant_id": "t1"}},
		{"AppGetNoClientID", iamAppServicePrefix, "Get", map[string]interface{}{"tenant_id": "t1"}},
		{"SessionGetNoID", iamSessionServicePrefix, "Get", map[string]interface{}{}},
		{"AuditGetNoEventID", iamAuditServicePrefix, "Get", map[string]interface{}{}},
		{"AdminGetNoAssignmentID", iamAdminServicePrefix, "GetAssignment", map[string]interface{}{}},
		{"MFAMissingUserID", iamMFAServicePrefix, "GetStatus", map[string]interface{}{"tenant_id": "t1"}},
		{"GDPRMissingUserID", iamGDPRServicePrefix, "GetConsent", map[string]interface{}{"tenant_id": "t1"}},
		{"GDPRMissingPurpose", iamGDPRServicePrefix, "GetConsent", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
		{"FederationGetNoProviderID", iamFederationServicePrefix, "GetProvider", map[string]interface{}{"tenant_id": "t1"}},
		{"IdentityMissingUserID", iamIdentityServicePrefix, "UnlinkIdentity", map[string]interface{}{"tenant_id": "t1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, tc.service, tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestIAMHandlerUnsupported verifies the unsupported-service and
// unsupported-method error branches.
func TestIAMHandlerUnsupported(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	t.Run("unsupported service", func(t *testing.T) {
		_, err := tr.Request(ctx, "quicbaas.IAM.Nope", "List", map[string]interface{}{})
		if err == nil {
			t.Fatal("expected unsupported-service error")
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		_, err := tr.Request(ctx, iamUserServicePrefix, "Telepathy", map[string]interface{}{"tenant_id": "t1"})
		if err == nil {
			t.Fatal("expected unsupported-method error")
		}
	})
}

// TestIAMHandlerServerError exercises the doRequest error-status decoding for
// the various HTTP status codes the handler maps to typed errors.
func TestIAMHandlerServerError(t *testing.T) {
	statuses := []int{
		http.StatusNotFound, http.StatusConflict, http.StatusBadRequest,
		http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError,
	}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"boom","error_description":"details"}`))
			}))
			defer server.Close()

			tr := newTestTransport(t, server)
			_, err := tr.Request(context.Background(), iamUserServicePrefix, "Get",
				map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
			if err == nil {
				t.Fatalf("expected error for status %d", status)
			}
		})
	}
}

// TestIAMHandlerClosedTransport verifies doRequest rejects requests on a closed
// transport.
func TestIAMHandlerClosedTransport(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := tr.Request(context.Background(), iamUserServicePrefix, "Get",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
	if err == nil {
		t.Fatal("expected error on closed transport")
	}
}

// TestIAMSessionParameterizedContract pins the transport contract for the
// parameterized Session verbs: List and RevokeAll always target the
// path-scoped admin surface /api/v1/tenants/{t}/users/{u}/sessions (matching
// the gRPC SessionService semantics) and require both identifiers — the
// self-scoped /api/v1/iam/sessions surface never receives identity query
// parameters from the SDK.
func TestIAMSessionParameterizedContract(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	t.Run("ListEmitsLimitQueryOnly", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "List", map[string]interface{}{
			"request": map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "limit": float64(5)},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/api/v1/tenants/t1/users/u1/sessions?limit=5"
		if rec.gotRawPath != want {
			t.Errorf("raw path: want %s, got %s", want, rec.gotRawPath)
		}
		if strings.Contains(rec.gotRawPath, "user_id=") || strings.Contains(rec.gotRawPath, "tenant_id=") {
			t.Errorf("identity must be path-scoped, not query parameters: %s", rec.gotRawPath)
		}
	})

	t.Run("ListStringLimit", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "List", map[string]interface{}{
			"request": map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "limit": "7"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/api/v1/tenants/t1/users/u1/sessions?limit=7"
		if rec.gotRawPath != want {
			t.Errorf("raw path: want %s, got %s", want, rec.gotRawPath)
		}
	})

	t.Run("ListZeroLimitOmitted", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "List", map[string]interface{}{
			"request": map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "limit": float64(0)},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/api/v1/tenants/t1/users/u1/sessions"
		if rec.gotRawPath != want {
			t.Errorf("raw path: want %s, got %s", want, rec.gotRawPath)
		}
	})

	t.Run("ListMissingTenantID", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "List", map[string]interface{}{
			"request": map[string]interface{}{"user_id": "u1"},
		})
		if err == nil {
			t.Fatal("expected error for missing tenant_id")
		}
		if !strings.Contains(err.Error(), "tenant_id is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("ListMissingUserID", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "List", map[string]interface{}{
			"request": map[string]interface{}{"tenant_id": "t1"},
		})
		if err == nil {
			t.Fatal("expected error for missing user_id")
		}
		if !strings.Contains(err.Error(), "user_id is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("RevokeAllNoQueryParameters", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "RevokeAll", map[string]interface{}{
			"tenant_id": "t1", "user_id": "u1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/api/v1/tenants/t1/users/u1/sessions"
		if rec.gotRawPath != want {
			t.Errorf("raw path: want %s, got %s", want, rec.gotRawPath)
		}
	})

	t.Run("RevokeAllMissingTenantID", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "RevokeAll", map[string]interface{}{
			"user_id": "u1",
		})
		if err == nil {
			t.Fatal("expected error for missing tenant_id")
		}
		if !strings.Contains(err.Error(), "tenant_id is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("RevokeAllMissingUserID", func(t *testing.T) {
		_, err := tr.Request(ctx, iamSessionServicePrefix, "RevokeAll", map[string]interface{}{
			"tenant_id": "t1",
		})
		if err == nil {
			t.Fatal("expected error for missing user_id")
		}
		if !strings.Contains(err.Error(), "user_id is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestIAMHandlerRefusesTheGroupService pins that quicbaas keeps no groups: the
// REST transport routes no quicbaas.IAM.Group request to the server.
func TestIAMHandlerRefusesTheGroupService(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	_, err := tr.Request(context.Background(), "quicbaas.IAM.Group", "List",
		map[string]interface{}{"tenant_id": "t1"})
	if err == nil {
		t.Fatal("a group request was served; quicbaas keeps no groups")
	}
	if rec.gotPath != "" {
		t.Fatalf("a group request reached the server at %s", rec.gotPath)
	}
}
