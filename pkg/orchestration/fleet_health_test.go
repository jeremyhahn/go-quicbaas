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
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func defaultJudge(t *testing.T) *FleetHealthJudge {
	t.Helper()
	judge, err := NewFleetHealthJudge(DefaultFleetHealthThresholds())
	if err != nil {
		t.Fatalf("NewFleetHealthJudge: %v", err)
	}
	return judge
}

// measuredNode is a node heard from at now with a fresh, healthy sample.
func measuredNode(id string, now time.Time) *FleetNode {
	return &FleetNode{ID: id, LastSeen: now.UnixNano(), HostMetrics: healthyHostMetrics(now)}
}

func TestFleetHealthJudge_AMeasuredQuietNodeIsHealthy(t *testing.T) {
	now := time.Now()
	verdict := defaultJudge(t).Judge(measuredNode("n1", now), now)
	if !verdict.Healthy || verdict.Reason != FleetNodeHealthOK || verdict.NodeID != "n1" {
		t.Fatalf("verdict = %+v, want healthy", verdict)
	}
}

func TestFleetHealthJudge_AnUnmeasuredNodeIsUnhealthy(t *testing.T) {
	now := time.Now()
	judge := defaultJudge(t)

	never := judge.Judge(&FleetNode{ID: "n1", LastSeen: now.UnixNano()}, now)
	if never.Healthy || never.Reason != FleetNodeHealthUnmeasured {
		t.Errorf("never-measured verdict = %+v, want unmeasured", never)
	}
	if none := judge.Judge(nil, now); none.Healthy || none.Reason != FleetNodeHealthUnmeasured {
		t.Errorf("nil-node verdict = %+v, want unmeasured", none)
	}
}

func TestFleetHealthJudge_AnUnreadJudgedSignalIsUnmeasured(t *testing.T) {
	cases := map[string]func(*HostMetrics){
		"cpu":            func(m *HostMetrics) { m.CPUUtilization = Unread() },
		"mem available":  func(m *HostMetrics) { m.MemAvailableBytes = Unread() },
		"mem total":      func(m *HostMetrics) { m.MemTotalBytes = Unread() },
		"disk available": func(m *HostMetrics) { m.DiskAvailableBytes = Unread() },
		"disk total":     func(m *HostMetrics) { m.DiskTotalBytes = Unread() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			node := measuredNode("n1", now)
			mutate(node.HostMetrics)
			verdict := defaultJudge(t).Judge(node, now)
			if verdict.Healthy || verdict.Reason != FleetNodeHealthUnmeasured {
				t.Fatalf("verdict = %+v, want unmeasured: an unread signal must not read as zero", verdict)
			}
		})
	}
}

func TestFleetHealthJudge_LoadIsDiagnosticNotJudged(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.HostMetrics.Load1 = Unread()
	if verdict := defaultJudge(t).Judge(node, now); !verdict.Healthy {
		t.Fatalf("verdict = %+v, want healthy: load is reported, not judged", verdict)
	}
}

func TestFleetHealthJudge_AnInvalidSampleIsUnhealthy(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.HostMetrics.CPUUtilization = Derived(2)
	verdict := defaultJudge(t).Judge(node, now)
	if verdict.Healthy || verdict.Reason != FleetNodeHealthInvalidSample {
		t.Fatalf("verdict = %+v, want invalid_sample", verdict)
	}
}

func TestFleetHealthJudge_SilencePastTheStaleBoundIsStale(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now.Add(-2*DefaultFleetHeartbeatTimeout))
	verdict := defaultJudge(t).Judge(node, now)
	if verdict.Healthy || verdict.Reason != FleetNodeHealthStale {
		t.Fatalf("verdict = %+v, want stale", verdict)
	}
	if verdict.SampleAge < 2*DefaultFleetHeartbeatTimeout {
		t.Errorf("sample age = %s, want at least %s", verdict.SampleAge, 2*DefaultFleetHeartbeatTimeout)
	}
}

func TestFleetHealthJudge_TheStaleBoundIsInclusive(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now.Add(-DefaultFleetHeartbeatTimeout))
	if verdict := defaultJudge(t).Judge(node, now); !verdict.Healthy {
		t.Fatalf("verdict = %+v, want healthy exactly at the bound", verdict)
	}
}

// wp11 fail-open fix: staleness was measured from ARRIVAL, so a sample
// collected long ago but delivered just now counted as fresh. The collection
// clock must age it.
func TestFleetHealthJudge_ALateDeliveredSampleIsAgedFromCollection(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.HostMetrics.CollectedAt = now.Add(-2 * DefaultFleetHeartbeatTimeout)
	verdict := defaultJudge(t).Judge(node, now)
	if verdict.Healthy || verdict.Reason != FleetNodeHealthStale {
		t.Fatalf("verdict = %+v, want stale: it arrived now but was taken two windows ago", verdict)
	}
}

// The other clock: a sample stamped in the future (a skewed daemon clock)
// cannot keep a silent node fresh, because arrival time still ages it.
func TestFleetHealthJudge_AFutureStampDoesNotHideSilence(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.LastSeen = now.Add(-2 * DefaultFleetHeartbeatTimeout).UnixNano()
	node.HostMetrics.CollectedAt = now.Add(time.Hour)
	verdict := defaultJudge(t).Judge(node, now)
	if verdict.Healthy || verdict.Reason != FleetNodeHealthStale {
		t.Fatalf("verdict = %+v, want stale", verdict)
	}
}

func TestFleetHealthJudge_BoundsAreEnforced(t *testing.T) {
	cases := map[string]struct {
		mutate func(*HostMetrics)
		reason FleetNodeHealthReason
	}{
		"cpu saturated":         {func(m *HostMetrics) { m.CPUUtilization = Derived(0.95) }, FleetNodeHealthCPUSaturated},
		"memory below floor":    {func(m *HostMetrics) { m.MemAvailableBytes = Measured(1 << 30) }, FleetNodeHealthMemoryExhausted},
		"memory past ceiling":   {func(m *HostMetrics) { m.MemTotalBytes = Measured(400 << 30); m.MemAvailableBytes = Measured(20 << 30) }, FleetNodeHealthMemoryExhausted},
		"disk below floor":      {func(m *HostMetrics) { m.DiskAvailableBytes = Measured(1 << 30) }, FleetNodeHealthDiskExhausted},
		"disk past ceiling":     {func(m *HostMetrics) { m.DiskAvailableBytes = Measured(20 << 30) }, FleetNodeHealthDiskExhausted},
		"zero total is unknown": {func(m *HostMetrics) { m.MemTotalBytes = Measured(0); m.MemAvailableBytes = Measured(0) }, FleetNodeHealthMemoryExhausted},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			node := measuredNode("n1", now)
			tc.mutate(node.HostMetrics)
			verdict := defaultJudge(t).Judge(node, now)
			if verdict.Healthy || verdict.Reason != tc.reason {
				t.Fatalf("verdict = %+v, want %s", verdict, tc.reason)
			}
			if verdict.Detail == "" {
				t.Error("an unhealthy verdict carries no detail")
			}
		})
	}
}

func TestFleetHealthJudge_HealthyNodesKeepsOnlyTheHealthy(t *testing.T) {
	now := time.Now()
	healthy := measuredNode("ok", now)
	stale := measuredNode("stale", now.Add(-time.Hour))
	unmeasured := &FleetNode{ID: "unmeasured", LastSeen: now.UnixNano()}
	got := defaultJudge(t).HealthyNodes([]*FleetNode{stale, healthy, nil, unmeasured}, now)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("healthy nodes = %v, want only ok", got)
	}
}

func TestNewFleetHealthJudge_RefusesUnusableBounds(t *testing.T) {
	cases := map[string]struct {
		mutate func(*FleetHealthThresholds)
		field  string
	}{
		"zero stale bound":   {func(th *FleetHealthThresholds) { th.MaxSampleAge = 0 }, "max_sample_age"},
		"negative mem floor": {func(th *FleetHealthThresholds) { th.MinMemAvailableBytes = -1 }, "min_mem_available_bytes"},
		"zero cpu ceiling":   {func(th *FleetHealthThresholds) { th.MaxCPUUtilization = 0 }, "max_cpu_utilization"},
		"NaN mem ceiling":    {func(th *FleetHealthThresholds) { th.MaxMemUsedFraction = math.NaN() }, "max_mem_used_fraction"},
		"disk ceiling > 1":   {func(th *FleetHealthThresholds) { th.MaxDiskUsedFraction = 1.5 }, "max_disk_used_fraction"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			thresholds := DefaultFleetHealthThresholds()
			tc.mutate(&thresholds)
			judge, err := NewFleetHealthJudge(thresholds)
			var invalid *FleetHealthThresholdsError
			if judge != nil || !errors.As(err, &invalid) || !errors.Is(err, ErrFleetHealthThresholdsInvalid) {
				t.Fatalf("NewFleetHealthJudge = %v, %v; want a FleetHealthThresholdsError", judge, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

func TestFleetHealthJudge_ThresholdsReturnsTheBounds(t *testing.T) {
	if got := defaultJudge(t).Thresholds(); got != DefaultFleetHealthThresholds() {
		t.Fatalf("Thresholds() = %+v, want the defaults", got)
	}
}

func TestJudgeFleetHealth_UsesTheFleetHeartbeatTimeoutAsTheStaleBound(t *testing.T) {
	svc, _ := reconcileHarness(t)
	registry := NewInMemoryFleetRegistry()
	svc.SetFleetRegistry(registry)
	svc.SetFleetHeartbeatTimeout(time.Hour)
	ctx := ContextWithTenant(context.Background(), "t")
	if err := registry.RegisterFleetNode(ctx, &FleetNode{ID: "n1", TenantID: "t", Name: "n1", Host: "h", NodeType: "claude-code"}); err != nil {
		t.Fatalf("RegisterFleetNode: %v", err)
	}
	// Collected well past the 90s default but inside the configured hour.
	sample := healthyHostMetrics(time.Now().Add(-10 * time.Minute))
	if err := registry.Heartbeat(ctx, "t", "n1", &FleetNodeObservation{Backends: []string{"claude"}, HostMetrics: sample}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	verdicts, err := svc.JudgeFleetHealth(ctx, "t")
	if err != nil {
		t.Fatalf("JudgeFleetHealth: %v", err)
	}
	if len(verdicts) != 1 || !verdicts[0].Healthy {
		t.Fatalf("verdicts = %+v, want one healthy node under a one-hour stale bound", verdicts)
	}

	svc.SetFleetHeartbeatTimeout(time.Minute)
	verdicts, err = svc.JudgeFleetHealth(ctx, "t")
	if err != nil {
		t.Fatalf("JudgeFleetHealth: %v", err)
	}
	if len(verdicts) != 1 || verdicts[0].Reason != FleetNodeHealthStale {
		t.Fatalf("verdicts = %+v, want stale under a one-minute stale bound", verdicts)
	}
}

func TestJudgeFleetHealth_RefusesWithoutARegistry(t *testing.T) {
	svc, _ := reconcileHarness(t)
	_, err := svc.JudgeFleetHealth(ContextWithTenant(context.Background(), "t"), "t")
	if !errors.Is(err, ErrFleetRegistryRequired) {
		t.Fatalf("JudgeFleetHealth = %v, want ErrFleetRegistryRequired", err)
	}
	if !strings.Contains(err.Error(), "registry") {
		t.Errorf("error %q does not name the registry", err)
	}
}
