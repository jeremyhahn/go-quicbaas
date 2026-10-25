// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	unixtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"
)

// sealRecordingBarrier is a barrier servicer that only records BarrierSeal;
// the SO PIN gate runs before it, so reaching it means the gate admitted the
// call. Any other barrier verb panics on the nil embedded interface.
type sealRecordingBarrier struct {
	qrdbsdk.BarrierServicer
	sealed atomic.Bool
}

func (b *sealRecordingBarrier) BarrierSeal(context.Context) error {
	b.sealed.Store(true)
	return nil
}

// healthyAdmin answers the Health probe the Unix transport's Connect makes.
type healthyAdmin struct {
	qrdbsdk.AdminServicer
}

func (healthyAdmin) Health(context.Context) (*qrdbsdk.HealthStatus, error) {
	return &qrdbsdk.HealthStatus{Status: "ok"}, nil
}

// sealOverQRDBGRPC serves srv's qrdb.v1 register options, plus barrier, on a
// TLS Unix socket and calls BarrierSeal through the qrdb SDK Unix transport,
// presenting soPIN as the SO PIN when it is not empty.
func sealOverQRDBGRPC(t *testing.T, srv *Server, barrier qrdbsdk.BarrierServicer, soPIN string) error {
	t.Helper()
	cert, roots := newSealSocketTLSCert(t)
	socket := filepath.Join(t.TempDir(), "qrdb.sock")
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := ggrpc.NewServer(ggrpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})))
	opts := append(srv.buildQRDBRegisterOptions(), qrdbsdk.WithRegisterBarrierService(barrier))
	qrdbsdk.RegisterGRPCService(gs, nil, healthyAdmin{}, opts...)
	served := make(chan error, 1)
	go func() { served <- gs.Serve(lis) }()
	t.Cleanup(func() {
		gs.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	client, err := unixtransport.New(
		transport.WithAddress(socket),
		transport.WithTLSConfig(&tls.Config{
			RootCAs:    roots,
			ServerName: sealSocketServerName,
			MinVersion: tls.VersionTLS13,
		}),
	)
	if err != nil {
		t.Fatalf("unix transport: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if soPIN != "" {
		bound, release, err := qrdbsdk.WithSOPIN(ctx, []byte(soPIN))
		if err != nil {
			t.Fatalf("WithSOPIN: %v", err)
		}
		t.Cleanup(release)
		ctx = bound
	}
	return client.BarrierSeal(ctx)
}

// sealSocketServerName is the name the seal socket's TLS certificate is
// issued to and the client verifies.
const sealSocketServerName = "qrdb-seal-socket"

// newSealSocketTLSCert issues a self-signed TLS server certificate for the
// seal socket, and the pool that trusts it. The SDK sends a bound SO PIN only
// over TLS, so the PIN tests need a TLS socket.
func newSealSocketTLSCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: sealSocketServerName},
		DNSNames:     []string{sealSocketServerName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}

// TestBuildQRDBRegisterOptions_WiresThePINBackend proves the qrdb.v1 gRPC
// registration hands the server's PIN backend to qrdb's SO PIN gate: with a
// backend that has no SO PIN provisioned yet, BarrierSeal is admitted and
// reaches the barrier.
func TestBuildQRDBRegisterOptions_WiresThePINBackend(t *testing.T) {
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	if err != nil {
		t.Fatalf("NewSoftwareBackend: %v", err)
	}
	barrier := &sealRecordingBarrier{}

	if err := sealOverQRDBGRPC(t, &Server{pinBackend: backend}, barrier, ""); err != nil {
		t.Fatalf("BarrierSeal with the PIN backend wired: %v", err)
	}
	if !barrier.sealed.Load() {
		t.Fatal("BarrierSeal never reached the barrier")
	}
}

// TestBuildQRDBRegisterOptions_NoPINBackendFailsClosed proves the gate the
// option feeds: a server with no PIN backend has BarrierSeal refused before
// the barrier is touched.
func TestBuildQRDBRegisterOptions_NoPINBackendFailsClosed(t *testing.T) {
	barrier := &sealRecordingBarrier{}

	if err := sealOverQRDBGRPC(t, &Server{}, barrier, ""); err == nil {
		t.Fatal("BarrierSeal without a PIN backend succeeded, want it refused")
	}
	if barrier.sealed.Load() {
		t.Fatal("BarrierSeal reached the barrier without a PIN backend")
	}
}

// testSOPIN is the SO PIN provisioned by newSOPINBackend.
const testSOPIN = "so-pin-123456"

// newSOPINBackend returns a software PIN backend with testSOPIN provisioned.
func newSOPINBackend(t *testing.T) qrdbsdk.PINBackend {
	t.Helper()
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	if err != nil {
		t.Fatalf("NewSoftwareBackend: %v", err)
	}
	if err := backend.SetSOPIN("", testSOPIN); err != nil {
		t.Fatalf("SetSOPIN: %v", err)
	}
	return backend
}

// TestBuildQRDBRegisterOptions_SOPINGatesSeal proves qrdb's SO PIN gate, fed
// the server's PIN backend by the register options, decides BarrierSeal over
// the qrdb.v1 gRPC service once an SO PIN is provisioned: a wrong or missing
// PIN is refused before the barrier is touched, and the right PIN seals.
func TestBuildQRDBRegisterOptions_SOPINGatesSeal(t *testing.T) {
	tests := []struct {
		name       string
		soPIN      string
		wantCode   codes.Code
		wantSealed bool
	}{
		{name: "wrong PIN refused", soPIN: "wrong-pin-654321", wantCode: codes.Unauthenticated},
		{name: "missing PIN refused", soPIN: "", wantCode: codes.Unauthenticated},
		{name: "right PIN seals", soPIN: testSOPIN, wantCode: codes.OK, wantSealed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			barrier := &sealRecordingBarrier{}
			srv := &Server{pinBackend: newSOPINBackend(t)}

			err := sealOverQRDBGRPC(t, srv, barrier, tt.soPIN)
			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("BarrierSeal code = %v, want %v (err: %v)", got, tt.wantCode, err)
			}
			if got := barrier.sealed.Load(); got != tt.wantSealed {
				t.Fatalf("barrier sealed = %v, want %v", got, tt.wantSealed)
			}
		})
	}
}
