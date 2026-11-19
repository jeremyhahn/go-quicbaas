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

//go:build integration

// sdk_verify.go layers an SDK-based verification on top of the per-cell
// readiness probes. The matrix uses these helpers to assert the SDK can
// actually reach the daemon over the selected transport — a stronger
// claim than the raw-socket probes in helpers.go because it exercises
// the production transport plumbing (TLS trust, request serialisation,
// response demarshalling).
//
// The verification target is Health.Liveness because:
//
//   - it is wired across every transport (rest / grpc / quic / mcp /
//     unix) per sdk/go/transport/<t>;
//   - it requires zero authentication so we do not pay the cost of
//     bootstrapping admin credentials per-cell;
//   - a 200 / Healthy response proves the SDK's transport handshake +
//     request envelope + response decoder all line up with the daemon's
//     listener for this cell.
package init

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// sdkLivenessTimeout bounds a single SDK Health.Liveness call. The
// daemon's health handler is in-memory; anything > a few seconds means
// the transport is malformed or the daemon is stuck behind a startup
// barrier we missed in waitForTransportReady.
const sdkLivenessTimeout = 10 * time.Second

// verifySDKLivenessOverTransport constructs an SDK client targeting the
// requested transport against the daemon at the supplied ports, calls
// Health.Liveness, and asserts the response is healthy. Failure here
// means the SDK's transport stack does not line up with the daemon's
// listener for this cell — a regression we want to catch loudly because
// it would silently break every downstream SDK caller.
//
// The function takes the data dir so it can wire up the auto-generated
// CA chain into the SDK's TLS trust store. Without that, every TLS
// transport (rest / quic / mcp / grpc) fails handshake against the
// self-signed daemon cert.
func verifySDKLivenessOverTransport(t *testing.T, transport string, ports PortBundle, dataDir, adminToken string) {
	t.Helper()

	if transport == "mcp" {
		// MCP has no anonymous session: an initialize without a bearer is
		// refused with 401 before the authenticated liveness call below.
		tlsCfg, err := loadDaemonCATLSConfig(dataDir)
		if err != nil {
			t.Fatalf("load daemon CA for transport \"mcp\": %v", err)
		}
		probeCtx, probeCancel := context.WithTimeout(context.Background(), sdkLivenessTimeout)
		probeErr := probeSealedMCPInitialize(probeCtx, fmt.Sprintf("https://127.0.0.1:%d/mcp", ports.MCP), tlsCfg)
		probeCancel()
		if probeErr != nil {
			t.Fatalf("anonymous MCP initialize: %v", probeErr)
		}
	}

	client, err := newSDKClient(transport, ports, dataDir, adminToken)
	if err != nil {
		t.Fatalf("construct SDK client for transport %q: %v", transport, err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Logf("SDK client Close: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), sdkLivenessTimeout)
	defer cancel()

	// We dispatch via Transport().Request directly because the SDK's
	// per-transport Liveness decoding is inconsistent: gRPC + Unix +
	// embedded decode into *HealthResponse, but REST + MCP + QUIC return
	// the raw JSON map. The matrix only asserts "the transport accepted
	// the request and the daemon's handler returned without error" — a
	// stronger claim than the raw-socket probes in helpers.go because it
	// flows through the SDK transport plumbing end-to-end.
	resp, err := client.Transport().Request(ctx, "quicbaas.Health", "Liveness", struct{}{})
	if err != nil {
		t.Fatalf("Health.Liveness over %q: %v", transport, err)
	}
	if resp == nil {
		t.Fatalf("Health.Liveness over %q: nil response", transport)
	}
	assertLivenessResponseHealthy(t, transport, resp)
}

// verifySealedMCPWindow is the SDK phase of a shamir MCP cell. A shamir init
// only records the M-of-N parameters, so the daemon boots SEALED with no
// administrator: first boot issues none until the officers' ceremony unseals
// it (cmd/quicbaasd init_runtime_test.go). There is no credential to log in
// with, and MCP refuses an uncredentialed session (CA ruling 571), so the
// cell proves the sealed window instead: no administrator certificate exists,
// the MCP listener completed its TLS handshake in the readiness probe, and an
// SDK client without a credential is refused rather than served.
func verifySealedMCPWindow(t *testing.T, ports PortBundle, dataDir string) {
	t.Helper()

	adminCert := filepath.Join(dataDir, "admin", "admin.crt")
	if _, err := os.Stat(adminCert); err == nil {
		t.Fatalf("sealed shamir node issued an administrator certificate (%s) before its ceremony", adminCert)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat %s: %v", adminCert, err)
	}

	if err := checkSealedMCPWindow(ports, dataDir); err != nil {
		t.Fatalf("sealed shamir node: %v", err)
	}
	t.Logf("uncredentialed MCP initialize refused with 401 on the sealed node")
}

// checkSealedMCPWindow probes the sealed node's MCP endpoint. It first
// sends the MCP initialize request by itself, with no session and no
// credentials, and requires HTTP 401 for it: a node that answers that
// initialize and refuses only a later notifications/initialized or
// tools/call has served an uncredentialed session, and fails here. Only
// then does it run the SDK's whole Health.Liveness exchange, which must
// also be refused with 401.
func checkSealedMCPWindow(ports PortBundle, dataDir string) error {
	tlsCfg, err := loadDaemonCATLSConfig(dataDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sdkLivenessTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("https://127.0.0.1:%d/mcp", ports.MCP)
	if err := probeSealedMCPInitialize(ctx, endpoint, tlsCfg); err != nil {
		return err
	}

	client, err := newSDKClient("mcp", ports, dataDir, "")
	if err != nil {
		return fmt.Errorf("construct uncredentialed MCP client for the sealed node: %w", err)
	}
	defer func() { _ = client.Close() }()
	_, err = client.Transport().Request(ctx, "quicbaas.Health", "Liveness", struct{}{})
	return classifySealedMCPRefusal(err)
}

// sealedMCPInitializeProbe is a bare MCP initialize request: no session id
// header and no Authorization header go with it.
const sealedMCPInitializeProbe = `{"jsonrpc":"2.0","id":1,"method":"initialize",` +
	`"params":{"protocolVersion":"2025-03-26","capabilities":{},` +
	`"clientInfo":{"name":"sealed-window-probe","version":"0.0.0"}}}`

// probeSealedMCPInitialize posts the bare initialize request to endpoint
// and returns nil only when it is refused with HTTP 401.
func probeSealedMCPInitialize(ctx context.Context, endpoint string, tlsCfg *tls.Config) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sealedMCPInitializeProbe))
	if err != nil {
		return fmt.Errorf("build the uncredentialed MCP initialize probe: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	defer httpClient.CloseIdleConnections()
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("the uncredentialed MCP initialize failed without an HTTP 401 refusal: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("the uncredentialed MCP initialize, sent alone, was answered with HTTP %d, want 401", resp.StatusCode)
	}
	return nil
}

// mcpHTTPStatusPattern matches the status mcp-go's Streamable HTTP client
// puts in the error of a non-2xx reply it has no sentinel for ("request
// failed with status 500: ..."); 401 and an initialize 4xx have sentinels.
var mcpHTTPStatusPattern = regexp.MustCompile(`request failed with status (\d{3})\b`)

// classifySealedMCPRefusal decides whether err, the result of an
// uncredentialed MCP call against a sealed node, is the authentication
// refusal the sealed window must produce: HTTP 401 on initialize. It returns
// nil for a 401 and an error naming what was seen otherwise: a served call, a
// timeout, or any other status or failure (404, 5xx, unknown tool, ...).
func classifySealedMCPRefusal(err error) error {
	if err == nil {
		return errors.New("an uncredentialed MCP session was served; MCP must refuse it with 401")
	}
	var oauthRequired *mcptransport.OAuthAuthorizationRequiredError
	if errors.Is(err, mcptransport.ErrUnauthorized) || errors.As(err, &oauthRequired) ||
		errors.Is(err, mcptransport.ErrOAuthAuthorizationRequired) {
		return nil
	}
	if errors.Is(err, mcptransport.ErrLegacySSEServer) || errors.Is(err, mcptransport.ErrSessionTerminated) {
		return fmt.Errorf("the uncredentialed MCP initialize was answered with a non-401 HTTP 4xx (404 or similar), want 401: %w", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the uncredentialed MCP call timed out instead of being refused with 401: %w", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("the uncredentialed MCP call timed out instead of being refused with 401: %w", err)
	}
	m := mcpHTTPStatusPattern.FindStringSubmatch(err.Error())
	if m == nil {
		return fmt.Errorf("the uncredentialed MCP call failed without an HTTP 401 refusal (%T): %w", err, err)
	}
	status, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return fmt.Errorf("the uncredentialed MCP call failed with an unreadable HTTP status %q: %w", m[1], err)
	}
	if status != http.StatusUnauthorized {
		return fmt.Errorf("the uncredentialed MCP call was answered with HTTP %d, want 401: %w", status, err)
	}
	return nil
}

// assertLivenessResponseHealthy normalises the Health.Liveness response
// across the SDK's transport-specific decoders. Each transport returns a
// slightly different shape:
//
//   - gRPC / Unix / embedded -> *health.HealthResponse with Healthy
//     bool + Status string.
//   - REST / MCP             -> map[string]interface{} carrying a
//     "status" field (e.g. {"status":"healthy"}).
//   - QUIC                   -> protobuf message decoded into the
//     transport's internal HealthResponse type (interface{}).
//
// The matrix only needs to know the daemon returned a healthy
// liveness; the precise decode is the SDK's concern.
func assertLivenessResponseHealthy(t *testing.T, transport string, resp interface{}) {
	t.Helper()
	switch v := resp.(type) {
	case map[string]interface{}:
		// The daemon's health envelope varies by transport: REST
		// returns {"status":"healthy", ...}; MCP / QUIC return
		// {"Alive": true} (the protobuf-encoded LivenessResponse
		// pass-through). We accept either as long as one of the two
		// healthy markers is present.
		if isMapHealthyByStatus(v) {
			return
		}
		if isMapHealthyByAlive(v) {
			return
		}
		t.Fatalf("Health.Liveness over %q: response map carries no healthy marker: %+v",
			transport, v)
	default:
		// Reflective probe: the *HealthResponse path. We avoid
		// importing the SDK's internal health.HealthResponse type
		// here so this file stays decoupled from the SDK's per-
		// transport response zoo.
		if !isReflectivelyHealthy(v) {
			t.Fatalf("Health.Liveness over %q: response shape unrecognised or unhealthy: %#v",
				transport, v)
		}
	}
}

// isMapHealthyByStatus accepts the REST shape: map with a "status"
// string equal to "ok" or "healthy".
func isMapHealthyByStatus(m map[string]interface{}) bool {
	statusAny, ok := m["status"]
	if !ok {
		return false
	}
	status, ok := statusAny.(string)
	if !ok {
		return false
	}
	return status == "ok" || status == "healthy"
}

// isMapHealthyByAlive accepts the QUIC / MCP shape: map with an "Alive"
// or "alive" bool set true.
func isMapHealthyByAlive(m map[string]interface{}) bool {
	for _, key := range []string{"Alive", "alive", "Healthy", "healthy"} {
		if v, ok := m[key].(bool); ok && v {
			return true
		}
	}
	return false
}

// newSDKClient constructs a quicbaas.Client targeting the requested
// transport. The TLS trust pool is loaded from the daemon's
// auto-generated CA chain so the SDK accepts the self-signed server
// cert; CLAUDE.md bans WithInsecure so explicit CA trust is the only
// supported path.
//
// adminToken is the first-boot administrator's session token. The MCP client
// presents it on initialize, which go-qrdb's MCP server refuses without a
// credential (CA ruling 571); the other transports serve Health.Liveness
// publicly and are dialled without it.
func newSDKClient(transport string, ports PortBundle, dataDir, adminToken string) (quicbaas.Client, error) {
	tlsCfg, err := loadDaemonCATLSConfig(dataDir)
	if err != nil {
		return nil, err
	}
	switch transport {
	case "rest":
		address := fmt.Sprintf("https://127.0.0.1:%d/api/v1", ports.REST)
		return quicbaas.NewClient(address,
			quicbaas.WithREST(),
			quicbaas.WithTLSConfig(tlsCfg),
		)
	case "grpc":
		address := fmt.Sprintf("127.0.0.1:%d", ports.GRPC)
		return quicbaas.NewClient(address,
			quicbaas.WithTLSConfig(tlsCfg),
		)
	case "quic":
		// The SDK's QUIC transport is the REST dialect over HTTP/3 and
		// the daemon serves HTTP/3 on the REST port (ports.QUIC is that
		// port on this row), so the address carries the same /api/v1
		// base path as the REST case: Health.Liveness maps to
		// <base>/health/live. The transport sets the "h3" ALPN itself.
		address := fmt.Sprintf("https://127.0.0.1:%d/api/v1", ports.QUIC)
		return quicbaas.NewClient(address,
			quicbaas.WithQUIC(),
			quicbaas.WithTLSConfig(tlsCfg),
		)
	case "mcp":
		address := fmt.Sprintf("https://127.0.0.1:%d/mcp", ports.MCP)
		return quicbaas.NewClient(address,
			quicbaas.WithMCP(),
			quicbaas.WithMCPAuthToken(adminToken),
			quicbaas.WithTLSConfig(tlsCfg),
		)
	case "unix":
		return quicbaas.NewClient("unix",
			quicbaas.WithUnix(ports.Unix),
			quicbaas.WithTLSConfig(tlsCfg),
		)
	}
	return nil, fmt.Errorf("newSDKClient: unknown transport %q", transport)
}

// loadDaemonCATLSConfig reads the CA chain quicbaasd writes after start
// and returns a *tls.Config that trusts it. ServerName is unset so the
// chain validation uses the cert's SANs (127.0.0.1 / localhost).
func loadDaemonCATLSConfig(dataDir string) (*tls.Config, error) {
	caPath := filepath.Join(dataDir, "certs", "ca-chain.crt")
	// #nosec G304 - caPath rooted at t.TempDir()
	pem, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("loadDaemonCATLSConfig: read %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("loadDaemonCATLSConfig: %s: no certificates parsed", caPath)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: "127.0.0.1",
	}, nil
}

// isReflectivelyHealthy inspects a non-map Liveness response via
// reflection to honour the gRPC / Unix / embedded decode path where the
// transport returns the SDK's internal *health.HealthResponse type. We
// avoid an explicit import of that type so the package stays free of
// the SDK's internal layout — the contract is purely structural.
//
// Accepted shapes:
//
//   - struct (or pointer-to-struct) with a "Healthy" bool field set true
//   - struct (or pointer-to-struct) with a "Status" string field equal
//     to "ok" or "healthy"
//
// Returns false for anything else so the caller surfaces an explicit
// "shape unrecognised" fatal.
func isReflectivelyHealthy(v interface{}) bool {
	if v == nil {
		return false
	}
	rv := reflectValue(v)
	if !rv.IsValid() {
		return false
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	if f := rv.FieldByName("Healthy"); f.IsValid() && f.Kind() == reflect.Bool && f.Bool() {
		return true
	}
	if f := rv.FieldByName("Status"); f.IsValid() && f.Kind() == reflect.String {
		switch f.String() {
		case "ok", "healthy":
			return true
		}
	}
	return false
}

// reflectValue is a tiny indirection so the reflect import is
// localised. Returns a zero Value when v is nil.
func reflectValue(v interface{}) reflect.Value {
	if v == nil {
		return reflect.Value{}
	}
	return reflect.ValueOf(v)
}
