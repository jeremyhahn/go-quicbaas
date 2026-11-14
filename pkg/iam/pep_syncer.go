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

package iam

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// PolicySyncer synchronizes policies from the IAM server to the local cache.
// It runs periodic sync and applies incremental updates.
type PolicySyncer struct {
	// client is the IAM client for fetching policies
	client IAMClient

	// cache is the policy cache to update
	cache *PolicyCache

	// tenantID is the tenant to sync policies for
	tenantID string

	// interval is the sync interval
	interval time.Duration

	// stopCh signals the syncer to stop
	stopCh chan struct{}

	// wg tracks running goroutines
	wg sync.WaitGroup

	// running indicates if the syncer is running
	running atomic.Bool

	// lastSyncTime tracks the last successful sync
	lastSyncTime atomic.Int64

	// syncErrors tracks the number of sync errors
	syncErrors atomic.Int64

	// mu protects sync operations
	mu sync.Mutex
}

// NewPolicySyncer creates a new policy syncer.
func NewPolicySyncer(client IAMClient, cache *PolicyCache, interval time.Duration, tenantID string) *PolicySyncer {
	return &PolicySyncer{
		client:   client,
		cache:    cache,
		tenantID: tenantID,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the policy syncer background process.
func (s *PolicySyncer) Start(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return nil // Already running
	}

	// Perform initial sync
	if err := s.SyncNow(ctx); err != nil {
		// Log error but don't fail startup
		s.syncErrors.Add(1)
	}

	// Start background sync goroutine
	s.wg.Add(1)
	go s.syncLoop(ctx)

	return nil
}

// Stop stops the policy syncer.
func (s *PolicySyncer) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return // Not running
	}

	close(s.stopCh)
	s.wg.Wait()
}

// SyncNow performs an immediate policy sync.
func (s *PolicySyncer) SyncNow(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	startTime := time.Now()

	// Fetch policies from IAM server
	// Use wildcard pattern to fetch all policies
	policies, err := s.client.FetchPolicies(ctx, s.tenantID, []string{"**"})
	if err != nil {
		s.syncErrors.Add(1)
		return NewPolicySyncError(s.tenantID, err)
	}

	// Update cache with fetched policies
	for _, policy := range policies {
		s.cache.Set(s.tenantID, policy)
	}

	// Update last sync time
	s.lastSyncTime.Store(time.Now().Unix())

	// Evict expired entries
	s.cache.EvictExpired()

	// Enforce max size
	s.cache.EnforceMaxSize()

	_ = startTime // For future metrics/logging
	return nil
}

// syncLoop runs the periodic sync process.
func (s *PolicySyncer) syncLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return

		case <-ctx.Done():
			// The loop has ended, so the syncer is no longer running (#49):
			// IsRunning reports the loop, not whether Stop was called. The
			// flag is cleared before returning, so a Start that sees it
			// false always starts a fresh loop.
			s.running.Store(false)
			return

		case <-ticker.C:
			// Perform sync
			if err := s.SyncNow(ctx); err != nil {
				// Log error but continue
				s.syncErrors.Add(1)
			}
		}
	}
}

// GetLastSyncTime returns the timestamp of the last successful sync.
func (s *PolicySyncer) GetLastSyncTime() time.Time {
	timestamp := s.lastSyncTime.Load()
	if timestamp == 0 {
		return time.Time{}
	}
	return time.Unix(timestamp, 0)
}

// GetSyncErrors returns the number of sync errors encountered.
func (s *PolicySyncer) GetSyncErrors() int64 {
	return s.syncErrors.Load()
}

// ResetSyncErrors resets the sync error counter.
func (s *PolicySyncer) ResetSyncErrors() {
	s.syncErrors.Store(0)
}

// IsRunning returns true if the syncer is running.
func (s *PolicySyncer) IsRunning() bool {
	return s.running.Load()
}
