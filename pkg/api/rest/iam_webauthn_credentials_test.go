//go:build rest

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

package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// WebAuthn credential administration over REST (go-quicbaas#574)
//
// The transport's job on this surface is to carry a base64url credential id
// from the listing's JSON body, through a URL path segment, to the credential
// service unchanged. go-quicbaas#574 was reported as a per-id delete that 404s
// for a credential the listing had just returned, which is precisely what a
// mangled path segment, a drifted {credential_id} param name, or a
// trailing-slash sibling route shadowing the per-id route would look like from
// outside. These tests mount the PRODUCTION route patterns and handlers and
// drive real HTTP requests through them, so all three failure modes are
// covered by the round trip rather than by inspection.
// =============================================================================

// credentialStoreDouble is an in-memory credential administration service
// keyed the way the wire is: raw credential id bytes in, canonical unpadded
// base64url out. It is deliberately independent of pkg/iam's encoder — it
// spells the contract a CLIENT sees — so a divergence between what the listing
// publishes and what the delete accepts fails the round trip instead of
// cancelling out.
type credentialStoreDouble struct {
	// credentials holds the raw credential ids still registered.
	credentials [][]byte

	// deleteArgs records the credential id string each per-id delete was
	// called with, so a test can prove the path segment arrived verbatim.
	deleteArgs []string

	// listErr, when set, fails the list verb.
	listErr error
}

// ListCredentials implements service.WebAuthnCredentialService.
func (d *credentialStoreDouble) ListCredentials(
	_ context.Context, _, _ string,
) ([]iam.WebAuthnCredentialSummary, error) {
	if d.listErr != nil {
		return nil, d.listErr
	}
	out := make([]iam.WebAuthnCredentialSummary, 0, len(d.credentials))
	for _, raw := range d.credentials {
		out = append(out, iam.WebAuthnCredentialSummary{
			ID:              base64.RawURLEncoding.EncodeToString(raw),
			AttestationType: "none",
		})
	}
	return out, nil
}

// DeleteCredential implements service.WebAuthnCredentialService.
func (d *credentialStoreDouble) DeleteCredential(
	_ context.Context, _, _, credentialID string,
) error {
	d.deleteArgs = append(d.deleteArgs, credentialID)
	raw, err := base64.RawURLEncoding.DecodeString(credentialID)
	if err != nil {
		return &iam.WebAuthnCredentialIDError{CredentialID: credentialID, Cause: err}
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	for i, held := range d.credentials {
		if base64.RawURLEncoding.EncodeToString(held) == encoded {
			d.credentials = append(d.credentials[:i], d.credentials[i+1:]...)
			return nil
		}
	}
	for _, held := range d.credentials {
		full := base64.RawURLEncoding.EncodeToString(held)
		if len(encoded) < len(full) && strings.HasPrefix(full, encoded) {
			return &iam.WebAuthnCredentialTruncatedIDError{
				CredentialID: credentialID,
				Matches:      []string{full},
			}
		}
	}
	return &iam.WebAuthnCredentialNotFoundError{
		Email: "alice@example.com", CredentialID: credentialID,
	}
}

// DeleteCredentials implements service.WebAuthnCredentialService.
func (d *credentialStoreDouble) DeleteCredentials(_ context.Context, _, _ string) (int, error) {
	n := len(d.credentials)
	d.credentials = nil
	return n, nil
}

// credentialAdminService is the server-side service graph the credential
// handlers resolve through: the WebAuthnCredentials() provider assertion.
type credentialAdminService struct {
	*testutil.MockQuicBaaS
	credentials service.WebAuthnCredentialService
}

// WebAuthnCredentials implements the provider interface
// resolveWebAuthnCredentialSubject asserts for.
func (s *credentialAdminService) WebAuthnCredentials() service.WebAuthnCredentialService {
	return s.credentials
}

// mountCredentialRoutes builds a router carrying the PRODUCTION route patterns
// and handlers for the three credential verbs, so the test exercises route
// registration (pattern, param name, trailing-slash siblings) and not only the
// handler function.
func mountCredentialRoutes(t *testing.T, credentials service.WebAuthnCredentialService) http.Handler {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mock := testutil.NewMockQuicBaaS()
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mock, cfg, log)
	require.NoError(t, err)
	server.service = &credentialAdminService{MockQuicBaaS: mock, credentials: credentials}

	const marker = "/webauthn/credentials"
	r := chi.NewRouter()
	mounted := 0
	for _, spec := range Routes() {
		if !strings.Contains(spec.Pattern, marker) {
			continue
		}
		r.Method(spec.Method, spec.Pattern, server.BindRoute(spec))
		mounted++
	}
	require.Equal(t, 3, mounted,
		"expected the list, per-id delete and bulk delete routes in the production table")
	return r
}

// credentialPath builds the tenant/user-scoped collection path the operator
// calls.
func credentialPath(suffix string) string {
	return "/api/v1/tenants/acme/users/89482037924204544/webauthn/credentials/" + suffix
}

// doCredentialRequest issues one request against the mounted routes.
func doCredentialRequest(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// listCredentialIDs calls the list verb and returns the ids it published.
func listCredentialIDs(t *testing.T, h http.Handler) []string {
	t.Helper()
	rec := doCredentialRequest(h, http.MethodGet, credentialPath(""))
	require.Equal(t, http.StatusOK, rec.Code, "list body: %s", rec.Body.String())

	var body struct {
		Credentials []iam.WebAuthnCredentialSummary `json:"credentials"`
		Total       int                             `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, len(body.Credentials), body.Total, "total must match the payload")

	ids := make([]string, 0, len(body.Credentials))
	for _, c := range body.Credentials {
		ids = append(ids, c.ID)
	}
	return ids
}

// testCredentialIDs are raw credential ids whose canonical base64url spellings
// exercise the characters a URL path could mangle: the base64url alphabet's
// '-' and '_', which are the two bytes a std-base64 encoder would have written
// as '+' and '/' instead.
func testCredentialIDs() [][]byte {
	return [][]byte{
		{0x80, 0x36, 0xfe, 0x36, 0xb8, 0x3b, 0xce, 0xeb, 0xfb, 0x57, 0x37, 0xdf,
			0x5a, 0x32, 0x25, 0xe0, 0x8f, 0x85, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66,
			0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		{0xff, 0xef, 0xbf, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
			0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x02, 0x03, 0x04, 0x05,
			0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d},
	}
}

// TestWebAuthnCredentials_ListThenDeleteEachListedID is the headline #574
// regression guard: every id the list verb publishes must be accepted verbatim
// by DELETE .../credentials/{credential_id}, and removing them all must leave
// the listing empty. The reported bug was exactly this loop failing with a 404
// on the first id.
func TestWebAuthnCredentials_ListThenDeleteEachListedID(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	ids := listCredentialIDs(t, h)
	require.Len(t, ids, 2)
	for _, id := range ids {
		require.Contains(t, id, "-", "fixture must exercise the base64url alphabet")
	}

	for _, id := range ids {
		rec := doCredentialRequest(h, http.MethodDelete, credentialPath(id))
		require.Equalf(t, http.StatusNoContent, rec.Code,
			"delete %q body: %s", id, rec.Body.String())
	}

	require.Empty(t, listCredentialIDs(t, h), "deleting every listed id must empty the listing")
	require.Equal(t, ids, store.deleteArgs,
		"the path segment must reach the service byte-identical to the published id")
}

// TestWebAuthnCredentials_PerIDDeleteIsNotShadowedByTheBulkRoute pins the route
// registration itself: the trailing-slash collection route and the per-id route
// are siblings, and a request naming an id must reach the per-id handler. If
// the bulk route ever shadowed it, this deletes every credential and the
// remaining count catches it.
func TestWebAuthnCredentials_PerIDDeleteIsNotShadowedByTheBulkRoute(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	ids := listCredentialIDs(t, h)
	require.Len(t, ids, 2)

	rec := doCredentialRequest(h, http.MethodDelete, credentialPath(ids[0]))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	require.Equal(t, []string{ids[1]}, listCredentialIDs(t, h),
		"a per-id delete removed more than the credential it named")
	require.Len(t, store.deleteArgs, 1, "the request did not reach the per-id handler")
}

// TestWebAuthnCredentials_BulkDeleteRemovesEverything pins the operator
// recovery verb the per-id fix must leave untouched.
func TestWebAuthnCredentials_BulkDeleteRemovesEverything(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	rec := doCredentialRequest(h, http.MethodDelete, credentialPath(""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body webAuthnCredentialsDeletedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Deleted)
	require.Empty(t, listCredentialIDs(t, h))
	require.Empty(t, store.deleteArgs, "the bulk verb must not route through the per-id handler")
}

// TestWebAuthnCredentials_UnknownIDIs404 covers the genuine not-found: a
// well-formed id nobody holds is a 404 carrying the typed error's message.
func TestWebAuthnCredentials_UnknownIDIs404(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	unknown := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	rec := doCredentialRequest(h, http.MethodDelete, credentialPath(unknown))

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "no credential "+unknown+" is registered")
	require.Len(t, store.credentials, 2, "a failed delete removed something")
}

// TestWebAuthnCredentials_TruncatedIDIs400NamingTheFullID is the #574
// diagnosis on the wire: a fragment of an id the account holds is a 400 that
// names the full id, so the operator's next command is the correct one instead
// of another guess at an encoding.
func TestWebAuthnCredentials_TruncatedIDIs400NamingTheFullID(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	ids := listCredentialIDs(t, h)
	fragment := ids[0][:24]
	_, decErr := base64.RawURLEncoding.DecodeString(fragment)
	require.NoError(t, decErr, "the fragment must decode, or it is not the reported failure")

	rec := doCredentialRequest(h, http.MethodDelete, credentialPath(fragment))

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "truncated prefix")
	require.Contains(t, rec.Body.String(), ids[0])
	require.Len(t, store.credentials, 2, "a rejected delete removed something")
}

// TestWebAuthnCredentials_MalformedIDIs400 covers the other bad id: a value
// that is not base64url at all.
func TestWebAuthnCredentials_MalformedIDIs400(t *testing.T) {
	store := &credentialStoreDouble{credentials: testCredentialIDs()}
	h := mountCredentialRoutes(t, store)

	rec := doCredentialRequest(h, http.MethodDelete, credentialPath("not%20base64%21"))

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "not valid base64url")
}

// TestWebAuthnCredentials_ListFailureIsAServiceError pins the fall-through: an
// error that is neither of the credential-id shapes is reported as a service
// failure rather than being mapped to a 404 that hides it.
func TestWebAuthnCredentials_ListFailureIsAServiceError(t *testing.T) {
	store := &credentialStoreDouble{listErr: service.ErrWebAuthnCredentialNotConfigured}
	h := mountCredentialRoutes(t, store)

	rec := doCredentialRequest(h, http.MethodGet, credentialPath(""))

	require.NotEqual(t, http.StatusOK, rec.Code)
	require.NotEqual(t, http.StatusNotFound, rec.Code,
		"an unconfigured WebAuthn stack must not read as a missing credential")
}

// TestWebAuthnCredentials_MissingProviderIs503 covers the handler's other
// error path: a server whose service graph carries no credential
// administration surface reports the capability unavailable rather than
// panicking or 404ing.
func TestWebAuthnCredentials_MissingProviderIs503(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mock := testutil.NewMockQuicBaaS()
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mock, cfg, log)
	require.NoError(t, err)
	server.service = mock

	r := chi.NewRouter()
	for _, spec := range Routes() {
		if strings.Contains(spec.Pattern, "/webauthn/credentials") {
			r.Method(spec.Method, spec.Pattern, server.BindRoute(spec))
		}
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, credentialPath("")},
		{http.MethodDelete, credentialPath("")},
		{http.MethodDelete, credentialPath(base64.RawURLEncoding.EncodeToString(make([]byte, 32)))},
	} {
		rec := doCredentialRequest(r, tc.method, tc.path)
		require.Equalf(t, http.StatusServiceUnavailable, rec.Code,
			"%s %s body: %s", tc.method, tc.path, rec.Body.String())
	}
}
