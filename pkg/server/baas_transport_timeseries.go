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

package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Key prefixes for time-series data stored in the KV store.
const (
	tsKVPrefix        = "ts:"
	tsMetricIdxPrefix = "ts:idx:metric:"
	tsRetentionPrefix = "ts:retention:"
)

// tsEncodedPoint is the JSON-serializable representation of a time-series
// data point stored in the KV store.
type tsEncodedPoint struct {
	Metric      string            `json:"m"`
	Value       float64           `json:"v"`
	TimestampNs int64             `json:"t"`
	Tags        map[string]string `json:"g,omitempty"`
}

// ---------------------------------------------------------------------------
// transport.TimeSeriesClient - KV-backed implementations with optional
// delegation to TimeSeriesStorage when available.
// ---------------------------------------------------------------------------

// TSWrite stores a single time-series data point. When timeSeriesStorage
// is available, the write is delegated to the real timeseries storage
// backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSWrite(ctx context.Context, point *transport.TSDataPoint) error {
	if t.timeSeriesStorage != nil {
		return t.tsWritePointToStorage(ctx, point)
	}
	return t.tsWritePointToStore(ctx, point)
}

// TSWritePoint stores a single validated data point. When timeSeriesStorage
// is available, the write is delegated to the real timeseries storage
// backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSWritePoint(ctx context.Context, point *transport.TSDataPoint) error {
	if t.timeSeriesStorage != nil {
		return t.tsWritePointToStorage(ctx, point)
	}
	return t.tsWritePointToStore(ctx, point)
}

// TSWriteBatch stores multiple time-series data points. When timeSeriesStorage
// is available, the batch write is delegated to the real timeseries storage
// backend. Otherwise each point is written individually to the KV store.
func (t *baasTransport) TSWriteBatch(ctx context.Context, points []transport.TSDataPoint) error {
	if t.timeSeriesStorage != nil {
		return t.tsWriteBatchToStorage(ctx, points)
	}
	for i := range points {
		if err := t.tsWritePointToStore(ctx, &points[i]); err != nil {
			return err
		}
	}
	return nil
}

// TSQuery retrieves data points for a metric within a time range. When
// timeSeriesStorage is available, the query is delegated to the real
// timeseries storage backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSQuery(ctx context.Context, req *transport.TSQueryRequest) (*transport.TSQueryResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsQueryViaStorage(ctx, req)
	}
	return t.tsQueryViaStore(ctx, req)
}

// TSAggregate computes an aggregation over data points in a time range. When
// timeSeriesStorage is available, the aggregation is delegated to the real
// timeseries storage backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSAggregate(ctx context.Context, req *transport.TSAggregateRequest) (*transport.TSAggregateResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsAggregateViaStorage(ctx, req)
	}
	return t.tsAggregateViaStore(ctx, req)
}

// TSRichQuery performs a rich query with aggregation, downsampling, and
// grouping. When timeSeriesStorage is available, the query is delegated
// to the real timeseries storage backend. Otherwise it falls back to the
// KV store.
func (t *baasTransport) TSRichQuery(ctx context.Context, req *transport.TSRichQueryRequest) (*transport.TSRichQueryResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsRichQueryViaStorage(ctx, req)
	}
	return t.tsRichQueryViaStore(ctx, req)
}

// TSDeleteSeries deletes time-series data matching the given criteria. When
// timeSeriesStorage is available, the delete is delegated to the real
// timeseries storage backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSDeleteSeries(ctx context.Context, req *transport.TSDeleteRequest) (*transport.TSDeleteResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsDeleteSeriesViaStorage(ctx, req)
	}
	return t.tsDeleteSeriesViaStore(ctx, req)
}

// TSGetRetention retrieves the retention policy for a metric. When
// timeSeriesStorage is available, the query is delegated to the real
// timeseries storage backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSGetRetention(ctx context.Context, req *transport.TSGetRetentionRequest) (*transport.TSGetRetentionResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsGetRetentionViaStorage(req)
	}
	return t.tsGetRetentionViaStore(ctx, req)
}

// TSSetRetention sets the retention policy for a metric. When
// timeSeriesStorage is available, the write is delegated to the real
// timeseries storage backend. Otherwise it falls back to the KV store.
func (t *baasTransport) TSSetRetention(ctx context.Context, req *transport.TSSetRetentionRequest) error {
	if t.timeSeriesStorage != nil {
		return t.tsSetRetentionViaStorage(req)
	}
	return t.tsSetRetentionViaStore(ctx, req)
}

// TSListMetrics lists metric names matching a prefix. When timeSeriesStorage
// is available, the query is delegated to the real timeseries storage backend.
// Otherwise it falls back to the KV store.
func (t *baasTransport) TSListMetrics(ctx context.Context, req *transport.TSListMetricsRequest) (*transport.TSListMetricsResponse, error) {
	if t.timeSeriesStorage != nil {
		return t.tsListMetricsViaStorage(req)
	}
	return t.tsListMetricsViaStore(ctx, req)
}

// ---------------------------------------------------------------------------
// TimeSeriesStorage delegation helpers
// ---------------------------------------------------------------------------

// tsWritePointToStorage delegates a single point write to the real
// timeseries storage backend.
func (t *baasTransport) tsWritePointToStorage(ctx context.Context, point *transport.TSDataPoint) error {
	if point == nil {
		return nil
	}
	ts := time.Unix(0, point.TimestampNs)
	if point.TimestampNs == 0 {
		ts = time.Now()
	}
	dp := &interfaces.DataPoint{
		Metric:    point.Metric,
		Value:     point.Value,
		Timestamp: ts,
		Tags:      tsTagsToMap(point.Tags),
	}
	return t.timeSeriesStorage.WritePoint(ctx, dp)
}

// tsWriteBatchToStorage delegates a batch write to the real timeseries
// storage backend.
func (t *baasTransport) tsWriteBatchToStorage(_ context.Context, points []transport.TSDataPoint) error {
	if len(points) == 0 {
		return nil
	}
	dps := make([]interfaces.DataPoint, len(points))
	for i, p := range points {
		ts := time.Unix(0, p.TimestampNs)
		if p.TimestampNs == 0 {
			ts = time.Now()
		}
		dps[i] = interfaces.DataPoint{
			Metric:    p.Metric,
			Value:     p.Value,
			Timestamp: ts,
			Tags:      tsTagsToMap(p.Tags),
		}
	}
	return t.timeSeriesStorage.WriteBatch(dps)
}

// tsQueryViaStorage delegates a query to the real timeseries storage backend.
func (t *baasTransport) tsQueryViaStorage(ctx context.Context, req *transport.TSQueryRequest) (*transport.TSQueryResponse, error) {
	if req == nil {
		return &transport.TSQueryResponse{}, nil
	}

	startTime := time.Unix(0, req.StartNs)
	endTime := time.Unix(0, req.EndNs)
	if req.StartNs == 0 {
		startTime = time.Time{}
	}
	if req.EndNs == 0 {
		endTime = time.Time{}
	}

	storageReq := &interfaces.QueryRequest{
		Metric:    req.Metric,
		StartTime: startTime,
		EndTime:   endTime,
		Tags:      tsTagsToMap(req.Tags),
		Limit:     int(req.Limit),
	}

	result, err := t.timeSeriesStorage.QueryRich(ctx, storageReq)
	if err != nil {
		return nil, err
	}

	var points []transport.TSDataPoint
	if result != nil && len(result.Points) > 0 {
		points = make([]transport.TSDataPoint, len(result.Points))
		for i, p := range result.Points {
			points[i] = transport.TSDataPoint{
				Metric:      p.Metric,
				Value:       p.Value,
				TimestampNs: p.Timestamp.UnixNano(),
				Tags:        tsMapToTags(p.Tags),
			}
		}
	}

	// Sort by timestamp.
	sort.Slice(points, func(i, j int) bool {
		if req.Descending {
			return points[i].TimestampNs > points[j].TimestampNs
		}
		return points[i].TimestampNs < points[j].TimestampNs
	})

	// Apply limit.
	if req.Limit > 0 && int(req.Limit) < len(points) {
		points = points[:req.Limit]
	}

	return &transport.TSQueryResponse{Points: points}, nil
}

// tsAggregateViaStorage delegates an aggregation to the real timeseries
// storage backend by querying points and computing the aggregation locally.
func (t *baasTransport) tsAggregateViaStorage(ctx context.Context, req *transport.TSAggregateRequest) (*transport.TSAggregateResponse, error) {
	if req == nil {
		return &transport.TSAggregateResponse{}, nil
	}

	// Query all points in the range via storage.
	queryResp, err := t.tsQueryViaStorage(ctx, &transport.TSQueryRequest{
		Metric:  req.Metric,
		StartNs: req.StartNs,
		EndNs:   req.EndNs,
	})
	if err != nil {
		return nil, err
	}

	if len(queryResp.Points) == 0 {
		return &transport.TSAggregateResponse{}, nil
	}

	// Single bucket aggregation when no interval is specified.
	if req.IntervalNs <= 0 {
		result := tsAggregatePoints(queryResp.Points, req.Function, req.StartNs)
		return &transport.TSAggregateResponse{Results: []transport.TSAggResult{result}}, nil
	}

	// Bucket points by interval.
	buckets := make(map[int64][]transport.TSDataPoint)
	for _, p := range queryResp.Points {
		bucketKey := ((p.TimestampNs - req.StartNs) / req.IntervalNs) * req.IntervalNs
		bucketTS := req.StartNs + bucketKey
		buckets[bucketTS] = append(buckets[bucketTS], p)
	}

	// Sort bucket keys.
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	results := make([]transport.TSAggResult, 0, len(keys))
	for _, k := range keys {
		results = append(results, tsAggregatePoints(buckets[k], req.Function, k))
	}

	return &transport.TSAggregateResponse{Results: results}, nil
}

// tsRichQueryViaStorage delegates a rich query to the real timeseries
// storage backend.
func (t *baasTransport) tsRichQueryViaStorage(ctx context.Context, req *transport.TSRichQueryRequest) (*transport.TSRichQueryResponse, error) {
	if req == nil {
		return &transport.TSRichQueryResponse{}, nil
	}

	queryResp, err := t.tsQueryViaStorage(ctx, &transport.TSQueryRequest{
		Metric:  req.Metric,
		StartNs: req.StartNs,
		EndNs:   req.EndNs,
		Tags:    req.Tags,
		Limit:   int32(req.Limit),
	})
	if err != nil {
		return nil, err
	}

	resp := &transport.TSRichQueryResponse{
		Metric: req.Metric,
		Tags:   req.Tags,
		Points: queryResp.Points,
	}

	// Apply aggregation if requested.
	if req.Aggregation != transport.AggNone && len(queryResp.Points) > 0 {
		result := tsAggregatePoints(queryResp.Points, req.Aggregation, req.StartNs)
		resp.AggregatedValue = &result.Value
	}

	return resp, nil
}

// tsDeleteSeriesViaStorage delegates a delete to the real timeseries
// storage backend.
func (t *baasTransport) tsDeleteSeriesViaStorage(ctx context.Context, req *transport.TSDeleteRequest) (*transport.TSDeleteResponse, error) {
	if req == nil {
		return &transport.TSDeleteResponse{}, nil
	}

	startTime := time.Unix(0, req.StartNs)
	endTime := time.Unix(0, req.EndNs)
	if req.StartNs == 0 {
		startTime = time.Time{}
	}
	if req.EndNs == 0 {
		endTime = time.Time{}
	}

	delReq := &interfaces.DeleteRequest{
		Metric:    req.Metric,
		Tags:      tsTagsToMap(req.Tags),
		StartTime: startTime,
		EndTime:   endTime,
	}

	if err := t.timeSeriesStorage.DeleteSeries(ctx, delReq); err != nil {
		return nil, err
	}

	return &transport.TSDeleteResponse{}, nil
}

// tsGetRetentionViaStorage delegates a retention policy get to the real
// timeseries storage backend.
func (t *baasTransport) tsGetRetentionViaStorage(req *transport.TSGetRetentionRequest) (*transport.TSGetRetentionResponse, error) {
	if req == nil {
		return &transport.TSGetRetentionResponse{}, nil
	}

	policy, err := t.timeSeriesStorage.GetRetentionPolicy(req.Metric)
	if err != nil {
		// No retention policy set; return empty.
		return &transport.TSGetRetentionResponse{
			Policy: &transport.TSRetentionPolicy{},
		}, nil
	}

	if policy == nil {
		return &transport.TSGetRetentionResponse{
			Policy: &transport.TSRetentionPolicy{},
		}, nil
	}

	return &transport.TSGetRetentionResponse{
		Policy: &transport.TSRetentionPolicy{
			DurationNs:   int64(policy.Duration),
			ResolutionNs: int64(policy.Resolution),
			Aggregation:  transport.AggFunc(policy.Aggregation),
		},
	}, nil
}

// tsSetRetentionViaStorage delegates a retention policy set to the real
// timeseries storage backend.
func (t *baasTransport) tsSetRetentionViaStorage(req *transport.TSSetRetentionRequest) error {
	if req == nil {
		return nil
	}

	if req.Policy == nil {
		return t.timeSeriesStorage.SetRetentionPolicy(req.Metric, nil)
	}

	policy := &interfaces.RetentionPolicy{
		Duration:    time.Duration(req.Policy.DurationNs),
		Resolution:  time.Duration(req.Policy.ResolutionNs),
		Aggregation: interfaces.AggFunc(req.Policy.Aggregation),
	}
	return t.timeSeriesStorage.SetRetentionPolicy(req.Metric, policy)
}

// tsListMetricsViaStorage delegates a metric list to the real timeseries
// storage backend.
func (t *baasTransport) tsListMetricsViaStorage(req *transport.TSListMetricsRequest) (*transport.TSListMetricsResponse, error) {
	if req == nil {
		return &transport.TSListMetricsResponse{}, nil
	}

	metrics, err := t.timeSeriesStorage.ListMetrics(req.Prefix, int(req.Limit))
	if err != nil {
		return &transport.TSListMetricsResponse{}, nil
	}

	return &transport.TSListMetricsResponse{Metrics: metrics}, nil
}

// ---------------------------------------------------------------------------
// KV store fallback implementations
// ---------------------------------------------------------------------------

// tsQueryViaStore retrieves data points from the KV store.
func (t *baasTransport) tsQueryViaStore(ctx context.Context, req *transport.TSQueryRequest) (*transport.TSQueryResponse, error) {
	if req == nil {
		return &transport.TSQueryResponse{}, nil
	}

	prefix := tsKVPrefix + req.Metric + ":"
	var points []transport.TSDataPoint

	err := t.store.Scan(ctx, prefix, func(key string, value []byte) error {
		var ep tsEncodedPoint
		if jsonErr := json.Unmarshal(value, &ep); jsonErr != nil {
			return nil // skip corrupt entries
		}

		// Filter by time range.
		if req.StartNs > 0 && ep.TimestampNs < req.StartNs {
			return nil
		}
		if req.EndNs > 0 && ep.TimestampNs > req.EndNs {
			return nil
		}

		// Filter by tags if specified.
		if len(req.Tags) > 0 && !tsTagsMatch(ep.Tags, req.Tags) {
			return nil
		}

		points = append(points, transport.TSDataPoint{
			Metric:      ep.Metric,
			Value:       ep.Value,
			TimestampNs: ep.TimestampNs,
			Tags:        tsMapToTags(ep.Tags),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by timestamp.
	sort.Slice(points, func(i, j int) bool {
		if req.Descending {
			return points[i].TimestampNs > points[j].TimestampNs
		}
		return points[i].TimestampNs < points[j].TimestampNs
	})

	// Apply limit.
	if req.Limit > 0 && int(req.Limit) < len(points) {
		points = points[:req.Limit]
	}

	return &transport.TSQueryResponse{Points: points}, nil
}

// tsAggregateViaStore computes an aggregation using the KV store.
func (t *baasTransport) tsAggregateViaStore(ctx context.Context, req *transport.TSAggregateRequest) (*transport.TSAggregateResponse, error) {
	if req == nil {
		return &transport.TSAggregateResponse{}, nil
	}

	// Query all points in the range.
	queryResp, err := t.tsQueryViaStore(ctx, &transport.TSQueryRequest{
		Metric:  req.Metric,
		StartNs: req.StartNs,
		EndNs:   req.EndNs,
	})
	if err != nil {
		return nil, err
	}

	if len(queryResp.Points) == 0 {
		return &transport.TSAggregateResponse{}, nil
	}

	// Single bucket aggregation when no interval is specified.
	if req.IntervalNs <= 0 {
		result := tsAggregatePoints(queryResp.Points, req.Function, req.StartNs)
		return &transport.TSAggregateResponse{Results: []transport.TSAggResult{result}}, nil
	}

	// Bucket points by interval.
	buckets := make(map[int64][]transport.TSDataPoint)
	for _, p := range queryResp.Points {
		bucketKey := ((p.TimestampNs - req.StartNs) / req.IntervalNs) * req.IntervalNs
		bucketTS := req.StartNs + bucketKey
		buckets[bucketTS] = append(buckets[bucketTS], p)
	}

	// Sort bucket keys.
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	results := make([]transport.TSAggResult, 0, len(keys))
	for _, k := range keys {
		results = append(results, tsAggregatePoints(buckets[k], req.Function, k))
	}

	return &transport.TSAggregateResponse{Results: results}, nil
}

// tsRichQueryViaStore performs a rich query using the KV store.
func (t *baasTransport) tsRichQueryViaStore(ctx context.Context, req *transport.TSRichQueryRequest) (*transport.TSRichQueryResponse, error) {
	if req == nil {
		return &transport.TSRichQueryResponse{}, nil
	}

	queryResp, err := t.tsQueryViaStore(ctx, &transport.TSQueryRequest{
		Metric:  req.Metric,
		StartNs: req.StartNs,
		EndNs:   req.EndNs,
		Tags:    req.Tags,
		Limit:   int32(req.Limit),
	})
	if err != nil {
		return nil, err
	}

	resp := &transport.TSRichQueryResponse{
		Metric: req.Metric,
		Tags:   req.Tags,
		Points: queryResp.Points,
	}

	// Apply aggregation if requested.
	if req.Aggregation != transport.AggNone && len(queryResp.Points) > 0 {
		result := tsAggregatePoints(queryResp.Points, req.Aggregation, req.StartNs)
		resp.AggregatedValue = &result.Value
	}

	return resp, nil
}

// tsDeleteSeriesViaStore deletes time-series data from the KV store.
func (t *baasTransport) tsDeleteSeriesViaStore(ctx context.Context, req *transport.TSDeleteRequest) (*transport.TSDeleteResponse, error) {
	if req == nil {
		return &transport.TSDeleteResponse{}, nil
	}

	prefix := tsKVPrefix + req.Metric + ":"
	var keysToDelete []string

	err := t.store.Scan(ctx, prefix, func(key string, value []byte) error {
		// If time range is specified, decode and filter.
		if req.StartNs > 0 || req.EndNs > 0 {
			var ep tsEncodedPoint
			if jsonErr := json.Unmarshal(value, &ep); jsonErr != nil {
				return nil
			}
			if req.StartNs > 0 && ep.TimestampNs < req.StartNs {
				return nil
			}
			if req.EndNs > 0 && ep.TimestampNs > req.EndNs {
				return nil
			}
		}
		keysToDelete = append(keysToDelete, key)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, key := range keysToDelete {
		if delErr := t.store.Delete(ctx, key); delErr != nil {
			return nil, delErr
		}
	}

	// Clean up metric index if all data deleted.
	if req.StartNs == 0 && req.EndNs == 0 {
		_ = t.store.Delete(ctx, tsMetricIdxPrefix+req.Metric)
	}

	return &transport.TSDeleteResponse{}, nil
}

// tsGetRetentionViaStore retrieves the retention policy from the KV store.
func (t *baasTransport) tsGetRetentionViaStore(ctx context.Context, req *transport.TSGetRetentionRequest) (*transport.TSGetRetentionResponse, error) {
	if req == nil {
		return &transport.TSGetRetentionResponse{}, nil
	}

	key := tsRetentionPrefix + req.Metric
	data, err := t.store.Get(ctx, key)
	if err != nil {
		// No retention policy set; return empty.
		return &transport.TSGetRetentionResponse{
			Policy: &transport.TSRetentionPolicy{},
		}, nil
	}

	var policy transport.TSRetentionPolicy
	if jsonErr := json.Unmarshal(data, &policy); jsonErr != nil {
		return &transport.TSGetRetentionResponse{
			Policy: &transport.TSRetentionPolicy{},
		}, nil
	}

	return &transport.TSGetRetentionResponse{Policy: &policy}, nil
}

// tsSetRetentionViaStore sets the retention policy in the KV store.
func (t *baasTransport) tsSetRetentionViaStore(ctx context.Context, req *transport.TSSetRetentionRequest) error {
	if req == nil {
		return nil
	}

	key := tsRetentionPrefix + req.Metric
	if req.Policy == nil {
		return t.store.Delete(ctx, key)
	}

	data, err := json.Marshal(req.Policy)
	if err != nil {
		return err
	}
	return t.store.Put(ctx, key, data)
}

// tsListMetricsViaStore lists metric names from the KV store.
func (t *baasTransport) tsListMetricsViaStore(ctx context.Context, req *transport.TSListMetricsRequest) (*transport.TSListMetricsResponse, error) {
	if req == nil {
		return &transport.TSListMetricsResponse{}, nil
	}

	prefix := tsMetricIdxPrefix
	if req.Prefix != "" {
		prefix = tsMetricIdxPrefix + req.Prefix
	}

	keys, err := t.store.List(ctx, prefix)
	if err != nil {
		return &transport.TSListMetricsResponse{}, nil
	}

	metrics := make([]string, 0, len(keys))
	for _, key := range keys {
		metric := strings.TrimPrefix(key, tsMetricIdxPrefix)
		if metric != "" {
			metrics = append(metrics, metric)
		}
	}

	if req.Limit > 0 && int(req.Limit) < len(metrics) {
		metrics = metrics[:req.Limit]
	}

	return &transport.TSListMetricsResponse{Metrics: metrics}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// tsWritePointToStore encodes a TSDataPoint and stores it in the KV store.
func (t *baasTransport) tsWritePointToStore(ctx context.Context, point *transport.TSDataPoint) error {
	if point == nil {
		return nil
	}

	ts := point.TimestampNs
	if ts == 0 {
		ts = time.Now().UnixNano()
	}

	tags := tsTagsToMap(point.Tags)
	key := tsBuildKey(point.Metric, ts, tags)

	ep := tsEncodedPoint{
		Metric:      point.Metric,
		Value:       point.Value,
		TimestampNs: ts,
		Tags:        tags,
	}

	data, err := json.Marshal(&ep)
	if err != nil {
		return err
	}

	if putErr := t.store.Put(ctx, key, data); putErr != nil {
		return putErr
	}

	// Update metric index.
	return t.store.Put(ctx, tsMetricIdxPrefix+point.Metric, []byte{1})
}

// tsBuildKey constructs a KV key for a time-series data point.
// Format: "ts:" + metric + ":" + hex(timestamp_be) + ":" + hex(tag_hash).
func tsBuildKey(metric string, timestampNs int64, tags map[string]string) string {
	var buf strings.Builder
	buf.Grow(len(tsKVPrefix) + len(metric) + 1 + 16 + 1 + 16)

	buf.WriteString(tsKVPrefix)
	buf.WriteString(metric)
	buf.WriteByte(':')

	// Encode timestamp as 8-byte big-endian hex for lexicographic ordering.
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(timestampNs))
	tsHexAppend(&buf, tsBuf[:])

	buf.WriteByte(':')

	// Encode tag hash as 16 hex chars.
	tagHash := tsHashTags(tags)
	var hashBuf [8]byte
	binary.BigEndian.PutUint64(hashBuf[:], tagHash)
	tsHexAppend(&buf, hashBuf[:])

	return buf.String()
}

// tsHexAppend appends bytes as lowercase hexadecimal to the builder.
func tsHexAppend(buf *strings.Builder, data []byte) {
	const hexDigits = "0123456789abcdef"
	for _, b := range data {
		buf.WriteByte(hexDigits[b>>4])
		buf.WriteByte(hexDigits[b&0x0f])
	}
}

// tsHashTags computes a deterministic FNV-1a 64-bit hash of the tag map.
// Keys are sorted for determinism. Compatible with the qrdb timeseries format.
func tsHashTags(tags map[string]string) uint64 {
	if len(tags) == 0 {
		return 0
	}

	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(tags[k]))
		_, _ = h.Write([]byte(";"))
	}
	return h.Sum64()
}

// tsTagsToMap converts a slice of transport.TSTag to a map.
func tsTagsToMap(tags []transport.TSTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, tag := range tags {
		m[tag.Key] = tag.Value
	}
	return m
}

// tsMapToTags converts a map to a slice of transport.TSTag.
func tsMapToTags(m map[string]string) []transport.TSTag {
	if len(m) == 0 {
		return nil
	}
	tags := make([]transport.TSTag, 0, len(m))
	for k, v := range m {
		tags = append(tags, transport.TSTag{Key: k, Value: v})
	}
	// Sort for deterministic output.
	sort.Slice(tags, func(i, j int) bool { return tags[i].Key < tags[j].Key })
	return tags
}

// tsTagsMatch returns true if all required tags are present in the data point tags.
func tsTagsMatch(pointTags map[string]string, required []transport.TSTag) bool {
	for _, tag := range required {
		if v, ok := pointTags[tag.Key]; !ok || v != tag.Value {
			return false
		}
	}
	return true
}

// tsAggregatePoints computes an aggregation over a slice of data points.
func tsAggregatePoints(points []transport.TSDataPoint, fn transport.AggFunc, bucketTS int64) transport.TSAggResult {
	if len(points) == 0 {
		return transport.TSAggResult{TimestampNs: bucketTS}
	}

	result := transport.TSAggResult{
		TimestampNs: bucketTS,
		Count:       int64(len(points)),
	}

	switch fn {
	case transport.AggSum:
		var sum float64
		for _, p := range points {
			sum += p.Value
		}
		result.Value = sum

	case transport.AggAvg:
		var sum float64
		for _, p := range points {
			sum += p.Value
		}
		result.Value = sum / float64(len(points))

	case transport.AggMin:
		minVal := math.Inf(1)
		for _, p := range points {
			if p.Value < minVal {
				minVal = p.Value
			}
		}
		result.Value = minVal

	case transport.AggMax:
		maxVal := math.Inf(-1)
		for _, p := range points {
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
		result.Value = maxVal

	case transport.AggCount:
		result.Value = float64(len(points))

	case transport.AggFirst:
		result.Value = points[0].Value

	case transport.AggLast:
		result.Value = points[len(points)-1].Value

	default:
		// AggNone or unsupported - return sum as default.
		var sum float64
		for _, p := range points {
			sum += p.Value
		}
		result.Value = sum
	}

	return result
}
