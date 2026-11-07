// Package githubitest holds the scratch-repository fixtures the live GitHub
// merge-train e2e drives: the fail-closed environment gate, and a small
// REST client that cuts branches, commits files, opens and approves pull
// requests, installs the queue CI workflow and cleans up after a run.
//
// The package carries no build tag; only integration-tagged tests import
// it. It never logs and never puts a token in an error: a token travels in
// the Authorization header and nowhere else.
package githubitest

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The environment variables the live GitHub suites read.
const (
	EnvToken         = "GITHUB_ITEST_TOKEN"
	EnvReviewerToken = "GITHUB_ITEST_REVIEWER_TOKEN"
	EnvOwner         = "GITHUB_ITEST_OWNER"
	EnvRepo          = "GITHUB_ITEST_REPO"
	EnvBaseURL       = "GITHUB_ITEST_BASE_URL"
)

// DefaultBaseURL is the GitHub REST API root used when GITHUB_ITEST_BASE_URL
// is unset.
const DefaultBaseURL = "https://api.github.com"

// QueueCIPath is where InstallQueueCI commits the queue workflow.
const QueueCIPath = ".github/workflows/queue-ci.yml"

// QueueCICheck is the job name the queue workflow reports as its check run.
const QueueCICheck = "ci-gate"

// BreakCIFile is the file whose presence in a queue head fails QueueCICheck.
const BreakCIFile = "BREAK-CI"

const requestTimeout = 30 * time.Second

//go:embed testdata/queue-ci.yml
var queueCI []byte

// QueueCI returns the workflow InstallQueueCI commits.
func QueueCI() []byte {
	return bytes.Clone(queueCI)
}

// Env is the scratch repository a live run binds to.
type Env struct {
	Token         string
	ReviewerToken string
	Owner         string
	Repo          string
	BaseURL       string
}

// MissingEnvError names every variable a live run needs and did not find.
type MissingEnvError struct {
	Names []string
}

func (e *MissingEnvError) Error() string {
	return "the live GitHub train suite is not configured; set: " + strings.Join(e.Names, ", ")
}

// LoadEnv reads the scratch repository from the environment. It returns a
// *MissingEnvError naming every required variable that is unset; the
// caller fails the test on it, it never skips.
func LoadEnv() (Env, error) {
	env := Env{
		Token:         os.Getenv(EnvToken),
		ReviewerToken: os.Getenv(EnvReviewerToken),
		Owner:         os.Getenv(EnvOwner),
		Repo:          os.Getenv(EnvRepo),
		BaseURL:       strings.TrimRight(os.Getenv(EnvBaseURL), "/"),
	}
	missing := make([]string, 0, 4)
	if env.Token == "" {
		missing = append(missing, EnvToken)
	}
	if env.ReviewerToken == "" {
		missing = append(missing, EnvReviewerToken)
	}
	if env.Owner == "" {
		missing = append(missing, EnvOwner)
	}
	if env.Repo == "" {
		missing = append(missing, EnvRepo)
	}
	if len(missing) > 0 {
		return Env{}, &MissingEnvError{Names: missing}
	}
	if env.BaseURL == "" {
		env.BaseURL = DefaultBaseURL
	}
	return env, nil
}

// APIError is a non-2xx answer. It names the method, path and status, and
// GitHub's message when it sent one; it never carries a token.
type APIError struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	text := "githubitest: " + e.Method + " " + e.Path + ": status " + strconv.Itoa(e.Status)
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// TransportError is a request that got no answer.
type TransportError struct {
	Method string
	Path   string
	Err    error
}

func (e *TransportError) Error() string {
	return "githubitest: " + e.Method + " " + e.Path + ": " + e.Err.Error()
}

func (e *TransportError) Unwrap() error { return e.Err }

// DecodeError is an answer whose body did not parse.
type DecodeError struct {
	Method string
	Path   string
	Err    error
}

func (e *DecodeError) Error() string {
	return "githubitest: " + e.Method + " " + e.Path + ": decode: " + e.Err.Error()
}

func (e *DecodeError) Unwrap() error { return e.Err }

// EncodeError is a request body that did not marshal.
type EncodeError struct {
	Method string
	Path   string
	Err    error
}

func (e *EncodeError) Error() string {
	return "githubitest: " + e.Method + " " + e.Path + ": encode: " + e.Err.Error()
}

func (e *EncodeError) Unwrap() error { return e.Err }

// NamespaceError is a run namespace that could not draw its random suffix.
type NamespaceError struct {
	Err error
}

func (e *NamespaceError) Error() string {
	return "githubitest: namespace: " + e.Err.Error()
}

func (e *NamespaceError) Unwrap() error { return e.Err }

// Scratch drives one run against the scratch repository. It records every
// branch and pull request it makes, or is told about through Track*, so
// Cleanup can remove them.
type Scratch struct {
	env       Env
	client    *http.Client
	namespace string

	mu       sync.Mutex
	branches []string
	pulls    []int
}

// New returns a Scratch over env. A nil client gets a default with a
// bounded timeout.
func New(env Env, client *http.Client) (*Scratch, error) {
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return nil, &NamespaceError{Err: err}
	}
	ns := "itest-" + strconv.FormatInt(time.Now().Unix(), 10) + "-" + hex.EncodeToString(suffix)
	return &Scratch{env: env, client: client, namespace: ns}, nil
}

// Namespace is unique per run and safe in a branch name.
func (s *Scratch) Namespace() string {
	return s.namespace
}

// TrackBranch records a branch made outside Scratch (by the daemon) for
// Cleanup.
func (s *Scratch) TrackBranch(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.branches = append(s.branches, name)
}

// TrackPullRequest records a pull request opened outside Scratch for
// Cleanup.
func (s *Scratch) TrackPullRequest(number int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pulls = append(s.pulls, number)
}

// CutBase creates branch name from the repository's default branch.
func (s *Scratch) CutBase(ctx context.Context, name string) error {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath(""), nil, &repo); err != nil {
		return err
	}
	return s.CutBranch(ctx, name, repo.DefaultBranch)
}

// CutBranch creates branch name at the head of from.
func (s *Scratch) CutBranch(ctx context.Context, name, from string) error {
	sha, err := s.BranchHead(ctx, from)
	if err != nil {
		return err
	}
	body := map[string]string{"ref": "refs/heads/" + name, "sha": sha}
	if err := s.do(ctx, s.env.Token, http.MethodPost, s.repoPath("/git/refs"), body, nil); err != nil {
		return err
	}
	s.TrackBranch(name)
	return nil
}

// BranchHead returns the commit at the head of branch.
func (s *Scratch) BranchHead(ctx context.Context, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath("/git/ref/heads/"+escapePath(branch)), nil, &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// BranchExists reports whether branch is on the repository.
func (s *Scratch) BranchExists(ctx context.Context, branch string) (bool, error) {
	_, err := s.BranchHead(ctx, branch)
	if err == nil {
		return true, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

// CommitFile creates path on branch with content and returns the new
// commit's SHA.
func (s *Scratch) CommitFile(ctx context.Context, branch, path, content, message string) (string, error) {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	var answer struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := s.do(ctx, s.env.Token, http.MethodPut, s.repoPath("/contents/"+escapePath(path)), body, &answer); err != nil {
		return "", err
	}
	return answer.Commit.SHA, nil
}

// InstallQueueCI commits the embedded queue workflow to base and returns
// the commit's SHA.
func (s *Scratch) InstallQueueCI(ctx context.Context, base string) (string, error) {
	return s.CommitFile(ctx, base, QueueCIPath, string(queueCI), "ci: queue gate for the live train e2e")
}

// OpenPR opens a pull request from head into base and returns its number.
func (s *Scratch) OpenPR(ctx context.Context, head, base, title string) (int, error) {
	body := map[string]string{"title": title, "head": head, "base": base}
	var answer struct {
		Number int `json:"number"`
	}
	if err := s.do(ctx, s.env.Token, http.MethodPost, s.repoPath("/pulls"), body, &answer); err != nil {
		return 0, err
	}
	s.TrackPullRequest(answer.Number)
	return answer.Number, nil
}

// Approve submits an approving review of number at sha as the reviewer.
func (s *Scratch) Approve(ctx context.Context, number int, sha string) error {
	body := map[string]string{"commit_id": sha, "event": "APPROVE"}
	return s.do(ctx, s.env.ReviewerToken, http.MethodPost, s.repoPath("/pulls/"+strconv.Itoa(number)+"/reviews"), body, nil)
}

// PullRequest is the part of a pull request the train tests assert on.
type PullRequest struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// GetPullRequest reads one pull request.
func (s *Scratch) GetPullRequest(ctx context.Context, number int) (PullRequest, error) {
	var pr PullRequest
	err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath("/pulls/"+strconv.Itoa(number)), nil, &pr)
	return pr, err
}

// ListPullRequests lists the pull requests, open and closed, into base.
func (s *Scratch) ListPullRequests(ctx context.Context, base string) ([]PullRequest, error) {
	query := url.Values{"state": {"all"}, "base": {base}, "per_page": {"100"}}
	var prs []PullRequest
	err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath("/pulls?"+query.Encode()), nil, &prs)
	return prs, err
}

// CheckRun is one check run on a commit.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// CheckRuns lists the check runs on sha.
func (s *Scratch) CheckRuns(ctx context.Context, sha string) ([]CheckRun, error) {
	var answer struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath("/commits/"+sha+"/check-runs?per_page=100"), nil, &answer)
	return answer.CheckRuns, err
}

// CommitParents returns the parent SHAs of sha: one for a squash commit.
func (s *Scratch) CommitParents(ctx context.Context, sha string) ([]string, error) {
	var answer struct {
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := s.do(ctx, s.env.Token, http.MethodGet, s.repoPath("/commits/"+sha), nil, &answer); err != nil {
		return nil, err
	}
	parents := make([]string, 0, len(answer.Parents))
	for _, p := range answer.Parents {
		parents = append(parents, p.SHA)
	}
	return parents, nil
}

// Cleanup closes every tracked pull request still open and deletes every
// tracked branch still present. It attempts all of them and returns every
// failure joined into one error.
func (s *Scratch) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	pulls := append([]int(nil), s.pulls...)
	branches := append([]string(nil), s.branches...)
	s.mu.Unlock()

	var failures []error
	for _, number := range pulls {
		if err := s.closePullRequest(ctx, number); err != nil {
			failures = append(failures, err)
		}
	}
	for i := len(branches) - 1; i >= 0; i-- {
		if err := s.deleteBranch(ctx, branches[i]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Scratch) closePullRequest(ctx context.Context, number int) error {
	pr, err := s.GetPullRequest(ctx, number)
	if err != nil {
		return err
	}
	if pr.State != "open" {
		return nil
	}
	body := map[string]string{"state": "closed"}
	return s.do(ctx, s.env.Token, http.MethodPatch, s.repoPath("/pulls/"+strconv.Itoa(number)), body, nil)
}

// deleteBranch treats a branch already gone (404, or GitHub's 422
// "Reference does not exist") as deleted.
func (s *Scratch) deleteBranch(ctx context.Context, branch string) error {
	err := s.do(ctx, s.env.Token, http.MethodDelete, s.repoPath("/git/refs/heads/"+escapePath(branch)), nil, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusUnprocessableEntity) {
		return nil
	}
	return err
}

func (s *Scratch) repoPath(suffix string) string {
	return "/repos/" + url.PathEscape(s.env.Owner) + "/" + url.PathEscape(s.env.Repo) + suffix
}

// escapePath escapes each segment of a slash-separated path, keeping the
// slashes GitHub reads as separators in ref and content paths.
func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

// do sends one request. A nil body sends none; a nil out discards the
// answer. The path in every error excludes the base URL and the query.
func (s *Scratch) do(ctx context.Context, token, method, path string, body, out any) error {
	errPath, _, _ := strings.Cut(path, "?")
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return &EncodeError{Method: method, Path: errPath, Err: err}
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.env.BaseURL+path, reader)
	if err != nil {
		return &TransportError{Method: method, Path: errPath, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return &TransportError{Method: method, Path: errPath, Err: err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TransportError{Method: method, Path: errPath, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var answer struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &answer) != nil {
			answer.Message = ""
		}
		return &APIError{Method: method, Path: errPath, Status: resp.StatusCode, Message: answer.Message}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &DecodeError{Method: method, Path: errPath, Err: err}
	}
	return nil
}
