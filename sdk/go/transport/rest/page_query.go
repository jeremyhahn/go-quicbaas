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

package rest

import (
	neturl "net/url"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// setPageQuery writes the paging, sorting and search parameters a list
// request carries onto its query string.
//
// A ZERO FIELD IS OMITTED, NOT SENT AS ZERO: "limit=0" and an absent limit
// both mean "the server default", and sending the former would make every
// unpaged SDK call look paged in an access log.
func setPageQuery(query neturl.Values, params page.Params) {
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		query.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.Sort != "" {
		query.Set("sort", params.Sort)
	}
	if params.Query != "" {
		query.Set("q", params.Query)
	}
}

// pageQuerySuffix renders the same parameters for a URL that already carries
// a query string, escaped, and empty when nothing was asked for.
func pageQuerySuffix(params page.Params) string {
	query := neturl.Values{}
	setPageQuery(query, params)
	encoded := query.Encode()
	if encoded == "" {
		return ""
	}
	return "&" + encoded
}
