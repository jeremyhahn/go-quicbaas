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

// TestAEADSafetyTracker_ObserveKeyVersion: the first observed version adopts
// the counted usage, a newer one starts the budget over, and an older or zero
// version changes nothing.
func TestAEADSafetyTracker_ObserveKeyVersion(t *testing.T) {
	ctx := context.Background()
	tracker := NewAEADSafetyTracker()
	tracker.SetKeyConfig("k", AEADSafetyConfig{MaxOperations: 1 << 40})
	for i := 0; i < 3; i++ {
		assert.NoError(t, tracker.RecordKeyEncrypt(ctx, "k", 16))
	}
	ops := func() uint64 {
		n, _, _ := tracker.GetKeyUsage("k")
		return n
	}

	tracker.ObserveKeyVersion("k", 1)
	assert.Equal(t, uint64(3), ops(), "the first observation keeps the usage")
	tracker.ObserveKeyVersion("k", 1)
	assert.Equal(t, uint64(3), ops())
	tracker.ObserveKeyVersion("k", 2)
	assert.Equal(t, uint64(0), ops(), "a newer version starts the budget over")

	assert.NoError(t, tracker.RecordKeyEncrypt(ctx, "k", 16))
	tracker.ObserveKeyVersion("k", 1)
	assert.Equal(t, uint64(1), ops(), "an older version changes nothing")
	tracker.ObserveKeyVersion("k", 0)
	assert.Equal(t, uint64(1), ops())
}
