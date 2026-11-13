//go:build integration

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

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// =============================================================================
// The webhook ingress, end to end on a live node
// =============================================================================
//
// The route is public at the PEP layer; its authentication is the HMAC over
// the body, verified through the REAL Gitea adapter against the secret the
// binding names in the tenant's secrets. A verified delivery nudges the
// task-source poller; every refusal is indistinguishable to the caller.

// bindGiteaProject binds a project to the REAL gitea adapter, pointed at an
// in-memory Gitea that remembers.
//
// The base URL used to be an unreachable address, because only the signature
// path mattered here. Binding now ONBOARDS — it provisions the repository's
// webhook and labels through this same adapter — so an unreachable forge
// would make every test in this file fail at setup. A live fixture also
// makes the test truer: the delivery the node later verifies is one the node
// itself configured.
func bindGiteaProject(t *testing.T, srv *server.Server, repo string) string {
	t.Helper()
	gitea := giteafake.New(t, e2eOwner, e2eRepo)
	backend := newE2EBackend()
	projectID := registerFactoryProject(t, srv, backend, repo)
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	_, err := srv.SDLCService().Bind(ctx, loopE2ETenant, &sdlc.ProjectBinding{
		ProjectID: projectID,
		Forge: sdlc.ForgeConfig{
			Type: sdlc.ForgeGitea, BaseURL: gitea.APIURL(), Owner: e2eOwner, Repo: e2eRepo,
			TriggerLabel:     "task:ready",
			WebhookSecretRef: e2eWebhookRef, TokenSecretRef: e2eTokenRef,
		},
		Strategy: sdlc.GitHubFlowName,
		Worker:   orchestration.AgentRefTo(loopE2EWorkerAgent),
		Review:   sdlc.ReviewPolicy{RequiredApprovals: 1},
	}, service.BindOptions{})
	require.NoError(t, err)

	// Onboarding actually happened: the node provisioned its own delivery
	// and the binding's vocabulary. A bind that quietly skipped this is the
	// regression the whole feature exists to prevent.
	hooks := gitea.HookURLs()
	require.Len(t, hooks, 1, "binding did not provision a webhook")
	require.Contains(t, hooks[0], sdlc.WebhookPathPrefix+projectID)
	require.Equal(t, []string{"task:ready"}, gitea.LabelNames(), "binding did not provision the label vocabulary")

	return projectID
}

func webhookRouter(srv *server.Server) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/sdlc/webhook/{projectID}", srv.SDLCWebhookHandler())
	return r
}

func signedIssueDelivery(t *testing.T, secret string, number int) (body []byte, signature string) {
	t.Helper()
	payload := map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": number, "title": "an issue", "body": "a body", "state": "open", "labels": []map[string]any{},
		},
		"repository": map[string]any{"name": e2eRepo, "owner": map[string]any{"login": e2eOwner}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return body, hex.EncodeToString(mac.Sum(nil))
}

func deliver(t *testing.T, h http.Handler, projectID string, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sdlc/webhook/"+projectID, bytes.NewReader(body))
	req.Header.Set(sdlc.GiteaEventHeader, "issues")
	req.Header.Set(sdlc.GiteaDeliveryHeader, "delivery-1")
	if signature != "" {
		req.Header.Set(sdlc.GiteaSignatureHeader, signature)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSDLCWebhook_SignedDeliveryIsAcceptedAndNudgesThePoller(t *testing.T) {
	registerLoopForge(t)
	repo := bareFactoryRepo(t)
	srv := startFactoryNode(t, t.TempDir(), true, time.Hour, 1)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	projectID := bindGiteaProject(t, srv, repo)
	require.NotNil(t, srv.SDLCWebhookHandler(), "the webhook handler was not built")
	body, signature := signedIssueDelivery(t, e2eSecretValue, 41)
	rec := deliver(t, webhookRouter(srv), projectID, body, signature)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var accepted struct {
		Accepted bool `json:"accepted"`
		Nudged   bool `json:"nudged"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	require.True(t, accepted.Accepted)
	require.True(t, accepted.Nudged, "a verified delivery must nudge the poller for its project")
	t.Logf("OBSERVED: status=%d accepted=%v nudged=%v", rec.Code, accepted.Accepted, accepted.Nudged)
}

func TestSDLCWebhook_UnsignedAndForgedAreRefusedIdentically(t *testing.T) {
	registerLoopForge(t)
	repo := bareFactoryRepo(t)
	srv := startFactoryNode(t, t.TempDir(), true, time.Hour, 1)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	projectID := bindGiteaProject(t, srv, repo)
	router := webhookRouter(srv)
	body, goodSignature := signedIssueDelivery(t, e2eSecretValue, 42)
	mac := hmac.New(sha256.New, []byte("not-the-secret"))
	mac.Write(body)
	unsigned := deliver(t, router, projectID, body, "")
	wrongKey := deliver(t, router, projectID, body, hex.EncodeToString(mac.Sum(nil)))
	unknownProject := deliver(t, router, "no-such-project", body, goodSignature)
	for name, rec := range map[string]*httptest.ResponseRecorder{"unsigned": unsigned, "wrong key": wrongKey, "unknown project": unknownProject} {
		require.NotEqual(t, http.StatusAccepted, rec.Code, "%s delivery was ACCEPTED; verification is not fail-closed", name)
		t.Logf("OBSERVED: %s -> status=%d body=%s", name, rec.Code, rec.Body.String())
	}
	require.Equal(t, wrongKey.Code, unknownProject.Code, "project existence is an oracle")
	require.Equal(t, wrongKey.Body.String(), unknownProject.Body.String(), "project existence is an oracle")
	require.Equal(t, http.StatusUnauthorized, wrongKey.Code)
	require.Equal(t, http.StatusBadRequest, unsigned.Code)
}

// TestSDLCSecrets_StoredSecretSurvivesRestartAndReachesTheModule proves the
// secret the binding names is served from the persistent store after a
// restart — the webhook verifies against it on the successor too.
func TestSDLCSecrets_StoredSecretSurvivesRestartAndReachesTheModule(t *testing.T) {
	registerLoopForge(t)
	repo := bareFactoryRepo(t)
	dataDir := t.TempDir()
	srv := startFactoryNode(t, dataDir, true, time.Hour, 1)
	projectID := bindGiteaProject(t, srv, repo)
	body, signature := signedIssueDelivery(t, e2eSecretValue, 61)
	require.Equal(t, http.StatusAccepted, deliver(t, webhookRouter(srv), projectID, body, signature).Code)
	require.NoError(t, srv.Shutdown(context.Background()))

	srv2 := startFactoryNode(t, dataDir, false, time.Hour, 1)
	t.Cleanup(func() { _ = srv2.Shutdown(context.Background()) })
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	resp, err := srv2.Service().Secrets().GetSecret(ctx, &service.GetSecretRequest{SecretID: e2eWebhookRef})
	require.NoError(t, err, "a stored secret must survive a restart; not_found here means the client surface is serving an in-memory manager")
	require.NotNil(t, resp)
	require.Equal(t, e2eSecretValue, string(resp.Data))
	binding, err := srv2.SDLCService().Binding(ctx, loopE2ETenant, projectID)
	require.NoError(t, err, "the binding must survive a restart")
	require.Equal(t, e2eWebhookRef, binding.Forge.WebhookSecretRef)
	rec := deliver(t, webhookRouter(srv2), projectID, body, signature)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	t.Logf("OBSERVED: after restart the binding and the secret are served and a signed delivery is accepted (%d)", rec.Code)
}
