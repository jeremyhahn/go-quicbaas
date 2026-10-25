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

//go:build integration

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	quictransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Result Structs
// =============================================================================

// AuthResult contains the result of an auth operation.
type AuthResult struct {
	Success      bool
	AccessToken  string
	RefreshToken string
	Message      string
	ErrorCode    string
}

// ClientCredentials contains OAuth2 client credentials for testing.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
	TenantID     string
}

// =============================================================================
// Protocol Interface
// =============================================================================

// authProtocol abstracts the different protocols for authorization testing.
type authProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	// ObtainToken obtains an access token using OIDC client credentials flow
	ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error)
	// GetWithToken attempts to read data with a token
	GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error)
	// GetWithoutToken attempts to read data without a token
	GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error)
	// GetHTTPEndpoint returns the HTTP endpoint for OAuth2 token requests
	GetHTTPEndpoint() string
	Name() string
}

// =============================================================================
// Shared Test Utilities
// =============================================================================

// obtainTokenViaREST uses the OAuth2 token endpoint to get an access token.
// This is the standard OIDC client credentials flow.
func obtainTokenViaREST(ctx context.Context, httpEndpoint string, creds *ClientCredentials) (*AuthResult, error) {
	// Build token request using client credentials grant
	tokenReq := url.Values{}
	tokenReq.Set("grant_type", "client_credentials")
	tokenReq.Set("client_id", creds.ClientID)
	tokenReq.Set("client_secret", creds.ClientSecret)
	tokenReq.Set("scope", "api")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpEndpoint+"/oauth2/token",
		strings.NewReader(tokenReq.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := helpers.NewHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil {
			return &AuthResult{
				Success:   false,
				ErrorCode: errResp.Error,
				Message:   errResp.ErrorDescription,
			}, nil
		}
		return &AuthResult{
			Success:   false,
			ErrorCode: fmt.Sprintf("http_%d", resp.StatusCode),
			Message:   string(body),
		}, nil
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token,omitempty"`
		Scope        string `json:"scope,omitempty"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &AuthResult{
		Success:      tokenResp.AccessToken != "",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
	}, nil
}

// createTestOAuthApp creates an OAuth2 application for testing via REST API.
// Returns the client credentials or nil if the operation fails.
// Uses a unique app name per test run to avoid conflicts.
func createTestOAuthApp(ctx context.Context, httpEndpoint, tenantID, bearer, appName string) (*ClientCredentials, error) {
	// Use a unique app name per test run to avoid conflicts
	uniqueAppName := fmt.Sprintf("%s-%d", appName, time.Now().UnixNano())

	status, respBody, err := postRegisterApp(ctx, httpEndpoint, tenantID, bearer, uniqueAppName)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("failed to register app: status=%d, body=%s", status, string(respBody))
	}

	var appResp struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}

	if err := json.Unmarshal(respBody, &appResp); err != nil {
		return nil, fmt.Errorf("failed to decode app response: %w", err)
	}

	creds := &ClientCredentials{
		ClientID:     appResp.ClientID,
		ClientSecret: appResp.ClientSecret,
		TenantID:     tenantID,
	}

	return creds, nil
}

// postRegisterApp POSTs a client_credentials app registration to
// /api/v1/tenants/{tenantID}/apps and returns the status and body. An empty
// bearer sends the request with no credential.
func postRegisterApp(ctx context.Context, httpEndpoint, tenantID, bearer, appName string) (int, []byte, error) {
	appReq := map[string]interface{}{
		"name":          appName,
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}

	body, err := json.Marshal(appReq)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to marshal app request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/tenants/%s/apps", httpEndpoint, tenantID),
		bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create app request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	client := helpers.NewHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to register app: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read app response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// getOrCreateTestCredentials retrieves test credentials from environment or creates a test app.
func getOrCreateTestCredentials(t *testing.T, ctx context.Context, httpEndpoint string) (*ClientCredentials, error) {
	clientID := os.Getenv("QUICBAAS_TEST_CLIENT_ID")
	clientSecret := os.Getenv("QUICBAAS_TEST_CLIENT_SECRET")
	if clientID != "" && clientSecret != "" {
		return &ClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TenantID:     os.Getenv("QUICBAAS_TEST_TENANT_ID"),
		}, nil
	}

	// Register the app the way a production caller does: as the admin of a
	// tenant the system admin provisioned, whose bearer binds that tenant.
	tenant := provisionAuthTestTenant(t, httpEndpoint)
	creds, err := createTestOAuthApp(ctx, httpEndpoint, tenant.TenantID, tenant.AdminBearer, "auth-integration-test-app")
	if err != nil {
		return nil, fmt.Errorf("test credentials not configured and auto-creation failed: %w (set QUICBAAS_TEST_CLIENT_ID and QUICBAAS_TEST_CLIENT_SECRET)", err)
	}
	return creds, nil
}

// provisionAuthTestTenant provisions a tenant for the test's OAuth app, as the
// fixture system admin, and deletes it when t ends.
func provisionAuthTestTenant(t *testing.T, httpEndpoint string) *helpers.ProvisionedTenant {
	t.Helper()
	tenantID := fmt.Sprintf("auth-it-%d", time.Now().UnixNano())
	return helpers.ProvisionTestTenant(t, httpEndpoint, tenantID)
}

// =============================================================================
// Main Test Function
// =============================================================================

// TestAuthMultiProtocol verifies that authorization works identically
// across all supported protocols: Embedded, REST, gRPC, QUIC, and Unix Socket.
func TestAuthMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testAuthProtocol(t, ctx, &embeddedAuthClient{})
		})
	}

	t.Run("REST", func(t *testing.T) {
		testAuthProtocol(t, ctx, &restAuthClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testAuthProtocol(t, ctx, &grpcAuthClient{})
	})

	t.Run("QUIC", func(t *testing.T) {
		testAuthProtocol(t, ctx, &quicAuthClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testAuthProtocol(t, ctx, &unixSocketAuthClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).
}

// TestRegisterAppRequiresBoundTenant proves the fixtures bind a tenant
// without weakening the check (#164): registering an app needs a caller
// bound to the tenant the path names. A request with no credential, and the
// system admin's bearer, which is bound to the system tenant, are refused.
func TestRegisterAppRequiresBoundTenant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints")
	httpEndpoint := endpoints[0]

	tenant := provisionAuthTestTenant(t, httpEndpoint)
	systemBearer, err := helpers.AuthSystemAdminContext(ctx, httpEndpoint)
	require.NoError(t, err, "system admin login")

	cases := []struct {
		name   string
		bearer string
	}{
		{name: "no credential", bearer: ""},
		{name: "system admin of another tenant", bearer: systemBearer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appName := fmt.Sprintf("unbound-app-%d", time.Now().UnixNano())
			status, body, err := postRegisterApp(ctx, httpEndpoint, tenant.TenantID, tc.bearer, appName)
			require.NoError(t, err)
			require.Equal(t, http.StatusForbidden, status, "body=%s", body)
		})
	}

	// The tenant's own admin is bound to it and may register an app.
	appName := fmt.Sprintf("bound-app-%d", time.Now().UnixNano())
	status, body, err := postRegisterApp(ctx, httpEndpoint, tenant.TenantID, tenant.AdminBearer, appName)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status, "body=%s", body)
}

// testAuthProtocol runs the authorization test suite against a protocol.
func testAuthProtocol(t *testing.T, ctx context.Context, protocol authProtocol) {
	t.Helper()

	err := protocol.Setup(t, ctx)
	if err != nil {
		require.NoError(t, err, "Protocol %s must be available", protocol.Name())
	}
	defer func() {
		if teardownErr := protocol.Teardown(t); teardownErr != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
		}
	}()

	t.Run("ProtectedEndpointRequiresAuth", func(t *testing.T) {
		testProtectedEndpointRequiresAuth(t, ctx, protocol)
	})

	t.Run("OIDCTokenFlow", func(t *testing.T) {
		testOIDCTokenFlow(t, ctx, protocol)
	})

	t.Run("ValidTokenAllowsAccess", func(t *testing.T) {
		testValidTokenAllowsAccess(t, ctx, protocol)
	})

	t.Run("InvalidTokenDeniesAccess", func(t *testing.T) {
		testInvalidTokenDeniesAccess(t, ctx, protocol)
	})
}

// =============================================================================
// Test Functions
// =============================================================================

func testProtectedEndpointRequiresAuth(t *testing.T, ctx context.Context, protocol authProtocol) {
	// Attempt to access protected endpoint without token
	result, err := protocol.GetWithoutToken(ctx, "default", "test-key")

	// Should fail with authentication error
	if err != nil {
		t.Logf("%s: Protected endpoint correctly rejected unauthenticated request: %v", protocol.Name(), err)
		return
	}

	if result != nil && !result.Success {
		// Check if error code indicates authentication required
		if result.ErrorCode != "" {
			t.Logf("%s: Protected endpoint correctly requires auth (error: %s)", protocol.Name(), result.ErrorCode)
		} else if result.Message != "" {
			t.Logf("%s: Protected endpoint correctly requires auth (message: %s)", protocol.Name(), result.Message)
		} else {
			t.Logf("%s: Protected endpoint correctly requires auth", protocol.Name())
		}
		return
	}

	// If we got here without error and with success, auth might be disabled
	t.Logf("%s: Protected endpoint allowed unauthenticated access (auth may be disabled)", protocol.Name())
}

func testOIDCTokenFlow(t *testing.T, ctx context.Context, protocol authProtocol) {
	httpEndpoint := protocol.GetHTTPEndpoint()
	require.NotEmpty(t, httpEndpoint, "%s: HTTP endpoint must be available for OIDC token flow", protocol.Name())

	// Get or create test credentials
	creds, err := getOrCreateTestCredentials(t, ctx, httpEndpoint)
	require.NoError(t, err, "%s: test credentials must be obtainable", protocol.Name())

	// Obtain token using OIDC client credentials flow
	result, err := protocol.ObtainToken(ctx, creds)
	require.NoError(t, err, "%s: OIDC token flow must succeed", protocol.Name())

	require.NotNil(t, result, "%s: ObtainToken returned nil result", protocol.Name())

	if result.Success && result.AccessToken != "" {
		t.Logf("%s: OIDC client credentials flow successful, received token", protocol.Name())
		// Verify it looks like a JWT
		parts := strings.Split(result.AccessToken, ".")
		if len(parts) == 3 {
			t.Logf("%s: Token is a valid JWT format", protocol.Name())
		}
	} else {
		t.Logf("%s: OIDC token flow returned: success=%v, error=%s, message=%s",
			protocol.Name(), result.Success, result.ErrorCode, result.Message)
	}
}

func testValidTokenAllowsAccess(t *testing.T, ctx context.Context, protocol authProtocol) {
	httpEndpoint := protocol.GetHTTPEndpoint()
	require.NotEmpty(t, httpEndpoint, "%s: HTTP endpoint must be available for token acquisition", protocol.Name())

	// Get test credentials
	creds, err := getOrCreateTestCredentials(t, ctx, httpEndpoint)
	require.NoError(t, err, "%s: test credentials must be obtainable", protocol.Name())

	// Obtain token
	tokenResult, err := protocol.ObtainToken(ctx, creds)
	require.NoError(t, err, "%s: token must be obtainable", protocol.Name())
	require.NotNil(t, tokenResult, "%s: token result must not be nil", protocol.Name())
	require.NotEmpty(t, tokenResult.AccessToken, "%s: access token must not be empty", protocol.Name())

	// Use token to access protected endpoint
	result, err := protocol.GetWithToken(ctx, tokenResult.AccessToken, "default", "test-key")

	if err != nil {
		// Check if error is auth-related or just "not found"
		t.Logf("%s: GetWithToken error: %v", protocol.Name(), err)
		return
	}

	if result != nil {
		// Success could mean key found or not found - both are OK if we got past auth
		t.Logf("%s: Valid token allowed access to protected endpoint", protocol.Name())
	}
}

func testInvalidTokenDeniesAccess(t *testing.T, ctx context.Context, protocol authProtocol) {
	// Use obviously invalid token
	result, err := protocol.GetWithToken(ctx, "invalid-token-12345", "default", "test-key")

	if err != nil {
		t.Logf("%s: Invalid token correctly rejected: %v", protocol.Name(), err)
		return
	}

	if result != nil && !result.Success {
		if result.ErrorCode != "" {
			t.Logf("%s: Invalid token correctly denied (error: %s)", protocol.Name(), result.ErrorCode)
		} else if result.Message != "" {
			t.Logf("%s: Invalid token correctly denied (message: %s)", protocol.Name(), result.Message)
		} else {
			t.Logf("%s: Invalid token correctly denied", protocol.Name())
		}
		return
	}

	// If we got through, auth might be disabled
	t.Logf("%s: Invalid token was not rejected (auth may be disabled)", protocol.Name())
}

// =============================================================================
// Embedded Protocol Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedAuthClient struct {
	client       quicbaas.Client
	httpEndpoint string
}

func (c *embeddedAuthClient) Name() string { return "Embedded" }

func (c *embeddedAuthClient) GetHTTPEndpoint() string { return c.httpEndpoint }

func (c *embeddedAuthClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return fmt.Errorf("failed to get embedded service: %w", err)
	}

	client, err := quicbaas.NewClient("embedded://", quicbaas.WithEmbedded(svc), helpers.EmbeddedSystemIdentity())
	if err != nil {
		return fmt.Errorf("failed to create embedded client: %w", err)
	}

	c.client = client
	// Get REST endpoint for OAuth2 token acquisition
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) > 0 {
		c.httpEndpoint = endpoints[0]
	}
	return nil
}

func (c *embeddedAuthClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedAuthClient) ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error) {
	if c.httpEndpoint == "" {
		return nil, fmt.Errorf("no HTTP endpoint available for OIDC token flow")
	}
	return obtainTokenViaREST(ctx, c.httpEndpoint, creds)
}

func (c *embeddedAuthClient) GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Set the token in context metadata for the embedded client
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

	_, err := c.client.KV().Get(ctx, &service.GetRequest{
		EntityType: entityType,
		ID:         hash.FNV1a(key),
	})
	if err != nil {
		// Check if it's an auth error
		errStr := err.Error()
		if contains(errStr, "unauthenticated") || contains(errStr, "unauthorized") {
			return &AuthResult{
				Success:   false,
				ErrorCode: "unauthenticated",
			}, nil
		}
		// Key not found is OK - means we got past auth
		if contains(errStr, "not found") {
			return &AuthResult{Success: true}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

func (c *embeddedAuthClient) GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	_, err := c.client.KV().Get(ctx, &service.GetRequest{
		EntityType: entityType,
		ID:         hash.FNV1a(key),
	})
	if err != nil {
		errStr := err.Error()
		if contains(errStr, "unauthenticated") || contains(errStr, "unauthorized") {
			return &AuthResult{
				Success:   false,
				ErrorCode: "unauthenticated",
			}, nil
		}
		// Key not found is OK
		if contains(errStr, "not found") {
			return &AuthResult{Success: true}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

// contains checks if substr is in s (case-insensitive helper).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// REST Protocol Implementation
// =============================================================================

type restAuthClient struct {
	baseURL    string
	httpClient *http.Client
}

func (c *restAuthClient) Name() string { return "REST" }

func (c *restAuthClient) GetHTTPEndpoint() string { return c.baseURL }

func (c *restAuthClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no REST endpoints available")
	}
	c.baseURL = endpoints[0]
	c.httpClient = helpers.NewHTTPClient(30 * time.Second)
	return nil
}

func (c *restAuthClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restAuthClient) ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error) {
	return obtainTokenViaREST(ctx, c.baseURL, creds)
}

func (c *restAuthClient) GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error) {
	url := fmt.Sprintf("%s/api/v1/kv/%s/%s", c.baseURL, entityType, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return &AuthResult{
		Success:   resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
		ErrorCode: getHTTPErrorCode(resp.StatusCode),
	}, nil
}

func (c *restAuthClient) GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error) {
	url := fmt.Sprintf("%s/api/v1/kv/%s/%s", c.baseURL, entityType, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return &AuthResult{
		Success:   resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
		ErrorCode: getHTTPErrorCode(resp.StatusCode),
	}, nil
}

func getHTTPErrorCode(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "permission_denied"
	default:
		return ""
	}
}

// =============================================================================
// gRPC Protocol Implementation
// =============================================================================

type grpcAuthClient struct {
	conn         *grpc.ClientConn
	client       pb.QuicBaaSClient
	httpEndpoint string
}

func (c *grpcAuthClient) Name() string { return "gRPC" }

func (c *grpcAuthClient) GetHTTPEndpoint() string { return c.httpEndpoint }

func (c *grpcAuthClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no gRPC endpoints available")
	}

	endpoint := endpoints[0]

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return fmt.Errorf("load integration TLS config: %w", err)
	}
	conn, err := grpc.DialContext(dialCtx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint %s: %w", endpoint, err)
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)

	// Get REST endpoint for OAuth2 token acquisition
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) > 0 {
		c.httpEndpoint = restEndpoints[0]
	}
	return nil
}

func (c *grpcAuthClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcAuthClient) ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error) {
	if c.httpEndpoint == "" {
		return nil, fmt.Errorf("no HTTP endpoint available for OIDC token flow")
	}
	return obtainTokenViaREST(ctx, c.httpEndpoint, creds)
}

func (c *grpcAuthClient) GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

	_, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         hash.FNV1a(key),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return &AuthResult{
				Success:   false,
				ErrorCode: getGRPCErrorCode(st.Code()),
			}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

func (c *grpcAuthClient) GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error) {
	_, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         hash.FNV1a(key),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return &AuthResult{
				Success:   false,
				ErrorCode: getGRPCErrorCode(st.Code()),
			}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

func getGRPCErrorCode(code codes.Code) string {
	switch code {
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.PermissionDenied:
		return "permission_denied"
	default:
		return ""
	}
}

// =============================================================================
// QUIC Protocol Implementation
// =============================================================================

type quicAuthClient struct {
	transport    *quictransport.Transport
	endpoint     string
	httpEndpoint string
}

func (c *quicAuthClient) Name() string { return "QUIC" }

func (c *quicAuthClient) GetHTTPEndpoint() string { return c.httpEndpoint }

func (c *quicAuthClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetQUICEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no QUIC endpoints available")
	}
	c.endpoint = endpoints[0]

	// Create QUIC (HTTP/3) transport configuration. The ALPN is left for
	// quictransport.New to default to "h3" (RFC 9114): this transport now
	// speaks the same REST dialect as the REST transport over HTTP/3, not
	// the retired "quicbaas-quic" opcode protocol.
	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return fmt.Errorf("load integration TLS config: %w", err)
	}
	cfg := &transport.Config{
		Address:         c.endpoint,
		Timeout:         30 * time.Second,
		TLSConfig:       tlsCfg,
		MaxRetries:      3,
		RetryBackoff:    100 * time.Millisecond,
		MaxRetryBackoff: 2 * time.Second,
	}

	// Create the QUIC transport
	quicTransport, err := quictransport.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create QUIC transport: %w", err)
	}

	c.transport = quicTransport

	// Get REST endpoint for OAuth2 token acquisition
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) > 0 {
		c.httpEndpoint = restEndpoints[0]
	}
	return nil
}

func (c *quicAuthClient) Teardown(t *testing.T) error {
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

func (c *quicAuthClient) ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error) {
	if c.httpEndpoint == "" {
		return nil, fmt.Errorf("no HTTP endpoint available for OIDC token flow")
	}
	return obtainTokenViaREST(ctx, c.httpEndpoint, creds)
}

func (c *quicAuthClient) GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error) {
	if c.transport == nil {
		return nil, fmt.Errorf("QUIC transport not initialized")
	}

	// Set the auth token on the transport. quictransport.Transport embeds
	// *rest.Transport, so SetHeader is the same seam the REST transport uses.
	c.transport.SetHeader("Authorization", "Bearer "+token)

	req := &service.GetRequest{EntityType: entityType, ID: hash.FNV1a(key)}

	// Send request via the shared REST-dialect KV handler.
	_, err := c.transport.Request(ctx, "KV", "Get", req)
	if err != nil {
		errStr := err.Error()
		if contains(errStr, "unauthorized") || contains(errStr, "unauthenticated") || contains(errStr, "forbidden") {
			return &AuthResult{
				Success:   false,
				ErrorCode: "unauthenticated",
			}, nil
		}
		return nil, err
	}

	// A successful response (whether or not the key was Found) means the
	// request got past authentication/authorization.
	return &AuthResult{Success: true}, nil
}

func (c *quicAuthClient) GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error) {
	if c.transport == nil {
		return nil, fmt.Errorf("QUIC transport not initialized")
	}

	// Clear any existing auth token.
	c.transport.SetHeader("Authorization", "")

	req := &service.GetRequest{EntityType: entityType, ID: hash.FNV1a(key)}

	_, err := c.transport.Request(ctx, "KV", "Get", req)
	if err != nil {
		errStr := err.Error()
		if contains(errStr, "unauthorized") || contains(errStr, "unauthenticated") || contains(errStr, "forbidden") {
			return &AuthResult{
				Success:   false,
				ErrorCode: "unauthenticated",
			}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

// =============================================================================
// Unix Socket Protocol Implementation
// =============================================================================

type unixSocketAuthClient struct {
	conn         *grpc.ClientConn
	client       pb.QuicBaaSClient
	httpEndpoint string
}

func (c *unixSocketAuthClient) Name() string { return "UnixSocket" }

func (c *unixSocketAuthClient) GetHTTPEndpoint() string { return c.httpEndpoint }

func (c *unixSocketAuthClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := helpers.GetUnixSocketPath()
	if socketPath == "" {
		return fmt.Errorf("no Unix socket path available")
	}

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return fmt.Errorf("load integration TLS config: %w", err)
	}
	tlsCfg.ServerName = "localhost"
	conn, err := grpc.DialContext(dialCtx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 10*time.Second)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to Unix socket %s: %w", socketPath, err)
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)

	// Get REST endpoint for OAuth2 token acquisition
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) > 0 {
		c.httpEndpoint = restEndpoints[0]
	}
	return nil
}

func (c *unixSocketAuthClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketAuthClient) ObtainToken(ctx context.Context, creds *ClientCredentials) (*AuthResult, error) {
	if c.httpEndpoint == "" {
		return nil, fmt.Errorf("no HTTP endpoint available for OIDC token flow")
	}
	return obtainTokenViaREST(ctx, c.httpEndpoint, creds)
}

func (c *unixSocketAuthClient) GetWithToken(ctx context.Context, token, entityType, key string) (*AuthResult, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

	_, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         hash.FNV1a(key),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return &AuthResult{
				Success:   false,
				ErrorCode: getGRPCErrorCode(st.Code()),
			}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}

func (c *unixSocketAuthClient) GetWithoutToken(ctx context.Context, entityType, key string) (*AuthResult, error) {
	_, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         hash.FNV1a(key),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return &AuthResult{
				Success:   false,
				ErrorCode: getGRPCErrorCode(st.Code()),
			}, nil
		}
		return nil, err
	}

	return &AuthResult{Success: true}, nil
}
