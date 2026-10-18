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

// iam_extra_test.go covers the IAM MCP handler's response-mapping helpers and
// argument-transformation helpers that the end-to-end IAM tests do not reach:
// app/policy/list unwrapping, PEP check/batch rewriting, enum-to-string
// conversions, settings typing, and YAML/base64 decoding.

package mcp

import (
	"encoding/base64"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func newIAMHandlerForTest(t *testing.T) *iamHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newIAMHandler(tr)
}

// =============================================================================
// Response mapping
// =============================================================================

func TestIAMTransformResponseRouting(t *testing.T) {
	h := newIAMHandlerForTest(t)

	t.Run("nil and non-map passthrough", func(t *testing.T) {
		if r, err := h.transformResponse("quicbaas.App", "Get", nil); err != nil || r != nil {
			t.Errorf("nil = %v,%v", r, err)
		}
		if r, err := h.transformResponse("quicbaas.App", "Get", "scalar"); err != nil || r != "scalar" {
			t.Errorf("scalar = %v,%v", r, err)
		}
	})

	t.Run("App.Get unwraps app", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.App", "Get", map[string]interface{}{
			"app":           map[string]interface{}{"client_id": "id"},
			"client_secret": "sk",
			"timestamp":     "t",
		})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if m["client_id"] != "id" || m["client_secret"] != "sk" {
			t.Errorf("app unwrap = %v", m)
		}
	})

	t.Run("App.List renames apps", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.App", "List", map[string]interface{}{
			"apps": []interface{}{}, "total_count": float64(0)})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := out.(map[string]interface{})["applications"]; !ok {
			t.Errorf("App.List did not rename to applications: %v", out)
		}
	})

	t.Run("Federation.ListProviders unwraps", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.Federation", "ListProviders", map[string]interface{}{
			"providers": []interface{}{"p1"}})
		if err != nil {
			t.Fatal(err)
		}
		if arr, ok := out.([]interface{}); !ok || len(arr) != 1 {
			t.Errorf("providers unwrap = %v", out)
		}
	})

	t.Run("Policy.Get maps server format", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.Policy", "Get", map[string]interface{}{
			"policy": map[string]interface{}{
				"apiVersion": "v1",
				"metadata":   map[string]interface{}{"name": "p", "tenant": "tn"},
				"spec": map[string]interface{}{
					"rules": []interface{}{map[string]interface{}{
						"actions": []interface{}{"read"}, "resources": []interface{}{"*"},
						"effect": "allow",
					}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if m["name"] != "p" || m["tenant_id"] != "tn" {
			t.Errorf("policy map = %v", m)
		}
	})

	t.Run("Policy.List maps each", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.Policy", "List", map[string]interface{}{
			"policies": []interface{}{
				map[string]interface{}{"apiVersion": "v1",
					"metadata": map[string]interface{}{"name": "p"}},
			},
			"total_count": float64(1),
		})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if len(m["policies"].([]interface{})) != 1 {
			t.Errorf("policy list = %v", m)
		}
	})

	t.Run("Admin.ListAssignments maps role", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.Admin", "ListAssignments", map[string]interface{}{
			"assignments": []interface{}{
				map[string]interface{}{"admin_role": "owner"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		a := out.(map[string]interface{})["assignments"].([]interface{})[0].(map[string]interface{})
		if a["role_name"] != "owner" {
			t.Errorf("role_name not set: %v", a)
		}
	})

	t.Run("GDPR.ListConsents wraps", func(t *testing.T) {
		out, err := h.transformResponse("quicbaas.GDPR", "ListConsents", map[string]interface{}{
			"consents": []interface{}{"c1", "c2"}})
		if err != nil {
			t.Fatal(err)
		}
		if out.(map[string]interface{})["total_count"] != 2 {
			t.Errorf("consents total = %v", out)
		}
	})

	t.Run("unknown method passthrough", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformResponse("quicbaas.Other", "Thing", in)
		if err != nil {
			t.Fatal(err)
		}
		if out.(map[string]interface{})["x"] != 1 {
			t.Error("expected passthrough")
		}
	})
}

func TestIAMUnwrapHelpers(t *testing.T) {
	// unwrapAppResponse: no "app" key -> passthrough.
	if got := unwrapAppResponse(map[string]interface{}{"x": 1}); got.(map[string]interface{})["x"] != 1 {
		t.Error("unwrapAppResponse passthrough failed")
	}
	// app present but not a map -> passthrough.
	if got := unwrapAppResponse(map[string]interface{}{"app": "scalar"}); got.(map[string]interface{})["app"] != "scalar" {
		t.Error("unwrapAppResponse non-map app should passthrough")
	}

	// unwrapListResponse: missing key -> passthrough whole map.
	if got := unwrapListResponse(map[string]interface{}{"x": 1}, "providers"); got.(map[string]interface{})["x"] != 1 {
		t.Error("unwrapListResponse passthrough failed")
	}

	// unwrapFieldResponse: present and missing.
	if got := unwrapFieldResponse(map[string]interface{}{"k": "v"}, "k"); got != "v" {
		t.Errorf("unwrapFieldResponse = %v", got)
	}
	if got := unwrapFieldResponse(map[string]interface{}{"a": 1}, "k"); got.(map[string]interface{})["a"] != 1 {
		t.Error("unwrapFieldResponse missing key should passthrough")
	}
}

func TestMapMCPServerPolicyPassthrough(t *testing.T) {
	// Without apiVersion/metadata it is not server-format -> returned unchanged.
	in := map[string]interface{}{"name": "flat"}
	out := mapMCPServerPolicyToSDK(in)
	if out.(map[string]interface{})["name"] != "flat" {
		t.Errorf("flat policy should passthrough: %v", out)
	}
}

func TestTransformMCPServerRulesToSDK(t *testing.T) {
	rules := []interface{}{
		map[string]interface{}{
			"actions":   []interface{}{"read", "write"},
			"resources": []interface{}{"db/*"},
			"effect":    "deny",
			"subjects": map[string]interface{}{
				"authenticated": true,
				"users":         []interface{}{"alice"},
			},
		},
	}
	out := transformMCPServerRulesToSDK(rules).([]interface{})
	rule := out[0].(map[string]interface{})
	if len(rule["actions"].([]interface{})) != 2 {
		t.Errorf("actions not matcher-wrapped: %v", rule["actions"])
	}
	if rule["effect"] != float64(0) {
		t.Errorf("deny effect = %v, want 0", rule["effect"])
	}
	if len(rule["subjects"].([]interface{})) != 2 {
		t.Errorf("subjects = %v", rule["subjects"])
	}

	// Non-array input passes through unchanged.
	if got := transformMCPServerRulesToSDK("scalar"); got != "scalar" {
		t.Error("non-array rules should passthrough")
	}
}

// =============================================================================
// Argument transformation helpers
// =============================================================================

func TestTransformPEPCheckArguments(t *testing.T) {
	// Nested subject object -> principal + tenant_id.
	out := transformPEPCheckArguments(map[string]interface{}{
		"subject": map[string]interface{}{"id": "u1", "tenant_id": "t1"},
		"action":  "read",
	})
	if out["principal"] != "u1" || out["tenant_id"] != "t1" {
		t.Errorf("nested subject = %v", out)
	}
	if _, ok := out["subject"]; ok {
		t.Error("subject should be removed")
	}

	// String subject -> principal.
	out2 := transformPEPCheckArguments(map[string]interface{}{"subject": "u2"})
	if out2["principal"] != "u2" {
		t.Errorf("string subject = %v", out2)
	}

	// No subject -> unchanged.
	out3 := transformPEPCheckArguments(map[string]interface{}{"action": "x"})
	if out3["action"] != "x" {
		t.Errorf("no subject = %v", out3)
	}
}

func TestTransformPEPBatchArguments(t *testing.T) {
	out := transformPEPBatchArguments(map[string]interface{}{
		"requests": []interface{}{
			map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}, "action": "read"},
		},
	})
	checks, ok := out["checks"].([]interface{})
	if !ok || len(checks) != 1 {
		t.Fatalf("checks = %v", out["checks"])
	}
	if checks[0].(map[string]interface{})["principal"] != "u1" {
		t.Errorf("batch check not transformed: %v", checks[0])
	}
	if _, ok := out["requests"]; ok {
		t.Error("requests should be renamed to checks")
	}
}

func TestConvertGrantTypesToStrings(t *testing.T) {
	out := convertGrantTypesToStrings(map[string]interface{}{
		"grant_types": []interface{}{"authorization_code", float64(0)},
	})
	items := out["grant_types"].([]interface{})
	if len(items) == 0 || items[0] != "authorization_code" {
		t.Errorf("grant types = %v", items)
	}

	// No grant_types -> unchanged.
	if got := convertGrantTypesToStrings(map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Error("no grant_types should passthrough")
	}
	// Non-array grant_types -> unchanged.
	if got := convertGrantTypesToStrings(map[string]interface{}{"grant_types": "s"}); got["grant_types"] != "s" {
		t.Error("non-array grant_types should passthrough")
	}
}

func TestConvertMCPSettingsValues(t *testing.T) {
	out := convertMCPSettingsValues(map[string]interface{}{
		"settings": map[string]interface{}{
			"max_users":    "10",
			"mfa_required": "true",
			"name":         "acme",
			"keep":         42,
		},
	})
	s := out["settings"].(map[string]interface{})
	if s["max_users"] != 10 || s["mfa_required"] != true || s["name"] != "acme" || s["keep"] != 42 {
		t.Errorf("settings = %v", s)
	}

	// Invalid numeric/bool fall back to the raw string.
	out2 := convertMCPSettingsValues(map[string]interface{}{
		"settings": map[string]interface{}{"max_users": "abc", "mfa_required": "maybe"},
	})
	s2 := out2["settings"].(map[string]interface{})
	if s2["max_users"] != "abc" || s2["mfa_required"] != "maybe" {
		t.Errorf("invalid settings fallback = %v", s2)
	}

	// No settings -> unchanged.
	if got := convertMCPSettingsValues(map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Error("no settings should passthrough")
	}
}

func TestExtractConsentPurpose(t *testing.T) {
	out := extractConsentPurpose(map[string]interface{}{"purpose": float64(1)})
	if out["purpose"] != "analytics" {
		t.Errorf("purpose = %v", out["purpose"])
	}
	// Already a string -> unchanged.
	if got := extractConsentPurpose(map[string]interface{}{"purpose": "marketing"}); got["purpose"] != "marketing" {
		t.Error("string purpose should passthrough")
	}
	// No purpose -> unchanged.
	if got := extractConsentPurpose(map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Error("no purpose should passthrough")
	}
}

func TestConvertExportFormat(t *testing.T) {
	if got := convertExportFormat(map[string]interface{}{"format": float64(1)}); got["format"] != "csv" {
		t.Errorf("float format = %v", got["format"])
	}
	if got := convertExportFormat(map[string]interface{}{"format": 2}); got["format"] != "xml" {
		t.Errorf("int format = %v", got["format"])
	}
	if got := convertExportFormat(map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Error("no format should passthrough")
	}
}

func TestDecodeSpecYAMLBase64(t *testing.T) {
	yaml := "apiVersion: v1\nkind: Policy"
	encoded := base64.StdEncoding.EncodeToString([]byte(yaml))
	out := decodeSpecYAMLBase64(map[string]interface{}{"spec_yaml": encoded})
	if out["spec_yaml"] != yaml {
		t.Errorf("spec_yaml = %v", out["spec_yaml"])
	}
	// Non-string spec_yaml -> unchanged.
	if got := decodeSpecYAMLBase64(map[string]interface{}{"spec_yaml": 5}); got["spec_yaml"] != 5 {
		t.Error("non-string spec_yaml should passthrough")
	}
	// No spec_yaml -> unchanged.
	if got := decodeSpecYAMLBase64(map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Error("no spec_yaml should passthrough")
	}
}

func TestConvertMapResponseJSON(t *testing.T) {
	out, err := convertMapResponseJSON(map[string]interface{}{"a": "b"})
	if err != nil {
		t.Fatalf("convertMapResponseJSON: %v", err)
	}
	if out["a"] != "b" {
		t.Errorf("out = %v", out)
	}
	// Unmarshalable into a map (a scalar) errors.
	if _, err := convertMapResponseJSON(42); err == nil {
		t.Error("scalar should fail to unmarshal into map")
	}
}

func TestRegroupPolicyFields(t *testing.T) {
	// Already grouped under "policy" -> unchanged.
	grouped := map[string]interface{}{"policy": map[string]interface{}{"name": "p"}}
	if got := regroupPolicyFields(grouped); got["policy"] == nil {
		t.Error("already grouped should passthrough")
	}

	// Flat fields get regrouped under "policy", tenant_id preserved.
	out := regroupPolicyFields(map[string]interface{}{
		"tenant_id": "tn",
		"rules":     []interface{}{},
	})
	if out["tenant_id"] != "tn" {
		t.Errorf("tenant_id not preserved: %v", out)
	}
	if _, ok := out["policy"].(map[string]interface{}); !ok {
		t.Errorf("rules not regrouped under policy: %v", out)
	}
}

func TestRemoveZeroExpiresAt(t *testing.T) {
	// nil args is a no-op.
	removeZeroExpiresAt(nil)

	// A zero-time string is removed.
	args := map[string]interface{}{"expires_at": "0001-01-01T00:00:00Z"}
	removeZeroExpiresAt(args)
	if _, ok := args["expires_at"]; ok {
		t.Error("zero expires_at should be removed")
	}

	// A real timestamp is preserved.
	args2 := map[string]interface{}{"expires_at": "2026-05-29T00:00:00Z"}
	removeZeroExpiresAt(args2)
	if _, ok := args2["expires_at"]; !ok {
		t.Error("non-zero expires_at should be preserved")
	}

	// No expires_at is a no-op.
	args3 := map[string]interface{}{"x": 1}
	removeZeroExpiresAt(args3)
	if args3["x"] != 1 {
		t.Error("unrelated args should be untouched")
	}
}

// TestTransformIAMToolArguments drives each tool-name case of the IAM argument
// transformer to verify the SDK->server field renames and enum conversions.
func TestTransformIAMToolArguments(t *testing.T) {
	if transformIAMToolArguments("any", nil) != nil {
		t.Error("nil args should return nil")
	}

	t.Run("tenant get renames tenant_id to id", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_tenant_get",
			map[string]interface{}{"tenant_id": "t1"})
		if out["id"] != "t1" {
			t.Errorf("id = %v", out["id"])
		}
		if _, ok := out["tenant_id"]; ok {
			t.Error("tenant_id should be removed")
		}
	})

	t.Run("tenant update passes display_name through and types settings", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_tenant_update",
			map[string]interface{}{
				"tenant_id":    "t1",
				"display_name": "Acme",
				"settings":     map[string]interface{}{"max_users": "5"},
			})
		if out["display_name"] != "Acme" || out["id"] != "t1" {
			t.Errorf("update = %v", out)
		}
		if _, hasName := out["name"]; hasName {
			t.Errorf("display_name must not be renamed to name: %v", out)
		}
		if out["settings"].(map[string]interface{})["max_users"] != 5 {
			t.Errorf("settings not typed: %v", out["settings"])
		}
	})

	t.Run("tenant create", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_tenant_create",
			map[string]interface{}{"display_name": "Acme"})
		if out["name"] != "Acme" {
			t.Errorf("create name = %v", out["name"])
		}
	})

	t.Run("admin assignment create maps role and status", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_admin_assignment_create",
			map[string]interface{}{
				"role_id":    "owner",
				"status":     float64(0),
				"expires_at": "0001-01-01T00:00:00Z",
			})
		if out["admin_role"] != "owner" || out["active"] != true {
			t.Errorf("assignment = %v", out)
		}
		if _, ok := out["expires_at"]; ok {
			t.Error("zero expires_at should be removed")
		}
	})

	t.Run("admin assignment list maps admin_id", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_admin_assignment_list",
			map[string]interface{}{"admin_id": "u1", "status": float64(0)})
		if out["user_id"] != "u1" || out["active_only"] != true {
			t.Errorf("list = %v", out)
		}
	})

	t.Run("app register converts grant types", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_app_register",
			map[string]interface{}{"grant_types": []interface{}{float64(0)}})
		if _, ok := out["grant_types"]; !ok {
			t.Error("grant_types missing")
		}
	})

	t.Run("pep check rewrites subject", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_pep_check",
			map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}})
		if out["principal"] != "u1" {
			t.Errorf("pep check = %v", out)
		}
	})

	t.Run("pep check batch rewrites requests", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_pep_check_batch",
			map[string]interface{}{"requests": []interface{}{
				map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}},
			}})
		if _, ok := out["checks"]; !ok {
			t.Error("checks missing")
		}
	})

	t.Run("federation maps provider_id", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_federation_getprovider",
			map[string]interface{}{"provider_id": "p1"})
		if out["id"] != "p1" {
			t.Errorf("federation = %v", out)
		}
	})

	t.Run("policy get maps policy_name", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_policy_get",
			map[string]interface{}{"policy_name": "p1"})
		if out["name"] != "p1" {
			t.Errorf("policy get = %v", out)
		}
	})

	t.Run("policy create regroups", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_policy_create",
			map[string]interface{}{"tenant_id": "t1", "name": "p1"})
		if _, ok := out["policy"].(map[string]interface{}); !ok {
			t.Errorf("policy create = %v", out)
		}
	})

	t.Run("policy validate decodes spec", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_policy_validate",
			map[string]interface{}{"policy": map[string]interface{}{"name": "p"}})
		if _, ok := out["policy"]; !ok {
			t.Error("policy missing")
		}
	})

	t.Run("gdpr export maps requested_by and format", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_gdpr_export_request",
			map[string]interface{}{"user_id": "u1", "format": float64(1)})
		if out["requested_by"] != "u1" || out["format"] != "csv" {
			t.Errorf("gdpr export = %v", out)
		}
	})

	t.Run("gdpr deletion maps requested_by", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_gdpr_deletion_request",
			map[string]interface{}{"user_id": "u1"})
		if out["requested_by"] != "u1" {
			t.Errorf("gdpr deletion = %v", out)
		}
	})

	t.Run("unknown tool passes through", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_unknown",
			map[string]interface{}{"x": 1})
		if out["x"] != 1 {
			t.Errorf("unknown = %v", out)
		}
	})
}

// TestTransformIAMToolArguments_UserCreate verifies the SDK->server field
// renames for the create-user tool: the SDK CreateUserRequest serializes
// display_name and metadata, but the canonical create-user contract (REST
// body, QUIC wire, gRPC proto, MCP tool schema) uses name and attributes.
func TestTransformIAMToolArguments_UserCreate(t *testing.T) {
	t.Run("display_name renamed to name", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_user_create",
			map[string]interface{}{
				"tenant_id":    "t1",
				"email":        "u@example.com",
				"display_name": "Canonical User",
			})
		if out["name"] != "Canonical User" {
			t.Errorf("name = %v", out["name"])
		}
		if _, ok := out["display_name"]; ok {
			t.Error("display_name should be removed")
		}
	})

	t.Run("metadata renamed to attributes", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_user_create",
			map[string]interface{}{
				"tenant_id": "t1",
				"metadata":  map[string]interface{}{"department": "engineering"},
			})
		attrs, ok := out["attributes"].(map[string]interface{})
		if !ok || attrs["department"] != "engineering" {
			t.Errorf("attributes = %v", out["attributes"])
		}
		if _, ok := out["metadata"]; ok {
			t.Error("metadata should be removed")
		}
	})

	t.Run("existing name and attributes take precedence", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_user_create",
			map[string]interface{}{
				"name":         "Explicit",
				"display_name": "Ignored",
				"attributes":   map[string]interface{}{"a": "1"},
				"metadata":     map[string]interface{}{"b": "2"},
			})
		if out["name"] != "Explicit" {
			t.Errorf("name = %v", out["name"])
		}
		attrs := out["attributes"].(map[string]interface{})
		if attrs["a"] != "1" {
			t.Errorf("attributes = %v", attrs)
		}
	})

	t.Run("roles pass through untouched", func(t *testing.T) {
		out := transformIAMToolArguments("quicbaas_iam_user_create",
			map[string]interface{}{
				"roles": []interface{}{"role-a"},
			})
		if roles := out["roles"].([]interface{}); len(roles) != 1 || roles[0] != "role-a" {
			t.Errorf("roles = %v", out["roles"])
		}
	})
}
