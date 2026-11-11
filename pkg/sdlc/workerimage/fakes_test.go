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

package workerimage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// These doubles stand in for the engine, the registry, the binding service,
// the orchestrator and the audit trail. No test in this package builds an
// image, runs a container or reaches a network: the real engine is exercised
// by test/integration/workerimage.

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testPins() softwarefactoryworker.Pins {
	return softwarefactoryworker.Pins{Go: "1.27.0", Golangci: "v2.13.2", Gosec: "v2.29.0",
		Govulncheck: "v1.8.0", Gopls: "v0.23.0", Serena: "v1.7.0", ToolDependencyFloors: "golang.org/x/mod@v0.41.0",
		SerenaDependencyFloors: "urllib3>=2.8.0",
		Actionlint:             "v1.7.12", Shellcheck: "v0.11.0", ShellcheckSHA256X86_64: "8c3b", ShellcheckSHA256Aarch64: "12b3",
		Opencode: "v1.18.32", OpencodeSHA256X86_64: "1ce4", OpencodeSHA256Aarch64: "abf3",
		GH: "v2.103.3", GHSHA256X86_64: "9a9a", GHSHA256Aarch64: "8b8b",
		Protoc: "21.12", ProtocSHA256X86_64: "aaaa", ProtocSHA256Aarch64: "bbbb",
		ProtocGenGo: "v1.36.12", ProtocGenGoGRPC: "v1.6.1",
		Node: "v26.10.0", NodeSHA256X86_64: "cccc", NodeSHA256Aarch64: "dddd", Npm: "12.2.0",
		Uv: "0.12.23", UvSHA256X86_64: "eeee", UvSHA256Aarch64: "ffff", NpmBundledDependencyFloors: "undici@6.28.1=sha512-YmJi",
		Pyright: "1.1.414", Typescript: "6.0.3", TypescriptLanguageServer: "6.0.1",
		BashLanguageServer: "5.8.1", YamlLanguageServer: "1.24.0", RubyLsp: "0.26.11",
		Clangd: "23.1.0", ClangdSHA256X86_64: "a1a1",
		RustAnalyzer: "2026-10-05", RustAnalyzerSHA256X86_64: "b2b2", RustAnalyzerSHA256Aarch64: "c3c3",
		Marksman: "2026-02-08", MarksmanSHA256X86_64: "d4d4", MarksmanSHA256Aarch64: "e5e5",
		KotlinLsp:              "263.6379.0",
		KotlinLspSHA256X86_64:  "ab8ca4455dc2fc5fe1a24db2bccc46c104254d2c465155c4251ee65df8f3f7cc",
		KotlinLspSHA256Aarch64: "50999901ef8bcfa1e58561b6a8d782a72dea5620fcf92a64130807f8924a56fc",
		OpenJDK:                "21"}
}

func testConfig() Config {
	return Config{
		Repository: DefaultRepository,
		SmokeModel: "claude-opus-5-5",
		SmokeHost:  "agent-04",
		Base:       "0.11.0-alpha",
		Pins:       testPins(),
		UID:        os.Getuid(),
		GID:        os.Getgid(),
	}
}

// testGemini is the Gemini CLI version every fake image and registry carries:
// Gemini is not in the content tag, so the refresh cases vary only the tagged
// CLIs.
const testGemini = "0.64.0"

// labelsFor are the labels an image of the given agent versions carries.
func labelsFor(claude, codex string) map[string]string {
	labels := softwarefactoryworker.Build{Base: "0.11.0-alpha",
		Agents: softwarefactoryworker.Agents{ClaudeCode: claude, Codex: codex, Gemini: testGemini}, Pins: testPins()}.Labels()
	labels[softwarefactoryworker.LabelLanguages] = "go typescript yaml bash markdown"
	return labels
}

// fakeEngine is an image store in memory.
type fakeEngine struct {
	mu        sync.Mutex
	images    map[string]*orchestration.ImageInfo
	builds    []*orchestration.ImageBuildSpec
	runs      []*orchestration.ImageRunSpec
	tags      [][2]string
	pushes    []string
	pushAuth  []*orchestration.RegistryAuth
	failProbe string
	buildErr  error
	pushErr   error
	inspectFn func(ref string) (*orchestration.ImageInfo, error)

	// builtLabels overrides the labels a build produces.
	builtLabels map[string]string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{images: map[string]*orchestration.ImageInfo{}}
}

func (e *fakeEngine) hold(ref, id string, labels map[string]string) {
	info := &orchestration.ImageInfo{ID: id, Labels: labels}
	e.images[ref] = info
	e.images[id] = info
}

func (e *fakeEngine) BuildImage(_ context.Context, spec *orchestration.ImageBuildSpec) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.builds = append(e.builds, spec)
	if e.buildErr != nil {
		return "", e.buildErr
	}
	labels := e.builtLabels
	if labels == nil {
		labels = labelsFor(spec.BuildArgs["CLAUDE_CODE_VERSION"], spec.BuildArgs["CODEX_VERSION"])
	}
	id := "sha256:built-" + spec.Tags[0]
	for _, tag := range spec.Tags {
		e.hold(tag, id, labels)
	}
	return id, nil
}

func (e *fakeEngine) InspectImage(_ context.Context, ref string) (*orchestration.ImageInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inspectFn != nil {
		return e.inspectFn(ref)
	}
	info, ok := e.images[ref]
	if !ok {
		return nil, &orchestration.ImageNotFoundError{Image: ref}
	}
	return info, nil
}

func (e *fakeEngine) TagImage(_ context.Context, source, target string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tags = append(e.tags, [2]string{source, target})
	e.images[target] = e.images[source]
	return nil
}

func (e *fakeEngine) PushImage(_ context.Context, ref string, auth *orchestration.RegistryAuth) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pushes = append(e.pushes, ref)
	e.pushAuth = append(e.pushAuth, auth)
	if e.pushErr != nil {
		return "", e.pushErr
	}
	return testDigest, nil
}

// RunImage passes every probe except failProbe, writing the workspace file a
// mounted probe is expected to leave.
func (e *fakeEngine) RunImage(_ context.Context, spec *orchestration.ImageRunSpec) (*orchestration.ExecResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = append(e.runs, spec)
	script := spec.Command[len(spec.Command)-1]
	for _, probe := range softwarefactoryworker.Probes() {
		if probe.Script == script && probe.Name == e.failProbe {
			return &orchestration.ExecResult{ExitCode: 1, Stderr: "probe failed"}, nil
		}
	}
	for _, m := range spec.Mounts {
		if err := os.WriteFile(filepath.Join(m.Source, softwarefactoryworker.ProbeWorkspaceFile), []byte("work"), 0o600); err != nil {
			return nil, err
		}
	}
	return &orchestration.ExecResult{Stdout: "ok"}, nil
}

// fakeVersions answers from a map.
type fakeVersions struct {
	mu       sync.Mutex
	versions map[string]string
	err      error
	calls    int
	called   chan struct{}
}

func newFakeVersions(claude, codex string) *fakeVersions {
	return &fakeVersions{versions: map[string]string{
		softwarefactoryworker.ClaudeCodePackage: claude,
		softwarefactoryworker.CodexPackage:      codex,
		softwarefactoryworker.GeminiPackage:     testGemini,
	}}
}

func (v *fakeVersions) Latest(_ context.Context, pkg string) (string, error) {
	v.mu.Lock()
	v.calls++
	called := v.called
	err := v.err
	version := v.versions[pkg]
	v.mu.Unlock()
	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}
	return version, err
}

// fakeTenants lists fixed tenants.
type fakeTenants []string

func (t fakeTenants) ListTenantIDs(context.Context) ([]string, error) { return t, nil }

// fakeBindings is a binding service in memory.
type fakeBindings struct {
	mu       sync.Mutex
	byTenant map[string][]*sdlc.ProjectBinding
	listErr  map[string]error
}

func (b *fakeBindings) ListBindings(_ context.Context, tenantID string) ([]*sdlc.ProjectBinding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.listErr[tenantID]; err != nil {
		return nil, err
	}
	return b.byTenant[tenantID], nil
}

// fakeAgents is a tenant agent catalog in memory.
type fakeAgents struct {
	mu          sync.Mutex
	byTenant    map[string]map[string]orchestration.Agent
	catalogErr  map[string]error
	replaceErr  map[string]error
	replaceSeen []string
}

func (a *fakeAgents) Catalogue(_ context.Context, tenantID string) (orchestration.AgentCatalogue, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.catalogErr[tenantID]; err != nil {
		return orchestration.AgentCatalogue{}, err
	}
	agents := make(map[string]orchestration.Agent, len(a.byTenant[tenantID]))
	for id, agent := range a.byTenant[tenantID] {
		agents[id] = agent
	}
	return orchestration.AgentCatalogue{Agents: agents, Pools: map[string]orchestration.AgentPool{}}, nil
}

func (a *fakeAgents) ReplaceAgentImage(_ context.Context, tenantID, agentID string, stale []string, image string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replaceSeen = append(a.replaceSeen, agentID)
	if err := a.replaceErr[agentID]; err != nil {
		return false, err
	}
	agent, ok := a.byTenant[tenantID][agentID]
	if !ok {
		return false, &orchestration.UnknownAgentError{ID: agentID}
	}
	if !slices.Contains(stale, agent.Image) {
		return false, nil
	}
	agent.Image = image
	a.byTenant[tenantID][agentID] = agent
	return true, nil
}

func (a *fakeAgents) put(tenantID string, agent orchestration.Agent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.byTenant == nil {
		a.byTenant = map[string]map[string]orchestration.Agent{}
	}
	if a.byTenant[tenantID] == nil {
		a.byTenant[tenantID] = map[string]orchestration.Agent{}
	}
	a.byTenant[tenantID][agent.ID] = agent
}

func (a *fakeAgents) image(tenantID, agentID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byTenant[tenantID][agentID].Image
}

// fakeSmoker answers READY unless the backend is in fail.
type fakeSmoker struct {
	mu       sync.Mutex
	requests []SmokeRequest
	fail     map[string]bool
}

func (s *fakeSmoker) Smoke(_ context.Context, req SmokeRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if s.fail[req.Backend] {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "answer", Cause: errSmokeWrongAnswer}
	}
	return SmokeAnswer, nil
}

// fakeAuditor records events.
type fakeAuditor struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (a *fakeAuditor) RecordWorkerImageEvent(_ context.Context, event AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *fakeAuditor) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.events {
		out = append(out, e.Action)
	}
	return out
}

// fakeObserver records outcomes and signals each one.
type fakeObserver struct {
	mu       sync.Mutex
	outcomes []Outcome
	observed chan Outcome
}

func (o *fakeObserver) ObserveRefresh(outcome Outcome, _ time.Time) {
	o.mu.Lock()
	o.outcomes = append(o.outcomes, outcome)
	observed := o.observed
	o.mu.Unlock()
	if observed != nil {
		observed <- outcome
	}
}

// fakeCredential returns a fixed credential.
type fakeCredential struct{ err error }

func (c fakeCredential) Credential(context.Context) (*orchestration.RegistryAuth, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &orchestration.RegistryAuth{Username: "agent", Secret: []byte("token")}, nil
}

// fakeFleet answers each tenant's remote fleet nodes from a map.
type fakeFleet struct {
	remote map[string][]string
	err    error
	asked  []string
}

func (f *fakeFleet) RemoteFleetNodes(_ context.Context, tenantID string) ([]string, error) {
	f.asked = append(f.asked, tenantID)
	return f.remote[tenantID], f.err
}

// harness wires a refresher over the doubles.
type harness struct {
	engine   *fakeEngine
	versions *fakeVersions
	bindings *fakeBindings
	agents   *fakeAgents
	smoker   *fakeSmoker
	auditor  *fakeAuditor
	observer *fakeObserver
	fleet    *fakeFleet
	r        *Refresher
}

// staleImage is the bound image every default harness binding names.
const staleImage = DefaultRepository + ":0.10.0-alpha"

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	h := &harness{
		engine:   newFakeEngine(),
		versions: newFakeVersions("2.1.280", "0.156.0"),
		bindings: &fakeBindings{byTenant: map[string][]*sdlc.ProjectBinding{
			"acme": {
				{ProjectID: "p1",
					Worker:   orchestration.AgentRefTo("coder"),
					Reviewer: sdlc.ReviewerConfig{Agent: "reviewer", Login: "codex", TokenSecretRef: "tok"}},
				{ProjectID: "p2", Worker: orchestration.AgentRefTo("other")},
			},
		}},
		agents:   &fakeAgents{},
		smoker:   &fakeSmoker{},
		auditor:  &fakeAuditor{},
		observer: &fakeObserver{},
		fleet:    &fakeFleet{},
	}
	h.agents.put("acme", orchestration.Agent{ID: "coder", Backend: "claude", Model: "claude-opus-5-5", Image: staleImage})
	h.agents.put("acme", orchestration.Agent{ID: "reviewer", Backend: "codex", Model: "gpt-5.5", Image: staleImage})
	h.agents.put("acme", orchestration.Agent{ID: "other", Backend: "claude", Model: "claude-opus-5-5", Image: "someone-else/worker:1"})
	h.engine.hold(staleImage, "sha256:old", labelsFor("2.1.278", "0.150.0"))
	r, err := NewRefresher(cfg, Deps{
		Engine: h.engine, Versions: h.versions, Tenants: fakeTenants{"acme"}, Bindings: h.bindings, Agents: h.agents,
		Smoker: h.smoker, Auditor: h.auditor, Observer: h.observer,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hosts:  &fakeHosts{host: "resolved-local-agent"},
		Fleet:  h.fleet,
	})
	if err != nil {
		t.Fatalf("NewRefresher: %v", err)
	}
	h.r = r
	return h
}

var errFake = errors.New("fake failure")
