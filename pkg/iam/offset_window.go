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

import "slices"

// OffsetWindow returns the items at positions [offset, offset+limit) of a
// listing that its backend serves only in 1-based pages of a fixed size, so a
// storage honours the ListOptions offset exactly whether or not it falls on a
// page boundary. fetchPage returns page number page of pages size items long.
//
// It reads the page holding offset and, when the window runs past the end of
// that page, the page after; a short page is the end of the listing and stops
// the read. A limit of zero or less asks for page 1 at the backend's default
// size, ignoring offset, as the qrdb adapters always have.
func OffsetWindow[T any](offset, limit int, fetchPage func(page, size int) ([]T, error)) ([]T, error) {
	if limit <= 0 {
		return fetchPage(1, 0)
	}
	offset = max(offset, 0)
	page, skip := offset/limit+1, offset%limit

	first, err := fetchPage(page, limit)
	if err != nil {
		return nil, err
	}
	if skip == 0 {
		return first, nil
	}
	if skip >= len(first) {
		return first[:0], nil
	}
	if len(first) < limit {
		return first[skip:], nil
	}

	next, err := fetchPage(page+1, limit)
	if err != nil {
		return nil, err
	}
	// Concat copies, so the second page is never written into the first's
	// backing array.
	return slices.Concat(first[skip:], next[:min(skip, len(next))]), nil
}
