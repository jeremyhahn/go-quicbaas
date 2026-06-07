//go:build !integration

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

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
)

// fakeACMECA is a configurable ACMECA used to exercise the ACME service Issue
// verb without standing up a full certificate authority. It satisfies the
// narrow service.ACMECA capability only.
type fakeACMECA struct {
	certPEM  []byte
	chainPEM []byte
	err      error
	gotCSR   []byte
}

func (f *fakeACMECA) SignCSRPEM(_ context.Context, csrPEM []byte) ([]byte, []byte, error) {
	f.gotCSR = csrPEM
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.certPEM, f.chainPEM, nil
}

// newTestACMEService builds an acmeService directly (same-package access) with
// the supplied issuance CA. It bypasses NewACMEService — which requires a full
// TCGCA + storage factory to construct the xkms RestService — so the
// service-layer control-verb logic can be unit-tested in isolation. The
// RFC 8555 round-trip against the real composed server is covered by the
// integration suite.
func newTestACMEService(issueCA ACMECA) *acmeService {
	return &acmeService{
		directoryURL: "https://acme.test.example/acme/directory",
		issueCA:      issueCA,
		challenge:    map[string]xkmsacme.ACMEChallengeVerifierFunc{},
	}
}

// selfSignedLeafPEM generates a throwaway self-signed certificate PEM whose
// serial the Issue verb echoes back. Used to assert serial extraction.
func selfSignedLeafPEM(t *testing.T, serial *big.Int) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "acme-test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestNewACMEService_NotConfigured(t *testing.T) {
	t.Run("nil params returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(nil)
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})

	t.Run("missing CA returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(&ACMEServiceParams{
			BaseRESTURI:    "https://x",
			StorageFactory: nil,
		})
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})

	t.Run("missing base URI returns nil servicer", func(t *testing.T) {
		svc, err := NewACMEService(&ACMEServiceParams{})
		assert.Nil(t, svc)
		assert.NoError(t, err)
	})
}

func TestACMEService_RegisterChallenge(t *testing.T) {
	svc := newTestACMEService(nil)
	ctx := context.Background()

	t.Run("http-01 registers", func(t *testing.T) {
		resp, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: xkmsacme.ACMEChallengeTypeHTTP01})
		require.NoError(t, err)
		assert.True(t, resp.Registered)
		svc.mu.Lock()
		_, ok := svc.challenge[xkmsacme.ACMEChallengeTypeHTTP01]
		svc.mu.Unlock()
		assert.True(t, ok, "verifier installed into shared challenge map")
	})

	t.Run("dns-01 registers", func(t *testing.T) {
		resp, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: xkmsacme.ACMEChallengeTypeDNS01})
		require.NoError(t, err)
		assert.True(t, resp.Registered)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty name rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{})
		assert.ErrorIs(t, err, ErrACMEEmptyChallengeName)
	})

	t.Run("unknown challenge rejected", func(t *testing.T) {
		_, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: "tls-alpn-01"})
		assert.ErrorIs(t, err, ErrACMEUnknownChallenge)
	})
}

func TestACMEService_Issue(t *testing.T) {
	ctx := context.Background()
	serial := big.NewInt(0x4d2)
	leaf := selfSignedLeafPEM(t, serial)
	chain := []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----")

	t.Run("issues and echoes serial", func(t *testing.T) {
		ca := &fakeACMECA{certPEM: leaf, chainPEM: chain}
		svc := newTestACMEService(ca)
		resp, err := svc.Issue(ctx, &ACMEIssueRequest{
			CSRPEM:         []byte("CSR"),
			ChallengeType:  xkmsacme.ACMEChallengeTypeHTTP01,
			ChallengeValue: "example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, leaf, resp.CertificatePEM)
		assert.Equal(t, chain, resp.ChainPEM)
		assert.Equal(t, serial.Text(16), resp.SerialNumber)
		assert.Equal(t, []byte("CSR"), ca.gotCSR)
	})

	t.Run("nil request rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, nil)
		assert.ErrorIs(t, err, ErrACMENilRequest)
	})

	t.Run("empty csr rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{ChallengeType: "http-01"})
		assert.ErrorIs(t, err, ErrACMEEmptyCSR)
	})

	t.Run("empty challenge type rejected", func(t *testing.T) {
		svc := newTestACMEService(&fakeACMECA{})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR")})
		assert.ErrorIs(t, err, ErrACMEEmptyChallengeType)
	})

	t.Run("missing CA returns typed error", func(t *testing.T) {
		svc := newTestACMEService(nil)
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
		assert.ErrorIs(t, err, ErrACMECAMissing)
	})

	t.Run("CA error propagates", func(t *testing.T) {
		boom := stderrors.New("sign failed")
		svc := newTestACMEService(&fakeACMECA{err: boom})
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
		require.Error(t, err)
		assert.ErrorIs(t, err, boom)
	})
}

func TestACMEService_DirectoryURL(t *testing.T) {
	svc := newTestACMEService(nil)
	assert.Equal(t, "https://acme.test.example/acme/directory", svc.DirectoryURL())
}

func TestSerialFromCertPEM(t *testing.T) {
	t.Run("parses serial", func(t *testing.T) {
		leaf := selfSignedLeafPEM(t, big.NewInt(255))
		assert.Equal(t, "ff", serialFromCertPEM(leaf))
	})
	t.Run("empty PEM yields empty serial", func(t *testing.T) {
		assert.Equal(t, "", serialFromCertPEM(nil))
	})
	t.Run("garbage PEM yields empty serial", func(t *testing.T) {
		assert.Equal(t, "", serialFromCertPEM([]byte("not a pem")))
	})
}
