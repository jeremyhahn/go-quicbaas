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

package quicbaas

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/grpc"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// certIdentityTransport builds the Auth service of an SDK client whose qrdb
// user lookups reach a directory holding bound over one transport.
//
// The unit tier covers the transports whose server side the qrdb SDK
// exposes: the embedded dispatcher and the gRPC service, served over TCP
// loopback and over a Unix socket (the qrdb Unix transport is gRPC over a
// Unix domain socket). REST, QUIC and MCP are served by qrdb's native
// unified-server handlers, which only a running node carries; the same
// cases run on them against a real daemon in test/integration/login.
type certIdentityTransport struct {
	name string
	auth func(t *testing.T, bound map[string]*qrdbsdk.User, options ...Option) AuthService
}

func certIdentityTransports() []certIdentityTransport {
	return []certIdentityTransport{
		{name: "embedded", auth: newCertIdentityEmbeddedAuth},
		{name: "grpc", auth: newCertIdentityGRPCAuth},
		{name: "unix", auth: newCertIdentityUnixAuth},
	}
}

// certIdentityCase is one certificate-to-user resolution every transport
// must answer the same way.
type certIdentityCase struct {
	name string
	// userTenant is the tenant of the user bound to the certificate; empty
	// leaves the certificate unbound.
	userTenant string
	// ctxTenant is the tenant carried on the request context.
	ctxTenant string
	// options configure the client, e.g. WithTenant.
	options   []Option
	wantFound bool
}

func certIdentityCases() []certIdentityCase {
	return []certIdentityCase{
		{
			name:       "found in the caller's tenant",
			userTenant: "acme",
			ctxTenant:  "acme",
			wantFound:  true,
		},
		{
			name:      "unbound certificate is NotFound",
			ctxTenant: "acme",
		},
		{
			name:       "another tenant's user is NotFound by context tenant",
			userTenant: "globex",
			ctxTenant:  "acme",
		},
		{
			name:       "another tenant's user is NotFound by client tenant",
			userTenant: "globex",
			options:    []Option{WithTenant("acme")},
		},
		{
			name:       "context tenant overrides client tenant",
			userTenant: "globex",
			ctxTenant:  "acme",
			options:    []Option{WithTenant("globex")},
		},
		{
			name:       "caller with no tenant sees every tenant",
			userTenant: "globex",
			wantFound:  true,
		},
	}
}

func TestAuthUserByCertificate_EveryTransport(t *testing.T) {
	for _, tr := range certIdentityTransports() {
		for _, tc := range certIdentityCases() {
			t.Run(tr.name+"/"+tc.name, func(t *testing.T) {
				cert := newTestCert(t)
				bound := map[string]*qrdbsdk.User{}
				if tc.userTenant != "" {
					bound[ComputeSPKIThumbprint(cert)] = &qrdbsdk.User{ID: 7, Username: "device", TenantID: tc.userTenant}
				}
				auth := tr.auth(t, bound, tc.options...)

				ctx := context.Background()
				if tc.ctxTenant != "" {
					ctx = transport.ContextWithTenant(ctx, tc.ctxTenant)
				}
				got, err := auth.UserByCertificate(ctx, cert)
				if !tc.wantFound {
					if !sdkerrors.IsNotFound(err) {
						t.Fatalf("want NotFoundError, got %+v, %T: %v", got, err, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("UserByCertificate: %v", err)
				}
				if got.ID != 7 || got.Username != "device" || got.TenantID != tc.userTenant {
					t.Errorf("resolved %+v, want user 7 \"device\" in tenant %s", got, tc.userTenant)
				}
			})
		}
	}
}

// newCertIdentityEmbeddedAuth dispatches the qrdb user surface in-process
// through the qrdb embedded transport.
func newCertIdentityEmbeddedAuth(t *testing.T, bound map[string]*qrdbsdk.User, options ...Option) AuthService {
	t.Helper()
	dir := &certBindingDirectory{directoryUserServicer: newDirectory(), byThumbprint: bound}
	servicers := QRDBEmbeddedServicers{KV: newMemKV(), Admin: stubAdminService{}, User: dir}
	return newCertIdentityClient(t, "embedded://local", append([]Option{WithEmbeddedQRDB(servicers)}, options...)...)
}

// newCertIdentityGRPCAuth serves the qrdb user surface on a real loopback
// gRPC listener with a kernel-assigned port; gRPC is the default transport.
func newCertIdentityGRPCAuth(t *testing.T, bound map[string]*qrdbsdk.User, options ...Option) AuthService {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveCertIdentityGRPC(t, lis, bound)
	return newCertIdentityClient(t, lis.Addr().String(), options...)
}

// newCertIdentityUnixAuth serves the qrdb user surface on a Unix domain
// socket in a temporary directory.
func newCertIdentityUnixAuth(t *testing.T, bound map[string]*qrdbsdk.User, options ...Option) AuthService {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "qrdb.sock")
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveCertIdentityGRPC(t, lis, bound)
	return newCertIdentityClient(t, socket, append([]Option{WithUnix(socket)}, options...)...)
}

// serveCertIdentityGRPC registers the qrdb user surface on lis the way the
// server registers it (qrdbsdk.RegisterGRPCService) and serves it until the
// test ends.
func serveCertIdentityGRPC(t *testing.T, lis net.Listener, bound map[string]*qrdbsdk.User) {
	t.Helper()
	srv := grpc.NewServer()
	dir := &certBindingDirectory{directoryUserServicer: newDirectory(), byThumbprint: bound}
	qrdbsdk.RegisterGRPCService(srv, nil, nil, qrdbsdk.WithRegisterUserService(dir))
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
}

// newCertIdentityClient builds an SDK client for address from options and
// returns its Auth service, closing the qrdb sub-client when the test ends.
func newCertIdentityClient(t *testing.T, address string, options ...Option) AuthService {
	t.Helper()
	opts := defaultOptions()
	for _, apply := range options {
		if err := apply(opts); err != nil {
			t.Fatalf("option: %v", err)
		}
	}
	c := &client{opts: opts, address: address}
	t.Cleanup(func() {
		if qc, err := c.qrdbClient(); err == nil {
			if err := qc.Close(); err != nil {
				t.Errorf("close qrdb client: %v", err)
			}
		}
	})
	return c.Security().Auth()
}
