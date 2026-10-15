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

//go:build !integration

package grpc

// gRPC interop leg (go-quicbaas#203). The server side is grpc-go with the
// interceptor chain startGRPCServer composes, authz included, behind a real
// platform-token validator. The client side is connectrpc.com/connect in
// gRPC mode: its own framing, trailer parsing, status mapping and HTTP/2
// stack (net/http), sharing no wire code with grpc-go. A server that only
// interoperates with grpc-go clients fails here.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const (
	interopIssuer   = "https://issuer.interop.test"
	interopAudience = "https://quicbaas.interop.test"
	interopKeyID    = "interop-kid"
	interopTenant   = "interop-tenant"
)

// grpcInteropHarness is a grpc-go server on a loopback TLS listener and an
// HTTP/2 client that a connect client drives.
type grpcInteropHarness struct {
	baseURL string
	client  *http.Client
	svc     *testutil.MockQuicBaaS
	signer  *ecdsa.PrivateKey
	now     time.Time
}

func newGRPCInteropHarness(t *testing.T) *grpcInteropHarness {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout", Component: "grpc-interop"})
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:   interopIssuer,
		Audience: interopAudience,
		JWKS: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: signer.Public(), KeyID: interopKeyID, Algorithm: "ES256", Use: "sig",
		}}},
		Clock:        func() time.Time { return now },
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)

	// The chain startGRPCServer builds, in its order, with an unsealed
	// barrier and an operational node.
	probe := &grpcTogglableProbe{}
	bypass := sealedwindow.NewAuthBypass(probe, &grpcCeremonyStateProbe{operational: true}, sealedwindow.NewLoggerAuditSink(log))
	allow := sealedwindow.GRPCFullMethodAllowlist()
	unary := []grpc.UnaryServerInterceptor{
		UnaryTimeoutInterceptor(30 * time.Second),
		UnaryLoggingInterceptor(log),
		UnaryMetricsInterceptor(),
		UnaryRecoveryInterceptor(log),
		UnaryTenantInterceptor(""),
		qrdbsdk.SealedWindowUnaryInterceptor(probe, allow),
		UnaryAuthorizationInterceptor(validator, WithSealedWindowAuthBypass(bypass)),
	}
	stream := []grpc.StreamServerInterceptor{
		StreamTimeoutInterceptor(5 * time.Minute),
		StreamLoggingInterceptor(log),
		StreamMetricsInterceptor(),
		StreamRecoveryInterceptor(log),
		StreamTenantInterceptor(""),
		qrdbsdk.SealedWindowStreamInterceptor(probe, allow),
		StreamAuthorizationInterceptor(validator, WithSealedWindowAuthBypass(bypass)),
	}

	certFile, keyFile, caFile := generateTestCertificates(t)
	creds, err := LoadServerTLSCredentials(&TLSConfig{Enabled: true, CertFile: certFile, KeyFile: keyFile})
	require.NoError(t, err)

	grpcServer := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)
	svc := testutil.NewMockQuicBaaS()
	apiServer, err := NewServer(svc, log, 7, "interop-v1")
	require.NoError(t, err)
	pb.RegisterQuicBaaSServer(grpcServer, apiServer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan error, 1)
	go func() { served <- grpcServer.Serve(listener) }()

	caPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))

	// HTTP/2 only: a server that does not negotiate h2 over ALPN fails the
	// handshake instead of falling back to HTTP/1.1.
	protocols := new(http.Protocols)
	protocols.SetHTTP2(true)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
		Protocols:       protocols,
	}

	t.Cleanup(func() {
		transport.CloseIdleConnections()
		grpcServer.Stop()
		require.NoError(t, <-served)
	})

	return &grpcInteropHarness{
		baseURL: "https://" + listener.Addr().String(),
		client:  &http.Client{Transport: transport},
		svc:     svc,
		signer:  signer,
		now:     now,
	}
}

// token mints a platform access token for the interop tenant with scopes.
func (h *grpcInteropHarness) token(t *testing.T, scopes ...string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":       interopIssuer,
		"aud":       []string{interopAudience},
		"sub":       "interop-user",
		"tenant_id": interopTenant,
		"scp":       scopes,
		"iat":       h.now.Unix(),
		"exp":       h.now.Add(10 * time.Minute).Unix(),
		"jti":       "jti-interop",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = interopKeyID
	token.Header["typ"] = "at+jwt"
	signed, err := token.SignedString(h.signer)
	require.NoError(t, err)
	return signed
}

func interopClient[Req, Res any](h *grpcInteropHarness, procedure string) *connect.Client[Req, Res] {
	return connect.NewClient[Req, Res](h.client, h.baseURL+procedure, connect.WithGRPC())
}

func bearerRequest[T any](msg *T, token string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	return req
}

// requireGRPCStatus asserts err is a gRPC status the connect client parsed
// from the response, not a transport failure, and that it carries want.
func requireGRPCStatus(t *testing.T, err error, want connect.Code) {
	t.Helper()
	require.Error(t, err)
	var connectErr *connect.Error
	require.True(t, errors.As(err, &connectErr), "want a gRPC status, got a transport error: %v", err)
	require.Equal(t, want, connect.CodeOf(err), "status code; error: %v", err)
}

// TestGRPCInterop_BaselineHealth is the baseline: one public unary call over
// TLS 1.3 with ALPN h2 must succeed before any other result means anything.
func TestGRPCInterop_BaselineHealth(t *testing.T) {
	h := newGRPCInteropHarness(t)
	h.svc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status:        service.HealthStatusHealthy,
		NodeID:        7,
		Version:       "interop-v1",
		UptimeSeconds: 42,
	}, nil).Once()

	var state tls.ConnectionState
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		TLSHandshakeDone: func(cs tls.ConnectionState, _ error) { state = cs },
	})
	client := interopClient[pb.HealthRequest, pb.HealthResponse](h, pb.QuicBaaS_Health_FullMethodName)
	resp, err := client.CallUnary(ctx, connect.NewRequest(&pb.HealthRequest{}))
	require.NoError(t, err, "baseline: an independent gRPC client could not call Health; every later interop result is void")

	assert.Equal(t, "h2", state.NegotiatedProtocol)
	assert.Equal(t, uint16(tls.VersionTLS13), state.Version)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Msg.GetStatus())
	assert.Equal(t, uint64(7), resp.Msg.GetNodeId())
	assert.Equal(t, "interop-v1", resp.Msg.GetVersion())
	assert.Equal(t, uint64(42), resp.Msg.GetUptimeSeconds())
	h.svc.AssertExpectations(t)
}

func TestGRPCInterop_AuthenticatedGet(t *testing.T) {
	h := newGRPCInteropHarness(t)
	h.svc.On("Get", mock.Anything, mock.MatchedBy(func(req *service.GetRequest) bool {
		return req.EntityType == "widget" && req.ID == 11
	})).Return(&service.GetResponse{ID: 11, Data: []byte(`{"n":1}`), Version: 3, Timestamp: 99, Found: true}, nil).Once()

	client := interopClient[pb.GetRequest, pb.GetResponse](h, pb.QuicBaaS_Get_FullMethodName)
	resp, err := client.CallUnary(context.Background(),
		bearerRequest(&pb.GetRequest{EntityType: "widget", Id: 11}, h.token(t, "kv:get")))
	require.NoError(t, err)

	assert.Equal(t, uint64(11), resp.Msg.GetId())
	assert.Equal(t, []byte(`{"n":1}`), resp.Msg.GetData())
	assert.Equal(t, uint64(3), resp.Msg.GetVersion())
	assert.True(t, resp.Msg.GetFound())
	h.svc.AssertExpectations(t)
}

func TestGRPCInterop_RefusedCallsCarryStandardStatus(t *testing.T) {
	h := newGRPCInteropHarness(t)
	client := interopClient[pb.GetRequest, pb.GetResponse](h, pb.QuicBaaS_Get_FullMethodName)
	foreign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	forged := &grpcInteropHarness{signer: foreign, now: h.now}

	cases := []struct {
		name  string
		token string
		want  connect.Code
	}{
		{"no token", "", connect.CodeUnauthenticated},
		{"token signed by a foreign key", forged.token(t, "kv:get"), connect.CodeUnauthenticated},
		{"token without the kv:get scope", h.token(t, "watch:watch"), connect.CodePermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.CallUnary(context.Background(),
				bearerRequest(&pb.GetRequest{EntityType: "widget", Id: 11}, tc.token))
			requireGRPCStatus(t, err, tc.want)
		})
	}
	h.svc.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
}

func TestGRPCInterop_UnknownMethodIsUnimplemented(t *testing.T) {
	h := newGRPCInteropHarness(t)
	for _, procedure := range []string{"/quicbaas.v1.QuicBaaS/NoSuchMethod", "/quicbaas.v1.NoSuchService/Health"} {
		t.Run(procedure, func(t *testing.T) {
			client := interopClient[pb.HealthRequest, pb.HealthResponse](h, procedure)
			_, err := client.CallUnary(context.Background(), bearerRequest(&pb.HealthRequest{}, h.token(t, "kv:get")))
			requireGRPCStatus(t, err, connect.CodeUnimplemented)
		})
	}
}

// TestGRPCInterop_WatchStreamEndsWithOKTrailer receives every message of a
// server stream, then the end of stream: connect reports a nil Err only when
// the trailers carry grpc-status 0.
func TestGRPCInterop_WatchStreamEndsWithOKTrailer(t *testing.T) {
	h := newGRPCInteropHarness(t)
	const events = 3
	h.svc.On("Watch", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			out := args.Get(2).(chan<- *service.WatchResponse)
			for i := int64(1); i <= events; i++ {
				out <- &service.WatchResponse{
					WatchID:  5,
					Revision: i,
					Events:   []*service.WatchEvent{{Type: service.WatchEventTypePut, Key: []byte("k"), Revision: i}},
				}
			}
			close(out)
		}).
		Return(int64(5), nil).Once()

	client := interopClient[pb.WatchRequest, pb.WatchResponse](h, pb.QuicBaaS_Watch_FullMethodName)
	stream, err := client.CallServerStream(context.Background(),
		bearerRequest(&pb.WatchRequest{Key: []byte("k")}, h.token(t, "watch:watch")))
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Close()) }()

	var revisions []int64
	for stream.Receive() {
		require.Len(t, stream.Msg().GetEvents(), 1)
		revisions = append(revisions, stream.Msg().GetRevision())
	}
	require.NoError(t, stream.Err(), "stream must end with grpc-status 0 in the trailers")
	assert.Equal(t, []int64{1, 2, 3}, revisions)
	h.svc.AssertExpectations(t)
}

func TestGRPCInterop_WatchStreamRefusedWithoutToken(t *testing.T) {
	h := newGRPCInteropHarness(t)
	client := interopClient[pb.WatchRequest, pb.WatchResponse](h, pb.QuicBaaS_Watch_FullMethodName)
	stream, err := client.CallServerStream(context.Background(), connect.NewRequest(&pb.WatchRequest{Key: []byte("k")}))
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Close()) }()

	assert.False(t, stream.Receive())
	requireGRPCStatus(t, stream.Err(), connect.CodeUnauthenticated)
	h.svc.AssertNotCalled(t, "Watch", mock.Anything, mock.Anything, mock.Anything)
}
