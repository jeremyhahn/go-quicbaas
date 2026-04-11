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
	"sync/atomic"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// distTestLogger is a no-op logger for distributed search tests.
type distTestLogger struct{}

func (l *distTestLogger) Debug(msg string, args ...any)                                    {}
func (l *distTestLogger) Debugf(format string, args ...interface{})           {}
func (l *distTestLogger) Info(msg string, args ...any)                                     {}
func (l *distTestLogger) Infof(format string, args ...interface{})            {}
func (l *distTestLogger) Warn(msg string, args ...any)                                     {}
func (l *distTestLogger) Warnf(format string, args ...interface{})            {}
func (l *distTestLogger) Error(msg string, args ...any)                                    {}
func (l *distTestLogger) Errorf(format string, args ...interface{})           {}
func (l *distTestLogger) Fatal(msg string)                                    {}
func (l *distTestLogger) Fatalf(format string, args ...interface{})           {}
func (l *distTestLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l *distTestLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l *distTestLogger) WithError(error) interfaces.Logger                   { return l }

var _ interfaces.Logger = (*distTestLogger)(nil)

// mockShardSearcher is a test double for ShardSearcher.
type mockShardSearcher struct {
	// results maps shard ID -> results to return.
	results map[uint64][]*VectorSearchResult

	// errors maps shard ID -> error to return.
	errors map[uint64]error

	// callCount tracks how many times SearchShard was called.
	callCount atomic.Int32

	// delay is an optional delay before returning results.
	delay time.Duration
}

func newMockShardSearcher() *mockShardSearcher {
	return &mockShardSearcher{
		results: make(map[uint64][]*VectorSearchResult),
		errors:  make(map[uint64]error),
	}
}

func (m *mockShardSearcher) SearchShard(
	ctx context.Context,
	shard DistributedShardInfo,
	req *SearchSimilarRequest,
) ([]*VectorSearchResult, error) {
	m.callCount.Add(1)

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	if err, ok := m.errors[shard.ShardID]; ok {
		return nil, err
	}
	return m.results[shard.ShardID], nil
}

var _ ShardSearcher = (*mockShardSearcher)(nil)

func TestDistributedSearch_SingleShard(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{
		{ID: 10, Distance: 0.1},
		{ID: 20, Distance: 0.3},
	}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
	}
	req := &SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0, 2.0},
		K:           10,
	}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.ShardsSearched != 1 {
		t.Errorf("expected 1 shard searched, got %d", resp.ShardsSearched)
	}
	if resp.ShardsErrored != 0 {
		t.Errorf("expected 0 shards errored, got %d", resp.ShardsErrored)
	}
	if len(resp.ShardMetadata) != 1 {
		t.Errorf("expected 1 shard metadata entry, got %d", len(resp.ShardMetadata))
	}
}

func TestDistributedSearch_SingleShard_InvalidK(t *testing.T) {
	searcher := newMockShardSearcher()
	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
	}
	req := &SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           0,
	}

	_, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err == nil {
		t.Fatal("expected error for k=0")
	}
	var de *dberrors.QBError
	if !errors.As(err, &de) || de.Code != dberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestDistributedSearch_MultipleShards(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{
		{ID: 10, Distance: 0.1},
		{ID: 11, Distance: 0.5},
	}
	searcher.results[2] = []*VectorSearchResult{
		{ID: 20, Distance: 0.2},
		{ID: 21, Distance: 0.4},
	}
	searcher.results[3] = []*VectorSearchResult{
		{ID: 30, Distance: 0.3},
		{ID: 31, Distance: 0.6},
	}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
		{ShardID: 3, NodeID: "node-3", Address: "localhost:9003"},
	}
	req := &SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0, 2.0},
		K:           4,
	}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(resp.Results))
	}

	// Verify sorted by distance.
	for i := 1; i < len(resp.Results); i++ {
		if resp.Results[i].Distance < resp.Results[i-1].Distance {
			t.Errorf("not sorted at index %d: %f < %f",
				i, resp.Results[i].Distance, resp.Results[i-1].Distance)
		}
	}

	if resp.ShardsSearched != 3 {
		t.Errorf("expected 3 shards searched, got %d", resp.ShardsSearched)
	}
	if int(searcher.callCount.Load()) != 3 {
		t.Errorf("expected 3 SearchShard calls, got %d", searcher.callCount.Load())
	}
}

func TestDistributedSearch_MultipleShards_AllShardsEmpty(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{}
	searcher.results[2] = []*VectorSearchResult{}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
}

func TestDistributedSearch_ShardFailure(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{
		{ID: 10, Distance: 0.1},
		{ID: 11, Distance: 0.3},
	}
	searcher.errors[2] = &dberrors.MessageError{Message: "connection refused"}
	searcher.results[3] = []*VectorSearchResult{
		{ID: 30, Distance: 0.2},
	}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
		{ShardID: 3, NodeID: "node-3", Address: "localhost:9003"},
	}
	req := &SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0, 2.0},
		K:           10,
	}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have results from shard 1 and 3.
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results from successful shards, got %d", len(resp.Results))
	}
	if resp.ShardsSearched != 2 {
		t.Errorf("expected 2 shards searched, got %d", resp.ShardsSearched)
	}
	if resp.ShardsErrored != 1 {
		t.Errorf("expected 1 shard errored, got %d", resp.ShardsErrored)
	}
}

func TestDistributedSearch_ShardFailure_AllFail(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.errors[1] = &dberrors.MessageError{Message: "down"}
	searcher.errors[2] = &dberrors.MessageError{Message: "down"}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	_, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err == nil {
		t.Fatal("expected error when all shards fail")
	}
	var de *dberrors.QBError
	if !errors.As(err, &de) || de.Code != dberrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestDistributedSearch_ContextCanceled(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.delay = 2 * time.Second // slow shard

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 100*time.Millisecond)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	_, err := coord.SearchAcrossShards(ctx, shards, req)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
}

func TestDistributedSearch_ContextCanceled_PreCanceled(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.delay = 5 * time.Second

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 50*time.Millisecond)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	// The coordinator timeout (50ms) will expire before the shards respond (5s delay).
	_, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err == nil {
		t.Fatal("expected error when timeout expires")
	}
}

func TestDistributedSearch_EmptyShards(t *testing.T) {
	searcher := newMockShardSearcher()
	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	_, err := coord.SearchAcrossShards(context.Background(), nil, req)
	if err == nil {
		t.Fatal("expected error for nil shards")
	}
	var de *dberrors.QBError
	if !errors.As(err, &de) || de.Code != dberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestDistributedSearch_EmptyShards_EmptySlice(t *testing.T) {
	searcher := newMockShardSearcher()
	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	_, err := coord.SearchAcrossShards(context.Background(), []DistributedShardInfo{}, req)
	if err == nil {
		t.Fatal("expected error for empty shards slice")
	}
}

func TestDistributedSearch_Deduplication(t *testing.T) {
	searcher := newMockShardSearcher()
	// Both shards return ID=100 with different distances.
	searcher.results[1] = []*VectorSearchResult{
		{ID: 100, Distance: 0.5},
		{ID: 200, Distance: 0.2},
	}
	searcher.results[2] = []*VectorSearchResult{
		{ID: 100, Distance: 0.1}, // closer
		{ID: 300, Distance: 0.3},
	}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           10,
	}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 unique IDs after dedup.
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results after dedup, got %d", len(resp.Results))
	}

	// Verify ID=100 has the closer distance.
	for _, r := range resp.Results {
		if r.ID == 100 {
			if r.Distance != 0.1 {
				t.Errorf("ID=100 should have distance 0.1, got %f", r.Distance)
			}
			return
		}
	}
	t.Error("ID=100 not found in deduplicated results")
}

func TestDistributedSearch_Deduplication_AllDuplicates(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
	}
	searcher.results[2] = []*VectorSearchResult{
		{ID: 1, Distance: 0.3},
	}
	searcher.results[3] = []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
	}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
		{ShardID: 3, NodeID: "node-3", Address: "localhost:9003"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result after dedup, got %d", len(resp.Results))
	}
	if resp.Results[0].Distance != 0.1 {
		t.Errorf("expected distance 0.1, got %f", resp.Results[0].Distance)
	}
}

func TestDistributedSearch_ResponseMetadata(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{{ID: 1, Distance: 0.1}}
	searcher.results[2] = []*VectorSearchResult{{ID: 2, Distance: 0.2}}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
	if resp.QueryDuration <= 0 {
		t.Error("expected positive query duration")
	}
	if len(resp.ShardMetadata) != 2 {
		t.Fatalf("expected 2 shard metadata entries, got %d", len(resp.ShardMetadata))
	}
	for _, meta := range resp.ShardMetadata {
		if meta.ResultsCount != 1 {
			t.Errorf("shard %d: expected 1 result count, got %d", meta.ShardID, meta.ResultsCount)
		}
	}
}

func TestDistributedSearch_ResponseMetadata_WithErrors(t *testing.T) {
	searcher := newMockShardSearcher()
	searcher.results[1] = []*VectorSearchResult{{ID: 1, Distance: 0.1}}
	searcher.errors[2] = &dberrors.MessageError{Message: "shard down"}

	coord := NewDistributedSearchCoordinator(searcher, &distTestLogger{}, 5*time.Second)

	shards := []DistributedShardInfo{
		{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"},
		{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"},
	}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	resp, err := coord.SearchAcrossShards(context.Background(), shards, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Failed shard should have ResultsCount = 0.
	for _, meta := range resp.ShardMetadata {
		if meta.ShardID == 2 && meta.ResultsCount != 0 {
			t.Errorf("failed shard should have 0 results, got %d", meta.ResultsCount)
		}
	}
}

// TestGRPCShardSearcher_SearchShard_NoTransport verifies that the gRPC searcher
// returns an appropriate error when the transport is not yet wired.
func TestGRPCShardSearcher_SearchShard_NoTransport(t *testing.T) {
	// We can't use a real QRDBClientProvider here, but we verify the constructor
	// and that SearchShard returns the expected error pattern.
	searcher := NewGRPCShardSearcher(nil, &distTestLogger{})

	shard := DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"}
	req := &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1.0}, K: 10}

	_, err := searcher.SearchShard(context.Background(), shard, req)
	if err == nil {
		t.Fatal("expected error from uninitialized gRPC searcher")
	}

	var de *dberrors.QBError
	if !errors.As(err, &de) || de.Code != dberrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestGRPCShardSearcher_Constructor(t *testing.T) {
	searcher := NewGRPCShardSearcher(nil, &distTestLogger{})
	if searcher == nil {
		t.Fatal("expected non-nil GRPCShardSearcher")
	}
	if searcher.logger == nil {
		t.Error("expected logger to be set")
	}
}
