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
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A forge that misbehaves on cue
// =============================================================================
//
// The fixture in forge_gitea_test.go answers every request the same way. These
// tests need a forge whose answer depends on how many times it has been asked
// and on what an earlier write did to it — a merge that lands while its caller
// stops waiting — so this one is scripted per request and holds state.
//
// NOTHING HERE WAITS ON A CLOCK. A stall blocks on the request's own context,
// which the client cancels when the attempt's deadline passes; a dropped
// connection is a hijack and a close.

// flakyGitea is a scripted Gitea: each route's handler sees how many times the
// route has been called.
type flakyGitea struct {
	server *httptest.Server

	mu     sync.Mutex
	routes map[string]func(w http.ResponseWriter, r *http.Request, call int)
	calls  map[string]int
}

func newFlakyGitea(t *testing.T) *flakyGitea {
	t.Helper()
	f := &flakyGitea{
		routes: map[string]func(http.ResponseWriter, *http.Request, int){},
		calls:  map[string]int{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.calls[key]++
		call := f.calls[key]
		handler, ok := f.routes[key]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		handler(w, r, call)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// on registers a route.
func (f *flakyGitea) on(key string, handler func(w http.ResponseWriter, r *http.Request, call int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[key] = handler
}

// count is how many times a route was called.
func (f *flakyGitea) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

// backend builds an adapter against the server with transport, retrying
// without waiting.
func (f *flakyGitea) backend(t *testing.T, transport ForgeTransport) (*GiteaBackend, *immediateWaiter) {
	t.Helper()
	waiter := &immediateWaiter{}
	backend, err := NewGiteaBackend(GiteaConfig{
		BaseURL:   f.server.URL + "/api/v1",
		Token:     "test-token",
		Transport: transport,
		Waiter:    waiter,
	})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	return backend, waiter
}

// stall holds the request until the client gives up on it: the attempt's
// deadline passing is the only thing that ends it.
func stall(_ http.ResponseWriter, r *http.Request) {
	awaitClientGone(r)
}

// awaitClientGone blocks until the client abandons the request.
//
// THE BODY IS DRAINED FIRST: net/http only notices a closed connection — and
// cancels the request's context — once the handler has consumed the body, so a
// handler that waits without reading a POST's body would wait for ever.
func awaitClientGone(r *http.Request) {
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return
	}
	<-r.Context().Done()
}

// dropConnection closes the connection without an answer.
func dropConnection(t *testing.T) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("the test server cannot hijack a connection")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		if err := conn.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}
}

// writeJSON answers status with payload.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("encode: %v", err)
	}
}

// shortReads is a transport whose reads give up quickly, so a stalled read
// fails fast; writes keep a generous deadline so only what a test stalls
// times out.
var shortReads = ForgeTransport{
	ReadTimeout: 250 * time.Millisecond, WriteTimeout: 30 * time.Second,
	HeavyWriteTimeout: 30 * time.Second, ReadAttempts: 3,
}

// shortWrites is the converse: writes give up quickly, reads never do.
var shortWrites = ForgeTransport{
	ReadTimeout: 30 * time.Second, WriteTimeout: 250 * time.Millisecond,
	HeavyWriteTimeout: 250 * time.Millisecond, ReadAttempts: 3,
}

const (
	issuePath   = "/api/v1/repos/jeremyhahn/go-quicbaas/issues/32"
	pullsPath   = "/api/v1/repos/jeremyhahn/go-quicbaas/pulls"
	pull41Path  = "/api/v1/repos/jeremyhahn/go-quicbaas/pulls/41"
	merge41Path = "/api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/merge"
	approved    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// =============================================================================
// Reads: retried while unanswered, final once answered
// =============================================================================

// TestGiteaRead_AnUnansweredReadIsRetried covers every shape "no answer" takes
// on the wire. The 18:12 triage listing and the 18:55 review read on the
// burn-in node were each one unanswered attempt away from succeeding.
func TestGiteaRead_AnUnansweredReadIsRetried(t *testing.T) {
	cases := map[string]func(t *testing.T) func(w http.ResponseWriter, r *http.Request){
		"stalled past the read deadline": func(*testing.T) func(http.ResponseWriter, *http.Request) {
			return stall
		},
		"connection dropped": func(t *testing.T) func(http.ResponseWriter, *http.Request) {
			drop := dropConnection(t)
			return func(w http.ResponseWriter, _ *http.Request) { drop(w) }
		},
		"502 from the gateway": gatewayStatus(http.StatusBadGateway),
		"503 from the gateway": gatewayStatus(http.StatusServiceUnavailable),
		"504 from the gateway": gatewayStatus(http.StatusGatewayTimeout),
	}
	for name, first := range cases {
		t.Run(name, func(t *testing.T) {
			forge := newFlakyGitea(t)
			failFirst := first(t)
			forge.on("GET "+issuePath, func(w http.ResponseWriter, r *http.Request, call int) {
				if call == 1 {
					failFirst(w, r)
					return
				}
				writeJSON(t, w, http.StatusOK, map[string]any{"number": 32, "title": "the issue", "state": "open"})
			})
			backend, waiter := forge.backend(t, shortReads)

			issue, err := backend.GetIssue(context.Background(), testRepo, 32)
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if issue.Number != 32 {
				t.Errorf("issue = %+v", issue)
			}
			if got := forge.count("GET " + issuePath); got != 2 {
				t.Errorf("attempts = %d, want 2", got)
			}
			if waiter.waits != 1 {
				t.Errorf("waits = %d, want one backoff between the two attempts", waiter.waits)
			}
		})
	}
}

func gatewayStatus(status int) func(*testing.T) func(http.ResponseWriter, *http.Request) {
	return func(t *testing.T) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, status, map[string]string{"message": "upstream unavailable"})
		}
	}
}

// TestGiteaRead_ExhaustionIsTypedAndAttributedToTheForge proves the bound
// holds and what it ends with: a typed error the engine reads as the forge's
// fault, not the work's.
func TestGiteaRead_ExhaustionIsTypedAndAttributedToTheForge(t *testing.T) {
	forge := newFlakyGitea(t)
	forge.on("GET "+issuePath, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusServiceUnavailable, map[string]string{"message": "down"})
	})
	backend, waiter := forge.backend(t, shortReads)

	_, err := backend.GetIssue(context.Background(), testRepo, 32)
	var exhausted *ForgeRetriesExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("error = %v, want ForgeRetriesExhaustedError", err)
	}
	if exhausted.Attempts != 3 || exhausted.Operation != "GetIssue" {
		t.Errorf("exhausted = %+v, want 3 attempts at GetIssue", exhausted)
	}
	for _, sentinel := range []error{ErrForgeRetriesExhausted, ErrForgeUnanswered, ErrBackend, orchestration.ErrForgeUnreachable} {
		if !errors.Is(err, sentinel) {
			t.Errorf("error does not unwrap to %v", sentinel)
		}
	}
	if got := forge.count("GET " + issuePath); got != 3 {
		t.Errorf("attempts = %d, want the bound of 3", got)
	}
	if waiter.waits != 2 {
		t.Errorf("waits = %d, want 2 (none after the last attempt)", waiter.waits)
	}
}

// TestGiteaRead_AnAnswerIsFinal proves only silence is retried. A status the
// forge itself chose is what it did, and asking again changes nothing.
func TestGiteaRead_AnAnswerIsFinal(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			forge := newFlakyGitea(t)
			forge.on("GET "+issuePath, func(w http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, w, status, map[string]string{"message": "no"})
			})
			backend, _ := forge.backend(t, shortReads)

			_, err := backend.GetIssue(context.Background(), testRepo, 32)
			if err == nil {
				t.Fatal("expected an error")
			}
			if errors.Is(err, ErrForgeUnanswered) || errors.Is(err, orchestration.ErrForgeUnreachable) {
				t.Errorf("an answer was classified as silence: %v", err)
			}
			if got := forge.count("GET " + issuePath); got != 1 {
				t.Errorf("attempts = %d, want 1", got)
			}
		})
	}
}

// TestGiteaRead_TheCallersCancellationIsNotTheForge proves a job being
// cancelled is neither retried nor blamed on the forge.
func TestGiteaRead_TheCallersCancellationIsNotTheForge(t *testing.T) {
	forge := newFlakyGitea(t)
	ctx, cancel := context.WithCancel(context.Background())
	forge.on("GET "+issuePath, func(_ http.ResponseWriter, r *http.Request, _ int) {
		cancel()
		awaitClientGone(r)
	})
	backend, _ := forge.backend(t, shortWrites) // a read deadline far past the cancel

	_, err := backend.GetIssue(ctx, testRepo, 32)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrForgeUnanswered) || errors.Is(err, orchestration.ErrForgeUnreachable) {
		t.Errorf("a cancelled caller was attributed to the forge: %v", err)
	}
	if got := forge.count("GET " + issuePath); got != 1 {
		t.Errorf("attempts = %d, want 1: a cancelled caller is not retried", got)
	}
}

// =============================================================================
// Call policy: deadlines and recovery per operation
// =============================================================================

// TestGiteaCallPolicy_SplitsDeadlinesByWhatTheCallCosts pins the policy each
// kind of call gets.
func TestGiteaCallPolicy_SplitsDeadlinesByWhatTheCallCosts(t *testing.T) {
	transport := ForgeTransport{
		ReadTimeout: time.Second, WriteTimeout: 2 * time.Second, HeavyWriteTimeout: 3 * time.Second,
		ReadAttempts: 4, RetryBackoff: time.Millisecond,
	}
	backend, err := NewGiteaBackend(GiteaConfig{BaseURL: "http://gitea.test/api/v1", Token: "t", Transport: transport})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	cases := map[string]struct {
		req  giteaRequest
		want giteaCallPolicy
	}{
		"a read retries under the read deadline": {
			giteaRequest{method: http.MethodGet, operation: "ListIssues"},
			giteaCallPolicy{deadline: time.Second, attempts: 4, recovery: recoverReplay},
		},
		"a merge is heavy and reconciled": {
			giteaRequest{method: http.MethodPost, operation: "MergePullRequest"},
			giteaCallPolicy{deadline: 3 * time.Second, attempts: 1, recovery: recoverReconcile},
		},
		"opening a pull request is heavy and reconciled": {
			giteaRequest{method: http.MethodPost, operation: "CreatePullRequest"},
			giteaCallPolicy{deadline: 3 * time.Second, attempts: 1, recovery: recoverReconcile},
		},
		"an idempotent verified write is replayed": {
			giteaRequest{method: http.MethodPut, operation: "SetIssueLabels"},
			giteaCallPolicy{deadline: 2 * time.Second, attempts: 4, recovery: recoverReplay},
		},
		"a comment is sent once": {
			giteaRequest{method: http.MethodPost, operation: "CommentOnIssue"},
			giteaCallPolicy{deadline: 2 * time.Second, attempts: 1, recovery: recoverUnconfirmed},
		},
		"an unclassified write fails safe": {
			giteaRequest{method: http.MethodPost, operation: "SomethingNew"},
			giteaCallPolicy{deadline: 2 * time.Second, attempts: 1, recovery: recoverUnconfirmed},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := backend.callPolicy(tc.req); got != tc.want {
				t.Errorf("policy = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestGiteaWritePolicies_EveryWriteIsClassified is the drift gate over the
// write table: every request literal in the adapter that is not a read names
// an operation the table classifies. An unclassified write still fails safe,
// but it fails as an error where a replay or a reconciliation would have
// succeeded, and nobody decided that.
func TestGiteaWritePolicies_EveryWriteIsClassified(t *testing.T) {
	files, err := filepath.Glob("forge_gitea*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	writes := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if ident, ok := lit.Type.(*ast.Ident); !ok || ident.Name != "giteaRequest" {
				return true
			}
			method, operation := requestLiteralFields(lit)
			if method == "" || giteaReadMethods[method] {
				return true
			}
			writes++
			if operation == "" {
				t.Errorf("%s: a %s request names no literal operation", fset.Position(lit.Pos()), method)
				return true
			}
			if _, ok := giteaWritePolicies[operation]; !ok {
				t.Errorf("%s: write %q is not in giteaWritePolicies", fset.Position(lit.Pos()), operation)
			}
			return true
		})
	}
	if writes == 0 {
		t.Fatal("no writes were found; the gate would pass vacuously")
	}
}

// requestLiteralFields reads the method (as net/http's constant value) and the
// literal operation from a giteaRequest composite literal.
func requestLiteralFields(lit *ast.CompositeLit) (method, operation string) {
	methods := map[string]string{
		"MethodGet": http.MethodGet, "MethodHead": http.MethodHead, "MethodPost": http.MethodPost,
		"MethodPut": http.MethodPut, "MethodPatch": http.MethodPatch, "MethodDelete": http.MethodDelete,
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "method":
			if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
				method = methods[sel.Sel.Name]
			}
		case "operation":
			if basic, ok := kv.Value.(*ast.BasicLit); ok {
				operation = strings.Trim(basic.Value, `"`)
			}
		}
	}
	return method, operation
}

// TestGiteaWrite_AWriteThatIsNotSafeToRepeatIsSentOnce proves the refusal to
// replay: a comment that went unanswered may have posted, and a second post
// is a second comment.
func TestGiteaWrite_AWriteThatIsNotSafeToRepeatIsSentOnce(t *testing.T) {
	forge := newFlakyGitea(t)
	drop := dropConnection(t)
	key := "POST " + issuePath + "/comments"
	forge.on(key, func(w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
	backend, _ := forge.backend(t, shortWrites)

	_, err := backend.CommentOnIssue(context.Background(), testRepo, 32, "hello")
	var unconfirmed *ForgeWriteUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("error = %v, want ForgeWriteUnconfirmedError", err)
	}
	if !errors.Is(err, orchestration.ErrForgeUnreachable) {
		t.Error("an unconfirmed write is not attributed to the forge")
	}
	if got := forge.count(key); got != 1 {
		t.Errorf("posts = %d, want exactly 1", got)
	}
}

// TestGiteaWrite_AnIdempotentVerifiedWriteIsReplayed proves the other half: a
// label replacement cannot double, and its read-back proves where it landed.
func TestGiteaWrite_AnIdempotentVerifiedWriteIsReplayed(t *testing.T) {
	forge := newFlakyGitea(t)
	drop := dropConnection(t)
	forge.on("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusOK, []map[string]any{{"id": 7, "name": "sdlc:in-progress"}})
	})
	put := "PUT " + issuePath + "/labels"
	forge.on(put, func(w http.ResponseWriter, _ *http.Request, call int) {
		if call == 1 {
			drop(w)
			return
		}
		writeJSON(t, w, http.StatusOK, nil)
	})
	forge.on("GET "+issuePath, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"number": 32, "state": "open", "labels": []map[string]any{{"id": 7, "name": "sdlc:in-progress"}},
		})
	})
	backend, _ := forge.backend(t, shortWrites)

	if err := backend.SetIssueLabels(context.Background(), testRepo, 32, []string{"sdlc:in-progress"}); err != nil {
		t.Fatalf("SetIssueLabels: %v", err)
	}
	if got := forge.count(put); got != 2 {
		t.Errorf("writes = %d, want the unanswered one and its replay", got)
	}
}

// =============================================================================
// Merge: reconciled by reading the pull request back
// =============================================================================

// mergeForge is a pull request #41 whose merge endpoint runs script and whose
// state the script can change.
type mergeForge struct {
	*flakyGitea
	mu        sync.Mutex
	merged    bool
	mergeable bool
	state     string
}

func newMergeForge(t *testing.T, script func(m *mergeForge, w http.ResponseWriter, r *http.Request, call int)) *mergeForge {
	m := &mergeForge{flakyGitea: newFlakyGitea(t), mergeable: true, state: "open"}
	m.on("GET "+pull41Path, func(w http.ResponseWriter, _ *http.Request, _ int) {
		m.mu.Lock()
		payload := pullPayload(41, "issue/32-slug", "main", approved, m.state, m.merged)
		payload["mergeable"] = m.mergeable
		if m.merged {
			payload["merge_commit_sha"] = "cccccccccccccccccccccccccccccccccccccccc"
		}
		m.mu.Unlock()
		writeJSON(t, w, http.StatusOK, payload)
	})
	m.on("POST "+merge41Path, func(w http.ResponseWriter, r *http.Request, call int) {
		script(m, w, r, call)
	})
	return m
}

// land marks the pull request merged, as Gitea does when a squash finishes.
func (m *mergeForge) land() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.merged, m.state = true, "closed"
}

// TestGiteaMerge_UnansweredButLandedIsSuccess is go-quicbaas#971 exactly: the
// squash outlasted the client's deadline, Gitea merged anyway, and the job was
// recorded failed on a change that was on main.
func TestGiteaMerge_UnansweredButLandedIsSuccess(t *testing.T) {
	forge := newMergeForge(t, func(m *mergeForge, _ http.ResponseWriter, r *http.Request, _ int) {
		m.land()
		awaitClientGone(r) // the answer never arrives
	})
	backend, _ := forge.backend(t, shortWrites)

	err := backend.MergePullRequest(context.Background(), testRepo, 41, MergeSpec{Method: "squash"}, approved)
	if err != nil {
		t.Fatalf("a merge the forge completed was reported as %v", err)
	}
	if got := forge.count("POST " + merge41Path); got != 1 {
		t.Errorf("merge requests = %d, want 1: a merge is never re-sent on a timeout alone", got)
	}
}

// TestGiteaMerge_UnansweredAndStillOpenIsNotReady proves the handover to the
// bounded merge retry: open and mergeable after an unanswered merge is "not
// yet", and it still carries the forge's fault.
func TestGiteaMerge_UnansweredAndStillOpenIsNotReady(t *testing.T) {
	drop := dropConnection(t)
	forge := newMergeForge(t, func(_ *mergeForge, w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
	backend, _ := forge.backend(t, shortWrites)

	err := backend.MergePullRequest(context.Background(), testRepo, 41, MergeSpec{Method: "squash"}, approved)
	if !errors.Is(err, ErrForgeNotReady) {
		t.Fatalf("error = %v, want ErrForgeNotReady so the merge retry decides", err)
	}
	if !errors.Is(err, orchestration.ErrForgeUnreachable) {
		t.Error("the not-ready answer lost the unanswered merge behind it")
	}
}

// TestGiteaMerge_UnansweredAndUnmergeableIsUnconfirmed proves the adapter does
// not guess: a read that shows neither a merge nor a mergeable pull request
// proves nothing about what the unanswered request did.
func TestGiteaMerge_UnansweredAndUnmergeableIsUnconfirmed(t *testing.T) {
	drop := dropConnection(t)
	forge := newMergeForge(t, func(_ *mergeForge, w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
	forge.mergeable = false
	backend, _ := forge.backend(t, shortWrites)

	err := backend.MergePullRequest(context.Background(), testRepo, 41, MergeSpec{Method: "squash"}, approved)
	var unconfirmed *ForgeWriteUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("error = %v, want ForgeWriteUnconfirmedError", err)
	}
	if !strings.Contains(unconfirmed.Observed, "mergeable false") {
		t.Errorf("observed = %q, want what the read-back found", unconfirmed.Observed)
	}
}

// TestGiteaMerge_AnAlreadyMergedPullRequestIsNotMergedAgain proves the retry
// converges: once the forge has finished, the next attempt stops at the read.
func TestGiteaMerge_AnAlreadyMergedPullRequestIsNotMergedAgain(t *testing.T) {
	forge := newMergeForge(t, func(_ *mergeForge, w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusMethodNotAllowed, map[string]string{"message": "already merged"})
	})
	forge.land()
	backend, _ := forge.backend(t, shortWrites)

	if err := backend.MergePullRequest(context.Background(), testRepo, 41, MergeSpec{Method: "squash"}, approved); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if got := forge.count("POST " + merge41Path); got != 0 {
		t.Errorf("merge requests = %d, want none for a merged pull request", got)
	}
}

// TestHooksMerge_AnUnansweredMergeThatDidNotLandIsRetried walks the whole
// retry path through the hook: the first merge is dropped unapplied, the read
// shows the pull request open, the hook's bounded retry sends the merge again,
// and that one lands.
func TestHooksMerge_AnUnansweredMergeThatDidNotLandIsRetried(t *testing.T) {
	drop := dropConnection(t)
	forge := newMergeForge(t, func(m *mergeForge, w http.ResponseWriter, _ *http.Request, call int) {
		if call == 1 {
			drop(w)
			return
		}
		m.land()
		writeJSON(t, w, http.StatusOK, nil)
	})
	backend, _ := forge.backend(t, shortWrites)
	hooks := &Hooks{attempts: 3, waiter: &immediateWaiter{}}

	if err := hooks.merge(context.Background(), backend, testRepo, 41, MergeSpec{Method: "squash"}, approved); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := forge.count("POST " + merge41Path); got != 2 {
		t.Errorf("merge requests = %d, want the dropped one and the retry", got)
	}
}

// TestHooksMerge_ExhaustionOnUnansweredMergesIsTheForgesFault proves the bound
// keeps the attribution: a merge the forge never answered, every time, is not a
// failure of the work.
func TestHooksMerge_ExhaustionOnUnansweredMergesIsTheForgesFault(t *testing.T) {
	drop := dropConnection(t)
	forge := newMergeForge(t, func(_ *mergeForge, w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
	backend, _ := forge.backend(t, shortWrites)
	hooks := &Hooks{attempts: 2, waiter: &immediateWaiter{}}

	err := hooks.merge(context.Background(), backend, testRepo, 41, MergeSpec{Method: "squash"}, approved)
	if !errors.Is(err, ErrMergeNotReadyExhausted) {
		t.Fatalf("error = %v, want ErrMergeNotReadyExhausted", err)
	}
	if !errors.Is(err, orchestration.ErrForgeUnreachable) {
		t.Error("the exhausted merge retry lost the forge's fault")
	}
}

// =============================================================================
// Create pull request: adopt, never duplicate
// =============================================================================

// createForge holds the open pull requests for the create tests.
type createForge struct {
	*flakyGitea
	mu   sync.Mutex
	open []map[string]any
}

func newCreateForge(t *testing.T, create func(c *createForge, w http.ResponseWriter, r *http.Request, call int)) *createForge {
	c := &createForge{flakyGitea: newFlakyGitea(t)}
	c.on("GET "+pullsPath, func(w http.ResponseWriter, _ *http.Request, _ int) {
		c.mu.Lock()
		listing := append([]map[string]any{}, c.open...)
		c.mu.Unlock()
		writeJSON(t, w, http.StatusOK, listing)
	})
	c.on("POST "+pullsPath, func(w http.ResponseWriter, r *http.Request, call int) {
		create(c, w, r, call)
	})
	return c
}

// opened records pull request number as open for head -> main, and serves it.
func (c *createForge) opened(t *testing.T, number int, head string) map[string]any {
	payload := pullPayload(number, head, "main", "abc123", "open", false)
	c.mu.Lock()
	c.open = append(c.open, payload)
	c.mu.Unlock()
	c.on("GET "+pullsPath+"/"+strconv.Itoa(number), func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusOK, payload)
	})
	return payload
}

const frostHead = "issue/19-ci-yml-overwrites-the-baked-golangci-lint"

var frostSpec = PullRequestSpec{Title: "ci: stop overwriting golangci-lint", HeadRef: frostHead, BaseRef: "main"}

// TestCreatePullRequest_AdoptsThePullRequestAnUnansweredCreateOpened is
// go-frost#26 exactly: the create outlasted its deadline, Gitea opened the pull
// request anyway, and the factory never recorded it. The adapter must find it
// and hand it back rather than fail — or create a second.
func TestCreatePullRequest_AdoptsThePullRequestAnUnansweredCreateOpened(t *testing.T) {
	forge := newCreateForge(t, func(c *createForge, _ http.ResponseWriter, r *http.Request, _ int) {
		c.opened(t, 26, frostHead)
		awaitClientGone(r) // Gitea opened it; the answer never arrives
	})
	backend, _ := forge.backend(t, shortWrites)

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pull.Number != 26 || pull.HeadRef != frostHead {
		t.Errorf("pull = %+v, want the adopted #26", pull)
	}
	if got := forge.count("POST " + pullsPath); got != 1 {
		t.Errorf("creates = %d, want 1: the pull request was adopted, not re-created", got)
	}
}

// forkPull is an open pull request from a FORK's branch that shares this
// task's head and base names.
func forkPull(number int, head string) map[string]any {
	payload := pullPayload(number, head, "main", "f0f0f0", "open", false)
	payload["head"] = map[string]any{"ref": head, "sha": "f0f0f0", "repo": map[string]any{
		"name": testRepo.Repo, "owner": map[string]any{"login": "someone-else"},
	}}
	return payload
}

// TestCreatePullRequest_AForkWithTheSameBranchNameIsNeverAdopted covers the
// initial lookup: a fork's pull request from an identically named branch is a
// different change, and the create goes ahead for this repository's own branch.
func TestCreatePullRequest_AForkWithTheSameBranchNameIsNeverAdopted(t *testing.T) {
	forge := newCreateForge(t, func(c *createForge, w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusCreated, c.opened(t, 27, frostHead))
	})
	forge.mu.Lock()
	forge.open = append(forge.open, forkPull(26, frostHead))
	forge.mu.Unlock()
	backend, _ := forge.backend(t, shortWrites)

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pull.Number != 27 {
		t.Errorf("pull = #%d, want this repository's own #27, not the fork's #26", pull.Number)
	}
	if got := forge.count("POST " + pullsPath); got != 1 {
		t.Errorf("creates = %d, want 1", got)
	}
}

// TestCreatePullRequest_ReconciliationNeverAdoptsAFork covers the lookups after
// an unanswered create and after a 409: with only a fork's same-named pull
// request open, neither adopts it.
func TestCreatePullRequest_ReconciliationNeverAdoptsAFork(t *testing.T) {
	t.Run("after an unanswered create", func(t *testing.T) {
		drop := dropConnection(t)
		forge := newCreateForge(t, func(_ *createForge, w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
		forge.open = append(forge.open, forkPull(26, frostHead))
		backend, _ := forge.backend(t, shortWrites)

		_, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
		if !errors.Is(err, ErrForgeWriteUnconfirmed) {
			t.Fatalf("error = %v, want ErrForgeWriteUnconfirmed rather than the fork adopted", err)
		}
	})
	t.Run("after a duplicate refusal", func(t *testing.T) {
		forge := newCreateForge(t, func(_ *createForge, w http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, w, http.StatusConflict, map[string]string{"message": "pull request already exists for these targets"})
		})
		forge.open = append(forge.open, forkPull(26, frostHead))
		backend, _ := forge.backend(t, shortWrites)

		_, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
		if giteaStatusOf(err) != http.StatusConflict {
			t.Fatalf("error = %v, want the forge's 409 rather than the fork adopted", err)
		}
	})
}

// TestGiteaPull_CarriesItsHeadRepository proves the decode: the source
// repository is what adoption keys on, and a deleted fork reports none.
func TestGiteaPull_CarriesItsHeadRepository(t *testing.T) {
	raw := `{"number":1,"head":{"ref":"b","repo":{"name":"api","owner":{"login":"acme"}}}}`
	var pull giteaPull
	if err := json.Unmarshal([]byte(raw), &pull); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := pull.toForgePullRequest().HeadRepo; got != (RepoRef{Owner: "acme", Repo: "api"}) {
		t.Errorf("HeadRepo = %+v", got)
	}
	var orphan giteaPull
	if err := json.Unmarshal([]byte(`{"number":2,"head":{"ref":"b","repo":null}}`), &orphan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := orphan.toForgePullRequest().HeadRepo; got != (RepoRef{}) {
		t.Errorf("a deleted fork's HeadRepo = %+v, want zero", got)
	}
	if sameRepo(RepoRef{}, RepoRef{}) {
		t.Error("an unknown head repository matched")
	}
	if !sameRepo(RepoRef{Owner: "Acme", Repo: "API"}, RepoRef{Owner: "acme", Repo: "api"}) {
		t.Error("repository names are case-insensitive on Gitea")
	}
}

// TestCreatePullRequest_TheRetryAdoptsWhatTheLastAttemptOpened is the other
// half of go-frost#26: the NEXT attempt, finding the pull request open, adopts
// it without sending a create Gitea would refuse as a duplicate.
func TestCreatePullRequest_TheRetryAdoptsWhatTheLastAttemptOpened(t *testing.T) {
	forge := newCreateForge(t, func(_ *createForge, w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusConflict, map[string]string{"message": "pull request already exists for these targets"})
	})
	forge.opened(t, 26, frostHead)
	backend, _ := forge.backend(t, shortWrites)

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pull.Number != 26 {
		t.Errorf("pull = %+v, want #26", pull)
	}
	if got := forge.count("POST " + pullsPath); got != 0 {
		t.Errorf("creates = %d, want none for refs that already have an open pull request", got)
	}
}

// TestCreatePullRequest_ADuplicateRefusalIsReconciledByAdoption covers the race
// the pre-create lookup cannot close: the pull request appears between the
// lookup and the create, and Gitea's 409 is the proof it exists.
func TestCreatePullRequest_ADuplicateRefusalIsReconciledByAdoption(t *testing.T) {
	forge := newCreateForge(t, func(c *createForge, w http.ResponseWriter, _ *http.Request, _ int) {
		c.opened(t, 26, frostHead)
		writeJSON(t, w, http.StatusConflict, map[string]string{"message": "pull request already exists for these targets"})
	})
	backend, _ := forge.backend(t, shortWrites)

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pull.Number != 26 {
		t.Errorf("pull = %+v, want #26", pull)
	}
}

// TestCreatePullRequest_AConflictWithNothingOpenIsTheForgesAnswer proves a 409
// is only adopted when it IS the duplicate: with nothing open for the refs, the
// refusal stands.
func TestCreatePullRequest_AConflictWithNothingOpenIsTheForgesAnswer(t *testing.T) {
	forge := newCreateForge(t, func(_ *createForge, w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, w, http.StatusConflict, map[string]string{"message": "something else"})
	})
	backend, _ := forge.backend(t, shortWrites)

	_, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if giteaStatusOf(err) != http.StatusConflict {
		t.Fatalf("error = %v, want the forge's 409", err)
	}
}

// TestCreatePullRequest_AnUnansweredCreateThatDidNotLandIsSentAgain proves a
// create is repeated only when a read has just shown it did not land.
func TestCreatePullRequest_AnUnansweredCreateThatDidNotLandIsSentAgain(t *testing.T) {
	drop := dropConnection(t)
	forge := newCreateForge(t, func(c *createForge, w http.ResponseWriter, _ *http.Request, call int) {
		if call == 1 {
			drop(w)
			return
		}
		writeJSON(t, w, http.StatusCreated, c.opened(t, 27, frostHead))
	})
	backend, _ := forge.backend(t, shortWrites)

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pull.Number != 27 {
		t.Errorf("pull = %+v, want #27", pull)
	}
	if got := forge.count("POST " + pullsPath); got != 2 {
		t.Errorf("creates = %d, want the dropped one and one more", got)
	}
}

// TestCreatePullRequest_NeverAnsweredIsUnconfirmedAndBounded proves the bound
// and the typed end of it.
func TestCreatePullRequest_NeverAnsweredIsUnconfirmedAndBounded(t *testing.T) {
	drop := dropConnection(t)
	forge := newCreateForge(t, func(_ *createForge, w http.ResponseWriter, _ *http.Request, _ int) { drop(w) })
	backend, _ := forge.backend(t, shortWrites)

	_, err := backend.CreatePullRequest(context.Background(), testRepo, frostSpec)
	var unconfirmed *ForgeWriteUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("error = %v, want ForgeWriteUnconfirmedError", err)
	}
	if !errors.Is(err, orchestration.ErrForgeUnreachable) {
		t.Error("an unconfirmed create is not attributed to the forge")
	}
	if got := forge.count("POST " + pullsPath); got != shortWrites.ReadAttempts {
		t.Errorf("creates = %d, want the bound of %d", got, shortWrites.ReadAttempts)
	}
}

// =============================================================================
// Transport policy
// =============================================================================

// TestForgeTransport_ZeroTakesTheDefaults proves an unset policy is the
// engine's, field by field.
func TestForgeTransport_ZeroTakesTheDefaults(t *testing.T) {
	if got := (ForgeTransport{}).Resolved(); got != DefaultForgeTransport {
		t.Errorf("Resolved() = %+v, want %+v", got, DefaultForgeTransport)
	}
	set := ForgeTransport{ReadTimeout: time.Second}
	if got := set.Resolved(); got.ReadTimeout != time.Second || got.WriteTimeout != DefaultForgeTransport.WriteTimeout {
		t.Errorf("Resolved() = %+v; a set field must survive and an unset one take the default", got)
	}
}

// TestForgeTransport_NegativeIsRefused proves each field's refusal names it,
// and that the adapter refuses at construction.
func TestForgeTransport_NegativeIsRefused(t *testing.T) {
	cases := map[string]ForgeTransport{
		"forge.read_timeout":        {ReadTimeout: -time.Second},
		"forge.write_timeout":       {WriteTimeout: -time.Second},
		"forge.heavy_write_timeout": {HeavyWriteTimeout: -time.Second},
		"forge.read_attempts":       {ReadAttempts: -1},
		"forge.retry_backoff":       {RetryBackoff: -time.Second},
	}
	for field, transport := range cases {
		t.Run(field, func(t *testing.T) {
			err := transport.Validate()
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) || invalid.Field != field {
				t.Fatalf("Validate() = %v, want InvalidInputError on %s", err, field)
			}
			if _, err := NewGiteaBackend(GiteaConfig{BaseURL: "http://gitea.test/api/v1", Token: "t", Transport: transport}); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("NewGiteaBackend accepted %+v (err = %v)", transport, err)
			}
		})
	}
	if err := DefaultForgeTransport.Validate(); err != nil {
		t.Errorf("the default policy does not validate: %v", err)
	}
}
