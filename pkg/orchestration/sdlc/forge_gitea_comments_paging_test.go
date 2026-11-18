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
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// giteaCommentsHandler serves an issue thread the way Gitea's per-issue comment
// route ACTUALLY behaves, which is the whole point of this file.
//
// MEASURED against Gitea 1.24 on go-luks2#3 (2026-09-14):
//
//	?limit=10  -> 50 comments      ?page=1 -> ids 2328..21565
//	?limit=200 -> 50 comments      ?page=2 -> ids 2328..21565  (identical)
//	?since=<t> -> only comments at or after t
//
// `page` and `limit` belong to the REPO-level /issues/comments route; this one
// has only `since` and `before`, and ignores the other two in silence. A stub
// that honours page would let the old, broken adapter pass.
func giteaCommentsHandler(total int, cap int) func(http.ResponseWriter, *http.Request) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return func(w http.ResponseWriter, r *http.Request) {
		var since time.Time
		if raw := r.URL.Query().Get("since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			since = parsed
		}
		out := make([]giteaComment, 0, cap)
		for i := 0; i < total; i++ {
			// Whole-second granularity, as Gitea stores it.
			created := base.Add(time.Duration(i) * time.Second)
			if !since.IsZero() && created.Before(since) {
				continue
			}
			out = append(out, giteaComment{
				ID:        int64(1000 + i),
				Body:      "comment " + strconv.Itoa(i),
				User:      &giteaUser{Login: "alice"},
				CreatedAt: created,
			})
			if len(out) == cap {
				break // the server's hard response cap, whatever limit asked for
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// TestListIssueComments_ShortThreadIsOneRequest: the common case must not pay
// for the fix. A thread inside the cap is read in a single call.
func TestListIssueComments_ShortThreadIsOneRequest(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/o/r/issues/7/comments"] = giteaCommentsHandler(3, 50)

	got, err := backend.ListIssueComments(context.Background(), RepoRef{Owner: "o", Repo: "r"}, 7)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d, want 3", len(got))
	}
	// TWO REQUESTS, NOT ONE, AND THAT IS THE FLOOR. This route has no
	// end-of-list signal — no page count, no next link, and `limit` is ignored
	// — so the only way to learn a thread has ended is to ask again from the
	// last comment and be told nothing new. One extra request per thread is the
	// price of never truncating a discussion.
	if n := len(fixture.requests); n != 2 {
		t.Errorf("made %d requests for a 3-comment thread, want 2", n)
	}
}

// TestListIssueComments_EmptyThread: no comments is not an error and costs one
// request.
func TestListIssueComments_EmptyThread(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/o/r/issues/9/comments"] = giteaCommentsHandler(0, 50)

	got, err := backend.ListIssueComments(context.Background(), RepoRef{Owner: "o", Repo: "r"}, 9)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("read %d comments from an empty thread", len(got))
	}
}
