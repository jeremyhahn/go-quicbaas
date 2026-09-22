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

package orchestration

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// Paging, Sorting and Search
//
// ONE CONTRACT, APPLIED ONCE, FOR EVERY LIST ENDPOINT. Every orchestrator
// listing used to answer with the whole collection: the burn-in tenant's job
// list is ~2,000 records and the UI loaded all of them on every visit. The
// answer is not a per-endpoint page implementation — seven of those drift into
// seven slightly different meanings of `limit` — but one Page request, one
// PageResult envelope, and one ApplyPage that both narrows and orders, driven
// by a per-endpoint PageSpec naming the fields it sorts and searches on.
// =============================================================================

const (
	// DefaultPageLimit is the page size a request that names none receives.
	DefaultPageLimit = 25

	// MaxPageLimit is the largest page any endpoint will serve. A larger
	// limit is CAPPED, not refused: a caller asking for more than the server
	// will send is asking a reasonable question badly, and a 400 would cost
	// it the answer it could have had.
	MaxPageLimit = 100

	// sortDescendingSuffix marks a descending sort: "started_at:desc".
	sortDescendingSuffix = ":desc"

	// sortAscendingSuffix is accepted for symmetry so a caller that spells
	// the direction out on both halves of a toggle is not refused.
	sortAscendingSuffix = ":asc"
)

// Page is the paging, sorting and search request every orchestrator list
// endpoint accepts. The zero value asks for the first DefaultPageLimit
// records in the endpoint's natural order.
type Page struct {
	// Limit is the page size. Zero means DefaultPageLimit; anything above
	// MaxPageLimit is capped to it.
	Limit int `json:"limit,omitempty"`

	// Offset is how many records to skip.
	Offset int `json:"offset,omitempty"`

	// Sort is "<field>" or "<field>:desc", naming a field from the
	// endpoint's allow-list. Empty means the endpoint's natural order.
	Sort string `json:"sort,omitempty"`

	// Query is a case-insensitive substring matched against the endpoint's
	// documented text fields. Empty matches everything.
	Query string `json:"q,omitempty"`
}

// PageSpec describes how ONE endpoint's records are ordered and searched.
//
// Sorts is the allow-list: a field absent from it is refused rather than
// ignored, because a listing silently returned in an order the caller did not
// ask for is indistinguishable from one that was.
type PageSpec[T any] struct {
	// Sorts maps an allow-listed field name to its ASCENDING comparator,
	// which returns a negative number, zero, or a positive number as a
	// sorts before, with, or after b.
	Sorts map[string]func(a, b T) int

	// DefaultSort names the field used when the request asks for no order.
	DefaultSort string

	// DefaultDescending reverses the default field — "newest first" for
	// every endpoint whose natural order is a timestamp.
	DefaultDescending bool

	// Text extracts the fields the search query matches against.
	Text func(T) []string
}

// SortFields returns the endpoint's allow-listed sort fields in lexical
// order, for documentation and for the refusal message.
func (s PageSpec[T]) SortFields() []string {
	fields := make([]string, 0, len(s.Sorts))
	for field := range s.Sorts {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// PageResult is one page of a list endpoint's answer.
type PageResult[T any] struct {
	// Items is the page itself, never nil.
	Items []T

	// Total is the number of records AFTER filters and search, BEFORE
	// paging — the size of the collection the page was cut from.
	Total int

	// Limit and Offset are the applied values, not the requested ones: a
	// caller that asked for 10,000 records is told it was served 100.
	Limit  int
	Offset int

	// NextOffset is the offset of the following page; meaningful only when
	// HasNext is set.
	NextOffset int

	// HasNext reports whether records remain past this page.
	HasNext bool
}

// Envelope renders the page as a transport response body, with the records
// under itemsKey.
//
// THE ITEMS KEY IS THE ENDPOINT'S EXISTING ONE — "jobs", "tasks", "agents" —
// so every consumer that reads the listing today keeps reading it after
// paging arrives, and only a consumer that wants a second page needs to know
// the envelope exists. next_offset is omitted on the last page: its absence
// is how a client knows it has reached the end without arithmetic.
func (p *PageResult[T]) Envelope(itemsKey string) map[string]interface{} {
	envelope := map[string]interface{}{
		itemsKey: p.Items,
		"total":  p.Total,
		"limit":  p.Limit,
		"offset": p.Offset,
	}
	if p.HasNext {
		envelope["next_offset"] = p.NextOffset
	}
	return envelope
}

// ParsePage reads the four raw query parameters into a Page.
//
// It is the ONE place a transport turns strings into a paging request, so
// "limit=abc" means the same thing — a 400 naming the parameter — on every
// endpoint and every protocol. An empty string is an absent parameter and
// takes the default; a value that is not a number, or a negative one, is a
// typed InvalidPageParamError.
func ParsePage(limit, offset, sortField, query string) (*Page, error) {
	page := &Page{Sort: strings.TrimSpace(sortField), Query: strings.TrimSpace(query)}

	parsed, err := parsePageInt("limit", limit)
	if err != nil {
		return nil, err
	}
	page.Limit = parsed

	parsed, err = parsePageInt("offset", offset)
	if err != nil {
		return nil, err
	}
	page.Offset = parsed

	return page.Normalized(), nil
}

// parsePageInt reads one non-negative integer parameter. An empty value is
// zero, which each field reads as "unset" in Normalized.
func parsePageInt(param, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, &InvalidPageParamError{Param: param, Value: raw}
	}
	return value, nil
}

// Normalized returns a copy with the limit defaulted and capped. A nil Page
// normalizes to the default page, so every caller can hand ApplyPage whatever
// the request carried.
func (p *Page) Normalized() *Page {
	normalized := Page{}
	if p != nil {
		normalized = *p
	}
	if normalized.Limit <= 0 {
		normalized.Limit = DefaultPageLimit
	}
	if normalized.Limit > MaxPageLimit {
		normalized.Limit = MaxPageLimit
	}
	if normalized.Offset < 0 {
		normalized.Offset = 0
	}
	return &normalized
}

// sortRequest splits the Sort parameter into a field and a direction,
// falling back to the endpoint's natural order when none was asked for.
func (p *Page) sortRequest(defaultField string, defaultDescending bool) (field string, descending bool) {
	raw := ""
	if p != nil {
		raw = strings.TrimSpace(p.Sort)
	}
	if raw == "" {
		return defaultField, defaultDescending
	}
	lowered := strings.ToLower(raw)
	switch {
	case strings.HasSuffix(lowered, sortDescendingSuffix):
		return strings.TrimSuffix(lowered, sortDescendingSuffix), true
	case strings.HasSuffix(lowered, sortAscendingSuffix):
		return strings.TrimSuffix(lowered, sortAscendingSuffix), false
	default:
		return lowered, false
	}
}

// ApplyPage searches, orders and cuts one page out of items.
//
// The order of operations is the contract: SEARCH, then SORT, then CUT, so
// Total counts what the caller asked for rather than what happened to fit on
// a page. The input slice is never reordered — callers hand ApplyPage a
// listing they may still be holding — and Items is always non-nil so a
// transport renders an empty page as [] rather than null.
//
// A NIL PAGE IS "NO PAGING ASKED FOR" AND RETURNS EVERYTHING, ordered and
// searched but not cut. It is NOT the same as a zero Page, which is a paging
// request that takes the defaults. The distinction is what keeps the callers
// that cannot page from being silently truncated: the health monitor's
// session sweep, and a transport whose request message has no paging fields
// yet, both pass nil and must see the whole set — a 25-record answer to
// "how many sessions are alive" is a wrong answer, not a first page. Every
// transport that DOES carry the parameters passes a non-nil page (ParsePage
// never returns nil), so an absent ?limit= still means the default page.
//
// An unknown sort field is a typed UnknownSortFieldError, which every
// transport maps to a 400 naming the field.
func ApplyPage[T any](items []T, page *Page, spec PageSpec[T]) (*PageResult[T], error) {
	field, descending := page.sortRequest(spec.DefaultSort, spec.DefaultDescending)
	comparator, known := spec.Sorts[field]
	if field != "" && !known {
		return nil, &UnknownSortFieldError{Field: field, Allowed: spec.SortFields()}
	}

	query := ""
	if page != nil {
		query = page.Query
	}
	matched := searchItems(items, query, spec.Text)

	if comparator != nil {
		slices.SortStableFunc(matched, orderedBy(comparator, descending))
	}

	total := len(matched)
	start, end := 0, total
	limit, offset := 0, 0
	if page != nil {
		normalized := page.Normalized()
		limit, offset = normalized.Limit, normalized.Offset
		start = min(offset, total)
		end = min(start+limit, total)
	}

	// A NON-NIL PAGE, ALWAYS: slicing a nil listing yields nil, and a nil
	// slice renders as JSON null, which a UI reading items.length crashes on.
	pageItems := matched[start:end]
	if pageItems == nil {
		pageItems = []T{}
	}

	result := &PageResult[T]{
		Items:  pageItems,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	if end < total {
		result.NextOffset = end
		result.HasNext = true
	}
	return result, nil
}

// searchItems returns the records whose text fields contain query, or a copy
// of every record when no query was asked for. The copy is what keeps
// ApplyPage from reordering the caller's slice.
func searchItems[T any](items []T, query string, text func(T) []string) []T {
	if query == "" || text == nil {
		return slices.Clone(items)
	}
	needle := strings.ToLower(query)
	matched := make([]T, 0, len(items))
	for _, item := range items {
		for _, field := range text(item) {
			if field != "" && strings.Contains(strings.ToLower(field), needle) {
				matched = append(matched, item)
				break
			}
		}
	}
	return matched
}

// orderedBy adapts an ascending comparator to the requested direction.
func orderedBy[T any](comparator func(a, b T) int, descending bool) func(a, b T) int {
	if !descending {
		return comparator
	}
	return func(a, b T) int { return comparator(b, a) }
}

// compareStrings orders two strings case-insensitively, so a listing sorted
// by name reads the way an operator expects rather than putting every
// capitalised name first.
func compareStrings(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// compareInt64 orders two integer fields — timestamps, counters — ascending.
func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
