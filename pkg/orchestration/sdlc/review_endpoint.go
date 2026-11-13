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
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Handing a local model server to an opencode reviewer
// =============================================================================

// opencodeConfigEnv is the environment variable opencode reads an inline
// configuration from, merged over its own.
const opencodeConfigEnv = "OPENCODE_CONFIG_CONTENT"

// opencodeBuiltinAgents are the agents opencode ships — primary, subagent and
// the hidden ones it runs for titles, summaries and compaction. Each is pinned
// to the reviewer's model so none answers on a host default.
var opencodeBuiltinAgents = []string{"build", "plan", "general", "explore", "title", "summary", "compaction"}

// opencodeModelRef is the model reference an opencode reviewer is spawned
// with: the model on the ollama provider this module declares.
func opencodeModelRef(model string) string {
	return orchestration.ModelProviderOllama.Ref(model)
}

// opencodeProviderConfig renders the inline opencode configuration that
// points its "ollama" provider at the reviewer's endpoint and declares the
// model on it.
//
// OLLAMA SPEAKS THE OPENAI CHAT DIALECT UNDER /v1, which is what opencode's
// openai-compatible provider package drives.
//
// THE MODEL IS SELECTED HERE, NOT ONLY NAMED IN THE SPAWN. opencode's ACP
// server ignores the model a session/new request carries and opens the
// session on its configured default, so a reviewer spawned with
// "ollama/review-q8:latest" on a host whose own configuration defaults to
// another model would review on that other model under this one's name. The
// inline configuration is merged over the host's, so pinning "model" — and
// "small_model", which opencode sends the conversation to for its titles —
// makes the declared model the one that answers.
//
// AND NO OTHER MODEL CAN ANSWER AT ALL. An inherited agent — the built-in
// "general" subagent, or any custom one — may carry its own model, say
// anthropic/claude-…, and do delegated review work while the verdict is
// attributed to the review model. Pinning the agents we know of cannot reach
// the ones we do not, so three fences hold together:
//
//   - enabled_providers is ONLY the endpoint's provider, and
//     disabled_providers is cleared so a host cannot switch that one off in
//     favour of another: an inherited agent naming any other provider has no
//     model to run on.
//   - every built-in agent — primary, subagent and hidden — is pinned to the
//     declared model, so none falls back to a host default.
//   - the task tool is denied, so the session cannot delegate to a subagent.
func opencodeProviderConfig(endpoint, model string) (string, error) {
	provider := string(orchestration.ModelProviderOllama)
	ref := opencodeModelRef(model)
	name := strings.TrimPrefix(ref, provider+"/")
	agents := make(map[string]any, len(opencodeBuiltinAgents))
	for _, agent := range opencodeBuiltinAgents {
		agents[agent] = map[string]any{"model": ref}
	}
	config := map[string]any{
		"$schema":            "https://opencode.ai/config.json",
		"model":              ref,
		"small_model":        ref,
		"enabled_providers":  []string{provider},
		"disabled_providers": []string{},
		"agent":              agents,
		"permission":         map[string]any{"task": "deny"},
		"tools":              map[string]any{"task": false},
		"provider": map[string]any{
			provider: map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Ollama (review endpoint)",
				"options": map[string]any{
					"baseURL": strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/v1",
				},
				"models": map[string]any{
					name: map[string]any{"name": name},
				},
			},
		},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", &InvalidInputError{Field: "reviewer.endpoint", Message: "the opencode provider config does not render: " + err.Error()}
	}
	return string(raw), nil
}
