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

package orchestration

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// stubTokenSource answers every Token call with token and err.
type stubTokenSource struct {
	token *oauth2.Token
	err   error
}

func (s *stubTokenSource) Token(context.Context) (*oauth2.Token, error) {
	return s.token, s.err
}

// wedgedTokenSource is a token endpoint that never answers: Token returns only
// when its caller's context ends.
type wedgedTokenSource struct{}

func (wedgedTokenSource) Token(ctx context.Context) (*oauth2.Token, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// newTokenTestBackend builds a backend whose connection is never dialled: an
// RPC sent on it fails as a *RemoteBackendRPCError, which tells a test apart
// from one refused before sending.
func newTokenTestBackend(t *testing.T, ts RemoteTokenSource) *RemoteACPBackend {
	t.Helper()
	b, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: &FleetNode{
			ID:       "token-agent",
			TenantID: "token-tenant",
			Name:     "token-agent",
			Host:     "daemon.invalid:9443",
			Backend:  "acp",
			Kind:     FleetNodeKindDaemon,
		},
		Backend:     "acp",
		Logger:      &peerTestLogger{},
		TokenSource: ts,
		ConnDialer: func(_ context.Context, _ string) (*grpc.ClientConn, error) {
			return grpc.NewClient("passthrough:///daemon.invalid:9443",
				grpc.WithTransportCredentials(insecure.NewCredentials()))
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := b.CloseAll(); closeErr != nil {
			t.Errorf("CloseAll: %v", closeErr)
		}
	})
	return b
}

func TestRemoteACPBackend_AuthCtx_SetsBearerAndTenantMetadata(t *testing.T) {
	t.Parallel()
	b := newTokenTestBackend(t, &stubTokenSource{token: &oauth2.Token{AccessToken: "bearer-1"}})

	ctx, err := b.authCtx(context.Background())
	if err != nil {
		t.Fatalf("authCtx: %v", err)
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("authCtx attached no outgoing metadata")
	}
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer bearer-1" {
		t.Errorf("authorization = %v, want [Bearer bearer-1]", got)
	}
	if got := md.Get("x-quicbaas-tenant"); len(got) != 1 || got[0] != "token-tenant" {
		t.Errorf("x-quicbaas-tenant = %v, want [token-tenant]", got)
	}
}

func TestRemoteACPBackend_AuthCtx_NoTokenSourceSendsNoBearer(t *testing.T) {
	t.Parallel()
	b := newTokenTestBackend(t, nil)

	ctx, err := b.authCtx(context.Background())
	if err != nil {
		t.Fatalf("authCtx: %v", err)
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("authCtx attached no outgoing metadata")
	}
	if got := md.Get("authorization"); len(got) != 0 {
		t.Errorf("authorization = %v, want none", got)
	}
}

func TestRemoteACPBackend_AuthCtx_TokenFailureIsTypedError(t *testing.T) {
	t.Parallel()
	errEndpoint := errors.New("token endpoint refused")
	cases := []struct {
		name  string
		ts    RemoteTokenSource
		cause error
	}{
		{"source error", &stubTokenSource{err: errEndpoint}, errEndpoint},
		{"nil token", &stubTokenSource{}, ErrRemoteBackendEmptyToken},
		{"empty access token", &stubTokenSource{token: &oauth2.Token{}}, ErrRemoteBackendEmptyToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newTokenTestBackend(t, tc.ts)

			_, err := b.authCtx(context.Background())
			var tokenErr *RemoteBackendTokenError
			if !errors.As(err, &tokenErr) {
				t.Fatalf("authCtx error = %v, want *RemoteBackendTokenError", err)
			}
			if tokenErr.Host != "daemon.invalid:9443" {
				t.Errorf("Host = %q, want daemon.invalid:9443", tokenErr.Host)
			}
			if !errors.Is(err, ErrRemoteBackendToken) || !errors.Is(err, tc.cause) {
				t.Errorf("error %v does not match ErrRemoteBackendToken and %v", err, tc.cause)
			}
		})
	}
}

// A caller whose context ends while the token endpoint is wedged gets the
// typed token error back, and the RPC is never sent (go-quicbaas#193).
func TestRemoteACPBackend_CallerContextBoundsWedgedTokenFetch(t *testing.T) {
	t.Parallel()
	b := newTokenTestBackend(t, wedgedTokenSource{})

	calls := map[string]func(ctx context.Context) error{
		"Spawn": func(ctx context.Context) error {
			_, err := b.Spawn(ctx, &SpawnRequest{ProjectID: "p"})
			return err
		},
		"Prompt": func(ctx context.Context) error {
			_, err := b.Prompt(ctx, &PromptRequest{SessionID: "s", Prompt: "hi"})
			return err
		},
		"Status": func(ctx context.Context) error {
			_, err := b.Status(ctx, "s")
			return err
		},
		"Close": func(ctx context.Context) error { return b.Close(ctx, "s") },
	}
	for name, call := range calls {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := call(ctx)
		var rpcErr *RemoteBackendRPCError
		if errors.As(err, &rpcErr) {
			t.Errorf("%s sent the RPC without a token: %v", name, err)
		}
		if !errors.Is(err, ErrRemoteBackendToken) || !errors.Is(err, context.Canceled) {
			t.Errorf("%s error = %v, want ErrRemoteBackendToken wrapping context.Canceled", name, err)
		}
	}
}
