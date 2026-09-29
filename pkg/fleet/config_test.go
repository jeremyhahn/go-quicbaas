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

package fleet

import "testing"

// TestEffectiveOllamaBaseURL_DefaultsLocalAndTakesAnOverride covers the field
// that stopped the local-model backends being loopback-only.
//
// THE HARDCODE WAS RIGHT FOR EXACTLY ONE NODE — the GPU host itself. Every
// other daemon in a fleet has to reach that host over the network and had no
// way to say so, so the model was unusable from anywhere else.
func TestEffectiveOllamaBaseURL_DefaultsLocalAndTakesAnOverride(t *testing.T) {
	var zero Config
	if got := zero.EffectiveOllamaBaseURL(); got != DefaultOllamaBaseURL {
		t.Fatalf("the zero config must stay on the local server, got %q", got)
	}
	if got := (&Config{OllamaBaseURL: "   "}).EffectiveOllamaBaseURL(); got != DefaultOllamaBaseURL {
		t.Fatalf("blank must fall back to the default, got %q", got)
	}

	cfg := &Config{OllamaBaseURL: "http://gpu.invalid:11434"}
	if got := cfg.EffectiveOllamaBaseURL(); got != "http://gpu.invalid:11434" {
		t.Fatalf("an explicit endpoint must be used verbatim, got %q", got)
	}
}

// TestEffectiveOllamaBaseURL_TrimsTrailingSlash keeps callers from building a
// double slash when they append a path.
func TestEffectiveOllamaBaseURL_TrimsTrailingSlash(t *testing.T) {
	for _, in := range []string{"http://gpu.invalid:11434/", "http://gpu.invalid:11434//", "  http://gpu.invalid:11434/  "} {
		if got := (&Config{OllamaBaseURL: in}).EffectiveOllamaBaseURL(); got != "http://gpu.invalid:11434" {
			t.Fatalf("EffectiveOllamaBaseURL(%q) = %q", in, got)
		}
	}
}
