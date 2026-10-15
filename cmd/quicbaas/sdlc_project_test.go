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

package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func (m *mockSDLCClient) OnboardRepo(_ context.Context, req *sdlcSDK.RepoOnboardRequest) (*sdlcSDK.RepoOnboardingReport, error) {
	m.repoOnboarded = req
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.RepoOnboardingReport{
		TenantID: req.TenantID, ProjectID: "p2", Repo: "acme/" + req.Repo, Steps: m.repoSteps,
		Conformance: m.report("p2", req.TenantID),
	}, nil
}

// withSDLCProjectOnboardFlags sets the onboard verb's flags for one test.
func withSDLCProjectOnboardFlags(t *testing.T, repo, like string, maxConcurrency int, paused, preflight bool) {
	t.Helper()
	origRepo, origLike, origMax := sdlcOnboardRepo, sdlcOnboardLike, sdlcOnboardMaxConcurrency
	origPaused, origPreflight := sdlcOnboardPaused, sdlcOnboardPreflight
	sdlcOnboardRepo, sdlcOnboardLike, sdlcOnboardMaxConcurrency = repo, like, maxConcurrency
	sdlcOnboardPaused, sdlcOnboardPreflight = paused, preflight
	t.Cleanup(func() {
		sdlcOnboardRepo, sdlcOnboardLike, sdlcOnboardMaxConcurrency = origRepo, origLike, origMax
		sdlcOnboardPaused, sdlcOnboardPreflight = origPaused, origPreflight
	})
}

func TestSDLCProjectOnboard_SendsTheFlagsAndSucceedsWithoutFindings(t *testing.T) {
	mc := &mockSDLCClient{conformant: true, repoSteps: []sdlcSDK.RepoOnboardStep{
		{Step: "project", Outcome: sdlcSDK.OnboardCreated},
		{Step: "binding", Outcome: sdlcSDK.OnboardUnchanged},
	}}
	withSDLCMock(t, mc)
	withSDLCProjectOnboardFlags(t, "web", "go-quicbaas", 2, true, true)

	require.NoError(t, runSDLCProjectOnboard(nil, nil))
	require.NotNil(t, mc.repoOnboarded)
	assert.Equal(t, sdlcSDK.RepoOnboardRequest{TenantID: "tenant-a", Repo: "web", Like: "go-quicbaas", MaxConcurrency: 2, Paused: true, Preflight: true},
		*mc.repoOnboarded)
}

// A finding is the report's answer, not a failure to answer: the report
// prints, and the exit code lets a script gate on it.
func TestSDLCProjectOnboard_ExitsNonZeroOnAFinding(t *testing.T) {
	mc := &mockSDLCClient{conformant: true, repoSteps: []sdlcSDK.RepoOnboardStep{
		{Step: "project", Outcome: sdlcSDK.OnboardCreated},
		{Step: "signing_key", Outcome: sdlcSDK.OnboardFinding, Detail: "no key", Remedy: "install agent-web-signing"},
	}}
	withSDLCMock(t, mc)
	withSDLCProjectOnboardFlags(t, "web", "go-quicbaas", 0, false, false)

	err := runSDLCProjectOnboard(nil, nil)
	var notOnboarded *SDLCRepoNotOnboardedError
	require.True(t, errors.As(err, &notOnboarded), "error = %v, want *SDLCRepoNotOnboardedError", err)
	assert.Equal(t, 1, notOnboarded.Findings)
	assert.Contains(t, notOnboarded.Error(), "web")
}

func TestSDLCProjectOnboard_RefusesAClientError(t *testing.T) {
	withSDLCMock(t, &mockSDLCClient{err: errors.New("boom")})
	withSDLCProjectOnboardFlags(t, "web", "go-quicbaas", 0, false, false)
	assert.Error(t, runSDLCProjectOnboard(nil, nil))
}

func TestSDLCProjectOnboard_RequiresRepoAndLike(t *testing.T) {
	withSDLCMock(t, &mockSDLCClient{})
	cmd := newSDLCProjectCommand()
	cmd.SetArgs([]string{"onboard", "--repo", "web"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	assert.Error(t, cmd.Execute(), "onboard ran without --like")
}

func TestFormatSDLCRepoOnboarding_Table(t *testing.T) {
	report := &sdlcSDK.RepoOnboardingReport{ProjectID: "p2", Repo: "acme/web", Steps: []sdlcSDK.RepoOnboardStep{
		{Step: "protection", Outcome: sdlcSDK.OnboardFinding, Detail: "quicbaas/review not required", Remedy: "require it"},
	}}
	var out bytes.Buffer
	require.NoError(t, formatSDLCRepoOnboarding(report, "table", &out))
	assert.Contains(t, out.String(), "acme/web")
	assert.Contains(t, out.String(), "require it")
	assert.Error(t, formatSDLCRepoOnboarding(report, "nope", &out))
}
