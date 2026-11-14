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
	"testing"
	"time"

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

	resp, err := h.transformResponse("SetRetention", map[string]interface{}{"success": true})
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
	resp, err := h.transformResponse("GetRetention", map[string]interface{}{
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

	resp, _ = h.transformResponse("GetRetention", map[string]interface{}{"metric": "cpu"})
	if got := resp.(*timeseries.GetRetentionResponse); got.Policy != nil {
		t.Errorf("no policy must decode to a nil Policy, got %+v", got.Policy)
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
