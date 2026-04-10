//go:build mcp

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

// Package mcp implements Dynamic Secrets MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerDynamicSecretsTools registers all Dynamic Secrets tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerDynamicSecretsTools() {
	dynamicSecretsTools := getAllDynamicSecretsTools()
	for _, tool := range dynamicSecretsTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeDynamicSecretsTool executes a Dynamic Secrets tool and returns the result.
func (r *ToolRegistry) executeDynamicSecretsTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	dynamicSecretsService := r.getDynamicSecretsService()
	if dynamicSecretsService == nil {
		return nil, api.NewServiceNotConfiguredError("dynamic secrets")
	}

	switch name {
	case "quicbaas_dynamic_secrets_create_role":
		return r.handleDynamicSecretsCreateRole(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_get_role":
		return r.handleDynamicSecretsGetRole(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_list_roles":
		return r.handleDynamicSecretsListRoles(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_delete_role":
		return r.handleDynamicSecretsDeleteRole(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_generate":
		return r.handleDynamicSecretsGenerate(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_renew_lease":
		return r.handleDynamicSecretsRenewLease(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_revoke_lease":
		return r.handleDynamicSecretsRevokeLease(ctx, dynamicSecretsService, args)
	case "quicbaas_dynamic_secrets_list_leases":
		return r.handleDynamicSecretsListLeases(ctx, dynamicSecretsService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// getDynamicSecretsService retrieves the DynamicSecretsService from the service.
// Note: This needs to be implemented based on how the QuicBaaS service exposes DynamicSecretsService.
func (r *ToolRegistry) getDynamicSecretsService() service.DynamicSecretsService {
	// The DynamicSecretsService may need to be added to the QuicBaaS interface
	// For now, we check if it's available through type assertion or direct accessor
	if svc, ok := r.service.(interface {
		DynamicSecrets() service.DynamicSecretsService
	}); ok {
		return svc.DynamicSecrets()
	}
	return nil
}

// handleDynamicSecretsCreateRole handles the quicbaas_dynamic_secrets_create_role tool.
func (r *ToolRegistry) handleDynamicSecretsCreateRole(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	generatorType := getString(args, "generator_type")
	if generatorType == "" {
		return nil, api.NewRequiredFieldError("generator_type")
	}

	defaultTTLStr := getString(args, "default_ttl")
	if defaultTTLStr == "" {
		return nil, api.NewRequiredFieldError("default_ttl")
	}
	defaultTTL, err := time.ParseDuration(defaultTTLStr)
	if err != nil {
		return nil, api.NewInvalidFormatError("default_ttl", err)
	}

	maxTTLStr := getString(args, "max_ttl")
	if maxTTLStr == "" {
		return nil, api.NewRequiredFieldError("max_ttl")
	}
	maxTTL, err := time.ParseDuration(maxTTLStr)
	if err != nil {
		return nil, api.NewInvalidFormatError("max_ttl", err)
	}

	req := &service.CreateRoleRequest{
		Name:          name,
		GeneratorType: generatorType,
		DefaultTTL:    defaultTTL,
		MaxTTL:        maxTTL,
	}

	// Parse optional config
	if configJSON := getString(args, "config"); configJSON != "" {
		var config map[string]string
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, api.NewInvalidFormatError("config JSON", err)
		}
		req.Config = config
	}

	resp, err := svc.CreateRole(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("create role", err)
	}

	result := map[string]interface{}{
		"created_at": resp.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Role != nil {
		result["role"] = map[string]interface{}{
			"name":           resp.Role.Name,
			"generator_type": resp.Role.GeneratorType,
			"default_ttl":    resp.Role.DefaultTTL.String(),
			"max_ttl":        resp.Role.MaxTTL.String(),
		}
		if len(resp.Role.Config) > 0 {
			if roleMap, ok := result["role"].(map[string]interface{}); ok {
				roleMap["config"] = resp.Role.Config
			}
		}
	}

	return result, nil
}

// handleDynamicSecretsGetRole handles the quicbaas_dynamic_secrets_get_role tool.
func (r *ToolRegistry) handleDynamicSecretsGetRole(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.GetRoleRequest{
		Name: name,
	}

	resp, err := svc.GetRole(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get role", err)
	}

	result := map[string]interface{}{}
	if resp.Role != nil {
		result["role"] = map[string]interface{}{
			"name":           resp.Role.Name,
			"generator_type": resp.Role.GeneratorType,
			"default_ttl":    resp.Role.DefaultTTL.String(),
			"max_ttl":        resp.Role.MaxTTL.String(),
		}
		if len(resp.Role.Config) > 0 {
			if roleMap, ok := result["role"].(map[string]interface{}); ok {
				roleMap["config"] = resp.Role.Config
			}
		}
	}

	return result, nil
}

// handleDynamicSecretsListRoles handles the quicbaas_dynamic_secrets_list_roles tool.
func (r *ToolRegistry) handleDynamicSecretsListRoles(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	req := &service.ListRolesRequest{
		Prefix: getString(args, "prefix"),
	}

	resp, err := svc.ListRoles(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("list roles", err)
	}

	roles := make([]map[string]interface{}, 0, len(resp.Roles))
	for _, role := range resp.Roles {
		roleMap := map[string]interface{}{
			"name":           role.Name,
			"generator_type": role.GeneratorType,
			"default_ttl":    role.DefaultTTL.String(),
			"max_ttl":        role.MaxTTL.String(),
		}
		if len(role.Config) > 0 {
			roleMap["config"] = role.Config
		}
		roles = append(roles, roleMap)
	}

	return map[string]interface{}{
		"roles": roles,
		"count": resp.Count,
	}, nil
}

// handleDynamicSecretsDeleteRole handles the quicbaas_dynamic_secrets_delete_role tool.
func (r *ToolRegistry) handleDynamicSecretsDeleteRole(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.DeleteRoleRequest{
		Name: name,
	}

	resp, err := svc.DeleteRole(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("delete role", err)
	}

	return map[string]interface{}{
		"deleted_at": resp.DeletedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleDynamicSecretsGenerate handles the quicbaas_dynamic_secrets_generate tool.
func (r *ToolRegistry) handleDynamicSecretsGenerate(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	roleName := getString(args, "role_name")
	if roleName == "" {
		return nil, api.NewRequiredFieldError("role_name")
	}

	req := &service.GenerateCredentialRequest{
		RoleName: roleName,
	}

	// Parse optional TTL
	if ttlStr := getString(args, "ttl"); ttlStr != "" {
		ttl, err := time.ParseDuration(ttlStr)
		if err != nil {
			return nil, api.NewInvalidFormatError("ttl", err)
		}
		req.TTL = ttl
	}

	resp, err := svc.GenerateCredential(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("generate credential", err)
	}

	result := map[string]interface{}{
		"lease_id": resp.LeaseID,
	}

	if resp.Credential != nil {
		credMap := map[string]interface{}{
			"credential_id": resp.Credential.CredentialID,
			"type":          resp.Credential.Type,
			"lease_id":      resp.Credential.LeaseID,
			"created_at":    resp.Credential.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"expires_at":    resp.Credential.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		// Include credential data (contains username, password, etc.)
		if len(resp.Credential.Data) > 0 {
			credMap["data"] = resp.Credential.Data
		}
		result["credential"] = credMap
	}

	if resp.Lease != nil {
		result["lease"] = map[string]interface{}{
			"lease_id":    resp.Lease.LeaseID,
			"secret_path": resp.Lease.SecretPath,
			"ttl":         resp.Lease.TTL.String(),
			"expires_at":  resp.Lease.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			"renewable":   resp.Lease.Renewable,
			"renew_count": resp.Lease.RenewCount,
		}
	}

	return result, nil
}

// handleDynamicSecretsRenewLease handles the quicbaas_dynamic_secrets_renew_lease tool.
func (r *ToolRegistry) handleDynamicSecretsRenewLease(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	leaseID := getString(args, "lease_id")
	if leaseID == "" {
		return nil, api.NewRequiredFieldError("lease_id")
	}

	req := &service.RenewLeaseRequest{
		LeaseID: leaseID,
	}

	// Parse optional increment
	if incrementStr := getString(args, "increment"); incrementStr != "" {
		increment, err := time.ParseDuration(incrementStr)
		if err != nil {
			return nil, api.NewInvalidFormatError("increment", err)
		}
		req.Increment = increment
	}

	resp, err := svc.RenewLease(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("renew lease", err)
	}

	result := map[string]interface{}{
		"renewed_at": resp.RenewedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Lease != nil {
		result["lease"] = map[string]interface{}{
			"lease_id":    resp.Lease.LeaseID,
			"secret_path": resp.Lease.SecretPath,
			"ttl":         resp.Lease.TTL.String(),
			"expires_at":  resp.Lease.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			"renewable":   resp.Lease.Renewable,
			"renew_count": resp.Lease.RenewCount,
		}
	}

	return result, nil
}

// handleDynamicSecretsRevokeLease handles the quicbaas_dynamic_secrets_revoke_lease tool.
func (r *ToolRegistry) handleDynamicSecretsRevokeLease(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	leaseID := getString(args, "lease_id")
	if leaseID == "" {
		return nil, api.NewRequiredFieldError("lease_id")
	}

	req := &service.RevokeLeaseRequest{
		LeaseID: leaseID,
	}

	resp, err := svc.RevokeLease(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("revoke lease", err)
	}

	return map[string]interface{}{
		"revoked_at": resp.RevokedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleDynamicSecretsListLeases handles the quicbaas_dynamic_secrets_list_leases tool.
func (r *ToolRegistry) handleDynamicSecretsListLeases(ctx context.Context, svc service.DynamicSecretsService, args map[string]interface{}) (interface{}, error) {
	req := &service.ListLeasesRequest{
		Prefix: getString(args, "prefix"),
	}

	resp, err := svc.ListLeases(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("list leases", err)
	}

	leases := make([]map[string]interface{}, 0, len(resp.Leases))
	for _, lease := range resp.Leases {
		leaseMap := map[string]interface{}{
			"lease_id":    lease.LeaseID,
			"secret_path": lease.SecretPath,
			"ttl":         lease.TTL.String(),
			"expires_at":  lease.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			"renewable":   lease.Renewable,
			"renew_count": lease.RenewCount,
		}
		if lease.Metadata != nil {
			leaseMap["metadata"] = lease.Metadata
		}
		leases = append(leases, leaseMap)
	}

	return map[string]interface{}{
		"leases": leases,
		"count":  resp.Count,
	}, nil
}

// isDynamicSecretsTool returns true if the tool name is a Dynamic Secrets tool.
func isDynamicSecretsTool(name string) bool {
	switch name {
	case "quicbaas_dynamic_secrets_create_role",
		"quicbaas_dynamic_secrets_get_role",
		"quicbaas_dynamic_secrets_list_roles",
		"quicbaas_dynamic_secrets_delete_role",
		"quicbaas_dynamic_secrets_generate",
		"quicbaas_dynamic_secrets_renew_lease",
		"quicbaas_dynamic_secrets_revoke_lease",
		"quicbaas_dynamic_secrets_list_leases":
		return true
	default:
		return false
	}
}
