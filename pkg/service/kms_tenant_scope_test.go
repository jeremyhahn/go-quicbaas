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
	"bytes"
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	scopeOwnerTenant = "acme"
	scopeOtherTenant = "globex"
	scopeKeyName     = "acme-key"
	scopeDEKPath     = "acme/dek"
)

// kmsScopeVerb invokes one KMS verb against the object named id.
type kmsScopeVerb func(svc *kmsServiceImpl, ctx context.Context, id string) error

// newTenantScopeKMS returns a KMS service in which the owner tenant holds
// one key and one DEK.
func newTenantScopeKMS(t *testing.T) *kmsServiceImpl {
	t.Helper()
	scopedKeyID := tenantScopedKeyID(scopeOwnerTenant, scopeKeyName)
	mgr := newMockKMSManager()
	mgr.keys[scopedKeyID] = &mockKeyData{
		keyID:     scopedKeyID,
		algorithm: "ECDSA-P256",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	// The production manager reports an absent key as kms.ErrKeyNotFound;
	// the double's default plain error would hide the not-found mapping.
	mgr.rotateKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		key, ok := mgr.keys[keyID]
		if !ok {
			return nil, kms.ErrKeyNotFound
		}
		key.version++
		return &kms.KeyMetadata{KeyID: key.keyID, Algorithm: key.algorithm, Version: int(key.version), State: kms.KeyState(key.status)}, nil
	}
	mgr.revokeKeyFn = func(_ context.Context, keyID string) error {
		if _, ok := mgr.keys[keyID]; !ok {
			return kms.ErrKeyNotFound
		}
		return nil
	}
	mgr.deleteKeyFn = func(_ context.Context, keyID string) error {
		if _, ok := mgr.keys[keyID]; !ok {
			return kms.ErrKeyNotFound
		}
		delete(mgr.keys, keyID)
		return nil
	}
	svc := newKMSService(mgr, nil)
	svc.keyNames.Store(scopedKeyID, scopedKeyID)
	seedTenantKeyOwners(t, svc, scopeOwnerTenant, scopeKeyName)

	ownerCtx := tenantctx.WithTenant(context.Background(), scopeOwnerTenant)
	_, err := svc.CreateDEK(ownerCtx, &CreateDEKRequest{Path: scopeDEKPath, Algorithm: KeyAlgAES256GCM})
	require.NoError(t, err)
	return svc
}

func kmsKeyScopeVerbs() map[string]kmsScopeVerb {
	return map[string]kmsScopeVerb{
		"GetKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.GetKey(ctx, &GetKeyRequest{KeyID: id})
			return err
		},
		"GetPublicKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.GetPublicKey(ctx, &GetPublicKeyRequest{KeyID: id})
			return err
		},
		"RotateKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.RotateKey(ctx, &RotateKeyRequest{KeyID: id})
			return err
		},
		"RevokeKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.RevokeKey(ctx, &RevokeKeyRequest{KeyID: id})
			return err
		},
		"DeleteKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: id, Force: true})
			return err
		},
		"GetKeyMetadata": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.GetKeyMetadata(ctx, &GetKeyMetadataRequest{KeyID: id})
			return err
		},
		"UpdateKeyMetadata": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.UpdateKeyMetadata(ctx, &UpdateKeyMetadataRequest{KeyID: id, Tags: map[string]string{"env": "prod"}})
			return err
		},
		"SetKeyPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.SetKeyPolicy(ctx, &SetKeyPolicyRequest{
				KeyID:  id,
				Policy: &KeyPolicy{AllowedOperations: []KeyUsage{KeyUsageSign}},
			})
			return err
		},
		"GetKeyPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.GetKeyPolicy(ctx, &GetKeyPolicyRequest{KeyID: id})
			return err
		},
		"DeleteKeyPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.DeleteKeyPolicy(ctx, &DeleteKeyPolicyRequest{KeyID: id})
			return err
		},
		"SetKeyRotationPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
				KeyID:  id,
				Policy: &RotationPolicy{Enabled: true, AutoRotate: true, Interval: time.Hour},
			})
			return err
		},
		"GetKeyRotationPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.GetKeyRotationPolicy(ctx, &GetKeyRotationPolicyRequest{KeyID: id})
			return err
		},
		"DeleteKeyRotationPolicy": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.DeleteKeyRotationPolicy(ctx, &DeleteKeyRotationPolicyRequest{KeyID: id})
			return err
		},
		"UpdateKeyStatus": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.UpdateKeyStatus(ctx, &UpdateKeyStatusRequest{KeyID: id, Status: KeyStatusDisabled})
			return err
		},
		"ListKeyVersions": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.ListKeyVersions(ctx, &ListKeyVersionsRequest{KeyID: id})
			return err
		},
		"SetDefaultKey": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.SetDefaultKey(ctx, &SetDefaultKeyRequest{Name: id})
			return err
		},
		"SetKeySafety": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.SetKeySafety(ctx, &SetKeySafetyRequest{KeyID: id, MaxOperations: 1000})
			return err
		},
		"ClearKeySafety": func(svc *kmsServiceImpl, ctx context.Context, id string) error {
			_, err := svc.ClearKeySafety(ctx, &ClearKeySafetyRequest{KeyID: id})
			return err
		},
	}
}

func kmsDEKScopeVerbs() map[string]kmsScopeVerb {
	return map[string]kmsScopeVerb{
		"GetDEK": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.GetDEK(ctx, &GetDEKRequest{Path: path})
			return err
		},
		"RotateDEK": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.RotateDEK(ctx, &RotateDEKRequest{Path: path})
			return err
		},
		"DeleteDEK": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.DeleteDEK(ctx, &DeleteDEKRequest{Path: path, Force: true})
			return err
		},
		"GetDEKStatus": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.GetDEKStatus(ctx, &GetDEKStatusRequest{Path: path})
			return err
		},
		"SetDefaultDEK": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.SetDefaultDEK(ctx, &SetDefaultDEKRequest{Path: path})
			return err
		},
		"SetDEKSafety": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.SetDEKSafety(ctx, &SetDEKSafetyRequest{Path: path, MaxOperations: 1000})
			return err
		},
		"ClearDEKSafety": func(svc *kmsServiceImpl, ctx context.Context, path string) error {
			_, err := svc.ClearDEKSafety(ctx, &ClearDEKSafetyRequest{Path: path})
			return err
		},
	}
}

// TestKMSService_EveryVerbIsTenantScoped proves that each verb addressing a
// key, policy, rotation policy, metadata, version or DEK resolves the id
// inside the caller's tenant: the owner reaches its object, another tenant
// gets not-found for the same id, and an unbound context is refused.
// tenantScopeCase is one caller context and the outcome a tenant-scoped verb
// must produce for it.
type tenantScopeCase struct {
	name  string
	ctx   context.Context
	check func(t *testing.T, err error)
}

// tenantScopeCases are the three callers every tenant-scoped verb is proved
// against: the owning tenant, another tenant, and a context with no tenant.
func tenantScopeCases() []tenantScopeCase {
	return []tenantScopeCase{
		{
			name: "same tenant succeeds",
			ctx:  tenantctx.WithTenant(context.Background(), scopeOwnerTenant),
			check: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "other tenant gets not found",
			ctx:  tenantctx.WithTenant(context.Background(), scopeOtherTenant),
			check: func(t *testing.T, err error) {
				var qbErr *quicbaasErrors.QBError
				require.True(t, stderrors.As(err, &qbErr), "error %v is not a QBError", err)
				require.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
			},
		},
		{
			name: "unbound context is refused",
			ctx:  context.Background(),
			check: func(t *testing.T, err error) {
				var missing *tenantctx.MissingTenantError
				require.True(t, stderrors.As(err, &missing), "error %v does not wrap MissingTenantError", err)
			},
		},
	}
}

func TestKMSService_EveryVerbIsTenantScoped(t *testing.T) {
	groups := []struct {
		id    string
		verbs map[string]kmsScopeVerb
	}{
		{id: scopeKeyName, verbs: kmsKeyScopeVerbs()},
		{id: scopeDEKPath, verbs: kmsDEKScopeVerbs()},
	}
	cases := tenantScopeCases()

	for _, group := range groups {
		for verbName, verb := range group.verbs {
			for _, tc := range cases {
				t.Run(verbName+"/"+tc.name, func(t *testing.T) {
					svc := newTenantScopeKMS(t)
					tc.check(t, verb(svc, tc.ctx, group.id))
				})
			}
		}
	}
}

const (
	scopeWrappingKey = "wrapper"
	scopeImportedKey = "imported"
)

// newTenantScopeWrapKMS returns a service whose owner tenant holds an
// exportable key and a wrapping key. The manager double binds a wrapped blob
// to the wrapping key id and the AAD, as an AEAD does, so a blob opens only
// under the exact (key, AAD) pair it was sealed with.
func newTenantScopeWrapKMS(t *testing.T) (*kmsServiceImpl, *mockKMSManager) {
	t.Helper()
	mgr := newMockKMSManager()
	for _, name := range []string{scopeKeyName, scopeWrappingKey} {
		id := tenantScopedKeyID(scopeOwnerTenant, name)
		mgr.keys[id] = &mockKeyData{keyID: id, algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled", createdAt: time.Now()}
	}
	mgr.getKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		key, ok := mgr.keys[keyID]
		if !ok {
			return nil, kms.ErrKeyNotFound
		}
		return &KeyMetadata{KeyID: key.keyID, Algorithm: KeyAlgorithm(key.algorithm), Version: key.version, Status: KeyStatusEnabled, Exportable: true}, nil
	}
	mgr.encryptFn = func(_ context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return append([]byte(keyID+"|"+string(aad)+"|"), plaintext...), nil
	}
	mgr.decryptFn = func(_ context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		binding := []byte(keyID + "|" + string(aad) + "|")
		if !bytes.HasPrefix(ciphertext, binding) {
			return nil, kms.ErrKeyNotFound
		}
		return ciphertext[len(binding):], nil
	}
	svc := newKMSService(mgr, nil)
	seedTenantKeyOwners(t, svc, scopeOwnerTenant, scopeKeyName, scopeWrappingKey)
	return svc, mgr
}

// exportOwnerKey exports the owner's key under the owner's wrapping key.
func exportOwnerKey(t *testing.T, svc *kmsServiceImpl) *ExportKeyResponse {
	t.Helper()
	resp, err := svc.ExportKey(tenantctx.WithTenant(context.Background(), scopeOwnerTenant), &ExportKeyRequest{
		KeyID:             scopeKeyName,
		WrappingKeyID:     scopeWrappingKey,
		WrappingAlgorithm: "AES-KW",
	})
	require.NoError(t, err)
	return resp
}

func TestKMSService_ExportKey_IsTenantScoped(t *testing.T) {
	for _, tc := range tenantScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTenantScopeWrapKMS(t)
			resp, err := svc.ExportKey(tc.ctx, &ExportKeyRequest{
				KeyID:             scopeKeyName,
				WrappingKeyID:     scopeWrappingKey,
				WrappingAlgorithm: "AES-KW",
			})
			tc.check(t, err)
			if err == nil {
				require.Equal(t, scopeKeyName, resp.KeyID, "the caller sees its own key id, not the scoped one")
				require.Equal(t, scopeWrappingKey, resp.WrappingKeyID)
			}
		})
	}
}

func TestKMSService_UnwrapKey_IsTenantScoped(t *testing.T) {
	for _, tc := range tenantScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, mgr := newTenantScopeWrapKMS(t)
			exported := exportOwnerKey(t, svc)
			resp, err := svc.UnwrapKey(tc.ctx, &UnwrapKeyRequest{
				KeyID:             scopeImportedKey,
				WrappedKey:        exported.WrappedKey,
				WrappingKeyID:     scopeWrappingKey,
				WrappingAlgorithm: "AES-KW",
				Algorithm:         string(KeyAlgAES256GCM),
				SourceKeyID:       scopeKeyName,
				SourceVersion:     exported.Version,
			})
			tc.check(t, err)
			if err == nil {
				require.Equal(t, scopeImportedKey, resp.KeyID)
				require.Contains(t, mgr.keys, tenantScopedKeyID(scopeOwnerTenant, scopeImportedKey), "the key is imported into the caller's tenant")
			}
		})
	}
}

// A blob exported by one tenant must not open for another, even when the
// other tenant owns a wrapping key of the same name: the AAD binds the
// exporting tenant's scoped key id.
func TestKMSService_UnwrapKey_OtherTenantBlobDoesNotOpen(t *testing.T) {
	svc, mgr := newTenantScopeWrapKMS(t)
	exported := exportOwnerKey(t, svc)
	otherWrapper := tenantScopedKeyID(scopeOtherTenant, scopeWrappingKey)
	mgr.keys[otherWrapper] = &mockKeyData{keyID: otherWrapper, algorithm: string(KeyAlgAES256GCM), version: 1, status: "enabled", createdAt: time.Now()}
	seedTenantKeyOwners(t, svc, scopeOtherTenant, scopeWrappingKey)

	_, err := svc.UnwrapKey(tenantctx.WithTenant(context.Background(), scopeOtherTenant), &UnwrapKeyRequest{
		KeyID:             scopeImportedKey,
		WrappedKey:        exported.WrappedKey,
		WrappingKeyID:     scopeWrappingKey,
		WrappingAlgorithm: "AES-KW",
		Algorithm:         string(KeyAlgAES256GCM),
		SourceKeyID:       scopeKeyName,
		SourceVersion:     exported.Version,
	})
	require.Error(t, err)
	// The double reports a blob that fails to open as kms.ErrKeyNotFound.
	require.True(t, stderrors.Is(err, kms.ErrKeyNotFound), "error %v is not the double's open failure", err)
	require.NotContains(t, mgr.keys, tenantScopedKeyID(scopeOtherTenant, scopeImportedKey))
}

// Deleting a key releases its name in the caller's tenant only.
func TestKMSService_DeleteKey_ReleasesOnlyTheCallersName(t *testing.T) {
	ownerID := tenantScopedKeyID(scopeOwnerTenant, scopeKeyName)
	otherID := tenantScopedKeyID(scopeOtherTenant, scopeKeyName)

	for _, tc := range tenantScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTenantScopeKMS(t)
			svc.keyNames.Store(otherID, otherID)

			_, err := svc.DeleteKey(tc.ctx, &DeleteKeyRequest{KeyID: scopeKeyName})
			tc.check(t, err)

			_, ownerKept := svc.keyNames.Load(ownerID)
			_, otherKept := svc.keyNames.Load(otherID)
			require.Equal(t, err != nil, ownerKept, "the owner's name is released only by the owner's delete")
			require.True(t, otherKept, "another tenant's name of the same key is never released")
		})
	}
}

// TestKMSService_ImportKey_RefusesReservedTenantNamespace proves an import
// name inside the "tenant." namespace is refused before the manager is
// reached: scoping it would let the caller plant a key in another tenant.
func TestKMSService_ImportKey_RefusesReservedTenantNamespace(t *testing.T) {
	mgr := newMockKMSManager()
	imported := false
	mgr.importKeyFn = func(_ context.Context, _ string, _ []byte, _ string) (interface{}, error) {
		imported = true
		return nil, nil
	}
	svc := newKMSService(mgr, nil)
	ownerCtx := tenantctx.WithTenant(context.Background(), scopeOwnerTenant)

	resp, err := svc.ImportKey(ownerCtx, &ImportKeyRequest{
		Name:        kmsTenantKeyPrefix + scopeOtherTenant + kmsTenantKeySuffix + scopeImportedKey,
		KeyMaterial: []byte("key-material"),
	})

	require.Error(t, err)
	require.Nil(t, resp)
	require.True(t, IsInvalidInput(err), "the refusal must carry the invalid-input code, got %v", err)
	require.Contains(t, err.Error(), "reserved tenant namespace")
	require.False(t, imported, "a refused import must never reach the manager")
	require.Empty(t, mgr.keys, "a refused import must write nothing")
}

// TestKMSService_CreatingVerbsRefuseReservedTenantPrefix proves that no
// caller, tenant or system, can name a new key or DEK inside another
// tenant's namespace. The system scope stores names unscoped, so without
// the refusal "tenant.globex.planted" would become a key globex owns.
func TestKMSService_CreatingVerbsRefuseReservedTenantPrefix(t *testing.T) {
	planted := kmsTenantKeyPrefix + scopeOtherTenant + kmsTenantKeySuffix + "planted"
	verbs := map[string]func(svc *kmsServiceImpl, ctx context.Context) error{
		"GenerateKey": func(svc *kmsServiceImpl, ctx context.Context) error {
			_, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
				Name:          planted,
				Usage:         []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
				KeyAttributes: buildSymmetricAttrs(),
			})
			return err
		},
		"ImportKey": func(svc *kmsServiceImpl, ctx context.Context) error {
			_, err := svc.ImportKey(ctx, &ImportKeyRequest{Name: planted, KeyMaterial: []byte("key-material")})
			return err
		},
		"UnwrapKey": func(svc *kmsServiceImpl, ctx context.Context) error {
			_, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
				KeyID:             planted,
				WrappedKey:        []byte("wrapped"),
				WrappingKeyID:     scopeWrappingKey,
				WrappingAlgorithm: "AES-KW",
				Algorithm:         string(KeyAlgAES256GCM),
			})
			return err
		},
		"CreateDEK": func(svc *kmsServiceImpl, ctx context.Context) error {
			_, err := svc.CreateDEK(ctx, &CreateDEKRequest{Path: planted, Algorithm: KeyAlgAES256GCM})
			return err
		},
	}
	callers := map[string]context.Context{
		"tenant caller": tenantctx.WithTenant(context.Background(), scopeOwnerTenant),
		"system caller": systemTenantCtx(),
	}
	for verbName, verb := range verbs {
		for callerName, ctx := range callers {
			t.Run(verbName+"/"+callerName, func(t *testing.T) {
				svc, mgr := newTenantScopeWrapKMS(t)
				keysBefore := len(mgr.keys)

				err := verb(svc, ctx)

				var reserved *ReservedKeyNameError
				require.True(t, stderrors.As(err, &reserved), "error %v does not wrap ReservedKeyNameError", err)
				require.Equal(t, planted, reserved.Name)
				require.True(t, IsInvalidInput(err), "the refusal must carry the invalid-input code, got %v", err)
				require.Len(t, mgr.keys, keysBefore, "a refused name must write nothing")
			})
		}
	}
}

func TestRejectReservedKeyName(t *testing.T) {
	require.NoError(t, rejectReservedKeyName("my-key", "GenerateKey"))
	require.NoError(t, rejectReservedKeyName("mytenant.key", "GenerateKey"), "only a leading prefix is reserved")

	err := rejectReservedKeyName(kmsTenantKeyPrefix+"acme.key", "GenerateKey")
	var reserved *ReservedKeyNameError
	require.True(t, stderrors.As(err, &reserved), "error %v does not wrap ReservedKeyNameError", err)
	require.True(t, IsInvalidInput(err))
}

// startOwnerReencryption starts a re-encryption in the owner tenant. The
// throttle holds the one-key run open, so the operation is still in progress
// when the test inspects it; cleanup cancels it.
func startOwnerReencryption(t *testing.T, svc *kmsServiceImpl) {
	t.Helper()
	_, err := svc.StartReencryption(tenantctx.WithTenant(context.Background(), scopeOwnerTenant), &StartReencryptionRequest{
		FromVersion:  1,
		ToVersion:    2,
		ThrottleRate: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		svc.reencryptionState.mu.Lock()
		defer svc.reencryptionState.mu.Unlock()
		svc.reencryptionState.cancelFunc()
	})
}

func TestKMSService_StartReencryption_ListsOnlyTheCallersKeys(t *testing.T) {
	cases := []struct {
		name       string
		ctx        context.Context
		wantPrefix string
	}{
		{name: "tenant lists its own keys", ctx: tenantctx.WithTenant(context.Background(), scopeOwnerTenant), wantPrefix: tenantListPrefix(scopeOwnerTenant)},
		{name: "system scope keeps its reach", ctx: systemTenantCtx(), wantPrefix: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTenantScopeKMS(t)
			mgr := svc.kmsManager.(*mockKMSManager)
			var gotPrefix string
			mgr.listKeysFn = func(_ context.Context, prefix string) ([]interface{}, error) {
				gotPrefix = prefix
				return nil, nil
			}
			_, err := svc.StartReencryption(tc.ctx, &StartReencryptionRequest{FromVersion: 1, ToVersion: 2})
			require.NoError(t, err)
			require.Equal(t, tc.wantPrefix, gotPrefix)
		})
	}

	t.Run("unbound context is refused", func(t *testing.T) {
		svc := newTenantScopeKMS(t)
		_, err := svc.StartReencryption(context.Background(), &StartReencryptionRequest{FromVersion: 1, ToVersion: 2})
		var missing *tenantctx.MissingTenantError
		require.True(t, stderrors.As(err, &missing), "error %v does not wrap MissingTenantError", err)
	})
}

func TestKMSService_ReencryptionProgressAndCancel_AreTenantScoped(t *testing.T) {
	verbs := map[string]func(svc *kmsServiceImpl, ctx context.Context) error{
		"GetReencryptionProgress": func(svc *kmsServiceImpl, ctx context.Context) error {
			_, err := svc.GetReencryptionProgress(ctx, &GetReencryptionProgressRequest{})
			return err
		},
		"CancelReencryption": func(svc *kmsServiceImpl, ctx context.Context) error {
			return svc.CancelReencryption(ctx)
		},
	}
	for verbName, verb := range verbs {
		for _, tc := range tenantScopeCases() {
			t.Run(verbName+"/"+tc.name, func(t *testing.T) {
				svc := newTenantScopeKMS(t)
				startOwnerReencryption(t, svc)
				tc.check(t, verb(svc, tc.ctx))
			})
		}
	}
}
