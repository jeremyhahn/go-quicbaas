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
	"fmt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Federation Manager Access
// =============================================================================

// federationManagerAccessor provides access to the federation manager from the QuicBaaS service.
type federationManagerAccessor interface {
	GetFederationManager(ctx context.Context) (iam.FederationManager, error)
}

// FederationServiceUnavailableError indicates the federation service is not available.
type FederationServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *FederationServiceUnavailableError) Error() string {
	return fmt.Sprintf("federation service unavailable: %s", e.Reason)
}

// IdentityServiceUnavailableError indicates the identity service is not available.
type IdentityServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *IdentityServiceUnavailableError) Error() string {
	return fmt.Sprintf("identity service unavailable: %s", e.Reason)
}

// getFederationManager retrieves the federation manager from the QuicBaaS service.
func (r *ToolRegistry) getFederationManager(ctx context.Context) (iam.FederationManager, error) {
	accessor, ok := r.service.(federationManagerAccessor)
	if !ok {
		return nil, &FederationServiceUnavailableError{Reason: "service does not support federation"}
	}
	mgr, err := accessor.GetFederationManager(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get federation manager", err)
	}
	if mgr == nil {
		return nil, &FederationServiceUnavailableError{Reason: "federation service not configured"}
	}
	return mgr, nil
}

// getProviderIDFromArgs extracts the provider ID from args, checking both
// "id" and "provider_id" fields for compatibility with SDK and MCP clients.
func getProviderIDFromArgs(args map[string]interface{}) string {
	if id := getString(args, "id"); id != "" {
		return id
	}
	return getString(args, "provider_id")
}

// =============================================================================
// Federation Tool Registration
// =============================================================================

// Tool names follow the SDK transport mapping pattern:
// quicbaas_iam_<subsystem>_<lowercase_method>
// where method names from the SDK client are lowercased directly.

func (r *ToolRegistry) registerFederationTools() {
	// Provider Management
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_createprovider",
		Description: "Register a new external identity provider for federated authentication",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Unique provider identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable provider name",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Provider type (oidc, saml, oauth2)",
					"enum":        []string{"oidc", "saml", "oauth2"},
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the provider is active",
					"default":     true,
				},
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "OAuth2 client identifier",
				},
				"client_secret": map[string]interface{}{
					"type":        "string",
					"description": "OAuth2 client secret",
				},
				"issuer_url": map[string]interface{}{
					"type":        "string",
					"description": "OIDC issuer URL for discovery",
				},
				"scopes": map[string]interface{}{
					"type":        "array",
					"description": "OAuth2 scopes to request",
					"items":       map[string]interface{}{"type": "string"},
				},
				"auto_provision": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable JIT user provisioning",
				},
				"allow_linking": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable account linking by email",
				},
				"default_roles": map[string]interface{}{
					"type":        "array",
					"description": "Roles assigned to auto-provisioned users",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"id", "tenant_id", "name", "type", "client_id", "issuer_url"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_getprovider",
		Description: "Retrieve an external identity provider by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "Provider ID",
				},
			},
			"required": []string{"tenant_id", "provider_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_updateprovider",
		Description: "Update an existing external identity provider",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Provider ID to update",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated provider name",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the provider is active",
				},
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "Updated OAuth2 client identifier",
				},
				"client_secret": map[string]interface{}{
					"type":        "string",
					"description": "Updated OAuth2 client secret",
				},
				"issuer_url": map[string]interface{}{
					"type":        "string",
					"description": "Updated OIDC issuer URL",
				},
				"scopes": map[string]interface{}{
					"type":        "array",
					"description": "Updated OAuth2 scopes",
					"items":       map[string]interface{}{"type": "string"},
				},
				"auto_provision": map[string]interface{}{
					"type":        "boolean",
					"description": "Updated JIT provisioning flag",
				},
				"allow_linking": map[string]interface{}{
					"type":        "boolean",
					"description": "Updated account linking flag",
				},
			},
			"required": []string{"id", "tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_deleteprovider",
		Description: "Delete an external identity provider",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "Provider ID to delete",
				},
			},
			"required": []string{"tenant_id", "provider_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_listproviders",
		Description: "List all external identity providers for a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of results to skip",
					"default":     0,
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	// Authentication Flow
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_beginlogin",
		Description: "Begin an OAuth/OIDC login flow with an external identity provider",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_name": map[string]interface{}{
					"type":        "string",
					"description": "Provider name to authenticate with",
				},
				"redirect_uri": map[string]interface{}{
					"type":        "string",
					"description": "URI to redirect to after authentication",
				},
			},
			"required": []string{"tenant_id", "provider_name", "redirect_uri"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_handlecallback",
		Description: "Handle the OAuth/OIDC callback after external authentication",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"state_token": map[string]interface{}{
					"type":        "string",
					"description": "CSRF state token from the login redirect",
				},
				"auth_code": map[string]interface{}{
					"type":        "string",
					"description": "Authorization code from the identity provider",
				},
			},
			"required": []string{"state_token", "auth_code"},
		},
	})

	// Account Linking
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_linkaccount",
		Description: "Link a user account to an external identity provider",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID to link",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "External provider ID",
				},
				"external_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the external provider for identity verification",
				},
			},
			"required": []string{"user_id", "tenant_id", "provider_id", "external_token"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_unlinkaccount",
		Description: "Remove the link between a user account and an external identity provider",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID to unlink",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "External provider ID to unlink from",
				},
			},
			"required": []string{"user_id", "tenant_id", "provider_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_federation_getuserlinks",
		Description: "List all linked external identities for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
			},
			"required": []string{"user_id", "tenant_id"},
		},
	})
}

// =============================================================================
// Identity Tool Registration
// =============================================================================

func (r *ToolRegistry) registerIdentityTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_identity_linkidentity",
		Description: "Link an external identity to a QuicBaaS user account",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "External identity provider ID",
				},
				"external_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the external provider",
				},
			},
			"required": []string{"user_id", "tenant_id", "provider_id", "external_token"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_identity_unlinkidentity",
		Description: "Remove an external identity link from a user account",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"provider_id": map[string]interface{}{
					"type":        "string",
					"description": "External identity provider ID to unlink",
				},
			},
			"required": []string{"user_id", "tenant_id", "provider_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_identity_listuseridentities",
		Description: "List all linked external identities for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "QuicBaaS user ID",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
			},
			"required": []string{"user_id", "tenant_id"},
		},
	})
}

// =============================================================================
// Federation Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMFederationCreateProvider(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	provider := &iam.ExternalProvider{
		ID:            getProviderIDFromArgs(args),
		TenantID:      getString(args, "tenant_id"),
		Name:          getString(args, "name"),
		Type:          iam.ProviderType(getString(args, "type")),
		Enabled:       getBool(args, "enabled"),
		ClientID:      getString(args, "client_id"),
		ClientSecret:  getString(args, "client_secret"),
		IssuerURL:     getString(args, "issuer_url"),
		Scopes:        getStringSlice(args, "scopes"),
		AutoProvision: getBool(args, "auto_provision"),
		AllowLinking:  getBool(args, "allow_linking"),
		DefaultRoles:  getStringSlice(args, "default_roles"),
	}

	// Default enabled to true if not explicitly provided
	if !provider.Enabled {
		if _, ok := args["enabled"]; !ok {
			provider.Enabled = true
		}
	}

	provider.ApplyDefaults()

	if err := fedMgr.RegisterProvider(ctx, provider); err != nil {
		return nil, err
	}
	return marshalResult(provider.Redacted())
}

func (r *ToolRegistry) executeIAMFederationGetProvider(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	provider, err := fedMgr.GetProvider(ctx, getString(args, "tenant_id"), getProviderIDFromArgs(args))
	if err != nil {
		return nil, err
	}
	return marshalResult(provider.Redacted())
}

func (r *ToolRegistry) executeIAMFederationUpdateProvider(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	// Get existing provider to apply partial updates
	provider, err := fedMgr.GetProvider(ctx, getString(args, "tenant_id"), getProviderIDFromArgs(args))
	if err != nil {
		return nil, err
	}

	if name := getString(args, "name"); name != "" {
		provider.Name = name
	}
	if _, ok := args["enabled"]; ok {
		provider.Enabled = getBool(args, "enabled")
	}
	if clientID := getString(args, "client_id"); clientID != "" {
		provider.ClientID = clientID
	}
	if clientSecret := getString(args, "client_secret"); clientSecret != "" {
		provider.ClientSecret = clientSecret
	}
	if issuerURL := getString(args, "issuer_url"); issuerURL != "" {
		provider.IssuerURL = issuerURL
	}
	if scopes := getStringSlice(args, "scopes"); len(scopes) > 0 {
		provider.Scopes = scopes
	}
	if _, ok := args["auto_provision"]; ok {
		provider.AutoProvision = getBool(args, "auto_provision")
	}
	if _, ok := args["allow_linking"]; ok {
		provider.AllowLinking = getBool(args, "allow_linking")
	}

	if err := fedMgr.UpdateProvider(ctx, provider); err != nil {
		return nil, err
	}
	return marshalResult(provider.Redacted())
}

func (r *ToolRegistry) executeIAMFederationDeleteProvider(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	if err := fedMgr.DeleteProvider(ctx, getString(args, "tenant_id"), getProviderIDFromArgs(args)); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"status": "deleted"})
}

func (r *ToolRegistry) executeIAMFederationListProviders(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	providers, err := fedMgr.ListProviders(ctx, getString(args, "tenant_id"), opts)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"providers": iam.RedactProviders(providers),
		"count":     len(providers),
	})
}

func (r *ToolRegistry) executeIAMFederationBeginLogin(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	redirect, err := fedMgr.BeginLogin(ctx,
		getString(args, "tenant_id"),
		getString(args, "provider_name"),
		getString(args, "redirect_uri"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(redirect)
}

func (r *ToolRegistry) executeIAMFederationHandleCallback(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	result, err := fedMgr.HandleCallback(ctx,
		getString(args, "state_token"),
		getString(args, "auth_code"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(result)
}

func (r *ToolRegistry) executeIAMFederationLinkAccount(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	if err := fedMgr.LinkAccount(ctx,
		userID,
		getString(args, "tenant_id"),
		getString(args, "provider_id"),
		getString(args, "external_token"),
	); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"status": "linked"})
}

func (r *ToolRegistry) executeIAMFederationUnlinkAccount(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	if err := fedMgr.UnlinkAccount(ctx,
		userID,
		getString(args, "tenant_id"),
		getString(args, "provider_id"),
	); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"status": "unlinked"})
}

func (r *ToolRegistry) executeIAMFederationGetUserLinks(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	links, err := fedMgr.GetUserLinks(ctx,
		userID,
		getString(args, "tenant_id"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"links": links,
		"count": len(links),
	})
}

// =============================================================================
// Identity Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMIdentityLink(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	if err := fedMgr.LinkAccount(ctx,
		userID,
		getString(args, "tenant_id"),
		getString(args, "provider_id"),
		getString(args, "external_token"),
	); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"status": "linked"})
}

func (r *ToolRegistry) executeIAMIdentityUnlink(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	if err := fedMgr.UnlinkAccount(ctx,
		userID,
		getString(args, "tenant_id"),
		getString(args, "provider_id"),
	); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"status": "unlinked"})
}

func (r *ToolRegistry) executeIAMIdentityListUserIdentities(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fedMgr, err := r.getFederationManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	links, err := fedMgr.GetUserLinks(ctx,
		userID,
		getString(args, "tenant_id"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"identities": links,
		"count":      len(links),
	})
}
