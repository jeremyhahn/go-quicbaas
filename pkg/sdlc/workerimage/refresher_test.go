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
	"slices"
	"strings"
	"sync"
	"testing"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

const candidateTag = DefaultRepository + ":0.11.0-alpha-cc2.1.280-cx0.156.0"

func TestRefresh_PromotesAStaleImageInLocalMode(t *testing.T) {
	h := newHarness(t, testConfig())

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s), want promoted", report.Outcome, report.Error)
	}
	wantBound := []BoundImage{{Image: staleImage, Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.278", Codex: "0.150.0", Gemini: testGemini}, Uses: 2, Stale: true}}
	if !slices.Equal(report.Bound, wantBound) {
		t.Fatalf("bound = %+v, want the bound image with its CLI versions and both roles", report.Bound)
	}
	if report.Candidate != candidateTag || report.Reference != candidateTag {
		t.Fatalf("candidate=%q reference=%q, want the local content tag", report.Candidate, report.Reference)
	}
	if len(h.engine.builds) != 1 {
		t.Fatalf("want one build, got %d", len(h.engine.builds))
	}
	build := h.engine.builds[0]
	if build.BuildArgs["CLAUDE_CODE_VERSION"] != "2.1.280" || build.BuildArgs["CODEX_VERSION"] != "0.156.0" || !build.Pull {
		t.Fatalf("build = %+v", build)
	}
	if string(build.Dockerfile) != string(softwarefactoryworker.Dockerfile) {
		t.Fatal("the build must use the embedded Dockerfile")
	}
	for _, run := range h.engine.runs {
		if run.Image != report.ImageID {
			t.Fatalf("a probe ran against %q, not the built image ID %q", run.Image, report.ImageID)
		}
	}
	if len(report.Probes) != len(softwarefactoryworker.Probes()) {
		t.Fatalf("want every probe reported, got %d", len(report.Probes))
	}

	if h.agents.image("acme", "coder") != candidateTag || h.agents.image("acme", "reviewer") != candidateTag {
		t.Fatalf("p1's agents not repointed: %q %q", h.agents.image("acme", "coder"), h.agents.image("acme", "reviewer"))
	}
	if got := h.agents.image("acme", "other"); got != "someone-else/worker:1" {
		t.Fatalf("an unmanaged image was touched: %q", got)
	}
	if got := h.auditor.actions(); !slices.Equal(got, []string{AuditActionPromote, AuditActionPromote, AuditActionRefresh}) {
		t.Fatalf("one promotion is audited per agent: %v", got)
	}
	if !slices.Equal(h.observer.outcomes, []Outcome{OutcomePromoted}) {
		t.Fatalf("observed %v", h.observer.outcomes)
	}
	if len(h.engine.pushes) != 0 {
		t.Fatal("local mode must not push")
	}
}

func TestRefresh_SmokesEveryAgentThePromotionWouldRunPlusTheDefaultModel(t *testing.T) {
	h := newHarness(t, testConfig())
	h.r.Refresh(t.Context(), TriggerManual)

	var got []string
	for _, req := range h.smoker.requests {
		got = append(got, req.Backend+"/"+req.Model)
		if req.Image != candidateTag || req.Host != "agent-04" || req.TenantID != "acme" || req.ProjectID != "p1" {
			t.Fatalf("smoke request = %+v", req)
		}
	}
	want := []string{"claude/claude-opus-5-5", "codex/gpt-5.5"}
	if !slices.Equal(got, want) {
		t.Fatalf("smokes = %v, want %v (the default model is not repeated)", got, want)
	}
}

func TestRefresh_PublishesThenBindsTheDigestPinnedReference(t *testing.T) {
	cfg := testConfig()
	cfg.Registry = "198.51.100.50:3000/jeremyhahn/software-factory-worker"
	cfg.Credential = fakeCredential{}
	h := newHarness(t, cfg)

	report := h.r.Refresh(t.Context(), TriggerManual)

	remote := cfg.Registry + ":0.11.0-alpha-cc2.1.280-cx0.156.0"
	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s)", report.Outcome, report.Error)
	}
	if !slices.Equal(h.engine.pushes, []string{remote}) || h.engine.pushAuth[0] == nil {
		t.Fatalf("pushes = %v auth = %v", h.engine.pushes, h.engine.pushAuth)
	}
	if report.Digest != testDigest || report.Reference != remote+"@"+testDigest {
		t.Fatalf("digest=%q reference=%q", report.Digest, report.Reference)
	}
	if got := h.agents.image("acme", "coder"); got != remote+"@"+testDigest {
		t.Fatalf("worker image = %q, want the digest-pinned registry reference", got)
	}
}

func TestRefresh_ARegistryReferenceIsManagedToo(t *testing.T) {
	cfg := testConfig()
	cfg.Registry = "reg.example:3000/o/worker"
	h := newHarness(t, cfg)
	pinned := cfg.Registry + ":0.10.0-alpha@" + testDigest
	h.agents.put("acme", orchestration.Agent{ID: "coder", Backend: "claude", Model: "claude-opus-5-5", Image: pinned})
	h.agents.put("acme", orchestration.Agent{ID: "reviewer", Backend: "codex", Model: "gpt-5.5", Image: pinned})
	h.engine.hold(pinned, "sha256:old-remote", labelsFor("2.1.278", "0.150.0"))

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted || !slices.Equal(report.Stale, []string{pinned}) {
		t.Fatalf("outcome=%s stale=%v err=%s", report.Outcome, report.Stale, report.Error)
	}
}

func TestRefresh_CurrentImagesBuildNothingAndAuditNothing(t *testing.T) {
	h := newHarness(t, testConfig())
	h.engine.hold(staleImage, "sha256:old", labelsFor("2.1.280", "0.156.0"))

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	if len(report.Bound) != 1 || report.Bound[0].Stale || report.Bound[0].Agents.ClaudeCode != "2.1.280" {
		t.Fatalf("bound = %+v", report.Bound)
	}
	if report.Outcome != OutcomeCurrent || len(h.engine.builds) != 0 || len(h.auditor.events) != 0 {
		t.Fatalf("outcome=%s builds=%d audit=%d", report.Outcome, len(h.engine.builds), len(h.auditor.events))
	}
}

func TestRefresh_ARegistryThatMovesBackwardsNeverDowngrades(t *testing.T) {
	h := newHarness(t, testConfig())
	h.engine.hold(staleImage, "sha256:old", labelsFor("2.1.290", "0.160.0"))

	if report := h.r.Refresh(t.Context(), TriggerSchedule); report.Outcome != OutcomeCurrent {
		t.Fatalf("outcome = %s, want current", report.Outcome)
	}
}

func TestRefresh_AnImageWithUnknownVersionsIsStale(t *testing.T) {
	cases := map[string]func(h *harness){
		"unlabelled": func(h *harness) { h.engine.hold(staleImage, "sha256:old", map[string]string{}) },
		"not held":   func(h *harness) { delete(h.engine.images, staleImage) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, testConfig())
			setup(h)
			report := h.r.Refresh(t.Context(), TriggerSchedule)
			if report.Outcome != OutcomePromoted {
				t.Fatalf("outcome = %s (%s), want promoted", report.Outcome, report.Error)
			}
		})
	}
}

func TestRefresh_ReusesACorrectlyLabelledCandidate(t *testing.T) {
	h := newHarness(t, testConfig())
	h.engine.hold(candidateTag, "sha256:earlier-build", labelsFor("2.1.280", "0.156.0"))

	report := h.r.Refresh(t.Context(), TriggerManual)

	if len(h.engine.builds) != 0 || report.ImageID != "sha256:earlier-build" {
		t.Fatalf("builds=%d id=%q, want the held candidate reused", len(h.engine.builds), report.ImageID)
	}
	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s)", report.Outcome, report.Error)
	}
}

// assertUnchanged fails when a failed pass touched a binding or published.
func assertUnchanged(t *testing.T, h *harness, report *Report) {
	t.Helper()
	if report.Outcome != OutcomeFailed || report.Error == "" {
		t.Fatalf("outcome = %s error = %q, want a failed pass that says why", report.Outcome, report.Error)
	}
	if got := h.agents.image("acme", "coder"); got != staleImage {
		t.Fatalf("a failed pass rebound p1 to %q", got)
	}
	if !slices.Equal(h.observer.outcomes, []Outcome{OutcomeFailed}) {
		t.Fatalf("observed %v", h.observer.outcomes)
	}
	events := h.auditor.events
	if len(events) != 1 || events[0].Action != AuditActionRefresh || events[0].Success {
		t.Fatalf("a failed pass must leave one failed refresh audit entry, got %+v", events)
	}
}

func TestRefresh_FailuresLeaveBindingsAlone(t *testing.T) {
	cases := map[string]struct {
		cfg   func(*Config)
		setup func(*harness)
		want  error
	}{
		"registry unavailable": {setup: func(h *harness) { h.versions.err = errFake }, want: errFake},
		"build fails":          {setup: func(h *harness) { h.engine.buildErr = errFake }, want: errFake},
		"a probe fails": {setup: func(h *harness) {
			h.engine.failProbe = softwarefactoryworker.Probes()[3].Name
		}, want: ErrGate},
		"a smoke fails":    {setup: func(h *harness) { h.smoker.fail = map[string]bool{"codex": true} }, want: ErrGate},
		"labels are wrong": {setup: func(h *harness) { h.engine.builtLabels = labelsFor("2.1.280", "0.1.0") }, want: ErrGate},
		"no languages label": {setup: func(h *harness) {
			h.engine.builtLabels = labelsFor("2.1.280", "0.156.0")
			delete(h.engine.builtLabels, softwarefactoryworker.LabelLanguages)
		}, want: ErrGate},
		"inspect is broken":     {setup: func(h *harness) { h.engine.inspectFn = brokenInspect }, want: errFake},
		"push fails":            {cfg: withRegistry, setup: func(h *harness) { h.engine.pushErr = errFake }, want: errFake},
		"credential unreadable": {cfg: withCredentialError, want: errFake},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			h := newHarness(t, cfg)
			if tc.setup != nil {
				tc.setup(h)
			}
			report := h.r.Refresh(t.Context(), TriggerManual)
			assertUnchanged(t, h, report)
		})
	}
}

func TestCheckLanguages_AcceptsKnownLanguages(t *testing.T) {
	labels := map[string]string{softwarefactoryworker.LabelLanguages: "go typescript  yaml"}
	if err := checkLanguages(candidateTag, labels); err != nil {
		t.Fatalf("checkLanguages = %v, want nil for known serena languages", err)
	}
}

func TestCheckLanguages_RefusesAMissingEmptyOrUnknownList(t *testing.T) {
	cases := map[string]struct {
		labels map[string]string
		want   string
	}{
		"missing": {labels: map[string]string{}, want: softwarefactoryworker.LabelLanguages + " is missing or empty"},
		"blank":   {labels: map[string]string{softwarefactoryworker.LabelLanguages: "  "}, want: softwarefactoryworker.LabelLanguages + " is missing or empty"},
		"unknown": {labels: map[string]string{softwarefactoryworker.LabelLanguages: "go cobol"}, want: `unknown language "cobol"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkLanguages(candidateTag, tc.labels)
			var gate *GateError
			if !errors.As(err, &gate) || !errors.Is(err, ErrGate) {
				t.Fatalf("checkLanguages = %v, want a *GateError", err)
			}
			if gate.Image != candidateTag || !slices.ContainsFunc(gate.Failed, func(f string) bool { return strings.Contains(f, tc.want) }) {
				t.Fatalf("gate = %+v, want image %q and a failure naming %q", gate, candidateTag, tc.want)
			}
		})
	}
}

func withRegistry(cfg *Config) { cfg.Registry = "reg.example:3000/o/worker" }

func withCredentialError(cfg *Config) {
	withRegistry(cfg)
	cfg.Credential = fakeCredential{err: errFake}
}

func brokenInspect(string) (*orchestration.ImageInfo, error) { return nil, errFake }

func TestRefresh_AFailedGatePublishesNothing(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	h := newHarness(t, cfg)
	h.smoker.fail = map[string]bool{"claude": true}

	h.r.Refresh(t.Context(), TriggerManual)

	if len(h.engine.pushes) != 0 || len(h.engine.tags) != 0 {
		t.Fatalf("an image that failed its gate was published: tags=%v pushes=%v", h.engine.tags, h.engine.pushes)
	}
}

func TestRefresh_ATagMovedDuringTheGateFailsIt(t *testing.T) {
	h := newHarness(t, testConfig())
	inspections := 0
	h.engine.inspectFn = func(ref string) (*orchestration.ImageInfo, error) {
		info, ok := h.engine.images[ref]
		if !ok {
			return nil, &orchestration.ImageNotFoundError{Image: ref}
		}
		if ref == candidateTag {
			inspections++
			if inspections >= 1 {
				return &orchestration.ImageInfo{ID: "sha256:someone-else", Labels: info.Labels}, nil
			}
		}
		return info, nil
	}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if !strings.Contains(report.Error, "moved") {
		t.Fatalf("error = %q, want the moved tag named", report.Error)
	}
	assertUnchanged(t, h, report)
}

func TestRefresh_ABindingThatCannotBeReboundIsPartial(t *testing.T) {
	h := newHarness(t, testConfig())
	h.bindings.byTenant["acme"] = append(h.bindings.byTenant["acme"],
		&sdlc.ProjectBinding{ProjectID: "p3", Worker: orchestration.AgentRefTo("coder3")})
	h.agents.put("acme", orchestration.Agent{ID: "coder3", Backend: "claude", Model: "claude-opus-5-5", Image: staleImage})
	h.agents.replaceErr = map[string]error{"coder": errFake}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePartial {
		t.Fatalf("outcome = %s, want partial", report.Outcome)
	}
	if got := h.agents.image("acme", "coder3"); got != candidateTag {
		t.Fatalf("p3 must still be rebound, got %q", got)
	}
	var failed int
	for _, p := range report.Promotions {
		if p.Error != "" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("want one failed promotion reported, got %+v", report.Promotions)
	}
}

func TestRefresh_AnUnreadableTenantIsSkippedAndReported(t *testing.T) {
	h := newHarness(t, testConfig())
	h.r.deps.Tenants = fakeTenants{"acme", "sealed"}
	h.bindings.listErr = map[string]error{"sealed": errFake}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePartial || len(report.Skipped) != 1 || !strings.HasPrefix(report.Skipped[0], "sealed:") {
		t.Fatalf("outcome=%s skipped=%v", report.Outcome, report.Skipped)
	}
}

// recordingHandler keeps every record's level and message.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) errors() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level >= slog.LevelError {
			n++
		}
	}
	return n
}

// assertSurfacedFailure pins the failure surface: an error log, the outcome
// counted, and a failed refresh audit entry.
func assertSurfacedFailure(t *testing.T, h *harness, logs *recordingHandler, want Outcome) {
	t.Helper()
	if logs.errors() == 0 {
		t.Error("the pass was not logged at error level")
	}
	if !slices.Equal(h.observer.outcomes, []Outcome{want}) {
		t.Errorf("observed %v, want [%s]", h.observer.outcomes, want)
	}
	var audited bool
	for _, e := range h.auditor.events {
		if e.Action == AuditActionRefresh && !e.Success && e.Details["outcome"] == string(want) {
			audited = true
		}
	}
	if !audited {
		t.Errorf("no failed refresh audit entry: %+v", h.auditor.events)
	}
}

func TestRefresh_NoReadableTenantIsAFailedPassNotCurrent(t *testing.T) {
	h := newHarness(t, testConfig())
	logs := &recordingHandler{}
	h.r.deps.Logger = slog.New(logs)
	h.r.deps.Tenants = fakeTenants{"acme", "sealed"}
	h.bindings.listErr = map[string]error{"acme": errFake, "sealed": errFake}

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	if report.Outcome != OutcomeFailed || !strings.Contains(report.Error, ErrBindingsUnreadable.Error()) {
		t.Fatalf("outcome=%s error=%q, want a failed pass naming the unreadable bindings", report.Outcome, report.Error)
	}
	if len(report.Skipped) != 2 || len(h.engine.builds) != 0 {
		t.Fatalf("skipped=%v builds=%d", report.Skipped, len(h.engine.builds))
	}
	assertSurfacedFailure(t, h, logs, OutcomeFailed)
}

func TestRefresh_AnUnreadableTenantBesideCurrentOnesIsPartialNotCurrent(t *testing.T) {
	h := newHarness(t, testConfig())
	logs := &recordingHandler{}
	h.r.deps.Logger = slog.New(logs)
	h.engine.hold(staleImage, "sha256:old", labelsFor("2.1.280", "0.156.0"))
	h.r.deps.Tenants = fakeTenants{"acme", "sealed"}
	h.bindings.listErr = map[string]error{"sealed": errFake}

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	if report.Outcome != OutcomePartial || report.Error == "" || len(h.engine.builds) != 0 {
		t.Fatalf("outcome=%s error=%q builds=%d", report.Outcome, report.Error, len(h.engine.builds))
	}
	assertSurfacedFailure(t, h, logs, OutcomePartial)
}

func TestRefresh_NeverTradesOneCLIForTheOther(t *testing.T) {
	cases := map[string]struct {
		claude, codex string
		wantOutcome   Outcome
	}{
		// Claude ahead of the registry, Codex behind: the build would
		// downgrade Claude to bring Codex forward.
		"claude ahead, codex behind": {claude: "2.1.290", codex: "0.150.0", wantOutcome: OutcomeFailed},
		"codex ahead, claude behind": {claude: "2.1.278", codex: "0.160.0", wantOutcome: OutcomeFailed},
		"both behind":                {claude: "2.1.278", codex: "0.150.0", wantOutcome: OutcomePromoted},
		"one behind, one equal":      {claude: "2.1.280", codex: "0.150.0", wantOutcome: OutcomePromoted},
		"both ahead":                 {claude: "2.1.290", codex: "0.160.0", wantOutcome: OutcomeCurrent},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, testConfig())
			h.engine.hold(staleImage, "sha256:old", labelsFor(tc.claude, tc.codex))

			report := h.r.Refresh(t.Context(), TriggerSchedule)

			if report.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %s (%s), want %s", report.Outcome, report.Error, tc.wantOutcome)
			}
			if tc.wantOutcome != OutcomeFailed {
				return
			}
			if !strings.Contains(report.Error, ErrDowngrade.Error()) || len(h.engine.builds) != 0 {
				t.Fatalf("error=%q builds=%d, want the downgrade refused before any build", report.Error, len(h.engine.builds))
			}
			if got := h.agents.image("acme", "coder"); got != staleImage {
				t.Fatalf("a refused promotion rebound the binding to %q", got)
			}
		})
	}
}

func TestRefuseDowngrade(t *testing.T) {
	latest := softwarefactoryworker.Agents{ClaudeCode: "2.1.280", Codex: "0.156.0"}
	err := refuseDowngrade([]BoundImage{
		{Image: "current", Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.300", Codex: "0.156.0"}},
		{Image: "stale", Stale: true, Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.278", Codex: "0.157.1"}},
	}, latest)
	var refused *DowngradeRefusedError
	if !errors.As(err, &refused) || refused.Image != "stale" || refused.Component != softwarefactoryworker.CodexPackage ||
		refused.Bound != "0.157.1" || refused.Latest != "0.156.0" {
		t.Fatalf("err = %v", err)
	}
	// A stale image ahead in Gemini would lose it to the rebuild too.
	latest.Gemini = "0.64.0"
	err = refuseDowngrade([]BoundImage{{Image: "gemini-ahead", Stale: true,
		Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.278", Codex: "0.156.0", Gemini: "0.65.0"}}}, latest)
	if !errors.As(err, &refused) || refused.Component != softwarefactoryworker.GeminiPackage ||
		refused.Bound != "0.65.0" || refused.Latest != "0.64.0" {
		t.Fatalf("gemini ahead: err = %v", err)
	}
	unknown := []BoundImage{{Image: "unlabelled", Stale: true}, {Image: "odd", Stale: true,
		Agents: softwarefactoryworker.Agents{ClaudeCode: "nightly", Codex: "0.1.0"}}}
	if err := refuseDowngrade(unknown, latest); err != nil {
		t.Fatalf("unknown versions have nothing to lose: %v", err)
	}
}

func TestRefresh_AnUnreadableRegistryIsAFailedPass(t *testing.T) {
	h := newHarness(t, testConfig())
	h.versions.err = errFake

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomeFailed || h.r.Status().Last != report || len(h.engine.builds) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRefresh_ReadsTheRegistryOncePerPass(t *testing.T) {
	h := newHarness(t, testConfig())
	h.r.Refresh(t.Context(), TriggerManual)
	if h.versions.calls != 3 {
		t.Fatalf("registry lookups = %d, want one per package per pass", h.versions.calls)
	}
}

func TestNewRefresher_RefusesAnIncompleteConfiguration(t *testing.T) {
	cases := map[string]func(*Config){
		"no repository":         func(c *Config) { c.Repository = "" },
		"tagged repository":     func(c *Config) { c.Repository = "worker:1" },
		"digest repository":     func(c *Config) { c.Repository = "worker@" + testDigest },
		"registry without path": func(c *Config) { c.Registry = "registry" },
		"tagged registry":       func(c *Config) { c.Registry = "reg.example:3000/o/w:1" },
		"no smoke model":        func(c *Config) { c.SmokeModel = " " },
		"negative uid":          func(c *Config) { c.UID = -1 },
		"bad base":              func(c *Config) { c.Base = "1.0+x" },
		"missing pin":           func(c *Config) { c.Pins.Serena = "" },
	}
	deps := Deps{
		Engine: newFakeEngine(), Versions: newFakeVersions("1", "1"), Tenants: fakeTenants{},
		Bindings: &fakeBindings{}, Agents: &fakeAgents{}, Smoker: &fakeSmoker{}, Auditor: &fakeAuditor{}, Observer: &fakeObserver{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Fleet: &fakeFleet{},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			mutate(&cfg)
			if _, err := NewRefresher(cfg, deps); err == nil {
				t.Fatal("NewRefresher accepted an invalid configuration")
			}
		})
	}
	t.Run("missing collaborator", func(t *testing.T) {
		incomplete := deps
		incomplete.Smoker = nil
		if _, err := NewRefresher(testConfig(), incomplete); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("err = %v, want ErrInvalidConfig", err)
		}
	})
}

func TestVersionBehind(t *testing.T) {
	cases := []struct {
		have, latest string
		want         bool
	}{
		{"2.1.278", "2.1.280", true},
		{"2.1.280", "2.1.280", false},
		{"2.1.290", "2.1.280", false},
		{"", "2.1.280", true},
		{"0.157.0-alpha.9", "0.157.0", true},
		{"weird", "other", true},
		{"same", "same", false},
	}
	for _, tc := range cases {
		if got := versionBehind(tc.have, tc.latest); got != tc.want {
			t.Errorf("versionBehind(%q, %q) = %v, want %v", tc.have, tc.latest, got, tc.want)
		}
	}
}

// fakeHosts resolves every smoke to one agent, or refuses.
type fakeHosts struct {
	host  string
	err   error
	asked []string
}

func (f *fakeHosts) LocalFleetNodeHost(_ context.Context, tenantID, backend string) (string, error) {
	f.asked = append(f.asked, tenantID+"/"+backend)
	return f.host, f.err
}

func TestRefresh_ResolvesTheLocalAgentForEachSmokeWhenNoHostIsConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.SmokeHost = ""
	h := newHarness(t, cfg)
	hosts := &fakeHosts{host: "embedded-agent-04"}
	h.r.deps.Hosts = hosts

	if report := h.r.Refresh(t.Context(), TriggerManual); report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s)", report.Outcome, report.Error)
	}
	if len(hosts.asked) != 2 || hosts.asked[1] != "acme/codex" {
		t.Fatalf("asked = %v, want one resolution per smoke", hosts.asked)
	}
	for _, req := range h.smoker.requests {
		if req.Host != "embedded-agent-04" {
			t.Fatalf("smoke placed on %q", req.Host)
		}
	}
}

func TestRefresh_AnUnresolvableLocalAgentFailsTheGate(t *testing.T) {
	cfg := testConfig()
	cfg.SmokeHost = ""
	h := newHarness(t, cfg)
	h.r.deps.Hosts = &fakeHosts{err: errFake}

	report := h.r.Refresh(t.Context(), TriggerManual)

	assertUnchanged(t, h, report)
	if len(h.smoker.requests) != 0 || !strings.Contains(report.Smokes[0].Detail, "placement") {
		t.Fatalf("smokes = %+v", report.Smokes)
	}
}

func TestNewRefresher_NeedsAHostResolverWithoutASmokeHost(t *testing.T) {
	cfg := testConfig()
	cfg.SmokeHost = ""
	_, err := NewRefresher(cfg, Deps{
		Engine: newFakeEngine(), Versions: newFakeVersions("1", "1"), Tenants: fakeTenants{},
		Bindings: &fakeBindings{}, Agents: &fakeAgents{}, Smoker: &fakeSmoker{}, Auditor: &fakeAuditor{}, Observer: &fakeObserver{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Fleet: &fakeFleet{},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}

// TestRefresh_AnAgentTwoBindingsShareIsPromotedOnce: the image is the Agent's,
// so one write repoints every binding that uses it.
func TestRefresh_AnAgentTwoBindingsShareIsPromotedOnce(t *testing.T) {
	h := newHarness(t, testConfig())
	h.bindings.byTenant["acme"] = append(h.bindings.byTenant["acme"],
		&sdlc.ProjectBinding{ProjectID: "p0", Worker: orchestration.AgentRefTo("coder")})

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s)", report.Outcome, report.Error)
	}
	if !slices.Equal(h.agents.replaceSeen, []string{"coder", "reviewer"}) {
		t.Fatalf("repointed %v, want each agent once", h.agents.replaceSeen)
	}
	// The smoke of the shared agent runs under the first project that uses it.
	for _, req := range h.smoker.requests {
		if req.Backend == "claude" && req.ProjectID != "p0" {
			t.Fatalf("the shared agent's smoke ran under %q, want the first project by ID", req.ProjectID)
		}
	}
}

// TestRefresh_APoolsMembersAreEachRefreshed: a slot naming a pool runs every
// member, so every managed member's image is kept current.
func TestRefresh_APoolsMembersAreEachRefreshed(t *testing.T) {
	h := newHarness(t, testConfig())
	h.bindings.byTenant["acme"] = []*sdlc.ProjectBinding{{ProjectID: "p1", Worker: orchestration.PoolRefTo("coders")}}
	h.agents.put("acme", orchestration.Agent{ID: "coder2", Backend: "codex", Model: "gpt-5.5", Image: staleImage})
	h.r.deps.Agents = poolAgents{fakeAgents: h.agents, pool: orchestration.AgentPool{ID: "coders", Agents: []string{"coder", "coder2"}}}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted || h.agents.image("acme", "coder2") != candidateTag || h.agents.image("acme", "coder") != candidateTag {
		t.Fatalf("outcome=%s err=%s coder2=%q", report.Outcome, report.Error, h.agents.image("acme", "coder2"))
	}
}

// poolAgents is fakeAgents with one pool in the catalog.
type poolAgents struct {
	*fakeAgents
	pool orchestration.AgentPool
}

func (p poolAgents) Catalogue(ctx context.Context, tenantID string) (orchestration.AgentCatalogue, error) {
	catalogue, err := p.fakeAgents.Catalogue(ctx, tenantID)
	catalogue.Pools[p.pool.ID] = p.pool
	return catalogue, err
}

// TestRefresh_ATenantWhoseCatalogCannotBeReadIsSkipped: the bindings alone do
// not say which image an Agent runs.
func TestRefresh_ATenantWhoseCatalogCannotBeReadIsSkipped(t *testing.T) {
	h := newHarness(t, testConfig())
	h.r.deps.Tenants = fakeTenants{"acme", "sealed"}
	h.agents.catalogErr = map[string]error{"sealed": errFake}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePartial || len(report.Skipped) != 1 || !strings.HasPrefix(report.Skipped[0], "sealed:") {
		t.Fatalf("outcome=%s skipped=%v", report.Outcome, report.Skipped)
	}
}

func TestNewRefresher_NeedsAgents(t *testing.T) {
	_, err := NewRefresher(testConfig(), Deps{
		Engine: newFakeEngine(), Versions: newFakeVersions("1", "1"), Tenants: fakeTenants{},
		Bindings: &fakeBindings{}, Smoker: &fakeSmoker{}, Auditor: &fakeAuditor{}, Observer: &fakeObserver{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Hosts: &fakeHosts{}, Fleet: &fakeFleet{},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want refused for no agents", err)
	}
}

func TestRefresh_LocalModeRefusesToStrandRemoteFleetNodes(t *testing.T) {
	h := newHarness(t, testConfig())
	h.fleet.remote = map[string][]string{"acme": {"worker-host-2@10.0.0.5", "build-host@10.0.0.2"}}

	report := h.r.Refresh(t.Context(), TriggerManual)

	assertUnchanged(t, h, report)
	if !strings.Contains(report.Error, ErrLocalPromotion.Error()) || !strings.Contains(report.Error, "build-host@10.0.0.2, worker-host-2@10.0.0.5") {
		t.Fatalf("error = %q, want the refusal naming the remote nodes, sorted", report.Error)
	}
	if len(h.engine.builds) != 0 {
		t.Fatal("a pass that may not promote must not build")
	}
	if !slices.Equal(h.fleet.asked, []string{"acme"}) {
		t.Fatalf("fleet asked for %v, want the stale Agents' tenant", h.fleet.asked)
	}
}

func TestRefresh_LocalModeFailsWhenTheFleetCannotBeRead(t *testing.T) {
	h := newHarness(t, testConfig())
	h.fleet.err = errFake

	report := h.r.Refresh(t.Context(), TriggerManual)

	assertUnchanged(t, h, report)
	if len(h.engine.builds) != 0 {
		t.Fatal("a pass that cannot prove the fleet is local must not build")
	}
}

func TestRefresh_ARegistryPromotesWhateverTheFleetLooksLike(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	h := newHarness(t, cfg)
	h.fleet.remote = map[string][]string{"acme": {"build-host@10.0.0.2"}}

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s), want promoted", report.Outcome, report.Error)
	}
	if len(h.fleet.asked) != 0 {
		t.Fatal("a pass that publishes has no local tag to guard")
	}
}

// currentLocal makes the harness's bound image current under a local tag.
func currentLocal(h *harness) {
	h.engine.hold(staleImage, "sha256:current-local", labelsFor("2.1.280", "0.156.0"))
}

func TestRefresh_ARegistryPublishesACurrentLocalImageWithoutRebuilding(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	cfg.Credential = fakeCredential{}
	h := newHarness(t, cfg)
	currentLocal(h)

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	remote := cfg.Registry + ":0.10.0-alpha"
	pinned := remote + "@" + testDigest
	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s), want promoted", report.Outcome, report.Error)
	}
	if len(h.engine.builds) != 0 || len(h.engine.runs) != 0 || len(h.smoker.requests) != 0 {
		t.Fatalf("builds=%d probes=%d smokes=%d, want the held image published as it is",
			len(h.engine.builds), len(h.engine.runs), len(h.smoker.requests))
	}
	if !slices.Equal(h.engine.tags, [][2]string{{"sha256:current-local", remote}}) {
		t.Fatalf("tags = %v, want the held image ID tagged into the registry under its own tag", h.engine.tags)
	}
	if !slices.Equal(h.engine.pushes, []string{remote}) || h.engine.pushAuth[0] == nil {
		t.Fatalf("pushes = %v auth = %v, want one authenticated push of the image both Agents share", h.engine.pushes, h.engine.pushAuth)
	}
	for _, agent := range []string{"coder", "reviewer"} {
		if got := h.agents.image("acme", agent); got != pinned {
			t.Fatalf("%s = %q, want %q", agent, got, pinned)
		}
	}
	if got := h.agents.image("acme", "other"); got != "someone-else/worker:1" {
		t.Fatalf("an unmanaged image was touched: %q", got)
	}
	if got := h.auditor.actions(); !slices.Equal(got, []string{AuditActionPromote, AuditActionPromote, AuditActionRefresh}) {
		t.Fatalf("audit = %v, want each repoint audited", got)
	}
}

func TestRefresh_AFailedPublishOfAHeldImageIsPartial(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	h := newHarness(t, cfg)
	currentLocal(h)
	h.engine.pushErr = errFake

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	if report.Outcome != OutcomePartial || !strings.Contains(report.Error, "2 of 2 agents") {
		t.Fatalf("outcome = %s (%s), want partial naming both Agents", report.Outcome, report.Error)
	}
	for _, promotion := range report.Promotions {
		if !strings.Contains(promotion.Error, errFake.Error()) {
			t.Fatalf("promotion = %+v, want the push failure recorded", promotion)
		}
	}
	if got := h.agents.image("acme", "coder"); got != staleImage {
		t.Fatalf("coder = %q, want it left on the local tag", got)
	}
}

func TestRefresh_ACurrentRegistryReferenceIsNotRepublished(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	h := newHarness(t, cfg)
	pinned := cfg.Registry + ":0.10.0-alpha@" + testDigest
	h.agents.put("acme", orchestration.Agent{ID: "coder", Backend: "claude", Model: "claude-opus-5-5", Image: pinned})
	h.agents.put("acme", orchestration.Agent{ID: "reviewer", Backend: "codex", Model: "gpt-5.5", Image: pinned})
	h.engine.hold(pinned, "sha256:remote", labelsFor("2.1.280", "0.156.0"))

	report := h.r.Refresh(t.Context(), TriggerSchedule)

	if report.Outcome != OutcomeCurrent || len(h.engine.pushes) != 0 {
		t.Fatalf("outcome=%s pushes=%v, want nothing to do", report.Outcome, h.engine.pushes)
	}
}

func TestTagOf(t *testing.T) {
	cases := map[string]string{
		DefaultRepository + ":0.10.0-alpha":               "0.10.0-alpha",
		DefaultRepository:                                 "latest",
		"localhost:5000/o/w:1.2":                          "1.2",
		"localhost:5000/o/w":                              "latest",
		DefaultRepository + ":0.10.0-alpha@" + testDigest: "0.10.0-alpha",
	}
	for image, want := range cases {
		if got := tagOf(image); got != want {
			t.Errorf("tagOf(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestNewRefresher_NeedsTheFleet(t *testing.T) {
	_, err := NewRefresher(testConfig(), Deps{
		Engine: newFakeEngine(), Versions: newFakeVersions("1", "1"), Tenants: fakeTenants{},
		Bindings: &fakeBindings{}, Agents: &fakeAgents{}, Smoker: &fakeSmoker{}, Auditor: &fakeAuditor{}, Observer: &fakeObserver{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Hosts: &fakeHosts{},
	})
	var invalid *InvalidConfigError
	if !errors.As(err, &invalid) || invalid.Field != "fleet" {
		t.Fatalf("err = %v, want refused for no fleet", err)
	}
}

func TestRefresh_ARegistryRefreshesStaleImagesAndPublishesCurrentOnesInOnePass(t *testing.T) {
	cfg := testConfig()
	withRegistry(&cfg)
	h := newHarness(t, cfg)
	held := DefaultRepository + ":0.11.0-held"
	h.engine.hold(held, "sha256:held", labelsFor("2.1.280", "0.156.0"))
	h.agents.put("acme", orchestration.Agent{ID: "reviewer", Backend: "codex", Model: "gpt-5.5", Image: held})

	report := h.r.Refresh(t.Context(), TriggerManual)

	candidate := cfg.Registry + ":0.11.0-alpha-cc2.1.280-cx0.156.0@" + testDigest
	published := cfg.Registry + ":0.11.0-held@" + testDigest
	if report.Outcome != OutcomePromoted || len(h.engine.builds) != 1 {
		t.Fatalf("outcome=%s builds=%d (%s)", report.Outcome, len(h.engine.builds), report.Error)
	}
	if got := h.agents.image("acme", "coder"); got != candidate {
		t.Fatalf("coder = %q, want the refreshed candidate %q", got, candidate)
	}
	if got := h.agents.image("acme", "reviewer"); got != published {
		t.Fatalf("reviewer = %q, want its own image published as %q", got, published)
	}
	if report.Reference != candidate {
		t.Fatalf("reference = %q, want the candidate's", report.Reference)
	}
}
