package quicbaas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Over MCP, Security().Barrier() must carry the bearer set by WithAuthToken
// from initialize on: a node's MCP endpoint answers an uncredentialed
// initialize with 401, which is how `barrier status` failed over MCP while
// gRPC and REST passed (TestBarrierStatus/mcp, #1700).
func TestBarrierStatus_MCP_SendsPlatformBearer(t *testing.T) {
	var unauthenticated atomic.Int32
	srv := mcpserver.NewMCPServer("barrier", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_barrier_status"),
		func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText(`{"sealed":true,"strategy":"shamir"}`), nil
		})
	h := mcpserver.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1700" {
			unauthenticated.Add(1)
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	c, err := NewClient(ts.URL, WithMCP(), WithAuthToken("t-1700"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	require.NoError(t, err)
	status, err := barrier.BarrierStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Sealed)
	assert.Zero(t, unauthenticated.Load(), "a request reached the MCP endpoint without the bearer")
}
