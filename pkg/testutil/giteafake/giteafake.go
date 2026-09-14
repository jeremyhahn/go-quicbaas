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

// Package giteafake serves a STATEFUL in-memory Gitea over httptest, for the
// tests that need a forge which remembers.
//
// WHY A SERVER AND NOT A DOUBLE. The properties the onboarding surface has to
// hold — run it twice and there is one webhook, delete the webhook by hand and
// the next run puts it back, run it over a repository that already has the
// labels and nothing changes — are properties of a SEQUENCE of calls against
// something that keeps state. A double that answers each call with a canned
// value agrees with whatever the implementation did and proves nothing; a
// responder keyed by path is the same thing with extra steps.
//
// WHAT IT MODELS ON PURPOSE, because each of these has broken a naive
// implementation:
//
//   - labels are PAGINATED, at the page size a caller asks for
//   - a webhook's events are EXPANDED into their family on write, so a
//     read-back comparing sets for equality fails against a correct write
//   - the contents route answers with an ARRAY for a directory and an OBJECT
//     for a file
//   - an absent contents path and an unprotected branch are both 404
//   - creating a label that already exists is a 409
//   - pull requests are PAGINATED and filtered by state, so a caller that
//     stops at the first page silently misses the rest
//   - a commit's statuses are APPENDED, newest last, and the review floor's
//     whole question — has this head been judged — is read from them
//
// The wire shapes here are restated rather than shared with the adapter under
// test. That is deliberate: a fake built out of the adapter's own structs
// cannot disagree with it, and disagreeing is the fake's whole job.
//
// MEASURED AGAINST GITEA 1.27.1 on 2026-08-20 for the event expansion; the
// rest is the documented API surface.
package giteafake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultPageSize is the page size the listing routes fall back to when a
// caller asks for none.
const DefaultPageSize = 50

// Label is a repository label as Gitea reports it.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Hook is a repository webhook as Gitea reports it.
type Hook struct {
	ID     int64             `json:"id"`
	Type   string            `json:"type"`
	Active bool              `json:"active"`
	Events []string          `json:"events"`
	Config map[string]string `json:"config"`
}

// Content is one entry of Gitea's contents listing.
type Content struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// PullRequest is a pull request as Gitea reports it in a listing.
type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Head   Commit `json:"head"`
	Base   Commit `json:"base"`
	URL    string `json:"html_url"`
}

// Commit is the head or base end of a pull request.
type Commit struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// CommitStatus is one status against a commit, which is what Gitea Actions
// and every other CI attached to the repository publish.
type CommitStatus struct {
	ID          int64     `json:"id"`
	State       string    `json:"status"`
	Context     string    `json:"context"`
	Description string    `json:"description"`
	TargetURL   string    `json:"target_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BranchProtection is a protected branch's rule as Gitea reports it.
type BranchProtection struct {
	BranchName          string   `json:"branch_name"`
	RuleName            string   `json:"rule_name"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
}

// eventFamilies is Gitea's expansion of a coarse event into the set it
// actually stores.
//
// An independent statement of the same measurement the adapter carries. Two
// copies is the point here rather than the usual defect: if the fake expanded
// with the adapter's own map, a read-back that compared the two would be
// comparing a value with itself.
var eventFamilies = map[string][]string{
	"issues": {
		"issues", "issue_assign", "issue_comment", "issue_label", "issue_milestone",
	},
	"pull_request": {
		"pull_request", "pull_request_assign", "pull_request_comment",
		"pull_request_label", "pull_request_milestone", "pull_request_review",
		"pull_request_review_request", "pull_request_sync",
	},
}

// ExpandEvents returns the events Gitea stores for a requested set.
func ExpandEvents(requested []string) []string {
	expanded := make(map[string]bool, len(requested)*4)
	for _, event := range requested {
		key := strings.ToLower(strings.TrimSpace(event))
		family, ok := eventFamilies[key]
		if !ok {
			family = []string{key}
		}
		for _, member := range family {
			expanded[member] = true
		}
	}
	out := make([]string, 0, len(expanded))
	for event := range expanded {
		out = append(out, event)
	}
	sort.Strings(out)
	return out
}

// Server is one in-memory Gitea instance serving one repository.
type Server struct {
	t      *testing.T
	mu     sync.Mutex
	server *httptest.Server
	prefix string

	labels     []Label
	hooks      []Hook
	contents   map[string][]Content
	protection map[string]BranchProtection
	pulls      []PullRequest
	statuses   map[string][]CommitStatus

	nextLabelID  int64
	nextHookID   int64
	nextStatusID int64

	calls    map[string]int
	failNext map[string]bool
	forced   map[string]forcedStatus
}

// forcedStatus is a status a route answers until it is cleared, for the
// failures a single staged 500 cannot express — an account the forge refuses,
// a repository it will not show.
type forcedStatus struct {
	code    int
	message string
}

// New starts an instance serving owner/repo and returns it. The server is
// closed when the test ends.
func New(t *testing.T, owner, repo string) *Server {
	t.Helper()
	server := &Server{
		t:          t,
		prefix:     "/api/v1/repos/" + owner + "/" + repo,
		contents:   map[string][]Content{},
		protection: map[string]BranchProtection{},
		statuses:   map[string][]CommitStatus{},
		calls:      map[string]int{},
		failNext:   map[string]bool{},
		forced:     map[string]forcedStatus{},
	}
	server.server = httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(server.server.Close)
	return server
}

// ForceStatus makes one route answer code/message until ClearStatus, for a
// refusal that persists rather than a single staged failure: an account without
// admin rights answers 403 to every attempt, not only the first.
//
// key is method and path suffix, e.g. ForceStatus("GET", "/hooks", 403, "…").
func (s *Server) ForceStatus(method, suffix string, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forced[method+" "+suffix] = forcedStatus{code: code, message: message}
}

// ClearStatus removes a ForceStatus override.
func (s *Server) ClearStatus(method, suffix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.forced, method+" "+suffix)
}

// APIURL is the instance's API root, for a GiteaConfig's BaseURL.
func (s *Server) APIURL() string { return s.server.URL + "/api/v1" }

// URL is the instance's origin.
func (s *Server) URL() string { return s.server.URL }

// serve routes one request onto the instance's state.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !strings.HasPrefix(r.URL.Path, s.prefix) {
		s.fail(w, http.StatusNotFound, "unknown repository")
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, s.prefix)
	key := r.Method + " " + suffix
	s.calls[key]++
	if s.failNext[key] {
		delete(s.failNext, key)
		s.fail(w, http.StatusInternalServerError, "staged failure for "+key)
		return
	}
	if forced, ok := s.forced[key]; ok {
		s.fail(w, forced.code, forced.message)
		return
	}

	switch {
	case suffix == "/labels" && r.Method == http.MethodGet:
		s.listLabels(w, r)
	case suffix == "/labels" && r.Method == http.MethodPost:
		s.createLabel(w, r)
	case suffix == "/hooks" && r.Method == http.MethodGet:
		s.writeJSON(w, http.StatusOK, s.hooks)
	case suffix == "/hooks" && r.Method == http.MethodPost:
		s.createHook(w, r)
	case strings.HasPrefix(suffix, "/hooks/") && r.Method == http.MethodPatch:
		s.patchHook(w, r, strings.TrimPrefix(suffix, "/hooks/"))
	case strings.HasPrefix(suffix, "/hooks/") && r.Method == http.MethodDelete:
		s.deleteHook(w, strings.TrimPrefix(suffix, "/hooks/"))
	case suffix == "/pulls" && r.Method == http.MethodGet:
		s.listPulls(w, r)
	case strings.HasPrefix(suffix, "/pulls/") && r.Method == http.MethodGet:
		s.getPull(w, strings.TrimPrefix(suffix, "/pulls/"))
	case strings.HasPrefix(suffix, "/commits/") && strings.HasSuffix(suffix, "/statuses") && r.Method == http.MethodGet:
		s.listStatuses(w, strings.TrimSuffix(strings.TrimPrefix(suffix, "/commits/"), "/statuses"))
	case strings.HasPrefix(suffix, "/statuses/") && r.Method == http.MethodPost:
		s.createStatus(w, r, strings.TrimPrefix(suffix, "/statuses/"))
	case strings.HasPrefix(suffix, "/contents"):
		s.listContents(w, strings.Trim(strings.TrimPrefix(suffix, "/contents"), "/"))
	case strings.HasPrefix(suffix, "/branch_protections/"):
		s.getProtection(w, strings.TrimPrefix(suffix, "/branch_protections/"))
	default:
		s.fail(w, http.StatusNotFound, "no route for "+key)
	}
}

func (s *Server) listLabels(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = DefaultPageSize
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(s.labels) {
		s.writeJSON(w, http.StatusOK, []Label{})
		return
	}
	end := start + limit
	if end > len(s.labels) {
		end = len(s.labels)
	}
	s.writeJSON(w, http.StatusOK, s.labels[start:end])
}

// createLabel adds a label, refusing a duplicate name the way Gitea does.
func (s *Server) createLabel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, http.StatusBadRequest, "undecodable body")
		return
	}
	for _, held := range s.labels {
		if strings.EqualFold(held.Name, body.Name) {
			s.fail(w, http.StatusConflict, "label already exists")
			return
		}
	}
	s.nextLabelID++
	label := Label{
		ID: s.nextLabelID, Name: body.Name,
		Color:       strings.TrimPrefix(body.Color, "#"),
		Description: body.Description,
	}
	s.labels = append(s.labels, label)
	s.writeJSON(w, http.StatusCreated, label)
}

func (s *Server) createHook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Active bool              `json:"active"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, http.StatusBadRequest, "undecodable body")
		return
	}
	s.nextHookID++
	hook := Hook{
		ID: s.nextHookID, Type: "gitea", Active: body.Active,
		Events: ExpandEvents(body.Events), Config: body.Config,
	}
	s.hooks = append(s.hooks, hook)
	s.writeJSON(w, http.StatusCreated, hook)
}

func (s *Server) patchHook(w http.ResponseWriter, r *http.Request, rawID string) {
	var body struct {
		Active bool              `json:"active"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, http.StatusBadRequest, "undecodable body")
		return
	}
	id, _ := strconv.ParseInt(rawID, 10, 64)
	for i := range s.hooks {
		if s.hooks[i].ID != id {
			continue
		}
		s.hooks[i].Active = body.Active
		s.hooks[i].Events = ExpandEvents(body.Events)
		s.hooks[i].Config = body.Config
		s.writeJSON(w, http.StatusOK, s.hooks[i])
		return
	}
	s.fail(w, http.StatusNotFound, "no such hook")
}

func (s *Server) deleteHook(w http.ResponseWriter, rawID string) {
	id, _ := strconv.ParseInt(rawID, 10, 64)
	for i := range s.hooks {
		if s.hooks[i].ID != id {
			continue
		}
		s.hooks = append(s.hooks[:i], s.hooks[i+1:]...)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.fail(w, http.StatusNotFound, "no such hook")
}

// listContents serves a directory as an array and a file as an object, and
// 404s a path the repository does not have.
// listPulls answers the pull-request listing, paginated and filtered by state
// the way Gitea does. A caller that reads only the first page sees a prefix,
// which is exactly the failure the adapter pages against.
func (s *Server) listPulls(w http.ResponseWriter, r *http.Request) {
	state := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	matching := make([]PullRequest, 0, len(s.pulls))
	for _, pull := range s.pulls {
		if state == "" || state == "all" || strings.EqualFold(pull.State, state) {
			matching = append(matching, pull)
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = DefaultPageSize
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(matching) {
		s.writeJSON(w, http.StatusOK, []PullRequest{})
		return
	}
	end := start + limit
	if end > len(matching) {
		end = len(matching)
	}
	s.writeJSON(w, http.StatusOK, matching[start:end])
}

// getPull answers one pull request, and 404s for a number the repository does
// not have — which is what makes a dispatcher's re-read a real check rather
// than a formality.
func (s *Server) getPull(w http.ResponseWriter, rawNumber string) {
	number, err := strconv.Atoi(strings.TrimSpace(rawNumber))
	if err != nil {
		s.fail(w, http.StatusBadRequest, "a pull request is addressed by number")
		return
	}
	for _, pull := range s.pulls {
		if pull.Number == number {
			s.writeJSON(w, http.StatusOK, pull)
			return
		}
	}
	s.fail(w, http.StatusNotFound, "no such pull request")
}

// listStatuses answers a commit's statuses, oldest first, which is how a
// forge reports them and why the adapter takes the newest per context.
func (s *Server) listStatuses(w http.ResponseWriter, sha string) {
	s.writeJSON(w, http.StatusOK, append([]CommitStatus{}, s.statuses[sha]...))
}

// createStatus APPENDS a status rather than replacing the one under the same
// context, which is what Gitea does: a context's history stays readable and
// the newest entry is the operative one.
func (s *Server) createStatus(w http.ResponseWriter, r *http.Request, sha string) {
	var body struct {
		State       string `json:"state"`
		Context     string `json:"context"`
		Description string `json:"description"`
		TargetURL   string `json:"target_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, http.StatusBadRequest, "undecodable body")
		return
	}
	if strings.TrimSpace(sha) == "" || strings.TrimSpace(body.Context) == "" {
		s.fail(w, http.StatusBadRequest, "a status names a commit and a context")
		return
	}
	s.nextStatusID++
	status := CommitStatus{
		ID: s.nextStatusID, State: body.State, Context: body.Context,
		Description: body.Description, TargetURL: body.TargetURL,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.statuses[sha] = append(s.statuses[sha], status)
	s.writeJSON(w, http.StatusCreated, status)
}

func (s *Server) listContents(w http.ResponseWriter, path string) {
	entries, ok := s.contents[path]
	if !ok {
		s.fail(w, http.StatusNotFound, "no such path")
		return
	}
	if len(entries) == 1 && entries[0].Type == "file" && entries[0].Path == path {
		s.writeJSON(w, http.StatusOK, entries[0])
		return
	}
	s.writeJSON(w, http.StatusOK, entries)
}

func (s *Server) getProtection(w http.ResponseWriter, branch string) {
	rule, ok := s.protection[branch]
	if !ok {
		s.fail(w, http.StatusNotFound, "branch is not protected")
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.t.Errorf("giteafake could not encode a response: %v", err)
	}
}

// fail answers Gitea's refusal shape. The message is ENCODED rather than
// interpolated: it carries a caller-supplied path, and a hand-built JSON
// string would both break on a quote and taint the body.
func (s *Server) fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"message": message}); err != nil {
		s.t.Errorf("giteafake could not write a refusal: %v", err)
	}
}

// =============================================================================
// State a test seeds and reads
// =============================================================================

// PutLabel seeds a label as a human created it.
func (s *Server) PutLabel(name, color, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextLabelID++
	s.labels = append(s.labels, Label{
		ID: s.nextLabelID, Name: name, Color: color, Description: description,
	})
}

// LabelNames returns the instance's label names, sorted.
func (s *Server) LabelNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.labels))
	for _, label := range s.labels {
		names = append(names, label.Name)
	}
	sort.Strings(names)
	return names
}

// Label returns a label by name, case-insensitively.
func (s *Server) Label(name string) (Label, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, held := range s.labels {
		if strings.EqualFold(held.Name, name) {
			return held, true
		}
	}
	return Label{}, false
}

// PutHook seeds a delivery as another node configured it.
func (s *Server) PutHook(hookURL string, active bool, events ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextHookID++
	s.hooks = append(s.hooks, Hook{
		ID: s.nextHookID, Type: "gitea", Active: active,
		Events: ExpandEvents(events),
		Config: map[string]string{"url": hookURL, "content_type": "json"},
	})
}

// HookURLs returns the instance's configured delivery URLs, sorted.
func (s *Server) HookURLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	urls := make([]string, 0, len(s.hooks))
	for _, hook := range s.hooks {
		urls = append(urls, hook.Config["url"])
	}
	sort.Strings(urls)
	return urls
}

// DropHooks removes every delivery, standing in for an operator deleting it
// in the forge's UI.
func (s *Server) DropHooks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = nil
}

// DeactivateHooks switches every delivery off without removing it.
func (s *Server) DeactivateHooks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.hooks {
		s.hooks[i].Active = false
	}
}

// PutDir seeds a directory listing.
func (s *Server) PutDir(dir string, names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]Content, 0, len(names))
	for _, name := range names {
		entries = append(entries, Content{Name: name, Path: dir + "/" + name, Type: "file"})
	}
	s.contents[dir] = entries
}

// PutFile seeds a single file at path, which the contents route answers as an
// object rather than an array.
func (s *Server) PutFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	segments := strings.Split(path, "/")
	s.contents[path] = []Content{{Name: segments[len(segments)-1], Path: path, Type: "file"}}
}

// PutPullRequest seeds an open pull request on headSHA. A test that needs a
// closed or draft one takes the returned record apart with PutPullRequestRaw.
func (s *Server) PutPullRequest(number int, branch, headSHA string) {
	s.PutPullRequestRaw(PullRequest{
		Number: number, Title: "change " + strconv.Itoa(number), State: "open",
		Head: Commit{Ref: branch, SHA: headSHA}, Base: Commit{Ref: "main"},
		URL: s.URL() + "/pulls/" + strconv.Itoa(number),
	})
}

// PutPullRequestRaw seeds a pull request exactly as given, for the states a
// convenience constructor would hide: closed, merged, draft.
func (s *Server) PutPullRequestRaw(pull PullRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.pulls {
		if s.pulls[i].Number == pull.Number {
			s.pulls[i] = pull
			return
		}
	}
	s.pulls = append(s.pulls, pull)
}

// PutStatus seeds one commit status, as a CI publishes it.
func (s *Server) PutStatus(sha, statusContext, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextStatusID++
	s.statuses[sha] = append(s.statuses[sha], CommitStatus{
		ID: s.nextStatusID, State: state, Context: statusContext,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

// StatusContexts returns the contexts recorded against a commit, sorted, so a
// test can assert what a head carries without reaching into the instance.
func (s *Server) StatusContexts(sha string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	contexts := make([]string, 0, len(s.statuses[sha]))
	for _, status := range s.statuses[sha] {
		contexts = append(contexts, status.Context)
	}
	sort.Strings(contexts)
	return contexts
}

// PutProtection seeds a branch's protection rule.
func (s *Server) PutProtection(branch string, statusChecks bool, contexts ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protection[branch] = BranchProtection{
		BranchName: branch, RuleName: branch,
		EnableStatusCheck: statusChecks, StatusCheckContexts: contexts,
	}
}

// Calls returns how many times a "METHOD path-suffix" has been served, where
// the suffix is the part after /api/v1/repos/{owner}/{repo}.
func (s *Server) Calls(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[key]
}

// FailOnce stages a 500 for the next "METHOD path-suffix", which is the one
// thing a live instance will not produce on demand.
func (s *Server) FailOnce(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[key] = true
}
