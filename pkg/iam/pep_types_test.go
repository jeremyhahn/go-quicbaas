//go:build iam

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
	"testing"
)

func TestPolicyUpdateType_String(t *testing.T) {
	tests := []struct {
		name     string
		ut       PolicyUpdateType
		expected string
	}{
		{
			name:     "snapshot type",
			ut:       PolicyUpdateSnapshot,
			expected: "snapshot",
		},
		{
			name:     "created type",
			ut:       PolicyUpdateCreated,
			expected: "created",
		},
		{
			name:     "updated type",
			ut:       PolicyUpdateUpdated,
			expected: "updated",
		},
		{
			name:     "deleted type",
			ut:       PolicyUpdateDeleted,
			expected: "deleted",
		},
		{
			name:     "batch type",
			ut:       PolicyUpdateBatch,
			expected: "batch",
		},
		{
			name:     "unknown type - negative value",
			ut:       PolicyUpdateType(-1),
			expected: "unknown",
		},
		{
			name:     "unknown type - out of range value",
			ut:       PolicyUpdateType(999),
			expected: "unknown",
		},
		{
			name:     "unknown type - just beyond batch",
			ut:       PolicyUpdateType(PolicyUpdateBatch + 1),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ut.String()
			if result != tt.expected {
				t.Errorf("PolicyUpdateType.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPolicyUpdateType_Values(t *testing.T) {
	// Verify the iota values are sequential starting from 0
	if PolicyUpdateSnapshot != 0 {
		t.Errorf("PolicyUpdateSnapshot = %d, want 0", PolicyUpdateSnapshot)
	}
	if PolicyUpdateCreated != 1 {
		t.Errorf("PolicyUpdateCreated = %d, want 1", PolicyUpdateCreated)
	}
	if PolicyUpdateUpdated != 2 {
		t.Errorf("PolicyUpdateUpdated = %d, want 2", PolicyUpdateUpdated)
	}
	if PolicyUpdateDeleted != 3 {
		t.Errorf("PolicyUpdateDeleted = %d, want 3", PolicyUpdateDeleted)
	}
	if PolicyUpdateBatch != 4 {
		t.Errorf("PolicyUpdateBatch = %d, want 4", PolicyUpdateBatch)
	}
}

func TestPolicyUpdate_Fields(t *testing.T) {
	// Test that PolicyUpdate struct can be created with all fields
	update := PolicyUpdate{
		Type:     PolicyUpdateCreated,
		PolicyID: "test-policy-id",
		Version:  1,
	}

	if update.Type != PolicyUpdateCreated {
		t.Errorf("PolicyUpdate.Type = %v, want %v", update.Type, PolicyUpdateCreated)
	}
	if update.PolicyID != "test-policy-id" {
		t.Errorf("PolicyUpdate.PolicyID = %s, want %s", update.PolicyID, "test-policy-id")
	}
	if update.Version != 1 {
		t.Errorf("PolicyUpdate.Version = %d, want 1", update.Version)
	}
}

func TestBatchCheckRequest_Fields(t *testing.T) {
	// Test that BatchCheckRequest struct can be created
	req := &BatchCheckRequest{
		Checks: []*CheckRequest{
			{
				Subject: Subject{
					ID:       "user-1",
					TenantID: "tenant-1",
				},
				Action:   "read",
				Resource: "resource-1",
			},
		},
	}

	if len(req.Checks) != 1 {
		t.Errorf("BatchCheckRequest.Checks length = %d, want 1", len(req.Checks))
	}
	if req.Checks[0].Subject.ID != "user-1" {
		t.Errorf("BatchCheckRequest.Checks[0].Subject.ID = %s, want user-1", req.Checks[0].Subject.ID)
	}
}

func TestBatchCheckResponse_Fields(t *testing.T) {
	// Test that BatchCheckResponse struct can be created
	resp := &BatchCheckResponse{
		Responses: []*CheckResponse{
			{
				Allowed: true,
				Reason:  "policy allows",
			},
		},
		TotalEvaluationTimeNs: 1000,
	}

	if len(resp.Responses) != 1 {
		t.Errorf("BatchCheckResponse.Responses length = %d, want 1", len(resp.Responses))
	}
	if !resp.Responses[0].Allowed {
		t.Error("BatchCheckResponse.Responses[0].Allowed should be true")
	}
	if resp.TotalEvaluationTimeNs != 1000 {
		t.Errorf("BatchCheckResponse.TotalEvaluationTimeNs = %d, want 1000", resp.TotalEvaluationTimeNs)
	}
}

func TestPEPStats_Fields(t *testing.T) {
	// Test that PEPStats struct can be created with all fields
	stats := &PEPStats{
		ChecksTotal:      100,
		ChecksAllowed:    80,
		ChecksDenied:     20,
		AverageLatencyNs: 5000,
		P50LatencyNs:     4000,
		P95LatencyNs:     8000,
		P99LatencyNs:     10000,
		CacheHits:        90,
		CacheMisses:      10,
		CacheHitRate:     0.9,
		PolicyUpdates:    5,
		SyncErrors:       1,
		CachedPolicies:   50,
	}

	if stats.ChecksTotal != 100 {
		t.Errorf("PEPStats.ChecksTotal = %d, want 100", stats.ChecksTotal)
	}
	if stats.ChecksAllowed != 80 {
		t.Errorf("PEPStats.ChecksAllowed = %d, want 80", stats.ChecksAllowed)
	}
	if stats.ChecksDenied != 20 {
		t.Errorf("PEPStats.ChecksDenied = %d, want 20", stats.ChecksDenied)
	}
	if stats.AverageLatencyNs != 5000 {
		t.Errorf("PEPStats.AverageLatencyNs = %d, want 5000", stats.AverageLatencyNs)
	}
	if stats.P50LatencyNs != 4000 {
		t.Errorf("PEPStats.P50LatencyNs = %d, want 4000", stats.P50LatencyNs)
	}
	if stats.P95LatencyNs != 8000 {
		t.Errorf("PEPStats.P95LatencyNs = %d, want 8000", stats.P95LatencyNs)
	}
	if stats.P99LatencyNs != 10000 {
		t.Errorf("PEPStats.P99LatencyNs = %d, want 10000", stats.P99LatencyNs)
	}
	if stats.CacheHits != 90 {
		t.Errorf("PEPStats.CacheHits = %d, want 90", stats.CacheHits)
	}
	if stats.CacheMisses != 10 {
		t.Errorf("PEPStats.CacheMisses = %d, want 10", stats.CacheMisses)
	}
	if stats.CacheHitRate != 0.9 {
		t.Errorf("PEPStats.CacheHitRate = %f, want 0.9", stats.CacheHitRate)
	}
	if stats.PolicyUpdates != 5 {
		t.Errorf("PEPStats.PolicyUpdates = %d, want 5", stats.PolicyUpdates)
	}
	if stats.SyncErrors != 1 {
		t.Errorf("PEPStats.SyncErrors = %d, want 1", stats.SyncErrors)
	}
	if stats.CachedPolicies != 50 {
		t.Errorf("PEPStats.CachedPolicies = %d, want 50", stats.CachedPolicies)
	}
}
