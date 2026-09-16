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
	"context"
	"strings"
)

// =============================================================================
// Agent profiles — runtime, model and provider are three things, not one name
// =============================================================================
//
// THE MISTAKE THIS REPLACES. A backend was registered under the name "qwen",
// which is a MODEL. What it actually was: the codex runtime, driving a
// qwen3-8b model, served by an ollama endpoint — three independent facts fused
// into one registry key, with the endpoint baked into the backend's argv. The
// consequence is immediate: one codex registration can reach exactly one
// provider, so a second provider needs a second backend name, and the registry
// fills with model names pretending to be runtimes.
//
// The three axes are independent and belong apart:
//
//	runtime   claude | codex | opencode | openvino   — runs the agent loop
//	model     qwen3-8b | gpt-5.5 | claude-opus-5     — what it thinks with
//	provider  OVMS@gpu-host | ollama@gpu-host | ""   — where that model is served
//
// A profile names one combination. "codex+gpt-5.5+OpenAI",
// "codex+qwen3-8b+ollama@gpu-host" and "openvino+qwen3-8b+OVMS@gpu-host" are
// three profiles over TWO runtimes — which is the shape the registry should
// have had.
//
// WHY THE PROVIDER TRAVELS PER SPAWN RATHER THAN PER REGISTRATION. It reaches
// the CLI through SpawnRequest.ExtraArgs, which already existed and already
// reaches the command. Nothing about the backend changes; what changes is that
// the endpoint stops being a property of the registration.

// ProviderEndpoint locates where a profile's model is served.
//
// THE ZERO VALUE MEANS "THE RUNTIME'S OWN DEFAULT", which is what a hosted CLI
// wants: codex against OpenAI and claude against Anthropic name no endpoint,
// and a profile for them leaves this empty rather than restating a vendor URL
// the CLI already knows.
type ProviderEndpoint struct {
	// Name is how the runtime refers to this provider in its own config. It
	// is an identifier, not a hostname.
	Name string `json:"name,omitempty"`

	// BaseURL is the server root INCLUDING the version segment the runtime
	// expects (e.g. "http://gpu-host:9000/v1"), because which segment is
	// correct differs by runtime and by server and is not this type's to
	// guess.
	BaseURL string `json:"base_url,omitempty"`

	// WireAPI names the dialect the endpoint speaks: "responses" or "chat".
	//
	// IT IS NOT COSMETIC AND IT IS NOT GUESSABLE. codex 0.154 REFUSES
	// wire_api "chat" outright, and an OpenAI-compatible server may serve one,
	// the other or both — ollama and OVMS each serve /v1/responses, many
	// others do not. A profile that gets this wrong fails at first use with a
	// message about configuration rather than about the endpoint.
	WireAPI string `json:"wire_api,omitempty"`

	// RequiresAuth says the endpoint expects the runtime's vendor credential.
	// A locally served model does not, and saying so is what lets a profile
	// run with no account at all.
	RequiresAuth bool `json:"requires_auth,omitempty"`
}

// Configured reports whether this endpoint names a server.
func (p ProviderEndpoint) Configured() bool { return strings.TrimSpace(p.BaseURL) != "" }

// AgentProfile is one runnable combination of runtime, model and provider.
type AgentProfile struct {
	// Name identifies the profile. It is what a pool refers to.
	Name string `json:"name"`

	// Runtime is the registered backend that runs the agent loop.
	Runtime string `json:"runtime"`

	// Model is what that runtime thinks with. Empty takes the runtime's own
	// default, which is what a CLI with a configured default model wants.
	Model string `json:"model,omitempty"`

	// Provider locates the model. The zero value is the runtime's default.
	Provider ProviderEndpoint `json:"provider,omitempty"`

	// Image is the container image a sandboxed spawn of this profile runs in.
	// Empty takes the caller's.
	Image string `json:"image,omitempty"`

	// ContextTokens is what this profile's model accepts, as the operator
	// knows it. Zero declares nothing and the backend learns the limit from
	// the first refusal.
	//
	// IT IS ON THE PROFILE BECAUSE THE LIMIT IS A PROPERTY OF THE COMBINATION,
	// not of the runtime or the model alone: the same model served with a
	// larger compiled context on one endpoint and a smaller one on another has
	// two different limits, and a fleet routing between them needs to know
	// both. Declaring it is also the only rung that can be right BEFORE the
	// first request — see context_limit.go for the ladder.
	ContextTokens int `json:"context_tokens,omitempty"`

	// Roles says who this profile is FOR and on what terms it helps other
	// roles. The zero value prefers nobody, which is how every profile
	// behaved before the policy existed. See role_policy.go.
	Roles RolePolicy `json:"roles,omitempty"`

	// MaxConcurrent bounds how many jobs may run on this profile at once.
	// Zero means unbounded.
	//
	// IT IS ON THE PROFILE BECAUSE THE LIMIT BELONGS TO THE SERVER, not to the
	// runtime or the model. One NPU serving one model answers two requests at
	// a time whichever runtime asks; a hosted API has no such ceiling. This is
	// the number the pool selects on.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
}

// Validate refuses a profile that cannot be spawned.
func (p AgentProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return &InputValidationError{Field: "profile.name", Message: "a profile is referred to by name and must have one"}
	}
	if strings.TrimSpace(p.Runtime) == "" {
		return &InputValidationError{Field: "profile.runtime", Message: "a profile names the backend that runs it"}
	}
	if p.MaxConcurrent < 0 {
		return &InputValidationError{Field: "profile.max_concurrent", Message: "use zero for unbounded, not a negative"}
	}
	if p.ContextTokens < 0 {
		return &InputValidationError{Field: "profile.context_tokens", Message: "use zero for undeclared, not a negative"}
	}
	if err := p.Roles.Validate(); err != nil {
		return err
	}
	// A provider that names a server must say which dialect it speaks: the
	// runtime cannot probe for it, and guessing produces a failure that reads
	// as a configuration error rather than an endpoint mismatch.
	if p.Provider.Configured() && strings.TrimSpace(p.Provider.WireAPI) == "" {
		return &InputValidationError{
			Field:   "profile.provider.wire_api",
			Message: "an endpoint must state its dialect (\"responses\" or \"chat\"); it cannot be probed",
		}
	}
	return nil
}

// AgentPool is an ORDERED preference list of profile names.
//
// ORDER IS THE POLICY AND LOAD IS THE TIE-BREAK. The first profile with
// capacity wins, so a pool reads as "prefer this, fall back to that" — put the
// local model first and the metered one second and the fleet drains to the
// cheap resource until it is full, then spills. That is the whole of the
// routing policy, and it is data rather than code.
type AgentPool struct {
	// Name identifies the pool.
	Name string `json:"name"`

	// Profiles are profile names, most preferred first.
	Profiles []string `json:"profiles"`
}

// Validate refuses a pool that selects nothing.
func (p AgentPool) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return &InputValidationError{Field: "pool.name", Message: "a pool is referred to by name and must have one"}
	}
	if len(p.Profiles) == 0 {
		return &InputValidationError{Field: "pool.profiles", Message: "a pool with no profiles selects nothing"}
	}
	return nil
}

// ProfileLoadReader answers how many jobs are live on a profile's runtime.
//
// IT IS AN INTERFACE OVER DATA THAT ALREADY EXISTS. JobLease carries the
// backend a job runs on and the lease store already counts live leases per
// backend for the concurrency cap — so placement reads the same number the cap
// enforces, and the two cannot disagree about what "busy" means. Inventing a
// second load signal is how a placer starts admitting work the cap then
// refuses.
type ProfileLoadReader interface {
	// LiveJobsForBackend returns how many jobs hold a live lease naming this
	// backend, across every project of the tenant.
	LiveJobsForBackend(ctx context.Context, tenantID, backend string) (int, error)
}
