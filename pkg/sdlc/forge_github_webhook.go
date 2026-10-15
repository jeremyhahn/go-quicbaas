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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// =============================================================================
// GitHub: webhooks
// =============================================================================

// The headers a GitHub delivery arrives with; see the Gitea constants for why
// they are exported.
const (
	// GitHubSignatureHeader carries "sha256=" and the hex HMAC-SHA256 of the
	// body.
	GitHubSignatureHeader = "X-Hub-Signature-256"

	// GitHubEventHeader names the event type ParseWebhook maps.
	GitHubEventHeader = "X-GitHub-Event"

	// GitHubDeliveryHeader uniquely identifies one delivery.
	GitHubDeliveryHeader = "X-GitHub-Delivery"
)

// githubSignaturePrefix names the digest in GitHub's signature header.
const githubSignaturePrefix = "sha256="

// VerifyWebhook authenticates a delivery, fail-closed and in constant time,
// exactly as the Gitea adapter does once GitHub's "sha256=" prefix is removed.
// A signature without the prefix is refused: it is either the legacy SHA-1
// header or something nobody signed.
func (g *GitHubBackend) VerifyWebhook(payload []byte, signature string, secret []byte) error {
	if len(secret) == 0 {
		return &WebhookUnverifiedError{Type: ForgeGitHub, Reason: "no signing secret is configured"}
	}
	if signature == "" {
		return &WebhookUnverifiedError{Type: ForgeGitHub, Reason: "delivery carried no signature"}
	}
	digest, ok := strings.CutPrefix(strings.TrimSpace(signature), githubSignaturePrefix)
	if !ok {
		return &WebhookUnverifiedError{Type: ForgeGitHub, Reason: "signature is not sha256"}
	}
	provided, err := hex.DecodeString(digest)
	if err != nil {
		return &WebhookUnverifiedError{Type: ForgeGitHub, Reason: "signature is not hex"}
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return &WebhookUnverifiedError{Type: ForgeGitHub, Reason: "signature does not match the payload"}
	}
	return nil
}

// githubHeadCommit is the commit a CI delivery names.
type githubHeadCommit struct {
	HeadSHA string `json:"head_sha"`
}

// githubHookPayload is the union of the delivery fields the module reads.
type githubHookPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Issue  *struct {
		Number      int             `json:"number"`
		PullRequest json.RawMessage `json:"pull_request"`
	} `json:"issue"`
	PullRequest *struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Review *struct {
		CommitID string `json:"commit_id"`
	} `json:"review"`

	// The CI deliveries: each names its commit in its own object, and a
	// commit status names it at the top level.
	SHA         string            `json:"sha"`
	CheckRun    *githubHeadCommit `json:"check_run"`
	CheckSuite  *githubHeadCommit `json:"check_suite"`
	WorkflowJob *githubHeadCommit `json:"workflow_job"`
	WorkflowRun *githubHeadCommit `json:"workflow_run"`

	Repository *struct {
		Name  string      `json:"name"`
		Owner *githubUser `json:"owner"`
	} `json:"repository"`
}

// headSHA returns the commit a delivery names, preferring the most specific.
func (p *githubHookPayload) headSHA() string {
	if p.Review != nil && p.Review.CommitID != "" {
		return p.Review.CommitID
	}
	if p.PullRequest != nil && p.PullRequest.Head.SHA != "" {
		return p.PullRequest.Head.SHA
	}
	for _, ci := range []*githubHeadCommit{p.CheckRun, p.CheckSuite, p.WorkflowJob, p.WorkflowRun} {
		if ci != nil && ci.HeadSHA != "" {
			return ci.HeadSHA
		}
	}
	return p.SHA
}

// githubIssueKinds maps an issues delivery's action onto the event it is.
// GitHub announces every label change as labeled or unlabeled.
var githubIssueKinds = map[string]ForgeEventKind{
	"opened":    EventIssueOpened,
	"reopened":  EventIssueOpened,
	"edited":    EventIssueEdited,
	"closed":    EventIssueClosed,
	"labeled":   EventIssueLabeled,
	"unlabeled": EventIssueLabeled,
}

// githubPullKinds maps a pull_request delivery's action onto the event it is,
// for every action but "closed", which needs the merged flag.
var githubPullKinds = map[string]ForgeEventKind{
	"opened":      EventPullRequestOpened,
	"reopened":    EventPullRequestOpened,
	"synchronize": EventPullRequestUpdated,
}

// githubCIEvents are the deliveries that report CI against a commit.
var githubCIEvents = map[string]bool{
	"status":       true,
	"check_run":    true,
	"check_suite":  true,
	"workflow_job": true,
	"workflow_run": true,
}

// ParseWebhook maps a VERIFIED delivery onto a ForgeEvent. As with Gitea, an
// unmapped event is EventUnsupported rather than an error, and the result is
// only a hint the reconciler re-reads.
func (g *GitHubBackend) ParseWebhook(eventType string, payload []byte) (*ForgeEvent, error) {
	var body githubHookPayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, backendError("ParseWebhook.decode", err)
	}

	event := &ForgeEvent{Kind: EventUnsupported, HeadSHA: body.headSHA()}
	if body.Repository != nil {
		event.Repo.Repo = body.Repository.Name
		if body.Repository.Owner != nil {
			event.Repo.Owner = body.Repository.Owner.Login
		}
	}
	if body.PullRequest != nil {
		event.PullRequestNumber = body.PullRequest.Number
	}

	// GitHub delivers an issue_comment for a comment on a pull request too,
	// marking it with issue.pull_request; such a comment names the pull
	// request, not an issue.
	onPull := body.Issue != nil && len(body.Issue.PullRequest) > 0 && string(body.Issue.PullRequest) != "null"
	if body.Issue != nil {
		if onPull {
			event.PullRequestNumber = body.Issue.Number
		} else {
			event.IssueNumber = body.Issue.Number
		}
	}

	switch eventType = strings.ToLower(eventType); {
	case eventType == "issues":
		if kind, ok := githubIssueKinds[body.Action]; ok {
			event.Kind = kind
		}
	case eventType == "issue_comment":
		// Only a NEW comment on an issue can be a ruling.
		if body.Action == "created" && event.IssueNumber > 0 {
			event.Kind = EventIssueCommented
		}
	case eventType == "pull_request":
		if kind, ok := githubPullKinds[body.Action]; ok {
			event.Kind = kind
		} else if body.Action == "closed" && body.PullRequest != nil && body.PullRequest.Merged {
			event.Kind = EventPullRequestMerged
		} else if body.Action == "closed" {
			event.Kind = EventPullRequestClosed
		}
		if event.PullRequestNumber == 0 {
			event.PullRequestNumber = body.Number
		}
	case eventType == "pull_request_review", eventType == "pull_request_review_comment":
		event.Kind = EventReviewSubmitted
	case githubCIEvents[eventType]:
		event.Kind = EventCheckRunUpdated
	}
	return event, nil
}

type githubHook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		// URL alone is read: GitHub masks the secret and types the other
		// settings inconsistently across versions.
		URL string `json:"url"`
	} `json:"config"`
}

// listHooks returns every webhook on the repository.
func (g *GitHubBackend) listHooks(ctx context.Context, repo RepoRef) ([]githubHook, error) {
	var hooks []githubHook
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListWebhooks",
		path:      repoPath(repo) + "/hooks",
		maxPages:  maxLabelPages,
		exhausted: &IncompleteResponseError{
			Op:      "ListWebhooks " + repo.String(),
			Missing: "hooks beyond " + strconv.Itoa(maxLabelPages*githubPageSize),
		},
	}, func(page []githubHook) bool {
		hooks = append(hooks, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return hooks, nil
}

// EnsureWebhook makes the repository's delivery match spec, verified by
// read-back. The secret travels as an argument and is never compared on the
// way back — GitHub masks it — for the reasons the Gitea adapter gives.
//
// GitHub does not expand an event into a family, so the read-back's
// both-directions comparison is exact here.
func (g *GitHubBackend) EnsureWebhook(ctx context.Context, repo RepoRef, spec WebhookSpec, secret []byte) error {
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

	existing, err := g.listHooks(ctx, repo)
	if err != nil {
		return err
	}
	config := map[string]string{
		"url":          spec.URL,
		"content_type": "json",
		"secret":       string(secret),
		"insecure_ssl": "0",
	}
	body := map[string]any{"active": true, "events": spec.Events, "config": config}

	request := forgeRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/hooks",
		body:      body,
		operation: "CreateWebhook",
	}
	if current := findHook(existing, spec.URL); current != nil {
		request = forgeRequest{
			method:    http.MethodPatch,
			path:      repoPath(repo) + "/hooks/" + strconv.FormatInt(current.ID, 10),
			body:      body,
			operation: "UpdateWebhook",
		}
	} else {
		body["name"] = "web"
	}
	if err := g.do(ctx, request); err != nil {
		return err
	}

	existing, err = g.listHooks(ctx, repo)
	if err != nil {
		return err
	}
	hook := findHook(existing, spec.URL)
	if hook == nil {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: "a delivery to " + spec.URL, Got: "none",
		}
	}
	if !hook.Active {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: "active", Got: "inactive",
		}
	}
	if missing := missingEvents(spec.Events, hook.Events); len(missing) > 0 {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: normalizedLabels(spec.Events),
			Got:  normalizedLabels(hook.Events) + " (missing " + normalizedLabels(missing) + ")",
		}
	}
	if extra := unexpectedEvents(spec.Events, hook.Events); len(extra) > 0 {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: normalizedLabels(spec.Events),
			Got:  normalizedLabels(hook.Events) + " (unexpected " + normalizedLabels(extra) + ")",
		}
	}
	return nil
}

// findHook returns the webhook delivering to hookURL, or nil.
func findHook(hooks []githubHook, hookURL string) *githubHook {
	for i := range hooks {
		if hooks[i].Config.URL == hookURL {
			return &hooks[i]
		}
	}
	return nil
}
