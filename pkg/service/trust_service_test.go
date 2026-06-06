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
	stderrors "errors"
	"testing"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTrustCA is a configurable TrustCA used to exercise the trust service
// without a live xkms CA.
type fakeTrustCA struct {
	rootPEMs  [][]byte
	rootErr   error
	importErr error
	imported  []*x509.Certificate
}

func (f *fakeTrustCA) CrossSignedRootPEMs() ([][]byte, error) {
	if f.rootErr != nil {
		return nil, f.rootErr
	}
	return f.rootPEMs, nil
}

func (f *fakeTrustCA) ImportIssuingCAs(cert *x509.Certificate) error {
	if f.importErr != nil {
		return f.importErr
	}
	f.imported = append(f.imported, cert)
	return nil
}

// selfSignedDER produces a minimal self-signed certificate's DER bytes for
// use as a chain entry in AutoImportAIA tests.
func selfSignedDER(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return der
}

func TestNewTrustService_NilCA(t *testing.T) {
	assert.Nil(t, NewTrustService(nil))
}

func TestTrustService_CrossSignedRootPool(t *testing.T) {
	ca := &fakeTrustCA{rootPEMs: [][]byte{[]byte("root-a"), []byte("root-b")}}
	svc := NewTrustService(ca)
	require.NotNil(t, svc)

	resp, err := svc.CrossSignedRootPool(context.Background(), &CrossSignedRootPoolRequest{})
	require.NoError(t, err)
	require.Len(t, resp.RootPEMs, 2)
}

func TestTrustService_CrossSignedRootPool_CAError(t *testing.T) {
	boom := stderrors.New("boom")
	svc := NewTrustService(&fakeTrustCA{rootErr: boom})

	_, err := svc.CrossSignedRootPool(context.Background(), &CrossSignedRootPoolRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestTrustService_CrossSignedRootPool_CanceledContext(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{rootPEMs: [][]byte{[]byte("root")}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.CrossSignedRootPool(ctx, &CrossSignedRootPoolRequest{})
	require.Error(t, err)
}

func TestTrustService_AutoImportAIA(t *testing.T) {
	ca := &fakeTrustCA{}
	svc := NewTrustService(ca)

	der := selfSignedDER(t)
	resp, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{
		ChainDER: [][]byte{der, der},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Imported)
	assert.Len(t, ca.imported, 2)
}

func TestTrustService_AutoImportAIA_NilRequest(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{})
	_, err := svc.AutoImportAIA(context.Background(), nil)
	require.ErrorIs(t, err, ErrTrustNilRequest)
}

func TestTrustService_AutoImportAIA_EmptyChain(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{})
	_, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{})
	require.ErrorIs(t, err, ErrTrustEmptyChain)
}

func TestTrustService_AutoImportAIA_EmptyEntry(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{})
	_, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{
		ChainDER: [][]byte{{}},
	})
	require.ErrorIs(t, err, ErrTrustNilCertDER)
}

func TestTrustService_AutoImportAIA_BadDER(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{})
	_, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{
		ChainDER: [][]byte{{0xDE, 0xAD, 0xBE, 0xEF}},
	})
	require.Error(t, err)
}

func TestTrustService_AutoImportAIA_ImportError(t *testing.T) {
	boom := stderrors.New("import boom")
	svc := NewTrustService(&fakeTrustCA{importErr: boom})
	_, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{
		ChainDER: [][]byte{selfSignedDER(t)},
	})
	require.ErrorIs(t, err, boom)
}
