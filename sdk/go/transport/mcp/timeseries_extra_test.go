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

// timeseries_extra_test.go covers the timeseries handler's request dispatch,
// the CLI map-based request branches, and every response transform.

package mcp

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func newTSHandler(t *testing.T) *timeseriesHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newTimeseriesHandler(tr)
}

func TestTimeseriesTransformRequestDispatch(t *testing.T) {
	h := newTSHandler(t)
	now := time.Now()

	cases := []struct {
		name   string
		method string
		req    interface{}
		want   string // an argument key that must be present
	}{
		{"write sdk", "Write", &timeseries.WriteRequest{Metric: "m", Value: 1}, "metric"},
		{"write map", "TSWritePoint", map[string]interface{}{"metric": "m", "value": float64(2),
			"timestamp": now, "tags": map[string]string{"a": "b"}}, "metric"},
		{"write batch", "WriteBatch", map[string]interface{}{
			"points": []interface{}{map[string]interface{}{"metric": "m"}}}, "points"},
		{"query sdk", "Query", &timeseries.QueryRequest{Metric: "m", Start: now, End: now}, "metric"},
		{"query map", "TSQuery", map[string]interface{}{"metric": "m",
			"start_time": now, "end_time": now, "aggregation": "avg"}, "metric"},
		{"delete", "DeleteSeries", map[string]interface{}{"metric": "m",
			"tags": map[string]string{"x": "y"}, "start_time": now, "end_time": now}, "metric"},
		{"get retention", "GetRetentionPolicy", map[string]interface{}{"metric": "m"}, "metric"},
		{"set retention", "SetRetentionPolicy", map[string]interface{}{"metric": "m",
			"policy": map[string]interface{}{"duration": time.Hour, "resolution": time.Minute,
				"aggregation": "avg"}}, "metric"},
		{"list metrics", "ListMetrics", map[string]interface{}{"prefix": "cpu",
			"tags": map[string]string{"x": "y"}, "limit": 10, "cursor": "c"}, "prefix"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := h.transformRequest(tc.method, tc.req)
			if err != nil {
				t.Fatalf("transformRequest(%s): %v", tc.method, err)
			}
			if _, ok := args[tc.want]; !ok {
				t.Errorf("missing argument %q in %v", tc.want, args)
			}
		})
	}
}

func TestTimeseriesTransformRequestUnknownMethod(t *testing.T) {
	h := newTSHandler(t)
	// Unknown method falls through to generic conversion (no error).
	args, err := h.transformRequest("Bogus", &timeseries.WriteRequest{Metric: "m"})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}
	if args == nil {
		t.Error("expected non-nil args from generic conversion")
	}
}

func TestTimeseriesTransformWritePointInvalidType(t *testing.T) {
	h := newTSHandler(t)
	if _, err := h.transformWritePoint(42); err == nil {
		t.Error("expected error for non-WriteRequest, non-map input")
	}
}

func TestTimeseriesTransformQueryInvalidType(t *testing.T) {
	h := newTSHandler(t)
	if _, err := h.transformQuery(42); err == nil {
		t.Error("expected error for non-QueryRequest, non-map input")
	}
}

func TestTimeseriesQueryFromMapInt64Times(t *testing.T) {
	h := newTSHandler(t)
	args, err := h.transformQueryFromMap(map[string]interface{}{
		"metric":           "m",
		"start_time":       int64(100),
		"end_time":         int64(200),
		"tags":             map[string]string{"a": "b"},
		"interval_seconds": int64(60),
		"group_by":         []string{"host"},
		"limit":            5,
		"percentile":       0.95,
	})
	if err != nil {
		t.Fatalf("transformQueryFromMap: %v", err)
	}
	if args["start_time"] != int64(100) || args["end_time"] != int64(200) {
		t.Errorf("times = %v / %v", args["start_time"], args["end_time"])
	}
	if args["group_by"] != `["host"]` {
		t.Errorf("group_by = %v", args["group_by"])
	}
	if args["percentile"] != "0.95" {
		t.Errorf("percentile = %v", args["percentile"])
	}
}

func TestTimeseriesResponseTransforms(t *testing.T) {
	h := newTSHandler(t)
	ts := "2026-05-29T00:00:00Z"

	t.Run("write point", func(t *testing.T) {
		out, err := h.transformResponse("Write", map[string]interface{}{"success": true, "timestamp": ts})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if m["success"] != true {
			t.Errorf("success = %v", m["success"])
		}
	})

	t.Run("write batch", func(t *testing.T) {
		out, err := h.transformResponse("WriteBatch", map[string]interface{}{
			"success": true, "points_written": float64(3), "timestamp": ts})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if m["points_written"] != int64(3) {
			t.Errorf("points_written = %v", m["points_written"])
		}
	})

	t.Run("query", func(t *testing.T) {
		out, err := h.transformResponse("Query", map[string]interface{}{
			"points": []interface{}{
				map[string]interface{}{"timestamp": ts, "value": 1.5},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp := out.(*timeseries.QueryResponse)
		if len(resp.Series) != 1 || resp.Series[0].Value != 1.5 {
			t.Errorf("series = %+v", resp.Series)
		}
	})

	t.Run("delete", func(t *testing.T) {
		out, err := h.transformResponse("DeleteSeries", map[string]interface{}{"success": true, "timestamp": ts})
		if err != nil {
			t.Fatal(err)
		}
		if out.(map[string]interface{})["success"] != true {
			t.Error("expected success")
		}
	})

	t.Run("get retention", func(t *testing.T) {
		out, err := h.transformResponse("GetRetentionPolicy", map[string]interface{}{
			"metric": "m", "duration_seconds": float64(3600), "resolution_seconds": float64(60),
			"aggregation": "avg", "timestamp": ts})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		if m["metric"] != "m" {
			t.Errorf("metric = %v", m["metric"])
		}
	})

	t.Run("set retention", func(t *testing.T) {
		out, err := h.transformResponse("SetRetentionPolicy", map[string]interface{}{"success": true})
		if err != nil {
			t.Fatal(err)
		}
		if out.(map[string]interface{})["success"] != true {
			t.Error("expected success")
		}
	})

	t.Run("list metrics", func(t *testing.T) {
		out, err := h.transformResponse("ListMetrics", map[string]interface{}{
			"metrics": []interface{}{"a", "b"}, "next_cursor": "c", "timestamp": ts})
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]interface{})
		metrics := m["metrics"].([]string)
		if len(metrics) != 2 || m["next_cursor"] != "c" {
			t.Errorf("list = %v", m)
		}
	})

	t.Run("unknown method passthrough", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformResponse("Bogus", in)
		if err != nil {
			t.Fatal(err)
		}
		if out.(map[string]interface{})["x"] != 1 {
			t.Error("expected passthrough")
		}
	})
}

func TestTimeseriesResponseNilAndNonMap(t *testing.T) {
	h := newTSHandler(t)
	methods := []string{"Write", "WriteBatch", "Query", "DeleteSeries",
		"GetRetentionPolicy", "SetRetentionPolicy", "ListMetrics"}
	for _, m := range methods {
		if _, err := h.transformResponse(m, nil); err != nil {
			t.Errorf("%s nil response: %v", m, err)
		}
		// Non-map response passes through unchanged.
		out, err := h.transformResponse(m, "scalar")
		if err != nil {
			t.Errorf("%s scalar response: %v", m, err)
		}
		if m == "GetRetentionPolicy" {
			// passthrough
		} else if out == nil && m != "Query" {
			t.Errorf("%s scalar produced nil", m)
		}
	}
}

func TestTimeseriesTsGetTimeFormats(t *testing.T) {
	rfc3339 := tsGetTime(map[string]interface{}{"t": "2026-05-29T00:00:00Z"}, "t")
	if rfc3339.IsZero() {
		t.Error("RFC3339 parse failed")
	}
	nano := tsGetTime(map[string]interface{}{"t": "2026-05-29T00:00:00.5Z"}, "t")
	if nano.IsZero() {
		t.Error("RFC3339Nano parse failed")
	}
	unix := tsGetTime(map[string]interface{}{"t": float64(1_000_000_000)}, "t")
	if unix.IsZero() {
		t.Error("unix nano parse failed")
	}
	if !tsGetTime(map[string]interface{}{"t": "garbage"}, "t").IsZero() {
		t.Error("garbage should yield zero time")
	}
	if !tsGetTime(map[string]interface{}{}, "missing").IsZero() {
		t.Error("missing key should yield zero time")
	}
}
