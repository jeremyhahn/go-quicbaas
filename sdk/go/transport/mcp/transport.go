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
	"encoding/json"
	stderrors "errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	secure "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// MCPVersion is the supported MCP protocol version.
const MCPVersion = "2024-11-05"

// MCPProtocolVersionHeader is the HTTP header for MCP protocol version.
const MCPProtocolVersionHeader = "MCP-Protocol-Version"

// Transport implements the transport.Transport interface for MCP (Model
// Context Protocol). It speaks the real Model Context Protocol over
// Streamable HTTP (github.com/mark3labs/mcp-go): every SDK service/method
// call is invoked as an MCP tool call against the server, using the SAME
// tool-name derivation and argument/response transforms as the retired
// hand-rolled JSON-RPC client. Only the wire engine changed.
type Transport struct {
	cfg      *transport.Config
	client   *http.Client
	baseURL  string
	endpoint string
	headers  map[string]string
	closed   atomic.Bool

	lastHealth atomic.Int64
	requestID  atomic.Uint64

	// mcpClient is the real MCP client session used by Request/doRequest: it
	// owns the negotiated protocol version and the tools/call dispatch.
	// Constructed lazily by ensureInitialized/connect on first use.
	mcpClient       *mcpclient.Client
	initialized     atomic.Bool
	serverInfo      *serverInfo
	initMu          sync.Mutex
	protocolVersion string

	// rawTransport backs CallRPC (rpc.go): a separate, lower-level MCP
	// transport.Interface that forwards a caller's OWN JSON-RPC frames
	// (including their own "initialize" handshake) verbatim, independent of
	// mcpClient's session state. Constructed lazily on first CallRPC.
	rawTransport   mcptransport.Interface
	rawTransportMu sync.Mutex

	// Authentication state
	username      string
	password      *secure.Secret
	optionErr     error
	accessToken   string
	authenticated atomic.Bool
	authMu        sync.Mutex

	mu sync.RWMutex
}

// serverInfo holds information about the connected MCP server.
type serverInfo struct {
	Name    string
	Version string
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
	if t.optionErr != nil {
		return nil, t.optionErr
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

// MCPEndpointPath is the path the Streamable HTTP MCP surface is mounted at,
// on the quicbaas REST listener and on go-qrdb's unified server alike.
const MCPEndpointPath = "/mcp"

// buildBaseURL constructs the base URL and MCP endpoint from config.
func (t *Transport) buildBaseURL() {
	tlsConfigured := t.cfg.TLSConfig != nil
	t.baseURL = transport.HTTPBaseURL(t.cfg.Address, tlsConfigured)
	t.endpoint = transport.HTTPEndpoint(t.cfg.Address, tlsConfigured, MCPEndpointPath)
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	// Refused before any I/O: the secret must never be fetched over MCP.
	if isSecretsGetData(service, method) {
		return nil, newSecretsGetDataUnsupportedError()
	}
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
// applyTenantFromContext stamps the caller's tenant onto tool arguments that
// do not already carry one.
//
// THE TENANT TRAVELS IN THE CONTEXT AND THE TOOLS TAKE IT AS AN ARGUMENT.
// Every other transport reads transport.ContextWithTenant -- gRPC puts it on
// the proto, REST on the query -- but an MCP tool call carries only its
// arguments, and the server's session tools reject a call with no tenant_id
// outright. A request struct that has no tenant field therefore has to be
// given one here, or the call cannot succeed at all.
//
// An argument the caller set explicitly always wins: this fills a gap, it does
// not override an intent.
func applyTenantFromContext(ctx context.Context, arguments map[string]interface{}) map[string]interface{} {
	tenantID := transport.TenantFromContext(ctx)
	if tenantID == "" {
		return arguments
	}
	if arguments == nil {
		return map[string]interface{}{"tenant_id": tenantID}
	}
	if _, hasTenant := arguments["tenant_id"]; !hasTenant {
		arguments["tenant_id"] = tenantID
	}
	return arguments
}

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
	// Public tools: quicbaas_login, qrdb_health, quicbaas_liveness, quicbaas_readiness
	if !t.isPublicTool(toolName) {
		if err := t.ensureAuthenticated(ctx); err != nil {
			return nil, err
		}
	}

	// KV Put/Get/Delete are served by quicbaas's quicbaas_kv_* tools, which
	// call the KV service; a request that is not the typed SDK struct keeps
	// the generic path.
	if verb, ok := kvVerbs[method]; ok && isMCPKVService(service) {
		if args, typed := verb.args(reqBody); typed {
			return t.requestKV(ctx, verb, args)
		}
	}

	// The Agents catalog is a closed set of tools with typed answers.
	if isMCPAgentsService(service) {
		return t.requestAgents(ctx, method, reqBody)
	}

	// The service registry: string-encoded lease IDs and typed results.
	if isMCPRegistryService(service) {
		arguments, err := registryArguments(method, reqBody)
		if err != nil {
			return nil, err
		}
		result, err := t.callTool(ctx, toolName, arguments)
		if err != nil {
			return nil, err
		}
		return registryResult(method, result)
	}

	// The parameter store is a closed set of string-typed tools with typed
	// answers; a method with no tool fails fast.
	if isMCPParameterStoreService(service) {
		verb, ok := parameterStoreVerbs[method]
		if !ok {
			return nil, newParameterStoreUnsupportedError(method)
		}
		return t.requestToolVerb(ctx, "mcp.parameterstore."+method, verb, reqBody)
	}

	// The Prompts surface is the closed prompt_* tool set with typed answers.
	if isMCPPromptsService(service) {
		return t.requestPrompt(ctx, method, reqBody)
	}

	// Ollama's verbs decode into the client types; EmbedWithModel keeps the
	// generic path below.
	if verb, ok := ollamaVerbs[method]; ok && isMCPOllamaService(service) {
		return t.requestToolVerb(ctx, "mcp.ollama."+method, verb, reqBody)
	}

	// These KMS verbs decode into the client types; the rest keep the
	// generic path below.
	if verb, ok := kmsVerbs[method]; ok && isMCPKMSService(service) {
		return t.requestToolVerb(ctx, "mcp.kms."+method, verb, reqBody)
	}
	if method == "GetPublicKey" && isMCPKMSService(service) {
		return t.requestKMSPublicKey(ctx, reqBody)
	}

	// Check if this is a service that needs special handling
	isVector := isMCPVectorService(service)
	isScheduler := isMCPSchedulerService(service)
	isAuth := isMCPAuthService(service)
	isTimeseries := isMCPTimeseriesService(service)
	isCluster := isMCPClusterService(service)
	isEmbedding := isMCPEmbeddingService(service)
	isIAM := isMCPIAMService(service)
	isOrchestrator := isMCPOrchestratorService(service)
	isAccount := isMCPAccountService(service)
	isSSH := isMCPSSHService(service)
	isProject := isMCPProjectService(service)
	isSDLC := isMCPSDLCService(service)
	isCertificate := isMCPCertificateService(service)
	isMessaging := isMCPMessagingService(service)
	isObjectStorage := isMCPObjectStorageService(service)
	isObjectStorageAdmin := isMCPObjectStorageAdminService(service)
	isAttestation := isMCPAttestationService(service)
	isDynamicSecrets := isMCPDynamicSecretsService(service)
	isPIV := isMCPPIVService(service)
	isFIDO2 := isMCPFIDO2Service(service)

	// The PIV surface is a closed set of xkms.piv.* tools; a method with no
	// tool fails fast with a typed Unimplemented naming the transport.
	if isPIV {
		if _, ok := pivToolName(method); !ok {
			return nil, errors.NewUnimplemented("mcp.piv."+method,
				"method not exposed by the PIV MCP tool set")
		}
	}

	// Likewise for the FIDO2 ceremony surface (xkms.fido2.* tools).
	if isFIDO2 {
		if _, ok := fido2ToolName(method); !ok {
			return nil, errors.NewUnimplemented("mcp.fido2."+method,
				"method not exposed by the FIDO2 MCP tool set")
		}
	}

	// The Project surface is a closed set of tools. A method with no tool
	// fails fast with a typed Unimplemented naming the transport
	// rather than asking the server for a tool it does not register.
	if isProject {
		if _, ok := projectToolName(method); !ok {
			return nil, newProjectUnsupportedError(method)
		}
	}
	if isSDLC {
		if _, ok := sdlcToolName(method); !ok {
			return nil, newSDLCUnsupportedError(method)
		}
	}

	// The DynamicSecrets surface is a closed set of eight tools; fail fast on
	// a method the server does not register rather than calling a tool that
	// does not exist.
	if isDynamicSecrets {
		if _, ok := dynamicSecretsToolName(method); !ok {
			return nil, newDynamicSecretsUnsupportedError(method)
		}
	}

	// The SSH surface is likewise a closed set of tools; fail fast on a
	// method the server does not register.
	if isSSH {
		if _, ok := sshToolName(method); !ok {
			return nil, newSSHUnsupportedError(method)
		}
	}

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
		arguments = applyTenantFromContext(ctx, arguments)
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
	} else if isSSH {
		// SSH ships two binary-bearing verbs (import PEM, sign digest) whose
		// tool arguments are base64 under _b64 names; the rest pass through
		// their JSON tags unchanged.
		sh := newSSHMCPHandler()
		arguments, err = sh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isCertificate {
		ch := newCertificateHandler(t)
		arguments, err = ch.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isMessaging {
		mh := newMessagingHandler(t)
		arguments, err = mh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isObjectStorageAdmin {
		arguments, err = objectStorageAdminArguments(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isObjectStorage {
		oh := newObjectStorageHandler(t)
		arguments, err = oh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isDynamicSecrets {
		// The dynamic secrets tool schema is string-typed: durations travel as
		// Go duration strings and the generator config as a JSON string, so
		// the generic argument conversion would send the wrong shapes.
		dh := newDynamicSecretsHandler()
		arguments, err = dh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isAttestation {
		// Attestation carries binary evidence and a certificate chain; the
		// MCP tool schema is string-typed, so the handler base64-encodes
		// the former and concatenates the latter into one PEM bundle.
		ah := newAttestationHandler()
		arguments, err = ah.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isPIV {
		// PIV requests are the go-xkms transport structs; their JSON tags
		// are the tool argument names, so the transform is a round-trip.
		ph := newPIVMCPHandler()
		arguments, err = ph.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
	} else if isFIDO2 {
		// FIDO2 requests are the go-xkms transport structs. The tenant
		// scope is injected from the request context, mirroring the
		// scheduler enrichment: xkms wire structs carry no tenant field
		// because xkms runs a single relying party.
		fh := newFIDO2MCPHandler()
		arguments, err = fh.transformRequest(method, reqBody)
		if err != nil {
			return nil, err
		}
		arguments = applyTenantFromContext(ctx, arguments)
	} else if isOrchestrator {
		// The orchestrator's SDK request structs carry every field the tools
		// need EXCEPT the tenant: ListSessionsRequest is {backend, status},
		// because on the wire transports the tenant rides the context rather
		// than the body. The server's session tools require tenant_id and
		// refuse without it, so a plain generic conversion produced a call
		// that could never succeed -- a filtered listing came back as a
		// required-field error instead of a session list.
		arguments = applyTenantFromContext(ctx, t.convertToArguments(reqBody))
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

	// Transform response for the Attestation service so callers receive the
	// leaf typed response every other transport returns instead of a map.
	if isAttestation {
		ah := newAttestationHandler()
		return ah.transformResponse(method, result)
	}

	// Transform PIV and FIDO2 results into the go-xkms transport response
	// types every other transport returns.
	if isPIV {
		ph := newPIVMCPHandler()
		return ph.transformResponse(method, result)
	}
	if isFIDO2 {
		fh := newFIDO2MCPHandler()
		return fh.transformResponse(method, result)
	}

	// Transform response for the pooled provider Account service. The
	// generic convertToArguments path already produced the tool arguments:
	// the SDK account request types carry the same snake_case JSON tags the
	// tools expect, so no request transform is needed.
	if isSSH {
		sh := newSSHMCPHandler()
		return sh.transformResponse(method, result)
	}

	if isAccount {
		ah := newAccountMCPHandler()
		return ah.transformResponse(method, result)
	}

	// Transform the DynamicSecrets result so callers receive the leaf typed
	// response every other transport returns instead of a raw map.
	if isDynamicSecrets {
		dh := newDynamicSecretsHandler()
		return dh.transformResponse(method, result)
	}

	// Transform response for the Project service. The project tools return the
	// pkg/service response envelopes, which do not match the flat SDK types.
	if isProject {
		ph := newProjectMCPHandler()
		return ph.transformResponse(method, result)
	}
	if isSDLC {
		return transformSDLCResponse(method, result)
	}

	// Transform response for Scheduler service
	if isScheduler {
		sh := newSchedulerHandler()
		return sh.transformResponse(method, reqBody, result)
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

	// Transform response for Orchestrator Inference so the client receives
	// the strongly-typed *orchestrator.InferenceResponse.
	if isOrchestrator {
		oh := newOrchestratorMCPHandler()
		return oh.transformResponse(method, result)
	}

	// Transform response for Certificate service into the concrete SDK
	// response types the certificate client expects.
	if isCertificate {
		ch := newCertificateHandler(t)
		return ch.transformResponse(method, reqBody, result)
	}

	// Transform response for Messaging service.
	if isMessaging {
		mh := newMessagingHandler(t)
		return mh.transformResponse(method, result)
	}

	// Transform response for ObjectStorage service.
	if isObjectStorage {
		oh := newObjectStorageHandler(t)
		return oh.transformResponse(method, result)
	}
	if isObjectStorageAdmin {
		return objectStorageAdminResponse(method, result)
	}

	// Decode the DEK progress verbs into the service responses the DEK
	// client accepts.
	if isMCPDEKService(service) {
		return transformDEKResponse(method, result)
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

	if err := t.connect(ctx); err != nil {
		return err
	}

	t.initialized.Store(true)
	return nil
}

// ensureAuthenticated ensures the transport is authenticated.
// This is called automatically when credentials are provided.
func (t *Transport) ensureAuthenticated(ctx context.Context) error {
	if t.authenticated.Load() {
		return nil
	}

	t.authMu.Lock()
	defer t.authMu.Unlock()

	if t.closed.Load() {
		return errors.NewConnection("mcp.ensureAuthenticated", "transport is closed")
	}

	if t.optionErr != nil {
		return t.optionErr
	}

	if t.authenticated.Load() {
		return nil
	}

	// If no credentials, skip authentication (allow public tools).
	if t.username == "" && t.password == nil {
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
	password := ""
	if t.password != nil {
		opened, err := t.password.Open()
		if err != nil {
			return errors.Wrap(err, errors.CodeUnauthorized, "mcp.authenticate",
				"failed to open protected password")
		}
		defer opened.Destroy()
		//secure:unwiped injected-interface MCP login arguments are JSON strings at the wire boundary.
		password = string(opened.Bytes()) //secure:unborrowed injected-interface the protected password is opened only for the login call.
	}
	result, err := t.callTool(ctx, "quicbaas_login", map[string]interface{}{
		"username": t.username,
		"password": password,
	})
	password = ""
	if err != nil {
		return errors.Wrap(err, errors.CodeUnauthorized, "mcp.authenticate",
			"failed to authenticate")
	}

	// Extract token from response
	token, err := t.extractAccessToken(result)
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

// extractAccessToken extracts the access token from the (already JSON-decoded)
// quicbaas_login tool result.
func (t *Transport) extractAccessToken(result interface{}) (string, error) {
	loginResp, ok := result.(map[string]interface{})
	if !ok {
		return "", errors.New(errors.CodeInternal, "mcp.extractAccessToken",
			"unexpected login response shape")
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

// isPublicTool returns true if the tool doesn't require authentication.
func (t *Transport) isPublicTool(toolName string) bool {
	publicTools := map[string]bool{
		"quicbaas_login":        true,
		"quicbaas_login_begin":  true,
		"quicbaas_login_finish": true,
		"qrdb_health":           true,
		"quicbaas_liveness":     true,
		"quicbaas_readiness":    true,
	}
	return publicTools[toolName]
}

// streamableHTTPOptions builds the mcp-go Streamable HTTP transport options
// shared by connect() (the session mcpClient uses for Request/doRequest) and
// ensureRawTransport (the independent transport CallRPC uses): the same HTTP
// client and the same dynamically-read header set, so a header set via
// SetHeader/SetHeaders or the login flow (Authorization) takes effect on
// every in-flight and future request on both.
func (t *Transport) streamableHTTPOptions() []mcptransport.StreamableHTTPCOption {
	opts := []mcptransport.StreamableHTTPCOption{
		mcptransport.WithHTTPBasicClient(t.client),
		mcptransport.WithHTTPHeaderFunc(t.dynamicHeaders),
	}
	if t.cfg.Timeout > 0 {
		opts = append(opts, mcptransport.WithHTTPTimeout(t.cfg.Timeout))
	}
	return opts
}

// dynamicHeaders snapshots the transport's current custom headers for the
// mcp-go client to attach to each outgoing request. Read under RLock so a
// concurrent SetHeader/SetHeaders (or the login flow setting Authorization)
// is observed by the very next request.
func (t *Transport) dynamicHeaders(context.Context) map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	headers := make(map[string]string, len(t.headers))
	for k, v := range t.headers {
		headers[k] = v
	}
	return headers
}

// connect establishes the real MCP Streamable HTTP session used by
// Request/doRequest: it builds the mcp-go client over the transport's HTTP
// client, starts the Streamable HTTP transport, and negotiates the MCP
// protocol version with Initialize.
func (t *Transport) connect(ctx context.Context) error {
	mcpClient, err := mcpclient.NewStreamableHttpClient(t.endpoint, t.streamableHTTPOptions()...)
	if err != nil {
		return &InitializationError{
			Op:      "mcp.initialize",
			Message: "failed to create MCP client",
			Err:     err,
		}
	}

	if err := mcpClient.Start(ctx); err != nil {
		return &InitializationError{
			Op:      "mcp.initialize",
			Message: "failed to start MCP session",
			Err:     err,
		}
	}

	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: t.protocolVersion,
			ClientInfo:      mcp.Implementation{Name: "QuicBaaS Go SDK", Version: "1.0.0"},
			Capabilities:    mcp.ClientCapabilities{},
		},
	}

	result, err := mcpClient.Initialize(ctx, initReq)
	if err != nil {
		_ = mcpClient.Close()
		return &InitializationError{
			Op:      "mcp.initialize",
			Message: "failed to initialize MCP session",
			Err:     err,
		}
	}

	t.serverInfo = &serverInfo{Name: result.ServerInfo.Name, Version: result.ServerInfo.Version}
	t.mcpClient = mcpClient
	t.lastHealth.Store(time.Now().UnixNano())

	return nil
}

// ensureRawTransport lazily constructs and starts the independent MCP
// transport backing CallRPC (rpc.go), separate from mcpClient's session:
// CallRPC forwards a caller-owned JSON-RPC frame verbatim (including the
// caller's own "initialize"), so it must not share connect()'s session
// state or its already-negotiated protocol version.
func (t *Transport) ensureRawTransport(ctx context.Context) (mcptransport.Interface, error) {
	t.rawTransportMu.Lock()
	defer t.rawTransportMu.Unlock()

	if t.rawTransport != nil {
		return t.rawTransport, nil
	}

	raw, err := mcptransport.NewStreamableHTTP(t.endpoint, t.streamableHTTPOptions()...)
	if err != nil {
		return nil, &InitializationError{
			Op:      "mcp.CallRPC",
			Message: "failed to create MCP transport",
			Err:     err,
		}
	}

	if err := raw.Start(ctx); err != nil {
		return nil, &InitializationError{
			Op:      "mcp.CallRPC",
			Message: "failed to start MCP transport",
			Err:     err,
		}
	}

	t.rawTransport = raw
	return raw, nil
}

// callTool invokes an MCP tool via the negotiated mcpClient session.
func (t *Transport) callTool(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
	result, err := t.callToolResult(ctx, name, arguments)
	if err != nil {
		return nil, err
	}
	return t.extractContent(result.Content)
}

// callToolResult calls a tool and returns its raw result, converting a
// transport failure or a tool error into an SDK error.
func (t *Transport) callToolResult(ctx context.Context, name string, arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	result, err := t.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: arguments,
		},
	})
	if err != nil {
		return nil, t.convertHTTPError(err, "mcp.callTool")
	}

	// Check for tool error
	if result.IsError {
		return nil, t.extractToolError(result)
	}

	t.lastHealth.Store(time.Now().UnixNano())
	return result, nil
}

// sendRPC forwards a caller-owned JSON-RPC 2.0 request verbatim over the
// independent raw MCP transport (see ensureRawTransport) and returns the
// "result" member of the response. It backs ONLY CallRPC (rpc.go); the
// Request/doRequest path uses callTool over the negotiated mcpClient
// session instead.
func (t *Transport) sendRPC(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	raw, err := t.ensureRawTransport(ctx)
	if err != nil {
		return nil, err
	}

	id := t.requestID.Add(1)
	req := mcptransport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(id),
		Method:  method,
		Params:  params,
	}

	resp, err := raw.SendRequest(ctx, req)
	if err != nil {
		return nil, t.convertHTTPError(err, "mcp.sendRPC")
	}

	if resp.Error != nil {
		rpcErr := &JSONRPCError{Code: resp.Error.Code, Message: resp.Error.Message, Data: resp.Error.Data}
		return nil, rpcErr.ToSDKError("mcp.sendRPC")
	}

	// Update health timestamp on success
	t.lastHealth.Store(time.Now().UnixNano())

	return resp.Result, nil
}

// mapToToolName maps a service/method combination to an MCP tool name.
// Pattern: quicbaas_<lowercase_operation>
func (t *Transport) mapToToolName(service, method string) string {
	return ToolName(service, method)
}

// ToolName derives the MCP tool name the server registers for an SDK
// service/method pair. It is the single source of truth for SDK-side tool
// naming and is exported so the server package can assert, at build time,
// that every name the SDK can emit is actually registered.
func ToolName(service, method string) string {
	// If method is already a fully-formed tool name (starts with "quicbaas_"
	// or "qrdb_"), return it as-is. This handles cases where the CLI passes
	// pre-constructed tool names directly.
	if strings.HasPrefix(method, "quicbaas_") || strings.HasPrefix(method, "qrdb_") {
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

	// Orchestrator session tools are named after the operation, not the RPC
	// (quicbaas_orchestrator_prompt, not _prompt_session), so the generic
	// service_method derivation below does not reach them.
	if isMCPOrchestratorService(service) {
		if toolName, ok := orchestratorMethodTools[method]; ok {
			return toolName
		}
	}

	// Pooled provider account tools are likewise named after the operation
	// (agent_account_rotate, not _rotate_credential).
	if isMCPAccountService(service) {
		if toolName, ok := accountMethodTools[method]; ok {
			return toolName
		}
	}

	// SSH tools are named after the resource and operation (ssh_key_generate,
	// not _generate_key), so the generic derivation below would never reach
	// them.
	if isMCPSSHService(service) {
		if toolName, ok := sshToolName(method); ok {
			return toolName
		}
	}

	// Dynamic secrets tools are registered under quicbaas_dynamic_secrets_*;
	// the generic derivation collapses the service name to "dynamicsecrets"
	// and would never reach them.
	if isMCPDynamicSecretsService(service) {
		if toolName, ok := dynamicSecretsToolName(method); ok {
			return toolName
		}
	}

	// PIV tools are the xkms compatibility set (xkms.piv.*) — dotted names
	// the generic quicbaas_* derivation would never produce.
	if isMCPPIVService(service) {
		if toolName, ok := pivToolName(method); ok {
			return toolName
		}
	}

	// FIDO2 ceremony tools are likewise the xkms compatibility set
	// (xkms.fido2.*).
	if isMCPFIDO2Service(service) {
		if toolName, ok := fido2ToolName(method); ok {
			return toolName
		}
	}

	// Project tools are named after the resource and operation (project_create,
	// project_grant_fleet_node), not the RPC, and carry no quicbaas_ prefix, so the
	// generic derivation below would never reach them.
	if isMCPProjectService(service) {
		if toolName, ok := projectToolName(method); ok {
			return toolName
		}
	}
	if isMCPSDLCService(service) {
		if toolName, ok := sdlcToolName(method); ok {
			return toolName
		}
	}
	if isMCPAgentsService(service) {
		if toolName, ok := agentsToolName(method); ok {
			return toolName
		}
	}

	// Parameter store tools are quicbaas_parameter_* and Ollama's model tools
	// are resource-first (model_list); the generic derivation reaches neither.
	if isMCPParameterStoreService(service) {
		if verb, ok := parameterStoreVerbs[method]; ok {
			return verb.tool
		}
	}
	if isMCPOllamaService(service) {
		if verb, ok := ollamaVerbs[method]; ok {
			return verb.tool
		}
	}
	if isMCPPromptsService(service) {
		if verb, ok := promptVerbs[method]; ok {
			return verb.tool
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
		// Core KV operations are qrdb-mirror tools (canonical qrdb_*).
		// Quicbaas-specific methods (ListEntityTypes, Watch, etc.) keep
		// the quicbaas_ prefix.
		switch method {
		case "Put", "Get", "Delete", "List", "Scan":
			return "qrdb_" + strings.ToLower(method)
		default:
			return "quicbaas_" + strings.ToLower(method)
		}
	case "Batch":
		return "quicbaas_batch_" + camelToSnake(method)
	case "Health":
		switch method {
		case "Check":
			return "qrdb_health"
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
		// The MCP server registers several KMS tools under SHORTENED names
		// (the "Key" segment elided); map those explicitly, generic
		// camelToSnake covers the rest. Keep in sync with
		// pkg/api/mcp/tools_kms.go registrations.
		kmsToolMap := map[string]string{
			"GetKeyMetadata":          "quicbaas_kms_get_metadata",
			"UpdateKeyMetadata":       "quicbaas_kms_update_metadata",
			"SetKeyPolicy":            "quicbaas_kms_set_policy",
			"GetKeyPolicy":            "quicbaas_kms_get_policy",
			"SetKeyRotationPolicy":    "quicbaas_kms_set_rotation_policy",
			"GetKeyRotationPolicy":    "quicbaas_kms_get_rotation_policy",
			"DeleteKeyRotationPolicy": "quicbaas_kms_delete_rotation_policy",
			"ListKeyVersions":         "quicbaas_kms_key_versions",
			"UpdateKeyStatus":         "quicbaas_kms_key_status_update",
		}
		if name, ok := kmsToolMap[method]; ok {
			return name
		}
		return "quicbaas_kms_" + camelToSnake(method)
	case "DEK":
		// DEK method names already embed the resource noun ("CreateDEK"), so
		// the generic derivation would double it (quicbaas_dek_create_dek).
		// Keep in sync with pkg/api/mcp/tools_kms_dek.go registrations.
		dekToolMap := map[string]string{
			"CreateDEK":               "quicbaas_dek_create",
			"GetDEK":                  "quicbaas_dek_get",
			"ListDEKs":                "quicbaas_dek_list",
			"RotateDEK":               "quicbaas_dek_rotate",
			"DeleteDEK":               "quicbaas_dek_delete",
			"GetDEKStatus":            "quicbaas_dek_status",
			"SetDefaultDEK":           "quicbaas_dek_set_default",
			"SetDEKSafety":            "quicbaas_dek_set_safety",
			"ClearDEKSafety":          "quicbaas_dek_clear_safety",
			"StartReencryption":       "quicbaas_dek_start_reencryption",
			"GetReencryptionProgress": "quicbaas_dek_get_reencryption_progress",
		}
		if toolName, ok := dekToolMap[method]; ok {
			return toolName
		}
		return "quicbaas_dek_" + camelToSnake(method)
	case "Secrets":
		return "quicbaas_secrets_" + camelToSnake(method)
	case "Crypto":
		return "quicbaas_crypto_" + camelToSnake(method)
	case "RBAC":
		// Roles only: the RBAC client has no user verbs, and the MCP surface has
		// no user mutation tools outside the quicbaas_iam_user_* family. See #905.
		rbacToolMap := map[string]string{
			"CreateRole": "qrdb_role_create",
			"DeleteRole": "qrdb_role_delete",
			"ListRoles":  "qrdb_role_list",
			"AssignRole": "qrdb_role_assign",
			"RevokeRole": "quicbaas_role_revoke",
		}
		if toolName, ok := rbacToolMap[method]; ok {
			return toolName
		}
		return "quicbaas_rbac_" + camelToSnake(method)
	case "Auth":
		switch method {
		case "BeginLogin":
			return "quicbaas_login_begin"
		case "FinishLogin":
			return "quicbaas_login_finish"
		default:
			return "quicbaas_" + strings.ToLower(method)
		}
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
		return mapVectorMethodToTool(method)
	case "Timeseries", "TimeSeries":
		// TimeSeries tools use the pattern: quicbaas_ts_<operation>
		// Maps SDK method names to MCP tool names
		return mapTimeseriesMethodToTool(method)
	case "Operations":
		return "quicbaas_" + strings.ToLower(method)
	case "Certificate":
		if toolName, ok := mapCertificateMethodToTool(method); ok {
			return toolName
		}
		return "quicbaas_certificate_" + camelToSnake(method)
	case "Messaging":
		if toolName, ok := mapMessagingMethodToTool(method); ok {
			return toolName
		}
		return "quicbaas_msg_" + camelToSnake(method)
	case "ObjectStorage":
		if toolName, ok := mapObjectStorageMethodToTool(method); ok {
			return toolName
		}
		return "quicbaas_object_" + camelToSnake(method)
	case "ObjectStorageAdmin":
		return objectStorageAdminToolName(method)
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

// mapVectorMethodToTool maps Vector SDK method names to MCP tool names. Methods
// that mirror the qrdb Vector surface route to qrdb_vector_*; quicbaas-specific
// methods (HybridSearch, SearchByMetadata, GetStats, etc.) keep the
// quicbaas_vector_ prefix.
func mapVectorMethodToTool(method string) string {
	vectorToolMap := map[string]string{
		"CreateNamespace":  "qrdb_vector_create_namespace",
		"DeleteNamespace":  "qrdb_vector_delete_namespace",
		"ListNamespaces":   "qrdb_vector_list_namespaces",
		"GetNamespaceInfo": "qrdb_vector_get_namespace_info",
		"Upsert":           "qrdb_vector_upsert",
		"Get":              "qrdb_vector_get",
		"Delete":           "qrdb_vector_delete",
		"BatchUpsert":      "qrdb_vector_batch_upsert",
		"BatchDelete":      "qrdb_vector_batch_delete",
		"SearchSimilar":    "qrdb_vector_search_similar",
		"SearchByID":       "qrdb_vector_search_by_id",
		"RebuildIndex":     "qrdb_vector_rebuild_index",
		"List":             "qrdb_vector_list",
		"UpdateMetadata":   "qrdb_vector_update_metadata",
	}
	if toolName, ok := vectorToolMap[method]; ok {
		return toolName
	}
	// Quicbaas-specific vector methods fall through to the legacy prefix.
	return "quicbaas_vector_" + camelToSnake(method)
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
// Note: Do NOT require the content type to be "text" - the MCP server may
// return content items without the type field explicitly set. Just check
// for the first item with non-empty text.
func (t *Transport) extractContent(content []mcp.Content) (interface{}, error) {
	for _, item := range content {
		text, ok := mcp.AsTextContent(item)
		if !ok || text.Text == "" {
			continue
		}
		// Parse JSON from text
		var result interface{}
		if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
			// Return raw text if not valid JSON
			return text.Text, nil
		}
		return result, nil
	}

	return nil, nil
}

// extractToolError turns a failed tool result into the SDK error its
// structured error code names, with the text content as the message.
func (t *Transport) extractToolError(result *mcp.CallToolResult) error {
	rpcErr := &JSONRPCError{
		Code:    toolErrorCode(result.StructuredContent),
		Message: "tool execution failed",
	}
	for _, item := range result.Content {
		if text, ok := mcp.AsTextContent(item); ok && text.Text != "" {
			rpcErr.Message = text.Text
			break
		}
	}
	return rpcErr.ToSDKError("mcp.callTool")
}

// convertHTTPError converts MCP client / HTTP transport errors to QBErrors.
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

	// Classify protocol-level JSON-RPC failures the mcp-go client surfaces as
	// sentinel-wrapped errors (a failure completing "tools/call" itself, e.g.
	// an unknown tool name — distinct from a tool-level IsError result).
	switch {
	case stderrors.Is(err, mcp.ErrMethodNotFound), stderrors.Is(err, mcp.ErrResourceNotFound):
		return errors.Wrap(err, errors.CodeNotFound, op, "not found")
	case stderrors.Is(err, mcp.ErrInvalidParams), stderrors.Is(err, mcp.ErrInvalidRequest), stderrors.Is(err, mcp.ErrParseError):
		return errors.Wrap(err, errors.CodeInvalidInput, op, "invalid request")
	case stderrors.Is(err, mcp.ErrRequestInterrupted):
		return errors.Wrap(err, errors.CodeCanceled, op, "request interrupted")
	case stderrors.Is(err, mcp.ErrInternalError):
		return errors.Wrap(err, errors.CodeInternal, op, "internal error")
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
	// The registry watch is served as a long poll (registry_handler.go).
	if isMCPRegistryService(service) {
		return t.openRegistryWatch(ctx, method, req)
	}
	return nil, errors.New(errors.CodeUnimplemented, "mcp.RequestStream",
		"MCP transport does not support bidirectional streaming")
}

// Conn returns the underlying HTTP client.
func (t *Transport) Conn() interface{} {
	return t.client
}

// Close closes the transport, including the negotiated MCP session (if one
// was ever established) and the independent CallRPC transport (if used).
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	var firstErr error

	if t.mcpClient != nil {
		if err := t.mcpClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	t.rawTransportMu.Lock()
	raw := t.rawTransport
	t.rawTransportMu.Unlock()
	if raw != nil {
		if err := raw.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Close idle connections
	if t.client != nil && t.client.Transport != nil {
		if httpTransport, ok := t.client.Transport.(*http.Transport); ok {
			httpTransport.CloseIdleConnections()
		}
	}

	t.authMu.Lock()
	if t.password != nil {
		t.password.Destroy()
		t.password = nil
	}
	t.optionErr = nil
	t.accessToken = ""
	t.mu.Lock()
	delete(t.headers, "Authorization")
	t.mu.Unlock()
	t.authMu.Unlock()

	return firstErr
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
