//go:build rest

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

package rest

import (
	stderrors "errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// List Paging Parameters
//
// ONE PARSER FOR EVERY LIST ENDPOINT. `limit`, `offset`, `sort` and `q` mean
// the same thing on the jobs listing as on the accounts listing because they
// are read in one place and validated by one contract — see
// orchestration.ParsePage and docs/software-factory/api.md.
//
// THE HANDLER ONLY PARSES. Which fields may be sorted on, and which are
// searched, is the service's answer (orchestration.PageSpec), not the
// transport's: a second transport reading the same parameters must get the
// same allow-list rather than its own copy.
// =============================================================================

// pageFromRequest reads the shared paging, sorting and search parameters off
// a list request's query string.
func pageFromRequest(r *http.Request) (*orchestration.Page, error) {
	query := r.URL.Query()
	return orchestration.ParsePage(
		query.Get("limit"),
		query.Get("offset"),
		query.Get("sort"),
		query.Get("q"),
	)
}

// isPageInputError reports whether err is a refused paging parameter — an
// unreadable limit or offset, or a sort field the endpoint does not order by.
// Each is the caller's mistake and answers 400 naming the parameter.
func isPageInputError(err error) bool {
	return stderrors.Is(err, orchestration.ErrPageParamInvalid) ||
		stderrors.Is(err, orchestration.ErrPageSortFieldUnknown)
}
