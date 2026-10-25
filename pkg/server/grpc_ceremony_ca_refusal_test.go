//go:build !integration

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/pem"
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	grpctransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	unixtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ceremonyCAUpstreamIssue is the upstream issue every refusal must name.
const ceremonyCAUpstreamIssue = "go-qrdb #1129"

// qrdbCeremonyTransport is the slice of the qrdb SDK gRPC and Unix transports
// these tests drive.
type qrdbCeremonyTransport interface {
	CeremonyInitialize(ctx context.Context, req *transport.CeremonyInitializeRequest) (*transport.CeremonyInitializeResponse, error)
	Connect(ctx context.Context) error
	Close() error
}

// serveQRDBCeremony serves the qrdb.v1 surface buildQRDBRegisterOptions wires,
// behind the ceremony CA refusal interceptor, on network ("tcp" or "unix"),
// and returns a connected qrdb SDK transport of the matching kind. The
// ceremony servicer is qrdb's real ceremony core, so a request that gets past
// the refusal reaches it.
func serveQRDBCeremony(t *testing.T, network string) qrdbCeremonyTransport {
	t.Helper()
	srv := withTempServer(t)
	cert, roots := newSealSocketTLSCert(t)
	srv.ceremonyServices = newNodeCeremonyServices(t, cert)

	address := "127.0.0.1:0"
	if network == "unix" {
		address = filepath.Join(t.TempDir(), "qrdb.sock")
	}
	lis, err := net.Listen(network, address)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := ggrpc.NewServer(
		ggrpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		})),
		ggrpc.ChainUnaryInterceptor(ceremonyCARefusalUnaryInterceptor()),
	)
	// healthyAdmin answers the Health RPC the transports' Connect calls;
	// everything else is the option set registerQRDBService registers.
	qrdbsdk.RegisterGRPCService(gs, nil, healthyAdmin{}, srv.buildQRDBRegisterOptions()...)
	served := make(chan error, 1)
	go func() { served <- gs.Serve(lis) }()
	t.Cleanup(func() {
		gs.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	opts := []transport.Option{
		transport.WithAddress(lis.Addr().String()),
		transport.WithTLSConfig(&tls.Config{
			RootCAs:    roots,
			ServerName: sealSocketServerName,
			MinVersion: tls.VersionTLS13,
		}),
	}
	var client qrdbCeremonyTransport
	if network == "unix" {
		client, err = unixtransport.New(opts...)
	} else {
		client, err = grpctransport.New(opts...)
	}
	if err != nil {
		t.Fatalf("%s transport: %v", network, err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return client
}

// newNodeCeremonyServices builds the ceremony stack the way an embedded qrdb
// node builds its own: no CeremonyCA, with the node's TLS server certificate
// as the SPKI pin source and a software PIN backend, so the bring-your-own-certificate mode can complete.
func newNodeCeremonyServices(t *testing.T, serverCert tls.Certificate) *CeremonyServices {
	t.Helper()
	logger := slog.Default()
	barrier, err := qrdbsdk.NewStorageBarrier(
		logger,
		qrdbsdk.NewMemoryBackend(),
		qrdbsdk.BarrierConfig{RootKeyPath: "test/barrier/root-key"},
		qrdbsdk.NewDeferredSoftwareStrategy(),
	)
	if err != nil {
		t.Fatalf("barrier: %v", err)
	}
	core, err := qrdbsdk.NewCeremonyService(&qrdbsdk.CeremonyConfig{
		DataDir:       t.TempDir(),
		ServerCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert.Certificate[0]}),
	}, barrier, logger)
	if err != nil {
		t.Fatalf("ceremony core: %v", err)
	}
	core.SetTrustDomain(testTrustDomain)
	pinCfg := &config.Config{}
	pinCfg.Storage.DataDir = t.TempDir()
	pinBackend, err := buildPINBackend(pinCfg, logger, nil)
	if err != nil {
		t.Fatalf("PIN backend: %v", err)
	}
	core.SetPINBackend(pinBackend)
	return &CeremonyServices{
		Barrier:  qrdbsdk.NewBarrierService(barrier),
		Ceremony: qrdbsdk.NewServiceCeremonyService(core),
		Core:     core,
	}
}

// newAdminCSRPEM returns a PEM CSR for the bootstrap admin.
func newAdminCSRPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// requireCeremonyCARefusal asserts err is the typed ceremony CA refusal as a
// qrdb SDK transport surfaces it: gRPC FailedPrecondition as both the status
// and the transport.RequestError status code, and a message naming the upstream issue and
// the transports that serve the mode.
func requireCeremonyCARefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("ceremony initialization must be refused, got success")
	}
	if code := status.Code(err); code != codes.FailedPrecondition {
		var st interface{ GRPCStatus() *status.Status }
		if !errors.As(err, &st) || st.GRPCStatus().Code() != codes.FailedPrecondition {
			t.Fatalf("got %v (%T), want gRPC FailedPrecondition", err, err)
		}
	}
	var reqErr *transport.RequestError
	if !errors.As(err, &reqErr) || reqErr.StatusCode != int(codes.FailedPrecondition) {
		t.Errorf("got %v (%T), want a transport.RequestError with status %d", err, err, codes.FailedPrecondition)
	}
	for _, want := range []string{ceremonyCAUpstreamIssue, "REST", "MCP"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err.Error(), want)
		}
	}
}

func TestQRDBCeremonyInitialize_CAModesRefused(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			client := serveQRDBCeremony(t, network)
			modes := map[string]*transport.CeremonyInitializeRequest{
				"csr-sign":        {SOPin: "123456", UserPin: "654321", AdminCSRPEM: newAdminCSRPEM(t)},
				"generated-admin": {SOPin: "123456", UserPin: "654321", AdminGenerate: true},
			}
			for mode, req := range modes {
				t.Run(mode, func(t *testing.T) {
					_, err := client.CeremonyInitialize(context.Background(), req)
					requireCeremonyCARefusal(t, err)
				})
			}
		})
	}
}

func TestQRDBCeremonyInitialize_BringYourOwnAdminCertIsServed(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			client := serveQRDBCeremony(t, network)
			adminCert := string(newBYOAdminCertPEM(t))
			resp, err := client.CeremonyInitialize(context.Background(), &transport.CeremonyInitializeRequest{
				SOPin:            "123456",
				UserPin:          "654321",
				AdminCertPEM:     adminCert,
				AdminCertPoolPEM: adminCert,
				Secret:           []byte("system-barrier-passphrase"),
			})
			if err != nil {
				t.Fatalf("bring-your-own admin certificate over %s: %v", network, err)
			}
			if resp.AdminCredentialSource == "" {
				t.Errorf("response names no admin credential source: %+v", resp)
			}
		})
	}
}
