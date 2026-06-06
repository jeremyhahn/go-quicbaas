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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	timeseriesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
)

func TestTimeseriesRESTHandler_Write_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/timeseries/write" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body timeseriesWritePointRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Metric != "cpu" || body.Value != 0.75 {
			t.Errorf("unexpected point: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Write", &timeseriesclient.WriteRequest{
		Metric:    "cpu",
		Value:     0.75,
		Timestamp: time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func TestTimeseriesRESTHandler_Query_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/timeseries/query" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body timeseriesQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Aggregation != "AVG" || body.IntervalSec != 60 {
			t.Errorf("unexpected aggregation: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(timeseriesQueryResponseWire{
			Metric: "cpu",
			Points: []timeseriesDataPointWire{
				{Value: 1.0, Timestamp: time.Unix(1700000000, 0)},
				{Value: 2.0, Timestamp: time.Unix(1700000060, 0)},
			},
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", &timeseriesclient.QueryRequest{
		Metric:    "cpu",
		Start:     time.Unix(1700000000, 0),
		End:       time.Unix(1700003600, 0),
		Aggregate: timeseriesclient.AggregateAvg,
		Interval:  time.Minute,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	resp := result.(*timeseriesclient.QueryResponse)
	if len(resp.Series) != 2 || resp.Series[1].Value != 2.0 {
		t.Errorf("unexpected series: %+v", resp.Series)
	}
}

func TestTimeseriesRESTHandler_Query_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", &timeseriesclient.QueryRequest{Metric: "cpu"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestTimeseriesRESTHandler_Query_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", &timeseriesclient.QueryRequest{Metric: "cpu"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
