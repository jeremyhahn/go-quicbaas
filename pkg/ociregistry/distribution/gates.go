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
	"net/http"

	"github.com/gorilla/mux"

	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry"
)

// HeaderRegistrySession carries the attested session ID: the gates set it
// on the response that created the session, and a client sends it back
// on later requests instead of a fresh quote.
const HeaderRegistrySession = "X-Registry-Session"

// attestationChallengePath is the registry's own challenge route. No
// distribution route ends in /_attestation/challenge, so it cannot be
// taken for a repository path.
const attestationChallengePath = "/v2/_attestation/challenge"

// IdentityFunc returns the workload identity of a request's caller, and
// false when the request carries none.
type IdentityFunc func(*http.Request) (string, bool)

// VerifiedPeerIdentity is the IdentityFunc for mTLS: the first URI SAN
// (the SPIFFE ID) of the leaf of the first verified client chain. It
// returns false when the connection is not TLS, the client certificate
// was not verified, or the leaf has no URI SAN. Unverified peer
// certificates are never trusted.
func VerifiedPeerIdentity(r *http.Request) (string, bool) {
	if r == nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		return "", false
	}
	leaf := r.TLS.VerifiedChains[0][0]
	if leaf == nil || len(leaf.URIs) == 0 || leaf.URIs[0] == nil {
		return "", false
	}
	id := leaf.URIs[0].String()
	if id == "" {
		return "", false
	}
	return id, true
}

// IdentityRequiredError is returned when the gates or the challenge route
// are built without an IdentityFunc.
type IdentityRequiredError struct{}

// Error implements the error interface.
func (e *IdentityRequiredError) Error() string {
	return "registry: the OCI distribution gates require an identity function"
}

// AttestationHandlerRequiredError is returned when attestation is
// required, or the challenge route is mounted, without an auth handler.
type AttestationHandlerRequiredError struct{}

// Error implements the error interface.
func (e *AttestationHandlerRequiredError) Error() string {
	return "registry: attestation requires a registry auth handler"
}

// NewAttestedGates builds the /v2/ read and write gates. Every request
// needs a workload identity (otherwise 401 UNAUTHORIZED). With
// cfg.RequireAttestation it also needs either a live session bound to
// that identity (HeaderRegistrySession) or fresh attestation evidence
// over a server-issued nonce (ociregistry.HeaderAttestationEvidence),
// otherwise 403 DENIED. Evidence that authenticates sets
// HeaderRegistrySession on the response.
func NewAttestedGates(identity IdentityFunc, auth *ociregistry.RegistryAuthHandler, cfg ociregistry.RegistryAttestationConfig) (OCIDistGates, error) {
	if identity == nil {
		return OCIDistGates{}, &IdentityRequiredError{}
	}
	if cfg.RequireAttestation && auth == nil {
		return OCIDistGates{}, &AttestationHandlerRequiredError{}
	}
	g := &attestedGate{identity: identity, auth: auth, require: cfg.RequireAttestation}
	return OCIDistGates{Read: g.wrap, Write: g.wrap}, nil
}

// attestedGate is the chain both gates run.
type attestedGate struct {
	identity IdentityFunc
	auth     *ociregistry.RegistryAuthHandler
	require  bool
}

func (g *attestedGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := g.identity(r)
		if !ok || id == "" {
			writeOCIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "a verified workload identity is required")
			return
		}
		if !g.require {
			next.ServeHTTP(w, r)
			return
		}
		if msg, ok := g.attest(w, r, id); !ok {
			writeOCIError(w, http.StatusForbidden, "DENIED", msg)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// attest admits the request on a session bound to id or on fresh
// evidence for id. On refusal it returns the reason; the reason never
// echoes the evidence.
func (g *attestedGate) attest(w http.ResponseWriter, r *http.Request, id string) (string, bool) {
	ctx := r.Context()
	reason := "attestation required"
	if sid := r.Header.Get(HeaderRegistrySession); sid != "" {
		session, err := g.auth.LookupSession(ctx, sid)
		switch {
		case err != nil:
			reason = err.Error()
		case session.ClientID != id:
			reason = "session is bound to another identity"
		default:
			return "", true
		}
	}
	if r.Header.Get(ociregistry.HeaderAttestationEvidence) == "" {
		return reason, false
	}
	session, err := g.auth.Authenticate(ctx, r)
	if err != nil {
		return err.Error(), false
	}
	if session.ClientID != id {
		_ = g.auth.InvalidateSession(ctx, session.SessionID)
		return "attestation evidence is for another identity", false
	}
	w.Header().Set(HeaderRegistrySession, session.SessionID)
	return "", true
}

// RegisterAttestationChallengeRoute mounts POST /v2/_attestation/challenge
// on parent behind the identity gate alone. It answers
// {"nonce": "<base64>"}, a single-use nonce issued to the caller's
// identity for the TPM quote's qualifying data. Register it before
// RegisterOCIDistRoutes.
func RegisterAttestationChallengeRoute(parent *mux.Router, identity IdentityFunc, auth *ociregistry.RegistryAuthHandler) error {
	if identity == nil {
		return &IdentityRequiredError{}
	}
	if auth == nil {
		return &AttestationHandlerRequiredError{}
	}
	parent.HandleFunc(attestationChallengePath, func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity(r)
		if !ok || id == "" {
			writeOCIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "a verified workload identity is required")
			return
		}
		nonce, err := auth.IssueChallenge(id)
		if err != nil {
			writeOCIError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"nonce": base64.StdEncoding.EncodeToString(nonce)})
	}).Methods(http.MethodPost)
	return nil
}
