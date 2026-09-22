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

// Facets is how many records carry each value of each faceted field:
// facets["status"]["completed"] is how many completed records the question
// would yield. A key the endpoint facets on is ALWAYS present, even when its
// counts are empty, so a filter bar renders its choices from the answer
// rather than from a list it keeps itself.
type Facets map[string]map[string]int

// FacetSpec describes how ONE endpoint's records are COUNTED, beside the
// PageSpec that says how they are ordered and searched.
//
// THE COUNTS ARE THE OTHER HALF OF A FILTER BAR. A bar that offers "status:
// blocked" without saying how many blocked records there are makes the
// operator click to find out, and a bar whose counts came from the PAGE would
// say "3" about a tenant with two hundred. So the counts are taken over the
// set the search left, BEFORE the faceted narrowings are applied — standard
// faceted search, where each count answers "what would selecting this yield".
type FacetSpec[T any] struct {
	// Values maps a facet key — "status", "project_id", "agent_id" — to the
	// extractor naming the values one record is counted under.
	//
	// IT RETURNS A SLICE BECAUSE A RECORD CAN BELONG UNDER SEVERAL. One
	// daemon serving claude, codex and opencode matches the backend filter
	// on all three (AgentServesBackend), so counting it once under its
	// primary would print a number the filter contradicts. Empty values are
	// not counted: "no project" is the absence of a choice, not a choice.
	Values map[string]func(T) []string
}

// FacetKeys returns the endpoint's facet keys in lexical order, for
// documentation and for tests that assert the contract.
func (f FacetSpec[T]) FacetKeys() []string {
	keys := make([]string, 0, len(f.Values))
	for key := range f.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

	// Facets counts, per facet key, how many records carry each value —
	// what a faceted filter bar renders beside each choice. It is nil when
	// no paging was asked for; see ApplyFacetedPage.
	Facets Facets
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
	if p.Facets != nil {
		envelope["facets"] = p.Facets
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

// ParseOptionalBool reads a TRI-STATE boolean list filter: present and true,
// present and false, or absent.
//
// It is the ONE place a transport turns such a parameter into a *bool, for
// the same reason ParsePage is the one place paging is parsed: `auto_spawn`
// must mean the same thing, and refuse the same values, on every protocol.
// An empty string is "the caller did not ask", which is nil and narrows
// nothing — NOT false, which narrows to the queued tasks. Anything
// strconv.ParseBool refuses is a typed InvalidFilterParamError naming the
// parameter.
func ParseOptionalBool(param, raw string) (*bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return nil, &InvalidFilterParamError{Param: param, Value: raw}
	}
	return &value, nil
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
	return ApplyFacetedPage(items, page, spec, FacetSpec[T]{}, nil)
}

// ApplyFacetedPage is ApplyPage plus the two halves of faceted search: the
// COUNTS the filter bar renders, and the NARROWING a chosen facet applies.
//
// THE ORDER OF OPERATIONS IS THE CONTRACT, and it is the reason this is one
// function rather than a count helper each endpoint calls where it likes:
//
//	SEARCH (q) -> COUNT THE FACETS -> NARROW BY THEM -> SORT -> CUT
//
// The counts are taken between the search and the narrowing, so they answer
// "how many would selecting this yield" over the set the caller is looking at
// — the standard faceted-search reading. Counting AFTER the narrowing instead
// would print 12 beside "blocked" and 0 beside every other status the moment
// blocked was selected, which is a bar the operator cannot use to change their
// mind. Total still counts what the caller asked for, narrowings included.
//
// keep is the faceted narrowing, nil when the endpoint has none. It runs here
// rather than in the store read because the counts have to see the records it
// removes.
func ApplyFacetedPage[T any](items []T, page *Page, spec PageSpec[T], facets FacetSpec[T], keep func(T) bool) (*PageResult[T], error) {
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

	// A NIL PAGE GETS NO FACETS, for the same reason it gets no cut: it is
	// not a UI listing. The health monitor's session sweep asks for every
	// record and reads none of the counts, and computing them would charge
	// every such caller for a filter bar nobody renders.
	var counted Facets
	if page != nil {
		counted = countFacets(matched, facets)
	}

	if keep != nil {
		narrowed := make([]T, 0, len(matched))
		for _, item := range matched {
			if keep(item) {
				narrowed = append(narrowed, item)
			}
		}
		matched = narrowed
	}

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
		Facets: counted,
	}
	if end < total {
		result.NextOffset = end
		result.HasNext = true
	}
	return result, nil
}

// countFacets counts how many of items carry each value of each facet.
//
// A RECORD IS COUNTED ONCE PER FACET VALUE, however many times its extractor
// names it: an agent whose primary backend also appears in its capabilities
// is one agent serving that backend, not two.
func countFacets[T any](items []T, spec FacetSpec[T]) Facets {
	if len(spec.Values) == 0 {
		return nil
	}
	counted := make(Facets, len(spec.Values))
	seen := make(map[string]struct{})
	for key, values := range spec.Values {
		counts := make(map[string]int)
		for _, item := range items {
			clear(seen)
			for _, value := range values(item) {
				if value == "" {
					continue
				}
				if _, repeated := seen[value]; repeated {
					continue
				}
				seen[value] = struct{}{}
				counts[value]++
			}
		}
		counted[key] = counts
	}
	return counted
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

// compareBool orders false before true, so "autonomous" ascending reads as
// "the ones that are not, then the ones that are".
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
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
