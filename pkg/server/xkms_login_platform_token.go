// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// xkmsLoginPlatformTokenModule answers the xkms module's PIV login with
// platform tokens the node's own OIDC provider issues. The pinned module
// signs its session tokens on its own key backend, in a shape the node's
// platform validator does not accept; the node's current OIDC signing key
// must sign every token the node accepts. The module still checks the
// certificate; this wrapper only replaces the tokens of a login it accepted.
type xkmsLoginPlatformTokenModule struct {
	qrdbsdk.ServiceModule
	qrdbsdk.ModuleMCPToolPermissioner
	qrdbsdk.ModuleGRPCPublicMethodLister

	inner  qrdbsdk.ModuleAuthFallback
	server *Server
	routes map[string]struct{}
}

// XKMSLoginPlatformTokenNilServerError is returned when the exchange is built
// without the node whose OIDC provider issues the platform tokens.
type XKMSLoginPlatformTokenNilServerError struct{}

func (e *XKMSLoginPlatformTokenNilServerError) Error() string {
	return "xkms login platform token: server is nil"
}

// newXKMSLoginPlatformTokenModule wraps mod so that a successful login on one
// of routes answers with platform tokens issued by s's OIDC provider, and so
// that its bearer fallback accepts platform tokens first.
func newXKMSLoginPlatformTokenModule(s *Server, mod qrdbsdk.ServiceModule, routes []string) (qrdbsdk.ServiceModule, error) {
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

	set := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		set[route] = struct{}{}
	}
	return &xkmsLoginPlatformTokenModule{
		ServiceModule:                mod,
		ModuleMCPToolPermissioner:    toolPermissions,
		ModuleGRPCPublicMethodLister: publicGRPC,
		inner:                        authFallback,
		server:                       s,
		routes:                       set,
	}, nil
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
			if _, ok := m.routes[pattern]; ok {
				h = m.exchangeLoginTokens(h)
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

// exchangeLoginTokens runs next and, when it answered 200, replaces the
// module's tokens with platform tokens for the IAM user the verified client
// certificate names. Every other outcome is passed through or fails closed.
func (m *xkmsLoginPlatformTokenModule) exchangeLoginTokens(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := newBufferedResponseWriter()
		next(rec, r)
		if rec.status != http.StatusOK {
			rec.copyTo(w)
			return
		}

		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0] == nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		leaf := r.TLS.PeerCertificates[0]

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

		ctx := r.Context()
		spkiThumbprint := qrdbsdk.ComputeSPKIThumbprint(leaf)
		legacyDigest := sha256.Sum256(leaf.Raw)
		legacyThumbprint := hex.EncodeToString(legacyDigest[:])
		user, err := store.GetUserByPIVThumbprint(ctx, spkiThumbprint)
		if err != nil || user == nil {
			user, err = store.GetUserByPIVThumbprint(ctx, legacyThumbprint)
		}
		if err != nil || user == nil || !loginTenantMatches(r, user.TenantID) {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}

		// The module route exists only under allow_unattested, so these are
		// the unattested claims of the quicbaas PIV login: amr mtls, no acr
		// and no attestation.
		tokens, err := provider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
			User: user,
			AMR:  []string{"mtls"},
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
