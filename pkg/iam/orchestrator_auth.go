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

	// ErrServiceAccountAuthFailed indicates client-credentials authentication
	// for a service account failed.
	ErrServiceAccountAuthFailed = errors.New("orchestrator: service account authentication failed")

	// ErrSecretHashFailed indicates the client secret could not be hashed.
	ErrSecretHashFailed = errors.New("orchestrator: client secret hashing failed")
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

// ServiceAccountAuthFailedError reports a failed client-credentials
// authentication. It deliberately carries no detail — no client ID, no reason —
// so neither the value nor its message lets a caller tell an unknown client
// from a wrong secret, however the error is logged or serialized downstream.
// The specific reason goes to the service's own audit log instead.
type ServiceAccountAuthFailedError struct{}

// Error implements the error interface.
func (e *ServiceAccountAuthFailedError) Error() string {
	return ErrServiceAccountAuthFailed.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServiceAccountAuthFailedError) Unwrap() error {
	return ErrServiceAccountAuthFailed
}

// errServiceAccountAuthFailed is the single value every authentication failure
// returns. Sharing one instance keeps the failure paths allocation-free and
// makes it impossible for a call site to attach distinguishing detail.
var errServiceAccountAuthFailed = &ServiceAccountAuthFailedError{}

// SecretHashError reports a failure of the client-secret KDF.
type SecretHashError struct {
	Err error
}

// Error implements the error interface.
func (e *SecretHashError) Error() string {
	if e.Err == nil {
		return ErrSecretHashFailed.Error()
	}
	return ErrSecretHashFailed.Error() + ": " + e.Err.Error()
}

// Unwrap returns both the sentinel and the underlying KDF error so
// errors.Is matches either.
func (e *SecretHashError) Unwrap() []error {
	return []error{ErrSecretHashFailed, e.Err}
}

// ServiceAccount represents an LLM agent's identity in the IAM system.
//
// SecretHash holds the PBKDF2 digest of the client secret — never the secret
// itself. Records written before the KDF landed hold the hex-encoded secret
// there instead; see InvalidateLegacySecretHashes.
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

	// ListServiceAccounts returns the live service accounts for a tenant.
	// Revoked accounts are excluded.
	ListServiceAccounts(ctx context.Context, tenantID string) ([]*ServiceAccount, error)

	// ListServiceAccountsIncludingRevoked returns every service account for a
	// tenant, revoked ones included. Credential maintenance has to reach
	// records that authentication never will: a revoked account still holds
	// whatever its SecretHash field stored, which for pre-KDF records is the
	// client secret itself. See InvalidateLegacySecretHashes.
	ListServiceAccountsIncludingRevoked(ctx context.Context, tenantID string) ([]*ServiceAccount, error)

	// ClearSecretHashIfUnchanged clears an account's SecretHash, but only
	// while it still holds expected, and reports whether it did.
	//
	// Implementations MUST make the comparison and the write atomic with
	// respect to their other writers. Credential invalidation necessarily
	// works from a record it read earlier, and a plain read-modify-write
	// would let it clobber anything committed in between: a secret issued by
	// RotateCredentials would be wiped, and — because the stale copy carries
	// every other field too — a RevokeServiceAccount could be undone,
	// resurrecting an account somebody had just shut off. Writing only the
	// one field, and only if nothing has moved underneath, is what makes the
	// operation safe to run against a live store.
	ClearSecretHashIfUnchanged(ctx context.Context, accountID, expected string) (bool, error)

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

	secretHash, err := hashSecret(clientSecret)
	if err != nil {
		return nil, nil, err
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
		SecretHash:  secretHash,
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

	secretHash, err := hashSecret(newSecret)
	if err != nil {
		return nil, err
	}

	account.SecretHash = secretHash

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

// AuthenticateServiceAccount authenticates an OAuth2 client-credentials pair
// against the stored PBKDF2 digest and returns the owning service account.
//
// It is fail-closed and non-enumerating: an unknown client ID, a revoked or
// expired account, a legacy (pre-KDF) or cleared hash, and a wrong secret all
// return the same detail-free *ServiceAccountAuthFailedError, and all of them
// perform exactly one key derivation — against timingEqualizerDigest when
// the account has no usable digest — so neither the error nor the time taken
// reveals whether a client ID exists. The reason is recorded in the audit log.
func (s *OrchestratorAuthService) AuthenticateServiceAccount(
	ctx context.Context,
	clientID, clientSecret string,
) (*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// An empty credential is a malformed request rather than a guess, and it
	// cannot probe anything: both checks are independent of stored state.
	if clientID == "" {
		return nil, s.authFailure("", "empty client_id")
	}
	if clientSecret == "" {
		return nil, s.authFailure(clientID, "empty client_secret")
	}

	account, err := s.store.GetServiceAccountByClientID(ctx, clientID)

	// A missing record is a credential verdict. Anything else — a store
	// outage, a corrupt record, a cancelled or expired context — is an
	// operational failure, and answering it with the opaque authentication
	// error would bury the outage under what looks like a wave of bad
	// passwords, after making every request pay for a key derivation first.
	// Only ErrServiceAccountNotFound continues down the constant-cost path.
	// This leaks nothing: a store failure does not depend on whether the
	// client ID exists.
	if err != nil && !errors.Is(err, ErrServiceAccountNotFound) {
		return nil, err
	}

	// Pick the digest to compare against without returning early: every
	// ineligible account falls back to the equalizer digest so the
	// comparison below costs the same as a real verification.
	digest := timingEqualizerDigest
	reason := ""
	switch {
	case err != nil || account == nil:
		reason = "client not found"
	case account.Revoked:
		reason = "account revoked"
	case account.ExpiresAt != nil && !time.Now().UTC().Before(*account.ExpiresAt):
		reason = "account expired"
	case account.SecretHash == "":
		reason = "no client secret set"
	case isLegacySecretHash(account.SecretHash):
		// A legacy hash is the client secret itself, so it is treated as
		// compromised: never compared against, never migrated in place.
		s.logger.Warnf("service account holds a legacy secret hash; rotation required: account=%s", account.ID)
		reason = "legacy secret hash"
	default:
		digest = account.SecretHash
	}

	matched := verifySecret(digest, clientSecret)
	if reason == "" && !matched {
		reason = "secret mismatch"
	}
	if reason != "" {
		return nil, s.authFailure(clientID, reason)
	}

	return account, nil
}

// authFailure records why an authentication attempt failed and returns the
// opaque error the caller sees. The reason never leaves the audit log.
func (s *OrchestratorAuthService) authFailure(clientID, reason string) error {
	s.logger.Debugf("service account authentication failed: client=%s reason=%s", clientID, reason)
	return errServiceAccountAuthFailed
}

// InvalidateLegacySecretHashes clears every legacy (pre-KDF) secret hash held
// by tenantID's service accounts, revoked ones included, and reports how many
// it cleared.
//
// The legacy form stored the client secret itself rather than a digest of it,
// so the credential is treated as compromised rather than migrated: the
// account survives, authentication fails closed, and RotateCredentials issues
// a fresh secret hashed with the current KDF. Revoked accounts cannot
// authenticate either way, but their stored secret is just as recoverable by
// anyone who reads the store, so they are purged too.
//
// The clear is conditional on the record still holding the same legacy hash,
// and touches only that one field, so running this against a live store cannot
// clobber a credential RotateCredentials issued in the meantime or undo a
// concurrent RevokeServiceAccount. A record that moved underneath is skipped
// rather than overwritten, and is not counted: the returned count describes
// exactly what this call changed.
func (s *OrchestratorAuthService) InvalidateLegacySecretHashes(ctx context.Context, tenantID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if tenantID == "" {
		return 0, &ServiceAccountRequiredError{Field: "tenant_id"}
	}

	accounts, err := s.store.ListServiceAccountsIncludingRevoked(ctx, tenantID)
	if err != nil {
		return 0, err
	}

	invalidated := 0
	for _, account := range accounts {
		// The listed record is a snapshot, so remember the exact value the
		// decision was made on and let the store refuse the write if it is no
		// longer there.
		legacyHash := account.SecretHash
		if legacyHash == "" || !isLegacySecretHash(legacyHash) {
			continue
		}

		cleared, err := s.store.ClearSecretHashIfUnchanged(ctx, account.ID, legacyHash)
		if errors.Is(err, ErrServiceAccountNotFound) {
			// Deleted between the listing and the write. There is no longer a
			// credential to invalidate, which is the outcome this was after.
			continue
		}
		if err != nil {
			return invalidated, err
		}
		if !cleared {
			// Something rewrote the record between the list and the write.
			// Whatever it wrote is newer than what this call was holding, so
			// leaving it alone is the right outcome, not a failure.
			s.logger.Debugf("skipped legacy secret hash that changed underneath: account=%s tenant=%s", account.ID, tenantID)
			continue
		}

		invalidated++
		s.logger.Warnf("invalidated legacy secret hash: account=%s tenant=%s", account.ID, tenantID)
	}

	return invalidated, nil
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

// hashSecret derives the stored form of a client secret with
// PBKDF2-HMAC-SHA256; see secret_kdf.go for the construction and its
// parameters.
func hashSecret(secret string) (string, error) {
	return deriveSecretHash(secret)
}

// verifySecret reports whether secret matches the stored digest, in constant
// time.
//
// It is a variable so tests can substitute a counting verifier and prove that
// every authentication path performs exactly one comparison; production code
// must never reassign it.
var verifySecret = compareSecretHash

// productionTimingEqualizerDigest is a PBKDF2 digest, at pbkdf2Iterations,
// of a random value that was discarded when it was generated: no caller can
// ever present a secret that matches it. AuthenticateServiceAccount compares
// against it whenever the account has no usable digest, so every attempt pays
// the same derivation cost and the response time carries no signal about which
// client IDs exist.
//
// It is a constant rather than a lazily derived value so that no attempt ever
// pays double — deriving the equalizer and then comparing — which would make
// exactly one request distinguishable.
// TestTimingEqualizerDigest_MatchesCurrentParameters fails if pbkdf2Iterations
// moves and this is not regenerated.
const productionTimingEqualizerDigest = "$pbkdf2-sha256$i=600000$sSoZG5u3qQzR+p1jRLT51w$R3hFF/POmVN1Wy4U3sqkMhlR0Cxrxs8movzMOVl2pxc"

// timingEqualizerDigest is the equalizer this process compares against. It is
// productionTimingEqualizerDigest in every production binary; only pkg/iam's
// TestMain replaces it, with a digest at the lowered secretWorkFactor, so the
// equalizer keeps costing exactly what a real verification costs (#721).
var timingEqualizerDigest = productionTimingEqualizerDigest

// isLegacySecretHash reports whether stored is not a digest this package
// wrote. Accounts written before the KDF landed hold the hex-encoded client
// secret here, which is a recoverable credential rather than a hash. Such
// accounts fail closed until InvalidateLegacySecretHashes clears them and the
// operator rotates.
//
// The test is "does not parse", so it also catches a digest left behind by
// some other KDF: anything unrecognized fails closed rather than being fed to
// a comparison that cannot understand it.
func isLegacySecretHash(stored string) bool {
	_, err := parseSecretHash(stored)
	return err != nil
}

// =============================================================================
// In-Memory ServiceAccountStore
// =============================================================================

// inMemoryServiceAccountStore implements ServiceAccountStore using sync.Map.
//
// writes serializes the store's read-modify-write paths against each other.
// sync.Map makes individual operations safe but cannot make a comparison and a
// write one step, which is what ClearSecretHashIfUnchanged has to promise —
// and the promise is only worth anything if the writers it races against take
// the same lock, so PutServiceAccount takes it too. Reads stay lock-free.
type inMemoryServiceAccountStore struct {
	accounts sync.Map // map[accountID]*ServiceAccount
	writes   sync.Mutex
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
	s.writes.Lock()
	defer s.writes.Unlock()
	s.accounts.Store(account.ID, account)
	return nil
}

// ClearSecretHashIfUnchanged clears the account's SecretHash while it still
// holds expected, and reports whether it did.
//
// The stored record is replaced with a copy rather than mutated in place:
// callers hold pointers this store handed out, and editing the live struct
// would change what they see mid-read.
func (s *inMemoryServiceAccountStore) ClearSecretHashIfUnchanged(ctx context.Context, accountID, expected string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	s.writes.Lock()
	defer s.writes.Unlock()

	val, ok := s.accounts.Load(accountID)
	if !ok {
		return false, &ServiceAccountNotFoundError{AccountID: accountID}
	}
	account, ok := val.(*ServiceAccount)
	if !ok {
		return false, &ServiceAccountNotFoundError{AccountID: accountID}
	}

	if account.SecretHash != expected {
		return false, nil
	}

	cleared := *account
	cleared.SecretHash = ""
	s.accounts.Store(accountID, &cleared)
	return true, nil
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
	// Deletion is a writer, so it takes the same lock as the others. Left
	// outside it, a delete landing between ClearSecretHashIfUnchanged's read
	// and its write would be undone by the store that follows — the record
	// would come back after deletion had reported success.
	s.writes.Lock()
	defer s.writes.Unlock()
	_, loaded := s.accounts.LoadAndDelete(accountID)
	if !loaded {
		return &ServiceAccountNotFoundError{AccountID: accountID}
	}
	return nil
}

// ListServiceAccountsIncludingRevoked returns every service account for a
// tenant, revoked ones included.
func (s *inMemoryServiceAccountStore) ListServiceAccountsIncludingRevoked(ctx context.Context, tenantID string) ([]*ServiceAccount, error) {
	return s.listByTenant(ctx, tenantID, true)
}

// ListServiceAccounts returns the live service accounts for a tenant.
func (s *inMemoryServiceAccountStore) ListServiceAccounts(ctx context.Context, tenantID string) ([]*ServiceAccount, error) {
	return s.listByTenant(ctx, tenantID, false)
}

// listByTenant collects a tenant's accounts, optionally including revoked ones.
func (s *inMemoryServiceAccountStore) listByTenant(ctx context.Context, tenantID string, includeRevoked bool) ([]*ServiceAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []*ServiceAccount
	s.accounts.Range(func(key, value interface{}) bool {
		account, ok := value.(*ServiceAccount)
		if !ok {
			return true
		}
		if account.TenantID == tenantID && (includeRevoked || !account.Revoked) {
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
