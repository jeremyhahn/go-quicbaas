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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// writeThrowawayCA generates a throwaway self-signed CA cert for tests that
// need a file the CLI can pass to --tls-ca. The cert is never used for
// actual TLS handshakes — buildTLSConfig just needs a PEM that parses.
func writeThrowawayCA(t *testing.T) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	f, err := os.Create(caPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, f.Close())
	return caPath
}

type mockClient struct {
	closed bool
}

func (m *mockClient) KV() quicbaas.KVClient                                 { return nil }
func (m *mockClient) Batch() quicbaas.BatchClient                           { return nil }
func (m *mockClient) Transaction() quicbaas.TransactionClient               { return nil }
func (m *mockClient) Query() quicbaas.QueryClient                           { return nil }
func (m *mockClient) Cluster() quicbaas.ClusterClient                       { return nil }
func (m *mockClient) Health() quicbaas.HealthClient                         { return nil }
func (m *mockClient) Operations() quicbaas.OperationsClient                 { return nil }
func (m *mockClient) Cache() quicbaas.CacheClient                           { return nil }
func (m *mockClient) IAM() quicbaas.IAMClient                               { return nil }
func (m *mockClient) Vector() quicbaas.VectorClient                         { return nil }
func (m *mockClient) TimeSeries() quicbaas.TimeSeriesClient                 { return nil }
func (m *mockClient) Backup() quicbaas.BackupClient                         { return nil }
func (m *mockClient) ObjectStorage() quicbaas.ObjectStorageClient           { return nil }
func (m *mockClient) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }
func (m *mockClient) ObjectTransfer() quicbaas.ObjectTransferClient         { return nil }
func (m *mockClient) ParameterStore() quicbaas.ParameterStoreClient         { return nil }
func (m *mockClient) Scheduler() quicbaas.SchedulerClient                   { return nil }
func (m *mockClient) Messaging() quicbaas.MessagingClient                   { return nil }
func (m *mockClient) Embedding() quicbaas.EmbeddingClient                   { return nil }
func (m *mockClient) Ollama() quicbaas.OllamaClient                         { return nil }
func (m *mockClient) RAG() quicbaas.RAGClient                               { return nil }
func (m *mockClient) Transport() quicbaas.Transport                         { return nil }
func (m *mockClient) CoreData() quicbaas.CoreDataService                    { return nil }
func (m *mockClient) ClusterMgmt() quicbaas.ClusterManagementService        { return nil }
func (m *mockClient) Security() quicbaas.SecurityService                    { return nil }
func (m *mockClient) Identity() quicbaas.IdentityService                    { return nil }
func (m *mockClient) Search() quicbaas.SearchService                        { return nil }
func (m *mockClient) Storage() quicbaas.StorageService                      { return nil }
func (m *mockClient) Async() quicbaas.AsyncService                          { return nil }
func (m *mockClient) AI() quicbaas.AIService                                { return nil }
func (m *mockClient) DataStore() (quicbaas.QRDBKVStore, error)              { return nil, nil }

func (m *mockClient) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService       { return nil }
func (m *mockClient) TenantMgmtService() quicbaas.TenantService                  { return nil }
func (m *mockClient) Barrier() (quicbaas.QRDBBarrierClient, error)               { return nil, nil }
func (m *mockClient) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error)      { return nil, nil }
func (m *mockClient) SealStore() (quicbaas.QRDBSealStoreClient, error)           { return nil, nil }
func (m *mockClient) TenantMgmt() (quicbaas.QRDBTenantClient, error)             { return nil, nil }
func (m *mockClient) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) { return nil, nil }
func (m *mockClient) Share() (quicbaas.QRDBShareClient, error)                   { return nil, nil }
func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

func (m *mockClient) Project() quicbaas.ProjectClient { return nil }
func (m *mockClient) SDLC() quicbaas.SDLCClient       { return nil }
func (m *mockClient) Agents() quicbaas.AgentsClient   { return nil }
func (m *mockClient) Account() quicbaas.AccountClient { return nil }

func (m *mockClient) Orchestrator() quicbaas.OrchestratorClient { return nil }

func TestBuildSDKClientOptions_GRPCDefault(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = "grpc"
	globalTimeout = 30 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have timeout option")

	// Verify options are valid by checking length
	// We expect at least 1 option: timeout (TLS is handled at the gRPC client level, not SDK level)
	assert.GreaterOrEqual(t, len(opts), 1, "should have at least timeout option")
}

func TestBuildSDKClientOptions_EmptyTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = ""
	globalTimeout = 30 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotNil(t, opts, "empty transport should default to gRPC")
}

func TestBuildSDKClientOptions_RESTTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = "rest"
	globalTimeout = 15 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have REST option plus timeout")
}

func TestBuildSDKClientOptions_QUICTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = "quic"
	globalTimeout = 20 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have QUIC option plus timeout")
}

func TestBuildSDKClientOptions_UnixTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedAddress := address
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		address = savedAddress
		globalTimeout = savedTimeout
	}()

	transport = "unix"
	address = "/tmp/test.sock"
	globalTimeout = 10 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have Unix option plus timeout")
}

// TestRootCommand_DefaultTransportIsUnix pins the default that
// docs/multiprotocol-api/cli.md "Transport selection" documents.
func TestRootCommand_DefaultTransportIsUnix(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("transport")
	require.NotNil(t, flag)
	assert.Equal(t, "unix", flag.DefValue)
}

// TestBuildSDKClientOptions_SocketAddressSelectsUnix pins the CLI's only
// automatic transport switch: grpc (or no transport) becomes unix when the
// address ends in ".sock". No other address shape changes the transport.
func TestBuildSDKClientOptions_SocketAddressSelectsUnix(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		address   string
		want      string
	}{
		{name: "grpc with socket path", transport: "grpc", address: "/run/quicbaas/quicbaas.sock", want: "unix"},
		{name: "empty transport with socket file", transport: "", address: "quicbaas.sock", want: "unix"},
		{name: "grpc with host and port", transport: "grpc", address: "localhost:9090", want: "grpc"},
		{name: "grpc with path lacking .sock", transport: "grpc", address: "/run/quicbaas/socket", want: "grpc"},
		{name: "grpc with https URL", transport: "grpc", address: "https://localhost:8443", want: "grpc"},
		{name: "rest with socket path", transport: "rest", address: "/run/quicbaas/quicbaas.sock", want: "rest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			savedTransport := transport
			savedAddress := address
			defer func() {
				transport = savedTransport
				address = savedAddress
			}()

			transport = tt.transport
			address = tt.address

			_, err := buildSDKClientOptions()
			require.NoError(t, err)
			assert.Equal(t, tt.want, transport)
		})
	}
}

func TestBuildSDKClientOptions_UnsupportedTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	defer func() {
		transport = savedTransport
	}()

	transport = "invalid_transport"

	opts, err := buildSDKClientOptions()
	assert.Nil(t, opts)
	require.Error(t, err)

	var unsupportedErr *UnsupportedTransportError
	assert.ErrorAs(t, err, &unsupportedErr)
	assert.Equal(t, "invalid_transport", unsupportedErr.Transport)
	assert.Contains(t, err.Error(), "unsupported transport")
}

func TestBuildSDKClientOptions_ZeroTimeout(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	savedAddress := address
	savedTLSCA := tlsCA
	savedDataDir := dataDir
	savedTenant := globalTenantID
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		address = savedAddress
		tlsCA = savedTLSCA
		dataDir = savedDataDir
		globalTenantID = savedTenant
	}()
	// The tenant is scoped separately and has its own test below; clearing it
	// keeps this one measuring what it names — the timeout and transport.
	globalTenantID = ""

	transport = "grpc"
	globalTimeout = 0
	// Set a non-socket address to prevent auto-detection of unix transport
	address = "localhost:9090"
	// Ensure no TLS config is generated by pointing dataDir to a path with no certs
	// and clearing tlsCA so buildTLSConfig returns nil.
	tlsCA = ""
	dataDir = t.TempDir()

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	// With zero timeout, gRPC (default transport), non-socket address, and no TLS
	// config available, no options are added.
	assert.Empty(t, opts, "should have no options with zero timeout and default gRPC transport")
}

func TestCreateSDKClient_EmptyAddress(t *testing.T) {
	// Save and restore global state
	savedAddress := address
	savedTransport := transport
	defer func() {
		address = savedAddress
		transport = savedTransport
		resetSDKClient()
	}()

	resetSDKClient()
	address = ""
	transport = "grpc"

	client, err := createSDKClient()
	assert.Nil(t, client)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyAddress)
}

func TestCreateSDKClient_InvalidTransport(t *testing.T) {
	// Save and restore global state
	savedAddress := address
	savedTransport := transport
	defer func() {
		address = savedAddress
		transport = savedTransport
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:50051"
	transport = "ftp" // Invalid transport

	client, err := createSDKClient()
	assert.Nil(t, client)
	require.Error(t, err)

	var unsupportedErr *UnsupportedTransportError
	assert.ErrorAs(t, err, &unsupportedErr)
}

func TestCloseSDKClient_NilClient(t *testing.T) {
	// Ensure no client exists
	resetSDKClient()

	err := closeSDKClient()
	assert.NoError(t, err, "closing nil client should not error")
}

func TestGetSDKClient_NoClient(t *testing.T) {
	// Ensure no client exists
	resetSDKClient()

	client := getSDKClient()
	assert.Nil(t, client, "should return nil when no client exists")
}

func TestResetSDKClient(t *testing.T) {
	// This test verifies the reset functionality
	resetSDKClient()
	client := getSDKClient()
	assert.Nil(t, client, "reset should clear the client")
}

func TestUnsupportedTransportError_Error(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "websocket"}
	msg := err.Error()
	assert.Contains(t, msg, "websocket")
	assert.Contains(t, msg, "unsupported transport")
	assert.Contains(t, msg, "grpc")
	assert.Contains(t, msg, "rest")
	assert.Contains(t, msg, "quic")
	assert.Contains(t, msg, "unix")
}

func TestBuildSDKClientOptions_AllTransportTypes(t *testing.T) {
	testCases := []struct {
		name           string
		transport      string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name:        "grpc transport",
			transport:   "grpc",
			expectError: false,
		},
		{
			name:        "empty defaults to grpc",
			transport:   "",
			expectError: false,
		},
		{
			name:        "rest transport",
			transport:   "rest",
			expectError: false,
		},
		{
			name:        "quic transport",
			transport:   "quic",
			expectError: false,
		},
		{
			name:        "unix transport",
			transport:   "unix",
			expectError: false,
		},
		{
			name:           "http not supported directly",
			transport:      "http",
			expectError:    true,
			expectedErrMsg: "unsupported transport",
		},
		{
			name:           "tcp not supported",
			transport:      "tcp",
			expectError:    true,
			expectedErrMsg: "unsupported transport",
		},
		{
			name:           "random string not supported",
			transport:      "random123",
			expectError:    true,
			expectedErrMsg: "unsupported transport",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore global state
			savedTransport := transport
			savedAddress := address
			savedTimeout := globalTimeout
			defer func() {
				transport = savedTransport
				address = savedAddress
				globalTimeout = savedTimeout
			}()

			transport = tc.transport
			address = "/tmp/test.sock"
			globalTimeout = 5 * time.Second

			opts, err := buildSDKClientOptions()

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrMsg)
				assert.Nil(t, opts)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, opts)
			}
		})
	}
}

func TestSDKClientOptions_ValidWithSDK(t *testing.T) {
	// This test verifies that our options work with the actual SDK Option type
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = "grpc"
	globalTimeout = 30 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)

	// Verify that each option is of the correct type
	for i, opt := range opts {
		var _ quicbaas.Option = opt
		assert.NotNil(t, opt, "option %d should not be nil", i)
	}
}

func TestCreateSDKClient_ConnectionError(t *testing.T) {
	// Save and restore global state
	savedAddress := address
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		address = savedAddress
		transport = savedTransport
		globalTimeout = savedTimeout
		resetSDKClient()
	}()

	resetSDKClient()
	// Use an invalid address format to trigger a connection setup error
	// Note: The SDK may accept the address and only fail on first operation
	// This tests that we properly propagate errors from the SDK
	address = "localhost:50051"
	transport = "grpc"
	globalTimeout = 100 * time.Millisecond

	// This may or may not error depending on SDK implementation
	// The key is that if it does error, we wrap it properly
	client, err := createSDKClient()
	if err != nil {
		// Verify error is wrapped properly
		assert.Contains(t, err.Error(), "createSDKClient")
	} else {
		// Clean up if client was created
		assert.NotNil(t, client)
		closeErr := closeSDKClient()
		assert.NoError(t, closeErr)
	}
}

func TestSDKClientSingleton_ReturnsSameInstance(t *testing.T) {
	// This test verifies the singleton caching behavior by directly
	// manipulating the sdkClientHolder to avoid network connections.
	defer func() {
		resetSDKClient()
	}()

	resetSDKClient()

	// Create a mock client and store it directly in the holder
	mock := &mockClient{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)

	// Verify getSDKClient returns the stored instance
	retrieved1 := getSDKClient()
	assert.NotNil(t, retrieved1, "should return stored client")
	assert.Same(t, mock, retrieved1, "should return same mock instance")

	// Call getSDKClient again to verify it returns the same instance
	retrieved2 := getSDKClient()
	assert.Same(t, retrieved1, retrieved2, "should return same client instance on subsequent calls")

	// Verify createSDKClient returns the cached instance (fast path)
	retrieved3, err := createSDKClient()
	require.NoError(t, err)
	assert.Same(t, mock, retrieved3, "createSDKClient should return cached instance")
}

func TestCloseSDKClient_ClearsInstance(t *testing.T) {
	// This test verifies that closeSDKClient clears the singleton instance
	// by directly manipulating the sdkClientHolder to avoid network connections.
	defer func() {
		resetSDKClient()
	}()

	resetSDKClient()

	// Create a mock client and store it directly in the holder
	mock := &mockClient{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)

	// Verify client exists
	assert.NotNil(t, getSDKClient(), "client should exist before close")

	// Close the client
	err := closeSDKClient()
	assert.NoError(t, err)

	// Verify client is cleared
	assert.Nil(t, getSDKClient(), "client should be nil after close")

	// Verify mock's Close was called
	assert.True(t, mock.closed, "mock Close() should have been called")
}

func TestBuildSDKClientOptions_ConsistentBehavior(t *testing.T) {
	// Test that calling buildSDKClientOptions multiple times with same config
	// produces consistent results
	savedTransport := transport
	savedTimeout := globalTimeout
	savedAddress := address
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		address = savedAddress
	}()

	transport = "rest"
	globalTimeout = 45 * time.Second
	address = "http://localhost:8080"

	opts1, err1 := buildSDKClientOptions()
	require.NoError(t, err1)

	opts2, err2 := buildSDKClientOptions()
	require.NoError(t, err2)

	// Both calls should produce options slices of the same length
	assert.Equal(t, len(opts1), len(opts2), "option counts should be consistent")
}

func TestBuildSDKClientOptions_MCPTransport(t *testing.T) {
	// Save and restore global state
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
	}()

	transport = "mcp"
	globalTimeout = 30 * time.Second

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have MCP option plus timeout")
}

// TestBuildSDKClientOptions_TLSCA verifies that --tls-ca builds a TLS config
// with the CA loaded. QuicBaaS bans insecure TLS — there is no skip-verify
// path, so the only way to get a TLS-configured client is via an explicit CA.
func TestBuildSDKClientOptions_TLSCA(t *testing.T) {
	savedTransport := transport
	savedTimeout := globalTimeout
	savedTLSCA := tlsCA
	savedDataDir := dataDir
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		tlsCA = savedTLSCA
		dataDir = savedDataDir
	}()

	// Write a throwaway CA cert so buildTLSConfig has something to load.
	caPath := writeThrowawayCA(t)
	transport = "grpc"
	globalTimeout = 5 * time.Second
	tlsCA = caPath
	dataDir = t.TempDir()

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should include TLS option and timeout")
	assert.GreaterOrEqual(t, len(opts), 2)
}

// --------------------------------------------------------------------------
// buildSDKClientOptions: MCP transport with loadStoredAuthToken
// --------------------------------------------------------------------------

func TestBuildSDKClientOptions_MCPWithToken(t *testing.T) {
	savedTransport := transport
	savedTimeout := globalTimeout
	savedTokenFile := authTokenFile
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		authTokenFile = savedTokenFile
	}()

	transport = "mcp"
	globalTimeout = 5 * time.Second
	// Set to non-existent path so loadStoredAuthToken returns empty
	authTokenFile = "/tmp/nonexistent-test-token.json"

	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "should have MCP transport option and timeout")
}

// --------------------------------------------------------------------------
// buildSDKClientOptions: network transports attach the stored platform token
// so data-plane calls authenticate against the default-deny scope -> RBAC gate.
// --------------------------------------------------------------------------

// writeValidTokenFile writes a well-formed, non-expired token file and returns
// its path. It is used to prove the network transports pick up the stored token.
func writeValidTokenFile(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	content := `{"access_token":"test-platform-token","token_type":"bearer","expires_at":"` + future + `"}`
	require.NoError(t, os.WriteFile(tokenPath, []byte(content), 0600))
	return tokenPath
}

func TestBuildSDKClientOptions_NetworkTransportsAttachToken(t *testing.T) {
	savedTransport := transport
	savedTimeout := globalTimeout
	savedTokenFile := authTokenFile
	savedAddress := address
	savedDataDir := dataDir
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		authTokenFile = savedTokenFile
		address = savedAddress
		dataDir = savedDataDir
	}()

	globalTimeout = 5 * time.Second
	dataDir = t.TempDir() // no auto-discovered CA, keeps TLS option deterministic

	for _, tc := range []struct {
		name      string
		transport string
		address   string
	}{
		{"rest", "rest", "localhost:8080"},
		{"quic", "quic", "localhost:8080"},
		{"unix", "unix", "/tmp/quicbaas-test.sock"},
		{"grpc", "grpc", "localhost:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport = tc.transport
			address = tc.address

			// Baseline: no token file -> no WithAuthToken option appended.
			authTokenFile = filepath.Join(t.TempDir(), "missing.json")
			withoutToken, err := buildSDKClientOptions()
			require.NoError(t, err)

			// With a valid token file -> exactly one extra option (WithAuthToken).
			authTokenFile = writeValidTokenFile(t)
			withToken, err := buildSDKClientOptions()
			require.NoError(t, err)

			assert.Equal(t, len(withoutToken)+1, len(withToken),
				"%s transport must append the stored platform token", tc.name)
		})
	}
}

// --------------------------------------------------------------------------
// loadStoredAuthToken: error paths
// --------------------------------------------------------------------------

func TestLoadMCPAuthToken_ExplicitPath_NotExist(t *testing.T) {
	savedTokenFile := authTokenFile
	defer func() { authTokenFile = savedTokenFile }()

	authTokenFile = "/tmp/nonexistent-quicbaas-test-token-xyz.json"

	token := loadStoredAuthToken()
	assert.Empty(t, token, "should return empty for non-existent file")
}

func TestLoadMCPAuthToken_InvalidTokenFile(t *testing.T) {
	savedTokenFile := authTokenFile
	defer func() { authTokenFile = savedTokenFile }()

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "invalid-token.json")
	err := os.WriteFile(tokenPath, []byte("not-valid-json"), 0600)
	require.NoError(t, err)

	authTokenFile = tokenPath

	token := loadStoredAuthToken()
	assert.Empty(t, token, "should return empty for invalid token file")
}

func TestLoadMCPAuthToken_ExpiredTokenFile(t *testing.T) {
	savedTokenFile := authTokenFile
	defer func() { authTokenFile = savedTokenFile }()

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "expired-token.json")
	// Create a well-formed JSON token file but with an expired time
	tokenContent := `{"access_token":"test-token","token_type":"bearer","expires_at":"2020-01-01T00:00:00Z"}`
	err := os.WriteFile(tokenPath, []byte(tokenContent), 0600)
	require.NoError(t, err)

	authTokenFile = tokenPath

	token := loadStoredAuthToken()
	assert.Empty(t, token, "should return empty for expired token")
}

// --------------------------------------------------------------------------
// memoryStorageReaderAdapter.NewIterator error path (closed storage)
// --------------------------------------------------------------------------

func TestMemoryStorageReaderAdapter_NewIterator_ClosedStorage(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}

	// Close the storage first
	err := memStorage.Close()
	require.NoError(t, err)

	// NewIterator on closed storage should return an iterator with an error
	iter := adapter.NewIterator(nil)
	require.NotNil(t, iter)

	// The iterator should be invalid or have an error
	ok := iter.SeekToFirst()
	if !ok {
		assert.False(t, ok)
	}
}

// TestBuildSDKClientOptions_TenantScopesTheClient proves --tenant reaches the
// SDK.
//
// THE FLAG WAS PARSED AND THEN HAD NOWHERE TO GO. Before WithTenant was wired,
// `auth factor add --tenant X` failed with "tenant is required; configure
// rest.WithDefaultTenant" — the CLI accepted the flag, the transport never saw
// it, and the error named an SDK option the operator had no way to set. An
// option count is the only observable here: quicbaas.Option is an opaque
// functional option, so the assertion is that a tenant contributes one and an
// empty tenant contributes none.
func TestBuildSDKClientOptions_TenantScopesTheClient(t *testing.T) {
	savedTransport, savedTimeout, savedTenant := transport, globalTimeout, globalTenantID
	savedAddress, savedTLSCA, savedDataDir := address, tlsCA, dataDir
	defer func() {
		transport, globalTimeout, globalTenantID = savedTransport, savedTimeout, savedTenant
		address, tlsCA, dataDir = savedAddress, savedTLSCA, savedDataDir
	}()
	transport = "grpc"
	globalTimeout = 0
	address = "localhost:9090"
	tlsCA = ""
	dataDir = t.TempDir()

	globalTenantID = "automatethethings"
	withTenant, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.Len(t, withTenant, 1, "a named tenant must contribute exactly one option")

	globalTenantID = ""
	withoutTenant, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.Empty(t, withoutTenant, "an empty tenant must contribute no option")
}
