package sandbox

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy"
)

// hmacKey stands in for go-xkms: one key for every tenant.
type hmacKey []byte

func (k hmacKey) Sign(_ context.Context, _ string, payload []byte) (string, []byte, error) {
	m := hmac.New(sha256.New, k)
	m.Write(payload)
	return "test", m.Sum(nil), nil
}

func (k hmacKey) Verify(ctx context.Context, tenant, _ string, payload, sig []byte) error {
	_, want, _ := k.Sign(ctx, tenant, payload)
	if !hmac.Equal(want, sig) {
		return errors.New("mismatch")
	}
	return nil
}

func load(t *testing.T, spec string) (*policy.Engine, *policy.Loaded, error) {
	t.Helper()
	ctx := context.Background()
	key := hmacKey("sandbox-policy-test-key")
	e, err := policy.NewEngine(key, Evaluator{})
	if err != nil {
		t.Fatal(err)
	}
	sd, err := policy.Sign(ctx, key, policy.Document{
		Kind: policy.KindSandbox, Name: "sandbox", Tenant: "acme", Version: 1, Spec: json.RawMessage(spec),
	})
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Load(ctx, "acme", sd)
	return e, l, err
}

func TestSandboxKindDecisions(t *testing.T) {
	cases := []struct {
		name   string
		spec   string
		req    Request
		allow  bool
		reason string
	}{
		{"opt-in default runs raw", `{"policy":"opt-in"}`,
			Request{orchestration.SandboxModeDefault, orchestration.SandboxPolicyOptIn, true}, true, "mode " + orchestration.SandboxModeNone.String()},
		{"tenant required tightens default", `{"policy":"required"}`,
			Request{orchestration.SandboxModeDefault, orchestration.SandboxPolicyOptIn, true}, true, "mode " + orchestration.SandboxModeRequired.String()},
		{"tenant required denies raw exec", `{"policy":"required"}`,
			Request{orchestration.SandboxModeNone, orchestration.SandboxPolicyOptIn, true}, false, "raw exec forbidden"},
		{"tenant required, no runtime", `{"policy":"required"}`,
			Request{orchestration.SandboxModeDefault, orchestration.SandboxPolicyOptIn, false}, false, "runtime is unavailable"},
		{"tenant opt-in cannot loosen daemon required", `{"policy":"opt-in"}`,
			Request{orchestration.SandboxModeNone, orchestration.SandboxPolicyRequired, true}, false, "raw exec forbidden"},
		{"preferred falls back without runtime", `{"policy":"preferred"}`,
			Request{orchestration.SandboxModeDefault, orchestration.SandboxPolicyOptIn, false}, true, "mode " + orchestration.SandboxModeNone.String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, l, err := load(t, tc.spec)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			d, err := e.Evaluate(context.Background(), l, tc.req)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if d.Allow != tc.allow || !strings.Contains(d.Reason, tc.reason) || d.Kind != policy.KindSandbox {
				t.Errorf("decision = %+v, want allow=%v reason~%q", d, tc.allow, tc.reason)
			}
		})
	}
}

func TestSandboxKindRefusesBadSpec(t *testing.T) {
	for _, spec := range []string{`{}`, `{"policy":""}`, `{"policy":"lax"}`, `[]`} {
		if _, _, err := load(t, spec); !errors.Is(err, policy.ErrInvalidSpec) {
			t.Errorf("spec %s: Load err = %v, want ErrInvalidSpec", spec, err)
		}
	}
}

func TestSandboxKindRefusesWrongInput(t *testing.T) {
	e, l, err := load(t, `{"policy":"required"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Evaluate(context.Background(), l, "required"); err == nil {
		t.Error("Evaluate accepted a non-Request input")
	}
}
