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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Sentinel errors for orchestrator service account operations.
var (
	// ErrServiceAccountNotFound indicates a service account was not found.
	ErrServiceAccountNotFound = errors.New("orchestrator: service account not found")

	// ErrServiceAccountExists indicates a service account with the same name already exists.
	ErrServiceAccountExists = errors.New("orchestrator: service account already exists")

	// ErrDeviceCertAlreadyBound indicates the device certificate is already bound to a service account.
	ErrDeviceCertAlreadyBound = errors.New("orchestrator: device certificate already bound")

	// ErrInvalidScope indicates an invalid OAuth scope was requested.
	ErrInvalidScope = errors.New("orchestrator: invalid scope")

	// ErrServiceAccountRequired indicates a required field is missing.
	ErrServiceAccountRequired = errors.New("orchestrator: required field missing")
)

// validScopeRegex validates that a scope follows the resource:action format.
// Accepts two-segment (e.g. "kms:read") or three-segment (e.g. "kms:keys:read")
// formats. Each segment must start with a letter or wildcard and contain only
// alphanumeric characters, underscores, hyphens, or wildcards.
var validScopeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*:[a-zA-Z*][a-zA-Z0-9_*-]*(?::[a-zA-Z*][a-zA-Z0-9_*-]*)?$`)

// IsValidScope checks whether a scope string has a valid resource:action format.
func IsValidScope(scope string) bool {
	return validScopeRegex.MatchString(scope)
}

// ServiceAccountNotFoundError indicates a service account was not found.
type ServiceAccountNotFoundError struct {
	AccountID string
}

// Error implements the error interface.
func (e *ServiceAccountNotFoundError) Error() string {
	return ErrServiceAccountNotFound.Error() + ": " + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServiceAccountNotFoundError) Unwrap() error {
	return ErrServiceAccountNotFound
}

// ServiceAccountExistsError indicates a service account with the same name already exists in the tenant.
type ServiceAccountExistsError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *ServiceAccountExistsError) Error() string {
	return ErrServiceAccountExists.Error() + ": tenant=" + e.TenantID + " name=" + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServiceAccountExistsError) Unwrap() error {
	return ErrServiceAccountExists
}

// DeviceCertAlreadyBoundError indicates the device certificate CN is already bound.
type DeviceCertAlreadyBoundError struct {
	CertCN    string
	AccountID string
}

// Error implements the error interface.
func (e *DeviceCertAlreadyBoundError) Error() string {
	return ErrDeviceCertAlreadyBound.Error() + ": cn=" + e.CertCN + " account=" + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DeviceCertAlreadyBoundError) Unwrap() error {
	return ErrDeviceCertAlreadyBound
}

// InvalidScopeError indicates an invalid OAuth scope was requested.
type InvalidScopeError struct {
	Scope string
}

// Error implements the error interface.
func (e *InvalidScopeError) Error() string {
	return ErrInvalidScope.Error() + ": " + e.Scope
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidScopeError) Unwrap() error {
	return ErrInvalidScope
}

// ServiceAccountRequiredError indicates a required field is missing.
type ServiceAccountRequiredError struct {
	Field string
}

// Error implements the error interface.
func (e *ServiceAccountRequiredError) Error() string {
	return ErrServiceAccountRequired.Error() + ": " + e.Field
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServiceAccountRequiredError) Unwrap() error {
	return ErrServiceAccountRequired
}

// ServiceAccount represents an LLM agent's identity in the IAM system.
type ServiceAccount struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Name        string            `json:"name"`
	BackendType string            `json:"backend_type"`
	ClientID    string            `json:"client_id"`
	SecretHash  string            `json:"secret_hash"`
	Scopes      []string          `json:"scopes"`
	DeviceCert  string            `json:"device_cert,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Revoked     bool              `json:"revoked"`
}

// CreateServiceAccountRequest contains parameters for creating a service account.
type CreateServiceAccountRequest struct {
	TenantID    string
	Name        string
	BackendType string
	Scopes      []string
	DeviceCert  string
	ExpiresIn   time.Duration
	Metadata    map[string]string
}

// ClientCredentials contains the OAuth2 client credentials for a service account.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// ServiceAccountStore provides persistence operations for service accounts.
type ServiceAccountStore interface {
	// PutServiceAccount persists a service account.
	PutServiceAccount(ctx context.Context, account *ServiceAccount) error

	// GetServiceAccount retrieves a service account by ID.
	GetServiceAccount(ctx context.Context, accountID string) (*ServiceAccount, error)

	// DeleteServiceAccount removes a service account.
	DeleteServiceAccount(ctx context.Context, accountID string) error

	// ListServiceAccounts returns all service accounts for a tenant.
	ListServiceAccounts(ctx context.Context, tenantID string) ([]*ServiceAccount, error)

	// GetServiceAccountByClientID retrieves a service account by OAuth client ID.
	GetServiceAccountByClientID(ctx context.Context, clientID string) (*ServiceAccount, error)

	// GetServiceAccountByDeviceCert retrieves a service account by device certificate CN.
	GetServiceAccountByDeviceCert(ctx context.Context, certCN string) (*ServiceAccount, error)

	// GetServiceAccountByName retrieves a service account by tenant and name.
	GetServiceAccountByName(ctx context.Context, tenantID, name string) (*ServiceAccount, error)
}

// OrchestratorAuthService manages service accounts for LLM agents.
type OrchestratorAuthService struct {
	store  ServiceAccountStore
	logger interfaces.Logger
}

// NewOrchestratorAuthService creates a new OrchestratorAuthService.
func NewOrchestratorAuthService(
	store ServiceAccountStore,
	logger interfaces.Logger,
) (*OrchestratorAuthService, error) {
	if store == nil {
		return nil, &ServiceAccountRequiredError{Field: "store"}
	}
	if logger == nil {
		return nil, &ServiceAccountRequiredError{Field: "logger"}
	}
	return &OrchestratorAuthService{
		store:  store,
		logger: logger,
	}, nil
}

// CreateServiceAccount creates a new service account for an LLM agent.
// Returns the account with client credentials.
func (s *OrchestratorAuthService) CreateServiceAccount(
	ctx context.Context,
	req *CreateServiceAccountRequest,
) (*ServiceAccount, *ClientCredentials, error) {

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if req.TenantID == "" {
		return nil, nil, &ServiceAccountRequiredError{Field: "tenant_id"}
	}
	if req.Name == "" {
		return nil, nil, &ServiceAccountRequiredError{Field: "name"}
	}
	if req.BackendType == "" {
		return nil, nil, &ServiceAccountRequiredError{Field: "backend_type"}
	}
	if len(req.Scopes) == 0 {
		return nil, nil, &ServiceAccountRequiredError{Field: "scopes"}
	}

	// Validate all scopes use valid resource:action format
	for _, scope := range req.Scopes {
		if !IsValidScope(scope) {
			return nil, nil, &InvalidScopeError{Scope: scope}
		}
	}

	// Check for duplicate name within tenant
	existing, err := s.store.GetServiceAccountByName(ctx, req.TenantID, req.Name)
	if err != nil && !errors.Is(err, ErrServiceAccountNotFound) {
		return nil, nil, err
	}
	if existing != nil {
		return nil, nil, &ServiceAccountExistsError{TenantID: req.TenantID, Name: req.Name}
	}

	// Check device cert uniqueness if provided
	if req.DeviceCert != "" {
		existingCert, certErr := s.store.GetServiceAccountByDeviceCert(ctx, req.DeviceCert)
		if certErr != nil && !errors.Is(certErr, ErrServiceAccountNotFound) {
			return nil, nil, certErr
		}
		if existingCert != nil {
			return nil, nil, &DeviceCertAlreadyBoundError{
				CertCN:    req.DeviceCert,
				AccountID: existingCert.ID,
			}
		}
	}

	// Generate unique IDs
	accountID, err := generateRandomHex(16)
	if err != nil {
		return nil, nil, &ServiceAccountRequiredError{Field: "id generation failed"}
	}

	clientID, err := generateRandomHex(16)
	if err != nil {
		return nil, nil, &ServiceAccountRequiredError{Field: "client_id generation failed"}
	}

	clientSecret, err := generateRandomHex(32)
	if err != nil {
		return nil, nil, &ServiceAccountRequiredError{Field: "client_secret generation failed"}
	}

	now := time.Now().UTC()

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := now.Add(req.ExpiresIn)
		expiresAt = &t
	}

	account := &ServiceAccount{
		ID:          accountID,
		TenantID:    req.TenantID,
		Name:        req.Name,
		BackendType: req.BackendType,
		ClientID:    clientID,
		SecretHash:  hashSecret(clientSecret),
		Scopes:      req.Scopes,
		DeviceCert:  req.DeviceCert,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Metadata:    req.Metadata,
		Revoked:     false,
	}

	if err := s.store.PutServiceAccount(ctx, account); err != nil {
		return nil, nil, err
	}

	creds := &ClientCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}

	s.logger.Debugf("created service account: id=%s tenant=%s name=%s", accountID, req.TenantID, req.Name)

	return account, creds, nil
}

// RotateCredentials rotates the client secret for a service account.
func (s *OrchestratorAuthService) RotateCredentials(ctx context.Context, accountID string) (*ClientCredentials, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if accountID == "" {
		return nil, &ServiceAccountRequiredError{Field: "account_id"}
	}

	account, err := s.store.GetServiceAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if account.Revoked {
		return nil, &ServiceAccountNotFoundError{AccountID: accountID}
	}

	newSecret, err := generateRandomHex(32)
	if err != nil {
		return nil, &ServiceAccountRequiredError{Field: "secret generation failed"}
	}

	account.SecretHash = hashSecret(newSecret)

	if err := s.store.PutServiceAccount(ctx, account); err != nil {
		return nil, err
	}

	s.logger.Debugf("rotated credentials: account=%s", accountID)

	return &ClientCredentials{
		ClientID:     account.ClientID,
		ClientSecret: newSecret,
	}, nil
}

// RevokeServiceAccount revokes a service account and all its tokens.
func (s *OrchestratorAuthService) RevokeServiceAccount(ctx context.Context, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if accountID == "" {
		return &ServiceAccountRequiredError{Field: "account_id"}
	}

	account, err := s.store.GetServiceAccount(ctx, accountID)
	if err != nil {
		return err
	}

	account.Revoked = true

	if err := s.store.PutServiceAccount(ctx, account); err != nil {
		return err
	}

	s.logger.Debugf("revoked service account: id=%s tenant=%s", accountID, account.TenantID)

	return nil
}

// BindDeviceCertificate binds a device certificate to a service account.
func (s *OrchestratorAuthService) BindDeviceCertificate(ctx context.Context, accountID, certCN string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if accountID == "" {
		return &ServiceAccountRequiredError{Field: "account_id"}
	}
	if certCN == "" {
		return &ServiceAccountRequiredError{Field: "cert_cn"}
	}

	// Check that the cert CN is not already bound
	existing, err := s.store.GetServiceAccountByDeviceCert(ctx, certCN)
	if err != nil && !errors.Is(err, ErrServiceAccountNotFound) {
		return err
	}
	if existing != nil {
		return &DeviceCertAlreadyBoundError{CertCN: certCN, AccountID: existing.ID}
	}

	account, err := s.store.GetServiceAccount(ctx, accountID)
	if err != nil {
		return err
	}

	account.DeviceCert = certCN

	if err := s.store.PutServiceAccount(ctx, account); err != nil {
		return err
	}

	s.logger.Debugf("bound device cert: account=%s cn=%s", accountID, certCN)

	return nil
}

// GetByDeviceCert looks up a service account by its bound device certificate CN.
func (s *OrchestratorAuthService) GetByDeviceCert(ctx context.Context, certCN string) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if certCN == "" {
		return nil, &ServiceAccountRequiredError{Field: "cert_cn"}
	}

	return s.store.GetServiceAccountByDeviceCert(ctx, certCN)
}

// ListServiceAccounts lists service accounts for a tenant.
func (s *OrchestratorAuthService) ListServiceAccounts(ctx context.Context, tenantID string) ([]*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if tenantID == "" {
		return nil, &ServiceAccountRequiredError{Field: "tenant_id"}
	}

	return s.store.ListServiceAccounts(ctx, tenantID)
}

// =============================================================================
// Internal Helpers
// =============================================================================

// generateRandomHex produces a random hex-encoded string of the given byte length.
func generateRandomHex(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashSecret produces a simple hex-encoded hash of the client secret.
// In production, this should use bcrypt or argon2; for now we use a
// SHA-256-style approach via hex encoding of the raw secret bytes.
// This provides a deterministic mapping for testing while being
// replaceable with a proper KDF.
func hashSecret(secret string) string {
	return hex.EncodeToString([]byte(secret))
}

// =============================================================================
// In-Memory ServiceAccountStore
// =============================================================================

// inMemoryServiceAccountStore implements ServiceAccountStore using sync.Map.
type inMemoryServiceAccountStore struct {
	accounts sync.Map // map[accountID]*ServiceAccount
}

// NewInMemoryServiceAccountStore creates a new in-memory service account store.
func NewInMemoryServiceAccountStore() ServiceAccountStore {
	return &inMemoryServiceAccountStore{}
}

// PutServiceAccount persists a service account in memory.
func (s *inMemoryServiceAccountStore) PutServiceAccount(ctx context.Context, account *ServiceAccount) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if account == nil {
		return &ServiceAccountRequiredError{Field: "account"}
	}
	s.accounts.Store(account.ID, account)
	return nil
}

// GetServiceAccount retrieves a service account by ID.
func (s *inMemoryServiceAccountStore) GetServiceAccount(ctx context.Context, accountID string) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	val, ok := s.accounts.Load(accountID)
	if !ok {
		return nil, &ServiceAccountNotFoundError{AccountID: accountID}
	}
	account, ok := val.(*ServiceAccount)
	if !ok {
		return nil, &ServiceAccountNotFoundError{AccountID: accountID}
	}
	return account, nil
}

// DeleteServiceAccount removes a service account.
func (s *inMemoryServiceAccountStore) DeleteServiceAccount(ctx context.Context, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, loaded := s.accounts.LoadAndDelete(accountID)
	if !loaded {
		return &ServiceAccountNotFoundError{AccountID: accountID}
	}
	return nil
}

// ListServiceAccounts returns all service accounts for a tenant.
func (s *inMemoryServiceAccountStore) ListServiceAccounts(ctx context.Context, tenantID string) ([]*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []*ServiceAccount
	s.accounts.Range(func(key, value interface{}) bool {
		account, ok := value.(*ServiceAccount)
		if !ok {
			return true
		}
		if account.TenantID == tenantID && !account.Revoked {
			result = append(result, account)
		}
		return true
	})
	return result, nil
}

// GetServiceAccountByClientID retrieves a service account by OAuth client ID.
func (s *inMemoryServiceAccountStore) GetServiceAccountByClientID(ctx context.Context, clientID string) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found *ServiceAccount
	s.accounts.Range(func(key, value interface{}) bool {
		account, ok := value.(*ServiceAccount)
		if !ok {
			return true
		}
		if account.ClientID == clientID {
			found = account
			return false
		}
		return true
	})
	if found == nil {
		return nil, &ServiceAccountNotFoundError{AccountID: "client_id=" + clientID}
	}
	return found, nil
}

// GetServiceAccountByDeviceCert retrieves a service account by device certificate CN.
func (s *inMemoryServiceAccountStore) GetServiceAccountByDeviceCert(ctx context.Context, certCN string) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found *ServiceAccount
	s.accounts.Range(func(key, value interface{}) bool {
		account, ok := value.(*ServiceAccount)
		if !ok {
			return true
		}
		if account.DeviceCert == certCN {
			found = account
			return false
		}
		return true
	})
	if found == nil {
		return nil, &ServiceAccountNotFoundError{AccountID: "cert_cn=" + certCN}
	}
	return found, nil
}

// GetServiceAccountByName retrieves a service account by tenant and name.
func (s *inMemoryServiceAccountStore) GetServiceAccountByName(ctx context.Context, tenantID, name string) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found *ServiceAccount
	s.accounts.Range(func(key, value interface{}) bool {
		account, ok := value.(*ServiceAccount)
		if !ok {
			return true
		}
		if account.TenantID == tenantID && strings.EqualFold(account.Name, name) && !account.Revoked {
			found = account
			return false
		}
		return true
	})
	if found == nil {
		return nil, &ServiceAccountNotFoundError{AccountID: "tenant=" + tenantID + " name=" + name}
	}
	return found, nil
}

// Compile-time interface satisfaction checks.
var _ ServiceAccountStore = (*inMemoryServiceAccountStore)(nil)
