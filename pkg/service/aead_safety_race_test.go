package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAEADSafetyTracker_SetKeyConfigConcurrentWithRecord runs SetKeyConfig
// and RecordKeyEncrypt on one key at the same time. Under -race it fails if
// the usage's Config is read and written without cfgMu.
func TestAEADSafetyTracker_SetKeyConfigConcurrentWithRecord(t *testing.T) {
	tracker := NewAEADSafetyTracker()
	ctx := context.Background()
	const keyID = "race-key"
	const rounds = 200

	tracker.SetKeyConfig(keyID, AEADSafetyConfig{MaxOperations: 1 << 40, WarningPercent: 80, AutoRotatePercent: 90})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			tracker.SetKeyConfig(keyID, AEADSafetyConfig{
				MaxOperations:     1 << 40,
				MaxBytes:          uint64(1<<40 + i),
				WarningPercent:    80,
				AutoRotatePercent: 90,
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			assert.NoError(t, tracker.RecordKeyEncrypt(ctx, keyID, 1))
		}
	}()
	wg.Wait()

	ops, _, _ := tracker.GetKeyUsage(keyID)
	assert.Equal(t, uint64(rounds), ops)
}
