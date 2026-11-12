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

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Helpers
// =============================================================================

// subjectCtx returns a context carrying an authenticated Subject.
func subjectCtx(subject interfaces.Subject) context.Context {
	return auth.SubjectIntoContext(systemTenantCtx(), subject)
}

// authenticatedSubject returns an authenticated Subject with the given id/username/roles.
func authenticatedSubject(id, username string, roles ...string) interfaces.Subject {
	return interfaces.Subject{
		ID:            id,
		Username:      username,
		Roles:         roles,
		Authenticated: true,
	}
}

// unauthenticatedSubject returns an unauthenticated Subject.
func unauthenticatedSubject() interfaces.Subject {
	return interfaces.Subject{Authenticated: false}
}

// newAuthzEnabledKMS returns a kmsServiceImpl with authz enabled and an in-memory KVStore.
func newAuthzEnabledKMS() (*kmsServiceImpl, *mockKVStore) {
	kv := newMockKVStore()
	km := newMockKMSManager()
	svc := newKMSService(km, nil)
	svc.kvStore = kv
	svc.authzEnabled = true
	return svc, kv
}

// =============================================================================
// Mock KVStore for authz tests
// =============================================================================

type mockKVStore struct {
	data map[string][]byte
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{data: make(map[string][]byte)}
}

func (m *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	m.data[key] = value
	return nil
}

func (m *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "mockKVStore.Get", Err: &notFoundError{key: key}}
	}
	return v, nil
}

func (m *mockKVStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if len(prefix) == 0 || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if len(prefix) == 0 || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

// notFoundError satisfies errors.IsNotFound.
type notFoundError struct{ key string }

func (e *notFoundError) Error() string { return "key not found: " + e.key }

// =============================================================================
// Mock audit logger for key-use capture
// =============================================================================

type mockKeyUseAuditLogger struct {
	events []keyUseEvent
}

type keyUseEvent struct {
	principal string
	keyID     string
	operation string
	outcome   keyUseOutcome
	timestamp time.Time
}

func (m *mockKeyUseAuditLogger) LogKeyExport(_ context.Context, _, _, _, _, _, _, _ string, _ bool) error {
	return nil
}
func (m *mockKeyUseAuditLogger) LogKeyGenerate(_ context.Context, _, _, _, _, _, _ string, _ bool) error {
	return nil
}
func (m *mockKeyUseAuditLogger) LogKeyImport(_ context.Context, _, _, _, _, _, _ string, _ bool) error {
	return nil
}
func (m *mockKeyUseAuditLogger) LogKeyDeletion(_ context.Context, _, _, _, _ string, _ bool) error {
	return nil
}
func (m *mockKeyUseAuditLogger) LogGracePeriodDisable(_ context.Context, _ string, _ uint64, _ string, _ bool, _ string) error {
	return nil
}
func (m *mockKeyUseAuditLogger) LogKeyUse(_ context.Context, principal, keyID, operation string, outcome keyUseOutcome, ts time.Time) error {
	m.events = append(m.events, keyUseEvent{
		principal: principal,
		keyID:     keyID,
		operation: operation,
		outcome:   outcome,
		timestamp: ts,
	})
	return nil
}

// =============================================================================
// Tests: Subject context helpers
// =============================================================================

func TestSubjectIntoContext_RoundTrip(t *testing.T) {
	subject := authenticatedSubject("user-1", "alice", "operator")
	ctx := subjectCtx(subject)
	got, ok := auth.SubjectFromContext(ctx)
	require.True(t, ok, "SubjectFromContext should return true for injected subject")
	assert.Equal(t, subject.ID, got.ID)
	assert.Equal(t, subject.Username, got.Username)
	assert.True(t, got.Authenticated)
}

func TestSubjectFromContext_MissingReturnsNotOK(t *testing.T) {
	_, ok := auth.SubjectFromContext(systemTenantCtx())
	assert.False(t, ok, "SubjectFromContext should return false for empty context")
}

func TestIsSystemContext_False(t *testing.T) {
	assert.False(t, auth.IsSystemContext(systemTenantCtx()))
}

func TestIsSystemContext_True(t *testing.T) {
	ctx := auth.NewSystemContext(systemTenantCtx())
	assert.True(t, auth.IsSystemContext(ctx))
}

// =============================================================================
// Tests: checkKeyAuthz
// =============================================================================

func TestCheckKeyAuthz_AuthzDisabled_Allows(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	svc.authzEnabled = false
	// No subject in context — must still allow when authz is disabled.
	err := svc.checkKeyAuthz(systemTenantCtx(), "key1", "encrypt")
	require.NoError(t, err)
}

func TestCheckKeyAuthz_SystemContext_Allows(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	ctx := auth.NewSystemContext(systemTenantCtx())
	// System context bypasses authz entirely regardless of policy.
	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.NoError(t, err)
}

func TestCheckKeyAuthz_NoSubjectInContext_Denies(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	err := svc.checkKeyAuthz(systemTenantCtx(), "key1", "encrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCheckKeyAuthz_UnauthenticatedSubject_Denies(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	ctx := subjectCtx(unauthenticatedSubject())
	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCheckKeyAuthz_NoPolicy_Allows(t *testing.T) {
	// No policy stored — default-allow for any authenticated principal.
	svc, _ := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))
	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.NoError(t, err)
}

func TestCheckKeyAuthz_AllowedPrincipals_Allowed(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	policy := &KeyPolicy{
		KeyID:             "key1",
		AllowedPrincipals: []string{"user-1"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.NoError(t, err)
}

func TestCheckKeyAuthz_AllowedPrincipals_Denied(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-2", "bob"))

	policy := &KeyPolicy{
		KeyID:             "key1",
		AllowedPrincipals: []string{"user-1"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCheckKeyAuthz_DeniedPrincipal_Denies(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	policy := &KeyPolicy{
		KeyID:            "key1",
		DeniedPrincipals: []string{"user-1"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCheckKeyAuthz_AllowedOperations_Allowed(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	policy := &KeyPolicy{
		KeyID:             "key1",
		AllowedOperations: []KeyUsage{KeyUsageEncrypt},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.NoError(t, err)
}

func TestCheckKeyAuthz_AllowedOperations_Denied(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	policy := &KeyPolicy{
		KeyID:             "key1",
		AllowedOperations: []KeyUsage{KeyUsageEncrypt}, // only encrypt; not decrypt
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "decrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCheckKeyAuthz_ExpiredPolicy_Denies(t *testing.T) {
	svc, kv := newAuthzEnabledKMS()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	past := time.Now().Add(-time.Hour)
	policy := &KeyPolicy{
		KeyID:     "key1",
		ExpiresAt: &past,
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.Error(t, err)
}

// =============================================================================
// Tests: key-use audit emission
// =============================================================================

func TestEmitKeyUseAudit_AllowEmitted(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	logger := &mockKeyUseAuditLogger{}
	svc.auditLogger = logger

	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))
	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	require.NoError(t, err)

	require.Len(t, logger.events, 1)
	assert.Equal(t, keyUseOutcomeAllow, logger.events[0].outcome)
	assert.Equal(t, "encrypt", logger.events[0].operation)
	assert.Equal(t, "key1", logger.events[0].keyID)
	assert.Equal(t, "user-1", logger.events[0].principal)
}

func TestEmitKeyUseAudit_DenyEmitted(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	logger := &mockKeyUseAuditLogger{}
	svc.auditLogger = logger

	// No subject → deny
	err := svc.checkKeyAuthz(systemTenantCtx(), "key1", "decrypt")
	require.Error(t, err)

	require.Len(t, logger.events, 1)
	assert.Equal(t, keyUseOutcomeDeny, logger.events[0].outcome)
	assert.Equal(t, "decrypt", logger.events[0].operation)
}

// =============================================================================
// Tests: SetKeyPolicy / GetKeyPolicy persistence
// =============================================================================

func TestSetKeyPolicy_PersistAndRetrieve(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	// Wire a real key so GetKey succeeds.
	km := svc.kmsManager.(*mockKMSManager)
	km.keys["test-key"] = &mockKeyData{keyID: "test-key", status: "enabled"}

	ctx := systemTenantCtx()

	policy := &KeyPolicy{
		KeyID:             "test-key",
		AllowedPrincipals: []string{"alice"},
		AllowedOperations: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
	}

	setResp, err := svc.SetKeyPolicy(ctx, &SetKeyPolicyRequest{KeyID: "test-key", Policy: policy})
	require.NoError(t, err)
	assert.Equal(t, "test-key", setResp.KeyID)
	assert.EqualValues(t, 1, setResp.PolicyVersion)

	getResp, err := svc.GetKeyPolicy(ctx, &GetKeyPolicyRequest{KeyID: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, getResp.Policy)
	assert.Equal(t, "test-key", getResp.Policy.KeyID)
	assert.Equal(t, []string{"alice"}, getResp.Policy.AllowedPrincipals)
	assert.EqualValues(t, 1, getResp.Policy.Version)
}

func TestSetKeyPolicy_KeyNotFound(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	ctx := systemTenantCtx()
	_, err := svc.SetKeyPolicy(ctx, &SetKeyPolicyRequest{
		KeyID: "nonexistent",
		Policy: &KeyPolicy{
			KeyID: "nonexistent",
		},
	})
	require.Error(t, err)
}

func TestGetKeyPolicy_NoPolicy_ReturnsDefault(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	km := svc.kmsManager.(*mockKMSManager)
	km.keys["my-key"] = &mockKeyData{keyID: "my-key", status: "enabled"}

	ctx := systemTenantCtx()
	resp, err := svc.GetKeyPolicy(ctx, &GetKeyPolicyRequest{KeyID: "my-key"})
	require.NoError(t, err)
	require.NotNil(t, resp.Policy)
	// Default policy has no restrictions.
	assert.Empty(t, resp.Policy.AllowedPrincipals)
}

func TestDeleteKeyPolicy_RemovesPolicy(t *testing.T) {
	svc, _ := newAuthzEnabledKMS()
	km := svc.kmsManager.(*mockKMSManager)
	km.keys["del-key"] = &mockKeyData{keyID: "del-key", status: "enabled"}

	ctx := systemTenantCtx()
	_, err := svc.SetKeyPolicy(ctx, &SetKeyPolicyRequest{
		KeyID: "del-key",
		Policy: &KeyPolicy{
			KeyID:             "del-key",
			AllowedPrincipals: []string{"alice"},
		},
	})
	require.NoError(t, err)

	_, err = svc.DeleteKeyPolicy(ctx, &DeleteKeyPolicyRequest{KeyID: "del-key"})
	require.NoError(t, err)

	// After deletion, get should return the default (empty) policy.
	getResp, err := svc.GetKeyPolicy(ctx, &GetKeyPolicyRequest{KeyID: "del-key"})
	require.NoError(t, err)
	assert.Empty(t, getResp.Policy.AllowedPrincipals)
}

// =============================================================================
// Tests: cryptoService authz integration
// =============================================================================

func TestCryptoService_Encrypt_AuthzDenied(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kv := newMockKVStore()
	kmsSvc.kvStore = kv
	kmsSvc.authzEnabled = true

	// Store a policy that denies user-2.
	ctx := systemTenantCtx()
	kmsManager.keys["sym-key"] = &mockKeyData{keyID: "sym-key", status: "enabled"}
	policy := &KeyPolicy{
		KeyID:            "sym-key",
		DeniedPrincipals: []string{"user-2"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("sym-key"), data)

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	// user-2 is denied
	userCtx := subjectCtx(authenticatedSubject("user-2", "bob"))
	_, err := svc.Encrypt(userCtx, &EncryptRequest{
		KeyID:     "sym-key",
		Plaintext: []byte("hello"),
	})
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCryptoService_Encrypt_AuthzAllowed(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kv := newMockKVStore()
	kmsSvc.kvStore = kv
	kmsSvc.authzEnabled = true

	ctx := systemTenantCtx()
	kmsManager.keys["sym-key"] = &mockKeyData{keyID: "sym-key", status: "enabled"}
	// Policy allows only alice.
	policy := &KeyPolicy{
		KeyID:             "sym-key",
		AllowedPrincipals: []string{"user-1"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("sym-key"), data)

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	// user-1 is allowed
	userCtx := subjectCtx(authenticatedSubject("user-1", "alice"))
	resp, err := svc.Encrypt(userCtx, &EncryptRequest{
		KeyID:     "sym-key",
		Plaintext: []byte("hello"),
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestCryptoService_Decrypt_MissingSubject_Denied(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kmsSvc.kvStore = newMockKVStore()
	kmsSvc.authzEnabled = true

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	_, err := svc.Decrypt(systemTenantCtx(), &DecryptRequest{
		KeyID:      "key1",
		Ciphertext: []byte("encrypted"),
	})
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCryptoService_Sign_AuthzDenied(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kv := newMockKVStore()
	kmsSvc.kvStore = kv
	kmsSvc.authzEnabled = true

	ctx := systemTenantCtx()
	kmsManager.keys["sign-key"] = &mockKeyData{keyID: "sign-key", status: "enabled"}
	// Only encrypt is allowed — sign should be denied.
	policy := &KeyPolicy{
		KeyID:             "sign-key",
		AllowedOperations: []KeyUsage{KeyUsageEncrypt},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("sign-key"), data)

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	userCtx := subjectCtx(authenticatedSubject("user-1", "alice"))
	_, err := svc.Sign(userCtx, &SignRequest{
		KeyID:   "sign-key",
		Message: []byte("message"),
	})
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}

func TestCryptoService_KeyUseAudit_EmittedOnAllow(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kmsSvc.kvStore = newMockKVStore()
	kmsSvc.authzEnabled = true

	auditLog := &mockKeyUseAuditLogger{}
	kmsSvc.auditLogger = auditLog

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))
	resp, err := svc.Encrypt(ctx, &EncryptRequest{
		KeyID:     "any-key",
		Plaintext: []byte("hello"),
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	require.Len(t, auditLog.events, 1)
	assert.Equal(t, keyUseOutcomeAllow, auditLog.events[0].outcome)
	assert.Equal(t, "encrypt", auditLog.events[0].operation)
}

func TestCryptoService_KeyUseAudit_EmittedOnDeny(t *testing.T) {
	kmsManager := newMockKMSManager()
	cryptoMgr := newMockCryptoManager()
	kmsSvc := newKMSService(kmsManager, nil)
	kv := newMockKVStore()
	kmsSvc.kvStore = kv
	kmsSvc.authzEnabled = true

	auditLog := &mockKeyUseAuditLogger{}
	kmsSvc.auditLogger = auditLog

	ctx := systemTenantCtx()
	kmsManager.keys["key1"] = &mockKeyData{keyID: "key1", status: "enabled"}
	policy := &KeyPolicy{
		KeyID:            "key1",
		DeniedPrincipals: []string{"user-1"},
	}
	data, _ := json.Marshal(policy)
	_ = kv.Put(ctx, kmsKeyPolicyKey("key1"), data)

	svc := newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	svc.kmsSvc = kmsSvc
	svc.authzEnabled = true

	userCtx := subjectCtx(authenticatedSubject("user-1", "alice"))
	_, err := svc.Encrypt(userCtx, &EncryptRequest{
		KeyID:     "key1",
		Plaintext: []byte("data"),
	})
	require.Error(t, err)

	require.Len(t, auditLog.events, 1)
	assert.Equal(t, keyUseOutcomeDeny, auditLog.events[0].outcome)
}

// newAuthzEnabledKMSWithoutStore builds the KMS service the way quicbaas.go
// does when no KV store provider is configured: authz on, no policy store.
func newAuthzEnabledKMSWithoutStore() *kmsServiceImpl {
	km := newMockKMSManager()
	km.keys["key1"] = &mockKeyData{keyID: "key1", status: "enabled"}
	svc := newKMSService(km, nil)
	WithKMSAuthzEnabled(true)(svc)
	return svc
}

func requireQBErrorCode(t *testing.T, err error, code errors.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, code, qe.Code)
}

func TestSetKeyPolicy_NoStore_ReturnsUnavailable(t *testing.T) {
	svc := newAuthzEnabledKMSWithoutStore()
	ctx := systemTenantCtx()

	_, err := svc.SetKeyPolicy(ctx, &SetKeyPolicyRequest{
		KeyID:  "key1",
		Policy: &KeyPolicy{KeyID: "key1", DeniedPrincipals: []string{"alice"}},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "key policy store is not configured")

	// Nothing was stored: once a store is attached, the key has no policy.
	svc.SetKVStore(newMockKVStore())
	policy, err := svc.loadKeyPolicy(ctx, "key1")
	require.NoError(t, err)
	assert.Nil(t, policy)
}

func TestCheckKeyAuthz_NoStore_DeniesAuthenticatedSubject(t *testing.T) {
	svc := newAuthzEnabledKMSWithoutStore()
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	err := svc.checkKeyAuthz(ctx, "key1", "encrypt")
	requireQBErrorCode(t, err, errors.ErrForbidden)
}

func TestCheckKeyAuthz_NoStore_SystemContextAllows(t *testing.T) {
	svc := newAuthzEnabledKMSWithoutStore()
	ctx := auth.NewSystemContext(systemTenantCtx())

	require.NoError(t, svc.checkKeyAuthz(ctx, "key1", "encrypt"))
}

func TestCheckKeyAuthz_StoreWithoutPolicy_Allows(t *testing.T) {
	svc := newAuthzEnabledKMSWithoutStore()
	svc.SetKVStore(newMockKVStore())
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	require.NoError(t, svc.checkKeyAuthz(ctx, "key1", "encrypt"))
}
