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

package orchestration

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type sandboxPolicyDoc struct {
	Policy SandboxPolicy `yaml:"sandbox_policy"`
}

// TestSandboxPolicy_YAMLRoundTrip pins the rendered form to the policy's name:
// a config file that read "sandbox_policy: 2" would change meaning the day the
// constants were reordered.
func TestSandboxPolicy_YAMLRoundTrip(t *testing.T) {
	for _, policy := range []SandboxPolicy{SandboxPolicyRequired, SandboxPolicyPreferred, SandboxPolicyOptIn} {
		out, err := yaml.Marshal(sandboxPolicyDoc{Policy: policy})
		if err != nil {
			t.Fatalf("marshal %v: %v", policy, err)
		}
		want := "sandbox_policy: " + policy.String() + "\n"
		if string(out) != want {
			t.Fatalf("marshal %v = %q, want %q", policy, out, want)
		}
		var back sandboxPolicyDoc
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", out, err)
		}
		if back.Policy != policy {
			t.Fatalf("round trip %v -> %v", policy, back.Policy)
		}
	}
}

// TestSandboxPolicy_YAMLRefusesUnknown proves an unknown name or an integer is
// refused instead of decoding to the zero value, which is opt-in — the least
// secure policy.
func TestSandboxPolicy_YAMLRefusesUnknown(t *testing.T) {
	for _, doc := range []string{"sandbox_policy: strict\n", "sandbox_policy: 2\n", "sandbox_policy: [required]\n"} {
		var got sandboxPolicyDoc
		err := yaml.Unmarshal([]byte(doc), &got)
		if err == nil {
			t.Fatalf("unmarshal %q: expected an error, got policy %v", doc, got.Policy)
		}
		var invalid *InputValidationError
		if !errors.As(err, &invalid) && !strings.Contains(err.Error(), "sandbox_policy") {
			t.Fatalf("unmarshal %q: error %v does not name the field", doc, err)
		}
	}
}
