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
	"time"
)

// BatchCheckRequest represents a batch authorization check request.
type BatchCheckRequest struct {
	// Checks is the list of authorization checks to perform
	Checks []*CheckRequest `json:"checks" yaml:"checks"`
}

// BatchCheckResponse represents a batch authorization check response.
type BatchCheckResponse struct {
	// Responses is the list of authorization check responses
	Responses []*CheckResponse `json:"responses" yaml:"responses"`

	// TotalEvaluationTimeNs is the total evaluation duration in nanoseconds
	TotalEvaluationTimeNs int64 `json:"total_evaluation_time_ns" yaml:"total_evaluation_time_ns"`
}

// PolicyUpdate represents a policy update from the sync stream.
type PolicyUpdate struct {
	// Type is the update type (snapshot, created, updated, deleted)
	Type PolicyUpdateType `json:"type" yaml:"type"`

	// Policy is the updated policy (nil for delete)
	Policy *Policy `json:"policy,omitempty" yaml:"policy,omitempty"`

	// PolicyID is the policy identifier
	PolicyID string `json:"policy_id" yaml:"policy_id"`

	// Version is the policy version
	Version int64 `json:"version" yaml:"version"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
}

// PolicyUpdateType represents the type of policy update.
type PolicyUpdateType int

const (
	// PolicyUpdateSnapshot indicates a full policy snapshot.
	PolicyUpdateSnapshot PolicyUpdateType = iota

	// PolicyUpdateCreated indicates a new policy was created.
	PolicyUpdateCreated

	// PolicyUpdateUpdated indicates an existing policy was updated.
	PolicyUpdateUpdated

	// PolicyUpdateDeleted indicates a policy was deleted.
	PolicyUpdateDeleted

	// PolicyUpdateBatch indicates a batch of policy updates.
	PolicyUpdateBatch
)

// String returns the string representation of the update type.
func (t PolicyUpdateType) String() string {
	switch t {
	case PolicyUpdateSnapshot:
		return "snapshot"
	case PolicyUpdateCreated:
		return "created"
	case PolicyUpdateUpdated:
		return "updated"
	case PolicyUpdateDeleted:
		return "deleted"
	case PolicyUpdateBatch:
		return "batch"
	default:
		return "unknown"
	}
}

// PEPStats represents PEP performance statistics.
type PEPStats struct {
	// ChecksTotal is the total number of checks performed
	ChecksTotal int64 `json:"checks_total" yaml:"checks_total"`

	// ChecksAllowed is the number of checks that were allowed
	ChecksAllowed int64 `json:"checks_allowed" yaml:"checks_allowed"`

	// ChecksDenied is the number of checks that were denied
	ChecksDenied int64 `json:"checks_denied" yaml:"checks_denied"`

	// AverageLatencyNs is the average check latency in nanoseconds
	AverageLatencyNs int64 `json:"average_latency_ns" yaml:"average_latency_ns"`

	// P50LatencyNs is the p50 latency in nanoseconds
	P50LatencyNs int64 `json:"p50_latency_ns" yaml:"p50_latency_ns"`

	// P95LatencyNs is the p95 latency in nanoseconds
	P95LatencyNs int64 `json:"p95_latency_ns" yaml:"p95_latency_ns"`

	// P99LatencyNs is the p99 latency in nanoseconds
	P99LatencyNs int64 `json:"p99_latency_ns" yaml:"p99_latency_ns"`

	// CacheHits is the number of cache hits
	CacheHits int64 `json:"cache_hits" yaml:"cache_hits"`

	// CacheMisses is the number of cache misses
	CacheMisses int64 `json:"cache_misses" yaml:"cache_misses"`

	// CacheHitRate is the cache hit rate (0.0-1.0)
	CacheHitRate float64 `json:"cache_hit_rate" yaml:"cache_hit_rate"`

	// PolicyUpdates is the number of policy updates received
	PolicyUpdates int64 `json:"policy_updates" yaml:"policy_updates"`

	// SyncErrors is the number of sync errors
	SyncErrors int64 `json:"sync_errors" yaml:"sync_errors"`

	// LastSyncTime is the timestamp of the last successful sync
	LastSyncTime time.Time `json:"last_sync_time" yaml:"last_sync_time"`

	// CachedPolicies is the number of policies in cache
	CachedPolicies int64 `json:"cached_policies" yaml:"cached_policies"`
}

// IAMClient represents a client for communicating with the IAM server.
// This interface allows for different implementations (gRPC, HTTP, etc.).
type IAMClient interface {
	// FetchPolicies fetches all policies for a tenant
	FetchPolicies(tenantID string, patterns []string) ([]*Policy, error)

	// Subscribe subscribes to policy updates for a tenant
	Subscribe(tenantID string, patterns []string) (<-chan *PolicyUpdate, error)

	// Close closes the client connection
	Close() error
}
