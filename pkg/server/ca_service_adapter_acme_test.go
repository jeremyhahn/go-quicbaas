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

package server

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestCAServiceAdapter_CertificateBySerial(t *testing.T) {
	t.Run("resolves matching serial as PEM", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.certStore = &fakeCertStore{certs: []*x509.Certificate{ca.caCert}}
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		serialHex := ca.caCert.SerialNumber.Text(16)
		got, err := adapter.CertificateBySerial(serialHex)
		require.NoError(t, err)

		block, _ := pem.Decode(got)
		require.NotNil(t, block)
		parsed, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		assert.Equal(t, ca.caCert.SerialNumber, parsed.SerialNumber)
	})

	t.Run("not found returns typed sentinel", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.certStore = &fakeCertStore{certs: []*x509.Certificate{ca.caCert}}
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		_, err = adapter.CertificateBySerial("deadbeef")
		assert.ErrorIs(t, err, service.ErrACMECertNotFound)
	})

	t.Run("invalid serial returns typed error", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.certStore = &fakeCertStore{}
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		_, err = adapter.CertificateBySerial("not-hex!!")
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidSerial)
	})

	t.Run("no cert store returns typed error", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.certStore = nil
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		_, err = adapter.CertificateBySerial("ff")
		assert.ErrorIs(t, err, ErrCAServiceAdapterCertStoreUnavailable)
	})

	t.Run("store error propagates", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.certStore = &fakeCertStore{listErr: errors.New("store read failed")}
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		_, err = adapter.CertificateBySerial("ff")
		require.Error(t, err)
		assert.Equal(t, "store read failed", err.Error())
	})
}

func TestCAServiceAdapter_RevokeBySerial(t *testing.T) {
	t.Run("revokes the serial on the CA", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		require.NoError(t, adapter.RevokeBySerial("ff", 0))
		assert.True(t, ca.revokedSet["ff"], "serial must be recorded as revoked")
	})

	t.Run("invalid serial returns typed error", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		err = adapter.RevokeBySerial("zzz", 0)
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidSerial)
	})

	t.Run("CA revoke error propagates", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		ca.revokeErr = errors.New("revoke failed")
		adapter, err := newCAServiceAdapter(ca)
		require.NoError(t, err)

		err = adapter.RevokeBySerial("ff", 0)
		require.Error(t, err)
		assert.Equal(t, "revoke failed", err.Error())
	})
}

func TestNormalizeSerial(t *testing.T) {
	t.Run("plain hex", func(t *testing.T) {
		n, ok := normalizeSerial("ff")
		require.True(t, ok)
		assert.Equal(t, int64(255), n.Int64())
	})
	t.Run("0x prefix and whitespace", func(t *testing.T) {
		n, ok := normalizeSerial("  0xFF ")
		require.True(t, ok)
		assert.Equal(t, int64(255), n.Int64())
	})
	t.Run("empty rejected", func(t *testing.T) {
		_, ok := normalizeSerial("")
		assert.False(t, ok)
	})
	t.Run("garbage rejected", func(t *testing.T) {
		_, ok := normalizeSerial("zz")
		assert.False(t, ok)
	})
}
