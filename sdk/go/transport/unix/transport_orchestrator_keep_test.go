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

package unix

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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// spawnRecorder hands every SpawnSession request it receives to spawns.
type spawnRecorder struct {
	pb.UnimplementedOrchestratorServiceServer
	spawns chan *pb.SpawnSessionRequest
}

func (s *spawnRecorder) SpawnSession(_ context.Context, req *pb.SpawnSessionRequest) (*pb.SpawnSessionResponse, error) {
	s.spawns <- req
	return &pb.SpawnSessionResponse{SessionId: "s1"}, nil
}

// TestTransport_SpawnSessionCarriesKeep spawns over a real Unix socket and
// asserts the keep marker reaches the server, and that a spawn which does not
// set it arrives unkept.
func TestTransport_SpawnSessionCarriesKeep(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	recorder := &spawnRecorder{spawns: make(chan *pb.SpawnSessionRequest, 2)}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterOrchestratorServiceServer(server, recorder)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	for _, keep := range []bool{true, false} {
		if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
			&orchestratorclient.SpawnSessionRequest{Backend: "claude", Keep: keep}); err != nil {
			t.Fatalf("SpawnSession(keep=%v): %v", keep, err)
		}
		if got := (<-recorder.spawns).GetKeep(); got != keep {
			t.Errorf("server saw keep=%v, the caller sent keep=%v", got, keep)
		}
	}
}

// unixTestTLS is a self-signed server identity for "localhost", the authority
// gRPC names a Unix-socket target by, and a client config that trusts it.
func unixTestTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	server = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}},
	}
	client = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "localhost"}
	return server, client
}
