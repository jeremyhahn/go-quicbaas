package quicbaas

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// TestQRDBAudit_REST_SendsPlatformBearer proves Security().QRDBAudit() over
// REST sends the configured bearer on QueryAudit.
func TestQRDBAudit_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-1659"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ac, err := c.Security().QRDBAudit()
	require.NoError(t, err)
	_, err = ac.QueryAudit(context.Background(), QRDBAuditFilter{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Bearer t-1659", auth)
}

// TestQRDBAudit_QUIC_SendsPlatformBearer proves the QUIC selection sends the
// bearer over HTTP/3 through the REST transport's audit client.
func TestQRDBAudit_QUIC_SendsPlatformBearer(t *testing.T) {
	var (
		mu    sync.Mutex
		auth  string
		proto int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth, proto = r.Header.Get("Authorization"), r.ProtoMajor
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})

	cert := h3BarrierTestCert(t)
	addr := startUDPOnlyHTTP3Server(t, cert, mux)

	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)
	c, err := NewClient(addr,
		WithQUIC(),
		WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}),
		WithAuthToken("t-1659"),
		WithTimeout(5*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ac, err := c.Security().QRDBAudit()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = ac.QueryAudit(ctx, QRDBAuditFilter{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Bearer t-1659", auth)
	assert.Equal(t, 3, proto, "request protocol major version")
}

// TestQRDBAudit_MCP_SendsPlatformBearer proves the MCP selection runs the
// audit tool on a session that carries the bearer: the server refuses any
// request without it.
func TestQRDBAudit_MCP_SendsPlatformBearer(t *testing.T) {
	const token = "t-1659"
	var (
		mu     sync.Mutex
		called bool
	)
	srv := mcpserver.NewMCPServer("qrdb-audit", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_audit_query"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		mu.Lock()
		called = true
		mu.Unlock()
		return mcpgo.NewToolResultText(`{"events":[{"actor":"alice"}],"total":1}`), nil
	})
	h := mcpserver.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	c, err := NewClient(ts.URL, WithMCP(), WithAuthToken(token))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ac, err := c.Security().QRDBAudit()
	require.NoError(t, err)
	page, err := ac.QueryAudit(context.Background(), QRDBAuditFilter{})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.Equal(t, "alice", page.Events[0].Actor)
	assert.Equal(t, 1, page.Total)

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, called, "qrdb_audit_query was not called")
}

type fakeBearerAuditClient struct {
	mu   sync.Mutex
	auth []string
}

func (f *fakeBearerAuditClient) record(ctx context.Context) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, md.Get("authorization")...)
}

func (f *fakeBearerAuditClient) EmitAudit(ctx context.Context, _ QRDBAuditEvent) error {
	f.record(ctx)
	return nil
}

func (f *fakeBearerAuditClient) QueryAudit(ctx context.Context, _ QRDBAuditFilter) (*QRDBAuditPage, error) {
	f.record(ctx)
	return &QRDBAuditPage{}, nil
}

// TestQRDBAudit_BearerClientForwardsQueryAndEmit proves the gRPC/Unix
// wrapper attaches the bearer as authorization metadata on both verbs.
func TestQRDBAudit_BearerClientForwardsQueryAndEmit(t *testing.T) {
	fake := &fakeBearerAuditClient{}
	ac := &qrdbBearerAuditClient{inner: fake, bearer: "Bearer x"}
	ctx := context.Background()

	require.NoError(t, ac.EmitAudit(ctx, QRDBAuditEvent{Actor: "a"}))
	_, err := ac.QueryAudit(ctx, QRDBAuditFilter{})
	require.NoError(t, err)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, []string{"Bearer x", "Bearer x"}, fake.auth)
}
