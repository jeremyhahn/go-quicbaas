package quicbaas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// TestBarrierStatus_MCP_SendsPlatformBearer pins #2556: over MCP the barrier
// verbs must carry the platform bearer the client was built with, so the
// node's /mcp gate does not answer the session's initialize with 401.
func TestBarrierStatus_MCP_SendsPlatformBearer(t *testing.T) {
	const want = "Bearer t-mcp"
	var (
		mu    sync.Mutex
		auths []string
	)
	server := mcpserver.NewMCPServer("barrier-bearer", "1.0.0")
	server.AddTool(mcpgo.NewTool("qrdb_barrier_status"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText(`{"sealed":false,"strategy":"passphrase"}`), nil
	})
	mcpHandler := mcpserver.NewStreamableHTTPServer(server)
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

	c, err := NewClient(srv.URL+"/mcp", WithMCP(), WithMCPAuthToken("t-mcp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	status, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if got, wantStrategy := status.Strategy, "passphrase"; got != wantStrategy {
		t.Errorf("strategy: got %q, want %q", got, wantStrategy)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auths) == 0 {
		t.Fatal("no request reached the MCP server")
	}
	for i, got := range auths {
		if got != want {
			t.Errorf("request %d Authorization: got %q, want %q", i, got, want)
		}
	}
}

// TestBarrierStatus_MCP_EmptyResultIsError: a barrier status tool result with
// no content is an error, not an unsealed barrier.
func TestBarrierStatus_MCP_EmptyResultIsError(t *testing.T) {
	const want = "Bearer t-mcp"
	server := mcpserver.NewMCPServer("barrier-empty", "1.0.0")
	server.AddTool(mcpgo.NewTool("qrdb_barrier_status"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return &mcpgo.CallToolResult{}, nil
	})
	mcpHandler := mcpserver.NewStreamableHTTPServer(server)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL+"/mcp", WithMCP(), WithMCPAuthToken("t-mcp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	status, err := barrier.BarrierStatus(context.Background())
	if err == nil {
		t.Fatalf("BarrierStatus with no result content = %+v, nil; want an error", status)
	}
	if status != nil {
		t.Errorf("BarrierStatus status = %+v, want nil on error", status)
	}
}

// TestBarrierStatus_MCP_AuthTokenFallbackSendsPlatformBearer: a client built
// WithAuthToken alone (no WithMCPAuthToken), pointed at the node's root URL,
// still sends that bearer on every MCP request from initialize on, so
// `barrier status` over MCP is not refused with 401 (TestBarrierStatus/mcp).
func TestBarrierStatus_MCP_AuthTokenFallbackSendsPlatformBearer(t *testing.T) {
	var unauthenticated atomic.Int32
	server := mcpserver.NewMCPServer("barrier-authtoken", "1.0.0")
	server.AddTool(mcpgo.NewTool("qrdb_barrier_status"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText(`{"sealed":true,"strategy":"shamir"}`), nil
	})
	mcpHandler := mcpserver.NewStreamableHTTPServer(server)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1700" {
			unauthenticated.Add(1)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithMCP(), WithAuthToken("t-1700"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	status, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if !status.Sealed {
		t.Errorf("sealed: got false, want true")
	}
	if n := unauthenticated.Load(); n != 0 {
		t.Errorf("%d request(s) reached the MCP endpoint without the bearer", n)
	}
}
