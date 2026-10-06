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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These tests run the repository Makefile's push-worker-image target with
// DOCKER and TRIVY pointed at this test binary, which stands in for both
// (TestMain). No engine, scanner or registry is touched: the fakes only record
// what they were asked into a log under the test's temporary directory.

const (
	// fakeToolEnv switches the test binary into a fake docker or trivy.
	fakeToolEnv = "QUICBAAS_TEST_FAKE_TOOL"

	// fakeLogEnv is the file each fake invocation appends its argv to.
	fakeLogEnv = "QUICBAAS_TEST_FAKE_LOG"

	// fakeTrivyExitEnv is the exit code the fake trivy returns.
	fakeTrivyExitEnv = "QUICBAAS_TEST_FAKE_TRIVY_EXIT"

	// fakePushDigestEnv is the digest the fake push reports; empty reports
	// none.
	fakePushDigestEnv = "QUICBAAS_TEST_FAKE_PUSH_DIGEST"

	testPushRegistry = "registry.example:3001/owner/software-factory-worker"
	testPushDigest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeToolEnv) != "" {
		os.Exit(runFakeTool(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runFakeTool is the fake docker and trivy: args[0] names which.
func runFakeTool(args []string) int {
	logFile, err := os.OpenFile(os.Getenv(fakeLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer func() { _ = logFile.Close() }()
	if _, err := fmt.Fprintln(logFile, strings.Join(args, " ")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	switch {
	case args[0] == "trivy":
		code, err := strconv.Atoi(os.Getenv(fakeTrivyExitEnv))
		if err != nil {
			return 0
		}
		return code
	case args[0] == "docker" && len(args) > 2 && args[1] == "push":
		tag := args[2][strings.LastIndex(args[2], ":")+1:]
		fmt.Fprintln(os.Stdout, "The push refers to repository ["+args[2]+"]")
		fmt.Fprintln(os.Stdout, "5f70bf18a086: Pushed")
		if digest := os.Getenv(fakePushDigestEnv); digest != "" {
			fmt.Fprintln(os.Stdout, tag+": digest: "+digest+" size: 4321")
		}
		return 0
	case args[0] == "docker" && len(args) > 1 && args[1] == "build":
		// A bare `--build-arg NAME` (no "=value") is real docker's own
		// convention for reading the value from docker's process
		// environment. This fake stands in for that lookup, so a test can
		// prove the Makefile actually exported the variable down to this
		// process rather than merely naming it on the command line.
		for _, name := range []string{"DEVCACHE_GOPROXY", "DEVCACHE_GOPRIVATE", "DEVCACHE_NPM_REGISTRY"} {
			if v, ok := os.LookupEnv(name); ok {
				fmt.Fprintln(logFile, "ENV "+name+"="+v)
			}
		}
		return 0
	default:
		return 0
	}
}

// pushRun is one make invocation and what the fakes were asked.
type pushRun struct {
	output string
	err    error
	calls  []string
}

// environWithout is this process's environment less one variable, so a
// registry the developer exported cannot stand in for the Makefile's own
// (absent) default.
func environWithout(name string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, name+"=") {
			env = append(env, kv)
		}
	}
	return env
}

// environWithoutAny is this process's environment less every named variable,
// so a host that already exports a dev-cache variable for its own use (apt,
// docker's own mirror, ...) cannot stand in for what a test passes.
func environWithoutAny(names ...string) []string {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		if k := kv[:strings.Index(kv, "=")]; !drop[k] {
			env = append(env, kv)
		}
	}
	return env
}

// runPushTarget runs `make push-worker-image` against the fakes; an empty
// registry names none, so the Makefile's default is what the target sees.
func runPushTarget(t *testing.T, registry string, env ...string) pushRun {
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
	args := []string{"--no-print-directory", "-C", root, "push-worker-image", "DOCKER=" + self + " docker", "TRIVY=" + self + " trivy"}
	if registry != "" {
		args = append(args, "WORKER_IMAGE_REGISTRY="+registry)
	}
	cmd := exec.CommandContext(t.Context(), "make", args...)
	cmd.Env = append(environWithout("WORKER_IMAGE_REGISTRY"), append([]string{fakeToolEnv + "=1", fakeLogEnv + "=" + logPath}, env...)...)
	out, err := cmd.CombinedOutput()
	run := pushRun{output: string(out), err: err}
	if raw, readErr := os.ReadFile(logPath); readErr == nil {
		run.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return run
}

func TestPushWorkerImage_ScansThenPushesAndPrintsTheDigestPinnedReference(t *testing.T) {
	run := runPushTarget(t, testPushRegistry, fakePushDigestEnv+"="+testPushDigest)
	if run.err != nil {
		t.Fatalf("make push-worker-image: %v\n%s", run.err, run.output)
	}
	if len(run.calls) != 3 {
		t.Fatalf("calls = %q, want scan, tag, push", run.calls)
	}
	scan := strings.Fields(run.calls[0])
	wantScan := []string{"trivy", "image", "--severity", "CRITICAL,HIGH", "--ignore-unfixed", "--exit-code", "1"}
	if len(scan) != len(wantScan)+1 || strings.Join(scan[:len(wantScan)], " ") != strings.Join(wantScan, " ") {
		t.Fatalf("scan = %q, want exactly %q <image>", run.calls[0], strings.Join(wantScan, " "))
	}
	local := scan[len(scan)-1]
	tag := local[strings.LastIndex(local, ":")+1:]
	remote := testPushRegistry + ":" + tag
	if run.calls[1] != "docker tag "+local+" "+remote || run.calls[2] != "docker push "+remote {
		t.Fatalf("calls = %q, want the scanned image tagged into the registry and pushed", run.calls)
	}

	lines := strings.Split(strings.TrimSpace(run.output), "\n")
	if last := lines[len(lines)-1]; last != "WORKER_IMAGE_REF="+remote+"@"+testPushDigest {
		t.Fatalf("last line = %q, want the machine-readable, digest-pinned reference", last)
	}
}

func TestPushWorkerImage_AFailedScanPushesNothing(t *testing.T) {
	run := runPushTarget(t, testPushRegistry, fakeTrivyExitEnv+"=1", fakePushDigestEnv+"="+testPushDigest)
	if run.err == nil {
		t.Fatalf("a failed scan must fail the target:\n%s", run.output)
	}
	if len(run.calls) != 1 || !strings.HasPrefix(run.calls[0], "trivy image ") {
		t.Fatalf("calls = %q, want the scan and nothing after it", run.calls)
	}
	if strings.Contains(run.output, "WORKER_IMAGE_REF=") {
		t.Fatal("a failed scan must print no reference")
	}
}

func TestPushWorkerImage_APushWithNoDigestFails(t *testing.T) {
	run := runPushTarget(t, testPushRegistry)
	if run.err == nil || strings.Contains(run.output, "WORKER_IMAGE_REF=") {
		t.Fatalf("err = %v, want a failure and no reference:\n%s", run.err, run.output)
	}
	if !strings.Contains(run.output, "no manifest digest") {
		t.Fatalf("output = %s, want it to say the push reported no digest", run.output)
	}
}

// TestPushWorkerImage_RefusesWithoutARegistry is also the proof the Makefile
// carries no default registry: the deployment names it.
func TestPushWorkerImage_RefusesWithoutARegistry(t *testing.T) {
	run := runPushTarget(t, "")
	if run.err == nil || !strings.Contains(run.output, "WORKER_IMAGE_REGISTRY is required") {
		t.Fatalf("err = %v, want the missing registry named:\n%s", run.err, run.output)
	}
	if len(run.calls) != 0 {
		t.Fatalf("calls = %q, want nothing run", run.calls)
	}
}

func TestPushWorkerImage_RefusesWithoutAScanner(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "calls")
	cmd := exec.CommandContext(t.Context(), "make", "--no-print-directory", "-C", root, "push-worker-image",
		"WORKER_IMAGE_REGISTRY="+testPushRegistry, "DOCKER="+self+" docker",
		"TRIVY="+filepath.Join(t.TempDir(), "no-such-trivy"))
	cmd.Env = append(environWithout("WORKER_IMAGE_REGISTRY"), fakeToolEnv+"=1", fakeLogEnv+"="+logPath)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "trivy not found") {
		t.Fatalf("err = %v, want the missing scanner refused:\n%s", err, out)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatal("nothing may be tagged or pushed without a scan")
	}
}
