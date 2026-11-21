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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAEADSafety_RotateKeyUsageOrdersUsesAroundTheReset proves that a key's
// rotation and its crypto calls never overlap: a use already holding the gate
// finishes before the rotation, so its count is erased with the old version's,
// and a use arriving during the rotation waits for it, so its count lands on
// the new version and survives.
func TestAEADSafety_RotateKeyUsageOrdersUsesAroundTheReset(t *testing.T) {
	ctx := context.Background()
	const key = "tenant/rotating-key"

	t.Run("a use in flight blocks the rotation and is counted before the reset", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()
		tracker.SetKeyConfig(key, AEADSafetyConfig{MaxOperations: 10})

		end := tracker.BeginKeyUse(key)
		if err := tracker.RecordKeyEncrypt(ctx, key, 1); err != nil {
			t.Fatalf("RecordKeyEncrypt: %v", err)
		}

		var rotated atomic.Bool
		done := make(chan error, 1)
		go func() {
			done <- tracker.RotateKeyUsage(key, func() error {
				rotated.Store(true)
				return nil
			})
		}()

		select {
		case <-done:
			t.Fatal("RotateKeyUsage returned while a use of the key held its gate")
		case <-time.After(50 * time.Millisecond):
		}
		if rotated.Load() {
			t.Fatal("rotate ran while a use of the key held its gate")
		}

		end()
		if err := <-done; err != nil {
			t.Fatalf("RotateKeyUsage: %v", err)
		}
		if ops, _, _ := tracker.GetKeyUsage(key); ops != 0 {
			t.Fatalf("operations after rotation = %d, want 0: the pre-rotation use belongs to the old version", ops)
		}
	})

	t.Run("a use during the rotation waits and is counted on the new version", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()
		tracker.SetKeyConfig(key, AEADSafetyConfig{MaxOperations: 10})
		for i := 0; i < 5; i++ {
			if err := tracker.RecordKeyEncrypt(ctx, key, 1); err != nil {
				t.Fatalf("seed RecordKeyEncrypt: %v", err)
			}
		}

		rotating := make(chan struct{})
		release := make(chan struct{})
		rotateDone := make(chan error, 1)
		go func() {
			rotateDone <- tracker.RotateKeyUsage(key, func() error {
				close(rotating)
				<-release
				return nil
			})
		}()

		<-rotating
		useDone := make(chan error, 1)
		go func() {
			end := tracker.BeginKeyUse(key)
			err := tracker.RecordKeyEncrypt(ctx, key, 1)
			end()
			useDone <- err
		}()

		select {
		case <-useDone:
			t.Fatal("a use of the key finished while its rotation held the gate")
		case <-time.After(50 * time.Millisecond):
		}

		close(release)
		if err := <-rotateDone; err != nil {
			t.Fatalf("RotateKeyUsage: %v", err)
		}
		if err := <-useDone; err != nil {
			t.Fatalf("post-rotation RecordKeyEncrypt: %v", err)
		}
		if ops, _, _ := tracker.GetKeyUsage(key); ops != 1 {
			t.Fatalf("operations after rotation = %d, want 1: the post-rotation use must be counted, not erased", ops)
		}
	})
}

// TestAEADSafety_RotateKeyUsageKeepsCountsWhenRotationFails proves a failed
// rotation leaves the key's counts as they were: the old version is still
// current, so its usage still applies.
func TestAEADSafety_RotateKeyUsageKeepsCountsWhenRotationFails(t *testing.T) {
	ctx := context.Background()
	const key = "tenant/unrotated-key"

	tracker := NewAEADSafetyTracker()
	tracker.SetKeyConfig(key, AEADSafetyConfig{MaxOperations: 10})
	for i := 0; i < 3; i++ {
		if err := tracker.RecordKeyEncrypt(ctx, key, 7); err != nil {
			t.Fatalf("seed RecordKeyEncrypt: %v", err)
		}
	}

	errRotate := errors.New("rotation refused")
	if err := tracker.RotateKeyUsage(key, func() error { return errRotate }); !errors.Is(err, errRotate) {
		t.Fatalf("RotateKeyUsage error = %v, want %v", err, errRotate)
	}

	ops, bytes, _ := tracker.GetKeyUsage(key)
	if ops != 3 || bytes != 21 {
		t.Fatalf("usage after a failed rotation = %d ops, %d bytes, want 3 ops, 21 bytes", ops, bytes)
	}

	// The gate is released: a later use and rotation proceed.
	end := tracker.BeginKeyUse(key)
	end()
	if err := tracker.RotateKeyUsage(key, func() error { return nil }); err != nil {
		t.Fatalf("RotateKeyUsage after a failed one: %v", err)
	}
	if ops, _, _ := tracker.GetKeyUsage(key); ops != 0 {
		t.Fatalf("operations after a successful rotation = %d, want 0", ops)
	}
}

// TestAEADSafety_ConfigChangesRaceFreeWithUse runs config writes against
// reservations and reads of one key. Under -race it fails if any Config
// access is unsynchronised.
func TestAEADSafety_ConfigChangesRaceFreeWithUse(t *testing.T) {
	ctx := context.Background()
	const (
		key        = "tenant/reconfigured-key"
		dek        = "tenant/reconfigured-dek"
		iterations = 200
	)

	tracker := NewAEADSafetyTracker()
	var wg sync.WaitGroup
	run := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				fn(i)
			}
		}()
	}

	run(func(i int) {
		tracker.SetKeyConfig(key, AEADSafetyConfig{MaxOperations: uint64(1000 + i), WarningPercent: 80, AutoRotatePercent: 95})
		tracker.SetDEKConfig(dek, AEADSafetyConfig{MaxBytes: uint64(1 << 20), WarningPercent: 50})
	})
	run(func(int) {
		tracker.ClearKeyConfig(key)
		tracker.ClearDEKConfig(dek)
	})
	run(func(int) { _ = tracker.RecordKeyEncrypt(ctx, key, 16) })
	run(func(int) { _ = tracker.RecordKeyEncryptBatch(ctx, key, 3, 48) })
	run(func(int) { _ = tracker.RecordDEKEncrypt(ctx, dek, 16) })
	run(func(int) { _ = tracker.SafetyCheck(0) })
	run(func(int) {
		_, _, _ = tracker.GetKeyUsage(key)
		_, _, _ = tracker.GetDEKUsage(dek)
	})
	wg.Wait()

	ops, _, _ := tracker.GetKeyUsage(key)
	if want := uint64(iterations + 3*iterations); ops != want {
		t.Fatalf("key operations = %d, want %d: a config change lost a reservation", ops, want)
	}
}

// TestAEADSafety_ResetNeverAdmitsAnOversizedBatch runs batch reservations
// against resets of one key. A batch's add and its limit check run under the
// usage record's lock, so a reset can only fall before or after them: a
// reservation of 3 under MaxOperations 4 is admitted only when it started
// from a count of zero, which takes a reset since the last admitted one.
func TestAEADSafety_ResetNeverAdmitsAnOversizedBatch(t *testing.T) {
	ctx := context.Background()
	const (
		key        = "tenant/batch-key"
		iterations = 200
	)

	tracker := NewAEADSafetyTracker()
	tracker.SetKeyConfig(key, AEADSafetyConfig{MaxOperations: 4})

	var admitted, resets atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if tracker.RecordKeyEncryptBatch(ctx, key, 3, 3) == nil {
					admitted.Add(1)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tracker.ResetKeyUsage(key)
			resets.Add(1)
		}
	}()
	wg.Wait()

	if got, limit := admitted.Load(), resets.Load()+1; got > limit {
		t.Fatalf("%d batches of 3 admitted under MaxOperations 4 with %d resets; at most %d can start from zero", got, resets.Load(), limit)
	}

	// With no reset in between, a batch on top of an admitted one is refused.
	tracker.ResetKeyUsage(key)
	if err := tracker.RecordKeyEncryptBatch(ctx, key, 3, 3); err != nil {
		t.Fatalf("batch from zero: %v", err)
	}
	if err := tracker.RecordKeyEncryptBatch(ctx, key, 3, 3); err == nil {
		t.Fatal("a batch taking the count to 6 under MaxOperations 4 was admitted")
	}
}
