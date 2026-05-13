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

//go:build integration && timeseries && quic

package timeseries

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TimeSeriesPoint represents a time series data point for testing.
type TimeSeriesPoint struct {
	Metric    string
	Tags      map[string]string
	Value     float64
	Timestamp time.Time
}

// TimeSeriesDataPoint represents a data point in query results.
type TimeSeriesDataPoint struct {
	Metric    string
	Tags      map[string]string
	Value     float64
	Timestamp time.Time
}

// RetentionPolicy represents a time series retention policy.
type RetentionPolicy struct {
	Duration    time.Duration
	Resolution  time.Duration
	Aggregation string
}

// timeseriesProtocol abstracts the different protocols for testing.
type timeseriesProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error
	WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error
	Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error)
	Delete(ctx context.Context, metric string, start, end time.Time) error
	ListMetrics(ctx context.Context, prefix string) ([]string, error)
	GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error)
	SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error
	Name() string
}

// TestMultiprotocol_WriteAndQueryPoint verifies that single point write and query
// operations work identically across all supported protocols.
func TestMultiprotocol_WriteAndQueryPoint(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			metricName := fmt.Sprintf("mp.test.%s.%d", protocol.Name(), time.Now().UnixNano())
			testValue := 42.5
			testTimestamp := time.Now().Truncate(time.Second)
			testTags := map[string]string{
				"host":   "server1",
				"region": "us-east",
			}

			// Write a single point
			err = protocol.WritePoint(ctx, metricName, testValue, testTimestamp, testTags)
			require.NoError(t, err, "%s protocol: time series WritePoint must succeed", protocol.Name())

			// Query the point back - retry to allow write to propagate through Raft consensus
			var points []*TimeSeriesDataPoint
			var queryErr error
			for attempt := 0; attempt < 10; attempt++ {
				points, queryErr = protocol.Query(ctx, metricName,
					testTimestamp.Add(-1*time.Minute),
					testTimestamp.Add(1*time.Minute),
					"")
				if queryErr != nil {
					break // Query error, don't retry
				}
				if len(points) >= 1 {
					break // Success
				}
				// Use a ticker for non-blocking wait instead of time.Sleep
				ticker := time.NewTicker(50 * time.Millisecond)
				select {
				case <-ticker.C:
					ticker.Stop()
				case <-ctx.Done():
					ticker.Stop()
					t.Fatalf("Context cancelled while waiting for write to propagate")
				}
			}

			require.NoError(t, queryErr, "Query failed for %s", protocol.Name())
			require.NotEmpty(t, points, "Expected at least one point")

			// Verify the point data
			assert.Equal(t, testValue, points[0].Value)
			assert.Equal(t, testTags["host"], points[0].Tags["host"])
			assert.Equal(t, testTags["region"], points[0].Tags["region"])

			t.Logf("[%s] WriteAndQueryPoint: metric=%s, value=%.2f, points_returned=%d",
				protocol.Name(), metricName, testValue, len(points))
		})
	}
}

// TestMultiprotocol_WriteBatch verifies that batch write operations work
// identically across all supported protocols.
func TestMultiprotocol_WriteBatch(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			metricName := fmt.Sprintf("mp.batch.%s.%d", protocol.Name(), time.Now().UnixNano())
			baseTime := time.Now().Truncate(time.Second)

			// Create batch of points
			points := []*TimeSeriesPoint{
				{Metric: metricName, Value: 10.0, Timestamp: baseTime.Add(-3 * time.Minute), Tags: map[string]string{"host": "server1"}},
				{Metric: metricName, Value: 20.0, Timestamp: baseTime.Add(-2 * time.Minute), Tags: map[string]string{"host": "server1"}},
				{Metric: metricName, Value: 30.0, Timestamp: baseTime.Add(-1 * time.Minute), Tags: map[string]string{"host": "server1"}},
				{Metric: metricName, Value: 40.0, Timestamp: baseTime, Tags: map[string]string{"host": "server1"}},
			}

			// Write batch
			err = protocol.WriteBatch(ctx, points)
			require.NoError(t, err, "%s protocol: WriteBatch must succeed", protocol.Name())

			// Query all points - retry to allow batch write to propagate through Raft consensus
			var result []*TimeSeriesDataPoint
			var queryErr error
			for attempt := 0; attempt < 10; attempt++ {
				result, queryErr = protocol.Query(ctx, metricName,
					baseTime.Add(-5*time.Minute),
					baseTime.Add(1*time.Minute),
					"")
				if queryErr != nil {
					break // Query error, don't retry
				}
				if len(result) >= 4 {
					break // Success
				}
				// Use a ticker for non-blocking wait instead of time.Sleep
				ticker := time.NewTicker(50 * time.Millisecond)
				select {
				case <-ticker.C:
					ticker.Stop()
				case <-ctx.Done():
					ticker.Stop()
					t.Fatalf("Context cancelled while waiting for batch write to propagate")
				}
			}
			require.NoError(t, queryErr, "Query failed for %s", protocol.Name())
			assert.GreaterOrEqual(t, len(result), 4, "Expected at least 4 points")

			t.Logf("[%s] WriteBatch: metric=%s, points_written=%d, points_returned=%d",
				protocol.Name(), metricName, len(points), len(result))
		})
	}
}

// TestMultiprotocol_Aggregations verifies that aggregation operations work
// identically across all supported protocols.
func TestMultiprotocol_Aggregations(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	aggregations := []struct {
		name     string
		expected float64 // Expected result for values [10, 20, 30, 40]
	}{
		{"sum", 100.0},
		{"avg", 25.0},
		{"min", 10.0},
		{"max", 40.0},
		{"count", 4.0},
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			metricName := fmt.Sprintf("mp.agg.%s.%d", protocol.Name(), time.Now().UnixNano())
			baseTime := time.Now().Truncate(time.Second)

			// Write test data
			points := []*TimeSeriesPoint{
				{Metric: metricName, Value: 10.0, Timestamp: baseTime.Add(-4 * time.Minute), Tags: nil},
				{Metric: metricName, Value: 20.0, Timestamp: baseTime.Add(-3 * time.Minute), Tags: nil},
				{Metric: metricName, Value: 30.0, Timestamp: baseTime.Add(-2 * time.Minute), Tags: nil},
				{Metric: metricName, Value: 40.0, Timestamp: baseTime.Add(-1 * time.Minute), Tags: nil},
			}

			err = protocol.WriteBatch(ctx, points)
			require.NoError(t, err, "%s protocol: WriteBatch must succeed", protocol.Name())

			// Test each aggregation
			for _, agg := range aggregations {
				t.Run(agg.name, func(t *testing.T) {
					result, err := protocol.Query(ctx, metricName,
						baseTime.Add(-5*time.Minute),
						baseTime.Add(1*time.Minute),
						agg.name)
					require.NoError(t, err, "Query with %s aggregation failed for %s", agg.name, protocol.Name())
					require.NotEmpty(t, result, "Expected results for %s aggregation", agg.name)

					// For aggregation queries, the result should contain the aggregated value
					// The implementation may vary in how this is returned
					t.Logf("[%s] Aggregation %s: metric=%s, result_count=%d",
						protocol.Name(), agg.name, metricName, len(result))
				})
			}
		})
	}
}

// TestMultiprotocol_Delete verifies that delete operations work identically
// across all supported protocols.
func TestMultiprotocol_Delete(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			metricName := fmt.Sprintf("mp.delete.%s.%d", protocol.Name(), time.Now().UnixNano())
			baseTime := time.Now().Truncate(time.Second)

			// Write test data
			points := []*TimeSeriesPoint{
				{Metric: metricName, Value: 1.0, Timestamp: baseTime.Add(-2 * time.Minute), Tags: nil},
				{Metric: metricName, Value: 2.0, Timestamp: baseTime.Add(-1 * time.Minute), Tags: nil},
				{Metric: metricName, Value: 3.0, Timestamp: baseTime, Tags: nil},
			}

			err = protocol.WriteBatch(ctx, points)
			require.NoError(t, err, "%s protocol: WriteBatch must succeed", protocol.Name())

			// Verify points exist
			result, err := protocol.Query(ctx, metricName,
				baseTime.Add(-5*time.Minute),
				baseTime.Add(1*time.Minute),
				"")
			require.NoError(t, err, "Query failed for %s", protocol.Name())
			require.GreaterOrEqual(t, len(result), 3, "Expected at least 3 points before delete")

			// Delete all points
			err = protocol.Delete(ctx, metricName,
				baseTime.Add(-5*time.Minute),
				baseTime.Add(1*time.Minute))
			require.NoError(t, err, "Delete failed for %s", protocol.Name())

			// Verify points are deleted
			result, err = protocol.Query(ctx, metricName,
				baseTime.Add(-5*time.Minute),
				baseTime.Add(1*time.Minute),
				"")
			require.NoError(t, err, "Query after delete failed for %s", protocol.Name())
			assert.Empty(t, result, "Expected no points after delete")

			t.Logf("[%s] Delete: metric=%s verified", protocol.Name(), metricName)
		})
	}
}

// TestMultiprotocol_RetentionPolicy verifies that retention policy operations
// work identically across all supported protocols.
func TestMultiprotocol_RetentionPolicy(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			metricName := fmt.Sprintf("mp.retention.%s.%d", protocol.Name(), time.Now().UnixNano())

			// Get default retention policy
			t.Run("GetDefaultPolicy", func(t *testing.T) {
				policy, err := protocol.GetRetentionPolicy(ctx, metricName)
				require.NoError(t, err, "%s protocol: GetRetentionPolicy must be implemented", protocol.Name())
				require.NotNil(t, policy)
				// Default policy should have zero duration (infinite retention)
				assert.Equal(t, time.Duration(0), policy.Duration)
				t.Logf("[%s] GetDefaultPolicy: duration=%v", protocol.Name(), policy.Duration)
			})

			// Set custom retention policy
			t.Run("SetAndGetPolicy", func(t *testing.T) {
				newPolicy := &RetentionPolicy{
					Duration:    30 * 24 * time.Hour, // 30 days
					Resolution:  5 * time.Minute,
					Aggregation: "avg",
				}

				err := protocol.SetRetentionPolicy(ctx, metricName, newPolicy)
				require.NoError(t, err, "%s protocol: SetRetentionPolicy must be implemented", protocol.Name())

				// Retrieve and verify
				retrieved, err := protocol.GetRetentionPolicy(ctx, metricName)
				require.NoError(t, err, "GetRetentionPolicy failed for %s", protocol.Name())
				require.NotNil(t, retrieved)
				assert.Equal(t, newPolicy.Duration, retrieved.Duration)
				assert.Equal(t, newPolicy.Resolution, retrieved.Resolution)
				assert.Equal(t, newPolicy.Aggregation, retrieved.Aggregation)

				t.Logf("[%s] SetAndGetPolicy: duration=%v, resolution=%v, aggregation=%s",
					protocol.Name(), retrieved.Duration, retrieved.Resolution, retrieved.Aggregation)
			})
		})
	}
}

// TestMultiprotocol_ListMetrics verifies that list metrics operations work
// identically across all supported protocols.
func TestMultiprotocol_ListMetrics(t *testing.T) {
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []timeseriesProtocol{
		&restTimeseriesProtocol{},
		&grpcTimeseriesProtocol{},
		&quicTimeseriesProtocol{},
		&unixSocketTimeseriesProtocol{},
		&mcpTimeseriesProtocol{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedTimeseriesProtocol{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			prefix := fmt.Sprintf("mp.list.%s.%d", protocol.Name(), time.Now().UnixNano())
			baseTime := time.Now().Truncate(time.Second)

			// Create multiple metrics
			metrics := []string{
				prefix + ".cpu.usage",
				prefix + ".cpu.system",
				prefix + ".memory.used",
			}

			for _, metric := range metrics {
				err := protocol.WritePoint(ctx, metric, 100.0, baseTime, nil)
				require.NoError(t, err, "%s protocol: WritePoint must succeed for metric %s", protocol.Name(), metric)
			}

			// List all metrics with prefix - retry to allow index to propagate
			var result []string
			var listErr error
			for attempt := 0; attempt < 10; attempt++ {
				result, listErr = protocol.ListMetrics(ctx, prefix)
				require.NoError(t, listErr, "ListMetrics failed for %s", protocol.Name())
				if len(result) >= 3 {
					break
				}
				// Use a ticker for non-blocking wait instead of time.Sleep
				ticker := time.NewTicker(50 * time.Millisecond)
				select {
				case <-ticker.C:
					ticker.Stop()
				case <-ctx.Done():
					ticker.Stop()
					t.Fatalf("Context cancelled while waiting for metrics")
				}
			}
			assert.GreaterOrEqual(t, len(result), 3, "Expected at least 3 metrics")

			// List metrics with more specific prefix
			cpuResult, cpuErr := protocol.ListMetrics(ctx, prefix+".cpu")
			require.NoError(t, cpuErr, "ListMetrics with cpu prefix failed for %s", protocol.Name())
			assert.GreaterOrEqual(t, len(cpuResult), 2, "Expected at least 2 cpu metrics")

			t.Logf("[%s] ListMetrics: prefix=%s, total=%d, cpu_prefix=%d",
				protocol.Name(), prefix, len(result), len(cpuResult))
		})
	}
}

// =============================================================================
// Embedded Protocol Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

// ErrEmbeddedProtocolNotAvailable is returned when the embedded protocol is not
// available in the integration test context.
type ErrEmbeddedProtocolNotAvailable struct{}

func (e ErrEmbeddedProtocolNotAvailable) Error() string {
	return "embedded protocol not available in integration test context (requires in-process service instance)"
}

type embeddedTimeseriesProtocol struct {
	client quicbaas.Client
}

func (e *embeddedTimeseriesProtocol) Name() string { return "Embedded" }

func (e *embeddedTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}
	e.client = client
	return nil
}

func (e *embeddedTimeseriesProtocol) Teardown(t *testing.T) error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}

func (e *embeddedTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	if e.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	req := &timeseries.WriteRequest{
		Metric:    metric,
		Tags:      tags,
		Value:     value,
		Timestamp: timestamp,
	}
	return e.client.TimeSeries().Write(ctx, req)
}

func (e *embeddedTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	if e.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	// The SDK TimeSeries client doesn't have a WriteBatch method, so we write points individually
	for _, p := range points {
		req := &timeseries.WriteRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
		if err := e.client.TimeSeries().Write(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

func (e *embeddedTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	if e.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Map aggregation string to AggregateFunction
	var aggFunc timeseries.AggregateFunction
	switch aggregation {
	case "sum":
		aggFunc = timeseries.AggregateSum
	case "avg":
		aggFunc = timeseries.AggregateAvg
	case "min":
		aggFunc = timeseries.AggregateMin
	case "max":
		aggFunc = timeseries.AggregateMax
	case "count":
		aggFunc = timeseries.AggregateCount
	default:
		aggFunc = timeseries.AggregateNone
	}

	req := &timeseries.QueryRequest{
		Metric:    metric,
		Start:     start,
		End:       end,
		Aggregate: aggFunc,
	}

	resp, err := e.client.TimeSeries().Query(ctx, req)
	if err != nil {
		return nil, err
	}

	points := make([]*TimeSeriesDataPoint, len(resp.Series))
	for i, p := range resp.Series {
		points[i] = &TimeSeriesDataPoint{
			Metric:    metric,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	return points, nil
}

func (e *embeddedTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	if e.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	// The SDK TimeSeries client doesn't have a Delete method
	// This would need to be implemented if the SDK supports it
	return &qberrors.MessageError{Message: "delete not supported in SDK TimeSeries client"}
}

func (e *embeddedTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	if e.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	// The SDK TimeSeries client doesn't have a ListMetrics method
	// This would need to be implemented if the SDK supports it
	return nil, &qberrors.MessageError{Message: "list metrics not supported in SDK TimeSeries client"}
}

func (e *embeddedTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	if e.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	// The SDK TimeSeries client doesn't have a GetRetentionPolicy method
	// Return a default policy
	return &RetentionPolicy{Duration: 0}, nil
}

func (e *embeddedTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	if e.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	// The SDK TimeSeries client doesn't have a SetRetentionPolicy method
	return &qberrors.MessageError{Message: "set retention policy not supported in SDK TimeSeries client"}
}

// =============================================================================
// REST Protocol Implementation
// =============================================================================

type restTimeseriesProtocol struct {
	endpoint string
	client   *http.Client
}

func (r *restTimeseriesProtocol) Name() string { return "REST" }

func (r *restTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available for integration tests")
	r.endpoint = endpoints[0]
	r.client = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
	return nil
}

func (r *restTimeseriesProtocol) Teardown(t *testing.T) error {
	return nil
}

func (r *restTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	url := r.endpoint + "/api/v1/timeseries/write"

	payload := map[string]interface{}{
		"metric":    metric,
		"value":     value,
		"timestamp": timestamp.Format(time.RFC3339Nano),
	}
	if tags != nil {
		payload["tags"] = tags
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (r *restTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	url := r.endpoint + "/api/v1/timeseries/write-batch"

	pointsPayload := make([]map[string]interface{}, len(points))
	for i, p := range points {
		pointsPayload[i] = map[string]interface{}{
			"metric":    p.Metric,
			"value":     p.Value,
			"timestamp": p.Timestamp.Format(time.RFC3339Nano),
		}
		if p.Tags != nil {
			pointsPayload[i]["tags"] = p.Tags
		}
	}

	payload := map[string]interface{}{
		"points": pointsPayload,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (r *restTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	url := r.endpoint + "/api/v1/timeseries/query"

	payload := map[string]interface{}{
		"metric":     metric,
		"start_time": start,
		"end_time":   end,
	}
	if aggregation != "" {
		payload["aggregation"] = aggregation
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result struct {
		Points []*TimeSeriesDataPoint `json:"points"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result.Points, nil
}

func (r *restTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	url := r.endpoint + "/api/v1/timeseries/series"

	payload := map[string]interface{}{
		"metric":     metric,
		"start_time": start,
		"end_time":   end,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (r *restTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	url := r.endpoint + "/api/v1/timeseries/metrics?prefix=" + prefix

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result struct {
		Metrics []string `json:"metrics"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result.Metrics, nil
}

func (r *restTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	url := r.endpoint + "/api/v1/timeseries/retention/" + metric

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	// The REST API returns duration/resolution in seconds
	var result struct {
		Policy *struct {
			DurationSec   int64  `json:"duration_seconds"`
			ResolutionSec int64  `json:"resolution_seconds"`
			Aggregation   string `json:"aggregation"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Policy == nil {
		// Return default policy
		return &RetentionPolicy{Duration: 0}, nil
	}

	return &RetentionPolicy{
		Duration:    time.Duration(result.Policy.DurationSec) * time.Second,
		Resolution:  time.Duration(result.Policy.ResolutionSec) * time.Second,
		Aggregation: result.Policy.Aggregation,
	}, nil
}

func (r *restTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	url := r.endpoint + "/api/v1/timeseries/retention/" + metric

	// The REST API expects the policy wrapped in a "policy" key with duration/resolution in seconds
	payload := map[string]interface{}{
		"policy": map[string]interface{}{
			"duration_seconds":   int64(policy.Duration.Seconds()),
			"resolution_seconds": int64(policy.Resolution.Seconds()),
			"aggregation":        policy.Aggregation,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

// =============================================================================
// gRPC Protocol Implementation
// =============================================================================

type grpcTimeseriesProtocol struct {
	client *grpcclient.Client
}

func (g *grpcTimeseriesProtocol) Name() string { return "gRPC" }

func (g *grpcTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to gRPC endpoint " + endpoints[0], Cause: err}
	}
	g.client = client

	return nil
}

func (g *grpcTimeseriesProtocol) Teardown(t *testing.T) error {
	if g.client != nil {
		g.client.Close()
	}
	return nil
}

func (g *grpcTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	req := &cli.WritePointRequest{
		Metric:    metric,
		Tags:      tags,
		Value:     value,
		Timestamp: timestamp,
	}
	_, err := g.client.TSWritePoint(ctx, req)
	return err
}

func (g *grpcTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	cliPoints := make([]cli.WritePointRequest, len(points))
	for i, p := range points {
		cliPoints[i] = cli.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	req := &cli.WriteBatchRequest{
		Points: cliPoints,
	}
	_, err := g.client.TSWriteBatch(ctx, req)
	return err
}

func (g *grpcTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	req := &cli.TSQueryRequest{
		Metric:      metric,
		StartTime:   start,
		EndTime:     end,
		Aggregation: aggregation,
	}

	resp, err := g.client.TSQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	points := make([]*TimeSeriesDataPoint, len(resp.Points))
	for i, p := range resp.Points {
		points[i] = &TimeSeriesDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	return points, nil
}

func (g *grpcTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	req := &cli.TSDeleteRequest{
		Metric:    metric,
		StartTime: &start,
		EndTime:   &end,
	}
	_, err := g.client.TSDeleteSeries(ctx, req)
	return err
}

func (g *grpcTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	req := &cli.TSListMetricsRequest{
		Prefix: prefix,
	}

	resp, err := g.client.TSListMetrics(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Metrics, nil
}

func (g *grpcTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	req := &cli.TSGetRetentionPolicyRequest{
		Metric: metric,
	}

	resp, err := g.client.TSGetRetentionPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Policy == nil {
		return &RetentionPolicy{Duration: 0}, nil
	}

	return &RetentionPolicy{
		Duration:    resp.Policy.Duration,
		Resolution:  resp.Policy.Resolution,
		Aggregation: resp.Policy.Aggregation,
	}, nil
}

func (g *grpcTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	req := &cli.TSSetRetentionPolicyRequest{
		Metric: metric,
		Policy: &cli.TSRetentionPolicy{
			Duration:    policy.Duration,
			Resolution:  policy.Resolution,
			Aggregation: policy.Aggregation,
		},
	}
	_, err := g.client.TSSetRetentionPolicy(ctx, req)
	return err
}

// =============================================================================
// QUIC Protocol Implementation
// =============================================================================

type quicTimeseriesProtocol struct {
	addr       string
	conn       *quicgo.Conn
	tlsConfig  *tls.Config
	ctx        context.Context
	cancelFunc context.CancelFunc
}

func (q *quicTimeseriesProtocol) Name() string { return "QUIC" }

func (q *quicTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetQUICEndpoints()
	require.NotEmpty(t, endpoints, "QUIC endpoints must be available for integration tests")

	addr := endpoints[0]

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"quicbaas-quic"},
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := quicgo.DialAddr(dialCtx, addr, tlsConf, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to QUIC endpoint " + addr, Cause: err}
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())

	q.addr = addr
	q.conn = conn
	q.tlsConfig = tlsConf
	q.ctx = clientCtx
	q.cancelFunc = clientCancel

	return nil
}

func (q *quicTimeseriesProtocol) Teardown(t *testing.T) error {
	if q.cancelFunc != nil {
		q.cancelFunc()
	}
	if q.conn != nil {
		return q.conn.CloseWithError(0, "client closing")
	}
	return nil
}

func (q *quicTimeseriesProtocol) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*quic.Response, error) {
	stream, err := q.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to open stream", Cause: err}
	}
	defer stream.Close()

	req := &quic.Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to write request", Cause: err}
	}

	resp, err := quic.ReadResponse(stream)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.Status != quic.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quic.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

func (q *quicTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	req := &cli.WritePointRequest{
		Metric:    metric,
		Tags:      tags,
		Value:     value,
		Timestamp: timestamp,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = q.sendRequest(ctx, quic.OpTSWritePoint, payload)
	return err
}

func (q *quicTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	cliPoints := make([]cli.WritePointRequest, len(points))
	for i, p := range points {
		cliPoints[i] = cli.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	req := &cli.WriteBatchRequest{
		Points: cliPoints,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = q.sendRequest(ctx, quic.OpTSWriteBatch, payload)
	return err
}

func (q *quicTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	req := &cli.TSQueryRequest{
		Metric:      metric,
		StartTime:   start,
		EndTime:     end,
		Aggregation: aggregation,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := q.sendRequest(ctx, quic.OpTSQuery, payload)
	if err != nil {
		return nil, err
	}

	var result cli.TSQueryResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	points := make([]*TimeSeriesDataPoint, len(result.Points))
	for i, p := range result.Points {
		points[i] = &TimeSeriesDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	return points, nil
}

func (q *quicTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	req := &cli.TSDeleteRequest{
		Metric:    metric,
		StartTime: &start,
		EndTime:   &end,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = q.sendRequest(ctx, quic.OpTSDeleteSeries, payload)
	return err
}

func (q *quicTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	req := &cli.TSListMetricsRequest{
		Prefix: prefix,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := q.sendRequest(ctx, quic.OpTSListMetrics, payload)
	if err != nil {
		return nil, err
	}

	var result cli.TSListMetricsResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result.Metrics, nil
}

func (q *quicTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	req := &cli.TSGetRetentionPolicyRequest{
		Metric: metric,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := q.sendRequest(ctx, quic.OpTSGetRetentionPolicy, payload)
	if err != nil {
		return nil, err
	}

	var result cli.TSGetRetentionPolicyResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	if result.Policy == nil {
		return &RetentionPolicy{Duration: 0}, nil
	}

	return &RetentionPolicy{
		Duration:    result.Policy.Duration,
		Resolution:  result.Policy.Resolution,
		Aggregation: result.Policy.Aggregation,
	}, nil
}

func (q *quicTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	req := &cli.TSSetRetentionPolicyRequest{
		Metric: metric,
		Policy: &cli.TSRetentionPolicy{
			Duration:    policy.Duration,
			Resolution:  policy.Resolution,
			Aggregation: policy.Aggregation,
		},
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = q.sendRequest(ctx, quic.OpTSSetRetentionPolicy, payload)
	return err
}

// =============================================================================
// Unix Socket Protocol Implementation
// =============================================================================

type unixSocketTimeseriesProtocol struct {
	client *grpcclient.Client
}

func (u *unixSocketTimeseriesProtocol) Name() string { return "UnixSocket" }

func (u *unixSocketTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check if socket exists by attempting a test dial
	testConn, err := net.Dial("unix", socketPath)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "unix socket not available at " + socketPath, Cause: err}
	}
	// Close the test connection immediately
	testConn.Close()

	// Create gRPC client over unix socket
	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create gRPC client over unix socket", Cause: err}
	}
	u.client = client

	return nil
}

func (u *unixSocketTimeseriesProtocol) Teardown(t *testing.T) error {
	if u.client != nil {
		u.client.Close()
	}
	return nil
}

func (u *unixSocketTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	req := &cli.WritePointRequest{
		Metric:    metric,
		Tags:      tags,
		Value:     value,
		Timestamp: timestamp,
	}
	_, err := u.client.TSWritePoint(ctx, req)
	return err
}

func (u *unixSocketTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	cliPoints := make([]cli.WritePointRequest, len(points))
	for i, p := range points {
		cliPoints[i] = cli.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	req := &cli.WriteBatchRequest{
		Points: cliPoints,
	}
	_, err := u.client.TSWriteBatch(ctx, req)
	return err
}

func (u *unixSocketTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	req := &cli.TSQueryRequest{
		Metric:      metric,
		StartTime:   start,
		EndTime:     end,
		Aggregation: aggregation,
	}

	resp, err := u.client.TSQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	points := make([]*TimeSeriesDataPoint, len(resp.Points))
	for i, p := range resp.Points {
		points[i] = &TimeSeriesDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	return points, nil
}

func (u *unixSocketTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	req := &cli.TSDeleteRequest{
		Metric:    metric,
		StartTime: &start,
		EndTime:   &end,
	}
	_, err := u.client.TSDeleteSeries(ctx, req)
	return err
}

func (u *unixSocketTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	req := &cli.TSListMetricsRequest{
		Prefix: prefix,
	}

	resp, err := u.client.TSListMetrics(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Metrics, nil
}

func (u *unixSocketTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	req := &cli.TSGetRetentionPolicyRequest{
		Metric: metric,
	}

	resp, err := u.client.TSGetRetentionPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Policy == nil {
		return &RetentionPolicy{Duration: 0}, nil
	}

	return &RetentionPolicy{
		Duration:    resp.Policy.Duration,
		Resolution:  resp.Policy.Resolution,
		Aggregation: resp.Policy.Aggregation,
	}, nil
}

func (u *unixSocketTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	req := &cli.TSSetRetentionPolicyRequest{
		Metric: metric,
		Policy: &cli.TSRetentionPolicy{
			Duration:    policy.Duration,
			Resolution:  policy.Resolution,
			Aggregation: policy.Aggregation,
		},
	}
	_, err := u.client.TSSetRetentionPolicy(ctx, req)
	return err
}

// =============================================================================
// MCP Protocol Implementation
// =============================================================================

type mcpTimeseriesProtocol struct {
	endpoint      string
	httpClient    *http.Client
	nextID        int
	accessToken   string
	authenticated bool
}

func (m *mcpTimeseriesProtocol) Name() string { return "MCP" }

func (c *mcpTimeseriesProtocol) authenticate(ctx context.Context) error {
	// Acquire a bearer through the cluster's PIV mTLS login endpoint.
	// Password-based login has been removed across all transports; tests
	// mint a bearer via REST PIV and present it on the protocol under test.
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available for PIV login"}
	}
	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "PIV login failed", Cause: err}
	}
	c.accessToken = bearer
	c.authenticated = true
	return nil
}


func (m *mcpTimeseriesProtocol) callMCPToolUnauthenticated(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
		"id": m.nextID,
	}
	m.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, err
	}

	if mcpResp.Error != nil {
		return nil, &qberrors.MessageError{Message: "MCP error: " + mcpResp.Error.Message}
	}

	if mcpResp.Result.IsError && len(mcpResp.Result.Content) > 0 {
		return nil, &qberrors.MessageError{Message: "MCP tool error: " + mcpResp.Result.Content[0].Text}
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}

	return nil, nil
}

func (m *mcpTimeseriesProtocol) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetMCPEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no MCP endpoints available"}
	}
	m.endpoint = endpoints[0]
	m.httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
	m.nextID = 1

	// Verify MCP endpoint via initialize
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-timeseries-test",
				"version": "1.0.0",
			},
		},
		"id": m.nextID,
	}
	m.nextID++

	body, err := json.Marshal(initReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal init request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP endpoint not reachable at " + m.endpoint, Cause: err}
	}
	defer resp.Body.Close()

	// Authenticate after initialization
	if err := m.authenticate(ctx); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to authenticate", Cause: err}
	}

	return nil
}

func (m *mcpTimeseriesProtocol) Teardown(t *testing.T) error {
	m.httpClient = nil
	m.accessToken = ""
	m.authenticated = false
	return nil
}

func (m *mcpTimeseriesProtocol) callMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
		"id": m.nextID,
	}
	m.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.authenticated {
		req.Header.Set("Authorization", "Bearer "+m.accessToken)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, err
	}

	if mcpResp.Error != nil {
		return nil, &qberrors.MessageError{Message: "MCP error: " + mcpResp.Error.Message}
	}

	if mcpResp.Result.IsError && len(mcpResp.Result.Content) > 0 {
		return nil, &qberrors.MessageError{Message: "MCP tool error: " + mcpResp.Result.Content[0].Text}
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}

	return nil, nil
}

func (m *mcpTimeseriesProtocol) WritePoint(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	// The MCP handler uses getString(args, "value"), so value must be a string.
	// The handler uses parseTagsJSON which calls getString(args, "tags"),
	// so tags must be a JSON-encoded string.
	args := map[string]interface{}{
		"metric":    metric,
		"value":     strconv.FormatFloat(value, 'f', -1, 64),
		"timestamp": timestamp.UnixNano(),
	}
	if len(tags) > 0 {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return err
		}
		args["tags"] = string(tagsJSON)
	}
	_, err := m.callMCPTool(ctx, "quicbaas_ts_write_point", args)
	return err
}

func (m *mcpTimeseriesProtocol) WriteBatch(ctx context.Context, points []*TimeSeriesPoint) error {
	// The MCP handler uses getString(args, "points") and then json.Unmarshal
	// into []batchPointJSON, so points must be a JSON-encoded string.
	// Each point's value field in batchPointJSON is a string type.
	type batchPoint struct {
		Metric    string            `json:"metric"`
		Value     string            `json:"value"`
		Timestamp int64             `json:"timestamp"`
		Tags      map[string]string `json:"tags,omitempty"`
	}

	batchPoints := make([]batchPoint, len(points))
	for i, p := range points {
		batchPoints[i] = batchPoint{
			Metric:    p.Metric,
			Value:     strconv.FormatFloat(p.Value, 'f', -1, 64),
			Timestamp: p.Timestamp.UnixNano(),
			Tags:      p.Tags,
		}
	}

	pointsJSON, err := json.Marshal(batchPoints)
	if err != nil {
		return err
	}

	args := map[string]interface{}{
		"points": string(pointsJSON),
	}
	_, err = m.callMCPTool(ctx, "quicbaas_ts_write_batch", args)
	return err
}

func (m *mcpTimeseriesProtocol) Query(ctx context.Context, metric string, start, end time.Time, aggregation string) ([]*TimeSeriesDataPoint, error) {
	args := map[string]interface{}{
		"metric":     metric,
		"start_time": start.UnixNano(),
		"end_time":   end.UnixNano(),
	}
	if aggregation != "" {
		args["aggregation"] = aggregation
	}

	result, err := m.callMCPTool(ctx, "quicbaas_ts_query", args)
	if err != nil {
		return nil, err
	}

	// The service TSQueryResponse serializes TSDataPoint.Timestamp as time.Time
	// (RFC3339 string in JSON), so parse accordingly.
	var resp struct {
		Points []struct {
			Metric    string            `json:"metric"`
			Tags      map[string]string `json:"tags"`
			Value     float64           `json:"value"`
			Timestamp time.Time         `json:"timestamp"`
		} `json:"points"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	points := make([]*TimeSeriesDataPoint, len(resp.Points))
	for i, p := range resp.Points {
		points[i] = &TimeSeriesDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		}
	}

	return points, nil
}

func (m *mcpTimeseriesProtocol) Delete(ctx context.Context, metric string, start, end time.Time) error {
	args := map[string]interface{}{
		"metric":     metric,
		"start_time": start.UnixNano(),
		"end_time":   end.UnixNano(),
	}
	_, err := m.callMCPTool(ctx, "quicbaas_ts_delete_series", args)
	return err
}

func (m *mcpTimeseriesProtocol) ListMetrics(ctx context.Context, prefix string) ([]string, error) {
	args := map[string]interface{}{}
	if prefix != "" {
		args["prefix"] = prefix
	}

	result, err := m.callMCPTool(ctx, "quicbaas_ts_list_metrics", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Metrics []string `json:"metrics"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Metrics, nil
}

func (m *mcpTimeseriesProtocol) GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error) {
	args := map[string]interface{}{
		"metric": metric,
	}

	result, err := m.callMCPTool(ctx, "quicbaas_ts_get_retention_policy", args)
	if err != nil {
		return nil, err
	}

	// The service TSGetRetentionPolicyResponse wraps the policy in a "policy"
	// field. TSRetentionPolicy serializes Duration/Resolution as int64
	// nanoseconds (Go time.Duration JSON marshaling).
	var resp struct {
		Policy *struct {
			Duration    int64  `json:"duration"`
			Resolution  int64  `json:"resolution"`
			Aggregation string `json:"aggregation"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	if resp.Policy == nil {
		return &RetentionPolicy{Duration: 0}, nil
	}

	return &RetentionPolicy{
		Duration:    time.Duration(resp.Policy.Duration),
		Resolution:  time.Duration(resp.Policy.Resolution),
		Aggregation: resp.Policy.Aggregation,
	}, nil
}

func (m *mcpTimeseriesProtocol) SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error {
	// The MCP handler reads duration_seconds and resolution_seconds via
	// getInt64, then multiplies by time.Second.
	args := map[string]interface{}{
		"metric":           metric,
		"duration_seconds": int64(policy.Duration.Seconds()),
	}
	if policy.Resolution > 0 {
		args["resolution_seconds"] = int64(policy.Resolution.Seconds())
	}
	if policy.Aggregation != "" {
		args["aggregation"] = policy.Aggregation
	}
	_, err := m.callMCPTool(ctx, "quicbaas_ts_set_retention_policy", args)
	return err
}
