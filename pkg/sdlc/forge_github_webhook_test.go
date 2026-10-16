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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// githubWebhookSecret signs every delivery in these tests.
var githubWebhookSecret = []byte("s3cret")

// githubSign is the X-Hub-Signature-256 value GitHub sends for payload.
func githubSign(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return githubSignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubVerifyWebhook_AcceptsTheSignedPayload(t *testing.T) {
	backend, _ := newGitHubStub(t)
	payload := []byte(`{"action":"opened"}`)

	if err := backend.VerifyWebhook(payload, githubSign(payload, githubWebhookSecret), githubWebhookSecret); err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
}

func TestGitHubVerifyWebhook_RefusesEveryUnprovenDelivery(t *testing.T) {
	backend, _ := newGitHubStub(t)
	payload := []byte(`{"action":"opened"}`)
	valid := githubSign(payload, githubWebhookSecret)

	cases := map[string]struct {
		payload   []byte
		signature string
		secret    []byte
	}{
		"no secret":           {payload, valid, nil},
		"no signature":        {payload, "", githubWebhookSecret},
		"the legacy sha1":     {payload, "sha1=" + strings.TrimPrefix(valid, githubSignaturePrefix), githubWebhookSecret},
		"not hex":             {payload, "sha256=zz", githubWebhookSecret},
		"another secret":      {payload, githubSign(payload, []byte("other")), githubWebhookSecret},
		"a tampered payload":  {[]byte(`{"action":"closed"}`), valid, githubWebhookSecret},
		"a truncated digest":  {payload, valid[:len(valid)-2], githubWebhookSecret},
		"an unprefixed match": {payload, strings.TrimPrefix(valid, githubSignaturePrefix), githubWebhookSecret},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := backend.VerifyWebhook(tc.payload, tc.signature, tc.secret)
			if !errors.Is(err, ErrWebhookUnverified) {
				t.Fatalf("err = %v, want ErrWebhookUnverified", err)
			}
		})
	}
}

func TestGitHubParseWebhook_MapsEveryDelivery(t *testing.T) {
	backend, stub := newGitHubStub(t)

	cases := []struct {
		name      string
		eventType string
		payload   string
		want      ForgeEvent
	}{
		{"issue opened", "issues", `{"action":"opened","issue":{"number":3}}`,
			ForgeEvent{Kind: EventIssueOpened, IssueNumber: 3}},
		{"issue reopened", "Issues", `{"action":"reopened","issue":{"number":3}}`,
			ForgeEvent{Kind: EventIssueOpened, IssueNumber: 3}},
		{"issue edited", "issues", `{"action":"edited","issue":{"number":3}}`,
			ForgeEvent{Kind: EventIssueEdited, IssueNumber: 3}},
		{"issue closed", "issues", `{"action":"closed","issue":{"number":3}}`,
			ForgeEvent{Kind: EventIssueClosed, IssueNumber: 3}},
		{"issue unlabeled", "issues", `{"action":"unlabeled","issue":{"number":3}}`,
			ForgeEvent{Kind: EventIssueLabeled, IssueNumber: 3}},
		{"issue assigned", "issues", `{"action":"assigned","issue":{"number":3}}`,
			ForgeEvent{Kind: EventUnsupported, IssueNumber: 3}},
		{"comment on an issue", "issue_comment", `{"action":"created","issue":{"number":3,"pull_request":null}}`,
			ForgeEvent{Kind: EventIssueCommented, IssueNumber: 3}},
		{"comment edited", "issue_comment", `{"action":"edited","issue":{"number":3}}`,
			ForgeEvent{Kind: EventUnsupported, IssueNumber: 3}},
		{"comment on a pull request", "issue_comment", stub.load("webhook_issue_comment_on_pull.json"),
			ForgeEvent{Kind: EventUnsupported, PullRequestNumber: 12, Repo: githubRepo}},
		{"pull request opened", "pull_request", `{"action":"opened","pull_request":{"number":12,"head":{"sha":"` + githubHeadSHA + `"}}}`,
			ForgeEvent{Kind: EventPullRequestOpened, PullRequestNumber: 12, HeadSHA: githubHeadSHA}},
		{"pull request synchronized", "pull_request", `{"action":"synchronize","pull_request":{"number":12}}`,
			ForgeEvent{Kind: EventPullRequestUpdated, PullRequestNumber: 12}},
		{"pull request merged", "pull_request", stub.load("webhook_pull_request_closed.json"),
			ForgeEvent{Kind: EventPullRequestMerged, PullRequestNumber: 12, HeadSHA: githubHeadSHA, Repo: githubRepo}},
		{"pull request closed unmerged", "pull_request", `{"action":"closed","number":12,"pull_request":{"number":12,"merged":false}}`,
			ForgeEvent{Kind: EventPullRequestClosed, PullRequestNumber: 12}},
		{"pull request number at the top level", "pull_request", `{"action":"labeled","number":14}`,
			ForgeEvent{Kind: EventUnsupported, PullRequestNumber: 14}},
		// The review names the commit it judged, not where the head is now.
		{"review submitted", "pull_request_review", stub.load("webhook_review_submitted.json"),
			ForgeEvent{Kind: EventReviewSubmitted, PullRequestNumber: 12, HeadSHA: githubHeadSHA, Repo: githubRepo}},
		{"review comment", "pull_request_review_comment", `{"action":"created","pull_request":{"number":12}}`,
			ForgeEvent{Kind: EventReviewSubmitted, PullRequestNumber: 12}},
		{"workflow job", "workflow_job", stub.load("webhook_workflow_job.json"),
			ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: githubHeadSHA, Repo: githubRepo}},
		{"check run", "check_run", `{"action":"completed","check_run":{"head_sha":"` + githubHeadSHA + `"}}`,
			ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: githubHeadSHA}},
		{"check suite", "check_suite", `{"action":"completed","check_suite":{"head_sha":"` + githubHeadSHA + `"}}`,
			ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: githubHeadSHA}},
		{"workflow run", "workflow_run", `{"action":"completed","workflow_run":{"head_sha":"` + githubHeadSHA + `"}}`,
			ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: githubHeadSHA}},
		{"commit status", "status", `{"sha":"` + githubHeadSHA + `","state":"success"}`,
			ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: githubHeadSHA}},
		{"an unmapped event", "star", `{"action":"created","repository":{"name":"widgets"}}`,
			ForgeEvent{Kind: EventUnsupported, Repo: RepoRef{Repo: "widgets"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := backend.ParseWebhook(tc.eventType, []byte(tc.payload))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if *event != tc.want {
				t.Fatalf("event = %+v, want %+v", *event, tc.want)
			}
		})
	}
}

func TestGitHubParseWebhook_RefusesAnUndecodablePayload(t *testing.T) {
	backend, _ := newGitHubStub(t)

	if _, err := backend.ParseWebhook("issues", []byte(`{"action":`)); err == nil {
		t.Fatal("ParseWebhook accepted a truncated payload")
	}
}

// githubHookSpec is the delivery EnsureWebhook is asked for.
var githubHookSpec = WebhookSpec{URL: githubHookURL, Events: []string{"issues", "pull_request"}, SecretRef: "wh"}

func TestGitHubEnsureWebhook_CreatesAMissingHook(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubHooksPath,
		forgeJSON(http.StatusOK, []map[string]any{}),
		forgeJSON(http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true, "pull_request", "issues")}),
	)
	stub.json("POST "+githubHooksPath, http.StatusCreated, githubHookAt(7, githubHookURL, true, "issues", "pull_request"))

	if err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret); err != nil {
		t.Fatalf("EnsureWebhook: %v", err)
	}
	created := stub.requests("POST " + githubHooksPath)
	if len(created) != 1 {
		t.Fatalf("created %d hooks, want 1", len(created))
	}
	body := decodeBody(t, created[0])
	config, _ := body["config"].(map[string]any)
	if body["name"] != "web" || body["active"] != true || config["url"] != githubHookURL ||
		config["content_type"] != "json" || config["secret"] != string(githubWebhookSecret) || config["insecure_ssl"] != "0" {
		t.Fatalf("create body = %v", body)
	}
}

func TestGitHubEnsureWebhook_UpdatesTheExistingHook(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubHooksPath,
		forgeJSON(http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, false, "push")}),
		forgeJSON(http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true, "issues", "pull_request")}),
	)
	stub.json("PATCH "+githubHooksPath+"/7", http.StatusOK, githubHookAt(7, githubHookURL, true, "issues", "pull_request"))

	if err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret); err != nil {
		t.Fatalf("EnsureWebhook: %v", err)
	}
	if created := stub.requests("POST " + githubHooksPath); len(created) != 0 {
		t.Fatalf("created %d hooks beside the existing one", len(created))
	}
	updated := stub.requests("PATCH " + githubHooksPath + "/7")
	if len(updated) != 1 {
		t.Fatalf("updated %d times, want 1", len(updated))
	}
	if body := decodeBody(t, updated[0]); body["name"] != nil {
		t.Fatalf("an update renamed the hook: %v", body)
	}
}

func TestGitHubEnsureWebhook_RefusesAnIncompleteSpec(t *testing.T) {
	backend, stub := newGitHubStub(t)
	cases := map[string]struct {
		spec   WebhookSpec
		secret []byte
	}{
		"no url":    {WebhookSpec{Events: githubHookSpec.Events}, githubWebhookSecret},
		"no events": {WebhookSpec{URL: githubHookURL}, githubWebhookSecret},
		"no secret": {githubHookSpec, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var invalid *InvalidInputError
			if err := backend.EnsureWebhook(context.Background(), githubRepo, tc.spec, tc.secret); !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want InvalidInputError", err)
			}
		})
	}
	if calls := stub.requests("GET " + githubHooksPath); len(calls) != 0 {
		t.Fatalf("listed hooks %d times for an invalid spec", len(calls))
	}
}

func TestGitHubEnsureWebhook_RefusedCalls(t *testing.T) {
	t.Run("the listing is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusForbidden, map[string]string{"message": "admin required"})
		if err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret); err == nil {
			t.Fatal("EnsureWebhook succeeded without listing")
		}
	})

	t.Run("the create is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{})
		stub.json("POST "+githubHooksPath, http.StatusUnprocessableEntity,
			map[string]string{"message": "secret " + string(githubWebhookSecret) + " rejected"})
		err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret)
		if err == nil {
			t.Fatal("EnsureWebhook succeeded through a refused create")
		}
		if strings.Contains(err.Error(), string(githubWebhookSecret)) || strings.Contains(err.Error(), githubTestToken) {
			t.Fatalf("the error leaked a credential: %v", err)
		}
	})

	t.Run("the update is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true)})
		stub.json("PATCH "+githubHooksPath+"/7", http.StatusUnprocessableEntity,
			map[string]string{"message": "secret " + string(githubWebhookSecret) + " rejected"})
		err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret)
		if err == nil {
			t.Fatal("EnsureWebhook succeeded through a refused update")
		}
		if strings.Contains(err.Error(), string(githubWebhookSecret)) {
			t.Fatalf("the error leaked the webhook secret: %v", err)
		}
	})

	t.Run("the read-back fails", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.sequence("GET "+githubHooksPath,
			forgeJSON(http.StatusOK, []map[string]any{}),
			forgeJSON(http.StatusForbidden, map[string]string{"message": "admin required"}),
		)
		stub.json("POST "+githubHooksPath, http.StatusCreated, githubHookAt(7, githubHookURL, true))
		if err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret); err == nil {
			t.Fatal("EnsureWebhook succeeded without its read-back")
		}
	})
}

func TestGitHubEnsureWebhook_ReadBackMismatches(t *testing.T) {
	cases := map[string]map[string]any{
		"the hook is absent":     githubHookAt(8, "https://elsewhere.example/hook", true, "issues", "pull_request"),
		"the hook is inactive":   githubHookAt(7, githubHookURL, false, "issues", "pull_request"),
		"an event is missing":    githubHookAt(7, githubHookURL, true, "issues"),
		"an event is unexpected": githubHookAt(7, githubHookURL, true, "issues", "pull_request", "push"),
	}
	for name, after := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitHubStub(t)
			stub.sequence("GET "+githubHooksPath,
				forgeJSON(http.StatusOK, []map[string]any{}),
				forgeJSON(http.StatusOK, []map[string]any{after}),
			)
			stub.json("POST "+githubHooksPath, http.StatusCreated, after)
			var notApplied *ForgeWriteNotAppliedError
			err := backend.EnsureWebhook(context.Background(), githubRepo, githubHookSpec, githubWebhookSecret)
			if !errors.As(err, &notApplied) {
				t.Fatalf("err = %v, want ForgeWriteNotAppliedError", err)
			}
		})
	}
}

func TestWebhookDeliveryFromHeaders_ReadsTheBoundForgesNames(t *testing.T) {
	header := http.Header{}
	header.Set(GitHubEventHeader, "pull_request")
	header.Set(GitHubDeliveryHeader, "gh-1")
	header.Set(GitHubSignatureHeader, "sha256=ab")
	header.Set(GiteaEventHeader, "issues")

	got := WebhookDelivery{Header: header}.fromHeaders(ForgeGitHub)
	if got.EventType != "pull_request" || got.DeliveryID != "gh-1" || got.Signature != "sha256=ab" {
		t.Fatalf("delivery = %+v", got)
	}

	// What the transport already named is kept.
	kept := WebhookDelivery{EventType: "issues", DeliveryID: "d0", Signature: "s0", Header: header}.fromHeaders(ForgeGitHub)
	if kept.EventType != "issues" || kept.DeliveryID != "d0" || kept.Signature != "s0" {
		t.Fatalf("delivery = %+v", kept)
	}
}

func TestWebhookDeliveryFromHeaders_LeavesWhatItCannotRead(t *testing.T) {
	header := http.Header{}
	header.Set(GitHubEventHeader, "pull_request")

	if got := (WebhookDelivery{Header: header}).fromHeaders(ForgeType("bitbucket")); got.EventType != "" {
		t.Fatalf("an unknown forge filled the event: %+v", got)
	}
	if got := (WebhookDelivery{}).fromHeaders(ForgeGitHub); got.EventType != "" || got.Signature != "" {
		t.Fatalf("a delivery without headers changed: %+v", got)
	}
	// Gitea's names, not GitHub's: nothing a Gitea binding reads is present.
	if got := (WebhookDelivery{Header: header}).fromHeaders(ForgeGitea); got.EventType != "" {
		t.Fatalf("a Gitea binding read GitHub's header: %+v", got)
	}
}

// githubForges resolves every binding to one GitHub adapter.
type githubForges struct {
	backend *GitHubBackend
}

func (f *githubForges) ForgeFor(_ context.Context, _ string, _ ForgeConfig) (ForgeBackend, error) {
	return f.backend, nil
}

// newGitHubWebhookRig is newWebhookRig for a project bound to GitHub, verified
// and parsed by the real adapter.
func newGitHubWebhookRig(t *testing.T) *webhookRig {
	t.Helper()
	backend, _ := newGitHubStub(t)
	store := newTestStore(t)
	binding := testBinding()
	binding.Forge.Type = ForgeGitHub
	binding.Forge.Owner, binding.Forge.Repo = githubRepo.Owner, githubRepo.Repo
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	nudger := &fakeNudger{}
	secrets := &staticSecrets{secrets: map[string][]byte{"wh": githubWebhookSecret}}
	reviews := &fakeReviewTrigger{}
	ingestor, err := NewWebhookIngestor(&staticRoutes{tenants: map[string]string{testProject: testTenant}},
		store, secrets, &githubForges{backend: backend}, nudger, reviews)
	if err != nil {
		t.Fatal(err)
	}
	return &webhookRig{ingestor: ingestor, nudger: nudger, secrets: secrets, store: store, reviews: reviews}
}

// githubDeliveryHeader is the header GitHub sends with a signed payload.
func githubDeliveryHeader(event string, payload []byte) http.Header {
	header := http.Header{}
	header.Set(GitHubEventHeader, event)
	header.Set(GitHubDeliveryHeader, "gh-delivery-1")
	header.Set(GitHubSignatureHeader, githubSign(payload, githubWebhookSecret))
	return header
}

func TestWebhookIngest_GitHubDeliveryIsReadByItsOwnHeaders(t *testing.T) {
	rig := newGitHubWebhookRig(t)
	payload := []byte(`{"action":"opened","issue":{"number":3},"repository":{"name":"widgets","owner":{"login":"acme"}}}`)

	result, err := rig.ingestor.Ingest(context.Background(), WebhookDelivery{
		ProjectID: testProject, Payload: payload, Header: githubDeliveryHeader("issues", payload),
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.Nudged || result.Event.Kind != EventIssueOpened || result.Event.IssueNumber != 3 {
		t.Fatalf("result = %+v", result)
	}
}

func TestWebhookIngest_GitHubBindingRefusesOtherForgesHeaders(t *testing.T) {
	rig := newGitHubWebhookRig(t)
	payload := []byte(`{"action":"opened","issue":{"number":3}}`)
	header := http.Header{}
	header.Set(GiteaEventHeader, "issues")
	header.Set(GiteaDeliveryHeader, "d1")
	header.Set(GiteaSignatureHeader, "irrelevant")

	_, err := rig.ingestor.Ingest(context.Background(), WebhookDelivery{ProjectID: testProject, Payload: payload, Header: header})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidInputError", err)
	}
	rig.nudger.mu.Lock()
	nudges := len(rig.nudger.nudges)
	rig.nudger.mu.Unlock()
	if nudges != 0 {
		t.Fatalf("an unread delivery nudged the project %d times", nudges)
	}

	// An envelope with no body is refused before the binding is read at all.
	if _, err := rig.ingestor.Ingest(context.Background(), WebhookDelivery{ProjectID: testProject, Header: header}); !errors.As(err, &invalid) {
		t.Fatalf("empty body: err = %v, want InvalidInputError", err)
	}
}

func TestWebhookHTTP_GitHubDeliveries(t *testing.T) {
	rig := newGitHubWebhookRig(t)
	h, _ := newWebhookHTTP(t, rig, 1<<16)
	payload := []byte(`{"action":"opened","issue":{"number":3},"repository":{"name":"widgets","owner":{"login":"acme"}}}`)

	req := httptest.NewRequest(http.MethodPost, "/hook/"+testProject, strings.NewReader(string(payload)))
	req.Header = githubDeliveryHeader("issues", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed GitHub delivery: %d %s", rec.Code, rec.Body.String())
	}

	// The same body under Gitea's header names: the binding says GitHub, so
	// nothing is read from them and the delivery is unsigned.
	if rec := post(h, testProject, string(payload), githubSign(payload, githubWebhookSecret)); rec.Code != http.StatusBadRequest {
		t.Fatalf("Gitea headers on a GitHub binding: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/hook/"+testProject, strings.NewReader(string(payload)))
	req.Header = githubDeliveryHeader("issues", []byte(`{"forged":true}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("signature mismatch: %d %s", rec.Code, rec.Body.String())
	}
}
