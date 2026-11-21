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
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// An opencode reviewer on a local model (go-quicbaas#1068, carried over)
// =============================================================================

const (
	reviewQ8      = "ollama/review-q8:latest"
	reviewQ8Host  = "http://gpu-host.internal:11434"
	gpuHostAffine = "gpu-host"
)

// localQwenReviewer is the qwen seat on gpu-host: its Agent runs opencode on
// the review model gpu-host's ollama serves, and the seat pins it to that host
// and hands it that server.
func localQwenReviewer() ReviewerConfig {
	return ReviewerConfig{
		Name: qwenProfile, Agent: testOpencodeQwenAgent, Login: qwenLogin,
		TokenSecretRef: "sdlc/reviewer/api/qwen-token",
		Endpoint:       reviewQ8Host, HostAffinity: gpuHostAffine,
	}
}

// localQwenAgent is the Agent localQwenReviewer names.
func localQwenAgent(t *testing.T) orchestration.Agent {
	t.Helper()
	agent, ok := testAgents().Agent(testOpencodeQwenAgent)
	if !ok {
		t.Fatal("the fixture catalog does not define the opencode qwen reviewer")
	}
	return agent
}

// TestReviewerConfig_ValidateEndpoint: on its own, a seat's endpoint is only
// checked for being an http(s) server root; whether it may carry one is its
// Agent's to say, and is checked against the catalog.
func TestReviewerConfig_ValidateEndpoint(t *testing.T) {
	if err := localQwenReviewer().Validate(); err != nil {
		t.Fatalf("a seat with an http(s) endpoint is valid: %v", err)
	}
	for name, endpoint := range map[string]string{
		"endpoint that is not a URL": "gpu-host:11434",
		"endpoint on another scheme": "ftp://gpu-host:11434",
	} {
		t.Run(name, func(t *testing.T) {
			reviewer := localQwenReviewer()
			reviewer.Endpoint = endpoint
			var invalid *InvalidInputError
			if err := reviewer.Validate(); !errors.As(err, &invalid) || invalid.Field != "reviewer.endpoint" {
				t.Fatalf("Validate() = %v, want a refusal of reviewer.endpoint", err)
			}
		})
	}
}

// TestProjectBinding_CheckAgentsDecidesTheEndpoint: the endpoint is required
// exactly when the seat's Agent runs opencode on a local model.
func TestProjectBinding_CheckAgentsDecidesTheEndpoint(t *testing.T) {
	bindWith := func(seat ReviewerConfig) *ProjectBinding {
		binding := profiledBinding()
		binding.Reviewers = []ReviewerConfig{seat}
		return binding
	}
	if err := bindWith(localQwenReviewer()).CheckAgents(testAgents()); err != nil {
		t.Fatalf("an opencode seat on a local model with an endpoint was refused: %v", err)
	}
	hosted := localQwenReviewer()
	hosted.Agent, hosted.Endpoint = testOpencodeHostedAgent, ""
	if err := bindWith(hosted).CheckAgents(testAgents()); err != nil {
		t.Fatalf("an opencode seat on a hosted model needs no endpoint: %v", err)
	}

	refusals := map[string]func(*ReviewerConfig){
		"local model with no endpoint":    func(r *ReviewerConfig) { r.Endpoint = "" },
		"endpoint on a hosted model":      func(r *ReviewerConfig) { r.Agent = testOpencodeHostedAgent },
		"endpoint on a codex reviewer":    func(r *ReviewerConfig) { r.Agent = testReviewerAgent },
		"endpoint on a non-opencode qwen": func(r *ReviewerConfig) { r.Agent = testQwenAgent },
	}
	for name, mutate := range refusals {
		t.Run(name, func(t *testing.T) {
			seat := localQwenReviewer()
			mutate(&seat)
			var invalid *InvalidInputError
			if err := bindWith(seat).CheckAgents(testAgents()); !errors.As(err, &invalid) || invalid.Field != "reviewers[0].endpoint" {
				t.Fatalf("CheckAgents() = %v, want a refusal of reviewers[0].endpoint", err)
			}
		})
	}
}

// TestOpencodeProviderConfig_PinsTheModelAndFencesEverythingElse reads the
// rendered configuration the way opencode would: only the endpoint's
// provider is enabled, every model slot and every built-in agent is the
// review model, and delegation is denied.
func TestOpencodeProviderConfig_PinsTheModelAndFencesEverythingElse(t *testing.T) {
	raw, err := opencodeProviderConfig(reviewQ8Host+"/", "review-q8:latest")
	if err != nil {
		t.Fatalf("opencodeProviderConfig: %v", err)
	}
	var config struct {
		Model             string                    `json:"model"`
		SmallModel        string                    `json:"small_model"`
		EnabledProviders  []string                  `json:"enabled_providers"`
		DisabledProviders []string                  `json:"disabled_providers"`
		Agent             map[string]map[string]any `json:"agent"`
		Permission        map[string]string         `json:"permission"`
		Tools             map[string]bool           `json:"tools"`
		Provider          map[string]struct {
			Options map[string]string         `json:"options"`
			Models  map[string]map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("the config is not JSON: %v", err)
	}
	if config.Model != reviewQ8 || config.SmallModel != reviewQ8 {
		t.Fatalf("model=%q small_model=%q, want both %q", config.Model, config.SmallModel, reviewQ8)
	}
	if !slices.Equal(config.EnabledProviders, []string{"ollama"}) || config.DisabledProviders == nil || len(config.DisabledProviders) != 0 {
		t.Fatalf("providers enabled=%v disabled=%v, want only ollama and none disabled", config.EnabledProviders, config.DisabledProviders)
	}
	for _, agent := range opencodeBuiltinAgents {
		if config.Agent[agent]["model"] != reviewQ8 {
			t.Fatalf("agent %s runs %v, want %q", agent, config.Agent[agent]["model"], reviewQ8)
		}
	}
	if config.Permission["task"] != "deny" || config.Tools["task"] {
		t.Fatalf("delegation is not denied: permission=%v tools=%v", config.Permission, config.Tools)
	}
	ollama := config.Provider["ollama"]
	if ollama.Options["baseURL"] != reviewQ8Host+"/v1" {
		t.Fatalf("baseURL = %q, want the endpoint's /v1", ollama.Options["baseURL"])
	}
	if _, declared := ollama.Models["review-q8:latest"]; !declared || len(ollama.Models) != 1 {
		t.Fatalf("declared models = %v, want only review-q8:latest", ollama.Models)
	}
}

func TestPlaceReviewer(t *testing.T) {
	t.Run("a local model is pinned to its endpoint and host", func(t *testing.T) {
		// The request as the spawn resolves it: the Agent supplied the
		// backend and the model.
		req := &orchestration.SpawnSessionRequest{Backend: "opencode", Model: reviewQ8, Env: map[string]string{"KEEP": "1"}}
		if err := placeReviewer(req, localQwenReviewer(), localQwenAgent(t)); err != nil {
			t.Fatalf("placeReviewer: %v", err)
		}
		if req.HostAffinity != gpuHostAffine || !req.HostAffinityStrict {
			t.Fatalf("affinity = %q strict=%v, want a strict pin to %s", req.HostAffinity, req.HostAffinityStrict, gpuHostAffine)
		}
		if req.Model != reviewQ8 || !strings.Contains(req.Env[opencodeConfigEnv], reviewQ8Host) || req.Env["KEEP"] != "1" {
			t.Fatalf("spawn = %+v, want the pinned config beside the caller's env", req)
		}
		// The same request needs no pooled account once the engine applies
		// its environment.
		orchestration.ApplyWorkerSpawnEnvironment(req, "")
		if req.AccountAuto {
			t.Fatal("a local-model reviewer was sent to the account pool")
		}
	})
	t.Run("codex is placed by the fleet and handed nothing", func(t *testing.T) {
		codex, _ := testAgents().Agent(testReviewerAgent)
		req := &orchestration.SpawnSessionRequest{AgentID: testReviewerAgent}
		if err := placeReviewer(req, ReviewerConfig{Agent: testReviewerAgent, Login: "codex"}, codex); err != nil {
			t.Fatalf("placeReviewer: %v", err)
		}
		if req.HostAffinity != "" || req.HostAffinityStrict || req.Env != nil {
			t.Fatalf("a codex reviewer with no pin was narrowed: %+v", req)
		}
	})
	t.Run("a seat whose Agent is not defined is handed nothing", func(t *testing.T) {
		req := &orchestration.SpawnSessionRequest{AgentID: "gone"}
		if err := placeReviewer(req, localQwenReviewer(), orchestration.Agent{}); err != nil {
			t.Fatalf("placeReviewer: %v", err)
		}
		if req.Env != nil {
			t.Fatalf("an undefined Agent was handed a provider config: %+v", req.Env)
		}
	})
}

// TestReviewWorkspaceKey_OneDirectoryPerReviewer: two reviewers of one head
// never share a checkout, and one reviewer's re-review of it reuses its own.
func TestReviewWorkspaceKey_OneDirectoryPerReviewer(t *testing.T) {
	codex := reviewWorkspaceKey(7, "0123456789abcdef", DefaultReviewerProfileName, "codex")
	qwen := reviewWorkspaceKey(7, "0123456789abcdef", qwenProfile, qwenLogin)
	if codex == qwen {
		t.Fatalf("two reviewers of one head share workspace %q", codex)
	}
	if again := reviewWorkspaceKey(7, "0123456789abcdef", qwenProfile, " QWEN-Reviewer "); again != qwen {
		t.Fatalf("one reviewer's key moved with its login's spelling: %q vs %q", again, qwen)
	}
	if other := reviewWorkspaceKey(7, "0123456789abcdef", qwenProfile, "someone-else"); other == qwen {
		t.Fatal("a profile re-added under another login reused the old identity's checkout")
	}
	if !strings.HasPrefix(qwen, "review-7-") {
		t.Fatalf("key = %q", qwen)
	}
}

// TestReviewRunner_AnOpencodeReviewerOnALocalModelIsPinned drives the runner:
// the qwen seat is spawned on opencode, strictly on its host, with its model
// pinned through the inline config, and publishes under its own context.
func TestReviewRunner_AnOpencodeReviewerOnALocalModelIsPinned(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewer.Login = "codex-reviewer"
		qwen := localQwenReviewer()
		qwen.Login = fakeReviewerLogin
		b.Reviewers = []ReviewerConfig{qwen}
		delete(b.Review.Reviewers, fakeReviewerLogin)
	})
	rig.reviewerSays(reviewSaying("I read it; it is fine.", ReviewCodeApprove))
	if err := rig.runner.Run(context.Background(), rig.qwenJob()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spawn := rig.agents.spawns[0]
	if spawn.AgentID != testOpencodeQwenAgent || spawn.Backend != "" || spawn.Model != "" ||
		!spawn.HostAffinityStrict || spawn.HostAffinity != gpuHostAffine {
		t.Fatalf("spawn = agent %q backend %q model %q affinity %q strict %v",
			spawn.AgentID, spawn.Backend, spawn.Model, spawn.HostAffinity, spawn.HostAffinityStrict)
	}
	if !strings.Contains(spawn.Env[opencodeConfigEnv], `"enabled_providers":["ollama"]`) {
		t.Fatalf("the reviewer was not handed its pinned provider: %q", spawn.Env[opencodeConfigEnv])
	}
	statuses, _ := rig.forge.publishedStatuses()
	for _, status := range statuses {
		if status.Context != DefaultReviewStatusContext+"/"+qwenProfile {
			t.Fatalf("the advisory seat published under %q, not its own context", status.Context)
		}
	}
}

// =============================================================================
// Model family attribution
// =============================================================================

func TestModelFamily_TheLocalReviewModelsAreQwen(t *testing.T) {
	for _, model := range []string{reviewQ8, "review-bf16", "ollama/review-q4_K_M"} {
		if got := ModelFamily("opencode", model); got != ModelFamilyQwen {
			t.Errorf("ModelFamily(opencode, %q) = %q, want qwen", model, got)
		}
	}
	if got := ModelFamily("opencode", "anthropic/claude-opus-5-5"); got != ModelFamilyAnthropic {
		t.Fatalf("an opencode reviewer on Claude is %q, want anthropic", got)
	}
	if got := ModelFamily("opencode", "ollama/llama3"); got != "" {
		t.Fatalf("an unknown local model is %q, want no family", got)
	}
}

// TestReviewPolicy_LoginLookupIgnoresCase: a binding naming CODEX and a forge
// reporting codex are one account, or the approval never gets a family.
func TestReviewPolicy_LoginLookupIgnoresCase(t *testing.T) {
	policy := ReviewPolicy{Reviewers: map[string]string{"CODEX": ModelFamilyOpenAI}}
	if got := policy.ReviewerModelFamily("codex"); got != ModelFamilyOpenAI {
		t.Fatalf("ReviewerModelFamily(codex) = %q, want openai", got)
	}
	if got := policy.ReviewerModelFamily("someone-else"); got != "" {
		t.Fatalf("an unknown login has family %q", got)
	}

	binding := profiledBinding()
	binding.Reviewer.Login = "CODEX"
	if got := binding.reviewPolicy(bindingAgentsFrom(testAgents(), binding)).ReviewerModelFamily("codex"); got != ModelFamilyOpenAI {
		t.Fatalf("a profile's login in another case lost its family: %q", got)
	}
}

// TestProjectBinding_AFamilyOverrideCannotLaunderTheAuthorsFamily: mapping a
// configured codex reviewer's login to another family would let codex judge
// codex work as "independent". The binding is refused against the catalog, and
// a stored binding that carries the entry still attributes the derived family.
func TestProjectBinding_AFamilyOverrideCannotLaunderTheAuthorsFamily(t *testing.T) {
	binding := profiledBinding()
	binding.Review.Reviewers[strings.ToUpper(fakeReviewerLogin)] = ModelFamilyQwen
	var invalid *InvalidInputError
	if err := binding.CheckAgents(testAgents()); !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Field, "review.reviewers[") {
		t.Fatalf("CheckAgents() = %v, want the contradicting family entry refused", err)
	}
	if got := binding.reviewPolicy(bindingAgentsFrom(testAgents(), binding)).ReviewerModelFamily(fakeReviewerLogin); got != ModelFamilyOpenAI {
		t.Fatalf("the stored override won: family = %q, want openai", got)
	}

	// Restating the derived family, or naming a login no profile uses, is fine.
	agreeing := profiledBinding()
	agreeing.Review.Reviewers[fakeReviewerLogin] = "OpenAI"
	agreeing.Review.Reviewers["a-human"] = ModelFamilyAnthropic
	if err := agreeing.CheckAgents(testAgents()); err != nil {
		t.Fatalf("an agreeing entry and an unrelated login were refused: %v", err)
	}
}
