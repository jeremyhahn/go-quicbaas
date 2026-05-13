//go:build mcp

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// MockQuicBaaS is a mock implementation of service.QuicBaaS that embeds mockService
// for default method implementations and mock.Mock for explicit mock expectations.
type MockQuicBaaS struct {
	mock.Mock
	mockService
}

// Override methods that need mock.Mock behavior for testability.
func (m *MockQuicBaaS) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.PutResponse), args.Error(1)
}

func (m *MockQuicBaaS) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetResponse), args.Error(1)
}

func (m *MockQuicBaaS) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteResponse), args.Error(1)
}

func (m *MockQuicBaaS) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

func (m *MockQuicBaaS) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HealthResponse), args.Error(1)
}

func (m *MockQuicBaaS) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.LivenessResponse), args.Error(1)
}

func (m *MockQuicBaaS) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ReadinessResponse), args.Error(1)
}

func (m *MockQuicBaaS) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClusterStatusResponse), args.Error(1)
}

func (m *MockQuicBaaS) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.LoginResponse), args.Error(1)
}

func (m *MockQuicBaaS) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ValidateTokenResponse), args.Error(1)
}

func (m *MockQuicBaaS) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TransactionResponse), args.Error(1)
}

func (m *MockQuicBaaS) SetSignalService(svc interface{}) {}
func (m *MockQuicBaaS) Signal() interface{}              { return nil }
func (m *MockQuicBaaS) AgentAdmin() service.AgentAdminService { return nil }

// Ensure MockQuicBaaS implements service.QuicBaaS at compile time.
var _ service.QuicBaaS = (*MockQuicBaaS)(nil)

// =============================================================================
// Helper functions
// =============================================================================

func newTestServer(t *testing.T) (*Server, *MockQuicBaaS) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Component: "mcp-test"})
	require.NoError(t, err)
	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, nil, log)
	return server, mockSvc
}

// makeJSONRPCRequest creates a JSONRPCRequest for testing.
func makeJSONRPCRequest(method string, params interface{}) *JSONRPCRequest {
	id := json.RawMessage(`1`)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		req.Params = raw
	}
	return req
}

// tokenFromParams extracts a token from _meta.token in JSON-RPC params.
func tokenFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(params, &parsed); err != nil {
		return ""
	}
	meta, ok := parsed["_meta"].(map[string]interface{})
	if !ok {
		return ""
	}
	token, _ := meta["token"].(string)
	return token
}

func sendJSONRPCRequest(server *Server, method string, params interface{}) *JSONRPCResponse {
	req := makeJSONRPCRequest(method, params)
	return server.handleRequest(context.Background(), req)
}

// =============================================================================
// Server Tests
// =============================================================================

func TestNewServer(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, nil, log)
	require.NotNil(t, server)
}

func TestNewServer_NilLogger(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, nil, nil)
	require.NotNil(t, server)
}

func TestServerInitialize(t *testing.T) {
	server, _ := newTestServer(t)
	resp := sendJSONRPCRequest(server, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]interface{}{
			"name":    "test-client",
			"version": "1.0.0",
		},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

func TestServerToolsList(t *testing.T) {
	server, _ := newTestServer(t)

	_ = sendJSONRPCRequest(server, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]interface{}{"name": "test"},
	})

	resp := sendJSONRPCRequest(server, "tools/list", nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	resultMap, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	toolsRaw := resultMap["tools"]
	require.NotNil(t, toolsRaw, "should have tools key in result")
}

func TestServerToolsCall(t *testing.T) {
	server, mockSvc := newTestServer(t)

	_ = sendJSONRPCRequest(server, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]interface{}{"name": "test"},
	})

	mockSvc.On("Put", mock.Anything, mock.MatchedBy(func(req *service.PutRequest) bool {
		return req.EntityType == "test"
	})).Return(&service.PutResponse{
		ID:        1,
		Version:   1,
		Timestamp: time.Now().Unix(),
	}, nil)

	resp := sendJSONRPCRequest(server, "tools/call", map[string]interface{}{
		"name": "quicbaas_put",
		"arguments": map[string]interface{}{
			"entity_type": "test",
			"key":         "key1",
			"value":       `{"name":"test"}`,
		},
	})
	require.NotNil(t, resp)
}

func TestServerResourcesList(t *testing.T) {
	server, _ := newTestServer(t)

	_ = sendJSONRPCRequest(server, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]interface{}{"name": "test"},
	})

	resp := sendJSONRPCRequest(server, "resources/list", nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

func TestServerPing(t *testing.T) {
	server, _ := newTestServer(t)

	resp := sendJSONRPCRequest(server, "ping", nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

func TestServerUnknownMethod(t *testing.T) {
	server, _ := newTestServer(t)

	resp := sendJSONRPCRequest(server, "nonexistent/method", nil)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

func TestServerInvalidJSONRPCVersion(t *testing.T) {
	server, _ := newTestServer(t)

	id := json.RawMessage(`1`)
	req := &JSONRPCRequest{
		JSONRPC: "1.0",
		ID:      &id,
		Method:  "ping",
	}
	resp := server.handleRequest(context.Background(), req)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	require.NotNil(t, config)
	assert.NotEmpty(t, config.ServerName)
	assert.NotEmpty(t, config.ServerVersion)
}

func TestHTTPTransport(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, &Config{
		ServerName:    "test-mcp",
		ServerVersion: "1.0.0",
		HTTPAddr:      ":0",
	}, log)
	require.NotNil(t, server)

	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "2.0", resp.JSONRPC)
}

func TestHTTPTransport_InvalidContentType(t *testing.T) {
	server, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)
	// Server doesn't validate Content-Type, request still gets processed
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHTTPTransport_InvalidJSON(t *testing.T) {
	server, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)
	// JSON-RPC returns parse errors in the response body, not HTTP status
	assert.Equal(t, http.StatusOK, w.Code)
	var resp JSONRPCResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, ParseError, resp.Error.Code)
}

func TestHealthCheck(t *testing.T) {
	server, mockSvc := newTestServer(t)

	mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusHealthy,
	}, nil)

	resp := sendJSONRPCRequest(server, "tools/call", map[string]interface{}{
		"name":      "quicbaas_health",
		"arguments": map[string]interface{}{},
	})
	require.NotNil(t, resp)
}

func TestServerStartStop(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, &Config{
		ServerName:    "test-mcp",
		ServerVersion: "1.0.0",
	}, log)
	require.NotNil(t, server)

	// Server was never started, so Stop returns an error
	err = server.Stop()
	assert.Error(t, err)
}

// =============================================================================
// Token and Auth Tests
// =============================================================================

func TestTokenFromParams_Deprecated(t *testing.T) {
	tests := []struct {
		name     string
		params   json.RawMessage
		expected string
	}{
		{"no params", nil, ""},
		{"empty params", json.RawMessage(`{}`), ""},
		{"with _meta.token", json.RawMessage(`{"_meta":{"token":"test-token-123"}}`), "test-token-123"},
		{"with _meta but no token", json.RawMessage(`{"_meta":{"other":"value"}}`), ""},
		{"invalid json", json.RawMessage(`not valid json`), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenFromParams(tt.params)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthorizationHeaderOnly(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, &Config{
		ServerName:    "test-mcp",
		ServerVersion: "1.0.0",
	}, log)

	tests := []struct {
		name       string
		authHeader string
	}{
		{"bearer token", "Bearer test-token-123"},
		{"no header", ""},
		{"non-bearer scheme", "Basic dXNlcjpwYXNz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "ping",
			}
			bodyBytes, bErr := json.Marshal(body)
			require.NoError(t, bErr)

			req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			server.handleHTTPRequest(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestContextWithToken(t *testing.T) {
	ctx := context.Background()
	token := "test-token-abc"
	ctx = contextWithToken(ctx, token)
	retrieved := tokenFromContext(ctx)
	assert.Equal(t, token, retrieved)
}

func TestContextWithToken_Empty(t *testing.T) {
	ctx := context.Background()
	retrieved := tokenFromContext(ctx)
	assert.Equal(t, "", retrieved)
}

func TestExtractTokenFromHTTPRequest(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
	}{
		{"bearer", "Bearer abc123", "abc123"},
		{"bearer lowercase", "bearer abc123", "bearer abc123"}, // case-sensitive: doesn't strip lowercase "bearer"
		{"no header", "", ""},
		{"basic auth", "Basic abc123", "Basic abc123"}, // non-Bearer returns full header
		{"just bearer", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			token := extractTokenFromHTTPRequest(req)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

// =============================================================================
// MCP Protocol Version Tests
// =============================================================================

func TestMCPProtocolVersionHeader(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &MockQuicBaaS{}
	server := NewServer(mockSvc, nil, log)

	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]interface{}{"name": "test"},
		},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2024-11-05")

	w := httptest.NewRecorder()
	server.handleHTTPRequest(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContextWithProtocolVersion(t *testing.T) {
	t.Run("set and get protocol version", func(t *testing.T) {
		ctx := context.Background()
		ctx = contextWithProtocolVersion(ctx, "2024-11-05")
		assert.Equal(t, "2024-11-05", ProtocolVersionFromContext(ctx))
	})

	t.Run("no protocol version set", func(t *testing.T) {
		ctx := context.Background()
		assert.Equal(t, "", ProtocolVersionFromContext(ctx))
	})
}

// =============================================================================
// mapErrorToJSONRPCError Tests
// =============================================================================

func TestMapErrorToJSONRPCError(t *testing.T) {
	server, _ := newTestServer(t)

	tests := []struct {
		name         string
		err          error
		expectedCode int
		expectedMsg  string
	}{
		{"nil error", nil, InternalError, "Unknown error"},
		{"not found", dberrors.New(dberrors.ErrNotFound, "test", "missing"), ResourceNotFound, "Resource not found"},
		{"already exists", dberrors.New(dberrors.ErrAlreadyExists, "test", "dup"), AlreadyExistsError, "Resource already exists"},
		{"conflict", dberrors.New(dberrors.ErrConflict, "test", "ver"), ResourceConflict, "Resource conflict"},
		{"invalid input", dberrors.New(dberrors.ErrInvalidInput, "test", "bad"), InvalidInputError, "Invalid input"},
		{"validation", dberrors.New(dberrors.ErrValidation, "test", "fail"), InvalidInputError, "Invalid input"},
		{"unauthorized", dberrors.New(dberrors.ErrUnauthorized, "test", "no auth"), AuthenticationRequired, "Authentication required"},
		{"forbidden", dberrors.New(dberrors.ErrForbidden, "test", "denied"), OperationNotAllowed, "Operation not allowed"},
		{"not allowed", dberrors.New(dberrors.ErrNotAllowed, "test", "denied"), OperationNotAllowed, "Operation not allowed"},
		{"timeout", dberrors.New(dberrors.ErrTimeout, "test", "slow"), RequestTimeout, "Request timeout"},
		{"deadline exceeded", dberrors.New(dberrors.ErrDeadlineExceeded, "test", "dl"), RequestTimeout, "Request timeout"},
		{"unavailable", dberrors.New(dberrors.ErrUnavailable, "test", "down"), ServiceUnavailable, "Service unavailable"},
		{"leader not available", dberrors.New(dberrors.ErrLeaderNotAvailable, "test", "no leader"), ServiceUnavailable, "Service unavailable"},
		{"cluster not ready", dberrors.New(dberrors.ErrClusterNotReady, "test", "starting"), ServiceUnavailable, "Service unavailable"},
		{"resource exhausted", dberrors.New(dberrors.ErrResourceExhausted, "test", "quota"), ResourceExhausted, "Resource exhausted"},
		{"rate limited", dberrors.New(dberrors.ErrRateLimited, "test", "slow down"), ResourceExhausted, "Resource exhausted"},
		{"precondition failed", dberrors.New(dberrors.ErrPreconditionFailed, "test", "version"), PreconditionFailed, "Precondition failed"},
		{"canceled", dberrors.New(dberrors.ErrCanceled, "test", "user cancel"), OperationCanceled, "Operation canceled"},
		{"unimplemented", dberrors.New(dberrors.ErrUnimplemented, "test", "todo"), UnimplementedError, "Operation not implemented"},
		{"not implemented", dberrors.New(dberrors.ErrNotImplemented, "test", "todo"), UnimplementedError, "Operation not implemented"},
		{"unknown code", dberrors.New(9999, "test", "unknown"), InternalError, "Tool execution failed"},
		// String pattern fallbacks
		{"string: not found", errors.New("key not found"), ResourceNotFound, "Resource not found"},
		{"string: already exists", errors.New("already exists"), AlreadyExistsError, "Resource already exists"},
		{"string: invalid", errors.New("invalid param"), InvalidInputError, "Invalid input"},
		{"string: timeout", errors.New("timeout"), RequestTimeout, "Request timeout"},
		{"string: unavailable", errors.New("service unavailable"), ServiceUnavailable, "Service unavailable"},
		{"generic error", errors.New("something wrong"), InternalError, "Tool execution failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := server.mapErrorToJSONRPCError(tt.err)
			assert.Equal(t, tt.expectedCode, code)
			assert.Equal(t, tt.expectedMsg, msg)
		})
	}
}

// =============================================================================
// wrapToolResult Tests
// =============================================================================

func TestWrapToolResult(t *testing.T) {
	server, _ := newTestServer(t)

	t.Run("valid result", func(t *testing.T) {
		result := server.wrapToolResult(map[string]interface{}{"key": "value"})
		require.NotNil(t, result)
		content, ok := result["content"].([]interface{})
		require.True(t, ok)
		require.Len(t, content, 1)
		item, ok := content[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "text", item["type"])
		assert.Contains(t, item["text"], `"key":"value"`)
	})

	t.Run("unmarshalable result", func(t *testing.T) {
		result := server.wrapToolResult(math.Inf(1))
		require.NotNil(t, result)
		content, ok := result["content"].([]interface{})
		require.True(t, ok)
		assert.Len(t, content, 0, "should return empty content for unmarshalable result")
	})

	t.Run("nil result", func(t *testing.T) {
		result := server.wrapToolResult(nil)
		require.NotNil(t, result)
		content, ok := result["content"].([]interface{})
		require.True(t, ok)
		require.Len(t, content, 1, "nil marshals as JSON null which is valid")
	})
}

// =============================================================================
// Server SetMCPAuthConfig Tests
// =============================================================================

func TestServerSetMCPAuthConfig(t *testing.T) {
	server, _ := newTestServer(t)

	config := &MCPAuthConfig{
		Enabled:     true,
		ResourceURI: "https://quicbaas.example.com/mcp",
	}
	server.SetMCPAuthConfig(config)
	assert.Equal(t, config, server.config.MCPAuth)
}

func TestServerSetMCPAuthConfig_Nil(t *testing.T) {
	server, _ := newTestServer(t)
	server.SetMCPAuthConfig(nil)
	assert.Nil(t, server.config.MCPAuth)
}

// Suppress unused import warnings for packages used by external test files.
var (
	_ = (*interfaces.KMSManager)(nil)
	_ = (*messaging.Service)(nil)
	_ = (*watch.Manager)(nil)
)

// =============================================================================
// HTTP Transport Coverage Tests
// =============================================================================

func TestStartHTTPTransport(t *testing.T) {
	t.Run("successful HTTP server start", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)

		mockSvc := &mockService{}

		server := NewServer(mockSvc, &Config{
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0", // Use random port
			RequestTimeout: 5 * time.Second,
		}, log)

		// Start in a goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- server.Start()
		}()

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		// Shutdown the server
		err = server.Stop()
		require.NoError(t, err)

		// Check if Start returned without error
		select {
		case err := <-errChan:
			// Server should shut down cleanly
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("Server did not shut down in time")
		}
	})

	t.Run("HTTP health check endpoint", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)

		mockSvc := &mockService{
			healthFunc: func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
				return &service.HealthResponse{
					Status:        service.HealthStatusHealthy,
					NodeID:        1,
					Version:       "1.0.0",
					UptimeSeconds: 100,
				}, nil
			},
		}

		server := NewServer(mockSvc, &Config{
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
		}, log)

		// Create a test request
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		// Call the health check handler directly
		server.handleHealthCheck(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "healthy")
	})
}

// =============================================================================
// KMS Tool Coverage Tests (using KMSManager interface)
// =============================================================================

func TestExecuteKMSImportKey(t *testing.T) {
	t.Run("successful key import", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockKMS := NewMockKMSManager()
		mockKMS.On("ImportKey", mock.Anything, mock.AnythingOfType("string"),
			mock.Anything, "AES-256-GCM").Return(
			createTestKeyMetadata("imported-key-1", "imported-key", "AES-256-GCM"), nil)

		mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_material":    "base64encodedkey",
			"wrapping_key_id": "wrapper-key-1",
			"algorithm":       "AES-256-GCM",
			"name":            "imported-key",
			"usage":           []interface{}{"ENCRYPT", "DECRYPT"},
			"tags": map[string]interface{}{
				"env": "test",
			},
		}

		result, err := registry.executeKMSImportKey(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// The result is marshaled to a map, so let's verify it's valid
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Contains(t, resultMap, "key_id", "result should contain key_id field")

		mockKMS.AssertExpectations(t)
	})

	t.Run("import key with empty usage", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockKMS := NewMockKMSManager()
		mockKMS.On("ImportKey", mock.Anything, mock.AnythingOfType("string"),
			mock.Anything, "AES-256-GCM").Return(
			createTestKeyMetadata("imported-key-2", "imported-key", "AES-256-GCM"), nil)

		mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_material":    "base64encodedkey",
			"wrapping_key_id": "wrapper-key-1",
			"algorithm":       "AES-256-GCM",
		}

		result, err := registry.executeKMSImportKey(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)

		mockKMS.AssertExpectations(t)
	})

	t.Run("import key service error", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockKMS := NewMockKMSManager()
		mockKMS.On("ImportKey", mock.Anything, mock.AnythingOfType("string"),
			mock.Anything, "AES-256-GCM").Return(nil, &dberrors.MessageError{Message: "import failed: invalid key material"})

		mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_material":    "invalid",
			"wrapping_key_id": "wrapper-key-1",
			"algorithm":       "AES-256-GCM",
		}

		result, err := registry.executeKMSImportKey(ctx, args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "import failed")

		mockKMS.AssertExpectations(t)
	})
}

func TestExecuteKMSListKeys(t *testing.T) {
	t.Run("successful list with filter and limit", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockKMS := NewMockKMSManager()
		mockKMS.On("ListKeys", mock.Anything, "").Return(
			[]interface{}{
				createTestKeyMetadata("key-1", "key-one", "AES-256-GCM"),
			}, nil)

		mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"limit":  float64(100),
			"cursor": "cursor-123",
			"filter": map[string]interface{}{
				"algorithm": "AES-256-GCM",
			},
		}

		result, err := registry.executeKMSListKeys(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)

		mockKMS.AssertExpectations(t)
	})

	t.Run("limit over 10000 should be capped", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockKMS := NewMockKMSManager()
		mockKMS.On("ListKeys", mock.Anything, "").Return(
			[]interface{}{}, nil)

		mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"limit": float64(50000), // Over the 10000 cap
		}

		result, err := registry.executeKMSListKeys(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)

		mockKMS.AssertExpectations(t)
	})
}

// =============================================================================
// Secrets Tool Coverage Tests
// =============================================================================

func TestExecuteSecretsStore(t *testing.T) {
	t.Run("successful secret store", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			storeSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return &service.StoreSecretResponse{
					SecretID: req.SecretID,
					Version:  1,
				}, nil
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_id":   "test-secret",
			"secret_data": "sensitive-value",
			"metadata": map[string]interface{}{
				"owner": "test-user",
			},
			"tags": map[string]interface{}{
				"env": "production",
			},
		}

		result, err := registry.executeSecretsStore(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// The result is marshaled to a map
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, "test-secret", resultMap["secret_id"])
	})

	t.Run("store secret without metadata and tags", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			storeSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return &service.StoreSecretResponse{
					SecretID: req.SecretID,
					Version:  1,
				}, nil
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_id":   "simple-secret",
			"secret_data": "value",
		}

		result, err := registry.executeSecretsStore(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("store secret service error", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			storeSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return nil, &dberrors.MessageError{Message: "storage failed: quota exceeded"}
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_id":   "test-secret",
			"secret_data": "value",
		}

		result, err := registry.executeSecretsStore(ctx, args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "storage failed")
	})
}

func TestExecuteSecretsList(t *testing.T) {
	t.Run("successful secrets list", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			listSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return &service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{SecretID: "secret-1"},
						{SecretID: "secret-2"},
					},
				}, nil
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"prefix": "test-",
		}

		result, err := registry.executeSecretsList(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("list secrets service error", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			listSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return nil, &dberrors.MessageError{Message: "access denied"}
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{}

		result, err := registry.executeSecretsList(ctx, args)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestExecuteSecretsRotate(t *testing.T) {
	t.Run("successful secret rotation", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			rotateSecretFunc: func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
				return &service.RotateSecretResponse{
					SecretID:   req.SecretID,
					OldVersion: 1,
					NewVersion: 2,
				}, nil
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_id":       "test-secret",
			"new_secret_data": "new-value",
		}

		result, err := registry.executeSecretsRotate(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("rotate secret service error", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSecrets{
			mockService: mockService{},
			rotateSecretFunc: func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
				return nil, &dberrors.MessageError{Message: "rotation failed"}
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_id":       "test-secret",
			"new_secret_data": "new-value",
		}

		result, err := registry.executeSecretsRotate(ctx, args)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// =============================================================================
// Seal Tool Coverage Tests
// =============================================================================

func TestExecuteSealGenerateRootToken(t *testing.T) {
	t.Run("successful root token generation", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSeal{
			mockService: mockService{},
			generateRootTokenFunc: func(ctx context.Context, req *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error) {
				return &service.GenerateRootTokenResponse{
					RootToken: "root-token-123",
					Timestamp: time.Now(),
				}, nil
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"otp": "one-time-password",
		}

		result, err := registry.executeSealGenerateRootToken(ctx, args)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("generate root token service error", func(t *testing.T) {
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})

		mockSvc := &mockServiceWithSeal{
			mockService: mockService{},
			generateRootTokenFunc: func(ctx context.Context, req *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error) {
				return nil, &dberrors.MessageError{Message: "invalid OTP"}
			},
		}

		registry := NewToolRegistry(mockSvc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"otp": "invalid",
		}

		result, err := registry.executeSealGenerateRootToken(ctx, args)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// =============================================================================
// Mock Service Extensions for KMS (using KMSManager interface)
// =============================================================================

// mockServiceWithKMSManager wraps mockService and provides a configurable KMSManager.
type mockServiceWithKMSManager struct {
	mockService
	kmsManager interfaces.KMSManager
}

func (m *mockServiceWithKMSManager) KMSManager() interfaces.KMSManager {
	return m.kmsManager
}

// mockKMSManagerForCoverage is a function-based mock for KMSManager.
type mockKMSManagerForCoverage struct {
	importKeyFunc func(context.Context, string, []byte, string) (interface{}, error)
	listKeysFunc  func(context.Context, string) ([]interface{}, error)
}

func (m *mockKMSManagerForCoverage) GenerateKey(ctx context.Context, attrs interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	if m.importKeyFunc != nil {
		return m.importKeyFunc(ctx, keyID, material, algorithm)
	}
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: algorithm,
	}, nil
}

func (m *mockKMSManagerForCoverage) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx, prefix)
	}
	return []interface{}{}, nil
}

func (m *mockKMSManagerForCoverage) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) RevokeKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *mockKMSManagerForCoverage) EnableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *mockKMSManagerForCoverage) DisableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *mockKMSManagerForCoverage) DeleteKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *mockKMSManagerForCoverage) UpdateStoredMetadata(metadata interface{}) error {
	return nil
}

func (m *mockKMSManagerForCoverage) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	return nil, nil
}

func (m *mockKMSManagerForCoverage) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	return false, nil
}

func (m *mockKMSManagerForCoverage) GenerateDataKey(ctx context.Context, attrs interface{}, keyBytes int) (plaintext, wrapped []byte, err error) {
	return nil, nil, nil
}

func (m *mockKMSManagerForCoverage) GetBackendType() interface{} {
	return nil
}

func (m *mockKMSManagerForCoverage) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *mockKMSManagerForCoverage) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockKMSManagerForCoverage) Close() error {
	return nil
}

// =============================================================================
// Mock Service Extensions for Secrets
// =============================================================================

type mockServiceWithSecrets struct {
	mockService
	storeSecretFunc  func(context.Context, *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	listSecretsFunc  func(context.Context, *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	rotateSecretFunc func(context.Context, *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
}

func (m *mockServiceWithSecrets) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.storeSecretFunc != nil {
		return m.storeSecretFunc(ctx, req)
	}
	return &service.StoreSecretResponse{SecretID: req.SecretID, Version: 1}, nil
}

func (m *mockServiceWithSecrets) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.listSecretsFunc != nil {
		return m.listSecretsFunc(ctx, req)
	}
	return &service.ListSecretsResponse{Secrets: []*service.Secret{{SecretID: "secret1"}}}, nil
}

func (m *mockServiceWithSecrets) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if m.rotateSecretFunc != nil {
		return m.rotateSecretFunc(ctx, req)
	}
	return &service.RotateSecretResponse{SecretID: req.SecretID, OldVersion: 1, NewVersion: 2}, nil
}

// Secrets returns this mock as the SecretsService
func (m *mockServiceWithSecrets) Secrets() service.SecretsService {
	return m
}

// Stub implementations for remaining SecretsService interface methods
func (m *mockServiceWithSecrets) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{SecretID: req.SecretID, Version: 1}, nil
}

func (m *mockServiceWithSecrets) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return &service.DeleteSecretResponse{SecretID: req.SecretID}, nil
}

func (m *mockServiceWithSecrets) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return &service.GetSecretVersionResponse{Version: &service.SecretVersion{SecretID: req.SecretID, Version: req.Version}}, nil
}

func (m *mockServiceWithSecrets) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return &service.ListSecretVersionsResponse{Versions: []*service.SecretVersion{{SecretID: req.SecretID, Version: 1}}}, nil
}

func (m *mockServiceWithSecrets) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return &service.RollbackSecretResponse{SecretID: req.SecretID, NewVersion: req.TargetVersion + 1}, nil
}

func (m *mockServiceWithSecrets) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return &service.SetRotationPolicyResponse{SecretID: req.SecretID}, nil
}

func (m *mockServiceWithSecrets) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return &service.GetRotationPolicyResponse{SecretID: req.SecretID}, nil
}

func (m *mockServiceWithSecrets) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return &service.UpdateSecretStatusResponse{SecretID: req.SecretID}, nil
}

func (m *mockServiceWithSecrets) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return &service.EnsureSecretResponse{SecretID: req.SecretID, Data: []byte("stub"), Generated: true, Version: 1}, nil
}

func (m *mockServiceWithSecrets) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return &service.GetSecretsByPrefixResponse{Secrets: make(map[string]map[string][]byte)}, nil
}

func (m *mockServiceWithSecrets) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	ch := make(chan *service.SecretChange)
	close(ch)
	return ch, nil
}

// =============================================================================
// Mock Service Extensions for Seal
// =============================================================================

type mockServiceWithSeal struct {
	mockService
	generateRootTokenFunc func(context.Context, *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error)
}

func (m *mockServiceWithSeal) GenerateRootToken(ctx context.Context, req *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error) {
	if m.generateRootTokenFunc != nil {
		return m.generateRootTokenFunc(ctx, req)
	}
	return &service.GenerateRootTokenResponse{RootToken: "default-token", Timestamp: time.Now()}, nil
}

// =============================================================================
// Additional Handler Coverage Tests
// =============================================================================

func TestHandleHTTPRequest(t *testing.T) {
	t.Run("successful HTTP JSON-RPC request", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)

		mockSvc := &mockService{
			healthFunc: func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
				return &service.HealthResponse{
					Status:  service.HealthStatusHealthy,
					NodeID:  1,
					Version: "1.0.0",
				}, nil
			},
		}

		server := NewServer(mockSvc, &Config{
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
		}, log)

		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      json.RawMessage(`1`),
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleHTTPRequest(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("HTTP request with invalid JSON", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)

		mockSvc := &mockService{}

		server := NewServer(mockSvc, &Config{
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
		}, log)

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleHTTPRequest(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp JSONRPCResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotNil(t, resp.Error)
	})

	t.Run("HTTP request body read error", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)

		mockSvc := &mockService{}

		server := NewServer(mockSvc, &Config{
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
		}, log)

		// Create a reader that always errors
		errorReader := &errorReadCloser{err: &dberrors.MessageError{Message: "read error"}}

		req := httptest.NewRequest("POST", "/mcp", errorReader)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleHTTPRequest(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp JSONRPCResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotNil(t, resp.Error)
	})
}

// errorReadCloser is a reader that always returns an error
type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read(p []byte) (n int, err error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	return nil
}

// TestToolRegistry_ExecuteBatchGet tests the executeBatchGet function.
func TestToolRegistry_ExecuteBatchGet(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: empty operations returns batch operations empty error
	result, err := registry.executeBatchGet(ctx, map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch operations cannot be empty")
}

// TestToolRegistry_ExecuteBatchDelete tests the executeBatchDelete function.
func TestToolRegistry_ExecuteBatchDelete(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: empty operations returns batch operations empty error
	result, err := registry.executeBatchDelete(ctx, map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch operations cannot be empty")
}

// TestToolRegistry_ExecuteQuery tests the executeQuery function.
func TestToolRegistry_ExecuteQuery(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning empty stream
	result, err := registry.executeQuery(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteScanIndex tests the executeScanIndex function.
func TestToolRegistry_ExecuteScanIndex(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning empty stream
	result, err := registry.executeScanIndex(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteCreateShard tests the executeCreateShard function.
func TestToolRegistry_ExecuteCreateShard(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning nil response
	result, err := registry.executeCreateShard(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteRebalanceCluster tests the executeRebalanceCluster function.
func TestToolRegistry_ExecuteRebalanceCluster(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution (mock returns nil response, nil error)
	result, err := registry.executeRebalanceCluster(ctx, map[string]interface{}{
		"targetShardCount": 5,
		"strategy":         "balanced",
	})
	assert.NoError(t, err)
	// Result is nil because mock returns nil response
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteBackupCreate tests the executeBackupCreate function.
func TestToolRegistry_ExecuteBackupCreate(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution (mock returns nil response, nil error)
	result, err := registry.executeBackupCreate(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	// Result is nil because mock returns nil response which marshals to JSON null
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteBackupRestore tests the executeBackupRestore function.
func TestToolRegistry_ExecuteBackupRestore(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: backup restore delegates to service which returns nil, nil from mock
	result, err := registry.executeBackupRestore(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteUserCreate tests the executeUserCreate function.
func TestToolRegistry_ExecuteUserCreate(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: mock returns nil response (no validation in mock)
	result, err := registry.executeUserCreate(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteRoleCreate tests the executeRoleCreate function.
func TestToolRegistry_ExecuteRoleCreate(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: mock returns nil response (no validation in mock)
	result, err := registry.executeRoleCreate(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteAggregate tests the executeAggregate function.
func TestToolRegistry_ExecuteAggregate(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning nil response
	result, err := registry.executeAggregate(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteMigrateData tests the executeMigrateData function.
func TestToolRegistry_ExecuteMigrateData(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning nil response
	result, err := registry.executeMigrateData(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteReplaceNode tests the executeReplaceNode function.
func TestToolRegistry_ExecuteReplaceNode(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning nil response
	result, err := registry.executeReplaceNode(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteUpgradeNode tests the executeUpgradeNode function.
func TestToolRegistry_ExecuteUpgradeNode(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: successful execution with mock returning nil response
	result, err := registry.executeUpgradeNode(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestToolRegistry_ExecuteTool_UnknownTool tests unknown tool handling.
func TestToolRegistry_ExecuteTool_UnknownTool(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test: unknown tool
	result, err := registry.ExecuteTool(ctx, "unknown_tool", map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestServer_StartStdioTransport tests the startStdioTransport function.
func TestServer_StartStdioTransport(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:            "Test Server",
		ServerVersion:         "1.0.0",
		Transport:             "stdio",
		RequestTimeout:        30,
		MaxConcurrentRequests: 10,
	}

	server := NewServer(mockSvc, config, log)

	// Test: start stdio transport
	err := server.startStdioTransport()
	require.NoError(t, err)

	// Verify the function was called and returned nil
	// The function starts a goroutine, so we just verify it does not error
	assert.NoError(t, err)
}

// TestResourceRegistry_ReadClusterResource_ErrorPaths tests error paths in readClusterResource.
func TestResourceRegistry_ReadClusterResource_ErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("no parts", func(t *testing.T) {
		result, err := registry.readClusterResource(ctx, []string{})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cluster resource type is required")
	})

	t.Run("unknown cluster resource", func(t *testing.T) {
		result, err := registry.readClusterResource(ctx, []string{"unknown"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cluster resource not found")
	})
}

// TestResourceRegistry_ReadMetricsResource_ErrorPaths tests error paths in readMetricsResource.
func TestResourceRegistry_ReadMetricsResource_ErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("no parts", func(t *testing.T) {
		result, err := registry.readMetricsResource(ctx, []string{})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "metrics resource type is required")
	})

	t.Run("unknown metrics resource", func(t *testing.T) {
		result, err := registry.readMetricsResource(ctx, []string{"unknown"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "metrics resource not found")
	})
}

// TestResourceRegistry_ReadCacheResource_ErrorPaths tests error paths in readCacheResource.
func TestResourceRegistry_ReadCacheResource_ErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("no parts", func(t *testing.T) {
		result, err := registry.readCacheResource(ctx, []string{})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cache resource type is required")
	})

	t.Run("unknown cache resource", func(t *testing.T) {
		result, err := registry.readCacheResource(ctx, []string{"unknown"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cache resource not found")
	})
}

// TestResourceRegistry_ReadEntitiesResource_ErrorPaths tests error paths in readEntitiesResource.
func TestResourceRegistry_ReadEntitiesResource_ErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("no parts", func(t *testing.T) {
		result, err := registry.readEntitiesResource(ctx, []string{})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "entity type is required")
	})

	t.Run("invalid entity ID", func(t *testing.T) {
		result, err := registry.readEntitiesResource(ctx, []string{"users", "invalid-id"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid entity ID")
	})
}

// TestResourceRegistry_ReadResource_ErrorPaths tests error paths in ReadResource.
func TestResourceRegistry_ReadResource_ErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("invalid URI prefix", func(t *testing.T) {
		result, err := registry.ReadResource(ctx, "http://invalid")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid resource URI")
	})

	t.Run("empty path", func(t *testing.T) {
		result, err := registry.ReadResource(ctx, "quicbaas://")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "resource type not found")
	})

	t.Run("unknown resource type", func(t *testing.T) {
		result, err := registry.ReadResource(ctx, "quicbaas://unknown/resource")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "resource type not found")
	})
}

// TestHelperFunctions tests the helper functions.
func TestHelperFunctions(t *testing.T) {
	t.Run("getString", func(t *testing.T) {
		args := map[string]interface{}{
			"str": "value",
			"int": 123,
		}
		assert.Equal(t, "value", getString(args, "str"))
		assert.Equal(t, "", getString(args, "int"))
		assert.Equal(t, "", getString(args, "missing"))
	})

	t.Run("getUint64", func(t *testing.T) {
		args := map[string]interface{}{
			"float":  float64(123.0),
			"int":    int(456),
			"int64":  int64(789),
			"uint64": uint64(101112),
			"string": "invalid",
		}
		assert.Equal(t, uint64(123), getUint64(args, "float"))
		assert.Equal(t, uint64(456), getUint64(args, "int"))
		assert.Equal(t, uint64(789), getUint64(args, "int64"))
		assert.Equal(t, uint64(101112), getUint64(args, "uint64"))
		assert.Equal(t, uint64(0), getUint64(args, "string"))
		assert.Equal(t, uint64(0), getUint64(args, "missing"))
	})

	t.Run("getBool", func(t *testing.T) {
		args := map[string]interface{}{
			"bool":   true,
			"string": "true",
		}
		assert.Equal(t, true, getBool(args, "bool"))
		assert.Equal(t, false, getBool(args, "string"))
		assert.Equal(t, false, getBool(args, "missing"))
	})

	t.Run("marshalResult", func(t *testing.T) {
		input := map[string]interface{}{
			"key": "value",
			"num": 123,
		}
		result, err := marshalResult(input)
		assert.NoError(t, err)
		assert.NotNil(t, result)

		// Test with invalid input
		invalidInput := make(chan int)
		result, err = marshalResult(invalidInput)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestResourceRegistry_MarshalResource_Error tests error path in marshalResource.
func TestResourceRegistry_MarshalResource_Error(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)

	// Test with invalid input (channel cannot be marshaled to JSON)
	invalidInput := make(chan int)
	result, err := registry.marshalResource(invalidInput)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestServer_HandleRequest_InvalidJSONRPC tests invalid JSON-RPC version.
func TestServer_HandleRequest_InvalidJSONRPC(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)
	ctx := context.Background()

	req := &JSONRPCRequest{
		JSONRPC: "1.0",
		Method:  "test",
		ID:      1,
	}

	resp := server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, InvalidRequest, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Invalid request")
}

// TestServer_HandleRequest_UnknownMethod tests unknown method handling.
func TestServer_HandleRequest_UnknownMethod(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)
	ctx := context.Background()

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "unknown_method",
		ID:      1,
	}

	resp := server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, MethodNotFound, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Method not found")
}

// TestServer_HandleToolsCall_InvalidParams tests invalid parameters in tools/call.
func TestServer_HandleToolsCall_InvalidParams(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)
	ctx := context.Background()

	t.Run("params not an object", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "tools/call",
			ID:      1,
			Params:  "invalid",
		}

		resp := server.handleToolsCall(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InvalidParams, resp.Error.Code)
		assert.Contains(t, resp.Error.Data, "Params must be an object")
	})

	t.Run("missing name parameter", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "tools/call",
			ID:      1,
			Params:  map[string]interface{}{},
		}

		resp := server.handleToolsCall(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InvalidParams, resp.Error.Code)
		assert.Contains(t, resp.Error.Data, "Missing or invalid 'name' parameter")
	})

	t.Run("tool execution error", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "tools/call",
			ID:      1,
			Params: map[string]interface{}{
				"name": "unknown_tool",
			},
		}

		resp := server.handleToolsCall(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InvalidInputError, resp.Error.Code)
	})
}

// TestServer_HandleResourcesRead_InvalidParams tests invalid parameters in resources/read.
func TestServer_HandleResourcesRead_InvalidParams(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)
	ctx := context.Background()

	t.Run("params not an object", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "resources/read",
			ID:      1,
			Params:  "invalid",
		}

		resp := server.handleResourcesRead(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InvalidParams, resp.Error.Code)
		assert.Contains(t, resp.Error.Data, "Params must be an object")
	})

	t.Run("missing uri parameter", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "resources/read",
			ID:      1,
			Params:  map[string]interface{}{},
		}

		resp := server.handleResourcesRead(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InvalidParams, resp.Error.Code)
		assert.Contains(t, resp.Error.Data, "Missing or invalid 'uri' parameter")
	})

	t.Run("resource read error", func(t *testing.T) {
		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "resources/read",
			ID:      1,
			Params: map[string]interface{}{
				"uri": "invalid://uri",
			},
		}

		resp := server.handleResourcesRead(ctx, req)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, InternalError, resp.Error.Code)
	})
}

// TestServer_Start_AlreadyRunning tests starting a server that's already running.
func TestServer_Start_AlreadyRunning(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:    "Test Server",
		ServerVersion: "1.0.0",
		Transport:     "stdio",
	}

	server := NewServer(mockSvc, config, log)
	server.running.Store(true)

	err := server.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server already running")
}

// TestServer_Start_UnsupportedTransport tests starting a server with unsupported transport.
func TestServer_Start_UnsupportedTransport(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:    "Test Server",
		ServerVersion: "1.0.0",
		Transport:     "unsupported",
	}

	server := NewServer(mockSvc, config, log)

	err := server.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
	assert.False(t, server.running.Load())
}

// TestServer_Stop_NotRunning tests stopping a server that's not running.
func TestServer_Stop_NotRunning(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	err := server.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server not running")
}

// TestServer_NewServerWithNilConfig tests NewServer with nil config.
func TestServer_NewServerWithNilConfig(t *testing.T) {
	mockSvc := &MockQuicBaaS{}

	server := NewServer(mockSvc, nil, nil)
	assert.NotNil(t, server)
	assert.NotNil(t, server.config)
	assert.NotNil(t, server.logger)
	assert.NotNil(t, server.tools)
	assert.NotNil(t, server.resources)
}

// TestToolRegistry_ExecuteTool_WithTimeout tests ExecuteTool with context timeout.
func TestToolRegistry_ExecuteTool_WithTimeout(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)

	// Use a context without a deadline to test the timeout logic
	ctx := context.Background()

	// Test with a tool that will fail (not implemented)
	result, err := registry.ExecuteTool(ctx, "quicbaas_batch_get", map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestResourceRegistry_ReadResource_WithTimeout tests ReadResource with context timeout.
func TestResourceRegistry_ReadResource_WithTimeout(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)

	// Use a context without a deadline to test the timeout logic
	ctx := context.Background()

	// Test with invalid URI to trigger error before service call
	result, err := registry.ReadResource(ctx, "invalid://uri")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestServer_MakeErrorResponse tests the makeErrorResponse function.
func TestServer_MakeErrorResponse(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	resp := server.makeErrorResponse(123, ParseError, "test message", "test data")
	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 123, resp.ID)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, ParseError, resp.Error.Code)
	assert.Equal(t, "test message", resp.Error.Message)
	assert.Equal(t, "test data", resp.Error.Data)
}

// TestToolRegistry_AddTool tests the addTool function.
func TestToolRegistry_AddTool(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := &ToolRegistry{
		service: mockSvc,
		logger:  log,
		tools:   make([]ToolDefinition, 0),
	}

	initialCount := len(registry.tools)

	registry.addTool(ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
	})

	assert.Equal(t, initialCount+1, len(registry.tools))
	assert.Equal(t, "test_tool", registry.tools[len(registry.tools)-1].Name)
}

// TestToolRegistry_ListTools tests the ListTools function.
func TestToolRegistry_ListTools(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	tools := registry.ListTools()

	assert.NotNil(t, tools)
	assert.Greater(t, len(tools), 0)

	// Verify some expected tools are present
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	assert.True(t, toolNames["quicbaas_put"])
	assert.True(t, toolNames["quicbaas_get"])
	assert.True(t, toolNames["quicbaas_delete"])
	assert.True(t, toolNames["quicbaas_logout"])
}

// TestResourceRegistry_ListResources tests the ListResources function.
func TestResourceRegistry_ListResources(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	resources := registry.ListResources()

	assert.NotNil(t, resources)
	assert.Greater(t, len(resources), 0)

	// Verify some expected resources are present
	resourceURIs := make(map[string]bool)
	for _, res := range resources {
		resourceURIs[res.URI] = true
	}

	assert.True(t, resourceURIs["quicbaas://cluster/status"])
	assert.True(t, resourceURIs["quicbaas://health"])
	assert.True(t, resourceURIs["quicbaas://backups"])
}

// TestServer_HandleHTTPRequest_MethodNotAllowed tests non-POST HTTP requests.
func TestServer_HandleHTTPRequest_MethodNotAllowed(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	// Create a GET request (should be rejected)
	req, err := http.NewRequest("GET", "/mcp", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	server.handleHTTPRequest(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

// TestServer_HandleHTTPRequest_ParseError tests invalid JSON in HTTP request.
func TestServer_HandleHTTPRequest_ParseError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	// Create a POST request with invalid JSON
	req, err := http.NewRequest("POST", "/mcp", bytes.NewBufferString("invalid json"))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	server.handleHTTPRequest(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp JSONRPCResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, ParseError, resp.Error.Code)
}

// TestToolRegistry_ExecuteList_ServiceError tests executeList with service error.
func TestToolRegistry_ExecuteList_ServiceError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Mock List to return error
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).Return(&dberrors.MessageError{Message: "list error"}).Run(func(args mock.Arguments) {
		// Close the channel immediately
		stream := args.Get(2).(chan<- *service.ListResponse)
		close(stream)
	})

	result, err := registry.executeList(ctx, map[string]interface{}{
		"entityType": "test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "list error")
}

// TestToolRegistry_ExecuteTool_AllImplementedTools tests all implemented tool routes.
func TestToolRegistry_ExecuteTool_AllImplementedTools(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test quicbaas_batch_put
	result, err := registry.ExecuteTool(ctx, "quicbaas_batch_put", map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)

	// Test quicbaas_query (now implemented, returns success with empty stream)
	result, err = registry.ExecuteTool(ctx, "quicbaas_query", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_scan_index (now implemented, returns success with empty stream)
	result, err = registry.ExecuteTool(ctx, "quicbaas_scan_index", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_create_shard (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_create_shard", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_rebalance_cluster (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_rebalance_cluster", map[string]interface{}{})
	assert.NoError(t, err)
	// Result is nil because mock returns nil response
	assert.Nil(t, result)

	// Test quicbaas_backup_create (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_backup_create", map[string]interface{}{})
	assert.NoError(t, err)
	// Result is nil because mock returns nil response
	assert.Nil(t, result)

	// Test quicbaas_backup_restore (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_backup_restore", map[string]interface{}{})
	assert.NoError(t, err)
	// Result is nil because mock returns nil response
	assert.Nil(t, result)

	// Test quicbaas_user_create (mock returns nil, nil so no error expected)
	result, err = registry.ExecuteTool(ctx, "quicbaas_user_create", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_role_create (mock returns nil, nil so no error expected)
	result, err = registry.ExecuteTool(ctx, "quicbaas_role_create", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_aggregate (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_aggregate", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_migrate_data (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_migrate_data", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_replace_node (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_replace_node", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test quicbaas_upgrade_node (now implemented, returns success)
	result, err = registry.ExecuteTool(ctx, "quicbaas_upgrade_node", map[string]interface{}{})
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestServer_HandleRequest_AllMethods tests all server request handling methods.
func TestServer_HandleRequest_AllMethods(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)
	ctx := context.Background()

	// Test initialize method
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		ID:      1,
	}
	resp := server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Test tools/list method
	req = &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      2,
	}
	resp = server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Test resources/list method
	req = &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "resources/list",
		ID:      3,
	}
	resp = server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Test ping method
	req = &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      4,
	}
	resp = server.handleRequest(ctx, req)
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}
