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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry"
)

const (
	pusherID = "spiffe://example.org/ns/build/sa/pusher"
	otherID  = "spiffe://example.org/ns/build/sa/other"
)

// gateAttestor accepts any quote that carries a nonce, unless fail is set.
type gateAttestor struct{ fail bool }

func (a *gateAttestor) VerifyQuote(_ context.Context, _, _, _, nonce []byte) (map[int][]byte, error) {
	if a.fail {
		return nil, errors.New("quote signature does not verify")
	}
	if len(nonce) == 0 {
		return nil, errors.New("quote carries no nonce")
	}
	return map[int][]byte{0: make([]byte, 32)}, nil
}

func (a *gateAttestor) GenerateNonce(context.Context) ([]byte, error) {
	return make([]byte, 32), nil
}

// allowPolicy allows every request.
type allowPolicy struct{}

func (allowPolicy) Evaluate(context.Context, string, map[string]any) (*ociregistry.PolicyDecision, error) {
	return &ociregistry.PolicyDecision{Allow: true}, nil
}

// memSessions is an in-memory session store.
type memSessions struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memSessions) Put(_ context.Context, entityType, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[entityType+"/"+key] = value
	return nil
}

func (s *memSessions) Get(_ context.Context, entityType, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[entityType+"/"+key]
	return v, ok, nil
}

func (s *memSessions) Delete(_ context.Context, entityType, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, entityType+"/"+key)
	return nil
}

func newGateAuth(t *testing.T, attestor *gateAttestor) *ociregistry.RegistryAuthHandler {
	t.Helper()
	h, err := ociregistry.NewRegistryAuthHandler(attestor, allowPolicy{}, &memSessions{})
	require.NoError(t, err)
	return h
}

// gateRouter mounts the challenge route and the distribution surface
// behind gates built from cfg.
func gateRouter(t *testing.T, auth *ociregistry.RegistryAuthHandler, cfg ociregistry.RegistryAttestationConfig) *mux.Router {
	t.Helper()
	gates, err := NewAttestedGates(VerifiedPeerIdentity, auth, cfg)
	require.NoError(t, err)
	root := mux.NewRouter()
	if auth != nil {
		require.NoError(t, RegisterAttestationChallengeRoute(root, VerifiedPeerIdentity, auth))
	}
	require.NoError(t, RegisterOCIDistRoutes(root, slog.Default(), newFakeOCIBackend(), gates, OCIDistLimits{}))
	return root
}

// peerRequest is a request whose connection presented a verified client
// certificate with uri as its URI SAN.
func peerRequest(t *testing.T, method, target, uri string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	leaf := &x509.Certificate{}
	if uri != "" {
		u, err := url.Parse(uri)
		require.NoError(t, err)
		leaf.URIs = []*url.URL{u}
	}
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}}
	return req
}

func encodeEvidence(t *testing.T, clientID string, nonce []byte) string {
	t.Helper()
	data, err := json.Marshal(ociregistry.AttestationEvidence{
		Quote:     []byte("quote"),
		Signature: []byte("signature"),
		AIK:       []byte("aik"),
		ClientID:  clientID,
		PCRs:      map[int][]byte{0: make([]byte, 32)},
		Nonce:     nonce,
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(data)
}

// evidenceFor issues a challenge to clientID and returns evidence over it.
func evidenceFor(t *testing.T, h *ociregistry.RegistryAuthHandler, clientID string) string {
	t.Helper()
	nonce, err := h.IssueChallenge(clientID)
	require.NoError(t, err)
	return encodeEvidence(t, clientID, nonce)
}

func serve(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func requireOCIError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	require.Equal(t, code, body.Errors[0].Code)
}

// attest runs a GET /v2/ with fresh evidence for pusherID and returns the
// session ID it opened.
func attest(t *testing.T, router http.Handler, h *ociregistry.RegistryAuthHandler) string {
	t.Helper()
	req := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	req.Header.Set(ociregistry.HeaderAttestationEvidence, evidenceFor(t, h, pusherID))
	rec := serve(router, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	sid := rec.Header().Get(HeaderRegistrySession)
	require.NotEmpty(t, sid)
	return sid
}

func TestAttestedGates_NoIdentityIs401(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())

	rec := serve(router, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	requireOCIError(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")

	rec = serve(router, httptest.NewRequest(http.MethodPut, "/v2/repo/manifests/latest", nil))
	requireOCIError(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestAttestedGates_UnverifiedOrNoURIIs401(t *testing.T) {
	router := gateRouter(t, newGateAuth(t, &gateAttestor{}), ociregistry.DefaultAttestationConfig())

	unverified := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	unverified.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
	requireOCIError(t, serve(router, unverified), http.StatusUnauthorized, "UNAUTHORIZED")

	noURI := peerRequest(t, http.MethodGet, "/v2/", "")
	requireOCIError(t, serve(router, noURI), http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestVerifiedPeerIdentity_FirstURISAN(t *testing.T) {
	id, ok := VerifiedPeerIdentity(peerRequest(t, http.MethodGet, "/v2/", pusherID))
	require.True(t, ok)
	require.Equal(t, pusherID, id)

	id, ok = VerifiedPeerIdentity(httptest.NewRequest(http.MethodGet, "/v2/", nil))
	require.False(t, ok)
	require.Empty(t, id)

	unverified := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	u, err := url.Parse(pusherID)
	require.NoError(t, err)
	unverified.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{u}}}}
	id, ok = VerifiedPeerIdentity(unverified)
	require.False(t, ok)
	require.Empty(t, id)

	id, ok = VerifiedPeerIdentity(peerRequest(t, http.MethodGet, "/v2/", ""))
	require.False(t, ok)
	require.Empty(t, id)
}

func TestAttestedGates_UnattestedIs403ByDefault(t *testing.T) {
	router := gateRouter(t, newGateAuth(t, &gateAttestor{}), ociregistry.DefaultAttestationConfig())
	rec := serve(router, peerRequest(t, http.MethodGet, "/v2/", pusherID))
	requireOCIError(t, rec, http.StatusForbidden, "DENIED")
}

func TestAttestedGates_AttestedIs200WithSession(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())
	attest(t, router, auth)
}

func TestAttestedGates_SessionFromSameIdentityIs200(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())
	sid := attest(t, router, auth)

	req := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	req.Header.Set(HeaderRegistrySession, sid)
	rec := serve(router, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestAttestedGates_SessionFromOtherIdentityIs403(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())
	sid := attest(t, router, auth)

	req := peerRequest(t, http.MethodGet, "/v2/", otherID)
	req.Header.Set(HeaderRegistrySession, sid)
	requireOCIError(t, serve(router, req), http.StatusForbidden, "DENIED")
}

func TestAttestedGates_EvidenceForOtherIdentityIs403(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())

	req := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	req.Header.Set(ociregistry.HeaderAttestationEvidence, evidenceFor(t, auth, otherID))
	rec := serve(router, req)
	requireOCIError(t, rec, http.StatusForbidden, "DENIED")
	require.Empty(t, rec.Header().Get(HeaderRegistrySession))
}

func TestAttestedGates_ReplayedEvidenceIs403(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())
	evidence := evidenceFor(t, auth, pusherID)

	first := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	first.Header.Set(ociregistry.HeaderAttestationEvidence, evidence)
	require.Equal(t, http.StatusOK, serve(router, first).Code)

	second := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	second.Header.Set(ociregistry.HeaderAttestationEvidence, evidence)
	requireOCIError(t, serve(router, second), http.StatusForbidden, "DENIED")
}

func TestAttestedGates_AttestorFailureIs403(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{fail: true})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())

	req := peerRequest(t, http.MethodGet, "/v2/", pusherID)
	req.Header.Set(ociregistry.HeaderAttestationEvidence, evidenceFor(t, auth, pusherID))
	rec := serve(router, req)
	requireOCIError(t, rec, http.StatusForbidden, "DENIED")
	require.Empty(t, rec.Header().Get(HeaderRegistrySession))
}

func TestAttestedGates_AttestationOffNeedsIdentityOnly(t *testing.T) {
	router := gateRouter(t, nil, ociregistry.RegistryAttestationConfig{RequireAttestation: false})

	rec := serve(router, peerRequest(t, http.MethodGet, "/v2/", pusherID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = serve(router, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	requireOCIError(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestAttestedGates_RefusesMissingArguments(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})

	_, err := NewAttestedGates(nil, auth, ociregistry.DefaultAttestationConfig())
	var noIdentity *IdentityRequiredError
	require.ErrorAs(t, err, &noIdentity)

	_, err = NewAttestedGates(VerifiedPeerIdentity, nil, ociregistry.DefaultAttestationConfig())
	var noHandler *AttestationHandlerRequiredError
	require.ErrorAs(t, err, &noHandler)

	root := mux.NewRouter()
	require.ErrorAs(t, RegisterAttestationChallengeRoute(root, nil, auth), &noIdentity)
	require.ErrorAs(t, RegisterAttestationChallengeRoute(root, VerifiedPeerIdentity, nil), &noHandler)
}

func TestAttestationChallengeRoute_IssuesNonceForIdentity(t *testing.T) {
	auth := newGateAuth(t, &gateAttestor{})
	router := gateRouter(t, auth, ociregistry.DefaultAttestationConfig())

	rec := serve(router, httptest.NewRequest(http.MethodPost, "/v2/_attestation/challenge", nil))
	requireOCIError(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")

	rec = serve(router, peerRequest(t, http.MethodPost, "/v2/_attestation/challenge", pusherID))
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
	rec = serve(router, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, rec.Header().Get(HeaderRegistrySession))
}
