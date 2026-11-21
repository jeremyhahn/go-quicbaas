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

import "strings"

// =============================================================================
// Model families — the independence rule's unit
// =============================================================================
//
// A review is independent when the reviewer's model family differs from the
// family that wrote the code. The family is derived from the backend first
// and the model name second, so an unknown backend with a recognisable model
// still classifies.

const (
	// ModelFamilyAnthropic is every Claude model.
	ModelFamilyAnthropic = "anthropic"

	// ModelFamilyOpenAI is every GPT / Codex model.
	ModelFamilyOpenAI = "openai"

	// ModelFamilyQwen is every Qwen model, wherever it is served from.
	//
	// THE FAMILY IS THE MODEL'S LINEAGE, NOT ITS HOST. A Qwen served by a
	// local ollama is as independent of Anthropic as one served by Alibaba,
	// and the independence rule asks only "did a different family judge this
	// change". Naming the family after the host instead would make the same
	// model count as two families depending on where it ran.
	ModelFamilyQwen = "qwen"
)

// backendFamilies maps a backend name to its family.
//
// A RUNTIME THAT RUNS ANY MODEL HAS NO FAMILY OF ITS OWN. opencode is absent
// here on purpose: it drives Claude, GPT and the local Qwen builds alike, so
// the family of an opencode session is its model's.
//
// THE BACKEND NAME IS LOAD-BEARING FOR QWEN IN A WAY IT IS NOT FOR THE OTHERS.
// The locally served review models are tagged `review-bf16`, `review-q8` and
// `review-q4_K_M` — deliberately named for their JOB, and carrying no "qwen"
// anywhere in the string. No prefix rule can classify them, so the backend
// name is the only thing that can, and a reviewer configured with
// backend "qwen" is what makes the independence rule answer at all.
var backendFamilies = map[string]string{
	"claude":      ModelFamilyAnthropic,
	"claude-code": ModelFamilyAnthropic,
	"codex":       ModelFamilyOpenAI,
	"qwen":        ModelFamilyQwen,
}

// prefixFamilies maps a model-name prefix to its family, in match order.
var prefixFamilies = []struct {
	prefix string
	family string
}{
	{"anthropic/", ModelFamilyAnthropic},
	{"openai/", ModelFamilyOpenAI},
	{"qwen/", ModelFamilyQwen},
	{"claude-", ModelFamilyAnthropic},
	{"gpt-", ModelFamilyOpenAI},
	{"qwen", ModelFamilyQwen},
	// The locally served review models are tagged `review-bf16`,
	// `review-q8` and `review-q4_K_M` — named for their JOB, and carrying no
	// "qwen" anywhere in the string. They are the qwen builds the handbook's
	// model-usage convention names (review.Modelfile), so the tag prefix is
	// what classifies them; without it an opencode reviewer on
	// ollama/review-q8 would have no family, and its approval would never
	// count as independent.
	{"review-", ModelFamilyQwen},
}

// ModelFamily returns the family of a backend/model pair, or empty when
// neither identifies one.
func ModelFamily(backend, model string) string {
	if family, ok := backendFamilies[backend]; ok {
		return family
	}
	for _, candidate := range prefixFamilies {
		if strings.HasPrefix(model, candidate.prefix) {
			return candidate.family
		}
	}
	bare := bareModelName(model)
	for _, candidate := range prefixFamilies {
		if strings.HasPrefix(bare, candidate.prefix) {
			return candidate.family
		}
	}
	return ""
}

// bareModelName strips a provider prefix ("anthropic/claude-…" →
// "claude-…").
func bareModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}
