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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

type mockSDLCClient struct {
	bound    *sdlcSDK.BindRequest
	unbound  string
	got      string
	delivery string
	tenant   string
	err      error

	onboarded  string
	conformed  string
	conformant bool
	findings   []sdlcSDK.ConformanceFinding

	// reviewerCall records the last reviewer-override verb and its request.
	reviewerCall string
	reviewerPR   int
	reviewerSet  *sdlcSDK.SetReviewerRequest
	// adopted is the last adoption request.
	adopted *sdlcSDK.AdoptIssueRequest
	// The worker image verbs: statuses answers successive status reads (the
	// last one repeats), refreshes counts requests.
	statuses  []*sdlcSDK.WorkerImageStatus
	refreshes int

	// filed is the last issue filing the CLI asked for.
	filed *sdlcSDK.FileIssueRequest
}

func (m *mockSDLCClient) Reviewer(_ context.Context, tenantID, projectID string, pullRequest int) (*sdlcSDK.ReviewerSelection, error) {
	m.reviewerCall, m.tenant, m.got, m.reviewerPR = "Reviewer", tenantID, projectID, pullRequest
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ReviewerSelection{ProjectID: projectID, PullRequest: pullRequest, Profile: "default", CountsTowardMerge: true, StatusContext: "quicbaas/review"}, nil
}

func (m *mockSDLCClient) SetReviewer(_ context.Context, req *sdlcSDK.SetReviewerRequest) (*sdlcSDK.ReviewerSelection, error) {
	m.reviewerCall, m.tenant, m.reviewerSet = "SetReviewer", req.TenantID, req
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ReviewerSelection{ProjectID: req.ProjectID, PullRequest: req.PullRequest, Profile: req.Profile, Overridden: true,
		StatusContext: "quicbaas/review/" + req.Profile, UpdatedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}, nil
}

func (m *mockSDLCClient) AdoptIssue(_ context.Context, req *sdlcSDK.AdoptIssueRequest) (*sdlcSDK.Adoption, error) {
	m.adopted, m.tenant = req, req.TenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.Adoption{TaskID: "task-1", Issue: req.Issue, PullRequest: req.PullRequest, Branch: "issue/141-coverage-gate"}, nil
}

func (m *mockSDLCClient) ClearReviewer(_ context.Context, tenantID, projectID string, pullRequest int) (*sdlcSDK.ReviewerSelection, error) {
	m.reviewerCall, m.tenant, m.got, m.reviewerPR = "ClearReviewer", tenantID, projectID, pullRequest
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ReviewerSelection{ProjectID: projectID, PullRequest: pullRequest, Profile: "default", CountsTowardMerge: true}, nil
}

func (m *mockSDLCClient) Bind(_ context.Context, req *sdlcSDK.BindRequest) (*sdlcSDK.ProjectBinding, error) {
	m.bound, m.tenant = req, req.TenantID
	if m.err != nil {
		return nil, m.err
	}
	b := req.Binding
	b.UpdatedAt = time.Now()
	return &b, nil
}

func (m *mockSDLCClient) Unbind(_ context.Context, tenantID, projectID string) error {
	m.unbound, m.tenant = projectID, tenantID
	return m.err
}

func (m *mockSDLCClient) Binding(_ context.Context, tenantID, projectID string) (*sdlcSDK.ProjectBinding, error) {
	m.got, m.tenant = projectID, tenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ProjectBinding{ProjectID: projectID, Forge: sdlcSDK.ForgeConfig{Type: "gitea", Owner: "acme", Repo: "api", TriggerLabel: "task:ready", WebhookSecretRef: "wh"}}, nil
}

func (m *mockSDLCClient) Delivery(_ context.Context, tenantID, taskID string) (*sdlcSDK.Delivery, error) {
	m.delivery, m.tenant = taskID, tenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.Delivery{TaskID: taskID, Attempt: 2, Branch: "issue/1-x", Base: "main", HeadSHA: "abc",
		PullRequest: sdlcSDK.PullRequest{Number: 7, URL: "http://forge/pr/7", State: "open"},
		Reviews:     []sdlcSDK.ReviewRecord{{Reviewer: "codex", Verdict: "approve", HeadSHA: "abc", Round: 1}},
		Checks:      []sdlcSDK.CheckRecord{{Name: "ci", Status: "success"}}}, nil
}

func (m *mockSDLCClient) Onboard(_ context.Context, tenantID, projectID string) (*sdlcSDK.OnboardingReport, error) {
	m.onboarded, m.tenant = projectID, tenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.OnboardingReport{
		ProjectID: projectID, TenantID: tenantID, Repo: "acme/api",
		WebhookURL: "https://factory.test/api/v1/sdlc/webhook/" + projectID, WebhookEnsured: true,
		LabelsCreated: []string{"task:ready"},
		Conformance:   m.report(projectID, tenantID),
	}, nil
}

func (m *mockSDLCClient) Conformance(_ context.Context, tenantID, projectID string) (*sdlcSDK.ConformanceReport, error) {
	m.conformed, m.tenant = projectID, tenantID
	if m.err != nil {
		return nil, m.err
	}
	return m.report(projectID, tenantID), nil
}

func (m *mockSDLCClient) OpenWork(_ context.Context, tenantID, projectID string) (*sdlcSDK.OpenWork, error) {
	m.got, m.tenant = projectID, tenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.OpenWork{
		ProjectID: projectID, Repo: "acme/api", Agent: "agent-api",
		PullRequests: []sdlcSDK.OpenPullRequest{{Number: 7, Title: "fix the thing", HeadRef: "issue/7-fix",
			CIState: "failure", ReviewState: "changes_requested", AgeSeconds: 26 * 3600}},
		Issues: []sdlcSDK.OpenIssue{{Number: 3, Title: "needs an answer", Labels: []string{"factory:needs-info"}, AgeSeconds: 90}},
	}, nil
}

func (m *mockSDLCClient) report(projectID, tenantID string) *sdlcSDK.ConformanceReport {
	return &sdlcSDK.ConformanceReport{
		ProjectID: projectID, TenantID: tenantID, Repo: "acme/api",
		Conformant: m.conformant,
		Checks: []sdlcSDK.ConformanceCheckResult{
			{Name: "binding", Status: "pass"},
			{Name: "webhook", Status: "pass"},
		},
		Findings: m.findings,
	}
}

// ListForges answers the build's registered forge adapters. The verb takes
// no tenant and no request: the adapter set is the build's, not a tenant's
// (go-quicbaas#606).
func (m *mockSDLCClient) ListForges(_ context.Context) (*sdlcSDK.ListForgesResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ListForgesResponse{Forges: []string{"gitea"}, Count: 1}, nil
}

func withSDLCMock(t *testing.T, mc *mockSDLCClient) {
	t.Helper()
	origFactory, origTimeout, origTenant, origOutput, origFile := sdlcClientFactory, globalTimeout, globalTenantID, sdlcOutputFmt, sdlcFile
	sdlcClientFactory = func() (sdlcSDK.Client, error) { return mc, nil }
	globalTimeout = time.Second
	globalTenantID = "tenant-a"
	sdlcOutputFmt = "json"
	sdlcFile = ""
	t.Cleanup(func() {
		sdlcClientFactory, globalTimeout, globalTenantID, sdlcOutputFmt, sdlcFile = origFactory, origTimeout, origTenant, origOutput, origFile
	})
}

func TestSDLCBind_ReadsTheBindingFromAFile(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	path := filepath.Join(t.TempDir(), "binding.yaml")
	require.NoError(t, os.WriteFile(path, []byte("forge:\n  type: gitea\n  owner: acme\n  repo: api\n  trigger_label: task:ready\nreview:\n  required_approvals: 1\n"), 0o600))
	sdlcFile = path
	require.NoError(t, runSDLCBind(nil, []string{"proj-1"}))
	require.NotNil(t, mc.bound)
	assert.Equal(t, "proj-1", mc.bound.ProjectID)
	assert.Equal(t, "tenant-a", mc.bound.TenantID)
	assert.Equal(t, "task:ready", mc.bound.Binding.Forge.TriggerLabel)
	assert.Equal(t, 1, mc.bound.Binding.Review.RequiredApprovals)
}

func TestSDLCBind_RefusesWithoutOrWithABadFile(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	err := runSDLCBind(nil, []string{"proj-1"})
	require.Error(t, err)
	var required *BindingFileRequiredError
	assert.True(t, errors.As(err, &required), "%v", err)
	assert.Nil(t, mc.bound)

	sdlcFile = filepath.Join(t.TempDir(), "missing.yaml")
	err = runSDLCBind(nil, []string{"proj-1"})
	var fileErr *BindingFileError
	assert.True(t, errors.As(err, &fileErr), "%v", err)

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("forge: [not a map"), 0o600))
	sdlcFile = bad
	err = runSDLCBind(nil, []string{"proj-1"})
	assert.True(t, errors.As(err, &fileErr), "%v", err)

	// A client refusal is surfaced, not swallowed.
	good := filepath.Join(t.TempDir(), "good.yaml")
	require.NoError(t, os.WriteFile(good, []byte("forge:\n  type: gitea\n"), 0o600))
	sdlcFile = good
	mc.err = errors.New("repository host refused")
	require.Error(t, runSDLCBind(nil, []string{"proj-1"}))
}

func TestSDLCUnbindBindingDelivery(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	require.NoError(t, runSDLCUnbind(nil, []string{"proj-1"}))
	assert.Equal(t, "proj-1", mc.unbound)
	require.NoError(t, runSDLCBinding(nil, []string{"proj-1"}))
	assert.Equal(t, "proj-1", mc.got)
	require.NoError(t, runSDLCDelivery(nil, []string{"task-9"}))
	assert.Equal(t, "task-9", mc.delivery)
	assert.Equal(t, "tenant-a", mc.tenant)

	mc.err = errors.New("not found")
	require.Error(t, runSDLCUnbind(nil, []string{"proj-1"}))
	require.Error(t, runSDLCBinding(nil, []string{"proj-1"}))
	require.Error(t, runSDLCDelivery(nil, []string{"task-9"}))

	sdlcClientFactory = func() (sdlcSDK.Client, error) { return nil, errors.New("no client") }
	require.Error(t, runSDLCBind(nil, []string{"proj-1"}))
	require.Error(t, runSDLCUnbind(nil, []string{"proj-1"}))
	require.Error(t, runSDLCBinding(nil, []string{"proj-1"}))
	require.Error(t, runSDLCDelivery(nil, []string{"task-9"}))
}

func TestFormatSDLC_EveryFormat(t *testing.T) {
	mc := &mockSDLCClient{}
	binding, err := mc.Binding(context.Background(), "t", "proj-1")
	require.NoError(t, err)
	delivery, err := mc.Delivery(context.Background(), "t", "task-9")
	require.NoError(t, err)
	for _, format := range []string{"json", "yaml", "table"} {
		var buf bytes.Buffer
		require.NoError(t, formatSDLCBinding(binding, format, &buf), format)
		assert.Contains(t, buf.String(), "proj-1", format)
		buf.Reset()
		require.NoError(t, formatSDLCDelivery(delivery, format, &buf), format)
		assert.Contains(t, buf.String(), "task-9", format)
		assert.Contains(t, buf.String(), "codex", format)
		buf.Reset()
		require.NoError(t, formatSDLCUnbound("proj-1", format, &buf), format)
		assert.Contains(t, buf.String(), "proj-1", format)
	}
	var table bytes.Buffer
	require.NoError(t, formatSDLCBinding(binding, "table", &table))
	assert.True(t, strings.Contains(table.String(), "Trigger label:") && strings.Contains(table.String(), "configured"))
	table.Reset()
	require.NoError(t, formatSDLCDelivery(delivery, "table", &table))
	assert.Contains(t, table.String(), "#7 http://forge/pr/7 (open)")
	assert.Error(t, formatSDLCBinding(binding, "nope", &table))
	assert.Error(t, formatSDLCDelivery(delivery, "nope", &table))
	assert.Error(t, formatSDLCUnbound("p", "nope", &table))
	assert.Equal(t, "-", boolWord(false))
}

func TestSDLCCommand_Shape(t *testing.T) {
	cmd := newSDLCCommand()
	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[strings.Fields(sub.Use)[0]] = true
	}
	for _, want := range []string{"bind", "unbind", "binding", "delivery"} {
		assert.True(t, names[want], want)
	}
	assert.False(t, names["item"], "the work-item surface is gone")
	assert.False(t, names["project"], "project registration is gone")
}

// =============================================================================
// Onboarding and conformance
// =============================================================================

// TestSDLCOnboard_ReportsWhatItProvisioned covers the write verb's output.
func TestSDLCOnboard_ReportsWhatItProvisioned(t *testing.T) {
	mc := &mockSDLCClient{conformant: true}
	withSDLCMock(t, mc)

	require.NoError(t, runSDLCOnboard(nil, []string{"go-truststrap"}))
	assert.Equal(t, "go-truststrap", mc.onboarded)
	assert.Equal(t, "tenant-a", mc.tenant)
}

// TestSDLCConformance_ExitsNonZeroWhenNotConformant.
//
// The exit code is what lets `quicbaas sdlc conformance <project>` gate a
// script. The report still prints either way: an exit code that says
// "something is wrong" without saying what is the thing this verb replaces.
func TestSDLCConformance_ExitsNonZeroWhenNotConformant(t *testing.T) {
	mc := &mockSDLCClient{conformant: false, findings: []sdlcSDK.ConformanceFinding{
		{Class: "webhook_missing", Check: "webhook", Subject: "https://factory.test/hook", Detail: "no delivery", Remedy: "onboard the project"},
	}}
	withSDLCMock(t, mc)

	err := runSDLCConformance(nil, []string{"go-truststrap"})
	require.Error(t, err, "a non-conformant project exited zero; nothing could gate on it")

	var notConformant *SDLCNotConformantError
	require.True(t, errors.As(err, &notConformant), "error = %v, want *SDLCNotConformantError", err)
	assert.Equal(t, "go-truststrap", notConformant.ProjectID)
	assert.Equal(t, 1, notConformant.Findings)
	assert.Equal(t, "go-truststrap", mc.conformed)
}

// TestSDLCConformance_ExitsZeroWhenConformant is the other half: a clean
// repository must not fail a pipeline.
func TestSDLCConformance_ExitsZeroWhenConformant(t *testing.T) {
	mc := &mockSDLCClient{conformant: true}
	withSDLCMock(t, mc)

	assert.NoError(t, runSDLCConformance(nil, []string{"go-truststrap"}))
}

// TestSDLCConformance_TableNamesEveryFindingAndItsRemedy.
//
// "Not conformant" is useless. The table has to carry the class, the subject
// and the fix, or the operator reading it at three in the morning still does
// not know what to do.
func TestSDLCConformance_TableNamesEveryFindingAndItsRemedy(t *testing.T) {
	report := &sdlcSDK.ConformanceReport{
		ProjectID: "go-truststrap", Repo: "jeremyhahn/go-truststrap", Conformant: false,
		Checks: []sdlcSDK.ConformanceCheckResult{
			{Name: "binding", Status: "pass"},
			{Name: "webhook", Status: "fail", Findings: 1},
			{Name: "protection", Status: "unsupported"},
		},
		Findings: []sdlcSDK.ConformanceFinding{
			{Class: "webhook_missing", Check: "webhook", Subject: "https://factory.test/hook",
				Detail: "the repository has no delivery", Remedy: "onboard the project to provision it"},
		},
	}

	var out strings.Builder
	require.NoError(t, formatSDLCConformance(report, "table", &out))

	rendered := out.String()
	for _, want := range []string{
		"go-truststrap", "jeremyhahn/go-truststrap", "NO",
		// Every check, including the ones that PASSED: a report listing only
		// failures cannot be told from one whose checks never ran.
		"binding", "pass", "webhook", "fail", "protection", "unsupported",
		"webhook_missing", "the repository has no delivery", "onboard the project to provision it",
	} {
		assert.Contains(t, rendered, want)
	}
}

// TestSDLCOnboarding_JSONAndYAMLRoundTrip keeps the machine-readable formats
// honest: a fleet loop parses these.
func TestSDLCOnboarding_JSONAndYAMLRoundTrip(t *testing.T) {
	report := &sdlcSDK.OnboardingReport{
		ProjectID: "p1", Repo: "acme/api", WebhookEnsured: true,
		WebhookURL: "https://factory.test/api/v1/sdlc/webhook/p1", LabelsCreated: []string{"task:ready"},
		Conformance: &sdlcSDK.ConformanceReport{ProjectID: "p1", Conformant: true},
	}

	var jsonOut strings.Builder
	require.NoError(t, formatSDLCOnboarding(report, "json", &jsonOut))
	var decoded sdlcSDK.OnboardingReport
	require.NoError(t, json.Unmarshal([]byte(jsonOut.String()), &decoded))
	assert.Equal(t, report.LabelsCreated, decoded.LabelsCreated)
	assert.True(t, decoded.WebhookEnsured)

	var yamlOut strings.Builder
	require.NoError(t, formatSDLCOnboarding(report, "yaml", &yamlOut))
	assert.Contains(t, yamlOut.String(), "webhook_ensured")
}

// TestSDLCOnboarding_PollOnlyProjectIsNotReportedAsAFailedProvisioning.
//
// A binding that names no webhook secret is poll-only BY DECLARATION, and
// rendering that the same as "provisioning did nothing" would send an
// operator looking for a fault that is a configuration.
func TestSDLCOnboarding_PollOnlyProjectIsNotReportedAsAFailedProvisioning(t *testing.T) {
	pollOnly := &sdlcSDK.OnboardingReport{ProjectID: "p1", WebhookEnsured: false, WebhookURL: ""}
	assert.Contains(t, webhookLabel(pollOnly), "poll-only")

	notProvisioned := &sdlcSDK.OnboardingReport{ProjectID: "p1", WebhookEnsured: false, WebhookURL: "https://x/hook"}
	assert.Equal(t, "not provisioned", webhookLabel(notProvisioned))

	provisioned := &sdlcSDK.OnboardingReport{ProjectID: "p1", WebhookEnsured: true, WebhookURL: "https://x/hook"}
	assert.Contains(t, webhookLabel(provisioned), "provisioned → https://x/hook")
}

// TestSDLCOnboarding_SurfacesAClientFailure is the error half of both verbs.
func TestSDLCOnboarding_SurfacesAClientFailure(t *testing.T) {
	mc := &mockSDLCClient{err: errors.New("forge unreachable")}
	withSDLCMock(t, mc)

	assert.Error(t, runSDLCOnboard(nil, []string{"p1"}))
	assert.Error(t, runSDLCConformance(nil, []string{"p1"}))
}

// TestSDLCOnboarding_RefusesAnUnknownOutputFormat.
func TestSDLCOnboarding_RefusesAnUnknownOutputFormat(t *testing.T) {
	var out strings.Builder
	assert.Error(t, formatSDLCOnboarding(&sdlcSDK.OnboardingReport{}, "toml", &out))
	assert.Error(t, formatSDLCConformance(&sdlcSDK.ConformanceReport{}, "toml", &out))
}

// TestReviewerLabel_NamesAnUnconfiguredReviewer is the operator-facing half of
// the reviewer fix.
//
// A BLANK OR DASHED ROW WOULD READ AS A COSMETIC GAP. A project with no
// reviewer is skipped by both dispatch paths without a log line, a task state
// or a forge comment, so this row is the only place an operator can see why a
// finished, green pull request is sitting there. It has to say so in words.
func TestReviewerLabel_NamesAnUnconfiguredReviewer(t *testing.T) {
	got := reviewerLabel(sdlcSDK.ReviewerConfig{})
	if !strings.Contains(got, "none") || !strings.Contains(got, "not self-reviewed") {
		t.Fatalf("an unconfigured reviewer must be named in words, got %q", got)
	}
	if got := reviewerLabel(sdlcSDK.ReviewerConfig{Agent: "   "}); !strings.Contains(got, "none") {
		t.Fatalf("a blank agent is unconfigured, got %q", got)
	}
}

// TestReviewerLabel_RendersAConfiguredReviewer covers the populated case,
// including the partial shapes the row has to survive.
func TestReviewerLabel_RendersAConfiguredReviewer(t *testing.T) {
	if got := reviewerLabel(sdlcSDK.ReviewerConfig{Agent: "codex-reviewer", Login: "codex"}); got != "agent:codex-reviewer as codex" {
		t.Fatalf("agent and login, got %q", got)
	}
	if got := reviewerLabel(sdlcSDK.ReviewerConfig{Agent: "codex-reviewer"}); got != "agent:codex-reviewer" {
		t.Fatalf("an agent alone renders as its reference, got %q", got)
	}
}

// TestDiscoveryLabel_NamesTheDefault keeps the empty discovery mode from
// reading as "nothing runs" when in fact both paths do.
func TestDiscoveryLabel_NamesTheDefault(t *testing.T) {
	if got := discoveryLabel(""); !strings.Contains(got, "both") || !strings.Contains(got, "default") {
		t.Fatalf("empty discovery must name the default, got %q", got)
	}
	if got := discoveryLabel("  "); !strings.Contains(got, "both") {
		t.Fatalf("blank discovery must name the default, got %q", got)
	}
	if got := discoveryLabel("poll"); got != "poll" {
		t.Fatalf("an explicit mode is shown verbatim, got %q", got)
	}
}

// TestMergeTrainLabel_SaysWhyATrainIsOff keeps a train that is on in the
// binding but cannot run from reading as running: a train's pull request lands
// only on a verdict from a reviewer profile that counts toward merge, so a
// project with none cannot run one.
func TestMergeTrainLabel_SaysWhyATrainIsOff(t *testing.T) {
	no, yes := false, true
	reviewer := sdlcSDK.ReviewerConfig{Agent: "codex-reviewer", Login: "codex"}
	advisory := sdlcSDK.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", CountsTowardMerge: &no}
	counting := sdlcSDK.ReviewerConfig{Name: "qwen", Agent: "vllm-reviewer", Login: "qwen", CountsTowardMerge: &yes}
	const off = "off (no reviewer profile that counts toward merge to review a train)"
	cases := []struct {
		name       string
		policy     sdlcSDK.MergeTrainPolicy
		reviewer   sdlcSDK.ReviewerConfig
		alternates []sdlcSDK.ReviewerConfig
		want       string
	}{
		{"default size", sdlcSDK.MergeTrainPolicy{}, reviewer, nil, "on, default size"},
		{"bounded", sdlcSDK.MergeTrainPolicy{MaxSize: 3}, reviewer, nil, "on, up to 3 change(s)"},
		{"disabled", sdlcSDK.MergeTrainPolicy{Disabled: true}, reviewer, nil, "off"},
		{"no reviewer", sdlcSDK.MergeTrainPolicy{}, sdlcSDK.ReviewerConfig{}, nil, off},
		{"advisory only", sdlcSDK.MergeTrainPolicy{}, advisory, []sdlcSDK.ReviewerConfig{{Name: "other", Agent: "vllm-reviewer"}}, off},
		{"counting alternate", sdlcSDK.MergeTrainPolicy{}, advisory, []sdlcSDK.ReviewerConfig{counting}, "on, default size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeTrainLabel(tc.policy, tc.reviewer, tc.alternates); got != tc.want {
				t.Fatalf("mergeTrainLabel(%+v) = %q, want %q", tc.policy, got, tc.want)
			}
		})
	}
}

func TestClusteringLabel_NamesTheDefaultAndTheBound(t *testing.T) {
	for policy, want := range map[sdlcSDK.ClusteringPolicy]string{
		{}:               "on, default size",
		{MaxSize: 2}:     "on, up to 2 issue(s)",
		{Disabled: true}: "off",
	} {
		if got := clusteringLabel(policy); got != want {
			t.Fatalf("clusteringLabel(%+v) = %q, want %q", policy, got, want)
		}
	}
}

// TestSDLCOpenWork_ReadsTheProjectInTheCallersTenant pins the verb's subject
// and the table an operator reads.
func TestSDLCOpenWork_ReadsTheProjectInTheCallersTenant(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)

	require.NoError(t, runSDLCOpenWork(nil, []string{"go-luks2"}))
	assert.Equal(t, "go-luks2", mc.got)
	assert.Equal(t, "tenant-a", mc.tenant)

	work, err := mc.OpenWork(context.Background(), "tenant-a", "go-luks2")
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, formatSDLCOpenWork(work, "table", &out))
	table := out.String()
	for _, want := range []string{"Agent: agent-api", "Pull requests (1)", "issue/7-fix", "failure", "changes_requested", "1d2h",
		"Issues (1)", "factory:needs-info", "1m"} {
		assert.Contains(t, table, want)
	}
}

// TestSDLCOpenWork_ReportsFailuresAndBadFormats is the error half.
func TestSDLCOpenWork_ReportsFailuresAndBadFormats(t *testing.T) {
	withSDLCMock(t, &mockSDLCClient{err: errors.New("forge down")})
	assert.Error(t, runSDLCOpenWork(nil, []string{"go-luks2"}))
	assert.Error(t, formatSDLCOpenWork(&sdlcSDK.OpenWork{}, "xml", &bytes.Buffer{}))
}

func TestAgeLabel(t *testing.T) {
	for seconds, want := range map[int64]string{0: "0m", 59: "0m", 90: "1m", 3*3600 + 120: "3h2m", 49 * 3600: "2d1h"} {
		assert.Equal(t, want, ageLabel(seconds), "%d seconds", seconds)
	}
}
