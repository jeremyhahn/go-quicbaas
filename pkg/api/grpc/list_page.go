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
	stderrors "errors"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// =============================================================================
// List Paging Parameters
//
// THE SAME CONTRACT AS EVERY OTHER TRANSPORT. The list RPCs carry limit,
// offset, sort and q as request fields, and they are read by the one parser
// REST, QUIC, Unix, MCP and the embedded transport use —
// orchestration.ParsePage — so a negative limit, or a sort field outside the
// endpoint's allow-list (orchestration/page_specs.go), is refused here exactly
// as it is there: InvalidArgument naming the parameter.
//
// THE PAGE IS NEVER NIL. ParsePage never returns nil, so an RPC that names no
// paging receives the default page, as an HTTP request with no ?limit= does.
// =============================================================================

// pageFromRPC reads a list request's paging fields into a Page.
func pageFromRPC(limit, offset int32, sortField, query string) (*orchestration.Page, error) {
	page, err := orchestration.ParsePage(
		strconv.FormatInt(int64(limit), 10),
		strconv.FormatInt(int64(offset), 10),
		sortField,
		query,
	)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return page, nil
}

// isPageInputError reports whether err is a refused list parameter — an
// unreadable limit or offset, or a sort field the endpoint does not order by.
// Each is the caller's mistake and answers InvalidArgument.
func isPageInputError(err error) bool {
	return stderrors.Is(err, orchestration.ErrPageParamInvalid) ||
		stderrors.Is(err, orchestration.ErrPageSortFieldUnknown)
}

// convertListError maps a list RPC's service error: a refused paging
// parameter is InvalidArgument, anything else goes through the service's own
// converter.
func convertListError(err error, convert func(error) error) error {
	if isPageInputError(err) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return convert(err)
}

// pageInt32 narrows an applied page value to the wire's int32. Every value
// is bounded — limit by orchestration.MaxPageLimit, offset and total by the
// size of a listing held in memory — so the narrowing saturates rather than
// wraps if that ever stops being true.
func pageInt32(v int) int32 {
	return conv.ClampIntToInt32(max(v, 0))
}
