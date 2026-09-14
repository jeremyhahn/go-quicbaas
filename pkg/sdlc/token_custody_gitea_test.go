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

package sdlc

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =============================================================================
// Gitea token custody
// =============================================================================
//
// These tests assert WHICH CREDENTIAL each call authenticates with, not only
// that the calls succeed. That is the substance of the design: Gitea cannot
// mint a token from the token being replaced, so mint and revoke must use the
// provisioning credential, while verification must use the NEW token and
// nothing else. A test that only checked results would pass with every one of
// those wired to the wrong credential.

const (
	custodyUser     = "provisioning"
	custodyPassword = "provisioning-password"
	adapterToken    = "test-token"
)

// newCustodyFixture starts a stub server and an adapter configured with a
// custody credential.
func newCustodyFixture(t *testing.T) (*GiteaBackend, *giteaFixture) {
	t.Helper()
	fixture := &giteaFixture{t: t, handlers: map[string]func(http.ResponseWriter, *http.Request){}}

	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAllString(r)
		fixture.requests = append(fixture.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   body,
			Auth:   r.Header.Get("Authorization"),
		})

		key := r.Method + " " + r.URL.Path
		if handler, ok := fixture.handlers[key]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no fixture for ` + key + `"}`))
	}))
	t.Cleanup(fixture.server.Close)

	backend, err := NewGiteaBackend(GiteaConfig{
		BaseURL: fixture.server.URL + "/api/v1",
		Token:   adapterToken,
		Custody: CustodyCredential{Username: custodyUser, Password: custodyPassword},
	})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	return backend, fixture
}

// basicAuthHeader renders the header a correct custody call must send.
func basicAuthHeader(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// lastRequestTo returns the most recent recorded request for a method and path.
func lastRequestTo(t *testing.T, f *giteaFixture, method, path string) recordedRequest {
	t.Helper()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Method == method && f.requests[i].Path == path {
			return f.requests[i]
		}
	}
	t.Fatalf("no recorded %s %s in %d requests", method, path, len(f.requests))
	return recordedRequest{}
}

// TestGitea_TokenCapabilities_SelfServiceMintIsFalse pins the measured platform
// fact the rotation design rests on.
//
// If this ever flips to true it must be because Gitea changed, and the rotation
// job can then drop its dependency on a password-equivalent entirely. Encoding
// it as data is what lets that be a config observation rather than a rewrite.
func TestGitea_TokenCapabilities_SelfServiceMintIsFalse(t *testing.T) {
	backend, _ := newCustodyFixture(t)

	caps := backend.TokenCapabilities()
	if caps.SelfServiceMint {
		t.Error("Gitea cannot mint a token from the token being replaced; " +
			"its token endpoints need the user-level write scope or basic auth")
	}
	if !caps.ScopedTokens {
		t.Error("Gitea does scope tokens")
	}
	if !caps.RevokeByName {
		t.Error("Gitea revokes by name, which is what makes a crashed rotation recoverable")
	}
}

// TestGitea_MintToken_UsesTheCustodyCredential is the test that would catch the
// most tempting wrong implementation: minting with the adapter's own token.
//
// Against a real Gitea that fails with a 403 naming a scope, which is
// discoverable. Against a fixture it would pass silently, so the credential
// itself is asserted.
func TestGitea_MintToken_UsesTheCustodyCredential(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	fixture.respondJSON("POST /api/v1/users/agent-go-quicbaas/tokens", http.StatusCreated,
		map[string]any{"id": 7, "name": "rotating", "sha1": "the-new-secret"})

	minted, err := backend.MintToken(context.Background(), TokenSpec{
		Username: "agent-go-quicbaas",
		Name:     "rotating",
		Scopes:   []string{"read:user"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	defer minted.Secret.Destroy()
	if string(minted.Secret.Bytes()) != "the-new-secret" {
		t.Errorf("secret = %q, want the minted one", minted.Secret.Bytes())
	}
	if minted.ID != "7" || minted.Name != "rotating" {
		t.Errorf("minted = %+v, want id 7 named rotating", minted)
	}

	sent := lastRequestTo(t, fixture, "POST", "/api/v1/users/agent-go-quicbaas/tokens")
	if want := basicAuthHeader(custodyUser, custodyPassword); sent.Auth != want {
		t.Errorf("mint authenticated with %q, want the custody credential; "+
			"minting with the adapter token is what Gitea refuses", firstWord(sent.Auth))
	}
	if !strings.Contains(sent.Body, "read:user") {
		t.Errorf("scopes not sent: %s — without an identity scope the new token "+
			"cannot verify itself", sent.Body)
	}
}

// TestGitea_MintToken_WithoutCustodyRefusesLoudly proves the absent-credential
// case fails rather than degrading.
//
// A rotation job that quietly did nothing would let a credential drift past its
// rotation interval while the job reported success.
func TestGitea_MintToken_WithoutCustodyRefusesLoudly(t *testing.T) {
	backend, _ := newGiteaFixture(t) // no custody credential

	_, err := backend.MintToken(context.Background(), TokenSpec{
		Username: "agent-go-quicbaas",
		Name:     "rotating",
	})
	if !errors.Is(err, ErrCustodyCredentialAbsent) {
		t.Fatalf("err = %v, want ErrCustodyCredentialAbsent", err)
	}
}

// TestGitea_MintToken_RejectsAResponseWithNoSecret covers the cheerful-status
// case.
//
// A 201 carrying no sha1 is a failed mint however successful it looks: the
// caller would store an empty credential and discover it at the next
// authenticated call, long after the old token was revoked.
func TestGitea_MintToken_RejectsAResponseWithNoSecret(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	fixture.respondJSON("POST /api/v1/users/agent-go-quicbaas/tokens", http.StatusCreated,
		map[string]any{"id": 7, "name": "rotating"})

	_, err := backend.MintToken(context.Background(), TokenSpec{
		Username: "agent-go-quicbaas",
		Name:     "rotating",
	})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGitea_MintToken_RequiresAName guards the property that makes a crashed
// rotation recoverable.
func TestGitea_MintToken_RequiresAName(t *testing.T) {
	backend, _ := newCustodyFixture(t)

	_, err := backend.MintToken(context.Background(), TokenSpec{Username: "agent-go-quicbaas"})
	if err == nil {
		t.Fatal("an unnamed token cannot be found again to revoke; it must be refused")
	}
}

// TestGitea_VerifyToken_AuthenticatesAsTheTokenUnderTest is the safety property
// of the whole rotation design.
//
// Verification exists to prove the REPLACEMENT works before the original is
// destroyed. Checking with the custody credential, or with the adapter's own
// token, would succeed against a replacement that is broken — and rotation
// would then revoke a working credential in favour of a dead one.
func TestGitea_VerifyToken_AuthenticatesAsTheTokenUnderTest(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	fixture.respondJSON("GET /api/v1/user", http.StatusOK,
		map[string]any{"login": "agent-go-quicbaas"})

	identity, err := backend.VerifyToken(context.Background(), []byte("the-new-secret"))
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if identity.Username != "agent-go-quicbaas" {
		t.Errorf("username = %q", identity.Username)
	}

	sent := lastRequestTo(t, fixture, "GET", "/api/v1/user")
	if sent.Auth != "token the-new-secret" {
		t.Errorf("verification authenticated with %q, not the token under test; "+
			"that proves the OTHER credential works, which is not the question",
			firstWord(sent.Auth))
	}
}

// TestGitea_RevokeToken_ReadsBackAndCatchesASilentFailure covers the direction
// of this error that actually hurts.
//
// Gitea has returned success for writes that did not take effect. A revocation
// that silently did not happen leaves a LIVE credential that everything
// downstream believes is destroyed — so read-back is not ceremony here.
func TestGitea_RevokeToken_ReadsBackAndCatchesASilentFailure(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	fixture.respondJSON("DELETE /api/v1/users/agent-go-quicbaas/tokens/stale", http.StatusNoContent, nil)
	// The server claims success and still lists the token.
	fixture.respondJSON("GET /api/v1/users/agent-go-quicbaas/tokens", http.StatusOK,
		[]map[string]any{{"id": 3, "name": "stale"}})

	err := backend.RevokeToken(context.Background(), "agent-go-quicbaas", "stale")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("err = %v, want ErrForgeWriteNotApplied: a token still listed "+
			"after a successful delete is still live", err)
	}
}

// TestGitea_RevokeToken_SucceedsWhenTheTokenIsGone is the other side, so the
// read-back cannot pass by always failing.
func TestGitea_RevokeToken_SucceedsWhenTheTokenIsGone(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	fixture.respondJSON("DELETE /api/v1/users/agent-go-quicbaas/tokens/stale", http.StatusNoContent, nil)
	fixture.respondJSON("GET /api/v1/users/agent-go-quicbaas/tokens", http.StatusOK,
		[]map[string]any{{"id": 9, "name": "current"}})

	if err := backend.RevokeToken(context.Background(), "agent-go-quicbaas", "stale"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	sent := lastRequestTo(t, fixture, "DELETE", "/api/v1/users/agent-go-quicbaas/tokens/stale")
	if want := basicAuthHeader(custodyUser, custodyPassword); sent.Auth != want {
		t.Errorf("revoke authenticated with %q, want the custody credential", firstWord(sent.Auth))
	}
}

// firstWord returns the scheme of an Authorization header, so a failure message
// can say which credential was used without printing the credential.
func firstWord(header string) string {
	if header == "" {
		return "(none)"
	}
	if i := strings.IndexByte(header, ' '); i > 0 {
		return header[:i] + " …"
	}
	return "(malformed)"
}

// TestGitea_ErrorBodyNeverCarriesACredential is the regression for a review
// finding.
//
// Gitea's message is relayed to operators and stored. A server or a proxy in
// front of it can echo the Authorization header or the token back in its
// response body, and that body becomes the Detail/Message of a typed error.
// Redaction happens where all three credentials are held, because anywhere
// further out would be guessing at what a secret looks like.
func TestGitea_ErrorBodyNeverCarriesACredential(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	// A server that echoes what it was sent — the case being defended against.
	fixture.handlers["POST /api/v1/users/agent/tokens"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"rejected credential ` + custodyPassword +
			` for ` + adapterToken + `"}`))
	}

	_, err := backend.MintToken(context.Background(), TokenSpec{Username: "agent", Name: "t"})
	if err == nil {
		t.Fatal("a 403 must fail")
	}

	rendered := err.Error()
	if strings.Contains(rendered, custodyPassword) {
		t.Errorf("the custody password reached a typed error: %s", rendered)
	}
	if strings.Contains(rendered, adapterToken) {
		t.Errorf("the adapter token reached a typed error: %s", rendered)
	}
	if !strings.Contains(rendered, "[redacted]") {
		t.Errorf("nothing was redacted, so the echo was not detected: %s", rendered)
	}
}

// TestRawJSONStringBytes pins the decode boundary of the (a)-lite mint
// extension: the secret leaves as owned bytes and the raw JSON copy is
// zeroed, on the unescaped fast path and the escaped fallback alike.
func TestRawJSONStringBytes(t *testing.T) {
	t.Run("UnescapedFastPathWipesRaw", func(t *testing.T) {
		raw := []byte(`"deadbeefcafe"`)
		out, err := rawJSONStringBytes(raw)
		if err != nil {
			t.Fatalf("rawJSONStringBytes: %v", err)
		}
		if string(out) != "deadbeefcafe" {
			t.Fatalf("extracted %q", out)
		}
		for i, b := range raw {
			if b != 0 {
				t.Fatalf("byte %d of the raw JSON copy survived extraction", i)
			}
		}
	})
	t.Run("EscapedFallbackStillDecodesAndWipesRaw", func(t *testing.T) {
		raw := []byte(`"with\"escape"`)
		out, err := rawJSONStringBytes(raw)
		if err != nil {
			t.Fatalf("rawJSONStringBytes: %v", err)
		}
		if string(out) != `with"escape` {
			t.Fatalf("extracted %q", out)
		}
		for i, b := range raw {
			if b != 0 {
				t.Fatalf("byte %d of the raw JSON copy survived the fallback", i)
			}
		}
	})
	t.Run("AbsentIsNil", func(t *testing.T) {
		if out, err := rawJSONStringBytes(nil); err != nil || out != nil {
			t.Fatalf("absent = (%q, %v), want (nil, nil)", out, err)
		}
	})
}

// TestGitea_ResponseBodyIsWipedOnEveryExit is review 991's first finding:
// the response body is adapter-owned secret-bearing memory (a mint response
// carries the token verbatim), and do wipes it on success and error exits
// alike. The seam observes the buffer AFTER the deferred wipe.
func TestGitea_ResponseBodyIsWipedOnEveryExit(t *testing.T) {
	backend, fixture := newCustodyFixture(t)

	var observed [][]byte
	backend.bodySeam = func(body []byte) { observed = append(observed, body) }

	// Success exit: the mint response held the secret.
	fixture.respondJSON("POST /api/v1/users/agent-go-quicbaas/tokens", http.StatusCreated,
		map[string]any{"id": 7, "name": "rotating", "sha1": "the-new-secret"})
	minted, err := backend.MintToken(context.Background(), TokenSpec{
		Username: "agent-go-quicbaas", Name: "rotating", Scopes: []string{"read:user"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	defer minted.Secret.Destroy()

	// Error exit: a body that echoes credential-looking content.
	fixture.respondJSON("GET /api/v1/user", http.StatusForbidden,
		map[string]any{"message": "bad token the-new-secret"})
	if _, err := backend.VerifyToken(context.Background(), []byte("the-new-secret")); err == nil {
		t.Fatal("a 403 verification reported success")
	}

	if len(observed) < 2 {
		t.Fatalf("the seam observed %d bodies, want both exits", len(observed))
	}
	for n, body := range observed {
		for i, b := range body {
			if b != 0 {
				t.Fatalf("byte %d of response body %d survived do's exit: "+
					"the adapter left secret-bearing memory unwiped", i, n)
			}
		}
	}
}

// TestGitea_ScrubDoesNotAliasTheRawBody pins scrubCredentials' ownership
// contract: the returned error-safe copy never aliases the raw body, so the
// caller's wipe of the raw buffer cannot hollow out the error message — and
// the scrub still redacts every credential class byte-wise.
func TestGitea_ScrubDoesNotAliasTheRawBody(t *testing.T) {
	backend, _ := newCustodyFixture(t)

	subject := []byte("subject-secret")
	raw := []byte(`{"message":"echo subject-secret and more"}`)
	scrubbed := backend.scrubCredentials(raw, subject)

	if bytes.Contains(scrubbed, subject) {
		t.Fatalf("the subject token survived the scrub: %s", scrubbed)
	}
	// The caller wipes the raw buffer; the scrubbed copy must be unaffected.
	for i := range raw {
		raw[i] = 0
	}
	if !bytes.Contains(scrubbed, []byte("[redacted]")) {
		t.Fatalf("scrubbed message hollowed out by the raw wipe: %q", scrubbed)
	}

	// The no-replacement path must also return an independent copy.
	clean := []byte(`{"message":"nothing secret here"}`)
	out := backend.scrubCredentials(clean, []byte("absent-token"))
	for i := range clean {
		clean[i] = 0
	}
	if !bytes.Contains(out, []byte("nothing secret here")) {
		t.Fatalf("no-replacement scrub aliased the raw body: %q", out)
	}
}
