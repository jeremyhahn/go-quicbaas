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

package iam

import (
	"context"
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// clientAuthPaths runs every token-endpoint client authentication against p
// and returns each path's error by name.
func clientAuthPaths(p *OIDCProvider, clientID, secret string) map[string]error {
	ctx := context.Background()
	out := map[string]error{}
	_, out["client_credentials"] = p.handleClientCredentialsGrant(ctx, &TokenRequest{
		GrantType: GrantTypeClientCredentials, ClientID: clientID, ClientSecret: secret,
	})
	_, out["authorization_code"] = p.handleAuthorizationCodeGrant(ctx, &TokenRequest{
		ClientID: clientID, ClientSecret: secret, Code: "code", RedirectURI: "https://app.example/cb",
	})
	_, out["refresh_token"] = p.handleRefreshTokenGrant(ctx, &TokenRequest{
		ClientID: clientID, ClientSecret: secret, RefreshToken: "rt",
	})
	out["AuthenticateClient"] = p.AuthenticateClient(ctx, clientID, secret)
	return out
}

func assertOAuthCodes(t *testing.T, errs map[string]error, want string) {
	t.Helper()
	for path, err := range errs {
		var oerr *OAuthError
		if !errors.As(err, &oerr) {
			t.Errorf("%s: error = %v, want *OAuthError", path, err)
			continue
		}
		if oerr.ErrorCode != want {
			t.Errorf("%s: error code = %q, want %q", path, oerr.ErrorCode, want)
		}
	}
}

func TestTokenEndpoint_ClientStoreOutageIsServerError(t *testing.T) {
	var kv interfaces.KVStore = &failingKVStore{
		KVStore: newMockKVStore(),
		err:     &dberrors.QBError{Code: dberrors.ErrInternal, Op: "mock.get"},
	}
	p := &OIDCProvider{appStorage: NewBackendStorage(kv)}
	assertOAuthCodes(t, clientAuthPaths(p, "app_t1_any", "sk_guess"), ErrOAuthServerError)
}

func TestTokenEndpoint_ClientStoreUnknownClientStaysInvalidClient(t *testing.T) {
	p := &OIDCProvider{appStorage: NewBackendStorage(newMockKVStore())}
	assertOAuthCodes(t, clientAuthPaths(p, "app_t1_absent", "sk_guess"), ErrOAuthInvalidClient)
}
