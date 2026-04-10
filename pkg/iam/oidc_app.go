//go:build iam

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

// RegisterApp registers a new OAuth2 application.
func (m *AppManager) RegisterApp(ctx context.Context, tenantID string, opts RegisterAppOptions) (*Application, error) {
	// Validate inputs
	if err := m.validateRegisterOptions(opts); err != nil {
		return nil, err
	}

	// Check tenant quota
	if m.tenants != nil {
		if err := m.tenants.CheckQuota(ctx, tenantID, QuotaResourceApps); err != nil {
			return nil, err
		}
	}

	// Generate client ID and secret
	clientID, err := generateClientID(tenantID, opts.Name)
	if err != nil {
		return nil, &IAMContextError{Message: "failed to generate client ID", Cause: err}
	}

	clientSecret, hashedSecret, err := generateClientSecret()
	if err != nil {
		return nil, &IAMContextError{Message: "failed to generate client secret", Cause: err}
	}

	now := time.Now()

	// Create application
	app := &Application{
		ClientID:       clientID,
		ClientSecret:   hashedSecret,
		Name:           opts.Name,
		TenantID:       tenantID,
		RedirectURIs:   opts.RedirectURIs,
		ResourcePrefix: opts.ResourcePrefix,
		GrantTypes:     opts.GrantTypes,
		ResponseTypes:  opts.ResponseTypes,
		Created:        now,
		Updated:        now,
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
		return nil, &IAMContextError{Message: "failed to store application", Cause: err}
	}

	// Return application with plaintext secret (only time it's visible)
	result := *app
	result.ClientSecret = clientSecret

	return &result, nil
}

// GetApp retrieves an application by client ID.
func (m *AppManager) GetApp(ctx context.Context, clientID string) (*Application, error) {
	app, err := m.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}

	// Don't return the hashed secret
	app.ClientSecret = ""
	return app, nil
}

// UpdateApp updates an existing application.
func (m *AppManager) UpdateApp(ctx context.Context, clientID string, opts UpdateAppOptions) (*Application, error) {
	// Get existing application
	app, err := m.storage.GetApp(ctx, clientID)
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

	// Don't return the hashed secret
	app.ClientSecret = ""
	return app, nil
}

// DeleteApp deletes an application.
func (m *AppManager) DeleteApp(ctx context.Context, clientID string) error {
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
		app.ClientSecret = ""
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

	// Don't return the hashed secret
	app.ClientSecret = ""
	return app, nil
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
	if len(opts.RedirectURIs) == 0 {
		return &IAMMessageError{Message: "at least one redirect URI is required"}
	}

	// Validate redirect URIs
	for _, uri := range opts.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			return &IAMContextError{Message: "invalid redirect URI " + uri, Cause: err}
		}
	}

	// Validate grant types (if provided) - empty is allowed since defaults will be applied
	for _, gt := range opts.GrantTypes {
		if !isValidGrantType(gt) {
			return &IAMMessageError{Message: "invalid grant type: " + gt}
		}
	}

	return nil
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

// isValidGrantType checks if a grant type is a valid OAuth2 grant type.
func isValidGrantType(grantType string) bool {
	validGrantTypes := map[string]bool{
		"authorization_code": true,
		"client_credentials": true,
		"refresh_token":      true,
		"implicit":           true,
		"password":           true,
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
