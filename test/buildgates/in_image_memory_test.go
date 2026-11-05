package buildgates

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var inToolchainImageScript = filepath.Join(repoRoot, "scripts", "in-toolchain-image.sh")

// fakeDocker logs each invocation as one line of tab-terminated arguments and
// drains stdin for `docker cp`, so the script's tar pipe never breaks. The Go
// raw string hands the shell one backslash before t, the escape printf expands
// into a real tab, which is the separator runInToolchainImage splits
// each line on.
const fakeDocker = `#!/bin/sh
printf '%s\t' "$@" >> "$FAKE_DOCKER_LOG"; echo >> "$FAKE_DOCKER_LOG"
if [ "$1" = cp ]; then cat > /dev/null; fi
`

// inImageRun is the outcome of one run of scripts/in-toolchain-image.sh
// against the fake docker.
type inImageRun struct {
	exitCode    int
	stderr      string
	invocations [][]string
}

// runInToolchainImage runs the script with the fake docker first on PATH.
// env holds the case's IN_IMAGE_MEMORY and CI_CGROUP_PARENT; cgroupMemoryMax,
// when non-nil, is written as the cgroup memory.max file, and when nil the
// file is missing.
func runInToolchainImage(t *testing.T, env map[string]string, cgroupMemoryMax *string) inImageRun {
	t.Helper()
	binDir, stateDir, workDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	logPath := filepath.Join(stateDir, "docker.log")
	memoryMax := filepath.Join(stateDir, "memory.max")
	if cgroupMemoryMax != nil {
		if err := os.WriteFile(memoryMax, []byte(*cgroupMemoryMax), 0o644); err != nil {
			t.Fatalf("write cgroup memory.max: %v", err)
		}
	}
	script, err := filepath.Abs(inToolchainImageScript)
	if err != nil {
		t.Fatalf("resolve %s: %v", inToolchainImageScript, err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", script, "toolchain:test", "true")
	cmd.Dir = workDir
	cmd.Env = []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + stateDir,
		"FAKE_DOCKER_LOG=" + logPath,
		"IN_IMAGE_CGROUP_MEMORY_MAX=" + memoryMax,
		"IN_IMAGE_NETWORK=none",
		"IN_IMAGE_UID=10001",
		"CI_STACK_PREFIX=buildgates-ci-",
		"CI_CACHE=on",
		"CI_CACHE_CLASS=pr",
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	run := inImageRun{}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v", script, err)
		}
		run.exitCode = exitErr.ExitCode()
	}
	run.stderr = stderr.String()

	log, err := os.ReadFile(logPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read docker log: %v", err)
	}
	for line := range strings.Lines(string(log)) {
		args := strings.Split(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\t"), "\t")
		run.invocations = append(run.invocations, args)
	}
	return run
}

// containerArgs returns the arguments of the `docker create` that makes the
// sidecar, the invocation that carries its resource limits.
func (r inImageRun) containerArgs(t *testing.T) []string {
	t.Helper()
	if r.exitCode != 0 {
		t.Fatalf("script exited %d: %s", r.exitCode, r.stderr)
	}
	for _, args := range r.invocations {
		if len(args) > 0 && args[0] == "create" {
			return args
		}
	}
	t.Fatalf("no docker create among %q", r.invocations)
	return nil
}

// flagValue returns the argument that follows flag, and whether flag is present.
func flagValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func assertMemoryAndSwap(t *testing.T, args []string, want string) {
	t.Helper()
	for _, flag := range []string{"--memory", "--memory-swap"} {
		if got, ok := flagValue(args, flag); !ok || got != want {
			t.Errorf("%s = %q (present %v), want %q", flag, got, ok, want)
		}
	}
}

func TestInImageMemoryRejectsAnInvalidSizeBeforeDocker(t *testing.T) {
	for _, value := range []string{"0", "16x", "-1", "1.5g", "16G", "abc"} {
		t.Run(value, func(t *testing.T) {
			run := runInToolchainImage(t, map[string]string{"IN_IMAGE_MEMORY": value}, nil)
			if run.exitCode != 2 {
				t.Errorf("exit code = %d, want 2", run.exitCode)
			}
			if !strings.Contains(run.stderr, "IN_IMAGE_MEMORY") {
				t.Errorf("stderr %q does not name IN_IMAGE_MEMORY", run.stderr)
			}
			if len(run.invocations) != 0 {
				t.Errorf("docker was invoked: %q", run.invocations)
			}
		})
	}
}

func TestInImageMemoryOverrideSetsEqualMemoryAndSwap(t *testing.T) {
	for _, value := range []string{"16g", "512m", "1073741824"} {
		t.Run(value, func(t *testing.T) {
			run := runInToolchainImage(t, map[string]string{"IN_IMAGE_MEMORY": value}, nil)
			assertMemoryAndSwap(t, run.containerArgs(t), value)
		})
	}
}

func TestInImageMemoryDefaultsToTheCgroupLimit(t *testing.T) {
	limit := "4294967296\n"
	run := runInToolchainImage(t, nil, &limit)
	assertMemoryAndSwap(t, run.containerArgs(t), "4294967296")
}

func TestInImageMemoryFallsBackTo8gWhenUnbounded(t *testing.T) {
	unbounded := "max\n"
	cases := map[string]*string{
		"max":     &unbounded,
		"missing": nil,
	}
	for name, cgroupMemoryMax := range cases {
		t.Run(name, func(t *testing.T) {
			run := runInToolchainImage(t, nil, cgroupMemoryMax)
			assertMemoryAndSwap(t, run.containerArgs(t), "8g")
		})
	}
}

func TestInImageMemoryCgroupParentOnlyWhenSet(t *testing.T) {
	cases := map[string]struct {
		env        map[string]string
		wantParent string
	}{
		"set":   {env: map[string]string{"CI_CGROUP_PARENT": "runner.slice"}, wantParent: "runner.slice"},
		"unset": {env: nil},
		"empty": {env: map[string]string{"CI_CGROUP_PARENT": ""}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := runInToolchainImage(t, tc.env, nil).containerArgs(t)
			got, ok := flagValue(args, "--cgroup-parent")
			if tc.wantParent == "" {
				if slices.Contains(args, "--cgroup-parent") {
					t.Errorf("--cgroup-parent %q passed, want none", got)
				}
				return
			}
			if !ok || got != tc.wantParent {
				t.Errorf("--cgroup-parent = %q (present %v), want %q", got, ok, tc.wantParent)
			}
		})
	}
}
