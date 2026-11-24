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

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestConfig_EffectiveBackends_Selection pins what each selection serves. The
// gpu-host case is the one that matters: a GPU node that names opencode,
// openvino and ollama serves exactly those, so placement can never route
// claude or codex work to it.
func TestConfig_EffectiveBackends_Selection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		backends []BackendKind
		openvino string
		vllm     string
		want     []BackendKind
	}{
		{
			name:     "individual backends, no hosted CLIs",
			backends: []BackendKind{BackendOpencode, BackendOpenVINO, BackendOllama},
			openvino: "http://127.0.0.1:9000",
			want:     []BackendKind{BackendOpencode, BackendOpenVINO, BackendOllama},
		},
		{
			name:     "the acp group skips endpoints with no server",
			backends: []BackendKind{BackendACP},
			want:     []BackendKind{BackendClaude, BackendCodex, BackendOpencode, BackendOllamaAgent},
		},
		{
			name:     "the acp group includes configured endpoints",
			backends: []BackendKind{BackendACP, BackendOllama},
			openvino: "http://npu:9000",
			vllm:     "http://gpu:11435",
			want: []BackendKind{BackendClaude, BackendCodex, BackendOpencode, BackendOpenVINO,
				BackendOllamaAgent, BackendVLLM, BackendOllama},
		},
		{
			name:     "repeats collapse, first position wins",
			backends: []BackendKind{BackendOpencode, BackendACP, BackendOpencode},
			want:     []BackendKind{BackendOpencode, BackendClaude, BackendCodex, BackendOllamaAgent},
		},
		{
			name: "empty is acp and ollama",
			want: []BackendKind{BackendClaude, BackendCodex, BackendOpencode, BackendOllamaAgent, BackendOllama},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Config{Backends: tc.backends, OpenVINOBaseURL: tc.openvino, VLLMBaseURL: tc.vllm}
			if got := c.EffectiveBackends(); !slices.Equal(got, tc.want) {
				t.Fatalf("EffectiveBackends() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfig_Serves(t *testing.T) {
	t.Parallel()
	c := &Config{Backends: []BackendKind{BackendOpencode, BackendOllama}}
	if !c.Serves(BackendOllama) || !c.Serves(BackendOpencode) {
		t.Fatal("a selected backend must be served")
	}
	if c.Serves(BackendClaude) || c.Serves(BackendCodex) {
		t.Fatal("a backend the node did not name must not be served")
	}
}

func TestConfig_Validate_Backends(t *testing.T) {
	t.Parallel()
	t.Run("every known name is accepted", func(t *testing.T) {
		t.Parallel()
		c := newTestConfig(t)
		c.OpenVINOBaseURL = "http://127.0.0.1:9000"
		c.VLLMBaseURL = "http://127.0.0.1:11435"
		c.Backends = []BackendKind{BackendACP, BackendClaude, BackendCodex, BackendOpencode,
			BackendOpenVINO, BackendOllamaAgent, BackendVLLM, BackendOllama}
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() = %v", err)
		}
	})
	t.Run("an unknown name is refused and the known ones listed", func(t *testing.T) {
		t.Parallel()
		c := newTestConfig(t)
		c.Backends = []BackendKind{BackendOpencode, "qwen"}
		err := c.Validate()
		if !errors.Is(err, ErrConfigUnknownBackend) || !strings.Contains(err.Error(), `"qwen"`) ||
			!strings.Contains(err.Error(), "opencode") {
			t.Fatalf("Validate() = %v, want ErrConfigUnknownBackend naming qwen and the known set", err)
		}
	})
	t.Run("an endpoint backend named without its server is refused", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []BackendKind{BackendOpenVINO, BackendVLLM} {
			c := newTestConfig(t)
			c.Backends = []BackendKind{kind}
			if err := c.Validate(); !errors.Is(err, ErrConfigBackendEndpointRequired) {
				t.Fatalf("Validate(%s) = %v, want ErrConfigBackendEndpointRequired", kind, err)
			}
		}
	})
}

func TestParseBackendKinds(t *testing.T) {
	t.Parallel()
	got := ParseBackendKinds(" opencode, openvino ,,ollama ")
	if want := []BackendKind{BackendOpencode, BackendOpenVINO, BackendOllama}; !slices.Equal(got, want) {
		t.Fatalf("ParseBackendKinds() = %v, want %v", got, want)
	}
	if got := ParseBackendKinds(""); len(got) != 0 {
		t.Fatalf("ParseBackendKinds(\"\") = %v, want none", got)
	}
}
