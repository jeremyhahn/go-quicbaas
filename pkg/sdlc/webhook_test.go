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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

type staticRoutes struct {
	tenants map[string]string
}

func (r *staticRoutes) Tenant(_ context.Context, projectID string) (string, error) {
	if tenant, ok := r.tenants[projectID]; ok {
		return tenant, nil
	}
	return "", &NotFoundError{Entity: webhookRouteEntity, ID: projectID}
}

type staticSecrets struct {
	secrets map[string][]byte
	err     error
}

func (s *staticSecrets) WebhookSecret(_ context.Context, _, ref string) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.secrets[ref]...), nil
}

type webhookRig struct {
	ingestor *WebhookIngestor
	forge    *fakeForge
	nudger   *fakeNudger
	secrets  *staticSecrets
	store    *Store
	reviews  *fakeReviewTrigger
}

func newWebhookRig(t *testing.T) *webhookRig {
	t.Helper()
	store := newTestStore(t)
	if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	nudger := &fakeNudger{}
	secrets := &staticSecrets{secrets: map[string][]byte{"wh": []byte("s3cret")}}
	reviews := &fakeReviewTrigger{}
	ingestor, err := NewWebhookIngestor(&staticRoutes{tenants: map[string]string{testProject: testTenant}}, store, secrets, &fakeForges{forge: forge}, nudger, reviews)
	if err != nil {
		t.Fatal(err)
	}
	return &webhookRig{ingestor: ingestor, forge: forge, nudger: nudger, secrets: secrets, store: store, reviews: reviews}
}

func delivery() WebhookDelivery {
	return WebhookDelivery{ProjectID: testProject, EventType: "issues", DeliveryID: "d1", Signature: "sig", Payload: []byte(`{"action":"opened"}`)}
}

func TestWebhookIngest_VerifiedDeliveryNudgesTheProject(t *testing.T) {
	rig := newWebhookRig(t)
	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil || !result.Nudged || result.TenantID != testTenant || result.ProjectID != testProject || result.Event.Kind != EventIssueOpened {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if len(rig.nudger.nudges) != 1 || rig.nudger.nudges[0] != testTenant+"/"+testProject {
		t.Fatalf("nudges = %v", rig.nudger.nudges)
	}
	rig.forge.parseEvent = &ForgeEvent{Kind: EventUnsupported}
	result, err = rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil || result.Nudged || len(rig.nudger.nudges) != 1 {
		t.Fatalf("unsupported event nudged: %+v %v", result, err)
	}
	rig.forge.parseEvent = &ForgeEvent{Kind: EventPullRequestOpened, Repo: RepoRef{Owner: "ACME", Repo: "API"}}
	if result, err := rig.ingestor.Ingest(context.Background(), delivery()); err != nil || !result.Nudged {
		t.Fatalf("case-different repository refused: %+v %v", result, err)
	}
	rig.forge.parseEvent = &ForgeEvent{Kind: EventPullRequestOpened, Repo: RepoRef{}}
	if result, err := rig.ingestor.Ingest(context.Background(), delivery()); err != nil || !result.Nudged {
		t.Fatalf("delivery naming no repository refused: %+v %v", result, err)
	}
}

func TestWebhookIngest_Refusals(t *testing.T) {
	rig := newWebhookRig(t)
	cases := map[string]struct {
		mutate func(*webhookRig, *WebhookDelivery)
		want   error
	}{
		"unknown project": {func(_ *webhookRig, d *WebhookDelivery) { d.ProjectID = "nope" }, ErrNotFound},
		"empty project":   {func(_ *webhookRig, d *WebhookDelivery) { d.ProjectID = "" }, ErrInvalidInput},
		"empty event":     {func(_ *webhookRig, d *WebhookDelivery) { d.EventType = "" }, ErrInvalidInput},
		"empty body":      {func(_ *webhookRig, d *WebhookDelivery) { d.Payload = nil }, ErrInvalidInput},
		"unsigned":        {func(_ *webhookRig, d *WebhookDelivery) { d.Signature = "" }, ErrInvalidInput},
		"bad signature": {func(r *webhookRig, _ *WebhookDelivery) {
			r.forge.verifyErr = &WebhookUnverifiedError{Type: ForgeGitea, Reason: "hmac"}
		}, ErrWebhookUnverified},
		"empty secret":      {func(r *webhookRig, _ *WebhookDelivery) { r.secrets.secrets["wh"] = nil }, ErrWebhookSecretMissing},
		"secret unreadable": {func(r *webhookRig, _ *WebhookDelivery) { r.secrets.err = errors.New("sealed") }, ErrBackend},
		"repo mismatch": {func(r *webhookRig, _ *WebhookDelivery) {
			r.forge.parseEvent = &ForgeEvent{Kind: EventIssueOpened, Repo: RepoRef{Owner: "other", Repo: "repo"}}
		}, ErrWebhookRepoMismatch},
		"unparseable": {func(r *webhookRig, _ *WebhookDelivery) {
			r.forge.parseErr = &InvalidInputError{Field: "payload", Message: "bad"}
		}, ErrInvalidInput},
		"nudge failed": {func(r *webhookRig, _ *WebhookDelivery) { r.nudger.err = errors.New("poller down") }, ErrBackend},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newWebhookRig(t)
			d := delivery()
			tc.mutate(rig, &d)
			_, err := rig.ingestor.Ingest(context.Background(), d)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(rig.nudger.nudges) != 0 {
				t.Fatal("a refused delivery nudged the poller")
			}
		})
	}
	// A binding with no webhook secret cannot verify anything.
	binding := testBinding()
	binding.Forge.WebhookSecretRef = ""
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.ingestor.Ingest(context.Background(), delivery()); !errors.Is(err, ErrWebhookSecretMissing) {
		t.Fatalf("no secret ref: %v", err)
	}
	// The secret handed to the adapter is wiped afterwards.
	secret := []byte("s3cret")
	zeroize(secret)
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatal("zeroize")
	}
	nilForge, _ := NewWebhookIngestor(&staticRoutes{tenants: map[string]string{testProject: testTenant}}, rig.store, rig.secrets, &fakeForges{}, rig.nudger, nil)
	binding.Forge.WebhookSecretRef = "wh"
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := nilForge.Ingest(context.Background(), delivery()); !errors.Is(err, ErrForgeNotRegistered) {
		t.Fatalf("nil forge: %v", err)
	}
}

func TestNewWebhookIngestor_RefusesIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	routes := &staticRoutes{}
	secrets := &staticSecrets{}
	forges := &fakeForges{}
	nudger := &fakeNudger{}
	// A nil review trigger is a NODE THAT REVIEWS NOTHING, which is a real
	// configuration: the constructor accepts it, and every other
	// collaborator remains required.
	if _, err := NewWebhookIngestor(routes, store, secrets, forges, nudger, nil); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][5]bool{
		"routes": {true, false, false, false, false}, "bindings": {false, true, false, false, false},
		"secrets": {false, false, true, false, false}, "forges": {false, false, false, true, false}, "nudger": {false, false, false, false, true},
	} {
		var r WebhookRouter = routes
		var b BindingReader = store
		var s WebhookSecretResolver = secrets
		var f ForgeResolver = forges
		var n ProjectNudger = nudger
		if args[0] {
			r = nil
		}
		if args[1] {
			b = nil
		}
		if args[2] {
			s = nil
		}
		if args[3] {
			f = nil
		}
		if args[4] {
			n = nil
		}
		if _, err := NewWebhookIngestor(r, b, s, f, n, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s nil accepted: %v", name, err)
		}
	}
}

func newWebhookHTTP(t *testing.T, rig *webhookRig, maxBody int64) (*WebhookHTTPHandler, *[]error) {
	t.Helper()
	var seen []error
	h, err := NewWebhookHTTPHandler(rig.ingestor, func(r *http.Request) string { return strings.TrimPrefix(r.URL.Path, "/hook/") }, maxBody,
		func(_ *http.Request, err error) { seen = append(seen, err) })
	if err != nil {
		t.Fatal(err)
	}
	return h, &seen
}

func post(h http.Handler, projectID, body, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/hook/"+projectID, strings.NewReader(body))
	req.Header.Set(GiteaEventHeader, "issues")
	req.Header.Set(GiteaDeliveryHeader, "d1")
	if signature != "" {
		req.Header.Set(GiteaSignatureHeader, signature)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWebhookHTTP_AcceptsAndRefuses(t *testing.T) {
	rig := newWebhookRig(t)
	h, seen := newWebhookHTTP(t, rig, 64)
	rec := post(h, testProject, `{"action":"opened"}`, "sig")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accepted: %d %s", rec.Code, rec.Body.String())
	}
	var accepted webhookAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil || !accepted.Accepted || !accepted.Nudged {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if rec := post(h, testProject, `{"action":"opened"}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsigned: %d", rec.Code)
	}
	if rec := post(h, "nope", `{"action":"opened"}`, "sig"); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "nope") {
		t.Fatalf("unknown project must be indistinguishable: %d %s", rec.Code, rec.Body.String())
	}
	rig.forge.verifyErr = &WebhookUnverifiedError{Type: ForgeGitea, Reason: "hmac"}
	if rec := post(h, testProject, `{"action":"opened"}`, "bad"); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "hmac") {
		t.Fatalf("bad signature: %d %s", rec.Code, rec.Body.String())
	}
	rig.forge.verifyErr = nil
	if rec := post(h, testProject, strings.Repeat("x", 65), "sig"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/hook/"+testProject, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET: %d", rec.Code)
	}
	if len(*seen) < 3 {
		t.Fatalf("the operator saw %d errors", len(*seen))
	}
}

// TestWebhookHTTP_ReportsATriageWakeFailureAndStillAccepts proves the
// handler accepts a delivery whose triage wake failed — the forge must not
// retry it — and hands the failure to the operator's error hook, so an
// outage of webhook-driven triage is logged rather than silent.
func TestWebhookHTTP_ReportsATriageWakeFailureAndStillAccepts(t *testing.T) {
	rig := newWebhookRig(t)
	cause := errors.New("the triage scheduler is unavailable")
	rig.ingestor.SetTriageNudger(&fakeNudger{err: cause})
	h, seen := newWebhookHTTP(t, rig, 64)
	rec := post(h, testProject, `{"action":"opened"}`, "sig")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a failed triage wake still accepts: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), cause.Error()) {
		t.Fatalf("the forge is not told the internal failure: %s", rec.Body.String())
	}
	if len(*seen) != 1 {
		t.Fatalf("the operator sees the triage failure once, got %d: %v", len(*seen), *seen)
	}
	var wake *TriageWakeError
	if !errors.As((*seen)[0], &wake) || !errors.Is((*seen)[0], cause) {
		t.Fatalf("the operator sees the typed failure, got %T: %v", (*seen)[0], (*seen)[0])
	}

	rig.ingestor.SetTriageNudger(&fakeNudger{})
	*seen = nil
	if rec := post(h, testProject, `{"action":"opened"}`, "sig"); rec.Code != http.StatusAccepted || len(*seen) != 0 {
		t.Fatalf("a healthy wake reports nothing: %d %v", rec.Code, *seen)
	}
}

func TestWebhookRefusal_Mapping(t *testing.T) {
	for err, want := range map[error]int{
		&WebhookUnverifiedError{}:          http.StatusUnauthorized,
		&WebhookSecretMissingError{}:       http.StatusUnauthorized,
		&WebhookRepoMismatchError{}:        http.StatusBadRequest,
		&InvalidInputError{}:               http.StatusBadRequest,
		&NotFoundError{}:                   http.StatusUnauthorized,
		&ForgeNotRegisteredError{}:         http.StatusNotImplemented,
		backendError("x", errors.New("y")): http.StatusBadGateway,
		errors.New("mystery"):              http.StatusInternalServerError,
	} {
		if status, _ := webhookRefusal(err); status != want {
			t.Fatalf("%T → %d, want %d", err, status, want)
		}
	}
	if _, err := NewWebhookHTTPHandler(nil, nil, 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil ingestor")
	}
	rig := newWebhookRig(t)
	if _, err := NewWebhookHTTPHandler(rig.ingestor, nil, 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil params")
	}
	if _, err := NewWebhookHTTPHandler(rig.ingestor, func(*http.Request) string { return "" }, 0, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unbounded body")
	}
}

func TestWebhookRouteIndex_RecordTenantForget(t *testing.T) {
	n := qrdbnode.Shared(t)
	index, err := NewWebhookRouteIndex(n.Transaction, n.KV)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	projectID := "route-" + t.Name()
	if _, err := index.Tenant(ctx, projectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent route: %v", err)
	}
	if err := index.Record(ctx, projectID, testTenant); err != nil {
		t.Fatal(err)
	}
	if err := index.Record(ctx, projectID, testTenant); err != nil {
		t.Fatalf("re-recording the same tenant: %v", err)
	}
	if err := index.Record(ctx, projectID, "tenant-b"); !errors.Is(err, ErrWebhookRouteConflict) {
		t.Fatalf("another tenant's claim: %v", err)
	}
	if tenant, err := index.Tenant(ctx, projectID); err != nil || tenant != testTenant {
		t.Fatalf("Tenant = %q, %v", tenant, err)
	}
	if err := index.Forget(ctx, projectID, "tenant-b"); err != nil {
		t.Fatalf("forget by another tenant must be a no-op: %v", err)
	}
	if tenant, err := index.Tenant(ctx, projectID); err != nil || tenant != testTenant {
		t.Fatalf("route lost to a stranger's forget: %q %v", tenant, err)
	}
	if err := index.Forget(ctx, projectID, testTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Tenant(ctx, projectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after forget: %v", err)
	}
	if err := index.Forget(ctx, projectID, testTenant); err != nil {
		t.Fatalf("double forget: %v", err)
	}
	if err := index.Record(ctx, projectID, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty tenant: %v", err)
	}
	if err := index.Record(ctx, "a/b", testTenant); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("bad project id: %v", err)
	}
	if _, err := NewWebhookRouteIndex(nil, n.KV); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil transactor")
	}
	var conflict *WebhookRouteConflictError
	if err := index.Record(ctx, projectID, "tenant-c"); err == nil {
		// forgotten above; recorded afresh for tenant-c, then a conflict for tenant-d
		if err := index.Record(ctx, projectID, "tenant-d"); !errors.As(err, &conflict) || conflict.ProjectID != projectID {
			t.Fatalf("conflict shape: %v", err)
		}
	}
}

// =============================================================================
// Review dispatch
// =============================================================================

func TestWebhookIngest_APullRequestDeliveryAsksForAReviewAndStillNudges(t *testing.T) {
	rig := newWebhookRig(t)
	rig.reviews.started = true
	rig.forge.parseEvent = &ForgeEvent{Kind: EventPullRequestOpened, Repo: testRepoRef, PullRequestNumber: 41}

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.ReviewStarted {
		t.Fatal("a pull-request delivery started no review")
	}
	if got := rig.reviews.requests(); len(got) != 1 || got[0].PullRequest != 41 {
		t.Fatalf("review requests = %v", got)
	}
	// THE REVIEW IS AN ADDITIONAL CONSEQUENCE, NOT AN ALTERNATIVE. The
	// poller stays the single path an issue becomes a Task through,
	// whatever else a delivery is about.
	if !result.Nudged || len(rig.nudger.nudges) != 1 {
		t.Fatalf("a pull-request delivery skipped the nudge: %+v", result)
	}
}

func TestWebhookIngest_AnIssueDeliveryAsksForNoReview(t *testing.T) {
	rig := newWebhookRig(t)
	rig.reviews.started = true
	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.ReviewStarted || len(rig.reviews.requests()) != 0 {
		t.Fatalf("an issue delivery asked for a review: %+v", result)
	}
}

// TestWebhookIngest_APollOnlyProjectIgnoresTheReviewHalfOfADelivery proves
// the two discovery paths are genuinely independent at the ingestor: a
// project that has said its reviews are found by polling ignores the review
// half of a delivery, and STILL nudges the poller — because the mode selects
// how reviews are discovered, not whether a verified delivery is worth
// anything.
func TestWebhookIngest_APollOnlyProjectIgnoresTheReviewHalfOfADelivery(t *testing.T) {
	rig := newWebhookRig(t)
	rig.reviews.started = true
	binding := testBinding()
	binding.Discovery = DiscoveryPoll
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	rig.forge.parseEvent = &ForgeEvent{Kind: EventCheckRunUpdated, Repo: testRepoRef, PullRequestNumber: 41}

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.ReviewStarted || len(rig.reviews.requests()) != 0 {
		t.Fatalf("a poll-only project reviewed from a delivery: %+v", result)
	}
	if !result.Nudged {
		t.Fatal("a poll-only project must still have its poller nudged")
	}
}

func TestWebhookIngest_AFailedReviewDispatchIsReported(t *testing.T) {
	// Not swallowed: a delivery that verified and then could not start the
	// review it implies is an outage, and reporting it is how the forge's
	// own delivery log shows the retry.
	rig := newWebhookRig(t)
	rig.reviews.err = errors.New("engine is down")
	rig.forge.parseEvent = &ForgeEvent{Kind: EventPullRequestUpdated, Repo: testRepoRef, PullRequestNumber: 41}
	if _, err := rig.ingestor.Ingest(context.Background(), delivery()); err == nil {
		t.Fatal("a failed review dispatch was swallowed")
	}
}
