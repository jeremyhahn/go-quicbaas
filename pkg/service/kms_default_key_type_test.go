package service

import (
	"crypto/x509"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
)

func TestDefaultKeyType_SymmetricKeyIsSecret(t *testing.T) {
	attrs := &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305}

	assert.Equal(t, xkmssdk.KeyTypeSecret, DefaultKeyType(attrs))
}

func TestDefaultKeyType_AsymmetricOrUnsetKeySigns(t *testing.T) {
	assert.Equal(t, xkmssdk.KeyTypeSigning, DefaultKeyType(&xkmssdk.KeyAttributes{KeyAlgorithm: x509.Ed25519}))
	assert.Equal(t, xkmssdk.KeyTypeSigning, DefaultKeyType(&xkmssdk.KeyAttributes{}),
		"attributes naming no symmetric algorithm are not treated as symmetric")
}
