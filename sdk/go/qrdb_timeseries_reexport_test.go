package quicbaas

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tsStubCall is one request the go-qrdb time-series stub received.
type tsStubCall struct {
	method string
	path   string
	body   string
}

// newQRDBTimeSeriesStub stands in for a go-qrdb node's REST time-series
// routes. It records every request and answers query and get-retention
// with fixed bodies.
func newQRDBTimeSeriesStub(t *testing.T) (*httptest.Server, func() []tsStubCall) {
	t.Helper()

	var (
		mu    sync.Mutex
		calls []tsStubCall
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, tsStubCall{method: r.Method, path: r.URL.Path, body: string(body)})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/ts/query"):
			_, _ = w.Write([]byte(`{"points":[{"metric":"cpu","value":1.5,"timestamp":100}]}`))
		case strings.HasSuffix(r.URL.Path, "/retention") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"policy":{"duration_ns":3600000000000}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []tsStubCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]tsStubCall(nil), calls...)
	}
}

// TestSearchQRDBTimeSeries_ReExportsGoQRDBClient proves the sdk/go
// re-export of go-qrdb's TimeSeriesClient: Search().QRDBTimeSeries()
// returns go-qrdb's own client, and its write, query and retention calls
// reach the go-qrdb node's /ts routes.
func TestSearchQRDBTimeSeries_ReExportsGoQRDBClient(t *testing.T) {
	srv, calls := newQRDBTimeSeriesStub(t)

	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)
	c := &client{opts: opts, address: strings.TrimPrefix(srv.URL, "https://")}
	tr, err := createRESTTransport(c.address, c.opts)
	if err != nil {
		t.Fatalf("createRESTTransport: %v", err)
	}
	c.transport = tr
	t.Cleanup(func() { _ = tr.Close() })

	ts, err := c.Search().QRDBTimeSeries()
	if err != nil {
		t.Fatalf("Search().QRDBTimeSeries(): %v", err)
	}
	// The alias is go-qrdb's interface itself, not a copy of it.
	var _ qrdbtransport.TimeSeriesClient = ts

	ctx := context.Background()
	if err := ts.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "cpu", Value: 1.5, TimestampNs: 100}); err != nil {
		t.Fatalf("TSWrite: %v", err)
	}
	resp, err := ts.TSQuery(ctx, &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 0, EndNs: 200})
	if err != nil {
		t.Fatalf("TSQuery: %v", err)
	}
	if len(resp.Points) != 1 || resp.Points[0].Value != 1.5 {
		t.Fatalf("TSQuery points: got %+v, want one point of value 1.5", resp.Points)
	}
	if err := ts.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{
		Metric: "cpu",
		Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: 3600000000000},
	}); err != nil {
		t.Fatalf("TSSetRetention: %v", err)
	}
	ret, err := ts.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("TSGetRetention: %v", err)
	}
	if ret.Policy == nil || ret.Policy.DurationNs != 3600000000000 {
		t.Fatalf("TSGetRetention policy: got %+v, want duration 1h", ret.Policy)
	}

	var sawWrite, sawQuery, sawSetRet, sawGetRet bool
	for _, call := range calls() {
		switch {
		case call.method == http.MethodPost && strings.HasSuffix(call.path, "/ts/write"):
			var wire map[string]any
			if err := json.Unmarshal([]byte(call.body), &wire); err != nil || wire["metric"] != "cpu" {
				t.Errorf("TSWrite body: got %q, want metric cpu", call.body)
			}
			sawWrite = true
		case call.method == http.MethodGet && strings.HasSuffix(call.path, "/ts/query"):
			sawQuery = true
		case call.method != http.MethodGet && strings.HasSuffix(call.path, "/ts/series/cpu/retention"):
			sawSetRet = true
		case call.method == http.MethodGet && strings.HasSuffix(call.path, "/ts/series/cpu/retention"):
			sawGetRet = true
		}
	}
	if !sawWrite || !sawQuery || !sawSetRet || !sawGetRet {
		t.Fatalf("go-qrdb /ts routes reached: write=%v query=%v set-retention=%v get-retention=%v; calls: %+v",
			sawWrite, sawQuery, sawSetRet, sawGetRet, calls())
	}
}

// TestSearchQRDBTimeSeries_BuildErrorReturned proves a go-qrdb client that
// cannot be built surfaces as an error, never as a nil client.
func TestSearchQRDBTimeSeries_BuildErrorReturned(t *testing.T) {
	c := &client{opts: defaultOptions(), address: "embedded://local"}
	c.opts.transportType = TransportEmbedded // no qrdb servicers installed

	ts, err := c.Search().QRDBTimeSeries()
	if err == nil {
		t.Fatal("Search().QRDBTimeSeries(): want an error when the go-qrdb client cannot be built")
	}
	if ts != nil {
		t.Errorf("Search().QRDBTimeSeries(): want a nil client with the error, got %T", ts)
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("want InvalidInput, got %T: %v", err, err)
	}
}

// TestSearchQRDBTimeSeries_NoUnauthenticatedFallback pins that a REST, QUIC or
// MCP selection without its concrete transport is refused rather than dialed
// through go-qrdb's unauthenticated sub-client.
func TestSearchQRDBTimeSeries_NoUnauthenticatedFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		tt   TransportType
	}{
		{"REST", TransportREST},
		{"QUIC", TransportQUIC},
		{"MCP", TransportMCP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := defaultOptions()
			opts.transportType = tc.tt
			c := &client{opts: opts, address: "127.0.0.1:1"}

			ts, err := c.Search().QRDBTimeSeries()
			if err == nil {
				t.Fatal("Search().QRDBTimeSeries(): want an error without an authenticated adapter")
			}
			if ts != nil {
				t.Errorf("want a nil client with the error, got %T", ts)
			}
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("want InvalidInput, got %T: %v", err, err)
			}
		})
	}
}
