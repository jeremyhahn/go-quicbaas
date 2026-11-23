// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
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

// MCP's retention tool takes whole seconds: a sub-second duration or
// resolution is refused before the tool is called, never truncated.
func TestMCPQRDBTimeSeriesSetRetentionRejectsSubsecond(t *testing.T) {
	url, calls := newQTSServer(t, []string{"qrdb_ts_set_retention_policy"},
		func(string, map[string]interface{}) string { return `{"success":true}` })
	tr, err := New(&transport.Config{Address: url, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	c := tr.QRDBTimeSeries()
	ctx := context.Background()

	refused := []struct {
		name   string
		policy *qrdbtransport.TSRetentionPolicy
	}{
		{"subsecond duration", &qrdbtransport.TSRetentionPolicy{DurationNs: int64(500 * time.Millisecond)}},
		{"fractional resolution", &qrdbtransport.TSRetentionPolicy{DurationNs: int64(time.Hour), ResolutionNs: int64(1500 * time.Millisecond)}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			err := c.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{Metric: "cpu", Policy: tc.policy})
			if !errors.IsInvalidInput(err) {
				t.Fatalf("TSSetRetention error = %v, want InvalidInput", err)
			}
			if n := len(calls()); n != 0 {
				t.Errorf("calls = %d, want 0", n)
			}
		})
	}

	t.Run("whole seconds", func(t *testing.T) {
		err := c.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{Metric: "cpu",
			Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: int64(2 * time.Hour), ResolutionNs: int64(time.Minute)}})
		if err != nil {
			t.Fatalf("TSSetRetention: %v", err)
		}
		got := calls()
		if len(got) != 1 || got[0].tool != "qrdb_ts_set_retention_policy" {
			t.Fatalf("calls = %+v", got)
		}
		if got[0].args["duration_seconds"] != float64(7200) || got[0].args["resolution_seconds"] != float64(60) {
			t.Errorf("args = %v", got[0].args)
		}
	})
}

// newQTSErrServer starts an MCP test server whose tools all answer with a
// tool error carrying msg.
func newQTSErrServer(t *testing.T, tools []string, msg string) string {
	t.Helper()
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	handler := func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError(msg), nil
	}
	for _, name := range tools {
		srv.AddTool(mcpgo.NewTool(name), handler)
	}
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL
}

// TestMCPQRDBTimeSeriesClientRemainingMethods pins the tool arguments, the
// decoding of native go-qrdb responses and the error propagation of the MCP
// adapter's batch write, point write, rich query, series delete and
// retention methods.
func TestMCPQRDBTimeSeriesClientRemainingMethods(t *testing.T) {
	ts := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	newClient := func(t *testing.T, url string) *QRDBTimeSeriesClient {
		t.Helper()
		tr, err := New(&transport.Config{Address: url, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = tr.Close() })
		return tr.QRDBTimeSeries()
	}
	ok := func(string, map[string]interface{}) string { return "ok" }

	t.Run("TSWriteBatch sends points as JSON", func(t *testing.T) {
		url, calls := newQTSServer(t, []string{"qrdb_ts_write_batch"}, ok)
		c := newClient(t, url)
		err := c.TSWriteBatch(ctx, []qrdbtransport.TSDataPoint{
			{Metric: "cpu", Value: 1.5, TimestampNs: ts.UnixNano(), Tags: []qrdbtransport.TSTag{{Key: "h", Value: "a"}}},
			{Metric: "cpu", Value: 2.5, TimestampNs: ts.Add(time.Second).UnixNano()},
		})
		if err != nil {
			t.Fatalf("TSWriteBatch: %v", err)
		}
		got := calls()
		if len(got) != 1 || got[0].tool != "qrdb_ts_write_batch" {
			t.Fatalf("calls = %+v, want one qrdb_ts_write_batch", got)
		}
		raw, isStr := got[0].args["points"].(string)
		if !isStr {
			t.Fatalf("points arg = %T, want a JSON string", got[0].args["points"])
		}
		var points []struct {
			Metric    string            `json:"metric"`
			Value     float64           `json:"value"`
			Timestamp int64             `json:"timestamp"`
			Tags      map[string]string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(raw), &points); err != nil {
			t.Fatalf("decode points: %v", err)
		}
		if len(points) != 2 {
			t.Fatalf("points = %+v, want 2", points)
		}
		if p := points[0]; p.Metric != "cpu" || p.Value != 1.5 || p.Timestamp != ts.UnixNano() || p.Tags["h"] != "a" {
			t.Errorf("first point = %+v", p)
		}
		if p := points[1]; p.Metric != "cpu" || p.Value != 2.5 || p.Timestamp != ts.Add(time.Second).UnixNano() || len(p.Tags) != 0 {
			t.Errorf("second point = %+v", p)
		}
	})

	t.Run("TSWritePoint sends metric value timestamp tags", func(t *testing.T) {
		url, calls := newQTSServer(t, []string{"qrdb_ts_write_point"}, ok)
		c := newClient(t, url)
		if err := c.TSWritePoint(ctx, &qrdbtransport.TSDataPoint{Metric: "cpu", Value: 3.25, TimestampNs: ts.UnixNano(),
			Tags: []qrdbtransport.TSTag{{Key: "h", Value: "b"}}}); err != nil {
			t.Fatalf("TSWritePoint: %v", err)
		}
		got := calls()
		if len(got) != 1 || got[0].tool != "qrdb_ts_write_point" {
			t.Fatalf("calls = %+v, want one qrdb_ts_write_point", got)
		}
		a := got[0].args
		if a["metric"] != "cpu" || a["value"] != 3.25 || a["timestamp"] != float64(ts.UnixNano()) || a["tags"] != `{"h":"b"}` {
			t.Errorf("TSWritePoint args = %v", a)
		}
	})

	t.Run("TSRichQuery sends options and decodes grouped results", func(t *testing.T) {
		url, calls := newQTSServer(t, []string{"qrdb_ts_rich_query"}, func(string, map[string]interface{}) string {
			return string(mustMarshalJSON(map[string]interface{}{
				"metric": "cpu",
				"tags":   map[string]string{"dc": "x"},
				"points": []map[string]interface{}{
					{"metric": "cpu", "value": 2, "timestamp": ts.Format(time.RFC3339Nano), "tags": map[string]string{"host": "a"}},
				},
				"aggregated_value": 7.5,
				"grouped_results": []map[string]interface{}{
					{
						"tags":             map[string]string{"host": "a"},
						"points":           []map[string]interface{}{{"metric": "cpu", "value": 2, "timestamp": ts.Format(time.RFC3339Nano)}},
						"aggregated_value": 2,
					},
					{
						"tags":             map[string]string{"host": "b"},
						"points":           []map[string]interface{}{},
						"aggregated_value": 5.5,
					},
				},
			}))
		})
		c := newClient(t, url)
		resp, err := c.TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{
			Metric: "cpu", StartNs: 1, EndNs: ts.UnixNano(),
			Tags:        []qrdbtransport.TSTag{{Key: "dc", Value: "x"}},
			Aggregation: qrdbtransport.AggMax, IntervalNs: int64(time.Minute),
			GroupBy: []string{"host"}, Limit: 10, Percentile: 0.95, Consistency: "linearizable",
		})
		if err != nil {
			t.Fatalf("TSRichQuery: %v", err)
		}
		got := calls()
		if len(got) != 1 || got[0].tool != "qrdb_ts_rich_query" {
			t.Fatalf("calls = %+v, want one qrdb_ts_rich_query", got)
		}
		a := got[0].args
		want := map[string]interface{}{
			"metric": "cpu", "start": float64(1), "end": float64(ts.UnixNano()), "tags": `{"dc":"x"}`,
			"aggregation": "max", "interval_ns": float64(time.Minute), "group_by": `["host"]`,
			"limit": float64(10), "percentile": 0.95, "consistency": "linearizable",
		}
		for k, v := range want {
			if a[k] != v {
				t.Errorf("TSRichQuery arg %s = %v (%T), want %v", k, a[k], a[k], v)
			}
		}

		if resp.Metric != "cpu" {
			t.Errorf("Metric = %q, want cpu", resp.Metric)
		}
		if len(resp.Points) != 1 || resp.Points[0].Value != 2 || resp.Points[0].TimestampNs != ts.UnixNano() ||
			len(resp.Points[0].Tags) != 1 || resp.Points[0].Tags[0] != (qrdbtransport.TSTag{Key: "host", Value: "a"}) {
			t.Errorf("Points = %+v", resp.Points)
		}
		if resp.AggregatedValue == nil || *resp.AggregatedValue != 7.5 {
			t.Errorf("AggregatedValue = %v, want 7.5", resp.AggregatedValue)
		}
		if len(resp.GroupedResults) != 2 {
			t.Fatalf("GroupedResults = %+v, want 2", resp.GroupedResults)
		}
		g0, g1 := resp.GroupedResults[0], resp.GroupedResults[1]
		if len(g0.Tags) != 1 || g0.Tags[0] != (qrdbtransport.TSTag{Key: "host", Value: "a"}) || len(g0.Points) != 1 ||
			g0.AggregatedValue == nil || *g0.AggregatedValue != 2 {
			t.Errorf("first group = %+v", g0)
		}
		if len(g1.Tags) != 1 || g1.Tags[0] != (qrdbtransport.TSTag{Key: "host", Value: "b"}) || len(g1.Points) != 0 ||
			g1.AggregatedValue == nil || *g1.AggregatedValue != 5.5 {
			t.Errorf("second group = %+v", g1)
		}

		if _, err := c.TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 2}); err != nil {
			t.Fatalf("plain TSRichQuery: %v", err)
		}
		got = calls()
		if len(got) != 2 {
			t.Fatalf("calls = %d, want 2", len(got))
		}
		for _, k := range []string{"aggregation", "interval_ns", "group_by", "limit", "percentile", "consistency"} {
			if _, present := got[1].args[k]; present {
				t.Errorf("plain TSRichQuery sent %s = %v", k, got[1].args[k])
			}
		}
	})

	t.Run("TSDeleteSeries sends tags and only non-zero bounds", func(t *testing.T) {
		url, calls := newQTSServer(t, []string{"qrdb_ts_delete_series"}, ok)
		c := newClient(t, url)
		resp, err := c.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "cpu",
			Tags: []qrdbtransport.TSTag{{Key: "h", Value: "a"}}, StartNs: 5, EndNs: 9})
		if err != nil {
			t.Fatalf("TSDeleteSeries: %v", err)
		}
		if resp == nil {
			t.Error("TSDeleteSeries returned a nil response")
		}
		if _, err := c.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "cpu"}); err != nil {
			t.Fatalf("plain TSDeleteSeries: %v", err)
		}
		got := calls()
		if len(got) != 2 {
			t.Fatalf("calls = %+v, want 2", got)
		}
		a := got[0].args
		if a["metric"] != "cpu" || a["tags"] != `{"h":"a"}` || a["start"] != float64(5) || a["end"] != float64(9) {
			t.Errorf("TSDeleteSeries args = %v", a)
		}
		for _, k := range []string{"start", "end", "tags"} {
			if _, present := got[1].args[k]; present {
				t.Errorf("plain TSDeleteSeries sent %s = %v", k, got[1].args[k])
			}
		}
	})

	t.Run("retention set then get round-trips", func(t *testing.T) {
		var (
			mu      sync.Mutex
			setArgs map[string]interface{}
		)
		aggByName := map[string]qrdbtimeseries.AggFunc{"avg": qrdbtimeseries.AggAvg, "max": qrdbtimeseries.AggMax}
		url, calls := newQTSServer(t, []string{"qrdb_ts_set_retention_policy", "qrdb_ts_get_retention_policy"},
			func(tool string, args map[string]interface{}) string {
				mu.Lock()
				defer mu.Unlock()
				if tool == "qrdb_ts_set_retention_policy" {
					setArgs = args
					return "ok"
				}
				dur, _ := setArgs["duration_seconds"].(float64)
				res, _ := setArgs["resolution_seconds"].(float64)
				name, _ := setArgs["aggregation"].(string)
				return string(mustMarshalJSON(&qrdbtimeseries.RetentionPolicy{
					Duration:    time.Duration(dur) * time.Second,
					Resolution:  time.Duration(res) * time.Second,
					Aggregation: aggByName[name],
				}))
			})
		c := newClient(t, url)
		if err := c.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{Metric: "cpu", Policy: &qrdbtransport.TSRetentionPolicy{
			DurationNs: int64(48 * time.Hour), ResolutionNs: int64(5 * time.Minute), Aggregation: qrdbtransport.AggAvg,
		}}); err != nil {
			t.Fatalf("TSSetRetention: %v", err)
		}
		mu.Lock()
		sa := setArgs
		mu.Unlock()
		if sa["duration_seconds"] != float64(172800) || sa["resolution_seconds"] != float64(300) || sa["aggregation"] != "avg" {
			t.Errorf("TSSetRetention args = %v", sa)
		}

		got, err := c.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
		if err != nil {
			t.Fatalf("TSGetRetention: %v", err)
		}
		cs := calls()
		if len(cs) != 2 || cs[1].tool != "qrdb_ts_get_retention_policy" || cs[1].args["metric"] != "cpu" {
			t.Errorf("calls = %+v", cs)
		}
		if got.Policy == nil {
			t.Fatal("TSGetRetention returned a nil policy")
		}
		if got.Policy.DurationNs != int64(48*time.Hour) || got.Policy.ResolutionNs != int64(5*time.Minute) ||
			got.Policy.Aggregation != qrdbtransport.AggAvg {
			t.Errorf("TSGetRetention policy = %+v", got.Policy)
		}
	})

	t.Run("tool errors propagate", func(t *testing.T) {
		const msg = "qrdb: series cpu not found"
		url := newQTSErrServer(t, []string{
			"qrdb_ts_write_batch", "qrdb_ts_write_point", "qrdb_ts_rich_query",
			"qrdb_ts_delete_series", "qrdb_ts_get_retention_policy",
		}, msg)
		c := newClient(t, url)
		point := qrdbtransport.TSDataPoint{Metric: "cpu", Value: 1, TimestampNs: ts.UnixNano()}
		cases := []struct {
			name string
			call func() (resp interface{}, err error)
		}{
			{"TSWriteBatch", func() (interface{}, error) {
				return nil, c.TSWriteBatch(ctx, []qrdbtransport.TSDataPoint{point})
			}},
			{"TSWritePoint", func() (interface{}, error) { return nil, c.TSWritePoint(ctx, &point) }},
			{"TSRichQuery", func() (interface{}, error) {
				r, err := c.TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 2})
				if r == nil {
					return nil, err
				}
				return r, err
			}},
			{"TSDeleteSeries", func() (interface{}, error) {
				r, err := c.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "cpu"})
				if r == nil {
					return nil, err
				}
				return r, err
			}},
			{"TSGetRetention", func() (interface{}, error) {
				r, err := c.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
				if r == nil {
					return nil, err
				}
				return r, err
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				resp, err := tc.call()
				if err == nil || !strings.Contains(err.Error(), msg) {
					t.Errorf("%s error = %v, want one containing %q", tc.name, err, msg)
				}
				if resp != nil {
					t.Errorf("%s response = %+v, want nil", tc.name, resp)
				}
			})
		}
	})
}
