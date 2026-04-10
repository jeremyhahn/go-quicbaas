//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ---------------------------------------------------------------------------
// BaasTransportNotSupportedError
// ---------------------------------------------------------------------------

func TestBaasTransportNotSupportedError(t *testing.T) {
	t.Run("formats message with operation name", func(t *testing.T) {
		err := &BaasTransportNotSupportedError{Operation: "SomeOp"}
		assert.Equal(t, "baas embedded transport: SomeOp not supported", err.Error())
	})
}

// ---------------------------------------------------------------------------
// baasTransport lifecycle
// ---------------------------------------------------------------------------

func TestBaasTransport_ConnectCloseHealthy(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)

	t.Run("connect is no-op", func(t *testing.T) {
		err := bt.Connect(context.Background())
		require.NoError(t, err)
	})

	t.Run("healthy when open", func(t *testing.T) {
		assert.True(t, bt.Healthy(context.Background()))
	})

	t.Run("close sets closed flag", func(t *testing.T) {
		require.NoError(t, bt.Close())
		assert.False(t, bt.Healthy(context.Background()))
	})
}

func TestBaasTransport_Request(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)

	t.Run("returns not-supported error", func(t *testing.T) {
		err := bt.Request(context.Background(), "doSomething", nil, nil)
		require.Error(t, err)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
		assert.Contains(t, nsErr.Error(), "Request(doSomething)")
	})
}

// ---------------------------------------------------------------------------
// KVClient operations
// ---------------------------------------------------------------------------

func TestBaasTransport_PutGetDeleteExists(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("put and get round-trip", func(t *testing.T) {
		err := bt.Put(ctx, "k1", []byte("v1"))
		require.NoError(t, err)

		val, err := bt.Get(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, []byte("v1"), val)
	})

	t.Run("get missing key returns KeyNotFoundError", func(t *testing.T) {
		_, err := bt.Get(ctx, "missing")
		require.Error(t, err)
		var knf *transport.KeyNotFoundError
		assert.True(t, errors.As(err, &knf))
	})

	t.Run("exists returns true for existing key", func(t *testing.T) {
		ok, err := bt.Exists(ctx, "k1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("exists returns false for missing key", func(t *testing.T) {
		ok, err := bt.Exists(ctx, "missing")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("delete removes key", func(t *testing.T) {
		err := bt.Delete(ctx, "k1")
		require.NoError(t, err)

		_, err = bt.Get(ctx, "k1")
		require.Error(t, err)
	})
}

func TestBaasTransport_ListAndScan(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	require.NoError(t, bt.Put(ctx, "prefix/a", []byte("1")))
	require.NoError(t, bt.Put(ctx, "prefix/b", []byte("2")))
	require.NoError(t, bt.Put(ctx, "other/c", []byte("3")))

	t.Run("list filters by prefix", func(t *testing.T) {
		keys, err := bt.List(ctx, "prefix/")
		require.NoError(t, err)
		assert.Len(t, keys, 2)
	})

	t.Run("scan returns matching key-value pairs", func(t *testing.T) {
		pairs, err := bt.Scan(ctx, "prefix/")
		require.NoError(t, err)
		assert.Len(t, pairs, 2)
		assert.Equal(t, []byte("1"), pairs["prefix/a"])
	})

	t.Run("scan with empty prefix returns all", func(t *testing.T) {
		pairs, err := bt.Scan(ctx, "")
		require.NoError(t, err)
		assert.Len(t, pairs, 3)
	})
}

// ---------------------------------------------------------------------------
// AdminClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_AdminStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("Health returns healthy", func(t *testing.T) {
		resp, err := bt.Health(ctx)
		require.NoError(t, err)
		assert.Equal(t, "healthy", resp.Status)
	})

	t.Run("Status returns empty response", func(t *testing.T) {
		resp, err := bt.Status(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Peers returns empty response", func(t *testing.T) {
		resp, err := bt.Peers(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Capabilities returns empty response", func(t *testing.T) {
		resp, err := bt.Capabilities(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetShardState returns empty response", func(t *testing.T) {
		resp, err := bt.GetShardState(ctx, 1)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetTopology returns empty response", func(t *testing.T) {
		resp, err := bt.GetTopology(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("BarrierStatus returns empty response", func(t *testing.T) {
		resp, err := bt.BarrierStatus(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	// Verify stub error operations
	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"AddMember", func() error { return bt.AddMember(ctx, 1, 1, "addr") }},
		{"RemoveMember", func() error { return bt.RemoveMember(ctx, 1, 1) }},
		{"RequestSnapshot", func() error { return bt.RequestSnapshot(ctx, 1) }},
		{"Drain", func() error { return bt.Drain(ctx) }},
		{"StartShard", func() error { return bt.StartShard(ctx, nil) }},
		{"StopShard", func() error { return bt.StopShard(ctx, 1) }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported error", func(t *testing.T) {
			err := op.fn()
			require.Error(t, err)
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// MVCCClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_MVCCStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("PutWithLease", func(t *testing.T) {
		_, err := bt.PutWithLease(ctx, "k", nil, 0)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("GetRevision", func(t *testing.T) {
		_, err := bt.GetRevision(ctx, "k")
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("GetAtRevision", func(t *testing.T) {
		_, err := bt.GetAtRevision(ctx, "k", 1)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("Range", func(t *testing.T) {
		_, err := bt.Range(ctx, nil, nil, transport.RangeOptions{})
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("Compact", func(t *testing.T) {
		err := bt.Compact(ctx, 1)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("CurrentRevision", func(t *testing.T) {
		_, err := bt.CurrentRevision(ctx)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})
}

// ---------------------------------------------------------------------------
// TransactionClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_ExecuteTransaction(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("returns not-supported error", func(t *testing.T) {
		_, err := bt.ExecuteTransaction(ctx, nil)
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})
}

// ---------------------------------------------------------------------------
// BatchClient
// ---------------------------------------------------------------------------

func TestBaasTransport_ExecuteBatch(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("processes put, get, delete operations", func(t *testing.T) {
		ops := []transport.Op{
			{Type: transport.OpPut, Key: []byte("bk1"), Value: []byte("bv1")},
			{Type: transport.OpGet, Key: []byte("bk1")},
			{Type: transport.OpDelete, Key: []byte("bk1")},
			{Type: transport.OpType(255), Key: []byte("bk1")},
		}

		resp, err := bt.ExecuteBatch(ctx, ops)
		require.NoError(t, err)
		require.Len(t, resp.Responses, 4)
		assert.True(t, resp.Responses[0].Succeeded, "put should succeed")
		assert.True(t, resp.Responses[1].Succeeded, "get should succeed")
		assert.True(t, resp.Responses[2].Succeeded, "delete should succeed")
		assert.False(t, resp.Responses[3].Succeeded, "unknown op should fail")
	})

	t.Run("get in batch returns kvs on success", func(t *testing.T) {
		require.NoError(t, bt.Put(ctx, "existing", []byte("data")))
		ops := []transport.Op{
			{Type: transport.OpGet, Key: []byte("existing")},
		}
		resp, err := bt.ExecuteBatch(ctx, ops)
		require.NoError(t, err)
		require.Len(t, resp.Responses, 1)
		assert.True(t, resp.Responses[0].Succeeded)
		require.Len(t, resp.Responses[0].Kvs, 1)
		assert.Equal(t, []byte("data"), resp.Responses[0].Kvs[0].Value)
	})

	t.Run("get in batch for missing key marks not succeeded", func(t *testing.T) {
		ops := []transport.Op{
			{Type: transport.OpGet, Key: []byte("nonexistent-batch-key")},
		}
		resp, err := bt.ExecuteBatch(ctx, ops)
		require.NoError(t, err)
		assert.False(t, resp.Responses[0].Succeeded)
	})
}

// ---------------------------------------------------------------------------
// ImportFile stub
// ---------------------------------------------------------------------------

func TestBaasTransport_ImportFile(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)

	t.Run("returns not-supported error", func(t *testing.T) {
		_, err := bt.ImportFile(context.Background(), "file.csv", nil, transport.ImportOptions{})
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})
}

// ---------------------------------------------------------------------------
// WatchClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_WatchStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("Watch returns not-supported", func(t *testing.T) {
		_, err := bt.Watch(ctx, "key", transport.WatchOptions{})
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("WatchPrefix returns not-supported", func(t *testing.T) {
		_, err := bt.WatchPrefix(ctx, "prefix", transport.WatchOptions{})
		var nsErr *BaasTransportNotSupportedError
		assert.True(t, errors.As(err, &nsErr))
	})
}

// ---------------------------------------------------------------------------
// LeaseClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_LeaseStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"LeaseGrant", func() error { _, e := bt.LeaseGrant(ctx, 10); return e }},
		{"LeaseRevoke", func() error { return bt.LeaseRevoke(ctx, 1) }},
		{"LeaseKeepAlive", func() error { _, e := bt.LeaseKeepAlive(ctx, 1); return e }},
		{"LeaseTimeToLive", func() error { _, e := bt.LeaseTimeToLive(ctx, 1); return e }},
		{"LeaseList", func() error { _, e := bt.LeaseList(ctx); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// UserClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_UserStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateUser", func() error { _, e := bt.CreateUser(ctx, nil); return e }},
		{"GetUser", func() error { _, e := bt.GetUser(ctx, "u"); return e }},
		{"UpdateUser", func() error { _, e := bt.UpdateUser(ctx, nil); return e }},
		{"DeleteUser", func() error { return bt.DeleteUser(ctx, "u") }},
		{"ListUsers", func() error { _, e := bt.ListUsers(ctx, nil); return e }},
		{"EnableUser", func() error { return bt.EnableUser(ctx, "u") }},
		{"DisableUser", func() error { return bt.DisableUser(ctx, "u") }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// RoleClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_RoleStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateRole", func() error { _, e := bt.CreateRole(ctx, nil); return e }},
		{"GetRole", func() error { _, e := bt.GetRole(ctx, "r"); return e }},
		{"UpdateRole", func() error { _, e := bt.UpdateRole(ctx, nil); return e }},
		{"DeleteRole", func() error { return bt.DeleteRole(ctx, "r") }},
		{"ListRoles", func() error { _, e := bt.ListRoles(ctx, nil); return e }},
		{"AssignRole", func() error { return bt.AssignRole(ctx, "u", "r") }},
		{"UnassignRole", func() error { return bt.UnassignRole(ctx, "u", "r") }},
		{"ListPermissions", func() error { _, e := bt.ListPermissions(ctx); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// CertClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_CertStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"GenerateCert", func() error { _, e := bt.GenerateCert(ctx, nil); return e }},
		{"ExportCert", func() error { _, e := bt.ExportCert(ctx, nil); return e }},
		{"RevokeCert", func() error { return bt.RevokeCert(ctx, "serial") }},
		{"ListCerts", func() error { _, e := bt.ListCerts(ctx, ""); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// BarrierClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_BarrierStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"BarrierInitialize", func() error { return bt.BarrierInitialize(ctx, nil) }},
		{"BarrierUnseal", func() error { return bt.BarrierUnseal(ctx, nil) }},
		{"BarrierSeal", func() error { return bt.BarrierSeal(ctx) }},
		{"BarrierInitializeShamir", func() error { _, e := bt.BarrierInitializeShamir(ctx, nil); return e }},
		{"BarrierUnsealWithShare", func() error { _, e := bt.BarrierUnsealWithShare(ctx, nil); return e }},
		{"BarrierUnsealWithShares", func() error { return bt.BarrierUnsealWithShares(ctx, nil) }},
		{"BarrierShamirListShares", func() error { _, e := bt.BarrierShamirListShares(ctx); return e }},
		{"BarrierShamirDeleteShare", func() error { return bt.BarrierShamirDeleteShare(ctx, nil) }},
		{"BarrierShamirDeleteAllShares", func() error { return bt.BarrierShamirDeleteAllShares(ctx) }},
		{"BarrierShamirVerify", func() error { return bt.BarrierShamirVerify(ctx) }},
		{"BarrierRekey", func() error { _, e := bt.BarrierRekey(ctx, nil); return e }},
		{"BarrierGenerateRecoveryKeys", func() error { _, e := bt.BarrierGenerateRecoveryKeys(ctx, nil); return e }},
		{"BarrierRecoverWithKeys", func() error { return bt.BarrierRecoverWithKeys(ctx, nil) }},
		{"BarrierDeleteRecoveryKeys", func() error { return bt.BarrierDeleteRecoveryKeys(ctx) }},
		{"BarrierHasRecoveryKeys", func() error { _, e := bt.BarrierHasRecoveryKeys(ctx); return e }},
		{"BarrierGenerateRootToken", func() error { _, e := bt.BarrierGenerateRootToken(ctx, nil); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// DEKClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_DEKStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"DEKInitialize", func() error { return bt.DEKInitialize(ctx, nil) }},
		{"DEKGetStatus", func() error { _, e := bt.DEKGetStatus(ctx); return e }},
		{"DEKRotate", func() error { return bt.DEKRotate(ctx) }},
		{"DEKGetCurrent", func() error { _, e := bt.DEKGetCurrent(ctx); return e }},
		{"DEKSyncToNode", func() error { return bt.DEKSyncToNode(ctx, nil) }},
		{"TenantDEKGetOrCreate", func() error { _, e := bt.TenantDEKGetOrCreate(ctx, nil); return e }},
		{"TenantDEKRotate", func() error { return bt.TenantDEKRotate(ctx, nil) }},
		{"TenantDEKDelete", func() error { return bt.TenantDEKDelete(ctx, nil) }},
		{"TenantDEKList", func() error { _, e := bt.TenantDEKList(ctx); return e }},
		{"TenantDEKGetStatus", func() error { _, e := bt.TenantDEKGetStatus(ctx, nil); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// SealStoreClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_SealStoreStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"SealStorePut", func() error { return bt.SealStorePut(ctx, nil) }},
		{"SealStoreGet", func() error { _, e := bt.SealStoreGet(ctx, nil); return e }},
		{"SealStoreDelete", func() error { return bt.SealStoreDelete(ctx, nil) }},
		{"SealStoreList", func() error { _, e := bt.SealStoreList(ctx); return e }},
		{"SealStoreReseal", func() error { return bt.SealStoreReseal(ctx, nil) }},
		{"SealStoreStatus", func() error { _, e := bt.SealStoreStatus(ctx); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// CustodianGroup + Share + Tenant stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_CustodianGroupStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateCustodianGroup", func() error { _, e := bt.CreateCustodianGroup(ctx, nil); return e }},
		{"GetCustodianGroup", func() error { _, e := bt.GetCustodianGroup(ctx, "g"); return e }},
		{"ListCustodianGroups", func() error { _, e := bt.ListCustodianGroups(ctx); return e }},
		{"DeleteCustodianGroup", func() error { return bt.DeleteCustodianGroup(ctx, "g") }},
		{"AddCustodianMember", func() error { _, e := bt.AddCustodianMember(ctx, nil); return e }},
		{"RemoveCustodianMember", func() error { return bt.RemoveCustodianMember(ctx, nil) }},
		{"DistributeShares", func() error { _, e := bt.DistributeShares(ctx, nil); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

func TestBaasTransport_ShareStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"SubmitShare", func() error { _, e := bt.SubmitShare(ctx, nil); return e }},
		{"ListShares", func() error { _, e := bt.ListShares(ctx); return e }},
		{"GetShareCollectionStatus", func() error { _, e := bt.GetShareCollectionStatus(ctx, "s"); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

func TestBaasTransport_TenantStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateTenant", func() error { _, e := bt.CreateTenant(ctx, nil); return e }},
		{"GetTenant", func() error { _, e := bt.GetTenant(ctx, "t"); return e }},
		{"ListTenants", func() error { _, e := bt.ListTenants(ctx); return e }},
		{"DeleteTenant", func() error { return bt.DeleteTenant(ctx, "t") }},
		{"TenantBarrierInit", func() error { return bt.TenantBarrierInit(ctx, nil) }},
		{"TenantBarrierUnseal", func() error { return bt.TenantBarrierUnseal(ctx, nil) }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// CeremonyClient + CredentialClient stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_CeremonyStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CeremonyInitialize", func() error { _, e := bt.CeremonyInitialize(ctx, nil); return e }},
		{"CeremonyStatus", func() error { _, e := bt.CeremonyStatus(ctx); return e }},
		{"CeremonyClaimCertBegin", func() error { _, e := bt.CeremonyClaimCertBegin(ctx, nil); return e }},
		{"CeremonyClaimCertComplete", func() error { _, e := bt.CeremonyClaimCertComplete(ctx, nil); return e }},
		{"CeremonyClaimShare", func() error { _, e := bt.CeremonyClaimShare(ctx, nil); return e }},
		{"CeremonySignCSR", func() error { _, e := bt.CeremonySignCSR(ctx, nil); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

func TestBaasTransport_CredentialStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CredentialSubmit", func() error { return bt.CredentialSubmit(ctx, nil) }},
		{"CredentialStrategy", func() error { _, e := bt.CredentialStrategy(ctx); return e }},
		{"CredentialDelete", func() error { return bt.CredentialDelete(ctx, "") }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// Collection (Document) stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_CollectionStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateCollection", func() error { _, e := bt.CreateCollection(ctx, transport.CreateCollectionRequest{}); return e }},
		{"GetCollectionInfo", func() error { _, e := bt.GetCollectionInfo(ctx, "", ""); return e }},
		{"ListCollections", func() error { _, e := bt.ListCollections(ctx, ""); return e }},
		{"DropCollection", func() error { return bt.DropCollection(ctx, "", "") }},
		{"InsertDocument", func() error { _, e := bt.InsertDocument(ctx, transport.InsertDocumentRequest{}); return e }},
		{"GetDocument", func() error { _, e := bt.GetDocument(ctx, "", "", 0); return e }},
		{"UpdateDocument", func() error { return bt.UpdateDocument(ctx, transport.UpdateDocumentRequest{}) }},
		{"DeleteDocument", func() error { return bt.DeleteDocument(ctx, "", "", 0) }},
		{"ListDocuments", func() error { _, e := bt.ListDocuments(ctx, transport.ListDocumentsRequest{}); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// Backup + Rebalance + Index stubs
// ---------------------------------------------------------------------------

func TestBaasTransport_BackupStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"CreateBackup", func() error { _, e := bt.CreateBackup(ctx, nil); return e }},
		{"ListBackups", func() error { _, e := bt.ListBackups(ctx); return e }},
		{"GetBackup", func() error { _, e := bt.GetBackup(ctx, ""); return e }},
		{"DeleteBackup", func() error { return bt.DeleteBackup(ctx, "") }},
		{"RestoreBackup", func() error { _, e := bt.RestoreBackup(ctx, nil); return e }},
		{"VerifyBackup", func() error { _, e := bt.VerifyBackup(ctx, ""); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

func TestBaasTransport_RebalanceStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"GetShardStats", func() error { _, e := bt.GetShardStats(ctx, 1); return e }},
		{"CreateRebalancePlan", func() error { _, e := bt.CreateRebalancePlan(ctx, nil); return e }},
		{"ExecuteRebalance", func() error { _, e := bt.ExecuteRebalance(ctx, nil); return e }},
		{"GetImbalanceReport", func() error { _, e := bt.GetImbalanceReport(ctx); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

func TestBaasTransport_IndexStubs(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	stubOps := []struct {
		name string
		fn   func() error
	}{
		{"IndexCreate", func() error { return bt.IndexCreate(ctx, "", "", transport.IndexConfig{}) }},
		{"IndexDrop", func() error { return bt.IndexDrop(ctx, "") }},
		{"IndexQuery", func() error { _, e := bt.IndexQuery(ctx, "", ""); return e }},
		{"IndexScan", func() error { _, e := bt.IndexScan(ctx, "", "", ""); return e }},
		{"IndexList", func() error { _, e := bt.IndexList(ctx); return e }},
	}

	for _, op := range stubOps {
		t.Run(op.name+" returns not-supported", func(t *testing.T) {
			err := op.fn()
			var nsErr *BaasTransportNotSupportedError
			assert.True(t, errors.As(err, &nsErr))
		})
	}
}

// ---------------------------------------------------------------------------
// BaasTransportStoreError
// ---------------------------------------------------------------------------

func TestBaasTransportStoreError(t *testing.T) {
	t.Run("formats error with shard and backend info", func(t *testing.T) {
		inner := &ServerMessageError{Message: "disk full"}
		err := &BaasTransportStoreError{ShardID: 42, Backend: "pebble", Err: inner}
		assert.Contains(t, err.Error(), "shard 42")
		assert.Contains(t, err.Error(), "pebble")
		assert.Contains(t, err.Error(), "disk full")
	})

	t.Run("unwrap returns inner error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "fail"}
		err := &BaasTransportStoreError{ShardID: 1, Backend: "memory", Err: inner}
		assert.Equal(t, inner, err.Unwrap())
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore
// ---------------------------------------------------------------------------

func TestBaasTransportStore_BackendDefaults(t *testing.T) {
	t.Run("nil config defaults to memory", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		assert.Equal(t, "memory", s.backend())
		assert.Equal(t, "", s.dataDir())
	})

	t.Run("empty backend defaults to memory", func(t *testing.T) {
		s := newBaasTransportStore(&config.StorageConfig{Backend: ""})
		assert.Equal(t, "memory", s.backend())
	})
}

func TestBaasTransportStore_MemoryBackend(t *testing.T) {
	s := newBaasTransportStore(nil)

	t.Run("creates memory store for shard", func(t *testing.T) {
		store, err := s.storeForShard(1)
		require.NoError(t, err)
		assert.NotNil(t, store)
	})

	t.Run("returns same store on subsequent calls", func(t *testing.T) {
		s1, err1 := s.storeForShard(1)
		require.NoError(t, err1)
		s2, err2 := s.storeForShard(1)
		require.NoError(t, err2)
		assert.Same(t, s1, s2)
	})

	t.Run("close is safe on memory stores", func(t *testing.T) {
		err := s.Close()
		require.NoError(t, err)
	})
}

func TestBaasTransportStore_FileBackend(t *testing.T) {
	tmpDir := t.TempDir()
	s := newBaasTransportStore(&config.StorageConfig{
		Backend: "file",
		DataDir: tmpDir,
	})

	t.Run("creates file store for shard", func(t *testing.T) {
		store, err := s.storeForShard(1)
		require.NoError(t, err)
		assert.NotNil(t, store)
	})

	t.Run("close is safe on file stores", func(t *testing.T) {
		err := s.Close()
		require.NoError(t, err)
	})
}

func TestBaasTransportStore_PebbleBackend(t *testing.T) {
	tmpDir := t.TempDir()
	s := newBaasTransportStore(&config.StorageConfig{
		Backend: "pebble",
		DataDir: tmpDir,
	})

	t.Run("creates pebble store for shard", func(t *testing.T) {
		store, err := s.storeForShard(1)
		require.NoError(t, err)
		assert.NotNil(t, store)
	})

	t.Run("close closes pebble stores", func(t *testing.T) {
		err := s.Close()
		require.NoError(t, err)
	})
}

func TestBaasTransportStore_TransportForShard(t *testing.T) {
	s := newBaasTransportStore(nil)

	t.Run("returns transport backed by store", func(t *testing.T) {
		tr, err := s.transportForShard(1)
		require.NoError(t, err)
		assert.NotNil(t, tr)

		// Verify transport can do basic KV operations
		ctx := context.Background()
		require.NoError(t, tr.Put(ctx, "key", []byte("val")))
		val, err := tr.Get(ctx, "key")
		require.NoError(t, err)
		assert.Equal(t, []byte("val"), val)
	})
}
