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

package sdlc

import (
	"net/http"
	"testing"
)

// TestNextPageNumber_ReadsTheDialectsHeader proves a forge naming its next
// page in a header is followed by that number.
func TestNextPageNumber_ReadsTheDialectsHeader(t *testing.T) {
	h := &forgeHTTP{dialect: forgeDialect{pageHeader: "X-Next-Page"}}
	header := http.Header{}
	header.Set("X-Next-Page", " 3 ")
	if got := h.nextPageNumber(header); got != "3" {
		t.Errorf("nextPageNumber = %q, want 3", got)
	}
}

// TestNextPageNumber_LeavesAnythingElseToTheLinkHeader proves an empty, zero,
// negative or non-numeric header — or a dialect naming no header at all —
// yields no page number, so the pager falls back to Link rather than
// requesting a page the forge never named.
func TestNextPageNumber_LeavesAnythingElseToTheLinkHeader(t *testing.T) {
	paged := &forgeHTTP{dialect: forgeDialect{pageHeader: "X-Next-Page"}}
	for _, value := range []string{"", "0", "00", "-2", "two", "2; drop"} {
		header := http.Header{}
		header.Set("X-Next-Page", value)
		if got := paged.nextPageNumber(header); got != "" {
			t.Errorf("nextPageNumber(%q) = %q, want none", value, got)
		}
	}

	linkOnly := &forgeHTTP{}
	header := http.Header{}
	header.Set("X-Next-Page", "2")
	if got := linkOnly.nextPageNumber(header); got != "" {
		t.Errorf("a Link-only dialect read %q from a header it does not name", got)
	}
}
