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

package grpc

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// getSecretServer answers GetSecret with a fixed payload.
type getSecretServer struct {
	pb.UnimplementedSecretsManagementServer
	fail    bool
	data    []byte
	lastID  string
	lastVer uint64
}

func (f *getSecretServer) GetSecret(_ context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastID, f.lastVer = req.SecretId, req.Version
	return &pb.GetSecretResponse{SecretId: req.SecretId, Version: 1, Data: f.data}, nil
}

// secretsHandlerTransport routes the secrets client straight into a
// secretsHandler, so the test exercises the client and the gRPC handler
// together over a real gRPC connection.
type secretsHandlerTransport struct{ h *secretsHandler }

func (s secretsHandlerTransport) Request(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	return s.h.handleRequest(ctx, svc, method, req)
}

func (secretsHandlerTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (secretsHandlerTransport) Conn() interface{} { return nil }
func (secretsHandlerTransport) Close() error      { return nil }
func (secretsHandlerTransport) Healthy() bool     { return true }

func newGetSecretHandler(t *testing.T, fake *getSecretServer) *secretsHandler {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterSecretsManagementServer(s, fake)
	})
	h, err := newSecretsHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newSecretsHandler: %v", err)
	}
	return h
}

// TestSecretsHandler_GetData_Borrow drives Client.GetWithBytes over gRPC. The
// lent value is compared in place inside fn, never copied out
// (testing-guidelines.md).
func TestSecretsHandler_GetData_Borrow(t *testing.T) {
	fake := &getSecretServer{data: []byte(`{"tcp_ao_key":"k\u00e9y"}`)}
	h := newGetSecretHandler(t, fake)

	matched := false
	err := secretsclient.NewClient(secretsHandlerTransport{h}).GetWithBytes(context.Background(),
		"gw/ao", "tcp_ao_key", func(b []byte) error {
			matched = bytes.Equal(b, []byte("kéy"))
			return nil
		}, secretsclient.WithVersion(4))
	if err != nil {
		t.Fatalf("GetWithBytes: %v", err)
	}
	if !matched {
		t.Error("lent bytes differ from the stored field")
	}
	if fake.lastID != "gw/ao" || fake.lastVer != 4 {
		t.Errorf("server saw id %q version %d, want gw/ao 4", fake.lastID, fake.lastVer)
	}
}

// TestSecretsHandler_GetData_ReturnsBytes pins that GetData hands the client
// the service response — the payload as bytes — rather than a decoded map.
func TestSecretsHandler_GetData_ReturnsBytes(t *testing.T) {
	h := newGetSecretHandler(t, &getSecretServer{data: []byte(`{"k":"v"}`)})
	resp, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "GetData",
		&struct {
			Path    string
			Version int
		}{Path: "p", Version: -1})
	if err != nil {
		t.Fatalf("GetData: %v", err)
	}
	svcResp, ok := resp.(*service.GetSecretResponse)
	if !ok {
		t.Fatalf("response type %T, want *service.GetSecretResponse", resp)
	}
	if !bytes.Equal(svcResp.Data, []byte(`{"k":"v"}`)) {
		t.Error("payload bytes not passed through")
	}
}

func TestSecretsHandler_GetData_Errors(t *testing.T) {
	h := newGetSecretHandler(t, &getSecretServer{fail: true})
	if _, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "GetData", "bad"); err == nil {
		t.Error("expected error for invalid request type")
	}
	if _, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "GetData",
		&struct{ Path string }{Path: "p"}); err == nil {
		t.Error("expected error from failing server")
	}
}
