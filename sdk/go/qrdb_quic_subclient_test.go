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
	"strings"
	"testing"

	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"
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
