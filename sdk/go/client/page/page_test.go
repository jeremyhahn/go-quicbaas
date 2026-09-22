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

package page_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// listRequest stands in for a list request that embeds the shared params.
type listRequest struct {
	TenantID string `json:"tenant_id"`
	page.Params
}

// listResponse stands in for a list response that embeds the envelope.
type listResponse struct {
	Items []string `json:"items"`
	page.Envelope
}

// TestParams_MarshalFlatAndOmitZero proves the embedded params travel as
// top-level fields — the wire shape the server reads — and that an unpaged
// request sends nothing at all rather than "limit": 0.
func TestParams_MarshalFlatAndOmitZero(t *testing.T) {
	t.Parallel()

	empty, err := json.Marshal(listRequest{TenantID: "acme"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(empty), "limit") {
		t.Errorf("unpaged request = %s, want no paging fields", empty)
	}

	paged, err := json.Marshal(listRequest{
		TenantID: "acme",
		Params:   page.Params{Limit: 10, Offset: 20, Sort: "name:desc", Query: "sdlc"},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, want := range []string{`"limit":10`, `"offset":20`, `"sort":"name:desc"`, `"q":"sdlc"`} {
		if !strings.Contains(string(paged), want) {
			t.Errorf("request = %s, want it to contain %s", paged, want)
		}
	}
}

// TestEnvelope_UnmarshalsTheServerShape proves the envelope decodes from the
// flat body every list endpoint answers with, alongside the records key.
func TestEnvelope_UnmarshalsTheServerShape(t *testing.T) {
	t.Parallel()

	var resp listResponse
	body := `{"items":["a","b"],"total":7,"limit":2,"offset":2,"next_offset":4}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("items = %v, want 2 records", resp.Items)
	}
	if resp.Total != 7 || resp.Limit != 2 || resp.Offset != 2 || resp.NextOffset != 4 {
		t.Errorf("envelope = %+v, want total 7 limit 2 offset 2 next 4", resp.Envelope)
	}
	if !resp.HasMore() {
		t.Error("HasMore() = false, want true while a next offset is named")
	}
}

// TestEnvelope_LastPageHasNoMore proves the absent next_offset a last page
// carries reads as "no more records" rather than "offset zero".
func TestEnvelope_LastPageHasNoMore(t *testing.T) {
	t.Parallel()

	var resp listResponse
	if err := json.Unmarshal([]byte(`{"items":["a"],"total":1,"limit":25,"offset":0}`), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if resp.NextOffset != 0 {
		t.Errorf("NextOffset = %d, want 0 when the server omits it", resp.NextOffset)
	}
	if resp.HasMore() {
		t.Error("HasMore() = true on the last page")
	}
}
