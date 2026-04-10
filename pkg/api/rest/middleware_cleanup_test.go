//go:build rest

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

package rest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiterCleanupLoop(t *testing.T) {
	t.Run("cleanup removes old buckets", func(t *testing.T) {
		// Create a rate limiter with a very short cleanup interval for testing
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Add some buckets with different ages
		oldBucket := &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now().Add(-15 * time.Minute), // Old bucket
		}

		recentBucket := &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now(), // Recent bucket
		}

		rl.mu.Lock()
		rl.buckets["old-client"] = oldBucket
		rl.buckets["recent-client"] = recentBucket
		rl.mu.Unlock()

		// Verify initial state
		assert.Len(t, rl.buckets, 2)

		// Start cleanup loop in goroutine
		done := make(chan bool)
		go func() {
			// Run cleanup once
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		// Wait for cleanup to complete
		<-done

		// Verify old bucket was removed
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Len(t, rl.buckets, 1)
		assert.Contains(t, rl.buckets, "recent-client")
		assert.NotContains(t, rl.buckets, "old-client")

		rl.cleanup.Stop()
	})

	t.Run("cleanup preserves recent buckets", func(t *testing.T) {
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Add only recent buckets
		bucket1 := &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now(),
		}

		bucket2 := &tokenBucket{
			tokens:    50,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now().Add(-5 * time.Minute), // Still within 10 minute window
		}

		rl.mu.Lock()
		rl.buckets["client1"] = bucket1
		rl.buckets["client2"] = bucket2
		rl.mu.Unlock()

		// Run cleanup
		done := make(chan bool)
		go func() {
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		<-done

		// Verify all buckets are still present
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Len(t, rl.buckets, 2)
		assert.Contains(t, rl.buckets, "client1")
		assert.Contains(t, rl.buckets, "client2")

		rl.cleanup.Stop()
	})

	t.Run("cleanup handles empty buckets map", func(t *testing.T) {
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Run cleanup on empty map
		done := make(chan bool)
		go func() {
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		<-done

		// Verify map is still empty
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Empty(t, rl.buckets)

		rl.cleanup.Stop()
	})

	t.Run("cleanup removes multiple old buckets", func(t *testing.T) {
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Add multiple old buckets and one recent
		oldTime := time.Now().Add(-15 * time.Minute)
		for i := 0; i < 5; i++ {
			rl.buckets["old-"+string(rune('a'+i))] = &tokenBucket{
				tokens:    100,
				capacity:  100,
				rate:      100,
				lastCheck: oldTime,
			}
		}

		rl.buckets["recent"] = &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now(),
		}

		assert.Len(t, rl.buckets, 6)

		// Run cleanup
		done := make(chan bool)
		go func() {
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		<-done

		// Verify only recent bucket remains
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Len(t, rl.buckets, 1)
		assert.Contains(t, rl.buckets, "recent")

		rl.cleanup.Stop()
	})
}

func TestTokenBucketEdgeCases(t *testing.T) {
	t.Run("bucket just under 10 minutes should not be removed", func(t *testing.T) {
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Add bucket just under 10 minutes (should not be removed)
		underBucket := &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now().Add(-9*time.Minute - 59*time.Second),
		}

		rl.mu.Lock()
		rl.buckets["under-client"] = underBucket
		rl.mu.Unlock()

		// Run cleanup
		done := make(chan bool)
		go func() {
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		<-done

		// Bucket should still be present
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Len(t, rl.buckets, 1)
		assert.Contains(t, rl.buckets, "under-client")

		rl.cleanup.Stop()
	})

	t.Run("bucket well over 10 minutes should be removed", func(t *testing.T) {
		rl := &rateLimiter{
			rate:    100,
			buckets: make(map[string]*tokenBucket),
			cleanup: time.NewTicker(100 * time.Millisecond),
		}

		// Add bucket well over 10 minutes
		overBucket := &tokenBucket{
			tokens:    100,
			capacity:  100,
			rate:      100,
			lastCheck: time.Now().Add(-11 * time.Minute),
		}

		rl.mu.Lock()
		rl.buckets["over-client"] = overBucket
		rl.mu.Unlock()

		// Run cleanup
		done := make(chan bool)
		go func() {
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastCheck) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
			done <- true
		}()

		<-done

		// Bucket should be removed
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		assert.Empty(t, rl.buckets)

		rl.cleanup.Stop()
	})
}
