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

package mcp

import (
	"context"
	"encoding/base64"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

func (r *ToolRegistry) registerSecretsTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_store",
		Description: "Store a secret with optional metadata and automatic encryption",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier (optional for new secrets)",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable secret name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Secret description",
				},
				"data": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded secret data",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Version-specific metadata",
				},
				"tags": map[string]interface{}{
					"type":        "object",
					"description": "User-defined tags",
				},
				"expires_at": map[string]interface{}{
					"type":        "string",
					"description": "ISO 8601 timestamp for version expiration",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for tenant-specific encryption key",
				},
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Custom key identifier for encryption",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Encryption algorithm (e.g., AES-256-GCM, AES-128-GCM, CHACHA20-POLY1305)",
				},
			},
			"required": []string{"name", "data"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_get",
		Description: "Retrieve the latest version of a secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific version to retrieve (optional, defaults to latest)",
				},
			},
			"required": []string{"secret_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_list",
		Description: "List all secrets accessible to the authenticated user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor",
				},
				"filter": map[string]interface{}{
					"type":        "object",
					"description": "Filter by tags, owner, etc.",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_delete",
		Description: "Delete a secret (soft delete by default)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"permanently_delete": map[string]interface{}{
					"type":        "boolean",
					"description": "Bypass soft-delete and permanently remove",
					"default":     false,
				},
			},
			"required": []string{"secret_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_versions",
		Description: "List all versions of a secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor",
				},
			},
			"required": []string{"secret_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_rollback",
		Description: "Roll back to a previous version of a secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"target_version": map[string]interface{}{
					"type":        "integer",
					"description": "Version to roll back to",
				},
			},
			"required": []string{"secret_id", "target_version"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_rotate",
		Description: "Manually rotate a secret to a new value",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"new_data": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded new secret data",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Version-specific metadata",
				},
			},
			"required": []string{"secret_id", "new_data"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_set_rotation_policy",
		Description: "Set or update the automatic rotation policy for a secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"policy": map[string]interface{}{
					"type":        "object",
					"description": "Rotation policy configuration",
					"properties": map[string]interface{}{
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Enable automatic rotation",
						},
						"interval": map[string]interface{}{
							"type":        "string",
							"description": "Rotation interval (e.g., '24h', '7d')",
						},
						"retain_versions": map[string]interface{}{
							"type":        "integer",
							"description": "Number of old versions to keep",
						},
						"rotation_lambda": map[string]interface{}{
							"type":        "string",
							"description": "Function ARN to call during rotation",
						},
					},
				},
			},
			"required": []string{"secret_id", "policy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_get_rotation_policy",
		Description: "Retrieve the current rotation policy for a secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
			},
			"required": []string{"secret_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_get_prefix",
		Description: "Retrieve all secrets matching a path prefix for bulk operations",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Path prefix to match (e.g., 'apps/myapp/')",
				},
				"include_data": map[string]interface{}{
					"type":        "boolean",
					"description": "Include secret data in response (default true)",
					"default":     true,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of secrets to return",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor for fetching next page",
				},
			},
			"required": []string{"prefix"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_watch",
		Description: "Watch for real-time changes to secrets under a prefix",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Path prefix to watch (e.g., 'apps/myapp/')",
				},
				"start_version": map[string]interface{}{
					"type":        "integer",
					"description": "Start watching from this version (0 for current)",
					"default":     0,
				},
				"timeout_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum time to wait for changes in seconds (0 for indefinite)",
					"default":     30,
				},
			},
			"required": []string{"prefix"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_secrets_update_status",
		Description: "Update the status of a secret (e.g., enable, disable, deprecate)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret_id": map[string]interface{}{
					"type":        "string",
					"description": "Secret identifier",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "New status for the secret (active, inactive, deprecated)",
				},
			},
			"required": []string{"secret_id", "status"},
		},
	})
	r.registerSecretsEnsureTool()
}

func (r *ToolRegistry) executeSecretsStore(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	var metadata map[string]string
	if metaRaw, ok := args["metadata"].(map[string]interface{}); ok {
		metadata = make(map[string]string)
		for k, v := range metaRaw {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
		}
	}

	var tags map[string]string
	if tagsRaw, ok := args["tags"].(map[string]interface{}); ok {
		tags = make(map[string]string)
		for k, v := range tagsRaw {
			if str, ok := v.(string); ok {
				tags[k] = str
			}
		}
	}

	// Decode base64-encoded secret data
	dataB64 := getString(args, "data")
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("data", err)
	}

	req := &service.StoreSecretRequest{
		SecretID:    getString(args, "secret_id"),
		Name:        getString(args, "name"),
		Description: getString(args, "description"),
		Data:        data,
		Metadata:    metadata,
		Tags:        tags,
		TenantID:    getString(args, "tenant_id"),
		KeyID:       getString(args, "key_id"),
		Algorithm:   getString(args, "algorithm"),
	}

	resp, err := secretsSvc.StoreSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	req := &service.GetSecretRequest{
		SecretID: getString(args, "secret_id"),
		Version:  getUint64(args, "version"),
	}

	resp, err := secretsSvc.GetSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	var filter map[string]string
	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		filter = make(map[string]string)
		for k, v := range filterRaw {
			if str, ok := v.(string); ok {
				filter[k] = str
			}
		}
	}

	ints := newIntArgReader(args)
	req := &service.ListSecretsRequest{
		Limit:  ints.Int("limit"),
		Cursor: getString(args, "cursor"),
		Filter: filter,
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}

	resp, err := secretsSvc.ListSecrets(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	req := &service.DeleteSecretRequest{
		SecretID:          getString(args, "secret_id"),
		PermanentlyDelete: getBool(args, "permanently_delete"),
	}

	resp, err := secretsSvc.DeleteSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsVersions(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	ints := newIntArgReader(args)
	req := &service.ListSecretVersionsRequest{
		SecretID: getString(args, "secret_id"),
		Limit:    ints.Int("limit"),
		Cursor:   getString(args, "cursor"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}

	resp, err := secretsSvc.ListSecretVersions(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsRollback(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	req := &service.RollbackSecretRequest{
		SecretID:      getString(args, "secret_id"),
		TargetVersion: getUint64(args, "target_version"),
	}

	resp, err := secretsSvc.RollbackSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsRotate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	var metadata map[string]string
	if metaRaw, ok := args["metadata"].(map[string]interface{}); ok {
		metadata = make(map[string]string)
		for k, v := range metaRaw {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
		}
	}

	// Decode base64-encoded new data
	newDataB64 := getString(args, "new_data")
	newData, err := base64.StdEncoding.DecodeString(newDataB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("new_data", err)
	}

	req := &service.RotateSecretRequest{
		SecretID: getString(args, "secret_id"),
		NewData:  newData,
		Metadata: metadata,
	}

	resp, err := secretsSvc.RotateSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsSetRotationPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	_, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	policy := &service.SecretRotationPolicy{}

	req := &service.SetRotationPolicyRequest{
		SecretID: getString(args, "secret_id"),
		Policy:   policy,
	}

	resp, err := secretsSvc.SetRotationPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsGetRotationPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	req := &service.GetRotationPolicyRequest{
		SecretID: getString(args, "secret_id"),
	}

	resp, err := secretsSvc.GetRotationPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsGetPrefix(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	includeData := true
	if v, ok := args["include_data"].(bool); ok {
		includeData = v
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.GetSecretsByPrefixRequest{
		Prefix:      getString(args, "prefix"),
		IncludeData: includeData,
		Limit:       limit,
		Cursor:      getString(args, "cursor"),
	}

	resp, err := secretsSvc.GetSecretsByPrefix(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsUpdateStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	req := &service.UpdateSecretStatusRequest{
		SecretID: getString(args, "secret_id"),
		Status:   service.SecretStatus(getString(args, "status")),
	}

	resp, err := secretsSvc.UpdateSecretStatus(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSecretsWatch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	ints := newIntArgReader(args)
	timeoutSeconds := ints.Int64("timeout_seconds")
	if timeoutSeconds == 0 {
		timeoutSeconds = 30
	}

	req := &service.WatchSecretsRequest{
		Prefix:       getString(args, "prefix"),
		StartVersion: ints.Int64("start_version"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}
	timeout, err := conv.SafeSecondsToDuration(timeoutSeconds)
	if err != nil {
		return nil, newIntArgError("timeout_seconds", err)
	}

	// Create a context with timeout if specified
	watchCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		watchCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	ch, err := secretsSvc.WatchSecretsByPrefix(watchCtx, req)
	if err != nil {
		return nil, err
	}

	// Collect changes until timeout or context cancellation
	var changes []*service.SecretChange
	for {
		select {
		case change, ok := <-ch:
			if !ok {
				// Channel closed, return collected changes
				return marshalResult(map[string]interface{}{
					"changes": changes,
					"count":   len(changes),
				})
			}
			changes = append(changes, change)
		case <-watchCtx.Done():
			// Timeout or cancellation, return collected changes
			return marshalResult(map[string]interface{}{
				"changes": changes,
				"count":   len(changes),
			})
		}
	}
}
