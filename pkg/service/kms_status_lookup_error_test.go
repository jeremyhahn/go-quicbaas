//go:build !integration

package service

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// readErrorKVStore fails every read and counts the writes made through it.
type readErrorKVStore struct {
	*mockKVStoreForKMS
	puts atomic.Int64
}

func (s *readErrorKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, stderrors.New("raft: no leader")
}

func (s *readErrorKVStore) Put(ctx context.Context, key string, value []byte) error {
	s.puts.Add(1)
	return s.mockKVStoreForKMS.Put(ctx, key, value)
}

// A version status that cannot be read fails the status change before any
// write: it is not treated as "no status set".
func TestKMSUpdateKeyStatus_StatusReadErrorFailsClosed(t *testing.T) {
	for _, status := range []KeyStatus{KeyStatusEnabled, KeyStatusDisabled} {
		t.Run(string(status), func(t *testing.T) {
			f := newVersionedCryptoFixture(t)
			store := &readErrorKVStore{mockKVStoreForKMS: &mockKVStoreForKMS{data: map[string][]byte{}}}
			f.kmsSvc.SetKVStore(store)

			req := &UpdateKeyStatusRequest{KeyID: versionStatusKeyID, Version: 1, Status: status}
			_, err := f.kmsSvc.UpdateKeyStatus(systemTenantCtx(), req)
			require.Error(t, err, "a status read failure must fail the status change")
			require.Zero(t, store.puts.Load(), "no status may be written after a failed read")
		})
	}

	t.Run("grace period disable", func(t *testing.T) {
		f := newVersionedCryptoFixture(t)
		store := &readErrorKVStore{mockKVStoreForKMS: &mockKVStoreForKMS{data: map[string][]byte{}}}
		f.kmsSvc.SetKVStore(store)

		_, err := f.kmsSvc.disableKeyVersion(systemTenantCtx(), f.scoped, 1)
		require.Error(t, err)
		require.Zero(t, store.puts.Load())
	})
}
