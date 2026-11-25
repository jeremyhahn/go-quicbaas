// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// barrierCallRecorder records, per unary RPC that reaches the server, the
// method name and the authorization metadata it carried.
type barrierCallRecorder struct {
	mu    sync.Mutex
	calls []barrierCall
}

type barrierCall struct {
	method string
	auth   []string
}

func (r *barrierCallRecorder) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	method := info.FullMethod[strings.LastIndex(info.FullMethod, "/")+1:]
	r.mu.Lock()
	r.calls = append(r.calls, barrierCall{method: method, auth: md.Get("authorization")})
	r.mu.Unlock()
	return handler(ctx, req)
}

func (r *barrierCallRecorder) snapshot() []barrierCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]barrierCall(nil), r.calls...)
}

// serveQRDBBarrierGRPC serves go-qrdb's gRPC service with no barrier servicer:
// the handlers refuse, but the interceptor sees each call's metadata first.
func serveQRDBBarrierGRPC(t *testing.T, lis net.Listener) *barrierCallRecorder {
	t.Helper()
	rec := &barrierCallRecorder{}
	srv := grpc.NewServer(grpc.UnaryInterceptor(rec.intercept))
	qrdbsdk.RegisterGRPCService(srv, nil, nil)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return rec
}

// TestBarrierStatus_GRPCAndUnix_SendPlatformBearer: over gRPC and unix,
// Security().Barrier() verbs carry the WithAuthToken bearer; with no token
// set, no authorization header is sent.
func TestBarrierStatus_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
	cases := []struct {
		name   string
		listen func(t *testing.T) (net.Listener, []Option)
	}{
		{"grpc", func(t *testing.T) (net.Listener, []Option) {
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			return lis, nil
		}},
		{"unix", func(t *testing.T) (net.Listener, []Option) {
			socket := filepath.Join(shortSocketDir(t), "qrdb.sock")
			lis, err := net.Listen("unix", socket)
			require.NoError(t, err)
			return lis, []Option{WithUnix(socket)}
		}},
	}
	for _, tc := range cases {
		for _, token := range []string{"t-2427", ""} {
			name := tc.name + "/token"
			if token == "" {
				name = tc.name + "/no-token"
			}
			t.Run(name, func(t *testing.T) {
				lis, extra := tc.listen(t)
				rec := serveQRDBBarrierGRPC(t, lis)

				applied := extra
				if token != "" {
					applied = append([]Option{WithAuthToken(token)}, extra...)
				}
				opts := defaultOptions()
				for _, apply := range applied {
					require.NoError(t, apply(opts))
				}
				c := &client{opts: opts, address: lis.Addr().String()}
				t.Cleanup(func() {
					if qc, err := c.qrdbClient(); err == nil {
						if err := qc.Close(); err != nil {
							t.Errorf("close qrdb client: %v", err)
						}
					}
				})

				bc, err := (&securityService{c: c}).Barrier()
				require.NoError(t, err)
				_, statusErr := bc.BarrierStatus(context.Background())
				require.Error(t, statusErr, "the fake server has no barrier servicer")
				_, regErr := bc.OfficerRegister(context.Background(), &qrdbtransport.OfficerRegisterRequest{})
				require.Error(t, regErr, "the fake server has no barrier servicer")

				var want []string
				if token != "" {
					want = []string{"Bearer " + token}
				}
				// go-qrdb's connect-time Health probe is not a barrier verb.
				var calls []barrierCall
				for _, call := range rec.snapshot() {
					if strings.HasPrefix(call.method, "Barrier") {
						calls = append(calls, call)
					}
				}
				require.Len(t, calls, 2)
				assert.Equal(t, "BarrierStatus", calls[0].method)
				assert.Equal(t, "BarrierOfficerRegister", calls[1].method)
				for _, call := range calls {
					assert.Equal(t, want, call.auth, call.method)
				}
			})
		}
	}
}

// fakeBarrierClient records the outgoing authorization metadata each verb
// receives, keyed by verb name.
type fakeBarrierClient struct {
	got map[string][]string
}

func (f *fakeBarrierClient) rec(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.got[verb] = md.Get("authorization")
}

func (f *fakeBarrierClient) BarrierInitialize(ctx context.Context, _ *qrdbtransport.BarrierInitializeRequest) error {
	f.rec(ctx, "BarrierInitialize")
	return nil
}

func (f *fakeBarrierClient) BarrierUnseal(ctx context.Context, _ *qrdbtransport.BarrierUnsealRequest) error {
	f.rec(ctx, "BarrierUnseal")
	return nil
}

func (f *fakeBarrierClient) BarrierSeal(ctx context.Context) error {
	f.rec(ctx, "BarrierSeal")
	return nil
}

func (f *fakeBarrierClient) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	f.rec(ctx, "BarrierStatus")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierInitializeShamir(ctx context.Context, _ *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	f.rec(ctx, "BarrierInitializeShamir")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierUnsealWithShare(ctx context.Context, _ *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	f.rec(ctx, "BarrierUnsealWithShare")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierUnsealWithShares(ctx context.Context, _ *qrdbtransport.BarrierUnsealSharesRequest) error {
	f.rec(ctx, "BarrierUnsealWithShares")
	return nil
}

func (f *fakeBarrierClient) BarrierRekey(ctx context.Context, _ *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	f.rec(ctx, "BarrierRekey")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierEnableEncryption(ctx context.Context, _ *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	f.rec(ctx, "BarrierEnableEncryption")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, _ *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	f.rec(ctx, "BarrierGenerateRecoveryKeys")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierRecoverWithKeys(ctx context.Context, _ *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	f.rec(ctx, "BarrierRecoverWithKeys")
	return nil
}

func (f *fakeBarrierClient) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	f.rec(ctx, "BarrierDeleteRecoveryKeys")
	return nil
}

func (f *fakeBarrierClient) BarrierHasRecoveryKeys(ctx context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	f.rec(ctx, "BarrierHasRecoveryKeys")
	return nil, nil
}

func (f *fakeBarrierClient) BarrierGenerateRootToken(ctx context.Context, _ *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	f.rec(ctx, "BarrierGenerateRootToken")
	return nil, nil
}

func (f *fakeBarrierClient) OfficerRegister(ctx context.Context, _ *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	f.rec(ctx, "OfficerRegister")
	return nil, nil
}

func (f *fakeBarrierClient) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	f.rec(ctx, "OfficerList")
	return nil, nil
}

func (f *fakeBarrierClient) OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	f.rec(ctx, "OfficerClaimStatus")
	return nil, nil
}

func (f *fakeBarrierClient) ShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.rec(ctx, "ShareClaimBegin")
	return nil, nil
}

func (f *fakeBarrierClient) ShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.rec(ctx, "ShareClaimComplete")
	return nil, nil
}

func (f *fakeBarrierClient) ShamirSealVote(ctx context.Context, _ *qrdbtransport.ShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	f.rec(ctx, "ShamirSealVote")
	return nil, nil
}

func (f *fakeBarrierClient) RecoveryOfficerRegister(ctx context.Context, _ *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	f.rec(ctx, "RecoveryOfficerRegister")
	return nil, nil
}

func (f *fakeBarrierClient) RecoveryShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.rec(ctx, "RecoveryShareClaimBegin")
	return nil, nil
}

func (f *fakeBarrierClient) RecoveryShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.rec(ctx, "RecoveryShareClaimComplete")
	return nil, nil
}

// TestQRDBBarrier_BearerClientForwardsEveryVerb: each of the 23 barrier verbs
// reaches the matching inner verb with the bearer attached exactly once.
func TestQRDBBarrier_BearerClientForwardsEveryVerb(t *testing.T) {
	fake := &fakeBarrierClient{got: map[string][]string{}}
	b := &qrdbBearerBarrierClient{inner: fake, bearer: "Bearer t-2427"}
	ctx := context.Background()

	require.NoError(t, b.BarrierInitialize(ctx, nil))
	require.NoError(t, b.BarrierUnseal(ctx, nil))
	require.NoError(t, b.BarrierSeal(ctx))
	_, err := b.BarrierStatus(ctx)
	require.NoError(t, err)
	_, err = b.BarrierInitializeShamir(ctx, nil)
	require.NoError(t, err)
	_, err = b.BarrierUnsealWithShare(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, b.BarrierUnsealWithShares(ctx, nil))
	_, err = b.BarrierRekey(ctx, nil)
	require.NoError(t, err)
	_, err = b.BarrierEnableEncryption(ctx, nil)
	require.NoError(t, err)
	_, err = b.BarrierGenerateRecoveryKeys(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, b.BarrierRecoverWithKeys(ctx, nil))
	require.NoError(t, b.BarrierDeleteRecoveryKeys(ctx))
	_, err = b.BarrierHasRecoveryKeys(ctx)
	require.NoError(t, err)
	_, err = b.BarrierGenerateRootToken(ctx, nil)
	require.NoError(t, err)
	_, err = b.OfficerRegister(ctx, nil)
	require.NoError(t, err)
	_, err = b.OfficerList(ctx)
	require.NoError(t, err)
	_, err = b.OfficerClaimStatus(ctx)
	require.NoError(t, err)
	_, err = b.ShareClaimBegin(ctx, nil)
	require.NoError(t, err)
	_, err = b.ShareClaimComplete(ctx, nil)
	require.NoError(t, err)
	_, err = b.ShamirSealVote(ctx, nil)
	require.NoError(t, err)
	_, err = b.RecoveryOfficerRegister(ctx, nil)
	require.NoError(t, err)
	_, err = b.RecoveryShareClaimBegin(ctx, nil)
	require.NoError(t, err)
	_, err = b.RecoveryShareClaimComplete(ctx, nil)
	require.NoError(t, err)

	require.Len(t, fake.got, 23)
	for verb, auth := range fake.got {
		assert.Equal(t, []string{"Bearer t-2427"}, auth, verb)
	}
}
