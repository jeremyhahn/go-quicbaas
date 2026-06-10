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
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// AuthorizationError represents an authorization-related error for MCP.
type AuthorizationError struct {
	Type    AuthorizationErrorType
	Message string
}

// AuthorizationErrorType defines the type of authorization error.
type AuthorizationErrorType int

const (
	// AuthErrorMissingToken indicates no authentication token was provided.
	AuthErrorMissingToken AuthorizationErrorType = iota

	// AuthErrorInvalidToken indicates the token is malformed or expired.
	AuthErrorInvalidToken

	// AuthErrorPermissionDenied indicates the user lacks required permissions.
	AuthErrorPermissionDenied

	// AuthErrorMisconfigured indicates the authorization decision could not be
	// made because of a server-side wiring error (e.g. the platform-token
	// validator's liveness checker or replay cache required by the tenant's authz
	// mode is absent). The request is denied fail-closed; the operator must fix
	// the wiring. It maps to a JSON-RPC InternalError, not a client auth error.
	AuthErrorMisconfigured
)

// Error implements the error interface.
func (e *AuthorizationError) Error() string {
	return e.Message
}

// publicTools lists tools that do not require authentication.
// These tools can be called by anyone without a valid token.
var publicTools = map[string]bool{
	"quicbaas_health":    true,
	"quicbaas_version":   true,
	"quicbaas_liveness":  true,
	"quicbaas_readiness": true,
	// qrdb-mirror health tool (Health.Check is exposed as qrdb_health — see
	// pkg/api/mcp/CLAUDE.md). Liveness/readiness stay quicbaas_*.
	"qrdb_health": true,
}

// toolResourceActionMap maps tool names to their required resource and action.
// Format: toolName -> (resource, action).
type resourceAction struct {
	resource string
	action   string
}

// getToolResourceAction returns the resource and action required for a given tool.
// Returns empty strings if the tool is not found (should be treated as requiring admin).
func getToolResourceAction(toolName string) (resource, action string) {
	// Check exact matches first
	if mapping, ok := toolResourceMapping[toolName]; ok {
		return mapping.resource, mapping.action
	}

	// Check prefix patterns
	for prefix, mapping := range toolPrefixMapping {
		if strings.HasPrefix(toolName, prefix) {
			return mapping.resource, mapping.action
		}
	}

	// Default to requiring admin access for unknown tools
	return interfaces.ResourceCluster, interfaces.ActionAdmin
}

// toolResourceMapping contains exact tool name to resource/action mappings.
var toolResourceMapping = map[string]resourceAction{
	// KV Operations - Read
	"quicbaas_get":        {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_list":       {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_query":      {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_scan":       {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_scan_index": {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_batch_get":  {interfaces.ResourceKeys, interfaces.ActionRead},

	// KV Operations - Write
	"quicbaas_put":       {interfaces.ResourceKeys, interfaces.ActionWrite},
	"quicbaas_batch_put": {interfaces.ResourceKeys, interfaces.ActionWrite},
	"quicbaas_set":       {interfaces.ResourceKeys, interfaces.ActionWrite},

	// KV Operations - Delete
	"quicbaas_delete":       {interfaces.ResourceKeys, interfaces.ActionDelete},
	"quicbaas_batch_delete": {interfaces.ResourceKeys, interfaces.ActionDelete},

	// Transaction Operations
	"quicbaas_transaction": {interfaces.ResourceTransactions, interfaces.ActionWrite},

	// Cluster Operations
	"quicbaas_cluster_status":     {interfaces.ResourceCluster, interfaces.ActionRead},
	"quicbaas_get_leader":         {interfaces.ResourceCluster, interfaces.ActionRead},
	"quicbaas_add_node":           {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_remove_node":        {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_create_shard":       {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_rebalance_cluster":  {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_rebalance_status":   {interfaces.ResourceCluster, interfaces.ActionRead},
	"quicbaas_migrate_data":       {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_migration_progress": {interfaces.ResourceCluster, interfaces.ActionRead},
	"quicbaas_replace_node":       {interfaces.ResourceCluster, interfaces.ActionAdmin},
	"quicbaas_upgrade_node":       {interfaces.ResourceCluster, interfaces.ActionAdmin},

	// Backup Operations
	"quicbaas_backup_create":  {interfaces.ResourceBackups, interfaces.ActionWrite},
	"quicbaas_backup_list":    {interfaces.ResourceBackups, interfaces.ActionRead},
	"quicbaas_backup_restore": {interfaces.ResourceBackups, interfaces.ActionWrite},
	"quicbaas_backup_delete":  {interfaces.ResourceBackups, interfaces.ActionDelete},
	"quicbaas_backup_verify":  {interfaces.ResourceBackups, interfaces.ActionRead},

	// User Management
	"quicbaas_user_create": {interfaces.ResourceUsers, interfaces.ActionWrite},
	"quicbaas_user_delete": {interfaces.ResourceUsers, interfaces.ActionDelete},
	"quicbaas_user_list":   {interfaces.ResourceUsers, interfaces.ActionRead},

	// Role Management
	"quicbaas_role_create": {interfaces.ResourceRoles, interfaces.ActionWrite},
	"quicbaas_role_delete": {interfaces.ResourceRoles, interfaces.ActionDelete},
	"quicbaas_role_list":   {interfaces.ResourceRoles, interfaces.ActionRead},
	"quicbaas_role_assign": {interfaces.ResourceRoles, interfaces.ActionWrite},
	"quicbaas_role_revoke": {interfaces.ResourceRoles, interfaces.ActionWrite},

	// qrdb-mirror exact tools (data plane owned by go-qrdb; identical semantics
	// to their quicbaas_* counterparts — see pkg/api/mcp/CLAUDE.md).
	"qrdb_get":    {interfaces.ResourceKeys, interfaces.ActionRead},
	"qrdb_list":   {interfaces.ResourceKeys, interfaces.ActionRead},
	"qrdb_put":    {interfaces.ResourceKeys, interfaces.ActionWrite},
	"qrdb_delete": {interfaces.ResourceKeys, interfaces.ActionDelete},

	"qrdb_user_create": {interfaces.ResourceUsers, interfaces.ActionWrite},
	"qrdb_user_delete": {interfaces.ResourceUsers, interfaces.ActionDelete},
	"qrdb_user_list":   {interfaces.ResourceUsers, interfaces.ActionRead},

	"qrdb_role_create": {interfaces.ResourceRoles, interfaces.ActionWrite},
	"qrdb_role_delete": {interfaces.ResourceRoles, interfaces.ActionDelete},
	"qrdb_role_list":   {interfaces.ResourceRoles, interfaces.ActionRead},
	"qrdb_role_assign": {interfaces.ResourceRoles, interfaces.ActionWrite},

	// Cache Operations
	"quicbaas_cache_stats": {interfaces.ResourceKeys, interfaces.ActionRead},
	"quicbaas_cache_clear": {interfaces.ResourceKeys, interfaces.ActionAdmin},

	// Note: Health tools (health, liveness, readiness) are public - see publicTools map

	// Aggregate/Time-Series
	"quicbaas_aggregate": {interfaces.ResourceTimeseries, interfaces.ActionRead},

	// Auth Operations (logout requires auth)
	"quicbaas_logout": {interfaces.ResourceUsers, interfaces.ActionWrite},

	// Orchestrator Operations - Read
	"quicbaas_orchestrator_discover":      {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_list":          {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_status":        {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_list_sessions": {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_list_backends": {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_health":        {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"quicbaas_orchestrator_cache_get":     {interfaces.ResourceOrchestrator, interfaces.ActionRead},

	// Orchestrator Operations - Write
	"quicbaas_orchestrator_register":  {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_spawn":     {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_prompt":    {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_close":     {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_decompose": {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_broadcast": {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"quicbaas_orchestrator_cache_put": {interfaces.ResourceOrchestrator, interfaces.ActionWrite},

	// Project Operations - Read
	"project_create":       {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"project_get":          {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"project_list":         {interfaces.ResourceOrchestrator, interfaces.ActionRead},
	"project_update":       {interfaces.ResourceOrchestrator, interfaces.ActionWrite},
	"project_delete":       {interfaces.ResourceOrchestrator, interfaces.ActionAdmin},
	"project_grant_agent":  {interfaces.ResourceOrchestrator, interfaces.ActionAdmin},
	"project_revoke_agent": {interfaces.ResourceOrchestrator, interfaces.ActionAdmin},

	// Agent Admin Operations - require admin
	"agent_disable": {interfaces.ResourceOrchestrator, interfaces.ActionAdmin},
	"agent_enable":  {interfaces.ResourceOrchestrator, interfaces.ActionAdmin},
}

// toolPrefixMapping contains prefix-based mappings for tool groups.
var toolPrefixMapping = map[string]resourceAction{
	// KMS Operations
	"quicbaas_kms_get":              {interfaces.ResourceKMS, interfaces.ActionRead},
	"quicbaas_kms_list":             {interfaces.ResourceKMS, interfaces.ActionRead},
	"quicbaas_kms_create":           {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_rotate":           {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_import":           {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_update":           {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_set":              {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_delete":           {interfaces.ResourceKMS, interfaces.ActionDelete},
	"quicbaas_kms_revoke":           {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_generate":         {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_set_key_safety":   {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_clear_key_safety": {interfaces.ResourceKMS, interfaces.ActionWrite},
	"quicbaas_kms_safety_check":     {interfaces.ResourceKMS, interfaces.ActionRead},
	"quicbaas_kms_safety_report":    {interfaces.ResourceKMS, interfaces.ActionRead},
	"quicbaas_kms_backend_status":   {interfaces.ResourceKMS, interfaces.ActionRead},
	"quicbaas_kms_backend_test":     {interfaces.ResourceKMS, interfaces.ActionRead},

	// Secrets Operations
	"quicbaas_secrets_get":                 {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_list":                {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_store":               {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_ensure":              {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_put":                 {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_delete":              {interfaces.ResourceSecrets, interfaces.ActionDelete},
	"quicbaas_secrets_version":             {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_versions":            {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_rollback":            {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_rotate":              {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_set":                 {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_set_rotation_policy": {interfaces.ResourceSecrets, interfaces.ActionWrite},
	"quicbaas_secrets_get_rotation_policy": {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_get_prefix":          {interfaces.ResourceSecrets, interfaces.ActionRead},
	"quicbaas_secrets_watch":               {interfaces.ResourceSecrets, interfaces.ActionRead},

	// Trust Operations (cross-sign / AIA / DANE composition over the CA)
	"quicbaas_trust_cross_signed_root_pool": {interfaces.ResourceCertificate, interfaces.ActionRead},
	"quicbaas_trust_auto_import_aia":        {interfaces.ResourceCertificate, interfaces.ActionWrite},
	"quicbaas_trust_publish_dane":           {interfaces.ResourceCertificate, interfaces.ActionWrite},

	// Crypto Operations
	"quicbaas_crypto_encrypt": {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_decrypt": {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_sign":    {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_verify":  {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_hmac":    {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_hash":    {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_random":  {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_datakey": {interfaces.ResourceCrypto, interfaces.ActionWrite},
	"quicbaas_crypto_rewrap":  {interfaces.ResourceCrypto, interfaces.ActionWrite},

	// Seal Operations
	"quicbaas_seal":   {interfaces.ResourceSeal, interfaces.ActionAdmin},
	"quicbaas_unseal": {interfaces.ResourceSeal, interfaces.ActionAdmin},

	// Timeseries Operations
	"quicbaas_timeseries_query":  {interfaces.ResourceTimeseries, interfaces.ActionRead},
	"quicbaas_timeseries_write":  {interfaces.ResourceTimeseries, interfaces.ActionWrite},
	"quicbaas_timeseries_delete": {interfaces.ResourceTimeseries, interfaces.ActionDelete},

	// Vector Operations
	"quicbaas_vector_search": {interfaces.ResourceVector, interfaces.ActionRead},
	"quicbaas_vector_get":    {interfaces.ResourceVector, interfaces.ActionRead},
	"quicbaas_vector_upsert": {interfaces.ResourceVector, interfaces.ActionWrite},
	"quicbaas_vector_insert": {interfaces.ResourceVector, interfaces.ActionWrite},
	"quicbaas_vector_delete": {interfaces.ResourceVector, interfaces.ActionDelete},

	// Parameter Store Operations
	"quicbaas_parameter_get":          {interfaces.ResourceParameters, interfaces.ActionRead},
	"quicbaas_parameter_set":          {interfaces.ResourceParameters, interfaces.ActionWrite},
	"quicbaas_parameter_delete":       {interfaces.ResourceParameters, interfaces.ActionDelete},
	"quicbaas_parameter_list":         {interfaces.ResourceParameters, interfaces.ActionRead},
	"quicbaas_parameter_batch_get":    {interfaces.ResourceParameters, interfaces.ActionRead},
	"quicbaas_parameter_batch_set":    {interfaces.ResourceParameters, interfaces.ActionWrite},
	"quicbaas_parameter_batch_delete": {interfaces.ResourceParameters, interfaces.ActionDelete},
	"quicbaas_parameter_search":       {interfaces.ResourceParameters, interfaces.ActionRead},

	// qrdb-mirror prefix tools (data plane owned by go-qrdb). Ordering within
	// the prefix map does not matter for correctness because getToolResourceAction
	// uses HasPrefix; the more specific prefixes (e.g. *_search/*_get → Read,
	// *_delete → Delete) are listed so they match before broader fallbacks.

	// Vector (mirror set)
	"qrdb_vector_search":       {interfaces.ResourceVector, interfaces.ActionRead},
	"qrdb_vector_get":          {interfaces.ResourceVector, interfaces.ActionRead},
	"qrdb_vector_list":         {interfaces.ResourceVector, interfaces.ActionRead},
	"qrdb_vector_upsert":       {interfaces.ResourceVector, interfaces.ActionWrite},
	"qrdb_vector_update":       {interfaces.ResourceVector, interfaces.ActionWrite},
	"qrdb_vector_batch_upsert": {interfaces.ResourceVector, interfaces.ActionWrite},
	"qrdb_vector_create":       {interfaces.ResourceVector, interfaces.ActionWrite},
	"qrdb_vector_rebuild":      {interfaces.ResourceVector, interfaces.ActionWrite},
	"qrdb_vector_delete":       {interfaces.ResourceVector, interfaces.ActionDelete},
	"qrdb_vector_batch_delete": {interfaces.ResourceVector, interfaces.ActionDelete},

	// Timeseries
	"qrdb_ts_query":  {interfaces.ResourceTimeseries, interfaces.ActionRead},
	"qrdb_ts_list":   {interfaces.ResourceTimeseries, interfaces.ActionRead},
	"qrdb_ts_get":    {interfaces.ResourceTimeseries, interfaces.ActionRead},
	"qrdb_ts_write":  {interfaces.ResourceTimeseries, interfaces.ActionWrite},
	"qrdb_ts_set":    {interfaces.ResourceTimeseries, interfaces.ActionWrite},
	"qrdb_ts_delete": {interfaces.ResourceTimeseries, interfaces.ActionDelete},

	// Barrier lifecycle (privileged)
	"qrdb_barrier_status":          {interfaces.ResourceBarrier, interfaces.ActionRead},
	"qrdb_barrier_recovery_status": {interfaces.ResourceBarrier, interfaces.ActionRead},
	"qrdb_barrier_root_token":      {interfaces.ResourceBarrier, interfaces.ActionAdmin},
	"qrdb_barrier":                 {interfaces.ResourceBarrier, interfaces.ActionAdmin},

	// Ceremony lifecycle (privileged)
	"qrdb_ceremony_status": {interfaces.ResourceCeremony, interfaces.ActionRead},
	"qrdb_ceremony":        {interfaces.ResourceCeremony, interfaces.ActionAdmin},

	// Credentials (versioned named-credential rotation)
	"qrdb_credential_list":     {interfaces.ResourceSecrets, interfaces.ActionRead},
	"qrdb_credential_retrieve": {interfaces.ResourceSecrets, interfaces.ActionRead},
	"qrdb_credential_strategy": {interfaces.ResourceSecrets, interfaces.ActionRead},
	"qrdb_credential_delete":   {interfaces.ResourceSecrets, interfaces.ActionDelete},
	"qrdb_credential":          {interfaces.ResourceSecrets, interfaces.ActionWrite},
}

// IsPublicTool returns true if the tool does not require authentication.
func IsPublicTool(toolName string) bool {
	return publicTools[toolName]
}

// AuthorizationChecker provides authorization checking for MCP tools.
type AuthorizationChecker struct {
	authService   interfaces.AuthorizationService
	authManager   interfaces.AuthManager
	mcpAuthConfig *MCPAuthConfig
	oidcProvider  interfaces.OIDCProvider

	// validatorAuthorizer, when set, is the unified platform-token authorization
	// decision point: a SINGLE call validates the bearer offline (sig / iss / aud
	// / exp), enforces DPoP sender-constraint for cnf.jkt-bound tokens, runs the
	// offline scp -> permission decision against the qrdb-canonical permission,
	// and applies the per-tenant liveness/revocation check. When installed it
	// REPLACES the OIDC-validate + online authService PDP path entirely. It is
	// wired by lifecycle (pkg/server) under RequireAuthz=true.
	validatorAuthorizer ValidatorAuthorizer
}

// SetValidatorAuthorizer installs the unified platform-token Validator as the
// request-time authorization decision point. When set it REPLACES the
// OIDC-validate + online authService PDP path: the validator performs token
// validation, DPoP sender-constraint, the offline scp decision, and the
// per-tenant liveness check in one call. The validator authorizes on the
// qrdb-canonical colon "resource:action".
func (ac *AuthorizationChecker) SetValidatorAuthorizer(v ValidatorAuthorizer) {
	ac.validatorAuthorizer = v
}

// NewAuthorizationChecker creates a new authorization checker.
// If authService is nil, authorization is effectively disabled (all requests pass).
func NewAuthorizationChecker(authService interfaces.AuthorizationService, authManager interfaces.AuthManager) *AuthorizationChecker {
	return &AuthorizationChecker{
		authService:   authService,
		authManager:   authManager,
		mcpAuthConfig: nil,
		oidcProvider:  nil,
	}
}

// NewAuthorizationCheckerWithConfig creates a new authorization checker with MCP auth config.
// The MCPAuthConfig enables audience validation for token security.
func NewAuthorizationCheckerWithConfig(authService interfaces.AuthorizationService, authManager interfaces.AuthManager, mcpAuthConfig *MCPAuthConfig) *AuthorizationChecker {
	return &AuthorizationChecker{
		authService:   authService,
		authManager:   authManager,
		mcpAuthConfig: mcpAuthConfig,
		oidcProvider:  nil,
	}
}

// SetMCPAuthConfig sets the MCP authorization configuration for audience validation.
func (ac *AuthorizationChecker) SetMCPAuthConfig(config *MCPAuthConfig) {
	ac.mcpAuthConfig = config
}

// CheckToolAuthorization validates that the given token has permission to execute the tool.
// Returns nil if authorized, or an AuthorizationError if not.
//
// When the unified platform-token validator is installed (RequireAuthz=true), the
// decision is made by a SINGLE validator.Authorize call: offline token validation
// + DPoP sender-constraint + offline scp decision + per-tenant liveness. This
// REPLACES the OIDC-validate + online authService PDP path below.
//
// Legacy token validation priority (validator NOT installed):
// 1. If OIDCProvider is configured, use it for validation (supports federation)
// 2. Fall back to AuthManager if OIDC validation fails or is not configured.
func (ac *AuthorizationChecker) CheckToolAuthorization(ctx context.Context, toolName, token string) *AuthorizationError {
	// Unified platform-token validator path (RequireAuthz=true). One call decides.
	if ac.validatorAuthorizer != nil {
		_, authErr := ac.authorizeViaValidator(ctx, toolName, token)
		return authErr
	}

	// Check if tool is public (no auth required)
	// Public tools like quicbaas_health, quicbaas_version, etc. bypass all auth checks.
	if IsPublicTool(toolName) {
		return nil
	}

	// If no auth is configured at all, allow all requests (auth disabled mode).
	// This checks all three possible auth providers: authService, oidcProvider, and authManager.
	if ac.authService == nil && ac.oidcProvider == nil && ac.authManager == nil {
		return nil
	}

	// Token is required for non-public tools when auth is enabled
	if token == "" {
		return &AuthorizationError{
			Type:    AuthErrorMissingToken,
			Message: "authentication required",
		}
	}

	// Track validation state
	var valid bool
	var userID, username, tenantID string
	var roles []string
	var validatedWithOIDC bool

	// Try OIDC provider first if configured
	if ac.oidcProvider != nil {
		claims, err := ac.oidcProvider.ValidateAccessToken(ctx, token)
		if err == nil && claims != nil {
			valid = true
			validatedWithOIDC = true
			userID = claims.GetSubject()
			username = claims.GetName()
			roles = claims.GetRoles()
			tenantID = claims.GetTenantID()

			// Validate audience from claims directly (more efficient)
			if audErr := ac.validateTokenAudienceFromClaims(claims); audErr != nil {
				return audErr
			}
		}
	}

	// Fall back to AuthManager if OIDC validation failed or was not attempted
	if !valid && ac.authManager != nil {
		validationResult, err := ac.authManager.ValidateToken(ctx, token)
		if err != nil {
			return &AuthorizationError{
				Type:    AuthErrorInvalidToken,
				Message: "invalid token",
			}
		}

		// Try to extract validation info using reflection-free approach
		switch v := validationResult.(type) {
		case interface{ Valid() bool }:
			valid = v.Valid()
		}

		// Access fields through map if returned as struct pointer
		if validMap, ok := validationResult.(map[string]interface{}); ok {
			if v, ok := validMap["Valid"].(bool); ok {
				valid = v
			}
			if v, ok := validMap["UserID"].(string); ok {
				userID = v
			}
			if v, ok := validMap["Username"].(string); ok {
				username = v
			}
			if v, ok := validMap["Roles"].([]string); ok {
				roles = v
			}
		} else {
			// Use type reflection-safe approach for struct
			if !valid {
				valid = extractBoolField(validationResult, "Valid")
			}
			if userID == "" {
				userID = extractStringField(validationResult, "UserID")
			}
			if username == "" {
				username = extractStringField(validationResult, "Username")
			}
			if len(roles) == 0 {
				roles = extractStringSliceField(validationResult, "Roles")
			}
		}

		// Capture the tenant from the validated result so the qrdb PDP scope
		// rules resolve against the caller's tenant. A missing tenant claim is
		// not an error here; tenant enforcement is not this method's job.
		if tenantID == "" {
			if g, ok := validationResult.(interface{ GetTenantID() string }); ok {
				tenantID = g.GetTenantID()
			}
		}

		if !valid {
			return &AuthorizationError{
				Type:    AuthErrorInvalidToken,
				Message: "invalid token",
			}
		}

		// Validate token audience (RFC 8707 Resource Indicators)
		// Only needed if we didn't validate with OIDC provider
		if !validatedWithOIDC {
			if audErr := ac.ValidateTokenAudience(token); audErr != nil {
				return audErr
			}
		}
	}

	// If neither OIDC nor AuthManager produced a valid result
	if !valid {
		return &AuthorizationError{
			Type:    AuthErrorInvalidToken,
			Message: "invalid token",
		}
	}

	// If no authorization service is configured, allow access after token validation
	if ac.authService == nil {
		return nil
	}

	// Get resource and action for this tool from the broker-name mapper. This is
	// the legacy AuthzService path, reached only when a non-nil authService is
	// installed without the platform-token validator (RequireAuthz=false).
	resource, action := getToolResourceAction(toolName)

	// Build authorization request
	authReq := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            userID,
			Username:      username,
			TenantID:      tenantID,
			Roles:         roles,
			Authenticated: true,
		},
		Resource: resource,
		Action:   action,
	}

	// Check authorization
	decision, err := ac.authService.Authorize(ctx, authReq)
	if err != nil {
		return &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: authorization check failed",
		}
	}

	if !decision.Allowed {
		reason := decision.Reason
		if reason == "" {
			reason = "insufficient permissions"
		}
		return &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: " + reason,
		}
	}

	return nil
}

// CheckToolAuthorizationWithContext validates authorization for the given tool and,
// on success, returns a context enriched with the authenticated Subject and the
// caller's tenant so that service-layer per-key authorization can access the
// principal. MCP authenticates via bearer token (not mTLS / peer cert), so the
// tenant is sourced from the validated token's tenant claim. A missing tenant
// claim is not an error here — tenant enforcement is not this method's job.
// On denial it returns the original ctx and a non-nil *AuthorizationError.
func (ac *AuthorizationChecker) CheckToolAuthorizationWithContext(ctx context.Context, toolName, token string) (context.Context, *AuthorizationError) {
	// Unified platform-token validator path (RequireAuthz=true). The single
	// Authorize call both decides and returns the canonical post-validation
	// identity, from which the principal + tenant are reconstructed and injected.
	if ac.validatorAuthorizer != nil {
		authzCtx, authErr := ac.authorizeViaValidator(ctx, toolName, token)
		if authErr != nil {
			return ctx, authErr
		}
		// authzCtx is nil for public tools (no token, no principal to inject).
		if authzCtx != nil {
			ctx = injectPrincipalFromAuthorizedContext(ctx, authzCtx)
		}
		return ctx, nil
	}

	// Reuse existing logic for auth check.
	if authErr := ac.CheckToolAuthorization(ctx, toolName, token); authErr != nil {
		return ctx, authErr
	}

	// CheckToolAuthorization succeeded — rebuild Subject from token and capture the
	// tenant from the SAME validated result, then inject both into the context for
	// downstream service-layer per-key authorization and tenant isolation.
	var subject interfaces.Subject
	var tenantID string
	if token != "" {
		if ac.oidcProvider != nil {
			if claims, err := ac.oidcProvider.ValidateAccessToken(ctx, token); err == nil && claims != nil {
				subject = interfaces.Subject{
					ID:            claims.GetSubject(),
					Username:      claims.GetName(),
					Roles:         claims.GetRoles(),
					Authenticated: true,
				}
				tenantID = claims.GetTenantID()
			}
		}
		if !subject.Authenticated && ac.authManager != nil {
			if result, err := ac.authManager.ValidateToken(ctx, token); err == nil {
				// Type-assert the result; handle both concrete and map forms.
				if tvr, ok := result.(*auth.TokenValidationResult); ok && tvr.Valid {
					subject = interfaces.Subject{
						ID:            tvr.UserID,
						Username:      tvr.Username,
						Roles:         tvr.Roles,
						Authenticated: true,
					}
					tenantID = tvr.GetTenantID()
				}
			}
		}
	}
	if subject.Authenticated {
		ctx = auth.SubjectIntoContext(ctx, subject)
	}
	if tenantID != "" {
		ctx = iam.ContextWithTenant(ctx, tenantID)
	}
	return ctx, nil
}

// Helper functions to extract fields from interface{} safely

func extractBoolField(v interface{}, fieldName string) bool {
	// Use struct field access if possible
	type boolGetter interface {
		GetValid() bool
	}
	if bg, ok := v.(boolGetter); ok && fieldName == "Valid" {
		return bg.GetValid()
	}

	// For TokenValidationResult, access Valid field directly
	if fieldName == "Valid" {
		if tvr, ok := v.(interface{ GetValid() bool }); ok {
			return tvr.GetValid()
		}
		// Try accessing as struct with Valid field
		if hasValid, ok := v.(interface{ IsValid() bool }); ok {
			return hasValid.IsValid()
		}
	}
	return false
}

func extractStringField(v interface{}, fieldName string) string {
	switch fieldName {
	case "UserID":
		if g, ok := v.(interface{ GetUserID() string }); ok {
			return g.GetUserID()
		}
	case "Username":
		if g, ok := v.(interface{ GetUsername() string }); ok {
			return g.GetUsername()
		}
	}
	return ""
}

func extractStringSliceField(v interface{}, fieldName string) []string {
	if fieldName == "Roles" {
		if g, ok := v.(interface{ GetRoles() []string }); ok {
			return g.GetRoles()
		}
	}
	return nil
}

// TokenValidationResultAccessor provides access to TokenValidationResult fields.
// This interface allows accessing fields from *auth.TokenValidationResult without
// importing the auth package directly.
type TokenValidationResultAccessor interface {
	GetValid() bool
	GetUserID() string
	GetUsername() string
	GetRoles() []string
}

// ExtractTokenInfo extracts user information from a token validation result.
// This is a helper for building authorization subjects from validated tokens.
func ExtractTokenInfo(result interface{}) (valid bool, userID, username string, roles []string) {
	if accessor, ok := result.(TokenValidationResultAccessor); ok {
		return accessor.GetValid(), accessor.GetUserID(), accessor.GetUsername(), accessor.GetRoles()
	}

	// Fallback: try direct field access
	valid = extractBoolField(result, "Valid")
	userID = extractStringField(result, "UserID")
	username = extractStringField(result, "Username")
	roles = extractStringSliceField(result, "Roles")
	return
}

// ValidateTokenAudience validates that a JWT token's audience contains the MCP resource URI.
// This is required per RFC 8707 Resource Indicators and MCP Authorization spec.
// Returns nil if valid, or an error if the audience doesn't match.
func (ac *AuthorizationChecker) ValidateTokenAudience(token string) *AuthorizationError {
	// Skip audience validation if not configured
	if ac.mcpAuthConfig == nil || !ac.mcpAuthConfig.Enabled || ac.mcpAuthConfig.ResourceURI == "" {
		return nil
	}

	// Parse JWT to extract audience (without verification - already validated by authManager)
	audience, err := extractJWTAudience(token)
	if err != nil {
		// If we can't parse audience, skip validation (allow for backwards compatibility)
		return nil
	}

	// Check if MCP resource URI is in the audience
	resourceURI := ac.mcpAuthConfig.ResourceURI
	for _, aud := range audience {
		if aud == resourceURI {
			return nil // Audience matches
		}
	}

	return &AuthorizationError{
		Type:    AuthErrorInvalidToken,
		Message: "token audience does not include MCP resource URI",
	}
}

// extractJWTAudience extracts the audience claim from a JWT token.
// It parses the token without verification (verification is done separately).
func extractJWTAudience(token string) ([]string, error) {
	// Split JWT into parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, &AuthorizationError{Type: AuthErrorInvalidToken, Message: "invalid JWT format"}
	}

	// Decode payload (second part)
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, &AuthorizationError{Type: AuthErrorInvalidToken, Message: "invalid JWT payload encoding"}
	}

	// Parse JSON payload
	var claims map[string]interface{}
	if err := parseJSON(payload, &claims); err != nil {
		return nil, &AuthorizationError{Type: AuthErrorInvalidToken, Message: "invalid JWT payload JSON"}
	}

	// Extract audience claim
	audClaim, ok := claims["aud"]
	if !ok {
		return nil, nil // No audience claim is valid (optional claim)
	}

	// Audience can be a string or array of strings
	switch aud := audClaim.(type) {
	case string:
		return []string{aud}, nil
	case []interface{}:
		result := make([]string, 0, len(aud))
		for _, v := range aud {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result, nil
	default:
		return nil, nil // Unknown format, treat as no audience
	}
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Use RawURLEncoding which handles no-padding base64url format
	return base64.RawURLEncoding.DecodeString(s)
}

// parseJSON parses JSON data into a map.
func parseJSON(data []byte, v *map[string]interface{}) error {
	return json.Unmarshal(data, v)
}
