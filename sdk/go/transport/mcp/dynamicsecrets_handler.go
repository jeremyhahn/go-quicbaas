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

// dynamicsecrets_handler.go adapts Security().DynamicSecrets() to the eight
// quicbaas_dynamic_secrets_* MCP tools.
//
// Two conventions force a typed handler rather than the generic argument
// conversion: the tool schema is entirely string-typed, so durations travel as
// Go duration strings ("1h") instead of the nanosecond integers time.Duration
// marshals to, and the create-role generator config travels as a
// JSON-stringified object. The tool results are likewise loosely-typed maps
// that must be decoded back into the leaf client/dynamicsecrets types the SDK
// client type-asserts on.

package mcp

import (
	"encoding/json"
	"strings"
	"time"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// dynamicSecretsServiceName is the SDK service identifier for the dynamic
// secrets surface.
const dynamicSecretsServiceName = "quicbaas.DynamicSecrets"

// isMCPDynamicSecretsService reports whether the service name targets the
// dynamic secrets surface.
func isMCPDynamicSecretsService(service string) bool {
	return strings.HasPrefix(service, dynamicSecretsServiceName) || service == "DynamicSecrets"
}

// dynamicSecretsMethodTools maps SDK method names to the MCP tool the server
// registers (pkg/api/mcp/dynamic_secrets_tools.go). The generic
// service_method derivation would produce quicbaas_dynamicsecrets_*, which the
// server does not register, so the closed set is spelled out here.
var dynamicSecretsMethodTools = map[string]string{
	"CreateRole":         "quicbaas_dynamic_secrets_create_role",
	"GetRole":            "quicbaas_dynamic_secrets_get_role",
	"ListRoles":          "quicbaas_dynamic_secrets_list_roles",
	"DeleteRole":         "quicbaas_dynamic_secrets_delete_role",
	"GenerateCredential": "quicbaas_dynamic_secrets_generate",
	"RenewLease":         "quicbaas_dynamic_secrets_renew_lease",
	"RevokeLease":        "quicbaas_dynamic_secrets_revoke_lease",
	"ListLeases":         "quicbaas_dynamic_secrets_list_leases",
}

// dynamicSecretsToolName resolves the MCP tool name for an SDK method.
func dynamicSecretsToolName(method string) (string, bool) {
	name, ok := dynamicSecretsMethodTools[method]
	return name, ok
}

// newDynamicSecretsUnsupportedError reports a method the dynamic secrets tool
// set does not cover, naming the transport so the caller can retry elsewhere.
func newDynamicSecretsUnsupportedError(method string) error {
	return errors.NewUnimplemented("mcp.dynamicsecrets."+method,
		"method not exposed by the DynamicSecrets MCP tool set")
}

// dynamicSecretsHandler adapts the leaf dynamic secrets types to the MCP tool
// argument and result shapes. It carries no state, so the zero value is
// complete.
type dynamicSecretsHandler struct{}

// newDynamicSecretsHandler creates a dynamic secrets MCP handler.
func newDynamicSecretsHandler() *dynamicSecretsHandler { return &dynamicSecretsHandler{} }

// transformRequest converts an SDK dynamic secrets request into MCP tool
// arguments.
func (h *dynamicSecretsHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "CreateRole":
		return h.createRoleArgs(req)
	case "GetRole":
		return h.roleNameArgs("GetRole", req)
	case "ListRoles":
		return h.listRolesArgs(req)
	case "DeleteRole":
		return h.roleNameArgs("DeleteRole", req)
	case "GenerateCredential":
		return h.generateArgs(req)
	case "RenewLease":
		return h.renewArgs(req)
	case "RevokeLease":
		return h.revokeArgs(req)
	case "ListLeases":
		return h.listLeasesArgs(req)
	default:
		return nil, newDynamicSecretsUnsupportedError(method)
	}
}

// transformResponse converts an MCP tool result into the leaf dynamic secrets
// response type.
func (h *dynamicSecretsHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	m, err := asResultMap(result, "mcp.dynamicsecrets."+method)
	if err != nil {
		return nil, err
	}

	switch method {
	case "CreateRole":
		return &dsclient.CreateRoleResponse{
			Role:      dsRoleFromResult(nestedResultMap(m, "role")),
			CreatedAt: getResultTime(m, "created_at"),
		}, nil
	case "GetRole":
		return &dsclient.GetRoleResponse{Role: dsRoleFromResult(nestedResultMap(m, "role"))}, nil
	case "ListRoles":
		roles := make([]*dsclient.Role, 0)
		for _, entry := range resultMapSlice(m, "roles") {
			roles = append(roles, dsRoleFromResult(entry))
		}
		return &dsclient.ListRolesResponse{
			Roles: roles,
			Count: int(getResultInt64(m, "count")),
		}, nil
	case "DeleteRole":
		return &dsclient.DeleteRoleResponse{DeletedAt: getResultTime(m, "deleted_at")}, nil
	case "GenerateCredential":
		return &dsclient.GenerateCredentialResponse{
			Credential: dsCredentialFromResult(nestedResultMap(m, "credential")),
			Lease:      dsLeaseFromResult(nestedResultMap(m, "lease")),
			LeaseID:    getResultString(m, "lease_id"),
		}, nil
	case "RenewLease":
		return &dsclient.RenewLeaseResponse{
			Lease:     dsLeaseFromResult(nestedResultMap(m, "lease")),
			RenewedAt: getResultTime(m, "renewed_at"),
		}, nil
	case "RevokeLease":
		return &dsclient.RevokeLeaseResponse{RevokedAt: getResultTime(m, "revoked_at")}, nil
	case "ListLeases":
		leases := make([]*dsclient.Lease, 0)
		for _, entry := range resultMapSlice(m, "leases") {
			leases = append(leases, dsLeaseFromResult(entry))
		}
		return &dsclient.ListLeasesResponse{
			Leases: leases,
			Count:  int(getResultInt64(m, "count")),
		}, nil
	default:
		return nil, newDynamicSecretsUnsupportedError(method)
	}
}

// =============================================================================
// Request transforms
// =============================================================================

func (h *dynamicSecretsHandler) createRoleArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*dsclient.CreateRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.CreateRole",
			"invalid request type, expected *dynamicsecrets.CreateRoleRequest")
	}

	args := map[string]interface{}{
		"name":           r.Name,
		"generator_type": r.GeneratorType,
		"default_ttl":    r.DefaultTTL.String(),
		"max_ttl":        r.MaxTTL.String(),
	}
	if len(r.Config) > 0 {
		encoded, err := json.Marshal(r.Config)
		if err != nil {
			return nil, errors.NewInternal("mcp.dynamicsecrets.CreateRole",
				"failed to encode config: "+err.Error())
		}
		args["config"] = string(encoded)
	}
	return args, nil
}

// roleNameArgs builds the single-argument payload shared by GetRole and
// DeleteRole.
func (h *dynamicSecretsHandler) roleNameArgs(method string, req interface{}) (map[string]interface{}, error) {
	var name string
	switch r := req.(type) {
	case *dsclient.GetRoleRequest:
		if r != nil {
			name = r.Name
		}
	case *dsclient.DeleteRoleRequest:
		if r != nil {
			name = r.Name
		}
	default:
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets."+method,
			"invalid request type, expected *dynamicsecrets."+method+"Request")
	}
	if name == "" {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets."+method, "name is required")
	}
	return map[string]interface{}{"name": name}, nil
}

func (h *dynamicSecretsHandler) listRolesArgs(req interface{}) (map[string]interface{}, error) {
	args := map[string]interface{}{}
	if r, ok := req.(*dsclient.ListRolesRequest); ok && r != nil {
		putStringArg(args, "prefix", r.Prefix)
	}
	return args, nil
}

func (h *dynamicSecretsHandler) generateArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*dsclient.GenerateCredentialRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.GenerateCredential",
			"invalid request type, expected *dynamicsecrets.GenerateCredentialRequest")
	}
	if r.RoleName == "" {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.GenerateCredential",
			"role_name is required")
	}

	args := map[string]interface{}{"role_name": r.RoleName}
	if r.TTL > 0 {
		args["ttl"] = r.TTL.String()
	}
	return args, nil
}

func (h *dynamicSecretsHandler) renewArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*dsclient.RenewLeaseRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.RenewLease",
			"invalid request type, expected *dynamicsecrets.RenewLeaseRequest")
	}
	if r.LeaseID == "" {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.RenewLease", "lease_id is required")
	}

	args := map[string]interface{}{"lease_id": r.LeaseID}
	if r.Increment > 0 {
		args["increment"] = r.Increment.String()
	}
	return args, nil
}

func (h *dynamicSecretsHandler) revokeArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*dsclient.RevokeLeaseRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.RevokeLease",
			"invalid request type, expected *dynamicsecrets.RevokeLeaseRequest")
	}
	if r.LeaseID == "" {
		return nil, errors.NewInvalidInput("mcp.dynamicsecrets.RevokeLease", "lease_id is required")
	}
	return map[string]interface{}{"lease_id": r.LeaseID}, nil
}

func (h *dynamicSecretsHandler) listLeasesArgs(req interface{}) (map[string]interface{}, error) {
	args := map[string]interface{}{}
	if r, ok := req.(*dsclient.ListLeasesRequest); ok && r != nil {
		putStringArg(args, "prefix", r.Prefix)
	}
	return args, nil
}

// =============================================================================
// Result decoding
// =============================================================================

// nestedResultMap returns the object stored under key, or nil when absent.
func nestedResultMap(m map[string]interface{}, key string) map[string]interface{} {
	nested, ok := m[key].(map[string]interface{})
	if !ok {
		return nil
	}
	return nested
}

// resultMapSlice returns the array of objects stored under key, skipping any
// element that is not a JSON object.
func resultMapSlice(m map[string]interface{}, key string) []map[string]interface{} {
	raw, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		if obj, ok := entry.(map[string]interface{}); ok {
			out = append(out, obj)
		}
	}
	return out
}

// resultDuration parses a Go duration string field, yielding zero when the
// field is absent or malformed.
func resultDuration(m map[string]interface{}, key string) time.Duration {
	raw := getResultString(m, key)
	if raw == "" {
		return 0
	}
	d, err := parseDurationLoose(raw)
	if err != nil {
		return 0
	}
	return d
}

// dsRoleFromResult decodes a tool role object into the leaf SDK role type.
func dsRoleFromResult(m map[string]interface{}) *dsclient.Role {
	if m == nil {
		return nil
	}
	return &dsclient.Role{
		Name:          getResultString(m, "name"),
		GeneratorType: getResultString(m, "generator_type"),
		Config:        getResultStringMap(m, "config"),
		DefaultTTL:    resultDuration(m, "default_ttl"),
		MaxTTL:        resultDuration(m, "max_ttl"),
	}
}

// dsCredentialFromResult decodes a tool credential object into the leaf SDK
// credential type.
func dsCredentialFromResult(m map[string]interface{}) *dsclient.Credential {
	if m == nil {
		return nil
	}
	return &dsclient.Credential{
		CredentialID: getResultString(m, "credential_id"),
		Type:         getResultString(m, "type"),
		Data:         getResultStringMap(m, "data"),
		LeaseID:      getResultString(m, "lease_id"),
		CreatedAt:    getResultTime(m, "created_at"),
		ExpiresAt:    getResultTime(m, "expires_at"),
	}
}

// dsLeaseFromResult decodes a tool lease object into the leaf SDK lease type.
func dsLeaseFromResult(m map[string]interface{}) *dsclient.Lease {
	if m == nil {
		return nil
	}
	return &dsclient.Lease{
		LeaseID:    getResultString(m, "lease_id"),
		SecretPath: getResultString(m, "secret_path"),
		ExpiresAt:  getResultTime(m, "expires_at"),
		TTL:        resultDuration(m, "ttl"),
		RenewCount: int(getResultInt64(m, "renew_count")),
		Renewable:  getResultBool(m, "renewable"),
		Metadata:   getResultStringMap(m, "metadata"),
	}
}
