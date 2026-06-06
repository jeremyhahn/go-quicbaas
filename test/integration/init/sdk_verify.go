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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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
func verifySDKLivenessOverTransport(t *testing.T, transport string, ports PortBundle, dataDir string) {
	t.Helper()

	client, err := newSDKClient(transport, ports, dataDir)
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
func newSDKClient(transport string, ports PortBundle, dataDir string) (quicbaas.Client, error) {
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
		address := fmt.Sprintf("127.0.0.1:%d", ports.QUIC)
		// QUIC requires NextProtos that the server advertises. The
		// daemon uses the standard "h3" + "quicbaas" ALPNs; we let the
		// transport layer set its own preference list.
		return quicbaas.NewClient(address,
			quicbaas.WithQUIC(),
			quicbaas.WithTLSConfig(tlsCfg),
		)
	case "mcp":
		address := fmt.Sprintf("https://127.0.0.1:%d/mcp", ports.MCP)
		return quicbaas.NewClient(address,
			quicbaas.WithMCP(),
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
	if rv.Kind() == reflect.Ptr {
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
