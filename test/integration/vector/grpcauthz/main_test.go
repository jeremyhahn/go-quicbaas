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

//go:build integration

// Package grpcauthz is the end-to-end coverage of the vector service's
// per-tenant isolation and per-verb grant enforcement over a real gRPC server
// running the LIVE authorization interceptor (go-quicbaas#920).
//
// # What is real here
//
// Every layer a production request crosses is the shipped one:
//
//   - the server is pkg/server booted in-process in standalone (non-embedded)
//     mode with RequireAuthz=true, so the gRPC server, its TLS credentials and
//     its interceptor chain are built by pkg/server.startGRPCServer — the
//     platform-token authorization gate is pkg/api/grpc's
//     UnaryAuthorizationInterceptor / StreamAuthorizationInterceptor over the
//     same platformtoken.Validator the daemon builds, not a test-local chain;
//   - the listener is the daemon's Unix-domain-socket gRPC listener. That is
//     the one production listener that serves quicbaas.vector.v1.VectorDB
//     behind pkg/api/grpc's chain: network (TCP) gRPC is served by the
//     embedded qrdb node's unified server, which serves qrdb's own native
//     vector plane under qrdb's reflection interceptor and deliberately does
//     not register the quicbaas VectorDB wrapper (pkg/apimodule/module.go);
//   - the identity is minted by the server's own iam.OIDCProvider through the
//     OAuth2 client_credentials grant, against applications registered in real
//     tenants through the server's own iam.AppManager / iam.TenantManager;
//   - the vector handlers and the vector service underneath them are the
//     production ones, so tenant scoping is decided by the service layer from
//     the tenant the interceptor stamped onto the context.
//
// # Why authzfake is not used
//
// The files #905 deleted drove the dead interceptor, whose decision came from
// an interfaces.AuthorizationService — the seam test/integration/authzfake
// doubles. The live gRPC gate has no such seam: its decision is the
// platform-token Validator's offline scp check (GRPCValidatorAuthorizer). The
// grant each principal holds is therefore expressed the way production
// expresses it — as the permission scopes on its OAuth2 application, which the
// provider stamps into the token's scp claim — rather than through a double
// the live path never consults.
package grpcauthz

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// bootTimeout bounds how long the in-process server may take to become
// ready. First boot generates the CA, the barrier and the IAM plane.
const bootTimeout = 90 * time.Second

// readyProbeTimeout bounds a single readiness probe RPC.
const readyProbeTimeout = 2 * time.Second

// shutdownTimeout bounds the graceful shutdown at the end of the package.
const shutdownTimeout = 30 * time.Second

// harness is the package-wide server every test in this package talks to.
// One node per package keeps the suite's cost bounded; tests isolate from each
// other by provisioning their own tenants.
var harness *testHarness

// BootError reports that the in-process server could not be brought up.
type BootError struct {
	Op    string
	Cause error
}

func (e *BootError) Error() string {
	if e.Cause != nil {
		return "grpcauthz boot: " + e.Op + ": " + e.Cause.Error()
	}
	return "grpcauthz boot: " + e.Op
}

func (e *BootError) Unwrap() error { return e.Cause }

// testHarness is the running server plus what a test needs to reach it.
type testHarness struct {
	srv        *server.Server
	tempDir    string
	socketPath string
	conn       *grpc.ClientConn
	// started receives Start's return value. Standalone Start blocks until
	// shutdown, so a value arriving before readiness is a boot failure.
	started chan error
}

func TestMain(m *testing.M) {
	h, err := bootServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		if h != nil {
			h.teardown()
		}
		os.Exit(1)
	}
	harness = h

	code := m.Run()

	h.teardown()
	os.Exit(code)
}

// bootServer starts pkg/server in standalone mode with ONLY the Unix-socket
// gRPC listener enabled and the authorization gate on, and returns once that
// listener answers through the gate.
func bootServer() (*testHarness, error) {
	// A short temp root keeps the socket path well inside the 108-byte
	// sun_path limit.
	tempDir, err := os.MkdirTemp("", "qb-vecauthz-*")
	if err != nil {
		return nil, &BootError{Op: "create temp dir", Cause: err}
	}
	h := &testHarness{
		tempDir:    tempDir,
		socketPath: filepath.Join(tempDir, "grpc.sock"),
		started:    make(chan error, 1),
	}

	raftPort, err := freePort()
	if err != nil {
		return h, &BootError{Op: "allocate raft port", Cause: err}
	}

	// EmbeddedConfig is the secure in-process baseline: TLS on, anonymous
	// access off, RequireAuthz on, every network transport off. Turning
	// Embedded off is what makes pkg/server build its gRPC server with the
	// full production interceptor chain; enabling the Unix socket is what
	// serves it. No network listener is opened, so the only way in is the
	// gate under test.
	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Server.Embedded = false
	cfg.Server.Unix.Enabled = true
	cfg.Server.Unix.Socket = h.socketPath
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Logging.Level = "error"
	cfg.Vector.Enabled = true
	cfg.Security.RequireAuthz = true

	srv, err := server.NewServer(cfg)
	if err != nil {
		return h, &BootError{Op: "construct server", Cause: err}
	}
	h.srv = srv

	// Standalone Start blocks until shutdown, so it runs on its own goroutine
	// and reports its return value on h.started.
	go func() { h.started <- srv.Start() }()

	if err := h.waitReady(filepath.Join(cfg.Storage.DataDir, "certs", "ca-chain.crt")); err != nil {
		return h, err
	}
	return h, nil
}

// waitReady blocks until the Unix-socket listener exists and an anonymous
// vector RPC is refused with Unauthenticated — the observable proof that the
// listener is serving, the VectorDB service is registered, and the
// authorization gate is in the chain. server.IsStarted is NOT a readiness
// signal: it flips at the top of initialize, before any component exists.
func (h *testHarness) waitReady(caPath string) error {
	deadline := time.NewTimer(bootTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	// lastErr is what the most recent probe saw, reported if the deadline
	// passes so a boot that never gets ready says why.
	var lastErr error = context.DeadlineExceeded
	for {
		select {
		case err := <-h.started:
			return &BootError{Op: "server returned before becoming ready", Cause: err}
		case <-deadline.C:
			return &BootError{Op: "wait for the Unix-socket gRPC gate", Cause: lastErr}
		case <-tick.C:
		}

		if _, err := os.Stat(h.socketPath); err != nil {
			lastErr = err
			continue
		}
		if h.conn == nil {
			conn, err := dialUnixTLS(h.socketPath, caPath)
			if err != nil {
				return &BootError{Op: "dial Unix-socket gRPC listener", Cause: err}
			}
			h.conn = conn
		}

		ctx, cancel := context.WithTimeout(context.Background(), readyProbeTimeout)
		_, err := pb.NewVectorDBClient(h.conn).ListNamespaces(ctx, &pb.ListNamespacesRequest{})
		cancel()
		lastErr = err
		switch status.Code(err) {
		case codes.Unauthenticated:
			return nil
		case codes.OK:
			return &BootError{Op: "anonymous vector call was admitted: the authorization gate is not in the chain"}
		}
	}
}

// dialUnixTLS opens a gRPC client connection to the daemon's Unix socket. The
// daemon serves TLS on that socket too, so the client verifies the server
// certificate against the CA the daemon generated at first boot.
func dialUnixTLS(socketPath, caPath string) (*grpc.ClientConn, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, &BootError{Op: "read server CA chain", Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, &BootError{Op: "parse server CA chain"}
	}
	creds := credentials.NewTLS(&tls.Config{
		RootCAs:    pool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
	})
	return grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(creds))
}

// teardown stops the server, waits for Start to return, and removes the data.
func (h *testHarness) teardown() {
	if h.conn != nil {
		if err := h.conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "grpcauthz: close client connection: %v\n", err)
		}
	}
	if h.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := h.srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "grpcauthz: shutdown: %v\n", err)
		}
		select {
		case <-h.started:
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "grpcauthz: server did not return from Start before the shutdown deadline")
		}
		cancel()
	}
	if h.tempDir != "" {
		if err := os.RemoveAll(h.tempDir); err != nil {
			fmt.Fprintf(os.Stderr, "grpcauthz: remove temp dir: %v\n", err)
		}
	}
}

// freePort reserves a loopback port for a server that binds it later. It is
// reserved through netport rather than drawn from :0 and released, so nothing
// else can be handed it in between (go-quicbaas#721). The reservation lasts
// for the life of the test process.
func freePort() (int, error) {
	ports, err := netport.Reserve(1)
	if err != nil {
		return 0, err
	}
	return ports[0], nil
}
