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

package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	sdkgrpc "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// Until go-quicbaas #1816 re-pins go-qrdb, the server refuses a verified login
// for any tenant other than the system tenant. These tests pin that the SDK
// surfaces the refusal as the typed TenantLoginRefusedError on each distinct
// error-mapping path: REST (which QUIC shares), gRPC (which Unix shares) and
// the embedded transport.

// tenantRefusingLoginClient is a node LoginClient whose FinishLogin fails the
// way the server's session issuer does for a non-system tenant.
type tenantRefusingLoginClient struct {
	peerCertLoginClient
}

func (f *tenantRefusingLoginClient) FinishLogin(context.Context, qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	return nil, service.RefuseTenantLogin("acme")
}

// tenantRefusalRequest is a PIV login finish for a non-system tenant user.
func tenantRefusalRequest() FinishLoginRequest {
	return FinishLoginRequest{
		Type:      qrdbtransport.FactorTypePIV,
		SessionID: "sess",
		LookupKey: []byte("piv-slot-hash"),
		Signature: []byte("sig"),
	}
}

// restLoginFinishStub serves POST /api/v1/auth/login/finish with the given
// status and IAM error body, the shape pkg/api/rest answers a refused login.
func restLoginFinishStub(t *testing.T, status int, code string) AuthService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login/finish" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error":             code,
			"error_description": code,
		}); err != nil {
			t.Errorf("encode stub body: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	tr, err := rest.New(&transport.Config{Address: server.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("rest.New: %v", err)
	}
	return &authServiceImpl{t: tr}
}

func TestFinishLogin_RESTTenantRefusalIsTyped(t *testing.T) {
	auth := restLoginFinishStub(t, http.StatusPreconditionFailed, "tenant_login_refused")

	_, err := auth.FinishLogin(context.Background(), tenantRefusalRequest())

	var refused *sdkerrors.TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("FinishLogin error = %v, want TenantLoginRefusedError", err)
	}
	if got := sdkerrors.Code(err); got != sdkerrors.CodePreconditionFailed {
		t.Errorf("FinishLogin code = %s, want %s", got, sdkerrors.CodePreconditionFailed)
	}
}

func TestFinishLogin_RESTOtherErrorPassesThrough(t *testing.T) {
	auth := restLoginFinishStub(t, http.StatusUnauthorized, "login_rejected")

	_, err := auth.FinishLogin(context.Background(), tenantRefusalRequest())

	var refused *sdkerrors.TenantLoginRefusedError
	if errors.As(err, &refused) {
		t.Fatalf("FinishLogin error = %v, want the rejection unchanged", err)
	}
	if got := sdkerrors.Code(err); got != sdkerrors.CodeUnauthorized {
		t.Errorf("FinishLogin code = %s, want %s", got, sdkerrors.CodeUnauthorized)
	}
}

func TestFinishLogin_GRPCTenantRefusalIsTyped(t *testing.T) {
	creds := newAdminFactorTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	server := grpclib.NewServer(grpclib.Creds(credentials.NewTLS(creds.server(nil))))
	grpcapi.RegisterLoginServer(server, service.NewLoginService(&tenantRefusingLoginClient{}), nil)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("grpc serve: %v", err)
		}
	})
	tr, err := sdkgrpc.New(&transport.Config{
		Address:   listener.Addr().String(),
		Timeout:   5 * time.Second,
		TLSConfig: creds.client(),
	})
	if err != nil {
		t.Fatalf("grpc.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	auth := &authServiceImpl{t: tr}

	_, err = auth.FinishLogin(context.Background(), tenantRefusalRequest())

	var refused *sdkerrors.TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("FinishLogin error = %v, want TenantLoginRefusedError", err)
	}
}

func TestFinishLogin_EmbeddedTenantRefusalIsTyped(t *testing.T) {
	tr, err := embedded.New(&peerCertQuicBaaS{login: service.NewLoginService(&tenantRefusingLoginClient{})})
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	auth := &authServiceImpl{t: tr}

	_, err = auth.FinishLogin(context.Background(), tenantRefusalRequest())

	var refused *sdkerrors.TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("FinishLogin error = %v, want TenantLoginRefusedError", err)
	}
	if got := sdkerrors.Code(err); got != sdkerrors.CodePreconditionFailed {
		t.Errorf("FinishLogin code = %s, want %s", got, sdkerrors.CodePreconditionFailed)
	}
}
