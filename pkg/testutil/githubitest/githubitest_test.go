package githubitest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	testToken    = "author-token-value"
	testReviewer = "reviewer-token-value"
)

// recorded is one request the fake GitHub saw.
type recorded struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// fakeGitHub answers each "METHOD path" from routes and records every
// request; an unrouted request is a 404.
type fakeGitHub struct {
	t      *testing.T
	routes map[string]func(w http.ResponseWriter)
	seen   []recorded
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *Scratch) {
	t.Helper()
	fake := &fakeGitHub{t: t, routes: map[string]func(w http.ResponseWriter){}}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	s, err := New(Env{
		Token:         testToken,
		ReviewerToken: testReviewer,
		Owner:         "acme",
		Repo:          "scratch",
		BaseURL:       server.URL,
	}, server.Client())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return fake, s
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	rec := recorded{Method: r.Method, Path: r.URL.EscapedPath(), Auth: r.Header.Get("Authorization")}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read body: %v", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &rec.Body); err != nil {
			f.t.Errorf("body is not JSON: %v", err)
		}
	}
	f.seen = append(f.seen, rec)
	route, ok := f.routes[r.Method+" "+rec.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeBody(f.t, w, `{"message":"Not Found"}`)
		return
	}
	route(w)
}

func writeBody(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write: %v", err)
	}
}

func (f *fakeGitHub) answer(key string, status int, body string) {
	f.routes[key] = func(w http.ResponseWriter) {
		w.WriteHeader(status)
		writeBody(f.t, w, body)
	}
}

func (f *fakeGitHub) only(t *testing.T) recorded {
	t.Helper()
	if len(f.seen) != 1 {
		t.Fatalf("requests = %d, want 1: %+v", len(f.seen), f.seen)
	}
	return f.seen[0]
}

func TestLoadEnv_NamesEveryMissingVariable(t *testing.T) {
	for _, name := range []string{EnvToken, EnvReviewerToken, EnvOwner, EnvRepo, EnvBaseURL} {
		t.Setenv(name, "")
	}
	_, err := LoadEnv()
	var missing *MissingEnvError
	if !errors.As(err, &missing) {
		t.Fatalf("LoadEnv error = %v, want *MissingEnvError", err)
	}
	for _, name := range []string{EnvToken, EnvReviewerToken, EnvOwner, EnvRepo} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	if strings.Contains(err.Error(), EnvBaseURL) {
		t.Errorf("error %q names the optional %s", err, EnvBaseURL)
	}
}

func TestLoadEnv_NamesOnlyTheMissingOne(t *testing.T) {
	t.Setenv(EnvToken, testToken)
	t.Setenv(EnvReviewerToken, testReviewer)
	t.Setenv(EnvOwner, "acme")
	t.Setenv(EnvRepo, "")
	_, err := LoadEnv()
	var missing *MissingEnvError
	if !errors.As(err, &missing) {
		t.Fatalf("LoadEnv error = %v, want *MissingEnvError", err)
	}
	if len(missing.Names) != 1 || missing.Names[0] != EnvRepo {
		t.Fatalf("missing = %v, want [%s]", missing.Names, EnvRepo)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error %q carries the token", err)
	}
}

func TestLoadEnv_ReturnsEnvWhenSet(t *testing.T) {
	t.Setenv(EnvToken, testToken)
	t.Setenv(EnvReviewerToken, testReviewer)
	t.Setenv(EnvOwner, "acme")
	t.Setenv(EnvRepo, "scratch")
	t.Setenv(EnvBaseURL, "")
	env, err := LoadEnv()
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	want := Env{Token: testToken, ReviewerToken: testReviewer, Owner: "acme", Repo: "scratch", BaseURL: DefaultBaseURL}
	if env != want {
		t.Fatalf("env = %+v, want %+v", env, want)
	}

	t.Setenv(EnvBaseURL, "https://ghe.example/api/v3/")
	env, err = LoadEnv()
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if env.BaseURL != "https://ghe.example/api/v3" {
		t.Fatalf("BaseURL = %q, want the trailing slash trimmed", env.BaseURL)
	}
}

func TestNamespace_IsUniquePerRunAndBranchSafe(t *testing.T) {
	_, a := newFakeGitHub(t)
	_, b := newFakeGitHub(t)
	if a.Namespace() == b.Namespace() {
		t.Fatalf("two runs share namespace %q", a.Namespace())
	}
	for _, r := range a.Namespace() {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Fatalf("namespace %q holds %q", a.Namespace(), r)
		}
	}
}

func TestCommitFile_PutsBase64ContentOnTheBranch(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("PUT /repos/acme/scratch/contents/dir/a%20b.txt", http.StatusCreated,
		`{"content":{"sha":"blob1"},"commit":{"sha":"commit1"}}`)

	sha, err := s.CommitFile(context.Background(), "feat/x", "dir/a b.txt", "hello", "add a")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	if sha != "commit1" {
		t.Fatalf("sha = %q, want commit1", sha)
	}
	rec := fake.only(t)
	if rec.Auth != "Bearer "+testToken {
		t.Fatalf("auth = %q, want the author token", rec.Auth)
	}
	want := map[string]any{
		"message": "add a",
		"content": base64.StdEncoding.EncodeToString([]byte("hello")),
		"branch":  "feat/x",
	}
	for k, v := range want {
		if rec.Body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, rec.Body[k], v)
		}
	}
}

func TestCommitFile_A4xxIsTypedAndCarriesNoToken(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("PUT /repos/acme/scratch/contents/a.txt", http.StatusUnprocessableEntity,
		`{"message":"Invalid request"}`)

	_, err := s.CommitFile(context.Background(), "main", "a.txt", "x", "m")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.Method != http.MethodPut || apiErr.Path != "/repos/acme/scratch/contents/a.txt" || apiErr.Status != 422 {
		t.Fatalf("APIError = %+v", apiErr)
	}
	for _, secret := range []string{testToken, testReviewer} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q carries a token", err)
		}
	}
}

func TestOpenPR_PostsHeadAndBaseAndTracksTheNumber(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("POST /repos/acme/scratch/pulls", http.StatusCreated, `{"number":42}`)

	number, err := s.OpenPR(context.Background(), "feat/x", "base", "Add x")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if number != 42 {
		t.Fatalf("number = %d, want 42", number)
	}
	rec := fake.only(t)
	if rec.Body["head"] != "feat/x" || rec.Body["base"] != "base" || rec.Body["title"] != "Add x" {
		t.Fatalf("body = %v", rec.Body)
	}
	if len(s.pulls) != 1 || s.pulls[0] != 42 {
		t.Fatalf("tracked pulls = %v, want [42]", s.pulls)
	}
}

func TestOpenPR_ARefusalTracksNothing(t *testing.T) {
	_, s := newFakeGitHub(t)
	_, err := s.OpenPR(context.Background(), "feat/x", "base", "Add x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("error = %v, want a 404 *APIError", err)
	}
	if len(s.pulls) != 0 {
		t.Fatalf("tracked pulls = %v, want none", s.pulls)
	}
}

func TestApprove_ReviewsTheHeadAsTheReviewer(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("POST /repos/acme/scratch/pulls/7/reviews", http.StatusOK, `{"id":1}`)

	if err := s.Approve(context.Background(), 7, "abc123"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	rec := fake.only(t)
	if rec.Auth != "Bearer "+testReviewer {
		t.Fatalf("auth = %q, want the reviewer token", rec.Auth)
	}
	if rec.Body["event"] != "APPROVE" || rec.Body["commit_id"] != "abc123" {
		t.Fatalf("body = %v", rec.Body)
	}
}

func TestApprove_AForbiddenReviewIsTyped(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("POST /repos/acme/scratch/pulls/7/reviews", http.StatusForbidden, `{"message":"cannot approve"}`)

	err := s.Approve(context.Background(), 7, "abc123")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Message != "cannot approve" {
		t.Fatalf("error = %v, want a 403 *APIError with the message", err)
	}
	if strings.Contains(err.Error(), testReviewer) {
		t.Fatalf("error %q carries the reviewer token", err)
	}
}

func TestCutBase_BranchesFromTheDefaultBranch(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("GET /repos/acme/scratch", http.StatusOK, `{"default_branch":"trunk"}`)
	fake.answer("GET /repos/acme/scratch/git/ref/heads/trunk", http.StatusOK, `{"object":{"sha":"tip"}}`)
	fake.answer("POST /repos/acme/scratch/git/refs", http.StatusCreated, `{}`)

	if err := s.CutBase(context.Background(), "itest/base"); err != nil {
		t.Fatalf("CutBase: %v", err)
	}
	last := fake.seen[len(fake.seen)-1]
	if last.Body["ref"] != "refs/heads/itest/base" || last.Body["sha"] != "tip" {
		t.Fatalf("create ref body = %v", last.Body)
	}
	if len(s.branches) != 1 || s.branches[0] != "itest/base" {
		t.Fatalf("tracked branches = %v", s.branches)
	}
}

func TestCutBase_AMissingRepositoryIsTyped(t *testing.T) {
	_, s := newFakeGitHub(t)
	err := s.CutBase(context.Background(), "itest/base")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Path != "/repos/acme/scratch" {
		t.Fatalf("error = %v, want *APIError on the repository", err)
	}
}

func TestInstallQueueCI_CommitsTheEmbeddedWorkflow(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("PUT /repos/acme/scratch/contents/.github/workflows/queue-ci.yml", http.StatusCreated,
		`{"commit":{"sha":"ci1"}}`)

	sha, err := s.InstallQueueCI(context.Background(), "itest/base")
	if err != nil || sha != "ci1" {
		t.Fatalf("InstallQueueCI = %q, %v", sha, err)
	}
	content, ok := fake.only(t).Body["content"].(string)
	if !ok {
		t.Fatalf("content is not a string")
	}
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if string(decoded) != string(QueueCI()) {
		t.Fatalf("committed workflow differs from the embedded one")
	}
}

func TestQueueCI_ParsesAndGatesQueueHeadsOnBreakCI(t *testing.T) {
	var workflow struct {
		On   string `yaml:"on"`
		Jobs map[string]struct {
			Name  string `yaml:"name"`
			If    string `yaml:"if"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(QueueCI(), &workflow); err != nil {
		t.Fatalf("queue-ci.yml does not parse: %v", err)
	}
	if workflow.On != "pull_request" {
		t.Fatalf("on = %q, want pull_request", workflow.On)
	}
	job, ok := workflow.Jobs[QueueCICheck]
	if !ok || job.Name != QueueCICheck {
		t.Fatalf("no job named %s: %+v", QueueCICheck, workflow.Jobs)
	}
	if !strings.Contains(job.If, "startsWith(github.head_ref, 'queue/')") {
		t.Fatalf("if = %q, want a queue/* head guard", job.If)
	}
	var breaks bool
	for _, step := range job.Steps {
		breaks = breaks || strings.Contains(step.Run, BreakCIFile)
	}
	if !breaks {
		t.Fatalf("no step tests for %s", BreakCIFile)
	}
}

func TestCleanup_ClosesOpenPullsAndDeletesBranches(t *testing.T) {
	fake, s := newFakeGitHub(t)
	s.TrackPullRequest(1)
	s.TrackPullRequest(2)
	s.TrackBranch("itest/a")
	s.TrackBranch("itest/gone")
	fake.answer("GET /repos/acme/scratch/pulls/1", http.StatusOK, `{"number":1,"state":"open"}`)
	fake.answer("GET /repos/acme/scratch/pulls/2", http.StatusOK, `{"number":2,"state":"closed","merged":true}`)
	fake.answer("PATCH /repos/acme/scratch/pulls/1", http.StatusOK, `{}`)
	fake.answer("DELETE /repos/acme/scratch/git/refs/heads/itest/a", http.StatusNoContent, ``)
	fake.answer("DELETE /repos/acme/scratch/git/refs/heads/itest/gone", http.StatusUnprocessableEntity,
		`{"message":"Reference does not exist"}`)

	if err := s.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	var patched, deleted int
	for _, rec := range fake.seen {
		switch rec.Method {
		case http.MethodPatch:
			patched++
			if rec.Body["state"] != "closed" {
				t.Errorf("patch body = %v", rec.Body)
			}
		case http.MethodDelete:
			deleted++
		}
	}
	if patched != 1 || deleted != 2 {
		t.Fatalf("patched %d, deleted %d; want 1 and 2", patched, deleted)
	}
}

func TestCleanup_ReturnsEveryFailure(t *testing.T) {
	fake, s := newFakeGitHub(t)
	s.TrackPullRequest(9)
	s.TrackBranch("itest/a")
	s.TrackBranch("itest/b")
	fake.answer("GET /repos/acme/scratch/pulls/9", http.StatusInternalServerError, `{}`)
	fake.answer("DELETE /repos/acme/scratch/git/refs/heads/itest/a", http.StatusForbidden, `{}`)
	fake.answer("DELETE /repos/acme/scratch/git/refs/heads/itest/b", http.StatusInternalServerError, `{}`)

	err := s.Cleanup(context.Background())
	if err == nil {
		t.Fatalf("Cleanup succeeded, want three failures")
	}
	for _, want := range []string{"/pulls/9", "/heads/itest/a", "/heads/itest/b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error %q carries the token", err)
	}
}

func TestBranchExists_ReadsANotFoundAsAbsent(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("GET /repos/acme/scratch/git/ref/heads/here", http.StatusOK, `{"object":{"sha":"s"}}`)
	fake.answer("GET /repos/acme/scratch/git/ref/heads/broken", http.StatusBadGateway, `{}`)

	if ok, err := s.BranchExists(context.Background(), "here"); err != nil || !ok {
		t.Fatalf("BranchExists(here) = %v, %v", ok, err)
	}
	if ok, err := s.BranchExists(context.Background(), "gone"); err != nil || ok {
		t.Fatalf("BranchExists(gone) = %v, %v", ok, err)
	}
	if _, err := s.BranchExists(context.Background(), "broken"); err == nil {
		t.Fatalf("BranchExists(broken) succeeded, want the 502")
	}
}

func TestDo_AnUnparsableAnswerIsADecodeError(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("POST /repos/acme/scratch/pulls", http.StatusCreated, `not json`)

	_, err := s.OpenPR(context.Background(), "h", "b", "t")
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("error = %v, want *DecodeError", err)
	}
}

func TestCloneURL_FollowsTheAPIRoot(t *testing.T) {
	cases := map[string]string{
		DefaultBaseURL:               "https://github.com/acme/scratch.git",
		"https://ghe.example/api/v3": "https://ghe.example/acme/scratch.git",
	}
	for base, want := range cases {
		env := Env{Owner: "acme", Repo: "scratch", BaseURL: base}
		if got := env.CloneURL(); got != want {
			t.Errorf("CloneURL(%s) = %q, want %q", base, got, want)
		}
	}
}

func TestOpenIssue_PostsLabelsAndCleanupClosesIt(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("POST /repos/acme/scratch/issues", http.StatusCreated, `{"number":5}`)
	fake.answer("PATCH /repos/acme/scratch/issues/5", http.StatusOK, `{}`)

	number, err := s.OpenIssue(context.Background(), "Fix it", "body", "task:ready")
	if err != nil || number != 5 {
		t.Fatalf("OpenIssue = %d, %v", number, err)
	}
	labels, ok := fake.seen[0].Body["labels"].([]any)
	if !ok || len(labels) != 1 || labels[0] != "task:ready" {
		t.Fatalf("labels = %v", fake.seen[0].Body["labels"])
	}
	if err := s.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	last := fake.seen[len(fake.seen)-1]
	if last.Method != http.MethodPatch || last.Body["state"] != "closed" {
		t.Fatalf("cleanup sent %+v, want the issue closed", last)
	}
}

func TestOpenIssue_ARefusalIsTyped(t *testing.T) {
	_, s := newFakeGitHub(t)
	_, err := s.OpenIssue(context.Background(), "Fix it", "body")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Path != "/repos/acme/scratch/issues" {
		t.Fatalf("error = %v, want *APIError on the issues path", err)
	}
}

func TestFileExists_ReadsTheTreeAtARef(t *testing.T) {
	fake, s := newFakeGitHub(t)
	fake.answer("GET /repos/acme/scratch/contents/a.txt", http.StatusOK, `{"sha":"x"}`)
	fake.answer("GET /repos/acme/scratch/contents/broken.txt", http.StatusInternalServerError, `{}`)

	if ok, err := s.FileExists(context.Background(), "base", "a.txt"); err != nil || !ok {
		t.Fatalf("FileExists(a.txt) = %v, %v", ok, err)
	}
	if ok, err := s.FileExists(context.Background(), "base", "b.txt"); err != nil || ok {
		t.Fatalf("FileExists(b.txt) = %v, %v", ok, err)
	}
	if _, err := s.FileExists(context.Background(), "base", "broken.txt"); err == nil {
		t.Fatalf("FileExists(broken.txt) succeeded, want the 500")
	}
}
