//go:build !integration

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

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newACMEPrincipalServer builds the ACME service against an embedded node
// that enforces authorization, the way a production node runs.
func newACMEPrincipalServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, _ := newPrincipalServer(t)
	cfg := &config.Config{}
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 8443
	cfg.Server.Host = "localhost"
	s.config = cfg
	s.logger = buildTestLogger(t)
	s.xkmsCA = newRealSoftwareCA(t)
	return s, s.acmeBaseURI()
}

// An ACME client registers an account on an authorization-enabled node: the
// account is persisted under quicbaas's scoped service identity, so the node
// authorizes the write and the client gets 201 Created.
func TestACMENewAccount_AuthorizationEnabledNodePersistsTheAccount(t *testing.T) {
	s, baseURI := newACMEPrincipalServer(t)
	adapter, err := newCAServiceAdapter(s.xkmsCA)
	require.NoError(t, err)
	acmeSvc, err := s.buildACMEService(adapter)
	require.NoError(t, err)
	require.NotNil(t, acmeSvc)
	rest := acmeSvc.RestServicer()
	require.NotNil(t, rest)

	nonceRec := httptest.NewRecorder()
	rest.NewNonceHandler(nonceRec, httptest.NewRequest(http.MethodHead, baseURI+"/acme/new-nonce", nil))
	nonce := nonceRec.Header().Get("Replay-Nonce")
	require.NotEmpty(t, nonce, "new-nonce status %d", nonceRec.Code)

	accountKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	accountURL := baseURI + "/acme/new-account"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: accountKey},
		&jose.SignerOptions{
			EmbedJWK: true,
			ExtraHeaders: map[jose.HeaderKey]interface{}{
				"nonce": nonce,
				"url":   accountURL,
			},
		})
	require.NoError(t, err)
	jws, err := signer.Sign([]byte(`{"termsOfServiceAgreed":true,"contact":["mailto:ops@example.com"]}`))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, accountURL, strings.NewReader(jws.FullSerialize()))
	req.Header.Set("Content-Type", "application/jose+json")
	rec := httptest.NewRecorder()
	rest.NewAccountHandler(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "new-account body: %s", rec.Body.String())
}
