// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package xkms

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// A hosted rotation of a key in the platform OIDC signing backend is refused
// before anything reaches xkms, whatever the case of the backend segment;
// other backends and bare key names pass the check.
func TestCheckHostedRotation_RefusesOIDCSigningBackend(t *testing.T) {
	for _, kid := range []string{
		"oidc-replicated:::oidc-signing",
		"OIDC-Replicated:signing:ecdsa-p256:oidc-signing",
		"oidc-replicated:::tenant-key",
	} {
		err := CheckHostedRotation(kid)
		var refused *OIDCSigningKeyRotationRefusedError
		require.True(t, errors.As(err, &refused), kid)
		assert.Equal(t, kid, refused.KID)
		assert.Equal(t, dberrors.ErrForbidden, refused.ErrorCode())
		assert.True(t, strings.HasPrefix(err.Error(), "xkms: "), err.Error())
		assert.Equal(t, strings.ToLower(err.Error()), err.Error())
	}
	for _, kid := range []string{"software:::oidc-signing", "oidc-signing", "pkcs11:signing:rsa:k"} {
		assert.NoError(t, CheckHostedRotation(kid), kid)
	}
}

// RotateKey, the package's hosted rotation entry point, refuses the OIDC
// signing backend with the typed error.
func TestRotateKey_RefusesOIDCSigningBackend(t *testing.T) {
	_, err := RotateKey(context.Background(), OIDCSigningBackendName+":::oidc-signing")
	var refused *OIDCSigningKeyRotationRefusedError
	require.ErrorAs(t, err, &refused)
}
