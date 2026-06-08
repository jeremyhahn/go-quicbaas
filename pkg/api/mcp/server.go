//go:build mcp

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

// Package mcp implements the Model Context Protocol (MCP) transport layer for QuicBaaS.
// MCP is a protocol for exposing tools and resources to AI/LLM systems using JSON-RPC 2.0.
//
// The MCP server provides:
// - JSON-RPC 2.0 protocol over stdio or HTTP transport
// - Database operations exposed as MCP "tools"
// - Data exposed as MCP "resources"
// - Full integration with QuicBaaS facade
// - Authorization enforcement for all tool executions
// - RFC 7591 Dynamic Client Registration at /oauth2/register
//
// BREAKING CHANGE (v1.1.0): The _token and accessToken parameters in request body/arguments
// are no longer supported. Authentication tokens MUST be provided via the HTTP Authorization
// header using Bearer token format: "Authorization: Bearer <token>". This change enforces
// RFC 6750 Bearer Token Usage and improves security by preventing token leakage in request logs.
//
// Reference: https://modelcontextprotocol.io/
package mcp

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const (
	// MCPVersion is the supported MCP protocol version.
	MCPVersion = "2024-11-05"

	// MCPProtocolVersionHeader is the HTTP header name for MCP protocol version.
	MCPProtocolVersionHeader = "MCP-Protocol-Version"

	// JSON-RPC 2.0 error codes.
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

	// Custom error codes for authorization.
	AuthenticationRequired = -32001
	InvalidToken           = -32002
	PermissionDenied       = -32003

	// Custom error codes for service errors.
	// These map QuicBaaS service errors to JSON-RPC error codes.
	ResourceNotFound    = -32004
	ResourceConflict    = -32005
	ServiceUnavailable  = -32006
	RequestTimeout      = -32007
	ResourceExhausted   = -32008
	PreconditionFailed  = -32009
	OperationNotAllowed = -32010
	OperationCanceled   = -32011
	InvalidInputError   = -32012
	AlreadyExistsError  = -32013
	UnimplementedError  = -32014
)

// ServerAlreadyRunningError indicates the server is already running.
type ServerAlreadyRunningError struct{}

func (e *ServerAlreadyRunningError) Error() string { return "server already running" }

// ServerNotRunningError indicates the server is not running.
type ServerNotRunningError struct{}

func (e *ServerNotRunningError) Error() string { return "server not running" }

// UnsupportedTransportError indicates an unsupported transport type.
type UnsupportedTransportError struct {
	Transport string
}

func (e *UnsupportedTransportError) Error() string {
	return "unsupported transport: " + e.Transport
}

// Server implements the MCP protocol server.
type Server struct {
	service service.QuicBaaS
	logger  *logger.Logger
	config  *Config

	// Authorization
	authChecker *AuthorizationChecker

	// Runtime state
	running  atomic.Bool
	shutdown chan struct{}
	wg       sync.WaitGroup

	// HTTP server for HTTP transport (atomic for thread-safe access)
	httpServer atomic.Pointer[http.Server]

	// Tools and resources registry
	tools     *ToolRegistry
	resources *ResourceRegistry

	// Client registration (RFC 7591)
	appRegistrar             AppRegistrar
	inMemoryClientStore      *InMemoryClientStore
	enableClientRegistration bool
}

// Config contains MCP server configuration.
type Config struct {
	// ServerName is the MCP server name
	ServerName string

	// ServerVersion is the QuicBaaS version
	ServerVersion string

	// Transport is the transport type: "stdio" or "http"
	Transport string

	// HTTPAddr is the HTTP listen address (for HTTP transport)
	HTTPAddr string

	// TLSConfig, when non-nil, makes the HTTP transport listen for TLS
	// connections (https). The CLAUDE.md security posture requires every
	// public listener to terminate TLS; the daemon passes its in-memory
	// tls.Config (backed by OpaqueKeys) here so MCP shares the same chain
	// as the REST/gRPC/QUIC listeners.
	TLSConfig *tls.Config

	// RequestTimeout is the default timeout for operations
	RequestTimeout time.Duration

	// MaxConcurrentRequests limits concurrent requests
	MaxConcurrentRequests int

	// AuthorizationService for checking permissions (optional)
	AuthorizationService interfaces.AuthorizationService

	// AuthManager for validating tokens (optional)
	AuthManager interfaces.AuthManager

	// MCPAuth contains MCP authorization configuration (optional)
	// When configured, enables RFC 9728 Protected Resource Metadata
	MCPAuth *MCPAuthConfig

	// EnableClientRegistration enables RFC 7591 dynamic client registration endpoint.
	// When enabled without an AppRegistrar, an in-memory store is used.
	EnableClientRegistration bool
}

// NewServer creates a new MCP server instance.
func NewServer(svc service.QuicBaaS, config *Config, log *logger.Logger) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	if log == nil {
		log, _ = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "mcp",
		})
	}

	s := &Server{
		service:                  svc,
		logger:                   log,
		config:                   config,
		shutdown:                 make(chan struct{}),
		enableClientRegistration: config.EnableClientRegistration,
	}

	// Initialize authorization checker with MCP auth config for audience validation
	s.authChecker = NewAuthorizationCheckerWithConfig(config.AuthorizationService, config.AuthManager, config.MCPAuth)

	// Initialize tools and resources registries
	s.tools = NewToolRegistry(svc, log)
	s.resources = NewResourceRegistry(svc, log)

	// Set authorization checker on tools registry
	s.tools.SetAuthorizationChecker(s.authChecker)

	// Initialize in-memory client store if client registration is enabled
	if config.EnableClientRegistration {
		s.inMemoryClientStore = NewInMemoryClientStore()
	}

	return s
}

// DefaultConfig returns default MCP server configuration.
func DefaultConfig() *Config {
	return &Config{
		ServerName:               "QuicBaaS MCP Server",
		ServerVersion:            "1.0.0",
		Transport:                "stdio",
		HTTPAddr:                 "127.0.0.1:8888",
		RequestTimeout:           30 * time.Second,
		MaxConcurrentRequests:    100,
		EnableClientRegistration: false,
	}
}

// SetAuthorizationService sets the authorization service for the server.
// This can be called after construction to configure authorization.
func (s *Server) SetAuthorizationService(authService interfaces.AuthorizationService, authManager interfaces.AuthManager) {
	s.authChecker = NewAuthorizationCheckerWithConfig(authService, authManager, s.config.MCPAuth)
	s.tools.SetAuthorizationChecker(s.authChecker)
}

// EnableQRDBAuthzMapping switches the authorization checker to the qrdb-canonical
// tool -> permission mapper and the default-deny posture for unmapped tools. It
// is called by lifecycle wiring when the qrdb-backed authorization service is
// installed under RequireAuthz=true.
func (s *Server) EnableQRDBAuthzMapping() {
	if s.authChecker != nil {
		s.authChecker.EnableQRDBMapping()
	}
}

// SetMCPAuthConfig sets the MCP authorization configuration.
// This enables audience validation and protected resource metadata.
func (s *Server) SetMCPAuthConfig(config *MCPAuthConfig) {
	s.config.MCPAuth = config
	if s.authChecker != nil {
		s.authChecker.SetMCPAuthConfig(config)
	}
}

// SetAppRegistrar sets the application registrar for dynamic client registration.
// This allows integration with the IAM AppManager for persistent client storage.
// If not set and client registration is enabled, an in-memory store is used.
func (s *Server) SetAppRegistrar(registrar AppRegistrar) {
	s.appRegistrar = registrar
}

// SetBarrierService installs the BarrierServicer used by every
// quicbaas_barrier_* MCP tool. Lifecycle wiring calls this once the qrdb SDK
// BarrierClient is online. Passing nil takes the barrier surface offline;
// tools then return BarrierServiceUnavailableError so callers can distinguish
// "no service" from a transient failure.
func (s *Server) SetBarrierService(svc service.BarrierServicer) {
	if s.tools != nil {
		s.tools.SetBarrierService(svc)
	}
}

// SetCeremonyService installs the CeremonyServicer used by every
// quicbaas_ceremony_* MCP tool. Lifecycle wiring calls this once the qrdb
// SDK CeremonyClient is online. Passing nil takes the ceremony surface
// offline; tools then return CeremonyServiceUnavailableError.
func (s *Server) SetCeremonyService(svc service.CeremonyServicer) {
	if s.tools != nil {
		s.tools.SetCeremonyService(svc)
	}
}

// SetCredentialService installs the CredentialServicer used by every
// qrdb_credential_* MCP tool. Lifecycle wiring calls this once the qrdb
// credential service is online. Passing nil takes the credential surface
// offline; tools then return CredentialServiceUnavailableError.
func (s *Server) SetCredentialService(svc qrdbsdk.ServiceCredentialServicer) {
	if s.tools != nil {
		s.tools.SetCredentialService(svc)
	}
}

// EnableClientRegistrationEndpoint enables or disables the RFC 7591 dynamic client
// registration endpoint. If no AppRegistrar is configured, an in-memory store is used.
func (s *Server) EnableClientRegistrationEndpoint(enable bool) {
	s.enableClientRegistration = enable
	if enable && s.inMemoryClientStore == nil && s.appRegistrar == nil {
		s.inMemoryClientStore = NewInMemoryClientStore()
	}
}

// Start starts the MCP server with the configured transport.
func (s *Server) Start() error {
	if s.running.Swap(true) {
		return &ServerAlreadyRunningError{}
	}

	s.logger.WithFields(map[string]interface{}{
		"transport":   s.config.Transport,
		"server_name": s.config.ServerName,
		"version":     s.config.ServerVersion,
	}).Info("Starting MCP server")

	switch s.config.Transport {
	case "stdio":
		return s.startStdioTransport()
	case "http":
		return s.startHTTPTransport()
	default:
		s.running.Store(false)
		return &UnsupportedTransportError{Transport: s.config.Transport}
	}
}

// Stop gracefully shuts down the MCP server.
func (s *Server) Stop() error {
	if !s.running.Swap(false) {
		return &ServerNotRunningError{}
	}

	s.logger.Info("Stopping MCP server")

	// Signal shutdown
	close(s.shutdown)

	// Stop HTTP server if running (atomic load for thread-safe access)
	if httpSrv := s.httpServer.Load(); httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			s.logger.WithError(err).Error("Error shutting down HTTP server")
		}
	}

	// Wait for all goroutines to finish
	s.wg.Wait()

	s.logger.Info("MCP server stopped")
	return nil
}

// startStdioTransport starts the stdio transport (JSON-RPC over stdin/stdout).
func (s *Server) startStdioTransport() error {
	s.logger.Info("Starting stdio transport")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleStdioRequests()
	}()

	return nil
}

// handleStdioRequests handles JSON-RPC requests from stdin and writes responses to stdout.
func (s *Server) handleStdioRequests() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		select {
		case <-s.shutdown:
			return
		default:
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Parse JSON-RPC request
			var req JSONRPCRequest
			if err := json.Unmarshal(line, &req); err != nil {
				resp := s.makeErrorResponse(nil, ParseError, "Parse error", err.Error())
				if err := encoder.Encode(resp); err != nil {
					s.logger.WithError(err).Error("Failed to write error response")
				}
				continue
			}

			// Handle request with timeout
			ctx, cancel := context.WithTimeout(context.Background(), s.config.RequestTimeout)
			resp := s.handleRequest(ctx, &req)
			cancel()

			// Write response
			if err := encoder.Encode(resp); err != nil {
				s.logger.WithError(err).Error("Failed to write response")
			}
		}
	}

	if err := scanner.Err(); err != nil && !stderrors.Is(err, io.EOF) {
		s.logger.WithError(err).Error("Error reading from stdin")
	}
}

// startHTTPTransport starts the HTTP transport (JSON-RPC over HTTP).
func (s *Server) startHTTPTransport() error {
	s.logger.WithField("addr", s.config.HTTPAddr).Info("Starting HTTP transport")

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleHTTPRequest)
	mux.HandleFunc("/health", s.handleHealthCheck)
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata) // RFC 9728
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleOAuthASMetadata)         // RFC 8414

	// RFC 7591 Dynamic Client Registration
	if s.enableClientRegistration {
		mux.HandleFunc("/oauth2/register", s.handleClientRegistration)
		s.logger.Info("RFC 7591 dynamic client registration enabled at /oauth2/register")
	}

	httpSrv := &http.Server{
		Addr:         s.config.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  s.config.RequestTimeout,
		WriteTimeout: s.config.RequestTimeout,
	}

	// Store HTTP server atomically for thread-safe access from Stop()
	s.httpServer.Store(httpSrv)

	// Start HTTP server in goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		listener, err := net.Listen("tcp", s.config.HTTPAddr)
		if err != nil {
			s.logger.WithError(err).Error("Failed to listen on HTTP address")
			return
		}

		if s.config.TLSConfig != nil {
			httpSrv.TLSConfig = s.config.TLSConfig.Clone()
			tlsListener := tls.NewListener(listener, httpSrv.TLSConfig)
			if err := httpSrv.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
				s.logger.WithError(err).Error("HTTPS server error")
			}
			return
		}

		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("HTTP server error")
		}
	}()

	return nil
}

// handleHTTPRequest handles HTTP POST requests containing JSON-RPC payloads.
func (s *Server) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse JSON-RPC request
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resp := s.makeErrorResponse(nil, ParseError, "Parse error", err.Error())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Extract token from Authorization header for HTTP requests (Bearer only)
	token := ExtractBearerToken(r)
	// Fall back to X-Auth-Token for backwards compatibility
	if token == "" {
		token = r.Header.Get("X-Auth-Token")
	}

	// Create context with token
	ctx := contextWithToken(r.Context(), token)

	// Thread the verified mTLS peer certificate into the context so the unified
	// login ceremony (quicbaas_login_finish) can derive the mtls factor lookup
	// key from the verified handshake rather than a client-supplied value.
	// VerifiedChains guarantees the cert passed chain validation against the
	// configured client CA.
	if r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
		ctx = service.WithVerifiedPeerCert(ctx, r.TLS.VerifiedChains[0][0])
	}

	// Extract and validate MCP-Protocol-Version header
	clientProtocolVersion := r.Header.Get(MCPProtocolVersionHeader)
	if clientProtocolVersion != "" {
		ctx = contextWithProtocolVersion(ctx, clientProtocolVersion)

		// Log warning if client version doesn't match server version
		if clientProtocolVersion != MCPVersion {
			s.logger.WithFields(map[string]interface{}{
				"client_version": clientProtocolVersion,
				"server_version": MCPVersion,
			}).Warn("MCP protocol version mismatch")
		}
	}

	// Handle request
	resp := s.handleRequest(ctx, &req)

	// Check for authorization errors and return proper HTTP status codes
	// per RFC 6750 (Bearer Token Usage)
	if resp.Error != nil {
		realm := DefaultRealm
		if s.config.MCPAuth != nil && s.config.MCPAuth.ResourceURI != "" {
			realm = "quicbaas-mcp"
		}

		switch resp.Error.Code {
		case AuthenticationRequired:
			// Preserve the original error message (e.g., "invalid credentials",
			// "account locked") instead of replacing with generic "authentication required"
			message := resp.Error.Message
			if message == "" {
				message = "authentication required"
			}
			RespondAuthError(w, r, realm, &HTTPAuthError{
				StatusCode: http.StatusUnauthorized,
				Error:      WWWAuthenticateErrorInvalidRequest,
				Message:    message,
			})
			return
		case InvalidToken:
			RespondAuthError(w, r, realm, ErrorInvalidToken(resp.Error.Message))
			return
		case PermissionDenied:
			RespondAuthError(w, r, realm, ErrorPermissionDenied(resp.Error.Message))
			return
		}
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.WithError(err).Error("Failed to write HTTP response")
	}
}

// handleHealthCheck handles health check requests.
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"version": s.config.ServerVersion,
		"mcp":     MCPVersion,
	})
}

// handleRequest processes a JSON-RPC request and returns a response.
func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	s.logger.WithFields(map[string]interface{}{
		"method": req.Method,
		"id":     req.ID,
	}).Debug("Handling MCP request")

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		return s.makeErrorResponse(req.ID, InvalidRequest, "Invalid request", "JSON-RPC version must be 2.0")
	}

	// Route to appropriate handler
	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(ctx, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "ping":
		return s.handlePing(ctx, req)
	default:
		return s.makeErrorResponse(req.ID, MethodNotFound, "Method not found", fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

// handleInitialize handles the "initialize" method.
func (s *Server) handleInitialize(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	result := map[string]interface{}{
		"protocolVersion": MCPVersion,
		"serverInfo": map[string]interface{}{
			"name":    s.config.ServerName,
			"version": s.config.ServerVersion,
		},
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsList handles the "tools/list" method.
// When a valid authentication token is present in the context, the tool list is
// filtered to only include tools the caller is authorized to use. Unauthenticated
// callers see the full tool list for backward compatibility.
func (s *Server) handleToolsList(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	token := tokenFromContext(ctx)

	var tools []ToolDefinition
	if token != "" && s.authChecker != nil {
		tools = s.tools.FilterToolsByAuthorization(ctx, s.authChecker, token)
	} else {
		tools = s.tools.ListTools()
	}

	result := map[string]interface{}{
		"tools": tools,
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsCall handles the "tools/call" method.
func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// Parse parameters
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return s.makeErrorResponse(req.ID, InvalidParams, "Invalid params", "Params must be an object")
	}

	name, ok := params["name"].(string)
	if !ok {
		return s.makeErrorResponse(req.ID, InvalidParams, "Invalid params", "Missing or invalid 'name' parameter")
	}

	arguments, _ := params["arguments"].(map[string]interface{})

	// Token comes from Authorization header only (set in context by HTTP handler)
	// BREAKING CHANGE (v1.1.0): _token and accessToken in params are no longer supported
	token := tokenFromContext(ctx)

	// Check authorization before executing tool; inject Subject into context so
	// service-layer per-key authorization can access the principal uniformly.
	var authErr *AuthorizationError
	ctx, authErr = s.authChecker.CheckToolAuthorizationWithContext(ctx, name, token)
	if authErr != nil {
		return s.handleAuthError(req.ID, authErr)
	}

	// Execute tool
	result, err := s.tools.ExecuteTool(ctx, name, arguments)
	if err != nil {
		// Map error to appropriate JSON-RPC error code
		code, message := s.mapErrorToJSONRPCError(err)
		return s.makeErrorResponse(req.ID, code, message, err.Error())
	}

	// Wrap result in MCP content format
	wrappedResult := s.wrapToolResult(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  wrappedResult,
	}
}

// mapErrorToJSONRPCError maps a service error to a JSON-RPC error code and message.
// This ensures that error types are properly propagated to MCP clients.
func (s *Server) mapErrorToJSONRPCError(err error) (int, string) {
	if err == nil {
		return InternalError, "Unknown error"
	}

	// Check for QBError and map based on error code
	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		switch dErr.Code {
		case dberrors.ErrNotFound:
			return ResourceNotFound, "Resource not found"
		case dberrors.ErrAlreadyExists:
			return AlreadyExistsError, "Resource already exists"
		case dberrors.ErrConflict:
			return ResourceConflict, "Resource conflict"
		case dberrors.ErrInvalidInput, dberrors.ErrValidation:
			return InvalidInputError, "Invalid input"
		case dberrors.ErrUnauthorized:
			return AuthenticationRequired, "Authentication required"
		case dberrors.ErrForbidden, dberrors.ErrNotAllowed:
			return OperationNotAllowed, "Operation not allowed"
		case dberrors.ErrTimeout, dberrors.ErrDeadlineExceeded:
			return RequestTimeout, "Request timeout"
		case dberrors.ErrUnavailable, dberrors.ErrLeaderNotAvailable, dberrors.ErrClusterNotReady:
			return ServiceUnavailable, "Service unavailable"
		case dberrors.ErrResourceExhausted, dberrors.ErrRateLimited:
			return ResourceExhausted, "Resource exhausted"
		case dberrors.ErrPreconditionFailed:
			return PreconditionFailed, "Precondition failed"
		case dberrors.ErrCanceled:
			return OperationCanceled, "Operation canceled"
		case dberrors.ErrUnimplemented, dberrors.ErrNotImplemented:
			return UnimplementedError, "Operation not implemented"
		default:
			return InternalError, "Tool execution failed"
		}
	}

	// Check error message for common patterns (fallback for non-QBError)
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "not found") {
		return ResourceNotFound, "Resource not found"
	}
	if strings.Contains(errMsg, "already exists") {
		return AlreadyExistsError, "Resource already exists"
	}
	if strings.Contains(errMsg, "invalid") {
		return InvalidInputError, "Invalid input"
	}
	if strings.Contains(errMsg, "timeout") {
		return RequestTimeout, "Request timeout"
	}
	if strings.Contains(errMsg, "unavailable") {
		return ServiceUnavailable, "Service unavailable"
	}

	return InternalError, "Tool execution failed"
}

// wrapToolResult wraps a tool execution result in the MCP content format.
// MCP protocol expects tool results to be wrapped in a content array.
func (s *Server) wrapToolResult(result interface{}) map[string]interface{} {
	// Convert result to JSON string for text content
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		// If we can't marshal, return empty content
		return map[string]interface{}{
			"content": []interface{}{},
		}
	}

	return map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": string(jsonBytes),
			},
		},
	}
}

// handleResourcesList handles the "resources/list" method.
func (s *Server) handleResourcesList(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	resources := s.resources.ListResources()

	result := map[string]interface{}{
		"resources": resources,
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleResourcesRead handles the "resources/read" method.
func (s *Server) handleResourcesRead(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// Parse parameters
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return s.makeErrorResponse(req.ID, InvalidParams, "Invalid params", "Params must be an object")
	}

	uri, ok := params["uri"].(string)
	if !ok {
		return s.makeErrorResponse(req.ID, InvalidParams, "Invalid params", "Missing or invalid 'uri' parameter")
	}

	// Read resource
	result, err := s.resources.ReadResource(ctx, uri)
	if err != nil {
		return s.makeErrorResponse(req.ID, InternalError, "Resource read failed", err.Error())
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handlePing handles the "ping" method for connection testing.
func (s *Server) handlePing(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"pong": true},
	}
}

// handleAuthError converts an authorization error to a JSON-RPC error response.
func (s *Server) handleAuthError(id interface{}, authErr *AuthorizationError) *JSONRPCResponse {
	var code int
	switch authErr.Type {
	case AuthErrorMissingToken:
		code = AuthenticationRequired
	case AuthErrorInvalidToken:
		code = InvalidToken
	case AuthErrorPermissionDenied:
		code = PermissionDenied
	default:
		code = InternalError
	}

	return s.makeErrorResponse(id, code, authErr.Message, "")
}

// makeErrorResponse creates a JSON-RPC error response.
func (s *Server) makeErrorResponse(id interface{}, code int, message string, data string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Token context key type for avoiding collisions.
type tokenContextKey struct{}

// contextWithToken adds a token to the context.
func contextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenContextKey{}, token)
}

// tokenFromContext extracts a token from the context.
func tokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(tokenContextKey{}).(string); ok {
		return token
	}
	return ""
}

// mcpProtocolVersionKey is the context key type for MCP protocol version.
type mcpProtocolVersionKey struct{}

// contextWithProtocolVersion adds the MCP protocol version to the context.
func contextWithProtocolVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, mcpProtocolVersionKey{}, version)
}

// ProtocolVersionFromContext extracts the MCP protocol version from the context.
// Returns an empty string if no version was set.
func ProtocolVersionFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(mcpProtocolVersionKey{}).(string); ok {
		return v
	}
	return ""
}

// extractTokenFromHTTPRequest extracts the authentication token from HTTP headers.
func extractTokenFromHTTPRequest(r *http.Request) string {
	// Check Authorization header (Bearer token)
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}

	// Check X-Auth-Token header
	if token := r.Header.Get("X-Auth-Token"); token != "" {
		return token
	}

	return ""
}
