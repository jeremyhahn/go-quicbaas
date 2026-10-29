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
	stderrors "errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// tenantScopedKeyID tests
// =============================================================================

func TestTenantScopedKeyID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: key ID is returned unchanged
	keyID := "my-key"
	result := tenantScopedKeyID("", keyID)
	assert.Equal(t, keyID, result)
}

func TestTenantScopedKeyID_WithTenant(t *testing.T) {
	// Multi-tenant mode: key ID is prefixed with tenant namespace
	result := tenantScopedKeyID("acme-corp", "my-key")
	assert.Equal(t, "tenant.acme-corp.my-key", result)
}

func TestTenantScopedKeyID_PreservesComplexKeyID(t *testing.T) {
	// Key IDs with special characters are preserved
	result := tenantScopedKeyID("tenant-123", "key/with/slashes")
	assert.Equal(t, "tenant.tenant-123.key/with/slashes", result)
}

// =============================================================================
// tenantStripKeyID tests
// =============================================================================

func TestTenantStripKeyID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := tenantStripKeyID("", "my-key")
	assert.Equal(t, "my-key", result)
}

func TestTenantStripKeyID_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := tenantStripKeyID("acme-corp", "tenant.acme-corp.my-key")
	assert.Equal(t, "my-key", result)
}

func TestTenantStripKeyID_NoMatchingPrefix(t *testing.T) {
	// If the key doesn't have the tenant prefix, it's returned as-is
	result := tenantStripKeyID("acme-corp", "other-key")
	assert.Equal(t, "other-key", result)
}

func TestTenantStripKeyID_WrongTenant(t *testing.T) {
	// If the key belongs to a different tenant, the prefix is not stripped
	result := tenantStripKeyID("acme-corp", "tenant.other-corp.my-key")
	assert.Equal(t, "tenant.other-corp.my-key", result)
}

// =============================================================================
// tenantKeyBelongsTo tests
// =============================================================================

func TestTenantKeyBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all keys belong
	assert.True(t, tenantKeyBelongsTo("", "any-key"))
	assert.True(t, tenantKeyBelongsTo("", "tenant.acme-corp.key"))
}

func TestTenantKeyBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, tenantKeyBelongsTo("acme-corp", "tenant.acme-corp.my-key"))
}

func TestTenantKeyBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, tenantKeyBelongsTo("acme-corp", "tenant.other-corp.my-key"))
}

func TestTenantKeyBelongsTo_UnscopedKey(t *testing.T) {
	// A key without tenant prefix does not belong to a multi-tenant request
	assert.False(t, tenantKeyBelongsTo("acme-corp", "my-key"))
}

// =============================================================================
// tenantListPrefix tests
// =============================================================================

func TestTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", tenantListPrefix(""))
}

func TestTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant.acme-corp.", tenantListPrefix("acme-corp"))
}

// =============================================================================
// prepareTenantContext tests (go-quicbaas#1150)
// =============================================================================

// TestPrepareTenantContext_RefusesUnboundContext pins that an absent tenant is
// refused with the typed error, coded ErrForbidden, rather than read as
// system reach.
func TestPrepareTenantContext_RefusesUnboundContext(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), tenantctx.WithTenant(context.Background(), "")} {
		_, err := prepareTenantContext(ctx)
		var missing *tenantctx.MissingTenantError
		require.ErrorAs(t, err, &missing)
		var qb *errors.QBError
		require.ErrorAs(t, err, &qb)
		assert.Equal(t, errors.ErrForbidden, qb.Code)
	}
}

// TestPrepareTenantContext_ReturnsBoundTenant proves the bound tenant, the
// system tenant as the platform scope "", is returned.
func TestPrepareTenantContext_ReturnsBoundTenant(t *testing.T) {
	tenantID, err := prepareTenantContext(tenantctx.WithTenant(context.Background(), "my-tenant"))
	require.NoError(t, err)
	assert.Equal(t, "my-tenant", tenantID)

	tenantID, err = prepareTenantContext(tenantctx.WithSystem(context.Background()))
	require.NoError(t, err)
	assert.Equal(t, "", tenantID, "the system binding is the services' platform scope")
}

// =============================================================================
// validateTenantID tests
// =============================================================================

func TestValidateTenantID_Empty(t *testing.T) {
	// Empty is valid (single-tenant mode)
	assert.NoError(t, validateTenantID(""))
}

func TestValidateTenantID_ValidAlphanumeric(t *testing.T) {
	assert.NoError(t, validateTenantID("acme-corp"))
	assert.NoError(t, validateTenantID("tenant_123"))
	assert.NoError(t, validateTenantID("MyTenant"))
	assert.NoError(t, validateTenantID("a"))
	assert.NoError(t, validateTenantID("UPPERCASE"))
	assert.NoError(t, validateTenantID("mix-of_123-chars"))
}

func TestValidateTenantID_InvalidCharacters(t *testing.T) {
	// Special characters should fail
	err := validateTenantID("tenant/bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant@bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant.bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestValidateTenantID_TooLong(t *testing.T) {
	// Maximum 128 characters
	longID := strings.Repeat("a", 129)
	err := validateTenantID(longID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")

	// Exactly 128 should pass
	maxID := strings.Repeat("a", 128)
	assert.NoError(t, validateTenantID(maxID))
}

// =============================================================================
// isValidTenantChar tests
// =============================================================================

func TestIsValidTenantChar(t *testing.T) {
	tests := []struct {
		char  rune
		valid bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'-', true},
		{'_', true},
		{' ', false},
		{'/', false},
		{'.', false},
		{'@', false},
		{'!', false},
		{'#', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidTenantChar(tt.char))
		})
	}
}

// =============================================================================
// enforceTenantAccess tests
// =============================================================================

func TestEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, enforceTenantAccess("", "any-key", "GetKey"))
}

func TestEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, enforceTenantAccess("acme-corp", "tenant.acme-corp.my-key", "GetKey"))
}

func TestEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := enforceTenantAccess("acme-corp", "tenant.other-corp.my-key", "GetKey")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestEnforceTenantAccess_UnscopedKeyDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped key
	err := enforceTenantAccess("acme-corp", "unscoped-key", "DeleteKey")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// prepareTenantContext tests
// =============================================================================

func TestPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "invalid/tenant")
	_, err := prepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestTenantKeyID_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		keyID    string
	}{
		{"empty-tenant", "", "my-key"},
		{"with-tenant", "acme-corp", "my-key"},
		{"complex-key", "tenant-123", "sub/path/key"},
		{"underscore-tenant", "my_tenant", "key-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := tenantScopedKeyID(tt.tenantID, tt.keyID)
			stripped := tenantStripKeyID(tt.tenantID, scoped)
			assert.Equal(t, tt.keyID, stripped, "roundtrip should return original key ID")
		})
	}
}

func TestTenantKeyID_ScopeAndBelongsTo(t *testing.T) {
	// A scoped key should belong to the tenant that scoped it
	tenantID := "acme-corp"
	keyID := "my-key"
	scoped := tenantScopedKeyID(tenantID, keyID)
	assert.True(t, tenantKeyBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, tenantKeyBelongsTo("other-corp", scoped))
}

func TestTenantKeyID_ListPrefixMatchesScopedKeys(t *testing.T) {
	tenantID := "acme-corp"
	keyID := "my-key"
	scoped := tenantScopedKeyID(tenantID, keyID)
	prefix := tenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped key should start with tenant list prefix")
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestKMSTenant_SystemTenantIsPlatformScoped proves a caller carrying the
// literal system tenant is treated identically to single-tenant mode (no key
// prefixing, unrestricted access) rather than being isolated into a tenant
// namespace named "system".
func TestKMSTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "my-key", tenantScopedKeyID(tenantID, "my-key"))
			assert.True(t, tenantKeyBelongsTo(tenantID, "any-key"))
			assert.NoError(t, enforceTenantAccess(tenantID, "any-key", "GetKey"))
		})
	}
}

// TestKMSTenant_OrdinaryTenantStaysIsolated proves the system-tenant carve-out
// does not widen ordinary tenants' reach: a lookalike ID that merely shares
// the "system" prefix is still fully isolated.
func TestKMSTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := tenantScopedKeyID("system-2", "my-key")
	assert.Equal(t, "tenant.system-2.my-key", scoped)
	assert.False(t, tenantKeyBelongsTo("system-2", "my-key"))
	err := enforceTenantAccess("system-2", "my-key", "GetKey")
	require.Error(t, err)
}

// xkmsKeyReference builds the key reference the XKMS backend hands to xkms
// for an encryption key: "backend:encryption:algorithm:keyname".
func xkmsKeyReference(keyName string) string {
	return "software:encryption:aes256-gcm:" + keyName
}

// A tenant-scoped key ID is the keyname of the key reference every crypto
// path builds, so xkms must accept it as one.
func TestTenantScopedKeyID_IsAValidXKMSKeyReference(t *testing.T) {
	scoped := tenantScopedKeyID("acme-corp", "my_key-1.v2")
	_, _, _, keyName, err := xkmssdk.ServiceParseKeyID(xkmsKeyReference(scoped))
	require.NoError(t, err, "xkms must accept the tenant-scoped key ID %q", scoped)
	assert.Equal(t, scoped, keyName, "the keyname xkms resolves is the scoped key ID")
}

// The key reference check xkms applies refuses a keyname outside its
// character set, which the tenant namespace delimiter must never introduce.
func TestTenantScopedKeyID_XKMSRefusesAnInvalidKeyName(t *testing.T) {
	_, _, _, _, err := xkmssdk.ServiceParseKeyID(xkmsKeyReference("tenant/acme-corp/my-key"))
	require.Error(t, err, "xkms must refuse a keyname containing '/'")
}

// seedTenantKeyOwners gives svc an in-memory ownership store when it has
// none and records tenantID as the owner of each named key, as a tenant's
// create would.
func seedTenantKeyOwners(t *testing.T, svc *kmsServiceImpl, tenantID string, names ...string) {
	t.Helper()
	if svc.kvStore == nil {
		svc.SetKVStore(qrdbsdk.NewMemoryKVStore())
	}
	for _, name := range names {
		require.NoError(t, svc.kvStore.Put(tenantctx.WithSystem(context.Background()),
			kmsOwnerRecordKey(tenantScopedKeyID(tenantID, name)), []byte(tenantID)))
	}
}

// ownershipFailingStore is a KV store whose reads fail, so a test can prove
// a store fault surfaces as a typed storage error.
type ownershipFailingStore struct {
	interfaces.KVStore
}

func (s *ownershipFailingStore) Get(context.Context, string) ([]byte, error) {
	return nil, stderrors.New("disk on fire")
}

const (
	ownershipTenant      = "acme"
	ownershipOtherTenant = "globex"
	ownershipKey         = "k"
)

// newOwnershipKMS returns a KMS service over a manager that holds the key
// tenant.acme.k and counts the GetKey calls that reach it.
func newOwnershipKMS(t *testing.T) (*kmsServiceImpl, *mockKMSManager, *int) {
	t.Helper()
	mgr := newMockKMSManager()
	scoped := tenantScopedKeyID(ownershipTenant, ownershipKey)
	mgr.keys[scoped] = &mockKeyData{keyID: scoped, algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled"}
	reached := 0
	mgr.getKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		reached++
		if key, ok := mgr.keys[keyID]; ok {
			return &KeyMetadata{KeyID: key.keyID, Algorithm: KeyAlgorithm(key.algorithm), Version: key.version, Status: KeyStatusEnabled}, nil
		}
		return nil, kms.ErrKeyNotFound
	}
	svc := newKMSService(mgr, nil)
	svc.SetKVStore(qrdbsdk.NewMemoryKVStore())
	return svc, mgr, &reached
}

func requireKeyOwnershipError(t *testing.T, err error, tenantID, key string) {
	t.Helper()
	require.Error(t, err)
	var ownErr *KeyOwnershipError
	require.True(t, stderrors.As(err, &ownErr), "want KeyOwnershipError, got %v", err)
	require.Equal(t, tenantID, ownErr.TenantID)
	require.Equal(t, key, ownErr.Key)
}

func TestKMSOwnership_MatchingRecordSucceeds(t *testing.T) {
	svc, _, reached := newOwnershipKMS(t)
	seedTenantKeyOwners(t, svc, ownershipTenant, ownershipKey)

	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	resp, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})

	require.NoError(t, err)
	require.Equal(t, ownershipKey, resp.Key.KeyID)
	require.Equal(t, 1, *reached)
}

// TestKMSOwnership_LegacySystemKeyIsRefused proves a key named tenant.acme.k
// by the system tenant, which has no ownership record, is unreachable from
// tenant acme and never reaches the manager.
func TestKMSOwnership_LegacySystemKeyIsRefused(t *testing.T) {
	svc, _, reached := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})
	requireKeyOwnershipError(t, err, ownershipTenant, ownershipKey)
	require.True(t, IsNotFound(err), "a refused key reads as not found, got %v", err)

	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: ownershipKey})
	requireKeyOwnershipError(t, err, ownershipTenant, ownershipKey)
	require.Zero(t, *reached, "a refused key must never reach the manager")
}

func TestKMSOwnership_OtherTenantsRecordIsRefused(t *testing.T) {
	svc, _, reached := newOwnershipKMS(t)
	scoped := tenantScopedKeyID(ownershipTenant, ownershipKey)
	require.NoError(t, svc.kvStore.Put(context.Background(), kmsOwnerRecordKey(scoped), []byte(ownershipOtherTenant)))

	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	_, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})

	requireKeyOwnershipError(t, err, ownershipTenant, ownershipKey)
	require.Zero(t, *reached)
}

func TestKMSOwnership_SystemTenantNeedsNoRecord(t *testing.T) {
	svc, _, reached := newOwnershipKMS(t)

	_, err := svc.GetKey(tenantctx.WithSystem(context.Background()),
		&GetKeyRequest{KeyID: tenantScopedKeyID(ownershipTenant, ownershipKey)})

	require.NoError(t, err)
	require.Equal(t, 1, *reached)
}

func TestKMSOwnership_CreateRecordsAndDeleteReleases(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "fresh"
	req := &GenerateKeyRequest{
		Name:          name,
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	}

	_, err := svc.GenerateKey(ctx, req)
	require.NoError(t, err)
	owner, err := svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, name), "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner)

	svc.keyNames.Delete(tenantScopedKeyID(ownershipTenant, name))
	_, err = svc.GenerateKey(ctx, req)
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrAlreadyExists, qe.Code, "a recorded name must not be created twice")

	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)
	owner, err = svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, name), "test")
	require.NoError(t, err)
	require.Empty(t, owner, "delete must remove the ownership record")
}

func TestKMSOwnership_FailedCreateLeavesNoRecord(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	mgr.generateKeyFn = func(context.Context, *xkmssdk.KeyAttributes) (interface{}, error) {
		return nil, stderrors.New("backend down")
	}
	mgr.importKeyFn = func(context.Context, string, []byte, string) (interface{}, error) {
		return nil, stderrors.New("backend down")
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
		Name:          "gen",
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	})
	require.ErrorContains(t, err, "failed to generate key")

	_, err = svc.ImportKey(ctx, &ImportKeyRequest{
		Name:          "imp",
		KeyMaterial:   make([]byte, 32),
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	})
	require.ErrorContains(t, err, "failed to import key")

	for _, name := range []string{"gen", "imp"} {
		owner, readErr := svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, name), "test")
		require.NoError(t, readErr)
		require.Empty(t, owner, "a failed create of %q must leave no record", name)
	}
}

func TestKMSOwnership_StoreFaultsAreTyped(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	t.Run("read error", func(t *testing.T) {
		svc, _, reached := newOwnershipKMS(t)
		svc.SetKVStore(&ownershipFailingStore{KVStore: qrdbsdk.NewMemoryKVStore()})

		_, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})

		require.True(t, IsStorageError(err), "want a storage error, got %v", err)
		require.Zero(t, *reached)
	})

	t.Run("no store", func(t *testing.T) {
		svc, _, reached := newOwnershipKMS(t)
		svc.SetKVStore(nil)

		_, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})

		var unset *KeyOwnershipStoreUnsetError
		require.True(t, stderrors.As(err, &unset), "want KeyOwnershipStoreUnsetError, got %v", err)
		require.Zero(t, *reached)
	})
}

// TestKMSOwnership_ImportOverUnownedKeyClaimsNothing pins Codex 8340: a
// failed import of a name that already holds a legacy system key must not
// leave the tenant a record that makes the platform key its own.
func TestKMSOwnership_ImportOverUnownedKeyClaimsNothing(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	imported := 0
	mgr.importKeyFn = func(context.Context, string, []byte, string) (interface{}, error) {
		imported++
		return nil, stderrors.New("malformed key material")
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.ImportKey(ctx, &ImportKeyRequest{
		Name:          ownershipKey,
		KeyMaterial:   []byte{0x01, 0x02, 0x03},
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	})
	requireKeyOwnershipError(t, err, ownershipTenant, ownershipKey)
	require.Zero(t, imported, "an existing key's name must be refused before the import runs")

	owner, err := svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, ownershipKey), "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a refused import must write no record")

	_, err = svc.GetKey(ctx, &GetKeyRequest{KeyID: ownershipKey})
	requireKeyOwnershipError(t, err, ownershipTenant, ownershipKey)
}

func TestKMSOwnership_CreateOverOwnedKeyIsAlreadyExists(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	seedTenantKeyOwners(t, svc, ownershipTenant, ownershipKey)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.claimTenantKey(ctx, ownershipTenant, ownershipKey, "test")

	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrAlreadyExists, qe.Code)
}

func TestKMSOwnership_ConcurrentClaimsHaveOneWinner(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	// The fixture's GetKey counts calls; a stateless one keeps the race
	// detector on the claim itself.
	mgr.getKeyFn = func(context.Context, string) (interface{}, error) {
		return nil, kms.ErrKeyNotFound
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const contenders = 8

	start := make(chan struct{})
	claims := make([]*tenantKeyClaim, contenders)
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claims[i], errs[i] = svc.claimTenantKey(ctx, ownershipTenant, "contested", "test")
		}()
	}
	close(start)
	wg.Wait()

	winners := 0
	for i := range contenders {
		if errs[i] == nil {
			winners++
			continue
		}
		var qe *errors.QBError
		require.True(t, stderrors.As(errs[i], &qe), "want a QBError, got %v", errs[i])
		require.Equal(t, errors.ErrAlreadyExists, qe.Code, "a losing claim must be already-exists")
		require.Nil(t, claims[i])
	}
	require.Equal(t, 1, winners, "exactly one claim on a name may win")
}

func TestKMSOwnership_RollbackLeavesAnotherRequestsRecord(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	scopedID := tenantScopedKeyID(ownershipTenant, "contested")

	first, err := svc.claimTenantKey(ctx, ownershipTenant, "contested", "test")
	require.NoError(t, err)
	createErr := stderrors.New("backend down")
	require.Equal(t, createErr, svc.abandonTenantKeyClaim(ctx, first, "test", createErr))

	winner, err := svc.claimTenantKey(ctx, ownershipTenant, "contested", "test")
	require.NoError(t, err)

	// The first request's rollback runs again, late: its value is no longer
	// the record's, so the winner's record must survive it.
	require.Equal(t, createErr, svc.abandonTenantKeyClaim(ctx, first, "test", createErr))

	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID))
	require.NoError(t, err)
	require.Equal(t, winner.value, stored, "a stale rollback must not erase the winner's record")
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner)
}

// ownershipPlainStore hides the atomic methods of the store it wraps.
type ownershipPlainStore struct {
	interfaces.KVStore
}

func TestKMSOwnership_NonAtomicStoreIsRefused(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	svc.SetKVStore(&ownershipPlainStore{KVStore: qrdbsdk.NewMemoryKVStore()})
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
		Name:          "fresh",
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	})

	var notAtomic *KeyOwnershipStoreNotAtomicError
	require.True(t, stderrors.As(err, &notAtomic), "want KeyOwnershipStoreNotAtomicError, got %v", err)
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe))
	require.Equal(t, errors.ErrUnavailable, qe.Code)
}

func TestKMSOwnership_ListShowsOnlyOwnedKeys(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	owned := tenantScopedKeyID(ownershipTenant, "mine")
	mgr.keys[owned] = &mockKeyData{keyID: owned, algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled"}
	seedTenantKeyOwners(t, svc, ownershipTenant, "mine")
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	resp, err := svc.ListKeys(ctx, &ListKeysRequest{})

	require.NoError(t, err)
	require.Len(t, resp.Keys, 1, "the unrecorded tenant.acme.k must be hidden")
	require.Equal(t, "mine", resp.Keys[0].KeyID)
	require.Equal(t, 1, resp.TotalCount)
}
