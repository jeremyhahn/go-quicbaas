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
	"strconv"
	"testing"
)

// serveRerunnableJob serves one Actions run whose single job is described by
// the attempts and statuses given, one entry per call of the jobs listing.
//
// The listing is asked TWICE per re-run — once to establish the job is this
// commit's, once to prove the write landed — so a sequence is what a test of
// the read-back needs.
func serveRerunnableJob(fixture *giteaFixture, runID, jobID, headSHA string, listings []string) *int {
	calls := 0
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/actions/runs/"+runID+"/jobs"] =
		func(w http.ResponseWriter, _ *http.Request) {
			body := listings[len(listings)-1]
			if calls < len(listings) {
				body = listings[calls]
			}
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}
	fixture.handlers["POST /api/v1/repos/jeremyhahn/go-quicbaas/actions/runs/"+runID+"/rerun-failed-jobs"] =
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	return &calls
}

// jobListing renders one job listing body.
func jobListing(jobID, runID, headSHA, status string, attempt int) string {
	return `{"jobs":[{"id":` + jobID + `,"run_id":` + runID + `,"head_sha":"` + headSHA +
		`","status":"` + status + `","run_attempt":` + strconv.Itoa(attempt) + `}]}`
}

// rerunnableRun is a check run whose status target URL names an Actions job on
// this repository, the shape Gitea itself writes.
func rerunnableRun() ForgeCheckRun {
	return ForgeCheckRun{
		ID:      "24",
		Name:    "CI / Test (pull_request)",
		HeadSHA: "abc123",
		URL:     "/jeremyhahn/go-quicbaas/actions/runs/8133/jobs/32352",
	}
}

// TestGiteaRerunCheckRun_RestartsTheRunTheStatusNames covers the ordinary
// success, in both shapes the read-back accepts.
func TestGiteaRerunCheckRun_RestartsTheRunTheStatusNames(t *testing.T) {
	cases := map[string][]string{
		"the attempt is bumped": {
			jobListing("32352", "8133", "abc123", "completed", 1),
			jobListing("32352", "8133", "abc123", "completed", 2),
		},
		"the job is waiting again": {
			jobListing("32352", "8133", "abc123", "completed", 1),
			jobListing("32352", "8133", "abc123", "waiting", 1),
		},
		"both at once": {
			jobListing("32352", "8133", "abc123", "completed", 1),
			jobListing("32352", "8133", "abc123", "running", 2),
		},
	}
	for name, listings := range cases {
		t.Run(name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			serveRerunnableJob(fixture, "8133", "32352", "abc123", listings)

			if err := backend.RerunCheckRun(context.Background(), testRepo, rerunnableRun()); err != nil {
				t.Fatalf("RerunCheckRun: %v", err)
			}
			posted := false
			for _, request := range fixture.requests {
				if request.Method == http.MethodPost &&
					request.Path == "/api/v1/repos/jeremyhahn/go-quicbaas/actions/runs/8133/rerun-failed-jobs" {
					posted = true
				}
			}
			if !posted {
				t.Fatalf("the rerun was never posted: %+v", fixture.requests)
			}
		})
	}
}

// TestGiteaRerunCheckRun_ReportsAWriteThatDidNotLand covers the read-back. A
// rerun reported as applied when it was not leaves the engine waiting out its
// whole review poll for a result nobody asked for.
func TestGiteaRerunCheckRun_ReportsAWriteThatDidNotLand(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	unchanged := jobListing("32352", "8133", "abc123", "completed", 1)
	serveRerunnableJob(fixture, "8133", "32352", "abc123", []string{unchanged, unchanged})

	err := backend.RerunCheckRun(context.Background(), testRepo, rerunnableRun())
	var notApplied *ForgeWriteNotAppliedError
	if !errors.As(err, &notApplied) {
		t.Fatalf("err = %v, want a *ForgeWriteNotAppliedError", err)
	}
	if notApplied.Operation != "RerunCheckRun" {
		t.Errorf("Operation = %q", notApplied.Operation)
	}
}

// TestGiteaRerunCheckRun_RefusesAStatusURLThisForgeDidNotWrite is the confused
// deputy again, and sharper than it is for the log route because this one is a
// WRITE: a publisher able to name any run could make a credentialed client
// restart CI it has nothing to do with, burning the fleet's capacity from a
// field anybody holding status-write controls.
//
// Every case is refused with NO request made at all, which the empty request
// log proves.
func TestGiteaRerunCheckRun_RefusesAStatusURLThisForgeDidNotWrite(t *testing.T) {
	for _, tc := range []struct{ name, url string }{
		{"a foreign host carrying the expected path", "https://attacker.invalid/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"another repository on this forge", "/jeremyhahn/go-truststrap/actions/runs/8133/jobs/32352"},
		{"another owner on this forge", "/someone-else/go-quicbaas/actions/runs/8133/jobs/32352"},
		{"the path buried under a prefix", "/anything/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"traversal that resolves elsewhere", "/jeremyhahn/go-quicbaas/actions/runs/1/jobs/../../../../other/repo/actions/runs/1/jobs/32352"},
		{"userinfo smuggling the host", "https://forge.internal:3001@attacker.invalid/jeremyhahn/go-quicbaas/actions/runs/1/jobs/32352"},
		{"a non-numeric run", "/jeremyhahn/go-quicbaas/actions/runs/latest/jobs/32352"},
		{"no job at all", "/jeremyhahn/go-quicbaas/actions/runs/1"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			run := rerunnableRun()
			run.URL = tc.url

			err := backend.RerunCheckRun(context.Background(), testRepo, run)
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

// TestGiteaRerunCheckRun_RefusesAJobThatIsNotThisCommitsCovers the second
// proof: the URL said which job, and the forge has to agree whose it is.
func TestGiteaRerunCheckRun_RefusesAJobThatIsNotThisCommits(t *testing.T) {
	cases := map[string]string{
		"the job belongs to another run": jobListing("32352", "9999", "abc123", "completed", 1),
		"the job covers another head":    jobListing("32352", "8133", "deadbeef", "completed", 1),
		"the job is absent from the run": jobListing("11111", "8133", "abc123", "completed", 1),
		"the run lists no jobs at all":   `{"jobs":[]}`,
	}
	for name, listing := range cases {
		t.Run(name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			serveRerunnableJob(fixture, "8133", "32352", "abc123", []string{listing})

			err := backend.RerunCheckRun(context.Background(), testRepo, rerunnableRun())
			var unsupported *ForgeUnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			for _, request := range fixture.requests {
				if request.Method == http.MethodPost {
					t.Fatalf("the adapter posted a rerun it had not proved it could make: %+v", request)
				}
			}
		})
	}
}

// TestGiteaRerunCheckRun_RefusesARunWithNoHead covers the last uncertainty: a
// check nothing records a commit for cannot be proved to be this commit's.
func TestGiteaRerunCheckRun_RefusesARunWithNoHead(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	run := rerunnableRun()
	run.HeadSHA = ""

	err := backend.RerunCheckRun(context.Background(), testRepo, run)
	var unsupported *ForgeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("the adapter called the forge before refusing: %+v", fixture.requests)
	}
}

// TestGiteaCapabilities_DeclareTheRerunRoute pins the capability the engine
// branches on. Declaring false where the platform can would make every void
// red escalate to an operator instead of being re-run.
func TestGiteaCapabilities_DeclareTheRerunRoute(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	caps := backend.Capabilities()
	if !caps.RerunCheckRuns {
		t.Error("Gitea publishes rerun-failed-jobs; declaring otherwise escalates every void red")
	}
	if caps.CancelCheckRuns {
		t.Error("Gitea publishes no cancel route")
	}
}
