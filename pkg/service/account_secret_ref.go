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

package service

import (
	"context"
	stderrors "errors"
	"math"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Pooled accounts whose credential is a tenant secret (credential_secret_ref).
//
// The account names a secret by its SHORT, tenant-relative name — the same
// names an SDLC binding's forge.token_secret_ref uses — and every resolution
// reads it through SecretsService in the ACCOUNT's tenant. See
// pkg/orchestration/account_secret_ref.go for the model.
// =============================================================================

// accountSecretResolver implements orchestration.AccountSecretResolver over
// SecretsService.
type accountSecretResolver struct {
	secrets SecretsService
}

// NewAccountSecretResolver returns the resolver pooled accounts read a
// credential_secret_ref through.
//
// The read is scoped by the tenant it is handed — the account's — and that
// scope OVERRIDES whatever tenant the incoming context carried. A system
// administrator provisioning tenant T's account therefore checks T's secret,
// and a spawn running under T resolves in T; no caller can steer the read into
// another keyspace by the context it arrives with.
func NewAccountSecretResolver(secrets SecretsService) orchestration.AccountSecretResolver {
	return &accountSecretResolver{secrets: secrets}
}

// ResolveAccountSecret implements orchestration.AccountSecretResolver.
//
// The value's generation is the secret's VERSION number, which every write
// advances, so a rewritten secret reads as a newer copy of the account's
// credential. It is deliberately not the response's CreatedAt: the secrets
// service reports the time of the READ there when the version carries no
// creation stamp, which would make every read look like a rotation.
func (r *accountSecretResolver) ResolveAccountSecret(ctx context.Context, tenantID, ref string) (*orchestration.AccountSecretValue, error) {
	if r == nil || r.secrets == nil {
		return nil, &AccountSecretsServiceNotConfiguredError{}
	}
	resp, err := r.secrets.GetSecret(iam.ContextWithTenant(ctx, tenantID), &GetSecretRequest{SecretID: ref})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return &orchestration.AccountSecretValue{Data: resp.Data, Generation: secretVersionGeneration(resp.Version)}, nil
}

// secretVersionGeneration converts a secret version number into a credential
// generation. A version too large for the generation's type cannot be ordered
// against the others and is reported as unknown (zero).
func secretVersionGeneration(version uint64) int64 {
	if version > math.MaxInt64 {
		return 0
	}
	return int64(version)
}

// AccountSecretsServiceNotConfiguredError reports an account secret resolver
// built with no SecretsService behind it.
type AccountSecretsServiceNotConfiguredError struct{}

// Error implements the error interface.
func (e *AccountSecretsServiceNotConfiguredError) Error() string {
	return "account: secrets service not configured"
}

// createWithSecretRef persists a new account whose credential is a tenant
// secret. The secret must resolve now: a reference that cannot be read is the
// operator's request to fix, not the first spawn's surprise. Nothing is written
// to the account's own envelope.
func (s *accountServiceImpl) createWithSecretRef(ctx context.Context, account *orchestration.Account) (*CreateAccountResponse, error) {
	if err := s.checkSecretRef(ctx, account); err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, account); err != nil {
		return nil, convertAccountStoreError(err)
	}
	return &CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
}

// applySecretRef points account at ref (and file), validates the result and
// checks the secret resolves.
//
// A successful check is also the recovery path: an account the spawn path put
// in AccountStatusError because its secret was missing returns to active here,
// since the operator has just shown the secret now resolves. Any other status
// (revoked, limited) is left alone — those are not the secret's to clear.
func (s *accountServiceImpl) applySecretRef(ctx context.Context, account *orchestration.Account, ref, file string) error {
	account.CredentialSecretRef = ref
	account.CredentialSecretFile = file
	if err := s.providers.ValidateAccountCredentialSource(account.Provider,
		effectiveRefreshPolicy(account.RefreshPolicy), false, ref, file); err != nil {
		return convertAccountStoreError(err)
	}
	if err := s.checkSecretRef(ctx, account); err != nil {
		return err
	}
	// The inline envelope's expiry described a credential this account no
	// longer reads; the secret declares none.
	account.CredentialExpiresAt = time.Time{}
	// Pointing the account at a secret changes its credential, and
	// last_refreshed_at is the signal a worker host's cache evicts on — so a
	// switch or re-point reaches a daemon on its next spawn rather than when
	// its cached copy ages out.
	account.LastRefreshedAt = time.Now().UTC()
	if account.Status == orchestration.AccountStatusError {
		account.Status = orchestration.AccountStatusActive
		account.LastError = ""
	}
	return nil
}

// checkSecretRef reads the secret account names and refuses the write when it
// does not yield a credential.
func (s *accountServiceImpl) checkSecretRef(ctx context.Context, account *orchestration.Account) error {
	_, err := orchestration.ReadAccountSecretRef(ctx, s.secretRefs, account)
	if err == nil {
		return nil
	}
	var unresolved *orchestration.AccountSecretRefUnresolvedError
	if stderrors.As(err, &unresolved) {
		return &AccountInvalidError{
			Field:   "credential_secret_ref",
			Message: "secret " + unresolved.Ref + " in tenant " + unresolved.TenantID + ": " + unresolved.Reason,
			Cause:   err,
		}
	}
	return err
}
