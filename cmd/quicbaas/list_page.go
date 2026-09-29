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

package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// allPageSize is the page size --all asks for when --limit is not given: the
// server's maximum, so walking a long listing takes as few round trips as the
// server allows. A server with a lower cap answers with that cap instead.
const allPageSize = 100

// pageFlags is the flag set every list command shares: the paging, sorting
// and search parameters of package page, plus --all to walk every page.
//
// ONE DEFINITION FOR EVERY LIST COMMAND, so --limit means the same thing to
// `job list` as to `project list`, the way page.Params does for the SDK.
type pageFlags struct {
	page.Params

	// All walks every page from --offset to the end instead of printing one.
	All bool
}

// bind registers the shared list flags on cmd.
func (f *pageFlags) bind(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.IntVar(&f.Limit, "limit", 0, "Page size (0 takes the server default of 25; the server caps it at 100)")
	flags.IntVar(&f.Offset, "offset", 0, "Number of records to skip")
	flags.StringVar(&f.Sort, "sort", "", `Order by "<field>" or "<field>:desc"; an unknown field is refused by the server`)
	flags.StringVar(&f.Query, "search", "", "Case-insensitive substring to match against the listing's text fields")
	flags.BoolVar(&f.All, "all", false, "Fetch every page from --offset to the end")
}

// validate refuses the values no server would accept, before a round trip.
func (f *pageFlags) validate() error {
	if f.Limit < 0 {
		return &ListPageFlagError{Flag: "limit", Value: f.Limit}
	}
	if f.Offset < 0 {
		return &ListPageFlagError{Flag: "offset", Value: f.Offset}
	}
	return nil
}

// fetchPages runs fetch for the page the flags ask for and, with --all,
// keeps following next_offset until the server names no further page.
//
// With --all the returned envelope describes the whole walk: it starts at the
// first page's offset, carries the last page's total, and names no next page,
// so the footer reads "showing 1–T of T" and JSON output stays an envelope.
func fetchPages[T any](f *pageFlags, fetch func(page.Params) ([]T, page.Envelope, error)) ([]T, page.Envelope, error) {
	if err := f.validate(); err != nil {
		return nil, page.Envelope{}, err
	}

	params := f.Params
	if f.All && params.Limit == 0 {
		params.Limit = allPageSize
	}

	records, env, err := fetch(params)
	if err != nil || !f.All {
		return records, env, err
	}

	first := env
	for env.HasMore() {
		if env.NextOffset <= env.Offset {
			return nil, page.Envelope{}, &ListPageStalledError{Offset: env.Offset, NextOffset: env.NextOffset}
		}
		params.Offset = env.NextOffset
		var more []T
		more, env, err = fetch(params)
		if err != nil {
			return nil, page.Envelope{}, err
		}
		records = append(records, more...)
	}

	return records, page.Envelope{
		Total:  env.Total,
		Limit:  first.Limit,
		Offset: first.Offset,
		Facets: first.Facets,
	}, nil
}

// writePageFooter prints the "showing N–M of T" line under a table or text
// listing, and where the next page starts when there is one.
//
// It prints nothing for a listing nothing matched: the formatter's own "no
// records" line already says so.
func writePageFooter(w io.Writer, shown int, env page.Envelope) {
	if shown == 0 {
		if env.Total > 0 {
			fmt.Fprintf(w, "Showing 0 of %d: offset %d is past the end.\n", env.Total, env.Offset)
		}
		return
	}

	last := env.Offset + shown
	if env.Total < last {
		// A response that carried no envelope says nothing about the total,
		// and "of 0" under a page of records would be a lie.
		fmt.Fprintf(w, "Showing %d–%d.", env.Offset+1, last)
	} else {
		fmt.Fprintf(w, "Showing %d–%d of %d.", env.Offset+1, last, env.Total)
	}
	if env.HasMore() {
		fmt.Fprintf(w, " Next page: --offset %d (or --all for every page).", env.NextOffset)
	}
	fmt.Fprintln(w)
}

// ListPageFlagError is returned when a list command's --limit or --offset is
// negative.
type ListPageFlagError struct {
	Flag  string
	Value int
}

// Error implements error.
func (e *ListPageFlagError) Error() string {
	return "--" + e.Flag + " must not be negative, got " + strconv.Itoa(e.Value)
}

// ListPageStalledError is returned when --all is walking a listing and the
// server names a next page that does not move past the current one, which
// would otherwise loop forever.
type ListPageStalledError struct {
	Offset     int
	NextOffset int
}

// Error implements error.
func (e *ListPageStalledError) Error() string {
	return "list --all: the server named next_offset " + strconv.Itoa(e.NextOffset) +
		" on the page at offset " + strconv.Itoa(e.Offset) + ", which does not advance"
}
