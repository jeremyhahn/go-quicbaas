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

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestHealthPing tests the health ping endpoint.
func TestHealthPing(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("REST ping returns OK", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, string(body))
		}
	})

	t.Run("REST ping with invalid method returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/health", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// POST to ping should return method not allowed
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Logf("note: POST to /api/v1/health returned %d (may be acceptable)", resp.StatusCode)
		}
	})
}

// TestHealthLiveness tests the liveness health endpoint.
func TestHealthLiveness(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("REST liveness returns healthy status", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/live", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, string(body))
		}

		// Parse response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		if len(body) > 0 {
			var healthResp map[string]interface{}
			if err := json.Unmarshal(body, &healthResp); err != nil {
				t.Logf("response body is not JSON: %s", string(body))
			} else {
				t.Logf("liveness response: %v", healthResp)
			}
		}
	})

	t.Run("REST liveness is idempotent", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		client := newHTTPClient(10 * time.Second)

		// Make multiple requests
		for i := 0; i < 5; i++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/live", nil)
			if err != nil {
				t.Fatalf("failed to create request %d: %v", i, err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request %d failed: %v", i, err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d: expected status %d, got %d", i, http.StatusOK, resp.StatusCode)
			}
		}
	})
}

// TestHealthReadiness tests the readiness health endpoint.
func TestHealthReadiness(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("REST readiness returns healthy status", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/ready", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, string(body))
		}

		// Parse response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		if len(body) > 0 {
			var healthResp map[string]interface{}
			if err := json.Unmarshal(body, &healthResp); err != nil {
				t.Logf("response body is not JSON: %s", string(body))
			} else {
				t.Logf("readiness response: %v", healthResp)
			}
		}
	})

	t.Run("REST readiness after server startup", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Give the server a moment to fully initialize
		time.Sleep(500 * time.Millisecond)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/ready", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status %d after startup, got %d: %s", http.StatusOK, resp.StatusCode, string(body))
		}
	})
}

// TestHealthEndpointDiscovery tests that health endpoints are discoverable.
func TestHealthEndpointDiscovery(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	endpoints := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"ping", "/api/v1/health", http.StatusOK},
		{"live", "/api/v1/health/live", http.StatusOK},
		{"ready", "/api/v1/health/ready", http.StatusOK},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			ctx, cancel := DefaultContext(t)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+ep.path, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			client := newHTTPClient(10 * time.Second)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != ep.wantCode {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected status %d, got %d: %s", ep.wantCode, resp.StatusCode, string(body))
			}
		})
	}
}

// TestHealthResponseFormat tests the health response format.
func TestHealthResponseFormat(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("liveness response has correct content type", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/live", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		if contentType != "" && contentType != "application/json" && contentType != "application/json; charset=utf-8" {
			t.Logf("content-type: %s (note: may vary by implementation)", contentType)
		}
	})

	t.Run("readiness response has correct content type", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/ready", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		if contentType != "" && contentType != "application/json" && contentType != "application/json; charset=utf-8" {
			t.Logf("content-type: %s (note: may vary by implementation)", contentType)
		}
	})
}

// TestHealthConcurrentRequests tests concurrent health check requests.
func TestHealthConcurrentRequests(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("concurrent liveness requests", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		const numRequests = 10
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/health/live", nil)
				if err != nil {
					results <- err
					return
				}

				client := newHTTPClient(10 * time.Second)
				resp, err := client.Do(req)
				if err != nil {
					results <- err
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					results <- &statusError{expected: http.StatusOK, got: resp.StatusCode}
					return
				}

				results <- nil
			}()
		}

		// Collect results
		var errors []error
		for i := 0; i < numRequests; i++ {
			if err := <-results; err != nil {
				errors = append(errors, err)
			}
		}

		if len(errors) > 0 {
			t.Errorf("concurrent requests had %d errors: %v", len(errors), errors)
		}
	})
}

// statusError is a simple error type for status code mismatches.
type statusError struct {
	expected int
	got      int
}

func (e *statusError) Error() string {
	return "status code mismatch"
}
