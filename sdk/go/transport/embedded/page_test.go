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

package embedded

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// TestPageFromSDK_UnpagedRequestAsksForTheDefault proves an in-process caller
// that never mentions paging asks for the same thing a REST caller with no
// query string does: a paging request that takes the server's defaults, so
// one SDK call does not answer differently on two transports.
func TestPageFromSDK_UnpagedRequestAsksForTheDefault(t *testing.T) {
	t.Parallel()

	zero := pageFromSDK(page.Params{})
	if zero == nil {
		t.Fatal("pageFromSDK(zero) = nil, want a paging request taking the defaults")
	}
	if got := zero.Normalized().Limit; got != orchestration.DefaultPageLimit {
		t.Errorf("default limit = %d, want %d", got, orchestration.DefaultPageLimit)
	}

	got := pageFromSDK(page.Params{Limit: 10, Offset: 20, Sort: "state", Query: "sdlc"})
	if got == nil {
		t.Fatal("pageFromSDK() = nil for a paged request")
	}
	want := orchestration.Page{Limit: 10, Offset: 20, Sort: "state", Query: "sdlc"}
	if *got != want {
		t.Errorf("pageFromSDK() = %+v, want %+v", *got, want)
	}
}

// TestEnvelopeFromPage_CarriesTheServedNumbers proves the envelope repeats
// what the service applied, and survives a nil result rather than panicking.
func TestEnvelopeFromPage_CarriesTheServedNumbers(t *testing.T) {
	t.Parallel()

	result, err := orchestration.ApplyPage(
		[]*orchestration.WorkflowDefinition{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		&orchestration.Page{Limit: 2},
		orchestration.WorkflowPageSpec(),
	)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v", err)
	}

	got := envelopeFromPage(result)
	if got.Total != 3 || got.Limit != 2 || got.Offset != 0 || got.NextOffset != 2 {
		t.Errorf("envelopeFromPage() = %+v, want total 3 limit 2 offset 0 next 2", got)
	}
	if !got.HasMore() {
		t.Error("HasMore() = false, want true while a next page exists")
	}

	empty := envelopeFromPage[*orchestration.WorkflowDefinition](nil)
	if empty.Total != 0 || empty.Limit != 0 || empty.Offset != 0 || empty.NextOffset != 0 || empty.Facets != nil {
		t.Errorf("envelopeFromPage(nil) = %+v, want the zero envelope", empty)
	}
}
