package qrdbservice_test

import (
	"context"
	"os"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// TestRealNode_TenantBoundCallsKeepTheTenantPrefix pins Codex review 6681's
// P1-1. A tenant REST job reaches the wrappers with its caller's tenant bound;
// the service principal is a system identity, and qrdb refuses a system
// identity acting for another tenant. The job-lease transaction and the KV
// wrappers run as the system tenant, while the caller's tenant stays in the
// keys they write.
func TestRealNode_TenantBoundCallsKeepTheTenantPrefix(t *testing.T) {
	n := qrdbnode.Shared(t)
	const tenant = "qrdbservice-bound"
	ctx := tenantctx.WithTenant(context.Background(), tenant)
	leaseKey := "jobs/" + tenant + "/lease/nightly"
	recordKey := "jobs/" + tenant + "/record/nightly"

	resp, err := n.Transaction.ExecuteTransaction(ctx, &transport.TransactionRequest{
		Compares: []transport.Compare{{
			Key:            []byte(leaseKey),
			Target:         transport.CompareCreateRevision,
			Result:         transport.CompareEqual,
			CreateRevision: 0,
		}},
		SuccessOps: []transport.Op{{Type: transport.OpPut, Key: []byte(leaseKey), Value: []byte("holder")}},
	})
	require.NoError(t, err, "job-lease transaction with a tenant-bound context")
	require.True(t, resp.Succeeded, "the first lease claim must win")

	require.NoError(t, n.KV.Put(ctx, recordKey, []byte("record")), "KV Put with a tenant-bound context")
	got, err := n.KV.Get(ctx, recordKey)
	require.NoError(t, err, "KV Get with a tenant-bound context")
	require.Equal(t, []byte("record"), got)

	lease, err := n.KV.Get(context.Background(), leaseKey)
	require.NoError(t, err, "lease under the caller's tenant prefix")
	require.Equal(t, []byte("holder"), lease)
}
