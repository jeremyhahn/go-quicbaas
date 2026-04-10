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

// Package interfaces provides interface definitions for all major QuicBaaS components.
package interfaces

import (
	"context"
)

// Subject represents the entity making an authorization request.
// This includes identity information, roles, tenant context, and custom attributes
// that can be used for attribute-based access control (ABAC) decisions.
type Subject struct {
	// ID is the unique identifier for the subject (e.g., user ID, service account ID).
	ID string

	// Username is the human-readable name of the subject.
	Username string

	// TenantID identifies the tenant context for multi-tenant deployments.
	// Empty for single-tenant deployments.
	TenantID string

	// Roles contains the RBAC roles assigned to the subject.
	Roles []string

	// Groups contains the groups the subject belongs to.
	Groups []string

	// Attributes contains custom attributes for ABAC policy evaluation.
	// Example: {"department": "engineering", "clearance_level": "secret"}
	Attributes map[string]interface{}

	// Authenticated indicates whether the subject has been authenticated.
	// False for anonymous/unauthenticated requests.
	Authenticated bool
}

// AuthorizationRequest represents a request for authorization.
// It contains the subject (who), resource (what), action (how), and context.
type AuthorizationRequest struct {
	// Subject is the entity making the request.
	Subject Subject

	// Resource is the resource being accessed (e.g., "keys", "secrets", "cluster").
	Resource string

	// Action is the action being performed (e.g., "read", "write", "delete", "admin").
	Action string

	// Context contains additional contextual information for policy evaluation.
	// Example: {"ip_address": "192.168.1.100", "time": "2025-01-15T10:30:00Z"}
	Context map[string]interface{}
}

// AuthorizationDecision represents the result of an authorization check.
type AuthorizationDecision struct {
	// Allowed indicates whether the request is authorized.
	Allowed bool

	// Reason provides a human-readable explanation for the decision.
	// Especially useful for deny decisions to aid debugging.
	Reason string

	// Provider indicates which authorization provider made the decision.
	// Values: "rbac", "abac", "hybrid"
	Provider string

	// MatchedPolicy is the name/identifier of the policy that matched.
	// For RBAC: role name (e.g., "admin", "readwrite")
	// For ABAC: policy name (e.g., "engineering-data-access")
	MatchedPolicy string

	// DecisionID is a unique identifier for this decision (for audit purposes).
	DecisionID string

	// EvaluationTimeNs is the time taken to evaluate the request in nanoseconds.
	EvaluationTimeNs int64
}

// AuthorizationProvider defines the interface for authorization backends.
// Implementations include RBAC (role-based), ABAC (attribute-based), and Hybrid providers.
//
// Implementations must be thread-safe and support concurrent authorization checks.
//
// Performance targets:
//   - RBAC: < 1us per check (simple map lookup)
//   - ABAC: < 5us per check (with compiled policy cache)
type AuthorizationProvider interface {
	// Authorize checks if the request is allowed.
	// Returns a decision with allow/deny, reason, and metadata.
	//
	// Context is used for cancellation and deadline propagation.
	// Implementations should respect context cancellation.
	Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)

	// BatchAuthorize performs multiple authorization checks efficiently.
	// This is useful for bulk operations where multiple resources need authorization.
	//
	// Returns a slice of decisions corresponding to each request.
	// If an individual check fails, that decision will contain the error info.
	BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)

	// GetProviderType returns the provider type identifier.
	// Values: "rbac", "abac", "hybrid"
	GetProviderType() string

	// Close releases any resources held by the provider.
	// This should be called when the provider is no longer needed.
	Close() error
}

// AuthorizationService provides unified authorization across all protocols.
// It wraps an AuthorizationProvider with additional capabilities:
//   - Audit logging of all authorization decisions
//   - Prometheus metrics collection
//   - Decision caching (optional)
//
// All protocol handlers (REST, gRPC, QUIC, MCP) should use this service
// rather than calling providers directly.
type AuthorizationService interface {
	// Authorize performs a single authorization check.
	// This method logs the decision for audit purposes and records metrics.
	Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)

	// BatchAuthorize performs multiple authorization checks.
	// Each decision is logged individually for audit purposes.
	BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)

	// GetProvider returns the underlying authorization provider.
	// This can be used to access provider-specific functionality.
	GetProvider() AuthorizationProvider

	// GetProviderType returns the type of the underlying provider.
	// Convenience method equivalent to GetProvider().GetProviderType().
	GetProviderType() string

	// Close releases any resources held by the service and provider.
	Close() error
}

// Common resource names used across QuicBaaS.
// These constants ensure consistency between RBAC and ABAC configurations.
const (
	// ResourceKeys represents key-value data operations.
	ResourceKeys = "keys"

	// ResourceTransactions represents transaction operations.
	ResourceTransactions = "transactions"

	// ResourceCluster represents cluster management operations.
	ResourceCluster = "cluster"

	// ResourceBackups represents backup and restore operations.
	ResourceBackups = "backups"

	// ResourceUsers represents user management operations.
	ResourceUsers = "users"

	// ResourceRoles represents role management operations.
	ResourceRoles = "roles"

	// ResourceSecrets represents secrets management operations.
	ResourceSecrets = "secrets"

	// ResourceKMS represents KMS (Key Management Service) operations.
	ResourceKMS = "kms"

	// ResourceCrypto represents cryptographic operations (encrypt/decrypt/sign/verify).
	ResourceCrypto = "crypto"

	// ResourceSeal represents seal/unseal operations.
	ResourceSeal = "seal"

	// ResourceHealth represents health check operations.
	ResourceHealth = "health"

	// ResourceMetrics represents metrics access.
	ResourceMetrics = "metrics"

	// ResourceWatch represents watch/subscription operations.
	ResourceWatch = "watch"

	// ResourceTimeseries represents timeseries data operations.
	ResourceTimeseries = "timeseries"

	// ResourceVector represents vector database operations.
	ResourceVector = "vector"

	// ResourceAll represents all resources (wildcard).
	ResourceAll = "*"

	// ResourceObjectStore represents object storage service operations.
	ResourceObjectStore = "objstore"

	// ResourceBucket represents object namespace/bucket operations.
	ResourceBucket = "bucket"

	// ResourceObject represents individual object operations.
	ResourceObject = "object"

	// ResourceCatalog represents catalog operations.
	ResourceCatalog = "catalog"

	// ResourceCertificate represents certificate management operations (issue, sign, revoke, verify).
	ResourceCertificate = "certificate"

	// ResourceIAM represents IAM (Identity and Access Management) operations.
	ResourceIAM = "iam"

	// ResourceIAMTenant represents IAM tenant operations.
	ResourceIAMTenant = "iam:tenant"

	// ResourceIAMUser represents IAM user operations.
	ResourceIAMUser = "iam:user"

	// ResourceIAMGroup represents IAM group operations.
	ResourceIAMGroup = "iam:group"

	// ResourceIAMRole represents IAM role operations.
	ResourceIAMRole = "iam:role"

	// ResourceIAMPolicy represents IAM policy operations.
	ResourceIAMPolicy = "iam:policy"

	// ResourceIAMApp represents IAM application operations.
	ResourceIAMApp = "iam:app"

	// ResourceParameters represents parameter store operations.
	ResourceParameters = "parameters"

	// ResourceOrchestrator represents orchestrator operations (spawn, prompt, task, broadcast).
	ResourceOrchestrator = "orchestrator"
)

// Common action names used across QuicBaaS.
const (
	// ActionRead represents read operations (GET, LIST, QUERY).
	ActionRead = "read"

	// ActionWrite represents write operations (PUT, POST, UPDATE).
	ActionWrite = "write"

	// ActionDelete represents delete operations.
	ActionDelete = "delete"

	// ActionAdmin represents administrative operations.
	ActionAdmin = "admin"

	// ActionAll represents all actions (wildcard).
	ActionAll = "*"
)

// Vector-specific fine-grained actions.
// These provide more granular control over vector database operations.
const (
	// ActionVectorRead represents vector read operations (Get, List, Search, SearchByID).
	ActionVectorRead = "vector:read"

	// ActionVectorWrite represents vector write operations (Upsert, BatchUpsert, UpdateMetadata).
	ActionVectorWrite = "vector:write"

	// ActionVectorDelete represents vector delete operations (Delete, BatchDelete).
	ActionVectorDelete = "vector:delete"

	// ActionVectorAdmin represents vector administrative operations (RebuildIndex, Compact, Export/Import).
	ActionVectorAdmin = "vector:admin"

	// ActionVectorNamespace represents vector namespace operations (CreateNamespace, DeleteNamespace, UpdateNamespace).
	ActionVectorNamespace = "vector:namespace"

	// ActionVectorSearch represents vector search operations (SearchSimilar, HybridSearch, SearchByMetadata).
	ActionVectorSearch = "vector:search"

	// ActionVectorStats represents vector stats and health operations (Health, GetVectorStats).
	ActionVectorStats = "vector:stats"
)

// Object storage-specific fine-grained actions.
// These provide granular control over object storage operations.
const (
	// ActionObjectPut represents object put/upload operations.
	ActionObjectPut = "object:put"

	// ActionObjectGet represents object get/download operations.
	ActionObjectGet = "object:get"

	// ActionObjectDelete represents object delete operations.
	ActionObjectDelete = "object:delete"

	// ActionObjectList represents object list operations.
	ActionObjectList = "object:list"

	// ActionObjectReplicate represents object replication operations.
	ActionObjectReplicate = "object:replicate"

	// ActionObjectMigrate represents object migration operations.
	ActionObjectMigrate = "object:migrate"

	// ActionObjectArchive represents object archive operations.
	ActionObjectArchive = "object:archive"

	// ActionObjectVerify represents object verification operations.
	ActionObjectVerify = "object:verify"

	// ActionObjectSetPolicy represents object policy management operations.
	ActionObjectSetPolicy = "object:set_policy"

	// ActionObjectSetACL represents object ACL management operations.
	ActionObjectSetACL = "object:set_acl"

	// ActionBucketCreate represents bucket/namespace creation operations.
	ActionBucketCreate = "bucket:create"

	// ActionBucketDelete represents bucket/namespace deletion operations.
	ActionBucketDelete = "bucket:delete"

	// ActionBucketList represents bucket/namespace list operations.
	ActionBucketList = "bucket:list"

	// ActionBucketSetPolicy represents bucket policy management operations.
	ActionBucketSetPolicy = "bucket:set_policy"
)

// Namespace-level resource format prefix.
// Format: "vector:namespace:{namespace_name}".
const (
	// ResourceVectorNamespacePrefix is the prefix for namespace-level resources.
	ResourceVectorNamespacePrefix = "vector:namespace:"

	// ResourceVectorNamespaceWildcard represents all vector namespaces.
	ResourceVectorNamespaceWildcard = "vector:namespace:*"

	// ResourceObjectBucketPrefix is the prefix for object bucket-level resources.
	ResourceObjectBucketPrefix = "object:bucket:"

	// ResourceObjectBucketWildcard represents all object buckets.
	ResourceObjectBucketWildcard = "object:bucket:*"
)

// VectorNamespaceResource returns the resource string for a specific vector namespace.
// Example: VectorNamespaceResource("embeddings") returns "vector:namespace:embeddings".
func VectorNamespaceResource(namespace string) string {
	return ResourceVectorNamespacePrefix + namespace
}

// IsVectorNamespaceResource checks if a resource string represents a vector namespace.
func IsVectorNamespaceResource(resource string) bool {
	return len(resource) > len(ResourceVectorNamespacePrefix) &&
		resource[:len(ResourceVectorNamespacePrefix)] == ResourceVectorNamespacePrefix
}

// ExtractVectorNamespace extracts the namespace name from a namespace resource string.
// Returns empty string if the resource is not a valid namespace resource.
func ExtractVectorNamespace(resource string) string {
	if !IsVectorNamespaceResource(resource) {
		return ""
	}
	return resource[len(ResourceVectorNamespacePrefix):]
}

// MatchesVectorNamespace checks if a permission resource matches a requested namespace.
// Supports wildcard matching: "vector:namespace:*" matches all namespaces.
// Supports prefix matching: "vector:namespace:prod-*" matches "prod-embeddings", "prod-vectors", etc.
func MatchesVectorNamespace(permResource, requestedNamespace string) bool {
	// Check for global vector resource (matches all)
	if permResource == ResourceVector || permResource == ResourceAll {
		return true
	}

	// Check for namespace wildcard
	if permResource == ResourceVectorNamespaceWildcard {
		return true
	}

	// Check if it's a namespace resource
	if !IsVectorNamespaceResource(permResource) {
		return false
	}

	permNamespace := ExtractVectorNamespace(permResource)

	// Exact match
	if permNamespace == requestedNamespace {
		return true
	}

	// Prefix wildcard match (e.g., "prod-*" matches "prod-embeddings")
	if len(permNamespace) > 0 && permNamespace[len(permNamespace)-1] == '*' {
		prefix := permNamespace[:len(permNamespace)-1]
		return len(requestedNamespace) >= len(prefix) &&
			requestedNamespace[:len(prefix)] == prefix
	}

	return false
}

// ObjectBucketResource returns the resource string for a specific object bucket.
// Example: ObjectBucketResource("my-bucket") returns "object:bucket:my-bucket".
func ObjectBucketResource(bucket string) string {
	return ResourceObjectBucketPrefix + bucket
}

// IsObjectBucketResource checks if a resource string represents an object bucket.
func IsObjectBucketResource(resource string) bool {
	return len(resource) > len(ResourceObjectBucketPrefix) &&
		resource[:len(ResourceObjectBucketPrefix)] == ResourceObjectBucketPrefix
}

// ExtractObjectBucket extracts the bucket name from a bucket resource string.
// Returns empty string if the resource is not a valid bucket resource.
func ExtractObjectBucket(resource string) string {
	if !IsObjectBucketResource(resource) {
		return ""
	}
	return resource[len(ResourceObjectBucketPrefix):]
}

// MatchesObjectBucket checks if a permission resource matches a requested bucket.
// Supports wildcard matching: "object:bucket:*" matches all buckets.
// Supports prefix matching: "object:bucket:prod-*" matches "prod-data", "prod-logs", etc.
func MatchesObjectBucket(permResource, requestedBucket string) bool {
	// Check for global object resource (matches all)
	if permResource == ResourceObject || permResource == ResourceObjectStore || permResource == ResourceAll {
		return true
	}

	// Check for bucket wildcard
	if permResource == ResourceObjectBucketWildcard {
		return true
	}

	// Check if it's a bucket resource
	if !IsObjectBucketResource(permResource) {
		return false
	}

	permBucket := ExtractObjectBucket(permResource)

	// Exact match
	if permBucket == requestedBucket {
		return true
	}

	// Prefix wildcard match (e.g., "prod-*" matches "prod-data")
	if len(permBucket) > 0 && permBucket[len(permBucket)-1] == '*' {
		prefix := permBucket[:len(permBucket)-1]
		return len(requestedBucket) >= len(prefix) &&
			requestedBucket[:len(prefix)] == prefix
	}

	return false
}

// Parameter store-specific fine-grained actions.
// These provide granular control over parameter store operations.
const (
	// ActionParameterRead represents parameter read operations (Get, List).
	ActionParameterRead = "parameter:read"

	// ActionParameterWrite represents parameter write operations (Set, Update).
	ActionParameterWrite = "parameter:write"

	// ActionParameterDelete represents parameter delete operations.
	ActionParameterDelete = "parameter:delete"

	// ActionParameterList represents parameter list/search operations.
	ActionParameterList = "parameter:list"

	// ActionParameterAdmin represents parameter administrative operations.
	ActionParameterAdmin = "parameter:admin"
)

// Parameter path-level resource format prefix.
// Format: "parameter:path:{parameter_path}".
const (
	// ResourceParameterPathPrefix is the prefix for path-level parameter resources.
	ResourceParameterPathPrefix = "parameter:path:"

	// ResourceParameterPathWildcard represents all parameter paths.
	ResourceParameterPathWildcard = "parameter:path:*"
)

// ParameterPathResource returns the resource string for a specific parameter path.
// Example: ParameterPathResource("/app/database/host") returns "parameter:path:/app/database/host".
func ParameterPathResource(path string) string {
	return ResourceParameterPathPrefix + path
}

// IsParameterPathResource checks if a resource string represents a parameter path.
func IsParameterPathResource(resource string) bool {
	return len(resource) > len(ResourceParameterPathPrefix) &&
		resource[:len(ResourceParameterPathPrefix)] == ResourceParameterPathPrefix
}

// ExtractParameterPath extracts the path from a parameter path resource string.
// Returns empty string if the resource is not a valid parameter path resource.
func ExtractParameterPath(resource string) string {
	if !IsParameterPathResource(resource) {
		return ""
	}
	return resource[len(ResourceParameterPathPrefix):]
}

// MatchesParameterPath checks if a permission resource matches a requested parameter path.
// Supports wildcard matching:
//   - "parameter:path:*" matches all paths
//   - "parameter:path:/app/*" matches single-level wildcard
//   - "parameter:path:/app/**" matches multi-level wildcard (all nested paths)
//
// Examples:
//
//	MatchesParameterPath("parameter:path:/app/**", "/app/database/host") -> true
//	MatchesParameterPath("parameter:path:/app/*", "/app/database") -> true
//	MatchesParameterPath("parameter:path:/app/*", "/app/database/host") -> false
func MatchesParameterPath(permResource, requestedPath string) bool {
	// Check for global parameter resource (matches all)
	if permResource == ResourceParameters || permResource == ResourceAll {
		return true
	}

	// Check for parameter path wildcard
	if permResource == ResourceParameterPathWildcard {
		return true
	}

	// Check if it's a parameter path resource
	if !IsParameterPathResource(permResource) {
		return false
	}

	permPath := ExtractParameterPath(permResource)

	// Exact match
	if permPath == requestedPath {
		return true
	}

	// Multi-level wildcard match (e.g., "/app/**" matches "/app/database/host")
	if len(permPath) >= 3 && permPath[len(permPath)-2:] == "**" {
		prefix := permPath[:len(permPath)-2]
		return len(requestedPath) >= len(prefix) &&
			requestedPath[:len(prefix)] == prefix
	}

	// Single-level wildcard match (e.g., "/app/*" matches "/app/database" but not "/app/database/host")
	if len(permPath) > 0 && permPath[len(permPath)-1] == '*' {
		prefix := permPath[:len(permPath)-1]
		if len(requestedPath) < len(prefix) {
			return false
		}
		if requestedPath[:len(prefix)] != prefix {
			return false
		}
		// Check that there's no additional path separator after the prefix
		remaining := requestedPath[len(prefix):]
		for i := 0; i < len(remaining); i++ {
			if remaining[i] == '/' {
				return false
			}
		}
		return true
	}

	return false
}
