// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	qrdbapigrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbdek "github.com/jeremyhahn/go-qrdb/pkg/dek"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// tenantKeyringDEKServicer keeps one keyring per tenant and resolves the
// tenant the way go-qrdb's keyring service does: from the request, else from
// the tenant bound on the context, else the system tenant.
type tenantKeyringDEKServicer struct {
	mu      sync.Mutex
	current map[string]uint64
	tenants []string
}

func newTenantKeyringDEKServicer(tenants ...string) *tenantKeyringDEKServicer {
	s := &tenantKeyringDEKServicer{current: map[string]uint64{}}
	for _, id := range tenants {
		s.current[id] = 1
	}
	return s
}

func (s *tenantKeyringDEKServicer) resolve(ctx context.Context, tenantID string) string {
	if tenantID == "" {
		if bound, ok := qrdbsdk.TenantFromContext(ctx); ok && bound != "" {
			tenantID = bound
		} else {
			tenantID = qrdbsdk.SystemTenantID
		}
	}
	s.tenants = append(s.tenants, tenantID)
	if _, ok := s.current[tenantID]; !ok {
		s.current[tenantID] = 1
	}
	return tenantID
}

func (s *tenantKeyringDEKServicer) status(id string) *qrdbdek.Status {
	return &qrdbdek.Status{TenantID: id, Algorithm: qrdbdek.Algorithm, Current: s.current[id],
		Versions: []qrdbdek.VersionInfo{{Version: s.current[id]}}}
}

func (s *tenantKeyringDEKServicer) version(id string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current[id]
}

func (s *tenantKeyringDEKServicer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tenants...)
}

func (s *tenantKeyringDEKServicer) Status(ctx context.Context, tenantID string) (*qrdbdek.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status(s.resolve(ctx, tenantID)), nil
}

func (s *tenantKeyringDEKServicer) Rotate(ctx context.Context, tenantID string) (*qrdbdek.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.resolve(ctx, tenantID)
	s.current[id]++
	return s.status(id), nil
}

func (s *tenantKeyringDEKServicer) Retire(ctx context.Context, tenantID string, _ uint64) (*qrdbdek.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status(s.resolve(ctx, tenantID)), nil
}

func (s *tenantKeyringDEKServicer) Encrypt(ctx context.Context, tenantID string, plaintext, _ []byte) (*qrdbdek.Sealed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.resolve(ctx, tenantID)
	return &qrdbdek.Sealed{Ciphertext: append([]byte("ct:"), plaintext...), Version: s.current[id]}, nil
}

func (s *tenantKeyringDEKServicer) Decrypt(ctx context.Context, tenantID string, ciphertext, _ []byte) (*qrdbdek.Opened, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.resolve(ctx, tenantID)
	return &qrdbdek.Opened{Plaintext: ciphertext[len("ct:"):], Version: s.current[id]}, nil
}

func (s *tenantKeyringDEKServicer) Rewrap(ctx context.Context, tenantID string, ciphertext, _ []byte) (*qrdbdek.Rewrapped, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.resolve(ctx, tenantID)
	return &qrdbdek.Rewrapped{Ciphertext: ciphertext, FromVersion: 1, ToVersion: s.current[id]}, nil
}

// exerciseTenantDEK drives every keyring verb once through c.
func exerciseTenantDEK(t *testing.T, c QRDBDEKClient) {
	t.Helper()
	ctx := context.Background()
	_, err := c.TenantDEKStatus(ctx, &QRDBTenantDEKRequest{})
	require.NoError(t, err, "status")
	_, err = c.TenantDEKRotate(ctx, &QRDBTenantDEKRequest{})
	require.NoError(t, err, "rotate")
	_, err = c.TenantDEKRetire(ctx, &QRDBTenantDEKRetireRequest{Version: 1})
	require.NoError(t, err, "retire")
	sealed, err := c.TenantDEKEncrypt(ctx, &QRDBTenantDEKEncryptRequest{Plaintext: []byte("hi"), AAD: []byte("a")})
	require.NoError(t, err, "encrypt")
	_, err = c.TenantDEKDecrypt(ctx, &QRDBTenantDEKDecryptRequest{Ciphertext: sealed.Ciphertext, AAD: []byte("a")})
	require.NoError(t, err, "decrypt")
	_, err = c.TenantDEKRewrap(ctx, &QRDBTenantDEKRewrapRequest{Ciphertext: sealed.Ciphertext, AAD: []byte("a")})
	require.NoError(t, err, "rewrap")
}

// TestTenantDEK_REST_SendsPlatformBearer proves every keyring verb over REST
// carries the bearer set by WithAuthToken.
func TestTenantDEK_REST_SendsPlatformBearer(t *testing.T) {
	mux, recorded := tenantDEKRecordingMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-dek"), WithTLSConfig(stubTLSConfig(t, srv)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	exerciseTenantDEK(t, keyring)

	assert.Equal(t, map[string]string{
		"GET /v1/dek/status":   "Bearer t-dek",
		"POST /v1/dek/rotate":  "Bearer t-dek",
		"POST /v1/dek/retire":  "Bearer t-dek",
		"POST /v1/dek/encrypt": "Bearer t-dek",
		"POST /v1/dek/decrypt": "Bearer t-dek",
		"POST /v1/dek/rewrap":  "Bearer t-dek",
	}, recorded())
}

// tenantDEKRecordingMux serves the six tenant DEK routes with canned
// responses and records the Authorization header of every call it receives;
// recorded returns a copy of that map.
func tenantDEKRecordingMux() (mux *http.ServeMux, recorded func() map[string]string) {
	var (
		mu    sync.Mutex
		auths = map[string]string{}
	)
	mux = http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	respond := func(v any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			auths[r.Method+" "+r.URL.Path] = r.Header.Get("Authorization")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	status := &QRDBTenantDEKStatusResponse{TenantID: "acme", CurrentVersion: 2}
	mux.HandleFunc("/v1/dek/status", respond(status))
	mux.HandleFunc("/v1/dek/rotate", respond(status))
	mux.HandleFunc("/v1/dek/retire", respond(status))
	mux.HandleFunc("/v1/dek/encrypt", respond(&QRDBTenantDEKEncryptResponse{Ciphertext: []byte("ct"), Version: 2}))
	mux.HandleFunc("/v1/dek/decrypt", respond(&QRDBTenantDEKDecryptResponse{Plaintext: []byte("hi"), Version: 2}))
	mux.HandleFunc("/v1/dek/rewrap", respond(&QRDBTenantDEKRewrapResponse{Ciphertext: []byte("ct"), FromVersion: 1, ToVersion: 2}))
	recorded = func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]string, len(auths))
		for k, v := range auths {
			out[k] = v
		}
		return out
	}
	return mux, recorded
}

// TestTenantDEK_REST_RefusesPlaintextHTTP: over a plain http address the
// REST keyring refuses to send plaintext data and the bearer, so nothing
// reaches the server.
func TestTenantDEK_REST_RefusesPlaintextHTTP(t *testing.T) {
	mux, recorded := tenantDEKRecordingMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-dek"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	_, err = keyring.TenantDEKEncrypt(context.Background(), &QRDBTenantDEKEncryptRequest{Plaintext: []byte("hi"), AAD: []byte("a")})
	require.Error(t, err)
	assert.Empty(t, recorded(), "no request may reach the server")
}

// TestTenantDEK_MCP_SendsBearer proves the MCP session's initialize and every
// keyring tool call carry the bearer: the server refuses any request without it.
func TestTenantDEK_MCP_SendsBearer(t *testing.T) {
	var (
		mu     sync.Mutex
		called []string
		args   = map[string]map[string]any{}
	)
	answer := func(name string, v any) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			mu.Lock()
			called = append(called, name)
			args[name] = req.GetArguments()
			mu.Unlock()
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return mcpgo.NewToolResultText(string(b)), nil
		}
	}
	status := &QRDBTenantDEKStatusResponse{TenantID: "acme", CurrentVersion: 2}
	srv := mcpserver.NewMCPServer("qrdb-dek", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_dek_status"), answer("qrdb_dek_status", status))
	srv.AddTool(mcpgo.NewTool("qrdb_dek_rotate"), answer("qrdb_dek_rotate", status))
	srv.AddTool(mcpgo.NewTool("qrdb_dek_retire"), answer("qrdb_dek_retire", status))
	srv.AddTool(mcpgo.NewTool("qrdb_dek_encrypt"), answer("qrdb_dek_encrypt", &QRDBTenantDEKEncryptResponse{Ciphertext: []byte("ct"), Version: 2}))
	srv.AddTool(mcpgo.NewTool("qrdb_dek_decrypt"), answer("qrdb_dek_decrypt", &QRDBTenantDEKDecryptResponse{Plaintext: []byte("hi"), Version: 2}))
	srv.AddTool(mcpgo.NewTool("qrdb_dek_rewrap"), answer("qrdb_dek_rewrap", &QRDBTenantDEKRewrapResponse{Ciphertext: []byte("ct"), FromVersion: 1, ToVersion: 2}))
	h := mcpserver.NewStreamableHTTPServer(srv)
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-dek" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	c, err := NewClient(ts.URL, WithMCP(), WithAuthToken("t-dek"), WithTLSConfig(stubTLSConfig(t, ts)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	exerciseTenantDEK(t, keyring)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"qrdb_dek_status", "qrdb_dek_rotate", "qrdb_dek_retire",
		"qrdb_dek_encrypt", "qrdb_dek_decrypt", "qrdb_dek_rewrap"}, called)
	assert.EqualValues(t, 1, args["qrdb_dek_retire"]["version"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("hi")), args["qrdb_dek_encrypt"]["plaintext"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("a")), args["qrdb_dek_encrypt"]["aad"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("ct")), args["qrdb_dek_rewrap"]["ciphertext"])
}

func TestTenantDEK_MCP_RefusesPlaintextHTTP(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithMCP(), WithAuthToken("t-dek"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	_, err = keyring.TenantDEKEncrypt(context.Background(), &QRDBTenantDEKEncryptRequest{Plaintext: []byte("hi"), AAD: []byte("a")})
	require.Error(t, err)
	var hopErr *mcp.DEKHopError
	assert.ErrorAs(t, err, &hopErr)
	assert.Equal(t, errors.CodeForbidden, errors.Code(err))
	assert.Zero(t, hits.Load(), "no request may reach the server")
}

// TestTenantDEK_GRPCAndUnix_SendPlatformBearer proves every keyring verb
// over go-qrdb's gRPC and Unix sub-clients carries the bearer as
// authorization metadata.
func TestTenantDEK_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			lis, extra := tc.listen(t)
			var (
				recMu sync.Mutex
				auths []string
			)
			record := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				if strings.Contains(info.FullMethod, "DEK") {
					md, _ := metadata.FromIncomingContext(ctx)
					recMu.Lock()
					auths = append(auths, strings.Join(md.Get("authorization"), ","))
					recMu.Unlock()
				}
				return handler(ctx, req)
			}
			srv := grpc.NewServer(grpc.UnaryInterceptor(record))
			qrdbsdk.RegisterGRPCService(srv, nil, nil, qrdbapigrpc.WithRegisterDEKService(newTenantKeyringDEKServicer()))
			served := make(chan error, 1)
			go func() { served <- srv.Serve(lis) }()
			t.Cleanup(func() {
				srv.Stop()
				if err := <-served; err != nil {
					t.Errorf("serve: %v", err)
				}
			})

			opts := defaultOptions()
			for _, apply := range append([]Option{WithAuthToken("t-dek")}, extra...) {
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

			keyring, err := c.qrdbTenantDEK()
			require.NoError(t, err)
			exerciseTenantDEK(t, keyring)
			recMu.Lock()
			got := append([]string(nil), auths...)
			recMu.Unlock()
			require.Len(t, got, 6, "one authorization value per keyring verb")
			for _, v := range got {
				assert.Equal(t, "Bearer t-dek", v)
			}
		})
	}
}

// TestTenantDEK_EmbeddedBindsSelectedTenant proves an embedded client acts
// on the tenant named by WithTenant, never the system tenant, and that
// another tenant's keyring stays unchanged.
func TestTenantDEK_EmbeddedBindsSelectedTenant(t *testing.T) {
	svc := newTenantKeyringDEKServicer("acme", "other", qrdbsdk.SystemTenantID)
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
		DEK:   svc,
	})
	require.NoError(t, WithTenant("acme")(c.opts))

	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	rotated, err := keyring.TenantDEKRotate(context.Background(), &QRDBTenantDEKRequest{})
	require.NoError(t, err)
	assert.Equal(t, "acme", rotated.TenantID)
	exerciseTenantDEK(t, keyring)

	for _, id := range svc.seen() {
		assert.Equal(t, "acme", id, "every embedded keyring call acts on the selected tenant")
	}
	assert.EqualValues(t, 3, svc.version("acme"), "two rotations on acme")
	assert.EqualValues(t, 1, svc.version("other"), "another tenant's keyring is unchanged")
	assert.EqualValues(t, 1, svc.version(qrdbsdk.SystemTenantID), "the system keyring is unchanged")
}

// TestTenantDEK_EmbeddedWithoutTenantKeepsCallerContext proves an embedded
// client with no WithTenant leaves the context alone, so a tenant the caller
// bound itself still applies.
func TestTenantDEK_EmbeddedWithoutTenantKeepsCallerContext(t *testing.T) {
	svc := newTenantKeyringDEKServicer("acme")
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
		DEK:   svc,
	})
	keyring, err := c.Security().TenantDEK()
	require.NoError(t, err)
	st, err := keyring.TenantDEKStatus(qrdbsdk.ContextWithTenant(context.Background(), "acme"), &QRDBTenantDEKRequest{})
	require.NoError(t, err)
	assert.Equal(t, "acme", st.TenantID)
}
