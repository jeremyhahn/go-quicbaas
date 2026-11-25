package mcp

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

var qrdbCredentialTools = []string{
	"qrdb_credential_submit",
	"qrdb_credential_strategy",
	"qrdb_credential_delete",
	"qrdb_credential_list",
	"qrdb_credential_rotate",
	"qrdb_credential_retrieve_version",
	"qrdb_credential_list_versions",
	"qrdb_credential_gc_expired",
	"qrdb_credential_store",
}

// TestQRDBCredentialClient_VerbsCallGoQRDBTools pins that every credential
// verb calls go-qrdb's qrdb_credential_* tool with go-qrdb's argument names,
// values base64-encoded.
func TestQRDBCredentialClient_VerbsCallGoQRDBTools(t *testing.T) {
	var (
		mu   sync.Mutex
		args = map[string]map[string]interface{}{}
	)
	tr, called, cleanup := newMultiToolMCPServer(t, qrdbCredentialTools,
		func(_ *testing.T, tool string, a map[string]interface{}) map[string]interface{} {
			mu.Lock()
			args[tool] = a
			mu.Unlock()
			if tool == "qrdb_credential_strategy" {
				return map[string]interface{}{"strategy": "manual"}
			}
			return map[string]interface{}{}
		})
	defer cleanup()
	c := tr.QRDBCredentials()
	ctx := context.Background()
	v := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff})

	require.NoError(t, c.CredentialSubmit(ctx, &qrdbtransport.CredentialSubmitRequest{Name: "n", Value: []byte{0x00, 0xff}}))
	strategy, err := c.CredentialStrategy(ctx)
	require.NoError(t, err)
	assert.Equal(t, "manual", strategy.Strategy)
	require.NoError(t, c.CredentialDelete(ctx, "n"))
	_, err = c.CredentialList(ctx, nil)
	require.NoError(t, err)
	_, err = c.CredentialRotate(ctx, &qrdbtransport.CredentialRotateRequest{Name: "n", NewValue: []byte{0x00, 0xff}, GracePeriodSeconds: 30})
	require.NoError(t, err)
	_, err = c.CredentialRetrieveVersion(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{Name: "n", Version: 3})
	require.NoError(t, err)
	_, err = c.CredentialListVersions(ctx, &qrdbtransport.CredentialListVersionsRequest{Name: "n"})
	require.NoError(t, err)
	_, err = c.CredentialGCExpired(ctx, &qrdbtransport.CredentialGCRequest{Name: "n"})
	require.NoError(t, err)
	require.NoError(t, c.CredentialStore(ctx, "n", []byte{0x00, 0xff}))

	assert.Equal(t, qrdbCredentialTools, *called)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]interface{}{"name": "n", "value": v}, args["qrdb_credential_submit"])
	assert.Empty(t, args["qrdb_credential_strategy"])
	assert.Equal(t, map[string]interface{}{"name": "n"}, args["qrdb_credential_delete"])
	assert.Empty(t, args["qrdb_credential_list"])
	assert.Equal(t, map[string]interface{}{"name": "n", "new_value": v, "grace_period_seconds": float64(30)}, args["qrdb_credential_rotate"])
	assert.Equal(t, map[string]interface{}{"name": "n", "version": float64(3)}, args["qrdb_credential_retrieve_version"])
	assert.Equal(t, map[string]interface{}{"name": "n"}, args["qrdb_credential_list_versions"])
	assert.Equal(t, map[string]interface{}{"name": "n"}, args["qrdb_credential_gc_expired"])
	assert.Equal(t, map[string]interface{}{"name": "n", "value": v}, args["qrdb_credential_store"])
}

// TestQRDBCredentialClient_RetrieveDecodesBase64 pins that CredentialRetrieve
// decodes the tool's base64 text back to the raw bytes.
func TestQRDBCredentialClient_RetrieveDecodesBase64(t *testing.T) {
	value := []byte{0x00, 0xff, 0x80, 0xfe}
	var gotName interface{}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_credential_retrieve"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		gotName = req.GetArguments()["name"]
		return mcpgo.NewToolResultText(base64.StdEncoding.EncodeToString(value)), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()
	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	defer func() { _ = tr.Close() }()

	got, err := tr.QRDBCredentials().CredentialRetrieve(context.Background(), "bin")
	require.NoError(t, err)
	assert.Equal(t, value, got)
	assert.Equal(t, "bin", gotName)
}

// TestQRDBCredentialClient_RefusesNilRequest pins that the request verbs
// refuse a nil request before any tool is called.
func TestQRDBCredentialClient_RefusesNilRequest(t *testing.T) {
	tr, called, cleanup := newMultiToolMCPServer(t, qrdbCredentialTools, nil)
	defer cleanup()
	c := tr.QRDBCredentials()
	ctx := context.Background()
	assert.Error(t, c.CredentialSubmit(ctx, nil))
	_, err := c.CredentialRotate(ctx, nil)
	assert.Error(t, err)
	_, err = c.CredentialRetrieveVersion(ctx, nil)
	assert.Error(t, err)
	_, err = c.CredentialListVersions(ctx, nil)
	assert.Error(t, err)
	_, err = c.CredentialGCExpired(ctx, nil)
	assert.Error(t, err)
	assert.Empty(t, *called)
}
