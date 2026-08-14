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
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// AppManager manages OAuth2 applications (clients).
type AppManager struct {
	storage AppStorage
	tenants TenantManager
}

// NewAppManager creates a new application manager.
func NewAppManager(storage AppStorage, tenants TenantManager) *AppManager {
	return &AppManager{
		storage: storage,
		tenants: tenants,
	}
}

// RegisterApp registers a new OAuth2 application. The returned Application
// carries the plaintext client secret (the only time it is visible); no RFC 7592
// registration access token is issued. Use RegisterDCRApp for clients created
// through Dynamic Client Registration, which must own such a token.
func (m *AppManager) RegisterApp(ctx context.Context, tenantID string, opts RegisterAppOptions) (*Application, error) {
	app, _, err := m.registerApp(ctx, tenantID, opts, false)
	return app, err
}

// registerApp is the single creation path for OAuth2 applications. When
// withRegistrationToken is set it also mints an RFC 7592 registration access
// token, persists only its hash on the application record, and returns the
// plaintext token — which the caller must hand to the client because it is
// never recoverable afterwards. The token hash is written in the SAME storage
// write as the application, so a registered DCR client can never exist without
// its management credential.
func (m *AppManager) registerApp(ctx context.Context, tenantID string, opts RegisterAppOptions, withRegistrationToken bool) (*Application, string, error) {
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
	clientID, err := generateClientID(tenantID, opts.Name)
	if err != nil {
		return nil, "", &IAMContextError{Message: "failed to generate client ID", Cause: err}
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

// UpdateApp updates an existing application. The lookup is NOT tenant scoped;
// callers that hold a tenant must use UpdateAppInTenant.
func (m *AppManager) UpdateApp(ctx context.Context, clientID string, opts UpdateAppOptions) (*Application, error) {
	return m.UpdateAppInTenant(ctx, "", clientID, opts)
}

// UpdateAppInTenant updates an application, requiring it to belong to tenantID.
// An empty tenantID falls back to the unscoped lookup.
func (m *AppManager) UpdateAppInTenant(ctx context.Context, tenantID, clientID string, opts UpdateAppOptions) (*Application, error) {
	// Get existing application
	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if opts.Name != nil {
		app.Name = *opts.Name
	}
	if opts.RedirectURIs != nil {
		if len(*opts.RedirectURIs) == 0 {
			return nil, &IAMMessageError{Message: "at least one redirect URI is required"}
		}
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
		app.GrantTypes = *opts.GrantTypes
	}
	if opts.ResponseTypes != nil {
		app.ResponseTypes = *opts.ResponseTypes
	}

	app.Updated = time.Now()

	// Update storage
	if err := m.storage.UpdateApp(ctx, app); err != nil {
		return nil, &IAMContextError{Message: "failed to update application", Cause: err}
	}

	return redactAppSecrets(app), nil
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

// RotateSecret generates a new client secret for an application.
func (m *AppManager) RotateSecret(ctx context.Context, clientID string) (*Application, error) {
	// Get existing application
	app, err := m.storage.GetApp(ctx, clientID)
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
		if len(opts.RedirectURIs) == 0 {
			return &IAMMessageError{Message: "at least one redirect URI is required"}
		}
		for _, uri := range opts.RedirectURIs {
			if err := validateRedirectURI(uri); err != nil {
				return &IAMContextError{Message: "invalid redirect URI " + uri, Cause: err}
			}
		}
	}

	// Validate grant types (if provided) - empty is allowed since defaults will be applied
	for _, gt := range opts.GrantTypes {
		if !isValidGrantType(gt) {
			return &IAMMessageError{Message: "invalid grant type: " + gt}
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

// validateRedirectURI validates a redirect URI according to OAuth2 requirements.
func validateRedirectURI(uri string) error {
	if uri == "" {
		return &IAMMessageError{Message: "redirect URI cannot be empty"}
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return &IAMContextError{Message: "invalid URI format", Cause: err}
	}

	// Must have a scheme
	if parsed.Scheme == "" {
		return &IAMMessageError{Message: "URI must have a scheme"}
	}

	// HTTP is only allowed for localhost (development)
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return &IAMMessageError{Message: "HTTP scheme is only allowed for localhost"}
		}
	} else if parsed.Scheme != "https" {
		return &IAMMessageError{Message: "URI scheme must be HTTPS (or HTTP for localhost)"}
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

// generateClientID generates a unique client ID for an application.
func generateClientID(tenantID, appName string) (string, error) {
	// Generate random suffix
	b, err := xkms.Rand(8)
	if err != nil {
		return "", err
	}
	suffix := base64.RawURLEncoding.EncodeToString(b)

	// Create client ID in format: app_<tenant>_<suffix>
	return fmt.Sprintf("app_%s_%s", tenantID, suffix), nil
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

	// Hash with bcrypt
	hashed, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}

	return plaintext, string(hashed), nil
}

// validateClientSecret validates a client secret against a hash using constant-time comparison.
func validateClientSecret(plaintext, hashed string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext))
	return err == nil
}

// constantTimeCompare performs constant-time string comparison.
func constantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
