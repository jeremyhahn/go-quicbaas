package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// tsWireCall is one request the wire-contract server recorded.
type tsWireCall struct {
	method, path, query, body, auth string
}

// TestQRDBTimeSeriesClient_WireContract pins the adapter to go-qrdb's own
// time-series REST contract (sdk/go transport/rest/transport_timeseries.go):
// per verb the HTTP method, path, query, request body and decoded response,
// and the platform bearer on every request.
func TestQRDBTimeSeriesClient_WireContract(t *testing.T) {
	const token = "t-2500"
	agg := 2.5
	tests := []struct {
		name      string
		status    int
		reply     string
		call      func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error)
		wantErr   bool
		method    string
		path      string
		query     string
		body      string // JSON, compared structurally; "" means no body
		wantValue any
	}{
		{
			name:  "TSWrite",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return nil, c.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "cpu", Value: 1.5, TimestampNs: 10,
					Tags: []qrdbtransport.TSTag{{Key: "host", Value: "a"}}})
			},
			method: http.MethodPost, path: "/ts/write",
			body: `{"metric":"cpu","value":1.5,"timestamp":10,"tags":{"host":"a"}}`,
		},
		{
			name:  "TSWriteBatch",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return nil, c.TSWriteBatch(ctx, []qrdbtransport.TSDataPoint{
					{Metric: "cpu", Value: 1, TimestampNs: 1}, {Metric: "cpu", Value: 2, TimestampNs: 2},
				})
			},
			method: http.MethodPost, path: "/ts/write-batch",
			body: `{"points":[{"metric":"cpu","value":1,"timestamp":1},{"metric":"cpu","value":2,"timestamp":2}]}`,
		},
		{
			name:  "TSQuery",
			reply: `{"points":[{"metric":"cpu","value":3,"timestamp":7,"tags":{"host":"a"}}]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSQuery(ctx, &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 9,
					Tags:  []qrdbtransport.TSTag{{Key: "host", Value: "a"}, {Key: "dc", Value: "x"}},
					Limit: 5, Descending: true, Consistency: "strong"})
			},
			method: http.MethodGet, path: "/ts/query",
			query: "consistency=strong&descending=true&end_ns=9&limit=5&metric=cpu&start_ns=1&tags=host%3Da%2Cdc%3Dx",
			wantValue: &qrdbtransport.TSQueryResponse{Points: []qrdbtransport.TSDataPoint{
				{Metric: "cpu", Value: 3, TimestampNs: 7, Tags: []qrdbtransport.TSTag{{Key: "host", Value: "a"}}},
			}},
		},
		{
			name:  "TSAggregate",
			reply: `{"results":[{"timestamp":100,"value":4.5,"count":3}]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSAggregate(ctx, &qrdbtransport.TSAggregateRequest{Metric: "cpu", StartNs: 1, EndNs: 9,
					Function: qrdbtransport.AggAvg, IntervalNs: 60, Consistency: "eventual"})
			},
			method: http.MethodGet, path: "/ts/aggregate",
			query: "consistency=eventual&end_ns=9&function=avg&interval_ns=60&metric=cpu&start_ns=1",
			wantValue: &qrdbtransport.TSAggregateResponse{Results: []qrdbtransport.TSAggResult{
				{TimestampNs: 100, Value: 4.5, Count: 3},
			}},
		},
		{
			name:  "TSWritePoint",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return nil, c.TSWritePoint(ctx, &qrdbtransport.TSDataPoint{Metric: "cpu", Value: 2, TimestampNs: 5})
			},
			method: http.MethodPost, path: "/ts/write-point",
			body: `{"metric":"cpu","value":2,"timestamp":5}`,
		},
		{
			name: "TSRichQuery",
			reply: `{"metric":"cpu","tags":{"host":"a"},"points":[{"metric":"cpu","value":1,"timestamp":2}],` +
				`"aggregated_value":2.5,"grouped_results":[{"tags":{"dc":"x"},"points":[],"aggregated_value":2.5}]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 9,
					Tags:        []qrdbtransport.TSTag{{Key: "host", Value: "a"}},
					Aggregation: qrdbtransport.AggPercentile, IntervalNs: 60, GroupBy: []string{"dc"},
					Limit: 10, Percentile: 99, Consistency: "strong"})
			},
			method: http.MethodPost, path: "/ts/query-rich", query: "consistency=strong",
			body: `{"metric":"cpu","tags":{"host":"a"},"start_ns":1,"end_ns":9,"aggregation":"percentile",` +
				`"interval_ns":60,"group_by":["dc"],"limit":10,"percentile":99}`,
			wantValue: &qrdbtransport.TSRichQueryResponse{
				Metric:          "cpu",
				Tags:            []qrdbtransport.TSTag{{Key: "host", Value: "a"}},
				Points:          []qrdbtransport.TSDataPoint{{Metric: "cpu", Value: 1, TimestampNs: 2}},
				AggregatedValue: &agg,
				GroupedResults: []qrdbtransport.TSGroupedResult{
					{Tags: []qrdbtransport.TSTag{{Key: "dc", Value: "x"}}, Points: []qrdbtransport.TSDataPoint{}, AggregatedValue: &agg},
				},
			},
		},
		{
			name:  "TSRichQueryNoAggregation",
			reply: `{"metric":"cpu","points":[]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 9})
			},
			method: http.MethodPost, path: "/ts/query-rich",
			body:      `{"metric":"cpu","start_ns":1,"end_ns":9}`,
			wantValue: &qrdbtransport.TSRichQueryResponse{Metric: "cpu", Points: []qrdbtransport.TSDataPoint{}},
		},
		{
			name:  "TSDeleteSeries",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "a/b",
					Tags: []qrdbtransport.TSTag{{Key: "host", Value: "a"}}, StartNs: 3, EndNs: 4})
			},
			method: http.MethodDelete, path: "/ts/series/a%2Fb",
			query:     "end_ns=4&start_ns=3&tags=host%3Da",
			wantValue: &qrdbtransport.TSDeleteResponse{},
		},
		{
			name:  "TSDeleteSeriesNoQuery",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "cpu"})
			},
			method: http.MethodDelete, path: "/ts/series/cpu",
			wantValue: &qrdbtransport.TSDeleteResponse{},
		},
		{
			name:  "TSGetRetention",
			reply: `{"metric":"cpu","policy":{"duration_ns":3600000000000,"resolution_ns":60,"aggregation":"max"}}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
			},
			method: http.MethodGet, path: "/ts/series/cpu/retention",
			wantValue: &qrdbtransport.TSGetRetentionResponse{Policy: &qrdbtransport.TSRetentionPolicy{
				DurationNs: int64(time.Hour), ResolutionNs: 60, Aggregation: qrdbtransport.AggMax,
			}},
		},
		{
			name:  "TSGetRetentionNoPolicy",
			reply: `{"metric":"cpu","policy":null}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
			},
			method: http.MethodGet, path: "/ts/series/cpu/retention",
			wantValue: &qrdbtransport.TSGetRetentionResponse{},
		},
		{
			name:  "TSGetRetentionEmptyBody",
			reply: ``,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
			},
			method: http.MethodGet, path: "/ts/series/cpu/retention",
			wantErr: true,
		},
		{
			name:  "TSSetRetention",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return nil, c.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{Metric: "cpu",
					Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: 100, ResolutionNs: 10, Aggregation: qrdbtransport.AggSum}})
			},
			method: http.MethodPut, path: "/ts/series/cpu/retention",
			body: `{"duration_ns":100,"resolution_ns":10,"aggregation":"sum"}`,
		},
		{
			name:  "TSSetRetentionNoAggregation",
			reply: `{}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return nil, c.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{Metric: "cpu",
					Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: 100}})
			},
			method: http.MethodPut, path: "/ts/series/cpu/retention",
			body: `{"duration_ns":100}`,
		},
		{
			name:  "TSListMetrics",
			reply: `{"metrics":["cpu","mem"]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{Prefix: "c", Limit: 2, Consistency: "strong"})
			},
			method: http.MethodGet, path: "/ts/metrics", query: "consistency=strong&limit=2&prefix=c",
			wantValue: &qrdbtransport.TSListMetricsResponse{Metrics: []string{"cpu", "mem"}},
		},
		{
			name:  "TSListMetricsNoQuery",
			reply: `{"metrics":[]}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{})
			},
			method: http.MethodGet, path: "/ts/metrics",
			wantValue: &qrdbtransport.TSListMetricsResponse{Metrics: []string{}},
		},
		{
			name:   "Unauthorized",
			status: http.StatusUnauthorized,
			reply:  `{"error":"platform bearer authorization is missing"}`,
			call: func(ctx context.Context, c *QRDBTimeSeriesClient) (any, error) {
				return c.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{})
			},
			method: http.MethodGet, path: "/ts/metrics",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				calls []tsWireCall
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				mu.Lock()
				calls = append(calls, tsWireCall{
					method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery,
					body: string(body), auth: r.Header.Get("Authorization"),
				})
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte(tt.reply))
			}))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithAuthToken(token))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			got, err := tt.call(context.Background(), tr.QRDBTimeSeries())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", got)
				}
			} else if err != nil {
				t.Fatalf("call: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(calls) != 1 {
				t.Fatalf("requests: got %d, want 1", len(calls))
			}
			c := calls[0]
			if c.method != tt.method || c.path != tt.path || c.query != tt.query {
				t.Errorf("request: got %s %s ?%s, want %s %s ?%s", c.method, c.path, c.query, tt.method, tt.path, tt.query)
			}
			if c.auth != "Bearer "+token {
				t.Errorf("Authorization: got %q, want %q", c.auth, "Bearer "+token)
			}
			assertJSONBody(t, c.body, tt.body)
			if !tt.wantErr && tt.wantValue != nil && !reflect.DeepEqual(got, tt.wantValue) {
				t.Errorf("response: got %#v, want %#v", got, tt.wantValue)
			}
		})
	}
}

// assertJSONBody compares two JSON documents structurally; an empty want
// requires an empty body.
func assertJSONBody(t *testing.T, got, want string) {
	t.Helper()
	if want == "" {
		if got != "" {
			t.Errorf("body: got %s, want none", got)
		}
		return
	}
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("decode request body %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("decode wanted body %q: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body: got %s, want %s", got, want)
	}
}
