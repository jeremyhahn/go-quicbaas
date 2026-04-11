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

package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestNew(t *testing.T) {
	t.Run("creates transport with valid config", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
			Timeout: 30 * time.Second,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer tr.Close()

		if tr == nil {
			t.Fatal("expected transport to be non-nil")
		}
		if tr.endpoint != "http://localhost:8888/mcp" {
			t.Errorf("expected endpoint to be http://localhost:8888/mcp, got %s", tr.endpoint)
		}
	})

	t.Run("returns error with nil config", func(t *testing.T) {
		_, err := New(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("returns error with empty address", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "",
		}

		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty address")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("applies options correctly", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg,
			WithAuthToken("test-token"),
			WithHeader("X-Custom", "value"),
			WithUserAgent("TestClient/1.0"),
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer tr.Close()

		if tr.headers["Authorization"] != "Bearer test-token" {
			t.Errorf("expected Authorization header, got %s", tr.headers["Authorization"])
		}
		if tr.headers["X-Custom"] != "value" {
			t.Errorf("expected X-Custom header, got %s", tr.headers["X-Custom"])
		}
		if tr.headers["User-Agent"] != "TestClient/1.0" {
			t.Errorf("expected User-Agent header, got %s", tr.headers["User-Agent"])
		}
	})

	t.Run("plain TCP defaults to https", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "https://localhost:8888" {
			t.Errorf("expected https base URL for plain TCP, got %s", tr.baseURL)
		}
	})

	t.Run("builds URL with TLS config", func(t *testing.T) {
		cfg := &transport.Config{
			Address:   "localhost:8888",
			TLSConfig: &tls.Config{},
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer tr.Close()

		if tr.baseURL != "https://localhost:8888" {
			t.Errorf("expected https base URL for TLS, got %s", tr.baseURL)
		}
	})

	t.Run("uses default timeout", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer tr.Close()

		if tr.client.Timeout != 30*time.Second {
			t.Errorf("expected default timeout of 30s, got %v", tr.client.Timeout)
		}
	})
}

func TestTransport_Request(t *testing.T) {
	t.Run("successful request with initialization", func(t *testing.T) {
		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)

			// Verify headers
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}
			if r.Header.Get(MCPProtocolVersionHeader) != MCPVersion {
				t.Errorf("expected MCP-Protocol-Version %s, got %s", MCPVersion, r.Header.Get(MCPProtocolVersionHeader))
			}

			// Parse request
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp interface{}
			switch req.Method {
			case "initialize":
				resp = map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			case "tools/call":
				params, _ := req.Params.(map[string]interface{})
				toolName, _ := params["name"].(string)
				if toolName != "quicbaas_put" {
					t.Errorf("expected tool name quicbaas_put, got %s", toolName)
				}
				resp = map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": `{"success":true}`,
						},
					},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshalJSON(resp),
			}); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address: server.URL,
			Timeout: 5 * time.Second,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		result, err := tr.Request(ctx, "quicbaas.KV", "Put", map[string]interface{}{
			"entityType": "test",
			"id":         1,
			"data":       "value",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should have made 2 requests: initialize + tools/call
		if requestCount.Load() != 2 {
			t.Errorf("expected 2 requests, got %d", requestCount.Load())
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map result, got %T", result)
		}
		if resultMap["success"] != true {
			t.Errorf("expected success true, got %v", resultMap["success"])
		}

		// Verify server info was captured
		if tr.ServerInfo() == nil {
			t.Error("expected server info to be set")
		}
		if tr.ServerInfo().Name != "TestServer" {
			t.Errorf("expected server name TestServer, got %s", tr.ServerInfo().Name)
		}
		if !tr.IsInitialized() {
			t.Error("expected transport to be initialized")
		}
	})

	t.Run("subsequent requests skip initialization", func(t *testing.T) {
		var initCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp interface{}
			if req.Method == "initialize" {
				initCount.Add(1)
				resp = map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			} else {
				resp = map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": `{"ok":true}`,
						},
					},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshalJSON(resp),
			}); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address: server.URL,
			Timeout: 5 * time.Second,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()

		// Make multiple requests
		for i := 0; i < 3; i++ {
			_, err := tr.Request(ctx, "quicbaas.KV", "Get", map[string]interface{}{"id": i})
			if err != nil {
				t.Fatalf("request %d failed: %v", i, err)
			}
		}

		// Should only initialize once
		if initCount.Load() != 1 {
			t.Errorf("expected 1 initialization, got %d", initCount.Load())
		}
	})

	t.Run("handles JSON-RPC errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp jsonRPCResponse
			resp.JSONRPC = "2.0"
			resp.ID = req.ID

			if req.Method == "initialize" {
				resp.Result = mustMarshalJSON(map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				})
			} else {
				resp.Error = &JSONRPCError{
					Code:    JSONRPCInternalError,
					Message: "Internal error",
					Data:    "test error details",
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address:    server.URL,
			Timeout:    5 * time.Second,
			MaxRetries: 0,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err == nil {
			t.Fatal("expected error from JSON-RPC error response")
		}

		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatalf("expected QBError, got %T", err)
		}
		if qbErr.Code != errors.CodeInternal {
			t.Errorf("expected internal error code, got %v", qbErr.Code)
		}
	})

	t.Run("handles authentication errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp jsonRPCResponse
			resp.JSONRPC = "2.0"
			resp.ID = req.ID

			if req.Method == "initialize" {
				resp.Result = mustMarshalJSON(map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				})
			} else {
				resp.Error = &JSONRPCError{
					Code:    MCPAuthenticationRequired,
					Message: "Authentication required",
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address:    server.URL,
			Timeout:    5 * time.Second,
			MaxRetries: 0,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "quicbaas.KV", "Put", nil)
		if err == nil {
			t.Fatal("expected authentication error")
		}

		if !errors.IsUnauthorized(err) {
			t.Errorf("expected unauthorized error, got %v", err)
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delay response to allow cancellation
			select {
			case <-r.Context().Done():
				return
			case <-time.After(1 * time.Second):
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address: server.URL,
			Timeout: 5 * time.Second,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		_, err = tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err == nil {
			t.Fatal("expected error from context cancellation")
		}

		if !errors.IsCanceled(err) {
			t.Errorf("expected canceled error, got %v", err)
		}
	})

	t.Run("handles empty content response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp interface{}
			if req.Method == "initialize" {
				resp = map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			} else {
				// Return empty content
				resp = map[string]interface{}{
					"content": []interface{}{},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshalJSON(resp),
			}); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address:    server.URL,
			Timeout:    5 * time.Second,
			MaxRetries: 0,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		result, err := tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result for empty content, got %v", result)
		}
	})

	t.Run("handles raw text response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp interface{}
			if req.Method == "initialize" {
				resp = map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			} else {
				// Return non-JSON text
				resp = map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "plain text response",
						},
					},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshalJSON(resp),
			}); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address:    server.URL,
			Timeout:    5 * time.Second,
			MaxRetries: 0,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		result, err := tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result != "plain text response" {
			t.Errorf("expected raw text result, got %v", result)
		}
	})

	t.Run("handles tool error without content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
				return
			}

			var resp interface{}
			if req.Method == "initialize" {
				resp = map[string]interface{}{
					"protocolVersion": MCPVersion,
					"serverInfo": map[string]interface{}{
						"name":    "TestServer",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			} else {
				// Return tool error with empty content
				resp = map[string]interface{}{
					"content": []interface{}{},
					"isError": true,
				}
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshalJSON(resp),
			}); err != nil {
				t.Errorf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := &transport.Config{
			Address:    server.URL,
			Timeout:    5 * time.Second,
			MaxRetries: 0,
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		ctx := context.Background()
		_, err = tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err == nil {
			t.Fatal("expected error from tool error response")
		}
		if errors.Message(err) != "tool execution failed" {
			t.Errorf("expected default error message, got %v", errors.Message(err))
		}
	})
}

func TestTransport_MapToToolName(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	tests := []struct {
		service  string
		method   string
		expected string
	}{
		// KV operations
		{"quicbaas.KV", "Put", "quicbaas_put"},
		{"quicbaas.KV", "Get", "quicbaas_get"},
		{"quicbaas.KV", "Delete", "quicbaas_delete"},
		{"quicbaas.KV", "List", "quicbaas_list"},

		// Batch operations
		{"quicbaas.Batch", "Put", "quicbaas_batch_put"},
		{"quicbaas.Batch", "Get", "quicbaas_batch_get"},

		// Health operations
		{"quicbaas.Health", "Check", "quicbaas_health"},
		{"quicbaas.Health", "Liveness", "quicbaas_liveness"},
		{"quicbaas.Health", "Readiness", "quicbaas_readiness"},
		{"quicbaas.Health", "Status", "quicbaas_health_status"},

		// Cluster operations
		{"quicbaas.Cluster", "Status", "quicbaas_cluster_status"},
		{"quicbaas.Cluster", "AddNode", "quicbaas_cluster_add_node"},

		// Seal operations
		{"quicbaas.Seal", "Unseal", "quicbaas_unseal"},
		{"quicbaas.Seal", "Seal", "quicbaas_seal"},

		// KMS operations
		{"quicbaas.KMS", "GenerateKey", "quicbaas_kms_generate_key"},
		{"quicbaas.KMS", "Encrypt", "quicbaas_kms_encrypt"},

		// Secrets operations
		{"quicbaas.Secrets", "Store", "quicbaas_secrets_store"},
		{"quicbaas.Secrets", "Get", "quicbaas_secrets_get"},

		// Crypto operations
		{"quicbaas.Crypto", "Sign", "quicbaas_crypto_sign"},
		{"quicbaas.Crypto", "Verify", "quicbaas_crypto_verify"},

		// RBAC operations
		{"quicbaas.RBAC", "CreateUser", "quicbaas_user_create"},
		{"quicbaas.RBAC", "AssignRole", "quicbaas_role_assign"},

		// Auth operations
		{"quicbaas.Auth", "Login", "quicbaas_login"},
		{"quicbaas.Auth", "Logout", "quicbaas_logout"},

		// Backup operations
		{"quicbaas.Backup", "Create", "quicbaas_backup_create"},
		{"quicbaas.Backup", "Restore", "quicbaas_backup_restore"},

		// Cache operations
		{"quicbaas.Cache", "GetStats", "quicbaas_cache_stats"},
		{"quicbaas.Cache", "Clear", "quicbaas_cache_clear"},

		// Vector operations
		{"quicbaas.Vector", "Upsert", "quicbaas_vector_upsert"},
		{"quicbaas.Vector", "Search", "quicbaas_vector_search"},

		// Embedding operations
		{"quicbaas.Embedding", "GenerateEmbedding", "quicbaas_embedding_generate"},
		{"quicbaas.Embedding", "BatchGenerateEmbeddings", "quicbaas_embedding_batch"},
		{"quicbaas.Embedding", "ListProviders", "quicbaas_embedding_list_providers"},
		{"quicbaas.Embedding", "GetProviderInfo", "quicbaas_embedding_get_provider"},
		{"quicbaas.Embedding", "SetDefaultProvider", "quicbaas_embedding_set_default"},
		{"quicbaas.Embedding", "GetCacheStats", "quicbaas_embedding_cache_stats"},
		{"quicbaas.Embedding", "ClearCache", "quicbaas_embedding_cache_clear"},
		// Timeseries operations
		{"quicbaas.Timeseries", "Write", "quicbaas_ts_write_point"},
		{"quicbaas.TimeSeries", "Query", "quicbaas_ts_query"},

		// Operations service
		{"quicbaas.Operations", "Migrate", "quicbaas_migrate"},

		// Object storage
		{"quicbaas.ObjectStorage", "Put", "quicbaas_object_put"},
		{"quicbaas.ObjectStorageAdmin", "CreateBackend", "quicbaas_object_create_backend"},

		// IAM Tenant operations
		{"quicbaas.IAM.Tenant", "Create", "quicbaas_iam_tenant_create"},
		{"quicbaas.IAM.Tenant", "Get", "quicbaas_iam_tenant_get"},
		{"quicbaas.IAM.Tenant", "Update", "quicbaas_iam_tenant_update"},
		{"quicbaas.IAM.Tenant", "Delete", "quicbaas_iam_tenant_delete"},
		{"quicbaas.IAM.Tenant", "List", "quicbaas_iam_tenant_list"},

		// IAM User operations
		{"quicbaas.IAM.User", "Create", "quicbaas_iam_user_create"},
		{"quicbaas.IAM.User", "Get", "quicbaas_iam_user_get"},
		{"quicbaas.IAM.User", "Update", "quicbaas_iam_user_update"},
		{"quicbaas.IAM.User", "Delete", "quicbaas_iam_user_delete"},
		{"quicbaas.IAM.User", "List", "quicbaas_iam_user_list"},

		// IAM Group operations
		{"quicbaas.IAM.Group", "Create", "quicbaas_iam_group_create"},
		{"quicbaas.IAM.Group", "Get", "quicbaas_iam_group_get"},
		{"quicbaas.IAM.Group", "Delete", "quicbaas_iam_group_delete"},

		// IAM Role operations
		{"quicbaas.IAM.Role", "Create", "quicbaas_iam_role_create"},
		{"quicbaas.IAM.Role", "Get", "quicbaas_iam_role_get"},
		{"quicbaas.IAM.Role", "Delete", "quicbaas_iam_role_delete"},

		// IAM Policy operations
		{"quicbaas.IAM.Policy", "Create", "quicbaas_iam_policy_create"},
		{"quicbaas.IAM.Policy", "Get", "quicbaas_iam_policy_get"},
		{"quicbaas.IAM.Policy", "Delete", "quicbaas_iam_policy_delete"},

		// IAM App operations
		{"quicbaas.IAM.App", "Register", "quicbaas_iam_app_register"},
		{"quicbaas.IAM.App", "Get", "quicbaas_iam_app_get"},
		{"quicbaas.IAM.App", "Delete", "quicbaas_iam_app_delete"},

		// IAM Admin operations
		{"quicbaas.IAM.Admin", "AssignmentCreate", "quicbaas_iam_admin_assignment_create"},
		{"quicbaas.IAM.Admin", "CheckPermission", "quicbaas_iam_admin_check_permission"},

		// IAM GDPR operations
		{"quicbaas.IAM.GDPR", "ConsentGrant", "quicbaas_iam_gdpr_consent_grant"},
		{"quicbaas.IAM.GDPR", "ExportRequest", "quicbaas_iam_gdpr_export_request"},

		// IAM MFA operations
		{"quicbaas.IAM.MFA", "EnrollBegin", "quicbaas_iam_mfa_enroll_begin"},
		{"quicbaas.IAM.MFA", "Disable", "quicbaas_iam_mfa_disable"},

		// IAM PEP operations
		{"quicbaas.IAM.PEP", "Check", "quicbaas_pep_check"},
		{"quicbaas.IAM.PEP", "CheckBatch", "quicbaas_pep_check_batch"},

		// IAM Session operations
		{"quicbaas.IAM.Session", "Create", "quicbaas_iam_session_create"},
		{"quicbaas.IAM.Session", "Get", "quicbaas_iam_session_get"},

		// IAM Federation operations
		{"quicbaas.IAM.Federation", "Configure", "quicbaas_iam_federation_configure"},

		// IAM Identity operations
		{"quicbaas.IAM.Identity", "Resolve", "quicbaas_iam_identity_resolve"},

		// IAM Audit operations
		{"quicbaas.IAM.Audit", "Log", "quicbaas_iam_audit_log"},

		// Without package prefix
		{"KV", "Put", "quicbaas_put"},
		{"Health", "Check", "quicbaas_health"},

		// Unknown service (default mapping)
		{"quicbaas.Unknown", "Test", "quicbaas_unknown_test"},
		{"Unknown", "Method", "quicbaas_unknown_method"},
	}

	for _, tc := range tests {
		t.Run(tc.service+"/"+tc.method, func(t *testing.T) {
			result := tr.mapToToolName(tc.service, tc.method)
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestTransport_Close(t *testing.T) {
	t.Run("closes transport successfully", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}

		if tr.closed.Load() {
			t.Error("expected transport to not be closed initially")
		}

		if err := tr.Close(); err != nil {
			t.Errorf("unexpected error closing transport: %v", err)
		}

		if !tr.closed.Load() {
			t.Error("expected transport to be closed")
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}

		// Close multiple times
		for i := 0; i < 3; i++ {
			if err := tr.Close(); err != nil {
				t.Errorf("close %d: unexpected error: %v", i, err)
			}
		}
	})

	t.Run("request fails after close", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}

		if err := tr.Close(); err != nil {
			t.Fatalf("failed to close transport: %v", err)
		}

		ctx := context.Background()
		_, err = tr.Request(ctx, "quicbaas.KV", "Get", nil)
		if err == nil {
			t.Fatal("expected error from closed transport")
		}
	})
}

func TestTransport_Healthy(t *testing.T) {
	t.Run("healthy when not closed", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if !tr.Healthy() {
			t.Error("expected transport to be healthy")
		}
	})

	t.Run("unhealthy when closed", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}

		if err := tr.Close(); err != nil {
			t.Fatalf("failed to close transport: %v", err)
		}

		if tr.Healthy() {
			t.Error("expected transport to be unhealthy when closed")
		}
	})

	t.Run("unhealthy when stale", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "http://localhost:8888",
		}

		tr, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		// Set last health to 1 minute ago (stale)
		staleTime := time.Now().Add(-1 * time.Minute).UnixNano()
		tr.lastHealth.Store(staleTime)

		if tr.Healthy() {
			t.Error("expected transport to be unhealthy when stale")
		}
	})
}

func TestTransport_SetHeader(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	t.Run("sets single header", func(t *testing.T) {
		tr.SetHeader("X-Test", "value")
		if tr.headers["X-Test"] != "value" {
			t.Errorf("expected header X-Test=value, got %s", tr.headers["X-Test"])
		}
	})

	t.Run("sets multiple headers", func(t *testing.T) {
		tr.SetHeaders(map[string]string{
			"X-Header1": "value1",
			"X-Header2": "value2",
		})
		if tr.headers["X-Header1"] != "value1" {
			t.Errorf("expected X-Header1=value1, got %s", tr.headers["X-Header1"])
		}
		if tr.headers["X-Header2"] != "value2" {
			t.Errorf("expected X-Header2=value2, got %s", tr.headers["X-Header2"])
		}
	})
}

func TestTransport_RequestStream(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	_, err = tr.RequestStream(ctx, "quicbaas.KV", "Watch", nil)
	if err == nil {
		t.Fatal("expected error for streaming not supported")
	}

	if !errors.IsUnimplemented(err) {
		t.Errorf("expected unimplemented error, got %v", err)
	}
}

func TestTransport_Conn(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	conn := tr.Conn()
	if conn == nil {
		t.Error("expected conn to be non-nil")
	}

	_, ok := conn.(*http.Client)
	if !ok {
		t.Errorf("expected *http.Client, got %T", conn)
	}
}

func TestTransport_ConvertToArguments(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	t.Run("handles nil input", func(t *testing.T) {
		args := tr.convertToArguments(nil)
		if args != nil {
			t.Errorf("expected nil, got %v", args)
		}
	})

	t.Run("handles map input", func(t *testing.T) {
		input := map[string]interface{}{
			"key": "value",
		}
		args := tr.convertToArguments(input)
		if args["key"] != "value" {
			t.Errorf("expected key=value, got %v", args["key"])
		}
	})

	t.Run("handles struct input", func(t *testing.T) {
		type testStruct struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		}
		input := testStruct{Name: "test", ID: 123}
		args := tr.convertToArguments(input)
		if args["name"] != "test" {
			t.Errorf("expected name=test, got %v", args["name"])
		}
		// JSON unmarshals numbers as float64
		if args["id"] != float64(123) {
			t.Errorf("expected id=123, got %v", args["id"])
		}
	})

	t.Run("handles unmarshalable input", func(t *testing.T) {
		// Channel cannot be marshaled to JSON
		ch := make(chan int)
		args := tr.convertToArguments(ch)
		if args != nil {
			t.Errorf("expected nil for unmarshalable input, got %v", args)
		}
	})
}

func TestFactory_Create(t *testing.T) {
	factory := &Factory{}

	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := factory.Create(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer tr.Close()

	if tr == nil {
		t.Fatal("expected transport to be non-nil")
	}
}

func TestTransport_ToolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			return
		}

		var resp interface{}
		if req.Method == "initialize" {
			resp = map[string]interface{}{
				"protocolVersion": MCPVersion,
				"serverInfo": map[string]interface{}{
					"name":    "TestServer",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			}
		} else {
			// Return tool error via isError flag
			resp = map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Key not found",
					},
				},
				"isError": true,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshalJSON(resp),
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &transport.Config{
		Address:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	_, err = tr.Request(ctx, "quicbaas.KV", "Get", map[string]interface{}{"id": 123})
	if err == nil {
		t.Fatal("expected error from tool error response")
	}

	if errors.Message(err) != "Key not found" {
		t.Errorf("expected error message 'Key not found', got %v", errors.Message(err))
	}
}

func TestOptions(t *testing.T) {
	t.Run("WithHTTPClient", func(t *testing.T) {
		customClient := &http.Client{Timeout: 60 * time.Second}
		cfg := &transport.Config{Address: "http://localhost:8888"}

		tr, err := New(cfg, WithHTTPClient(customClient))
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if tr.client != customClient {
			t.Error("expected custom HTTP client to be set")
		}
	})

	t.Run("WithBaseURL", func(t *testing.T) {
		cfg := &transport.Config{Address: "http://localhost:8888"}

		tr, err := New(cfg, WithBaseURL("https://custom.example.com"))
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if tr.endpoint != "https://custom.example.com/mcp" {
			t.Errorf("expected endpoint https://custom.example.com/mcp, got %s", tr.endpoint)
		}
	})

	t.Run("WithEndpoint", func(t *testing.T) {
		cfg := &transport.Config{Address: "http://localhost:8888"}

		tr, err := New(cfg, WithEndpoint("https://custom.example.com/v1/mcp"))
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if tr.endpoint != "https://custom.example.com/v1/mcp" {
			t.Errorf("expected custom endpoint, got %s", tr.endpoint)
		}
	})

	t.Run("WithProtocolVersion", func(t *testing.T) {
		cfg := &transport.Config{Address: "http://localhost:8888"}

		tr, err := New(cfg, WithProtocolVersion("2025-01-01"))
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if tr.protocolVersion != "2025-01-01" {
			t.Errorf("expected protocol version 2025-01-01, got %s", tr.protocolVersion)
		}
	})

	t.Run("WithHeaders", func(t *testing.T) {
		cfg := &transport.Config{Address: "http://localhost:8888"}

		tr, err := New(cfg, WithHeaders(map[string]string{
			"X-Custom-1": "value1",
			"X-Custom-2": "value2",
		}))
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer tr.Close()

		if tr.headers["X-Custom-1"] != "value1" {
			t.Errorf("expected X-Custom-1=value1, got %s", tr.headers["X-Custom-1"])
		}
		if tr.headers["X-Custom-2"] != "value2" {
			t.Errorf("expected X-Custom-2=value2, got %s", tr.headers["X-Custom-2"])
		}
	})
}

func TestTransport_ServerInfo(t *testing.T) {
	cfg := &transport.Config{
		Address: "http://localhost:8888",
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	// Before initialization, server info should be nil
	if tr.ServerInfo() != nil {
		t.Error("expected server info to be nil before initialization")
	}

	if tr.IsInitialized() {
		t.Error("expected transport to not be initialized")
	}
}

func TestMinDuration(t *testing.T) {
	tests := []struct {
		a, b     time.Duration
		expected time.Duration
	}{
		{1 * time.Second, 2 * time.Second, 1 * time.Second},
		{2 * time.Second, 1 * time.Second, 1 * time.Second},
		{1 * time.Second, 0, 1 * time.Second},
		{0, 1 * time.Second, 0},
		{1 * time.Second, 1 * time.Second, 1 * time.Second},
	}

	for _, tc := range tests {
		result := minDuration(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("minDuration(%v, %v) = %v, expected %v", tc.a, tc.b, result, tc.expected)
		}
	}
}

// mustMarshalJSON marshals value to JSON and returns as json.RawMessage.
func mustMarshalJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
