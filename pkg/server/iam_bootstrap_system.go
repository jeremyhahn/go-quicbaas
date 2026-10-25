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
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// runBootstrapSystem executes qrdb's canonical daemon-init bootstrap. When
// seal is enabled and a barrier registry is reachable it routes through
// qrdbsdk.BootstrapSystem — the SDK's declared bootstrap entry point — which
// initializes the system barrier per the resolved strategy (idempotent when
// the barrier is already initialized/unsealed, deferred to the ceremony for a
// still-sealed shamir barrier) and then runs first-boot. With seal disabled
// it keeps the direct first-boot path, exactly mirroring qrdbd's runFirstBoot
// (go-qrdb/cmd/qrdbd/start.go).
//
// The operator's seal passphrase copy (bootstrapSealPassphrase) is consumed
// here and zeroized before returning, success or failure.
func (s *Server) runBootstrapSystem(ctx context.Context, deps *qrdbsdk.FirstBootDependencies) (*qrdbsdk.FirstBootResult, error) {
	defer s.zeroBootstrapSealPassphrase()

	registry := s.bootstrapBarrierRegistry()
	if !s.config.Seal.Enabled || registry == nil {
		result, err := qrdbsdk.FirstBootInitialize(ctx, *deps)
		if err != nil {
			return nil, &FirstBootBootstrapError{Op: "firstboot.Initialize", Cause: err}
		}
		return result, nil
	}

	strategy, _, _, _, err := resolveBarrierStrategy(s.config, s.config.Seal.StrategyExplicit)
	if err != nil {
		return nil, &FirstBootBootstrapError{Op: "resolve barrier strategy", Cause: err}
	}

	// BootstrapSystemConfig.Passphrase is a *secure.Buffer as of go-qrdb
	// v0.19. An unusable buffer must be an ERROR here, never a silent
	// downgrade: BootstrapSystem would otherwise bring up a StrategyNone
	// barrier that reports itself unsealed, leaving an operator who asked for
	// a passphrase barrier with plaintext at rest and nothing said about it.
	// An absent passphrase stays legitimate — a hardware or KMS strategy
	// supplies no operator secret — so only non-empty material is wrapped.
	// NewBufferFromBytes WIPES its source, which moves the plaintext out of
	// s.bootstrapSealPassphrase here rather than at the deferred zeroing; that
	// defer stays because it is the only thing covering the paths above, where
	// no buffer is built at all.
	var passphrase *secure.Buffer
	if len(s.bootstrapSealPassphrase) > 0 {
		passphrase, err = newProtectedPassphrase(s.bootstrapSealPassphrase)
		if err != nil {
			return nil, &FirstBootBootstrapError{Op: "protect seal passphrase", Cause: err}
		}
		defer passphrase.Destroy()
	}

	result, err := qrdbsdk.BootstrapSystem(ctx, qrdbsdk.BootstrapSystemConfig{
		Strategy:           strategy,
		Passphrase:         passphrase,
		SeparationOfDuties: deps.SeparationOfDuties,
		AdminCSRPEM:        deps.AdminCSRPEM,
	}, qrdbsdk.BootstrapDependencies{
		Tenants:     deps.Tenants,
		Users:       deps.Users,
		Roles:       deps.Roles,
		CA:          deps.CA,
		Signer:      deps.Signer,
		Binder:      deps.Binder,
		Barriers:    registry,
		Logger:      deps.Logger,
		TrustDomain: deps.TrustDomain,
	})
	if err != nil {
		return nil, &FirstBootBootstrapError{Op: "bootstrap.BootstrapSystem", Cause: err}
	}
	return result.FirstBoot, nil
}

// bootstrapBarrierRegistry resolves the barrier registry BootstrapSystem
// operates on. The embedded qrdb node's own registry wins (the node owns the
// system barrier in node mode); otherwise the quicbaas-owned data-plane
// StorageBarrier is wrapped in a fresh registry. Nil when neither exists —
// the caller then keeps the direct first-boot path.
func (s *Server) bootstrapBarrierRegistry() *qrdbsdk.BarrierRegistry {
	if node := s.getQRDBNode(); node != nil {
		if registry := node.BarrierRegistry(); registry != nil {
			return registry
		}
	}
	if s.dataPlaneBarrier == nil {
		return nil
	}
	registry, err := qrdbsdk.NewBarrierRegistry(slog.Default(), s.dataPlaneBarrier)
	if err != nil {
		s.logger.WithError(err).Warn("First-boot: data-plane barrier registry construction failed; using direct first-boot path")
		return nil
	}
	return registry
}

// newProtectedPassphrase moves the operator's seal passphrase into protected
// memory. It is a package variable rather than a direct call so the failure
// path can be exercised: the allocation needs locked pages the kernel may
// refuse, and on a GOEXPERIMENT=boringcrypto build it always fails. That path
// must return an error rather than bootstrap a passwordless barrier, and a
// branch with no test is a branch that has never run.
var newProtectedPassphrase = secure.NewBufferFromBytes

// zeroBootstrapSealPassphrase wipes and releases the retained seal
// passphrase copy. Idempotent.
func (s *Server) zeroBootstrapSealPassphrase() {
	for i := range s.bootstrapSealPassphrase {
		s.bootstrapSealPassphrase[i] = 0
	}
	s.bootstrapSealPassphrase = nil
}
