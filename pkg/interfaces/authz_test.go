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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAuthorizationProvider is a mock implementation for testing.
type mockAuthorizationProvider struct {
	authorizeFunc      func(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)
	batchAuthorizeFunc func(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)
	providerType       string
	closed             bool
}

func (m *mockAuthorizationProvider) Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error) {
	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req)
	}
	return &AuthorizationDecision{Allowed: true, Provider: m.providerType}, nil
}

func (m *mockAuthorizationProvider) BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error) {
	if m.batchAuthorizeFunc != nil {
		return m.batchAuthorizeFunc(ctx, reqs)
	}
	decisions := make([]*AuthorizationDecision, len(reqs))
	for i := range reqs {
		decisions[i] = &AuthorizationDecision{Allowed: true, Provider: m.providerType}
	}
	return decisions, nil
}

func (m *mockAuthorizationProvider) GetProviderType() string {
	if m.providerType == "" {
		return "mock"
	}
	return m.providerType
}

func (m *mockAuthorizationProvider) Close() error {
	m.closed = true
	return nil
}

// Compile-time check that mockAuthorizationProvider implements AuthorizationProvider.
var _ AuthorizationProvider = (*mockAuthorizationProvider)(nil)

// TestSubjectStruct tests the Subject struct.
func TestSubjectStruct(t *testing.T) {
	t.Run("empty subject", func(t *testing.T) {
		subject := Subject{}
		assert.Empty(t, subject.ID)
		assert.Empty(t, subject.Username)
		assert.Empty(t, subject.TenantID)
		assert.Empty(t, subject.Roles)
		assert.Empty(t, subject.Groups)
		assert.Nil(t, subject.Attributes)
		assert.False(t, subject.Authenticated)
	})

	t.Run("fully populated subject", func(t *testing.T) {
		subject := Subject{
			ID:       "user-123",
			Username: "alice",
			TenantID: "tenant-abc",
			Roles:    []string{"admin", "developer"},
			Groups:   []string{"engineering", "platform"},
			Attributes: map[string]interface{}{
				"department":      "engineering",
				"clearance_level": "secret",
			},
			Authenticated: true,
		}

		assert.Equal(t, "user-123", subject.ID)
		assert.Equal(t, "alice", subject.Username)
		assert.Equal(t, "tenant-abc", subject.TenantID)
		assert.Equal(t, []string{"admin", "developer"}, subject.Roles)
		assert.Equal(t, []string{"engineering", "platform"}, subject.Groups)
		assert.Equal(t, "engineering", subject.Attributes["department"])
		assert.Equal(t, "secret", subject.Attributes["clearance_level"])
		assert.True(t, subject.Authenticated)
	})
}

// TestAuthorizationRequestStruct tests the AuthorizationRequest struct.
func TestAuthorizationRequestStruct(t *testing.T) {
	t.Run("minimal request", func(t *testing.T) {
		req := AuthorizationRequest{
			Subject:  Subject{ID: "user-1"},
			Resource: ResourceKeys,
			Action:   ActionRead,
		}

		assert.Equal(t, "user-1", req.Subject.ID)
		assert.Equal(t, ResourceKeys, req.Resource)
		assert.Equal(t, ActionRead, req.Action)
		assert.Nil(t, req.Context)
	})

	t.Run("full request with context", func(t *testing.T) {
		req := AuthorizationRequest{
			Subject: Subject{
				ID:            "user-1",
				Username:      "alice",
				Roles:         []string{"readwrite"},
				Authenticated: true,
			},
			Resource: ResourceSecrets,
			Action:   ActionWrite,
			Context: map[string]interface{}{
				"ip_address": "192.168.1.100",
				"user_agent": "quicbaas-cli/1.0",
				"method":     "POST",
				"path":       "/api/v1/secrets/db-password",
			},
		}

		assert.Equal(t, "alice", req.Subject.Username)
		assert.Equal(t, ResourceSecrets, req.Resource)
		assert.Equal(t, ActionWrite, req.Action)
		assert.Equal(t, "192.168.1.100", req.Context["ip_address"])
	})
}

// TestAuthorizationDecisionStruct tests the AuthorizationDecision struct.
func TestAuthorizationDecisionStruct(t *testing.T) {
	t.Run("allow decision", func(t *testing.T) {
		decision := AuthorizationDecision{
			Allowed:          true,
			Provider:         "rbac",
			MatchedPolicy:    "admin",
			DecisionID:       "dec-123",
			EvaluationTimeNs: 500,
		}

		assert.True(t, decision.Allowed)
		assert.Empty(t, decision.Reason)
		assert.Equal(t, "rbac", decision.Provider)
		assert.Equal(t, "admin", decision.MatchedPolicy)
		assert.Equal(t, "dec-123", decision.DecisionID)
		assert.Equal(t, int64(500), decision.EvaluationTimeNs)
	})

	t.Run("deny decision with reason", func(t *testing.T) {
		decision := AuthorizationDecision{
			Allowed:          false,
			Reason:           "user does not have write permission on secrets",
			Provider:         "abac",
			MatchedPolicy:    "deny-external-access",
			DecisionID:       "dec-456",
			EvaluationTimeNs: 2500,
		}

		assert.False(t, decision.Allowed)
		assert.Equal(t, "user does not have write permission on secrets", decision.Reason)
		assert.Equal(t, "abac", decision.Provider)
	})
}

// TestMockAuthorizationProvider tests the mock provider implementation.
func TestMockAuthorizationProvider(t *testing.T) {
	t.Run("default authorize returns allowed", func(t *testing.T) {
		provider := &mockAuthorizationProvider{providerType: "test"}
		req := &AuthorizationRequest{
			Subject:  Subject{ID: "user-1"},
			Resource: ResourceKeys,
			Action:   ActionRead,
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "test", decision.Provider)
	})

	t.Run("custom authorize function", func(t *testing.T) {
		provider := &mockAuthorizationProvider{
			authorizeFunc: func(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error) {
				if req.Action == ActionAdmin {
					return &AuthorizationDecision{
						Allowed: false,
						Reason:  "admin access denied",
					}, nil
				}
				return &AuthorizationDecision{Allowed: true}, nil
			},
		}

		// Test allowed
		req := &AuthorizationRequest{
			Subject:  Subject{ID: "user-1"},
			Resource: ResourceKeys,
			Action:   ActionRead,
		}
		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, decision.Allowed)

		// Test denied
		req.Action = ActionAdmin
		decision, err = provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "admin access denied", decision.Reason)
	})

	t.Run("batch authorize", func(t *testing.T) {
		provider := &mockAuthorizationProvider{providerType: "batch-test"}
		reqs := []*AuthorizationRequest{
			{Subject: Subject{ID: "user-1"}, Resource: ResourceKeys, Action: ActionRead},
			{Subject: Subject{ID: "user-1"}, Resource: ResourceSecrets, Action: ActionWrite},
			{Subject: Subject{ID: "user-1"}, Resource: ResourceCluster, Action: ActionAdmin},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)
		require.NoError(t, err)
		require.Len(t, decisions, 3)
		for _, d := range decisions {
			assert.True(t, d.Allowed)
			assert.Equal(t, "batch-test", d.Provider)
		}
	})

	t.Run("get provider type", func(t *testing.T) {
		provider := &mockAuthorizationProvider{providerType: "rbac"}
		assert.Equal(t, "rbac", provider.GetProviderType())

		provider2 := &mockAuthorizationProvider{}
		assert.Equal(t, "mock", provider2.GetProviderType())
	})

	t.Run("close", func(t *testing.T) {
		provider := &mockAuthorizationProvider{}
		assert.False(t, provider.closed)

		err := provider.Close()
		require.NoError(t, err)
		assert.True(t, provider.closed)
	})
}

// TestResourceConstants tests that resource constants are defined correctly.
func TestResourceConstants(t *testing.T) {
	resources := []string{
		ResourceKeys,
		ResourceTransactions,
		ResourceCluster,
		ResourceBackups,
		ResourceUsers,
		ResourceRoles,
		ResourceSecrets,
		ResourceKMS,
		ResourceCrypto,
		ResourceSeal,
		ResourceHealth,
		ResourceMetrics,
		ResourceWatch,
		ResourceTimeseries,
		ResourceVector,
		ResourceAll,
	}

	// Verify all resources are non-empty and unique
	seen := make(map[string]bool)
	for _, r := range resources {
		assert.NotEmpty(t, r, "resource constant should not be empty")
		assert.False(t, seen[r], "resource %q appears more than once", r)
		seen[r] = true
	}

	// Verify specific values
	assert.Equal(t, "keys", ResourceKeys)
	assert.Equal(t, "secrets", ResourceSecrets)
	assert.Equal(t, "*", ResourceAll)
}

// TestActionConstants tests that action constants are defined correctly.
func TestActionConstants(t *testing.T) {
	actions := []string{
		ActionRead,
		ActionWrite,
		ActionDelete,
		ActionAdmin,
		ActionAll,
	}

	// Verify all actions are non-empty and unique
	seen := make(map[string]bool)
	for _, a := range actions {
		assert.NotEmpty(t, a, "action constant should not be empty")
		assert.False(t, seen[a], "action %q appears more than once", a)
		seen[a] = true
	}

	// Verify specific values
	assert.Equal(t, "read", ActionRead)
	assert.Equal(t, "write", ActionWrite)
	assert.Equal(t, "delete", ActionDelete)
	assert.Equal(t, "admin", ActionAdmin)
	assert.Equal(t, "*", ActionAll)
}

// TestContextCancellation tests that providers respect context cancellation.
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	provider := &mockAuthorizationProvider{
		authorizeFunc: func(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error) {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return &AuthorizationDecision{Allowed: true}, nil
			}
		},
	}

	req := &AuthorizationRequest{
		Subject:  Subject{ID: "user-1"},
		Resource: ResourceKeys,
		Action:   ActionRead,
	}

	_, err := provider.Authorize(ctx, req)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// TestVectorActionConstants tests that vector-specific action constants are defined correctly.
func TestVectorActionConstants(t *testing.T) {
	actions := []string{
		ActionVectorRead,
		ActionVectorWrite,
		ActionVectorDelete,
		ActionVectorAdmin,
		ActionVectorNamespace,
		ActionVectorSearch,
		ActionVectorStats,
	}

	// Verify all actions are non-empty and unique
	seen := make(map[string]bool)
	for _, a := range actions {
		assert.NotEmpty(t, a, "action constant should not be empty")
		assert.False(t, seen[a], "action %q appears more than once", a)
		seen[a] = true
	}

	// Verify specific values
	assert.Equal(t, "vector:read", ActionVectorRead)
	assert.Equal(t, "vector:write", ActionVectorWrite)
	assert.Equal(t, "vector:delete", ActionVectorDelete)
	assert.Equal(t, "vector:admin", ActionVectorAdmin)
	assert.Equal(t, "vector:namespace", ActionVectorNamespace)
	assert.Equal(t, "vector:search", ActionVectorSearch)
	assert.Equal(t, "vector:stats", ActionVectorStats)
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

// TestVectorNamespaceConstants tests the namespace constants.
func TestVectorNamespaceConstants(t *testing.T) {
	assert.Equal(t, "vector:namespace:", ResourceVectorNamespacePrefix)
	assert.Equal(t, "vector:namespace:*", ResourceVectorNamespaceWildcard)
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

// TestObjectBucketConstants tests the object bucket constants.
func TestObjectBucketConstants(t *testing.T) {
	assert.Equal(t, "object:bucket:", ResourceObjectBucketPrefix)
	assert.Equal(t, "object:bucket:*", ResourceObjectBucketWildcard)
}

// TestObjectActionConstants tests that object storage-specific action constants are defined correctly.
func TestObjectActionConstants(t *testing.T) {
	actions := []string{
		ActionObjectPut,
		ActionObjectGet,
		ActionObjectDelete,
		ActionObjectList,
		ActionObjectReplicate,
		ActionObjectMigrate,
		ActionObjectArchive,
		ActionObjectVerify,
		ActionObjectSetPolicy,
		ActionObjectSetACL,
		ActionBucketCreate,
		ActionBucketDelete,
		ActionBucketList,
		ActionBucketSetPolicy,
	}

	// Verify all actions are non-empty and unique
	seen := make(map[string]bool)
	for _, a := range actions {
		assert.NotEmpty(t, a, "action constant should not be empty")
		assert.False(t, seen[a], "action %q appears more than once", a)
		seen[a] = true
	}

	// Verify specific values
	assert.Equal(t, "object:put", ActionObjectPut)
	assert.Equal(t, "object:get", ActionObjectGet)
	assert.Equal(t, "object:delete", ActionObjectDelete)
	assert.Equal(t, "object:list", ActionObjectList)
	assert.Equal(t, "bucket:create", ActionBucketCreate)
	assert.Equal(t, "bucket:delete", ActionBucketDelete)
	assert.Equal(t, "bucket:list", ActionBucketList)
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

// TestParameterPathConstants tests the parameter path constants.
func TestParameterPathConstants(t *testing.T) {
	assert.Equal(t, "parameter:path:", ResourceParameterPathPrefix)
	assert.Equal(t, "parameter:path:*", ResourceParameterPathWildcard)
}

// TestParameterActionConstants tests that parameter-specific action constants are defined correctly.
func TestParameterActionConstants(t *testing.T) {
	actions := []string{
		ActionParameterRead,
		ActionParameterWrite,
		ActionParameterDelete,
		ActionParameterList,
		ActionParameterAdmin,
	}

	// Verify all actions are non-empty and unique
	seen := make(map[string]bool)
	for _, a := range actions {
		assert.NotEmpty(t, a, "action constant should not be empty")
		assert.False(t, seen[a], "action %q appears more than once", a)
		seen[a] = true
	}

	// Verify specific values
	assert.Equal(t, "parameter:read", ActionParameterRead)
	assert.Equal(t, "parameter:write", ActionParameterWrite)
	assert.Equal(t, "parameter:delete", ActionParameterDelete)
	assert.Equal(t, "parameter:list", ActionParameterList)
	assert.Equal(t, "parameter:admin", ActionParameterAdmin)
}

// TestAdditionalResourceConstants tests additional resource constants.
func TestAdditionalResourceConstants(t *testing.T) {
	resources := map[string]string{
		"ResourceObjectStore": ResourceObjectStore,
		"ResourceBucket":      ResourceBucket,
		"ResourceObject":      ResourceObject,
		"ResourceCatalog":     ResourceCatalog,
		"ResourceCertificate": ResourceCertificate,
		"ResourceIAM":         ResourceIAM,
		"ResourceIAMTenant":   ResourceIAMTenant,
		"ResourceIAMUser":     ResourceIAMUser,
		"ResourceIAMRole":     ResourceIAMRole,
		"ResourceIAMPolicy":   ResourceIAMPolicy,
		"ResourceIAMApp":      ResourceIAMApp,
		"ResourceParameters":  ResourceParameters,
	}

	// Verify all resources are non-empty
	for name, value := range resources {
		assert.NotEmpty(t, value, "%s should not be empty", name)
	}

	// Verify specific values
	assert.Equal(t, "objstore", ResourceObjectStore)
	assert.Equal(t, "bucket", ResourceBucket)
	assert.Equal(t, "object", ResourceObject)
	assert.Equal(t, "catalog", ResourceCatalog)
	assert.Equal(t, "certificate", ResourceCertificate)
	assert.Equal(t, "iam", ResourceIAM)
	assert.Equal(t, "iam:tenant", ResourceIAMTenant)
	assert.Equal(t, "iam:user", ResourceIAMUser)
	assert.Equal(t, "iam:role", ResourceIAMRole)
	assert.Equal(t, "iam:policy", ResourceIAMPolicy)
	assert.Equal(t, "iam:app", ResourceIAMApp)
	assert.Equal(t, "parameters", ResourceParameters)
}

// TestMatchesVectorNamespaceEdgeCases tests edge cases for MatchesVectorNamespace.
func TestMatchesVectorNamespaceEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		permResource string
		requestedNS  string
		expected     bool
	}{
		{"empty wildcard pattern", "vector:namespace:*", "", true},
		{"prefix wildcard with empty suffix", "vector:namespace:*", "anything", true},
		{"single char namespace", "vector:namespace:a", "a", true},
		{"single char namespace no match", "vector:namespace:a", "b", false},
		{"namespace with special chars", "vector:namespace:ns-with_special.chars", "ns-with_special.chars", true},
		{"prefix with underscore", "vector:namespace:test_*", "test_namespace", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesVectorNamespace(tt.permResource, tt.requestedNS)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesObjectBucketEdgeCases tests edge cases for MatchesObjectBucket.
func TestMatchesObjectBucketEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		permResource    string
		requestedBucket string
		expected        bool
	}{
		{"empty wildcard pattern", "object:bucket:*", "", true},
		{"prefix wildcard with empty suffix", "object:bucket:*", "anything", true},
		{"single char bucket", "object:bucket:a", "a", true},
		{"single char bucket no match", "object:bucket:a", "b", false},
		{"bucket with special chars", "object:bucket:bucket-with_special.chars", "bucket-with_special.chars", true},
		{"prefix with underscore", "object:bucket:test_*", "test_bucket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesObjectBucket(tt.permResource, tt.requestedBucket)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesParameterPathEdgeCases tests edge cases for MatchesParameterPath.
func TestMatchesParameterPathEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		permResource  string
		requestedPath string
		expected      bool
	}{
		{"empty path with wildcard", "parameter:path:*", "", true},
		{"single char path", "parameter:path:/a", "/a", true},
		{"single char path no match", "parameter:path:/a", "/b", false},
		{"path with special chars", "parameter:path:/app/config-v1.2", "/app/config-v1.2", true},
		{"multi-level wildcard empty remaining", "parameter:path:/app/**", "/app/", true},
		{"single-level wildcard empty remaining", "parameter:path:/app/*", "/app/", true},
		{"deep nested path with multi-level", "parameter:path:/**", "/a/b/c/d/e/f/g", true},
		// Additional edge case for path shorter than prefix in single-level wildcard
		{"single-level wildcard with very long prefix", "parameter:path:/very/long/prefix/*", "/short", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesParameterPath(tt.permResource, tt.requestedPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBatchAuthorizeWithCustomFunction tests BatchAuthorize with a custom function.
func TestBatchAuthorizeWithCustomFunction(t *testing.T) {
	provider := &mockAuthorizationProvider{
		batchAuthorizeFunc: func(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error) {
			decisions := make([]*AuthorizationDecision, len(reqs))
			for i, req := range reqs {
				allowed := req.Action != ActionAdmin
				decisions[i] = &AuthorizationDecision{
					Allowed:  allowed,
					Provider: "custom-batch",
				}
				if !allowed {
					decisions[i].Reason = "admin access denied in batch"
				}
			}
			return decisions, nil
		},
	}

	reqs := []*AuthorizationRequest{
		{Subject: Subject{ID: "user-1"}, Resource: ResourceKeys, Action: ActionRead},
		{Subject: Subject{ID: "user-1"}, Resource: ResourceCluster, Action: ActionAdmin},
		{Subject: Subject{ID: "user-1"}, Resource: ResourceSecrets, Action: ActionWrite},
	}

	decisions, err := provider.BatchAuthorize(context.Background(), reqs)
	require.NoError(t, err)
	require.Len(t, decisions, 3)

	assert.True(t, decisions[0].Allowed)
	assert.False(t, decisions[1].Allowed)
	assert.Equal(t, "admin access denied in batch", decisions[1].Reason)
	assert.True(t, decisions[2].Allowed)
}
