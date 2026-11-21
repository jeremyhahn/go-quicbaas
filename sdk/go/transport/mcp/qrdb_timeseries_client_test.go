// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

type qtsAuthKey struct{}

type qtsCall struct {
	tool string
	args map[string]interface{}
	auth string
}

// newQTSServer starts an MCP test server whose tools answer with the text
// fn returns and records every call with the HTTP Authorization header.
func newQTSServer(t *testing.T, tools []string, fn func(tool string, args map[string]interface{}) string) (string, func() []qtsCall) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []qtsCall
	)
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	handler := func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		auth, _ := ctx.Value(qtsAuthKey{}).(string)
		mu.Lock()
		calls = append(calls, qtsCall{tool: req.Params.Name, args: req.GetArguments(), auth: auth})
		mu.Unlock()
		return mcpgo.NewToolResultText(fn(req.Params.Name, req.GetArguments())), nil
	}
	for _, name := range tools {
		srv.AddTool(mcpgo.NewTool(name), handler)
	}
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv,
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, qtsAuthKey{}, r.Header.Get("Authorization"))
		}),
	)
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL, func() []qtsCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]qtsCall(nil), calls...)
	}
}

// TestMCPTimeseriesAggregateQueryUsesRichQuery drives an aggregated Query
// through the transport's request path: it must reach qrdb_ts_rich_query
// with interval_ns, and the decoded response must carry the downsampled
// buckets, not the raw samples. A raw Query stays on qrdb_ts_query.
func TestMCPTimeseriesAggregateQueryUsesRichQuery(t *testing.T) {
	base := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	raw := []float64{1, 2, 3, 10, 20, 30} // three samples per one-minute bucket

	url, calls := newQTSServer(t, []string{"qrdb_ts_query", "qrdb_ts_rich_query"},
		func(tool string, args map[string]interface{}) string {
			if tool == "qrdb_ts_query" {
				pts := make([]map[string]interface{}, len(raw))
				for i, v := range raw {
					pts[i] = map[string]interface{}{"metric": "cpu", "value": v,
						"timestamp": base.Add(time.Duration(i) * 20 * time.Second).Format(time.RFC3339Nano)}
				}
				return string(mustMarshalJSON(pts))
			}
			// Server-side downsampling of the six raw points into two
			// one-minute buckets of three.
			interval := time.Duration(int64(args["interval_ns"].(float64)))
			buckets := []map[string]interface{}{}
			for b := 0; b < len(raw)/3; b++ {
				sum := 0.0
				for _, v := range raw[b*3 : b*3+3] {
					sum += v
				}
				buckets = append(buckets, map[string]interface{}{"metric": "cpu", "value": sum / 3,
					"timestamp": base.Add(time.Duration(b) * interval).Format(time.RFC3339Nano)})
			}
			return string(mustMarshalJSON(map[string]interface{}{"metric": "cpu", "points": buckets}))
		})

	tr, err := New(&transport.Config{Address: url, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx := context.Background()
	out, err := tr.Request(ctx, "quicbaas.TimeSeries", "Query", &timeseries.QueryRequest{
		Metric: "cpu", Start: base, End: base.Add(2 * time.Minute),
		Aggregate: timeseries.AggregateAvg, Interval: time.Minute,
	})
	if err != nil {
		t.Fatalf("aggregate Query: %v", err)
	}
	got := calls()
	if len(got) != 1 || got[0].tool != "qrdb_ts_rich_query" {
		t.Fatalf("aggregate Query calls = %+v, want one qrdb_ts_rich_query", got)
	}
	if got[0].args["aggregation"] != "avg" {
		t.Errorf("aggregation = %v, want avg", got[0].args["aggregation"])
	}
	if got[0].args["interval_ns"] != float64(time.Minute) {
		t.Errorf("interval_ns = %v, want %d", got[0].args["interval_ns"], int64(time.Minute))
	}
	if _, ok := got[0].args["interval_seconds"]; ok {
		t.Errorf("interval_seconds must not be sent: %v", got[0].args)
	}
	resp, ok := out.(*timeseries.QueryResponse)
	if !ok {
		t.Fatalf("response type = %T", out)
	}
	if len(resp.Series) != 2 || resp.Series[0].Value != 2 || resp.Series[1].Value != 20 {
		t.Fatalf("series = %+v, want the two bucket averages 2 and 20", resp.Series)
	}

	out, err = tr.Request(ctx, "quicbaas.TimeSeries", "Query", &timeseries.QueryRequest{
		Metric: "cpu", Start: base, End: base.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("raw Query: %v", err)
	}
	got = calls()
	if len(got) != 2 || got[1].tool != "qrdb_ts_query" {
		t.Fatalf("raw Query calls = %+v, want qrdb_ts_query second", got)
	}
	if resp := out.(*timeseries.QueryResponse); len(resp.Series) != len(raw) {
		t.Errorf("raw series = %d points, want %d", len(resp.Series), len(raw))
	}
}

// TestMCPQRDBTimeSeriesClientRoundTripsWithBearer drives the go-qrdb
// TimeSeriesClient served over the quicbaas MCP transport: every qrdb_ts_*
// call must carry the transport's MCP token.
func TestMCPQRDBTimeSeriesClientRoundTripsWithBearer(t *testing.T) {
	const token = "mcp-tok-2500"
	ts := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

	url, calls := newQTSServer(t,
		[]string{"qrdb_ts_write", "qrdb_ts_query", "qrdb_ts_aggregate", "qrdb_ts_list_metrics"},
		func(tool string, _ map[string]interface{}) string {
			switch tool {
			case "qrdb_ts_query":
				return string(mustMarshalJSON([]map[string]interface{}{
					{"metric": "cpu", "value": 1.5, "timestamp": ts.Format(time.RFC3339Nano), "tags": map[string]string{"h": "a"}},
				}))
			case "qrdb_ts_aggregate":
				return string(mustMarshalJSON([]map[string]interface{}{
					{"Timestamp": ts.Format(time.RFC3339Nano), "Value": 4.5, "Count": 3},
				}))
			case "qrdb_ts_list_metrics":
				return `["cpu","mem"]`
			}
			return `{"success":true}`
		})

	// The quicbaas client installs WithMCPAuthToken (or WithAuthToken) this way.
	tr, err := New(&transport.Config{Address: url, Timeout: 5 * time.Second}, WithAuthToken(token))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	var c qrdbtransport.TimeSeriesClient = tr.QRDBTimeSeries()
	ctx := context.Background()

	if err := c.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "cpu", Value: 1.5, TimestampNs: ts.UnixNano(),
		Tags: []qrdbtransport.TSTag{{Key: "h", Value: "a"}}}); err != nil {
		t.Fatalf("TSWrite: %v", err)
	}
	q, err := c.TSQuery(ctx, &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 1, EndNs: ts.UnixNano() + 1, Limit: 5})
	if err != nil {
		t.Fatalf("TSQuery: %v", err)
	}
	if len(q.Points) != 1 || q.Points[0].Value != 1.5 || q.Points[0].TimestampNs != ts.UnixNano() ||
		len(q.Points[0].Tags) != 1 || q.Points[0].Tags[0].Value != "a" {
		t.Errorf("TSQuery points = %+v", q.Points)
	}
	a, err := c.TSAggregate(ctx, &qrdbtransport.TSAggregateRequest{Metric: "cpu", StartNs: 1, EndNs: 2,
		Function: qrdbtransport.AggAvg, IntervalNs: int64(time.Minute)})
	if err != nil {
		t.Fatalf("TSAggregate: %v", err)
	}
	if len(a.Results) != 1 || a.Results[0].Value != 4.5 || a.Results[0].Count != 3 || a.Results[0].TimestampNs != ts.UnixNano() {
		t.Errorf("TSAggregate results = %+v", a.Results)
	}
	m, err := c.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{Prefix: "c"})
	if err != nil {
		t.Fatalf("TSListMetrics: %v", err)
	}
	if len(m.Metrics) != 2 || m.Metrics[0] != "cpu" {
		t.Errorf("TSListMetrics = %v", m.Metrics)
	}

	got := calls()
	want := []string{"qrdb_ts_write", "qrdb_ts_query", "qrdb_ts_aggregate", "qrdb_ts_list_metrics"}
	if len(got) != len(want) {
		t.Fatalf("calls = %+v, want %v", got, want)
	}
	for i, call := range got {
		if call.tool != want[i] {
			t.Errorf("call %d tool = %s, want %s", i, call.tool, want[i])
		}
		if call.auth != "Bearer "+token {
			t.Errorf("%s Authorization = %q, want %q", call.tool, call.auth, "Bearer "+token)
		}
	}
	if got[0].args["tags"] != `{"h":"a"}` || got[0].args["timestamp"] != float64(ts.UnixNano()) {
		t.Errorf("TSWrite args = %v", got[0].args)
	}
	if got[2].args["func"] != "avg" || got[2].args["interval_ns"] != float64(time.Minute) {
		t.Errorf("TSAggregate args = %v", got[2].args)
	}

	// A closed transport refuses without reaching the server.
	_ = tr.Close()
	if _, err := c.TSQuery(ctx, &qrdbtransport.TSQueryRequest{Metric: "cpu"}); err == nil {
		t.Error("TSQuery after Close succeeded")
	}
	if n := len(calls()); n != len(want) {
		t.Errorf("calls after Close = %d, want %d", n, len(want))
	}
}
