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

package iam

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// AppManager manages OAuth2 applications (clients).
type AppManager struct {
	storage AppStorage
	tenants TenantManager
	// grantor resolves the calling identity's qrdb effective permissions for
	// the grantor gate on application updates (authorizeScopeGrant). When
	// nil, an update that submits permission scopes is refused.
	grantor PermissionResolver
}

// NewAppManager creates a new application manager.
func NewAppManager(storage AppStorage, tenants TenantManager, opts ...AppManagerOption) *AppManager {
	m := &AppManager{
		storage: storage,
		tenants: tenants,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// RegisterApp registers a new OAuth2 application. The returned Application
// carries the plaintext client secret (the only time it is visible); no RFC 7592
// registration access token is issued. Use RegisterDCRApp for clients created
// through Dynamic Client Registration, which must own such a token.
func (m *AppManager) RegisterApp(ctx context.Context, tenantID string, opts RegisterAppOptions) (*Application, error) {
	app, _, err := m.registerApp(ctx, tenantID, opts, false)
	return app, err
}

// RegisterAgentApp registers the OAuth2 client a fleet node daemon authenticates
// with, keyed on the agent's identity (tenant + certificate Common Name) so the
// call is IDEMPOTENT: a daemon that enrolls again -- after a restart, a dropped
// response, or a supervisor retry loop -- gets the client it already has, with
// a freshly rotated secret it can actually use.
//
// Re-registration used to mint a new random client every time while the mTLS
// client and the fleet registry both answered already_exists, so a looping
// daemon left a trail of orphan OAuth2 records and never converged on a working
// credential (go-quicbaas#644). The secret is rotated rather than returned,
// because it is stored as a digest and cannot be recovered; the caller is the
// register endpoint, which authenticated the daemon's mTLS certificate for this
// exact identity before calling, so it is entitled to a new one.
//
// The returned application carries the PLAINTEXT secret. It is the only time
// the value exists outside the daemon.
func (m *AppManager) RegisterAgentApp(ctx context.Context, tenantID, commonName string, opts RegisterAppOptions) (*Application, error) {
	if tenantID == "" {
		return nil, &IAMMessageError{Message: "tenant ID is required"}
	}
	if commonName == "" {
		return nil, &IAMMessageError{Message: "certificate common name is required"}
	}

	clientID := FleetNodeClientID(tenantID, commonName)
	if _, err := m.storage.GetAppByTenant(ctx, tenantID, clientID); err == nil {
		return m.rotateAgentCredential(ctx, clientID, opts.Scopes)
	} else if !dberrors.IsNotFound(err) {
		return nil, &IAMContextError{Message: "failed to resolve agent application", Cause: err}
	}

	app, _, err := m.registerAppWithClientID(ctx, tenantID, clientID, opts, false)
	if err == nil {
		return app, nil
	}
	// A concurrent enrollment of the same identity won the create; converge on
	// it rather than failing the daemon that lost the race.
	//
	// coded-sentinel-ok: the whole already-exists class is the intent. The
	// client id is derived from the identity this call was given, so anything
	// the store reports as already present under it is that same agent's
	// record, whichever layer raised it -- and the response to all of them is
	// the same: adopt the existing client rather than mint a second one.
	if errors.Is(err, ErrAlreadyExists) {
		return m.rotateAgentCredential(ctx, clientID, opts.Scopes)
	}
	return nil, err
}

// rotateAgentCredential is RotateSecret for a re-enrolling agent: it rotates
// the secret AND converges the application's grant onto the set enrollment is
// conferring, in the same storage write.
//
// The convergence is what makes the grant a live entitlement rather than a
// one-shot stamped at first enrollment. An application registered by an older
// build carries no grant at all, so without this a daemon that has already
// enrolled once could rotate its secret forever and still be refused on every
// heartbeat (go-quicbaas#653); and a build that narrows the agent grant would
// leave every existing daemon holding the wider one.
func (m *AppManager) rotateAgentCredential(ctx context.Context, clientID string, scopes []string) (*Application, error) {
	app, err := m.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}

	clientSecret, hashedSecret, err := generateClientSecret()
	if err != nil {
		return nil, &IAMContextError{Message: "failed to generate client secret", Cause: err}
	}

	app.ClientSecret = hashedSecret
	app.Scopes = scopes
	app.Updated = time.Now()

	if err := m.storage.UpdateApp(ctx, app); err != nil {
		return nil, &IAMContextError{Message: "failed to update application", Cause: err}
	}

	result := *app
	result.ClientSecret = clientSecret
	result.RegistrationTokenHash = ""
	return &result, nil
}

// registerApp is the single creation path for OAuth2 applications. When
// withRegistrationToken is set it also mints an RFC 7592 registration access
// token, persists only its hash on the application record, and returns the
// plaintext token — which the caller must hand to the client because it is
// never recoverable afterwards. The token hash is written in the SAME storage
// write as the application, so a registered DCR client can never exist without
// its management credential.
func (m *AppManager) registerApp(ctx context.Context, tenantID string, opts RegisterAppOptions, withRegistrationToken bool) (*Application, string, error) {
	return m.registerAppWithClientID(ctx, tenantID, "", opts, withRegistrationToken)
}

// registerAppWithClientID is registerApp with the client identifier supplied by
// the caller. An empty clientID mints a random one, which is the path every
// public registration verb takes; a non-empty one is a DERIVED identifier the
// manager itself computed (FleetNodeClientID), which is what makes an enrollment
// idempotent. It is deliberately unexported and absent from
// RegisterAppOptions: a transport that could name its own client id could
// squat another tenant's identifier space.
func (m *AppManager) registerAppWithClientID(ctx context.Context, tenantID, clientID string, opts RegisterAppOptions, withRegistrationToken bool) (*Application, string, error) {
	// Validate inputs
	if err := m.validateRegisterOptions(opts); err != nil {
		return nil, "", err
	}

	// Check tenant quota
	if m.tenants != nil {
		if err := m.tenants.CheckQuota(ctx, tenantID, QuotaResourceApps); err != nil {
			return nil, "", err
		}
	}

	// Generate client ID and secret
	var err error
	if clientID == "" {
		clientID, err = generateClientID(tenantID)
		if err != nil {
			return nil, "", &IAMContextError{Message: "failed to generate client ID", Cause: err}
		}
	}

	clientSecret, hashedSecret, err := generateClientSecret()
	if err != nil {
		return nil, "", &IAMContextError{Message: "failed to generate client secret", Cause: err}
	}

	var registrationToken, registrationTokenHash string
	if withRegistrationToken {
		registrationToken, registrationTokenHash, err = generateRegistrationAccessToken()
		if err != nil {
			return nil, "", &IAMContextError{Message: "failed to generate registration access token", Cause: err}
		}
	}

	now := time.Now()

	// Create application
	app := &Application{
		ClientID:              clientID,
		ClientSecret:          hashedSecret,
		RegistrationTokenHash: registrationTokenHash,
		Name:                  opts.Name,
		TenantID:              tenantID,
		RedirectURIs:          opts.RedirectURIs,
		ResourcePrefix:        opts.ResourcePrefix,
		Scopes:                opts.Scopes,
		DefaultAudience:       opts.DefaultAudience,
		GrantTypes:            opts.GrantTypes,
		ResponseTypes:         opts.ResponseTypes,
		Created:               now,
		Updated:               now,
	}

	// Set defaults
	if len(app.GrantTypes) == 0 {
		app.GrantTypes = []string{GrantTypeAuthorizationCode, GrantTypeRefreshToken}
	}
	if len(app.ResponseTypes) == 0 {
		app.ResponseTypes = []string{ResponseTypeCode}
	}

	// Store application
	if err := m.storage.CreateApp(ctx, app); err != nil {
		return nil, "", &IAMContextError{Message: "failed to store application", Cause: err}
	}

	// Return application with plaintext secret (only time it's visible). The
	// registration token hash never leaves the manager.
	result := *app
	result.ClientSecret = clientSecret
	result.RegistrationTokenHash = ""

	return &result, registrationToken, nil
}

// GetApp retrieves an application by client ID. The lookup is NOT tenant
// scoped; callers that hold a tenant (every authenticated management surface)
// must use GetAppInTenant so a client id from one tenant cannot address a
// client in another.
func (m *AppManager) GetApp(ctx context.Context, clientID string) (*Application, error) {
	app, err := m.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}
	return redactAppSecrets(app), nil
}

// GetAppInTenant retrieves an application by client ID, requiring it to belong
// to tenantID. An empty tenantID falls back to the unscoped lookup.
func (m *AppManager) GetAppInTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}
	return redactAppSecrets(app), nil
}

// resolveApp loads the raw stored application, tenant-scoped when tenantID is
// non-empty. It is the single read seam every AppManager verb funnels through,
// so tenant scoping cannot be forgotten in one path and enforced in another.
// The returned record still carries its credential hashes; callers that hand it
// to a consumer must redact it.
func (m *AppManager) resolveApp(ctx context.Context, tenantID, clientID string) (*Application, error) {
	if tenantID == "" {
		return m.storage.GetApp(ctx, clientID)
	}
	return m.storage.GetAppByTenant(ctx, tenantID, clientID)
}

// redactAppSecrets clears every credential-bearing field before an Application
// leaves the manager. It is the single place the hashed client secret and the
// RFC 7592 registration access token hash are stripped, so no read path can
// leak them to a transport.
func redactAppSecrets(app *Application) *Application {
	app.ClientSecret = ""
	app.RegistrationTokenHash = ""
	return app
}

// GrantScopes converges an application's granted permission set onto scopes and
// returns the updated record. It is idempotent: an application that already
// agrees writes nothing.
//
// GrantScopes (through applyScopeGrant) is the single writer of
// Application.Scopes after registration. Called directly it is a server-side
// grantor path -- agent enrollment, the central's dispatch-client bootstrap --
// and checks no caller. Scopes submitted by a caller on UpdateAppOptions reach
// it only through the grantor gate in UpdateAppInTenant (authorizeScopeGrant):
// a caller may grant only permissions its own qrdb identity holds, so "update
// my app" never becomes "grant my app any permission I can name".
func (m *AppManager) GrantScopes(ctx context.Context, tenantID, clientID string, scopes []string) (*Application, error) {
	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}
	if !applyScopeGrant(app, scopes) {
		return redactAppSecrets(app), nil
	}

	app.Updated = time.Now()
	if err := m.storage.UpdateApp(ctx, app); err != nil {
		return nil, &IAMContextError{Message: "failed to update application", Cause: err}
	}
	return redactAppSecrets(app), nil
}

// applyScopeGrant sets app's granted permission set to scopes and reports
// whether it changed. It is the one assignment to Application.Scopes after
// registration; GrantScopes and the gated UpdateAppInTenant both call it.
func applyScopeGrant(app *Application, scopes []string) bool {
	if slices.Equal(app.Scopes, scopes) {
		return false
	}
	app.Scopes = scopes
	return true
}

// UpdateApp updates an existing application. The lookup is NOT tenant scoped;
// callers that hold a tenant must use UpdateAppInTenant.
func (m *AppManager) UpdateApp(ctx context.Context, clientID string, opts UpdateAppOptions) (*Application, error) {
	return m.UpdateAppInTenant(ctx, "", clientID, opts)
}

// UpdateAppInTenant updates an application, requiring it to belong to tenantID.
// An empty tenantID falls back to the unscoped lookup.
func (m *AppManager) UpdateAppInTenant(ctx context.Context, tenantID, clientID string, opts UpdateAppOptions) (*Application, error) {
	// A field Application cannot store is refused, never dropped.
	if fields := opts.unstoredFields(); len(fields) > 0 {
		return nil, &AppFieldNotStoredError{Fields: fields}
	}

	// Validate new redirect URIs exactly as registration does, before any
	// field is applied, so a refused update leaves the app unchanged.
	if opts.RedirectURIs != nil {
		if err := validateRedirectURIs(*opts.RedirectURIs); err != nil {
			return nil, err
		}
	}

	// Get existing application
	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}

	// The grantor gate runs before any field is applied: a submitted scope
	// the caller cannot grant refuses the whole update.
	if opts.Scopes != nil {
		if err := m.authorizeScopeGrant(ctx, app.TenantID, app.ClientID, *opts.Scopes); err != nil {
			return nil, err
		}
	}

	// Apply updates. Every field, the scope grant included, is applied to
	// this in-memory copy and committed by the single storage write below,
	// so an update is either refused before that write with the application
	// unchanged, or lands whole.
	if opts.Name != nil {
		app.Name = *opts.Name
	}
	if opts.RedirectURIs != nil {
		app.RedirectURIs = *opts.RedirectURIs
	}
	if opts.ResourcePrefix != nil {
		app.ResourcePrefix = *opts.ResourcePrefix
	}
	if opts.DefaultAudience != nil {
		if err := ValidateDefaultAudience(*opts.DefaultAudience); err != nil {
			return nil, &IAMContextError{Message: "invalid default audience", Cause: err}
		}
		app.DefaultAudience = *opts.DefaultAudience
	}
	if opts.GrantTypes != nil {
		for _, gt := range *opts.GrantTypes {
			if !isValidGrantType(gt) {
				return nil, newInvalidGrantTypeError(gt)
			}
		}
		app.GrantTypes = *opts.GrantTypes
	}
	if opts.ResponseTypes != nil {
		app.ResponseTypes = *opts.ResponseTypes
	}
	if opts.Scopes != nil {
		applyScopeGrant(app, *opts.Scopes)
	}
	if err := applyMTLSUpdate(app, opts); err != nil {
		return nil, err
	}

	app.Updated = time.Now()

	// Update storage
	if err := m.storage.UpdateApp(ctx, app); err != nil {
		return nil, &IAMContextError{Message: "failed to update application", Cause: err}
	}

	return redactAppSecrets(app), nil
}

// applyMTLSUpdate applies the token endpoint auth method and certificate pin
// an update carries. The new pin replaces the old one in the same write, so
// the old certificate stops binding at once. A self_signed_tls_client_auth
// client may not be left without a pin.
func applyMTLSUpdate(app *Application, opts UpdateAppOptions) error {
	if opts.TokenEndpointAuthMethod == nil && opts.MTLSCertThumbprint == nil {
		return nil
	}
	if opts.TokenEndpointAuthMethod != nil {
		if err := ValidateTokenEndpointAuthMethod(*opts.TokenEndpointAuthMethod); err != nil {
			return err
		}
		app.TokenEndpointAuthMethod = *opts.TokenEndpointAuthMethod
	}
	if opts.MTLSCertThumbprint != nil {
		if *opts.MTLSCertThumbprint != "" {
			if err := ValidateCertThumbprint(*opts.MTLSCertThumbprint); err != nil {
				return err
			}
		}
		app.MTLSCertThumbprint = *opts.MTLSCertThumbprint
	}
	if app.TokenEndpointAuthMethod == AuthMethodSelfSignedTLSClientAuth && app.MTLSCertThumbprint == "" {
		return ErrMTLSThumbprintRequired
	}
	return nil
}

// newInvalidGrantTypeError codes an unsupported grant type as invalid input,
// so the transports answer InvalidArgument / 400 rather than an internal error.
func newInvalidGrantTypeError(grantType string) error {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.app",
		Err:  &IAMMessageError{Message: "invalid grant type: " + grantType},
	}
}

// DeleteApp deletes an application. The lookup is NOT tenant scoped; callers
// that hold a tenant must use DeleteAppInTenant.
func (m *AppManager) DeleteApp(ctx context.Context, clientID string) error {
	return m.DeleteAppInTenant(ctx, "", clientID)
}

// DeleteAppInTenant deletes an application, requiring it to belong to tenantID.
// An empty tenantID falls back to the unscoped delete.
func (m *AppManager) DeleteAppInTenant(ctx context.Context, tenantID, clientID string) error {
	if tenantID != "" {
		if _, err := m.storage.GetAppByTenant(ctx, tenantID, clientID); err != nil {
			return err
		}
	}
	if err := m.storage.DeleteApp(ctx, clientID); err != nil {
		return &IAMContextError{Message: "failed to delete application", Cause: err}
	}
	return nil
}

// ListApps lists applications for a tenant.
func (m *AppManager) ListApps(ctx context.Context, tenantID string, opts ListOptions) ([]*Application, error) {
	apps, err := m.storage.ListApps(ctx, tenantID, opts)
	if err != nil {
		return nil, &IAMContextError{Message: "failed to list applications", Cause: err}
	}

	// Remove hashed secrets
	for _, app := range apps {
		redactAppSecrets(app)
	}

	return apps, nil
}

// RotateSecret generates a new client secret for an application. The lookup is
// NOT tenant scoped; callers that hold a tenant must use RotateSecretInTenant.
func (m *AppManager) RotateSecret(ctx context.Context, clientID string) (*Application, error) {
	return m.RotateSecretInTenant(ctx, "", clientID)
}

// RotateSecretInTenant generates a new client secret for an application,
// requiring it to belong to tenantID. An empty tenantID falls back to the
// unscoped lookup.
func (m *AppManager) RotateSecretInTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	// Get existing application
	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}

	// Generate new secret
	clientSecret, hashedSecret, err := generateClientSecret()
	if err != nil {
		return nil, &IAMContextError{Message: "failed to generate client secret", Cause: err}
	}

	// Update application
	app.ClientSecret = hashedSecret
	app.Updated = time.Now()

	if err := m.storage.UpdateApp(ctx, app); err != nil {
		return nil, &IAMContextError{Message: "failed to update application", Cause: err}
	}

	// Return application with plaintext secret (only time it's visible)
	result := *app
	result.ClientSecret = clientSecret
	result.RegistrationTokenHash = ""

	return &result, nil
}

// ValidateClient validates a client ID and secret.
func (m *AppManager) ValidateClient(ctx context.Context, clientID, clientSecret string) (*Application, error) {
	// Get application
	app, err := m.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, &IAMMessageError{Message: "invalid client credentials"}
	}

	// Validate secret
	valid, err := m.storage.ValidateClientSecret(ctx, clientID, clientSecret)
	if err != nil || !valid {
		return nil, &IAMMessageError{Message: "invalid client credentials"}
	}

	return redactAppSecrets(app), nil
}

// ValidateClientCredentials validates client credentials and returns whether they are valid.
func (m *AppManager) ValidateClientCredentials(ctx context.Context, clientID, clientSecret string) (bool, error) {
	_, err := m.ValidateClient(ctx, clientID, clientSecret)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Helper methods

func (m *AppManager) validateRegisterOptions(opts RegisterAppOptions) error {
	if opts.Name == "" {
		return &IAMMessageError{Message: "application name is required"}
	}

	// Redirect URIs are only required for interactive grants (authorization
	// code, implicit, hybrid). A client configured exclusively for
	// non-interactive grants (client_credentials, device, JWT bearer) does
	// not use redirect URIs at all — per RFC 6749 §4.4 they are irrelevant.
	if appRequiresRedirectURIs(opts.GrantTypes) {
		if err := validateRedirectURIs(opts.RedirectURIs); err != nil {
			return err
		}
	}

	// Validate grant types (if provided) - empty is allowed since defaults will be applied
	for _, gt := range opts.GrantTypes {
		if !isValidGrantType(gt) {
			return newInvalidGrantTypeError(gt)
		}
	}

	// Validate the per-app default service audience (empty = no default).
	if err := ValidateDefaultAudience(opts.DefaultAudience); err != nil {
		return &IAMContextError{Message: "invalid default audience", Cause: err}
	}

	return nil
}

// appRequiresRedirectURIs reports whether any of the provided grant types is
// an interactive grant that uses redirect URIs. When the grant types list is
// empty we return true because the defaults applied in RegisterApp include
// authorization_code, which does require redirects.
func appRequiresRedirectURIs(grants []string) bool {
	if len(grants) == 0 {
		return true
	}
	for _, g := range grants {
		if g == GrantTypeAuthorizationCode {
			return true
		}
	}
	return false
}

// RedirectURIFragmentError reports a redirect URI that carries a fragment
// component, which draft-ietf-oauth-v2-1 §2.3 forbids.
type RedirectURIFragmentError struct {
	URI string
}

func (e *RedirectURIFragmentError) Error() string {
	return "redirect URI must not contain a fragment: " + e.URI
}

// validateRedirectURIs checks a non-empty set of redirect URIs with
// validateRedirectURI. Registration and update share it so both refuse the
// same URIs with the same error.
func validateRedirectURIs(uris []string) error {
	if len(uris) == 0 {
		return &InvalidRedirectURIError{Reason: "at least one redirect URI is required"}
	}
	for _, uri := range uris {
		if err := validateRedirectURI(uri); err != nil {
			return err
		}
	}
	return nil
}

// validateRedirectURI validates a redirect URI according to OAuth2
// requirements. Every refusal is an *InvalidRedirectURIError.
func validateRedirectURI(uri string) error {
	if uri == "" {
		return &InvalidRedirectURIError{Reason: "redirect URI cannot be empty"}
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return &InvalidRedirectURIError{URI: uri, Reason: "invalid URI format", Cause: err}
	}

	// Must not have a fragment, not even an empty one (draft-ietf-oauth-v2-1
	// §2.3): url.Parse leaves Fragment empty for a trailing bare '#', so
	// check the raw string.
	if strings.Contains(uri, "#") {
		return &InvalidRedirectURIError{
			URI:    uri,
			Reason: "redirect URI must not contain a fragment",
			Cause:  &RedirectURIFragmentError{URI: uri},
		}
	}

	// Must have a scheme
	if parsed.Scheme == "" {
		return &InvalidRedirectURIError{URI: uri, Reason: "URI must have a scheme"}
	}

	// HTTP is only allowed for localhost (development)
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return &InvalidRedirectURIError{URI: uri, Reason: "HTTP scheme is only allowed for localhost"}
		}
	} else if parsed.Scheme != "https" {
		return &InvalidRedirectURIError{URI: uri, Reason: "URI scheme must be HTTPS (or HTTP for localhost)"}
	}

	return nil
}

// isValidGrantType checks if a grant type is a valid OAuth2.1 grant type.
// The password and implicit grant types are excluded per OAuth 2.1 (draft-ietf-oauth-v2-1).
func isValidGrantType(grantType string) bool {
	validGrantTypes := map[string]bool{
		"authorization_code": true,
		"client_credentials": true,
		"refresh_token":      true,
		"urn:ietf:params:oauth:grant-type:device_code": true,
	}
	return validGrantTypes[grantType]
}

// Helper functions

// clientIDSuffixEncoding encodes the random segment of a client identifier.
//
// It is base32 (RFC 4648, unpadded) rather than base64url deliberately: the
// identifier is tenant + ClientIDSeparator + suffix, and TenantFromClientID
// recovers the tenant by splitting on the LAST separator. base64url's alphabet
// contains "_", so roughly one suffix in six carried the separator and the
// split cut the identifier in the wrong place — the client was then looked up
// under a tenant that does not exist and could never authenticate
// (go-quicbaas#644). base32's alphabet is A-Z2-7, which is disjoint from the
// separator, so the split is unambiguous for every suffix the generator can
// produce, whatever the tenant id contains.
var clientIDSuffixEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateClientID generates a unique client ID for an application. The tenant
// segment is recoverable with TenantFromClientID, which is how every token
// endpoint scopes its storage context before resolving the client.
func generateClientID(tenantID string) (string, error) {
	// Generate random suffix
	b, err := xkms.Rand(8)
	if err != nil {
		return "", err
	}
	suffix := clientIDSuffixEncoding.EncodeToString(b)

	return ClientIDPrefix + tenantID + ClientIDSeparator + suffix, nil
}

// generateClientSecret generates a secure client secret and its hash.
// Returns (plaintext_secret, hashed_secret, error).
func generateClientSecret() (string, string, error) {
	// Generate secure random bytes
	b, err := xkms.Rand(32)
	if err != nil {
		return "", "", err
	}

	// Encode as base64
	plaintext := "sk_" + base64.RawURLEncoding.EncodeToString(b)

	// Store the secret as a PBKDF2-HMAC-SHA256 digest (#37). bcrypt's Blowfish
	// core is not FIPS-approved, and x/crypto/bcrypt never enters crypto/* so
	// the fips140=only gate cannot catch it; deriveSecretHash is the KDF the
	// rest of IAM already uses.
	hashed, err := deriveSecretHash(plaintext)
	if err != nil {
		return "", "", err
	}

	return plaintext, hashed, nil
}

// verifyClientSecret checks a plaintext client secret against its stored hash.
//
// Only the current PBKDF2-HMAC-SHA256 digest is verified. Anything else --
// the pre-#37 bcrypt form, or any value that does not parse as a digest this
// package wrote -- is refused outright, in every mode, and reported as
// ErrSecretRotationRequired rather than as a mismatch (owner-ruled
// 2026-08-25). The predicate is orchestrator_auth.go's isLegacySecretHash,
// the same "does not parse" test service-account secrets already use, so an
// unrecognized digest is never fed to a comparison that cannot understand it.
//
// Refusing rather than accepting is the safer half of the migration, for three
// reasons. bcrypt cannot run under GODEBUG=fips140=only at all -- its Blowfish
// core is unapproved and x/crypto/bcrypt never enters crypto/*, so the module
// cannot intercept it -- which would make legacy acceptance dead code in
// exactly the regulated deployments #37 exists to serve. Honouring a stored
// digest's own embedded cost would also let a tampered record dictate how long
// an authentication worker is occupied; secret_kdf.go already refuses that for
// PBKDF2, calling a single accepted work factor "a security property rather
// than a simplification", and accepting arbitrary bcrypt costs would
// contradict it. And refusal removes the cost-pinning denial-of-service class
// outright instead of capping it.
//
// The remedy for a legacy record is rotation, which is why the error is
// distinguishable from bad credentials: an operator reading it should see
// "rotate me", not "authentication is broken". Callers on the wire must still
// not echo it -- see the AuthenticateClient path, which maps every failure to
// one opaque response.
func verifyClientSecret(stored, plaintext string) (bool, error) {
	// Every path performs exactly one derivation, at the accepted work factor.
	// A record with no usable digest is compared against timingEqualizerDigest
	// and the result discarded: returning early instead would make an obsolete
	// or malformed record answer in microseconds while a current one costs
	// pbkdf2Iterations, so response latency would tell an unauthenticated
	// caller which client IDs exist and how their secrets are stored. That is
	// the enumeration channel secret_kdf.go refuses for the work factor, and
	// collapsing the error text alone does not close it (Codex review of #118).
	if isLegacySecretHash(stored) {
		_ = verifySecret(timingEqualizerDigest, plaintext)
		return false, &SecretRotationRequiredError{}
	}
	return verifySecret(stored, plaintext), nil
}

// StorageOption configures an IAM storage implementation. It is variadic on
// the constructors so adding one never breaks an existing caller.
type StorageOption func(*storageOptions)

type storageOptions struct {
	logger storageLogger
}

// storageLogger is the smallest logger a storage needs: one leveled formatter.
// Narrowing it to this rather than interfaces.Logger is what lets the server
// pass its concrete *logger.Logger directly -- that type satisfies Warnf but
// not the full interface, whose WithError returns a different logger type.
type storageLogger interface {
	Warnf(format string, args ...interface{})
}

// WithStorageLogger gives a storage a logger for operator-facing events.
// Without one those events are silently dropped, which is the pre-existing
// behavior and is why this is an option rather than a required argument.
func WithStorageLogger(l storageLogger) StorageOption {
	return func(o *storageOptions) { o.logger = l }
}

func applyStorageOptions(opts []StorageOption) storageOptions {
	var o storageOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return o
}

// warnRotationRequired records that a stored client secret predates the
// current KDF and the application must be rotated.
//
// Without it the refusal is invisible: ErrSecretRotationRequired is collapsed
// into the same opaque response as any other authentication failure before it
// reaches the caller, so an operator upgrading a deployment that still holds
// legacy records would see a wave of invalid_client with nothing anywhere
// naming rotation as the remedy. This mirrors what AuthenticateServiceAccount
// already does for service accounts (#118 F6).
func warnRotationRequired(l storageLogger, clientID string) {
	if l == nil {
		return
	}
	l.Warnf("oauth2 application holds a pre-KDF client secret; rotation required (see AppManager.RotateSecret or iam.HashClientSecret): client_id=%s", clientID)
}

// constantTimeCompare performs constant-time string comparison.
func constantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
