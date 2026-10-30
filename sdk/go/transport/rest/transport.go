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
	streamClient        *http.Client // lazily built by streamHTTPClient; no per-request timeout
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
	projectHandler      *projectRESTHandler
	sdlcHandler         *sdlcRESTHandler
	promptHandler       *promptRESTHandler
	batchHandler        *batchRESTHandler
	accountHandler      *accountRESTHandler
	sshHandler          *sshRESTHandler
	certificateHandler  *certificateRESTHandler
	kmsHandler          *kmsRESTHandler
	dekHandler          *dekRESTHandler
	secretsHandler      *secretsRESTHandler
	dynSecretsHandler   *dynamicSecretsRESTHandler
	cryptoHandler       *cryptoRESTHandler
	messagingHandler    *messagingRESTHandler
	timeseriesHandler   *timeseriesRESTHandler
	securityHandler     *securityRESTHandler
	schedulerHandler    *schedulerRESTHandler
	rbacHandler         *rbacRESTHandler

	// defaultTenant scopes tenant-bound Security() verbs (e.g. WebAuthn)
	// whose SDK request structs do not themselves carry a tenant. It
	// mirrors the IAM transport's existing "tenant_id travels with the
	// request" mechanism for verbs whose public SDK signature predates
	// multi-tenant routing. Configured via WithDefaultTenant.
	defaultTenant string

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

	// Initialize Project handler
	t.projectHandler = newProjectRESTHandler(t)

	// Initialize SDLC project registration handler
	t.sdlcHandler = newSDLCRESTHandler(t)
	t.promptHandler = newPromptRESTHandler(t)
	t.batchHandler = newBatchRESTHandler(t)

	// Initialize pooled provider account handler
	t.accountHandler = newAccountRESTHandler(t)
	t.sshHandler = newSSHRESTHandler(t)

	// Initialize Certificate handler
	t.certificateHandler = newCertificateRESTHandler(t)
	t.kmsHandler = newKMSRESTHandler(t)
	t.dekHandler = newDEKRESTHandler(t)
	t.secretsHandler = newSecretsRESTHandler(t)
	t.dynSecretsHandler = newDynamicSecretsRESTHandler(t)
	t.cryptoHandler = newCryptoRESTHandler(t)
	t.messagingHandler = newMessagingRESTHandler(t)
	t.timeseriesHandler = newTimeseriesRESTHandler(t)
	t.securityHandler = newSecurityRESTHandler(t)
	t.schedulerHandler = newSchedulerRESTHandler(t)
	t.rbacHandler = newRBACRESTHandler(t)

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

	// Configure TLS. This transport speaks HTTP/1.1 only, so it offers only
	// http/1.1 through ALPN.
	if t.cfg.TLSConfig != nil {
		httpTransport.TLSClientConfig = transport.HTTP1TLSConfig(t.cfg.TLSConfig)
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
	t.baseURL = transport.HTTPBaseURL(t.cfg.Address, t.cfg.TLSConfig != nil)
	t.basePath = transport.HTTPPath(t.cfg.Address)
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

	// Route Project requests through the Project handler.
	if isProjectService(service) {
		return t.projectHandler.handleRequest(ctx, method, req)
	}

	// Route the tenant AI agent catalog through its handler.
	if isAgentsService(service) {
		return t.handleAgentsRequest(ctx, method, req)
	}

	// Route SDLC project registration through its handler.
	if isSDLCService(service) {
		return t.sdlcHandler.handleRequest(ctx, method, req)
	}
	if isPromptsService(service) {
		return t.promptHandler.handleRequest(ctx, method, req)
	}
	if isBatchService(service) {
		return t.batchHandler.handleRequest(ctx, method, req)
	}

	// Route SSH requests through the SSH handler. The routes it targets are
	// the same /api/v1/ssh mount the go-xkms REST SSH client alias uses, so
	// the SDK and that client observe one server surface.
	if isSSHService(service) {
		return t.sshHandler.handleRequest(ctx, method, req)
	}

	// Route pooled provider account requests through the Account handler.
	if isAccountService(service) {
		return t.accountHandler.handleRequest(ctx, method, req)
	}

	// Route Certificate requests through the Certificate handler for
	// proper typed response decoding.
	if isCertificateService(service) {
		return t.certificateHandler.handleRequest(ctx, service, method, req)
	}

	// Route KMS requests through the KMS handler so path parameters such as
	// {name} are substituted and responses are decoded into service types.
	if isKMSService(service) {
		return t.kmsHandler.handleRequest(ctx, method, req)
	}

	// Route DEK requests through the DEK handler.
	if isDEKService(service) {
		return t.dekHandler.handleRequest(ctx, method, req)
	}

	// Route Secrets requests through the Secrets handler.
	if isSecretsService(service) {
		return t.secretsHandler.handleRequest(ctx, method, req)
	}

	// Route DynamicSecrets requests through the DynamicSecrets handler.
	// The engine is mounted at /api/v1/secrets/dynamic, which the generic
	// service/method endpoint mapping cannot reach.
	if isDynamicSecretsService(service) {
		return t.dynSecretsHandler.handleRequest(ctx, method, req)
	}

	// Route Crypto requests through the Crypto handler.
	if isCryptoService(service) {
		return t.cryptoHandler.handleRequest(ctx, method, req)
	}

	// Route Messaging requests through the Messaging handler.
	if isMessagingService(service) {
		return t.messagingHandler.handleRequest(ctx, method, req)
	}

	// Route TimeSeries requests through the TimeSeries handler.
	if isTimeseriesService(service) {
		return t.timeseriesHandler.handleRequest(ctx, method, req)
	}

	// Route the service registry through its handler: its routes carry path
	// parameters the generic service/method mapping cannot express.
	if isRegistryService(service) {
		return t.handleRegistryRequest(ctx, method, req)
	}

	// Route Async().Scheduler() — jobs and workload placement — through the
	// scheduler adapter, which reaches the /api/v1/scheduler routes in
	// service-layer shape for scheduler.Dispatch to map.
	if isSchedulerService(service) {
		return t.schedulerHandler.handleRequest(ctx, method, req)
	}

	// RBAC verbs go to the tenant-scoped IAM role routes.
	if isRBACService(service) {
		return t.rbacHandler.handleRequest(ctx, method, req)
	}

	// Route Security services (WebAuthn/FIDO2/PIV/ACME/Trust) through the
	// security handler so tenant-scoped WebAuthn verbs reach the real
	// IAM-mounted server routes and so verbs without a server surface
	// return a precise typed error rather than a broken generic dispatch.
	if isSecurityService(service) {
		return t.securityHandler.handleRequest(ctx, service, method, req)
	}

	// ObjectStorageAdmin verbs go to the /api/v1/admin object-backend,
	// storage and object-repair routes with typed replies.
	if isObjectStorageAdminService(service) {
		return t.handleObjectStorageAdminRequest(ctx, method, req)
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
	return t.doJSONRequestWithHeaders(ctx, httpMethod, endpoint, reqBody, result, nil)
}

// doJSONRequestWithHeaders is doJSONRequest with per-request headers that
// overlay the transport's configured headers for this request only. Use it
// instead of SetHeader when a single call needs a different value (e.g. a
// bearer other than the caller's): SetHeader mutates state every concurrent
// request shares.
func (t *Transport) doJSONRequestWithHeaders(ctx context.Context, httpMethod, endpoint string, reqBody interface{}, result interface{}, reqHeaders map[string]string) error {
	var body []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, "rest.doJSONRequest",
				"failed to encode request body")
		}
		body = data
	}

	resp, err := t.doRawRequest(ctx, httpMethod, endpoint, body, reqHeaders)
	if err != nil {
		return err
	}

	if result != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, result); err != nil {
			return errors.Wrap(err, errors.CodeInternal, "rest.doJSONRequest",
				"failed to decode response")
		}
	}

	return nil
}

// rawResponse carries the raw body bytes and selected response headers
// from a REST call. It is used by handlers that must observe both the
// response body and headers (e.g. the WebAuthn ceremony, where the
// session identifier is returned in the X-Session-Id response header).
type rawResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

// doRawRequest executes an HTTP request with a caller-supplied raw byte
// body and request headers, returning the raw response body and headers
// without JSON-decoding. The reqHeaders override the transport defaults
// for the duration of the call. This is required for transports whose
// server contract exchanges opaque WebAuthn ceremony JSON plus correlation
// headers rather than a flat JSON envelope.
func (t *Transport) doRawRequest(ctx context.Context, httpMethod, endpoint string, reqBody []byte, reqHeaders map[string]string) (*rawResponse, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("rest.doRawRequest", "transport is closed")
	}

	url := t.baseURL + t.basePath + endpoint

	var body io.Reader
	if len(reqBody) > 0 {
		body = bytes.NewReader(reqBody)
	}

	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, url, body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.doRawRequest",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	for k, v := range reqHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, t.convertHTTPError(err, "rest.doRawRequest")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.doRawRequest",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, t.parseErrorResponse(resp.StatusCode, respBody)
	}

	t.lastHealth.Store(time.Now().UnixNano())

	return &rawResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Header:     resp.Header.Clone(),
	}, nil
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
	case "Crypto":
		return t.mapCryptoMethod(method)
	case "Vector":
		return t.mapVectorMethod(method)
	case "Timeseries", "TimeSeries":
		return t.mapTimeseriesMethod(method)
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
	case "Scheduler":
		return t.mapSchedulerMethod(method)
	case "RAG":
		return t.mapRAGMethod(method)
	case "Certificate":
		return t.mapCertificateMethod(method)
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
		return authLogoutPath, http.MethodPost
	case "RefreshToken":
		return authRefreshPath, http.MethodPost
	case "ValidateToken":
		return authValidatePath, http.MethodPost
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

	// Workload placement: nodes and placements
	case "RegisterNode":
		return "/api/v1/scheduler/nodes", http.MethodPost
	case "ListNodes":
		return "/api/v1/scheduler/nodes", http.MethodGet
	case "GetNode":
		return "/api/v1/scheduler/nodes/{id}", http.MethodGet
	case "DeregisterNode":
		return "/api/v1/scheduler/nodes/{id}", http.MethodDelete
	case "RenewNode":
		return "/api/v1/scheduler/nodes/{id}/renew", http.MethodPost
	case "CordonNode":
		return "/api/v1/scheduler/nodes/{id}/cordon", http.MethodPost
	case "UncordonNode":
		return "/api/v1/scheduler/nodes/{id}/uncordon", http.MethodPost
	case "DrainNode":
		return "/api/v1/scheduler/nodes/{id}/drain", http.MethodPost
	case "Place":
		return "/api/v1/scheduler/placements", http.MethodPost
	case "ListPlacements":
		return "/api/v1/scheduler/placements", http.MethodGet
	case "GetPlacement":
		return "/api/v1/scheduler/placements/{id}", http.MethodGet
	case "Release":
		return "/api/v1/scheduler/placements/{id}", http.MethodDelete

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

// convertHTTPError converts HTTP client errors to QBErrors.
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
//
// REST has no bidirectional streaming, so this rejects every RPC that needs
// it with ErrStreamNotSupported — except Orchestrator.SubscribeSessionEvents,
// which is server-to-client only and rides the SSE endpoint documented at
// GET /api/v1/orchestrator/sessions/{id}/events (see orchestrator_stream.go).
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if isOrchestratorService(service) && method == "SubscribeSessionEvents" {
		return t.orchestratorHandler.subscribeSessionEvents(ctx, req)
	}
	if isRegistryService(service) {
		return t.openRegistryWatch(ctx, method, req)
	}
	return nil, ErrStreamNotSupported
}

// streamHTTPClient returns an HTTP client for long-lived server-streaming
// reads (SSE). It shares the transport's connection pool and TLS
// configuration but has no overall request timeout: a follow stream can
// legitimately outlive t.client's configured timeout, so cancellation for
// streaming calls comes solely from the caller's context, matching the gRPC
// and QUIC stream transports.
func (t *Transport) streamHTTPClient() *http.Client {
	t.mu.RLock()
	client := t.streamClient
	t.mu.RUnlock()
	if client != nil {
		return client
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.streamClient == nil {
		t.streamClient = &http.Client{Transport: t.client.Transport}
	}
	return t.streamClient
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

// mapCertificateMethod maps Certificate service methods to REST
// endpoints exposed by the quicbaasd server. The CA-administration
// endpoints (CAInfo / CABundle / ListCertificates) live under
// /api/v1/certificate/ca/* so the canonical `quicbaas ca` CLI surface
// has a stable, dedicated subtree.
func (t *Transport) mapCertificateMethod(method string) (string, string) {
	switch method {
	case "IssueCertificate":
		return "/api/v1/certificate/issue", http.MethodPost
	case "SignCSR":
		return "/api/v1/certificate/sign-csr", http.MethodPost
	case "RevokeCertificate":
		return "/api/v1/certificate/revoke", http.MethodPost
	case "VerifyCertificate":
		return "/api/v1/certificate/verify", http.MethodPost
	case "GetCACertificate":
		return "/api/v1/certificate/ca", http.MethodGet
	case "GetTLSConfig":
		return "/api/v1/certificate/tls-config", http.MethodPost
	case "CAInfo":
		return "/api/v1/certificate/ca/info", http.MethodGet
	case "GetCABundle":
		return "/api/v1/certificate/ca/bundle", http.MethodGet
	case "ListCertificates":
		return "/api/v1/certificate/ca/list", http.MethodGet
	case "CrossSign":
		return "/api/v1/certificate/cross-sign", http.MethodPost
	case "Chain":
		return "/api/v1/certificate/chain", http.MethodPost
	case "IssueDeviceIdentity":
		return "/api/v1/certificate/device-identity", http.MethodPost
	default:
		return "/api/v1/certificate/" + strings.ToLower(method), http.MethodPost
	}
}
