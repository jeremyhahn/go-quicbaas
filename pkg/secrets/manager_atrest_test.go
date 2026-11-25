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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func newAtRestManager(t *testing.T, barrier func() bool) (*Manager, *MemoryBackend) {
	t.Helper()
	backend := NewMemoryBackend()
	m, err := NewManager(&ManagerOptions{Backend: backend, AtRestBarrier: barrier})
	require.NoError(t, err)
	return m, backend
}

func assertAtRestRefusal(t *testing.T, name string, err error) {
	t.Helper()
	require.Error(t, err, name)
	assert.True(t, stderrors.Is(err, ErrAtRestEncryptionRequired), "%s: errors.Is the sentinel: %v", name, err)
	var qe *qberrors.QBError
	require.True(t, stderrors.As(err, &qe), "%s: a QBError", name)
	assert.Equal(t, qberrors.ErrPreconditionFailed, qe.Code, name)
}

func barrierOff() bool { return false }
func barrierOn() bool  { return true }

func TestManager_RefusesWritesWithoutAtRestBarrier(t *testing.T) {
	ctx := context.Background()
	m, backend := newAtRestManager(t, barrierOff)
	data := map[string][]byte{"v": []byte("x")}

	assertAtRestRefusal(t, "Store", m.Store(ctx, "a/store", data, nil))
	assertAtRestRefusal(t, "StoreWithOptions", m.StoreWithOptions(ctx, "a/opts", data, nil, &interfaces.StoreOptions{}))
	_, err := m.StoreVersion(ctx, "a/ver", data, nil)
	assertAtRestRefusal(t, "StoreVersion", err)
	_, err = m.StoreVersionWithOptions(ctx, "a/veropts", data, nil, nil)
	assertAtRestRefusal(t, "StoreVersionWithOptions", err)
	_, _, err = m.EnsureSecret(ctx, "a/ensure", &SecretSpec{Length: 8, Encoding: EncodingNumeric})
	assertAtRestRefusal(t, "EnsureSecret", err)

	paths, err := backend.List(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, paths, "nothing reaches the backend")

	// Rollback over a secret seeded straight into the backend adds no version.
	_, err = backend.StoreVersion(ctx, "a/seeded", data, nil)
	require.NoError(t, err)
	assertAtRestRefusal(t, "Rollback", m.Rollback(ctx, "a/seeded", 1))
	versions, err := backend.ListVersions(ctx, "a/seeded")
	require.NoError(t, err)
	assert.Len(t, versions, 1, "Rollback wrote no version")
}

func TestManager_RefusesValueReadsWithoutAtRestBarrier(t *testing.T) {
	ctx := context.Background()
	m, backend := newAtRestManager(t, barrierOff)
	require.NoError(t, backend.Store(ctx, "a/b", map[string][]byte{"v": []byte("x")}, nil))

	_, _, err := m.Get(ctx, "a/b")
	assertAtRestRefusal(t, "Get", err)
	_, _, err = m.GetVersion(ctx, "a/b", 1)
	assertAtRestRefusal(t, "GetVersion", err)
	called := false
	err = m.UseSecret(ctx, "a/b", "v", func([]byte) error { called = true; return nil })
	assertAtRestRefusal(t, "UseSecret", err)
	assert.False(t, called, "UseSecret never calls fn")

	paths, err := m.List(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"a/b"}, paths)
	_, err = m.ListVersions(ctx, "a/b")
	require.NoError(t, err)
	require.NoError(t, m.Delete(ctx, "a/b"))
	require.NoError(t, m.Destroy(ctx, "a/b"))
}

func exerciseManager(t *testing.T, m *Manager) {
	t.Helper()
	ctx := context.Background()
	data := map[string][]byte{"v": []byte("x")}

	require.NoError(t, m.Store(ctx, "a/b", data, nil))
	require.NoError(t, m.StoreWithOptions(ctx, "a/b", data, nil, &interfaces.StoreOptions{}))
	_, err := m.StoreVersion(ctx, "a/b", data, nil)
	require.NoError(t, err)
	_, err = m.StoreVersionWithOptions(ctx, "a/b", data, nil, nil)
	require.NoError(t, err)
	require.NoError(t, m.Rollback(ctx, "a/b", 1))
	_, _, err = m.Get(ctx, "a/b")
	require.NoError(t, err)
	_, _, err = m.GetVersion(ctx, "a/b", 1)
	require.NoError(t, err)
	require.NoError(t, m.UseSecret(ctx, "a/b", "v", func([]byte) error { return nil }))
	_, _, err = m.EnsureSecret(ctx, "a/ensure", &SecretSpec{Length: 8, Encoding: EncodingNumeric})
	require.NoError(t, err)
	_, err = m.List(ctx, "")
	require.NoError(t, err)
	_, err = m.ListVersions(ctx, "a/b")
	require.NoError(t, err)
	require.NoError(t, m.Delete(ctx, "a/b"))
	require.NoError(t, m.Destroy(ctx, "a/b"))
}

func TestManager_AtRestBarrierOnAllowsEverything(t *testing.T) {
	m, _ := newAtRestManager(t, barrierOn)
	exerciseManager(t, m)
}

func TestManager_NilAtRestBarrierIsUnchecked(t *testing.T) {
	m, _ := newAtRestManager(t, nil)
	exerciseManager(t, m)
}

func TestErrAtRestEncryptionRequired_NamesTheRemedy(t *testing.T) {
	m, _ := newAtRestManager(t, barrierOff)
	err := m.Store(context.Background(), "a/b", map[string][]byte{"v": []byte("x")}, nil)
	require.Error(t, err)
	msg := ErrAtRestEncryptionRequired.Error()
	assert.Contains(t, msg, "seal.enabled")
	assert.Contains(t, msg, "quicbaasd init")
	assert.True(t, qberrors.IsCallerSafe(err), "the refusal is caller-safe through its QBError")
	assert.True(t, qberrors.IsCallerSafe(ErrAtRestEncryptionRequired))
}
