// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package distribution

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry"
)

// registryHandler composes the /v2/ surface with VerifiedPeerIdentity,
// the default attestation config and a fake backend.
func registryHandler(t *testing.T, auth *ociregistry.RegistryAuthHandler) http.Handler {
	t.Helper()
	h, err := NewRegistryHandler(RegistryHandlerConfig{
		Backend:     newFakeOCIBackend(),
		Identity:    VerifiedPeerIdentity,
		Auth:        auth,
		Attestation: ociregistry.DefaultAttestationConfig(),
	})
	require.NoError(t, err)
	return h
}

func TestRegistryHandler_NoIdentityIs401(t *testing.T) {
	h := registryHandler(t, newGateAuth(t, &gateAttestor{}))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	requireOCIError(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestRegistryHandler_UnattestedIs403ByDefault(t *testing.T) {
	h := registryHandler(t, newGateAuth(t, &gateAttestor{}))
	rec := serve(h, peerRequest(t, http.MethodGet, "/v2/", pusherID))
	requireOCIError(t, rec, http.StatusForbidden, "DENIED")
}

func TestRegistryHandler_ChallengeThenAttestedSession(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	h := registryHandler(t, auth)

	rec := serve(h, peerRequest(t, http.MethodPost, "/v2/_attestation/challenge", pusherID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Nonce string `json:"nonce"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	nonce, err := base64.StdEncoding.DecodeString(body.Nonce)
	require.NoError(t, err)
	require.NotEmpty(t, nonce)

	req := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	req.Header.Set(ociregistry.HeaderAttestationEvidence, encodeEvidence(t, pusherID, nonce))
	rec = serve(h, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	sid := rec.Header().Get(HeaderRegistrySession)
	require.NotEmpty(t, sid)

	req = peerRequest(t, http.MethodGet, "/v2/_catalog", pusherID)
	req.Header.Set(HeaderRegistrySession, sid)
	rec = serve(h, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestRegistryHandler_AttestationOffNeedsIdentityOnly(t *testing.T) {
	h, err := NewRegistryHandler(RegistryHandlerConfig{
		Backend:     newFakeOCIBackend(),
		Identity:    VerifiedPeerIdentity,
		Attestation: ociregistry.RegistryAttestationConfig{RequireAttestation: false},
	})
	require.NoError(t, err)

	rec := serve(h, peerRequest(t, http.MethodGet, "/v2/", pusherID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = serve(h, peerRequest(t, http.MethodPost, "/v2/_attestation/challenge", pusherID))
	require.NotEqual(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestRegistryHandler_UnderChiMountSeesFullPath(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	r := chi.NewRouter()
	r.Handle("/v2/*", registryHandler(t, auth))
	attest(t, r, auth)
}

func TestRegistryHandler_RefusesMissingConfig(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	cfg := ociregistry.DefaultAttestationConfig()

	_, err := NewRegistryHandler(RegistryHandlerConfig{Identity: VerifiedPeerIdentity, Auth: auth, Attestation: cfg})
	var noBackend *OCIDistBackendRequiredError
	require.ErrorAs(t, err, &noBackend)

	_, err = NewRegistryHandler(RegistryHandlerConfig{Backend: newFakeOCIBackend(), Auth: auth, Attestation: cfg})
	var noIdentity *IdentityRequiredError
	require.ErrorAs(t, err, &noIdentity)

	_, err = NewRegistryHandler(RegistryHandlerConfig{Backend: newFakeOCIBackend(), Identity: VerifiedPeerIdentity, Attestation: cfg})
	var noHandler *AttestationHandlerRequiredError
	require.ErrorAs(t, err, &noHandler)
}
