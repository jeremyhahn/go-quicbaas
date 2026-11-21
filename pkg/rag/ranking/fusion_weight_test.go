//go:build rag

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

package ranking

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// weightLists returns a vector list holding only "both", and a keyword
// list led by "kw0" with "both" at rank 10. Unweighted, "both" leads: it
// scores in both lists. Weighted 0.1 vector / 0.9 keyword, "kw0" leads.
func weightLists() (vector, keyword []Result) {
	vector = []Result{{ID: "both", Content: "pool", VectorScore: 0.9}}
	for i := 0; i < 10; i++ {
		keyword = append(keyword, Result{ID: fmt.Sprintf("kw%d", i), Content: "pool", KeywordScore: 1})
	}
	keyword = append(keyword, Result{ID: "both", Content: "pool", KeywordScore: 1})
	return vector, keyword
}

func TestWeightedRRF_WeightsDecideTheLead(t *testing.T) {
	vector, keyword := weightLists()

	equal := NewRRF(60).Fuse(vector, keyword)
	require.NotEmpty(t, equal)
	assert.Equal(t, "both", equal[0].ID)

	unset := NewWeightedRRF(60, 0, 0).Fuse(vector, keyword)
	assert.Equal(t, "both", unset[0].ID, "two zero weights weigh both lists equally")

	keywordHeavy := NewWeightedRRF(60, 0.1, 0.9).Fuse(vector, keyword)
	assert.Equal(t, "kw0", keywordHeavy[0].ID)
	assert.InDelta(t, 0.9/60, keywordHeavy[0].FinalScore, 1e-6)
}

// TestHybridRanker_ReadsVectorAndKeywordWeight proves ranking.Config's
// VectorWeight and KeywordWeight change the fused order: they used to be
// validated and documented but never read.
func TestHybridRanker_ReadsVectorAndKeywordWeight(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableRerank = false

	cfg.VectorWeight, cfg.KeywordWeight = 0.5, 0.5
	vector, keyword := weightLists()
	got, err := NewHybridRanker(cfg, nil).RankHybrid(vector, keyword, "pool")
	require.NoError(t, err)
	assert.Equal(t, "both", got[0].ID)

	cfg.VectorWeight, cfg.KeywordWeight = 0.1, 0.9
	vector, keyword = weightLists()
	got, err = NewHybridRanker(cfg, nil).RankHybrid(vector, keyword, "pool")
	require.NoError(t, err)
	assert.Equal(t, "kw0", got[0].ID)
}

// TestRRF_EqualScoresHaveAStableOrder proves tied results come back in ID
// order on every call, not in map order.
func TestRRF_EqualScoresHaveAStableOrder(t *testing.T) {
	vector := []Result{{ID: "a"}}
	keyword := []Result{{ID: "b"}}
	for i := 0; i < 50; i++ {
		got := NewRRF(60).Fuse(vector, keyword)
		require.Len(t, got, 2)
		assert.Equal(t, []string{"a", "b"}, []string{got[0].ID, got[1].ID})
	}
}
