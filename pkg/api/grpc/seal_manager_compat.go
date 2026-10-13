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

package grpc

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
)

// This file provides compatibility shims for SealManager construction
// referenced by pkg/server/lifecycle.go. The intended replacement is
// pkg/seal.NewBarrierSealManager (the SDK-adapter implementation), but
// the lifecycle.go seam variables target three legacy entry points
// here: InitializeDefaultSealManager, NewDefaultSealManager,
// NewPersistentDefaultSealManager.
//
// The shims below preserve the legacy signatures and route to a no-op
// implementation. Production callers always set the seal manager
// explicitly via Server.SetSealManager (lifecycle.go:1546 short-
// circuits when s.sealManager != nil) so the no-op path is the
// expected behaviour. Tests that need a functioning manager already
// inject one via the newSealManagerFn seam.
//
// When the platform finishes the seal-manager refactor, these shims
// should be deleted along with the seam variables in lifecycle.go.

// noopSealManager satisfies interfaces.BarrierManager
// with the minimum surface needed to make Server.SetSealManager-aware
// callers compile and short-circuit at runtime. Production callers
// always replace this via SetSealManager before serving any RPC, so the
// no-op behaviour is the expected reset point.
type noopSealManager struct{}

func (n *noopSealManager) Status(_ context.Context) (interface{}, error) {
	return map[string]any{"strategy": "noop", "initialized": false, "sealed": true}, nil
}
func (n *noopSealManager) IsSealed() bool      { return true }
func (n *noopSealManager) IsInitialized() bool { return false }
func (n *noopSealManager) Initialize(_ context.Context, _ int, _ int) (interface{}, error) {
	return nil, nil
}
func (n *noopSealManager) Unseal(_ context.Context, _ []byte) (interface{}, error) {
	return map[string]any{"sealed": true, "progress": 0, "threshold": 0}, nil
}
func (n *noopSealManager) Seal(_ context.Context) error { return nil }
func (n *noopSealManager) Rekey(_ context.Context, _ int, _ int) (interface{}, error) {
	return nil, nil
}
func (n *noopSealManager) GenerateRecoveryKeys(_ context.Context, _, _ int) ([][]byte, error) {
	return nil, nil
}
func (n *noopSealManager) DeleteRecoveryKeys(_ context.Context) error { return nil }
func (n *noopSealManager) GenerateRootToken(_ context.Context, _ string, _ [][]byte) (string, error) {
	return "", nil
}
func (n *noopSealManager) RotateRootKey(_ context.Context) error { return nil }
func (n *noopSealManager) Close() error                          { return nil }

var _ interfaces.BarrierManager = (*noopSealManager)(nil)

// InitializeDefaultSealManager is a no-op compatibility shim. Returns
// nil so the lifecycle seam's failure path stays quiet for callers
// that have already injected a real manager via SetSealManager.
func InitializeDefaultSealManager(_ *logger.Logger) error {
	return nil
}

// NewDefaultSealManager returns a no-op SealManager that reports the
// barrier as sealed and rejects all unseal/seal calls with success
// (no-op). Production callers replace the value via
// Server.SetSealManager before any RPC is served.
func NewDefaultSealManager(_ *quicbaasseal.Config) interfaces.BarrierManager {
	return &noopSealManager{}
}

// NewPersistentDefaultSealManager has the same semantics as
// NewDefaultSealManager — the persistent variant existed in an earlier
// API design that has since been folded into pkg/seal.BarrierSealManager.
// Returns the no-op manager and a nil error so the lifecycle.go seam's
// fallback branch never trips on a missing function.
func NewPersistentDefaultSealManager(_ *quicbaasseal.Config, _ string) (interfaces.BarrierManager, error) {
	return &noopSealManager{}, nil
}
