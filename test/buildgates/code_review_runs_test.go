package buildgates

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The run-management paths of code-review.yml: the supersede job, which
// cancels this PR's runs on an older head, and the gate job's handoff step,
// which re-runs a newer twin's red gate on this head. Both steps run with
// bash against a fake forge, as runGate runs the gate step.

const (
	supersedeJob      = "supersede"
	supersedeStepName = "Cancel this PR's runs on an older head"
	handoffStepName   = "Re-run a newer twin's failed gate on this head"
	olderHeadSHA      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	otherHeadSHA      = "cccccccccccccccccccccccccccccccccccccccc"
)

// workflowJob returns the named job of code-review.yml.
func workflowJob(t *testing.T, job string) *yaml.Node {
	t.Helper()
	jobs, err := workflowJobs([]byte(readFile(t, codeReviewWorkflowPath)))
	if err != nil {
		t.Fatalf("parse %s: %v", codeReviewWorkflowPath, err)
	}
	j := mapValue(jobs, job)
	if j == nil {
		t.Fatalf("code-review.yml has no %s job", job)
	}
	return j
}

// workflowStepNode returns the named step of the named job.
func workflowStepNode(t *testing.T, job, step string) *yaml.Node {
	t.Helper()
	steps := mapValue(workflowJob(t, job), "steps")
	if steps == nil {
		t.Fatalf("job %s has no steps", job)
	}
	for _, s := range steps.Content {
		if n := mapValue(s, "name"); n != nil && n.Value == step {
			return s
		}
	}
	t.Fatalf("job %s has no step %q", job, step)
	return nil
}

// workflowStep returns the run text of a job's step and the job's env.
func workflowStep(t *testing.T, job, step string) (string, map[string]string) {
	t.Helper()
	env := map[string]string{}
	if e := mapValue(workflowJob(t, job), "env"); e != nil {
		for i := 0; i+1 < len(e.Content); i += 2 {
			env[e.Content[i].Value] = e.Content[i+1].Value
		}
	}
	run := mapValue(workflowStepNode(t, job, step), "run")
	if run == nil || run.Value == "" {
		t.Fatalf("step %q of job %s has no run text", step, job)
	}
	return run.Value, env
}

type stepRun struct {
	code     int
	output   string
	requests []string // "METHOD RequestURI", in order
	sleeps   []string // the fake sleep's arguments, one per call
}

// runStep runs script with bash against a fake forge served by h.
func runStep(t *testing.T, script string, env []string, h http.HandlerFunc) stepRun {
	t.Helper()
	for _, tool := range []string{"bash", "curl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the step needs %s on PATH: %v", tool, err)
		}
	}
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		h(w, r)
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
	fakeSleep := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> '%s'\nexit 0\n", marker)
	if err := os.WriteFile(filepath.Join(bin, "sleep"), []byte(fakeSleep), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = work
	cmd.Env = append([]string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + dir,
		"REPO_API=" + srv.URL,
		"GH_API_TOKEN=x",
		fmt.Sprintf("GITHUB_RUN_ID=%d", gateRunID),
	}, env...)
	out, err := cmd.CombinedOutput()
	res := stepRun{output: string(out)}
	if ctx.Err() != nil {
		t.Fatalf("the step ran past 60 s:\n%s", out)
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("run the step: %v", err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		res.sleeps = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
	mu.Lock()
	res.requests = append([]string(nil), requests...)
	mu.Unlock()
	return res
}

// posts returns the POST requests only.
func posts(res stepRun) []string {
	var p []string
	for _, r := range res.requests {
		if strings.HasPrefix(r, "POST ") {
			p = append(p, r)
		}
	}
	return p
}

// fakeRun is one run of the code-review workflow on the fake forge.
type fakeRun struct {
	id      int
	title   string
	headSHA string
	suite   int
	// status[0] is the status the run lists with; each GET of the run
	// answers the next one, and the last one repeats.
	status []string
	page   int // the page of its status list (0: page 1)
	jobs   []map[string]any
}

// fakeForge is the state the fake forge answers from.
type fakeForge struct {
	pullHead  string // "" -> gateHeadSHA
	pullState string // "" -> open
	runs      []fakeRun
	// Routes that answer 500: pull, me, runs, jobs, run, cancel, rerun.
	fail map[string]bool

	mu    sync.Mutex
	polls map[int]int
}

func reviewTitle(pr int, base, head string) string {
	return fmt.Sprintf("Code Review: PR #%d into %s at %s", pr, base, head)
}

func job(id int, name, status, conclusion string) map[string]any {
	j := map[string]any{"id": id, "name": name, "status": status}
	if conclusion != "" {
		j["conclusion"] = conclusion
	}
	return j
}

func (f *fakeForge) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, q := r.URL.Path, r.URL.Query()
	route, id := "", 0
	switch {
	case r.Method == http.MethodGet && p == "/pulls/7":
		route = "pull"
	case r.Method == http.MethodGet && p == fmt.Sprintf("/actions/runs/%d", gateRunID):
		route = "me"
	case r.Method == http.MethodGet && p == "/actions/workflows/5/runs":
		route = "runs"
	case strings.HasPrefix(p, "/actions/runs/") || strings.HasPrefix(p, "/actions/jobs/"):
		parts := strings.Split(p, "/")
		if len(parts) < 4 {
			break
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil {
			break
		}
		id = n
		switch {
		case r.Method == http.MethodGet && len(parts) == 4 && parts[2] == "runs":
			route = "run"
		case r.Method == http.MethodGet && len(parts) == 5 && parts[2] == "runs" && parts[4] == "jobs":
			route = "jobs"
		case r.Method == http.MethodPost && len(parts) == 5 && parts[2] == "runs" && parts[4] == "cancel":
			route = "cancel"
		case r.Method == http.MethodPost && len(parts) == 5 && parts[2] == "jobs" && parts[4] == "rerun":
			route = "rerun"
		}
	}
	if route == "" {
		http.NotFound(w, r)
		return
	}
	if f.fail[route] {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		return
	}
	switch route {
	case "pull":
		head, state := f.pullHead, f.pullState
		if head == "" {
			head = gateHeadSHA
		}
		if state == "" {
			state = "open"
		}
		writeJSON(w, map[string]any{"number": 7, "state": state, "head": map[string]any{"sha": head}})
	case "me":
		writeJSON(w, map[string]any{"id": gateRunID, "workflow_id": 5, "check_suite_id": 500})
	case "runs":
		runs := []map[string]any{}
		for _, fr := range f.runs {
			if h := q.Get("head_sha"); h != "" {
				if fr.headSHA != h {
					continue
				}
			} else {
				page := fr.page
				if page == 0 {
					page = 1
				}
				if fr.status[0] != q.Get("status") || strconv.Itoa(page) != q.Get("page") {
					continue
				}
			}
			runs = append(runs, map[string]any{
				"id": fr.id, "display_title": fr.title, "head_sha": fr.headSHA,
				"check_suite_id": fr.suite, "status": fr.status[0],
			})
		}
		writeJSON(w, map[string]any{"total_count": len(runs), "workflow_runs": runs})
	case "run":
		for _, fr := range f.runs {
			if fr.id == id {
				if f.polls == nil {
					f.polls = map[int]int{}
				}
				f.polls[id]++
				i := min(f.polls[id], len(fr.status)-1)
				writeJSON(w, map[string]any{"id": id, "status": fr.status[i]})
				return
			}
		}
		http.NotFound(w, r)
	case "jobs":
		jobs := []map[string]any{}
		for _, fr := range f.runs {
			if fr.id == id && fr.jobs != nil {
				jobs = fr.jobs
			}
		}
		writeJSON(w, map[string]any{"total_count": len(jobs), "jobs": jobs})
	case "cancel":
		w.WriteHeader(http.StatusAccepted)
	case "rerun":
		w.WriteHeader(http.StatusCreated)
	}
}

// runCase is one fake-forge run of a step and what it must show.
type runCase struct {
	name     string
	env      []string // added to the step's base env
	forge    *fakeForge
	mutate   [2]string // replace mutate[0] (exactly once in the script) with mutate[1]
	code     int
	posts    []string // the exact POSTs; nil: none
	sleeps   []string // the exact sleep arguments; nil: none
	out      []string // substrings of the output
	request  []string // substrings some request must contain
	noReq    []string // substrings no request may contain
	anyPosts bool     // the POSTs are not checked (a failing POST is retried)
	noServer bool     // no request may reach the fake forge
}

func checkRun(t *testing.T, script string, base []string, tc runCase) {
	t.Helper()
	if tc.mutate[0] != "" {
		if n := strings.Count(script, tc.mutate[0]); n != 1 {
			t.Fatalf("the mutation's text %q occurs %d times in the step, want 1", tc.mutate[0], n)
		}
		script = strings.Replace(script, tc.mutate[0], tc.mutate[1], 1)
	}
	res := runStep(t, script, append(append([]string(nil), base...), tc.env...), tc.forge.handler)
	if res.code != tc.code {
		t.Errorf("exit %d, want %d", res.code, tc.code)
	}
	if !tc.anyPosts && !reflect.DeepEqual(posts(res), tc.posts) {
		t.Errorf("POSTs %q, want %q", posts(res), tc.posts)
	}
	if !reflect.DeepEqual(res.sleeps, tc.sleeps) {
		t.Errorf("sleeps %q, want %q", res.sleeps, tc.sleeps)
	}
	for _, s := range tc.out {
		if !strings.Contains(res.output, s) {
			t.Errorf("output lacks %q", s)
		}
	}
	for _, s := range tc.request {
		found := false
		for _, r := range res.requests {
			if strings.Contains(r, s) {
				found = true
			}
		}
		if !found {
			t.Errorf("no request contains %q", s)
		}
	}
	for _, s := range tc.noReq {
		for _, r := range res.requests {
			if strings.Contains(r, s) {
				t.Errorf("request %q contains %q", r, s)
			}
		}
	}
	if tc.noServer && len(res.requests) != 0 {
		t.Errorf("requests %q, want none", res.requests)
	}
	if t.Failed() {
		t.Logf("output:\n%s\nrequests: %q", res.output, res.requests)
	}
}

// supersedeRun is a run of PR pr on head, listed under status.
func supersedeRun(id, pr int, head, status string) fakeRun {
	return fakeRun{id: id, title: reviewTitle(pr, "main", head), headSHA: head, status: []string{status}}
}

// supersedeForge holds this run (77, in progress on the current head) and runs.
func supersedeForge(runs ...fakeRun) *fakeForge {
	return &fakeForge{runs: append([]fakeRun{supersedeRun(gateRunID, 7, gateHeadSHA, "in_progress")}, runs...)}
}

func supersedeExclusions() *fakeForge {
	r61 := supersedeRun(61, 7, gateHeadSHA, "queued")
	r61.headSHA = olderHeadSHA
	r62 := supersedeRun(62, 7, olderHeadSHA, "queued")
	r62.headSHA = gateHeadSHA
	r64 := supersedeRun(64, 7, olderHeadSHA, "in_progress")
	r64.jobs = []map[string]any{job(641, "scope", "completed", "success"), job(642, "Codex review", "in_progress", "")}
	r65 := supersedeRun(65, 7, olderHeadSHA, "queued")
	r65.jobs = []map[string]any{job(651, "Code Review (opencode)", "in_progress", "")}
	return supersedeForge(
		r61, r62,
		supersedeRun(90, 7, olderHeadSHA, "queued"),
		supersedeRun(63, 8, olderHeadSHA, "queued"),
		r64, r65,
		supersedeRun(60, 7, olderHeadSHA, "queued"),
	)
}

func TestCodeReviewSupersede(t *testing.T) {
	sj := workflowJob(t, supersedeJob)
	if a := mapValue(mapValue(sj, "permissions"), "actions"); a == nil || a.Value != "write" {
		t.Errorf("the supersede job's permissions.actions is not write")
	}
	if c := mapValue(sj, "if"); c == nil || !strings.Contains(c.Value, "github.event.pull_request.draft != true") {
		t.Errorf("the supersede job's if does not skip drafts")
	}
	script, _ := workflowStep(t, supersedeJob, supersedeStepName)
	base := []string{"PR_NUM=7", "EVENT_SHA=" + gateHeadSHA}
	cancel60 := []string{"POST /actions/runs/60/cancel"}

	older60 := supersedeRun(60, 7, olderHeadSHA, "queued")
	older60.jobs = []map[string]any{job(601, "scope", "completed", "success"), job(602, "Codex review", "completed", "success")}

	page1 := []fakeRun{}
	for i := 0; i < 100; i++ {
		page1 = append(page1, supersedeRun(1000+i, 8, olderHeadSHA, "queued"))
	}
	page2 := supersedeRun(60, 7, olderHeadSHA, "queued")
	page2.page = 2

	cases := []runCase{
		{name: "older head queued", forge: supersedeForge(older60), posts: cancel60,
			out: []string{"Run 60 is on an older head"}},
		{name: "exclusions", forge: supersedeExclusions(), posts: cancel60,
			out: []string{"Run 64: its review is in progress", "Run 65: its review is in progress"}},
		{name: "stale event head", forge: func() *fakeForge {
			f := supersedeForge(older60)
			f.pullHead = otherHeadSHA
			return f
		}(), out: []string{"nothing to cancel"}, noReq: []string{"/actions/workflows/"}},
		{name: "closed PR", forge: func() *fakeForge {
			f := supersedeForge(older60)
			f.pullState = "closed"
			return f
		}(), out: []string{"nothing to cancel"}, noReq: []string{"/actions/workflows/"}},
		{name: "second page", forge: supersedeForge(append(page1, page2)...), posts: cancel60,
			request: []string{"status=queued&per_page=100&page=2"}},
		{name: "every status", forge: supersedeForge(supersedeRun(60, 7, olderHeadSHA, "waiting")), posts: cancel60,
			request: []string{"status=waiting"}},
		{name: "pull read fails", forge: func() *fakeForge {
			f := supersedeForge(older60)
			f.fail = map[string]bool{"pull": true}
			return f
		}(), out: []string{"::warning::Cancel superseded runs:"}},
		{name: "cancel fails", forge: func() *fakeForge {
			f := supersedeForge(older60)
			f.fail = map[string]bool{"cancel": true}
			return f
		}(), anyPosts: true, out: []string{"answered HTTP 500"}, request: cancel60},
		{name: "mutation: no id bound", forge: supersedeExclusions(),
			mutate: [2]string{" and .id < $me)", ")"},
			posts:  []string{"POST /actions/runs/60/cancel", "POST /actions/runs/90/cancel"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkRun(t, script, base, tc)
		})
	}
}

// handoffRun is a run of PR #7 into main on this head in check suite suite.
func handoffRun(id, suite int, jobs []map[string]any, status ...string) fakeRun {
	return fakeRun{id: id, title: reviewTitle(7, "main", gateHeadSHA), headSHA: gateHeadSHA,
		suite: suite, status: status, jobs: jobs}
}

// handoffForge holds this run (77, suite 500, in progress) and runs.
func handoffForge(runs ...fakeRun) *fakeForge {
	return &fakeForge{runs: append([]fakeRun{handoffRun(gateRunID, 500, nil, "in_progress")}, runs...)}
}

func redGate(id int, conclusion string) []map[string]any {
	return []map[string]any{
		job(id-1, "Codex review", "completed", "success"),
		job(id, codexGateName, "completed", conclusion),
	}
}

func handoffNotNewer() *fakeForge {
	r89 := handoffRun(89, 700, redGate(891, "failure"), "completed")
	r89.title = reviewTitle(7, "release", gateHeadSHA)
	return handoffForge(handoffRun(70, 400, redGate(701, "failure"), "completed"), r89)
}

func TestCodeReviewHandoff(t *testing.T) {
	if a := mapValue(mapValue(workflowJob(t, "gate"), "permissions"), "actions"); a == nil || a.Value != "write" {
		t.Errorf("the gate job's permissions.actions is not write")
	}
	if c := mapValue(workflowStepNode(t, "gate", handoffStepName), "if"); c == nil || c.Value != "success() && env.FORGE == 'github'" {
		t.Errorf("the handoff step's if is not success() && env.FORGE == 'github'")
	}
	script, _ := workflowStep(t, "gate", handoffStepName)
	base := []string{"PR_NUM=7", "EVENT_SHA=" + gateHeadSHA, "BASE_REF=main", "HANDOFF_POLL_S=0", "HANDOFF_WAIT_S=60"}
	rerun901 := []string{"POST /actions/jobs/901/rerun"}
	inProgress := func(jobs ...map[string]any) *fakeForge {
		return handoffForge(handoffRun(88, 600, jobs, "in_progress"))
	}

	cases := []runCase{
		{name: "no newer run", forge: handoffForge(), out: []string{"this gate's check stands"}},
		{name: "newer run, red gate", forge: handoffForge(handoffRun(88, 600, redGate(901, "failure"), "completed")),
			posts: rerun901},
		{name: "newer run, cancelled gate", forge: handoffForge(handoffRun(88, 600, redGate(901, "cancelled"), "completed")),
			posts: rerun901},
		{name: "newer run, green gate", forge: handoffForge(handoffRun(88, 600, redGate(901, "success"), "completed")),
			out: []string{"did not end red"}},
		{name: "not newer or not this PR", forge: handoffNotNewer()},
		{name: "newer run reviews itself, queued", forge: inProgress(job(900, "Codex review", "queued", "")),
			out: []string{"reviews " + gateHeadSHA + " itself"}},
		{name: "newer run reviews itself, in progress", forge: inProgress(job(900, "Codex review", "in_progress", "")),
			out: []string{"reviews " + gateHeadSHA + " itself"}},
		{name: "gate not started, no gate job", forge: inProgress(job(900, "Codex review", "completed", "success")),
			out: []string{"has not run its gate yet"}},
		{name: "gate not started, gate queued", forge: inProgress(
			job(900, "Codex review", "completed", "success"), job(901, codexGateName, "queued", "")),
			out: []string{"has not run its gate yet"}},
		{name: "polls until the run ends",
			forge:  handoffForge(handoffRun(88, 600, redGate(901, "failure"), "in_progress", "completed")),
			sleeps: []string{"0"}, posts: rerun901, request: []string{"GET /actions/runs/88"}},
		{name: "bound passes, red gate", env: []string{"HANDOFF_WAIT_S=0"},
			forge: handoffForge(handoffRun(88, 600, redGate(901, "failure"), "in_progress")), code: 5,
			out: []string{"::error::Code Review (codex):", "is still in_progress after the handoff's 0 s bound"}},
		{name: "bound passes, gate running", env: []string{"HANDOFF_WAIT_S=0"},
			forge: inProgress(job(900, "Codex review", "completed", "success"), job(901, codexGateName, "in_progress", "")),
			out:   []string{"is still running after the handoff's 0 s bound"}},
		{name: "listing fails", forge: func() *fakeForge {
			f := handoffForge(handoffRun(88, 600, redGate(901, "failure"), "completed"))
			f.fail = map[string]bool{"runs": true}
			return f
		}(), out: []string{"a newer twin's gate is not re-run"}},
		{name: "rerun fails", forge: func() *fakeForge {
			f := handoffForge(handoffRun(88, 600, redGate(901, "failure"), "completed"))
			f.fail = map[string]bool{"rerun": true}
			return f
		}(), anyPosts: true, out: []string{"answered HTTP 500"}, request: rerun901},
		{name: "bad bounds", env: []string{"HANDOFF_POLL_S=abc"}, forge: handoffForge(),
			out: []string{"must be whole seconds"}, noServer: true},
		{name: "mutation: no suite bound", forge: handoffNotNewer(),
			mutate: [2]string{" and .check_suite_id > $s", ""},
			posts:  []string{"POST /actions/jobs/701/rerun"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkRun(t, script, base, tc)
		})
	}
}
