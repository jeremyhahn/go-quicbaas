//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// The tenant-scoped WebAuthn ceremony surface,
// /api/v1/iam/{tenant_id}/webauthn/{registration,login}/*, exposing xkms's
// canonical handlers — /registration/begin|finish|status,
// /login/begin|finish — which use the X-Session-Id header for ceremony
// correlation and accept email/display-name bodies.
//
// It is registered in EXACTLY ONE place: the module route table
// (routes_iam.go iamManagedRouteSpecs), served through go-qrdb's unified
// server, dispatching through moduleWebAuthnHandler below. Enforcement for the
// ENROLLMENT verbs — registration/begin, registration/finish — is that table's
// frozen user_authfactors:add permission, so an upstream-identity or
// local-platform-token caller without that grant cannot bind a new
// authenticator to another user's account. The LOGIN ceremony (login/begin,
// login/finish) is RouteAuthPublic in the frozen table: WebAuthn IS the
// authentication mechanism — the caller holds no token until FinishLogin mints
// one. Browser sign-in rides the unified ceremony at
// /api/v1/auth/login/{begin,finish}.
//
// #529 deleted a SECOND registration of these same five paths — a root
// chi.Route mounted from RegisterRoutes onto the legacy in-process router —
// whose mount returned early and silently whenever the xkms-backed manager was
// not yet resolvable. Two handlers claiming one tenant-scoped security path,
// with which one answered decided by initialization order and reported
// nowhere, is not a state to make safe; the second registration is gone, so
// there is no ordering to reason about. Do not reintroduce a mount here: add
// the route to the module table or nowhere.

// resolveWebAuthnFactory pulls the xkms-backed factory out of the
// service layer when the WebAuthn manager is the multi-tenant
// XKMSWebAuthnManager. Returns nil otherwise (legacy code path or
// uninitialized). ctx is the initialization context used for the manager
// lookup; it should be a short-lived context tied to server startup, not
// context.Background().
func (h *IAMHandler) resolveWebAuthnFactory(ctx context.Context) *iam.WebAuthnServiceFactory {
	if h.server == nil || h.server.service == nil {
		return nil
	}
	getter, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		return nil
	}
	mgr, err := getter.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		return nil
	}
	xkmsMgr, ok := mgr.(*iam.XKMSWebAuthnManager)
	if !ok {
		return nil
	}
	return xkmsMgr.Factory()
}

// buildXKMSWebAuthnDispatcher returns an http.HandlerFunc that resolves
// the tenant id from the chi URL parameter, looks up the per-tenant
// xkms WebAuthn service, and dispatches the request to the matching
// xkms HTTP handler. The dispatcher is shared across all five
// ceremony routes; xkms's HandlerFunc internally routes by URL path.
//
// The url-strip approach: chi routes the request with the prefix
// "/api/v1/iam/{tenant_id}/webauthn" already stripped from the path
// pattern but kept in r.URL.Path. We therefore rewrite r.URL.Path to
// the suffix the xkms handler expects (e.g. "/registration/begin")
// before invoking xkms.Handler.HandlerFunc.
func buildXKMSWebAuthnDispatcher(factory *iam.WebAuthnServiceFactory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "tenant_id")
		if tenantID == "" {
			http.Error(w, "tenant_id is required", http.StatusBadRequest)
			return
		}
		svc, err := factory.ServiceFor(r.Context(), tenantID)
		if err != nil {
			http.Error(w, "tenant webauthn service unavailable", http.StatusServiceUnavailable)
			return
		}
		handler := xkmssdk.NewWebAuthnHTTPHandler(svc)

		// Compute the suffix the xkms HandlerFunc expects: strip the
		// "/api/v1/iam/{tenantID}/webauthn" prefix from r.URL.Path.
		prefix := "/api/v1/iam/" + tenantID + "/webauthn"
		stripped := r.URL.Path[len(prefix):]
		if stripped == "" {
			http.NotFound(w, r)
			return
		}

		// Verify the attestation trust path before handing the ceremony to
		// xkms, and carry the result on the context for the assurance gate.
		//
		// This is the LIVE registration route, and it is the reason the
		// verification cannot live in XKMSWebAuthnManager: the request goes
		// straight into xkms's own handler and never passes through the
		// manager. The attestation object exists only here, in the request
		// body — by the time the credential store sees the credential, the
		// statement is gone.
		ctx, body, err := verifyDispatchedAttestation(factory, r, stripped)
		if err != nil {
			// Same rule as the PIV login endpoint: the detail describes the
			// server and this route is public, so it goes to the log and the
			// caller gets a stable sentence. This surface had no server-side
			// sink at all, which is the other half of the problem — the text
			// was only ever visible to the one party who should not see it.
			slog.Default().Warn("webauthn registration: request rejected before dispatch",
				"path", stripped,
				"tenant_id", tenantID,
				"error", err)
			http.Error(w, ErrMsgWebAuthnRegistrationRejected, http.StatusBadRequest)
			return
		}

		// Clone the request with the rewritten path so the xkms
		// HandlerFunc routes correctly without mutating shared state.
		newURL := *r.URL
		newURL.Path = stripped
		r2 := r.Clone(ctx)
		r2.URL = &newURL
		if body != nil {
			// The body was consumed to read the attestation, so replay it.
			r2.Body = io.NopCloser(bytes.NewReader(body))
			r2.ContentLength = int64(len(body))
		}

		handler.HandlerFunc()(w, r2)
	}
}

// verifyDispatchedAttestation reads a registration/finish body, verifies the
// attestation trust path, and returns a context carrying the result along with
// the body bytes to replay.
//
// It returns an error ONLY when the request body could not be read at all —
// which is a malformed request, not a policy decision. A chain that does not
// verify is not an error here: whether this registrant must prove hardware is
// the assurance gate's call, and the context simply carries no verified chain.
// Every other route passes through untouched with its body intact.
func verifyDispatchedAttestation(
	factory *iam.WebAuthnServiceFactory,
	r *http.Request,
	strippedPath string,
) (context.Context, []byte, error) {
	// EVERY return carries either a proof this request just produced or none at
	// all. An incoming context is never passed through untouched on a
	// registration path: a caller able to arrange for a chain to be present
	// could otherwise skip verification with an unparseable attestation and
	// keep it.
	ctx := iam.ClearAttestationChain(r.Context())
	if r.Method != http.MethodPost || strippedPath != "/registration/finish" || r.Body == nil {
		return ctx, nil, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ctx, nil, &iam.WebAuthnAttestationChainError{
			Reason: "the registration body could not be read",
			Cause:  err,
		}
	}

	parsed, parseErr := protocol.ParseCredentialCreationResponseBytes(body)
	if parseErr != nil {
		// Let xkms produce the canonical parse error from the replayed body,
		// so a malformed credential is reported the same way on every route.
		return ctx, body, nil
	}

	verified, verifyErr := factory.VerifyAttestationChain(ctx, parsed)
	if verifyErr != nil {
		// Not a refusal: the gate decides. Recorded so an operator debugging a
		// rejected security key can see why the chain did not verify.
		// VerifyAttestationChain clears the proof itself on failure.
		slog.Default().Debug("webauthn registration: attestation trust path not verified",
			"path", strippedPath,
			"error", verifyErr)
	}
	return verified, body, nil
}

// tenantWebAuthnRoutes names the five paths moduleWebAuthnHandler serves. It
// exists so the unavailability warning can say WHICH surface is down instead
// of leaving an operator to infer it from a 503 body.
var tenantWebAuthnRoutes = []string{
	"POST /api/v1/iam/{tenant_id}/webauthn/registration/begin",
	"POST /api/v1/iam/{tenant_id}/webauthn/registration/finish",
	"GET /api/v1/iam/{tenant_id}/webauthn/registration/status",
	"POST /api/v1/iam/{tenant_id}/webauthn/login/begin",
	"POST /api/v1/iam/{tenant_id}/webauthn/login/finish",
}

// moduleWebAuthnHandler returns the http.HandlerFunc registered by the module
// route table for the five tenant-scoped WebAuthn paths. It resolves the
// xkms-native factory PER REQUEST and dispatches through
// buildXKMSWebAuthnDispatcher, which reads the tenant from the URL, so the
// handler serving a tenant-named path is always the one scoped to that tenant.
//
// Resolving per request rather than once at registration time is what lets the
// route exist before the xkms-backed manager does. The cost is that
// unavailability is a RUNTIME condition, and #529's rule applies to it: a
// security-relevant surface that is not answering says so. Every request gets a
// 503 (never a silent 404, and never a fallback to a handler with different
// tenant semantics), and the first request of each unavailability episode logs
// a WARN naming the affected routes and the consequence.
//
// The warning is emitted once per episode rather than per request: an
// unavailable manager on a login page is a request-rate event, and a line per
// request buries the one that matters. The episode flag is the SERVER's, not
// this closure's, because the module table binds this handler once per
// ceremony path — five closures would each warn about one outage. It resets the
// moment the factory resolves, so a later outage warns again.
func (s *Server) moduleWebAuthnHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := s.iamHTTP()
		if h == nil {
			warnTenantWebAuthnUnavailable(&s.tenantWebAuthnWarned, "iam is not configured on this server")
			http.Error(w, "iam is not configured", http.StatusServiceUnavailable)
			return
		}
		factory := h.resolveWebAuthnFactory(r.Context())
		if factory == nil {
			warnTenantWebAuthnUnavailable(&s.tenantWebAuthnWarned,
				"the xkms-backed multi-tenant WebAuthn manager did not resolve")
			http.Error(w, "webauthn is not configured on this tenant", http.StatusServiceUnavailable)
			return
		}
		s.tenantWebAuthnWarned.Store(false)
		buildXKMSWebAuthnDispatcher(factory)(w, r)
	}
}

// warnTenantWebAuthnUnavailable logs the first refusal of an unavailability
// episode, naming the routes that are down and what it costs the deployment.
// warned is the per-handler episode flag; CompareAndSwap keeps concurrent
// first requests to one line.
func warnTenantWebAuthnUnavailable(warned *atomic.Bool, reason string) {
	if !warned.CompareAndSwap(false, true) {
		return
	}
	slog.Default().Warn(
		"tenant WebAuthn ceremony unavailable: no user in any tenant can enroll or present a passkey on these routes",
		"reason", reason,
		"routes", tenantWebAuthnRoutes,
		"status", http.StatusServiceUnavailable)
}
