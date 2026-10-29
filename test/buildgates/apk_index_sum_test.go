package buildgates

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ci-template v1.29 (ruling 220(5)): the Alpine package index checksum,
// APK_INDEX_SUM, joins the integration image's content tag, so a new Alpine
// package release changes the tag and the next run rebuilds the image.

const (
	readApkIndexStep  = "- name: Read the Alpine package index"
	nameITImageStep   = "- name: Name the integration image"
	apkIndexSumScript = "scripts/apk-index-sum.sh"
)

// itImageTag prints `make -s it-image` with APK_INDEX_SUM set to sum.
func itImageTag(t *testing.T, sum string) string {
	t.Helper()
	cmd := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot, "it-image")
	cmd.Env = append(os.Environ(), "APK_INDEX_SUM="+sum)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -s it-image with APK_INDEX_SUM=%q: %v", sum, err)
	}
	return strings.TrimSpace(string(out))
}

// TestEveryIntegrationImageJobReadsTheApkIndexFirst: each job that names the
// integration image runs apk-index-sum.sh itself, before it names the image,
// so the name hashes the index and no job waits on the image job for it.
func TestEveryIntegrationImageJobReadsTheApkIndexFirst(t *testing.T) {
	order, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	named := 0
	for _, id := range order {
		body := jobs[id]
		nameAt := strings.Index(body, nameITImageStep)
		if nameAt < 0 {
			continue
		}
		named++
		readAt := strings.Index(body, readApkIndexStep)
		if readAt < 0 || readAt > nameAt {
			t.Errorf("job %s names the integration image without reading the "+
				"Alpine package index first (ci-template v1.29)", id)
			continue
		}
		if !strings.Contains(body[readAt:nameAt], apkIndexSumScript) {
			t.Errorf("job %s's %q step does not run %s", id, readApkIndexStep, apkIndexSumScript)
		}
	}
	if named < 4 {
		t.Fatalf("found %d jobs naming the integration image, want the image, "+
			"lint-gates, integration and soak jobs; job discovery has broken", named)
	}
}

// TestITImageTagHashesTheApkIndexSum: a new Alpine index changes the tag.
func TestITImageTagHashesTheApkIndexSum(t *testing.T) {
	first := itImageTag(t, "0000000000000000000000000000000000000000000000000000000000000001")
	second := itImageTag(t, "0000000000000000000000000000000000000000000000000000000000000002")
	if first == second {
		t.Errorf("make -s it-image prints %s for two APK_INDEX_SUM values; the "+
			"tag does not hash the Alpine package index", first)
	}
	if again := itImageTag(t, "0000000000000000000000000000000000000000000000000000000000000001"); again != first {
		t.Errorf("make -s it-image is not stable for one APK_INDEX_SUM: %s, then %s", first, again)
	}
}

// TestApkBaseImagesNamesTheDockerfileBase: the Dockerfile's FROM names global
// ARGs without defaults, so CI hands apk-index-sum.sh `make -s
// apk-base-images`; it must be the base the Dockerfile builds from.
func TestApkBaseImagesNamesTheDockerfileBase(t *testing.T) {
	out, err := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot, "apk-base-images").Output()
	if err != nil {
		t.Fatalf("make -s apk-base-images: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "golang:" + makeVar(t, "GO_VERSION") + "-trixie@" + makeVar(t, "GO_IMAGE_DIGEST")
	if got != want {
		t.Errorf("make -s apk-base-images = %q, want %q", got, want)
	}
	dockerfile := readFile(t, devcontainerDockerfilePath)
	if !strings.Contains(dockerfile, "FROM golang:${GO_VERSION}-trixie@${GO_IMAGE_DIGEST} AS base") {
		t.Errorf("%s no longer builds from golang:${GO_VERSION}-trixie@${GO_IMAGE_DIGEST}; "+
			"update the apk-base-images target", devcontainerDockerfilePath)
	}
}

// makeVar prints one Makefile variable.
func makeVar(t *testing.T, name string) string {
	t.Helper()
	const goal = "print-apk-test-var"
	out, err := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot,
		"--eval", goal+": ; @echo $("+name+")", goal).Output()
	if err != nil {
		t.Fatalf("make: printing %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// TestDockerfileDeclaresApkIndexSumOnlyBeforeAnUpgrade: a stage that declares
// ARG APK_INDEX_SUM runs `apk upgrade --no-cache` after it, so a new sum takes
// the fixed packages; base and toolchain never declare it, so the toolchain
// tag does not move with Alpine. The devcontainer's base is Debian, which has
// no apk, so no stage declares it at all (ci-template v1.29: `none`).
func TestDockerfileDeclaresApkIndexSumOnlyBeforeAnUpgrade(t *testing.T) {
	const arg = "ARG APK_INDEX_SUM"
	stages := parseDockerStages(readFile(t, devcontainerDockerfilePath))
	if len(stages) == 0 {
		t.Fatalf("%s: found no stages", devcontainerDockerfilePath)
	}
	for _, s := range stages {
		at := strings.Index(s.body, arg)
		if at < 0 {
			continue
		}
		if s.name == "base" || s.name == "toolchain" {
			t.Errorf("%s: the %s stage declares %s; only the integration stage may",
				devcontainerDockerfilePath, s.name, arg)
		}
		if !strings.Contains(s.body[at:], "apk upgrade --no-cache") {
			t.Errorf("%s: the %s stage declares %s but runs no `apk upgrade --no-cache` after it",
				devcontainerDockerfilePath, s.name, arg)
		}
		if strings.Contains(readFile(t, devcontainerDockerfilePath), "-trixie@") {
			t.Errorf("%s: the %s stage declares %s on a Debian base, which has no apk",
				devcontainerDockerfilePath, s.name, arg)
		}
	}
}

// apkDockerStub stands in for docker: it logs the image each `docker run`
// names (third argument from the end, before `-c <probe>`) to
// APK_STUB_LOG, then prints `none`, prints APK_STUB_OUT or exits 3, as
// APK_STUB_MODE says.
const apkDockerStub = `#!/usr/bin/env bash
if [ -n "${APK_STUB_LOG:-}" ]; then printf '%s %s\n' "$1" "${@: -3:1}" >> "$APK_STUB_LOG"; fi
case "${APK_STUB_MODE:-}" in
  none) echo none ;;
  alpine) printf '%s\n' "$APK_STUB_OUT" ;;
  *) exit 3 ;;
esac
`

// apkIndexSumRun is one run of apk-index-sum.sh against the docker stub.
type apkIndexSumRun struct {
	stdout, stderr string
	code           int
}

// runApkIndexSum runs the real apk-index-sum.sh from the repo root with the
// docker stub first on PATH, env added to an environment that has no
// APK_BASE_IMAGES or APK_DOCKERFILE of its own.
func runApkIndexSum(t *testing.T, env []string, args ...string) apkIndexSumRun {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(apkDockerStub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{apkIndexSumScript}, args...)...)
	cmd.Dir = repoRoot
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "PATH="),
			strings.HasPrefix(kv, "APK_BASE_IMAGES="),
			strings.HasPrefix(kv, "APK_DOCKERFILE="):
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	run := apkIndexSumRun{}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %s: %v", apkIndexSumScript, err)
		}
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	return run
}

// TestApkIndexSumPrintsNoneForABaseWithoutApk: a Debian base, whose probe
// prints `none`, gives the `none` every image-building job hashes.
func TestApkIndexSumPrintsNoneForABaseWithoutApk(t *testing.T) {
	run := runApkIndexSum(t, []string{"APK_STUB_MODE=none", "APK_BASE_IMAGES=debian:trixie-slim"})
	if run.code != 0 || run.stdout != "none\n" {
		t.Fatalf("exit %d, stdout %q, want exit 0 and %q; stderr:\n%s", run.code, run.stdout, "none\n", run.stderr)
	}
}

// TestApkIndexSumFailsWhenTheProbeFails: a base image docker cannot run or
// update is a failure that names the image, never a sum or `none`.
func TestApkIndexSumFailsWhenTheProbeFails(t *testing.T) {
	run := runApkIndexSum(t, []string{"APK_STUB_MODE=fail", "APK_BASE_IMAGES=alpine:3.22"})
	if run.code != 1 {
		t.Errorf("exit %d, want 1", run.code)
	}
	if run.stdout != "" {
		t.Errorf("stdout %q, want nothing", run.stdout)
	}
	if !strings.Contains(run.stderr, "alpine:3.22") || !strings.Contains(run.stderr, "failed") {
		t.Errorf("stderr %q names neither the image nor the failure", run.stderr)
	}
}

// TestApkIndexSumHashesTheAlpineIndex: an Alpine probe's output gives a
// sha256 that is stable across runs and changes with an index checksum.
func TestApkIndexSumHashesTheAlpineIndex(t *testing.T) {
	const repos = "https://dl-cdn.alpinelinux.org/alpine/v3.22/community\n" +
		"https://dl-cdn.alpinelinux.org/alpine/v3.22/main\n"
	sum := func(index string) string {
		t.Helper()
		run := runApkIndexSum(t, []string{
			"APK_STUB_MODE=alpine",
			"APK_BASE_IMAGES=alpine:3.22",
			"APK_STUB_OUT=" + repos + index + " APKINDEX.abc123.tar.gz",
		})
		if run.code != 0 {
			t.Fatalf("exit %d, want 0; stderr:\n%s", run.code, run.stderr)
		}
		got := strings.TrimSuffix(run.stdout, "\n")
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) {
			t.Fatalf("stdout %q is not one sha256", run.stdout)
		}
		return got
	}
	first := sum(strings.Repeat("a", 64))
	if again := sum(strings.Repeat("a", 64)); again != first {
		t.Errorf("the same index gave %s then %s", first, again)
	}
	if changed := sum(strings.Repeat("b", 64)); changed == first {
		t.Errorf("a changed index checksum kept the sum %s", first)
	}
}

// TestApkIndexSumReadsTheExternalBaseFromTheDockerfile: a global ARG default
// is substituted, and an earlier stage and scratch are not probed.
func TestApkIndexSumReadsTheExternalBaseFromTheDockerfile(t *testing.T) {
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	body := "ARG BASE=alpine:3.22\nFROM ${BASE} AS build\nFROM build\nFROM scratch\n"
	if err := os.WriteFile(dockerfile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "docker.log")
	run := runApkIndexSum(t, []string{"APK_STUB_MODE=none", "APK_STUB_LOG=" + log}, dockerfile)
	if run.code != 0 {
		t.Fatalf("exit %d, want 0; stderr:\n%s", run.code, run.stderr)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("docker was never run: %v", err)
	}
	if got := string(calls); got != "run alpine:3.22\n" {
		t.Errorf("docker calls %q, want one `run` of alpine:3.22", got)
	}
}

// TestApkIndexSumFailsWithoutADockerfile: a Dockerfile path that does not
// exist, with no APK_BASE_IMAGES, is a failure.
func TestApkIndexSumFailsWithoutADockerfile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "Dockerfile")
	run := runApkIndexSum(t, []string{"APK_STUB_MODE=none"}, missing)
	if run.code != 1 {
		t.Errorf("exit %d, want 1", run.code)
	}
	if !strings.Contains(run.stderr, "no Dockerfile") {
		t.Errorf("stderr %q does not say there is no Dockerfile", run.stderr)
	}
}
