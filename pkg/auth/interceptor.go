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

// Package auth provides gRPC interceptors for authentication and authorization
package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// InterceptorConfig configures the gRPC auth interceptor behavior.
type InterceptorConfig struct {
	// AuthManager is the legacy auth manager for backward compatibility.
	// Used when AuthzService is nil.
	AuthManager *AuthManager

	// AuthzService is the unified authorization service.
	// When set, this takes precedence over AuthManager for authorization.
	AuthzService interfaces.AuthorizationService

	// Logger is the logger for diagnostic output.
	Logger *logger.Logger

	// PublicMethods lists gRPC methods that do not require authentication.
	// Default public methods are automatically included.
	PublicMethods []string
}

// defaultPublicMethods lists gRPC methods that should be excluded from auth.
var defaultPublicMethods = map[string]bool{
	"/quicbaas.v1.QuicBaaS/Login":                                    true,
	"/quicbaas.v1.Health/Check":                                      true,
	"/quicbaas.v1.Health/Watch":                                      true,
	"/grpc.health.v1.Health/Check":                                   true,
	"/grpc.health.v1.Health/Watch":                                   true,
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
}

// UnaryAuthInterceptor creates a gRPC unary interceptor for authentication.
// This is the backward-compatible function signature.
func UnaryAuthInterceptor(authManager *AuthManager, log *logger.Logger) grpc.UnaryServerInterceptor {
	return UnaryAuthInterceptorWithConfig(&InterceptorConfig{
		AuthManager: authManager,
		Logger:      log,
	})
}

// UnaryAuthInterceptorWithService creates a gRPC unary interceptor using the AuthorizationService.
func UnaryAuthInterceptorWithService(authManager *AuthManager, authzService interfaces.AuthorizationService, log *logger.Logger) grpc.UnaryServerInterceptor {
	return UnaryAuthInterceptorWithConfig(&InterceptorConfig{
		AuthManager:  authManager,
		AuthzService: authzService,
		Logger:       log,
	})
}

// UnaryAuthInterceptorWithConfig creates a gRPC unary interceptor with full configuration.
func UnaryAuthInterceptorWithConfig(config *InterceptorConfig) grpc.UnaryServerInterceptor {
	publicMethods := buildPublicMethodsMap(config.PublicMethods)

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Skip auth if disabled
		if !config.AuthManager.config.Enabled {
			return handler(ctx, req)
		}

		// Check if method is public (no auth required)
		if isPublicMethod(info.FullMethod, publicMethods) {
			return handler(ctx, req)
		}

		// Extract token from metadata
		token, err := extractToken(ctx)
		if err != nil {
			config.Logger.WithError(err).Warn("Failed to extract auth token")
			if !config.AuthManager.config.AllowAnonymous {
				return nil, status.Error(codes.Unauthenticated, "missing or invalid authentication token")
			}
		}

		// Parse method to determine resource and action
		resource, action := parseGRPCMethod(info.FullMethod)

		// Use AuthorizationService if available, otherwise fall back to AuthManager
		if config.AuthzService != nil {
			if err := authorizeWithService(ctx, config, token, resource, action, info.FullMethod); err != nil {
				return nil, err
			}
		} else {
			// Legacy path using AuthManager directly
			if err := config.AuthManager.Authorize(ctx, token, resource, action); err != nil {
				config.Logger.WithError(err).Warnf("Authorization failed for method: %s", info.FullMethod)
				return nil, status.Error(codes.PermissionDenied, "insufficient permissions")
			}
		}

		// Inject authenticated Subject into context so service-layer per-key
		// authorization can access the principal without re-parsing the token.
		ctx = InjectSubjectIntoContext(ctx, config, token)

		// Proceed with the request
		return handler(ctx, req)
	}
}

// StreamAuthInterceptor creates a gRPC stream interceptor for authentication.
// This is the backward-compatible function signature.
func StreamAuthInterceptor(authManager *AuthManager, log *logger.Logger) grpc.StreamServerInterceptor {
	return StreamAuthInterceptorWithConfig(&InterceptorConfig{
		AuthManager: authManager,
		Logger:      log,
	})
}

// StreamAuthInterceptorWithService creates a gRPC stream interceptor using the AuthorizationService.
func StreamAuthInterceptorWithService(authManager *AuthManager, authzService interfaces.AuthorizationService, log *logger.Logger) grpc.StreamServerInterceptor {
	return StreamAuthInterceptorWithConfig(&InterceptorConfig{
		AuthManager:  authManager,
		AuthzService: authzService,
		Logger:       log,
	})
}

// StreamAuthInterceptorWithConfig creates a gRPC stream interceptor with full configuration.
func StreamAuthInterceptorWithConfig(config *InterceptorConfig) grpc.StreamServerInterceptor {
	publicMethods := buildPublicMethodsMap(config.PublicMethods)

	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		// Skip auth if disabled
		if !config.AuthManager.config.Enabled {
			return handler(srv, ss)
		}

		// Check if method is public (no auth required)
		if isPublicMethod(info.FullMethod, publicMethods) {
			return handler(srv, ss)
		}

		// Extract token from metadata
		token, err := extractToken(ss.Context())
		if err != nil {
			config.Logger.WithError(err).Warn("Failed to extract auth token")
			if !config.AuthManager.config.AllowAnonymous {
				return status.Error(codes.Unauthenticated, "missing or invalid authentication token")
			}
		}

		// Parse method to determine resource and action
		resource, action := parseGRPCMethod(info.FullMethod)

		// Use AuthorizationService if available, otherwise fall back to AuthManager
		if config.AuthzService != nil {
			if err := authorizeWithService(ss.Context(), config, token, resource, action, info.FullMethod); err != nil {
				return err
			}
		} else {
			// Legacy path using AuthManager directly
			if err := config.AuthManager.Authorize(ss.Context(), token, resource, action); err != nil {
				config.Logger.WithError(err).Warnf("Authorization failed for method: %s", info.FullMethod)
				return status.Error(codes.PermissionDenied, "insufficient permissions")
			}
		}

		// Proceed with the stream
		return handler(srv, ss)
	}
}

// authorizeWithService performs authorization using the unified AuthorizationService.
func authorizeWithService(ctx context.Context, config *InterceptorConfig, token, resource, action, fullMethod string) error {
	// Build subject from token validation
	subject, err := buildSubjectFromToken(ctx, config.AuthManager, token)
	if err != nil {
		config.Logger.WithError(err).Warn("Failed to validate token")
		return status.Error(codes.Unauthenticated, "invalid authentication token")
	}

	// SECURITY: Validate tenant header against token's tenant
	// This prevents cross-tenant access by spoofing the x-tenant-id header
	if err := validateTenantHeader(ctx, subject, config.Logger); err != nil {
		return err
	}

	// Build authorization request
	authzReq := &interfaces.AuthorizationRequest{
		Subject:  *subject,
		Resource: resource,
		Action:   action,
		Context: map[string]interface{}{
			"grpc_method": fullMethod,
			"protocol":    "grpc",
		},
	}

	// Perform authorization
	decision, err := config.AuthzService.Authorize(ctx, authzReq)
	if err != nil {
		config.Logger.WithError(err).Warnf("Authorization service error for method: %s", fullMethod)
		return status.Error(codes.Internal, "authorization service error")
	}

	if !decision.Allowed {
		config.Logger.WithFields(map[string]interface{}{
			"method":   fullMethod,
			"resource": resource,
			"action":   action,
			"reason":   decision.Reason,
			"provider": decision.Provider,
		}).Warn("Authorization denied")
		return status.Error(codes.PermissionDenied, "insufficient permissions")
	}

	return nil
}

// InjectSubjectIntoContext injects the authenticated Subject derived from token
// into ctx so downstream service-layer per-key authorization can access the
// principal without re-parsing credentials.
//
// This is called by the gRPC unary and stream interceptors immediately after
// successful authentication. It is exported so REST, QUIC, and MCP middleware
// can reuse the same Subject derivation logic.
func InjectSubjectIntoContext(ctx context.Context, config *InterceptorConfig, token string) context.Context {
	subject, err := buildSubjectFromToken(ctx, config.AuthManager, token)
	if err != nil || subject == nil {
		return ctx
	}
	return SubjectIntoContext(ctx, *subject)
}

// validateTenantHeader validates that the x-tenant-id header matches the token's tenant.
// This is a critical security check that prevents cross-tenant access attacks.
// If a user's token has a tenant ID, the x-tenant-id header MUST match it.
// If the token has no tenant ID (single-tenant mode), this check is skipped.
func validateTenantHeader(ctx context.Context, subject *interfaces.Subject, log *logger.Logger) error {
	// If the user has no tenant ID in their token, skip tenant validation
	// This allows single-tenant deployments to work without tenant headers
	if subject.TenantID == "" {
		return nil
	}

	// Extract x-tenant-id header from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Warn("Missing metadata for tenant validation")
		return status.Error(codes.InvalidArgument, "missing request metadata")
	}

	tenantHeaders := md.Get("x-tenant-id")
	if len(tenantHeaders) == 0 {
		log.WithFields(map[string]interface{}{
			"user_id":       subject.ID,
			"user_tenant":   subject.TenantID,
			"header_tenant": "",
		}).Warn("Missing x-tenant-id header for multi-tenant user")
		return status.Error(codes.InvalidArgument, "x-tenant-id header required for multi-tenant access")
	}

	headerTenantID := tenantHeaders[0]

	// CRITICAL SECURITY CHECK: The header tenant must match the token's tenant
	if headerTenantID != subject.TenantID {
		log.WithFields(map[string]interface{}{
			"user_id":       subject.ID,
			"user_tenant":   subject.TenantID,
			"header_tenant": headerTenantID,
		}).Warn("Cross-tenant access attempt detected: x-tenant-id header does not match token tenant")
		return status.Error(codes.PermissionDenied, "cross-tenant access denied")
	}

	return nil
}

// buildSubjectFromToken validates the token and constructs a Subject.
func buildSubjectFromToken(ctx context.Context, am *AuthManager, token string) (*interfaces.Subject, error) {
	// Handle empty token (anonymous access)
	if token == "" {
		return &interfaces.Subject{
			ID:            "anonymous",
			Username:      "anonymous",
			Authenticated: false,
			Roles:         []string{},
			Groups:        []string{},
			Attributes:    make(map[string]interface{}),
		}, nil
	}

	// Validate the token
	result, err := am.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	if !result.Valid {
		return nil, status.Error(codes.Unauthenticated, "token is not valid")
	}

	return &interfaces.Subject{
		ID:            result.UserID,
		Username:      result.Username,
		TenantID:      result.TenantID,
		Authenticated: true,
		Roles:         result.Roles,
		Groups:        []string{}, // Groups not currently tracked in AuthManager
		Attributes:    make(map[string]interface{}),
	}, nil
}

// buildPublicMethodsMap creates a lookup map from the default and custom public methods.
func buildPublicMethodsMap(customMethods []string) map[string]bool {
	methods := make(map[string]bool, len(defaultPublicMethods)+len(customMethods))
	for k, v := range defaultPublicMethods {
		methods[k] = v
	}
	for _, m := range customMethods {
		methods[m] = true
	}
	return methods
}

// isPublicMethod checks if a gRPC method should bypass authentication.
func isPublicMethod(fullMethod string, publicMethods map[string]bool) bool {
	// Check exact match
	if publicMethods[fullMethod] {
		return true
	}

	// Check prefix match for services (e.g., "/grpc.health.v1.Health/*")
	for method := range publicMethods {
		if strings.HasSuffix(method, "/*") {
			prefix := strings.TrimSuffix(method, "*")
			if strings.HasPrefix(fullMethod, prefix) {
				return true
			}
		}
	}

	return false
}

// extractToken extracts the authentication token from gRPC metadata.
func extractToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Try "authorization" header first
	values := md.Get("authorization")
	if len(values) == 0 {
		// Try "x-auth-token" as fallback
		values = md.Get("x-auth-token")
		if len(values) == 0 {
			return "", status.Error(codes.Unauthenticated, "missing authorization token")
		}
	}

	token := values[0]

	// Remove "Bearer " prefix if present
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}

	return token, nil
}

// ExtractTokenFromContext extracts the authentication token from context metadata.
// This is an exported wrapper for use by other packages that need to extract tokens.
// It checks both gRPC incoming metadata and outgoing metadata.
// Returns an empty string and nil error if no token is found (for optional auth scenarios).
// Returns the token string if found.
func ExtractTokenFromContext(ctx context.Context) (string, error) {
	// First try incoming context (server-side)
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			token := values[0]
			if strings.HasPrefix(token, "Bearer ") {
				token = strings.TrimPrefix(token, "Bearer ")
			}
			return token, nil
		}
		if values := md.Get("x-auth-token"); len(values) > 0 {
			return values[0], nil
		}
	}

	// Then try outgoing context (client-side)
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			token := values[0]
			if strings.HasPrefix(token, "Bearer ") {
				token = strings.TrimPrefix(token, "Bearer ")
			}
			return token, nil
		}
		if values := md.Get("x-auth-token"); len(values) > 0 {
			return values[0], nil
		}
	}

	// No token found - return empty string (not an error for optional auth)
	return "", nil
}

// ExtractTenantFromContext extracts the tenant ID from context metadata.
// This is an exported function for use by services that need tenant information.
// Returns an empty string if no tenant header is found.
func ExtractTenantFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	tenants := md.Get("x-tenant-id")
	if len(tenants) == 0 {
		return ""
	}
	return tenants[0]
}

// parseGRPCMethod extracts resource and action from gRPC method name.
func parseGRPCMethod(fullMethod string) (resource, action string) {
	// fullMethod format: /package.Service/Method
	// Example: /quicbaas.v1.QuicBaaS/Get

	parts := strings.Split(fullMethod, "/")
	if len(parts) < 3 {
		return ResourceAll, ActionAll
	}

	servicePart := parts[1] // e.g., "quicbaas.v1.QuicBaaS" or "quicbaas.v1.Cluster"
	method := parts[2]      // e.g., "Get", "Put", etc.

	// Determine resource based on service
	resource, action = mapMethodToResourceAction(servicePart, method)
	return resource, action
}

// mapMethodToResourceAction maps a gRPC service and method to a resource and action.
func mapMethodToResourceAction(service, method string) (resource, action string) {
	// Handle service-specific routing
	switch {
	case strings.HasSuffix(service, ".Cluster"):
		return interfaces.ResourceCluster, interfaces.ActionAdmin

	case strings.HasSuffix(service, ".KMS"):
		return mapKMSMethod(method)

	case strings.HasSuffix(service, ".Secrets"):
		return mapSecretsMethod(method)

	case strings.HasSuffix(service, ".Crypto"):
		return mapCryptoMethod(method)

	case strings.HasSuffix(service, ".Seal"):
		return interfaces.ResourceSeal, interfaces.ActionAdmin

	case strings.HasSuffix(service, ".TimeSeries"):
		return mapTimeSeriesMethod(method)

	case strings.HasSuffix(service, ".Vector"),
		strings.HasSuffix(service, ".VectorDB"),
		strings.HasSuffix(service, ".VectorAdmin"):
		// Match Vector, VectorDB and VectorAdmin services
		return mapVectorMethod(method)

	case strings.HasSuffix(service, ".ObjectStorage"),
		strings.HasSuffix(service, ".ObjectStorageService"):
		// Match Object Storage services
		return mapObjectStorageMethod(method)

	case strings.HasSuffix(service, ".Catalog"),
		strings.HasSuffix(service, ".CatalogService"):
		// Match Catalog services
		return mapCatalogMethod(method)

	case strings.HasSuffix(service, ".Health"):
		return interfaces.ResourceHealth, interfaces.ActionRead

	default:
		// Default QuicBaaS service methods
		return mapQuicBaaSMethod(method)
	}
}

// mapQuicBaaSMethod maps QuicBaaS service methods to resource/action.
func mapQuicBaaSMethod(method string) (resource, action string) {
	switch method {
	// Key read operations
	case "Get", "Range", "List", "Query", "ScanIndex", "BatchGet":
		return interfaces.ResourceKeys, interfaces.ActionRead

	// Key write operations
	case "Put", "BatchPut":
		return interfaces.ResourceKeys, interfaces.ActionWrite

	// Key delete operations
	case "Delete", "BatchDelete":
		return interfaces.ResourceKeys, interfaces.ActionDelete

	// Transaction operations
	case "Txn", "Transaction":
		return interfaces.ResourceTransactions, interfaces.ActionWrite

	// Cluster operations
	case "ClusterStatus", "GetLeader":
		return interfaces.ResourceCluster, interfaces.ActionRead

	// Watch operations
	case "Watch":
		return interfaces.ResourceWatch, interfaces.ActionRead

	// Health operations
	case "Health":
		return interfaces.ResourceHealth, interfaces.ActionRead

	// Aggregation operations
	case "Aggregate":
		return interfaces.ResourceKeys, interfaces.ActionRead

	// Login is public but included for completeness
	case "Login":
		return interfaces.ResourceUsers, interfaces.ActionRead

	// Default to requiring admin access for unknown methods
	default:
		return interfaces.ResourceAll, interfaces.ActionAdmin
	}
}

// mapKMSMethod maps KMS service methods to resource/action.
func mapKMSMethod(method string) (resource, action string) {
	switch method {
	case "GetKey", "ListKeys", "GetKeyVersion", "GetKeyVersions":
		return interfaces.ResourceKMS, interfaces.ActionRead
	case "CreateKey", "RotateKey", "ImportKey":
		return interfaces.ResourceKMS, interfaces.ActionWrite
	case "DeleteKey", "DeleteKeyVersion":
		return interfaces.ResourceKMS, interfaces.ActionDelete
	case "EnableKey", "DisableKey":
		return interfaces.ResourceKMS, interfaces.ActionAdmin
	default:
		return interfaces.ResourceKMS, interfaces.ActionAdmin
	}
}

// mapSecretsMethod maps Secrets service methods to resource/action.
func mapSecretsMethod(method string) (resource, action string) {
	switch method {
	case "GetSecret", "ListSecrets", "GetSecretVersion", "ListSecretVersions":
		return interfaces.ResourceSecrets, interfaces.ActionRead
	case "CreateSecret", "UpdateSecret", "SetSecret":
		return interfaces.ResourceSecrets, interfaces.ActionWrite
	case "DeleteSecret", "DeleteSecretVersion":
		return interfaces.ResourceSecrets, interfaces.ActionDelete
	default:
		return interfaces.ResourceSecrets, interfaces.ActionAdmin
	}
}

// mapCryptoMethod maps Crypto service methods to resource/action.
func mapCryptoMethod(method string) (resource, action string) {
	switch method {
	case "Encrypt", "Decrypt", "Sign", "Verify", "Hash":
		return interfaces.ResourceCrypto, interfaces.ActionWrite
	case "GenerateRandom":
		return interfaces.ResourceCrypto, interfaces.ActionRead
	default:
		return interfaces.ResourceCrypto, interfaces.ActionAdmin
	}
}

// mapTimeSeriesMethod maps TimeSeries service methods to resource/action.
func mapTimeSeriesMethod(method string) (resource, action string) {
	switch method {
	case "Query", "Range", "Get", "List":
		return interfaces.ResourceTimeseries, interfaces.ActionRead
	case "Write", "BatchWrite", "Put":
		return interfaces.ResourceTimeseries, interfaces.ActionWrite
	case "Delete", "DeleteRange":
		return interfaces.ResourceTimeseries, interfaces.ActionDelete
	default:
		return interfaces.ResourceTimeseries, interfaces.ActionAdmin
	}
}

// mapVectorMethod maps Vector service methods to resource/action with fine-grained permissions.
// This supports both generic actions (for backward compatibility) and vector-specific actions.
func mapVectorMethod(method string) (resource, action string) {
	switch method {
	// Read operations - vector:read
	case "GetVector", "Get", "ListVectors", "List":
		return interfaces.ResourceVector, interfaces.ActionVectorRead

	// Search operations - vector:search (separate from basic read for fine-grained control)
	case "SearchSimilar", "Search", "SearchByID", "HybridSearch", "SearchByMetadata":
		return interfaces.ResourceVector, interfaces.ActionVectorSearch

	// Write operations - vector:write
	case "UpsertVector", "Upsert", "Put", "BatchUpsertVectors", "BatchUpsert", "UpdateMetadata":
		return interfaces.ResourceVector, interfaces.ActionVectorWrite

	// Delete operations - vector:delete
	case "DeleteVector", "Delete", "BatchDeleteVectors", "BatchDelete":
		return interfaces.ResourceVector, interfaces.ActionVectorDelete

	// Namespace operations - vector:namespace
	case "CreateNamespace", "DeleteNamespace", "UpdateNamespace", "GetNamespaceInfo":
		return interfaces.ResourceVector, interfaces.ActionVectorNamespace

	// ListNamespaces is a read operation that should be accessible by readers
	case "ListNamespaces":
		return interfaces.ResourceVector, interfaces.ActionVectorRead

	// Stats and health operations - vector:stats
	case "Health", "GetVectorStats", "VectorHealth":
		return interfaces.ResourceVector, interfaces.ActionVectorStats

	// Admin operations - vector:admin (index management, export/import, etc.)
	case "RebuildIndex", "GetIndexProgress", "CompactNamespace", "ExportVectors", "ImportVectors":
		return interfaces.ResourceVector, interfaces.ActionVectorAdmin

	// Default to admin for unknown methods
	default:
		return interfaces.ResourceVector, interfaces.ActionVectorAdmin
	}
}

// mapObjectStorageMethod maps ObjectStorage service methods to resource/action.
func mapObjectStorageMethod(method string) (resource, action string) {
	switch method {
	// Object read operations
	case "GetObject", "HeadObject", "DownloadObject":
		return interfaces.ResourceObject, interfaces.ActionObjectGet

	// Object list operations
	case "ListObjects", "ListObjectVersions":
		return interfaces.ResourceObject, interfaces.ActionObjectList

	// Object write operations
	case "PutObject", "UploadObject", "CopyObject", "UpdateObjectMetadata":
		return interfaces.ResourceObject, interfaces.ActionObjectPut

	// Object delete operations
	case "DeleteObject", "DeleteObjects", "BatchDeleteObjects":
		return interfaces.ResourceObject, interfaces.ActionObjectDelete

	// Object replication operations
	case "ReplicateObject", "StartReplication":
		return interfaces.ResourceObject, interfaces.ActionObjectReplicate

	// Object migration operations
	case "MigrateObject", "StartMigration":
		return interfaces.ResourceObject, interfaces.ActionObjectMigrate

	// Object archive operations
	case "ArchiveObject", "RestoreObject":
		return interfaces.ResourceObject, interfaces.ActionObjectArchive

	// Object verification operations
	case "VerifyObject", "VerifyIntegrity":
		return interfaces.ResourceObject, interfaces.ActionObjectVerify

	// Object policy operations
	case "SetObjectPolicy", "GetObjectPolicy", "DeleteObjectPolicy":
		return interfaces.ResourceObject, interfaces.ActionObjectSetPolicy

	// Object ACL operations
	case "SetObjectACL", "GetObjectACL", "GrantAccess", "RevokeAccess":
		return interfaces.ResourceObject, interfaces.ActionObjectSetACL

	// Bucket operations
	case "CreateBucket", "CreateNamespace":
		return interfaces.ResourceBucket, interfaces.ActionBucketCreate
	case "DeleteBucket", "DeleteNamespace":
		return interfaces.ResourceBucket, interfaces.ActionBucketDelete
	case "ListBuckets", "ListNamespaces":
		return interfaces.ResourceBucket, interfaces.ActionBucketList
	case "SetBucketPolicy", "GetBucketPolicy":
		return interfaces.ResourceBucket, interfaces.ActionBucketSetPolicy

	// Health and stats
	case "Health", "GetStorageStats":
		return interfaces.ResourceObjectStore, interfaces.ActionRead

	// Default to admin for unknown methods
	default:
		return interfaces.ResourceObjectStore, interfaces.ActionAdmin
	}
}

// mapCatalogMethod maps Catalog service methods to resource/action.
func mapCatalogMethod(method string) (resource, action string) {
	switch method {
	// Catalog read operations
	case "GetEntry", "GetCatalogEntry", "ListEntries", "SearchEntries":
		return interfaces.ResourceCatalog, interfaces.ActionRead

	// Catalog write operations
	case "PutEntry", "UpdateEntry", "CreateEntry":
		return interfaces.ResourceCatalog, interfaces.ActionWrite

	// Catalog delete operations
	case "DeleteEntry", "PurgeEntry":
		return interfaces.ResourceCatalog, interfaces.ActionDelete

	// Catalog admin operations
	case "RebuildIndexes", "CompactCatalog", "SyncCatalog":
		return interfaces.ResourceCatalog, interfaces.ActionAdmin

	// Default to admin for unknown methods
	default:
		return interfaces.ResourceCatalog, interfaces.ActionAdmin
	}
}

// AddTokenToContext adds an authentication token to the context metadata.
func AddTokenToContext(ctx context.Context, token string) context.Context {
	// Add Bearer prefix if not present
	if !strings.HasPrefix(token, "Bearer ") {
		token = "Bearer " + token
	}

	md := metadata.Pairs("authorization", token)
	return metadata.NewOutgoingContext(ctx, md)
}
