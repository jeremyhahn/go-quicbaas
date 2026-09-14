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

package sdlc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestReadBodyTail_KeepsTheEndWithinTheBound.
//
// The two buffers are the part worth testing: a body that lands exactly on the
// bound, one that straddles both buffers, and one that fills the newer buffer
// several times over all have to produce the same suffix.
func TestReadBodyTail_KeepsTheEndWithinTheBound(t *testing.T) {
	body := strings.Repeat("abcdefghij", 100) // 1000 bytes
	for _, max := range []int{1, 7, 999, 1000, 1001, 4096} {
		got, err := readBodyTail(strings.NewReader(body), max)
		if err != nil {
			t.Fatalf("max %d: %v", max, err)
		}
		want := body
		if max < len(body) {
			want = body[len(body)-max:]
		}
		if string(got) != want {
			t.Fatalf("max %d: tail = %q, want %q", max, got, want)
		}
	}
}

// TestReadBodyTail_UnboundedReadsTheWholeBody is the contract every existing
// caller depends on: zero means unchanged.
func TestReadBodyTail_UnboundedReadsTheWholeBody(t *testing.T) {
	body := strings.Repeat("x", 5000)
	got, err := readBodyTail(strings.NewReader(body), 0)
	if err != nil {
		t.Fatalf("readBodyTail: %v", err)
	}
	if string(got) != body {
		t.Fatalf("read %d bytes, want the whole %d", len(got), len(body))
	}
}

// failingReader yields some bytes and then an error, the way a connection
// dropped mid-log does.
type failingReader struct {
	head string
	sent bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("connection reset")
	}
	r.sent = true
	return copy(p, r.head), nil
}

// TestReadBodyTail_ReportsAReadFailure: a truncated log must not be returned
// as the log. Half a CI failure quoted as the whole one is worse than none.
func TestReadBodyTail_ReportsAReadFailure(t *testing.T) {
	if _, err := readBodyTail(&failingReader{head: "partial"}, 64); err == nil {
		t.Fatal("a broken read was reported as a complete body")
	}
}

// serveJobLog wires a run's job listing and that job's log the way Gitea does,
// so a test exercises the ownership check rather than skipping past it.
func serveJobLog(fixture *giteaFixture, runID, jobID, headSHA, log string) {
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/actions/runs/"+runID+"/jobs"] =
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jobs":[{"id":`+jobID+`,"run_id":`+runID+
				`,"head_sha":"`+headSHA+`"}]}`)
		}
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/actions/jobs/"+jobID+"/logs"] =
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, log)
		}
}

// TestGiteaCheckRunLogs_ReadsTheJobNamedByTheStatusURL.
//
// The status id addresses no log; the Actions job is named only inside the
// target URL, in the shape Gitea itself writes — root-relative, and naming the
// repository the run belongs to.
func TestGiteaCheckRunLogs_ReadsTheJobNamedByTheStatusURL(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	log := strings.Repeat("noise\n", 500) + "make: *** [lint] Error 1\n"
	serveJobLog(fixture, "8133", "32352", "abc123", log)

	run := ForgeCheckRun{
		ID:      "24",
		Name:    "CI / Lint (pull_request)",
		HeadSHA: "abc123",
		URL:     "/jeremyhahn/go-quicbaas/actions/runs/8133/jobs/32352",
	}
	got, err := backend.CheckRunLogs(context.Background(), testRepo, run, 64)
	if err != nil {
		t.Fatalf("CheckRunLogs: %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("read %d bytes, want the 64 asked for", len(got))
	}
	if !strings.HasSuffix(got, "make: *** [lint] Error 1\n") {
		t.Fatalf("excerpt = %q, want the end of the log", got)
	}
}

// TestGiteaCheckRunLogs_RefusesAStatusURLThisForgeDidNotWrite is the confused
// deputy this route would otherwise be (hosted review of #288).
//
// A commit status is publishable by anything holding status-write, so its
// target URL is third-party input — and the job id inside it decides which log
// a CREDENTIALED client fetches from the configured repository. Searching for
// "/actions/runs/N/jobs/M" anywhere in the string let a publisher point at any
// host, name any job, and have that job's log quoted into a worker's prompt as
// this check's failure.
//
// Every case below is refused with no request made at all, which the empty
// request log proves: a refusal that still called the forge would have chosen
// the resource before deciding it was allowed to.
func TestGiteaCheckRunLogs_RefusesAStatusURLThisForgeDidNotWrite(t *testing.T) {
	for _, tc := range []struct{ name, url string }{
		{"a foreign host carrying the expected path", "https://attacker.invalid/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"a foreign host with only the tail", "https://attacker.invalid/actions/runs/1/jobs/32352"},
		{"another repository on this forge", "/jeremyhahn/go-truststrap/actions/runs/8133/jobs/32352"},
		{"another owner on this forge", "/someone-else/go-quicbaas/actions/runs/8133/jobs/32352"},
		{"the path buried under a prefix", "/anything/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"the path with a suffix appended", "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352/extra"},
		{"traversal that resolves elsewhere", "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/../../../../other/repo/actions/runs/1/jobs/32352"},
		{"a query string carrying the shape", "/jeremyhahn/go-quicbaas?x=/actions/runs/1/jobs/32352"},
		{"a fragment carrying the shape", "/jeremyhahn/go-quicbaas#/actions/runs/1/jobs/32352"},
		{"userinfo smuggling the host", "https://forge.internal:3001@attacker.invalid/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"a scheme mismatch", "https://127.0.0.1:1/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"a non-numeric job", "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352abc"},
		{"a non-numeric run", "/jeremyhahn/go-quicbaas/actions/runs/latest/jobs/32352"},
		{"no job at all", "/jeremyhahn/go-quicbaas/actions/runs/1"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			_, err := backend.CheckRunLogs(context.Background(), testRepo,
				ForgeCheckRun{Name: "CI / Lint", URL: tc.url}, 64)

			var unsupported *ForgeUnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if len(fixture.requests) != 0 {
				t.Fatalf("the adapter called the forge before refusing: %+v", fixture.requests)
			}
		})
	}
}

// TestGiteaJobFromStatusURL_AcceptsWhatGiteaWrites: the strictness must not
// refuse the real thing. Both spellings Gitea produces are accepted, and a
// forge served from a subpath carries that subpath in the field.
func TestGiteaJobFromStatusURL_AcceptsWhatGiteaWrites(t *testing.T) {
	for _, tc := range []struct{ name, base, url string }{
		{"root-relative, as observed live", "http://forge.internal:3001/api/v1", "/jeremyhahn/go-quicbaas/actions/runs/8133/jobs/32352"},
		{"absolute on the same origin", "http://forge.internal:3001/api/v1", "http://forge.internal:3001/jeremyhahn/go-quicbaas/actions/runs/8133/jobs/32352"},
		{"a forge served from a subpath", "https://git.example.invalid/gitea/api/v1", "/gitea/jeremyhahn/go-quicbaas/actions/runs/8133/jobs/32352"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, job, ok := giteaJobFromStatusURL(tc.base, testRepo, tc.url)
			if !ok || run != "8133" || job != "32352" {
				t.Fatalf("run = %q job = %q ok = %v, want 8133/32352", run, job, ok)
			}
		})
	}
}

// TestGiteaCheckRunLogs_RefusesAStatusThatNamesNoJob is the CI-agnostic half of
// the design: ListCheckRuns reads commit statuses so any CI is visible, and a
// status published by something that is not Gitea Actions has no log to read.
// The refusal is what lets the caller quote the contexts alone rather than
// report an empty log as the finding.
func TestGiteaCheckRunLogs_RefusesAStatusThatNamesNoJob(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	run := ForgeCheckRun{ID: "24", Name: "external/ci", URL: "https://ci.example.invalid/builds/7"}

	_, err := backend.CheckRunLogs(context.Background(), testRepo, run, 64)
	var unsupported *ForgeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want ForgeUnsupportedError", err)
	}
}

// TestGiteaCheckRunLogs_RefusesAnUnboundedRead: the excerpt exists to be
// quoted into a prompt, and a caller that named no bound has not decided how
// much of someone else's log it is willing to carry.
func TestGiteaCheckRunLogs_RefusesAnUnboundedRead(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	run := ForgeCheckRun{HeadSHA: "abc123", URL: "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/2"}
	if _, err := backend.CheckRunLogs(context.Background(), testRepo, run, 0); err == nil {
		t.Fatal("read a log with no bound")
	}
}

// TestGiteaCheckRunLogs_ScrubsTheAdapterCredential.
//
// A log excerpt travels further than any other body this adapter returns: into
// a review record, and from there into a worker prompt. A CI job that echoed
// the token — a `set -x` around a curl, a runner debug flag — would otherwise
// publish it to everything downstream.
func TestGiteaCheckRunLogs_ScrubsTheAdapterCredential(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	serveJobLog(fixture, "1", "9", "abc123",
		"curl -H 'Authorization: token test-token' failed\n")

	got, err := backend.CheckRunLogs(context.Background(), testRepo,
		ForgeCheckRun{HeadSHA: "abc123", URL: "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/9"}, 4096)
	if err != nil {
		t.Fatalf("CheckRunLogs: %v", err)
	}
	if strings.Contains(got, "test-token") {
		t.Fatalf("the excerpt carries the adapter's token:\n%s", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("excerpt = %q, want the credential redacted in place", got)
	}
}

// TestGiteaCheckRunLogs_ScrubsACredentialStraddlingTheBound.
//
// The scrub matches whole credentials. A token cut in half by the tail bound
// would lose its head to the read and its remainder would survive the scrub
// unrecognised — a partial secret in a body that goes on to a review record
// and a worker prompt. Mutation evidence: removing credentialWidth from the
// read bound leaves "token" in the excerpt.
func TestGiteaCheckRunLogs_ScrubsACredentialStraddlingTheBound(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// The fixture's token is "test-token" (10 bytes). A tail of 8 cuts inside
	// it, leaving "token!!!" behind.
	serveJobLog(fixture, "1", "9", "abc123", strings.Repeat("A", 100)+"test-token!!!")

	got, err := backend.CheckRunLogs(context.Background(), testRepo,
		ForgeCheckRun{HeadSHA: "abc123", URL: "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/9"}, 8)
	if err != nil {
		t.Fatalf("CheckRunLogs: %v", err)
	}
	if len(got) > 8 {
		t.Fatalf("excerpt is %d bytes, want the caller's bound of 8: %q", len(got), got)
	}
	if strings.Contains(got, "token") {
		t.Fatalf("a credential cut by the bound survived the scrub: %q", got)
	}
}

// TestGiteaCheckRunLogs_RefusesAJobBelongingToAnotherHead is the second layer
// the hosted review asked for.
//
// Parsing proves the status pointed at THIS repository and no more: a publisher
// may still name a real job from some other run on it, whose log would then be
// quoted into a worker's prompt as this check's failure. The run's own job
// listing is what settles it — the excerpt claims to be this commit's build
// output, so the forge is asked to confirm exactly that.
func TestGiteaCheckRunLogs_RefusesAJobBelongingToAnotherHead(t *testing.T) {
	for _, tc := range []struct {
		name          string
		listedJob     string
		listedRun     string
		listedHead    string
		checkHead     string
		wantLogServed bool
	}{
		{"the job covers a different commit", "9", "1", "deadbeef", "abc123", false},
		{"the job belongs to a different run", "9", "77", "abc123", "abc123", false},
		{"the run does not list that job at all", "8", "1", "abc123", "abc123", false},
		{"the job is this commit's", "9", "1", "abc123", "abc123", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			serveJobLog(fixture, "1", tc.listedJob, tc.listedHead, "secret build output")
			// The listing is keyed on the run the URL names; re-register it
			// with the run id the fixture should report for the job.
			fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/actions/runs/1/jobs"] =
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"jobs":[{"id":`+tc.listedJob+`,"run_id":`+
						tc.listedRun+`,"head_sha":"`+tc.listedHead+`"}]}`)
				}

			got, err := backend.CheckRunLogs(context.Background(), testRepo,
				ForgeCheckRun{Name: "CI / Lint", HeadSHA: tc.checkHead,
					URL: "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/9"}, 64)

			if tc.wantLogServed {
				if err != nil || !strings.Contains(got, "secret build output") {
					t.Fatalf("got = %q err = %v, want this commit's own log", got, err)
				}
				return
			}
			var unsupported *ForgeUnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v got = %q, want a refusal", err, got)
			}
			if strings.Contains(got, "secret build output") {
				t.Fatal("another run's log was quoted as this check's failure")
			}
		})
	}
}

// TestGiteaCheckRunLogs_RefusesWhenNothingRecordsTheCommit: the ownership check
// compares against the head this check covers, so with no head there is nothing
// to compare and the answer is a refusal, never an unverified fetch.
func TestGiteaCheckRunLogs_RefusesWhenNothingRecordsTheCommit(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	serveJobLog(fixture, "1", "9", "abc123", "build output")

	_, err := backend.CheckRunLogs(context.Background(), testRepo,
		ForgeCheckRun{Name: "CI / Lint", URL: "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/9"}, 64)

	var unsupported *ForgeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("the adapter called the forge before refusing: %+v", fixture.requests)
	}
}
