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

package mcp

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestMapTimeseriesMethodToTool(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		expected string
	}{
		{
			name:     "SDK Write method",
			method:   "Write",
			expected: "qrdb_ts_write_point",
		},
		{
			name:     "SDK Query method",
			method:   "Query",
			expected: "qrdb_ts_query",
		},
		{
			name:     "CLI TSWritePoint method",
			method:   "TSWritePoint",
			expected: "qrdb_ts_write_point",
		},
		{
			name:     "CLI TSWriteBatch method",
			method:   "TSWriteBatch",
			expected: "qrdb_ts_write_batch",
		},
		{
			name:     "CLI TSQuery method",
			method:   "TSQuery",
			expected: "qrdb_ts_query",
		},
		{
			name:     "CLI TSDeleteSeries method",
			method:   "TSDeleteSeries",
			expected: "qrdb_ts_delete_series",
		},
		{
			name:     "CLI TSGetRetentionPolicy method",
			method:   "TSGetRetentionPolicy",
			expected: "qrdb_ts_get_retention_policy",
		},
		{
			name:     "CLI TSSetRetentionPolicy method",
			method:   "TSSetRetentionPolicy",
			expected: "qrdb_ts_set_retention_policy",
		},
		{
			name:     "CLI TSListMetrics method",
			method:   "TSListMetrics",
			expected: "qrdb_ts_list_metrics",
		},
		{
			name:     "Alternative WritePoint method",
			method:   "WritePoint",
			expected: "qrdb_ts_write_point",
		},
		{
			name:     "Alternative WriteBatch method",
			method:   "WriteBatch",
			expected: "qrdb_ts_write_batch",
		},
		{
			name:     "Unknown method fallback",
			method:   "UnknownMethod",
			expected: "quicbaas_ts_unknown_method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapTimeseriesMethodToTool(tt.method)
			if result != tt.expected {
				t.Errorf("mapTimeseriesMethodToTool(%q) = %q, want %q", tt.method, result, tt.expected)
			}
		})
	}
}

func TestIsMCPTimeseriesService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "quicbaas.TimeSeries service",
			service:  "quicbaas.TimeSeries",
			expected: true,
		},
		{
			name:     "quicbaas.Timeseries service",
			service:  "quicbaas.Timeseries",
			expected: true,
		},
		{
			name:     "TimeSeries only",
			service:  "TimeSeries",
			expected: true,
		},
		{
			name:     "Timeseries only",
			service:  "Timeseries",
			expected: true,
		},
		{
			name:     "quicbaas.KV service",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "quicbaas.Vector service",
			service:  "quicbaas.Vector",
			expected: false,
		},
		{
			name:     "empty string",
			service:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMCPTimeseriesService(tt.service)
			if result != tt.expected {
				t.Errorf("isMCPTimeseriesService(%q) = %v, want %v", tt.service, result, tt.expected)
			}
		})
	}
}

func TestTimeseriesHandlerTransformWritePoint(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8080",
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer tr.Close()

	handler := newTimeseriesHandler(tr)

	now := time.Now()
	req := &timeseries.WriteRequest{
		Metric:    "cpu.usage",
		Value:     75.5,
		Timestamp: now,
		Tags: map[string]string{
			"host": "server1",
			"dc":   "us-east",
		},
	}

	args, err := handler.transformWritePoint(req)
	if err != nil {
		t.Fatalf("transformWritePoint failed: %v", err)
	}

	if args["metric"] != "cpu.usage" {
		t.Errorf("metric = %v, want cpu.usage", args["metric"])
	}

	if args["value"] != "75.5" {
		t.Errorf("value = %v, want 75.5", args["value"])
	}

	if args["timestamp"] != now.UnixNano() {
		t.Errorf("timestamp = %v, want %v", args["timestamp"], now.UnixNano())
	}

	if args["tags"] == nil {
		t.Error("tags should not be nil")
	}
}

func TestTimeseriesHandlerTransformQuery(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8080",
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer tr.Close()

	handler := newTimeseriesHandler(tr)

	start := time.Now().Add(-time.Hour)
	end := time.Now()
	req := &timeseries.QueryRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "server1"},
		Start:     start,
		End:       end,
		Aggregate: timeseries.AggregateAvg,
		Interval:  time.Minute,
	}

	args, err := handler.transformQuery(req)
	if err != nil {
		t.Fatalf("transformQuery failed: %v", err)
	}

	if args["metric"] != "cpu.usage" {
		t.Errorf("metric = %v, want cpu.usage", args["metric"])
	}

	if args["start_time"] != start.UnixNano() {
		t.Errorf("start_time = %v, want %v", args["start_time"], start.UnixNano())
	}

	if args["end_time"] != end.UnixNano() {
		t.Errorf("end_time = %v, want %v", args["end_time"], end.UnixNano())
	}

	if args["aggregation"] != "avg" {
		t.Errorf("aggregation = %v, want avg", args["aggregation"])
	}

	if args["interval_seconds"] != int64(60) {
		t.Errorf("interval_seconds = %v, want 60", args["interval_seconds"])
	}
}

func TestFormatFloatValue(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		expected string
	}{
		{
			name:     "integer value",
			value:    42,
			expected: "42",
		},
		{
			name:     "decimal value",
			value:    3.14,
			expected: "3.14",
		},
		{
			name:     "negative value",
			value:    -100.5,
			expected: "-100.5",
		},
		{
			name:     "zero",
			value:    0,
			expected: "0",
		},
		{
			name:     "small decimal",
			value:    0.001,
			expected: "0.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatFloatValue(tt.value)
			if result != tt.expected {
				t.Errorf("formatFloatValue(%v) = %q, want %q", tt.value, result, tt.expected)
			}
		})
	}
}

func TestTsGetHelpers(t *testing.T) {
	testMap := map[string]interface{}{
		"string_key": "test_value",
		"bool_key":   true,
		"int_key":    float64(42),
		"float_key":  float64(3.14),
	}

	t.Run("tsGetString", func(t *testing.T) {
		if got := tsGetString(testMap, "string_key"); got != "test_value" {
			t.Errorf("tsGetString() = %v, want test_value", got)
		}
		if got := tsGetString(testMap, "missing"); got != "" {
			t.Errorf("tsGetString() for missing key = %v, want empty string", got)
		}
	})

	t.Run("tsGetBool", func(t *testing.T) {
		if got := tsGetBool(testMap, "bool_key"); !got {
			t.Error("tsGetBool() = false, want true")
		}
		if got := tsGetBool(testMap, "missing"); got {
			t.Error("tsGetBool() for missing key = true, want false")
		}
	})

	t.Run("tsGetInt64", func(t *testing.T) {
		if got := tsGetInt64(testMap, "int_key"); got != 42 {
			t.Errorf("tsGetInt64() = %v, want 42", got)
		}
		if got := tsGetInt64(testMap, "missing"); got != 0 {
			t.Errorf("tsGetInt64() for missing key = %v, want 0", got)
		}
	})

	t.Run("tsGetFloat64", func(t *testing.T) {
		if got := tsGetFloat64(testMap, "float_key"); got != 3.14 {
			t.Errorf("tsGetFloat64() = %v, want 3.14", got)
		}
		if got := tsGetFloat64(testMap, "missing"); got != 0 {
			t.Errorf("tsGetFloat64() for missing key = %v, want 0", got)
		}
	})
}

func TestTimeseriesHandlerTransformResponses(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8080",
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer tr.Close()

	handler := newTimeseriesHandler(tr)

	t.Run("transformWritePointResponse nil", func(t *testing.T) {
		resp, err := handler.transformWritePointResponse(nil)
		if err != nil {
			t.Fatalf("transformWritePointResponse failed: %v", err)
		}
		m := resp.(map[string]interface{})
		if !m["success"].(bool) {
			t.Error("expected success to be true")
		}
	})

	t.Run("transformQueryResponse nil", func(t *testing.T) {
		resp, err := handler.transformQueryResponse(nil)
		if err != nil {
			t.Fatalf("transformQueryResponse failed: %v", err)
		}
		queryResp := resp.(*timeseries.QueryResponse)
		if len(queryResp.Series) != 0 {
			t.Errorf("expected empty series, got %d", len(queryResp.Series))
		}
	})

	t.Run("transformListMetricsResponse nil", func(t *testing.T) {
		resp, err := handler.transformListMetricsResponse(nil)
		if err != nil {
			t.Fatalf("transformListMetricsResponse failed: %v", err)
		}
		m := resp.(map[string]interface{})
		if m["metrics"] == nil {
			t.Error("expected metrics to be present")
		}
	})
}
