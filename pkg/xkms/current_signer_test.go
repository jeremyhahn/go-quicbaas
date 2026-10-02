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

package xkms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCurrentSigner_FollowsRotation proves CurrentSigner answers with the
// key's current version: version 1 before a rotation, the rotated version
// after it, while Signer keeps addressing version 1.
func TestCurrentSigner_FollowsRotation(t *testing.T) {
	setupFullTestXKMS(t)
	ctx := context.Background()
	attrs, fullKeyID := createTestECDSAKey(t, "current-signer-test")

	before, err := CurrentSigner(ctx, attrs)
	require.NoError(t, err)
	v1, err := Signer(ctx, attrs)
	require.NoError(t, err)
	require.True(t, before.Public().(*ecdsa.PublicKey).Equal(v1.Public()),
		"an unrotated key is its own current version")

	rotated, err := RotateKey(ctx, fullKeyID)
	require.NoError(t, err)
	rotatedPub := rotated.(crypto.Signer).Public()

	after, err := CurrentSigner(ctx, attrs)
	require.NoError(t, err)
	require.True(t, after.Public().(*ecdsa.PublicKey).Equal(rotatedPub),
		"CurrentSigner must answer with the rotated version")

	still, err := Signer(ctx, attrs)
	require.NoError(t, err)
	require.True(t, still.Public().(*ecdsa.PublicKey).Equal(v1.Public()),
		"Signer keeps addressing version 1")
}

// TestCurrentSigner_Errors covers the refusals: an uninitialized service and
// nil attributes.
func TestCurrentSigner_Errors(t *testing.T) {
	Reset()
	_, err := CurrentSigner(context.Background(), nil)
	require.ErrorIs(t, err, ErrNotInitialized)

	setupFullTestXKMS(t)
	_, err = CurrentSigner(context.Background(), nil)
	require.Error(t, err)
}
