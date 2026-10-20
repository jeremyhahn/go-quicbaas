// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package dek

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport returns a fixed response for every request.
type mockTransport struct {
	resp interface{}
	err  error
}

func (m *mockTransport) Request(context.Context, string, string, interface{}) (interface{}, error) {
	return m.resp, m.err
}

func (m *mockTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockTransport) Conn() interface{} { return nil }

func (m *mockTransport) Close() error { return nil }

func (m *mockTransport) Healthy() bool { return true }

// serviceProgress returns a service progress with every field set.
func serviceProgress(started, eta time.Time) *service.ReencryptionProgress {
	return &service.ReencryptionProgress{
		Status:              "running",
		InProgress:          true,
		TotalKeys:           100,
		ProcessedKeys:       40,
		FailedKeys:          2,
		PercentComplete:     40,
		StartedAt:           &started,
		EstimatedCompletion: &eta,
		ErrorMessage:        "partial",
	}
}

func assertProgress(t *testing.T, got *ReencryptionProgress, started, eta time.Time) {
	t.Helper()
	want := ReencryptionProgress{
		Status:              "running",
		InProgress:          true,
		TotalKeys:           100,
		ProcessedKeys:       40,
		FailedKeys:          2,
		PercentComplete:     40,
		StartedAt:           started,
		EstimatedCompletion: eta,
		ErrorMessage:        "partial",
	}
	if got == nil {
		t.Fatal("progress is nil")
	}
	if *got != want {
		t.Fatalf("progress = %+v, want %+v", *got, want)
	}
}

func TestClient_GetReencryptionProgress_CarriesStatusAndEstimatedCompletion(t *testing.T) {
	started := time.Unix(1700000000, 0).UTC()
	eta := time.Unix(1700003600, 0).UTC()
	c := NewClient(&mockTransport{resp: &service.GetReencryptionProgressResponse{
		OperationID: "op-1",
		Path:        "/db/a",
		Status:      "running",
		Progress:    serviceProgress(started, eta),
	}})

	resp, err := c.GetReencryptionProgress(context.Background(), &GetReencryptionProgressRequest{Path: "/db/a", OperationID: "op-1"})
	if err != nil {
		t.Fatalf("GetReencryptionProgress: %v", err)
	}
	assertProgress(t, resp.Progress, started, eta)
}

func TestClient_GetDEKStatus_CarriesStatusAndEstimatedCompletion(t *testing.T) {
	started := time.Unix(1700000000, 0).UTC()
	eta := time.Unix(1700003600, 0).UTC()
	c := NewClient(&mockTransport{resp: &service.GetDEKStatusResponse{
		ReencryptionProgress: serviceProgress(started, eta),
	}})

	resp, err := c.GetDEKStatus(context.Background(), "/db/a")
	if err != nil {
		t.Fatalf("GetDEKStatus: %v", err)
	}
	assertProgress(t, resp.ReencryptionProgress, started, eta)
}

func TestClient_GetReencryptionProgress_NilEstimatedCompletionIsZero(t *testing.T) {
	c := NewClient(&mockTransport{resp: &service.GetReencryptionProgressResponse{
		Progress: &service.ReencryptionProgress{Status: "completed"},
	}})

	resp, err := c.GetReencryptionProgress(context.Background(), &GetReencryptionProgressRequest{Path: "/db/a"})
	if err != nil {
		t.Fatalf("GetReencryptionProgress: %v", err)
	}
	if resp.Progress.Status != "completed" {
		t.Fatalf("Status = %q, want completed", resp.Progress.Status)
	}
	if !resp.Progress.EstimatedCompletion.IsZero() || !resp.Progress.StartedAt.IsZero() {
		t.Fatalf("times = %v / %v, want zero", resp.Progress.StartedAt, resp.Progress.EstimatedCompletion)
	}
}

func TestClient_GetReencryptionProgress_RejectsNilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	if _, err := c.GetReencryptionProgress(context.Background(), nil); err == nil {
		t.Fatal("expected an error for a nil request")
	}
}
