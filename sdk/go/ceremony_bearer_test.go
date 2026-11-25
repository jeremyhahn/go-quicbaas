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

// TestCeremonyStatus_REST_SendsPlatformBearer pins that over REST,
// Security().Ceremony() carries the bearer set by WithAuthToken on every
// ceremony request, as Security().Barrier() does. Without it an operational
// node's default-deny gate answers 401 "platform bearer authorization is
// missing" to ceremony status.
func TestCeremonyStatus_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
		seen bool
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/ceremony/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		seen = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"state":"complete","spki_pin":"pin-1700"}`)); err != nil {
			t.Errorf("write ceremony status body: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-1700"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ceremony, err := c.Security().Ceremony()
	if err != nil {
		t.Fatalf("Security().Ceremony(): %v", err)
	}
	status, err := ceremony.CeremonyStatus(context.Background())
	if err != nil {
		t.Fatalf("CeremonyStatus: %v", err)
	}
	if status.State != "complete" {
		t.Errorf("state: got %q, want %q", status.State, "complete")
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("ceremony status never reached the server")
	}
	if got, want := auth, "Bearer t-1700"; got != want {
		t.Errorf("Authorization on /v1/ceremony/status: got %q, want %q", got, want)
	}
}

// fakeCeremonyClient records the outgoing authorization metadata of every
// ceremony verb it receives.
type fakeCeremonyClient struct {
	got map[string][]string
}

func (f *fakeCeremonyClient) rec(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.got[verb] = md.Get("authorization")
}

func (f *fakeCeremonyClient) CeremonyInitialize(ctx context.Context, _ *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	f.rec(ctx, "CeremonyInitialize")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	f.rec(ctx, "CeremonyStatus")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyClaimCertBegin(ctx context.Context, _ *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error) {
	f.rec(ctx, "CeremonyClaimCertBegin")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyClaimCertComplete(ctx context.Context, _ *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error) {
	f.rec(ctx, "CeremonyClaimCertComplete")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyBeginClaimShare(ctx context.Context, _ *qrdbtransport.BeginClaimShareRequest) (*qrdbtransport.BeginClaimShareResponse, error) {
	f.rec(ctx, "CeremonyBeginClaimShare")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyCompleteClaimShare(ctx context.Context, _ *qrdbtransport.CompleteClaimShareRequest) (*qrdbtransport.CompleteClaimShareResponse, error) {
	f.rec(ctx, "CeremonyCompleteClaimShare")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonySignCSR(ctx context.Context, _ *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	f.rec(ctx, "CeremonySignCSR")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyClaimShareBegin(ctx context.Context, _ *qrdbtransport.CeremonyClaimShareBeginRequest) (*qrdbtransport.CeremonyClaimShareBeginResponse, error) {
	f.rec(ctx, "CeremonyClaimShareBegin")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyClaimShareComplete(ctx context.Context, _ *qrdbtransport.CeremonyClaimShareCompleteRequest) (*qrdbtransport.CeremonyCustodianInfo, error) {
	f.rec(ctx, "CeremonyClaimShareComplete")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonySubmitUnsealShare(ctx context.Context, _ *qrdbtransport.CeremonySubmitUnsealShareRequest) (*qrdbtransport.CeremonySubmitUnsealShareResponse, error) {
	f.rec(ctx, "CeremonySubmitUnsealShare")
	return nil, nil
}

func (f *fakeCeremonyClient) CeremonyEnrollExternalOfficer(ctx context.Context, _ *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error) {
	f.rec(ctx, "CeremonyEnrollExternalOfficer")
	return nil, nil
}

// TestCeremonyClient_GRPCAndUnix_SendPlatformBearer: over gRPC and unix,
// Security().Ceremony() wraps go-qrdb's gRPC sub-client so every ceremony
// verb carries the WithAuthToken bearer as outgoing authorization metadata;
// with no token set, go-qrdb's client is returned unwrapped.
func TestCeremonyClient_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
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

				for _, cc := range []func() (QRDBCeremonyClient, error){
					(&securityService{c: c}).Ceremony,
					(&barrierCeremonyService{c: c}).Ceremony,
				} {
					got, err := cc()
					require.NoError(t, err)
					wrapped, ok := got.(*qrdbBearerCeremonyClient)
					if token == "" {
						assert.False(t, ok, "no token: go-qrdb's client is returned unwrapped")
						continue
					}
					require.True(t, ok, "with a token the gRPC sub-client is wrapped, got %T", got)
					assert.Equal(t, "Bearer "+token, wrapped.bearer)
				}
			})
		}
	}

	fake := &fakeCeremonyClient{got: map[string][]string{}}
	b := &qrdbBearerCeremonyClient{inner: fake, bearer: "Bearer t-1700"}
	ctx := context.Background()
	calls := []func() error{
		func() error { _, err := b.CeremonyInitialize(ctx, nil); return err },
		func() error { _, err := b.CeremonyStatus(ctx); return err },
		func() error { _, err := b.CeremonyClaimCertBegin(ctx, nil); return err },
		func() error { _, err := b.CeremonyClaimCertComplete(ctx, nil); return err },
		func() error { _, err := b.CeremonyBeginClaimShare(ctx, nil); return err },
		func() error { _, err := b.CeremonyCompleteClaimShare(ctx, nil); return err },
		func() error { _, err := b.CeremonySignCSR(ctx, nil); return err },
		func() error { _, err := b.CeremonyClaimShareBegin(ctx, nil); return err },
		func() error { _, err := b.CeremonyClaimShareComplete(ctx, nil); return err },
		func() error { _, err := b.CeremonySubmitUnsealShare(ctx, nil); return err },
		func() error { _, err := b.CeremonyEnrollExternalOfficer(ctx, nil); return err },
	}
	for _, call := range calls {
		require.NoError(t, call())
	}
	require.Len(t, fake.got, len(calls), "every ceremony verb reached the inner client")
	for verb, auth := range fake.got {
		assert.Equal(t, []string{"Bearer t-1700"}, auth, verb)
	}
}

// TestCeremonyClient_MCP_UsesSDKTransport: over MCP, Security().Ceremony()
// returns the SDK MCP transport itself, which carries the platform bearer,
// not go-qrdb's bearer-less MCP sub-client.
func TestCeremonyClient_MCP_UsesSDKTransport(t *testing.T) {
	const want = "Bearer t-1700"
	server := mcpserver.NewMCPServer("ceremony-bearer", "1.0.0")
	server.AddTool(mcpgo.NewTool("qrdb_ceremony_status"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText(`{"state":"complete"}`), nil
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

	ceremony, err := c.Security().Ceremony()
	require.NoError(t, err)
	_, ok := ceremony.(*mcp.Transport)
	require.True(t, ok, "Security().Ceremony() over MCP: got %T, want *mcp.Transport", ceremony)
	bcm, err := c.BarrierCeremonyMgmt().Ceremony()
	require.NoError(t, err)
	_, ok = bcm.(*mcp.Transport)
	require.True(t, ok, "BarrierCeremonyMgmt().Ceremony() over MCP: got %T, want *mcp.Transport", bcm)

	status, err := ceremony.CeremonyStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "complete", status.State)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, auths)
	for _, a := range auths {
		assert.Equal(t, want, a)
	}
}
