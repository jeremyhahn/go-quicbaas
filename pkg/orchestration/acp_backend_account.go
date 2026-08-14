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
	"path/filepath"
	"sort"
)

// accountEnvID / accountEnvStrategy surface which pooled account (if any) a
// subprocess was spawned with, and which strategy chose it, in the
// subprocess's own environment. This makes the choice observable in the
// agent's own process listing / transcript-adjacent tooling without
// requiring every caller to thread Session.AccountID through to a persisted
// record — see Session.AccountID's doc comment for the complementary,
// structured path.
const (
	accountEnvID       = "QUICBAAS_ACCOUNT_ID"
	accountEnvStrategy = "QUICBAAS_ACCOUNT_STRATEGY"
)

// resolvedSpawnAccount is what resolveAccount folded into a spawn request.
//
// EnvKeys is the part that only matters once a session can ROTATE: replacing
// one account's credential with another's means retracting the first one's
// environment, and the only honest way to know which variables that is, is to
// have recorded them when they were projected. Merging the new account's
// variables over the old set without retracting would leave a dead — but still
// present — credential in the subprocess environment of the next turn.
type resolvedSpawnAccount struct {
	// AccountID is the account the resolver selected.
	AccountID string

	// Strategy names the policy that selected it.
	Strategy string

	// EnvKeys are the environment variables this resolution wrote into the
	// spawn request, sorted so the set is deterministic in logs and tests.
	EnvKeys []string

	// HomeDir is the materialized per-spawn credential directory, empty when
	// the account carries no file-based material.
	HomeDir string

	// cleanup releases HomeDir. Nil when there is nothing to release.
	cleanup func()
}

// resolveAccount resolves req's pooled provider account (if any) and folds
// the result into req in place:
//
//   - Env-based credential material is merged into req.Env, which every
//     dispatch path (spawnCLI, spawnJSONRPC, spawnOneShotCLI,
//     spawnContainerJSONRPC) already forwards to the subprocess/container
//     verbatim.
//   - File-based credential material is materialized into a fresh per-spawn
//     temporary directory (never the host's shared config); req.AccountHomeDir
//     is set to its root so spawnJSONRPC / spawnContainerJSONRPC can point
//     the subprocess's isolated HOME / container mount at it instead of a
//     bare isolation directory or the host's shared identity.
//
// It returns a nil *resolvedSpawnAccount — no resolver call, no error, nothing
// written to req — when the caller set neither AccountID nor AccountAuto, so
// every pre-existing spawn path is completely unaffected by this feature unless
// a caller explicitly opts in.
func (b *ACPBackend) resolveAccount(ctx context.Context, req *SpawnRequest) (*resolvedSpawnAccount, error) {
	if req.AccountID == "" && !req.AccountAuto {
		return nil, nil
	}
	if b.accountResolver == nil {
		return nil, &AccountResolverNotConfiguredError{Backend: b.Type()}
	}

	resolved, resolveErr := b.accountResolver.Resolve(ctx, AccountResolveRequest{
		TenantID:          TenantFromContext(ctx),
		ProjectID:         req.ProjectID,
		Provider:          b.accountProvider(),
		AccountID:         req.AccountID,
		Auto:              req.AccountAuto,
		StrategyName:      req.AccountStrategy,
		AllowedAccountIDs: req.AccountAllow,
		ExcludeAccountIDs: req.AccountExclude,
	})
	if resolveErr != nil {
		return nil, resolveErr
	}

	out := &resolvedSpawnAccount{
		AccountID: resolved.AccountID,
		Strategy:  resolved.Strategy,
	}

	if req.Env == nil {
		req.Env = make(map[string]string, len(resolved.Env)+2)
	}
	keys := make([]string, 0, len(resolved.Env)+2)
	for k, v := range resolved.Env {
		req.Env[k] = v
		keys = append(keys, k)
	}
	req.Env[accountEnvID] = resolved.AccountID
	req.Env[accountEnvStrategy] = resolved.Strategy
	keys = append(keys, accountEnvID, accountEnvStrategy)
	sort.Strings(keys)
	out.EnvKeys = keys

	if len(resolved.Files) > 0 {
		dir, cleanup, matErr := materializeAccountFiles(resolved.Files)
		if matErr != nil {
			return nil, matErr
		}
		req.AccountHomeDir = dir
		out.HomeDir = dir
		out.cleanup = cleanup
	}

	return out, nil
}

// accountProvider is the AccountProvider this backend's accounts belong to,
// derived from its command basename (see AccountProvider).
func (b *ACPBackend) accountProvider() AccountProvider {
	return AccountProvider(filepath.Base(b.config.Command))
}
