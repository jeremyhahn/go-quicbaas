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

// Package rest provides the REST/HTTP transport implementation for the QuicBaaS SDK.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// ErrStreamNotSupported indicates that streaming operations are not supported by REST transport.
var ErrStreamNotSupported = errors.New(errors.CodeUnimplemented, "rest",
	"REST transport does not support bidirectional streaming")

// Transport implements the transport.Transport interface for REST/HTTP.
type Transport struct {
	cfg                 *transport.Config
	client              *http.Client
	baseURL             string
	basePath            string // URL path prefix for all requests (from HTTP URL addresses)
	headers             map[string]string
	closed              atomic.Bool
	lastHealth          atomic.Int64
	authHandler         *authRESTHandler
	iamHandler          *iamRESTHandler
	kvHandler           *kvHandler
	clusterHandler      *clusterHandler
	vectorHandler       *vectorHandler
	ragHandler          *ragHandler
	embeddingHandler    *embeddingHandler
	backupHandler       *backupHandler
	orchestratorHandler *orchestratorHandler

	mu sync.RWMutex
}

// New creates a new REST transport.
func New(cfg *transport.Config, opts ...Option) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("rest.New", "config is required")
	}

	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("rest.New", "address is required")
	}

	t := &Transport{
		cfg:     cfg,
		headers: make(map[string]string),
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

	// Build base URL
	if t.baseURL == "" {
		t.buildBaseURL()
	}

	// Initialize Auth handler
	t.authHandler = newAuthRESTHandler(t)

	// Initialize IAM handler
	t.iamHandler = newIAMRESTHandler(t)

	// Initialize KV handler
	t.kvHandler = newKVHandler(t)

	// Initialize Cluster handler
	t.clusterHandler = newClusterHandler(t)

	// Initialize Vector handler
	t.vectorHandler = newVectorHandler(t)

	// Initialize RAG handler
	t.ragHandler = newRAGHandler(t)

	// Initialize Embedding handler
	t.embeddingHandler = newEmbeddingHandler(t)

	// Initialize Backup handler
	t.backupHandler = newBackupHandler(t)

	// Initialize Orchestrator handler
	t.orchestratorHandler = newOrchestratorHandler(t)

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
		ForceAttemptHTTP2:   false,
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

// buildBaseURL constructs the base URL from config.
// It sets both t.baseURL and t.basePath for HTTP URL addresses that include a path.
func (t *Transport) buildBaseURL() {
	parsed := transport.ParseAddress(t.cfg.Address)

	// For Unix sockets, use a placeholder host
	if parsed.IsUnix() {
		scheme := "http"
		if t.cfg.TLSConfig != nil {
			scheme = "https"
		}
		t.baseURL = scheme + "://localhost"
		t.basePath = ""
		return
	}

	// For HTTP/HTTPS URLs, use the parsed scheme and preserve the path
	if parsed.IsHTTP() {
		t.baseURL = parsed.Scheme + "://" + parsed.Address
		t.basePath = parsed.Path
		return
	}

	// For plain TCP addresses, TLS is required
	t.baseURL = "https://" + parsed.Address
	t.basePath = ""
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	// Route Auth requests through the Auth handler
	if isAuthService(service) {
		return t.authHandler.handleRequest(ctx, service, method, req)
	}

	// Route IAM requests through the IAM handler
	if isIAMService(service) {
		return t.iamHandler.handleRequest(ctx, service, method, req)
	}

	// Route KV requests through the KV handler for proper response type conversion
	if isKVService(service) {
		return t.kvHandler.handleRequest(ctx, method, req)
	}

	// Route Cluster requests through the Cluster handler for proper response type conversion
	if isClusterService(service) {
		return t.clusterHandler.handleRequest(ctx, method, req)
	}

	// Route Vector requests through the Vector handler for proper response type conversion
	if isVectorService(service) {
		return t.vectorHandler.handleRequest(ctx, method, req)
	}

	// Route RAG requests through the RAG handler for proper response type conversion
	if isRAGService(service) {
		return t.ragHandler.handleRequest(ctx, service, method, req)
	}

	// Route Embedding requests through the Embedding handler for proper response type conversion
	if isEmbeddingService(service) {
		return t.embeddingHandler.handleRequest(ctx, service, method, req)
	}

	// Route Backup requests through the Backup handler for proper response type conversion
	if isBackupService(service) {
		return t.backupHandler.handleRequest(ctx, service, method, req)
	}

	// Route Orchestrator requests through the Orchestrator handler for proper response type conversion
	if isOrchestratorService(service) {
		return t.orchestratorHandler.handleRequest(ctx, service, method, req)
	}

	return t.requestWithRetry(ctx, service, method, req)
}

// isKVService returns true if the service is a KV service.
func isKVService(service string) bool {
	// Remove package prefix if present
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "KV"
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
				return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "rest.Request",
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

// doRequest executes a single HTTP request.
func (t *Transport) doRequest(ctx context.Context, service, method string, reqBody interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("rest.doRequest", "transport is closed")
	}

	// Map service/method to REST endpoint
	endpoint, httpMethod := t.mapToEndpoint(service, method)

	// Construct URL with base path prefix if present
	url := t.baseURL + t.basePath + endpoint

	// Encode request body
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.doRequest",
				"failed to encode request body")
		}
		body = bytes.NewReader(data)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, url, body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.doRequest",
			"failed to create HTTP request")
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	// Execute request
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, t.convertHTTPError(err, "rest.doRequest")
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.doRequest",
			"failed to read response body")
	}

	// Check for error status codes
	if resp.StatusCode >= 400 {
		return nil, t.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Decode success response
	var result interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "rest.doRequest",
				"failed to decode response")
		}
	}

	// Update health timestamp on success
	t.lastHealth.Store(time.Now().UnixNano())

	return result, nil
}

// doJSONRequest executes a JSON HTTP request and decodes the response into the provided result.
func (t *Transport) doJSONRequest(ctx context.Context, httpMethod, endpoint string, reqBody interface{}, result interface{}) error {
	if t.closed.Load() {
		return errors.NewConnection("rest.doJSONRequest", "transport is closed")
	}

	// Construct URL with base path prefix if present
	url := t.baseURL + t.basePath + endpoint

	// Encode request body
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, "rest.doJSONRequest",
				"failed to encode request body")
		}
		body = bytes.NewReader(data)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, url, body)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal, "rest.doJSONRequest",
			"failed to create HTTP request")
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	// Execute request
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return t.convertHTTPError(err, "rest.doJSONRequest")
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal, "rest.doJSONRequest",
			"failed to read response body")
	}

	// Check for error status codes
	if resp.StatusCode >= 400 {
		return t.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Decode success response
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return errors.Wrap(err, errors.CodeInternal, "rest.doJSONRequest",
				"failed to decode response")
		}
	}

	// Update health timestamp on success
	t.lastHealth.Store(time.Now().UnixNano())

	return nil
}

// mapToEndpoint maps a gRPC-style service/method to a REST endpoint.
func (t *Transport) mapToEndpoint(service, method string) (string, string) {
	// Remove package prefix if present
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}

	// Common mappings based on the REST API structure
	switch serviceName {
	case "KV":
		return t.mapKVMethod(method)
	case "Batch":
		return t.mapBatchMethod(method)
	case "Transaction":
		return t.mapTransactionMethod(method)
	case "Query":
		return t.mapQueryMethod(method)
	case "Cluster":
		return t.mapClusterMethod(method)
	case "Health":
		return t.mapHealthMethod(method)
	case "KMS":
		return t.mapKMSMethod(method)
	case "Secrets":
		return t.mapSecretsMethod(method)
	case "Seal":
		return t.mapSealMethod(method)
	case "Crypto":
		return t.mapCryptoMethod(method)
	case "Vector":
		return t.mapVectorMethod(method)
	case "Timeseries", "TimeSeries":
		return t.mapTimeseriesMethod(method)
	case "RBAC":
		return t.mapRBACMethod(method)
	case "Backup":
		return t.mapBackupMethod(method)
	case "Auth":
		return t.mapAuthMethod(method)
	case "Cache":
		return t.mapCacheMethod(method)
	case "Operations":
		return t.mapOperationsMethod(method)
	case "Embedding":
		return t.mapEmbeddingMethod(method)
	case "ObjectStorageAdmin":
		return t.mapObjectStorageAdminMethod(method)
	case "Scheduler":
		return t.mapSchedulerMethod(method)
	case "RAG":
		return t.mapRAGMethod(method)
	default:
		// Default mapping: /api/v1/{service}/{method}
		return "/api/v1/" + strings.ToLower(serviceName) + "/" + strings.ToLower(method), http.MethodPost
	}
}

// mapKVMethod maps KV service methods to REST endpoints.
func (t *Transport) mapKVMethod(method string) (string, string) {
	switch method {
	case "Put":
		return "/api/v1/entities/{type}", http.MethodPost
	case "Get":
		return "/api/v1/entities/{type}/{id}", http.MethodGet
	case "Delete":
		return "/api/v1/entities/{type}/{id}", http.MethodDelete
	case "List":
		return "/api/v1/entities/{type}", http.MethodGet
	case "ListEntityTypes":
		return "/api/v1/entities/types", http.MethodGet
	case "BatchPut":
		return "/api/v1/batch/put", http.MethodPost
	case "BatchDelete":
		return "/api/v1/batch/delete", http.MethodPost
	case "Query":
		return "/api/v1/query/{type}/{index}", http.MethodGet
	case "RangeQuery":
		return "/api/v1/query/range", http.MethodPost
	case "Transaction":
		return "/api/v1/transaction", http.MethodPost
	case "Watch":
		// Streaming operation - not supported via REST
		return "/api/v1/kv/watch", http.MethodPost
	default:
		return "/api/v1/kv/" + strings.ToLower(method), http.MethodPost
	}
}

// mapBatchMethod maps Batch service methods to REST endpoints.
func (t *Transport) mapBatchMethod(method string) (string, string) {
	switch method {
	case "Put":
		return "/api/v1/batch/put", http.MethodPost
	case "Get":
		return "/api/v1/batch/get", http.MethodPost
	case "Delete":
		return "/api/v1/batch/delete", http.MethodPost
	default:
		return "/api/v1/batch/" + strings.ToLower(method), http.MethodPost
	}
}

// mapTransactionMethod maps Transaction service methods to REST endpoints.
func (t *Transport) mapTransactionMethod(method string) (string, string) {
	switch method {
	case "Execute":
		return "/api/v1/transaction/execute", http.MethodPost
	case "Begin":
		return "/api/v1/transaction/begin", http.MethodPost
	case "Commit":
		return "/api/v1/transaction/commit", http.MethodPost
	case "Rollback":
		return "/api/v1/transaction/rollback", http.MethodPost
	default:
		return "/api/v1/transaction/" + strings.ToLower(method), http.MethodPost
	}
}

// mapQueryMethod maps Query service methods to REST endpoints.
func (t *Transport) mapQueryMethod(method string) (string, string) {
	switch method {
	case "Query":
		return "/api/v1/query", http.MethodPost
	case "ScanIndex":
		return "/api/v1/query/scan", http.MethodPost
	case "Range":
		return "/api/v1/query/range", http.MethodPost
	default:
		return "/api/v1/query/" + strings.ToLower(method), http.MethodPost
	}
}

// mapClusterMethod maps Cluster service methods to REST endpoints.
func (t *Transport) mapClusterMethod(method string) (string, string) {
	switch method {
	case "Status":
		return "/api/v1/cluster/status", http.MethodGet
	case "GetLeader":
		return "/api/v1/cluster/leader/{shardID}", http.MethodGet
	case "Shards":
		return "/api/v1/cluster/shards", http.MethodGet
	case "AddNode":
		return "/api/v1/cluster/nodes", http.MethodPost
	case "RemoveNode":
		return "/api/v1/cluster/nodes/{nodeID}", http.MethodDelete
	case "ListNodes":
		return "/api/v1/cluster/nodes", http.MethodGet
	case "CreateShard":
		return "/api/v1/cluster/shards", http.MethodPost
	case "StartRebalance", "Rebalance":
		return "/api/v1/cluster/rebalance", http.MethodPost
	case "RebalanceStatus", "GetRebalanceStatus", "GetRebalanceProgress":
		return "/api/v1/cluster/rebalance/status", http.MethodGet
	default:
		return "/api/v1/cluster/" + strings.ToLower(method), http.MethodPost
	}
}

// mapHealthMethod maps Health service methods to REST endpoints.
func (t *Transport) mapHealthMethod(method string) (string, string) {
	switch method {
	case "Check", "Health":
		return "/health", http.MethodGet
	case "Liveness":
		return "/health/live", http.MethodGet
	case "Readiness":
		return "/health/ready", http.MethodGet
	default:
		return "/health/" + strings.ToLower(method), http.MethodGet
	}
}

// mapKMSMethod maps KMS service methods to REST endpoints.
func (t *Transport) mapKMSMethod(method string) (string, string) {
	switch method {
	// Key operations
	case "GenerateKey":
		return "/api/v1/kms/keys", http.MethodPost
	case "ImportKey":
		return "/api/v1/kms/keys/import", http.MethodPost
	case "ExportKey":
		return "/api/v1/kms/keys/{name}/export", http.MethodPost
	case "ListKeys":
		return "/api/v1/kms/keys", http.MethodGet
	case "GetKey":
		return "/api/v1/kms/keys/{name}", http.MethodGet
	case "RotateKey":
		return "/api/v1/kms/keys/{name}/rotate", http.MethodPost
	case "RevokeKey":
		return "/api/v1/kms/keys/{name}/revoke", http.MethodPost
	case "DeleteKey":
		return "/api/v1/kms/keys/{name}", http.MethodDelete
	case "UnwrapKey":
		return "/api/v1/kms/keys/{name}/unwrap", http.MethodPost

	// Key metadata operations
	case "GetKeyMetadata":
		return "/api/v1/kms/keys/{name}/metadata", http.MethodGet
	case "UpdateKeyMetadata":
		return "/api/v1/kms/keys/{name}/metadata", http.MethodPut
	case "UpdateKeyStatus":
		return "/api/v1/kms/keys/{name}/status", http.MethodPut
	case "ListKeyVersions":
		return "/api/v1/kms/keys/{name}/versions", http.MethodGet

	// Key policy operations
	case "SetKeyPolicy":
		return "/api/v1/kms/keys/{name}/policy", http.MethodPut
	case "GetKeyPolicy":
		return "/api/v1/kms/keys/{name}/policy", http.MethodGet

	// Key safety operations
	case "SetKeySafety":
		return "/api/v1/kms/keys/{name}/safety", http.MethodPut
	case "ClearKeySafety":
		return "/api/v1/kms/keys/{name}/safety", http.MethodDelete
	case "SafetyCheck":
		return "/api/v1/kms/safety/check", http.MethodPost
	case "SafetyReport":
		return "/api/v1/kms/safety/report", http.MethodGet

	// Encryption operations
	case "Encrypt":
		return "/api/v1/kms/encrypt/{name}", http.MethodPost
	case "Decrypt":
		return "/api/v1/kms/decrypt/{name}", http.MethodPost

	// Backend operations
	case "GetSupportedAlgorithms":
		return "/api/v1/kms/algorithms", http.MethodGet
	case "BackendStatus":
		return "/api/v1/kms/backend/status", http.MethodGet
	case "BackendTest":
		return "/api/v1/kms/backend/test", http.MethodPost

	// DEK (Data Encryption Key) operations
	case "CreateDEK":
		return "/api/v1/kms/deks", http.MethodPost
	case "GetDEK":
		return "/api/v1/kms/deks/{name}", http.MethodGet
	case "ListDEKs":
		return "/api/v1/kms/deks", http.MethodGet
	case "RotateDEK":
		return "/api/v1/kms/deks/{name}/rotate", http.MethodPost
	case "DeleteDEK":
		return "/api/v1/kms/deks/{name}", http.MethodDelete
	case "GetDEKStatus":
		return "/api/v1/kms/deks/{name}/status", http.MethodGet
	case "SetDefaultDEK":
		return "/api/v1/kms/deks/{name}/default", http.MethodPost
	case "SetDEKSafety":
		return "/api/v1/kms/deks/{name}/safety", http.MethodPut
	case "ClearDEKSafety":
		return "/api/v1/kms/deks/{name}/safety", http.MethodDelete

	// Re-encryption operations
	case "StartReencryption":
		return "/api/v1/kms/reencryption/start", http.MethodPost
	case "GetReencryptionProgress":
		return "/api/v1/kms/reencryption/progress", http.MethodGet

	default:
		return "/api/v1/kms/" + strings.ToLower(method), http.MethodPost
	}
}

// mapSecretsMethod maps Secrets service methods to REST endpoints.
func (t *Transport) mapSecretsMethod(method string) (string, string) {
	switch method {
	case "Store":
		return "/api/v1/secrets/{path}", http.MethodPost
	case "Get":
		return "/api/v1/secrets/{path}", http.MethodGet
	case "Delete":
		return "/api/v1/secrets/{path}", http.MethodDelete
	case "List":
		return "/api/v1/secrets", http.MethodGet
	case "Rotate":
		return "/api/v1/secrets/{path}/rotate", http.MethodPost
	default:
		return "/api/v1/secrets/" + strings.ToLower(method), http.MethodPost
	}
}

// mapSealMethod maps Seal service methods to REST endpoints.
func (t *Transport) mapSealMethod(method string) (string, string) {
	switch method {
	case "Status":
		return "/api/v1/seal/status", http.MethodGet
	case "Init":
		return "/api/v1/seal/init", http.MethodPost
	case "Unseal":
		return "/api/v1/seal/unseal", http.MethodPost
	case "Seal":
		return "/api/v1/seal/seal", http.MethodPost
	case "Rekey":
		return "/api/v1/seal/rekey", http.MethodPost
	default:
		return "/api/v1/seal/" + strings.ToLower(method), http.MethodPost
	}
}

// mapCryptoMethod maps Crypto service methods to REST endpoints.
func (t *Transport) mapCryptoMethod(method string) (string, string) {
	switch method {
	case "Encrypt":
		return "/api/v1/crypto/encrypt/{name}", http.MethodPost
	case "Decrypt":
		return "/api/v1/crypto/decrypt/{name}", http.MethodPost
	case "Sign":
		return "/api/v1/crypto/sign/{name}", http.MethodPost
	case "Verify":
		return "/api/v1/crypto/verify/{name}", http.MethodPost
	case "Hash":
		return "/api/v1/crypto/hash", http.MethodPost
	case "Random":
		return "/api/v1/crypto/random", http.MethodPost
	default:
		return "/api/v1/crypto/" + strings.ToLower(method), http.MethodPost
	}
}

// mapVectorMethod maps Vector service methods to REST endpoints.
func (t *Transport) mapVectorMethod(method string) (string, string) {
	switch method {
	// Namespace management operations
	case "CreateNamespace":
		return "/api/v1/vector/namespaces", http.MethodPost
	case "ListNamespaces":
		return "/api/v1/vector/namespaces", http.MethodGet
	case "GetNamespaceInfo":
		return "/api/v1/vector/namespaces/{namespace}", http.MethodGet
	case "UpdateNamespace":
		return "/api/v1/vector/namespaces/{namespace}", http.MethodPut
	case "DeleteNamespace":
		return "/api/v1/vector/namespaces/{namespace}", http.MethodDelete
	case "GetNamespaceStats":
		return "/api/v1/vector/namespaces/{namespace}/stats", http.MethodGet

	// Index management operations
	case "RebuildIndex":
		return "/api/v1/vector/namespaces/{namespace}/index/rebuild", http.MethodPost
	case "GetIndexProgress":
		return "/api/v1/vector/namespaces/{namespace}/index/progress", http.MethodGet

	// Vector CRUD operations
	case "Upsert", "UpsertVector":
		return "/api/v1/vector/namespaces/{namespace}/vectors", http.MethodPost
	case "ListVectors":
		return "/api/v1/vector/namespaces/{namespace}/vectors", http.MethodGet
	case "BatchUpsert", "BatchUpsertVectors":
		return "/api/v1/vector/namespaces/{namespace}/vectors/batch", http.MethodPost
	case "BatchDelete", "BatchDeleteVectors":
		return "/api/v1/vector/namespaces/{namespace}/vectors/batch", http.MethodDelete
	case "Get", "GetVector":
		return "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodGet
	case "Delete", "DeleteVector":
		return "/api/v1/vector/namespaces/{namespace}/vectors/{id}", http.MethodDelete
	case "UpdateMetadata":
		return "/api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata", http.MethodPut

	// Search operations
	case "Search", "SearchSimilar":
		return "/api/v1/vector/namespaces/{namespace}/search/similar", http.MethodPost
	case "SearchByID":
		return "/api/v1/vector/namespaces/{namespace}/search/by-id/{id}", http.MethodPost
	case "HybridSearch":
		return "/api/v1/vector/hybrid-search", http.MethodPost
	case "SearchByMetadata":
		return "/api/v1/vector/search-by-metadata", http.MethodPost

	// Cluster and admin operations
	case "GetGlobalStats", "ClusterStats":
		return "/api/v1/vector/cluster/stats", http.MethodGet
	case "Compact", "CompactNamespace":
		return "/api/v1/vector/admin/compact", http.MethodPost
	case "GetCompactionProgress":
		return "/api/v1/vector/admin/compact/progress", http.MethodGet
	case "ExportVectors":
		return "/api/v1/vector/admin/export/{namespace}", http.MethodPost
	case "ImportVectors":
		return "/api/v1/vector/admin/import/{namespace}", http.MethodPost

	// Health check
	case "Health":
		return "/api/v1/vector/health", http.MethodGet

	default:
		return "/api/v1/vector/" + strings.ToLower(method), http.MethodPost
	}
}

// mapTimeseriesMethod maps Timeseries service methods to REST endpoints.
func (t *Transport) mapTimeseriesMethod(method string) (string, string) {
	switch method {
	case "Write", "WritePoint":
		return "/api/v1/timeseries/write", http.MethodPost
	case "WriteBatch":
		return "/api/v1/timeseries/write/batch", http.MethodPost
	case "Query":
		return "/api/v1/timeseries/query", http.MethodPost
	case "Delete", "DeleteSeries":
		return "/api/v1/timeseries/delete", http.MethodPost
	case "Aggregate":
		return "/api/v1/timeseries/aggregate", http.MethodPost
	case "GetRetentionPolicy":
		return "/api/v1/timeseries/retention", http.MethodGet
	case "SetRetentionPolicy":
		return "/api/v1/timeseries/retention", http.MethodPut
	case "ListMetrics":
		return "/api/v1/timeseries/metrics", http.MethodGet
	default:
		return "/api/v1/timeseries/" + strings.ToLower(method), http.MethodPost
	}
}

// mapRBACMethod maps RBAC service methods to REST endpoints.
func (t *Transport) mapRBACMethod(method string) (string, string) {
	switch method {
	// User operations
	case "CreateUser":
		return "/api/v1/rbac/users", http.MethodPost
	case "GetUser":
		return "/api/v1/rbac/users/{username}", http.MethodGet
	case "UpdateUser":
		return "/api/v1/rbac/users/{username}", http.MethodPut
	case "DeleteUser":
		return "/api/v1/rbac/users/{username}", http.MethodDelete
	case "ListUsers":
		return "/api/v1/rbac/users", http.MethodGet

	// Role operations
	case "CreateRole":
		return "/api/v1/rbac/roles", http.MethodPost
	case "GetRole":
		return "/api/v1/rbac/roles/{name}", http.MethodGet
	case "UpdateRole":
		return "/api/v1/rbac/roles/{name}", http.MethodPut
	case "DeleteRole":
		return "/api/v1/rbac/roles/{name}", http.MethodDelete
	case "ListRoles":
		return "/api/v1/rbac/roles", http.MethodGet

	// Role assignment operations
	case "AssignRole":
		return "/api/v1/rbac/users/{username}/roles", http.MethodPost
	case "RevokeRole":
		return "/api/v1/rbac/users/{username}/roles/{role}", http.MethodDelete
	case "GetUserRoles":
		return "/api/v1/rbac/users/{username}/roles", http.MethodGet

	// Permission operations
	case "CheckPermission":
		return "/api/v1/rbac/permissions/check", http.MethodPost
	case "ListPermissions":
		return "/api/v1/rbac/permissions", http.MethodGet

	default:
		return "/api/v1/rbac/" + strings.ToLower(method), http.MethodPost
	}
}

// mapBackupMethod maps Backup service methods to REST endpoints.
func (t *Transport) mapBackupMethod(method string) (string, string) {
	switch method {
	case "Create":
		return "/api/v1/backups", http.MethodPost
	case "List":
		return "/api/v1/backups", http.MethodGet
	case "Get":
		return "/api/v1/backups/{id}", http.MethodGet
	case "Restore":
		return "/api/v1/backups/{id}/restore", http.MethodPost
	case "Delete":
		return "/api/v1/backups/{id}", http.MethodDelete
	case "Verify":
		return "/api/v1/backups/{id}/verify", http.MethodPost
	case "Status":
		return "/api/v1/backups/{id}/status", http.MethodGet
	case "Schedule":
		return "/api/v1/backups/schedule", http.MethodPost
	case "ListSchedules":
		return "/api/v1/backups/schedules", http.MethodGet
	case "DeleteSchedule":
		return "/api/v1/backups/schedules/{id}", http.MethodDelete
	default:
		return "/api/v1/backups/" + strings.ToLower(method), http.MethodPost
	}
}

// mapAuthMethod maps Auth service methods to REST endpoints.
func (t *Transport) mapAuthMethod(method string) (string, string) {
	switch method {
	case "Login":
		return "/api/v1/auth/login", http.MethodPost
	case "Logout":
		return "/api/v1/auth/logout", http.MethodPost
	case "RefreshToken":
		return "/api/v1/auth/refresh", http.MethodPost
	case "ValidateToken":
		return "/api/v1/auth/validate", http.MethodPost
	case "ChangePassword":
		return "/api/v1/auth/password", http.MethodPut
	case "ResetPassword":
		return "/api/v1/auth/password/reset", http.MethodPost
	case "GetSession":
		return "/api/v1/auth/session", http.MethodGet
	case "ListSessions":
		return "/api/v1/auth/sessions", http.MethodGet
	case "RevokeSession":
		return "/api/v1/auth/sessions/{id}", http.MethodDelete
	default:
		return "/api/v1/auth/" + strings.ToLower(method), http.MethodPost
	}
}

// mapCacheMethod maps Cache service methods to REST endpoints.
func (t *Transport) mapCacheMethod(method string) (string, string) {
	switch method {
	case "GetStats":
		return "/api/v1/cache/stats", http.MethodGet
	case "Clear":
		return "/api/v1/cache/clear", http.MethodPost
	case "GetConfig":
		return "/api/v1/cache/config", http.MethodGet
	case "UpdateConfig":
		return "/api/v1/cache/config", http.MethodPut
	case "Invalidate":
		return "/api/v1/cache/invalidate", http.MethodPost
	case "Warm":
		return "/api/v1/cache/warm", http.MethodPost
	default:
		return "/api/v1/cache/" + strings.ToLower(method), http.MethodPost
	}
}

// mapOperationsMethod maps Operations service methods to REST endpoints.
func (t *Transport) mapOperationsMethod(method string) (string, string) {
	switch method {
	// Migration operations
	case "MigrateData":
		return "/api/v1/operations/migrate", http.MethodPost
	case "GetMigrationProgress":
		return "/api/v1/operations/migrate/progress", http.MethodGet
	case "CancelMigration":
		return "/api/v1/operations/migrate/cancel", http.MethodPost

	// Node operations
	case "ReplaceNode":
		return "/api/v1/operations/node/replace", http.MethodPost
	case "UpgradeNode":
		return "/api/v1/operations/node/upgrade", http.MethodPost
	case "GetUpgradeStatus":
		return "/api/v1/operations/node/upgrade/status", http.MethodGet

	// Maintenance operations
	case "StartMaintenance":
		return "/api/v1/operations/maintenance/start", http.MethodPost
	case "EndMaintenance":
		return "/api/v1/operations/maintenance/end", http.MethodPost
	case "GetMaintenanceStatus":
		return "/api/v1/operations/maintenance/status", http.MethodGet

	// Compaction operations
	case "TriggerCompaction":
		return "/api/v1/operations/compaction", http.MethodPost
	case "GetCompactionStatus":
		return "/api/v1/operations/compaction/status", http.MethodGet

	default:
		return "/api/v1/operations/" + strings.ToLower(method), http.MethodPost
	}
}

// mapEmbeddingMethod maps Embedding service methods to REST endpoints.
func (t *Transport) mapEmbeddingMethod(method string) (string, string) {
	switch method {
	// Embedding generation (short names for backward compatibility)
	case "Generate", "GenerateEmbedding":
		return "/api/v1/embedding/generate", http.MethodPost
	case "BatchGenerate", "BatchGenerateEmbeddings":
		return "/api/v1/embedding/batch", http.MethodPost

	// Legacy CRUD operations (backward compatibility)
	case "Search":
		return "/api/v1/embedding/search", http.MethodPost
	case "Upsert":
		return "/api/v1/embedding/upsert", http.MethodPost
	case "Delete":
		return "/api/v1/embedding/delete", http.MethodPost
	case "Get":
		return "/api/v1/embedding/get", http.MethodPost

	// Provider management
	case "ListProviders":
		return "/api/v1/embedding/providers", http.MethodGet
	case "GetProviderInfo":
		// Handled directly by embeddingHandler.handleGetProviderInfo with path parameter
		return "/api/v1/embedding/providers/{provider}", http.MethodGet
	case "SetDefaultProvider":
		return "/api/v1/embedding/providers/default", http.MethodPut

	// Legacy model management (backward compatibility)
	case "ListModels":
		return "/api/v1/embedding/providers", http.MethodGet
	case "GetModel":
		// Handled directly by embeddingHandler.handleGetProviderInfo with path parameter
		return "/api/v1/embedding/providers/{provider}", http.MethodGet
	case "SetDefaultModel":
		return "/api/v1/embedding/providers/default", http.MethodPut

	// Cache management
	case "GetCacheStats":
		return "/api/v1/embedding/cache/stats", http.MethodGet
	case "ClearCache":
		return "/api/v1/embedding/cache", http.MethodDelete

	default:
		return "/api/v1/embedding/" + strings.ToLower(method), http.MethodPost
	}
}

// mapObjectStorageAdminMethod maps ObjectStorageAdmin service methods to REST endpoints.
func (t *Transport) mapObjectStorageAdminMethod(method string) (string, string) {
	switch method {
	// Backend management operations
	case "CreateBackend":
		return "/api/v1/storage/admin/backends", http.MethodPost
	case "UpdateBackend":
		return "/api/v1/storage/admin/backends/{id}", http.MethodPut
	case "DeleteBackend":
		return "/api/v1/storage/admin/backends/{id}", http.MethodDelete
	case "SetBackendStatus":
		return "/api/v1/storage/admin/backends/{id}/status", http.MethodPost

	// Storage stats and maintenance operations
	case "GetStorageStats":
		return "/api/v1/storage/admin/stats", http.MethodGet
	case "RunGarbageCollection":
		return "/api/v1/storage/admin/gc", http.MethodPost
	case "GetGarbageCollectionProgress":
		return "/api/v1/storage/admin/gc/progress", http.MethodGet

	// Object repair and compaction operations
	case "RepairObject":
		return "/api/v1/storage/admin/repair", http.MethodPost
	case "CompactBackend":
		return "/api/v1/storage/admin/backends/{id}/compact", http.MethodPost

	default:
		return "/api/v1/storage/admin/" + strings.ToLower(method), http.MethodPost
	}
}

// mapSchedulerMethod maps Scheduler service methods to REST endpoints.
func (t *Transport) mapSchedulerMethod(method string) (string, string) {
	switch method {
	// Job CRUD operations
	case "CreateJob":
		return "/api/v1/scheduler/jobs", http.MethodPost
	case "GetJob":
		return "/api/v1/scheduler/jobs/{id}", http.MethodGet
	case "UpdateJob":
		return "/api/v1/scheduler/jobs/{id}", http.MethodPut
	case "DeleteJob":
		return "/api/v1/scheduler/jobs/{id}", http.MethodDelete
	case "ListJobs":
		return "/api/v1/scheduler/jobs", http.MethodGet

	// Job control operations
	case "TriggerJob":
		return "/api/v1/scheduler/jobs/{id}/trigger", http.MethodPost
	case "PauseJob":
		return "/api/v1/scheduler/jobs/{id}/pause", http.MethodPost
	case "ResumeJob":
		return "/api/v1/scheduler/jobs/{id}/resume", http.MethodPost
	case "CancelJob":
		return "/api/v1/scheduler/jobs/{id}/cancel", http.MethodPost

	// Job history and status operations
	case "GetJobHistory":
		return "/api/v1/scheduler/jobs/{id}/history", http.MethodGet
	case "GetStatus":
		return "/api/v1/scheduler/status", http.MethodGet

	default:
		return "/api/v1/scheduler/" + strings.ToLower(method), http.MethodPost
	}
}

// mapRAGMethod maps RAG service methods to REST endpoints.
func (t *Transport) mapRAGMethod(method string) (string, string) {
	switch method {
	case "Query":
		return "/api/v1/rag/query", http.MethodPost
	case "BuildContext":
		return "/api/v1/rag/context", http.MethodPost
	default:
		return "/api/v1/rag/" + strings.ToLower(method), http.MethodPost
	}
}

// parseErrorResponse parses an error response from the server.
func (t *Transport) parseErrorResponse(statusCode int, body []byte) error {
	// Try to parse as JSON error response
	var errResp struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		code := t.statusCodeToErrorCode(statusCode)
		return errors.New(code, "rest.parseErrorResponse", errResp.Error)
	}

	// Fallback to status code mapping
	code := t.statusCodeToErrorCode(statusCode)
	return errors.New(code, "rest.parseErrorResponse",
		"HTTP "+strconv.Itoa(statusCode)+": "+string(body))
}

// statusCodeToErrorCode maps HTTP status codes to error codes.
func (t *Transport) statusCodeToErrorCode(statusCode int) errors.ErrorCode {
	switch statusCode {
	case http.StatusBadRequest:
		return errors.CodeInvalidInput
	case http.StatusUnauthorized:
		return errors.CodeUnauthorized
	case http.StatusForbidden:
		return errors.CodeForbidden
	case http.StatusNotFound:
		return errors.CodeNotFound
	case http.StatusConflict:
		return errors.CodeConflict
	case http.StatusPreconditionFailed:
		return errors.CodePreconditionFailed
	case http.StatusTooManyRequests:
		return errors.CodeRateLimited
	case http.StatusInternalServerError:
		return errors.CodeInternal
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return errors.CodeUnavailable
	case http.StatusGatewayTimeout:
		return errors.CodeTimeout
	default:
		if statusCode >= 400 && statusCode < 500 {
			return errors.CodeInvalidInput
		}
		if statusCode >= 500 {
			return errors.CodeInternal
		}
		return errors.CodeUnknown
	}
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
// REST does not support bidirectional streaming, so this returns ErrStreamNotSupported.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, ErrStreamNotSupported
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

// Factory implements transport.Factory for REST.
type Factory struct{}

// Create creates a new REST transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg)
}
