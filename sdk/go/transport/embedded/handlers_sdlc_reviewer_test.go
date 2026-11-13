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

package embedded

import (
	"context"
	"reflect"
	"testing"
	"time"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCEmbedded_ARebindKeepsTheReviewerProfiles proves the in-process hop
// carries every alternate, every field of each, and the presence of
// counts_toward_merge in both directions — the embedded half of the
// reviewer_not_on_the_wire regression.
func TestSDLCEmbedded_ARebindKeepsTheReviewerProfiles(t *testing.T) {
	advisory, counted := false, true
	sdk := &sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer:  sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlc.ReviewerConfig{
			{Name: "qwen", Agent: "qwen-reviewer", Login: "q", TokenSecretRef: "q-tok", CountsTowardMerge: &advisory},
			{Name: "gpt", Agent: "codex-reviewer", Login: "g", TokenSecretRef: "g-tok", CountsTowardMerge: &counted, StatusContext: "quicbaas/review"},
			{Name: "unset", Agent: "codex-reviewer", Login: "u", TokenSecretRef: "u-tok"},
		},
	}
	domain := sdlcBindingToDomain(sdk)
	back := sdlcBindingFromDomain(domain)
	if !reflect.DeepEqual(back.Reviewer, sdk.Reviewer) || !reflect.DeepEqual(back.Reviewers, sdk.Reviewers) {
		t.Fatalf("round trip: %+v / %+v", back.Reviewer, back.Reviewers)
	}
	if back.Reviewers[2].CountsTowardMerge != nil {
		t.Fatal("an unset counts_toward_merge came back set")
	}
	// The two records never share a flag: editing one cannot edit the other.
	*domain.Reviewers[0].CountsTowardMerge = true
	if *sdk.Reviewers[0].CountsTowardMerge {
		t.Fatal("the domain record aliases the SDK's flag")
	}
	if none := sdlcBindingFromDomain(sdlcBindingToDomain(&sdlc.ProjectBinding{ProjectID: "p"})); none.Reviewers != nil {
		t.Fatalf("absent alternates became %+v", none.Reviewers)
	}
}

func TestSDLCEmbedded_ReviewerVerbsRefuseWithoutTheModuleAndOnBadTypes(t *testing.T) {
	ctx := context.Background()
	for method, req := range map[string]interface{}{
		"Reviewer":      &sdlc.ReviewerRequest{ProjectID: "p", PullRequest: 1},
		"SetReviewer":   &sdlc.SetReviewerRequest{ProjectID: "p", PullRequest: 1, Profile: "qwen"},
		"ClearReviewer": &sdlc.ReviewerRequest{ProjectID: "p", PullRequest: 1},
	} {
		handler := unaryHandlers["quicbaas.SDLC/"+method]
		if handler == nil {
			t.Fatalf("%s is not routed", method)
		}
		if _, err := handler(ctx, sdlcNilFacade{}, req); err != pkgsdlc.ErrUnavailable {
			t.Fatalf("%s without the module: %v", method, err)
		}
		if _, err := handler(ctx, sdlcNilFacade{}, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
	at := time.Unix(1_700_000_000, 0).UTC()
	got := sdlcReviewerSelectionFromDomain(&pkgsdlc.ReviewerSelection{
		ProjectID: "p", PullRequest: 41, Profile: "qwen", Overridden: true, StatusContext: "quicbaas/review/qwen", UpdatedAt: at,
	})
	if got.Profile != "qwen" || !got.Overridden || got.CountsTowardMerge || !got.UpdatedAt.Equal(at) {
		t.Fatalf("selection = %+v", got)
	}
	if sdlcReviewerSelectionFromDomain(nil) != nil {
		t.Fatal("a nil selection became a value")
	}
}

func TestSDLCEmbedded_AdoptIssueRefusesWithoutTheModuleAndOnBadTypes(t *testing.T) {
	handler := unaryHandlers["quicbaas.SDLC/AdoptIssue"]
	if handler == nil {
		t.Fatal("AdoptIssue is not routed")
	}
	ctx := context.Background()
	if _, err := handler(ctx, sdlcNilFacade{}, &sdlc.AdoptIssueRequest{ProjectID: "p", Issue: 1}); err != pkgsdlc.ErrUnavailable {
		t.Fatalf("AdoptIssue without the module: %v", err)
	}
	if _, err := handler(ctx, sdlcNilFacade{}, "wrong"); err == nil {
		t.Fatal("AdoptIssue accepted the wrong request type")
	}
}
