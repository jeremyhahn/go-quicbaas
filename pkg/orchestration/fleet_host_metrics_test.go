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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// healthyHostMetrics is a fully measured sample of a quiet host collected at.
func healthyHostMetrics(at time.Time) *HostMetrics {
	return &HostMetrics{
		CollectedAt:        at,
		CPUUtilization:     Derived(0.25),
		CPUCount:           Measured(16),
		Load1:              Measured(2.5),
		Load5:              Measured(2),
		Load15:             Measured(1.5),
		MemTotalBytes:      Measured(64 << 30),
		MemAvailableBytes:  Measured(48 << 30),
		DiskTotalBytes:     Measured(1 << 40),
		DiskAvailableBytes: Measured(512 << 30),
	}
}

func TestHostMetricsValidate_AcceptsAMeasuredSample(t *testing.T) {
	if err := healthyHostMetrics(time.Now()).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestHostMetricsValidate_AnUnreadSignalIsAbsentNotAFault(t *testing.T) {
	sample := healthyHostMetrics(time.Now())
	sample.CPUUtilization = Unread()
	sample.DiskTotalBytes = Unread()
	if err := sample.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: an unread signal is absent, not invalid", err)
	}
}

func TestHostMetricsValidate_RefusesWhatCannotBeAHost(t *testing.T) {
	cases := map[string]struct {
		mutate func(*HostMetrics)
		field  string
	}{
		"no collection time": {func(m *HostMetrics) { m.CollectedAt = time.Time{} }, "collected_at"},
		"negative memory":    {func(m *HostMetrics) { m.MemAvailableBytes = Measured(-1) }, "mem_available_bytes"},
		"negative load":      {func(m *HostMetrics) { m.Load1 = Measured(-0.5) }, "load1"},
		"cpu above one":      {func(m *HostMetrics) { m.CPUUtilization = Derived(1.5) }, "cpu_utilization"},
		"cpu below zero":     {func(m *HostMetrics) { m.CPUUtilization = Derived(-0.1) }, "cpu_utilization"},
		"memory overcommit":  {func(m *HostMetrics) { m.MemAvailableBytes = Measured(65 << 30) }, "mem_available_bytes"},
		"disk overcommit":    {func(m *HostMetrics) { m.DiskAvailableBytes = Measured(2 << 40) }, "disk_available_bytes"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sample := healthyHostMetrics(time.Now())
			tc.mutate(sample)
			err := sample.Validate()
			var invalid *HostMetricsInvalidError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrHostMetricsInvalid) {
				t.Fatalf("Validate() = %v, want a HostMetricsInvalidError", err)
			}
			if invalid.Field != tc.field {
				t.Errorf("field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

func TestHostMetricsValidate_RefusesNil(t *testing.T) {
	var sample *HostMetrics
	if err := sample.Validate(); !errors.Is(err, ErrHostMetricsInvalid) {
		t.Fatalf("Validate() on nil = %v, want ErrHostMetricsInvalid", err)
	}
}

func TestHostMetricsJSON_AbsentTravelsAsAbsentNotZero(t *testing.T) {
	sample := healthyHostMetrics(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	sample.MemAvailableBytes = Unread()

	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"mem_available_bytes":{"value":0`) {
		t.Fatalf("an unread signal was written as zero: %s", data)
	}

	var decoded HostMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.MemAvailableBytes.Known() {
		t.Errorf("unread memory decoded as known %v", decoded.MemAvailableBytes)
	}
	if got, ok := decoded.CPUUtilization.Value(); !ok || got != 0.25 || decoded.CPUUtilization.Provenance() != ProvenanceDerived {
		t.Errorf("cpu = %v (%v), want derived 0.25", got, decoded.CPUUtilization.Provenance())
	}
	if got, ok := decoded.MemTotalBytes.Value(); !ok || got != 64<<30 || decoded.MemTotalBytes.Provenance() != ProvenanceMeasured {
		t.Errorf("mem total = %v, want measured 64 GiB", got)
	}
	if !decoded.CollectedAt.Equal(sample.CollectedAt) {
		t.Errorf("collected_at = %v, want %v", decoded.CollectedAt, sample.CollectedAt)
	}
}

// A failed calculation (a division by a zero interval) produces NaN, and NaN
// compares false against every bound. It must never become a measurement
// (wp11 fail-open fix: NaN passed validation).
func TestHostMetrics_NaNIsNeverAMeasurement(t *testing.T) {
	sample := healthyHostMetrics(time.Now())
	sample.CPUUtilization = Derived(math.NaN())
	sample.MemAvailableBytes = Measured(math.Inf(1))

	if sample.CPUUtilization.Known() || sample.MemAvailableBytes.Known() {
		t.Fatal("a non-finite value became a known reading")
	}
	judge := defaultJudge(t)
	now := time.Now()
	verdict := judge.Judge(&FleetNode{ID: "n", LastSeen: now.UnixNano(), HostMetrics: sample}, now)
	if verdict.Healthy || verdict.Reason != FleetNodeHealthUnmeasured {
		t.Fatalf("verdict = %+v, want unhealthy and unmeasured", verdict)
	}
}

func TestHeartbeat_RecordsTheReportedHostMetrics(t *testing.T) {
	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	if err := registry.RegisterFleetNode(ctx, &FleetNode{ID: "n1", TenantID: "t", Name: "n1", Host: "h", NodeType: "claude-code"}); err != nil {
		t.Fatalf("RegisterFleetNode: %v", err)
	}
	sample := healthyHostMetrics(time.Now().Truncate(time.Second))
	observed := &FleetNodeObservation{Backends: []string{"claude"}, HostMetrics: sample}
	if err := registry.Heartbeat(ctx, "t", "n1", observed); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	node, err := registry.GetFleetNode(ctx, "t", "n1")
	if err != nil {
		t.Fatalf("GetFleetNode: %v", err)
	}
	if node.HostMetrics == nil {
		t.Fatal("the reported sample was not recorded")
	}
	if got, _ := node.HostMetrics.MemAvailableBytes.Value(); got != 48<<30 {
		t.Errorf("mem available = %v, want 48 GiB", got)
	}
	if !node.HostMetrics.CollectedAt.Equal(sample.CollectedAt) {
		t.Errorf("collected_at = %v, want %v", node.HostMetrics.CollectedAt, sample.CollectedAt)
	}
}

func TestHeartbeat_SilenceOnHostMetricsKeepsTheSampleToAgeOut(t *testing.T) {
	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	if err := registry.RegisterFleetNode(ctx, &FleetNode{ID: "n1", TenantID: "t", Name: "n1", Host: "h", NodeType: "claude-code"}); err != nil {
		t.Fatalf("RegisterFleetNode: %v", err)
	}
	first := healthyHostMetrics(time.Now().Add(-time.Hour))
	if err := registry.Heartbeat(ctx, "t", "n1", &FleetNodeObservation{Backends: []string{"claude"}, HostMetrics: first}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := registry.Heartbeat(ctx, "t", "n1", &FleetNodeObservation{Backends: []string{"claude"}}); err != nil {
		t.Fatalf("Heartbeat without metrics: %v", err)
	}
	if err := registry.Heartbeat(ctx, "t", "n1", nil); err != nil {
		t.Fatalf("bare Heartbeat: %v", err)
	}
	node, err := registry.GetFleetNode(ctx, "t", "n1")
	if err != nil {
		t.Fatalf("GetFleetNode: %v", err)
	}
	if node.HostMetrics == nil || !node.HostMetrics.CollectedAt.Equal(first.CollectedAt) {
		t.Fatalf("host metrics = %+v, want the earlier sample kept", node.HostMetrics)
	}
	// The kept sample is an hour old by its collection clock even though the
	// node heartbeated just now, so the verdict is stale, not healthy.
	verdict := defaultJudge(t).Judge(node, time.Now())
	if verdict.Healthy || verdict.Reason != FleetNodeHealthStale {
		t.Fatalf("verdict = %+v, want stale", verdict)
	}
}
