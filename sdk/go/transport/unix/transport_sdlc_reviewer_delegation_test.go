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

package unix

import (
	"context"
	"testing"
	"time"

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestTransport_ReviewerVerbs_Delegate proves the Unix transport hands the
// three reviewer-override verbs to the gRPC SDLC handler rather than refusing
// them locally: with no server listening they fail on the socket, never with
// "unknown method".
func TestTransport_ReviewerVerbs_Delegate(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	for method, req := range map[string]interface{}{
		"Reviewer":      &sdlcclient.ReviewerRequest{ProjectID: "p1", PullRequest: 41},
		"SetReviewer":   &sdlcclient.SetReviewerRequest{ProjectID: "p1", PullRequest: 41, Profile: "qwen"},
		"ClearReviewer": &sdlcclient.ReviewerRequest{ProjectID: "p1", PullRequest: 41},
		"AdoptIssue":    &sdlcclient.AdoptIssueRequest{ProjectID: "p1", Issue: 141},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := tr.Request(ctx, "quicbaas.SDLC", method, req)
		cancel()
		if err == nil {
			t.Fatalf("%s: expected a connection error, no server is listening", method)
		}
		if refusedAsUnsupported(err) {
			t.Fatalf("%s was refused before the socket, dispatch regression: %v", method, err)
		}
	}
}
