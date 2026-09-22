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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// TestSetPageQuery_WritesOnlyWhatWasAsked proves an unpaged request adds no
// parameters and a paged one adds exactly its own.
func TestSetPageQuery_WritesOnlyWhatWasAsked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params page.Params
		want   string
	}{
		{name: "nothing asked for", params: page.Params{}, want: ""},
		{name: "limit only", params: page.Params{Limit: 10}, want: "limit=10"},
		{name: "offset only", params: page.Params{Offset: 25}, want: "offset=25"},
		{
			name:   "every parameter",
			params: page.Params{Limit: 10, Offset: 25, Sort: "started_at:desc", Query: "sdlc"},
			want:   "limit=10&offset=25&q=sdlc&sort=started_at%3Adesc",
		},
		{
			name:   "values are escaped",
			params: page.Params{Query: "a&b=c"},
			want:   "q=a%26b%3Dc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query := neturl.Values{}
			setPageQuery(query, tc.params)
			if got := query.Encode(); got != tc.want {
				t.Errorf("query = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPageQuerySuffix_AppendsToAnExistingQuery proves the suffix form joins
// onto a URL that already carries a tenant, and disappears entirely when
// nothing was asked for.
func TestPageQuerySuffix_AppendsToAnExistingQuery(t *testing.T) {
	t.Parallel()

	if got := pageQuerySuffix(page.Params{}); got != "" {
		t.Errorf("suffix = %q, want empty for an unpaged request", got)
	}

	url := "/api/v1/orchestrator/jobs?tenant_id=acme" + pageQuerySuffix(page.Params{Limit: 5, Sort: "state"})
	parsed, err := neturl.Parse(url)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", url, err)
	}
	query := parsed.Query()
	if query.Get("tenant_id") != "acme" {
		t.Errorf("tenant_id = %q, want acme; the suffix must not disturb the query it joins", query.Get("tenant_id"))
	}
	if query.Get("limit") != "5" || query.Get("sort") != "state" {
		t.Errorf("paging = %v, want limit 5 and sort state", query)
	}
}
