// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// TestClusterHandlerRebalanceMethods drives the Rebalance and
// GetRebalanceStatus methods through the typed cluster handler.
func TestClusterHandlerRebalanceMethods(t *testing.T) {
	var gotMethod, gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/cluster/rebalance":
			_, _ = w.Write([]byte(`{"operation_id":"op1","estimated_completion":1700000000}`))
		default:
			_, _ = w.Write([]byte(`{"operation_id":"op1","status":"running","percent_complete":42.5,` +
				`"shards_moved":2,"total_shards":8,"elapsed_seconds":12}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("Rebalance", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Cluster", "Rebalance",
			&cluster.RebalanceClusterRequest{Strategy: "even"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, ok := resp.(*cluster.RebalanceClusterResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if out.OperationID != "op1" {
			t.Errorf("expected operation id op1, got %q", out.OperationID)
		}
		if gotMethod != http.MethodPost || gotURI != "/api/v1/cluster/rebalance" {
			t.Errorf("unexpected request: %s %s", gotMethod, gotURI)
		}
	})

	t.Run("GetRebalanceStatus", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Cluster", "GetRebalanceStatus",
			&cluster.GetRebalanceProgressRequest{OperationID: "op1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, ok := resp.(*cluster.GetRebalanceProgressResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if out.Status != "running" || out.PercentComplete != 42.5 || out.ShardsMoved != 2 {
			t.Errorf("unexpected progress: %+v", out)
		}
		if gotMethod != http.MethodGet || gotURI != "/api/v1/cluster/rebalance/status?operation_id=op1" {
			t.Errorf("unexpected request: %s %s", gotMethod, gotURI)
		}
	})
}

// TestClusterHandlerRebalanceValidation exercises the invalid-request-type
// guards on the typed cluster methods.
func TestClusterHandlerRebalanceValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	t.Run("Rebalance bad type", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Cluster", "Rebalance", "x"); err == nil {
			t.Fatal("expected invalid-input error")
		}
	})
	t.Run("GetRebalanceStatus bad type", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Cluster", "GetRebalanceStatus", 1); err == nil {
			t.Fatal("expected invalid-input error")
		}
	})
}

// TestClusterHandlerRebalanceServerError exercises the doJSONRequest error path.
func TestClusterHandlerRebalanceServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Cluster", "Rebalance",
		&cluster.RebalanceClusterRequest{Strategy: "even"})
	if err == nil {
		t.Fatal("expected error from server 503")
	}
}
