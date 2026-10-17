// Copyright 2026 Jeremy Hahn
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	qrdbunix "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const testSealSOPIN = "so-pin-1234"

// sealRecorder is the qrdb barrier servicer behind the canonical route. Only
// BarrierSeal is exercised; it counts the seals that reached the barrier.
type sealRecorder struct {
	qrdbsdk.BarrierServicer
	sealed atomic.Int32
}

func (r *sealRecorder) BarrierSeal(context.Context) error {
	r.sealed.Add(1)
	return nil
}

// sealClient is the part of a qrdb SDK wire transport the tests drive.
type sealClient interface {
	Connect(ctx context.Context) error
	BarrierSeal(ctx context.Context) error
	Close() error
}

// sealTLS returns a server and a client TLS config sharing one self-signed
// certificate for localhost and 127.0.0.1.
func sealTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,

		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	server := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}},
		MinVersion:   tls.VersionTLS13,
	}
	client := &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	return server, client
}

// startSealServer serves the canonical qrdb.v1 barrier surface, guarded by
// UnaryBarrierSealSOPINInterceptor, on a TCP port and on a Unix socket —
// the two listeners quicbaasd's gRPC server serves — and returns a client
// factory per transport.
func startSealServer(t *testing.T, backend qrdbsdk.PINBackend) (*sealRecorder, map[string]func() sealClient) {
	t.Helper()
	serverTLS, clientTLS := sealTLS(t)
	recorder := &sealRecorder{}
	gs := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ChainUnaryInterceptor(UnaryBarrierSealSOPINInterceptor(backend)),
	)
	qrdbsdk.RegisterGRPCService(gs, nil, nil, qrdbsdk.WithRegisterBarrierService(recorder))

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	// A short directory keeps the socket path inside the sun_path limit.
	dir, err := os.MkdirTemp("", "qbseal")
	if err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	socket := filepath.Join(dir, "s.sock")
	unix, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	go func() { _ = gs.Serve(tcp) }()
	go func() { _ = gs.Serve(unix) }()
	t.Cleanup(func() {
		gs.Stop()
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket dir: %v", err)
		}
	})

	clients := map[string]func() sealClient{
		"grpc": func() sealClient {
			c, err := qrdbgrpc.New(qrdbtransport.WithAddress(tcp.Addr().String()), qrdbtransport.WithTLSConfig(clientTLS))
			if err != nil {
				t.Fatalf("grpc client: %v", err)
			}
			return c
		},
		"unix": func() sealClient {
			c, err := qrdbunix.New(qrdbtransport.WithAddress(socket), qrdbtransport.WithTLSConfig(clientTLS))
			if err != nil {
				t.Fatalf("unix client: %v", err)
			}
			return c
		},
	}
	return recorder, clients
}

// sealOver connects a fresh SDK client and seals with pin bound by
// qrdbsdk.WithSOPIN; an empty pin binds none.
func sealOver(t *testing.T, newClient func() sealClient, pin string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := newClient()
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if pin != "" {
		bound, release, err := qrdbsdk.WithSOPIN(ctx, []byte(pin))
		if err != nil {
			t.Fatalf("bind SO PIN: %v", err)
		}
		defer release()
		ctx = bound
	}
	return client.BarrierSeal(ctx)
}

func sealPINBackend(t *testing.T, soPIN string) qrdbsdk.PINBackend {
	t.Helper()
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	if err != nil {
		t.Fatalf("PIN backend: %v", err)
	}
	if soPIN != "" {
		if err := backend.SetSOPIN("", soPIN); err != nil {
			t.Fatalf("set SO PIN: %v", err)
		}
	}
	return backend
}

// TestBarrierSealSOPIN_CanonicalRoute drives /qrdb.v1.QRDBService/BarrierSeal
// with the Go SDK's gRPC and Unix clients: with an SO PIN configured, a
// missing or wrong PIN is refused with PermissionDenied — the ErrForbidden
// REST and MCP return — and the barrier is never reached; the right PIN
// seals; with no SO PIN configured, seal is unchanged.
func TestBarrierSealSOPIN_CanonicalRoute(t *testing.T) {
	tests := []struct {
		name       string
		configured string // SO PIN set on the backend; "" leaves none set
		noBackend  bool
		pin        string
		wantSealed bool
	}{
		{name: "missing PIN is refused", configured: testSealSOPIN, pin: ""},
		{name: "wrong PIN is refused", configured: testSealSOPIN, pin: "wrong-pin"},
		{name: "right PIN seals", configured: testSealSOPIN, pin: testSealSOPIN, wantSealed: true},
		{name: "no SO PIN set seals without a PIN", configured: "", pin: "", wantSealed: true},
		{name: "no PIN backend seals without a PIN", noBackend: true, pin: "", wantSealed: true},
	}
	for _, transport := range []string{"grpc", "unix"} {
		for _, tt := range tests {
			t.Run(transport+"/"+tt.name, func(t *testing.T) {
				var backend qrdbsdk.PINBackend
				if !tt.noBackend {
					backend = sealPINBackend(t, tt.configured)
				}
				recorder, clients := startSealServer(t, backend)

				err := sealOver(t, clients[transport], tt.pin)

				if tt.wantSealed {
					if err != nil {
						t.Fatalf("seal: %v", err)
					}
					if got := recorder.sealed.Load(); got != 1 {
						t.Fatalf("barrier sealed %d times, want 1", got)
					}
					return
				}
				var reqErr *qrdbtransport.RequestError
				if !errors.As(err, &reqErr) || reqErr.StatusCode != int(codes.PermissionDenied) {
					t.Fatalf("want PermissionDenied RequestError, got %T %v", err, err)
				}
				if got := recorder.sealed.Load(); got != 0 {
					t.Fatalf("refused seal reached the barrier %d times", got)
				}
			})
		}
	}
}

// fieldPINRequest stands in for a BarrierSealRequest that carries the PIN only
// in its so_pin field, as a raw-proto caller sends it.
type fieldPINRequest struct{ pin string }

func (r fieldPINRequest) GetSoPin() string { return r.pin }

func TestUnaryBarrierSealSOPINInterceptor_Direct(t *testing.T) {
	interceptor := UnaryBarrierSealSOPINInterceptor(sealPINBackend(t, testSealSOPIN))
	sealInfo := &grpc.UnaryServerInfo{FullMethod: qrdbBarrierSealMethod}
	handled := func(context.Context, any) (any, error) { return "sealed", nil }

	t.Run("request field PIN is accepted without metadata", func(t *testing.T) {
		resp, err := interceptor(context.Background(), fieldPINRequest{pin: testSealSOPIN}, sealInfo, handled)
		if err != nil || resp != "sealed" {
			t.Fatalf("want sealed, got %v, %v", resp, err)
		}
	})

	t.Run("metadata PIN wins over the request field", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(),
			metadata.Pairs(qrdbtransport.SOPINMetadataKey, "wrong-pin"))
		_, err := interceptor(ctx, fieldPINRequest{pin: testSealSOPIN}, sealInfo, handled)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("want PermissionDenied, got %v", err)
		}
	})

	t.Run("missing PIN carries ErrForbidden semantics", func(t *testing.T) {
		_, err := interceptor(context.Background(), fieldPINRequest{}, sealInfo, handled)
		want := ToGRPCError(&dberrors.QBError{Code: dberrors.ErrForbidden, Op: "barrier.Seal", Err: qrdbsdk.ErrSOPINRequired})
		if status.Code(err) != status.Code(want) {
			t.Fatalf("want %v, got %v", status.Code(want), err)
		}
	})

	t.Run("other methods pass through unguarded", func(t *testing.T) {
		other := &grpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/BarrierStatus"}
		resp, err := interceptor(context.Background(), fieldPINRequest{}, other, handled)
		if err != nil || resp != "sealed" {
			t.Fatalf("want pass-through, got %v, %v", resp, err)
		}
	})
}
