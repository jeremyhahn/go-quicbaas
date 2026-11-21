package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy"
)

// Spec is a sandbox policy document's spec: the tenant's sandbox policy,
// applied over the daemon's as orchestration.ResolveSandboxMode's tenant
// override (it can only tighten).
type Spec struct {
	Policy string `json:"policy"`
}

// Request is the sandbox kind's input: the mode a spawn asks for, the
// daemon's own policy, and whether a container runtime is usable.
type Request struct {
	Requested           orchestration.SandboxMode
	DaemonPolicy        orchestration.SandboxPolicy
	HasContainerRuntime bool
}

// Evaluator decides sandbox requests for the policy engine.
type Evaluator struct{}

var _ policy.Evaluator = Evaluator{}

// Kind is policy.KindSandbox.
func (Evaluator) Kind() policy.Kind { return policy.KindSandbox }

func parseSpec(raw json.RawMessage) (orchestration.SandboxPolicy, error) {
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	// A signed document names its policy: an empty one would silently mean
	// opt-in.
	if s.Policy == "" {
		return 0, errors.New("policy required (required, preferred or opt-in)")
	}
	return orchestration.ParseSandboxPolicy(s.Policy)
}

// Validate accepts a spec naming a known sandbox policy.
func (Evaluator) Validate(raw json.RawMessage) error {
	_, err := parseSpec(raw)
	return err
}

// Evaluate resolves the requested mode under the daemon's policy tightened by
// the document's. A mode the policies forbid is a deny with the violation's
// reason; an allow's reason names the effective mode.
func (Evaluator) Evaluate(_ context.Context, raw json.RawMessage, input any) (policy.Decision, error) {
	tenant, err := parseSpec(raw)
	if err != nil {
		return policy.Decision{}, err
	}
	req, ok := input.(Request)
	if !ok {
		return policy.Decision{}, fmt.Errorf("sandbox: input is %T, want sandbox.Request", input)
	}
	mode, err := orchestration.ResolveSandboxMode(req.Requested, req.DaemonPolicy, &tenant, req.HasContainerRuntime)
	var v *orchestration.SandboxModeViolationError
	switch {
	case errors.As(err, &v):
		return policy.Decision{Reason: v.Error()}, nil
	case err != nil:
		return policy.Decision{}, err
	}
	return policy.Decision{Allow: true, Reason: "mode " + mode.String()}, nil
}
