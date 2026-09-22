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
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// pageFromSDK translates the SDK's paging parameters into the service's page
// request.
//
// IT IS ALWAYS A PAGING REQUEST, even when the caller set nothing: this
// transport carries the four parameters, so an in-process listing must answer
// the way the same SDK call answers over REST — the first
// orchestration.DefaultPageLimit records — rather than handing back a whole
// collection one transport would truncate and the other would not.
func pageFromSDK(params page.Params) *orchestration.Page {
	return &orchestration.Page{
		Limit:  params.Limit,
		Offset: params.Offset,
		Sort:   params.Sort,
		Query:  params.Query,
	}
}

// envelopeFromPage translates a served page into the SDK's paging envelope.
func envelopeFromPage[T any](result *orchestration.PageResult[T]) page.Envelope {
	if result == nil {
		return page.Envelope{}
	}
	return page.Envelope{
		Total:      result.Total,
		Limit:      result.Limit,
		Offset:     result.Offset,
		NextOffset: result.NextOffset,
	}
}
