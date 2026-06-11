// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest && mcp

package login

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// MCP transport login coverage.
//
// MCP is JSON-RPC 2.0 over HTTPS. The cluster's MCP listener terminates TLS
// with the same chain as REST/gRPC/QUIC and threads the VERIFIED peer
// certificate onto the request context (pkg/api/mcp/server.go:
// r.TLS.VerifiedChains[0][0] -> service.WithVerifiedPeerCert). The unified
// login ceremony tools quicbaas_login_begin / quicbaas_login_finish are public
// (no bearer required) and delegate to the SAME LoginCeremony() facade every
// other transport converges on. Consequently MCP mTLS / PIV login behaves
// exactly like REST: the proof for mtls is the handshake, and for piv it is the
// ECDSA signature over the server nonce.
//
// The headline SDK-driven mTLS and PIV logins for MCP are exercised by the
// shared table-driven matrix (mtls_login_test.go / piv_login_test.go via
// allLoginTransports, which now includes transportMCP). This file adds the
// MCP-shaped SECURITY assertions analogous to the REST direct-wire cases:
//
//   - no client cert on the handshake -> finish FAILS CLOSED (JSON-RPC error,
//     no bearer);
//   - a spoofed lookup_key in the finish body is IGNORED -> the peer cert's
//     user (admin) is resolved (peer-cert-wins).
//
// These use a direct JSON-RPC dialer (mcpCeremonyClient) so the test controls
// the exact TLS handshake (cert present / absent) and the exact tool arguments,
// mirroring restCeremonyClient. The dialer reuses the established integration
// MCP JSON-RPC pattern (initialize handshake + tools/call) from
// test/integration/auth/multiprotocol_test.go.
// =============================================================================

// MCPCeremonyError is returned when a direct MCP JSON-RPC ceremony call cannot
// be performed at the transport level (connection, encode, decode). A login
// REJECTION surfaces in the decoded response (a non-nil JSON-RPC error or a
// tool isError), NOT as this error — callers assert on the response shape.
type MCPCeremonyError struct {
	Op    string
	Cause error
}

func (e *MCPCeremonyError) Error() string {
	return fmt.Sprintf("login-test: mcp-ceremony %s: %v", e.Op, e.Cause)
}

func (e *MCPCeremonyError) Unwrap() error { return e.Cause }

// mcpJSONRPCResult mirrors the relevant subset of a JSON-RPC tools/call
// response. A tool execution result is wrapped in result.content[0].text as a
// JSON string (pkg/api/mcp/server.go wrapToolResult); a tool/dispatch failure
// surfaces as a top-level error object (the login facade's
// MTLSPeerCertRequiredError maps through mapErrorToJSONRPCError to a non-nil
// error). isError covers the alternative tool-level error envelope.
type mcpJSONRPCResult struct {
	Result *struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rejected reports whether the response represents a login REJECTION: either a
// top-level JSON-RPC error or a tool-level isError envelope. A nil result with
// no error is also treated as a rejection (no bearer was produced).
func (r *mcpJSONRPCResult) rejected() bool {
	if r.Error != nil {
		return true
	}
	if r.Result == nil {
		return true
	}
	return r.Result.IsError
}

// toolText returns the decoded tool result JSON text (the BeginLogin /
// FinishLogin response object), or "" when the response carried no content.
func (r *mcpJSONRPCResult) toolText() string {
	if r.Result == nil || len(r.Result.Content) == 0 {
		return ""
	}
	return r.Result.Content[0].Text
}

// mcpCeremonyClient drives the quicbaas_login_begin / quicbaas_login_finish MCP
// tools directly over a caller-supplied tls.Config so the test controls whether
// a client cert is presented and exactly what the finish arguments are. This is
// the MCP analogue of restCeremonyClient.
type mcpCeremonyClient struct {
	endpoint  string
	http      *http.Client
	requestID atomic.Uint64
}

// newMCPCeremonyClient builds a direct MCP ceremony client with the given TLS
// config (client-cert-bearing or not) and performs the MCP initialize
// handshake so subsequent tools/call requests are accepted.
func newMCPCeremonyClient(t *testing.T, ctx context.Context, tlsCfg *tls.Config) *mcpCeremonyClient {
	t.Helper()
	endpoints := helpers.GetMCPEndpoints()
	if len(endpoints) == 0 {
		t.Fatalf("login-test: no MCP endpoints")
	}
	c := &mcpCeremonyClient{
		endpoint: endpoints[0],
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     tlsCfg,
				TLSHandshakeTimeout: 10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
	if err := c.initialize(ctx); err != nil {
		t.Fatalf("%v", &MCPCeremonyError{Op: "initialize", Cause: err})
	}
	return c
}

// initialize performs the MCP protocol handshake (initialize +
// notifications/initialized), matching the integration JSON-RPC pattern.
func (c *mcpCeremonyClient) initialize(ctx context.Context) error {
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-login-test",
				"version": "1.0.0",
			},
		},
		"id": c.requestID.Add(1),
	}
	if _, err := c.post(ctx, initReq); err != nil {
		return err
	}

	notify := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	if _, err := c.post(ctx, notify); err != nil {
		return err
	}
	return nil
}

// callTool issues a tools/call for the named login tool with the supplied
// arguments and returns the decoded JSON-RPC result. A login rejection is
// reported via the result (rejected()), not via the returned error; the error
// is reserved for genuine transport/encode/decode failures.
func (c *mcpCeremonyClient) callTool(ctx context.Context, name string, args map[string]interface{}) (*mcpJSONRPCResult, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
		"id": c.requestID.Add(1),
	}
	body, err := c.post(ctx, req)
	if err != nil {
		return nil, err
	}
	var out mcpJSONRPCResult
	if jerr := json.Unmarshal(body, &out); jerr != nil {
		return nil, &MCPCeremonyError{Op: "decode tools/call", Cause: jerr}
	}
	return &out, nil
}

// post performs a single JSON-RPC POST and returns the raw response body.
func (c *mcpCeremonyClient) post(ctx context.Context, payload interface{}) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &MCPCeremonyError{Op: "marshal request", Cause: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &MCPCeremonyError{Op: "build request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &MCPCeremonyError{Op: "do request", Cause: err}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &MCPCeremonyError{Op: "read response", Cause: err}
	}
	return respBody, nil
}

// beginLogin drives quicbaas_login_begin and returns the decoded
// BeginLoginResponse. A transport/decode failure or a ceremony rejection is a
// hard test failure here (begin must succeed for the security assertions).
func (c *mcpCeremonyClient) beginLogin(t *testing.T, ctx context.Context, factorType, hint string) *qrdbtransport.BeginLoginResponse {
	t.Helper()
	res, err := c.callTool(ctx, "quicbaas_login_begin", map[string]interface{}{
		"type":      factorType,
		"hint":      hint,
		"tenant_id": systemTenantID,
	})
	require.NoError(t, err, "MCP begin transport call must complete")
	require.False(t, res.rejected(), "MCP begin must succeed (challenge issuance only); got %+v", res)
	text := res.toolText()
	require.NotEmpty(t, text, "MCP begin must return a tool result body")
	var out qrdbtransport.BeginLoginResponse
	require.NoError(t, json.Unmarshal([]byte(text), &out), "decode MCP begin response")
	return &out
}

// =============================================================================
// MCP security assertions
// =============================================================================

// TestUnifiedLogin_MCP_NoClientCertFailsClosed is the MCP analogue of
// TestUnifiedLogin_MTLS_NoClientCertFailsClosed: an mtls quicbaas_login_finish
// driven over MCP with NO client certificate on the handshake must FAIL CLOSED.
// The begin step issues a challenge without needing a cert; finish must reject
// because the MCP server has no verified peer cert to thread onto the context,
// so the facade returns MTLSPeerCertRequiredError. It must NOT fall through to
// trusting the client-supplied lookup_key.
func TestUnifiedLogin_MCP_NoClientCertFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Drive the MCP ceremony with a TLS config that presents NO client cert.
	wire := newMCPCeremonyClient(t, ctx, noCertTLSConfig(t))

	begin := wire.beginLogin(t, ctx, "mtls", adminSubject)
	require.NotEmpty(t, begin.SessionID, "MCP begin must return a session id")

	// Finish with the CORRECT admin lookup key but NO presented client cert.
	// The MCP server threads no verified peer cert, so the facade must fail
	// closed with MTLSPeerCertRequiredError (surfaced as a JSON-RPC error).
	finish, err := wire.callTool(ctx, "quicbaas_login_finish", map[string]interface{}{
		"session_id": begin.SessionID,
		"type":       "mtls",
		"lookup_key": base64.StdEncoding.EncodeToString(certSHA256(adminLeaf)),
	})
	require.NoError(t, err, "MCP finish transport call should complete")
	require.True(t, finish.rejected(),
		"mTLS finish over MCP with NO client cert MUST fail closed; got %+v", finish)

	// Defense in depth: even if a result body were present, it must carry no bearer.
	if text := finish.toolText(); text != "" {
		var fr qrdbtransport.FinishLoginResponse
		require.NoError(t, json.Unmarshal([]byte(text), &fr), "decode MCP finish body")
		require.Empty(t, fr.Bearer, "no bearer may be issued when the client cert is absent")
	}
}

// TestUnifiedLogin_MCP_SpoofedLookupKeyResolvesToPeerCert is the MCP analogue
// of TestUnifiedLogin_MTLS_SpoofedLookupKeyResolvesToPeerCert (peer-cert-wins):
// an mtls quicbaas_login_finish that PRESENTS the admin client cert but SENDS a
// bogus lookup_key in the body must still succeed AND resolve to the admin (the
// peer cert's user) — the spoofed value is overwritten with SHA-256(peer cert).
func TestUnifiedLogin_MCP_SpoofedLookupKeyResolvesToPeerCert(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Ensure the admin mtls factor is enrolled (peer cert -> admin).
	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err)
	defer enrollClient.Close()
	if eerr := enrollMTLSFactor(ctx, enrollClient, adminSubject, adminLeaf); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr), "enroll admin mtls factor: %v", eerr)
	}

	// Establish the legitimate resolved subject over MCP (SDK path) for parity.
	legitClient, lerr := newLoginClient(t, transportMCP, mtlsTLSConfig(t))
	require.NoError(t, lerr)
	defer legitClient.Close()
	legit, lerr := runSDKLogin(ctx, legitClient, qrdbtransport.FactorTypeMTLS,
		adminSubject, certSHA256(adminLeaf), nil, nil)
	require.NoError(t, lerr, "baseline MCP mTLS login must succeed")
	require.NotEmpty(t, legit.subject)

	// Drive the MCP ceremony PRESENTING the admin cert but SENDING a spoofed
	// lookup key in the finish body.
	wire := newMCPCeremonyClient(t, ctx, mtlsTLSConfig(t))
	begin := wire.beginLogin(t, ctx, "mtls", adminSubject)

	spoofed := make([]byte, 32)
	for i := range spoofed {
		spoofed[i] = 0xAB // deliberately not the admin cert hash
	}
	require.NotEqual(t, certSHA256(adminLeaf), spoofed)

	finish, err := wire.callTool(ctx, "quicbaas_login_finish", map[string]interface{}{
		"session_id": begin.SessionID,
		"type":       "mtls",
		"lookup_key": base64.StdEncoding.EncodeToString(spoofed), // attacker-chosen victim key
	})
	require.NoError(t, err, "MCP finish transport call should complete")
	require.False(t, finish.rejected(),
		"mTLS finish over MCP with a presented peer cert must succeed (peer cert wins); got %+v", finish)

	text := finish.toolText()
	require.NotEmpty(t, text, "MCP finish must return a tool result body")
	var fr qrdbtransport.FinishLoginResponse
	require.NoError(t, json.Unmarshal([]byte(text), &fr), "decode MCP finish body")
	require.NotEmpty(t, fr.Bearer, "peer-cert-resolved login must mint a bearer")
	require.NotNil(t, fr.Identity, "MCP finish must carry a resolved identity")

	// The spoofed key must have been ignored: the resolved subject is the PEER
	// CERT's user (admin), identical to the legitimate MCP login above.
	require.Equal(t, legit.subject, fr.Identity.Subject,
		"spoofed lookup key must be ignored; identity must resolve to the PEER CERT's user")
	require.Equal(t, systemTenantID, fr.Identity.TenantID,
		"resolved tenant must be the peer cert's tenant")
	require.NoError(t, validateBearerOverREST(ctx, fr.Bearer),
		"the peer-cert-resolved MCP bearer must validate against /oauth2/userinfo")
}

// mcpFactorTypeString is a defensive sanity check that the qrdb mtls factor
// type string the SDK matrix uses matches the wire string the MCP login tools
// expect ("mtls"); a drift here would silently make the MCP security
// assertions test the wrong factor.
func mcpFactorTypeString() string {
	return strings.ToLower(string(qrdbtransport.FactorTypeMTLS))
}

func TestUnifiedLogin_MCP_FactorTypeWireString(t *testing.T) {
	require.Equal(t, "mtls", mcpFactorTypeString(),
		"mtls wire string must match the MCP login tool factor type")
}
