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

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// TestSDLCModule_ForwardersAnswerUnavailableWithoutARuntime pins the host's
// contract with the process-global seam registries: the registered
// forwarders never act on a dead store — with no live runtime every moment
// refuses typed, and the source reports an outage the poller logs once.
func TestSDLCModule_ForwardersAnswerUnavailableWithoutARuntime(t *testing.T) {
	sdlcModule.live.Store(nil)
	ctx := context.Background()
	job := &orchestration.JobContext{TenantID: "t", ProjectID: "p", TaskID: "task"}

	var src sdlcSourceForwarder
	if src.Name() != sdlc.TaskSourceName {
		t.Fatal("source name")
	}
	_, err := src.Poll(ctx, &orchestration.Project{ID: "p"})
	var outage *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &outage) || !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("poll without a runtime: %v", err)
	}
	if _, err := sdlcReviewForwarder(ctx, job, orchestration.ReviewCriterion{Name: "r"}); !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("review without a runtime: %v", err)
	}
	// The runner's SHAPE is static — its name, its states and its resume
	// point are the module's, not its runtime's — so a stopped module still
	// answers them. Only driving a job needs the live runtime.
	var runner sdlcRunnerForwarder
	if runner.Name() != sdlc.JobRunnerName {
		t.Fatalf("runner name = %q", runner.Name())
	}
	if len(runner.States()) == 0 {
		t.Fatal("a stopped module must still declare the runner's states")
	}
	if got := runner.ResumeState(orchestration.JobStateCompleting); got != orchestration.JobStateCompleting {
		t.Fatalf("resume state = %q", got)
	}
	// An escalated job resumes IN escalating so the park reason reaches the
	// issue before the job fails; resumeJob launches no run for a terminal
	// answer, and initializing would re-run the pipeline (#626).
	if got := runner.ResumeState(orchestration.JobStateEscalating); got != orchestration.JobStateEscalating {
		t.Fatalf("escalating resume state = %q, want escalating", got)
	}
	cfg := &orchestration.JobConfig{TenantID: "t", ProjectID: "p", TaskID: "task"}
	if err := runner.Validate(ctx, cfg); !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("Validate without a runtime: %v", err)
	}
	if err := runner.Run(ctx, nil); !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("Run without a runtime: %v", err)
	}
	// A task that stops while the module is down must not be reported
	// against a dead store either. It is the same rule as the moments, on
	// the seam that carries the conclusion (go-quicbaas#788).
	var conclusions sdlcConclusionForwarder
	err = conclusions.TaskConcluded(ctx, orchestration.TaskConclusion{
		TenantID: "t", Reason: orchestration.TaskConcludedBudgetExhausted,
		Task: &orchestration.Task{ID: "task", TenantID: "t"},
	})
	if !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("TaskConcluded without a runtime: %v", err)
	}
}

// TestSDLCModule_ConclusionForwarderReachesTheLiveModule is the positive: with
// a runtime mounted the forwarder hands the conclusion to the module's hooks,
// which answer for a task the module never adopted by doing nothing.
func TestSDLCModule_ConclusionForwarderReachesTheLiveModule(t *testing.T) {
	hooks, err := sdlc.NewHooks(sdlc.HooksConfig{
		Projects: stubSDLCPorts{}, Bindings: stubSDLCPorts{}, Deliveries: stubSDLCPorts{},
		Tasks: stubSDLCPorts{}, Forges: stubSDLCPorts{},
	})
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}
	sdlcModule.live.Store(&sdlcRuntime{hooks: hooks})
	t.Cleanup(func() { sdlcModule.live.Store(nil) })

	foreign := &orchestration.Task{
		ID: "task", TenantID: "t", ProjectID: "p",
		Source: &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKind("cron"), Ref: "nightly"},
	}
	if err := (sdlcConclusionForwarder{}).TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: "t", Reason: orchestration.TaskConcludedBudgetExhausted, Task: foreign,
	}); err != nil {
		t.Fatalf("a task the module never adopted is not its business: %v", err)
	}
}

// stubSDLCPorts satisfies every narrow port the hooks require. Nothing on it
// is reached by a conclusion about a task with no forge provenance, which is
// the point of the test above: the ownership test comes before any I/O.
type stubSDLCPorts struct{}

func (stubSDLCPorts) Get(context.Context, string, string) (*orchestration.Project, error) {
	return nil, ErrSDLCUnavailable
}

func (stubSDLCPorts) GetBinding(context.Context, string, string) (*sdlc.ProjectBinding, error) {
	return nil, ErrSDLCUnavailable
}

func (stubSDLCPorts) ListBindings(context.Context, string) ([]*sdlc.ProjectBinding, error) {
	return nil, ErrSDLCUnavailable
}

func (stubSDLCPorts) GetDelivery(context.Context, string, string) (*sdlc.Delivery, error) {
	return nil, ErrSDLCUnavailable
}

func (stubSDLCPorts) PutDelivery(context.Context, string, *sdlc.Delivery) error {
	return ErrSDLCUnavailable
}

func (stubSDLCPorts) UpdateDelivery(context.Context, string, *sdlc.Delivery) error {
	return ErrSDLCUnavailable
}

func (stubSDLCPorts) GetTask(context.Context, string, string) (*orchestration.Task, error) {
	return nil, ErrSDLCUnavailable
}

func (stubSDLCPorts) SubmitTaskArtifacts(context.Context, string, string, []orchestration.Artifact) error {
	return ErrSDLCUnavailable
}

func (stubSDLCPorts) ForgeFor(context.Context, string, sdlc.ForgeConfig) (sdlc.ForgeBackend, error) {
	return nil, ErrSDLCUnavailable
}

// TestSDLCModule_RegistersOnce proves a second registration (a second
// server in one process, as the e2e rig does) is a no-op rather than a
// duplicate-name refusal.
func TestSDLCModule_RegistersOnce(t *testing.T) {
	if err := registerSDLCModule(); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := registerSDLCModule(); err != nil {
		t.Fatalf("second registration: %v", err)
	}
	if _, ok := orchestration.LookupTaskSource(sdlc.TaskSourceName); !ok {
		t.Fatal("task source not registered")
	}
	if _, ok := orchestration.LookupJobRunner(sdlc.JobRunnerName); !ok {
		t.Fatal("job runner not registered")
	}
	// The runners endpoint and the UI's stepper read the REGISTRY, so what
	// the module registered has to carry its states and their labels — a
	// runner enumerated with an unlabelled state renders a job's progress as
	// a row of blanks.
	var listed orchestration.JobRunner
	for _, runner := range orchestration.JobRunners() {
		if runner.Name() == sdlc.JobRunnerName {
			listed = runner
		}
	}
	if listed == nil {
		t.Fatalf("the sdlc runner is not enumerated: %v", runnerNames())
	}
	states := listed.States()
	if len(states) != len(sdlc.RunnerStates()) {
		t.Fatalf("enumerated %d states, the module declares %d", len(states), len(sdlc.RunnerStates()))
	}
	for _, spec := range states {
		if spec.Label == "" {
			t.Errorf("state %q is enumerated with no label for a person", spec.State)
		}
		switch spec.State {
		case orchestration.JobStateDecomposing, orchestration.JobStateScheduling, orchestration.JobStateAssigning:
			t.Errorf("the sdlc runner declares %q; an issue is not decomposed", spec.State)
		}
	}
	if err := orchestration.ValidateValidatorKind(sdlc.ValidatorKindForgeReview); err != nil {
		t.Fatalf("validator not registered: %v", err)
	}
	// The conclusion observer is the fifth seam: registered once, under the
	// module's own name, and append-only like the rest.
	if err := orchestration.RegisterTaskConclusionObserver(sdlc.JobRunnerName, sdlcConclusionForwarder{}); !errors.Is(err, orchestration.ErrTaskConclusionAlreadyRegistered) {
		t.Fatalf("the conclusion observer is not registered: %v", err)
	}
}

func TestSDLCModule_ServerAccessorsWithoutTheModule(t *testing.T) {
	s := &Server{}
	if s.SDLCService() != nil || s.SDLCWebhookHandler() != nil {
		t.Fatal("a server without the module must answer nil")
	}
	s.stopSDLC() // a no-op when nothing is running
	err := s.NudgeProject(context.Background(), "t", "p")
	var cfgErr *orchestration.TaskSourcePollerConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("nudge without a poller: %v", err)
	}
	initErr := &SDLCInitError{Component: "store", Cause: ErrSDLCUnavailable}
	if initErr.Error() != "sdlc: store: "+ErrSDLCUnavailable.Error() || !errors.Is(initErr, ErrSDLCUnavailable) {
		t.Fatal("SDLCInitError shape")
	}
}

func TestSDLCWebhookPathParams(t *testing.T) {
	r := chi.NewRouter()
	var got string
	r.Post("/api/v1/sdlc/webhook/{projectID}", func(w http.ResponseWriter, r *http.Request) {
		got = sdlcWebhookPathParams(r)
		w.WriteHeader(http.StatusAccepted)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sdlc/webhook/proj-1", nil))
	if rec.Code != http.StatusAccepted || got != "proj-1" {
		t.Fatalf("params = %q (%d)", got, rec.Code)
	}
}

// TestSDLCWebhookFailure_TellsAnAcceptedTriageFailureFromARefusal proves the
// operator's log does not call a delivery refused when it was accepted and
// only its triage wake failed.
func TestSDLCWebhookFailure_TellsAnAcceptedTriageFailureFromARefusal(t *testing.T) {
	wake := &sdlc.TriageWakeError{TenantID: "t", ProjectID: "p", Cause: errors.New("scheduler down")}
	if got := sdlcWebhookFailure(wake); got != "delivery accepted but triage was not woken" {
		t.Fatalf("triage wake failure logged as %q", got)
	}
	if got := sdlcWebhookFailure(&sdlc.WebhookUnverifiedError{Type: sdlc.ForgeGitea, Reason: "hmac"}); got != "delivery refused" {
		t.Fatalf("refusal logged as %q", got)
	}
}

// runnerNames is what the registry holds, for a refusal that says what it
// found instead of only what it wanted.
func runnerNames() []string {
	registered := orchestration.JobRunners()
	names := make([]string, 0, len(registered))
	for _, runner := range registered {
		names = append(names, runner.Name())
	}
	return names
}

// TestSDLCSocketsRoot_TheDefaultFitsUnderAnyRealisticProjectsRoot is the
// composition go-quicbaas#628 reported broken.
//
// The signing agent's socket used to be bound under
// <projects_root>/<tenant_id>/<project_id>/workspace, which on the burn-in
// node came to 148 bytes against a 107-byte kernel field — every realistic
// projects root failed, and it failed at initializing on the first delivery.
// The host now derives a short, dedicated root instead, and this pins that
// the derivation the host actually calls yields a socket that fits no matter
// how deep the projects root is.
func TestSDLCSocketsRoot_TheDefaultFitsUnderAnyRealisticProjectsRoot(t *testing.T) {
	dataDir := "/var/lib/quicbaas/go-truststrap-run2/data"
	deepProjects := "/" + strings.Repeat("p", 199)

	for name, runtimeDir := range map[string]string{
		"with a runtime directory": "/run/user/1000",
		"without one":              "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
			orch := config.OrchestrationConfig{ProjectsRoot: deepProjects}
			root, err := orch.EffectiveSocketsRoot(dataDir)
			if err != nil {
				t.Fatalf("the host could not derive a sockets root: %v", err)
			}
			if root == "" {
				t.Fatal("the host derived no sockets root")
			}
			if got := orchestration.LongestSigningSocketPath(root); got > orchestration.MaxSigningSocketPath {
				t.Fatalf("sockets root %q yields %d-byte socket paths, over the %d-byte limit",
					root, got, orchestration.MaxSigningSocketPath)
			}
		})
	}

	// And the placement this replaced: the same deployment's managed
	// workspace could not hold one, which is why the root had to move rather
	// than be shortened.
	workspace := filepath.Join(deepProjects, "automatethethings", "0f8c1f26-2a1e-4a7e-9f0a-3b7c5d9e1a24", "workspace")
	if orchestration.LongestSigningSocketPath(workspace) <= orchestration.MaxSigningSocketPath {
		t.Fatalf("the workspace placement fits after all (%d bytes); this test no longer proves anything",
			orchestration.LongestSigningSocketPath(workspace))
	}
}

// TestServer_SocketsRootIsPerDaemon is go-quicbaas#687 at the composition
// point: the accessor the sdlc runner, the orchestrator's mount policy and
// the embedded agent's mount policy all read.
//
// $XDG_RUNTIME_DIR is per-USER, so before the instance segment two quicbaasd
// processes of one user bound their signing sockets under one root and each
// one's startup sweep (#633) could reach the other's live sessions.
func TestServer_SocketsRootIsPerDaemon(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	daemon := func(dataDir string) *Server {
		return &Server{config: &config.Config{
			Storage: config.StorageConfig{DataDir: dataDir},
		}}
	}
	first, err := daemon("/srv/quicbaas/alpha/data").socketsRoot()
	if err != nil {
		t.Fatalf("first daemon: %v", err)
	}
	second, err := daemon("/srv/quicbaas/beta/data").socketsRoot()
	if err != nil {
		t.Fatalf("second daemon: %v", err)
	}
	if first == "" || second == "" {
		t.Fatalf("a daemon derived no sockets root: %q, %q", first, second)
	}
	if first == second {
		t.Fatalf("two daemons of one user share the sockets root %q", first)
	}
	// The three consumers must be handed the SAME root, or the mount policy
	// would deny the socket the runner binds.
	again, err := daemon("/srv/quicbaas/alpha/data").socketsRoot()
	if err != nil {
		t.Fatalf("re-derivation: %v", err)
	}
	if again != first {
		t.Fatalf("the accessor derived %q and then %q for one daemon", first, again)
	}
	// And it must fit: the runner refuses a root no socket could bind under.
	if got := orchestration.LongestSigningSocketPath(first); got > orchestration.MaxSigningSocketPath {
		t.Fatalf("the per-daemon root %q yields %d-byte socket paths, over %d",
			first, got, orchestration.MaxSigningSocketPath)
	}

	// An operator's explicit root is still exactly what they wrote: it names
	// one deployment already, and a segment appended to it would silently
	// move the directory they provisioned.
	explicit := &Server{config: &config.Config{
		Storage:       config.StorageConfig{DataDir: "/srv/quicbaas/alpha/data"},
		Orchestration: config.OrchestrationConfig{SocketsRoot: "/run/quicbaas-sockets"},
	}}
	if root, rootErr := explicit.socketsRoot(); rootErr != nil || root != "/run/quicbaas-sockets" {
		t.Fatalf("explicit sockets root = %q (%v)", root, rootErr)
	}
}

// TestServer_SocketsRootWithoutAConfiguration is the bad-input half: a server
// with no configuration must answer emptily rather than dereference nil. The
// three call sites run during startup, where a half-built server is reachable.
func TestServer_SocketsRootWithoutAConfiguration(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	root, err := (&Server{}).socketsRoot()
	if err != nil {
		t.Fatalf("a server with no configuration: %v", err)
	}
	if root != "" {
		t.Fatalf("a server with no configuration derived the sockets root %q", root)
	}
	// A configured server that stores nothing on disk is the same answer: it
	// has no identity to derive a per-daemon root from, and sharing the
	// user's would be the defect #687 reported.
	root, err = (&Server{config: &config.Config{}}).socketsRoot()
	if err != nil {
		t.Fatalf("a server with no data directory: %v", err)
	}
	if root != "" {
		t.Fatalf("a server with no data directory derived the sockets root %q", root)
	}
}

// TestSDLCModule_DerivedIdleThresholdsAreUnchanged is the #621 guard at the
// only place both runners exist.
//
// The stall detector's bounds and its session-backed set used to be tables
// kept beside the detector; they are now derived from the registered runners'
// JobStateSpecs. sdlc registers here, in pkg/server, so pkg/orchestration's
// own tests see the workflow runner alone and CANNOT observe what the
// derivation does to an sdlc job. This test does: with both runners
// registered, every state sdlc drives must keep the bound and the
// session-backing it had before the derivation.
//
// THE EXPECTATIONS ARE THE PRE-#621 TABLES VERBATIM, not a re-derivation,
// except where sdlc has since sized a bound to its own work on purpose:
// validating and reviewing were widened on 2026-09-21 (a verification runs
// `make test` three times over; reviewing waits on a CI pipeline and then a
// reviewer, and the thirty-minute bound ended a resumed job whose checks were
// still being judged). Those two rows carry the deliberate values.
func TestSDLCModule_DerivedIdleThresholdsAreUnchanged(t *testing.T) {
	if err := registerSDLCModule(); err != nil {
		t.Fatalf("registerSDLCModule: %v", err)
	}

	// job_stall.go's DefaultJobIdleThresholds and sessionBackedJobStates as
	// they read before the derivation, restricted to the states sdlc drives.
	want := map[orchestration.JobState]struct {
		threshold     time.Duration
		sessionBacked bool
	}{
		orchestration.JobStateInitializing: {10 * time.Minute, false},
		orchestration.JobStateExecuting:    {3 * time.Hour, true},
		orchestration.JobStateValidating:   {2 * time.Hour, false},
		orchestration.JobStateReviewing:    {4 * time.Hour, true},
		orchestration.JobStateRedelegating: {10 * time.Minute, false},
		orchestration.JobStateCompleting:   {10 * time.Minute, false},
		orchestration.JobStateCleanup:      {10 * time.Minute, false},
		orchestration.JobStateEscalating:   {10 * time.Minute, false},
	}

	derived := make(map[orchestration.JobState]orchestration.JobStateSpec)
	for _, spec := range orchestration.KnownJobStates() {
		derived[spec.State] = spec
	}
	thresholds := orchestration.DefaultJobIdleThresholds()

	for _, spec := range sdlc.RunnerStates() {
		expected, known := want[spec.State]
		if !known {
			t.Fatalf("sdlc declares %q, which had no pre-#621 bound; the table above must be updated deliberately", spec.State)
		}
		if got := thresholds[spec.State]; got != expected.threshold {
			t.Errorf("%s: derived bound %s, was %s before #621", spec.State, got, expected.threshold)
		}
		if got := derived[spec.State].SessionBacked; got != expected.sessionBacked {
			t.Errorf("%s: derived SessionBacked %v, was %v before #621", spec.State, got, expected.sessionBacked)
		}
	}

	// Resume states are the runner's own and must be untouched by any of
	// this; the derivation reads States(), never ResumeState.
	runner := &sdlc.Runner{}
	if got := runner.ResumeState(orchestration.JobStateExecuting); got != orchestration.JobStateInitializing {
		t.Errorf("sdlc resume from executing = %s, want initializing", got)
	}
	if got := runner.ResumeState(orchestration.JobStateReviewing); got != orchestration.JobStateReviewing {
		t.Errorf("sdlc resume from reviewing = %s, want reviewing", got)
	}
}
