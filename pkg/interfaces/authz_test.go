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

package interfaces

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPermissionWireValues pins the resource and action strings policies store.
func TestPermissionWireValues(t *testing.T) {
	// Role policies store these strings, so a changed value silently
	// revokes or widens every grant that names it.
	tests := []struct {
		got  string
		want string
	}{
		{ResourceKeys, "keys"},
		{ResourceTransactions, "transactions"},
		{ResourceCluster, "cluster"},
		{ResourceBackups, "backups"},
		{ResourceUsers, "users"},
		{ResourceRoles, "roles"},
		{ResourceSecrets, "secrets"},
		{ResourceKMS, "kms"},
		{ResourceCrypto, "crypto"},
		{ResourceSeal, "seal"},
		{ResourceHealth, "health"},
		{ResourceMetrics, "metrics"},
		{ResourceWatch, "watch"},
		{ResourceTimeseries, "timeseries"},
		{ResourceVector, "vector"},
		{ResourceAll, "*"},
		{ResourceObjectStore, "objstore"},
		{ResourceBucket, "bucket"},
		{ResourceObject, "object"},
		{ResourceCatalog, "catalog"},
		{ResourceCertificate, "certificate"},
		{ResourceIAM, "iam"},
		{ResourceIAMTenant, "iam:tenant"},
		{ResourceIAMUser, "iam:user"},
		{ResourceIAMRole, "iam:role"},
		{ResourceIAMPolicy, "iam:policy"},
		{ResourceIAMApp, "iam:app"},
		{ResourceParameters, "parameters"},

		{ActionRead, "read"},
		{ActionWrite, "write"},
		{ActionDelete, "delete"},
		{ActionAdmin, "admin"},
		{ActionAll, "*"},
		{ActionVectorRead, "vector:read"},
		{ActionVectorWrite, "vector:write"},
		{ActionVectorDelete, "vector:delete"},
		{ActionVectorAdmin, "vector:admin"},
		{ActionVectorNamespace, "vector:namespace"},
		{ActionVectorSearch, "vector:search"},
		{ActionVectorStats, "vector:stats"},
		{ActionObjectPut, "object:put"},
		{ActionObjectGet, "object:get"},
		{ActionObjectDelete, "object:delete"},
		{ActionObjectList, "object:list"},
		{ActionObjectReplicate, "object:replicate"},
		{ActionObjectMigrate, "object:migrate"},
		{ActionObjectArchive, "object:archive"},
		{ActionObjectVerify, "object:verify"},
		{ActionObjectSetPolicy, "object:set_policy"},
		{ActionObjectSetACL, "object:set_acl"},
		{ActionBucketCreate, "bucket:create"},
		{ActionBucketDelete, "bucket:delete"},
		{ActionBucketList, "bucket:list"},
		{ActionBucketSetPolicy, "bucket:set_policy"},
		{ActionParameterRead, "parameter:read"},
		{ActionParameterWrite, "parameter:write"},
		{ActionParameterDelete, "parameter:delete"},
		{ActionParameterList, "parameter:list"},
		{ActionParameterAdmin, "parameter:admin"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.got)
	}
}


// TestVectorNamespaceResource tests the VectorNamespaceResource function.
func TestVectorNamespaceResource(t *testing.T) {
	tests := []struct {
		namespace string
		expected  string
	}{
		{"embeddings", "vector:namespace:embeddings"},
		{"prod-vectors", "vector:namespace:prod-vectors"},
		{"test", "vector:namespace:test"},
		{"", "vector:namespace:"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			result := VectorNamespaceResource(tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsVectorNamespaceResource tests the IsVectorNamespaceResource function.
func TestIsVectorNamespaceResource(t *testing.T) {
	tests := []struct {
		resource string
		expected bool
	}{
		{"vector:namespace:embeddings", true},
		{"vector:namespace:prod-vectors", true},
		{"vector:namespace:", false}, // Empty namespace name
		{"vector:namespace", false},  // Missing colon
		{"vector", false},
		{"keys", false},
		{"", false},
		{"vector:namespace:*", true},
	}

	for _, tt := range tests {
		name := tt.resource
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			result := IsVectorNamespaceResource(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractVectorNamespace tests the ExtractVectorNamespace function.
func TestExtractVectorNamespace(t *testing.T) {
	tests := []struct {
		resource string
		expected string
	}{
		{"vector:namespace:embeddings", "embeddings"},
		{"vector:namespace:prod-vectors", "prod-vectors"},
		{"vector:namespace:test-ns-123", "test-ns-123"},
		{"vector:namespace:*", "*"},
		{"vector:namespace:", ""}, // Empty namespace
		{"vector:namespace", ""},  // Not a valid namespace resource
		{"vector", ""},            // Not a namespace resource
		{"keys", ""},              // Not a vector resource
		{"", ""},                  // Empty string
	}

	for _, tt := range tests {
		name := tt.resource
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			result := ExtractVectorNamespace(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesVectorNamespace tests the MatchesVectorNamespace function.
func TestMatchesVectorNamespace(t *testing.T) {
	tests := []struct {
		name         string
		permResource string
		requestedNS  string
		expected     bool
	}{
		// Wildcard resources
		{"all resources wildcard", ResourceAll, "embeddings", true},
		{"vector resource grants all namespaces", ResourceVector, "embeddings", true},
		{"namespace wildcard grants all", ResourceVectorNamespaceWildcard, "embeddings", true},
		{"namespace wildcard grants any ns", ResourceVectorNamespaceWildcard, "prod-vectors", true},

		// Exact matches
		{"exact namespace match", "vector:namespace:embeddings", "embeddings", true},
		{"exact namespace no match", "vector:namespace:embeddings", "other", false},

		// Prefix wildcard matching
		{"prefix wildcard match", "vector:namespace:prod-*", "prod-embeddings", true},
		{"prefix wildcard match 2", "vector:namespace:prod-*", "prod-vectors", true},
		{"prefix wildcard no match", "vector:namespace:prod-*", "dev-embeddings", false},
		{"prefix wildcard partial name", "vector:namespace:embed*", "embeddings", true},
		{"prefix wildcard no match short", "vector:namespace:prod-*", "pro", false},
		{"prefix with underscore", "vector:namespace:test_*", "test_namespace", true},

		// Edge inputs
		{"namespace wildcard grants empty ns", "vector:namespace:*", "", true},
		{"single char namespace", "vector:namespace:a", "a", true},
		{"single char namespace no match", "vector:namespace:a", "b", false},
		{"namespace with special chars", "vector:namespace:ns-with_special.chars", "ns-with_special.chars", true},

		// Non-namespace resources
		{"non-namespace resource", "keys", "embeddings", false},
		{"empty permission resource", "", "embeddings", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesVectorNamespace(tt.permResource, tt.requestedNS)
			assert.Equal(t, tt.expected, result, "permResource=%q, requestedNS=%q", tt.permResource, tt.requestedNS)
		})
	}
}

// TestObjectBucketResource tests the ObjectBucketResource function.
func TestObjectBucketResource(t *testing.T) {
	tests := []struct {
		name     string
		bucket   string
		expected string
	}{
		{"simple bucket", "my-bucket", "object:bucket:my-bucket"},
		{"production bucket", "prod-data", "object:bucket:prod-data"},
		{"bucket with numbers", "bucket-123", "object:bucket:bucket-123"},
		{"empty bucket", "", "object:bucket:"},
		{"bucket with dashes", "my-data-bucket", "object:bucket:my-data-bucket"},
		{"bucket with underscores", "my_data_bucket", "object:bucket:my_data_bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ObjectBucketResource(tt.bucket)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsObjectBucketResource tests the IsObjectBucketResource function.
func TestIsObjectBucketResource(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		expected bool
	}{
		{"valid bucket resource", "object:bucket:my-bucket", true},
		{"prod bucket resource", "object:bucket:prod-data", true},
		{"bucket wildcard", "object:bucket:*", true},
		{"bucket with prefix pattern", "object:bucket:prod-*", true},
		{"empty bucket name", "object:bucket:", false},
		{"missing bucket prefix", "object:bucket", false},
		{"just object", "object", false},
		{"keys resource", "keys", false},
		{"empty string", "", false},
		{"vector namespace", "vector:namespace:test", false},
		{"partial prefix match", "object:bucke", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsObjectBucketResource(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractObjectBucket tests the ExtractObjectBucket function.
func TestExtractObjectBucket(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		expected string
	}{
		{"simple bucket", "object:bucket:my-bucket", "my-bucket"},
		{"prod bucket", "object:bucket:prod-data", "prod-data"},
		{"bucket with numbers", "object:bucket:bucket-123", "bucket-123"},
		{"bucket wildcard", "object:bucket:*", "*"},
		{"prefix pattern", "object:bucket:prod-*", "prod-*"},
		{"empty bucket name", "object:bucket:", ""},
		{"invalid resource missing colon", "object:bucket", ""},
		{"just object", "object", ""},
		{"keys resource", "keys", ""},
		{"empty string", "", ""},
		{"vector namespace resource", "vector:namespace:test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractObjectBucket(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesObjectBucket tests the MatchesObjectBucket function.
func TestMatchesObjectBucket(t *testing.T) {
	tests := []struct {
		name            string
		permResource    string
		requestedBucket string
		expected        bool
	}{
		// Global wildcards
		{"all resources wildcard", ResourceAll, "my-bucket", true},
		{"object resource grants all buckets", ResourceObject, "my-bucket", true},
		{"objstore resource grants all buckets", ResourceObjectStore, "any-bucket", true},
		{"bucket wildcard grants all", ResourceObjectBucketWildcard, "prod-data", true},
		{"bucket wildcard grants any bucket", ResourceObjectBucketWildcard, "dev-logs", true},

		// Exact matches
		{"exact bucket match", "object:bucket:my-bucket", "my-bucket", true},
		{"exact bucket no match", "object:bucket:my-bucket", "other-bucket", false},
		{"exact bucket prod match", "object:bucket:prod-data", "prod-data", true},

		// Prefix wildcard matching
		{"prefix wildcard match", "object:bucket:prod-*", "prod-data", true},
		{"prefix wildcard match logs", "object:bucket:prod-*", "prod-logs", true},
		{"prefix wildcard no match", "object:bucket:prod-*", "dev-data", false},
		{"prefix wildcard partial name", "object:bucket:data*", "data-bucket", true},
		{"prefix wildcard no match short", "object:bucket:prod-*", "pro", false},
		{"prefix wildcard exact prefix", "object:bucket:prod-*", "prod-", true},
		{"prefix with underscore", "object:bucket:test_*", "test_bucket", true},

		// Edge inputs
		{"bucket wildcard grants empty bucket", "object:bucket:*", "", true},
		{"single char bucket", "object:bucket:a", "a", true},
		{"single char bucket no match", "object:bucket:a", "b", false},
		{"bucket with special chars", "object:bucket:bucket-with_special.chars", "bucket-with_special.chars", true},

		// Non-bucket resources
		{"keys resource", "keys", "my-bucket", false},
		{"vector resource", ResourceVector, "my-bucket", false},
		{"empty permission resource", "", "my-bucket", false},
		{"vector namespace resource", "vector:namespace:test", "my-bucket", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesObjectBucket(tt.permResource, tt.requestedBucket)
			assert.Equal(t, tt.expected, result, "permResource=%q, requestedBucket=%q", tt.permResource, tt.requestedBucket)
		})
	}
}

// TestParameterPathResource tests the ParameterPathResource function.
func TestParameterPathResource(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple path", "/app/config", "parameter:path:/app/config"},
		{"database host path", "/app/database/host", "parameter:path:/app/database/host"},
		{"root path", "/", "parameter:path:/"},
		{"empty path", "", "parameter:path:"},
		{"nested path", "/app/service/api/key", "parameter:path:/app/service/api/key"},
		{"path with numbers", "/app/v1/config", "parameter:path:/app/v1/config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParameterPathResource(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsParameterPathResource tests the IsParameterPathResource function.
func TestIsParameterPathResource(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		expected bool
	}{
		{"valid parameter path", "parameter:path:/app/config", true},
		{"parameter path wildcard", "parameter:path:*", true},
		{"parameter path multi-level wildcard", "parameter:path:/app/**", true},
		{"parameter path single-level wildcard", "parameter:path:/app/*", true},
		{"empty path name", "parameter:path:", false},
		{"missing path prefix", "parameter:path", false},
		{"just parameter", "parameter", false},
		{"keys resource", "keys", false},
		{"empty string", "", false},
		{"vector namespace", "vector:namespace:test", false},
		{"object bucket", "object:bucket:test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsParameterPathResource(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractParameterPath tests the ExtractParameterPath function.
func TestExtractParameterPath(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		expected string
	}{
		{"simple path", "parameter:path:/app/config", "/app/config"},
		{"database path", "parameter:path:/app/database/host", "/app/database/host"},
		{"wildcard", "parameter:path:*", "*"},
		{"multi-level wildcard", "parameter:path:/app/**", "/app/**"},
		{"single-level wildcard", "parameter:path:/app/*", "/app/*"},
		{"root path", "parameter:path:/", "/"},
		{"empty path name", "parameter:path:", ""},
		{"invalid resource missing colon", "parameter:path", ""},
		{"just parameter", "parameter", ""},
		{"keys resource", "keys", ""},
		{"empty string", "", ""},
		{"object bucket resource", "object:bucket:test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractParameterPath(tt.resource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesParameterPath tests the MatchesParameterPath function.
func TestMatchesParameterPath(t *testing.T) {
	tests := []struct {
		name          string
		permResource  string
		requestedPath string
		expected      bool
	}{
		// Global wildcards
		{"all resources wildcard", ResourceAll, "/app/config", true},
		{"parameters resource grants all paths", ResourceParameters, "/app/config", true},
		{"parameter path wildcard grants all", ResourceParameterPathWildcard, "/app/database/host", true},
		{"parameter path wildcard grants any path", ResourceParameterPathWildcard, "/other/path", true},

		// Exact matches
		{"exact path match", "parameter:path:/app/config", "/app/config", true},
		{"exact path no match", "parameter:path:/app/config", "/other/config", false},
		{"exact nested path match", "parameter:path:/app/database/host", "/app/database/host", true},

		// Multi-level wildcard matching (**)
		{"multi-level wildcard match", "parameter:path:/app/**", "/app/database/host", true},
		{"multi-level wildcard match nested", "parameter:path:/app/**", "/app/service/api/key", true},
		{"multi-level wildcard match single level", "parameter:path:/app/**", "/app/config", true},
		{"multi-level wildcard no match", "parameter:path:/app/**", "/other/config", false},
		{"multi-level wildcard at root", "parameter:path:/**", "/any/path/here", true},

		// Single-level wildcard matching (*)
		{"single-level wildcard match", "parameter:path:/app/*", "/app/config", true},
		{"single-level wildcard match database", "parameter:path:/app/*", "/app/database", true},
		{"single-level wildcard no match nested", "parameter:path:/app/*", "/app/database/host", false},
		{"single-level wildcard no match other", "parameter:path:/app/*", "/other/config", false},
		{"single-level wildcard exact prefix", "parameter:path:/app/*", "/app/", true},
		// Test case where requested path is shorter than the wildcard prefix
		{"single-level wildcard path shorter than prefix", "parameter:path:/app/config/*", "/app/", false},
		{"single-level wildcard with very long prefix", "parameter:path:/very/long/prefix/*", "/short", false},
		{"multi-level wildcard empty remaining", "parameter:path:/app/**", "/app/", true},
		{"multi-level wildcard deep nested", "parameter:path:/**", "/a/b/c/d/e/f/g", true},

		// Edge inputs
		{"path wildcard grants empty path", "parameter:path:*", "", true},
		{"single char path", "parameter:path:/a", "/a", true},
		{"single char path no match", "parameter:path:/a", "/b", false},
		{"path with special chars", "parameter:path:/app/config-v1.2", "/app/config-v1.2", true},

		// Non-parameter resources
		{"keys resource", "keys", "/app/config", false},
		{"vector resource", ResourceVector, "/app/config", false},
		{"empty permission resource", "", "/app/config", false},
		{"object bucket resource", "object:bucket:test", "/app/config", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesParameterPath(tt.permResource, tt.requestedPath)
			assert.Equal(t, tt.expected, result, "permResource=%q, requestedPath=%q", tt.permResource, tt.requestedPath)
		})
	}
}

