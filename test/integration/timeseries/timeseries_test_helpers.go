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

//go:build integration

package timeseries

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// requireTimeseriesAvailableREST probes the timeseries service via the REST API
// by issuing a lightweight list-metrics request. Fatals if the service is not available.
func requireTimeseriesAvailableREST(t *testing.T) {
	t.Helper()

	endpoints := helpers.GetRESTEndpoints()
	endpoint := endpoints[0]
	probeClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/timeseries/metrics", nil)
	if err != nil {
		t.Fatalf("Timeseries probe: failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := probeClient.Do(req)
	if err != nil {
		t.Fatalf("Timeseries service must be available: probe request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Timeseries probe: failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Timeseries service must be available: probe returned status %d: %s", resp.StatusCode, string(respBody))
	}
}

// inMemoryTimeSeriesStorage is an in-memory implementation of interfaces.TimeSeriesStorage
// used for integration testing of the time series test infrastructure.
type inMemoryTimeSeriesStorage struct {
	mu        sync.RWMutex
	points    []*interfaces.DataPoint
	retention map[string]*interfaces.RetentionPolicy
}

var _ interfaces.TimeSeriesStorage = (*inMemoryTimeSeriesStorage)(nil)

// newTimeSeriesTestStorage creates an in-memory TimeSeriesStorage for integration testing.
// Returns the storage, a cleanup function, and any initialization error.
func newTimeSeriesTestStorage(_ *testing.T) (interfaces.TimeSeriesStorage, func(), error) {
	storage := &inMemoryTimeSeriesStorage{
		points:    make([]*interfaces.DataPoint, 0),
		retention: make(map[string]*interfaces.RetentionPolicy),
	}
	cleanup := func() {
		storage.mu.Lock()
		defer storage.mu.Unlock()
		storage.points = nil
		storage.retention = nil
	}
	return storage, cleanup, nil
}

// WritePoint writes a single data point to the in-memory store.
func (s *inMemoryTimeSeriesStorage) WritePoint(_ context.Context, point *interfaces.DataPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := *point
	if cp.Tags == nil {
		cp.Tags = make(map[string]string)
	}
	s.points = append(s.points, &cp)
	return nil
}

// WriteBatch writes multiple data points atomically.
func (s *inMemoryTimeSeriesStorage) WriteBatch(points []interfaces.DataPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range points {
		cp := points[i]
		if cp.Tags == nil {
			cp.Tags = make(map[string]string)
		}
		s.points = append(s.points, &cp)
	}
	return nil
}

// QueryRich performs a time range query with optional filtering and aggregation.
func (s *inMemoryTimeSeriesStorage) QueryRich(_ context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*interfaces.DataPoint
	for _, p := range s.points {
		if p.Metric != req.Metric {
			continue
		}
		if !req.StartTime.IsZero() && p.Timestamp.Before(req.StartTime) {
			continue
		}
		if !req.EndTime.IsZero() && p.Timestamp.After(req.EndTime) {
			continue
		}
		if len(req.Tags) > 0 && !tagsMatch(p.Tags, req.Tags) {
			continue
		}
		cp := *p
		matched = append(matched, &cp)
	}

	// Sort by timestamp
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Timestamp.Before(matched[j].Timestamp)
	})

	result := &interfaces.QueryResult{
		Metric: req.Metric,
		Points: matched,
	}

	if req.Aggregation != interfaces.AggNone && len(matched) > 0 {
		aggVal := computeAggregation(matched, req.Aggregation)
		result.AggregatedValue = &aggVal
	}

	return result, nil
}

// DeleteSeries deletes data points matching the request criteria.
func (s *inMemoryTimeSeriesStorage) DeleteSeries(_ context.Context, req *interfaces.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]*interfaces.DataPoint, 0, len(s.points))
	for _, p := range s.points {
		if p.Metric == req.Metric {
			if len(req.Tags) > 0 && !tagsMatch(p.Tags, req.Tags) {
				filtered = append(filtered, p)
				continue
			}
			if !req.StartTime.IsZero() && p.Timestamp.Before(req.StartTime) {
				filtered = append(filtered, p)
				continue
			}
			if !req.EndTime.IsZero() && p.Timestamp.After(req.EndTime) {
				filtered = append(filtered, p)
				continue
			}
			// Point matches delete criteria, skip it
			continue
		}
		filtered = append(filtered, p)
	}
	s.points = filtered
	return nil
}

// GetRetentionPolicy retrieves the retention policy for a metric.
func (s *inMemoryTimeSeriesStorage) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if policy, ok := s.retention[metric]; ok {
		return policy, nil
	}
	// Return default policy with infinite retention
	return &interfaces.RetentionPolicy{
		Duration: time.Duration(0),
	}, nil
}

// SetRetentionPolicy sets the retention policy for a metric.
func (s *inMemoryTimeSeriesStorage) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.retention[metric] = policy
	return nil
}

// ListMetrics returns metric names matching the optional prefix.
func (s *inMemoryTimeSeriesStorage) ListMetrics(prefix string, limit int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	var metrics []string

	for _, p := range s.points {
		if _, ok := seen[p.Metric]; ok {
			continue
		}
		if prefix != "" && !strings.HasPrefix(p.Metric, prefix) {
			continue
		}
		seen[p.Metric] = struct{}{}
		metrics = append(metrics, p.Metric)
	}

	sort.Strings(metrics)

	if limit > 0 && len(metrics) > limit {
		metrics = metrics[:limit]
	}

	return metrics, nil
}

// tagsMatch checks if the point's tags contain all the required tags.
func tagsMatch(pointTags, requiredTags map[string]string) bool {
	for k, v := range requiredTags {
		if pointTags[k] != v {
			return false
		}
	}
	return true
}

// computeAggregation computes the aggregation value for matched data points.
func computeAggregation(points []*interfaces.DataPoint, agg interfaces.AggFunc) float64 {
	if len(points) == 0 {
		return 0
	}

	switch agg {
	case interfaces.AggSum:
		var sum float64
		for _, p := range points {
			sum += p.Value
		}
		return sum
	case interfaces.AggAvg:
		var sum float64
		for _, p := range points {
			sum += p.Value
		}
		return sum / float64(len(points))
	case interfaces.AggMin:
		min := points[0].Value
		for _, p := range points[1:] {
			if p.Value < min {
				min = p.Value
			}
		}
		return min
	case interfaces.AggMax:
		max := points[0].Value
		for _, p := range points[1:] {
			if p.Value > max {
				max = p.Value
			}
		}
		return max
	case interfaces.AggCount:
		return float64(len(points))
	default:
		return 0
	}
}
