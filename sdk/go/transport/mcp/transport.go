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
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// MCPVersion is the supported MCP protocol version.
const MCPVersion = "2024-11-05"

// MCPProtocolVersionHeader is the HTTP header for MCP protocol version.
const MCPProtocolVersionHeader = "MCP-Protocol-Version"

// Transport implements the transport.Transport interface for MCP (Model Context Protocol).
// MCP uses JSON-RPC 2.0 over HTTP for communication.
type Transport struct {
	cfg        *transport.Config
	client     *http.Client
	baseURL    string
	endpoint   string
	headers    map[string]string
	closed     atomic.Bool
	lastHealth atomic.Int64
	requestID  atomic.Uint64

	// Session state
	initialized     atomic.Bool
	serverInfo      *serverInfo
	initMu          sync.Mutex
	protocolVersion string

	// Authentication state
	username      string
	password      string
	accessToken   string
	authenticated atomic.Bool
	authMu        sync.Mutex

	mu sync.RWMutex
}

// serverInfo holds information about the connected MCP server.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// jsonRPCRequest represents a JSON-RPC 2.0 request message.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      uint64      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response message.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// initializeParams represents parameters for the initialize method.
type initializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	ClientInfo      map[string]string `json:"clientInfo"`
	Capabilities    map[string]any    `json:"capabilities"`
}

// initializeResult represents the result of the initialize method.
type initializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	ServerInfo      serverInfo             `json:"serverInfo"`
	Capabilities    map[string]interface{} `json:"capabilities"`
}

// toolCallParams represents parameters for the tools/call method.
type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// toolCallResult represents the result of a tools/call method.
type toolCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// contentItem represents a content item in an MCP response.
type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// New creates a new MCP transport.
func New(cfg *transport.Config, opts ...Option) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("mcp.New", "config is required")
	}

	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("mcp.New", "address is required")
	}

	t := &Transport{
		cfg:             cfg,
		headers:         make(map[string]string),
		protocolVersion: MCPVersion,
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	// Build HTTP client if not provided via options
	if t.client == nil {
		client, err := t.buildHTTPClient()
		if err != nil {
			return nil, err
		}
		t.client = client
	}

	// Build base URL and endpoint only if not explicitly set via options
	if t.baseURL == "" && t.endpoint == "" {
		t.buildBaseURL()
	}

	// Mark as healthy initially
	t.lastHealth.Store(time.Now().UnixNano())

	return t, nil
}

// buildHTTPClient constructs an HTTP client from config.
func (t *Transport) buildHTTPClient() (*http.Client, error) {
	parsed := transport.ParseAddress(t.cfg.Address)

	dialTimeout := t.cfg.Timeout
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second
	}

	httpTransport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: dialTimeout,
		DisableCompression:  false,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	// Configure TLS
	if t.cfg.TLSConfig != nil {
		httpTransport.TLSClientConfig = t.cfg.TLSConfig
	}

	// Configure for Unix socket if needed
	if parsed.IsUnix() {
		socketPath := parsed.Address
		httpTransport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: t.cfg.KeepaliveTime,
			}
			return dialer.DialContext(ctx, "unix", socketPath)
		}
	}

	timeout := t.cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &http.Client{
		Transport: httpTransport,
		Timeout:   timeout,
	}, nil
}

// buildBaseURL constructs the base URL and MCP endpoint from config.
func (t *Transport) buildBaseURL() {
	parsed := transport.ParseAddress(t.cfg.Address)

	// For Unix sockets, use a placeholder host
	if parsed.IsUnix() {
		scheme := "http"
		if t.cfg.TLSConfig != nil {
			scheme = "https"
		}
		t.baseURL = scheme + "://localhost"
		t.endpoint = t.baseURL + "/mcp"
		return
	}

	// For HTTP/HTTPS URLs, use the parsed scheme
	if parsed.IsHTTP() {
		t.baseURL = parsed.Scheme + "://" + parsed.Address
		if parsed.Path != "" {
			// If path already ends with /mcp, use it as-is
			if strings.HasSuffix(parsed.Path, "/mcp") {
				t.endpoint = t.baseURL + parsed.Path
			} else {
				t.endpoint = t.baseURL + parsed.Path + "/mcp"
			}
		} else {
			t.endpoint = t.baseURL + "/mcp"
		}
		return
	}

	// For plain TCP addresses, TLS is required
	t.baseURL = "https://" + parsed.Address
	t.endpoint = t.baseURL + "/mcp"
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	return t.requestWithRetry(ctx, service, method, req)
}

// requestWithRetry executes a request with retry logic.
func (t *Transport) requestWithRetry(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	var lastErr error
	backoff := t.cfg.RetryBackoff
	if backoff == 0 {
		backoff = 100 * time.Millisecond
	}

	maxRetries := t.cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "mcp.Request",
					"request canceled during retry")
			case <-time.After(backoff):
				backoff = minDuration(backoff*2, t.cfg.MaxRetryBackoff)
			}
		}

		resp, err := t.doRequest(ctx, service, method, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		if !errors.IsRetryable(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// doRequest executes a single MCP request via JSON-RPC.
func (t *Transport) doRequest(ctx context.Context, service, method string, reqBody interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("mcp.doRequest", "transport is closed")
	}

	// Ensure MCP session is initialized
	if err := t.ensureInitialized(ctx); err != nil {
		return nil, err
	}

	// Map service/method to MCP tool name
	toolName := t.mapToToolName(service, method)

	// Ensure authenticated for non-public tools
	// Public tools: quicbaas_login, quicbaas_health, quicbaas_liveness, quicbaas_readiness
	if !t.isPublicTool(toolName) {
		if err := t.ensureAuthenticated(ctx); err != nil {
			return nil, err
		}
	}

	// Check if this is a service that needs special handling
	isVector := isMCPVectorService(service)
	isScheduler := isMCPSchedulerService(service)
	isAuth := isMCPAuthService(service)
	isTimeseries := isMCPTimeseriesService(service)
	isCluster := isMCPClusterService(service)
	isEmbedding := isMCPEmbeddingService(service)
	isIAM := isMCPIAMService(service)
	var arguments map[string]interface{}
	var err error

	if isVector {
		// Use vector handler to transform SDK request to MCP arguments
		vh := newVectorHandler(t)
		arguments, err = vh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isScheduler {
		// Use scheduler handler to transform SDK request to MCP arguments
		sh := newSchedulerHandler()
		arguments, err = sh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
		// Inject tenant_id from context if not already present in arguments.
		// The SDK scheduler client methods don't accept tenant as a parameter,
		// so the caller enriches context via transport.ContextWithTenant.
		if _, hasTenant := arguments["tenant_id"]; !hasTenant {
			if tid := transport.TenantFromContext(ctx); tid != "" {
				arguments["tenant_id"] = tid
			}
		}
	} else if isTimeseries {
		// Use timeseries handler to transform SDK request to MCP arguments
		th := newTimeseriesHandler(t)
		arguments, err = th.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isEmbedding {
		// Use embedding handler to transform SDK request to MCP arguments
		eh := newEmbeddingHandler()
		arguments, err = eh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isIAM {
		// Use IAM handler to flatten nested SDK request structs and
		// apply field name transformations for MCP tool handlers
		ih := newIAMHandler(t)
		arguments, err = ih.transformRequest(service, method, reqBody)
		if err != nil {
			return nil, err
		}
	} else {
		// Convert request to tool arguments using generic conversion
		arguments = t.convertToArguments(reqBody)
	}

	// Call the tool via JSON-RPC
	result, err := t.callTool(ctx, toolName, arguments)
	if err != nil {
		return nil, err
	}

	// Transform response for Vector service
	if isVector {
		vh := newVectorHandler(t)
		return vh.transformResponse(method, result)
	}

	// Transform response for Scheduler service
	if isScheduler {
		sh := newSchedulerHandler()
		return sh.transformResponse(method, result)
	}

	// Transform response for TimeSeries service
	if isTimeseries {
		th := newTimeseriesHandler(t)
		return th.transformResponse(method, result)
	}

	// Transform response for Auth service
	if isAuth {
		ah := newAuthHandler(t)
		return ah.transformResponse(method, result)
	}

	// Transform response for Cluster service
	if isCluster {
		return transformClusterResponse(method, result)
	}

	// Transform response for Embedding service
	if isEmbedding {
		eh := newEmbeddingHandler()
		return eh.transformResponse(method, result)
	}

	// Transform response for IAM service
	if isIAM {
		ih := newIAMHandler(t)
		return ih.transformResponse(service, method, result)
	}

	return result, nil
}

// ensureInitialized ensures the MCP session is initialized.
func (t *Transport) ensureInitialized(ctx context.Context) error {
	if t.initialized.Load() {
		return nil
	}

	t.initMu.Lock()
	defer t.initMu.Unlock()

	// Double-check after acquiring lock
	if t.initialized.Load() {
		return nil
	}

	if err := t.initialize(ctx); err != nil {
		return err
	}

	t.initialized.Store(true)
	return nil
}

// ensureAuthenticated ensures the transport is authenticated.
// This is called automatically when credentials are provided.
func (t *Transport) ensureAuthenticated(ctx context.Context) error {
	// Skip if already authenticated or no credentials provided
	if t.authenticated.Load() {
		return nil
	}

	// If no credentials, skip authentication (allow public tools)
	if t.username == "" && t.password == "" {
		return nil
	}

	t.authMu.Lock()
	defer t.authMu.Unlock()

	// Double-check after acquiring lock
	if t.authenticated.Load() {
		return nil
	}

	if err := t.authenticate(ctx); err != nil {
		return err
	}

	t.authenticated.Store(true)
	return nil
}

// authenticate performs authentication using the quicbaas_login tool.
// This tool is public and doesn't require authentication itself.
func (t *Transport) authenticate(ctx context.Context) error {
	// quicbaas_login is a public tool that doesn't require authentication
	params := toolCallParams{
		Name: "quicbaas_login",
		Arguments: map[string]interface{}{
			"username": t.username,
			"password": t.password,
		},
	}

	result, err := t.sendRPC(ctx, "tools/call", params)
	if err != nil {
		return errors.Wrap(err, errors.CodeUnauthorized, "mcp.authenticate",
			"failed to authenticate")
	}

	// Parse tool call result
	var toolResult toolCallResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "mcp.authenticate",
			"failed to parse login response")
	}

	// Check for tool error
	if toolResult.IsError {
		return t.extractToolError(toolResult.Content)
	}

	// Extract token from response
	token, err := t.extractAccessToken(toolResult.Content)
	if err != nil {
		return err
	}

	// Store token and set Authorization header
	t.accessToken = token
	t.mu.Lock()
	t.headers["Authorization"] = "Bearer " + token
	t.mu.Unlock()

	return nil
}

// extractAccessToken extracts the access token from the login response.
func (t *Transport) extractAccessToken(content []contentItem) (string, error) {
	for _, item := range content {
		// MCP server may not set the type field, so just check for non-empty text
		if item.Text != "" {
			// Parse JSON response
			var loginResp map[string]interface{}
			if err := json.Unmarshal([]byte(item.Text), &loginResp); err != nil {
				return "", errors.Wrap(err, errors.CodeInternal, "mcp.extractAccessToken",
					"failed to parse login response JSON")
			}

			// Try PascalCase first (Go struct marshaling without JSON tags)
			if token, ok := loginResp["AccessToken"].(string); ok && token != "" {
				return token, nil
			}

			// Try snake_case as fallback
			if token, ok := loginResp["access_token"].(string); ok && token != "" {
				return token, nil
			}

			return "", errors.New(errors.CodeInternal, "mcp.extractAccessToken",
				"access token not found in login response")
		}
	}

	return "", errors.New(errors.CodeInternal, "mcp.extractAccessToken",
		"no content in login response")
}

// isPublicTool returns true if the tool doesn't require authentication.
func (t *Transport) isPublicTool(toolName string) bool {
	publicTools := map[string]bool{
		"quicbaas_login":     true,
		"quicbaas_health":    true,
		"quicbaas_liveness":  true,
		"quicbaas_readiness": true,
	}
	return publicTools[toolName]
}

// initialize performs the MCP protocol handshake.
func (t *Transport) initialize(ctx context.Context) error {
	params := initializeParams{
		ProtocolVersion: t.protocolVersion,
		ClientInfo: map[string]string{
			"name":    "QuicBaaS Go SDK",
			"version": "1.0.0",
		},
		Capabilities: map[string]any{},
	}

	result, err := t.sendRPC(ctx, "initialize", params)
	if err != nil {
		return &InitializationError{
			Op:      "mcp.initialize",
			Message: "failed to initialize MCP session",
			Err:     err,
		}
	}

	var initResult initializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return &InitializationError{
			Op:      "mcp.initialize",
			Message: "failed to parse initialize response",
			Err:     err,
		}
	}

	t.serverInfo = &initResult.ServerInfo

	return nil
}

// callTool invokes an MCP tool via the tools/call method.
func (t *Transport) callTool(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
	params := toolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	result, err := t.sendRPC(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	// Parse tool call result
	var toolResult toolCallResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, &ParseError{
			Op:      "mcp.callTool",
			Message: "failed to parse tool result",
			Err:     err,
		}
	}

	// Check for tool error
	if toolResult.IsError {
		return nil, t.extractToolError(toolResult.Content)
	}

	// Extract result from content
	return t.extractContent(toolResult.Content)
}

// sendRPC sends a JSON-RPC request and returns the result.
func (t *Transport) sendRPC(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := t.requestID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.sendRPC",
			"failed to encode request body")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sendRPC",
			"failed to create HTTP request")
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)

	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	// Execute request
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, t.convertHTTPError(err, "mcp.sendRPC")
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sendRPC",
			"failed to read response body")
	}

	// Parse JSON-RPC response
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, &ParseError{
			Op:      "mcp.sendRPC",
			Message: "failed to parse JSON-RPC response",
			Err:     err,
		}
	}

	// Check for JSON-RPC error
	if rpcResp.Error != nil {
		return nil, rpcResp.Error.ToSDKError("mcp.sendRPC")
	}

	// Update health timestamp on success
	t.lastHealth.Store(time.Now().UnixNano())

	return rpcResp.Result, nil
}

// mapToToolName maps a service/method combination to an MCP tool name.
// Pattern: quicbaas_<lowercase_operation>
func (t *Transport) mapToToolName(service, method string) string {
	// If method is already a fully-formed tool name (starts with "quicbaas_"),
	// return it as-is. This handles cases where the CLI passes pre-constructed
	// tool names directly.
	if strings.HasPrefix(method, "quicbaas_") {
		return method
	}

	// Check for IAM services first (format: quicbaas.IAM.<Subsystem>)
	// IAM tools have non-uniform naming, so use an explicit lookup table
	if isMCPIAMService(service) {
		if toolName, ok := mapIAMToolName(service, method); ok {
			return toolName
		}
		// Fallback: quicbaas_iam_<subsystem>_<lowercase_method>
		iamIdx := strings.Index(service, ".IAM.")
		if iamIdx >= 0 {
			subsystem := service[iamIdx+5:]
			return "quicbaas_iam_" + strings.ToLower(subsystem) + "_" + camelToSnake(method)
		}
	}

	// Extract service name without package prefix
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}

	// Build tool name based on service and method
	// All tool names use snake_case: quicbaas_<service>_<method_in_snake_case>
	switch serviceName {
	case "KV":
		// KV tools use simple lowercase: quicbaas_get, quicbaas_put, quicbaas_delete
		return "quicbaas_" + strings.ToLower(method)
	case "Batch":
		return "quicbaas_batch_" + camelToSnake(method)
	case "Health":
		switch method {
		case "Check":
			return "quicbaas_health"
		case "Liveness":
			return "quicbaas_liveness"
		case "Readiness":
			return "quicbaas_readiness"
		default:
			return "quicbaas_health_" + camelToSnake(method)
		}
	case "Cluster":
		clusterToolMap := map[string]string{
			"Rebalance":          "quicbaas_rebalance_cluster",
			"GetRebalanceStatus": "quicbaas_rebalance_status",
		}
		if toolName, ok := clusterToolMap[method]; ok {
			return toolName
		}
		return "quicbaas_cluster_" + camelToSnake(method)
	case "Seal":
		return "quicbaas_" + strings.ToLower(method)
	case "KMS":
		return "quicbaas_kms_" + camelToSnake(method)
	case "Secrets":
		return "quicbaas_secrets_" + camelToSnake(method)
	case "Crypto":
		return "quicbaas_crypto_" + camelToSnake(method)
	case "RBAC":
		rbacToolMap := map[string]string{
			"CreateUser": "quicbaas_user_create",
			"DeleteUser": "quicbaas_user_delete",
			"ListUsers":  "quicbaas_user_list",
			"GetUser":    "quicbaas_user_get",
			"UpdateUser": "quicbaas_user_update",
			"CreateRole": "quicbaas_role_create",
			"DeleteRole": "quicbaas_role_delete",
			"ListRoles":  "quicbaas_role_list",
			"AssignRole": "quicbaas_role_assign",
			"RevokeRole": "quicbaas_role_revoke",
		}
		if toolName, ok := rbacToolMap[method]; ok {
			return toolName
		}
		return "quicbaas_rbac_" + camelToSnake(method)
	case "Auth":
		return "quicbaas_" + strings.ToLower(method)
	case "Backup":
		return "quicbaas_backup_" + camelToSnake(method)
	case "Cache":
		switch method {
		case "GetStats":
			return "quicbaas_cache_stats"
		default:
			return "quicbaas_cache_" + camelToSnake(method)
		}
	case "Vector":
		return "quicbaas_vector_" + camelToSnake(method)
	case "Timeseries", "TimeSeries":
		// TimeSeries tools use the pattern: quicbaas_ts_<operation>
		// Maps SDK method names to MCP tool names
		return mapTimeseriesMethodToTool(method)
	case "Operations":
		return "quicbaas_" + strings.ToLower(method)
	case "ObjectStorage", "ObjectStorageAdmin":
		return "quicbaas_object_" + camelToSnake(method)
	case "Scheduler":
		return "quicbaas_scheduler_" + camelToSnake(method)
	case "Embedding":
		// Embedding tools: quicbaas_embedding_<operation>
		// Maps SDK method names to MCP tool names
		return mapEmbeddingMethodToTool(method)
	default:
		// Default: quicbaas_<service>_<method>
		return "quicbaas_" + strings.ToLower(serviceName) + "_" + camelToSnake(method)
	}
}

// mapEmbeddingMethodToTool maps Embedding SDK method names to MCP tool names.
// SDK methods use CamelCase while MCP tools use snake_case with simplified names.
func mapEmbeddingMethodToTool(method string) string {
	embeddingToolMap := map[string]string{
		"GenerateEmbedding":       "quicbaas_embedding_generate",
		"BatchGenerateEmbeddings": "quicbaas_embedding_batch",
		"ListProviders":           "quicbaas_embedding_list_providers",
		"GetProviderInfo":         "quicbaas_embedding_get_provider",
		"SetDefaultProvider":      "quicbaas_embedding_set_default",
		"GetCacheStats":           "quicbaas_embedding_cache_stats",
		"ClearCache":              "quicbaas_embedding_cache_clear",
	}

	if toolName, ok := embeddingToolMap[method]; ok {
		return toolName
	}

	// Fallback for unknown methods
	return "quicbaas_embedding_" + camelToSnake(method)
}

// camelToSnake converts a CamelCase string to snake_case.
// Examples: CreateJob -> create_job, GetJobHistory -> job_history, GetStatus -> status
// Handles acronyms: SearchByID -> search_by_id, GetAPIKey -> get_api_key
func camelToSnake(s string) string {
	// Special cases for scheduler methods that don't follow standard naming
	if s == "GetStatus" {
		return "status"
	}
	if s == "GetJobHistory" {
		return "job_history"
	}

	runes := []rune(s)
	var result strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Insert underscore before uppercase if:
			// 1. Not at the start, AND
			// 2. Either the previous char is lowercase, OR
			//    the next char exists and is lowercase (handles end of acronym like "ID" in "ByID")
			if i > 0 {
				prevLower := unicode.IsLower(runes[i-1])
				nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if prevLower || nextIsLower {
					result.WriteRune('_')
				}
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// convertToArguments converts a request object to MCP tool arguments.
// Field names are converted to snake_case to match MCP tool expectations.
func (t *Transport) convertToArguments(req interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}

	// If already a map, convert keys to snake_case and return
	if m, ok := req.(map[string]interface{}); ok {
		return convertKeysToSnakeCase(m)
	}

	// Convert struct to map via JSON round-trip
	data, err := json.Marshal(req)
	if err != nil {
		return nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil
	}

	// Convert keys to snake_case for MCP tools
	return convertKeysToSnakeCase(args)
}

// convertKeysToSnakeCase recursively converts all map keys to snake_case.
func convertKeysToSnakeCase(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		newKey := camelToSnake(k)
		// Recursively convert nested maps
		if nested, ok := v.(map[string]interface{}); ok {
			v = convertKeysToSnakeCase(nested)
		}
		result[newKey] = v
	}
	return result
}

// extractContent extracts the result from MCP content items.
func (t *Transport) extractContent(content []contentItem) (interface{}, error) {
	if len(content) == 0 {
		return nil, nil
	}

	// Find first text content item
	// Note: Do NOT require item.Type == "text" - the MCP server may return
	// content items without the type field explicitly set. Just check for non-empty text.
	for _, item := range content {
		if item.Text != "" {
			// Parse JSON from text
			var result interface{}
			if err := json.Unmarshal([]byte(item.Text), &result); err != nil {
				// Return raw text if not valid JSON
				return item.Text, nil
			}
			return result, nil
		}
	}

	return nil, nil
}

// extractToolError extracts an error from tool result content.
// Note: Do NOT require item.Type == "text" - the MCP server may return
// content items without the type field explicitly set.
func (t *Transport) extractToolError(content []contentItem) error {
	for _, item := range content {
		if item.Text != "" {
			return errors.New(errors.CodeInternal, "mcp.callTool", item.Text)
		}
	}
	return errors.New(errors.CodeInternal, "mcp.callTool", "tool execution failed")
}

// convertHTTPError converts HTTP client errors to DragonErrors.
func (t *Transport) convertHTTPError(err error, op string) error {
	if err == nil {
		return nil
	}

	// Check for context cancellation
	if stderrors.Is(err, context.Canceled) {
		return errors.Wrap(err, errors.CodeCanceled, op, "request canceled")
	}
	if stderrors.Is(err, context.DeadlineExceeded) {
		return errors.Wrap(err, errors.CodeTimeout, op, "request timeout")
	}

	// Check for network errors
	var netErr net.Error
	if stderrors.As(err, &netErr) {
		if netErr.Timeout() {
			return errors.Wrap(err, errors.CodeTimeout, op, "network timeout")
		}
		return errors.Wrap(err, errors.CodeUnavailable, op, "network error")
	}

	return errors.Wrap(err, errors.CodeConnection, op, "HTTP request failed")
}

// RequestStream initiates a streaming request.
// MCP does not support bidirectional streaming via HTTP transport.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, errors.New(errors.CodeUnimplemented, "mcp.RequestStream",
		"MCP transport does not support bidirectional streaming")
}

// Conn returns the underlying HTTP client.
func (t *Transport) Conn() interface{} {
	return t.client
}

// Close closes the transport.
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close idle connections
	if t.client != nil && t.client.Transport != nil {
		if httpTransport, ok := t.client.Transport.(*http.Transport); ok {
			httpTransport.CloseIdleConnections()
		}
	}

	return nil
}

// Healthy returns true if the transport is healthy.
func (t *Transport) Healthy() bool {
	if t.closed.Load() {
		return false
	}

	// Consider healthy if we had a successful request in the last 30 seconds
	lastHealthTime := t.lastHealth.Load()
	if lastHealthTime == 0 {
		return true // Not yet used, assume healthy
	}

	return time.Since(time.Unix(0, lastHealthTime)) < 30*time.Second
}

// SetHeader sets a custom header for all requests.
func (t *Transport) SetHeader(key, value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.headers[key] = value
}

// SetHeaders sets multiple custom headers.
func (t *Transport) SetHeaders(headers map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range headers {
		t.headers[k] = v
	}
}

// ServerInfo returns information about the connected MCP server.
// Returns nil if the session is not initialized.
func (t *Transport) ServerInfo() *serverInfo {
	return t.serverInfo
}

// IsInitialized returns true if the MCP session has been initialized.
func (t *Transport) IsInitialized() bool {
	return t.initialized.Load()
}

// minDuration returns the minimum of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

// Factory implements transport.Factory for MCP.
type Factory struct{}

// Create creates a new MCP transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg)
}
