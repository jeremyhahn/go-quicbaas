//go:build quicbaas_signal

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

package signal

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jeremyhahn/go-signal/pkg/keys"
)

// preKeyTakeGuard wraps a request's PreKeyStore and records any failure of
// a one-time pre-key take other than exhaustion.
//
// go-signal's KeysManager.GetPreKeys discards every TakeEC and TakeKEM error:
// a failed atomic delete (a backend or transaction error, or ErrNoTransactions)
// would otherwise yield a successful bundle without the one-time EC key and
// with the last-resort KEM key, silently weakening session establishment.
// GetPreKeys checks Err after the call and refuses the bundle instead.
type preKeyTakeGuard struct {
	keys.PreKeyStore

	mu  sync.Mutex
	err error
}

func newPreKeyTakeGuard(inner keys.PreKeyStore) *preKeyTakeGuard {
	return &preKeyTakeGuard{PreKeyStore: inner}
}

// TakeEC records a non-exhaustion failure and returns the result unchanged.
func (g *preKeyTakeGuard) TakeEC(ctx context.Context, uuid string, deviceID uint64) (*keys.PreKey, error) {
	pk, err := g.PreKeyStore.TakeEC(ctx, uuid, deviceID)
	g.record("EC", err)
	return pk, err
}

// TakeKEM records a non-exhaustion failure and returns the result unchanged.
func (g *preKeyTakeGuard) TakeKEM(ctx context.Context, uuid string, deviceID uint64) (*keys.KEMSignedPreKey, error) {
	pk, err := g.PreKeyStore.TakeKEM(ctx, uuid, deviceID)
	g.record("KEM", err)
	return pk, err
}

func (g *preKeyTakeGuard) record(keyType string, err error) {
	if err == nil {
		return
	}
	var notFound *keys.PreKeyNotFoundError
	if errors.As(err, &notFound) {
		return // genuine exhaustion: the bundle legitimately omits the key
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err == nil {
		g.err = fmt.Errorf("signal: consume one-time %s pre-key: %w", keyType, err)
	}
}

// Err returns the first non-exhaustion take failure, or nil.
func (g *preKeyTakeGuard) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}
