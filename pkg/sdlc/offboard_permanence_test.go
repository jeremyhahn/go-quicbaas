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

package sdlc

import (
	"errors"
	"fmt"
	"testing"
)

// TestForgeRefusalIsPermanent_TheTwoRealFailures pins the exact errors that
// stranded a binding on the burn-in node, in the order they surfaced.
//
// The second one is why this predicate exists rather than a bare not-found
// check: restoring the deleted secret only moved the refusal from 404 to 403,
// and the binding was still undeletable.
func TestForgeRefusalIsPermanent_TheTwoRealFailures(t *testing.T) {
	secretGone := fmt.Errorf("resolve forge token: %w", ErrNotFound)
	if !ForgeRefusalIsPermanent(secretGone) {
		t.Fatal("a binding whose token secret is gone can never be offboarded")
	}

	notAdmin := &GiteaAPIError{
		Status:    403,
		Operation: "ListWebhooks",
		Message:   "user should be an owner or a collaborator with admin write of a repository",
	}
	if !ForgeRefusalIsPermanent(notAdmin) {
		t.Fatal("a 403 on ListWebhooks cannot be resolved by repeating the call")
	}
	// Wrapped, which is how it actually arrives through backendError.
	if !ForgeRefusalIsPermanent(fmt.Errorf("sdlc: ListWebhooks: %w", notAdmin)) {
		t.Fatal("the predicate must see through wrapping")
	}
}

// TestForgeRefusalIsPermanent_UnknownAndTransientRefuse guards the half that
// keeps this from becoming "ignore all offboard failures".
//
// A forge that is merely DOWN will work on retry, and dropping the local record
// then would leave a live webhook delivering to a node that no longer routes
// it, while reporting a cleanup that did not happen.
func TestForgeRefusalIsPermanent_UnknownAndTransientRefuse(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"server error", &GiteaAPIError{Status: 500, Operation: "ListWebhooks"}},
		{"bad gateway", &GiteaAPIError{Status: 502, Operation: "ListWebhooks"}},
		{"rate limited", &GiteaAPIError{Status: 429, Operation: "ListWebhooks"}},
		{"request timeout", &GiteaAPIError{Status: 408, Operation: "ListWebhooks"}},
		{"transport failure", errors.New("dial tcp: connection refused")},
	}
	for _, c := range cases {
		if ForgeRefusalIsPermanent(c.err) {
			t.Fatalf("%s must be treated as possibly transient so the operator retries", c.name)
		}
	}
}
