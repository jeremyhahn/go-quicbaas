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
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig()

	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("expected BaseURL %s, got %s", DefaultBaseURL, cfg.BaseURL)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("expected Timeout %v, got %v", DefaultTimeout, cfg.Timeout)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected MaxRetries %d, got %d", DefaultMaxRetries, cfg.MaxRetries)
	}
	if cfg.RetryBackoff != DefaultRetryBackoff {
		t.Errorf("expected RetryBackoff %v, got %v", DefaultRetryBackoff, cfg.RetryBackoff)
	}
	if cfg.HealthCheckInterval != DefaultHealthCheckInterval {
		t.Errorf("expected HealthCheckInterval %v, got %v", DefaultHealthCheckInterval, cfg.HealthCheckInterval)
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClientConfig
		wantURL string
	}{
		{
			name:    "default config",
			cfg:     ClientConfig{},
			wantURL: DefaultBaseURL,
		},
		{
			name: "custom URL",
			cfg: ClientConfig{
				BaseURL: "http://custom:11434",
			},
			wantURL: "http://custom:11434",
		},
		{
			name: "URL with trailing slash",
			cfg: ClientConfig{
				BaseURL: "http://custom:11434/",
			},
			wantURL: "http://custom:11434",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.cfg)
			if client.BaseURL() != tt.wantURL {
				t.Errorf("expected BaseURL %s, got %s", tt.wantURL, client.BaseURL())
			}
		})
	}
}

func TestClientClose(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	err := client.Close()
	if err != nil {
		t.Errorf("expected Close to return nil, got %v", err)
	}
}

func TestClientDoRequest_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
	})
	defer client.Close()

	ctx := context.Background()
	body, err := client.doRequest(ctx, "GET", "/test", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["result"] != "success" {
		t.Errorf("expected result 'success', got %s", result["result"])
	}
}

func TestClientDoRequest_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "model 'test' not found"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0, // No retries for this test
	})
	defer client.Close()

	ctx := context.Background()
	_, err := client.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClientDoRequest_BadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	defer client.Close()

	ctx := context.Background()
	_, err := client.doRequest(ctx, "POST", "/test", map[string]string{"bad": "data"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClientDoRequest_ServerError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		MaxRetries:   2,
		RetryBackoff: 10 * time.Millisecond,
	})
	defer client.Close()

	ctx := context.Background()
	_, err := client.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should have tried 3 times (initial + 2 retries)
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestClientDoRequest_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
	})
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClientDoRequest_WithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type application/json")
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		if body["key"] != "value" {
			t.Errorf("expected body key 'value', got %s", body["key"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
	})
	defer client.Close()

	ctx := context.Background()
	_, err := client.doRequest(ctx, "POST", "/test", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestIsConnectionRefused(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      ErrRequestFailed,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConnectionRefused(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClientDoRequest_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	ctx := context.Background()
	body, err := client.doRequest(ctx, "GET", "/test", nil)
	// This should succeed - the client just returns raw bytes
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(body) != "not valid json" {
		t.Errorf("expected raw body, got %s", string(body))
	}
}

func TestClientDoRequest_GatewayTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`{"error": "gateway timeout"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	defer client.Close()

	ctx := context.Background()
	_, err := client.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientHandleErrorResponse_UnknownStatus(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	err := client.handleErrorResponse(418, []byte("I'm a teapot"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRequestFailed) {
		t.Errorf("expected ErrRequestFailed, got %v", err)
	}
}

func TestClientHandleErrorResponse_EmptyBody(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	err := client.handleErrorResponse(http.StatusBadRequest, []byte(""))
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Consolidated from coverage_91_test.go
// =============================================================================

// TestIsConnectionRefused_NilError tests isConnectionRefused with nil error.
func TestIsConnectionRefused_NilError(t *testing.T) {
	if isConnectionRefused(nil) {
		t.Error("expected false for nil error")
	}
}

// TestIsConnectionRefused_NonNetError tests isConnectionRefused with non-net error.
func TestIsConnectionRefused_NonNetError(t *testing.T) {
	err := errors.New("some random error")
	if isConnectionRefused(err) {
		t.Error("expected false for non-net error")
	}
}

// TestIsConnectionRefused_NetOpError tests isConnectionRefused with net.OpError.
func TestIsConnectionRefused_NetOpError(t *testing.T) {
	// Create a connection refused error
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ECONNREFUSED,
	}
	if !isConnectionRefused(opErr) {
		t.Error("expected true for connection refused error")
	}
}

// TestIsConnectionRefused_OtherNetError tests isConnectionRefused with other net error.
func TestIsConnectionRefused_OtherNetError(t *testing.T) {
	// Create a different network error
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ETIMEDOUT,
	}
	if isConnectionRefused(opErr) {
		t.Error("expected false for timeout error")
	}
}

// TestHealthChecker_IsAvailable_NilStatus tests IsAvailable with no status stored.
func TestHealthChecker_IsAvailable_NilStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	checker := NewHealthChecker(client)

	// Should be false before any check
	if checker.IsAvailable() {
		t.Error("expected false when no status is stored")
	}
}

// TestHealthChecker_LastStatus_NilStatus tests LastStatus with no status stored.
func TestHealthChecker_LastStatus_NilStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	checker := NewHealthChecker(client)

	// Should return unavailable status when nothing stored
	status := checker.LastStatus()
	if status.Available {
		t.Error("expected Available=false when no status is stored")
	}
}

// TestHealthChecker_GetVersion_Success tests getVersion method.
func TestHealthChecker_GetVersion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			resp := VersionResponse{Version: "0.1.25"}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	checker := NewHealthChecker(client)

	version, err := checker.getVersion(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "0.1.25" {
		t.Errorf("expected version 0.1.25, got %s", version)
	}
}

// TestHealthChecker_GetVersion_InvalidJSON tests getVersion with invalid JSON.
func TestHealthChecker_GetVersion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	checker := NewHealthChecker(client)

	_, err := checker.getVersion(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestModelService_List_InvalidJSON tests List with invalid JSON response.
func TestModelService_List_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewModelService(client)

	_, err := svc.List(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

// TestModelService_Show_InvalidJSON tests Show with invalid JSON response.
func TestModelService_Show_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewModelService(client)

	_, err := svc.Show(context.Background(), "test-model")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

// TestModelService_IsAvailable_ListError tests IsAvailable when List fails.
func TestModelService_IsAvailable_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewModelService(client)

	_, err := svc.IsAvailable(context.Background(), "test-model")
	if err == nil {
		t.Fatal("expected error when List fails")
	}
}

// TestModelService_GetModelInfo_ListError tests GetModelInfo when List fails.
func TestModelService_GetModelInfo_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewModelService(client)

	_, err := svc.GetModelInfo(context.Background(), "test-model")
	if err == nil {
		t.Fatal("expected error when List fails")
	}
}

// TestEmbeddingService_EmbedWithModel_Error tests EmbedWithModel error path.
func TestEmbeddingService_EmbedWithModel_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewEmbeddingService(client, "test-model")

	_, err := svc.EmbedWithModel(context.Background(), "model", "test text")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestEmbeddingService_EmbedBatchSequential_Error tests embedBatchSequential error path.
func TestEmbeddingService_EmbedBatchSequential_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	svc := NewEmbeddingService(client, "test-model")

	_, err := svc.embedBatchSequential(context.Background(), "model", []string{"text1", "text2"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestClientDoRequestOnce_MarshalError tests doRequestOnce with unmarshalable body.
func TestClientDoRequestOnce_MarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL

	// Create a body that cannot be marshaled
	body := make(chan int) // channels cannot be marshaled to JSON

	_, err := client.doRequestOnce(context.Background(), "POST", "/test", body)
	if err == nil {
		t.Fatal("expected error for unmarshalable body")
	}
}

// TestClientDoRequestOnce_NilBody tests doRequestOnce with nil body.
func TestClientDoRequestOnce_NilBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL

	resp, err := client.doRequestOnce(context.Background(), "GET", "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp) == 0 {
		t.Error("expected non-empty response")
	}
}

// TestClientHandleErrorResponse_NotFoundWithModel tests 404 with model in message.
func TestClientHandleErrorResponse_NotFoundWithModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())

	err := client.handleErrorResponse(http.StatusNotFound, []byte(`{"error":"model 'xyz' not found"}`))
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

// TestClientHandleErrorResponse_NotFoundGeneric tests 404 without model.
func TestClientHandleErrorResponse_NotFoundGeneric(t *testing.T) {
	client := NewClient(DefaultClientConfig())

	err := client.handleErrorResponse(http.StatusNotFound, []byte(`{"error":"resource not found"}`))
	if !errors.Is(err, ErrRequestFailed) {
		t.Errorf("expected ErrRequestFailed for generic 404, got %v", err)
	}
}

// TestClientHandleErrorResponse_BadGateway tests 502 error.
func TestClientHandleErrorResponse_BadGateway(t *testing.T) {
	client := NewClient(DefaultClientConfig())

	err := client.handleErrorResponse(http.StatusBadGateway, []byte(`{"error":"bad gateway"}`))
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("expected ErrOllamaUnavailable, got %v", err)
	}
}

// TestClientHandleErrorResponse_ServiceUnavailable tests 503 error.
func TestClientHandleErrorResponse_ServiceUnavailable(t *testing.T) {
	client := NewClient(DefaultClientConfig())

	err := client.handleErrorResponse(http.StatusServiceUnavailable, []byte(`{"error":"service unavailable"}`))
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("expected ErrOllamaUnavailable, got %v", err)
	}
}

// TestClientDoRequest_RetrySuccess tests retry logic success.
func TestClientDoRequest_RetrySuccess(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	cfg := DefaultClientConfig()
	cfg.MaxRetries = 3
	client := NewClient(cfg)
	client.baseURL = server.URL

	_, err := client.doRequest(context.Background(), "GET", "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
}

// TestClientDoRequest_AllRetriesExhausted tests retry exhaustion.
func TestClientDoRequest_AllRetriesExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := DefaultClientConfig()
	cfg.MaxRetries = 2
	client := NewClient(cfg)
	client.baseURL = server.URL

	_, err := client.doRequest(context.Background(), "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}

// TestHealthChecker_Check_WithVersion tests Check method stores version.
func TestHealthChecker_Check_WithVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte("Ollama is running"))
		case "/api/version":
			json.NewEncoder(w).Encode(VersionResponse{Version: "0.1.30"})
		}
	}))
	defer server.Close()

	client := NewClient(DefaultClientConfig())
	client.baseURL = server.URL
	checker := NewHealthChecker(client)

	status, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Available {
		t.Error("expected available=true")
	}
	if status.Version != "0.1.30" {
		t.Errorf("expected version 0.1.30, got %s", status.Version)
	}

	// IsAvailable should now return true
	if !checker.IsAvailable() {
		t.Error("expected IsAvailable() to return true after successful check")
	}
}

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// TestIsConnectionRefused_WithOpError tests isConnectionRefused with net.OpError wrapping ECONNREFUSED.
func TestIsConnectionRefused_WithOpError(t *testing.T) {
	// Create a proper net.OpError with ECONNREFUSED
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ECONNREFUSED,
	}

	if !isConnectionRefused(opErr) {
		t.Error("expected true for connection refused error")
	}
}

// TestIsConnectionRefused_WithOtherSyscallError tests isConnectionRefused with other syscall errors.
func TestIsConnectionRefused_WithOtherSyscallError(t *testing.T) {
	// Create a net.OpError with a different error
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ETIMEDOUT,
	}

	if isConnectionRefused(opErr) {
		t.Error("expected false for timeout error")
	}
}

// TestIsConnectionRefused_WithWrappedError tests isConnectionRefused with wrapped error.
func TestIsConnectionRefused_WithWrappedError(t *testing.T) {
	// Non-net.OpError should return false
	wrappedErr := errors.New("connection error")
	if isConnectionRefused(wrappedErr) {
		t.Error("expected false for non-OpError")
	}
}

// TestModelService_List_RequestError tests List when the request fails.
func TestModelService_List_RequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	svc := NewModelService(client)

	_, err := svc.List(context.Background())
	if err == nil {
		t.Fatal("expected error when request fails")
	}
}

// TestModelService_Show_RequestError tests Show when the request fails.
func TestModelService_Show_RequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "service unavailable"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	svc := NewModelService(client)

	_, err := svc.Show(context.Background(), "test-model")
	if err == nil {
		t.Fatal("expected error when request fails")
	}
}

// TestEmbeddingService_EmbedWithModel_InvalidJSON tests EmbedWithModel with invalid JSON response.
func TestEmbeddingService_EmbedWithModel_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	svc := NewEmbeddingService(client, "test-model")

	_, err := svc.EmbedWithModel(context.Background(), "model", "test text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

// TestEmbeddingService_EmbedWithModel_EmptyEmbedding tests EmbedWithModel with empty embedding response.
func TestEmbeddingService_EmbedWithModel_EmptyEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbedResponse{Embedding: []float32{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	svc := NewEmbeddingService(client, "test-model")

	_, err := svc.EmbedWithModel(context.Background(), "model", "test text")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

// TestClientDoRequest_ContextCanceledDuringBackoff tests context cancellation during retry backoff.
func TestClientDoRequest_ContextCanceledDuringBackoff(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "unavailable"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		MaxRetries:   5,
		RetryBackoff: 500 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	// Should have been cancelled during backoff, so only 1-2 calls
	if callCount > 2 {
		t.Errorf("expected at most 2 calls due to context cancellation, got %d", callCount)
	}
}

// TestClientDoRequest_NoRetryOnInvalidInput tests that invalid input errors are not retried.
func TestClientDoRequest_NoRetryOnInvalidInput(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid input"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 5,
	})

	_, err := client.doRequest(context.Background(), "POST", "/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	// Should not retry on client errors
	if callCount != 1 {
		t.Errorf("expected 1 call (no retries for client error), got %d", callCount)
	}
}

// TestClientDoRequest_NoRetryOnModelNotFound tests that model not found errors are not retried.
func TestClientDoRequest_NoRetryOnModelNotFound(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "model 'xyz' not found"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 5,
	})

	_, err := client.doRequest(context.Background(), "POST", "/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	// Should not retry on model not found
	if callCount != 1 {
		t.Errorf("expected 1 call (no retries for model not found), got %d", callCount)
	}
}

// TestClientDoRequestOnce_ConnectionRefused tests doRequestOnce when connection is refused.
func TestClientDoRequestOnce_ConnectionRefused(t *testing.T) {
	// Use a server that we immediately close to simulate connection refused
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL
	server.Close() // Close immediately to get connection refused

	client := NewClient(ClientConfig{
		BaseURL: serverURL,
		Timeout: 1 * time.Second,
	})

	_, err := client.doRequestOnce(context.Background(), "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for closed server")
	}
	// Should be wrapped as ErrOllamaUnavailable
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("expected ErrOllamaUnavailable, got %v", err)
	}
}

// TestClientDoRequestOnce_ContextDeadlineExceeded tests doRequestOnce with deadline exceeded.
func TestClientDoRequestOnce_ContextDeadlineExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		Timeout: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.doRequestOnce(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for deadline exceeded")
	}
}

// TestHealthChecker_GetVersion_RequestError tests getVersion when request fails.
func TestHealthChecker_GetVersion_RequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	checker := NewHealthChecker(client)

	_, err := checker.getVersion(context.Background())
	if err == nil {
		t.Fatal("expected error for failed request")
	}
}

// TestHealthChecker_Check_StoresError tests that Check stores error in status.
func TestHealthChecker_Check_StoresError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "service unavailable"}`))
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

	// Status should still be returned with error info
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Available {
		t.Error("expected Available=false")
	}
	if status.Error == "" {
		t.Error("expected Error to be set")
	}

	// LastStatus should reflect this
	lastStatus := checker.LastStatus()
	if lastStatus.Error == "" {
		t.Error("expected LastStatus().Error to be set")
	}
}

// TestClientHandleErrorResponse_InvalidJSONBody tests handleErrorResponse with invalid JSON.
func TestClientHandleErrorResponse_InvalidJSONBody(t *testing.T) {
	client := NewClient(DefaultClientConfig())

	// Body that's not valid JSON but not empty
	err := client.handleErrorResponse(http.StatusBadRequest, []byte("plain text error"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

// TestEmbeddingService_EmbedBatchWithModel_EmptyTextInBatch tests EmbedBatchWithModel with empty text in batch.
func TestEmbeddingService_EmbedBatchWithModel_EmptyTextInBatch(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	svc := NewEmbeddingService(client, "test-model")

	_, err := svc.EmbedBatchWithModel(context.Background(), "model", []string{"text1", "", "text3"})
	if err == nil {
		t.Fatal("expected error for empty text in batch")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

// TestEmbeddingService_EmbedBatch_SequentialMode tests batch embedding with small batch (sequential).
func TestEmbeddingService_EmbedBatch_SequentialMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	svc := NewEmbeddingService(client, "test-model")

	// Small batch (<=3) uses sequential processing
	texts := []string{"text1", "text2"}
	embeddings, err := svc.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}
}

// TestEmbeddingService_EmbedBatch_ConcurrentMode tests batch embedding with large batch (concurrent).
func TestEmbeddingService_EmbedBatch_ConcurrentMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	svc := NewEmbeddingService(client, "test-model")

	// Large batch (>3) uses concurrent processing
	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	embeddings, err := svc.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(embeddings) != 5 {
		t.Errorf("expected 5 embeddings, got %d", len(embeddings))
	}
}

// TestEmbeddingService_EmbedBatchConcurrent_ContextCanceled tests concurrent batch with context canceled.
func TestEmbeddingService_EmbedBatchConcurrent_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	svc := NewEmbeddingService(client, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	_, err := svc.embedBatchConcurrent(ctx, "model", texts)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// TestEmbeddingService_EmbedBatchConcurrent_Error tests concurrent batch with request error.
func TestEmbeddingService_EmbedBatchConcurrent_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	svc := NewEmbeddingService(client, "test-model")

	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	_, err := svc.embedBatchConcurrent(context.Background(), "model", texts)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestHealthChecker_IsAvailable_InitialState tests IsAvailable returns false initially.
func TestHealthChecker_IsAvailable_InitialState(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	checker := NewHealthChecker(client)

	// Should be unavailable initially
	if checker.IsAvailable() {
		t.Error("expected false when checker is newly created")
	}
}

// TestHealthChecker_LastStatus_InitialState tests LastStatus returns unavailable initially.
func TestHealthChecker_LastStatus_InitialState(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	checker := NewHealthChecker(client)

	// Should return unavailable status initially
	status := checker.LastStatus()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Available {
		t.Error("expected Available=false initially")
	}
}
