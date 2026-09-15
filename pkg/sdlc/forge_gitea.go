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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// Gitea adapter
// =============================================================================
//
// The module's first forge, and the one it runs on. COMPLETE as of this
// commit: every method Gitea's API supports is implemented, so the adapter now
// asserts ForgeBackend and registers itself. The assertion below is what makes
// "complete" a compiler fact rather than a claim — it did not compile until the
// last method existed.
//
// Written against Gitea's own API rather than a client library: no maintained
// Go SDK matches the version this module targets, and the surface used here is
// small enough that a dependency would be more to keep in step than the calls
// themselves.

// giteaDefaultTimeout bounds a single API call.
//
// A forge that stops answering must not hold a worker forever; the reconciler
// will come back. It is generous because Gitea under CI load is slow, not
// because a long wait is useful.
const giteaDefaultTimeout = 30 * time.Second

// giteaDraftPrefix is how Gitea marks a pull request as a draft: there is no
// draft field on the create API, only this title convention. The prefix is
// server-configurable (repository.PULL_REQUEST_WORK_IN_PROGRESS_PREFIXES) and
// this is the shipped default; CreatePullRequest verifies by read-back rather
// than trusting it.
const giteaDraftPrefix = "WIP:"

// maxLabelPages bounds label pagination.
//
// A bound rather than an unbounded loop: a server that always returns a full
// page would otherwise spin forever. At the default page size this covers a
// vocabulary far larger than any real project's, and exhausting it means
// something is wrong with the server rather than with the repository.
const maxLabelPages = 20

// maxIssuePages bounds an UNBOUNDED issue listing.
//
// It applies only when the caller set no Limit, because a caller that set one
// gets exactly the pages that limit needs and can never reach a ceiling. So
// this is not a cap on how large a repository may be — it is the point past
// which "the forge never said that is all" stops being credible as a large
// repository and starts being credible as a forge stuck in a loop. A thousand
// pages of fifty is fifty thousand open issues without a single short page.
const maxIssuePages = 1000

// errIssuePagesExhausted reports an unbounded listing that never ended.
//
// A fault rather than a truncation, and reported as one. A short page is how a
// forge says "that is all"; a forge that never sends one is not answering the
// question asked, and returning the pages collected so far would present a
// prefix as a complete listing — the exact failure paging was added to remove.
//
// The message names the remedy because the caller has one: asking for a bounded
// listing turns an unanswerable question into an answerable one.
var errIssuePagesExhausted = errors.New(
	"issue listing never returned a short page; set IssueFilter.Limit to bound it")

// errCommentPagesExhausted reports a discussion listing that never ended.
//
// It names no remedy because the caller has none to offer: the discussion is
// read whole or not at all — a bound would silently drop the newest comments,
// which is where an answer lives. A thread this long is a forge fault or a
// repository nobody should be dispatching from, and both want a human.
var errCommentPagesExhausted = errors.New(
	"issue comment listing never returned a short page")

// issuePageBound is how many pages a listing may fetch before the adapter
// concludes the forge is not going to stop.
//
// THE CALLER'S LIMIT DECIDES IT WHEREVER THERE IS ONE, and that is a fix for a
// real defect rather than a tidy-up. Under a fixed ceiling, a repository large
// enough to fill every page up to the cap is indistinguishable from a forge
// stuck in a loop, so a caller asking for more issues than the ceiling allowed
// received ErrBackend instead of its issues — "this project has a lot of open
// issues" reported as "this project does not mirror". A bounded listing needs
// exactly ceil(limit/pageSize) pages to satisfy itself and can never reach a
// ceiling, so it can never be misread that way.
//
// Only an UNBOUNDED listing keeps the absolute ceiling, because only an
// unbounded listing has no other way to terminate.
func issuePageBound(limit int) int {
	if limit <= 0 {
		return maxIssuePages
	}
	return (limit + giteaDefaultPageSize - 1) / giteaDefaultPageSize
}

// giteaDefaultPageSize is the page size used when a filter names no limit.
const giteaDefaultPageSize = 50

// GiteaConfig configures the adapter.
type GiteaConfig struct {
	// BaseURL is the API root, including the version segment — for example
	// "http://gitea.internal:3001/api/v1".
	BaseURL string

	// Token authenticates every call. It is a VALUE here because the adapter
	// has to send it; callers resolve it from the SecretsService and never
	// store it on a record. It is never logged, never placed in an error, and
	// never rendered by String.
	Token string

	// Custody is the password-equivalent used to mint and revoke tokens.
	//
	// Optional: an adapter without it serves every workflow call normally and
	// refuses only token custody. Resolved from the SecretsService by the
	// caller, like Token.
	Custody CustodyCredential

	// HTTPClient is optional. A caller supplying one owns its timeout;
	// otherwise the adapter builds one with giteaDefaultTimeout.
	HTTPClient *http.Client
}

// GiteaBackend talks to one Gitea instance.
type GiteaBackend struct {
	// bodySeam, when non-nil (tests only), observes each response body
	// buffer AFTER do's deferred wipe — the seam that makes the wipe
	// assertable. Production leaves it nil.
	bodySeam func(body []byte)

	baseURL string
	token   string
	custody CustodyCredential
	client  *http.Client
}

// CustodyCredential is the password-equivalent the provisioning plane holds so
// tokens can be minted and revoked.
//
// It is a VALUE, resolved from the SecretsService by the caller at
// construction, for the same reason the adapter's token is: the adapter has to
// send it, and an adapter that reached into custody itself could not be tested
// without a secret store. An empty credential is a legitimate configuration —
// it means this forge's tokens are managed by hand, and rotation refuses rather
// than half-working.
type CustodyCredential struct {
	// Username is the account whose password-equivalent this is.
	Username string

	// Password is the password or password-equivalent. Never logged, never
	// placed in an error, never rendered by String.
	Password string
}

// NewGiteaBackend builds the adapter, failing closed on missing configuration.
//
// An adapter without a base URL or a token cannot do anything except fail on
// first use, and failing at construction says so where an operator is looking.
func NewGiteaBackend(cfg GiteaConfig) (*GiteaBackend, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		return nil, &InvalidInputError{Field: "base_url", Message: "must not be empty"}
	}
	if _, err := url.Parse(base); err != nil {
		return nil, &InvalidInputError{Field: "base_url", Message: "is not a valid URL"}
	}
	if cfg.Token == "" {
		return nil, &InvalidInputError{
			Field:   "token",
			Message: "must not be empty; an unauthenticated adapter can only fail on first use",
		}
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: giteaDefaultTimeout}
	}
	return &GiteaBackend{baseURL: base, token: cfg.Token, custody: cfg.Custody, client: client}, nil
}

// Compile-time proof the adapter is complete. It fails to build if a method is
// missing, which is the whole reason registration waited for it.
var _ ForgeBackend = (*GiteaBackend)(nil)

// Registered as a FACTORY: each project brings its own base URL and its own
// token, so an adapter is built per project rather than shared.
func init() {
	RegisterForgeFactory(ForgeGitea, func(cfg ForgeConfig, creds ForgeCredentials) (ForgeBackend, error) {
		return NewGiteaBackend(GiteaConfig{
			BaseURL: cfg.BaseURL,
			Token:   creds.Token,
			Custody: creds.Custody,
		})
	})
}

// Type reports the platform this adapter serves.
func (g *GiteaBackend) Type() ForgeType { return ForgeGitea }

// Capabilities reports what GITEA can do, measured against its API rather than
// against this adapter's progress.
//
// The two false flags are the ones that change how the engine behaves:
//
//   - CancelCheckRuns: Gitea publishes no route to cancel a running Actions
//     job. Supersession therefore cannot stop a displaced run, which is why a
//     SupersessionRecord marks it an orphan instead of claiming a cancellation.
//   - ReliableLabelEvents: Gitea's label deliveries are unreliable. Harmless to
//     correctness, since labels are display output and no decision reads them,
//     but a reconciler that assumed delivery would let the display drift from
//     reality indefinitely.
//
// RerunCheckRuns is true, and it is the one half of the pair Gitea does have:
// POST .../actions/runs/{run}/rerun-failed-jobs restarts the failed jobs of a
// FINISHED run. Only the failed ones, deliberately — the green jobs of that run
// are answers about the same head and re-running them would buy nothing and
// cost a runner slot each.
//
// AdditiveLabels is true: Gitea publishes POST .../labels to add and DELETE
// .../labels/{id} to remove one, alongside the PUT that replaces the set. That
// is what lets display sync touch only the module's own labels and leave a
// human's alone, rather than computing a replacement from a read that may
// already be stale.
func (g *GiteaBackend) Capabilities() ForgeCapabilities {
	return ForgeCapabilities{
		CancelCheckRuns:     false,
		RerunCheckRuns:      true,
		CheckRunLogs:        true,
		ReliableLabelEvents: false,
		AdditiveLabels:      true,
		DraftPullRequests:   true,
		RequiredReviewers:   true,
		SquashMerge:         true,
		DeleteBranchOnMerge: true,

		// Gitea's branch_protections rule carries enable_status_check and
		// status_check_contexts, so "this branch must require
		// quicbaas/review" is a question the platform can answer.
		BranchProtectionContexts: true,
		SubmitReviews:            true,
		CommitStatuses:           true,
	}
}

// =============================================================================
// Transport
// =============================================================================

// scrubCredentials removes any credential this call could have sent from
// bytes that are about to become an error message. BYTE-WISE (review 991):
// the old string round-trips minted unwipeable copies of secret-bearing
// regions; this one allocates only []byte generations and LOOP-ZEROES every
// intermediate it creates — the needle copies of the adapter credentials and
// each partially-scrubbed generation — before returning. The input body is
// NOT wiped here (the caller owns it and wipes it on every exit); the
// returned slice contains no secrets and is safe to outlive the call.
func (g *GiteaBackend) scrubCredentials(body []byte, subjectToken []byte) []byte {
	redacted := []byte("[redacted]")
	scrubbed := body
	scrub := func(secret []byte) {
		if len(secret) == 0 {
			return
		}
		next := bytes.ReplaceAll(scrubbed, secret, redacted)
		// bytes.ReplaceAll always returns a fresh slice, so a superseded
		// generation this function allocated is wiped the moment it is
		// replaced. The original body (generation zero) is the caller's.
		if len(scrubbed) != 0 && &scrubbed[0] != &body[0] {
			for i := range scrubbed {
				scrubbed[i] = 0
			}
		}
		scrubbed = next
	}
	for _, cred := range []string{g.token, g.custody.Password} {
		if cred == "" {
			continue
		}
		needle := []byte(cred)
		scrub(needle)
		for i := range needle {
			needle[i] = 0
		}
	}
	scrub(subjectToken)
	if len(scrubbed) != 0 && &scrubbed[0] == &body[0] {
		// No replacement ever happened: hand back a copy so the caller's
		// wipe of body cannot reach into the error message.
		out := make([]byte, len(scrubbed))
		copy(out, scrubbed)
		return out
	}
	return scrubbed
}

// giteaRequest describes one API call.
type giteaRequest struct {
	method string
	path   string
	query  url.Values

	// body is marshalled as JSON when non-nil.
	body any

	// out receives the decoded response when non-nil.
	out any

	// raw receives the response body when non-nil, for the routes that answer
	// in something other than JSON. It is a SCRUBBED COPY — the adapter's own
	// buffer is zeroed on the way out, and a body that outlives the call must
	// carry no credential (see do).
	raw *[]byte

	// maxBody bounds what a response may cost in memory and what the caller
	// receives, keeping the LAST maxBody bytes of a longer one. Zero — every
	// existing call — reads the body whole, exactly as before.
	//
	// The tail rather than a prefix because the only route that sets this is
	// the job log, and a CI log's answer is at its end. do reads slightly past
	// the bound so the scrub cannot meet a half credential; the caller still
	// gets at most maxBody.
	maxBody int

	// operation names the call for error attribution.
	operation string

	// auth selects the credential. The zero value is the adapter's own token,
	// so every workflow call is unchanged by the custody plane existing.
	auth giteaAuth

	// subjectToken is the token to authenticate AS when auth is
	// authSubjectToken. Never logged, never placed in an error. A borrowed
	// byte view under the caller's no-retain contract: giteaRequest is
	// per-call and never stored, and do converts it to the string the HTTP
	// header requires only transiently (go-quicbaas#97 custody debt).
	subjectToken []byte
}

// giteaAuth selects which credential authenticates a request.
type giteaAuth int

const (
	// authAdapterToken uses the adapter's own token. The default.
	authAdapterToken giteaAuth = iota

	// authCustody uses the provisioning-held basic credential.
	//
	// Required for minting and revoking: Gitea's token endpoints are not
	// reachable with the token being replaced, so rotation runs on a
	// password-equivalent held by the provisioning plane.
	authCustody

	// authSubjectToken authenticates as a token supplied per call.
	//
	// This exists for verification and nothing else. Proving a replacement
	// works means using IT — checking with the credential that minted it would
	// prove only that minting succeeded.
	authSubjectToken
)

// do performs one API call and decodes its response.
//
// Status handling is explicit rather than "2xx is fine": Gitea distinguishes
// absent (404) from forbidden (403) from broken (5xx), and each means something
// different to a caller — a missing issue is ordinary, a forbidden one is a
// token problem an operator must fix, and a 5xx is worth retrying.
func (g *GiteaBackend) do(ctx context.Context, req giteaRequest) error {
	endpoint := g.baseURL + req.path
	if len(req.query) > 0 {
		endpoint += "?" + req.query.Encode()
	}

	var payload io.Reader
	if req.body != nil {
		encoded, err := json.Marshal(req.body)
		if err != nil {
			return backendError(req.operation+".encode", err)
		}
		payload = bytes.NewReader(encoded)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, endpoint, payload)
	if err != nil {
		return backendError(req.operation+".request", err)
	}
	// Gitea's own scheme. No credential is ever logged or reaches an error.
	switch req.auth {
	case authCustody:
		httpReq.SetBasicAuth(g.custody.Username, g.custody.Password)
	case authSubjectToken:
		// THE NET/HTTP BOUNDARY, request side: header values are strings by
		// API — there is no []byte path — so this conversion is constructed
		// at the last possible moment, inside the one call that sends it,
		// and nothing retains it beyond the request's round-trip. It is the
		// same floor already stated on the response side (do's body wipe
		// comment): what net/http holds — this header string included — is
		// memory this code never owns. Replacing it means an HTTP client
		// abstraction over secure buffers: tracked on go-quicbaas#97.
		httpReq.Header.Set("Authorization", "token "+string(req.subjectToken))
	case authAdapterToken:
		fallthrough
	default:
		// The default is deliberate rather than incidental: an unrecognised
		// mode must still authenticate as somebody, and the adapter's own
		// token is the least-surprising choice. Sending nothing would turn a
		// programming slip into an anonymous request the forge might accept.
		httpReq.Header.Set("Authorization", "token "+g.token)
	}
	httpReq.Header.Set("Accept", "application/json")
	if req.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return backendError(req.operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read the body before branching: an error response carries Gitea's own
	// message, and discarding it leaves an operator with a bare status code.
	//
	// READ PAST THE BOUND BY ONE CREDENTIAL'S WIDTH. The scrub below matches
	// whole credentials, so a token that straddles the cut would lose its head
	// to the tail read and its remainder would survive the scrub as an
	// unrecognisable suffix — a partial secret in a body that outlives the
	// call. Reading the margin and trimming AFTER the scrub means every
	// credential that touches the retained region is retained whole, and so
	// redacted whole.
	readBound := req.maxBody
	if readBound > 0 {
		readBound += g.credentialWidth(req.subjectToken)
	}
	rawBody, readErr := readBodyTail(resp.Body, readBound)
	if readErr != nil {
		return backendError(req.operation+".read", readErr)
	}
	// THE RAW BODY IS ADAPTER-OWNED, SECRET-BEARING MEMORY (review 991): a
	// mint response carries the token, and any response may echo a
	// credential back. It is LOOP-ZEROED on every exit — success, error,
	// panic — after the success decode or the scrub has copied out what
	// outlives the call. With this wipe the adapter's documented residue
	// shrinks to what net/http's internals transiently held while reading
	// the response — memory this code never owns, the genuine #97
	// secure-transport floor. bodySeam (tests only) observes the buffer
	// AFTER the wipe.
	defer func() {
		for i := range rawBody {
			rawBody[i] = 0
		}
		if g.bodySeam != nil {
			g.bodySeam(rawBody)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Entity: "gitea " + req.operation, ID: req.path}
	}
	// SCRUB THE CREDENTIALS OUT OF THE BODY before it can reach a typed
	// error. Gitea's message is relayed to operators and stored, and a
	// server or a proxy in front of it may echo the Authorization header or
	// the token back in its response. Redacting by VALUE is possible
	// precisely here, because this is the one place that holds all three
	// credentials — anywhere further out would be guessing at what a secret
	// looks like. Computed only on the error paths: the scrubbed copy is
	// secret-free and may outlive the call inside an error; the raw body
	// above never leaves it.
	var body []byte
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body = g.scrubCredentials(rawBody, req.subjectToken)
	}

	switch {
	case resp.StatusCode == http.StatusMethodNotAllowed && giteaIsNotReady(body):
		// Gitea computes a pull request's mergeability asynchronously and
		// answers 405 "Please try again later" until it has. Measured against
		// 1.27.1: a merge issued immediately after creating a pull request, or
		// after its head moves, hits this routinely.
		//
		// Reported as RETRYABLE rather than as a failure, because the merge was
		// neither performed nor refused and the same call succeeds moments
		// later. Folding it into a backend failure would make the module
		// abandon merges that were about to become possible.
		return &ForgeNotReadyError{
			Type:      ForgeGitea,
			Operation: req.operation,
			Subject:   req.path,
			Detail:    giteaMessage(body),
		}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return backendError(req.operation, &GiteaAPIError{
			Status:    resp.StatusCode,
			Operation: req.operation,
			Message:   giteaMessage(body),
		})
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return backendError(req.operation, &GiteaAPIError{
			Status:    resp.StatusCode,
			Operation: req.operation,
			Message:   giteaMessage(body),
		})
	}

	if req.raw != nil {
		// SCRUBBED, NOT MERELY COPIED, and by the rule this file already
		// states for the error path: a body that OUTLIVES the call must be
		// secret-free, because nothing further out holds all three credentials
		// to redact by value. It matters more here than there — a job log is
		// quoted into a review record and into a worker prompt, which is
		// further than an error message ever travels — and scrubCredentials
		// returns adapter-owned memory either way, so the deferred wipe of
		// rawBody cannot reach it.
		scrubbed := g.scrubCredentials(rawBody, req.subjectToken)
		if req.maxBody > 0 && len(scrubbed) > req.maxBody {
			// Now the caller's bound, applied to a body with nothing left in
			// it to cut in half. Redaction only ever shortens, so this is the
			// margin coming back off.
			scrubbed = scrubbed[len(scrubbed)-req.maxBody:]
		}
		*req.raw = scrubbed
	}
	if req.out == nil || len(rawBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(rawBody, req.out); err != nil {
		return backendError(req.operation+".decode", err)
	}
	return nil
}

// credentialWidth is the longest credential this call could have sent.
//
// The margin a bounded read has to overshoot by so no secret can be cut in
// half by the bound; see do. A LENGTH is all this computes — no copy of any
// credential is made, and none can be inferred from the number beyond what the
// scheme already fixes.
func (g *GiteaBackend) credentialWidth(subjectToken []byte) int {
	widest := len(subjectToken)
	for _, cred := range []string{g.token, g.custody.Password} {
		if len(cred) > widest {
			widest = len(cred)
		}
	}
	return widest
}

// readBodyTail reads a response body, keeping at most max bytes of its END.
//
// max <= 0 reads the whole body — the behaviour every caller but the job log
// has always had. Above zero it streams through a fixed pair of buffers and
// never holds more than 2*max, so a hundred-megabyte CI log costs the sweep
// the bound and not the log.
func readBodyTail(body io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		return io.ReadAll(body)
	}
	// Two buffers, filled alternately: when the newer one is full the older
	// is discarded, so the pair always spans the last max..2*max bytes read
	// and the tail is a suffix of their concatenation.
	older := make([]byte, 0, max)
	newer := make([]byte, 0, max)
	chunk := make([]byte, 32*1024)
	for {
		n, err := body.Read(chunk)
		if n > 0 {
			for read := chunk[:n]; len(read) > 0; {
				room := max - len(newer)
				if room == 0 {
					older, newer = newer, older[:0]
					continue
				}
				if room > len(read) {
					room = len(read)
				}
				newer = append(newer, read[:room]...)
				read = read[room:]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(newer) >= max || len(older) == 0 {
		return newer, nil
	}
	// The newer buffer alone is short of the bound; take the rest from the
	// end of the older one.
	tail := make([]byte, 0, max)
	tail = append(tail, older[len(older)-(max-len(newer)):]...)
	return append(tail, newer...), nil
}

// giteaIsNotReady reports whether a 405 is Gitea's "still computing" answer
// rather than a genuine method refusal.
//
// The status alone is ambiguous, so the message is what distinguishes them.
func giteaIsNotReady(body []byte) bool {
	return strings.Contains(strings.ToLower(giteaMessage(body)), "try again later")
}

// giteaMessage extracts Gitea's error message, falling back to the raw body.
//
// Bounded: an error body is remote input, and an unbounded one would put an
// arbitrary amount of someone else's text into a log line.
func giteaMessage(body []byte) string {
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Message != "" {
		return truncate(envelope.Message, 200)
	}
	return truncate(strings.TrimSpace(string(body)), 200)
}

// truncate bounds s to n bytes, marking it when it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GiteaAPIError reports a Gitea response the adapter could not use. It is
// reported as the cause inside a BackendError.
type GiteaAPIError struct {
	Status    int
	Operation string
	Message   string
}

// Error implements the error interface.
func (e *GiteaAPIError) Error() string {
	msg := "gitea " + e.Operation + ": HTTP " + strconv.Itoa(e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// repoPath renders the API path prefix for one repository.
func repoPath(repo RepoRef) string {
	return "/repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo)
}

// =============================================================================
// Wire shapes
// =============================================================================
//
// Only the fields the module reads. Gitea's payloads are much larger, and
// decoding into a narrow struct means a field added upstream cannot change what
// this adapter does.

type giteaUser struct {
	Login string `json:"login"`
}

type giteaLabel struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// giteaMilestone is the milestone Gitea attaches to an issue.
type giteaMilestone struct {
	Title string `json:"title"`
}

type giteaIssue struct {
	Number    int             `json:"number"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	State     string          `json:"state"`
	Labels    []giteaLabel    `json:"labels"`
	Milestone *giteaMilestone `json:"milestone"`
	User      *giteaUser      `json:"user"`
	Assignee  *giteaUser      `json:"assignee"`
	HTMLURL   string          `json:"html_url"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// toForgeIssue converts Gitea's shape to the module's.
func (i *giteaIssue) toForgeIssue() *ForgeIssue {
	issue := &ForgeIssue{
		Number:  i.Number,
		Title:   i.Title,
		Body:    i.Body,
		State:   i.State,
		URL:     i.HTMLURL,
		Created: i.CreatedAt,
		Updated: i.UpdatedAt,
	}
	if i.Milestone != nil {
		issue.Milestone = i.Milestone.Title
	}
	if i.User != nil {
		issue.Author = i.User.Login
	}
	if i.Assignee != nil {
		issue.Assignee = i.Assignee.Login
	}
	for _, label := range i.Labels {
		issue.Labels = append(issue.Labels, label.Name)
	}
	return issue
}

type giteaComment struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	User      *giteaUser `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
}

// =============================================================================
// Issues
// =============================================================================

// ListIssues returns issues matching filter.
//
// Gitea returns pull requests from the issues endpoint too — a PR is an issue
// there — so they are filtered out. A PR arriving where the dispatcher expects
// work items would be picked up as one and worked a second time.
func (g *GiteaBackend) ListIssues(ctx context.Context, repo RepoRef, filter IssueFilter) ([]ForgeIssue, error) {
	query := url.Values{}
	state := filter.State
	if state == "" {
		state = "open"
	}
	query.Set("state", state)
	query.Set("type", "issues") // excludes pull requests

	if len(filter.Labels) > 0 {
		query.Set("labels", strings.Join(filter.Labels, ","))
	}
	if !filter.Since.IsZero() {
		query.Set("since", filter.Since.UTC().Format(time.RFC3339))
	}
	// PAGINATED, because reading only the first page is how the polling
	// reconciler silently orphaned every issue past it. The floor that
	// everything else depends on cannot cover a prefix and call it a sweep.
	query.Set("limit", strconv.Itoa(giteaDefaultPageSize))

	pages := issuePageBound(filter.Limit)

	var issues []ForgeIssue
	for page := 1; page <= pages; page++ {
		query.Set("page", strconv.Itoa(page))

		var raw []giteaIssue
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/issues",
			query:     query,
			out:       &raw,
			operation: "ListIssues",
		}); err != nil {
			return nil, err
		}

		for i := range raw {
			issues = append(issues, *raw[i].toForgeIssue())
			if filter.Limit > 0 && len(issues) >= filter.Limit {
				// The CALLER's bound, reached. Returning here is not a
				// truncation the adapter is hiding — the caller asked for at
				// most this many and knows it may be seeing a prefix.
				return issues, nil
			}
		}

		// A short page is the forge saying that is all there is.
		if len(raw) < giteaDefaultPageSize {
			return issues, nil
		}
	}

	// EVERY PAGE UP TO THE CEILING WAS FULL, so the forge has not yet said
	// there is nothing more. Only an UNBOUNDED listing reaches here — a
	// bounded one returns above the moment it has its limit — and the two
	// things that look identical from here are a repository holding exactly
	// this many issues and one holding more. One extra request tells them
	// apart, and getting that wrong in either direction is a real failure:
	// reporting a fault for a repository that simply ended on a page boundary
	// is a false alarm, and returning the collected pages with a nil error
	// presents a prefix as a complete listing, which is the exact hole paging
	// was added to close.
	query.Set("page", strconv.Itoa(pages+1))
	var beyond []giteaIssue
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/issues",
		query:     query,
		out:       &beyond,
		operation: "ListIssues",
	}); err != nil {
		return nil, err
	}
	if len(beyond) == 0 {
		// It ended exactly on the boundary. Nothing was missed.
		return issues, nil
	}
	return nil, backendError("ListIssues", errIssuePagesExhausted)
}

// GetIssue returns one issue.
func (g *GiteaBackend) GetIssue(ctx context.Context, repo RepoRef, number int) (*ForgeIssue, error) {
	var raw giteaIssue
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/issues/" + strconv.Itoa(number),
		out:       &raw,
		operation: "GetIssue",
	}); err != nil {
		return nil, err
	}
	return raw.toForgeIssue(), nil
}

// CommentOnIssue posts a comment and returns it as Gitea stored it.
//
// The returned record IS the read-back for this write: Gitea echoes the created
// comment, so a body that came back different, or an ID of zero, means the
// write did not land as asked.
func (g *GiteaBackend) CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, &InvalidInputError{Field: "body", Message: "a comment must say something"}
	}

	var raw giteaComment
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/issues/" + strconv.Itoa(number) + "/comments",
		body:      map[string]string{"body": body},
		out:       &raw,
		operation: "CommentOnIssue",
	}); err != nil {
		return nil, err
	}

	if raw.ID == 0 || raw.Body != body {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CommentOnIssue",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      truncate(body, 80),
			Got:       truncate(raw.Body, 80),
		}
	}

	comment := &ForgeComment{ID: raw.ID, Body: raw.Body, Created: raw.CreatedAt}
	if raw.User != nil {
		comment.Author = raw.User.Login
	}
	return comment, nil
}

// CloseIssue closes an issue, verified by read-back.
func (g *GiteaBackend) CloseIssue(ctx context.Context, repo RepoRef, number int) error {
	var raw giteaIssue
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPatch,
		path:      repoPath(repo) + "/issues/" + strconv.Itoa(number),
		body:      map[string]string{"state": "closed"},
		out:       &raw,
		operation: "CloseIssue",
	}); err != nil {
		return err
	}
	// READ-BACK, like every other write in this adapter. A forge that accepts
	// the PATCH and leaves the issue open is the failure a caller most needs
	// told about, because the comment it just posted says the issue is being
	// closed.
	if !strings.EqualFold(strings.TrimSpace(raw.State), "closed") {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CloseIssue",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "closed",
			Got:       raw.State,
		}
	}
	return nil
}

// ListIssueComments returns an issue's discussion, oldest first.
//
// Gitea returns comments in creation order, which is the order the port
// promises, so nothing here re-sorts: a sort on a timestamp the forge rounds to
// the second would REORDER two comments posted in the same second, and the pair
// most likely to be a second apart is a park's question and its answer.
//
// PAGINATED, AND A FORGE THAT NEVER ENDS IS A FAULT. The first page of a long
// thread holds the OLDEST comments, so returning a prefix would deliver the
// original contract and drop the answer that re-scoped it — the exact defect
// this method exists to close (#263). Reported as an error, never as a short
// discussion, because the caller cannot tell the two apart.
// PAGED BY `since`, NOT BY `page`, BECAUSE THIS ENDPOINT HAS NO PAGE. Gitea's
// per-issue comment listing takes `since` and `before`; `page` and `limit` are
// parameters of the REPO-level /issues/comments route, and this one silently
// ignores both. Measured against Gitea 1.24 on a 50+ comment thread:
//
//	?limit=10  -> 50 comments      ?page=1 -> ids 2328..21565
//	?limit=200 -> 50 comments      ?page=2 -> ids 2328..21565  (identical)
//
// So the old page loop re-read page one for ever. On a thread SHORTER than the
// server's cap it happened to work — the first response was short, and the loop
// ended before the bug could show — which is why this survived until a project
// with a 50-comment issue was onboarded. Then every sweep spent 1000 requests
// and failed the whole issue source, taking every other project down with it
// (go-luks2#3, 2026-09-14).
//
// The cursor is the last comment's creation time. `since` is inclusive and
// Gitea stores whole seconds, so a page always re-delivers at least the
// comments sharing that second; they are dropped by id, which is also what
// makes the loop terminate — a page that adds nothing new is the end.
func (g *GiteaBackend) ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	var (
		comments []ForgeComment
		seen     = map[int64]struct{}{}
		since    time.Time
	)
	for page := 1; page <= maxIssuePages; page++ {
		query := url.Values{}
		if !since.IsZero() {
			query.Set("since", since.UTC().Format(time.RFC3339))
		}

		var raw []giteaComment
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/issues/" + strconv.Itoa(number) + "/comments",
			query:     query,
			out:       &raw,
			operation: "ListIssueComments",
		}); err != nil {
			return nil, err
		}

		added := 0
		ignoredCursor := false
		// The cursor as SENT, pinned: `since` advances as this page is consumed,
		// and comparing against a moving value would not be the question asked.
		cursor := since
		for _, c := range raw {
			// Older than the cursor we sent: the forge did not apply `since`.
			if !cursor.IsZero() && c.CreatedAt.Before(cursor) {
				ignoredCursor = true
			}
			if _, dup := seen[c.ID]; dup {
				continue
			}
			seen[c.ID] = struct{}{}
			comment := ForgeComment{ID: c.ID, Body: c.Body, Created: c.CreatedAt}
			if c.User != nil {
				comment.Author = c.User.Login
			}
			comments = append(comments, comment)
			if c.CreatedAt.After(since) {
				since = c.CreatedAt
			}
			added++
		}
		// A FORGE THAT IGNORES THE CURSOR IS A FAULT, and it says so itself by
		// handing back a comment OLDER than what we asked from. `since` is
		// inclusive, so the cursor's own second comes back and that is expected;
		// anything before it means the filter was not applied, the window will
		// never move, and any answer we return is a prefix. Returning a prefix
		// with a nil error is the lie the caller cannot detect — what #263
		// refused, and what this keeps refusing.
		if ignoredCursor {
			return nil, backendError("ListIssueComments", errCommentPagesExhausted)
		}
		// Nothing new after an honoured cursor is simply the end of the thread.
		if added == 0 {
			return comments, nil
		}
	}
	return nil, backendError("ListIssueComments", errCommentPagesExhausted)
}

// SetIssueLabels replaces an issue's labels and verifies the result by reading
// the issue back.
//
// Two things this does not do, both deliberate. It does not trust the response:
// Gitea has documented cases of answering 200 for a write that did not take
// effect, and the label call is one of them — believing it is how the shell
// module displayed state that was not real. And it does not create missing
// labels: a name the repository does not define is a configuration error in the
// project's label vocabulary, and inventing labels on the fly would paper over
// a typo by permanently adding it to the repo.
//
// Labels are DISPLAY OUTPUT. Nothing in the module decides anything by reading
// them; this exists so a human watching the forge can see what the module is
// doing.
func (g *GiteaBackend) SetIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	ids, err := g.resolveLabelIDs(ctx, repo, labels)
	if err != nil {
		return err
	}

	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPut,
		path:      repoPath(repo) + "/issues/" + strconv.Itoa(number) + "/labels",
		body:      map[string][]int64{"labels": ids},
		operation: "SetIssueLabels",
	}); err != nil {
		return err
	}

	// The read-back. This is the whole reason the method is not two lines.
	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	want, got := normalizedLabels(labels), normalizedLabels(issue.Labels)
	if want != got {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "SetIssueLabels",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      want,
			Got:       got,
		}
	}
	return nil
}

// AddIssueLabels adds labels to an issue and verifies the result by reading the
// issue back.
//
// POST, not PUT: Gitea's POST route ADDS to the set where PUT replaces it. That
// is the whole reason this method exists — a replacement has to be computed
// from the labels as they were read a moment earlier, and anything a human
// added in between is destroyed by a write that did exactly what it was told.
//
// The read-back checks that every requested label is PRESENT, not that the set
// matches: the labels this call did not name are supposed to still be there,
// and comparing the whole set would report a correct write as failed.
func (g *GiteaBackend) AddIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	if len(labels) == 0 {
		// Not an error, and not a request either. A write that adds nothing
		// would still cost a round trip and a read-back to prove it did
		// nothing.
		return nil
	}

	ids, err := g.resolveLabelIDs(ctx, repo, labels)
	if err != nil {
		return err
	}

	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/issues/" + strconv.Itoa(number) + "/labels",
		body:      map[string][]int64{"labels": ids},
		operation: "AddIssueLabels",
	}); err != nil {
		return err
	}

	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(issue.Labels))
	for _, label := range issue.Labels {
		present[strings.ToLower(strings.TrimSpace(label))] = true
	}
	var missing []string
	for _, label := range labels {
		if !present[strings.ToLower(strings.TrimSpace(label))] {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "AddIssueLabels",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      normalizedLabels(labels),
			Got:       normalizedLabels(issue.Labels) + " (missing " + normalizedLabels(missing) + ")",
		}
	}
	return nil
}

// RemoveIssueLabel removes one label from an issue and verifies the result by
// reading the issue back.
//
// Removing a label the issue does not carry is a SUCCESS, not an error. Gitea
// answers 404 for it, and the reconciler's honest question is "is this label
// off the issue" rather than "did I personally take it off" — treating an
// already-absent label as a failure would make every idempotent re-sync fail
// once somebody removed a label by hand.
func (g *GiteaBackend) RemoveIssueLabel(ctx context.Context, repo RepoRef, number int, label string) error {
	if strings.TrimSpace(label) == "" {
		return &InvalidInputError{Field: "label", Message: "must not be empty"}
	}

	ids, err := g.resolveLabelIDs(ctx, repo, []string{label})
	if err != nil {
		return err
	}

	err = g.do(ctx, giteaRequest{
		method: http.MethodDelete,
		path: repoPath(repo) + "/issues/" + strconv.Itoa(number) +
			"/labels/" + strconv.FormatInt(ids[0], 10),
		operation: "RemoveIssueLabel",
	})
	// A label that was not on the issue is already in the state asked for. The
	// read-back below is what actually decides, so this only avoids reporting
	// a no-op as a failure.
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	wanted := strings.ToLower(strings.TrimSpace(label))
	for _, held := range issue.Labels {
		if strings.ToLower(strings.TrimSpace(held)) == wanted {
			return &ForgeWriteNotAppliedError{
				Type:      ForgeGitea,
				Operation: "RemoveIssueLabel",
				Subject:   fmt.Sprintf("%s#%d", repo, number),
				Want:      "without " + wanted,
				Got:       normalizedLabels(issue.Labels),
			}
		}
	}
	return nil
}

// resolveLabelIDs maps label names onto the repository's label IDs.
//
// Gitea's label endpoint takes IDs, not names. Resolving through the
// repository's own vocabulary means a name it does not define fails HERE, with
// the name in the error, rather than being silently dropped by a write that
// still returns 200.
func (g *GiteaBackend) resolveLabelIDs(ctx context.Context, repo RepoRef, names []string) ([]int64, error) {
	if len(names) == 0 {
		return []int64{}, nil
	}

	// PAGINATED by listLabels, because the failure of reading only the first
	// page is a lie rather than a truncation: a repository with more labels
	// than one page would report "defines no label named X" for a label it
	// plainly defines, sending whoever reads it to fix a label vocabulary that
	// is already correct. Long-lived repositories accumulate labels, so this
	// is a matter of time rather than an edge case.
	defined, err := g.listLabels(ctx, repo)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]int64, len(defined))
	for _, label := range defined {
		byName[strings.ToLower(label.Name)] = label.ID
	}

	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := byName[strings.ToLower(name)]
		if !ok {
			return nil, &InvalidInputError{
				Field:   "labels",
				Message: "repository " + repo.String() + " defines no label named " + name,
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// giteaEventFamilies maps a requested webhook event onto the set Gitea actually
// configures for it.
//
// MEASURED against a live 1.27.1 instance on 2026-08-20, not guessed: asking
// for issues, pull_request and push returned the union below. Gitea expands a
// coarse event into its family and stores the expansion, so a read-back that
// compared sets for equality reported a correctly applied write as failed.
//
// An event with no entry here is its own family. That is deliberate and
// fail-closed: if a future Gitea expands something this map does not know
// about, the extra events read as unexpected and EnsureWebhook fails loudly,
// which is how the expansion gets measured and added rather than silently
// accepted.
var giteaEventFamilies = map[string][]string{
	"issues": {
		"issues", "issue_assign", "issue_comment", "issue_label", "issue_milestone",
	},
	"pull_request": {
		"pull_request", "pull_request_assign", "pull_request_comment",
		"pull_request_label", "pull_request_milestone", "pull_request_review",
		"pull_request_review_request", "pull_request_sync",
	},
}

// expectedEvents returns every event Gitea may legitimately configure for the
// requested set.
func expectedEvents(requested []string) map[string]bool {
	expected := make(map[string]bool, len(requested)*4)
	for _, event := range requested {
		key := strings.ToLower(strings.TrimSpace(event))
		family, ok := giteaEventFamilies[key]
		if !ok {
			family = []string{key}
		}
		for _, member := range family {
			expected[member] = true
		}
	}
	return expected
}

// unexpectedEvents returns configured events that no requested event accounts
// for.
//
// These matter as much as missing ones. An UPDATE that failed to remove a
// stale subscription leaves the module receiving deliveries outside its spec —
// accepting any superset would call that success, which is the same
// trust-the-status-code failure read-back exists to prevent, one level up.
func unexpectedEvents(requested, configured []string) []string {
	expected := expectedEvents(requested)
	unexpected := make([]string, 0)
	for _, event := range configured {
		if !expected[strings.ToLower(strings.TrimSpace(event))] {
			unexpected = append(unexpected, event)
		}
	}
	return unexpected
}

// missingEvents returns the requested events absent from configured.
//
// Comparison is case-insensitive for the same reason label comparison is: a
// difference that carries no meaning must not read as a failed write.
func missingEvents(requested, configured []string) []string {
	present := make(map[string]bool, len(configured))
	for _, event := range configured {
		present[strings.ToLower(strings.TrimSpace(event))] = true
	}
	missing := make([]string, 0)
	for _, event := range requested {
		if !present[strings.ToLower(strings.TrimSpace(event))] {
			missing = append(missing, event)
		}
	}
	return missing
}

// normalizedLabels renders a label set in a comparable form.
//
// Sorted and lowercased because neither order nor case carries meaning: Gitea
// returns labels in its own order, and comparing raw slices would report a
// difference where there is none — a read-back that cries wolf gets ignored,
// which costs more than having no read-back at all.
func normalizedLabels(labels []string) string {
	if len(labels) == 0 {
		return "[]"
	}
	normalized := make([]string, 0, len(labels))
	for _, label := range labels {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(label)))
	}
	sort.Strings(normalized)
	return "[" + strings.Join(normalized, " ") + "]"
}

// =============================================================================
// Branches
// =============================================================================

type giteaBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// toForgeBranch converts Gitea's shape to the module's.
func (b *giteaBranch) toForgeBranch() *ForgeBranch {
	return &ForgeBranch{Name: b.Name, SHA: b.Commit.ID}
}

// giteaTag is Gitea's tag shape.
type giteaTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// GetTag returns one tag.
//
// GET /repos/{owner}/{repo}/tags/{tag}, whose 404 the shared transport already
// maps to ErrNotFound — so "this version has not been released" arrives as the
// same typed absence every other missing record does, and a caller cannot
// mistake it for an unreachable forge.
func (g *GiteaBackend) GetTag(ctx context.Context, repo RepoRef, tag string) (*ForgeTag, error) {
	if strings.TrimSpace(tag) == "" {
		return nil, &InvalidInputError{Field: "tag", Message: "a tag read names the tag"}
	}
	var raw giteaTag
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/tags/" + url.PathEscape(tag),
		out:       &raw,
		operation: "GetTag",
	}); err != nil {
		return nil, err
	}
	return &ForgeTag{Name: raw.Name, SHA: raw.Commit.SHA}, nil
}

// CreateBranch creates a branch at fromRef and verifies it by reading it back.
//
// The read-back is not ceremony here either: a branch that was not actually
// created would surface later as a push to a ref that does not exist, at which
// point the failure is attributed to the worker rather than to this call.
func (g *GiteaBackend) CreateBranch(ctx context.Context, repo RepoRef, name, fromRef string) (*ForgeBranch, error) {
	if name == "" {
		return nil, &InvalidInputError{Field: "name", Message: "a branch must be named"}
	}
	if fromRef == "" {
		return nil, &InvalidInputError{Field: "from_ref", Message: "a branch must be cut from somewhere"}
	}

	if err := g.do(ctx, giteaRequest{
		method: http.MethodPost,
		path:   repoPath(repo) + "/branches",
		body: map[string]string{
			"new_branch_name": name,
			"old_ref_name":    fromRef,
		},
		operation: "CreateBranch",
	}); err != nil {
		return nil, err
	}

	branch, err := g.GetBranch(ctx, repo, name)
	if err != nil {
		// An absent branch after an accepted create IS the write-not-applied
		// case, reported as such rather than as a puzzling NotFound from a
		// method the caller never invoked.
		if errors.Is(err, ErrNotFound) {
			return nil, &ForgeWriteNotAppliedError{
				Type:      ForgeGitea,
				Operation: "CreateBranch",
				Subject:   repo.String() + " " + name,
				Want:      "branch present",
				Got:       "absent",
			}
		}
		return nil, err
	}
	return branch, nil
}

// GetBranch returns one branch.
func (g *GiteaBackend) GetBranch(ctx context.Context, repo RepoRef, name string) (*ForgeBranch, error) {
	var raw giteaBranch
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/branches/" + url.PathEscape(name),
		out:       &raw,
		operation: "GetBranch",
	}); err != nil {
		return nil, err
	}
	return raw.toForgeBranch(), nil
}

// DeleteBranch removes a branch and verifies its absence.
//
// Deleting an already-absent branch succeeds: branch cleanup runs after a merge
// that may itself have deleted it, and a reconciler that failed on "already
// gone" would report a problem where the desired state holds.
func (g *GiteaBackend) DeleteBranch(ctx context.Context, repo RepoRef, name string) error {
	err := g.do(ctx, giteaRequest{
		method:    http.MethodDelete,
		path:      repoPath(repo) + "/branches/" + url.PathEscape(name),
		operation: "DeleteBranch",
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	// Read-back: the branch must be gone.
	if _, err := g.GetBranch(ctx, repo, name); err == nil {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "DeleteBranch",
			Subject:   repo.String() + " " + name,
			Want:      "absent",
			Got:       "branch present",
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// =============================================================================
// Pull requests
// =============================================================================

type giteaPull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Draft     bool      `json:"draft"`
	Merged    bool      `json:"merged"`
	MergeSHA  string    `json:"merge_commit_sha"`
	Mergeable bool      `json:"mergeable"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// toForgePullRequest converts Gitea's shape to the module's.
func (p *giteaPull) toForgePullRequest() *ForgePullRequest {
	return &ForgePullRequest{
		Number:    p.Number,
		Title:     p.Title,
		Body:      p.Body,
		State:     p.State,
		HeadRef:   p.Head.Ref,
		HeadSHA:   p.Head.SHA,
		BaseRef:   p.Base.Ref,
		Draft:     p.Draft,
		Merged:    p.Merged,
		MergeSHA:  p.MergeSHA,
		Mergeable: p.Mergeable,
		URL:       p.HTMLURL,
		Created:   p.CreatedAt,
		Updated:   p.UpdatedAt,
	}
}

// errOpenPullPagesExhausted reports an open-pull-request listing that never
// ended.
//
// A fault rather than a truncation, for the reason errReviewPagesExhausted is:
// the caller is asking which pull request a commit is the head of, and a
// truncated listing answers "none" — indistinguishable from a commit that
// really belongs to no pull request, and the change whose pipeline just went
// green is then never reviewed.
var errOpenPullPagesExhausted = errors.New(
	"open pull request listing never returned a short page")

// ListOpenPullRequests returns every open pull request on the repository,
// paging until the forge returns a short page.
func (g *GiteaBackend) ListOpenPullRequests(ctx context.Context, repo RepoRef) ([]ForgePullRequest, error) {
	var pulls []ForgePullRequest
	for page := 1; page <= maxIssuePages; page++ {
		query := url.Values{}
		query.Set("state", "open")
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var raw []giteaPull
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/pulls",
			query:     query,
			out:       &raw,
			operation: "ListOpenPullRequests",
		}); err != nil {
			return nil, err
		}
		for i := range raw {
			pulls = append(pulls, *raw[i].toForgePullRequest())
		}
		// A short page is the forge saying that is all there is.
		if len(raw) < giteaDefaultPageSize {
			return pulls, nil
		}
	}
	return nil, backendError("ListOpenPullRequests", errOpenPullPagesExhausted)
}

// CreatePullRequest opens a pull request and verifies it by reading it back.
func (g *GiteaBackend) CreatePullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (*ForgePullRequest, error) {
	switch {
	case spec.Title == "":
		return nil, &InvalidInputError{Field: "title", Message: "a pull request must be titled"}
	case spec.HeadRef == "":
		return nil, &InvalidInputError{Field: "head_ref", Message: "a pull request must name its source branch"}
	case spec.BaseRef == "":
		return nil, &InvalidInputError{Field: "base_ref", Message: "a pull request must name its target branch"}
	}

	// Gitea has no `draft` field on CreatePullRequestOption (checked against
	// the running server's swagger.v1.json, 1.27.1). It marks a pull request
	// draft by a TITLE PREFIX, so sending a draft boolean sets nothing and
	// quietly produces an ordinary pull request — which is worse than not
	// supporting drafts, because the caller believes the gate is on.
	//
	// The prefix is server-configurable, so this is not guaranteed to be the
	// one in force. That is precisely why the read-back below asserts Draft
	// came back true: a prefix this server does not recognise fails loudly
	// here rather than opening work for review that was meant to be held.
	title := spec.Title
	if spec.Draft {
		title = giteaDraftPrefix + " " + title
	}
	body := map[string]any{
		"title": title,
		"body":  spec.Body,
		"head":  spec.HeadRef,
		"base":  spec.BaseRef,
	}

	var created giteaPull
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/pulls",
		body:      body,
		out:       &created,
		operation: "CreatePullRequest",
	}); err != nil {
		return nil, err
	}
	if created.Number == 0 {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CreatePullRequest",
			Subject:   repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef,
			Want:      "a pull request number",
			Got:       "none",
		}
	}

	// Read-back against the forge's own view, not the create response: the
	// refs are what a later merge is guarded on, so they are worth confirming
	// from a second read rather than trusting the echo.
	pull, err := g.GetPullRequest(ctx, repo, created.Number)
	if err != nil {
		return nil, err
	}
	if spec.Draft && !pull.Draft {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, created.Number),
			Want:      "draft",
			Got:       "ready for review",
		}
	}
	if pull.HeadRef != spec.HeadRef || pull.BaseRef != spec.BaseRef {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, created.Number),
			Want:      spec.HeadRef + " -> " + spec.BaseRef,
			Got:       pull.HeadRef + " -> " + pull.BaseRef,
		}
	}

	if len(spec.Labels) > 0 {
		if err := g.SetIssueLabels(ctx, repo, created.Number, spec.Labels); err != nil {
			return nil, err
		}
		pull, err = g.GetPullRequest(ctx, repo, created.Number)
		if err != nil {
			return nil, err
		}
	}
	return pull, nil
}

// GetPullRequest returns one pull request.
func (g *GiteaBackend) GetPullRequest(ctx context.Context, repo RepoRef, number int) (*ForgePullRequest, error) {
	var raw giteaPull
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/pulls/" + strconv.Itoa(number),
		out:       &raw,
		operation: "GetPullRequest",
	}); err != nil {
		return nil, err
	}
	return raw.toForgePullRequest(), nil
}

// giteaMergeMethod maps a MergeStrategy's forge-neutral method name onto
// Gitea's own, refusing anything the platform does not implement.
//
// Refusing rather than substituting: landing a merge the wrong way is not a
// degraded success, and a squash strategy that silently produced a merge commit
// would corrupt the history the strategy exists to shape.
func (g *GiteaBackend) giteaMergeMethod(method string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "squash":
		return "squash", nil
	case "merge":
		return "merge", nil
	case "rebase":
		return "rebase", nil
	default:
		return "", &ForgeUnsupportedError{
			Type:       ForgeGitea,
			Capability: "merge method " + method,
		}
	}
}

type giteaChangedFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

// errPullFilePagesExhausted reports a changed-file listing that never ended.
var errPullFilePagesExhausted = errors.New(
	"pull request file listing never returned a short page")

// ListPullRequestFiles returns every file the pull request changes.
//
// PAGINATED, AND A FORGE THAT NEVER ENDS IS A FAULT. Gitea caps a page at its
// configured page size and says nothing about how many pages there are; a
// single request therefore answers "what does this change touch" with
// whatever fitted, and a reviewer handed that list reviews that list. The
// short page is how the forge says there is no more, and a listing that never
// produces one is reported rather than returned, because a caller cannot
// tell a truncated map of a change from a complete one.
func (g *GiteaBackend) ListPullRequestFiles(ctx context.Context, repo RepoRef, number int) ([]ForgeChangedFile, error) {
	var files []ForgeChangedFile
	for page := 1; page <= maxIssuePages; page++ {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var raw []giteaChangedFile
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/pulls/" + strconv.Itoa(number) + "/files",
			query:     query,
			out:       &raw,
			operation: "ListPullRequestFiles",
		}); err != nil {
			return nil, err
		}
		for _, f := range raw {
			files = append(files, ForgeChangedFile{
				Path:         f.Filename,
				PreviousPath: f.PreviousFilename,
				Status:       strings.ToLower(f.Status),
				Additions:    f.Additions,
				Deletions:    f.Deletions,
			})
		}
		if len(raw) < giteaDefaultPageSize {
			return files, nil
		}
	}
	return nil, backendError("ListPullRequestFiles", errPullFilePagesExhausted)
}

// MergePullRequest merges according to spec, refusing when the head has moved
// past the tree the caller approved.
//
// TWO GUARDS, and the difference between them matters. expectedHead is sent to
// Gitea as head_commit_id, which makes the FORGE refuse the merge if the branch
// moved — that is the authoritative check, decided where the race is. The read
// before it exists only to fail early with a legible error naming both SHAs; on
// its own it would be check-then-act with a window a push can land in.
//
// A caller passing an empty expectedHead gets no guard at all, so it must not:
// the whole point is that a commit pushed after a review cannot ride in on that
// review's approval.
func (g *GiteaBackend) MergePullRequest(ctx context.Context, repo RepoRef, number int, spec MergeSpec, expectedHead string) error {
	method, err := g.giteaMergeMethod(spec.Method)
	if err != nil {
		return err
	}
	if expectedHead == "" {
		return &InvalidInputError{
			Field:   "expected_head",
			Message: "a merge must name the tree that was approved, or a later commit lands on an older review",
		}
	}

	pull, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if pull.HeadSHA != expectedHead {
		return &HeadMovedError{
			Subject:  repo.String() + "#" + strconv.Itoa(number),
			Reviewed: shortSHA(expectedHead),
			Current:  shortSHA(pull.HeadSHA),
		}
	}

	// Field names are Gitea's own, taken from the running server's
	// swagger.v1.json (MergePullRequestOption) rather than from memory: this
	// struct's JSON tags are snake_case, and "Do"/"MergeTitleField" — the Go
	// field names, which older Gitea did use — bind to nothing. A merge sent
	// that way fails on the required "do", and the title and message would
	// have been silently dropped even if it did not.
	body := map[string]any{
		"do":                        method,
		"merge_title_field":         spec.CommitTitle,
		"merge_message_field":       spec.CommitMessage,
		"delete_branch_after_merge": spec.ExpectSourceDeleted,
		// The authoritative guard: Gitea refuses the merge if the head moved.
		// Present in MergePullRequestOption as of 1.27.1, which is what the
		// module runs against.
		"head_commit_id": expectedHead,
	}
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/pulls/" + strconv.Itoa(number) + "/merge",
		body:      body,
		operation: "MergePullRequest",
	}); err != nil {
		return err
	}

	// Read-back: Gitea reports 200 for merges that did not happen.
	merged, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if !merged.Merged {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "MergePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "merged",
			Got:       "state " + merged.State,
		}
	}
	return nil
}

// shortSHA renders a commit for a human-readable error.
func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// =============================================================================
// Reviews
// =============================================================================

type giteaReview struct {
	ID          int64      `json:"id"`
	User        *giteaUser `json:"user"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	CommitID    string     `json:"commit_id"`
	SubmittedAt time.Time  `json:"submitted_at"`
}

// ListReviews returns the reviews on one pull request.
//
// CommitID is carried through as HeadSHA and is load-bearing: a verdict applies
// to the tree it was given, and the review gate binds an approval to that SHA
// so a review cannot be carried onto a later commit nobody read.
func (g *GiteaBackend) ListReviews(ctx context.Context, repo RepoRef, number int) ([]ForgeReview, error) {
	var reviews []ForgeReview
	for page := 1; page <= maxIssuePages; page++ {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var raw []giteaReview
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/pulls/" + strconv.Itoa(number) + "/reviews",
			query:     query,
			out:       &raw,
			operation: "ListReviews",
		}); err != nil {
			return nil, err
		}
		for _, r := range raw {
			reviews = append(reviews, r.toForgeReview())
		}
		// A short page is the forge saying that is all there is.
		if len(raw) < giteaDefaultPageSize {
			return reviews, nil
		}
	}
	return nil, backendError("ListReviews", errReviewPagesExhausted)
}

// errReviewPagesExhausted reports a review listing that never ended.
//
// A fault rather than a truncation, and it names no remedy because the
// caller has none: the gate needs the NEWEST verdicts, which live on the last
// page, so a bound would drop exactly the reviews that decide the merge.
var errReviewPagesExhausted = errors.New(
	"pull request review listing never returned a short page")

// toForgeReview converts Gitea's shape to the module's.
func (r *giteaReview) toForgeReview() ForgeReview {
	review := ForgeReview{
		ID:        r.ID,
		State:     r.State,
		Body:      r.Body,
		HeadSHA:   r.CommitID,
		Submitted: r.SubmittedAt,
	}
	if r.User != nil {
		review.Reviewer = r.User.Login
	}
	return review
}

// giteaReviewStates maps the module's verdict onto Gitea's review event.
//
// Map-based, and the map is the whole of the platform knowledge this write
// carries: an adapter for another forge writes its own table and nothing
// above this line changes. A verdict absent from it is refused rather than
// approximated — posting the nearest available event would publish a
// different opinion than the reviewer formed.
var giteaReviewStates = map[ReviewVerdict]string{
	VerdictApprove:        "APPROVED",
	VerdictRequestChanges: "REQUEST_CHANGES",
	VerdictComment:        "COMMENT",
}

// SubmitReview posts a verdict on one pull request and verifies it by reading
// the submission back.
//
// THE HEAD IS SENT AND THE HEAD IS CHECKED. Gitea binds a review to
// commit_id, and the read-back compares it: a submission the server stored
// against a different commit is a verdict about a tree the reviewer did not
// read, and returning it as success would put an approval of an unexamined
// head into the gate's input.
//
// The response is not trusted on its own account either. Gitea has documented
// cases of answering 200 for a write that did not take effect, and a review
// is the one write in this adapter whose absence is invisible — the module
// would write a green commit status for an approval the forge never stored.
func (g *GiteaBackend) SubmitReview(ctx context.Context, repo RepoRef, number int, submission ReviewSubmission) (*ForgeReview, error) {
	if err := submission.Validate(); err != nil {
		return nil, err
	}
	event, ok := giteaReviewStates[submission.Verdict]
	if !ok {
		return nil, &ForgeUnsupportedError{
			Type:       ForgeGitea,
			Capability: "submitting a " + string(submission.Verdict) + " review",
		}
	}

	var raw giteaReview
	if err := g.do(ctx, giteaRequest{
		method: http.MethodPost,
		path:   repoPath(repo) + "/pulls/" + strconv.Itoa(number) + "/reviews",
		body: map[string]string{
			"event":     event,
			"body":      submission.Body,
			"commit_id": submission.HeadSHA,
		},
		out:       &raw,
		operation: "SubmitReview",
	}); err != nil {
		return nil, err
	}

	stored := raw.toForgeReview()
	if stored.ID == 0 || !strings.EqualFold(stored.State, event) {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "SubmitReview",
			Subject:   fmt.Sprintf("%s#%d@%s", repo, number, shortSHA(submission.HeadSHA)),
			Want:      event,
			Got:       stored.State,
		}
	}
	// A HEAD GITEA DID NOT ECHO IS NOT A HEAD GITEA DISAGREED WITH. Older
	// servers omit commit_id from the create response while storing it, so
	// an empty value is filled from what was sent; a value that is present
	// and different is the refusal this check exists for.
	if stored.HeadSHA == "" {
		stored.HeadSHA = submission.HeadSHA
	} else if !strings.EqualFold(stored.HeadSHA, submission.HeadSHA) {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "SubmitReview",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      shortSHA(submission.HeadSHA),
			Got:       shortSHA(stored.HeadSHA),
		}
	}
	return &stored, nil
}

// =============================================================================
// CI runs
// =============================================================================

type giteaStatus struct {
	ID          int64     `json:"id"`
	State       string    `json:"status"`
	Context     string    `json:"context"`
	Description string    `json:"description"`
	TargetURL   string    `json:"target_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListCheckRuns returns the CI runs against one commit.
//
// Read through the COMMIT STATUS api rather than an Actions-specific one.
// Statuses are what Gitea Actions publishes and what any other CI attached to
// the repository publishes too, so a project running its pipeline elsewhere is
// visible through the same call — and the endpoint is stable across the Gitea
// versions this module has to work against.
func (g *GiteaBackend) ListCheckRuns(ctx context.Context, repo RepoRef, sha string) ([]ForgeCheckRun, error) {
	if sha == "" {
		return nil, &InvalidInputError{Field: "sha", Message: "check runs are read against a commit"}
	}

	var raw []giteaStatus
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/commits/" + url.PathEscape(sha) + "/statuses",
		out:       &raw,
		operation: "ListCheckRuns",
	}); err != nil {
		return nil, err
	}

	runs := make([]ForgeCheckRun, 0, len(raw))
	for _, s := range raw {
		status, conclusion := giteaStatusToRun(s.State)
		runs = append(runs, ForgeCheckRun{
			ID:         strconv.FormatInt(s.ID, 10),
			Name:       s.Context,
			HeadSHA:    sha,
			Status:     status,
			Conclusion: conclusion,
			URL:        s.TargetURL,
			Started:    s.CreatedAt,
			Completed:  s.UpdatedAt,
		})
	}
	return runs, nil
}

// =============================================================================
// The trust boundary on the check-log path
// =============================================================================
//
// A COMMIT STATUS IS THIRD-PARTY TEXT, and this is the one route that acts on
// it. Anything holding status-write on the repository publishes a status —
// every CI attached to it, and anyone a token was issued to — so neither the
// status's target URL nor the log it leads to is the module's own word. The
// excerpt then travels further than any other value this adapter returns: onto
// a Review record and into a prompt an agent acts on.
//
// The boundary is therefore crossed twice, and both crossings are guarded:
//
//   - THE CONTENT. Untrusted output must not be published verbatim; the
//     excerpt is redacted against known credential shapes before it is bounded
//     (checks_failure.go, public_reason.go).
//   - THE SELECTOR, below. Untrusted input must not choose which privileged
//     resource a credentialed client reads. The job is taken only from a URL
//     that is provably this forge's own page for THIS repository's run.
//
// Whoever extends this path: a new field read off a status is on the wrong
// side of that line until something here proves otherwise.

// giteaJobFromStatusURL returns the Actions job id a commit status points at,
// and false for every URL that is not this forge's own job page for this repo.
//
// PARSED AND MATCHED WHOLE, NEVER PATTERN-SEARCHED (hosted review of #288). The
// job id decides which log a credentialed client fetches from the CONFIGURED
// repository, so an unanchored search for "/actions/runs/N/jobs/M" anywhere in
// the string was a confused deputy: a status publisher could write
// https://attacker.invalid/actions/runs/1/jobs/999 and have the adapter read
// job 999's log out of the real repository and quote it into a worker's prompt
// as this check's failure. The URL is not evidence about the job unless the URL
// itself is this forge's.
//
// So every part is checked and nothing is repaired: an absolute URL must carry
// this forge's own scheme and host and no userinfo, and the path must be
// EXACTLY the forge root followed by owner, repo, actions, runs, <run>, jobs,
// <job> — segment for segment, both ids decimal. A URL that fails any of it is
// refused outright rather than salvaged, because a URL that needed salvaging is
// one nobody can vouch for.
func giteaJobFromStatusURL(baseURL string, repo RepoRef, statusURL string) (runID, jobID string, ok bool) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", "", false
	}
	target, err := url.Parse(strings.TrimSpace(statusURL))
	if err != nil {
		return "", "", false
	}
	// A relative URL is this forge's by construction — it is how Gitea writes
	// the field. An absolute one has to prove it.
	if target.Scheme != "" || target.Host != "" || target.User != nil {
		if target.User != nil ||
			!strings.EqualFold(target.Scheme, base.Scheme) ||
			!strings.EqualFold(target.Host, base.Host) {
			return "", "", false
		}
	}

	// The forge's web root is the API base without its version suffix: a Gitea
	// served from a subpath writes that subpath into the field too.
	root := pathSegments(strings.TrimSuffix(strings.TrimSuffix(base.Path, "/"), "/api/v1"))
	tail := []string{repo.Owner, repo.Repo, "actions", "runs", "", "jobs", ""}
	want := make([]string, 0, len(root)+len(tail))
	want = append(want, root...)
	want = append(want, tail...)
	got := pathSegments(path.Clean("/" + strings.TrimPrefix(target.Path, "/")))
	if len(got) != len(want) {
		return "", "", false
	}
	for i, segment := range want {
		// The two empty entries are the ids, checked as numbers below.
		if segment != "" && got[i] != segment {
			return "", "", false
		}
	}
	runID, jobID = got[len(got)-3], got[len(got)-1]
	if !isDecimal(runID) || !isDecimal(jobID) {
		return "", "", false
	}
	return runID, jobID, true
}

// pathSegments splits a URL path into its non-empty segments.
func pathSegments(p string) []string {
	var out []string
	for _, segment := range strings.Split(p, "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

// isDecimal reports whether s is a non-empty run of decimal digits, bounded so
// a caller cannot hand the route an arbitrarily long "number".
func isDecimal(s string) bool {
	if s == "" || len(s) > 19 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// CheckRunLogs returns the tail of one Actions job's log.
//
// Read through the Actions job route while ListCheckRuns reads commit
// statuses, and the split is deliberate: statuses are what every CI publishes
// and are how the gate stays CI-agnostic, but a LOG only exists where the
// pipeline ran, which for this module's projects is Gitea Actions. A status
// from anything else — or one whose target URL this forge did not write — is
// refused here and the caller quotes the contexts alone.
func (g *GiteaBackend) CheckRunLogs(ctx context.Context, repo RepoRef, run ForgeCheckRun, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		return "", &InvalidInputError{
			Field:   "max_bytes",
			Message: "a log excerpt must state how much of it is wanted",
		}
	}
	runID, jobID, ok := giteaJobFromStatusURL(g.baseURL, repo, run.URL)
	if !ok {
		return "", &ForgeUnsupportedError{
			Type: ForgeGitea,
			Capability: "reading logs for check run " + run.Name +
				" (its status names no Actions job on this repository)",
		}
	}

	// Ownership is established by actionsJob, which asks the run's own job
	// listing for the two facts the excerpt implicitly claims: that the job
	// belongs to the named run, and that the run covers the very head this
	// check is about. Fail closed on every uncertainty — the job absent from
	// the listing, a head that does not match, a head we were never told. The
	// caller loses an excerpt and still names the red contexts.
	if run.HeadSHA == "" {
		return "", &ForgeUnsupportedError{
			Type: ForgeGitea,
			Capability: "reading logs for check run " + run.Name +
				" (nothing records which commit it covers)",
		}
	}
	if _, err := g.actionsJob(ctx, repo, runID, jobID, run); err != nil {
		return "", err
	}

	var body []byte
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/actions/jobs/" + jobID + "/logs",
		raw:       &body,
		maxBody:   maxBytes,
		operation: "CheckRunLogs",
	}); err != nil {
		return "", err
	}
	return string(body), nil
}

// giteaStatusToRun splits Gitea's single status value into the status and
// conclusion the module reasons about.
//
// The distinction is load-bearing for supersession: "still running" is what
// makes a displaced run worth cancelling, while a finished one needs nothing.
// An unrecognised value is reported as still running with no conclusion —
// erring toward "might still be consuming capacity" rather than assuming a
// state Gitea did not claim.
func giteaStatusToRun(state string) (status, conclusion string) {
	switch strings.ToLower(state) {
	case "pending":
		return "pending", ""
	case "running":
		return "in_progress", ""
	case "success":
		return "completed", "success"
	case "failure", "error":
		return "completed", "failure"
	default:
		return "in_progress", ""
	}
}

// CancelCheckRun is REFUSED on Gitea, which publishes no cancel route.
//
// It returns ErrForgeUnsupported and never nil. A caller told a run was
// cancelled stops tracking capacity the run is still consuming, and the
// supersession record would claim a cancellation that never happened — the
// difference between an orphan an operator can see and a capacity leak nobody
// can explain.
//
// This is the capability flag made real: CancelCheckRuns is false, and this
// method's behaviour is what that flag promises.
// giteaCommitStates maps the module's CheckStatus onto Gitea's commit-status
// state. It is the platform knowledge SetCommitStatus carries, and the only
// thing another adapter would rewrite.
//
// CheckCancelled IS ABSENT, and that is a refusal rather than an oversight.
// Gitea publishes pending, success, warning, failure and error, none of which
// means "this never finished": mapping a cancellation onto failure would
// report a run nobody completed as a change that failed, and mapping it onto
// pending would leave a required check waiting for ever. The module's own
// read path already collapses error onto failure, so writing a state that
// reads back as a different one would fail the write's own verification.
var giteaCommitStates = map[CheckStatus]string{
	CheckPending: "pending",
	CheckSuccess: "success",
	CheckFailure: "failure",
}

// SetCommitStatus writes a named status against one commit and verifies it by
// reading the commit's statuses back.
//
// THE READ-BACK COMPARES THE STATE THE MODULE ASKED FOR AGAINST THE STATE THE
// FORGE NOW REPORTS, through the same conversion ListCheckRuns uses. Going
// out and coming back through one mapping is what makes the check meaningful:
// a write that landed as some other state, or that did not land at all, is
// caught here rather than becoming a green required check over a rejected
// change.
func (g *GiteaBackend) SetCommitStatus(ctx context.Context, repo RepoRef, sha string, status CommitStatus) error {
	if strings.TrimSpace(sha) == "" {
		return &InvalidInputError{Field: "sha", Message: "a status is written against a commit"}
	}
	if err := status.Validate(); err != nil {
		return err
	}
	state, ok := giteaCommitStates[status.Status]
	if !ok {
		return &ForgeUnsupportedError{
			Type:       ForgeGitea,
			Capability: "publishing a commit status of " + status.Status.String(),
		}
	}

	body := map[string]string{"state": state, "context": status.Context}
	if status.Description != "" {
		body["description"] = status.Description
	}
	if status.TargetURL != "" {
		body["target_url"] = status.TargetURL
	}
	var raw giteaStatus
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/statuses/" + url.PathEscape(sha),
		body:      body,
		out:       &raw,
		operation: "SetCommitStatus",
	}); err != nil {
		return err
	}

	runs, err := g.ListCheckRuns(ctx, repo, sha)
	if err != nil {
		return err
	}
	for _, run := range latestCheckPerContext(runs) {
		if run.Name != status.Context {
			continue
		}
		if got := checkStatusFor(run); got != status.Status {
			return &ForgeWriteNotAppliedError{
				Type:      ForgeGitea,
				Operation: "SetCommitStatus",
				Subject:   repo.String() + "@" + shortSHA(sha) + " " + status.Context,
				Want:      status.Status.String(),
				Got:       got.String(),
			}
		}
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      ForgeGitea,
		Operation: "SetCommitStatus",
		Subject:   repo.String() + "@" + shortSHA(sha) + " " + status.Context,
		Want:      status.Status.String(),
		Got:       "no status with that name is recorded on the commit",
	}
}

func (g *GiteaBackend) CancelCheckRun(_ context.Context, _ RepoRef, _ string) error {
	return &ForgeUnsupportedError{
		Type:       ForgeGitea,
		Capability: "cancelling check runs (Gitea publishes no cancel route)",
	}
}

// RerunCheckRun restarts the failed jobs of the Actions run one commit status
// points at, verified by read-back.
//
// IT RESOLVES THE RUN THE SAME WAY CheckRunLogs RESOLVES THE LOG, through
// giteaJobFromStatusURL and the run's own job listing, and for a sharper
// reason: this is a WRITE. A status publisher that could name any run on this
// repository would be able to make a credentialed client restart CI it has
// nothing to do with — burning the fleet's capacity from a field anybody who
// can post a commit status controls. So the URL must be this forge's own page
// for THIS repository, and the job it names must belong to the named run and
// cover the very head this check is about. Every uncertainty is fatal.
//
// THE READ-BACK IS run_attempt OR A JOB THAT IS NO LONGER COMPLETE, either of
// which proves the rerun took. Both are checked because neither alone is
// safe: a job that fails again within the round trip is "completed" once more,
// and a platform that restarts without bumping the attempt would look like a
// write that never landed. A rerun reported as applied when it was not would
// leave the engine waiting out its whole review poll for a result nobody
// asked for.
func (g *GiteaBackend) RerunCheckRun(ctx context.Context, repo RepoRef, run ForgeCheckRun) error {
	runID, jobID, ok := giteaJobFromStatusURL(g.baseURL, repo, run.URL)
	if !ok {
		return &ForgeUnsupportedError{
			Type: ForgeGitea,
			Capability: "re-running check run " + run.Name +
				" (its status names no Actions job on this repository)",
		}
	}
	if run.HeadSHA == "" {
		return &ForgeUnsupportedError{
			Type: ForgeGitea,
			Capability: "re-running check run " + run.Name +
				" (nothing records which commit it covers)",
		}
	}

	before, err := g.actionsJob(ctx, repo, runID, jobID, run)
	if err != nil {
		return err
	}

	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/actions/runs/" + runID + "/rerun-failed-jobs",
		operation: "RerunCheckRun",
	}); err != nil {
		return err
	}

	after, err := g.actionsJob(ctx, repo, runID, jobID, run)
	if err != nil {
		return err
	}
	if after.RunAttempt > before.RunAttempt || !strings.EqualFold(after.Status, "completed") {
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      ForgeGitea,
		Operation: "RerunCheckRun",
		Subject:   repo.String() + "@" + shortSHA(run.HeadSHA) + " " + run.Name,
		Want:      "a restarted job",
		Got:       "attempt " + strconv.Itoa(after.RunAttempt) + " still " + after.Status,
	}
}

// giteaActionsJob is the part of one Actions job this adapter reads.
type giteaActionsJob struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	RunAttempt int    `json:"run_attempt"`
}

// actionsJob returns one job of one Actions run, PROVING as it goes that the
// job belongs to that run and covers the head the commit status claims.
//
// THE URL SAYS WHICH JOB; THE FORGE SAYS WHOSE IT IS. Parsing a status target
// proves only that it pointed at this repository — a publisher may still name
// a real job belonging to some OTHER run on it. Every caller here acts on the
// answer, by quoting a log into a worker's prompt or by restarting CI, so the
// two facts are established once, here, and every uncertainty is fatal.
func (g *GiteaBackend) actionsJob(ctx context.Context, repo RepoRef, runID, jobID string, run ForgeCheckRun) (giteaActionsJob, error) {
	var listing struct {
		Jobs []giteaActionsJob `json:"jobs"`
	}
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/actions/runs/" + runID + "/jobs",
		out:       &listing,
		operation: "actionsJob",
	}); err != nil {
		return giteaActionsJob{}, err
	}
	for _, job := range listing.Jobs {
		if strconv.FormatInt(job.ID, 10) != jobID {
			continue
		}
		if strconv.FormatInt(job.RunID, 10) == runID && strings.EqualFold(job.HeadSHA, run.HeadSHA) {
			return job, nil
		}
		break
	}
	return giteaActionsJob{}, &ForgeUnsupportedError{
		Type: ForgeGitea,
		Capability: "acting on check run " + run.Name +
			" (the job its status names does not belong to this commit's run)",
	}
}

// =============================================================================
// Webhooks
// =============================================================================

// The headers a Gitea delivery arrives with. Exported because the HTTP ingest
// layer reads them and hands the values to VerifyWebhook and ParseWebhook —
// which take the signature and event type as arguments precisely so they can be
// tested and reused without an http.Request.
const (
	// GiteaSignatureHeader carries the HMAC-SHA256 of the body, hex-encoded.
	GiteaSignatureHeader = "X-Gitea-Signature"

	// GiteaEventHeader names the event type ParseWebhook maps.
	GiteaEventHeader = "X-Gitea-Event"

	// GiteaDeliveryHeader uniquely identifies one delivery, for
	// de-duplication and for correlating a reconcile back to what prompted it.
	GiteaDeliveryHeader = "X-Gitea-Delivery"
)

// VerifyWebhook authenticates a delivery, fail-closed and in constant time.
//
// HMAC-SHA256 over the exact bytes received, compared with hmac.Equal so the
// comparison cannot be timed to recover the expected digest a byte at a time.
//
// Every refusal path returns the SAME error shape and quotes nothing from the
// payload: at this point the payload is attacker-controlled, and echoing it
// into a log is an injection surface. An empty secret or signature is a
// refusal, never a bypass — a misconfigured deployment must not silently accept
// unsigned deliveries.
func (g *GiteaBackend) VerifyWebhook(payload []byte, signature string, secret []byte) error {
	if len(secret) == 0 {
		return &WebhookUnverifiedError{Type: ForgeGitea, Reason: "no signing secret is configured"}
	}
	if signature == "" {
		return &WebhookUnverifiedError{Type: ForgeGitea, Reason: "delivery carried no signature"}
	}

	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return &WebhookUnverifiedError{Type: ForgeGitea, Reason: "signature is not hex"}
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(provided, expected) {
		return &WebhookUnverifiedError{Type: ForgeGitea, Reason: "signature does not match the payload"}
	}
	return nil
}

// giteaHookPayload is the union of the delivery fields the module reads.
type giteaHookPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Issue  *struct {
		Number int `json:"number"`
	} `json:"issue"`
	PullRequest *struct {
		Number int `json:"number"`
		Merged bool
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Review *struct {
		Type string `json:"type"`
	} `json:"review"`
	CommitID   string `json:"commit_id"`
	SHA        string `json:"sha"`
	Repository *struct {
		Name  string `json:"name"`
		Owner *struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// ParseWebhook maps a VERIFIED delivery onto a ForgeEvent.
//
// Callers verify first. Parsing an unverified payload is parsing whatever an
// attacker sent, and the resulting event would name a repository and an issue
// of their choosing.
//
// An unmapped event type is reported as EventUnsupported rather than an error:
// forges send plenty the module does not act on, and treating ordinary traffic
// as a failure would make a healthy webhook look like an outage. The result is
// still only a HINT — the reconciler re-reads the subject before deciding.
func (g *GiteaBackend) ParseWebhook(eventType string, payload []byte) (*ForgeEvent, error) {
	var body giteaHookPayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, backendError("ParseWebhook.decode", err)
	}

	event := &ForgeEvent{Kind: EventUnsupported}
	if body.Repository != nil {
		event.Repo.Repo = body.Repository.Name
		if body.Repository.Owner != nil {
			event.Repo.Owner = body.Repository.Owner.Login
		}
	}
	if body.Issue != nil {
		event.IssueNumber = body.Issue.Number
	}
	if body.PullRequest != nil {
		event.PullRequestNumber = body.PullRequest.Number
		event.HeadSHA = body.PullRequest.Head.SHA
	}
	switch {
	case body.CommitID != "":
		event.HeadSHA = body.CommitID
	case body.SHA != "" && event.HeadSHA == "":
		event.HeadSHA = body.SHA
	}

	switch strings.ToLower(eventType) {
	case "issues":
		switch body.Action {
		case "opened":
			event.Kind = EventIssueOpened
		case "edited":
			event.Kind = EventIssueEdited
		case "closed":
			event.Kind = EventIssueClosed
		case "label_updated", "label_cleared":
			event.Kind = EventIssueLabeled
		}
	case "pull_request":
		switch {
		case body.Action == "opened":
			event.Kind = EventPullRequestOpened
		case body.Action == "synchronized":
			event.Kind = EventPullRequestUpdated
		case body.Action == "closed" && body.PullRequest != nil && body.PullRequest.Merged:
			event.Kind = EventPullRequestMerged
		case body.Action == "closed":
			event.Kind = EventPullRequestClosed
		}
		if event.PullRequestNumber == 0 {
			event.PullRequestNumber = body.Number
		}
	case "pull_request_review", "pull_request_review_comment":
		event.Kind = EventReviewSubmitted
		if event.PullRequestNumber == 0 {
			event.PullRequestNumber = body.Number
		}
	case "status", "check_run", "workflow_job":
		event.Kind = EventCheckRunUpdated
	}
	return event, nil
}

type giteaHook struct {
	ID     int64             `json:"id"`
	Type   string            `json:"type"`
	Active bool              `json:"active"`
	Events []string          `json:"events"`
	Config map[string]string `json:"config"`
}

// EnsureWebhook makes the repository's delivery match spec, verified by
// read-back.
//
// The secret travels as an argument, never on the spec: a WebhookSpec is
// logged, stored and compared, and a secret inside it would leak through all
// three. The read-back deliberately does NOT compare the secret — Gitea does
// not return it, and a check that cannot see the value must not pretend to
// verify it.
func (g *GiteaBackend) EnsureWebhook(ctx context.Context, repo RepoRef, spec WebhookSpec, secret []byte) error {
	if spec.URL == "" {
		return &InvalidInputError{Field: "url", Message: "a webhook must have somewhere to deliver to"}
	}
	if len(spec.Events) == 0 {
		return &InvalidInputError{Field: "events", Message: "a webhook subscribing to nothing delivers nothing"}
	}
	if len(secret) == 0 {
		return &InvalidInputError{
			Field:   "secret",
			Message: "an unsigned webhook cannot be verified, and an unverifiable delivery is discarded",
		}
	}

	var existing []giteaHook
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/hooks",
		out:       &existing,
		operation: "ListWebhooks",
	}); err != nil {
		return err
	}

	config := map[string]string{
		"url":          spec.URL,
		"content_type": "json",
		"secret":       string(secret),
	}

	var current *giteaHook
	for i := range existing {
		if existing[i].Config["url"] == spec.URL {
			current = &existing[i]
			break
		}
	}

	if current == nil {
		if err := g.do(ctx, giteaRequest{
			method: http.MethodPost,
			path:   repoPath(repo) + "/hooks",
			body: map[string]any{
				"type":   "gitea",
				"active": true,
				"events": spec.Events,
				"config": config,
			},
			operation: "CreateWebhook",
		}); err != nil {
			return err
		}
	} else {
		if err := g.do(ctx, giteaRequest{
			method: http.MethodPatch,
			path:   repoPath(repo) + "/hooks/" + strconv.FormatInt(current.ID, 10),
			body: map[string]any{
				"active": true,
				"events": spec.Events,
				"config": config,
			},
			operation: "UpdateWebhook",
		}); err != nil {
			return err
		}
	}

	// Read-back: the delivery must now exist, be active, and carry the events
	// asked for. The secret is not compared; see the doc comment.
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/hooks",
		out:       &existing,
		operation: "ListWebhooks",
	}); err != nil {
		return err
	}
	for i := range existing {
		if existing[i].Config["url"] != spec.URL {
			continue
		}
		if !existing[i].Active {
			return &ForgeWriteNotAppliedError{
				Type: ForgeGitea, Operation: "EnsureWebhook", Subject: repo.String(),
				Want: "active", Got: "inactive",
			}
		}
		// SUBSET, not equality: Gitea EXPANDS an event into its family.
		// Asking for "issues" yields issue_assign, issue_comment, issue_label,
		// issue_milestone and issues. Measured against a live 1.27.1 instance —
		// the fixture suite could not have known this, because a fixture
		// returns whatever it is told and equality looked right.
		//
		// The check still catches what it exists to catch: an event that was
		// asked for and is NOT configured means the write did not take effect.
		// Extra events are the server's own doing, not a failed write.
		// Both directions. A MISSING event means the write did not take
		// effect; an UNEXPECTED one means an update failed to remove a stale
		// subscription and the module is receiving deliveries outside its
		// spec. Accepting any superset would have called the second success.
		if missing := missingEvents(spec.Events, existing[i].Events); len(missing) > 0 {
			return &ForgeWriteNotAppliedError{
				Type: ForgeGitea, Operation: "EnsureWebhook", Subject: repo.String(),
				Want: normalizedLabels(spec.Events),
				Got:  normalizedLabels(existing[i].Events) + " (missing " + normalizedLabels(missing) + ")",
			}
		}
		if extra := unexpectedEvents(spec.Events, existing[i].Events); len(extra) > 0 {
			return &ForgeWriteNotAppliedError{
				Type: ForgeGitea, Operation: "EnsureWebhook", Subject: repo.String(),
				Want: normalizedLabels(spec.Events),
				Got:  normalizedLabels(existing[i].Events) + " (unexpected " + normalizedLabels(extra) + ")",
			}
		}
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type: ForgeGitea, Operation: "EnsureWebhook", Subject: repo.String(),
		Want: "a delivery to " + spec.URL, Got: "none",
	}
}

// =============================================================================
// Token custody
// =============================================================================

// Compile-time proof the custody plane is complete for this forge. Scoped to
// this interface: a forge may implement it or not, and one that does implements
// all of it.
var _ ForgeTokenLifecycle = (*GiteaBackend)(nil)

// giteaTokenPath is the token collection for one user.
func giteaTokenPath(username string) string {
	return "/users/" + url.PathEscape(username) + "/tokens"
}

// giteaToken is Gitea's access-token record. The secret appears only in a mint
// response, under sha1.
type giteaToken struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`

	// SHA1 is the token secret, present only in the mint response. Decoded
	// as raw JSON so the secret can be extracted into OWNED bytes and the
	// raw copy wiped, instead of materializing an unwipeable Go string
	// (review 982; the (a)-lite mint extension). The token LIST endpoint
	// never returns it, so readback decodes leave it nil.
	SHA1   json.RawMessage `json:"sha1"`
	Scopes []string        `json:"scopes"`
}

// rawJSONStringBytes extracts a JSON string's contents from its raw encoding
// into a fresh owned []byte, wiping the raw copy before returning. The fast
// path handles the unescaped form (every Gitea token secret: hex) without any
// string materialization; the escaped fallback pays one transient string —
// the irreducible transport-decode residue, go-quicbaas#97's secure-transport
// floor (the HTTP body buffer inside do() is the same scope).
func rawJSONStringBytes(raw json.RawMessage) ([]byte, error) {
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' && !bytes.ContainsRune(raw[1:len(raw)-1], '\\') {
		out := make([]byte, len(raw)-2)
		copy(out, raw[1:len(raw)-1])
		return out, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// TokenCapabilities reports what GITEA permits, measured against its API.
//
// SelfServiceMint is false, and the reason is worth stating because it drives
// the whole rotation design: Gitea's token endpoints are not usable by the
// token being replaced. Reaching a user's OWN tokens with a token requires the
// user-level write scope that also confers SSH-key management — the scope agent
// registration deliberately withholds — and reaching another user's requires
// basic auth outright. Rotation therefore runs on a provisioning-held
// password-equivalent, not on the expiring credential.
func (g *GiteaBackend) TokenCapabilities() TokenLifecycleCapabilities {
	return TokenLifecycleCapabilities{
		SelfServiceMint: false,
		ScopedTokens:    true,
		RevokeByName:    true,
	}
}

// MintToken issues a new token for the named user.
//
// Authenticates with the custody credential, never with the token being
// replaced — see TokenCapabilities.
func (g *GiteaBackend) MintToken(ctx context.Context, spec TokenSpec) (*MintedToken, error) {
	if g.custody.Username == "" {
		return nil, ErrCustodyCredentialAbsent
	}
	if spec.Username == "" {
		return nil, &InvalidInputError{Field: "username", Message: "must not be empty"}
	}
	if spec.Name == "" {
		return nil, &InvalidInputError{
			Field:   "name",
			Message: "must not be empty; an unnamed token cannot be found again to revoke",
		}
	}

	body := map[string]any{"name": spec.Name}
	if len(spec.Scopes) > 0 {
		body["scopes"] = spec.Scopes
	}

	var out giteaToken
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      giteaTokenPath(spec.Username),
		body:      body,
		out:       &out,
		operation: "gitea.MintToken",
		auth:      authCustody,
	}); err != nil {
		return nil, err
	}

	// The secret is disclosed exactly once, here, and leaves this function
	// as OWNED BYTES — extracted at the decode boundary with the raw copy
	// wiped (review 982). A mint that returns no secret is a failed mint
	// however cheerful its status code: the caller would store an empty
	// credential and only discover it at the next authenticated call.
	secret, decodeErr := rawJSONStringBytes(out.SHA1)
	if decodeErr != nil {
		return nil, backendError("gitea.MintToken.secret", decodeErr)
	}
	if len(secret) == 0 {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "mint token",
			Subject:   spec.Name,
			Want:      "a token secret",
			Got:       "none",
		}
	}
	// INTO THE ENCLAVE at the earliest possible moment (#97 rotation
	// slice): NewBufferFromBytes MOVES the extracted bytes into mlocked,
	// guard-paged memory and WIPES the source — after this line the only
	// live copy this function made is the protected one.
	buf, bufErr := secure.NewBufferFromBytes(secret)
	if bufErr != nil {
		return nil, backendError("gitea.MintToken.secret", bufErr)
	}

	return &MintedToken{
		ID:     strconv.FormatInt(out.ID, 10),
		Name:   out.Name,
		Secret: buf,
	}, nil
}

// VerifyToken reports who the given token authenticates as.
//
// Authenticates AS the supplied token. That is the entire point: checking with
// any other credential would prove the other credential works.
//
// token is borrowed under ForgeTokenLifecycle's no-retain contract: this
// adapter holds it only for the duration of the request it authenticates —
// the per-call giteaRequest is not retained — and keeps no copy afterwards.
// The one representation it cannot avoid is the transient string the HTTP
// Authorization header requires (see do), recorded as go-quicbaas#97
// secure-buffer custody debt.
func (g *GiteaBackend) VerifyToken(ctx context.Context, token []byte) (*TokenIdentity, error) {
	if len(token) == 0 {
		return nil, &InvalidInputError{Field: "token", Message: "must not be empty"}
	}

	var out giteaUser
	if err := g.do(ctx, giteaRequest{
		method:       http.MethodGet,
		path:         "/user",
		out:          &out,
		operation:    "gitea.VerifyToken",
		auth:         authSubjectToken,
		subjectToken: token,
	}); err != nil {
		return nil, err
	}

	if out.Login == "" {
		return nil, &TokenVerificationError{
			Name:     "(under test)",
			Expected: "an authenticated account",
			Actual:   "none",
		}
	}

	return &TokenIdentity{Username: out.Login}, nil
}

// RevokeToken destroys a token by name or id, verified by read-back.
//
// The read-back matters as much here as on any other write: Gitea has returned
// success for writes that did not take effect, and a revocation that silently
// did not happen leaves a live credential everything downstream believes is
// destroyed — the worst direction for this particular error.
func (g *GiteaBackend) RevokeToken(ctx context.Context, username, nameOrID string) error {
	if g.custody.Username == "" {
		return ErrCustodyCredentialAbsent
	}
	if username == "" {
		return &InvalidInputError{Field: "username", Message: "must not be empty"}
	}
	if nameOrID == "" {
		return &InvalidInputError{Field: "name_or_id", Message: "must not be empty"}
	}

	if err := g.do(ctx, giteaRequest{
		method:    http.MethodDelete,
		path:      giteaTokenPath(username) + "/" + url.PathEscape(nameOrID),
		operation: "gitea.RevokeToken",
		auth:      authCustody,
	}); err != nil {
		return err
	}

	var remaining []giteaToken
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodGet,
		path:      giteaTokenPath(username),
		out:       &remaining,
		operation: "gitea.RevokeToken.readback",
		auth:      authCustody,
	}); err != nil {
		return err
	}

	for _, t := range remaining {
		if t.Name == nameOrID || strconv.FormatInt(t.ID, 10) == nameOrID {
			return &ForgeWriteNotAppliedError{
				Type:      ForgeGitea,
				Operation: "revoke token",
				Subject:   nameOrID,
				Want:      "the token gone",
				Got:       "still listed",
			}
		}
	}

	return nil
}
