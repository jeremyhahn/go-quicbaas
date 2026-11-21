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
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The headers a GitLab delivery carries.
const (
	// GitLabSignatureHeader carries the webhook's secret token, verbatim. GitLab
	// does not sign the payload; the token is compared in constant time.
	GitLabSignatureHeader = "X-Gitlab-Token"

	// GitLabEventHeader names the hook ("Merge Request Hook").
	GitLabEventHeader = "X-Gitlab-Event"

	// GitLabDeliveryHeader carries the delivery's unique id.
	GitLabDeliveryHeader = "X-Gitlab-Event-UUID"
)

// VerifyWebhook implements ForgeBackend: the delivery's token must equal the
// configured secret.
//
// GitLab sends the secret itself rather than a signature over the payload, so
// the token authenticates the sender but not the body; TLS protects the body
// in transit, which is why EnsureWebhook turns certificate verification on.
func (g *GitLabBackend) VerifyWebhook(_ []byte, token string, secret []byte) error {
	if len(secret) == 0 {
		return &WebhookUnverifiedError{Type: ForgeGitLab, Reason: "no secret token is configured"}
	}
	if token == "" {
		return &WebhookUnverifiedError{Type: ForgeGitLab, Reason: "delivery carried no token"}
	}
	if subtle.ConstantTimeCompare([]byte(token), secret) != 1 {
		return &WebhookUnverifiedError{Type: ForgeGitLab, Reason: "token does not match the configured secret"}
	}
	return nil
}

// =============================================================================
// Parsing
// =============================================================================

// gitlabHookPayload is the union of the hook bodies ParseWebhook reads. Every
// hook names its project and its object_kind; the rest depends on the kind.
type gitlabHookPayload struct {
	ObjectKind string `json:"object_kind"`
	Project    *struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes *struct {
		IID        int    `json:"iid"`
		Action     string `json:"action"`
		OldRev     string `json:"oldrev"`
		SHA        string `json:"sha"`
		LastCommit *struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
	Changes *struct {
		Labels json.RawMessage `json:"labels"`
	} `json:"changes"`
	Issue *struct {
		IID int `json:"iid"`
	} `json:"issue"`
	MergeRequest *struct {
		IID int `json:"iid"`
	} `json:"merge_request"`

	// SHA is the job hook's commit, which it carries at the top level.
	SHA string `json:"sha"`
}

// gitlabHookParsers maps a hook's object_kind onto what it means.
var gitlabHookParsers = map[string]func(*gitlabHookPayload, *ForgeEvent){
	"issue":         parseGitLabIssueHook,
	"merge_request": parseGitLabMergeRequestHook,
	"note":          parseGitLabNoteHook,
	"pipeline":      parseGitLabCIHook,
	"build":         parseGitLabCIHook,
}

// gitlabIssueKinds maps an issue hook's action onto the event it is, for
// every action but "update", which needs the changes.
var gitlabIssueKinds = map[string]ForgeEventKind{
	"open":   EventIssueOpened,
	"reopen": EventIssueOpened,
	"close":  EventIssueClosed,
}

// gitlabMergeRequestKinds maps a merge request hook's action onto the event it
// is, for every action but "update", which needs oldrev. An approval and its
// withdrawal arrive here: GitLab has no separate review hook.
var gitlabMergeRequestKinds = map[string]ForgeEventKind{
	"open":       EventPullRequestOpened,
	"reopen":     EventPullRequestOpened,
	"merge":      EventPullRequestMerged,
	"close":      EventPullRequestClosed,
	"approved":   EventReviewSubmitted,
	"unapproved": EventReviewSubmitted,
	"approval":   EventReviewSubmitted,
	"unapproval": EventReviewSubmitted,
}

// ParseWebhook maps a VERIFIED delivery onto a ForgeEvent. As with the other
// adapters, an unmapped hook is EventUnsupported rather than an error, and the
// result is only a hint the reconciler re-reads.
//
// The hook is read from the body's object_kind, which every GitLab hook
// carries, rather than from the event header, which names the same thing in
// prose ("Confidential Issue Hook" as well as "Issue Hook").
func (g *GitLabBackend) ParseWebhook(_ string, payload []byte) (*ForgeEvent, error) {
	var body gitlabHookPayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, backendError("ParseWebhook.decode", err)
	}

	event := &ForgeEvent{Kind: EventUnsupported}
	if body.Project != nil {
		event.Repo = gitlabRepoRef(body.Project.PathWithNamespace)
	}
	if parse, ok := gitlabHookParsers[body.ObjectKind]; ok {
		parse(&body, event)
	}
	return event, nil
}

// gitlabRepoRef splits a project's full path at its last slash: a nested
// group stays in the owner, which is how gitlabProjectPath joins it back.
func gitlabRepoRef(path string) RepoRef {
	cut := strings.LastIndex(path, "/")
	if cut < 0 {
		return RepoRef{Repo: path}
	}
	return RepoRef{Owner: path[:cut], Repo: path[cut+1:]}
}

func parseGitLabIssueHook(body *gitlabHookPayload, event *ForgeEvent) {
	attrs := body.ObjectAttributes
	if attrs == nil {
		return
	}
	event.IssueNumber = attrs.IID
	if kind, ok := gitlabIssueKinds[attrs.Action]; ok {
		event.Kind = kind
		return
	}
	if attrs.Action != "update" {
		return
	}
	event.Kind = EventIssueEdited
	if body.Changes != nil && len(body.Changes.Labels) > 0 && string(body.Changes.Labels) != "null" {
		event.Kind = EventIssueLabeled
	}
}

func parseGitLabMergeRequestHook(body *gitlabHookPayload, event *ForgeEvent) {
	attrs := body.ObjectAttributes
	if attrs == nil {
		return
	}
	event.PullRequestNumber = attrs.IID
	if attrs.LastCommit != nil {
		event.HeadSHA = attrs.LastCommit.ID
	}
	if kind, ok := gitlabMergeRequestKinds[attrs.Action]; ok {
		event.Kind = kind
		return
	}
	// An update with oldrev is a push to the source branch; any other update
	// is an edit, which, as on GitHub, nothing reconciles on.
	if attrs.Action == "update" && attrs.OldRev != "" {
		event.Kind = EventPullRequestUpdated
	}
}

// parseGitLabNoteHook maps a comment. Only a comment on an issue can be a
// ruling; a comment on a merge request names it and nothing more, as on
// GitHub.
//
// A note hook carries the object the note is on — issue, merge_request,
// commit or snippet — and only that one, so which object is present says
// what was commented on.
func parseGitLabNoteHook(body *gitlabHookPayload, event *ForgeEvent) {
	switch {
	case body.Issue != nil:
		event.IssueNumber = body.Issue.IID
		event.Kind = EventIssueCommented
	case body.MergeRequest != nil:
		event.PullRequestNumber = body.MergeRequest.IID
	}
}

// parseGitLabCIHook maps a pipeline or job hook: CI moved on a commit.
func parseGitLabCIHook(body *gitlabHookPayload, event *ForgeEvent) {
	event.Kind = EventCheckRunUpdated
	event.HeadSHA = body.SHA
	if body.ObjectAttributes != nil && body.ObjectAttributes.SHA != "" {
		event.HeadSHA = body.ObjectAttributes.SHA
	}
	if body.MergeRequest != nil {
		event.PullRequestNumber = body.MergeRequest.IID
	}
}

// =============================================================================
// Configuration
// =============================================================================

// gitlabEventFlags maps each event name the module subscribes by onto the
// project hook flags that deliver it. GitLab subscribes per flag, not per
// name, so a name is configured when every one of its flags is set.
//
// A merge request's approvals arrive on its own hook, so a review needs no
// flag of its own; a comment on an issue arrives as a note, which the issues
// family includes, as Gitea's does.
var gitlabEventFlags = map[string][]string{
	"issues":              {"issues_events", "note_events"},
	"issue_comment":       {"note_events"},
	"pull_request":        {"merge_requests_events"},
	"pull_request_review": {"merge_requests_events"},
	"status":              {"pipeline_events", "job_events"},
	"push":                {"push_events"},
}

// gitlabManagedFlags are every flag gitlabEventFlags sets, each of which a
// configured hook states explicitly — on or off — so a hook carries exactly
// the subscriptions asked for.
var gitlabManagedFlags = []string{
	"issues_events", "note_events", "merge_requests_events",
	"pipeline_events", "job_events", "push_events",
}

// gitlabHook is a project hook, decoded with its flags kept by name.
type gitlabHook struct {
	ID          int64
	URL         string
	AlertStatus string
	Flags       map[string]bool
}

// UnmarshalJSON reads a hook's id, url, delivery state and every managed
// flag.
func (h *gitlabHook) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var head struct {
		ID          int64  `json:"id"`
		URL         string `json:"url"`
		AlertStatus string `json:"alert_status"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return err
	}
	h.ID, h.URL, h.AlertStatus = head.ID, head.URL, head.AlertStatus
	h.Flags = make(map[string]bool, len(gitlabManagedFlags))
	for _, flag := range gitlabManagedFlags {
		var on bool
		if value, ok := fields[flag]; ok {
			if err := json.Unmarshal(value, &on); err != nil {
				return err
			}
		}
		h.Flags[flag] = on
	}
	return nil
}

// active reports whether GitLab will deliver: a hook GitLab disabled after
// repeated failures still lists, and delivers nothing.
func (h *gitlabHook) active() bool {
	return h.AlertStatus == "" || h.AlertStatus == "executable"
}

// events returns the event names the hook's flags deliver, in a stable order.
func (h *gitlabHook) events() []string {
	var names []string
	for name, flags := range gitlabEventFlags {
		if !slices.ContainsFunc(flags, func(flag string) bool { return !h.Flags[flag] }) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// gitlabFlagsFor returns the state of every managed flag for the requested
// event names, refusing a name GitLab has no flag for.
func gitlabFlagsFor(events []string) (map[string]bool, error) {
	flags := make(map[string]bool, len(gitlabManagedFlags))
	for _, flag := range gitlabManagedFlags {
		flags[flag] = false
	}
	for _, name := range events {
		set, ok := gitlabEventFlags[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, &InvalidInputError{
				Field:   "events",
				Message: "GitLab has no hook for the event " + strconv.Quote(name),
			}
		}
		for _, flag := range set {
			flags[flag] = true
		}
	}
	return flags, nil
}

// listHooks reads every hook on the project.
func (g *GitLabBackend) listHooks(ctx context.Context, repo RepoRef) ([]gitlabHook, error) {
	var hooks []gitlabHook
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListWebhooks",
		path:      gitlabProjectPath(repo) + "/hooks",
		maxPages:  maxLabelPages,
		exhausted: &IncompleteResponseError{
			Op:      "ListWebhooks " + repo.String(),
			Missing: "hooks beyond " + strconv.Itoa(maxLabelPages*gitlabPageSize),
		},
	}, func(page []gitlabHook) bool {
		hooks = append(hooks, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return hooks, nil
}

// gitlabHookTo returns the hook delivering to hookURL, or nil.
func gitlabHookTo(hooks []gitlabHook, hookURL string) *gitlabHook {
	i := slices.IndexFunc(hooks, func(hook gitlabHook) bool { return hook.URL == hookURL })
	if i < 0 {
		return nil
	}
	return &hooks[i]
}

// EnsureWebhook creates or updates the project hook delivering to spec.URL,
// verified by read-back: it must deliver, and carry exactly the flags the
// requested events need.
func (g *GitLabBackend) EnsureWebhook(ctx context.Context, repo RepoRef, spec WebhookSpec, secret []byte) error {
	if spec.URL == "" {
		return &InvalidInputError{Field: "url", Message: "a webhook must have somewhere to deliver to"}
	}
	if len(spec.Events) == 0 {
		return &InvalidInputError{Field: "events", Message: "a webhook subscribing to nothing delivers nothing"}
	}
	if len(secret) == 0 {
		return &InvalidInputError{
			Field:   "secret",
			Message: "a hook without a secret token cannot be verified, and an unverifiable delivery is discarded",
		}
	}
	flags, err := gitlabFlagsFor(spec.Events)
	if err != nil {
		return err
	}

	existing, err := g.listHooks(ctx, repo)
	if err != nil {
		return err
	}
	body := map[string]any{
		"url":                     spec.URL,
		"token":                   string(secret),
		"enable_ssl_verification": true,
	}
	for flag, on := range flags {
		body[flag] = on
	}
	request := forgeRequest{
		method:      http.MethodPost,
		path:        gitlabProjectPath(repo) + "/hooks",
		body:        body,
		operation:   "CreateWebhook",
		bodySecrets: [][]byte{secret},
	}
	if current := gitlabHookTo(existing, spec.URL); current != nil {
		request.method = http.MethodPut
		request.path += "/" + gitlabID(current.ID)
		request.operation = "UpdateWebhook"
	}
	if err := g.do(ctx, request); err != nil {
		return err
	}

	existing, err = g.listHooks(ctx, repo)
	if err != nil {
		return err
	}
	hook := gitlabHookTo(existing, spec.URL)
	if hook == nil {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitLab, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: "a delivery to " + spec.URL, Got: "none",
		}
	}
	if !hook.active() {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitLab, Operation: "EnsureWebhook", Subject: repo.String(),
			Want: "active", Got: hook.AlertStatus,
		}
	}
	for _, flag := range gitlabManagedFlags {
		if hook.Flags[flag] != flags[flag] {
			return &ForgeWriteNotAppliedError{
				Type: ForgeGitLab, Operation: "EnsureWebhook", Subject: repo.String(),
				Want: flag + "=" + strconv.FormatBool(flags[flag]),
				Got:  flag + "=" + strconv.FormatBool(hook.Flags[flag]),
			}
		}
	}
	return nil
}
