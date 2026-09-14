// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qbmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// barrierStatusJSON is the wire shape go-qrdb's BarrierStatusResponse
// decodes: the same three fields go-quicbaas#405 requires every transport to
// agree on, byte for byte.
const barrierStatusJSON = `{"sealed":false,"strategy":"passphrase","hardware_backed":false,` +
	`"initialized_at":"2026-08-25T04:40:05.727835519Z"}`

// newBarrierRESTStub starts a TLS test server answering the reachability
// probe and GET /v1/barrier/status, and returns it alongside a recorder of
// every path it was asked for.
func newBarrierRESTStub(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()

	var paths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/barrier/status", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(barrierStatusJSON)); err != nil {
			t.Errorf("write barrier status body: %v", err)
		}
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv, &paths
}

// stubTLSConfig trusts the test server's own certificate. No
// InsecureSkipVerify: the SDK's TLS posture is that verification always
// happens, and a test that skipped it would not exercise the same code path
// the CLI does.
func stubTLSConfig(t *testing.T, srv *httptest.Server) *tls.Config {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// TestBuildQRDBClient_RESTBareHostPort_ReachesBarrierStatus is the client-side
// half of go-quicbaas#405. The CLI passes `--address 127.0.0.1:18443`, a bare
// host:port with no scheme, and the quicbaas SDK handed it to go-qrdb's REST
// transport verbatim. That transport concatenates its path onto the address,
// so `barrier status --transport rest` died before a byte left the process:
//
//	transport: request BarrierStatus failed:
//	parse "127.0.0.1:18443/v1/barrier/status": first path segment in URL cannot contain colon
//
// This drives the exact CLI address spelling through the real qrdb REST
// transport against a live TLS listener and asserts the request arrived at
// /v1/barrier/status with the served state decoded.
func TestBuildQRDBClient_RESTBareHostPort_ReachesBarrierStatus(t *testing.T) {
	srv, paths := newBarrierRESTStub(t)

	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)

	// Exactly what the CLI supplies: host:port, no scheme, no path.
	address := strings.TrimPrefix(srv.URL, "https://")
	if strings.Contains(address, "://") {
		t.Fatalf("test setup: address must be a bare host:port, got %q", address)
	}
	c := &client{opts: opts, address: address}

	qrdbClient, err := c.buildQRDBClient()
	if err != nil {
		t.Fatalf("buildQRDBClient over REST with a bare host:port address: %v", err)
	}

	status, err := qrdbClient.Barrier().BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}

	if status.Sealed {
		t.Errorf("sealed: got true, want false")
	}
	if got, want := status.Strategy, "passphrase"; got != want {
		t.Errorf("strategy: got %q, want %q", got, want)
	}
	if got, want := status.InitializedAt, "2026-08-25T04:40:05.727835519Z"; got != want {
		t.Errorf("initialized_at: got %q, want %q", got, want)
	}

	var sawStatusPath bool
	for _, p := range *paths {
		if p == "/v1/barrier/status" {
			sawStatusPath = true
		}
	}
	if !sawStatusPath {
		t.Fatalf("barrier status never reached the server; paths seen: %v", *paths)
	}
}

// TestBuildQRDBClient_RESTExplicitURL_PreservesSchemeAndPath proves an
// address a caller already spelled in full is respected rather than
// re-prefixed: the scheme is kept and a mount path is preserved, so a node
// served behind a path prefix stays reachable.
func TestBuildQRDBClient_RESTExplicitURL_PreservesSchemeAndPath(t *testing.T) {
	srv, paths := newBarrierRESTStub(t)

	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)

	c := &client{opts: opts, address: srv.URL}

	qrdbClient, err := c.buildQRDBClient()
	if err != nil {
		t.Fatalf("buildQRDBClient over REST with an explicit URL: %v", err)
	}
	if _, err := qrdbClient.Barrier().BarrierStatus(context.Background()); err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if len(*paths) == 0 {
		t.Fatal("no request reached the server")
	}
}

// TestQRDBSubClientAddress covers the address normalization for every
// transport selection in one table: the HTTP-shaped transports get a
// scheme-qualified URL (MCP with its endpoint path), and the wire transports
// keep the bare host:port they dial.
//
// go-qrdb's transports expose no Address() accessor, so this asserts the
// builder's own decision — the value that reaches
// qrdbtransport.WithAddress — rather than reading it back off a constructed
// transport.
func TestQRDBSubClientAddress(t *testing.T) {
	tests := []struct {
		name          string
		transportType TransportType
		address       string
		tlsConfigured bool
		want          string
	}{
		{"rest bare host:port", TransportREST, "127.0.0.1:18443", true, "https://127.0.0.1:18443"},
		{"rest explicit https", TransportREST, "https://node.example.com:8443", true, "https://node.example.com:8443"},
		{"rest explicit http", TransportREST, "http://node.example.com:8080", false, "http://node.example.com:8080"},
		{"mcp bare host:port", TransportMCP, "127.0.0.1:18443", true, "https://127.0.0.1:18443" + qbmcp.MCPEndpointPath},
		{"mcp endpoint already full", TransportMCP, "https://node.example.com:8443/mcp", true, "https://node.example.com:8443/mcp"},
		{"grpc stays bare", TransportGRPC, "127.0.0.1:18443", true, "127.0.0.1:18443"},
		{"quic stays bare", TransportQUIC, "127.0.0.1:18443", true, "127.0.0.1:18443"},
		{"unix stays a socket path", TransportUnix, "/var/run/quicbaas/quicbaasd.sock", true, "/var/run/quicbaas/quicbaasd.sock"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := qrdbSubClientAddress(tc.transportType, tc.address, tc.tlsConfigured); got != tc.want {
				t.Errorf("qrdbSubClientAddress: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestQRDBSubClientAddress_RESTNeverDowngradesToHTTP proves a scheme-less
// address is promoted to https, never http: TLS is required on every
// quicbaas listener, and silently dialing plaintext would be a downgrade the
// caller never asked for.
func TestQRDBSubClientAddress_RESTNeverDowngradesToHTTP(t *testing.T) {
	for _, tlsConfigured := range []bool{true, false} {
		got := qrdbSubClientAddress(TransportREST, "node.example.com:8443", tlsConfigured)
		if !strings.HasPrefix(got, "https://") {
			t.Errorf("tlsConfigured=%v: got %q, want an https:// origin", tlsConfigured, got)
		}
	}
}
