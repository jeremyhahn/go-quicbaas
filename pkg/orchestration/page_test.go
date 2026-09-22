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
	"errors"
	"strconv"
	"strings"
	"testing"
)

// pageRecord is a stand-in for a listed record: one text field to search on
// and one numeric field to order by.
type pageRecord struct {
	ID   string
	Rank int64
}

// pageRecordSpec orders pageRecords by rank (newest first by default) and by
// id, and searches the id.
var pageRecordSpec = PageSpec[*pageRecord]{
	Sorts: map[string]func(a, b *pageRecord) int{
		"id":   func(a, b *pageRecord) int { return compareStrings(a.ID, b.ID) },
		"rank": func(a, b *pageRecord) int { return compareInt64(a.Rank, b.Rank) },
	},
	DefaultSort:       "rank",
	DefaultDescending: true,
	Text:              func(r *pageRecord) []string { return []string{r.ID} },
}

// pageRecords builds n records with ascending ranks and ids rec-00..rec-NN.
func pageRecords(n int) []*pageRecord {
	records := make([]*pageRecord, 0, n)
	for i := range n {
		records = append(records, &pageRecord{
			ID:   "rec-" + strconv.Itoa(i),
			Rank: int64(i),
		})
	}
	return records
}

func ids(records []*pageRecord) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.ID)
	}
	return out
}

// TestApplyPage_CutsPageAndCounts proves the envelope's three numbers: the
// page holds Limit records, Total counts the whole matching set, and
// NextOffset points at the record after the page.
func TestApplyPage_CutsPageAndCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		page           *Page
		wantIDs        []string
		wantTotal      int
		wantLimit      int
		wantOffset     int
		wantNextOffset int
		wantHasNext    bool
	}{
		{
			name:           "first page",
			page:           &Page{Limit: 3},
			wantIDs:        []string{"rec-9", "rec-8", "rec-7"},
			wantTotal:      10,
			wantLimit:      3,
			wantNextOffset: 3,
			wantHasNext:    true,
		},
		{
			name:           "middle page",
			page:           &Page{Limit: 3, Offset: 3},
			wantIDs:        []string{"rec-6", "rec-5", "rec-4"},
			wantTotal:      10,
			wantLimit:      3,
			wantOffset:     3,
			wantNextOffset: 6,
			wantHasNext:    true,
		},
		{
			name:       "last page carries no next offset",
			page:       &Page{Limit: 3, Offset: 9},
			wantIDs:    []string{"rec-0"},
			wantTotal:  10,
			wantLimit:  3,
			wantOffset: 9,
		},
		{
			name:       "offset past the end is an empty page, not an error",
			page:       &Page{Limit: 3, Offset: 50},
			wantIDs:    []string{},
			wantTotal:  10,
			wantLimit:  3,
			wantOffset: 50,
		},
		{
			name:      "a zero page takes the default limit",
			page:      &Page{},
			wantIDs:   nil,
			wantTotal: 10,
			wantLimit: DefaultPageLimit,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := ApplyPage(pageRecords(10), tc.page, pageRecordSpec)
			if err != nil {
				t.Fatalf("ApplyPage() error = %v, want nil", err)
			}
			if result.Total != tc.wantTotal {
				t.Errorf("Total = %d, want %d", result.Total, tc.wantTotal)
			}
			if result.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", result.Limit, tc.wantLimit)
			}
			if result.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", result.Offset, tc.wantOffset)
			}
			if result.HasNext != tc.wantHasNext {
				t.Errorf("HasNext = %v, want %v", result.HasNext, tc.wantHasNext)
			}
			if result.NextOffset != tc.wantNextOffset {
				t.Errorf("NextOffset = %d, want %d", result.NextOffset, tc.wantNextOffset)
			}
			if len(tc.wantIDs) > 0 {
				got := ids(result.Items)
				if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
					t.Errorf("items = %v, want %v", got, tc.wantIDs)
				}
			}
			if result.Items == nil {
				t.Error("Items is nil; an empty page must render as [] and not null")
			}
		})
	}
}

// TestApplyPage_NilPageIsNotAPagingRequest proves the distinction the paged
// listings rest on: a NIL page means "no paging asked for" and returns the
// whole ordered set, while a ZERO page is a paging request that takes the
// default limit. Collapsing the two would silently truncate every caller that
// cannot page — the health monitor's session sweep, and any transport whose
// request message has no paging fields.
func TestApplyPage_NilPageIsNotAPagingRequest(t *testing.T) {
	t.Parallel()

	unpaged, err := ApplyPage(pageRecords(60), nil, pageRecordSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if len(unpaged.Items) != 60 {
		t.Errorf("unpaged items = %d, want all 60", len(unpaged.Items))
	}
	if unpaged.Limit != 0 {
		t.Errorf("Limit = %d, want 0 to say no limit was applied", unpaged.Limit)
	}
	if unpaged.HasNext {
		t.Error("HasNext = true on an unpaged listing")
	}
	if unpaged.Items[0].ID != "rec-59" {
		t.Errorf("first item = %q, want the natural order to still apply", unpaged.Items[0].ID)
	}

	paged, err := ApplyPage(pageRecords(60), &Page{}, pageRecordSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if len(paged.Items) != DefaultPageLimit {
		t.Errorf("zero-page items = %d, want the default %d", len(paged.Items), DefaultPageLimit)
	}
}

// TestApplyPage_LimitIsCappedNotRefused proves a caller asking for more than
// the server will send is served the maximum rather than a refusal, and is
// TOLD what it got.
func TestApplyPage_LimitIsCappedNotRefused(t *testing.T) {
	t.Parallel()

	result, err := ApplyPage(pageRecords(500), &Page{Limit: 10_000}, pageRecordSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if len(result.Items) != MaxPageLimit {
		t.Errorf("page size = %d, want %d", len(result.Items), MaxPageLimit)
	}
	if result.Limit != MaxPageLimit {
		t.Errorf("Limit = %d, want the applied %d, not the requested 10000", result.Limit, MaxPageLimit)
	}
	if result.Total != 500 {
		t.Errorf("Total = %d, want 500", result.Total)
	}
}

// TestApplyPage_SortsOnAllowListedField covers the orders a caller may ask
// for, including the endpoint's default.
func TestApplyPage_SortsOnAllowListedField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sort    string
		wantIDs []string
	}{
		{name: "default is the endpoint's natural order", sort: "", wantIDs: []string{"rec-3", "rec-2", "rec-1"}},
		{name: "ascending field", sort: "rank", wantIDs: []string{"rec-0", "rec-1", "rec-2"}},
		{name: "descending field", sort: "rank:desc", wantIDs: []string{"rec-3", "rec-2", "rec-1"}},
		{name: "explicit ascending suffix", sort: "rank:asc", wantIDs: []string{"rec-0", "rec-1", "rec-2"}},
		{name: "case-insensitive field name", sort: "RANK:DESC", wantIDs: []string{"rec-3", "rec-2", "rec-1"}},
		{name: "second allow-listed field", sort: "id", wantIDs: []string{"rec-0", "rec-1", "rec-2"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := ApplyPage(pageRecords(4), &Page{Limit: 3, Sort: tc.sort}, pageRecordSpec)
			if err != nil {
				t.Fatalf("ApplyPage() error = %v, want nil", err)
			}
			got := ids(result.Items)
			if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
				t.Errorf("items = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

// TestApplyPage_RefusesUnknownSortField proves an order the endpoint cannot
// serve is REFUSED with a typed error naming the field and the allow-list,
// not silently answered in some other order.
func TestApplyPage_RefusesUnknownSortField(t *testing.T) {
	t.Parallel()

	for _, sortParam := range []string{"nope", "nope:desc"} {
		t.Run(sortParam, func(t *testing.T) {
			t.Parallel()

			result, err := ApplyPage(pageRecords(3), &Page{Sort: sortParam}, pageRecordSpec)
			if result != nil {
				t.Error("ApplyPage() returned a page for a sort it cannot serve")
			}
			if !errors.Is(err, ErrPageSortFieldUnknown) {
				t.Fatalf("error = %v, want ErrPageSortFieldUnknown", err)
			}
			var unknown *UnknownSortFieldError
			if !errors.As(err, &unknown) {
				t.Fatalf("error = %v, want *UnknownSortFieldError", err)
			}
			if unknown.Field != "nope" {
				t.Errorf("Field = %q, want %q", unknown.Field, "nope")
			}
			if !strings.Contains(err.Error(), "rank") {
				t.Errorf("message %q does not name the allow-list", err.Error())
			}
		})
	}
}

// TestApplyPage_SearchNarrowsBeforeCounting proves `q` is applied BEFORE the
// count and the cut: Total is what the search left, not the collection size.
func TestApplyPage_SearchNarrowsBeforeCounting(t *testing.T) {
	t.Parallel()

	records := []*pageRecord{
		{ID: "sdlc-1", Rank: 1},
		{ID: "TRIAGE-2", Rank: 2},
		{ID: "sdlc-3", Rank: 3},
		{ID: "review-4", Rank: 4},
	}

	tests := []struct {
		name      string
		query     string
		wantTotal int
		wantFirst string
	}{
		{name: "matches a substring", query: "sdlc", wantTotal: 2, wantFirst: "sdlc-3"},
		{name: "is case insensitive", query: "triage", wantTotal: 1, wantFirst: "TRIAGE-2"},
		{name: "matches nothing", query: "absent", wantTotal: 0},
		{name: "empty query matches everything", query: "", wantTotal: 4, wantFirst: "review-4"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := ApplyPage(records, &Page{Query: tc.query}, pageRecordSpec)
			if err != nil {
				t.Fatalf("ApplyPage() error = %v, want nil", err)
			}
			if result.Total != tc.wantTotal {
				t.Errorf("Total = %d, want %d", result.Total, tc.wantTotal)
			}
			if tc.wantFirst != "" && result.Items[0].ID != tc.wantFirst {
				t.Errorf("first item = %q, want %q", result.Items[0].ID, tc.wantFirst)
			}
		})
	}
}

// TestApplyPage_LeavesTheInputAlone proves the caller's slice is neither
// reordered nor truncated: a service that still holds the listing it handed
// over must find it as it left it.
func TestApplyPage_LeavesTheInputAlone(t *testing.T) {
	t.Parallel()

	records := pageRecords(5)
	before := ids(records)

	if _, err := ApplyPage(records, &Page{Limit: 2, Sort: "rank:desc"}, pageRecordSpec); err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}

	after := ids(records)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("input order = %v, want it unchanged at %v", after, before)
	}
}

// TestApplyPage_WithoutSpecFieldsStillPages proves a spec with no sorts and
// no text extractor pages rather than panicking — the degenerate case a new
// endpoint starts from.
func TestApplyPage_WithoutSpecFieldsStillPages(t *testing.T) {
	t.Parallel()

	result, err := ApplyPage(pageRecords(4), &Page{Limit: 2, Query: "ignored"}, PageSpec[*pageRecord]{})
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if result.Total != 4 {
		t.Errorf("Total = %d, want 4 (no text extractor means no narrowing)", result.Total)
	}
	if len(result.Items) != 2 {
		t.Errorf("page size = %d, want 2", len(result.Items))
	}
}

// TestParsePage_ReadsTheQueryString covers the accepted parameter shapes.
func TestParsePage_ReadsTheQueryString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		limit      string
		offset     string
		sort       string
		query      string
		wantLimit  int
		wantOffset int
		wantSort   string
		wantQuery  string
	}{
		{name: "absent parameters take the defaults", wantLimit: DefaultPageLimit},
		{name: "explicit values", limit: "10", offset: "20", sort: "name", query: "sdlc",
			wantLimit: 10, wantOffset: 20, wantSort: "name", wantQuery: "sdlc"},
		{name: "limit zero means the default", limit: "0", wantLimit: DefaultPageLimit},
		{name: "limit above the maximum is capped", limit: "5000", wantLimit: MaxPageLimit},
		{name: "surrounding whitespace is trimmed", limit: " 5 ", offset: " 1 ", sort: " name ", query: " x ",
			wantLimit: 5, wantOffset: 1, wantSort: "name", wantQuery: "x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			page, err := ParsePage(tc.limit, tc.offset, tc.sort, tc.query)
			if err != nil {
				t.Fatalf("ParsePage() error = %v, want nil", err)
			}
			if page.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", page.Limit, tc.wantLimit)
			}
			if page.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", page.Offset, tc.wantOffset)
			}
			if page.Sort != tc.wantSort {
				t.Errorf("Sort = %q, want %q", page.Sort, tc.wantSort)
			}
			if page.Query != tc.wantQuery {
				t.Errorf("Query = %q, want %q", page.Query, tc.wantQuery)
			}
		})
	}
}

// TestParsePage_RefusesUnreadableNumbers proves a paging parameter that is
// not a non-negative number is a typed refusal naming the parameter, rather
// than a silent zero that would page from somewhere the caller did not ask.
func TestParsePage_RefusesUnreadableNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		limit     string
		offset    string
		wantParam string
		wantValue string
	}{
		{name: "limit is not a number", limit: "abc", wantParam: "limit", wantValue: "abc"},
		{name: "limit is negative", limit: "-1", wantParam: "limit", wantValue: "-1"},
		{name: "offset is not a number", offset: "page-two", wantParam: "offset", wantValue: "page-two"},
		{name: "offset is negative", offset: "-25", wantParam: "offset", wantValue: "-25"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			page, err := ParsePage(tc.limit, tc.offset, "", "")
			if page != nil {
				t.Error("ParsePage() returned a page for a parameter it could not read")
			}
			if !errors.Is(err, ErrPageParamInvalid) {
				t.Fatalf("error = %v, want ErrPageParamInvalid", err)
			}
			var invalid *InvalidPageParamError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want *InvalidPageParamError", err)
			}
			if invalid.Param != tc.wantParam || invalid.Value != tc.wantValue {
				t.Errorf("error = %+v, want param %q value %q", invalid, tc.wantParam, tc.wantValue)
			}
			if !strings.Contains(err.Error(), tc.wantParam) {
				t.Errorf("message %q does not name the parameter", err.Error())
			}
		})
	}
}

// TestPageNormalized_HandlesNilAndNegatives proves the nil and out-of-range
// cases a transport can hand over.
func TestPageNormalized_HandlesNilAndNegatives(t *testing.T) {
	t.Parallel()

	var absent *Page
	if got := absent.Normalized(); got.Limit != DefaultPageLimit || got.Offset != 0 {
		t.Errorf("nil page normalized to %+v, want the default page", got)
	}

	got := (&Page{Limit: -5, Offset: -5}).Normalized()
	if got.Limit != DefaultPageLimit {
		t.Errorf("Limit = %d, want %d", got.Limit, DefaultPageLimit)
	}
	if got.Offset != 0 {
		t.Errorf("Offset = %d, want 0", got.Offset)
	}
}

// TestPageResultEnvelope_OmitsNextOffsetOnTheLastPage proves the absence of
// next_offset is the end-of-listing signal, and that the records travel under
// the endpoint's own key.
func TestPageResultEnvelope_OmitsNextOffsetOnTheLastPage(t *testing.T) {
	t.Parallel()

	more, err := ApplyPage(pageRecords(10), &Page{Limit: 4}, pageRecordSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	envelope := more.Envelope("jobs")
	if _, ok := envelope["jobs"]; !ok {
		t.Error("envelope has no \"jobs\" key; the endpoint's records key must survive paging")
	}
	if envelope["total"] != 10 || envelope["limit"] != 4 || envelope["offset"] != 0 {
		t.Errorf("envelope = %v, want total 10 limit 4 offset 0", envelope)
	}
	if envelope["next_offset"] != 4 {
		t.Errorf("next_offset = %v, want 4", envelope["next_offset"])
	}

	last, err := ApplyPage(pageRecords(4), &Page{Limit: 4}, pageRecordSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if _, ok := last.Envelope("jobs")["next_offset"]; ok {
		t.Error("next_offset present on the last page; its absence is how a client stops")
	}
}

// TestPageSpecSortFields_AreLexicalAndComplete proves the allow-list a
// refusal quotes is the spec's own, in a stable order.
func TestPageSpecSortFields_AreLexicalAndComplete(t *testing.T) {
	t.Parallel()

	fields := pageRecordSpec.SortFields()
	if strings.Join(fields, ",") != "id,rank" {
		t.Errorf("SortFields() = %v, want [id rank]", fields)
	}

	empty := PageSpec[*pageRecord]{}.SortFields()
	if len(empty) != 0 {
		t.Errorf("SortFields() on an empty spec = %v, want none", empty)
	}
}

// TestParseOptionalBool_ReadsTheThreeStates proves the tri-state filter
// parameter distinguishes "narrow to true", "narrow to false" and "do not
// narrow", which is the whole reason it is a pointer.
func TestParseOptionalBool_ReadsTheThreeStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want *bool
	}{
		{name: "absent does not narrow", raw: "", want: nil},
		{name: "blank does not narrow", raw: "   ", want: nil},
		{name: "true narrows to true", raw: "true", want: boolPtr(true)},
		{name: "false narrows to false", raw: "false", want: boolPtr(false)},
		{name: "one is true", raw: "1", want: boolPtr(true)},
		{name: "zero is false", raw: "0", want: boolPtr(false)},
		{name: "surrounding whitespace is trimmed", raw: " true ", want: boolPtr(true)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOptionalBool("auto_spawn", tc.raw)
			if err != nil {
				t.Fatalf("ParseOptionalBool() error = %v, want nil", err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("ParseOptionalBool(%q) = %v, want nil", tc.raw, *got)
			case tc.want != nil && got == nil:
				t.Fatalf("ParseOptionalBool(%q) = nil, want %v", tc.raw, *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("ParseOptionalBool(%q) = %v, want %v", tc.raw, *got, *tc.want)
			}
		})
	}
}

// TestParseOptionalBool_RefusesUnreadableValues proves a value that is not a
// boolean is a typed refusal naming the parameter, rather than a silent false
// — which would narrow the listing to exactly the records the caller did not
// ask for.
func TestParseOptionalBool_RefusesUnreadableValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"maybe", "yes please", "2"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOptionalBool("auto_spawn", raw)
			if got != nil {
				t.Error("ParseOptionalBool() returned a value for input it could not read")
			}
			if !errors.Is(err, ErrFilterParamInvalid) {
				t.Fatalf("error = %v, want ErrFilterParamInvalid", err)
			}
			var invalid *InvalidFilterParamError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want *InvalidFilterParamError", err)
			}
			if invalid.Param != "auto_spawn" || invalid.Value != raw {
				t.Errorf("error = %+v, want param %q value %q", invalid, "auto_spawn", raw)
			}
			if !strings.Contains(err.Error(), "auto_spawn") {
				t.Errorf("message %q does not name the parameter", err.Error())
			}
		})
	}
}

// boolPtr is the tri-state filter's "present and this value".
func boolPtr(v bool) *bool { return &v }
