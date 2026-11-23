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

func TestHealthChecker_Check(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantErr     bool
		wantVersion string
	}{
		{
			name: "server answers version",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/version" {
					t.Errorf("expected path /api/version, got %s", r.URL.Path)
				}
				resp := VersionResponse{Version: "0.1.0"}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantVersion: "0.1.0",
		},
		{
			name: "server unavailable",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewClient(ClientConfig{
				BaseURL:    server.URL,
				MaxRetries: 0,
			})
			checker := NewHealthChecker(client)

			// A new checker reports unavailable until its first check.
			if checker.IsAvailable() {
				t.Error("expected initial status to be unavailable")
			}
			if checker.LastStatus().Available {
				t.Error("expected initial LastStatus to be unavailable")
			}

			status, err := checker.Check(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}

			if status.Available == tt.wantErr {
				t.Errorf("expected Available %v, got %v", !tt.wantErr, status.Available)
			}
			if status.Version != tt.wantVersion {
				t.Errorf("expected version %q, got %q", tt.wantVersion, status.Version)
			}
			if (status.Error != "") != tt.wantErr {
				t.Errorf("expected error message set %v, got %q", tt.wantErr, status.Error)
			}
			if status.LastChecked.IsZero() {
				t.Error("expected LastChecked to be set")
			}

			if checker.IsAvailable() != status.Available {
				t.Errorf("expected IsAvailable %v after check", status.Available)
			}
			if checker.LastStatus() != status {
				t.Error("expected LastStatus to return the status of the last check")
			}
		})
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
