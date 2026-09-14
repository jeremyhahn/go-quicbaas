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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Dynamic secrets — the wiring that was missing.
//
// pkg/service declared dynamicSecretsService, DynamicSecrets() returned it, and
// NOTHING EVER ASSIGNED IT: Security().DynamicSecrets() was nil in every
// deployment, so all eight verbs reported the capability as unsupported on all
// five transports. NewDynamicSecretsHandler and lease.NewManager had no
// non-test callers at all.
//
// The blocker was storage. The old comment in lifecycle.go said BaaS mode "has
// no local pebble storage" to build a lease manager from, and that was true of
// pkg/storage.Storage — a LOCAL engine interface this server never instantiates.
// It was never true of the store the server does have: the embedded qrdb node,
// reached through interfaces.KVStore, which is what every other durable
// quicbaas component already persists through (orchestration's stores, IAM's
// backends, the scheduler). Lease and role state belongs there and nowhere else:
//
//   - it is DURABLE, so a lease survives the restart that kills the process
//     holding the credential it governs — which is exactly the case where a
//     lease has to outlive the thing it leases;
//   - it is REPLICATED, so a revocation is a fleet-wide fact rather than one
//     node's opinion;
//   - it is ENCRYPTED AT REST by the storage barrier, so lease metadata gets
//     the same protection as every other record the platform keeps;
//   - it is TENANT-ROUTED, so one tenant's leases are not in another's
//     keyspace.
//
// Building a local store instead would have been a parallel implementation of
// something qrdb already owns, which CLAUDE.md's Layer Ownership forbids, and
// it would have made revocation node-local — a security control that does not
// hold across a restart is not one.
// =============================================================================

// startDynamicSecrets composes the dynamic secrets capability and attaches it
// to the service facade.
//
// It runs after startService (the facade must exist to attach to) and before
// startOrchestration (the account credential leaser is built over the lease
// manager this creates).
//
// It is NOT gated on a config flag. Dynamic secrets is part of the locked
// Security() surface rather than an optional subsystem, every transport already
// routes its eight verbs, and the leases that govern delivery of the agent
// pool's provider credentials live here — a deployment that could switch this
// off would be one where a session's credential lease could not be revoked. The
// only background cost is one reaper tick.
func (s *Server) startDynamicSecrets() error {
	s.logger.Info("Initializing dynamic secrets subsystem...")

	store := s.durableKVStore()
	if store == nil {
		// No qrdb client means no durable store, which happens only in the
		// sealed window and in tests that stand up a bare Server. Leaving the
		// accessor unconfigured is the honest outcome: a lease manager over a
		// store that is not there would accept grants it could not revoke.
		s.logger.Warn("dynamic secrets: no durable store available; capability left unconfigured")
		return nil
	}

	handler, leaseManager, generators, err := composeDynamicSecrets(
		context.Background(), store, logger.NewAdapter(s.logger))
	if err != nil {
		return err
	}

	// The reaper runs on the SERVER's lifetime, not a request's: it converts
	// lapsed leases into tombstones and purges tombstones past retention.
	// Enforcement does not depend on it — Validate refuses an expired lease
	// from its own ExpiresAt — so a failed reaper degrades bookkeeping rather
	// than authorization.
	if err := leaseManager.StartExpirationLoop(context.Background()); err != nil {
		return err
	}

	s.mu.Lock()
	s.leaseManager = leaseManager
	s.generatorRegistry = generators
	s.mu.Unlock()

	if s.quicBaaSService != nil {
		s.quicBaaSService.SetDynamicSecretsService(handler)
		s.logger.Info("Dynamic secrets service attached to QuicBaaS facade")
	}

	s.logger.Info("Dynamic secrets subsystem started")
	return nil
}

// composeDynamicSecrets builds the capability over store.
//
// Split out from startDynamicSecrets so the COMPOSITION is testable without a
// running node: everything above it is server plumbing (which store, which
// logger, where to attach), and everything in it is the decision about what
// the capability is made of.
func composeDynamicSecrets(
	ctx context.Context,
	store interfaces.KVStore,
	log interfaces.Logger,
) (service.DynamicSecretsService, lease.LeaseManager, *credentials.GeneratorRegistry, error) {
	leaseManager, err := lease.NewManager(&lease.ManagerConfig{Store: store, Logger: log})
	if err != nil {
		return nil, nil, nil, err
	}

	// THE REGISTRY IS REAL AND STARTS EMPTY. pkg/credentials/database ships
	// PostgreSQL and MySQL generators, but both take a DBConnector and this
	// repository has no production implementation of one — registering them
	// against a nil connector would give an operator a role that fails at
	// generation time rather than at creation time. An unregistered type is
	// refused by CreateRole with "generator not found", which is the correct
	// answer, and adding a connector is a self-contained later change.
	//
	// Nothing else about the capability depends on a generator: leases, their
	// renewal, their revocation and their listing are what the agent credential
	// lifecycle is built on, and they work with an empty registry.
	generators := credentials.NewGeneratorRegistry()

	handler, err := service.NewDynamicSecretsHandler(ctx, &service.DynamicSecretsConfig{
		Store:             store,
		LeaseManager:      leaseManager,
		GeneratorRegistry: generators,
		Logger:            log,
	})
	if err != nil {
		_ = leaseManager.Close()
		return nil, nil, nil, err
	}

	return handler, leaseManager, generators, nil
}

// stopDynamicSecrets stops the lease reaper. The leases themselves are durable
// and deliberately survive: a lease that vanished on shutdown would let a
// credential delivered before the restart look ungoverned after it.
func (s *Server) stopDynamicSecrets() error {
	s.mu.Lock()
	leaseManager := s.leaseManager
	s.leaseManager = nil
	s.mu.Unlock()

	if leaseManager == nil {
		return nil
	}
	return leaseManager.Close()
}

// LeaseManager returns the lease manager dynamic secrets and the agent account
// credential lifecycle share, or nil before startDynamicSecrets has run.
//
// It is exported for the orchestration wiring in lifecycle_orchestration.go:
// the account credential leaser composes THIS manager rather than making one of
// its own, so a lease issued for a session's provider credential is the same
// kind of object — in the same keyspace, visible to the same ListLeases, and
// revocable through the same SDK verb — as every other lease in the platform.
func (s *Server) LeaseManager() lease.LeaseManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.leaseManager == nil {
		return nil
	}
	return s.leaseManager
}
