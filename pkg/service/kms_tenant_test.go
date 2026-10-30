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
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
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

func TestKMSOwnership_StaleDeleteLeavesRecreatedKeysRecord(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "recycled"
	req := &GenerateKeyRequest{
		Name:          name,
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	}
	_, err := svc.GenerateKey(ctx, req)
	require.NoError(t, err)

	// Delete A resolves the first generation's record, then stalls.
	scopedID, staleRecord, err := svc.resolveDeletedKey(ctx, ownershipTenant, name, "DeleteKey")
	require.NoError(t, err)

	// Delete B runs to completion, and the name is created again.
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)
	_, err = svc.GenerateKey(ctx, req)
	require.NoError(t, err)

	// Delete A's release runs late: the record is the new generation's.
	require.NoError(t, svc.releaseTenantKey(ctx, scopedID, staleRecord, "DeleteKey"))

	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID))
	require.NoError(t, err)
	require.NotEqual(t, staleRecord, stored, "a stale delete must not erase the recreated key's record")
	_, err = svc.GetKey(ctx, &GetKeyRequest{KeyID: name})
	require.NoError(t, err, "the tenant must still reach its recreated key")
}

func TestKMSOwnership_SystemDeleteOfTenantKeyReleasesRecord(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	sysCtx := tenantctx.WithSystem(context.Background())
	const name = "reclaimed"
	req := &GenerateKeyRequest{
		Name:          name,
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	}
	_, err := svc.GenerateKey(ctx, req)
	require.NoError(t, err)

	_, err = svc.DeleteKey(sysCtx, &DeleteKeyRequest{KeyID: tenantScopedKeyID(ownershipTenant, name)})
	require.NoError(t, err)
	owner, err := svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, name), "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a system delete must release the tenant's record")

	_, err = svc.GenerateKey(ctx, req)
	require.NoError(t, err, "the tenant must be able to create the name again")
}

func TestKMSOwnership_SystemDeleteOfSystemKeyNeedsNoStore(t *testing.T) {
	mgr := newMockKMSManager()
	mgr.keys["platform"] = &mockKeyData{keyID: "platform", algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled"}
	svc := newKMSService(mgr, nil)

	_, err := svc.DeleteKey(tenantctx.WithSystem(context.Background()), &DeleteKeyRequest{KeyID: "platform"})

	require.NoError(t, err, "a system key has no ownership record to release")
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

// ownershipServerShapedStore has the method set of the server's guarded KV
// store: the six interfaces.KVStore verbs and the three guarded writes, and
// none of go-qrdb's secondary-index methods.
type ownershipServerShapedStore struct {
	interfaces.GuardedKVStore
}

// TestKMSOwnership_ServerShapedGuardedStoreIsAccepted pins the round 34
// Integration failure on go-quicbaas#1993: a store carrying the guarded writes
// but not go-qrdb's index methods was refused as "not atomic", so every tenant
// key create against the real server failed.
func TestKMSOwnership_ServerShapedGuardedStoreIsAccepted(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	svc.SetKVStore(&ownershipServerShapedStore{GuardedKVStore: qrdbsdk.NewMemoryKVStore()})
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("fresh"))
	require.NoError(t, err)
	owner, err := svc.readKeyOwner(ctx, tenantScopedKeyID(ownershipTenant, "fresh"), "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner, "the create must record the tenant's claim")
}

// ownershipCancelRefusingStore refuses writes on a cancelled context, as a
// qrdb node refuses a proposal whose request was cancelled.
type ownershipCancelRefusingStore struct {
	qrdbsdk.AtomicKVStore
}

func (s *ownershipCancelRefusingStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.AtomicKVStore.PutIfAbsent(ctx, key, value)
}

func (s *ownershipCancelRefusingStore) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.AtomicKVStore.CompareAndDelete(ctx, key, expected)
}

func (s *ownershipCancelRefusingStore) CompareAndSwap(ctx context.Context, key string, expected, value []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.AtomicKVStore.CompareAndSwap(ctx, key, expected, value)
}

// ownershipGenerateRequest is a create of name that the fixtures accept.
func ownershipGenerateRequest(name string) *GenerateKeyRequest {
	return &GenerateKeyRequest{
		Name:          name,
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	}
}

func TestKMSOwnership_CancelledCreateReleasesClaim(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	svc.SetKVStore(&ownershipCancelRefusingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()})
	ctx, cancel := context.WithCancel(tenantctx.WithTenant(context.Background(), ownershipTenant))
	defer cancel()
	mgr.generateKeyFn = func(context.Context, *xkmssdk.KeyAttributes) (interface{}, error) {
		// The request is cancelled after the reservation, mid-create.
		cancel()
		return nil, context.Canceled
	}

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("cancelled"))
	require.Error(t, err)
	owner, err := svc.readKeyOwner(context.Background(), tenantScopedKeyID(ownershipTenant, "cancelled"), "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a cancelled create must still release its claim")

	mgr.generateKeyFn = nil
	_, err = svc.GenerateKey(tenantctx.WithTenant(context.Background(), ownershipTenant), ownershipGenerateRequest("cancelled"))
	require.NoError(t, err, "a retry must be able to create the name")
}

// TestKMSOwnership_DeleteLeavesPendingClaim pauses a create after it has
// reserved the name and deletes the name in that window: the backend has no
// key yet, but the reservation belongs to the create in flight, so the delete
// must leave it and the create must finish with a reachable key.
func TestKMSOwnership_DeleteLeavesPendingClaim(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "in-flight"
	scopedID := tenantScopedKeyID(ownershipTenant, name)

	reserved := make(chan struct{})
	release := make(chan struct{})
	mgr.generateKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		close(reserved)
		<-release
		mgr.keys[attrs.CN] = &mockKeyData{keyID: attrs.CN, algorithm: string(attrs.SymmetricAlgorithm), version: 1, status: "enabled"}
		return mgr.keys[attrs.CN], nil
	}
	mgr.deleteKeyFn = func(context.Context, string) error {
		return kms.ErrKeyNotFound
	}

	generated := make(chan error, 1)
	go func() {
		_, err := svc.GenerateKey(ctx, ownershipGenerateRequest(name))
		generated <- err
	}()
	<-reserved

	_, err := svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrNotFound, qe.Code)

	close(release)
	require.NoError(t, <-generated)

	_, err = svc.GetKey(ctx, &GetKeyRequest{KeyID: name})
	require.NoError(t, err, "the created key must stay reachable")
	record, err := svc.readKeyOwnerRecord(ctx, scopedID, "test")
	require.NoError(t, err)
	_, completed, err := tenantKeyClaimTime(record)
	require.NoError(t, err)
	require.True(t, completed, "the create must complete its claim")
}

// TestKMSOwnership_DeleteKeepsCompletedRecordOnUnconfirmedMiss shows that a
// not-found from the backend does not release a completed record, since the
// backend folds lookup failures into it, and that a later delete does.
func TestKMSOwnership_DeleteKeepsCompletedRecordOnUnconfirmedMiss(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "cold-cache"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err)

	// After a restart the backend's metadata cache is cold, and a transient
	// key-store failure surfaces as ErrKeyNotFound although the key survives.
	mgr.deleteKeyFn = func(context.Context, string) error {
		return kms.ErrKeyNotFound
	}
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrNotFound, qe.Code)
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner, "an unconfirmed miss must keep the ownership record")
	_, err = svc.GetKey(ctx, &GetKeyRequest{KeyID: name})
	require.NoError(t, err, "the surviving key must stay reachable by its tenant")

	// Once the backend recovers, the retried delete removes the key and
	// releases the name for reuse.
	mgr.deleteKeyFn = nil
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)
	owner, err = svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a confirmed delete must release the ownership record")
	_, err = svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err, "the released name must be reusable")
}

// ownershipReleaseFailingStore fails CompareAndDelete while fail is set, as a
// store outage between a backend delete and the release of its record would.
type ownershipReleaseFailingStore struct {
	qrdbsdk.AtomicKVStore
	fail bool
}

func (s *ownershipReleaseFailingStore) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if s.fail {
		return false, stderrors.New("store unavailable")
	}
	return s.AtomicKVStore.CompareAndDelete(ctx, key, expected)
}

// TestKMSOwnership_DeleteRetryReleasesDeleteIntent shows that when the release
// fails after a successful backend delete, the retry, which meets
// ErrKeyNotFound, releases the record carrying a delete intent and the name is reusable.
func TestKMSOwnership_DeleteRetryReleasesDeleteIntent(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	store := &ownershipReleaseFailingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()}
	svc.SetKVStore(store)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "half-deleted"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err)

	store.fail = true
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.Error(t, err, "a failed release must surface")
	record, err := svc.readKeyOwnerRecord(ctx, scopedID, "test")
	require.NoError(t, err)
	require.True(t, tenantKeyRecordDeleting(record), "a backend delete whose release failed must leave its intent, got %q", record)

	// The backend no longer has the key and reports it as the real one does.
	store.fail = false
	mgr.deleteKeyFn = func(context.Context, string) error {
		return kms.ErrKeyNotFound
	}
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.Error(t, err)
	mgr.deleteKeyFn = nil
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrNotFound, qe.Code)
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Empty(t, owner, "the retry must release the record carrying a delete intent")
	_, err = svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err, "the released name must be reusable")
}

// TestKMSOwnership_DeleteIntentIsReclaimed shows that a create reclaims a
// record carrying a delete intent at once, however fresh it is.
func TestKMSOwnership_DeleteIntentIsReclaimed(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "tombstoned"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	store, err := svc.atomicOwnerStore("test")
	require.NoError(t, err)
	tombstone := []byte(ownershipTenant + kmsOwnerRecordSeparator + "cafe" + kmsOwnerRecordSeparator + kmsOwnerRecordDeleting)
	stored, err := store.PutIfAbsent(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID), tombstone)
	require.NoError(t, err)
	require.True(t, stored)

	_, err = svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err, "a delete intent block a new key of the same name")
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner)
}

func TestTenantKeyRecordDeleting(t *testing.T) {
	require.True(t, tenantKeyRecordDeleting([]byte("t:h:"+kmsOwnerRecordDeleting)))
	require.False(t, tenantKeyRecordDeleting([]byte("t:h:"+kmsOwnerRecordCompleted)))
	require.False(t, tenantKeyRecordDeleting([]byte("t:h:42")))
	require.False(t, tenantKeyRecordDeleting(nil))
	require.False(t, tenantKeyRecordDeleting([]byte(":h:"+kmsOwnerRecordDeleting)))
}

// seedOwnershipClaim writes a claim record for name, stamped at claimedAt,
// with no key behind it, as a failed cleanup leaves one.
func seedOwnershipClaim(t *testing.T, svc *kmsServiceImpl, name string, claimedAt time.Time) []byte {
	t.Helper()
	record := []byte(ownershipTenant + kmsOwnerRecordSeparator + "deadbeef" + kmsOwnerRecordSeparator +
		strconv.FormatInt(claimedAt.UTC().UnixNano(), 10))
	require.NoError(t, svc.kvStore.Put(tenantctx.WithSystem(context.Background()),
		kmsOwnerRecordKey(tenantScopedKeyID(ownershipTenant, name)), record))
	return record
}

// errRestartedBackendOutage is the create failure of a restartedBackend in
// an outage.
var errRestartedBackendOutage = stderrors.New("backend unavailable")

// restartedBackend behaves as the production XKMSBackend does after a
// restart: in an outage its metadata lookup reports kms.ErrKeyNotFound even
// for a live key. It embeds kms.Backend and defines only the methods the
// production KMSService calls on the generate path.
type restartedBackend struct {
	kms.Backend
	mu     sync.Mutex
	keys   map[string]*kms.KeyMetadata
	outage bool
	// deleted, when set, runs after a successful DeleteKey.
	deleted func()
}

func (b *restartedBackend) Type() config.BackendType { return config.BackendTypeSoftware }

func (b *restartedBackend) IsAvailable(context.Context) bool { return true }

func (b *restartedBackend) SupportedAlgorithms(context.Context) []string {
	return []string{kms.AlgAES256GCM}
}

func (b *restartedBackend) setOutage(outage bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.outage = outage
}

func (b *restartedBackend) CreateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (*kms.KeyMetadata, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.outage {
		return nil, errRestartedBackendOutage
	}
	meta := &kms.KeyMetadata{KeyID: attrs.CN, Name: attrs.CN, Algorithm: kms.AlgAES256GCM, State: kms.KeyStateEnabled, Version: 1}
	b.keys[attrs.CN] = meta
	return meta, nil
}

func (b *restartedBackend) GetKeyMetadata(_ context.Context, keyID string) (*kms.KeyMetadata, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	meta, ok := b.keys[keyID]
	if b.outage || !ok {
		return nil, fmt.Errorf("signer lookup: %w", kms.ErrKeyNotFound)
	}
	return meta, nil
}

func (b *restartedBackend) DeleteKey(_ context.Context, keyID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.outage {
		return errRestartedBackendOutage
	}
	if _, ok := b.keys[keyID]; !ok {
		return fmt.Errorf("delete: %w", kms.ErrKeyNotFound)
	}
	delete(b.keys, keyID)
	if b.deleted != nil {
		b.deleted()
	}
	return nil
}

func (b *restartedBackend) hasKey(keyID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.keys[keyID]
	return ok
}

// newProductionChainKMS wires the KMS service to the production
// kms.KMSService, with a fresh metadata cache, over backend and the
// ownership store kv.
func newProductionChainKMS(t *testing.T, backend kms.Backend, kv interfaces.KVStore) *kmsServiceImpl {
	t.Helper()
	registry := kms.NewBackendRegistry(nil)
	require.NoError(t, registry.Register("software", backend, 1))
	mgr, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registry})
	require.NoError(t, err)
	svc := newKMSService(mgr, nil)
	svc.SetKVStore(kv)
	return svc
}

func TestKMSOwnership_CompletedRecordSurvivesBackendMissAfterRestart(t *testing.T) {
	backend := &restartedBackend{keys: map[string]*kms.KeyMetadata{}}
	kv := qrdbsdk.NewMemoryKVStore()
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	scopedID := tenantScopedKeyID(ownershipTenant, "durable")
	recordKey := kmsOwnerRecordKey(scopedID)

	_, err := newProductionChainKMS(t, backend, kv).GenerateKey(ctx, ownershipGenerateRequest("durable"))
	require.NoError(t, err)
	completed, err := kv.Get(tenantctx.WithSystem(ctx), recordKey)
	require.NoError(t, err)
	_, done, err := tenantKeyClaimTime(completed)
	require.NoError(t, err)
	require.True(t, done, "a successful create must mark its ownership record completed")

	// After a restart the metadata cache is empty, the backend reports the
	// live key as not found, and the record is far past the stale age.
	backend.setOutage(true)
	restarted := newProductionChainKMS(t, backend, kv)
	later := time.Now().Add(kmsClaimStaleAfter + time.Hour)
	restarted.clock = func() time.Time { return later }

	_, err = restarted.GenerateKey(ctx, ownershipGenerateRequest("durable"))
	require.Error(t, err)

	stored, err := kv.Get(tenantctx.WithSystem(ctx), recordKey)
	require.NoError(t, err, "a backend miss must not delete a completed ownership record")
	require.Equal(t, completed, stored)
	backend.setOutage(false)
	owner, err := restarted.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner, "the tenant must keep its key once the backend recovers")
}

func TestKMSOwnership_CompleteClaimLeavesReplacedRecord(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	claim, err := svc.claimTenantKey(ctx, ownershipTenant, "replaced", "test")
	require.NoError(t, err)
	pending := claim.value
	other := []byte("other" + kmsOwnerRecordSeparator + "cafe" + kmsOwnerRecordSeparator + kmsOwnerRecordCompleted)
	require.NoError(t, svc.kvStore.Put(tenantctx.WithSystem(ctx), claim.key, other))

	err = svc.completeTenantKeyClaim(ctx, claim, "test")
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrConflict, qe.Code)
	var changed *KeyOwnershipRecordChangedError
	require.True(t, stderrors.As(err, &changed))

	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), claim.key)
	require.NoError(t, err)
	require.Equal(t, other, stored, "a lost compare must not overwrite the record")
	require.Equal(t, pending, claim.value, "a failed mark must leave the claim pending")
}

// errOwnershipSwapFault is the fault ownershipSwapFailingStore injects.
var errOwnershipSwapFault = stderrors.New("compare-and-swap unavailable")

// ownershipSwapFailingStore fails the CompareAndSwap calls that move an
// ownership record to state: the first failures of them, or all of them
// when failures is negative. With applied set, a failing swap writes its
// value before reporting the fault, as a store whose reply is lost does.
// attempts counts the swaps to state.
type ownershipSwapFailingStore struct {
	qrdbsdk.AtomicKVStore
	state    string
	failures int
	applied  bool
	attempts int
}

func (s *ownershipSwapFailingStore) CompareAndSwap(ctx context.Context, key string, expected, value []byte) (bool, error) {
	if !strings.HasSuffix(string(value), kmsOwnerRecordSeparator+s.state) {
		return s.AtomicKVStore.CompareAndSwap(ctx, key, expected, value)
	}
	s.attempts++
	if s.failures == 0 {
		return s.AtomicKVStore.CompareAndSwap(ctx, key, expected, value)
	}
	if s.failures > 0 {
		s.failures--
	}
	if s.applied {
		if _, err := s.AtomicKVStore.CompareAndSwap(ctx, key, expected, value); err != nil {
			return false, err
		}
	}
	return false, errOwnershipSwapFault
}

// requireCompletedRecord asserts that scopedID's ownership record is
// completed and owned by ownershipTenant.
func requireCompletedRecord(t *testing.T, svc *kmsServiceImpl, scopedID string) {
	t.Helper()
	record, err := svc.readKeyOwnerRecord(tenantctx.WithSystem(context.Background()), scopedID, "test")
	require.NoError(t, err)
	_, completed, err := tenantKeyClaimTime(record)
	require.NoError(t, err)
	require.True(t, completed, "want a completed record, got %q", record)
	owner, _, _ := strings.Cut(string(record), kmsOwnerRecordSeparator)
	require.Equal(t, ownershipTenant, owner)
}

// TestKMSOwnership_FailedCompletionRollsBackCreate drives a create through
// the production manager chain over a store that never completes the
// ownership record. The create must fail and delete the key it made, so no
// live key is left behind a pending record a later create could reclaim,
// and the name is free once the store recovers.
func TestKMSOwnership_FailedCompletionRollsBackCreate(t *testing.T) {
	backend := &restartedBackend{keys: map[string]*kms.KeyMetadata{}}
	store := &ownershipSwapFailingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore(), state: kmsOwnerRecordCompleted, failures: -1}
	svc := newProductionChainKMS(t, backend, store)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	scopedID := tenantScopedKeyID(ownershipTenant, "unmarked")

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("unmarked"))
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrStorage, qe.Code)
	require.ErrorIs(t, err, errOwnershipSwapFault)
	require.Equal(t, kmsClaimCompleteAttempts, store.attempts, "a store fault must be retried, bounded")
	require.False(t, backend.hasKey(scopedID), "a create that cannot complete its record must delete its key")
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Empty(t, owner, "the rollback must release the claim")

	store.failures = 0
	_, err = svc.GenerateKey(ctx, ownershipGenerateRequest("unmarked"))
	require.NoError(t, err, "the name must be free once the store recovers")
	requireCompletedRecord(t, svc, scopedID)
}

// TestKMSOwnership_CompletionRetriesTransientFault shows that one failed
// completion write is retried, including one that applied although its
// reply was lost, and the create succeeds with a completed record.
func TestKMSOwnership_CompletionRetriesTransientFault(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprintf("applied=%t", applied), func(t *testing.T) {
			backend := &restartedBackend{keys: map[string]*kms.KeyMetadata{}}
			store := &ownershipSwapFailingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore(), state: kmsOwnerRecordCompleted, failures: 1, applied: applied}
			svc := newProductionChainKMS(t, backend, store)
			ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
			scopedID := tenantScopedKeyID(ownershipTenant, "flaky")

			_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("flaky"))
			require.NoError(t, err)
			require.Equal(t, 2, store.attempts)
			require.True(t, backend.hasKey(scopedID))
			requireCompletedRecord(t, svc, scopedID)
		})
	}
}

// TestKMSOwnership_UndeletableKeyKeepsItsClaim shows that when the rollback
// of an uncompleted create cannot delete the key, the claim is kept, so the
// key stays its tenant's to delete instead of becoming a key nobody owns.
func TestKMSOwnership_UndeletableKeyKeepsItsClaim(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	svc.SetKVStore(&ownershipSwapFailingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore(), state: kmsOwnerRecordCompleted, failures: -1})
	mgr.deleteKeyFn = func(context.Context, string) error {
		return stderrors.New("hsm unavailable")
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	scopedID := tenantScopedKeyID(ownershipTenant, "stuck")

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("stuck"))
	require.ErrorIs(t, err, errOwnershipSwapFault)
	require.Contains(t, mgr.keys, scopedID)
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner, "a key the rollback could not delete must keep its claim")
}

// TestKMSOwnership_CancelledCreateStillCompletes shows that a request
// cancelled after its key was created still completes the record.
func TestKMSOwnership_CancelledCreateStillCompletes(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	svc.SetKVStore(&ownershipCancelRefusingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()})
	ctx, cancel := context.WithCancel(tenantctx.WithTenant(context.Background(), ownershipTenant))
	defer cancel()
	mgr.generateKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		mgr.keys[attrs.CN] = &mockKeyData{keyID: attrs.CN, algorithm: string(attrs.SymmetricAlgorithm), version: 1, status: "enabled"}
		cancel()
		return mgr.keys[attrs.CN], nil
	}

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("late-cancel"))
	require.NoError(t, err)
	requireCompletedRecord(t, svc, tenantScopedKeyID(ownershipTenant, "late-cancel"))
}

// TestKMSOwnership_FailedDeleteIntentKeepsKey drives a delete through the
// production manager chain over a store that refuses the deletion intent.
// The delete must fail before it touches the key, so the key and its
// completed record survive, and the retry deletes it and frees the name.
func TestKMSOwnership_FailedDeleteIntentKeepsKey(t *testing.T) {
	backend := &restartedBackend{keys: map[string]*kms.KeyMetadata{}}
	store := &ownershipSwapFailingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore(), state: kmsOwnerRecordDeleting}
	svc := newProductionChainKMS(t, backend, store)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "intent-refused"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err)

	store.failures = -1
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.ErrorIs(t, err, errOwnershipSwapFault)
	require.True(t, backend.hasKey(scopedID), "a delete whose intent was not written must not delete the key")
	requireCompletedRecord(t, svc, scopedID)

	store.failures = 0
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)
	require.False(t, backend.hasKey(scopedID))
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a confirmed delete must release the ownership record")
	_, err = svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err, "the released name must be reusable")
}

// TestKMSOwnership_CancelledDeleteStillReleases drives a delete through
// the production manager chain whose request is cancelled once the backend
// delete has run. The release outlives the request and frees the name.
func TestKMSOwnership_CancelledDeleteStillReleases(t *testing.T) {
	backend := &restartedBackend{keys: map[string]*kms.KeyMetadata{}}
	svc := newProductionChainKMS(t, backend, &ownershipCancelRefusingStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()})
	const name = "cancelled-delete"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	_, err := svc.GenerateKey(tenantctx.WithTenant(context.Background(), ownershipTenant), ownershipGenerateRequest(name))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(tenantctx.WithTenant(context.Background(), ownershipTenant))
	defer cancel()
	backend.deleted = cancel
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)
	require.False(t, backend.hasKey(scopedID))
	owner, err := svc.readKeyOwner(context.Background(), scopedID, "test")
	require.NoError(t, err)
	require.Empty(t, owner, "a delete cancelled after the backend delete must still release the record")
}

// TestKMSOwnership_FailedBackendDeleteRestoresRecord shows that a backend
// delete that fails puts the record back as it was, so the surviving key
// is never mistaken for one whose delete reached the backend.
func TestKMSOwnership_FailedBackendDeleteRestoresRecord(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)
	const name = "hsm-down"
	scopedID := tenantScopedKeyID(ownershipTenant, name)
	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest(name))
	require.NoError(t, err)
	before, err := svc.readKeyOwnerRecord(ctx, scopedID, "test")
	require.NoError(t, err)

	mgr.deleteKeyFn = func(context.Context, string) error {
		return stderrors.New("hsm unavailable")
	}
	_, err = svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: name})
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrInternal, qe.Code)
	after, err := svc.readKeyOwnerRecord(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, before, after, "a failed backend delete must restore the record")
}

func TestTenantKeyClaimTime(t *testing.T) {
	claimedAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	got, done, err := tenantKeyClaimTime([]byte("t:h:" + strconv.FormatInt(claimedAt.UnixNano(), 10)))
	require.NoError(t, err)
	require.False(t, done)
	require.Equal(t, claimedAt, got)

	_, done, err = tenantKeyClaimTime([]byte("t:h:" + kmsOwnerRecordCompleted))
	require.NoError(t, err)
	require.True(t, done)

	for _, bad := range []string{"t:h", "t:h:soon", ":h:1", "t::1"} {
		_, _, err = tenantKeyClaimTime([]byte(bad))
		var malformed *KeyOwnershipRecordMalformedError
		require.ErrorAs(t, err, &malformed, bad)
	}
}

func TestKMSOwnership_StaleClaimIsReclaimed(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	stale := seedOwnershipClaim(t, svc, "orphan", now.Add(-kmsClaimStaleAfter-time.Second))
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("orphan"))

	require.NoError(t, err, "a stale claim with no key must be reclaimed")
	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(tenantScopedKeyID(ownershipTenant, "orphan")))
	require.NoError(t, err)
	require.NotEqual(t, stale, stored, "the new create must own the record")
}

func TestKMSOwnership_FreshClaimIsNotReclaimed(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	fresh := seedOwnershipClaim(t, svc, "inflight", now.Add(-time.Minute))
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("inflight"))

	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrAlreadyExists, qe.Code)
	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(tenantScopedKeyID(ownershipTenant, "inflight")))
	require.NoError(t, err)
	require.Equal(t, fresh, stored, "a create in flight must keep its claim")
}

func TestKMSOwnership_MalformedClaimIsRefused(t *testing.T) {
	svc, _, _ := newOwnershipKMS(t)
	scopedID := tenantScopedKeyID(ownershipTenant, "garbled")
	require.NoError(t, svc.kvStore.Put(tenantctx.WithSystem(context.Background()),
		kmsOwnerRecordKey(scopedID), []byte(ownershipTenant+kmsOwnerRecordSeparator+"deadbeef")))
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("garbled"))

	var malformed *KeyOwnershipRecordMalformedError
	require.True(t, stderrors.As(err, &malformed), "want KeyOwnershipRecordMalformedError, got %v", err)
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe))
	require.Equal(t, errors.ErrStorage, qe.Code)
	owner, err := svc.readKeyOwner(ctx, scopedID, "test")
	require.NoError(t, err)
	require.Equal(t, ownershipTenant, owner, "a malformed record must never be reclaimed")
}

func TestKMSOwnership_LookupFailureNeverReclaims(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	stale := seedOwnershipClaim(t, svc, "unreachable", now.Add(-kmsClaimStaleAfter-time.Second))
	mgr.getKeyFn = func(context.Context, string) (interface{}, error) {
		return nil, stderrors.New("backend unavailable")
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("unreachable"))

	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrStorage, qe.Code, "a failed lookup must not read as absence")
	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(tenantScopedKeyID(ownershipTenant, "unreachable")))
	require.NoError(t, err)
	require.Equal(t, stale, stored, "a failed lookup must leave the record untouched")
}

func TestKMSOwnership_ConfirmedAbsenceReclaims(t *testing.T) {
	svc, mgr, _ := newOwnershipKMS(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	stale := seedOwnershipClaim(t, svc, "absent", now.Add(-kmsClaimStaleAfter-time.Second))
	mgr.getKeyFn = func(context.Context, string) (interface{}, error) {
		return nil, fmt.Errorf("lookup: %w", kms.ErrKeyNotFound)
	}
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.GenerateKey(ctx, ownershipGenerateRequest("absent"))

	require.NoError(t, err, "a confirmed not-found must let a stale claim be reclaimed")
	stored, err := svc.kvStore.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(tenantScopedKeyID(ownershipTenant, "absent")))
	require.NoError(t, err)
	require.NotEqual(t, stale, stored)
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

// newOwnershipMixKMS holds, in ownershipTenant's namespace, one owned key
// ("mine"), one whose record names another tenant ("theirs"), and the
// fixture's unrecorded legacy key; plus a key in the other tenant's own
// namespace. Each has safety usage recorded.
func newOwnershipMixKMS(t *testing.T) *kmsServiceImpl {
	t.Helper()
	svc, mgr, _ := newOwnershipKMS(t)
	scoped := []string{
		tenantScopedKeyID(ownershipTenant, "mine"),
		tenantScopedKeyID(ownershipTenant, "theirs"),
		tenantScopedKeyID(ownershipTenant, ownershipKey),
		tenantScopedKeyID(ownershipOtherTenant, "other"),
	}
	for _, id := range scoped {
		mgr.keys[id] = &mockKeyData{keyID: id, algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled"}
		// One operation of two puts each key at 50%, inside SafetyCheck's
		// threshold as well as the report.
		svc.safetyTracker.SetKeyConfig(id, AEADSafetyConfig{MaxOperations: 2, MaxBytes: 1 << 20, WarningPercent: 80, AutoRotatePercent: 95})
		require.NoError(t, svc.safetyTracker.RecordKeyEncrypt(context.Background(), id, 100))
	}
	seedTenantKeyOwners(t, svc, ownershipTenant, "mine")
	seedTenantKeyOwners(t, svc, ownershipOtherTenant, "other")
	require.NoError(t, svc.kvStore.Put(tenantctx.WithSystem(context.Background()),
		kmsOwnerRecordKey(scoped[1]), []byte(ownershipOtherTenant)))
	return svc
}

func TestKMSOwnership_ReencryptionCountsOnlyOwnedKeys(t *testing.T) {
	svc := newOwnershipMixKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	resp, err := svc.StartReencryption(ctx, &StartReencryptionRequest{FromVersion: 1, ToVersion: 2})

	require.NoError(t, err)
	require.Equal(t, int64(1), resp.TotalEntities,
		"only the owned key counts: another tenant's record, the legacy key and the other namespace are excluded")
}

func TestKMSOwnership_ReencryptionStoreFaultIsTyped(t *testing.T) {
	svc := newOwnershipMixKMS(t)
	svc.SetKVStore(&ownershipFailingStore{KVStore: qrdbsdk.NewMemoryKVStore()})
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.StartReencryption(ctx, &StartReencryptionRequest{FromVersion: 1, ToVersion: 2})

	require.True(t, IsStorageError(err), "want a storage error, got %v", err)
	_, err = svc.StartReencryption(ctx, &StartReencryptionRequest{FromVersion: 1, ToVersion: 2})
	require.True(t, IsStorageError(err), "a failed start must not leave a re-encryption in progress, got %v", err)
}

func TestKMSOwnership_SafetyResultsShowOnlyOwnedKeys(t *testing.T) {
	svc := newOwnershipMixKMS(t)
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	report, err := svc.SafetyReport(ctx, &SafetyReportRequest{})
	require.NoError(t, err)
	require.Len(t, report.Items, 1, "another tenant's record and the legacy key must be hidden")
	require.Equal(t, "mine", report.Items[0].ID)

	check, err := svc.SafetyCheck(ctx, &SafetyCheckRequest{ThresholdPercent: 10})
	require.NoError(t, err)
	require.Len(t, check.Items, 1, "SafetyCheck must show only the owned key")
	require.Equal(t, "mine", check.Items[0].ID)
}

func TestKMSOwnership_SafetyResultsStoreFaultIsTyped(t *testing.T) {
	svc := newOwnershipMixKMS(t)
	svc.SetKVStore(&ownershipFailingStore{KVStore: qrdbsdk.NewMemoryKVStore()})
	ctx := tenantctx.WithTenant(context.Background(), ownershipTenant)

	_, err := svc.SafetyReport(ctx, &SafetyReportRequest{})
	require.True(t, IsStorageError(err), "want a storage error, got %v", err)
	_, err = svc.SafetyCheck(ctx, &SafetyCheckRequest{ThresholdPercent: 10})
	require.True(t, IsStorageError(err), "want a storage error, got %v", err)
}
