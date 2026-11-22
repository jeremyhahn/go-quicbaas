package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// TestKMSService_ProductionFacadeWiresKeyRegistryIntoBackends drives the
// production construction (newKMSService over a kms.KMSService whose
// registry holds a real XKMSBackend, then SetKVStore) and proves the
// backends receive the durable key registry: without it, storedVersion
// infers versions from names and deleting "report" deletes an
// independent "report_v2".
func TestKMSService_ProductionFacadeWiresKeyRegistryIntoBackends(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	assert.Same(t, manager.GetRegistry(), svc.backendRegistry,
		"the service must adopt the production manager's backend registry")
	kv := kvstore.NewMemory()
	svc.SetKVStore(kv)

	ctx := systemTenantCtx()
	gen := func(name string) string {
		resp, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
			Name:          name,
			Usage:         []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
			KeyAttributes: buildSymmetricAttrs(),
		})
		require.NoError(t, err)
		return resp.Key.KeyID
	}
	reportID := gen("report")
	report2ID := gen("report_v2")

	_, err := svc.DeleteKey(ctx, &DeleteKeyRequest{KeyID: reportID, Force: true})
	require.NoError(t, err)

	_, err = svc.GetKey(ctx, &GetKeyRequest{KeyID: report2ID})
	assert.NoError(t, err, "deleting report must not delete the independent report_v2")
	_, err = manager.Encrypt(ctx, report2ID, []byte("p"), nil)
	assert.NoError(t, err, "report_v2's material must survive the delete")
}
