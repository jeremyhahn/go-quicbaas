package buildgates

import (
	"os"
	"os/exec"
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
