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
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	glHooks         = glProject + "/hooks"
	gitlabHookURL   = "https://factory.example/webhooks/gitlab"
	gitlabHookToken = "hook-secret-never-shown"
)

// gitlabHookAnswer renders a project hook delivering to url with the named
// flags on and every other managed flag off.
func gitlabHookAnswer(id int64, url, alertStatus string, on ...string) map[string]any {
	hook := map[string]any{"id": id, "url": url, "alert_status": alertStatus}
	for _, flag := range gitlabManagedFlags {
		hook[flag] = slices.Contains(on, flag)
	}
	return hook
}

// =============================================================================
// Verification
// =============================================================================

func TestGitLabBackend_VerifyWebhook(t *testing.T) {
	backend, _ := newGitLabStub(t)
	secret := []byte(gitlabHookToken)
	if err := backend.VerifyWebhook([]byte(`{}`), gitlabHookToken, secret); err != nil {
		t.Fatalf("matching token refused: %v", err)
	}
	cases := map[string]struct {
		token  string
		secret []byte
	}{
		"token mismatch":   {"hook-secret-never-show", secret},
		"no token":         {"", secret},
		"no secret":        {gitlabHookToken, nil},
		"token is longer":  {gitlabHookToken + "x", secret},
		"case is not kept": {strings.ToUpper(gitlabHookToken), secret},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := backend.VerifyWebhook([]byte(`{}`), tc.token, tc.secret)
			if !errors.Is(err, ErrWebhookUnverified) {
				t.Fatalf("err = %v, want ErrWebhookUnverified", err)
			}
			if strings.Contains(err.Error(), gitlabHookToken) {
				t.Fatalf("the secret reached the error: %v", err)
			}
		})
	}
}

func TestWebhookDeliveryFromHeaders_GitLab(t *testing.T) {
	header := http.Header{}
	header.Set(GitLabEventHeader, "Merge Request Hook")
	header.Set(GitLabDeliveryHeader, "gl-1")
	header.Set(GitLabSignatureHeader, gitlabHookToken)
	header.Set(GitHubEventHeader, "pull_request")

	got := WebhookDelivery{Header: header}.fromHeaders(ForgeGitLab)
	if got.EventType != "Merge Request Hook" || got.DeliveryID != "gl-1" || got.Signature != gitlabHookToken {
		t.Fatalf("delivery = %+v", got)
	}
	if other := (WebhookDelivery{Header: header}).fromHeaders(ForgeGitea); other.Signature != "" {
		t.Fatalf("a Gitea binding read GitLab's token header: %+v", other)
	}
}

// =============================================================================
// Parsing
// =============================================================================

func TestGitLabBackend_ParseWebhook_Fixtures(t *testing.T) {
	backend, stub := newGitLabStub(t)
	cases := map[string]ForgeEvent{
		"hook_merge_request.json": {Kind: EventPullRequestUpdated, Repo: gitlabRepo, PullRequestNumber: 5, HeadSHA: gitlabHeadSHA},
		"hook_note.json":          {Kind: EventIssueCommented, Repo: gitlabRepo, IssueNumber: 7},
		"hook_pipeline.json":      {Kind: EventCheckRunUpdated, Repo: gitlabRepo, PullRequestNumber: 5, HeadSHA: gitlabHeadSHA},
		"hook_build.json":         {Kind: EventCheckRunUpdated, Repo: gitlabRepo, HeadSHA: gitlabHeadSHA},
		"hook_issue.json":         {Kind: EventIssueLabeled, Repo: gitlabRepo, IssueNumber: 7},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := backend.ParseWebhook("", []byte(stub.load(name)))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if got.Kind != want.Kind || got.Repo != want.Repo || got.IssueNumber != want.IssueNumber ||
				got.PullRequestNumber != want.PullRequestNumber || got.HeadSHA != want.HeadSHA {
				t.Fatalf("event = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGitLabBackend_ParseWebhook_Actions(t *testing.T) {
	backend, _ := newGitLabStub(t)
	project := `"project":{"path_with_namespace":"acme/widgets"}`
	cases := map[string]struct {
		payload string
		want    ForgeEventKind
	}{
		"issue opened":        {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"open"}}`, EventIssueOpened},
		"issue reopened":      {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"reopen"}}`, EventIssueOpened},
		"issue closed":        {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"close"}}`, EventIssueClosed},
		"issue edited":        {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"update"},"changes":{"title":{}}}`, EventIssueEdited},
		"issue labels null":   {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"update"},"changes":{"labels":null}}`, EventIssueEdited},
		"issue other action":  {`{"object_kind":"issue",` + project + `,"object_attributes":{"iid":7,"action":"move"}}`, EventUnsupported},
		"issue no attributes": {`{"object_kind":"issue",` + project + `}`, EventUnsupported},
		"mr opened":           {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"open"}}`, EventPullRequestOpened},
		"mr merged":           {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"merge"}}`, EventPullRequestMerged},
		"mr closed":           {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"close"}}`, EventPullRequestClosed},
		"mr approved":         {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"approved"}}`, EventReviewSubmitted},
		"mr unapproved":       {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"unapproved"}}`, EventReviewSubmitted},
		"mr edited":           {`{"object_kind":"merge_request",` + project + `,"object_attributes":{"iid":5,"action":"update"}}`, EventUnsupported},
		"mr no attributes":    {`{"object_kind":"merge_request",` + project + `}`, EventUnsupported},
		"note on mr":          {`{"object_kind":"note",` + project + `,"object_attributes":{"id":601},"merge_request":{"iid":5}}`, EventUnsupported},
		"note on a commit":    {`{"object_kind":"note",` + project + `,"object_attributes":{"id":601},"commit":{"id":"abc"}}`, EventUnsupported},
		"note on nothing":     {`{"object_kind":"note",` + project + `}`, EventUnsupported},
		"wiki page":           {`{"object_kind":"wiki_page",` + project + `}`, EventUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := backend.ParseWebhook("", []byte(tc.payload))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if got.Kind != tc.want || got.Repo != gitlabRepo {
				t.Fatalf("event = %+v, want %s on %s", got, tc.want, gitlabRepo)
			}
		})
	}
}

func TestGitLabBackend_ParseWebhook_Refusals(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.ParseWebhook("", []byte(`{"object_kind":`)); !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend for a body that is not JSON", err)
	}
	got, err := backend.ParseWebhook("", []byte(`{"object_kind":"push"}`))
	if err != nil || got.Kind != EventUnsupported || got.Repo != (RepoRef{}) {
		t.Fatalf("event = %+v, err = %v; want unsupported with no project", got, err)
	}
}

func TestGitLabRepoRef(t *testing.T) {
	if got := gitlabRepoRef("acme/platform/widgets"); got != (RepoRef{Owner: "acme/platform", Repo: "widgets"}) {
		t.Errorf("nested group = %+v, want the group kept in the owner", got)
	}
	if got := gitlabRepoRef("widgets"); got != (RepoRef{Repo: "widgets"}) {
		t.Errorf("bare path = %+v", got)
	}
}

// =============================================================================
// Configuration
// =============================================================================

func TestGitLabHook_Decoding(t *testing.T) {
	var hook gitlabHook
	if err := json.Unmarshal([]byte(`{"id":3,"url":"`+gitlabHookURL+`","issues_events":true,"note_events":true,"alert_status":"executable"}`), &hook); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if hook.ID != 3 || hook.URL != gitlabHookURL || !hook.active() {
		t.Fatalf("hook = %+v", hook)
	}
	if got := hook.events(); !slices.Equal(got, []string{"issue_comment", "issues"}) {
		t.Fatalf("events = %v", got)
	}
	if hook.Flags["push_events"] {
		t.Fatal("an absent flag decoded as on")
	}
	if disabled := (gitlabHook{AlertStatus: "disabled"}); disabled.active() {
		t.Fatal("a hook GitLab disabled reads as delivering")
	}

	for name, raw := range map[string]string{
		"not an object":    `[1]`,
		"id not a number":  `{"id":"three"}`,
		"flag not boolean": `{"id":3,"push_events":"yes"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var bad gitlabHook
			if err := json.Unmarshal([]byte(raw), &bad); err == nil {
				t.Fatalf("decoded %s", raw)
			}
		})
	}
}

func TestGitLabFlagsFor(t *testing.T) {
	flags, err := gitlabFlagsFor([]string{" Status ", "pull_request_review"})
	if err != nil {
		t.Fatalf("gitlabFlagsFor: %v", err)
	}
	if len(flags) != len(gitlabManagedFlags) {
		t.Fatalf("flags = %v, want every managed flag stated", flags)
	}
	if !flags["pipeline_events"] || !flags["job_events"] || !flags["merge_requests_events"] || flags["push_events"] {
		t.Fatalf("flags = %v", flags)
	}
	if _, err := gitlabFlagsFor([]string{"deployment"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput for an event GitLab has no hook for", err)
	}
}

func TestGitLabBackend_EnsureWebhook_CreatesTheHook(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glHooks,
		forgeJSON(http.StatusOK, []any{}),
		forgeJSON(http.StatusOK, []any{gitlabHookAnswer(3, gitlabHookURL, "executable", "merge_requests_events")}))
	stub.json("POST "+glHooks, http.StatusCreated, map[string]any{"id": 3})

	spec := WebhookSpec{URL: gitlabHookURL, Events: []string{"pull_request"}}
	if err := backend.EnsureWebhook(context.Background(), gitlabRepo, spec, []byte(gitlabHookToken)); err != nil {
		t.Fatalf("EnsureWebhook: %v", err)
	}
	calls := stub.requests("POST " + glHooks)
	if len(calls) != 1 {
		t.Fatalf("hook created %d times", len(calls))
	}
	body := decodeBody(t, calls[0])
	if body["url"] != gitlabHookURL || body["token"] != gitlabHookToken || body["enable_ssl_verification"] != true ||
		body["merge_requests_events"] != true || body["push_events"] != false {
		t.Fatalf("hook body = %+v", body)
	}
}

func TestGitLabBackend_EnsureWebhook_UpdatesTheExistingHook(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glHooks,
		forgeJSON(http.StatusOK, []any{
			gitlabHookAnswer(2, "https://elsewhere.example/hook", "executable", "push_events"),
			gitlabHookAnswer(3, gitlabHookURL, "executable", "push_events"),
		}),
		forgeJSON(http.StatusOK, []any{gitlabHookAnswer(3, gitlabHookURL, "", "pipeline_events", "job_events")}))
	stub.json("PUT "+glHooks+"/3", http.StatusOK, map[string]any{"id": 3})

	spec := WebhookSpec{URL: gitlabHookURL, Events: []string{"status"}}
	if err := backend.EnsureWebhook(context.Background(), gitlabRepo, spec, []byte(gitlabHookToken)); err != nil {
		t.Fatalf("EnsureWebhook: %v", err)
	}
	if calls := stub.requests("POST " + glHooks); len(calls) != 0 {
		t.Fatal("a second hook was created beside the existing one")
	}
	if body := decodeBody(t, stub.requests("PUT " + glHooks + "/3")[0]); body["push_events"] != false {
		t.Fatalf("a flag no longer requested was left on: %+v", body)
	}
}

func TestGitLabBackend_EnsureWebhook_ReadBackDisagrees(t *testing.T) {
	cases := map[string][]any{
		"no hook delivers": {},
		"hook disabled":    {gitlabHookAnswer(3, gitlabHookURL, "disabled", "merge_requests_events")},
		"flag missing":     {gitlabHookAnswer(3, gitlabHookURL, "executable")},
		"flag extra":       {gitlabHookAnswer(3, gitlabHookURL, "executable", "merge_requests_events", "push_events")},
	}
	for name, after := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.sequence("GET "+glHooks, forgeJSON(http.StatusOK, []any{}), forgeJSON(http.StatusOK, after))
			stub.json("POST "+glHooks, http.StatusCreated, map[string]any{"id": 3})
			spec := WebhookSpec{URL: gitlabHookURL, Events: []string{"pull_request"}}
			if err := backend.EnsureWebhook(context.Background(), gitlabRepo, spec, []byte(gitlabHookToken)); !errors.Is(err, ErrForgeWriteNotApplied) {
				t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
			}
		})
	}
}

func TestGitLabBackend_EnsureWebhook_Refusals(t *testing.T) {
	valid := WebhookSpec{URL: gitlabHookURL, Events: []string{"issues"}}
	secret := []byte(gitlabHookToken)
	for name, tc := range map[string]struct {
		spec   WebhookSpec
		secret []byte
	}{
		"no url":        {WebhookSpec{Events: valid.Events}, secret},
		"no events":     {WebhookSpec{URL: gitlabHookURL}, secret},
		"no secret":     {valid, nil},
		"unknown event": {WebhookSpec{URL: gitlabHookURL, Events: []string{"deployment"}}, secret},
	} {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			if err := backend.EnsureWebhook(context.Background(), gitlabRepo, tc.spec, tc.secret); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if calls := stub.requests("GET " + glHooks); len(calls) != 0 {
				t.Fatal("an invalid hook reached GitLab")
			}
		})
	}

	t.Run("listing refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glHooks, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if err := backend.EnsureWebhook(context.Background(), gitlabRepo, valid, secret); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("write refused without echoing the token", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glHooks, http.StatusOK, []any{})
		stub.json("POST "+glHooks, http.StatusUnprocessableEntity, map[string]any{"message": "token " + gitlabHookToken + " rejected"})
		err := backend.EnsureWebhook(context.Background(), gitlabRepo, valid, secret)
		if !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
		if strings.Contains(err.Error(), gitlabTestToken) || strings.Contains(err.Error(), gitlabHookToken) {
			t.Fatalf("a token reached the error: %v", err)
		}
	})
	t.Run("read-back refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.sequence("GET "+glHooks,
			forgeJSON(http.StatusOK, []any{}),
			forgeJSON(http.StatusForbidden, map[string]any{"message": "403 Forbidden"}))
		stub.json("POST "+glHooks, http.StatusCreated, map[string]any{"id": 3})
		if err := backend.EnsureWebhook(context.Background(), gitlabRepo, valid, secret); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
}
