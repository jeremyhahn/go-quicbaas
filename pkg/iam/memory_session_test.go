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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(id string, userID uint64, expires time.Time) *Session {
	return &Session{
		ID:           id,
		UserID:       userID,
		TenantID:     "t1",
		Created:      time.Now(),
		Expires:      expires,
		LastActivity: time.Now(),
	}
}

func TestMemorySessionStorage_CreateGet(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	sess := newTestSession("s1", 30001, time.Now().Add(time.Hour))
	require.NoError(t, store.CreateSession(ctx, sess))

	got, err := store.GetSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, uint64(30001), got.UserID)
}

func TestMemorySessionStorage_Create_Duplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	sess := newTestSession("s1", 30001, time.Now().Add(time.Hour))
	require.NoError(t, store.CreateSession(ctx, sess))
	if err := store.CreateSession(ctx, sess); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestMemorySessionStorage_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	_, err := store.GetSession(ctx, "missing")
	require.Error(t, err)
}

func TestMemorySessionStorage_Update(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	sess := newTestSession("s1", 30001, time.Now().Add(time.Hour))
	require.NoError(t, store.CreateSession(ctx, sess))

	sess.AuthMethod = "webauthn"
	require.NoError(t, store.UpdateSession(ctx, sess))

	got, err := store.GetSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "webauthn", got.AuthMethod)
}

func TestMemorySessionStorage_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	err := store.UpdateSession(ctx, newTestSession("missing", 30001, time.Now().Add(time.Hour)))
	require.Error(t, err)
}

func TestMemorySessionStorage_DeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	require.NoError(t, store.CreateSession(ctx, newTestSession("keep", 30001, time.Now().Add(time.Hour))))
	require.NoError(t, store.CreateSession(ctx, newTestSession("expired", 30001, time.Now().Add(-time.Hour))))

	count, err := store.DeleteExpiredSessions(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	_, err = store.GetSession(ctx, "expired")
	require.Error(t, err)
	_, err = store.GetSession(ctx, "keep")
	require.NoError(t, err)
}

func TestMemorySessionStorage_DeleteExpiredSessions_None(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStorage()

	require.NoError(t, store.CreateSession(ctx, newTestSession("keep", 30001, time.Now().Add(time.Hour))))

	count, err := store.DeleteExpiredSessions(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
