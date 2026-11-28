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

package buildgates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var codeReviewWorkflowPath = filepath.Join(repoRoot, ".github", "workflows", "code-review.yml")

const (
	codexGateName = "Code Review (codex)"
	gateStepName  = "Require a Codex approval of the head, or a no-review change"
	gateGroup     = "${{ format('code-review-gate-{0}-{1}', github.event.pull_request.number, github.event.pull_request.head.sha) }}"
	gateHeadSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	gateBaseSHA   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	gateRunID     = 77
	gateReviewer  = "codex-reviewer[bot]"
)

// workflowJobs parses a workflow and returns its `jobs` mapping.
func workflowJobs(body []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("empty workflow")
	}
	jobs := mapValue(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil, errors.New("workflow has no jobs mapping")
	}
	return jobs, nil
}

// gateStep returns the run text of the gate job's approval step and the
// gate job's env.
func gateStep(t *testing.T) (string, map[string]string) {
	t.Helper()
	jobs, err := workflowJobs([]byte(readFile(t, codeReviewWorkflowPath)))
	if err != nil {
		t.Fatalf("parse %s: %v", codeReviewWorkflowPath, err)
	}
	gate := mapValue(jobs, "gate")
	if gate == nil {
		t.Fatal("code-review.yml has no gate job")
	}
	env := map[string]string{}
	if e := mapValue(gate, "env"); e != nil {
		for i := 0; i+1 < len(e.Content); i += 2 {
			env[e.Content[i].Value] = e.Content[i+1].Value
		}
	}
	steps := mapValue(gate, "steps")
	if steps == nil {
		t.Fatal("gate job has no steps")
	}
	for _, s := range steps.Content {
		if n := mapValue(s, "name"); n != nil && n.Value == gateStepName {
			run := mapValue(s, "run")
			if run == nil || run.Value == "" {
				t.Fatalf("gate step %q has no run text", gateStepName)
			}
			return run.Value, env
		}
	}
	t.Fatalf("gate job has no step %q", gateStepName)
	return "", nil
}

// gateTwin is another run of the workflow on the head.
type gateTwin struct {
	id           int
	runStatus    string // the status the runs listing files it under
	reviewStatus string // the status of its `Codex review` job
}

// gateCase is the fake API's state for one gate run.
type gateCase struct {
	twins  []gateTwin
	review string          // the reviewer's state on the head, or "" for none
	fail   map[string]bool // route -> answer HTTP 500
	base   string          // the live PR's base ref; "" means main
	// reviewResult is the review job's result (needs.review.result); ""
	// means skipped.
	reviewResult string
	// mutate replaces mutate[0] (exactly once in the step) with mutate[1].
	mutate [2]string
}

// gateResult is what one run of the gate step did.
type gateResult struct {
	code     int
	output   string
	slept    bool
	requests []string
	elapsed  time.Duration
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// runGate runs the gate step's script from code-review.yml against a fake
// forge API.
func runGate(t *testing.T, tc gateCase) gateResult {
	t.Helper()
	for _, tool := range []string{"bash", "curl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the gate step needs %s on PATH: %v", tool, err)
		}
	}
	script, env := gateStep(t)
	if tc.mutate[0] != "" {
		if n := strings.Count(script, tc.mutate[0]); n != 1 {
			t.Fatalf("the gate step has %d copies of %q, want 1", n, tc.mutate[0])
		}
		script = strings.Replace(script, tc.mutate[0], tc.mutate[1], 1)
	}
	reviewResult := tc.reviewResult
	if reviewResult == "" {
		reviewResult = "skipped"
	}

	var mu sync.Mutex
	var requests []string
	title := fmt.Sprintf("Code Review: PR #7 into main at %s", gateHeadSHA)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		p, q := r.URL.Path, r.URL.Query()
		route := ""
		switch {
		case p == "/compare/"+gateBaseSHA+"..."+gateHeadSHA:
			route = "compare"
		case p == fmt.Sprintf("/actions/runs/%d", gateRunID):
			route = "run"
		case p == "/actions/workflows/5/runs":
			route = "runs"
		case strings.HasPrefix(p, "/actions/runs/") && strings.HasSuffix(p, "/jobs"):
			route = "jobs"
		case p == "/pulls/7":
			route = "pull"
		case p == "/pulls/7/reviews":
			route = "reviews"
		default:
			http.NotFound(w, r)
			return
		}
		if tc.fail[route] {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		switch route {
		case "compare":
			writeJSON(w, map[string]any{"files": []map[string]any{{"filename": "pkg/x.go", "status": "modified"}}})
		case "run":
			writeJSON(w, map[string]any{"id": gateRunID, "workflow_id": 5})
		case "runs":
			st := q.Get("status")
			runs := []map[string]any{{"id": gateRunID, "display_title": title, "status": st}}
			for _, tw := range tc.twins {
				if tw.runStatus == st {
					runs = append(runs, map[string]any{"id": tw.id, "display_title": title, "status": st})
				}
			}
			writeJSON(w, map[string]any{"total_count": len(runs), "workflow_runs": runs})
		case "jobs":
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/actions/runs/"), "/jobs")
			review := "completed"
			for _, tw := range tc.twins {
				if fmt.Sprint(tw.id) == id {
					review = tw.reviewStatus
				}
			}
			writeJSON(w, map[string]any{"jobs": []map[string]any{
				{"name": "scope", "status": "completed", "conclusion": "success"},
				{"name": "Codex review", "status": review},
			}})
		case "pull":
			base := tc.base
			if base == "" {
				base = "main"
			}
			writeJSON(w, map[string]any{
				"number": 7,
				"head":   map[string]any{"sha": gateHeadSHA},
				"base":   map[string]any{"ref": base},
				"labels": []map[string]any{{"name": "code-review"}},
			})
		case "reviews":
			reviews := []map[string]any{}
			if q.Get("page") == "1" && tc.review != "" {
				reviews = append(reviews, map[string]any{
					"id":           1,
					"user":         map[string]any{"login": gateReviewer},
					"commit_id":    gateHeadSHA,
					"state":        tc.review,
					"submitted_at": "2026-11-01T00:00:00Z",
				})
			}
			writeJSON(w, reviews)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	work := filepath.Join(dir, "work")
	for _, d := range []string{bin, work} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(dir, "slept")
	fakeSleep := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> '%s'\nexit 99\n", marker)
	if err := os.WriteFile(filepath.Join(bin, "sleep"), []byte(fakeSleep), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = work
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + dir,
		"FORGE=github",
		"REVIEWER=" + gateReviewer,
		"REPO_API=" + srv.URL,
		"GH_API_TOKEN=x",
		"PR_NUM=7",
		"EVENT_SHA=" + gateHeadSHA,
		"BASE_SHA=" + gateBaseSHA,
		"BASE_REF=main",
		fmt.Sprintf("GITHUB_RUN_ID=%d", gateRunID),
		"NO_REVIEW_PATHS=" + env["NO_REVIEW_PATHS"],
		"DOCS_PATHS=" + env["DOCS_PATHS"],
		"SCOPE_SKIP=false",
		"SCOPE_RESULT=success",
		"REVIEW_RESULT=" + reviewResult,
	}
	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := gateResult{output: string(out), elapsed: time.Since(start)}
	if ctx.Err() != nil {
		t.Fatalf("the gate step ran past 60 s:\n%s", out)
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("run the gate step: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		res.slept = true
	}
	mu.Lock()
	res.requests = append([]string(nil), requests...)
	mu.Unlock()
	return res
}

func TestCodeReviewGateTwin(t *testing.T) {
	twinText := "runs in run 88"
	noApproval := "no " + gateReviewer + " approval"
	cases := map[string]struct {
		tc       gateCase
		wantCode int
		wantText string
		notText  string
	}{
		"queued twin, no review": {
			tc:       gateCase{twins: []gateTwin{{id: 88, runStatus: "queued", reviewStatus: "queued"}}},
			wantCode: 3, wantText: twinText,
		},
		"running twin, no review": {
			tc:       gateCase{twins: []gateTwin{{id: 88, runStatus: "in_progress", reviewStatus: "in_progress"}}},
			wantCode: 3, wantText: twinText,
		},
		"twin, changes requested": {
			tc: gateCase{
				twins:  []gateTwin{{id: 88, runStatus: "in_progress", reviewStatus: "in_progress"}},
				review: "CHANGES_REQUESTED",
			},
			wantCode: 3, wantText: twinText,
		},
		"twin, approved": {
			tc: gateCase{
				twins:  []gateTwin{{id: 88, runStatus: "queued", reviewStatus: "queued"}},
				review: "APPROVED",
			},
			wantCode: 3, wantText: twinText,
		},
		"no twin, approved": {
			tc:       gateCase{review: "APPROVED"},
			wantCode: 0, wantText: "approved " + gateHeadSHA,
		},
		"no twin, no review": {
			tc:       gateCase{},
			wantCode: 3, wantText: noApproval,
		},
		"twin whose review is completed, no review": {
			tc:       gateCase{twins: []gateTwin{{id: 88, runStatus: "in_progress", reviewStatus: "completed"}}},
			wantCode: 3, wantText: noApproval, notText: twinText,
		},
		"runs listing fails, approved": {
			tc:       gateCase{review: "APPROVED", fail: map[string]bool{"runs": true}},
			wantCode: 3, wantText: "cannot list the",
		},
		"twin jobs fail, approved": {
			tc: gateCase{
				twins:  []gateTwin{{id: 88, runStatus: "queued", reviewStatus: "queued"}},
				review: "APPROVED",
				fail:   map[string]bool{"jobs": true},
			},
			wantCode: 3, wantText: "cannot read the jobs of run 88",
		},
		"retargeted, approved": {
			tc:       gateCase{review: "APPROVED", base: "release"},
			wantCode: 3, wantText: "was retargeted from main to release",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := runGate(t, c.tc)
			if res.code != c.wantCode {
				t.Errorf("exit %d, want %d", res.code, c.wantCode)
			}
			if !strings.Contains(res.output, c.wantText) {
				t.Errorf("output does not contain %q", c.wantText)
			}
			if c.notText != "" && strings.Contains(res.output, c.notText) {
				t.Errorf("output contains %q", c.notText)
			}
			if res.slept {
				t.Error("the gate step ran sleep")
			}
			if len(c.tc.fail) == 0 && res.elapsed > 30*time.Second {
				t.Errorf("the gate step took %s", res.elapsed)
			}
			if t.Failed() {
				t.Logf("output:\n%s\nrequests:\n%s", res.output, strings.Join(res.requests, "\n"))
			}
		})
	}
}

// TestCodeReviewGateReviewResult runs the gate step with each result of
// this run's review job against a head the reviewer already approved: a
// cancelled or failed review must not let the earlier approval through,
// while a skipped or successful one does.
func TestCodeReviewGateReviewResult(t *testing.T) {
	if _, env := gateStep(t); env["REVIEW_RESULT"] != "${{ needs.review.result }}" {
		t.Fatalf("the gate job's REVIEW_RESULT is %q, want ${{ needs.review.result }}", env["REVIEW_RESULT"])
	}
	approved := "approved " + gateHeadSHA
	cancelled := "this run's Codex review job was cancelled, so no earlier approval of " + gateHeadSHA + " is reused"
	cases := map[string]struct {
		tc        gateCase
		wantCode  int
		wantText  string
		notText   string
		noVerdict bool // the step must not read the reviews
	}{
		"skipped review, approved": {
			tc:       gateCase{review: "APPROVED"},
			wantCode: 0, wantText: approved,
		},
		"successful review, approved": {
			tc:       gateCase{review: "APPROVED", reviewResult: "success"},
			wantCode: 0, wantText: approved,
		},
		"cancelled review, approved": {
			tc:       gateCase{review: "APPROVED", reviewResult: "cancelled"},
			wantCode: 3, wantText: cancelled, notText: approved, noVerdict: true,
		},
		"cancelled review, no review": {
			tc:       gateCase{reviewResult: "cancelled"},
			wantCode: 3, wantText: cancelled,
		},
		"failed review, approved": {
			tc:       gateCase{review: "APPROVED", reviewResult: "failure"},
			wantCode: 3, wantText: "this run's Codex review job failed, so no earlier approval of", notText: approved,
		},
		// Without the cancelled refusal the gate reuses the earlier
		// approval, so "cancelled review, approved" would fail.
		"mutation: no cancelled refusal": {
			tc: gateCase{
				review:       "APPROVED",
				reviewResult: "cancelled",
				mutate: [2]string{
					`cancelled) fail "this run's Codex review job was cancelled`,
					`cancelled-mutated) fail "this run's Codex review job was cancelled`,
				},
			},
			wantCode: 0, wantText: approved,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := runGate(t, c.tc)
			if res.code != c.wantCode {
				t.Errorf("exit %d, want %d", res.code, c.wantCode)
			}
			if !strings.Contains(res.output, c.wantText) {
				t.Errorf("output does not contain %q", c.wantText)
			}
			if c.notText != "" && strings.Contains(res.output, c.notText) {
				t.Errorf("output contains %q", c.notText)
			}
			if c.noVerdict {
				for _, r := range res.requests {
					if strings.Contains(r, "/pulls/7/reviews") {
						t.Errorf("the step read the reviews (%s) after refusing", r)
					}
				}
			}
			if res.slept {
				t.Error("the gate step ran sleep")
			}
			if res.elapsed > 30*time.Second {
				t.Errorf("the gate step took %s", res.elapsed)
			}
			if t.Failed() {
				t.Logf("output:\n%s\nrequests:\n%s", res.output, strings.Join(res.requests, "\n"))
			}
		})
	}
}

func TestCodeReviewGateTimeout(t *testing.T) {
	jobs, err := workflowJobs([]byte(readFile(t, codeReviewWorkflowPath)))
	if err != nil {
		t.Fatal(err)
	}
	tm := mapValue(mapValue(jobs, "gate"), "timeout-minutes")
	if tm == nil {
		t.Error("gate has no timeout-minutes, want 10")
	} else if tm.Value != "10" {
		t.Errorf("gate timeout-minutes is %s, want 10", tm.Value)
	}
	script, _ := gateStep(t)
	for _, s := range []string{"sleep", "4500"} {
		if strings.Contains(script, s) {
			t.Errorf("the gate step contains %q: no twin wait is left", s)
		}
	}
}

// gateHandoffProblems lists what breaks the gate's handoff to a twin run: the
// twin's gate must post the newest check of the same static name on the
// head, after its review ends, and newer gates must cancel older ones.
// gateDraftName is the gate's name since v5.12.9: the required name on a
// non-draft PR, another name on a draft, so a skipped gate never posts the
// required context.
const gateDraftName = "${{ github.event.pull_request.draft != true && '" + codexGateName + "' || 'Code Review (not requested)' }}"

// gateDraftIf is the gate's if since v5.12.9: always(), so it runs after a
// failed or skipped review, and never on a draft.
const gateDraftIf = "always() && github.event.pull_request.draft != true"

func gateHandoffProblems(body []byte) []string {
	jobs, err := workflowJobs(body)
	if err != nil {
		return []string{"parse: " + err.Error()}
	}
	var problems []string
	var named []string
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		if n := mapValue(jobs.Content[i+1], "name"); n != nil && (n.Value == codexGateName || n.Value == gateDraftName) {
			named = append(named, jobs.Content[i].Value)
		}
	}
	if len(named) != 1 || named[0] != "gate" {
		problems = append(problems, fmt.Sprintf("jobs named %q are %v, want only gate", codexGateName, named))
	}
	gate := mapValue(jobs, "gate")
	if gate == nil {
		return append(problems, "no gate job")
	}
	if n := mapValue(gate, "name"); n == nil || (n.Value != codexGateName && n.Value != gateDraftName) {
		problems = append(problems, fmt.Sprintf("gate name is not the literal %q or the draft-conditional name", codexGateName))
	}
	needsReview := false
	if needs := mapValue(gate, "needs"); needs != nil {
		if needs.Kind == yaml.ScalarNode {
			needsReview = needs.Value == "review"
		}
		for _, n := range needs.Content {
			needsReview = needsReview || n.Value == "review"
		}
	}
	if !needsReview {
		problems = append(problems, "gate needs does not include review")
	}
	if n := mapValue(gate, "if"); n == nil || (n.Value != "always()" && n.Value != gateDraftIf) {
		problems = append(problems, "gate if is not always()")
	}
	if mapValue(gate, "continue-on-error") != nil {
		problems = append(problems, "gate sets continue-on-error")
	}
	conc := mapValue(gate, "concurrency")
	if g := mapValue(conc, "group"); g == nil || g.Value != gateGroup {
		problems = append(problems, "gate concurrency group is not "+gateGroup)
	}
	if c := mapValue(conc, "cancel-in-progress"); c == nil || c.Value != "true" {
		problems = append(problems, "gate cancel-in-progress is not true")
	}
	review := mapValue(jobs, "review")
	if review == nil {
		return append(problems, "no review job")
	}
	if n := mapValue(review, "name"); n == nil || n.Value != "Codex review" {
		problems = append(problems, "review job name is not Codex review")
	}
	g := mapValue(mapValue(review, "concurrency"), "group")
	if g == nil || !strings.Contains(g.Value, "github.event.pull_request.number") ||
		!strings.Contains(g.Value, "github.event.pull_request.head.sha") {
		problems = append(problems, "review concurrency group is not keyed by PR number and head sha")
	}
	return problems
}

func TestCodeReviewGateHandsOffToTheTwin(t *testing.T) {
	if p := gateHandoffProblems([]byte(readFile(t, codeReviewWorkflowPath))); len(p) > 0 {
		t.Errorf("code-review.yml breaks the gate's handoff to a twin run:\n%s", strings.Join(p, "\n"))
	}
}

func TestCodeReviewGateHandsOffToTheTwin_Mutations(t *testing.T) {
	body := readFile(t, codeReviewWorkflowPath)
	gateName := "    name: >-\n      ${{ github.event.pull_request.draft != true\n      && 'Code Review (codex)' || 'Code Review (not requested)' }}\n"
	gateNeeds := "    needs: [scope, review]\n"
	gateIf := "    if: >-\n      always() && github.event.pull_request.draft != true\n"
	gateHead := gateName + gateNeeds + gateIf
	cancel := gateGroup + "\n      cancel-in-progress: true\n"
	cases := map[string]struct {
		old, new string
		want     string
	}{
		"needs scope only": {
			old: gateHead, new: gateName + "    needs: [scope]\n" + gateIf,
			want: "gate needs does not include review",
		},
		"if success": {
			old: gateHead, new: gateName + gateNeeds + "    if: >-\n      success() && github.event.pull_request.draft != true\n",
			want: "gate if is not always()",
		},
		"name per run": {
			old: gateHead, new: "    name: Code Review (codex) ${{ github.run_id }}\n" + gateNeeds + gateIf,
			want: "gate name is not the literal",
		},
		"second job of the name": {
			old: "\n  gate:\n", new: "\n  gate2:\n    name: Code Review (codex)\n    runs-on: ubuntu-latest\n    steps:\n      - run: 'true'\n  gate:\n",
			want: "want only gate",
		},
		"continue on error": {
			old: gateHead, new: gateHead + "    continue-on-error: true\n",
			want: "gate sets continue-on-error",
		},
		"no cancel in progress": {
			old: cancel, new: gateGroup + "\n      cancel-in-progress: false\n",
			want: "gate cancel-in-progress is not true",
		},
		"review job renamed": {
			old: "\n  review:\n    name: Codex review\n", new: "\n  review:\n    name: Codex review job\n",
			want: "review job name is not Codex review",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if strings.Count(body, c.old) != 1 {
				t.Fatalf("code-review.yml holds %q %d times, want once", c.old, strings.Count(body, c.old))
			}
			p := gateHandoffProblems([]byte(strings.Replace(body, c.old, c.new, 1)))
			if !strings.Contains(strings.Join(p, "\n"), c.want) {
				t.Errorf("problems %v do not name %q", p, c.want)
			}
		})
	}
}
