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

package agent

import (
	"context"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// apiAccountStore — orchestration.AccountStore backed by the control plane.
//
// The daemon runs the SAME orchestration.AccountResolver the control plane
// runs (selection strategies, allow/exclude filtering, limit-hold cooldown,
// project fallback). Only the two READS underneath it change: instead of a
// tenant-routed KV, they are two calls to /api/v1/agent-accounts. Everything
// else about how an account is chosen is therefore identical on both sides by
// construction rather than by two implementations agreeing.
//
// The four MUTATING methods are refused, not forwarded. Creating, updating,
// deleting or re-defaulting a pooled account are operator verbs performed on
// the control plane, where the pool and its audit trail live; a worker host
// must not be able to reshape the pool it draws from, and the daemon's grant
// (iam.AgentRuntimeGrant) does not carry the permissions that would let it.
// =============================================================================

// accountObserver is notified of every account record the store reads.
//
// It exists so the credential cache can learn that an ENVELOPE changed WITHOUT
// asking for the secret: the resolver always reads metadata (Get or
// ListByTenant) before it reads a credential, so an observation always
// precedes the read it invalidates. See accountCredentialCache.Observe.
type accountObserver interface {
	// Observe records when the account's envelope was last replaced, dropping
	// any cached credential released before that.
	Observe(accountID string, lastRefreshedAt time.Time)
}

// apiAccountStore reads a tenant's pooled accounts from the control plane.
type apiAccountStore struct {
	client   *AccountAPIClient
	observer accountObserver
}

// newAPIAccountStore returns a read-only AccountStore over client. A nil
// observer disables cache invalidation, which is correct only for a caller
// that keeps no cache.
func newAPIAccountStore(client *AccountAPIClient, observer accountObserver) *apiAccountStore {
	return &apiAccountStore{client: client, observer: observer}
}

// Get retrieves one account's metadata record.
func (s *apiAccountStore) Get(ctx context.Context, tenantID, accountID string) (*orchestration.Account, error) {
	if err := s.assertTenant(tenantID); err != nil {
		return nil, err
	}
	account, err := s.client.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	s.observe(account)
	return account, nil
}

// ListByTenant returns the tenant's accounts, optionally filtered to one
// provider and/or project.
func (s *apiAccountStore) ListByTenant(
	ctx context.Context,
	tenantID string,
	provider orchestration.AccountProvider,
	projectID string,
) ([]*orchestration.Account, error) {
	if err := s.assertTenant(tenantID); err != nil {
		return nil, err
	}
	accounts, err := s.client.ListAccounts(ctx, provider, projectID)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		s.observe(account)
	}
	return accounts, nil
}

// Touch is a LOCAL NO-OP that reports success.
//
// It is not a silent failure and not a refusal, and both alternatives would be
// wrong here. The bookkeeping this method exists to write — LastUsedAt, and the
// status normalization after a limit hold lapses — is performed on the CONTROL
// PLANE, inside the same materialize call this daemon makes to obtain the
// credential (pkg/service/account_materialize.go stamps MarkUsed +
// MarkValidated on every release). So the write did happen; it simply did not
// happen here, and there is nothing left for this store to do.
//
// Returning an error instead would produce a warning on every single spawn
// (accountResolver.logTouchFailure) for bookkeeping that is already correct,
// and would train an operator to ignore that warning.
//
// The one thing it costs: a spawn served entirely from the daemon's credential
// cache makes no materialize call, so it advances nothing. LastUsedAt on an
// account released to a remote daemon therefore means "last released to a
// worker host", not "last spawned against" — which is the only thing the
// control plane can truthfully observe about a host it does not run.
func (s *apiAccountStore) Touch(_ context.Context, tenantID, _ string, _ orchestration.TouchAccountOptions) error {
	return s.assertTenant(tenantID)
}

// Create refuses: pool membership is an operator verb on the control plane.
func (s *apiAccountStore) Create(_ context.Context, account *orchestration.Account) error {
	id := ""
	if account != nil {
		id = account.ID
	}
	return &AccountStoreReadOnlyError{Method: "Create", AccountID: id}
}

// Update refuses: account metadata is owned by the control plane.
func (s *apiAccountStore) Update(_ context.Context, account *orchestration.Account) error {
	id := ""
	if account != nil {
		id = account.ID
	}
	return &AccountStoreReadOnlyError{Method: "Update", AccountID: id}
}

// Delete refuses: removing a pooled account destroys its credential envelope,
// which lives on the control plane.
func (s *apiAccountStore) Delete(_ context.Context, _, accountID string) error {
	return &AccountStoreReadOnlyError{Method: "Delete", AccountID: accountID}
}

// SetDefault refuses: the scope default decides what EVERY host in the fleet
// gets for an unpinned spawn, so one host must not be able to move it.
func (s *apiAccountStore) SetDefault(_ context.Context, _, accountID string) error {
	return &AccountStoreReadOnlyError{Method: "SetDefault", AccountID: accountID}
}

// assertTenant refuses a request naming a tenant other than the daemon's own.
func (s *apiAccountStore) assertTenant(tenantID string) error {
	if tenantID == "" || tenantID == s.client.TenantID() {
		return nil
	}
	return &AccountAPITenantMismatchError{Requested: tenantID, Bound: s.client.TenantID()}
}

// observe forwards one freshly-read record to the cache, if any.
//
// The signal is LastRefreshedAt, not UpdatedAt, and the distinction is
// load-bearing. UpdatedAt moves on ANY write to the record — including the
// usage stamp the control plane makes as part of the release itself, which
// would make every release look like a rotation and defeat the cache entirely.
// LastRefreshedAt moves only when the ENVELOPE is replaced: an operator's
// rotate, or an oauth renewal on the control plane. That is exactly the event a
// cached credential must not survive.
func (s *apiAccountStore) observe(account *orchestration.Account) {
	if s.observer == nil || account == nil {
		return
	}
	s.observer.Observe(account.ID, account.LastRefreshedAt)
}

// AccountStoreReadOnlyError reports a pool mutation attempted on an agent
// daemon.
//
// The daemon reads its tenant's pool over the control plane's API and holds
// exactly one credential-plane permission (agent_accounts:materialize). Every
// verb that reshapes the pool belongs to an operator acting on the control
// plane, where the records and their audit trail live.
type AccountStoreReadOnlyError struct {
	// Method is the refused AccountStore method.
	Method string

	// AccountID is the account the call concerned, when it named one.
	AccountID string
}

// Error implements the error interface.
func (e *AccountStoreReadOnlyError) Error() string {
	msg := "agent account store: " + e.Method +
		" is not available on an agent daemon: the pooled account pool is owned by the control plane"
	if e.AccountID != "" {
		msg += " (account " + strconv.Quote(e.AccountID) + ")"
	}
	return msg
}

// Compile-time proof the API-backed store satisfies the full contract the
// resolver composes over.
var _ orchestration.AccountStore = (*apiAccountStore)(nil)
