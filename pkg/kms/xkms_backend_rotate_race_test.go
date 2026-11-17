package kms

import (
	"context"
	"fmt"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestXKMSBackend_EncryptConcurrentRotate_RealXKMS runs Encrypt and RotateKey
// at the same time on one key. Encrypt must read the cached metadata's
// version once under the lock, so the key it encrypts with and the version it
// records in the blob agree: every returned blob then decrypts. Run under
// -race it also proves Encrypt no longer reads the version unlocked.
func TestXKMSBackend_EncryptConcurrentRotate_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	const keyID = "rot-enc-race"
	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	const (
		encrypters = 4
		perWorker  = 15
		rotations  = 8
	)

	type sealed struct {
		plaintext []byte
		blob      []byte
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []sealed
		errs    []error
	)

	for w := 0; w < encrypters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				pt := []byte(fmt.Sprintf("worker %d message %d", w, i))
				blob, encErr := backend.Encrypt(ctx, keyID, pt, nil)
				mu.Lock()
				if encErr != nil {
					errs = append(errs, encErr)
				} else {
					results = append(results, sealed{plaintext: pt, blob: blob})
				}
				mu.Unlock()
			}
		}(w)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := 0; r < rotations; r++ {
			if _, rotErr := backend.RotateKey(ctx, keyID); rotErr != nil {
				mu.Lock()
				errs = append(errs, rotErr)
				mu.Unlock()
			}
		}
	}()

	wg.Wait()
	require.Empty(t, errs)
	require.Len(t, results, encrypters*perWorker)

	for _, r := range results {
		got, decErr := backend.Decrypt(ctx, keyID, r.blob, nil)
		require.NoError(t, decErr)
		require.Equal(t, r.plaintext, got)
	}
}
