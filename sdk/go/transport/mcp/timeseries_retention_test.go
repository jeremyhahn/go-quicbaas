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

package mcp

import (
	"encoding/json"
	"testing"
	"time"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestTimeseriesRetention_MapsToServerTools(t *testing.T) {
	if got := mapTimeseriesMethodToTool("SetRetention"); got != "qrdb_ts_set_retention_policy" {
		t.Errorf("SetRetention tool = %q", got)
	}
	if got := mapTimeseriesMethodToTool("GetRetention"); got != "qrdb_ts_get_retention_policy" {
		t.Errorf("GetRetention tool = %q", got)
	}
}

func TestTimeseriesSetRetention_ToolArguments(t *testing.T) {
	h := newTSHandler(t)
	args, err := h.transformRequest("SetRetention", &timeseries.SetRetentionRequest{
		Metric: "cpu",
		Policy: timeseries.RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: timeseries.AggregateAvg},
	})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}
	if args["metric"] != "cpu" || args["duration_seconds"] != int64(3600) || args["resolution_seconds"] != int64(60) || args["aggregation"] != "avg" {
		t.Errorf("SetRetention arguments: %v", args)
	}

	args, err = h.transformRequest("SetRetention", &timeseries.SetRetentionRequest{
		Metric: "cpu", Policy: timeseries.RetentionPolicy{Duration: time.Hour},
	})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}
	if _, ok := args["resolution_seconds"]; ok {
		t.Errorf("no resolution must send no resolution_seconds: %v", args)
	}
	if _, ok := args["aggregation"]; ok {
		t.Errorf("AggregateNone must send no aggregation: %v", args)
	}

	resp, err := h.transformResponse("SetRetention", nil, map[string]interface{}{"success": true})
	if err != nil || resp != nil {
		t.Errorf("SetRetention response: got %v, %v; want nil, nil", resp, err)
	}
}

func TestTimeseriesGetRetention_ToolArgumentsAndResult(t *testing.T) {
	h := newTSHandler(t)
	args, err := h.transformRequest("GetRetention", &timeseries.GetRetentionRequest{Metric: "cpu"})
	if err != nil || args["metric"] != "cpu" {
		t.Fatalf("GetRetention arguments: %v, %v", args, err)
	}

	// The server's TSGetRetentionPolicyResponse as JSON: durations in ns.
	resp, err := h.transformResponse("GetRetention", nil, map[string]interface{}{
		"metric": "cpu",
		"policy": map[string]interface{}{
			"duration":    float64(24 * time.Hour),
			"resolution":  float64(5 * time.Minute),
			"aggregation": "max",
		},
		"timestamp": "2026-11-14T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	got, ok := resp.(*timeseries.GetRetentionResponse)
	if !ok {
		t.Fatalf("response type %T", resp)
	}
	want := timeseries.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: timeseries.AggregateMax}
	if got.Metric != "cpu" || got.Policy == nil || *got.Policy != want {
		t.Fatalf("GetRetention: got %+v (policy %+v), want %+v", got, got.Policy, want)
	}

	resp, _ = h.transformResponse("GetRetention", nil, map[string]interface{}{"metric": "cpu"})
	if got := resp.(*timeseries.GetRetentionResponse); got.Policy != nil {
		t.Errorf("no policy must decode to a nil Policy, got %+v", got.Policy)
	}
}

// TestTimeseriesGetRetentionPolicy_ReadsServerPolicy proves the untyped
// GetRetentionPolicy reader takes the server's nested policy (durations in
// ns) and reports whole seconds, and still reads the older top-level fields.
func TestTimeseriesGetRetentionPolicy_ReadsServerPolicy(t *testing.T) {
	h := newTSHandler(t)
	for name, in := range map[string]map[string]interface{}{
		"server policy": {
			"metric": "cpu",
			"policy": map[string]interface{}{
				"duration":    float64(24 * time.Hour),
				"resolution":  float64(5 * time.Minute),
				"aggregation": "max",
			},
		},
		"top-level seconds": {
			"metric": "cpu", "duration_seconds": float64(86400), "resolution_seconds": float64(300), "aggregation": "max",
		},
	} {
		out, err := h.transformResponse("GetRetentionPolicy", nil, in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		p := out.(map[string]interface{})["policy"].(map[string]interface{})
		if p["duration"] != int64(86400) || p["resolution"] != int64(300) || p["aggregation"] != "max" {
			t.Errorf("%s: policy = %v, want 86400s, 300s, max", name, p)
		}
	}
}

// nativeRetentionResult is go-qrdb's own qrdb_ts_get_retention_policy tool
// result, json.Marshal of *timeseries.RetentionPolicy, decoded the way
// extractContent decodes a tool result. Building it from go-qrdb's type pins
// the real wire format: top level, durations in ns, aggregation a number.
func nativeRetentionResult(t *testing.T, policy *qrdbtimeseries.RetentionPolicy) interface{} {
	t.Helper()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}
	return out
}

// TestTimeseriesGetRetention_NativeQRDBResult proves the typed GetRetention
// reads go-qrdb's native result, maps its AggFunc numbers by name (go-qrdb
// and the SDK order them differently), and reports the requested metric.
func TestTimeseriesGetRetention_NativeQRDBResult(t *testing.T) {
	h := newTSHandler(t)
	req := &timeseries.GetRetentionRequest{Metric: "cpu"}
	for _, c := range []struct {
		agg  qrdbtimeseries.AggFunc
		want timeseries.AggregateFunction
	}{
		{qrdbtimeseries.AggMax, timeseries.AggregateMax},
		{qrdbtimeseries.AggAvg, timeseries.AggregateAvg},
		{qrdbtimeseries.AggSum, timeseries.AggregateSum},
	} {
		result := nativeRetentionResult(t, &qrdbtimeseries.RetentionPolicy{
			Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: c.agg,
		})
		resp, err := h.transformResponse("GetRetention", req, result)
		if err != nil {
			t.Fatalf("agg %v: transformResponse: %v", c.agg, err)
		}
		got := resp.(*timeseries.GetRetentionResponse)
		want := timeseries.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: c.want}
		if got.Metric != "cpu" || got.Policy == nil || *got.Policy != want {
			t.Errorf("agg %v: got metric %q policy %+v, want cpu %+v", c.agg, got.Metric, got.Policy, want)
		}
	}

	for name, result := range map[string]interface{}{
		"no policy":   nativeRetentionResult(t, &qrdbtimeseries.RetentionPolicy{}),
		"null result": nil,
	} {
		resp, err := h.transformResponse("GetRetention", req, result)
		if err != nil {
			t.Fatalf("%s: transformResponse: %v", name, err)
		}
		got := resp.(*timeseries.GetRetentionResponse)
		if got.Metric != "cpu" || got.Policy != nil {
			t.Errorf("%s: got metric %q policy %+v, want cpu and a nil Policy", name, got.Metric, got.Policy)
		}
	}
}

// TestTimeseriesGetRetentionPolicy_NativeQRDBResult proves the untyped
// reader takes go-qrdb's native result and the request's metric.
func TestTimeseriesGetRetentionPolicy_NativeQRDBResult(t *testing.T) {
	h := newTSHandler(t)
	result := nativeRetentionResult(t, &qrdbtimeseries.RetentionPolicy{
		Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: qrdbtimeseries.AggMax,
	})
	out, err := h.transformResponse("GetRetentionPolicy", map[string]interface{}{"metric": "cpu"}, result)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	m := out.(map[string]interface{})
	p := m["policy"].(map[string]interface{})
	if m["metric"] != "cpu" || p["duration"] != int64(86400) || p["resolution"] != int64(300) || p["aggregation"] != "max" {
		t.Errorf("GetRetentionPolicy = %v, want cpu, 86400s, 300s, max", m)
	}
}

func TestTimeseriesRetention_WrongRequestTypeIsInvalidInput(t *testing.T) {
	h := newTSHandler(t)
	for _, method := range []string{"SetRetention", "GetRetention"} {
		if _, err := h.transformRequest(method, "bogus"); !errors.IsInvalidInput(err) {
			t.Errorf("%s with a bad request: want InvalidInput, got %v", method, err)
		}
	}
}
