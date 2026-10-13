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

package iam

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedSource serves a sequence of ints in 1-based fixed-size pages, the way
// the qrdb user and role services do, and records every page it was asked for.
type pagedSource struct {
	items       []int
	defaultSize int
	failPage    int
	err         error
	asked       []int
}

func newPagedSource(n int) *pagedSource {
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}
	return &pagedSource{items: items, defaultSize: 100}
}

func (p *pagedSource) fetch(page, size int) ([]int, error) {
	p.asked = append(p.asked, page)
	if page == p.failPage {
		return nil, p.err
	}
	if size <= 0 {
		size = p.defaultSize
	}
	start := min((page-1)*size, len(p.items))
	end := min(start+size, len(p.items))
	return p.items[start:end], nil
}

func intRange(from, to int) []int {
	out := make([]int, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, i)
	}
	return out
}

func TestOffsetWindow_ReturnsExactWindow(t *testing.T) {
	for _, tc := range []struct {
		name          string
		total         int
		offset, limit int
		want          []int
		wantPages     []int
	}{
		{name: "first page", total: 600, offset: 0, limit: 256, want: intRange(0, 256), wantPages: []int{1}},
		{name: "aligned offset", total: 600, offset: 256, limit: 256, want: intRange(256, 512), wantPages: []int{2}},
		{name: "unaligned offset spans two pages", total: 600, offset: 255, limit: 256, want: intRange(255, 511), wantPages: []int{1, 2}},
		{name: "unaligned window ends in a short page", total: 300, offset: 255, limit: 256, want: intRange(255, 300), wantPages: []int{1, 2}},
		{name: "second page shorter than the overlap", total: 12, offset: 5, limit: 10, want: intRange(5, 12), wantPages: []int{1, 2}},
		{name: "short page stops early", total: 7, offset: 3, limit: 10, want: intRange(3, 7), wantPages: []int{1}},
		{name: "offset at the end", total: 512, offset: 512, limit: 256, want: []int{}, wantPages: []int{3}},
		{name: "unaligned offset past the end", total: 10, offset: 15, limit: 4, want: []int{}, wantPages: []int{4}},
		{name: "negative offset reads from the start", total: 10, offset: -3, limit: 4, want: intRange(0, 4), wantPages: []int{1}},
		{name: "limit zero keeps the backend's first page", total: 150, offset: 40, limit: 0, want: intRange(0, 100), wantPages: []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newPagedSource(tc.total)
			got, err := OffsetWindow(tc.offset, tc.limit, src.fetch)
			require.NoError(t, err)
			assert.Equal(t, tc.want, append([]int{}, got...))
			assert.Equal(t, tc.wantPages, src.asked)
		})
	}
}

func TestOffsetWindow_DoesNotWriteIntoThePage(t *testing.T) {
	src := newPagedSource(30)
	got, err := OffsetWindow(5, 10, src.fetch)
	require.NoError(t, err)
	require.Equal(t, intRange(5, 15), got)
	assert.Equal(t, intRange(0, 30), src.items, "the backend's page must not be overwritten by the second page")
}

func TestOffsetWindow_ReturnsFetchError(t *testing.T) {
	errList := errors.New("list failed")
	for _, tc := range []struct {
		name     string
		failPage int
	}{
		{name: "first page", failPage: 1},
		{name: "second page", failPage: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newPagedSource(40)
			src.failPage, src.err = tc.failPage, errList
			got, err := OffsetWindow(5, 10, src.fetch)
			require.ErrorIs(t, err, errList)
			assert.Nil(t, got)
		})
	}
}
