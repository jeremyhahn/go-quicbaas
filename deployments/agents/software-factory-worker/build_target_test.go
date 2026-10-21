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

package softwarefactoryworker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the repository Makefile's build-worker-image target with
// DOCKER pointed at this test binary (TestMain, push_target_test.go), which
// records what it was asked and, for a "docker build" call, the value of each
// of the three DEVCACHE_* variables in ITS OWN process environment -- the
// only way a bare `--build-arg NAME` (no "=value") reaches docker, and so the
// only way to prove the Makefile's `export` actually carries a value through
// rather than merely naming the flag (go-quicbaas#1159).

// runBuildTarget runs `make build-worker-image` against the fake DOCKER, with
// the host's own DEVCACHE_* stripped first so a value the environment already
// carries (this fleet sets some for apt/docker's own mirror) cannot stand in
// for what the test passes.
func runBuildTarget(t *testing.T, env ...string) pushRun {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "calls")
	cmd := exec.CommandContext(t.Context(), "make", "--no-print-directory", "-C", root, "build-worker-image",
		"DOCKER="+self+" docker")
	base := environWithoutAny("DEVCACHE_GOPROXY", "DEVCACHE_GOPRIVATE", "DEVCACHE_NPM_REGISTRY")
	cmd.Env = append(base, append([]string{fakeToolEnv + "=1", fakeLogEnv + "=" + logPath}, env...)...)
	out, err := cmd.CombinedOutput()
	run := pushRun{output: string(out), err: err}
	if raw, readErr := os.ReadFile(logPath); readErr == nil {
		run.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return run
}

// devcacheEnvValue finds the fake docker's "ENV NAME=value" line for name and
// returns its value; a variable make never exported at all (impossible today,
// since the Makefile unconditionally `export`s all three) and one exported
// empty are indistinguishable here on purpose -- GNU Make's bare `export VAR`
// always places VAR in the recipe's environment, empty when VAR has no
// value, and Docker's bare `--build-arg NAME` treats an empty and an absent
// environment variable identically. Both are "no cache".
func devcacheEnvValue(t *testing.T, log, name string) string {
	t.Helper()
	prefix := "ENV " + name + "="
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("docker build's environment carried no %s entry at all: %s", name, log)
	return ""
}

// TestBuildWorkerImage_PassesDevcacheArgsThroughByNameOnly proves the
// Makefile's `--build-arg DEVCACHE_GOPROXY` (no "=value") is backed by a real
// `export`: a value given on the make command line reaches the DOCKER
// process's own environment, which is where real docker reads a bare
// --build-arg from. GOPRIVATE is left unset to prove the converse: a
// variable nobody set reaches that environment empty, which is "no cache"
// for both docker's own resolution and the Dockerfile's `:+` guard.
func TestBuildWorkerImage_PassesDevcacheArgsThroughByNameOnly(t *testing.T) {
	run := runBuildTarget(t,
		"DEVCACHE_GOPROXY=http://cache.example:3000",
		"DEVCACHE_NPM_REGISTRY=http://cache.example:4873",
	)
	if run.err != nil {
		t.Fatalf("make build-worker-image: %v\n%s", run.err, run.output)
	}
	log := strings.Join(run.calls, "\n")
	for _, name := range []string{"DEVCACHE_GOPROXY", "DEVCACHE_GOPRIVATE", "DEVCACHE_NPM_REGISTRY"} {
		if !strings.Contains(log, "--build-arg "+name+" ") && !strings.HasSuffix(strings.TrimSpace(log), "--build-arg "+name) {
			t.Errorf("docker build was not passed --build-arg %s by name: %s", name, log)
		}
		if strings.Contains(log, "--build-arg "+name+"=") {
			t.Errorf("the Makefile must pass %s by name only, never a value: %s", name, log)
		}
	}
	if v := devcacheEnvValue(t, log, "DEVCACHE_GOPROXY"); v != "http://cache.example:3000" {
		t.Errorf("DEVCACHE_GOPROXY reached docker's environment as %q, want the value make was given", v)
	}
	if v := devcacheEnvValue(t, log, "DEVCACHE_NPM_REGISTRY"); v != "http://cache.example:4873" {
		t.Errorf("DEVCACHE_NPM_REGISTRY reached docker's environment as %q, want the value make was given", v)
	}
	if v := devcacheEnvValue(t, log, "DEVCACHE_GOPRIVATE"); v != "" {
		t.Errorf("DEVCACHE_GOPRIVATE reached docker's environment as %q, want empty: nobody set it", v)
	}
}

// TestBuildWorkerImage_BuildsWithNoDevcacheVariablesSet proves the target
// still runs to completion with all three unset -- the state of every build
// off this fleet -- and that all three reach docker's environment empty.
func TestBuildWorkerImage_BuildsWithNoDevcacheVariablesSet(t *testing.T) {
	run := runBuildTarget(t)
	if run.err != nil {
		t.Fatalf("make build-worker-image: %v\n%s", run.err, run.output)
	}
	log := strings.Join(run.calls, "\n")
	for _, name := range []string{"DEVCACHE_GOPROXY", "DEVCACHE_GOPRIVATE", "DEVCACHE_NPM_REGISTRY"} {
		if v := devcacheEnvValue(t, log, name); v != "" {
			t.Errorf("%s reached docker's environment as %q, want empty: nothing set it", name, v)
		}
	}
}

// TestBuildWorkerImage_PassesEveryToolchainPin proves `make
// build-worker-image` hands docker every toolchain pin Go's Build.BuildArgs
// carries, at toolchain.mk's value: a pin added to one and not the other
// fails the Dockerfile's required-argument check only at build time.
func TestBuildWorkerImage_PassesEveryToolchainPin(t *testing.T) {
	pins, err := ToolchainPins()
	if err != nil {
		t.Fatalf("ToolchainPins: %v", err)
	}
	run := runBuildTarget(t)
	if run.err != nil {
		t.Fatalf("make build-worker-image: %v\n%s", run.err, run.output)
	}
	log := strings.Join(run.calls, "\n")
	for _, pin := range pins.named() {
		if !strings.Contains(log, "--build-arg "+pin.buildArg+"="+pin.value) {
			t.Errorf("docker build was not passed --build-arg %s=%s", pin.buildArg, pin.value)
		}
	}
}
