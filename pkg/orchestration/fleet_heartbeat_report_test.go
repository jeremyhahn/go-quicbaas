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
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

func TestFleetHeartbeatReport_ObservationOfNothing(t *testing.T) {
	var report *FleetHeartbeatReport
	if got := report.Observation(); got != nil {
		t.Fatalf("a nil report observed %#v", got)
	}
	capacityOnly := &FleetHeartbeatReport{MaxConcurrentJobs: 4}
	if got := capacityOnly.Observation(); got != nil {
		t.Fatalf("a report with no backends key observed %#v; it must reconcile nothing", got)
	}
}

func TestFleetHeartbeatReport_ObservationCopiesEveryField(t *testing.T) {
	backends := []string{"claude"}
	agents := []string{}
	images := []string{"img:1"}
	relay := true
	sample := &HostMetrics{CollectedAt: time.Unix(100, 0), CPUCount: Measured(8)}
	report := &FleetHeartbeatReport{
		Backends:          &backends,
		MaxConcurrentJobs: 2,
		SigningAgents:     &agents,
		SigningRelay:      &relay,
		Images:            &images,
		Devices:           []DeviceSample{{Kind: DeviceKind("gpu"), ID: "g0"}},
		HostMetrics:       sample,
	}

	got := report.Observation()
	if got == nil {
		t.Fatal("a report with backends must observe")
	}
	if got.MaxConcurrentJobs != 2 || len(got.Devices) != 1 {
		t.Errorf("capacity or devices lost: %#v", got)
	}
	if got.SigningAgents == nil || len(got.SigningAgents) != 0 {
		t.Errorf("an empty signing-agent report must stay a non-nil empty list, got %#v", got.SigningAgents)
	}
	if got.HostMetrics == nil || got.HostMetrics == sample || !got.HostMetrics.CollectedAt.Equal(sample.CollectedAt) {
		t.Errorf("host metrics must be carried as a copy, got %#v", got.HostMetrics)
	}

	// The observation must not alias the report: the registry keeps it.
	backends[0] = "mutated"
	images[0] = "mutated"
	relay = false
	if got.Backends[0] != "claude" || (*got.Images)[0] != "img:1" || !*got.SigningRelay {
		t.Errorf("the observation aliases the report: %#v", got)
	}
}

func TestFleetHeartbeatReport_EmptyServedListIsStillAnObservation(t *testing.T) {
	got, err := ParseFleetHeartbeatReport([]byte(`{"backends":[]}`))
	if err != nil {
		t.Fatalf("ParseFleetHeartbeatReport: %v", err)
	}
	if got == nil || got.Backends == nil || len(got.Backends) != 0 {
		t.Fatalf("an empty served list must be carried through for the registry to refuse, got %#v", got)
	}
}

func TestParseFleetHeartbeatReport_BareHeartbeat(t *testing.T) {
	got, err := ParseFleetHeartbeatReport(nil)
	if err != nil || got != nil {
		t.Fatalf("an empty body = (%#v, %v), want (nil, nil)", got, err)
	}
}

func TestParseFleetHeartbeatReport_Malformed(t *testing.T) {
	_, err := ParseFleetHeartbeatReport([]byte(`{"backends":"claude"}`))
	if !errors.Is(err, ErrFleetHeartbeatReportInvalid) {
		t.Fatalf("err = %v, want ErrFleetHeartbeatReportInvalid", err)
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Errorf("the decoding error must stay reachable, got %v", err)
	}
}

func TestDecodeFleetHeartbeatReport(t *testing.T) {
	got, err := DecodeFleetHeartbeatReport(nil)
	if err != nil || got != nil {
		t.Fatalf("nil = (%#v, %v), want (nil, nil)", got, err)
	}

	got, err = DecodeFleetHeartbeatReport(map[string]interface{}{
		"backends":     []interface{}{"codex"},
		"host_metrics": map[string]interface{}{"collected_at": "2026-10-12T09:30:00Z", "load1": map[string]interface{}{"value": 0.5, "provenance": "measured"}},
	})
	if err != nil {
		t.Fatalf("DecodeFleetHeartbeatReport: %v", err)
	}
	if got == nil || got.Backends[0] != "codex" || got.HostMetrics == nil {
		t.Fatalf("decoded %#v", got)
	}
	if v, ok := got.HostMetrics.Load1.Value(); !ok || v != 0.5 {
		t.Errorf("load1 = %v (known %v), want 0.5", v, ok)
	}
	if got.HostMetrics.Load5.Known() {
		t.Errorf("an absent reading must be unread, got %#v", got.HostMetrics.Load5)
	}
}

func TestDecodeFleetHeartbeatReport_Unencodable(t *testing.T) {
	_, err := DecodeFleetHeartbeatReport(map[string]interface{}{"backends": make(chan int)})
	if !errors.Is(err, ErrFleetHeartbeatReportInvalid) {
		t.Fatalf("err = %v, want ErrFleetHeartbeatReportInvalid", err)
	}
}

func TestParseReading(t *testing.T) {
	value := 3.5
	notANumber := math.NaN()
	cases := []struct {
		name       string
		value      *float64
		provenance string
		want       Reading
	}{
		{"measured", &value, "measured", Measured(3.5)},
		{"derived", &value, "derived", Derived(3.5)},
		{"absent value is unread, not zero", nil, "measured", Unread()},
		{"unknown provenance is unread", &value, "unknown", Unread()},
		{"estimated is not telemetry", &value, "estimated", Unread()},
		{"unrecognised provenance is unread", &value, "guessed", Unread()},
		{"NaN is never a measurement", &notANumber, "measured", Unread()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseReading(tc.value, tc.provenance); got != tc.want {
				t.Fatalf("ParseReading = %#v, want %#v", got, tc.want)
			}
		})
	}
}
