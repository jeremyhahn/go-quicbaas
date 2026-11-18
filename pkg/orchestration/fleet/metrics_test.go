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

package fleet

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// readCounterLabels returns the count of the single label-series matching
// labelValues. Returns 0 if the series has never been observed.
func readCounterLabels(t *testing.T, vec *prometheus.CounterVec, labelValues ...string) float64 {
	t.Helper()
	var pb dto.Metric
	if err := vec.WithLabelValues(labelValues...).Write(&pb); err != nil {
		t.Fatalf("metric write: %v", err)
	}
	if c := pb.Counter; c != nil {
		return c.GetValue()
	}
	return 0
}

// readGaugeLabels returns the current gauge value for a specific label series.
func readGaugeLabels(t *testing.T, vec *prometheus.GaugeVec, labelValues ...string) float64 {
	t.Helper()
	var pb dto.Metric
	if err := vec.WithLabelValues(labelValues...).Write(&pb); err != nil {
		t.Fatalf("metric write: %v", err)
	}
	if g := pb.Gauge; g != nil {
		return g.GetValue()
	}
	return 0
}

// readHistogramCount returns the total number of observations recorded
// across every label series of the supplied HistogramVec.
func readHistogramCount(t *testing.T, vec *prometheus.HistogramVec) uint64 {
	t.Helper()
	mchan := make(chan prometheus.Metric, 128)
	go func() {
		vec.Collect(mchan)
		close(mchan)
	}()
	var total uint64
	for m := range mchan {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			t.Fatalf("metric write: %v", err)
		}
		if h := pb.Histogram; h != nil {
			total += h.GetSampleCount()
		}
	}
	return total
}

func TestRegisterMetrics_NilRegistryReturnsError(t *testing.T) {
	t.Parallel()
	if err := RegisterMetrics(nil); !errors.Is(err, ErrMetricsNilRegistry) {
		t.Fatalf("got %v, want ErrMetricsNilRegistry", err)
	}
}

func TestRegisterMetrics_FreshRegistrySucceedsAndSpawnIncrements(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	if err := RegisterMetrics(reg); err != nil {
		t.Fatalf("RegisterMetrics: %v", err)
	}

	const tenant = "metrics-fresh-tenant"
	const backend = "metrics-fresh-backend"
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{backend: &mockBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	before := readCounterLabels(t, SpawnsTotal, tenant, backend, "host")
	if _, err := rt.Spawn(context.Background(), tenant, nil, backend,
		&orchestration.SpawnRequest{WorkDir: t.TempDir()}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	after := readCounterLabels(t, SpawnsTotal, tenant, backend, "host")
	if after-before != 1 {
		t.Fatalf("SpawnsTotal delta: got %v want 1", after-before)
	}

	// Verify the collectors actually made it into the registry we built.
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected families in fresh registry, got none")
	}
}

func TestRegisterMetrics_DuplicateRegistrationReportsError(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	if err := RegisterMetrics(reg); err != nil {
		t.Fatalf("first RegisterMetrics: %v", err)
	}
	err := RegisterMetrics(reg)
	if !errors.Is(err, ErrMetricsAlreadyRegistered) {
		t.Fatalf("second RegisterMetrics: got %v want ErrMetricsAlreadyRegistered", err)
	}
}

func TestRuntime_Close_DecrementsActiveSessions(t *testing.T) {
	t.Parallel()

	const tenant = "close-tenant"
	const backend = "close-backend"
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{backend: &mockBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := rt.Spawn(context.Background(), tenant, nil, backend,
		&orchestration.SpawnRequest{WorkDir: t.TempDir()}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if got := readGaugeLabels(t, ActiveSessions, tenant, backend); got != 1 {
		t.Fatalf("ActiveSessions after Spawn: got %v want 1", got)
	}
	if err := rt.Close(context.Background(), backend, "sess-test"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := readGaugeLabels(t, ActiveSessions, tenant, backend); got != 0 {
		t.Fatalf("ActiveSessions after Close: got %v want 0", got)
	}
}
