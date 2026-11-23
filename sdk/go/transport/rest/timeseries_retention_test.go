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

// TestTimeseriesRESTHandler_SetRetention_PutsServerWire proves SetRetention
// is PUT /api/v1/timeseries/retention/{metric} with the server's
// TSRetentionPolicy body: whole seconds and a lower-case aggregation.
func TestTimeseriesRESTHandler_SetRetention_PutsServerWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/timeseries/retention/cpu.load" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Policy map[string]interface{} `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Policy["duration_seconds"] != float64(3600) || body.Policy["resolution_seconds"] != float64(60) || body.Policy["aggregation"] != "avg" {
			t.Errorf("unexpected policy wire: %v", body.Policy)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "SetRetention", &timeseriesclient.SetRetentionRequest{
		Metric: "cpu.load",
		Policy: timeseriesclient.RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: timeseriesclient.AggregateAvg},
	})
	if err != nil {
		t.Fatalf("SetRetention failed: %v", err)
	}
}

// TestTimeseriesRESTHandler_GetRetention_DecodesServerWire proves
// GetRetention is GET /api/v1/timeseries/retention/{metric} and decodes the
// server's GetRetentionPolicyResponse into the typed policy.
func TestTimeseriesRESTHandler_GetRetention_DecodesServerWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/timeseries/retention/cpu" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metric":"cpu","policy":{"duration_seconds":86400,"resolution_seconds":300,"aggregation":"max"},"timestamp":"2026-11-14T00:00:00Z"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "GetRetention", &timeseriesclient.GetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("GetRetention failed: %v", err)
	}
	got, ok := resp.(*timeseriesclient.GetRetentionResponse)
	if !ok {
		t.Fatalf("response type %T, want *timeseries.GetRetentionResponse", resp)
	}
	want := timeseriesclient.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: timeseriesclient.AggregateMax}
	if got.Metric != "cpu" || got.Policy == nil || *got.Policy != want {
		t.Fatalf("GetRetention: got %+v (policy %+v), want metric cpu and policy %+v", got, got.Policy, want)
	}
}

// TestTimeseriesRESTHandler_GetRetention_NoPolicy proves a metric without a
// policy comes back with a nil Policy, not a zero one.
func TestTimeseriesRESTHandler_GetRetention_NoPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metric":"cpu","timestamp":"2026-11-14T00:00:00Z"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "GetRetention", &timeseriesclient.GetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("GetRetention failed: %v", err)
	}
	if got := resp.(*timeseriesclient.GetRetentionResponse); got.Policy != nil {
		t.Fatalf("want a nil policy, got %+v", got.Policy)
	}
}
