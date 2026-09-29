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
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCReviewer_ShowSetAndClearReachTheClient(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)

	require.NoError(t, runSDLCReviewer(nil, []string{"go-quicbaas", "1075"}))
	assert.Equal(t, "Reviewer", mc.reviewerCall)
	assert.Equal(t, 1075, mc.reviewerPR)
	assert.Equal(t, "tenant-a", mc.tenant)

	require.NoError(t, runSDLCReviewerSet(nil, []string{"go-quicbaas", "1075", "qwen"}))
	require.NotNil(t, mc.reviewerSet)
	assert.Equal(t, sdlcSDK.SetReviewerRequest{TenantID: "tenant-a", ProjectID: "go-quicbaas", PullRequest: 1075, Profile: "qwen"}, *mc.reviewerSet)

	require.NoError(t, runSDLCReviewerClear(nil, []string{"go-quicbaas", "1075"}))
	assert.Equal(t, "ClearReviewer", mc.reviewerCall)
}

func TestSDLCReviewer_RefusesABadPullRequestAndSurfacesClientFailures(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	for _, arg := range []string{"0", "-3", "abc", ""} {
		err := runSDLCReviewer(nil, []string{"p", arg})
		var bad *PullRequestNumberError
		require.Error(t, err, arg)
		assert.True(t, errors.As(err, &bad) || strings.Contains(err.Error(), "pull request"), "%q: %v", arg, err)
	}
	assert.Empty(t, mc.reviewerCall, "a refused argument reached the client")

	mc.err = errors.New("no such profile")
	assert.Error(t, runSDLCReviewerSet(nil, []string{"p", "1", "gone"}))
}

func TestFormatSDLCReviewerSelection_SaysWhoChoseAndWhetherItCounts(t *testing.T) {
	var table bytes.Buffer
	require.NoError(t, formatSDLCReviewerSelection(&sdlcSDK.ReviewerSelection{
		ProjectID: "p", PullRequest: 41, Profile: "qwen", Overridden: true, StatusContext: "quicbaas/review/qwen",
	}, "table", &table))
	for _, want := range []string{"#41", "qwen (operator override)", "advisory", "quicbaas/review/qwen"} {
		assert.Contains(t, table.String(), want)
	}

	table.Reset()
	require.NoError(t, formatSDLCReviewerSelection(&sdlcSDK.ReviewerSelection{Profile: "default", CountsTowardMerge: true}, "table", &table))
	assert.Contains(t, table.String(), "default (project default)")
	assert.Contains(t, table.String(), "yes")

	var js bytes.Buffer
	require.NoError(t, formatSDLCReviewerSelection(&sdlcSDK.ReviewerSelection{Profile: "qwen"}, "json", &js))
	assert.Contains(t, js.String(), `"profile": "qwen"`)
	assert.Error(t, formatSDLCReviewerSelection(&sdlcSDK.ReviewerSelection{}, "nope", &js))
}

func TestAlternatesLabel_NamesEveryProfileAndItsRole(t *testing.T) {
	assert.Equal(t, "-", alternatesLabel(nil))
	counted := true
	got := alternatesLabel([]sdlcSDK.ReviewerConfig{
		{Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer"},
		{Name: "gpt", Agent: "codex-reviewer", Login: "gpt", CountsTowardMerge: &counted},
	})
	for _, want := range []string{"qwen: agent:qwen-reviewer as qwen-reviewer [advisory]", "gpt: agent:codex-reviewer as gpt [counts toward merge]"} {
		assert.Contains(t, got, want)
	}
}

func TestSDLCCommand_HasTheReviewerVerbs(t *testing.T) {
	var verbs []string
	found := false
	for _, sub := range newSDLCCommand().Commands() {
		if strings.Fields(sub.Use)[0] != "reviewer" {
			continue
		}
		found = true
		for _, verb := range sub.Commands() {
			verbs = append(verbs, strings.Fields(verb.Use)[0])
		}
	}
	require.True(t, found, "sdlc reviewer is not registered")
	assert.ElementsMatch(t, []string{"set", "clear"}, verbs)
}
