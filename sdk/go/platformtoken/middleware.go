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

package platformtoken

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// The route guard below is what a downstream daemon's REST plane mounts to
// authenticate and authorise every request with go-quicbaas, so the daemon
// keeps no auth model, route guard or token format of its own
// (go-trusted-platform#644). The principal comes from the go-quicbaas
// platform token only: its tenant and user are the token's, never a request
// field.

type authorizedContextKey struct{}

// FromContext returns the principal the guard authorised the request as.
func FromContext(ctx context.Context) (*AuthorizedContext, bool) {
	ac, ok := ctx.Value(authorizedContextKey{}).(*AuthorizedContext)
	return ac, ok && ac != nil
}

// WithAuthorizedContext binds ac onto ctx, as the guard does for the next
// handler.
func WithAuthorizedContext(ctx context.Context, ac *AuthorizedContext) context.Context {
	return context.WithValue(ctx, authorizedContextKey{}, ac)
}

// Middleware returns the route guard for one permission (a scope such as
// "keys:get"; empty authenticates only). Each request must carry a platform
// access token as "Authorization: Bearer <token>" or "Authorization: DPoP
// <token>". The guard then:
//
//   - verifies the token (signature against the issuer JWKS, iss, aud, exp,
//     at+jwt type) and, for a DPoP-bound token (cnf.jkt), the DPoP proof in
//     the DPoP header against the request's method and URL (RFC 9449);
//   - for an mTLS-bound token (cnf x5t#S256), requires the TLS client
//     certificate whose SHA-256 thumbprint the token names (RFC 8705), so a
//     PIV or device certificate login cannot be replayed from another key;
//   - requires the permission among the token's granted scopes, then the
//     liveness check the validator's mode asks for, then asks go-qrdb for
//     the permission as the token's complete principal (subject, roles,
//     scope and authentication claims) in the token's tenant: first
//     Options.RBAC, whose mandatory restrictions such as the SO deny-list
//     hold whatever an ABAC policy says, then Options.Authorizer (RBAC with
//     its ABAC policies). The token's scope bounds every grant: a role
//     never admits a permission outside it.
//
// A missing or invalid credential is 401 with a WWW-Authenticate challenge, a
// missing permission 403, a permission the authorizer denies 403, and a guard
// that cannot decide (no JWKS, no liveness authority, no authorizer or RBAC
// for a permission route) 503. The authorised principal is bound onto the
// request's context; read it with FromContext.
func (v *Validator) Middleware(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac, err := v.AuthorizeRequest(r, permission)
			if err != nil {
				writeGuardError(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithAuthorizedContext(r.Context(), ac)))
		})
	}
}

// AuthorizeRequest is the guard's decision for one request, for a daemon
// that routes without net/http middleware.
func (v *Validator) AuthorizeRequest(r *http.Request, permission string) (*AuthorizedContext, error) {
	token, err := requestToken(r)
	if err != nil {
		return nil, err
	}
	ac, claims, err := v.validateAndProject(r.Context(), token, r.Header.Get("DPoP"), r.Method, requestHTU(r))
	if err != nil {
		return nil, err
	}
	if err := requireCertificateBinding(r, ac.Confirmation); err != nil {
		return nil, err
	}
	if permission != "" {
		if err := ac.RequireScope(permission); err != nil {
			return nil, err
		}
	}
	if err := v.enforceLivenessByMode(r.Context(), token, claims); err != nil {
		return nil, err
	}
	if permission != "" {
		if err := v.authorizePermission(r.Context(), ac, permission); err != nil {
			return nil, err
		}
	}
	return ac, nil
}

// authorizePermission asks go-qrdb for permission's resource and action as
// the token's complete principal, in the token's tenant. It fails closed: a
// permission outside the token's scope is refused before anything is
// asked, a missing authorizer or RBAC is a configuration error, and any
// error from either is a refusal.
//
// The token's scope bounds every grant. go-qrdb's RBAC unions a role's
// permissions with Identity.ScopePermissions, so the authorizer alone
// would let a role grant beyond the token. The scope check therefore runs
// here as well as in AuthorizeRequest.
//
// Both decisions see the same complete identity (authorizerIdentity):
//
//   - Options.RBAC first. In go-qrdb a matched ABAC allow is decisive and
//     never reaches the inner RBAC, so RBAC's mandatory restrictions (the
//     SO deny-list for cryptographic operations, FIPS 140-3 AS10.03, and
//     the tenant scope rules) are enforced by asking it directly;
//   - then Options.Authorizer, RBAC decorated with ABAC. ABAC sees the
//     token's roles and authentication claims together, so a deny
//     selecting either matches, and an absence-based condition
//     ("notexists") or an unless exemption is judged on what the token
//     actually carries, including a token that carries no authentication
//     claim at all.
//
// A request is admitted only when its permission is in scope and both
// allow.
func (v *Validator) authorizePermission(ctx context.Context, ac *AuthorizedContext, permission string) error {
	if err := ac.RequireScope(permission); err != nil {
		return err
	}
	if v.authorizer == nil {
		return &ConfigError{Detail: "a permission route needs Options.Authorizer"}
	}
	if v.rbac == nil {
		return &ConfigError{Detail: "a permission route needs Options.RBAC"}
	}
	ctx = qrdbsdk.ContextWithTenant(ctx, qrdbsdk.NormalizeTenantID(ac.TenantID))
	perm := qrdbsdk.Permission(permission)
	resource, action := qrdbsdk.PermissionResource(perm), qrdbsdk.PermissionAction(perm)
	for _, decide := range []qrdbsdk.Authorizer{v.rbac, v.authorizer} {
		if err := decide.Authorize(ctx, authorizerIdentity(ac), resource, action); err != nil {
			return &PolicyDeniedError{Required: permission, Cause: err}
		}
	}
	return nil
}

// guardAuthSourceKey and guardAuthSource mark, in both Identity.Metadata and
// Identity.Attributes, an identity the route guard built from a verified
// platform token. The mark keeps both attribute namespaces go-qrdb sees
// non-empty for every guard caller: go-qrdb's ABAC decorator turns an empty
// map in either namespace (Identity.Metadata, read as subject.attrs, and
// Identity.Attributes, read as subject.user_attrs) into a nil namespace, and
// it resolves a name against that nil namespace as present. So without the
// mark, a deny on an attribute's absence ("notexists") would not refuse a
// token with no authentication claims or any guard caller (the guard reads
// no user record), and an "exists" exemption would cover it. The key follows
// go-qrdb's own auth_source mark. Its value is never go-qrdb's on_behalf.
const (
	guardAuthSourceKey = "auth_source"
	guardAuthSource    = "platform_token"
)

// authorizerIdentity is the complete verified principal the guard asks
// go-qrdb about:
//
//   - the token's subject, tenant and roles;
//   - its scope, as ScopePermissions, which is its only grant;
//   - its authentication claims, as Identity.Metadata, together with the
//     guard's auth_source mark (guardAuthSourceKey), set last so no claim
//     can replace it;
//   - in Identity.Attributes, the user-record namespace, only the guard's
//     mark: the guard reads no user record and never copies a token claim
//     there.
//
// Neither map is ever empty, so go-qrdb judges a condition on a missing
// claim, and on any subject.user_attrs.<name> other than the mark, as
// missing. Every call returns fresh copies of the slices and new maps, so neither decision can change the authorised principal or
// the identity the other decision sees.
func authorizerIdentity(ac *AuthorizedContext) *qrdbsdk.Identity {
	metadata := make(map[string]string, len(ac.AuthAttributes)+1)
	maps.Copy(metadata, ac.AuthAttributes)
	metadata[guardAuthSourceKey] = guardAuthSource
	return &qrdbsdk.Identity{
		Subject:          ac.Subject,
		TenantID:         ac.TenantID,
		Roles:            slices.Clone(ac.Roles),
		ScopePermissions: slices.Clone(ac.Scopes),
		Metadata:         metadata,
		Attributes:       map[string]string{guardAuthSourceKey: guardAuthSource},
	}
}

// requestToken reads the access token from the Authorization header.
func requestToken(r *http.Request) (string, error) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return "", &InvalidTokenError{Detail: "no Authorization header"}
	}
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || (!strings.EqualFold(scheme, "Bearer") && !strings.EqualFold(scheme, "DPoP")) {
		return "", &InvalidTokenError{Detail: "Authorization is not a Bearer or DPoP token"}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", &InvalidTokenError{Detail: "empty access token"}
	}
	return token, nil
}

// requestHTU is the DPoP htu of r: scheme, host and path, without query or
// fragment (RFC 9449 §4.3).
func requestHTU(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + r.URL.EscapedPath()
}

// requireCertificateBinding enforces an RFC 8705 x5t#S256 binding against the
// request's TLS client certificate. A token without one passes.
func requireCertificateBinding(r *http.Request, cnf *Confirmation) error {
	if cnf == nil || cnf.X5TS256 == "" {
		return nil
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return &CertificateBindingError{Detail: "token is bound to a client certificate and the request presented none"}
	}
	sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(got), []byte(cnf.X5TS256)) != 1 {
		return &CertificateBindingError{Detail: "client certificate does not match the token's x5t#S256 binding"}
	}
	return nil
}

// writeGuardError maps a guard refusal to its status. The error's text is not
// written: a caller learns the class of refusal, not the validator's detail.
func writeGuardError(w http.ResponseWriter, err error) {
	var (
		scope  *ScopeDeniedError
		policy *PolicyDeniedError
		cfg    *ConfigError
		jwks   *JWKSFetchError
		live   *LivenessCheckError
	)
	switch {
	case errors.As(err, &scope):
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+scope.Required+`"`)
		http.Error(w, "insufficient_scope", http.StatusForbidden)
	case errors.As(err, &policy):
		http.Error(w, "access_denied", http.StatusForbidden)
	case errors.As(err, &cfg), errors.As(err, &jwks), errors.As(err, &live):
		http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
	default:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "invalid_token", http.StatusUnauthorized)
	}
}
