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

// Package page carries the paging, sorting and search contract every
// QuicBaaS list endpoint speaks.
//
// ONE DEFINITION, EMBEDDED BY EVERY LIST REQUEST AND RESPONSE, so `limit`
// means the same thing to the jobs client as to the accounts client and a
// consumer learns the contract once. The server side of the same contract is
// orchestration.Page / orchestration.PageResult; the wire shape is documented
// in docs/software-factory/api.md.
package page

// Params are the paging, sorting and search parameters a list request may
// carry. The zero value asks for the server's default page — the first 25
// records in the endpoint's natural order — so a caller that does not page
// need not mention it.
type Params struct {
	// Limit is the page size. Zero takes the server default (25); the
	// server caps any request above its maximum (100) rather than refusing
	// it.
	Limit int `json:"limit,omitempty"`

	// Offset is how many records to skip.
	Offset int `json:"offset,omitempty"`

	// Sort is "<field>" or "<field>:desc". The field must be one the
	// endpoint documents; an unknown one is refused with a 400 naming it.
	Sort string `json:"sort,omitempty"`

	// Query is a case-insensitive substring matched against the endpoint's
	// documented text fields.
	Query string `json:"q,omitempty"`
}

// Envelope is the paging half of a list response. It travels beside the
// endpoint's own records key — "jobs", "tasks", "accounts" — which keeps its
// name and meaning, so a consumer that ignores paging reads the response it
// always has.
type Envelope struct {
	// Total is how many records matched the filters and the search, BEFORE
	// the page was cut. It is the number a "showing 25 of N" reads.
	Total int `json:"total"`

	// Limit and Offset are the values the server APPLIED, which is not
	// always what was asked for: a limit above the server's maximum comes
	// back capped.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// NextOffset is the offset of the following page. It is ABSENT on the
	// last page, which is how a caller knows to stop without arithmetic.
	NextOffset int `json:"next_offset,omitempty"`
}

// HasMore reports whether records remain past this page.
func (e Envelope) HasMore() bool { return e.NextOffset > 0 }
