package quicbaas

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestCredentialStrategy_REST_SendsPlatformBearer pins that over REST,
// Security().Credentials() carries the bearer set by WithAuthToken on every
// credential request, as Security().Ceremony() does. Without it an
// operational node's default-deny gate answers 401 "platform bearer
// authorization is missing" to every credential verb.
func TestCredentialStrategy_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
		seen bool
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/credentials/strategy", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		seen = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"strategy":"manual"}`)); err != nil {
			t.Errorf("write credential strategy body: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-1700"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	creds, err := c.Security().Credentials()
	if err != nil {
		t.Fatalf("Security().Credentials(): %v", err)
	}
	strategy, err := creds.CredentialStrategy(context.Background())
	if err != nil {
		t.Fatalf("CredentialStrategy: %v", err)
	}
	if strategy.Strategy != "manual" {
		t.Errorf("strategy: got %q, want %q", strategy.Strategy, "manual")
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("credential strategy never reached the server")
	}
	if got, want := auth, "Bearer t-1700"; got != want {
		t.Errorf("Authorization on /v1/credentials/strategy: got %q, want %q", got, want)
	}
}

// fakeCredentialClient records the outgoing authorization metadata of every
// credential verb it receives.
type fakeCredentialClient struct {
	got map[string][]string
}

func (f *fakeCredentialClient) rec(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.got[verb] = md.Get("authorization")
}

func (f *fakeCredentialClient) CredentialSubmit(ctx context.Context, _ *qrdbtransport.CredentialSubmitRequest) error {
	f.rec(ctx, "CredentialSubmit")
	return nil
}

func (f *fakeCredentialClient) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	f.rec(ctx, "CredentialStrategy")
	return &qrdbtransport.CredentialStrategyResponse{}, nil
}

func (f *fakeCredentialClient) CredentialDelete(ctx context.Context, _ string) error {
	f.rec(ctx, "CredentialDelete")
	return nil
}

func (f *fakeCredentialClient) CredentialList(ctx context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	f.rec(ctx, "CredentialList")
	return &qrdbtransport.CredentialListResponse{}, nil
}

func (f *fakeCredentialClient) CredentialRotate(ctx context.Context, _ *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	f.rec(ctx, "CredentialRotate")
	return &qrdbtransport.CredentialRotateResponse{}, nil
}

func (f *fakeCredentialClient) CredentialRetrieveVersion(ctx context.Context, _ *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
	f.rec(ctx, "CredentialRetrieveVersion")
	return &qrdbtransport.CredentialRetrieveVersionResponse{}, nil
}

func (f *fakeCredentialClient) CredentialListVersions(ctx context.Context, _ *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	f.rec(ctx, "CredentialListVersions")
	return &qrdbtransport.CredentialListVersionsResponse{}, nil
}

func (f *fakeCredentialClient) CredentialGCExpired(ctx context.Context, _ *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	f.rec(ctx, "CredentialGCExpired")
	return &qrdbtransport.CredentialGCResponse{}, nil
}

func (f *fakeCredentialClient) CredentialStore(ctx context.Context, _ string, _ []byte) error {
	f.rec(ctx, "CredentialStore")
	return nil
}

func (f *fakeCredentialClient) CredentialRetrieve(ctx context.Context, _ string) ([]byte, error) {
	f.rec(ctx, "CredentialRetrieve")
	return nil, nil
}

// TestCredentialClient_GRPCAndUnix_SendPlatformBearer pins that over gRPC
// and Unix, Security().Credentials() wraps go-qrdb's gRPC sub-client so every
// credential verb carries the bearer set by WithAuthToken as outgoing
// authorization metadata, and leaves it unwrapped without a token.
func TestCredentialClient_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
	cases := []struct {
		name   string
		listen func(t *testing.T) (net.Listener, []Option)
	}{
		{"grpc", func(t *testing.T) (net.Listener, []Option) {
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			return lis, nil
		}},
		{"unix", func(t *testing.T) (net.Listener, []Option) {
			socket := filepath.Join(shortSocketDir(t), "qrdb.sock")
			lis, err := net.Listen("unix", socket)
			require.NoError(t, err)
			return lis, []Option{WithUnix(socket)}
		}},
	}
	for _, tc := range cases {
		for _, token := range []string{"t-1700", ""} {
			name := tc.name + "/token"
			if token == "" {
				name = tc.name + "/no-token"
			}
			t.Run(name, func(t *testing.T) {
				lis, extra := tc.listen(t)
				serveQRDBBarrierGRPC(t, lis)

				applied := extra
				if token != "" {
					applied = append([]Option{WithAuthToken(token)}, extra...)
				}
				opts := defaultOptions()
				for _, apply := range applied {
					require.NoError(t, apply(opts))
				}
				c := &client{opts: opts, address: lis.Addr().String()}
				t.Cleanup(func() {
					if qc, err := c.qrdbClient(); err == nil {
						if err := qc.Close(); err != nil {
							t.Errorf("close qrdb client: %v", err)
						}
					}
				})

				got, err := (&securityService{c: c}).Credentials()
				require.NoError(t, err)
				wrapped, ok := got.(*qrdbBearerCredentialClient)
				if token == "" {
					assert.False(t, ok, "no token: go-qrdb's client is returned unwrapped")
					return
				}
				require.True(t, ok, "with a token the gRPC sub-client is wrapped, got %T", got)
				assert.Equal(t, "Bearer "+token, wrapped.bearer)
			})
		}
	}

	fake := &fakeCredentialClient{got: map[string][]string{}}
	b := &qrdbBearerCredentialClient{inner: fake, bearer: "Bearer t-1700"}
	ctx := context.Background()
	calls := []func() error{
		func() error { return b.CredentialSubmit(ctx, nil) },
		func() error { _, err := b.CredentialStrategy(ctx); return err },
		func() error { return b.CredentialDelete(ctx, "n") },
		func() error { _, err := b.CredentialList(ctx, nil); return err },
		func() error { _, err := b.CredentialRotate(ctx, nil); return err },
		func() error { _, err := b.CredentialRetrieveVersion(ctx, nil); return err },
		func() error { _, err := b.CredentialListVersions(ctx, nil); return err },
		func() error { _, err := b.CredentialGCExpired(ctx, nil); return err },
		func() error { return b.CredentialStore(ctx, "n", []byte("v")) },
		func() error { _, err := b.CredentialRetrieve(ctx, "n"); return err },
	}
	for _, call := range calls {
		require.NoError(t, call())
	}
	require.Len(t, fake.got, len(calls), "every credential verb reached the inner client")
	for verb, auth := range fake.got {
		assert.Equal(t, []string{"Bearer t-1700"}, auth, verb)
	}
}

// TestCredentialClient_MCP_UsesSDKTransport pins that over MCP,
// Security().Credentials() calls the qrdb_credential_* tools through the SDK
// MCP transport's own session, so every request carries the platform bearer.
// go-qrdb's MCP sub-client sends none and an operational node refuses its
// initialize with 401.
func TestCredentialClient_MCP_UsesSDKTransport(t *testing.T) {
	const want = "Bearer t-1700"
	server := mcpserver.NewMCPServer("credential-bearer", "1.0.0")
	server.AddTool(mcpgo.NewTool("qrdb_credential_strategy"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText(`{"strategy":"manual"}`), nil
	})
	mcpHandler := mcpserver.NewStreamableHTTPServer(server)
	var (
		mu    sync.Mutex
		auths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		auths = append(auths, auth)
		mu.Unlock()
		if auth != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL+"/mcp", WithMCP(), WithMCPAuthToken("t-1700"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	creds, err := c.Security().Credentials()
	require.NoError(t, err)
	_, ok := creds.(*mcp.QRDBCredentialClient)
	require.True(t, ok, "Security().Credentials() over MCP: got %T, want *mcp.QRDBCredentialClient", creds)

	strategy, err := creds.CredentialStrategy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "manual", strategy.Strategy)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, auths)
	for _, a := range auths {
		assert.Equal(t, want, a)
	}
}
