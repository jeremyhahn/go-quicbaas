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

package secrets

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// casOnlyKV is a pkg/kvstore.Store: it has CompareAndSwap (promoted) and no
// PutIfAbsent. onAbsent is a hook that runs once, in the window after a
// writer has observed the record at key as absent and before it publishes.
type casOnlyKV struct {
	*kvstore.Store
	key      string
	onAbsent func()
}

func (k *casOnlyKV) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := k.Store.Get(ctx, key)
	if k.onAbsent != nil && key == k.key && qberrors.IsNotFound(err) {
		hook := k.onAbsent
		k.onAbsent = nil
		hook()
	}
	return value, err
}

// TestEncryptedBackend_CASOnlyStoreRefusesPruneAndConditionalWrite runs the
// interleaving a CAS-only store cannot arbitrate: instance A observes the
// path as absent; instance B then creates versions 1-3 and tries to prune
// and to write conditionally; A then publishes its first version.
// pkg/kvstore.Store has CompareAndSwap but no PutIfAbsent, so A's first write
// is a plain Put that can overwrite B's history. The backend therefore claims
// no arbitration on such a store: B's prune and conditional write are refused.
func TestEncryptedBackend_CASOnlyStoreRefusesPruneAndConditionalWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := &casOnlyKV{Store: kvstore.NewMemory(), key: secretKey("app/s")}
	enc := newTestMockSecretsEncryptor()
	a, b := newGuardedEncryptedBackend(t, store, enc), newGuardedEncryptedBackend(t, store, enc)

	store.onAbsent = func() {
		for i := 1; i <= 3; i++ {
			_, err := b.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("b%d", i))}, nil)
			require.NoError(t, err)
		}
		removed, err := b.PruneVersions(ctx, "app/s", 1)
		require.ErrorIs(t, err, ErrPruneUnsupported)
		assert.Zero(t, removed)

		read := readCurrent(t, b, "app/s", 3)
		_, err = b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
		require.ErrorIs(t, err, ErrConditionalWriteUnsupported)
	}

	_, err := a.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte("a1")}, nil)
	require.NoError(t, err)
	require.Nil(t, store.onAbsent, "the interleaving ran")
}
