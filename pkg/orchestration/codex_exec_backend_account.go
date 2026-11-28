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

package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"sort"
)

// codex finds auth.json through CODEX_HOME, else $HOME/.codex (the same rule
// codexConfiguredModel follows). A pooled account's file credential is
// materialized at <per-session home>/.codex and named through CODEX_HOME;
// HOME itself is left alone.
const (
	codexAccountHomeRel = ".codex"
	codexAccountHomeEnv = "CODEX_HOME"
)

// SetAccountResolver wires the pooled provider account resolver consulted at
// Spawn when a SpawnRequest sets AccountID or AccountAuto. Leaving it unset
// preserves the pre-account-pool behavior: a spawn that names no account
// never consults it, and one that does fails closed with
// AccountResolverNotConfiguredError rather than falling back to the host's
// shared identity.
func (b *CodexExecBackend) SetAccountResolver(r AccountResolver) {
	b.accountResolver = r
}

// SetAccountCredentialLeaser wires the authority that withdraws a credential
// delivery grant. The backend needs it only for a spawn whose account
// resolution succeeded and which then failed: no Session is returned, so
// nothing downstream learns the lease ID (see ACPBackend's leaser).
func (b *CodexExecBackend) SetAccountCredentialLeaser(l AccountCredentialLeaser) {
	b.accountLeases = l
}

// resolveAccount resolves req's pooled codex account and folds it into req:
// its environment credential into req.Env and, for a file credential, the
// materialized .codex directory into CODEX_HOME. It returns nil, nil when req
// names no account.
func (b *CodexExecBackend) resolveAccount(ctx context.Context, req *SpawnRequest) (*resolvedSpawnAccount, error) {
	statedHome, homeStated := req.Env[codexAccountHomeEnv]

	// The provider is fixed: the command is often a path, never a basename
	// that names the provider.
	account, err := resolveSpawnAccount(ctx, b.accountResolver, b.Type(), AccountProviderCodex, req,
		func(a *resolvedSpawnAccount) { b.releaseAccountReservation(ctx, a) })
	if err != nil || account == nil {
		return account, err
	}
	if req.AccountHomeDir == "" {
		return account, nil
	}

	codexHome := filepath.Join(req.AccountHomeDir, codexAccountHomeRel)
	if homeStated && statedHome != codexHome {
		b.releaseResolvedAccount(ctx, account)
		return nil, &AccountEnvConflictError{AccountID: account.AccountID, Key: codexAccountHomeEnv}
	}
	if info, statErr := os.Stat(codexHome); statErr != nil || !info.IsDir() {
		b.releaseResolvedAccount(ctx, account)
		return nil, &CredentialDirMissingError{
			Backend: b.command,
			Source:  codexHome,
			Target:  codexHome,
			Rel:     codexAccountHomeRel,
		}
	}
	req.Env[codexAccountHomeEnv] = codexHome
	account.EnvKeys = append(account.EnvKeys, codexAccountHomeEnv)
	sort.Strings(account.EnvKeys)
	return account, nil
}

// releaseResolvedAccount gives back everything a resolution took for a spawn
// that did not happen: the materialized directory, the delivery grant and the
// session reservation.
func (b *CodexExecBackend) releaseResolvedAccount(ctx context.Context, account *resolvedSpawnAccount) {
	if account == nil {
		return
	}
	if account.cleanup != nil {
		account.cleanup()
	}
	b.releaseAccountLease(ctx, account.LeaseID)
	b.releaseAccountReservation(ctx, account)
}

// releaseAccountLease withdraws a delivery grant this backend took out and
// could not hand on. Best effort: the spawn has already failed.
func (b *CodexExecBackend) releaseAccountLease(ctx context.Context, leaseID string) {
	if b.accountLeases == nil || leaseID == "" {
		return
	}
	// Nothing to report to; the grant's TTL and the reaper bound a missed revoke.
	_ = b.accountLeases.Revoke(ctx, leaseID)
}

// releaseAccountReservation gives back the session slot a resolution reserved
// for a spawn that did not happen. Best effort, as releaseAccountLease.
func (b *CodexExecBackend) releaseAccountReservation(ctx context.Context, account *resolvedSpawnAccount) {
	if account == nil || account.ReservationID == "" {
		return
	}
	releaser, ok := b.accountResolver.(AccountReservationReleaser)
	if !ok {
		return
	}
	// Nothing to report to; an unreleased reservation expires on its TTL.
	_ = releaser.ReleaseReservation(ctx, account.TenantID, account.AccountID, account.ReservationID)
}
