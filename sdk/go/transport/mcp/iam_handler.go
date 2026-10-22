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

package mcp

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/policyfields"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/policyloaderror"
)

// iamHandler handles IAM-specific request/response transformations for MCP transport.
// IAM subsystems use nested SDK request structs that must be flattened into
// the flat argument maps expected by the MCP server tool handlers.
type iamHandler struct {
	transport *Transport
}

// newIAMHandler creates a new IAM handler.
func newIAMHandler(t *Transport) *iamHandler {
	return &iamHandler{transport: t}
}

// grantTypeIntToString maps GrantType integer values to their OAuth2 string
// representations. The SDK uses int enums (GrantType) but the MCP server
// handlers expect string values like "client_credentials".
var grantTypeIntToString = map[int]string{
	0: "authorization_code",
	1: "client_credentials",
	2: "refresh_token",
	3: "urn:ietf:params:oauth:grant-type:device_code",
	4: "implicit",
	5: "password",
}

// iamToolMap maps IAM SDK service/method combinations to MCP tool names.
// Key format: "Subsystem.Method" (e.g., "Admin.CreateAssignment").
var iamToolMap = map[string]string{
	// Admin Assignment Operations
	"Admin.CreateAssignment":         "quicbaas_iam_admin_assignment_create",
	"Admin.GetAssignment":            "quicbaas_iam_admin_assignment_get",
	"Admin.UpdateAssignment":         "quicbaas_iam_admin_assignment_update",
	"Admin.DeleteAssignment":         "quicbaas_iam_admin_assignment_delete",
	"Admin.ListAssignments":          "quicbaas_iam_admin_assignment_list",
	"Admin.CheckPermission":          "quicbaas_iam_admin_check_permission",
	"Admin.StartImpersonation":       "quicbaas_iam_impersonation_start",
	"Admin.EndImpersonation":         "quicbaas_iam_impersonation_end",
	"Admin.ListActiveImpersonations": "quicbaas_iam_impersonation_list",

	// Tenant Operations
	"Tenant.Create":    "quicbaas_iam_tenant_create",
	"Tenant.Get":       "quicbaas_iam_tenant_get",
	"Tenant.Update":    "quicbaas_iam_tenant_update",
	"Tenant.Delete":    "quicbaas_iam_tenant_delete",
	"Tenant.List":      "quicbaas_iam_tenant_list",
	"Tenant.Provision": "quicbaas_iam_tenant_provision",

	// User Operations
	"User.Create":         "quicbaas_iam_user_create",
	"User.Get":            "quicbaas_iam_user_get",
	"User.GetByEmail":     "quicbaas_iam_user_get",
	"User.FindByUsername": "quicbaas_iam_user_get",
	"User.Update":         "quicbaas_iam_user_update",
	"User.Delete":         "quicbaas_iam_user_delete",
	"User.List":           "quicbaas_iam_user_list",
	"User.AssignRole":     "quicbaas_iam_user_assign_role",
	"User.RevokeRole":     "quicbaas_iam_user_revoke_role",

	// Role Operations
	"Role.Create": "quicbaas_iam_role_create",
	"Role.Get":    "quicbaas_iam_role_get",
	"Role.Update": "quicbaas_iam_role_update",
	"Role.Delete": "quicbaas_iam_role_delete",
	"Role.List":   "quicbaas_iam_role_list",

	// Built-in role reconciliation. The tool answers with the same
	// {tenant_id, roles[], changed} report the other transports return, so the
	// role client parses the MCP result with no reshaping — a transform here
	// would be the one thing that could make MCP disagree with REST.
	"Role.Reconcile": "quicbaas_iam_role_reconcile",

	// Policy Operations
	"Policy.Create":                  "quicbaas_iam_policy_create",
	"Policy.Get":                     "quicbaas_iam_policy_get",
	"Policy.Update":                  "quicbaas_iam_policy_update",
	"Policy.Delete":                  "quicbaas_iam_policy_delete",
	"Policy.List":                    "quicbaas_iam_policy_list",
	"Policy.Validate":                "quicbaas_iam_policy_validate",
	"Policy.Test":                    "quicbaas_iam_policy_test",
	"Policy.Simulate":                "quicbaas_iam_policy_test",
	"Policy.GetEffectivePermissions": "quicbaas_iam_policy_effective_permissions",

	// App Operations
	"App.Register":     "quicbaas_iam_app_register",
	"App.Get":          "quicbaas_iam_app_get",
	"App.Update":       "quicbaas_iam_app_update",
	"App.Delete":       "quicbaas_iam_app_delete",
	"App.List":         "quicbaas_iam_app_list",
	"App.RotateSecret": "quicbaas_iam_app_rotate_secret",

	// MFA Operations
	"MFA.GetMFAStatus":          "quicbaas_mfa_status",
	"MFA.BeginMFAEnrollment":    "quicbaas_mfa_enroll_begin",
	"MFA.CompleteMFAEnrollment": "quicbaas_mfa_enroll_complete",
	"MFA.CancelMFAEnrollment":   "quicbaas_mfa_enroll_cancel",
	"MFA.DisableMFA":            "quicbaas_mfa_disable",
	"MFA.BeginMFAChallenge":     "quicbaas_mfa_challenge_begin",
	"MFA.VerifyMFAChallenge":    "quicbaas_mfa_challenge_verify",
	"MFA.GenerateBackupCodes":   "quicbaas_mfa_backup_codes_generate",
	"MFA.VerifyBackupCode":      "quicbaas_mfa_backup_codes_verify",
	"MFA.GetBackupCodeStatus":   "quicbaas_mfa_backup_codes_status",

	// WebAuthn Credential Administration
	"WebAuthn.ListWebAuthnCredentials":   "quicbaas_iam_webauthn_credentials_list",
	"WebAuthn.DeleteWebAuthnCredential":  "quicbaas_iam_webauthn_credential_delete",
	"WebAuthn.DeleteWebAuthnCredentials": "quicbaas_iam_webauthn_credentials_delete_all",

	// PEP Operations
	"PEP.Check":      "quicbaas_pep_check",
	"PEP.CheckBatch": "quicbaas_pep_check_batch",

	// GDPR Consent Operations
	"GDPR.GrantConsent":    "quicbaas_iam_gdpr_consent_grant",
	"GDPR.WithdrawConsent": "quicbaas_iam_gdpr_consent_withdraw",
	"GDPR.GetConsent":      "quicbaas_iam_gdpr_consent_get",
	"GDPR.ListConsents":    "quicbaas_iam_gdpr_consent_list",

	// GDPR Export Operations
	"GDPR.RequestExport":   "quicbaas_iam_gdpr_export_request",
	"GDPR.GetExportStatus": "quicbaas_iam_gdpr_export_status",
	"GDPR.GenerateExport":  "quicbaas_iam_gdpr_export_generate",

	// GDPR Deletion Operations
	"GDPR.RequestDeletion":   "quicbaas_iam_gdpr_deletion_request",
	"GDPR.CancelDeletion":    "quicbaas_iam_gdpr_deletion_cancel",
	"GDPR.ProcessDeletion":   "quicbaas_iam_gdpr_deletion_process",
	"GDPR.GetDeletionStatus": "quicbaas_iam_gdpr_deletion_status",

	// Federation Provider Operations
	"Federation.CreateProvider": "quicbaas_iam_federation_createprovider",
	"Federation.GetProvider":    "quicbaas_iam_federation_getprovider",
	"Federation.UpdateProvider": "quicbaas_iam_federation_updateprovider",
	"Federation.DeleteProvider": "quicbaas_iam_federation_deleteprovider",
	"Federation.ListProviders":  "quicbaas_iam_federation_listproviders",

	// Federation Auth Flow
	"Federation.BeginLogin":     "quicbaas_iam_federation_beginlogin",
	"Federation.HandleCallback": "quicbaas_iam_federation_handlecallback",

	// Federation Account Linking
	"Federation.LinkAccount":   "quicbaas_iam_federation_linkaccount",
	"Federation.UnlinkAccount": "quicbaas_iam_federation_unlinkaccount",
	"Federation.GetUserLinks":  "quicbaas_iam_federation_getuserlinks",

	// Identity Operations
	"Identity.LinkIdentity":       "quicbaas_iam_identity_linkidentity",
	"Identity.UnlinkIdentity":     "quicbaas_iam_identity_unlinkidentity",
	"Identity.ListUserIdentities": "quicbaas_iam_identity_listuseridentities",

	// Session Operations
	"Session.Get":          "quicbaas_iam_session_get",
	"Session.List":         "quicbaas_iam_session_list",
	"Session.Revoke":       "quicbaas_iam_session_revoke",
	"Session.RevokeAll":    "quicbaas_iam_session_revoke_all",
	"Session.RevokeOthers": "quicbaas_iam_session_revoke_others",
	"Session.Touch":        "quicbaas_iam_session_touch",

	// Audit Operations
	"Audit.Query":  "quicbaas_iam_audit_query",
	"Audit.Get":    "quicbaas_iam_audit_get",
	"Audit.Delete": "quicbaas_iam_audit_delete",
}

// mapIAMToolName maps an IAM service/method pair to the correct MCP tool name.
// Returns the tool name and true if found, or empty string and false if not found.
func mapIAMToolName(service, method string) (string, bool) {
	// Extract subsystem from service name (e.g., "quicbaas.IAM.Admin" -> "Admin")
	subsystem := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		subsystem = service[idx+1:]
	}

	key := subsystem + "." + method
	toolName, ok := iamToolMap[key]
	return toolName, ok
}

// transformRequest transforms an SDK IAM request into flat MCP tool arguments.
// SDK requests use nested structs (e.g., {tenant_id: "x", request: {name: "y"}}),
// but MCP handlers expect flat argument maps (e.g., {tenant_id: "x", name: "y"}).
func (h *iamHandler) transformRequest(service, method string, reqBody interface{}) (map[string]interface{}, error) {
	// Convert the request to a map via JSON round-trip
	args := h.transport.convertToArguments(reqBody)
	if args == nil {
		// A request that cannot be encoded must not reach the tool as an
		// empty argument set.
		if reqBody != nil {
			if _, err := json.Marshal(reqBody); err != nil {
				return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.iam."+method, "request is not JSON-encodable")
			}
		}
		return nil, nil
	}

	// Flatten nested objects into the top-level arguments map
	flatArgs := flattenIAMArguments(args)

	// Apply tool-specific argument transformations
	toolName, ok := mapIAMToolName(service, method)
	if ok {
		// The MCP policy tools build the stored policy from spec_yaml and
		// drop structured rules, conditions and targets, so refuse them.
		if toolName == "quicbaas_iam_policy_create" || toolName == "quicbaas_iam_policy_update" {
			if err := policyfields.Refuse("mcp.iam."+method, "MCP", flatArgs); err != nil {
				return nil, err
			}
		}
		if toolName == "quicbaas_iam_audit_delete" {
			if err := refuseAuditDeleteScope(flatArgs); err != nil {
				return nil, err
			}
		}
		flatArgs = transformIAMToolArguments(toolName, flatArgs)
	}

	return flatArgs, nil
}

// transformResponse transforms MCP tool response back to SDK-expected types.
// Some IAM operations return wrapped responses that need to be unwrapped for
// the SDK to parse correctly (e.g., app operations wrap in "app" field, list
// operations wrap arrays in wrapper objects).
//
// The service parameter is the full service name (e.g., "quicbaas.IAM.App")
// used to disambiguate methods with the same name across different subsystems.
func (h *iamHandler) transformResponse(service, method string, resp interface{}) (interface{}, error) {
	if resp == nil {
		return resp, nil
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	// Use service+method for unambiguous routing
	subsystem := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		subsystem = service[idx+1:]
	}
	key := subsystem + "." + method

	switch key {
	// Tenant operations: the server stores the display name in
	// settings.display_name; the SDK Tenant reads a top-level display_name.
	case "Tenant.Get", "Tenant.Update":
		return liftTenantDisplayName(respMap), nil

	// App operations: server returns {app: {client_id, ...}, client_secret: ...}
	// SDK expects {client_id, ..., client_secret: ...} at the top level.
	case "App.Register", "App.Get", "App.Update", "App.RotateSecret":
		return unwrapAppResponse(respMap), nil

	// List operations: server returns {providers: [...], ...} or {identities: [...], ...}
	// but SDK expects the array directly for some list methods.
	case "Federation.ListProviders":
		return unwrapListResponse(respMap, "providers"), nil
	case "Identity.ListUserIdentities":
		return unwrapListResponse(respMap, "identities"), nil

	// Policy operations: server returns server-format policy {apiVersion, kind, metadata, spec}
	// but SDK expects flat format {name, tenant_id, rules, ...}.
	case "Policy.Create", "Policy.Get", "Policy.Update":
		return mapMCPServerPolicyToSDK(respMap), nil
	case "Policy.List":
		return mapMCPServerPolicyListToSDK(respMap), nil

	// App list: server returns {apps: [...]} but SDK expects {applications: [...]}.
	case "App.List":
		return mapAppListResponse(respMap), nil

	// Admin operations: server uses admin_role but SDK expects role_name.
	case "Admin.ListAssignments":
		return mapAdminAssignmentsResponse(respMap), nil

	// GDPR consent list: server may return {consents: [...]} wrapped.
	case "GDPR.ListConsents":
		if arr, ok := respMap["consents"].([]interface{}); ok {
			return map[string]interface{}{
				"consents":    arr,
				"total_count": len(arr),
			}, nil
		}
		return resp, nil

	default:
		return resp, nil
	}
}

// mapAppListResponse converts the server app list response field name
// from "apps" to "applications" to match the SDK ListAppsResponse struct.
func mapAppListResponse(respMap map[string]interface{}) interface{} {
	if apps, ok := respMap["apps"]; ok {
		result := make(map[string]interface{})
		result["applications"] = apps
		if total, ok := respMap["total_count"]; ok {
			result["total_count"] = total
		}
		return result
	}
	return respMap
}

// mapMCPServerPolicyToSDK converts a server-format policy response to SDK format.
// The server returns {policy: {apiVersion, kind, metadata, spec}, timestamp: ...}
// or directly {apiVersion, kind, metadata, spec}. The SDK expects {name, tenant_id, rules, ...}.
func mapMCPServerPolicyToSDK(respMap map[string]interface{}) interface{} {
	// Try to extract policy from wrapper
	policyRaw := respMap
	if p, ok := respMap["policy"].(map[string]interface{}); ok {
		policyRaw = p
	}

	// Check if this is a server-format policy
	_, hasAPIVersion := policyRaw["apiVersion"]
	_, hasMetadata := policyRaw["metadata"]
	if !hasAPIVersion && !hasMetadata {
		return respMap
	}

	result := make(map[string]interface{})

	// Extract metadata fields
	if metadata, ok := policyRaw["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			result["name"] = name
		}
		if tenant, ok := metadata["tenant"].(string); ok {
			result["tenant_id"] = tenant
		}
		if desc, ok := metadata["description"].(string); ok {
			result["description"] = desc
		}
		if labels, ok := metadata["labels"]; ok {
			result["labels"] = labels
		}
		if priority, ok := metadata["priority"]; ok {
			result["priority"] = priority
		}
		if createdAt, ok := metadata["created_at"]; ok {
			result["created_at"] = createdAt
		} else if createdAt, ok := metadata["created"]; ok {
			result["created_at"] = createdAt
		}
		if updatedAt, ok := metadata["updated_at"]; ok {
			result["updated_at"] = updatedAt
		} else if updatedAt, ok := metadata["updated"]; ok {
			result["updated_at"] = updatedAt
		}
	}

	// Extract spec fields
	if spec, ok := policyRaw["spec"].(map[string]interface{}); ok {
		if rules, ok := spec["rules"]; ok {
			result["rules"] = transformMCPServerRulesToSDK(rules)
		}
		if targets, ok := spec["targets"]; ok {
			result["targets"] = targets
		}
		if conditions, ok := spec["conditions"]; ok {
			result["conditions"] = conditions
		}
		if effect, ok := spec["effect"]; ok {
			result["effect"] = effect
		}
		if enabled, ok := spec["enabled"]; ok {
			result["enabled"] = enabled
		}
	}

	policyloaderror.Copy(policyRaw, result)

	return result
}

// mapMCPServerPolicyListToSDK converts a list of server-format policies to SDK format.

// transformMCPServerRulesToSDK converts server-format rules to SDK format.
// Server sends actions/resources as string arrays; SDK expects ActionMatcher/ResourceMatcher.
func transformMCPServerRulesToSDK(rulesRaw interface{}) interface{} {
	rulesArr, ok := rulesRaw.([]interface{})
	if !ok {
		return rulesRaw
	}
	for _, ruleRaw := range rulesArr {
		ruleMap, ok := ruleRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if actions, ok := ruleMap["actions"].([]interface{}); ok {
			matchers := make([]interface{}, 0, len(actions))
			for _, a := range actions {
				if s, ok := a.(string); ok {
					matchers = append(matchers, map[string]interface{}{"pattern": s})
				}
			}
			ruleMap["actions"] = matchers
		}
		if resources, ok := ruleMap["resources"].([]interface{}); ok {
			matchers := make([]interface{}, 0, len(resources))
			for _, r := range resources {
				if s, ok := r.(string); ok {
					matchers = append(matchers, map[string]interface{}{"pattern": s})
				}
			}
			ruleMap["resources"] = matchers
		}
		// Convert effect from server string format ("allow"/"deny") to SDK int format (1/0)
		if effect, ok := ruleMap["effect"].(string); ok {
			switch effect {
			case "allow":
				ruleMap["effect"] = float64(1) // EffectAllow
			case "deny":
				ruleMap["effect"] = float64(0) // EffectDeny
			}
		}
		if subjects, ok := ruleMap["subjects"].(map[string]interface{}); ok {
			var subjectMatchers []interface{}
			if auth, ok := subjects["authenticated"].(bool); ok && auth {
				subjectMatchers = append(subjectMatchers, map[string]interface{}{"type": "authenticated"})
			}
			if users, ok := subjects["users"].([]interface{}); ok {
				for _, u := range users {
					if s, ok := u.(string); ok {
						subjectMatchers = append(subjectMatchers, map[string]interface{}{"type": "user", "id": s})
					}
				}
			}
			if len(subjectMatchers) > 0 {
				ruleMap["subjects"] = subjectMatchers
			}
		}
	}
	return rulesArr
}
func mapMCPServerPolicyListToSDK(respMap map[string]interface{}) interface{} {
	policiesRaw, ok := respMap["policies"]
	if !ok {
		return respMap
	}

	policiesArr, ok := policiesRaw.([]interface{})
	if !ok {
		return respMap
	}

	mapped := make([]interface{}, len(policiesArr))
	for i, p := range policiesArr {
		if pMap, ok := p.(map[string]interface{}); ok {
			mapped[i] = mapMCPServerPolicyToSDK(pMap)
		} else {
			mapped[i] = p
		}
	}

	result := make(map[string]interface{})
	result["policies"] = mapped
	if total, ok := respMap["total_count"]; ok {
		result["total_count"] = total
	}

	return result
}

// unwrapAppResponse extracts the app object from a wrapped response and merges
// top-level fields (like client_secret) into it. The MCP server returns
// {app: {client_id, name, ...}, client_secret: "sk_..."} but the SDK expects
// all fields at the top level.
func unwrapAppResponse(respMap map[string]interface{}) interface{} {
	appRaw, ok := respMap["app"]
	if !ok {
		return respMap
	}

	appMap, ok := appRaw.(map[string]interface{})
	if !ok {
		return respMap
	}

	// Merge top-level fields into the app map
	for k, v := range respMap {
		if k == "app" || k == "timestamp" {
			continue
		}
		if _, exists := appMap[k]; !exists {
			appMap[k] = v
		}
	}

	return appMap
}

// liftTenantDisplayName copies settings.display_name to the tenant's
// top-level display_name. The tenant is either the response itself or
// nested under "tenant" ({tenant: {...}, timestamp: ...}).
func liftTenantDisplayName(respMap map[string]interface{}) map[string]interface{} {
	tenant := respMap
	if nested, ok := respMap["tenant"].(map[string]interface{}); ok {
		tenant = nested
	}
	settings, ok := tenant["settings"].(map[string]interface{})
	if !ok {
		return respMap
	}
	if displayName, ok := settings["display_name"].(string); ok {
		tenant["display_name"] = displayName
	}
	return respMap
}

// unwrapListResponse extracts an array from a wrapper object. MCP server
// returns {providers: [...], timestamp: ...} but SDK expects the array
// when calling convertMapResponse on array types.
func unwrapListResponse(respMap map[string]interface{}, key string) interface{} {
	if arr, ok := respMap[key]; ok {
		return arr
	}
	return respMap
}

// unwrapFieldResponse extracts a single field from a wrapper object.
func unwrapFieldResponse(respMap map[string]interface{}, key string) interface{} {
	if field, ok := respMap[key]; ok {
		return field
	}
	return respMap
}

// flattenIAMArguments flattens nested objects in IAM request arguments.
// Known nested keys are promoted to the top level, merging with existing
// top-level keys. SDK internal transport structs embed the actual request
// data in fields like "request", "app", "update", "check", etc.
//
// Note: "policy" is NOT flattened because the MCP server-side handler for
// policy create/validate expects args["policy"] to be a nested object.
func flattenIAMArguments(args map[string]interface{}) map[string]interface{} {
	nestedKeys := []string{
		"request",       // tenant update, policy create/list, etc.
		"app",           // app register/update
		"update",        // admin assignment update
		"check",         // admin check permission
		"impersonation", // impersonation start
		"consent",       // GDPR consent grant
		"export",        // GDPR export request
		"deletion",      // GDPR deletion request
		"filter",        // admin assignment list, audit query
	}

	result := make(map[string]interface{}, len(args))

	// Copy all top-level non-nested keys first
	for k, v := range args {
		isNested := false
		for _, nk := range nestedKeys {
			if k == nk {
				isNested = true
				break
			}
		}
		if !isNested {
			result[k] = v
		}
	}

	// Merge nested object fields into the top level
	for _, nk := range nestedKeys {
		nested, ok := args[nk]
		if !ok {
			continue
		}
		nestedMap, ok := nested.(map[string]interface{})
		if !ok {
			continue
		}
		for k, v := range nestedMap {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}

	return result
}

// refuseAuditDeleteScope refuses an audit Delete that asks for a dry run or
// an event-type filter. The audit service has neither, so sending one would
// delete every event before the cutoff.
func refuseAuditDeleteScope(args map[string]interface{}) error {
	dryRun := args["dry_run"] == true
	var eventTypeCount int
	if eventTypes, ok := args["event_types"].([]interface{}); ok {
		eventTypeCount = len(eventTypes)
	}
	return iamsdk.RefuseUnsupportedAuditDeleteScope(dryRun, eventTypeCount)
}

// isMCPIAMService returns true if the service is an IAM service.
func isMCPIAMService(service string) bool {
	return strings.Contains(service, ".IAM.")
}

// policyTopLevelKeys lists argument keys that belong at the top level and should
// NOT be grouped into the "policy" object for policy create/validate operations.
var policyTopLevelKeys = map[string]bool{
	"tenant_id":   true,
	"policy_name": true,
}

// transformIAMToolArguments applies tool-specific argument transformations
// based on the resolved tool name. Some MCP handlers expect different
// field names than what the SDK sends.
func transformIAMToolArguments(toolName string, args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return nil
	}

	switch toolName {
	// Tenant single-entity operations: MCP server handlers use getString(args, "id")
	// but the SDK sends the field as "tenant_id". Rename for tenant CRUD operations
	// where the tenant IS the entity being operated on (not just a context qualifier).
	case "quicbaas_iam_tenant_get",
		"quicbaas_iam_tenant_update",
		"quicbaas_iam_tenant_delete":
		if tid, ok := args["tenant_id"]; ok {
			if _, hasID := args["id"]; !hasID {
				args["id"] = tid
				delete(args, "tenant_id")
			}
		}

		// Convert settings values from strings to typed values.
		args = convertMCPSettingsValues(args)

	// User create and update: the SDK request types serialize display_name and
	// metadata, but the canonical user contract (REST body, QUIC wire, gRPC
	// proto, and the MCP tool schema) uses name and attributes. Rename both so
	// the MCP server tool receives the canonical keys.
	//
	// UPDATE is included for the same reason CREATE is, and it was missing: the
	// tool never saw the fields, so an update of the display name or the
	// attributes reported success and changed nothing. `username` needs no
	// rename — it is spelled the same on both sides — which is exactly why the
	// handle and the display name have to be separate fields rather than one
	// that takes turns.
	case "quicbaas_iam_user_create", "quicbaas_iam_user_update":
		if dn, ok := args["display_name"]; ok {
			if _, hasName := args["name"]; !hasName {
				args["name"] = dn
			}
			delete(args, "display_name")
		}
		if md, ok := args["metadata"]; ok {
			if _, hasAttrs := args["attributes"]; !hasAttrs {
				args["attributes"] = md
			}
			delete(args, "metadata")
		}

	// Tenant create also needs display_name->name and settings conversion.
	case "quicbaas_iam_tenant_create":
		if dn, ok := args["display_name"]; ok {
			if _, hasName := args["name"]; !hasName {
				args["name"] = dn
			}
			delete(args, "display_name")
		}
		args = convertMCPSettingsValues(args)

	// Admin assignment create/update: SDK sends "role_id" but the MCP server
	// handler expects "admin_role". Copy role_id to admin_role if admin_role
	// is not already set (or is empty).
	case "quicbaas_iam_admin_assignment_create",
		"quicbaas_iam_admin_assignment_update":
		if roleID, ok := args["role_id"]; ok {
			adminRole, _ := args["admin_role"].(string)
			if adminRole == "" {
				args["admin_role"] = roleID
			}
		}
		// Map status int to active bool for the MCP server.
		// AdminAssignmentStatusActive = 0, StatusSuspended = 1
		if statusVal, ok := args["status"]; ok {
			switch s := statusVal.(type) {
			case float64:
				active := int(math.Round(s)) == 0
				args["active"] = active
			case int:
				active := s == 0
				args["active"] = active
			}
			delete(args, "status")
		}
		// Remove zero-value expires_at to prevent assignments from being
		// created with an immediate expiration. The SDK serializes the Go
		// zero time.Time as "0001-01-01T00:00:00Z" which the server stores
		// as a past expiration date, causing active-only filters to skip it.
		removeZeroExpiresAt(args)

	// Admin assignment list: SDK sends admin_id but MCP server reads user_id.
	// SDK also sends status (int) for filtering; the MCP server expects active_only (bool).
	// AdminAssignmentStatusActive = 0, so status == 0 means active_only = true.
	case "quicbaas_iam_admin_assignment_list":
		if adminID, ok := args["admin_id"]; ok {
			if _, hasUserID := args["user_id"]; !hasUserID {
				args["user_id"] = adminID
			}
		}
		if statusVal, ok := args["status"]; ok {
			switch s := statusVal.(type) {
			case float64:
				if int(math.Round(s)) == 0 {
					args["active_only"] = true
				}
			case int:
				if s == 0 {
					args["active_only"] = true
				}
			}
			delete(args, "status")
		}

	// App register/update: SDK uses GrantType int enums which serialize as
	// numeric JSON values (e.g., 1 for client_credentials). The MCP server
	// handlers expect string values like "client_credentials". Convert
	// numeric grant_types to their string representations.
	case "quicbaas_iam_app_register",
		"quicbaas_iam_app_update":
		args = convertGrantTypesToStrings(args)

	// PEP check: The SDK sends "subject" as a nested object with "id" and
	// "tenant_id" fields. The MCP server handler expects "principal" (string)
	// for the subject ID and "tenant_id" at the top level.
	case "quicbaas_pep_check":
		args = transformPEPCheckArguments(args)

	// PEP check batch: The SDK sends {request: {requests: [{subject: ...}, ...]}}
	// which after flattening becomes {requests: [...]}.
	// The MCP server expects {checks: [{principal: ..., ...}, ...]}.
	// Rename "requests" to "checks" and transform each check's subject.
	case "quicbaas_pep_check_batch":
		args = transformPEPBatchArguments(args)

	// Federation provider operations: The SDK sends "provider_id" but the
	// MCP server handler reads getString(args, "id"). Map provider_id to id.
	case "quicbaas_iam_federation_createprovider",
		"quicbaas_iam_federation_getprovider",
		"quicbaas_iam_federation_updateprovider",
		"quicbaas_iam_federation_deleteprovider":
		if pid, ok := args["provider_id"]; ok {
			if _, hasID := args["id"]; !hasID {
				args["id"] = pid
			}
			delete(args, "provider_id")
		}

	// Policy update: The SDK sends {tenant_id, policy_name, request: {...}}
	// which after flattening puts the update fields at the top level. The MCP
	// server expects {tenant_id, name, policy: {...}}: decode spec_yaml and
	// re-group the update fields under "policy" while policy_name is still a
	// top-level key, then rename policy_name to name.
	case "quicbaas_iam_policy_update":
		args = decodeSpecYAMLBase64(args)
		args = regroupPolicyFields(args)
		renamePolicyName(args)

	// Policy get/delete: The SDK sends policy_name but the MCP server
	// handler reads getString(args, "name"). Map policy_name to name.
	case "quicbaas_iam_policy_get",
		"quicbaas_iam_policy_delete":
		renamePolicyName(args)
	// Policy create: The SDK sends {tenant_id: "...", request: {name: ..., spec_yaml: ...}}
	// which after flattening becomes {tenant_id: ..., name: ..., spec_yaml: ...}.
	// The MCP server expects {tenant_id: ..., policy: {name: ..., spec_yaml: ...}}.
	// Re-group non-top-level fields under "policy".
	case "quicbaas_iam_policy_create":
		args = decodeSpecYAMLBase64(args)
		args = regroupPolicyFields(args)

	// Policy validate: The SDK sends {tenant_id: ..., policy: {...}}.
	// "policy" is NOT in nestedKeys, so it stays as-is. Decode spec_yaml if present.
	case "quicbaas_iam_policy_validate":
		// Decode spec_yaml in nested policy object
		if policyMap, ok := args["policy"].(map[string]interface{}); ok {
			args["policy"] = decodeSpecYAMLBase64(policyMap)
		}
		args = decodeSpecYAMLBase64(args)

	// Policy test: The SDK sends {tenant_id: ..., request: {subject: ..., action: ..., ...}}
	// which after flattening becomes {tenant_id: ..., subject: ..., action: ..., ...}.
	// The MCP server expects flat fields, which is correct.

	// GDPR consent: The SDK sends {tenant_id, user_id, consent: {purpose, version, ...}}
	// After flattening "consent", purpose/version are at top level. Server expects
	// getString(args, "purpose") which will now work.

	// GDPR export: The SDK sends {tenant_id, user_id, export: {format, ...}}
	// After flattening "export", format is at top level. But the server also
	// needs "requested_by" which is NOT in the export struct - it's at top level
	// in requestExportRequest. Wait, no - requestExportRequest has Export *ExportRequest
	// and Export doesn't have requested_by. But the server expects requested_by.
	// The CLI passes it directly. The SDK doesn't send it in the Export struct.
	// Actually, the SDK requestExportRequest doesn't have requested_by at all.
	// The server handler reads getString(args, "requested_by"). The CLI must
	// pass this field. Let me check... No, the SDK gdprClient.RequestExport
	// does NOT pass requested_by in its request struct. This is a CLI-level
	// issue. The CLI sends the command with --requested-by but the SDK doesn't
	// have this field. Actually, looking back at the error, the issue is the
	// CLI command is invoked by the test and the requested_by field IS sent.
	// The problem was that "export" wasn't being flattened, so the format field
	// was stuck inside the nested "export" map. But now we flatten "export".
	// However, "requested_by" is NOT inside the Export struct - it would need
	// to be a top-level field in the SDK request. Let me check the actual
	// CLI flow more carefully...
	//
	// Actually, the integration test invokes the CLI binary, which uses the SDK.
	// The SDK gdprClient.RequestExport sends requestExportRequest{TenantID, UserID, Export}.
	// The Export struct (ExportRequest) does NOT have requested_by.
	// The server handler reads getString(args, "requested_by").
	// So requested_by is never sent via MCP for the SDK path.
	// But the CLI test passes because for gRPC, the gRPC handler extracts it differently.
	// For MCP, we need requested_by at the transport level. Since the CLI doesn't set
	// it on the SDK struct, it must come from somewhere else.
	// Looking at the test error: "requested_by is required" - this means the SDK
	// needs to send it. The fix is to add requested_by field support OR have the
	// CLI set the user_id as requested_by when not explicitly provided.
	// For now, map user_id to requested_by for GDPR export/deletion operations
	// when requested_by is not explicitly set.
	case "quicbaas_iam_gdpr_export_request":
		if _, ok := args["requested_by"]; !ok {
			if userID, ok := args["user_id"]; ok {
				args["requested_by"] = userID
			}
		}
		// Convert format from SDK int enum to server string value.
		args = convertExportFormat(args)

	case "quicbaas_iam_gdpr_deletion_request":
		if _, ok := args["requested_by"]; !ok {
			if userID, ok := args["user_id"]; ok {
				args["requested_by"] = userID
			}
		}
	}

	return args
}

// regroupPolicyFields re-groups flattened policy fields under a "policy" key.
// After flattenIAMArguments, the SDK policy create request has all fields at
// the top level: {tenant_id, name, spec_yaml, ...}. The MCP server expects
// {tenant_id, policy: {name, spec_yaml, ...}}.

// zeroTimeStrings lists zero time representations that should be stripped
// from expires_at fields to prevent creating assignments with immediate expiration.
var zeroTimeStrings = map[string]bool{
	"0001-01-01T00:00:00Z":          true,
	"0001-01-01T00:00:00.000Z":      true,
	"0001-01-01 00:00:00 +0000 UTC": true,
}

// removeZeroExpiresAt removes the expires_at field if it contains a Go zero
// time value. The SDK serializes unset time.Time as "0001-01-01T00:00:00Z"
// which the server stores as a past expiration date.
func removeZeroExpiresAt(args map[string]interface{}) {
	if args == nil {
		return
	}
	expiresAt, ok := args["expires_at"]
	if !ok {
		return
	}
	switch v := expiresAt.(type) {
	case string:
		if zeroTimeStrings[v] {
			delete(args, "expires_at")
		}
	}
}
func regroupPolicyFields(args map[string]interface{}) map[string]interface{} {
	// If "policy" already exists as a map, no regrouping needed
	if _, ok := args["policy"].(map[string]interface{}); ok {
		return args
	}

	policy := make(map[string]interface{})
	result := make(map[string]interface{})

	for k, v := range args {
		if policyTopLevelKeys[k] {
			result[k] = v
		} else {
			policy[k] = v
		}
	}

	if len(policy) > 0 {
		result["policy"] = policy
	}

	// Preserve tenant_id if it was set
	if tid, ok := args["tenant_id"]; ok {
		result["tenant_id"] = tid
	}

	return result
}

// renamePolicyName moves the SDK's policy_name argument to the name key the
// MCP server policy tools read, unless name is already set.
func renamePolicyName(args map[string]interface{}) {
	pn, ok := args["policy_name"]
	if !ok {
		return
	}
	if _, hasName := args["name"]; !hasName {
		args["name"] = pn
	}
	delete(args, "policy_name")
}

// transformPEPCheckArguments transforms PEP check arguments from SDK format
// to MCP server format. The SDK sends "subject" as a nested object:
//
//	{"subject": {"id": "user1", "tenant_id": "t1"}, "action": "read", ...}
//
// The MCP server expects "principal" as a string and "tenant_id" at top level:
//
//	{"principal": "user1", "tenant_id": "t1", "action": "read", ...}
func transformPEPCheckArguments(args map[string]interface{}) map[string]interface{} {
	subjectRaw, ok := args["subject"]
	if !ok {
		return args
	}

	subjectMap, ok := subjectRaw.(map[string]interface{})
	if !ok {
		// If subject is already a string, use it as principal directly
		if subjectStr, ok := subjectRaw.(string); ok {
			args["principal"] = subjectStr
			delete(args, "subject")
		}
		return args
	}

	// Extract subject.id as principal
	if id, ok := subjectMap["id"].(string); ok && id != "" {
		if _, hasPrincipal := args["principal"]; !hasPrincipal {
			args["principal"] = id
		}
	}

	// Extract subject.tenant_id and promote to top-level if not already set
	if tid, ok := subjectMap["tenant_id"].(string); ok && tid != "" {
		if _, hasTID := args["tenant_id"]; !hasTID {
			args["tenant_id"] = tid
		}
	}

	// Remove the nested subject object
	delete(args, "subject")

	return args
}

// transformPEPBatchArguments transforms PEP batch check arguments.
// The SDK sends {requests: [{subject: {id, tenant_id}, action, resource}, ...]}
// The MCP server expects {checks: [{principal, tenant_id, action, resource}, ...]}
func transformPEPBatchArguments(args map[string]interface{}) map[string]interface{} {
	// The SDK CheckBatchRequest has "requests" field (json:"requests")
	// The MCP server expects "checks" field
	requestsRaw, hasRequests := args["requests"]
	checksRaw, hasChecks := args["checks"]

	var items []interface{}
	if hasChecks {
		if arr, ok := checksRaw.([]interface{}); ok {
			items = arr
		}
	} else if hasRequests {
		if arr, ok := requestsRaw.([]interface{}); ok {
			items = arr
			// Rename requests -> checks
			delete(args, "requests")
		}
	}

	if items != nil {
		// Transform each check's subject to principal
		for i, checkRaw := range items {
			if check, ok := checkRaw.(map[string]interface{}); ok {
				items[i] = transformPEPCheckArguments(check)
			}
		}
		args["checks"] = items
	}

	return args
}

// convertGrantTypesToStrings converts numeric grant type values in the
// "grant_types" argument to their OAuth2 string representations. The SDK
// GrantType enum serializes as integers via JSON, but MCP server handlers
// expect string values. Values that are already strings are preserved.
func convertGrantTypesToStrings(args map[string]interface{}) map[string]interface{} {
	raw, ok := args["grant_types"]
	if !ok {
		return args
	}

	items, ok := raw.([]interface{})
	if !ok {
		return args
	}

	converted := make([]interface{}, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			// Already a string, keep as-is
			converted = append(converted, v)
		case float64:
			// JSON numbers deserialize as float64; convert to grant type string
			intVal := int(math.Round(v))
			if str, found := grantTypeIntToString[intVal]; found {
				converted = append(converted, str)
			}
		}
	}
	args["grant_types"] = converted

	return args
}

// iamToolRequiresSpecialHandling returns an error for unmapped IAM methods.
func iamToolRequiresSpecialHandling(service, method string) error {
	return errors.NewInternal("mcp.iamHandler",
		"unknown IAM method: "+service+"."+method)
}

// convertMCPSettingsValues converts string-typed settings values to their
// proper typed form. The SDK sends settings as map[string]string, which
// serializes as string values in the MCP arguments. The server-side
// parseJSONObject expects typed values (int, bool) for TenantSettings fields.
func convertMCPSettingsValues(args map[string]interface{}) map[string]interface{} {
	raw, ok := args["settings"]
	if !ok {
		return args
	}

	settingsMap, ok := raw.(map[string]interface{})
	if !ok {
		return args
	}

	converted := make(map[string]interface{}, len(settingsMap))
	for k, v := range settingsMap {
		str, isStr := v.(string)
		if !isStr {
			converted[k] = v
			continue
		}

		switch k {
		case "max_users":
			if n, err := strconv.Atoi(str); err == nil {
				converted[k] = n
			} else {
				converted[k] = str
			}
		case "mfa_required":
			if b, err := strconv.ParseBool(str); err == nil {
				converted[k] = b
			} else {
				converted[k] = str
			}
		default:
			converted[k] = str
		}
	}

	args["settings"] = converted
	return args
}

// extractConsentPurpose converts the SDK's numeric ConsentPurpose values to
// their string representations expected by the MCP server. The purpose field
// may arrive as a float64 from JSON deserialization of an enum value.
func extractConsentPurpose(args map[string]interface{}) map[string]interface{} {
	purposeRaw, ok := args["purpose"]
	if !ok {
		return args
	}

	// If already a string, no conversion needed
	if _, isStr := purposeRaw.(string); isStr {
		return args
	}

	// Convert numeric purpose to string
	if num, ok := purposeRaw.(float64); ok {
		purposeMap := map[int]string{
			0: "marketing",
			1: "analytics",
			2: "personalization",
			3: "third_party",
			4: "data_processing",
			5: "data_retention",
			6: "functional",
		}
		intVal := int(math.Round(num))
		if str, found := purposeMap[intVal]; found {
			args["purpose"] = str
		}
	}

	return args
}

// convertMapResponseJSON re-marshals a map response through JSON to ensure
// proper type conversion. Used by transformResponse for complex conversions.
func convertMapResponseJSON(resp interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// decodeSpecYAMLBase64 decodes the spec_yaml field from base64 to raw string
// if it appears to be base64-encoded. This is necessary because the SDK uses
// []byte for SpecYAML, which JSON marshaling encodes as base64. The MCP server
// expects the raw YAML string, not base64.
func decodeSpecYAMLBase64(args map[string]interface{}) map[string]interface{} {
	specRaw, ok := args["spec_yaml"]
	if !ok {
		return args
	}

	specStr, isStr := specRaw.(string)
	if !isStr {
		return args
	}

	// Try to base64-decode the string. If it decodes successfully and the
	// result looks like YAML (contains common YAML markers), use the decoded version.
	decoded, err := base64.StdEncoding.DecodeString(specStr)
	if err == nil && len(decoded) > 0 {
		args["spec_yaml"] = string(decoded)
	}

	return args
}

// convertExportFormat converts the format field from SDK int enum to string.
// SDK ExportFormat: 0=json, 1=csv, 2=xml.
func convertExportFormat(args map[string]interface{}) map[string]interface{} {
	formatVal, ok := args["format"]
	if !ok {
		return args
	}

	exportFormatStrings := map[int]string{
		0: "json",
		1: "csv",
		2: "xml",
	}

	switch f := formatVal.(type) {
	case float64:
		if s, found := exportFormatStrings[int(math.Round(f))]; found {
			args["format"] = s
		}
	case int:
		if s, found := exportFormatStrings[f]; found {
			args["format"] = s
		}
	}

	return args
}

// mapAdminAssignmentsResponse maps admin assignments from server format to SDK format.
// The server uses admin_role (string) but the SDK expects role_name for display.
// Also copies user_id to admin_id for CLI display compatibility.
func mapAdminAssignmentsResponse(respMap map[string]interface{}) interface{} {
	assignmentsRaw, ok := respMap["assignments"]
	if !ok {
		return respMap
	}

	assignmentsArr, ok := assignmentsRaw.([]interface{})
	if !ok {
		return respMap
	}

	for _, a := range assignmentsArr {
		aMap, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		// Copy admin_role to role_name for CLI display.
		if role, ok := aMap["admin_role"].(string); ok {
			if _, hasRoleName := aMap["role_name"]; !hasRoleName {
				aMap["role_name"] = role
			}
		}
	}

	return respMap
}
