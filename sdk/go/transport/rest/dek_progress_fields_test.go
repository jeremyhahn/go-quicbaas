// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// TestDEKClientOverREST_ProgressCarriesStatusAndEstimatedCompletion serves
// the JSON the REST server encodes for the status and re-encryption routes and
// checks the DEK client sees the progress Status and EstimatedCompletion.
func TestDEKClientOverREST_ProgressCarriesStatusAndEstimatedCompletion(t *testing.T) {
	started := time.Unix(1700000000, 0).UTC()
	eta := time.Unix(1700003600, 0).UTC()
	progress := &service.ReencryptionProgress{
		Status:              "running",
		InProgress:          true,
		TotalKeys:           4,
		ProcessedKeys:       2,
		StartedAt:           &started,
		EstimatedCompletion: &eta,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body interface{}
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			body = &service.GetDEKStatusResponse{ReencryptionProgress: progress}
		case strings.HasSuffix(r.URL.Path, "/reencrypt"):
			body = &service.GetReencryptionProgressResponse{Path: "app/key", Status: "running", Progress: progress}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer server.Close()

	client := dekclient.NewClient(newTestTransport(t, server))
	ctx := context.Background()

	status, err := client.GetDEKStatus(ctx, "app/key")
	if err != nil {
		t.Fatalf("GetDEKStatus: %v", err)
	}
	assertRESTProgressFields(t, status.ReencryptionProgress, eta)

	resp, err := client.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{Path: "app/key"})
	if err != nil {
		t.Fatalf("GetReencryptionProgress: %v", err)
	}
	assertRESTProgressFields(t, resp.Progress, eta)
}

func assertRESTProgressFields(t *testing.T, got *dekclient.ReencryptionProgress, eta time.Time) {
	t.Helper()
	if got == nil {
		t.Fatal("progress is nil")
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if !got.EstimatedCompletion.Equal(eta) {
		t.Errorf("EstimatedCompletion = %v, want %v", got.EstimatedCompletion, eta)
	}
}
