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

//go:build integration

package vector

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// testMultiTenantContext provides infrastructure for multi-tenant vector tests.
type testMultiTenantContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.AuthManager
	authzService interfaces.AuthorizationService
	server       *grpc.Server
	listener     net.Listener
	conn         *grpc.ClientConn
	tenants      map[string]string // tenantID -> adminToken
}

// mockMultiTenantVectorServer implements VectorDB with tenant isolation.
type mockMultiTenantVectorServer struct {
	pb.UnimplementedVectorDBServer
	mu         sync.RWMutex
	namespaces map[string]*mockNamespace   // tenantID:namespace -> data
	vectors    map[string]*pb.VectorEntity // tenantID:namespace:vectorID -> vector
}

// mockNamespace represents a namespace owned by a tenant.
type mockNamespace struct {
	tenantID   string
	namespace  string
	dimensions int32
	metric     pb.DistanceMetric
}

func newMockMultiTenantVectorServer() *mockMultiTenantVectorServer {
	return &mockMultiTenantVectorServer{
		namespaces: make(map[string]*mockNamespace),
		vectors:    make(map[string]*pb.VectorEntity),
	}
}

// extractTenantFromContext extracts tenant ID from gRPC metadata.
func extractTenantFromContext(ctx context.Context) string {
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

// CreateNamespace implements tenant-aware namespace creation.
func (s *mockMultiTenantVectorServer) CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := tenantID + ":" + req.Config.Namespace
	if _, exists := s.namespaces[key]; exists {
		return nil, status.Error(codes.AlreadyExists, "namespace already exists")
	}

	s.namespaces[key] = &mockNamespace{
		tenantID:   tenantID,
		namespace:  req.Config.Namespace,
		dimensions: req.Config.Dimensions,
		metric:     req.Config.DistanceMetric,
	}

	return &pb.CreateNamespaceResponse{Namespace: req.Config.Namespace}, nil
}

// DeleteNamespace implements tenant-aware namespace deletion.
func (s *mockMultiTenantVectorServer) DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := tenantID + ":" + req.Namespace
	ns, exists := s.namespaces[key]
	if !exists {
		return nil, status.Error(codes.NotFound, "namespace not found")
	}

	// Check tenant ownership
	if ns.tenantID != tenantID {
		return nil, status.Error(codes.PermissionDenied, "cross-tenant access denied")
	}

	delete(s.namespaces, key)

	// Delete all vectors in this namespace
	for vecKey := range s.vectors {
		if len(vecKey) > len(key) && vecKey[:len(key)+1] == key+":" {
			delete(s.vectors, vecKey)
		}
	}

	return &pb.DeleteNamespaceResponse{Deleted: true}, nil
}

// ListNamespaces implements tenant-aware namespace listing.
func (s *mockMultiTenantVectorServer) ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var namespaces []*pb.NamespaceInfo
	for key, ns := range s.namespaces {
		if ns.tenantID == tenantID {
			namespaces = append(namespaces, &pb.NamespaceInfo{
				Config: &pb.NamespaceConfig{
					Namespace:      ns.namespace,
					Dimensions:     ns.dimensions,
					DistanceMetric: ns.metric,
				},
			})
			_ = key // silence unused warning
		}
	}

	return &pb.ListNamespacesResponse{Namespaces: namespaces}, nil
}

// UpsertVector implements tenant-aware vector upsert.
func (s *mockMultiTenantVectorServer) UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nsKey := tenantID + ":" + req.Vector.Namespace
	ns, exists := s.namespaces[nsKey]
	if !exists {
		return nil, status.Error(codes.NotFound, "namespace not found")
	}

	// Check tenant ownership
	if ns.tenantID != tenantID {
		return nil, status.Error(codes.PermissionDenied, "cross-tenant access denied")
	}

	vecKey := tenantID + ":" + req.Vector.Namespace + ":" + req.Vector.Id
	s.vectors[vecKey] = req.Vector

	return &pb.UpsertVectorResponse{
		Id:      req.Vector.Id,
		Version: 1,
	}, nil
}

// GetVector implements tenant-aware vector retrieval.
func (s *mockMultiTenantVectorServer) GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	vecKey := tenantID + ":" + req.Namespace + ":" + req.Id
	vec, exists := s.vectors[vecKey]
	if !exists {
		return &pb.GetVectorResponse{Found: false}, nil
	}

	return &pb.GetVectorResponse{
		Found:  true,
		Vector: vec,
	}, nil
}

// DeleteVector implements tenant-aware vector deletion.
func (s *mockMultiTenantVectorServer) DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	tenantID := extractTenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant ID required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	vecKey := tenantID + ":" + req.Namespace + ":" + req.Id
	_, exists := s.vectors[vecKey]
	if !exists {
		return &pb.DeleteVectorResponse{Deleted: false}, nil
	}

	delete(s.vectors, vecKey)
	return &pb.DeleteVectorResponse{Deleted: true}, nil
}

// newTestMultiTenantContext creates a test context for multi-tenant tests.
func newTestMultiTenantContext(t *testing.T) *testMultiTenantContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-multi-tenant-signing-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Tenant isolation is enforced by the interceptor's x-tenant-id validation
	// and the tenant-keyed mock store, not by RBAC roles. All tenant users hold
	// the vector-admin role, so the server grants every vector operation; the
	// cross-tenant denials the tests assert come from the tenant boundary.
	authzService := authzfake.New(adminGrants...)

	unaryInterceptor := auth.UnaryAuthInterceptorWithService(authManager, authzService, log)
	streamInterceptor := auth.StreamAuthInterceptorWithService(authManager, authzService, log)

	server := grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
	)

	// Register multi-tenant mock vector service
	pb.RegisterVectorDBServer(server, newMockMultiTenantVectorServer())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		if err := server.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			log.WithError(err).Error("gRPC server error")
		}
	}()

	conn, err := grpc.DialContext(ctx, listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return &testMultiTenantContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		authzService: authzService,
		server:       server,
		listener:     listener,
		conn:         conn,
		tenants:      make(map[string]string),
	}
}

// close shuts down the test context.
func (c *testMultiTenantContext) close() {
	if c.conn != nil {
		c.conn.Close()
	}
	if c.server != nil {
		c.server.GracefulStop()
	}
	if c.listener != nil {
		c.listener.Close()
	}
	if c.cancel != nil {
		c.cancel()
	}
}

// issueTokenForUser retrieves a user from the store and issues a token.
// This replaces the deprecated password-based Authenticate method.
func issueTokenForUser(ctx context.Context, authManager *auth.AuthManager, username string) (*auth.Token, error) {
	userInfo := authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, &qberrors.MessageError{Message: "user not found: " + username}
	}
	return authManager.IssueTokenForUser(ctx, userInfo)
}

// createTenantUser creates a user for a specific tenant.
// The tenantID is included in the JWT token to enable server-side tenant validation.
// This prevents cross-tenant access attacks where a user tries to spoof the x-tenant-id header.
func (c *testMultiTenantContext) createTenantUser(tenantID, userID, username string, roles []string) string {
	c.t.Helper()

	// Create user via the auth manager with tenant ID
	// The tenant ID will be included in the JWT token claims
	_, err := c.authManager.CreateUserWithTenant(username, tenantID, roles)
	if err != nil {
		c.t.Logf("user may already exist: %v", err)
	}

	// Issue token for the user (replaces deprecated password-based Authenticate)
	ctx := context.Background()
	token, err := issueTokenForUser(ctx, c.authManager, username)
	require.NoError(c.t, err)

	c.tenants[tenantID] = token.AccessToken
	return token.AccessToken
}

// addTenantContext adds tenant and auth info to context.
func addTenantContext(ctx context.Context, token, tenantID string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"authorization", "Bearer "+token,
		"x-tenant-id", tenantID,
	))
}

// =============================================================================
// Multi-Tenancy Integration Tests
// =============================================================================

// TestMultiTenantNamespaceIsolation tests namespace isolation between tenants.
func TestMultiTenantNamespaceIsolation(t *testing.T) {
	testCtx := newTestMultiTenantContext(t)
	defer testCtx.close()

	client := pb.NewVectorDBClient(testCtx.conn)
	ctx := context.Background()

	// Create users for two tenants
	tenant1Token := testCtx.createTenantUser("tenant-1", "user-t1", "user_tenant1", []string{"vector-admin"})
	tenant2Token := testCtx.createTenantUser("tenant-2", "user-t2", "user_tenant2", []string{"vector-admin"})

	t.Run("tenant1 creates namespace", func(t *testing.T) {
		tenant1Ctx := addTenantContext(ctx, tenant1Token, "tenant-1")

		resp, err := client.CreateNamespace(tenant1Ctx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:      "shared-name",
				Dimensions:     128,
				DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Namespace)
	})

	t.Run("tenant2 creates namespace with same name", func(t *testing.T) {
		tenant2Ctx := addTenantContext(ctx, tenant2Token, "tenant-2")

		// Should succeed - different tenant, same namespace name
		resp, err := client.CreateNamespace(tenant2Ctx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:      "shared-name",
				Dimensions:     256, // Different dimensions
				DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_L2,
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Namespace)
	})

	t.Run("tenant1 only sees own namespaces", func(t *testing.T) {
		tenant1Ctx := addTenantContext(ctx, tenant1Token, "tenant-1")

		resp, err := client.ListNamespaces(tenant1Ctx, &pb.ListNamespacesRequest{})
		require.NoError(t, err)

		// Should only see tenant1's namespace
		assert.Len(t, resp.Namespaces, 1)
		if len(resp.Namespaces) > 0 && resp.Namespaces[0].Config != nil {
			assert.Equal(t, int32(128), resp.Namespaces[0].Config.Dimensions)
		}
	})

	t.Run("tenant2 only sees own namespaces", func(t *testing.T) {
		tenant2Ctx := addTenantContext(ctx, tenant2Token, "tenant-2")

		resp, err := client.ListNamespaces(tenant2Ctx, &pb.ListNamespacesRequest{})
		require.NoError(t, err)

		// Should only see tenant2's namespace
		assert.Len(t, resp.Namespaces, 1)
		if len(resp.Namespaces) > 0 && resp.Namespaces[0].Config != nil {
			assert.Equal(t, int32(256), resp.Namespaces[0].Config.Dimensions)
		}
	})
}

// TestMultiTenantVectorIsolation tests vector data isolation between tenants.
func TestMultiTenantVectorIsolation(t *testing.T) {
	testCtx := newTestMultiTenantContext(t)
	defer testCtx.close()

	client := pb.NewVectorDBClient(testCtx.conn)
	ctx := context.Background()

	tenant1Token := testCtx.createTenantUser("tenant-a", "user-ta", "user_ta", []string{"vector-admin"})
	tenant2Token := testCtx.createTenantUser("tenant-b", "user-tb", "user_tb", []string{"vector-admin"})

	// Setup namespaces
	tenant1Ctx := addTenantContext(ctx, tenant1Token, "tenant-a")
	tenant2Ctx := addTenantContext(ctx, tenant2Token, "tenant-b")

	_, err := client.CreateNamespace(tenant1Ctx, &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "vectors",
			Dimensions: 128,
		},
	})
	require.NoError(t, err)

	_, err = client.CreateNamespace(tenant2Ctx, &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "vectors",
			Dimensions: 128,
		},
	})
	require.NoError(t, err)

	t.Run("tenant1 upserts vector", func(t *testing.T) {
		resp, err := client.UpsertVector(tenant1Ctx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "vec-shared-id",
				Namespace: "vectors",
				Vector:    make([]float32, 128),
				Metadata:  map[string]string{"owner": "tenant-a"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "vec-shared-id", resp.Id)
	})

	t.Run("tenant2 upserts vector with same ID", func(t *testing.T) {
		resp, err := client.UpsertVector(tenant2Ctx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "vec-shared-id",
				Namespace: "vectors",
				Vector:    make([]float32, 128),
				Metadata:  map[string]string{"owner": "tenant-b"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "vec-shared-id", resp.Id)
	})

	t.Run("tenant1 gets own vector", func(t *testing.T) {
		resp, err := client.GetVector(tenant1Ctx, &pb.GetVectorRequest{
			Namespace:       "vectors",
			Id:              "vec-shared-id",
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, "tenant-a", resp.Vector.Metadata["owner"])
	})

	t.Run("tenant2 gets own vector", func(t *testing.T) {
		resp, err := client.GetVector(tenant2Ctx, &pb.GetVectorRequest{
			Namespace:       "vectors",
			Id:              "vec-shared-id",
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, "tenant-b", resp.Vector.Metadata["owner"])
	})

	t.Run("tenant1 cannot see tenant2 exclusive vector", func(t *testing.T) {
		// Tenant2 creates an exclusive vector
		_, err := client.UpsertVector(tenant2Ctx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "exclusive-vec",
				Namespace: "vectors",
				Vector:    make([]float32, 128),
			},
		})
		require.NoError(t, err)

		// Tenant1 tries to get it
		resp, err := client.GetVector(tenant1Ctx, &pb.GetVectorRequest{
			Namespace: "vectors",
			Id:        "exclusive-vec",
		})
		require.NoError(t, err)
		assert.False(t, resp.Found, "tenant1 should not find tenant2's exclusive vector")
	})
}

// TestMultiTenantConcurrentOperations tests concurrent operations from multiple tenants.
func TestMultiTenantConcurrentOperations(t *testing.T) {
	testCtx := newTestMultiTenantContext(t)
	defer testCtx.close()

	client := pb.NewVectorDBClient(testCtx.conn)
	ctx := context.Background()

	numTenants := 5
	tokens := make([]string, numTenants)
	tenantIDs := make([]string, numTenants)

	// Create tenants
	for i := 0; i < numTenants; i++ {
		tenantID := fmt.Sprintf("tenant-%d", i)
		userID := fmt.Sprintf("user-%d", i)
		username := fmt.Sprintf("user%d", i)

		tokens[i] = testCtx.createTenantUser(tenantID, userID, username, []string{"vector-admin"})
		tenantIDs[i] = tenantID

		// Create namespace for each tenant
		tenantCtx := addTenantContext(ctx, tokens[i], tenantID)
		_, err := client.CreateNamespace(tenantCtx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:  "concurrent-ns",
				Dimensions: 64,
			},
		})
		require.NoError(t, err)
	}

	// Concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, numTenants*10)

	for i := 0; i < numTenants; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			tenantCtx := addTenantContext(ctx, tokens[idx], tenantIDs[idx])

			for j := 0; j < 10; j++ {
				vecID := "vec-" + strconv.Itoa(idx) + "-" + strconv.Itoa(j)

				// Upsert
				_, err := client.UpsertVector(tenantCtx, &pb.UpsertVectorRequest{
					Vector: &pb.VectorEntity{
						Id:        vecID,
						Namespace: "concurrent-ns",
						Vector:    make([]float32, 64),
						Metadata:  map[string]string{"idx": strconv.Itoa(j)},
					},
				})
				if err != nil {
					errors <- &qberrors.WrappedMessageError{Message: "tenant " + strconv.Itoa(idx) + " upsert " + strconv.Itoa(j), Cause: err}
					continue
				}

				// Get
				resp, err := client.GetVector(tenantCtx, &pb.GetVectorRequest{
					Namespace: "concurrent-ns",
					Id:        vecID,
				})
				if err != nil {
					errors <- &qberrors.WrappedMessageError{Message: "tenant " + strconv.Itoa(idx) + " get " + strconv.Itoa(j), Cause: err}
					continue
				}
				if !resp.Found {
					errors <- &qberrors.MessageError{Message: "tenant " + strconv.Itoa(idx) + " get " + strconv.Itoa(j) + ": not found"}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errCount int
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
		errCount++
	}
	assert.Equal(t, 0, errCount, "no concurrent operation errors expected")
}

// TestMultiTenantCrossTenantAccessDenied tests that cross-tenant access is denied.
func TestMultiTenantCrossTenantAccessDenied(t *testing.T) {
	testCtx := newTestMultiTenantContext(t)
	defer testCtx.close()

	client := pb.NewVectorDBClient(testCtx.conn)
	ctx := context.Background()

	// Create two tenants - tokens will contain tenant IDs in JWT claims
	tenant1Token := testCtx.createTenantUser("secure-1", "user-s1", "user_s1", []string{"vector-admin"})
	tenant2Token := testCtx.createTenantUser("secure-2", "user-s2", "user_s2", []string{"vector-admin"})

	tenant1Ctx := addTenantContext(ctx, tenant1Token, "secure-1")

	// Tenant1 creates namespace
	_, err := client.CreateNamespace(tenant1Ctx, &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "private-ns",
			Dimensions: 128,
		},
	})
	require.NoError(t, err)

	// Tenant1 creates vector
	_, err = client.UpsertVector(tenant1Ctx, &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        "secret-vec",
			Namespace: "private-ns",
			Vector:    make([]float32, 128),
			Metadata:  map[string]string{"secret": "data"},
		},
	})
	require.NoError(t, err)

	t.Run("tenant2 cannot access tenant1 vector by spoofing tenant header", func(t *testing.T) {
		// Tenant2 tries to access with tenant1's namespace
		// Using tenant2's token but claiming to be tenant1 via spoofed header
		// The auth interceptor should reject this because the token's tenant (secure-2)
		// does not match the x-tenant-id header (secure-1)
		spoofedCtx := addTenantContext(ctx, tenant2Token, "secure-1")

		resp, err := client.GetVector(spoofedCtx, &pb.GetVectorRequest{
			Namespace: "private-ns",
			Id:        "secret-vec",
		})

		// The interceptor should deny access with PermissionDenied
		if err == nil {
			// If no error, vector should not be found (defense in depth)
			assert.False(t, resp.Found, "cross-tenant vector should not be accessible")
		} else {
			// Should get permission denied from tenant validation
			st, ok := status.FromError(err)
			require.True(t, ok, "error should be a gRPC status error")
			assert.Contains(t, []codes.Code{codes.PermissionDenied, codes.NotFound}, st.Code(),
				"expected PermissionDenied or NotFound, got: %v", st.Code())
		}
	})

	t.Run("missing tenant header is rejected", func(t *testing.T) {
		// No tenant header - multi-tenant users require x-tenant-id header
		authOnlyCtx := addAuthToContext(ctx, tenant1Token)

		_, err := client.GetVector(authOnlyCtx, &pb.GetVectorRequest{
			Namespace: "private-ns",
			Id:        "secret-vec",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}
