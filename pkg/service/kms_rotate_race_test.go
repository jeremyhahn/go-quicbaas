package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// TestKMSService_RotateKey_ConcurrentEncryptUsage runs Crypto Encrypt in a
// loop on several goroutines while RotateKey runs, and checks that after the
// rotation the key's usage count equals the operations that ran on the new
// version: the reset can neither erase an operation counted against the new
// version nor leave one that ran on the old version. The limit is then
// enforced on the new version's count.
func TestKMSService_RotateKey_ConcurrentEncryptUsage(t *testing.T) {
	const keyID = "rotate-race-key"
	ctx := systemTenantCtx()

	var version atomic.Int64
	version.Store(1)
	var opsOnNewVersion atomic.Uint64

	kmsMgr := newMockKMSManager()
	kmsMgr.keys[keyID] = &mockKeyData{
		keyID:     keyID,
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	kmsMgr.rotateKeyFn = func(_ context.Context, id string) (interface{}, error) {
		// Widen the window between the version change and the usage reset.
		time.Sleep(2 * time.Millisecond)
		version.Store(2)
		time.Sleep(2 * time.Millisecond)
		return &kms.KeyMetadata{KeyID: id, Name: id, Algorithm: "AES-256-GCM", Version: 2, State: kms.KeyStateEnabled}, nil
	}
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.encryptFn = func(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
		v := version.Load()
		time.Sleep(50 * time.Microsecond)
		if v == 2 {
			opsOnNewVersion.Add(1)
		}
		return append([]byte("ct:"), plaintext...), nil
	}

	logger := newMockLogger()
	kmsSvc := newKMSService(kmsMgr, logger)
	tracker := kmsSvc.GetSafetyTracker()
	require.NotNil(t, tracker)
	cryptoSvc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)
	tracker.SetKeyConfig(scoped, AEADSafetyConfig{MaxOperations: 1 << 40})

	encrypt := func() error {
		_, err := cryptoSvc.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("pt")})
		return err
	}

	stop := make(chan struct{})
	var started sync.WaitGroup
	var wg sync.WaitGroup
	for range 8 {
		started.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			first := true
			for {
				select {
				case <-stop:
					return
				default:
				}
				assert.NoError(t, encrypt())
				if first {
					started.Done()
					first = false
				}
			}
		}()
	}
	started.Wait()

	_, err = kmsSvc.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)
	close(stop)
	wg.Wait()

	ops, _, _ := tracker.GetKeyUsage(scoped)
	require.Equal(t, opsOnNewVersion.Load(), ops,
		"the new version's usage equals the operations that ran on it")

	// The tracker refuses the operation that reaches MaxOperations.
	tracker.SetKeyConfig(scoped, AEADSafetyConfig{MaxOperations: ops + 2})
	require.NoError(t, encrypt())
	require.Error(t, encrypt(), "the limit is enforced on the new version's count")
}

func TestAEADSafetyTracker_KeyLock(t *testing.T) {
	tracker := NewAEADSafetyTracker()
	a := tracker.KeyLock("t/a")
	require.Same(t, a, tracker.KeyLock("t/a"))
	require.NotSame(t, a, tracker.KeyLock("t/b"))

	var zero AEADSafetyTracker
	require.NotNil(t, zero.KeyLock("k"))
}
