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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// With --transport quic the barrier verbs must reach a node that serves only
// its QUIC (UDP) listener: the request has to go over HTTP/3, not TCP
// (go-quicbaas#2533).

// h3BarrierTestCert is a self-signed loopback certificate for the UDP-only
// HTTP/3 listener (copied from transport/quic's newLoopbackCert).
func h3BarrierTestCert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}
}

// startUDPOnlyHTTP3Server serves handler over HTTP/3 on a UDP socket only:
// nothing listens on TCP at the returned address (copied from
// transport/quic's startTestHTTP3Server).
func startUDPOnlyHTTP3Server(t *testing.T, cert tls.Certificate, handler http.Handler) string {
	t.Helper()

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	srv := &http3.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h3"},
		},
		Handler: handler,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(udpConn)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = udpConn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return udpConn.LocalAddr().String()
}

func TestSecurityBarrier_QUIC_BarrierStatusReachesUDPOnlyHTTP3Listener(t *testing.T) {
	var statusHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/barrier/status", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 3 {
			t.Errorf("barrier status request protocol: got %s, want HTTP/3", r.Proto)
		}
		statusHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(qrdbtransport.BarrierStatusResponse{
			Sealed:   false,
			Strategy: "master_key",
		})
	})

	cert := h3BarrierTestCert(t)
	addr := startUDPOnlyHTTP3Server(t, cert, mux)

	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)
	c, err := NewClient(addr,
		WithQUIC(),
		WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}),
	)
	if err != nil {
		t.Fatalf("NewClient over QUIC: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := barrier.BarrierStatus(ctx)
	if err != nil {
		t.Fatalf("BarrierStatus over QUIC against a UDP-only HTTP/3 listener: %v", err)
	}
	if status.Sealed || status.Strategy != "master_key" {
		t.Errorf("BarrierStatus: got %+v, want unsealed master_key", status)
	}
	if got := statusHits.Load(); got != 1 {
		t.Errorf("server saw %d GET /v1/barrier/status requests, want 1", got)
	}
}
