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
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	namespaceOwnerTenant = "acme"
	namespaceOtherTenant = "globex"
	namespaceKeyName     = "orders-kek"
)

// newNamespaceKMS returns a KMS service over the in-memory double whose
// GenerateKey records the attributes the service hands to the manager.
func newNamespaceKMS(t *testing.T) (*kmsServiceImpl, *mockKMSManager, *[]*xkmssdk.KeyAttributes) {
	t.Helper()
	mgr := newMockKMSManager()
	var generated []*xkmssdk.KeyAttributes
	mgr.generateKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		recorded := *attrs
		generated = append(generated, &recorded)
		mgr.keys[attrs.CN] = &mockKeyData{
			keyID:     attrs.CN,
			algorithm: string(attrs.SymmetricAlgorithm),
			version:   1,
			status:    "enabled",
			createdAt: time.Now(),
		}
		return mgr.keys[attrs.CN], nil
	}
	svc := newKMSService(mgr, nil)
	svc.SetKVStore(newMockKVStore())
	return svc, mgr, &generated
}

func generateNamespaceKey(t *testing.T, svc *kmsServiceImpl, tenantID string) {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), tenantID)
	_, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
		Name:  namespaceKeyName,
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.NoError(t, err)
}

// A tenant key reaches the manager in go-xkms's own form: the CN is the
// caller's key name and the tenant is the xkms TenantID attribute, the same
// form the gRPC transport sends to xkms.v1.
func TestKMSNamespace_GenerateKeyUsesXKMSForm(t *testing.T) {
	// Red-first test from PR #2279: the service still names a tenant key
	// tenant/<t>/<k>. Row #1654's #2196 slice moves it to CN=K TenantID=T,
	// with #2128's verified-ownership path for keys already stored, and
	// removes this skip in the same commit.
	t.Skip("#2196: tenant keys still use tenant/<t>/<k>; see docs/plans/1654.md")
	svc, _, generated := newNamespaceKMS(t)

	generateNamespaceKey(t, svc, namespaceOwnerTenant)

	require.Len(t, *generated, 1)
	attrs := (*generated)[0]
	require.Equal(t, namespaceKeyName, attrs.CN, "the stored CN is the caller's key name")
	require.Equal(t, namespaceOwnerTenant, attrs.TenantID, "the tenant is the xkms TenantID attribute")
}

// Another tenant cannot get, list or delete a tenant's key, even though the
// key's CN carries no tenant prefix.
func TestKMSNamespace_OtherTenantCannotReachKey(t *testing.T) {
	svc, mgr, _ := newNamespaceKMS(t)
	generateNamespaceKey(t, svc, namespaceOwnerTenant)
	otherCtx := tenantctx.WithTenant(context.Background(), namespaceOtherTenant)

	_, err := svc.GetKey(otherCtx, &GetKeyRequest{KeyID: namespaceKeyName})
	require.Error(t, err, "another tenant must not get the key")

	listed, err := svc.ListKeys(otherCtx, &ListKeysRequest{})
	require.NoError(t, err)
	for _, key := range listed.Keys {
		require.NotEqual(t, namespaceKeyName, key.KeyID, "another tenant must not list the key")
	}

	_, err = svc.DeleteKey(otherCtx, &DeleteKeyRequest{KeyID: namespaceKeyName})
	require.Error(t, err, "another tenant must not delete the key")
	require.NotEmpty(t, mgr.keys, "the owner's key survives the other tenant's delete")

	ownerCtx := tenantctx.WithTenant(context.Background(), namespaceOwnerTenant)
	_, err = svc.GetKey(ownerCtx, &GetKeyRequest{KeyID: namespaceKeyName})
	require.NoError(t, err, "the owner still gets its key")
}
