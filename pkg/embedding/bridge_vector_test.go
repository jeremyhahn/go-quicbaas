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

// TestProviderBridgeIn_Embed covers both embedding methods of the inbound
// bridge: the inner vectors come through, an inner error is returned as is,
// and an empty inner result is rejected by Embed.
func TestProviderBridgeIn_Embed(t *testing.T) {
	innerErr := errors.New("inner failure")
	tests := []struct {
		name    string
		batch   bool
		inner   *nativeQRDBProvider
		texts   []string
		wantLen int
		wantErr error
	}{
		{name: "Embed success", inner: &nativeQRDBProvider{dims: 3}, texts: []string{"hi"}, wantLen: 3},
		{name: "Embed inner error", inner: &nativeQRDBProvider{embedErr: innerErr}, texts: []string{"hi"}, wantErr: innerErr},
		{name: "Embed empty result", inner: &nativeQRDBProvider{empty: true}, texts: []string{"hi"}, wantErr: ErrInvalidInput},
		{name: "EmbedBatch success", batch: true, inner: &nativeQRDBProvider{dims: 2}, texts: []string{"a", "b"}, wantLen: 2},
		{name: "EmbedBatch inner error", batch: true, inner: &nativeQRDBProvider{embedErr: innerErr}, texts: []string{"a"}, wantErr: innerErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &providerBridgeIn{inner: tt.inner, model: "m"}

			var gotLen int
			var err error
			if tt.batch {
				var vecs [][]float32
				vecs, err = b.EmbedBatch(context.Background(), tt.texts)
				gotLen = len(vecs)
			} else {
				var vec []float32
				vec, err = b.Embed(context.Background(), tt.texts[0])
				gotLen = len(vec)
			}

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLen, gotLen)
		})
	}
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

// TestProviderBridgeOut_Embed covers the routing of the outbound bridge: one
// text goes to the inner Embed, several texts go to the inner EmbedBatch, and
// an error from either is returned unwrapped.
func TestProviderBridgeOut_Embed(t *testing.T) {
	tests := []struct {
		name           string
		inner          *fakeProvider
		texts          []string
		wantLen        int
		wantEmbedCalls int
		wantBatchCalls int
		wantErr        bool
	}{
		{name: "single text", inner: &fakeProvider{name: "fake"}, texts: []string{"only"}, wantLen: 1, wantEmbedCalls: 1},
		{name: "single text error", inner: &fakeProvider{embedErr: &fakeProviderError{msg: "single fail"}}, texts: []string{"only"}, wantErr: true},
		{name: "multiple texts", inner: &fakeProvider{name: "fake"}, texts: []string{"a", "b"}, wantLen: 2, wantBatchCalls: 1},
		{name: "multiple texts error", inner: &fakeProvider{batchErr: &fakeProviderError{msg: "batch fail"}}, texts: []string{"a", "b"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &providerBridgeOut{inner: tt.inner}

			res, err := b.Embed(context.Background(), tt.texts, "ignored")
			if tt.wantErr {
				var fe *fakeProviderError
				assert.True(t, errors.As(err, &fe), "inner error must be returned unwrapped, got %v", err)
				return
			}
			require.NoError(t, err)
			require.Len(t, res.Embeddings, tt.wantLen)
			assert.Equal(t, "fake", res.Model)
			assert.Equal(t, tt.wantEmbedCalls, tt.inner.embedCalls, "Embed hook calls")
			assert.Equal(t, tt.wantBatchCalls, tt.inner.batchCalls, "EmbedBatch hook calls")
		})
	}
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
