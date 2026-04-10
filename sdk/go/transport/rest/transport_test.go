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

package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestNew(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		_, err := New(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("empty address returns error", func(t *testing.T) {
		_, err := New(&transport.Config{})
		if err == nil {
			t.Fatal("expected error for empty address")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("valid config creates transport", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.Conn() == nil {
			t.Error("expected HTTP client to be set")
		}
		if !tr.Healthy() {
			t.Error("expected transport to be healthy")
		}
	})

	t.Run("with custom options", func(t *testing.T) {
		customClient := &http.Client{Timeout: 5 * time.Second}
		headers := map[string]string{"X-Custom": "value"}

		tr, err := New(
			&transport.Config{
				Address: "localhost:8080",
			},
			WithHTTPClient(customClient),
			WithBaseURL("http://custom.example.com"),
			WithHeaders(headers),
			WithHeader("X-Another", "header"),
			WithAuthToken("test-token"),
			WithUserAgent("test-agent"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.client != customClient {
			t.Error("expected custom HTTP client to be set")
		}
		if tr.baseURL != "http://custom.example.com" {
			t.Errorf("expected custom base URL, got: %s", tr.baseURL)
		}
	})

	t.Run("default timeout is applied", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			// No Timeout set
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		client, ok := tr.Conn().(*http.Client)
		if !ok {
			t.Fatal("expected *http.Client")
		}
		if client.Timeout != 30*time.Second {
			t.Errorf("expected default timeout 30s, got %v", client.Timeout)
		}
	})
}

func TestTransportRequest(t *testing.T) {
	t.Run("successful GET request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("expected Content-Type header")
			}
			if r.Header.Get("Accept") != "application/json" {
				t.Error("expected Accept header")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		resp, err := tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("successful POST request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}

			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			json.Unmarshal(body, &req)

			if req["key"] != "test-key" {
				t.Errorf("expected key=test-key, got %v", req["key"])
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      123,
				"created": true,
			})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		req := map[string]string{"key": "test-key"}
		// Use Cluster service which goes through generic handler, not the typed KV handler
		resp, err := tr.Request(ctx, "Cluster", "AddNode", req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("custom headers are sent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Custom") != "custom-value" {
				t.Error("expected custom header")
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("expected authorization header")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
			WithHeader("X-Custom", "custom-value"),
			WithAuthToken("test-token"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("SetHeader updates headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Dynamic") != "dynamic-value" {
				t.Error("expected dynamic header")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		tr.SetHeader("X-Dynamic", "dynamic-value")

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("SetHeaders updates multiple headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Header1") != "value1" {
				t.Error("expected header1")
			}
			if r.Header.Get("X-Header2") != "value2" {
				t.Error("expected header2")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		tr.SetHeaders(map[string]string{
			"X-Header1": "value1",
			"X-Header2": "value2",
		})

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTransportErrorHandling(t *testing.T) {
	// Note: These tests use Cluster service instead of KV service because KV has a
	// specialized handler that requires proper typed requests (*service.PutRequest, etc.).
	// Cluster service uses the generic handler which accepts any request type including nil.

	t.Run("400 bad request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid input"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("401 unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsUnauthorized(err) {
			t.Errorf("expected unauthorized error, got: %v", err)
		}
	})

	t.Run("403 forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected forbidden error, got: %v", err)
		}
	})

	t.Run("404 not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "Status", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("409 conflict", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsConflict(err) {
			t.Errorf("expected conflict error, got: %v", err)
		}
	})

	t.Run("429 rate limited", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Code(err) != errors.CodeRateLimited {
			t.Errorf("expected rate limited error, got: %v", err)
		}
	})

	t.Run("500 internal error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected internal error, got: %v", err)
		}
	})

	t.Run("503 service unavailable is retryable", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address:         server.Listener.Addr().String(),
				Timeout:         10 * time.Second,
				MaxRetries:      3,
				RetryBackoff:    1 * time.Millisecond,
				MaxRetryBackoff: 10 * time.Millisecond,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("expected success after retries, got: %v", err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsCanceled(err) {
			t.Errorf("expected canceled error, got: %v", err)
		}
	})

	t.Run("request on closed transport", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err == nil {
			t.Fatal("expected error on closed transport")
		}
	})

	t.Run("connection refused error", func(t *testing.T) {
		tr, err := New(
			&transport.Config{
				Address:         "127.0.0.1:1", // Port 1 should not be listening
				Timeout:         100 * time.Millisecond,
				MaxRetries:      0, // No retries
				RetryBackoff:    1 * time.Millisecond,
				MaxRetryBackoff: 5 * time.Millisecond,
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err == nil {
			t.Fatal("expected error for connection refused")
		}
		// Should be connection or unavailable error
		code := errors.Code(err)
		if code != errors.CodeConnection && code != errors.CodeUnavailable && code != errors.CodeTimeout {
			t.Errorf("expected connection/unavailable/timeout error, got: %v (code: %s)", err, code)
		}
	})

	t.Run("deadline exceeded during request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address:         server.Listener.Addr().String(),
				Timeout:         50 * time.Millisecond, // Short timeout
				MaxRetries:      0,
				RetryBackoff:    1 * time.Millisecond,
				MaxRetryBackoff: 5 * time.Millisecond,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err == nil {
			t.Fatal("expected timeout error")
		}
		code := errors.Code(err)
		if code != errors.CodeTimeout && code != errors.CodeConnection {
			t.Errorf("expected timeout/connection error, got: %v (code: %s)", err, code)
		}
	})
}

func TestTransportStreaming(t *testing.T) {
	t.Run("streaming not supported", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.RequestStream(ctx, "Cluster", "Watch", nil)
		if err == nil {
			t.Fatal("expected error for streaming")
		}
		if errors.Code(err) != errors.CodeUnimplemented {
			t.Errorf("expected unimplemented error, got: %v", err)
		}
	})
}

func TestTransportClose(t *testing.T) {
	t.Run("close is idempotent", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := tr.Close(); err != nil {
			t.Errorf("first close failed: %v", err)
		}
		if err := tr.Close(); err != nil {
			t.Errorf("second close failed: %v", err)
		}

		if tr.Healthy() {
			t.Error("expected transport to be unhealthy after close")
		}
	})
}

func TestTransportHealthy(t *testing.T) {
	t.Run("healthy after successful request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if !tr.Healthy() {
			t.Error("expected transport to be healthy initially")
		}

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !tr.Healthy() {
			t.Error("expected transport to be healthy after successful request")
		}
	})

	t.Run("unhealthy after close", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr.Close()

		if tr.Healthy() {
			t.Error("expected transport to be unhealthy after close")
		}
	})

	t.Run("healthy when lastHealth is zero", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		// Reset lastHealth to zero
		tr.lastHealth.Store(0)

		if !tr.Healthy() {
			t.Error("expected transport to be healthy when lastHealth is zero")
		}
	})
}

func TestTransportTLS(t *testing.T) {
	t.Run("TLS configuration", func(t *testing.T) {
		tlsConfig := &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true,
		}

		tr, err := New(&transport.Config{
			Address:   "localhost:8443",
			Timeout:   10 * time.Second,
			TLSConfig: tlsConfig,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "https://localhost:8443" {
			t.Errorf("expected https URL, got: %s", tr.baseURL)
		}
	})

	t.Run("TLS server connection", func(t *testing.T) {
		// Create a TLS test server
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		// Get the server's certificate
		certPool := x509.NewCertPool()
		certPool.AddCert(server.Certificate())

		tlsConfig := &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS13,
		}

		tr, err := New(
			&transport.Config{
				Address:   server.Listener.Addr().String(),
				Timeout:   10 * time.Second,
				TLSConfig: tlsConfig,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		resp, err := tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})
}

func TestTransportUnixSocket(t *testing.T) {
	t.Run("Unix socket connection", func(t *testing.T) {
		// Create a temporary socket path
		tempDir := t.TempDir()
		socketPath := filepath.Join(tempDir, "test.sock")

		// Create a Unix socket listener
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("failed to create Unix socket: %v", err)
		}
		defer listener.Close()

		// Create HTTP server on Unix socket
		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}),
		}
		go server.Serve(listener)
		defer server.Close()

		// Give server time to start
		time.Sleep(10 * time.Millisecond)

		// Create transport with Unix socket
		tr, err := New(&transport.Config{
			Address: socketPath,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		resp, err := tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("Unix socket with explicit prefix", func(t *testing.T) {
		tempDir := t.TempDir()
		socketPath := filepath.Join(tempDir, "test2.sock")

		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("failed to create Unix socket: %v", err)
		}
		defer listener.Close()

		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}),
		}
		go server.Serve(listener)
		defer server.Close()

		time.Sleep(10 * time.Millisecond)

		tr, err := New(&transport.Config{
			Address: "unix://" + socketPath,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		resp, err := tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})
}

func TestEndpointMapping(t *testing.T) {
	testCases := []struct {
		service        string
		method         string
		expectedPath   string
		expectedMethod string
	}{
		// KV methods
		{"KV", "Put", "/api/v1/entities/{type}", http.MethodPost},
		{"KV", "Get", "/api/v1/entities/{type}/{id}", http.MethodGet},
		{"KV", "Delete", "/api/v1/entities/{type}/{id}", http.MethodDelete},
		{"KV", "List", "/api/v1/entities/{type}", http.MethodGet},
		{"KV", "BatchPut", "/api/v1/batch/put", http.MethodPost},
		{"KV", "BatchDelete", "/api/v1/batch/delete", http.MethodPost},
		{"KV", "Query", "/api/v1/query/{type}/{index}", http.MethodGet},
		{"KV", "RangeQuery", "/api/v1/query/range", http.MethodPost},
		{"KV", "Transaction", "/api/v1/transaction", http.MethodPost},
		{"KV", "CustomKV", "/api/v1/kv/customkv", http.MethodPost},

		// Cluster methods
		{"Cluster", "Status", "/api/v1/cluster/status", http.MethodGet},
		{"Cluster", "GetLeader", "/api/v1/cluster/leader/{shardID}", http.MethodGet},
		{"Cluster", "Shards", "/api/v1/cluster/shards", http.MethodGet},
		{"Cluster", "AddNode", "/api/v1/cluster/nodes", http.MethodPost},
		{"Cluster", "RemoveNode", "/api/v1/cluster/nodes/{nodeID}", http.MethodDelete},
		{"Cluster", "ListNodes", "/api/v1/cluster/nodes", http.MethodGet},
		{"Cluster", "StartRebalance", "/api/v1/cluster/rebalance", http.MethodPost},
		{"Cluster", "RebalanceStatus", "/api/v1/cluster/rebalance/status", http.MethodGet},
		{"Cluster", "CustomCluster", "/api/v1/cluster/customcluster", http.MethodPost},

		// Health methods
		{"Health", "Check", "/health", http.MethodGet},
		{"Health", "Health", "/health", http.MethodGet},
		{"Health", "Liveness", "/health/live", http.MethodGet},
		{"Health", "Readiness", "/health/ready", http.MethodGet},
		{"Health", "CustomHealth", "/health/customhealth", http.MethodGet},

		// KMS methods
		{"KMS", "GenerateKey", "/api/v1/kms/keys", http.MethodPost},
		{"KMS", "ImportKey", "/api/v1/kms/keys/import", http.MethodPost},
		{"KMS", "ListKeys", "/api/v1/kms/keys", http.MethodGet},
		{"KMS", "GetKey", "/api/v1/kms/keys/{name}", http.MethodGet},
		{"KMS", "RotateKey", "/api/v1/kms/keys/{name}/rotate", http.MethodPost},
		{"KMS", "DeleteKey", "/api/v1/kms/keys/{name}", http.MethodDelete},
		{"KMS", "Encrypt", "/api/v1/kms/encrypt/{name}", http.MethodPost},
		{"KMS", "Decrypt", "/api/v1/kms/decrypt/{name}", http.MethodPost},
		{"KMS", "CustomKMS", "/api/v1/kms/customkms", http.MethodPost},

		// Secrets methods
		{"Secrets", "Store", "/api/v1/secrets/{path}", http.MethodPost},
		{"Secrets", "Get", "/api/v1/secrets/{path}", http.MethodGet},
		{"Secrets", "Delete", "/api/v1/secrets/{path}", http.MethodDelete},
		{"Secrets", "List", "/api/v1/secrets", http.MethodGet},
		{"Secrets", "Rotate", "/api/v1/secrets/{path}/rotate", http.MethodPost},
		{"Secrets", "CustomSecrets", "/api/v1/secrets/customsecrets", http.MethodPost},

		// Seal methods
		{"Seal", "Status", "/api/v1/seal/status", http.MethodGet},
		{"Seal", "Init", "/api/v1/seal/init", http.MethodPost},
		{"Seal", "Unseal", "/api/v1/seal/unseal", http.MethodPost},
		{"Seal", "Seal", "/api/v1/seal/seal", http.MethodPost},
		{"Seal", "Rekey", "/api/v1/seal/rekey", http.MethodPost},
		{"Seal", "CustomSeal", "/api/v1/seal/customseal", http.MethodPost},

		// Crypto methods
		{"Crypto", "Encrypt", "/api/v1/crypto/encrypt/{name}", http.MethodPost},
		{"Crypto", "Decrypt", "/api/v1/crypto/decrypt/{name}", http.MethodPost},
		{"Crypto", "Sign", "/api/v1/crypto/sign/{name}", http.MethodPost},
		{"Crypto", "Verify", "/api/v1/crypto/verify/{name}", http.MethodPost},
		{"Crypto", "Hash", "/api/v1/crypto/hash", http.MethodPost},
		{"Crypto", "Random", "/api/v1/crypto/random", http.MethodPost},
		{"Crypto", "CustomCrypto", "/api/v1/crypto/customcrypto", http.MethodPost},

		// Vector methods - updated to match actual RESTful implementation
		{"Vector", "CreateNamespace", "/api/v1/vector/namespaces", http.MethodPost},
		{"Vector", "ListNamespaces", "/api/v1/vector/namespaces", http.MethodGet},
		{"Vector", "GetNamespaceInfo", "/api/v1/vector/namespaces/{namespace}", http.MethodGet},
		{"Vector", "UpdateNamespace", "/api/v1/vector/namespaces/{namespace}", http.MethodPut},
		{"Vector", "DeleteNamespace", "/api/v1/vector/namespaces/{namespace}", http.MethodDelete},
		{"Vector", "GetNamespaceStats", "/api/v1/vector/namespaces/{namespace}/stats", http.MethodGet},
		{"Vector", "Upsert", "/api/v1/vector/namespaces/{namespace}/vectors", http.MethodPost},
		{"Vector", "UpsertVector", "/api/v1/vector/namespaces/{namespace}/vectors", http.MethodPost},
		{"Vector", "ListVectors", "/api/v1/vector/namespaces/{namespace}/vectors", http.MethodGet},
		{"Vector", "BatchUpsert", "/api/v1/vector/namespaces/{namespace}/vectors/batch", http.MethodPost},
		{"Vector", "BatchDelete", "/api/v1/vector/namespaces/{namespace}/vectors/batch", http.MethodDelete},
		{"Vector", "Get", "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodGet},
		{"Vector", "GetVector", "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodGet},
		{"Vector", "Delete", "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodDelete},
		{"Vector", "DeleteVector", "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodDelete},
		{"Vector", "UpdateMetadata", "/api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata", http.MethodPut},
		{"Vector", "Search", "/api/v1/vector/namespaces/{namespace}/search/similar", http.MethodPost},
		{"Vector", "SearchSimilar", "/api/v1/vector/namespaces/{namespace}/search/similar", http.MethodPost},
		{"Vector", "SearchByID", "/api/v1/vector/namespaces/{namespace}/search/by-id/{id}", http.MethodPost},
		{"Vector", "HybridSearch", "/api/v1/vector/hybrid-search", http.MethodPost},
		{"Vector", "SearchByMetadata", "/api/v1/vector/search-by-metadata", http.MethodPost},
		{"Vector", "GetGlobalStats", "/api/v1/vector/cluster/stats", http.MethodGet},
		{"Vector", "ClusterStats", "/api/v1/vector/cluster/stats", http.MethodGet},
		{"Vector", "Compact", "/api/v1/vector/admin/compact", http.MethodPost},
		{"Vector", "RebuildIndex", "/api/v1/vector/namespaces/{namespace}/index/rebuild", http.MethodPost},
		{"Vector", "GetIndexProgress", "/api/v1/vector/namespaces/{namespace}/index/progress", http.MethodGet},
		{"Vector", "Health", "/api/v1/vector/health", http.MethodGet},
		{"Vector", "CustomVector", "/api/v1/vector/customvector", http.MethodPost},

		// Timeseries methods
		{"Timeseries", "Write", "/api/v1/timeseries/write", http.MethodPost},
		{"Timeseries", "Query", "/api/v1/timeseries/query", http.MethodPost},
		{"Timeseries", "Delete", "/api/v1/timeseries/delete", http.MethodPost},
		{"Timeseries", "CustomTimeseries", "/api/v1/timeseries/customtimeseries", http.MethodPost},

		// With package prefix
		{"quicbaas.KV", "Put", "/api/v1/entities/{type}", http.MethodPost},
		{"com.example.Health", "Check", "/health", http.MethodGet},

		// Unknown service/method fallback
		{"Unknown", "CustomMethod", "/api/v1/unknown/custommethod", http.MethodPost},
	}

	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	for _, tc := range testCases {
		t.Run(tc.service+"/"+tc.method, func(t *testing.T) {
			path, method := tr.mapToEndpoint(tc.service, tc.method)
			if path != tc.expectedPath {
				t.Errorf("expected path %s, got %s", tc.expectedPath, path)
			}
			if method != tc.expectedMethod {
				t.Errorf("expected method %s, got %s", tc.expectedMethod, method)
			}
		})
	}
}

func TestStatusCodeMapping(t *testing.T) {
	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	testCases := []struct {
		statusCode   int
		expectedCode errors.ErrorCode
	}{
		{http.StatusBadRequest, errors.CodeInvalidInput},
		{http.StatusUnauthorized, errors.CodeUnauthorized},
		{http.StatusForbidden, errors.CodeForbidden},
		{http.StatusNotFound, errors.CodeNotFound},
		{http.StatusConflict, errors.CodeConflict},
		{http.StatusPreconditionFailed, errors.CodePreconditionFailed},
		{http.StatusTooManyRequests, errors.CodeRateLimited},
		{http.StatusInternalServerError, errors.CodeInternal},
		{http.StatusBadGateway, errors.CodeUnavailable},
		{http.StatusServiceUnavailable, errors.CodeUnavailable},
		{http.StatusGatewayTimeout, errors.CodeTimeout},
		{418, errors.CodeInvalidInput}, // Teapot - client error
		{599, errors.CodeInternal},     // Server error
		{200, errors.CodeUnknown},      // Success code
	}

	for _, tc := range testCases {
		t.Run(http.StatusText(tc.statusCode), func(t *testing.T) {
			code := tr.statusCodeToErrorCode(tc.statusCode)
			if code != tc.expectedCode {
				t.Errorf("expected %s, got %s", tc.expectedCode, code)
			}
		})
	}
}

func TestFactory(t *testing.T) {
	t.Run("factory creates transport", func(t *testing.T) {
		factory := &Factory{}
		tr, err := factory.Create(&transport.Config{
			Address: "localhost:8080",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr == nil {
			t.Fatal("expected transport")
		}
	})

	t.Run("factory returns error for invalid config", func(t *testing.T) {
		factory := &Factory{}
		_, err := factory.Create(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithBasicAuth sets credentials", func(t *testing.T) {
		tr, err := New(
			&transport.Config{
				Address: "localhost:8080",
				Timeout: 10 * time.Second,
			},
			WithBasicAuth("user", "pass"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.headers["X-QuicBaaS-BasicAuth-User"] != "user" {
			t.Error("expected username to be set")
		}
		if tr.headers["X-QuicBaaS-BasicAuth-Pass"] != "pass" {
			t.Error("expected password to be set")
		}
	})
}

func TestBuildBaseURL(t *testing.T) {
	testCases := []struct {
		name      string
		address   string
		tlsConfig *tls.Config
		expected  string
	}{
		{
			name:     "TCP defaults to https",
			address:  "localhost:8080",
			expected: "https://localhost:8080",
		},
		{
			name:      "TCP with TLS",
			address:   "localhost:8443",
			tlsConfig: &tls.Config{},
			expected:  "https://localhost:8443",
		},
		{
			name:     "Unix socket defaults to http",
			address:  "/var/run/quicbaas.sock",
			expected: "http://localhost",
		},
		{
			name:      "Unix socket with TLS",
			address:   "/var/run/quicbaas.sock",
			tlsConfig: &tls.Config{},
			expected:  "https://localhost",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := New(&transport.Config{
				Address:   tc.address,
				Timeout:   10 * time.Second,
				TLSConfig: tc.tlsConfig,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer tr.Close()

			if tr.baseURL != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tr.baseURL)
			}
		})
	}
}

func TestMinDuration(t *testing.T) {
	testCases := []struct {
		a, b     time.Duration
		expected time.Duration
	}{
		{1 * time.Second, 2 * time.Second, 1 * time.Second},
		{2 * time.Second, 1 * time.Second, 1 * time.Second},
		{1 * time.Second, 1 * time.Second, 1 * time.Second},
		{1 * time.Second, 0, 1 * time.Second},
		{0, 1 * time.Second, 0},
	}

	for _, tc := range testCases {
		result := minDuration(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("minDuration(%v, %v) = %v, expected %v", tc.a, tc.b, result, tc.expected)
		}
	}
}

func TestErrorParsing(t *testing.T) {
	t.Run("JSON error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":     "validation failed: field x is required",
				"code":      "INVALID_INPUT",
				"retryable": false,
			})
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("plain text error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Bad Request"))
		}))
		defer server.Close()

		tr, err := New(
			&transport.Config{
				Address: server.Listener.Addr().String(),
				Timeout: 10 * time.Second,
			},
			WithBaseURL(server.URL),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Cluster", "AddNode", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestHTTPMethods(t *testing.T) {
	// Test GET, POST, and DELETE methods (not PUT since our API doesn't use it for service/method mapping)
	methods := []string{http.MethodGet, http.MethodPost, http.MethodDelete}

	for _, expectedMethod := range methods {
		t.Run(expectedMethod, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != expectedMethod {
					t.Errorf("expected %s, got %s", expectedMethod, r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}))
			defer server.Close()

			tr, err := New(
				&transport.Config{
					Address: server.Listener.Addr().String(),
					Timeout: 10 * time.Second,
				},
				WithBaseURL(server.URL),
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer tr.Close()

			ctx := context.Background()
			var service, method string
			switch expectedMethod {
			case http.MethodGet:
				service, method = "Health", "Check"
			case http.MethodPost:
				service, method = "Cluster", "AddNode"
			case http.MethodDelete:
				service, method = "Cluster", "RemoveNode"
			}

			_, err = tr.Request(ctx, service, method, nil)
			// We don't care about the result, just that the correct method was used
			_ = err
		})
	}
}

func TestConn(t *testing.T) {
	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	conn := tr.Conn()
	if conn == nil {
		t.Fatal("expected connection")
	}

	client, ok := conn.(*http.Client)
	if !ok {
		t.Fatal("expected *http.Client")
	}
	if client.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", client.Timeout)
	}
}

func TestEmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tr, err := New(
		&transport.Config{
			Address: server.Listener.Addr().String(),
			Timeout: 10 * time.Second,
		},
		WithBaseURL(server.URL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	resp, err := tr.Request(ctx, "Cluster", "RemoveNode", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response for empty body")
	}
}

func TestInvalidRequestBody(t *testing.T) {
	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()

	// Create an unmarshable value (channel)
	invalidBody := make(chan int)

	_, err = tr.Request(ctx, "Cluster", "AddNode", invalidBody)
	if err == nil {
		t.Fatal("expected error for invalid body")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestRetryWithMaxBackoff(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	tr, err := New(
		&transport.Config{
			Address:         server.Listener.Addr().String(),
			Timeout:         10 * time.Second,
			MaxRetries:      2,
			RetryBackoff:    1 * time.Millisecond,
			MaxRetryBackoff: 5 * time.Millisecond,
		},
		WithBaseURL(server.URL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	_, err = tr.Request(ctx, "Health", "Check", nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if attempts != 3 { // Initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestConvertHTTPError(t *testing.T) {
	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Close()

	t.Run("nil error returns nil", func(t *testing.T) {
		result := tr.convertHTTPError(nil, "test.op")
		if result != nil {
			t.Error("expected nil for nil error")
		}
	})

	t.Run("context.Canceled returns canceled error", func(t *testing.T) {
		result := tr.convertHTTPError(context.Canceled, "test.op")
		if result == nil {
			t.Fatal("expected error")
		}
		if !errors.IsCanceled(result) {
			t.Errorf("expected canceled error, got: %v", result)
		}
	})

	t.Run("context.DeadlineExceeded returns timeout error", func(t *testing.T) {
		result := tr.convertHTTPError(context.DeadlineExceeded, "test.op")
		if result == nil {
			t.Fatal("expected error")
		}
		if errors.Code(result) != errors.CodeTimeout {
			t.Errorf("expected timeout error, got: %v", result)
		}
	})
}

// Test file creation for coverage - ensure file is cleaned up
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// TestHTTPURLAddressHandling tests that HTTP URLs are properly parsed and used for REST transport.
// This verifies the fix for the "too many colons in address" error when passing URLs like
// "http://test-node1:8888/mcp" to transports.
func TestHTTPURLAddressHandling(t *testing.T) {
	t.Run("HTTP URL with path is parsed correctly", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "http://test-node1:8888/mcp",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		// Verify baseURL contains scheme and host:port
		if tr.baseURL != "http://test-node1:8888" {
			t.Errorf("expected baseURL 'http://test-node1:8888', got %q", tr.baseURL)
		}

		// Verify basePath contains the path portion
		if tr.basePath != "/mcp" {
			t.Errorf("expected basePath '/mcp', got %q", tr.basePath)
		}
	})

	t.Run("HTTPS URL with path is parsed correctly", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "https://secure.example.com:8443/api/v2",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "https://secure.example.com:8443" {
			t.Errorf("expected baseURL 'https://secure.example.com:8443', got %q", tr.baseURL)
		}

		if tr.basePath != "/api/v2" {
			t.Errorf("expected basePath '/api/v2', got %q", tr.basePath)
		}
	})

	t.Run("HTTP URL without port uses default port 80", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "http://example.com/api",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "http://example.com:80" {
			t.Errorf("expected baseURL 'http://example.com:80', got %q", tr.baseURL)
		}
	})

	t.Run("HTTPS URL without port uses default port 443", func(t *testing.T) {
		tr, err := New(&transport.Config{
			Address: "https://secure.example.com/api",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "https://secure.example.com:443" {
			t.Errorf("expected baseURL 'https://secure.example.com:443', got %q", tr.baseURL)
		}
	})

	t.Run("HTTP URL request includes base path prefix", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the URL path includes the base path prefix
			expectedPath := "/mcp/health"
			if r.URL.Path != expectedPath {
				t.Errorf("expected path %q, got %q", expectedPath, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		// Replace server URL with a path component
		addr := server.URL + "/mcp"

		tr, err := New(&transport.Config{
			Address: addr,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "Health", "Check", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
