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

package mcp

import (
	"encoding/json"
	"testing"
	"time"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestDynamicSecretsToolNames pins the eight tool names to the ones quicbaasd
// registers (pkg/api/mcp/dynamic_secrets_tools.go). The generic derivation
// would emit quicbaas_dynamicsecrets_*, which no tool answers, so a drift here
// is a silent 404.
func TestDynamicSecretsToolNames(t *testing.T) {
	want := map[string]string{
		"CreateRole":         "quicbaas_dynamic_secrets_create_role",
		"GetRole":            "quicbaas_dynamic_secrets_get_role",
		"ListRoles":          "quicbaas_dynamic_secrets_list_roles",
		"DeleteRole":         "quicbaas_dynamic_secrets_delete_role",
		"GenerateCredential": "quicbaas_dynamic_secrets_generate",
		"RenewLease":         "quicbaas_dynamic_secrets_renew_lease",
		"RevokeLease":        "quicbaas_dynamic_secrets_revoke_lease",
		"ListLeases":         "quicbaas_dynamic_secrets_list_leases",
	}

	tr := &Transport{}
	for method, expected := range want {
		got, ok := dynamicSecretsToolName(method)
		if !ok || got != expected {
			t.Errorf("dynamicSecretsToolName(%q) = %q, %v; want %q, true", method, got, ok, expected)
		}
		// The transport's own resolution must agree; the generic fallback
		// would otherwise silently take over.
		if mapped := tr.mapToToolName(dynamicSecretsServiceName, method); mapped != expected {
			t.Errorf("mapToToolName(%q) = %q, want %q", method, mapped, expected)
		}
	}

	if _, ok := dynamicSecretsToolName("NotAVerb"); ok {
		t.Error("an unknown method must not resolve to a tool")
	}
}

func TestIsMCPDynamicSecretsService(t *testing.T) {
	tests := []struct {
		svc  string
		want bool
	}{
		{"quicbaas.DynamicSecrets", true},
		{"quicbaas.DynamicSecrets/CreateRole", true},
		{"DynamicSecrets", true},
		{"quicbaas.Secrets", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isMCPDynamicSecretsService(tt.svc); got != tt.want {
			t.Errorf("isMCPDynamicSecretsService(%q) = %v, want %v", tt.svc, got, tt.want)
		}
	}
}

func TestDynamicSecretsHandler_TransformRequest(t *testing.T) {
	h := newDynamicSecretsHandler()

	t.Run("CreateRole encodes durations as strings and config as JSON", func(t *testing.T) {
		args, err := h.transformRequest("CreateRole", &dsclient.CreateRoleRequest{
			Name:          "db-readonly",
			GeneratorType: "postgres",
			Config:        map[string]string{"db": "app"},
			DefaultTTL:    time.Hour,
			MaxTTL:        24 * time.Hour,
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		if args["default_ttl"] != "1h0m0s" {
			t.Errorf("default_ttl = %v, want the duration string", args["default_ttl"])
		}
		if args["max_ttl"] != "24h0m0s" {
			t.Errorf("max_ttl = %v", args["max_ttl"])
		}
		raw, ok := args["config"].(string)
		if !ok {
			t.Fatalf("config = %T, want a JSON string", args["config"])
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("config is not valid JSON: %v", err)
		}
		if decoded["db"] != "app" {
			t.Errorf("config lost data: %+v", decoded)
		}
	})

	t.Run("CreateRole omits an empty config", func(t *testing.T) {
		args, err := h.transformRequest("CreateRole", &dsclient.CreateRoleRequest{
			Name: "r", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: time.Hour,
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		if _, present := args["config"]; present {
			t.Error("an empty config must not be sent")
		}
	})

	t.Run("GetRole and DeleteRole", func(t *testing.T) {
		args, err := h.transformRequest("GetRole", &dsclient.GetRoleRequest{Name: "r"})
		if err != nil || args["name"] != "r" {
			t.Fatalf("GetRole args=%v err=%v", args, err)
		}
		args, err = h.transformRequest("DeleteRole", &dsclient.DeleteRoleRequest{Name: "r"})
		if err != nil || args["name"] != "r" {
			t.Fatalf("DeleteRole args=%v err=%v", args, err)
		}
	})

	t.Run("list verbs pass an optional prefix", func(t *testing.T) {
		args, err := h.transformRequest("ListRoles", &dsclient.ListRolesRequest{Prefix: "db"})
		if err != nil || args["prefix"] != "db" {
			t.Fatalf("ListRoles args=%v err=%v", args, err)
		}
		args, err = h.transformRequest("ListLeases", &dsclient.ListLeasesRequest{})
		if err != nil {
			t.Fatalf("ListLeases: %v", err)
		}
		if _, present := args["prefix"]; present {
			t.Error("an empty prefix must not be sent")
		}
	})

	t.Run("GenerateCredential and lease verbs", func(t *testing.T) {
		args, err := h.transformRequest("GenerateCredential",
			&dsclient.GenerateCredentialRequest{RoleName: "r", TTL: 30 * time.Minute})
		if err != nil {
			t.Fatalf("GenerateCredential: %v", err)
		}
		if args["role_name"] != "r" || args["ttl"] != "30m0s" {
			t.Errorf("args = %+v", args)
		}

		args, err = h.transformRequest("RenewLease",
			&dsclient.RenewLeaseRequest{LeaseID: "l", Increment: time.Hour})
		if err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		if args["lease_id"] != "l" || args["increment"] != "1h0m0s" {
			t.Errorf("args = %+v", args)
		}

		args, err = h.transformRequest("RevokeLease", &dsclient.RevokeLeaseRequest{LeaseID: "l"})
		if err != nil || args["lease_id"] != "l" {
			t.Fatalf("RevokeLease args=%v err=%v", args, err)
		}
	})

	t.Run("zero-valued optional durations are omitted", func(t *testing.T) {
		args, err := h.transformRequest("GenerateCredential",
			&dsclient.GenerateCredentialRequest{RoleName: "r"})
		if err != nil {
			t.Fatalf("GenerateCredential: %v", err)
		}
		if _, present := args["ttl"]; present {
			t.Error("a zero TTL must not be sent; the role default applies")
		}
	})
}

func TestDynamicSecretsHandler_TransformRequestErrors(t *testing.T) {
	h := newDynamicSecretsHandler()

	t.Run("unknown method", func(t *testing.T) {
		_, err := h.transformRequest("NotAVerb", nil)
		if err == nil || !sdkerrors.IsUnimplemented(err) {
			t.Fatalf("got %v, want an Unimplemented error", err)
		}
	})

	t.Run("wrong request type", func(t *testing.T) {
		for _, method := range []string{
			"CreateRole", "GetRole", "DeleteRole",
			"GenerateCredential", "RenewLease", "RevokeLease",
		} {
			if _, err := h.transformRequest(method, "not-a-request"); err == nil {
				t.Errorf("%s accepted a string request", method)
			}
		}
	})

	t.Run("missing required identifiers", func(t *testing.T) {
		if _, err := h.transformRequest("GetRole", &dsclient.GetRoleRequest{}); err == nil {
			t.Error("GetRole accepted an empty name")
		}
		if _, err := h.transformRequest("GenerateCredential", &dsclient.GenerateCredentialRequest{}); err == nil {
			t.Error("GenerateCredential accepted an empty role name")
		}
		if _, err := h.transformRequest("RenewLease", &dsclient.RenewLeaseRequest{}); err == nil {
			t.Error("RenewLease accepted an empty lease ID")
		}
		if _, err := h.transformRequest("RevokeLease", &dsclient.RevokeLeaseRequest{}); err == nil {
			t.Error("RevokeLease accepted an empty lease ID")
		}
	})
}

func TestDynamicSecretsHandler_TransformResponse(t *testing.T) {
	h := newDynamicSecretsHandler()
	const stamp = "2023-11-14T22:13:20Z"

	t.Run("CreateRole", func(t *testing.T) {
		got, err := h.transformResponse("CreateRole", map[string]interface{}{
			"created_at": stamp,
			"role": map[string]interface{}{
				"name":           "db-readonly",
				"generator_type": "postgres",
				"default_ttl":    "1h0m0s",
				"max_ttl":        "24h0m0s",
				"config":         map[string]interface{}{"db": "app"},
			},
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		resp := got.(*dsclient.CreateRoleResponse)
		if resp.Role.Name != "db-readonly" || resp.Role.DefaultTTL != time.Hour {
			t.Errorf("role = %+v", resp.Role)
		}
		if resp.Role.Config["db"] != "app" {
			t.Errorf("config lost: %+v", resp.Role.Config)
		}
		if resp.CreatedAt.IsZero() {
			t.Error("created_at not parsed")
		}
	})

	t.Run("GenerateCredential", func(t *testing.T) {
		got, err := h.transformResponse("GenerateCredential", map[string]interface{}{
			"lease_id": "lease-1",
			"credential": map[string]interface{}{
				"credential_id": "cred-1",
				"type":          "database",
				"lease_id":      "lease-1",
				"expires_at":    stamp,
				"data":          map[string]interface{}{"username": "u", "password": "p"},
			},
			"lease": map[string]interface{}{
				"lease_id":    "lease-1",
				"secret_path": "postgres/creds/db-readonly",
				"ttl":         "1h0m0s",
				"expires_at":  stamp,
				"renewable":   true,
				"renew_count": float64(2),
			},
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		resp := got.(*dsclient.GenerateCredentialResponse)
		if resp.LeaseID != "lease-1" || resp.Credential.CredentialID != "cred-1" {
			t.Errorf("resp = %+v", resp)
		}
		if resp.Credential.Data["password"] != "p" {
			t.Errorf("credential data lost: %+v", resp.Credential.Data)
		}
		if resp.Lease.RenewCount != 2 || !resp.Lease.Renewable || resp.Lease.TTL != time.Hour {
			t.Errorf("lease = %+v", resp.Lease)
		}
	})

	t.Run("list verbs", func(t *testing.T) {
		got, err := h.transformResponse("ListRoles", map[string]interface{}{
			"roles": []interface{}{
				map[string]interface{}{"name": "a"},
				"not-an-object",
				map[string]interface{}{"name": "b"},
			},
			"count": float64(2),
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		roles := got.(*dsclient.ListRolesResponse)
		if len(roles.Roles) != 2 {
			t.Errorf("non-object entries must be skipped, got %d roles", len(roles.Roles))
		}

		got, err = h.transformResponse("ListLeases", map[string]interface{}{
			"leases": []interface{}{
				map[string]interface{}{"lease_id": "l", "metadata": map[string]interface{}{"k": "v"}},
			},
			"count": float64(1),
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		leases := got.(*dsclient.ListLeasesResponse)
		if len(leases.Leases) != 1 || leases.Leases[0].Metadata["k"] != "v" {
			t.Errorf("leases = %+v", leases.Leases)
		}
	})

	t.Run("timestamp-only verbs", func(t *testing.T) {
		got, err := h.transformResponse("DeleteRole", map[string]interface{}{"deleted_at": stamp})
		if err != nil || got.(*dsclient.DeleteRoleResponse).DeletedAt.IsZero() {
			t.Fatalf("DeleteRole: %v, %+v", err, got)
		}
		got, err = h.transformResponse("RevokeLease", map[string]interface{}{"revoked_at": stamp})
		if err != nil || got.(*dsclient.RevokeLeaseResponse).RevokedAt.IsZero() {
			t.Fatalf("RevokeLease: %v, %+v", err, got)
		}
		got, err = h.transformResponse("RenewLease", map[string]interface{}{
			"renewed_at": stamp,
			"lease":      map[string]interface{}{"lease_id": "l", "renew_count": float64(3)},
		})
		if err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		if got.(*dsclient.RenewLeaseResponse).Lease.RenewCount != 3 {
			t.Errorf("renew count lost: %+v", got)
		}
	})

	t.Run("missing nested objects yield nil, not a panic", func(t *testing.T) {
		got, err := h.transformResponse("GetRole", map[string]interface{}{})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		if got.(*dsclient.GetRoleResponse).Role != nil {
			t.Error("an absent role must decode to nil")
		}
	})

	t.Run("malformed duration decodes to zero", func(t *testing.T) {
		got, err := h.transformResponse("GetRole", map[string]interface{}{
			"role": map[string]interface{}{"name": "r", "default_ttl": "not-a-duration"},
		})
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		if got.(*dsclient.GetRoleResponse).Role.DefaultTTL != 0 {
			t.Error("a malformed duration must decode to zero, not error")
		}
	})

	t.Run("unexpected result shape errors", func(t *testing.T) {
		if _, err := h.transformResponse("GetRole", "not-an-object"); err == nil {
			t.Fatal("expected an error for a scalar tool result")
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		_, err := h.transformResponse("NotAVerb", map[string]interface{}{})
		if err == nil || !sdkerrors.IsUnimplemented(err) {
			t.Fatalf("got %v, want an Unimplemented error", err)
		}
	})
}
