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
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Mock AppStorage for testing
type mockAppStorage struct {
	apps map[string]*Application
}

func newMockAppStorage() *mockAppStorage {
	return &mockAppStorage{
		apps: make(map[string]*Application),
	}
}

func (m *mockAppStorage) CreateApp(ctx context.Context, app *Application) error {
	if _, exists := m.apps[app.ClientID]; exists {
		return ErrTenantAlreadyExists
	}
	m.apps[app.ClientID] = app
	return nil
}

func (m *mockAppStorage) GetApp(ctx context.Context, clientID string) (*Application, error) {
	app, ok := m.apps[clientID]
	if !ok {
		return nil, ErrUserNotFound
	}
	return app, nil
}

func (m *mockAppStorage) GetAppByTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	app, ok := m.apps[clientID]
	if !ok || app.TenantID != tenantID {
		return nil, ErrUserNotFound
	}
	return app, nil
}

func (m *mockAppStorage) UpdateApp(ctx context.Context, app *Application) error {
	if _, ok := m.apps[app.ClientID]; !ok {
		return ErrUserNotFound
	}
	m.apps[app.ClientID] = app
	return nil
}

func (m *mockAppStorage) DeleteApp(ctx context.Context, clientID string) error {
	if _, ok := m.apps[clientID]; !ok {
		return ErrUserNotFound
	}
	delete(m.apps, clientID)
	return nil
}

func (m *mockAppStorage) ListApps(ctx context.Context, tenantID string, opts ListOptions) ([]*Application, error) {
	var result []*Application
	for _, app := range m.apps {
		if app.TenantID == tenantID {
			result = append(result, app)
		}
	}
	return result, nil
}

func (m *mockAppStorage) ValidateClientSecret(ctx context.Context, clientID, clientSecret string) (bool, error) {
	app, ok := m.apps[clientID]
	if !ok {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(app.ClientSecret), []byte(clientSecret))
	return err == nil, nil
}

func createTestAppManager(t *testing.T) *AppManager {
	storage := newMockAppStorage()
	return NewAppManager(storage, nil)
}

func TestAppManager_RegisterApp(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	opts := RegisterAppOptions{
		Name:           "Test App",
		RedirectURIs:   []string{"https://app.example.com/callback"},
		ResourcePrefix: "app://",
		GrantTypes:     []string{GrantTypeAuthorizationCode, GrantTypeRefreshToken},
		ResponseTypes:  []string{ResponseTypeCode},
	}

	app, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	if app.ClientID == "" {
		t.Error("ClientID is empty")
	}

	if app.ClientSecret == "" {
		t.Error("ClientSecret is empty")
	}

	if !strings.HasPrefix(app.ClientID, "app_tenant-1_") {
		t.Errorf("ClientID format is incorrect: %s", app.ClientID)
	}

	if !strings.HasPrefix(app.ClientSecret, "sk_") {
		t.Errorf("ClientSecret format is incorrect: %s", app.ClientSecret)
	}

	if app.Name != opts.Name {
		t.Errorf("Name = %v, want %v", app.Name, opts.Name)
	}

	if app.TenantID != "tenant-1" {
		t.Errorf("TenantID = %v, want tenant-1", app.TenantID)
	}

	if len(app.RedirectURIs) != 1 || app.RedirectURIs[0] != opts.RedirectURIs[0] {
		t.Errorf("RedirectURIs = %v, want %v", app.RedirectURIs, opts.RedirectURIs)
	}

	if len(app.GrantTypes) != 2 {
		t.Errorf("GrantTypes length = %d, want 2", len(app.GrantTypes))
	}
}

func TestAppManager_RegisterApp_Defaults(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
		// GrantTypes and ResponseTypes not specified
	}

	app, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	// Should set defaults
	if len(app.GrantTypes) == 0 {
		t.Error("GrantTypes should have defaults")
	}

	if len(app.ResponseTypes) == 0 {
		t.Error("ResponseTypes should have defaults")
	}

	// Verify default grant types
	hasAuthCode := false
	hasRefresh := false
	for _, gt := range app.GrantTypes {
		if gt == GrantTypeAuthorizationCode {
			hasAuthCode = true
		}
		if gt == GrantTypeRefreshToken {
			hasRefresh = true
		}
	}

	if !hasAuthCode {
		t.Error("default GrantTypes should include authorization_code")
	}

	if !hasRefresh {
		t.Error("default GrantTypes should include refresh_token")
	}
}

func TestAppManager_RegisterApp_ValidationErrors(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    RegisterAppOptions
		wantErr bool
	}{
		{
			name: "missing name",
			opts: RegisterAppOptions{
				RedirectURIs: []string{"https://app.example.com/callback"},
			},
			wantErr: true,
		},
		{
			name: "missing redirect URIs",
			opts: RegisterAppOptions{
				Name: "Test App",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.RegisterApp(ctx, "tenant-1", tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterApp() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAppManager_GetApp(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app first
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	// Get the app
	app, err := manager.GetApp(ctx, registered.ClientID)
	if err != nil {
		t.Fatalf("GetApp() error = %v", err)
	}

	if app.ClientID != registered.ClientID {
		t.Errorf("ClientID = %v, want %v", app.ClientID, registered.ClientID)
	}

	// Client secret should not be returned
	if app.ClientSecret != "" {
		t.Error("GetApp() should not return client secret")
	}
}

func TestAppManager_UpdateApp(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app first
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	// Update the app
	newName := "Updated App"
	newRedirectURIs := []string{"https://app.example.com/callback", "https://app2.example.com/callback"}
	updateOpts := UpdateAppOptions{
		Name:         &newName,
		RedirectURIs: &newRedirectURIs,
	}

	updated, err := manager.UpdateApp(ctx, registered.ClientID, updateOpts)
	if err != nil {
		t.Fatalf("UpdateApp() error = %v", err)
	}

	if updated.Name != newName {
		t.Errorf("Name = %v, want %v", updated.Name, newName)
	}

	if len(updated.RedirectURIs) != 2 {
		t.Errorf("RedirectURIs length = %d, want 2", len(updated.RedirectURIs))
	}

	// Updated timestamp should be set
	if updated.Updated.Before(registered.Created) {
		t.Error("Updated timestamp should be after Created")
	}
}

func TestAppManager_DeleteApp(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app first
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	// Delete the app
	if err := manager.DeleteApp(ctx, registered.ClientID); err != nil {
		t.Fatalf("DeleteApp() error = %v", err)
	}

	// Verify it's deleted
	_, err = manager.GetApp(ctx, registered.ClientID)
	if err == nil {
		t.Error("GetApp() should return error for deleted app")
	}
}

func TestAppManager_ListApps(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register multiple apps
	for i := 0; i < 3; i++ {
		opts := RegisterAppOptions{
			Name:         "Test App",
			RedirectURIs: []string{"https://app.example.com/callback"},
		}
		if _, err := manager.RegisterApp(ctx, "tenant-1", opts); err != nil {
			t.Fatalf("RegisterApp() error = %v", err)
		}
	}

	// List apps
	apps, err := manager.ListApps(ctx, "tenant-1", ListOptions{})
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}

	if len(apps) != 3 {
		t.Errorf("ListApps() returned %d apps, want 3", len(apps))
	}

	// Verify secrets are not returned
	for _, app := range apps {
		if app.ClientSecret != "" {
			t.Error("ListApps() should not return client secrets")
		}
	}
}

func TestAppManager_RotateSecret(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app first
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	originalSecret := registered.ClientSecret

	// Wait a moment to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Rotate the secret
	rotated, err := manager.RotateSecret(ctx, registered.ClientID)
	if err != nil {
		t.Fatalf("RotateSecret() error = %v", err)
	}

	if rotated.ClientSecret == "" {
		t.Error("RotateSecret() returned empty secret")
	}

	if rotated.ClientSecret == originalSecret {
		t.Error("RotateSecret() should return a new secret")
	}

	// Updated timestamp should be set
	if !rotated.Updated.After(registered.Created) {
		t.Error("Updated timestamp should be after Created")
	}
}

func TestAppManager_ValidateClient(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	// Test valid credentials
	app, err := manager.ValidateClient(ctx, registered.ClientID, registered.ClientSecret)
	if err != nil {
		t.Errorf("ValidateClient() error = %v", err)
	}

	if app == nil {
		t.Fatal("ValidateClient() returned nil app")
	}

	if app.ClientID != registered.ClientID {
		t.Errorf("ClientID = %v, want %v", app.ClientID, registered.ClientID)
	}

	// Test invalid secret
	_, err = manager.ValidateClient(ctx, registered.ClientID, "wrong-secret")
	if err == nil {
		t.Error("ValidateClient() should fail with invalid secret")
	}

	// Test invalid client ID
	_, err = manager.ValidateClient(ctx, "nonexistent", registered.ClientSecret)
	if err == nil {
		t.Error("ValidateClient() should fail with invalid client ID")
	}
}

func TestGenerateClientID(t *testing.T) {
	id1, err := generateClientID("tenant-1", "App 1")
	if err != nil {
		t.Fatalf("generateClientID() error = %v", err)
	}

	id2, err := generateClientID("tenant-1", "App 2")
	if err != nil {
		t.Fatalf("generateClientID() error = %v", err)
	}

	if id1 == id2 {
		t.Error("generateClientID() should return unique IDs")
	}

	if !strings.HasPrefix(id1, "app_tenant-1_") {
		t.Errorf("client ID format is incorrect: %s", id1)
	}
}

func TestGenerateClientSecret(t *testing.T) {
	plaintext1, hashed1, err := generateClientSecret()
	if err != nil {
		t.Fatalf("generateClientSecret() error = %v", err)
	}

	plaintext2, hashed2, err := generateClientSecret()
	if err != nil {
		t.Fatalf("generateClientSecret() error = %v", err)
	}

	// Plaintexts should be different
	if plaintext1 == plaintext2 {
		t.Error("generateClientSecret() should return unique secrets")
	}

	// Hashes should be different
	if hashed1 == hashed2 {
		t.Error("generateClientSecret() should return unique hashes")
	}

	// Plaintext should start with sk_
	if !strings.HasPrefix(plaintext1, "sk_") {
		t.Errorf("plaintext format is incorrect: %s", plaintext1)
	}

	// Should be able to verify plaintext against hash
	if !validateClientSecret(plaintext1, hashed1) {
		t.Error("validateClientSecret() failed for valid secret")
	}

	if validateClientSecret(plaintext2, hashed1) {
		t.Error("validateClientSecret() should fail for wrong secret")
	}
}

func TestValidateClientSecret(t *testing.T) {
	plaintext, hashed, err := generateClientSecret()
	if err != nil {
		t.Fatalf("generateClientSecret() error = %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		hashed    string
		want      bool
	}{
		{
			name:      "valid secret",
			plaintext: plaintext,
			hashed:    hashed,
			want:      true,
		},
		{
			name:      "invalid secret",
			plaintext: "wrong-secret",
			hashed:    hashed,
			want:      false,
		},
		{
			name:      "empty secret",
			plaintext: "",
			hashed:    hashed,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateClientSecret(tt.plaintext, tt.hashed); got != tt.want {
				t.Errorf("validateClientSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConstantTimeCompare(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "equal strings",
			a:    "hello world",
			b:    "hello world",
			want: true,
		},
		{
			name: "different strings same length",
			a:    "hello world",
			b:    "hello there",
			want: false,
		},
		{
			name: "different strings different length",
			a:    "hello",
			b:    "hello world",
			want: false,
		},
		{
			name: "empty strings",
			a:    "",
			b:    "",
			want: true,
		},
		{
			name: "one empty one non-empty",
			a:    "hello",
			b:    "",
			want: false,
		},
		{
			name: "special characters equal",
			a:    "test@#$%^&*()_+{}",
			b:    "test@#$%^&*()_+{}",
			want: true,
		},
		{
			name: "special characters different",
			a:    "test@#$%^&*()_+{}",
			b:    "test@#$%^&*()_+{]",
			want: false,
		},
		{
			name: "unicode equal",
			a:    "hello世界",
			b:    "hello世界",
			want: true,
		},
		{
			name: "unicode different",
			a:    "hello世界",
			b:    "hello世间",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constantTimeCompare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("constantTimeCompare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
