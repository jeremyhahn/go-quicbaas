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

package grpc

import (
	"math"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// =============================================================================
// List Paging on the Wire
//
// The list RPCs carry limit, offset, sort and q as request fields and answer
// with total, limit, offset and next_offset — the same contract the REST
// transport speaks as query parameters and a JSON envelope (see package page
// and docs/software-factory/api.md). These helpers are the one place the SDK
// types cross to and from those fields.
// =============================================================================

// pageFields is a list request's paging fields as the proto carries them.
type pageFields struct {
	limit, offset int32
	sort, q       string
}

// pageFieldsFromParams renders the SDK paging parameters as proto fields.
//
// A NEGATIVE VALUE IS SENT AS ZERO, the server default, exactly as the REST
// transport omits it from the query string: the two transports must not
// answer the same SDK call differently. A value past the int32 range is
// saturated; the server caps the limit at its maximum regardless.
func pageFieldsFromParams(params page.Params) pageFields {
	return pageFields{
		limit:  wireInt32(params.Limit),
		offset: wireInt32(params.Offset),
		sort:   params.Sort,
		q:      params.Query,
	}
}

// pageEnvelopeFromProto reads a list response's paging envelope.
func pageEnvelopeFromProto(total, limit, offset, nextOffset int32) page.Envelope {
	return page.Envelope{
		Total:      int(total),
		Limit:      int(limit),
		Offset:     int(offset),
		NextOffset: int(nextOffset),
	}
}

// wireInt32 clamps a non-negative paging value into the wire's int32.
func wireInt32(v int) int32 {
	if v <= 0 {
		return 0
	}
	if v >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}
