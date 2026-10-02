// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// memoryTSStorage is an in-memory interfaces.TimeSeriesStorage that files
// points and retention policies under the exact metric name it is given, the
// way the node's engine does, so a test sees whether reads and writes agree on
// a name.
type memoryTSStorage struct {
	mu       sync.Mutex
	points   map[string][]*interfaces.DataPoint
	policies map[string]*interfaces.RetentionPolicy
}

var _ interfaces.TimeSeriesStorage = (*memoryTSStorage)(nil)

func newMemoryTSStorage() *memoryTSStorage {
	return &memoryTSStorage{
		points:   map[string][]*interfaces.DataPoint{},
		policies: map[string]*interfaces.RetentionPolicy{},
	}
}

func (m *memoryTSStorage) WritePoint(_ context.Context, point *interfaces.DataPoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.points[point.Metric] = append(m.points[point.Metric], point)
	return nil
}

func (m *memoryTSStorage) WriteBatch(points []interfaces.DataPoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range points {
		p := points[i]
		m.points[p.Metric] = append(m.points[p.Metric], &p)
	}
	return nil
}

func (m *memoryTSStorage) QueryRich(_ context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := &interfaces.QueryResult{Metric: req.Metric}
	for _, p := range m.points[req.Metric] {
		if p.Timestamp.Before(req.StartTime) || p.Timestamp.After(req.EndTime) {
			continue
		}
		result.Points = append(result.Points, p)
	}
	return result, nil
}

func (m *memoryTSStorage) DeleteSeries(_ context.Context, req *interfaces.DeleteRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.points, req.Metric)
	return nil
}

func (m *memoryTSStorage) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policies[metric], nil
}

func (m *memoryTSStorage) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[metric] = policy
	return nil
}

func (m *memoryTSStorage) ListMetrics(prefix string, _ int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name := range m.points {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// newScopingTestService returns a time-series service over storage with no
// Raft client, so writes land in storage directly.
func newScopingTestService(t *testing.T, storage interfaces.TimeSeriesStorage) *timeSeriesService {
	t.Helper()
	return &timeSeriesService{svc: &quicBaaS{logger: createTestLogger(t)}, tsStorage: storage}
}

// queryWindow is a TSQuery over metric covering at.
func queryWindow(metric string, at time.Time) *TSQueryRequest {
	return &TSQueryRequest{Metric: metric, StartTime: at.Add(-time.Minute), EndTime: at.Add(time.Minute)}
}

// Regression: a tenant's TSQuery read the bare metric name while the write
// path filed it under the tenant's scope, so the tenant never saw its data.
func TestTimeSeriesScoping_ATenantReadsWhatItWrote(t *testing.T) {
	storage := newMemoryTSStorage()
	tsSvc := newScopingTestService(t, storage)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	at := time.Now().UTC()

	_, err := tsSvc.WritePoint(acme, &WritePointRequest{Metric: "cpu.usage", Value: 42, Timestamp: at})
	require.NoError(t, err)
	_, err = tsSvc.WriteBatch(acme, &WriteBatchRequest{Points: []*WritePointRequest{
		{Metric: "cpu.usage", Value: 43, Timestamp: at},
	}})
	require.NoError(t, err)

	resp, err := tsSvc.TSQuery(acme, queryWindow("cpu.usage", at))
	require.NoError(t, err)
	assert.Equal(t, "cpu.usage", resp.Metric, "the tenant sees its own name, not the internal scope")
	require.Len(t, resp.Points, 2)
	for _, p := range resp.Points {
		assert.Equal(t, "cpu.usage", p.Metric)
	}

	listed, err := tsSvc.ListMetrics(acme, &TSListMetricsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"cpu.usage"}, listed.Metrics)
}

func TestTimeSeriesScoping_ATenantDoesNotReadAnothersSeries(t *testing.T) {
	storage := newMemoryTSStorage()
	tsSvc := newScopingTestService(t, storage)
	at := time.Now().UTC()

	_, err := tsSvc.WritePoint(tenantctx.WithTenant(context.Background(), "acme"),
		&WritePointRequest{Metric: "cpu.usage", Value: 42, Timestamp: at})
	require.NoError(t, err)

	globex := tenantctx.WithTenant(context.Background(), "globex")
	resp, err := tsSvc.TSQuery(globex, queryWindow("cpu.usage", at))
	require.NoError(t, err)
	assert.Empty(t, resp.Points)

	listed, err := tsSvc.ListMetrics(globex, &TSListMetricsRequest{})
	require.NoError(t, err)
	assert.Empty(t, listed.Metrics)
}

func TestTimeSeriesScoping_RetentionAndDeleteActOnTheTenantsSeries(t *testing.T) {
	storage := newMemoryTSStorage()
	tsSvc := newScopingTestService(t, storage)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	at := time.Now().UTC()

	_, err := tsSvc.SetRetentionPolicy(acme, &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage", Policy: &TSRetentionPolicy{Duration: time.Hour, Aggregation: "none"},
	})
	require.NoError(t, err)
	assert.Contains(t, storage.policies, "ts-tenant/acme/cpu.usage")

	got, err := tsSvc.GetRetentionPolicy(acme, &TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
	require.NoError(t, err)
	require.NotNil(t, got.Policy)
	assert.Equal(t, time.Hour, got.Policy.Duration)

	_, err = tsSvc.WritePoint(acme, &WritePointRequest{Metric: "cpu.usage", Value: 1, Timestamp: at})
	require.NoError(t, err)
	_, err = tsSvc.DeleteSeries(acme, &TSDeleteRequest{Metric: "cpu.usage"})
	require.NoError(t, err)
	resp, err := tsSvc.TSQuery(acme, queryWindow("cpu.usage", at))
	require.NoError(t, err)
	assert.Empty(t, resp.Points, "the delete reached the series the tenant wrote")
}

func TestTimeSeriesScoping_TheSystemTenantUsesTheBareName(t *testing.T) {
	storage := newMemoryTSStorage()
	tsSvc := newScopingTestService(t, storage)
	at := time.Now().UTC()

	_, err := tsSvc.WritePoint(tenantctx.WithSystem(context.Background()), &WritePointRequest{Metric: "cpu.usage", Value: 7, Timestamp: at})
	require.NoError(t, err)
	assert.Contains(t, storage.points, "cpu.usage")

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), queryWindow("cpu.usage", at))
	require.NoError(t, err)
	require.Len(t, resp.Points, 1)
}

func TestTimeSeriesScoping_AnInvalidTenantIsRefusedOnEveryPath(t *testing.T) {
	tsSvc := newScopingTestService(t, newMemoryTSStorage())
	bad := tenantctx.WithTenant(context.Background(), "not a tenant!")
	at := time.Now().UTC()

	calls := map[string]func() error{
		"write point": func() error {
			_, err := tsSvc.WritePoint(bad, &WritePointRequest{Metric: "m", Timestamp: at})
			return err
		},
		"write batch": func() error {
			_, err := tsSvc.WriteBatch(bad, &WriteBatchRequest{Points: []*WritePointRequest{{Metric: "m"}}})
			return err
		},
		"query": func() error {
			_, err := tsSvc.TSQuery(bad, queryWindow("m", at))
			return err
		},
		"delete": func() error {
			_, err := tsSvc.DeleteSeries(bad, &TSDeleteRequest{Metric: "m"})
			return err
		},
		"get retention": func() error {
			_, err := tsSvc.GetRetentionPolicy(bad, &TSGetRetentionPolicyRequest{Metric: "m"})
			return err
		},
		"set retention": func() error {
			_, err := tsSvc.SetRetentionPolicy(bad, &TSSetRetentionPolicyRequest{Metric: "m", Policy: &TSRetentionPolicy{}})
			return err
		},
		"list metrics": func() error {
			_, err := tsSvc.ListMetrics(bad, &TSListMetricsRequest{})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
			var invalid *TimeSeriesTenantValidationError
			assert.ErrorAs(t, err, &invalid)
		})
	}
}
