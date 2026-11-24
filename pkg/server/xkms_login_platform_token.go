// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// xkmsPasskeyLoginRoutes are the go-xkms passkey (FIDO2) login routes whose
// successful answer carries a module-issued session token: the browser
// ceremony's finish, which answers {"token","refresh_token"}, and the SDK
// ceremony's finish, which answers {"success","token","username","message"}.
var xkmsPasskeyLoginRoutes = []string{
	"/api/v1/webauthn/login/finish",
	"/api/v1/webauthn/sdk/authentication/finish",
}

const (
	// xkmsSubjectKindKey is the identity metadata key go-xkms names the
	// kind of a token's Subject under.
	xkmsSubjectKindKey = "subject_kind"
	// xkmsPasskeySubjectKind is the subject kind go-xkms gives a passkey
	// token's identity: its Subject is the WebAuthn user handle, the qrdb
	// user id as 8 big-endian bytes in unpadded base64url.
	xkmsPasskeySubjectKind = "user_handle"
)

// xkmsLoginPlatformTokenModule answers the xkms module's PIV and passkey
// logins with platform tokens the node's own OIDC provider issues. The pinned
// module signs its session tokens on its own key backend, in a shape the
// node's platform validator does not accept and under a kid the node's JWKS
// does not publish; the node's current OIDC signing key must sign every token
// the node accepts. The module still checks the certificate or the passkey
// assertion; this wrapper only replaces the tokens of a login it accepted.
type xkmsLoginPlatformTokenModule struct {
	qrdbsdk.ServiceModule
	qrdbsdk.ModuleMCPToolPermissioner
	qrdbsdk.ModuleGRPCPublicMethodLister

	inner         qrdbsdk.ModuleAuthFallback
	server        *Server
	certRoutes    map[string]struct{}
	passkeyRoutes map[string]struct{}
}

// XKMSLoginPlatformTokenNilServerError is returned when the exchange is built
// without the node whose OIDC provider issues the platform tokens.
type XKMSLoginPlatformTokenNilServerError struct{}

func (e *XKMSLoginPlatformTokenNilServerError) Error() string {
	return "xkms login platform token: server is nil"
}

// xkmsLoginExchange describes how one kind of module login is answered with
// platform tokens.
type xkmsLoginExchange struct {
	// precheck, when set, refuses the login with 401 before IAM is
	// consulted.
	precheck func(r *http.Request) bool
	// resolve names the IAM user the accepted login belongs to from the
	// request and the module's buffered answer; nil refuses it with 401.
	resolve func(r *http.Request, buffered *bufferedResponseWriter, store *iam.IdentityStore) *iam.User
	// amr is the authentication methods the platform tokens claim.
	amr []string
	// replaceToken also replaces the body's token field, which go-xkms's
	// passkey clients read the session token from.
	replaceToken bool
}

// newXKMSLoginPlatformTokenModule wraps mod so that a successful login on one
// of certRoutes (client certificate) or passkeyRoutes (go-xkms passkey)
// answers with platform tokens issued by s's OIDC provider, and so that its
// bearer fallback accepts platform tokens first.
func newXKMSLoginPlatformTokenModule(s *Server, mod qrdbsdk.ServiceModule, certRoutes, passkeyRoutes []string) (qrdbsdk.ServiceModule, error) {
	if mod == nil {
		return nil, &apimodule.NilModuleError{}
	}
	if s == nil {
		return nil, &XKMSLoginPlatformTokenNilServerError{}
	}
	toolPermissions, ok := mod.(qrdbsdk.ModuleMCPToolPermissioner)
	if !ok {
		return nil, &apimodule.ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleMCPToolPermissioner"}
	}
	authFallback, ok := mod.(qrdbsdk.ModuleAuthFallback)
	if !ok {
		return nil, &apimodule.ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleAuthFallback"}
	}
	publicGRPC, ok := mod.(qrdbsdk.ModuleGRPCPublicMethodLister)
	if !ok {
		return nil, &apimodule.ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleGRPCPublicMethodLister"}
	}

	return &xkmsLoginPlatformTokenModule{
		ServiceModule:                mod,
		ModuleMCPToolPermissioner:    toolPermissions,
		ModuleGRPCPublicMethodLister: publicGRPC,
		inner:                        authFallback,
		server:                       s,
		certRoutes:                   routeSet(certRoutes),
		passkeyRoutes:                routeSet(passkeyRoutes),
	}, nil
}

func routeSet(routes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		set[route] = struct{}{}
	}
	return set
}

// RegisterHTTP registers the module's routes, wrapping the public login
// routes named at construction with the platform-token exchange.
func (m *xkmsLoginPlatformTokenModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	if r == nil {
		return
	}
	bound := qrdbsdk.NewModuleHTTPRegistrar(
		r.Router,
		r.Handle,
		func(method, pattern string, h http.HandlerFunc) {
			if _, ok := m.certRoutes[pattern]; ok {
				// The module route exists only under allow_unattested, so
				// these are the unattested claims of the quicbaas PIV login:
				// amr mtls, no acr and no attestation.
				h = m.exchangeLoginTokens(h, xkmsLoginExchange{
					precheck: hasPeerCertificate,
					resolve:  resolveCertUser,
					amr:      []string{"mtls"},
				})
			} else if _, ok := m.passkeyRoutes[pattern]; ok {
				// The exchange sees no attestation evidence, so a passkey
				// login claims amr webauthn and never hwk, with no acr.
				h = m.exchangeLoginTokens(h, xkmsLoginExchange{
					resolve:      m.resolvePasskeyUser,
					amr:          []string{"webauthn"},
					replaceToken: true,
				})
			}
			r.HandlePublic(method, pattern, h)
		},
	)
	m.ServiceModule.RegisterHTTP(bound)
}

// AuthenticateHTTP accepts a platform bearer first and otherwise defers to
// the module's own fallback, so no bearer the module accepted is refused.
func (m *xkmsLoginPlatformTokenModule) AuthenticateHTTP(req *http.Request) (*qrdbsdk.Identity, error) {
	if authn, ok := newPlatformBearerAuthenticator(m.server).(*platformBearerAuthenticator); ok {
		identity, err := authn.authenticateAuthorization(req.Context(), req.Header.Get("Authorization"))
		if err == nil && identity != nil {
			return identity, nil
		}
	}
	return m.inner.AuthenticateHTTP(req)
}

// hasPeerCertificate reports whether the request presented a client
// certificate.
func hasPeerCertificate(r *http.Request) bool {
	return r.TLS != nil && len(r.TLS.PeerCertificates) > 0 && r.TLS.PeerCertificates[0] != nil
}

// resolveCertUser names the IAM user whose PIV thumbprint is the verified
// client certificate's.
func resolveCertUser(r *http.Request, _ *bufferedResponseWriter, store *iam.IdentityStore) *iam.User {
	if !hasPeerCertificate(r) {
		return nil
	}
	leaf := r.TLS.PeerCertificates[0]
	ctx := r.Context()
	spkiThumbprint := qrdbsdk.ComputeSPKIThumbprint(leaf)
	legacyDigest := sha256.Sum256(leaf.Raw)
	legacyThumbprint := hex.EncodeToString(legacyDigest[:])
	user, err := store.GetUserByPIVThumbprint(ctx, spkiThumbprint)
	if err != nil || user == nil {
		user, err = store.GetUserByPIVThumbprint(ctx, legacyThumbprint)
	}
	if err != nil || user == nil || !loginTenantMatches(r, user.TenantID) {
		return nil
	}
	return user
}

// resolvePasskeyUser names the IAM user a go-xkms passkey login belongs to.
// The identity comes only from the module's own token, verified by the
// module's bearer fallback; the body's username is never trusted.
func (m *xkmsLoginPlatformTokenModule) resolvePasskeyUser(r *http.Request, buffered *bufferedResponseWriter, store *iam.IdentityStore) *iam.User {
	var answer struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(buffered.body.Bytes(), &answer); err != nil || strings.TrimSpace(answer.Token) == "" {
		return nil
	}
	verify := r.Clone(r.Context())
	verify.Header.Set("Authorization", "Bearer "+answer.Token)
	identity, err := m.inner.AuthenticateHTTP(verify)
	if err != nil || identity == nil || identity.Metadata[xkmsSubjectKindKey] != xkmsPasskeySubjectKind {
		return nil
	}
	handle, err := base64.RawURLEncoding.DecodeString(identity.Subject)
	if err != nil || len(handle) != 8 {
		return nil
	}
	user, err := store.GetUser(r.Context(), identity.TenantID, binary.BigEndian.Uint64(handle))
	if err != nil || user == nil || !loginTenantMatches(r, user.TenantID) {
		return nil
	}
	return user
}

// exchangeLoginTokens runs next and, when it answered 200, replaces the
// module's tokens with platform tokens for the IAM user ex resolves. Every
// other outcome is passed through or fails closed: a 200 whose user cannot be
// resolved becomes 401 and none of the module's tokens reach the client.
func (m *xkmsLoginPlatformTokenModule) exchangeLoginTokens(next http.HandlerFunc, ex xkmsLoginExchange) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := newBufferedResponseWriter()
		next(rec, r)
		if rec.status != http.StatusOK {
			rec.copyTo(w)
			return
		}

		if ex.precheck != nil && !ex.precheck(r) {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}

		if m.server.iamComponents == nil {
			http.Error(w, "identity service unavailable", http.StatusServiceUnavailable)
			return
		}
		store := m.server.iamComponents.GetIdentityStore()
		provider := m.server.iamComponents.GetOIDCProvider()
		if store == nil || provider == nil {
			http.Error(w, "identity service unavailable", http.StatusServiceUnavailable)
			return
		}

		user := ex.resolve(r, rec, store)
		if user == nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}

		tokens, err := provider.IssueHardwareTokens(r.Context(), iam.HardwareTokenOptions{
			User: user,
			AMR:  ex.amr,
		})
		if err != nil || tokens == nil {
			http.Error(w, "token issuance failed", http.StatusInternalServerError)
			return
		}

		var payload map[string]any
		if err := json.Unmarshal(rec.body.Bytes(), &payload); err != nil || payload == nil {
			http.Error(w, "login response is not a JSON object", http.StatusBadGateway)
			return
		}
		if ex.replaceToken {
			// go-xkms's passkey clients, xkey auth login among them, read
			// the session token from token.
			payload["token"] = tokens.AccessToken
		}
		payload["access_token"] = tokens.AccessToken
		payload["id_token"] = tokens.IDToken
		payload["refresh_token"] = tokens.RefreshToken
		payload["token_type"] = tokens.TokenType
		payload["expires_in"] = tokens.ExpiresIn
		out, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, "token issuance failed", http.StatusInternalServerError)
			return
		}

		for key, values := range rec.header {
			if http.CanonicalHeaderKey(key) == "Content-Length" {
				continue
			}
			w.Header()[key] = append([]string(nil), values...)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	}
}

// loginTenantMatches reports whether userTenant is the tenant the request is
// bound to. With no tenant bound, only a system-tenant user matches.
func loginTenantMatches(r *http.Request, userTenant string) bool {
	bound, ok := tenantctx.Lookup(r.Context())
	if !ok || bound == "" {
		return userTenant == "" || tenantctx.IsSystem(userTenant)
	}
	return userTenant == bound
}

// bufferedResponseWriter records a handler's status, headers and body so the
// exchange can inspect them before anything reaches the client.
type bufferedResponseWriter struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header), status: http.StatusOK}
}

func (b *bufferedResponseWriter) Header() http.Header { return b.header }

func (b *bufferedResponseWriter) WriteHeader(status int) {
	if b.wroteHeader {
		return
	}
	b.wroteHeader = true
	b.status = status
}

func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
	return b.body.Write(p)
}

// copyTo writes the recorded response to w unchanged.
func (b *bufferedResponseWriter) copyTo(w http.ResponseWriter) {
	for key, values := range b.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}
