//go:build rag

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

package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHealthChecker(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	checker := NewHealthChecker(client)

	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}

	// Initial status should be unavailable
	if checker.IsAvailable() {
		t.Error("expected initial status to be unavailable")
	}
}

func TestHealthChecker_Check_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("expected path /api/version, got %s", r.URL.Path)
		}

		resp := VersionResponse{Version: "0.1.0"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	checker := NewHealthChecker(client)

	status, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !status.Available {
		t.Error("expected status to be available")
	}

	if status.Version != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %s", status.Version)
	}

	if status.Error != "" {
		t.Errorf("expected no error, got %s", status.Error)
	}

	if status.LastChecked.IsZero() {
		t.Error("expected LastChecked to be set")
	}
}

func TestHealthChecker_Check_Unavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	checker := NewHealthChecker(client)

	status, err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	if status.Available {
		t.Error("expected status to be unavailable")
	}

	if status.Error == "" {
		t.Error("expected error message to be set")
	}
}

func TestHealthChecker_IsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := VersionResponse{Version: "0.1.0"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	checker := NewHealthChecker(client)

	// Initially unavailable
	if checker.IsAvailable() {
		t.Error("expected initial status to be unavailable")
	}

	// After successful check
	_, _ = checker.Check(context.Background())
	if !checker.IsAvailable() {
		t.Error("expected status to be available after check")
	}
}

func TestHealthChecker_LastStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := VersionResponse{Version: "0.1.0"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	checker := NewHealthChecker(client)

	// Get initial status
	initialStatus := checker.LastStatus()
	if initialStatus.Available {
		t.Error("expected initial status to be unavailable")
	}

	// Perform check
	_, _ = checker.Check(context.Background())

	// Get updated status
	updatedStatus := checker.LastStatus()
	if !updatedStatus.Available {
		t.Error("expected updated status to be available")
	}

	if updatedStatus.Version != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %s", updatedStatus.Version)
	}
}

func TestHealthChecker_Check_UpdatesLastStatus(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			resp := VersionResponse{Version: "0.1.0"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	checker := NewHealthChecker(client)

	// First check - success
	status1, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("expected no error on first check, got %v", err)
	}
	if !status1.Available {
		t.Error("expected first check to be available")
	}

	// Verify IsAvailable reflects the check
	if !checker.IsAvailable() {
		t.Error("expected IsAvailable to return true after successful check")
	}

	// Second check - failure
	status2, err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error on second check")
	}
	if status2.Available {
		t.Error("expected second check to be unavailable")
	}

	// Verify IsAvailable reflects the updated status
	if checker.IsAvailable() {
		t.Error("expected IsAvailable to return false after failed check")
	}
}
