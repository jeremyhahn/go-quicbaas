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
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// =============================================================================
// Paging
// =============================================================================

// forgeListing is one paged listing.
type forgeListing struct {
	operation string
	path      string
	query     url.Values

	// maxPages bounds the listing; exhausted is returned when it is reached
	// and the forge still names a next page.
	maxPages  int
	exhausted error
}

// forgePages reads a listing page by page and hands each decoded page to
// visit. visit returns false to stop early.
//
// THE FORGE, NOT A SHORT PAGE, ENDS THE LISTING. A forge that names the next
// page number in a header of its own (the dialect's pageHeader) is followed by
// that number; otherwise, or when the header is empty, by the Link header's
// rel="next". So a full last page costs no extra request, and a short page in
// the middle (some forges filter a listing after paging it) cannot end it
// early. A next link pointing anywhere but this API root is refused, because
// the request that follows it carries the token.
func forgePages[P any](ctx context.Context, h *forgeHTTP, listing forgeListing, visit func(page P) bool) error {
	query := url.Values{}
	for key, values := range listing.query {
		query[key] = append([]string(nil), values...)
	}
	query.Set("per_page", strconv.Itoa(h.dialect.pageSize))
	path := listing.path

	for page := 1; page <= listing.maxPages; page++ {
		var out P
		var next string
		var header http.Header
		if err := h.do(ctx, forgeRequest{
			method:    http.MethodGet,
			path:      path,
			query:     query,
			out:       &out,
			next:      &next,
			header:    &header,
			operation: listing.operation,
		}); err != nil {
			return err
		}
		if !visit(out) {
			return nil
		}
		if number := h.nextPageNumber(header); number != "" {
			query.Set("page", number)
			continue
		}
		if next == "" {
			return nil
		}
		var err error
		path, query, err = h.followLink(listing.operation, next)
		if err != nil {
			return err
		}
	}
	return listing.exhausted
}

// nextPageNumber returns the page number the dialect's page header names, or
// "" when the forge names none or the header holds anything but a positive
// decimal — which then leaves the Link header to decide.
func (h *forgeHTTP) nextPageNumber(header http.Header) string {
	if h.dialect.pageHeader == "" {
		return ""
	}
	number := strings.TrimSpace(header.Get(h.dialect.pageHeader))
	if !isDecimal(number) || strings.Trim(number, "0") == "" {
		return ""
	}
	return number
}
