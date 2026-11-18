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
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

// quicbaas's QUIC transport is its REST transport carried over HTTP/3 (ALPN
// "h3") on the REST port: the node serves no native qrdb-quic listener, so a
// QUIC-selected qrdb sub-client built over go-qrdb's native QUIC transport
// offers wire.ProtocolV2 and the server rejects the handshake with
// "tls: no application protocol" (go-quicbaas#2533). The qrdb sub-client for
// QUIC must reach the same qrdb REST API at the same origin.

// TestBuildQRDBTransport_QUIC_UsesRESTTransport pins the transport kind.
func TestBuildQRDBTransport_QUIC_UsesRESTTransport(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportQUIC
	c := &client{opts: opts, address: "test-node1:8080"}

	tr, err := c.buildQRDBTransport()
	if err != nil {
		t.Fatalf("buildQRDBTransport: %v", err)
	}
	if _, ok := tr.(*qrdbrest.Transport); !ok {
		t.Fatalf("expected *qrdbrest.Transport for the QUIC selection, got %T", tr)
	}
}

// TestBuildQRDBClient_QUICBareHostPort_ReachesBarrierStatus drives a
// QUIC-selected client against a qrdb REST stub at a bare host:port, the
// address the CLI supplies for --transport quic.
func TestBuildQRDBClient_QUICBareHostPort_ReachesBarrierStatus(t *testing.T) {
	srv, paths := newBarrierRESTStub(t)

	opts := defaultOptions()
	opts.transportType = TransportQUIC
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)

	address := strings.TrimPrefix(srv.URL, "https://")
	c := &client{opts: opts, address: address}

	qrdbClient, err := c.buildQRDBClient()
	if err != nil {
		t.Fatalf("buildQRDBClient over QUIC with a bare host:port address: %v", err)
	}
	if _, err := qrdbClient.Barrier().BarrierStatus(context.Background()); err != nil {
		t.Fatalf("BarrierStatus: %v", err)
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

// An embedded QRDB wins over the QUIC transport's HTTP/3 barrier shortcut:
// with WithQUIC() and WithEmbeddedQRDB(...) the barrier verbs must reach the
// in-process servicer, never the remote node (go-quicbaas#2533 review P1).
func TestBarrier_QUICWithEmbeddedQRDB_UsesEmbedded(t *testing.T) {
	var remoteHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/barrier/", func(w http.ResponseWriter, _ *http.Request) {
		remoteHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	cert := h3BarrierTestCert(t)
	addr := startUDPOnlyHTTP3Server(t, cert, mux)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)

	want := &qrdbsdk.BarrierStatusInfo{Strategy: "embedded-stub"}
	c, err := NewClient(addr,
		WithQUIC(),
		WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}),
		WithEmbeddedQRDB(QRDBEmbeddedServicers{
			KV:      stubKVService{},
			Admin:   stubAdminService{},
			Barrier: &stubBarrierService{status: want},
		}),
	)
	if err != nil {
		t.Fatalf("NewClient over QUIC with an embedded QRDB: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	qt, ok := c.(*client).transport.(*quic.Transport)
	if !ok {
		t.Fatalf("transport: got %T, want *quic.Transport", c.(*client).transport)
	}

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	if got, quicType := fmt.Sprintf("%T", barrier), fmt.Sprintf("%T", qt.Transport.Barrier()); got == quicType {
		t.Fatalf("Security().Barrier() returned the QUIC HTTP/3 adapter %s, want the embedded barrier", got)
	}

	resp, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if resp.Strategy != want.Strategy {
		t.Errorf("Strategy: got %q, want %q from the embedded stub", resp.Strategy, want.Strategy)
	}
	if n := remoteHits.Load(); n != 0 {
		t.Errorf("barrier verbs reached the remote node %d times, want 0", n)
	}
}
