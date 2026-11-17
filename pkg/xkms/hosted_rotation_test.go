// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package xkms

import (
	"context"
	"crypto"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var oidcSigningKIDs = []string{
	"oidc-replicated:::oidc-signing",
	"OIDC-Replicated:signing:ecdsa-p256:oidc-signing",
	"oidc-replicated:::tenant-key",
}

// A kid in the OIDC signing backend with no delegate registered is
// unavailable; it never falls back to a direct xkms rotation.
func TestHostedRotationDelegate_UnavailableWithoutDelegate(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	for _, kid := range oidcSigningKIDs {
		d, err := HostedRotationDelegate(kid)
		require.Nil(t, d, kid)
		var unavailable *RotationDelegateUnavailableError
		require.True(t, errors.As(err, &unavailable), kid)
		assert.Equal(t, kid, unavailable.KID)
		assert.Equal(t, OIDCSigningBackendName, unavailable.Backend)
		assert.Equal(t, dberrors.ErrUnavailable, unavailable.ErrorCode())
		assert.True(t, strings.HasPrefix(err.Error(), "xkms: "), err.Error())
		assert.Equal(t, strings.ToLower(err.Error()), err.Error())
	}
	for _, kid := range []string{"software:::oidc-signing", "oidc-signing", "pkcs11:signing:rsa:k"} {
		d, err := HostedRotationDelegate(kid)
		assert.NoError(t, err, kid)
		assert.Nil(t, d, kid)
	}
}

// RotateKey sends an OIDC signing backend kid to its registered delegate, and
// with none registered returns the typed error.
func TestRotateKey_OIDCSigningBackendGoesToDelegate(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	kid := OIDCSigningBackendName + ":::oidc-signing"
	_, err := RotateKey(context.Background(), kid)
	require.ErrorAs(t, err, new(*RotationDelegateUnavailableError))

	type key struct{ crypto.PrivateKey }
	want := &key{}
	var got []string
	RegisterRotationDelegate(OIDCSigningBackendName, func(_ context.Context, k string) (crypto.PrivateKey, error) {
		got = append(got, k)
		return want, nil
	})
	for _, kid := range oidcSigningKIDs {
		priv, err := RotateKey(context.Background(), kid)
		require.NoError(t, err, kid)
		require.Same(t, want, priv)
	}
	require.Equal(t, oidcSigningKIDs, got)

	d, err := HostedRotationDelegate("software:::oidc-signing")
	require.NoError(t, err)
	require.Nil(t, d)

	RegisterRotationDelegate(OIDCSigningBackendName, nil)
	_, err = RotateKey(context.Background(), kid)
	require.ErrorAs(t, err, new(*RotationDelegateUnavailableError))
}
