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

// Package service provides the unified service facade for QuicBaaS.
// This file implements scatter-gather distributed k-NN search across Raft shard replicas.
package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ShardSearcher executes a search on a specific shard. This interface
// decouples the distributed coordinator from the transport layer, enabling
// unit testing without real network calls.
type ShardSearcher interface {
	// SearchShard executes a k-NN search on the specified shard and returns
	// the per-shard results.
	SearchShard(ctx context.Context, shard DistributedShardInfo, req *SearchSimilarRequest) ([]*VectorSearchResult, error)
}

// ShardSearchMeta contains metadata about a single shard's search execution.
type ShardSearchMeta struct {
	// ShardID is the shard that was searched.
	ShardID uint64

	// ResultsCount is the number of results returned from this shard.
	ResultsCount int32

	// SearchDurationMs is the search duration in milliseconds.
	SearchDurationMs int64
}

// DistributedSearchResponse extends the standard search response with
// per-shard metadata for observability.
type DistributedSearchResponse struct {
	// Results contains the merged global top-K results.
	Results []*VectorSearchResult

	// Timestamp is the search timestamp.
	Timestamp int64

	// QueryDuration is the total search duration in microseconds.
	QueryDuration int64

	// ShardMetadata contains per-shard search metadata.
	ShardMetadata []*ShardSearchMeta

	// ShardsSearched is the number of shards that returned results.
	ShardsSearched int

	// ShardsErrored is the number of shards that returned errors.
	ShardsErrored int
}

// shardResult holds the result from a single shard search.
type shardResult struct {
	shardID  uint64
	results  []*VectorSearchResult
	duration time.Duration
	err      error
}

// DistributedSearchCoordinator fans out search queries to shard owners
// and merges results using a min-heap for global top-K selection.
type DistributedSearchCoordinator struct {
	searcher ShardSearcher
	logger   interfaces.Logger
	timeout  time.Duration
}

// NewDistributedSearchCoordinator creates a coordinator for distributed
// k-NN search across shards.
func NewDistributedSearchCoordinator(
	searcher ShardSearcher,
	logger interfaces.Logger,
	timeout time.Duration,
) *DistributedSearchCoordinator {
	return &DistributedSearchCoordinator{
		searcher: searcher,
		logger:   logger,
		timeout:  timeout,
	}
}

// SearchAcrossShards performs scatter-gather k-NN search across multiple shards.
// It sends parallel search requests to each shard, collects results, and merges
// them using a max-heap to return the global top-K results.
//
// Partial failure behavior: if some shards fail but at least one succeeds,
// the coordinator returns the merged results from the successful shards along
// with shard metadata indicating which shards failed.
func (d *DistributedSearchCoordinator) SearchAcrossShards(
	ctx context.Context,
	shards []DistributedShardInfo,
	req *SearchSimilarRequest,
) (*DistributedSearchResponse, error) {
	if len(shards) == 0 {
		return nil, api.NewDistributedSearchNoShardsError("DistributedSearchCoordinator.SearchAcrossShards", req.Namespace)
	}

	if req.K <= 0 {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "DistributedSearchCoordinator.SearchAcrossShards",
			Err:  &dberrors.MessageError{Message: "k must be greater than zero"},
		}
	}

	// Apply timeout from coordinator config or parent context.
	searchCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	start := time.Now()

	// Fan out searches to all shards concurrently.
	resultsCh := make(chan shardResult, len(shards))
	var wg sync.WaitGroup

	for _, shard := range shards {
		wg.Add(1)
		go func(s DistributedShardInfo) {
			defer wg.Done()
			shardStart := time.Now()
			results, err := d.searcher.SearchShard(searchCtx, s, req)
			resultsCh <- shardResult{
				shardID:  s.ShardID,
				results:  results,
				duration: time.Since(shardStart),
				err:      err,
			}
		}(shard)
	}

	// Close the channel once all goroutines complete.
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results from all shards.
	var (
		allShardResults [][]*VectorSearchResult
		shardMeta       []*ShardSearchMeta
		shardsSearched  atomic.Int32
		shardsErrored   atomic.Int32
	)

	for sr := range resultsCh {
		meta := &ShardSearchMeta{
			ShardID:          sr.shardID,
			SearchDurationMs: sr.duration.Milliseconds(),
		}

		if sr.err != nil {
			shardsErrored.Add(1)
			meta.ResultsCount = 0
			d.logger.Warnf("shard %d search failed: %v", sr.shardID, sr.err)
		} else {
			shardsSearched.Add(1)
			meta.ResultsCount = int32(len(sr.results))
			allShardResults = append(allShardResults, sr.results)
		}
		shardMeta = append(shardMeta, meta)
	}

	// Check context for cancellation/timeout.
	if searchCtx.Err() != nil {
		if shardsSearched.Load() == 0 {
			return nil, api.NewDistributedSearchTimeoutError(
				"DistributedSearchCoordinator.SearchAcrossShards",
				req.Namespace,
				d.timeout,
			)
		}
		// Partial results available despite timeout.
		d.logger.Warnf("distributed search for namespace %s timed out, returning partial results from %d shards",
			req.Namespace, shardsSearched.Load())
	}

	// All shards failed.
	if shardsSearched.Load() == 0 {
		return nil, api.NewShardUnavailableError(
			"DistributedSearchCoordinator.SearchAcrossShards",
			0,
			"all",
			&dberrors.MessageError{Message: "all shards failed during distributed search"},
		)
	}

	// Merge results from all successful shards.
	merged := MergeSearchResults(allShardResults, req.K)

	elapsed := time.Since(start)

	return &DistributedSearchResponse{
		Results:        merged,
		Timestamp:      time.Now().UnixMicro(),
		QueryDuration:  elapsed.Microseconds(),
		ShardMetadata:  shardMeta,
		ShardsSearched: int(shardsSearched.Load()),
		ShardsErrored:  int(shardsErrored.Load()),
	}, nil
}

// GRPCShardSearcher implements ShardSearcher using the QRDBClientProvider
// to send gRPC search requests to remote shard owners.
type GRPCShardSearcher struct {
	sdkProvider QRDBClientProvider
	logger      interfaces.Logger
}

// NewGRPCShardSearcher creates a ShardSearcher that uses gRPC via the
// QRDBClientProvider to search remote shards.
func NewGRPCShardSearcher(
	sdkProvider QRDBClientProvider,
	logger interfaces.Logger,
) *GRPCShardSearcher {
	return &GRPCShardSearcher{
		sdkProvider: sdkProvider,
		logger:      logger,
	}
}

// SearchShard executes a k-NN search on the specified shard via gRPC.
func (g *GRPCShardSearcher) SearchShard(
	ctx context.Context,
	shard DistributedShardInfo,
	req *SearchSimilarRequest,
) ([]*VectorSearchResult, error) {
	if g.sdkProvider == nil {
		return nil, api.NewShardUnavailableError(
			"GRPCShardSearcher.SearchShard",
			shard.ShardID,
			shard.NodeID,
			&dberrors.MessageError{Message: "SDK provider not initialized"},
		)
	}

	client, err := g.sdkProvider.ForShard(shard.ShardID)
	if err != nil {
		return nil, api.NewShardUnavailableError(
			"GRPCShardSearcher.SearchShard",
			shard.ShardID,
			shard.NodeID,
			err,
		)
	}

	// Build the SDK transport search request from the service-level request.
	sdkReq := &qrdbtransport.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     req.QueryVector,
		K:               req.K,
		EfSearch:        req.EfSearch,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  req.MetadataFilter,
		ShardID:         shard.ShardID,
		Consistency:     uint8(req.Consistency),
	}

	// Execute the gRPC vector search via the SDK client.
	resp, err := client.Vector().SearchSimilar(ctx, sdkReq)
	if err != nil {
		return nil, api.NewShardUnavailableError(
			"GRPCShardSearcher.SearchShard",
			shard.ShardID,
			shard.NodeID,
			err,
		)
	}

	// Convert SDK transport results to service-level results.
	results := make([]*VectorSearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = &VectorSearchResult{
			ID:       r.ID,
			Score:    r.Score,
			Vector:   r.Vector,
			Metadata: r.Metadata,
			Distance: r.Distance,
			Rank:     r.Rank,
		}
	}

	return results, nil
}
