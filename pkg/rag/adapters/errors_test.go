//go:build rag

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

package adapters

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	t.Run("ranker errors are distinct", func(t *testing.T) {
		assert.NotEqual(t, ErrNilHybridRanker, ErrEmptyQuery)
		assert.NotEqual(t, ErrEmptyQuery, ErrEmptyResults)
		assert.NotEqual(t, ErrEmptyResults, ErrRankingFailed)
	})

	t.Run("context builder errors are distinct", func(t *testing.T) {
		assert.NotEqual(t, ErrNilContextBuilder, ErrContextBuildFailed)
		assert.NotEqual(t, ErrContextBuildFailed, ErrInvalidMaxTokens)
		assert.NotEqual(t, ErrInvalidMaxTokens, ErrNoResultsProvided)
	})

	t.Run("errors can be wrapped and unwrapped", func(t *testing.T) {
		wrapped := errors.Join(ErrNilHybridRanker, errors.New("additional context"))
		assert.True(t, errors.Is(wrapped, ErrNilHybridRanker))
	})

	t.Run("error messages are meaningful", func(t *testing.T) {
		assert.Contains(t, ErrNilHybridRanker.Error(), "nil")
		assert.Contains(t, ErrEmptyQuery.Error(), "empty")
		assert.Contains(t, ErrEmptyResults.Error(), "results")
		assert.Contains(t, ErrRankingFailed.Error(), "ranking")
		assert.Contains(t, ErrNilContextBuilder.Error(), "nil")
		assert.Contains(t, ErrContextBuildFailed.Error(), "context")
		assert.Contains(t, ErrInvalidMaxTokens.Error(), "tokens")
		assert.Contains(t, ErrNoResultsProvided.Error(), "results")
		assert.Contains(t, ErrInvalidConfig.Error(), "config")
	})
}
