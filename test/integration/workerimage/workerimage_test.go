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

//go:build integration

package workerimage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// =============================================================================
// The worker image refresh against a REAL engine and a REAL registry
// =============================================================================
//
// The unit tier drives the refresh through doubles of the engine and the
// registry. What it cannot prove is that the argv ExecContainerRuntime builds
// is one docker accepts — a Dockerfile on stdin with an empty context, an
// --iidfile, a push that prints its digest, a pull by that digest — or that the
// gate's refusal holds when the probes run in a real container.
//
// THE ONE THING NOT BUILT HERE IS THE REAL WORKER IMAGE. Its build installs the
// agent CLIs from npm and serena from GitHub and takes tens of minutes; that
// build and its probes are `make test-worker-image`. These scenarios use tiny
// images built from alpine, and the npm registry — a third-party API — is an
// httptest double, as the integration contract requires.

func engine(t *testing.T) *orchestration.ExecContainerRuntime {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)
	rt, err := orchestration.NewExecContainerRuntimeWithCommand(orchestration.RuntimeDocker, logger.NewAdapter(log))
	require.NoError(t, err, "docker is required for the worker image refresh scenarios")
	return rt
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Only stdout is parsed: docker writes pull progress for an image that is
	// not cached locally to stderr, which is kept for the error message.
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "docker %s: %s%s", strings.Join(args, " "), stdout.String(), stderr.String())
	return strings.TrimSpace(stdout.String())
}

func removeImages(t *testing.T, refs ...string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		args := append([]string{"rmi", "-f"}, refs...)
		if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
			t.Logf("cleanup docker rmi: %v: %s", err, out)
		}
	})
}

// startRegistry runs a throwaway registry:2 on a kernel-assigned port and
// returns its host:port. docker treats a localhost registry as insecure by
// default, so no engine configuration — and no TLS weakening — is needed.
func startRegistry(t *testing.T) string {
	t.Helper()
	id := docker(t, "run", "-d", "--rm", "-p", "127.0.0.1::5000", "registry:2")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", id).CombinedOutput(); err != nil {
			t.Logf("cleanup registry: %v: %s", err, out)
		}
	})
	port := docker(t, "port", id, "5000/tcp")
	return "localhost:" + port[strings.LastIndex(port, ":")+1:]
}

const tinyDockerfile = "FROM alpine:3.21\nCMD [\"true\"]\n"

func TestE2E_BuildInspectTagPushAndPullByDigest(t *testing.T) {
	rt := engine(t)
	ctx := context.Background()
	local := "quicbaas-worker-image-it:" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	labels := map[string]string{softwarefactoryworker.LabelClaudeCodeVersion: "2.1.280"}

	id, err := rt.BuildImage(ctx, &orchestration.ImageBuildSpec{Dockerfile: []byte(tinyDockerfile), Tags: []string{local}, Labels: labels})
	require.NoError(t, err)
	removeImages(t, local)
	require.True(t, strings.HasPrefix(id, "sha256:"), "the build must report its image ID, got %q", id)

	info, err := rt.InspectImage(ctx, local)
	require.NoError(t, err)
	assert.Equal(t, id, info.ID)
	assert.Equal(t, "2.1.280", info.Labels[softwarefactoryworker.LabelClaudeCodeVersion])

	registry := startRegistry(t)
	remote := registry + "/it/software-factory-worker:1"
	require.NoError(t, rt.TagImage(ctx, id, remote))
	removeImages(t, remote)
	digest, err := rt.PushImage(ctx, remote, nil)
	// CommandError.Error() withholds docker's stderr, so a failed push logs
	// it here, in the test's own output, before the assertion stops the test.
	var pushErr *orchestration.CommandError
	if errors.As(err, &pushErr) {
		t.Logf("docker %s output: %s", pushErr.Op, pushErr.Output)
	}
	require.NoError(t, err)
	require.NoError(t, orchestration.ValidateImageDigest(digest))

	// A node that never held the image fetches it by digest — the reference
	// a promotion binds projects to — and gets exactly the build that was
	// pushed.
	pinned := registry + "/it/software-factory-worker@" + digest
	docker(t, "rmi", "-f", remote)
	_, err = rt.InspectImage(ctx, pinned)
	require.ErrorIs(t, err, orchestration.ErrImageNotFound)
	require.NoError(t, rt.Pull(ctx, pinned))
	removeImages(t, pinned)
	pulled, err := rt.InspectImage(ctx, pinned)
	require.NoError(t, err)
	assert.Equal(t, id, pulled.ID, "the image pulled by digest must be the image that was built")
}

func TestE2E_RunImageReportsTheExitAndWritesAsTheSpawnUID(t *testing.T) {
	rt := engine(t)
	ctx := context.Background()
	local := "quicbaas-worker-image-it:run"
	_, err := rt.BuildImage(ctx, &orchestration.ImageBuildSpec{Dockerfile: []byte(tinyDockerfile), Tags: []string{local}})
	require.NoError(t, err)
	removeImages(t, local)

	res, err := rt.RunImage(ctx, &orchestration.ImageRunSpec{
		Image: local, NetworkNone: true, Env: map[string]string{"PROBE": "value"},
		Command: []string{"sh", "-c", `echo "$PROBE"; exit 3`},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, res.ExitCode, "a failing command is a result, not an error")
	assert.Equal(t, "value", strings.TrimSpace(res.Stdout), "the environment must reach the command through the env file")
}

// npmDouble answers GET /<package>/latest.
func npmDouble(t *testing.T, claude, codex, gemini string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := map[string]string{
			"/@anthropic-ai%2Fclaude-code/latest": claude,
			"/@openai%2Fcodex/latest":             codex,
			"/@google%2Fgemini-cli/latest":        gemini,
		}[r.URL.EscapedPath()]
		if version == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type oneTenant struct{}

func (oneTenant) ListTenantIDs(context.Context) ([]string, error) { return []string{"acme"}, nil }

// bindingsDouble is the SDLC service's binding surface in memory.
type bindingsDouble struct {
	binding *sdlc.ProjectBinding
}

func (b *bindingsDouble) ListBindings(context.Context, string) ([]*sdlc.ProjectBinding, error) {
	return []*sdlc.ProjectBinding{b.binding}, nil
}

// agentsDouble is the tenant agent catalog in memory: the one Agent the
// binding's worker names, which carries the image a promotion repoints.
type agentsDouble struct {
	mu       sync.Mutex
	agent    orchestration.Agent
	replaced int
}

func (a *agentsDouble) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return orchestration.AgentCatalogue{
		Agents: map[string]orchestration.Agent{a.agent.ID: a.agent},
		Pools:  map[string]orchestration.AgentPool{},
	}, nil
}

func (a *agentsDouble) ReplaceAgentImage(_ context.Context, _, agentID string, stale []string, image string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replaced++
	if agentID != a.agent.ID || !slices.Contains(stale, a.agent.Image) {
		return false, nil
	}
	a.agent.Image = image
	return true, nil
}

func (a *agentsDouble) image() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agent.Image
}

type smokeNever struct{ asked int }

func (s *smokeNever) Smoke(context.Context, workerimage.SmokeRequest) (string, error) {
	s.asked++
	return "", nil
}

type auditLog struct {
	mu     sync.Mutex
	events []workerimage.AuditEvent
}

func (a *auditLog) RecordWorkerImageEvent(_ context.Context, e workerimage.AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
}

type outcomes struct{ seen []workerimage.Outcome }

func (o *outcomes) ObserveRefresh(outcome workerimage.Outcome, _ time.Time) {
	o.seen = append(o.seen, outcome)
}

// TestE2E_ACandidateThatFailsItsProbesIsNeverPromoted runs a real pass: the
// bound image's labels are read from the real engine, the candidate the pass
// would build is already on it (so no npm install is needed), and the probes
// run for real in docker — where an alpine image lacks every tool the worker
// needs and fails. The binding must be left exactly as it was, nothing may be
// smoked, and the failure must be recorded.
func TestE2E_ACandidateThatFailsItsProbesIsNeverPromoted(t *testing.T) {
	rt := engine(t)
	ctx := context.Background()
	pins, err := softwarefactoryworker.ToolchainPins()
	require.NoError(t, err)
	const repository = "quicbaas-worker-image-it-repo"
	stale := repository + ":old"
	build := softwarefactoryworker.Build{
		Base: "0.0.1-it", Agents: softwarefactoryworker.Agents{ClaudeCode: "9.9.9", Codex: "9.9.9", Gemini: "9.9.9"},
		Pins: pins, UID: os.Getuid(), GID: os.Getgid(),
	}
	candidate := repository + ":" + build.Tag()

	oldBuild := build
	oldBuild.Agents = softwarefactoryworker.Agents{ClaudeCode: "1.0.0", Codex: "1.0.0", Gemini: "1.0.0"}
	_, err = rt.BuildImage(ctx, &orchestration.ImageBuildSpec{Dockerfile: []byte(tinyDockerfile), Tags: []string{stale}, Labels: oldBuild.Labels()})
	require.NoError(t, err)
	// The worker Dockerfile declares its languages in a LABEL, not through
	// Build.Labels, and the refresher refuses a candidate without them before
	// any probe runs. The candidate carries the label so the probes are what
	// fail it.
	candidateLabels := build.Labels()
	candidateLabels[softwarefactoryworker.LabelLanguages] = "go"
	_, err = rt.BuildImage(ctx, &orchestration.ImageBuildSpec{Dockerfile: []byte(tinyDockerfile), Tags: []string{candidate}, Labels: candidateLabels})
	require.NoError(t, err)
	removeImages(t, stale, candidate)

	versions, err := workerimage.NewNPMRegistry(npmDouble(t, "9.9.9", "9.9.9", "9.9.9").URL, nil)
	require.NoError(t, err)
	agents := &agentsDouble{agent: orchestration.Agent{
		ID: "claude-coder", Name: "claude-coder", Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding},
		Backend: "claude", Model: "claude-opus-5-5", Image: stale,
	}}
	bindings := &bindingsDouble{binding: &sdlc.ProjectBinding{ProjectID: "p1", Worker: orchestration.AgentRefTo(agents.agent.ID)}}
	smoker := &smokeNever{}
	audit := &auditLog{}
	seen := &outcomes{}
	refresher, err := workerimage.NewRefresher(workerimage.Config{
		Repository: repository, SmokeModel: "claude-opus-5-5", SmokeHost: "never-reached", Base: build.Base, Pins: pins, UID: build.UID, GID: build.GID,
	}, workerimage.Deps{
		Engine: rt, Versions: versions, Tenants: oneTenant{}, Bindings: bindings, Agents: agents, Smoker: smoker,
		Auditor: audit, Observer: seen, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Fleet: noRemoteNodes{},
	})
	require.NoError(t, err)

	passCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	report := refresher.Refresh(passCtx, workerimage.TriggerManual)

	require.Equal(t, workerimage.OutcomeFailed, report.Outcome, "report: %+v", report)
	assert.Equal(t, []string{stale}, report.Stale, "the real engine's labels must mark the bound image stale")
	assert.Equal(t, candidate, report.Candidate)
	require.NotEmpty(t, report.Probes)
	assert.False(t, report.Probes[0].Passed, "alpine carries none of the worker's tools")
	assert.Zero(t, smoker.asked, "an image that failed its probes must not be smoked")
	assert.Zero(t, agents.replaced, "a failed pass must not repoint any Agent")
	assert.Equal(t, stale, agents.image())
	assert.Equal(t, []workerimage.Outcome{workerimage.OutcomeFailed}, seen.seen)
	require.Len(t, audit.events, 1)
	assert.False(t, audit.events[0].Success)
}

// noRemoteNodes is a single-host fleet: local mode may promote.
type noRemoteNodes struct{}

func (noRemoteNodes) RemoteFleetNodes(context.Context, string) ([]string, error) { return nil, nil }
