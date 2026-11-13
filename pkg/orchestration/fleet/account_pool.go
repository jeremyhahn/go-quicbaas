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

package fleet

import (
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The daemon's pooled provider account resolver.
//
// This file is the whole of go-quicbaas#734's daemon half, and it is
// deliberately short: nothing about SELECTION is reimplemented here. The
// daemon composes the SAME orchestration.AccountResolver the control plane
// composes (pkg/server/lifecycle_orchestration.go) over three daemon-side
// pieces:
//
//	 orchestration.NewAccountResolver(
//	     apiAccountStore,                        <- metadata over the API
//	     AccountTokenManager(materializingSecrets),  <- credentials over the API
//	     AccountStrategyRegistry(AccountAPIClient),  <- usage over the API
//	     WithSpendAuthority(AccountAPIClient),       <- spend verdict over the API
//	 )
//
// so AccountID / AccountAuto / AccountStrategy / AccountAllow / AccountExclude
// mean exactly what they mean on the control plane, by construction rather than
// by two implementations happening to agree.
//
// The credential itself travels ONCE, over the daemon's own mTLS + bearer
// channel, in response to an explicit release. It is never in a Spawn RPC —
// which is the boundary pkg/orchestration/remote_acp_backend.go documents and
// this file finally makes true on both ends.
// =============================================================================

// AccountPoolConfig wires the daemon's account resolver to the control plane.
type AccountPoolConfig struct {
	// ServerAddr is the base URL of the control plane.
	ServerAddr string

	// TenantID is the tenant this daemon is enrolled in. Its pool is the only
	// pool this daemon can reach.
	TenantID string

	// HTTPClient is the daemon's existing control-plane client: it already
	// presents the daemon's certificate and trusts the control plane's CA.
	// Sharing it is the point — a second client would be a second place to
	// keep the daemon's credentials correct.
	HTTPClient *http.Client

	// Tokens is the daemon's existing token source, shared with the heartbeat
	// and the event forwarder so one /oauth2/token exchange serves them all.
	Tokens TokenSource

	// Logger is used for diagnostics. Required. It is never handed credential
	// material.
	Logger interfaces.Logger

	// ProjectID optionally scopes every release this daemon makes to one
	// project. Empty is the ordinary case; the project a spawn belongs to
	// still reaches the control plane through the resolver's own project
	// filter.
	ProjectID string
}

// AccountPool is the daemon's resolver plus the handles a caller needs to
// observe it. It is returned rather than a bare AccountResolver so a daemon can
// report cache behaviour without the resolver growing an accessor for it.
type AccountPool struct {
	// Resolver is what the ACP backends are given.
	Resolver orchestration.AccountResolver

	// cache is the released-credential cache, exposed only through Stats.
	cache *accountCredentialCache
}

// Stats reports how many credential reads were served from cache and how many
// required a release. It never reports credential content.
func (p *AccountPool) Stats() (hits, misses int64) {
	if p == nil || p.cache == nil {
		return 0, 0
	}
	return p.cache.Stats()
}

// NewAccountPool builds the daemon's pooled provider account resolver.
//
// It fails rather than degrading when its dependencies are incomplete: a
// resolver that silently resolved nothing would turn a wiring mistake into a
// per-spawn "no account available", which reads like an empty pool and sends an
// operator to the wrong host.
func NewAccountPool(cfg AccountPoolConfig) (*AccountPool, error) {
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: cfg.ServerAddr,
		TenantID:   cfg.TenantID,
		HTTPClient: cfg.HTTPClient,
		Tokens:     cfg.Tokens,
		Logger:     cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	cache := &accountCredentialCache{}
	secrets := newMaterializingSecrets(client, cache, cfg.Logger)
	secrets.projectID = cfg.ProjectID

	store := newAPIAccountStore(client, cache)

	// The token manager is composed exactly as the control plane composes it,
	// with ONE deliberate omission: no TokenRefresher is registered. Renewal
	// belongs to the side that holds the refresh token, and the control plane
	// deliberately never sends one to a daemon (see the materialize response
	// shape). The cache's freshness margin keeps the manager's renewal path out
	// of reach, so the omission is a statement of ownership rather than a hole:
	// a credential this daemon cannot renew is one it re-requests instead.
	tokens := orchestration.NewAccountTokenManager(
		orchestration.NewAccountSecretStore(secrets), store, 0)

	// Usage AND live sessions come from the control plane, so the daemon's
	// default strategy balances on the same numbers the control plane's does:
	// the ledger's recorded usage and the session records' live count. A
	// daemon balancing on its own sessions alone would see an empty pool on
	// every other host's accounts and pile onto them (go-quicbaas#1046).
	strategies, err := orchestration.NewBalancedAccountStrategyRegistry(client, client)
	if err != nil {
		return nil, err
	}

	return &AccountPool{
		Resolver: orchestration.NewAccountResolver(store, tokens, strategies,
			orchestration.WithStoreLogger(cfg.Logger),
			// THE GRANT THE CONTROL PLANE ISSUED IS REPORTED BACK, so the
			// session record the control plane writes names it and the
			// session's conclusion withdraws it. This daemon issues nothing —
			// see account_grant.go for why reporting is the only honest answer
			// on a host with no lease store.
			orchestration.WithAccountDeliveryGrantor(newDeliveryGrantReporter(cache)),
			// THE SESSION CAP IS RESERVED AGAINST THE CONTROL PLANE'S TABLE,
			// so spawns on every host contend for one set of slots.
			orchestration.WithAccountSessionReserver(client),
			// THE SPEND VERDICT IS THE CONTROL PLANE'S, read from the same
			// usage call: a candidate whose allowance is spent is filtered out
			// before the strategy runs, so this daemon's pool fails over too
			// (go-quicbaas#1058, ruling F6).
			orchestration.WithSpendAuthority(client)),
		cache: cache,
	}, nil
}
