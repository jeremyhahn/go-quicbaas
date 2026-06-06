//go:build vector && qrdb_vector

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

package embedding

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nativeQRDBProvider implements qrdbsdk.EmbeddingProvider (batch-first) so the
// providerBridgeIn adapter can be exercised directly and through the Manager.
type nativeQRDBProvider struct {
	dims     int
	embedErr error
	empty    bool
	closeErr error
}

func (n *nativeQRDBProvider) Embed(_ context.Context, texts []string, _ string) (*qrdbsdk.EmbeddingResult, error) {
	if n.embedErr != nil {
		return nil, n.embedErr
	}
	if n.empty {
		return &qrdbsdk.EmbeddingResult{Embeddings: nil}, nil
	}
	emb := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, n.dims)
		for j := range vec {
			vec[j] = float32(i + j)
		}
		emb[i] = vec
	}
	return &qrdbsdk.EmbeddingResult{Embeddings: emb, Model: "native"}, nil
}

func (n *nativeQRDBProvider) Dimensions() int { return n.dims }
func (n *nativeQRDBProvider) Name() string    { return "native" }
func (n *nativeQRDBProvider) Models(_ context.Context) ([]qrdbsdk.EmbeddingModelInfo, error) {
	return nil, nil
}
func (n *nativeQRDBProvider) Close() error { return n.closeErr }

// =============================================================================
// providerBridgeIn — qrdb provider adapted to go-quicbaas Provider
// =============================================================================

func TestProviderBridgeIn_Embed_Success(t *testing.T) {
	b := &providerBridgeIn{inner: &nativeQRDBProvider{dims: 3}, model: "m"}
	vec, err := b.Embed(context.Background(), "hi")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestProviderBridgeIn_Embed_Error(t *testing.T) {
	wantErr := errors.New("inner failure")
	b := &providerBridgeIn{inner: &nativeQRDBProvider{embedErr: wantErr}}
	_, err := b.Embed(context.Background(), "hi")
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderBridgeIn_Embed_EmptyResult(t *testing.T) {
	b := &providerBridgeIn{inner: &nativeQRDBProvider{empty: true}}
	_, err := b.Embed(context.Background(), "hi")
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestProviderBridgeIn_EmbedBatch_Success(t *testing.T) {
	b := &providerBridgeIn{inner: &nativeQRDBProvider{dims: 2}}
	out, err := b.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, out, 2)
}

func TestProviderBridgeIn_EmbedBatch_Error(t *testing.T) {
	wantErr := errors.New("batch failure")
	b := &providerBridgeIn{inner: &nativeQRDBProvider{embedErr: wantErr}}
	_, err := b.EmbedBatch(context.Background(), []string{"a"})
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderBridgeIn_Accessors(t *testing.T) {
	closeErr := errors.New("close fail")
	b := &providerBridgeIn{inner: &nativeQRDBProvider{dims: 9, closeErr: closeErr}}
	assert.Equal(t, 9, b.Dimensions())
	assert.Equal(t, 0, b.MaxTokens())
	assert.Equal(t, "native", b.Name())
	assert.ErrorIs(t, b.Close(), closeErr)
}

// =============================================================================
// providerBridgeOut — go-quicbaas Provider adapted to qrdb provider
// =============================================================================

func TestProviderBridgeOut_Embed_SingleText(t *testing.T) {
	fp := &fakeProvider{name: "fake"}
	b := &providerBridgeOut{inner: fp}

	res, err := b.Embed(context.Background(), []string{"only"}, "ignored")
	require.NoError(t, err)
	require.Len(t, res.Embeddings, 1)
	assert.Equal(t, "fake", res.Model)
	assert.Equal(t, 1, fp.embedCalls, "single text must route to Embed hook")
	assert.Equal(t, 0, fp.batchCalls)
}

func TestProviderBridgeOut_Embed_SingleText_Error(t *testing.T) {
	fp := &fakeProvider{embedErr: &fakeProviderError{msg: "single fail"}}
	b := &providerBridgeOut{inner: fp}

	_, err := b.Embed(context.Background(), []string{"only"}, "")
	require.Error(t, err)
	var fe *fakeProviderError
	assert.True(t, errors.As(err, &fe))
}

func TestProviderBridgeOut_Embed_MultiText(t *testing.T) {
	fp := &fakeProvider{name: "fake"}
	b := &providerBridgeOut{inner: fp}

	res, err := b.Embed(context.Background(), []string{"a", "b"}, "")
	require.NoError(t, err)
	require.Len(t, res.Embeddings, 2)
	assert.Equal(t, 1, fp.batchCalls, "multiple texts must route to EmbedBatch hook")
	assert.Equal(t, 0, fp.embedCalls)
}

func TestProviderBridgeOut_Embed_MultiText_Error(t *testing.T) {
	fp := &fakeProvider{batchErr: &fakeProviderError{msg: "batch fail"}}
	b := &providerBridgeOut{inner: fp}

	_, err := b.Embed(context.Background(), []string{"a", "b"}, "")
	require.Error(t, err)
	var fe *fakeProviderError
	assert.True(t, errors.As(err, &fe))
}

func TestProviderBridgeOut_Accessors(t *testing.T) {
	closeErr := errors.New("close fail")
	fp := &fakeProvider{name: "fake", dims: 7, closeErr: closeErr}
	b := &providerBridgeOut{inner: fp}

	assert.Equal(t, 7, b.Dimensions())
	assert.Equal(t, "fake", b.Name())
	models, err := b.Models(context.Background())
	require.NoError(t, err)
	assert.Nil(t, models)
	assert.ErrorIs(t, b.Close(), closeErr)
}
