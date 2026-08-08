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
	"net/http"
	"testing"
)

// TestMapRequestToPermission_XKMSWebAuthnEnrollment pins the PEP vocabulary for
// the canonical xkms WebAuthn mount: enrollment is factor enrollment, so it
// authorizes with the qrdb user_authfactors:* family (the /v1/users/factors
// precedent) rather than the coarse tenants:* the "/api/v1/iam" prefix would
// otherwise apply.
func TestMapRequestToPermission_XKMSWebAuthnEnrollment(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"registration begin", "/api/v1/iam/system/webauthn/registration/begin", http.MethodPost, "user_authfactors", "add"},
		{"registration finish", "/api/v1/iam/system/webauthn/registration/finish", http.MethodPost, "user_authfactors", "add"},
		{"registration status", "/api/v1/iam/system/webauthn/registration/status", http.MethodGet, "user_authfactors", "get"},
		{"other tenant", "/api/v1/iam/acme-corp/webauthn/registration/begin", http.MethodPost, "user_authfactors", "add"},
		{"trailing slash", "/api/v1/iam/system/webauthn/registration/begin/", http.MethodPost, "user_authfactors", "add"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok = false, want true", tc.path, tc.method)
			}
			if resource != tc.wantResource || action != tc.wantAction {
				t.Errorf("mapRequestToPermission(%q, %q) = (%q, %q), want (%q, %q)",
					tc.path, tc.method, resource, action, tc.wantResource, tc.wantAction)
			}
		})
	}
}

// TestMapRequestToPermission_XKMSWebAuthnEnrollmentDoesNotWidenSiblings pins
// that the enrollment rules do not bleed onto neighbouring IAM utility routes,
// which keep the tenants:* / users:* vocabulary they already had.
func TestMapRequestToPermission_XKMSWebAuthnEnrollmentDoesNotWidenSiblings(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"iam sessions", "/api/v1/iam/sessions", http.MethodGet, "users", "read"},
		{"iam audit", "/api/v1/iam/audit", http.MethodGet, "audit", "query"},
		{"iam approve", "/api/v1/iam/users/u1/approve", http.MethodPost, "users", "enable"},
		// The legacy /api/v1/tenants WebAuthn mount is unchanged: it keeps the
		// surface-wide tenants:* mapping its own gate has always used.
		{"legacy register begin", "/api/v1/tenants/system/webauthn/register/begin", http.MethodPost, "tenants", "create"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok = false, want true", tc.path, tc.method)
			}
			if resource != tc.wantResource || action != tc.wantAction {
				t.Errorf("mapRequestToPermission(%q, %q) = (%q, %q), want (%q, %q)",
					tc.path, tc.method, resource, action, tc.wantResource, tc.wantAction)
			}
		})
	}
}

// TestMapRequestToPermission_DelegatedAdmin pins that every endpoint of the
// root-mounted /api/v1/admin group resolves to a registered qrdb permission.
// Without a mapping the validator gate default-denies the whole surface, so
// this table is what makes gating it viable.
func TestMapRequestToPermission_DelegatedAdmin(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"create assignment", "/api/v1/admin/assignments", http.MethodPost, "roles", "assign"},
		{"list assignments", "/api/v1/admin/assignments", http.MethodGet, "roles", "list"},
		{"get assignment", "/api/v1/admin/assignments/a1", http.MethodGet, "roles", "read"},
		{"update assignment", "/api/v1/admin/assignments/a1", http.MethodPut, "roles", "assign"},
		{"delete assignment", "/api/v1/admin/assignments/a1", http.MethodDelete, "roles", "unassign"},
		{"check permission", "/api/v1/admin/check-permission", http.MethodPost, "permissions", "resolve"},
		{"start impersonation", "/api/v1/admin/impersonation/start", http.MethodPost, "users", "update"},
		{"end impersonation", "/api/v1/admin/impersonation/s1/end", http.MethodPost, "users", "update"},
		{"list impersonations", "/api/v1/admin/impersonation", http.MethodGet, "users", "read"},
		{"user assignments", "/api/v1/admin/users/u1/assignments", http.MethodGet, "roles", "list"},
		{"user impersonations", "/api/v1/admin/users/u1/impersonations", http.MethodGet, "users", "read"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok = false, want true", tc.path, tc.method)
			}
			if resource != tc.wantResource || action != tc.wantAction {
				t.Errorf("mapRequestToPermission(%q, %q) = (%q, %q), want (%q, %q)",
					tc.path, tc.method, resource, action, tc.wantResource, tc.wantAction)
			}
		})
	}
}

// TestRequiredScopeForRequest_NewlyGatedSurfaces pins the default-deny scopes
// for the two surfaces this change gated, including the deliberate coarse
// resolution of the tenant-scoped WebAuthn mount (a literal prefix cannot
// express the tenant segment, so the stronger tenants:create applies there).
func TestRequiredScopeForRequest_NewlyGatedSurfaces(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		method string
		want   string
	}{
		{"admin create assignment", "/api/v1/admin/assignments", http.MethodPost, "roles:assign"},
		{"admin list assignments", "/api/v1/admin/assignments", http.MethodGet, "roles:read"},
		{"admin delete assignment", "/api/v1/admin/assignments/a1", http.MethodDelete, "roles:assign"},
		{"webauthn registration begin", "/api/v1/iam/system/webauthn/registration/begin", http.MethodPost, "tenants:create"},
		{"webauthn registration status", "/api/v1/iam/system/webauthn/registration/status", http.MethodGet, "tenants:read"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredScopeForRequest(tc.path, tc.method); got != tc.want {
				t.Errorf("requiredScopeForRequest(%q, %q) = %q, want %q",
					tc.path, tc.method, got, tc.want)
			}
		})
	}
}

// TestMapRequestToPermission_DebugShardRoute pins that the env-gated debug
// mount resolves to a registered qrdb permission. Without a mapping the
// validator gate default-denies it, which would make the newly installed
// authorization chain on that mount a hard 403 rather than a scope check.
func TestMapRequestToPermission_DebugShardRoute(t *testing.T) {
	resource, action, ok := mapRequestToPermission("/api/v1/debug/route/users/123", http.MethodGet)
	if !ok {
		t.Fatal("mapRequestToPermission(debug route) ok = false, want true")
	}
	if resource != "admin" || action != "status" {
		t.Errorf("mapRequestToPermission(debug route) = (%q, %q), want (admin, status)", resource, action)
	}
	if got := requiredScopeForRequest("/api/v1/debug/route/users/123", http.MethodGet); got != "admin:status" {
		t.Errorf("requiredScopeForRequest(debug route) = %q, want admin:status", got)
	}
}
