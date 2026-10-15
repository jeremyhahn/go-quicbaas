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
	"slices"
	"testing"
)

// TestCanonicalLabelNames_TakesTheRepositorysSpelling proves names match the
// vocabulary case-insensitively and come back as the repository spells them.
func TestCanonicalLabelNames_TakesTheRepositorysSpelling(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	got, err := canonicalLabelNames(repo, []string{"Bug", "needs-triage"}, []string{"bug", "NEEDS-TRIAGE"})
	if err != nil {
		t.Fatalf("canonicalLabelNames: %v", err)
	}
	if want := []string{"Bug", "needs-triage"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCanonicalLabelNames_RefusesAnUndefinedName proves a name the repository
// does not define is refused rather than passed on for the forge to invent.
func TestCanonicalLabelNames_RefusesAnUndefinedName(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	_, err := canonicalLabelNames(repo, []string{"bug"}, []string{"bug", "bgu"})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "labels" {
		t.Fatalf("err = %v, want an InvalidInputError on labels", err)
	}
}
