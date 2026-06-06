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

package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// quicAuthManagerAdapter wraps auth.InMemoryAuthManager to satisfy interfaces.AuthManager.
type quicAuthManagerAdapter struct {
	manager *auth.InMemoryAuthManager
}

// Authenticate wraps the concrete Authenticate to return interface{}.
func (a *quicAuthManagerAdapter) Authenticate(ctx context.Context, username string, password *secure.Buffer) (interface{}, error) {
	return a.manager.Authenticate(ctx, username, password)
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated
// via mTLS certificate, FIDO2/WebAuthn, or other IAM provider.
func (a *quicAuthManagerAdapter) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (interface{}, error) {
	return a.manager.IssueTokenForUser(ctx, user)
}

// Authorize wraps the concrete Authorize method.
func (a *quicAuthManagerAdapter) Authorize(ctx context.Context, token, resource, action string) error {
	return a.manager.Authorize(ctx, token, resource, action)
}

// RefreshToken wraps the concrete RefreshToken to return interface{}.
func (a *quicAuthManagerAdapter) RefreshToken(ctx context.Context, refreshToken string) (interface{}, error) {
	return a.manager.RefreshToken(ctx, refreshToken)
}

// ValidateToken wraps the concrete ValidateToken.
func (a *quicAuthManagerAdapter) ValidateToken(ctx context.Context, token string) (interface{}, error) {
	return a.manager.ValidateToken(ctx, token)
}

// RevokeToken wraps the concrete RevokeToken method.
func (a *quicAuthManagerAdapter) RevokeToken(ctx context.Context, token string) error {
	return a.manager.RevokeToken(ctx, token)
}

// CreateUser wraps the concrete CreateUser to return interface{}.
func (a *quicAuthManagerAdapter) CreateUser(username, password string, roles []string) (interface{}, error) {
	return a.manager.CreateUser(username, password, roles)
}

// GetUserStore returns the user store.
func (a *quicAuthManagerAdapter) GetUserStore() interfaces.UserStore {
	return a.manager.GetUserStore()
}

// GetRoleManager returns the role manager.
func (a *quicAuthManagerAdapter) GetRoleManager() interfaces.RoleManager {
	return a.manager.GetRoleManager()
}

// testQUICContext provides infrastructure for QUIC authorization tests.
type testQUICContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.InMemoryAuthManager
	authzService interfaces.AuthorizationService
	handler      *quic.Handler
}

// mockQuicBaaSService is a minimal mock service for QUIC handler testing.
type mockQuicBaaSService struct {
	service.QuicBaaS
}

// newTestQUICContext creates a new test context for QUIC authorization tests.
func newTestQUICContext(t *testing.T) *testQUICContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-quic-secret-key-for-integration-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	rbacProvider, err := providers.NewRBACProvider(roleManager, log)
	require.NoError(t, err)

	authzService, err := auth.NewAuthorizationService(rbacProvider, log)
	require.NoError(t, err)

	// Create adapter to satisfy interfaces.AuthManager
	authAdapter := &quicAuthManagerAdapter{manager: authManager}

	// Create QUIC handler with authorization
	handler := quic.NewHandler(
		nil, // QuicBaaS service - nil for auth-only testing
		nil, // KMS service
		nil, // Secrets service
		nil, // Crypto service
		log,
		quic.WithHandlerAuthManager(authAdapter),
		quic.WithHandlerAuthorizationService(authzService),
	)

	return &testQUICContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		authzService: authzService,
		handler:      handler,
	}
}

// Close releases test resources.
func (tc *testQUICContext) Close() {
	tc.cancel()
	if tc.authzService != nil {
		tc.authzService.Close()
	}
}

// createTestUser creates a user and returns the token.
// Note: password parameter removed as password-based authentication is disabled.
// Users authenticate via mTLS certificates, WebAuthn, or OIDC.
func (tc *testQUICContext) createTestUser(username string, roles []string) (string, error) {
	_, err := tc.authManager.CreateUser(username, "", roles)
	if err != nil {
		return "", err
	}

	userInfo := tc.authManager.GetUserStore().GetByUsername(username)
	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	if err != nil {
		return "", err
	}

	return token.AccessToken, nil
}

// TestQUICPublicOperationsNoAuth tests that public operations bypass authorization.
func TestQUICPublicOperationsNoAuth(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	publicOperations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpHealth, "Health"},
		{quic.OpLiveness, "Liveness"},
		{quic.OpReadiness, "Readiness"},
	}

	for _, op := range publicOperations {
		t.Run(op.name+"_is_public", func(t *testing.T) {
			req := &quic.Request{
				Opcode:  op.opcode,
				Token:   "", // No token
				Payload: []byte{},
			}

			resp := tc.handler.HandleRequest(tc.ctx, req)

			// Public operations should not return StatusUnauthorized
			assert.NotEqual(t, quic.StatusUnauthorized, resp.Status,
				"Public operation %s should not require auth", op.name)
		})
	}
}

// TestQUICProtectedOperationsRequireToken tests that protected operations require a token.
func TestQUICProtectedOperationsRequireToken(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	protectedOperations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpGet, "Get"},
		{quic.OpPut, "Put"},
		{quic.OpDelete, "Delete"},
		{quic.OpList, "List"},
		{quic.OpBatchGet, "BatchGet"},
		{quic.OpBatchPut, "BatchPut"},
		{quic.OpTransaction, "Transaction"},
		{quic.OpQuery, "Query"},
		{quic.OpClusterStatus, "ClusterStatus"},
	}

	for _, op := range protectedOperations {
		t.Run(op.name+"_requires_token", func(t *testing.T) {
			req := &quic.Request{
				Opcode:  op.opcode,
				Token:   "", // No token
				Payload: []byte{},
			}

			resp := tc.handler.HandleRequest(tc.ctx, req)

			assert.Equal(t, quic.StatusUnauthorized, resp.Status,
				"Protected operation %s should return StatusUnauthorized without token", op.name)
		})
	}
}

// TestQUICStatusUnauthorizedForMissingToken tests StatusUnauthorized (0x03) for missing token.
func TestQUICStatusUnauthorizedForMissingToken(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	req := &quic.Request{
		Opcode:  quic.OpGet,
		Token:   "",
		Payload: []byte{},
	}

	resp := tc.handler.HandleRequest(tc.ctx, req)

	assert.Equal(t, quic.StatusUnauthorized, resp.Status)
	assert.Equal(t, byte(0x03), resp.Status, "StatusUnauthorized should be 0x03")
	assert.Contains(t, string(resp.Payload), "authentication required")
}

// TestQUICStatusUnauthorizedForInvalidToken tests StatusUnauthorized for invalid token.
func TestQUICStatusUnauthorizedForInvalidToken(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	req := &quic.Request{
		Opcode:  quic.OpGet,
		Token:   "invalid-token-12345",
		Payload: []byte{},
	}

	resp := tc.handler.HandleRequest(tc.ctx, req)

	assert.Equal(t, quic.StatusUnauthorized, resp.Status)
	assert.Contains(t, string(resp.Payload), "invalid token")
}

// TestQUICStatusPermissionDeniedForDeniedAccess tests StatusPermissionDenied (0x07) for denied access.
func TestQUICStatusPermissionDeniedForDeniedAccess(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	// Create readonly user
	token, err := tc.createTestUser("readonly-quic", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)

	// Readonly user trying to Put (write action)
	req := &quic.Request{
		Opcode:  quic.OpPut,
		Token:   token,
		Payload: []byte{}, // Empty payload - just testing auth
	}

	resp := tc.handler.HandleRequest(tc.ctx, req)

	assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	assert.Equal(t, byte(0x07), resp.Status, "StatusPermissionDenied should be 0x07")
	assert.Contains(t, string(resp.Payload), "permission denied")
}

// TestQUICAdminRoleAccess tests that admin role has full access.
func TestQUICAdminRoleAccess(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	token, err := tc.createTestUser("admin-quic", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	operations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpGet, "Get"},
		{quic.OpPut, "Put"},
		{quic.OpDelete, "Delete"},
		{quic.OpList, "List"},
		{quic.OpClusterStatus, "ClusterStatus"},
		{quic.OpAddNode, "AddNode"},
	}

	for _, op := range operations {
		t.Run("admin_can_"+op.name, func(t *testing.T) {
			req := &quic.Request{
				Opcode:  op.opcode,
				Token:   token,
				Payload: []byte{},
			}

			resp := tc.handler.HandleRequest(tc.ctx, req)

			// Should not return permission denied or unauthorized
			assert.NotEqual(t, quic.StatusUnauthorized, resp.Status,
				"Admin should be authorized for %s", op.name)
			assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status,
				"Admin should have permission for %s", op.name)
		})
	}
}

// TestQUICReadWriteRoleAccess tests readwrite role permissions.
func TestQUICReadWriteRoleAccess(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	token, err := tc.createTestUser("readwrite-quic", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("can_read", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpGet,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("can_write", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpPut,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("can_delete", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpDelete,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_admin_cluster", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpAddNode,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICReadOnlyRoleAccess tests readonly role permissions.
func TestQUICReadOnlyRoleAccess(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	token, err := tc.createTestUser("readonly-quic-2", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)

	t.Run("can_read", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpGet,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_write", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpPut,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_delete", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpDelete,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICOpcodeToResourceActionMapping tests opcode to resource/action mapping.
func TestQUICOpcodeToResourceActionMapping(t *testing.T) {
	testCases := []struct {
		opcode           byte
		expectedResource string
		expectedAction   string
		description      string
	}{
		// Key operations
		{quic.OpGet, interfaces.ResourceKeys, interfaces.ActionRead, "Get -> keys:read"},
		{quic.OpPut, interfaces.ResourceKeys, interfaces.ActionWrite, "Put -> keys:write"},
		{quic.OpDelete, interfaces.ResourceKeys, interfaces.ActionDelete, "Delete -> keys:delete"},
		{quic.OpList, interfaces.ResourceKeys, interfaces.ActionRead, "List -> keys:read"},
		{quic.OpBatchGet, interfaces.ResourceKeys, interfaces.ActionRead, "BatchGet -> keys:read"},
		{quic.OpBatchPut, interfaces.ResourceKeys, interfaces.ActionWrite, "BatchPut -> keys:write"},
		{quic.OpBatchDelete, interfaces.ResourceKeys, interfaces.ActionDelete, "BatchDelete -> keys:delete"},

		// Transaction operations
		{quic.OpTransaction, interfaces.ResourceTransactions, interfaces.ActionWrite, "Transaction -> transactions:write"},

		// Query operations
		{quic.OpQuery, interfaces.ResourceKeys, interfaces.ActionRead, "Query -> keys:read"},
		{quic.OpScanIndex, interfaces.ResourceKeys, interfaces.ActionRead, "ScanIndex -> keys:read"},

		// Cluster operations
		{quic.OpClusterStatus, interfaces.ResourceCluster, interfaces.ActionRead, "ClusterStatus -> cluster:read"},
		{quic.OpGetLeader, interfaces.ResourceCluster, interfaces.ActionRead, "GetLeader -> cluster:read"},
		{quic.OpAddNode, interfaces.ResourceCluster, interfaces.ActionAdmin, "AddNode -> cluster:admin"},
		{quic.OpRemoveNode, interfaces.ResourceCluster, interfaces.ActionAdmin, "RemoveNode -> cluster:admin"},

		// Backup operations
		{quic.OpListBackups, interfaces.ResourceBackups, interfaces.ActionRead, "ListBackups -> backups:read"},
		{quic.OpCreateBackup, interfaces.ResourceBackups, interfaces.ActionWrite, "CreateBackup -> backups:write"},
		{quic.OpDeleteBackup, interfaces.ResourceBackups, interfaces.ActionDelete, "DeleteBackup -> backups:delete"},

		// Barrier/Ceremony operations: each uses a single dispatch opcode with
		// the sub-op carried in the payload, so authorization is enforced at the
		// dispatch-opcode granularity (seal/unseal/rekey are barrier sub-ops).
		{quic.OpBarrier, interfaces.ResourceBarrier, interfaces.ActionAdmin, "Barrier -> barrier:admin"},
		{quic.OpCeremony, interfaces.ResourceCeremony, interfaces.ActionAdmin, "Ceremony -> ceremony:admin"},

		// KMS operations
		{quic.OpKMSGetKey, interfaces.ResourceKMS, interfaces.ActionRead, "KMSGetKey -> kms:read"},
		{quic.OpKMSListKeys, interfaces.ResourceKMS, interfaces.ActionRead, "KMSListKeys -> kms:read"},
		{quic.OpKMSGenerateKey, interfaces.ResourceKMS, interfaces.ActionWrite, "KMSGenerateKey -> kms:write"},
		{quic.OpKMSDeleteKey, interfaces.ResourceKMS, interfaces.ActionDelete, "KMSDeleteKey -> kms:delete"},

		// Secrets operations
		{quic.OpSecretsGet, interfaces.ResourceSecrets, interfaces.ActionRead, "SecretsGet -> secrets:read"},
		{quic.OpSecretsList, interfaces.ResourceSecrets, interfaces.ActionRead, "SecretsList -> secrets:read"},
		{quic.OpSecretsStore, interfaces.ResourceSecrets, interfaces.ActionWrite, "SecretsStore -> secrets:write"},
		{quic.OpSecretsDelete, interfaces.ResourceSecrets, interfaces.ActionDelete, "SecretsDelete -> secrets:delete"},

		// Crypto operations
		{quic.OpCryptoEncrypt, interfaces.ResourceCrypto, interfaces.ActionWrite, "CryptoEncrypt -> crypto:write"},
		{quic.OpCryptoDecrypt, interfaces.ResourceCrypto, interfaces.ActionWrite, "CryptoDecrypt -> crypto:write"},

		// Timeseries operations
		{quic.OpTSQuery, interfaces.ResourceTimeseries, interfaces.ActionRead, "TSQuery -> timeseries:read"},
		{quic.OpTSWritePoint, interfaces.ResourceTimeseries, interfaces.ActionWrite, "TSWritePoint -> timeseries:write"},
		{quic.OpTSDeleteSeries, interfaces.ResourceTimeseries, interfaces.ActionDelete, "TSDeleteSeries -> timeseries:delete"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			resource, action := quic.GetResourceAndActionForTest(tc.opcode)
			assert.Equal(t, tc.expectedResource, resource, "Resource mismatch for opcode 0x%02x", tc.opcode)
			assert.Equal(t, tc.expectedAction, action, "Action mismatch for opcode 0x%02x", tc.opcode)
		})
	}
}

// TestQUICRevokedTokenRejected tests that revoked tokens are rejected.
func TestQUICRevokedTokenRejected(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	token, err := tc.createTestUser("revoke-quic", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	// Verify token works initially
	req := &quic.Request{
		Opcode:  quic.OpGet,
		Token:   token,
		Payload: []byte{},
	}

	resp := tc.handler.HandleRequest(tc.ctx, req)
	assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)

	// Revoke the token
	err = tc.authManager.RevokeToken(tc.ctx, token)
	require.NoError(t, err)

	// Verify token is now rejected
	resp = tc.handler.HandleRequest(tc.ctx, req)
	assert.Equal(t, quic.StatusUnauthorized, resp.Status)
}

// TestQUICCustomRoleAuthorization tests authorization with custom roles.
func TestQUICCustomRoleAuthorization(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	// Create custom KMS-only role
	roleManager := tc.authManager.GetRoleManager()
	kmsRole := &auth.Role{
		Name:        "kms-operator",
		Description: "Can only manage KMS keys",
		Permissions: []*auth.Permission{
			{
				Resource: interfaces.ResourceKMS,
				Actions:  []string{interfaces.ActionRead, interfaces.ActionWrite},
			},
		},
	}
	err := roleManager.AddRole(kmsRole)
	require.NoError(t, err)

	token, err := tc.createTestUser("kms-only-quic", []string{"kms-operator"})
	require.NoError(t, err)

	t.Run("can_read_kms", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpKMSGetKey,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("can_write_kms", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpKMSGenerateKey,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_delete_kms", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpKMSDeleteKey,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_access_keys", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpGet,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICMultipleRolesAuthorization tests authorization with multiple roles.
func TestQUICMultipleRolesAuthorization(t *testing.T) {
	tc := newTestQUICContext(t)
	defer tc.Close()

	token, err := tc.createTestUser("multi-role-quic", []string{auth.RoleNameReadOnly, auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("can_read", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpGet,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("can_write_from_readwrite", func(t *testing.T) {
		req := &quic.Request{
			Opcode:  quic.OpPut,
			Token:   token,
			Payload: []byte{},
		}

		resp := tc.handler.HandleRequest(tc.ctx, req)

		assert.NotEqual(t, quic.StatusUnauthorized, resp.Status)
		assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status)
	})
}
