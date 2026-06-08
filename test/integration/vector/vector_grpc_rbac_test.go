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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// Vector permission grants in "resource:action" form as produced by the gRPC
// auth interceptor's mapVectorMethod (resource is "vector", the action is the
// vector-specific action string). The authzfake double splits on the first
// colon, so each grant is "vector:<vector-action>".
const (
	grantVectorRead      = interfaces.ResourceVector + ":" + interfaces.ActionVectorRead
	grantVectorWrite     = interfaces.ResourceVector + ":" + interfaces.ActionVectorWrite
	grantVectorDelete    = interfaces.ResourceVector + ":" + interfaces.ActionVectorDelete
	grantVectorNamespace = interfaces.ResourceVector + ":" + interfaces.ActionVectorNamespace
	grantVectorSearch    = interfaces.ResourceVector + ":" + interfaces.ActionVectorSearch
	grantVectorStats     = interfaces.ResourceVector + ":" + interfaces.ActionVectorStats
)

// Role grant bundles model the vector-reader / vector-writer / vector-admin
// roles the deleted fork role manager used to seed. Each test configures the
// server's authzfake allow-list with the bundle for the role under test.
var (
	readerGrants = []string{grantVectorRead, grantVectorSearch, grantVectorStats}
	writerGrants = []string{grantVectorRead, grantVectorWrite, grantVectorSearch, grantVectorStats}
	adminGrants  = []string{
		grantVectorRead, grantVectorWrite, grantVectorDelete,
		grantVectorNamespace, grantVectorSearch, grantVectorStats,
	}
)

// testVectorRBACContext provides infrastructure for vector RBAC tests.
type testVectorRBACContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.InMemoryAuthManager
	authzService interfaces.AuthorizationService
	server       *grpc.Server
	listener     net.Listener
	conn         *grpc.ClientConn
}

// newTestVectorRBACContext creates a test context for vector RBAC tests. The
// server authorizes requests whose "resource:action" matches one of allowed.
func newTestVectorRBACContext(t *testing.T, allowed ...string) *testVectorRBACContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-vector-rbac-secret-key-for-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	authzService := authzfake.New(allowed...)

	// Create gRPC server with auth interceptors
	unaryInterceptor := auth.UnaryAuthInterceptorWithService(authManager, authzService, log)
	streamInterceptor := auth.StreamAuthInterceptorWithService(authManager, authzService, log)

	server := grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
	)

	// Register mock vector service for testing
	pb.RegisterVectorDBServer(server, &mockVectorDBServer{})

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		if err := server.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			log.WithError(err).Error("gRPC server error")
		}
	}()

	// Create client connection
	conn, err := grpc.DialContext(ctx, listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return &testVectorRBACContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		authzService: authzService,
		server:       server,
		listener:     listener,
		conn:         conn,
	}
}

// close shuts down the test context.
func (c *testVectorRBACContext) close() {
	if c.conn != nil {
		c.conn.Close()
	}
	if c.server != nil {
		c.server.GracefulStop()
	}
	if c.listener != nil {
		c.listener.Close()
	}
	if c.authzService != nil {
		c.authzService.Close()
	}
	if c.cancel != nil {
		c.cancel()
	}
}

// issueTokenForRBACUser issues a token for a user that has already been created.
// This replaces the deprecated password-based authentication flow.
func issueTokenForRBACUser(ctx context.Context, authManager *auth.InMemoryAuthManager, username string) (*auth.Token, error) {
	userInfo := authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	return authManager.IssueTokenForUser(ctx, userInfo)
}

// createUserToken creates a JWT token for a test user with specified roles.
func (c *testVectorRBACContext) createUserToken(_ string, username string, roles []string) string {
	c.t.Helper()

	ctx := context.Background()

	// Create user with roles - InMemoryAuthManager assigns roles at creation time
	_, err := c.authManager.CreateUser(username, roles)
	if err != nil {
		c.t.Logf("user may already exist: %v", err)
	}

	// Issue token for the user (replaces deprecated password-based authentication)
	token, err := issueTokenForRBACUser(ctx, c.authManager, username)
	require.NoError(c.t, err)

	return token.AccessToken
}

// addAuthToContext adds authorization token to context.
func addAuthToContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

// mockVectorDBServer implements VectorDB for testing.
type mockVectorDBServer struct {
	pb.UnimplementedVectorDBServer
}

// GetVector implements a mock GetVector method.
func (s *mockVectorDBServer) GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	return &pb.GetVectorResponse{
		Found: true,
		Vector: &pb.VectorEntity{
			Id:        req.Id,
			Namespace: req.Namespace,
			Vector:    []float32{0.1, 0.2, 0.3},
		},
	}, nil
}

// UpsertVector implements a mock UpsertVector method.
func (s *mockVectorDBServer) UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	return &pb.UpsertVectorResponse{
		Id:      req.Vector.Id,
		Version: 1,
	}, nil
}

// DeleteVector implements a mock DeleteVector method.
func (s *mockVectorDBServer) DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	return &pb.DeleteVectorResponse{
		Deleted: true,
	}, nil
}

// CreateNamespace implements a mock CreateNamespace method.
func (s *mockVectorDBServer) CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	return &pb.CreateNamespaceResponse{
		Namespace: req.Config.Namespace,
	}, nil
}

// DeleteNamespace implements a mock DeleteNamespace method.
func (s *mockVectorDBServer) DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	return &pb.DeleteNamespaceResponse{
		Deleted: true,
	}, nil
}

// ListNamespaces implements a mock ListNamespaces method.
func (s *mockVectorDBServer) ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	return &pb.ListNamespacesResponse{
		Namespaces: []*pb.NamespaceInfo{
			{Config: &pb.NamespaceConfig{Namespace: "test-ns", Dimensions: 128}},
		},
	}, nil
}

// =============================================================================
// Vector RBAC Integration Tests
// =============================================================================

// TestVectorRBACReadOperations tests that read operations require reader role.
func TestVectorRBACReadOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("reader can get vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("reader-1", "reader1", []string{"vector-reader"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("reader can list namespaces", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("reader-2", "reader2", []string{"vector-reader"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.ListNamespaces(authCtx, &pb.ListNamespacesRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("unauthenticated user cannot read", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		_, err := client.GetVector(ctx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

// TestVectorRBACWriteOperations tests that write operations require writer role.
func TestVectorRBACWriteOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("writer can upsert vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("writer-1", "writer1", []string{"vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.UpsertVector(authCtx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "vec-new",
				Namespace: "test-ns",
				Vector:    []float32{0.1, 0.2, 0.3},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "vec-new", resp.Id)
	})

	t.Run("reader cannot upsert vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("reader-3", "reader3", []string{"vector-reader"})
		authCtx := addAuthToContext(ctx, token)

		_, err := client.UpsertVector(authCtx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "vec-denied",
				Namespace: "test-ns",
				Vector:    []float32{0.1, 0.2, 0.3},
			},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("writer can also read", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("writer-2", "writer2", []string{"vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})
}

// TestVectorRBACDeleteOperations tests that delete operations require admin role.
func TestVectorRBACDeleteOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("admin can delete vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, adminGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("admin-1", "admin1", []string{"vector-admin"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.DeleteVector(authCtx, &pb.DeleteVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-to-delete",
		})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})

	t.Run("writer cannot delete vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("writer-3", "writer3", []string{"vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		_, err := client.DeleteVector(authCtx, &pb.DeleteVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-forbidden",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("reader cannot delete vector", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("reader-4", "reader4", []string{"vector-reader"})
		authCtx := addAuthToContext(ctx, token)

		_, err := client.DeleteVector(authCtx, &pb.DeleteVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-forbidden",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

// TestVectorRBACNamespaceOperations tests namespace operations require admin role.
func TestVectorRBACNamespaceOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("admin can create namespace", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, adminGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("admin-2", "admin2", []string{"vector-admin"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.CreateNamespace(authCtx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:      "new-ns",
				Dimensions:     128,
				DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Namespace)
	})

	t.Run("admin can delete namespace", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, adminGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("admin-3", "admin3", []string{"vector-admin"})
		authCtx := addAuthToContext(ctx, token)

		resp, err := client.DeleteNamespace(authCtx, &pb.DeleteNamespaceRequest{
			Namespace: "to-delete-ns",
			Force:     true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})

	t.Run("writer cannot create namespace", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("writer-4", "writer4", []string{"vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		_, err := client.CreateNamespace(authCtx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:  "forbidden-ns",
				Dimensions: 128,
			},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("writer cannot delete namespace", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("writer-5", "writer5", []string{"vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		_, err := client.DeleteNamespace(authCtx, &pb.DeleteNamespaceRequest{
			Namespace: "forbidden-ns",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

// TestVectorRBACMultipleRoles tests a user whose combined grants permit read
// and write but not delete.
func TestVectorRBACMultipleRoles(t *testing.T) {
	ctx := context.Background()

	t.Run("user with reader and writer roles", func(t *testing.T) {
		// reader+writer combined grants = writerGrants (read+write+search+stats),
		// which notably excludes delete.
		testCtx := newTestVectorRBACContext(t, writerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		token := testCtx.createUserToken("multi-1", "multi1", []string{"vector-reader", "vector-writer"})
		authCtx := addAuthToContext(ctx, token)

		// Can read
		getResp, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.NoError(t, err)
		assert.True(t, getResp.Found)

		// Can write
		upsertResp, err := client.UpsertVector(authCtx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{
				Id:        "vec-multi",
				Namespace: "test-ns",
				Vector:    []float32{0.1, 0.2, 0.3},
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, upsertResp.Id)

		// Cannot delete (no delete permission)
		_, err = client.DeleteVector(authCtx, &pb.DeleteVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-multi",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

// TestVectorRBACInvalidToken tests behavior with invalid tokens.
func TestVectorRBACInvalidToken(t *testing.T) {
	ctx := context.Background()

	t.Run("malformed token is rejected", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		authCtx := addAuthToContext(ctx, "not-a-valid-jwt-token")

		_, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("empty token is rejected as permission denied", func(t *testing.T) {
		// An empty authorization header results in anonymous access, which is
		// then denied by the authorization service (PermissionDenied). This is
		// different from a missing authorization header (Unauthenticated).
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		authCtx := addAuthToContext(ctx, "")

		_, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("tampered token is rejected", func(t *testing.T) {
		testCtx := newTestVectorRBACContext(t, readerGrants...)
		defer testCtx.close()
		client := pb.NewVectorDBClient(testCtx.conn)

		validToken := testCtx.createUserToken("tamper-1", "tamper1", []string{"vector-reader"})
		tamperedToken := validToken[:len(validToken)-5] + "XXXXX"
		authCtx := addAuthToContext(ctx, tamperedToken)

		_, err := client.GetVector(authCtx, &pb.GetVectorRequest{
			Namespace: "test-ns",
			Id:        "vec-1",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}
