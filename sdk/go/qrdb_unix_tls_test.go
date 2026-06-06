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
	"crypto/tls"
	"testing"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	qrdbunix "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"
)

// newUnixClient builds a quicbaas SDK client configured for the Unix
// transport with the given socket path. TLS is left unset; callers enable it
// per test.
func newUnixClient(socketPath string) *client {
	opts := defaultOptions()
	opts.transportType = TransportUnix
	opts.unixSocketPath = socketPath
	return &client{opts: opts, address: socketPath}
}

// TestBuildQRDBTransport_UnixWithTLS_UsesGRPCTransport verifies that when a
// TLS config is present, the qrdb Unix sub-client is built over go-qrdb's gRPC
// transport (which dials with TLS credentials) rather than go-qrdb's native
// insecure unix transport. This is the fix for the
// "error reading server preface: EOF" mismatch against quicbaasd's TLS Unix
// listener.
func TestBuildQRDBTransport_UnixWithTLS_UsesGRPCTransport(t *testing.T) {
	c := newUnixClient("/var/run/quicbaas/quicbaas.sock")
	c.opts.tlsEnabled = true
	c.opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}

	tr, err := c.buildQRDBTransport()
	if err != nil {
		t.Fatalf("buildQRDBTransport: %v", err)
	}
	if _, ok := tr.(*qrdbgrpc.Transport); !ok {
		t.Fatalf("expected *qrdbgrpc.Transport for unix+TLS, got %T", tr)
	}
}

// TestBuildQRDBTransport_UnixWithoutTLS_UsesUnixTransport verifies that
// without a TLS config the qrdb Unix sub-client falls back to go-qrdb's native
// insecure unix transport, preserving standalone-qrdb behavior.
func TestBuildQRDBTransport_UnixWithoutTLS_UsesUnixTransport(t *testing.T) {
	c := newUnixClient("/var/run/quicbaas/quicbaas.sock")
	c.opts.tlsEnabled = false
	c.opts.tlsConfig = nil

	tr, err := c.buildQRDBTransport()
	if err != nil {
		t.Fatalf("buildQRDBTransport: %v", err)
	}
	if _, ok := tr.(*qrdbunix.Transport); !ok {
		t.Fatalf("expected *qrdbunix.Transport for unix without TLS, got %T", tr)
	}
}

// TestBuildQRDBTransport_UnixWithTLS_FallsBackToAddress verifies the socket
// path resolves from c.address when unixSocketPath is unset, matching the
// no-network address fallback used elsewhere in the builder.
func TestBuildQRDBTransport_UnixWithTLS_FallsBackToAddress(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportUnix
	opts.tlsEnabled = true
	opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	// unixSocketPath intentionally left empty; address carries the path.
	c := &client{opts: opts, address: "/var/run/quicbaas/quicbaas.sock"}

	tr, err := c.buildQRDBTransport()
	if err != nil {
		t.Fatalf("buildQRDBTransport: %v", err)
	}
	if _, ok := tr.(*qrdbgrpc.Transport); !ok {
		t.Fatalf("expected *qrdbgrpc.Transport for unix+TLS, got %T", tr)
	}
}

// TestUnixGRPCTarget normalizes a raw socket path and preserves explicit
// unix schemes so go-qrdb's gRPC unix name resolver dials the socket.
func TestUnixGRPCTarget(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"raw absolute path", "/var/run/quicbaas.sock", "unix:///var/run/quicbaas.sock"},
		{"already unix double-slash", "unix:///var/run/quicbaas.sock", "unix:///var/run/quicbaas.sock"},
		{"already unix single-colon", "unix:/var/run/quicbaas.sock", "unix:/var/run/quicbaas.sock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unixGRPCTarget(tc.in); got != tc.want {
				t.Errorf("unixGRPCTarget(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
